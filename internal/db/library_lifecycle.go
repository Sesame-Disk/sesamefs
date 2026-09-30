package db

import (
	"errors"
	"fmt"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

// Lifecycle of a library (ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01).
//
// The canonical lifecycle cells (libraries.deleted_at / deleted_by /
// lifecycle_at and the row's existence) are written only by global-SERIAL LWTs
// pinned to LibraryHeadSerialConsistency, the partition's existing Paxos
// domain: soft delete, restore and hard delete. Paxos ballots on a partition
// only grow, so a transition that commits later carries the larger write
// timestamp whatever the clocks say.
//
// libraries.lifecycle_at is the library's lifecycle clock. Soft delete and
// restore advance it strictly (NextLibraryLifecycleAt) inside their LWT, and
// soft delete uses the new value as deleted_at, so a trash generation is unique
// per library: a stale owner holding generation D1 can never match a later
// generation D2, even one created in the same millisecond. Each transition also
// stamps its derived writes (marker, admin read model, lookup) with
// LibraryLifecycleWriteTimestamp(its lifecycle_at): canonical transition order
// is derived-write order, whenever a paused, retried or repaired completion
// lands, on any node and in any datacenter.

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

// LibraryLifecycleState is the canonical lifecycle state of a library, read
// in its Paxos domain.
type LibraryLifecycleState struct {
	Present     bool
	DeletedAt   time.Time // zero when active
	LifecycleAt time.Time // zero before the first lifecycle transition
}

// ReadLibraryLifecycleSerial reads the canonical lifecycle state at SERIAL, so
// it observes every committed lifecycle LWT.
func ReadLibraryLifecycleSerial(session *gocql.Session, orgID, libraryID string) (LibraryLifecycleState, error) {
	var state LibraryLifecycleState
	err := session.Query(`SELECT deleted_at, lifecycle_at FROM libraries WHERE org_id = ? AND library_id = ?`,
		orgID, libraryID).Consistency(LibraryHeadSerialConsistency).Scan(&state.DeletedAt, &state.LifecycleAt)
	if errors.Is(err, gocql.ErrNotFound) {
		return LibraryLifecycleState{}, nil
	}
	if err != nil {
		return LibraryLifecycleState{}, err
	}
	state.Present = true
	return state, nil
}

// NextLibraryLifecycleAt is the lifecycle clock value of the next transition:
// now at the millisecond precision of a Cassandra timestamp, raised to one
// millisecond after the previous value when the clock has not passed it.
func NextLibraryLifecycleAt(previous, now time.Time) time.Time {
	next := now.UTC().Truncate(time.Millisecond)
	if !previous.IsZero() && !next.After(previous) {
		next = previous.UTC().Truncate(time.Millisecond).Add(time.Millisecond)
	}
	return next
}

// LibraryLifecycleWriteTimestamp is the write timestamp (microseconds) of the
// derived writes of the lifecycle transition at lifecycleAt.
func LibraryLifecycleWriteTimestamp(lifecycleAt time.Time) int64 {
	return lifecycleAt.UnixMicro()
}

// softDeleteAttempts bounds the retries of a soft delete whose lifecycle clock
// moved between its SERIAL read and its LWT (a concurrent restore/re-trash).
const softDeleteAttempts = 5

// SoftDeleteLibraryGeneration moves an active library to the trash under a new,
// unique generation: deleted_at = lifecycle_at = NextLibraryLifecycleAt(previous
// lifecycle_at, now), by an LWT conditioned on the row being active, existing
// (created_at != null, so no ghost row) and still at the lifecycle clock value it
// read. It returns the generation it created, or the current one when the
// library is already in the trash.
//
// Outcomes: Applied; TargetAbsent (no library); GenerationChanged (already in
// the trash). An ambiguous outcome is settled by a SERIAL read: the row at the
// new generation reports Applied; a row still at the previous state reports
// ErrLibraryLifecycleOutcomeUnknown.
func SoftDeleteLibraryGeneration(session *gocql.Session, orgID, libraryID, deletedBy string, now time.Time) (time.Time, LibraryLifecycleOutcome, error) {
	for attempt := 0; attempt < softDeleteAttempts; attempt++ {
		state, err := ReadLibraryLifecycleSerial(session, orgID, libraryID)
		if err != nil {
			return time.Time{}, LibraryLifecycleGenerationChanged, fmt.Errorf("read library %s before soft delete: %w", libraryID, err)
		}
		if !state.Present {
			return time.Time{}, LibraryLifecycleTargetAbsent, nil
		}
		if !state.DeletedAt.IsZero() {
			return state.DeletedAt, LibraryLifecycleGenerationChanged, nil
		}
		deletedAt := NextLibraryLifecycleAt(state.LifecycleAt, now)
		var previousLifecycleAt interface{}
		if !state.LifecycleAt.IsZero() {
			previousLifecycleAt = state.LifecycleAt
		}
		previous := map[string]interface{}{}
		applied, err := session.Query(`
			UPDATE libraries SET deleted_at = ?, deleted_by = ?, updated_at = ?, lifecycle_at = ?
			WHERE org_id = ? AND library_id = ?
			IF deleted_at = null AND created_at != null AND lifecycle_at = ?
		`, deletedAt, deletedBy, deletedAt, deletedAt, orgID, libraryID, previousLifecycleAt).
			SerialConsistency(LibraryHeadSerialConsistency).
			MapScanCAS(previous)
		if err == nil {
			if applied {
				return deletedAt, LibraryLifecycleApplied, nil
			}
			if createdAt, ok := previous["created_at"].(time.Time); !ok || createdAt.IsZero() {
				return time.Time{}, LibraryLifecycleTargetAbsent, nil
			}
			continue // the lifecycle moved; the next read decides
		}
		if !isAmbiguousLibraryLifecycleCASError(err) {
			return time.Time{}, LibraryLifecycleGenerationChanged, fmt.Errorf("soft-delete library %s: %w", libraryID, err)
		}
		settled, readErr := ReadLibraryLifecycleSerial(session, orgID, libraryID)
		outcome, settleErr := settleLibrarySoftDelete(libraryID, deletedAt, state, err, settled, readErr)
		switch {
		case settleErr != nil:
			return time.Time{}, outcome, settleErr
		case outcome == LibraryLifecycleApplied:
			return deletedAt, outcome, nil
		case outcome == LibraryLifecycleTargetAbsent:
			return time.Time{}, outcome, nil
		case !settled.DeletedAt.IsZero():
			return settled.DeletedAt, outcome, nil
		}
		// Active again under a moved lifecycle clock: retry against the new state.
	}
	return time.Time{}, LibraryLifecycleGenerationChanged, fmt.Errorf("soft-delete library %s: lifecycle kept changing", libraryID)
}

// settleLibrarySoftDelete settles an ambiguous SoftDeleteLibraryGeneration from
// a SERIAL read taken after it.
func settleLibrarySoftDelete(libraryID string, deletedAt time.Time, before LibraryLifecycleState, casErr error, after LibraryLifecycleState, readErr error) (LibraryLifecycleOutcome, error) {
	if readErr != nil {
		return LibraryLifecycleGenerationChanged, errors.Join(fmt.Errorf("%w: soft-delete library %s: %w", ErrLibraryLifecycleOutcomeUnknown, libraryID, casErr), fmt.Errorf("settlement read failed: %w", readErr))
	}
	switch {
	case !after.Present:
		return LibraryLifecycleTargetAbsent, nil
	case after.DeletedAt.Equal(deletedAt):
		return LibraryLifecycleApplied, nil
	case after.DeletedAt.IsZero() && after.LifecycleAt.Equal(before.LifecycleAt):
		return LibraryLifecycleGenerationChanged, fmt.Errorf("%w: soft-delete library %s: %w", ErrLibraryLifecycleOutcomeUnknown, libraryID, casErr)
	default:
		// Another transition committed (a different trash generation, or a
		// restore that moved the clock): not ours.
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
// canonical row only while it is still trashed under generation deletedAt, and
// advances the lifecycle clock past it. It returns the restore's lifecycle
// value (the timestamp of its derived writes). It is the final, fenced
// lifecycle mutation of a restore: a conditional UPDATE never creates a row, so
// a restore owner that lost the lease cannot resurrect a library another owner
// permanently deleted, and it cannot restore a newer trash generation.
//
// Outcomes: Applied; TargetAbsent (permanently deleted); GenerationChanged.
//
// On an ambiguous LWT outcome a SERIAL read settles it. An active row at the
// restore's lifecycle value reports Applied (the completion writes are still
// due). A row still at deletedAt reports ErrLibraryLifecycleOutcomeUnknown.
func RestoreTrashedLibraryGeneration(session *gocql.Session, orgID, libraryID string, deletedAt, now time.Time) (time.Time, LibraryLifecycleOutcome, error) {
	if deletedAt.IsZero() {
		return time.Time{}, LibraryLifecycleGenerationChanged, fmt.Errorf("restore trashed library %s: zero deleted_at generation", libraryID)
	}
	// A trashed row carries lifecycle_at = deleted_at (set by the same soft-delete
	// LWT), so the restore's value is strictly after it.
	restoredAt := NextLibraryLifecycleAt(deletedAt, now)
	previous := map[string]interface{}{}
	applied, err := session.Query(`
		UPDATE libraries SET updated_at = ?, deleted_at = null, deleted_by = null, lifecycle_at = ?
		WHERE org_id = ? AND library_id = ?
		IF deleted_at = ?
	`, restoredAt, restoredAt, orgID, libraryID, deletedAt).
		SerialConsistency(LibraryHeadSerialConsistency).
		MapScanCAS(previous)
	if err == nil {
		if applied {
			return restoredAt, LibraryLifecycleApplied, nil
		}
		if present, _ := casDeletedAt(previous); !present {
			return time.Time{}, LibraryLifecycleTargetAbsent, nil
		}
		return time.Time{}, LibraryLifecycleGenerationChanged, nil
	}
	if !isAmbiguousLibraryLifecycleCASError(err) {
		return time.Time{}, LibraryLifecycleGenerationChanged, fmt.Errorf("restore trashed library %s: %w", libraryID, err)
	}
	settled, readErr := ReadLibraryLifecycleSerial(session, orgID, libraryID)
	outcome, settleErr := settleTrashedLibraryRestore(libraryID, deletedAt, restoredAt, err, settled, readErr)
	if outcome == LibraryLifecycleApplied {
		return restoredAt, outcome, settleErr
	}
	return time.Time{}, outcome, settleErr
}

// settleTrashedLibraryRestore settles an ambiguous RestoreTrashedLibraryGeneration
// from a SERIAL read of the canonical row.
func settleTrashedLibraryRestore(libraryID string, deletedAt, restoredAt time.Time, casErr error, after LibraryLifecycleState, readErr error) (LibraryLifecycleOutcome, error) {
	if readErr != nil {
		return LibraryLifecycleGenerationChanged, errors.Join(fmt.Errorf("%w: restore trashed library %s: %w", ErrLibraryLifecycleOutcomeUnknown, libraryID, casErr), fmt.Errorf("settlement read failed: %w", readErr))
	}
	switch {
	case !after.Present:
		return LibraryLifecycleTargetAbsent, nil
	case after.DeletedAt.IsZero() && after.LifecycleAt.Equal(restoredAt):
		return LibraryLifecycleApplied, nil
	case after.DeletedAt.Equal(deletedAt):
		return LibraryLifecycleGenerationChanged, fmt.Errorf("%w: restore trashed library %s: %w", ErrLibraryLifecycleOutcomeUnknown, libraryID, casErr)
	default:
		return LibraryLifecycleGenerationChanged, nil
	}
}

// ClearRestoredLibraryMarker repeats the marker removal of the restore that
// made an active library active: DELETE of its deleted_libraries marker at the
// restore's own lifecycle write timestamp. A restore that committed its
// canonical transition but not its completion leaves the old marker behind; the
// GC cascade calls this after its fenced delete reported a changed generation.
// It does nothing while the row is absent or trashed. A marker written by a
// later transition carries a later timestamp and survives.
func ClearRestoredLibraryMarker(session *gocql.Session, orgID, libraryID string) error {
	state, err := ReadLibraryLifecycleSerial(session, orgID, libraryID)
	if err != nil {
		return fmt.Errorf("read canonical library %s before clearing a restored marker: %w", libraryID, err)
	}
	if !state.Present || !state.DeletedAt.IsZero() || state.LifecycleAt.IsZero() {
		return nil
	}
	if err := session.Query(`DELETE FROM deleted_libraries USING TIMESTAMP ? WHERE library_id = ?`,
		LibraryLifecycleWriteTimestamp(state.LifecycleAt), libraryID).Exec(); err != nil {
		return fmt.Errorf("clear restored library marker %s: %w", libraryID, err)
	}
	return nil
}

// ExecLibraryLifecycleCompletionFn executes the completion batch of a
// lifecycle transition (soft delete, restore, permanent delete, GC hard
// delete). It is a variable so integration tests can fail a completion after
// its canonical transition applied.
var ExecLibraryLifecycleCompletionFn = func(batch *gocql.Batch) error { return batch.Exec() }

// AddTrashedLibraryDerivedStateQueries adds the derived state of trash
// generation row.DeletedAt: its soft-delete marker, its admin read-model rows,
// and the removal of trash read-model rows of other generations. The caller
// stamps the batch with LibraryLifecycleWriteTimestamp(*row.DeletedAt), so the
// writes repeat the generation's own completion and lose to any later one.
func AddTrashedLibraryDerivedStateQueries(session *gocql.Session, batch *gocql.Batch, row AdminLibraryProjectionRow, blockRepresentationID string) error {
	if row.DeletedAt == nil || row.DeletedAt.IsZero() {
		return fmt.Errorf("trashed library %s derived state without a generation", row.LibraryID)
	}
	batch.Query(`
		INSERT INTO deleted_libraries (library_id, org_id, deleted_at, storage_class, block_representation_id)
		VALUES (?, ?, ?, ?, ?)`,
		row.LibraryID, row.OrgID, *row.DeletedAt, row.StorageClass, blockRepresentationID)
	AddUpsertAdminLibraryReadModelQuery(batch, row)
	return addDeleteOtherTrashRowsQueries(session, batch, row.OrgID, row.LibraryID, *row.DeletedAt)
}

// AddRestoredLibraryDerivedStateQueries adds the derived state of an active
// library after a restore: no marker, active admin read-model rows, no trash
// read-model rows. The caller stamps the batch with the restore's
// LibraryLifecycleWriteTimestamp.
func AddRestoredLibraryDerivedStateQueries(session *gocql.Session, batch *gocql.Batch, row AdminLibraryProjectionRow) error {
	if row.DeletedAt != nil {
		return fmt.Errorf("restored library %s derived state with a trash generation", row.LibraryID)
	}
	batch.Query(`DELETE FROM deleted_libraries WHERE library_id = ?`, row.LibraryID)
	AddUpsertAdminLibraryReadModelQuery(batch, row)
	return addDeleteOtherTrashRowsQueries(session, batch, row.OrgID, row.LibraryID, time.Time{})
}

func addDeleteOtherTrashRowsQueries(session *gocql.Session, batch *gocql.Batch, orgID, libraryID string, keep time.Time) error {
	rows, err := ListDeletedAdminLibraryRowsByOrg(session, orgID)
	if err != nil {
		return err
	}
	for _, trashRow := range rows {
		if trashRow.LibraryID == libraryID && (keep.IsZero() || !trashRow.DeletedAt.Equal(keep)) {
			AddDeleteDeletedAdminLibraryReadModelQuery(batch, trashRow)
		}
	}
	return nil
}

// RepairLibraryLifecycleDerivedState rewrites the derived state (marker and
// admin read model) of the library's current canonical lifecycle generation,
// taking the canonical row as the only authority: a trashed row gets its own
// generation's marker (replacing a marker of any older generation) and trashed
// read model; an active row that went through a restore loses its marker and
// gets an active read model. The writes carry the generation's own lifecycle
// timestamp, so they are idempotent and never override a later transition. It
// does nothing for an absent row (see the permanent-delete resume) or for a row
// whose canonical read at session consistency does not show the SERIAL state yet.
func RepairLibraryLifecycleDerivedState(session *gocql.Session, orgID, libraryID string, resolveBlockRepresentation func() string) error {
	state, err := ReadLibraryLifecycleSerial(session, orgID, libraryID)
	if err != nil {
		return fmt.Errorf("read canonical library %s for repair: %w", libraryID, err)
	}
	if !state.Present {
		return nil
	}
	row, err := ReadAdminLibraryProjectionRow(session, orgID, libraryID)
	if err != nil {
		return fmt.Errorf("read canonical library %s for repair: %w", libraryID, err)
	}
	var batch *gocql.Batch
	if !state.DeletedAt.IsZero() {
		if row.DeletedAt == nil || !row.DeletedAt.Equal(state.DeletedAt) {
			return nil
		}
		batch = session.Batch(gocql.LoggedBatch).WithTimestamp(LibraryLifecycleWriteTimestamp(state.DeletedAt))
		if err := AddTrashedLibraryDerivedStateQueries(session, batch, row, resolveBlockRepresentation()); err != nil {
			return err
		}
	} else {
		if state.LifecycleAt.IsZero() || row.DeletedAt != nil {
			return nil
		}
		batch = session.Batch(gocql.LoggedBatch).WithTimestamp(LibraryLifecycleWriteTimestamp(state.LifecycleAt))
		if err := AddRestoredLibraryDerivedStateQueries(session, batch, row); err != nil {
			return err
		}
	}
	if err := ExecLibraryLifecycleCompletionFn(batch); err != nil {
		return fmt.Errorf("repair library %s derived state: %w", libraryID, err)
	}
	return nil
}
