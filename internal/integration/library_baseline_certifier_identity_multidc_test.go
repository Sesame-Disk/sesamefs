//go:build integration

package integration

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

const (
	libraryBaselineCertifierIdentityDivergenceEvidenceEnv  = "SESAMEFS_REQUIRE_LIBRARY_BASELINE_CERTIFIER_IDENTITY_DIVERGENCE_EVIDENCE"
	libraryBaselineCertifierIdentityDivergencePhaseEnv     = "SESAMEFS_LIBRARY_BASELINE_CERTIFIER_IDENTITY_DIVERGENCE_PHASE"
	libraryBaselineCertifierIdentityDivergenceRunIDEnv     = "SESAMEFS_LIBRARY_BASELINE_CERTIFIER_IDENTITY_DIVERGENCE_RUN_ID"
	libraryBaselineCertifierIdentityUnavailableEvidenceEnv = "SESAMEFS_REQUIRE_LIBRARY_BASELINE_CERTIFIER_IDENTITY_UNAVAILABLE_EVIDENCE"
	libraryBaselineCertifierIdentityUnavailablePhaseEnv    = "SESAMEFS_LIBRARY_BASELINE_CERTIFIER_IDENTITY_UNAVAILABLE_PHASE"
	libraryBaselineCertifierIdentityUnavailableRunIDEnv    = "SESAMEFS_LIBRARY_BASELINE_CERTIFIER_IDENTITY_UNAVAILABLE_RUN_ID"
)

