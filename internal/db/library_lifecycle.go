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
// domain: soft delete, restore, hard delete, and the reaper's fence of an
// abandoned attempt. Paxos ballots on a partition only grow, so a transition
// that commits later carries the larger write timestamp whatever the clocks say.
//
// libraries.lifecycle_at is the library's lifecycle clock. Every lifecycle LWT
// is conditioned on the clock value it read, and soft delete, restore and the
// fence advance it strictly (NextLibraryLifecycleAt). Soft delete uses the new
// value as deleted_at, so a trash generation is unique per library: a stale
// owner holding generation D1 can never match a later generation D2, even one
// created in the same millisecond.
//
// Derived state is written in two parts (see AddTrashedLifecycleOwnedQueries):
// lifecycle-owned rows (the deleted_libraries marker and the org trash listing)
// are stamped with LibraryLifecycleWriteTimestamp(lifecycle value), so canonical
// transition order is their write order wherever a paused, retried or repaired
// completion lands; the ordinary owner/org/global read-model rows carry client
// timestamps like every other writer of those columns, and are published only
// from a SERIAL snapshot that a second SERIAL read proves current
// (publishLibraryReadModel).
//
// Each attempt records a durable continuation (library_lifecycle_pending) before
// its LWT; see LibraryLifecyclePending for its identity and retirement.

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
	return SoftDeleteLibraryGenerationWithIntent(session, orgID, libraryID, deletedBy, now, nil)
}

