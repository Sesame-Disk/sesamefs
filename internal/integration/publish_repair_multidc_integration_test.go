//go:build integration

package integration

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	v2api "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	"github.com/Sesame-Disk/sesamefs/internal/config"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

const (
	w2PostHeadMultidcEvidenceEnv = "SESAMEFS_REQUIRE_W2_POST_HEAD_MULTIDC_EVIDENCE"
	w2PostHeadMultidcEndpoints   = "W2_POST_HEAD_3DC_HOSTS"
)

var w2PostHeadMultidcEvidence bool

func w2PostHead3DCEndpoints(t *testing.T) map[string]string {
	t.Helper()

	raw := strings.TrimSpace(os.Getenv(w2PostHeadMultidcEndpoints))
	if raw == "" {
		t.Fatalf("%s must name dc-na, dc-eu, and dc-asia", w2PostHeadMultidcEndpoints)
	}
	endpoints := make(map[string]string)
	for _, entry := range strings.Split(raw, ",") {
		dc, host, ok := strings.Cut(strings.TrimSpace(entry), "=")
		dc, host = strings.TrimSpace(dc), strings.TrimSpace(host)
		if !ok || dc == "" || host == "" {
			t.Fatalf("malformed %s entry %q; want dc=host:port", w2PostHeadMultidcEndpoints, entry)
		}
		endpoints[dc] = host
	}
	for _, dc := range []string{"dc-na", "dc-eu", "dc-asia"} {
		if strings.TrimSpace(endpoints[dc]) == "" {
			t.Fatalf("%s is missing %s", w2PostHeadMultidcEndpoints, dc)
		}
	}
	return endpoints
}

func w2PostHead3DCConnect(t *testing.T, dc string, endpoints map[string]string) *dbpkg.DB {
	t.Helper()
	return w2PostHead3DCConnectSerial(t, dc, endpoints, "SERIAL")
}

func w2PostHead3DCConnectSerial(t *testing.T, dc string, endpoints map[string]string, serialConsistency string) *dbpkg.DB {
	t.Helper()

	database, err := dbpkg.New(config.DatabaseConfig{
		Hosts:             []string{endpoints[dc]},
		Keyspace:          envOrDefault("CASSANDRA_KEYSPACE", "sesamefs"),
		Consistency:       "LOCAL_QUORUM",
		SerialConsistency: serialConsistency,
		LocalDC:           dc,
		ReplicationClass:  "NetworkTopologyStrategy",
		ReplicationDCs: map[string]int{
			"dc-na":   1,
			"dc-eu":   1,
			"dc-asia": 1,
		},
		Username: os.Getenv("CASSANDRA_USERNAME"),
		Password: os.Getenv("CASSANDRA_PASSWORD"),
	})
	if err != nil {
		t.Fatalf("connect to %s: %v", dc, err)
	}
	t.Cleanup(database.Close)
	return database
}

// w2PostHeadRetryEachQuorum retries op while it fails with the transient
// errors a DC that just returned (or is still stopped) produces on a global
// round — unavailable, read/write timeout, "received only N responses". The
// seeds and the EACH_QUORUM presence reads below go through it: a node that
// gossip already reports UN can still time out its first cross-DC reads for
// a few seconds after a restart, and that fixture warm-up must not be read
// as evidence. Legs that assert an outage read at LOCAL_QUORUM and never
// retry through it. Any other error, or a transient one that outlives the
// deadline, fails the test.
func w2PostHeadRetryEachQuorum(t *testing.T, what string, op func() error) {
	t.Helper()
	var err error
	deadline := time.Now().Add(45 * time.Second)
	for {
		err = op()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %v", what, err)
		}
		var unavailable *gocql.RequestErrUnavailable
		var writeTimeout *gocql.RequestErrWriteTimeout
		var readTimeout *gocql.RequestErrReadTimeout
		msg := strings.ToLower(err.Error())
		if !errors.As(err, &unavailable) && !errors.As(err, &writeTimeout) && !errors.As(err, &readTimeout) &&
			!strings.Contains(msg, "received only") && !strings.Contains(msg, "timed out") {
			t.Fatalf("%s: %v", what, err)
		}
		time.Sleep(2 * time.Second)
	}
}

// w2PostHeadPresent runs one presence read through w2PostHeadRetryEachQuorum:
// found is the read's result, ErrNotFound is absence (not an error), and a
// transient cross-DC failure is retried instead of being reported as either.
func w2PostHeadPresent(t *testing.T, what string, read func() error) bool {
	t.Helper()
	found := false
	w2PostHeadRetryEachQuorum(t, what, func() error {
		err := read()
		if errors.Is(err, gocql.ErrNotFound) {
			found = false
			return nil
		}
		found = err == nil
		return err
	})
	return found
}

func w2PostHead3DCIDs(t *testing.T) (orgID, repoID, parentID string) {
	t.Helper()
	for name, value := range map[string]string{
		"W2_POST_HEAD_ORG":    os.Getenv("W2_POST_HEAD_ORG"),
		"W2_POST_HEAD_REPO":   os.Getenv("W2_POST_HEAD_REPO"),
		"W2_POST_HEAD_PARENT": os.Getenv("W2_POST_HEAD_PARENT"),
	} {
		if strings.TrimSpace(value) == "" {
			t.Fatalf("%s is required", name)
		}
	}
	return os.Getenv("W2_POST_HEAD_ORG"), os.Getenv("W2_POST_HEAD_REPO"), os.Getenv("W2_POST_HEAD_PARENT")
}

