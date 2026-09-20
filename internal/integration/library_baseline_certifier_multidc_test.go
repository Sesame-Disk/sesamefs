//go:build integration

package integration

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
	"time"

	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

const libraryBaselineCertifierEvidenceEnv = "SESAMEFS_REQUIRE_LIBRARY_BASELINE_CERTIFIER_EVIDENCE"

var libraryBaselineCertifierEvidence bool

func seedLibraryBaselineCertifierLibrary(t *testing.T, database *dbpkg.DB, orgID, libraryID, ownerID, head string) {
	t.Helper()
	now := time.Now().UTC()
	w2PostHeadRetryEachQuorum(t, "seed baseline-certifier library", func() error {
		return database.Session().Query(`
			INSERT INTO libraries (org_id, library_id, owner_id, name, encrypted, block_representation_id, storage_class, size_bytes, file_count, head_commit_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, orgID, libraryID, ownerID, "pc-d1b1-certifier-3dc", false, dbpkg.PlainBlockRepresentationID, "evidence", int64(1), int64(1), head, now, now).Consistency(gocql.EachQuorum).Exec()
	})
}
func seedLibraryBaselineCertifierCommit(t *testing.T, database *dbpkg.DB, libraryID, commitID, rootFSID string) {
	t.Helper()
	now := time.Now().UTC()
	w2PostHeadRetryEachQuorum(t, "seed baseline-certifier commit", func() error {
		return database.Session().Query(`
			INSERT INTO commits (library_id, commit_id, parent_id, root_fs_id, description, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, libraryID, commitID, "", rootFSID, "pc-d1b1 certifier", now).Consistency(gocql.EachQuorum).Exec()
	})
}
func seedLibraryBaselineCertifierTree(t *testing.T, database *dbpkg.DB, orgID, libraryID, rootFSID, fileFSID, blockID, externalID string) {
	t.Helper()
	entries, err := json.Marshal([]map[string]interface{}{{"id": fileFSID, "mode": 33188, "mtime": time.Now().Unix(), "name": "evidence.txt"}})
	if err != nil {
		t.Fatalf("marshal baseline-certifier directory: %v", err)
	}
	now := time.Now().UTC()
	w2PostHeadRetryEachQuorum(t, "seed baseline-certifier root", func() error {
		return database.Session().Query(`
			INSERT INTO fs_objects (library_id, fs_id, obj_type, dir_entries, mtime)
			VALUES (?, ?, ?, ?, ?)
		`, libraryID, rootFSID, "dir", string(entries), now.Unix()).Consistency(gocql.EachQuorum).Exec()
	})
	w2PostHeadRetryEachQuorum(t, "seed baseline-certifier file", func() error {
		return database.Session().Query(`
			INSERT INTO fs_objects (library_id, fs_id, obj_type, size_bytes, mtime, block_ids, seafile_block_ids_sha1)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, libraryID, fileFSID, "file", int64(1), now.Unix(), []string{blockID}, []string{externalID}).Consistency(gocql.EachQuorum).Exec()
	})
	w2PostHeadRetryEachQuorum(t, "seed baseline-certifier mapping", func() error {
		return database.Session().Query(`
			INSERT INTO block_id_mappings (org_id, representation_id, external_id, internal_id, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, orgID, dbpkg.PlainBlockRepresentationID, externalID, blockID, now).Consistency(gocql.EachQuorum).Exec()
	})
}
func seedLibraryBaselineCertifierBlock(t *testing.T, database *dbpkg.DB, orgID, blockID, externalID, storageClass, storageKey string) {
	t.Helper()
	now := time.Now().UTC()
	w2PostHeadRetryEachQuorum(t, "seed baseline-certifier block", func() error {
		return database.Session().Query(`
			INSERT INTO blocks (org_id, block_id, representation_id, sha1, size_bytes, storage_class, storage_key, gc_state, gc_claim_id, created_at, last_accessed)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, orgID, blockID, dbpkg.PlainBlockRepresentationID, externalID, int64(1), storageClass, storageKey, "", "", now, now).Consistency(gocql.EachQuorum).Exec()
	})
}
func TestLibraryBaselineCertifier3DC(t *testing.T) {
	if os.Getenv(libraryBaselineCertifierEvidenceEnv) != "1" {
		t.Skipf("%s is not set", libraryBaselineCertifierEvidenceEnv)
	}
	endpoints := w2PostHead3DCEndpoints(t)
	na := w2PostHead3DCConnectSerial(t, "dc-na", endpoints, "LOCAL_SERIAL")
	eu := w2PostHead3DCConnectSerial(t, "dc-eu", endpoints, "LOCAL_SERIAL")
	asia := w2PostHead3DCConnectSerial(t, "dc-asia", endpoints, "LOCAL_SERIAL")
	orgID, libraryID, ownerID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	h0, h1 := "pc-d1b1-h0-"+uuid.NewString(), "pc-d1b1-h1-"+uuid.NewString()
	rootFSID, fileFSID := "pc-d1b1-root-"+uuid.NewString(), "pc-d1b1-file-"+uuid.NewString()
	content := []byte("pc-d1b1 certified baseline")
	sha256Sum := sha256.Sum256(content)
	sha1Sum := sha1.Sum(content)
	blockID, externalID := hex.EncodeToString(sha256Sum[:]), hex.EncodeToString(sha1Sum[:])
	blockStore, err := storage.NewOrgBlockStore(nil, "blocks/", orgID)
	if err != nil {
		t.Fatalf("create evidence block store: %v", err)
	}
	p1, err := blockStore.MintStorageKey(blockID)
	if err != nil {
		t.Fatalf("mint evidence P1: %v", err)
	}
	p2, err := blockStore.MintStorageKey(blockID)
	if err != nil {
		t.Fatalf("mint evidence P2: %v", err)
	}
	seedLibraryBaselineCertifierLibrary(t, na, orgID, libraryID, ownerID, h0)
	seedLibraryBaselineCertifierCommit(t, na, libraryID, h0, rootFSID)
	seedLibraryBaselineCertifierCommit(t, na, libraryID, h1, rootFSID)
	seedLibraryBaselineCertifierTree(t, na, orgID, libraryID, rootFSID, fileFSID, blockID, externalID)
	seedLibraryBaselineCertifierBlock(t, na, orgID, blockID, externalID, "evidence", p1)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	certified := na.CertifyLibraryBaseline(ctx, orgID, libraryID, h0)
	if certified.Outcome != dbpkg.LibraryBaselineCertificationCertified || certified.Reason != dbpkg.LibraryBaselineReasonApplied {
		t.Fatalf("successful baseline certification = %+v", certified)
	}
	if certified.FSObjectsWalked != 2 || certified.UniqueBlocks != 1 || certified.PermanentLivenessWrites != 1 || certified.PhysicalRevalidations != 1 {
		t.Fatalf("successful baseline proof counters = %+v", certified)
	}
	referrer := dbpkg.BlockReferrerForFSObject(libraryID, fileFSID)
	var storedLibraryID string
	var libraryTTL, createdTTL *int
	if err := na.Session().Query(`
		SELECT library_id, TTL(library_id), TTL(created_at) FROM block_references WHERE org_id = ? AND block_id = ? AND referrer = ?
	`, orgID, blockID, referrer).Consistency(gocql.EachQuorum).Scan(&storedLibraryID, &libraryTTL, &createdTTL); err != nil {
		t.Fatalf("read permanent baseline reference: %v", err)
	}
	libraryTTLValue, createdTTLValue := -1, -1
	if libraryTTL != nil {
		libraryTTLValue = *libraryTTL
	}
	if createdTTL != nil {
		createdTTLValue = *createdTTL
	}
	if storedLibraryID != libraryID || libraryTTLValue != -1 || createdTTLValue != -1 {
		t.Fatalf("baseline reference is not exact permanent liveness: library=%q library_ttl=%d created_ttl=%d", storedLibraryID, libraryTTLValue, createdTTLValue)
	}
	retry := eu.CertifyLibraryBaseline(ctx, orgID, libraryID, h0)
	if retry.Outcome != dbpkg.LibraryBaselineCertificationCertified || retry.PermanentLivenessWrites != 0 {
		t.Fatalf("idempotent baseline retry = %+v", retry)
	}
	if err := v2pkg.NewFSHelper(na).UpdateLibraryHead(orgID, libraryID, h1, h0); err != nil {
		t.Fatalf("advance HEAD H0->H1: %v", err)
	}
	stale := asia.CertifyLibraryBaseline(ctx, orgID, libraryID, h0)
	if stale.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || stale.Reason != dbpkg.LibraryBaselineReasonHeadChanged {
		t.Fatalf("moving-HEAD stale certification = %+v", stale)
	}
	var settledHead string
	var settledCertifiedHead *string
	if err := asia.Session().Query("SELECT head_commit_id, continuity_certified_head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?", orgID, libraryID).Consistency(gocql.Serial).Scan(&settledHead, &settledCertifiedHead); err != nil {
		t.Fatalf("serial-read stale witness state: %v", err)
	}
	if settledHead != h1 || settledCertifiedHead == nil || *settledCertifiedHead != h0 {
		t.Fatalf("moving HEAD changed stale witness unexpectedly: head=%q certified=%v", settledHead, settledCertifiedHead)
	}
	if err := na.Session().Query(`UPDATE blocks SET storage_key = ? WHERE org_id = ? AND block_id = ?`, p2, orgID, blockID).Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatalf("change canonical physical P: %v", err)
	}
	authority, err := na.ValidateLibraryContinuityPhysicalAuthorityContext(ctx, orgID, blockID, dbpkg.BlockPhysicalLocation{StorageClass: "evidence", StorageKey: p1})
	if err == nil || authority != dbpkg.BlockRepairAuthorityChanged {
		t.Fatalf("changed P authority = %v/%v, want Changed with diagnostic", authority, err)
	}
	legacyKey := blockStore.StorageKeyForHash(blockID)
	if err := na.Session().Query(`UPDATE blocks SET storage_key = ? WHERE org_id = ? AND block_id = ?`, legacyKey, orgID, blockID).Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatalf("install legacy deterministic locator: %v", err)
	}
	legacy := na.CertifyLibraryBaseline(ctx, orgID, libraryID, h1)
	if legacy.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || legacy.Reason != dbpkg.LibraryBaselineReasonLegacyLocator {
		t.Fatalf("legacy locator certification = %+v", legacy)
	}
	if err := na.Session().Query(`UPDATE blocks SET storage_key = ? WHERE org_id = ? AND block_id = ?`, p2, orgID, blockID).Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatalf("restore minted P2 after legacy locator rejection: %v", err)
	}
	fresh := eu.CertifyLibraryBaseline(ctx, orgID, libraryID, h1)
	if fresh.Outcome != dbpkg.LibraryBaselineCertificationCertified {
		t.Fatalf("fresh H1 certification after P change = %+v", fresh)
	}
	claimID := uuid.NewString()
	if err := na.Session().Query(`UPDATE blocks SET gc_state = ?, gc_claim_id = ?, gc_claimed_at = ? WHERE org_id = ? AND block_id = ?`, dbpkg.BlockGCStateDeleting, claimID, time.Now().UTC(), orgID, blockID).Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatalf("install GC claim state: %v", err)
	}
	gcBlocked := na.CertifyLibraryBaseline(ctx, orgID, libraryID, h1)
	if gcBlocked.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || gcBlocked.Reason != dbpkg.LibraryBaselineReasonGCAuthorityConflict {
		t.Fatalf("GC-blocked certification = %+v", gcBlocked)
	}
	missingOrgID, missingLibraryID := uuid.NewString(), uuid.NewString()
	missingHead := "pc-d1b1-missing-" + uuid.NewString()
	seedLibraryBaselineCertifierLibrary(t, na, missingOrgID, missingLibraryID, uuid.NewString(), missingHead)
	missing := na.CertifyLibraryBaseline(ctx, missingOrgID, missingLibraryID, missingHead)
	if missing.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || missing.Reason != dbpkg.LibraryBaselineReasonMissingCommit {
		t.Fatalf("missing-commit certification = %+v", missing)
	}
	deletedOrgID, deletedLibraryID := uuid.NewString(), uuid.NewString()
	deletedHead, deletedRoot := "pc-d1b1-deleted-"+uuid.NewString(), "pc-d1b1-deleted-root-"+uuid.NewString()
	seedLibraryBaselineCertifierLibrary(t, na, deletedOrgID, deletedLibraryID, uuid.NewString(), deletedHead)
	seedLibraryBaselineCertifierCommit(t, na, deletedLibraryID, deletedHead, deletedRoot)
	w2PostHeadRetryEachQuorum(t, "seed deleted baseline-certifier root", func() error {
		return na.Session().Query(`INSERT INTO fs_objects (library_id, fs_id, obj_type, dir_entries) VALUES (?, ?, ?, ?)`, deletedLibraryID, deletedRoot, "dir", "[]").Consistency(gocql.EachQuorum).Exec()
	})
	if err := na.Session().Query(`UPDATE libraries SET deleted_at = ? WHERE org_id = ? AND library_id = ?`, time.Now().UTC(), deletedOrgID, deletedLibraryID).Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatalf("soft-delete baseline-certifier library: %v", err)
	}
	deleted := asia.CertifyLibraryBaseline(ctx, deletedOrgID, deletedLibraryID, deletedHead)
	if deleted.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || deleted.Reason != dbpkg.LibraryBaselineReasonLibraryDeleted {
		t.Fatalf("deleted-library certification = %+v", deleted)
	}
	libraryBaselineCertifierEvidence = true
	t.Logf("GREEN: full reachable tree certified with exact minted P, permanent EACH_QUORUM-visible liveness, idempotent retry, stale HEAD rejection, P-change/legacy/GC fail-closed legs, and missing/deleted proof rejection")
}
