//go:build integration

package v2

import (
	"errors"
	"fmt"
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

type trashCompletionFixture struct {
	db              *dbpkg.DB
	org, lib, owner uuid.UUID
}

func newTrashCompletionFixture(t *testing.T) trashCompletionFixture {
	t.Helper()
	f := trashCompletionFixture{restoreGuardDBForTest(t), uuid.New(), uuid.New(), uuid.New()}
	seedActiveLibraryForRestoreGuard(t, f.db.Session(), f.org, f.lib, f.owner, "trash-completion-regression")
	if err := f.db.Session().Query(`INSERT INTO users (org_id,user_id,role) VALUES (?,?,?)`, f.org.String(), f.owner.String(), "user").Exec(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		softDeleteLibraryAfterCASHook = nil
		restoreDeletedLibraryAfterCASHook = nil
		hardDeleteLibraryAfterCASHook = nil
		gcpkg.LibraryTrashCompletionAfterReadHook = nil
		gcpkg.LibraryTrashCompletionBeforeMarkerDeleteHook = nil
		s := f.db.Session()
		_ = s.Query(`DELETE FROM users WHERE org_id=? AND user_id=?`, f.org.String(), f.owner.String()).Exec()
		_ = s.Query(`DELETE FROM libraries_by_owner WHERE org_id=? AND owner_id=? AND library_id=?`, f.org.String(), f.owner.String(), f.lib.String()).Exec()
		_ = s.Query(`DELETE FROM libraries_by_org_updated WHERE org_id=? AND library_id=?`, f.org.String(), f.lib.String()).Exec()
		_ = s.Query(`DELETE FROM libraries_deleted_by_org WHERE org_id=?`, f.org.String()).Exec()
		_ = s.Query(`DELETE FROM libraries_admin_global_by_updated WHERE bucket_day=? AND org_id=? AND library_id=?`, dbpkg.AdminLibraryBucketDay(time.Now().UTC().Add(-4*time.Hour)), f.org.String(), f.lib.String()).Exec()
		_ = s.Query(`DELETE FROM libraries_by_id WHERE library_id=?`, f.lib.String()).Exec()
		_ = traffic.DeleteLibraryStorageCounter(f.db, f.org.String(), f.lib.String())
		for _, scope := range []string{traffic.OrganizationStorageScope(f.org.String()), traffic.UserStorageScope(f.org.String(), f.owner.String())} {
			_ = s.Query(`DELETE FROM storage_counters WHERE scope=? AND shard=?`, scope, 0).Exec()
			_ = s.Query(`DELETE FROM gc_storage_counter_reconciliation WHERE scope=?`, scope).Exec()
		}
	})
	return f
}
func (f trashCompletionFixture) trash(t *testing.T) {
	t.Helper()
	if err := softDeleteLibrary(f.db, f.org.String(), f.owner.String(), f.owner.String(), f.lib.String()); err != nil {
		t.Fatal(err)
	}
}
func (f trashCompletionFixture) restore(t *testing.T) {
	t.Helper()
	if err := restoreDeletedLibrary(f.db, f.org.String(), f.owner.String(), f.lib.String()); err != nil {
		t.Fatal(err)
	}
}
func (f trashCompletionFixture) assertGenerationMatchesDerived(t *testing.T) {
	t.Helper()
	canonical, exists := canonicalLibraryGenerationForRestoreGuard(t, f.db.Session(), f.org, f.lib)
	if !exists {
		t.Fatal("canonical must exist")
	}
	var projected time.Time
	if err := f.db.Session().Query(`SELECT deleted_at FROM libraries_by_owner WHERE org_id=? AND owner_id=? AND library_id=?`, f.org.String(), f.owner.String(), f.lib.String()).Scan(&projected); err != nil {
		t.Fatal(err)
	}
	if !canonical.Equal(projected) {
		t.Fatalf("projection=%s canonical=%s", projected, canonical)
	}
	marker, present := markerGenerationForRestoreGuard(t, f.db.Session(), f.lib)
	if canonical.IsZero() {
		if present {
			t.Fatalf("active library has trash marker %s", marker)
		}
	} else if !present || !marker.Equal(canonical) {
		t.Fatalf("marker present=%v generation=%s canonical=%s", present, marker, canonical)
	}
	// Inspect stored keys before invoking the read-side reconciler: it must not
	// conceal a duplicate left by lifecycle completion.
	iter := f.db.Session().Query(`SELECT library_id,deleted_at FROM libraries_deleted_by_org WHERE org_id=?`, f.org.String()).Iter()
	var id string
	var generation time.Time
	rawCount := 0
	for iter.Scan(&id, &generation) {
		if id == f.lib.String() {
			rawCount++
			if !generation.Equal(canonical) {
				t.Fatalf("obsolete raw trash projection: %s canonical=%s", generation, canonical)
			}
		}
	}
	if err := iter.Close(); err != nil {
		t.Fatal(err)
	}
	wantRaw := 0
	if !canonical.IsZero() {
		wantRaw = 1
	}
	if rawCount != wantRaw {
		t.Fatalf("raw trash projection count=%d want %d", rawCount, wantRaw)
	}
	rows, _, err := dbpkg.ReconcileDeletedAdminLibraryRowsByOrg(f.db.Session(), f.org.String())
	if err != nil {
		t.Fatal(err)
	}
	want := 0
	if !canonical.IsZero() {
		want = 1
	}
	if len(rows) != want {
		t.Fatalf("trash projection count=%d want %d", len(rows), want)
	}
}

