package db

import (
	"fmt"
	"strings"
	"testing"
)

// PC-D1B.4 certification-window model.
//
// This is an exhaustive interleaving checker, not a simulation of Cassandra.
// It exists to decide, before any runtime change, which fence closes the race
// between the certifier's final revalidation and its witness settlement, and to
// freeze the mutation contract the runtime PR (PC-D1B.5) has to turn RED.
//
// One covered identity F stands for every piece of state a witness for HEAD H
// depends on (the commit row of H, every reachable fs_objects row, and every
// permanent fs:<library>:<fs_id> reference). F is library-scoped in the real
// schema: commits and fs_objects are keyed by library_id and the permanent
// referrer embeds the library, so a destroyer always knows the single library
// row whose witness could depend on what it destroys.
//
// Atomicity follows the real system:
//   - Every step on the `libraries` row that is an LWT (witness CAS, HEAD CAS,
//     destruction intents) is linearized in one global SERIAL Paxos domain; no
//     other Paxos step can run between a CAS's read and its commit.
//   - Plain writes (soft-delete, restore, hard delete, fs_objects deletes and
//     upserts) are not in that domain: they can land between a CAS's read and
//     its commit, and a plain soft-delete can be invisible to the Paxos read
//     (a LOCAL_QUORUM write in another DC).
//   - The certifier's final revalidation re-reads F; a destroyer's delete is
//     visible to that read once acknowledged (EACH_QUORUM delete versus
//     LOCAL_QUORUM read, or LOCAL_QUORUM delete versus EACH_QUORUM read).
//
// The safety invariant is the combination of I1, I2, I3 and I5 from
// docs/PC-D1B-CERTIFICATION-WINDOW-FENCE.md: in every reachable state, a
// witness that a reader would treat as valid for the current HEAD (from the
// merged view or from a stale Paxos view that has not seen a plain
// soft-delete) implies F still holds the certified content A. I4 is checked
// on every CERTIFIED result the certifier returns.

type cwContent uint8

const (
	cwAbsent cwContent = iota
	cwContentA
	cwContentB
)

func (c cwContent) String() string {
	switch c {
	case cwContentA:
		return "A"
	case cwContentB:
		return "B"
	default:
		return "absent"
	}
}

// cwDesign is one candidate fence (or the current runtime), plus the mutation
// switches the runtime contract must turn RED. A zero value is the current
// runtime: no destruction intent, a witness CAS predicating HEAD and deleted_at.
type cwDesign struct {
	name string

	// Destroyer protocol.
	beginBumpsEpoch  bool // intent LWT sets a fresh epoch
	beginClearsWit   bool // intent LWT nulls the stored witness
	beginAddsPending bool // intent LWT adds the destroyer's token
	// pendingByGeneration binds each pending token to the generation of the
	// intent that set it (map token -> generation). A completion clears the
	// token only while its own generation still owns it. Without it the
	// pending entry is a plain set member any completion of that token clears.
	pendingByGeneration bool
	endBumpsEpoch       bool // completion LWT sets a fresh epoch
	endClearsWit        bool // completion LWT nulls the stored witness
	endBeforeDelete     bool // CW-M6: completion is issued before the destroy is acknowledged
	destroyerBypasses   bool // CW-M7: a destroyer skips its intent and completion LWTs

	// Certifier protocol.
	captures              bool // SERIAL capture of epoch/pending before the CAS
	captureRequiresIdle   bool // capture refuses a non-empty pending set
	captureAfterFinal     bool // CW-M5: capture placed after the final revalidation
	captureMayBeStale     bool // capture is a LOCAL_QUORUM read that may miss the latest fence write
	casEpochPredicate     bool // witness CAS predicates epoch == captured
	casPendingPredicate   bool // witness CAS predicates pending == null
	settlementTrustsApply bool // CW-M10: UNKNOWN settles from the CAS's own applied flag

	// Environment assumptions.
	paxosNotLinearized bool // CW-M8: intents use a per-DC serial domain (LOCAL_SERIAL)
	unfencedJanitor    bool // CW-M14: a janitor removes a live destroyer's token without fencing it
	claimsMutable      bool // I6 removed: a writer may re-materialize F with different content
}

