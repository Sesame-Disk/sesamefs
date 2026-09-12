package db

import (
	"fmt"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

const libraryRollbackPendingListDefaultPageSize = 100

// LibraryRollbackPending is the durable discovery record for a new-library
// rollback whose derived-state cleanup may still be outstanding.
//
// It is not cleanup authority. The HEAD-domain LWT
// (DELETE FROM libraries ... IF head_commit_id = null) remains the only gate.
// The row exists so a crash after that LWT can be found again: owner_id and
// created_at are the snapshot needed to delete the exact read-model keys
// after the canonical libraries row is gone.
type LibraryRollbackPending struct {
	RecoveryBucket int
	OrgID          string
	LibraryID      string
	OwnerID        string
	CreatedAt      time.Time
	RecordedAt     time.Time
}

// LibraryRollbackPendingBucket hashes identity into one of GCDiscoveryBucketCount
// fixed recovery partitions. The bucket is discovery only.
func LibraryRollbackPendingBucket(orgID, libraryID string) int {
	return GCDiscoveryBucket(orgID, libraryID)
}

// LibraryRollbackPendingFromProjection stores the minimum projection snapshot
// required to replay AddDeleteAdminLibraryReadModelQuery after the canonical
// row disappears. Name, email, size and similar display fields are omitted.
func LibraryRollbackPendingFromProjection(row AdminLibraryProjectionRow) LibraryRollbackPending {
	return LibraryRollbackPending{
		RecoveryBucket: LibraryRollbackPendingBucket(row.OrgID, row.LibraryID),
		OrgID:          row.OrgID,
		LibraryID:      row.LibraryID,
		OwnerID:        row.OwnerID,
		CreatedAt:      row.CreatedAt,
		RecordedAt:     time.Now().UTC(),
	}
}

// ProjectionRow reconstructs the delete keys for the admin/owner/org/global
// read models. DeletedAt is left nil: creation rollback never wrote trash.
func (p LibraryRollbackPending) ProjectionRow() AdminLibraryProjectionRow {
	return AdminLibraryProjectionRow{
		OrgID:     p.OrgID,
		LibraryID: p.LibraryID,
		OwnerID:   p.OwnerID,
		CreatedAt: p.CreatedAt,
	}
}

func (p LibraryRollbackPending) bucket() int {
	return LibraryRollbackPendingBucket(p.OrgID, p.LibraryID)
}

// InsertLibraryRollbackPending upserts the marker. A row that already exists
// is success: the work is already durable. The bucket is always derived from
// (org_id, library_id) so a stale RecoveryBucket on the struct cannot land
// the row in the wrong partition.
func InsertLibraryRollbackPending(session *gocql.Session, pending LibraryRollbackPending) error {
	if session == nil {
		return fmt.Errorf("insert library rollback pending: session is nil")
	}
	recordedAt := pending.RecordedAt
	if recordedAt.IsZero() {
		recordedAt = time.Now().UTC()
	}
	if err := session.Query(`
		INSERT INTO library_rollback_pending (
			recovery_bucket, org_id, library_id, owner_id, created_at, recorded_at
		) VALUES (?, ?, ?, ?, ?, ?)
	`, pending.bucket(), pending.OrgID, pending.LibraryID, pending.OwnerID, pending.CreatedAt.UTC(), recordedAt.UTC()).Exec(); err != nil {
		return fmt.Errorf("insert library rollback pending org=%s library=%s: %w", pending.OrgID, pending.LibraryID, err)
	}
	return nil
}

// DeleteLibraryRollbackPending removes the marker for (org_id, library_id).
// A missing row is a no-op (Cassandra DELETE).
func DeleteLibraryRollbackPending(session *gocql.Session, pending LibraryRollbackPending) error {
	if session == nil {
		return fmt.Errorf("delete library rollback pending: session is nil")
	}
	if err := session.Query(`
		DELETE FROM library_rollback_pending
		WHERE recovery_bucket = ? AND org_id = ? AND library_id = ?
	`, pending.bucket(), pending.OrgID, pending.LibraryID).Exec(); err != nil {
		return fmt.Errorf("delete library rollback pending org=%s library=%s: %w", pending.OrgID, pending.LibraryID, err)
	}
	return nil
}

// GetLibraryRollbackPending reads the exact identity row. gocql.ErrNotFound
// means the marker is absent (settled, or never written).
func GetLibraryRollbackPending(session *gocql.Session, orgID, libraryID string) (LibraryRollbackPending, error) {
	if session == nil {
		return LibraryRollbackPending{}, fmt.Errorf("get library rollback pending: session is nil")
	}
	bucket := LibraryRollbackPendingBucket(orgID, libraryID)
	row := LibraryRollbackPending{RecoveryBucket: bucket}
	err := session.Query(`
		SELECT org_id, library_id, owner_id, created_at, recorded_at
		FROM library_rollback_pending
		WHERE recovery_bucket = ? AND org_id = ? AND library_id = ?
	`, bucket, orgID, libraryID).Scan(&row.OrgID, &row.LibraryID, &row.OwnerID, &row.CreatedAt, &row.RecordedAt)
	if err != nil {
		return LibraryRollbackPending{}, err
	}
	row.CreatedAt = row.CreatedAt.UTC()
	row.RecordedAt = row.RecordedAt.UTC()
	return row, nil
}

// LibraryRollbackPendingPage is one Cassandra page of pending rollback markers.
type LibraryRollbackPendingPage struct {
	Rows      []LibraryRollbackPending
	PageState []byte
}

// ListLibraryRollbackPending pages one recovery bucket. pageSize defaults to 100.
func ListLibraryRollbackPending(session *gocql.Session, bucket int, pageState []byte, pageSize int) (LibraryRollbackPendingPage, error) {
	if session == nil {
		return LibraryRollbackPendingPage{}, fmt.Errorf("list library rollback pending: session is nil")
	}
	if pageSize <= 0 {
		pageSize = libraryRollbackPendingListDefaultPageSize
	}
	iter := session.Query(`
		SELECT org_id, library_id, owner_id, created_at, recorded_at
		FROM library_rollback_pending
		WHERE recovery_bucket = ?
	`, bucket).PageSize(pageSize).PageState(pageState).Iter()
	var out LibraryRollbackPendingPage
	var orgID, libraryID, ownerID string
	var createdAt, recordedAt time.Time
	for iter.Scan(&orgID, &libraryID, &ownerID, &createdAt, &recordedAt) {
		out.Rows = append(out.Rows, LibraryRollbackPending{
			RecoveryBucket: bucket,
			OrgID:          orgID,
			LibraryID:      libraryID,
			OwnerID:        ownerID,
			CreatedAt:      createdAt.UTC(),
			RecordedAt:     recordedAt.UTC(),
		})
	}
	out.PageState = iter.PageState()
	if err := iter.Close(); err != nil {
		return LibraryRollbackPendingPage{}, fmt.Errorf("list library rollback pending bucket=%d: %w", bucket, err)
	}
	return out, nil
}
