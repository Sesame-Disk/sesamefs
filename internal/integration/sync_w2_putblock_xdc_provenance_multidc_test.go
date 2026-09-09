//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	apipkg "github.com/Sesame-Disk/sesamefs/internal/api"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

// Real 3-DC evidence for ISSUE-SYNC-PUTBLOCK-CROSS-DC-PROVENANCE-VISIBILITY-01.
//
// This is intentionally separate from scripts/w2-post-head-multidc-validation.sh:
// that leg exercises the post-HEAD publication-repair classifier against a
// HEAD written only in dc-eu. This leg exercises the pre-HEAD Sync PutBlock
// provenance scope gate (syncBlockHasOwnLivenessProvenanceFn) against an
// up:sync:<repo>:<block> reference acknowledged only in dc-eu, reusing the
// same w2PostHead3DCConnect/w2PostHead3DCEndpoints connection helpers rather
// than inventing a parallel 3-DC harness.
//
// No sesamefs application instance is required per datacenter: both legs
// below call the real production function directly through a *db.DB
// connected to a specific datacenter (via the internal/api ...ForIntegration
// wrappers), the same pattern publish_repair_multidc_integration_test.go
// already uses for the post-HEAD classifier.

const w2SyncXDCEvidenceEnv = "SESAMEFS_REQUIRE_W2_SYNC_PUTBLOCK_XDC_EVIDENCE"

var w2SyncXDCEvidence bool

func testW2SyncXDCSHA256BlockID(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

// w2SyncXDCCostBlockIDs deterministically regenerates the same N block IDs
// from one shared prefix, so the write side (in dc-eu) and the read side (in
// dc-na) agree on exactly which blocks exist without passing a long list
// through env vars.
func w2SyncXDCCostBlockIDs(prefix string, n int) []string {
	blockIDs := make([]string, n)
	for i := range blockIDs {
		blockIDs[i] = testW2SyncXDCSHA256BlockID(fmt.Sprintf("%s-%d", prefix, i))
	}
	return blockIDs
}

// TestW2SyncXDCPutBlockWritesProvenanceInEU3DC simulates a real PutBlock
// landing in dc-eu while dc-na and dc-asia are stopped: this establishes the
// LOCAL_QUORUM-acknowledged up:sync:<repo>:<block> reference the next tests
// prove dc-na can still recover after an immediate, blind restart.
//
// W2_SYNC_XDC_BLOCK_COUNT (default 1) additionally seeds that many more
// blocks under a shared, logged prefix, so
// TestW2SyncXDCAllCrossDCHitCostAtN3DC can measure the all-cross-DC-hit cost
// scenario the single-DC characterization cannot: a single-DC keyspace
// cannot distinguish LOCAL_QUORUM from EACH_QUORUM, so it can only measure
// the genuinely-unprovenanced and local-hit cases, never a real cross-DC hit.
func TestW2SyncXDCPutBlockWritesProvenanceInEU3DC(t *testing.T) {
	if os.Getenv("W2_SYNC_XDC_WRITE_EU") != "1" {
		t.Skip("W2_SYNC_XDC_WRITE_EU is not set")
	}
	endpoints := w2PostHead3DCEndpoints(t)
	database := w2PostHead3DCConnect(t, "dc-eu", endpoints)

	orgID := uuid.NewString()
	repoID := uuid.NewString()
	blockID := testW2SyncXDCSHA256BlockID("w2-sync-xdc-" + uuid.NewString())
	expiresAt := time.Now().UTC().Add(48 * time.Hour)

	if err := apipkg.SyncSimulatePutBlockProvenanceForIntegration(database, orgID, repoID, blockID, "hot", expiresAt); err != nil {
		t.Fatalf("simulate PutBlock provenance write in dc-eu: %v", err)
	}
	t.Logf("W2_SYNC_XDC_ORG=%s", orgID)
	t.Logf("W2_SYNC_XDC_REPO=%s", repoID)
	t.Logf("W2_SYNC_XDC_BLOCK=%s", blockID)

	costCount := 0
	if raw := strings.TrimSpace(os.Getenv("W2_SYNC_XDC_BLOCK_COUNT")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			t.Fatalf("W2_SYNC_XDC_BLOCK_COUNT must be a non-negative integer, got %q", raw)
		}
		costCount = parsed
	}
	if costCount > 0 {
		costPrefix := "w2-sync-xdc-cost-" + uuid.NewString()
		costBlockIDs := w2SyncXDCCostBlockIDs(costPrefix, costCount)
		g := new(errgroup.Group)
		g.SetLimit(20)
		for _, blockID := range costBlockIDs {
			blockID := blockID
			g.Go(func() error {
				return apipkg.SyncSimulatePutBlockProvenanceForIntegration(database, orgID, repoID, blockID, "hot", expiresAt)
			})
		}
		if err := g.Wait(); err != nil {
			t.Fatalf("seed cost characterization blocks in dc-eu: %v", err)
		}
		t.Logf("W2_SYNC_XDC_COST_PREFIX=%s", costPrefix)
		t.Logf("W2_SYNC_XDC_COST_COUNT=%d", costCount)
	}
}

