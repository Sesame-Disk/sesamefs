package gc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestP4B_CommitBlockDeleteOrphanHandoffSourceContract(t *testing.T) {
	file := parseGCStoreFile(t)
	text := formattedGCFunction(t, file, "CommitBlockDeleteOrphanHandoff")

	if !strings.Contains(text, "UPDATE blocks SET gc_orphan_handoff = true") {
		t.Fatal("CommitBlockDeleteOrphanHandoff must set gc_orphan_handoff = true")
	}
	if strings.Contains(text, "gc_orphan_handoff = false") {
		t.Fatal("CommitBlockDeleteOrphanHandoff must never write gc_orphan_handoff = false")
	}
	for _, column := range []string{
		"storage_class = ?",
		"storage_key = ?",
		"gc_state = ?",
		"gc_claim_id = ?",
		"gc_claimed_at = ?",
		"gc_orphan_handoff = null",
	} {
		if !strings.Contains(text, column) {
			t.Fatalf("CommitBlockDeleteOrphanHandoff IF must name %s", column)
		}
	}
	if !strings.Contains(text, "Consistency(gocql.EachQuorum)") {
		t.Fatal("CommitBlockDeleteOrphanHandoff must pin regular consistency to EachQuorum")
	}
	if !strings.Contains(text, "SerialConsistency(gocql.Serial)") {
		t.Fatal("CommitBlockDeleteOrphanHandoff must pin the LWT serial domain")
	}
	if !strings.Contains(text, "Idempotent(false)") || !strings.Contains(text, "NumRetries: 0") || !strings.Contains(text, "NonSpeculativeExecution") {
		t.Fatal("CommitBlockDeleteOrphanHandoff must not hide an uncertain LWT behind driver retries")
	}
	if !strings.Contains(text, "len(existing) == 0") || !strings.Contains(text, "settleBlockDeleteHandoff") {
		t.Fatal("empty non-applied handoff CAS must SERIAL-settle, never classify as Invalid from the empty map")
	}
	if !strings.Contains(text, "maybeConfirmAlreadyCommittedHandoffEachQuorum") {
		t.Fatal("CAS-map AlreadyCommitted must confirm EACH_QUORUM visibility before success")
	}
}

func TestP4B_ReleaseAndClaimRefuseCommittedHandoff(t *testing.T) {
	store := NewMockStore()
	orgID := uuid.New()
	blockID := "blk-handoff-release"
	store.AddBlock(orgID, blockID, "hot", 0)
	owner := store.SeedBlockClaimForTest(orgID, blockID, "d1", time.Now().UTC().Add(-time.Hour))
	seedPreparedBlockDeleteOrphanForTest(t, store, orgID, blockID, owner)
	store.SeedBlockHandoffForTest(orgID, blockID)

	if outcome, err := store.ReleaseBlockClaim(orgID, blockID, owner); err != nil || outcome != BlockReleaseNotOwner {
		t.Fatalf("ReleaseBlockClaim after handoff = %s, %v; want not_owner", outcome, err)
	}
	if blk := store.GetBlock(orgID, blockID); blk == nil || blk.GCClaimID != "d1" || !orphanHandoffCommitted(blk.GCOrphanHandoff) {
		t.Fatalf("release dropped a committed authority: %+v", blk)
	}

	stale, err := store.ReleaseStaleBlockClaim(orgID, blockID, owner.Target, time.Now().UTC())
	if err != nil || stale != BlockClaimCommittedHandoff {
		t.Fatalf("ReleaseStaleBlockClaim after handoff = %s, %v; want committed_handoff", stale, err)
	}

	fresh := store.BlockDeleteAuthorityForTest(orgID, blockID, "d2", time.Now().UTC())
	claim, err := store.ClaimBlockDelete(orgID, blockID, fresh)
	if err != nil || claim.Outcome != BlockClaimCommittedOwner {
		t.Fatalf("ClaimBlockDelete after handoff = %s, %v; want committed_owner", claim.Outcome, err)
	}
	if claim.Owner.ClaimID != "d1" {
		t.Fatalf("CommittedOwner resumed %q, want stored d1", claim.Owner.ClaimID)
	}
}

func TestP4B_FinalizeRequiresCommittedHandoff(t *testing.T) {
	store := NewMockStore()
	orgID := uuid.New()
	blockID := "blk-finalize-handoff"
	store.AddBlock(orgID, blockID, "hot", 0)
	owner := store.SeedBlockClaimForTest(orgID, blockID, "d1", time.Now().UTC())
	seedPreparedBlockDeleteOrphanForTest(t, store, orgID, blockID, owner)

	result, err := store.FinalizeBlockDelete(orgID, blockID, committedBlockDeleteAuthority(owner))
	if err == nil || result.Outcome != BlockDeleteNotAuthority {
		t.Fatalf("finalize without handoff = %+v, %v; want not_authority", result, err)
	}
	if store.GetBlock(orgID, blockID) == nil {
		t.Fatal("finalize without handoff deleted the canonical row")
	}

	store.SeedBlockHandoffForTest(orgID, blockID)
	result, err = store.FinalizeBlockDelete(orgID, blockID, committedBlockDeleteAuthority(owner))
	if err != nil || !result.ok() {
		t.Fatalf("finalize with handoff = %+v, %v; want applied", result, err)
	}
	if store.GetBlock(orgID, blockID) != nil {
		t.Fatal("finalize with handoff left the canonical row")
	}
}