var (
	cwCurrentRuntime = cwDesign{name: "current runtime (main@62a2c0e0)"}

	cwBeginEpochOnly = cwDesign{name: "A/B-lite: epoch bumped and witness cleared before destroy",
		beginBumpsEpoch: true, beginClearsWit: true,
		captures: true, casEpochPredicate: true}

	cwEndEpochOnly = cwDesign{name: "E: epoch bumped and witness cleared after destroy",
		endBumpsEpoch: true, endClearsWit: true,
		captures: true, casEpochPredicate: true}

	cwBeginAndEndEpoch = cwDesign{name: "B: epoch bumped and witness cleared before and after destroy",
		beginBumpsEpoch: true, beginClearsWit: true, endBumpsEpoch: true, endClearsWit: true,
		captures: true, casEpochPredicate: true}

	cwPendingOnly = cwDesign{name: "D-lite: pending destruction set without an epoch",
		beginAddsPending: true, beginClearsWit: true,
		captures: true, captureRequiresIdle: true, casPendingPredicate: true}

	// cwSelected is the PC-D1B.4 decision: a per-library destruction epoch plus
	// a pending-destruction set, both written only by global-SERIAL LWTs on the
	// canonical libraries row. The intent LWT sets a fresh epoch, adds its
	// token and clears the witness; the completion LWT removes the token after
	// the destroy is acknowledged. The certifier captures (epoch, pending)
	// before its final revalidation, refuses a busy library, and its witness
	// CAS predicates the captured epoch.
	cwSelected = cwDesign{name: "selected: destruction epoch + generation-owned pending intents (PC-D1B.4)",
		beginBumpsEpoch: true, beginClearsWit: true, beginAddsPending: true, pendingByGeneration: true,
		captures: true, captureRequiresIdle: true, casEpochPredicate: true}
)

type cwScenario struct {
	name        string
	destroyers  int  // concurrent destroyers of F (0..2)
	writer      bool // a supported writer may re-materialize F (same digest, or B when claims are mutable)
	lifecycle   bool // soft-delete (visible or not to Paxos), restore, hard delete
	headMoves   bool // a HEAD writer advances H -> H2
	headRepeats bool // H2 -> H is allowed (assumption probe; not reachable on main)
	ambiguous   bool // the witness CAS may report UNKNOWN whether or not it applied
	// retrySameToken makes destroyer 1 a retry of destroyer 0's destruction
	// unit: same deterministic token, fresh generation, started once destroyer
	// 0 is presumed dead. Destroyer 0 may be a paused process that resumes and
	// issues its completion LWT after the retry's intent. (An UNKNOWN LWT
	// cannot land after a later round: the next Paxos proposer finishes an
	// in-flight proposal first, so it would land before the retry's intent.)
	// Destroyer 0 issues no new destructive write after the retry starts:
	// that is the owner-fencing assumption A-OWN in the ADR.
	retrySameToken bool
}

var cwScenarios = []cwScenario{
	{name: "window/one-destroyer", destroyers: 1, ambiguous: true},
	{name: "window/two-destroyers", destroyers: 2},
	{name: "window/destroyer+writer", destroyers: 1, writer: true},
	{name: "lifecycle/soft-restore-hard", destroyers: 1, lifecycle: true, ambiguous: true},
	{name: "head/moves-and-repeats", destroyers: 1, headMoves: true, headRepeats: true},
	{name: "retry/same-token-stale-completion", destroyers: 2, writer: true, retrySameToken: true},
}

// Certifier program counters.
const (
	cwCertWalk uint8 = iota
	cwCertCaptureEarly
	cwCertFinal
	cwCertCaptureLate
	cwCertCASRead
	cwCertCASCommit
	cwCertSettle
	cwCertDone
)

type cwState struct {
	// Canonical libraries row.
	rowExists     bool
	head          uint8 // 0 = null, 1 = H, 2 = H2
	witness       uint8 // 0 = null, else a HEAD value
	deleted       bool  // merged deleted_at
	deletedPaxos  bool  // deleted_at visible to the Paxos read
	epoch         uint8 // 0 = null; fresh values never repeat
	pending       uint8 // bitmask of pending destruction tokens
	pendingGen    [2]uint8
	destGen       [2]uint8 // generation each destroyer's intent established
	nextGen       uint8
	lateComplete  [2]bool // a crashed destroyer's completion LWT may still land
	prevEpoch     uint8   // fence columns before the latest fence write, for stale captures
	prevPending   uint8
	nextEpoch     uint8
	f             cwContent
	softDeleted   bool
	restored      bool
	hardDeleted   bool
	headAdvanced  bool
	headReturned  bool
	writerDone    bool
	destPC        [2]uint8
	destCrashed   [2]bool
	certPC        uint8
	certCaptured  uint8
	certDecision  bool // the CAS read's predicate outcome
	certApplied   bool
	certCertified bool
}

