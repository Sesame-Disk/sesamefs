package gc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Exercise the productive G1/G2/G3 handoff, then restart recovery with P2 live.
func TestG4RecoveryDeletesOnlyRetiredLifeWithReplacement(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "storage_failure_restart"}[retry], func(t *testing.T) {
			store := NewMockStore()
			sp := &MockStorageProvider{}
			org := uuid.New()
			block := testSHA256BlockID("g4-overlap")
			store.AddBlock(org, block, "hot", 0)
			store.EnqueueBlockForTest(org, time.Now().Add(-time.Hour), block, "hot", 0)
			worker := NewWorker(store, sp, NewQueue(store), 100, 0, false, &Stats{})
			if n, err := worker.ProcessOnce(context.Background()); err != nil || n != 1 {
				t.Fatalf("handoff: %d %v", n, err)
			}
			orphans := store.AllS3Orphans()
			if len(orphans) != 1 || orphans[0].RecoveryState != S3OrphanRecoveryStateCommitted {
				t.Fatalf("COMMITTED: %+v", orphans)
			}
			p1 := orphans[0]
			p2Key := MockCanonicalStorageKey(org.String(), block) + "." + uuid.NewString()
			store.AddBlock(org, block, "hot", 1)
			store.SetBlockStorageKeyForTest(org, block, p2Key)
			if retry {
				sp.FailAlways(errors.New("injected storage outage"))
			}
			restarted := NewWorker(store, sp, NewQueue(store), 100, 0, false, &Stats{})
			n, err := restarted.RecoverS3Orphans(context.Background(), 100)
			if retry {
				if err == nil || n != 0 || store.S3OrphanCount() != 1 {
					t.Fatalf("outage lost authority: %d %v", n, err)
				}
				sp = &MockStorageProvider{}
				restarted = NewWorker(store, sp, NewQueue(store), 100, 0, false, &Stats{})
				n, err = restarted.RecoverS3Orphans(context.Background(), 100)
			}
			if err != nil || n != 1 {
				t.Fatalf("physical continuation: %d %v", n, err)
			}
			deletes := sp.ScopedBlockDeletes()
			if len(deletes) != 1 || deletes[0].StorageKey != p1.StorageKey {
				t.Fatalf("D1 exact delete: %+v", deletes)
			}
			current := store.GetBlock(org, block)
			if current == nil || current.StorageKey != p2Key {
				t.Fatalf("D1 touched P2: %+v", current)
			}
			if refs, err := store.BlockHasReferencesGlobal(org, block); err != nil || !refs {
				t.Fatal("P2 reference lost")
			}
			if store.S3OrphanCount() != 0 {
				t.Fatal("P1 orphan did not settle")
			}
			if n, err := restarted.RecoverS3Orphans(context.Background(), 100); n != 0 || err != nil {
				t.Fatalf("restart replay: %d %v", n, err)
			}
			if len(sp.ScopedBlockDeletes()) != 1 {
				t.Fatal("terminal replay reacquired physical authority")
			}
		})
	}
}

func TestG4RecoveryFailsClosedWithoutExactSettlement(t *testing.T) {
	for _, scenario := range []string{"ambiguous_retirement", "topology_rejected", "certificate_missing", "certificate_terminal"} {
		t.Run(scenario, func(t *testing.T) {
			store := NewMockStore()
			sp := &MockStorageProvider{}
			org := uuid.New()
			block := testSHA256BlockID("g4-negative-" + scenario)
			store.AddBlock(org, block, "hot", 0)
			old := time.Now().Add(-time.Hour)
			d1 := store.SeedBlockClaimForTest(org, block, "g4-d1", old)
			if result := store.PrepareBlockDeleteOrphan(org, block, d1, "", old); result.Outcome != StartBlockDeleteOrphanCreated {
				t.Fatalf("prepare: %+v", result)
			}
			if result, err := store.CommitBlockDeleteOrphanHandoff(org, block, d1); err != nil || result.Outcome != BlockDeleteHandoffCommitted {
				t.Fatalf("commit: %+v %v", result, err)
			}
			committed := committedBlockDeleteAuthority(d1)
			if result := store.PromoteBlockDeleteOrphan(org, block, committed); result.Outcome != StartBlockDeleteOrphanCreated {
				t.Fatalf("promote: %+v", result)
			}
			if scenario == "ambiguous_retirement" {
				store.SetFinalizeBlockDeleteAmbiguousOnceForTest()
			}
			if scenario == "topology_rejected" {
				store.SetValidateDestructiveGCTopologyErrForTest(errors.New("injected topology drift"))
			}
			if scenario == "certificate_missing" || scenario == "certificate_terminal" {
				if result, err := store.FinalizeBlockDelete(org, block, committed); err != nil || result.Outcome != BlockDeleteFinalized {
					t.Fatalf("retire: %+v %v", result, err)
				}
				store.AddBlock(org, block, "hot", 1)
				store.SetBlockStorageKeyForTest(org, block, MockCanonicalStorageKey(org.String(), block)+"."+uuid.NewString())
				if scenario == "certificate_missing" {
					store.mu.Lock()
					delete(store.blockDeleteLifecycles, mockBlockDeleteLifecycleKey(org, block, d1.ClaimID))
					store.mu.Unlock()
				} else {
					if result, err := store.TerminateBlockDeleteLifecycle(org, block, committed); err != nil || !result.ok() {
						t.Fatalf("terminal: %+v %v", result, err)
					}
				}
			}
			worker := NewWorker(store, sp, NewQueue(store), 100, 0, false, &Stats{})
			_, err := worker.RecoverS3Orphans(context.Background(), 100)
			if scenario != "certificate_terminal" && err == nil {
				t.Fatal("unsettled state must report failure")
			}
			if len(sp.ScopedBlockDeletes()) != 0 {
				t.Fatal("unsettled/terminal D reacquired delete authority")
			}
			if scenario != "certificate_terminal" && store.S3OrphanCount() != 1 {
				t.Fatal("unsettled authority disappeared")
			}
		})
	}
}
