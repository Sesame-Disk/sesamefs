package api

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// ISSUE-SYNC-PUTBLOCK-CROSS-DC-PROVENANCE-VISIBILITY-01.
//
// These tests pin the routing behavior of syncBlockHasOwnLivenessProvenanceFn
// in isolation, at the seam level, without a real Cassandra session: a local
// LOCAL_QUORUM hit or a local read error must both settle the answer without
// ever reaching the EACH_QUORUM fallback, only a clean local "not found" may
// escalate, and the fallback's own result (found, absent, or error) is
// returned as-is. Real cross-DC evidence that a LOCAL_QUORUM miss in one
// datacenter is recovered by the EACH_QUORUM fallback lives in
// internal/integration/sync_w2_putblock_xdc_provenance_multidc_test.go
// (SESAMEFS_REQUIRE_W2_SYNC_PUTBLOCK_XDC_EVIDENCE=1, real 3-DC Cassandra).

func withW2SyncXDCSeams(t *testing.T) {
	t.Helper()
	origLocal := syncBlockReferenceExistsLocalQuorumFn
	origGlobal := syncBlockReferenceExistsEachQuorumFn
	t.Cleanup(func() {
		syncBlockReferenceExistsLocalQuorumFn = origLocal
		syncBlockReferenceExistsEachQuorumFn = origGlobal
	})
}

func TestSyncBlockHasOwnLivenessProvenance_LocalHitNeverCallsGlobal(t *testing.T) {
	withW2SyncXDCSeams(t)
	h := newHandshakeHandler()

	globalCalls := 0
	syncBlockReferenceExistsLocalQuorumFn = func(*SyncHandler, string, string, string) (bool, error) {
		return true, nil
	}
	syncBlockReferenceExistsEachQuorumFn = func(*SyncHandler, string, string, string) (bool, error) {
		globalCalls++
		return false, errors.New("global fallback must not run on a local hit")
	}

	found, err := syncBlockHasOwnLivenessProvenanceFn(h, handshakeOrgID, handshakeRepoID, "block-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("local hit must report found")
	}
	if globalCalls != 0 {
		t.Fatalf("global fallback calls = %d, want 0 on a local hit", globalCalls)
	}
}

func TestSyncBlockHasOwnLivenessProvenance_LocalErrorFailsClosedNeverCallsGlobal(t *testing.T) {
	withW2SyncXDCSeams(t)
	h := newHandshakeHandler()

	localErr := errors.New("local read unavailable")
	globalCalls := 0
	syncBlockReferenceExistsLocalQuorumFn = func(*SyncHandler, string, string, string) (bool, error) {
		return false, localErr
	}
	syncBlockReferenceExistsEachQuorumFn = func(*SyncHandler, string, string, string) (bool, error) {
		globalCalls++
		return false, nil
	}

	found, err := syncBlockHasOwnLivenessProvenanceFn(h, handshakeOrgID, handshakeRepoID, "block-1")
	if !errors.Is(err, localErr) {
		t.Fatalf("error = %v, want the local read error propagated verbatim", err)
	}
	if found {
		t.Fatal("a local error must not report found")
	}
	if globalCalls != 0 {
		t.Fatalf("global fallback calls = %d, want 0 on a local read error (fail closed, no fallback attempted)", globalCalls)
	}
}

func TestSyncBlockHasOwnLivenessProvenance_LocalMissGlobalHitRecoversProvenance(t *testing.T) {
	withW2SyncXDCSeams(t)
	h := newHandshakeHandler()

	var gotOrgID, gotBlockID, gotReferrer string
	syncBlockReferenceExistsLocalQuorumFn = func(*SyncHandler, string, string, string) (bool, error) {
		return false, nil
	}
	syncBlockReferenceExistsEachQuorumFn = func(_ *SyncHandler, orgID, blockID, referrer string) (bool, error) {
		gotOrgID, gotBlockID, gotReferrer = orgID, blockID, referrer
		return true, nil
	}

	found, err := syncBlockHasOwnLivenessProvenanceFn(h, handshakeOrgID, handshakeRepoID, "block-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("a global hit after a local miss must report found, recovering cross-DC provenance")
	}
	if gotOrgID != handshakeOrgID || gotBlockID != "block-1" {
		t.Fatalf("global fallback called with org=%s block=%s, want org=%s block=block-1", gotOrgID, gotBlockID, handshakeOrgID)
	}
	wantReferrer := syncBlockUploadReferrer(handshakeRepoID, "block-1")
	if gotReferrer != wantReferrer {
		t.Fatalf("global fallback referrer = %s, want the exact same up:sync:<repo>:<block> referrer %s the local check used, not any/foreign reference", gotReferrer, wantReferrer)
	}
}

