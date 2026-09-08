package api

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	v2 "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	"github.com/Sesame-Disk/sesamefs/internal/db"
)

// W2 Sync PutBlock -> HEAD publication continuity.
//
// These tests exercise the new pre-HEAD readiness gate and durable repair-row
// bookkeeping added to sync.go in isolation, using the same seam pattern as
// sync_publish_handshake_test.go, without a real Cassandra session. Coverage
// of the full production control flow through handleSyncHeadPromotion and
// tryAutoMergeSyncHeadPromotion against a real CAS belongs to the integration
// leg (docs/TESTING.md, SESAMEFS_REQUIRE_W2_SYNC_PUTBLOCK_HEAD_EVIDENCE=1).

func withW2SyncSeams(t *testing.T) {
	t.Helper()
	origHasProvenance := syncBlockHasOwnLivenessProvenanceFn
	origProbe := syncProbeBlockReuseForPlacementFn
	origAuthority := syncValidateBorrowedFSPublicationAuthorityFn
	origResolvePositional := resolveSyncBlockIDsPositionalFn
	origQueue := queueSyncCommitBlockReferenceRepairsFn
	origClear := clearSyncCommitBlockReferenceRepairsFn
	origBarrier := syncAfterHeadCASBeforeBlockFinalizeFn
	t.Cleanup(func() {
		syncBlockHasOwnLivenessProvenanceFn = origHasProvenance
		syncProbeBlockReuseForPlacementFn = origProbe
		syncValidateBorrowedFSPublicationAuthorityFn = origAuthority
		resolveSyncBlockIDsPositionalFn = origResolvePositional
		queueSyncCommitBlockReferenceRepairsFn = origQueue
		clearSyncCommitBlockReferenceRepairsFn = origClear
		syncAfterHeadCASBeforeBlockFinalizeFn = origBarrier
	})
	resolveSyncBlockIDsPositionalFn = func(_ *SyncHandler, _, _ string, blockIDs []string) ([]string, error) {
		return append([]string(nil), blockIDs...), nil
	}
}

// --- resolveSyncCommitAddedFilesCanonical: per-file canonical association ---

func TestResolveSyncCommitAddedFilesCanonical_PerFileAssociationNoCrossFileLeakage(t *testing.T) {
	withW2SyncSeams(t)
	h := newHandshakeHandler()

	addedFiles := []syncCommitFileReference{
		{fsID: "fs-1", blockIDs: []string{"a", "b"}},
		{fsID: "fs-2", blockIDs: []string{"c"}},
	}
	result, err := h.resolveSyncCommitAddedFilesCanonical(handshakeOrgID, handshakeRepoID, addedFiles)
	if err != nil {
		t.Fatalf("resolveSyncCommitAddedFilesCanonical returned error: %v", err)
	}
	if got := result["fs-1"]; len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("fs-1 canonical = %v, want [a b]", got)
	}
	if got := result["fs-2"]; len(got) != 1 || got[0] != "c" {
		t.Fatalf("fs-2 canonical = %v, want [c]", got)
	}
	// fs-2 must never see fs-1's blocks and vice versa: this is the P1 audit
	// finding — a flattened cross-file aggregate would silently corrupt this.
	for _, blockID := range result["fs-1"] {
		if blockID == "c" {
			t.Fatalf("fs-1 leaked fs-2's block %q", blockID)
		}
	}
}

