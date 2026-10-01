package db

import (
	"errors"
	"fmt"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

// BeforeLibraryOwnerTransferFn runs between an owner transfer's SERIAL read and
// its LWT. A variable so integration tests can pause a transfer there.
var BeforeLibraryOwnerTransferFn = func(orgID, libraryID string) {}

// TransferLibraryOwnerCanonical changes the owner of the canonical libraries row
// by a global-SERIAL LWT conditioned on the row existing (created_at != null)
// and still being at the lifecycle state it read (deleted_at and lifecycle_at).
// A plain UPDATE is an upsert: a transfer that read the row before a permanent
// delete and wrote after it would recreate the canonical row
// (ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01). The conditional update can never
// create a row, and Paxos orders it against every lifecycle transition. It
// returns the canonical row it transferred (before the change) and its
// lifecycle state, or gocql.ErrNotFound when the library does not exist.
func TransferLibraryOwnerCanonical(session *gocql.Session, orgID, libraryID, newOwnerID string, updatedAt time.Time) (AdminLibraryProjectionRow, LibraryLifecycleState, error) {
	for attempt := 0; attempt < lifecycleTransitionAttempts; attempt++ {
		row, state, err := readCanonicalLibraryRowSerial(session, orgID, libraryID)
		if err != nil {
			return AdminLibraryProjectionRow{}, LibraryLifecycleState{}, fmt.Errorf("read library %s before owner transfer: %w", libraryID, err)
		}
		if !state.Present {
			return AdminLibraryProjectionRow{}, LibraryLifecycleState{}, gocql.ErrNotFound
		}
		BeforeLibraryOwnerTransferFn(orgID, libraryID)
		applied, err := session.Query(`
			UPDATE libraries SET owner_id = ?, updated_at = ?
			WHERE org_id = ? AND library_id = ?
			IF created_at != null AND deleted_at = ? AND lifecycle_at = ?
		`, newOwnerID, updatedAt, orgID, libraryID, nullableTime(state.DeletedAt), nullableTime(state.LifecycleAt)).
			SerialConsistency(LibraryHeadSerialConsistency).
			MapScanCAS(map[string]interface{}{})
		if err == nil {
			if applied {
				return row, state, nil
			}
			continue // gone or another lifecycle state: the next read decides
		}
		if !isAmbiguousLibraryLifecycleCASError(err) {
			return AdminLibraryProjectionRow{}, LibraryLifecycleState{}, fmt.Errorf("transfer library %s: %w", libraryID, err)
		}
		after, afterState, readErr := readCanonicalLibraryRowSerial(session, orgID, libraryID)
		if readErr != nil {
			return AdminLibraryProjectionRow{}, LibraryLifecycleState{}, errors.Join(fmt.Errorf("%w: transfer library %s: %w", ErrLibraryLifecycleOutcomeUnknown, libraryID, err), readErr)
		}
		if afterState.Present && after.OwnerID == newOwnerID && afterState.DeletedAt.Equal(state.DeletedAt) && afterState.LifecycleAt.Equal(state.LifecycleAt) {
			return row, state, nil
		}
		if afterState.Present && after.OwnerID == row.OwnerID && afterState.DeletedAt.Equal(state.DeletedAt) && afterState.LifecycleAt.Equal(state.LifecycleAt) {
			return AdminLibraryProjectionRow{}, LibraryLifecycleState{}, fmt.Errorf("%w: transfer library %s: %w", ErrLibraryLifecycleOutcomeUnknown, libraryID, err)
		}
		// The library moved on (deleted, or another lifecycle state): retry.
	}
	return AdminLibraryProjectionRow{}, LibraryLifecycleState{}, fmt.Errorf("transfer library %s: lifecycle kept changing", libraryID)
}

// LibraryLookupWriteTimeFloor returns one microsecond after the latest write
// timestamp of any cell of the library's libraries_by_id lookup row, read at
// EACH_QUORUM (zero when the row is absent). A permanent delete stamps its
// completion at or above it, so a lookup written by a node whose clock is ahead
// cannot outlive the delete. A failed read fails the caller (retry later).
func LibraryLookupWriteTimeFloor(session *gocql.Session, libraryID string) (int64, error) {
	var org, owner, name, encrypted *int64
	err := session.Query(`
		SELECT WRITETIME(org_id), WRITETIME(owner_id), WRITETIME(name), WRITETIME(encrypted)
		FROM libraries_by_id WHERE library_id = ?
	`, libraryID).Consistency(gocql.EachQuorum).Scan(&org, &owner, &name, &encrypted)
	if errors.Is(err, gocql.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read library lookup write time %s: %w", libraryID, err)
	}
	var floor int64
	for _, w := range []*int64{org, owner, name, encrypted} {
		if w != nil && *w+1 > floor {
			floor = *w + 1
		}
	}
	return floor, nil
}