type cwStep struct {
	label string
	next  cwState
	// certifiedResult marks a step at which the certifier returns CERTIFIED.
	certifiedResult bool
}

func (s cwState) validWitness() bool {
	return s.rowExists && s.head != 0 && s.witness == s.head && !s.deleted
}

// staleValidWitness is what a reader whose Paxos view has not seen a plain
// soft-delete observes (I5).
func (s cwState) staleValidWitness() bool {
	return s.rowExists && s.head != 0 && s.witness == s.head && !s.deletedPaxos
}

func (s cwState) certifiedWitnessIsFalse() bool {
	// The only certified HEAD in the model is H; a witness for H is backed
	// only while F still holds A.
	if s.validWitness() && s.witness == 1 && s.f != cwContentA {
		return true
	}
	return s.staleValidWitness() && s.witness == 1 && s.f != cwContentA
}

func cwDestroyerProgram(d cwDesign) []string {
	if d.destroyerBypasses {
		return []string{"delete"}
	}
	hasBegin := d.beginBumpsEpoch || d.beginClearsWit || d.beginAddsPending
	hasEnd := d.beginAddsPending || d.endBumpsEpoch || d.endClearsWit
	var program []string
	if hasBegin {
		program = append(program, "begin")
	}
	if d.endBeforeDelete && hasEnd {
		program = append(program, "end", "delete")
		return program
	}
	program = append(program, "delete")
	if hasEnd {
		program = append(program, "end")
	}
	return program
}

// cwComplete applies a completion LWT for token by the intent of generation
// gen. With generation ownership it clears only its own entry; a stale
// completion of an earlier attempt of the same destruction unit is a no-op.
func cwComplete(d cwDesign, n *cwState, token int, gen uint8) {
	bit := uint8(1) << token
	if n.pending&bit == 0 {
		return
	}
	if d.pendingByGeneration && n.pendingGen[token] != gen {
		return
	}
	n.pending &^= bit
	n.pendingGen[token] = 0
}

// paxosBusy reports whether a witness CAS is between its read and its commit.
// A linearized serial domain admits no other LWT on the row in that interval.
func (s cwState) paxosBusy(d cwDesign) bool {
	return s.certPC == cwCertCASCommit && !d.paxosNotLinearized
}

