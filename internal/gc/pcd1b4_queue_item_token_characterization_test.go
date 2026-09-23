package gc

import (
	"testing"
	"time"

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

// With a zero IdentityAt, Identity() falls back to QueuedAt, which a retry
// rewrites (IncrementRetry requeues with a new QueuedAt), so a token derived
// from it would change across retries of the same durable item.
func TestPCD1B4Characterization_QueueItemIdentityChangesAcrossRetryWithoutIdentityAt(t *testing.T) {
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