func TestSyncBlockHasOwnLivenessProvenance_LocalMissGlobalMissStaysUnprovenanced(t *testing.T) {
	withW2SyncXDCSeams(t)
	h := newHandshakeHandler()

	syncBlockReferenceExistsLocalQuorumFn = func(*SyncHandler, string, string, string) (bool, error) {
		return false, nil
	}
	syncBlockReferenceExistsEachQuorumFn = func(*SyncHandler, string, string, string) (bool, error) {
		return false, nil
	}

	found, err := syncBlockHasOwnLivenessProvenanceFn(h, handshakeOrgID, handshakeRepoID, "block-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("a genuine global miss must not fabricate provenance; the block must stay unprovenanced")
	}
}

func TestSyncBlockHasOwnLivenessProvenance_LocalMissGlobalErrorFailsClosed(t *testing.T) {
	withW2SyncXDCSeams(t)
	h := newHandshakeHandler()

	globalErr := errors.New("datacenter unreachable")
	syncBlockReferenceExistsLocalQuorumFn = func(*SyncHandler, string, string, string) (bool, error) {
		return false, nil
	}
	syncBlockReferenceExistsEachQuorumFn = func(*SyncHandler, string, string, string) (bool, error) {
		return false, globalErr
	}

	found, err := syncBlockHasOwnLivenessProvenanceFn(h, handshakeOrgID, handshakeRepoID, "block-1")
	if !errors.Is(err, globalErr) {
		t.Fatalf("error = %v, want the global fallback error propagated verbatim so the caller fails closed", err)
	}
	if found {
		t.Fatal("a global error must not report found")
	}
}

// TestSyncCommitProvenancedBlockIDs_GlobalFailureStopsAdditionalDBProbes
// proves the fan-out is bounded, not O(N): when every block is a clean local
// miss and the EACH_QUORUM fallback fails for all of them (a degraded/down
// datacenter), the number of fallback calls actually attempted must stay at
// syncCommitBlockPlacementConcurrency, not grow toward the total block
// count. Without cancellation, a commit with hundreds of dedup-only blocks
// during a datacenter outage would attempt a slow, failing global lookup
// for every single one before finally failing the whole readiness call;
// with it, only the wave already in flight when the first failure lands
// makes its own external call -- later blocks' goroutines still get
// created (admission is not itself gated), they just observe the
// cancelled context and return before calling the fallback, so "no further
// DB probes" is the precise property, not "no further goroutines."
func TestSyncCommitProvenancedBlockIDs_GlobalFailureStopsAdditionalDBProbes(t *testing.T) {
	withW2SyncXDCSeams(t)
	h := newHandshakeHandler()

	const totalBlocks = 200
	blockIDs := make([]string, totalBlocks)
	for i := range blockIDs {
		blockIDs[i] = fmt.Sprintf("block-%d", i)
	}
	canonicalByFile := map[string][]string{"fs-1": blockIDs}

	var globalCalls int64
	syncBlockReferenceExistsLocalQuorumFn = func(*SyncHandler, string, string, string) (bool, error) {
		return false, nil // every block is a clean local miss
	}
	syncBlockReferenceExistsEachQuorumFn = func(*SyncHandler, string, string, string) (bool, error) {
		atomic.AddInt64(&globalCalls, 1)
		time.Sleep(30 * time.Millisecond) // simulate a slow/degraded datacenter
		return false, errors.New("datacenter unreachable")
	}

	if _, err := h.syncCommitProvenancedBlockIDs(handshakeOrgID, handshakeRepoID, canonicalByFile); err == nil {
		t.Fatal("expected an error from the failing global fallback")
	}

	got := atomic.LoadInt64(&globalCalls)
	// Exactly syncCommitBlockPlacementConcurrency, not a multiple of it: every
	// goroutine in the first (and only) admitted wave calls the fallback and
	// starts sleeping before any of them can return an error and cancel ctx,
	// and every later goroutine checks ctx.Done() before ever calling the
	// fallback, so it can add zero calls, never one. Confirmed exactly 20/20
	// across 30 consecutive runs; a docs/PR claim of "roughly one wave" gets a
	// test that actually pins one wave, not a multiple of it.
	const bound = int64(syncCommitBlockPlacementConcurrency)
	if got != bound {
		t.Fatalf("global fallback calls = %d out of %d blocks, want exactly the concurrency limit (%d), not more or fewer", got, totalBlocks, syncCommitBlockPlacementConcurrency)
	}
	t.Logf("global fallback calls = %d out of %d blocks (concurrency=%d) -- bounded fail-fast confirmed", got, totalBlocks, syncCommitBlockPlacementConcurrency)
}
