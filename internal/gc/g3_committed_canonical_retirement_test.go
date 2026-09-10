package gc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// G3 — canonical retirement after committed handoff.
//
// The property under test throughout this file:
//
//	orphan(P1,D1) == COMMITTED  (confirmed separately in the recovery partition
//	                             after exact blocks(P1,D1) authority was settled
//	                             in the blocks partition's SERIAL domain)
//	        =>  FinalizeBlockDelete(P1,D1) may retire blocks(L)
//	        =>  orphan(P1,D1) remains COMMITTED and durable afterward.
//
// PromoteBlockDeleteOrphan's own success (Created/SameAuthority) already IS
// that exact confirmation — see store_cassandra.go: it settles the exact
// blocks authority, requires gc_orphan_handoff=true, and only then observes
// the orphan at EACH_QUORUM. So the invariant this file pins is really that
// processBlock's gate (the switch in processBlock, immediately before
// finalizeAfterCommittedHandoff) never lets a non-success Promote outcome
// reach Finalize — because FinalizeBlockDelete's own CAS only consults
// `blocks.gc_orphan_handoff`, which is already true as soon as
// CommitBlockDeleteOrphanHandoff applies, BEFORE the orphan is promoted to
// COMMITTED. Skipping the gate would let Finalize retire blocks(L) while the
// orphan is still merely PREPARED.

func testG3Worker(store *MockStore) *Worker {
	return NewWorker(store, &MockStorageProvider{}, NewQueue(store), 100, 0, false, &Stats{})
}

// TestG3PromoteAmbiguousNeverReachesFinalize covers G3-1 (no Finalize before
// COMMITTED) for the case where Promote's own confirmation is ambiguous:
// D is committed on `blocks`, but the orphan's exact COMMITTED state was not
// itself confirmed. FinalizeBlockDelete must never run here, because its CAS
// alone cannot tell PREPARED from COMMITTED.
func TestG3PromoteAmbiguousNeverReachesFinalize(t *testing.T) {
	store := NewMockStore()
	worker := testG3Worker(store)
	orgID := uuid.New()
	blockID := testSHA256BlockID("g3-promote-ambiguous-no-finalize")
	store.AddBlock(orgID, blockID, "hot", 0)
	store.EnqueueBlockForTest(orgID, time.Now().UTC().Add(-time.Hour), blockID, "hot", 0)
	store.SetPromoteBlockDeleteOrphanAmbiguousOnceForTest()

	if _, err := worker.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("ProcessOnce returned a fatal error: %v", err)
	}

	if block := store.GetBlock(orgID, blockID); block == nil {
		t.Fatal("ambiguous promotion must not retire blocks(L): canonical row is gone")
	}
	if got := len(store.QueueItems(orgID)); got != 1 {
		t.Fatalf("queue items = %d, want 1 retained: ambiguous promotion must not complete the item", got)
	}
	if got := store.QueueCompleteCallsForTest(); got != 0 {
		t.Fatalf("queue completion calls = %d, want 0", got)
	}
}

// TestG3CommitAmbiguousNeverReachesFinalize covers the same G3-1 property one
// step earlier: the orphan-handoff commit CAS itself is unsettled, so `blocks`
// may or may not carry gc_orphan_handoff=true yet. Finalize must not run.
func TestG3CommitAmbiguousNeverReachesFinalize(t *testing.T) {
	store := NewMockStore()
	worker := testG3Worker(store)
	orgID := uuid.New()
	blockID := testSHA256BlockID("g3-commit-ambiguous-no-finalize")
	store.AddBlock(orgID, blockID, "hot", 0)
	store.EnqueueBlockForTest(orgID, time.Now().UTC().Add(-time.Hour), blockID, "hot", 0)
	store.SetCommitHandoffErrForTest(errors.New("test: commit LWT timeout"))
	store.SetCommitHandoffSettleErrForTest(errors.New("test: serial settlement unavailable"))

	if _, err := worker.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("ProcessOnce returned a fatal error: %v", err)
	}

	if block := store.GetBlock(orgID, blockID); block == nil {
		t.Fatal("ambiguous commit must not retire blocks(L): canonical row is gone")
	}
	if block := store.GetBlock(orgID, blockID); block != nil && block.GCOrphanHandoff != nil && *block.GCOrphanHandoff {
		t.Fatal("ambiguous commit must not have published the irreversible handoff")
	}
	if got := len(store.QueueItems(orgID)); got != 1 {
		t.Fatalf("queue items = %d, want 1 retained: ambiguous commit must not complete the item", got)
	}
}

