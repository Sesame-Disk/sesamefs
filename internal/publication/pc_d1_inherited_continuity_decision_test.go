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

func pcd1ValidationScriptPath() string {
	return filepath.Join("..", "..", "scripts", "pc-d1-inherited-continuity-validation.sh")
}

func pcd1MutationScriptPath() string {
	return filepath.Join("..", "..", "scripts", "pc-d1-inherited-continuity-mutation-validation.sh")
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
		"Decision development baseline",
		"PR merge baseline",
		"resolve/capture exact physical incarnation P",
		"establish durable library-owned liveness",
		"revalidate exact P + GC authority",
		"non-expiring current-library liveness",
		"bounded-TTL",
		"can never by itself justify the witness",
		"legacy deterministic",
		"rematerialize/migrate",
		"configured global `SERIAL`/LWT quorum",
		"Loss of enough replicas to satisfy that domain blocks certification",
		"global `SERIAL` Paxos domain",
		"LOCAL_SERIAL",
		"ISSUE-LIBRARY-HEAD-SERIAL-DOMAIN-01",
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
		"a remote failure blocks certification",
		"remote failure blocks certification, not already-certified incremental publishes",
		"resolve/capture exact P + incarnation",
		"revalidate exact P + incarnation",
	} {
		if strings.Contains(doc, forbidden) {
			t.Fatalf("PC-D1 decision contains forbidden claim %q", forbidden)
		}
	}
}

func TestPCD1ScriptSignalTrapsFailClosed(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		exitTrap     string
		combinedTrap string
	}{
		{
			name:         "3-DC validation",
			path:         pcd1ValidationScriptPath(),
			exitTrap:     "trap cleanup EXIT",
			combinedTrap: "trap cleanup EXIT INT TERM",
		},
		{
			name:         "mutation validation",
			path:         pcd1MutationScriptPath(),
			exitTrap:     "trap restore EXIT",
			combinedTrap: "trap restore EXIT INT TERM",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatalf("read script: %v", err)
			}
			script := string(raw)
			for _, required := range []string{
				tc.exitTrap,
				"trap 'exit 130' INT",
				"trap 'exit 143' TERM",
			} {
				if !strings.Contains(script, required) {
					t.Fatalf("script is missing fail-closed signal trap %q", required)
				}
			}
			if strings.Contains(script, tc.combinedTrap) {
				t.Fatalf("script still handles signals through the success-preserving EXIT trap %q", tc.combinedTrap)
			}
		})
	}
}

func TestPCD1MutationFailuresRequireSpecificEvidence(t *testing.T) {
	raw, err := os.ReadFile(pcd1MutationScriptPath())
	if err != nil {
		t.Fatalf("read mutation script: %v", err)
	}
	script := string(raw)
	for _, required := range []string{
		`[ -n "$needle" ] || fail`,
		"PC-D1 decision mutations are red (12/12)",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("mutation harness is missing %q", required)
		}
	}
	if strings.Contains(script, "expect_publication_red ''") {
		t.Fatal("mutation harness accepts an arbitrary non-zero test exit as expected RED")
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
		"resolve/capture exact physical incarnation P",
		"establish durable library-owned liveness",
		"revalidate exact P + GC authority",
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
	if !strings.Contains(section, "bounded-TTL") || !strings.Contains(section, "can never by itself justify the witness") {
		t.Fatal("baseline handshake does not limit TTL liveness to a certification bridge")
	}
	if !strings.Contains(section, "non-expiring current-library") {
		t.Fatal("baseline handshake does not require non-expiring current-library liveness")
	}
}

