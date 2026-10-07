//go:build integration

package integration

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	v2pkg "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	"github.com/Sesame-Disk/sesamefs/internal/config"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

const e114EvidenceEnv = "SESAMEFS_REQUIRE_E114_PUB_ZERO_REF_EVIDENCE"

var e114Legs = []string{"pub-live-control", "up-expires-pub-remains", "final-pub-expiry-no-repair", "final-pub-expiry-with-repair", "repair-renews-after-zero-ref"}
var e114Observed = map[string]bool{}

func e114Missing(seen map[string]bool) []string {
	var out []string
	for _, leg := range e114Legs {
		if !seen[leg] {
			out = append(out, leg)
		}
	}
	return out
}

// Discovery listings are narrowed to the owned block and day cursors stay in
// memory. Classification, liveness reads, candidate persistence and projection
// deletion are the unmodified production scanner body against Cassandra.
type e114ScopedScan struct {
	gcpkg.GCStore
	org                   uuid.UUID
	block                 string
	cursors               map[string]string
	provisional, observed int
}

func (s *e114ScopedScan) ListProvisionalBlockRefExpiriesByDay(day time.Time, bucket int) ([]gcpkg.ProvisionalBlockRefExpiryInfo, error) {
	rows, err := s.GCStore.ListProvisionalBlockRefExpiriesByDay(day, bucket)
	if err != nil {
		return nil, err
	}
	var own []gcpkg.ProvisionalBlockRefExpiryInfo
	for _, row := range rows {
		if row.OrgID == s.org && row.BlockID == s.block {
			own = append(own, row)
		}
	}
	s.provisional += len(own)
	return own, nil
}
func (s *e114ScopedScan) ListBlockGCCandidatesByDay(day time.Time, bucket int) ([]gcpkg.BlockGCCandidateInfo, error) {
	rows, err := s.GCStore.ListBlockGCCandidatesByDay(day, bucket)
	if err != nil {
		return nil, err
	}
	var own []gcpkg.BlockGCCandidateInfo
	for _, row := range rows {
		if row.OrgID == s.org && row.BlockID == s.block {
			own = append(own, row)
		}
	}
	s.observed += len(own)
	return own, nil
}
func (s *e114ScopedScan) LoadGCStats(key string) (string, error) {
	if value, ok := s.cursors[key]; ok {
		return value, nil
	}
	return "", gocql.ErrNotFound
}
func (s *e114ScopedScan) SaveGCStats(key, value string) error {
	s.cursors[key] = value
	return nil
}

type e114Fixture struct {
	fx         *w2CreateFileFixture
	repair     *e113Fixture
	pub, up    string
	head       string
	trackers   []e112ExpiryRow
	candidates []gcpkg.BlockGCCandidateIdentity
}

func (f *e114Fixture) scope() *e114ScopedScan {
	return &e114ScopedScan{GCStore: gcpkg.NewCassandraStore(f.fx.database), org: f.fx.orgUUID, block: f.fx.blockID, cursors: map[string]string{}}
}