// TestW2SyncXDCRecoversCrossDCProvenanceFromBlindNA3DC proves the minimum
// contract for ISSUE-SYNC-PUTBLOCK-CROSS-DC-PROVENANCE-VISIBILITY-01: a
// PutBlock acknowledged in dc-eu must not be misclassified as absent when
// the pre-HEAD readiness scope gate runs in dc-na immediately after dc-na
// restarts, before any hint/repair delivery could have converged the write.
//
// This calls the REAL production scope-gate function
// (syncBlockHasOwnLivenessProvenanceFn, via SyncBlockHasOwnLivenessProvenanceForIntegration),
// not a reimplementation: without the EACH_QUORUM fallback this leg is RED
// (found=false, exactly the bug this closes); with it, GREEN.
func TestW2SyncXDCRecoversCrossDCProvenanceFromBlindNA3DC(t *testing.T) {
	if os.Getenv(w2SyncXDCEvidenceEnv) != "1" {
		t.Skipf("%s is not set", w2SyncXDCEvidenceEnv)
	}
	endpoints := w2PostHead3DCEndpoints(t)
	database := w2PostHead3DCConnect(t, "dc-na", endpoints)

	orgID := strings.TrimSpace(os.Getenv("W2_SYNC_XDC_ORG"))
	repoID := strings.TrimSpace(os.Getenv("W2_SYNC_XDC_REPO"))
	blockID := strings.TrimSpace(os.Getenv("W2_SYNC_XDC_BLOCK"))
	if orgID == "" || repoID == "" || blockID == "" {
		t.Fatal("W2_SYNC_XDC_ORG, W2_SYNC_XDC_REPO, and W2_SYNC_XDC_BLOCK are required")
	}
	referrer := apipkg.SyncBlockUploadReferrerForIntegration(repoID, blockID)
	t.Cleanup(func() {
		_ = database.Session().Query(`DELETE FROM block_references WHERE org_id = ? AND block_id = ? AND referrer = ?`,
			orgID, blockID, referrer).Consistency(gocql.EachQuorum).Exec()
	})

	// Confirm the fixture is actually blind before asserting anything about
	// recovery -- otherwise a slow/fast test environment could accidentally
	// let replication converge first and this leg would pass for the wrong
	// reason.
	localFound, localErr := database.BlockReferenceExistsLocalQuorum(orgID, blockID, referrer)
	if localErr != nil {
		t.Fatalf("local read itself failed (expected a clean miss, not an error): %v", localErr)
	}
	if localFound {
		t.Fatal("fixture is not blind: dc-na's LOCAL_QUORUM read already sees the dc-eu write; this leg must run immediately after dc-na restarts, before replication converges")
	}
	t.Log("confirmed: dc-na's LOCAL_QUORUM read is blind to the dc-eu PutBlock write, as expected")

	found, err := apipkg.SyncBlockHasOwnLivenessProvenanceForIntegration(database, orgID, repoID, blockID)
	if err != nil {
		t.Fatalf("production scope gate returned an error instead of recovering provenance: %v", err)
	}
	if !found {
		t.Fatal("RED: the production scope gate reports no provenance for a block PutBlock acknowledged in dc-eu, solely because dc-na has not observed it locally yet -- ISSUE-SYNC-PUTBLOCK-CROSS-DC-PROVENANCE-VISIBILITY-01 reproduced")
	}
	t.Log("GREEN: the EACH_QUORUM fallback recovered cross-DC provenance dc-na's local read alone missed")
	w2SyncXDCEvidence = true
}

