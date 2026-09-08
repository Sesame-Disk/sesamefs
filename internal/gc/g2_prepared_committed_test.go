package gc

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func seedPreparedBlockDeleteOrphanForTest(t *testing.T, store *MockStore, orgID uuid.UUID, blockID string, authority BlockDeleteAuthority) {
	t.Helper()
	prepared := store.PrepareBlockDeleteOrphan(orgID, blockID, authority, "", authority.ClaimedAt)
	if prepared.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("seed PREPARED orphan: %s: %v", prepared.Outcome, prepared.Cause)
	}
}

func TestG2AbortSourceRequiresExactUncommittedOwner(t *testing.T) {
	text := formattedGCFunction(t, parseGCStoreFile(t), "AbortBlockDeleteHandoff")
	for _, required := range []string{
		"storage_class = ?",
		"storage_key = ?",
		"gc_claim_id = ?",
		"gc_claimed_at = ?",
		"gc_orphan_handoff = null",
		"SerialConsistency(gocql.Serial)",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("AbortBlockDeleteHandoff is missing exact-owner predicate %q", required)
		}
	}
}

func TestG2NotOwnerRecoveryClassifiesExactState(t *testing.T) {
	text := formattedGCFunction(t, parseGCWorkerFile(t), "recoverPreparedS3Orphan")
	for _, required := range []string{
		"GetS3OrphanExact",
		"switch strings.TrimSpace(exact.RecoveryState)",
		"case S3OrphanRecoveryStatePrepared:",
		"case S3OrphanRecoveryStateCommitted:",
		"DeletePreparedBlockDeleteOrphan",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("NotOwner recovery is missing exact-state classification %q", required)
		}
	}
}

func parseGCWorkerFile(t *testing.T) *ast.File {
	t.Helper()
	source, err := os.ReadFile("worker.go")
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "worker.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func TestG2DeletePreparedUsesExactRecoveryIdentity(t *testing.T) {
	text := formattedGCFunction(t, parseGCStoreFile(t), "DeletePreparedBlockDeleteOrphan")
	for _, required := range []string{
		"gc_claim_id = ?",
		"gc_claimed_at = ?",
		"IF recovery_state = ?",
		"DeleteS3OrphanRecoveryRoot",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("PREPARED cleanup is missing exact identity guard %q", required)
		}
	}
}

func blockAuthorityFromMockBlock(t *testing.T, store *MockStore, orgID uuid.UUID, blockID string) BlockDeleteAuthority {
	t.Helper()
	block := store.GetBlock(orgID, blockID)
	if block == nil || block.GCClaimedAt == nil {
		t.Fatalf("block %s has no claim authority: %+v", blockID, block)
	}
	return BlockDeleteAuthority{
		Target:    BlockDeleteTarget{StorageClass: block.StorageClass, StorageKey: block.StorageKey},
		ClaimID:   block.GCClaimID,
		ClaimedAt: *block.GCClaimedAt,
	}
}

func TestG2ProcessBlockStopsAtCommittedHandoff(t *testing.T) {
	store := NewMockStore()
	storage := &MockStorageProvider{}
	worker := NewWorker(store, storage, NewQueue(store), 100, 0, false, &Stats{})
	orgID := uuid.New()
	blockID := testSHA256BlockID("g2-stop-at-committed")
	store.AddBlock(orgID, blockID, "hot", 0)
	store.EnqueueBlockForTest(orgID, time.Now().Add(-2*time.Hour), blockID, "hot", 0)

	if _, err := worker.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("ProcessOnce returned error: %v", err)
	}
	if got := len(storage.ScopedBlockDeletes()); got != 0 {
		t.Fatalf("G2 must not delete physical bytes, got %d deletes", got)
	}
	block := store.GetBlock(orgID, blockID)
	if block == nil || block.GCState != "deleting" || block.GCOrphanHandoff == nil || !*block.GCOrphanHandoff {
		t.Fatalf("canonical block is not left at committed handoff: %+v", block)
	}
	authority := BlockDeleteAuthority{Target: BlockDeleteTarget{StorageClass: block.StorageClass, StorageKey: block.StorageKey}, ClaimID: block.GCClaimID, ClaimedAt: *block.GCClaimedAt}
	orphan, found, err := store.GetS3OrphanExact(orgID, blockID, authority)
	if err != nil || !found {
		t.Fatalf("committed orphan missing: found=%v err=%v", found, err)
	}
	if orphan.RecoveryState != S3OrphanRecoveryStateCommitted {
		t.Fatalf("recovery state = %q, want COMMITTED", orphan.RecoveryState)
	}
	if got := store.BlockDeleteLifecyclePhaseForTest(orgID, blockID, authority.ClaimID); got != BlockDeleteLifecyclePhasePublished {
		t.Fatalf("lifecycle phase = %q, want published", got)
	}
	if len(store.QueueItems(orgID)) != 1 {
		t.Fatal("G2 must leave the queue item for the later physical-delete executor")
	}
}