// Teardown is registered before the fixture so it runs after metadata, expiry
// and owner cleanup; the candidate/queue check covers the causal mutation too.
func e114Teardown(t *testing.T, get func() *e114Fixture) {
	t.Helper()
	verification := shareProjectionDBForTest(t)
	t.Cleanup(func() {
		f := get()
		if f == nil {
			return
		}
		clean := true
		var count int
		if err := verification.Session().Query(`SELECT count(*) FROM gc_block_candidates WHERE org_id=? AND block_id=?`, f.fx.orgID, f.fx.blockID).Consistency(gocql.EachQuorum).Scan(&count); err != nil || count != 0 {
			clean = false
			t.Errorf("E1-14 teardown candidates rows=%d err=%v", count, err)
		}
		for _, c := range f.candidates {
			if err := verification.Session().Query(`SELECT count(*) FROM gc_block_candidates_by_day WHERE candidate_day=? AND bucket=? AND candidate_at=? AND org_id=? AND block_id=? AND storage_class=? AND storage_key=?`, dbpkg.GCProjectionUTCDate(c.CandidateAt), dbpkg.GCDiscoveryBucket(f.fx.orgID, f.fx.blockID), c.CandidateAt, f.fx.orgID, f.fx.blockID, c.Target.StorageClass, c.Target.StorageKey).Consistency(gocql.EachQuorum).Scan(&count); err != nil || count != 0 {
				clean = false
				t.Errorf("E1-14 teardown candidate projection rows=%d err=%v", count, err)
			}
		}
		if queued, err := e114Queued(verification, f); err != nil || queued != 0 {
			clean = false
			t.Errorf("E1-14 teardown queue rows=%d err=%v", queued, err)
		}
		// Trackers moved by a renewal leave no canonical row for their old
		// deadline; their exact by-day coordinates are verified explicitly.
		for _, row := range f.trackers {
			if err := verification.Session().Query(`SELECT count(*) FROM gc_provisional_block_refs_by_day WHERE expiry_day=? AND bucket=? AND expires_at=? AND org_id=? AND block_id=? AND referrer=?`, dbpkg.GCProjectionUTCDate(row.expires), dbpkg.GCDiscoveryBucket(f.fx.orgID, f.fx.blockID, row.ref), row.expires, f.fx.orgID, f.fx.blockID, row.ref).Consistency(gocql.EachQuorum).Scan(&count); err != nil || count != 0 {
				clean = false
				t.Errorf("E1-14 teardown moved projection rows=%d err=%v", count, err)
			}
		}
		if clean {
			t.Logf("E1-14 teardown verified org=%s block=%s: candidates/projections/queue absent; %d moved tracker projections absent", f.fx.orgID, f.fx.blockID, len(f.trackers))
		}
	})
}

// The production pending probe requires an exact P identity, which a block
// without any candidate cannot name. The org is exclusively owned, so every
// gc_queue row in it (32 default buckets) and every pending block row counts.
func e114Queued(database *dbpkg.DB, f *e114Fixture) (int, error) {
	total := 0
	for bucket := 0; bucket < 32; bucket++ {
		var count int
		if err := database.Session().Query(`SELECT count(*) FROM gc_queue WHERE org_id=? AND bucket=?`, f.fx.orgID, bucket).Consistency(gocql.EachQuorum).Scan(&count); err != nil {
			return total, err
		}
		total += count
	}
	var count int
	if err := database.Session().Query(`SELECT count(*) FROM gc_pending_items WHERE org_id=? AND bucket=? AND item_type=? AND library_id=? AND item_id=?`, f.fx.orgID, gcpkg.PendingItemBucket(f.fx.orgUUID, uuid.Nil, gcpkg.ItemBlock, f.fx.blockID), string(gcpkg.ItemBlock), uuid.Nil.String(), f.fx.blockID).Consistency(gocql.EachQuorum).Scan(&count); err != nil {
		return total, err
	}
	return total + count, nil
}

// The real writer stops after durable pub: staging and before durable repair
// queueing, via the existing CreateFile seam. No reference row is fabricated.
func e114NoRepairFixture(t *testing.T) *e114Fixture {
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
		}
	})
	fx = w2RepairFixture(t)
	t.Cleanup(func() { expiry = e112CleanupExpiry(t, fx) })
	t.Cleanup(func() { owners = e113CleanupOwners(t, fx) })
	func() {
		restore := v2pkg.SetFileFromBlocksPublicationBarriersForTest(fx.repoID, nil, nil, func() { panic("w2-process-death") }, nil)
		defer restore()
		w2Crash(t, fx)
	}()
	if rows := w2Repairs(t, fx); len(rows) != 0 {
		t.Fatalf("writer stopped before repair queueing left a repair: %+v", rows)
	}
	fx.assertHeadUnchanged(t)
	f = &e114Fixture{fx: fx, head: borrowedFSReadHead(t, fx.database, fx.orgID, fx.repoID)}
	f.references(t)
	return f
}