// TestW2PostHeadSeedGlobalBaseFor3DC creates the stale base HEAD in every DC.
// The runner script invokes this before it stops dc-na and dc-asia.
func TestW2PostHeadSeedGlobalBaseFor3DC(t *testing.T) {
	if os.Getenv("W2_POST_HEAD_SEED_BASE") != "1" {
		t.Skip("W2_POST_HEAD_SEED_BASE is not set")
	}
	endpoints := w2PostHead3DCEndpoints(t)
	database := w2PostHead3DCConnect(t, "dc-na", endpoints)
	orgID, repoID, parentID := uuid.NewString(), uuid.NewString(), "w2-3dc-parent-"+uuid.NewString()
	now := time.Now().UTC()

	// Freshly started DCs can still miss a replica for a few seconds even
	// after the readiness probes pass; these are fixture writes, not the
	// classifier under test, so retry the EACH_QUORUM seed like the later
	// post-rejoin setup writes.
	w2PostHeadRetryEachQuorum(t, "seed global base library", func() error {
		return database.Session().Query(`
			INSERT INTO libraries (org_id, library_id, name, head_commit_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, orgID, repoID, "w2-post-head-3dc", parentID, now, now).Consistency(gocql.EachQuorum).Exec()
	})
	rootFSID := "w2-3dc-root-" + uuid.NewString()
	w2PostHeadRetryEachQuorum(t, "seed global base commit", func() error {
		return database.Session().Query(`
			INSERT INTO commits (library_id, commit_id, parent_id, root_fs_id, description, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, repoID, parentID, "", rootFSID, "w2 3dc base", now).Consistency(gocql.EachQuorum).Exec()
	})
	t.Logf("W2_POST_HEAD_ORG=%s", orgID)
	t.Logf("W2_POST_HEAD_REPO=%s", repoID)
	t.Logf("W2_POST_HEAD_PARENT=%s", parentID)
}

// TestW2PostHeadWriteRemoteCommitFor3DC publishes the new HEAD only in dc-eu
// while the other two datacenters are stopped and hinted handoff is disabled.
func TestW2PostHeadWriteRemoteCommitFor3DC(t *testing.T) {
	if os.Getenv("W2_POST_HEAD_WRITE_REMOTE") != "1" {
		t.Skip("W2_POST_HEAD_WRITE_REMOTE is not set")
	}
	endpoints := w2PostHead3DCEndpoints(t)
	database := w2PostHead3DCConnect(t, "dc-eu", endpoints)
	orgID, repoID, parentID := w2PostHead3DCIDs(t)
	commitID := "w2-3dc-remote-" + uuid.NewString()
	now := time.Now().UTC()

	if err := database.Session().Query(`
		INSERT INTO commits (library_id, commit_id, parent_id, root_fs_id, description, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, repoID, commitID, parentID, "w2-3dc-root-"+uuid.NewString(), "w2 3dc remote head", now).Consistency(gocql.LocalQuorum).Exec(); err != nil {
		t.Fatalf("seed remote commit in dc-eu: %v", err)
	}
	casState := map[string]interface{}{}
	applied, err := database.Session().Query(`
		UPDATE libraries SET head_commit_id = ?, updated_at = ?
		WHERE org_id = ? AND library_id = ?
		IF head_commit_id = ?
		`, commitID, now, orgID, repoID, parentID).
		Consistency(gocql.LocalQuorum).
		SerialConsistency(gocql.LocalSerial).
		MapScanCAS(casState)
	if err != nil {
		t.Fatalf("publish remote HEAD CAS in dc-eu: %v (state=%v)", err, casState)
	}
	if !applied {
		t.Fatalf("remote HEAD CAS lost unexpectedly: state=%v", casState)
	}
	t.Logf("W2_POST_HEAD_COMMIT=%s", commitID)
}

// TestW2PostHeadRepairDoesNotMisclassifyRemoteHead3DC proves the minimum
// multi-DC contract for this slice: a local blind read must never turn a
// commit published in another DC into cleanup-authorizing evidence.
func TestW2PostHeadRepairDoesNotMisclassifyRemoteHead3DC(t *testing.T) {
	if os.Getenv(w2PostHeadMultidcEvidenceEnv) != "1" {
		t.Skipf("%s is not set", w2PostHeadMultidcEvidenceEnv)
	}
	endpoints := w2PostHead3DCEndpoints(t)
	database := w2PostHead3DCConnect(t, "dc-na", endpoints)
	orgID, repoID, parentID := w2PostHead3DCIDs(t)
	commitID := strings.TrimSpace(os.Getenv("W2_POST_HEAD_COMMIT"))
	if commitID == "" {
		t.Fatal("W2_POST_HEAD_COMMIT is required")
	}
	var localHead string
	if err := database.Session().Query(`
		SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, repoID).Consistency(gocql.LocalQuorum).Scan(&localHead); err != nil {
		t.Fatalf("read local stale HEAD from dc-na: %v", err)
	}
	if localHead != parentID {
		t.Fatalf("fixture is not divergent: dc-na local HEAD=%q, want stale parent %q", localHead, parentID)
	}

	outcome, err := v2api.PublishedBlockReferenceRepairCommitOutcomeForIntegration(database, orgID, repoID, commitID)
	if outcome != "reachable" && outcome != "unknown" {
		t.Fatalf("unexpected W2 3DC repair outcome %q; local blindness must never authorize cleanup (err=%v)", outcome, err)
	}
	if err != nil {
		t.Logf("W2 3DC safe fail-closed outcome=%s with confirmation error: %v", outcome, err)
	} else {
		t.Logf("W2 3DC remote publication outcome=%s; local LOCAL_QUORUM remained blind", outcome)
	}
	w2PostHeadMultidcEvidence = true
}

// TestW2PostHeadAdvanceRemoteCommitFor3DC converges the deliberately local-only
// publication used by the blindness leg, then advances HEAD once more through a
// SERIAL CAS. The target remains a validated ancestor of the new HEAD.
func TestW2PostHeadAdvanceRemoteCommitFor3DC(t *testing.T) {
	if os.Getenv("W2_POST_HEAD_ADVANCE") != "1" {
		t.Skip("W2_POST_HEAD_ADVANCE is not set")
	}
	endpoints := w2PostHead3DCEndpoints(t)
	database := w2PostHead3DCConnect(t, "dc-na", endpoints)
	orgID, repoID, _ := w2PostHead3DCIDs(t)
	targetCommitID := strings.TrimSpace(os.Getenv("W2_POST_HEAD_COMMIT"))
	if targetCommitID == "" {
		t.Fatal("W2_POST_HEAD_COMMIT is required")
	}
	now := time.Now().UTC()
	w2PostHeadRetryEachQuorum(t, "converge remote publication before advancement", func() error {
		return database.Session().Query(`
			UPDATE libraries SET head_commit_id = ?, updated_at = ?
			WHERE org_id = ? AND library_id = ?
		`, targetCommitID, now, orgID, repoID).Consistency(gocql.EachQuorum).Exec()
	})

	advancedCommitID := "w2-3dc-advanced-" + uuid.NewString()
	w2PostHeadRetryEachQuorum(t, "seed advanced commit with EACH_QUORUM", func() error {
		return database.Session().Query(`
			INSERT INTO commits (library_id, commit_id, parent_id, root_fs_id, description, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, repoID, advancedCommitID, targetCommitID, "w2-3dc-root-"+uuid.NewString(), "w2 3dc advanced head", now).Consistency(gocql.EachQuorum).Exec()
	})
	state := map[string]interface{}{}
	applied, err := database.Session().Query(`
		UPDATE libraries SET head_commit_id = ?, updated_at = ?
		WHERE org_id = ? AND library_id = ?
		IF head_commit_id = ?
	`, advancedCommitID, now, orgID, repoID, targetCommitID).
		Consistency(gocql.LocalQuorum).
		SerialConsistency(gocql.Serial).
		MapScanCAS(state)
	if err != nil || !applied {
		t.Fatalf("advance canonical HEAD: applied=%v err=%v state=%v", applied, err, state)
	}
	t.Logf("W2_POST_HEAD_ADVANCED_COMMIT=%s", advancedCommitID)
}

func TestW2PostHeadAncestorAfterAdvancementIsReachable3DC(t *testing.T) {
	if os.Getenv("W2_POST_HEAD_VERIFY_ADVANCED") != "1" {
		t.Skip("W2_POST_HEAD_VERIFY_ADVANCED is not set")
	}
	endpoints := w2PostHead3DCEndpoints(t)
	database := w2PostHead3DCConnect(t, "dc-eu", endpoints)
	orgID, repoID, _ := w2PostHead3DCIDs(t)
	targetCommitID := strings.TrimSpace(os.Getenv("W2_POST_HEAD_COMMIT"))
	advancedCommitID := strings.TrimSpace(os.Getenv("W2_POST_HEAD_ADVANCED_COMMIT"))
	if targetCommitID == "" || advancedCommitID == "" {
		t.Fatal("W2_POST_HEAD_COMMIT and W2_POST_HEAD_ADVANCED_COMMIT are required")
	}
	if targetCommitID == advancedCommitID {
		t.Fatalf("advanced HEAD must be distinct from target commit %q", targetCommitID)
	}
	var observedHead string
	if err := database.Session().Query(`
		SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, repoID).Consistency(gocql.EachQuorum).Scan(&observedHead); err != nil {
		t.Fatalf("read advanced HEAD with EACH_QUORUM: %v", err)
	}
	if strings.TrimSpace(observedHead) != advancedCommitID {
		t.Fatalf("advanced HEAD was not observed from dc-eu: got %q, want %q", observedHead, advancedCommitID)
	}
	outcome, err := v2api.PublishedBlockReferenceRepairCommitOutcomeForIntegration(database, orgID, repoID, targetCommitID)
	if err != nil || outcome != "reachable" {
		t.Fatalf("advanced HEAD ancestor classification = (%q, %v), want reachable", outcome, err)
	}
}

func TestW2PostHeadUnavailableDCIsUnknownAndRetained3DC(t *testing.T) {
	if os.Getenv("W2_POST_HEAD_VERIFY_UNAVAILABLE") != "1" {
		t.Skip("W2_POST_HEAD_VERIFY_UNAVAILABLE is not set")
	}
	endpoints := w2PostHead3DCEndpoints(t)
	database := w2PostHead3DCConnect(t, "dc-na", endpoints)
	orgID, repoID, _ := w2PostHead3DCIDs(t)
	targetCommitID := strings.TrimSpace(os.Getenv("W2_POST_HEAD_COMMIT"))
	advancedCommitID := strings.TrimSpace(os.Getenv("W2_POST_HEAD_ADVANCED_COMMIT"))
	if targetCommitID == "" || advancedCommitID == "" {
		t.Fatal("W2_POST_HEAD_COMMIT and W2_POST_HEAD_ADVANCED_COMMIT are required")
	}
	if targetCommitID == advancedCommitID {
		t.Fatalf("advanced HEAD must be distinct from target commit %q", targetCommitID)
	}
	var observedHead string
	if err := database.Session().Query(`
		SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, repoID).Consistency(gocql.LocalQuorum).Scan(&observedHead); err != nil {
		t.Fatalf("read local advanced HEAD before DC outage: %v", err)
	}
	if strings.TrimSpace(observedHead) != advancedCommitID {
		t.Fatalf("dc-na HEAD was not advanced before DC outage: got %q, want %q", observedHead, advancedCommitID)
	}
	var advancedParent string
	ancestryErr := database.Session().Query(`
		SELECT parent_id FROM commits WHERE library_id = ? AND commit_id = ?
	`, repoID, advancedCommitID).Consistency(gocql.EachQuorum).Scan(&advancedParent)
	if ancestryErr == nil {
		t.Fatal("EACH_QUORUM ancestry read unexpectedly succeeded with dc-asia unavailable")
	}
	var unavailableErr *gocql.RequestErrUnavailable
	var readTimeoutErr *gocql.RequestErrReadTimeout
	if !errors.As(ancestryErr, &unavailableErr) && !errors.As(ancestryErr, &readTimeoutErr) {
		t.Fatalf("EACH_QUORUM ancestry read failed for an unexpected reason: %T: %v", ancestryErr, ancestryErr)
	}
	fsID := "w2-3dc-retained-" + uuid.NewString()
	blockID := "w2-3dc-block-" + uuid.NewString()
	if err := v2api.QueuePublishedFSObjectBlockReferenceRepair(database, orgID, repoID, targetCommitID, fsID, []string{blockID}); err != nil {
		t.Fatalf("queue repair before unavailable-DC classification: %v", err)
	}
	bucket := publishRepairIntegrationBucket(orgID, repoID, targetCommitID, fsID)
	t.Cleanup(func() {
		_ = v2api.ClearPublishedFSObjectBlockReferenceRepair(database, orgID, repoID, targetCommitID, fsID)
	})

	outcome, classifyErr := v2api.PublishedBlockReferenceRepairCommitOutcomeForIntegration(database, orgID, repoID, targetCommitID)
	if outcome != "unknown" || classifyErr == nil {
		t.Fatalf("classification with one DC unavailable = (%q, %v), want UNKNOWN with an authority error", outcome, classifyErr)
	}
	err := v2api.RepairPublishedFSObjectBlockReferenceRepair(database, orgID, repoID, targetCommitID, fsID, []string{blockID})
	if err == nil {
		t.Fatal("classification with one DC unavailable unexpectedly succeeded")
	}
	var storedFSID string
	if readErr := database.Session().Query(`
		SELECT fs_id FROM published_block_reference_repairs
		WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
	`, bucket, orgID, repoID, targetCommitID, fsID).Consistency(gocql.LocalQuorum).Scan(&storedFSID); readErr != nil || storedFSID != fsID {
		t.Fatalf("UNKNOWN classification did not retain repair: fs_id=%q err=%v", storedFSID, readErr)
	}
}

func TestW2PostHeadResumableCursorRetainsProgressWhileDCUnavailable3DC(t *testing.T) {
	if os.Getenv("W2_POST_HEAD_VERIFY_CURSOR_OUTAGE") != "1" {
		t.Skip("W2_POST_HEAD_VERIFY_CURSOR_OUTAGE is not set")
	}
	endpoints := w2PostHead3DCEndpoints(t)
	database := w2PostHead3DCConnect(t, "dc-na", endpoints)
	orgID, repoID, _ := w2PostHead3DCIDs(t)
	targetCommitID := strings.TrimSpace(os.Getenv("W2_POST_HEAD_COMMIT"))
	advancedCommitID := strings.TrimSpace(os.Getenv("W2_POST_HEAD_ADVANCED_COMMIT"))
	if targetCommitID == "" || advancedCommitID == "" {
		t.Fatal("W2_POST_HEAD_COMMIT and W2_POST_HEAD_ADVANCED_COMMIT are required")
	}
	fsID := "w2-3dc-cursor-" + uuid.NewString()
	blockID := "w2-3dc-cursor-block-" + uuid.NewString()
	if err := v2api.QueuePublishedFSObjectBlockReferenceRepair(database, orgID, repoID, targetCommitID, fsID, []string{blockID}); err != nil {
		t.Fatalf("queue resumable repair before DC outage: %v", err)
	}
	// Each call is one worker visit. Right after a DC drops, the SERIAL HEAD
	// read or the anchor LWT can time out before the coordinator marks that
	// DC down; the classifier returns UNKNOWN and the next visit retries,
	// which is exactly what the resumable design promises. Model a few
	// visits, but only across timeout/unavailable-class errors: any other
	// error, or a settlement, is a failure.
	var anchor, cursor string
	deadline := time.Now().Add(45 * time.Second)
	for {
		err := v2api.RepairPublishedFSObjectBlockReferenceRepair(database, orgID, repoID, targetCommitID, fsID, []string{blockID})
		if err == nil {
			t.Fatal("resumable repair settled while a datacenter was unavailable")
		}
		t.Logf("visit with dc-asia unavailable: %v", err)
		var progressErr error
		anchor, cursor, progressErr = v2api.PublishedBlockReferenceRepairProgressForIntegration(database, orgID, repoID, targetCommitID, fsID)
		if progressErr != nil {
			t.Fatalf("repair row disappeared during outage: %v", progressErr)
		}
		if strings.TrimSpace(anchor) != "" {
			break
		}
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "timed out") && !strings.Contains(msg, "unavailable") && !strings.Contains(msg, "received only") {
			t.Fatalf("SERIAL reachability anchor was not persisted during outage and the visit did not fail on availability: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("SERIAL reachability anchor was not persisted during outage after repeated visits: %v", err)
		}
		time.Sleep(2 * time.Second)
	}
	if cursor != "" && cursor != anchor {
		t.Fatalf("outage advanced the cursor past the unread node: cursor=%q anchor=%q", cursor, anchor)
	}
	t.Logf("W2_POST_HEAD_CURSOR_FSID=%s", fsID)
	t.Logf("W2_POST_HEAD_CURSOR_BLOCK=%s", blockID)
	t.Logf("W2_POST_HEAD_CURSOR_ANCHOR=%s", anchor)
}

func TestW2PostHeadResumableCursorResumesAfterOutageAndIgnoresMovingHEAD3DC(t *testing.T) {
	if os.Getenv("W2_POST_HEAD_VERIFY_CURSOR_RESUME") != "1" {
		t.Skip("W2_POST_HEAD_VERIFY_CURSOR_RESUME is not set")
	}
	endpoints := w2PostHead3DCEndpoints(t)
	na := w2PostHead3DCConnect(t, "dc-na", endpoints)
	eu := w2PostHead3DCConnect(t, "dc-eu", endpoints)
	orgID, repoID, _ := w2PostHead3DCIDs(t)
	targetCommitID := strings.TrimSpace(os.Getenv("W2_POST_HEAD_COMMIT"))
	fsID := strings.TrimSpace(os.Getenv("W2_POST_HEAD_CURSOR_FSID"))
	blockID := strings.TrimSpace(os.Getenv("W2_POST_HEAD_CURSOR_BLOCK"))
	if targetCommitID == "" || fsID == "" || blockID == "" {
		t.Fatal("W2_POST_HEAD_COMMIT, W2_POST_HEAD_CURSOR_FSID, and W2_POST_HEAD_CURSOR_BLOCK are required")
	}
	anchorBefore, _, err := v2api.PublishedBlockReferenceRepairProgressForIntegration(na, orgID, repoID, targetCommitID, fsID)
	if err != nil {
		t.Fatalf("expected durable repair row before resume: %v", err)
	}
	if strings.TrimSpace(anchorBefore) == "" {
		t.Fatal("resume started without a durable SERIAL anchor")
	}
	movedHEAD := "w2-3dc-moved-" + uuid.NewString()
	now := time.Now().UTC()
	w2PostHeadRetryEachQuorum(t, "insert unrelated live HEAD", func() error {
		return na.Session().Query(`
			INSERT INTO commits (library_id, commit_id, parent_id, root_fs_id, description, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, repoID, movedHEAD, "", "w2-3dc-moved-root", "unrelated live HEAD", now).Consistency(gocql.EachQuorum).Exec()
	})
	if err := na.Session().Query(`
		UPDATE libraries SET head_commit_id = ? WHERE org_id = ? AND library_id = ?
	`, movedHEAD, orgID, repoID).Exec(); err != nil {
		t.Fatalf("move live HEAD: %v", err)
	}
	if anchorBefore == movedHEAD {
		t.Fatal("moved live HEAD collided with the durable anchor")
	}

	// One round is one concurrent visit from two DCs. A DC that just rejoined
	// can still miss an EACH_QUORUM response for a few seconds; the
	// classifier then fails closed (UNKNOWN + availability error) and the
	// next visit resumes from the same durable cursor. Repeat the pair only
	// across timeout/unavailable-class errors; anything else is a failure.
	deadline := time.Now().Add(45 * time.Second)
	for {
		outcomes := make(chan string, 2)
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for _, database := range []*dbpkg.DB{na, eu} {
			wg.Add(1)
			go func(database *dbpkg.DB) {
				defer wg.Done()
				outcome, err := v2api.ClassifyPublishedBlockReferenceRepairResumableForIntegration(database, orgID, repoID, targetCommitID, fsID)
				outcomes <- outcome
				errs <- err
			}(database)
		}
		wg.Wait()
		close(outcomes)
		close(errs)
		var availabilityErr error
		for err := range errs {
			if err == nil {
				continue
			}
			msg := strings.ToLower(err.Error())
			if strings.Contains(msg, "timed out") || strings.Contains(msg, "unavailable") || strings.Contains(msg, "received only") {
				availabilityErr = err
				continue
			}
			t.Fatalf("resume classify under moved HEAD: %v", err)
		}
		if availabilityErr != nil {
			t.Logf("concurrent resume visit failed closed on availability, retrying: %v", availabilityErr)
			if time.Now().After(deadline) {
				t.Fatalf("resume classify under moved HEAD kept failing on availability: %v", availabilityErr)
			}
			time.Sleep(2 * time.Second)
			continue
		}
		reachable := 0
		for outcome := range outcomes {
			if outcome != "reachable" {
				t.Fatalf("resume classify = %q, want reachable from the durable cursor", outcome)
			}
			reachable++
		}
		if reachable != 2 {
			t.Fatalf("concurrent resume reachable=%d, want 2", reachable)
		}
		break
	}
	anchorAfter, _, progressErr := v2api.PublishedBlockReferenceRepairProgressForIntegration(na, orgID, repoID, targetCommitID, fsID)
	if progressErr != nil {
		t.Fatalf("durable repair row missing after resume classify: %v", progressErr)
	}
	if anchorAfter != anchorBefore {
		t.Fatalf("anchor reset after moving HEAD: before=%q after=%q moved=%q", anchorBefore, anchorAfter, movedHEAD)
	}
	if err := v2api.ClearPublishedFSObjectBlockReferenceRepair(na, orgID, repoID, targetCommitID, fsID); err != nil {
		t.Fatalf("clear resumed repair: %v", err)
	}
}

// --- Cleanup-intent sweep authority in three DCs -----------------------------
//
// A write-ahead cleanup intent (published_repair_liveness_cleanups) can be
// visible in one DC before the repair row it belongs to has replicated there.
// The sweep decides "repair row gone => remove the producer-specific
// pub:<repo:commit:fsID>:<producer_token>" and is therefore destructive; its
// absence read must be EACH_QUORUM and fail closed. These legs build exactly
// that state: intent + pin written globally,
// the repair row written only in dc-eu with hinted handoff disabled, then the
// production sweep run from blind dc-na, and once more with dc-asia down.

func w2PostHeadCleanupIDs(t *testing.T) (fsID, blockID string) {
	t.Helper()
	fsID = strings.TrimSpace(os.Getenv("W2_POST_HEAD_CLEANUP_FSID"))
	blockID = strings.TrimSpace(os.Getenv("W2_POST_HEAD_CLEANUP_BLOCK"))
	if fsID == "" || blockID == "" {
		t.Fatal("W2_POST_HEAD_CLEANUP_FSID and W2_POST_HEAD_CLEANUP_BLOCK are required")
	}
	return fsID, blockID
}

func w2PostHeadCleanupTokens(t *testing.T, database *dbpkg.DB, consistency gocql.Consistency, orgID, repoID, commitID, fsID string) []string {
	t.Helper()
	bucket := v2api.PublishedBlockReferenceRepairBucketForIntegration(orgID, repoID, commitID, fsID)
	var tokens []string
	w2PostHeadRetryEachQuorum(t, fmt.Sprintf("read cleanup intents at %s", consistency), func() error {
		iter := database.Session().Query(`
			SELECT producer_token FROM published_repair_liveness_cleanups
			WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
		`, bucket, orgID, repoID, commitID, fsID).Consistency(consistency).Iter()
		var token string
		tokens = nil
		for iter.Scan(&token) {
			tokens = append(tokens, token)
		}
		return iter.Close()
	})
	return tokens
}

func w2PostHeadCleanupPinPresent(t *testing.T, database *dbpkg.DB, consistency gocql.Consistency, orgID, repoID, commitID, fsID, blockID string, producerToken ...string) bool {
	t.Helper()
	tokens := producerToken
	if len(tokens) == 0 {
		tokens = w2PostHeadCleanupTokens(t, database, consistency, orgID, repoID, commitID, fsID)
	}
	for _, token := range tokens {
		referrer := v2api.PublishedBlockReferenceRepairLivenessReferrerForIntegration(repoID, commitID, fsID, token)
		if w2PostHeadPresent(t, fmt.Sprintf("read repair-owned pin at %s", consistency), func() error {
			var got string
			return database.Session().Query(`
				SELECT referrer FROM block_references WHERE org_id = ? AND block_id = ? AND referrer = ?
			`, orgID, blockID, referrer).Consistency(consistency).Scan(&got)
		}) {
			return true
		}
	}
	return false
}

func w2PostHeadCleanupIntentPresent(t *testing.T, database *dbpkg.DB, consistency gocql.Consistency, orgID, repoID, commitID, fsID string) bool {
	t.Helper()
	return len(w2PostHeadCleanupTokens(t, database, consistency, orgID, repoID, commitID, fsID)) > 0
}

func w2PostHeadRepairRowPresent(t *testing.T, database *dbpkg.DB, consistency gocql.Consistency, orgID, repoID, commitID, fsID string) bool {
	t.Helper()
	bucket := publishRepairIntegrationBucket(orgID, repoID, commitID, fsID)
	return w2PostHeadPresent(t, fmt.Sprintf("read repair row at %s", consistency), func() error {
		var got string
		return database.Session().Query(`
			SELECT fs_id FROM published_block_reference_repairs
			WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
		`, bucket, orgID, repoID, commitID, fsID).Consistency(consistency).Scan(&got)
	})
}

// TestW2PostHeadSeedCleanupIntentFor3DC writes a cleanup intent and its
// repair-owned pin in every DC (EACH_QUORUM), with no repair row yet.
func TestW2PostHeadSeedCleanupIntentFor3DC(t *testing.T) {
	if os.Getenv("W2_POST_HEAD_CLEANUP_SEED") != "1" {
		t.Skip("W2_POST_HEAD_CLEANUP_SEED is not set")
	}
	endpoints := w2PostHead3DCEndpoints(t)
	database := w2PostHead3DCConnect(t, "dc-na", endpoints)
	orgID, repoID, _ := w2PostHead3DCIDs(t)
	commitID := strings.TrimSpace(os.Getenv("W2_POST_HEAD_COMMIT"))
	if commitID == "" {
		t.Fatal("W2_POST_HEAD_COMMIT is required")
	}
	fsID := "w2-3dc-cleanup-" + uuid.NewString()
	blockID := "w2-3dc-cleanup-block-" + uuid.NewString()
	bucket := v2api.PublishedBlockReferenceRepairBucketForIntegration(orgID, repoID, commitID, fsID)
	generation := time.Now().UTC().Truncate(time.Millisecond)
	producerToken := uuid.NewString()
	referrer := v2api.PublishedBlockReferenceRepairLivenessReferrerForIntegration(repoID, commitID, fsID, producerToken)
	// Armed: the producer's fan-out is over, so the intent is consumable and
	// the only thing standing between the sweep and the pin is the authority
	// read of the repair row.
	w2PostHeadRetryEachQuorum(t, "seed cleanup intent", func() error {
		return database.Session().Query(`
			INSERT INTO published_repair_liveness_cleanups (bucket, org_id, repo_id, commit_id, fs_id, producer_token, staged_block_ids, created_at, armed, lease_expires_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, true, ?)
		`, bucket, orgID, repoID, commitID, fsID, producerToken, []string{blockID}, generation, generation).Consistency(gocql.EachQuorum).Exec()
	})
	w2PostHeadRetryEachQuorum(t, "seed repair-owned pin", func() error {
		return database.Session().Query(`
			INSERT INTO block_references (org_id, block_id, referrer, library_id, created_at)
			VALUES (?, ?, ?, ?, ?) USING TTL ?
		`, orgID, blockID, referrer, repoID, generation, dbpkg.PublishAttemptReferenceTTLSeconds).Consistency(gocql.EachQuorum).Exec()
	})
	if !w2PostHeadCleanupIntentPresent(t, database, gocql.EachQuorum, orgID, repoID, commitID, fsID) || !w2PostHeadCleanupPinPresent(t, database, gocql.EachQuorum, orgID, repoID, commitID, fsID, blockID) {
		t.Fatal("seeded cleanup intent or pin not globally visible")
	}
	t.Logf("W2_POST_HEAD_CLEANUP_FSID=%s W2_POST_HEAD_CLEANUP_BLOCK=%s", fsID, blockID)
}

// TestW2PostHeadWriteRepairRowInSingleDC3DC queues the repair row from dc-eu
// while dc-na and dc-asia are stopped and hinted handoff is disabled, so the
// row exists only in dc-eu when they come back.
func TestW2PostHeadWriteRepairRowInSingleDC3DC(t *testing.T) {
	if os.Getenv("W2_POST_HEAD_CLEANUP_WRITE_ROW") != "1" {
		t.Skip("W2_POST_HEAD_CLEANUP_WRITE_ROW is not set")
	}
	endpoints := w2PostHead3DCEndpoints(t)
	database := w2PostHead3DCConnect(t, "dc-eu", endpoints)
	orgID, repoID, _ := w2PostHead3DCIDs(t)
	commitID := strings.TrimSpace(os.Getenv("W2_POST_HEAD_COMMIT"))
	fsID, blockID := w2PostHeadCleanupIDs(t)
	if err := v2api.QueuePublishedFSObjectBlockReferenceRepair(database, orgID, repoID, commitID, fsID, []string{blockID}); err != nil {
		t.Fatalf("queue repair row in dc-eu only: %v", err)
	}
	if !w2PostHeadRepairRowPresent(t, database, gocql.LocalQuorum, orgID, repoID, commitID, fsID) {
		t.Fatal("repair row not visible in dc-eu after the LOCAL_QUORUM write")
	}
}

// TestW2PostHeadCleanupIntentBlindDCDoesNotRemovePub3DC runs the production
// intent sweep from dc-na, which sees the intent and the pin but not the
// repair row at LOCAL_QUORUM. The EACH_QUORUM authority read must find the
// row in dc-eu and keep both the pin and the intent.
func TestW2PostHeadCleanupIntentBlindDCDoesNotRemovePub3DC(t *testing.T) {
	if os.Getenv("W2_POST_HEAD_CLEANUP_VERIFY_BLIND") != "1" {
		t.Skip("W2_POST_HEAD_CLEANUP_VERIFY_BLIND is not set")
	}
	endpoints := w2PostHead3DCEndpoints(t)
	database := w2PostHead3DCConnect(t, "dc-na", endpoints)
	orgID, repoID, _ := w2PostHead3DCIDs(t)
	commitID := strings.TrimSpace(os.Getenv("W2_POST_HEAD_COMMIT"))
	fsID, blockID := w2PostHeadCleanupIDs(t)

	if !w2PostHeadCleanupIntentPresent(t, database, gocql.LocalQuorum, orgID, repoID, commitID, fsID) {
		t.Fatal("dc-na does not see the cleanup intent; the fixture is not the intended interleaving")
	}
	if !w2PostHeadCleanupPinPresent(t, database, gocql.LocalQuorum, orgID, repoID, commitID, fsID, blockID) {
		t.Fatal("dc-na does not see the repair-owned pin; the fixture is not the intended interleaving")
	}
	if w2PostHeadRepairRowPresent(t, database, gocql.LocalQuorum, orgID, repoID, commitID, fsID) {
		t.Fatal("dc-na already sees the repair row at LOCAL_QUORUM; local blindness was not established (hinted handoff still enabled?)")
	}

	w2PostHeadRetryEachQuorum(t, "blind-DC cleanup sweep", func() error {
		return v2api.SweepPublishedBlockReferenceRepairLivenessCleanupsForIntegration(database, orgID, repoID, commitID, fsID)
	})

	if !w2PostHeadCleanupPinPresent(t, database, gocql.EachQuorum, orgID, repoID, commitID, fsID, blockID) {
		t.Fatal("blind dc-na sweep removed the repair-owned pin of a repair row still pending in dc-eu: local absence authorized destructive cleanup")
	}
	if !w2PostHeadCleanupIntentPresent(t, database, gocql.EachQuorum, orgID, repoID, commitID, fsID) {
		t.Fatal("blind dc-na sweep deleted the cleanup intent of a pending repair")
	}
	if !w2PostHeadRepairRowPresent(t, database, gocql.EachQuorum, orgID, repoID, commitID, fsID) {
		t.Fatal("repair row is not globally visible; the authority read had nothing to protect")
	}
	t.Log("W2 3DC cleanup sweep from blind dc-na kept the pin and the intent of a repair row visible only through EACH_QUORUM")
}

// TestW2PostHeadCleanupIntentUnavailableDCRetains3DC runs the sweep with one
// DC down: the EACH_QUORUM authority read must fail and the sweep must keep
// the pin and the intent (fail closed) rather than treat the failure as
// absence.
func TestW2PostHeadCleanupIntentUnavailableDCRetains3DC(t *testing.T) {
	if os.Getenv("W2_POST_HEAD_CLEANUP_VERIFY_UNAVAILABLE") != "1" {
		t.Skip("W2_POST_HEAD_CLEANUP_VERIFY_UNAVAILABLE is not set")
	}
	endpoints := w2PostHead3DCEndpoints(t)
	database := w2PostHead3DCConnect(t, "dc-na", endpoints)
	orgID, repoID, _ := w2PostHead3DCIDs(t)
	commitID := strings.TrimSpace(os.Getenv("W2_POST_HEAD_COMMIT"))
	fsID, blockID := w2PostHeadCleanupIDs(t)
	// The local read may only retain; this leg needs dc-na still blind so the
	// decision has to escalate to EACH_QUORUM, which dc-asia being down must
	// turn into a fail-closed error.
	if w2PostHeadRepairRowPresent(t, database, gocql.LocalQuorum, orgID, repoID, commitID, fsID) {
		t.Fatal("dc-na already sees the repair row at LOCAL_QUORUM; run this leg before any EACH_QUORUM read repaired it")
	}

	err := v2api.SweepPublishedBlockReferenceRepairLivenessCleanupsForIntegration(database, orgID, repoID, commitID, fsID)
	if err == nil {
		t.Fatal("cleanup sweep with one DC unavailable unexpectedly succeeded; absence must not be concluded from incomplete evidence")
	}
	if !strings.Contains(err.Error(), fsID) || !strings.Contains(err.Error(), "EACH_QUORUM") {
		t.Fatalf("cleanup sweep error = %v, want the EACH_QUORUM authority failure for %s", err, fsID)
	}
	if !w2PostHeadCleanupPinPresent(t, database, gocql.LocalQuorum, orgID, repoID, commitID, fsID, blockID) {
		t.Fatal("sweep removed the repair-owned pin while a DC was unavailable")
	}
	if !w2PostHeadCleanupIntentPresent(t, database, gocql.LocalQuorum, orgID, repoID, commitID, fsID) {
		t.Fatal("sweep deleted the cleanup intent while a DC was unavailable")
	}
}

// TestW2PostHeadStaleLeaseSweepLosesToExtend3DC is the cross-DC stale-lease
// interleaving: a sweeper in dc-na lists a PREPARING intent whose lease L1 has
// expired; while it holds that snapshot the producer, coordinated in dc-eu,
// extends the same intent to L2 and writes a pin under L2; the repair row is
// conclusively gone. The sweeper's claim must be the exact-lease freeze CAS,
// which competes with that EXTEND in one global Paxos domain: dc-na must lose
// it, must not tombstone at L1, and must not delete the witness. Once the
// sweeper's clock is past L2 and its listing is current it wins the freeze,
// tombstones at L2 and deletes the witness; the producer's later EXTEND and
// ARM from dc-eu are refused and a late pin under L2 stays shadowed.
func TestW2PostHeadStaleLeaseSweepLosesToExtend3DC(t *testing.T) {
	if os.Getenv("W2_POST_HEAD_STALE_LEASE") != "1" {
		t.Skip("W2_POST_HEAD_STALE_LEASE is not set")
	}
	endpoints := w2PostHead3DCEndpoints(t)
	sweeper := w2PostHead3DCConnect(t, "dc-na", endpoints)
	producer := w2PostHead3DCConnect(t, "dc-eu", endpoints)
	orgID, repoID, _ := w2PostHead3DCIDs(t)
	commitID := strings.TrimSpace(os.Getenv("W2_POST_HEAD_COMMIT"))
	if commitID == "" {
		t.Fatal("W2_POST_HEAD_COMMIT is required")
	}
	fsID := "w2-3dc-stale-lease-" + uuid.NewString()
	blockID := "w2-3dc-stale-lease-block-" + uuid.NewString()
	blocks := []string{blockID}
	l1 := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond) // already expired for every clock
	l2 := l1.Add(10 * time.Minute)
	l3 := l2.Add(10 * time.Minute)

	token := uuid.NewString()
	w2PostHeadRetryEachQuorum(t, "seed PREPARING(L1) producer from dc-eu", func() error {
		return v2api.RecordPreparingPublishedBlockReferenceRepairLivenessCleanupForIntegration(producer, orgID, repoID, commitID, fsID, blocks, token, l1)
	})
	w2PostHeadRetryEachQuorum(t, "seed pin under L1 from dc-eu", func() error {
		return v2api.WritePublishedBlockReferenceRepairLivenessPinForIntegration(producer, orgID, repoID, commitID, fsID, blockID, token, l1)
	})
	if !w2PostHeadCleanupIntentPresent(t, sweeper, gocql.EachQuorum, orgID, repoID, commitID, fsID) || !w2PostHeadCleanupPinPresent(t, sweeper, gocql.EachQuorum, orgID, repoID, commitID, fsID, blockID) {
		t.Fatal("seeded PREPARING intent or pin not globally visible")
	}
	if w2PostHeadRepairRowPresent(t, sweeper, gocql.EachQuorum, orgID, repoID, commitID, fsID) {
		t.Fatal("repair row unexpectedly present; this leg needs a conclusively gone row")
	}
	state := func(database *dbpkg.DB) v2api.LivenessCleanupStateForIntegration {
		t.Helper()
		s, err := v2api.PublishedBlockReferenceRepairLivenessCleanupStateForIntegration(database, orgID, repoID, commitID, fsID, token)
		if err != nil {
			t.Fatalf("read intent state: %v", err)
		}
		return s
	}

	// Stale snapshot in dc-na; EXTEND L1->L2 and a pin under L2 from dc-eu
	// while the snapshot is held.
	err := v2api.SweepPublishedBlockReferenceRepairLivenessCleanupsGatedAtForIntegration(sweeper, orgID, repoID, commitID, fsID, l1.Add(time.Second), func() {
		applied, err := v2api.ExtendPublishedBlockReferenceRepairLivenessCleanupForIntegration(producer, orgID, repoID, commitID, fsID, blocks, token, l1, l2)
		if err != nil || !applied {
			t.Fatalf("dc-eu EXTEND L1->L2 = applied=%v err=%v, want applied", applied, err)
		}
		if err := v2api.WritePublishedBlockReferenceRepairLivenessPinForIntegration(producer, orgID, repoID, commitID, fsID, blockID, token, l2); err != nil {
			t.Fatalf("dc-eu pin under L2: %v", err)
		}
	})
	if err != nil && strings.Contains(err.Error(), fsID) {
		t.Fatalf("dc-na sweep over the stale PREPARING(L1) snapshot: %v", err)
	}
	if s := state(sweeper); !s.Present || s.Armed || s.Consumed || !s.Lease.Equal(l2) {
		t.Fatalf("intent after the stale dc-na sweep = %+v, want PREPARING(L2) untouched: the stale freeze on L1 must lose to the dc-eu EXTEND", s)
	}
	if !w2PostHeadCleanupPinPresent(t, sweeper, gocql.EachQuorum, orgID, repoID, commitID, fsID, blockID) {
		t.Fatal("dc-na sweep with a stale L1 snapshot removed the pin of a producer that had extended to L2 in dc-eu")
	}

	// Current listing, clock past L2: dc-na wins the freeze and consumes.
	w2PostHeadRetryEachQuorum(t, "dc-na sweep past L2", func() error {
		return v2api.SweepPublishedBlockReferenceRepairLivenessCleanupsGatedAtForIntegration(sweeper, orgID, repoID, commitID, fsID, l2.Add(time.Second), nil)
	})
	if s := state(sweeper); !s.Present || !s.Armed || !s.Consumed {
		t.Fatalf("intent after the winning dc-na sweep = %+v, want CONSUMED (retired, not deleted: it must keep re-fencing past gc_grace)", s)
	}
	if w2PostHeadCleanupPinPresent(t, sweeper, gocql.EachQuorum, orgID, repoID, commitID, fsID, blockID) {
		t.Fatal("dc-na left the pin of the producer it froze")
	}
	if applied, err := v2api.ExtendPublishedBlockReferenceRepairLivenessCleanupForIntegration(producer, orgID, repoID, commitID, fsID, blocks, token, l2, l3); err != nil || applied {
		t.Fatalf("dc-eu EXTEND after the freeze = applied=%v err=%v, want not applied", applied, err)
	}
	if applied, err := v2api.ArmPublishedBlockReferenceRepairLivenessCleanupForIntegration(producer, orgID, repoID, commitID, fsID, blocks, token, l2); err != nil || applied {
		t.Fatalf("dc-eu ARM after the freeze = applied=%v err=%v, want not applied", applied, err)
	}
	if s := state(producer); !s.Present || !s.Consumed {
		t.Fatalf("a refused ARM from dc-eu changed the retired witness: %+v", s)
	}
	w2PostHeadRetryEachQuorum(t, "late dc-eu pin under L2", func() error {
		return v2api.WritePublishedBlockReferenceRepairLivenessPinForIntegration(producer, orgID, repoID, commitID, fsID, blockID, token, l2)
	})
	if w2PostHeadCleanupPinPresent(t, sweeper, gocql.EachQuorum, orgID, repoID, commitID, fsID, blockID) {
		t.Fatal("a late dc-eu write under L2 revived the pin after the freeze-authorized tombstone at L2")
	}
	t.Log("W2 3DC stale-lease: a dc-na sweeper holding an expired PREPARING(L1) snapshot lost the exact-lease freeze to a dc-eu EXTEND L1->L2 and removed nothing; past L2 it won the freeze, tombstoned at L2, retired the witness to CONSUMED, and the producer's later EXTEND/ARM/pin were fenced")
	t.Logf("W2_POST_HEAD_CONSUMED_FSID=%s W2_POST_HEAD_CONSUMED_BLOCK=%s W2_POST_HEAD_CONSUMED_TOKEN=%s", fsID, blockID, token)
}

func w2PostHeadConsumedIDs(t *testing.T) (fsID, blockID, token string) {
	t.Helper()
	fsID = strings.TrimSpace(os.Getenv("W2_POST_HEAD_CONSUMED_FSID"))
	blockID = strings.TrimSpace(os.Getenv("W2_POST_HEAD_CONSUMED_BLOCK"))
	token = strings.TrimSpace(os.Getenv("W2_POST_HEAD_CONSUMED_TOKEN"))
	if fsID == "" || blockID == "" || token == "" {
		t.Fatal("W2_POST_HEAD_CONSUMED_FSID, W2_POST_HEAD_CONSUMED_BLOCK and W2_POST_HEAD_CONSUMED_TOKEN are required")
	}
	return fsID, blockID, token
}

// TestW2PostHeadConsumedWitnessFenceFailsClosedWithDCDown3DC runs the sweep
// over the CONSUMED witness the stale-lease leg left behind while dc-asia is
// stopped. The re-fence tombstone is EACH_QUORUM: it must fail, refenced_at
// must not advance (the witness records a fence only once every DC has
// acknowledged it), and at retention expiry the mandatory final fence must
// fail the same way and the witness must NOT be deleted.
func TestW2PostHeadConsumedWitnessFenceFailsClosedWithDCDown3DC(t *testing.T) {
	if os.Getenv("W2_POST_HEAD_CONSUMED_FENCE_UNAVAILABLE") != "1" {
		t.Skip("W2_POST_HEAD_CONSUMED_FENCE_UNAVAILABLE is not set")
	}
	endpoints := w2PostHead3DCEndpoints(t)
	sweeper := w2PostHead3DCConnect(t, "dc-na", endpoints)
	orgID, repoID, _ := w2PostHead3DCIDs(t)
	commitID := strings.TrimSpace(os.Getenv("W2_POST_HEAD_COMMIT"))
	fsID, _, token := w2PostHeadConsumedIDs(t)
	before, err := v2api.PublishedBlockReferenceRepairLivenessCleanupStateForIntegration(sweeper, orgID, repoID, commitID, fsID, token)
	if err != nil {
		t.Fatalf("read consumed witness: %v", err)
	}
	if !before.Present || !before.Consumed {
		t.Fatalf("witness = %+v, want the CONSUMED witness the stale-lease leg retired", before)
	}
	due := before.RefencedAt.Add(v2api.PublishedBlockReferenceRepairLivenessRefenceIntervalForIntegration())
	err = v2api.SweepPublishedBlockReferenceRepairLivenessCleanupsGatedAtForIntegration(sweeper, orgID, repoID, commitID, fsID, due, nil)
	if err == nil {
		t.Fatalf("re-fence sweep with dc-asia down = nil, want the EACH_QUORUM fence failure surfaced (fs_object %s)", fsID)
	}
	after, err := v2api.PublishedBlockReferenceRepairLivenessCleanupStateForIntegration(sweeper, orgID, repoID, commitID, fsID, token)
	if err != nil {
		t.Fatalf("re-read consumed witness: %v", err)
	}
	if !after.Present || !after.Consumed || !after.RefencedAt.Equal(before.RefencedAt) {
		t.Fatalf("witness after a failed global re-fence = %+v, want refenced_at unchanged (%s): a fence one DC did not acknowledge must not be recorded", after, before.RefencedAt)
	}
	expired := before.ConsumedAt.Add(v2api.PublishedBlockReferenceRepairLivenessConsumedRetentionForIntegration())
	err = v2api.SweepPublishedBlockReferenceRepairLivenessCleanupsGatedAtForIntegration(sweeper, orgID, repoID, commitID, fsID, expired, nil)
	if err == nil {
		t.Fatal("retention sweep with dc-asia down = nil, want the final fence failure surfaced")
	}
	final, err := v2api.PublishedBlockReferenceRepairLivenessCleanupStateForIntegration(sweeper, orgID, repoID, commitID, fsID, token)
	if err != nil {
		t.Fatalf("re-read consumed witness after retention: %v", err)
	}
	if !final.Present || !final.Consumed || !final.RefencedAt.Equal(before.RefencedAt) {
		t.Fatalf("witness after a failed FINAL fence at retention = %+v, want retained untouched: the last cleanup root must not disappear without a globally acknowledged fence", final)
	}
	t.Log("W2 3DC consumed witness: with dc-asia down neither the periodic re-fence nor the final fence at retention was recorded; refenced_at unchanged and the witness retained")
}

// TestW2PostHeadConsumedWitnessFenceAdvancesWhenEveryDCIsUp3DC is the same
// witness after dc-asia returns: a requeue gets a different physical pub
// referrer, so the periodic and final fences of the old producer proceed
// independently while the new producer's pin remains present.
func TestW2PostHeadConsumedWitnessFenceAdvancesWhenEveryDCIsUp3DC(t *testing.T) {
	if os.Getenv("W2_POST_HEAD_CONSUMED_FENCE_ADVANCES") != "1" {
		t.Skip("W2_POST_HEAD_CONSUMED_FENCE_ADVANCES is not set")
	}
	endpoints := w2PostHead3DCEndpoints(t)
	sweeper := w2PostHead3DCConnect(t, "dc-na", endpoints)
	producer := w2PostHead3DCConnect(t, "dc-eu", endpoints)
	orgID, repoID, _ := w2PostHead3DCIDs(t)
	commitID := strings.TrimSpace(os.Getenv("W2_POST_HEAD_COMMIT"))
	fsID, blockID, token := w2PostHeadConsumedIDs(t)
	before, err := v2api.PublishedBlockReferenceRepairLivenessCleanupStateForIntegration(sweeper, orgID, repoID, commitID, fsID, token)
	if err != nil {
		t.Fatalf("read consumed witness: %v", err)
	}
	if !before.Present || !before.Consumed {
		t.Fatalf("witness = %+v, want the CONSUMED witness retained through the outage", before)
	}
	due := before.RefencedAt.Add(v2api.PublishedBlockReferenceRepairLivenessRefenceIntervalForIntegration())
	// Pending requeue of the same identity, written in dc-eu: its producer
	// token must own a different physical referrer. Keep that producer in
	// PREPARING with a live lease so the sweep must leave its pin untouched.
	if err := v2api.QueuePublishedFSObjectBlockReferenceRepair(producer, orgID, repoID, commitID, fsID, []string{blockID}); err != nil {
		t.Fatalf("requeue the identity from dc-eu: %v", err)
	}
	pendingToken := uuid.NewString()
	pendingLease := due.Add(time.Hour)
	w2PostHeadRetryEachQuorum(t, "seed requeued producer witness", func() error {
		if err := v2api.RecordPreparingPublishedBlockReferenceRepairLivenessCleanupForIntegration(producer, orgID, repoID, commitID, fsID, []string{blockID}, pendingToken, pendingLease); err != nil {
			return err
		}
		return v2api.WritePublishedBlockReferenceRepairLivenessPinForIntegration(producer, orgID, repoID, commitID, fsID, blockID, pendingToken, pendingLease)
	})
	if !w2PostHeadCleanupPinPresent(t, sweeper, gocql.EachQuorum, orgID, repoID, commitID, fsID, blockID, pendingToken) {
		t.Fatal("requeued producer pin is not globally visible before the old witness re-fence")
	}
	w2PostHeadRetryEachQuorum(t, "re-fence sweep with the identity pending again", func() error {
		return v2api.SweepPublishedBlockReferenceRepairLivenessCleanupsGatedAtForIntegration(sweeper, orgID, repoID, commitID, fsID, due, nil)
	})
	if s, err := v2api.PublishedBlockReferenceRepairLivenessCleanupStateForIntegration(sweeper, orgID, repoID, commitID, fsID, token); err != nil || !s.Present || !s.Consumed || !s.RefencedAt.Equal(due) {
		t.Fatalf("old witness after a re-fence sweep with the identity pending again = %+v (err=%v), want refenced_at=%s", s, err, due)
	}
	if !w2PostHeadCleanupPinPresent(t, sweeper, gocql.EachQuorum, orgID, repoID, commitID, fsID, blockID, pendingToken) {
		t.Fatal("old producer re-fence removed the requeued producer pin")
	}
	// A late write of the fenced producer from dc-eu under its lease is still
	// shadowed everywhere.
	w2PostHeadRetryEachQuorum(t, "late dc-eu pin under the producer lease", func() error {
		return v2api.WritePublishedBlockReferenceRepairLivenessPinForIntegration(producer, orgID, repoID, commitID, fsID, blockID, token, before.Lease)
	})
	if w2PostHeadCleanupPinPresent(t, sweeper, gocql.EachQuorum, orgID, repoID, commitID, fsID, blockID, token) {
		t.Fatal("a late dc-eu write under the producer lease survived the globally acknowledged re-fence")
	}
	expired := before.ConsumedAt.Add(v2api.PublishedBlockReferenceRepairLivenessConsumedRetentionForIntegration())
	w2PostHeadRetryEachQuorum(t, "final fence at retention", func() error {
		return v2api.SweepPublishedBlockReferenceRepairLivenessCleanupsGatedAtForIntegration(sweeper, orgID, repoID, commitID, fsID, expired, nil)
	})
	final, err := v2api.PublishedBlockReferenceRepairLivenessCleanupStateForIntegration(sweeper, orgID, repoID, commitID, fsID, token)
	if err != nil {
		t.Fatalf("re-read witness after retention: %v", err)
	}
	if final.Present {
		t.Fatalf("witness after the final fence at retention = %+v, want deleted", final)
	}
	if w2PostHeadCleanupPinPresent(t, sweeper, gocql.EachQuorum, orgID, repoID, commitID, fsID, blockID, token) {
		t.Fatal("old producer pin present after the final fence and witness delete")
	}
	if !w2PostHeadCleanupPinPresent(t, sweeper, gocql.EachQuorum, orgID, repoID, commitID, fsID, blockID, pendingToken) {
		t.Fatal("requeued producer pin did not survive the old producer's final fence")
	}
	// Clear the requeue and finish the synthetic producer witness so this
	// directed evidence leaves no durable state for a later W2 leg.
	bucket := publishRepairIntegrationBucket(orgID, repoID, commitID, fsID)
	w2PostHeadRetryEachQuorum(t, "clear the requeue in every DC", func() error {
		return sweeper.Session().Query(`
			DELETE FROM published_block_reference_repairs
			WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
		`, bucket, orgID, repoID, commitID, fsID).Consistency(gocql.EachQuorum).Exec()
	})
	w2PostHeadRetryEachQuorum(t, "consume the requeued producer witness", func() error {
		return v2api.SweepPublishedBlockReferenceRepairLivenessCleanupsGatedAtForIntegration(sweeper, orgID, repoID, commitID, fsID, pendingLease.Add(time.Second), nil)
	})
	t.Log("W2 3DC consumed witness: the old producer re-fenced and was deleted while a pending requeue kept its token-specific pin; late writes of the old token stayed fenced")
}
