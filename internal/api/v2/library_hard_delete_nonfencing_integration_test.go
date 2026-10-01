//go:build integration

package v2

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/Sesame-Disk/sesamefs/internal/middleware"
	"github.com/Sesame-Disk/sesamefs/internal/traffic"
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
	lib := nonfencingSeedActiveLibrary(t, db)
	lib.DeletedAt = nonfencingSoftDelete(t, db, lib)
	return lib
}

// nonfencingKeepSeeds keeps seeded rows past the test (multi-phase 3-DC legs).
var nonfencingKeepSeeds bool

// nonfencingSeedActiveLibrary creates an active library with its lookup row.
func nonfencingSeedActiveLibrary(t *testing.T, db *dbpkg.DB) nonfencingLibrary {
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
		if nonfencingKeepSeeds {
			return
		}
		_ = session.Query(`DELETE FROM gc_library_hard_delete_locks WHERE library_id = ?`, lib.LibraryID).Exec()
		_ = session.Query(`DELETE FROM deleted_libraries WHERE library_id = ?`, lib.LibraryID).Exec()
		_ = session.Query(`DELETE FROM libraries_by_id WHERE library_id = ?`, lib.LibraryID).Exec()
		_ = session.Query(`DELETE FROM libraries WHERE org_id = ? AND library_id = ?`, lib.OrgID, lib.LibraryID).Exec()
	})
	return lib
}

