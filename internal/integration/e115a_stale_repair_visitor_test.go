//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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

const e115aEvidenceEnv = "SESAMEFS_REQUIRE_E115A_STALE_VISITOR_EVIDENCE"

var e115aLegs = []string{"retained-control", "clear-before-recheck", "clear-after-recheck", "fresh-sweep-after-clear"}
var e115aObserved = map[string]bool{}

func e115aMissing(seen map[string]bool) []string {
	var out []string
	for _, leg := range e115aLegs {
		if !seen[leg] {
			out = append(out, leg)
		}
	}
	return out
}

// The real Office writer is held after exact-P authority and before HEAD on its
// first attempt. On release it either wins, or every attempt (CreateFile retries
// HEAD conflicts) meets a real competitor HEAD and runs real loser cleanup.
type e115aWriter struct {
	fx       *w2CreateFileFixture
	paused   chan struct{}
	release  chan bool
	done     chan *httptest.ResponseRecorder
	mu       sync.Mutex
	attempts int
	losers   []string
	failures []string
}

func e115aStartWriter(t *testing.T, fx *w2CreateFileFixture) *e115aWriter {
	t.Helper()
	w := &e115aWriter{fx: fx, paused: make(chan struct{}), release: make(chan bool, 1), done: make(chan *httptest.ResponseRecorder, 1)}
	competing, compete := false, false
	t.Cleanup(v2pkg.SetW2PublicationAfterAuthorityForTest(fx.repoID, func() {
		if competing {
			return
		}
		w.mu.Lock()
		w.attempts++
		attempt := w.attempts
		w.mu.Unlock()
		if attempt == 1 {
			close(w.paused)
			compete = <-w.release
		}
		if !compete {
			return
		}
		competing = true
		name := fx.filename
		fx.filename = fmt.Sprintf("competitor-%d.txt", attempt)
		rec := fx.create(t)
		fx.filename = name
		competing = false
		if rec.Code != http.StatusCreated {
			w.mu.Lock()
			w.failures = append(w.failures, fmt.Sprintf("competitor %d: %d %s", attempt, rec.Code, rec.Body.String()))
			w.mu.Unlock()
		}
	}))
	t.Cleanup(v2pkg.SetKnownLoserBeforeCleanupForIntegration(fx.database, fx.repoID, func(commit string) {
		w.mu.Lock()
		w.losers = append(w.losers, commit)
		w.mu.Unlock()
	}))
	go func() { w.done <- fx.create(t) }()
	t.Cleanup(func() {
		select {
		case w.release <- false:
		default:
		}
		select {
		case <-w.done:
		case <-time.After(30 * time.Second):
			t.Error("E1-15A writer did not finish")
		}
	})
	select {
	case <-w.paused:
	case rec := <-w.done:
		t.Fatalf("writer finished before HEAD barrier: %d %s", rec.Code, rec.Body.String())
	case <-time.After(30 * time.Second):
		t.Fatal("writer did not reach HEAD barrier")
	}
	return w
}

func (w *e115aWriter) finish(t *testing.T, compete bool) *httptest.ResponseRecorder {
	t.Helper()
	w.release <- compete
	select {
	case rec := <-w.done:
		w.done <- rec
		w.mu.Lock()
		defer w.mu.Unlock()
		if len(w.failures) != 0 {
			t.Fatalf("real competitor failed: %v", w.failures)
		}
		return rec
	case <-time.After(60 * time.Second):
		t.Fatal("writer did not finish")
	}
	return nil
}

// Records every reference write the visitor's own session sends for this block.
type e115aWrites struct {
	mu                  sync.Mutex
	org, block          string
	inserts, deletes    []string
}

func (o *e115aWrites) ObserveQuery(_ context.Context, q gocql.ObservedQuery) {
	statement := strings.ToLower(strings.Join(strings.Fields(q.Statement), " "))
	if len(q.Values) < 3 || q.Values[0] != o.org || q.Values[1] != o.block || q.Err != nil {
		return
	}
	referrer, _ := q.Values[2].(string)
	o.mu.Lock()
	defer o.mu.Unlock()
	switch {
	case strings.HasPrefix(statement, "insert into block_references "):
		o.inserts = append(o.inserts, referrer)
	case strings.HasPrefix(statement, "delete from block_references "):
		o.deletes = append(o.deletes, referrer)
	}
}
func (o *e115aWrites) snapshot() ([]string, []string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.inserts...), append([]string(nil), o.deletes...)
}

