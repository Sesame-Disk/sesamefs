package gc

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
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
	if root := g1FindRoot(t, store, authority.Authority()); !root.FirstSeenAt.Equal(first.FirstSeenAt) {
		t.Fatalf("replay recovery root first_seen_at = %v, want stable %v", root.FirstSeenAt, first.FirstSeenAt)
	}
}

func TestG1RootOnlyReplayReusesLifecycleTokenAcrossDifferentClocks(t *testing.T) {
	store := NewMockStore()
	orgID := uuid.New()
	blockID := testSHA256BlockID("g1-root-only-replay-token")
	authority := testCommittedOrphanAuthorityForOrg(orgID, blockID, "hot")
	firstNow := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	first := store.StartBlockDeleteOrphan(orgID, blockID, authority, "sha1-original", firstNow)
	if first.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("initial publication = %s: %v", first.Outcome, first.Cause)
	}
	store.DeleteS3OrphanCanonicalForTest(orgID, blockID)

	replayed := store.StartBlockDeleteOrphan(orgID, blockID, authority, "sha1-replayed", firstNow.Add(24*time.Hour))
	if replayed.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("root-only replay = %s: %v, want canonical recreation", replayed.Outcome, replayed.Cause)
	}
	if !replayed.FirstSeenAt.Equal(first.FirstSeenAt) {
		t.Fatalf("replay token = %v, want lifecycle token %v", replayed.FirstSeenAt, first.FirstSeenAt)
	}
	canonical, found, err := store.GetS3OrphanExact(orgID, blockID, authority.Authority())
	if err != nil || !found {
		t.Fatalf("replayed canonical = found:%v err:%v", found, err)
	}
	if !canonical.FirstSeenAt.Equal(first.FirstSeenAt) {
		t.Fatalf("canonical token = %v, want %v", canonical.FirstSeenAt, first.FirstSeenAt)
	}
	if projection, found := store.GetS3OrphanProjectionForTest(orgID, blockID, first.FirstSeenAt); !found || !projection.FirstSeenAt.Equal(first.FirstSeenAt) {
		t.Fatalf("replayed projection = %+v found:%v, want token %v", projection, found, first.FirstSeenAt)
	}
	if root := g1FindRoot(t, store, authority.Authority()); !root.FirstSeenAt.Equal(first.FirstSeenAt) {
		t.Fatalf("replayed root token = %v, want %v", root.FirstSeenAt, first.FirstSeenAt)
	}
}

func TestG1RecoveryRootPublicationFailureLeavesCanonicalAbsent(t *testing.T) {
	store := NewMockStore()
	orgID := uuid.New()
	blockID := testSHA256BlockID("g1-root-publication-failure")
	authority := testCommittedOrphanAuthorityForOrg(orgID, blockID, "hot")
	rootErr := errors.New("test: recovery-root publication failed")
	store.SetStartBlockDeleteOrphanRecoveryRootErrOnceForTest(rootErr)

	result := store.StartBlockDeleteOrphan(orgID, blockID, authority, "sha1", time.Now().UTC())
	if result.Outcome == StartBlockDeleteOrphanCreated || result.Outcome == StartBlockDeleteOrphanSameAuthority {
		t.Fatalf("root publication failure outcome = %s, want failure: %v", result.Outcome, result.Cause)
	}
	if result.Cause == nil || !strings.Contains(result.Cause.Error(), rootErr.Error()) {
		t.Fatalf("root publication failure cause = %v, want %v", result.Cause, rootErr)
	}
	if store.S3OrphanCount() != 0 {
		t.Fatalf("canonical orphan count after root publication failure = %d, want 0", store.S3OrphanCount())
	}
	if roots := g1RootCount(t, store); roots != 0 {
		t.Fatalf("recovery roots after root publication failure = %d, want 0", roots)
	}
}

