package gc

import (
	"testing"
	"time"

	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/google/uuid"
)

// PC-D1B.4 freezes one destruction token per durable QueueItem
// (docs/PC-D1B-CERTIFICATION-WINDOW-FENCE.md §10.2). These characterizations
// record why QueueItem.Identity() on main cannot be that token, so PC-D1B.5
// derives it from the explicit tuple the ADR freezes instead.

// Identity() carries only IdentityAt and the block candidate: distinct
// destruction units enqueued in the same cascade collapse onto one identity.
func TestPCD1B4Characterization_QueueItemIdentityCollidesAcrossUnits(t *testing.T) {
	orgID, libraryA, libraryB := uuid.New(), uuid.New(), uuid.New()
	identityAt := time.Unix(1_700_000_000, 0).UTC()
	units := []QueueItem{
		{OrgID: orgID, LibraryID: libraryA, ItemType: ItemCommit, ItemID: "commit-1", IdentityAt: identityAt, QueuedAt: identityAt},
		{OrgID: orgID, LibraryID: libraryA, ItemType: ItemCommit, ItemID: "commit-2", IdentityAt: identityAt, QueuedAt: identityAt},
		{OrgID: orgID, LibraryID: libraryA, ItemType: ItemFSObject, ItemID: "commit-1", IdentityAt: identityAt, QueuedAt: identityAt},
		{OrgID: orgID, LibraryID: libraryB, ItemType: ItemFSObject, ItemID: "fs-1", IdentityAt: identityAt, QueuedAt: identityAt},
	}
	for i := 1; i < len(units); i++ {
		if units[i].Identity() != units[0].Identity() {
			t.Fatalf("CURRENT: distinct destruction units share Identity() on main; unit %d now differs (%+v vs %+v). Update the PC-D1B.4 token tuple rationale", i, units[i].Identity(), units[0].Identity())
		}
	}
}

// Before persistence, a zero IdentityAt makes Identity() fall back to
// QueuedAt. This is not evidence that durable retries lose identity: enqueue
// persists the effective identity_at and the requeue path keeps that stored
// value. It does show why PC-D1B.5 must derive a token from a hydrated durable
// row rather than a pre-persistence QueueItem.
func TestPCD1B4Characterization_PrePersistenceIdentityFallsBackToQueuedAt(t *testing.T) {
	first := QueueItem{OrgID: uuid.New(), LibraryID: uuid.New(), ItemType: ItemCommit, ItemID: "commit-1", QueuedAt: time.Unix(1_700_000_000, 0).UTC()}
	retried := first
	retried.QueuedAt = first.QueuedAt.Add(time.Minute)
	retried.RetryCount++
	if first.Identity() == retried.Identity() {
		t.Fatal("CURRENT: a retry of an item without IdentityAt is expected to change Identity(); update the PC-D1B.4 token tuple rationale")
	}
	stamped := first
	stamped.IdentityAt = first.QueuedAt
	stampedRetry := stamped
	stampedRetry.QueuedAt = retried.QueuedAt
	stampedRetry.RetryCount++
	if stamped.Identity() != stampedRetry.Identity() {
		t.Fatal("a stamped IdentityAt must stay stable across retries")
	}
}

func TestPCD1B4Characterization_PersistedIdentityAtSurvivesRetry(t *testing.T) {
	orgID, libraryID := uuid.New(), uuid.New()
	queuedAt := time.Unix(1_700_000_000, 0).UTC()
	store := NewMockStore()
	queue := NewQueue(store)
	items := []QueueItem{
		{OrgID: orgID, LibraryID: libraryID, ItemType: ItemCommit, ItemID: "commit-1", QueuedAt: queuedAt, BlockRepresentationID: dbpkg.PlainBlockRepresentationID},
		{OrgID: orgID, LibraryID: libraryID, ItemType: ItemCommit, ItemID: "commit-2", QueuedAt: queuedAt, BlockRepresentationID: dbpkg.PlainBlockRepresentationID},
	}
	if err := queue.EnqueueBatch(items); err != nil {
		t.Fatalf("enqueue durable items: %v", err)
	}
	persisted := store.QueueItems(orgID)
	if len(persisted) != 2 {
		t.Fatalf("persisted queue rows = %d, want 2", len(persisted))
	}
	for _, item := range persisted {
		if item.IdentityAt.IsZero() || !item.IdentityAt.Equal(queuedAt) {
			t.Fatalf("persisted identity_at = %v, want durable fallback %v", item.IdentityAt, queuedAt)
		}
	}
	if persisted[0].IdentityAt != persisted[1].IdentityAt || persisted[0].ItemID == persisted[1].ItemID {
		t.Fatal("test precondition: distinct durable items share identity_at but retain distinct item identities")
	}

	firstIdentityAt := persisted[0].IdentityAt
	if err := queue.IncrementRetry(persisted[0]); err != nil {
		t.Fatalf("retry persisted queue item: %v", err)
	}
	afterRetry := store.QueueItems(orgID)
	if len(afterRetry) != 2 {
		t.Fatalf("queue rows after retry = %d, want 2", len(afterRetry))
	}
	for _, item := range afterRetry {
		if item.ItemID == "commit-1" && (!item.IdentityAt.Equal(firstIdentityAt) || item.QueuedAt.Equal(queuedAt)) {
			t.Fatalf("retry must move queued_at but preserve durable identity_at; got %+v", item)
		}
	}
}