func TestLibraryBaselineCertifierIdentityDivergence3DC(t *testing.T) {
	phase := strings.TrimSpace(os.Getenv(libraryBaselineCertifierIdentityDivergencePhaseEnv))
	if phase == "" {
		t.Skipf("%s is not set", libraryBaselineCertifierIdentityDivergencePhaseEnv)
	}
	if phase != "prepare" && phase != "degrade" && phase != "verify" {
		t.Fatalf("%s=%q, want prepare, degrade, or verify", libraryBaselineCertifierIdentityDivergencePhaseEnv, phase)
	}
	if phase == "verify" && os.Getenv(libraryBaselineCertifierIdentityDivergenceEvidenceEnv) != "1" {
		t.Fatalf("%s=1 is required for the verification phase", libraryBaselineCertifierIdentityDivergenceEvidenceEnv)
	}
	runID := strings.TrimSpace(os.Getenv(libraryBaselineCertifierIdentityDivergenceRunIDEnv))
	namespace, err := uuid.Parse(runID)
	if err != nil {
		t.Fatalf("%s must be a UUID: %v", libraryBaselineCertifierIdentityDivergenceRunIDEnv, err)
	}
	stableID := func(name string) string {
		return uuid.NewSHA1(namespace, []byte("pc-d1b1-identity-divergence-"+name)).String()
	}
	orgID := stableID("org")
	fsLibraryID, commitLibraryID := stableID("fs-library"), stableID("commit-library")
	ownerID := stableID("owner")
	fsHead, commitHead := "pc-d1b1-identity-fs-head-"+strings.ReplaceAll(runID, "-", ""), "pc-d1b1-identity-commit-head-"+strings.ReplaceAll(runID, "-", "")
	fsRoot, fsFile := baselineCertifierTestFSID("identity-"+runID+"-fs-root"), baselineCertifierTestFSID("identity-"+runID+"-fs-file")
	commitRootA, commitRootB := baselineCertifierTestFSID("identity-"+runID+"-commit-root-a"), baselineCertifierTestFSID("identity-"+runID+"-commit-root-b")
	commitFileA, commitFileB := baselineCertifierTestFSID("identity-"+runID+"-commit-file-a"), baselineCertifierTestFSID("identity-"+runID+"-commit-file-b")
	contentA, contentB := []byte("identity-authority-projection-A"), []byte("identity-authority-projection-B")
	sha256A, sha1A := sha256.Sum256(contentA), sha1.Sum(contentA)
	sha256B, sha1B := sha256.Sum256(contentB), sha1.Sum(contentB)
	blockA, externalA := hex.EncodeToString(sha256A[:]), hex.EncodeToString(sha1A[:])
	blockB, externalB := hex.EncodeToString(sha256B[:]), hex.EncodeToString(sha1B[:])
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	endpoints := w2PostHead3DCEndpoints(t)
	na := w2PostHead3DCConnectSerial(t, "dc-na", endpoints, "LOCAL_SERIAL")

	if phase == "prepare" {
		storageManager, blockStore := newLibraryBaselineCertifierStorage(t, ctx, orgID, libraryBaselineCertifierMinIOEndpoint, true)
		storageKey, err := blockStore.MintStorageKey(blockB)
		if err != nil {
			t.Fatalf("mint exact B physical incarnation: %v", err)
		}
		if _, err := blockStore.PutObjectAutoDirect(ctx, storageKey, contentB); err != nil {
			t.Fatalf("store B bytes at exact minted P: %v", err)
		}
		_ = storageManager
		sizeA, sizeB := int64(len(contentA)), int64(len(contentB))
		seedLibraryBaselineCertifierLibrary(t, na, orgID, fsLibraryID, ownerID, fsHead, sizeA)
		seedLibraryBaselineCertifierCommit(t, na, fsLibraryID, fsHead, fsRoot)
		seedLibraryBaselineCertifierTree(t, na, orgID, fsLibraryID, fsRoot, fsFile, blockA, externalA, sizeA)

		seedLibraryBaselineCertifierLibrary(t, na, orgID, commitLibraryID, ownerID, commitHead, sizeA)
		seedLibraryBaselineCertifierCommit(t, na, commitLibraryID, commitHead, commitRootA)
		seedLibraryBaselineCertifierTree(t, na, orgID, commitLibraryID, commitRootA, commitFileA, blockA, externalA, sizeA)
		seedLibraryBaselineCertifierTreeProjection(t, na, commitLibraryID, commitRootB, commitFileB, blockB, externalB, sizeB)
		seedLibraryBaselineCertifierBlock(t, na, orgID, blockB, externalB, "evidence", storageKey, sizeB)
		w2PostHeadRetryEachQuorum(t, "seed identity-divergence B mapping", func() error {
			return na.Session().Query(`
				INSERT INTO block_id_mappings (org_id, representation_id, external_id, internal_id, created_at)
				VALUES (?, ?, ?, ?, ?)
			`, orgID, dbpkg.PlainBlockRepresentationID, externalB, blockB, time.Now().UTC()).Consistency(gocql.EachQuorum).Exec()
		})
		return
	}

	if phase == "degrade" {
		w2PostHeadRetryEachQuorum(t, "write complete local fs_object B projection", func() error {
			return na.Session().Query(`
				UPDATE fs_objects SET size_bytes = ?, block_ids = ?, seafile_block_ids_sha1 = ?
				WHERE library_id = ? AND fs_id = ?
			`, int64(len(contentB)), []string{blockB}, []string{externalB}, fsLibraryID, fsFile).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec()
		})
		w2PostHeadRetryEachQuorum(t, "write local commit H->R2 projection", func() error {
			return na.Session().Query(`UPDATE commits SET root_fs_id = ? WHERE library_id = ? AND commit_id = ?`, commitRootB, commitLibraryID, commitHead).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec()
		})
		return
	}

	eu := w2PostHead3DCConnectSerial(t, "dc-eu", endpoints, "LOCAL_SERIAL")
	assertFSProjection := func(database *dbpkg.DB, libraryID, fileID, wantBlock, wantExternal, label string) {
		t.Helper()
		var objectType *string
		var sizeBytes *int64
		var blockIDs *[]string
		var externalIDs *[]string
		if err := database.Session().Query(`
			SELECT obj_type, size_bytes, block_ids, seafile_block_ids_sha1
			FROM fs_objects WHERE library_id = ? AND fs_id = ?
		`, libraryID, fileID).WithContext(ctx).Consistency(gocql.LocalQuorum).Scan(&objectType, &sizeBytes, &blockIDs, &externalIDs); err != nil {
			t.Fatalf("read %s fs_object projection: %v", label, err)
		}
		if objectType == nil || *objectType != "file" || sizeBytes == nil || blockIDs == nil || externalIDs == nil ||
			len(*blockIDs) != 1 || (*blockIDs)[0] != wantBlock || len(*externalIDs) != 1 || (*externalIDs)[0] != wantExternal {
			t.Fatalf("%s fs_object projection = type=%v size=%v block_ids=%v external_ids=%v", label, objectType, sizeBytes, blockIDs, externalIDs)
		}
	}
	assertFSProjection(na, fsLibraryID, fsFile, blockB, externalB, "dc-na")
	assertFSProjection(eu, fsLibraryID, fsFile, blockA, externalA, "dc-eu")
	var localRoot, remoteRoot string
	if err := na.Session().Query(`SELECT root_fs_id FROM commits WHERE library_id = ? AND commit_id = ?`, commitLibraryID, commitHead).WithContext(ctx).Consistency(gocql.LocalQuorum).Scan(&localRoot); err != nil {
		t.Fatalf("read local commit H projection: %v", err)
	}
	if err := eu.Session().Query(`SELECT root_fs_id FROM commits WHERE library_id = ? AND commit_id = ?`, commitLibraryID, commitHead).WithContext(ctx).Consistency(gocql.LocalQuorum).Scan(&remoteRoot); err != nil {
		t.Fatalf("read remote commit H projection: %v", err)
	}
	if localRoot != commitRootB || remoteRoot != commitRootA {
		t.Fatalf("commit H source divergence = local %q / remote %q, want R2 / R1", localRoot, remoteRoot)
	}

	storageManager, _ := newLibraryBaselineCertifierStorage(t, ctx, orgID, libraryBaselineCertifierMinIOEndpoint, false)
	fsResult := na.CertifyLibraryBaseline(ctx, storageManager, orgID, fsLibraryID, fsHead)
	if fsResult.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || fsResult.Reason != dbpkg.LibraryBaselineReasonIdentityConflict ||
		fsResult.CommitsWalked != 1 || fsResult.UniqueBlocks != 0 || fsResult.PermanentLivenessWrites != 0 || fsResult.PhysicalRevalidations != 0 {
		t.Fatalf("complete fs_object authority A vs local B was not rejected before physical/liveness work: %+v", fsResult)
	}
	commitResult := na.CertifyLibraryBaseline(ctx, storageManager, orgID, commitLibraryID, commitHead)
	if commitResult.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || commitResult.Reason != dbpkg.LibraryBaselineReasonIdentityConflict ||
		commitResult.CommitsWalked != 0 || commitResult.FSObjectsWalked != 0 || commitResult.PermanentLivenessWrites != 0 || commitResult.PhysicalRevalidations != 0 {
		t.Fatalf("commit H->R1 authority vs local H->R2 was not rejected before tree/physical/liveness work: %+v", commitResult)
	}
	for _, fixture := range []struct{ libraryID, head string }{{fsLibraryID, fsHead}, {commitLibraryID, commitHead}} {
		if current, certified := readLibraryBaselineCertifierWitness(t, na, orgID, fixture.libraryID); current != fixture.head || certified != nil {
			t.Fatalf("identity divergence created a witness for %s: head=%q certified=%v", fixture.libraryID, current, certified)
		}
	}
	referrer := dbpkg.BlockReferrerForFSObject(fsLibraryID, fsFile)
	var existing string
	err = na.Session().Query(`SELECT referrer FROM block_references WHERE org_id = ? AND block_id = ? AND referrer = ?`, orgID, blockB, referrer).WithContext(ctx).Consistency(gocql.EachQuorum).Scan(&existing)
	if err == nil || !errors.Is(err, gocql.ErrNotFound) {
		t.Fatalf("identity conflict wrote or ambiguously read a permanent liveness reference: referrer=%q err=%v", existing, err)
	}
	t.Logf("GREEN: globally claimed complete fs_object A vs local B and claimed H->R1 vs local H->R2 both failed before liveness/physical proof and without a witness")
}