// Existing E1-13 shape: durable repair, writer stopped before HEAD, then a real
// competitor HEAD makes the attempt natively UNKNOWN. The real lease is kept
// ahead of the shared e19 repair daemon.
func e114RepairFixture(t *testing.T) *e114Fixture {
	t.Helper()
	var f *e114Fixture
	e114Teardown(t, func() *e114Fixture { return f })
	r := e113RealFixture(t)
	r.eligible(t)
	f = &e114Fixture{fx: r.fx, repair: r, head: r.head}
	f.references(t)
	if f.pub != dbpkg.BlockReferrerForPublishAttempt(r.repair.commitID) {
		t.Fatalf("writer pub: is not the repaired attempt: %s", f.pub)
	}
	// Visitor progress is read once refs reach zero (unvisited also requires that).
	r.retained(t)
	return f
}

func (f *e114Fixture) references(t *testing.T) {
	t.Helper()
	refs, err := f.fx.database.ListBlockReferrers(f.fx.orgID, f.fx.blockID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs {
		switch {
		case strings.HasPrefix(ref, "up:") && f.up == "":
			f.up = ref
		case strings.HasPrefix(ref, "pub:") && f.pub == "":
			f.pub = ref
		default:
			t.Fatalf("unexpected saving reference %s in %v", ref, refs)
		}
	}
	if f.up == "" || f.pub == "" {
		t.Fatalf("productive up:/pub: absent: %v", refs)
	}
	for _, ref := range []string{f.up, f.pub} {
		var ttl int
		if err := f.fx.database.Session().Query(`SELECT TTL(created_at) FROM block_references WHERE org_id=? AND block_id=? AND referrer=?`, f.fx.orgID, f.fx.blockID, ref).Consistency(gocql.EachQuorum).Scan(&ttl); err != nil || ttl <= 0 {
			t.Fatalf("writer reference %s not TTL-bound: ttl=%d err=%v", ref, ttl, err)
		}
		if strings.HasPrefix(ref, "pub:") && ttl > dbpkg.PublishAttemptReferenceTTLSeconds {
			t.Fatalf("pub: TTL %d exceeds production bound", ttl)
		}
	}
	if f.fx.target.StorageKey == "" || f.fx.readTarget(t) != f.fx.target {
		t.Fatal("exact materialized P1 missing")
	}
}

// Exact retained state: no candidate, no queue, no D/root, exact P1/K1, HEAD
// and tree unchanged, no fs: promotion.
func (f *e114Fixture) assertUndiscovered(t *testing.T, label string) {
	t.Helper()
	if candidates := gcCandidateIdentitiesForTest(t, f.fx.orgID, f.fx.blockID); len(candidates) != 0 {
		f.candidates = append(f.candidates, candidates...)
		t.Fatalf("E1-14 DISCOVERED (%s): productive zero-ref candidate %+v", label, candidates)
	}
	if queued, err := e114Queued(f.fx.database, f); err != nil || queued != 0 {
		t.Fatalf("E1-14 %s: queue rows=%d err=%v", label, queued, err)
	}
	e12AssertNoDeleteLifecycle(t, f.fx)
	if f.fx.readTarget(t) != f.fx.target {
		t.Fatal("exact P1 changed")
	}
	w2AssertBytes(t, f.fx)
	if f.fx.hasOwnFSReferrer(t) {
		t.Fatal("unexpected fs: promotion")
	}
	if f.repair != nil {
		f.repair.retained(t)
	} else if borrowedFSReadHead(t, f.fx.database, f.fx.orgID, f.fx.repoID) != f.head {
		t.Fatal("HEAD changed")
	}
}

func (f *e114Fixture) phase0(t *testing.T) (*e114ScopedScan, int) {
	t.Helper()
	scope := f.scope()
	cleaned, err := gcpkg.NewScanner(scope, gcpkg.NewQueue(scope), &gcpkg.Stats{}, config.GCConfig{}).ScanExpiredProvisionalBlockRefsOnce(context.Background())
	if err != nil {
		t.Fatalf("productive Phase 0: %v", err)
	}
	return scope, cleaned
}

func (f *e114Fixture) projection(t *testing.T, row e112ExpiryRow) bool {
	t.Helper()
	var count int
	if err := f.fx.database.Session().Query(`SELECT count(*) FROM gc_provisional_block_refs_by_day WHERE expiry_day=? AND bucket=? AND expires_at=? AND org_id=? AND block_id=? AND referrer=?`, dbpkg.GCProjectionUTCDate(row.expires), dbpkg.GCDiscoveryBucket(f.fx.orgID, f.fx.blockID, row.ref), row.expires, f.fx.orgID, f.fx.blockID, row.ref).Consistency(gocql.EachQuorum).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count != 0
}

func (f *e114Fixture) awaitAbsent(t *testing.T, ref string, after time.Time) {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		present, err := f.fx.database.BlockReferenceExistsEachQuorumContext(context.Background(), f.fx.orgID, f.fx.blockID, ref)
		if err != nil {
			t.Fatal(err)
		}
		if !present && time.Now().After(after) {
			return
		}
	}
	t.Fatalf("actual Cassandra TTL did not retire %s", ref)
}

