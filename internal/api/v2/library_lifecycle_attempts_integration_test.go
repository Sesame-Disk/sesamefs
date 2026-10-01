//go:build integration

package v2

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

// ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01, round 5: identity and retirement of
// lifecycle attempts, and the convergence of the ordinary read model against
// concurrent ordinary writers. Real Cassandra.

// nonfencingPendingRows returns the library's lifecycle continuations.
func nonfencingPendingRows(t *testing.T, db *dbpkg.DB, lib nonfencingLibrary) []dbpkg.LibraryLifecyclePending {
	t.Helper()
	rows, err := dbpkg.ListLibraryLifecyclePending(db.Session(), dbpkg.GCDiscoveryBucket(lib.OrgID, lib.LibraryID))
	if err != nil {
		t.Fatalf("list lifecycle continuations: %v", err)
	}
	var out []dbpkg.LibraryLifecyclePending
	for _, row := range rows {
		if row.LibraryID == lib.LibraryID {
			out = append(out, row)
		}
	}
	return out
}

// parkFirstLifecycleAttempt parks the first attempt of operation on lib right
// before its canonical LWT, after its continuation is durable. The returned
// release lets it go (also run at cleanup).
func parkFirstLifecycleAttempt(t *testing.T, lib nonfencingLibrary, operation string) (parked <-chan struct{}, release func()) {
	t.Helper()
	parkedCh, resume := make(chan struct{}), make(chan struct{})
	var first, released sync.Once
	original := beforeLibraryLifecycleTransitionFn
	beforeLibraryLifecycleTransitionFn = func(op, libraryID string) error {
		if op == operation && libraryID == lib.LibraryID {
			park := false
			first.Do(func() { park = true })
			if park {
				close(parkedCh)
				<-resume
			}
		}
		return original(op, libraryID)
	}
	release = func() { released.Do(func() { close(resume) }) }
	t.Cleanup(func() {
		release()
		beforeLibraryLifecycleTransitionFn = original
	})
	return parkedCh, release
}

func awaitParked(t *testing.T, parked <-chan struct{}) {
	t.Helper()
	select {
	case <-parked:
	case <-time.After(30 * time.Second):
		t.Fatal("attempt never reached its pre-LWT pause")
	}
}

// withLifecycleAttemptsAbandoned makes every continuation old enough for the
// reaper to fence it.
func withLifecycleAttemptsAbandoned(t *testing.T) {
	t.Helper()
	original := dbpkg.LibraryLifecycleAttemptAbandonAfter
	dbpkg.LibraryLifecycleAttemptAbandonAfter = 0
	t.Cleanup(func() { dbpkg.LibraryLifecycleAttemptAbandonAfter = original })
}

func nonfencingOwnerListing(t *testing.T, db *dbpkg.DB, lib nonfencingLibrary, ownerID string) int {
	t.Helper()
	var count int
	if err := db.Session().Query(`SELECT COUNT(*) FROM libraries_by_owner WHERE org_id = ? AND owner_id = ? AND library_id = ?`,
		lib.OrgID, ownerID, lib.LibraryID).Scan(&count); err != nil {
		t.Fatalf("read owner listing: %v", err)
	}
	return count
}

func assertOnlyOwnerListed(t *testing.T, db *dbpkg.DB, lib nonfencingLibrary, oldOwner, newOwner, label string) {
	t.Helper()
	if n := nonfencingOwnerListing(t, db, lib, oldOwner); n != 0 {
		t.Fatalf("%s: the previous owner still lists the library (rows=%d)", label, n)
	}
	if n := nonfencingOwnerListing(t, db, lib, newOwner); n != 1 {
		t.Fatalf("%s: the new owner does not list the library (rows=%d)", label, n)
	}
	var owner string
	if err := db.Session().Query(`SELECT owner_id FROM libraries_by_org_updated WHERE org_id = ? AND library_id = ?`,
		lib.OrgID, lib.LibraryID).Scan(&owner); err == nil && owner != newOwner {
		t.Fatalf("%s: org read model owner = %s, want %s", label, owner, newOwner)
	}
}

// transferBeforeOrdinaryPublication transfers lib to newOwner right before the
// first lifecycle batch that writes the owner listing executes: the transfer
// lands after that publication took its canonical snapshot.
func transferBeforeOrdinaryPublication(t *testing.T, db *dbpkg.DB, lib nonfencingLibrary, newOwner string) {
	t.Helper()
	original := dbpkg.ExecLibraryLifecycleCompletionFn
	var once sync.Once
	dbpkg.ExecLibraryLifecycleCompletionFn = func(batch *gocql.Batch) error {
		for _, entry := range batch.Entries {
			if strings.Contains(entry.Stmt, "INSERT INTO libraries_by_owner") {
				once.Do(func() {
					if err := updateLibraryOwner(db, lib.OrgID, lib.LibraryID, newOwner, time.Now().UTC()); err != nil {
						t.Errorf("transfer: %v", err)
					}
				})
				break
			}
		}
		return original(batch)
	}
	t.Cleanup(func() { dbpkg.ExecLibraryLifecycleCompletionFn = original })
}

