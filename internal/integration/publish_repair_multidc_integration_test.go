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

// --- ISSUE-PUBLISH-REPAIR-RENEWAL-AFTER-CLASSIFY-01: cleanup authority across DCs ---
//
// The renewal compensation removes the durable repair pin
// pub:<repo:commit:fsID> only when the repair row is conclusively gone:
// the local read may only retain, a local absence is escalated to an
// EACH_QUORUM read, and an unavailable DC fails closed. These legs prove it
// on the real 3-DC fixture with the production decider: the pin is written
// in every DC, the repair row only in dc-eu (hinted handoff disabled while
// dc-na and dc-asia are stopped), and the compensation is run from dc-na,
// which is blind to the row at LOCAL_QUORUM.

// w2PostHeadPresent runs one presence read through w2PostHeadRetryEachQuorum:
// found is the read's result, ErrNotFound is absence (not an error), and a
// transient cross-DC failure (a DC that just restarted can time out its
// first EACH_QUORUM reads for a few seconds) is retried instead of being
// reported as either. Legs that assert an outage read at LOCAL_QUORUM.
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

func w2PostHeadCleanupIDs(t *testing.T) (fsID, blockID string) {
	t.Helper()
	fsID = strings.TrimSpace(os.Getenv("W2_POST_HEAD_CLEANUP_FSID"))
	blockID = strings.TrimSpace(os.Getenv("W2_POST_HEAD_CLEANUP_BLOCK"))
	if fsID == "" || blockID == "" {
		t.Fatal("W2_POST_HEAD_CLEANUP_FSID and W2_POST_HEAD_CLEANUP_BLOCK are required")
	}
	return fsID, blockID
}

