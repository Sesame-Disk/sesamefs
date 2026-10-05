//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

const w2ClosureEvidenceEnv = "SESAMEFS_REQUIRE_W2_CLOSURE_EVIDENCE"

var w2ClosureObserved = map[string]bool{}
var w2ClosureRequired = func() []string {
	var required []string
	for _, funnel := range w2ClosureFunnels {
		for _, leg := range []string{"appliedReadable", "appliedUnconfirmed", "requestLostUnconfirmed"} {
			required = append(required, "TestW2NativeHEADAmbiguity/"+funnel+"/"+leg)
		}
		for _, phase := range []string{"beforeRepair", "afterAuthority", "inFlightAppliedHEAD"} {
			required = append(required, "TestW2ProcessKillContinuity/"+funnel+"/"+phase)
		}
	}
	return append(required, "TestW2ProcessKillContinuity/Office/afterHEAD")
}()

func w2ClosureMissing() []string {
	var out []string
	for _, name := range w2ClosureRequired {
		if !w2ClosureObserved[name] {
			out = append(out, name)
		}
	}
	return out
}
func w2ObserveClosure(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		if !t.Failed() && !t.Skipped() {
			w2ClosureObserved[t.Name()] = true
		}
	})
}

func w2AssertBytes(t *testing.T, fx *w2CreateFileFixture) {
	t.Helper()
	data, err := newVerificationBlockStore(t, fx.orgID).GetBlockByStorageKey(context.Background(), fx.target.StorageKey)
	if err != nil || !bytes.Equal(data, fx.content) {
		t.Fatalf("exact P bytes lost: %v", err)
	}
}

// Discovery is restricted to this actual queued candidate. Claims, liveness,
// handoff and queue lifecycle still execute against the real Cassandra store.
// Standard integration fixtures may share an org with unrelated GC work.
type w2ClosureOwnedQueue struct {
	gcpkg.GCStore
	org      uuid.UUID
	block    string
	identity gcpkg.BlockGCCandidateIdentity
	visited  bool
}

func (s *w2ClosureOwnedQueue) DequeueBatch(org uuid.UUID, _ int, cutoff time.Time) ([]gcpkg.QueueItem, error) {
	rows, err := s.GCStore.DequeueBatch(org, 100000, cutoff)
	if err != nil {
		return nil, err
	}
	var own []gcpkg.QueueItem
	for _, row := range rows {
		if row.OrgID == s.org && row.ItemType == gcpkg.ItemBlock && row.ItemID == s.block && row.BlockGCCandidateIdentity == s.identity {
			own = append(own, row)
		}
	}
	if len(own) != 1 {
		return nil, fmt.Errorf("expected exactly one owned real queue row, found %d", len(own))
	}
	return own, nil
}
func (s *w2ClosureOwnedQueue) BlockPublicationLivenessGlobal(org uuid.UUID, block string) (dbpkg.BlockPublicationLiveness, error) {
	if org == s.org && block == s.block {
		s.visited = true
	}
	return s.GCStore.BlockPublicationLivenessGlobal(org, block)
}

func w2AssertGCBlocked(t *testing.T, fx *w2CreateFileFixture) {
	t.Helper()
	store := gcpkg.NewCassandraStore(fx.database)
	c := w2Candidate(t, store, fx.orgUUID, fx.blockID, fx.target.StorageClass)
	for attempt := 0; attempt < 3; attempt++ {
		scope := &w2ClosureOwnedQueue{GCStore: store, org: fx.orgUUID, block: fx.blockID, identity: c.Identity()}
		n, workerErr := w2Worker(t, scope, fx.target.StorageClass).ProcessOrgOnce(t.Context(), fx.orgUUID)
		// A daemon already visiting this org can postpone discovery past our
		// captured dequeue cutoff. Retry the real read with a fresh cutoff only
		// while the same candidate/P remain protected; do not enqueue a duplicate.
		// Never count the peer as our proof: success requires our own read.
		peerConsumed := n == 0 && !scope.visited && workerErr != nil && strings.Contains(workerErr.Error(), "expected exactly one owned real queue row, found 0")
		if !peerConsumed && (workerErr != nil || n != 0) {
			t.Fatalf("GC progressed while publication unresolved: n=%d err=%v", n, workerErr)
		}
		if _, found, err := store.GetBlockGCCandidateExact(fx.orgUUID, fx.blockID, c.Identity()); err != nil || !found {
			t.Fatalf("guard lost candidate: found=%v err=%v", found, err)
		}
		x1AssertCanonicalPresent(t, store, fx.orgUUID, fx.blockID, fx.target.StorageKey)
		w2AssertBytes(t, fx)
		if peerConsumed {
			t.Logf("peer consumed discovery attempt %d; exact candidate/P intact; require our own real liveness probe", attempt+1)
			continue
		}
		if !scope.visited {
			t.Fatal("productive worker did not execute the own candidate liveness probe")
		}
		return
	}
	t.Fatal("three discovery attempts failed to obtain our own real blocked GC probe")
}

