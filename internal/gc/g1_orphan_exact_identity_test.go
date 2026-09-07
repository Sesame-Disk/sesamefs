package gc

import (
	"context"
	"testing"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/google/uuid"
)

func TestG1MockOrphanRowsRemainDistinctByExactPD(t *testing.T) {
	store := NewMockStore()
	orgID := uuid.New()
	blockID := testSHA256BlockID("g1-exact-identity")
	claimedAt := time.Date(2026, 9, 1, 12, 0, 0, 123456789, time.UTC)
	authority1 := committedBlockDeleteAuthority(BlockDeleteAuthority{
		Target:    BlockDeleteTarget{StorageClass: "hot", StorageKey: MockCanonicalStorageKey(orgID.String(), blockID)},
		ClaimID:   "g1-claim-1",
		ClaimedAt: claimedAt,
	})
	authority2 := committedBlockDeleteAuthority(BlockDeleteAuthority{
		Target:    authority1.Authority().Target,
		ClaimID:   "g1-claim-2",
		ClaimedAt: claimedAt.Add(time.Second),
	})
	authority3 := committedBlockDeleteAuthority(BlockDeleteAuthority{
		Target:    BlockDeleteTarget{StorageClass: "cold", StorageKey: "cold/" + blockID},
		ClaimID:   "g1-claim-3",
		ClaimedAt: claimedAt.Add(2 * time.Second),
	})
	for _, authority := range []CommittedBlockDeleteAuthority{authority1, authority2, authority3} {
		result := store.StartBlockDeleteOrphan(orgID, blockID, authority, "", claimedAt)
		if result.Outcome != StartBlockDeleteOrphanCreated {
			t.Fatalf("StartBlockDeleteOrphan(%s): %s: %v", authority.Authority().Target.StorageClass, result.Outcome, result.Cause)
		}
	}

	if err := store.UpdateS3OrphanAttempt(orgID, blockID, authority1.Authority(), "P1", time.Now().UTC()); err != nil {
		t.Fatalf("update P1: %v", err)
	}
	if err := store.DeleteS3Orphan(orgID, blockID, authority1.Authority(), time.Time{}); err != nil {
		t.Fatalf("delete P1: %v", err)
	}
	for name, authority := range map[string]CommittedBlockDeleteAuthority{"P1/D2": authority2, "P2/D3": authority3} {
		remaining, found, err := store.GetS3OrphanExact(orgID, blockID, authority.Authority())
		if err != nil || !found {
			t.Fatalf("exact %s read = found:%v err:%v", name, found, err)
		}
		if !remaining.Authority.sameAuthority(normalizeBlockDeleteAuthority(authority.Authority())) || remaining.RetryCount != 0 {
			t.Fatalf("%s changed after exact P1 mutation: %+v", name, remaining)
		}
		if remaining.Authority.ClaimedAt.Nanosecond()%int(time.Millisecond) != 0 {
			t.Fatalf("%s claimed_at was not normalized to milliseconds: %v", name, remaining.Authority.ClaimedAt)
		}
	}
	if roots := g1RootCount(t, store); roots != 2 {
		t.Fatalf("recovery roots after exact P1 delete = %d, want 2", roots)
	}
}

func TestG1RecoveryRootRepairsMissingProjectionAndRecoversCanonical(t *testing.T) {
	store := NewMockStore()
	storage := &MockStorageProvider{}
	worker := NewWorker(store, storage, NewQueue(store), 100, 0, false, &Stats{})
	orgID := uuid.New()
	blockID := testSHA256BlockID("g1-root-repair")
	authority := testCommittedOrphanAuthorityForOrg(orgID, blockID, "hot")
	firstSeenAt := time.Now().UTC().Add(-time.Hour)
	result := store.StartBlockDeleteOrphan(orgID, blockID, authority, "", firstSeenAt)
	if result.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("seed root repair orphan: %s: %v", result.Outcome, result.Cause)
	}
	store.DeleteS3OrphanProjectionForTest(orgID, blockID, result.FirstSeenAt)

	recovered, err := worker.RecoverS3Orphans(context.Background(), 100)
	if err != nil || recovered != 1 {
		t.Fatalf("RecoverS3Orphans() = (%d, %v), want one root-recovered orphan", recovered, err)
	}
	if len(storage.DeletedBlocks()) != 1 || store.S3OrphanCount() != 0 {
		t.Fatalf("root repair did not settle exact orphan: deletes=%v rows=%d", storage.DeletedBlocks(), store.S3OrphanCount())
	}
	if roots := g1RootCount(t, store); roots != 0 {
		t.Fatalf("recovery root count = %d, want 0 after exact settlement", roots)
	}
}