func TestTrashCompletion_PausedSoftDeleteDoesNotHideRestoredLibrary(t *testing.T) {
	f := newTrashCompletionFixture(t)
	softDeleteLibraryAfterCASHook = func() { softDeleteLibraryAfterCASHook = nil; f.restore(t) }
	f.trash(t)
	f.assertGenerationMatchesDerived(t)
}
func TestTrashCompletion_PausedSoftDeletePreservesNewerMarker(t *testing.T) {
	f := newTrashCompletionFixture(t)
	softDeleteLibraryAfterCASHook = func() {
		softDeleteLibraryAfterCASHook = nil
		f.restore(t)
		time.Sleep(2 * time.Millisecond)
		f.trash(t)
	}
	f.trash(t)
	f.assertGenerationMatchesDerived(t)
}
func TestTrashCompletion_RestoreRemovesPreviousTrashProjection(t *testing.T) {
	f := newTrashCompletionFixture(t)
	f.trash(t)
	restoreDeletedLibraryAfterCASHook = func() { restoreDeletedLibraryAfterCASHook = nil; time.Sleep(2 * time.Millisecond); f.trash(t) }
	f.restore(t)
	f.assertGenerationMatchesDerived(t)
}
func TestTrashCompletion_RestoreRevalidatesAfterPublishing(t *testing.T) {
	f := newTrashCompletionFixture(t)
	f.trash(t)
	gcpkg.LibraryTrashCompletionAfterReadHook = func() { gcpkg.LibraryTrashCompletionAfterReadHook = nil; f.trash(t) }
	f.restore(t)
	f.assertGenerationMatchesDerived(t)
}
func TestTrashCompletion_RestoreRepairsIndexesAfterConcurrentPurge(t *testing.T) {
	f := newTrashCompletionFixture(t)
	f.trash(t)
	gcpkg.LibraryTrashCompletionAfterReadHook = func() {
		gcpkg.LibraryTrashCompletionAfterReadHook = nil
		f.trash(t)
		generation, _ := canonicalLibraryGenerationForRestoreGuard(t, f.db.Session(), f.org, f.lib)
		if err := hardDeleteLibraryRowsFn(f.db, f.org.String(), f.lib.String(), "hot", dbpkg.PlainBlockRepresentationID, generation); err != nil {
			t.Fatal(err)
		}
	}
	f.restore(t)
	var id string
	err := f.db.Session().Query(`SELECT library_id FROM libraries_by_owner WHERE org_id=? AND owner_id=? AND library_id=?`, f.org.String(), f.owner.String(), f.lib.String()).Scan(&id)
	if !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("purged library projection re-created: err=%v id=%s", err, id)
	}
}
func TestTrashCompletion_RestoreOwesStorageEvenAfterD2(t *testing.T) {
	for _, otherBytes := range []int64{0, 100} {
		t.Run(fmt.Sprintf("other_live_bytes_%d", otherBytes), func(t *testing.T) {
			f := newTrashCompletionFixture(t)
			s := f.db.Session()
			if err := s.Query(`UPDATE libraries SET size_bytes=?,file_count=? WHERE org_id=? AND library_id=?`, int64(100), int64(1), f.org.String(), f.lib.String()).Exec(); err != nil {
				t.Fatal(err)
			}
			if err := traffic.IncrementStorageCountersSync(f.db, f.org.String(), f.owner.String(), f.lib.String(), 100, 1); err != nil {
				t.Fatal(err)
			}
			if otherBytes > 0 {
				otherLib := uuid.New()
				seedActiveLibraryForRestoreGuard(t, s, f.org, otherLib, f.owner, "other-live-storage")
				if err := s.Query(`UPDATE libraries SET size_bytes=?,file_count=? WHERE org_id=? AND library_id=?`, otherBytes, int64(1), f.org.String(), otherLib.String()).Exec(); err != nil {
					t.Fatal(err)
				}
				if err := traffic.IncrementStorageCountersSync(f.db, f.org.String(), f.owner.String(), "", otherBytes, 1); err != nil {
					t.Fatal(err)
				}
			}
			f.trash(t)
			restoreDeletedLibraryAfterCASHook = func() { restoreDeletedLibraryAfterCASHook = nil; f.trash(t) }
			f.restore(t)
			for _, scope := range []string{traffic.OrganizationStorageScope(f.org.String()), traffic.UserStorageScope(f.org.String(), f.owner.String())} {
				if used := traffic.ReadStorageUsed(f.db, scope); used != otherBytes {
					t.Fatalf("scope=%s bytes=%d want %d after D1 restore + D2", scope, used, otherBytes)
				}
			}
		})
	}
}