// A1: two soft deletes of one library propose the same transition (a frozen
// clock gives both the same target). The winner dies right after its LWT; the
// loser clears only its own continuation, so the reaper still completes the
// winner's accounting and derived state.
func TestNonfencingA1SameTargetLoserKeepsWinnerContinuation(t *testing.T) {
	db := restoreGuardDBForTest(t)
	frozen := time.Now().UTC().Truncate(time.Millisecond)
	withLibraryLifecycleClock(t, func() time.Time { return frozen })
	lib := nonfencingSeedCountedLibrary(t, db)

	parked, release := parkFirstLifecycleAttempt(t, lib, "soft-delete")
	loser := runOwner(func() error { return softDeleteLibrary(db, lib.OrgID, lib.OwnerID, lib.OwnerID, lib.LibraryID) })
	awaitParked(t, parked)

	simulateDeathAfterLifecycleTransition(t, "soft-delete")
	if err := softDeleteLibrary(db, lib.OrgID, lib.OwnerID, lib.OwnerID, lib.LibraryID); err == nil {
		t.Fatal("expected the winner's simulated death to surface")
	}
	present, d := nonfencingCanonical(t, db, lib)
	if !present || !d.Equal(frozen) {
		t.Fatalf("setup: winner's soft delete present=%v deleted_at=%v, want %s", present, d, frozen)
	}
	release()
	if err := awaitOwner(t, loser); err != nil {
		t.Fatalf("losing soft delete: %v", err)
	}

	rows := nonfencingPendingRows(t, db, lib)
	if len(rows) != 1 || !rows[0].TargetAt.Equal(d) {
		t.Fatalf("NONFENCING RED: the losing attempt removed the winner's continuation: rows=%+v", rows)
	}
	nonfencingRecover(t, db)
	if org, user := nonfencingOrgAndUserBytes(db, lib); org != 0 || user != 0 {
		t.Fatalf("NONFENCING RED: counters after recovery org=%d user=%d, want 0 (library trashed)", org, user)
	}
	assertNonfencingTrashedDerivedState(t, db, lib, d, "winner recovered by the reaper")
	if rows := nonfencingPendingRows(t, db, lib); len(rows) != 0 {
		t.Fatalf("continuation left after recovery: %+v", rows)
	}
}

// A2: a soft delete whose process dies after recording its continuation but
// before its LWT. The reaper keeps the attempt while it may still be in flight,
// then fences and retires it; the fenced attempt's LWT can no longer apply and
// the library stays usable.
func TestNonfencingA2SoftDeleteAbandonedBeforeLWTIsFenced(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedActiveLibrary(t, db)
	original := beforeLibraryLifecycleTransitionFn
	beforeLibraryLifecycleTransitionFn = func(op, libraryID string) error {
		if op == "soft-delete" && libraryID == lib.LibraryID {
			return errors.New("simulated process death before the canonical transition")
		}
		return original(op, libraryID)
	}
	t.Cleanup(func() { beforeLibraryLifecycleTransitionFn = original })
	if err := softDeleteLibrary(db, lib.OrgID, lib.OwnerID, lib.OwnerID, lib.LibraryID); err == nil {
		t.Fatal("expected the simulated death to surface")
	}
	beforeLibraryLifecycleTransitionFn = original
	if rows := nonfencingPendingRows(t, db, lib); len(rows) != 1 {
		t.Fatalf("setup: continuations = %+v, want the abandoned attempt", rows)
	}

	if err := RecoverPendingLibraryLifecycles(context.Background(), db); err != nil {
		t.Fatalf("lifecycle reaper: %v", err)
	}
	if rows := nonfencingPendingRows(t, db, lib); len(rows) != 1 {
		t.Fatalf("NONFENCING RED: a fresh attempt that may still be in flight was dropped: %+v", rows)
	}

	withLifecycleAttemptsAbandoned(t)
	if err := RecoverPendingLibraryLifecycles(context.Background(), db); err != nil {
		t.Fatalf("lifecycle reaper: %v", err)
	}
	if rows := nonfencingPendingRows(t, db, lib); len(rows) != 0 {
		t.Fatalf("NONFENCING RED: abandoned soft-delete attempt still pending: %+v", rows)
	}
	if present, deletedAt := nonfencingCanonical(t, db, lib); !present || !deletedAt.IsZero() {
		t.Fatalf("the fence changed the lifecycle: present=%v deleted_at=%v", present, deletedAt)
	}
	stale := time.Now().UTC()
	applied, err := db.Session().Query(`
		UPDATE libraries SET deleted_at = ?, lifecycle_at = ? WHERE org_id = ? AND library_id = ?
		IF deleted_at = null AND created_at != null AND lifecycle_at = null`,
		stale, stale, lib.OrgID, lib.LibraryID).SerialConsistency(gocql.Serial).MapScanCAS(map[string]interface{}{})
	if err != nil || applied {
		t.Fatalf("NONFENCING RED: the fenced attempt's LWT still applies: applied=%v err=%v", applied, err)
	}
	nonfencingSoftDelete(t, db, lib)
}