func cwSuccessors(d cwDesign, sc cwScenario, s cwState) []cwStep {
	var steps []cwStep
	program := cwDestroyerProgram(d)

	// Certifier.
	if s.certPC != cwCertDone {
		abort := s
		abort.certPC = cwCertDone
		// The certifier's LOCAL_QUORUM reads may miss a plain soft-delete
		// acknowledged in another DC. Using that weaker view lets the
		// certifier proceed in strictly more executions, so every dangerous
		// interleaving of the informed view is still explored.
		readOK := s.rowExists && s.head == 1 && !s.deletedPaxos && s.f == cwContentA
		switch s.certPC {
		case cwCertWalk:
			if readOK {
				n := s
				n.certPC = cwCertCaptureEarly
				steps = append(steps, cwStep{label: "certifier walks H (F=A)", next: n})
			} else {
				steps = append(steps, cwStep{label: "certifier walk fails closed", next: abort})
			}
		case cwCertCaptureEarly, cwCertCaptureLate:
			isLate := s.certPC == cwCertCaptureLate
			n := s
			if isLate {
				n.certPC = cwCertCASRead
			} else {
				n.certPC = cwCertFinal
			}
			if !d.captures || isLate != d.captureAfterFinal {
				steps = append(steps, cwStep{label: "", next: n})
				break
			}
			if s.paxosBusy(d) {
				break
			}
			if d.captureRequiresIdle && s.pending != 0 {
				steps = append(steps, cwStep{label: "certifier capture refuses busy library", next: abort})
				break
			}
			n.certCaptured = s.epoch
			steps = append(steps, cwStep{label: fmt.Sprintf("certifier captures epoch=%d pending=%b", s.epoch, s.pending), next: n})
			if d.captureMayBeStale && (s.prevEpoch != s.epoch || s.prevPending != s.pending) {
				if d.captureRequiresIdle && s.prevPending != 0 {
					steps = append(steps, cwStep{label: "certifier stale capture refuses busy library", next: abort})
					break
				}
				stale := n
				stale.certCaptured = s.prevEpoch
				steps = append(steps, cwStep{label: fmt.Sprintf("certifier stale capture epoch=%d pending=%b", s.prevEpoch, s.prevPending), next: stale})
			}
		case cwCertFinal:
			if readOK {
				n := s
				n.certPC = cwCertCaptureLate
				steps = append(steps, cwStep{label: "certifier final revalidation sees F=A", next: n})
			} else {
				steps = append(steps, cwStep{label: "certifier final revalidation fails closed", next: abort})
			}
		case cwCertCASRead:
			n := s
			n.certPC = cwCertCASCommit
			n.certDecision = s.rowExists && s.head == 1 && !s.deletedPaxos &&
				(!d.casEpochPredicate || s.epoch == s.certCaptured) &&
				(!d.casPendingPredicate || s.pending == 0)
			steps = append(steps, cwStep{label: fmt.Sprintf("witness CAS reads row (applies=%v)", n.certDecision), next: n})
		case cwCertCASCommit:
			n := s
			if s.certDecision {
				// A commit that lands after a plain hard delete leaves witness
				// cells on an otherwise-absent row (see R9 in the ADR).
				n.rowExists = true
				n.witness = 1
				n.certApplied = true
			}
			reported := n
			reported.certPC = cwCertDone
			if n.certApplied {
				reported.certCertified = true
				steps = append(steps, cwStep{label: "witness CAS APPLIED -> CERTIFIED", next: reported, certifiedResult: true})
			} else {
				steps = append(steps, cwStep{label: "witness CAS NOT_APPLIED", next: reported})
			}
			if sc.ambiguous {
				unknown := n
				unknown.certPC = cwCertSettle
				steps = append(steps, cwStep{label: fmt.Sprintf("witness CAS UNKNOWN (applied=%v)", n.certApplied), next: unknown})
			}
		case cwCertSettle:
			if s.paxosBusy(d) {
				break
			}
			n := s
			n.certPC = cwCertDone
			certified := s.validWitness() && s.witness == 1
			if d.settlementTrustsApply {
				certified = s.certApplied
			}
			n.certCertified = certified
			steps = append(steps, cwStep{label: fmt.Sprintf("SERIAL settlement -> certified=%v", certified), next: n, certifiedResult: certified})
		}
	}

	// Destroyers.
	beginAt, endAt := -1, -1
	for index, op := range program {
		switch op {
		case "begin":
			beginAt = index
		case "end":
			endAt = index
		}
	}
	for i := 0; i < sc.destroyers; i++ {
		token := i
		if sc.retrySameToken && i == 1 {
			token = 0
		}
		bit := uint8(1) << token

		if s.lateComplete[i] && s.rowExists && !s.paxosBusy(d) {
			n := s
			n.lateComplete[i] = false
			n.prevEpoch, n.prevPending = s.epoch, s.pending
			cwComplete(d, &n, token, s.destGen[i])
			steps = append(steps, cwStep{label: fmt.Sprintf("destroyer %d stale completion lands late", i), next: n})
		}
		if sc.retrySameToken && i == 1 && !s.destCrashed[0] {
			continue
		}
		if s.destCrashed[i] || int(s.destPC[i]) >= len(program) {
			continue
		}
		crash := s
		crash.destCrashed[i] = true
		began := beginAt >= 0 && int(s.destPC[i]) > beginAt
		ended := endAt >= 0 && int(s.destPC[i]) > endAt
		crash.lateComplete[i] = began && !ended && endAt >= 0
		steps = append(steps, cwStep{label: fmt.Sprintf("destroyer %d crashes", i), next: crash})

		n := s
		n.destPC[i]++
		switch program[s.destPC[i]] {
		case "begin":
			if s.paxosBusy(d) {
				continue
			}
			if n.rowExists {
				n.prevEpoch, n.prevPending = s.epoch, s.pending
				if d.beginBumpsEpoch {
					n.nextEpoch++
					n.epoch = n.nextEpoch
				}
				if d.beginClearsWit {
					n.witness = 0
				}
				n.nextGen++
				n.destGen[i] = n.nextGen
				if d.beginAddsPending {
					n.pending |= bit
					n.pendingGen[token] = n.nextGen
				}
			}
			steps = append(steps, cwStep{label: fmt.Sprintf("destroyer %d intent LWT", i), next: n})
		case "delete":
			n.f = cwAbsent
			steps = append(steps, cwStep{label: fmt.Sprintf("destroyer %d deletes F", i), next: n})
		case "end":
			if s.paxosBusy(d) {
				continue
			}
			if n.rowExists {
				n.prevEpoch, n.prevPending = s.epoch, s.pending
				cwComplete(d, &n, token, s.destGen[i])
				if d.endBumpsEpoch {
					n.nextEpoch++
					n.epoch = n.nextEpoch
				}
				if d.endClearsWit {
					n.witness = 0
				}
			}
			steps = append(steps, cwStep{label: fmt.Sprintf("destroyer %d completion LWT", i), next: n})
		}
	}

	// CW-M14: an operator or DLQ path that drops a pending token without
	// fencing the destroyer that owns it.
	if d.unfencedJanitor && s.pending != 0 && s.rowExists && !s.paxosBusy(d) {
		n := s
		n.prevEpoch, n.prevPending = s.epoch, s.pending
		n.pending = 0
		n.pendingGen = [2]uint8{}
		steps = append(steps, cwStep{label: "janitor drops pending tokens without fencing their destroyers", next: n})
	}

	// Supported writer: re-materializes F through the identity gateway. With
	// immutable claims only the claimed content A can be written again.
	if sc.writer && !s.writerDone {
		if s.f == cwAbsent {
			n := s
			n.f = cwContentA
			n.writerDone = true
			steps = append(steps, cwStep{label: "writer re-creates F with the claimed digest A", next: n})
		}
		if d.claimsMutable && s.f != cwContentB {
			n := s
			n.f = cwContentB
			n.writerDone = true
			steps = append(steps, cwStep{label: "writer re-creates F with different content B", next: n})
		}
	}

	// Library lifecycle: plain writes on the canonical row, as in production.
	if sc.lifecycle {
		if !s.softDeleted && s.rowExists && s.head != 0 {
			for _, visible := range []bool{true, false} {
				n := s
				n.softDeleted = true
				n.deleted = true
				n.deletedPaxos = visible
				steps = append(steps, cwStep{label: fmt.Sprintf("soft-delete (visible to Paxos=%v)", visible), next: n})
			}
		}
		if s.deleted && !s.deletedPaxos {
			n := s
			n.deletedPaxos = true
			steps = append(steps, cwStep{label: "soft-delete propagates", next: n})
		}
		if s.deleted && s.deletedPaxos && !s.restored && s.rowExists {
			n := s
			n.restored = true
			n.deleted = false
			n.deletedPaxos = false
			steps = append(steps, cwStep{label: "restore clears deleted_at", next: n})
		}
		if !s.hardDeleted && s.rowExists && s.deleted {
			n := s
			n.hardDeleted = true
			n.rowExists = false
			n.head = 0
			n.witness = 0
			n.deleted = false
			n.deletedPaxos = false
			n.epoch = 0
			n.pending = 0
			n.pendingGen = [2]uint8{}
			n.prevEpoch, n.prevPending = 0, 0
			steps = append(steps, cwStep{label: "hard delete removes the canonical row", next: n})
		}
	}

	// HEAD writers (LWTs in the same domain).
	if sc.headMoves && !s.paxosBusy(d) {
		if !s.headAdvanced && s.rowExists && s.head == 1 {
			n := s
			n.headAdvanced = true
			n.head = 2
			steps = append(steps, cwStep{label: "HEAD CAS H -> H2", next: n})
		}
		if sc.headRepeats && s.headAdvanced && !s.headReturned && s.rowExists && s.head == 2 {
			n := s
			n.headReturned = true
			n.head = 1
			steps = append(steps, cwStep{label: "HEAD CAS H2 -> H (repeat)", next: n})
		}
	}
	return steps
}