func TestResolveSyncCommitAddedFilesCanonical_SharedBlockAcrossFilesResolvedOnce(t *testing.T) {
	withW2SyncSeams(t)
	calls := 0
	resolveSyncBlockIDsPositionalFn = func(_ *SyncHandler, _, _ string, blockIDs []string) ([]string, error) {
		calls++
		return append([]string(nil), blockIDs...), nil
	}
	h := newHandshakeHandler()

	addedFiles := []syncCommitFileReference{
		{fsID: "fs-1", blockIDs: []string{"shared", "a"}},
		{fsID: "fs-2", blockIDs: []string{"shared", "b"}},
	}
	result, err := h.resolveSyncCommitAddedFilesCanonical(handshakeOrgID, handshakeRepoID, addedFiles)
	if err != nil {
		t.Fatalf("resolveSyncCommitAddedFilesCanonical returned error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("positional resolver called %d times, want 1 batch call (no N-per-file resolutions)", calls)
	}
	if got := result["fs-1"]; len(got) != 2 {
		t.Fatalf("fs-1 canonical = %v, want 2 entries", got)
	}
	if got := result["fs-2"]; len(got) != 2 {
		t.Fatalf("fs-2 canonical = %v, want 2 entries", got)
	}
}

func TestResolveSyncCommitAddedFilesCanonical_CollisionPreservesBatchMapping(t *testing.T) {
	withW2SyncSeams(t)
	calls := 0
	resolveSyncBlockIDsPositionalFn = func(_ *SyncHandler, _, _ string, blockIDs []string) ([]string, error) {
		calls++
		out := make([]string, len(blockIDs))
		for i := range out {
			out[i] = "canonical-x"
		}
		return out, nil
	}
	h := newHandshakeHandler()

	addedFiles := []syncCommitFileReference{
		{fsID: "fs-1", blockIDs: []string{"legacy-sha1"}},
		{fsID: "fs-2", blockIDs: []string{"already-canonical"}},
	}
	result, err := h.resolveSyncCommitAddedFilesCanonical(handshakeOrgID, handshakeRepoID, addedFiles)
	if err != nil {
		t.Fatalf("resolveSyncCommitAddedFilesCanonical returned error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("positional resolver called %d times, want 1 batch call", calls)
	}
	if got := result["fs-1"]; len(got) != 1 || got[0] != "canonical-x" {
		t.Fatalf("fs-1 canonical = %v, want [canonical-x]", got)
	}
	if got := result["fs-2"]; len(got) != 1 || got[0] != "canonical-x" {
		t.Fatalf("fs-2 canonical = %v, want [canonical-x]", got)
	}
}

// --- syncCommitProvenancedBlockIDs / ensureSyncCommitBlockPublicationReadiness: scope gate ---
func TestStageSyncCommitBlockDeltaReusesOnePositionalResolution(t *testing.T) {
	rec := installHandshakeSeams(t)
	calls := 0
	resolveSyncBlockIDsPositionalFn = func(_ *SyncHandler, _, _ string, blockIDs []string) ([]string, error) {
		calls++
		return []string{handshakeBlockTwo, handshakeBlockOne}, nil
	}

	staged, err := newHandshakeHandler().stageSyncCommitBlockDelta(handshakeOrgID, handshakeRepoID, handshakeHeadID)
	if err != nil {
		t.Fatalf("stageSyncCommitBlockDelta returned error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("positional canonical resolver called %d times, want exactly once", calls)
	}
	if got := staged.canonicalAddedBlockIDsByFile["fs-r25"]; len(got) != 2 || got[0] != handshakeBlockTwo || got[1] != handshakeBlockOne {
		t.Fatalf("canonicalAddedBlockIDsByFile = %v, want the single positional result", got)
	}
	if got := staged.resolvedAddedBlockIDs; len(got) != 2 || got[0] != handshakeBlockTwo || got[1] != handshakeBlockOne {
		t.Fatalf("resolvedAddedBlockIDs = %v, want the same canonical result used for pub staging", got)
	}
	if got := rec.staged[staged.publishAttemptID]; len(got) != 2 || got[0] != handshakeBlockTwo || got[1] != handshakeBlockOne {
		t.Fatalf("staged pub IDs = %v, want the same canonical result", got)
	}
}

func TestSyncCommitProvenancedBlockIDs_OnlyBlocksWithExistingUpReferencePass(t *testing.T) {
	withW2SyncSeams(t)
	provenanced := map[string]bool{"has-putblock": true}
	syncBlockHasOwnLivenessProvenanceFn = func(_ *SyncHandler, _, _, blockID string) (bool, error) {
		return provenanced[blockID], nil
	}
	h := newHandshakeHandler()

	got, err := h.syncCommitProvenancedBlockIDs(handshakeOrgID, handshakeRepoID, map[string][]string{
		"fs-1": {"has-putblock", "dedup-only-no-putblock"},
	})
	if err != nil {
		t.Fatalf("syncCommitProvenancedBlockIDs returned error: %v", err)
	}
	if len(got) != 1 || got[0] != "has-putblock" {
		t.Fatalf("provenanced = %v, want [has-putblock] — a block with no up: reference must never be included (docs/R3-LIVENESS-CONTINUITY.md: \"Sync commit whose block had no associated PutBlock\" stays untouched)", got)
	}
}

func TestEnsureSyncCommitBlockPublicationReadiness_NoProvenanceIsANoOp(t *testing.T) {
	withW2SyncSeams(t)
	syncBlockHasOwnLivenessProvenanceFn = func(*SyncHandler, string, string, string) (bool, error) { return false, nil }
	probeCalls := 0
	syncProbeBlockReuseForPlacementFn = func(*SyncHandler, string, string) (db.BlockReuseProbe, error) {
		probeCalls++
		return db.BlockReuseProbe{}, errors.New("must not be called")
	}
	h := newHandshakeHandler()

	if err := h.ensureSyncCommitBlockPublicationReadiness(handshakeOrgID, handshakeRepoID, map[string][]string{"fs-1": {"dedup-only"}}); err != nil {
		t.Fatalf("ensureSyncCommitBlockPublicationReadiness returned error: %v", err)
	}
	if probeCalls != 0 {
		t.Fatalf("placement was probed %d time(s) for a block with no PutBlock provenance; scope must never widen to unprovenanced blocks", probeCalls)
	}
}

// --- ordering: own liveness before placement validation ---

func TestEnsureSyncCommitBlockPublicationReadiness_LivenessRenewedBeforeFenceValidated(t *testing.T) {
	withW2SyncSeams(t)
	syncBlockHasOwnLivenessProvenanceFn = func(*SyncHandler, string, string, string) (bool, error) { return true, nil }
	syncProbeBlockReuseForPlacementFn = func(_ *SyncHandler, _, blockID string) (db.BlockReuseProbe, error) {
		return db.BlockReuseProbe{Decision: db.BlockReuseReusable, StorageClass: "hot", StorageKey: "key-" + blockID}, nil
	}
	var order []string
	origAddRef := syncAddProvisionalBlockReferenceFn
	t.Cleanup(func() { syncAddProvisionalBlockReferenceFn = origAddRef })
	syncAddProvisionalBlockReferenceFn = func(_ *db.DB, orgID, blockID, referrer, libraryID, storageClass string, expiresAt time.Time) error {
		order = append(order, "renew:"+blockID)
		return nil
	}
	syncValidateBorrowedFSPublicationAuthorityFn = func(_ *db.DB, _, blockID string, _ db.BlockPhysicalLocation) (db.BlockRepairAuthorityOutcome, error) {
		order = append(order, "validate:"+blockID)
		return db.BlockRepairAuthorityAuthorized, nil
	}
	h := newHandshakeHandler()

	if err := h.ensureSyncCommitBlockPublicationReadiness(handshakeOrgID, handshakeRepoID, map[string][]string{"fs-1": {"b1"}}); err != nil {
		t.Fatalf("ensureSyncCommitBlockPublicationReadiness returned error: %v", err)
	}
	if len(order) != 2 || order[0] != "renew:b1" || order[1] != "validate:b1" {
		t.Fatalf("order = %v, want [renew:b1 validate:b1] — own liveness must be renewed before the exact-placement fence is validated", order)
	}
}

func TestAutoMergeSyncPublicationReadinessPrecedesRepairQueue(t *testing.T) {
	withW2SyncSeams(t)
	queueCalls := 0
	queueSyncCommitBlockReferenceRepairsFn = func(*db.DB, string, string, string, map[string][]string) error {
		queueCalls++
		return nil
	}
	syncBlockHasOwnLivenessProvenanceFn = func(*SyncHandler, string, string, string) (bool, error) {
		return true, nil
	}
	syncProbeBlockReuseForPlacementFn = func(*SyncHandler, string, string) (db.BlockReuseProbe, error) {
		return db.BlockReuseProbe{Decision: db.BlockReuseBlockedByGC}, nil
	}

	err := newHandshakeHandler().ensureAndQueueAutoMergeSyncPublication(
		handshakeOrgID,
		handshakeRepoID,
		handshakeHeadID,
		map[string][]string{"fs-1": {"blocked-block"}},
	)
	if !errors.Is(err, v2.ErrBlockDeleteInProgress) {
		t.Fatalf("auto-merge readiness error = %v, want %v", err, v2.ErrBlockDeleteInProgress)
	}
	if queueCalls != 0 {
		t.Fatalf("queue was called %d time(s) after readiness failed; auto-merge must not create durable repair intent before readiness succeeds", queueCalls)
	}
}

// --- failure behavior: placement / fence rejection blocks readiness ---

func TestResolveSyncCommitBlockPlacements_NonReusableFailsClosed(t *testing.T) {
	withW2SyncSeams(t)
	syncProbeBlockReuseForPlacementFn = func(*SyncHandler, string, string) (db.BlockReuseProbe, error) {
		return db.BlockReuseProbe{Decision: db.BlockReuseBlockedByGC}, nil
	}
	h := newHandshakeHandler()

	_, err := h.resolveSyncCommitBlockPlacements(handshakeOrgID, []string{"fenced-block"})
	if !errors.Is(err, v2.ErrBlockDeleteInProgress) {
		t.Fatalf("resolveSyncCommitBlockPlacements error = %v, want wrapping v2.ErrBlockDeleteInProgress", err)
	}
}

func TestValidateSyncCommitBlockPublicationFences_RejectsAnyNonAuthorizedOutcome(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outcome db.BlockRepairAuthorityOutcome
	}{
		{"blocked", db.BlockRepairAuthorityBlocked},
		{"changed", db.BlockRepairAuthorityChanged},
		{"permanent", db.BlockRepairAuthorityPermanent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withW2SyncSeams(t)
			syncValidateBorrowedFSPublicationAuthorityFn = func(*db.DB, string, string, db.BlockPhysicalLocation) (db.BlockRepairAuthorityOutcome, error) {
				return tc.outcome, errors.New("fence detail")
			}
			h := newHandshakeHandler()

			placements := []syncCommitBlockPlacement{{blockID: "b1", storageClass: "hot", storageKey: "k1"}}
			if err := h.validateSyncCommitBlockPublicationFences(handshakeOrgID, placements); err == nil {
				t.Fatalf("validateSyncCommitBlockPublicationFences outcome=%v: want error, got nil", tc.outcome)
			}
		})
	}
}

func TestValidateSyncCommitBlockPublicationFences_AuthorizedIsNoError(t *testing.T) {
	withW2SyncSeams(t)
	syncValidateBorrowedFSPublicationAuthorityFn = func(*db.DB, string, string, db.BlockPhysicalLocation) (db.BlockRepairAuthorityOutcome, error) {
		return db.BlockRepairAuthorityAuthorized, nil
	}
	h := newHandshakeHandler()
	placements := []syncCommitBlockPlacement{{blockID: "b1", storageClass: "hot", storageKey: "k1"}}
	if err := h.validateSyncCommitBlockPublicationFences(handshakeOrgID, placements); err != nil {
		t.Fatalf("validateSyncCommitBlockPublicationFences: %v", err)
	}
}

// --- durable repair-row queueing: per-file identity, shared-row retention on ambiguous failure ---

func TestQueueSyncCommitBlockReferenceRepairs_PartialFailureRetainsSharedRows(t *testing.T) {
	origQueue := publishRepairQueueFn
	origClear := publishRepairClearFn
	t.Cleanup(func() {
		publishRepairQueueFn = origQueue
		publishRepairClearFn = origClear
	})
	var cleared []string
	wantErr := errors.New("queue boom on fs-2")
	publishRepairQueueFn = func(_ *db.DB, _, _, _, fsID string, _ []string) error {
		if fsID == "fs-2" {
			return wantErr
		}
		return nil
	}
	publishRepairClearFn = func(_ *db.DB, _, _, _, fsID string) error {
		cleared = append(cleared, fsID)
		return nil
	}

	canonicalByFile := map[string][]string{
		"fs-1": {"a"},
		"fs-2": {"b"},
	}
	err := queueSyncCommitBlockReferenceRepairsFn(&db.DB{}, handshakeOrgID, handshakeRepoID, handshakeHeadID, canonicalByFile)
	if !errors.Is(err, wantErr) {
		t.Fatalf("queueSyncCommitBlockReferenceRepairsFn error = %v, want %v", err, wantErr)
	}
	if len(cleared) != 0 {
		t.Fatalf("cleared = %v, want no shared repair rows removed after ambiguous queue failure", cleared)
	}
}

// --- settlement: finalize wrapper clears on success, schedules on failure ---
func syncW2ManyCanonicalFiles(count int) map[string][]string {
	canonicalByFile := make(map[string][]string, count)
	for i := 0; i < count; i++ {
		canonicalByFile[fmt.Sprintf("fs-%02d", i)] = []string{fmt.Sprintf("%064x", i+1)}
	}
	return canonicalByFile
}

func TestQueueSyncCommitBlockReferenceRepairsUsesBoundedConcurrency(t *testing.T) {
	origQueue := publishRepairQueueFn
	t.Cleanup(func() { publishRepairQueueFn = origQueue })
	canonicalByFile := syncW2ManyCanonicalFiles(40)
	assertSyncW2BoundedConcurrency(t, func(probe *syncW2ConcurrencyProbe) error {
		publishRepairQueueFn = func(_ *db.DB, _, _, _, _ string, _ []string) error {
			probe.enter()
			return nil
		}
		return queueSyncCommitBlockReferenceRepairsFn(&db.DB{}, handshakeOrgID, handshakeRepoID, handshakeHeadID, canonicalByFile)
	})
}

func TestClearSyncCommitBlockReferenceRepairsUsesBoundedConcurrency(t *testing.T) {
	origClear := publishRepairClearFn
	t.Cleanup(func() { publishRepairClearFn = origClear })
	canonicalByFile := syncW2ManyCanonicalFiles(40)
	assertSyncW2BoundedConcurrency(t, func(probe *syncW2ConcurrencyProbe) error {
		publishRepairClearFn = func(_ *db.DB, _, _, _, _ string) error {
			probe.enter()
			return nil
		}
		return clearSyncCommitBlockReferenceRepairsFn(&db.DB{}, handshakeOrgID, handshakeRepoID, handshakeHeadID, canonicalByFile)
	})
}

func TestFinalizeSyncCommitBlockDeltaAndSettleRepairIntent_ClearsOnSuccess(t *testing.T) {
	rec := installHandshakeSeams(t)
	var cleared []string
	origClear := clearSyncCommitBlockReferenceRepairsFn
	t.Cleanup(func() { clearSyncCommitBlockReferenceRepairsFn = origClear })
	clearSyncCommitBlockReferenceRepairsFn = func(_ *db.DB, _, _, _ string, canonicalByFile map[string][]string) error {
		for fsID := range canonicalByFile {
			cleared = append(cleared, fsID)
		}
		return nil
	}
	h := newHandshakeHandler()

	staged, err := h.stageSyncCommitBlockDelta(handshakeOrgID, handshakeRepoID, handshakeHeadID)
	if err != nil {
		t.Fatalf("stageSyncCommitBlockDelta: %v", err)
	}
	canonicalByFile := map[string][]string{"fs-r25": {handshakeBlockOne, handshakeBlockTwo}}
	if err := h.finalizeSyncCommitBlockDeltaAndSettleRepairIntent(handshakeOrgID, handshakeRepoID, handshakeHeadID, staged, canonicalByFile, "test"); err != nil {
		t.Fatalf("finalizeSyncCommitBlockDeltaAndSettleRepairIntent: %v", err)
	}
	if len(rec.promoted) == 0 {
		t.Fatal("finalize promoted nothing")
	}
	if len(cleared) != 1 || cleared[0] != "fs-r25" {
		t.Fatalf("cleared = %v, want [fs-r25] cleared on successful finalize", cleared)
	}
}

func TestFinalizeSyncCommitBlockDeltaAndSettleRepairIntent_SchedulesOnFailureNeverClears(t *testing.T) {
	rec := installHandshakeSeams(t)
	wantErr := errors.New("promote boom")
	promoteSyncPublishAttemptReferencesFn = func(_ *db.DB, _, attemptID string, blockIDs []string, _ func() error) error {
		rec.events = append(rec.events, handshakeEvent{attemptID: attemptID, phase: "promote"})
		return wantErr
	}
	clearCalls := 0
	origClear := clearSyncCommitBlockReferenceRepairsFn
	t.Cleanup(func() { clearSyncCommitBlockReferenceRepairsFn = origClear })
	clearSyncCommitBlockReferenceRepairsFn = func(*db.DB, string, string, string, map[string][]string) error {
		clearCalls++
		return nil
	}
	scheduleCalls := 0
	origSchedule := publishRepairScheduleFn
	t.Cleanup(func() { publishRepairScheduleFn = origSchedule })
	publishRepairScheduleFn = func(*db.DB, string, string, string, string, string, []string) {
		scheduleCalls++
	}
	h := newHandshakeHandler()

	staged, err := h.stageSyncCommitBlockDelta(handshakeOrgID, handshakeRepoID, handshakeHeadID)
	if err != nil {
		t.Fatalf("stageSyncCommitBlockDelta: %v", err)
	}
	canonicalByFile := map[string][]string{"fs-r25": {handshakeBlockOne, handshakeBlockTwo}}
	if err := h.finalizeSyncCommitBlockDeltaAndSettleRepairIntent(handshakeOrgID, handshakeRepoID, handshakeHeadID, staged, canonicalByFile, "test"); !errors.Is(err, wantErr) {
		t.Fatalf("finalizeSyncCommitBlockDeltaAndSettleRepairIntent error = %v, want %v", err, wantErr)
	}
	if clearCalls != 0 {
		t.Fatalf("clear was called %d time(s) on a failed finalize; a possibly-applied publish must retain its durable repair row", clearCalls)
	}
	if scheduleCalls != 1 {
		t.Fatalf("schedule was called %d time(s), want 1 on failed finalize", scheduleCalls)
	}
}

type syncW2ConcurrencyProbe struct {
	entered chan struct{}
	release chan struct{}
	active  int32
	max     int32
}

func (p *syncW2ConcurrencyProbe) enter() {
	active := atomic.AddInt32(&p.active, 1)
	for {
		max := atomic.LoadInt32(&p.max)
		if active <= max || atomic.CompareAndSwapInt32(&p.max, max, active) {
			break
		}
	}
	p.entered <- struct{}{}
	<-p.release
	atomic.AddInt32(&p.active, -1)
}

func assertSyncW2BoundedConcurrency(t *testing.T, run func(*syncW2ConcurrencyProbe) error) {
	t.Helper()
	probe := &syncW2ConcurrencyProbe{
		entered: make(chan struct{}, 64),
		release: make(chan struct{}),
	}
	done := make(chan error, 1)
	go func() {
		done <- run(probe)
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-probe.entered:
		case err := <-done:
			t.Fatalf("bounded-concurrency operation returned before concurrent work: %v", err)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for concurrent work")
		}
	}
	close(probe.release)
	if err := <-done; err != nil {
		t.Fatalf("bounded-concurrency operation returned error: %v", err)
	}
	if max := atomic.LoadInt32(&probe.max); max <= 1 {
		t.Fatalf("max concurrency = %d, want more than one worker", max)
	}
	if max := atomic.LoadInt32(&probe.max); max > syncCommitBlockPlacementConcurrency {
		t.Fatalf("max concurrency = %d, want <= %d", max, syncCommitBlockPlacementConcurrency)
	}
}

