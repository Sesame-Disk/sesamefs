//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
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
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

const e115bEvidenceEnv = "SESAMEFS_REQUIRE_E115B_WRITER_POST_D_EVIDENCE"

var e115bLegs = []string{"no-gc-control", "d-before-stage-no-crash", "d-before-stage-crash-before-queue", "d-before-stage-crash-after-queue"}
var e115bObserved = map[string]bool{}

func e115bMissing(seen map[string]bool) []string {
	var out []string
	for _, leg := range e115bLegs {
		if !seen[leg] {
			out = append(out, leg)
		}
	}
	return out
}

// The writer subprocess holds after materialization until the parent has driven
// a natural COMMITTED D(P1), then either completes or holds at a real seam for
// the parent's SIGKILL. Only the parent owns fixtures and evidence.
func TestE115BProcessChild(t *testing.T) {
	if os.Getenv("SESAMEFS_E115B_PROCESS") != "1" {
		t.Skip("parent-only process helper")
	}
	var data w2ChildFixture
	if err := json.Unmarshal([]byte(os.Getenv("SESAMEFS_W2_CHILD_FIXTURE")), &data); err != nil {
		t.Fatal(err)
	}
	database := shareProjectionDBForTest(t)
	mark := func(suffix, body string) {
		if err := os.WriteFile(data.Marker+suffix, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	hold := func(suffix string) {
		mark(suffix, data.Phase)
		for {
			time.Sleep(time.Second)
		}
	}
	var once sync.Once
	t.Cleanup(v2pkg.SetCreateFileAfterMaterializedBarrierForTest(data.Repo, func() {
		once.Do(func() {
			mark(".materialized", "up: registered, before pub: staging")
			e112AwaitFile(t, data.Marker+".resume")
		})
	}))
	switch data.Phase {
	case "crash-before-queue":
		t.Cleanup(v2pkg.SetFileFromBlocksPublicationBarriersForTest(data.Repo, nil, nil, func() { hold(".staged") }, nil))
	case "crash-after-queue":
		t.Cleanup(v2pkg.SetCreateFileBeforeFinalFenceForTest(data.Repo, func() { hold(".beforefence") }))
	case "no-crash":
	default:
		t.Fatalf("unknown phase %s", data.Phase)
	}
	org, err := uuid.Parse(data.Org)
	if err != nil {
		t.Fatal(err)
	}
	inner := &borrowedFSHeadFixture{database: database, repoID: data.Repo, orgID: data.Org, orgUUID: org, userID: data.User, filename: data.Filename, headBefore: data.Head, content: data.Content, blockID: data.Block, sha1ID: data.SHA1}
	fx := &w2CreateFileFixture{w2UploadFileFixture: &w2UploadFileFixture{borrowedFSHeadFixture: inner}}
	rec := w2InvokeWriter(t, fx, data, database)
	mark(".result", fmt.Sprintf("%d %s", rec.Code, rec.Body.String()))
}

func e115bCommand(t *testing.T, data w2ChildFixture) *exec.Cmd {
	t.Helper()
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestE115BProcessChild$", "-test.v", "-test.timeout=120s")
	for _, entry := range os.Environ() {
		name := strings.SplitN(entry, "=", 2)[0]
		if strings.HasPrefix(name, "SESAMEFS_REQUIRE_") || name == "SESAMEFS_W2_PROCESS_CHILD" || name == "SESAMEFS_W2_CHILD_FIXTURE" || name == "SESAMEFS_E115B_PROCESS" {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "SESAMEFS_W2_PROCESS_CHILD=1", "SESAMEFS_E115B_PROCESS=1", "SESAMEFS_W2_CHILD_FIXTURE="+string(encoded))
	return cmd
}

func e115bFixture(t *testing.T) *e114Fixture {
	t.Helper()
	var f *e114Fixture
	e114Teardown(t, func() *e114Fixture { return f })
	var fx *w2CreateFileFixture
	var expiry []e112ExpiryRow
	var owners e113Owners
	verification := shareProjectionDBForTest(t)
	t.Cleanup(func() {
		if fx != nil {
			e111VerifyCleanup(t, verification, fx)
			e112VerifyExpiryCleanup(t, verification, fx, expiry)
			e113VerifyOwners(t, verification, fx.repoID, owners)
			e115aVerifyNoRoot(t, verification, fx)
		}
	})
	fx = w2RepairFixture(t)
	t.Cleanup(func() { expiry = e112CleanupExpiry(t, fx) })
	t.Cleanup(func() { owners = e113CleanupOwners(t, fx) })
	f = &e114Fixture{fx: fx, head: fx.headBefore}
	return f
}

func (f *e114Fixture) refsExact(t *testing.T) []string {
	t.Helper()
	refs, err := f.fx.database.ListBlockReferrers(f.fx.orgID, f.fx.blockID)
	if err != nil {
		t.Fatal(err)
	}
	return refs
}

func (f *e114Fixture) refTTL(t *testing.T, ref string) int {
	t.Helper()
	var ttl int
	if err := f.fx.database.Session().Query(`SELECT TTL(created_at) FROM block_references WHERE org_id=? AND block_id=? AND referrer=?`, f.fx.orgID, f.fx.blockID, ref).Consistency(gocql.EachQuorum).Scan(&ttl); err != nil {
		t.Fatalf("ref %s TTL: %v", ref, err)
	}
	return ttl
}

// Post-D state common to every subprocess leg: canonical retired, exact
// COMMITTED lifecycle still recorded, HEAD unchanged, no fs: promotion.
func (f *e114Fixture) assertPostD(t *testing.T) {
	t.Helper()
	store := gcpkg.NewCassandraStore(f.fx.database)
	if exists, err := store.BlockExists(f.fx.orgUUID, f.fx.blockID); err != nil || exists {
		t.Fatalf("canonical P1 not retired: %t %v", exists, err)
	}
	var phase string
	if err := f.fx.database.Session().Query(`SELECT phase FROM gc_block_delete_lifecycles WHERE org_id=? AND block_id=?`, f.fx.orgID, f.fx.blockID).Consistency(gocql.EachQuorum).Scan(&phase); err != nil || phase != gcpkg.BlockDeleteLifecyclePhasePublished {
		t.Fatalf("exact COMMITTED lifecycle lost: %s %v", phase, err)
	}
	if borrowedFSReadHead(t, f.fx.database, f.fx.orgID, f.fx.repoID) != f.head {
		t.Fatal("HEAD changed")
	}
	if f.fx.hasOwnFSReferrer(t) {
		t.Fatal("fs: promoted after D")
	}
}

func TestWriterPostDStagingCrash(t *testing.T) {
	if endpoint := os.Getenv("SESAMEFS_E115B_ISOLATED_URL"); endpoint != "" && os.Getenv("SESAMEFS_E115B_CHILD") != "1" {
		e115bRunIsolated(t, endpoint)
		return
	}
	requireCassandra(t)
	if runtime.GOOS != "linux" || os.Getenv("SESAMEFS_TEST_IN_CONTAINER") != "1" {
		t.Fatal("E1-15B requires Linux Docker SIGKILL")
	}
	for _, endpoint := range []string{superadminClient.baseURL, envOrDefault("SESAMEFS_URL_2", "http://sesamefs-node-2:8080"), envOrDefault("SESAMEFS_URL_3", "http://sesamefs-node-3:8080")} {
		if err := e19CheckGCDisabled(newTestClient(endpoint, superadminClient.token)); err != nil {
			t.Fatalf("E1-15B isolation: %v", err)
		}
	}
	for _, leg := range e115bLegs {
		t.Run(leg, func(t *testing.T) {
			t.Cleanup(func() {
				if !t.Failed() && !t.Skipped() {
					e115bObserved[leg] = true
				}
			})
			f := e115bFixture(t)
			fx := f.fx
			if leg == "no-gc-control" {
				if rec := fx.create(t); rec.Code != http.StatusCreated {
					t.Fatalf("control: %d %s", rec.Code, rec.Body.String())
				}
				if !fx.hasOwnFSReferrer(t) || len(w2Repairs(t, fx)) != 0 {
					t.Fatal("normal publication did not promote fs: and settle its repair")
				}
				w2AssertBytes(t, fx)
				t.Log("E1-15B control: writer published; fs: present; repair settled; P1/K1 intact")
				return
			}
			phase := strings.TrimPrefix(leg, "d-before-stage-")
			data := w2ChildFixture{Org: fx.orgID, Repo: fx.repoID, User: fx.userID, Head: fx.headBefore, Filename: fx.filename, Phase: phase, Marker: filepath.Join(t.TempDir(), "writer"), Class: x1StorageClass(t), Funnel: "Office", Content: fx.content, Block: fx.blockID, SHA1: fx.sha1ID}
			cmd := e115bCommand(t, data)
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
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
			e112AwaitFile(t, data.Marker+".materialized")
			fx.target = fx.readTarget(t)
			refs := f.refsExact(t)
			if len(refs) != 1 || !strings.HasPrefix(refs[0], "up:") || len(w2Repairs(t, fx)) != 0 {
				t.Fatalf("before staging only the writer's real up: may exist: refs=%v repairs=%+v", refs, w2Repairs(t, fx))
			}
			fx.assertHeadUnchanged(t)
			e115aCommitD(t, f)
			// Deferred (not t.Cleanup, whose context is already canceled) so a failing
			// leg still completes its owned COMMITTED root before fixture teardown.
			defer w2AssertCommittedContinuation(t, gcpkg.NewCassandraStore(fx.database), fx.orgUUID, fx.blockID, fx.target.StorageClass, fx.target.StorageKey, newVerificationBlockStore(t, fx.orgID))
			if err := os.WriteFile(data.Marker+".resume", nil, 0600); err != nil {
				t.Fatal(err)
			}
			kill := func(marker string) {
				e112AwaitFile(t, data.Marker+marker)
				if err := cmd.Process.Kill(); err != nil {
					t.Fatal(err)
				}
				waitErr := <-done
				finished = true
				var exit *exec.ExitError
				if !errors.As(waitErr, &exit) || !w2WasSIGKILL(exit) {
					t.Fatalf("writer was not SIGKILL: %v\n%s", waitErr, output.String())
				}
			}
			switch phase {
			case "no-crash":
				waitErr := <-done
				finished = true
				if waitErr != nil {
					t.Fatalf("writer child: %v\n%s", waitErr, output.String())
				}
				result, err := os.ReadFile(data.Marker + ".result")
				if err != nil || strings.HasPrefix(string(result), "201") {
					t.Fatalf("final fence must reject the post-D publication: %q %v", result, err)
				}
				f.assertPostD(t)
				if refs := f.refsExact(t); len(refs) != 0 || len(w2Repairs(t, fx)) != 0 {
					t.Fatalf("rejected attempt left refs=%v repairs=%+v", refs, w2Repairs(t, fx))
				}
				t.Logf("E1-15B no-crash: writer resumed after COMMITTED D, staged, then the final fence rejected it (%s); pub:/repair cleaned; HEAD unchanged; the staging INSERT itself still ran post-D", strings.SplitN(string(result), " ", 2)[0])
			case "crash-before-queue":
				kill(".staged")
				f.assertPostD(t)
				refs := f.refsExact(t)
				if len(refs) != 1 || !strings.HasPrefix(refs[0], "pub:") || len(w2Repairs(t, fx)) != 0 {
					t.Fatalf("characterization changed: refs=%v repairs=%+v", refs, w2Repairs(t, fx))
				}
				ttl := f.refTTL(t, refs[0])
				if ttl <= 0 || ttl > dbpkg.PublishAttemptReferenceTTLSeconds {
					t.Fatalf("pub: TTL %d", ttl)
				}
				if live, err := fx.database.BlockHasReferencesGlobal(fx.orgID, fx.blockID); err != nil || !live {
					t.Fatalf("post-D pub: not globally visible: %t %v", live, err)
				}
				t.Logf("E1-15B RED characterized (%s): SIGKILL after staging; durable %s (ttl=%ds) written after exact COMMITTED D(P1); no repair; HEAD unchanged; harm: TTL-bounded dead pin", leg, refs[0], ttl)
			case "crash-after-queue":
				kill(".beforefence")
				f.assertPostD(t)
				rows := w2Repairs(t, fx)
				if len(rows) != 1 || len(rows[0].blocks) != 1 || rows[0].blocks[0] != fx.blockID {
					t.Fatalf("characterization changed: repairs=%+v", rows)
				}
				r := rows[0]
				staged := dbpkg.BlockReferrerForPublishAttempt(r.commitID)
				if refs := f.refsExact(t); len(refs) != 1 || refs[0] != staged {
					t.Fatalf("characterization changed: refs=%v want [%s]", refs, staged)
				}
				var repairTTL *int
				if err := fx.database.Session().Query(`SELECT TTL(created_at) FROM published_block_reference_repairs WHERE bucket=? AND org_id=? AND repo_id=? AND commit_id=? AND fs_id=?`, r.bucket, fx.orgID, fx.repoID, r.commitID, r.fsID).Consistency(gocql.EachQuorum).Scan(&repairTTL); err != nil || repairTTL != nil {
					t.Fatalf("post-D repair not durable: %v %v", repairTTL, err)
				}
				e112CommitRoot(t, fx.database, fx.repoID, r.commitID)
				t.Logf("E1-15B RED characterized (%s): SIGKILL after queue+insertCommit, before the final fence; durable %s and non-expiring repair %s/%s written after exact COMMITTED D(P1); HEAD unchanged", leg, staged, r.commitID, r.fsID)
				// E1-13 eligibility control: the real lease stays ahead of the shared daemon.
				if err := fx.database.Session().Query(`UPDATE published_block_reference_repairs SET created_at=?,lease_expires_at=? WHERE bucket=? AND org_id=? AND repo_id=? AND commit_id=? AND fs_id=?`, time.Now().Add(-time.Hour), time.Now().Add(time.Hour), r.bucket, fx.orgID, fx.repoID, r.commitID, r.fsID).Consistency(gocql.LocalQuorum).Exec(); err != nil {
					t.Fatal(err)
				}
				owned := e115aRepairOwned(fx, r)
				writes := &e115aWrites{org: fx.orgID, block: fx.blockID}
				sweepDB := w2EvidenceSession(t, splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042")[0], writes)
				var mu sync.Mutex
				var outcomes []string
				t.Cleanup(v2pkg.SetRepairAfterClassifyForIntegration(sweepDB, fx.repoID, func(outcome string, err error) {
					mu.Lock()
					defer mu.Unlock()
					outcomes = append(outcomes, fmt.Sprintf("%s/%v", outcome, err))
				}))
				for i, at := range []time.Duration{2 * time.Hour, 6 * time.Hour} {
					err := v2pkg.RunPublishedBlockReferenceRepairSweepAtForIntegration(sweepDB, time.Now().Add(at))
					inserts, deletes := writes.snapshot()
					mu.Lock()
					seen := append([]string(nil), outcomes...)
					mu.Unlock()
					if len(seen) != i+1 || seen[i] != "unknown/<nil>" || err == nil || !strings.Contains(err.Error(), "unknown; retain queued repair") {
						t.Fatalf("sweep %d characterization changed: outcomes=%v err=%v", i+1, seen, err)
					}
					if len(inserts) != i+1 || inserts[i] != owned || len(deletes) != 0 {
						t.Fatalf("sweep %d post-D renewal characterization changed: inserts=%v deletes=%v", i+1, inserts, deletes)
					}
					ttl := f.refTTL(t, owned)
					if ttl <= 0 || ttl > dbpkg.PublishAttemptReferenceTTLSeconds || len(w2Repairs(t, fx)) != 1 {
						t.Fatalf("sweep %d: renewed ttl=%d repairs=%d", i+1, ttl, len(w2Repairs(t, fx)))
					}
					t.Logf("E1-15B RED characterized (%s): sweep %d (eligibility +%s) classified native UNKNOWN and renewed %s (ttl=%ds) after exact COMMITTED D(P1); repair retained; #271 post-check kept it (row present)", leg, i+1, at, owned, ttl)
				}
				f.assertPostD(t)
			}
		})
	}
}

func e115bRunIsolated(t *testing.T, endpoint string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	run := "^TestWriterPostDStagingCrash$"
	if _, sub, ok := strings.Cut(flag.Lookup("test.run").Value.String(), "/"); ok {
		run += "/" + sub
	}
	cmd := exec.CommandContext(ctx, binary, "-test.run="+run, "-test.v", "-test.count=1", "-test.timeout=5m")
	for _, entry := range os.Environ() {
		name := strings.SplitN(entry, "=", 2)[0]
		if strings.HasPrefix(name, "SESAMEFS_REQUIRE_") || strings.HasSuffix(name, "_CHILD") || name == "SESAMEFS_URL" || name == "SESAMEFS_URL_2" || name == "SESAMEFS_URL_3" || name == "CASSANDRA_KEYSPACE" {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, e115bEvidenceEnv+"=1", "SESAMEFS_E115B_CHILD=1", "CASSANDRA_KEYSPACE=sesamefs_e19", "SESAMEFS_URL="+endpoint, "SESAMEFS_URL_2="+endpoint, "SESAMEFS_URL_3="+endpoint)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("isolated E1-15B: %v", err)
	}
	for _, leg := range e115bLegs {
		e115bObserved[leg] = true
	}
}

func TestWriterPostDStagingCrashCompleteness(t *testing.T) {
	if len(e115bMissing(nil)) != 4 {
		t.Fatal("four required legs")
	}
	all := map[string]bool{}
	for _, leg := range e115bLegs {
		if all[leg] {
			t.Fatal("duplicate leg")
		}
		all[leg] = true
	}
	if len(e115bMissing(all)) != 0 {
		t.Fatal("complete rejected")
	}
	for _, leg := range e115bLegs {
		delete(all, leg)
		if missing := e115bMissing(all); len(missing) != 1 || missing[0] != leg {
			t.Fatalf("omission %s: %v", leg, missing)
		}
		all[leg] = true
	}
}