// A3: a restore parks after recording its continuation; the reaper fences and
// retires it as abandoned. When the restore resumes and its process dies after
// the canonical transition, the transition that committed must have its own
// continuation: the fenced attempt cannot commit untracked.
func TestNonfencingA3RestoreFencedWhileParkedCannotCommitUntracked(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	parked, release := parkFirstLifecycleAttempt(t, lib, "restore")
	owner := runOwner(func() error { return nonfencingRestore(db, lib) })
	awaitParked(t, parked)

	withLifecycleAttemptsAbandoned(t)
	if err := RecoverPendingLibraryLifecycles(context.Background(), db); err != nil {
		t.Fatalf("lifecycle reaper: %v", err)
	}
	if rows := nonfencingPendingRows(t, db, lib); len(rows) != 0 {
		t.Fatalf("NONFENCING RED: abandoned restore attempt not retired: %+v", rows)
	}
	if present, deletedAt := nonfencingCanonical(t, db, lib); !present || !deletedAt.Equal(lib.DeletedAt) {
		t.Fatalf("the fence changed the trash generation: present=%v deleted_at=%v", present, deletedAt)
	}
	dbpkg.LibraryLifecycleAttemptAbandonAfter = time.Hour

	simulateDeathAfterLifecycleTransition(t, "restore")
	release()
	if err := awaitOwner(t, owner); err == nil {
		t.Fatal("expected the simulated death to surface")
	}
	if present, deletedAt := nonfencingCanonical(t, db, lib); !present || !deletedAt.IsZero() {
		t.Fatalf("setup: restore did not commit: present=%v deleted_at=%v", present, deletedAt)
	}
	if rows := nonfencingPendingRows(t, db, lib); len(rows) != 1 {
		t.Fatalf("NONFENCING RED: the fenced attempt committed without a continuation: rows=%+v", rows)
	}
	if err := RecoverPendingLibraryLifecycles(context.Background(), db); err != nil {
		t.Fatalf("lifecycle reaper: %v", err)
	}
	if marker := nonfencingReadMarker(t, db, lib); marker.Present {
		t.Fatalf("restore recovered by the reaper left its marker: %+v", marker)
	}
	if present, deletedAt, inTrash := nonfencingProjection(t, db, lib); !present || !deletedAt.IsZero() || inTrash {
		t.Fatalf("restore recovered by the reaper: read model present=%v deleted_at=%v in_trash=%v", present, deletedAt, inTrash)
	}
	if rows := nonfencingPendingRows(t, db, lib); len(rows) != 0 {
		t.Fatalf("continuation left after recovery: %+v", rows)
	}
}

