package db

import (
	"sort"
	"strings"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

type AdminLibraryProjectionRow struct {
	OrgID        string
	LibraryID    string
	OwnerID      string
	OwnerEmail   string
	OwnerName    string
	Name         string
	Encrypted    bool
	StorageClass string
	SizeBytes    int64
	FileCount    int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    *time.Time
}

type AdminDeletedLibraryProjectionRow struct {
	OrgID      string
	LibraryID  string
	OwnerID    string
	OwnerEmail string
	OwnerName  string
	Name       string
	Encrypted  bool
	SizeBytes  int64
	DeletedAt  time.Time
}

const staleDeletedAdminLibraryCleanupBatchSize = 50

func adminLibraryProjectionDeletedAtEqual(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func adminLibraryProjectionDeleteRequired(previous, next AdminLibraryProjectionRow) bool {
	if previous.OrgID != next.OrgID || previous.OwnerID != next.OwnerID || !previous.CreatedAt.Equal(next.CreatedAt) {
		return true
	}
	return false
}

func AdminLibraryBucketDay(createdAt time.Time) string {
	return createdAt.UTC().Format("2006-01-02")
}

func ResolveAdminLibraryOwnerFields(session *gocql.Session, orgID, ownerID string) (string, string) {
	var ownerEmail, ownerName string
	_ = session.Query(`SELECT email, name FROM users WHERE org_id = ? AND user_id = ?`, orgID, ownerID).Scan(&ownerEmail, &ownerName)
	if ownerEmail == "" {
		ownerEmail = ownerID
	}
	if ownerName == "" {
		parts := strings.Split(ownerEmail, "@")
		ownerName = parts[0]
	}
	return ownerEmail, ownerName
}

func ReadAdminLibraryProjectionRow(session *gocql.Session, orgID, libraryID string) (AdminLibraryProjectionRow, error) {
	row := AdminLibraryProjectionRow{OrgID: orgID, LibraryID: libraryID}
	var deletedAt time.Time
	err := session.Query(`
		SELECT owner_id, name, encrypted, storage_class, size_bytes, file_count, created_at, updated_at, deleted_at
		FROM libraries
		WHERE org_id = ? AND library_id = ?
	`, orgID, libraryID).Scan(
		&row.OwnerID,
		&row.Name,
		&row.Encrypted,
		&row.StorageClass,
		&row.SizeBytes,
		&row.FileCount,
		&row.CreatedAt,
		&row.UpdatedAt,
		&deletedAt,
	)
	if err != nil {
		return AdminLibraryProjectionRow{}, err
	}
	if !deletedAt.IsZero() {
		deletedCopy := deletedAt
		row.DeletedAt = &deletedCopy
	}
	row.OwnerEmail, row.OwnerName = ResolveAdminLibraryOwnerFields(session, orgID, row.OwnerID)
	return row, nil
}

func AddDeleteAdminLibraryReadModelQuery(batch *gocql.Batch, row AdminLibraryProjectionRow) {
	bucketDay := AdminLibraryBucketDay(row.CreatedAt)
	batch.Query(`
		DELETE FROM libraries_by_owner
		WHERE org_id = ? AND owner_id = ? AND library_id = ?
	`, row.OrgID, row.OwnerID, row.LibraryID)
	batch.Query(`
		DELETE FROM libraries_by_org_updated
		WHERE org_id = ? AND library_id = ?
	`, row.OrgID, row.LibraryID)
	batch.Query(`
		DELETE FROM libraries_admin_global_by_updated
		WHERE bucket_day = ? AND org_id = ? AND library_id = ?
	`, bucketDay, row.OrgID, row.LibraryID)
	if row.DeletedAt != nil && !row.DeletedAt.IsZero() {
		batch.Query(`
			DELETE FROM libraries_deleted_by_org
			WHERE org_id = ? AND deleted_at = ? AND library_id = ?
		`, row.OrgID, *row.DeletedAt, row.LibraryID)
	}
}

func AddDeleteDeletedAdminLibraryReadModelQuery(batch *gocql.Batch, row AdminDeletedLibraryProjectionRow) {
	batch.Query(`
		DELETE FROM libraries_deleted_by_org
		WHERE org_id = ? AND deleted_at = ? AND library_id = ?
	`, row.OrgID, row.DeletedAt, row.LibraryID)
}

func AddDeleteDeletedAdminLibraryReadModelFallbackQueries(session *gocql.Session, batch *gocql.Batch, row AdminDeletedLibraryProjectionRow) error {
	AddDeleteDeletedAdminLibraryReadModelQuery(batch, row)
	batch.Query(`
		DELETE FROM libraries_by_org_updated
		WHERE org_id = ? AND library_id = ?
	`, row.OrgID, row.LibraryID)
	batch.Query(`
		DELETE FROM libraries_by_owner
		WHERE org_id = ? AND owner_id = ? AND library_id = ?
	`, row.OrgID, row.OwnerID, row.LibraryID)

	buckets, err := ListAdminLibraryBucketDays(session)
	if err != nil {
		return err
	}
	for _, bucketDay := range buckets {
		batch.Query(`
			DELETE FROM libraries_admin_global_by_updated
			WHERE bucket_day = ? AND org_id = ? AND library_id = ?
		`, bucketDay, row.OrgID, row.LibraryID)
	}
	return nil
}

func AddRefreshAdminLibraryReadModelQueries(batch *gocql.Batch, row AdminLibraryProjectionRow, previous *AdminLibraryProjectionRow) {
	if previous != nil && adminLibraryProjectionDeleteRequired(*previous, row) {
		AddDeleteAdminLibraryReadModelQuery(batch, *previous)
	}
	if previous != nil && !adminLibraryProjectionDeletedAtEqual(previous.DeletedAt, row.DeletedAt) && previous.DeletedAt != nil && !previous.DeletedAt.IsZero() && row.DeletedAt == nil {
		batch.Query(`
			DELETE FROM libraries_deleted_by_org
			WHERE org_id = ? AND deleted_at = ? AND library_id = ?
		`, previous.OrgID, *previous.DeletedAt, previous.LibraryID)
	}
	AddUpsertAdminLibraryReadModelQuery(batch, row)
}

func AddUpsertAdminLibraryReadModelQuery(batch *gocql.Batch, row AdminLibraryProjectionRow) {
	AddUpsertAdminLibraryActiveRowsQuery(batch, row)
	AddInsertDeletedAdminLibraryRowQuery(batch, row)
}

// AddInsertDeletedAdminLibraryRowQuery adds the org trash listing row of a
// trashed library (no-op for an active one).
func AddInsertDeletedAdminLibraryRowQuery(batch *gocql.Batch, row AdminLibraryProjectionRow) {
	if row.DeletedAt != nil && !row.DeletedAt.IsZero() {
		batch.Query(`
			INSERT INTO libraries_deleted_by_org (
				org_id, deleted_at, library_id, owner_id, owner_email, owner_name, name, encrypted, size_bytes
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, row.OrgID, *row.DeletedAt, row.LibraryID, row.OwnerID, row.OwnerEmail, row.OwnerName, row.Name, row.Encrypted, row.SizeBytes)
	}
}

// AddUpsertAdminLibraryActiveRowsQuery adds the owner, org and global read-model
// rows (but not the trash listing row). Their ordinary columns (owner, name,
// size...) carry the batch's client timestamp. Their deleted_at cell is
// lifecycle state, so it is never written with a client timestamp: for a
// trashed row it is set at the lifecycle write timestamp of its generation
// (AddAdminLibraryDeletedAtCellQueries); for an active row it is left alone,
// since only the lifecycle transition that made it active clears it
// (ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01). The batch must not carry a batch
// timestamp when the row is trashed.
func AddUpsertAdminLibraryActiveRowsQuery(batch *gocql.Batch, row AdminLibraryProjectionRow) {
	AddUpsertAdminLibraryOrdinaryRowsQuery(batch, row)
	if row.DeletedAt != nil && !row.DeletedAt.IsZero() {
		AddAdminLibraryDeletedAtCellQueries(batch, row, *row.DeletedAt)
	}
}

// AddUpsertAdminLibraryOrdinaryRowsQuery adds the ordinary columns of the
// owner, org and global read-model rows, without deleted_at.
func AddUpsertAdminLibraryOrdinaryRowsQuery(batch *gocql.Batch, row AdminLibraryProjectionRow) {
	bucketDay := AdminLibraryBucketDay(row.CreatedAt)
	batch.Query(`INSERT INTO library_admin_global_buckets (bucket_day) VALUES (?)`, bucketDay)
	batch.Query(`
		INSERT INTO libraries_by_owner (
			org_id, owner_id, library_id, name, encrypted, storage_class,
			size_bytes, file_count, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, row.OrgID, row.OwnerID, row.LibraryID, row.Name, row.Encrypted, row.StorageClass,
		row.SizeBytes, row.FileCount, row.CreatedAt, row.UpdatedAt)
	batch.Query(`
		INSERT INTO libraries_by_org_updated (
			org_id, library_id, owner_id, owner_email, owner_name, name,
			encrypted, storage_class, size_bytes, file_count, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, row.OrgID, row.LibraryID, row.OwnerID, row.OwnerEmail, row.OwnerName, row.Name,
		row.Encrypted, row.StorageClass, row.SizeBytes, row.FileCount, row.CreatedAt, row.UpdatedAt)
	batch.Query(`
		INSERT INTO libraries_admin_global_by_updated (
			bucket_day, org_id, library_id, owner_id, owner_email, owner_name,
			name, encrypted, storage_class, size_bytes, file_count, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, bucketDay, row.OrgID, row.LibraryID, row.OwnerID, row.OwnerEmail, row.OwnerName,
		row.Name, row.Encrypted, row.StorageClass, row.SizeBytes, row.FileCount, row.CreatedAt, row.UpdatedAt)
}

// AddAdminLibraryDeletedAtCellQueries sets (row.DeletedAt != nil) or clears the
// deleted_at cell of the owner, org and global read-model rows at
// LibraryLifecycleWriteTimestamp(lifecycleAt), the lifecycle value of the
// transition that produced that state: a trash generation's own value, or the
// restore's. Lifecycle values only grow, so the cell follows canonical
// transition order whatever the writers' clocks. Each statement carries its own
// timestamp; the batch must not carry one.
func AddAdminLibraryDeletedAtCellQueries(batch *gocql.Batch, row AdminLibraryProjectionRow, lifecycleAt time.Time) {
	stamp := LibraryLifecycleWriteTimestamp(lifecycleAt)
	bucketDay := AdminLibraryBucketDay(row.CreatedAt)
	if row.DeletedAt != nil && !row.DeletedAt.IsZero() {
		deletedAt := *row.DeletedAt
		batch.Query(`UPDATE libraries_by_owner USING TIMESTAMP ? SET deleted_at = ? WHERE org_id = ? AND owner_id = ? AND library_id = ?`,
			stamp, deletedAt, row.OrgID, row.OwnerID, row.LibraryID)
		batch.Query(`UPDATE libraries_by_org_updated USING TIMESTAMP ? SET deleted_at = ? WHERE org_id = ? AND library_id = ?`,
			stamp, deletedAt, row.OrgID, row.LibraryID)
		batch.Query(`UPDATE libraries_admin_global_by_updated USING TIMESTAMP ? SET deleted_at = ? WHERE bucket_day = ? AND org_id = ? AND library_id = ?`,
			stamp, deletedAt, bucketDay, row.OrgID, row.LibraryID)
		return
	}
	batch.Query(`DELETE deleted_at FROM libraries_by_owner USING TIMESTAMP ? WHERE org_id = ? AND owner_id = ? AND library_id = ?`,
		stamp, row.OrgID, row.OwnerID, row.LibraryID)
	batch.Query(`DELETE deleted_at FROM libraries_by_org_updated USING TIMESTAMP ? WHERE org_id = ? AND library_id = ?`,
		stamp, row.OrgID, row.LibraryID)
	batch.Query(`DELETE deleted_at FROM libraries_admin_global_by_updated USING TIMESTAMP ? WHERE bucket_day = ? AND org_id = ? AND library_id = ?`,
		stamp, bucketDay, row.OrgID, row.LibraryID)
}

func ListAdminLibraryBucketDays(session *gocql.Session) ([]string, error) {
	iter := session.Query(`SELECT bucket_day FROM library_admin_global_buckets`).Iter()
	var buckets []string
	var bucketDay string
	for iter.Scan(&bucketDay) {
		buckets = append(buckets, bucketDay)
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	sort.Slice(buckets, func(i, j int) bool {
		return buckets[i] > buckets[j]
	})
	return buckets, nil
}

func ListAdminGlobalLibraryRows(session *gocql.Session) ([]AdminLibraryProjectionRow, error) {
	buckets, err := ListAdminLibraryBucketDays(session)
	if err != nil {
		return nil, err
	}

	var rows []AdminLibraryProjectionRow
	for _, bucketDay := range buckets {
		iter := session.Query(`
			SELECT org_id, library_id, owner_id, owner_email, owner_name,
			       name, encrypted, storage_class, size_bytes, file_count, created_at, updated_at, deleted_at
			FROM libraries_admin_global_by_updated
			WHERE bucket_day = ?
		`, bucketDay).Iter()

		var row AdminLibraryProjectionRow
		var deletedAt time.Time
		for iter.Scan(
			&row.OrgID,
			&row.LibraryID,
			&row.OwnerID,
			&row.OwnerEmail,
			&row.OwnerName,
			&row.Name,
			&row.Encrypted,
			&row.StorageClass,
			&row.SizeBytes,
			&row.FileCount,
			&row.CreatedAt,
			&row.UpdatedAt,
			&deletedAt,
		) {
			if deletedAt.IsZero() {
				row.DeletedAt = nil
			} else {
				deletedCopy := deletedAt
				row.DeletedAt = &deletedCopy
			}
			rows = append(rows, row)
			row = AdminLibraryProjectionRow{}
			deletedAt = time.Time{}
		}
		if err := iter.Close(); err != nil {
			return nil, err
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].UpdatedAt.Equal(rows[j].UpdatedAt) {
			if rows[i].OrgID == rows[j].OrgID {
				return rows[i].LibraryID < rows[j].LibraryID
			}
			return rows[i].OrgID < rows[j].OrgID
		}
		return rows[i].UpdatedAt.After(rows[j].UpdatedAt)
	})

	return rows, nil
}

func ListAdminOrgLibraryRows(session *gocql.Session, orgID string) ([]AdminLibraryProjectionRow, error) {
	iter := session.Query(`
		SELECT library_id, owner_id, owner_email, owner_name,
		       name, encrypted, storage_class, size_bytes, file_count, created_at, updated_at, deleted_at
		FROM libraries_by_org_updated
		WHERE org_id = ?
	`, orgID).Iter()

	var rows []AdminLibraryProjectionRow
	var row AdminLibraryProjectionRow
	var deletedAt time.Time
	for iter.Scan(
		&row.LibraryID,
		&row.OwnerID,
		&row.OwnerEmail,
		&row.OwnerName,
		&row.Name,
		&row.Encrypted,
		&row.StorageClass,
		&row.SizeBytes,
		&row.FileCount,
		&row.CreatedAt,
		&row.UpdatedAt,
		&deletedAt,
	) {
		row.OrgID = orgID
		if deletedAt.IsZero() {
			row.DeletedAt = nil
		} else {
			deletedCopy := deletedAt
			row.DeletedAt = &deletedCopy
		}
		rows = append(rows, row)
		row = AdminLibraryProjectionRow{}
		deletedAt = time.Time{}
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].UpdatedAt.Equal(rows[j].UpdatedAt) {
			return rows[i].LibraryID < rows[j].LibraryID
		}
		return rows[i].UpdatedAt.After(rows[j].UpdatedAt)
	})
	return rows, nil
}

func ListAdminOwnerLibraryRows(session *gocql.Session, orgID, ownerID string) ([]AdminLibraryProjectionRow, error) {
	ownerEmail, ownerName := ResolveAdminLibraryOwnerFields(session, orgID, ownerID)
	iter := session.Query(`
		SELECT library_id, name, encrypted, storage_class, size_bytes, file_count, created_at, updated_at, deleted_at
		FROM libraries_by_owner
		WHERE org_id = ? AND owner_id = ?
	`, orgID, ownerID).Iter()

	var rows []AdminLibraryProjectionRow
	var row AdminLibraryProjectionRow
	var deletedAt time.Time
	for iter.Scan(
		&row.LibraryID,
		&row.Name,
		&row.Encrypted,
		&row.StorageClass,
		&row.SizeBytes,
		&row.FileCount,
		&row.CreatedAt,
		&row.UpdatedAt,
		&deletedAt,
	) {
		row.OrgID = orgID
		row.OwnerID = ownerID
		row.OwnerEmail = ownerEmail
		row.OwnerName = ownerName
		if deletedAt.IsZero() {
			row.DeletedAt = nil
		} else {
			deletedCopy := deletedAt
			row.DeletedAt = &deletedCopy
		}
		rows = append(rows, row)
		row = AdminLibraryProjectionRow{}
		deletedAt = time.Time{}
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].UpdatedAt.Equal(rows[j].UpdatedAt) {
			return rows[i].LibraryID < rows[j].LibraryID
		}
		return rows[i].UpdatedAt.After(rows[j].UpdatedAt)
	})
	return rows, nil
}

func ListDeletedAdminLibraryRowsByOrg(session *gocql.Session, orgID string) ([]AdminDeletedLibraryProjectionRow, error) {
	return listDeletedAdminLibraryRowsByOrg(session.Query(`
		SELECT deleted_at, library_id, owner_id, owner_email, owner_name, name, encrypted, size_bytes
		FROM libraries_deleted_by_org
		WHERE org_id = ?
	`, orgID), session, orgID)
}

// ListDeletedAdminLibraryRowsByOrgEachQuorum is ListDeletedAdminLibraryRowsByOrg
// read at EACH_QUORUM, so it sees a row acknowledged at LOCAL_QUORUM in any
// datacenter (and fails, instead of missing it, when a DC is unreachable).
func ListDeletedAdminLibraryRowsByOrgEachQuorum(session *gocql.Session, orgID string) ([]AdminDeletedLibraryProjectionRow, error) {
	return listDeletedAdminLibraryRowsByOrg(session.Query(`
		SELECT deleted_at, library_id, owner_id, owner_email, owner_name, name, encrypted, size_bytes
		FROM libraries_deleted_by_org
		WHERE org_id = ?
	`, orgID).Consistency(gocql.EachQuorum), session, orgID)
}

func listDeletedAdminLibraryRowsByOrg(query *gocql.Query, session *gocql.Session, orgID string) ([]AdminDeletedLibraryProjectionRow, error) {
	iter := query.Iter()

	var rows []AdminDeletedLibraryProjectionRow
	var row AdminDeletedLibraryProjectionRow
	for iter.Scan(
		&row.DeletedAt,
		&row.LibraryID,
		&row.OwnerID,
		&row.OwnerEmail,
		&row.OwnerName,
		&row.Name,
		&row.Encrypted,
		&row.SizeBytes,
	) {
		row.OrgID = orgID
		if row.OwnerEmail == "" || row.OwnerName == "" {
			row.OwnerEmail, row.OwnerName = ResolveAdminLibraryOwnerFields(session, orgID, row.OwnerID)
		}
		rows = append(rows, row)
		row = AdminDeletedLibraryProjectionRow{}
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return rows, nil
}

func ReconcileDeletedAdminLibraryRowsByOrg(session *gocql.Session, orgID string) ([]AdminDeletedLibraryProjectionRow, int, error) {
	rows, err := ListDeletedAdminLibraryRowsByOrg(session, orgID)
	if err != nil {
		return nil, 0, err
	}
	if len(rows) == 0 {
		return rows, 0, nil
	}

	kept := make([]AdminDeletedLibraryProjectionRow, 0, len(rows))
	batch := session.Batch(gocql.LoggedBatch)
	pendingDeletes := 0
	cleaned := 0

	flush := func() error {
		if pendingDeletes == 0 {
			return nil
		}
		if err := batch.Exec(); err != nil {
			return err
		}
		batch = session.Batch(gocql.LoggedBatch)
		pendingDeletes = 0
		return nil
	}

	for _, row := range rows {
		// Keep only the row of the library's current trash generation: a row of an
		// older generation (restored and trashed again) is stale too. Keeping is
		// never destructive, so a session-consistency read may decide it; deleting
		// on a mismatch needs the lifecycle authority, because such a read in one
		// datacenter can still show an older generation while the row being judged
		// is already the current one. A failed authority read fails the whole
		// reconciliation instead of deleting anything.
		liveRow, err := ReadAdminLibraryProjectionRow(session, row.OrgID, row.LibraryID)
		if err == nil && liveRow.DeletedAt != nil && liveRow.DeletedAt.Equal(row.DeletedAt) {
			kept = append(kept, row)
			continue
		}
		state, err := ReadLibraryLifecycleSerial(session, row.OrgID, row.LibraryID)
		if err != nil {
			return nil, cleaned, err
		}
		if state.Present && state.DeletedAt.Equal(row.DeletedAt) {
			kept = append(kept, row)
			continue
		}

		AddDeleteDeletedAdminLibraryReadModelQuery(batch, row)
		pendingDeletes++
		cleaned++

		if pendingDeletes >= staleDeletedAdminLibraryCleanupBatchSize {
			if err := flush(); err != nil {
				return nil, cleaned, err
			}
		}
	}

	if err := flush(); err != nil {
		return nil, cleaned, err
	}

	return kept, cleaned, nil
}

func ReadDeletedAdminLibraryProjectionRow(session *gocql.Session, orgID, libraryID string) (AdminDeletedLibraryProjectionRow, error) {
	rows, err := ListDeletedAdminLibraryRowsByOrg(session, orgID)
	if err != nil {
		return AdminDeletedLibraryProjectionRow{}, err
	}
	for _, row := range rows {
		if row.LibraryID == libraryID {
			return row, nil
		}
	}
	return AdminDeletedLibraryProjectionRow{}, gocql.ErrNotFound
}

func AddDeleteAdminLibraryReadModelQueries(session *gocql.Session, batch *gocql.Batch, orgID, libraryID string) error {
	projectionRow, err := ReadAdminLibraryProjectionRow(session, orgID, libraryID)
	if err == nil {
		AddDeleteAdminLibraryReadModelQuery(batch, projectionRow)
		return nil
	}
	if err != gocql.ErrNotFound {
		return err
	}

	deletedRow, err := ReadDeletedAdminLibraryProjectionRow(session, orgID, libraryID)
	if err == nil {
		return AddDeleteDeletedAdminLibraryReadModelFallbackQueries(session, batch, deletedRow)
	}
	if err == gocql.ErrNotFound {
		return nil
	}
	return err
}

func SyncAdminLibraryReadModel(session *gocql.Session, orgID, libraryID string) error {
	row, err := ReadAdminLibraryProjectionRow(session, orgID, libraryID)
	if err != nil {
		if err == gocql.ErrNotFound {
			return nil
		}
		return err
	}

	batch := session.Batch(gocql.LoggedBatch)
	AddUpsertAdminLibraryReadModelQuery(batch, row)
	return batch.Exec()
}
