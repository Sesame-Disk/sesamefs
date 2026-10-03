package gc

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/google/uuid"
)

func TestG5CommittedRecoveryIgnoresDayScheduling(t *testing.T) {
	for _, cursor := range []string{"missing", "future", "corrupt", "passed"} {
		t.Run(cursor, func(t *testing.T) {
			store := NewMockStore()
			sp := &MockStorageProvider{}
			org := uuid.New()
			block := testSHA256BlockID("g5-" + cursor)
			now := time.Now().UTC().Truncate(time.Millisecond)
			old := now.AddDate(0, 0, -120)
			store.AddBlock(org, block, "hot", 0)
			store.EnqueueBlockForTest(org, old.Add(-time.Hour), block, "hot", 0)
			producer := NewWorker(store, sp, NewQueue(store), 100, 0, false, &Stats{})
			producer.clock = func() time.Time { return old }
			if n, err := producer.ProcessOnce(context.Background()); err != nil || n != 1 {
				t.Fatalf("handoff: %d %v", n, err)
			}
			rows := store.AllS3Orphans()
			if len(rows) != 1 || rows[0].RecoveryState != S3OrphanRecoveryStateCommitted {
				t.Fatalf("current COMMITTED fixture: %+v", rows)
			}
			p1 := rows[0]
			store.DeleteS3OrphanProjectionForTest(org, block, p1.FirstSeenAt)
			switch cursor {
			case "future":
				_ = store.SaveGCStats(gcS3OrphansCursorKey, db.GCProjectionDateString(now.AddDate(0, 0, 20)))
			case "corrupt":
				_ = store.SaveGCStats(gcS3OrphansCursorKey, "not-a-day")
			case "passed":
				_ = store.SaveGCStats(gcS3OrphansCursorKey, db.GCProjectionDateString(now))
			}
			p2key := MockCanonicalStorageKey(org.String(), block) + "." + uuid.NewString()
			store.AddBlock(org, block, "hot", 1)
			store.SetBlockStorageKeyForTest(org, block, p2key)
			restarted := NewWorker(store, sp, NewQueue(store), 100, 0, false, &Stats{})
			restarted.clock = func() time.Time { return now }
			if n, err := restarted.RecoverS3Orphans(context.Background(), 2); err != nil || n != 1 {
				t.Fatalf("120-day root must converge independent of %s cursor: recovered=%d err=%v", cursor, n, err)
			}
			deletes := sp.ScopedBlockDeletes()
			if len(deletes) != 1 || deletes[0].StorageKey != p1.StorageKey {
				t.Fatalf("exact K1 only: %+v", deletes)
			}
			if got := store.GetBlock(org, block); got == nil || got.StorageKey != p2key {
				t.Fatalf("P2 changed: %+v", got)
			}
			if store.S3OrphanCount() != 0 || g1RootCount(t, store) != 0 {
				t.Fatal("completed D1 retained exact recovery state")
			}
		})
	}
}

// Every fixture crosses the real PREPARED -> COMMITTED -> retirement protocol.
func g5SeedRoot(t *testing.T, store *MockStore, bucket int, at time.Time) S3OrphanInfo {
	t.Helper()
	org := uuid.New()
	block := testSHA256BlockID(uuid.NewString())
	store.AddBlock(org, block, "hot", 0)
	var authority BlockDeleteAuthority
	for i := 0; i < 10000; i++ {
		authority = BlockDeleteAuthority{Target: BlockDeleteTarget{"hot", MockCanonicalStorageKey(org.String(), block)}, ClaimID: fmt.Sprintf("g5-%d", i), ClaimedAt: at}
		if s3OrphanRecoveryRootBucket(authority) == bucket {
			break
		}
	}
	if s3OrphanRecoveryRootBucket(authority) != bucket {
		t.Fatal("fixture bucket not found")
	}
	authority = store.SeedBlockClaimForTest(org, block, authority.ClaimID, at)
	if r := store.PrepareBlockDeleteOrphan(org, block, authority, "", at); r.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatal(r)
	}
	if r, err := store.CommitBlockDeleteOrphanHandoff(org, block, authority); err != nil || r.Outcome != BlockDeleteHandoffCommitted {
		t.Fatalf("commit: %+v %v", r, err)
	}
	if r := store.PromoteBlockDeleteOrphan(org, block, committedBlockDeleteAuthority(authority)); r.Outcome != StartBlockDeleteOrphanCreated {
		t.Fatal(r)
	}
	if r, err := store.FinalizeBlockDelete(org, block, committedBlockDeleteAuthority(authority)); err != nil || r.Outcome != BlockDeleteFinalized {
		t.Fatalf("retire: %+v %v", r, err)
	}
	orphan, found, err := store.GetS3OrphanExact(org, block, authority)
	if err != nil || !found {
		t.Fatalf("canonical: %v %v", found, err)
	}
	return orphan
}