// The productive renewal API moves the exact real up: deadline and retracts its
// 48h projection in one batch; expiry itself is Cassandra TTL.
func (f *e114Fixture) expireUp(t *testing.T) {
	t.Helper()
	var class string
	var original time.Time
	if err := f.fx.database.Session().Query(`SELECT storage_class,expires_at FROM gc_provisional_block_refs WHERE org_id=? AND block_id=? AND referrer=?`, f.fx.orgID, f.fx.blockID, f.up).Consistency(gocql.EachQuorum).Scan(&class, &original); err != nil {
		t.Fatalf("real up: tracker missing: %v", err)
	}
	old := e112ExpiryRow{ref: f.up, expires: original.UTC()}
	f.trackers = append(f.trackers, old)
	if !f.projection(t, old) {
		t.Fatal("real up: projection missing")
	}
	deadline := time.Now().UTC().Add(3 * time.Second).Truncate(time.Millisecond)
	if err := f.fx.database.AddProvisionalBlockReferenceWithExpiry(f.fx.orgID, f.fx.blockID, f.up, f.fx.repoID, class, deadline); err != nil {
		t.Fatal(err)
	}
	short := e112ExpiryRow{ref: f.up, expires: deadline}
	f.trackers = append(f.trackers, short)
	if f.projection(t, old) || !f.projection(t, short) {
		t.Fatal("renewal did not move the exact up: projection")
	}
	f.awaitAbsent(t, f.up, deadline)
	if present, err := f.fx.database.BlockReferenceExistsEachQuorumContext(context.Background(), f.fx.orgID, f.fx.blockID, f.pub); err != nil || !present {
		t.Fatalf("pub: must outlive up: present=%t err=%v", present, err)
	}
	scope, cleaned := f.phase0(t)
	if scope.provisional != 1 || cleaned != 1 || f.projection(t, short) {
		t.Fatalf("Phase 0 did not resolve the expired up: listed=%d cleaned=%d", scope.provisional, cleaned)
	}
	if live, err := f.fx.database.BlockHasReferencesGlobal(f.fx.orgID, f.fx.blockID); err != nil || !live {
		t.Fatalf("pub:-only block must stay referenced: %t %v", live, err)
	}
	f.assertUndiscovered(t, "up-expired")
	t.Logf("E1-14 up: expired by TTL; Phase 0 resolved its projection; pub-only %s; candidate absent", f.pub)
}