func TestG2PreparedAbortCleansOnlyPreparedState(t *testing.T) {
	store := NewMockStore()
	orgID := uuid.New()
	blockID := testSHA256BlockID("g2-abort-prepared")
	store.AddBlock(orgID, blockID, "hot", 0)
	now := time.Now().UTC().Add(-time.Hour)
	authority := store.SeedBlockClaimForTest(orgID, blockID, "g2-abort-claim", now)

	prepared := store.PrepareBlockDeleteOrphan(orgID, blockID, authority, "sha1", now)
	if prepared.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("PrepareBlockDeleteOrphan outcome = %s: %v", prepared.Outcome, prepared.Cause)
	}
	abort := store.AbortBlockDeleteHandoff(orgID, blockID, authority)
	if abort.Outcome != BlockDeleteAbortApplied {
		t.Fatalf("AbortBlockDeleteHandoff outcome = %s: %v", abort.Outcome, abort.Cause)
	}
	if err := store.DeletePreparedBlockDeleteOrphan(orgID, blockID, authority); err != nil {
		t.Fatalf("DeletePreparedBlockDeleteOrphan: %v", err)
	}
	if _, found, err := store.GetS3OrphanExact(orgID, blockID, authority); err != nil || found {
		t.Fatalf("PREPARED orphan remains after abort cleanup: found=%v err=%v", found, err)
	}
	block := store.GetBlock(orgID, blockID)
	if block.GCState != "" || block.GCClaimID != "" || block.GCClaimedAt != nil {
		t.Fatalf("abort left a block claim behind: %+v", block)
	}
}

func TestG2AbortRefusesCommittedHandoff(t *testing.T) {
	store := NewMockStore()
	orgID := uuid.New()
	blockID := testSHA256BlockID("g2-abort-committed-handoff")
	store.AddBlock(orgID, blockID, "hot", 0)
	authority := store.SeedBlockClaimForTest(orgID, blockID, "g2-committed-claim", time.Now().UTC().Add(-time.Hour))
	store.SeedBlockHandoffForTest(orgID, blockID)

	abort := store.AbortBlockDeleteHandoff(orgID, blockID, authority)
	if abort.Outcome != BlockDeleteAbortCommitted {
		t.Fatalf("abort committed handoff = %s: %v, want committed", abort.Outcome, abort.Cause)
	}
	block := store.GetBlock(orgID, blockID)
	if block.GCState != "deleting" || block.GCClaimID != authority.ClaimID || block.GCOrphanHandoff == nil || !*block.GCOrphanHandoff {
		t.Fatalf("abort changed committed handoff: %+v", block)
	}
}

