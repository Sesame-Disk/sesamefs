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

	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

const (
	libraryBaselineCertifierEvidenceEnv              = "SESAMEFS_REQUIRE_LIBRARY_BASELINE_CERTIFIER_EVIDENCE"
	libraryBaselineCertifierUnavailableDCEvidenceEnv = "SESAMEFS_REQUIRE_LIBRARY_BASELINE_CERTIFIER_UNAVAILABLE_DC_EVIDENCE"
	libraryBaselineCertifierUnavailableDCPhaseEnv    = "SESAMEFS_LIBRARY_BASELINE_CERTIFIER_UNAVAILABLE_DC_PHASE"
	libraryBaselineCertifierUnavailableDCRunIDEnv    = "SESAMEFS_LIBRARY_BASELINE_CERTIFIER_UNAVAILABLE_DC_RUN_ID"
)

var (
	libraryBaselineCertifierEvidence              bool
	libraryBaselineCertifierUnavailableDCEvidence bool
)

const libraryBaselineCertifierMinIOEndpoint = "http://minio:9000"

func newLibraryBaselineCertifierStorage(t *testing.T, ctx context.Context, orgID, endpoint string, ensureBucket bool) (*storage.Manager, *storage.BlockStore) {
	t.Helper()
	if endpoint == "" {
		endpoint = libraryBaselineCertifierMinIOEndpoint
	}
	bucket := "pc-d1b1-" + strings.ReplaceAll(strings.ToLower(orgID), "-", "")
	if ensureBucket {
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
			awsconfig.WithRegion("us-east-1"),
			awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("minioadmin", "minioadmin", "")),
		)
		if err != nil {
			t.Fatalf("load MinIO evidence credentials: %v", err)
		}
		client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
			options.BaseEndpoint = aws.String(endpoint)
			options.UsePathStyle = true
		})
		if _, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)}); err != nil {
			if _, createErr := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); createErr != nil {
				t.Fatalf("create MinIO evidence bucket %s: %v", bucket, createErr)
			}
		}
	}
	s3Store, err := storage.NewS3Store(ctx, storage.S3Config{
		Endpoint: endpoint, Bucket: bucket, Region: "us-east-1",
		AccessKeyID: "minioadmin", SecretAccessKey: "minioadmin", UsePathStyle: true,
	})
	if err != nil {
		t.Fatalf("create evidence S3 store: %v", err)
	}
	manager := storage.NewManager()
	manager.RegisterBackend("evidence", s3Store, "")
	blockStore, err := manager.GetBlockStoreForOrg(orgID, "evidence")
	if err != nil {
		t.Fatalf("resolve evidence storage class: %v", err)
	}
	return manager, blockStore
}