func seedLibraryBaselineCertifierTreeProjection(t *testing.T, database *dbpkg.DB, libraryID, rootFSID, fileFSID, blockID, externalID string, sizeBytes int64) {
	t.Helper()
	entries, err := json.Marshal([]map[string]interface{}{{"id": fileFSID, "mode": 33188, "mtime": time.Now().Unix(), "name": "alternate.txt"}})
	if err != nil {
		t.Fatalf("marshal alternate identity-divergence tree: %v", err)
	}
	rootEntries := string(entries)
	w2PostHeadRetryEachQuorum(t, "seed alternate identity-divergence root", func() error {
		return database.Session().Query(`INSERT INTO fs_objects (library_id, fs_id, obj_type, dir_entries, mtime) VALUES (?, ?, ?, ?, ?)`, libraryID, rootFSID, "dir", rootEntries, time.Now().Unix()).Consistency(gocql.EachQuorum).Exec()
	})
	if _, err := dbpkg.AuthorizeFSObjectProjection(context.Background(), database.Session(), dbpkg.FSObjectProjection{LibraryID: libraryID, FSID: rootFSID, ObjectType: "dir", DirectoryEntries: rootEntries}); err != nil {
		t.Fatalf("establish alternate root identity: %v", err)
	}
	w2PostHeadRetryEachQuorum(t, "seed alternate identity-divergence file", func() error {
		return database.Session().Query(`
			INSERT INTO fs_objects (library_id, fs_id, obj_type, size_bytes, block_ids, seafile_block_ids_sha1, mtime)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, libraryID, fileFSID, "file", sizeBytes, []string{blockID}, []string{externalID}, time.Now().Unix()).Consistency(gocql.EachQuorum).Exec()
	})
	if _, err := dbpkg.AuthorizeFSObjectProjection(context.Background(), database.Session(), dbpkg.FSObjectProjection{
		LibraryID: libraryID, FSID: fileFSID, ObjectType: "file", SizeBytes: sizeBytes,
		FileLayout: dbpkg.FileStoragePairedCanonical, LogicalSHA1IDs: []string{externalID}, CanonicalSHA256IDs: []string{blockID},
	}); err != nil {
		t.Fatalf("establish alternate file identity: %v", err)
	}
}

func TestLibraryBaselineCertifierIdentityAuthorityUnavailable3DC(t *testing.T) {
	phase := strings.TrimSpace(os.Getenv(libraryBaselineCertifierIdentityUnavailablePhaseEnv))
	if phase == "" {
		t.Skipf("%s is not set", libraryBaselineCertifierIdentityUnavailablePhaseEnv)
	}
	if phase != "prepare" && phase != "verify" {
		t.Fatalf("%s=%q, want prepare or verify", libraryBaselineCertifierIdentityUnavailablePhaseEnv, phase)
	}
	if phase == "verify" && os.Getenv(libraryBaselineCertifierIdentityUnavailableEvidenceEnv) != "1" {
		t.Fatalf("%s=1 is required for the verification phase", libraryBaselineCertifierIdentityUnavailableEvidenceEnv)
	}
	runID := strings.TrimSpace(os.Getenv(libraryBaselineCertifierIdentityUnavailableRunIDEnv))
	namespace, err := uuid.Parse(runID)
	if err != nil {
		t.Fatalf("%s must be a UUID: %v", libraryBaselineCertifierIdentityUnavailableRunIDEnv, err)
	}
	stableID := func(name string) string {
		return uuid.NewSHA1(namespace, []byte("pc-d1b1-identity-unavailable-"+name)).String()
	}
	orgID, libraryID, ownerID := stableID("org"), stableID("library"), stableID("owner")
	head, rootFSID, fileFSID := "pc-d1b1-identity-unavailable-head-"+strings.ReplaceAll(runID, "-", ""), baselineCertifierTestFSID("identity-unavailable-"+runID+"-root"), baselineCertifierTestFSID("identity-unavailable-"+runID+"-file")
	content := []byte("identity authority unavailable must fail closed")
	sha256Sum, sha1Sum := sha256.Sum256(content), sha1.Sum(content)
	blockID, externalID := hex.EncodeToString(sha256Sum[:]), hex.EncodeToString(sha1Sum[:])
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	endpoints := w2PostHead3DCEndpoints(t)
	na := w2PostHead3DCConnectSerial(t, "dc-na", endpoints, "LOCAL_SERIAL")

	if phase == "prepare" {
		_, blockStore := newLibraryBaselineCertifierStorage(t, ctx, orgID, libraryBaselineCertifierMinIOEndpoint, true)
		storageKey, err := blockStore.MintStorageKey(blockID)
		if err != nil {
			t.Fatalf("mint identity-unavailable evidence P: %v", err)
		}
		if _, err := blockStore.PutObjectAutoDirect(ctx, storageKey, content); err != nil {
			t.Fatalf("store identity-unavailable evidence bytes: %v", err)
		}
		sizeBytes := int64(len(content))
		seedLibraryBaselineCertifierLibrary(t, na, orgID, libraryID, ownerID, head, sizeBytes)
		seedLibraryBaselineCertifierCommit(t, na, libraryID, head, rootFSID)
		seedLibraryBaselineCertifierTree(t, na, orgID, libraryID, rootFSID, fileFSID, blockID, externalID, sizeBytes)
		seedLibraryBaselineCertifierBlock(t, na, orgID, blockID, externalID, "evidence", storageKey, sizeBytes)
		return
	}

	storageManager, _ := newLibraryBaselineCertifierStorage(t, ctx, orgID, libraryBaselineCertifierMinIOEndpoint, false)
	result := na.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, head)
	if result.Outcome != dbpkg.LibraryBaselineCertificationUnknown || result.Reason != dbpkg.LibraryBaselineReasonIdentityAuthorityUnavailable ||
		result.CommitsWalked != 0 || result.FSObjectsWalked != 0 || result.PermanentLivenessWrites != 0 || result.PhysicalRevalidations != 0 {
		t.Fatalf("unavailable global identity authority did not stop certification before tree/physical/liveness work: %+v", result)
	}
	var certifiedHead *string
	if err := na.Session().Query(`SELECT continuity_certified_head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, libraryID).WithContext(ctx).Consistency(gocql.LocalQuorum).Scan(&certifiedHead); err != nil {
		t.Fatalf("locally read identity-unavailable witness state: %v", err)
	}
	if certifiedHead != nil {
		t.Fatalf("unavailable identity authority created a witness: %v", *certifiedHead)
	}
	t.Logf("GREEN: global SERIAL identity-authority outage returned UNKNOWN before the tree, physical, liveness, or witness steps")
}