func w2PostHeadRepairPinPresent(t *testing.T, database *dbpkg.DB, consistency gocql.Consistency, orgID, repoID, commitID, fsID, blockID string) bool {
	t.Helper()
	referrer := v2api.PublishedBlockReferenceRepairLivenessReferrerForIntegration(repoID, commitID, fsID)
	return w2PostHeadPresent(t, fmt.Sprintf("read repair-owned pin at %s", consistency), func() error {
		var got string
		return database.Session().Query(`
			SELECT referrer FROM block_references WHERE org_id = ? AND block_id = ? AND referrer = ?
		`, orgID, blockID, referrer).Consistency(consistency).Scan(&got)
	})
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

// TestW2PostHeadSeedRepairPinFor3DC writes the durable repair-owned pin of a
// fresh identity in every DC (EACH_QUORUM), with no repair row yet.
func TestW2PostHeadSeedRepairPinFor3DC(t *testing.T) {
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
	referrer := v2api.PublishedBlockReferenceRepairLivenessReferrerForIntegration(repoID, commitID, fsID)
	w2PostHeadRetryEachQuorum(t, "seed repair-owned pin", func() error {
		return database.Session().Query(`
			INSERT INTO block_references (org_id, block_id, referrer, library_id, created_at)
			VALUES (?, ?, ?, ?, ?) USING TTL ?
		`, orgID, blockID, referrer, repoID, time.Now().UTC(), dbpkg.PublishAttemptReferenceTTLSeconds).Consistency(gocql.EachQuorum).Exec()
	})
	if !w2PostHeadRepairPinPresent(t, database, gocql.EachQuorum, orgID, repoID, commitID, fsID, blockID) {
		t.Fatal("seeded repair-owned pin not globally visible")
	}
	if w2PostHeadRepairRowPresent(t, database, gocql.EachQuorum, orgID, repoID, commitID, fsID) {
		t.Fatal("repair row unexpectedly present for a fresh identity")
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

// TestW2PostHeadRepairCleanupAuthorityFailsClosed3DC runs the production
// compensation from blind dc-na with dc-asia stopped: the local read is
// NotFound, the escalated EACH_QUORUM read cannot be served, and the
// compensation must return that failure and keep the pin.
func TestW2PostHeadRepairCleanupAuthorityFailsClosed3DC(t *testing.T) {
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
	if !w2PostHeadRepairPinPresent(t, database, gocql.LocalQuorum, orgID, repoID, commitID, fsID, blockID) {
		t.Fatal("dc-na does not see the repair-owned pin; the fixture is not the intended interleaving")
	}

	gone, err := v2api.CompensatePublishedBlockReferenceRepairLivenessIfGoneForIntegration(database, orgID, repoID, commitID, fsID, []string{blockID})
	if err == nil {
		t.Fatalf("compensation with one DC unavailable = gone:%v err:nil, want the EACH_QUORUM authority failure; absence must not be concluded from incomplete evidence", gone)
	}
	if !strings.Contains(err.Error(), fsID) || !strings.Contains(err.Error(), "EACH_QUORUM") {
		t.Fatalf("compensation error = %v, want the EACH_QUORUM authority failure for %s", err, fsID)
	}
	if !w2PostHeadRepairPinPresent(t, database, gocql.LocalQuorum, orgID, repoID, commitID, fsID, blockID) {
		t.Fatal("compensation removed the repair-owned pin while a DC was unavailable")
	}
	t.Log("W2 3DC cleanup authority: with dc-asia down the compensation from blind dc-na failed closed and kept the pin")
}

// TestW2PostHeadRepairCleanupAuthorityIsGlobal3DC runs the production
// compensation from dc-na, which sees the pin but not the repair row at
// LOCAL_QUORUM. The EACH_QUORUM authority read must find the row in dc-eu
// and keep the pin. A positive control then clears the row in every DC and
// requires the same compensation, from the same DC, to remove the pin.
func TestW2PostHeadRepairCleanupAuthorityIsGlobal3DC(t *testing.T) {
	if os.Getenv("W2_POST_HEAD_CLEANUP_VERIFY_BLIND") != "1" {
		t.Skip("W2_POST_HEAD_CLEANUP_VERIFY_BLIND is not set")
	}
	endpoints := w2PostHead3DCEndpoints(t)
	database := w2PostHead3DCConnect(t, "dc-na", endpoints)
	orgID, repoID, _ := w2PostHead3DCIDs(t)
	commitID := strings.TrimSpace(os.Getenv("W2_POST_HEAD_COMMIT"))
	fsID, blockID := w2PostHeadCleanupIDs(t)

	if !w2PostHeadRepairPinPresent(t, database, gocql.LocalQuorum, orgID, repoID, commitID, fsID, blockID) {
		t.Fatal("dc-na does not see the repair-owned pin; the fixture is not the intended interleaving")
	}
	if w2PostHeadRepairRowPresent(t, database, gocql.LocalQuorum, orgID, repoID, commitID, fsID) {
		t.Fatal("dc-na already sees the repair row at LOCAL_QUORUM; local blindness was not established (hinted handoff still enabled?)")
	}

	var gone bool
	w2PostHeadRetryEachQuorum(t, "blind-DC compensation", func() error {
		var err error
		gone, err = v2api.CompensatePublishedBlockReferenceRepairLivenessIfGoneForIntegration(database, orgID, repoID, commitID, fsID, []string{blockID})
		return err
	})
	if gone {
		t.Fatal("blind dc-na compensation concluded Gone for a repair row still pending in dc-eu: local absence authorized destructive cleanup")
	}
	if !w2PostHeadRepairPinPresent(t, database, gocql.EachQuorum, orgID, repoID, commitID, fsID, blockID) {
		t.Fatal("blind dc-na compensation removed the repair-owned pin of a repair row still pending in dc-eu")
	}
	if !w2PostHeadRepairRowPresent(t, database, gocql.EachQuorum, orgID, repoID, commitID, fsID) {
		t.Fatal("repair row is not globally visible; the authority read had nothing to protect")
	}

	// Positive control: once the row is conclusively gone everywhere, the
	// same decider from the same DC removes exactly this identity.
	bucket := publishRepairIntegrationBucket(orgID, repoID, commitID, fsID)
	w2PostHeadRetryEachQuorum(t, "clear the repair row in every DC", func() error {
		return database.Session().Query(`
			DELETE FROM published_block_reference_repairs
			WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
		`, bucket, orgID, repoID, commitID, fsID).Consistency(gocql.EachQuorum).Exec()
	})
	w2PostHeadRetryEachQuorum(t, "compensation after the global clear", func() error {
		var err error
		gone, err = v2api.CompensatePublishedBlockReferenceRepairLivenessIfGoneForIntegration(database, orgID, repoID, commitID, fsID, []string{blockID})
		return err
	})
	if !gone {
		t.Fatal("compensation did not conclude Gone after the row was cleared in every DC")
	}
	if w2PostHeadRepairPinPresent(t, database, gocql.EachQuorum, orgID, repoID, commitID, fsID, blockID) {
		t.Fatal("compensation left the repair-owned pin after a conclusive EACH_QUORUM absence")
	}
	t.Log("W2 3DC cleanup authority: blind dc-na kept the pin of a repair row visible only through EACH_QUORUM, and removed it only after the row was gone in every DC")
}
