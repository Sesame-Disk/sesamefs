package gc

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Library trash contract: a library cascade may destroy a library only while the
// canonical deleted_at still equals the exact trash generation D that produced
// the cascade. Restored (deleted_at == null) or superseded (deleted_at == D')
// work is stale and stops without destroying anything.

func newLibraryTrashBoundaryFixture(t *testing.T) (*MockStore, *Worker, uuid.UUID, uuid.UUID, time.Time) {
	t.Helper()
	store := NewMockStore()
	w := NewWorker(store, nil, NewQueue(store), 100, 0, false, &Stats{})
	orgID, libID := uuid.New(), uuid.New()
	deletedAt := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Millisecond)
	store.AddOrganization(orgID)
	store.AddDeletedLibrary(orgID, libID, "hot", deletedAt)
	store.AddCommit(libID, "commit-1", "fs-root")
	store.AddFSObject(libID, "fs-root", "dir", nil)
	return store, w, orgID, libID, deletedAt
}

// drainLibraryTrashBoundaryQueue runs the worker until the org queue is empty so
// the guarded children enqueued by the parent cascade get their turn too.
func drainLibraryTrashBoundaryQueue(t *testing.T, store *MockStore, w *Worker, orgID uuid.UUID) {
	t.Helper()
	for i := 0; i < 10 && len(store.QueueItems(orgID)) > 0; i++ {
		if _, err := w.ProcessOnce(context.Background()); err != nil {
			t.Fatalf("ProcessOnce failed: %v", err)
		}
	}
	if items := store.QueueItems(orgID); len(items) != 0 {
		t.Fatalf("queue did not drain: %d items left", len(items))
	}
}

func assertLibraryTrashBoundaryLibraryIntact(t *testing.T, store *MockStore, orgID, libID uuid.UUID) {
	t.Helper()
	if exists, err := store.CanonicalLibraryExists(orgID, libID); err != nil || !exists {
		t.Fatalf("restored library canonical row must survive the stale cascade (exists=%v err=%v)", exists, err)
	}
	if store.GetCommitRecord(libID, "commit-1") == nil {
		t.Fatal("restored library commit must survive the stale cascade")
	}
	if _, err := store.GetFSObject(libID, "fs-root"); err != nil {
		t.Fatal("restored library fs_object must survive the stale cascade")
	}
	if store.deleteLibraryStorageCounterFor[libID] != 0 {
		t.Fatalf("restored library storage counter must not be deleted, got %d deletes", store.deleteLibraryStorageCounterFor[libID])
	}
	for _, entry := range store.AuditLogEntries() {
		if entry.Action == "gc_library_cascade_deleted" {
			t.Fatal("stale cascade must not record gc_library_cascade_deleted")
		}
	}
}

// The worker passes every stale check and fences its lease, then pauses past the
// stale threshold. A restore steals the lease and clears deleted_at. When the old
// worker resumes, its final destructive step must lose to the restore.
func TestLibraryTrashBoundary_RestoreAfterFenceWins(t *testing.T) {
	store, w, orgID, libID, deletedAt := newLibraryTrashBoundaryFixture(t)
	var restoreOnce sync.Once
	store.renewLibraryHardDeleteLockHook = func(id uuid.UUID) {
		restoreOnce.Do(func() { store.StealLibraryLeaseAndRestoreForTest(id) })
	}
	store.SeedQueueItemForTest(orgID, deletedAt, ItemLibraryCascade, libID.String(), uuid.Nil, "hot", 0)

	drainLibraryTrashBoundaryQueue(t, store, w, orgID)

	assertLibraryTrashBoundaryLibraryIntact(t, store, orgID, libID)
}

