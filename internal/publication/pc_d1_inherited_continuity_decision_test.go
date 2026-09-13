package publication

import (
	"os"
	"path/filepath"
	"reflect"
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
		"GC-aware baseline handshake",
		"resolve/capture exact P + incarnation",
		"establish durable library-owned liveness",
		"revalidate exact P + incarnation and current GC authority",
		"liveness write cannot revoke",
		"GC-authority interleaving (mandatory baseline rule)",
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

// TestPCD1BaselineHandshakeOrderIsFrozen closes the GC race identified by the
// PC-D1 audit. The HEAD predicate is only the logical frontier check; each
// dependency must first be made live in durable storage and then revalidated
// against the exact physical incarnation and GC authority.
func TestPCD1BaselineHandshakeOrderIsFrozen(t *testing.T) {
	raw, err := os.ReadFile(pcd1DecisionDocumentPath())
	if err != nil {
		t.Fatalf("read PC-D1 decision: %v", err)
	}
	doc := string(raw)
	start := strings.Index(doc, "GC-aware baseline handshake")
	if start < 0 {
		t.Fatal("PC-D1 baseline handshake section is missing")
	}
	end := strings.Index(doc[start:], "3. Commit a durable witness")
	if end < 0 {
		t.Fatal("PC-D1 baseline handshake section is missing")
	}
	section := doc[start : start+end]
	ordered := []string{
		"resolve/capture exact P + incarnation",
		"establish durable library-owned liveness",
		"revalidate exact P + incarnation and current GC authority",
	}
	previous := -1
	for _, token := range ordered {
		at := strings.Index(section, token)
		if at < 0 {
			t.Fatalf("baseline handshake is missing %q", token)
		}
		if at <= previous {
			t.Fatalf("baseline handshake order is not resolve/capture -> durable liveness -> revalidate: %q at %d after %d", token, at, previous)
		}
		previous = at
	}
	if !strings.Contains(section, "A late") || !strings.Contains(section, "liveness write cannot revoke") {
		t.Fatal("baseline handshake does not state that late liveness cannot revoke GC authority")
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

// pcd1BaselineDependency is a test-only model of one physical dependency in
// the baseline walk. P is the exact (storage_class, storage_key) placement;
// incarnation is kept separate here so the model cannot accidentally make a
// logical block id stand in for physical authority.
type pcd1BaselineDependency struct {
	capturedP           string
	capturedIncarnation string
	currentP            string
	currentIncarnation  string
	gcAuthorityWon      bool
	ownLiveness         bool
}

// pcd1CertifyBaselineDependency models the only admissible per-dependency
// order. It has no Cassandra or publication side effects: the liveness write
// is represented by the ownLiveness transition, and the final observation
// rejects an authority already won by GC or a changed physical incarnation.
func pcd1CertifyBaselineDependency(dep *pcd1BaselineDependency, events *[]string) bool {
	*events = append(*events, "resolve/capture exact P + incarnation")
	if dep == nil || dep.capturedP == "" || dep.capturedIncarnation == "" {
		return false
	}

	*events = append(*events, "establish durable library-owned liveness")
	dep.ownLiveness = true

	*events = append(*events, "revalidate exact P + incarnation and current GC authority")
	return dep.ownLiveness &&
		!dep.gcAuthorityWon &&
		dep.currentP == dep.capturedP &&
		dep.currentIncarnation == dep.capturedIncarnation
}

func TestPCD1LateLivenessDoesNotRevokeGCAuthority(t *testing.T) {
	dep := &pcd1BaselineDependency{
		capturedP:           "hot",
		capturedIncarnation: "K1",
		currentP:            "hot",
		currentIncarnation:  "K1",
		// GC's zero-proof already won before the writer's liveness arrived.
		gcAuthorityWon: true,
	}
	var events []string
	if pcd1CertifyBaselineDependency(dep, &events) {
		t.Fatal("late liveness incorrectly certified a dependency after GC won authority")
	}
	if !dep.ownLiveness {
		t.Fatal("test model did not establish the late durable liveness write")
	}
	want := []string{
		"resolve/capture exact P + incarnation",
		"establish durable library-owned liveness",
		"revalidate exact P + incarnation and current GC authority",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("baseline handshake events = %v, want %v", events, want)
	}
}

func TestPCD1BaselineRevalidationRejectsChangedIncarnation(t *testing.T) {
	dep := &pcd1BaselineDependency{
		capturedP:           "hot",
		capturedIncarnation: "K1",
		currentP:            "hot",
		currentIncarnation:  "K2",
	}
	events := []string{}
	if pcd1CertifyBaselineDependency(dep, &events) {
		t.Fatal("baseline certified a changed physical incarnation")
	}
}

func TestPCD1BaselineRevalidationRejectsChangedPlacement(t *testing.T) {
	dep := &pcd1BaselineDependency{
		capturedP:           "hot",
		capturedIncarnation: "K1",
		currentP:            "cold",
		currentIncarnation:  "K1",
	}
	if pcd1CertifyBaselineDependency(dep, &[]string{}) {
		t.Fatal("baseline certified a changed physical placement")
	}
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