func readLibraryBaselineCertifierWitness(t *testing.T, database *dbpkg.DB, orgID, libraryID string) (string, *string) {
	t.Helper()
	var head string
	var certifiedHead *string
	err := database.Session().Query(`
		SELECT head_commit_id, continuity_certified_head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, libraryID).Consistency(gocql.Serial).Scan(&head, &certifiedHead)
	if err != nil {
		t.Fatalf("serial-read baseline-certifier witness: %v", err)
	}
	return head, certifiedHead
}

func seedLibraryBaselineCertifierLibrary(t *testing.T, database *dbpkg.DB, orgID, libraryID, ownerID, head string, sizeBytes int64) {
	t.Helper()
	now := time.Now().UTC()
	w2PostHeadRetryEachQuorum(t, "seed baseline-certifier library", func() error {
		return database.Session().Query(`
			INSERT INTO libraries (org_id, library_id, owner_id, name, encrypted, block_representation_id, storage_class, size_bytes, file_count, head_commit_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, orgID, libraryID, ownerID, "pc-d1b1-certifier-3dc", false, dbpkg.PlainBlockRepresentationID, "evidence", sizeBytes, int64(1), head, now, now).Consistency(gocql.EachQuorum).Exec()
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
func seedLibraryBaselineCertifierTree(t *testing.T, database *dbpkg.DB, orgID, libraryID, rootFSID, fileFSID, blockID, externalID string, sizeBytes int64) {
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
		`, libraryID, fileFSID, "file", sizeBytes, now.Unix(), []string{blockID}, []string{externalID}).Consistency(gocql.EachQuorum).Exec()
	})
	w2PostHeadRetryEachQuorum(t, "seed baseline-certifier mapping", func() error {
		return database.Session().Query(`
			INSERT INTO block_id_mappings (org_id, representation_id, external_id, internal_id, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, orgID, dbpkg.PlainBlockRepresentationID, externalID, blockID, now).Consistency(gocql.EachQuorum).Exec()
	})
}
func seedLibraryBaselineCertifierBlock(t *testing.T, database *dbpkg.DB, orgID, blockID, externalID, storageClass, storageKey string, sizeBytes int64) {
	t.Helper()
	now := time.Now().UTC()
	w2PostHeadRetryEachQuorum(t, "seed baseline-certifier block", func() error {
		return database.Session().Query(`
			INSERT INTO blocks (org_id, block_id, representation_id, sha1, size_bytes, storage_class, storage_key, gc_state, gc_claim_id, created_at, last_accessed)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, orgID, blockID, dbpkg.PlainBlockRepresentationID, externalID, sizeBytes, storageClass, storageKey, "", "", now, now).Consistency(gocql.EachQuorum).Exec()
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
	h0, h1, h2 := "pc-d1b1-h0-"+uuid.NewString(), "pc-d1b1-h1-"+uuid.NewString(), "pc-d1b1-h2-"+uuid.NewString()
	rootFSID, fileFSID := "pc-d1b1-root-"+uuid.NewString(), "pc-d1b1-file-"+uuid.NewString()
	content := []byte("pc-d1b1 certified baseline")
	sha256Sum := sha256.Sum256(content)
	sha1Sum := sha1.Sum(content)
	blockID, externalID := hex.EncodeToString(sha256Sum[:]), hex.EncodeToString(sha1Sum[:])
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	sizeBytes := int64(len(content))
	storageManager, blockStore := newLibraryBaselineCertifierStorage(t, ctx, orgID, libraryBaselineCertifierMinIOEndpoint, true)
	p1, err := blockStore.MintStorageKey(blockID)
	if err != nil {
		t.Fatalf("mint evidence P1: %v", err)
	}
	p2, err := blockStore.MintStorageKey(blockID)
	if err != nil {
		t.Fatalf("mint evidence P2: %v", err)
	}
	seedLibraryBaselineCertifierLibrary(t, na, orgID, libraryID, ownerID, h0, sizeBytes)
	seedLibraryBaselineCertifierCommit(t, na, libraryID, h0, rootFSID)
	seedLibraryBaselineCertifierCommit(t, na, libraryID, h1, rootFSID)
	seedLibraryBaselineCertifierCommit(t, na, libraryID, h2, rootFSID)
	seedLibraryBaselineCertifierTree(t, na, orgID, libraryID, rootFSID, fileFSID, blockID, externalID, sizeBytes)
	seedLibraryBaselineCertifierBlock(t, na, orgID, blockID, externalID, "evidence", p1, sizeBytes)
	missingPhysical := na.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, h0)
	if missingPhysical.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || missingPhysical.Reason != dbpkg.LibraryBaselineReasonPhysicalBytesMissing || missingPhysical.PermanentLivenessWrites != 1 {
		t.Fatalf("missing physical-object certification = %+v", missingPhysical)
	}
	if _, certifiedHead := readLibraryBaselineCertifierWitness(t, na, orgID, libraryID); certifiedHead != nil {
		t.Fatalf("missing physical bytes created witness %q", *certifiedHead)
	}
	unavailableManager, _ := newLibraryBaselineCertifierStorage(t, ctx, orgID, "http://127.0.0.1:1", false)
	errorCtx, cancelError := context.WithTimeout(ctx, 5*time.Second)
	storageError := na.CertifyLibraryBaseline(errorCtx, unavailableManager, orgID, libraryID, h0)
	cancelError()
	if storageError.Outcome != dbpkg.LibraryBaselineCertificationUnknown || storageError.Reason != dbpkg.LibraryBaselineReasonPhysicalStorageUnavailable {
		t.Fatalf("unavailable physical storage certification = %+v", storageError)
	}
	if _, certifiedHead := readLibraryBaselineCertifierWitness(t, na, orgID, libraryID); certifiedHead != nil {
		t.Fatalf("unavailable physical storage created witness %q", *certifiedHead)
	}
	if _, err := blockStore.PutObjectAutoDirect(ctx, p1, content); err != nil {
		t.Fatalf("store exact P1 bytes in MinIO: %v", err)
	}
	certified := na.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, h0)
	if certified.Outcome != dbpkg.LibraryBaselineCertificationCertified || certified.Reason != dbpkg.LibraryBaselineReasonApplied {
		t.Fatalf("successful baseline certification = %+v", certified)
	}
	if certified.FSObjectsWalked != 2 || certified.UniqueBlocks != 1 || certified.PermanentLivenessWrites != 0 || certified.PhysicalRevalidations != 1 {
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
	retry := eu.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, h0)
	if retry.Outcome != dbpkg.LibraryBaselineCertificationCertified || retry.PermanentLivenessWrites != 0 {
		t.Fatalf("idempotent baseline retry = %+v", retry)
	}
	if err := v2pkg.NewFSHelper(na).UpdateLibraryHead(orgID, libraryID, h1, h0); err != nil {
		t.Fatalf("advance HEAD H0->H1: %v", err)
	}
	stale := asia.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, h0)
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
	pChangeHookRan := false
	pChanged := na.CertifyLibraryBaselineWithIntegrationHooks(ctx, storageManager, orgID, libraryID, h1, dbpkg.LibraryBaselineCertifierIntegrationHooks{
		AfterLiveness: func(hookCtx context.Context, hookOrgID, hookBlockID string, expected dbpkg.BlockPhysicalLocation) {
			if hookOrgID != orgID || hookBlockID != blockID || expected.StorageClass != "evidence" || expected.StorageKey != p1 {
				t.Fatalf("P-change hook observed org=%q block=%q expected=%+v", hookOrgID, hookBlockID, expected)
			}
			if hookErr := na.Session().Query(`UPDATE blocks SET storage_key = ? WHERE org_id = ? AND block_id = ?`, p2, orgID, blockID).WithContext(hookCtx).Consistency(gocql.EachQuorum).Exec(); hookErr != nil {
				t.Fatalf("race canonical P change after liveness: %v", hookErr)
			}
			pChangeHookRan = true
		},
	})
	if !pChangeHookRan || pChanged.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || pChanged.Reason != dbpkg.LibraryBaselineReasonPhysicalIncarnationChanged || pChanged.PhysicalRevalidations != 1 {
		t.Fatalf("end-to-end P-change race = hook=%t result=%+v", pChangeHookRan, pChanged)
	}
	settledHead, certifiedHead := readLibraryBaselineCertifierWitness(t, na, orgID, libraryID)
	if settledHead != h1 || certifiedHead == nil || *certifiedHead != h0 {
		t.Fatalf("P-change race altered the witness: head=%q certified=%v", settledHead, certifiedHead)
	}
	legacyKey := blockStore.StorageKeyForHash(blockID)
	if err := na.Session().Query(`UPDATE blocks SET storage_key = ? WHERE org_id = ? AND block_id = ?`, legacyKey, orgID, blockID).Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatalf("install legacy deterministic locator: %v", err)
	}
	legacy := na.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, h1)
	if legacy.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || legacy.Reason != dbpkg.LibraryBaselineReasonLegacyLocator {
		t.Fatalf("legacy locator certification = %+v", legacy)
	}
	if err := na.Session().Query(`UPDATE blocks SET storage_key = ? WHERE org_id = ? AND block_id = ?`, p2, orgID, blockID).Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatalf("restore minted P2 after legacy locator rejection: %v", err)
	}
	if _, err := blockStore.PutObjectAutoDirect(ctx, p2, content); err != nil {
		t.Fatalf("store exact P2 bytes in MinIO: %v", err)
	}
	fresh := eu.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, h1)
	if fresh.Outcome != dbpkg.LibraryBaselineCertificationCertified {
		t.Fatalf("fresh H1 certification after P change = %+v", fresh)
	}
	ambiguousNotAppliedErr := errors.New("injected ambiguous response after non-applied witness CAS")
	headAdvanceHookRan := false
	notApplied := na.CertifyLibraryBaselineWithIntegrationHooks(ctx, storageManager, orgID, libraryID, h1, dbpkg.LibraryBaselineCertifierIntegrationHooks{
		AfterLiveness: func(hookCtx context.Context, hookOrgID, hookBlockID string, expected dbpkg.BlockPhysicalLocation) {
			if hookOrgID != orgID || hookBlockID != blockID || expected.StorageKey != p2 {
				t.Fatalf("HEAD-advance hook observed org=%q block=%q expected=%+v", hookOrgID, hookBlockID, expected)
			}
			if hookErr := v2pkg.NewFSHelper(na).UpdateLibraryHead(orgID, libraryID, h2, h1); hookErr != nil {
				t.Fatalf("race HEAD H1->H2 before witness CAS: %v", hookErr)
			}
			headAdvanceHookRan = true
		},
		AfterWitnessCAS: func(cas dbpkg.LibraryContinuityCASResult, casErr error) (dbpkg.LibraryContinuityCASResult, error) {
			if casErr != nil || cas.Outcome != dbpkg.LibraryContinuityCASNotApplied {
				t.Errorf("real H1 witness CAS = %+v/%v, want NotApplied without transport error", cas, casErr)
			}
			return dbpkg.LibraryContinuityCASResult{Outcome: dbpkg.LibraryContinuityCASUnknown}, ambiguousNotAppliedErr
		},
	})
	if !headAdvanceHookRan || notApplied.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || notApplied.Reason != dbpkg.LibraryBaselineReasonHeadChanged || !errors.Is(notApplied.Diagnostic, ambiguousNotAppliedErr) {
		t.Fatalf("ambiguous non-applied CAS settlement = hook=%t result=%+v", headAdvanceHookRan, notApplied)
	}
	settledHead, certifiedHead = readLibraryBaselineCertifierWitness(t, na, orgID, libraryID)
	if settledHead != h2 || certifiedHead == nil || *certifiedHead != h1 {
		t.Fatalf("non-applied CAS changed the witness: head=%q certified=%v", settledHead, certifiedHead)
	}
	ambiguousAppliedErr := errors.New("injected ambiguous response after applied witness CAS")
	appliedHookRan := false
	settled := na.CertifyLibraryBaselineWithIntegrationHooks(ctx, storageManager, orgID, libraryID, h2, dbpkg.LibraryBaselineCertifierIntegrationHooks{
		AfterWitnessCAS: func(cas dbpkg.LibraryContinuityCASResult, casErr error) (dbpkg.LibraryContinuityCASResult, error) {
			if casErr != nil || cas.Outcome != dbpkg.LibraryContinuityCASApplied {
				t.Errorf("real H2 witness CAS = %+v/%v, want Applied without transport error", cas, casErr)
			}
			appliedHookRan = true
			return dbpkg.LibraryContinuityCASResult{Outcome: dbpkg.LibraryContinuityCASUnknown}, ambiguousAppliedErr
		},
	})
	if !appliedHookRan || settled.Outcome != dbpkg.LibraryBaselineCertificationCertified || settled.Reason != dbpkg.LibraryBaselineReasonWitnessSettled || !errors.Is(settled.Diagnostic, ambiguousAppliedErr) {
		t.Fatalf("ambiguous applied CAS settlement = hook=%t result=%+v", appliedHookRan, settled)
	}
	settledHead, certifiedHead = readLibraryBaselineCertifierWitness(t, na, orgID, libraryID)
	if settledHead != h2 || certifiedHead == nil || *certifiedHead != h2 {
		t.Fatalf("applied CAS settlement did not persist H2 witness: head=%q certified=%v", settledHead, certifiedHead)
	}
	claimID := uuid.NewString()
	if err := na.Session().Query(`UPDATE blocks SET gc_state = ?, gc_claim_id = ?, gc_claimed_at = ? WHERE org_id = ? AND block_id = ?`, dbpkg.BlockGCStateDeleting, claimID, time.Now().UTC(), orgID, blockID).Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatalf("install GC claim state: %v", err)
	}
	gcBlocked := na.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, h2)
	if gcBlocked.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || gcBlocked.Reason != dbpkg.LibraryBaselineReasonGCAuthorityConflict {
		t.Fatalf("GC-blocked certification = %+v", gcBlocked)
	}
	missingOrgID, missingLibraryID := uuid.NewString(), uuid.NewString()
	missingHead := "pc-d1b1-missing-" + uuid.NewString()
	seedLibraryBaselineCertifierLibrary(t, na, missingOrgID, missingLibraryID, uuid.NewString(), missingHead, 0)
	missing := na.CertifyLibraryBaseline(ctx, storageManager, missingOrgID, missingLibraryID, missingHead)
	if missing.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || missing.Reason != dbpkg.LibraryBaselineReasonMissingCommit {
		t.Fatalf("missing-commit certification = %+v", missing)
	}
	deletedOrgID, deletedLibraryID := uuid.NewString(), uuid.NewString()
	deletedHead, deletedRoot := "pc-d1b1-deleted-"+uuid.NewString(), "pc-d1b1-deleted-root-"+uuid.NewString()
	seedLibraryBaselineCertifierLibrary(t, na, deletedOrgID, deletedLibraryID, uuid.NewString(), deletedHead, 0)
	seedLibraryBaselineCertifierCommit(t, na, deletedLibraryID, deletedHead, deletedRoot)
	w2PostHeadRetryEachQuorum(t, "seed deleted baseline-certifier root", func() error {
		return na.Session().Query(`INSERT INTO fs_objects (library_id, fs_id, obj_type, dir_entries) VALUES (?, ?, ?, ?)`, deletedLibraryID, deletedRoot, "dir", "[]").Consistency(gocql.EachQuorum).Exec()
	})
	if err := na.Session().Query(`UPDATE libraries SET deleted_at = ? WHERE org_id = ? AND library_id = ?`, time.Now().UTC(), deletedOrgID, deletedLibraryID).Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatalf("soft-delete baseline-certifier library: %v", err)
	}
	deleted := asia.CertifyLibraryBaseline(ctx, storageManager, deletedOrgID, deletedLibraryID, deletedHead)
	if deleted.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || deleted.Reason != dbpkg.LibraryBaselineReasonLibraryDeleted {
		t.Fatalf("deleted-library certification = %+v", deleted)
	}
	libraryBaselineCertifierEvidence = true
	t.Logf("GREEN: full reachable tree certified with physical bytes at exact minted P, permanent EACH_QUORUM-visible liveness, idempotent retry, stale-HEAD and end-to-end P-change rejection, ambiguous applied/non-applied SERIAL settlement, legacy/GC rejection, and missing/deleted proof rejection")
}

func TestLibraryBaselineCertifierUnavailableDCEachQuorum(t *testing.T) {
	phase := strings.TrimSpace(os.Getenv(libraryBaselineCertifierUnavailableDCPhaseEnv))
	if phase == "" {
		t.Skipf("%s is not set", libraryBaselineCertifierUnavailableDCPhaseEnv)
	}
	if phase != "prepare" && phase != "verify" {
		t.Fatalf("%s=%q, want prepare or verify", libraryBaselineCertifierUnavailableDCPhaseEnv, phase)
	}
	if phase == "verify" && os.Getenv(libraryBaselineCertifierUnavailableDCEvidenceEnv) != "1" {
		t.Skipf("%s=1 is required for the verification phase", libraryBaselineCertifierUnavailableDCEvidenceEnv)
	}
	runID := strings.TrimSpace(os.Getenv(libraryBaselineCertifierUnavailableDCRunIDEnv))
	namespace, err := uuid.Parse(runID)
	if err != nil {
		t.Fatalf("%s must be a UUID: %v", libraryBaselineCertifierUnavailableDCRunIDEnv, err)
	}
	stableID := func(name string) string {
		return uuid.NewSHA1(namespace, []byte("pc-d1b1-unavailable-dc-"+name)).String()
	}
	orgID, libraryID, ownerID := stableID("org"), stableID("library"), stableID("owner")
	head, rootFSID, fileFSID := "pc-d1b1-unavailable-head-"+strings.ReplaceAll(runID, "-", ""), stableID("root"), stableID("file")
	content := []byte("pc-d1b1 EACH_QUORUM unavailable-datacenter evidence")
	sha256Sum := sha256.Sum256(content)
	sha1Sum := sha1.Sum(content)
	blockID, externalID := hex.EncodeToString(sha256Sum[:]), hex.EncodeToString(sha1Sum[:])
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	endpoints := w2PostHead3DCEndpoints(t)
	na := w2PostHead3DCConnectSerial(t, "dc-na", endpoints, "LOCAL_SERIAL")
	storageManager, blockStore := newLibraryBaselineCertifierStorage(t, ctx, orgID, libraryBaselineCertifierMinIOEndpoint, true)
	if phase == "prepare" {
		storageKey, err := blockStore.MintStorageKey(blockID)
		if err != nil {
			t.Fatalf("mint unavailable-DC evidence P: %v", err)
		}
		sizeBytes := int64(len(content))
		seedLibraryBaselineCertifierLibrary(t, na, orgID, libraryID, ownerID, head, sizeBytes)
		seedLibraryBaselineCertifierCommit(t, na, libraryID, head, rootFSID)
		seedLibraryBaselineCertifierTree(t, na, orgID, libraryID, rootFSID, fileFSID, blockID, externalID, sizeBytes)
		seedLibraryBaselineCertifierBlock(t, na, orgID, blockID, externalID, "evidence", storageKey, sizeBytes)
		if _, err := blockStore.PutObjectAutoDirect(ctx, storageKey, content); err != nil {
			t.Fatalf("store unavailable-DC evidence bytes: %v", err)
		}
		t.Logf("PREPARED: stable run %s has a complete baseline and bytes at exact P", runID)
		return
	}
	var storageClass, storageKey string
	if err := na.Session().Query(`SELECT storage_class, storage_key FROM blocks WHERE org_id = ? AND block_id = ?`, orgID, blockID).WithContext(ctx).Consistency(gocql.LocalQuorum).Scan(&storageClass, &storageKey); err != nil {
		t.Fatalf("read prepared physical metadata from dc-na: %v", err)
	}
	if storageClass != "evidence" || strings.TrimSpace(storageKey) == "" {
		t.Fatalf("prepared physical placement = %q/%q", storageClass, storageKey)
	}
	physicalExists, err := blockStore.ObjectExists(ctx, storageKey)
	if err != nil || !physicalExists {
		t.Fatalf("prepared bytes at exact P %s/%s exist=%t err=%v", storageClass, storageKey, physicalExists, err)
	}
	beforeHead, beforeCertified := readLibraryBaselineCertifierWitness(t, na, orgID, libraryID)
	if beforeHead != head || beforeCertified != nil {
		t.Fatalf("prepared witness before verify = head=%q certified=%v", beforeHead, beforeCertified)
	}
	result := na.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, head)
	if result.Outcome != dbpkg.LibraryBaselineCertificationUnknown || result.Reason != dbpkg.LibraryBaselineReasonLivenessReadFailed {
		t.Fatalf("certification with dc-asia unavailable = %+v, want UNKNOWN/liveness_read_failed from EACH_QUORUM", result)
	}
	if result.CommitsWalked != 1 || result.FSObjectsWalked != 2 || result.UniqueBlocks != 1 || result.PermanentLivenessWrites != 0 || result.PhysicalRevalidations != 0 {
		t.Fatalf("unavailable-DC proof did not reach the EACH_QUORUM liveness read cleanly: %+v", result)
	}
	afterHead, afterCertified := readLibraryBaselineCertifierWitness(t, na, orgID, libraryID)
	if afterHead != head || afterCertified != nil {
		t.Fatalf("unavailable-DC attempt created/changed a witness: head=%q certified=%v", afterHead, afterCertified)
	}
	libraryBaselineCertifierUnavailableDCEvidence = true
	t.Logf("GREEN: dc-asia stopped; exact bytes exist at captured P; EACH_QUORUM liveness read failed closed with no witness")
}