// A4: the same for a permanent delete whose completion fails after the
// canonical delete.
func TestNonfencingA4PermanentDeleteFencedWhileParkedCannotCommitUntracked(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	parked, release := parkFirstLifecycleAttempt(t, lib, "permanent-delete")
	owner := runOwner(func() error { return nonfencingPermanentDelete(db, lib) })
	awaitParked(t, parked)

	withLifecycleAttemptsAbandoned(t)
	if err := RecoverPendingLibraryLifecycles(context.Background(), db); err != nil {
		t.Fatalf("lifecycle reaper: %v", err)
	}
	if rows := nonfencingPendingRows(t, db, lib); len(rows) != 0 {
		t.Fatalf("NONFENCING RED: abandoned permanent-delete attempt not retired: %+v", rows)
	}
	dbpkg.LibraryLifecycleAttemptAbandonAfter = time.Hour

	restore := failLibraryLifecycleCompletions(t)
	release()
	if err := awaitOwner(t, owner); !errors.Is(err, errHardDeleteLibraryBatchExec) {
		t.Fatalf("permanent delete with a failing completion = %v", err)
	}
	restore()
	if present, _ := nonfencingCanonical(t, db, lib); present {
		t.Fatal("setup: the canonical delete did not apply")
	}
	if rows := nonfencingPendingRows(t, db, lib); len(rows) != 1 {
		t.Fatalf("NONFENCING RED: the fenced attempt committed without a continuation: rows=%+v", rows)
	}
	if err := RecoverPendingLibraryLifecycles(context.Background(), db); err != nil {
		t.Fatalf("lifecycle reaper: %v", err)
	}
	var orgID string
	if err := db.Session().Query(`SELECT org_id FROM libraries_by_id WHERE library_id = ?`, lib.LibraryID).Scan(&orgID); !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("permanent delete recovered by the reaper left libraries_by_id: err=%v", err)
	}
	if marker := nonfencingReadMarker(t, db, lib); !marker.Present || marker.PurgeRequestedAt.IsZero() {
		t.Fatalf("permanent delete recovered by the reaper: marker %+v, want a purge request", marker)
	}
}

// A5: a restore commits and parks before its completion; an owner transfer
// completes meanwhile. The late completion must not list the library under the
// previous owner again.
func TestNonfencingA5RestoreCompletionAfterTransfer(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	pause := pauseRestoreAfterTransition(t, lib)
	owner := runOwner(func() error { return nonfencingRestore(db, lib) })
	pause.wait(t)
	newOwner := uuid.NewString()
	if err := updateLibraryOwner(db, lib.OrgID, lib.LibraryID, newOwner, time.Now().UTC()); err != nil {
		t.Fatalf("transfer: %v", err)
	}
	pause.resumeOwner()
	if err := awaitOwner(t, owner); err != nil {
		t.Fatalf("restore completion: %v", err)
	}
	assertOnlyOwnerListed(t, db, lib, lib.OwnerID, newOwner, "NONFENCING RED: late restore completion after a transfer")
}

// A6: the transfer lands between the restore completion's canonical snapshot
// and its read-model write: the confirmation read must republish the newer
// owner and remove the stale listing.
func TestNonfencingA6RestorePublicationConfirmsItsSnapshot(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	newOwner := uuid.NewString()
	transferBeforeOrdinaryPublication(t, db, lib, newOwner)
	if err := nonfencingRestore(db, lib); err != nil {
		t.Fatalf("restore: %v", err)
	}
	assertOnlyOwnerListed(t, db, lib, lib.OwnerID, newOwner, "NONFENCING RED: restore published a stale snapshot over a transfer")
}

// A7: the same race for a repair (repeated request / reaper).
func TestNonfencingA7RepairPublicationConfirmsItsSnapshot(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	if err := nonfencingRestore(db, lib); err != nil {
		t.Fatalf("restore: %v", err)
	}
	newOwner := uuid.NewString()
	transferBeforeOrdinaryPublication(t, db, lib, newOwner)
	if err := repairLibraryLifecycleDerivedState(db, lib.OrgID, lib.LibraryID); err != nil {
		t.Fatalf("repair: %v", err)
	}
	assertOnlyOwnerListed(t, db, lib, lib.OwnerID, newOwner, "NONFENCING RED: repair published a stale snapshot over a transfer")
}

// A8: a permanent delete whose completion failed, seen from a datacenter whose
// local deleted_libraries scan does not see the marker yet: the bulk cleanup
// discovers it through its continuation (global QUORUM) and resumes it.
func TestNonfencingA8BulkCleanDiscoversDeleteThroughContinuation(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	restore := failLibraryLifecycleCompletions(t)
	if err := nonfencingPermanentDelete(db, lib); !errors.Is(err, errHardDeleteLibraryBatchExec) {
		t.Fatalf("permanent delete with a failing completion = %v", err)
	}
	restore()
	if present, _ := nonfencingCanonical(t, db, lib); present {
		t.Fatal("setup: the canonical delete did not apply")
	}
	original := listDeletedLibraryMarkersFn
	listDeletedLibraryMarkersFn = func(*dbpkg.DB, map[string]bool) ([][2]string, error) { return nil, nil }
	t.Cleanup(func() { listDeletedLibraryMarkersFn = original })

	resumed, failed := resumeCommittedPermanentDeletes(db, []string{lib.OrgID})
	if failed != 0 {
		t.Fatalf("bulk resume failed=%d", failed)
	}
	found := false
	for _, r := range resumed {
		found = found || r.Candidate.LibraryID == lib.LibraryID
	}
	if !found {
		t.Fatal("NONFENCING RED: bulk discovery missed a committed permanent delete the local marker scan did not see")
	}
	var orgID string
	if err := db.Session().Query(`SELECT org_id FROM libraries_by_id WHERE library_id = ?`, lib.LibraryID).Scan(&orgID); !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("bulk resume left libraries_by_id: err=%v", err)
	}
}