type cwResult struct {
	violation      bool
	trace          []string
	states         int
	certifiedPaths bool // at least one execution returned CERTIFIED
	busyRefusals   bool // at least one execution refused a busy library
}

func cwInitialState() cwState {
	return cwState{rowExists: true, head: 1, f: cwContentA}
}

func cwExplore(d cwDesign, sc cwScenario) cwResult {
	var result cwResult
	visited := map[cwState]bool{}
	var trace []string
	var dfs func(cwState) bool
	dfs = func(s cwState) bool {
		if visited[s] {
			return false
		}
		visited[s] = true
		result.states++
		if s.certifiedWitnessIsFalse() {
			result.violation = true
			result.trace = append([]string(nil), trace...)
			result.trace = append(result.trace, "=> a reader treats the witness for H as valid while F is "+s.f.String())
			return true
		}
		for _, step := range cwSuccessors(d, sc, s) {
			if step.certifiedResult {
				result.certifiedPaths = true
				if !(step.next.witness == 1 && step.next.f == cwContentA) {
					result.violation = true
					result.trace = append(append([]string(nil), trace...), step.label,
						fmt.Sprintf("=> the certifier returned CERTIFIED with stored witness=%d and F=%s", step.next.witness, step.next.f))
					return true
				}
			}
			if strings.Contains(step.label, "capture refuses") {
				result.busyRefusals = true
			}
			if step.label != "" {
				trace = append(trace, step.label)
			}
			if dfs(step.next) {
				return true
			}
			if step.label != "" {
				trace = trace[:len(trace)-1]
			}
		}
		return false
	}
	dfs(cwInitialState())
	return result
}

