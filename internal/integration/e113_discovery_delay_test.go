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
	"sort"
	"strings"
	"testing"
	"time"

	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

const e113EvidenceEnv = "SESAMEFS_REQUIRE_E113_DISCOVERY_DELAY_EVIDENCE"

var e113Legs = []string{"control-no-backlog", "backlog-before-target", "target-not-yet-visited-gc", "fresh-process-backlog", "eventual-target-visit"}
var e113Observed = map[string]bool{}

func e113Missing(seen map[string]bool) []string {
	var out []string
	for _, leg := range e113Legs {
		if !seen[leg] {
			out = append(out, leg)
		}
	}
	return out
}

type e113Coordinate struct {
	Repo, Commit, FS string
	Bucket           int
}
type e113Process struct {
	Rows           []e113Coordinate
	Target, Marker string
	Pause          bool
}

// The live parent owns fixture cleanup. Subprocesses observe actual visit entry
// and native classification through exact session/library hooks only.
func TestE113ProcessChild(t *testing.T) {
	if os.Getenv("SESAMEFS_E113_PROCESS") != "1" {
		t.Skip("parent-only sweep helper")
	}
	var data e113Process
	if err := json.Unmarshal([]byte(os.Getenv("SESAMEFS_E113_PROCESS_DATA")), &data); err != nil {
		t.Fatal(err)
	}
	database := shareProjectionDBForTest(t)
	visits := []string{}
	outcomes := map[string]string{}
	for _, coordinate := range data.Rows {
		row := coordinate
		t.Cleanup(v2pkg.SetRepairBeforeVisitForIntegration(database, row.Repo, func(commit, fs string) {
			if commit != row.Commit || fs != row.FS {
				t.Fatalf("unexpected repair identity %s/%s", commit, fs)
			}
			visits = append(visits, row.Repo)
			encoded, err := json.Marshal(visits)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(data.Marker+".visits", encoded, 0600); err != nil {
				t.Fatal(err)
			}
			if data.Pause && len(visits) == 1 {
				if row.Repo == data.Target {
					t.Fatal("target preceded real backlog")
				}
				if err := os.WriteFile(data.Marker+".paused", []byte(row.Repo), 0600); err != nil {
					t.Fatal(err)
				}
				e112AwaitFile(t, data.Marker+".resume")
			}
		}))
		t.Cleanup(v2pkg.SetRepairAfterClassifyForIntegration(database, row.Repo, func(outcome string, err error) {
			if err != nil {
				t.Errorf("native classifier error %s: %v", row.Repo, err)
			}
			if _, duplicate := outcomes[row.Repo]; duplicate {
				t.Errorf("duplicate classification %s", row.Repo)
			}
			outcomes[row.Repo] = outcome
		}))
	}
	err := v2pkg.RunPublishedBlockReferenceRepairSweepAtForIntegration(database, time.Now().Add(2*time.Hour))
	if err == nil || !strings.Contains(err.Error(), "unknown; retain queued repair") {
		t.Fatalf("native UNKNOWN sweep result: %v", err)
	}
	if len(visits) != len(data.Rows) || len(outcomes) != len(data.Rows) {
		t.Fatalf("missing visit/classifier: visits=%v outcomes=%v", visits, outcomes)
	}
	targetSeen := false
	for _, repo := range visits {
		if repo == data.Target {
			targetSeen = true
		} else if targetSeen {
			t.Fatal("backlog visited after target")
		}
		if outcomes[repo] != "unknown" {
			t.Fatalf("native outcome %s=%s", repo, outcomes[repo])
		}
	}
	if !targetSeen {
		t.Fatal("target not visited")
	}
	if err := os.WriteFile(data.Marker+".complete", []byte("native UNKNOWN retained"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("productive order=%v native outcomes=%v", visits, outcomes)
}

func e113Command(t *testing.T, data e113Process) *exec.Cmd {
	t.Helper()
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestE113ProcessChild$", "-test.v", "-test.timeout=90s")
	for _, entry := range os.Environ() {
		name := strings.SplitN(entry, "=", 2)[0]
		if strings.HasPrefix(name, "SESAMEFS_REQUIRE_") || name == "SESAMEFS_W2_PROCESS_CHILD" || name == "SESAMEFS_E113_PROCESS" || name == "SESAMEFS_E113_PROCESS_DATA" {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "SESAMEFS_W2_PROCESS_CHILD=1", "SESAMEFS_E113_PROCESS=1", "SESAMEFS_E113_PROCESS_DATA="+string(encoded))
	return cmd
}

type e113Fixture struct {
	fx                        *w2CreateFileFixture
	repair                    w2Repair
	head, headRoot, loserRoot string
}

func e113RealFixture(t *testing.T) *e113Fixture {
	t.Helper()
	var fx *w2CreateFileFixture
	var expiry []e112ExpiryRow
	var owners e113Owners
	verification := shareProjectionDBForTest(t)
	t.Cleanup(func() {
		if fx != nil {
			e111VerifyCleanup(t, verification, fx)
			e112VerifyExpiryCleanup(t, verification, fx, expiry)
			e113VerifyOwners(t, verification, fx.repoID, owners)
		}
	})
	fx = w2RepairFixture(t)
	t.Cleanup(func() { expiry = e112CleanupExpiry(t, fx) })
	t.Cleanup(func() { owners = e113CleanupOwners(t, fx) })
	func() {
		restore := v2pkg.SetW2PublicationAfterAuthorityForTest(fx.repoID, func() { panic("w2-process-death") })
		defer restore()
		w2Crash(t, fx)
	}()
	rows := w2Repairs(t, fx)
	if len(rows) != 1 || len(rows[0].blocks) != 1 || rows[0].blocks[0] != fx.blockID {
		t.Fatalf("real Office repair missing: %+v", rows)
	}
	r := rows[0]
	refs, err := fx.database.ListBlockReferrers(fx.orgID, fx.blockID)
	if err != nil {
		t.Fatal(err)
	}
	up, pub := false, false
	for _, ref := range refs {
		up = up || strings.HasPrefix(ref, "up:")
		pub = pub || ref == dbpkg.BlockReferrerForPublishAttempt(r.commitID)
	}
	if !up || !pub {
		t.Fatalf("productive up/pub absent: %v", refs)
	}
	fx.assertHeadUnchanged(t)
	name := fx.filename
	fx.filename = "competitor.txt"
	rec := fx.create(t)
	fx.filename = name
	if rec.Code != http.StatusCreated {
		t.Fatalf("productive competitor: %d %s", rec.Code, rec.Body.String())
	}
	f := &e113Fixture{fx: fx, repair: r, head: borrowedFSReadHead(t, fx.database, fx.orgID, fx.repoID)}
	if f.head == r.commitID {
		t.Fatal("target accidentally reachable")
	}
	f.headRoot = e112CommitRoot(t, fx.database, fx.repoID, f.head)
	f.loserRoot = e112CommitRoot(t, fx.database, fx.repoID, r.commitID)
	return f
}
func (f *e113Fixture) retained(t *testing.T) {
	t.Helper()
	e112AssertRetained(t, f.fx, f.repair, f.head, f.headRoot, f.loserRoot)
	var ttl *int
	if err := f.fx.database.Session().Query(`SELECT TTL(created_at) FROM published_block_reference_repairs WHERE bucket=? AND org_id=? AND repo_id=? AND commit_id=? AND fs_id=?`, f.repair.bucket, f.fx.orgID, f.fx.repoID, f.repair.commitID, f.repair.fsID).Consistency(gocql.EachQuorum).Scan(&ttl); err != nil || ttl != nil {
		t.Fatalf("repair not durable TTL=%v err=%v", ttl, err)
	}
}
func (f *e113Fixture) eligible(t *testing.T) {
	t.Helper()
	// Keep the real lease ahead of the shared daemon. The dedicated sweep advances
	// only eligibility time; this is neither row order nor publication authority.
	if err := f.fx.database.Session().Query(`UPDATE published_block_reference_repairs SET created_at=?,lease_expires_at=? WHERE bucket=? AND org_id=? AND repo_id=? AND commit_id=? AND fs_id=?`, time.Now().Add(-time.Hour), time.Now().Add(time.Hour), f.repair.bucket, f.fx.orgID, f.fx.repoID, f.repair.commitID, f.repair.fsID).Consistency(gocql.LocalQuorum).Exec(); err != nil {
		t.Fatal(err)
	}
}

// A durable cursor/anchor or a renewed pub would reveal an out-of-band visitor.
// Read the actual row, independently of the subprocess's own visit accounting.
func (f *e113Fixture) unvisited(t *testing.T) {
	t.Helper()
	var anchor, cursor *string
	var exhausted *bool
	if err := f.fx.database.Session().Query(`SELECT reachability_anchor_head_commit_id,reachability_cursor_commit_id,reachability_anchor_exhausted FROM published_block_reference_repairs WHERE bucket=? AND org_id=? AND repo_id=? AND commit_id=? AND fs_id=?`, f.repair.bucket, f.fx.orgID, f.fx.repoID, f.repair.commitID, f.repair.fsID).Consistency(gocql.EachQuorum).Scan(&anchor, &cursor, &exhausted); err != nil {
		t.Fatal(err)
	}
	if anchor != nil || cursor != nil || exhausted != nil {
		t.Fatalf("target already has visitor progress: anchor=%v cursor=%v exhausted=%v", anchor, cursor, exhausted)
	}
	if live, err := f.fx.database.BlockHasReferencesGlobal(f.fx.orgID, f.fx.blockID); err != nil || live {
		t.Fatalf("unvisited target must have zero refs: %t %v", live, err)
	}
}

func e113Visits(t *testing.T, marker string) []string {
	t.Helper()
	raw, err := os.ReadFile(marker + ".visits")
	if err != nil {
		t.Fatal(err)
	}
	var visits []string
	if err := json.Unmarshal(raw, &visits); err != nil {
		t.Fatal(err)
	}
	return visits
}

func TestRepairDiscoveryDelaySafety(t *testing.T) {
	if endpoint := os.Getenv("SESAMEFS_E113_ISOLATED_URL"); endpoint != "" && os.Getenv("SESAMEFS_E113_CHILD") != "1" {
		e113RunIsolated(t, endpoint)
		return
	}
	requireCassandra(t)
	if runtime.GOOS != "linux" || os.Getenv("SESAMEFS_TEST_IN_CONTAINER") != "1" {
		t.Fatal("E1-13 requires Linux Docker")
	}
	for _, endpoint := range []string{superadminClient.baseURL, envOrDefault("SESAMEFS_URL_2", "http://sesamefs-node-2:8080"), envOrDefault("SESAMEFS_URL_3", "http://sesamefs-node-3:8080")} {
		if err := e19CheckGCDisabled(newTestClient(endpoint, superadminClient.token)); err != nil {
			t.Fatalf("E1-13 isolation: %v", err)
		}
	}
	for _, leg := range e113Legs {
		t.Run(leg, func(t *testing.T) {
			t.Cleanup(func() {
				if !t.Failed() && !t.Skipped() {
					e113Observed[leg] = true
				}
			})
			fixtures := []*e113Fixture{e113RealFixture(t)}
			if leg != "control-no-backlog" {
				// Hash distribution is observed, not manufactured. Stop only when at least
				// three real repairs have naturally earlier buckets than a target.
				for len(fixtures) < 16 {
					fixtures = append(fixtures, e113RealFixture(t))
					sort.Slice(fixtures, func(i, j int) bool { return fixtures[i].repair.bucket < fixtures[j].repair.bucket })
					if len(fixtures) >= 4 && fixtures[2].repair.bucket < fixtures[len(fixtures)-1].repair.bucket {
						break
					}
				}
				if len(fixtures) < 4 || fixtures[2].repair.bucket >= fixtures[len(fixtures)-1].repair.bucket {
					t.Fatal("unable to obtain real earlier-bucket backlog")
				}
			}
			target := fixtures[len(fixtures)-1]
			rows := []e113Coordinate{}
			for _, f := range fixtures {
				// Equal-bucket extras stay ahead even of the controlled eligibility time.
				if f != target && f.repair.bucket >= target.repair.bucket {
					if err := f.fx.database.Session().Query(`UPDATE published_block_reference_repairs SET lease_expires_at=? WHERE bucket=? AND org_id=? AND repo_id=? AND commit_id=? AND fs_id=?`, time.Now().Add(3*time.Hour), f.repair.bucket, f.fx.orgID, f.fx.repoID, f.repair.commitID, f.repair.fsID).Consistency(gocql.LocalQuorum).Exec(); err != nil {
						t.Fatal(err)
					}
					continue
				}
				f.eligible(t)
				rows = append(rows, e113Coordinate{Repo: f.fx.repoID, Commit: f.repair.commitID, FS: f.repair.fsID, Bucket: f.repair.bucket})
				f.retained(t)
			}
			if leg != "control-no-backlog" && len(rows) < 4 {
				t.Fatalf("need at least three actual earlier-bucket repairs: %+v", rows)
			}
			refs, err := target.fx.database.ListBlockReferrers(target.fx.orgID, target.fx.blockID)
			if err != nil || len(refs) == 0 {
				t.Fatalf("target temporary refs missing: %v %v", refs, err)
			}
			for _, ref := range refs {
				if !strings.HasPrefix(ref, "up:") && !strings.HasPrefix(ref, "pub:") {
					t.Fatalf("unexpected saving ref %s", ref)
				}
			}
			e12ExpireRealTemporaryTTL(t, target.fx, refs)
			if live, err := target.fx.database.BlockHasReferencesGlobal(target.fx.orgID, target.fx.blockID); err != nil || live {
				t.Fatalf("target not zero refs: %t %v", live, err)
			}
			target.retained(t)
			target.unvisited(t)
			data := e113Process{Rows: rows, Target: target.fx.repoID, Marker: filepath.Join(t.TempDir(), "sweep"), Pause: leg != "control-no-backlog"}
			cmd := e113Command(t, data)
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
			if data.Pause {
				e112AwaitFile(t, data.Marker+".paused")
				visits := e113Visits(t, data.Marker)
				if len(visits) != 1 || visits[0] == data.Target {
					t.Fatalf("target already visited: %v", visits)
				}
				target.retained(t)
				target.unvisited(t)
				if live, err := target.fx.database.BlockHasReferencesGlobal(target.fx.orgID, target.fx.blockID); err != nil || live {
					t.Fatalf("unvisited target not zero refs %t %v", live, err)
				}
				t.Logf("E1-13 target NOT visited: backlog first=%s target=%s bucket=%d eligible rows=%+v", visits[0], data.Target, target.repair.bucket, rows)
				if leg == "target-not-yet-visited-gc" {
					trace := e111NewTrace(target.fx)
					workerDB := w2EvidenceSession(t, splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042")[0], trace)
					e111GC(t, target.fx, workerDB, trace, false)
					if visits := e113Visits(t, data.Marker); len(visits) != 1 {
						t.Fatalf("sweep advanced while paused: %v", visits)
					}
					target.retained(t)
					target.unvisited(t)
				}
				if leg == "fresh-process-backlog" {
					firstBacklog := visits[0]
					if err := cmd.Process.Kill(); err != nil {
						t.Fatal(err)
					}
					waitErr := <-done
					finished = true
					var exit *exec.ExitError
					if !errors.As(waitErr, &exit) || !w2WasSIGKILL(exit) {
						t.Fatalf("sweep not SIGKILL: %v", waitErr)
					}
					t.Log("verified sweep OS SIGKILL before backlog execution and target visit")
					data.Marker = filepath.Join(t.TempDir(), "restart")
					cmd = e113Command(t, data)
					output.Reset()
					cmd.Stdout, cmd.Stderr = &output, &output
					if err := cmd.Start(); err != nil {
						t.Fatal(err)
					}
					done = make(chan error, 1)
					finished = false
					go func() { done <- cmd.Wait() }()
					e112AwaitFile(t, data.Marker+".paused")
					visits := e113Visits(t, data.Marker)
					if len(visits) != 1 || visits[0] != firstBacklog {
						t.Fatalf("restart missed real first backlog: %v", visits)
					}
					target.retained(t)
					target.unvisited(t)
				}
				if err := os.WriteFile(data.Marker+".resume", nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			waitErr := <-done
			finished = true
			t.Logf("productive sweep:\n%s", output.String())
			if waitErr != nil {
				t.Fatalf("productive sweep child: %v", waitErr)
			}
			e112AwaitFile(t, data.Marker+".complete")
			visits := e113Visits(t, data.Marker)
			if len(visits) != len(rows) || visits[len(visits)-1] != data.Target {
				t.Fatalf("real backlog order/target visit: %v", visits)
			}
			for _, f := range fixtures {
				f.retained(t)
				selected := false
				for _, row := range rows {
					selected = selected || row.Repo == f.fx.repoID
				}
				if selected {
					refs, err := f.fx.database.ListBlockReferrers(f.fx.orgID, f.fx.blockID)
					expected := dbpkg.BlockReferrerForPublishAttempt(f.fx.repoID + ":" + f.repair.commitID + ":" + f.repair.fsID)
					renewed := false
					for _, ref := range refs {
						renewed = renewed || ref == expected
					}
					if err != nil || !renewed {
						t.Fatalf("native UNKNOWN exact pub renewal: %v %v", refs, err)
					}
				}
			}
			t.Logf("E1-13 measured target visited after %d earlier repairs; no discovery SLA claimed", len(rows)-1)
		})
	}
}

func e113RunIsolated(t *testing.T, endpoint string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	run := "^TestRepairDiscoveryDelaySafety$"
	if _, sub, ok := strings.Cut(flag.Lookup("test.run").Value.String(), "/"); ok {
		run += "/" + sub
	}
	cmd := exec.CommandContext(ctx, binary, "-test.run="+run, "-test.v", "-test.count=1", "-test.timeout=4m")
	for _, entry := range os.Environ() {
		name := strings.SplitN(entry, "=", 2)[0]
		if strings.HasPrefix(name, "SESAMEFS_REQUIRE_") || strings.HasSuffix(name, "_CHILD") || name == "SESAMEFS_URL" || name == "SESAMEFS_URL_2" || name == "SESAMEFS_URL_3" || name == "CASSANDRA_KEYSPACE" {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, e113EvidenceEnv+"=1", "SESAMEFS_E113_CHILD=1", "CASSANDRA_KEYSPACE=sesamefs_e19", "SESAMEFS_URL="+endpoint, "SESAMEFS_URL_2="+endpoint, "SESAMEFS_URL_3="+endpoint)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("isolated E1-13: %v", err)
	}
	for _, leg := range e113Legs {
		e113Observed[leg] = true
	}
}
func TestRepairDiscoveryDelaySafetyCompleteness(t *testing.T) {
	if len(e113Missing(nil)) != 5 {
		t.Fatal("five required legs")
	}
	all := map[string]bool{}
	for _, leg := range e113Legs {
		if all[leg] {
			t.Fatal("duplicate leg")
		}
		all[leg] = true
	}
	if len(e113Missing(all)) != 0 {
		t.Fatal("complete rejected")
	}
	for _, leg := range e113Legs {
		delete(all, leg)
		if missing := e113Missing(all); len(missing) != 1 || missing[0] != leg {
			t.Fatalf("omission %s: %v", leg, missing)
		}
		all[leg] = true
	}
}

// Pending fs owners are separate from repair/fs rows. Capture only this owned
// library's actual fs coordinates before fixture metadata disappears, then use
// the productive exact-owner helper; independently verify canonical/projection
// absence after all teardown, including the expected guard-omission RED.
type e113Owners struct {
	fsIDs []string
	rows  []dbpkg.PendingPublishedFSObjectOwner
}

func e113CleanupOwners(t *testing.T, fx *w2CreateFileFixture) e113Owners {
	t.Helper()
	var records e113Owners
	iter := fx.database.Session().Query(`SELECT fs_id FROM fs_objects WHERE library_id=?`, fx.repoID).Consistency(gocql.EachQuorum).Iter()
	var fs string
	for iter.Scan(&fs) {
		records.fsIDs = append(records.fsIDs, fs)
	}
	if err := iter.Close(); err != nil {
		t.Errorf("E1-13 owner teardown fs read: %v", err)
	}
	for _, fs := range records.fsIDs {
		iter := fx.database.Session().Query(`SELECT owner_id,created_at,org_id,attempt_id FROM pending_published_fs_objects WHERE repo_id=? AND fs_id=?`, fx.repoID, fs).Consistency(gocql.EachQuorum).Iter()
		var row dbpkg.PendingPublishedFSObjectOwner
		for iter.Scan(&row.OwnerID, &row.CreatedAt, &row.OrgID, &row.AttemptID) {
			row.RepoID, row.FSID = fx.repoID, fs
			records.rows = append(records.rows, row)
			row = dbpkg.PendingPublishedFSObjectOwner{}
		}
		if err := iter.Close(); err != nil {
			t.Errorf("E1-13 owner teardown read: %v", err)
		}
	}
	for _, row := range records.rows {
		if row.OrgID != fx.orgID || row.CreatedAt.IsZero() {
			t.Errorf("E1-13 owner teardown identity invalid: %+v", row)
			continue
		}
		if err := fx.database.DeletePendingPublishedFSObjectOwner(row.RepoID, row.FSID, row.OwnerID, row.CreatedAt); err != nil {
			t.Errorf("E1-13 owner teardown delete: %v", err)
		}
	}
	return records
}
func e113VerifyOwners(t *testing.T, database *dbpkg.DB, repo string, records e113Owners) {
	t.Helper()
	clean := true
	var count int
	for _, fs := range records.fsIDs {
		if err := database.Session().Query(`SELECT count(*) FROM pending_published_fs_objects WHERE repo_id=? AND fs_id=?`, repo, fs).Consistency(gocql.EachQuorum).Scan(&count); err != nil || count != 0 {
			clean = false
			t.Errorf("E1-13 owner teardown canonical rows=%d err=%v", count, err)
		}
	}
	for _, row := range records.rows {
		if err := database.Session().Query(`SELECT count(*) FROM pending_published_fs_objects_by_day WHERE created_day=? AND bucket=? AND created_at=? AND repo_id=? AND fs_id=? AND owner_id=?`, dbpkg.GCProjectionUTCDate(row.CreatedAt), dbpkg.GCDiscoveryBucket(row.RepoID, row.FSID, row.OwnerID), row.CreatedAt, row.RepoID, row.FSID, row.OwnerID).Consistency(gocql.EachQuorum).Scan(&count); err != nil || count != 0 {
			clean = false
			t.Errorf("E1-13 owner teardown projection rows=%d err=%v", count, err)
		}
	}
	if clean {
		t.Logf("E1-13 owner teardown verified repo=%s: %d fs partitions and %d exact owner/projection coordinates absent", repo, len(records.fsIDs), len(records.rows))
	}
}
