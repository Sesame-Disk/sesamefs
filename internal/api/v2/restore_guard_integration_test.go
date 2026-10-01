//go:build integration

package v2

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/config"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

var (
	restoreGuardDBOnce sync.Once
	restoreGuardDB     *dbpkg.DB
	restoreGuardDBErr  error
)

func restoreGuardDBForTest(t *testing.T) *dbpkg.DB {
	t.Helper()
	restoreGuardDBOnce.Do(func() {
		cfg := config.DatabaseConfig{
			Hosts:       restoreGuardHosts(restoreGuardEnv("CASSANDRA_HOSTS", "cassandra:9042")),
			Keyspace:    restoreGuardEnv("CASSANDRA_KEYSPACE", "sesamefs"),
			Consistency: restoreGuardEnv("CASSANDRA_CONSISTENCY", "LOCAL_QUORUM"),
			LocalDC:     restoreGuardEnv("CASSANDRA_LOCAL_DC", "datacenter1"),
			Username:    os.Getenv("CASSANDRA_USERNAME"),
			Password:    os.Getenv("CASSANDRA_PASSWORD"),
		}
		restoreGuardDB, restoreGuardDBErr = dbpkg.New(cfg)
	})
	if restoreGuardDBErr != nil {
		t.Fatalf("connect Cassandra for restore-guard test: %v", restoreGuardDBErr)
	}
	return restoreGuardDB
}

func restoreGuardEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func restoreGuardHosts(value string) []string {
	parts := strings.Split(value, ",")
	hosts := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			hosts = append(hosts, p)
		}
	}
	if len(hosts) == 0 {
		return []string{"cassandra:9042"}
	}
	return hosts
}

// A stale GC hard-delete lock can be stolen by restore's stale-aware lease
// acquisition. Because a Cassandra UPDATE is an upsert, restore must NOT be able
// to recreate the canonical `libraries` row over content a crashed worker already
// began purging. The only safe state to restore from is the original soft-deleted
// canonical row; if it is gone, restore must reject and leave `libraries` absent.
func TestRestoreDeletedLibrary_RejectsWhenCanonicalRowAbsent(t *testing.T) {
	db := restoreGuardDBForTest(t)
	session := db.Session()
	orgID, libraryID, ownerID := uuid.New(), uuid.New(), uuid.New()

	// Purge residue: a stale GC lock (heartbeat 2h old, so the lease is stealable),
	// but NO canonical `libraries` row.
	staleAt := time.Now().UTC().Add(-2 * time.Hour)
	if err := session.Query(`
		INSERT INTO gc_library_hard_delete_locks (library_id, started_at, heartbeat, lease_token)
		VALUES (?, ?, ?, ?)`,
		libraryID.String(), staleAt, staleAt, uuid.New().String()).Exec(); err != nil {
		t.Fatalf("seed stale lock: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Query(`DELETE FROM gc_library_hard_delete_locks WHERE library_id = ?`, libraryID.String()).Exec()
		_ = session.Query(`DELETE FROM libraries WHERE org_id = ? AND library_id = ?`, orgID.String(), libraryID.String()).Exec()
	})

	if err := restoreDeletedLibrary(db, orgID.String(), ownerID.String(), libraryID.String()); err == nil {
		t.Fatal("restore must reject when the canonical libraries row is absent")
	}

	var got string
	scanErr := session.Query(`SELECT library_id FROM libraries WHERE org_id = ? AND library_id = ?`,
		orgID.String(), libraryID.String()).Scan(&got)
	if !errors.Is(scanErr, gocql.ErrNotFound) {
		t.Fatalf("canonical libraries row must remain absent after a rejected restore; scanErr=%v got=%q", scanErr, got)
	}
}