func TestG2RecoveryAfterAbortCrashClassifiesNotOwnerAndCleansPrepared(t *testing.T) {
	store := NewMockStore()
	worker := NewWorker(store, nil, NewQueue(store), 100, 0, false, &Stats{})
	orgID := uuid.New()
	blockID := testSHA256BlockID("g2-abort-crash-before-cleanup")
	store.AddBlock(orgID, blockID, "hot", 0)
	now := time.Now().UTC().Add(-time.Hour)
	authority := store.SeedBlockClaimForTest(orgID, blockID, "g2-abort-crash-claim", now)
	prepared := store.PrepareBlockDeleteOrphan(orgID, blockID, authority, "sha1", now)
	if prepared.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("prepare outcome = %s: %v", prepared.Outcome, prepared.Cause)
	}
	if abort := store.AbortBlockDeleteHandoff(orgID, blockID, authority); abort.Outcome != BlockDeleteAbortApplied {
		t.Fatalf("abort outcome = %s: %v", abort.Outcome, abort.Cause)
	}

	// The process crashed after the abort CAS and before DeletePrepared ran.
	canonical, found, err := store.GetS3OrphanExact(orgID, blockID, authority)
	if err != nil || !found {
		t.Fatalf("PREPARED row missing in simulated crash window: found=%v err=%v", found, err)
	}
	if err := worker.recoverPreparedS3Orphan(canonical); err != nil {
		t.Fatalf("recovery after abort crash: %v", err)
	}
	if _, found, err := store.GetS3OrphanExact(orgID, blockID, authority); err != nil || found {
		t.Fatalf("NotOwner recovery did not clean PREPARED: found=%v err=%v", found, err)
	}
}

func TestG2AbortAndCommitRaceHasOneIrreversibleWinner(t *testing.T) {
	store := NewMockStore()
	orgID := uuid.New()
	blockID := testSHA256BlockID("g2-abort-commit-race")
	store.AddBlock(orgID, blockID, "hot", 0)
	authority := store.SeedBlockClaimForTest(orgID, blockID, "g2-race-claim", time.Now().UTC().Add(-time.Hour))
	if got := store.PrepareBlockDeleteOrphan(orgID, blockID, authority, "sha1", time.Now().UTC()); got.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("prepare outcome = %s: %v", got.Outcome, got.Cause)
	}

	var wg sync.WaitGroup
	var abort BlockDeleteAbortResult
	var commit BlockDeleteHandoffResult
	wg.Add(2)
	go func() {
		defer wg.Done()
		abort = store.AbortBlockDeleteHandoff(orgID, blockID, authority)
	}()
	go func() {
		defer wg.Done()
		commit, _ = store.CommitBlockDeleteOrphanHandoff(orgID, blockID, authority)
	}()
	wg.Wait()

	switch {
	case abort.Outcome == BlockDeleteAbortApplied:
		if commit.Outcome == BlockDeleteHandoffCommitted || commit.Outcome == BlockDeleteHandoffAlreadyCommitted {
			t.Fatal("commit applied after abort cleared the exact owner")
		}
	case abort.Outcome == BlockDeleteAbortCommitted:
		if commit.Outcome != BlockDeleteHandoffCommitted && commit.Outcome != BlockDeleteHandoffAlreadyCommitted {
			t.Fatalf("abort observed committed, but commit outcome was %s", commit.Outcome)
		}
	default:
		t.Fatalf("unexpected abort race outcome: %s", abort.Outcome)
	}
}

func TestG2RecoveryAbortsPreparedWithoutStorageManager(t *testing.T) {
	store := NewMockStore()
	worker := NewWorker(store, nil, NewQueue(store), 100, 0, false, &Stats{})
	orgID := uuid.New()
	blockID := testSHA256BlockID("g2-recovery-without-storage")
	store.AddBlock(orgID, blockID, "hot", 0)
	authority := store.SeedBlockClaimForTest(orgID, blockID, "g2-recovery-claim", time.Now().UTC().Add(-time.Hour))
	prepared := store.PrepareBlockDeleteOrphan(orgID, blockID, authority, "sha1", time.Now().UTC())
	if prepared.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("prepare outcome = %s: %v", prepared.Outcome, prepared.Cause)
	}

	if _, err := worker.RecoverS3Orphans(context.Background(), 100); err != nil {
		t.Fatalf("RecoverS3Orphans returned error: %v", err)
	}
	if _, found, err := store.GetS3OrphanExact(orgID, blockID, authority); err != nil || found {
		t.Fatalf("PREPARED orphan was not cleaned after recovery: found=%v err=%v", found, err)
	}
	if block := store.GetBlock(orgID, blockID); block.GCState != "" {
		t.Fatalf("prepared recovery left claim state %q", block.GCState)
	}
}