func TestTrashCompletion_SoftDeleteCrashIsReachableByHTTPRetry(t *testing.T) {
	f := newTrashCompletionFixture(t)
	softDeleteLibraryAfterCASHook = func() { softDeleteLibraryAfterCASHook = nil; panic("process stopped after committed CAS") }
	func() {
		defer func() {
			if recover() == nil {
				t.Error("crash seam did not run")
			}
		}()
		f.trash(t)
	}()
	c, w := newDeleteTestContext(http.MethodDelete, "/repos/"+f.lib.String())
	c.Set("org_id", f.org.String())
	c.Set("user_id", f.owner.String())
	c.Params = gin.Params{{Key: "repo_id", Value: f.lib.String()}}
	(&LibraryHandler{db: f.db}).DeleteLibrary(c)
	if w.Code != http.StatusOK {
		t.Fatalf("normal DELETE retry=%d %s", w.Code, w.Body.String())
	}
	f.assertGenerationMatchesDerived(t)
}
func TestTrashCompletion_PermanentDeleteCrashIsReachableByHTTPRetryWithoutGC(t *testing.T) {
	f := newTrashCompletionFixture(t)
	f.trash(t)
	generation, _ := canonicalLibraryGenerationForRestoreGuard(t, f.db.Session(), f.org, f.lib)
	if err := f.db.Session().Query(`INSERT INTO libraries_by_id (library_id,org_id,owner_id) VALUES (?,?,?)`, f.lib.String(), f.org.String(), f.owner.String()).Exec(); err != nil {
		t.Fatal(err)
	}
	hardDeleteLibraryAfterCASHook = func() error { return errors.New("process stopped before completion batch") }
	if err := hardDeleteLibraryRowsFn(f.db, f.org.String(), f.lib.String(), "hot", dbpkg.PlainBlockRepresentationID, generation); err == nil {
		t.Fatal("expected interrupted completion")
	}
	hardDeleteLibraryAfterCASHook = nil
	if _, exists := canonicalLibraryGenerationForRestoreGuard(t, f.db.Session(), f.org, f.lib); exists {
		t.Fatal("CAS did not remove canonical")
	}
	row, _, err := gcpkg.ReadPermanentLibraryDeleteIntent(f.db.Session(), f.lib.String())
	if err != nil || row.OwnerID != f.owner.String() {
		t.Fatalf("durable intent: %v %#v", err, row)
	}
	oldAsync := runAsyncLibraryDeleteSideEffectFn
	runAsyncLibraryDeleteSideEffectFn = func(fn func()) { fn() }
	t.Cleanup(func() { runAsyncLibraryDeleteSideEffectFn = oldAsync })
	h := &DeletedLibraryHandler{db: f.db, permMiddleware: middleware.NewPermissionMiddleware(f.db)}
	for attempt := 0; attempt < 2; attempt++ {
		c, w := newDeleteTestContext(http.MethodDelete, "/repos/deleted/"+f.lib.String())
		c.Set("org_id", f.org.String())
		c.Set("user_id", f.owner.String())
		c.Params = gin.Params{{Key: "repo_id", Value: f.lib.String()}}
		h.PermanentDeleteRepo(c)
		if w.Code != http.StatusOK {
			t.Fatalf("normal permanent DELETE retry=%d %s", w.Code, w.Body.String())
		}
	}
	var id string
	for _, query := range []string{`SELECT library_id FROM libraries_by_id WHERE library_id=?`, `SELECT library_id FROM libraries_by_owner WHERE org_id=? AND owner_id=? AND library_id=?`} {
		args := []interface{}{f.lib.String()}
		if query != `SELECT library_id FROM libraries_by_id WHERE library_id=?` {
			args = []interface{}{f.org.String(), f.owner.String(), f.lib.String()}
		}
		if err := f.db.Session().Query(query, args...).Scan(&id); !errors.Is(err, gocql.ErrNotFound) {
			t.Fatalf("derived row survived retry: err=%v id=%s", err, id)
		}
	}
}
func TestTrashCompletion_MarkerInsertFollowsLWTTombstoneDespiteClientSkew(t *testing.T) {
	f := newTrashCompletionFixture(t)
	f.trash(t)
	f.restore(t)
	generation := time.Now().UTC()
	if applied, _, _, err := gcpkg.SoftDeleteCanonicalLibraryGeneration(f.db.Session(), f.org.String(), f.lib.String(), generation, f.owner.String(), generation); err != nil || !applied {
		t.Fatalf("D2 CAS applied=%v err=%v", applied, err)
	}
	current, _ := canonicalLibraryGenerationForRestoreGuard(t, f.db.Session(), f.org, f.lib)
	// Reproduce the old plain insert with a client clock behind the LWT tombstone.
	if err := f.db.Session().Query(`INSERT INTO deleted_libraries (library_id,org_id,deleted_at) VALUES (?,?,?) USING TIMESTAMP ?`, f.lib.String(), f.org.String(), current, time.Now().Add(-time.Hour).UnixMicro()).Exec(); err != nil {
		t.Fatal(err)
	}
	if marker, present := markerGenerationForRestoreGuard(t, f.db.Session(), f.lib); present {
		t.Fatalf("old plain insert should be shadowed: %s", marker)
	}
	row, err := dbpkg.ReadAdminLibraryProjectionRowSerial(f.db.Session(), f.org.String(), f.lib.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gcpkg.RepairLibraryTrashDerivedState(f.db.Session(), f.org.String(), f.lib.String(), row, dbpkg.PlainBlockRepresentationID); err != nil {
		t.Fatal(err)
	}
	f.assertGenerationMatchesDerived(t)
}

func TestTrashCompletion_ConcurrentRestoresKeepSharedCounters(t *testing.T) {
	f := newTrashCompletionFixture(t)
	s := f.db.Session()
	second := uuid.New()
	seedActiveLibraryForRestoreGuard(t, s, f.org, second, f.owner, "concurrent-storage")
	for _, lib := range []uuid.UUID{f.lib, second} {
		if err := s.Query(`UPDATE libraries SET size_bytes=?,file_count=? WHERE org_id=? AND library_id=?`, int64(100), int64(1), f.org.String(), lib.String()).Exec(); err != nil {
			t.Fatal(err)
		}
		if err := softDeleteLibrary(f.db, f.org.String(), f.owner.String(), f.owner.String(), lib.String()); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, lib := range []uuid.UUID{f.lib, second} {
		wg.Add(1)
		go func(id uuid.UUID) {
			defer wg.Done()
			errs <- restoreDeletedLibrary(f.db, f.org.String(), f.owner.String(), id.String())
		}(lib)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, scope := range []string{traffic.OrganizationStorageScope(f.org.String()), traffic.UserStorageScope(f.org.String(), f.owner.String())} {
		if used := traffic.ReadStorageUsed(f.db, scope); used != 200 {
			t.Fatalf("concurrent restores: scope=%s used=%d want 200", scope, used)
		}
	}
	row, err := dbpkg.ReadAdminLibraryProjectionRow(s, f.org.String(), second.String())
	if err != nil {
		t.Fatal(err)
	}
	batch := s.Batch(gocql.LoggedBatch)
	dbpkg.AddDeleteAdminLibraryReadModelQuery(batch, row)
	if err := batch.Exec(); err != nil {
		t.Fatal(err)
	}
}

func TestTrashCompletion_PermanentDeleteRetryRetainsAuthorization(t *testing.T) {
	f := newTrashCompletionFixture(t)
	f.trash(t)
	generation, _ := canonicalLibraryGenerationForRestoreGuard(t, f.db.Session(), f.org, f.lib)
	hardDeleteLibraryAfterCASHook = func() error { return errors.New("crash before completion") }
	_ = hardDeleteLibraryRowsFn(f.db, f.org.String(), f.lib.String(), "hot", dbpkg.PlainBlockRepresentationID, generation)
	hardDeleteLibraryAfterCASHook = nil
	h := &DeletedLibraryHandler{db: f.db, permMiddleware: middleware.NewPermissionMiddleware(f.db)}
	for _, crossOrg := range []bool{false, true} {
		callerOrg, user := f.org, uuid.New()
		expected := http.StatusForbidden
		if crossOrg {
			callerOrg = uuid.New()
			expected = http.StatusNotFound
		}
		if err := f.db.Session().Query(`INSERT INTO users (org_id,user_id,role) VALUES (?,?,?)`, callerOrg.String(), user.String(), "user").Exec(); err != nil {
			t.Fatal(err)
		}
		c, w := newDeleteTestContext(http.MethodDelete, "/repos/deleted/"+f.lib.String())
		c.Set("org_id", callerOrg.String())
		c.Set("user_id", user.String())
		c.Params = gin.Params{{Key: "repo_id", Value: f.lib.String()}}
		h.PermanentDeleteRepo(c)
		_ = f.db.Session().Query(`DELETE FROM users WHERE org_id=? AND user_id=?`, callerOrg.String(), user.String()).Exec()
		if w.Code != expected {
			t.Fatalf("crossOrg=%v unauthorized retry=%d want %d: %s", crossOrg, w.Code, expected, w.Body.String())
		}
	}
}

func TestTrashCompletion_RestoreCrashIsReachableByHTTPRetryWithoutGC(t *testing.T) {
	f := newTrashCompletionFixture(t)
	if err := f.db.Session().Query(`UPDATE libraries SET size_bytes=?,file_count=? WHERE org_id=? AND library_id=?`, int64(100), int64(1), f.org.String(), f.lib.String()).Exec(); err != nil {
		t.Fatal(err)
	}
	if err := traffic.IncrementStorageCountersSync(f.db, f.org.String(), f.owner.String(), f.lib.String(), 100, 1); err != nil {
		t.Fatal(err)
	}
	f.trash(t)
	restoreDeletedLibraryAfterCASHook = func() { panic("crash after restore CAS") }
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected injected crash")
			}
		}()
		f.restore(t)
	}()
	restoreDeletedLibraryAfterCASHook = nil
	h := &DeletedLibraryHandler{db: f.db, permMiddleware: middleware.NewPermissionMiddleware(f.db)}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "repo_id", Value: f.lib.String()}}
	c.Set("org_id", f.org.String())
	c.Set("user_id", f.owner.String())
	h.RestoreDeletedRepo(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("restore retry: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	f.assertGenerationMatchesDerived(t)
	for _, scope := range []string{traffic.OrganizationStorageScope(f.org.String()), traffic.UserStorageScope(f.org.String(), f.owner.String())} {
		if used := traffic.ReadStorageUsed(f.db, scope); used != 100 {
			t.Fatalf("restore retry storage: %s=%d", scope, used)
		}
	}
}

func TestTrashCompletion_PermanentDeleteCleansTagCounters(t *testing.T) {
	f := newTrashCompletionFixture(t)
	session := f.db.Session()
	if err := session.Query(`INSERT INTO repo_tags (repo_id,tag_id,name) VALUES (?,?,?)`, f.lib.String(), 1, "cleanup-regression").Exec(); err != nil {
		t.Fatal(err)
	}
	if err := session.Query(`UPDATE repo_tag_file_counts SET file_count=file_count+1 WHERE repo_id=? AND tag_id=?`, f.lib.String(), 1).Exec(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Query(`DELETE FROM repo_tags WHERE repo_id=?`, f.lib.String()).Exec()
		_ = session.Query(`DELETE FROM repo_tag_file_counts WHERE repo_id=?`, f.lib.String()).Exec()
	})
	for attempt := 0; attempt < 2; attempt++ {
		if err := CleanupAllLibraryTags(f.db, f.lib.String()); err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	if err := session.Query(`SELECT file_count FROM repo_tag_file_counts WHERE repo_id=? AND tag_id=?`, f.lib.String(), 1).Scan(&count); !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("tag counter survived: count=%d err=%v", count, err)
	}
	var name string
	if err := session.Query(`SELECT name FROM repo_tags WHERE repo_id=? AND tag_id=?`, f.lib.String(), 1).Scan(&name); !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("tag metadata survived: name=%s err=%v", name, err)
	}
}

