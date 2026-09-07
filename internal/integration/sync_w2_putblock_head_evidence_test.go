//go:build integration

package integration

import (
	"os"
	"strings"
	"testing"
)

const w2SyncPutBlockHeadEvidenceEnv = "SESAMEFS_REQUIRE_W2_SYNC_PUTBLOCK_HEAD_EVIDENCE"

// w2SyncPutBlockHeadEvidenceState records each Sync PutBlock -> HEAD
// continuity leg by name. Completeness is the conjunction of these fields,
// never a counter: marking one leg twice cannot hide another.
type w2SyncPutBlockHeadEvidenceState struct {
	normalFlowRenewsLivenessAndSettles                   bool
	crossNodeIdempotentRepairSettlesWithoutProcessMemory bool
	activeGCClaimBlocksHeadFailClosed                    bool
	definitiveCASLoserCleansOnlyOwnAttempt               bool
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
		{"definitiveCASLoserCleansOnlyOwnAttempt", state.definitiveCASLoserCleansOnlyOwnAttempt},
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
	case "definitiveCASLoserCleansOnlyOwnAttempt":
		w2SyncPutBlockHeadEvidence.definitiveCASLoserCleansOnlyOwnAttempt = true
	case "autoMergeProductionPathSettlesWithRealBlocks":
		w2SyncPutBlockHeadEvidence.autoMergeProductionPathSettlesWithRealBlocks = true
	default:
		t.Fatalf("unknown W2 Sync PutBlock->HEAD evidence leg %q", leg)
	}
}

func TestW2SyncPutBlockHeadEvidenceRequiresEveryNamedLeg(t *testing.T) {
	partial := w2SyncPutBlockHeadEvidenceState{normalFlowRenewsLivenessAndSettles: true, activeGCClaimBlocksHeadFailClosed: true}
	if partial.complete() {
		t.Fatal("partial Sync PutBlock->HEAD evidence must not satisfy the package gate")
	}
	missing := strings.Join(partial.missing(), ",")
	if !strings.Contains(missing, "crossNodeIdempotentRepairSettlesWithoutProcessMemory") ||
		!strings.Contains(missing, "definitiveCASLoserCleansOnlyOwnAttempt") ||
		!strings.Contains(missing, "autoMergeProductionPathSettlesWithRealBlocks") {
		t.Fatalf("missing() must name absent legs individually, got %q", missing)
	}
	full := w2SyncPutBlockHeadEvidenceState{
		normalFlowRenewsLivenessAndSettles:                   true,
		crossNodeIdempotentRepairSettlesWithoutProcessMemory: true,
		activeGCClaimBlocksHeadFailClosed:                    true,
		definitiveCASLoserCleansOnlyOwnAttempt:               true,
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