// skewOrdinaryPublications stamps every lifecycle batch that writes the
// ordinary owner listing (and carries no per-statement timestamp) with a
// client clock skew ahead, as a node whose clock is ahead would.
func skewOrdinaryPublications(t *testing.T, skew time.Duration) func() {
	t.Helper()
	original := dbpkg.ExecLibraryLifecycleCompletionFn
	dbpkg.ExecLibraryLifecycleCompletionFn = func(batch *gocql.Batch) error {
		owner, stamped := false, false
		for _, entry := range batch.Entries {
			owner = owner || strings.Contains(entry.Stmt, "INSERT INTO libraries_by_owner")
			stamped = stamped || strings.Contains(entry.Stmt, "USING TIMESTAMP")
		}
		if owner && !stamped {
			batch.WithTimestamp(time.Now().Add(skew).UnixMicro())
		}
		return original(batch)
	}
	restore := func() { dbpkg.ExecLibraryLifecycleCompletionFn = original }
	t.Cleanup(restore)
	return restore
}

// nonfencingProjectedDeletedAt reads deleted_at of the owner and org read-model
// rows.
func nonfencingProjectedDeletedAt(t *testing.T, db *dbpkg.DB, lib nonfencingLibrary) (owner, org time.Time) {
	t.Helper()
	if err := db.Session().Query(`SELECT deleted_at FROM libraries_by_owner WHERE org_id = ? AND owner_id = ? AND library_id = ?`,
		lib.OrgID, lib.OwnerID, lib.LibraryID).Scan(&owner); err != nil {
		t.Fatalf("read owner row: %v", err)
	}
	if err := db.Session().Query(`SELECT deleted_at FROM libraries_by_org_updated WHERE org_id = ? AND library_id = ?`,
		lib.OrgID, lib.LibraryID).Scan(&org); err != nil {
		t.Fatalf("read org row: %v", err)
	}
	return owner, org
}

// A9: a soft delete on a node an hour ahead (lifecycle clock and client clock),
// then a restore on a normal node: the read model must show the library active.
func TestNonfencingA9ProjectedDeletedAtFollowsRestoreAfterFastSoftDelete(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedActiveLibrary(t, db)
	withLibraryLifecycleClock(t, func() time.Time { return time.Now().Add(time.Hour) })
	unskew := skewOrdinaryPublications(t, time.Hour)
	lib.DeletedAt = nonfencingSoftDelete(t, db, lib)
	unskew()
	libraryLifecycleNow = time.Now
	if err := nonfencingRestore(db, lib); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if owner, org := nonfencingProjectedDeletedAt(t, db, lib); !owner.IsZero() || !org.IsZero() {
		t.Fatalf("NONFENCING RED: restored library still projected as deleted (owner row %s, org row %s)", owner, org)
	}
}

// A10: a restore on a node an hour ahead, then a soft delete on a normal node:
// the read model must show the new trash generation.
func TestNonfencingA10ProjectedDeletedAtFollowsSoftDeleteAfterFastRestore(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	withLibraryLifecycleClock(t, func() time.Time { return time.Now().Add(time.Hour) })
	unskew := skewOrdinaryPublications(t, time.Hour)
	if err := nonfencingRestore(db, lib); err != nil {
		t.Fatalf("restore: %v", err)
	}
	unskew()
	libraryLifecycleNow = time.Now
	d2 := nonfencingSoftDelete(t, db, lib)
	if owner, org := nonfencingProjectedDeletedAt(t, db, lib); !owner.Equal(d2) || !org.Equal(d2) {
		t.Fatalf("NONFENCING RED: trashed library projected with deleted_at owner=%s org=%s, want %s", owner, org, d2)
	}
}

