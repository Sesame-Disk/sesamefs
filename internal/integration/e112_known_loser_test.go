//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

const e112EvidenceEnv = "SESAMEFS_REQUIRE_E112_KNOWN_LOSER_EVIDENCE"

var e112Legs = []string{"normal", "loser-cleanup", "crash-pending", "crash-ttl", "crash-gc", "crash-restart"}
var e112Observed = map[string]bool{}

func e112Missing(seen map[string]bool) []string {
	var missing []string
	for _, leg := range e112Legs {
		if !seen[leg] {
			missing = append(missing, leg)
		}
	}
	return missing
}

// These children bypass TestMain cleanup using the existing process-helper
// mechanism. Only their live parent owns fixtures and evidence accounting.
func TestE112ProcessChild(t *testing.T) {
	if os.Getenv("SESAMEFS_E112_PROCESS") != "1" {
		t.Skip("parent-only process helper")
	}
	var data w2ChildFixture
	if err := json.Unmarshal([]byte(os.Getenv("SESAMEFS_W2_CHILD_FIXTURE")), &data); err != nil {
		t.Fatal(err)
	}
	database := shareProjectionDBForTest(t)
	if data.Phase == "recovery" {
		var classifications []string
		restore := v2pkg.SetRepairAfterClassifyForIntegration(database, data.Repo, func(outcome string, err error) {
			if err != nil {
				t.Errorf("native classification: %v", err)
			}
			classifications = append(classifications, outcome)
		})
		defer restore()
		err := v2pkg.RunPublishedBlockReferenceRepairSweepForIntegration(database)
		if len(classifications) != 1 || classifications[0] != "unknown" {
			t.Fatalf("productive sweep missed exact UNKNOWN repair: %v err=%v", classifications, err)
		}
		if err != nil && !strings.Contains(err.Error(), "unknown; retain queued repair") {
			t.Fatalf("sweep: %v", err)
		}
		if err := os.WriteFile(data.Marker+".recovered", []byte("UNKNOWN retained by productive bucket sweep"), 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	var before, loser sync.Once
	t.Cleanup(v2pkg.SetW2PublicationAfterAuthorityForTest(data.Repo, func() {
		before.Do(func() {
			if err := os.WriteFile(data.Marker+".authority", []byte("final exact-P completed"), 0600); err != nil {
				t.Fatal(err)
			}
			e112AwaitFile(t, data.Marker+".resume")
		})
	}))
	t.Cleanup(v2pkg.SetKnownLoserBeforeCleanupForIntegration(database, data.Repo, func(commit string) {
		loser.Do(func() {
			if err := os.WriteFile(data.Marker+".loser", []byte(commit), 0600); err != nil {
				t.Fatal(err)
			}
			e112AwaitFile(t, data.Marker+".cleanup")
		})
	}))
	org, err := uuid.Parse(data.Org)
	if err != nil {
		t.Fatal(err)
	}
	inner := &borrowedFSHeadFixture{database: database, handler: newBorrowedFSHeadHandler(t, database, data.Class), repoID: data.Repo, orgID: data.Org, orgUUID: org, userID: data.User, filename: data.Filename, headBefore: data.Head, content: data.Content, blockID: data.Block, sha1ID: data.SHA1, sessionID: data.Session}
	fx := &w2CreateFileFixture{w2UploadFileFixture: &w2UploadFileFixture{borrowedFSHeadFixture: inner}}
	rec := w2InvokeWriter(t, fx, data, database)
	if data.Phase != "cleanup" {
		t.Fatalf("crash writer returned: %d %s", rec.Code, rec.Body.String())
	}
	// Normal request-local cleanup is followed by the handler's ordinary retry.
	if rec.Code != http.StatusCreated {
		t.Fatalf("normal conflict retry: %d %s", rec.Code, rec.Body.String())
	}
}

func e112AwaitFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.NewTimer(40 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatalf("process boundary missing: %s", path)
		case <-ticker.C:
			if _, err := os.Stat(path); err == nil {
				return
			}
		}
	}
}