type g5PoisonedRootsStore struct {
	*MockStore
	poison  map[string]bool
	calls   map[int]int
	maxPage int
}

func (s *g5PoisonedRootsStore) GetS3OrphanExact(org uuid.UUID, block string, authority BlockDeleteAuthority) (S3OrphanInfo, bool, error) {
	if s.poison[block] {
		return S3OrphanInfo{}, false, errors.New("permanent fixture read failure")
	}
	return s.MockStore.GetS3OrphanExact(org, block, authority)
}
func (s *g5PoisonedRootsStore) ListS3OrphanRecoveryRoots(bucket int, state []byte, limit int) (S3OrphanRecoveryRootPage, error) {
	s.calls[bucket]++
	page, err := s.MockStore.ListS3OrphanRecoveryRoots(bucket, state, limit)
	if len(page.Roots) > s.maxPage {
		s.maxPage = len(page.Roots)
	}
	return page, err
}
func TestG5FailedPrefixDoesNotStarveAfterRestart(t *testing.T) {
	base := NewMockStore()
	now := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	store := &g5PoisonedRootsStore{MockStore: base, poison: map[string]bool{}, calls: map[int]int{}}
	for i := 0; i < 3; i++ {
		row := g5SeedRoot(t, base, 0, now.Add(time.Duration(i)*time.Second))
		store.poison[row.BlockID] = true
	}
	healthy := g5SeedRoot(t, base, 0, now.Add(3*time.Second))
	sp := &MockStorageProvider{}
	for tick := 0; tick < 2; tick++ {
		worker := NewWorker(store, sp, NewQueue(store), 100, 0, false, &Stats{})
		n, err := worker.RecoverS3Orphans(context.Background(), 2)
		if err == nil || n != tick {
			t.Fatalf("tick %d: recovered=%d err=%v", tick, n, err)
		}
	}
	if got := sp.ScopedBlockDeletes(); len(got) != 1 || got[0].StorageKey != healthy.StorageKey {
		t.Fatalf("healthy tail not recovered exactly: %+v", got)
	}
	if store.maxPage != 2 {
		t.Fatalf("page bound: %d", store.maxPage)
	}
	for bucket := 0; bucket < db.GCDiscoveryBucketCount; bucket++ {
		if store.calls[bucket] != 2 {
			t.Fatalf("bucket %d: %d calls", bucket, store.calls[bucket])
		}
	}
	if g1RootCount(t, base) != 3 {
		t.Fatal("failed authorities lost")
	}
	// Complete the finite cycle, then revisit the prefix after the fault is fixed.
	store.poison = map[string]bool{}
	if n, err := NewWorker(store, sp, NewQueue(store), 100, 0, false, &Stats{}).RecoverS3Orphans(context.Background(), 2); err != nil || n != 2 {
		t.Fatalf("wrapped prefix: %d %v", n, err)
	}
}

func TestG5SeekSurvivesDeletionAndFiniteCycleConcurrentArrivals(t *testing.T) {
	store := NewMockStore()
	now := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	first := g5SeedRoot(t, store, 0, now)
	second := g5SeedRoot(t, store, 0, now.Add(time.Second))
	originalTail := g5SeedRoot(t, store, 0, now.Add(2*time.Second))
	page, err := store.ListS3OrphanRecoveryRoots(0, nil, 2)
	if err != nil || len(page.Roots) != 2 || len(page.PageState) == 0 {
		t.Fatalf("first page: %+v %v", page, err)
	}
	// Other executors delete both rows, including the exact seek row.
	for _, row := range []S3OrphanInfo{first, second} {
		if _, err := store.TerminateBlockDeleteLifecycle(row.OrgID, row.BlockID, committedBlockDeleteAuthority(row.Authority)); err != nil {
			t.Fatal(err)
		}
		if err := store.DeleteS3Orphan(row.OrgID, row.BlockID, row.Authority, row.FirstSeenAt); err != nil {
			t.Fatal(err)
		}
	}
	behind := g5SeedRoot(t, store, 0, now.Add(-time.Second))
	beyond := g5SeedRoot(t, store, 0, now.Add(3*time.Second))
	resumed, err := store.ListS3OrphanRecoveryRoots(0, page.PageState, 2)
	if err != nil || len(resumed.Roots) != 1 || resumed.Roots[0].BlockID != originalTail.BlockID || len(resumed.PageState) != 0 {
		t.Fatalf("finite resumed page: %+v %v", resumed, err)
	}
	wrapped, err := store.ListS3OrphanRecoveryRoots(0, nil, 10)
	if err != nil || len(wrapped.Roots) != 3 || wrapped.Roots[0].BlockID != behind.BlockID || wrapped.Roots[2].BlockID != beyond.BlockID {
		t.Fatalf("arrivals lost at wrap: %+v %v", wrapped, err)
	}
}

