//go:build integration

package integration

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
)

const e115eEvidenceEnv = "SESAMEFS_REQUIRE_E115E_REPAIR_SETTLEMENT_EVIDENCE"

var e115eLegs = []string{"parent-head-retains", "superseded-unblocks-d", "sync-after-settlement", "sync-paused-before-repair", "sync-paused-before-cas", "stale-visitor-renew"}
var e115eObserved = map[string]bool{}

func e115eMissing(seen map[string]bool) []string {
	var out []string
	for _, leg := range e115eLegs {
		if !seen[leg] {
			out = append(out, leg)
		}
	}
	return out
}

// One productive sweep on its own observed session: the classifier outcome of
// every visited repair of this library and the block_references writes.
type e115eSweepResult struct {
	outcomes         []string
	err              error
	inserts, deletes []string
}

func e115eSweep(t *testing.T, fx *w2CreateFileFixture, at time.Duration) e115eSweepResult {
	t.Helper()
	writes := &e115aWrites{org: fx.orgID, block: fx.blockID}
	database := w2EvidenceSession(t, splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042")[0], writes)
	var mu sync.Mutex
	var res e115eSweepResult
	restore := v2pkg.SetRepairAfterClassifyForIntegration(database, fx.repoID, func(outcome string, err error) {
		mu.Lock()
		defer mu.Unlock()
		res.outcomes = append(res.outcomes, fmt.Sprintf("%s/%v", outcome, err))
	})
	res.err = v2pkg.RunPublishedBlockReferenceRepairSweepAtForIntegration(database, time.Now().Add(at))
	restore()
	res.inserts, res.deletes = writes.snapshot()
	return res
}

// The settlement sweep: R classified natively SUPERSEDED, then exactly R's own
// pub: removed and the R row deleted; nothing else written to L.
func e115eSettle(t *testing.T, f *e114Fixture, r w2Repair, at time.Duration) {
	t.Helper()
	fx := f.fx
	owned := e115aRepairOwned(fx, r)
	res := e115eSweep(t, fx, at)
	superseded := 0
	for _, o := range res.outcomes {
		if o == "superseded/<nil>" {
			superseded++
		}
	}
	// The sweep covers the whole keyspace; only an error about R itself counts.
	if superseded != 1 || (res.err != nil && strings.Contains(res.err.Error(), r.commitID)) {
		t.Fatalf("settlement sweep at +%s: outcomes=%v err=%v", at, res.outcomes, res.err)
	}
	if len(res.inserts) != 0 || len(res.deletes) != 1 || res.deletes[0] != owned {
		t.Fatalf("settlement writes on L: inserts=%v deletes=%v (want only delete %s)", res.inserts, res.deletes, owned)
	}
	for _, row := range w2Repairs(t, fx) {
		if row.commitID == r.commitID {
			t.Fatalf("R survived settlement: %+v", row)
		}
	}
	for _, ref := range f.refsExact(t) {
		if ref == owned {
			t.Fatalf("R's own %s survived settlement", owned)
		}
	}
	t.Logf("E1-15E settlement at +%s: native SUPERSEDED for R=%s; removed only %s and the R row", at, r.commitID, owned)
}

func e115eCrashAndAdvance(t *testing.T, f *e114Fixture) (w2Repair, string) {
	t.Helper()
	fx := f.fx
	e115dCrashAfterQueueNoGC(t, f)
	r := e115dAbandoned(t, fx)
	if parent := e115dParent(t, fx, r.commitID); parent != fx.headBefore {
		t.Fatalf("abandoned commit parent %s != original HEAD %s", parent, fx.headBefore)
	}
	return r, e115dAdvanceHead(t, f)
}

func e115eHasRef(refs []string, want string) bool {
	for _, ref := range refs {
		if ref == want {
			return true
		}
	}
	return false
}

// Sync UpdateBranch?head=c1 must publish c1's content with its own fs: and
// settle its own repair; the bytes stay readable.
func e115eAssertSyncPublished(t *testing.T, f *e114Fixture, r w2Repair, advanced string, code int, body string) {
	t.Helper()
	fx := f.fx
	head := borrowedFSReadHead(t, fx.database, fx.orgID, fx.repoID)
	entries := e115cHeadEntries(t, fx, head)
	if code != http.StatusOK || head == advanced || entries[fx.filename] != r.fsID || e115dParent(t, fx, head) != advanced {
		t.Fatalf("Sync did not publish c1's content: status=%d body=%s head=%s entries=%v", code, strings.TrimSpace(body), head, entries)
	}
	if !e115eHasRef(f.refsExact(t), dbpkg.BlockReferrerForFSObject(fx.repoID, r.fsID)) {
		t.Fatal("Sync published c1's file without its own fs:")
	}
	if rows := w2Repairs(t, fx); len(rows) != 0 {
		t.Fatalf("repairs left after Sync publication: %+v", rows)
	}
	if fx.readTarget(t) != fx.target {
		t.Fatal("exact P1 changed")
	}
	w2AssertBytes(t, fx)
}

// Sync promotion of c1 paused inside its own publication; while paused, R is
// settled and the writer's real references are retired by TTL. Sync's own
// staged references must keep L live, so Phase 0 cannot make a candidate.
func e115eSyncPaused(t *testing.T, f *e114Fixture, beforeCAS bool) {
	t.Helper()
	fx := f.fx
	r, advanced := e115eCrashAndAdvance(t, f)
	writerRefs := f.refsExact(t)
	paused, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	barrier := func() { once.Do(func() { close(paused); <-resume }) }
	set := v2pkg.SetW2PublicationBeforeRepairForTest
	if beforeCAS {
		set = v2pkg.SetW2PublicationAfterAuthorityForTest
	}
	t.Cleanup(set(fx.repoID, barrier))
	type result struct {
		code int
		body string
	}
	done := make(chan result, 1)
	go func() {
		code, body := e115dSyncPromote(t, fx, r.commitID)
		done <- result{code, body}
	}()
	resumed := false
	t.Cleanup(func() {
		if !resumed {
			close(resume)
			<-done
		}
	})
	select {
	case <-paused:
	case res := <-done:
		t.Fatalf("Sync finished before its pause: %d %s", res.code, res.body)
	case <-time.After(30 * time.Second):
		t.Fatal("Sync did not reach its pause")
	}
	res := e115eSweep(t, fx, 2*time.Hour)
	if beforeCAS {
		// Sync's own merged-commit repair is queued and its parent is the
		// current HEAD (A = P): it must stay UNKNOWN and retained.
		if len(res.outcomes) != 2 || !strings.Contains(strings.Join(res.outcomes, ","), "superseded/<nil>") || !strings.Contains(strings.Join(res.outcomes, ","), "unknown/") {
			t.Fatalf("paused-before-CAS sweep: outcomes=%v err=%v", res.outcomes, res.err)
		}
		rows := w2Repairs(t, fx)
		if len(rows) != 1 || rows[0].commitID == r.commitID {
			t.Fatalf("Sync's own repair must survive and R must be gone: %+v", rows)
		}
	} else {
		if len(res.outcomes) != 1 || res.outcomes[0] != "superseded/<nil>" || (res.err != nil && strings.Contains(res.err.Error(), r.commitID)) {
			t.Fatalf("paused-before-repair sweep: outcomes=%v err=%v", res.outcomes, res.err)
		}
		if rows := w2Repairs(t, fx); len(rows) != 0 {
			t.Fatalf("R must be gone: %+v", rows)
		}
	}
	if e115eHasRef(f.refsExact(t), e115aRepairOwned(fx, r)) {
		t.Fatal("R's own pub: survived settlement")
	}
	e115aRetireByTTL(t, f, writerRefs)
	if live, err := fx.database.BlockHasReferencesGlobal(fx.orgID, fx.blockID); err != nil || !live {
		t.Fatalf("in-flight Sync left L without references: live=%t err=%v refs=%v", live, err, f.refsExact(t))
	}
	scope, _ := f.phase0(t)
	if candidates := gcCandidateIdentitiesForTest(t, fx.orgID, fx.blockID); len(candidates) != 0 {
		f.candidates = append(f.candidates, candidates...)
		t.Fatalf("Phase 0 made a candidate while Sync was in flight: listed=%d %+v", scope.provisional, candidates)
	}
	close(resume)
	resumed = true
	out := <-done
	e115eAssertSyncPublished(t, f, r, advanced, out.code, out.body)
	t.Logf("E1-15E Sync paused %s: R settled (SUPERSEDED) and the writer's refs retired; L stayed live on Sync's own refs; Phase 0 made no candidate; Sync then published c1's content with its own fs:", map[bool]string{false: "after staging+readiness", true: "after its own repair, before HEAD CAS"}[beforeCAS])
}

func TestAbandonedRepairSettlement(t *testing.T) {
	if endpoint := os.Getenv("SESAMEFS_E115E_ISOLATED_URL"); endpoint != "" && os.Getenv("SESAMEFS_E115E_CHILD") != "1" {
		e115eRunIsolated(t, endpoint)
		return
	}
	requireCassandra(t)
	if runtime.GOOS != "linux" || os.Getenv("SESAMEFS_TEST_IN_CONTAINER") != "1" {
		t.Fatal("E1-15E requires Linux Docker SIGKILL")
	}
	for _, endpoint := range []string{superadminClient.baseURL, envOrDefault("SESAMEFS_URL_2", "http://sesamefs-node-2:8080"), envOrDefault("SESAMEFS_URL_3", "http://sesamefs-node-3:8080")} {
		if err := e19CheckGCDisabled(newTestClient(endpoint, superadminClient.token)); err != nil {
			t.Fatalf("E1-15E isolation: %v", err)
		}
	}
	for _, leg := range e115eLegs {
		t.Run(leg, func(t *testing.T) {
			t.Cleanup(func() {
				if !t.Failed() && !t.Skipped() {
					e115eObserved[leg] = true
				}
			})
			f := e115bFixture(t)
			fx := f.fx
			// Deferred before GC runs: completes an owned COMMITTED D on any exit.
			defer e115bFinalizeCommitted(t, fx)
			switch leg {
			case "parent-head-retains":
				e115dCrashAfterQueueNoGC(t, f)
				r := e115dAbandoned(t, fx)
				if borrowedFSReadHead(t, fx.database, fx.orgID, fx.repoID) != e115dParent(t, fx, r.commitID) {
					t.Fatal("HEAD must still be c1.parent")
				}
				sweeper := e115cNewSweeper(t, fx)
				sweeper.age(t, fx, r)
				sweeper.sweep(t, 2*time.Hour, e115aRepairOwned(fx, r))
				if rows := w2Repairs(t, fx); len(rows) != 1 || rows[0].commitID != r.commitID {
					t.Fatalf("R must be retained while HEAD = c1.parent: %+v", rows)
				}
				t.Logf("E1-15E HEAD = c1.parent: no witness; R=%s natively UNKNOWN, retained and renewed", r.commitID)
			case "superseded-unblocks-d":
				r, advanced := e115eCrashAndAdvance(t, f)
				e115eSettle(t, f, r, 2*time.Hour)
				if fx.hasOwnFSReferrer(t) || borrowedFSReadHead(t, fx.database, fx.orgID, fx.repoID) != advanced || fx.readTarget(t) != fx.target {
					t.Fatal("settlement promoted fs:, moved HEAD or changed P1")
				}
				w2AssertBytes(t, fx)
				if again := e115eSweep(t, fx, 4*time.Hour); len(again.outcomes) != 0 || len(again.inserts)+len(again.deletes) != 0 {
					t.Fatalf("settled R visited again: %+v", again)
				}
				// Convergence: with R gone, retiring the writer's real refs lets a
				// natural D(P1) reach exact COMMITTED (E1-15C vetoed this).
				refs := f.refsExact(t)
				e115aNaturalD(t, f, e115aRetireByTTL(t, f, refs))
				t.Logf("E1-15E RESULT: after HEAD left c1.parent, R=%s settled natively; the writer's refs %v then expired and a natural D(P1) reached exact COMMITTED with no RepairGuardOnly veto", r.commitID, refs)
			case "sync-after-settlement":
				r, advanced := e115eCrashAndAdvance(t, f)
				e115eSettle(t, f, r, 2*time.Hour)
				code, body := e115dSyncPromote(t, fx, r.commitID)
				e115eAssertSyncPublished(t, f, r, advanced, code, body)
				t.Logf("E1-15E Sync after settlement: UpdateBranch?head=%s published c1's content with its own fs: and settled its own repair, without R", r.commitID)
			case "sync-paused-before-repair":
				e115eSyncPaused(t, f, false)
			case "sync-paused-before-cas":
				e115eSyncPaused(t, f, true)
			case "stale-visitor-renew":
				e115dCrashAfterQueueNoGC(t, f)
				r := e115dAbandoned(t, fx)
				owned := e115aRepairOwned(fx, r)
				v := e115aStartVisitor(t, fx, r.commitID, r.fsID, "renew")
				e115dAdvanceHead(t, f)
				e115eSettle(t, f, r, 4*time.Hour)
				if err := v.finish(t); err != nil {
					t.Fatalf("stale visitor: %v", err)
				}
				inserts, deletes := v.writes.snapshot()
				if v.outcome != "unknown" || len(inserts) != 1 || inserts[0] != owned || len(deletes) != 1 || deletes[0] != owned {
					t.Fatalf("stale visitor: outcome=%s inserts=%v deletes=%v", v.outcome, inserts, deletes)
				}
				if e115eHasRef(f.refsExact(t), owned) || len(w2Repairs(t, fx)) != 0 {
					t.Fatal("stale renewal left R's pub: or row")
				}
				t.Logf("E1-15E stale visitor: classified UNKNOWN at HEAD = c1.parent and paused before renewal; HEAD advanced; a second sweep settled R; the late renewal of %s was withdrawn by the global-absence check", owned)
			}
		})
	}
}

func e115eRunIsolated(t *testing.T, endpoint string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	run := "^TestAbandonedRepairSettlement$"
	if _, sub, ok := strings.Cut(flag.Lookup("test.run").Value.String(), "/"); ok {
		run += "/" + sub
	}
	cmd := exec.CommandContext(ctx, binary, "-test.run="+run, "-test.v", "-test.count=1", "-test.timeout=7m")
	for _, entry := range os.Environ() {
		name := strings.SplitN(entry, "=", 2)[0]
		if strings.HasPrefix(name, "SESAMEFS_REQUIRE_") || strings.HasSuffix(name, "_CHILD") || name == "SESAMEFS_URL" || name == "SESAMEFS_URL_2" || name == "SESAMEFS_URL_3" || name == "CASSANDRA_KEYSPACE" {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, e115eEvidenceEnv+"=1", "SESAMEFS_E115E_CHILD=1", "CASSANDRA_KEYSPACE=sesamefs_e19", "SESAMEFS_URL="+endpoint, "SESAMEFS_URL_2="+endpoint, "SESAMEFS_URL_3="+endpoint)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("isolated E1-15E: %v", err)
	}
	for _, leg := range e115eLegs {
		e115eObserved[leg] = true
	}
}

func TestAbandonedRepairSettlementCompleteness(t *testing.T) {
	if len(e115eMissing(nil)) != len(e115eLegs) {
		t.Fatal("every leg required")
	}
	all := map[string]bool{}
	for _, leg := range e115eLegs {
		all[leg] = true
	}
	if len(e115eMissing(all)) != 0 {
		t.Fatal("complete rejected")
	}
	for _, leg := range e115eLegs {
		delete(all, leg)
		if missing := e115eMissing(all); len(missing) != 1 || missing[0] != leg {
			t.Fatalf("omission %s: %v", leg, missing)
		}
		all[leg] = true
	}
}
