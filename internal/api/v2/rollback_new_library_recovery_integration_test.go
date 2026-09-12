//go:build integration

package v2

import (
	"context"
	"errors"
	"sync"
	"testing"

	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

func seedCrashWindowLibrary(t *testing.T, session *gocql.Session) dbpkg.AdminLibraryProjectionRow {
	t.Helper()
	orgID, ownerID, repoID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	row := seedUnpublishedLibraryRows(t, session, orgID, ownerID, repoID, "inttest-rollback-crash")
	seedUnpublishedLibraryDerivedState(t, session, row)
	seedPartialCommitAndRoot(t, session, ownerID, repoID)
	return row
}

func crashAfterAuthority(t *testing.T) {
	t.Helper()
	libraryRollbackAfterAuthorityFn = func() error {
		return errors.New("injected crash after authority LWT")
	}
}

func TestRollbackNewLibraryCrashBeforeAuthorityReaperRefusesPublishedHead(t *testing.T) {
	restoreLibraryRollbackHooks(t)
	database := rollbackNewLibraryDB(t)
	session := database.Session()
	row := seedCrashWindowLibrary(t, session)

	libraryRollbackAfterMarkerPersistFn = func() error {
		return errors.New("injected crash after marker persist")
	}
	if err := rollbackNewLibrary(database, row); err == nil {
		t.Fatal("injected crash after marker persist must surface")
	}
	libraryRollbackAfterMarkerPersistFn = nil

	if !rollbackMarkerPresent(t, session, row.OrgID, row.LibraryID) {
		t.Fatal("marker must remain durable across the crash before the LWT")
	}
	if _, ok := readHeadForRollbackTest(t, session, row.OrgID, row.LibraryID); !ok {
		t.Fatal("canonical row must still exist before another initializer publishes")
	}

	if err := NewFSHelper(database).InitializeLibraryFS(row.OrgID, row.LibraryID, row.OwnerID, "inttest-rollback-crash"); err != nil {
		t.Fatalf("other initializer InitializeLibraryFS: %v", err)
	}
	head, ok := readHeadForRollbackTest(t, session, row.OrgID, row.LibraryID)
	if !ok || head == "" {
		t.Fatalf("other initializer must publish a HEAD, present=%v head=%q", ok, head)
	}
	commitsBefore := countRowsForRollbackTest(t, session, "commits", row.LibraryID)
	fsBefore := countRowsForRollbackTest(t, session, "fs_objects", row.LibraryID)

	if err := RecoverPendingLibraryRollbacks(context.Background(), session); err != nil {
		t.Fatalf("recovery seam: %v", err)
	}
	gotHead, stillPresent := readHeadForRollbackTest(t, session, row.OrgID, row.LibraryID)
	if !stillPresent || gotHead != head {
		t.Fatalf("marker must not authorize cleanup of a published HEAD: present=%v head=%q want %q", stillPresent, gotHead, head)
	}
	if got := countRowsForRollbackTest(t, session, "commits", row.LibraryID); got != commitsBefore {
		t.Fatalf("reaper deleted commits under a published HEAD: %d -> %d", commitsBefore, got)
	}
	if got := countRowsForRollbackTest(t, session, "fs_objects", row.LibraryID); got != fsBefore {
		t.Fatalf("reaper deleted fs_objects under a published HEAD: %d -> %d", fsBefore, got)
	}
	if !scanExists(t, session, `SELECT org_id FROM libraries_by_id WHERE library_id = ?`, row.LibraryID) {
		t.Fatal("reaper deleted libraries_by_id under a published HEAD")
	}
	if rollbackMarkerPresent(t, session, row.OrgID, row.LibraryID) {
		t.Fatal("refused recovery must settle the marker")
	}
}

func TestRollbackNewLibraryCrashAfterAuthorityRecoveredByReaper(t *testing.T) {
	restoreLibraryRollbackHooks(t)
	database := rollbackNewLibraryDB(t)
	session := database.Session()
	row := seedCrashWindowLibrary(t, session)

	crashAfterAuthority(t)
	if err := rollbackNewLibrary(database, row); err == nil {
		t.Fatal("injected crash after authority must surface")
	}
	libraryRollbackAfterAuthorityFn = nil

	if _, ok := readHeadForRollbackTest(t, session, row.OrgID, row.LibraryID); ok {
		t.Fatal("authority LWT must have deleted the canonical libraries row")
	}
	assertRollbackDerivedState(t, session, row, true, 1, 1)
	if !rollbackMarkerPresent(t, session, row.OrgID, row.LibraryID) {
		t.Fatal("ghost window must leave the pending marker")
	}

	if err := RecoverPendingLibraryRollbacks(context.Background(), session); err != nil {
		t.Fatalf("recovery seam: %v", err)
	}
	assertLibraryFullyRolledBack(t, session, row)
}

func TestRollbackNewLibraryCleanupFailureRetainsMarkerUntilSecondRecovery(t *testing.T) {
	restoreLibraryRollbackHooks(t)
	database := rollbackNewLibraryDB(t)
	session := database.Session()
	row := seedCrashWindowLibrary(t, session)

	crashAfterAuthority(t)
	if err := rollbackNewLibrary(database, row); err == nil {
		t.Fatal("injected crash after authority must surface")
	}
	libraryRollbackAfterAuthorityFn = nil

	calls := 0
	cleanupRolledBackLibraryDerivedStateFn = func(session *gocql.Session, pending dbpkg.LibraryRollbackPending) error {
		calls++
		if calls == 1 {
			return errors.New("injected cleanup failure")
		}
		return cleanupRolledBackLibraryDerivedState(session, pending)
	}

	if err := RecoverPendingLibraryRollbacks(context.Background(), session); err == nil {
		t.Fatal("first recovery must surface the injected cleanup failure")
	}
	assertRollbackDerivedState(t, session, row, true, 1, 1)
	if !rollbackMarkerPresent(t, session, row.OrgID, row.LibraryID) {
		t.Fatal("cleanup failure must retain the marker")
	}

	if err := RecoverPendingLibraryRollbacks(context.Background(), session); err != nil {
		t.Fatalf("second recovery: %v", err)
	}
	assertLibraryFullyRolledBack(t, session, row)
}

func TestRollbackNewLibraryMarkerDeleteFailureIsIdempotentOnRecovery(t *testing.T) {
	restoreLibraryRollbackHooks(t)
	database := rollbackNewLibraryDB(t)
	session := database.Session()
	row := seedCrashWindowLibrary(t, session)

	calls := 0
	deleteLibraryRollbackPendingFn = func(session *gocql.Session, pending dbpkg.LibraryRollbackPending) error {
		calls++
		if calls == 1 {
			return errors.New("injected marker delete failure")
		}
		return dbpkg.DeleteLibraryRollbackPending(session, pending)
	}

	if err := rollbackNewLibrary(database, row); err == nil {
		t.Fatal("marker delete failure after cleanup must surface")
	}
	if _, ok := readHeadForRollbackTest(t, session, row.OrgID, row.LibraryID); ok {
		t.Fatal("canonical row must already be gone")
	}
	assertRollbackDerivedState(t, session, row, false, 0, 0)
	if !rollbackMarkerPresent(t, session, row.OrgID, row.LibraryID) {
		t.Fatal("marker must remain when its delete fails")
	}

	if err := RecoverPendingLibraryRollbacks(context.Background(), session); err != nil {
		t.Fatalf("idempotent recovery: %v", err)
	}
	assertLibraryFullyRolledBack(t, session, row)
}

func TestRollbackNewLibraryDuplicateRecoveryIsIdempotent(t *testing.T) {
	restoreLibraryRollbackHooks(t)
	database := rollbackNewLibraryDB(t)
	session := database.Session()
	row := seedCrashWindowLibrary(t, session)

	crashAfterAuthority(t)
	if err := rollbackNewLibrary(database, row); err == nil {
		t.Fatal("injected crash after authority must surface")
	}
	libraryRollbackAfterAuthorityFn = nil

	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- RecoverPendingLibraryRollbacks(context.Background(), session)
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent recovery: %v", err)
		}
	}
	assertLibraryFullyRolledBack(t, session, row)
}

