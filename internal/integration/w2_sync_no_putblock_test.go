//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	apipkg "github.com/Sesame-Disk/sesamefs/internal/api"
	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	"github.com/Sesame-Disk/sesamefs/internal/config"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Characterizes W2-4 on unchanged production code. Bytes come from the real
// web block-upload endpoint; no Sync PutBlock ever occurs in these fixtures.
// Removing only TTL-bound refs models a legitimate stalled request, as in
// the existing W2-0 tests. Neither HEAD nor D is injected by the harness.
// SESAMEFS_W24_ASSERT_SAFETY=1 changes the counterexample assertion to the
// desired invariant, making the known defect RED on this baseline.
func TestW2SyncNoPutBlock(t *testing.T) {
	requireCassandra(t)
	database := shareProjectionDBForTest(t)
	store := gcpkg.NewCassandraStore(database)
	class := x1StorageClass(t)
	for _, merge := range []bool{false, true} {
		path := "direct"
		if merge {
			path = "autoMerge"
		}
		for _, boundary := range []string{"writerFirst", "gcBeforeStage", "gcBeforeRepair", "repairFirst", "fullyRetired"} {
			t.Run(path+"/"+boundary, func(t *testing.T) {
				w24Observe(t)
				web := newSessionUploadHeadFixture(t, database, newBorrowedFSHeadHandler(t, database, class))
				fx := &w2CreateFileFixture{w2UploadFileFixture: &w2UploadFileFixture{borrowedFSHeadFixture: web.borrowedFSHeadFixture}}
				x1Cleanup(t, database, fx.orgUUID, fx.blockID)
				t.Cleanup(func() {
					for b := 0; b < dbpkg.PublishedBlockReferenceRepairBuckets; b++ {
						if err := database.Session().Query(`DELETE FROM published_block_reference_repairs WHERE bucket = ? AND org_id = ? AND repo_id = ?`, b, fx.orgID, fx.repoID).Exec(); err != nil {
							t.Error(err)
						}
					}
				})
				w24AssertNoSyncPin(t, fx)
				// Both wire representations must be deduplicated by the real endpoint.
				resp := syncW2DoRequest(t, adminClient, http.MethodPost, "/seafhttp/repo/"+fx.repoID+"/check-blocks", mustMarshalSyncObjectForTest(t, []string{fx.sha1ID, fx.blockID}), "application/json")
				expectStatus(t, resp, http.StatusOK)
				var missing []string
				decodeJSON(t, resp, &missing)
				resp.Body.Close()
				if len(missing) != 0 {
					t.Fatalf("real CheckBlocks did not dedup: %v", missing)
				}
				w24AssertNoSyncPin(t, fx)
				// Normal Seafile SHA-1 wire identity, not a fabricated missing mapping.
				fc := w24PutMetadata(t, fx, fx.headBefore, fx.sha1ID, "remote.txt")
				if merge {
					fx.filename = "local.txt" // empty CreateFile makes HEAD diverge without touching P.
					if rec := fx.create(t); rec.Code != http.StatusCreated {
						t.Fatalf("divergent HEAD: %d %s", rec.Code, rec.Body.String())
					}
					fx.headBefore = borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
				}
				handler := w24Handler(t, database, class)
				var authority gcpkg.BlockDeleteAuthority
				visited := false
				commitD := func() {
					visited = true
					w2ExpireTemporaryRefs(t, fx)
					w24AssertNoSyncPin(t, fx)
					live, err := store.BlockPublicationLivenessGlobal(fx.orgUUID, fx.blockID)
					if err != nil || live != dbpkg.BlockPublicationZero {
						t.Fatalf("pre-D liveness=%v err=%v", live, err)
					}
					attempt := x1Attempt(fx.target, "w24-"+boundary)
					x1ClaimAcquired(t, store, fx.orgUUID, fx.blockID, attempt)
					// Follow the current GC worker's G2 path against real Cassandra:
					// publish PREPARED, then settle the durable handoff COMMITTED.
					prepared := store.PrepareBlockDeleteOrphan(fx.orgUUID, fx.blockID, attempt, fx.sha1ID, time.Now().UTC())
					if prepared.Cause != nil || (prepared.Outcome != gcpkg.StartBlockDeleteOrphanCreated && prepared.Outcome != gcpkg.StartBlockDeleteOrphanSameAuthority) {
						t.Fatalf("PREPARED outcome=%s err=%v", prepared.Outcome, prepared.Cause)
					}
					handoff, err := store.CommitBlockDeleteOrphanHandoff(fx.orgUUID, fx.blockID, attempt)
					if err != nil || (handoff.Outcome != gcpkg.BlockDeleteHandoffCommitted && handoff.Outcome != gcpkg.BlockDeleteHandoffAlreadyCommitted) {
						t.Fatalf("COMMITTED handoff=%s err=%v", handoff.Outcome, err)
					}
					authority = attempt
					if !handoff.Authority.IsZero() {
						authority = handoff.Authority.Authority()
					}
					fx.assertDUnrevoked(t, authority)
					t.Logf("EVIDENCE: zero at EACH_QUORUM; D(P) committed for %s/%s/%s", fx.blockID, fx.target.StorageClass, fx.target.StorageKey)
				}
				switch boundary {
				case "gcBeforeStage", "fullyRetired":
					commitD()
					if boundary == "fullyRetired" {
						w24Retire(t, fx, store, authority)
						// Exercise the canonical-ID wire form independently of the
						// still-retained operational SHA-1 mapping.
						fc = w24PutMetadata(t, fx, fc.parent, fx.blockID, "canonical.txt")
					}
				case "gcBeforeRepair":
					t.Cleanup(v2pkg.SetW2PublicationBeforeRepairForTest(fx.repoID, commitD))
				case "repairFirst":
					t.Cleanup(v2pkg.SetW2PublicationAfterAuthorityForTest(fx.repoID, func() {
						visited = true
						w2ExpireTemporaryRefs(t, fx)
						w2AssertGuardOnly(t, fx)
						claim := x1Attempt(fx.target, "w24-repair-first")
						x1ClaimAcquired(t, store, fx.orgUUID, fx.blockID, claim)
						live, err := store.BlockPublicationLivenessGlobal(fx.orgUUID, fx.blockID)
						if err != nil || live != dbpkg.BlockPublicationRepairGuardOnly {
							t.Fatalf("repair guard: %v %v", live, err)
						}
						if _, err := store.ReleaseBlockClaim(fx.orgUUID, fx.blockID, claim); err != nil {
							t.Fatal(err)
						}
					}))
				}
				rec := w24Promote(fx, handler, fc.commit)
				unsafe := boundary == "gcBeforeStage" || boundary == "gcBeforeRepair" || boundary == "fullyRetired"
				head := borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
				if unsafe && os.Getenv("SESAMEFS_W24_ASSERT_SAFETY") == "1" && head != fx.headBefore {
					t.Fatalf("W2-4 VIOLATION: D(P) committed AND HEAD advanced depending on P; status=%d", rec.Code)
				}
				if rec.Code != http.StatusOK || head == fx.headBefore {
					t.Fatalf("current behavior changed: status=%d head=%s body=%s", rec.Code, head, rec.Body.String())
				}
				if !merge && head != fc.commit {
					t.Fatal("direct HEAD did not publish the target")
				}
				w24AssertHeadReaches(t, fx, head, fc.file)
				if !fx.hasOwnFSReferrer(t) {
					t.Fatal("permanent fs: was not promoted")
				}
				w24AssertNoSyncPin(t, fx)
				if boundary != "writerFirst" && !visited {
					t.Fatal("selected boundary never executed")
				}
				if unsafe {
					if boundary != "fullyRetired" {
						fx.assertDUnrevoked(t, authority)
					}
					t.Log("W2-4 COUNTEREXAMPLE: current HEAD reaches P after committed D(P); this is characterization, not closure")
				} else {
					t.Log("EVIDENCE: writer succeeded; repair-before-D prevents destructive zero after TTL refs expire")
				}
			})
		}
	}
}