func TestSyncCommitProvenancedBlockIDsUsesBoundedConcurrency(t *testing.T) {
	withW2SyncSeams(t)
	syncBlockHasOwnLivenessProvenanceFn = func(_ *SyncHandler, _, _, _ string) (bool, error) {
		return true, nil
	}
	h := newHandshakeHandler()
	canonicalByFile := map[string][]string{"fs-1": make([]string, 40)}
	for i := range canonicalByFile["fs-1"] {
		canonicalByFile["fs-1"][i] = fmt.Sprintf("block-%02d", i)
	}

	assertSyncW2BoundedConcurrency(t, func(probe *syncW2ConcurrencyProbe) error {
		orig := syncBlockHasOwnLivenessProvenanceFn
		syncBlockHasOwnLivenessProvenanceFn = func(_ *SyncHandler, _, _, _ string) (bool, error) {
			probe.enter()
			return true, nil
		}
		defer func() { syncBlockHasOwnLivenessProvenanceFn = orig }()
		_, err := h.syncCommitProvenancedBlockIDs(handshakeOrgID, handshakeRepoID, canonicalByFile)
		return err
	})
}

func TestEnsureSyncCommitBlockOwnLivenessUsesBoundedConcurrency(t *testing.T) {
	withW2SyncSeams(t)
	h := newHandshakeHandler()
	placements := make([]syncCommitBlockPlacement, 40)
	for i := range placements {
		placements[i] = syncCommitBlockPlacement{blockID: fmt.Sprintf("block-%02d", i), storageClass: "hot", storageKey: fmt.Sprintf("key-%02d", i)}
	}

	assertSyncW2BoundedConcurrency(t, func(probe *syncW2ConcurrencyProbe) error {
		orig := syncAddProvisionalBlockReferenceFn
		syncAddProvisionalBlockReferenceFn = func(_ *db.DB, _, _, _, _, _ string, _ time.Time) error {
			probe.enter()
			return nil
		}
		defer func() { syncAddProvisionalBlockReferenceFn = orig }()
		return h.ensureSyncCommitBlockOwnLiveness(handshakeOrgID, handshakeRepoID, placements)
	})
}

func TestValidateSyncCommitBlockPublicationFencesUsesBoundedConcurrency(t *testing.T) {
	withW2SyncSeams(t)
	h := newHandshakeHandler()
	placements := make([]syncCommitBlockPlacement, 40)
	for i := range placements {
		placements[i] = syncCommitBlockPlacement{blockID: fmt.Sprintf("block-%02d", i), storageClass: "hot", storageKey: fmt.Sprintf("key-%02d", i)}
	}

	assertSyncW2BoundedConcurrency(t, func(probe *syncW2ConcurrencyProbe) error {
		orig := syncValidateBorrowedFSPublicationAuthorityFn
		syncValidateBorrowedFSPublicationAuthorityFn = func(_ *db.DB, _, _ string, _ db.BlockPhysicalLocation) (db.BlockRepairAuthorityOutcome, error) {
			probe.enter()
			return db.BlockRepairAuthorityAuthorized, nil
		}
		defer func() { syncValidateBorrowedFSPublicationAuthorityFn = orig }()
		return h.validateSyncCommitBlockPublicationFences(handshakeOrgID, placements)
	})
}