func TestRollbackNewLibraryRecoveryRefusesWhenHeadExists(t *testing.T) {
	restoreLibraryRollbackHooks(t)
	database := rollbackNewLibraryDB(t)
	session := database.Session()
	row := seedCrashWindowLibrary(t, session)

	pending := dbpkg.LibraryRollbackPendingFromProjection(row)
	if err := dbpkg.InsertLibraryRollbackPending(session, pending); err != nil {
		t.Fatalf("insert marker: %v", err)
	}
	if err := NewFSHelper(database).InitializeLibraryFS(row.OrgID, row.LibraryID, row.OwnerID, "inttest-rollback-crash"); err != nil {
		t.Fatalf("InitializeLibraryFS: %v", err)
	}
	head, ok := readHeadForRollbackTest(t, session, row.OrgID, row.LibraryID)
	if !ok || head == "" {
		t.Fatal("HEAD must exist before recovery")
	}

	if err := RecoverPendingLibraryRollbacks(context.Background(), session); err != nil {
		t.Fatalf("recovery seam: %v", err)
	}
	gotHead, stillPresent := readHeadForRollbackTest(t, session, row.OrgID, row.LibraryID)
	if !stillPresent || gotHead != head {
		t.Fatalf("recovery with HEAD present must not cleanup: present=%v head=%q want %q", stillPresent, gotHead, head)
	}
	if !scanExists(t, session, `SELECT org_id FROM libraries_by_id WHERE library_id = ?`, row.LibraryID) {
		t.Fatal("libraries_by_id must survive recovery when HEAD exists")
	}
	if rollbackMarkerPresent(t, session, row.OrgID, row.LibraryID) {
		t.Fatal("refused recovery must settle the marker")
	}
}

func TestRollbackNewLibraryRecoveryClearsGhostsWhenCanonicalRowAlreadyGone(t *testing.T) {
	restoreLibraryRollbackHooks(t)
	database := rollbackNewLibraryDB(t)
	session := database.Session()
	row := seedCrashWindowLibrary(t, session)

	pending := dbpkg.LibraryRollbackPendingFromProjection(row)
	if err := dbpkg.InsertLibraryRollbackPending(session, pending); err != nil {
		t.Fatalf("insert marker: %v", err)
	}
	if err := session.Query(`DELETE FROM libraries WHERE org_id = ? AND library_id = ?`, row.OrgID, row.LibraryID).Exec(); err != nil {
		t.Fatalf("simulate authority-applied crash: %v", err)
	}
	assertRollbackDerivedState(t, session, row, true, 1, 1)
	if !rollbackMarkerPresent(t, session, row.OrgID, row.LibraryID) {
		t.Fatal("crash state must be discoverable from the marker")
	}

	if err := RecoverPendingLibraryRollbacks(context.Background(), session); err != nil {
		t.Fatalf("recovery seam: %v", err)
	}
	assertLibraryFullyRolledBack(t, session, row)
}
