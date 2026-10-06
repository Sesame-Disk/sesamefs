//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/traffic"
	"github.com/gin-gonic/gin"
)

const e110cEvidenceEnv = "SESAMEFS_REQUIRE_E110C_REVERTDIR_CHARACTERIZATION"

var e110cEvidence = map[string]bool{}
var e110cLegs = []string{"normal", "retained-history-gc", "head-conflict"}

func e110cMissing(observed map[string]bool) []string {
	var missing []string
	for _, leg := range e110cLegs {
		if !observed[leg] {
			missing = append(missing, leg)
		}
	}
	return missing
}

// No historical fs: is removed. The measured contract is retained-history only.
func TestRevertDirectoryRetainedHistory(t *testing.T) {
	if endpoint := os.Getenv("SESAMEFS_E110C_ISOLATED_URL"); endpoint != "" && os.Getenv("SESAMEFS_E110C_CHILD") != "1" {
		e110cRunIsolated(t, endpoint)
		return
	}
	requireCassandra(t)
	if os.Getenv("SESAMEFS_TEST_IN_CONTAINER") != "1" {
		t.Fatal("E1-10c requires the controlled Docker evidence fleet")
	}
	for _, endpoint := range []string{superadminClient.baseURL, envOrDefault("SESAMEFS_URL_2", "http://sesamefs-node-2:8080"), envOrDefault("SESAMEFS_URL_3", "http://sesamefs-node-3:8080")} {
		if err := e19CheckGCDisabled(newTestClient(endpoint, superadminClient.token)); err != nil {
			t.Fatalf("E1-10c owned-worker isolation: %v", err)
		}
	}
	for _, leg := range e110cLegs {
		t.Run(leg, func(t *testing.T) {
			database := shareProjectionDBForTest(t)
			fx := &w2CreateFileFixture{w2UploadFileFixture: newW2UploadFileFixture(t, database, newBorrowedFSHeadHandler(t, database, x1StorageClass(t)))}
			dirPath := "/old-dir"
			created := e16FileRequest(fx, http.MethodPost, "?p="+dirPath, nil, fx.handler.CreateDirectory)
			if created.Code != http.StatusCreated {
				t.Fatalf("productive CreateDirectory: %d %s", created.Code, created.Body.String())
			}
			e16UploadHistoryAt(t, fx, fx.content, false, dirPath)
			historyHead := borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
			helper := v2pkg.NewFSHelper(database)
			old, err := helper.TraverseToPath(fx.repoID, dirPath)
			if err != nil || old.TargetEntry == nil {
				t.Fatalf("historical entry: %v", err)
			}
			dirID := old.TargetEntry.ID
			if old.TargetEntry.Mode != v2pkg.ModeDir {
				t.Fatalf("historical entry must be directory: %+v", old.TargetEntry)
			}
			child, err := helper.TraverseToPath(fx.repoID, dirPath+"/"+fx.filename)
			if err != nil || child.TargetEntry == nil {
				t.Fatalf("historical child: %v", err)
			}
			fsID := child.TargetEntry.ID
			snapshot, err := helper.GetLibraryHeadSnapshot(fx.repoID)
			if err != nil {
				t.Fatal(err)
			}
			historicalRoot := snapshot.RootFSID

			readTree := func(root string) {
				t.Helper()
				directory, err := helper.TraverseToPathFromRoot(fx.repoID, root, dirPath)
				if err != nil || directory.TargetEntry == nil || directory.TargetEntry.ID != dirID || directory.TargetEntry.Mode != v2pkg.ModeDir {
					t.Fatalf("named directory chain: %+v %v", directory, err)
				}
				entries, err := helper.GetDirectoryEntries(fx.repoID, dirID)
				if err != nil || len(entries) != 1 || entries[0].Name != fx.filename || entries[0].ID != fsID || entries[0].Mode == v2pkg.ModeDir {
					t.Fatalf("exact single-file subtree: %+v %v", entries, err)
				}
				child, err := helper.TraverseToPathFromRoot(fx.repoID, root, dirPath+"/"+fx.filename)
				if err != nil || child.TargetEntry == nil || child.TargetEntry.ID != fsID {
					t.Fatalf("named child chain: %+v %v", child, err)
				}
				var internal, external []string
				var size int64
				if err := database.Session().Query(`SELECT block_ids, seafile_block_ids_sha1, size_bytes FROM fs_objects WHERE library_id = ? AND fs_id = ?`, fx.repoID, fsID).Scan(&internal, &external, &size); err != nil {
					t.Fatal(err)
				}
				if len(internal) != 1 || internal[0] != fx.blockID || len(external) != 1 || external[0] != fx.sha1ID || size != int64(len(fx.content)) {
					t.Fatalf("historical file layout: %v %v %d", internal, external, size)
				}
			}
			readHistory := func() {
				t.Helper()
				var root string
				if err := database.Session().Query(`SELECT root_fs_id FROM commits WHERE library_id = ? AND commit_id = ?`, fx.repoID, historyHead).Scan(&root); err != nil || root != historicalRoot {
					t.Fatalf("retained commit/root: %s %v", root, err)
				}
				readTree(root)
			}

			readHistory()
			fx.target = fx.readTarget(t)
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
				t.Fatalf("productive upload accounting prerequisite: %+v", before)
			}
			deletion := &e110bDeleteObserver{repo: fx.repoID, scope: traffic.LibraryStorageScope(fx.orgID, fx.repoID), bytes: int64(len(fx.content))}
			deleteDB := w2EvidenceSession(t, splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042")[0], deletion)
			deleteHandler := newBorrowedFSHeadHandler(t, deleteDB, x1StorageClass(t))
			// This fixture contains no tags. Observe the final empty tag scan and the
			// final daily/library decrement; both async paths have no further DB work.
			deleted := e16FileRequest(fx, http.MethodDelete, "?p="+dirPath, nil, deleteHandler.DeleteDirectory)
			if deleted.Code != http.StatusOK {
				t.Fatalf("productive DeleteDirectory: %d %s", deleted.Code, deleted.Body.String())
			}
			waitForIntegrationCondition(t, "productive directory-delete async work completes", deletion.complete)
			zero := traffic.ReadStorageSnapshot(database, traffic.LibraryStorageScope(fx.orgID, fx.repoID))
			if zero.BytesUsed != 0 || zero.FileCount != 0 {
				t.Fatalf("delete accounting has not settled: %+v", zero)
			}
			deletedSnapshot, err := helper.GetLibraryHeadSnapshot(fx.repoID)
			if err != nil {
				t.Fatalf("deleted HEAD snapshot: %v", err)
			}
			// GetDirectoryEntries deliberately self-heals a missing row as empty.
			// Require the persisted root itself before certifying target absence.
			var deletedEntriesJSON string
			if err := database.Session().Query(`SELECT dir_entries FROM fs_objects WHERE library_id = ? AND fs_id = ?`, fx.repoID, deletedSnapshot.RootFSID).Scan(&deletedEntriesJSON); err != nil {
				t.Fatalf("persisted deleted HEAD root: %v", err)
			}
			var deletedEntries []v2pkg.FSEntry
			if err := json.Unmarshal([]byte(deletedEntriesJSON), &deletedEntries); err != nil {
				t.Fatalf("persisted deleted HEAD entries: %v", err)
			}
			if v2pkg.FindEntryInList(deletedEntries, "old-dir") != nil {
				t.Fatal("directory remains reachable after delete")
			}

			fx.headBefore = borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
			if fx.headBefore == historyHead {
				t.Fatal("delete did not advance HEAD")
			}
			readHistory()
			e16AssertOnlyHistoricalRef(t, fx, permanent)
			trace := e13Observer(fx.orgID, fx.blockID, "revert-directory-writer")
			writerDB := w2EvidenceSession(t, splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042")[0], trace)
			handler := newBorrowedFSHeadHandler(t, writerDB, x1StorageClass(t))
			historicalVisits, headVisits := 0, 0
			competingHead := ""
			t.Cleanup(v2pkg.SetRevertDirectoryPublicationBarriersForTest(fx.repoID, func(observed string) {
				historicalVisits++
				if observed != dirID {
					t.Fatalf("actual oldEntry.ID=%s expected=%s", observed, dirID)
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
			}, func() {
				headVisits++
				if leg == "head-conflict" && headVisits == 1 {
					rec := e16FileRequest(fx, http.MethodPost, "?p=/e110c-competitor.txt", nil, fx.handler.CreateFile)
					if rec.Code != http.StatusCreated {
						t.Fatalf("competing CreateFile: %d %s", rec.Code, rec.Body.String())
					}
					competingHead = borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
					if competingHead == fx.headBefore {
						t.Fatal("competitor did not advance HEAD")
					}
				}
			}))
			body, err := json.Marshal(map[string]string{"commit_id": historyHead, "conflict_policy": ""})
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v2.1/repos/"+fx.repoID+"/dir/?operation=revert&p="+dirPath, bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Params = gin.Params{{Key: "repo_id", Value: fx.repoID}}
			c.Set("org_id", fx.orgID)
			c.Set("user_id", fx.userID)
			handler.RevertDirectory(c)
			if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"success":true}` {
				t.Fatalf("revert response: %d %s", rec.Code, rec.Body.String())
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
			var winningRoot string
			if err := database.Session().Query(`SELECT root_fs_id FROM commits WHERE library_id = ? AND commit_id = ?`, fx.repoID, head).Scan(&winningRoot); err != nil {
				t.Fatal(err)
			}
			readTree(winningRoot)
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
				if err != nil || v2pkg.FindEntryInList(entries, "e110c-competitor.txt") == nil {
					t.Fatalf("retry lost competitor: %v", err)
				}
			}
			readHistory()
			e16AssertOnlyHistoricalRef(t, fx, permanent)
			if fx.readTarget(t) != fx.target {
				t.Fatal("revert changed original exact P")
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
				t.Fatalf("revert trace: repair writes=%d settlement=%v errors=%v", writes, events, errs)
			}
			t.Logf("E1-10c %s: historical DIR=%s FILE=%s exact P=%+v unchanged; HEAD attempts=%d; inherited child fs: only; no retirement certificate", leg, dirID, fsID, fx.target, headVisits)
			if !t.Failed() {
				e110cEvidence[leg] = true
			}
		})
	}
}

func TestRevertDirectoryRetainedHistoryEvidenceRequiresEveryNamedLeg(t *testing.T) {
	required := []string{"normal", "retained-history-gc", "head-conflict"}
	all := map[string]bool{}
	for _, leg := range required {
		all[leg] = true
	}
	if len(e110cLegs) != len(required) || len(e110cMissing(nil)) != len(required) {
		t.Fatal("three-leg contract must not shrink")
	}
	if missing := e110cMissing(all); len(missing) != 0 {
		t.Fatalf("complete evidence missing=%v", missing)
	}
	for _, leg := range required {
		delete(all, leg)
		if missing := e110cMissing(all); len(missing) != 1 || missing[0] != leg {
			t.Fatalf("missing %s must be reported: %v", leg, missing)
		}
		all[leg] = true
	}
}

// Use the existing manual-GC proof keyspace; the shared dev daemon stays active.
// The same binary retains race instrumentation and the child's required gate.
func e110cRunIsolated(t *testing.T, endpoint string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	childRun := "^TestRevertDirectoryRetainedHistory"
	// Preserve subtest filters: a normal-only run must not secretly run all legs.
	if _, sub, ok := strings.Cut(flag.Lookup("test.run").Value.String(), "/"); ok {
		childRun += "/" + sub
	}
	cmd := exec.CommandContext(ctx, binary, "-test.run="+childRun, "-test.v", "-test.count=1", "-test.timeout=3m")
	for _, entry := range os.Environ() {
		name := strings.SplitN(entry, "=", 2)[0]
		if strings.HasPrefix(name, "SESAMEFS_REQUIRE_") || name == "SESAMEFS_URL" || name == "SESAMEFS_URL_2" || name == "SESAMEFS_URL_3" || name == "CASSANDRA_KEYSPACE" || name == "SESAMEFS_E110C_CHILD" || name == "SESAMEFS_E110B_CHILD" || name == "SESAMEFS_E110_CHILD" || name == "SESAMEFS_E19_CHILD" || name == "SESAMEFS_W2_PROCESS_CHILD" {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, e110cEvidenceEnv+"=1", "SESAMEFS_E110C_CHILD=1", "CASSANDRA_KEYSPACE=sesamefs_e19", "SESAMEFS_URL="+endpoint, "SESAMEFS_URL_2="+endpoint, "SESAMEFS_URL_3="+endpoint)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("isolated E1-10c evidence failed: %v", err)
	}
	for _, name := range e110cMissing(nil) {
		e110cEvidence[name] = true
	}
}