// TestG3PreparedAloneNeverAuthorizesFinalize is the direct store-level
// contract: a PREPARED orphan (D not yet committed on `blocks`) can never
// satisfy FinalizeBlockDelete's own CAS, because gc_orphan_handoff is not yet
// true. This is G3-1's "PREPARED -> Finalize" case, pinned independently of
// processBlock's gate.
func TestG3PreparedAloneNeverAuthorizesFinalize(t *testing.T) {
	store := NewMockStore()
	orgID := uuid.New()
	blockID := testSHA256BlockID("g3-prepared-alone-no-finalize")
	store.AddBlock(orgID, blockID, "hot", 0)
	authority := store.SeedBlockClaimForTest(orgID, blockID, "g3-prepared-claim", time.Now().UTC().Add(-time.Hour))
	prepared := store.PrepareBlockDeleteOrphan(orgID, blockID, authority, "sha1", time.Now().UTC())
	if prepared.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("prepare outcome = %s: %v", prepared.Outcome, prepared.Cause)
	}

	result, err := store.FinalizeBlockDelete(orgID, blockID, committedBlockDeleteAuthority(authority))
	if err == nil || result.Outcome == BlockDeleteFinalized || result.Outcome == BlockDeleteAlreadyFinalized {
		t.Fatalf("FinalizeBlockDelete from PREPARED = (%+v, %v), want refusal", result, err)
	}
	if block := store.GetBlock(orgID, blockID); block == nil {
		t.Fatal("refused finalize must not retire blocks(L)")
	}
	if orphan, found, err := store.GetS3OrphanExact(orgID, blockID, authority); err != nil || !found || orphan.RecoveryState != S3OrphanRecoveryStatePrepared {
		t.Fatalf("refused finalize disturbed the PREPARED orphan: orphan=%+v found=%v err=%v", orphan, found, err)
	}
}

// TestG3FinalizeExactPMismatchFailsClosed is the exact-P half of G3-2 / the
// exact-P ABA requirement: a committed authority for target Ptarget must never
// authorize Finalize against a different physical target, even for the same
// claim id and claimed_at.
func TestG3FinalizeExactPMismatchFailsClosed(t *testing.T) {
	store := NewMockStore()
	orgID := uuid.New()
	blockID := testSHA256BlockID("g3-exact-p-mismatch")
	store.AddBlock(orgID, blockID, "hot", 0)
	authority := store.SeedBlockClaimForTest(orgID, blockID, "g3-p-mismatch-claim", time.Now().UTC().Add(-time.Hour))
	store.SeedBlockHandoffForTest(orgID, blockID)

	wrongTarget := authority
	wrongTarget.Target = BlockDeleteTarget{StorageClass: authority.Target.StorageClass, StorageKey: authority.Target.StorageKey + "-different-physical-key"}

	result, err := store.FinalizeBlockDelete(orgID, blockID, committedBlockDeleteAuthority(wrongTarget))
	if err == nil || result.Outcome == BlockDeleteFinalized || result.Outcome == BlockDeleteAlreadyFinalized {
		t.Fatalf("FinalizeBlockDelete with a different P = (%+v, %v), want refusal", result, err)
	}
	block := store.GetBlock(orgID, blockID)
	if block == nil || block.GCState != "deleting" || block.GCClaimID != authority.ClaimID {
		t.Fatalf("a stale-P finalize attempt disturbed the real committed authority: %+v", block)
	}
}

// TestG3FinalizeExactDMismatchFailsClosed is the exact-D half: the same
// physical target P but a different claim identity must not authorize
// Finalize either. D1 committed(P1) must not let a stale/foreign D2 retire
// P1's canonical row.
func TestG3FinalizeExactDMismatchFailsClosed(t *testing.T) {
	store := NewMockStore()
	orgID := uuid.New()
	blockID := testSHA256BlockID("g3-exact-d-mismatch")
	store.AddBlock(orgID, blockID, "hot", 0)
	authority := store.SeedBlockClaimForTest(orgID, blockID, "g3-d1-claim", time.Now().UTC().Add(-time.Hour))
	store.SeedBlockHandoffForTest(orgID, blockID)

	foreignD := authority
	foreignD.ClaimID = "g3-d2-foreign-claim"

	result, err := store.FinalizeBlockDelete(orgID, blockID, committedBlockDeleteAuthority(foreignD))
	if err == nil || result.Outcome == BlockDeleteFinalized || result.Outcome == BlockDeleteAlreadyFinalized {
		t.Fatalf("FinalizeBlockDelete with a different D = (%+v, %v), want refusal", result, err)
	}
	block := store.GetBlock(orgID, blockID)
	if block == nil || block.GCClaimID != authority.ClaimID {
		t.Fatalf("a stale-D finalize attempt disturbed the real committed authority: %+v", block)
	}
}

