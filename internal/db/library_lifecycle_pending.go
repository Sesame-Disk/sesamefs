package db

import (
	"fmt"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

// Lifecycle operations recorded in library_lifecycle_pending.
const (
	LibraryLifecycleOpSoftDelete      = "soft_delete"
	LibraryLifecycleOpRestore         = "restore"
	LibraryLifecycleOpPermanentDelete = "permanent_delete"
)

// LibraryLifecyclePending is the durable continuation of one lifecycle
// transition (migration 029). It is written before the transition's canonical
// LWT and deleted after its completion; the lifecycle reaper finishes it after a
// crash. It is discovery only, never authority.
//
// The precondition fields are the transition's LWT condition: a soft delete
// applies only while the row is active at PrevLifecycleAt; a restore or a
// permanent delete only while the row is trashed at PrevDeletedAt.
type LibraryLifecyclePending struct {
	OrgID           string
	LibraryID       string
	Operation       string
	TargetAt        time.Time // the transition's lifecycle value (unique per library)
	OwnerID         string
	PrevLifecycleAt time.Time // zero: no lifecycle transition before
	PrevDeletedAt   time.Time
	RecordedAt      time.Time
}

func (p LibraryLifecyclePending) bucket() int {
	return GCDiscoveryBucket(p.OrgID, p.LibraryID)
}

// CanStillApply reports whether the transition's LWT could still commit given
// the canonical lifecycle state. While it can, the row must be kept.
func (p LibraryLifecyclePending) CanStillApply(state LibraryLifecycleState) bool {
	if !state.Present {
		return false
	}
	switch p.Operation {
	case LibraryLifecycleOpSoftDelete:
		return state.DeletedAt.IsZero() && state.LifecycleAt.Equal(p.PrevLifecycleAt)
	case LibraryLifecycleOpRestore, LibraryLifecycleOpPermanentDelete:
		return state.DeletedAt.Equal(p.PrevDeletedAt)
	default:
		return false
	}
}

func nullableTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

// InsertLibraryLifecyclePending makes the continuation durable before the
// canonical transition.
func InsertLibraryLifecyclePending(session *gocql.Session, p LibraryLifecyclePending) error {
	recordedAt := p.RecordedAt
	if recordedAt.IsZero() {
		recordedAt = time.Now().UTC()
	}
	var owner interface{}
	if p.OwnerID != "" {
		owner = p.OwnerID
	}
	if err := session.Query(`
		INSERT INTO library_lifecycle_pending (
			recovery_bucket, org_id, library_id, operation, target_at,
			owner_id, prev_lifecycle_at, prev_deleted_at, recorded_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, p.bucket(), p.OrgID, p.LibraryID, p.Operation, p.TargetAt.UTC(),
		owner, nullableTime(p.PrevLifecycleAt), nullableTime(p.PrevDeletedAt), recordedAt.UTC()).Exec(); err != nil {
		return fmt.Errorf("record library lifecycle continuation %s/%s %s: %w", p.OrgID, p.LibraryID, p.Operation, err)
	}
	return nil
}

// DeleteLibraryLifecyclePending removes exactly this transition's row.
func DeleteLibraryLifecyclePending(session *gocql.Session, p LibraryLifecyclePending) error {
	if err := session.Query(`
		DELETE FROM library_lifecycle_pending
		WHERE recovery_bucket = ? AND org_id = ? AND library_id = ? AND operation = ? AND target_at = ?
	`, p.bucket(), p.OrgID, p.LibraryID, p.Operation, p.TargetAt.UTC()).Exec(); err != nil {
		return fmt.Errorf("clear library lifecycle continuation %s/%s %s: %w", p.OrgID, p.LibraryID, p.Operation, err)
	}
	return nil
}

// ListLibraryLifecyclePending returns the rows of one recovery bucket.
func ListLibraryLifecyclePending(session *gocql.Session, bucket int) ([]LibraryLifecyclePending, error) {
	iter := session.Query(`
		SELECT org_id, library_id, operation, target_at, owner_id, prev_lifecycle_at, prev_deleted_at, recorded_at
		FROM library_lifecycle_pending WHERE recovery_bucket = ?
	`, bucket).Iter()
	var out []LibraryLifecyclePending
	var row LibraryLifecyclePending
	var owner *string
	for iter.Scan(&row.OrgID, &row.LibraryID, &row.Operation, &row.TargetAt, &owner, &row.PrevLifecycleAt, &row.PrevDeletedAt, &row.RecordedAt) {
		if owner != nil {
			row.OwnerID = *owner
		}
		out = append(out, row)
		row, owner = LibraryLifecyclePending{}, nil
	}
	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("list library lifecycle continuations bucket=%d: %w", bucket, err)
	}
	return out, nil
}