func TestG2RecoveryRetainsAmbiguousPrepared(t *testing.T) {
	store := NewMockStore()
	worker := NewWorker(store, nil, NewQueue(store), 100, 0, false, &Stats{})
	orgID := uuid.New()
	blockID := testSHA256BlockID("g2-ambiguous-abort")
	store.AddBlock(orgID, blockID, "hot", 0)
	authority := store.SeedBlockClaimForTest(orgID, blockID, "g2-ambiguous-claim", time.Now().UTC().Add(-time.Hour))
	if got := store.PrepareBlockDeleteOrphan(orgID, blockID, authority, "sha1", time.Now().UTC()); got.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("prepare outcome = %s: %v", got.Outcome, got.Cause)
	}
	store.SetAbortBlockDeleteHandoffAmbiguousOnceForTest()
	if _, err := worker.RecoverS3Orphans(context.Background(), 100); err == nil {
		t.Fatal("ambiguous abort should defer recovery")
	}
	if _, found, err := store.GetS3OrphanExact(orgID, blockID, authority); err != nil || !found {
		t.Fatalf("ambiguous abort must retain PREPARED orphan: found=%v err=%v", found, err)
	}
}

func TestG2RecoveryRetainsPreparedWhenPromotionIsAmbiguous(t *testing.T) {
	store := NewMockStore()
	worker := NewWorker(store, nil, NewQueue(store), 100, 0, false, &Stats{})
	orgID := uuid.New()
	blockID := testSHA256BlockID("g2-ambiguous-promotion")
	store.AddBlock(orgID, blockID, "hot", 0)
	store.EnqueueBlockForTest(orgID, time.Now().UTC().Add(-time.Hour), blockID, "hot", 0)
	store.SetPromoteBlockDeleteOrphanAmbiguousOnceForTest()

	if _, err := worker.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("ProcessOnce returned error: %v", err)
	}
	authority := blockAuthorityFromMockBlock(t, store, orgID, blockID)
	orphan, found, err := store.GetS3OrphanExact(orgID, blockID, authority)
	if err != nil || !found {
		t.Fatalf("ambiguous promotion lost PREPARED orphan: found=%v err=%v", found, err)
	}
	if orphan.RecoveryState != S3OrphanRecoveryStatePrepared {
		t.Fatalf("recovery state = %q after ambiguous promotion, want PREPARED", orphan.RecoveryState)
	}
	if len(store.QueueItems(orgID)) != 1 {
		t.Fatal("ambiguous promotion must leave the queue item for retry")
	}
}

func TestG2RetainsPreparedWhenCommitIsAmbiguous(t *testing.T) {
	store := NewMockStore()
	storage := &MockStorageProvider{}
	worker := NewWorker(store, storage, NewQueue(store), 100, 0, false, &Stats{})
	orgID := uuid.New()
	blockID := testSHA256BlockID("g2-ambiguous-commit")
	store.AddBlock(orgID, blockID, "hot", 0)
	store.EnqueueBlockForTest(orgID, time.Now().UTC().Add(-time.Hour), blockID, "hot", 0)
	store.SetCommitHandoffErrForTest(errors.New("test: commit LWT timeout"))
	store.SetCommitHandoffSettleErrForTest(errors.New("test: serial settlement unavailable"))

	if _, err := worker.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("ProcessOnce returned error: %v", err)
	}
	authority := blockAuthorityFromMockBlock(t, store, orgID, blockID)
	orphan, found, err := store.GetS3OrphanExact(orgID, blockID, authority)
	if err != nil || !found {
		t.Fatalf("ambiguous commit lost PREPARED orphan: found=%v err=%v", found, err)
	}
	if orphan.RecoveryState != S3OrphanRecoveryStatePrepared {
		t.Fatalf("recovery state = %q after ambiguous commit, want PREPARED", orphan.RecoveryState)
	}
	block := store.GetBlock(orgID, blockID)
	if block.GCOrphanHandoff != nil && *block.GCOrphanHandoff {
		t.Fatal("ambiguous commit incorrectly published the irreversible handoff")
	}
	if len(store.QueueItems(orgID)) != 1 {
		t.Fatal("ambiguous commit must leave the queue item for retry")
	}
	if got := storage.ScopedBlockDeletes(); len(got) != 0 {
		t.Fatalf("ambiguous commit authorized physical delete: %v", got)
	}
}

