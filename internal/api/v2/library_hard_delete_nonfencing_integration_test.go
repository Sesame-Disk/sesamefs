//go:build integration

package v2

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/Sesame-Disk/sesamefs/internal/middleware"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01 regressions against real Cassandra.
//
// Each stale-owner leg pauses the old owner right after its last successful
// lease renewal, ages that lease past the stale threshold (standing in for an
// arbitrary pause), lets a new owner take the lease over and finish its
// lifecycle transition, then resumes the old owner. The assertion is semantic:
// the old owner must not change the canonical lifecycle state the new owner
// committed.

type nonfencingLibrary struct {
	OrgID     string
	LibraryID string
	OwnerID   string
	DeletedAt time.Time
}

func (l nonfencingLibrary) candidate() trashLibraryCandidate {
	return trashLibraryCandidate{OrgID: l.OrgID, LibraryID: l.LibraryID, StorageClass: "hot", DeletedAt: l.DeletedAt}
}

func (l nonfencingLibrary) uuid(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(l.LibraryID)
	if err != nil {
		t.Fatalf("parse library id: %v", err)
	}
	return id
}

// nonfencingSeedTrashedLibrary creates an active library and soft-deletes it
// through the production softDeleteLibrary helper, so the canonical row and the
// deleted_libraries marker carry the same deleted_at generation.
func nonfencingSeedTrashedLibrary(t *testing.T, db *dbpkg.DB) nonfencingLibrary {
	t.Helper()
	session := db.Session()
	lib := nonfencingLibrary{OrgID: uuid.NewString(), LibraryID: uuid.NewString(), OwnerID: uuid.NewString()}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := session.Query(`
		INSERT INTO libraries (org_id, library_id, owner_id, name, encrypted, storage_class, size_bytes, file_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		lib.OrgID, lib.LibraryID, lib.OwnerID, "nonfencing", false, "hot", int64(0), int64(0), now, now).Exec(); err != nil {
		t.Fatalf("seed library: %v", err)
	}
	if err := session.Query(`
		INSERT INTO libraries_by_id (library_id, org_id, owner_id, name, encrypted)
		VALUES (?, ?, ?, ?, ?)`,
		lib.LibraryID, lib.OrgID, lib.OwnerID, "nonfencing", false).Exec(); err != nil {
		t.Fatalf("seed libraries_by_id: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Query(`DELETE FROM gc_library_hard_delete_locks WHERE library_id = ?`, lib.LibraryID).Exec()
		_ = session.Query(`DELETE FROM deleted_libraries WHERE library_id = ?`, lib.LibraryID).Exec()
		_ = session.Query(`DELETE FROM libraries_by_id WHERE library_id = ?`, lib.LibraryID).Exec()
		_ = session.Query(`DELETE FROM libraries WHERE org_id = ? AND library_id = ?`, lib.OrgID, lib.LibraryID).Exec()
	})
	lib.DeletedAt = nonfencingSoftDelete(t, db, lib)
	return lib
}

// nonfencingSoftDelete soft-deletes lib again and returns the new generation.
func nonfencingSoftDelete(t *testing.T, db *dbpkg.DB, lib nonfencingLibrary) time.Time {
	t.Helper()
	// deleted_at has millisecond precision; make each generation distinct.
	time.Sleep(5 * time.Millisecond)
	if err := softDeleteLibrary(db, lib.OrgID, lib.OwnerID, lib.OwnerID, lib.LibraryID); err != nil {
		t.Fatalf("softDeleteLibrary: %v", err)
	}
	present, deletedAt := nonfencingCanonical(t, db, lib)
	if !present || deletedAt.IsZero() {
		t.Fatalf("soft-deleted library: present=%v deleted_at=%v", present, deletedAt)
	}
	return deletedAt
}

// nonfencingCanonical reads the canonical row with a SERIAL read, so it observes
// every committed LWT on the partition.
func nonfencingCanonical(t *testing.T, db *dbpkg.DB, lib nonfencingLibrary) (bool, time.Time) {
	t.Helper()
	var deletedAt time.Time
	err := db.Session().Query(`SELECT deleted_at FROM libraries WHERE org_id = ? AND library_id = ?`,
		lib.OrgID, lib.LibraryID).Consistency(gocql.Serial).Scan(&deletedAt)
	if errors.Is(err, gocql.ErrNotFound) {
		return false, time.Time{}
	}
	if err != nil {
		t.Fatalf("read canonical library: %v", err)
	}
	return true, deletedAt
}

type nonfencingMarker struct {
	Present          bool
	DeletedAt        time.Time
	PurgeRequestedAt time.Time
}

func nonfencingReadMarker(t *testing.T, db *dbpkg.DB, lib nonfencingLibrary) nonfencingMarker {
	t.Helper()
	var m nonfencingMarker
	err := db.Session().Query(`SELECT deleted_at, purge_requested_at FROM deleted_libraries WHERE library_id = ?`,
		lib.LibraryID).Consistency(gocql.Serial).Scan(&m.DeletedAt, &m.PurgeRequestedAt)
	if errors.Is(err, gocql.ErrNotFound) {
		return m
	}
	if err != nil {
		t.Fatalf("read deleted_libraries marker: %v", err)
	}
	m.Present = true
	return m
}

func nonfencingLeaseToken(t *testing.T, db *dbpkg.DB, lib nonfencingLibrary) string {
	t.Helper()
	var token string
	err := db.Session().Query(`SELECT lease_token FROM gc_library_hard_delete_locks WHERE library_id = ?`,
		lib.LibraryID).Consistency(gocql.Serial).Scan(&token)
	if errors.Is(err, gocql.ErrNotFound) {
		return ""
	}
	if err != nil {
		t.Fatalf("read library hard-delete lease: %v", err)
	}
	return token
}

// nonfencingAgeLease makes the lease owned by token look stale, as if its owner
// had paused for longer than the stale-takeover threshold.
func nonfencingAgeLease(t *testing.T, db *dbpkg.DB, lib nonfencingLibrary, token uuid.UUID) {
	t.Helper()
	old := time.Now().UTC().Add(-3 * time.Hour)
	applied, err := db.Session().Query(`
		UPDATE gc_library_hard_delete_locks SET heartbeat = ? WHERE library_id = ? IF lease_token = ?`,
		old, lib.LibraryID, token.String()).SerialConsistency(gocql.Serial).MapScanCAS(map[string]interface{}{})
	if err != nil || !applied {
		t.Fatalf("age lease of the paused owner: applied=%v err=%v", applied, err)
	}
}

// nonfencingPause parks the first successful renewal of target's lease until
// release is called, and reports that owner's token.
type nonfencingPause struct {
	target  string
	paused  chan uuid.UUID
	resume  chan struct{}
	once    sync.Once
	release sync.Once
}

func newNonfencingPause(target string) *nonfencingPause {
	return &nonfencingPause{target: target, paused: make(chan uuid.UUID, 1), resume: make(chan struct{})}
}

func (p *nonfencingPause) after(libraryID, token uuid.UUID, owned bool, err error) {
	if err != nil || !owned || libraryID.String() != p.target {
		return
	}
	first := false
	p.once.Do(func() { first = true })
	if !first {
		return
	}
	p.paused <- token
	<-p.resume
}

func (p *nonfencingPause) wait(t *testing.T) uuid.UUID {
	t.Helper()
	select {
	case token := <-p.paused:
		return token
	case <-time.After(30 * time.Second):
		t.Fatal("old owner never reached its post-renewal pause")
		return uuid.Nil
	}
}

func (p *nonfencingPause) resumeOwner() { p.release.Do(func() { close(p.resume) }) }

// pauseDeleteAfterRenew parks the permanent-delete path after its fence renewal.
func pauseDeleteAfterRenew(t *testing.T, lib nonfencingLibrary) *nonfencingPause {
	t.Helper()
	p := newNonfencingPause(lib.LibraryID)
	original := renewLibraryHardDeleteLockLeaseFn
	renewLibraryHardDeleteLockLeaseFn = func(database *dbpkg.DB, libraryID, leaseToken uuid.UUID) (bool, error) {
		owned, err := original(database, libraryID, leaseToken)
		p.after(libraryID, leaseToken, owned, err)
		return owned, err
	}
	t.Cleanup(func() {
		p.resumeOwner()
		renewLibraryHardDeleteLockLeaseFn = original
	})
	return p
}

// pauseRestoreAfterRenew parks the restore path after its fence renewal.
func pauseRestoreAfterRenew(t *testing.T, lib nonfencingLibrary) *nonfencingPause {
	t.Helper()
	p := newNonfencingPause(lib.LibraryID)
	original := renewLibraryRestoreLeaseFn
	renewLibraryRestoreLeaseFn = func(session *gocql.Session, libraryID, leaseToken uuid.UUID) (bool, error) {
		owned, err := original(session, libraryID, leaseToken)
		p.after(libraryID, leaseToken, owned, err)
		return owned, err
	}
	t.Cleanup(func() {
		p.resumeOwner()
		renewLibraryRestoreLeaseFn = original
	})
	return p
}

// pauseRestoreAfterTransition parks the restore path after its canonical
// transition applied and before its completion writes.
func pauseRestoreAfterTransition(t *testing.T, lib nonfencingLibrary) *nonfencingPause {
	t.Helper()
	p := newNonfencingPause(lib.LibraryID)
	original := afterLibraryRestoreTransitionFn
	afterLibraryRestoreTransitionFn = func(libraryID string) {
		p.after(uuid.MustParse(libraryID), uuid.Nil, true, nil)
		original(libraryID)
	}
	t.Cleanup(func() {
		p.resumeOwner()
		afterLibraryRestoreTransitionFn = original
	})
	return p
}

// nonfencingProjection reads the admin read model: the deleted_at of the org
// row (zero when active) and whether the org trash listing has the library.
func nonfencingProjection(t *testing.T, db *dbpkg.DB, lib nonfencingLibrary) (present bool, deletedAt time.Time, inTrash bool) {
	t.Helper()
	err := db.Session().Query(`SELECT deleted_at FROM libraries_by_org_updated WHERE org_id = ? AND library_id = ?`,
		lib.OrgID, lib.LibraryID).Scan(&deletedAt)
	if err != nil && !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("read admin read model: %v", err)
	}
	present = err == nil
	row, err := dbpkg.ReadDeletedAdminLibraryProjectionRow(db.Session(), lib.OrgID, lib.LibraryID)
	if err != nil && !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("read admin trash read model: %v", err)
	}
	return present, deletedAt, err == nil && !row.DeletedAt.IsZero()
}

func nonfencingPermanentDelete(db *dbpkg.DB, lib nonfencingLibrary) error {
	// cleanupLinks=true is the PermanentDeleteRepo / org-admin single delete path.
	_, err := permanentlyDeleteTrashedLibraryCandidate(db, lib.candidate(), "permanent_delete", "PermanentDeleteRepo", true)
	return err
}

func nonfencingRestore(db *dbpkg.DB, lib nonfencingLibrary) error {
	return restoreDeletedLibrary(db, lib.OrgID, lib.OwnerID, lib.LibraryID)
}

func runOwner(fn func() error) <-chan error {
	done := make(chan error, 1)
	go func() { done <- fn() }()
	return done
}

func awaitOwner(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(60 * time.Second):
		t.Fatal("resumed old owner did not finish")
		return nil
	}
}

// T1: a single permanent-delete owner that keeps its lease deletes normally.
func TestNonfencingT1NormalPermanentDelete(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)

	if err := nonfencingPermanentDelete(db, lib); err != nil {
		t.Fatalf("permanent delete: %v", err)
	}
	if present, _ := nonfencingCanonical(t, db, lib); present {
		t.Fatal("canonical library row survived a normal permanent delete")
	}
	var orgID string
	if err := db.Session().Query(`SELECT org_id FROM libraries_by_id WHERE library_id = ?`, lib.LibraryID).Scan(&orgID); !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("libraries_by_id survived a normal permanent delete: err=%v", err)
	}
	marker := nonfencingReadMarker(t, db, lib)
	if !marker.Present || !marker.DeletedAt.Equal(lib.DeletedAt) || marker.PurgeRequestedAt.IsZero() {
		t.Fatalf("permanent delete marker = %+v, want deleted_at=%s with purge_requested_at", marker, lib.DeletedAt)
	}
	if token := nonfencingLeaseToken(t, db, lib); token != "" {
		t.Fatalf("lease %s left behind after a normal permanent delete", token)
	}
}

// T2: a single restore owner that keeps its lease restores normally.
func TestNonfencingT2NormalRestore(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)

	if err := nonfencingRestore(db, lib); err != nil {
		t.Fatalf("restore: %v", err)
	}
	present, deletedAt := nonfencingCanonical(t, db, lib)
	if !present || !deletedAt.IsZero() {
		t.Fatalf("after restore: present=%v deleted_at=%v, want an active library", present, deletedAt)
	}
	if marker := nonfencingReadMarker(t, db, lib); marker.Present {
		t.Fatalf("deleted_libraries marker survived a normal restore: %+v", marker)
	}
	if token := nonfencingLeaseToken(t, db, lib); token != "" {
		t.Fatalf("lease %s left behind after a normal restore", token)
	}
}

// T3 (Race A): a permanent delete renews, pauses, loses the lease to a restore
// that restores the library, then resumes. It must not delete the restored
// library.
func TestNonfencingT3StaleDeleteAfterRestore(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	pause := pauseDeleteAfterRenew(t, lib)

	oldOwner := runOwner(func() error { return nonfencingPermanentDelete(db, lib) })
	tokenA := pause.wait(t)
	nonfencingAgeLease(t, db, lib, tokenA)

	if err := nonfencingRestore(db, lib); err != nil {
		t.Fatalf("new owner restore after stale takeover: %v", err)
	}
	if present, deletedAt := nonfencingCanonical(t, db, lib); !present || !deletedAt.IsZero() {
		t.Fatalf("new owner restore did not commit: present=%v deleted_at=%v", present, deletedAt)
	}

	pause.resumeOwner()
	errA := awaitOwner(t, oldOwner)

	present, deletedAt := nonfencingCanonical(t, db, lib)
	if !present || !deletedAt.IsZero() {
		t.Fatalf("NONFENCING RED: stale permanent delete (err=%v) changed the library restored by the new owner: present=%v deleted_at=%v", errA, present, deletedAt)
	}
	if !errors.Is(errA, errPermanentDeleteCandidateStale) {
		t.Fatalf("stale permanent delete returned %v, want errPermanentDeleteCandidateStale", errA)
	}
	var orgID string
	if err := db.Session().Query(`SELECT org_id FROM libraries_by_id WHERE library_id = ?`, lib.LibraryID).Scan(&orgID); err != nil {
		t.Fatalf("stale permanent delete removed libraries_by_id of the restored library: %v", err)
	}
	if marker := nonfencingReadMarker(t, db, lib); marker.Present {
		t.Fatalf("stale permanent delete re-created a purge marker for the restored library: %+v", marker)
	}
}

// T4 (Race B): a restore renews, pauses, loses the lease to a permanent delete
// that removes the library, then resumes. It must not resurrect the library or
// drop the permanent-delete marker.
func TestNonfencingT4StaleRestoreAfterDelete(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	pause := pauseRestoreAfterRenew(t, lib)

	oldOwner := runOwner(func() error { return nonfencingRestore(db, lib) })
	tokenA := pause.wait(t)
	nonfencingAgeLease(t, db, lib, tokenA)

	if err := nonfencingPermanentDelete(db, lib); err != nil {
		t.Fatalf("new owner permanent delete after stale takeover: %v", err)
	}
	if present, _ := nonfencingCanonical(t, db, lib); present {
		t.Fatal("new owner permanent delete did not commit")
	}

	pause.resumeOwner()
	errA := awaitOwner(t, oldOwner)

	if present, deletedAt := nonfencingCanonical(t, db, lib); present {
		t.Fatalf("NONFENCING RED: stale restore (err=%v) resurrected the permanently deleted library: deleted_at=%v", errA, deletedAt)
	}
	marker := nonfencingReadMarker(t, db, lib)
	if !marker.Present || !marker.DeletedAt.Equal(lib.DeletedAt) || marker.PurgeRequestedAt.IsZero() {
		t.Fatalf("NONFENCING RED: stale restore (err=%v) dropped the permanent-delete marker: %+v", errA, marker)
	}
	if errA == nil {
		t.Fatal("stale restore reported success after the library was permanently deleted")
	}
}

// T4b: as T4, but the new owner is itself paused between its canonical delete
// and its completion writes, so the soft-delete marker is still in place when
// the stale restore resumes. Only the canonical restore fence stops it.
func TestNonfencingT4StaleRestoreBeforeDeleteCompletion(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	pause := pauseRestoreAfterRenew(t, lib)

	oldOwner := runOwner(func() error { return nonfencingRestore(db, lib) })
	tokenA := pause.wait(t)
	nonfencingAgeLease(t, db, lib, tokenA)
	tokenB := uuid.New()
	if acquired, err := gcpkg.AcquireLibraryHardDeleteLockLease(db.Session(), lib.uuid(t), tokenB); err != nil || !acquired {
		t.Fatalf("new owner takeover: acquired=%v err=%v", acquired, err)
	}
	if err := db.Session().Query(`DELETE FROM libraries WHERE org_id = ? AND library_id = ?`, lib.OrgID, lib.LibraryID).Exec(); err != nil {
		t.Fatalf("new owner canonical delete: %v", err)
	}

	pause.resumeOwner()
	errA := awaitOwner(t, oldOwner)

	if present, deletedAt := nonfencingCanonical(t, db, lib); present {
		t.Fatalf("NONFENCING RED: stale restore (err=%v) resurrected a library whose permanent delete had not completed yet: deleted_at=%v", errA, deletedAt)
	}
	if errA == nil {
		t.Fatal("stale restore reported success after the canonical row was deleted")
	}
	if token := nonfencingLeaseToken(t, db, lib); token != tokenB.String() {
		t.Fatalf("stale restore removed the new owner's lease: token=%q", token)
	}
}

// T5a: a permanent delete of generation D1 resumes after a restore and a new
// soft delete (generation D2). It must not delete generation D2.
func TestNonfencingT5StaleDeleteAgainstNewerGeneration(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	pause := pauseDeleteAfterRenew(t, lib)

	oldOwner := runOwner(func() error { return nonfencingPermanentDelete(db, lib) })
	tokenA := pause.wait(t)
	nonfencingAgeLease(t, db, lib, tokenA)
	if err := nonfencingRestore(db, lib); err != nil {
		t.Fatalf("new owner restore: %v", err)
	}
	d2 := nonfencingSoftDelete(t, db, lib)

	pause.resumeOwner()
	errA := awaitOwner(t, oldOwner)

	present, deletedAt := nonfencingCanonical(t, db, lib)
	if !present || !deletedAt.Equal(d2) {
		t.Fatalf("NONFENCING RED: stale generation-%s delete (err=%v) changed generation %s: present=%v deleted_at=%v", lib.DeletedAt, errA, d2, present, deletedAt)
	}
	marker := nonfencingReadMarker(t, db, lib)
	if !marker.Present || !marker.DeletedAt.Equal(d2) || !marker.PurgeRequestedAt.IsZero() {
		t.Fatalf("stale delete rewrote the generation-%s marker: %+v", d2, marker)
	}
	if !errors.Is(errA, errPermanentDeleteCandidateStale) {
		t.Fatalf("stale permanent delete returned %v, want errPermanentDeleteCandidateStale", errA)
	}
}

// T5b: a restore of generation D1 resumes after another restore and a new soft
// delete (generation D2). It must not restore generation D2.
func TestNonfencingT5StaleRestoreAgainstNewerGeneration(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	pause := pauseRestoreAfterRenew(t, lib)

	oldOwner := runOwner(func() error { return nonfencingRestore(db, lib) })
	tokenA := pause.wait(t)
	nonfencingAgeLease(t, db, lib, tokenA)
	if err := nonfencingRestore(db, lib); err != nil {
		t.Fatalf("new owner restore: %v", err)
	}
	d2 := nonfencingSoftDelete(t, db, lib)

	pause.resumeOwner()
	errA := awaitOwner(t, oldOwner)

	present, deletedAt := nonfencingCanonical(t, db, lib)
	if !present || !deletedAt.Equal(d2) {
		t.Fatalf("NONFENCING RED: stale generation-%s restore (err=%v) changed generation %s: present=%v deleted_at=%v", lib.DeletedAt, errA, d2, present, deletedAt)
	}
	marker := nonfencingReadMarker(t, db, lib)
	if !marker.Present || !marker.DeletedAt.Equal(d2) {
		t.Fatalf("NONFENCING RED: stale restore (err=%v) removed the generation-%s marker: %+v", errA, d2, marker)
	}
	if errA == nil {
		t.Fatal("stale restore reported success against a newer generation")
	}
}

// T5c: a restore whose canonical transition applied pauses before its
// completion writes; meanwhile the library is trashed again (generation D2).
// The resumed completion must not remove the D2 marker nor publish an active
// read model over D2.
func TestNonfencingT5StaleRestoreCompletionAgainstNewerGeneration(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	pause := pauseRestoreAfterTransition(t, lib)

	oldOwner := runOwner(func() error { return nonfencingRestore(db, lib) })
	pause.wait(t)
	if present, deletedAt := nonfencingCanonical(t, db, lib); !present || !deletedAt.IsZero() {
		t.Fatalf("restore did not commit before its marker cleanup: present=%v deleted_at=%v", present, deletedAt)
	}
	d2 := nonfencingSoftDelete(t, db, lib)

	pause.resumeOwner()
	errA := awaitOwner(t, oldOwner)

	marker := nonfencingReadMarker(t, db, lib)
	if !marker.Present || !marker.DeletedAt.Equal(d2) {
		t.Fatalf("NONFENCING RED: stale restore completion (err=%v) removed the generation-%s marker: %+v", errA, d2, marker)
	}
	if present, deletedAt := nonfencingCanonical(t, db, lib); !present || !deletedAt.Equal(d2) {
		t.Fatalf("stale restore completion changed generation %s: present=%v deleted_at=%v", d2, present, deletedAt)
	}
	if present, deletedAt, inTrash := nonfencingProjection(t, db, lib); !present || !deletedAt.Equal(d2) || !inTrash {
		t.Fatalf("NONFENCING RED: stale restore completion (err=%v) published an active read model over generation %s: present=%v deleted_at=%v in_trash=%v", errA, d2, present, deletedAt, inTrash)
	}
}

// T6: legitimate continuation is not rejected. A restore whose earlier attempt
// already removed the marker (and then failed) completes on retry, and a
// permanent delete of such a library still completes and writes its marker.
func TestNonfencingT6RetryAfterPartialAttempt(t *testing.T) {
	db := restoreGuardDBForTest(t)

	t.Run("restore", func(t *testing.T) {
		lib := nonfencingSeedTrashedLibrary(t, db)
		if err := db.Session().Query(`DELETE FROM deleted_libraries WHERE library_id = ?`, lib.LibraryID).Exec(); err != nil {
			t.Fatalf("drop marker: %v", err)
		}
		if err := nonfencingRestore(db, lib); err != nil {
			t.Fatalf("restore retry: %v", err)
		}
		if present, deletedAt := nonfencingCanonical(t, db, lib); !present || !deletedAt.IsZero() {
			t.Fatalf("restore retry did not restore: present=%v deleted_at=%v", present, deletedAt)
		}
	})

	t.Run("permanent delete", func(t *testing.T) {
		lib := nonfencingSeedTrashedLibrary(t, db)
		if err := db.Session().Query(`DELETE FROM deleted_libraries WHERE library_id = ?`, lib.LibraryID).Exec(); err != nil {
			t.Fatalf("drop marker: %v", err)
		}
		if err := nonfencingPermanentDelete(db, lib); err != nil {
			t.Fatalf("permanent delete: %v", err)
		}
		if present, _ := nonfencingCanonical(t, db, lib); present {
			t.Fatal("canonical library row survived permanent delete")
		}
		marker := nonfencingReadMarker(t, db, lib)
		if !marker.Present || !marker.DeletedAt.Equal(lib.DeletedAt) || marker.PurgeRequestedAt.IsZero() {
			t.Fatalf("permanent delete marker = %+v", marker)
		}
	})
}

// T7: a stale owner whose lease was taken over, and is now held by yet another
// token, cannot remove that owner's lease or change the lifecycle state.
func TestNonfencingT7StaleOwnerCannotTouchCurrentOwner(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	pause := pauseDeleteAfterRenew(t, lib)

	oldOwner := runOwner(func() error { return nonfencingPermanentDelete(db, lib) })
	tokenA := pause.wait(t)
	nonfencingAgeLease(t, db, lib, tokenA)
	if err := nonfencingRestore(db, lib); err != nil {
		t.Fatalf("new owner restore: %v", err)
	}
	tokenC := uuid.New()
	acquired, err := gcpkg.AcquireLibraryHardDeleteLockLease(db.Session(), lib.uuid(t), tokenC)
	if err != nil || !acquired {
		t.Fatalf("current owner acquire: acquired=%v err=%v", acquired, err)
	}

	pause.resumeOwner()
	errA := awaitOwner(t, oldOwner)

	if present, deletedAt := nonfencingCanonical(t, db, lib); !present || !deletedAt.IsZero() {
		t.Fatalf("NONFENCING RED: stale owner (err=%v) changed lifecycle state held by the current owner: present=%v deleted_at=%v", errA, present, deletedAt)
	}
	if token := nonfencingLeaseToken(t, db, lib); token != tokenC.String() {
		t.Fatalf("stale owner removed or replaced the current owner's lease: token=%q want %s", token, tokenC)
	}
	if errA == nil {
		t.Fatal("stale owner reported success")
	}
}

// The generation-fenced primitives against real Cassandra: each outcome,
// including the shape of a not-applied LWT on a missing row.
func TestNonfencingLifecyclePrimitiveOutcomes(t *testing.T) {
	db := restoreGuardDBForTest(t)
	session := db.Session()

	t.Run("delete", func(t *testing.T) {
		lib := nonfencingSeedTrashedLibrary(t, db)
		other := lib.DeletedAt.Add(-time.Second)
		if got, err := dbpkg.DeleteTrashedLibraryGeneration(session, lib.OrgID, lib.LibraryID, other); err != nil || got != dbpkg.LibraryLifecycleGenerationChanged {
			t.Fatalf("delete of another generation = %v, %v; want generation changed", got, err)
		}
		if present, _ := nonfencingCanonical(t, db, lib); !present {
			t.Fatal("delete of another generation removed the row")
		}
		if got, err := dbpkg.DeleteTrashedLibraryGeneration(session, lib.OrgID, lib.LibraryID, lib.DeletedAt); err != nil || got != dbpkg.LibraryLifecycleApplied {
			t.Fatalf("delete of the current generation = %v, %v; want applied", got, err)
		}
		if got, err := dbpkg.DeleteTrashedLibraryGeneration(session, lib.OrgID, lib.LibraryID, lib.DeletedAt); err != nil || got != dbpkg.LibraryLifecycleTargetAbsent {
			t.Fatalf("delete of a deleted library = %v, %v; want target absent", got, err)
		}
	})

	t.Run("delete active library", func(t *testing.T) {
		lib := nonfencingSeedTrashedLibrary(t, db)
		if err := nonfencingRestore(db, lib); err != nil {
			t.Fatalf("restore: %v", err)
		}
		if got, err := dbpkg.DeleteTrashedLibraryGeneration(session, lib.OrgID, lib.LibraryID, lib.DeletedAt); err != nil || got != dbpkg.LibraryLifecycleGenerationChanged {
			t.Fatalf("delete of an active library = %v, %v; want generation changed", got, err)
		}
	})

	t.Run("restore", func(t *testing.T) {
		lib := nonfencingSeedTrashedLibrary(t, db)
		now := time.Now().UTC()
		if got, err := dbpkg.RestoreTrashedLibraryGeneration(session, lib.OrgID, lib.LibraryID, lib.DeletedAt.Add(-time.Second), now); err != nil || got != dbpkg.LibraryLifecycleGenerationChanged {
			t.Fatalf("restore of another generation = %v, %v; want generation changed", got, err)
		}
		if got, err := dbpkg.RestoreTrashedLibraryGeneration(session, lib.OrgID, lib.LibraryID, lib.DeletedAt, now); err != nil || got != dbpkg.LibraryLifecycleApplied {
			t.Fatalf("restore of the current generation = %v, %v; want applied", got, err)
		}
		if present, deletedAt := nonfencingCanonical(t, db, lib); !present || !deletedAt.IsZero() {
			t.Fatalf("after restore: present=%v deleted_at=%v", present, deletedAt)
		}
		if got, err := dbpkg.RestoreTrashedLibraryGeneration(session, lib.OrgID, lib.LibraryID, lib.DeletedAt, now); err != nil || got != dbpkg.LibraryLifecycleGenerationChanged {
			t.Fatalf("second restore = %v, %v; want generation changed", got, err)
		}
	})

	t.Run("restore never creates a row", func(t *testing.T) {
		lib := nonfencingLibrary{OrgID: uuid.NewString(), LibraryID: uuid.NewString()}
		t.Cleanup(func() {
			_ = session.Query(`DELETE FROM libraries WHERE org_id = ? AND library_id = ?`, lib.OrgID, lib.LibraryID).Exec()
		})
		if got, err := dbpkg.RestoreTrashedLibraryGeneration(session, lib.OrgID, lib.LibraryID, time.Now().UTC(), time.Now().UTC()); err != nil || got != dbpkg.LibraryLifecycleTargetAbsent {
			t.Fatalf("restore of a missing row = %v, %v; want target absent", got, err)
		}
		if present, _ := nonfencingCanonical(t, db, lib); present {
			t.Fatal("restore of a missing row created a canonical row")
		}
	})

	t.Run("soft delete", func(t *testing.T) {
		lib := nonfencingSeedTrashedLibrary(t, db)
		d := time.Now().UTC().Truncate(time.Millisecond)
		if got, err := dbpkg.SoftDeleteLibraryGeneration(session, lib.OrgID, lib.LibraryID, d, lib.OwnerID); err != nil || got != dbpkg.LibraryLifecycleGenerationChanged {
			t.Fatalf("soft delete of a trashed library = %v, %v; want generation changed", got, err)
		}
		if _, deletedAt := nonfencingCanonical(t, db, lib); !deletedAt.Equal(lib.DeletedAt) {
			t.Fatalf("soft delete replaced trash generation %s with %s", lib.DeletedAt, deletedAt)
		}
		if err := nonfencingRestore(db, lib); err != nil {
			t.Fatalf("restore: %v", err)
		}
		if got, err := dbpkg.SoftDeleteLibraryGeneration(session, lib.OrgID, lib.LibraryID, d, lib.OwnerID); err != nil || got != dbpkg.LibraryLifecycleApplied {
			t.Fatalf("soft delete of an active library = %v, %v; want applied", got, err)
		}
		if _, deletedAt := nonfencingCanonical(t, db, lib); !deletedAt.Equal(d) {
			t.Fatalf("soft delete left deleted_at = %s, want %s", deletedAt, d)
		}
	})

	t.Run("soft delete never creates a row", func(t *testing.T) {
		lib := nonfencingLibrary{OrgID: uuid.NewString(), LibraryID: uuid.NewString()}
		t.Cleanup(func() {
			_ = session.Query(`DELETE FROM libraries WHERE org_id = ? AND library_id = ?`, lib.OrgID, lib.LibraryID).Exec()
		})
		if got, err := dbpkg.SoftDeleteLibraryGeneration(session, lib.OrgID, lib.LibraryID, time.Now().UTC(), uuid.NewString()); err != nil || got != dbpkg.LibraryLifecycleTargetAbsent {
			t.Fatalf("soft delete of a missing row = %v, %v; want target absent", got, err)
		}
		if present, _ := nonfencingCanonical(t, db, lib); present {
			t.Fatal("soft delete of a missing row created a canonical row")
		}
	})
}

// GC cascade path (GC_ENABLED=false in production): the store's hard delete is
// fenced on the same generation. A restored library is left alone; the trashed
// generation is deleted; a row already removed by an API permanent delete of the
// same generation still gets its completion writes.
func TestNonfencingGCHardDeleteLibraryIsGenerationFenced(t *testing.T) {
	db := restoreGuardDBForTest(t)
	store := gcpkg.NewCassandraStore(db)

	t.Run("restored library", func(t *testing.T) {
		lib := nonfencingSeedTrashedLibrary(t, db)
		if err := nonfencingRestore(db, lib); err != nil {
			t.Fatalf("restore: %v", err)
		}
		deleted, err := store.HardDeleteLibrary(uuid.MustParse(lib.OrgID), lib.uuid(t), lib.DeletedAt)
		if err != nil || deleted {
			t.Fatalf("stale GC hard delete of a restored library: deleted=%v err=%v", deleted, err)
		}
		if present, deletedAt := nonfencingCanonical(t, db, lib); !present || !deletedAt.IsZero() {
			t.Fatalf("NONFENCING RED: stale GC hard delete changed the restored library: present=%v deleted_at=%v", present, deletedAt)
		}
		var orgID string
		if err := db.Session().Query(`SELECT org_id FROM libraries_by_id WHERE library_id = ?`, lib.LibraryID).Scan(&orgID); err != nil {
			t.Fatalf("stale GC hard delete removed libraries_by_id: %v", err)
		}
	})

	t.Run("restore stopped before its marker cleanup", func(t *testing.T) {
		lib := nonfencingSeedTrashedLibrary(t, db)
		if outcome, err := dbpkg.RestoreTrashedLibraryGeneration(db.Session(), lib.OrgID, lib.LibraryID, lib.DeletedAt, time.Now().UTC()); err != nil || outcome != dbpkg.LibraryLifecycleApplied {
			t.Fatalf("canonical restore: %v, %v", outcome, err)
		}
		deleted, err := store.HardDeleteLibrary(uuid.MustParse(lib.OrgID), lib.uuid(t), lib.DeletedAt)
		if err != nil || deleted {
			t.Fatalf("GC hard delete of a restored library with a leftover marker: deleted=%v err=%v", deleted, err)
		}
		if present, deletedAt := nonfencingCanonical(t, db, lib); !present || !deletedAt.IsZero() {
			t.Fatalf("GC hard delete changed the restored library: present=%v deleted_at=%v", present, deletedAt)
		}
		if marker := nonfencingReadMarker(t, db, lib); marker.Present {
			t.Fatalf("GC left the restored library's stale marker behind: %+v", marker)
		}
	})

	t.Run("trashed generation", func(t *testing.T) {
		lib := nonfencingSeedTrashedLibrary(t, db)
		deleted, err := store.HardDeleteLibrary(uuid.MustParse(lib.OrgID), lib.uuid(t), lib.DeletedAt)
		if err != nil || !deleted {
			t.Fatalf("GC hard delete: deleted=%v err=%v", deleted, err)
		}
		if present, _ := nonfencingCanonical(t, db, lib); present {
			t.Fatal("GC hard delete left the canonical row")
		}
		if marker := nonfencingReadMarker(t, db, lib); marker.Present {
			t.Fatalf("GC hard delete left the marker: %+v", marker)
		}
	})

	t.Run("already permanently deleted", func(t *testing.T) {
		lib := nonfencingSeedTrashedLibrary(t, db)
		if err := nonfencingPermanentDelete(db, lib); err != nil {
			t.Fatalf("permanent delete: %v", err)
		}
		deleted, err := store.HardDeleteLibrary(uuid.MustParse(lib.OrgID), lib.uuid(t), lib.DeletedAt)
		if err != nil || !deleted {
			t.Fatalf("GC hard delete after permanent delete: deleted=%v err=%v", deleted, err)
		}
		if marker := nonfencingReadMarker(t, db, lib); marker.Present {
			t.Fatalf("GC hard delete left the purge marker: %+v", marker)
		}
	})
}

// Ordering does not depend on the client clock: a soft delete issued after a
// restore by a node whose clock is an hour behind Cassandra's still trashes the
// library, and its marker and read model still land after the restore's.
func TestNonfencingSoftDeleteAfterRestoreWithClientClockBehind(t *testing.T) {
	db := restoreGuardDBForTest(t)

	t.Run("characterization: a plain soft-delete batch loses", func(t *testing.T) {
		// The batch main's softDeleteLibrary issued, with the client clock behind
		// the restore's Paxos ballot: last-write-wins keeps the restore.
		lib := nonfencingSeedTrashedLibrary(t, db)
		if err := nonfencingRestore(db, lib); err != nil {
			t.Fatalf("restore: %v", err)
		}
		behind := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
		batch := db.Session().Batch(gocql.LoggedBatch).WithTimestamp(behind.UnixMicro())
		batch.Query(`UPDATE libraries SET deleted_at = ?, deleted_by = ?, updated_at = ? WHERE org_id = ? AND library_id = ?`,
			behind, lib.OwnerID, behind, lib.OrgID, lib.LibraryID)
		if err := batch.Exec(); err != nil {
			t.Fatalf("plain soft delete: %v", err)
		}
		if present, deletedAt := nonfencingCanonical(t, db, lib); !present || !deletedAt.IsZero() {
			t.Fatalf("expected the plain soft delete to lose to the restore LWT, got present=%v deleted_at=%v", present, deletedAt)
		}
	})

	t.Run("production soft delete", func(t *testing.T) {
		lib := nonfencingSeedTrashedLibrary(t, db)
		if err := nonfencingRestore(db, lib); err != nil {
			t.Fatalf("restore: %v", err)
		}
		original := libraryLifecycleNow
		libraryLifecycleNow = func() time.Time { return time.Now().Add(-time.Hour) }
		t.Cleanup(func() { libraryLifecycleNow = original })
		if err := softDeleteLibrary(db, lib.OrgID, lib.OwnerID, lib.OwnerID, lib.LibraryID); err != nil {
			t.Fatalf("soft delete: %v", err)
		}
		libraryLifecycleNow = original

		present, d2 := nonfencingCanonical(t, db, lib)
		if !present || d2.IsZero() {
			t.Fatalf("NONFENCING RED: soft delete after a restore lost with the client clock behind: present=%v deleted_at=%v", present, d2)
		}
		if marker := nonfencingReadMarker(t, db, lib); !marker.Present || !marker.DeletedAt.Equal(d2) {
			t.Fatalf("soft delete marker lost to the restore's marker removal: %+v", marker)
		}
		if projPresent, projDeletedAt, inTrash := nonfencingProjection(t, db, lib); !projPresent || !projDeletedAt.Equal(d2) || !inTrash {
			t.Fatalf("soft delete read model lost to the restore's: present=%v deleted_at=%v in_trash=%v", projPresent, projDeletedAt, inTrash)
		}
	})
}

// A permanent delete whose completion fails after its canonical delete applied
// is completed by repeating it; nothing relies on the GC cascade
// (GC_ENABLED=false).
func TestNonfencingPermanentDeleteCompletionFailureResumes(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)

	original := execLibraryLifecycleCompletionFn
	execLibraryLifecycleCompletionFn = func(*gocql.Batch) error { return errors.New("injected completion failure") }
	err := nonfencingPermanentDelete(db, lib)
	execLibraryLifecycleCompletionFn = original
	if !errors.Is(err, errHardDeleteLibraryBatchExec) {
		t.Fatalf("permanent delete with a failing completion = %v, want errHardDeleteLibraryBatchExec", err)
	}
	if present, _ := nonfencingCanonical(t, db, lib); present {
		t.Fatal("canonical delete did not apply before the injected failure")
	}
	var orgID string
	if err := db.Session().Query(`SELECT org_id FROM libraries_by_id WHERE library_id = ?`, lib.LibraryID).Scan(&orgID); err != nil {
		t.Fatalf("expected the lookup row to survive the failed completion: %v", err)
	}
	if marker := nonfencingReadMarker(t, db, lib); !marker.Present || !marker.PurgeRequestedAt.IsZero() {
		t.Fatalf("expected the soft-delete marker without purge request, got %+v", marker)
	}

	candidate, _, resumed, err := resumeCommittedPermanentDelete(db, lib.OrgID, lib.LibraryID)
	if err != nil || !resumed {
		t.Fatalf("resume: resumed=%v err=%v", resumed, err)
	}
	if !candidate.DeletedAt.Equal(lib.DeletedAt) {
		t.Fatalf("resumed generation %s, want %s", candidate.DeletedAt, lib.DeletedAt)
	}
	if err := db.Session().Query(`SELECT org_id FROM libraries_by_id WHERE library_id = ?`, lib.LibraryID).Scan(&orgID); !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("resume left libraries_by_id: err=%v", err)
	}
	marker := nonfencingReadMarker(t, db, lib)
	if !marker.Present || !marker.DeletedAt.Equal(lib.DeletedAt) || marker.PurgeRequestedAt.IsZero() {
		t.Fatalf("resume marker = %+v, want generation %s with purge_requested_at", marker, lib.DeletedAt)
	}
	if present, _, inTrash := nonfencingProjection(t, db, lib); present || inTrash {
		t.Fatalf("resume left the admin read model: present=%v in_trash=%v", present, inTrash)
	}
	if _, _, again, err := resumeCommittedPermanentDelete(db, lib.OrgID, lib.LibraryID); err != nil || again {
		t.Fatalf("second resume: resumed=%v err=%v, want nothing to do", again, err)
	}
}

// A soft delete whose completion fails after its canonical transition applied
// leaves the library in the trash; the repeated delete repairs its marker and
// read model.
func TestNonfencingSoftDeleteCompletionFailureRepairedOnRepeat(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	if err := nonfencingRestore(db, lib); err != nil {
		t.Fatalf("restore: %v", err)
	}

	original := execLibraryLifecycleCompletionFn
	execLibraryLifecycleCompletionFn = func(*gocql.Batch) error { return errors.New("injected completion failure") }
	err := softDeleteLibrary(db, lib.OrgID, lib.OwnerID, lib.OwnerID, lib.LibraryID)
	execLibraryLifecycleCompletionFn = original
	if err == nil {
		t.Fatal("soft delete with a failing completion reported success")
	}
	present, d2 := nonfencingCanonical(t, db, lib)
	if !present || d2.IsZero() {
		t.Fatalf("canonical soft delete did not apply: present=%v deleted_at=%v", present, d2)
	}
	if marker := nonfencingReadMarker(t, db, lib); marker.Present {
		t.Fatalf("expected no marker after the failed completion, got %+v", marker)
	}

	repairTrashedLibraryOnRepeatedDelete(db, lib.OrgID, lib.LibraryID)

	if marker := nonfencingReadMarker(t, db, lib); !marker.Present || !marker.DeletedAt.Equal(d2) {
		t.Fatalf("repair marker = %+v, want generation %s", marker, d2)
	}
	if projPresent, projDeletedAt, inTrash := nonfencingProjection(t, db, lib); !projPresent || !projDeletedAt.Equal(d2) || !inTrash {
		t.Fatalf("repair read model: present=%v deleted_at=%v in_trash=%v", projPresent, projDeletedAt, inTrash)
	}
}

// The resume is reachable through the real DELETE /repos/deleted/:repo_id
// handler: the owner repeats the request after a 500 and gets the permanent
// delete completed.
func TestNonfencingPermanentDeleteRepoHandlerResumes(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	if err := db.Session().Query(`INSERT INTO users (org_id, user_id, email, role) VALUES (?, ?, ?, ?)`,
		lib.OrgID, lib.OwnerID, lib.OwnerID+"@nonfencing.test", "user").Exec(); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Session().Query(`DELETE FROM users WHERE org_id = ? AND user_id = ?`, lib.OrgID, lib.OwnerID).Exec()
	})
	h := &DeletedLibraryHandler{db: db, permMiddleware: middleware.NewPermissionMiddleware(db)}
	r := gin.New()
	r.DELETE("/repos/deleted/:repo_id", func(c *gin.Context) {
		c.Set("org_id", lib.OrgID)
		c.Set("user_id", lib.OwnerID)
		h.PermanentDeleteRepo(c)
	})
	call := func() int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("DELETE", "/repos/deleted/"+lib.LibraryID, nil))
		return w.Code
	}

	original := execLibraryLifecycleCompletionFn
	execLibraryLifecycleCompletionFn = func(*gocql.Batch) error { return errors.New("injected completion failure") }
	first := call()
	execLibraryLifecycleCompletionFn = original
	if first != http.StatusInternalServerError {
		t.Fatalf("first attempt status = %d, want 500", first)
	}
	if second := call(); second != http.StatusOK {
		t.Fatalf("repeated permanent delete status = %d, want 200 (resumed)", second)
	}
	var orgID string
	if err := db.Session().Query(`SELECT org_id FROM libraries_by_id WHERE library_id = ?`, lib.LibraryID).Scan(&orgID); !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("resumed permanent delete left libraries_by_id: err=%v", err)
	}
	if marker := nonfencingReadMarker(t, db, lib); !marker.Present || marker.PurgeRequestedAt.IsZero() {
		t.Fatalf("resumed permanent delete marker = %+v, want purge_requested_at", marker)
	}
	if third := call(); third != http.StatusNotFound {
		t.Fatalf("permanent delete after completion status = %d, want 404", third)
	}
}