func TestTrashCompletion_PermanentDeleteAfterInterruptedTrashRepairsStorage(t *testing.T) {
	f := newTrashCompletionFixture(t)
	session := f.db.Session()
	if err := session.Query(`UPDATE libraries SET size_bytes=?,file_count=? WHERE org_id=? AND library_id=?`, int64(100), int64(1), f.org.String(), f.lib.String()).Exec(); err != nil {
		t.Fatal(err)
	}
	if err := traffic.IncrementStorageCountersSync(f.db, f.org.String(), f.owner.String(), f.lib.String(), 100, 1); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if applied, _, _, err := gcpkg.SoftDeleteCanonicalLibraryGeneration(session, f.org.String(), f.lib.String(), now, f.owner.String(), now); err != nil || !applied {
		t.Fatalf("interrupted trash CAS: applied=%v err=%v", applied, err)
	}
	oldAsync := runAsyncLibraryDeleteSideEffectFn
	runAsyncLibraryDeleteSideEffectFn = func(fn func()) { fn() }
	t.Cleanup(func() { runAsyncLibraryDeleteSideEffectFn = oldAsync })
	h := &DeletedLibraryHandler{db: f.db, permMiddleware: middleware.NewPermissionMiddleware(f.db)}
	c, w := newDeleteTestContext(http.MethodDelete, "/repos/deleted/"+f.lib.String())
	c.Set("org_id", f.org.String())
	c.Set("user_id", f.owner.String())
	c.Params = gin.Params{{Key: "repo_id", Value: f.lib.String()}}
	h.PermanentDeleteRepo(c)
	if w.Code != http.StatusOK {
		t.Fatalf("permanent delete: %d %s", w.Code, w.Body.String())
	}
	for _, scope := range []string{traffic.OrganizationStorageScope(f.org.String()), traffic.UserStorageScope(f.org.String(), f.owner.String())} {
		if used := traffic.ReadStorageUsed(f.db, scope); used != 0 {
			t.Fatalf("purge storage: %s=%d", scope, used)
		}
	}
}