// A11: a continuation written by a node an hour ahead is retired by a node with
// a normal clock: the retirement must not lose to the insert's timestamp.
func TestNonfencingA11PendingRetirementBeatsFastInsert(t *testing.T) {
	db := restoreGuardDBForTest(t)
	orgID, libraryID := uuid.NewString(), uuid.NewString()
	bucket := dbpkg.GCDiscoveryBucket(orgID, libraryID)
	now := time.Now().UTC()
	attemptID := uuid.NewString()
	if err := db.Session().Query(`
		INSERT INTO library_lifecycle_pending (recovery_bucket, org_id, library_id, operation, target_at, attempt_id, prev_deleted_at, prev_lifecycle_at, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) USING TIMESTAMP ?`,
		bucket, orgID, libraryID, dbpkg.LibraryLifecycleOpPermanentDelete, now, attemptID, now, now, now,
		now.Add(time.Hour).UnixMicro()).Exec(); err != nil {
		t.Fatalf("seed fast continuation: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Session().Query(`DELETE FROM library_lifecycle_pending USING TIMESTAMP ? WHERE recovery_bucket = ? AND org_id = ? AND library_id = ?`,
			time.Now().Add(2*time.Hour).UnixMicro(), bucket, orgID, libraryID).Exec()
	})
	lib := nonfencingLibrary{OrgID: orgID, LibraryID: libraryID}
	if rows := nonfencingPendingRows(t, db, lib); len(rows) != 1 {
		t.Fatalf("setup: continuations = %+v", rows)
	}
	if err := RecoverPendingLibraryLifecycles(context.Background(), db); err != nil {
		t.Fatalf("lifecycle reaper: %v", err)
	}
	if rows := nonfencingPendingRows(t, db, lib); len(rows) != 0 {
		t.Fatalf("NONFENCING RED: retired continuation still visible (its insert carried a later client timestamp): %+v", rows)
	}
}

// A12: a permanent delete parks before its LWT and is fenced by the reaper,
// whose repair rewrites the trashed library's derived rows at the fence value;
// the resumed delete then retries, commits and completes normally. Its
// completion must land after those rows: nothing of the library may survive.
func TestNonfencingA12PermanentDeleteCompletionAfterFenceRemovesRepairedRows(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	parked, release := parkFirstLifecycleAttempt(t, lib, "permanent-delete")
	owner := runOwner(func() error { return nonfencingPermanentDelete(db, lib) })
	awaitParked(t, parked)

	withLifecycleAttemptsAbandoned(t)
	if err := RecoverPendingLibraryLifecycles(context.Background(), db); err != nil {
		t.Fatalf("lifecycle reaper: %v", err)
	}
	if state, err := dbpkg.ReadLibraryLifecycleSerial(db.Session(), lib.OrgID, lib.LibraryID); err != nil || !state.DeletedAt.Equal(lib.DeletedAt) || !state.LifecycleAt.After(lib.DeletedAt) {
		t.Fatalf("setup: fence did not move the lifecycle clock: %+v err=%v", state, err)
	}
	dbpkg.LibraryLifecycleAttemptAbandonAfter = time.Hour

	release()
	if err := awaitOwner(t, owner); err != nil {
		t.Fatalf("permanent delete after the fence: %v", err)
	}
	if present, _ := nonfencingCanonical(t, db, lib); present {
		t.Fatal("setup: the canonical delete did not apply")
	}
	var n int
	for _, q := range []struct{ name, cql string }{
		{"libraries_by_id", `SELECT COUNT(*) FROM libraries_by_id WHERE library_id = ?`},
		{"libraries_by_owner", `SELECT COUNT(*) FROM libraries_by_owner WHERE org_id = ? AND owner_id = ? AND library_id = ?`},
		{"libraries_by_org_updated", `SELECT COUNT(*) FROM libraries_by_org_updated WHERE org_id = ? AND library_id = ?`},
		{"libraries_deleted_by_org", `SELECT COUNT(*) FROM libraries_deleted_by_org WHERE org_id = ? AND deleted_at = ? AND library_id = ?`},
	} {
		var args []interface{}
		switch q.name {
		case "libraries_by_id":
			args = []interface{}{lib.LibraryID}
		case "libraries_by_owner":
			args = []interface{}{lib.OrgID, lib.OwnerID, lib.LibraryID}
		case "libraries_by_org_updated":
			args = []interface{}{lib.OrgID, lib.LibraryID}
		default:
			args = []interface{}{lib.OrgID, lib.DeletedAt, lib.LibraryID}
		}
		if err := db.Session().Query(q.cql, args...).Scan(&n); err != nil || n != 0 {
			t.Fatalf("NONFENCING RED: %s survived the permanent delete completed after a fence (rows=%d err=%v)", q.name, n, err)
		}
	}
	if marker := nonfencingReadMarker(t, db, lib); !marker.Present || marker.PurgeRequestedAt.IsZero() {
		t.Fatalf("marker = %+v, want a purge request", marker)
	}
	if rows := nonfencingPendingRows(t, db, lib); len(rows) != 0 {
		t.Fatalf("continuation left after the completed delete: %+v", rows)
	}
}