func w2RecoverApplied(t *testing.T, fx *w2CreateFileFixture) {
	t.Helper()
	// Recovery uses a fresh session, never the writer/proxy session.
	recovery := w2EvidenceSession(t, splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042")[0], nil)
	for _, r := range w2Repairs(t, fx) {
		if err := recovery.Session().Query(`UPDATE published_block_reference_repairs SET created_at = ?, lease_expires_at = ? WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?`, time.Now().Add(-40*24*time.Hour), time.Now().Add(-40*24*time.Hour), r.bucket, fx.orgID, fx.repoID, r.commitID, r.fsID).Exec(); err != nil {
			t.Fatal(err)
		}
	}
	if err := v2pkg.RunPublishedBlockReferenceRepairSweepForIntegration(recovery); err != nil {
		t.Fatalf("independent recovery: %v", err)
	}
	if len(w2Repairs(t, fx)) != 0 || !fx.hasOwnFSReferrer(t) {
		t.Fatal("independent recovery did not promote reachable fs and settle repair")
	}
	w2AssertBytes(t, fx)
}
func TestW2NativeHEADAmbiguity(t *testing.T) {
	requireCassandra(t)
	for _, funnel := range w2ClosureFunnels {
		for _, leg := range []string{"appliedReadable", "appliedUnconfirmed", "requestLostUnconfirmed"} {
			t.Run(funnel+"/"+leg, func(t *testing.T) {
				w2ObserveClosure(t)
				fx, data := w2ClosureFixture(t, funnel)
				proxy := newW2WireProxy(t)
				observer := &w2HeadErrorObserver{}
				writer := w2EvidenceSession(t, proxy.listener.Addr().String(), observer)
				expectedHead := ""

				t.Cleanup(v2pkg.SetW2PublicationAfterAuthorityForTest(fx.repoID, func() {
					fx.target = fx.readTarget(t)
					if funnel == "BorrowedFS" {
						fx.dropForeignFS(t)
					}
					w2ExpireTemporaryRefs(t, fx)
					w2AssertGuardOnly(t, fx)
					rows := w2Repairs(t, fx)
					if len(rows) != 1 {
						t.Fatal("final authority did not acquire exactly this attempt repair")
					}
					expectedHead = rows[0].commitID
					proxy.arm(leg == "requestLostUnconfirmed", leg != "appliedReadable")
				}))
				rec := w2InvokeWriter(t, fx, data, writer)
				proxy.restore()
				select {
				case <-proxy.reached:
				default:
					t.Fatal("no actual HEAD request/response was lost")
				}
				observer.mu.Lock()
				errs := append([]error(nil), observer.errors...)
				observer.mu.Unlock()
				if len(errs) == 0 {
					t.Fatal("HEAD driver reported no error after native wire loss")
				}
				for _, err := range errs {
					t.Logf("actual HEAD driver error: %T: %v", err, err)
					if !errors.Is(err, gocql.ErrTimeoutNoResponse) && !errors.Is(err, gocql.ErrConnectionClosed) {
						t.Fatalf("unexpected real HEAD error: %T %v", err, err)
					}
				}
				if leg == "appliedReadable" && !strings.HasPrefix(funnel, "Sync") {
					if head := borrowedFSReadHead(t, fx.database, fx.orgID, fx.repoID); head != expectedHead {
						t.Fatalf("confirmed HEAD=%s differs from attempt repair %s", head, expectedHead)
					}
					if rec.Code < 200 || rec.Code >= 300 {
						t.Fatalf("confirmed applied status=%d body=%s", rec.Code, rec.Body.String())
					}
					fx.assertHeadAdvanced(t)
					if len(w2Repairs(t, fx)) != 0 || !fx.hasOwnFSReferrer(t) {
						t.Fatal("confirmed publication did not settle")
					}
					w2AssertBytes(t, fx)
					return
				}
				if rec.Code < 500 {
					t.Fatalf("unconfirmed HEAD must report failure: status=%d body=%s", rec.Code, rec.Body.String())
				}
				if len(w2Repairs(t, fx)) != 1 {
					t.Fatal("UNKNOWN wire outcome removed durable repair")
				}
				w2ExpireTemporaryRefs(t, fx)
				w2AssertGuardOnly(t, fx)
				w2AssertGCBlocked(t, fx)
				if leg != "requestLostUnconfirmed" {
					if head := borrowedFSReadHead(t, fx.database, fx.orgID, fx.repoID); head != expectedHead {
						t.Fatalf("applied HEAD=%s differs from attempt repair %s", head, expectedHead)
					}
					fx.assertHeadAdvanced(t)
					w2RecoverApplied(t, fx)
				} else {
					fx.assertHeadUnchanged(t)
				}
			})
		}
	}
}

type w2ChildFixture struct {
	Org, Repo, User, Head, Filename, Phase, Marker, Proxy, Class, Funnel, TargetCommit, Block, SHA1, Session string
	Content                                                                                                  []byte
}

func TestW2ProcessWriterChild(t *testing.T) {
	if os.Getenv("SESAMEFS_W2_PROCESS_CHILD") != "1" {
		t.Skip("parent-only subprocess helper")
	}
	var data w2ChildFixture
	if err := json.Unmarshal([]byte(os.Getenv("SESAMEFS_W2_CHILD_FIXTURE")), &data); err != nil {
		t.Fatal(err)
	}
	database := shareProjectionDBForTest(t)
	if data.Proxy != "" {
		database = w2EvidenceSession(t, data.Proxy, nil)
	}
	stop := func() {
		if err := os.WriteFile(data.Marker, []byte(data.Phase), 0600); err != nil {
			t.Fatal(err)
		}
		for {
			time.Sleep(time.Second)
		}
	}
	switch data.Phase {
	case "beforeRepair":
		if strings.HasPrefix(data.Funnel, "Sync") {
			v2pkg.SetW2PublicationBeforeRepairForTest(data.Repo, stop)
		} else {
			v2pkg.SetFileFromBlocksPublicationBarriersForTest(data.Repo, nil, nil, stop, nil)
		}
	case "afterAuthority":
		v2pkg.SetW2PublicationAfterAuthorityForTest(data.Repo, stop)
	case "afterHEAD":
		v2pkg.SetW2PublicationAfterHeadForTest(data.Repo, stop)
	case "inFlightAppliedHEAD": // Parent proxy reports real coordinator response.
	default:
		t.Fatal("unknown child phase")
	}
	org, err := uuid.Parse(data.Org)
	if err != nil {
		t.Fatal(err)
	}
	inner := &borrowedFSHeadFixture{database: database, handler: newBorrowedFSHeadHandler(t, database, data.Class), repoID: data.Repo, orgID: data.Org, orgUUID: org, userID: data.User, filename: data.Filename, headBefore: data.Head, content: data.Content, blockID: data.Block, sha1ID: data.SHA1, sessionID: data.Session}
	fx := &w2CreateFileFixture{w2UploadFileFixture: &w2UploadFileFixture{borrowedFSHeadFixture: inner}}
	rec := w2InvokeWriter(t, fx, data, database)
	t.Fatalf("child writer returned before SIGKILL: %d %s", rec.Code, rec.Body.String())
}
func TestW2ProcessKillContinuity(t *testing.T) {
	if runtime.GOOS != "linux" {
		if os.Getenv(w2ClosureEvidenceEnv) == "1" {
			t.Fatal("required OS SIGKILL evidence must run in Linux Docker")
		}
		t.Skip("OS SIGKILL evidence runs in Linux Docker")
	}
	requireCassandra(t)
	for _, funnel := range w2ClosureFunnels {
		phases := []string{"beforeRepair", "afterAuthority", "inFlightAppliedHEAD"}
		if funnel == "Office" {
			phases = append(phases, "afterHEAD")
		}
		for _, phase := range phases {
			t.Run(funnel+"/"+phase, func(t *testing.T) {
				w2ObserveClosure(t)
				fx, data := w2ClosureFixture(t, funnel)
				marker := filepath.Join(t.TempDir(), "ready")
				data.Phase = phase
				data.Marker = marker
				var proxy *w2WireProxy
				if phase == "inFlightAppliedHEAD" {
					proxy = newW2WireProxy(t)
					data.Proxy = proxy.listener.Addr().String()
					proxy.arm(false, true)
				}
				encoded, err := json.Marshal(data)
				if err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command(os.Args[0], "-test.run=^TestW2ProcessWriterChild$", "-test.v")
				var output bytes.Buffer
				cmd.Stdout = &output
				cmd.Stderr = &output
				// Child TestMain bypasses global cleanup/evidence accounting. It cannot
				// delete fixtures belonging to its live parent or claim a skipped test.
				for _, e := range os.Environ() {
					if !strings.HasPrefix(e, "SESAMEFS_W2_PROCESS_CHILD=") && !strings.HasPrefix(e, "SESAMEFS_W2_CHILD_FIXTURE=") {
						cmd.Env = append(cmd.Env, e)
					}
				}
				cmd.Env = append(cmd.Env, "SESAMEFS_W2_PROCESS_CHILD=1", "SESAMEFS_W2_CHILD_FIXTURE="+string(encoded))
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				exited := make(chan error, 1)
				go func() { exited <- cmd.Wait() }()
				finished := false
				t.Cleanup(func() {
					if !finished {
						cmd.Process.Kill()
						<-exited
					}
				})
				deadline := time.NewTimer(45 * time.Second)
				defer deadline.Stop()
				tick := time.NewTicker(25 * time.Millisecond)
				defer tick.Stop()
				ready := false
				for !ready {
					select {
					case err := <-exited:
						finished = true
						t.Fatalf("child exited before barrier: %v\n%s", err, output.String())
					case <-deadline.C:
						t.Fatal("child did not reach durable barrier")
					case <-tick.C:
						if proxy != nil {
							select {
							case <-proxy.reached:
								ready = true
							default:
							}
						} else {
							_, err := os.Stat(marker)
							ready = err == nil
						}
					}
				}
				fx.target = fx.readTarget(t)
				if funnel == "BorrowedFS" {
					fx.dropForeignFS(t)
				}
				rows := w2Repairs(t, fx)
				if phase == "beforeRepair" {
					if len(rows) != 0 {
						t.Fatal("before-repair barrier already acquired repair")
					}
				} else {
					if len(rows) != 1 {
						t.Fatal("barrier missing durable repair")
					}
				}
				if phase == "afterHEAD" || phase == "inFlightAppliedHEAD" {
					if len(rows) != 1 || borrowedFSReadHead(t, fx.database, fx.orgID, fx.repoID) != rows[0].commitID {
						t.Fatal("applied HEAD does not match the killed writer durable repair")
					}
					fx.assertHeadAdvanced(t)
				} else {
					fx.assertHeadUnchanged(t)
				}
				if err := cmd.Process.Kill(); err != nil {
					t.Fatal(err)
				}
				err = <-exited
				finished = true
				var exit *exec.ExitError
				if !errors.As(err, &exit) {
					t.Fatalf("SIGKILL did not terminate child: %v", err)
				}
				if !w2WasSIGKILL(exit) {
					t.Fatalf("child was not killed by OS SIGKILL: %v", exit)
				}
				t.Logf("verified OS SIGKILL at %s", phase)
				w2ExpireTemporaryRefs(t, fx)
				if phase == "beforeRepair" {
					store := gcpkg.NewCassandraStore(fx.database)
					d := x1CommitHandoffAfterZeroRefs(t, store, fx.orgUUID, fx.blockID, x1Attempt(fx.target, "w2-killed-before-repair"))
					fx.assertDUnrevoked(t, d)
					fx.assertHeadUnchanged(t)
					return
				}
				w2AssertGuardOnly(t, fx)
				w2AssertGCBlocked(t, fx)
				if phase == "afterHEAD" || phase == "inFlightAppliedHEAD" {
					if len(rows) != 1 || borrowedFSReadHead(t, fx.database, fx.orgID, fx.repoID) != rows[0].commitID {
						t.Fatal("applied HEAD does not match the killed writer durable repair")
					}
					w2RecoverApplied(t, fx)
				}
			})
		}
	}
}

func TestW2ClosureEvidenceCompleteness(t *testing.T) {
	if len(w2ClosureRequired) != 37 {
		t.Fatal("closure must require every real-wire and process-kill leg")
	}
	seen := map[string]bool{}
	for _, name := range w2ClosureRequired {
		if seen[name] {
			t.Fatalf("duplicate evidence leg %s", name)
		}
		seen[name] = true
	}

}
