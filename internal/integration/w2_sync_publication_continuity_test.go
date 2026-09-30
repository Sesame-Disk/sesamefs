//go:build integration

package integration

import (
	"context"
	"fmt"
	apipkg "github.com/Sesame-Disk/sesamefs/internal/api"
	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	"github.com/Sesame-Disk/sesamefs/internal/config"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestW2SyncPublicationContinuityAfterAuthority(t *testing.T) {
	requireCassandra(t)
	database := shareProjectionDBForTest(t)
	for _, scenario := range []struct{ merge, beforeRepair bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		merge, beforeRepair := scenario.merge, scenario.beforeRepair
		name := "direct"
		if merge {
			name = "autoMerge"
		}
		if beforeRepair {
			name += "BeforeRepair"
		}
		t.Run(name, func(t *testing.T) {
			w2ObservePublicationEvidence(t)
			repo := createTestLibrary(t, adminClient, fmt.Sprintf("inttest-w2continuity-sync-%d", time.Now().UnixNano()))
			org := resolveOrgID(t, repo)
			orgUUID, err := uuid.Parse(org)
			if err != nil {
				t.Fatal(err)
			}
			initial := readLibrarySyncHeadState(t, database.Session(), repo)
			content := []byte("sync-continuity-" + uuid.NewString())
			fc := syncW2PutFileCommit(t, adminClient, repo, initial.HeadCommitID, "remote.txt", content)
			var owner string
			if err := database.Session().Query(`SELECT owner_id FROM libraries WHERE org_id = ? AND library_id = ?`, org, repo).Scan(&owner); err != nil {
				t.Fatal(err)
			}
			fx := &w2CreateFileFixture{w2UploadFileFixture: &w2UploadFileFixture{borrowedFSHeadFixture: &borrowedFSHeadFixture{
				database: database, handler: newBorrowedFSHeadHandler(t, database, x1StorageClass(t)), repoID: repo, orgID: org, orgUUID: orgUUID, userID: owner,
				content: content, blockID: fc.internalBlockID, sha1ID: fc.externalBlockID, filename: "local.txt", headBefore: initial.HeadCommitID,
			}}}
			fx.target = fx.readTarget(t)
			x1Cleanup(t, database, orgUUID, fx.blockID)
			cleanupUploadedBlockArtifactsForTest(t, org, repo, fx.blockID, fx.sha1ID)
			key := fx.target.StorageKey
			t.Cleanup(func() {
				if err := newVerificationBlockStore(t, org).DeleteBlockByStorageKey(context.Background(), key); err != nil {
					t.Errorf("object cleanup: %v", err)
				}
			})
			t.Cleanup(func() {
				for b := 0; b < 32; b++ {
					if err := database.Session().Query(`DELETE FROM published_block_reference_repairs WHERE bucket = ? AND org_id = ? AND repo_id = ?`, b, org, repo).Exec(); err != nil {
						t.Errorf("owned Sync repair cleanup: %v", err)
					}
				}
			})
			if merge {
				if rec := fx.create(t); rec.Code != http.StatusCreated {
					t.Fatalf("local divergent HEAD: %d %s", rec.Code, rec.Body.String())
				}
				fx.headBefore = borrowedFSReadHead(t, database, org, repo)
			}
			manager := storage.NewManager()
			class := x1StorageClass(t)
			s3 := newVerificationS3Store(t)
			manager.SetDefaultClass(class)
			manager.RegisterBackend(class, s3, "")
			handler := apipkg.NewSyncHandler(database, s3, manager, &config.Config{Storage: config.StorageConfig{DefaultClass: class}}, nil)
			visited, protected := false, false
			pause := func() { visited = true; protected = w2TryDeleteAfterExpiry(t, fx) }
			if beforeRepair {
				t.Cleanup(v2pkg.SetW2PublicationBeforeRepairForTest(repo, pause))
			} else {
				t.Cleanup(v2pkg.SetW2PublicationAfterAuthorityForTest(repo, pause))
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/seafhttp/repo/"+repo+"/update-branch?head="+fc.commitID, nil)
			c.Params = gin.Params{{Key: "repo_id", Value: repo}}
			c.Set("org_id", org)
			c.Set("user_id", owner)
			handler.UpdateBranch(c)
			if !protected && borrowedFSReadHead(t, database, org, repo) != fx.headBefore {
				t.Fatal("W2-0 VIOLATION: D(P) committed AND HEAD advanced depending on P")
			}
			if beforeRepair {
				expectedStatus := http.StatusServiceUnavailable
				if merge {
					expectedStatus = http.StatusInternalServerError
				}
				if !visited || protected || rec.Code != expectedStatus {
					t.Fatalf("readiness-to-queue gap must commit D then reject HEAD: visited=%v protected=%v status=%d %s", visited, protected, rec.Code, rec.Body.String())
				}
				fx.assertHeadUnchanged(t)
				// The direct row is shared, so preserve existing conservative cleanup.
				// This isolated fixture owns every attempt for repo; remove its row below.
				return
			}
			if !visited || !protected || rec.Code != http.StatusOK {
				t.Fatalf("Sync continuity %s: visited=%v protected=%v status=%d %s", name, visited, protected, rec.Code, rec.Body.String())
			}
			fx.assertHeadAdvanced(t)
			if !fx.hasOwnFSReferrer(t) || len(w2Repairs(t, fx)) != 0 {
				t.Fatal("Sync success must promote fs: and settle repair")
			}
		})
	}
}
