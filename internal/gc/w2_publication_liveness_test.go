package gc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/google/uuid"
)

type w2LivenessStore struct {
	*MockStore
	live db.BlockPublicationLiveness
}

func (s *w2LivenessStore) BlockPublicationLivenessGlobal(uuid.UUID, string) (db.BlockPublicationLiveness, error) {
	return s.live, nil
}
func TestW2RepairOnlyPreservesWorkAndOwnership(t *testing.T) {
	for _, mode := range []string{"pending", "releaseError", "foreignOwner", "unknown", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			mock := NewMockStore()
			store := &w2LivenessStore{MockStore: mock, live: db.BlockPublicationRepairGuardOnly}
			org := uuid.New()
			block := testSHA256BlockID("w2-" + mode)
			mock.AddBlock(org, block, "hot", 0)
			mock.EnqueueBlockForTest(org, time.Now().Add(-2*time.Hour), block, "hot", 0)
			switch mode {
			case "releaseError":
				mock.SetReleaseBlockClaimErrForTest(errors.New("release unavailable"))
			case "foreignOwner":
				mock.SetReleaseBlockClaimHookForTest(func() { mock.SetBlockGCStateForTest(org, block, db.BlockGCStateDeleting, "foreign", time.Now()) })
			case "unknown":
				store.live = db.BlockPublicationUnknown
			case "invalid":
				store.live = db.BlockPublicationLiveness(255)
			}
			worker := NewWorker(store, &MockStorageProvider{}, NewQueue(store), 100, 0, false, &Stats{})
			if _, err := worker.ProcessOrgOnce(context.Background(), org); err != nil {
				t.Fatal(err)
			}
			if _, found := mock.GetBlockGCCandidateForTest(org, block); !found {
				t.Fatal("candidate consumed")
			}
			b := mock.GetBlock(org, block)
			if b == nil || (b.GCOrphanHandoff != nil && *b.GCOrphanHandoff) {
				t.Fatal("must never commit D")
			}
			if (mode == "pending" || mode == "unknown" || mode == "invalid") && (b.GCState != "" || b.GCClaimID != "") {
				t.Fatalf("claim remains: %+v", b)
			}
			if mode == "foreignOwner" && b.GCClaimID != "foreign" {
				t.Fatal("foreign ownership changed")
			}
			if mode == "pending" {
				items, err := mock.DequeueBatch(org, 100, time.Now().Add(time.Hour))
				if err != nil || len(items) != 1 || items[0].RetryCount != 0 {
					t.Fatalf("work/retry changed: %+v %v", items, err)
				}
				store.live = db.BlockPublicationZero
				if _, err := worker.ProcessOrgOnce(context.Background(), org); err != nil {
					t.Fatal(err)
				}
				if mock.GetBlock(org, block) != nil {
					t.Fatal("zero must permit canonical retirement on next pass")
				}
			}
		})
	}
}