// TestG3FinalizeSurvivesOrphanRecoveryAuthority pins the postcondition (G3-3):
// after successful canonical retirement, orphan(P,D) remains present, still
// COMMITTED, and still discoverable through both the exact read and the
// by-day/root recovery surfaces — none of which G3 may touch.
func TestG3FinalizeSurvivesOrphanRecoveryAuthority(t *testing.T) {
	store := NewMockStore()
	worker := testG3Worker(store)
	orgID := uuid.New()
	blockID := testSHA256BlockID("g3-finalize-survives-recovery-authority")
	store.AddBlock(orgID, blockID, "hot", 0)
	store.EnqueueBlockForTest(orgID, time.Now().UTC().Add(-time.Hour), blockID, "hot", 0)

	if _, err := worker.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("ProcessOnce returned a fatal error: %v", err)
	}
	if block := store.GetBlock(orgID, blockID); block != nil {
		t.Fatalf("canonical row was not retired: %+v", block)
	}

	orphans := store.AllS3Orphans()
	if len(orphans) != 1 {
		t.Fatalf("orphans = %+v, want exactly one surviving COMMITTED authority", orphans)
	}
	orphan := orphans[0]
	if orphan.RecoveryState != S3OrphanRecoveryStateCommitted {
		t.Fatalf("recovery state = %q, want COMMITTED", orphan.RecoveryState)
	}
	authority := orphan.Authority

	if _, found, err := store.GetS3OrphanExact(orgID, blockID, authority); err != nil || !found {
		t.Fatalf("exact orphan read after Finalize: found=%v err=%v", found, err)
	}
	if _, found, err := store.GetS3OrphanRecoveryRootExact(orgID, blockID, authority); err != nil || !found {
		t.Fatalf("recovery root after Finalize: found=%v err=%v", found, err)
	}
	if got := store.BlockDeleteLifecyclePhaseForTest(orgID, blockID, authority.ClaimID); got != BlockDeleteLifecyclePhasePublished {
		t.Fatalf("lifecycle phase after Finalize = %q, want published", got)
	}
}

// TestG3CrashBeforeFinalizeConverges is the first crash/replay leg: COMMITTED
// is durable, the process dies before Finalize ever runs, and a fresh worker
// (modeling a restart) must safely resume and finish canonical retirement.
func TestG3CrashBeforeFinalizeConverges(t *testing.T) {
	store := NewMockStore()
	orgID := uuid.New()
	blockID := testSHA256BlockID("g3-crash-before-finalize")
	store.AddBlock(orgID, blockID, "hot", 0)
	store.EnqueueBlockForTest(orgID, time.Now().UTC().Add(-time.Hour), blockID, "hot", 0)
	authority := store.SeedBlockClaimForTest(orgID, blockID, "g3-crash-before-claim", time.Now().UTC().Add(-time.Hour))
	prepared := store.PrepareBlockDeleteOrphan(orgID, blockID, authority, "sha1", time.Now().UTC())
	if prepared.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("prepare outcome = %s: %v", prepared.Outcome, prepared.Cause)
	}
	if handoff, _ := store.CommitBlockDeleteOrphanHandoff(orgID, blockID, authority); handoff.Outcome != BlockDeleteHandoffCommitted {
		t.Fatalf("commit outcome = %s", handoff.Outcome)
	}
	if promoted := store.PromoteBlockDeleteOrphan(orgID, blockID, committedBlockDeleteAuthority(authority)); promoted.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("promote outcome = %s: %v", promoted.Outcome, promoted.Cause)
	}
	// Simulate the crash: COMMITTED is durable, but Finalize never ran, and the
	// queue item was never touched.

	restarted := testG3Worker(store)
	if _, err := restarted.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("ProcessOnce after restart returned a fatal error: %v", err)
	}
	if block := store.GetBlock(orgID, blockID); block != nil {
		t.Fatalf("restart did not converge to canonical retirement: %+v", block)
	}
	if got := len(store.QueueItems(orgID)); got != 0 {
		t.Fatalf("queue items after restart = %d, want 0", got)
	}
	if orphan, found, err := store.GetS3OrphanExact(orgID, blockID, authority); err != nil || !found || orphan.RecoveryState != S3OrphanRecoveryStateCommitted {
		t.Fatalf("orphan after restart convergence: orphan=%+v found=%v err=%v", orphan, found, err)
	}
}

