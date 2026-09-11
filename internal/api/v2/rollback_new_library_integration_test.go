//go:build integration

package v2

import (
	"errors"
	"testing"
	"time"

	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

// rollbackNewLibrary's authority step (H1 review round 4): a creation
// handler's own failure — a pre-CAS batch error, a definitive CAS rejection —
// is not authority to destroy a library whose HEAD another initializer has
// already published. The library_id is discoverable as soon as the creation
// batch is durable (owner list, admin projections), so GET /commit/HEAD from
// a client that just listed it can publish before the creator's own
// InitializeLibraryFS finishes failing. Pinned against a real Cassandra
// because the guard is a conditional DELETE in the HEAD Paxos domain. Run:
//
//	go test -tags integration ./internal/api/v2/ -run RollbackNewLibrary

func seedUnpublishedLibraryRows(t *testing.T, session *gocql.Session, orgID, ownerID, repoID, name string) dbpkg.AdminLibraryProjectionRow {
	t.Helper()
	now := time.Now()
	blockRepresentationID := dbpkg.NewLibraryBlockRepresentationID(repoID, false)
	if err := session.Query(`
		INSERT INTO libraries (org_id, library_id, owner_id, name, encrypted, block_representation_id, storage_class, size_bytes, file_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, orgID, repoID, ownerID, name, false, blockRepresentationID, "default", int64(0), int64(0), now, now).Exec(); err != nil {
		t.Fatalf("seed libraries row: %v", err)
	}
	if err := session.Query(`
		INSERT INTO libraries_by_id (library_id, org_id, owner_id, name, encrypted, block_representation_id)
		VALUES (?, ?, ?, ?, ?, ?)
	`, repoID, orgID, ownerID, name, false, blockRepresentationID).Exec(); err != nil {
		t.Fatalf("seed libraries_by_id row: %v", err)
	}
	row, err := dbpkg.ReadAdminLibraryProjectionRow(session, orgID, repoID)
	if err != nil {
		t.Fatalf("read projection row: %v", err)
	}
	// Every seeded library is torn down regardless of how the test ends:
	// canonical + lookup rows, the initializer's commit/root, and the derived
	// projections InitializeLibraryFS refreshes (libraries_by_owner,
	// libraries_by_org_updated, admin buckets) via the same delete queries the
	// production rollback uses. Idempotent, so a test that already rolled the
	// library back is a no-op here.
	t.Cleanup(func() {
		batch := session.Batch(gocql.LoggedBatch)
		dbpkg.AddDeleteAdminLibraryReadModelQuery(batch, row)
		batch.Query(`DELETE FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, repoID)
		batch.Query(`DELETE FROM libraries_by_id WHERE library_id = ?`, repoID)
		batch.Query(`DELETE FROM commits WHERE library_id = ?`, repoID)
		batch.Query(`DELETE FROM fs_objects WHERE library_id = ?`, repoID)
		if err := batch.Exec(); err != nil {
			t.Errorf("cleanup seeded library %s: %v", repoID, err)
		}
	})
	return row
}