func TestG2PreparedMissingProjectionIsRecoveredFromRoot(t *testing.T) {
	store := NewMockStore()
	worker := NewWorker(store, nil, NewQueue(store), 100, 0, false, &Stats{})
	orgID := uuid.New()
	blockID := testSHA256BlockID("g2-prepared-missing-projection")
	store.AddBlock(orgID, blockID, "hot", 0)
	authority := store.SeedBlockClaimForTest(orgID, blockID, "g2-missing-projection-claim", time.Now().UTC().Add(-time.Hour))
	prepared := store.PrepareBlockDeleteOrphan(orgID, blockID, authority, "sha1", time.Now().UTC())
	if prepared.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("prepare outcome = %s: %v", prepared.Outcome, prepared.Cause)
	}
	store.DeleteS3OrphanProjectionForTest(orgID, blockID, prepared.FirstSeenAt)

	recovered, err := worker.RecoverS3Orphans(context.Background(), 100)
	if err != nil || recovered != 1 {
		t.Fatalf("root recovery of PREPARED orphan = (%d, %v), want one settled orphan", recovered, err)
	}
	if _, found, err := store.GetS3OrphanExact(orgID, blockID, authority); err != nil || found {
		t.Fatalf("root recovery left PREPARED canonical row: found=%v err=%v", found, err)
	}
	if g1RootCount(t, store) != 0 {
		t.Fatal("root recovery left the independent recovery root")
	}
}

func TestG2RecoverySettlesRootAfterPreparedCanonicalCleanupCrash(t *testing.T) {
	store := NewMockStore()
	worker := NewWorker(store, nil, NewQueue(store), 100, 0, false, &Stats{})
	orgID := uuid.New()
	blockID := testSHA256BlockID("g2-root-after-prepared-cleanup-crash")
	store.AddBlock(orgID, blockID, "hot", 0)
	authority := store.SeedBlockClaimForTest(orgID, blockID, "g2-root-cleanup-claim", time.Now().UTC().Add(-time.Hour))
	prepared := store.PrepareBlockDeleteOrphan(orgID, blockID, authority, "sha1", time.Now().UTC())
	if prepared.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("prepare outcome = %s: %v", prepared.Outcome, prepared.Cause)
	}
	if abort := store.AbortBlockDeleteHandoff(orgID, blockID, authority); abort.Outcome != BlockDeleteAbortApplied {
		t.Fatalf("abort outcome = %s: %v", abort.Outcome, abort.Cause)
	}
	// Model a process crash after the canonical PREPARED row was removed but
	// before its projection and independent recovery root were cleaned.
	store.DeleteS3OrphanCanonicalForTest(orgID, blockID)

	recovered, err := worker.RecoverS3Orphans(context.Background(), 100)
	if err != nil || recovered != 1 {
		t.Fatalf("root-only PREPARED cleanup = (%d, %v), want one settled root", recovered, err)
	}
	if g1RootCount(t, store) != 0 {
		t.Fatal("released PREPARED recovery root was retained after exact block observation")
	}
	if _, found := store.GetS3OrphanProjectionForTest(orgID, blockID, prepared.FirstSeenAt); found {
		t.Fatal("released PREPARED discovery projection was retained")
	}
}