// A13: the same after a crash. The generation was created on a node an hour
// ahead, so the fence value (and the repaired rows) are ahead of real time; the
// delete commits but its completion fails, and the reaper resumes it on a
// normal clock. The resumed completion must recover the winning lifecycle floor
// from the durable continuation and still remove the repaired rows.
func TestNonfencingA13ResumedPermanentDeleteRecoversLifecycleFloor(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedActiveLibrary(t, db)
	withLibraryLifecycleClock(t, func() time.Time { return time.Now().Add(time.Hour) })
	lib.DeletedAt = nonfencingSoftDelete(t, db, lib)
	libraryLifecycleNow = time.Now

	parked, release := parkFirstLifecycleAttempt(t, lib, "permanent-delete")
	owner := runOwner(func() error { return nonfencingPermanentDelete(db, lib) })
	awaitParked(t, parked)
	withLifecycleAttemptsAbandoned(t)
	if err := RecoverPendingLibraryLifecycles(context.Background(), db); err != nil {
		t.Fatalf("lifecycle reaper: %v", err)
	}
	dbpkg.LibraryLifecycleAttemptAbandonAfter = time.Hour

	restore := failLibraryLifecycleCompletions(t)
	release()
	if err := awaitOwner(t, owner); !errors.Is(err, errHardDeleteLibraryBatchExec) {
		t.Fatalf("permanent delete with a failing completion = %v", err)
	}
	restore()
	if err := RecoverPendingLibraryLifecycles(context.Background(), db); err != nil {
		t.Fatalf("lifecycle reaper: %v", err)
	}
	var n int
	if err := db.Session().Query(`SELECT COUNT(*) FROM libraries_deleted_by_org WHERE org_id = ? AND deleted_at = ? AND library_id = ?`,
		lib.OrgID, lib.DeletedAt, lib.LibraryID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("NONFENCING RED: the resumed completion lost to the rows repaired at the fence value (trash rows=%d err=%v)", n, err)
	}
	if err := db.Session().Query(`SELECT COUNT(*) FROM libraries_by_id WHERE library_id = ?`, lib.LibraryID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("resumed completion left libraries_by_id (rows=%d err=%v)", n, err)
	}
	if rows := nonfencingPendingRows(t, db, lib); len(rows) != 0 {
		t.Fatalf("continuation left after recovery: %+v", rows)
	}
}

// A14: A12 with the generation created on a node an hour ahead, so the fence
// value and the rows the reaper repairs at it are ahead of real time; the
// resumed delete completes normally on a normal clock. Only the winning
// lifecycle floor orders its completion after those rows.
func TestNonfencingA14PermanentDeleteCompletionUsesWinningLifecycleFloor(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedActiveLibrary(t, db)
	withLibraryLifecycleClock(t, func() time.Time { return time.Now().Add(time.Hour) })
	lib.DeletedAt = nonfencingSoftDelete(t, db, lib)
	libraryLifecycleNow = time.Now

	parked, release := parkFirstLifecycleAttempt(t, lib, "permanent-delete")
	owner := runOwner(func() error { return nonfencingPermanentDelete(db, lib) })
	awaitParked(t, parked)
	withLifecycleAttemptsAbandoned(t)
	if err := RecoverPendingLibraryLifecycles(context.Background(), db); err != nil {
		t.Fatalf("lifecycle reaper: %v", err)
	}
	dbpkg.LibraryLifecycleAttemptAbandonAfter = time.Hour
	release()
	if err := awaitOwner(t, owner); err != nil {
		t.Fatalf("permanent delete after the fence: %v", err)
	}
	var n int
	if err := db.Session().Query(`SELECT COUNT(*) FROM libraries_deleted_by_org WHERE org_id = ? AND deleted_at = ? AND library_id = ?`,
		lib.OrgID, lib.DeletedAt, lib.LibraryID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("NONFENCING RED: the completion lost to the rows repaired at a fence value ahead of real time (trash rows=%d err=%v)", n, err)
	}
	if rows := nonfencingPendingRows(t, db, lib); len(rows) != 0 {
		t.Fatalf("continuation left after the completed delete: %+v", rows)
	}
}

// parkOwnerTransferBeforeCanonicalWrite parks the first owner transfer of lib
// after its canonical read and before its canonical write.
func parkOwnerTransferBeforeCanonicalWrite(t *testing.T, lib nonfencingLibrary) (<-chan struct{}, func()) {
	t.Helper()
	parked, resume := make(chan struct{}), make(chan struct{})
	var first, released sync.Once
	original := dbpkg.BeforeLibraryOwnerTransferFn
	dbpkg.BeforeLibraryOwnerTransferFn = func(orgID, libraryID string) {
		if libraryID == lib.LibraryID {
			park := false
			first.Do(func() { park = true })
			if park {
				close(parked)
				<-resume
			}
		}
		original(orgID, libraryID)
	}
	release := func() { released.Do(func() { close(resume) }) }
	t.Cleanup(func() {
		release()
		dbpkg.BeforeLibraryOwnerTransferFn = original
	})
	return parked, release
}

