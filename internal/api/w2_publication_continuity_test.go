package api

import (
	"errors"
	"github.com/Sesame-Disk/sesamefs/internal/db"
	"testing"
	"time"
)

func TestW2SyncRetainsPlacementThroughRepairAcquisition(t *testing.T) {
	withW2SyncSeams(t)
	queued := false
	scopeReads, authorityReads := 0, 0
	syncBlockHasOwnLivenessProvenanceFn = func(*SyncHandler, string, string, string) (bool, error) { scopeReads++; return !queued, nil }
	syncProbeBlockReuseForPlacementFn = func(*SyncHandler, string, string) (db.BlockReuseProbe, error) {
		return db.BlockReuseProbe{Decision: db.BlockReuseReusable, StorageClass: "hot", StorageKey: "captured-key"}, nil
	}
	old := syncAddProvisionalBlockReferenceFn
	t.Cleanup(func() { syncAddProvisionalBlockReferenceFn = old })
	syncAddProvisionalBlockReferenceFn = func(*db.DB, string, string, string, string, string, time.Time) error { return nil }
	queueSyncCommitBlockReferenceRepairsFn = func(*db.DB, string, string, string, map[string][]string) error { queued = true; return nil }
	changed := errors.New("GC acquired authority in readiness-to-queue gap")
	syncValidateBorrowedFSPublicationAuthorityFn = func(_ *db.DB, _, _ string, p db.BlockPhysicalLocation) (db.BlockRepairAuthorityOutcome, error) {
		authorityReads++
		if p.StorageKey != "captured-key" {
			t.Fatal("placement changed during acquisition")
		}
		if queued {
			return db.BlockRepairAuthorityBlocked, changed
		}
		return db.BlockRepairAuthorityAuthorized, nil
	}
	err := newHandshakeHandler().ensureAndQueueAutoMergeSyncPublication(handshakeOrgID, handshakeRepoID, handshakeHeadID, map[string][]string{"fs-1": {"b1"}})
	if !errors.Is(err, changed) || scopeReads != 1 || authorityReads != 2 {
		t.Fatalf("must validate captured P after queue without rescoping expired provenance: err=%v scope=%d authority=%d", err, scopeReads, authorityReads)
	}
	var postQueue *syncAutoMergeRepairQueueError
	if !errors.As(err, &postQueue) {
		t.Fatal("post-queue failure must take existing owned repair cleanup path")
	}
}
