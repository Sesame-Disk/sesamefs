package db

import (
	"errors"
	"fmt"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

// Timestamp and Paxos discipline of the lifecycle of a library
// (ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01).
//
// The canonical lifecycle cells (libraries.deleted_at / deleted_by and the
// row's existence) are written only by global-SERIAL LWTs pinned to
// LibraryHeadSerialConsistency, the partition's existing Paxos domain: soft
// delete, restore and hard delete. Paxos ballots on a partition only grow, so a
// lifecycle transition that commits later always carries the larger write
// timestamp; no transition can lose to an earlier one because of the clock that
// stamped it. Nothing else writes those cells.
//
// The derived rows (the deleted_libraries marker, the admin read model,
// libraries_by_id, reconciliation requests) keep client timestamps, as before.
// Each transition stamps its derived writes with a client timestamp taken
// before its canonical transition, so the derived writes of any transition
// that commits later win, whenever a paused or retried completion lands.

// ErrLibraryLifecycleOutcomeUnknown reports a lifecycle LWT whose outcome
// Cassandra could not report and that a SERIAL read could not settle: the
// canonical state still matches the precondition, so the proposal may be
// completed by a later Paxos round. Callers must fail closed.
var ErrLibraryLifecycleOutcomeUnknown = errors.New("library lifecycle transition outcome unknown")

// LibraryLifecycleOutcome is the result of a generation-fenced lifecycle LWT.
type LibraryLifecycleOutcome int

const (
	// LibraryLifecycleApplied: the transition committed for the captured
	// generation (or, for the terminal delete after an ambiguous outcome, the
	// generation is already gone; see DeleteTrashedLibraryGeneration).
	LibraryLifecycleApplied LibraryLifecycleOutcome = iota
	// LibraryLifecycleTargetAbsent: nothing applied because the target row does
	// not exist.
	LibraryLifecycleTargetAbsent
	// LibraryLifecycleGenerationChanged: nothing applied because the row carries
	// another lifecycle generation (restored, trashed again, or already purged).
	LibraryLifecycleGenerationChanged
)

func (o LibraryLifecycleOutcome) String() string {
	switch o {
	case LibraryLifecycleApplied:
		return "applied"
	case LibraryLifecycleTargetAbsent:
		return "target absent"
	case LibraryLifecycleGenerationChanged:
		return "generation changed"
	default:
		return fmt.Sprintf("LibraryLifecycleOutcome(%d)", int(o))
	}
}

// isAmbiguousLibraryLifecycleCASError reports an LWT whose outcome Cassandra
// could not report (protocol v5 CAS_WRITE_UNKNOWN, or a v4 write timeout /
// failure with WriteType "CAS", or a lost response).
func isAmbiguousLibraryLifecycleCASError(err error) bool {
	var casUnknown *gocql.RequestErrCASWriteUnknown
	if errors.As(err, &casUnknown) || errors.Is(err, gocql.ErrTimeoutNoResponse) || errors.Is(err, gocql.ErrConnectionClosed) {
		return true
	}
	var writeTimeout *gocql.RequestErrWriteTimeout
	if errors.As(err, &writeTimeout) && writeTimeout.WriteType == "CAS" {
		return true
	}
	var writeFailure *gocql.RequestErrWriteFailure
	return errors.As(err, &writeFailure) && writeFailure.WriteType == "CAS"
}

// casDeletedAt interprets a not-applied LWT result: whether the row exists and
// the deleted_at it carries (zero when null).
func casDeletedAt(previous map[string]interface{}) (present bool, deletedAt time.Time) {
	raw, present := previous["deleted_at"]
	if !present {
		return false, time.Time{}
	}
	if t, ok := raw.(time.Time); ok {
		return true, t
	}
	return true, time.Time{}
}

// readLibraryDeletedAtSerial reads canonical deleted_at in the lifecycle Paxos
// domain, so it observes every committed lifecycle LWT.
func readLibraryDeletedAtSerial(session *gocql.Session, orgID, libraryID string) (bool, time.Time, error) {
	var deletedAt time.Time
	err := session.Query(`SELECT deleted_at FROM libraries WHERE org_id = ? AND library_id = ?`,
		orgID, libraryID).Consistency(LibraryHeadSerialConsistency).Scan(&deletedAt)
	if errors.Is(err, gocql.ErrNotFound) {
		return false, time.Time{}, nil
	}
	if err != nil {
		return false, time.Time{}, err
	}
	return true, deletedAt, nil
}

// SoftDeleteLibraryGeneration moves an active library to the trash under the
// new generation deletedAt: UPDATE ... IF deleted_at = null AND created_at !=
// null, so it never creates a row for a library that is gone and never
// replaces another trash generation.
//
// Outcomes: Applied; TargetAbsent (no library); GenerationChanged (already in
// the trash). An ambiguous outcome is settled by a SERIAL read: the row at
// deletedAt reports Applied, an active row reports
// ErrLibraryLifecycleOutcomeUnknown.
func SoftDeleteLibraryGeneration(session *gocql.Session, orgID, libraryID string, deletedAt time.Time, deletedBy string) (LibraryLifecycleOutcome, error) {
	if deletedAt.IsZero() {
		return LibraryLifecycleGenerationChanged, fmt.Errorf("soft-delete library %s: zero deleted_at generation", libraryID)
	}
	previous := map[string]interface{}{}
	applied, err := session.Query(`
		UPDATE libraries SET deleted_at = ?, deleted_by = ?, updated_at = ?
		WHERE org_id = ? AND library_id = ?
		IF deleted_at = null AND created_at != null
	`, deletedAt, deletedBy, deletedAt, orgID, libraryID).
		SerialConsistency(LibraryHeadSerialConsistency).
		MapScanCAS(previous)
	if err == nil {
		if applied {
			return LibraryLifecycleApplied, nil
		}
		present, current := casDeletedAt(previous)
		if createdAt, ok := previous["created_at"].(time.Time); !present || !ok || createdAt.IsZero() {
			return LibraryLifecycleTargetAbsent, nil
		}
		if current.IsZero() {
			// created_at present and deleted_at null would have applied.
			return LibraryLifecycleGenerationChanged, fmt.Errorf("%w: soft-delete library %s: condition failed on an active row", ErrLibraryLifecycleOutcomeUnknown, libraryID)
		}
		return LibraryLifecycleGenerationChanged, nil
	}
	if !isAmbiguousLibraryLifecycleCASError(err) {
		return LibraryLifecycleGenerationChanged, fmt.Errorf("soft-delete library %s: %w", libraryID, err)
	}
	present, current, readErr := readLibraryDeletedAtSerial(session, orgID, libraryID)
	return settleLibrarySoftDelete(libraryID, deletedAt, err, present, current, readErr)
}

// settleLibrarySoftDelete settles an ambiguous SoftDeleteLibraryGeneration from
// a SERIAL read of the canonical row.
func settleLibrarySoftDelete(libraryID string, deletedAt time.Time, casErr error, present bool, current time.Time, readErr error) (LibraryLifecycleOutcome, error) {
	if readErr != nil {
		return LibraryLifecycleGenerationChanged, errors.Join(fmt.Errorf("%w: soft-delete library %s: %w", ErrLibraryLifecycleOutcomeUnknown, libraryID, casErr), fmt.Errorf("settlement read failed: %w", readErr))
	}
	switch {
	case !present:
		return LibraryLifecycleTargetAbsent, nil
	case current.Equal(deletedAt):
		return LibraryLifecycleApplied, nil
	case current.IsZero():
		return LibraryLifecycleGenerationChanged, fmt.Errorf("%w: soft-delete library %s: %w", ErrLibraryLifecycleOutcomeUnknown, libraryID, casErr)
	default:
		return LibraryLifecycleGenerationChanged, nil
	}
}

// DeleteTrashedLibraryGeneration removes the canonical `libraries` row only
// while it is still trashed under generation deletedAt. It is the final,
// fenced lifecycle mutation of a permanent delete / library cascade: an owner
// that lost the hard-delete lease cannot remove a library that another owner
// restored (deleted_at = null) or that was trashed again (a new deleted_at),
// however long it paused, because the predicate is evaluated by Paxos when the
// delete commits.
//
// Outcomes: Applied; TargetAbsent (row already gone: this generation was
// already permanently deleted, which is terminal); GenerationChanged.
//
// On an ambiguous LWT outcome a SERIAL read settles it. A missing row reports
// Applied: a hard delete is terminal, so whichever delete removed the row the
// caller's completion writes are still due and cannot revive anything (another
// owner can only have removed it if the lease was taken over while this LWT was
// in flight). A row still at deletedAt reports ErrLibraryLifecycleOutcomeUnknown.
func DeleteTrashedLibraryGeneration(session *gocql.Session, orgID, libraryID string, deletedAt time.Time) (LibraryLifecycleOutcome, error) {
	if deletedAt.IsZero() {
		return LibraryLifecycleGenerationChanged, fmt.Errorf("delete trashed library %s: zero deleted_at generation", libraryID)
	}
	previous := map[string]interface{}{}
	applied, err := session.Query(`
		DELETE FROM libraries WHERE org_id = ? AND library_id = ?
		IF deleted_at = ?
	`, orgID, libraryID, deletedAt).
		SerialConsistency(LibraryHeadSerialConsistency).
		MapScanCAS(previous)
	if err == nil {
		if applied {
			return LibraryLifecycleApplied, nil
		}
		if present, _ := casDeletedAt(previous); !present {
			return LibraryLifecycleTargetAbsent, nil
		}
		return LibraryLifecycleGenerationChanged, nil
	}
	if !isAmbiguousLibraryLifecycleCASError(err) {
		return LibraryLifecycleGenerationChanged, fmt.Errorf("delete trashed library %s: %w", libraryID, err)
	}
	present, current, readErr := readLibraryDeletedAtSerial(session, orgID, libraryID)
	return settleTrashedLibraryDelete(libraryID, deletedAt, err, present, current, readErr)
}

// settleTrashedLibraryDelete settles an ambiguous DeleteTrashedLibraryGeneration
// from a SERIAL read of the canonical row.
func settleTrashedLibraryDelete(libraryID string, deletedAt time.Time, casErr error, present bool, current time.Time, readErr error) (LibraryLifecycleOutcome, error) {
	if readErr != nil {
		return LibraryLifecycleGenerationChanged, errors.Join(fmt.Errorf("%w: delete trashed library %s: %w", ErrLibraryLifecycleOutcomeUnknown, libraryID, casErr), fmt.Errorf("settlement read failed: %w", readErr))
	}
	if !present {
		return LibraryLifecycleApplied, nil
	}
	if current.Equal(deletedAt) {
		return LibraryLifecycleGenerationChanged, fmt.Errorf("%w: delete trashed library %s: %w", ErrLibraryLifecycleOutcomeUnknown, libraryID, casErr)
	}
	return LibraryLifecycleGenerationChanged, nil
}

// RestoreTrashedLibraryGeneration clears deleted_at / deleted_by on the
// canonical row only while it is still trashed under generation deletedAt. It
// is the final, fenced lifecycle mutation of a restore: a conditional UPDATE
// never creates a row, so a restore owner that lost the lease cannot resurrect
// a library another owner permanently deleted, and it cannot restore a newer
// trash generation.
//
// Outcomes: Applied; TargetAbsent (permanently deleted); GenerationChanged.
//
// On an ambiguous LWT outcome a SERIAL read settles it. An active row reports
// Applied (the restore of this generation is committed; the completion writes
// are still due). A row still at deletedAt reports
// ErrLibraryLifecycleOutcomeUnknown.
func RestoreTrashedLibraryGeneration(session *gocql.Session, orgID, libraryID string, deletedAt, updatedAt time.Time) (LibraryLifecycleOutcome, error) {
	if deletedAt.IsZero() {
		return LibraryLifecycleGenerationChanged, fmt.Errorf("restore trashed library %s: zero deleted_at generation", libraryID)
	}
	previous := map[string]interface{}{}
	applied, err := session.Query(`
		UPDATE libraries SET updated_at = ?, deleted_at = null, deleted_by = null
		WHERE org_id = ? AND library_id = ?
		IF deleted_at = ?
	`, updatedAt, orgID, libraryID, deletedAt).
		SerialConsistency(LibraryHeadSerialConsistency).
		MapScanCAS(previous)
	if err == nil {
		if applied {
			return LibraryLifecycleApplied, nil
		}
		if present, _ := casDeletedAt(previous); !present {
			return LibraryLifecycleTargetAbsent, nil
		}
		return LibraryLifecycleGenerationChanged, nil
	}
	if !isAmbiguousLibraryLifecycleCASError(err) {
		return LibraryLifecycleGenerationChanged, fmt.Errorf("restore trashed library %s: %w", libraryID, err)
	}
	present, current, readErr := readLibraryDeletedAtSerial(session, orgID, libraryID)
	return settleTrashedLibraryRestore(libraryID, deletedAt, err, present, current, readErr)
}

// settleTrashedLibraryRestore settles an ambiguous RestoreTrashedLibraryGeneration
// from a SERIAL read of the canonical row.
func settleTrashedLibraryRestore(libraryID string, deletedAt time.Time, casErr error, present bool, current time.Time, readErr error) (LibraryLifecycleOutcome, error) {
	if readErr != nil {
		return LibraryLifecycleGenerationChanged, errors.Join(fmt.Errorf("%w: restore trashed library %s: %w", ErrLibraryLifecycleOutcomeUnknown, libraryID, casErr), fmt.Errorf("settlement read failed: %w", readErr))
	}
	switch {
	case !present:
		return LibraryLifecycleTargetAbsent, nil
	case current.IsZero():
		return LibraryLifecycleApplied, nil
	case current.Equal(deletedAt):
		return LibraryLifecycleGenerationChanged, fmt.Errorf("%w: restore trashed library %s: %w", ErrLibraryLifecycleOutcomeUnknown, libraryID, casErr)
	default:
		return LibraryLifecycleGenerationChanged, nil
	}
}

// LibraryLifecycleCompletionStamp returns the client timestamp (microseconds)
// for the derived writes of a lifecycle transition, taken before the
// transition: now, raised above the last write to the library's admin
// read-model row. Every transition writes that row in its completion, so a
// node whose clock is behind still stamps its derived writes after the
// previous transition's.
func LibraryLifecycleCompletionStamp(session *gocql.Session, orgID, libraryID string, now time.Time) int64 {
	stamp := now.UnixMicro()
	var last int64
	if err := session.Query(`SELECT WRITETIME(updated_at) FROM libraries_by_org_updated WHERE org_id = ? AND library_id = ?`,
		orgID, libraryID).Scan(&last); err == nil && last >= stamp {
		stamp = last + 1
	}
	return stamp
}

// ReadLibraryLifecycleStateSerial reads the canonical lifecycle state in the
// library's Paxos domain: whether the row exists and its deleted_at (zero when
// active).
func ReadLibraryLifecycleStateSerial(session *gocql.Session, orgID, libraryID string) (bool, time.Time, error) {
	return readLibraryDeletedAtSerial(session, orgID, libraryID)
}

// ClearStaleSoftDeleteMarker removes the soft-delete marker of generation
// deletedAt from a library that is active again. A restore that committed its
// canonical transition but not its completion leaves exactly that state; the
// GC cascade calls this after its fenced delete reported a changed generation.
// It does nothing while the row is absent or trashed, or when the marker holds
// another generation or a purge request. The tombstone is stamped with the
// marker's own write timestamp, so it removes that write and nothing written
// after it.
func ClearStaleSoftDeleteMarker(session *gocql.Session, orgID, libraryID string, deletedAt time.Time) error {
	present, current, err := readLibraryDeletedAtSerial(session, orgID, libraryID)
	if err != nil {
		return fmt.Errorf("read canonical library %s before clearing a stale marker: %w", libraryID, err)
	}
	if !present || !current.IsZero() {
		return nil
	}
	var markerDeletedAt, purgeRequestedAt time.Time
	var writeTime int64
	err = session.Query(`SELECT deleted_at, purge_requested_at, WRITETIME(deleted_at) FROM deleted_libraries WHERE library_id = ?`,
		libraryID).Scan(&markerDeletedAt, &purgeRequestedAt, &writeTime)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read deleted library marker %s: %w", libraryID, err)
	}
	if !markerDeletedAt.Equal(deletedAt) || !purgeRequestedAt.IsZero() || writeTime == 0 {
		return nil
	}
	if err := session.Query(`DELETE FROM deleted_libraries USING TIMESTAMP ? WHERE library_id = ?`, writeTime, libraryID).Exec(); err != nil {
		return fmt.Errorf("clear stale deleted library marker %s: %w", libraryID, err)
	}
	return nil
}