func TestG2RecoveryRetainsRootWhilePreparedOwnerCanStillCommit(t *testing.T) {
	store := NewMockStore()
	worker := NewWorker(store, nil, NewQueue(store), 100, 0, false, &Stats{})
	orgID := uuid.New()
	blockID := testSHA256BlockID("g2-root-owner-still-live")
	store.AddBlock(orgID, blockID, "hot", 0)
	authority := store.SeedBlockClaimForTest(orgID, blockID, "g2-root-live-claim", time.Now().UTC().Add(-time.Hour))
	prepared := store.PrepareBlockDeleteOrphan(orgID, blockID, authority, "sha1", time.Now().UTC())
	if prepared.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("prepare outcome = %s: %v", prepared.Outcome, prepared.Cause)
	}
	store.DeleteS3OrphanCanonicalForTest(orgID, blockID)

	recovered, err := worker.RecoverS3Orphans(context.Background(), 100)
	if err != nil || recovered != 0 {
		t.Fatalf("live-owner root cleanup = (%d, %v), want retained root", recovered, err)
	}
	if g1RootCount(t, store) != 1 {
		t.Fatal("root was removed while its exact PREPARED owner could still commit")
	}
}

func TestG2RecoveryPromotesCommittedHandoffAfterCrash(t *testing.T) {
	store := NewMockStore()
	worker := NewWorker(store, nil, NewQueue(store), 100, 0, false, &Stats{})
	orgID := uuid.New()
	blockID := testSHA256BlockID("g2-promote-after-commit")
	store.AddBlock(orgID, blockID, "hot", 0)
	authority := store.SeedBlockClaimForTest(orgID, blockID, "g2-promote-claim", time.Now().UTC().Add(-time.Hour))
	if got := store.PrepareBlockDeleteOrphan(orgID, blockID, authority, "sha1", time.Now().UTC()); got.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("prepare outcome = %s: %v", got.Outcome, got.Cause)
	}
	committed, err := store.CommitBlockDeleteOrphanHandoff(orgID, blockID, authority)
	if err != nil || committed.Outcome != BlockDeleteHandoffCommitted {
		t.Fatalf("commit outcome = %s: %v", committed.Outcome, err)
	}

	if err := worker.recoverPreparedS3Orphan(S3OrphanInfo{OrgID: orgID, BlockID: blockID, Authority: authority, RecoveryState: S3OrphanRecoveryStatePrepared}); err != nil {
		t.Fatalf("promote committed handoff: %v", err)
	}
	orphan, found, err := store.GetS3OrphanExact(orgID, blockID, authority)
	if err != nil || !found || orphan.RecoveryState != S3OrphanRecoveryStateCommitted {
		t.Fatalf("recovery state after promotion = %+v, found=%v err=%v", orphan, found, err)
	}
	if store.BlockDeleteLifecyclePhaseForTest(orgID, blockID, authority.ClaimID) != BlockDeleteLifecyclePhasePublished {
		t.Fatal("promotion did not publish the durable lifecycle")
	}
}

func TestG2RecoveryCleansSupersededPreparedWithoutTouchingNewClaim(t *testing.T) {
	store := NewMockStore()
	worker := NewWorker(store, nil, NewQueue(store), 100, 0, false, &Stats{})
	orgID := uuid.New()
	blockID := testSHA256BlockID("g2-superseded-prepared")
	store.AddBlock(orgID, blockID, "hot", 0)
	p1 := store.SeedBlockClaimForTest(orgID, blockID, "g2-p1", time.Now().UTC().Add(-time.Hour))
	if got := store.PrepareBlockDeleteOrphan(orgID, blockID, p1, "sha1", time.Now().UTC()); got.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("prepare P1 outcome = %s: %v", got.Outcome, got.Cause)
	}
	store.SetBlockStorageKeyForTest(orgID, blockID, MockCanonicalStorageKey(orgID.String(), blockID)+"-p2")
	p2 := store.SeedBlockClaimForTest(orgID, blockID, "g2-p2", time.Now().UTC())

	if err := worker.recoverPreparedS3Orphan(S3OrphanInfo{OrgID: orgID, BlockID: blockID, Authority: p1, RecoveryState: S3OrphanRecoveryStatePrepared}); err != nil {
		t.Fatalf("recover superseded P1: %v", err)
	}
	if _, found, err := store.GetS3OrphanExact(orgID, blockID, p1); err != nil || found {
		t.Fatalf("P1 PREPARED orphan remains: found=%v err=%v", found, err)
	}
	block := store.GetBlock(orgID, blockID)
	if block.GCClaimID != p2.ClaimID || block.GCState != "deleting" {
		t.Fatalf("P2 claim was modified while cleaning P1: %+v", block)
	}
}

