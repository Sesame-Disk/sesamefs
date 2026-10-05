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

const e19EvidenceEnv = "SESAMEFS_REQUIRE_E19_CROSS_REPO_EVIDENCE"

var e19Evidence = map[string]bool{}
var e19Phases = []string{"normal", "committed", "terminal", "captured-committed", "captured-terminal", "repair-first", "head-conflict"}

func e19Missing(observed map[string]bool) []string {
	var missing []string
	for _, op := range []string{"copy", "move"} {
		for _, phase := range e19Phases {
			name := op + "/" + phase
			if !observed[name] {
				missing = append(missing, name)
			}
		}
	}
	return missing
}

// Completed actual source SELECT is held before its caller can acquire pub:.
// No SELECT response or publication state is substituted.
type e19Observer struct {
	*e13ProofObserver
	src, dst, fs, phase string
	captures            int
	events              []string
	once                sync.Once
	reached, release    chan struct{}
}

func (o *e19Observer) ObserveQuery(ctx context.Context, q gocql.ObservedQuery) {
	o.e13ProofObserver.ObserveQuery(ctx, q)
	statement := strings.ToLower(strings.Join(strings.Fields(q.Statement), " "))
	source := strings.HasPrefix(statement, "select obj_type, obj_name, dir_entries, block_ids, seafile_block_ids_sha1, size_bytes, mtime from fs_objects ") && len(q.Values) == 2 && fmt.Sprint(q.Values[0]) == o.src && fmt.Sprint(q.Values[1]) == o.fs
	captured := strings.HasPrefix(statement, "select storage_class, storage_key from blocks ") && len(q.Values) == 2 && fmt.Sprint(q.Values[0]) == o.org && fmt.Sprint(q.Values[1]) == o.block
	head := strings.HasPrefix(statement, "update libraries set head_commit_id = ") && len(q.Values) >= 6 && fmt.Sprint(q.Values[4]) == o.org
	fsWrite := strings.HasPrefix(statement, "insert into block_references ") && len(q.Values) >= 3 && fmt.Sprint(q.Values[0]) == o.org && fmt.Sprint(q.Values[1]) == o.block && strings.HasPrefix(fmt.Sprint(q.Values[2]), "fs:"+o.dst+":")
	if q.Err == nil && (head || fsWrite) {
		o.mu.Lock()
		if fsWrite {
			o.events = append(o.events, "destination-fs")
		}
		if head && fmt.Sprint(q.Values[5]) == o.src {
			o.events = append(o.events, "source-head")
		}
		if head && fmt.Sprint(q.Values[5]) == o.dst {
			o.events = append(o.events, "destination-head")
		}
		o.mu.Unlock()
	}
	if captured && q.Err == nil {
		o.mu.Lock()
		o.captures++
		o.mu.Unlock()
	}
	repair := strings.HasPrefix(statement, "insert into published_block_reference_repairs ") && len(q.Values) > 2 && fmt.Sprint(q.Values[2]) == o.dst
	if q.Err == nil && ((o.phase == "source" && source) || (o.phase == "repair" && repair) || (o.phase == "captured" && captured)) {
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
		for _, phase := range e19Phases {
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
				e19AssertFileLayout(t, fx, fx.repoID, fs)
				sourceRef := dbpkg.BlockReferrerForFSObject(fx.repoID, fs)
				ups, upErr := database.ListBlockReferrers(fx.orgID, fx.blockID)
				if upErr != nil {
					t.Fatal(upErr)
				}
				t.Cleanup(func() {
					for _, ref := range ups {
						if strings.HasPrefix(ref, "up:") {
							if err := database.DeleteProvisionalBlockReferenceExpiry(fx.orgID, fx.blockID, ref, time.Time{}); err != nil {
								t.Error(err)
							}
						}
					}
				})
				fx.dropOwnUploadRefs(t)
				e17AssertRefs(t, fx, sourceRef)
				dst := createTestLibrary(t, adminClient, "inttest-e19-dst-"+uuid.NewString())
				t.Cleanup(func() {
					refs, err := database.ListBlockReferrers(fx.orgID, fx.blockID)
					if err != nil {
						t.Error(err)
						return
					}
					for _, ref := range refs {
						if strings.HasPrefix(ref, "fs:"+dst+":") || strings.HasPrefix(ref, "pub:") {
							if err := database.RemoveBlockReference(fx.orgID, fx.blockID, ref); err != nil {
								t.Error(err)
							}
						}
					}
					for bucket := 0; bucket < dbpkg.PublishedBlockReferenceRepairBuckets; bucket++ {
						if err := database.Session().Query(`DELETE FROM published_block_reference_repairs WHERE bucket = ? AND org_id = ? AND repo_id = ?`, bucket, fx.orgID, dst).Exec(); err != nil {
							t.Error(err)
						}
					}
				})
				dstBefore := borrowedFSReadHead(t, database, fx.orgID, dst)
				trace := &e19Observer{e13ProofObserver: e13Observer(fx.orgID, fx.blockID, "e19"), src: fx.repoID, dst: dst, fs: fs, phase: "source", reached: make(chan struct{}), release: make(chan struct{})}
				if strings.HasPrefix(phase, "captured-") {
					trace.phase = "captured"
				}
				if phase == "repair-first" {
					trace.phase = "repair"
				}
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
				if phase != "repair-first" {
					e17AssertRefs(t, fx, sourceRef)
				}
				var authority gcpkg.BlockDeleteAuthority
				if phase == "head-conflict" {
					inner := *fx.borrowedFSHeadFixture
					inner.repoID = dst
					competitor := &w2CreateFileFixture{w2UploadFileFixture: &w2UploadFileFixture{borrowedFSHeadFixture: &inner}}
					rec := e16FileRequest(competitor, "POST", "?p=/e19-competitor.txt", nil, newBorrowedFSHeadHandler(t, database, x1StorageClass(t)).CreateFile)
					if rec.Code != http.StatusCreated {
						t.Fatalf("productive competing HEAD: %d %s", rec.Code, rec.Body.String())
					}
					if borrowedFSReadHead(t, database, fx.orgID, dst) == dstBefore {
						t.Fatal("competitor did not win HEAD")
					}
				}
				if phase != "normal" && phase != "head-conflict" {
					e19PurgeSource(t, fx, fs)
					if phase == "repair-first" {
						refs, err := database.ListBlockReferrers(fx.orgID, fx.blockID)
						if err != nil || len(refs) != 1 || !strings.HasPrefix(refs[0], "pub:") {
							t.Fatalf("repair-first refs: %v %v", refs, err)
						}
						e12ExpireRealTemporaryTTL(t, fx, refs)
						inner := *fx.borrowedFSHeadFixture
						inner.repoID = dst
						inner.headBefore = dstBefore
						dstFX := &w2CreateFileFixture{w2UploadFileFixture: &w2UploadFileFixture{borrowedFSHeadFixture: &inner}}
						rows := w2Repairs(t, dstFX)
						if len(rows) != 1 || rows[0].fsID != fs || len(rows[0].blocks) != 1 || rows[0].blocks[0] != fx.blockID {
							t.Fatalf("exact destination durable repair: %+v", rows)
						}
						w2AssertGuardOnly(t, dstFX)
						w2AssertGCBlocked(t, dstFX)
						e12AssertNoDeleteLifecycle(t, fx)
					} else {
						e17AssertRefs(t, fx)
						authority = e17CommitGC(t, fx)
						if strings.HasSuffix(phase, "terminal") {
							e17Recover(t, fx, authority)
						}
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
				if phase != "normal" && phase != "repair-first" && phase != "head-conflict" {
					if dstHead != dstBefore || (copyErr == nil && copied.TargetEntry != nil) {
						t.Fatalf("RED: destination reachable after exact P1 retirement (%s); task=%v refs=%v", phase, progress, refs)
					}
					if progress["failed"] != float64(1) {
						t.Fatalf("retired attempt must fail: %v", progress)
					}
					e17AssertRefs(t, fx)
					inner := *fx.borrowedFSHeadFixture
					inner.repoID = dst
					dstFX := &w2CreateFileFixture{w2UploadFileFixture: &w2UploadFileFixture{borrowedFSHeadFixture: &inner}}
					if rows := w2Repairs(t, dstFX); len(rows) != 0 {
						t.Fatalf("rejected attempt left repair: %+v", rows)
					}
					x1AssertCanonicalAbsent(t, gcpkg.NewCassandraStore(database), fx.orgUUID, fx.blockID)
					e17Recover(t, fx, authority)
					e19Evidence[op+"/"+phase] = true
					return
				}
				if phase == "repair-first" {
					e19AssertFileLayout(t, fx, dst, fs)
					e19AssertDownload(t, fx, dst)
					if dstHead == dstBefore || copyErr != nil || copied.TargetEntry == nil || copied.TargetEntry.ID != fs {
						t.Fatalf("protected publication failed: %v %v", progress, copyErr)
					}
					e17AssertRefs(t, fx, dbpkg.BlockReferrerForFSObject(dst, fs))
					w2AssertBytes(t, fx)
					e12AssertNoDeleteLifecycle(t, fx)
					if op == "copy" && progress["successful"] != float64(1) {
						t.Fatalf("repair-first copy: %v", progress)
					}
					if op == "move" && progress["failed"] != float64(1) {
						t.Fatalf("purged-source move reports source failure: %v", progress)
					}
					inner := *fx.borrowedFSHeadFixture
					inner.repoID = dst
					if rows := w2Repairs(t, &w2CreateFileFixture{w2UploadFileFixture: &w2UploadFileFixture{borrowedFSHeadFixture: &inner}}); len(rows) != 0 {
						t.Fatalf("settled repair remains: %+v", rows)
					}
					trace.mu.Lock()
					writes := append([]string(nil), trace.settlementWrites...)
					repairs := trace.repairWrites
					errs := append([]string(nil), trace.queryErrors...)
					trace.mu.Unlock()
					if repairs != 1 || len(writes) != 2 || writes[0] != "fs:" || writes[1] != "repair-delete" || len(errs) > 0 {
						t.Fatalf("repair-first settlement: %d %v %v", repairs, writes, errs)
					}
					e19Evidence[op+"/"+phase] = true
					return
				}
				if progress["successful"] != float64(1) || progress["failed"] != float64(0) || dstHead == dstBefore || copyErr != nil || copied.TargetEntry == nil || copied.TargetEntry.ID != fs {
					t.Fatalf("normal copy/move failed: %v %v", progress, copyErr)
				}
				e19AssertFileLayout(t, fx, dst, fs)
				e19AssertDownload(t, fx, dst)
				e17AssertRefs(t, fx, sourceRef, dbpkg.BlockReferrerForFSObject(dst, fs))
				w2AssertBytes(t, fx)
				trace.mu.Lock()
				writes := append([]string(nil), trace.settlementWrites...)
				repairs := trace.repairWrites
				captures := trace.captures
				events := append([]string(nil), trace.events...)
				errs := append([]string(nil), trace.queryErrors...)
				trace.mu.Unlock()
				wantRepairs, wantCaptures := 1, 1
				wantWrites := []string{"fs:", "repair-delete"}
				if phase == "head-conflict" {
					wantRepairs, wantCaptures = 2, 2
					wantWrites = []string{"repair-delete", "fs:", "repair-delete"}
				}
				if repairs != wantRepairs || captures != wantCaptures || fmt.Sprint(writes) != fmt.Sprint(wantWrites) || len(errs) > 0 {
					t.Fatalf("settlement: %v %d %v", writes, repairs, errs)
				}
				if op == "move" {
					fsIndex, srcIndex := -1, -1
					for i, event := range events {
						if event == "destination-fs" {
							fsIndex = i
						}
						if event == "source-head" {
							srcIndex = i
						}
					}
					if fsIndex < 0 || srcIndex <= fsIndex {
						t.Fatalf("move source published before destination settlement: %v", events)
					}
				}
				remaining, srcErr := helper.TraverseToPath(fx.repoID, "/"+fx.filename)
				if op == "copy" && (srcErr != nil || remaining.TargetEntry == nil) {
					t.Fatal("copy removed source")
				}
				if op == "move" && srcErr == nil && remaining.TargetEntry != nil {
					t.Fatal("move retained source path")
				}
				if phase == "head-conflict" {
					other, err := helper.TraverseToPath(dst, "/e19-competitor.txt")
					if err != nil || other.TargetEntry == nil {
						t.Fatal("retry lost competing HEAD content")
					}
				}
				// Explicit skip replay must leave existing destination publication intact.
				replay := e19Request(fx, "POST", "/async-batch-"+op+"-item/", map[string]interface{}{"src_repo_id": fx.repoID, "dst_repo_id": dst, "src_parent_dir": "/", "dst_parent_dir": "/", "src_dirents": []string{fx.filename}, "conflict_policy": "skip"}, fn)
				e17OK(t, replay)
				var retry map[string]string
				if err := json.Unmarshal(replay.Body.Bytes(), &retry); err != nil {
					t.Fatal(err)
				}
				result := e19Task(t, fx, h, retry["task_id"])
				// For move the source is already absent; a failed replay is permitted,
				// but no existing destination HEAD/ref/repair may be changed.
				if op == "copy" && result["successful"] != float64(1) {
					t.Fatalf("copy skip replay: %v", result)
				}
				if borrowedFSReadHead(t, database, fx.orgID, dst) != dstHead || fx.readTarget(t) != fx.target {
					t.Fatal("replay changed destination HEAD/P")
				}
				e17AssertRefs(t, fx, sourceRef, dbpkg.BlockReferrerForFSObject(dst, fs))
				e19Evidence[op+"/"+phase] = true
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

func TestE19EvidenceRequiresEveryNamedLeg(t *testing.T) {
	phases := []string{"normal", "committed", "terminal", "captured-committed", "captured-terminal", "repair-first", "head-conflict"}
	observed := map[string]bool{}
	for _, op := range []string{"copy", "move"} {
		for _, phase := range phases {
			observed[op+"/"+phase] = true
		}
	}
	if len(e19Phases) != len(phases) || len(e19Missing(nil)) != 14 || len(e19Missing(observed)) != 0 {
		t.Fatal("fourteen required copy/move legs must not shrink")
	}
	for name := range observed {
		delete(observed, name)
		missing := e19Missing(observed)
		if len(missing) != 1 || missing[0] != name {
			t.Fatalf("missing evidence hidden: %v", missing)
		}
		observed[name] = true
	}
}

func e19AssertFileLayout(t *testing.T, fx *w2CreateFileFixture, repo, fs string) {
	t.Helper()
	var internal, external []string
	var size int64
	if err := fx.database.Session().Query(`SELECT block_ids, seafile_block_ids_sha1, size_bytes FROM fs_objects WHERE library_id = ? AND fs_id = ?`, repo, fs).Scan(&internal, &external, &size); err != nil || len(internal) != 1 || len(external) != 1 || internal[0] != fx.blockID || external[0] != fx.sha1ID || size != int64(len(fx.content)) {
		t.Fatalf("exact paired metadata %s/%s: internal=%v external=%v size=%d err=%v", repo, fs, internal, external, size, err)
	}
}

func e19AssertDownload(t *testing.T, fx *w2CreateFileFixture, dst string) {
	t.Helper()
	link := rewriteUploadURLHost(downloadTokenURL(t, dst, "/"+fx.filename), adminClient.baseURL)
	status, body := getDownload(t, link)
	if status != http.StatusOK || body != string(fx.content) {
		t.Fatalf("destination HTTP download: status=%d body=%q", status, body)
	}
}
