//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	apipkg "github.com/Sesame-Disk/sesamefs/internal/api"
	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	"github.com/Sesame-Disk/sesamefs/internal/config"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/Sesame-Disk/sesamefs/internal/middleware"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/gin-gonic/gin"
)

const e16EvidenceEnv = "SESAMEFS_REQUIRE_E16_REVERTFILE_CHARACTERIZATION"

var e16Evidence = map[string]bool{}
var e16Legs = []string{"normal", "retained-history-gc", "multiblock-retained-history-gc", "head-conflict", "same-content", "skip"}

func e16Missing(observed map[string]bool) []string {
	var missing []string
	for _, leg := range e16Legs {
		if !observed[leg] {
			missing = append(missing, leg)
		}
	}
	return missing
}

// Characterization, not retirement certification: no fs: row is deleted to
// manufacture zero liveness. Candidate creation is a discovery control only.
func TestE16RevertFileRetainedHistory(t *testing.T) {
	requireCassandra(t)
	for _, leg := range e16Legs {
		t.Run(leg, func(t *testing.T) {
			database := shareProjectionDBForTest(t)
			fx := &w2CreateFileFixture{w2UploadFileFixture: newW2UploadFileFixture(t, database, newBorrowedFSHeadHandler(t, database, x1StorageClass(t)))}
			blocks := []*w2CreateFileFixture{fx}
			content := fx.content
			if leg == "multiblock-retained-history-gc" {
				seed := fx.blockID
				fx.content = bytes.Repeat([]byte(seed), 8*1024*1024/len(seed))
				fx.blockID, fx.sha1ID = sha256hex(fx.content), sha1hex(fx.content)
				inner := *fx.borrowedFSHeadFixture
				inner.content = []byte("e16-tail-" + seed)
				inner.blockID, inner.sha1ID = sha256hex(inner.content), sha1hex(inner.content)
				other := &w2CreateFileFixture{w2UploadFileFixture: &w2UploadFileFixture{borrowedFSHeadFixture: &inner}}
				blocks = append(blocks, other)
				content = append(append([]byte(nil), fx.content...), other.content...)
				for _, b := range blocks {
					x1Cleanup(t, database, b.orgUUID, b.blockID)
					cleanupUploadedBlockArtifactsForTest(t, b.orgID, b.repoID, b.blockID, b.sha1ID)
				}
			}
			e16UploadHistory(t, fx, content, len(blocks) > 1)
			historyHead := borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
			helper := v2pkg.NewFSHelper(database)
			old, err := helper.TraverseToPath(fx.repoID, "/"+fx.filename)
			if err != nil || old.TargetEntry == nil {
				t.Fatalf("historical entry: %v", err)
			}
			fsID := old.TargetEntry.ID
			var internal, external []string
			var size int64
			readLayout := func() {
				t.Helper()
				if err := database.Session().Query(`SELECT block_ids, seafile_block_ids_sha1, size_bytes FROM fs_objects WHERE library_id = ? AND fs_id = ?`, fx.repoID, fsID).Scan(&internal, &external, &size); err != nil {
					t.Fatal(err)
				}
				if len(internal) != len(blocks) || len(external) != len(blocks) || size != int64(len(content)) {
					t.Fatalf("historical layout: %v %v size=%d", internal, external, size)
				}
				for i, b := range blocks {
					if internal[i] != b.blockID || external[i] != b.sha1ID {
						t.Fatalf("positional historical layout: %v %v", internal, external)
					}
				}
			}
			readLayout()
			permanent := dbpkg.BlockReferrerForFSObject(fx.repoID, fsID)
			for _, b := range blocks {
				b.target = b.readTarget(t)
				refs, err := database.ListBlockReferrers(b.orgID, b.blockID)
				if err != nil {
					t.Fatal(err)
				}
				// Explicit lapse of this isolated upload's own temporary pin. The
				// historical permanent fs: remains untouched, and must be sufficient.
				own := 0
				for _, ref := range refs {
					if strings.HasPrefix(ref, "up:") {
						own++
						if err := database.RemoveBlockReference(b.orgID, b.blockID, ref); err != nil {
							t.Fatal(err)
						}
					}
				}
				if own != 1 {
					t.Fatalf("upload must own exactly one temporary pin: %v", refs)
				}
				e16AssertOnlyHistoricalRef(t, b, permanent)
			}
			if rows := w2Repairs(t, fx); len(rows) != 0 {
				t.Fatalf("upload did not settle: %+v", rows)
			}
			// No metadata fabrication: real delete advances HEAD while retaining the
			// old fs_object, blocks and permanent references for version retention.
			if leg != "same-content" {
				rec := e16FileRequest(fx, http.MethodDelete, "", nil, fx.handler.DeleteFile)
				if rec.Code != http.StatusOK {
					t.Fatalf("productive delete: %d %s", rec.Code, rec.Body.String())
				}
				gone, err := helper.TraverseToPath(fx.repoID, "/"+fx.filename)
				if err == nil && gone.TargetEntry != nil {
					t.Fatal("deleted version still in current HEAD")
				}
			}
			fx.headBefore = borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
			if leg != "same-content" && fx.headBefore == historyHead {
				t.Fatal("delete did not advance HEAD")
			}
			readLayout()
			for _, b := range blocks {
				e16AssertOnlyHistoricalRef(t, b, permanent)
			}
			if leg == "skip" {
				competitor := e16FileRequest(fx, http.MethodPost, "?p=/"+url.QueryEscape(fx.filename), nil, fx.handler.CreateFile)
				if competitor.Code != http.StatusCreated {
					t.Fatalf("skip competitor: %d %s", competitor.Code, competitor.Body.String())
				}
				fx.headBefore = borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
			}
			trace := e13Observer(fx.orgID, fx.blockID, "writer")
			writerDB := w2EvidenceSession(t, splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042")[0], trace)
			fx.handler = newBorrowedFSHeadHandler(t, writerDB, x1StorageClass(t))
			historicalVisits, headVisits := 0, 0
			competingHead := ""
			gcLeg := leg == "retained-history-gc" || leg == "multiblock-retained-history-gc" || leg == "head-conflict"
			t.Cleanup(v2pkg.SetRevertFilePublicationBarriersForTest(fx.repoID, func(observed string) {
				historicalVisits++
				if observed != fsID {
					t.Fatalf("actual oldEntry.ID=%s expected=%s", observed, fsID)
				}
				fx.assertHeadUnchanged(t)
				readLayout()
				for _, b := range blocks {
					e16AssertOnlyHistoricalRef(t, b, permanent)
					if gcLeg {
						e16AssertHistoricalPinBlocksGC(t, b)
					}
				}
				if rows := w2Repairs(t, fx); len(rows) != 0 {
					t.Fatalf("unexpected pre-HEAD repair: %+v", rows)
				}
			}, func() {
				headVisits++
				if leg == "head-conflict" && headVisits == 1 {
					rec := e16FileRequest(fx, http.MethodPost, "?p=/e16-competitor.txt", nil, newBorrowedFSHeadHandler(t, database, x1StorageClass(t)).CreateFile)
					if rec.Code != http.StatusCreated {
						t.Fatalf("competing actual CreateFile: %d %s", rec.Code, rec.Body.String())
					}
					competingHead = borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
					if competingHead == fx.headBefore {
						t.Fatal("competing HEAD did not win")
					}
				}
			}))
			policy := "replace"
			if leg == "skip" {
				policy = "skip"
			}
			rec := e16FileRequest(fx, http.MethodPost, "", url.Values{"commit_id": {historyHead}, "conflict_policy": {policy}}, fx.handler.RevertFile)
			if rec.Code != http.StatusOK {
				t.Fatalf("revert: %d %s", rec.Code, rec.Body.String())
			}
			expectedHeads := 1
			if leg == "head-conflict" {
				expectedHeads = 2
			}
			if leg == "same-content" || leg == "skip" {
				expectedHeads = 0
			}
			if historicalVisits != 1 || headVisits != expectedHeads {
				t.Fatalf("actual visits historical=%d HEAD=%d expected=%d", historicalVisits, headVisits, expectedHeads)
			}
			if expectedHeads == 0 {
				fx.assertHeadUnchanged(t)
				expectedMessage := "file already has the same content"
				if leg == "skip" {
					expectedMessage = "skipped"
				}
				if !strings.Contains(rec.Body.String(), expectedMessage) {
					t.Fatalf("no-op response: %s", rec.Body.String())
				}
			} else {
				fx.assertHeadAdvanced(t)
				w24AssertHeadReaches(t, fx, borrowedFSReadHead(t, database, fx.orgID, fx.repoID), fsID)
				if leg == "head-conflict" {
					var parent string
					head := borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
					if err := database.Session().Query(`SELECT parent_id FROM commits WHERE library_id = ? AND commit_id = ?`, fx.repoID, head).Scan(&parent); err != nil || parent != competingHead {
						t.Fatalf("actual CAS retry parent=%s competing=%s err=%v", parent, competingHead, err)
					}
					entries, err := helper.GetDirectoryEntries(fx.repoID, func() string {
						s, err := helper.GetLibraryHeadSnapshot(fx.repoID)
						if err != nil {
							t.Fatal(err)
						}
						return s.RootFSID
					}())
					if err != nil || v2pkg.FindEntryInList(entries, "e16-competitor.txt") == nil {
						t.Fatalf("retry lost competing file: %v", err)
					}
				}
			}
			readLayout()
			for _, b := range blocks {
				e16AssertOnlyHistoricalRef(t, b, permanent)
				if b.readTarget(t) != b.target {
					t.Fatal("revert changed original P")
				}
				w2AssertBytes(t, b)
				e12AssertNoDeleteLifecycle(t, b)
			}
			if rows := w2Repairs(t, fx); len(rows) != 0 {
				t.Fatalf("unexpected repair: %+v", rows)
			}
			trace.mu.Lock()
			writes, events, errs := trace.repairWrites, append([]string(nil), trace.settlementWrites...), append([]string(nil), trace.queryErrors...)
			trace.mu.Unlock()
			if writes != 0 || len(events) != 0 || len(errs) != 0 {
				t.Fatalf("current revert publication trace: repairs=%d events=%v errors=%v", writes, events, errs)
			}
			t.Logf("E1-6 %s: historical fs=%s; %d exact P unchanged; HEAD attempts=%d; retained fs: protects GC=%t; no invented zero proof, no COMMITTED/TERMINAL evidence", leg, fsID, len(blocks), headVisits, gcLeg)
			if !t.Failed() {
				e16Evidence[leg] = true
			}
		})
	}
}

func e16AssertOnlyHistoricalRef(t *testing.T, fx *w2CreateFileFixture, permanent string) {
	t.Helper()
	refs, err := fx.database.ListBlockReferrers(fx.orgID, fx.blockID)
	if err != nil || len(refs) != 1 || refs[0] != permanent {
		t.Fatalf("only retained historical fs: required: %v err=%v", refs, err)
	}
}

// Observe the worker's abort-early LOCAL_QUORUM reference read. A positive
// result aborts before claim/global proof; its candidate is settled, not postponed.
type e16WorkerObserver struct {
	*e13ProofObserver
	localRows []int
}

func (o *e16WorkerObserver) ObserveQuery(ctx context.Context, q gocql.ObservedQuery) {
	o.e13ProofObserver.ObserveQuery(ctx, q)
	statement := strings.ToLower(strings.Join(strings.Fields(q.Statement), " "))
	if strings.HasPrefix(statement, "select referrer from block_references ") && len(q.Values) == 2 && fmt.Sprint(q.Values[0]) == o.org && fmt.Sprint(q.Values[1]) == o.block && q.Query.GetConsistency() == gocql.LocalQuorum {
		o.mu.Lock()
		if q.Err == nil {
			o.localRows = append(o.localRows, q.Rows)
		} else {
			o.queryErrors = append(o.queryErrors, q.Err.Error())
		}
		o.mu.Unlock()
	}
}

func e16AssertHistoricalPinBlocksGC(t *testing.T, fx *w2CreateFileFixture) {
	t.Helper()
	proof := &e16WorkerObserver{e13ProofObserver: e13Observer(fx.orgID, fx.blockID, "retained-history")}
	workerDB := w2EvidenceSession(t, splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042")[0], proof)
	store := gcpkg.NewCassandraStore(workerDB)
	c := w2Candidate(t, gcpkg.NewCassandraStore(fx.database), fx.orgUUID, fx.blockID, fx.target.StorageClass)
	scope := &w2ClosureOwnedQueue{GCStore: store, org: fx.orgUUID, block: fx.blockID, identity: c.Identity()}
	if n, err := w2Worker(t, scope, fx.target.StorageClass).ProcessOrgOnce(t.Context(), fx.orgUUID); err != nil || n != 1 || scope.visited {
		t.Fatalf("productive GC skip n=%d globalProbeVisited=%t err=%v", n, scope.visited, err)
	}
	proof.mu.Lock()
	local := append([]int(nil), proof.localRows...)
	refs := append([]int(nil), proof.refs...)
	errs := append([]string(nil), proof.queryErrors...)
	buckets := len(proof.buckets)
	proof.mu.Unlock()
	if len(local) != 1 || local[0] != 1 || len(refs) != 0 || len(errs) != 0 || buckets != 0 {
		t.Fatalf("real local positive probe must skip before global proof: localRows=%v globalRows=%v errors=%v repairBuckets=%d", local, refs, errs, buckets)
	}
	if _, found, err := store.GetBlockGCCandidateExact(fx.orgUUID, fx.blockID, c.Identity()); err != nil || found {
		t.Fatalf("positive-reference candidate must settle: found=%t err=%v", found, err)
	}
	// Independent observation of global liveness; this is not a worker zero-proof
	// read and cannot be presented as a claimed pre-D handoff interleaving.
	if live, err := gcpkg.NewCassandraStore(fx.database).BlockPublicationLivenessGlobal(fx.orgUUID, fx.blockID); err != nil || live != dbpkg.BlockPublicationRealReference {
		t.Fatalf("retained historical global liveness=%v err=%v", live, err)
	}
	e12AssertNoDeleteLifecycle(t, fx)
	x1AssertCanonicalPresent(t, store, fx.orgUUID, fx.blockID, fx.target.StorageKey)
	w2AssertBytes(t, fx)
}

func e16FileRequest(fx *w2CreateFileFixture, method, query string, form url.Values, handler func(*gin.Context)) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	if query == "" {
		query = "?p=/" + url.QueryEscape(fx.filename)
	}
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	c.Request = httptest.NewRequest(method, "/api/v2.1/repos/"+fx.repoID+"/file/"+query, body)
	if form != nil {
		c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	c.Params = gin.Params{{Key: "repo_id", Value: fx.repoID}}
	c.Set("org_id", fx.orgID)
	c.Set("user_id", fx.userID)
	handler(c)
	return rec
}

func e16UploadHistory(t *testing.T, fx *w2CreateFileFixture, content []byte, chunked bool) {
	t.Helper()
	e16UploadHistoryAt(t, fx, content, chunked, "/")
}

func e16UploadHistoryAt(t *testing.T, fx *w2CreateFileFixture, content []byte, chunked bool, parent string) {
	t.Helper()
	s3 := newVerificationS3Store(t)
	manager := storage.NewManager()
	manager.SetDefaultClass(x1StorageClass(t))
	manager.RegisterBackend(x1StorageClass(t), s3, "")
	tokens := dbpkg.NewTokenStore(fx.database, time.Hour)
	token, err := tokens.CreateUploadToken(fx.orgID, fx.repoID, parent, fx.userID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := tokens.DeleteToken(token); err != nil {
			t.Error(err)
		}
	})
	cfg := &config.Config{Storage: config.StorageConfig{DefaultClass: x1StorageClass(t)}}
	handler := apipkg.NewSeafHTTPHandler(s3, manager, fx.database, apipkg.NewCassandraTokenAdapter(tokens), cfg, middleware.NewPermissionMiddleware(fx.database))
	router := gin.New()
	handler.RegisterSeafHTTPRoutes(router)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("parent_dir", parent); err != nil {
		t.Fatal(err)
	}
	if chunked {
		if err := form.WriteField("resumableIdentifier", "e16-"+fx.blockID); err != nil {
			t.Fatal(err)
		}
	}
	part, err := form.CreateFormFile("file", fx.filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/seafhttp/upload-api/"+token, &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	if chunked {
		req.Header.Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(content)-1, len(content)))
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != sha1hex(content) {
		t.Fatalf("productive historical upload: %d %s", rec.Code, rec.Body.String())
	}
}

func TestE16RevertFileEvidenceRequiresEveryNamedLeg(t *testing.T) {
	// Keep the expected contract independent of the suite's iteration list: a
	// dropped leg must not silently reduce both the run and its completion gate.
	required := []string{"normal", "retained-history-gc", "multiblock-retained-history-gc", "head-conflict", "same-content", "skip"}
	all := map[string]bool{}
	for _, leg := range required {
		all[leg] = true
	}
	if len(e16Legs) != len(required) || len(e16Missing(nil)) != len(required) {
		t.Fatal("the six-leg contract must not shrink")
	}
	if missing := e16Missing(all); len(missing) != 0 {
		t.Fatalf("complete evidence missing=%v", missing)
	}
	for _, leg := range required {
		delete(all, leg)
		if missing := e16Missing(all); len(missing) != 1 || missing[0] != leg {
			t.Fatalf("missing %s must be reported: %v", leg, missing)
		}
		all[leg] = true
	}
}