// TestG3CrashAfterFinalizeAppliedConverges is the second crash/replay leg:
// Finalize already applied, but the caller never observed success (a crash,
// or a lost response) and the queue item was never completed. Replay must
// converge safely without reviving P1, adopting a new life, or touching
// orphan/lifecycle authority — it relies on the same BlockExists guard that
// protects every other post-claim resume path in processBlock.
func TestG3CrashAfterFinalizeAppliedConverges(t *testing.T) {
	store := NewMockStore()
	orgID := uuid.New()
	blockID := testSHA256BlockID("g3-crash-after-finalize-applied")
	store.AddBlock(orgID, blockID, "hot", 0)
	store.EnqueueBlockForTest(orgID, time.Now().UTC().Add(-time.Hour), blockID, "hot", 0)
	authority := store.SeedBlockClaimForTest(orgID, blockID, "g3-crash-after-claim", time.Now().UTC().Add(-time.Hour))
	prepared := store.PrepareBlockDeleteOrphan(orgID, blockID, authority, "sha1", time.Now().UTC())
	if prepared.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("prepare outcome = %s: %v", prepared.Outcome, prepared.Cause)
	}
	if handoff, _ := store.CommitBlockDeleteOrphanHandoff(orgID, blockID, authority); handoff.Outcome != BlockDeleteHandoffCommitted {
		t.Fatalf("commit outcome = %s", handoff.Outcome)
	}
	if promoted := store.PromoteBlockDeleteOrphan(orgID, blockID, committedBlockDeleteAuthority(authority)); promoted.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("promote outcome = %s: %v", promoted.Outcome, promoted.Cause)
	}
	// Finalize already applied out-of-band (modeling a prior attempt whose
	// caller crashed before observing the result), but the queue item survives.
	finalize, err := store.FinalizeBlockDelete(orgID, blockID, committedBlockDeleteAuthority(authority))
	if err != nil || finalize.Outcome != BlockDeleteFinalized {
		t.Fatalf("seed finalize = %+v err=%v, want Finalized", finalize, err)
	}
	if got := len(store.QueueItems(orgID)); got != 1 {
		t.Fatalf("precondition: queue items = %d, want 1 (never completed)", got)
	}

	replay := testG3Worker(store)
	if _, err := replay.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("replay ProcessOnce returned a fatal error: %v", err)
	}
	if got := len(store.QueueItems(orgID)); got != 0 {
		t.Fatalf("queue items after replay = %d, want 0 (replay must complete the stale item)", got)
	}
	if got := len(store.AllBlockGCCandidates()); got != 0 {
		t.Fatalf("candidates after replay = %d, want 0: the exists=false branch settles the stale candidate too", got)
	}
	if orphan, found, err := store.GetS3OrphanExact(orgID, blockID, authority); err != nil || !found || orphan.RecoveryState != S3OrphanRecoveryStateCommitted {
		t.Fatalf("replay must not touch the surviving COMMITTED orphan: orphan=%+v found=%v err=%v", orphan, found, err)
	}
}

// TestG3CandidateCleanupFailureAfterFinalizeRetainsQueueThenConverges pins a
// liveness fix: a DeleteBlockGCCandidate failure that happens AFTER Finalize
// already retired blocks(L) must not let the queue item complete. Swallowing
// it (logged and forgotten) would strand the candidate/projection with no
// queue item left to carry a retry, and the scanner's rediscovery pass is not
// a guaranteed backstop for it — its discovery cursor and bounded overlap can
// already have moved past this exact candidate. The item must stay queued so
// a canonical-row-missing replay retries the same cleanup, through the
// same settleFinalizedBlockCandidate no-touch policy — never
// settleBlockCandidate's ordinary retry-then-DLQ path, which a persistent
// failure would eventually escape into (see
// TestG3CandidateCleanupPersistentFailureNeverReachesDLQ).
func TestG3CandidateCleanupFailureAfterFinalizeRetainsQueueThenConverges(t *testing.T) {
	store := NewMockStore()
	worker := testG3Worker(store)
	orgID := uuid.New()
	blockID := testSHA256BlockID("g3-candidate-cleanup-fails-after-finalize")
	store.AddBlock(orgID, blockID, "hot", 0)
	store.EnqueueBlockForTest(orgID, time.Now().UTC().Add(-time.Hour), blockID, "hot", 0)
	store.SetDeleteBlockGCCandidateDiscoveryErr(errors.New("test: candidate cleanup unavailable"))

	if _, err := worker.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("ProcessOnce returned a fatal error: %v", err)
	}

	if block := store.GetBlock(orgID, blockID); block != nil {
		t.Fatalf("canonical row must still be retired even though candidate cleanup failed: %+v", block)
	}
	if got := len(store.QueueItems(orgID)); got != 1 {
		t.Fatalf("queue items after cleanup failure = %d, want 1 retained: it is the only carrier left for the cleanup retry", got)
	}
	if got := store.QueueCompleteCallsForTest(); got != 0 {
		t.Fatalf("queue completion calls = %d, want 0: a failed candidate cleanup must not complete the item", got)
	}
	if got := len(store.AllBlockGCCandidates()); got != 1 {
		t.Fatalf("candidates after cleanup failure = %d, want 1 (cleanup did not apply)", got)
	}

	// Replay: the transient cleanup failure clears, and the canonical-row-missing
	// branch earlier in processBlock retries the same cleanup through
	// settleFinalizedBlockCandidate.
	store.SetDeleteBlockGCCandidateDiscoveryErr(nil)
	if _, err := worker.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("replay ProcessOnce returned a fatal error: %v", err)
	}
	if got := len(store.QueueItems(orgID)); got != 0 {
		t.Fatalf("queue items after replay = %d, want 0 (converged)", got)
	}
	if got := len(store.AllBlockGCCandidates()); got != 0 {
		t.Fatalf("candidates after replay = %d, want 0 (converged)", got)
	}
	orphans := store.AllS3Orphans()
	if len(orphans) != 1 || orphans[0].RecoveryState != S3OrphanRecoveryStateCommitted {
		t.Fatalf("orphan after replay = %+v, want exactly one surviving COMMITTED authority", orphans)
	}
}