func cwFirstViolation(d cwDesign) (string, cwResult, bool) {
	for _, sc := range cwScenarios {
		if result := cwExplore(d, sc); result.violation {
			return sc.name, result, true
		}
	}
	return "", cwResult{}, false
}

// The current runtime lets a witness settle after a covered identity is
// deleted inside the window: the witness CAS predicates only HEAD and
// deleted_at on the libraries row. This is the expected-current behavior; it
// must stay reachable in this model until the runtime fence lands.
func TestPCD1B4ModelCurrentRuntimeAdmitsFalseWitness(t *testing.T) {
	result := cwExplore(cwCurrentRuntime, cwScenario{name: "window/one-destroyer", destroyers: 1})
	if !result.violation {
		t.Fatalf("PC-D1B.4 MODEL: the current runtime no longer admits a witness born after an in-window delete; the model has drifted from main")
	}
	joined := strings.Join(result.trace, " | ")
	for _, want := range []string{"certifier final revalidation sees F=A", "destroyer 0 deletes F", "witness CAS"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("PC-D1B.4 MODEL: current-runtime counterexample %q does not contain %q", joined, want)
		}
	}
	t.Logf("current runtime counterexample: %s", joined)
}

// Each rejected candidate has a concrete counterexample. The ADR's
// rejected-alternatives section is backed by these traces, not by intuition.
func TestPCD1B4ModelRejectsWeakerFences(t *testing.T) {
	for _, design := range []cwDesign{cwBeginEpochOnly, cwEndEpochOnly, cwBeginAndEndEpoch, cwPendingOnly} {
		design := design
		t.Run(design.name, func(t *testing.T) {
			scenario, result, found := cwFirstViolation(design)
			if !found {
				t.Fatalf("PC-D1B.4 MODEL: %s has no counterexample; the rejection recorded in the ADR is no longer justified", design.name)
			}
			t.Logf("%s counterexample (%s): %s", design.name, scenario, strings.Join(result.trace, " | "))
		})
	}
}

