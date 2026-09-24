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
	// tombstoneAtGeneration writes every destructive mutation USING TIMESTAMP
	// equal to its generation's time instead of "now", so a destroyer can only
	// shadow versions written at or before its own generation.
	tombstoneAtGeneration bool
	// recordsSuperseded makes an intent that takes over a token owned by an
	// older generation raise the superseded high-water mark S in the same
	// LWT. A superseded generation may still be a paused process.
	recordsSuperseded bool
	// certifierReaffirms makes the certifier globally rewrite every covered
	// row whenever captured S is non-null, regardless of locally observed
	// write time, so a local timestamp is never mistaken for global proof.
	certifierReaffirms bool
	endBumpsEpoch      bool // completion LWT sets a fresh epoch
	endClearsWit       bool // completion LWT nulls the stored witness
	endBeforeDelete    bool // CW-M6: completion is issued before the destroy is acknowledged
	destroyerBypasses  bool // CW-M7: a destroyer skips its intent and completion LWTs

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
	cwSelected = cwDesign{name: "selected: destruction epoch + generation-owned pending intents + generation-fenced tombstones (PC-D1B.4)",
		beginBumpsEpoch: true, beginClearsWit: true, beginAddsPending: true, pendingByGeneration: true,
		tombstoneAtGeneration: true, recordsSuperseded: true, certifierReaffirms: true,
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
	// A paused destroyer may also resume and issue its destructive write:
	// the model makes no owner-fencing assumption.
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
	rowExists      bool
	head           uint8 // 0 = null, 1 = H, 2 = H2
	witness        uint8 // 0 = null, else a HEAD value
	deleted        bool  // merged deleted_at
	deletedPaxos   bool  // deleted_at visible to the Paxos read
	epoch          uint8 // 0 = null; fresh values never repeat
	pending        uint8 // bitmask of pending destruction tokens
	pendingGen     [2]uint8
	destGen        [2]uint8 // generation each destroyer's intent established
	nextGen        uint8
	lateComplete   [2]bool // a crashed destroyer's completion LWT may still land
	prevEpoch      uint8   // fence columns before the latest fence write, for stale captures
	prevPending    uint8
	nextEpoch      uint8
	f              cwContent // content of the newest write of F
	writeTs        uint8     // write timestamp of that version
	tombTs         uint8     // newest tombstone timestamp (0 = none)
	superseded     uint8     // S: highest generation replaced before completing
	prevSuperseded uint8
	certS          uint8 // S captured by the certifier
	destTs         [2]uint8
	lateDelete     [2]bool // a paused destroyer may still issue its destructive write
	softDeleted    bool
	restored       bool
	hardDeleted    bool
	headAdvanced   bool
	headReturned   bool
	writerDone     bool
	destPC         [2]uint8
	destCrashed    [2]bool
	certPC         uint8
	certCaptured   uint8
	certDecision   bool // the CAS read's predicate outcome
	certApplied    bool
	certCertified  bool
}

type cwStep struct {
	label string
	next  cwState
	// certifiedResult marks a step at which the certifier returns CERTIFIED.
	certifiedResult bool
}

// visibleF is what a read of F returns: Cassandra last-write-wins, with a
// tombstone winning a timestamp tie.
func (s cwState) visibleF() cwContent {
	if s.f == cwAbsent || s.writeTs <= s.tombTs {
		return cwAbsent
	}
	return s.f
}

func (s cwState) maxTs() uint8 {
	m := s.writeTs
	for _, v := range []uint8{s.tombTs, s.epoch, s.nextEpoch} {
		if v > m {
			m = v
		}
	}
	return m
}

