//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/Sesame-Disk/sesamefs/internal/templates"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const w2CreateFileExactPEnv = "SESAMEFS_REQUIRE_W2_CREATEFILE_EXACT_P_EVIDENCE"

var w2CreateFileExactPEvidence = map[string]bool{}

func w2CreateFileExactPMissing(observed map[string]bool) []string {
	var missing []string
	for _, ext := range []string{"docx", "xlsx", "pptx"} {
		for _, leg := range []string{"writerFirst", "gcCommittedBeforeStage", "gcFullyRetiredBeforeStage"} {
			name := ext + "/" + leg
			if !observed[name] {
				missing = append(missing, name)
			}
		}
	}
	if !observed["emptyFile"] {
		missing = append(missing, "emptyFile")
	}
	return missing
}

// Real production CreateFile, real Cassandra authority and physical MinIO objects.
// Removing the request's own up: models its 48h TTL lapsing, without a two-day wait.
func TestW2CreateFileOfficeTemplateExactPlacementBeforeHead(t *testing.T) {
	if os.Getenv(w2CreateFileExactPEnv) == "1" {
		t.Cleanup(func() {
			if t.Skipped() {
				t.Errorf("%s=1 requires real Cassandra/MinIO evidence; test skipped", w2CreateFileExactPEnv)
			}
		})
	}
	requireCassandra(t)
	database := shareProjectionDBForTest(t)
	store := gcpkg.NewCassandraStore(database)
	handler := newBorrowedFSHeadHandler(t, database, x1StorageClass(t))
	gin.SetMode(gin.TestMode)
	for _, ext := range []string{"docx", "xlsx", "pptx"} {
		t.Run(ext, func(t *testing.T) {
			for _, leg := range []string{"writerFirst", "gcCommittedBeforeStage", "gcFullyRetiredBeforeStage"} {
				t.Run(leg, func(t *testing.T) {
					fx := newW2CreateFileFixture(t, database, handler, "."+ext)
					var attempt gcpkg.BlockDeleteAuthority
					materialized, staged := 0, 0
					t.Cleanup(v2pkg.SetCreateFileAfterMaterializedBarrierForTest(fx.repoID, func() {
						materialized++
						target := fx.readTarget(t)
						if materialized > 1 && target != fx.target {
							t.Fatalf("template reuse changed exact placement: %+v -> %+v", fx.target, target)
						}
						fx.target = target
						refs, err := database.ListBlockReferrers(fx.orgID, fx.blockID)
						if err != nil {
							t.Fatal(err)
						}
						for _, ref := range refs {
							if strings.HasPrefix(ref, "up:") {
								fx.uploadRefs = append(fx.uploadRefs, ref)
							}
						}
						if borrowedFSCountPrefix(t, database, fx.orgID, fx.blockID, "up:") != 1 {
							t.Fatal("materialization must register exactly the request's own up:")
						}
						if leg == "writerFirst" {
							return
						}
						fx.dropOwnUploadRefs(t)
						attempt = x1CommitHandoffAfterZeroRefs(t, store, fx.orgUUID, fx.blockID, x1Attempt(target, "w2-createfile-"+leg))
						if leg != "gcFullyRetiredBeforeStage" {
							return
						}
						committed := gcpkg.CommittedBlockDeleteAuthorityForTest(attempt)
						publication := store.StartBlockDeleteOrphan(fx.orgUUID, fx.blockID, committed, fx.sha1ID, time.Now().UTC())
						if publication.Outcome != gcpkg.StartBlockDeleteOrphanCreated {
							t.Fatalf("StartBlockDeleteOrphan = %s, %v", publication.Outcome, publication.Cause)
						}
						finalized, err := store.FinalizeBlockDelete(fx.orgUUID, fx.blockID, committed)
						if err != nil || finalized.Outcome != gcpkg.BlockDeleteFinalized {
							t.Fatalf("FinalizeBlockDelete = %s, %v", finalized.Outcome, err)
						}
						blockStore := newVerificationBlockStore(t, fx.orgID)
						if err := blockStore.DeleteBlockByStorageKey(context.Background(), target.StorageKey); err != nil {
							t.Fatalf("physical delete: %v", err)
						}
						if exists, err := blockStore.ObjectExists(context.Background(), target.StorageKey); err != nil || exists {
							t.Fatalf("retired exact object still exists: exists=%v err=%v", exists, err)
						}
						if _, err := store.TerminateBlockDeleteLifecycle(fx.orgUUID, fx.blockID, committed); err != nil {
							t.Fatalf("TerminateBlockDeleteLifecycle: %v", err)
						}
						if err := store.DeleteS3Orphan(fx.orgUUID, fx.blockID, committed.Authority(), publication.FirstSeenAt); err != nil {
							t.Fatalf("DeleteS3Orphan: %v", err)
						}
						x1AssertCanonicalAbsent(t, store, fx.orgUUID, fx.blockID)
						fenced, err := database.BlockDeleteFenceActive(fx.orgID, fx.blockID)
						if err != nil || fenced {
							t.Fatalf("fully retired P has no fence left: fenced=%v err=%v", fenced, err)
						}
					}))
					t.Cleanup(v2pkg.SetFileFromBlocksPublicationBarriersForTest(fx.repoID, nil, nil, func() {
						staged++
						fx.assertPubCount(t, 1, "Office-template pub: must be durable before exact-P/HEAD")
					}, nil))
					rec := fx.create(t)
					if leg == "writerFirst" {
						if rec.Code != http.StatusCreated {
							t.Fatalf("writerFirst: create status=%d body=%s; want 201", rec.Code, rec.Body.String())
						}
						fx.assertHeadAdvanced(t)
						data, err := newVerificationBlockStore(t, fx.orgID).GetBlockByStorageKey(context.Background(), fx.target.StorageKey)
						if err != nil || !bytes.Equal(data, fx.content) {
							t.Fatalf("published exact object bytes: err=%v", err)
						}
						if !fx.hasOwnFSReferrer(t) {
							t.Fatal("published Office template must have its permanent fs: reference")
						}
						// A second file uses the same content-addressed template. Remove
						// the first request's up: to distinguish the new operation's pin.
						fx.dropOwnUploadRefs(t)
						fx.filename = "reused" + "." + ext
						if rec := fx.create(t); rec.Code != http.StatusCreated {
							t.Fatalf("reuse: status=%d body=%s; want 201", rec.Code, rec.Body.String())
						}
					} else {
						if rec.Code != http.StatusConflict {
							t.Errorf("%s: create status=%d body=%s; want 409", leg, rec.Code, rec.Body.String())
						}
						fx.assertHeadUnchanged(t)
						if fx.hasOwnFSReferrer(t) {
							t.Fatal("rejected publication left a permanent fs:")
						}
						if leg == "gcCommittedBeforeStage" {
							fx.assertDUnrevoked(t, attempt)
							x1AssertCanonicalPresent(t, store, fx.orgUUID, fx.blockID, fx.target.StorageKey)
						} else {
							x1AssertCanonicalAbsent(t, store, fx.orgUUID, fx.blockID)
						}
					}
					fx.assertPubCount(t, 0, "settled create must drop pub:")
					if materialized == 0 || staged != materialized {
						t.Fatalf("missing ordering evidence: materialized=%d staged=%d", materialized, staged)
					}
					if !t.Failed() {
						w2CreateFileExactPEvidence[ext+"/"+leg] = true
					}
				})
			}
		})
	}
	t.Run("emptyFile", func(t *testing.T) {
		fx := newW2CreateFileFixture(t, database, handler, ".txt")
		t.Cleanup(v2pkg.SetCreateFileAfterMaterializedBarrierForTest(fx.repoID, func() { t.Fatal("empty CreateFile must not materialize a block") }))
		rec := fx.create(t)
		if rec.Code != http.StatusCreated {
			t.Fatalf("emptyFile: status=%d body=%s; want 201", rec.Code, rec.Body.String())
		}
		var result struct {
			ID   string `json:"id"`
			Size int64  `json:"size"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		var blocks []string
		if err := database.Session().Query(`SELECT block_ids FROM fs_objects WHERE library_id = ? AND fs_id = ?`, fx.repoID, result.ID).Scan(&blocks); err != nil {
			t.Fatal(err)
		}
		if result.Size != 0 || len(blocks) != 0 {
			t.Fatalf("empty file has size=%d blocks=%v", result.Size, blocks)
		}
		fx.assertHeadAdvanced(t)
		w2CreateFileExactPEvidence["emptyFile"] = true
	})
}

// Each fixture owns a new organization: shared Office-template hashes must
// never let the simulated GC delete another library's object or metadata.
func newW2CreateFileFixture(t *testing.T, database *dbpkg.DB, handler *v2pkg.FileHandler, ext string) *w2CreateFileFixture {
	t.Helper()
	orgUUID, ownerID := uuid.New(), uuid.NewString()
	orgID := orgUUID.String()
	repoID := h1SeedLibrary(t, database, orgID, ownerID, "inttest-w2createfile-"+uuid.NewString())
	t.Cleanup(func() {
		batch := database.Session().Batch(gocql.LoggedBatch)
		if err := dbpkg.AddDeleteAdminLibraryReadModelQueries(database.Session(), batch, orgID, repoID); err != nil {
			t.Errorf("fixture library projection teardown: %v", err)
		} else if err := batch.Exec(); err != nil {
			t.Errorf("fixture library projection teardown batch: %v", err)
		}
		for _, query := range []string{
			`DELETE FROM fs_objects WHERE library_id = ?`,
			`DELETE FROM commits WHERE library_id = ?`,
			`DELETE FROM libraries_by_id WHERE library_id = ?`,
		} {
			if err := database.Session().Query(query, repoID).Exec(); err != nil {
				t.Errorf("fixture teardown: %v", err)
			}
		}
		if err := database.Session().Query(`DELETE FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, repoID).Exec(); err != nil {
			t.Errorf("fixture library teardown: %v", err)
		}
	})
	if err := v2pkg.NewFSHelper(database).InitializeLibraryFS(orgID, repoID, ownerID, "W2-6a"); err != nil {
		t.Fatalf("initialize isolated library: %v", err)
	}
	content, err := templates.GetTemplateForExtension(ext)
	if err != nil {
		t.Fatalf("get Office template: %v", err)
	}
	inner := &borrowedFSHeadFixture{
		database: database, handler: handler, repoID: repoID, orgID: orgID,
		orgUUID: orgUUID, userID: ownerID, content: content,
		blockID: sha256hex(content), sha1ID: sha1hex(content),
		filename: "new" + ext,
	}
	inner.headBefore = borrowedFSReadHead(t, database, orgID, repoID)
	fx := &w2CreateFileFixture{w2UploadFileFixture: &w2UploadFileFixture{borrowedFSHeadFixture: inner}}
	if len(content) > 0 {
		x1Cleanup(t, database, orgUUID, inner.blockID)
		cleanupUploadedBlockArtifactsForTest(t, orgID, repoID, inner.blockID, inner.sha1ID)
		blockStore := newVerificationBlockStore(t, orgID)
		t.Cleanup(func() {
			// Captured before GC can remove the row, so physical cleanup also
			// works when the fully-retired leg (or a RED run) erased metadata.
			if inner.target.StorageKey != "" {
				if err := blockStore.DeleteBlockByStorageKey(context.Background(), inner.target.StorageKey); err != nil {
					t.Errorf("fixture object teardown: %v", err)
				}
			}
			refs, err := database.ListBlockReferrers(orgID, inner.blockID)
			if err != nil {
				t.Errorf("fixture refs teardown: %v", err)
			}
			refs = append(refs, fx.uploadRefs...)
			for _, ref := range refs {
				if err := database.DeleteProvisionalBlockReferenceExpiry(orgID, inner.blockID, ref, time.Time{}); err != nil {
					t.Errorf("fixture expiry teardown: %v", err)
				}
			}
		})
	}
	return fx
}

type w2CreateFileFixture struct {
	*w2UploadFileFixture
	uploadRefs []string
}

func (fx *w2CreateFileFixture) create(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api2/repos/%s/file/?p=/%s", fx.repoID, fx.filename), nil)
	c.Params = gin.Params{{Key: "repo_id", Value: fx.repoID}}
	c.Set("org_id", fx.orgID)
	c.Set("user_id", fx.userID)
	fx.handler.CreateFile(c)
	return rec
}

func TestW2CreateFileExactPEvidenceRequiresEveryNamedLeg(t *testing.T) {
	observed := map[string]bool{}
	if len(w2CreateFileExactPMissing(observed)) != 10 {
		t.Fatal("empty evidence must miss all nine Office legs and emptyFile")
	}
	for _, leg := range w2CreateFileExactPMissing(observed) {
		observed[leg] = true
	}
	if len(w2CreateFileExactPMissing(observed)) != 0 {
		t.Fatal("full evidence is incomplete")
	}
	delete(observed, "pptx/gcFullyRetiredBeforeStage")
	if got := strings.Join(w2CreateFileExactPMissing(observed), ","); got != "pptx/gcFullyRetiredBeforeStage" {
		t.Fatalf("missing named leg = %q", got)
	}
}
