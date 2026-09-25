package db

import "testing"

// CW-M33 models the distinction between a globally visible ordinary read and
// a stable absence proof. An old HEAD CAS has already observed H0 but has not
// settled; a hard delete removes the merged row. EACH_QUORUM alone sees the
// absence, but does not settle the outstanding global-SERIAL Paxos operation.
// A correct barrier settles it first: if it commits H1, the following global
// absence read refuses the capability; if it does not commit, no old proposal
// remains that can resurrect HEAD after the proof.
type cwM33Design struct {
	name                    string
	settlesPreexistingPaxos bool
}

var cwM33StableAbsence = cwM33Design{
	name:                    "CW-M33 stable absence",
	settlesPreexistingPaxos: true,
}

type cwM33State struct {
	rowPresent     bool
	headH1         bool
	oldCASInFlight bool
	proofMinted    bool
}

func cwM33Run(d cwM33Design, oldCASCommitsAtSettlement bool) cwM33State {
	s := cwM33State{rowPresent: true, oldCASInFlight: true}
	// Plain hard delete: outside Paxos.
	s.rowPresent = false

	if d.settlesPreexistingPaxos {
		// The global-SERIAL barrier resolves the old proposal before the
		// EACH_QUORUM absence read. Explore both valid settlement outcomes.
		s.oldCASInFlight = false
		if oldCASCommitsAtSettlement {
			s.rowPresent, s.headH1 = true, true
		}
	}

	// EACH_QUORUM observes the merged canonical row in every DC.
	s.proofMinted = !s.rowPresent
	if !d.settlesPreexistingPaxos && s.oldCASInFlight && oldCASCommitsAtSettlement {
		// The already-observed old Paxos proposal completes after the proof.
		s.rowPresent, s.headH1, s.oldCASInFlight = true, true, false
	}
	return s
}

func TestPCD1B4ModelStableCanonicalAbsenceRequiresPaxosSettlement(t *testing.T) {
	for _, commits := range []bool{false, true} {
		state := cwM33Run(cwM33StableAbsence, commits)
		if state.proofMinted && state.rowPresent {
			t.Fatalf("CW-M33 selected contract minted proof with a later-effective HEAD (old CAS commits=%v): %+v", commits, state)
		}
		if state.oldCASInFlight {
			t.Fatalf("CW-M33 selected contract returned with an unresolved old proposal: %+v", state)
		}
	}

	absenceOnly := cwM33StableAbsence
	absenceOnly.name = "CW-M33 mutation: EACH_QUORUM only"
	absenceOnly.settlesPreexistingPaxos = false
	state := cwM33Run(absenceOnly, true)
	if !state.proofMinted || !state.rowPresent || !state.headH1 {
		t.Fatalf("CW-M33 mutation must RED with proof followed by resurrected HEAD, got %+v", state)
	}
}

// CW-M33's second race is after the SERIAL barrier: that read observes H0,
// then a new HEAD CAS starts and pauses before commit; a plain hard delete
// makes the later EACH_QUORUM read absent. Absence from the later ordinary
// read cannot override the SERIAL leg's present observation.
type cwM33ProofPolicy struct {
	requireSerialAbsence bool
}

var cwM33StableProofPolicy = cwM33ProofPolicy{requireSerialAbsence: true}

func cwM33MayMintGlobalAbsenceProof(policy cwM33ProofPolicy, serialReadAbsent, eachQuorumReadAbsent bool) bool {
	return eachQuorumReadAbsent && (!policy.requireSerialAbsence || serialReadAbsent)
}

func TestPCD1B4ModelSerialProofReadMustObserveAbsence(t *testing.T) {
	serialSawH0 := true
	eachQuorumSawAbsentAfterDelete := true
	if cwM33MayMintGlobalAbsenceProof(cwM33StableProofPolicy, !serialSawH0, eachQuorumSawAbsentAfterDelete) {
		t.Fatal("CW-M33: stable absence proof minted after SERIAL proof-read observed H0")
	}

	// Load-bearing mutation: the later EACH_QUORUM miss is incorrectly treated
	// as sufficient even though a post-barrier HEAD CAS may now be in flight.
	noSerialAbsencePredicate := cwM33ProofPolicy{}
	if !cwM33MayMintGlobalAbsenceProof(noSerialAbsencePredicate, !serialSawH0, eachQuorumSawAbsentAfterDelete) {
		t.Fatal("CW-M33 mutation setup: EACH_QUORUM-only path must mint the unsafe proof")
	}
}