func cwTombstone(d cwDesign, n *cwState, ts uint8) {
	// A destroyer without an intent has no generation to bound its tombstone.
	if !d.tombstoneAtGeneration || ts == 0 {
		// "now" when the write is issued: later than every earlier write.
		ts = n.maxTs() + 1
	}
	if ts > n.tombTs {
		n.tombTs = ts
	}
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
	if s.validWitness() && s.witness == 1 && s.visibleF() != cwContentA {
		return true
	}
	return s.staleValidWitness() && s.witness == 1 && s.visibleF() != cwContentA
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

// cwOwnerOf returns the destroyer whose generation currently owns token.
func cwOwnerOf(s cwState, token int) int {
	for i := range s.destGen {
		if s.destGen[i] != 0 && s.destGen[i] == s.pendingGen[token] {
			return i
		}
	}
	return 0
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
		readOK := s.rowExists && s.head == 1 && !s.deletedPaxos && s.visibleF() == cwContentA
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
			n.certS = s.superseded
			steps = append(steps, cwStep{label: fmt.Sprintf("certifier captures epoch=%d pending=%b", s.epoch, s.pending), next: n})
			if d.captureMayBeStale && (s.prevEpoch != s.epoch || s.prevPending != s.pending) {
				if d.captureRequiresIdle && s.prevPending != 0 {
					steps = append(steps, cwStep{label: "certifier stale capture refuses busy library", next: abort})
					break
				}
				stale := n
				stale.certCaptured = s.prevEpoch
				stale.certS = s.prevSuperseded
				steps = append(steps, cwStep{label: fmt.Sprintf("certifier stale capture epoch=%d pending=%b", s.prevEpoch, s.prevPending), next: stale})
			}
		case cwCertFinal:
			if readOK {
				n := s
				n.certPC = cwCertCaptureLate
				label := "certifier final revalidation sees F=A"
				if d.certifierReaffirms && s.certS != 0 {
					// A successful globally acknowledged write cannot lower an
					// already newer local cell; other DCs may still need the S+1
					// mutation, which the separate replica model represents.
					reaffirmTs := s.certS + 1
					if n.writeTs < reaffirmTs {
						n.writeTs = reaffirmTs
					}
					label += fmt.Sprintf(" and globally reaffirms it at ts=%d", reaffirmTs)
				}
				steps = append(steps, cwStep{label: label, next: n})
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

		// A paused process resumes its remaining steps in program order: its
		// destructive write (or its decision not to write) precedes its
		// completion.
		if s.lateComplete[i] && !s.lateDelete[i] && s.rowExists && !s.paxosBusy(d) {
			n := s
			n.lateComplete[i] = false
			n.prevEpoch, n.prevPending = s.epoch, s.pending
			cwComplete(d, &n, token, s.destGen[i])
			steps = append(steps, cwStep{label: fmt.Sprintf("destroyer %d stale completion lands late", i), next: n})
		}
		if s.lateDelete[i] {
			n := s
			n.lateDelete[i] = false
			cwTombstone(d, &n, s.destTs[i])
			steps = append(steps, cwStep{label: fmt.Sprintf("paused destroyer %d resumes and issues its stale destructive write", i), next: n})
			skip := s
			skip.lateDelete[i] = false
			steps = append(steps, cwStep{label: fmt.Sprintf("paused destroyer %d resumes and does not write", i), next: skip})
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
		deleteAt := -1
		for index, op := range program {
			if op == "delete" {
				deleteAt = index
			}
		}
		crash.lateDelete[i] = began && deleteAt >= 0 && int(s.destPC[i]) <= deleteAt
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
				n.prevSuperseded = s.superseded
				n.nextGen++
				n.destGen[i] = n.nextGen
				n.destTs[i] = n.epoch
				if !d.beginBumpsEpoch {
					n.destTs[i] = n.nextGen
				}
				if d.beginAddsPending {
					if old := s.pendingGen[token]; d.recordsSuperseded && s.pending&bit != 0 && old != n.nextGen {
						// The replaced owner's generation value is its epoch.
						if ts := s.destTs[cwOwnerOf(s, token)]; ts > n.superseded {
							n.superseded = ts
						}
					}
					n.pending |= bit
					n.pendingGen[token] = n.nextGen
				}
			}
			steps = append(steps, cwStep{label: fmt.Sprintf("destroyer %d intent LWT", i), next: n})
		case "delete":
			cwTombstone(d, &n, s.destTs[i])
			steps = append(steps, cwStep{label: fmt.Sprintf("destroyer %d deletes F", i), next: n})
			// A retry may re-evaluate and decide F must stay.
			keep := s
			keep.destPC[i]++
			steps = append(steps, cwStep{label: fmt.Sprintf("destroyer %d decides not to delete F", i), next: keep})
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
		if s.visibleF() == cwAbsent {
			// A normal clock writes after everything; a skewed one in the past.
			for _, ts := range []uint8{s.maxTs() + 1, 1} {
				n := s
				n.f = cwContentA
				n.writeTs = ts
				n.writerDone = true
				steps = append(steps, cwStep{label: fmt.Sprintf("writer re-creates F with the claimed digest A at ts=%d", ts), next: n})
			}
		}
		if d.claimsMutable && s.visibleF() != cwContentB {
			n := s
			n.f = cwContentB
			n.writeTs = s.maxTs() + 1
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
	return cwState{rowExists: true, head: 1, f: cwContentA, writeTs: 1}
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
			result.trace = append(result.trace, "=> a reader treats the witness for H as valid while F is "+s.visibleF().String())
			return true
		}
		for _, step := range cwSuccessors(d, sc, s) {
			if step.certifiedResult {
				result.certifiedPaths = true
				if !(step.next.witness == 1 && step.next.visibleF() == cwContentA) {
					result.violation = true
					result.trace = append(append([]string(nil), trace...), step.label,
						fmt.Sprintf("=> the certifier returned CERTIFIED with stored witness=%d and F=%s", step.next.witness, step.next.visibleF()))
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
		mutate("CW-M19 destructive writes use a current timestamp instead of their generation's", func(d *cwDesign) { d.tombstoneAtGeneration = false }),
		mutate("CW-M20 certifier does not reaffirm covered state above the superseded high-water mark", func(d *cwDesign) { d.certifierReaffirms = false }),
		mutate("CW-M21 a takeover does not record the superseded generation", func(d *cwDesign) { d.recordsSuperseded = false }),
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
	if !certified || !state.validWitness() || state.visibleF() != cwAbsent {
		t.Fatalf("PC-D1B.4 MODEL: the token-set replay must end with a valid witness over a deleted F: %+v", state)
	}
	result := cwExplore(setOnly, scenario)
	if !result.violation {
		t.Fatal("PC-D1B.4 MODEL: exhaustive search must also find the token-set violation")
	}
	joined := strings.Join(result.trace, " | ")
	for _, want := range []string{"destroyer 1 intent LWT", "destroyer 0 stale completion lands late"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("PC-D1B.4 MODEL: stale-completion counterexample %q lacks %q", joined, want)
		}
	}
	t.Logf("token-set minimal counterexample: %s", joined)
}

// A paused generation that loses ownership must not make a destructive write
// effective afterwards. Two replays, with no owner-fencing assumption:
//
//	audit trace: G1 intends, passes its fence and pauses; G2 takes over,
//	destroys and completes; a writer re-materializes F; the certifier
//	captures and revalidates; G1 resumes and issues its old delete.
//
//	stronger trace: G2 takes over but decides F must stay, so nothing is
//	re-materialized; G1 resumes and deletes the original version, which
//	existed before G1 lost ownership.
//
// Generation-fenced tombstones make the first harmless (G1's tombstone is
// older than the re-materialization); certifier reaffirmation above the
// superseded high-water mark makes the second harmless (G1's tombstone is
// older than the reaffirmed version). CW-M19 and CW-M20 each break one.
func TestPCD1B4ModelStaleGenerationCannotDestroyLate(t *testing.T) {
	scenario := cwScenario{name: "retry/same-token-stale-completion", destroyers: 2, writer: true, retrySameToken: true}
	certifyAcrossStaleWrite := []string{
		"certifier walks H (F=A)", "certifier captures", "certifier final revalidation sees F=A", "",
		"witness CAS reads row (applies=true)",
		"paused destroyer 0 resumes and issues its stale destructive write",
		"witness CAS APPLIED",
	}
	audit := append([]string{
		"destroyer 0 intent LWT", "destroyer 0 crashes", "destroyer 1 intent LWT",
		"destroyer 1 deletes F", "destroyer 1 completion LWT",
		"writer re-creates F with the claimed digest A at ts=",
	}, certifyAcrossStaleWrite...)
	stronger := append([]string{
		"destroyer 0 intent LWT", "destroyer 0 crashes", "destroyer 1 intent LWT",
		"destroyer 1 decides not to delete F", "destroyer 1 completion LWT",
	}, certifyAcrossStaleWrite...)

	check := func(name string, d cwDesign, trace []string, wantSafe bool) {
		t.Helper()
		state, certified := cwReplay(t, d, scenario, trace)
		safe := !(certified && state.validWitness() && state.visibleF() != cwContentA)
		if safe != wantSafe {
			t.Fatalf("PC-D1B.4 MODEL: %s under %q: safe=%v, want %v (F=%s witness valid=%v)", name, d.name, safe, wantSafe, state.visibleF(), state.validWitness())
		}
	}
	noFencedTombstones := cwSelected
	noFencedTombstones.name = "CW-M19"
	noFencedTombstones.tombstoneAtGeneration = false
	noReaffirm := cwSelected
	noReaffirm.name = "CW-M20"
	noReaffirm.certifierReaffirms = false

	check("audit stale destructive write", cwSelected, audit, true)
	check("audit stale destructive write", noFencedTombstones, audit, false)
	check("stale write against the original version", cwSelected, stronger, true)
	check("stale write against the original version", noReaffirm, stronger, false)
}

// CW-M27: one DC may retain the partial application of an EACH_QUORUM write
// that returned UNKNOWN. That local timestamp is not proof that the other DCs
// received the projection; a retry must repeat EACH_QUORUM even if its local
// WRITETIME is already above S.
type cwReplicaCell struct {
	writeTs int64
	tombTs  int64
	content cwContent
}

func (c cwReplicaCell) visible() bool {
	return c.content != cwAbsent && c.writeTs > c.tombTs
}

func (c *cwReplicaCell) reaffirm(ts int64) {
	if ts > c.writeTs {
		c.writeTs = ts
		c.content = cwContentA
	}
}

func (c *cwReplicaCell) delete(ts int64) {
	if ts > c.tombTs {
		c.tombTs = ts
	}
}

func TestPCD1B4ModelUnknownReaffirmationRetryCannotTrustLocalTimestamp(t *testing.T) {
	const (
		t0       = int64(10)
		s        = int64(20)
		reaffirm = s + 1
	)
	initial := map[string]*cwReplicaCell{
		"dc-na":   {writeTs: t0, content: cwContentA},
		"dc-eu":   {writeTs: t0, content: cwContentA},
		"dc-asia": {writeTs: t0, content: cwContentA},
	}

	// First EACH_QUORUM attempt applies in dc-na, then returns UNKNOWN because
	// the other DCs are unavailable. Retry observes WRITETIME>S locally.
	partial := make(map[string]*cwReplicaCell, len(initial))
	for dc, cell := range initial {
		cloned := *cell
		partial[dc] = &cloned
	}
	partial["dc-na"].reaffirm(reaffirm)
	if partial["dc-na"].writeTs <= s {
		t.Fatal("CW-M27 precondition: the ambiguous local application must leave WRITETIME>S")
	}

	applyRetry := func(skipOnLocalHighTimestamp bool) map[string]*cwReplicaCell {
		retried := make(map[string]*cwReplicaCell, len(partial))
		for dc, cell := range partial {
			cloned := *cell
			retried[dc] = &cloned
		}
		if !skipOnLocalHighTimestamp || retried["dc-na"].writeTs <= s {
			// Successful EACH_QUORUM retry acknowledges all three DCs.
			for _, dc := range []string{"dc-na", "dc-eu", "dc-asia"} {
				retried[dc].reaffirm(reaffirm)
			}
		}
		// A stale generation's tombstone is later delivered with EACH_QUORUM.
		for _, dc := range []string{"dc-na", "dc-eu", "dc-asia"} {
			retried[dc].delete(s)
		}
		return retried
	}

	for _, dc := range []string{"dc-na", "dc-eu", "dc-asia"} {
		if !applyRetry(false)[dc].visible() {
			t.Fatalf("CW-M27 correct retry: certified identity missing in %s after EACH_QUORUM reaffirmation", dc)
		}
	}
	mutated := applyRetry(true) // RED mutation: local WRITETIME>S skips the retry.
	for _, dc := range []string{"dc-na", "dc-eu", "dc-asia"} {
		if mutated[dc].visible() != (dc == "dc-na") {
			t.Fatalf("CW-M27 mutation should leave the identity only in dc-na; %s visible=%v", dc, mutated[dc].visible())
		}
	}
}

// CW-M28: a future row tombstone can outlive a normal successful materializer
// write under Cassandra LWW. Progress is allowed only when a generation above
// both E and W can be minted at or before wall clock; otherwise destruction is
// postponed.
func TestPCD1B4ModelFutureTombstoneCannotBeMinted(t *testing.T) {
	canMint := func(now, epoch, targetW int64) bool {
		return max(epoch, targetW) < now
	}
	const (
		now      = int64(100)
		epoch    = int64(99)
		writeW   = int64(104)
		futureGC = int64(105)
		writerTs = int64(101)
	)
	if canMint(now, epoch, writeW) {
		t.Fatal("CW-M28: a target WRITETIME ahead of wall clock must postpone destruction")
	}
	// Cassandra LWW characterization: the contract's rejected future DELETE
	// timestamp hides a later normal writer that returned success.
	cell := cwReplicaCell{writeTs: writeW, content: cwContentA}
	cell.delete(futureGC)
	cell.reaffirm(writerTs)
	if cell.visible() {
		t.Fatal("CW-M28 precondition: normal writer should be hidden by the future row tombstone")
	}
	if !canMint(writeW+1, epoch, writeW) {
		t.Fatal("CW-M28 liveness: after wall clock safely passes W and E, a non-future generation can be minted")
	}
}

type cwClockSafety struct {
	gcNow, slowWriterNow int64
	maxPairwiseSkew      int64
	observedPairwiseSkew int64
	timestampGuard       int64
	epoch, targetW       int64
	healthy, monotonic   bool
}

func (c cwClockSafety) timestampAuthoritiesMayWrite() bool {
	return c.healthy && c.monotonic && c.maxPairwiseSkew >= 0 && c.observedPairwiseSkew <= c.maxPairwiseSkew
}

func (c cwClockSafety) mintSafeGeneration() (int64, bool) {
	if !c.timestampAuthoritiesMayWrite() || c.timestampGuard < 1 {
		return 0, false
	}
	safeNow := c.gcNow - c.maxPairwiseSkew - c.timestampGuard
	floor := max(c.epoch, c.targetW)
	if floor >= safeNow {
		return 0, false
	}
	return floor + 1, true
}

// CW-M29: a GC-local timestamp can be behind local now but ahead of a slow
// normal writer's clock. The fleet-wide skew lease and one-microsecond LWW tie
// guard force the destructive generation below the slowest writer timestamp.
func TestPCD1B4ModelCrossNodeClockSkewCannotPoisonWriter(t *testing.T) {
	clock := cwClockSafety{
		gcNow: 103, slowWriterNow: 100, maxPairwiseSkew: 3, observedPairwiseSkew: 3, timestampGuard: 1,
		epoch: 100, targetW: 99, healthy: true, monotonic: true,
	}
	if _, ok := clock.mintSafeGeneration(); ok {
		t.Fatal("CW-M29: with no safe interval below the slow writer clock, GC must postpone destruction")
	}

	// The old single-clock rule admits g=102: it is above E/W and below the
	// fast GC clock, but ahead of the supported writer on the slow node.
	unsafeGeneration := clock.gcNow - 1
	if unsafeGeneration <= max(clock.epoch, clock.targetW) || unsafeGeneration > clock.gcNow || unsafeGeneration <= clock.slowWriterNow {
		t.Fatalf("CW-M29 precondition: expected a GC-legal but writer-future timestamp, got g=%d clock=%+v", unsafeGeneration, clock)
	}
	cell := cwReplicaCell{writeTs: clock.targetW, content: cwContentA}
	cell.delete(unsafeGeneration)
	cell.reaffirm(clock.slowWriterNow + 1) // writer rematerializes at real time T+ε.
	if cell.visible() {
		t.Fatal("CW-M29 mutation precondition: the GC-local future tombstone must hide the normal writer's successful materialization")
	}

	// At a later real time the same enforced Δ admits a timestamp which is
	// strictly behind every supported writer clock and above E/W.
	later := clock
	later.gcNow, later.slowWriterNow = 105, 102
	g, ok := later.mintSafeGeneration()
	if !ok || g <= max(later.epoch, later.targetW) || g > later.gcNow-later.maxPairwiseSkew-later.timestampGuard || g >= later.slowWriterNow {
		t.Fatalf("CW-M29 liveness: expected a safe timestamp below the slow writer clock, got g=%d ok=%v clock=%+v", g, ok, later)
	}

	unknownHealth := later
	unknownHealth.healthy = false
	if _, ok := unknownHealth.mintSafeGeneration(); ok {
		t.Fatal("CW-M29: unknown clock health must fail closed")
	}
	if unknownHealth.timestampAuthoritiesMayWrite() {
		t.Fatal("CW-M29: a timestamp authority with unknown clock health must be fenced from writes")
	}
	understatedBound := later
	understatedBound.maxPairwiseSkew = 2
	if _, ok := understatedBound.mintSafeGeneration(); ok {
		t.Fatal("CW-M29: an observed skew above the configured fleet bound must fail closed")
	}
	regressedClock := later
	regressedClock.monotonic = false
	if _, ok := regressedClock.mintSafeGeneration(); ok {
		t.Fatal("CW-M29: a clock regression must fail closed")
	}
}
