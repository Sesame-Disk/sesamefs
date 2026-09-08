//go:build integration

package integration

import (
	"os"
	"strings"
	"testing"
)

const w2SyncPutBlockHeadEvidenceEnv = "SESAMEFS_REQUIRE_W2_SYNC_PUTBLOCK_HEAD_EVIDENCE"
const w2SyncPutBlockHeadCrashEvidenceEnv = "SESAMEFS_REQUIRE_W2_SYNC_PUTBLOCK_HEAD_CRASH_EVIDENCE"

// w2SyncPutBlockHeadEvidenceState records each Sync PutBlock -> HEAD
// continuity leg by name. Completeness is the conjunction of these fields,
// never a counter: marking one leg twice cannot hide another.
type w2SyncPutBlockHeadEvidenceState struct {
	normalFlowRenewsLivenessAndSettles                   bool
	crossNodeIdempotentRepairSettlesWithoutProcessMemory bool
	activeGCClaimBlocksHeadFailClosed                    bool
	divergentCASLoserRetainsSharedRepairRow              bool
	autoMergeProductionPathSettlesWithRealBlocks         bool
}

func (state w2SyncPutBlockHeadEvidenceState) namedLegs() []struct {
	name string
	seen bool
} {
	return []struct {
		name string
		seen bool
	}{
		{"normalFlowRenewsLivenessAndSettles", state.normalFlowRenewsLivenessAndSettles},
		{"crossNodeIdempotentRepairSettlesWithoutProcessMemory", state.crossNodeIdempotentRepairSettlesWithoutProcessMemory},
		{"activeGCClaimBlocksHeadFailClosed", state.activeGCClaimBlocksHeadFailClosed},
		{"divergentCASLoserRetainsSharedRepairRow", state.divergentCASLoserRetainsSharedRepairRow},
		{"autoMergeProductionPathSettlesWithRealBlocks", state.autoMergeProductionPathSettlesWithRealBlocks},
	}
}

func (state w2SyncPutBlockHeadEvidenceState) complete() bool {
	for _, leg := range state.namedLegs() {
		if !leg.seen {
			return false
		}
	}
	return true
}

func (state w2SyncPutBlockHeadEvidenceState) missing() []string {
	missing := make([]string, 0, len(state.namedLegs()))
	for _, leg := range state.namedLegs() {
		if !leg.seen {
			missing = append(missing, leg.name)
		}
	}
	return missing
}

var w2SyncPutBlockHeadEvidence w2SyncPutBlockHeadEvidenceState

type w2SyncPutBlockHeadEvidenceGate struct{ observed bool }

func w2SyncPutBlockHeadRequireEvidence(t *testing.T) *w2SyncPutBlockHeadEvidenceGate {
	t.Helper()
	gate := &w2SyncPutBlockHeadEvidenceGate{}
	if os.Getenv(w2SyncPutBlockHeadEvidenceEnv) != "1" {
		return gate
	}
	t.Cleanup(func() {
		if t.Skipped() {
			t.Errorf("%s=1 requires real Cassandra+MinIO Sync PutBlock->HEAD evidence, but the test skipped", w2SyncPutBlockHeadEvidenceEnv)
		} else if !t.Failed() && !gate.observed {
			t.Errorf("%s=1 incomplete Sync PutBlock->HEAD evidence; missing=%s", w2SyncPutBlockHeadEvidenceEnv, strings.Join(w2SyncPutBlockHeadEvidence.missing(), ","))
		}
	})
	return gate
}

func markW2SyncPutBlockHeadEvidence(t *testing.T, leg string) {
	t.Helper()
	switch leg {
	case "normalFlowRenewsLivenessAndSettles":
		w2SyncPutBlockHeadEvidence.normalFlowRenewsLivenessAndSettles = true
	case "crossNodeIdempotentRepairSettlesWithoutProcessMemory":
		w2SyncPutBlockHeadEvidence.crossNodeIdempotentRepairSettlesWithoutProcessMemory = true
	case "activeGCClaimBlocksHeadFailClosed":
		w2SyncPutBlockHeadEvidence.activeGCClaimBlocksHeadFailClosed = true
	case "divergentCASLoserRetainsSharedRepairRow":
		w2SyncPutBlockHeadEvidence.divergentCASLoserRetainsSharedRepairRow = true
	case "autoMergeProductionPathSettlesWithRealBlocks":
		w2SyncPutBlockHeadEvidence.autoMergeProductionPathSettlesWithRealBlocks = true
	default:
		t.Fatalf("unknown W2 Sync PutBlock->HEAD evidence leg %q", leg)
	}
}

