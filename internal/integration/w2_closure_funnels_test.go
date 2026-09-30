//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	apipkg "github.com/Sesame-Disk/sesamefs/internal/api"
	"github.com/Sesame-Disk/sesamefs/internal/config"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var w2ClosureFunnels = []string{"Office", "UploadFile", "BorrowedFS", "SessionUpload", "SyncDirect", "SyncMerge"}

func w2ClosureFixture(t *testing.T, funnel string) (*w2CreateFileFixture, w2ChildFixture) {
	t.Helper()
	database := shareProjectionDBForTest(t)
	class := x1StorageClass(t)
	var fx *w2CreateFileFixture
	targetCommit := ""
	switch funnel {
	case "Office", "UploadFile":
		fx = w2RepairFixture(t)
	case "BorrowedFS", "SessionUpload":
		base := newBorrowedFSHeadFixture(t, database, newBorrowedFSHeadHandler(t, database, class), class)
		fx = &w2CreateFileFixture{w2UploadFileFixture: &w2UploadFileFixture{borrowedFSHeadFixture: base}}
		if funnel == "SessionUpload" {
			base.pinSessionUpload(t)
		}
		if funnel == "SessionUpload" {
			base.dropForeignFS(t)
		}
	case "SyncDirect", "SyncMerge":
		repo := createTestLibrary(t, adminClient, fmt.Sprintf("inttest-w2closure-sync-%d", time.Now().UnixNano()))
		org := resolveOrgID(t, repo)
		orgUUID, err := uuid.Parse(org)
		if err != nil {
			t.Fatal(err)
		}
		initial := readLibrarySyncHeadState(t, database.Session(), repo)
		content := []byte("w2-closure-sync-" + uuid.NewString())
		fc := syncW2PutFileCommit(t, adminClient, repo, initial.HeadCommitID, "remote.txt", content)
		targetCommit = fc.commitID
		var owner string
		if err := database.Session().Query(`SELECT owner_id FROM libraries WHERE org_id = ? AND library_id = ?`, org, repo).Scan(&owner); err != nil {
			t.Fatal(err)
		}
		base := &borrowedFSHeadFixture{database: database, handler: newBorrowedFSHeadHandler(t, database, class), repoID: repo, orgID: org, orgUUID: orgUUID, userID: owner, content: content, blockID: fc.internalBlockID, sha1ID: fc.externalBlockID, filename: "local.txt", headBefore: initial.HeadCommitID}
		fx = &w2CreateFileFixture{w2UploadFileFixture: &w2UploadFileFixture{borrowedFSHeadFixture: base}}
		fx.target = fx.readTarget(t)
		x1Cleanup(t, database, orgUUID, fx.blockID)
		cleanupUploadedBlockArtifactsForTest(t, org, repo, fx.blockID, fx.sha1ID)
		key := fx.target.StorageKey
		t.Cleanup(func() {
			if err := newVerificationBlockStore(t, org).DeleteBlockByStorageKey(context.Background(), key); err != nil {
				t.Errorf("owned object cleanup: %v", err)
			}
		})
		if funnel == "SyncMerge" {
			if rec := fx.create(t); rec.Code != http.StatusCreated {
				t.Fatalf("local divergent HEAD: %d %s", rec.Code, rec.Body.String())
			}
			fx.headBefore = borrowedFSReadHead(t, database, org, repo)
		}
	default:
		t.Fatalf("unknown funnel %s", funnel)
	}
	t.Cleanup(func() {
		for b := 0; b < dbpkg.PublishedBlockReferenceRepairBuckets; b++ {
			if err := database.Session().Query(`DELETE FROM published_block_reference_repairs WHERE bucket = ? AND org_id = ? AND repo_id = ?`, b, fx.orgID, fx.repoID).Exec(); err != nil {
				t.Errorf("owned repair cleanup: %v", err)
			}
		}
	})
	data := w2ChildFixture{Org: fx.orgID, Repo: fx.repoID, User: fx.userID, Head: fx.headBefore, Filename: fx.filename, Class: class, Funnel: funnel, TargetCommit: targetCommit, Content: fx.content, Block: fx.blockID, SHA1: fx.sha1ID, Session: fx.sessionID}
	return fx, data
}
func w2InvokeWriter(t *testing.T, fx *w2CreateFileFixture, data w2ChildFixture, database *dbpkg.DB) *httptest.ResponseRecorder {
	t.Helper()
	fx.handler = newBorrowedFSHeadHandler(t, database, data.Class)
	switch data.Funnel {
	case "Office":
		return fx.create(t)
	case "UploadFile":
		return fx.upload(t)
	case "BorrowedFS", "SessionUpload":
		return fx.commit(t)
	case "SyncDirect", "SyncMerge":
		manager := storage.NewManager()
		s3 := newVerificationS3Store(t)
		manager.SetDefaultClass(data.Class)
		manager.RegisterBackend(data.Class, s3, "")
		handler := apipkg.NewSyncHandler(database, s3, manager, &config.Config{Storage: config.StorageConfig{DefaultClass: data.Class}}, nil)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/seafhttp/repo/"+data.Repo+"/update-branch?head="+data.TargetCommit, nil)
		c.Params = gin.Params{{Key: "repo_id", Value: data.Repo}}
		c.Set("org_id", data.Org)
		c.Set("user_id", data.User)
		handler.UpdateBranch(c)
		return rec
	default:
		t.Fatalf("unknown writer %s", data.Funnel)
		return nil
	}
}
