//go:build integration

package integration

import (
	"context"
	"github.com/Sesame-Disk/sesamefs/internal/db"
	"testing"
	"time"

	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/google/uuid"
)

// seedS3Orphan creates integration state through the production lifecycle
// entry point. A failed initial delete is represented by the same follow-up
// mutation the worker uses, rather than by a second row-creating API.
func seedS3Orphan(t *testing.T, store gcpkg.GCStore, orgID uuid.UUID, blockID, storageClass, externalSHA1, errMsg string, firstSeenAt time.Time) time.Time {
	return seedS3OrphanWithStorageKey(t, store, orgID, blockID, syntheticCanonicalStorageKeyForTest(orgID.String(), blockID), storageClass, externalSHA1, errMsg, firstSeenAt)
}

func seedS3OrphanWithStorageKey(t *testing.T, store gcpkg.GCStore, orgID uuid.UUID, blockID, storageKey, storageClass, externalSHA1, errMsg string, firstSeenAt time.Time) time.Time {
	t.Helper()
	firstSeenAt = firstSeenAt.UTC().Truncate(time.Millisecond)
	authority := gcpkg.CommittedBlockDeleteAuthorityForTest(gcpkg.BlockDeleteAuthority{
		Target:    gcpkg.BlockDeleteTarget{StorageClass: storageClass, StorageKey: storageKey},
		ClaimID:   "test-orphan-claim:" + blockID,
		ClaimedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	result := store.StartBlockDeleteOrphan(orgID, blockID, authority, externalSHA1, firstSeenAt)
	if result.Outcome != gcpkg.StartBlockDeleteOrphanCreated && result.Outcome != gcpkg.StartBlockDeleteOrphanSameAuthority {
		t.Fatalf("StartBlockDeleteOrphan: outcome=%s cause=%v", result.Outcome, result.Cause)
	}
	effectiveFirstSeenAt := result.FirstSeenAt
	if errMsg != "" {
		if err := store.UpdateS3OrphanAttempt(orgID, blockID, gcpkg.BlockDeleteAuthority{
			Target:    gcpkg.BlockDeleteTarget{StorageClass: storageClass, StorageKey: storageKey},
			ClaimID:   "test-orphan-claim:" + blockID,
			ClaimedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		}, errMsg, firstSeenAt); err != nil {
			t.Fatalf("UpdateS3OrphanAttempt: %v", err)
		}
	}
	return effectiveFirstSeenAt
}

func testCommittedOrphanAuthority(blockID, storageClass, storageKey string) gcpkg.CommittedBlockDeleteAuthority {
	return testCommittedOrphanAuthorityWithClaimID(blockID, storageClass, storageKey, "test-orphan-claim:"+blockID)
}

func testCommittedOrphanAuthorityWithClaimID(blockID, storageClass, storageKey, claimID string) gcpkg.CommittedBlockDeleteAuthority {
	return gcpkg.CommittedBlockDeleteAuthorityForTest(gcpkg.BlockDeleteAuthority{
		Target:    gcpkg.BlockDeleteTarget{StorageClass: storageClass, StorageKey: storageKey},
		ClaimID:   claimID,
		ClaimedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	})
}

// Current-protocol fixture; no empty-state publisher or direct recovery-state write.
func seedCurrentS3Orphan(t *testing.T, store gcpkg.GCStore, org uuid.UUID, block string, authority gcpkg.BlockDeleteAuthority, sha1 string, firstSeen time.Time) gcpkg.StartBlockDeleteOrphanResult {
	t.Helper()
	database := shareProjectionDBForTest(t)
	location := db.BlockPhysicalLocation{StorageClass: authority.Target.StorageClass, StorageKey: authority.Target.StorageKey}
	if result := database.InstallBlockMetadata(context.Background(), org.String(), db.PlainBlockRepresentationID, block, "", 1, location); result.Outcome != db.InstallBlockMetadataApplied {
		t.Fatalf("fixture install: %+v", result)
	}
	if result, err := store.ClaimBlockDelete(org, block, authority); err != nil || result.Outcome != gcpkg.BlockClaimAcquired {
		t.Fatalf("fixture claim: %+v %v", result, err)
	}
	prepared := store.PrepareBlockDeleteOrphan(org, block, authority, sha1, firstSeen)
	if prepared.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
		t.Fatalf("fixture prepare: %+v", prepared)
	}
	handoff, err := store.CommitBlockDeleteOrphanHandoff(org, block, authority)
	if err != nil || handoff.Outcome != gcpkg.BlockDeleteHandoffCommitted {
		t.Fatalf("fixture commit: %+v %v", handoff, err)
	}
	promoted := store.PromoteBlockDeleteOrphan(org, block, handoff.Authority)
	if promoted.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
		t.Fatalf("fixture promote: %+v", promoted)
	}
	if result, err := store.FinalizeBlockDelete(org, block, handoff.Authority); err != nil || result.Outcome != gcpkg.BlockDeleteFinalized {
		t.Fatalf("fixture retirement: %+v %v", result, err)
	}
	return promoted
}
