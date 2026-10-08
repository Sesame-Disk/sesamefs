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
	"github.com/Sesame-Disk/sesamefs/internal/config"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

const e115cEvidenceEnv = "SESAMEFS_REQUIRE_E115C_POST_D_PHYSICAL_EVIDENCE"

var e115cLegs = []string{"post-d-pub-terminal", "post-d-repair-terminal", "p2-published-with-dead-repair", "dead-repair-blocks-unreferenced-p2"}
var e115cObserved = map[string]bool{}

func e115cMissing(seen map[string]bool) []string {
	var out []string
	for _, leg := range e115cLegs {
		if !seen[leg] {
			out = append(out, leg)
		}
	}
	return out
}

// The E1-15B schedule: a real writer subprocess resumed after a natural exact
// COMMITTED D(P1) is SIGKILLed after staging ("crash-before-queue") or after
// repair queueing and insertCommit ("crash-after-queue").
func e115cCrashAfterD(t *testing.T, f *e114Fixture, phase string) {
	t.Helper()
	fx := f.fx
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
	if refs := f.refsExact(t); len(refs) != 1 || !strings.HasPrefix(refs[0], "up:") || len(w2Repairs(t, fx)) != 0 {
		t.Fatalf("before staging only the writer's real up: may exist: refs=%v", refs)
	}
	e115aCommitD(t, f)
	if err := os.WriteFile(data.Marker+".resume", nil, 0600); err != nil {
		t.Fatal(err)
	}
	marker := map[string]string{"crash-before-queue": ".staged", "crash-after-queue": ".beforefence"}[phase]
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
	f.assertPostD(t)
}

// Productive continuation to TERMINAL while the post-D references are present,
// then positive evidence that P1 stayed retired and nothing was revoked.
func e115cTerminal(t *testing.T, f *e114Fixture, p1 gcpkg.BlockDeleteTarget, refsBefore []string) {
	t.Helper()
	fx := f.fx
	e115bFinalizeCommitted(t, fx)
	var phase, key string
	if err := fx.database.Session().Query(`SELECT phase,storage_key FROM gc_block_delete_lifecycles WHERE org_id=? AND block_id=?`, fx.orgID, fx.blockID).Consistency(gocql.EachQuorum).Scan(&phase, &key); err != nil || phase != gcpkg.BlockDeleteLifecyclePhaseTerminal || key != p1.StorageKey {
		t.Fatalf("D1 not TERMINAL at P1: phase=%s key=%s err=%v", phase, key, err)
	}
	if exists, err := gcpkg.NewCassandraStore(fx.database).BlockExists(fx.orgUUID, fx.blockID); err != nil || exists {
		t.Fatalf("P1 canonical reinstalled after TERMINAL: %t %v", exists, err)
	}
	if exists, err := newVerificationBlockStore(t, fx.orgID).ObjectExists(t.Context(), p1.StorageKey); err != nil || exists {
		t.Fatalf("K1 survived TERMINAL: %t %v", exists, err)
	}
	store := gcpkg.NewCassandraStore(fx.database)
	for bucket := 0; bucket < dbpkg.GCDiscoveryBucketCount; bucket++ {
		var pageState []byte
		for {
			page, err := store.ListS3OrphanRecoveryRoots(bucket, pageState, 1000)
			if err != nil {
				t.Fatal(err)
			}
			for _, root := range page.Roots {
				if root.OrgID == fx.orgUUID && root.BlockID == fx.blockID {
					t.Fatalf("recovery root survived TERMINAL: %+v", root)
				}
			}
			if pageState = page.PageState; len(pageState) == 0 {
				break
			}
		}
	}
	refs := f.refsExact(t)
	if strings.Join(refs, ",") != strings.Join(refsBefore, ",") {
		t.Fatalf("TERMINAL changed post-D references: %v -> %v", refsBefore, refs)
	}
	if borrowedFSReadHead(t, fx.database, fx.orgID, fx.repoID) != f.head || fx.hasOwnFSReferrer(t) {
		t.Fatal("HEAD/fs: changed across TERMINAL")
	}
	t.Logf("E1-15C TERMINAL with post-D refs present %v: D1 TERMINAL at P1=(%s,%s); K1/orphan/root absent; canonical not reinstalled; HEAD unchanged", refs, p1.StorageClass, p1.StorageKey)
}

