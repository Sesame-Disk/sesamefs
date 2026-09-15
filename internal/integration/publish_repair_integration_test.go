//go:build integration

package integration

import (
	"errors"
	"fmt"
	"hash/fnv"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	v2api "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

type publishRepairIntegrationFileState struct {
	orgID            string
	headCommitID     string
	fsID             string
	internalBlockIDs []string
}

const w2PostHeadEvidenceEnv = "SESAMEFS_REQUIRE_W2_POST_HEAD_EVIDENCE"

type w2PostHeadEvidenceState struct {
	normalSuccess                bool
	crashAfterAppliedHead        bool
	ambiguousCASApplied          bool
	ambiguousConfirmationUnknown bool
	leaseExpiryIsNotAuthority    bool
	casLoserCleanup              bool
	preHeadRepairRace            bool
	reachableAncestor            bool
	restartReplay                bool
	reachabilityConvergence      bool
	progressResidueReap          bool
	renewalBeforeClassify        bool
}

var w2PostHeadEvidence w2PostHeadEvidenceState

func (e w2PostHeadEvidenceState) complete() bool {
	return e.normalSuccess &&
		e.crashAfterAppliedHead &&
		e.ambiguousCASApplied &&
		e.ambiguousConfirmationUnknown &&
		e.leaseExpiryIsNotAuthority &&
		e.casLoserCleanup &&
		e.preHeadRepairRace &&
		e.reachableAncestor &&
		e.restartReplay &&
		e.reachabilityConvergence &&
		e.progressResidueReap &&
		e.renewalBeforeClassify
}

func (e w2PostHeadEvidenceState) missing() []string {
	missing := make([]string, 0, 12)
	if !e.normalSuccess {
		missing = append(missing, "normal_success")
	}
	if !e.crashAfterAppliedHead {
		missing = append(missing, "crash_after_applied_head")
	}
	if !e.ambiguousCASApplied {
		missing = append(missing, "ambiguous_cas_applied")
	}
	if !e.ambiguousConfirmationUnknown {
		missing = append(missing, "ambiguous_confirmation_unavailable_retains")
	}
	if !e.leaseExpiryIsNotAuthority {
		missing = append(missing, "lease_expiry_is_not_authority")
	}
	if !e.casLoserCleanup {
		missing = append(missing, "cas_loser_cleanup")
	}
	if !e.preHeadRepairRace {
		missing = append(missing, "pre_head_repair_race")
	}
	if !e.reachableAncestor {
		missing = append(missing, "reachable_ancestor")
	}
	if !e.restartReplay {
		missing = append(missing, "restart_replay")
	}
	if !e.reachabilityConvergence {
		missing = append(missing, "reachability_convergence")
	}
	if !e.progressResidueReap {
		missing = append(missing, "progress_residue_reap")
	}
	if !e.renewalBeforeClassify {
		missing = append(missing, "renewal_before_classify")
	}
	return missing
}

func markW2PostHeadEvidence(t *testing.T, leg string) {
	t.Helper()
	if os.Getenv(w2PostHeadEvidenceEnv) != "1" {
		return
	}
	switch leg {
	case "normal_success":
		w2PostHeadEvidence.normalSuccess = true
	case "crash_after_applied_head":
		w2PostHeadEvidence.crashAfterAppliedHead = true
	case "ambiguous_cas_applied":
		w2PostHeadEvidence.ambiguousCASApplied = true
	case "ambiguous_confirmation_unavailable_retains":
		w2PostHeadEvidence.ambiguousConfirmationUnknown = true
	case "lease_expiry_is_not_authority":
		w2PostHeadEvidence.leaseExpiryIsNotAuthority = true
	case "cas_loser_cleanup":
		w2PostHeadEvidence.casLoserCleanup = true
	case "pre_head_repair_race":
		w2PostHeadEvidence.preHeadRepairRace = true
	case "reachable_ancestor":
		w2PostHeadEvidence.reachableAncestor = true
	case "restart_replay":
		w2PostHeadEvidence.restartReplay = true
	case "reachability_convergence":
		w2PostHeadEvidence.reachabilityConvergence = true
	case "progress_residue_reap":
		w2PostHeadEvidence.progressResidueReap = true
	case "renewal_before_classify":
		w2PostHeadEvidence.renewalBeforeClassify = true
	default:
		t.Fatalf("unknown W2 evidence leg %q", leg)
	}
}

func TestPublishedBlockReferenceRepairWorker_ReplaysReachableQueuedRepairAfterRestart(t *testing.T) {
	requireCassandra(t)

	database := shareProjectionDBForTest(t)
	repoID := createTestLibrary(t, adminClient, fmt.Sprintf("inttest-publish-repair-replay-%d", time.Now().UnixNano()))
	fileName := "repair-replay.txt"
	fileContent := fmt.Sprintf("repair replay content %d\n", time.Now().UnixNano())

	uploadURL := getUploadLink(t, adminClient, repoID, "/")
	uploadFileThroughLink(t, adminClient, uploadURL, fileName, "/", fileContent)

	state := publishRepairIntegrationReadFileState(t, repoID, "/", fileName)
	if len(state.internalBlockIDs) != 1 {
		t.Fatalf("internalBlockIDs = %v, want exactly one block for focused repair replay test", state.internalBlockIDs)
	}

	fsReferrer := dbpkg.BlockReferrerForFSObject(repoID, state.fsID)
	pubReferrer := dbpkg.BlockReferrerForPublishAttempt(state.headCommitID)
	for _, blockID := range state.internalBlockIDs {
		if err := database.RemoveBlockReference(state.orgID, blockID, fsReferrer); err != nil {
			t.Fatalf("failed to remove fs ref %q for block %s: %v", fsReferrer, blockID, err)
		}
		if err := database.AddBlockReference(state.orgID, blockID, pubReferrer, repoID, 0); err != nil {
			t.Fatalf("failed to add pub ref %q for block %s: %v", pubReferrer, blockID, err)
		}
	}
	if err := v2api.QueuePublishedFSObjectBlockReferenceRepair(database, state.orgID, repoID, state.headCommitID, state.fsID, state.internalBlockIDs); err != nil {
		t.Fatalf("failed to queue durable publish repair: %v", err)
	}

	bucket := publishRepairIntegrationBucket(state.orgID, repoID, state.headCommitID, state.fsID)
	staleCreatedAt := time.Now().UTC().Add(-time.Minute)
	// The worker now treats lease_expires_at as the advisory next-retry time;
	// make this seeded stale row immediately eligible for the restart replay.
	leaseExpiresAt := time.Now().UTC().Add(-time.Minute)
	if err := database.Session().Query(`
		UPDATE published_block_reference_repairs
		SET created_at = ?, lease_expires_at = ?
		WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
	`, staleCreatedAt, leaseExpiresAt, bucket, state.orgID, repoID, state.headCommitID, state.fsID).Exec(); err != nil {
		t.Fatalf("failed to backdate queued publish repair row: %v", err)
	}

	t.Cleanup(func() {
		if err := v2api.ClearPublishedFSObjectBlockReferenceRepair(database, state.orgID, repoID, state.headCommitID, state.fsID); err != nil {
			t.Errorf("cleanup W2 repair row: %v", err)
		}
		for _, blockID := range state.internalBlockIDs {
			_ = database.RemoveBlockReference(state.orgID, blockID, pubReferrer)
			_ = database.AddBlockReference(state.orgID, blockID, fsReferrer, repoID, 0)
		}
	})

	publishRepairIntegrationAssertReferrers(t, repoID, "/", fileName, func(referrers []string) {
		if !publishRepairIntegrationHasReferrer(referrers, pubReferrer) {
			t.Fatalf("expected seeded pub ref %q before replay, got %v", pubReferrer, referrers)
		}
		if publishRepairIntegrationHasReferrer(referrers, fsReferrer) {
			t.Fatalf("expected fs ref %q to be removed before replay, got %v", fsReferrer, referrers)
		}
	})
	if !publishRepairIntegrationRepairRowExists(t, bucket, state.orgID, repoID, state.headCommitID, state.fsID) {
		t.Fatal("queued publish repair row missing before worker start")
	}

	v2api.StartPublishedBlockReferenceRepairer(database)

	if !pollUntil(t, 10*time.Second, 100*time.Millisecond, func() bool {
		referrers := uploadedFileBlockReferrers(t, repoID, "/", fileName)
		return publishRepairIntegrationHasReferrer(referrers, fsReferrer) &&
			!publishRepairIntegrationHasReferrer(referrers, pubReferrer) &&
			!publishRepairIntegrationRepairRowExists(t, bucket, state.orgID, repoID, state.headCommitID, state.fsID)
	}) {
		referrers := uploadedFileBlockReferrers(t, repoID, "/", fileName)
		t.Fatalf("timed out waiting for durable publish replay; referrers=%v rowExists=%v", referrers, publishRepairIntegrationRepairRowExists(t, bucket, state.orgID, repoID, state.headCommitID, state.fsID))
	}
	markW2PostHeadEvidence(t, "crash_after_applied_head")
	markW2PostHeadEvidence(t, "restart_replay")
}

func TestW2PublishedRepairReachabilityConvergesUnderMovingHEAD(t *testing.T) {
	if os.Getenv(w2PostHeadEvidenceEnv) != "1" {
		t.Skipf("%s is not enabled", w2PostHeadEvidenceEnv)
	}
	requireCassandra(t)

	database := shareProjectionDBForTest(t)
	repoID := createTestLibrary(t, adminClient, fmt.Sprintf("inttest-r31-reachability-%d", time.Now().UnixNano()))
	fileName := "r31-convergence.txt"
	uploadURL := getUploadLink(t, adminClient, repoID, "/")
	uploadFileThroughLink(t, adminClient, uploadURL, fileName, "/", fmt.Sprintf("r31 convergence %d\n", time.Now().UnixNano()))
	state := publishRepairIntegrationReadFileState(t, repoID, "/", fileName)
	targetCommitID := state.headCommitID
	if strings.TrimSpace(targetCommitID) == "" {
		t.Fatal("library HEAD is empty before synthetic ancestry insert")
	}

	session := database.Session()
	parentID := targetCommitID
	nonce := time.Now().UnixNano()
	creatorID := "00000000-0000-0000-0000-0000000000c1"
	now := time.Now().UTC()
	var tipCommitID string
	for i := 1; i <= v2api.PublishedCommitReachabilityMaxNodesForIntegration(); i++ {
		tipCommitID = fmt.Sprintf("r31-%d-%d", nonce, i)
		if err := session.Query(`
			INSERT INTO commits (library_id, commit_id, parent_id, root_fs_id, creator_id, description, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, repoID, tipCommitID, parentID, "r31-root", creatorID, "r31 convergence ancestor", now).Exec(); err != nil {
			t.Fatalf("insert synthetic commit %s: %v", tipCommitID, err)
		}
		parentID = tipCommitID
	}
	if err := session.Query(`
		UPDATE libraries SET head_commit_id = ? WHERE org_id = ? AND library_id = ?
	`, tipCommitID, state.orgID, repoID).Exec(); err != nil {
		t.Fatalf("advance HEAD to depth %d: %v", v2api.PublishedCommitReachabilityMaxNodesForIntegration(), err)
	}

	fsReferrer := dbpkg.BlockReferrerForFSObject(repoID, state.fsID)
	pubReferrer := dbpkg.BlockReferrerForPublishAttempt(targetCommitID)
	repairPubReferrer := v2api.PublishedBlockReferenceRepairLivenessReferrerForIntegration(repoID, targetCommitID, state.fsID)
	for _, blockID := range state.internalBlockIDs {
		if err := database.RemoveBlockReference(state.orgID, blockID, fsReferrer); err != nil {
			t.Fatalf("remove fs ref: %v", err)
		}
		if err := database.AddBlockReference(state.orgID, blockID, pubReferrer, repoID, 90); err != nil {
			t.Fatalf("seed short-lived pub ref: %v", err)
		}
	}
	if err := v2api.QueuePublishedFSObjectBlockReferenceRepair(database, state.orgID, repoID, targetCommitID, state.fsID, state.internalBlockIDs); err != nil {
		t.Fatalf("queue repair: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Query(`
			UPDATE libraries SET head_commit_id = ? WHERE org_id = ? AND library_id = ?
		`, targetCommitID, state.orgID, repoID).Exec()
		_ = v2api.ClearPublishedFSObjectBlockReferenceRepair(database, state.orgID, repoID, targetCommitID, state.fsID)
		for _, blockID := range state.internalBlockIDs {
			_ = database.RemoveBlockReference(state.orgID, blockID, pubReferrer)
			_ = database.RemoveBlockReference(state.orgID, blockID, repairPubReferrer)
			_ = database.AddBlockReference(state.orgID, blockID, fsReferrer, repoID, 0)
		}
	})

	err := v2api.RepairPublishedFSObjectBlockReferenceRepair(database, state.orgID, repoID, targetCommitID, state.fsID, state.internalBlockIDs)
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("first bounded pass = %v, want UNKNOWN limit", err)
	}
	anchor, cursor, err := v2api.PublishedBlockReferenceRepairProgressForIntegration(database, state.orgID, repoID, targetCommitID, state.fsID)
	if err != nil {
		t.Fatalf("load progress after first pass: %v", err)
	}
	if anchor != tipCommitID {
		t.Fatalf("anchor = %q, want first SERIAL HEAD %q", anchor, tipCommitID)
	}
	if cursor == "" || cursor == tipCommitID {
		t.Fatalf("cursor = %q, want progress away from the first HEAD", cursor)
	}

	movedHEAD := tipCommitID
	for i := 1; i <= 256; i++ {
		next := fmt.Sprintf("r31-%d-moved-%d", nonce, i)
		if err := session.Query(`
			INSERT INTO commits (library_id, commit_id, parent_id, root_fs_id, creator_id, description, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, repoID, next, movedHEAD, "r31-root", creatorID, "r31 moving head", now).Exec(); err != nil {
			t.Fatalf("insert moved HEAD commit %s: %v", next, err)
		}
		movedHEAD = next
	}
	if err := session.Query(`
		UPDATE libraries SET head_commit_id = ? WHERE org_id = ? AND library_id = ?
	`, movedHEAD, state.orgID, repoID).Exec(); err != nil {
		t.Fatalf("move live HEAD: %v", err)
	}

	var pubTTL int
	if err := session.Query(`
		SELECT TTL(created_at) FROM block_references WHERE org_id = ? AND block_id = ? AND referrer = ?
	`, state.orgID, state.internalBlockIDs[0], repairPubReferrer).Scan(&pubTTL); err != nil {
		t.Fatalf("read renewed repair-owned pub TTL: %v", err)
	}
	if pubTTL < 30*24*60*60 {
		t.Fatalf("unresolved repair pub TTL = %d, want renewal toward 35d on the per-repair identity, not the 90s commit-scoped seed", pubTTL)
	}

	err = v2api.RepairPublishedFSObjectBlockReferenceRepair(database, state.orgID, repoID, targetCommitID, state.fsID, state.internalBlockIDs)
	if err != nil {
		t.Fatalf("second pass under moved HEAD = %v, want REACHABLE", err)
	}
	_, cursorAfter, progressErr := v2api.PublishedBlockReferenceRepairProgressForIntegration(database, state.orgID, repoID, targetCommitID, state.fsID)
	if progressErr == nil {
		t.Fatalf("repair row remained after REACHABLE settlement; cursor=%q", cursorAfter)
	}
	if !errors.Is(progressErr, gocql.ErrNotFound) {
		t.Fatalf("progress after settlement: %v", progressErr)
	}
	// The live HEAD was deliberately moved onto synthetic commits, so directory
	// listing cannot witness settlement. The publication contract is the block
	// referrers and the repair row.
	for _, blockID := range state.internalBlockIDs {
		referrers := publishRepairIntegrationBlockReferrers(t, database, state.orgID, blockID)
		if !publishRepairIntegrationHasReferrer(referrers, fsReferrer) {
			t.Fatalf("REACHABLE settlement did not restore fs: for %s: %v", blockID, referrers)
		}
		if publishRepairIntegrationHasReferrer(referrers, pubReferrer) {
			t.Fatalf("REACHABLE settlement left commit-scoped pub: for %s: %v", blockID, referrers)
		}
		if publishRepairIntegrationHasReferrer(referrers, repairPubReferrer) {
			t.Fatalf("REACHABLE settlement left repair-owned pub: for %s: %v", blockID, referrers)
		}
	}
	markW2PostHeadEvidence(t, "reachability_convergence")
}

// TestW2PublishedRepairSweepReapsProgressOnlyResidue proves the residue of a
// progress LWT that outlived the ordinary settlement DELETE (only the primary
// key and reachability cells remain) is deleted by the sweep under a SERIAL
// condition, while a queued row with ordinary cells is never touched by that
// same conditional delete. Real Cassandra is required: the CQL `IF col = null`
// predicate and the row-without-row-marker shape are storage semantics, not
// fake-store behaviour.
func TestW2PublishedRepairSweepReapsProgressOnlyResidue(t *testing.T) {
	if os.Getenv(w2PostHeadEvidenceEnv) != "1" {
		t.Skipf("%s is not enabled", w2PostHeadEvidenceEnv)
	}
	requireCassandra(t)

	database := shareProjectionDBForTest(t)
	session := database.Session()
	orgID := uuid.NewString()
	repoID := uuid.NewString()
	nonce := time.Now().UnixNano()
	residueCommit := fmt.Sprintf("r31-residue-%d", nonce)
	residueFS := fmt.Sprintf("fs-residue-%d", nonce)
	residueBucket := v2api.PublishedBlockReferenceRepairBucketForIntegration(orgID, repoID, residueCommit, residueFS)
	// An UPDATE without a prior INSERT materializes exactly the residue shape:
	// reachability cells exist, created_at/lease_expires_at/staged_block_ids
	// are null, and there is no row marker.
	if err := session.Query(`
		UPDATE published_block_reference_repairs
		SET reachability_anchor_head_commit_id = ?, reachability_cursor_commit_id = ?, reachability_anchor_exhausted = false
		WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
	`, "h-stale", "c-stale", residueBucket, orgID, repoID, residueCommit, residueFS).Exec(); err != nil {
		t.Fatalf("seed progress-only residue: %v", err)
	}
	legitCommit := fmt.Sprintf("r31-legit-%d", nonce)
	legitFS := fmt.Sprintf("fs-legit-%d", nonce)
	legitBlocks := []string{fmt.Sprintf("block-legit-%d", nonce)}
	if err := v2api.QueuePublishedFSObjectBlockReferenceRepair(database, orgID, repoID, legitCommit, legitFS, legitBlocks); err != nil {
		t.Fatalf("queue legit repair: %v", err)
	}
	t.Cleanup(func() {
		_ = v2api.ClearPublishedFSObjectBlockReferenceRepair(database, orgID, repoID, legitCommit, legitFS)
		_ = session.Query(`
			DELETE FROM published_block_reference_repairs
			WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
		`, residueBucket, orgID, repoID, residueCommit, residueFS).Exec()
	})

	rowExists := func(commitID, fsID string) bool {
		t.Helper()
		var anchor string
		err := session.Query(`
			SELECT reachability_anchor_head_commit_id FROM published_block_reference_repairs
			WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
		`, v2api.PublishedBlockReferenceRepairBucketForIntegration(orgID, repoID, commitID, fsID), orgID, repoID, commitID, fsID).Consistency(gocql.Serial).Scan(&anchor)
		if errors.Is(err, gocql.ErrNotFound) {
			return false
		}
		if err != nil {
			t.Fatalf("read repair row %s/%s: %v", commitID, fsID, err)
		}
		return true
	}
	if !rowExists(residueCommit, residueFS) {
		t.Fatal("progress-only residue row is not visible before the sweep")
	}

	// The conditional delete must refuse a queued row outright.
	applied, err := v2api.ReapPublishedBlockReferenceRepairProgressOnlyRowForIntegration(database, orgID, repoID, legitCommit, legitFS)
	if err != nil {
		t.Fatalf("conditional reap against queued row: %v", err)
	}
	if applied {
		t.Fatal("conditional reap applied against a queued row with ordinary cells")
	}
	if !rowExists(legitCommit, legitFS) {
		t.Fatal("queued repair row disappeared after a refused conditional reap")
	}

	// One production sweep: residue is reaped, the queued row is untouched.
	// The queued row is younger than the stale cutoff, so the sweep does not
	// classify it; any returned error must not concern the residue.
	if err := v2api.RunPublishedBlockReferenceRepairSweepForIntegration(database); err != nil && strings.Contains(err.Error(), residueFS) {
		t.Fatalf("sweep failed on the residue row: %v", err)
	}
	if rowExists(residueCommit, residueFS) {
		t.Fatal("sweep left the progress-only residue row in place")
	}
	if !rowExists(legitCommit, legitFS) {
		t.Fatal("sweep removed a queued repair row")
	}
	// Idempotent: a second sweep has nothing to reap and still leaves the
	// queued row alone.
	if err := v2api.RunPublishedBlockReferenceRepairSweepForIntegration(database); err != nil && strings.Contains(err.Error(), residueFS) {
		t.Fatalf("second sweep failed on the residue identity: %v", err)
	}
	if !rowExists(legitCommit, legitFS) {
		t.Fatal("second sweep removed a queued repair row")
	}

	// Race model. The requeue INSERT is an ordinary write outside Paxos, so
	// the reaper can evaluate `created_at = null` against a quorum that has
	// not yet seen an acknowledged requeue whose timestamp is older than the
	// reaper's ballot. Reconciliation then decides by timestamp alone, which
	// is reproduced deterministically here: reap first, then land the
	// requeue with a timestamp one minute older than the reaper's tombstones.
	// Cell tombstones on reachability columns cannot shadow the queue cells,
	// so the requeued repair must survive and must not be reaped again.
	requeueTimestampMicros := time.Now().Add(-time.Minute).UnixMicro()
	queuedRowIsLive := func(commitID, fsID string) bool {
		t.Helper()
		var createdAt time.Time
		var staged []string
		err := session.Query(`
			SELECT created_at, staged_block_ids FROM published_block_reference_repairs
			WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
		`, v2api.PublishedBlockReferenceRepairBucketForIntegration(orgID, repoID, commitID, fsID), orgID, repoID, commitID, fsID).Consistency(gocql.Serial).Scan(&createdAt, &staged)
		if errors.Is(err, gocql.ErrNotFound) {
			return false
		}
		if err != nil {
			t.Fatalf("read requeued repair %s/%s: %v", commitID, fsID, err)
		}
		return !createdAt.IsZero() && len(staged) > 0
	}
	requeueOlderThanReaper := func(commitID, fsID string) {
		t.Helper()
		now := time.Now().UTC()
		if err := session.Query(`
			INSERT INTO published_block_reference_repairs (bucket, org_id, repo_id, commit_id, fs_id, staged_block_ids, created_at, lease_expires_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?) USING TIMESTAMP ?
		`, v2api.PublishedBlockReferenceRepairBucketForIntegration(orgID, repoID, commitID, fsID), orgID, repoID, commitID, fsID, legitBlocks, now, now.Add(5*time.Minute), requeueTimestampMicros).Exec(); err != nil {
			t.Fatalf("requeue %s/%s with an older timestamp: %v", commitID, fsID, err)
		}
	}
	t.Cleanup(func() {
		_ = session.Query(`
			DELETE FROM published_block_reference_repairs
			WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
		`, residueBucket, orgID, repoID, residueCommit, residueFS).Exec()
	})
	requeueOlderThanReaper(residueCommit, residueFS)
	if !queuedRowIsLive(residueCommit, residueFS) {
		t.Fatal("cell-only reaper shadowed an ordinary requeue whose timestamp predates the reaper's ballot")
	}
	if err := v2api.RunPublishedBlockReferenceRepairSweepForIntegration(database); err != nil && strings.Contains(err.Error(), residueFS) && !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("sweep after requeue failed on the requeued identity: %v", err)
	}
	if !queuedRowIsLive(residueCommit, residueFS) {
		t.Fatal("sweep reaped a requeued repair row that carries ordinary queue cells")
	}

	// Negative control on a third identity: the same race against a
	// whole-row conditional DELETE loses the requeue, which is exactly why
	// the production reaper must never tombstone the row. This proves the
	// timestamp model can distinguish the two shapes.
	controlCommit := fmt.Sprintf("r31-residue-control-%d", nonce)
	controlFS := fmt.Sprintf("fs-residue-control-%d", nonce)
	controlBucket := v2api.PublishedBlockReferenceRepairBucketForIntegration(orgID, repoID, controlCommit, controlFS)
	t.Cleanup(func() {
		_ = session.Query(`
			DELETE FROM published_block_reference_repairs
			WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
		`, controlBucket, orgID, repoID, controlCommit, controlFS).Exec()
	})
	if err := session.Query(`
		UPDATE published_block_reference_repairs
		SET reachability_anchor_head_commit_id = ?, reachability_cursor_commit_id = ?, reachability_anchor_exhausted = false
		WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
	`, "h-stale", "c-stale", controlBucket, orgID, repoID, controlCommit, controlFS).Exec(); err != nil {
		t.Fatalf("seed control residue: %v", err)
	}
	controlApplied, err := session.Query(`
		DELETE FROM published_block_reference_repairs
		WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
		IF created_at = null AND lease_expires_at = null
	`, controlBucket, orgID, repoID, controlCommit, controlFS).SerialConsistency(gocql.Serial).MapScanCAS(map[string]interface{}{})
	if err != nil || !controlApplied {
		t.Fatalf("control whole-row conditional delete applied=%v err=%v, want applied", controlApplied, err)
	}
	requeueOlderThanReaper(controlCommit, controlFS)
	if queuedRowIsLive(controlCommit, controlFS) {
		t.Fatal("control: a whole-row tombstone did not shadow the older requeue; the race model cannot distinguish the shapes")
	}
	markW2PostHeadEvidence(t, "progress_residue_reap")
}

func TestW2CreateFilePostHeadEvidenceAgainstRealCassandra(t *testing.T) {
	if os.Getenv(w2PostHeadEvidenceEnv) != "1" {
		t.Skipf("%s is not enabled", w2PostHeadEvidenceEnv)
	}
	requireCassandra(t)

	database := shareProjectionDBForTest(t)
	repoID := createTestLibrary(t, adminClient, fmt.Sprintf("inttest-w2-post-head-%d", time.Now().UnixNano()))
	upload := func(fileName string) publishRepairIntegrationFileState {
		uploadURL := getUploadLink(t, adminClient, repoID, "/")
		uploadFileThroughLink(t, adminClient, uploadURL, fileName, "/", fmt.Sprintf("W2 post-head evidence %s %d\n", fileName, time.Now().UnixNano()))
		return publishRepairIntegrationReadFileState(t, repoID, "/", fileName)
	}

	normal := upload("w2-normal.txt")
	normalPub := dbpkg.BlockReferrerForPublishAttempt(normal.headCommitID)
	normalRefs := uploadedFileBlockReferrers(t, repoID, "/", "w2-normal.txt")
	if !publishRepairIntegrationHasReferrer(normalRefs, dbpkg.BlockReferrerForFSObject(repoID, normal.fsID)) || publishRepairIntegrationHasReferrer(normalRefs, normalPub) {
		t.Fatalf("normal CreateFileFromBlocks publication did not converge: %v", normalRefs)
	}
	markW2PostHeadEvidence(t, "normal_success")

	crash := upload("w2-crash-after-head.txt")
	publishRepairIntegrationSeedQueuedRepair(t, database, repoID, crash, crash.headCommitID, time.Now().UTC().Add(-time.Minute), time.Now().UTC().Add(4*time.Minute), true)
	if err := v2api.RepairPublishedFSObjectBlockReferenceRepair(database, crash.orgID, repoID, crash.headCommitID, crash.fsID, crash.internalBlockIDs); err != nil {
		t.Fatalf("repair after an applied HEAD returned error: %v", err)
	}
	assertW2StateConverged(t, repoID, "w2-crash-after-head.txt", crash, crash.headCommitID)

	ambiguousApplied := upload("w2-ambiguous-applied.txt")
	publishRepairIntegrationSeedQueuedRepair(t, database, repoID, ambiguousApplied, ambiguousApplied.headCommitID, time.Now().UTC(), time.Now().UTC().Add(5*time.Minute), true)
	if err := v2api.SettlePublishedBlockReferenceRepairForIntegration(database, ambiguousApplied.orgID, repoID, ambiguousApplied.headCommitID, ambiguousApplied.fsID, ambiguousApplied.internalBlockIDs, "reachable", nil); err != nil {
		t.Fatalf("ambiguous CAS known-applied repair returned error: %v", err)
	}
	assertW2StateConverged(t, repoID, "w2-ambiguous-applied.txt", ambiguousApplied, ambiguousApplied.headCommitID)
	markW2PostHeadEvidence(t, "ambiguous_cas_applied")

	ambiguousUnknown := upload("w2-ambiguous-unknown.txt")
	publishRepairIntegrationSeedQueuedRepair(t, database, repoID, ambiguousUnknown, ambiguousUnknown.headCommitID, time.Now().UTC(), time.Now().UTC().Add(-time.Minute), true)
	err := v2api.SettlePublishedBlockReferenceRepairForIntegration(database, ambiguousUnknown.orgID, repoID, ambiguousUnknown.headCommitID, ambiguousUnknown.fsID, ambiguousUnknown.internalBlockIDs, "unknown", errors.New("confirmation unavailable"))
	if err == nil || !strings.Contains(err.Error(), "confirmation unavailable") {
		t.Fatalf("ambiguous confirmation should retain repair, error=%v", err)
	}
	unknownRefs := uploadedFileBlockReferrers(t, repoID, "/", "w2-ambiguous-unknown.txt")
	unknownPub := dbpkg.BlockReferrerForPublishAttempt(ambiguousUnknown.headCommitID)
	if publishRepairIntegrationHasReferrer(unknownRefs, dbpkg.BlockReferrerForFSObject(repoID, ambiguousUnknown.fsID)) || !publishRepairIntegrationHasReferrer(unknownRefs, unknownPub) {
		t.Fatalf("unknown publication outcome changed refs: %v", unknownRefs)
	}
	if !publishRepairIntegrationRepairRowExists(t, publishRepairIntegrationBucket(ambiguousUnknown.orgID, repoID, ambiguousUnknown.headCommitID, ambiguousUnknown.fsID), ambiguousUnknown.orgID, repoID, ambiguousUnknown.headCommitID, ambiguousUnknown.fsID) {
		t.Fatal("unknown publication outcome deleted the durable repair row")
	}
	markW2PostHeadEvidence(t, "ambiguous_confirmation_unavailable_retains")
	markW2PostHeadEvidence(t, "lease_expiry_is_not_authority")

	ancestor := upload("w2-reachable-ancestor.txt")
	_ = upload("w2-newer-head.txt")
	publishRepairIntegrationSeedQueuedRepair(t, database, repoID, ancestor, ancestor.headCommitID, time.Now().UTC(), time.Now().UTC().Add(5*time.Minute), true)
	if err := v2api.RepairPublishedFSObjectBlockReferenceRepair(database, ancestor.orgID, repoID, ancestor.headCommitID, ancestor.fsID, ancestor.internalBlockIDs); err != nil {
		t.Fatalf("reachable ancestor repair returned error: %v", err)
	}
	assertW2StateConverged(t, repoID, "w2-reachable-ancestor.txt", ancestor, ancestor.headCommitID)
	markW2PostHeadEvidence(t, "reachable_ancestor")

	// Reuse the W2 library already allocated above; the test environment has a
	// deliberately small active-library limit, and the race needs no new repo.
	casRepoID := repoID
	casHandler := newBorrowedFSHeadHandler(t, database, x1StorageClass(t))
	casA := newBorrowedFSHeadFixtureForRepo(t, database, casHandler, x1StorageClass(t), casRepoID)
	casB := newBorrowedFSHeadFixtureForRepo(t, database, casHandler, x1StorageClass(t), casRepoID)
	arrivals := make(chan struct{})
	release := make(chan struct{})
	var arrivalMu sync.Mutex
	arrivalCount := 0
	borrowedFSInstallBarriers(t, casA, func() {}, func() {}, func() {}, func() error {
		arrivalMu.Lock()
		arrivalCount++
		if arrivalCount == 2 {
			close(arrivals)
		}
		arrivalMu.Unlock()
		<-release
		return nil
	})
	casResults := make(chan *httptest.ResponseRecorder, 2)
	go func() { casResults <- casA.commit(t) }()
	go func() { casResults <- casB.commit(t) }()
	select {
	case <-arrivals:
	case <-time.After(20 * time.Second):
		close(release)
		t.Fatalf("real CAS-loser race did not bring both writers to the pre-HEAD barrier")
	}
	close(release)
	for i := 0; i < 2; i++ {
		select {
		case result := <-casResults:
			if result.Code != 200 {
				t.Fatalf("real CAS-loser writer %d failed: status=%d body=%s", i+1, result.Code, result.Body.String())
			}
		case <-time.After(30 * time.Second):
			t.Fatalf("real CAS-loser writer %d did not finish", i+1)
		}
	}
	casFSIDs := []string{
		publishRepairIntegrationLookupFileFSID(t, casRepoID, "/", casA.filename),
		publishRepairIntegrationLookupFileFSID(t, casRepoID, "/", casB.filename),
	}
	for _, fixture := range []*borrowedFSHeadFixture{casA, casB} {
		refs := publishRepairIntegrationBlockReferrers(t, database, fixture.orgID, fixture.blockID)
		fsID := publishRepairIntegrationLookupFileFSID(t, casRepoID, "/", fixture.filename)
		if !publishRepairIntegrationHasReferrer(refs, dbpkg.BlockReferrerForFSObject(casRepoID, fsID)) {
			t.Fatalf("real CAS-loser publication did not retain fs ref for %s: %v", fixture.filename, refs)
		}
		for _, ref := range refs {
			if strings.HasPrefix(ref, "pub:") {
				t.Fatalf("real CAS-loser cleanup left pub ref for %s: %v", fixture.filename, refs)
			}
		}
	}
	for bucket := 0; bucket < 32; bucket++ {
		iter := database.Session().Query(`
			SELECT fs_id FROM published_block_reference_repairs WHERE bucket = ?
		`, bucket).Iter()
		var repairFSID string
		for iter.Scan(&repairFSID) {
			for _, fsID := range casFSIDs {
				if repairFSID == fsID {
					t.Fatalf("real CAS-loser cleanup left a durable repair row for fs_object %s", fsID)
				}
			}
		}
		if err := iter.Close(); err != nil {
			t.Fatalf("scan CAS-loser repair bucket %d: %v", bucket, err)
		}
	}
	markW2PostHeadEvidence(t, "cas_loser_cleanup")

	t.Run("repairRetainsPreHeadRace", func(t *testing.T) {
		handler := newBorrowedFSHeadHandler(t, database, x1StorageClass(t))
		fx := newBorrowedFSHeadFixture(t, database, handler, x1StorageClass(t))
		entered := make(chan struct{})
		release := make(chan struct{})
		var releaseOnce sync.Once
		releaseWriter := func() { releaseOnce.Do(func() { close(release) }) }
		defer releaseWriter()
		borrowedFSInstallBarriers(t, fx, func() {}, func() {}, func() {}, func() error {
			close(entered)
			<-release
			return nil
		})

		type commitResult struct {
			code int
			body string
		}
		writerResult := make(chan commitResult, 1)
		go func() {
			rec := fx.commit(t)
			writerResult <- commitResult{code: rec.Code, body: rec.Body.String()}
		}()
		select {
		case <-entered:
		case result := <-writerResult:
			t.Fatalf("writer crossed pre-HEAD barrier unexpectedly: status=%d body=%s", result.code, result.body)
		case <-time.After(20 * time.Second):
			t.Fatal("writer did not reach the pre-HEAD barrier")
		}

		commitID, fsID := publishRepairIntegrationFindQueuedRepair(t, database, fx.orgID, fx.repoID, fx.blockID)
		if commitID == "" || fsID == "" {
			t.Fatal("pre-HEAD writer did not queue a durable repair row")
		}
		err := v2api.RepairPublishedFSObjectBlockReferenceRepair(database, fx.orgID, fx.repoID, commitID, fsID, []string{fx.blockID})
		if err == nil || !strings.Contains(err.Error(), "unknown") {
			t.Fatalf("pre-HEAD repair must retain an unresolved publication, error=%v", err)
		}
		if !publishRepairIntegrationRepairRowExists(t, publishRepairIntegrationBucket(fx.orgID, fx.repoID, commitID, fsID), fx.orgID, fx.repoID, commitID, fsID) {
			t.Fatal("pre-HEAD repair deleted its durable row")
		}
		referrers := publishRepairIntegrationBlockReferrers(t, database, fx.orgID, fx.blockID)
		if !publishRepairIntegrationHasReferrer(referrers, dbpkg.BlockReferrerForPublishAttempt(commitID)) {
			t.Fatalf("pre-HEAD repair removed pub: before HEAD publication: %v", referrers)
		}
		if publishRepairIntegrationHasReferrer(referrers, dbpkg.BlockReferrerForFSObject(fx.repoID, fsID)) {
			t.Fatalf("pre-HEAD repair promoted fs: before HEAD publication: %v", referrers)
		}

		releaseWriter()
		select {
		case result := <-writerResult:
			if result.code != 200 {
				t.Fatalf("writer failed after retained repair: status=%d body=%s", result.code, result.body)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("writer did not finish after pre-HEAD repair retained its artifacts")
		}
		fx.assertHeadAdvanced(t)
		finalRefs := publishRepairIntegrationBlockReferrers(t, database, fx.orgID, fx.blockID)
		if !publishRepairIntegrationHasReferrer(finalRefs, dbpkg.BlockReferrerForFSObject(fx.repoID, fsID)) || publishRepairIntegrationHasReferrer(finalRefs, dbpkg.BlockReferrerForPublishAttempt(commitID)) {
			t.Fatalf("pre-HEAD race did not converge after writer completed: %v", finalRefs)
		}
		if publishRepairIntegrationRepairRowExists(t, publishRepairIntegrationBucket(fx.orgID, fx.repoID, commitID, fsID), fx.orgID, fx.repoID, commitID, fsID) {
			t.Fatal("pre-HEAD race left a repair row after the writer settled")
		}
		markW2PostHeadEvidence(t, "pre_head_repair_race")
	})
}

// TestW2PublishedRepairRenewsLivenessBeforeClassify proves, against real
// Cassandra, that a visit which finds a live durable repair writes its
// repair-owned pub:<repo:commit:fsID> BEFORE the bounded ancestry classifier
// starts (ISSUE-PUBLISH-REPAIR-RENEWAL-AFTER-CLASSIFY-01). The classifier is
// held at its entry for this identity only; while it is held, the renewed
// pin must already be visible. Releasing it exercises the settlement legs
// with the production classifier: (1) a writer's ordinary
// ClearPublishedFSObjectBlockReferenceRepair landing while the walk is held
// deletes only the row, so the visit must remove the pin it wrote before the
// walk instead of leaving it ownerless for 35d (the window renew-first
// opens and must close itself); (2) after a requeue, a first UNKNOWN
// (bounded chunk under a deep synthetic HEAD) retains the row and the pin;
// (3) the next visit reaches the target and settles; (4) the write-ahead
// cleanup intent is the durable witness: it is visible before the walk,
// gone after every settlement, and a leftover intent with no repair row (the
// state a process loss leaves behind) is processed by the production sweep,
// which removes the pin and the intent while leaving an intent whose row is
// pending untouched. It does not claim continuity for a repair discovered
// after its prior pub: expired (ISSUE-PUBLISH-REPAIR-DISCOVERY-SCALE-01) nor
// during the per-block renewal fan-out itself.
func TestW2PublishedRepairRenewsLivenessBeforeClassify(t *testing.T) {
	if os.Getenv(w2PostHeadEvidenceEnv) != "1" {
		t.Skipf("%s is not enabled", w2PostHeadEvidenceEnv)
	}
	requireCassandra(t)

	database := shareProjectionDBForTest(t)
	repoID := createTestLibrary(t, adminClient, fmt.Sprintf("inttest-r31-renew-first-%d", time.Now().UnixNano()))
	fileName := "r31-renew-first.txt"
	uploadURL := getUploadLink(t, adminClient, repoID, "/")
	uploadFileThroughLink(t, adminClient, uploadURL, fileName, "/", fmt.Sprintf("r31 renew-first %d\n", time.Now().UnixNano()))
	state := publishRepairIntegrationReadFileState(t, repoID, "/", fileName)
	targetCommitID := state.headCommitID
	if strings.TrimSpace(targetCommitID) == "" {
		t.Fatal("library HEAD is empty before synthetic ancestry insert")
	}

	// Deep synthetic ancestry above the target so the first production walk
	// is a bounded UNKNOWN chunk rather than an immediate REACHABLE.
	session := database.Session()
	parentID := targetCommitID
	nonce := time.Now().UnixNano()
	creatorID := "00000000-0000-0000-0000-0000000000c1"
	now := time.Now().UTC()
	var tipCommitID string
	for i := 1; i <= v2api.PublishedCommitReachabilityMaxNodesForIntegration(); i++ {
		tipCommitID = fmt.Sprintf("r31rf-%d-%d", nonce, i)
		if err := session.Query(`
			INSERT INTO commits (library_id, commit_id, parent_id, root_fs_id, creator_id, description, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, repoID, tipCommitID, parentID, "r31-root", creatorID, "r31 renew-first ancestor", now).Exec(); err != nil {
			t.Fatalf("insert synthetic commit %s: %v", tipCommitID, err)
		}
		parentID = tipCommitID
	}
	if err := session.Query(`
		UPDATE libraries SET head_commit_id = ? WHERE org_id = ? AND library_id = ?
	`, tipCommitID, state.orgID, repoID).Exec(); err != nil {
		t.Fatalf("advance HEAD to depth %d: %v", v2api.PublishedCommitReachabilityMaxNodesForIntegration(), err)
	}

	fsReferrer := dbpkg.BlockReferrerForFSObject(repoID, state.fsID)
	priorPubReferrer := dbpkg.BlockReferrerForPublishAttempt(targetCommitID)
	repairPubReferrer := v2api.PublishedBlockReferenceRepairLivenessReferrerForIntegration(repoID, targetCommitID, state.fsID)
	// Prior liveness that is still valid when the visit starts but close to
	// expiry: exactly the window the old order left unprotected during the walk.
	for _, blockID := range state.internalBlockIDs {
		if err := database.RemoveBlockReference(state.orgID, blockID, fsReferrer); err != nil {
			t.Fatalf("remove fs ref: %v", err)
		}
		if err := database.AddBlockReference(state.orgID, blockID, priorPubReferrer, repoID, 90); err != nil {
			t.Fatalf("seed short-lived prior pub ref: %v", err)
		}
	}
	if err := v2api.QueuePublishedFSObjectBlockReferenceRepair(database, state.orgID, repoID, targetCommitID, state.fsID, state.internalBlockIDs); err != nil {
		t.Fatalf("queue repair: %v", err)
	}
	bucket := publishRepairIntegrationBucket(state.orgID, repoID, targetCommitID, state.fsID)
	t.Cleanup(func() {
		_ = session.Query(`
			UPDATE libraries SET head_commit_id = ? WHERE org_id = ? AND library_id = ?
		`, targetCommitID, state.orgID, repoID).Exec()
		_ = v2api.ClearPublishedFSObjectBlockReferenceRepair(database, state.orgID, repoID, targetCommitID, state.fsID)
		for _, blockID := range state.internalBlockIDs {
			_ = database.RemoveBlockReference(state.orgID, blockID, priorPubReferrer)
			_ = database.RemoveBlockReference(state.orgID, blockID, repairPubReferrer)
			_ = database.AddBlockReference(state.orgID, blockID, fsReferrer, repoID, 0)
		}
	})

	repairPubTTL := func(blockID string) (int, bool) {
		t.Helper()
		var ttl int
		err := session.Query(`
			SELECT TTL(created_at) FROM block_references WHERE org_id = ? AND block_id = ? AND referrer = ?
		`, state.orgID, blockID, repairPubReferrer).Scan(&ttl)
		if errors.Is(err, gocql.ErrNotFound) {
			return 0, false
		}
		if err != nil {
			t.Fatalf("read repair-owned pub TTL for %s: %v", blockID, err)
		}
		return ttl, true
	}
	intentExists := func() bool {
		t.Helper()
		exists, err := v2api.PublishedBlockReferenceRepairLivenessCleanupExistsForIntegration(database, state.orgID, repoID, targetCommitID, state.fsID)
		if err != nil {
			t.Fatalf("read cleanup intent: %v", err)
		}
		return exists
	}
	for _, blockID := range state.internalBlockIDs {
		if _, present := repairPubTTL(blockID); present {
			t.Fatalf("repair-owned pub: already present for %s before any visit", blockID)
		}
	}
	if intentExists() {
		t.Fatal("cleanup intent already present before any visit")
	}

	// gatedVisit runs one production visit and returns only after the
	// classifier for this identity has been entered; assertBeforeWalk runs
	// while the walk is still held.
	gatedVisit := func(assertBeforeWalk func()) error {
		t.Helper()
		entered := make(chan struct{})
		release := make(chan struct{})
		result := make(chan error, 1)
		go func() {
			result <- v2api.RepairPublishedFSObjectBlockReferenceRepairGatedForIntegration(database, state.orgID, repoID, targetCommitID, state.fsID, state.internalBlockIDs, func() {
				close(entered)
				<-release
			})
		}()
		select {
		case <-entered:
		case err := <-result:
			t.Fatalf("visit finished without entering the classifier: %v", err)
		case <-time.After(20 * time.Second):
			t.Fatal("visit did not reach the classifier")
		}
		assertBeforeWalk()
		close(release)
		select {
		case err := <-result:
			return err
		case <-time.After(2 * time.Minute):
			t.Fatal("visit did not finish after the classifier was released")
			return nil
		}
	}

	assertPinnedBeforeWalk := func() {
		t.Helper()
		if !publishRepairIntegrationRepairRowExists(t, bucket, state.orgID, repoID, targetCommitID, state.fsID) {
			t.Fatal("durable repair row is gone while the classifier is held")
		}
		if !intentExists() {
			t.Fatal("cleanup intent not visible while the classifier is held: the durable witness must precede the pin")
		}
		for _, blockID := range state.internalBlockIDs {
			ttl, present := repairPubTTL(blockID)
			if !present {
				t.Fatalf("repair-owned pub: %q not visible for %s while the classifier is held: renewal did not precede classification", repairPubReferrer, blockID)
			}
			if ttl < 30*24*60*60 {
				t.Fatalf("repair-owned pub TTL = %d for %s, want a fresh 35d renewal before the walk", ttl, blockID)
			}
		}
	}

	// External-clear leg: the pin is visible before the walk; a writer's
	// ordinary settlement deletes the row while the walk is held. The bounded
	// chunk then fails its cursor CAS against the missing row and reports
	// Gone; the visit must remove exactly the pin it wrote and nothing else.
	err := gatedVisit(func() {
		assertPinnedBeforeWalk()
		if err := v2api.ClearPublishedFSObjectBlockReferenceRepair(database, state.orgID, repoID, targetCommitID, state.fsID); err != nil {
			t.Fatalf("external clear while the classifier is held: %v", err)
		}
	})
	if err != nil {
		t.Fatalf("gated pass with external clear = %v, want nil terminal no-op", err)
	}
	if publishRepairIntegrationRepairRowExists(t, bucket, state.orgID, repoID, targetCommitID, state.fsID) {
		t.Fatal("externally cleared row reappeared after the visit")
	}
	if intentExists() {
		t.Fatal("cleanup intent left behind after a successful compensation")
	}
	for _, blockID := range state.internalBlockIDs {
		if _, present := repairPubTTL(blockID); present {
			t.Fatalf("repair-owned pub: left ownerless for %s after the row was cleared during the walk", blockID)
		}
		referrers := publishRepairIntegrationBlockReferrers(t, database, state.orgID, blockID)
		if !publishRepairIntegrationHasReferrer(referrers, priorPubReferrer) {
			t.Fatalf("visit removed a pin it does not own (%q) for %s: %v", priorPubReferrer, blockID, referrers)
		}
		if publishRepairIntegrationHasReferrer(referrers, fsReferrer) {
			t.Fatalf("row absence was treated as reachability: fs: promoted for %s: %v", blockID, referrers)
		}
	}

	// Requeue the same identity for the retention and settlement legs.
	if err := v2api.QueuePublishedFSObjectBlockReferenceRepair(database, state.orgID, repoID, targetCommitID, state.fsID, state.internalBlockIDs); err != nil {
		t.Fatalf("requeue repair: %v", err)
	}

	// UNKNOWN leg: pin visible before the walk; the bounded chunk retains.
	err = gatedVisit(assertPinnedBeforeWalk)
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("first gated pass = %v, want UNKNOWN limit retention", err)
	}
	if !publishRepairIntegrationRepairRowExists(t, bucket, state.orgID, repoID, targetCommitID, state.fsID) {
		t.Fatal("UNKNOWN visit deleted the durable repair row")
	}
	for _, blockID := range state.internalBlockIDs {
		if _, present := repairPubTTL(blockID); !present {
			t.Fatalf("UNKNOWN visit lost the repair-owned pub: for %s", blockID)
		}
		referrers := publishRepairIntegrationBlockReferrers(t, database, state.orgID, blockID)
		if publishRepairIntegrationHasReferrer(referrers, fsReferrer) {
			t.Fatalf("UNKNOWN visit promoted fs: for %s: %v", blockID, referrers)
		}
	}

	// REACHABLE leg: the next visit renews again before its walk, reaches the
	// target from the durable cursor, and settles.
	err = gatedVisit(assertPinnedBeforeWalk)
	if err != nil {
		t.Fatalf("second gated pass = %v, want REACHABLE settlement", err)
	}
	if publishRepairIntegrationRepairRowExists(t, bucket, state.orgID, repoID, targetCommitID, state.fsID) {
		t.Fatal("REACHABLE settlement left the durable repair row")
	}
	// The settling visit deleted its own witness; the UNKNOWN visit before it
	// left its armed witness by design (a retained row keeps its pin, and the
	// sweep consumes that witness once the row is gone). One sweep must now
	// clear every witness of this identity.
	if err := v2api.RunPublishedBlockReferenceRepairSweepForIntegration(database); err != nil && strings.Contains(err.Error(), state.fsID) {
		t.Fatalf("sweep after REACHABLE settlement: %v", err)
	}
	if intentExists() {
		t.Fatal("cleanup intents of a settled identity survived the sweep")
	}
	for _, blockID := range state.internalBlockIDs {
		referrers := publishRepairIntegrationBlockReferrers(t, database, state.orgID, blockID)
		if !publishRepairIntegrationHasReferrer(referrers, fsReferrer) {
			t.Fatalf("REACHABLE settlement did not restore fs: for %s: %v", blockID, referrers)
		}
		if publishRepairIntegrationHasReferrer(referrers, repairPubReferrer) {
			t.Fatalf("REACHABLE settlement left repair-owned pub: for %s: %v", blockID, referrers)
		}
	}

	// Durable rediscovery leg: the state a process loss leaves behind is a
	// pin plus its write-ahead intent and no repair row. One production
	// sweep must remove the pin and the intent. An intent whose repair row
	// is pending must survive that same sweep untouched.
	for _, blockID := range state.internalBlockIDs {
		if err := database.AddBlockReference(state.orgID, blockID, repairPubReferrer, repoID, dbpkg.PublishAttemptReferenceTTLSeconds); err != nil {
			t.Fatalf("seed orphaned repair-owned pub for %s: %v", blockID, err)
		}
	}
	if err := v2api.RecordPublishedBlockReferenceRepairLivenessCleanupForIntegration(database, state.orgID, repoID, targetCommitID, state.fsID, state.internalBlockIDs); err != nil {
		t.Fatalf("seed cleanup intent: %v", err)
	}
	if !intentExists() {
		t.Fatal("seeded cleanup intent not visible")
	}
	ownedCommit := fmt.Sprintf("r31rf-owned-%d", nonce)
	ownedFS := fmt.Sprintf("fs-owned-%d", nonce)
	ownedBlocks := state.internalBlockIDs
	ownedPubReferrer := v2api.PublishedBlockReferenceRepairLivenessReferrerForIntegration(repoID, ownedCommit, ownedFS)
	for _, blockID := range ownedBlocks {
		if err := database.AddBlockReference(state.orgID, blockID, ownedPubReferrer, repoID, dbpkg.PublishAttemptReferenceTTLSeconds); err != nil {
			t.Fatalf("seed owned repair pub for %s: %v", blockID, err)
		}
	}
	if err := v2api.QueuePublishedFSObjectBlockReferenceRepair(database, state.orgID, repoID, ownedCommit, ownedFS, ownedBlocks); err != nil {
		t.Fatalf("queue owned repair: %v", err)
	}
	if err := v2api.RecordPublishedBlockReferenceRepairLivenessCleanupForIntegration(database, state.orgID, repoID, ownedCommit, ownedFS, ownedBlocks); err != nil {
		t.Fatalf("seed owned cleanup intent: %v", err)
	}
	t.Cleanup(func() {
		_ = v2api.ClearPublishedFSObjectBlockReferenceRepair(database, state.orgID, repoID, ownedCommit, ownedFS)
		_ = session.Query(`
			DELETE FROM published_repair_liveness_cleanups WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
		`, v2api.PublishedBlockReferenceRepairBucketForIntegration(state.orgID, repoID, ownedCommit, ownedFS), state.orgID, repoID, ownedCommit, ownedFS).Exec()
		for _, blockID := range ownedBlocks {
			_ = database.RemoveBlockReference(state.orgID, blockID, ownedPubReferrer)
		}
	})
	// The freshly queued owned row is not yet eligible for a visit (young
	// created_at, live lease); the sweep only has intents to process here.
	if err := v2api.RunPublishedBlockReferenceRepairSweepForIntegration(database); err != nil && strings.Contains(err.Error(), state.fsID) {
		t.Fatalf("sweep over the orphaned intent: %v", err)
	}
	if intentExists() {
		t.Fatal("sweep left the orphaned cleanup intent")
	}
	for _, blockID := range state.internalBlockIDs {
		if _, present := repairPubTTL(blockID); present {
			t.Fatalf("sweep left the orphaned repair-owned pub: for %s", blockID)
		}
		referrers := publishRepairIntegrationBlockReferrers(t, database, state.orgID, blockID)
		if !publishRepairIntegrationHasReferrer(referrers, fsReferrer) {
			t.Fatalf("sweep removed a pin it does not own (fs:) for %s: %v", blockID, referrers)
		}
		if !publishRepairIntegrationHasReferrer(referrers, ownedPubReferrer) {
			t.Fatalf("sweep removed the pin of a pending repair (%q) for %s: %v", ownedPubReferrer, blockID, referrers)
		}
	}
	ownedIntent, err := v2api.PublishedBlockReferenceRepairLivenessCleanupExistsForIntegration(database, state.orgID, repoID, ownedCommit, ownedFS)
	if err != nil {
		t.Fatalf("read owned cleanup intent: %v", err)
	}
	if !ownedIntent {
		t.Fatal("sweep deleted the cleanup intent of a pending repair")
	}

	// Producer-fence leg. A PREPARING intent under a live lease is a producer
	// whose pin fan-out is in flight: the sweep must not consume it even
	// though its repair row is gone. A PREPARING intent whose lease has passed
	// is a producer that died mid fan-out: the sweep must consume it. Pins are
	// seeded with their lease as the write timestamp, exactly as the
	// production fan-out writes them, and leases only ever move forward, so
	// each producer's lease is later than every tombstone written before it.
	seedProducer := func(lease time.Time) {
		t.Helper()
		stamp := v2api.PublishedBlockReferenceRepairLeaseTimestampForIntegration(lease)
		for _, blockID := range state.internalBlockIDs {
			if err := session.Query(`
				INSERT INTO block_references (org_id, block_id, referrer, library_id, created_at)
				VALUES (?, ?, ?, ?, ?) USING TTL ? AND TIMESTAMP ?
			`, state.orgID, blockID, repairPubReferrer, repoID, time.Now().UTC(), dbpkg.PublishAttemptReferenceTTLSeconds, stamp).Exec(); err != nil {
				t.Fatalf("seed producer pin for %s: %v", blockID, err)
			}
		}
		if err := v2api.RecordPreparingPublishedBlockReferenceRepairLivenessCleanupForIntegration(database, state.orgID, repoID, targetCommitID, state.fsID, state.internalBlockIDs, lease); err != nil {
			t.Fatalf("seed preparing cleanup intent: %v", err)
		}
	}
	inFlightLease := time.Now().UTC().Add(10 * time.Minute)
	seedProducer(inFlightLease)
	if err := v2api.RunPublishedBlockReferenceRepairSweepForIntegration(database); err != nil && strings.Contains(err.Error(), state.fsID) {
		t.Fatalf("sweep over the preparing intent: %v", err)
	}
	if !intentExists() {
		t.Fatal("sweep consumed a PREPARING cleanup intent under a live lease: its producer may still be writing pins")
	}
	for _, blockID := range state.internalBlockIDs {
		if _, present := repairPubTTL(blockID); !present {
			t.Fatalf("sweep removed the pin of an in-flight producer for %s", blockID)
		}
	}
	// The same producer, seen by a sweep whose clock is past its lease: it
	// died mid fan-out and can no longer write; consume it.
	if err := v2api.RunPublishedBlockReferenceRepairSweepAtForIntegration(database, inFlightLease.Add(time.Second)); err != nil && strings.Contains(err.Error(), state.fsID) {
		t.Fatalf("sweep over the expired preparing intent: %v", err)
	}
	if intentExists() {
		t.Fatal("sweep left a PREPARING cleanup intent whose lease expired")
	}
	for _, blockID := range state.internalBlockIDs {
		if _, present := repairPubTTL(blockID); present {
			t.Fatalf("sweep left the pin of an expired producer for %s", blockID)
		}
	}

	// Timestamp-fence leg: the producer fence reaches the mutation itself. A
	// producer's pins carry USING TIMESTAMP = its lease and its cleanup
	// tombstones at that same timestamp; a write of that producer that lands
	// AFTER the cleanup (paused process, in-flight request) is shadowed by
	// the tombstone, while a later producer (later lease) is untouched.
	attemptID := v2api.PublishedBlockReferenceRepairLivenessAttemptIDForIntegration(repoID, targetCommitID, state.fsID)
	lease := inFlightLease.Add(time.Minute) // strictly after every tombstone this leg has written
	ts := v2api.PublishedBlockReferenceRepairLeaseTimestampForIntegration(lease)
	writePinAt := func(stamp int64) {
		t.Helper()
		for _, blockID := range state.internalBlockIDs {
			if err := session.Query(`
				INSERT INTO block_references (org_id, block_id, referrer, library_id, created_at)
				VALUES (?, ?, ?, ?, ?) USING TTL ? AND TIMESTAMP ?
			`, state.orgID, blockID, repairPubReferrer, repoID, time.Now().UTC(), dbpkg.PublishAttemptReferenceTTLSeconds, stamp).Exec(); err != nil {
				t.Fatalf("write pin at timestamp %d for %s: %v", stamp, blockID, err)
			}
		}
	}
	writePinAt(ts)
	for _, blockID := range state.internalBlockIDs {
		if _, present := repairPubTTL(blockID); !present {
			t.Fatalf("producer pin at its lease timestamp not visible for %s", blockID)
		}
	}
	if err := dbpkg.RemovePublishAttemptReferencesAt(database, state.orgID, attemptID, state.internalBlockIDs, ts); err != nil {
		t.Fatalf("tombstone at the producer lease timestamp: %v", err)
	}
	for _, blockID := range state.internalBlockIDs {
		if _, present := repairPubTTL(blockID); present {
			t.Fatalf("cleanup tombstone at the lease timestamp did not remove the pin for %s", blockID)
		}
	}
	writePinAt(ts) // the same producer's late write: same timestamp, after the tombstone
	for _, blockID := range state.internalBlockIDs {
		if _, present := repairPubTTL(blockID); present {
			t.Fatalf("a late write of the same producer revived the pin for %s: the fence did not reach the mutation", blockID)
		}
	}
	laterTS := v2api.PublishedBlockReferenceRepairLeaseTimestampForIntegration(lease.Add(time.Second))
	writePinAt(laterTS) // a later producer (later lease) is not shadowed
	for _, blockID := range state.internalBlockIDs {
		if _, present := repairPubTTL(blockID); !present {
			t.Fatalf("the older producer's tombstone shadowed a later producer's pin for %s", blockID)
		}
	}
	if err := dbpkg.RemovePublishAttemptReferencesAt(database, state.orgID, attemptID, state.internalBlockIDs, laterTS); err != nil {
		t.Fatalf("cleanup later producer pin: %v", err)
	}
	markW2PostHeadEvidence(t, "renewal_before_classify")
}

func cleanupIntentTokens(t *testing.T, session *gocql.Session, bucket int, orgID, repoID, commitID, fsID string) []string {
	t.Helper()
	iter := session.Query(`
		SELECT producer_token FROM published_repair_liveness_cleanups
		WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
	`, bucket, orgID, repoID, commitID, fsID).Iter()
	var token string
	var tokens []string
	for iter.Scan(&token) {
		tokens = append(tokens, token)
	}
	if err := iter.Close(); err != nil {
		t.Fatalf("list cleanup intent tokens: %v", err)
	}
	return tokens
}

func publishRepairIntegrationSeedQueuedRepair(t *testing.T, database *dbpkg.DB, repoID string, state publishRepairIntegrationFileState, commitID string, createdAt, leaseExpiresAt time.Time, removeFSRef bool) {
	t.Helper()
	fsReferrer := dbpkg.BlockReferrerForFSObject(repoID, state.fsID)
	pubReferrer := dbpkg.BlockReferrerForPublishAttempt(commitID)
	for _, blockID := range state.internalBlockIDs {
		if removeFSRef {
			if err := database.RemoveBlockReference(state.orgID, blockID, fsReferrer); err != nil {
				t.Fatalf("remove fs ref before W2 repair seed: %v", err)
			}
		} else if err := database.AddBlockReference(state.orgID, blockID, fsReferrer, repoID, 0); err != nil {
			t.Fatalf("restore winner fs ref before W2 loser seed: %v", err)
		}
		if err := database.AddBlockReference(state.orgID, blockID, pubReferrer, repoID, 0); err != nil {
			t.Fatalf("add pub ref before W2 repair seed: %v", err)
		}
	}
	if err := v2api.QueuePublishedFSObjectBlockReferenceRepair(database, state.orgID, repoID, commitID, state.fsID, state.internalBlockIDs); err != nil {
		t.Fatalf("queue W2 repair seed: %v", err)
	}
	bucket := publishRepairIntegrationBucket(state.orgID, repoID, commitID, state.fsID)
	if err := database.Session().Query(`
		UPDATE published_block_reference_repairs
		SET created_at = ?, lease_expires_at = ?
		WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
	`, createdAt, leaseExpiresAt, bucket, state.orgID, repoID, commitID, state.fsID).Exec(); err != nil {
		t.Fatalf("update W2 repair seed timestamps: %v", err)
	}
	t.Cleanup(func() {
		if err := v2api.ClearPublishedFSObjectBlockReferenceRepair(database, state.orgID, repoID, commitID, state.fsID); err != nil {
			t.Errorf("cleanup W2 repair row: %v", err)
		}
		for _, blockID := range state.internalBlockIDs {
			_ = database.RemoveBlockReference(state.orgID, blockID, pubReferrer)
			_ = database.AddBlockReference(state.orgID, blockID, fsReferrer, repoID, 0)
		}
	})
}

func publishRepairIntegrationFindQueuedRepair(t *testing.T, database *dbpkg.DB, orgID, repoID, blockID string) (string, string) {
	t.Helper()
	for bucket := 0; bucket < 32; bucket++ {
		iter := database.Session().Query(`
			SELECT org_id, repo_id, commit_id, fs_id
			FROM published_block_reference_repairs
			WHERE bucket = ?
		`, bucket).Iter()
		var rowOrgID, rowRepoID, commitID, fsID string
		for iter.Scan(&rowOrgID, &rowRepoID, &commitID, &fsID) {
			if rowOrgID != orgID || rowRepoID != repoID {
				continue
			}
			var blockIDs []string
			if err := database.Session().Query(`
				SELECT staged_block_ids
				FROM published_block_reference_repairs
				WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
			`, bucket, rowOrgID, rowRepoID, commitID, fsID).Scan(&blockIDs); err != nil {
				t.Fatalf("load queued repair block ids: %v", err)
			}
			for _, candidate := range blockIDs {
				if candidate == blockID {
					if err := iter.Close(); err != nil {
						t.Fatalf("close queued repair iterator: %v", err)
					}
					return commitID, fsID
				}
			}
		}
		if err := iter.Close(); err != nil {
			t.Fatalf("scan queued repairs for bucket %d: %v", bucket, err)
		}
	}
	return "", ""
}

func publishRepairIntegrationBlockReferrers(t *testing.T, database *dbpkg.DB, orgID, blockID string) []string {
	t.Helper()
	iter := database.Session().Query(`
		SELECT referrer FROM block_references WHERE org_id = ? AND block_id = ?
	`, orgID, blockID).Iter()
	var referrer string
	var referrers []string
	for iter.Scan(&referrer) {
		referrers = append(referrers, referrer)
	}
	if err := iter.Close(); err != nil {
		t.Fatalf("list block referrers for %s/%s: %v", orgID, blockID, err)
	}
	return referrers
}

func assertW2StateConverged(t *testing.T, repoID, fileName string, state publishRepairIntegrationFileState, commitID string) {
	t.Helper()
	referrers := uploadedFileBlockReferrers(t, repoID, "/", fileName)
	if !publishRepairIntegrationHasReferrer(referrers, dbpkg.BlockReferrerForFSObject(repoID, state.fsID)) || publishRepairIntegrationHasReferrer(referrers, dbpkg.BlockReferrerForPublishAttempt(commitID)) {
		t.Fatalf("W2 settlement did not converge for %s: %v", fileName, referrers)
	}
	if publishRepairIntegrationRepairRowExists(t, publishRepairIntegrationBucket(state.orgID, repoID, commitID, state.fsID), state.orgID, repoID, commitID, state.fsID) {
		t.Fatalf("W2 settlement left a repair row for %s", fileName)
	}
}

func publishRepairIntegrationReadFileState(t *testing.T, repoID, dirPath, fileName string) publishRepairIntegrationFileState {
	t.Helper()

	orgID := resolveOrgID(t, repoID)
	session := shareProjectionDBForTest(t).Session()
	fileFSID := publishRepairIntegrationLookupFileFSID(t, repoID, dirPath, fileName)

	var externalBlockIDs []string
	if err := session.Query(`SELECT block_ids FROM fs_objects WHERE library_id = ? AND fs_id = ?`, repoID, fileFSID).Scan(&externalBlockIDs); err != nil {
		t.Fatalf("failed to load block ids for %s/%s: %v", repoID, fileFSID, err)
	}
	if len(externalBlockIDs) == 0 {
		t.Fatalf("file %s/%s has no block ids", repoID, fileFSID)
	}

	internalBlockIDs := make([]string, 0, len(externalBlockIDs))
	for _, externalBlockID := range externalBlockIDs {
		var internalBlockID string
		err := session.Query(`SELECT internal_id FROM block_id_mappings WHERE org_id = ? AND representation_id = ? AND external_id = ?`, orgID, dbpkg.PlainBlockRepresentationID, externalBlockID).Scan(&internalBlockID)
		if err != nil {
			if errors.Is(err, gocql.ErrNotFound) {
				internalBlockID = externalBlockID
			} else {
				t.Fatalf("failed to resolve block mapping for %s/%s: %v", orgID, externalBlockID, err)
			}
		}
		internalBlockIDs = append(internalBlockIDs, internalBlockID)
	}

	var headCommitID string
	if err := session.Query(`SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, repoID).Scan(&headCommitID); err != nil {
		t.Fatalf("failed to read head commit for repo %s: %v", repoID, err)
	}
	if strings.TrimSpace(headCommitID) == "" {
		t.Fatalf("repo %s has empty head commit id", repoID)
	}

	return publishRepairIntegrationFileState{
		orgID:            orgID,
		headCommitID:     headCommitID,
		fsID:             fileFSID,
		internalBlockIDs: internalBlockIDs,
	}
}

func publishRepairIntegrationLookupFileFSID(t *testing.T, repoID, dirPath, fileName string) string {
	t.Helper()

	listResp := adminClient.Get(t, fmt.Sprintf("/api/v2.1/repos/%s/dir/?p=%s", repoID, url.QueryEscape(dirPath)))
	expectStatus(t, listResp, 200)
	listResult := responseJSON(t, listResp)
	entries, _ := listResult["dirent_list"].([]interface{})
	for _, rawEntry := range entries {
		entry, _ := rawEntry.(map[string]interface{})
		if name, _ := entry["name"].(string); name == fileName {
			if fsID, _ := entry["id"].(string); strings.TrimSpace(fsID) != "" {
				return fsID
			}
		}
	}
	t.Fatalf("file %q not found in repo=%s dir=%s", fileName, repoID, dirPath)
	return ""
}

func publishRepairIntegrationRepairRowExists(t *testing.T, bucket int, orgID, repoID, commitID, fsID string) bool {
	t.Helper()

	var storedFSID string
	err := shareProjectionDBForTest(t).Session().Query(`
		SELECT fs_id FROM published_block_reference_repairs
		WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?
	`, bucket, orgID, repoID, commitID, fsID).Scan(&storedFSID)
	if errors.Is(err, gocql.ErrNotFound) {
		return false
	}
	if err != nil {
		t.Fatalf("failed to read queued publish repair row: %v", err)
	}
	return storedFSID != ""
}

func publishRepairIntegrationAssertReferrers(t *testing.T, repoID, dirPath, fileName string, assertFn func([]string)) {
	t.Helper()
	assertFn(uploadedFileBlockReferrers(t, repoID, dirPath, fileName))
}

func publishRepairIntegrationHasReferrer(referrers []string, want string) bool {
	for _, referrer := range referrers {
		if referrer == want {
			return true
		}
	}
	return false
}

func publishRepairIntegrationBucket(orgID, repoID, commitID, fsID string) int {
	hasher := fnv.New32a()
	for _, part := range []string{orgID, repoID, commitID, fsID} {
		_, _ = hasher.Write([]byte(part))
		_, _ = hasher.Write([]byte{0})
	}
	return int(hasher.Sum32() % 32)
}