// The selected fence holds every invariant in every scenario, including
// destroyer crashes, two concurrent destroyers, supported re-materialization,
// plain soft-delete invisible to Paxos, restore, hard delete racing the CAS
// commit, HEAD movement (and even a hypothetical HEAD repeat), and UNKNOWN
// witness settlement. It is also not vacuous: certification still succeeds
// in some executions, and a crashed destroyer blocks certification rather
// than weakening it.
func TestPCD1B4ModelSelectedFenceHoldsInvariants(t *testing.T) {
	sawCertified, sawBusy := false, false
	for _, sc := range cwScenarios {
		result := cwExplore(cwSelected, sc)
		if result.violation {
			t.Fatalf("PC-D1B.4 MODEL: selected fence violated in %s: %s", sc.name, strings.Join(result.trace, " | "))
		}
		sawCertified = sawCertified || result.certifiedPaths
		sawBusy = sawBusy || result.busyRefusals
		t.Logf("%s: %d states explored, certified=%v busy-refusal=%v", sc.name, result.states, result.certifiedPaths, result.busyRefusals)
	}
	if !sawCertified {
		t.Fatal("PC-D1B.4 MODEL: the selected fence never certifies; the model is vacuous")
	}
	if !sawBusy {
		t.Fatal("PC-D1B.4 MODEL: no execution reaches a pending destruction at capture; the busy path is untested")
	}
	staleCapture := cwSelected
	staleCapture.name = "selected with a LOCAL_QUORUM capture"
	staleCapture.captureMayBeStale = true
	if scenario, result, found := cwFirstViolation(staleCapture); found {
		t.Fatalf("PC-D1B.4 MODEL: a stale capture must cost liveness only, but violated %s: %s", scenario, strings.Join(result.trace, " | "))
	}
	quiet := cwExplore(cwSelected, cwScenario{name: "no-destroyer", lifecycle: true, headMoves: true, ambiguous: true})
	if quiet.violation || !quiet.certifiedPaths {
		t.Fatalf("PC-D1B.4 MODEL: without destroyers the selected fence must certify and stay safe: %+v", quiet)
	}
}

// Library lifecycle needs no new mechanism: soft-delete, restore and hard
// delete stay plain writes in the selected design and the invariants hold,
// because soft-delete/restore do not change the certified dependency set and
// every destroyer that acts during trash clears the witness through its intent.
func TestPCD1B4ModelLifecycleNeedsNoRestoreEpoch(t *testing.T) {
	result := cwExplore(cwSelected, cwScenario{name: "lifecycle-only", destroyers: 1, lifecycle: true, ambiguous: true})
	if result.violation {
		t.Fatalf("PC-D1B.4 MODEL: plain lifecycle writes break the selected fence: %s", strings.Join(result.trace, " | "))
	}
	current := cwExplore(cwCurrentRuntime, cwScenario{name: "lifecycle-only", destroyers: 1, lifecycle: true})
	if !current.violation {
		t.Fatal("PC-D1B.4 MODEL: the current runtime is expected to revive a false witness across trash/restore")
	}
}

// The mutation contract for the runtime PR. Every switch is a way the
// implementation can weaken the selected fence; each must produce a concrete
// counterexample here, and the runtime PR must show the corresponding
// mutation RED against real code (docs/PC-D1B-CERTIFICATION-WINDOW-FENCE.md,
// "Mutation contract").
func TestPCD1B4ModelMutationContract(t *testing.T) {
	mutate := func(name string, change func(*cwDesign)) cwDesign {
		d := cwSelected
		d.name = name
		change(&d)
		return d
	}
	mutations := []cwDesign{
		mutate("CW-M1 witness CAS omits the epoch predicate", func(d *cwDesign) { d.casEpochPredicate = false }),
		mutate("CW-M2 intent does not set a fresh epoch", func(d *cwDesign) { d.beginBumpsEpoch = false }),
		mutate("CW-M3 intent does not clear the stored witness", func(d *cwDesign) { d.beginClearsWit = false }),
		mutate("CW-M4 capture ignores pending intents", func(d *cwDesign) { d.captureRequiresIdle = false }),
		mutate("CW-M5 capture placed after the final revalidation", func(d *cwDesign) { d.captureAfterFinal = true }),
		mutate("CW-M6 completion issued before the destroy is acknowledged", func(d *cwDesign) { d.endBeforeDelete = true }),
		mutate("CW-M7 a destroyer bypasses the intent", func(d *cwDesign) { d.destroyerBypasses = true }),
		mutate("CW-M8 intents are not linearized with the witness CAS (LOCAL_SERIAL)", func(d *cwDesign) { d.paxosNotLinearized = true }),
		mutate("CW-M10 UNKNOWN settles from the CAS applied flag", func(d *cwDesign) { d.settlementTrustsApply = true }),
		mutate("CW-M14 a janitor drops a live destroyer's token", func(d *cwDesign) { d.unfencedJanitor = true }),
		mutate("CW-M15 identity claims become mutable (I6 removed)", func(d *cwDesign) { d.claimsMutable = true }),
		mutate("CW-M16 completion clears the token without checking its generation", func(d *cwDesign) { d.pendingByGeneration = false }),
	}
	for _, mutation := range mutations {
		mutation := mutation
		t.Run(mutation.name, func(t *testing.T) {
			scenario, result, found := cwFirstViolation(mutation)
			if !found {
				t.Fatalf("PC-D1B.4 MODEL: %s is not caught; the mutation contract would not turn RED", mutation.name)
			}
			t.Logf("RED in %s: %s", scenario, strings.Join(result.trace, " | "))
		})
	}
}

