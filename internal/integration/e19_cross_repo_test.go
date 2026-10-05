//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	"github.com/Sesame-Disk/sesamefs/internal/config"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Completed actual source SELECT is held before its caller can acquire pub:.
// No SELECT response or publication state is substituted.
type e19Observer struct {
	*e13ProofObserver
	src, dst, fs, phase string
	once                sync.Once
	reached, release    chan struct{}
}

func (o *e19Observer) ObserveQuery(ctx context.Context, q gocql.ObservedQuery) {
	o.e13ProofObserver.ObserveQuery(ctx, q)
	statement := strings.ToLower(strings.Join(strings.Fields(q.Statement), " "))
	source := strings.HasPrefix(statement, "select obj_type, obj_name, dir_entries, block_ids, seafile_block_ids_sha1, size_bytes, mtime from fs_objects ") && len(q.Values) == 2 && fmt.Sprint(q.Values[0]) == o.src && fmt.Sprint(q.Values[1]) == o.fs
	repair := strings.HasPrefix(statement, "insert into published_block_reference_repairs ") && len(q.Values) > 2 && fmt.Sprint(q.Values[2]) == o.dst
	if q.Err == nil && ((o.phase == "source" && source) || (o.phase == "repair" && repair)) {
		o.once.Do(func() {
			close(o.reached)
			select {
			case <-o.release:
			case <-time.After(90 * time.Second):
				o.mu.Lock()
				o.queryErrors = append(o.queryErrors, "cross-repo barrier timeout")
				o.mu.Unlock()
			}
		})
	}
}
func e19Request(fx *w2CreateFileFixture, method, path string, body any, fn func(*gin.Context)) *httptest.ResponseRecorder {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(method, path, bytes.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("org_id", fx.orgID)
	c.Set("user_id", fx.userID)
	fn(c)
	return rec
}
func e19Task(t *testing.T, fx *w2CreateFileFixture, h *v2pkg.BatchOperationHandler, id string) map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		rec := e19Request(fx, "GET", "/query-copy-move-progress/?task_id="+id, nil, h.GetTaskProgress)
		var p map[string]interface{}
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &p) != nil {
			t.Fatalf("task progress: %d %s", rec.Code, rec.Body.String())
		}
		if p["done"] == true || p["failed"] == float64(1) {
			return p
		}
		if time.Now().After(deadline) {
			t.Fatalf("task timeout: %v", p)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
func TestE19CrossRepoPublication(t *testing.T) {
	requireCassandra(t)
	for _, op := range []string{"copy", "move"} {
		for _, phase := range []string{"normal", "committed", "terminal"} {
			t.Run(op+"/"+phase, func(t *testing.T) {
				database := shareProjectionDBForTest(t)
				fx := &w2CreateFileFixture{w2UploadFileFixture: newW2UploadFileFixture(t, database, newBorrowedFSHeadHandler(t, database, x1StorageClass(t)))}
				e16UploadHistory(t, fx, fx.content, false)
				fx.target = fx.readTarget(t)
				helper := v2pkg.NewFSHelper(database)
				src, err := helper.TraverseToPath(fx.repoID, "/"+fx.filename)
				if err != nil || src.TargetEntry == nil {
					t.Fatalf("source: %v", err)
				}
				fs := src.TargetEntry.ID
				sourceRef := dbpkg.BlockReferrerForFSObject(fx.repoID, fs)
				fx.dropOwnUploadRefs(t)
				e17AssertRefs(t, fx, sourceRef)
				dst := createTestLibrary(t, adminClient, "inttest-e19-dst-"+uuid.NewString())
				dstBefore := borrowedFSReadHead(t, database, fx.orgID, dst)
				trace := &e19Observer{e13ProofObserver: e13Observer(fx.orgID, fx.blockID, "e19"), src: fx.repoID, dst: dst, fs: fs, phase: "source", reached: make(chan struct{}), release: make(chan struct{})}
				var releaseOnce sync.Once
				resume := func() { releaseOnce.Do(func() { close(trace.release) }) }
				defer resume()
				writerDB := w2EvidenceSession(t, splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042")[0], trace)
				h := v2pkg.NewBatchOperationHandler(writerDB, &config.Config{})
				fn := h.AsyncBatchCopy
				if op == "move" {
					fn = h.AsyncBatchMove
				}
				rec := e19Request(fx, "POST", "/async-batch-"+op+"-item/", map[string]interface{}{"src_repo_id": fx.repoID, "dst_repo_id": dst, "src_parent_dir": "/", "dst_parent_dir": "/", "src_dirents": []string{fx.filename}, "conflict_policy": "autorename"}, fn)
				e17OK(t, rec)
				var task map[string]string
				if err := json.Unmarshal(rec.Body.Bytes(), &task); err != nil || task["task_id"] == "" {
					t.Fatalf("task: %s %v", rec.Body.String(), err)
				}
				select {
				case <-trace.reached:
				case <-time.After(10 * time.Second):
					t.Fatal("actual source-copy read not reached")
				}
				if borrowedFSReadHead(t, database, fx.orgID, dst) != dstBefore {
					t.Fatal("HEAD before stage")
				}
				e17AssertRefs(t, fx, sourceRef)
				var authority gcpkg.BlockDeleteAuthority
				if phase != "normal" {
					e19PurgeSource(t, fx, fs)
					e17AssertRefs(t, fx)
					authority = e17CommitGC(t, fx)
					if phase == "terminal" {
						e17Recover(t, fx, authority)
					}
				}
				resume()
				progress := e19Task(t, fx, h, task["task_id"])
				dstHead := borrowedFSReadHead(t, database, fx.orgID, dst)
				copied, copyErr := helper.TraverseToPath(dst, "/"+fx.filename)
				refs, refErr := database.ListBlockReferrers(fx.orgID, fx.blockID)
				if refErr != nil {
					t.Fatal(refErr)
				}
				t.Logf("E19 %s/%s task=%v dst HEAD before=%s after=%s copied=%+v err=%v refs=%v P1=%+v D=%+v", op, phase, progress, dstBefore, dstHead, copied, copyErr, refs, fx.target, authority)
				if phase != "normal" {
					if dstHead != dstBefore || (copyErr == nil && copied.TargetEntry != nil) {
						t.Fatalf("RED: destination reachable after exact P1 retirement (%s); task=%v refs=%v", phase, progress, refs)
					}
					if progress["failed"] != float64(1) {
						t.Fatalf("retired attempt must fail: %v", progress)
					}
					return
				}
				if progress["successful"] != float64(1) || progress["failed"] != float64(0) || dstHead == dstBefore || copyErr != nil || copied.TargetEntry == nil || copied.TargetEntry.ID != fs {
					t.Fatalf("normal copy/move failed: %v %v", progress, copyErr)
				}
				e17AssertRefs(t, fx, sourceRef, dbpkg.BlockReferrerForFSObject(dst, fs))
				w2AssertBytes(t, fx)
				trace.mu.Lock()
				writes := append([]string(nil), trace.settlementWrites...)
				repairs := trace.repairWrites
				errs := append([]string(nil), trace.queryErrors...)
				trace.mu.Unlock()
				if repairs != 1 || len(writes) != 2 || writes[0] != "fs:" || writes[1] != "repair-delete" || len(errs) > 0 {
					t.Fatalf("settlement: %v %d %v", writes, repairs, errs)
				}
				remaining, srcErr := helper.TraverseToPath(fx.repoID, "/"+fx.filename)
				if op == "copy" && (srcErr != nil || remaining.TargetEntry == nil) {
					t.Fatal("copy removed source")
				}
				if op == "move" && srcErr == nil && remaining.TargetEntry != nil {
					t.Fatal("move retained source path")
				}
			})
		}
	}
}

// Filter real queue rows to the source library cascade and its metadata only.
// Leave block processing to the separately observed exact zero-proof worker.
type e19LibraryQueue struct {
	gcpkg.GCStore
	repo uuid.UUID
}

func (s *e19LibraryQueue) DequeueBatch(org uuid.UUID, _ int, cutoff time.Time) ([]gcpkg.QueueItem, error) {
	rows, err := s.GCStore.DequeueBatch(org, 100000, cutoff)
	var own []gcpkg.QueueItem
	for _, row := range rows {
		if row.ItemType != gcpkg.ItemBlock && (row.LibraryID == s.repo || (row.ItemType == gcpkg.ItemLibraryCascade && row.ItemID == s.repo.String())) {
			own = append(own, row)
		}
	}
	return own, err
}
func e19PurgeSource(t *testing.T, fx *w2CreateFileFixture, fs string) {
	t.Helper()
	for _, path := range []string{"/api2/repos/" + fx.repoID + "/", "/api/v2.1/repos/deleted/" + fx.repoID + "/"} {
		response := adminClient.Delete(t, path)
		expectStatus(t, response, http.StatusOK)
		response.Body.Close()
	}
	store := gcpkg.NewCassandraStore(fx.database)
	scope := &e19LibraryQueue{GCStore: store, repo: uuid.MustParse(fx.repoID)}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := w2Worker(t, scope, fx.target.StorageClass).ProcessOrgOnce(t.Context(), fx.orgUUID); err != nil {
			t.Fatal(err)
		}
		var id string
		err := fx.database.Session().Query(`SELECT fs_id FROM fs_objects WHERE library_id = ? AND fs_id = ?`, fx.repoID, fs).Scan(&id)
		if err == gocql.ErrNotFound {
			refs, err := fx.database.ListBlockReferrers(fx.orgID, fx.blockID)
			if err != nil {
				t.Fatal(err)
			}
			for _, ref := range refs {
				if ref == dbpkg.BlockReferrerForFSObject(fx.repoID, fs) {
					t.Fatal("source fs ref remains after productive purge")
				}
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("productive source cascade did not remove fs_object")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