func TestTrashCompletion_StaleRestoreCannotDeleteNewerPurgeIntent(t *testing.T) {
	f := newTrashCompletionFixture(t)
	f.trash(t)
	gcpkg.LibraryTrashCompletionBeforeMarkerDeleteHook = func() {
		gcpkg.LibraryTrashCompletionBeforeMarkerDeleteHook = nil
		f.trash(t)
		generation, _ := canonicalLibraryGenerationForRestoreGuard(t, f.db.Session(), f.org, f.lib)
		hardDeleteLibraryAfterCASHook = func() error { return errors.New("D2 purge stopped before its completion") }
		if err := hardDeleteLibraryRowsFn(f.db, f.org.String(), f.lib.String(), "hot", dbpkg.PlainBlockRepresentationID, generation); err == nil {
			t.Fatal("expected interrupted D2 purge")
		}
		hardDeleteLibraryAfterCASHook = nil
	}
	f.restore(t)
	intent, _, err := gcpkg.ReadPermanentLibraryDeleteIntent(f.db.Session(), f.lib.String())
	if err != nil || intent.OwnerID != f.owner.String() {
		t.Fatalf("old restore destroyed D2 purge retry: intent=%#v err=%v", intent, err)
	}
	oldAsync := runAsyncLibraryDeleteSideEffectFn
	runAsyncLibraryDeleteSideEffectFn = func(fn func()) { fn() }
	t.Cleanup(func() { runAsyncLibraryDeleteSideEffectFn = oldAsync })
	h := &DeletedLibraryHandler{db: f.db, permMiddleware: middleware.NewPermissionMiddleware(f.db)}
	c, w := newDeleteTestContext(http.MethodDelete, "/repos/deleted/"+f.lib.String())
	c.Set("org_id", f.org.String())
	c.Set("user_id", f.owner.String())
	c.Params = gin.Params{{Key: "repo_id", Value: f.lib.String()}}
	h.PermanentDeleteRepo(c)
	if w.Code != http.StatusOK {
		t.Fatalf("D2 purge retry: %d %s", w.Code, w.Body.String())
	}
}

func TestTrashCompletion_PausedTrashPublicationPreservesNewerPurgeIntent(t *testing.T) {
	f := newTrashCompletionFixture(t)
	gcpkg.LibraryTrashCompletionAfterReadHook = func() {
		gcpkg.LibraryTrashCompletionAfterReadHook = nil
		f.restore(t)
		f.trash(t)
		generation, _ := canonicalLibraryGenerationForRestoreGuard(t, f.db.Session(), f.org, f.lib)
		hardDeleteLibraryAfterCASHook = func() error { return errors.New("D2 purge stopped before its completion") }
		if err := hardDeleteLibraryRowsFn(f.db, f.org.String(), f.lib.String(), "hot", dbpkg.PlainBlockRepresentationID, generation); err == nil {
			t.Fatal("expected interrupted D2 purge")
		}
		hardDeleteLibraryAfterCASHook = nil
	}
	f.trash(t)
	intent, _, err := gcpkg.ReadPermanentLibraryDeleteIntent(f.db.Session(), f.lib.String())
	if err != nil || intent.OwnerID != f.owner.String() {
		t.Fatalf("old trash publication destroyed D2 purge retry: intent=%#v err=%v", intent, err)
	}
}