// Productive sweeps of a real repair on an observed session; returns the
// classifier outcomes and the reference inserts made by those sweeps.
type e115cSweeper struct {
	db       *dbpkg.DB
	writes   *e115aWrites
	mu       sync.Mutex
	outcomes []string
}

func e115cNewSweeper(t *testing.T, fx *w2CreateFileFixture) *e115cSweeper {
	t.Helper()
	s := &e115cSweeper{writes: &e115aWrites{org: fx.orgID, block: fx.blockID}}
	s.db = w2EvidenceSession(t, splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042")[0], s.writes)
	t.Cleanup(v2pkg.SetRepairAfterClassifyForIntegration(s.db, fx.repoID, func(outcome string, err error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.outcomes = append(s.outcomes, fmt.Sprintf("%s/%v", outcome, err))
	}))
	return s
}

func (s *e115cSweeper) sweep(t *testing.T, at time.Duration, owned string) {
	t.Helper()
	before, _ := s.writes.snapshot()
	err := v2pkg.RunPublishedBlockReferenceRepairSweepAtForIntegration(s.db, time.Now().Add(at))
	after, deletes := s.writes.snapshot()
	s.mu.Lock()
	outcomes := append([]string(nil), s.outcomes...)
	s.mu.Unlock()
	if len(outcomes) == 0 || outcomes[len(outcomes)-1] != "unknown/<nil>" || err == nil || !strings.Contains(err.Error(), "unknown; retain queued repair") {
		t.Fatalf("sweep at +%s: outcomes=%v err=%v", at, outcomes, err)
	}
	if len(after) != len(before)+1 || after[len(after)-1] != owned || len(deletes) != 0 {
		t.Fatalf("sweep at +%s writes: %v -> %v deletes=%v", at, before, after, deletes)
	}
	t.Logf("E1-15C dead repair sweep at +%s: native UNKNOWN, renewed %s on logical block L", at, owned)
}

func (s *e115cSweeper) age(t *testing.T, fx *w2CreateFileFixture, r w2Repair) {
	t.Helper()
	if err := fx.database.Session().Query(`UPDATE published_block_reference_repairs SET created_at=?,lease_expires_at=? WHERE bucket=? AND org_id=? AND repo_id=? AND commit_id=? AND fs_id=?`, time.Now().Add(-time.Hour), time.Now().Add(time.Hour), r.bucket, fx.orgID, fx.repoID, r.commitID, r.fsID).Consistency(gocql.LocalQuorum).Exec(); err != nil {
		t.Fatal(err)
	}
}

// B: the dead repair R plus its two post-D renewals, then TERMINAL.
func e115cDeadRepairTerminal(t *testing.T, f *e114Fixture) (gcpkg.BlockDeleteTarget, w2Repair, string, *e115cSweeper) {
	t.Helper()
	fx := f.fx
	e115cCrashAfterD(t, f, "crash-after-queue")
	p1 := fx.target
	rows := w2Repairs(t, fx)
	if len(rows) != 1 || len(rows[0].blocks) != 1 || rows[0].blocks[0] != fx.blockID {
		t.Fatalf("post-D repair missing: %+v", rows)
	}
	r := rows[0]
	owned := e115aRepairOwned(fx, r)
	sweeper := e115cNewSweeper(t, fx)
	sweeper.age(t, fx, r)
	sweeper.sweep(t, 2*time.Hour, owned)
	sweeper.sweep(t, 6*time.Hour, owned)
	e115cTerminal(t, f, p1, f.refsExact(t))
	if rows := w2Repairs(t, fx); len(rows) != 1 || rows[0].commitID != r.commitID {
		t.Fatalf("dead repair not retained across TERMINAL: %+v", rows)
	}
	return p1, r, owned, sweeper
}

func e115cHeadEntries(t *testing.T, fx *w2CreateFileFixture, head string) map[string]string {
	t.Helper()
	var root, entries string
	if err := fx.database.Session().Query(`SELECT root_fs_id FROM commits WHERE library_id=? AND commit_id=?`, fx.repoID, head).Consistency(gocql.EachQuorum).Scan(&root); err != nil {
		t.Fatal(err)
	}
	if err := fx.database.Session().Query(`SELECT dir_entries FROM fs_objects WHERE library_id=? AND fs_id=?`, fx.repoID, root).Consistency(gocql.EachQuorum).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	var dirents []v2pkg.FSEntry
	if err := json.Unmarshal([]byte(entries), &dirents); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, d := range dirents {
		out[d.Name] = d.ID
	}
	return out
}

func TestPostDPhysicalLifeDisposition(t *testing.T) {
	if endpoint := os.Getenv("SESAMEFS_E115C_ISOLATED_URL"); endpoint != "" && os.Getenv("SESAMEFS_E115C_CHILD") != "1" {
		e115cRunIsolated(t, endpoint)
		return
	}
	requireCassandra(t)
	if runtime.GOOS != "linux" || os.Getenv("SESAMEFS_TEST_IN_CONTAINER") != "1" {
		t.Fatal("E1-15C requires Linux Docker SIGKILL")
	}
	for _, endpoint := range []string{superadminClient.baseURL, envOrDefault("SESAMEFS_URL_2", "http://sesamefs-node-2:8080"), envOrDefault("SESAMEFS_URL_3", "http://sesamefs-node-3:8080")} {
		if err := e19CheckGCDisabled(newTestClient(endpoint, superadminClient.token)); err != nil {
			t.Fatalf("E1-15C isolation: %v", err)
		}
	}
	for _, leg := range e115cLegs {
		t.Run(leg, func(t *testing.T) {
			t.Cleanup(func() {
				if !t.Failed() && !t.Skipped() {
					e115cObserved[leg] = true
				}
			})
			f := e115bFixture(t)
			fx := f.fx
			// Deferred before GC runs: completes the owned D1 root on any failure.
			defer e115bFinalizeCommitted(t, fx)
			loser := fx.filename
			switch leg {
			case "post-d-pub-terminal":
				e115cCrashAfterD(t, f, "crash-before-queue")
				refs := f.refsExact(t)
				if len(refs) != 1 || !strings.HasPrefix(refs[0], "pub:") || len(w2Repairs(t, fx)) != 0 {
					t.Fatalf("expected one post-D pub: and no repair: %v", refs)
				}
				e115cTerminal(t, f, fx.target, refs)
			case "post-d-repair-terminal":
				e115cDeadRepairTerminal(t, f)
			case "p2-published-with-dead-repair":
				p1, r, owned, sweeper := e115cDeadRepairTerminal(t, f)
				fx.filename = "rematerialized.docx"
				rec := fx.create(t)
				if rec.Code != http.StatusCreated {
					t.Fatalf("productive rematerialization after TERMINAL was rejected: %d %s", rec.Code, rec.Body.String())
				}
				var created struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
					t.Fatal(err)
				}
				p2 := fx.readTarget(t)
				if p2 != fx.target || p2.StorageKey == p1.StorageKey {
					t.Fatalf("P2 must be a new exact life: P1=%+v P2=%+v target=%+v", p1, p2, fx.target)
				}
				if exists, err := newVerificationBlockStore(t, fx.orgID).ObjectExists(t.Context(), p1.StorageKey); err != nil || exists {
					t.Fatalf("K1 reappeared: %t %v", exists, err)
				}
				w2AssertBytes(t, fx)
				head := borrowedFSReadHead(t, fx.database, fx.orgID, fx.repoID)
				entries := e115cHeadEntries(t, fx, head)
				if head == f.head || entries["rematerialized.docx"] != created.ID {
					t.Fatalf("HEAD must publish the rematerialized file: head=%s entries=%v", head, entries)
				}
				if _, ok := entries[loser]; ok {
					t.Fatalf("loser file %s reachable from HEAD", loser)
				}
				refs := f.refsExact(t)
				newFS, repairFS := dbpkg.BlockReferrerForFSObject(fx.repoID, created.ID), dbpkg.BlockReferrerForFSObject(fx.repoID, r.fsID)
				hasNew, hasRepair := false, false
				for _, ref := range refs {
					hasNew = hasNew || ref == newFS
					hasRepair = hasRepair || ref == repairFS
				}
				// fs_id is content-addressed: the legitimate P2 file must share R's
				// fs_id, so its fs: is the very identity R would promote.
				if created.ID != r.fsID || newFS != repairFS || !hasNew || !hasRepair {
					t.Fatalf("expected shared fs_id with a present legitimate fs:: new=%s repair=%s hasNew=%t refs=%v", created.ID, r.fsID, hasNew, refs)
				}
				sweeper.sweep(t, 14*time.Hour, owned)
				if rows := w2Repairs(t, fx); len(rows) != 1 || rows[0].commitID != r.commitID {
					t.Fatalf("dead repair settled unexpectedly: %+v", rows)
				}
				if borrowedFSReadHead(t, fx.database, fx.orgID, fx.repoID) != head || fx.readTarget(t) != p2 {
					t.Fatal("dead repair sweep changed HEAD or P2")
				}
				stillLegit := false
				for _, ref := range f.refsExact(t) {
					stillLegit = stillLegit || ref == newFS
				}
				if !stillLegit {
					t.Fatalf("legitimate %s lost after the dead repair sweep", newFS)
				}
				t.Logf("E1-15C C1: real rematerialization installed P2=(%s,%s) != P1=(%s,%s); K1 absent; HEAD %s publishes only the new file fs=%s, which equals the dead repair's fs_id (required); dead repair R=%s stays UNKNOWN and keeps renewing %s on L (now pinning P2); the legitimate shared fs: survives the sweep", p2.StorageClass, p2.StorageKey, p1.StorageClass, p1.StorageKey, head, created.ID, r.commitID, owned)
			case "dead-repair-blocks-unreferenced-p2":
				p1, r, owned, sweeper := e115cDeadRepairTerminal(t, f)
				fx.filename = "rematerialized.docx"
				func() {
					restore := v2pkg.SetFileFromBlocksPublicationBarriersForTest(fx.repoID, nil, nil, func() { panic("w2-process-death") }, nil)
					defer restore()
					w2Crash(t, fx)
				}()
				p2 := fx.readTarget(t)
				if p2 != fx.target || p2.StorageKey == p1.StorageKey {
					t.Fatalf("P2 must be a new exact life: P1=%+v P2=%+v", p1, p2)
				}
				if rows := w2Repairs(t, fx); len(rows) != 1 || rows[0].commitID != r.commitID {
					t.Fatalf("only the dead repair may exist: %+v", rows)
				}
				// Retire every remaining real reference through its own productive
				// write API and Cassandra TTL: up: by the renewal API, pub: by
				// AddBlockReference on the same referrer.
				refs := f.refsExact(t)
				deadline := time.Now().UTC().Add(3 * time.Second).Truncate(time.Millisecond)
				for _, ref := range refs {
					switch {
					case strings.HasPrefix(ref, "up:"):
						var class string
						var original time.Time
						if err := fx.database.Session().Query(`SELECT storage_class,expires_at FROM gc_provisional_block_refs WHERE org_id=? AND block_id=? AND referrer=?`, fx.orgID, fx.blockID, ref).Consistency(gocql.EachQuorum).Scan(&class, &original); err != nil {
							t.Fatalf("real up: tracker %s: %v", ref, err)
						}
						f.trackers = append(f.trackers, e112ExpiryRow{ref: ref, expires: original.UTC()}, e112ExpiryRow{ref: ref, expires: deadline})
						if err := fx.database.AddProvisionalBlockReferenceWithExpiry(fx.orgID, fx.blockID, ref, fx.repoID, class, deadline); err != nil {
							t.Fatal(err)
						}
					case strings.HasPrefix(ref, "pub:"):
						if f.refTTL(t, ref) <= 0 {
							t.Fatalf("pub: %s not TTL-bound", ref)
						}
						if err := fx.database.AddBlockReference(fx.orgID, fx.blockID, ref, fx.repoID, 2); err != nil {
							t.Fatal(err)
						}
					default:
						t.Fatalf("unexpected saving reference %s in %v", ref, refs)
					}
				}
				for _, ref := range refs {
					f.awaitAbsent(t, ref, deadline)
				}
				if live, err := fx.database.BlockHasReferencesGlobal(fx.orgID, fx.blockID); err != nil || live {
					t.Fatalf("global EQ refs not zero: %t %v", live, err)
				}
				scope, cleaned := f.phase0(t)
				candidates := gcCandidateIdentitiesForTest(t, fx.orgID, fx.blockID)
				f.candidates = append(f.candidates, candidates...)
				if len(candidates) != 1 || candidates[0].Target != p2 {
					t.Fatalf("Phase 0 natural P2 candidate: listed=%d cleaned=%d candidates=%+v", scope.provisional, cleaned, candidates)
				}
				phase1 := f.scope()
				if enqueued, err := gcpkg.NewScanner(phase1, gcpkg.NewQueue(phase1), &gcpkg.Stats{}, config.GCConfig{}).ScanOrphanedBlocksOnce(context.Background()); err != nil || enqueued != 1 {
					t.Fatalf("Phase 1 enqueue: %d %v", enqueued, err)
				}
				// Direct productive probe first: zero real refs, the dead repair alone
				// must classify the block RepairGuardOnly.
				w2AssertGuardOnly(t, fx)
				store := gcpkg.NewCassandraStore(fx.database)
				ownedQueue := &w2ClosureOwnedQueue{GCStore: store, org: fx.orgUUID, block: fx.blockID, identity: candidates[0]}
				n, workerErr := w2Worker(t, ownedQueue, p2.StorageClass).ProcessOrgOnce(t.Context(), fx.orgUUID)
				if !ownedQueue.visited || n != 0 || workerErr != nil || len(ownedQueue.liveness) == 0 {
					t.Fatalf("worker on unreferenced P2: visited=%t n=%d err=%v liveness=%v", ownedQueue.visited, n, workerErr, ownedQueue.liveness)
				}
				// The worker's own destructive proof must have been the dead-repair veto.
				for i, live := range ownedQueue.liveness {
					if ownedQueue.errs[i] != nil || live != dbpkg.BlockPublicationRepairGuardOnly {
						t.Fatalf("worker liveness answer %d = %v err=%v; want RepairGuardOnly", i, live, ownedQueue.errs[i])
					}
				}
				if _, found, err := store.GetBlockGCCandidateExact(fx.orgUUID, fx.blockID, candidates[0]); err != nil || !found {
					t.Fatalf("P2 candidate lost: %t %v", found, err)
				}
				iter := fx.database.Session().Query(`SELECT storage_key, phase FROM gc_block_delete_lifecycles WHERE org_id=? AND block_id=?`, fx.orgID, fx.blockID).Consistency(gocql.EachQuorum).Iter()
				var lifecycleKey, lifecyclePhase string
				for iter.Scan(&lifecycleKey, &lifecyclePhase) {
					if lifecycleKey == p2.StorageKey {
						t.Fatalf("D(P2) started despite the dead repair: phase=%s", lifecyclePhase)
					}
				}
				if err := iter.Close(); err != nil {
					t.Fatal(err)
				}
				if fx.readTarget(t) != p2 {
					t.Fatal("P2 canonical changed")
				}
				w2AssertBytes(t, fx)
				sweeper.sweep(t, 14*time.Hour, owned)
				if live, err := fx.database.BlockHasReferencesGlobal(fx.orgID, fx.blockID); err != nil || !live {
					t.Fatalf("dead repair renewal did not re-pin L: %t %v", live, err)
				}
				t.Logf("E1-15C C2: every real reference of unreferenced P2=(%s,%s) retired by TTL (global refs=0); natural candidate; productive worker's own liveness proof returned RepairGuardOnly (%d answer(s), no error) and vetoed D(P2) (n=0, candidate retained, no P2 lifecycle, K2 present) only because dead repair R=%s is pending for L; a further sweep re-pinned L with %s. Every later life of L in this org is uncollectable while R exists", p2.StorageClass, p2.StorageKey, len(ownedQueue.liveness), r.commitID, owned)
			}
		})
	}
}