func assertNonfencingLibraryGone(t *testing.T, db *dbpkg.DB, lib nonfencingLibrary, newOwner, label string) {
	t.Helper()
	if present, _ := nonfencingCanonical(t, db, lib); present {
		t.Fatalf("NONFENCING RED: %s: the canonical library row exists after its permanent delete", label)
	}
	var n int
	if err := db.Session().Query(`SELECT COUNT(*) FROM libraries_by_id WHERE library_id = ?`, lib.LibraryID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("NONFENCING RED: %s: libraries_by_id survived the permanent delete (rows=%d err=%v)", label, n, err)
	}
	if newOwner != "" {
		if n := nonfencingOwnerListing(t, db, lib, newOwner); n != 0 {
			t.Fatalf("NONFENCING RED: %s: the new owner lists a permanently deleted library (rows=%d)", label, n)
		}
	}
}

// A15: an owner transfer reads the library, then a permanent delete commits and
// completes, then the transfer writes. It must not recreate the canonical row
// or the lookup.
func TestNonfencingA15OwnerTransferCannotRecreateDeletedLibrary(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	parked, release := parkOwnerTransferBeforeCanonicalWrite(t, lib)
	newOwner := uuid.NewString()
	transfer := runOwner(func() error { return updateLibraryOwner(db, lib.OrgID, lib.LibraryID, newOwner, time.Now().UTC()) })
	awaitParked(t, parked)
	if err := nonfencingPermanentDelete(db, lib); err != nil {
		t.Fatalf("permanent delete: %v", err)
	}
	release()
	err := awaitOwner(t, transfer)
	assertNonfencingLibraryGone(t, db, lib, newOwner, "owner transfer resumed after a permanent delete")
	if !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("transfer of a deleted library returned %v, want not found", err)
	}
}

// A16: the transfer's canonical LWT commits first; the permanent delete then
// commits and completes before the transfer writes its lookup and read model.
// The transfer must remove what it wrote once it sees the library gone.
func TestNonfencingA16OwnerTransferDerivedRowsAfterDeleteAreRemoved(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	parked, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	original := execOwnerTransferDerivedFn
	execOwnerTransferDerivedFn = func(batch *gocql.Batch) error {
		once.Do(func() { close(parked); <-resume })
		return original(batch)
	}
	t.Cleanup(func() {
		select {
		case <-resume:
		default:
			close(resume)
		}
		execOwnerTransferDerivedFn = original
	})
	newOwner := uuid.NewString()
	transfer := runOwner(func() error { return updateLibraryOwner(db, lib.OrgID, lib.LibraryID, newOwner, time.Now().UTC()) })
	awaitParked(t, parked)
	if err := nonfencingPermanentDelete(db, lib); err != nil {
		t.Fatalf("permanent delete after the transfer's canonical write: %v", err)
	}
	close(resume)
	err := awaitOwner(t, transfer)
	assertNonfencingLibraryGone(t, db, lib, newOwner, "transfer derived rows written after the permanent delete")
	if !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("transfer racing a permanent delete returned %v, want not found", err)
	}
}

// A17: an owner transfer on a node an hour ahead, then a soft delete and a
// permanent delete on normal nodes: the canonical row and the lookup must be
// gone.
func TestNonfencingA17FutureTimestampTransferDoesNotSurvivePermanentDelete(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedActiveLibrary(t, db)
	original := execOwnerTransferDerivedFn
	execOwnerTransferDerivedFn = func(batch *gocql.Batch) error {
		batch.WithTimestamp(time.Now().Add(time.Hour).UnixMicro())
		return original(batch)
	}
	t.Cleanup(func() { execOwnerTransferDerivedFn = original })
	if err := updateLibraryOwner(db, lib.OrgID, lib.LibraryID, uuid.NewString(), time.Now().UTC()); err != nil {
		t.Fatalf("transfer: %v", err)
	}
	execOwnerTransferDerivedFn = original
	lib.DeletedAt = nonfencingSoftDelete(t, db, lib)
	if err := nonfencingPermanentDelete(db, lib); err != nil {
		t.Fatalf("permanent delete: %v", err)
	}
	assertNonfencingLibraryGone(t, db, lib, "", "permanent delete after a future-timestamp transfer")
}
