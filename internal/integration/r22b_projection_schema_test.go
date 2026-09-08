//go:build integration

package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/google/uuid"
)

// TestR22bProjectionSchemaIsIdentityOnly asserts the effective schema after the
// whole migration chain, not the text of any one migration file.
//
// This is what makes R22b structural. R22a could only promise that no reader
// consulted the projection payload; a future refactor could have re-wired one.
// G1 keeps the complete (P,D) identity in the projection key, so the gate
// requires every canonical identity column and rejects mutable payload columns.
func TestR22bProjectionSchemaIsIdentityOnly(t *testing.T) {
	requireCassandra(t)

	keyspace := envOrDefault("CASSANDRA_KEYSPACE", "sesamefs")
	session := shareProjectionDBForTest(t).Session()

	present := map[string]string{}
	iter := session.Query(`
		SELECT column_name, kind
		FROM system_schema.columns
		WHERE keyspace_name = ? AND table_name = ?
	`, keyspace, "gc_s3_orphans_by_day").Iter()
	var columnName, kind string
	for iter.Scan(&columnName, &kind) {
		present[columnName] = kind
	}
	if err := iter.Close(); err != nil {
		t.Fatalf("read effective gc_s3_orphans_by_day columns: %v", err)
	}
	if len(present) == 0 {
		t.Fatal("gc_s3_orphans_by_day has no columns in system_schema; the gate would pass vacuously")
	}

	expectedKinds := map[string]string{
		"first_seen_day": "partition_key",
		"bucket":         "partition_key",
		"first_seen_at":  "clustering",
		"org_id":         "clustering",
		"block_id":       "clustering",
		"storage_class":  "clustering",
		"storage_key":    "clustering",
		"gc_claim_id":    "clustering",
		"gc_claimed_at":  "clustering",
	}
	if len(present) != len(expectedKinds) {
		t.Errorf("gc_s3_orphans_by_day columns = %v, want exactly %v", present, expectedKinds)
	}
	for name, kind := range present {
		want, ok := expectedKinds[name]
		if !ok {
			t.Errorf("gc_s3_orphans_by_day has unexpected column %q (kind=%s); G1 projection must contain identity only", name, kind)
			continue
		}
		if kind != want {
			t.Errorf("gc_s3_orphans_by_day column %q has kind %q, want %q", name, kind, want)
		}
	}
}

// TestGC_R22bProjectionRowIsIdentityOnly confirms against the real engine that a
// row whose every column is a primary-key column still reads back and is still
// enumerable by the day scan. The discovery root and projection are non-expiring
// under G1, so neither depends on a table TTL.
func TestGC_R22bProjectionRowIsIdentityOnly(t *testing.T) {
	requireCassandra(t)

	database := shareProjectionDBForTest(t)
	store := gcpkg.NewCassandraStore(database)
	orgID := uuid.New()
	blockID := fmt.Sprintf("orph-r22b-identity-%d", time.Now().UnixNano())
	firstSeenAt := time.Now().UTC().Truncate(time.Millisecond)

	result := store.StartBlockDeleteOrphan(orgID, blockID, testCommittedOrphanAuthority(blockID, "hot", syntheticCanonicalStorageKeyForTest(orgID.String(), blockID)), "sha1-r22b", firstSeenAt)
	if result.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
		t.Fatalf("StartBlockDeleteOrphan: outcome=%s cause=%v", result.Outcome, result.Cause)
	}
	effectiveFirstSeenAt := result.FirstSeenAt
	t.Cleanup(func() {
		if err := store.DeleteS3Orphan(orgID, blockID, testCommittedOrphanAuthority(blockID, "hot", syntheticCanonicalStorageKeyForTest(orgID.String(), blockID)).Authority(), effectiveFirstSeenAt); err != nil {
			t.Errorf("cleanup DeleteS3Orphan: %v", err)
		}
	})

	// The marker-only row must come back from a direct point read.
	var gotFirstSeenAt time.Time
	var gotOrgID, gotBlockID string
	if err := database.Session().Query(`
		SELECT first_seen_at, org_id, block_id
		FROM gc_s3_orphans_by_day
		WHERE first_seen_day = ? AND bucket = ? AND first_seen_at = ? AND org_id = ? AND block_id = ?
	`, db.GCProjectionUTCDate(effectiveFirstSeenAt), db.GCDiscoveryBucket(orgID.String(), blockID),
		effectiveFirstSeenAt, orgID.String(), blockID).Scan(&gotFirstSeenAt, &gotOrgID, &gotBlockID); err != nil {
		t.Fatalf("a primary-key-only discovery row did not read back: %v", err)
	}
	if gotOrgID != orgID.String() || gotBlockID != blockID || !gotFirstSeenAt.UTC().Equal(effectiveFirstSeenAt.UTC()) {
		t.Fatalf("discovery identity = (%s, %s, %v), want (%s, %s, %v)",
			gotOrgID, gotBlockID, gotFirstSeenAt.UTC(), orgID, blockID, effectiveFirstSeenAt.UTC())
	}

	// And it must be enumerable, which is the only reason the row exists.
	discovery, err := store.ListS3OrphansByDay(effectiveFirstSeenAt, db.GCDiscoveryBucket(orgID.String(), blockID), 100)
	if err != nil {
		t.Fatalf("ListS3OrphansByDay: %v", err)
	}
	found := false
	for _, row := range discovery {
		if row.OrgID == orgID && row.BlockID == blockID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("marker-only discovery row is not enumerable by the day scan")
	}

	// G1 removes the pending TTL entirely. Both canonical and discovery tables
	// must have an explicit zero default so old 90-day behavior cannot return.
	keyspace := envOrDefault("CASSANDRA_KEYSPACE", "sesamefs")
	for _, table := range []string{"gc_s3_orphans_by_day", "gc_s3_orphans", "gc_s3_orphan_recovery_roots"} {
		var defaultTTL int
		if err := database.Session().Query(`
			SELECT default_time_to_live
			FROM system_schema.tables
			WHERE keyspace_name = ? AND table_name = ?
		`, keyspace, table).Scan(&defaultTTL); err != nil {
			t.Fatalf("read %s default_time_to_live: %v", table, err)
		}
		if defaultTTL != 0 {
			t.Fatalf("G1 orphan table %s default_time_to_live = %d, want 0", table, defaultTTL)
		}
	}
}