func e112Command(t *testing.T, data w2ChildFixture) *exec.Cmd {
	t.Helper()
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestE112ProcessChild$", "-test.v", "-test.timeout=90s")
	for _, entry := range os.Environ() {
		name := strings.SplitN(entry, "=", 2)[0]
		if strings.HasPrefix(name, "SESAMEFS_REQUIRE_") || name == "SESAMEFS_W2_PROCESS_CHILD" || name == "SESAMEFS_W2_CHILD_FIXTURE" || name == "SESAMEFS_E112_PROCESS" {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "SESAMEFS_W2_PROCESS_CHILD=1", "SESAMEFS_E112_PROCESS=1", "SESAMEFS_W2_CHILD_FIXTURE="+string(encoded))
	return cmd
}

func TestKnownLoserCrashSafety(t *testing.T) {
	if endpoint := os.Getenv("SESAMEFS_E112_ISOLATED_URL"); endpoint != "" && os.Getenv("SESAMEFS_E112_CHILD") != "1" {
		e112RunIsolated(t, endpoint)
		return
	}
	requireCassandra(t)
	if runtime.GOOS != "linux" || os.Getenv("SESAMEFS_TEST_IN_CONTAINER") != "1" {
		t.Fatal("E1-12 requires Linux Docker SIGKILL")
	}
	for _, endpoint := range []string{superadminClient.baseURL, envOrDefault("SESAMEFS_URL_2", "http://sesamefs-node-2:8080"), envOrDefault("SESAMEFS_URL_3", "http://sesamefs-node-3:8080")} {
		if err := e19CheckGCDisabled(newTestClient(endpoint, superadminClient.token)); err != nil {
			t.Fatalf("E1-12 isolation: %v", err)
		}
	}
	for _, leg := range e112Legs {
		t.Run(leg, func(t *testing.T) {
			// Record evidence only after the independent fixture teardown checks.
			t.Cleanup(func() {
				if !t.Failed() && !t.Skipped() {
					e112Observed[leg] = true
				}
			})
			var fx *w2CreateFileFixture
			verification := shareProjectionDBForTest(t)
			t.Cleanup(func() {
				if fx != nil {
					e111VerifyCleanup(t, verification, fx)
				}
			})
			var data w2ChildFixture
			fx, data = w2ClosureFixture(t, "Office")
			if leg == "normal" {
				if rec := fx.create(t); rec.Code != http.StatusCreated {
					t.Fatalf("control: %d %s", rec.Code, rec.Body.String())
				}
				fx.target = fx.readTarget(t)
				if len(w2Repairs(t, fx)) != 0 || !fx.hasOwnFSReferrer(t) {
					t.Fatal("normal publication did not settle")
				}
				refs, err := fx.database.ListBlockReferrers(fx.orgID, fx.blockID)
				if err != nil {
					t.Fatal(err)
				}
				for _, ref := range refs {
					if fs, ok := strings.CutPrefix(ref, "fs:"+fx.repoID+":"); ok {
						w24AssertHeadReaches(t, fx, borrowedFSReadHead(t, fx.database, fx.orgID, fx.repoID), fs)
					}
				}
				w2AssertBytes(t, fx)
				return
			}
			data.Marker = filepath.Join(t.TempDir(), "writer")
			data.Phase = "crash"
			if leg == "loser-cleanup" {
				data.Phase = "cleanup"
			}
			cmd := e112Command(t, data)
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			finished := false
			t.Cleanup(func() {
				if !finished {
					_ = cmd.Process.Kill()
					<-done
				}
			})
			e112AwaitFile(t, data.Marker+".authority")
			fx.target = fx.readTarget(t)
			rows := w2Repairs(t, fx)
			if len(rows) != 1 || len(rows[0].blocks) != 1 || rows[0].blocks[0] != fx.blockID {
				t.Fatalf("real attempt repair: %+v", rows)
			}
			r := rows[0]
			stageRefs, stageErr := fx.database.ListBlockReferrers(fx.orgID, fx.blockID)
			if stageErr != nil {
				t.Fatal(stageErr)
			}
			hasStagePub, hasUp := false, false
			for _, ref := range stageRefs {
				hasStagePub = hasStagePub || ref == dbpkg.BlockReferrerForPublishAttempt(r.commitID)
				hasUp = hasUp || strings.HasPrefix(ref, "up:")
			}
			if !hasStagePub || !hasUp {
				t.Fatalf("actual original up/pub missing: %v", stageRefs)
			}
			var ttl *int
			if err := fx.database.Session().Query(`SELECT TTL(created_at) FROM published_block_reference_repairs WHERE bucket=? AND org_id=? AND repo_id=? AND commit_id=? AND fs_id=?`, r.bucket, fx.orgID, fx.repoID, r.commitID, r.fsID).Consistency(gocql.EachQuorum).Scan(&ttl); err != nil || ttl != nil {
				t.Fatalf("repair TTL: %v %v", ttl, err)
			}
			fx.assertHeadUnchanged(t)
			// A real independent, blockless Office publication wins the captured HEAD.
			name := fx.filename
			fx.filename = "competitor.txt"
			rec := fx.create(t)
			fx.filename = name
			if rec.Code != http.StatusCreated {
				t.Fatalf("competitor: %d %s", rec.Code, rec.Body.String())
			}
			head := borrowedFSReadHead(t, fx.database, fx.orgID, fx.repoID)
			headRoot := e112CommitRoot(t, fx.database, fx.repoID, head)
			loserRoot := e112CommitRoot(t, fx.database, fx.repoID, r.commitID)
			if head == r.commitID || head == data.Head {
				t.Fatal("competitor did not win a different HEAD")
			}
			if err := os.WriteFile(data.Marker+".resume", nil, 0600); err != nil {
				t.Fatal(err)
			}
			e112AwaitFile(t, data.Marker+".loser")
			observed, err := os.ReadFile(data.Marker + ".loser")
			if err != nil || string(observed) != r.commitID {
				t.Fatalf("wrong definitive loser: %s %v", observed, err)
			}
			if len(w2Repairs(t, fx)) != 1 || fx.hasOwnFSReferrer(t) {
				t.Fatal("cleanup or fs promotion ran before loser pause")
			}
			if leg == "loser-cleanup" {
				if err := os.WriteFile(data.Marker+".cleanup", nil, 0600); err != nil {
					t.Fatal(err)
				}
				waitErr := <-done
				finished = true
				if waitErr != nil {
					t.Fatalf("normal cleanup child: %v\n%s", waitErr, output.String())
				}
				for _, row := range w2Repairs(t, fx) {
					if row.commitID == r.commitID {
						t.Fatal("loser repair survived normal cleanup")
					}
				}
				refs, err := fx.database.ListBlockReferrers(fx.orgID, fx.blockID)
				if err != nil {
					t.Fatal(err)
				}
				for _, ref := range refs {
					if ref == dbpkg.BlockReferrerForPublishAttempt(r.commitID) || ref == dbpkg.BlockReferrerForPublishAttempt(fx.repoID+":"+r.commitID+":"+r.fsID) {
						t.Fatal("loser pub survived cleanup")
					}
				}
				if len(w2Repairs(t, fx)) != 0 || !fx.hasOwnFSReferrer(t) {
					t.Fatal("normal retry failed to settle")
				}
				w2AssertBytes(t, fx)
				t.Log("real applied=false; normal cleanup completed; ordinary handler retry succeeded")
				return
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			waitErr := <-done
			finished = true
			var exit *exec.ExitError
			if !errors.As(waitErr, &exit) || !w2WasSIGKILL(exit) {
				t.Fatalf("writer was not SIGKILL: %v\n%s", waitErr, output.String())
			}
			t.Logf("verified OS SIGKILL after real applied=false before cleanup: commit=%s fs=%s P1=(%s,%s)", r.commitID, r.fsID, fx.target.StorageClass, fx.target.StorageKey)
			e112AssertRetained(t, fx, r, head, headRoot, loserRoot)
			if leg != "crash-pending" {
				refs, err := fx.database.ListBlockReferrers(fx.orgID, fx.blockID)
				if err != nil || len(refs) == 0 {
					t.Fatalf("real temporary refs missing: %v %v", refs, err)
				}
				for _, ref := range refs {
					if !strings.HasPrefix(ref, "up:") && !strings.HasPrefix(ref, "pub:") {
						t.Fatalf("saving ref: %s", ref)
					}
				}
				e12ExpireRealTemporaryTTL(t, fx, refs)
				if live, err := fx.database.BlockHasReferencesGlobal(fx.orgID, fx.blockID); err != nil || live {
					t.Fatalf("zero-ref boundary: live=%t err=%v", live, err)
				}
				e112AssertRetained(t, fx, r, head, headRoot, loserRoot)
			}
			if leg == "crash-gc" || leg == "crash-restart" {
				trace := e111NewTrace(fx)
				workerDB := w2EvidenceSession(t, splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042")[0], trace)
				e111GC(t, fx, workerDB, trace, false)
			}
			if leg == "crash-restart" {
				// Scheduling control only: age the actual row so the productive sweep can
				// visit immediately. It does not manufacture a repair or loser witness.
				if err := fx.database.Session().Query(`UPDATE published_block_reference_repairs SET created_at=?,lease_expires_at=? WHERE bucket=? AND org_id=? AND repo_id=? AND commit_id=? AND fs_id=?`, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour), r.bucket, fx.orgID, fx.repoID, r.commitID, r.fsID).Exec(); err != nil {
					t.Fatal(err)
				}
				data.Phase = "recovery"
				for visit := 0; visit < 2; visit++ {
					if err := fx.database.Session().Query(`UPDATE published_block_reference_repairs SET lease_expires_at=? WHERE bucket=? AND org_id=? AND repo_id=? AND commit_id=? AND fs_id=?`, time.Now().Add(-time.Hour), r.bucket, fx.orgID, fx.repoID, r.commitID, r.fsID).Exec(); err != nil {
						t.Fatal(err)
					}
					data.Marker = filepath.Join(t.TempDir(), "recovery")
					recovery := e112Command(t, data)
					recoveryOutput, err := recovery.CombinedOutput()
					t.Logf("new-process productive sweep %d:\n%s", visit+1, recoveryOutput)
					if err != nil {
						t.Fatalf("repair restart: %v", err)
					}
					e112AwaitFile(t, data.Marker+".recovered")
					e112AssertRetained(t, fx, r, head, headRoot, loserRoot)
					refs, err := fx.database.ListBlockReferrers(fx.orgID, fx.blockID)
					if err != nil || len(refs) == 0 {
						t.Fatalf("UNKNOWN did not renew real pub: %v %v", refs, err)
					}
					renewed := false
					for _, ref := range refs {
						renewed = renewed || ref == dbpkg.BlockReferrerForPublishAttempt(fx.repoID+":"+r.commitID+":"+r.fsID)
					}
					if !renewed {
						t.Fatalf("exact repair-owned pub not renewed: %v", refs)
					}
				}
			}
			e112AssertRetained(t, fx, r, head, headRoot, loserRoot)
		})
	}
}

func e112CommitRoot(t *testing.T, database *dbpkg.DB, repo, commit string) string {
	t.Helper()
	var root string
	if err := database.Session().Query(`SELECT root_fs_id FROM commits WHERE library_id=? AND commit_id=?`, repo, commit).Consistency(gocql.EachQuorum).Scan(&root); err != nil {
		t.Fatal(err)
	}
	return root
}
func e112AssertRetained(t *testing.T, fx *w2CreateFileFixture, r w2Repair, head, headRoot, loserRoot string) {
	t.Helper()
	rows := w2Repairs(t, fx)
	if len(rows) != 1 || rows[0].commitID != r.commitID || rows[0].fsID != r.fsID || len(rows[0].blocks) != 1 || rows[0].blocks[0] != fx.blockID {
		t.Fatalf("exact repair lost: %+v", rows)
	}
	if borrowedFSReadHead(t, fx.database, fx.orgID, fx.repoID) != head || e112CommitRoot(t, fx.database, fx.repoID, head) != headRoot || e112CommitRoot(t, fx.database, fx.repoID, r.commitID) != loserRoot {
		t.Fatal("HEAD/commit/root changed")
	}
	if fx.hasOwnFSReferrer(t) || fx.readTarget(t) != fx.target {
		t.Fatal("loser promoted fs or changed P1")
	}
	w24AssertHeadReaches(t, fx, r.commitID, r.fsID)
	e12AssertNoDeleteLifecycle(t, fx)
	w2AssertBytes(t, fx)
}

func e112RunIsolated(t *testing.T, endpoint string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	run := "^TestKnownLoserCrashSafety$"
	if _, sub, ok := strings.Cut(flag.Lookup("test.run").Value.String(), "/"); ok {
		run += "/" + sub
	}
	cmd := exec.CommandContext(ctx, binary, "-test.run="+run, "-test.v", "-test.count=1", "-test.timeout=3m")
	for _, entry := range os.Environ() {
		name := strings.SplitN(entry, "=", 2)[0]
		if strings.HasPrefix(name, "SESAMEFS_REQUIRE_") || strings.HasSuffix(name, "_CHILD") || name == "SESAMEFS_URL" || name == "SESAMEFS_URL_2" || name == "SESAMEFS_URL_3" || name == "CASSANDRA_KEYSPACE" {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, e112EvidenceEnv+"=1", "SESAMEFS_E112_CHILD=1", "CASSANDRA_KEYSPACE=sesamefs_e19", "SESAMEFS_URL="+endpoint, "SESAMEFS_URL_2="+endpoint, "SESAMEFS_URL_3="+endpoint)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("isolated E1-12: %v", err)
	}
	for _, leg := range e112Legs {
		e112Observed[leg] = true
	}
}
func TestKnownLoserCrashSafetyCompleteness(t *testing.T) {
	if len(e112Missing(nil)) != 6 {
		t.Fatal("six required legs")
	}
	all := map[string]bool{}
	for _, leg := range e112Legs {
		if all[leg] {
			t.Fatal("duplicate leg")
		}
		all[leg] = true
	}
	if len(e112Missing(all)) != 0 {
		t.Fatal("complete evidence rejected")
	}
	for _, leg := range e112Legs {
		delete(all, leg)
		if missing := e112Missing(all); len(missing) != 1 || missing[0] != leg {
			t.Fatalf("omission %s: %v", leg, missing)
		}
		all[leg] = true
	}
}