// TestG3CandidateCleanupPersistentFailureNeverReachesDLQ pins the other half
// of the same liveness fix: a DeleteBlockGCCandidate failure that keeps
// failing for a reason isClusterUnavailableError does not recognise (so it is
// NOT the transient case TestG3CandidateCleanupFailureAfterFinalizeRetainsQueueThenConverges
// covers) must still never burn the queue item's five-retry budget into the
// DLQ. ItemBlock never comes back from the DLQ, and once blocks(L) is gone
// the candidate/projection row this cleanup targets has no other path back to
// being retried — so if the replay (processBlock's canonical-row-missing branch)
// settled through settleBlockCandidate's ordinary failClosedIfUnavailable
// policy instead of settleFinalizedBlockCandidate's unconditional no-touch
// one, a persistent failure here would strand that row forever, the exact
// failure mode this whole G3 liveness fix exists to close.
func TestG3CandidateCleanupPersistentFailureNeverReachesDLQ(t *testing.T) {
	store := NewMockStore()
	worker := testG3Worker(store)
	orgID := uuid.New()
	blockID := testSHA256BlockID("g3-candidate-cleanup-persistently-broken")
	store.AddBlock(orgID, blockID, "hot", 0)
	store.EnqueueBlockForTest(orgID, time.Now().UTC().Add(-time.Hour), blockID, "hot", 0)
	store.SetDeleteBlockGCCandidateDiscoveryErr(errors.New("test: candidate cleanup persistently broken (not a cluster-unavailable error)"))

	// More than the five-retry DLQ cap, still failing every time.
	for i := 0; i < 8; i++ {
		if _, err := worker.ProcessOnce(context.Background()); err != nil {
			t.Fatalf("ProcessOnce[%d] returned a fatal error: %v", i, err)
		}
	}

	if block := store.GetBlock(orgID, blockID); block != nil {
		t.Fatalf("canonical row must still be retired even though candidate cleanup keeps failing: %+v", block)
	}
	if got := len(store.QueueItems(orgID)); got != 1 {
		t.Fatalf("queue items after 8 persistent cleanup failures = %d, want 1 retained: it is the only carrier left for the cleanup retry", got)
	}
	if got := store.QueueItems(orgID)[0].RetryCount; got != 0 {
		t.Fatalf("retry_count after 8 persistent cleanup failures = %d, want 0: a no-touch failure must never spend a retry, or five of them would DLQ the item", got)
	}
	if got := len(store.FailedItems(orgID)); got != 0 {
		t.Fatalf("DLQ items after 8 persistent cleanup failures = %d, want 0: ItemBlock never comes back from the DLQ, which would strand this cleanup forever", got)
	}
	if got := store.QueueCompleteCallsForTest(); got != 0 {
		t.Fatalf("queue completion calls = %d, want 0: a failed candidate cleanup must not complete the item", got)
	}

	// The failure clears; the very next pass converges.
	store.SetDeleteBlockGCCandidateDiscoveryErr(nil)
	if _, err := worker.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("replay ProcessOnce returned a fatal error: %v", err)
	}
	if got := len(store.QueueItems(orgID)); got != 0 {
		t.Fatalf("queue items after replay = %d, want 0 (converged)", got)
	}
	if got := len(store.AllBlockGCCandidates()); got != 0 {
		t.Fatalf("candidates after replay = %d, want 0 (converged)", got)
	}
}

