//go:build integration

package pcd1b4multidc

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

type cwm33CASResult struct {
	applied bool
	err     error
}

func waitForCWM33Latch(t *testing.T, path, hookSeen, casEntered string, casDone <-chan cwm33CASResult) []byte {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case result := <-casDone:
			seen, _ := os.ReadFile(hookSeen)
			entry, _ := os.ReadFile(casEntered)
			t.Fatalf("CW-M33 HEAD CAS returned before the Paxos latch: applied=%v err=%v entry=%q hook=%q", result.applied, result.err, entry, seen)
		default:
		}
		if data, err := os.ReadFile(path); err == nil {
			return data
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("CW-M33 timed out waiting for Cassandra's post-Paxos-accept latch at %s", path)
	return nil
}

func readCWM33Head(t *testing.T, database *gocql.Session, orgID, libraryID string, consistency gocql.Consistency) (string, error) {
	t.Helper()
	var head *string
	err := database.Query(`
		SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, libraryID).Consistency(consistency).Scan(&head)
	if err != nil {
		return "", err
	}
	if head == nil {
		return "", nil
	}
	return *head, nil
}

// TestPCD1B4StableAbsencePaxosRace3DC uses the test-only Cassandra latch at
// the exact boundary after a global-SERIAL HEAD CAS has observed H0 and its
// proposal has been accepted by Paxos, but before its commit RPC is sent.
//
// m33-barrier proves that a SERIAL read settles the accepted proposal before
// EACH_QUORUM absence can be considered. m33-no-barrier is the real-Cassandra
// mutation: it mints proof from EACH_QUORUM alone, releases the old coordinator,
// and must fail when H1 becomes effective after that proof.
func TestPCD1B4StableAbsencePaxosRace3DC(t *testing.T) {
	phase := strings.TrimSpace(os.Getenv(phaseEnv))
	if phase != "m33-barrier" && phase != "m33-no-barrier" {
		t.Skipf("%s=%q; run through the isolated 3-DC characterization script", phaseEnv, phase)
	}
	latchDir := strings.TrimSpace(os.Getenv("SESAMEFS_PCD1B4_PAXOS_LATCH_DIR"))
	if latchDir == "" {
		t.Fatal("SESAMEFS_PCD1B4_PAXOS_LATCH_DIR must name the shared Cassandra latch volume")
	}
	if err := os.MkdirAll(latchDir, 0o755); err != nil {
		t.Fatalf("create CW-M33 latch directory: %v", err)
	}

	na, eu := connect(t, "dc-na"), connect(t, "dc-eu")
	orgID, libraryID := uuid.NewString(), uuid.NewString()
	h0, h1 := uuid.NewString(), uuid.NewString()
	seedTS := time.Now().UTC().Add(-10 * time.Second).UnixMicro()
	if err := na.Session().Query(`
		INSERT INTO libraries (org_id, library_id, name, created_at, updated_at, head_commit_id)
		VALUES (?, ?, ?, ?, ?, ?) USING TIMESTAMP ?
	`, orgID, libraryID, "cw-m33-paxos-race", time.Now().UTC(), time.Now().UTC(), h0, seedTS).
		Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatalf("CW-M33 seed H0 at every DC: %v", err)
	}
	seedHead, err := readCWM33Head(t, na.Session(), orgID, libraryID, gocql.Serial)
	if err != nil || seedHead != h0 {
		t.Fatalf("CW-M33 setup failed to publish H0 before pausing Paxos: head=%q err=%v want=%s", seedHead, err, h0)
	}

	runID := uuid.NewString()
	armed := filepath.Join(latchDir, "armed")
	hookSeen := filepath.Join(latchDir, "hook-seen")
	casEntered := filepath.Join(latchDir, "cas-entered")
	claimed := filepath.Join(latchDir, "claimed-"+runID)
	entered := filepath.Join(latchDir, "entered-"+runID)
	release := filepath.Join(latchDir, "release-"+runID)
	for _, path := range []string{armed, hookSeen, casEntered, claimed, entered, release} {
		_ = os.Remove(path)
	}
	if err := os.WriteFile(armed, []byte(runID), 0o644); err != nil {
		t.Fatalf("arm Cassandra CW-M33 latch: %v", err)
	}
	defer func() {
		_ = os.WriteFile(release, []byte("continue"), 0o600)
		_ = os.Remove(armed)
		_ = os.Remove(claimed)
		_ = os.Remove(entered)
		_ = os.Remove(release)
	}()

	casDone := make(chan cwm33CASResult, 1)
	go func() {
		applied, err := na.Session().Query(`
			UPDATE libraries SET head_commit_id = ? WHERE org_id = ? AND library_id = ?
			IF head_commit_id = ?
		`, h1, orgID, libraryID, h0).
			Consistency(gocql.EachQuorum).SerialConsistency(gocql.Serial).ScanCAS()
		casDone <- cwm33CASResult{applied: applied, err: err}
	}()

	marker := waitForCWM33Latch(t, entered, hookSeen, casEntered, casDone)
	ballotText := strings.SplitN(string(marker), "\n", 2)[0]
	ballotTS, err := strconv.ParseInt(ballotText, 10, 64)
	if err != nil {
		t.Fatalf("CW-M33 Cassandra latch did not report the accepted Paxos ballot: %q (%v)", ballotText, err)
	}
	deleteTS := ballotTS - 1 // model a plain hard-delete timestamp behind the accepted ballot
	if deleteTS <= seedTS {
		t.Fatalf("CW-M33 timestamp ordering invalid: seed=%d delete=%d accepted ballot=%d", seedTS, deleteTS, ballotTS)
	}

	if err := eu.Session().Query(`
		DELETE FROM libraries USING TIMESTAMP ? WHERE org_id = ? AND library_id = ?
	`, deleteTS, orgID, libraryID).Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatalf("CW-M33 hard-delete libraries after the accepted HEAD proposal: %v", err)
	}

	proofMinted := false
	if phase == "m33-barrier" {
		// This global-SERIAL read sees and settles the accepted proposal before
		// the subsequent EACH_QUORUM row-absence check can mint authority.
		barrierHead, err := readCWM33Head(t, eu.Session(), orgID, libraryID, gocql.Serial)
		if err != nil || barrierHead != h1 {
			t.Fatalf("CW-M33 Paxos settlement barrier did not resolve old HEAD CAS: head=%q err=%v want H1=%s", barrierHead, err, h1)
		}
	}

	proofHead, proofErr := readCWM33Head(t, eu.Session(), orgID, libraryID, gocql.EachQuorum)
	if proofErr == nil {
		if proofHead == "" {
			t.Fatalf("CW-M33 EACH_QUORUM returned a present row with null HEAD")
		}
		proofMinted = false
	} else if errors.Is(proofErr, gocql.ErrNotFound) {
		proofMinted = true
	} else {
		t.Fatalf("CW-M33 global EACH_QUORUM proof read failed closed: %v", proofErr)
	}

	if phase == "m33-barrier" {
		if proofMinted {
			t.Fatalf("CW-M33 stable proof was minted despite the SERIAL barrier settling HEAD=%s", h1)
		}
	} else if !proofMinted {
		t.Fatalf("CW-M33 no-barrier mutation precondition: EACH_QUORUM must observe the tombstoned row absent, got HEAD=%q err=%v", proofHead, proofErr)
	}

	if err := os.WriteFile(release, []byte("continue"), 0o600); err != nil {
		t.Fatalf("release old CW-M33 HEAD CAS: %v", err)
	}
	select {
	case result := <-casDone:
		if result.err != nil || !result.applied {
			t.Fatalf("old CW-M33 HEAD CAS did not settle applied after resume: applied=%v err=%v", result.applied, result.err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("old CW-M33 HEAD CAS did not finish after releasing the Paxos latch")
	}

	finalHead, err := readCWM33Head(t, eu.Session(), orgID, libraryID, gocql.Serial)
	if err != nil {
		t.Fatalf("CW-M33 SERIAL settle after resuming old CAS: %v", err)
	}
	if phase == "m33-no-barrier" && proofMinted && finalHead == h1 {
		t.Fatalf("CW-M33: EACH_QUORUM-only proof coexisted with a resurrected HEAD after the old Paxos CAS resumed (proof=true HEAD=%s)", finalHead)
	}
	if phase == "m33-barrier" && (proofMinted || finalHead != h1) {
		t.Fatalf("CW-M33 settlement contract violated: proof=%v final HEAD=%q want %s", proofMinted, finalHead, h1)
	}
}

func pcd1b4RequireSerialAbsenceForProof() bool { return true }

// TestPCD1B4SerialProofReadPresenceRace3DC exercises the proof-issuance window
// after the SERIAL settlement barrier itself:
//
//	SERIAL observes H0
//	→ a new H0→H1 CAS is accepted and paused
//	→ hard delete
//	→ EACH_QUORUM sees absent
//
// The earlier SERIAL-present result must veto proof even though the later
// ordinary read sees absence. G22 removes exactly that predicate.
func TestPCD1B4SerialProofReadPresenceRace3DC(t *testing.T) {
	if phase := strings.TrimSpace(os.Getenv(phaseEnv)); phase != "m33-serial-presence" {
		t.Skipf("%s=%q; run through the isolated 3-DC characterization script", phaseEnv, phase)
	}
	latchDir := strings.TrimSpace(os.Getenv("SESAMEFS_PCD1B4_PAXOS_LATCH_DIR"))
	if latchDir == "" {
		t.Fatal("SESAMEFS_PCD1B4_PAXOS_LATCH_DIR must name the shared Cassandra latch volume")
	}
	if err := os.MkdirAll(latchDir, 0o755); err != nil {
		t.Fatalf("create CW-M33 latch directory: %v", err)
	}

	na, eu := connect(t, "dc-na"), connect(t, "dc-eu")
	orgID, libraryID := uuid.NewString(), uuid.NewString()
	h0, h1 := uuid.NewString(), uuid.NewString()
	seedTS := time.Now().UTC().Add(-10 * time.Second).UnixMicro()
	if err := na.Session().Query(`
		INSERT INTO libraries (org_id, library_id, name, created_at, updated_at, head_commit_id)
		VALUES (?, ?, ?, ?, ?, ?) USING TIMESTAMP ?
	`, orgID, libraryID, "cw-m33-serial-presence", time.Now().UTC(), time.Now().UTC(), h0, seedTS).
		Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatalf("CW-M33 seed H0 at every DC: %v", err)
	}
	serialHead, err := readCWM33Head(t, na.Session(), orgID, libraryID, gocql.Serial)
	if err != nil || serialHead != h0 {
		t.Fatalf("CW-M33 SERIAL proof-read must observe H0 before the new CAS: head=%q err=%v want=%s", serialHead, err, h0)
	}

	runID := uuid.NewString()
	armed := filepath.Join(latchDir, "armed")
	hookSeen := filepath.Join(latchDir, "hook-seen")
	casEntered := filepath.Join(latchDir, "cas-entered")
	claimed := filepath.Join(latchDir, "claimed-"+runID)
	entered := filepath.Join(latchDir, "entered-"+runID)
	release := filepath.Join(latchDir, "release-"+runID)
	for _, path := range []string{armed, hookSeen, casEntered, claimed, entered, release} {
		_ = os.Remove(path)
	}
	if err := os.WriteFile(armed, []byte(runID), 0o644); err != nil {
		t.Fatalf("arm Cassandra CW-M33 latch: %v", err)
	}
	defer func() {
		_ = os.WriteFile(release, []byte("continue"), 0o600)
		_ = os.Remove(armed)
		_ = os.Remove(claimed)
		_ = os.Remove(entered)
		_ = os.Remove(release)
	}()

	casDone := make(chan cwm33CASResult, 1)
	go func() {
		applied, err := na.Session().Query(`
			UPDATE libraries SET head_commit_id = ? WHERE org_id = ? AND library_id = ?
			IF head_commit_id = ?
		`, h1, orgID, libraryID, h0).
			Consistency(gocql.EachQuorum).SerialConsistency(gocql.Serial).ScanCAS()
		casDone <- cwm33CASResult{applied: applied, err: err}
	}()

	marker := waitForCWM33Latch(t, entered, hookSeen, casEntered, casDone)
	ballotText := strings.SplitN(string(marker), "\n", 2)[0]
	ballotTS, err := strconv.ParseInt(ballotText, 10, 64)
	if err != nil {
		t.Fatalf("CW-M33 Cassandra latch did not report the accepted Paxos ballot: %q (%v)", ballotText, err)
	}
	deleteTS := ballotTS - 1
	if deleteTS <= seedTS {
		t.Fatalf("CW-M33 timestamp ordering invalid: seed=%d delete=%d accepted ballot=%d", seedTS, deleteTS, ballotTS)
	}
	if err := eu.Session().Query(`
		DELETE FROM libraries USING TIMESTAMP ? WHERE org_id = ? AND library_id = ?
	`, deleteTS, orgID, libraryID).Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatalf("CW-M33 hard-delete after a post-barrier CAS was accepted: %v", err)
	}

	var eachQuorumHead *string
	eachQuorumErr := eu.Session().Query(`
		SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, libraryID).Consistency(gocql.EachQuorum).Scan(&eachQuorumHead)
	if !errors.Is(eachQuorumErr, gocql.ErrNotFound) {
		t.Fatalf("CW-M33 post-barrier race setup: EACH_QUORUM must see the hard-deleted row absent, head=%v err=%v", eachQuorumHead, eachQuorumErr)
	}
	serialReadAbsent := serialHead == ""
	proofMinted := errors.Is(eachQuorumErr, gocql.ErrNotFound) &&
		(!pcd1b4RequireSerialAbsenceForProof() || serialReadAbsent)

	if err := os.WriteFile(release, []byte("continue"), 0o600); err != nil {
		t.Fatalf("release post-barrier HEAD CAS: %v", err)
	}
	select {
	case result := <-casDone:
		if result.err != nil || !result.applied {
			t.Fatalf("post-barrier HEAD CAS did not settle applied: applied=%v err=%v", result.applied, result.err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("post-barrier HEAD CAS did not finish after releasing the Paxos latch")
	}
	finalHead, err := readCWM33Head(t, eu.Session(), orgID, libraryID, gocql.Serial)
	if err != nil {
		t.Fatalf("CW-M33 SERIAL settle after post-barrier CAS resumed: %v", err)
	}
	if proofMinted && finalHead == h1 {
		t.Fatalf("CW-M33: proof minted from EACH_QUORUM absence despite SERIAL read observing H0; new CAS restored H1 (proof=true HEAD=%s)", finalHead)
	}
	if proofMinted || finalHead != h1 {
		t.Fatalf("CW-M33 SERIAL-present contract violated: prior SERIAL HEAD=%s proof=%v final HEAD=%s", serialHead, proofMinted, finalHead)
	}
}

func TestPCD1B4SerialProofReadPresenceMutation3DC(t *testing.T) {
	if phase := strings.TrimSpace(os.Getenv(phaseEnv)); phase != "m33-serial-presence-mutation" {
		t.Skipf("%s=%q; run through the isolated 3-DC characterization script", phaseEnv, phase)
	}
	latchDir := strings.TrimSpace(os.Getenv("SESAMEFS_PCD1B4_PAXOS_LATCH_DIR"))
	if latchDir == "" {
		t.Fatal("SESAMEFS_PCD1B4_PAXOS_LATCH_DIR must name the shared Cassandra latch volume")
	}
	if err := os.MkdirAll(latchDir, 0o755); err != nil {
		t.Fatalf("create CW-M33 latch directory: %v", err)
	}

	na, eu := connect(t, "dc-na"), connect(t, "dc-eu")
	orgID, libraryID := uuid.NewString(), uuid.NewString()
	h0, h1 := uuid.NewString(), uuid.NewString()
	seedTS := time.Now().UTC().Add(-10 * time.Second).UnixMicro()
	if err := na.Session().Query(`
		INSERT INTO libraries (org_id, library_id, name, created_at, updated_at, head_commit_id)
		VALUES (?, ?, ?, ?, ?, ?) USING TIMESTAMP ?
	`, orgID, libraryID, "cw-m33-serial-presence", time.Now().UTC(), time.Now().UTC(), h0, seedTS).
		Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatalf("CW-M33 seed H0 at every DC: %v", err)
	}
	serialHead, err := readCWM33Head(t, na.Session(), orgID, libraryID, gocql.Serial)
	if err != nil || serialHead != h0 {
		t.Fatalf("CW-M33 SERIAL proof-read must observe the pre-existing H0: head=%q err=%v want=%s", serialHead, err, h0)
	}

	runID := uuid.NewString()
	armed := filepath.Join(latchDir, "armed")
	hookSeen := filepath.Join(latchDir, "hook-seen")
	casEntered := filepath.Join(latchDir, "cas-entered")
	claimed := filepath.Join(latchDir, "claimed-"+runID)
	entered := filepath.Join(latchDir, "entered-"+runID)
	release := filepath.Join(latchDir, "release-"+runID)
	for _, path := range []string{armed, hookSeen, casEntered, claimed, entered, release} {
		_ = os.Remove(path)
	}
	if err := os.WriteFile(armed, []byte(runID), 0o644); err != nil {
		t.Fatalf("arm Cassandra CW-M33 latch: %v", err)
	}
	defer func() {
		_ = os.WriteFile(release, []byte("continue"), 0o600)
		_ = os.Remove(armed)
		_ = os.Remove(claimed)
		_ = os.Remove(entered)
		_ = os.Remove(release)
	}()

	casDone := make(chan cwm33CASResult, 1)
	go func() {
		applied, err := na.Session().Query(`
			UPDATE libraries SET head_commit_id = ? WHERE org_id = ? AND library_id = ?
			IF head_commit_id = ?
		`, h1, orgID, libraryID, h0).
			Consistency(gocql.EachQuorum).SerialConsistency(gocql.Serial).ScanCAS()
		casDone <- cwm33CASResult{applied: applied, err: err}
	}()

	marker := waitForCWM33Latch(t, entered, hookSeen, casEntered, casDone)
	ballotText := strings.SplitN(string(marker), "\n", 2)[0]
	ballotTS, err := strconv.ParseInt(ballotText, 10, 64)
	if err != nil {
		t.Fatalf("CW-M33 Cassandra latch did not report the accepted Paxos ballot: %q (%v)", ballotText, err)
	}
	deleteTS := ballotTS - 1
	if deleteTS <= seedTS {
		t.Fatalf("CW-M33 timestamp ordering invalid: seed=%d delete=%d accepted ballot=%d", seedTS, deleteTS, ballotTS)
	}
	if err := eu.Session().Query(`
		DELETE FROM libraries USING TIMESTAMP ? WHERE org_id = ? AND library_id = ?
	`, deleteTS, orgID, libraryID).Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatalf("CW-M33 hard-delete libraries after the post-barrier CAS was accepted: %v", err)
	}

	var eqHead *string
	eqErr := eu.Session().Query(`
		SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, libraryID).Consistency(gocql.EachQuorum).Scan(&eqHead)
	eachQuorumAbsent := errors.Is(eqErr, gocql.ErrNotFound)
	if !eachQuorumAbsent {
		t.Fatalf("CW-M33 post-delete EACH_QUORUM precondition: expected absence, head=%v err=%v", eqHead, eqErr)
	}
	proofMinted := eachQuorumAbsent && (!pcd1b4RequireSerialAbsenceForProof() || serialHead == "")

	if err := os.WriteFile(release, []byte("continue"), 0o600); err != nil {
		t.Fatalf("release post-barrier old HEAD CAS: %v", err)
	}
	select {
	case result := <-casDone:
		if result.err != nil || !result.applied {
			t.Fatalf("post-barrier old HEAD CAS did not settle applied: applied=%v err=%v", result.applied, result.err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("post-barrier old HEAD CAS did not finish after releasing the Paxos latch")
	}

	finalHead, err := readCWM33Head(t, eu.Session(), orgID, libraryID, gocql.Serial)
	if err != nil {
		t.Fatalf("CW-M33 settle post-barrier HEAD CAS: %v", err)
	}
	if proofMinted && finalHead == h1 {
		t.Fatalf("CW-M33: proof minted from EACH_QUORUM absence despite SERIAL read observing H0; new CAS restored H1 (proof=true HEAD=%s)", finalHead)
	}
	if proofMinted || finalHead != h1 {
		t.Fatalf("CW-M33 SERIAL-present contract violated: serial HEAD=%s proof=%v final HEAD=%s", serialHead, proofMinted, finalHead)
	}
}
