package api

import (
	"errors"
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
	origResolve := resolveSyncBlockIDsFn
	origQueue := queueSyncCommitBlockReferenceRepairsFn
	origClear := clearSyncCommitBlockReferenceRepairsFn
	t.Cleanup(func() {
		syncBlockHasOwnLivenessProvenanceFn = origHasProvenance
		syncProbeBlockReuseForPlacementFn = origProbe
		syncValidateBorrowedFSPublicationAuthorityFn = origAuthority
		resolveSyncBlockIDsFn = origResolve
		queueSyncCommitBlockReferenceRepairsFn = origQueue
		clearSyncCommitBlockReferenceRepairsFn = origClear
	})
	resolveSyncBlockIDsFn = func(_ *SyncHandler, _, _ string, blockIDs []string) ([]string, error) {
		return db.NormalizeBlockIDs(blockIDs), nil
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
	resolveSyncBlockIDsFn = func(_ *SyncHandler, _, _ string, blockIDs []string) ([]string, error) {
		calls++
		return db.NormalizeBlockIDs(blockIDs), nil
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
		t.Fatalf("resolveSyncBlockIDsFn called %d times, want 1 batch call (no N-per-file resolutions)", calls)
	}
	if got := result["fs-1"]; len(got) != 2 {
		t.Fatalf("fs-1 canonical = %v, want 2 entries", got)
	}
	if got := result["fs-2"]; len(got) != 2 {
		t.Fatalf("fs-2 canonical = %v, want 2 entries", got)
	}
}

func TestResolveSyncCommitAddedFilesCanonical_CollisionFallsBackPerID(t *testing.T) {
	withW2SyncSeams(t)
	// Model two distinct raw IDs colliding onto the same canonical value: the
	// batch call's own final dedup shrinks its result below len(union), which
	// must trigger the individual-resolution fallback rather than a silent
	// mis-association.
	resolveSyncBlockIDsFn = func(_ *SyncHandler, _, _ string, blockIDs []string) ([]string, error) {
		if len(blockIDs) == 1 {
			if blockIDs[0] == "legacy-sha1" || blockIDs[0] == "already-canonical" {
				return []string{"canonical-x"}, nil
			}
			return blockIDs, nil
		}
		out := make([]string, 0, len(blockIDs))
		for range blockIDs {
			out = append(out, "canonical-x") // simulate collision then final dedup
		}
		return db.NormalizeBlockIDs(out), nil
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
	if got := result["fs-1"]; len(got) != 1 || got[0] != "canonical-x" {
		t.Fatalf("fs-1 canonical = %v, want [canonical-x]", got)
	}
	if got := result["fs-2"]; len(got) != 1 || got[0] != "canonical-x" {
		t.Fatalf("fs-2 canonical = %v, want [canonical-x]", got)
	}
}

// --- syncCommitProvenancedBlockIDs / ensureSyncCommitBlockPublicationReadiness: scope gate ---

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

// --- durable repair-row queueing: per-file identity, rollback on partial failure ---

func TestQueueSyncCommitBlockReferenceRepairs_PartialFailureRollsBackEarlierRows(t *testing.T) {
	origQueue := publishRepairQueueFn
	origClear := publishRepairClearFn
	t.Cleanup(func() {
		publishRepairQueueFn = origQueue
		publishRepairClearFn = origClear
	})
	var queued, cleared []string
	wantErr := errors.New("queue boom on fs-2")
	publishRepairQueueFn = func(_ *db.DB, _, _, _, fsID string, _ []string) error {
		queued = append(queued, fsID)
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
	if len(cleared) != 2 || cleared[0] != "fs-1" || cleared[1] != "fs-2" {
		t.Fatalf("cleared = %v, want [fs-1 fs-2] rolled back after fs-2 failed to queue", cleared)
	}
}

// --- settlement: finalize wrapper clears on success, schedules on failure ---

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
