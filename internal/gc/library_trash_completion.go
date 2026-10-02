package gc

import (
	"errors"
	"fmt"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/traffic"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

// LibraryTrashCompletionAfterReadHook is a test seam for a pause after reading
// canonical state, before publishing its derived state.
var LibraryTrashCompletionAfterReadHook func()

// LibraryTrashCompletionBeforeMarkerDeleteHook pauses after accounting, before
// conditional cleanup of the marker observed at completion entry. Test seam only.
var LibraryTrashCompletionBeforeMarkerDeleteHook func()

var errLibraryTrashCanonicalChanged = errors.New("canonical library changed before marker publication")

// UpsertLibraryTrashMarker uses only the marker's global SERIAL domain. Inserts
// cannot be hidden by an earlier LWT tombstone, even with client clock skew.
// The marker remains discovery, never canonical destruction authority.
func UpsertLibraryTrashMarker(session *gocql.Session, row db.AdminLibraryProjectionRow, representation string) error {
	if row.DeletedAt == nil {
		return fmt.Errorf("trash marker requires a generation")
	}
	for attempt := 0; attempt < 4; attempt++ {
		var current time.Time
		err := session.Query(`SELECT deleted_at FROM deleted_libraries WHERE library_id=?`, row.LibraryID).Consistency(gocql.Serial).Scan(&current)
		// Read marker identity first, then validate canonical authority. The LWT
		// below names that observed marker, so a purge intent published during a
		// pause cannot be overwritten by this older canonical snapshot.
		if err == nil && current.Equal(*row.DeletedAt) {
			return nil
		}
		if err != nil && !errors.Is(err, gocql.ErrNotFound) {
			return err
		}
		var canonical time.Time
		canonicalErr := session.Query(`SELECT deleted_at FROM libraries WHERE org_id=? AND library_id=?`, row.OrgID, row.LibraryID).Consistency(gocql.Serial).Scan(&canonical)
		if errors.Is(canonicalErr, gocql.ErrNotFound) || (canonicalErr == nil && !canonical.Equal(*row.DeletedAt)) {
			return errLibraryTrashCanonicalChanged
		}
		if canonicalErr != nil {
			return canonicalErr
		}
		var applied bool
		if errors.Is(err, gocql.ErrNotFound) {
			applied, err = session.Query(`INSERT INTO deleted_libraries (library_id,org_id,deleted_at,storage_class,block_representation_id) VALUES (?,?,?,?,?) IF NOT EXISTS USING TTL 0`, row.LibraryID, row.OrgID, *row.DeletedAt, row.StorageClass, representation).SerialConsistency(gocql.Serial).MapScanCAS(map[string]interface{}{})
		} else if err == nil {
			if current.Equal(*row.DeletedAt) {
				return nil
			}
			applied, err = session.Query(`UPDATE deleted_libraries USING TTL 0 SET org_id=?,deleted_at=?,storage_class=?,block_representation_id=?,purge_requested_at=null,owner_id=null,created_at=null WHERE library_id=? IF deleted_at=?`, row.OrgID, *row.DeletedAt, row.StorageClass, representation, row.LibraryID, current).SerialConsistency(gocql.Serial).MapScanCAS(map[string]interface{}{})
		}
		if err != nil {
			return err
		}
		if applied {
			return nil
		}
	}
	return fmt.Errorf("library trash marker changed repeatedly: %s", row.LibraryID)
}

// RepairLibraryTrashDerivedState publishes canonical state, then revalidates it.
// A resumed writer repairs the state it overwrote rather than leaving its old
// snapshot behind. An exhausted retry is an error, never a false success.
func RepairLibraryTrashDerivedState(session *gocql.Session, orgID, libraryID string, previous db.AdminLibraryProjectionRow, representation string) (*db.AdminLibraryProjectionRow, error) {
	for attempt := 0; attempt < 4; attempt++ {
		row, err := db.ReadAdminLibraryProjectionRowSerial(session, orgID, libraryID)
		if errors.Is(err, gocql.ErrNotFound) {
			batch := session.Batch(gocql.LoggedBatch)
			db.AddDeleteAdminLibraryReadModelQuery(batch, previous)
			if err := deleteOtherTrashProjections(session, batch, orgID, libraryID, nil); err != nil {
				return nil, err
			}
			return nil, batch.Exec()
		}
		if err != nil {
			return nil, err
		}
		if hook := LibraryTrashCompletionAfterReadHook; hook != nil {
			hook()
		}
		if row.DeletedAt != nil {
			if representation == "" {
				representation, err = db.ResolveBlockRepresentationIDForDelete(session, orgID, libraryID)
				if err != nil {
					return nil, err
				}
			}
			if err := UpsertLibraryTrashMarker(session, row, representation); err != nil {
				if errors.Is(err, errLibraryTrashCanonicalChanged) {
					previous = row
					continue
				}
				return nil, err
			}
		}

		batch := session.Batch(gocql.LoggedBatch)
		traffic.AddAggregateStorageReconciliationQueries(batch, orgID, row.OwnerID, time.Now().UTC())
		db.AddRefreshAdminLibraryReadModelQueries(batch, row, &previous)
		if err := deleteOtherTrashProjections(session, batch, orgID, libraryID, row.DeletedAt); err != nil {
			return nil, err
		}
		if err := batch.Exec(); err != nil {
			return nil, err
		}
		next, err := db.ReadAdminLibraryProjectionRowSerial(session, orgID, libraryID)
		if errors.Is(err, gocql.ErrNotFound) {
			previous = row
			continue
		}
		if err != nil {
			return nil, err
		}
		if sameTrashGeneration(row.DeletedAt, next.DeletedAt) {
			return &row, nil
		}
		previous = row
	}
	return nil, fmt.Errorf("library lifecycle changed repeatedly while publishing %s", libraryID)
}

func sameTrashGeneration(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func deleteOtherTrashProjections(session *gocql.Session, batch *gocql.Batch, orgID, libraryID string, keep *time.Time) error {
	iter := session.Query(`SELECT library_id,deleted_at FROM libraries_deleted_by_org WHERE org_id=?`, orgID).Iter()
	var id string
	var generation time.Time
	for iter.Scan(&id, &generation) {
		if id == libraryID && (keep == nil || !generation.Equal(*keep)) {
			batch.Query(`DELETE FROM libraries_deleted_by_org WHERE org_id=? AND deleted_at=? AND library_id=?`, orgID, generation, libraryID)
		}
	}
	return iter.Close()
}

// PreparePermanentLibraryDelete persists the completion keys before deleting the
// canonical row. TTL 0 keeps the recovery intent until GC settles its generation.
func PreparePermanentLibraryDelete(session *gocql.Session, row db.AdminLibraryProjectionRow, representation string) error {
	if err := UpsertLibraryTrashMarker(session, row, representation); err != nil {
		return err
	}
	applied, err := session.Query(`UPDATE deleted_libraries USING TTL 0 SET org_id=?,deleted_at=?,storage_class=?,block_representation_id=?,owner_id=?,created_at=?,purge_requested_at=? WHERE library_id=? IF deleted_at=?`, row.OrgID, *row.DeletedAt, row.StorageClass, representation, row.OwnerID, row.CreatedAt, time.Now().UTC(), row.LibraryID, *row.DeletedAt).SerialConsistency(gocql.Serial).MapScanCAS(map[string]interface{}{})
	if err != nil {
		return err
	}
	if !applied {
		return fmt.Errorf("permanent delete marker generation changed")
	}
	return nil
}

// ReadPermanentLibraryDeleteIntent returns retry authorization and exact cleanup
// keys only for an explicitly requested purge. Canonical state must still be checked.
func ReadPermanentLibraryDeleteIntent(session *gocql.Session, libraryID string) (db.AdminLibraryProjectionRow, string, error) {
	row := db.AdminLibraryProjectionRow{LibraryID: libraryID}
	var generation, purge time.Time
	var representation string
	err := session.Query(`SELECT org_id,owner_id,created_at,deleted_at,storage_class,block_representation_id,purge_requested_at FROM deleted_libraries WHERE library_id=?`, libraryID).Consistency(gocql.Serial).Scan(&row.OrgID, &row.OwnerID, &row.CreatedAt, &generation, &row.StorageClass, &representation, &purge)
	if err != nil {
		return row, "", err
	}
	if purge.IsZero() || generation.IsZero() || row.OwnerID == "" {
		return row, "", gocql.ErrNotFound
	}
	row.DeletedAt = &generation
	return row, representation, nil
}

// CompleteLibraryTrashLifecycle retains an active library's recovery marker until
// both projections and accounting have completed. A failed completion remains
// reachable through restore's authenticated HTTP retry, including with GC off.
func CompleteLibraryTrashLifecycle(database traffic.DBSession, orgID, ownerID, libraryID string, previous db.AdminLibraryProjectionRow, representation string) error {
	// Capture recovery identity before publishing. A later D2 purge intent must
	// survive an old ACTIVE completion, including if canonical D2 is already gone.
	var completionMarker time.Time
	markerErr := database.Session().Query(`SELECT deleted_at FROM deleted_libraries WHERE library_id=?`, libraryID).Consistency(gocql.Serial).Scan(&completionMarker)
	if markerErr != nil && !errors.Is(markerErr, gocql.ErrNotFound) {
		return markerErr
	}
	for attempt := 0; attempt < 4; attempt++ {
		row, err := RepairLibraryTrashDerivedState(database.Session(), orgID, libraryID, previous, representation)
		if err != nil {
			return err
		}
		if err := traffic.ReconcileLibraryLifecycleStorage(database, orgID, ownerID); err != nil {
			return err
		}
		if row == nil || row.DeletedAt != nil {
			return nil
		}
		if hook := LibraryTrashCompletionBeforeMarkerDeleteHook; hook != nil {
			hook()
		}
		if !completionMarker.IsZero() {
			if err := DeleteLibraryMarkerAtGeneration(database.Session(), libraryID, completionMarker); err != nil {
				return err
			}
		}

		current, err := db.ReadAdminLibraryProjectionRowSerial(database.Session(), orgID, libraryID)
		if err == nil && current.DeletedAt == nil {
			return nil
		}
		if err != nil && !errors.Is(err, gocql.ErrNotFound) {
			return err
		}
		previous = *row
	}
	return fmt.Errorf("library lifecycle changed repeatedly during completion: %s", libraryID)
}