// SoftDeleteLibraryGenerationWithIntent is SoftDeleteLibraryGeneration with a
// hook that runs before each LWT attempt with the state it read and the
// generation it is about to create: the caller records its durable
// continuation there, before the canonical transition can commit.
func SoftDeleteLibraryGenerationWithIntent(session *gocql.Session, orgID, libraryID, deletedBy string, now time.Time, beforeTransition func(previous LibraryLifecycleState, deletedAt time.Time) error) (time.Time, LibraryLifecycleOutcome, error) {
	for attempt := 0; attempt < lifecycleTransitionAttempts; attempt++ {
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
		if beforeTransition != nil {
			if err := beforeTransition(state, deletedAt); err != nil {
				return time.Time{}, LibraryLifecycleGenerationChanged, err
			}
		}
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

// lifecycleTransitionAttempts bounds the retries of a lifecycle transition
// whose lifecycle clock moved between its SERIAL read and its LWT while its
// generation stayed the same (a concurrent transition, or the reaper's fence of
// an abandoned attempt).
const lifecycleTransitionAttempts = 5

// laterLifecycleValue is the value a transition of a row in state must exceed:
// its lifecycle clock, or its trash generation for a row trashed before the
// clock existed.
func laterLifecycleValue(state LibraryLifecycleState) time.Time {
	if state.DeletedAt.After(state.LifecycleAt) {
		return state.DeletedAt
	}
	return state.LifecycleAt
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
// in flight). A row still at its precondition reports
// ErrLibraryLifecycleOutcomeUnknown.
func DeleteTrashedLibraryGeneration(session *gocql.Session, orgID, libraryID string, deletedAt time.Time) (LibraryLifecycleOutcome, error) {
	return DeleteTrashedLibraryGenerationWithIntent(session, orgID, libraryID, deletedAt, nil)
}

// DeleteTrashedLibraryGenerationWithIntent is DeleteTrashedLibraryGeneration
// with a hook that runs before each LWT attempt with the state it read: the
// caller records its durable continuation there. The LWT is also conditioned on
// that state's lifecycle clock, so the reaper can fence an abandoned attempt
// (FenceLibraryLifecycleAttempt); an attempt whose clock moved while its
// generation stayed is retried.
func DeleteTrashedLibraryGenerationWithIntent(session *gocql.Session, orgID, libraryID string, deletedAt time.Time, beforeTransition func(previous LibraryLifecycleState) error) (LibraryLifecycleOutcome, error) {
	if deletedAt.IsZero() {
		return LibraryLifecycleGenerationChanged, fmt.Errorf("delete trashed library %s: zero deleted_at generation", libraryID)
	}
	for attempt := 0; attempt < lifecycleTransitionAttempts; attempt++ {
		state, err := ReadLibraryLifecycleSerial(session, orgID, libraryID)
		if err != nil {
			return LibraryLifecycleGenerationChanged, fmt.Errorf("read library %s before delete: %w", libraryID, err)
		}
		if !state.Present {
			return LibraryLifecycleTargetAbsent, nil
		}
		if !state.DeletedAt.Equal(deletedAt) {
			return LibraryLifecycleGenerationChanged, nil
		}
		if beforeTransition != nil {
			if err := beforeTransition(state); err != nil {
				return LibraryLifecycleGenerationChanged, err
			}
		}
		previous := map[string]interface{}{}
		applied, err := session.Query(`
			DELETE FROM libraries WHERE org_id = ? AND library_id = ?
			IF deleted_at = ? AND lifecycle_at = ?
		`, orgID, libraryID, deletedAt, nullableTime(state.LifecycleAt)).
			SerialConsistency(LibraryHeadSerialConsistency).
			MapScanCAS(previous)
		if err == nil {
			if applied {
				return LibraryLifecycleApplied, nil
			}
			present, current := casDeletedAt(previous)
			if !present {
				return LibraryLifecycleTargetAbsent, nil
			}
			if !current.Equal(deletedAt) {
				return LibraryLifecycleGenerationChanged, nil
			}
			continue // only the lifecycle clock moved; the next read decides
		}
		if !isAmbiguousLibraryLifecycleCASError(err) {
			return LibraryLifecycleGenerationChanged, fmt.Errorf("delete trashed library %s: %w", libraryID, err)
		}
		settled, readErr := ReadLibraryLifecycleSerial(session, orgID, libraryID)
		outcome, settleErr := settleTrashedLibraryDelete(libraryID, deletedAt, state, err, settled, readErr)
		if settleErr != nil || outcome != LibraryLifecycleGenerationChanged || !settled.DeletedAt.Equal(deletedAt) {
			return outcome, settleErr
		}
		// Same generation under a moved lifecycle clock: retry against the new state.
	}
	return LibraryLifecycleGenerationChanged, fmt.Errorf("delete trashed library %s: lifecycle kept changing", libraryID)
}

// settleTrashedLibraryDelete settles an ambiguous DeleteTrashedLibraryGeneration
// attempt made from state before, from a SERIAL read taken after it.
func settleTrashedLibraryDelete(libraryID string, deletedAt time.Time, before LibraryLifecycleState, casErr error, after LibraryLifecycleState, readErr error) (LibraryLifecycleOutcome, error) {
	if readErr != nil {
		return LibraryLifecycleGenerationChanged, errors.Join(fmt.Errorf("%w: delete trashed library %s: %w", ErrLibraryLifecycleOutcomeUnknown, libraryID, casErr), fmt.Errorf("settlement read failed: %w", readErr))
	}
	switch {
	case !after.Present:
		return LibraryLifecycleApplied, nil
	case after.DeletedAt.Equal(deletedAt) && after.LifecycleAt.Equal(before.LifecycleAt):
		return LibraryLifecycleGenerationChanged, fmt.Errorf("%w: delete trashed library %s: %w", ErrLibraryLifecycleOutcomeUnknown, libraryID, casErr)
	default:
		return LibraryLifecycleGenerationChanged, nil
	}
}

// RestoreTrashedLibraryGeneration clears deleted_at / deleted_by on the
// canonical row only while it is still trashed under generation deletedAt, and
// advances the lifecycle clock past it. It returns the restore's lifecycle
// value (the timestamp of its lifecycle-owned writes). It is the final, fenced
// lifecycle mutation of a restore: a conditional UPDATE never creates a row, so
// a restore owner that lost the lease cannot resurrect a library another owner
// permanently deleted, and it cannot restore a newer trash generation.
//
// Outcomes: Applied; TargetAbsent (permanently deleted); GenerationChanged.
//
// On an ambiguous LWT outcome a SERIAL read settles it. An active row at the
// restore's lifecycle value reports Applied (the completion writes are still
// due). A row still at its precondition reports ErrLibraryLifecycleOutcomeUnknown.
func RestoreTrashedLibraryGeneration(session *gocql.Session, orgID, libraryID string, deletedAt, now time.Time) (time.Time, LibraryLifecycleOutcome, error) {
	return RestoreTrashedLibraryGenerationWithIntent(session, orgID, libraryID, deletedAt, now, nil)
}

// RestoreTrashedLibraryGenerationWithIntent is RestoreTrashedLibraryGeneration
// with a hook that runs before each LWT attempt with the state it read and the
// lifecycle value it is about to set: the caller records its durable
// continuation there. The LWT is also conditioned on that state's lifecycle
// clock (see DeleteTrashedLibraryGenerationWithIntent).
func RestoreTrashedLibraryGenerationWithIntent(session *gocql.Session, orgID, libraryID string, deletedAt, now time.Time, beforeTransition func(previous LibraryLifecycleState, restoredAt time.Time) error) (time.Time, LibraryLifecycleOutcome, error) {
	if deletedAt.IsZero() {
		return time.Time{}, LibraryLifecycleGenerationChanged, fmt.Errorf("restore trashed library %s: zero deleted_at generation", libraryID)
	}
	for attempt := 0; attempt < lifecycleTransitionAttempts; attempt++ {
		state, err := ReadLibraryLifecycleSerial(session, orgID, libraryID)
		if err != nil {
			return time.Time{}, LibraryLifecycleGenerationChanged, fmt.Errorf("read library %s before restore: %w", libraryID, err)
		}
		if !state.Present {
			return time.Time{}, LibraryLifecycleTargetAbsent, nil
		}
		if !state.DeletedAt.Equal(deletedAt) {
			return time.Time{}, LibraryLifecycleGenerationChanged, nil
		}
		restoredAt := NextLibraryLifecycleAt(laterLifecycleValue(state), now)
		if beforeTransition != nil {
			if err := beforeTransition(state, restoredAt); err != nil {
				return time.Time{}, LibraryLifecycleGenerationChanged, err
			}
		}
		previous := map[string]interface{}{}
		applied, err := session.Query(`
			UPDATE libraries SET updated_at = ?, deleted_at = null, deleted_by = null, lifecycle_at = ?
			WHERE org_id = ? AND library_id = ?
			IF deleted_at = ? AND lifecycle_at = ?
		`, restoredAt, restoredAt, orgID, libraryID, deletedAt, nullableTime(state.LifecycleAt)).
			SerialConsistency(LibraryHeadSerialConsistency).
			MapScanCAS(previous)
		if err == nil {
			if applied {
				return restoredAt, LibraryLifecycleApplied, nil
			}
			present, current := casDeletedAt(previous)
			if !present {
				return time.Time{}, LibraryLifecycleTargetAbsent, nil
			}
			if !current.Equal(deletedAt) {
				return time.Time{}, LibraryLifecycleGenerationChanged, nil
			}
			continue // only the lifecycle clock moved; the next read decides
		}
		if !isAmbiguousLibraryLifecycleCASError(err) {
			return time.Time{}, LibraryLifecycleGenerationChanged, fmt.Errorf("restore trashed library %s: %w", libraryID, err)
		}
		settled, readErr := ReadLibraryLifecycleSerial(session, orgID, libraryID)
		outcome, settleErr := settleTrashedLibraryRestore(libraryID, deletedAt, restoredAt, state, err, settled, readErr)
		if outcome == LibraryLifecycleApplied {
			return restoredAt, outcome, settleErr
		}
		if settleErr != nil || outcome != LibraryLifecycleGenerationChanged || !settled.DeletedAt.Equal(deletedAt) {
			return time.Time{}, outcome, settleErr
		}
		// Same generation under a moved lifecycle clock: retry against the new state.
	}
	return time.Time{}, LibraryLifecycleGenerationChanged, fmt.Errorf("restore trashed library %s: lifecycle kept changing", libraryID)
}

// settleTrashedLibraryRestore settles an ambiguous RestoreTrashedLibraryGeneration
// attempt made from state before, from a SERIAL read taken after it.
func settleTrashedLibraryRestore(libraryID string, deletedAt, restoredAt time.Time, before LibraryLifecycleState, casErr error, after LibraryLifecycleState, readErr error) (LibraryLifecycleOutcome, error) {
	if readErr != nil {
		return LibraryLifecycleGenerationChanged, errors.Join(fmt.Errorf("%w: restore trashed library %s: %w", ErrLibraryLifecycleOutcomeUnknown, libraryID, casErr), fmt.Errorf("settlement read failed: %w", readErr))
	}
	switch {
	case !after.Present:
		return LibraryLifecycleTargetAbsent, nil
	case after.DeletedAt.IsZero() && after.LifecycleAt.Equal(restoredAt):
		return LibraryLifecycleApplied, nil
	case after.DeletedAt.Equal(deletedAt) && after.LifecycleAt.Equal(before.LifecycleAt):
		return LibraryLifecycleGenerationChanged, fmt.Errorf("%w: restore trashed library %s: %w", ErrLibraryLifecycleOutcomeUnknown, libraryID, casErr)
	default:
		return LibraryLifecycleGenerationChanged, nil
	}
}

// FenceLibraryLifecycleAttempt makes an abandoned attempt (its producer died
// before its LWT, or the LWT failed without applying) unable to apply, so its
// continuation can be retired. While the attempt's precondition still holds, a
// global-SERIAL LWT conditioned on that same precondition advances the
// library's lifecycle clock and changes nothing else; every lifecycle LWT is
// conditioned on the clock value it read, so the attempt's LWT can no longer
// commit, and a proposal of it still pending in Paxos is superseded by the
// newer commit. A live producer whose attempt is fenced sees a moved clock and
// retries under a new attempt. It returns the canonical state after the fence;
// an LWT error is returned as is (retry later).
func FenceLibraryLifecycleAttempt(session *gocql.Session, p LibraryLifecyclePending, now time.Time) (LibraryLifecycleState, error) {
	state, err := ReadLibraryLifecycleSerial(session, p.OrgID, p.LibraryID)
	if err != nil || !p.CanStillApply(state) {
		return state, err
	}
	fencedAt := NextLibraryLifecycleAt(laterLifecycleValue(state), now)
	previous := map[string]interface{}{}
	if p.Operation == LibraryLifecycleOpSoftDelete {
		// IF deleted_at = null holds for a missing row too: created_at != null
		// keeps the fence from creating a ghost row.
		_, err = session.Query(`
			UPDATE libraries SET lifecycle_at = ? WHERE org_id = ? AND library_id = ?
			IF deleted_at = null AND created_at != null AND lifecycle_at = ?
		`, fencedAt, p.OrgID, p.LibraryID, nullableTime(state.LifecycleAt)).
			SerialConsistency(LibraryHeadSerialConsistency).
			MapScanCAS(previous)
	} else {
		_, err = session.Query(`
			UPDATE libraries SET lifecycle_at = ? WHERE org_id = ? AND library_id = ?
			IF deleted_at = ? AND lifecycle_at = ?
		`, fencedAt, p.OrgID, p.LibraryID, state.DeletedAt, nullableTime(state.LifecycleAt)).
			SerialConsistency(LibraryHeadSerialConsistency).
			MapScanCAS(previous)
	}
	if err != nil {
		return state, fmt.Errorf("fence abandoned %s attempt on library %s: %w", p.Operation, p.LibraryID, err)
	}
	return ReadLibraryLifecycleSerial(session, p.OrgID, p.LibraryID)
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

// The derived state of a lifecycle transition is written in two parts.
//
//   - Lifecycle-owned rows — the deleted_libraries marker and the org trash
//     listing rows — are written only by lifecycle transitions and are stamped
//     with LibraryLifecycleWriteTimestamp(lifecycle value), so they are ordered
//     like the canonical transitions whatever the clocks.
//   - The owner, org and global read-model rows hold ordinary mutable columns
//     (owner, name, size...) that other writers update with client timestamps;
//     a lifecycle completion writes them with its client timestamp too, never
//     with the lifecycle clock (which can run ahead of real time), and only
//     from a SERIAL snapshot taken after its LWT that a second SERIAL read
//     confirms (publishLibraryReadModel), so a paused completion or repair
//     cannot leave a stale owner, name or lifecycle state behind.

// AddTrashedLifecycleOwnedQueries adds the lifecycle-owned rows of trash
// generation *row.DeletedAt: its marker, its trash listing row, and the removal
// of the library's trash rows of other generations among trashRows.
func AddTrashedLifecycleOwnedQueries(batch *gocql.Batch, row AdminLibraryProjectionRow, blockRepresentationID string, trashRows []AdminDeletedLibraryProjectionRow) error {
	if row.DeletedAt == nil || row.DeletedAt.IsZero() {
		return fmt.Errorf("trashed library %s derived state without a generation", row.LibraryID)
	}
	batch.Query(`
		INSERT INTO deleted_libraries (library_id, org_id, deleted_at, storage_class, block_representation_id)
		VALUES (?, ?, ?, ?, ?)`,
		row.LibraryID, row.OrgID, *row.DeletedAt, row.StorageClass, blockRepresentationID)
	AddInsertDeletedAdminLibraryRowQuery(batch, row)
	for _, trashRow := range trashRows {
		if trashRow.LibraryID == row.LibraryID && !trashRow.DeletedAt.Equal(*row.DeletedAt) {
			AddDeleteDeletedAdminLibraryReadModelQuery(batch, trashRow)
		}
	}
	return nil
}

// AddRestoredLifecycleOwnedQueries adds the lifecycle-owned rows of an active
// library: no marker and none of its trash listing rows among trashRows.
func AddRestoredLifecycleOwnedQueries(batch *gocql.Batch, libraryID string, trashRows []AdminDeletedLibraryProjectionRow) {
	batch.Query(`DELETE FROM deleted_libraries WHERE library_id = ?`, libraryID)
	for _, trashRow := range trashRows {
		if trashRow.LibraryID == libraryID {
			AddDeleteDeletedAdminLibraryReadModelQuery(batch, trashRow)
		}
	}
}

// readCanonicalLibraryRowSerial reads the whole canonical row at SERIAL: the
// lifecycle state and the read-model columns come from the same observation.
func readCanonicalLibraryRowSerial(session *gocql.Session, orgID, libraryID string) (AdminLibraryProjectionRow, LibraryLifecycleState, error) {
	row := AdminLibraryProjectionRow{OrgID: orgID, LibraryID: libraryID}
	var state LibraryLifecycleState
	err := session.Query(`
		SELECT owner_id, name, encrypted, storage_class, size_bytes, file_count, created_at, updated_at, deleted_at, lifecycle_at
		FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, libraryID).Consistency(LibraryHeadSerialConsistency).Scan(
		&row.OwnerID, &row.Name, &row.Encrypted, &row.StorageClass, &row.SizeBytes, &row.FileCount,
		&row.CreatedAt, &row.UpdatedAt, &state.DeletedAt, &state.LifecycleAt)
	if errors.Is(err, gocql.ErrNotFound) {
		return AdminLibraryProjectionRow{}, LibraryLifecycleState{}, nil
	}
	if err != nil {
		return AdminLibraryProjectionRow{}, LibraryLifecycleState{}, err
	}
	state.Present = true
	if !state.DeletedAt.IsZero() {
		deletedAt := state.DeletedAt
		row.DeletedAt = &deletedAt
	}
	row.OwnerEmail, row.OwnerName = ResolveAdminLibraryOwnerFields(session, orgID, row.OwnerID)
	return row, state, nil
}

// libraryReadModelPublishAttempts bounds the rounds of publishLibraryReadModel
// against a canonical row that keeps changing under it.
const libraryReadModelPublishAttempts = 5

// libraryProjectionCurrent reports whether the read-model rows published from
// (row, state) still project the canonical snapshot (next, nextState): every
// projected canonical column, owner included, is unchanged.
func libraryProjectionCurrent(row AdminLibraryProjectionRow, state LibraryLifecycleState, next AdminLibraryProjectionRow, nextState LibraryLifecycleState) bool {
	if state.Present != nextState.Present {
		return false
	}
	if !state.Present {
		return true
	}
	return row.OwnerID == next.OwnerID && row.Name == next.Name && row.Encrypted == next.Encrypted &&
		row.StorageClass == next.StorageClass && row.SizeBytes == next.SizeBytes && row.FileCount == next.FileCount &&
		row.CreatedAt.Equal(next.CreatedAt) && row.UpdatedAt.Equal(next.UpdatedAt) && state.DeletedAt.Equal(nextState.DeletedAt)
}

// addDeleteSupersededAdminLibraryRowsQuery removes the owner and global rows
// that a published snapshot wrote under keys the current canonical row no
// longer has (another owner, another creation bucket), and every active row
// once the canonical row is gone. The org row shares its key across snapshots
// and is simply overwritten.
func addDeleteSupersededAdminLibraryRowsQuery(batch *gocql.Batch, published, row AdminLibraryProjectionRow, present bool) {
	if !present || published.OwnerID != row.OwnerID {
		batch.Query(`DELETE FROM libraries_by_owner WHERE org_id = ? AND owner_id = ? AND library_id = ?`,
			published.OrgID, published.OwnerID, published.LibraryID)
	}
	if !present {
		batch.Query(`DELETE FROM libraries_by_org_updated WHERE org_id = ? AND library_id = ?`,
			published.OrgID, published.LibraryID)
	}
	if !present || AdminLibraryBucketDay(published.CreatedAt) != AdminLibraryBucketDay(row.CreatedAt) {
		batch.Query(`DELETE FROM libraries_admin_global_by_updated WHERE bucket_day = ? AND org_id = ? AND library_id = ?`,
			AdminLibraryBucketDay(published.CreatedAt), published.OrgID, published.LibraryID)
	}
}

// ErrLibraryReadModelUnconfirmed reports a read-model publication whose
// snapshot could not be confirmed against ordinary writers in every datacenter
// (an EACH_QUORUM read failed). The rows written are not proven current: the
// caller keeps its continuation, so the reaper publishes again later.
var ErrLibraryReadModelUnconfirmed = errors.New("library read model not confirmed in every datacenter")

// overlayOrdinaryColumnsEachQuorum replaces the ordinary columns of a SERIAL
// snapshot with an EACH_QUORUM read of them. Owner transfers, renames and size
// updates are plain writes (LOCAL_QUORUM in their datacenter); a global SERIAL
// read need not intersect one acknowledged in another datacenter, an
// EACH_QUORUM read always does. The lifecycle columns stay from the SERIAL
// read, their authority. It fails with ErrLibraryReadModelUnconfirmed when a
// datacenter is unreachable.
func overlayOrdinaryColumnsEachQuorum(session *gocql.Session, row *AdminLibraryProjectionRow, state LibraryLifecycleState) error {
	if !state.Present {
		return nil
	}
	ordinary := *row
	err := session.Query(`
		SELECT owner_id, name, encrypted, storage_class, size_bytes, file_count, created_at, updated_at
		FROM libraries WHERE org_id = ? AND library_id = ?
	`, row.OrgID, row.LibraryID).Consistency(gocql.EachQuorum).Scan(
		&ordinary.OwnerID, &ordinary.Name, &ordinary.Encrypted, &ordinary.StorageClass, &ordinary.SizeBytes, &ordinary.FileCount,
		&ordinary.CreatedAt, &ordinary.UpdatedAt)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil // removed since the SERIAL read; the next confirmation sees it
	}
	if err != nil {
		return fmt.Errorf("%w: library %s: %w", ErrLibraryReadModelUnconfirmed, row.LibraryID, err)
	}
	if ordinary.OwnerID != row.OwnerID {
		ordinary.OwnerEmail, ordinary.OwnerName = ResolveAdminLibraryOwnerFields(session, row.OrgID, ordinary.OwnerID)
	}
	*row = ordinary
	return nil
}

// readLibraryProjectionSnapshot reads the canonical row for a read-model
// publication: lifecycle state at SERIAL, ordinary columns at EACH_QUORUM.
func readLibraryProjectionSnapshot(session *gocql.Session, orgID, libraryID string) (AdminLibraryProjectionRow, LibraryLifecycleState, error) {
	row, state, err := readCanonicalLibraryRowSerial(session, orgID, libraryID)
	if err != nil {
		return row, state, err
	}
	return row, state, overlayOrdinaryColumnsEachQuorum(session, &row, state)
}

// publishLibraryReadModel publishes the owner, org and global read-model rows
// of the canonical snapshot (row, state) and proves the snapshot is still
// current with a second read. The ordinary columns carry client timestamps, as
// every other writer of them does, so a snapshot that went stale while this
// publication was paused would otherwise land over a newer writer (an owner
// transfer, a rename, a later lifecycle transition) and stay; snapshot and
// confirmation therefore read the ordinary columns at EACH_QUORUM (a writer
// acknowledged in any datacenter is seen) and the lifecycle state at SERIAL.
// When the canonical row changed, the newer snapshot is published, removing
// what the stale one wrote under keys that no longer exist, until a round is
// confirmed. The deleted_at cell is written apart, stamped with the lifecycle
// value of the state it shows (AddAdminLibraryDeletedAtCellQueries), so it
// follows canonical order whatever the clocks. It returns an error (the caller
// keeps its continuation) if the row keeps changing, or
// ErrLibraryReadModelUnconfirmed if a datacenter cannot be read. An absent row
// publishes nothing.
func publishLibraryReadModel(session *gocql.Session, orgID, libraryID string, row AdminLibraryProjectionRow, state LibraryLifecycleState) error {
	if err := overlayOrdinaryColumnsEachQuorum(session, &row, state); err != nil {
		return err
	}
	var published *AdminLibraryProjectionRow
	for attempt := 0; attempt < libraryReadModelPublishAttempts; attempt++ {
		batch := session.Batch(gocql.LoggedBatch)
		if published != nil {
			addDeleteSupersededAdminLibraryRowsQuery(batch, *published, row, state.Present)
		}
		if state.Present {
			AddUpsertAdminLibraryOrdinaryRowsQuery(batch, row)
		}
		if len(batch.Entries) > 0 {
			if err := ExecLibraryLifecycleCompletionFn(batch); err != nil {
				return fmt.Errorf("publish library %s read model: %w", libraryID, err)
			}
		}
		if !state.Present {
			return nil
		}
		if cellAt := projectedLifecycleValue(state); !cellAt.IsZero() {
			cells := session.Batch(gocql.LoggedBatch)
			AddAdminLibraryDeletedAtCellQueries(cells, row, cellAt)
			if err := ExecLibraryLifecycleCompletionFn(cells); err != nil {
				return fmt.Errorf("publish library %s lifecycle state: %w", libraryID, err)
			}
		}
		next, nextState, err := readLibraryProjectionSnapshot(session, orgID, libraryID)
		if err != nil {
			return fmt.Errorf("confirm library %s read model: %w", libraryID, err)
		}
		if libraryProjectionCurrent(row, state, next, nextState) {
			return nil
		}
		stale := row
		published = &stale
		row, state = next, nextState
	}
	return fmt.Errorf("publish library %s read model: canonical row kept changing", libraryID)
}

// projectedLifecycleValue is the lifecycle value of the state a read model
// shows: a trashed row's generation, or the clock of an active row that went
// through a transition (zero for a row that never did).
func projectedLifecycleValue(state LibraryLifecycleState) time.Time {
	if !state.DeletedAt.IsZero() {
		return state.DeletedAt
	}
	return state.LifecycleAt
}

// RepairLibraryLifecycleDerivedState rewrites the derived state (marker, trash
// listing rows, owner/org/global read-model rows) of the library's current
// canonical lifecycle state, taking the canonical row read at SERIAL as the only
// authority. A trashed row gets its own generation's marker (replacing a marker
// of any other generation) and trash row; an active row that went through a
// restore loses its marker and trash rows. The library's existing trash rows are
// read at EACH_QUORUM, so a row acknowledged in another datacenter is seen; if
// any read cannot be made at that strength the repair returns an error (retry
// later) instead of reporting success. Lifecycle-owned rows carry the current
// generation's lifecycle timestamp, so a stale repair loses to any later
// transition; the ordinary read-model rows are published by
// publishLibraryReadModel, which confirms its snapshot. It does nothing for an
// absent row (see the permanent-delete resume).
func RepairLibraryLifecycleDerivedState(session *gocql.Session, orgID, libraryID string, resolveBlockRepresentation func() string) error {
	row, state, err := readCanonicalLibraryRowSerial(session, orgID, libraryID)
	if err != nil {
		return fmt.Errorf("read canonical library %s for repair: %w", libraryID, err)
	}
	if !state.Present {
		return nil
	}
	trashRows, err := ListDeletedAdminLibraryRowsByOrgEachQuorum(session, orgID)
	if err != nil {
		return fmt.Errorf("read trash listing of %s for repair: %w", libraryID, err)
	}
	if err := writeLifecycleOwnedRows(session, row, state, laterLifecycleValue(state), trashRows, resolveBlockRepresentation); err != nil {
		return fmt.Errorf("repair library %s lifecycle rows: %w", libraryID, err)
	}
	return publishLibraryReadModel(session, orgID, libraryID, row, state)
}

// writeLifecycleOwnedRows writes the lifecycle-owned rows of the canonical
// snapshot (row, state) stamped with lifecycle value at: the trashed
// generation's marker and trash row, or, for an active row that went through a
// lifecycle transition, the removal of its marker and of its trash rows among
// trashRows. An active row with no transition has none.
func writeLifecycleOwnedRows(session *gocql.Session, row AdminLibraryProjectionRow, state LibraryLifecycleState, at time.Time, trashRows []AdminDeletedLibraryProjectionRow, resolveBlockRepresentation func() string) error {
	if at.IsZero() {
		return nil
	}
	batch := session.Batch(gocql.LoggedBatch).WithTimestamp(LibraryLifecycleWriteTimestamp(at))
	if !state.DeletedAt.IsZero() {
		if err := AddTrashedLifecycleOwnedQueries(batch, row, resolveBlockRepresentation(), trashRows); err != nil {
			return err
		}
	} else {
		AddRestoredLifecycleOwnedQueries(batch, row.LibraryID, trashRows)
	}
	return ExecLibraryLifecycleCompletionFn(batch)
}

// CompleteLibraryLifecycleDerivedState writes the derived state of a soft
// delete or restore that committed at lifecycleAt, from the canonical row read
// at SERIAL after its LWT (never from a snapshot taken before it): the
// lifecycle-owned rows stamped with lifecycleAt (trashRows are the trash rows
// the transition is known to supersede, nil for a soft delete), then the
// ordinary read model through publishLibraryReadModel. If a later transition
// already committed, the whole derived state is repaired from the canonical row
// instead. An absent row (permanently deleted since) needs nothing: the
// permanent delete owns its completion.
func CompleteLibraryLifecycleDerivedState(session *gocql.Session, orgID, libraryID string, lifecycleAt time.Time, trashRows []AdminDeletedLibraryProjectionRow, resolveBlockRepresentation func() string) error {
	row, state, err := readCanonicalLibraryRowSerial(session, orgID, libraryID)
	if err != nil {
		return fmt.Errorf("read canonical library %s for completion: %w", libraryID, err)
	}
	if !state.Present {
		return nil
	}
	if !state.LifecycleAt.Equal(lifecycleAt) {
		return RepairLibraryLifecycleDerivedState(session, orgID, libraryID, resolveBlockRepresentation)
	}
	if err := writeLifecycleOwnedRows(session, row, state, lifecycleAt, trashRows, resolveBlockRepresentation); err != nil {
		return fmt.Errorf("complete library %s lifecycle rows: %w", libraryID, err)
	}
	return publishLibraryReadModel(session, orgID, libraryID, row, state)
}
