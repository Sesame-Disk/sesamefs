package publication

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pcd1DecisionDocumentPath() string {
	return filepath.Join("..", "..", "docs", "PC-D1-INHERITED-DEPENDENCY-CONTINUITY.md")
}

// TestPCD1DecisionDocumentPinsSingleOwnerAndBoundaries is a source contract,
// not a production coordinator. It prevents the documentation-only decision
// from silently drifting back to an unqualified newly-live claim or to a
// second coordinator/GC owner.
func TestPCD1DecisionDocumentPinsSingleOwnerAndBoundaries(t *testing.T) {
	raw, err := os.ReadFile(pcd1DecisionDocumentPath())
	if err != nil {
		t.Fatalf("read PC-D1 decision: %v", err)
	}
	doc := string(raw)

	if got := strings.Count(doc, "INHERITED CONTINUITY OWNER = CERTIFIED BASELINE FRONTIER"); got != 1 {
		t.Fatalf("PC-D1 owner declaration count = %d, want exactly one", got)
	}
	for _, required := range []string{
		"DECIDED architecture freeze",
		"LogicalPositiveBlockDelta",
		"This gives an induction proof",
		"TestPCD1LogicalPositiveDeltaOmitsUncertifiedInheritedDependencies",
		"Phase5",
		"continuity_certified_head_commit_id",
		"continuity_contract_version",
		"IF head_commit_id = H",
		"head != certified_head",
		"PC-2 may assume:",
		"PC-2 may NOT assume:",
		"GC_ENABLED=false",
		"W2, R31, X1",
	} {
		if !strings.Contains(doc, required) {
			t.Fatalf("PC-D1 decision is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"newly-live is unconditionally complete",
		"INHERITED CONTINUITY OWNER = GC",
		"INHERITED CONTINUITY OWNER = COORDINATOR",
		"GC already protects inherited dependencies",
	} {
		if strings.Contains(doc, forbidden) {
			t.Fatalf("PC-D1 decision contains forbidden claim %q", forbidden)
		}
	}
}

// pcd1WitnessState is a test-only model of the two canonical witness fields.
// It deliberately has no database or publication side effects.
type pcd1WitnessState struct {
	head          string
	certifiedHead string
	contract      string
}

func pcd1ApplyCertification(state *pcd1WitnessState, observedHead, contract string) bool {
	if state.head != observedHead {
		return false
	}
	state.certifiedHead = observedHead
	state.contract = contract
	return true
}

func pcd1WitnessValid(state pcd1WitnessState, contract string) bool {
	return state.head != "" &&
		state.head == state.certifiedHead &&
		state.contract == contract
}

// TestPCD1MovingHeadCannotCertifyObservedHeadAsNewHead proves the critical
// snapshot rule: a certifier that observed H cannot accidentally certify H'
// after another writer advances the canonical HEAD during its walk.
func TestPCD1MovingHeadCannotCertifyObservedHeadAsNewHead(t *testing.T) {
	state := pcd1WitnessState{head: "H"}
	observed := state.head

	// Concurrent writer wins before the certifier's final conditional write.
	state.head = "H'"
	if pcd1ApplyCertification(&state, observed, "V1") {
		t.Fatal("stale certification for H applied after HEAD moved to H'")
	}
	if state.certifiedHead != "" || state.contract != "" {
		t.Fatalf("stale certification mutated witness: %+v", state)
	}
	if pcd1WitnessValid(state, "V1") {
		t.Fatal("uncertified H' reported a valid witness")
	}

	// A fresh observation is the only way to certify the new HEAD.
	if !pcd1ApplyCertification(&state, state.head, "V1") {
		t.Fatal("fresh certification of H' was rejected")
	}
	if state.certifiedHead != "H'" || !pcd1WitnessValid(state, "V1") {
		t.Fatalf("fresh H' certification = %+v, want valid witness", state)
	}
}

func TestPCD1LegacyHeadAdvanceInvalidatesWitness(t *testing.T) {
	state := pcd1WitnessState{head: "H", certifiedHead: "H", contract: "V1"}
	if !pcd1WitnessValid(state, "V1") {
		t.Fatal("seeded baseline witness should be valid")
	}

	// A legacy writer that does not advance the witness is safe because the
	// equality predicate makes the old certificate unusable.
	state.head = "H-legacy"
	if pcd1WitnessValid(state, "V1") {
		t.Fatal("legacy HEAD advance left an apparently valid stale witness")
	}
}
