//go:build integration

package integration

import (
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	apipkg "github.com/Sesame-Disk/sesamefs/internal/api"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

// cleanupW2SyncXDCCostFixture removes exactly the rows one
// AddProvisionalBlockReferenceWithExpiry call created for (orgID, blockID,
// referrer): the block_references row, the canonical gc_provisional_block_refs
// tracker, and the gc_provisional_block_refs_by_day discovery projection
// (deliberately non-TTL, so it does not self-expire). No partition-wide or
// global cleanup -- exact-key deletes only, using the same production
// AddDeleteProvisionalBlockRefExpiryDiscoveryQuery helper the repair worker
// itself uses to retract a projection, so the bucket/day key computation
// can't drift from what the writer used.
func cleanupW2SyncXDCCostFixture(t *testing.T, database *dbpkg.DB, orgID, blockID, referrer string, expiresAt time.Time) {
	t.Helper()
	if err := database.Session().Query(`DELETE FROM block_references WHERE org_id = ? AND block_id = ? AND referrer = ?`,
		orgID, blockID, referrer).Exec(); err != nil {
		t.Logf("cleanup: delete block_references for org=%s block=%s failed: %v", orgID, blockID, err)
	}
	if err := database.Session().Query(`DELETE FROM gc_provisional_block_refs WHERE org_id = ? AND block_id = ? AND referrer = ?`,
		orgID, blockID, referrer).Exec(); err != nil {
		t.Logf("cleanup: delete gc_provisional_block_refs for org=%s block=%s failed: %v", orgID, blockID, err)
	}
	batch := database.Session().Batch(gocql.LoggedBatch)
	dbpkg.AddDeleteProvisionalBlockRefExpiryDiscoveryQuery(batch, orgID, blockID, referrer, expiresAt)
	if err := batch.Exec(); err != nil {
		t.Logf("cleanup: delete gc_provisional_block_refs_by_day for org=%s block=%s failed: %v", orgID, blockID, err)
	}
}

// Cost/availability characterization for
// ISSUE-SYNC-PUTBLOCK-CROSS-DC-PROVENANCE-VISIBILITY-01's EACH_QUORUM
// fallback (docs/KNOWN_ISSUES.md has the measured numbers and the resulting
// merge decision). This measures the real production scope-gate function
// (SyncBlockHasOwnLivenessProvenanceForIntegration, i.e.
// syncBlockHasOwnLivenessProvenanceFn) under the same bounded concurrency
// (20) production uses, against the running single-DC dev stack.
//
// Single-DC caveat, stated once here rather than repeated at every call
// site: this stack's keyspace has one datacenter, so an EACH_QUORUM read
// resolves to the same replica set a LOCAL_QUORUM read would -- there is no
// real WAN hop to measure here. This characterization is therefore a lower
// bound on latency (it isolates the added-round-trip cost in a low-latency
// environment) and an exact measurement of added query count; it is not a
// substitute for measuring real inter-region latency against a deployed
// multi-region cluster. The real cross-DC recovery behavior itself (a
// genuine WAN-separated fallback actually finding remote provenance, and
// failing closed when a datacenter is down) is proven separately by
// scripts/w2-sync-putblock-xdc-provenance-validation.sh against the real
// 3-DC fixture, not by this single-DC test.
func TestW2SyncXDCProvenanceCostCharacterization(t *testing.T) {
	if os.Getenv("SESAMEFS_MEASURE_W2_SYNC_XDC_COST") != "1" {
		t.Skip("SESAMEFS_MEASURE_W2_SYNC_XDC_COST is not set")
	}
	database, err := openIntegrationProjectionDB()
	if err != nil {
		t.Fatalf("connect to Cassandra: %v", err)
	}
	t.Cleanup(database.Close)

	const concurrency = 20 // syncCommitBlockPlacementConcurrency in internal/api/sync.go
	orgID := uuid.NewString()
	repoID := uuid.NewString()

	// runScenario times N calls to the real production scope-gate function
	// under the production concurrency bound, for a set of blocks where
	// exactly `hitCount` of them have a real, local, PutBlock-established
	// provenance reference and the rest have none anywhere.
	runScenario := func(t *testing.T, n, hitCount int) time.Duration {
		t.Helper()
		blockIDs := make([]string, n)
		for i := range blockIDs {
			blockIDs[i] = testW2SyncXDCSHA256BlockID(fmt.Sprintf("w2-xdc-cost-%s-%d", uuid.NewString(), i))
			if i < hitCount {
				expiresAt := time.Now().UTC().Add(48 * time.Hour)
				if err := apipkg.SyncSimulatePutBlockProvenanceForIntegration(database, orgID, repoID, blockIDs[i], "hot", expiresAt); err != nil {
					t.Fatalf("seed provenance for block %d: %v", i, err)
				}
				blockID, referrer := blockIDs[i], apipkg.SyncBlockUploadReferrerForIntegration(repoID, blockIDs[i])
				t.Cleanup(func() { cleanupW2SyncXDCCostFixture(t, database, orgID, blockID, referrer, expiresAt) })
			}
		}

		start := time.Now()
		g := new(errgroup.Group)
		g.SetLimit(concurrency)
		for _, blockID := range blockIDs {
			blockID := blockID
			g.Go(func() error {
				_, err := apipkg.SyncBlockHasOwnLivenessProvenanceForIntegration(database, orgID, repoID, blockID)
				return err
			})
		}
		if err := g.Wait(); err != nil {
			t.Fatalf("scope-gate call failed: %v", err)
		}
		return time.Since(start)
	}

	type row struct {
		n         int
		scenario  string
		hitCount  int
		elapsed   time.Duration
	}
	var rows []row

	for _, n := range []int{1, 10, 100, 1000} {
		scenarios := map[string]int{
			"A_all_local_hit":        n,     // every block has local provenance -- fast path only
			"D_all_unprovenanced":    0,     // no block has provenance anywhere -- every block pays the fallback
			"B_one_local_miss":       n - 1, // n-1 hit, exactly 1 pays the fallback (n=1 degenerates to D)
			"E_mixed_half":           n / 2, // half hit, half pay the fallback
		}
		names := make([]string, 0, len(scenarios))
		for name := range scenarios {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			hitCount := scenarios[name]
			if hitCount < 0 {
				hitCount = 0
			}
			elapsed := runScenario(t, n, hitCount)
			rows = append(rows, row{n: n, scenario: name, hitCount: hitCount, elapsed: elapsed})
			t.Logf("N=%d scenario=%s local_hits=%d local_misses=%d wall_clock=%s", n, name, hitCount, n-hitCount, elapsed)
		}
	}

	t.Log("--- W2 Sync XDC provenance cost characterization (single-DC dev stack; see doc comment for the WAN caveat) ---")
	for _, r := range rows {
		t.Logf("N=%-4d %-24s local_hits=%-4d local_misses=%-4d wall_clock=%s", r.n, r.scenario, r.hitCount, r.n-r.hitCount, r.elapsed)
	}
}
