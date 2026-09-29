//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const w2UploadFileExactPEnv = "SESAMEFS_REQUIRE_W2_UPLOADFILE_EXACT_P_EVIDENCE"

// w2UploadFileExactPEvidenceState records each W2-6 leg by name; completeness
// is the conjunction of the fields, never a counter.
type w2UploadFileExactPEvidenceState struct {
	writerFirst               bool
	gcCommittedBeforeStage    bool
	gcFullyRetiredBeforeStage bool
}

func (state w2UploadFileExactPEvidenceState) missing() []string {
	var missing []string
	for _, leg := range []struct {
		name string
		seen bool
	}{
		{"writerFirst", state.writerFirst},
		{"gcCommittedBeforeStage", state.gcCommittedBeforeStage},
		{"gcFullyRetiredBeforeStage", state.gcFullyRetiredBeforeStage},
	} {
		if !leg.seen {
			missing = append(missing, leg.name)
		}
	}
	return missing
}

var w2UploadFileExactPEvidence w2UploadFileExactPEvidenceState

// TestW2UploadFileExactPlacementBeforeHead is the W2-6 evidence
// (docs/X1-CRITICAL-PATH.md §4). UploadFile materializes its block under its
// own up:<operation> reference and then publishes through the shared
// finalizer, which stages pub: at LOCAL_QUORUM before HEAD. The legs model the
// R3 sequence "up -> fence clear -> stall past TTL": the own up: lapses and GC
// wins before pub: is staged. HEAD must not publish against the retired exact
// placement.
func TestW2UploadFileExactPlacementBeforeHead(t *testing.T) {
	requireCassandra(t)
	if os.Getenv(w2UploadFileExactPEnv) == "1" {
		t.Cleanup(func() {
			if t.Skipped() {
				t.Errorf("%s=1 requires real Cassandra+MinIO W2-6 evidence, but the test skipped", w2UploadFileExactPEnv)
			} else if missing := w2UploadFileExactPEvidence.missing(); !t.Failed() && len(missing) > 0 {
				t.Errorf("%s=1 incomplete W2-6 evidence; missing=%s", w2UploadFileExactPEnv, strings.Join(missing, ","))
			}
		})
	}
	database := shareProjectionDBForTest(t)
	store := gcpkg.NewCassandraStore(database)
	storageClass := x1StorageClass(t)
	handler := newBorrowedFSHeadHandler(t, database, storageClass)
	gin.SetMode(gin.TestMode)

	t.Run("writerFirst", func(t *testing.T) {
		fx := newW2UploadFileFixture(t, database, handler)
		rec := fx.upload(t)
		if rec.Code != http.StatusOK {
			t.Fatalf("writerFirst: upload status=%d body=%s; want 200", rec.Code, rec.Body.String())
		}
		fx.assertHeadAdvanced(t)
		w2UploadFileExactPEvidence.writerFirst = true
	})

	t.Run("gcCommittedBeforeStage", func(t *testing.T) {
		fx := newW2UploadFileFixture(t, database, handler)
		var attempt gcpkg.BlockDeleteAuthority
		fx.afterMaterialized(t, func() {
			target := fx.readTarget(t)
			fx.dropOwnUploadRefs(t)
			attempt = x1CommitHandoffAfterZeroRefs(t, store, fx.orgUUID, fx.blockID, x1Attempt(target, "w2-uploadfile-gc-committed"))
		})
		rec := fx.upload(t)
		if rec.Code != http.StatusConflict {
			t.Fatalf("gcCommittedBeforeStage: upload status=%d body=%s; want 409 (HEAD must not publish against a committed D)", rec.Code, rec.Body.String())
		}
		fx.assertHeadUnchanged(t)
		fx.assertDUnrevoked(t, attempt)
		if fx.hasOwnFSReferrer(t) {
			t.Fatal("gcCommittedBeforeStage: unexpected fs: after rejected publication")
		}
		fx.assertPubCount(t, 0, "gcCommittedBeforeStage: rejected publication must drop staged pub:")
		w2UploadFileExactPEvidence.gcCommittedBeforeStage = true
	})

	t.Run("gcFullyRetiredBeforeStage", func(t *testing.T) {
		fx := newW2UploadFileFixture(t, database, handler)
		blockStore := newVerificationBlockStore(t, fx.orgID)
		fx.afterMaterialized(t, func() {
			target := fx.readTarget(t)
			fx.dropOwnUploadRefs(t)
			attempt := x1CommitHandoffAfterZeroRefs(t, store, fx.orgUUID, fx.blockID, x1Attempt(target, "w2-uploadfile-fully-retired"))
			committed := gcpkg.CommittedBlockDeleteAuthorityForTest(attempt)
			publication := store.StartBlockDeleteOrphan(fx.orgUUID, fx.blockID, committed, fx.sha1ID, time.Now().UTC())
			if publication.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
				t.Fatalf("gcFullyRetiredBeforeStage: StartBlockDeleteOrphan = %s, %v", publication.Outcome, publication.Cause)
			}
			finalized, err := store.FinalizeBlockDelete(fx.orgUUID, fx.blockID, committed)
			if err != nil || finalized.Outcome != gcpkg.BlockDeleteFinalized {
				t.Fatalf("gcFullyRetiredBeforeStage: FinalizeBlockDelete = %s, %v", finalized.Outcome, err)
			}
			if err := blockStore.DeleteBlockByStorageKey(context.Background(), target.StorageKey); err != nil {
				t.Fatalf("gcFullyRetiredBeforeStage: physical delete: %v", err)
			}
			if _, err := store.TerminateBlockDeleteLifecycle(fx.orgUUID, fx.blockID, committed); err != nil {
				t.Fatalf("gcFullyRetiredBeforeStage: TerminateBlockDeleteLifecycle: %v", err)
			}
			if err := store.DeleteS3Orphan(fx.orgUUID, fx.blockID, committed.Authority(), publication.FirstSeenAt); err != nil {
				t.Fatalf("gcFullyRetiredBeforeStage: DeleteS3Orphan: %v", err)
			}
			x1AssertCanonicalAbsent(t, store, fx.orgUUID, fx.blockID)
		})
		rec := fx.upload(t)
		if rec.Code != http.StatusConflict {
			t.Fatalf("gcFullyRetiredBeforeStage: upload status=%d body=%s; want 409 (HEAD must not publish against a retired placement)", rec.Code, rec.Body.String())
		}
		fx.assertHeadUnchanged(t)
		if fx.hasOwnFSReferrer(t) {
			t.Fatal("gcFullyRetiredBeforeStage: unexpected fs: after rejected publication")
		}
		fx.assertPubCount(t, 0, "gcFullyRetiredBeforeStage: rejected publication must drop staged pub:")
		w2UploadFileExactPEvidence.gcFullyRetiredBeforeStage = true
	})
}