func readHeadForRollbackTest(t *testing.T, session *gocql.Session, orgID, repoID string) (string, bool) {
	t.Helper()
	var head string
	err := session.Query(`SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, repoID).Consistency(gocql.Serial).Scan(&head)
	if errors.Is(err, gocql.ErrNotFound) {
		return "", false
	}
	if err != nil {
		t.Fatalf("read head: %v", err)
	}
	return head, true
}

func countRowsForRollbackTest(t *testing.T, session *gocql.Session, table, repoID string) int {
	t.Helper()
	var n int
	if err := session.Query(`SELECT count(*) FROM `+table+` WHERE library_id = ?`, repoID).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// TestRollbackNewLibraryRefusesWhenAnotherInitializerPublishedHead is the
// race itself, with the two attempts sequenced: A's rows are durable, B
// (the production initializer, as GET /commit/HEAD would run it) publishes
// C1, then A — whose own initialization "failed" — asks for a rollback. The
// rollback must be refused and B's library, HEAD, commit and root must
// survive untouched.
func TestRollbackNewLibraryRefusesWhenAnotherInitializerPublishedHead(t *testing.T) {
	database := restoreGuardDBForTest(t)
	session := database.Session()
	orgID, ownerID, repoID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	row := seedUnpublishedLibraryRows(t, session, orgID, ownerID, repoID, "inttest-rollback-refused")

	// B publishes through the production path.
	if err := NewFSHelper(database).InitializeLibraryFS(orgID, repoID, ownerID, "inttest-rollback-refused"); err != nil {
		t.Fatalf("B InitializeLibraryFS: %v", err)
	}
	c1, ok := readHeadForRollbackTest(t, session, orgID, repoID)
	if !ok || c1 == "" {
		t.Fatalf("B must have published a HEAD, got head=%q present=%v", c1, ok)
	}
	commitsBefore := countRowsForRollbackTest(t, session, "commits", repoID)
	fsBefore := countRowsForRollbackTest(t, session, "fs_objects", repoID)
	if commitsBefore != 1 || fsBefore != 1 {
		t.Fatalf("precondition: commits=%d fs_objects=%d, want 1/1", commitsBefore, fsBefore)
	}

	// A's rollback after its own (unrelated) failure.
	err := rollbackNewLibrary(database, row)
	if !errors.Is(err, ErrLibraryRollbackRefusedHeadPublished) {
		t.Fatalf("rollback err = %v, want ErrLibraryRollbackRefusedHeadPublished", err)
	}

	head, ok := readHeadForRollbackTest(t, session, orgID, repoID)
	if !ok || head != c1 {
		t.Fatalf("RED: A's refused rollback still destroyed B's library/HEAD: present=%v head=%q want %q", ok, head, c1)
	}
	if got := countRowsForRollbackTest(t, session, "commits", repoID); got != commitsBefore {
		t.Fatalf("RED: B's commit rows changed by A's refused rollback: %d -> %d", commitsBefore, got)
	}
	if got := countRowsForRollbackTest(t, session, "fs_objects", repoID); got != fsBefore {
		t.Fatalf("RED: B's fs_objects changed by A's refused rollback: %d -> %d", fsBefore, got)
	}
	var byID string
	if err := session.Query(`SELECT org_id FROM libraries_by_id WHERE library_id = ?`, repoID).Scan(&byID); err != nil {
		t.Fatalf("RED: libraries_by_id for B's library is gone after A's refused rollback: %v", err)
	}
}

// TestRollbackNewLibraryDeletesUnpublishedLibrary: no HEAD was ever
// published, so the rollback takes authority and removes the library and the
// caller's partial initial state (a commit and root written before its CAS).
func TestRollbackNewLibraryDeletesUnpublishedLibrary(t *testing.T) {
	database := restoreGuardDBForTest(t)
	session := database.Session()
	orgID, ownerID, repoID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	row := seedUnpublishedLibraryRows(t, session, orgID, ownerID, repoID, "inttest-rollback-applies")
	if err := session.Query(`INSERT INTO commits (library_id, commit_id, root_fs_id, creator_id, description, created_at) VALUES (?, ?, ?, ?, ?, ?)`, repoID, "c-partial", "root", ownerID, "Initial commit", time.Now()).Exec(); err != nil {
		t.Fatalf("seed partial commit: %v", err)
	}
	if err := session.Query(`INSERT INTO fs_objects (library_id, fs_id, obj_type, obj_name, mtime) VALUES (?, ?, ?, ?, ?)`, repoID, "root", "dir", "", time.Now().Unix()).Exec(); err != nil {
		t.Fatalf("seed partial root: %v", err)
	}

	if err := rollbackNewLibrary(database, row); err != nil {
		t.Fatalf("rollback of an unpublished library must apply, got %v", err)
	}
	if _, ok := readHeadForRollbackTest(t, session, orgID, repoID); ok {
		t.Fatal("libraries row must be gone")
	}
	if got := countRowsForRollbackTest(t, session, "commits", repoID); got != 0 {
		t.Fatalf("commits left = %d, want 0", got)
	}
	if got := countRowsForRollbackTest(t, session, "fs_objects", repoID); got != 0 {
		t.Fatalf("fs_objects left = %d, want 0", got)
	}
	var byID string
	if err := session.Query(`SELECT org_id FROM libraries_by_id WHERE library_id = ?`, repoID).Scan(&byID); !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("libraries_by_id must be gone, got err=%v", err)
	}
}

// TestRollbackNewLibraryOnMissingCanonicalRowStillClearsDerivedRows pins,
// against a real Cassandra, how DELETE ... IF head_commit_id = null behaves on
// a partition that no longer exists: whatever [applied] it reports, the
// rollback must treat "no row" as "nothing to protect" and still remove the
// caller's derived rows.
func TestRollbackNewLibraryOnMissingCanonicalRowStillClearsDerivedRows(t *testing.T) {
	database := restoreGuardDBForTest(t)
	session := database.Session()
	orgID, ownerID, repoID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	row := seedUnpublishedLibraryRows(t, session, orgID, ownerID, repoID, "inttest-rollback-missing")
	if err := session.Query(`DELETE FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, repoID).Exec(); err != nil {
		t.Fatalf("remove canonical row: %v", err)
	}
	if err := rollbackNewLibrary(database, row); err != nil {
		t.Fatalf("rollback with a missing canonical row must still clear derived rows, got %v", err)
	}
	var byID string
	if err := session.Query(`SELECT org_id FROM libraries_by_id WHERE library_id = ?`, repoID).Scan(&byID); !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("libraries_by_id must be gone, got err=%v", err)
	}
}