func TestG5CorruptRootCheckpointIsRepairedWithoutAuthority(t *testing.T) {
	store := NewMockStore()
	row := g5SeedRoot(t, store, 0, time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond))
	if err := store.SaveGCStats("gc.scan.s3_roots.bucket.00", "corrupt"); err != nil {
		t.Fatal(err)
	}
	sp := &MockStorageProvider{}
	n, err := NewWorker(store, sp, NewQueue(store), 100, 0, false, &Stats{}).RecoverS3Orphans(context.Background(), 2)
	if err != nil || n != 1 {
		t.Fatalf("corrupt checkpoint repair: %d %v", n, err)
	}
	if got := sp.ScopedBlockDeletes(); len(got) != 1 || got[0].StorageKey != row.StorageKey {
		t.Fatalf("wrong authority: %+v", got)
	}
	if value, err := store.LoadGCStats("gc.scan.s3_roots.bucket.00"); err != nil || value != "" {
		t.Fatalf("checkpoint not repaired: %q %v", value, err)
	}
}

// Faults in auxiliary scheduling never manufacture or revoke exact P,D authority.
type g5SchedulingFaultStore struct {
	*MockStore
	failSave       bool
	failLoad       bool
	failProjection bool
	cancel         context.CancelFunc
	reads          int
}

func (s *g5SchedulingFaultStore) SaveGCStats(key, value string) error {
	if s.failSave {
		return errors.New("checkpoint write unavailable")
	}
	return s.MockStore.SaveGCStats(key, value)
}
func (s *g5SchedulingFaultStore) LoadGCStats(key string) (string, error) {
	if s.failLoad {
		return "", errors.New("checkpoint read unavailable")
	}
	return s.MockStore.LoadGCStats(key)
}
func (s *g5SchedulingFaultStore) PublishS3OrphanDiscovery(org uuid.UUID, block string, authority BlockDeleteAuthority, firstSeen time.Time) error {
	if s.failProjection {
		return errors.New("projection unavailable")
	}
	return s.MockStore.PublishS3OrphanDiscovery(org, block, authority, firstSeen)
}
func (s *g5SchedulingFaultStore) GetS3OrphanExact(org uuid.UUID, block string, authority BlockDeleteAuthority) (S3OrphanInfo, bool, error) {
	row, found, err := s.MockStore.GetS3OrphanExact(org, block, authority)
	s.reads++
	if s.cancel != nil && s.reads == 2 {
		s.cancel()
	}
	return row, found, err
}
func TestG5AuxiliarySchedulingFailuresCannotBlockExactExecution(t *testing.T) {
	for _, fault := range []string{"checkpoint-read", "checkpoint-write", "projection"} {
		t.Run(fault, func(t *testing.T) {
			base := NewMockStore()
			row := g5SeedRoot(t, base, 0, time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond))
			store := &g5SchedulingFaultStore{MockStore: base, failLoad: fault == "checkpoint-read", failSave: fault == "checkpoint-write", failProjection: fault == "projection"}
			sp := &MockStorageProvider{}
			n, err := NewWorker(store, sp, NewQueue(store), 100, 0, false, &Stats{}).RecoverS3Orphans(context.Background(), 2)
			if n != 1 || err == nil {
				t.Fatalf("observable auxiliary failure and independent recovery: %d %v", n, err)
			}
			if got := sp.ScopedBlockDeletes(); len(got) != 1 || got[0].StorageKey != row.StorageKey {
				t.Fatalf("exact K1: %+v", got)
			}
			if g1RootCount(t, base) != 0 {
				t.Fatal("completed authority retained")
			}
		})
	}
}
func TestG5CancellationDoesNotCheckpointAnUnattemptedRoot(t *testing.T) {
	base := NewMockStore()
	now := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	first := g5SeedRoot(t, base, 0, now)
	second := g5SeedRoot(t, base, 0, now.Add(time.Second))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &g5SchedulingFaultStore{MockStore: base, cancel: cancel}
	sp := &MockStorageProvider{}
	n, err := NewWorker(store, sp, NewQueue(store), 100, 0, false, &Stats{}).RecoverS3Orphans(ctx, 2)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not surfaced: %d %v", n, err)
	}
	if _, err := base.LoadGCStats("gc.scan.s3_roots.bucket.00"); err == nil {
		t.Fatal("partial page checkpointed")
	}
	if _, found, err := base.GetS3OrphanExact(second.OrgID, second.BlockID, second.Authority); err != nil || !found {
		t.Fatalf("unattempted root lost: %v %v", found, err)
	}
	// The mock storage ignores cancellation; the completed first identity is safe.
	store.cancel = nil
	if _, err := NewWorker(store, sp, NewQueue(store), 100, 0, false, &Stats{}).RecoverS3Orphans(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	if g1RootCount(t, base) != 0 {
		t.Fatal("restart did not recover retained authority")
	}
	deletes := sp.ScopedBlockDeletes()
	seen := map[string]bool{}
	for _, d := range deletes {
		seen[d.StorageKey] = true
	}
	if !seen[first.StorageKey] || !seen[second.StorageKey] {
		t.Fatalf("exact restart completion: %+v", deletes)
	}
}