// TestG3CandidateCleanupFailureCodeReflectsProvenAuthority pins the
// distinction between the two candidate-cleanup no-touch failure codes:
// blockDeleteCommittedPendingError asserts exact COMMITTED(P,D), which is
// only warranted where the caller directly proved it (FinalizeBlockDelete
// just applied, inside finalizeAfterCommittedHandoff); processBlock's
// canonical-row-missing replay has no such proof — it only knows the canonical
// row is gone — so it must use the weaker blockCandidateCleanupPendingError
// instead, even though both share the identical no-touch queue policy.
// Reusing committed_pending there would assert an authority that call site
// never established.
func TestG3CandidateCleanupFailureCodeReflectsProvenAuthority(t *testing.T) {
	store := NewMockStore()
	worker := testG3Worker(store)
	orgID := uuid.New()
	blockID := testSHA256BlockID("g3-candidate-cleanup-failure-code")
	store.AddBlock(orgID, blockID, "hot", 0)
	store.EnqueueBlockForTest(orgID, time.Now().UTC().Add(-time.Hour), blockID, "hot", 0)
	store.SetDeleteBlockGCCandidateDiscoveryErr(errors.New("test: candidate cleanup unavailable"))

	// First pass: Finalize applies for the first time inside
	// finalizeAfterCommittedHandoff, which has direct proof of COMMITTED(P,D).
	item := store.QueueItems(orgID)[0]
	err := worker.processBlock(context.Background(), item)
	if got := failureCodeForError(err); got != GCFailureCodeBlockDeleteCommittedPending {
		t.Fatalf("failure code after the first (Finalize-succeeded) cleanup failure = %q, want %q (err=%v)", got, GCFailureCodeBlockDeleteCommittedPending, err)
	}
	if block := store.GetBlock(orgID, blockID); block != nil {
		t.Fatalf("canonical row must already be retired: %+v", block)
	}

	// Second pass on the SAME untouched item: the canonical row is already
	// gone, so this replay never calls Finalize at all — it lands in the
	// canonical-row-missing branch, which has no direct proof of why the row is
	// gone.
	replayItem := store.QueueItems(orgID)[0]
	err = worker.processBlock(context.Background(), replayItem)
	if got := failureCodeForError(err); got != GCFailureCodeBlockCandidateCleanupPending {
		t.Fatalf("failure code after the canonical-row-missing replay cleanup failure = %q, want %q (err=%v)", got, GCFailureCodeBlockCandidateCleanupPending, err)
	}
	if got := len(store.QueueItems(orgID)); got != 1 {
		t.Fatalf("queue items after both failures = %d, want 1 retained", got)
	}

	// The failure clears; the very next pass converges regardless of which
	// code carried it there.
	store.SetDeleteBlockGCCandidateDiscoveryErr(nil)
	if _, err := worker.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("replay ProcessOnce returned a fatal error: %v", err)
	}
	if got := len(store.QueueItems(orgID)); got != 0 {
		t.Fatalf("queue items after replay = %d, want 0 (converged)", got)
	}
}

// TestG3CandidateCleanupPartialApplyConvergesThroughStaleDiscoverySelfHeal
// composes G3 with the pre-existing R26 self-heal. DeleteBlockGCCandidate is
// two Cassandra statements — a conditional canonical delete, then an
// unconditional projection delete — so a cleanup failure can also mean the
// canonical candidate row already applied and only the projection delete
// failed: the opposite shape from every other cleanup-failure test in this
// file, where the canonical candidate is what's left standing. That shape is
// caught earlier than the canonical-row-missing branch: GetBlockGCCandidateExact at the top
// of processBlock reports candidateFound=false, and R26's stale-discovery
// self-heal (DeleteBlockGCCandidateDiscovery) retires the leftover
// projection there instead, under its own postpone-without-retry policy —
// never reaching Finalize or blockCandidateCleanupPendingError again. This
// pins that the two paths actually compose once a real G3 Finalize is what
// produced the canonical retirement, not just a test helper deleting the row
// directly (see TestR26_StaleDiscoveryNoOpRetiresItsOwnRowInsteadOfLoopingForever
// for the generic, non-G3 version of this shape).
func TestG3CandidateCleanupPartialApplyConvergesThroughStaleDiscoverySelfHeal(t *testing.T) {
	store := NewMockStore()
	orgID := uuid.New()
	blockID := testSHA256BlockID("g3-candidate-cleanup-partial-apply")
	store.AddBlock(orgID, blockID, "hot", 0)
	store.EnqueueBlockForTest(orgID, time.Now().UTC().Add(-time.Hour), blockID, "hot", 0)
	authority := store.SeedBlockClaimForTest(orgID, blockID, "g3-partial-apply-claim", time.Now().UTC().Add(-time.Hour))
	prepared := store.PrepareBlockDeleteOrphan(orgID, blockID, authority, "sha1", time.Now().UTC())
	if prepared.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("prepare outcome = %s: %v", prepared.Outcome, prepared.Cause)
	}
	if handoff, _ := store.CommitBlockDeleteOrphanHandoff(orgID, blockID, authority); handoff.Outcome != BlockDeleteHandoffCommitted {
		t.Fatalf("commit outcome = %s", handoff.Outcome)
	}
	if promoted := store.PromoteBlockDeleteOrphan(orgID, blockID, committedBlockDeleteAuthority(authority)); promoted.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("promote outcome = %s: %v", promoted.Outcome, promoted.Cause)
	}

	// A real G3 Finalize retires the canonical row.
	finalize, err := store.FinalizeBlockDelete(orgID, blockID, committedBlockDeleteAuthority(authority))
	if err != nil || finalize.Outcome != BlockDeleteFinalized {
		t.Fatalf("seed finalize = %+v err=%v, want Finalized", finalize, err)
	}

	// Simulate DeleteBlockGCCandidate's partial-apply shape by hand: the
	// canonical candidate CAS applied, but the projection delete that would
	// have followed it did not. The queue item was never completed — that
	// only happens on settleFinalizedBlockCandidate's full success.
	item := store.QueueItems(orgID)[0]
	candidateInfo, found, err := store.GetBlockGCCandidateExact(orgID, blockID, item.BlockGCCandidateIdentity)
	if err != nil || !found {
		t.Fatalf("precondition: candidate not found before simulating partial apply: found=%v err=%v", found, err)
	}
	store.DeleteBlockGCCandidateCanonicalForTest(orgID, blockID, candidateInfo.Identity())
	if _, stillFound, err := store.GetBlockGCCandidateExact(orgID, blockID, item.BlockGCCandidateIdentity); err != nil || stillFound {
		t.Fatalf("precondition: canonical candidate should already be gone: found=%v err=%v", stillFound, err)
	}
	if rows := store.BlockGCCandidateProjectionsForTest(orgID, blockID); len(rows) != 1 {
		t.Fatalf("precondition: discovery rows = %d, want the stale one still standing", len(rows))
	}

	// Replay must converge through the top-of-function candidateFound=false /
	// R26 stale-discovery path, never reaching Finalize or a canonical-row-missing replay
	// again.
	replay := testG3Worker(store)
	if _, err := replay.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("replay ProcessOnce returned a fatal error: %v", err)
	}
	if got := len(store.QueueItems(orgID)); got != 0 {
		t.Fatalf("queue items after replay = %d, want 0 (converged)", got)
	}
	if rows := store.BlockGCCandidateProjectionsForTest(orgID, blockID); len(rows) != 0 {
		t.Fatalf("discovery rows after replay = %+v, want none", rows)
	}
	if orphan, found, err := store.GetS3OrphanExact(orgID, blockID, authority); err != nil || !found || orphan.RecoveryState != S3OrphanRecoveryStateCommitted {
		t.Fatalf("replay must not touch the surviving COMMITTED orphan: orphan=%+v found=%v err=%v", orphan, found, err)
	}
}

