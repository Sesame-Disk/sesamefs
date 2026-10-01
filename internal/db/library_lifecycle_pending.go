package db

import (
	"errors"
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

// LibraryLifecyclePendingConsistency is the consistency of every write, delete
// and read of library_lifecycle_pending. A global QUORUM write and a global
// QUORUM read always intersect, so a continuation acknowledged in one
// datacenter is seen by a reaper or a bulk cleanup running in any other, and a
// read that cannot reach a global quorum fails (retry later) instead of
// reporting nothing to do. It needs the same replicas as the global-SERIAL
// transition it guards.
const LibraryLifecyclePendingConsistency = gocql.Quorum

// LibraryLifecycleAttemptAbandonAfter is how old a continuation whose
// transition can still apply must be before the reaper fences it
// (FenceLibraryLifecycleAttempt). It only bounds how long an abandoned attempt
// (a producer that died before its LWT, or an LWT that failed without applying)
// is kept; the fence is safe at any age, because it is a Paxos transition on
// the canonical row and a fenced attempt's LWT can no longer apply. A variable
// for tests.
var LibraryLifecycleAttemptAbandonAfter = 10 * time.Minute

// LibraryLifecyclePending is the durable continuation of one lifecycle
// transition attempt (migration 029). It is written before the attempt's
// canonical LWT and deleted by the attempt itself once it is settled, or by the
// lifecycle reaper once the attempt can no longer apply. It is discovery only,
// never authority.
//
// AttemptID identifies the attempt: concurrent attempts that propose the same
// transition (same operation and target_at) have distinct rows, and an attempt
// only ever deletes its own row, so a loser cannot remove the continuation of
// the winner that died after its LWT.
//
// The precondition fields are the attempt's LWT condition: a soft delete
// applies only while the row is active at PrevLifecycleAt; a restore or a
// permanent delete only while the row is trashed at PrevDeletedAt with its
// lifecycle clock at PrevLifecycleAt.
type LibraryLifecyclePending struct {
	OrgID           string
	LibraryID       string
	Operation       string
	TargetAt        time.Time // the transition's lifecycle value
	AttemptID       string
	OwnerID         string
	PrevLifecycleAt time.Time // zero: no lifecycle transition before
	PrevDeletedAt   time.Time
	RecordedAt      time.Time
	// WriteTimestamp is the row's write timestamp (microseconds): set by
	// InsertLibraryLifecyclePending from RecordedAt and read back by
	// ListLibraryLifecyclePending (WRITETIME), so a retirement is always stamped
	// after the insert whatever the deleting node's clock.
	WriteTimestamp int64
}

func (p LibraryLifecyclePending) bucket() int {
	return GCDiscoveryBucket(p.OrgID, p.LibraryID)
}

// CanStillApply reports whether the attempt's LWT could still commit given the
// canonical lifecycle state. While it can, the row must be kept (or the attempt
// fenced first, see FenceLibraryLifecycleAttempt).
func (p LibraryLifecyclePending) CanStillApply(state LibraryLifecycleState) bool {
	if !state.Present || !state.LifecycleAt.Equal(p.PrevLifecycleAt) {
		return false
	}
	switch p.Operation {
	case LibraryLifecycleOpSoftDelete:
		return state.DeletedAt.IsZero()
	case LibraryLifecycleOpRestore, LibraryLifecycleOpPermanentDelete:
		return state.DeletedAt.Equal(p.PrevDeletedAt)
	default:
		return false
	}
}

// Abandoned reports whether the attempt is old enough to be fenced.
func (p LibraryLifecyclePending) Abandoned(now time.Time) bool {
	return !p.RecordedAt.IsZero() && now.Sub(p.RecordedAt) >= LibraryLifecycleAttemptAbandonAfter
}

func nullableTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

// NewLibraryLifecycleAttempt stamps a continuation with a fresh attempt id and
// its recording time, which is also its insert write timestamp.
func NewLibraryLifecycleAttempt(p LibraryLifecyclePending, attemptID string) LibraryLifecyclePending {
	p.AttemptID = attemptID
	p.RecordedAt = time.Now().UTC()
	p.WriteTimestamp = p.RecordedAt.UnixMicro()
	return p
}

// InsertLibraryLifecyclePending makes the attempt's continuation durable before
// its canonical transition. The row is written at p.WriteTimestamp (from
// NewLibraryLifecycleAttempt), never at an implicit client timestamp.
func InsertLibraryLifecyclePending(session *gocql.Session, p LibraryLifecyclePending) error {
	if p.AttemptID == "" || p.RecordedAt.IsZero() || p.WriteTimestamp == 0 {
		return errors.New("record library lifecycle continuation: attempt not stamped (NewLibraryLifecycleAttempt)")
	}
	recordedAt := p.RecordedAt
	var owner interface{}
	if p.OwnerID != "" {
		owner = p.OwnerID
	}
	if err := session.Query(`
		INSERT INTO library_lifecycle_pending (
			recovery_bucket, org_id, library_id, operation, target_at, attempt_id,
			owner_id, prev_lifecycle_at, prev_deleted_at, recorded_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) USING TIMESTAMP ?
	`, p.bucket(), p.OrgID, p.LibraryID, p.Operation, p.TargetAt.UTC(), p.AttemptID,
		owner, nullableTime(p.PrevLifecycleAt), nullableTime(p.PrevDeletedAt), recordedAt.UTC(), p.WriteTimestamp).
		Consistency(LibraryLifecyclePendingConsistency).Exec(); err != nil {
		return fmt.Errorf("record library lifecycle continuation %s/%s %s: %w", p.OrgID, p.LibraryID, p.Operation, err)
	}
	return nil
}

// libraryLifecyclePendingRetireTimestamp stamps a retirement after the row's
// insert (whose timestamp is known exactly) and not before the local clock.
// Rows are never re-inserted (attempt_id is unique), so the tombstone always
// wins, even when the retiring node's clock is behind the producer's.
func libraryLifecyclePendingRetireTimestamp(p LibraryLifecyclePending, now time.Time) int64 {
	stamp := now.UnixMicro()
	if p.WriteTimestamp >= stamp {
		stamp = p.WriteTimestamp + 1
	}
	return stamp
}

// DeleteLibraryLifecyclePending removes exactly this attempt's row.
func DeleteLibraryLifecyclePending(session *gocql.Session, p LibraryLifecyclePending) error {
	if err := session.Query(`
		DELETE FROM library_lifecycle_pending USING TIMESTAMP ?
		WHERE recovery_bucket = ? AND org_id = ? AND library_id = ? AND operation = ? AND target_at = ? AND attempt_id = ?
	`, libraryLifecyclePendingRetireTimestamp(p, time.Now()), p.bucket(), p.OrgID, p.LibraryID, p.Operation, p.TargetAt.UTC(), p.AttemptID).
		Consistency(LibraryLifecyclePendingConsistency).Exec(); err != nil {
		return fmt.Errorf("clear library lifecycle continuation %s/%s %s: %w", p.OrgID, p.LibraryID, p.Operation, err)
	}
	return nil
}

// ListLibraryLifecyclePending returns the rows of one recovery bucket.
func ListLibraryLifecyclePending(session *gocql.Session, bucket int) ([]LibraryLifecyclePending, error) {
	iter := session.Query(`
		SELECT org_id, library_id, operation, target_at, attempt_id, owner_id, prev_lifecycle_at, prev_deleted_at, recorded_at, WRITETIME(recorded_at)
		FROM library_lifecycle_pending WHERE recovery_bucket = ?
	`, bucket).Consistency(LibraryLifecyclePendingConsistency).Iter()
	var out []LibraryLifecyclePending
	var row LibraryLifecyclePending
	var owner *string
	for iter.Scan(&row.OrgID, &row.LibraryID, &row.Operation, &row.TargetAt, &row.AttemptID, &owner, &row.PrevLifecycleAt, &row.PrevDeletedAt, &row.RecordedAt, &row.WriteTimestamp) {
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