// A new zero-ref transition on the same physical life must serve a fresh grace.
// Observing liveness before that transition cannot donate an expired candidate age.
func TestG5NewZeroEpochDoesNotInheritReferencedCandidateGrace(t *testing.T) {
	for _, postClaim := range []bool{false, true} {
		t.Run(fmt.Sprintf("post_claim_%v", postClaim), func(t *testing.T) {
			store := NewMockStore()
			org := uuid.New()
			block := testSHA256BlockID("g5-zero-epoch")
			grace := time.Hour
			liveAt := time.Now().UTC().Add(-2 * grace).Truncate(time.Millisecond)
			oldAt := liveAt.Add(-24 * grace)
			store.AddBlock(org, block, "hot", 0)
			if err := store.EnqueueBlockForTest(org, oldAt, block, "hot", 0); err != nil {
				t.Fatal(err)
			}
			item := store.QueueItems(org)[0]
			reads := 0
			zero := false
			store.SetBlockHasReferencesHookForTest(func(_ uuid.UUID, _ string, _ bool) (bool, error) {
				reads++
				return !zero && (!postClaim || reads > 1), nil
			})
			storage := &MockStorageProvider{}
			worker := NewWorker(store, storage, NewQueue(store), 100, grace, false, &Stats{})
			worker.clock = func() time.Time { return liveAt }
			_ = worker.processBlock(context.Background(), item)
			zeroAt := liveAt.Add(grace - time.Second)
			zero = true
			candidate, err := store.EnsureBlockGCCandidateExact(org, block, "hot", zeroAt)
			if err != nil {
				t.Fatal(err)
			}
			item.BlockGCCandidateIdentity = candidate.Identity()
			item.IdentityAt = candidate.CandidateAt
			item.QueuedAt = liveAt // Even an already-eligible queue row cannot bypass candidate grace.
			claimsBefore := len(store.ClaimAttemptsForTest())
			worker.clock = func() time.Time { return liveAt.Add(grace) }
			err = worker.processBlock(context.Background(), item)
			if failureCodeForError(err) != GCFailureCodeBlockCandidateWithinGrace || len(store.ClaimAttemptsForTest()) != claimsBefore || store.S3OrphanCount() != 0 {
				t.Fatalf("new zero epoch bypassed grace: candidate_at=%v zero_at=%v err=%v claims=%d", candidate.CandidateAt, zeroAt, err, len(store.ClaimAttemptsForTest())-claimsBefore)
			}
			worker.clock = func() time.Time { return zeroAt.Add(grace + time.Second) }
			if err := worker.processBlock(context.Background(), item); err != nil {
				t.Fatalf("eligible zero epoch: %v", err)
			}
			if store.S3OrphanCount() != 1 {
				t.Fatal("full fresh grace must permit the current handoff")
			}
		})
	}
}