type w2UploadFileFixture struct {
	*borrowedFSHeadFixture
}

func newW2UploadFileFixture(t *testing.T, database *dbpkg.DB, handler *v2pkg.FileHandler) *w2UploadFileFixture {
	t.Helper()
	repoID := createTestLibrary(t, adminClient, fmt.Sprintf("inttest-w2up-%d", time.Now().UnixNano()))
	var orgID, ownerID string
	if err := database.Session().Query(`SELECT org_id, owner_id FROM libraries_by_id WHERE library_id = ?`, repoID).Scan(&orgID, &ownerID); err != nil {
		t.Fatalf("resolve org/owner for %s: %v", repoID, err)
	}
	orgUUID, err := uuid.Parse(orgID)
	if err != nil {
		t.Fatalf("parse org_id %q: %v", orgID, err)
	}
	content := []byte("w2-uploadfile-exact-p-" + uuid.NewString())
	blockID := sha256hex(content)
	sha1ID := sha1hex(content)
	inner := &borrowedFSHeadFixture{
		database: database, handler: handler,
		repoID: repoID, orgID: orgID, orgUUID: orgUUID,
		userID: ownerID, blockID: blockID, sha1ID: sha1ID, content: content,
		filename: "w2-uploadfile-" + uuid.NewString()[:8] + ".txt",
	}
	inner.headBefore = borrowedFSReadHead(t, database, orgID, repoID)
	if inner.headBefore == "" {
		t.Fatal("library has empty HEAD before upload")
	}
	x1Cleanup(t, database, orgUUID, blockID)
	// Cleanup callbacks run in LIFO order. Register the exact-object cleanup
	// after x1Cleanup so it runs first, while blocks.storage_key is still present.
	cleanupUploadedBlockArtifactsForTest(t, orgID, repoID, blockID, sha1ID)
	return &w2UploadFileFixture{borrowedFSHeadFixture: inner}
}