func TestG1RepeatedPublicationIsIdempotentForExactIdentity(t *testing.T) {
	store := NewMockStore()
	orgID := uuid.New()
	blockID := testSHA256BlockID("g1-restart-idempotence")
	authority := testCommittedOrphanAuthorityForOrg(orgID, blockID, "hot")
	firstSeenAt := time.Now().UTC().Add(-time.Hour).Add(123 * time.Microsecond)

	first := store.StartBlockDeleteOrphan(orgID, blockID, authority, "sha1-original", firstSeenAt)
	second := store.StartBlockDeleteOrphan(orgID, blockID, authority, "sha1-replayed", firstSeenAt.Add(time.Second))
	if first.Outcome != StartBlockDeleteOrphanCreated || second.Outcome != StartBlockDeleteOrphanSameAuthority {
		t.Fatalf("repeated publication outcomes = (%s, %s), want created/same_authority", first.Outcome, second.Outcome)
	}
	if err := store.DeleteS3OrphanRecoveryRoot(orgID, blockID, authority.Authority()); err != nil {
		t.Fatalf("remove recovery root fixture: %v", err)
	}
	replayed := store.StartBlockDeleteOrphan(orgID, blockID, authority, "sha1-replayed-again", firstSeenAt.Add(2*time.Second))
	if replayed.Outcome != StartBlockDeleteOrphanSameAuthority {
		t.Fatalf("replay after missing root = %s: %v, want same_authority", replayed.Outcome, replayed.Cause)
	}
	orphan, found, err := store.GetS3OrphanExact(orgID, blockID, authority.Authority())
	if err != nil || !found {
		t.Fatalf("exact replay read = found:%v err:%v", found, err)
	}
	if orphan.ExternalSHA1 != "sha1-original" || !orphan.FirstSeenAt.Equal(first.FirstSeenAt) {
		t.Fatalf("replay changed canonical row: %+v", orphan)
	}
	if got := g1RootCount(t, store); got != 1 {
		t.Fatalf("replay recovery roots = %d, want one exact root", got)
	}
}

func TestG1RootWithoutCanonicalIsRetained(t *testing.T) {
	store := NewMockStore()
	storage := &MockStorageProvider{}
	worker := NewWorker(store, storage, NewQueue(store), 100, 0, false, &Stats{})
	orgID := uuid.New()
	blockID := testSHA256BlockID("g1-root-without-canonical")
	authority := testCommittedOrphanAuthorityForOrg(orgID, blockID, "hot")
	result := store.StartBlockDeleteOrphan(orgID, blockID, authority, "", time.Now().UTC().Add(-time.Hour))
	if result.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("seed root-only orphan: %s: %v", result.Outcome, result.Cause)
	}
	store.DeleteS3OrphanCanonicalForTest(orgID, blockID)

	if recovered, err := worker.RecoverS3Orphans(context.Background(), 100); err != nil || recovered != 0 {
		t.Fatalf("root-only recovery = (%d, %v), want retained root", recovered, err)
	}
	if len(storage.DeletedBlocks()) != 0 || g1RootCount(t, store) != 1 {
		t.Fatalf("root-only lifecycle was retired or deleted: deletes=%v roots=%d", storage.DeletedBlocks(), g1RootCount(t, store))
	}
}

func TestG1TerminalLifecycleCleansRootAfterCanonicalLoss(t *testing.T) {
	store := NewMockStore()
	worker := NewWorker(store, &MockStorageProvider{}, NewQueue(store), 100, 0, false, &Stats{})
	orgID := uuid.New()
	blockID := testSHA256BlockID("g1-terminal-root-cleanup")
	authority := testCommittedOrphanAuthorityForOrg(orgID, blockID, "hot")
	result := store.StartBlockDeleteOrphan(orgID, blockID, authority, "", time.Now().UTC())
	if result.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("seed terminal-root orphan: %s: %v", result.Outcome, result.Cause)
	}
	if terminated, err := store.TerminateBlockDeleteLifecycle(orgID, blockID, authority); err != nil || !terminated.ok() {
		t.Fatalf("terminate lifecycle = %+v, %v", terminated, err)
	}
	store.DeleteS3OrphanCanonicalForTest(orgID, blockID)

	recovered, err := worker.RecoverS3Orphans(context.Background(), 100)
	if err != nil || recovered != 1 {
		t.Fatalf("terminal root cleanup = (%d, %v), want one settled root", recovered, err)
	}
	if roots := g1RootCount(t, store); roots != 0 {
		t.Fatalf("terminal recovery root count = %d, want 0", roots)
	}
}

