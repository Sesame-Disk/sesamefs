package db

import (
	"errors"
	"fmt"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

// Paxos domains of the generation-fenced lifecycle transitions of a trashed
// library (ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01).
//
// The canonical `libraries` transitions (API permanent delete / GC cascade row
// delete, restore) and their settlement reads use LibraryHeadSerialConsistency:
// a partition has one Paxos domain, and the `libraries` partition is already in
// global SERIAL for HEAD authority. The restore's conditional removal of the
// `deleted_libraries` marker uses DeletedLibraryMarkerSerialConsistency. Both
// are global SERIAL and never derived from database.serial_consistency /
// CASSANDRA_SERIAL_CONSISTENCY: under LOCAL_SERIAL each datacenter would run its
// own Paxos domain, so a restore in one DC and a stale permanent delete in
// another could both apply.
const DeletedLibraryMarkerSerialConsistency = gocql.Serial

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

// ClearSoftDeleteMarkerGeneration removes the `deleted_libraries` marker of a
// restored library only while it is the soft-delete marker of generation
// deletedAt: same deleted_at and no purge_requested_at. Restore calls it after
// its canonical transition applied (the canonical LWT is the fence), so even a
// restore that pauses in between cannot remove the marker of a newer trash
// generation or a permanent-delete marker.
//
// Outcomes: Applied; TargetAbsent (no marker); GenerationChanged (the marker
// belongs to another generation or records a purge request).
func ClearSoftDeleteMarkerGeneration(session *gocql.Session, libraryID string, deletedAt time.Time) (LibraryLifecycleOutcome, error) {
	if deletedAt.IsZero() {
		return LibraryLifecycleGenerationChanged, fmt.Errorf("clear deleted library marker %s: zero deleted_at generation", libraryID)
	}
	previous := map[string]interface{}{}
	applied, err := session.Query(`
		DELETE FROM deleted_libraries WHERE library_id = ?
		IF deleted_at = ? AND purge_requested_at = null
	`, libraryID, deletedAt).
		SerialConsistency(DeletedLibraryMarkerSerialConsistency).
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
		return LibraryLifecycleGenerationChanged, fmt.Errorf("clear deleted library marker %s: %w", libraryID, err)
	}
	var current, purgeRequestedAt time.Time
	readErr := session.Query(`SELECT deleted_at, purge_requested_at FROM deleted_libraries WHERE library_id = ?`,
		libraryID).Consistency(DeletedLibraryMarkerSerialConsistency).Scan(&current, &purgeRequestedAt)
	present := true
	if errors.Is(readErr, gocql.ErrNotFound) {
		present, readErr = false, nil
	}
	return settleSoftDeleteMarkerClear(libraryID, deletedAt, err, present, current, purgeRequestedAt, readErr)
}

// settleSoftDeleteMarkerClear settles an ambiguous ClearSoftDeleteMarkerGeneration
// from a SERIAL read of the marker.
func settleSoftDeleteMarkerClear(libraryID string, deletedAt time.Time, casErr error, present bool, current, purgeRequestedAt time.Time, readErr error) (LibraryLifecycleOutcome, error) {
	if readErr != nil {
		return LibraryLifecycleGenerationChanged, errors.Join(fmt.Errorf("%w: clear deleted library marker %s: %w", ErrLibraryLifecycleOutcomeUnknown, libraryID, casErr), fmt.Errorf("settlement read failed: %w", readErr))
	}
	if !present {
		return LibraryLifecycleTargetAbsent, nil
	}
	if current.Equal(deletedAt) && purgeRequestedAt.IsZero() {
		return LibraryLifecycleGenerationChanged, fmt.Errorf("%w: clear deleted library marker %s: %w", ErrLibraryLifecycleOutcomeUnknown, libraryID, casErr)
	}
	return LibraryLifecycleGenerationChanged, nil
}

// ClearSoftDeleteMarkerOfActiveLibrary removes the soft-delete marker of
// generation deletedAt when the canonical row is active again. A restore that
// committed its canonical transition but stopped before clearing its marker
// leaves exactly that state; the GC cascade calls this after its fenced delete
// reported a changed generation, so trash retention stops re-enqueuing the
// library. It does nothing while the row is absent or trashed, and the marker
// removal is itself conditioned on the generation, so a re-trash in between is
// never touched.
func ClearSoftDeleteMarkerOfActiveLibrary(session *gocql.Session, orgID, libraryID string, deletedAt time.Time) error {
	present, current, err := readLibraryDeletedAtSerial(session, orgID, libraryID)
	if err != nil {
		return fmt.Errorf("read canonical library %s before clearing a stale marker: %w", libraryID, err)
	}
	if !present || !current.IsZero() {
		return nil
	}
	if _, err := ClearSoftDeleteMarkerGeneration(session, libraryID, deletedAt); err != nil {
		return err
	}
	return nil
}