// Shorten only the existing real pub: and await Cassandra TTL. The harness never
// creates a candidate; every later step is productive discovery.
func (f *e114Fixture) expireFinalPub(t *testing.T) {
	t.Helper()
	// Current runtime writes pub: without an expiry tracker. Record any row only so
	// a causal mutation that adds one is still independently torn down.
	tracker := func() (e112ExpiryRow, bool) {
		var expires time.Time
		err := f.fx.database.Session().Query(`SELECT expires_at FROM gc_provisional_block_refs WHERE org_id=? AND block_id=? AND referrer=?`, f.fx.orgID, f.fx.blockID, f.pub).Consistency(gocql.EachQuorum).Scan(&expires)
		if err == gocql.ErrNotFound {
			return e112ExpiryRow{}, false
		}
		if err != nil {
			t.Fatal(err)
		}
		row := e112ExpiryRow{ref: f.pub, expires: expires.UTC()}
		f.trackers = append(f.trackers, row)
		return row, true
	}
	_, staged := tracker()
	if err := f.fx.database.AddBlockReference(f.fx.orgID, f.fx.blockID, f.pub, f.fx.repoID, 2); err != nil {
		t.Fatal(err)
	}
	moved, shortened := tracker()
	// Cassandra TTL has second granularity; a tracked deadline (mutation only) must
	// also have passed before Phase 0 may classify it.
	f.awaitAbsent(t, f.pub, moved.expires)
	refs, err := f.fx.database.ListBlockReferrers(f.fx.orgID, f.fx.blockID)
	if err != nil || len(refs) != 0 {
		t.Fatalf("refs remain: %v %v", refs, err)
	}
	if live, err := f.fx.database.BlockHasReferencesGlobal(f.fx.orgID, f.fx.blockID); err != nil || live {
		t.Fatalf("global EQ refs not zero: %t %v", live, err)
	}
	if f.repair != nil {
		f.repair.unvisited(t)
	}
	scope, cleaned := f.phase0(t)
	f.assertUndiscovered(t, "Phase 0 after final pub: expiry")
	phase1 := f.scope()
	enqueued, err := gcpkg.NewScanner(phase1, gcpkg.NewQueue(phase1), &gcpkg.Stats{}, config.GCConfig{}).ScanOrphanedBlocksOnce(context.Background())
	if err != nil || enqueued != 0 || phase1.observed != 0 {
		t.Fatalf("Phase 1: enqueued=%d observed=%d err=%v", enqueued, phase1.observed, err)
	}
	n, err := w2Worker(t, gcpkg.NewCassandraStore(f.fx.database), f.fx.target.StorageClass).ProcessOrgOnce(t.Context(), f.fx.orgUUID)
	if err != nil || n != 0 {
		t.Fatalf("productive worker processed owned org: n=%d err=%v", n, err)
	}
	f.assertUndiscovered(t, "after Phase 1 and worker")
	if f.repair != nil {
		f.repair.unvisited(t)
	}
	if staged || shortened {
		t.Fatalf("pub: unexpectedly has an expiry tracker: staged=%t shortened=%t", staged, shortened)
	}
	t.Logf("E1-14 final pub: %s (no expiry tracker/projection) expired by TTL: global refs=0; Phase 0 listed=%d cleaned=%d; Phase 1 enqueued=0; worker n=0; candidate/queue/D/root absent; P1=(%s,%s)/K1 retained", f.pub, scope.provisional, cleaned, f.fx.target.StorageClass, f.fx.target.StorageKey)
}

func (f *e114Fixture) repairRenews(t *testing.T) {
	t.Helper()
	r := f.repair
	visits, outcome := 0, ""
	t.Cleanup(v2pkg.SetRepairBeforeVisitForIntegration(f.fx.database, f.fx.repoID, func(commit, fs string) {
		if commit != r.repair.commitID || fs != r.repair.fsID {
			t.Errorf("unexpected repair identity %s/%s", commit, fs)
		}
		visits++
	}))
	t.Cleanup(v2pkg.SetRepairAfterClassifyForIntegration(f.fx.database, f.fx.repoID, func(got string, err error) {
		if err != nil {
			t.Errorf("native classifier error: %v", err)
		}
		outcome = got
	}))
	err := v2pkg.RunPublishedBlockReferenceRepairSweepAtForIntegration(f.fx.database, time.Now().Add(2*time.Hour))
	if err == nil || !strings.Contains(err.Error(), "unknown; retain queued repair") {
		t.Fatalf("native UNKNOWN sweep result: %v", err)
	}
	if visits != 1 || outcome != "unknown" {
		t.Fatalf("repair visit=%d outcome=%q", visits, outcome)
	}
	renewed := dbpkg.BlockReferrerForPublishAttempt(f.fx.repoID + ":" + r.repair.commitID + ":" + r.repair.fsID)
	refs, err := f.fx.database.ListBlockReferrers(f.fx.orgID, f.fx.blockID)
	if err != nil || len(refs) != 1 || refs[0] != renewed {
		t.Fatalf("native UNKNOWN must renew exactly %s: %v %v", renewed, refs, err)
	}
	var ttl int
	if err := f.fx.database.Session().Query(`SELECT TTL(created_at) FROM block_references WHERE org_id=? AND block_id=? AND referrer=?`, f.fx.orgID, f.fx.blockID, renewed).Consistency(gocql.EachQuorum).Scan(&ttl); err != nil || ttl <= 0 || ttl > dbpkg.PublishAttemptReferenceTTLSeconds {
		t.Fatalf("renewed pub: TTL=%d err=%v", ttl, err)
	}
	f.assertUndiscovered(t, "after repair renewal")
	t.Logf("E1-14 repair visited after zero refs: native UNKNOWN renewed %s (ttl=%d); repair retained; no fs:/HEAD/P change; candidate absent", renewed, ttl)
}