// TestW2SyncXDCAllCrossDCHitCostAtN3DC measures the one scenario the
// single-DC cost characterization (TestW2SyncXDCProvenanceCostCharacterization)
// structurally cannot: all-cross-DC-hit, at real inter-datacenter distance.
// A single-DC keyspace cannot distinguish LOCAL_QUORUM from EACH_QUORUM, so
// it can only measure the genuinely-unprovenanced and local-hit cases; this
// is the scenario #210 actually exists to fix (a local miss that recovers
// via a real EACH_QUORUM round trip to another datacenter), not merely the
// cost of an EACH_QUORUM read that finds nothing.
func TestW2SyncXDCAllCrossDCHitCostAtN3DC(t *testing.T) {
	// Deliberately does not gate on w2SyncXDCEvidenceEnv: that flag also
	// drives TestMain's cross-process completeness check for
	// TestW2SyncXDCRecoversCrossDCProvenanceFromBlindNA3DC, which this test
	// does not participate in (it runs in its own separate go test
	// invocation, see scripts/w2-sync-putblock-xdc-provenance-validation.sh).
	// Whether to run is entirely determined by having cost fixture data to
	// measure.
	prefix := strings.TrimSpace(os.Getenv("W2_SYNC_XDC_COST_PREFIX"))
	countRaw := strings.TrimSpace(os.Getenv("W2_SYNC_XDC_COST_COUNT"))
	if prefix == "" || countRaw == "" {
		t.Skip("W2_SYNC_XDC_COST_PREFIX/W2_SYNC_XDC_COST_COUNT not set (W2_SYNC_XDC_BLOCK_COUNT was 0 or unset for the write leg)")
	}
	total, err := strconv.Atoi(countRaw)
	if err != nil {
		t.Fatalf("W2_SYNC_XDC_COST_COUNT must be an integer, got %q", countRaw)
	}
	endpoints := w2PostHead3DCEndpoints(t)
	database := w2PostHead3DCConnect(t, "dc-na", endpoints)

	orgID := strings.TrimSpace(os.Getenv("W2_SYNC_XDC_ORG"))
	repoID := strings.TrimSpace(os.Getenv("W2_SYNC_XDC_REPO"))
	if orgID == "" || repoID == "" {
		t.Fatal("W2_SYNC_XDC_ORG and W2_SYNC_XDC_REPO are required")
	}
	allBlockIDs := w2SyncXDCCostBlockIDs(prefix, total)
	t.Cleanup(func() {
		for _, blockID := range allBlockIDs {
			referrer := apipkg.SyncBlockUploadReferrerForIntegration(repoID, blockID)
			_ = database.Session().Query(`DELETE FROM block_references WHERE org_id = ? AND block_id = ? AND referrer = ?`,
				orgID, blockID, referrer).Consistency(gocql.EachQuorum).Exec()
		}
	})

	for _, n := range []int{1, 10, 100, 1000} {
		if n > total {
			t.Logf("N=%d skipped: only %d cost blocks were seeded", n, total)
			continue
		}
		blockIDs := allBlockIDs[:n]
		start := time.Now()
		g := new(errgroup.Group)
		g.SetLimit(20)
		for _, blockID := range blockIDs {
			blockID := blockID
			g.Go(func() error {
				found, err := apipkg.SyncBlockHasOwnLivenessProvenanceForIntegration(database, orgID, repoID, blockID)
				if err != nil {
					return fmt.Errorf("block %s: %w", blockID, err)
				}
				if !found {
					return fmt.Errorf("block %s: real cross-DC provenance not recovered", blockID)
				}
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			t.Fatalf("N=%d all-cross-DC-hit: %v", n, err)
		}
		elapsed := time.Since(start)
		t.Logf("N=%-4d C_all_cross_dc_hit wall_clock=%s (real 3-DC, all %d blocks recovered via EACH_QUORUM from dc-na)", n, elapsed, n)
	}
}

// TestW2SyncXDCFallbackFailsClosedWhenADatacenterIsDown3DC proves the
// availability-domain consequence explicitly, not just as a performance
// number: with a datacenter unreachable, the EACH_QUORUM fallback for a
// genuinely unprovenanced block must fail closed (return an error) within a
// bounded time, never silently resolve to "not found" and never hang. This
// is scenario F/G from the cost/availability characterization
// (docs/KNOWN_ISSUES.md): "one DC down" is an availability-domain change
// for the readiness decision, not merely a latency number.
func TestW2SyncXDCFallbackFailsClosedWhenADatacenterIsDown3DC(t *testing.T) {
	if os.Getenv("W2_SYNC_XDC_ONE_DC_DOWN") != "1" {
		t.Skip("W2_SYNC_XDC_ONE_DC_DOWN is not set")
	}
	endpoints := w2PostHead3DCEndpoints(t)
	database := w2PostHead3DCConnect(t, "dc-na", endpoints)

	orgID := uuid.NewString()
	repoID := uuid.NewString()
	blockID := testW2SyncXDCSHA256BlockID("w2-sync-xdc-dc-down-" + uuid.NewString())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	type result struct {
		found bool
		err   error
	}
	done := make(chan result, 1)
	go func() {
		found, err := apipkg.SyncBlockHasOwnLivenessProvenanceForIntegration(database, orgID, repoID, blockID)
		done <- result{found: found, err: err}
	}()
	select {
	case r := <-done:
		if r.err == nil {
			t.Fatalf("scope gate returned found=%v, err=nil with one datacenter down; it must fail closed with an error, never silently resolve absence", r.found)
		}
		if r.found {
			t.Fatal("scope gate must never report found=true alongside an error")
		}
		t.Logf("confirmed: with one datacenter down, the scope gate fails closed within the timeout: %v", r.err)
	case <-ctx.Done():
		t.Fatal("scope gate did not return within 30s with one datacenter down -- it must fail closed with a bounded error, not hang")
	}
}
