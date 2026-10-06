//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/traffic"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/gin-gonic/gin"
)

const e110dEvidenceEnv = "SESAMEFS_REQUIRE_E110D_REVERTDIRENTS_CHARACTERIZATION"

var e110dEvidence = map[string]bool{}
var e110dLegs = []string{"normal", "retained-history-gc", "head-conflict"}

func e110dMissing(observed map[string]bool) []string {
	var missing []string
	for _, leg := range e110dLegs {
		if !observed[leg] {
			missing = append(missing, leg)
		}
	}
	return missing
}

// No historical fs: is removed. The measured contract is retained-history only.
func TestRevertDirentsRetainedHistory(t *testing.T) {
	if endpoint := os.Getenv("SESAMEFS_E110D_ISOLATED_URL"); endpoint != "" && os.Getenv("SESAMEFS_E110D_CHILD") != "1" {
		e110dRunIsolated(t, endpoint)
		return
	}
	requireCassandra(t)
	if os.Getenv("SESAMEFS_TEST_IN_CONTAINER") != "1" {
		t.Fatal("E1-10d requires the controlled Docker evidence fleet")
	}
	for _, endpoint := range []string{superadminClient.baseURL, envOrDefault("SESAMEFS_URL_2", "http://sesamefs-node-2:8080"), envOrDefault("SESAMEFS_URL_3", "http://sesamefs-node-3:8080")} {
		if err := e19CheckGCDisabled(newTestClient(endpoint, superadminClient.token)); err != nil {
			t.Fatalf("E1-10d owned-worker isolation: %v", err)
		}
	}
	for _, leg := range e110dLegs {
		t.Run(leg, func(t *testing.T) {
			database := shareProjectionDBForTest(t)
			fx := &w2CreateFileFixture{w2UploadFileFixture: newW2UploadFileFixture(t, database, newBorrowedFSHeadHandler(t, database, x1StorageClass(t)))}
			e16UploadHistory(t, fx, fx.content, false)
			historyHead := borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
			helper := v2pkg.NewFSHelper(database)
			old, err := helper.TraverseToPath(fx.repoID, "/"+fx.filename)
			if err != nil || old.TargetEntry == nil {
				t.Fatalf("historical entry: %v", err)
			}
			fsID := old.TargetEntry.ID
			snapshot, err := helper.GetLibraryHeadSnapshot(fx.repoID)
			if err != nil {
				t.Fatal(err)
			}
			historicalRoot := snapshot.RootFSID
			readHistory := func() {
				t.Helper()
				var root string
				if err := database.Session().Query(`SELECT root_fs_id FROM commits WHERE library_id = ? AND commit_id = ?`, fx.repoID, historyHead).Scan(&root); err != nil || root != historicalRoot {
					t.Fatalf("retained commit/root: %s %v", root, err)
				}
				entry, err := helper.TraverseToPathFromRoot(fx.repoID, root, "/"+fx.filename)
				if err != nil || entry.TargetEntry == nil || entry.TargetEntry.ID != fsID {
					t.Fatalf("retained historical tree: %+v %v", entry, err)
				}
				var internal, external []string
				var size int64
				if err := database.Session().Query(`SELECT block_ids, seafile_block_ids_sha1, size_bytes FROM fs_objects WHERE library_id = ? AND fs_id = ?`, fx.repoID, fsID).Scan(&internal, &external, &size); err != nil {
					t.Fatal(err)
				}
				if len(internal) != 1 || internal[0] != fx.blockID || len(external) != 1 || external[0] != fx.sha1ID || size != int64(len(fx.content)) {
					t.Fatalf("historical layout: %v %v %d", internal, external, size)
				}
			}
			readHistory()
			fx.target = fx.readTarget(t)
			w2AssertBytes(t, fx)
			permanent := dbpkg.BlockReferrerForFSObject(fx.repoID, fsID)
			refs, err := database.ListBlockReferrers(fx.orgID, fx.blockID)
			if err != nil {
				t.Fatal(err)
			}
			own := 0
			for _, ref := range refs {
				if strings.HasPrefix(ref, "up:") {
					own++
					// Keep cleanup ownership even after the reference itself is lapsed.
					t.Cleanup(func() {
						if err := database.DeleteProvisionalBlockReferenceExpiry(fx.orgID, fx.blockID, ref, time.Time{}); err != nil {
							t.Error(err)
						}
					})
					// Explicit expiry-state control on this isolated upload, not elapsed TTL.
					if err := database.RemoveBlockReference(fx.orgID, fx.blockID, ref); err != nil {
						t.Fatal(err)
					}
				}
			}
			if own != 1 {
				t.Fatalf("expected one own upload pin: %v", refs)
			}
			e16AssertOnlyHistoricalRef(t, fx, permanent)
			if rows := w2Repairs(t, fx); len(rows) != 0 {
				t.Fatalf("unsettled upload: %+v", rows)
			}
			before := traffic.ReadStorageSnapshot(database, traffic.LibraryStorageScope(fx.orgID, fx.repoID))
			if before.BytesUsed != int64(len(fx.content)) || before.FileCount != 1 {
				t.Fatalf("productive upload accounting: %+v", before)
			}
			deletion := &e110dDeleteObserver{repo: fx.repoID, itemPath: "/" + fx.filename, scope: traffic.LibraryStorageScope(fx.orgID, fx.repoID), bytes: int64(len(fx.content))}
			deleteDB := w2EvidenceSession(t, splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042")[0], deletion)
			deleteHandler := newBorrowedFSHeadHandler(t, deleteDB, x1StorageClass(t))
			deleted := e16FileRequest(fx, http.MethodDelete, "", nil, deleteHandler.DeleteFile)
			if deleted.Code != http.StatusOK {
				t.Fatalf("productive delete: %d %s", deleted.Code, deleted.Body.String())
			}
			// This fixture has no tags. Both final productive async queries must complete.
			waitForIntegrationCondition(t, "file deletion housekeeping completes", deletion.complete)
			zero := traffic.ReadStorageSnapshot(database, traffic.LibraryStorageScope(fx.orgID, fx.repoID))
			if zero.BytesUsed != 0 || zero.FileCount != 0 {
				t.Fatalf("delete accounting: %+v", zero)
			}
			deletedSnapshot, err := helper.GetLibraryHeadSnapshot(fx.repoID)
			if err != nil {
				t.Fatal(err)
			}
			var deletedJSON string
			if err := database.Session().Query(`SELECT dir_entries FROM fs_objects WHERE library_id = ? AND fs_id = ?`, fx.repoID, deletedSnapshot.RootFSID).Scan(&deletedJSON); err != nil {
				t.Fatal(err)
			}
			var deletedEntries []v2pkg.FSEntry
			if err := json.Unmarshal([]byte(deletedJSON), &deletedEntries); err != nil {
				t.Fatal(err)
			}
			if v2pkg.FindEntryInList(deletedEntries, fx.filename) != nil {
				t.Fatal("file remains in persisted HEAD root")
			}
			fx.headBefore = borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
			if fx.headBefore == historyHead {
				t.Fatal("delete did not advance HEAD")
			}
			readHistory()
			e16AssertOnlyHistoricalRef(t, fx, permanent)
			trace := e13Observer(fx.orgID, fx.blockID, "dirents-writer")
			writerDB := w2EvidenceSession(t, splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042")[0], trace)
			handler := v2pkg.NewTrashHandler(writerDB)
			historicalVisits, headVisits := 0, 0
			competingHead := ""
			t.Cleanup(v2pkg.SetRevertDirentsPublicationBarriersForTest(fx.repoID, func(itemPath, observed string) {
				historicalVisits++
				if itemPath != "/"+fx.filename || observed != fsID {
					t.Fatalf("actual oldEntry.ID=%s expected=%s", observed, fsID)
				}
				fx.assertHeadUnchanged(t)
				readHistory()
				e16AssertOnlyHistoricalRef(t, fx, permanent)
				if leg != "normal" {
					e16AssertHistoricalPinBlocksGC(t, fx)
				}
				if rows := w2Repairs(t, fx); len(rows) != 0 {
					t.Fatalf("unexpected pre-HEAD repair: %+v", rows)
				}
			}, func(itemPath string) {
				if itemPath != "/"+fx.filename {
					t.Fatalf("HEAD item path %q", itemPath)
				}
				headVisits++
				if leg == "head-conflict" && headVisits == 1 {
					rec := e16FileRequest(fx, http.MethodPost, "?p=/e110d-competitor.txt", nil, fx.handler.CreateFile)
					if rec.Code != http.StatusCreated {
						t.Fatalf("competing CreateFile: %d %s", rec.Code, rec.Body.String())
					}
					competingHead = borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
					if competingHead == fx.headBefore {
						t.Fatal("competitor did not advance HEAD")
					}
				}
			}))
			body := url.Values{"commit_id": {historyHead}, "path": {"/" + fx.filename}}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v2.1/repos/"+fx.repoID+"/trash/revert-dirents/", strings.NewReader(body.Encode()))
			c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			c.Params = gin.Params{{Key: "repo_id", Value: fx.repoID}}
			c.Set("org_id", fx.orgID)
			c.Set("user_id", fx.userID)
			handler.RevertDirents(c)
			var response struct {
				Success []struct {
					Path  string `json:"path"`
					IsDir bool   `json:"is_dir"`
				} `json:"success"`
				Failed []json.RawMessage `json:"failed"`
			}
			if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &response) != nil || response.Success == nil || response.Failed == nil || len(response.Success) != 1 || len(response.Failed) != 0 || response.Success[0].Path != "/"+fx.filename || response.Success[0].IsDir {
				t.Fatalf("dirent response: %d %s", rec.Code, rec.Body.String())
			}
			expectedHeads := 1
			if leg == "head-conflict" {
				expectedHeads = 2
			}
			if historicalVisits != 1 || headVisits != expectedHeads {
				t.Fatalf("actual scheduling: historical=%d HEAD=%d expected=%d", historicalVisits, headVisits, expectedHeads)
			}
			fx.assertHeadAdvanced(t)
			head := borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
			w24AssertHeadReaches(t, fx, head, fsID)
			winning, err := helper.GetLibraryHeadSnapshot(fx.repoID)
			if err != nil {
				t.Fatal(err)
			}
			entry, err := helper.TraverseToPathFromRoot(fx.repoID, winning.RootFSID, "/"+fx.filename)
			if err != nil || entry.TargetEntry == nil || entry.TargetEntry.ID != fsID || entry.TargetEntry.Mode == v2pkg.ModeDir {
				t.Fatalf("winning named file chain: %+v %v", entry, err)
			}
			var parent string
			if err := database.Session().Query(`SELECT parent_id FROM commits WHERE library_id = ? AND commit_id = ?`, fx.repoID, head).Scan(&parent); err != nil {
				t.Fatal(err)
			}
			expectedParent := fx.headBefore
			if leg == "head-conflict" {
				expectedParent = competingHead
			}
			if parent != expectedParent {
				t.Fatalf("winning commit parent=%s expected=%s", parent, expectedParent)
			}
			if leg == "head-conflict" {
				current, err := helper.GetLibraryHeadSnapshot(fx.repoID)
				if err != nil {
					t.Fatal(err)
				}
				entries, err := helper.GetDirectoryEntries(fx.repoID, current.RootFSID)
				if err != nil || v2pkg.FindEntryInList(entries, "e110d-competitor.txt") == nil {
					t.Fatalf("retry lost competitor: %v", err)
				}
			}
			readHistory()
			e16AssertOnlyHistoricalRef(t, fx, permanent)
			if fx.readTarget(t) != fx.target {
				t.Fatal("dirents revert changed original exact P")
			}
			w2AssertBytes(t, fx)
			e12AssertNoDeleteLifecycle(t, fx)
			if rows := w2Repairs(t, fx); len(rows) != 0 {
				t.Fatalf("unexpected repair: %+v", rows)
			}
			trace.mu.Lock()
			writes, events, errs := trace.repairWrites, append([]string(nil), trace.settlementWrites...), append([]string(nil), trace.queryErrors...)
			trace.mu.Unlock()
			if writes != 0 || len(events) != 0 || len(errs) != 0 {
				t.Fatalf("dirents revert trace: repair writes=%d settlement=%v errors=%v", writes, events, errs)
			}
			t.Logf("E1-10d %s: historical fs=%s exact P=%+v unchanged; HEAD attempts=%d; inherited fs: only; no retirement certificate", leg, fsID, fx.target, headVisits)
			if !t.Failed() {
				e110dEvidence[leg] = true
			}
		})
	}
}

func TestRevertDirentsRetainedHistoryEvidenceRequiresEveryNamedLeg(t *testing.T) {
	required := []string{"normal", "retained-history-gc", "head-conflict"}
	all := map[string]bool{}
	for _, leg := range required {
		all[leg] = true
	}
	if len(e110dLegs) != len(required) || len(e110dMissing(nil)) != len(required) {
		t.Fatal("three-leg contract must not shrink")
	}
	if missing := e110dMissing(all); len(missing) != 0 {
		t.Fatalf("complete evidence missing=%v", missing)
	}
	for _, leg := range required {
		delete(all, leg)
		if missing := e110dMissing(all); len(missing) != 1 || missing[0] != leg {
			t.Fatalf("missing %s must be reported: %v", leg, missing)
		}
		all[leg] = true
	}
}

// Use the existing manual-GC proof keyspace; the shared dev daemon stays active.
// The same binary retains race instrumentation and the child's required gate.
func e110dRunIsolated(t *testing.T, endpoint string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	childRun := "^TestRevertDirentsRetainedHistory"
	// Preserve subtest filters: a normal-only run must not secretly run all legs.
	if _, sub, ok := strings.Cut(flag.Lookup("test.run").Value.String(), "/"); ok {
		childRun += "/" + sub
	}
	cmd := exec.CommandContext(ctx, binary, "-test.run="+childRun, "-test.v", "-test.count=1", "-test.timeout=3m")
	for _, entry := range os.Environ() {
		name := strings.SplitN(entry, "=", 2)[0]
		if strings.HasPrefix(name, "SESAMEFS_REQUIRE_") || name == "SESAMEFS_URL" || name == "SESAMEFS_URL_2" || name == "SESAMEFS_URL_3" || name == "CASSANDRA_KEYSPACE" || name == "SESAMEFS_E110D_CHILD" || name == "SESAMEFS_E110C_CHILD" || name == "SESAMEFS_E110B_CHILD" || name == "SESAMEFS_E110_CHILD" || name == "SESAMEFS_E19_CHILD" || name == "SESAMEFS_W2_PROCESS_CHILD" {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, e110dEvidenceEnv+"=1", "SESAMEFS_E110D_CHILD=1", "CASSANDRA_KEYSPACE=sesamefs_e19", "SESAMEFS_URL="+endpoint, "SESAMEFS_URL_2="+endpoint, "SESAMEFS_URL_3="+endpoint)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("isolated E1-10d evidence failed: %v", err)
	}
	for _, name := range e110dMissing(nil) {
		e110dEvidence[name] = true
	}
}

// Observe completed productive queries, never substitute counters or GC state.
type e110dDeleteObserver struct {
	mu                    sync.Mutex
	repo, scope, itemPath string
	bytes                 int64
	tags, counters        bool
	errors                []string
}

func (o *e110dDeleteObserver) ObserveQuery(_ context.Context, q gocql.ObservedQuery) {
	statement := strings.ToLower(strings.Join(strings.Fields(q.Statement), " "))
	tags := strings.HasPrefix(statement, "select tag_id, file_tag_id from file_tags where repo_id = ? and file_path = ?") && len(q.Values) == 2 && fmt.Sprint(q.Values[0]) == o.repo && fmt.Sprint(q.Values[1]) == o.itemPath
	counter := strings.HasPrefix(statement, "update storage_counters set bytes_used = bytes_used + ?, file_count = file_count + ?") && len(q.Values) == 5 && fmt.Sprint(q.Values[2]) == o.scope && fmt.Sprint(q.Values[0]) == fmt.Sprint(-o.bytes) && fmt.Sprint(q.Values[1]) == "-1"
	if !tags && !counter {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if q.Err != nil {
		o.errors = append(o.errors, q.Err.Error())
		return
	}
	if tags && q.Rows == 0 {
		o.tags = true
	}
	if counter {
		if day, ok := q.Values[4].(time.Time); ok && day.Year() > 1970 {
			o.counters = true
		}
	}
}
func (o *e110dDeleteObserver) complete() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.tags && o.counters && len(o.errors) == 0
}