// CW-M34 freezes only the ordering property, not its runtime mechanism. A
// destroyer may either drain admitted writers and then revalidate liveness, or
// recover a late successful write above its destructive floor. Releasing an
// admission permit on timeout/UNKNOWN is unsafe while Cassandra may still
// accept the request.
type cwM34Policy struct {
	drainInFlight         bool
	revalidateAfterDrain  bool
	recoverAboveFloor     bool
	retainPermitOnUnknown bool
}

var cwM34SelectedPolicy = cwM34Policy{
	drainInFlight:         true,
	revalidateAfterDrain:  true,
	retainPermitOnUnknown: true,
}

type cwM34Result struct {
	writerSucceeded bool
	writerVisible   bool
	deleteIssued    bool
	permitRetained  bool
	trace           string
}

func cwM34Run(policy cwM34Policy, outcomeUnknown bool) cwM34Result {
	const (
		targetW      = int64(99)
		writerTS     = int64(100)
		destructiveG = int64(101)
	)
	if destructiveG <= targetW {
		return cwM34Result{trace: "invalid generation: g must exceed target W"}
	}
	active, referenceLive := true, false // admitted writer is paused in flight
	permitRetained := true
	if outcomeUnknown && !policy.retainPermitOnUnknown {
		active, permitRetained = false, false
	}

	var tombstone int64
	trace := "writer admitted at ts=100 and paused; target W=99; GC chooses g=101"
	if policy.drainInFlight && active {
		// The permit covers the entire uncertain lifetime, not only query issue.
		active = false
		referenceLive = true // settlement makes its associated liveness visible
		trace += " | GC drains writer through settlement"
		if policy.revalidateAfterDrain && referenceLive {
			return cwM34Result{writerSucceeded: true, writerVisible: true, permitRetained: permitRetained,
				trace: trace + " | post-drain liveness revalidation sees the materialization; delete refused"}
		}
	}

	if !active || !policy.drainInFlight {
		// The unsafe/mutated policy reaches the destructive write while a
		// previously admitted timestamp may still settle.
		tombstone = destructiveG
		trace += " | GC tombstone g=101 commits before old writer settles"
	}
	if active {
		active = false
		referenceLive = true
		trace += " | old writer settles successfully at ts=100"
	}
	visible := writerTS > tombstone
	if !visible && policy.recoverAboveFloor {
		// Recovery must use a fresh timestamp above the destructive floor and
		// revalidate the identity/reference before reporting success.
		visible = destructiveG+1 > tombstone
		trace += " | recovery rematerializes above g and verifies visibility"
	}
	return cwM34Result{writerSucceeded: true, writerVisible: visible, deleteIssued: tombstone != 0,
		permitRetained: permitRetained, trace: trace}
}

func TestPCD1B4ModelInFlightMaterializationNeedsBarrierOrRecovery(t *testing.T) {
	for _, policy := range []cwM34Policy{
		cwM34SelectedPolicy,
		{recoverAboveFloor: true, retainPermitOnUnknown: true},
	} {
		for _, unknown := range []bool{false, true} {
			result := cwM34Run(policy, unknown)
			if !result.writerSucceeded || !result.writerVisible {
				t.Fatalf("CW-M34 safe policy violated (policy=%+v UNKNOWN=%v): %+v", policy, unknown, result)
			}
			if unknown && policy.drainInFlight && !result.permitRetained {
				t.Fatalf("CW-M34 UNKNOWN released an active writer permit: %+v", result)
			}
		}
	}

	// Mutation: one-shot health admission with no drain, active-writer barrier,
	// or recovery. The already-admitted successful write is hidden by g.
	unsafe := cwM34Run(cwM34Policy{}, false)
	if !unsafe.writerSucceeded || unsafe.writerVisible || !unsafe.deleteIssued {
		t.Fatalf("CW-M34 missing-barrier mutation must RED with success hidden by tombstone, got %+v", unsafe)
	}
}