// The productive sweep on its own observed session. pauseAt is "classify"
// (W-pre: before both durable re-checks), "renew" (W-post: after the last
// re-check, before the renewal write) or "" (no pause).
type e115aVisitor struct {
	writes   *e115aWrites
	paused   chan struct{}
	resume   chan struct{}
	done     chan error
	mu       sync.Mutex
	visits   int
	outcome  string
	renewals int
	errs     []string
}

func e115aStartVisitor(t *testing.T, fx *w2CreateFileFixture, commit, fs, pauseAt string) *e115aVisitor {
	t.Helper()
	v := &e115aVisitor{writes: &e115aWrites{org: fx.orgID, block: fx.blockID}, paused: make(chan struct{}), resume: make(chan struct{}), done: make(chan error, 1)}
	database := w2EvidenceSession(t, splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042")[0], v.writes)
	var once sync.Once
	pause := func() {
		once.Do(func() {
			close(v.paused)
			<-v.resume
		})
	}
	identity := func(c, f string) {
		if c != commit || f != fs {
			v.mu.Lock()
			v.errs = append(v.errs, fmt.Sprintf("unexpected repair identity %s/%s", c, f))
			v.mu.Unlock()
		}
	}
	t.Cleanup(v2pkg.SetRepairBeforeVisitForIntegration(database, fx.repoID, func(c, f string) {
		identity(c, f)
		v.mu.Lock()
		v.visits++
		v.mu.Unlock()
	}))
	t.Cleanup(v2pkg.SetRepairAfterClassifyForIntegration(database, fx.repoID, func(outcome string, err error) {
		v.mu.Lock()
		v.outcome = outcome
		if err != nil {
			v.errs = append(v.errs, "classifier: "+err.Error())
		}
		v.mu.Unlock()
		if pauseAt == "classify" {
			pause()
		}
	}))
	t.Cleanup(v2pkg.SetRepairBeforeRenewForIntegration(database, fx.repoID, func(c, f string) {
		identity(c, f)
		v.mu.Lock()
		v.renewals++
		v.mu.Unlock()
		if pauseAt == "renew" {
			pause()
		}
	}))
	go func() { v.done <- v2pkg.RunPublishedBlockReferenceRepairSweepAtForIntegration(database, time.Now().Add(2*time.Hour)) }()
	t.Cleanup(func() {
		once.Do(func() {})
		select {
		case <-v.resume:
		default:
			close(v.resume)
		}
		select {
		case <-v.done:
		case <-time.After(30 * time.Second):
			t.Error("E1-15A visitor did not finish")
		}
	})
	if pauseAt != "" {
		select {
		case <-v.paused:
		case err := <-v.done:
			t.Fatalf("visitor finished before its %s pause: %v", pauseAt, err)
		case <-time.After(30 * time.Second):
			t.Fatalf("visitor did not reach its %s pause", pauseAt)
		}
	}
	return v
}

func (v *e115aVisitor) finish(t *testing.T) error {
	t.Helper()
	select {
	case <-v.resume:
	default:
		close(v.resume)
	}
	select {
	case err := <-v.done:
		v.done <- err
		v.mu.Lock()
		defer v.mu.Unlock()
		if len(v.errs) != 0 {
			t.Fatalf("visitor observation errors: %v", v.errs)
		}
		return err
	case <-time.After(30 * time.Second):
		t.Fatal("visitor did not finish")
	}
	return nil
}

func e115aFixture(t *testing.T) (*e114Fixture, w2Repair, *e115aWriter) {
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
	w := e115aStartWriter(t, fx)
	rows := w2Repairs(t, fx)
	if len(rows) != 1 || len(rows[0].blocks) != 1 || rows[0].blocks[0] != fx.blockID {
		t.Fatalf("real writer repair missing: %+v", rows)
	}
	r := rows[0]
	var ttl *int
	if err := fx.database.Session().Query(`SELECT TTL(created_at) FROM published_block_reference_repairs WHERE bucket=? AND org_id=? AND repo_id=? AND commit_id=? AND fs_id=?`, r.bucket, fx.orgID, fx.repoID, r.commitID, r.fsID).Consistency(gocql.EachQuorum).Scan(&ttl); err != nil || ttl != nil {
		t.Fatalf("repair not durable TTL=%v err=%v", ttl, err)
	}
	fx.assertHeadUnchanged(t)
	// E1-13 eligibility control: the real lease stays ahead of the shared daemon;
	// only the dedicated sweep's eligibility time is advanced.
	if err := fx.database.Session().Query(`UPDATE published_block_reference_repairs SET created_at=?,lease_expires_at=? WHERE bucket=? AND org_id=? AND repo_id=? AND commit_id=? AND fs_id=?`, time.Now().Add(-time.Hour), time.Now().Add(time.Hour), r.bucket, fx.orgID, fx.repoID, r.commitID, r.fsID).Consistency(gocql.LocalQuorum).Exec(); err != nil {
		t.Fatal(err)
	}
	return f, r, w
}

func e115aRepairOwned(fx *w2CreateFileFixture, r w2Repair) string {
	return dbpkg.BlockReferrerForPublishAttempt(fx.repoID + ":" + r.commitID + ":" + r.fsID)
}

// Definitive real loss on every retry: request fails, every attempt cleaned
// its own pub:/repair, nothing promoted, the loser file is not in HEAD.
func e115aAssertCancelled(t *testing.T, f *e114Fixture, w *e115aWriter, rec *httptest.ResponseRecorder) {
	t.Helper()
	fx := f.fx
	if rec.Code == http.StatusCreated {
		t.Fatalf("writer won despite a competitor at every attempt: %s", rec.Body.String())
	}
	w.mu.Lock()
	attempts, losers := w.attempts, append([]string(nil), w.losers...)
	w.mu.Unlock()
	if attempts < 2 || len(losers) != attempts {
		t.Fatalf("expected a real loser cleanup per attempt: attempts=%d losers=%v", attempts, losers)
	}
	if rows := w2Repairs(t, fx); len(rows) != 0 {
		t.Fatalf("loser cleanup left repairs: %+v", rows)
	}
	refs, err := fx.database.ListBlockReferrers(fx.orgID, fx.blockID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs {
		if !strings.HasPrefix(ref, "up:") {
			t.Fatalf("cancelled attempts left %s in %v", ref, refs)
		}
	}
	if fx.hasOwnFSReferrer(t) {
		t.Fatal("loser promoted fs:")
	}
	f.head = borrowedFSReadHead(t, fx.database, fx.orgID, fx.repoID)
	for _, loser := range losers {
		if loser == f.head {
			t.Fatal("loser commit is HEAD")
		}
	}
	var root, entries string
	if err := fx.database.Session().Query(`SELECT root_fs_id FROM commits WHERE library_id=? AND commit_id=?`, fx.repoID, f.head).Consistency(gocql.EachQuorum).Scan(&root); err != nil {
		t.Fatal(err)
	}
	if err := fx.database.Session().Query(`SELECT dir_entries FROM fs_objects WHERE library_id=? AND fs_id=?`, fx.repoID, root).Consistency(gocql.EachQuorum).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	var dirents []v2pkg.FSEntry
	if err := json.Unmarshal([]byte(entries), &dirents); err != nil {
		t.Fatal(err)
	}
	for _, d := range dirents {
		if d.Name == fx.filename {
			t.Fatalf("loser file reachable from HEAD %s", f.head)
		}
	}
	if fx.readTarget(t) != fx.target {
		t.Fatal("exact P1 changed")
	}
	w2AssertBytes(t, fx)
	t.Logf("E1-15A cancellation: status=%d attempts=%d real loser cleanups=%d; repairs/pub:/fs: absent; %d up: remain; loser not in HEAD %s", rec.Code, attempts, len(losers), len(refs), f.head)
}

// Natural discovery only: every real up: is moved by the productive renewal API
// and retired by Cassandra TTL; owned-scope Phase 0/1 and the productive worker
// then reach exact COMMITTED D(P1). The harness never creates the candidate.
func e115aCommitD(t *testing.T, f *e114Fixture) {
	t.Helper()
	fx := f.fx
	refs, err := fx.database.ListBlockReferrers(fx.orgID, fx.blockID)
	if err != nil || len(refs) == 0 {
		t.Fatalf("no real up: to expire: %v %v", refs, err)
	}
	for _, ref := range refs {
		if !strings.HasPrefix(ref, "up:") {
			t.Fatalf("E1-15A natural D expects only real up: refs: %v", refs)
		}
	}
	e115aNaturalD(t, f, e115aRetireByTTL(t, f, refs))
}

// e115aRetireByTTL retires the given real references through their own
// productive write API and Cassandra TTL (up: by the renewal API, pub: by
// AddBlockReference on the same referrer) and returns how many were up:.
func e115aRetireByTTL(t *testing.T, f *e114Fixture, refs []string) int {
	t.Helper()
	fx := f.fx
	deadline := time.Now().UTC().Add(3 * time.Second).Truncate(time.Millisecond)
	ups := 0
	for _, ref := range refs {
		switch {
		case strings.HasPrefix(ref, "up:"):
			ups++
			var class string
			var original time.Time
			if err := fx.database.Session().Query(`SELECT storage_class,expires_at FROM gc_provisional_block_refs WHERE org_id=? AND block_id=? AND referrer=?`, fx.orgID, fx.blockID, ref).Consistency(gocql.EachQuorum).Scan(&class, &original); err != nil {
				t.Fatalf("real up: tracker %s missing: %v", ref, err)
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
	return ups
}

// e115aNaturalD: with every real reference retired, productive Phase 0/1 and
// the worker must reach exact COMMITTED D(P) for fx.target.
func e115aNaturalD(t *testing.T, f *e114Fixture, ups int) {
	t.Helper()
	fx := f.fx
	if live, err := fx.database.BlockHasReferencesGlobal(fx.orgID, fx.blockID); err != nil || live {
		t.Fatalf("global EQ refs not zero: %t %v", live, err)
	}
	scope, cleaned := f.phase0(t)
	candidates := gcCandidateIdentitiesForTest(t, fx.orgID, fx.blockID)
	f.candidates = append(f.candidates, candidates...)
	if cleaned != ups || len(candidates) != 1 || candidates[0].Target != fx.target {
		t.Fatalf("Phase 0 natural candidate: listed=%d cleaned=%d/%d candidates=%+v", scope.provisional, cleaned, ups, candidates)
	}
	phase1 := f.scope()
	enqueued, err := gcpkg.NewScanner(phase1, gcpkg.NewQueue(phase1), &gcpkg.Stats{}, config.GCConfig{}).ScanOrphanedBlocksOnce(context.Background())
	if err != nil || enqueued != 1 {
		t.Fatalf("Phase 1 enqueue: %d %v", enqueued, err)
	}
	store := gcpkg.NewCassandraStore(fx.database)
	owned := &w2ClosureOwnedQueue{GCStore: store, org: fx.orgUUID, block: fx.blockID, identity: candidates[0]}
	n, workerErr := w2Worker(t, owned, fx.target.StorageClass).ProcessOrgOnce(t.Context(), fx.orgUUID)
	var phase, class, key, claim string
	var claimedAt time.Time
	scanErr := fx.database.Session().Query(`SELECT phase,storage_class,storage_key,claim_id,claimed_at FROM gc_block_delete_lifecycles WHERE org_id=? AND block_id=?`, fx.orgID, fx.blockID).Consistency(gocql.EachQuorum).Scan(&phase, &class, &key, &claim, &claimedAt)
	authority := gcpkg.BlockDeleteAuthority{Target: gcpkg.BlockDeleteTarget{StorageClass: class, StorageKey: key}, ClaimID: claim, ClaimedAt: claimedAt}
	orphan, orphanFound, orphanErr := store.GetS3OrphanExact(fx.orgUUID, fx.blockID, authority)
	_, rootFound, rootErr := store.GetS3OrphanRecoveryRootExact(fx.orgUUID, fx.blockID, authority)
	exists, existsErr := store.BlockExists(fx.orgUUID, fx.blockID)
	if !owned.visited || scanErr != nil || phase != gcpkg.BlockDeleteLifecyclePhasePublished || class != fx.target.StorageClass || key != fx.target.StorageKey || orphanErr != nil || !orphanFound || orphan.RecoveryState != gcpkg.S3OrphanRecoveryStateCommitted || rootErr != nil || !rootFound || existsErr != nil || exists {
		t.Fatalf("productive GC did not reach exact COMMITTED D(P1): n=%d err=%v visited=%t phase=%s scan=%v orphan=%t/%v root=%t/%v canonical=%t/%v", n, workerErr, owned.visited, phase, scanErr, orphanFound, orphanErr, rootFound, rootErr, exists, existsErr)
	}
	t.Logf("E1-15A natural D: %d up: retired by TTL; Phase 0 candidate at exact P1; worker n=%d reached exact COMMITTED D(P1)=(%s,%s) claim=%s; canonical retired", ups, n, class, key, claim)
}

func e115aVerifyNoRoot(t *testing.T, database *dbpkg.DB, fx *w2CreateFileFixture) {
	t.Helper()
	store := gcpkg.NewCassandraStore(database)
	for bucket := 0; bucket < dbpkg.GCDiscoveryBucketCount; bucket++ {
		var pageState []byte
		for {
			page, err := store.ListS3OrphanRecoveryRoots(bucket, pageState, 1000)
			if err != nil {
				t.Errorf("E1-15A teardown roots read: %v", err)
				return
			}
			for _, root := range page.Roots {
				if root.OrgID == fx.orgUUID && root.BlockID == fx.blockID {
					t.Errorf("E1-15A teardown recovery root remains: %+v", root)
					return
				}
			}
			pageState = page.PageState
			if len(pageState) == 0 {
				break
			}
		}
	}
	t.Logf("E1-15A teardown verified org=%s block=%s: no recovery root", fx.orgID, fx.blockID)
}

func TestStaleRepairVisitorCancellation(t *testing.T) {
	if endpoint := os.Getenv("SESAMEFS_E115A_ISOLATED_URL"); endpoint != "" && os.Getenv("SESAMEFS_E115A_CHILD") != "1" {
		e115aRunIsolated(t, endpoint)
		return
	}
	requireCassandra(t)
	if runtime.GOOS != "linux" || os.Getenv("SESAMEFS_TEST_IN_CONTAINER") != "1" {
		t.Fatal("E1-15A requires Linux Docker")
	}
	for _, endpoint := range []string{superadminClient.baseURL, envOrDefault("SESAMEFS_URL_2", "http://sesamefs-node-2:8080"), envOrDefault("SESAMEFS_URL_3", "http://sesamefs-node-3:8080")} {
		if err := e19CheckGCDisabled(newTestClient(endpoint, superadminClient.token)); err != nil {
			t.Fatalf("E1-15A isolation: %v", err)
		}
	}
	for _, leg := range e115aLegs {
		t.Run(leg, func(t *testing.T) {
			t.Cleanup(func() {
				if !t.Failed() && !t.Skipped() {
					e115aObserved[leg] = true
				}
			})
			f, r, w := e115aFixture(t)
			fx := f.fx
			owned := e115aRepairOwned(fx, r)
			switch leg {
			case "retained-control":
				v := e115aStartVisitor(t, fx, r.commitID, r.fsID, "classify")
				err := v.finish(t)
				if err == nil || !strings.Contains(err.Error(), "unknown; retain queued repair") {
					t.Fatalf("native UNKNOWN sweep result: %v", err)
				}
				inserts, _ := v.writes.snapshot()
				refs, _ := fx.database.ListBlockReferrers(fx.orgID, fx.blockID)
				renewed := false
				for _, ref := range refs {
					renewed = renewed || ref == owned
				}
				if v.outcome != "unknown" || v.renewals != 1 || !renewed || len(w2Repairs(t, fx)) != 1 {
					t.Fatalf("retained repair must renew: outcome=%s renewals=%d inserts=%v refs=%v", v.outcome, v.renewals, inserts, refs)
				}
				if rec := w.finish(t, false); rec.Code != http.StatusCreated {
					t.Fatalf("uncontested writer: %d %s", rec.Code, rec.Body.String())
				}
				if !fx.hasOwnFSReferrer(t) || len(w2Repairs(t, fx)) != 0 {
					t.Fatal("winning writer must promote fs: and settle its repair")
				}
				t.Logf("E1-15A control: R present at resume; native UNKNOWN renewed %s; writer then won and promoted fs:", owned)
			case "fresh-sweep-after-clear":
				e115aAssertCancelled(t, f, w, w.finish(t, true))
				v := e115aStartVisitor(t, fx, r.commitID, r.fsID, "")
				if err := v.finish(t); err != nil {
					t.Fatalf("fresh sweep: %v", err)
				}
				inserts, deletes := v.writes.snapshot()
				if v.visits != 0 || v.renewals != 0 || len(inserts) != 0 || len(deletes) != 0 {
					t.Fatalf("fresh sweep acted on a cancelled repair: visits=%d renewals=%d inserts=%v deletes=%v", v.visits, v.renewals, inserts, deletes)
				}
				t.Logf("E1-15A fresh sweep after durable cancellation: 0 visits, 0 reference writes")
			default:
				pauseAt := map[string]string{"clear-before-recheck": "classify", "clear-after-recheck": "renew"}[leg]
				v := e115aStartVisitor(t, fx, r.commitID, r.fsID, pauseAt)
				if v.outcome != "unknown" {
					t.Fatalf("visitor must have classified natively UNKNOWN: %q", v.outcome)
				}
				e115aAssertCancelled(t, f, w, w.finish(t, true))
				e115aCommitD(t, f)
				err := v.finish(t)
				inserts, deletes := v.writes.snapshot()
				refs, refsErr := fx.database.ListBlockReferrers(fx.orgID, fx.blockID)
				live, liveErr := fx.database.BlockHasReferencesGlobal(fx.orgID, fx.blockID)
				t.Logf("E1-15A stale visitor resumed after COMMITTED D: sweep=%v renew-entries=%d inserts=%v deletes=%v refs=%v", err, v.renewals, inserts, deletes, refs)
				if refsErr != nil || liveErr != nil || len(refs) != 0 || live {
					t.Errorf("E1-15A RED (%s): stale visitor left a durable reference after exact COMMITTED D(P1): refs=%v live=%t errs=%v/%v", leg, refs, live, refsErr, liveErr)
				} else if pauseAt == "classify" && (v.renewals != 0 || len(inserts) != 0) {
					t.Errorf("E1-15A pre-check bypassed (%s): renewal entered=%d writes=%v after COMMITTED D(P1)", leg, v.renewals, inserts)
				} else if pauseAt == "renew" && (len(inserts) != 1 || inserts[0] != owned || len(deletes) != 1 || deletes[0] != owned) {
					t.Errorf("E1-15A (%s): expected exactly one transient %s write withdrawn after global absence: inserts=%v deletes=%v", leg, owned, inserts, deletes)
				} else {
					t.Logf("E1-15A GREEN (%s): no durable post-D reference (transient writes=%v withdrawn=%v); no fs:/HEAD change", leg, inserts, deletes)
				}
				if fx.hasOwnFSReferrer(t) || borrowedFSReadHead(t, fx.database, fx.orgID, fx.repoID) != f.head || len(w2Repairs(t, fx)) != 0 {
					t.Error("stale visitor changed fs:/HEAD/repair after D")
				}
				// Complete the owned COMMITTED root to TERMINAL before teardown.
				w2AssertCommittedContinuation(t, gcpkg.NewCassandraStore(fx.database), fx.orgUUID, fx.blockID, fx.target.StorageClass, fx.target.StorageKey, newVerificationBlockStore(t, fx.orgID))
			}
		})
	}
}

func e115aRunIsolated(t *testing.T, endpoint string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	run := "^TestStaleRepairVisitorCancellation$"
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
	cmd.Env = append(cmd.Env, e115aEvidenceEnv+"=1", "SESAMEFS_E115A_CHILD=1", "CASSANDRA_KEYSPACE=sesamefs_e19", "SESAMEFS_URL="+endpoint, "SESAMEFS_URL_2="+endpoint, "SESAMEFS_URL_3="+endpoint)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("isolated E1-15A: %v", err)
	}
	for _, leg := range e115aLegs {
		e115aObserved[leg] = true
	}
}

func TestStaleRepairVisitorCancellationCompleteness(t *testing.T) {
	if len(e115aMissing(nil)) != 4 {
		t.Fatal("four required legs")
	}
	all := map[string]bool{}
	for _, leg := range e115aLegs {
		if all[leg] {
			t.Fatal("duplicate leg")
		}
		all[leg] = true
	}
	if len(e115aMissing(all)) != 0 {
		t.Fatal("complete rejected")
	}
	for _, leg := range e115aLegs {
		delete(all, leg)
		if missing := e115aMissing(all); len(missing) != 1 || missing[0] != leg {
			t.Fatalf("omission %s: %v", leg, missing)
		}
		all[leg] = true
	}
}