func TestG1DeleteRemovesProjectionWhenCanonicalIsAlreadyMissing(t *testing.T) {
	store := NewMockStore()
	orgID := uuid.New()
	blockID := testSHA256BlockID("g1-delete-stale-projection")
	authority := testCommittedOrphanAuthorityForOrg(orgID, blockID, "hot")
	result := store.StartBlockDeleteOrphan(orgID, blockID, authority, "", time.Now().UTC())
	if result.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("seed stale-projection orphan: %s: %v", result.Outcome, result.Cause)
	}
	store.DeleteS3OrphanCanonicalForTest(orgID, blockID)

	if err := store.DeleteS3Orphan(orgID, blockID, authority.Authority(), result.FirstSeenAt); err != nil {
		t.Fatalf("DeleteS3Orphan: %v", err)
	}
	if _, found := store.GetS3OrphanProjectionForTest(orgID, blockID, result.FirstSeenAt); found {
		t.Fatal("stale discovery projection survived exact canonical cleanup")
	}
	if roots := g1RootCount(t, store); roots != 0 {
		t.Fatalf("recovery roots after exact cleanup = %d, want 0", roots)
	}
}

func TestG1PreparedRecoveryStateIsRetainedWithoutPhysicalDelete(t *testing.T) {
	store := NewMockStore()
	storage := &MockStorageProvider{}
	worker := NewWorker(store, storage, NewQueue(store), 100, 0, false, &Stats{})
	orgID := uuid.New()
	blockID := testSHA256BlockID("g1-prepared")
	authority := testCommittedOrphanAuthorityForOrg(orgID, blockID, "hot")
	result := store.StartBlockDeleteOrphan(orgID, blockID, authority, "", time.Now().UTC().Add(-time.Hour))
	if result.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("seed prepared orphan: %s: %v", result.Outcome, result.Cause)
	}
	store.SetS3OrphanRecoveryStateForTest(orgID, blockID, S3OrphanRecoveryStatePrepared)

	if recovered, err := worker.RecoverS3Orphans(context.Background(), 100); err != nil || recovered != 0 {
		t.Fatalf("prepared recovery = (%d, %v), want retained state", recovered, err)
	}
	if len(storage.DeletedBlocks()) != 0 || store.S3OrphanCount() != 1 || g1RootCount(t, store) != 1 {
		t.Fatalf("prepared lifecycle was changed: deletes=%v rows=%d roots=%d", storage.DeletedBlocks(), store.S3OrphanCount(), g1RootCount(t, store))
	}
}

func TestG1RecoveryRootPaginationUsesContinuationState(t *testing.T) {
	store := NewMockStore()
	byBucket := make(map[int][]S3OrphanRecoveryRootInfo)
	for i := 0; i < 128; i++ {
		orgID := uuid.New()
		blockID := testSHA256BlockID("g1-root-page-" + uuid.NewString())
		authority := testCommittedOrphanAuthorityForOrg(orgID, blockID, "hot").Authority()
		result := store.StartBlockDeleteOrphan(orgID, blockID, committedBlockDeleteAuthority(authority), "", time.Now().UTC())
		if result.Outcome != StartBlockDeleteOrphanCreated {
			t.Fatalf("seed paginated root %d: %s: %v", i, result.Outcome, result.Cause)
		}
		bucket := s3OrphanRecoveryRootBucket(authority)
		page, err := store.ListS3OrphanRecoveryRoots(bucket, nil, 1000)
		if err != nil {
			t.Fatalf("list seeded root bucket %d: %v", bucket, err)
		}
		byBucket[bucket] = page.Roots
		if len(page.Roots) >= 3 {
			break
		}
	}

	var bucket int
	var expected []S3OrphanRecoveryRootInfo
	for candidateBucket, roots := range byBucket {
		if len(roots) >= 3 {
			bucket = candidateBucket
			expected = roots
			break
		}
	}
	if len(expected) < 3 {
		t.Fatalf("could not seed three roots in one bucket; buckets=%v", byBucket)
	}

	var got []S3OrphanRecoveryRootInfo
	var pageState []byte
	for {
		page, err := store.ListS3OrphanRecoveryRoots(bucket, pageState, 2)
		if err != nil {
			t.Fatalf("list paginated root bucket %d: %v", bucket, err)
		}
		got = append(got, page.Roots...)
		if len(page.PageState) == 0 {
			break
		}
		pageState = page.PageState
	}
	if len(got) != len(expected) {
		t.Fatalf("paginated roots = %d, want %d", len(got), len(expected))
	}
	for i := range expected {
		if got[i].OrgID != expected[i].OrgID || got[i].BlockID != expected[i].BlockID || !got[i].Authority.sameAuthority(expected[i].Authority) {
			t.Fatalf("paginated root[%d] = %+v, want %+v", i, got[i], expected[i])
		}
	}
}

func g1RootCount(t *testing.T, store *MockStore) int {
	t.Helper()
	total := 0
	for bucket := 0; bucket < db.GCDiscoveryBucketCount; bucket++ {
		page, err := store.ListS3OrphanRecoveryRoots(bucket, nil, 100)
		if err != nil {
			t.Fatalf("list root bucket %d: %v", bucket, err)
		}
		total += len(page.Roots)
	}
	return total
}