// A present-but-active canonical row (deleted_at == null) is not in trash: restore
// must reject rather than run its clearing batch.
func TestRestoreDeletedLibrary_RejectsWhenCanonicalRowActive(t *testing.T) {
	db := restoreGuardDBForTest(t)
	session := db.Session()
	orgID, libraryID, ownerID := uuid.New(), uuid.New(), uuid.New()

	if err := session.Query(`
		INSERT INTO libraries (org_id, library_id, owner_id, name, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		orgID.String(), libraryID.String(), ownerID.String(), "restore-guard-active",
		time.Now().UTC(), time.Now().UTC()).Exec(); err != nil {
		t.Fatalf("seed active library: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Query(`DELETE FROM libraries WHERE org_id = ? AND library_id = ?`, orgID.String(), libraryID.String()).Exec()
	})

	if err := restoreDeletedLibrary(db, orgID.String(), ownerID.String(), libraryID.String()); err == nil {
		t.Fatal("restore must reject a library that is not in trash (deleted_at is null)")
	}

	var deletedAt time.Time
	if err := session.Query(`SELECT deleted_at FROM libraries WHERE org_id = ? AND library_id = ?`,
		orgID.String(), libraryID.String()).Scan(&deletedAt); err != nil {
		t.Fatalf("read canonical library after rejected restore: %v", err)
	}
	if !deletedAt.IsZero() {
		t.Fatal("active library must stay active (deleted_at null) after a rejected restore")
	}
}

// seedTrashedLibraryForRestoreGuard writes a soft-deleted canonical row plus its
// deleted_libraries marker, both at generation deletedAt, the way softDeleteLibrary does.
//
// The seed is written USING TIMESTAMP at the trash time, not "now": the generation
// CAS writes carry Paxos (server-clock) timestamps, and a client-clock seed written
// milliseconds earlier on a host whose clock runs ahead of Cassandra's (Docker
// Desktop/WSL drifts by hundreds of ms) would otherwise shadow them. Real trash
// generations are always far older than the CAS that settles them.
func seedTrashedLibraryForRestoreGuard(t *testing.T, session *gocql.Session, orgID, libraryID, ownerID uuid.UUID, name string, deletedAt time.Time) {
	t.Helper()
	seededAt := deletedAt.UnixMicro()
	if err := session.Query(`
		INSERT INTO libraries (org_id, library_id, owner_id, name, created_at, updated_at, deleted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?) USING TIMESTAMP ?`,
		orgID.String(), libraryID.String(), ownerID.String(), name,
		deletedAt.Add(-4*time.Hour), deletedAt, deletedAt, seededAt).Exec(); err != nil {
		t.Fatalf("seed trashed library: %v", err)
	}
	if err := session.Query(`
		INSERT INTO deleted_libraries (library_id, org_id, deleted_at, storage_class)
		VALUES (?, ?, ?, ?) USING TIMESTAMP ?`,
		libraryID.String(), orgID.String(), deletedAt, "hot", seededAt).Exec(); err != nil {
		t.Fatalf("seed deleted_libraries marker: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Query(`DELETE FROM gc_library_hard_delete_locks WHERE library_id = ?`, libraryID.String()).Exec()
		_ = session.Query(`DELETE FROM deleted_libraries WHERE library_id = ?`, libraryID.String()).Exec()
		_ = session.Query(`DELETE FROM libraries WHERE org_id = ? AND library_id = ?`, orgID.String(), libraryID.String()).Exec()
		_ = session.Query(`DELETE FROM libraries_by_id WHERE library_id = ?`, libraryID.String()).Exec()
	})
}

// Restore passes every check and fences its lease, then pauses past the stale
// threshold. The GC takes the lease over and purges generation D. When restore
// resumes it must lose: its write must not recreate a partial canonical row over
// a library whose purge already won.
func TestRestoreDeletedLibrary_LosesToPurgeAfterFence(t *testing.T) {
	db := restoreGuardDBForTest(t)
	session := db.Session()
	orgID, libraryID, ownerID := uuid.New(), uuid.New(), uuid.New()
	deletedAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Millisecond)
	seedTrashedLibraryForRestoreGuard(t, session, orgID, libraryID, ownerID, "restore-guard-after-fence", deletedAt)

	restoreDeletedLibraryAfterFenceHook = func() {
		// The GC's stale takeover: the restore's lease token no longer owns the lock.
		if err := session.Query(`UPDATE gc_library_hard_delete_locks SET lease_token = ?, heartbeat = ? WHERE library_id = ?`,
			uuid.New().String(), time.Now().UTC(), libraryID.String()).Exec(); err != nil {
			t.Errorf("steal restore lease: %v", err)
		}
		applied, err := gcpkg.NewCassandraStore(db).HardDeleteLibrary(orgID, libraryID, deletedAt)
		if err != nil || !applied {
			t.Errorf("GC purge of generation D: applied=%v err=%v", applied, err)
		}
	}
	t.Cleanup(func() { restoreDeletedLibraryAfterFenceHook = nil })

	if err := restoreDeletedLibrary(db, orgID.String(), ownerID.String(), libraryID.String()); err == nil {
		t.Fatal("restore must fail once the purge of its generation has won")
	}

	var got string
	scanErr := session.Query(`SELECT library_id FROM libraries WHERE org_id = ? AND library_id = ?`,
		orgID.String(), libraryID.String()).Scan(&got)
	if !errors.Is(scanErr, gocql.ErrNotFound) {
		t.Fatalf("canonical libraries row must stay purged after the losing restore; scanErr=%v got=%q", scanErr, got)
	}
}

// A restore that loses to a permanent delete must not strand the purge. Restore
// removes the GC marker before its CAS; if the permanent delete already won (and
// rewrote the marker) while restore was paused, the losing restore must leave a
// marker behind, or nothing would ever reclaim the library's content.
func TestRestoreDeletedLibrary_LosingRestoreKeepsGCMarker(t *testing.T) {
	db := restoreGuardDBForTest(t)
	session := db.Session()
	orgID, libraryID, ownerID := uuid.New(), uuid.New(), uuid.New()
	deletedAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Millisecond)
	seedTrashedLibraryForRestoreGuard(t, session, orgID, libraryID, ownerID, "restore-loses-keeps-marker", deletedAt)

	restoreDeletedLibraryAfterFenceHook = func() {
		if err := hardDeleteLibraryRowsFn(db, orgID.String(), libraryID.String(), "hot", dbpkg.PlainBlockRepresentationID, deletedAt); err != nil {
			t.Errorf("permanent delete wins generation D: %v", err)
		}
	}
	t.Cleanup(func() { restoreDeletedLibraryAfterFenceHook = nil })

	if err := restoreDeletedLibrary(db, orgID.String(), ownerID.String(), libraryID.String()); err == nil {
		t.Fatal("restore must fail once the permanent delete of its generation has won")
	}

	var marker time.Time
	if err := session.Query(`SELECT deleted_at FROM deleted_libraries WHERE library_id = ?`, libraryID.String()).Scan(&marker); err != nil {
		t.Fatalf("losing restore must leave the GC marker for the purged library: %v", err)
	}
	if !marker.Equal(deletedAt) {
		t.Fatalf("GC marker generation = %s, want %s", marker, deletedAt)
	}
}

// The GC side of the same boundary against real Cassandra: once the library was
// restored (deleted_at cleared), a hard delete for generation D is a no-op.
func TestCassandraHardDeleteLibrary_StaleGenerationIsNoOp(t *testing.T) {
	db := restoreGuardDBForTest(t)
	session := db.Session()
	orgID, libraryID, ownerID := uuid.New(), uuid.New(), uuid.New()
	deletedAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Millisecond)
	seedTrashedLibraryForRestoreGuard(t, session, orgID, libraryID, ownerID, "hard-delete-stale-generation", deletedAt)

	if err := restoreDeletedLibrary(db, orgID.String(), ownerID.String(), libraryID.String()); err != nil {
		t.Fatalf("restore trashed library: %v", err)
	}

	applied, err := gcpkg.NewCassandraStore(db).HardDeleteLibrary(orgID, libraryID, deletedAt)
	if err != nil {
		t.Fatalf("HardDeleteLibrary: %v", err)
	}
	if applied {
		t.Fatal("hard delete of a restored library's old generation must not apply")
	}
	var canonicalDeletedAt time.Time
	if err := session.Query(`SELECT deleted_at FROM libraries WHERE org_id = ? AND library_id = ?`,
		orgID.String(), libraryID.String()).Scan(&canonicalDeletedAt); err != nil {
		t.Fatalf("restored library must survive the stale hard delete: %v", err)
	}
	if !canonicalDeletedAt.IsZero() {
		t.Fatalf("restored library must stay active, deleted_at=%s", canonicalDeletedAt)
	}
}

// The API permanent delete is the third writer of the same boundary
// (ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01). The request renews its lease (the
// final fence) and pauses past the stale threshold; a restore takes the lease
// over and restores the library. The resumed delete must not remove the restored
// row nor rewrite the GC marker over it.
func TestPermanentDelete_LosesToRestoreAfterFence(t *testing.T) {
	db := restoreGuardDBForTest(t)
	session := db.Session()
	orgID, libraryID, ownerID := uuid.New(), uuid.New(), uuid.New()
	deletedAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Millisecond)
	seedTrashedLibraryForRestoreGuard(t, session, orgID, libraryID, ownerID, "permanent-delete-stale-generation", deletedAt)

	realRenew := renewLibraryHardDeleteLockLeaseFn
	renewLibraryHardDeleteLockLeaseFn = func(database *dbpkg.DB, libraryUUID, leaseToken uuid.UUID) (bool, error) {
		owned, err := realRenew(database, libraryUUID, leaseToken)
		if err != nil || !owned {
			return owned, err
		}
		// Paused past the stale threshold: age the heartbeat (through the lease's own
		// SERIAL domain) so restore's real stale takeover wins the lease, then the
		// restore wins the generation while this request sleeps.
		staleAt := time.Now().UTC().Add(-2 * time.Hour)
		if _, err := session.Query(`UPDATE gc_library_hard_delete_locks SET heartbeat = ? WHERE library_id = ? IF lease_token = ?`,
			staleAt, libraryUUID.String(), leaseToken.String()).SerialConsistency(gocql.Serial).MapScanCAS(map[string]interface{}{}); err != nil {
			t.Errorf("age permanent-delete lease: %v", err)
		}
		if err := restoreDeletedLibrary(database, orgID.String(), ownerID.String(), libraryID.String()); err != nil {
			t.Errorf("restore while the permanent delete is paused: %v", err)
		}
		return true, nil
	}
	t.Cleanup(func() { renewLibraryHardDeleteLockLeaseFn = realRenew })

	_, err := permanentlyDeleteTrashedLibraryCandidate(db, trashLibraryCandidate{
		OrgID: orgID.String(), LibraryID: libraryID.String(), StorageClass: "hot", DeletedAt: deletedAt,
	}, "permanent_delete", "PermanentDeleteRepo", false)
	if !errors.Is(err, errPermanentDeleteCandidateStale) {
		t.Fatalf("resumed permanent delete of a restored generation: err=%v, want errPermanentDeleteCandidateStale", err)
	}
	var canonicalDeletedAt time.Time
	if err := session.Query(`SELECT deleted_at FROM libraries WHERE org_id = ? AND library_id = ?`,
		orgID.String(), libraryID.String()).Scan(&canonicalDeletedAt); err != nil {
		t.Fatalf("restored library must survive the stale permanent delete: %v", err)
	}
	var marker time.Time
	if err := session.Query(`SELECT deleted_at FROM deleted_libraries WHERE library_id = ?`, libraryID.String()).Scan(&marker); !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("stale permanent delete must not rewrite the GC marker over a restored library; err=%v marker=%s", err, marker)
	}
}

// While a fresh permanent-delete lease is actively owned, restore must reject
// instead of clearing deleted_at and resurrecting a library whose hard delete is
// already in progress.
func TestRestoreDeletedLibrary_RejectsWhileHardDeleteLeaseOwned(t *testing.T) {
	db := restoreGuardDBForTest(t)
	session := db.Session()
	orgID, libraryID, ownerID := uuid.New(), uuid.New(), uuid.New()
	deletedAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Millisecond)
	now := time.Now().UTC().Truncate(time.Millisecond)
	leaseToken := uuid.New()

	if err := session.Query(`
		INSERT INTO libraries (org_id, library_id, owner_id, name, created_at, updated_at, deleted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		orgID.String(), libraryID.String(), ownerID.String(), "restore-guard-locked",
		now.Add(-4*time.Hour), now, deletedAt).Exec(); err != nil {
		t.Fatalf("seed trashed library: %v", err)
	}
	if err := session.Query(`
		INSERT INTO deleted_libraries (library_id, org_id, deleted_at, storage_class)
		VALUES (?, ?, ?, ?)`,
		libraryID.String(), orgID.String(), deletedAt, "hot").Exec(); err != nil {
		t.Fatalf("seed deleted_libraries marker: %v", err)
	}
	acquired, err := gcpkg.AcquireLibraryHardDeleteLockLease(session, libraryID, leaseToken)
	if err != nil {
		t.Fatalf("AcquireLibraryHardDeleteLockLease: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire fresh library hard-delete lock")
	}
	t.Cleanup(func() {
		_ = gcpkg.ReleaseLibraryHardDeleteLockLease(session, libraryID, leaseToken)
		_ = session.Query(`DELETE FROM deleted_libraries WHERE library_id = ?`, libraryID.String()).Exec()
		_ = session.Query(`DELETE FROM libraries WHERE org_id = ? AND library_id = ?`, orgID.String(), libraryID.String()).Exec()
	})

	if err := restoreDeletedLibrary(db, orgID.String(), ownerID.String(), libraryID.String()); err == nil {
		t.Fatal("restore must reject while the library hard-delete lease is actively owned")
	}

	var canonicalDeletedAt time.Time
	if err := session.Query(`SELECT deleted_at FROM libraries WHERE org_id = ? AND library_id = ?`,
		orgID.String(), libraryID.String()).Scan(&canonicalDeletedAt); err != nil {
		t.Fatalf("read trashed library after rejected restore: %v", err)
	}
	if !canonicalDeletedAt.Equal(deletedAt) {
		t.Fatalf("deleted_at = %s, want %s after rejected restore", canonicalDeletedAt, deletedAt)
	}
}