type w2SyncPutBlockHeadCrashEvidenceGate struct{ observed bool }

// w2SyncPutBlockHeadCrashEvidenceObserved is a package-level flag, distinct
// from the per-test w2SyncPutBlockHeadCrashEvidenceGate above. The gate's own
// t.Cleanup check only runs if TestW2SyncPutBlockHeadEvidence actually ran; a
// -run filter that excludes it entirely (while the env var is still set)
// would otherwise let TestMain exit 0 having never executed the leg — the
// same false-green shape the package comment on TestMain's requireEvidence
// chain warns about for every other gate. Checking this flag after m.Run()
// closes that hole for the crash gate specifically.
var w2SyncPutBlockHeadCrashEvidenceObserved bool

func w2SyncPutBlockHeadRequireCrashEvidence(t *testing.T) *w2SyncPutBlockHeadCrashEvidenceGate {
	t.Helper()
	gate := &w2SyncPutBlockHeadCrashEvidenceGate{}
	if os.Getenv(w2SyncPutBlockHeadCrashEvidenceEnv) != "1" {
		return gate
	}
	t.Cleanup(func() {
		if t.Skipped() {
			t.Errorf("%s=1 requires real post-CAS crash/replay evidence, but the test skipped", w2SyncPutBlockHeadCrashEvidenceEnv)
		} else if !t.Failed() && !gate.observed {
			t.Errorf("%s=1 did not observe the real post-CAS crash/replay leg", w2SyncPutBlockHeadCrashEvidenceEnv)
		}
	})
	return gate
}

func markW2SyncPutBlockHeadCrashEvidence(t *testing.T, gate *w2SyncPutBlockHeadCrashEvidenceGate) {
	t.Helper()
	gate.observed = true
	w2SyncPutBlockHeadCrashEvidenceObserved = true
}

func TestW2SyncPutBlockHeadEvidenceRequiresEveryNamedLeg(t *testing.T) {
	partial := w2SyncPutBlockHeadEvidenceState{normalFlowRenewsLivenessAndSettles: true, activeGCClaimBlocksHeadFailClosed: true}
	if partial.complete() {
		t.Fatal("partial Sync PutBlock->HEAD evidence must not satisfy the package gate")
	}
	missing := strings.Join(partial.missing(), ",")
	if !strings.Contains(missing, "crossNodeIdempotentRepairSettlesWithoutProcessMemory") ||
		!strings.Contains(missing, "divergentCASLoserRetainsSharedRepairRow") ||
		!strings.Contains(missing, "autoMergeProductionPathSettlesWithRealBlocks") {
		t.Fatalf("missing() must name absent legs individually, got %q", missing)
	}
	full := w2SyncPutBlockHeadEvidenceState{
		normalFlowRenewsLivenessAndSettles:                   true,
		crossNodeIdempotentRepairSettlesWithoutProcessMemory: true,
		activeGCClaimBlocksHeadFailClosed:                    true,
		divergentCASLoserRetainsSharedRepairRow:              true,
		autoMergeProductionPathSettlesWithRealBlocks:         true,
	}
	if !full.complete() || len(full.missing()) != 0 || len(full.namedLegs()) != 5 {
		t.Fatalf("all 5 named legs should satisfy the package gate; missing=%v legs=%d", full.missing(), len(full.namedLegs()))
	}
	twice := partial
	twice.normalFlowRenewsLivenessAndSettles = true
	if twice.complete() {
		t.Fatal("marking one leg twice must not hide a different required leg")
	}
}