func e115cRunIsolated(t *testing.T, endpoint string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	run := "^TestPostDPhysicalLifeDisposition$"
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
	cmd.Env = append(cmd.Env, e115cEvidenceEnv+"=1", "SESAMEFS_E115C_CHILD=1", "CASSANDRA_KEYSPACE=sesamefs_e19", "SESAMEFS_URL="+endpoint, "SESAMEFS_URL_2="+endpoint, "SESAMEFS_URL_3="+endpoint)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("isolated E1-15C: %v", err)
	}
	for _, leg := range e115cLegs {
		e115cObserved[leg] = true
	}
}

func TestPostDPhysicalLifeDispositionCompleteness(t *testing.T) {
	if len(e115cMissing(nil)) != 4 {
		t.Fatal("four required legs")
	}
	all := map[string]bool{}
	for _, leg := range e115cLegs {
		if all[leg] {
			t.Fatal("duplicate leg")
		}
		all[leg] = true
	}
	if len(e115cMissing(all)) != 0 {
		t.Fatal("complete rejected")
	}
	for _, leg := range e115cLegs {
		delete(all, leg)
		if missing := e115cMissing(all); len(missing) != 1 || missing[0] != leg {
			t.Fatalf("omission %s: %v", leg, missing)
		}
		all[leg] = true
	}
}