// TestG3CandidateCleanupPersistentFailureWhileReferencedNeverReachesDLQ pins a
// second, distinct instance of the same DLQ-stranding gap
// TestG3CandidateCleanupPersistentFailureNeverReachesDLQ closes for the
// canonical-row-missing replay: this one is reachable through the OTHER branch
// that can observe the canonical row already gone, processBlock's
// BlockHasReferences path.
//
// RegisterUploadedBlockTarget writes its `up:` reference before it ever
// checks the fence (see docs/ARCHITECTURE.md), so a fresh reference for the
// same logical block_id can legitimately arrive after this exact candidate's
// canonical row was already retired by G3. On replay that makes hasRefs true,
// so the walk calls ReleaseStaleBlockClaim, which reports BlockClaimAbsent
// for an absent row exactly the same as for a present-but-unclaimed one. If
// that fell through to the ordinary settleBlockCandidate (retry-then-DLQ)
// policy, a persistent non-availability DeleteBlockGCCandidate failure would
// burn five retries into the DLQ, which ItemBlock never leaves, permanently
// stranding the candidate/projection row — indistinguishable in outcome from
// the canonical-row-missing fix already closed, just reached from the
// other caller that can observe the row gone.
func TestG3CandidateCleanupPersistentFailureWhileReferencedNeverReachesDLQ(t *testing.T) {
	store := NewMockStore()
	worker := testG3Worker(store)
	orgID := uuid.New()
	blockID := testSHA256BlockID("g3-candidate-cleanup-referenced-persistent")
	store.AddBlock(orgID, blockID, "hot", 0)
	store.EnqueueBlockForTest(orgID, time.Now().UTC().Add(-time.Hour), blockID, "hot", 0)
	store.SetDeleteBlockGCCandidateDiscoveryErr(errors.New("test: candidate cleanup persistently broken (not a cluster-unavailable error)"))

	// First pass: nothing references the block yet, so this is an ordinary G3
	// Finalize whose candidate cleanup fails.
	if _, err := worker.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("ProcessOnce returned a fatal error: %v", err)
	}
	if block := store.GetBlock(orgID, blockID); block != nil {
		t.Fatalf("canonical row must already be retired: %+v", block)
	}

	// A concurrent/subsequent upload re-references the same logical block_id.
	// Every remaining pass now takes the BlockHasReferences branch instead of
	// direct canonical-row-missing replay.
	store.AddBlockReferenceForTest(orgID, blockID, "up:concurrent-upload")
	callsBeforeReferenced := store.BlockExistsCallsForTest()
	store.SetBlockExistsErrForTest(errors.New("test: BlockExists must not run in the referenced canonical-row-missing replay"))
	defer store.SetBlockExistsErrForTest(nil)

	// More than the five-retry DLQ cap, still failing every time.
	for i := 0; i < 8; i++ {
		if _, err := worker.ProcessOnce(context.Background()); err != nil {
			t.Fatalf("ProcessOnce[%d] returned a fatal error: %v", i, err)
		}
	}

	if got := store.BlockExistsCallsForTest(); got != callsBeforeReferenced {
		t.Fatalf("BlockExists calls during referenced replay = %d, want %d: the hasRefs + BlockClaimMissing path must use its SERIAL claim observation without a second canonical-row read", got, callsBeforeReferenced)
	}

	if got := len(store.QueueItems(orgID)); got != 1 {
		t.Fatalf("queue items after 8 persistent cleanup failures while referenced = %d, want 1 retained: it is the only carrier left for the cleanup retry", got)
	}
	if got := store.QueueItems(orgID)[0].RetryCount; got != 0 {
		t.Fatalf("retry_count after 8 persistent cleanup failures while referenced = %d, want 0: a no-touch failure must never spend a retry, or five of them would DLQ the item", got)
	}
	if got := len(store.FailedItems(orgID)); got != 0 {
		t.Fatalf("DLQ items after 8 persistent cleanup failures while referenced = %d, want 0: BlockHasReferences must not bypass the no-touch cleanup policy for an absent canonical row", got)
	}
	if got := store.QueueCompleteCallsForTest(); got != 0 {
		t.Fatalf("queue completion calls = %d, want 0: a failed candidate cleanup must not complete the item", got)
	}

	// The failure clears; the very next pass converges.
	store.SetDeleteBlockGCCandidateDiscoveryErr(nil)
	if _, err := worker.ProcessOnce(context.Background()); err != nil {
		t.Fatalf("replay ProcessOnce returned a fatal error: %v", err)
	}
	if got := len(store.QueueItems(orgID)); got != 0 {
		t.Fatalf("queue items after replay = %d, want 0 (converged)", got)
	}
	if got := len(store.AllBlockGCCandidates()); got != 0 {
		t.Fatalf("candidates after replay = %d, want 0 (converged)", got)
	}
}