// A restore that lands between the worker's first marker read and its lease
// acquisition is caught by the second, under-lease stale check.
func TestLibraryTrashBoundary_RestoreBetweenFirstCheckAndLeaseIsStale(t *testing.T) {
	store, w, orgID, libID, deletedAt := newLibraryTrashBoundaryFixture(t)
	var restoreOnce sync.Once
	store.acquireLibraryHardDeleteLockHook = func(id uuid.UUID) {
		restoreOnce.Do(func() { store.StealLibraryLeaseAndRestoreForTest(id) })
	}
	store.SeedQueueItemForTest(orgID, deletedAt, ItemLibraryCascade, libID.String(), uuid.Nil, "hot", 0)

	drainLibraryTrashBoundaryQueue(t, store, w, orgID)

	assertLibraryTrashBoundaryLibraryIntact(t, store, orgID, libID)
	for _, item := range store.QueueItems(orgID) {
		t.Fatalf("stale cascade must not enqueue children, found %s/%s", item.ItemType, item.ItemID)
	}
}

// A cascade queued for generation D must not purge the library after it was
// restored and trashed again under a newer generation D'.
func TestLibraryTrashBoundary_SupersededGenerationIsStale(t *testing.T) {
	store, w, orgID, libID, deletedAt := newLibraryTrashBoundaryFixture(t)
	redeletedAt := deletedAt.Add(time.Hour)
	store.AddDeletedLibrary(orgID, libID, "hot", redeletedAt)
	store.SeedQueueItemForTest(orgID, deletedAt, ItemLibraryCascade, libID.String(), uuid.Nil, "hot", 0)

	drainLibraryTrashBoundaryQueue(t, store, w, orgID)

	assertLibraryTrashBoundaryLibraryIntact(t, store, orgID, libID)
	marker, err := store.GetLibraryDeletedAt(libID)
	if err != nil || marker == nil || !marker.Equal(redeletedAt) {
		t.Fatalf("generation D' marker must be untouched by the D cascade, got %v (err=%v)", marker, err)
	}
}

// A restore that won its CAS and crashed before removing the GC marker leaves the
// canonical row ACTIVE with the marker still at D. The cascade for D must treat
// the canonical generation as authoritative: stale, clear marker D, stop.
func TestLibraryTrashBoundary_StaleMarkerOverRestoredCanonicalIsSettled(t *testing.T) {
	store, w, orgID, libID, deletedAt := newLibraryTrashBoundaryFixture(t)
	store.mu.Lock()
	store.libraries[libID].DeletedAt = time.Time{}
	store.mu.Unlock()
	store.SeedQueueItemForTest(orgID, deletedAt, ItemLibraryCascade, libID.String(), uuid.Nil, "hot", 0)

	drainLibraryTrashBoundaryQueue(t, store, w, orgID)

	assertLibraryTrashBoundaryLibraryIntact(t, store, orgID, libID)
	if marker, err := store.GetLibraryDeletedAt(libID); err != nil || marker != nil {
		t.Fatalf("stale marker D over a restored canonical row must be cleared, got %v (err=%v)", marker, err)
	}
}

// An ambiguous restore CAS may be settled as won only while this restore still
// owns the lease: otherwise deleted_at == null may be another restore's commit.
func TestSettleAmbiguousLibraryGenerationClear(t *testing.T) {
	casErr := errors.New("cas outcome unknown")
	readNull := func() (time.Time, bool, error) { return time.Time{}, true, nil }
	readD := func() (time.Time, bool, error) { return time.Now(), true, nil }
	owned := func() error { return nil }
	lost := func() error { return errors.New("lost library restore lock") }

	if won, err := settleAmbiguousLibraryGenerationClear(casErr, lost, readNull); won || err == nil {
		t.Fatalf("lease lost: must not claim the clear (won=%v err=%v)", won, err)
	}
	if won, err := settleAmbiguousLibraryGenerationClear(casErr, owned, readD); won || err == nil {
		t.Fatalf("deleted_at still set: must not claim the clear (won=%v err=%v)", won, err)
	}
	if won, err := settleAmbiguousLibraryGenerationClear(casErr, owned, func() (time.Time, bool, error) { return time.Time{}, false, nil }); won || err == nil {
		t.Fatalf("row absent: must not claim the clear (won=%v err=%v)", won, err)
	}
	if won, err := settleAmbiguousLibraryGenerationClear(casErr, owned, readNull); !won || err != nil {
		t.Fatalf("still owner and deleted_at null: the clear is ours (won=%v err=%v)", won, err)
	}
}
