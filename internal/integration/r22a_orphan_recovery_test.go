//go:build integration

package integration

import (
	"testing"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/google/uuid"
)

func TestGC_R22aCanonicalReadAndDiscoveryIdentity(t *testing.T) {
	requireCassandra(t)

	database := shareProjectionDBForTest(t)
	store := gcpkg.NewCassandraStore(database)
	orgID := uuid.New()
	blockID := "r22a-canonical-" + uuid.NewString()
	firstSeenAt := time.Now().UTC().Truncate(time.Millisecond)
	effectiveFirstSeenAt := seedS3Orphan(t, store, orgID, blockID, "hot", "sha1-canonical", "seed", firstSeenAt)
	t.Cleanup(func() {
		if err := store.DeleteS3Orphan(orgID, blockID, testCommittedOrphanAuthority(blockID, "hot", syntheticCanonicalStorageKeyForTest(orgID.String(), blockID)).Authority(), effectiveFirstSeenAt); err != nil {
			t.Errorf("cleanup DeleteS3Orphan: %v", err)
		}
	})

	// R22b removed the poisoning step this test was built around. It used to write
	// storage_class/representation_id/external_sha1/recovery_phase onto the
	// discovery row and prove recovery ignored them; migration 014 dropped those
	// columns, so the statement is no longer expressible and the property is
	// structural — see TestR22bProjectionSchemaIsIdentityOnly. What remains here is
	// the functional half, which no schema gate can express: canonical state is
	// intact and discovery yields the identity that points at it.
	bucket := db.GCDiscoveryBucket(orgID.String(), blockID)

	canonical, found, err := store.GetS3OrphanExact(orgID, blockID, testCommittedOrphanAuthority(blockID, "hot", syntheticCanonicalStorageKeyForTest(orgID.String(), blockID)).Authority())
	if err != nil {
		t.Fatalf("GetS3OrphanGlobal: %v", err)
	}
	if !found {
		t.Fatal("canonical orphan not found")
	}
	if canonical.StorageClass != "hot" || canonical.ExternalSHA1 != "sha1-canonical" {
		t.Fatalf("canonical orphan state changed unexpectedly: %+v", canonical)
	}

	discovery, err := store.ListS3OrphansByDay(effectiveFirstSeenAt, bucket, 100)
	if err != nil {
		t.Fatalf("ListS3OrphansByDay: %v", err)
	}
	authority := testCommittedOrphanAuthority(blockID, "hot", syntheticCanonicalStorageKeyForTest(orgID.String(), blockID)).Authority()
	discoveryFound := false
	for _, row := range discovery {
		if row.OrgID == orgID && row.BlockID == blockID && row.FirstSeenAt.Equal(effectiveFirstSeenAt) && g1SameAuthority(row.Authority, authority) {
			discoveryFound = true
			break
		}
	}
	if !discoveryFound {
		t.Fatalf("unexpected discovery identity: %+v", discovery)
	}
}