func TestG1ConcurrentPublicationUsesOneLifecycleToken(t *testing.T) {
	store := NewMockStore()
	orgID := uuid.New()
	blockID := testSHA256BlockID("g1-concurrent-publication-token")
	authority := testCommittedOrphanAuthorityForOrg(orgID, blockID, "hot")
	firstNow := time.Now().UTC().Truncate(time.Millisecond)
	results := make([]StartBlockDeleteOrphanResult, 2)
	var wg sync.WaitGroup
	for i, now := range []time.Time{firstNow, firstNow.Add(time.Hour)} {
		wg.Add(1)
		go func(i int, now time.Time) {
			defer wg.Done()
			results[i] = store.StartBlockDeleteOrphan(orgID, blockID, authority, "", now)
		}(i, now)
	}
	wg.Wait()
	for i, result := range results {
		if result.Outcome != StartBlockDeleteOrphanCreated && result.Outcome != StartBlockDeleteOrphanSameAuthority {
			t.Fatalf("concurrent publication[%d] = %s: %v", i, result.Outcome, result.Cause)
		}
	}
	if results[0].FirstSeenAt.IsZero() || !results[0].FirstSeenAt.Equal(results[1].FirstSeenAt) {
		t.Fatalf("concurrent lifecycle tokens = %v and %v, want one stable value", results[0].FirstSeenAt, results[1].FirstSeenAt)
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
	if _, found := store.GetS3OrphanProjectionForTest(orgID, blockID, result.FirstSeenAt); found {
		t.Fatal("terminal root cleanup removed the root but left the exact discovery projection")
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

func TestG1MockDeleteS3OrphanDoesNotTreatStaleFirstSeenAsAuthority(t *testing.T) {
	store := NewMockStore()
	orgID := uuid.New()
	blockID := testSHA256BlockID("g1-stale-first-seen")
	authority := testCommittedOrphanAuthorityForOrg(orgID, blockID, "hot")
	result := store.StartBlockDeleteOrphan(orgID, blockID, authority, "", time.Now().UTC())
	if result.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("seed stale-first-seen orphan: %s: %v", result.Outcome, result.Cause)
	}
	if err := store.DeleteS3Orphan(orgID, blockID, authority.Authority(), result.FirstSeenAt.Add(time.Hour)); err != nil {
		t.Fatalf("DeleteS3Orphan with stale first_seen_at: %v", err)
	}
	if store.S3OrphanCount() != 0 {
		t.Fatal("stale first_seen_at prevented exact canonical deletion in mock")
	}
	if g1RootCount(t, store) != 0 {
		t.Fatal("stale first_seen_at prevented exact root deletion in mock")
	}
	if _, found := store.GetS3OrphanProjectionForTest(orgID, blockID, result.FirstSeenAt); found {
		t.Fatal("stale first_seen_at left the canonical discovery projection in mock")
	}
}

func TestG1MockDeleteS3OrphanRetainsMetadataWithoutCanonicalOrToken(t *testing.T) {
	store := NewMockStore()
	orgID := uuid.New()
	blockID := testSHA256BlockID("g1-missing-canonical-zero-token")
	authority := testCommittedOrphanAuthorityForOrg(orgID, blockID, "hot")
	result := store.StartBlockDeleteOrphan(orgID, blockID, authority, "", time.Now().UTC())
	if result.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("seed missing-canonical orphan: %s: %v", result.Outcome, result.Cause)
	}
	store.DeleteS3OrphanCanonicalForTest(orgID, blockID)

	if err := store.DeleteS3Orphan(orgID, blockID, authority.Authority(), time.Time{}); err == nil {
		t.Fatal("DeleteS3Orphan without canonical or token succeeded")
	}
	if _, found := store.GetS3OrphanProjectionForTest(orgID, blockID, result.FirstSeenAt); !found {
		t.Fatal("unknown first_seen_at removed the exact discovery projection")
	}
	if roots := g1RootCount(t, store); roots != 1 {
		t.Fatalf("unknown first_seen_at removed the recovery root: roots=%d", roots)
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
	replayed := store.StartBlockDeleteOrphan(orgID, blockID, authority, "", time.Now().UTC())
	if replayed.Outcome == StartBlockDeleteOrphanCreated || replayed.Outcome == StartBlockDeleteOrphanSameAuthority {
		t.Fatalf("PREPARED replay returned destructive publication outcome %s", replayed.Outcome)
	}
}

func TestG1TerminalRootSettlementIsPageBoundedAndExact(t *testing.T) {
	store := NewMockStore()
	worker := NewWorker(store, &MockStorageProvider{}, NewQueue(store), 100, 0, false, &Stats{})
	var seeded []struct {
		orgID     uuid.UUID
		blockID   string
		authority CommittedBlockDeleteAuthority
		firstSeen time.Time
	}
	var selectedBucket = -1
	for i := 0; i < 512 && len(seeded) < 5; i++ {
		orgID := uuid.New()
		blockID := testSHA256BlockID("g1-terminal-page-" + uuid.NewString())
		authority := testCommittedOrphanAuthorityForOrg(orgID, blockID, "hot")
		bucket := s3OrphanRecoveryRootBucket(authority.Authority())
		if selectedBucket >= 0 && bucket != selectedBucket {
			continue
		}
		if selectedBucket < 0 {
			selectedBucket = bucket
		}
		firstSeen := time.Now().UTC().Truncate(time.Millisecond)
		created := store.StartBlockDeleteOrphan(orgID, blockID, authority, "", firstSeen)
		if created.Outcome != StartBlockDeleteOrphanCreated {
			t.Fatalf("seed terminal root %d: %s: %v", i, created.Outcome, created.Cause)
		}
		if terminated, err := store.TerminateBlockDeleteLifecycle(orgID, blockID, authority); err != nil || !terminated.ok() {
			t.Fatalf("terminate root %d: %+v: %v", i, terminated, err)
		}
		store.DeleteS3OrphanCanonicalForTest(orgID, blockID)
		seeded = append(seeded, struct {
			orgID     uuid.UUID
			blockID   string
			authority CommittedBlockDeleteAuthority
			firstSeen time.Time
		}{orgID, blockID, authority, created.FirstSeenAt})
	}
	if len(seeded) != 5 {
		t.Fatalf("seeded %d terminal roots in one bucket, want 5", len(seeded))
	}

	recovered, err := worker.RecoverS3Orphans(context.Background(), 2)
	if err != nil || recovered != len(seeded) {
		t.Fatalf("page-bounded terminal settlement = (%d, %v), want (%d, nil)", recovered, err, len(seeded))
	}
	for _, item := range seeded {
		if _, found := store.GetS3OrphanProjectionForTest(item.orgID, item.blockID, item.firstSeen); found {
			t.Fatalf("terminal projection survived for %s", item.blockID)
		}
	}
	if got := g1RootCount(t, store); got != 0 {
		t.Fatalf("terminal roots after paginated settlement = %d, want 0", got)
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

func TestG1OldRootRepairsDiscoveryWithoutRewindingHistoricalScan(t *testing.T) {
	store := NewMockStore()
	storage := &MockStorageProvider{}
	worker := NewWorker(store, storage, NewQueue(store), 100, 0, false, &Stats{})
	orgID := uuid.New()
	blockID := testSHA256BlockID("g1-old-root")
	authority := testCommittedOrphanAuthorityForOrg(orgID, blockID, "hot")
	firstSeenAt := time.Now().UTC().AddDate(0, 0, -120)
	created := store.StartBlockDeleteOrphan(orgID, blockID, authority, "", firstSeenAt)
	if created.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("seed old root: %s: %v", created.Outcome, created.Cause)
	}
	store.DeleteS3OrphanProjectionForTest(orgID, blockID, created.FirstSeenAt)

	recovered, err := worker.RecoverS3Orphans(context.Background(), 100)
	if err != nil || recovered != 0 {
		t.Fatalf("old-root sweep = (%d, %v), want root repair without historical rewind", recovered, err)
	}
	if len(storage.DeletedBlocks()) != 0 {
		t.Fatalf("old root triggered physical recovery during bounded scheduling: %v", storage.DeletedBlocks())
	}
	if _, found := store.GetS3OrphanProjectionForTest(orgID, blockID, created.FirstSeenAt); !found {
		t.Fatal("old root did not repair its exact discovery projection")
	}
}

func TestG1RootScanReturnsUTCProjectionDay(t *testing.T) {
	store := NewMockStore()
	worker := NewWorker(store, nil, NewQueue(store), 100, 0, false, &Stats{})
	orgID := uuid.New()
	blockID := testSHA256BlockID("g1-root-scan-day")
	authority := testCommittedOrphanAuthorityForOrg(orgID, blockID, "hot")
	firstSeenAt := time.Date(2026, 9, 7, 23, 59, 59, 999000000, time.UTC)
	created := store.StartBlockDeleteOrphan(orgID, blockID, authority, "", firstSeenAt)
	if created.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatalf("seed root scan token: %s: %v", created.Outcome, created.Cause)
	}
	cutoffDay := db.GCProjectionUTCDate(firstSeenAt.Add(24 * time.Hour))
	_, err, rootScanStart, _ := worker.reconcileS3OrphanRecoveryRoots(context.Background(), 100, cutoffDay)
	if err != nil {
		t.Fatalf("reconcile roots: %v", err)
	}
	if want := db.GCProjectionUTCDate(firstSeenAt); !rootScanStart.Equal(want) {
		t.Fatalf("root scan start = %v, want UTC projection day %v", rootScanStart, want)
	}
}

func TestG1RootErrorDoesNotFreezeByDayCursor(t *testing.T) {
	store := NewMockStore()
	worker := NewWorker(store, &MockStorageProvider{}, NewQueue(store), 100, 0, false, &Stats{})
	rootErr := errors.New("test: recovery-root enumeration unavailable")
	store.SetListS3OrphanRecoveryRootsErrForTest(rootErr)

	cutoffDay := db.GCProjectionUTCDate(time.Now().UTC())
	previousCursor := db.GCProjectionDateString(cutoffDay.AddDate(0, 0, -2))
	if err := store.SaveGCStats(gcS3OrphansCursorKey, previousCursor); err != nil {
		t.Fatalf("seed recovery cursor: %v", err)
	}
	_, err := worker.RecoverS3Orphans(context.Background(), 100)
	if err == nil || !strings.Contains(err.Error(), rootErr.Error()) {
		t.Fatalf("RecoverS3Orphans error = %v, want root error", err)
	}
	cursor, cursorErr := store.LoadGCStats(gcS3OrphansCursorKey)
	wantCursor := db.GCProjectionDateString(cutoffDay.AddDate(0, 0, -1))
	if cursorErr != nil || cursor != wantCursor {
		t.Fatalf("cursor after root-only error = %q, err=%v, recovery err=%v, want %q", cursor, cursorErr, err, wantCursor)
	}
}

func TestG1SourceContractsKeepRootBeforeCanonicalAndSettlementBounded(t *testing.T) {
	source, err := os.ReadFile("store_cassandra.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start := strings.Index(text, "func (s *CassandraStore) StartBlockDeleteOrphan")
	if start < 0 {
		t.Fatal("StartBlockDeleteOrphan source not found")
	}
	end := strings.Index(text[start:], "\n}\n")
	if end < 0 {
		t.Fatal("StartBlockDeleteOrphan source boundary not found")
	}
	startBody := text[start : start+end]
	if rootAt, canonicalAt := strings.Index(startBody, "publishS3OrphanRecoveryRoot"), strings.Index(startBody, "INSERT INTO gc_s3_orphans"); rootAt < 0 || canonicalAt < 0 || rootAt > canonicalAt {
		t.Fatalf("recovery root must publish before canonical insert: root=%d canonical=%d", rootAt, canonicalAt)
	}
	rootPublication := formattedGCFunction(t, parseGCStoreFile(t), "publishS3OrphanRecoveryRoot")
	if strings.Contains(rootPublication, "SELECT first_seen_at") {
		t.Fatal("recovery-root publication must not read first_seen_at; the lifecycle token is authoritative")
	}
	if !strings.Contains(rootPublication, "INSERT INTO gc_s3_orphan_recovery_roots") {
		t.Fatal("recovery-root publication must write the durable root")
	}
	deleteBody := formattedGCFunction(t, parseGCStoreFile(t), "DeleteS3Orphan")
	canonicalDeleteAt := strings.Index(deleteBody, "DELETE FROM gc_s3_orphans\n")
	projectionDeleteAt := strings.Index(deleteBody, "DELETE FROM gc_s3_orphans_by_day")
	if canonicalDeleteAt < 0 || projectionDeleteAt < canonicalDeleteAt {
		t.Fatal("exact canonical orphan delete query not found")
	}
	canonicalDelete := deleteBody[canonicalDeleteAt:projectionDeleteAt]
	if !strings.Contains(canonicalDelete, "gc_claim_id = ? AND gc_claimed_at = ?") {
		t.Fatal("canonical orphan delete must include the exact D identity")
	}
	workerSource, err := os.ReadFile("worker.go")
	if err != nil {
		t.Fatal(err)
	}
	workerText := string(workerSource)
	if !strings.Contains(workerText, "ListS3OrphanRecoveryRoots(bucket, pageState, pageSize)") {
		t.Fatal("root reconciliation must pass its bounded page size to the store")
	}
	if strings.Contains(workerText, "settledRoots") || strings.Contains(workerText, "earliestRootCanonical") {
		t.Fatal("root reconciliation must not accumulate a bucket or rewind the historical discovery scan")
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

func g1FindRoot(t *testing.T, store *MockStore, authority BlockDeleteAuthority) S3OrphanRecoveryRootInfo {
	t.Helper()
	page, err := store.ListS3OrphanRecoveryRoots(s3OrphanRecoveryRootBucket(authority), nil, 100)
	if err != nil {
		t.Fatalf("list recovery root: %v", err)
	}
	for _, root := range page.Roots {
		if root.Authority.sameAuthority(authority) {
			return root
		}
	}
	t.Fatalf("recovery root for authority %+v not found", authority)
	return S3OrphanRecoveryRootInfo{}
}
