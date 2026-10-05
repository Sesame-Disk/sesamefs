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
	"github.com/gin-gonic/gin"
)

const e110EvidenceEnv = "SESAMEFS_REQUIRE_E110_RESTORETRASH_CHARACTERIZATION"

var e110Evidence = map[string]bool{}
var e110Legs = []string{"normal", "retained-history-gc", "head-conflict"}

func e110Missing(observed map[string]bool) []string {
	var missing []string
	for _, leg := range e110Legs {
		if !observed[leg] {
			missing = append(missing, leg)
		}
	}
	return missing
}

// No historical fs: is removed. The measured contract is retained-history only.
func TestE110RestoreTrashRetainedHistory(t *testing.T) {
	if endpoint := os.Getenv("SESAMEFS_E110_ISOLATED_URL"); endpoint != "" && os.Getenv("SESAMEFS_E110_CHILD") != "1" {
		e110RunIsolated(t, endpoint)
		return
	}
	requireCassandra(t)
	if os.Getenv("SESAMEFS_TEST_IN_CONTAINER") != "1" {
		t.Fatal("E1-10 requires the controlled Docker evidence fleet")
	}
	for _, endpoint := range []string{superadminClient.baseURL, envOrDefault("SESAMEFS_URL_2", "http://sesamefs-node-2:8080"), envOrDefault("SESAMEFS_URL_3", "http://sesamefs-node-3:8080")} {
		if err := e19CheckGCDisabled(newTestClient(endpoint, superadminClient.token)); err != nil {
			t.Fatalf("E1-10 owned-worker isolation: %v", err)
		}
	}
	for _, leg := range e110Legs {
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
			deleted := e16FileRequest(fx, http.MethodDelete, "", nil, fx.handler.DeleteFile)
			if deleted.Code != http.StatusOK {
				t.Fatalf("productive delete: %d %s", deleted.Code, deleted.Body.String())
			}
			gone, err := helper.TraverseToPath(fx.repoID, "/"+fx.filename)
			if err == nil && gone.TargetEntry != nil {
				t.Fatal("file remains in HEAD after delete")
			}
			fx.headBefore = borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
			if fx.headBefore == historyHead {
				t.Fatal("delete did not advance HEAD")
			}
			readHistory()
			e16AssertOnlyHistoricalRef(t, fx, permanent)
			trace := e13Observer(fx.orgID, fx.blockID, "restore-writer")
			writerDB := w2EvidenceSession(t, splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042")[0], trace)
			handler := v2pkg.NewTrashHandler(writerDB)
			historicalVisits, headVisits := 0, 0
			competingHead := ""
			t.Cleanup(v2pkg.SetRestoreTrashPublicationBarriersForTest(fx.repoID, func(observed string) {
				historicalVisits++
				if observed != fsID {
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
			}, func() {
				headVisits++
				if leg == "head-conflict" && headVisits == 1 {
					rec := e16FileRequest(fx, http.MethodPost, "?p=/e110-competitor.txt", nil, fx.handler.CreateFile)
					if rec.Code != http.StatusCreated {
						t.Fatalf("competing CreateFile: %d %s", rec.Code, rec.Body.String())
					}
					competingHead = borrowedFSReadHead(t, database, fx.orgID, fx.repoID)
					if competingHead == fx.headBefore {
						t.Fatal("competitor did not advance HEAD")
					}
				}
			}))
			body, err := json.Marshal(map[string]string{"commit_id": historyHead, "p": "/" + fx.filename})
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v2.1/repos/"+fx.repoID+"/file/restore/", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Params = gin.Params{{Key: "repo_id", Value: fx.repoID}}
			c.Set("org_id", fx.orgID)
			c.Set("user_id", fx.userID)
			handler.RestoreTrashItem(c)
			if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"success":true}` {
				t.Fatalf("restore response: %d %s", rec.Code, rec.Body.String())
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
				if err != nil || v2pkg.FindEntryInList(entries, "e110-competitor.txt") == nil {
					t.Fatalf("retry lost competitor: %v", err)
				}
			}
			readHistory()
			e16AssertOnlyHistoricalRef(t, fx, permanent)
			if fx.readTarget(t) != fx.target {
				t.Fatal("restore changed original exact P")
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
				t.Fatalf("restore trace: repair writes=%d settlement=%v errors=%v", writes, events, errs)
			}
			t.Logf("E1-10 %s: historical fs=%s exact P=%+v unchanged; HEAD attempts=%d; inherited fs: only; no retirement certificate", leg, fsID, fx.target, headVisits)
			if !t.Failed() {
				e110Evidence[leg] = true
			}
		})
	}
}

func TestE110RestoreTrashEvidenceRequiresEveryNamedLeg(t *testing.T) {
	required := []string{"normal", "retained-history-gc", "head-conflict"}
	all := map[string]bool{}
	for _, leg := range required {
		all[leg] = true
	}
	if len(e110Legs) != len(required) || len(e110Missing(nil)) != len(required) {
		t.Fatal("three-leg contract must not shrink")
	}
	if missing := e110Missing(all); len(missing) != 0 {
		t.Fatalf("complete evidence missing=%v", missing)
	}
	for _, leg := range required {
		delete(all, leg)
		if missing := e110Missing(all); len(missing) != 1 || missing[0] != leg {
			t.Fatalf("missing %s must be reported: %v", leg, missing)
		}
		all[leg] = true
	}
}

// Use the existing manual-GC proof keyspace; the shared dev daemon stays active.
// The same binary retains race instrumentation and the child's required gate.
func e110RunIsolated(t *testing.T, endpoint string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	childRun := "^TestE110"
	// Preserve subtest filters: a normal-only run must not secretly run all legs.
	if _, sub, ok := strings.Cut(flag.Lookup("test.run").Value.String(), "/"); ok {
		childRun += "/" + sub
	}
	cmd := exec.CommandContext(ctx, binary, "-test.run="+childRun, "-test.v", "-test.count=1", "-test.timeout=3m")
	for _, entry := range os.Environ() {
		name := strings.SplitN(entry, "=", 2)[0]
		if strings.HasPrefix(name, "SESAMEFS_REQUIRE_") || name == "SESAMEFS_URL" || name == "SESAMEFS_URL_2" || name == "SESAMEFS_URL_3" || name == "CASSANDRA_KEYSPACE" || name == "SESAMEFS_E110_CHILD" || name == "SESAMEFS_E19_CHILD" || name == "SESAMEFS_W2_PROCESS_CHILD" {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, e110EvidenceEnv+"=1", "SESAMEFS_E110_CHILD=1", "CASSANDRA_KEYSPACE=sesamefs_e19", "SESAMEFS_URL="+endpoint, "SESAMEFS_URL_2="+endpoint, "SESAMEFS_URL_3="+endpoint)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("isolated E1-10 evidence failed: %v", err)
	}
	for _, name := range e110Missing(nil) {
		e110Evidence[name] = true
	}
}