func w24AssertHeadReaches(t *testing.T, fx *w2CreateFileFixture, head, file string) {
	t.Helper()
	var root string
	if err := fx.database.Session().Query(`SELECT root_fs_id FROM commits WHERE library_id = ? AND commit_id = ?`, fx.repoID, head).Scan(&root); err != nil {
		t.Fatal(err)
	}
	var entriesJSON string
	if err := fx.database.Session().Query(`SELECT dir_entries FROM fs_objects WHERE library_id = ? AND fs_id = ?`, fx.repoID, root).Scan(&entriesJSON); err != nil {
		t.Fatal(err)
	}
	var entries []apipkg.FSEntry
	if err := json.Unmarshal([]byte(entriesJSON), &entries); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.ID == file {
			var blockIDs []string
			if err := fx.database.Session().Query(`SELECT block_ids FROM fs_objects WHERE library_id = ? AND fs_id = ?`, fx.repoID, file).Scan(&blockIDs); err != nil {
				t.Fatal(err)
			}
			for _, blockID := range blockIDs {
				if blockID == fx.blockID || blockID == fx.sha1ID {
					return
				}
			}
			t.Fatalf("HEAD reaches file %s but its stored block IDs %v do not name P=%s (legacy alias %s)", file, blockIDs, fx.blockID, fx.sha1ID)
		}
	}
	t.Fatalf("published HEAD %s does not reach file %s: %s", head, file, entriesJSON)
}

type w24Metadata struct{ commit, file, parent string }

