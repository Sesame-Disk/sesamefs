//go:build integration

package integration

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

func baselineCertifierTestFSID(label string) string {
	sum := sha1.Sum([]byte(label))
	return hex.EncodeToString(sum[:])
}

func seedBaselineCertifierEdgeFixture(t *testing.T, database *dbpkg.DB, orgID, libraryID, head, rootFSID, rootEntries, fileFSID string, claimEmptyFile bool) {
	t.Helper()
	now := time.Now().UTC()
	w2PostHeadRetryEachQuorum(t, "seed baseline-certifier edge root", func() error {
		return database.Session().Query(`INSERT INTO fs_objects (library_id, fs_id, obj_type, dir_entries, mtime) VALUES (?, ?, ?, ?, ?)`,
			libraryID, rootFSID, "dir", rootEntries, now.Unix()).Consistency(gocql.EachQuorum).Exec()
	})
	if _, err := dbpkg.AuthorizeFSObjectProjection(context.Background(), database.Session(), dbpkg.FSObjectProjection{
		LibraryID: libraryID, FSID: rootFSID, ObjectType: "dir", DirectoryEntries: rootEntries,
	}); err != nil {
		t.Fatalf("establish baseline-certifier edge root identity: %v", err)
	}

	w2PostHeadRetryEachQuorum(t, "seed baseline-certifier edge zero file", func() error {
		return database.Session().Query(`
			INSERT INTO fs_objects (library_id, fs_id, obj_type, size_bytes, mtime, block_ids, seafile_block_ids_sha1)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, libraryID, fileFSID, "file", int64(0), now.Unix(), []string{}, []string(nil)).Consistency(gocql.EachQuorum).Exec()
	})
	w2PostHeadRetryEachQuorum(t, "set baseline-certifier edge zero-file lists NULL", func() error {
		return database.Session().Query(`DELETE block_ids, seafile_block_ids_sha1 FROM fs_objects WHERE library_id = ? AND fs_id = ?`,
			libraryID, fileFSID).Consistency(gocql.EachQuorum).Exec()
	})
	row, err := dbpkg.ReadFSObjectIdentitySourceRow(context.Background(), database.Session(), libraryID, fileFSID)
	if err != nil {
		t.Fatalf("read Cassandra zero-file source: %v", err)
	}
	if row["obj_type"] != "file" || row["size_bytes"] != int64(0) ||
		dbpkg.IdentitySourceValuePresent(row, "block_ids") || dbpkg.IdentitySourceValuePresent(row, "seafile_block_ids_sha1") {
		t.Fatalf("zero-file Cassandra source=%#v; want file/size=0/both lists NULL", row)
	}
	if claimEmptyFile {
		_, err := dbpkg.AuthorizeFSObjectProjection(context.Background(), database.Session(), dbpkg.FSObjectProjection{
			LibraryID: libraryID, FSID: fileFSID, ObjectType: "file", SizeBytes: 0,
			FileLayout: dbpkg.FileStorageSHA1Only, LogicalSHA1IDs: []string{},
		})
		if err != nil {
			t.Fatalf("authorize Cassandra zero-file source: %v", err)
		}
	}
}

func TestLibraryBaselineCertifierZeroBlockFileRealCassandra(t *testing.T) {
	database := shareProjectionDBForTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	makeFixture := func(label string, claimEmptyFile bool) (string, string, string, string) {
		t.Helper()
		orgID, libraryID, ownerID := uuid.NewString(), uuid.NewString(), uuid.NewString()
		head := "pc-d1b1-zero-" + uuid.NewString()
		rootFSID := baselineCertifierTestFSID(label + "-root")
		fileFSID := baselineCertifierTestFSID(label + "-file")
		entries, err := json.Marshal([]map[string]interface{}{{"id": fileFSID, "mode": 33188, "mtime": time.Now().Unix(), "name": "empty.txt"}})
		if err != nil {
			t.Fatalf("marshal zero-file directory: %v", err)
		}
		seedLibraryBaselineCertifierLibrary(t, database, orgID, libraryID, ownerID, head, 0)
		seedLibraryBaselineCertifierCommit(t, database, libraryID, head, rootFSID)
		seedBaselineCertifierEdgeFixture(t, database, orgID, libraryID, head, rootFSID, string(entries), fileFSID, claimEmptyFile)
		return orgID, libraryID, head, fileFSID
	}

	storageManager := storage.NewManager()
	orgID, libraryID, head, _ := makeFixture("positive", true)
	certified := database.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, head)
	if certified.Outcome != dbpkg.LibraryBaselineCertificationCertified || certified.Reason != dbpkg.LibraryBaselineReasonApplied ||
		certified.FSObjectsWalked != 2 || certified.UniqueBlocks != 0 ||
		certified.PermanentLivenessWrites != 0 || certified.PhysicalRevalidations != 0 {
		t.Fatalf("authoritative real-Cassandra zero-block file certification=%+v; want CERTIFIED, two fs_objects and zero dependencies", certified)
	}
	if currentHead, witness := readLibraryBaselineCertifierWitness(t, database, orgID, libraryID); currentHead != head || witness == nil || *witness != head {
		t.Fatalf("zero-file witness head=%q certified=%v, want %q", currentHead, witness, head)
	}

	orgID, libraryID, head, _ = makeFixture("missing-claim", false)
	rejected := database.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, head)
	if rejected.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || rejected.Reason != dbpkg.LibraryBaselineReasonIdentityUnproven ||
		rejected.PermanentLivenessWrites != 0 || rejected.PhysicalRevalidations != 0 {
		t.Fatalf("zero-file without authority claim=%+v; want NOT_CERTIFIED/identity_unproven before physical or liveness work", rejected)
	}
	if _, witness := readLibraryBaselineCertifierWitness(t, database, orgID, libraryID); witness != nil {
		t.Fatalf("unclaimed zero file created witness %v", witness)
	}
}

func TestLibraryBaselineCertifierRejectsSHA1OnlyUnprovenMappingRealCassandra(t *testing.T) {
	database := shareProjectionDBForTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	orgID, libraryID, ownerID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	head := "pc-d1b1-sha1-unproven-" + uuid.NewString()
	rootFSID := baselineCertifierTestFSID("sha1-unproven-root-" + head)
	fileFSID := baselineCertifierTestFSID("sha1-unproven-file-" + head)
	content := []byte("legacy dependency remains unproven")
	sha1Sum, sha256Sum := sha1.Sum(content), sha256.Sum256(content)
	externalID, mappedCanonicalID := hex.EncodeToString(sha1Sum[:]), hex.EncodeToString(sha256Sum[:])
	entries, err := json.Marshal([]map[string]interface{}{{"id": fileFSID, "mode": 33188, "mtime": time.Now().Unix(), "name": "legacy.txt"}})
	if err != nil {
		t.Fatalf("marshal SHA1-only directory: %v", err)
	}

	seedLibraryBaselineCertifierLibrary(t, database, orgID, libraryID, ownerID, head, int64(len(content)))
	seedLibraryBaselineCertifierCommit(t, database, libraryID, head, rootFSID)
	now := time.Now().UTC()
	w2PostHeadRetryEachQuorum(t, "seed SHA1-only certifier root", func() error {
		return database.Session().Query(`INSERT INTO fs_objects (library_id, fs_id, obj_type, dir_entries, mtime) VALUES (?, ?, ?, ?, ?)`,
			libraryID, rootFSID, "dir", string(entries), now.Unix()).Consistency(gocql.EachQuorum).Exec()
	})
	if _, err := dbpkg.AuthorizeFSObjectProjection(context.Background(), database.Session(), dbpkg.FSObjectProjection{
		LibraryID: libraryID, FSID: rootFSID, ObjectType: "dir", DirectoryEntries: string(entries),
	}); err != nil {
		t.Fatalf("authorize SHA1-only root: %v", err)
	}
	w2PostHeadRetryEachQuorum(t, "seed SHA1-only certifier file", func() error {
		return database.Session().Query(`
			INSERT INTO fs_objects (library_id, fs_id, obj_type, size_bytes, mtime, block_ids, seafile_block_ids_sha1)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, libraryID, fileFSID, "file", int64(len(content)), now.Unix(), []string{externalID}, []string(nil)).Consistency(gocql.EachQuorum).Exec()
	})
	if _, err := dbpkg.AuthorizeFSObjectProjection(context.Background(), database.Session(), dbpkg.FSObjectProjection{
		LibraryID: libraryID, FSID: fileFSID, ObjectType: "file", SizeBytes: int64(len(content)),
		FileLayout: dbpkg.FileStorageSHA1Only, LogicalSHA1IDs: []string{externalID},
	}); err != nil {
		t.Fatalf("authorize SHA1-only file: %v", err)
	}
	w2PostHeadRetryEachQuorum(t, "seed mutable SHA1-only mapping without identity authority", func() error {
		return database.Session().Query(`
			INSERT INTO block_id_mappings (org_id, representation_id, external_id, internal_id, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, orgID, dbpkg.PlainBlockRepresentationID, externalID, mappedCanonicalID, now).Consistency(gocql.EachQuorum).Exec()
	})
	var storedMapping string
	if err := database.Session().Query(`
		SELECT internal_id FROM block_id_mappings
		WHERE org_id = ? AND representation_id = ? AND external_id = ?
	`, orgID, dbpkg.PlainBlockRepresentationID, externalID).Consistency(gocql.EachQuorum).Scan(&storedMapping); err != nil || storedMapping != mappedCanonicalID {
		t.Fatalf("confirm mutable mapping exists: value=%q err=%v", storedMapping, err)
	}

	result := database.CertifyLibraryBaseline(ctx, storage.NewManager(), orgID, libraryID, head)
	if result.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || result.Reason != dbpkg.LibraryBaselineReasonIdentityUnproven ||
		result.FSObjectsWalked != 2 || result.UniqueBlocks != 0 ||
		result.PermanentLivenessWrites != 0 || result.PhysicalRevalidations != 0 {
		t.Fatalf("SHA1-only identity with a present but unauthoritative mapping=%+v; want NOT_CERTIFIED/identity_unproven before physical or liveness work", result)
	}
	if _, witness := readLibraryBaselineCertifierWitness(t, database, orgID, libraryID); witness != nil {
		t.Fatalf("unproven SHA1-only mapping created witness %v", witness)
	}
}
func TestLibraryBaselineCertifierRejectsWhitespaceBoundFSIDsOnRealCassandra(t *testing.T) {
	database := shareProjectionDBForTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	storageManager := storage.NewManager()

	t.Run("commit root", func(t *testing.T) {
		orgID, libraryID, ownerID := uuid.NewString(), uuid.NewString(), uuid.NewString()
		head := "pc-d1b1-root-space-" + uuid.NewString()
		validRoot := baselineCertifierTestFSID("commit-root-" + head)
		claimedRoot := " " + validRoot + " "
		seedLibraryBaselineCertifierLibrary(t, database, orgID, libraryID, ownerID, head, 0)
		seedLibraryBaselineCertifierCommit(t, database, libraryID, head, claimedRoot)
		entries := "[]"
		w2PostHeadRetryEachQuorum(t, "seed valid tree at trimmed commit root", func() error {
			return database.Session().Query(`INSERT INTO fs_objects (library_id, fs_id, obj_type, dir_entries, mtime) VALUES (?, ?, ?, ?, ?)`,
				libraryID, validRoot, "dir", entries, time.Now().Unix()).Consistency(gocql.EachQuorum).Exec()
		})
		if _, err := dbpkg.AuthorizeFSObjectProjection(context.Background(), database.Session(), dbpkg.FSObjectProjection{
			LibraryID: libraryID, FSID: validRoot, ObjectType: "dir", DirectoryEntries: entries,
		}); err != nil {
			t.Fatalf("authorize trimmed-key tree: %v", err)
		}
		result := database.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, head)
		if result.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || result.Reason != dbpkg.LibraryBaselineReasonMalformedTree ||
			result.FSObjectsWalked != 0 || result.PermanentLivenessWrites != 0 || result.PhysicalRevalidations != 0 {
			t.Fatalf("whitespace-bound claimed root traversed another primary key: %+v", result)
		}
		if _, witness := readLibraryBaselineCertifierWitness(t, database, orgID, libraryID); witness != nil {
			t.Fatalf("malformed commit root created witness %v", witness)
		}
	})

	t.Run("directory entry", func(t *testing.T) {
		orgID, libraryID, ownerID := uuid.NewString(), uuid.NewString(), uuid.NewString()
		head := "pc-d1b1-entry-space-" + uuid.NewString()
		rootFSID := baselineCertifierTestFSID("entry-root-" + head)
		validChild := baselineCertifierTestFSID("entry-child-" + head)
		claimedChild := " " + validChild + " "
		entries, err := json.Marshal([]map[string]interface{}{{"id": claimedChild, "mode": 33188, "mtime": time.Now().Unix(), "name": "child"}})
		if err != nil {
			t.Fatalf("marshal whitespace-bound child entry: %v", err)
		}
		seedLibraryBaselineCertifierLibrary(t, database, orgID, libraryID, ownerID, head, 0)
		seedLibraryBaselineCertifierCommit(t, database, libraryID, head, rootFSID)
		seedBaselineCertifierEdgeFixture(t, database, orgID, libraryID, head, rootFSID, string(entries), validChild, true)
		result := database.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, head)
		if result.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || result.Reason != dbpkg.LibraryBaselineReasonMalformedTree ||
			result.FSObjectsWalked != 1 || result.PermanentLivenessWrites != 0 || result.PhysicalRevalidations != 0 {
			t.Fatalf("whitespace-bound directory entry traversed its trimmed child: %+v", result)
		}
		if _, witness := readLibraryBaselineCertifierWitness(t, database, orgID, libraryID); witness != nil {
			t.Fatalf("malformed directory entry created witness %v", witness)
		}
	})
}
