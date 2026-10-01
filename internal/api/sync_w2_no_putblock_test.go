package api

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
)

func TestSyncReadinessCapturesAllBlocksButRenewsOnlyPutBlockSubset(t *testing.T) {
	withW2SyncSeams(t)
	syncBlockHasOwnLivenessProvenanceFn = func(_ *SyncHandler, _, _, blockID string) (bool, error) {
		return blockID == "uploaded", nil
	}
	var mu sync.Mutex
	var probes, renewed, validated []string
	syncProbeBlockReuseForPlacementFn = func(_ *SyncHandler, _, blockID string) (db.BlockReuseProbe, error) {
		mu.Lock()
		probes = append(probes, blockID)
		mu.Unlock()
		return db.BlockReuseProbe{Decision: db.BlockReuseReusable, StorageClass: "hot", StorageKey: "P1-" + blockID}, nil
	}
	origAdd := syncAddProvisionalBlockReferenceFn
	t.Cleanup(func() { syncAddProvisionalBlockReferenceFn = origAdd })
	syncAddProvisionalBlockReferenceFn = func(_ *db.DB, _, blockID, referrer, _, _ string, _ time.Time) error {
		if referrer != syncBlockUploadReferrer(handshakeRepoID, blockID) {
			t.Errorf("wrong upload identity: %s", referrer)
		}
		mu.Lock()
		renewed = append(renewed, blockID)
		mu.Unlock()
		return nil
	}
	syncValidateBorrowedFSPublicationAuthorityFn = func(_ *db.DB, _, blockID string, placement db.BlockPhysicalLocation) (db.BlockRepairAuthorityOutcome, error) {
		if placement.StorageKey != "P1-"+blockID {
			t.Errorf("wrong captured placement: %+v", placement)
		}
		mu.Lock()
		validated = append(validated, blockID)
		mu.Unlock()
		return db.BlockRepairAuthorityAuthorized, nil
	}
	placements, err := newHandshakeHandler().prepareSyncCommitBlockPublicationReadiness(handshakeOrgID, handshakeRepoID,
		map[string][]string{"file-a": {"uploaded", "dedup"}, "file-b": {"dedup"}})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(probes)
	sort.Strings(validated)
	if len(placements) != 2 || !reflect.DeepEqual(probes, []string{"dedup", "uploaded"}) ||
		!reflect.DeepEqual(validated, probes) || !reflect.DeepEqual(renewed, []string{"uploaded"}) {
		t.Fatalf("placements=%v probes=%v validated=%v renewed=%v", placements, probes, validated, renewed)
	}
}

func TestAutoMergeRevalidatesCapturedDedupPlacementAfterRepair(t *testing.T) {
	for _, outcome := range []db.BlockRepairAuthorityOutcome{db.BlockRepairAuthorityBlocked, db.BlockRepairAuthorityChanged, db.BlockRepairAuthorityUnknown} {
		t.Run(fmt.Sprint(outcome), func(t *testing.T) {
			withW2SyncSeams(t)
			syncBlockHasOwnLivenessProvenanceFn = func(*SyncHandler, string, string, string) (bool, error) { return false, nil }
			probes := 0
			syncProbeBlockReuseForPlacementFn = func(*SyncHandler, string, string) (db.BlockReuseProbe, error) {
				probes++
				return db.BlockReuseProbe{Decision: db.BlockReuseReusable, StorageClass: "hot", StorageKey: "P1"}, nil
			}
			queued := false
			queueSyncCommitBlockReferenceRepairsFn = func(*db.DB, string, string, string, map[string][]string) error {
				queued = true
				return nil
			}
			validations := 0
			syncValidateBorrowedFSPublicationAuthorityFn = func(_ *db.DB, _, _ string, placement db.BlockPhysicalLocation) (db.BlockRepairAuthorityOutcome, error) {
				validations++
				if placement.StorageKey != "P1" {
					t.Fatalf("captured P1 was replaced after acquisition: %+v", placement)
				}
				if queued {
					return outcome, errors.New("GC won or authority could not be established")
				}
				return db.BlockRepairAuthorityAuthorized, nil
			}
			err := newHandshakeHandler().ensureAndQueueAutoMergeSyncPublication(handshakeOrgID, handshakeRepoID, handshakeHeadID,
				map[string][]string{"file": {"dedup"}})
			var queueErr *syncAutoMergeRepairQueueError
			if !errors.As(err, &queueErr) || !queued || validations != 2 || probes != 1 {
				t.Fatalf("final check must reject captured dedup P1 after repair: err=%v queued=%v validations=%d probes=%d", err, queued, validations, probes)
			}
		})
	}
}

func TestSyncReadinessEmptyDeltaRequiresNoPlacement(t *testing.T) {
	withW2SyncSeams(t)
	syncBlockHasOwnLivenessProvenanceFn = func(*SyncHandler, string, string, string) (bool, error) {
		t.Fatal("empty delta queried provenance")
		return false, nil
	}
	syncProbeBlockReuseForPlacementFn = func(*SyncHandler, string, string) (db.BlockReuseProbe, error) {
		t.Fatal("empty delta queried placement")
		return db.BlockReuseProbe{}, nil
	}
	placements, err := newHandshakeHandler().prepareSyncCommitBlockPublicationReadiness(handshakeOrgID, handshakeRepoID, nil)
	if err != nil || len(placements) != 0 {
		t.Fatalf("empty delta: placements=%v err=%v", placements, err)
	}
}