// cwReplay drives one exact interleaving: each wanted label must be the
// prefix of an enabled step's label (an empty want follows the certifier's
// silent step). It returns the final state and whether a CERTIFIED result
// was returned along the way.
func cwReplay(t *testing.T, d cwDesign, sc cwScenario, wants []string) (cwState, bool) {
	t.Helper()
	state, certified := cwInitialState(), false
	for _, want := range wants {
		found := false
		var labels []string
		for _, step := range cwSuccessors(d, sc, state) {
			labels = append(labels, step.label)
			if (want == "" && step.label == "") || (want != "" && strings.HasPrefix(step.label, want)) {
				state, found = step.next, true
				certified = certified || step.certifiedResult
				break
			}
		}
		if !found {
			t.Fatalf("PC-D1B.4 MODEL: step %q is not enabled for %s; enabled: %q", want, d.name, labels)
		}
	}
	return state, certified
}

// The audit's same-token retry trace: attempt G1 intends and deletes, a
// supported writer re-materializes the claimed identity, retry G2 reuses the
// token, G1's completion lands late, G2 deletes again, and the certifier
// runs across that delete. With generation-owned tokens the stale completion
// is a no-op and the certifier refuses the busy library; with a plain token
// set the stale completion clears G2's protection and a false witness
// settles. The exhaustive search must agree with both replays.
func TestPCD1B4ModelStaleCompletionCannotClearRetry(t *testing.T) {
	scenario := cwScenario{name: "retry/same-token-stale-completion", destroyers: 2, writer: true, retrySameToken: true}
	prefix := []string{
		"destroyer 0 intent LWT", "destroyer 0 deletes F", "destroyer 0 crashes",
		"writer re-creates F with the claimed digest A", "destroyer 1 intent LWT",
		"destroyer 0 stale completion lands late", "certifier walks H (F=A)",
	}

	state, _ := cwReplay(t, cwSelected, scenario, append(append([]string(nil), prefix...), "certifier capture refuses busy library"))
	if state.pending == 0 || state.certPC != cwCertDone || state.witness != 0 {
		t.Fatalf("PC-D1B.4 MODEL: G2 must still own the pending token and the certifier must stop without a witness: %+v", state)
	}
	if result := cwExplore(cwSelected, scenario); result.violation {
		t.Fatalf("PC-D1B.4 MODEL: generation-owned completion violated: %s", strings.Join(result.trace, " | "))
	}

	setOnly := cwSelected
	setOnly.name = "token set without generation"
	setOnly.pendingByGeneration = false
	state, certified := cwReplay(t, setOnly, scenario, append(append([]string(nil), prefix...),
		"certifier captures", "certifier final revalidation sees F=A", "", "witness CAS reads row (applies=true)",
		"destroyer 1 deletes F", "witness CAS APPLIED"))
	if !certified || !state.validWitness() || state.f != cwAbsent {
		t.Fatalf("PC-D1B.4 MODEL: the token-set replay must end with a valid witness over a deleted F: %+v", state)
	}
	result := cwExplore(setOnly, scenario)
	if !result.violation {
		t.Fatal("PC-D1B.4 MODEL: exhaustive search must also find the token-set violation")
	}
	joined := strings.Join(result.trace, " | ")
	for _, want := range []string{"destroyer 1 intent LWT", "destroyer 0 stale completion lands late", "destroyer 1 deletes F"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("PC-D1B.4 MODEL: stale-completion counterexample %q lacks %q", joined, want)
		}
	}
	t.Logf("token-set minimal counterexample: %s", joined)
}