func TestG2RecoveryCleansOnlyD1WhenD2SharesPhysicalTarget(t *testing.T) {
	store := NewMockStore()
	worker := NewWorker(store, nil, NewQueue(store), 100, 0, false, &Stats{})
	orgID := uuid.New()
	blockID := testSHA256BlockID("g2-d1-d2-same-target")
	store.AddBlock(orgID, blockID, "hot", 0)
	d1 := store.SeedBlockClaimForTest(orgID, blockID, "g2-same-target-d1", time.Now().UTC().Add(-time.Hour))
	if got := store.PrepareBlockDeleteOrphan(orgID, blockID, d1, "sha1-d1", time.Now().UTC()); got.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("prepare D1 outcome = %s: %v", got.Outcome, got.Cause)
	}
	d2 := store.SeedBlockClaimForTest(orgID, blockID, "g2-same-target-d2", time.Now().UTC())
	if got := store.PrepareBlockDeleteOrphan(orgID, blockID, d2, "sha1-d2", time.Now().UTC()); got.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("prepare D2 outcome = %s: %v", got.Outcome, got.Cause)
	}

	if err := worker.recoverPreparedS3Orphan(S3OrphanInfo{OrgID: orgID, BlockID: blockID, Authority: d1, RecoveryState: S3OrphanRecoveryStatePrepared}); err != nil {
		t.Fatalf("recover D1 with D2 current: %v", err)
	}
	if _, found, err := store.GetS3OrphanExact(orgID, blockID, d1); err != nil || found {
		t.Fatalf("D1 PREPARED remains: found=%v err=%v", found, err)
	}
	if _, found, err := store.GetS3OrphanExact(orgID, blockID, d2); err != nil || !found {
		t.Fatalf("D2 PREPARED was touched while cleaning D1: found=%v err=%v", found, err)
	}
	block := store.GetBlock(orgID, blockID)
	if block.GCClaimID != d2.ClaimID || block.GCState != "deleting" {
		t.Fatalf("D2 claim changed while cleaning D1: %+v", block)
	}
}

func TestG2PromoteRequiresCommittedHandoff(t *testing.T) {
	store := NewMockStore()
	orgID := uuid.New()
	blockID := testSHA256BlockID("g2-promote-without-handoff")
	store.AddBlock(orgID, blockID, "hot", 0)
	authority := store.SeedBlockClaimForTest(orgID, blockID, "g2-uncommitted-claim", time.Now().UTC().Add(-time.Hour))
	if got := store.PrepareBlockDeleteOrphan(orgID, blockID, authority, "sha1", time.Now().UTC()); got.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("prepare outcome = %s: %v", got.Outcome, got.Cause)
	}

	promotion := store.PromoteBlockDeleteOrphan(orgID, blockID, committedBlockDeleteAuthority(authority))
	if promotion.Outcome == StartBlockDeleteOrphanCreated || promotion.Outcome == StartBlockDeleteOrphanSameAuthority {
		t.Fatalf("promotion without committed handoff = %s, want refusal", promotion.Outcome)
	}
	orphan, found, err := store.GetS3OrphanExact(orgID, blockID, authority)
	if err != nil || !found || orphan.RecoveryState != S3OrphanRecoveryStatePrepared {
		t.Fatalf("promotion without handoff changed PREPARED state: orphan=%+v found=%v err=%v", orphan, found, err)
	}
}