// TestG3RetryDoesNotAdoptADifferentCanonicalLife is the exact-P ABA
// requirement (G3-4/exact-P ABA): once P1 is retired and a fresh P2 installs
// on the same logical block, a stale replay carrying P1's authority must
// never act against P2 — not by finalizing it, not by reviving P1.
func TestG3RetryDoesNotAdoptADifferentCanonicalLife(t *testing.T) {
	store := NewMockStore()
	orgID := uuid.New()
	blockID := testSHA256BlockID("g3-retry-no-cross-life-aba")
	store.AddBlock(orgID, blockID, "hot", 0)
	d1 := store.SeedBlockClaimForTest(orgID, blockID, "g3-aba-d1", time.Now().UTC().Add(-time.Hour))
	store.SeedBlockHandoffForTest(orgID, blockID)
	finalize, err := store.FinalizeBlockDelete(orgID, blockID, committedBlockDeleteAuthority(d1))
	if err != nil || finalize.Outcome != BlockDeleteFinalized {
		t.Fatalf("seed finalize of P1 = %+v err=%v, want Finalized", finalize, err)
	}

	// P2 installs fresh on the same logical block, with an independent physical
	// target, then is independently claimed.
	store.AddBlock(orgID, blockID, "hot", 0)
	store.SetBlockStorageKeyForTest(orgID, blockID, MockCanonicalStorageKey(orgID.String(), blockID)+"-p2")
	d2 := store.SeedBlockClaimForTest(orgID, blockID, "g3-aba-d2", time.Now().UTC())
	if d2.Target == d1.Target {
		t.Fatal("test fixture did not mint an independent physical target for P2")
	}

	// A stale replay of D1's Finalize must fail closed against P2's row rather
	// than retiring it.
	stale, staleErr := store.FinalizeBlockDelete(orgID, blockID, committedBlockDeleteAuthority(d1))
	if staleErr == nil || stale.Outcome == BlockDeleteFinalized {
		t.Fatalf("stale D1 finalize against P2 = (%+v, %v), want refusal", stale, staleErr)
	}
	block := store.GetBlock(orgID, blockID)
	if block == nil {
		t.Fatal("P2 canonical row was disturbed by a stale P1 finalize replay: row is gone")
	}
	gotTarget := BlockDeleteTarget{StorageClass: block.StorageClass, StorageKey: block.StorageKey}
	if block.GCClaimID != d2.ClaimID || gotTarget != d2.Target {
		t.Fatalf("P2 canonical row was disturbed by a stale P1 finalize replay: %+v", block)
	}
}