func w24PutMetadata(t *testing.T, fx *w2CreateFileFixture, parent, wireID, name string) w24Metadata {
	t.Helper()
	fileJSON := mustMarshalSyncObjectForTest(t, map[string]interface{}{"block_ids": []string{wireID}, "size": len(fx.content), "type": 1, "version": 1})
	file := syncSHA1HexForTest(fileJSON)
	rootJSON := mustMarshalSyncObjectForTest(t, map[string]interface{}{"dirents": []apipkg.FSEntry{{ID: file, Mode: 33188, Mtime: time.Now().Unix(), Name: name, Size: int64(len(fx.content))}}, "type": 3, "version": 1})
	root := syncSHA1HexForTest(rootJSON)
	commit := syncSHA1HexForTest([]byte("w24-" + uuid.NewString()))
	payload := mustMarshalSyncObjectForTest(t, map[string]interface{}{"commit_id": commit, "repo_id": fx.repoID, "root_id": root, "parent_id": parent, "description": "W2-4 no PutBlock", "ctime": time.Now().Unix(), "version": 1})
	resp := syncW2DoRequest(t, adminClient, http.MethodPut, fmt.Sprintf("/seafhttp/repo/%s/commit/%s", fx.repoID, commit), payload, "application/json")
	expectStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = syncW2DoRequest(t, adminClient, http.MethodPost, "/seafhttp/repo/"+fx.repoID+"/recv-fs", packSyncFSObjectsForTest(t, syncPackedFSObject{fsID: file, jsonData: fileJSON}, syncPackedFSObject{fsID: root, jsonData: rootJSON}), "application/octet-stream")
	expectStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	w24AssertNoSyncPin(t, fx)
	return w24Metadata{commit: commit, file: file, parent: parent}
}

func w24Handler(t *testing.T, database *dbpkg.DB, class string) *apipkg.SyncHandler {
	t.Helper()
	s3 := newVerificationS3Store(t)
	manager := storage.NewManager()
	manager.SetDefaultClass(class)
	manager.RegisterBackend(class, s3, "")
	return apipkg.NewSyncHandler(database, s3, manager, &config.Config{Storage: config.StorageConfig{DefaultClass: class}}, nil)
}

func w24Promote(fx *w2CreateFileFixture, handler *apipkg.SyncHandler, head string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/seafhttp/repo/"+fx.repoID+"/update-branch?head="+head, nil)
	c.Params = gin.Params{{Key: "repo_id", Value: fx.repoID}}
	c.Set("org_id", fx.orgID)
	c.Set("user_id", fx.userID)
	handler.UpdateBranch(c)
	return rec
}

func w24AssertNoSyncPin(t *testing.T, fx *w2CreateFileFixture) {
	t.Helper()
	ref := dbpkg.BlockReferrerForUpload("sync:" + fx.repoID + ":" + fx.blockID)
	found, err := fx.database.BlockReferenceExistsEachQuorum(fx.orgID, fx.blockID, ref)
	if err != nil || found {
		t.Fatalf("fixture must NEVER establish Sync PutBlock provenance: exists=%v err=%v", found, err)
	}
}

func w24Retire(t *testing.T, fx *w2CreateFileFixture, store *gcpkg.CassandraStore, a gcpkg.BlockDeleteAuthority) {
	t.Helper()
	committed := gcpkg.CommittedBlockDeleteAuthorityForTest(a)
	promoted := store.PromoteBlockDeleteOrphan(fx.orgUUID, fx.blockID, committed)
	if promoted.Cause != nil || (promoted.Outcome != gcpkg.StartBlockDeleteOrphanCreated && promoted.Outcome != gcpkg.StartBlockDeleteOrphanSameAuthority) {
		t.Fatalf("promote COMMITTED orphan: %s %v", promoted.Outcome, promoted.Cause)
	}
	finalized, err := store.FinalizeBlockDelete(fx.orgUUID, fx.blockID, committed)
	if err != nil || finalized.Outcome != gcpkg.BlockDeleteFinalized {
		t.Fatalf("finalize: %+v %v", finalized, err)
	}
	bs := newVerificationBlockStore(t, fx.orgID)
	if err := bs.DeleteBlockByStorageKey(context.Background(), fx.target.StorageKey); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TerminateBlockDeleteLifecycle(fx.orgUUID, fx.blockID, committed); err != nil {
		t.Fatal(err)
	}
	publication, exists, err := store.GetS3OrphanExact(fx.orgUUID, fx.blockID, committed.Authority())
	if err != nil || !exists {
		t.Fatalf("read prepared orphan: exists=%v err=%v", exists, err)
	}
	if err := store.DeleteS3Orphan(fx.orgUUID, fx.blockID, committed.Authority(), publication.FirstSeenAt); err != nil {
		t.Fatal(err)
	}
	x1AssertCanonicalAbsent(t, store, fx.orgUUID, fx.blockID)
	if exists, err := bs.BlockExists(t.Context(), fx.blockID); err != nil || exists {
		t.Fatalf("P still exists: %v %v", exists, err)
	}
}
