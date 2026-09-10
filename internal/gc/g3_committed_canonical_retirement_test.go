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
//	orphan(P1,D1) == COMMITTED  (confirmed exactly, in the same SERIAL domain
//	                             as `blocks`, by PromoteBlockDeleteOrphan)
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
	gotTarget := BlockDeleteTarget{StorageClass: block.StorageClass, StorageKey: block.StorageKey}
	if block == nil || block.GCClaimID != d2.ClaimID || gotTarget != d2.Target {
		t.Fatalf("P2 canonical row was disturbed by a stale P1 finalize replay: %+v", block)
	}
}