func TestPCD1ValidationCleanupPropagatesFailures(t *testing.T) {
	raw, err := os.ReadFile(pcd1ValidationScriptPath())
	if err != nil {
		t.Fatalf("read PC-D1 validation script: %v", err)
	}
	script := string(raw)
	start := strings.Index(script, "cleanup() {")
	if start < 0 {
		t.Fatal("PC-D1 validation cleanup function is missing")
	}
	end := strings.Index(script[start:], "trap cleanup EXIT")
	if end < 0 {
		t.Fatal("PC-D1 validation cleanup trap is missing")
	}
	cleanup := script[start : start+end]
	for _, required := range []string{
		"cleanup_rc=0",
		"cleanup_rc=1",
		`if [ "$rc" -eq 0 ] && [ "$cleanup_rc" -ne 0 ]`,
		"cleanup failed",
	} {
		if !strings.Contains(cleanup, required) {
			t.Fatalf("PC-D1 cleanup is missing %q", required)
		}
	}
	if strings.Contains(cleanup, "|| true") {
		t.Fatal("PC-D1 cleanup discards a cleanup failure")
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

// pcd1AdvanceCertifiedHead models the inductive HEAD+witness LWT. The
// predecessor must already be certified under the same contract; both HEAD
// fields advance together or the state is left untouched.
func pcd1AdvanceCertifiedHead(state *pcd1WitnessState, observedHead, nextHead, contract string) bool {
	if state == nil || observedHead == "" || nextHead == "" || contract == "" {
		return false
	}
	if state.head != observedHead ||
		state.certifiedHead != observedHead ||
		state.contract != contract {
		return false
	}
	state.head = nextHead
	state.certifiedHead = nextHead
	state.contract = contract
	return true
}

func pcd1WitnessValid(state pcd1WitnessState, contract string) bool {
	return state.head != "" &&
		state.head == state.certifiedHead &&
		state.contract == contract
}

// pcd1BaselineDependency is a test-only model of one physical dependency in
// the baseline walk. P is the exact physical incarnation and placement tuple
// (storage_class, storage_key); a logical block id is deliberately absent.
type pcd1BaselineDependency struct {
	capturedP      string
	currentP       string
	gcAuthorityWon bool
	ownLiveness    bool
}

func TestPCD1CertifiedHeadAdvanceIsAtomicInduction(t *testing.T) {
	state := pcd1WitnessState{head: "H", certifiedHead: "H", contract: "V1"}
	if !pcd1AdvanceCertifiedHead(&state, "H", "H'", "V1") {
		t.Fatal("valid certified frontier advance was rejected")
	}
	want := pcd1WitnessState{head: "H'", certifiedHead: "H'", contract: "V1"}
	if state != want {
		t.Fatalf("certified frontier advance = %+v, want %+v", state, want)
	}
	if !pcd1WitnessValid(state, "V1") {
		t.Fatal("atomic certified frontier advance did not leave a valid witness")
	}
}

func TestPCD1CertifiedHeadAdvanceRejectsInvalidPredecessor(t *testing.T) {
	cases := []struct {
		name  string
		state pcd1WitnessState
		head  string
		want  string
	}{
		{name: "missing predecessor certificate", state: pcd1WitnessState{head: "H", contract: "V1"}, head: "H"},
		{name: "stale predecessor certificate", state: pcd1WitnessState{head: "H", certifiedHead: "H-old", contract: "V1"}, head: "H"},
		{name: "wrong contract version", state: pcd1WitnessState{head: "H", certifiedHead: "H", contract: "V0"}, head: "H"},
		{name: "mismatched predecessor HEAD", state: pcd1WitnessState{head: "H2", certifiedHead: "H", contract: "V1"}, head: "H"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := tc.state
			if pcd1AdvanceCertifiedHead(&tc.state, tc.head, "H-next", "V1") {
				t.Fatalf("%s unexpectedly advanced the frontier", tc.name)
			}
			if tc.state != before {
				t.Fatalf("%s mutated state on rejected advance: before=%+v after=%+v", tc.name, before, tc.state)
			}
		})
	}
}

// pcd1CertifyBaselineDependency models the only admissible per-dependency
// order. It has no Cassandra or publication side effects: the liveness write
// is represented by the ownLiveness transition, and the final observation
// rejects an authority already won by GC or a changed physical incarnation P.
func pcd1CertifyBaselineDependency(dep *pcd1BaselineDependency, events *[]string) bool {
	*events = append(*events, "resolve/capture exact physical incarnation P")
	if dep == nil || dep.capturedP == "" {
		return false
	}

	*events = append(*events, "establish durable library-owned liveness")
	dep.ownLiveness = true

	*events = append(*events, "revalidate exact P + GC authority")
	return dep.ownLiveness &&
		!dep.gcAuthorityWon &&
		dep.currentP == dep.capturedP
}

func TestPCD1LateLivenessDoesNotRevokeGCAuthority(t *testing.T) {
	dep := &pcd1BaselineDependency{
		capturedP: "hot/hash.K1",
		currentP:  "hot/hash.K1",
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
		"resolve/capture exact physical incarnation P",
		"establish durable library-owned liveness",
		"revalidate exact P + GC authority",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("baseline handshake events = %v, want %v", events, want)
	}
}

func TestPCD1BaselineRevalidationRejectsChangedPhysicalP(t *testing.T) {
	dep := &pcd1BaselineDependency{
		capturedP: "hot/hash.K1",
		currentP:  "hot/hash.K2",
	}
	events := []string{}
	if pcd1CertifyBaselineDependency(dep, &events) {
		t.Fatal("baseline certified a changed physical P")
	}
}

func TestPCD1BaselineRevalidationRejectsChangedPlacement(t *testing.T) {
	dep := &pcd1BaselineDependency{
		capturedP: "hot/hash.K1",
		currentP:  "cold/hash.K1",
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