func (fx *w2UploadFileFixture) afterMaterialized(t *testing.T, fn func()) {
	t.Helper()
	t.Cleanup(v2pkg.SetUploadFileAfterMaterializedBarrierForTest(fx.repoID, fn))
}

// readTarget returns the exact placement UploadFile just installed.
func (fx *w2UploadFileFixture) readTarget(t *testing.T) gcpkg.BlockDeleteTarget {
	t.Helper()
	var storageClass, storageKey string
	if err := fx.database.Session().Query(
		`SELECT storage_class, storage_key FROM blocks WHERE org_id = ? AND block_id = ?`,
		fx.orgID, fx.blockID).Scan(&storageClass, &storageKey); err != nil {
		t.Fatalf("read materialized placement %s/%s: %v", fx.orgID, fx.blockID, err)
	}
	return x1Target(storageClass, storageKey)
}

// dropOwnUploadRefs models the request's own up:<operation> reference having
// lapsed by TTL before GC's zero-proof read (the R3 "stall past TTL" case).
func (fx *w2UploadFileFixture) dropOwnUploadRefs(t *testing.T) {
	t.Helper()
	referrers, err := fx.database.ListBlockReferrers(fx.orgID, fx.blockID)
	if err != nil {
		t.Fatalf("ListBlockReferrers: %v", err)
	}
	dropped := 0
	for _, referrer := range referrers {
		if !strings.HasPrefix(referrer, "up:") {
			continue
		}
		if err := fx.database.RemoveBlockReference(fx.orgID, fx.blockID, referrer); err != nil {
			t.Fatalf("drop own upload ref %s: %v", referrer, err)
		}
		dropped++
	}
	if dropped != 1 {
		t.Fatalf("dropped %d up: references, want exactly the request's own up:<operation>", dropped)
	}
}

func (fx *w2UploadFileFixture) upload(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("parent_dir", "/"); err != nil {
		t.Fatalf("write parent_dir: %v", err)
	}
	part, err := writer.CreateFormFile("file", fx.filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(fx.content); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/repos/"+fx.repoID+"/upload/", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	c.Request = req
	c.Params = gin.Params{{Key: "repo_id", Value: fx.repoID}}
	c.Set("org_id", fx.orgID)
	c.Set("user_id", fx.userID)
	fx.handler.UploadFile(c)
	return rec
}

func TestW2UploadFileExactPEvidenceRequiresEveryNamedLeg(t *testing.T) {
	partial := w2UploadFileExactPEvidenceState{writerFirst: true, gcCommittedBeforeStage: true}
	if got := strings.Join(partial.missing(), ","); got != "gcFullyRetiredBeforeStage" {
		t.Fatalf("missing() must name the absent leg individually, got %q", got)
	}
	full := w2UploadFileExactPEvidenceState{writerFirst: true, gcCommittedBeforeStage: true, gcFullyRetiredBeforeStage: true}
	if missing := full.missing(); len(missing) != 0 {
		t.Fatalf("all 3 named legs should satisfy the gate; missing=%v", missing)
	}
	if missing := (w2UploadFileExactPEvidenceState{}).missing(); len(missing) != 3 {
		t.Fatalf("an empty run must report all 3 legs missing, got %v", missing)
	}
}