func TestG4CommittedHandoffIsNotRevokedByLateRefs(t *testing.T) {
	store := NewMockStore()
	sp := &MockStorageProvider{}
	w := NewWorker(store, sp, NewQueue(store), 100, 0, false, &Stats{})
	org := uuid.New()
	block := testSHA256BlockID("g4-late-ref-retirement")
	store.AddBlock(org, block, "hot", 0)
	old := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Millisecond)
	candidate := ensureAndEnqueueBlockForTest(t, store, org, block, "hot", old, 0)
	stored := store.SeedBlockClaimForTest(org, block, "stored-d1", old)
	seedPreparedBlockDeleteOrphanForTest(t, store, org, block, stored)
	store.SeedBlockHandoffForTest(org, block)
	store.AddBlockReferenceForTest(org, block, "late-ref")
	if n, err := w.ProcessOnce(context.Background()); err != nil || n != 1 {
		t.Fatalf("committed retirement: %d %v", n, err)
	}
	if store.GetBlock(org, block) != nil {
		t.Fatal("late ref vetoed canonical retirement")
	}
	orphan, found, err := store.GetS3OrphanExact(org, block, stored)
	if err != nil || !found || orphan.RecoveryState != S3OrphanRecoveryStateCommitted || !orphan.Authority.sameAuthority(stored) {
		t.Fatalf("D1 revoked/replaced: %+v %v", orphan, err)
	}
	if len(sp.DeletedBlocks()) != 0 {
		t.Fatal("handoff worker performed physical deletion")
	}
	if _, found, err := store.GetBlockGCCandidateExact(org, block, candidate.Identity()); err != nil || found {
		t.Fatalf("candidate not settled: %v %v", found, err)
	}
	if has, err := store.BlockHasReferencesGlobal(org, block); err != nil || !has {
		t.Fatal("retirement changed logical refs")
	}
}

func TestProcessBlockCommittedOwnerRevalidationFailureLeavesQueueUntouched(t *testing.T) {
	store := NewMockStore()
	sp := &MockStorageProvider{}
	w := NewWorker(store, sp, NewQueue(store), 100, 0, false, &Stats{})
	orgID := uuid.New()
	blockID := testSHA256BlockID("p4b-h9-revalidate")
	store.AddBlock(orgID, blockID, "hot", 0)
	candidateAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Millisecond)
	candidate := ensureAndEnqueueBlockForTest(t, store, orgID, blockID, "hot", candidateAt, 0)
	original := store.QueueItems(orgID)[0]
	stored := store.SeedBlockClaimForTest(orgID, blockID, "stored-d1", candidateAt)
	seedPreparedBlockDeleteOrphanForTest(t, store, orgID, blockID, stored)
	store.SeedBlockHandoffForTest(orgID, blockID)

	var topologyCalls int
	w.SetDestructiveTopologyGate(func() error {
		topologyCalls++
		if topologyCalls == 1 {
			return nil
		}
		return errors.New("live replication map no longer matches the declared topology")
	})

	n, err := w.ProcessOnce(context.Background())
	if err != nil || n != 0 {
		t.Fatalf("ProcessOnce() = (%d, %v), want committed-pending refusal", n, err)
	}
	block := store.GetBlock(orgID, blockID)
	if block == nil || block.GCState != "deleting" || block.GCClaimID != "stored-d1" || !orphanHandoffCommitted(block.GCOrphanHandoff) {
		t.Fatalf("revalidation failure released a committed authority: %+v", block)
	}
	if store.QueueCompleteCallsForTest() != 0 || store.QueueRequeueCallsForTest() != 0 || store.QueueFailCallsForTest() != 0 {
		t.Fatalf("queue lifecycle calls = complete:%d requeue:%d fail:%d, want all zero", store.QueueCompleteCallsForTest(), store.QueueRequeueCallsForTest(), store.QueueFailCallsForTest())
	}
	items := store.QueueItems(orgID)
	if len(items) != 1 || items[0].RetryCount != original.RetryCount || !items[0].QueuedAt.Equal(original.QueuedAt) {
		t.Fatalf("queue after H9 refusal = %+v, want original %+v", items, original)
	}
	if _, ok, err := store.GetBlockGCCandidateExact(orgID, blockID, candidate.Identity()); err != nil || !ok {
		t.Fatalf("candidate was consumed after H9 refusal: ok=%v err=%v", ok, err)
	}
	if topologyCalls < 2 {
		t.Fatalf("CommittedOwner skipped topology revalidation (calls=%d)", topologyCalls)
	}
}

func TestProcessBlockCommittedOwnerDoesNotMintANewClaim(t *testing.T) {
	store := NewMockStore()
	sp := &MockStorageProvider{}
	w := NewWorker(store, sp, NewQueue(store), 100, 0, false, &Stats{})
	orgID := uuid.New()
	blockID := testSHA256BlockID("p4b-resume-d1")
	store.AddBlock(orgID, blockID, "hot", 0)
	candidateAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Millisecond)
	ensureAndEnqueueBlockForTest(t, store, orgID, blockID, "hot", candidateAt, 0)
	stored := store.SeedBlockClaimForTest(orgID, blockID, "stored-d1", candidateAt)
	seedPreparedBlockDeleteOrphanForTest(t, store, orgID, blockID, stored)
	store.SeedBlockHandoffForTest(orgID, blockID)

	n, err := w.ProcessOnce(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("ProcessOnce() = (%d, %v), want the resumed committed authority to reach G3 canonical retirement", n, err)
	}
	if block := store.GetBlock(orgID, blockID); block != nil {
		t.Fatalf("resume did not retire the canonical row after G3: %+v", block)
	}
	if got := sp.DeletedBlocks(); len(got) != 0 {
		t.Fatalf("physical deletes = %v, want none: G3 does not perform physical deletion", got)
	}
	if store.BlockDeleteLifecyclePhaseForTest(orgID, blockID, "stored-d1") != BlockDeleteLifecyclePhasePublished {
		t.Fatal("G3 canonical retirement must leave the lifecycle tombstone published")
	}
}