func TestPubZeroRefTransition(t *testing.T) {
	if endpoint := os.Getenv("SESAMEFS_E114_ISOLATED_URL"); endpoint != "" && os.Getenv("SESAMEFS_E114_CHILD") != "1" {
		e114RunIsolated(t, endpoint)
		return
	}
	requireCassandra(t)
	if runtime.GOOS != "linux" || os.Getenv("SESAMEFS_TEST_IN_CONTAINER") != "1" {
		t.Fatal("E1-14 requires Linux Docker")
	}
	for _, endpoint := range []string{superadminClient.baseURL, envOrDefault("SESAMEFS_URL_2", "http://sesamefs-node-2:8080"), envOrDefault("SESAMEFS_URL_3", "http://sesamefs-node-3:8080")} {
		if err := e19CheckGCDisabled(newTestClient(endpoint, superadminClient.token)); err != nil {
			t.Fatalf("E1-14 isolation: %v", err)
		}
	}
	for _, leg := range e114Legs {
		t.Run(leg, func(t *testing.T) {
			t.Cleanup(func() {
				if !t.Failed() && !t.Skipped() {
					e114Observed[leg] = true
				}
			})
			var f *e114Fixture
			if leg == "final-pub-expiry-with-repair" || leg == "repair-renews-after-zero-ref" {
				f = e114RepairFixture(t)
			} else {
				f = e114NoRepairFixture(t)
			}
			if live, err := f.fx.database.BlockHasReferencesGlobal(f.fx.orgID, f.fx.blockID); err != nil || !live {
				t.Fatalf("fixture must start referenced: %t %v", live, err)
			}
			if leg == "pub-live-control" {
				if _, cleaned := f.phase0(t); cleaned != 0 {
					t.Fatalf("Phase 0 resolved a live up: cleaned=%d", cleaned)
				}
				f.assertUndiscovered(t, "live control")
				t.Logf("E1-14 control: up=%s pub=%s live; no repair; Phase 0 cleaned=0; candidate absent; P1/K1 present", f.up, f.pub)
				return
			}
			f.expireUp(t)
			if leg == "up-expires-pub-remains" {
				return
			}
			f.expireFinalPub(t)
			if leg == "repair-renews-after-zero-ref" {
				f.repairRenews(t)
			}
		})
	}
}

func e114RunIsolated(t *testing.T, endpoint string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	run := "^TestPubZeroRefTransition$"
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
	cmd.Env = append(cmd.Env, e114EvidenceEnv+"=1", "SESAMEFS_E114_CHILD=1", "CASSANDRA_KEYSPACE=sesamefs_e19", "SESAMEFS_URL="+endpoint, "SESAMEFS_URL_2="+endpoint, "SESAMEFS_URL_3="+endpoint)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("isolated E1-14: %v", err)
	}
	for _, leg := range e114Legs {
		e114Observed[leg] = true
	}
}

func TestPubZeroRefTransitionCompleteness(t *testing.T) {
	if len(e114Missing(nil)) != 5 {
		t.Fatal("five required legs")
	}
	all := map[string]bool{}
	for _, leg := range e114Legs {
		if all[leg] {
			t.Fatal("duplicate leg")
		}
		all[leg] = true
	}
	if len(e114Missing(all)) != 0 {
		t.Fatal("complete rejected")
	}
	for _, leg := range e114Legs {
		delete(all, leg)
		if missing := e114Missing(all); len(missing) != 1 || missing[0] != leg {
			t.Fatalf("omission %s: %v", leg, missing)
		}
		all[leg] = true
	}
}
