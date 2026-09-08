//go:build integration

package integration

import (
	"fmt"
	"testing"
	"time"

	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/google/uuid"
)

// X1 / R19: UpdateS3OrphanAttempt must never bring an orphan into existence.
func TestGC_UpdateS3OrphanAttempt_DoesNotResurrectClearedOrphan(t *testing.T) {
	requireCassandra(t)

	database := shareProjectionDBForTest(t)
	store := gcpkg.NewCassandraStore(database)
	orgID := uuid.New()
	blockID := fmt.Sprintf("orph-no-resurrect-%d", time.Now().UnixNano())
	firstSeenAt := time.Now().UTC().Truncate(time.Millisecond)
	authority := testCommittedOrphanAuthority(blockID, "hot", syntheticCanonicalStorageKeyForTest(orgID.String(), blockID))

	result := store.StartBlockDeleteOrphan(orgID, blockID, authority, "", firstSeenAt)
	if result.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
		t.Fatalf("StartBlockDeleteOrphan: outcome=%s cause=%v", result.Outcome, result.Cause)
	}
	t.Cleanup(func() {
		if err := store.DeleteS3Orphan(orgID, blockID, authority.Authority(), firstSeenAt); err != nil {
			t.Logf("cleanup DeleteS3Orphan(%s): %v", blockID, err)
		}
	})

	if err := store.UpdateS3OrphanAttempt(orgID, blockID, authority.Authority(), "first failure", time.Now().UTC()); err != nil {
		t.Fatalf("UpdateS3OrphanAttempt on a live row: %v", err)
	}
	if !gcS3OrphanExists(t, orgID.String(), blockID) {
		t.Fatal("orphan row vanished after recording an attempt on it")
	}

	if err := store.DeleteS3Orphan(orgID, blockID, authority.Authority(), firstSeenAt); err != nil {
		t.Fatalf("DeleteS3Orphan: %v", err)
	}
	if gcS3OrphanExists(t, orgID.String(), blockID) {
		t.Fatal("orphan row still present after DeleteS3Orphan")
	}

	if err := store.UpdateS3OrphanAttempt(orgID, blockID, authority.Authority(), "late failure after clear", time.Now().UTC()); err != nil {
		t.Fatalf("UpdateS3OrphanAttempt after clear returned error: %v", err)
	}
	if gcS3OrphanExists(t, orgID.String(), blockID) {
		t.Fatal("UpdateS3OrphanAttempt resurrected a cleared orphan row")
	}
	if gcS3OrphanProjectionExists(t, orgID.String(), blockID, firstSeenAt) {
		t.Fatal("discovery projection reappeared for a cleared orphan")
	}
}

// A delayed P1 diagnostic must not update a different P2 identity sharing the
// same canonical key.
func TestGC_UpdateS3OrphanAttempt_RejectsDifferentLifecycleToken(t *testing.T) {
	requireCassandra(t)

	database := shareProjectionDBForTest(t)
	store := gcpkg.NewCassandraStore(database)
	orgID := uuid.New()
	blockID := fmt.Sprintf("orph-incarnation-%d", time.Now().UnixNano())
	p1FirstSeenAt := time.Now().UTC().Truncate(time.Millisecond)
	p2FirstSeenAt := p1FirstSeenAt.Add(time.Second)
	storageKey := syntheticCanonicalStorageKeyForTest(orgID.String(), blockID)
	p1Authority := testCommittedOrphanAuthority(blockID, "hot", storageKey)

	p1 := store.StartBlockDeleteOrphan(orgID, blockID, p1Authority, "sha1-p1", p1FirstSeenAt)
	if p1.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
		t.Fatalf("StartBlockDeleteOrphan P1: outcome=%s cause=%v", p1.Outcome, p1.Cause)
	}
	if err := store.DeleteS3Orphan(orgID, blockID, p1Authority.Authority(), p1FirstSeenAt); err != nil {
		t.Fatalf("DeleteS3Orphan P1: %v", err)
	}
	p2Authority := testCommittedOrphanAuthorityWithClaimID(blockID, "cold", storageKey, "test-orphan-claim:p2:"+uuid.NewString())
	p2 := store.StartBlockDeleteOrphan(orgID, blockID, p2Authority, "sha1-p2", p2FirstSeenAt)
	if p2.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
		t.Fatalf("StartBlockDeleteOrphan P2: outcome=%s cause=%v, want created", p2.Outcome, p2.Cause)
	}
	t.Cleanup(func() {
		if err := store.DeleteS3Orphan(orgID, blockID, p2Authority.Authority(), p2FirstSeenAt); err != nil {
			t.Logf("cleanup DeleteS3Orphan(%s): %v", blockID, err)
		}
	})

	if err := store.UpdateS3OrphanAttempt(orgID, blockID, p1Authority.Authority(), "late P1 failure", p2FirstSeenAt.Add(time.Second)); err != nil {
		t.Fatalf("UpdateS3OrphanAttempt for stale P1: %v", err)
	}

	var storedFirstSeenAt time.Time
	var retryCount int
	var lastError string
	if err := database.Session().Query(`
		SELECT first_seen_at, retry_count, last_error
		FROM gc_s3_orphans WHERE org_id = ? AND block_id = ?
	`, orgID.String(), blockID).Scan(&storedFirstSeenAt, &retryCount, &lastError); err != nil {
		t.Fatalf("read P2 orphan after stale update: %v", err)
	}
	if !storedFirstSeenAt.Equal(p2FirstSeenAt) {
		t.Fatalf("first_seen_at = %v, want P2 %v", storedFirstSeenAt, p2FirstSeenAt)
	}
	if retryCount != 0 {
		t.Fatalf("retry_count = %d after stale P1 update, want 0", retryCount)
	}
	if lastError != "" {
		t.Fatalf("last_error = %q after stale P1 update, want empty", lastError)
	}
}