// nonfencingSoftDelete soft-deletes lib again and returns the new generation.
func nonfencingSoftDelete(t *testing.T, db *dbpkg.DB, lib nonfencingLibrary) time.Time {
	t.Helper()
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
	original := afterLibraryLifecycleTransitionFn
	afterLibraryLifecycleTransitionFn = func(operation, libraryID string) error {
		if operation == "restore" {
			p.after(uuid.MustParse(libraryID), uuid.Nil, true, nil)
		}
		return original(operation, libraryID)
	}
	t.Cleanup(func() {
		p.resumeOwner()
		afterLibraryLifecycleTransitionFn = original
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
		if _, got, err := dbpkg.RestoreTrashedLibraryGeneration(session, lib.OrgID, lib.LibraryID, lib.DeletedAt.Add(-time.Second), now); err != nil || got != dbpkg.LibraryLifecycleGenerationChanged {
			t.Fatalf("restore of another generation = %v, %v; want generation changed", got, err)
		}
		if _, got, err := dbpkg.RestoreTrashedLibraryGeneration(session, lib.OrgID, lib.LibraryID, lib.DeletedAt, now); err != nil || got != dbpkg.LibraryLifecycleApplied {
			t.Fatalf("restore of the current generation = %v, %v; want applied", got, err)
		}
		if present, deletedAt := nonfencingCanonical(t, db, lib); !present || !deletedAt.IsZero() {
			t.Fatalf("after restore: present=%v deleted_at=%v", present, deletedAt)
		}
		if _, got, err := dbpkg.RestoreTrashedLibraryGeneration(session, lib.OrgID, lib.LibraryID, lib.DeletedAt, now); err != nil || got != dbpkg.LibraryLifecycleGenerationChanged {
			t.Fatalf("second restore = %v, %v; want generation changed", got, err)
		}
	})

	t.Run("restore never creates a row", func(t *testing.T) {
		lib := nonfencingLibrary{OrgID: uuid.NewString(), LibraryID: uuid.NewString()}
		t.Cleanup(func() {
			_ = session.Query(`DELETE FROM libraries WHERE org_id = ? AND library_id = ?`, lib.OrgID, lib.LibraryID).Exec()
		})
		if _, got, err := dbpkg.RestoreTrashedLibraryGeneration(session, lib.OrgID, lib.LibraryID, time.Now().UTC(), time.Now().UTC()); err != nil || got != dbpkg.LibraryLifecycleTargetAbsent {
			t.Fatalf("restore of a missing row = %v, %v; want target absent", got, err)
		}
		if present, _ := nonfencingCanonical(t, db, lib); present {
			t.Fatal("restore of a missing row created a canonical row")
		}
	})

	t.Run("soft delete", func(t *testing.T) {
		lib := nonfencingSeedTrashedLibrary(t, db)
		now := time.Now().UTC()
		if d, got, err := dbpkg.SoftDeleteLibraryGeneration(session, lib.OrgID, lib.LibraryID, lib.OwnerID, now); err != nil || got != dbpkg.LibraryLifecycleGenerationChanged || !d.Equal(lib.DeletedAt) {
			t.Fatalf("soft delete of a trashed library = %v, %v, %v; want generation changed at %s", d, got, err, lib.DeletedAt)
		}
		if err := nonfencingRestore(db, lib); err != nil {
			t.Fatalf("restore: %v", err)
		}
		d, got, err := dbpkg.SoftDeleteLibraryGeneration(session, lib.OrgID, lib.LibraryID, lib.OwnerID, now)
		if err != nil || got != dbpkg.LibraryLifecycleApplied {
			t.Fatalf("soft delete of an active library = %v, %v", got, err)
		}
		if !d.After(lib.DeletedAt) {
			t.Fatalf("new generation %s is not after the previous one %s", d, lib.DeletedAt)
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
		if _, got, err := dbpkg.SoftDeleteLibraryGeneration(session, lib.OrgID, lib.LibraryID, uuid.NewString(), time.Now().UTC()); err != nil || got != dbpkg.LibraryLifecycleTargetAbsent {
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
		if _, outcome, err := dbpkg.RestoreTrashedLibraryGeneration(db.Session(), lib.OrgID, lib.LibraryID, lib.DeletedAt, time.Now().UTC()); err != nil || outcome != dbpkg.LibraryLifecycleApplied {
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

	original := dbpkg.ExecLibraryLifecycleCompletionFn
	dbpkg.ExecLibraryLifecycleCompletionFn = func(*gocql.Batch) error { return errors.New("injected completion failure") }
	err := nonfencingPermanentDelete(db, lib)
	dbpkg.ExecLibraryLifecycleCompletionFn = original
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

	original := dbpkg.ExecLibraryLifecycleCompletionFn
	dbpkg.ExecLibraryLifecycleCompletionFn = func(*gocql.Batch) error { return errors.New("injected completion failure") }
	err := softDeleteLibrary(db, lib.OrgID, lib.OwnerID, lib.OwnerID, lib.LibraryID)
	dbpkg.ExecLibraryLifecycleCompletionFn = original
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

	original := dbpkg.ExecLibraryLifecycleCompletionFn
	dbpkg.ExecLibraryLifecycleCompletionFn = func(*gocql.Batch) error { return errors.New("injected completion failure") }
	first := call()
	dbpkg.ExecLibraryLifecycleCompletionFn = original
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

// withLibraryLifecycleClock runs the lifecycle helpers with the given clock.
func withLibraryLifecycleClock(t *testing.T, clock func() time.Time) {
	t.Helper()
	original := libraryLifecycleNow
	libraryLifecycleNow = clock
	t.Cleanup(func() { libraryLifecycleNow = original })
}

// failLibraryLifecycleCompletions makes every lifecycle completion batch fail
// until the returned function is called.
func failLibraryLifecycleCompletions(t *testing.T) func() {
	t.Helper()
	original := dbpkg.ExecLibraryLifecycleCompletionFn
	dbpkg.ExecLibraryLifecycleCompletionFn = func(*gocql.Batch) error { return errors.New("injected completion failure") }
	restore := func() { dbpkg.ExecLibraryLifecycleCompletionFn = original }
	t.Cleanup(restore)
	return restore
}

func nonfencingTrashRows(t *testing.T, db *dbpkg.DB, lib nonfencingLibrary) []time.Time {
	t.Helper()
	rows, err := dbpkg.ListDeletedAdminLibraryRowsByOrg(db.Session(), lib.OrgID)
	if err != nil {
		t.Fatalf("list trash read model: %v", err)
	}
	var generations []time.Time
	for _, row := range rows {
		if row.LibraryID == lib.LibraryID {
			generations = append(generations, row.DeletedAt)
		}
	}
	return generations
}

// assertNonfencingTrashedDerivedState checks that the marker and every admin
// read-model row show generation d.
func assertNonfencingTrashedDerivedState(t *testing.T, db *dbpkg.DB, lib nonfencingLibrary, d time.Time, label string) {
	t.Helper()
	if present, deletedAt := nonfencingCanonical(t, db, lib); !present || !deletedAt.Equal(d) {
		t.Fatalf("%s: canonical present=%v deleted_at=%v, want %s", label, present, deletedAt, d)
	}
	if marker := nonfencingReadMarker(t, db, lib); !marker.Present || !marker.DeletedAt.Equal(d) || !marker.PurgeRequestedAt.IsZero() {
		t.Fatalf("%s: marker = %+v, want the soft-delete marker of %s", label, marker, d)
	}
	if present, deletedAt, _ := nonfencingProjection(t, db, lib); !present || !deletedAt.Equal(d) {
		t.Fatalf("%s: admin read model present=%v deleted_at=%v, want %s", label, present, deletedAt, d)
	}
	if rows := nonfencingTrashRows(t, db, lib); len(rows) != 1 || !rows[0].Equal(d) {
		t.Fatalf("%s: trash read-model generations = %v, want only %s", label, rows, d)
	}
}

// G1: two trash generations created in the same millisecond (the clock does
// not move at all) are still distinct, and a stale permanent delete of the
// first cannot delete the second.
func TestNonfencingG1SameMillisecondGenerations(t *testing.T) {
	db := restoreGuardDBForTest(t)
	frozen := time.Now().UTC().Truncate(time.Millisecond)
	withLibraryLifecycleClock(t, func() time.Time { return frozen })
	lib := nonfencingSeedTrashedLibrary(t, db)
	pause := pauseDeleteAfterRenew(t, lib)

	oldOwner := runOwner(func() error { return nonfencingPermanentDelete(db, lib) })
	tokenA := pause.wait(t)
	nonfencingAgeLease(t, db, lib, tokenA)
	if err := nonfencingRestore(db, lib); err != nil {
		t.Fatalf("restore: %v", err)
	}
	d2 := nonfencingSoftDelete(t, db, lib)
	if !d2.After(lib.DeletedAt) {
		t.Fatalf("NONFENCING RED: second trash generation %s is not after the first %s under a frozen clock", d2, lib.DeletedAt)
	}

	pause.resumeOwner()
	errA := awaitOwner(t, oldOwner)
	if present, deletedAt := nonfencingCanonical(t, db, lib); !present || !deletedAt.Equal(d2) {
		t.Fatalf("NONFENCING RED: stale generation-%s delete (err=%v) acted on generation %s: present=%v deleted_at=%v", lib.DeletedAt, errA, d2, present, deletedAt)
	}
	if !errors.Is(errA, errPermanentDeleteCandidateStale) {
		t.Fatalf("stale delete returned %v, want errPermanentDeleteCandidateStale", errA)
	}
	assertNonfencingTrashedDerivedState(t, db, lib, d2, "after the stale delete")
}

// G2: a restore on a node whose clock is an hour ahead commits and pauses
// before its completion; a soft delete on a node an hour behind then commits
// and completes. The late restore completion must not override it.
func TestNonfencingG2FastOldCompletionSlowNewTransition(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	withLibraryLifecycleClock(t, func() time.Time { return time.Now().Add(time.Hour) })
	pause := pauseRestoreAfterTransition(t, lib)
	oldOwner := runOwner(func() error { return nonfencingRestore(db, lib) })
	pause.wait(t)

	libraryLifecycleNow = func() time.Time { return time.Now().Add(-time.Hour) }
	d2 := nonfencingSoftDelete(t, db, lib)

	pause.resumeOwner()
	errA := awaitOwner(t, oldOwner)
	if errA != nil {
		t.Fatalf("restore completion: %v", errA)
	}
	assertNonfencingTrashedDerivedState(t, db, lib, d2, "NONFENCING RED: late restore completion from a fast node over a newer generation")
}

// G2b: a restore on a node an hour ahead completes; then a soft delete on a
// node an hour behind. Its marker and trash row must not lose to the restore's
// earlier, future-stamped marker removal.
func TestNonfencingG2bSlowSoftDeleteAfterFastRestore(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	withLibraryLifecycleClock(t, func() time.Time { return time.Now().Add(time.Hour) })
	if err := nonfencingRestore(db, lib); err != nil {
		t.Fatalf("restore: %v", err)
	}
	libraryLifecycleNow = func() time.Time { return time.Now().Add(-time.Hour) }
	d2 := nonfencingSoftDelete(t, db, lib)
	assertNonfencingTrashedDerivedState(t, db, lib, d2, "NONFENCING RED: slow soft delete after a fast restore")
}

// G3: the same schedule with one frozen clock (both transitions would get the
// same client time): the lifecycle clock still orders them.
func TestNonfencingG3EqualClockOldCompletion(t *testing.T) {
	db := restoreGuardDBForTest(t)
	frozen := time.Now().UTC().Truncate(time.Millisecond)
	withLibraryLifecycleClock(t, func() time.Time { return frozen })
	lib := nonfencingSeedTrashedLibrary(t, db)
	pause := pauseRestoreAfterTransition(t, lib)
	oldOwner := runOwner(func() error { return nonfencingRestore(db, lib) })
	pause.wait(t)
	d2 := nonfencingSoftDelete(t, db, lib)
	pause.resumeOwner()
	if errA := awaitOwner(t, oldOwner); errA != nil {
		t.Fatalf("restore completion: %v", errA)
	}
	assertNonfencingTrashedDerivedState(t, db, lib, d2, "NONFENCING RED: late restore completion with an equal clock over a newer generation")
}

// nonfencingSeedCountedLibrary is an active library of 1000 bytes / 2 files
// whose lib, org and user storage counters include it.
func nonfencingSeedCountedLibrary(t *testing.T, db *dbpkg.DB) nonfencingLibrary {
	t.Helper()
	lib := nonfencingSeedActiveLibrary(t, db)
	if err := db.Session().Query(`UPDATE libraries SET size_bytes = ?, file_count = ? WHERE org_id = ? AND library_id = ?`,
		int64(1000), int64(2), lib.OrgID, lib.LibraryID).Exec(); err != nil {
		t.Fatalf("seed library size: %v", err)
	}
	if err := traffic.IncrementStorageCountersSync(db, lib.OrgID, lib.OwnerID, lib.LibraryID, 1000, 2); err != nil {
		t.Fatalf("seed storage counters: %v", err)
	}
	return lib
}

func nonfencingOrgAndUserBytes(db *dbpkg.DB, lib nonfencingLibrary) (int64, int64) {
	return traffic.ReadStorageSnapshot(db, traffic.OrganizationStorageScope(lib.OrgID)).BytesUsed,
		traffic.ReadStorageSnapshot(db, traffic.UserStorageScope(lib.OrgID, lib.OwnerID)).BytesUsed
}

// simulateDeathAfterLifecycleTransition makes the process "die" right after the
// canonical transition of the given operation: no counter adjustment, no
// completion.
func simulateDeathAfterLifecycleTransition(t *testing.T, operation string) {
	t.Helper()
	original := afterLibraryLifecycleTransitionFn
	afterLibraryLifecycleTransitionFn = func(op, libraryID string) error {
		if op == operation {
			return errors.New("simulated process death after the canonical transition")
		}
		return original(op, libraryID)
	}
	t.Cleanup(func() { afterLibraryLifecycleTransitionFn = original })
}

// reconcileBeforeLifecycleTransition runs the storage reconciliation and the
// lifecycle reaper right before the canonical LWT of the given operation (R1:
// the consumer is as early as it can be). The pending continuation must
// survive: its transition can still apply.
func reconcileBeforeLifecycleTransition(t *testing.T, db *dbpkg.DB, operation string) {
	t.Helper()
	original := beforeLibraryLifecycleTransitionFn
	beforeLibraryLifecycleTransitionFn = func(op, libraryID string) error {
		if op == operation {
			if err := RecoverPendingLibraryLifecycles(context.Background(), db); err != nil {
				t.Errorf("early reaper sweep: %v", err)
			}
			if _, err := gcpkg.NewCassandraStore(db).ReconcilePendingStorageCounters(); err != nil {
				t.Errorf("early reconciliation: %v", err)
			}
		}
		return original(op, libraryID)
	}
	t.Cleanup(func() { beforeLibraryLifecycleTransitionFn = original })
}

// nonfencingRecover runs what a later, independent pass does: the lifecycle
// reaper, then the storage reconciliation.
func nonfencingRecover(t *testing.T, db *dbpkg.DB) {
	t.Helper()
	if err := RecoverPendingLibraryLifecycles(context.Background(), db); err != nil {
		t.Fatalf("lifecycle reaper: %v", err)
	}
	if _, err := gcpkg.NewCassandraStore(db).ReconcilePendingStorageCounters(); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
}

// G4 / R1: the storage reconciliation runs right before the canonical soft
// delete (consuming every pending request), the process dies right after the
// transition, and nobody retries. The continuation survives the early pass;
// the next independent pass (reaper + reconciliation) converges the counters.
func TestNonfencingG4SoftDeleteDeathAccountingConverges(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedCountedLibrary(t, db)
	reconcileBeforeLifecycleTransition(t, db, "soft-delete")
	simulateDeathAfterLifecycleTransition(t, "soft-delete")
	if err := softDeleteLibrary(db, lib.OrgID, lib.OwnerID, lib.OwnerID, lib.LibraryID); err == nil {
		t.Fatal("expected the simulated death to surface")
	}
	if present, deletedAt := nonfencingCanonical(t, db, lib); !present || deletedAt.IsZero() {
		t.Fatalf("canonical soft delete did not commit: present=%v deleted_at=%v", present, deletedAt)
	}
	if org, user := nonfencingOrgAndUserBytes(db, lib); org != 1000 || user != 1000 {
		t.Fatalf("setup: counters before recovery org=%d user=%d, want the unadjusted 1000", org, user)
	}
	nonfencingRecover(t, db)
	if org, user := nonfencingOrgAndUserBytes(db, lib); org != 0 || user != 0 {
		t.Fatalf("NONFENCING RED: counters after recovery org=%d user=%d, want 0 (library trashed)", org, user)
	}
}

// G5 / R1: the same for a restore.
func TestNonfencingG5RestoreDeathAccountingConverges(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedCountedLibrary(t, db)
	lib.DeletedAt = nonfencingSoftDelete(t, db, lib)
	if org, user := nonfencingOrgAndUserBytes(db, lib); org != 0 || user != 0 {
		t.Fatalf("setup: counters after soft delete org=%d user=%d, want 0", org, user)
	}
	reconcileBeforeLifecycleTransition(t, db, "restore")
	simulateDeathAfterLifecycleTransition(t, "restore")
	if err := nonfencingRestore(db, lib); err == nil {
		t.Fatal("expected the simulated death to surface")
	}
	if present, deletedAt := nonfencingCanonical(t, db, lib); !present || !deletedAt.IsZero() {
		t.Fatalf("canonical restore did not commit: present=%v deleted_at=%v", present, deletedAt)
	}
	nonfencingRecover(t, db)
	if org, user := nonfencingOrgAndUserBytes(db, lib); org != 1000 || user != 1000 {
		t.Fatalf("NONFENCING RED: counters after recovery org=%d user=%d, want 1000 (library active)", org, user)
	}
}

// R5: a soft delete whose process dies right after the canonical transition,
// with no second request: the independent reaper pass rebuilds its marker and
// read model, so trash retention (Phase 13) discovers the library.
func TestNonfencingR5SoftDeleteDeathRecoveredByReaper(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedActiveLibrary(t, db)
	simulateDeathAfterLifecycleTransition(t, "soft-delete")
	if err := softDeleteLibrary(db, lib.OrgID, lib.OwnerID, lib.OwnerID, lib.LibraryID); err == nil {
		t.Fatal("expected the simulated death to surface")
	}
	_, d := nonfencingCanonical(t, db, lib)
	if marker := nonfencingReadMarker(t, db, lib); marker.Present {
		t.Fatalf("setup: marker written before the simulated death: %+v", marker)
	}
	afterLibraryLifecycleTransitionFn = func(string, string) error { return nil }

	if err := RecoverPendingLibraryLifecycles(context.Background(), db); err != nil {
		t.Fatalf("lifecycle reaper: %v", err)
	}
	assertNonfencingTrashedDerivedState(t, db, lib, d, "NONFENCING RED: reaper after a soft delete died")
	expired, err := gcpkg.NewCassandraStore(db).ListExpiredDeletedLibraries(0)
	if err != nil {
		t.Fatalf("phase 13 listing: %v", err)
	}
	found := false
	for _, e := range expired {
		if e.LibraryID.String() == lib.LibraryID {
			found = true
		}
	}
	if !found {
		t.Fatal("NONFENCING RED: Phase 13 does not discover the trashed library")
	}
}

// R2: a soft delete on a node an hour ahead pushes the lifecycle clock ahead;
// the restore that follows on a normal node inherits it. Ordinary writers on a
// normal clock (an owner transfer) must still win in every read model.
func TestNonfencingR2LifecycleClockDoesNotPoisonOrdinaryWrites(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedActiveLibrary(t, db)
	withLibraryLifecycleClock(t, func() time.Time { return time.Now().Add(time.Hour) })
	lib.DeletedAt = nonfencingSoftDelete(t, db, lib)
	libraryLifecycleNow = time.Now
	if err := nonfencingRestore(db, lib); err != nil {
		t.Fatalf("restore: %v", err)
	}

	newOwner := uuid.NewString()
	if err := updateLibraryOwner(db, lib.OrgID, lib.LibraryID, newOwner, time.Now().UTC()); err != nil {
		t.Fatalf("transfer: %v", err)
	}
	var count int
	if err := db.Session().Query(`SELECT COUNT(*) FROM libraries_by_owner WHERE org_id = ? AND owner_id = ? AND library_id = ?`, lib.OrgID, lib.OwnerID, lib.LibraryID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("NONFENCING RED: old owner still lists the library (rows=%d, err=%v)", count, err)
	}
	if err := db.Session().Query(`SELECT COUNT(*) FROM libraries_by_owner WHERE org_id = ? AND owner_id = ? AND library_id = ?`, lib.OrgID, newOwner, lib.LibraryID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("new owner does not list the library (rows=%d, err=%v)", count, err)
	}
}

// R6: a trash listing row of an old generation that reappears late (for example
// a delayed write from another datacenter) is dropped by the admin trash
// reconciliation, which keeps only the current generation's row.
func TestNonfencingR6LateOldGenerationTrashRowIsReconciled(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	d1 := lib.DeletedAt
	if err := nonfencingRestore(db, lib); err != nil {
		t.Fatalf("restore: %v", err)
	}
	d2 := nonfencingSoftDelete(t, db, lib)
	if err := db.Session().Query(`INSERT INTO libraries_deleted_by_org (org_id, deleted_at, library_id, owner_id, name) VALUES (?, ?, ?, ?, ?)`,
		lib.OrgID, d1, lib.LibraryID, lib.OwnerID, "late").Exec(); err != nil {
		t.Fatalf("late D1 row: %v", err)
	}
	kept, _, err := dbpkg.ReconcileDeletedAdminLibraryRowsByOrg(db.Session(), lib.OrgID)
	if err != nil {
		t.Fatalf("reconcile trash listing: %v", err)
	}
	for _, row := range kept {
		if row.LibraryID == lib.LibraryID && !row.DeletedAt.Equal(d2) {
			t.Fatalf("NONFENCING RED: trash listing kept generation %s next to the current %s", row.DeletedAt, d2)
		}
	}
	if rows := nonfencingTrashRows(t, db, lib); len(rows) != 1 || !rows[0].Equal(d2) {
		t.Fatalf("trash rows after reconciliation = %v, want only %s", rows, d2)
	}
}

// G6: a bulk org trash clean whose completion fails after the canonical delete
// is completed by repeating the same bulk clean through its HTTP handler.
func TestNonfencingG6BulkCleanResumesCommittedDelete(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	adminID := uuid.NewString()
	if err := db.Session().Query(`INSERT INTO users (org_id, user_id, email, role) VALUES (?, ?, ?, ?)`,
		lib.OrgID, adminID, adminID+"@nonfencing.test", "admin").Exec(); err != nil {
		t.Fatalf("seed org admin: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Session().Query(`DELETE FROM users WHERE org_id = ? AND user_id = ?`, lib.OrgID, adminID).Exec()
	})
	h := &OrgAdminHandler{db: db, permMiddleware: middleware.NewPermissionMiddleware(db)}
	r := gin.New()
	r.DELETE("/org/:org_id/admin/trash-libraries", func(c *gin.Context) {
		c.Set("org_id", lib.OrgID)
		c.Set("user_id", adminID)
		h.CleanOrgTrashLibraries(c)
	})
	clean := func() int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("DELETE", "/org/"+lib.OrgID+"/admin/trash-libraries", nil))
		return w.Code
	}

	restore := failLibraryLifecycleCompletions(t)
	clean()
	restore()
	if present, _ := nonfencingCanonical(t, db, lib); present {
		t.Fatal("setup: the canonical delete did not apply")
	}
	var orgID string
	if err := db.Session().Query(`SELECT org_id FROM libraries_by_id WHERE library_id = ?`, lib.LibraryID).Scan(&orgID); err != nil {
		t.Fatalf("setup: expected the lookup to survive the failed completion: %v", err)
	}

	if code := clean(); code != http.StatusOK {
		t.Fatalf("repeated bulk clean status = %d", code)
	}
	if err := db.Session().Query(`SELECT org_id FROM libraries_by_id WHERE library_id = ?`, lib.LibraryID).Scan(&orgID); !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("NONFENCING RED: repeated bulk clean left libraries_by_id: err=%v", err)
	}
	if marker := nonfencingReadMarker(t, db, lib); !marker.Present || marker.PurgeRequestedAt.IsZero() {
		t.Fatalf("NONFENCING RED: repeated bulk clean marker = %+v, want purge_requested_at", marker)
	}
}

// G7: a permanent delete of a trashed library whose marker is missing (its soft
// delete completion failed) and whose own completion fails is completed by the
// repeated single delete.
func TestNonfencingG7MissingMarkerPermanentDeleteResumes(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedActiveLibrary(t, db)
	restore := failLibraryLifecycleCompletions(t)
	if err := softDeleteLibrary(db, lib.OrgID, lib.OwnerID, lib.OwnerID, lib.LibraryID); err == nil {
		t.Fatal("setup: soft delete completion should fail")
	}
	_, lib.DeletedAt = nonfencingCanonical(t, db, lib)
	if marker := nonfencingReadMarker(t, db, lib); marker.Present {
		t.Fatalf("setup: expected no marker, got %+v", marker)
	}
	if err := nonfencingPermanentDelete(db, lib); !errors.Is(err, errHardDeleteLibraryBatchExec) {
		t.Fatalf("permanent delete with a failing completion = %v", err)
	}
	restore()
	if present, _ := nonfencingCanonical(t, db, lib); present {
		t.Fatal("setup: the canonical delete did not apply")
	}
	if _, _, resumed, err := resumeCommittedPermanentDelete(db, lib.OrgID, lib.LibraryID); err != nil || !resumed {
		t.Fatalf("NONFENCING RED: repeated delete could not resume: resumed=%v err=%v", resumed, err)
	}
	var orgID string
	if err := db.Session().Query(`SELECT org_id FROM libraries_by_id WHERE library_id = ?`, lib.LibraryID).Scan(&orgID); !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("resume left libraries_by_id: err=%v", err)
	}
	if marker := nonfencingReadMarker(t, db, lib); !marker.Present || !marker.DeletedAt.Equal(lib.DeletedAt) || marker.PurgeRequestedAt.IsZero() {
		t.Fatalf("resume marker = %+v", marker)
	}
}

// G8: restore and re-trash both fail their completion, so the marker still
// names the old generation D1 while the canonical row is at D2. The repeated
// delete repairs every derived row to D2.
func TestNonfencingG8StaleMarkerRepairedToCurrentGeneration(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedTrashedLibrary(t, db)
	restore := failLibraryLifecycleCompletions(t)
	if err := nonfencingRestore(db, lib); err == nil {
		t.Fatal("setup: restore completion should fail")
	}
	if err := softDeleteLibrary(db, lib.OrgID, lib.OwnerID, lib.OwnerID, lib.LibraryID); err == nil {
		t.Fatal("setup: soft delete completion should fail")
	}
	restore()
	_, d2 := nonfencingCanonical(t, db, lib)
	if marker := nonfencingReadMarker(t, db, lib); !marker.DeletedAt.Equal(lib.DeletedAt) {
		t.Fatalf("setup: marker = %+v, want the stale generation %s", marker, lib.DeletedAt)
	}

	repairTrashedLibraryOnRepeatedDelete(db, lib.OrgID, lib.LibraryID)

	assertNonfencingTrashedDerivedState(t, db, lib, d2, "NONFENCING RED: repair after a stale marker")
}

// G9: a GC soft delete (user/org cascade) whose completion fails is completed
// by the cascade's retry: marker and read model, not only the marker.
func TestNonfencingG9GCSoftDeleteRetryCompletesDerivedState(t *testing.T) {
	db := restoreGuardDBForTest(t)
	lib := nonfencingSeedActiveLibrary(t, db)
	store := gcpkg.NewCassandraStore(db)
	orgUUID, owner := uuid.MustParse(lib.OrgID), uuid.MustParse(lib.OwnerID)
	restore := failLibraryLifecycleCompletions(t)
	if err := store.SoftDeleteLibrary(orgUUID, lib.uuid(t), owner); err == nil {
		t.Fatal("setup: GC soft delete completion should fail")
	}
	restore()
	present, d := nonfencingCanonical(t, db, lib)
	if !present || d.IsZero() {
		t.Fatalf("setup: canonical GC soft delete did not commit: present=%v deleted_at=%v", present, d)
	}
	if err := store.SoftDeleteLibrary(orgUUID, lib.uuid(t), owner); err != nil {
		t.Fatalf("GC soft delete retry: %v", err)
	}
	assertNonfencingTrashedDerivedState(t, db, lib, d, "NONFENCING RED: GC soft delete retry")
}
