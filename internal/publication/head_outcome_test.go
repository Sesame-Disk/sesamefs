package publication

import (
	"errors"
	"testing"
)

func TestHeadOutcomesAreDistinctAndValid(t *testing.T) {
	outcomes := []HeadOutcome{HeadOutcomeApplied, HeadOutcomeKnownLoser, HeadOutcomeUnknown}
	seen := map[HeadOutcome]bool{}
	for _, outcome := range outcomes {
		if !outcome.Valid() {
			t.Fatalf("declared outcome %q is not Valid()", outcome)
		}
		if seen[outcome] {
			t.Fatalf("outcome %q declared twice", outcome)
		}
		seen[outcome] = true
	}
	if len(seen) != 3 {
		t.Fatalf("expected 3 distinct outcomes, got %d", len(seen))
	}
}

func TestHeadOutcomeZeroAndUnknownValuesAreInvalid(t *testing.T) {
	for _, outcome := range []HeadOutcome{"", "garbage", "APPLIED", "loser"} {
		if outcome.Valid() {
			t.Fatalf("%q must not be Valid()", outcome)
		}
	}
}

// TestHeadOutcomeUnknownNeverAuthorizesAttemptCleanup is the PUBL-4/PUBL-5
// rule: UNKNOWN != KNOWN_LOSER, and UNKNOWN never authorizes destructive
// cleanup attributable to the attempt.
func TestHeadOutcomeUnknownNeverAuthorizesAttemptCleanup(t *testing.T) {
	if HeadOutcomeUnknown == HeadOutcomeKnownLoser {
		t.Fatal("UNKNOWN and KNOWN_LOSER must be distinct outcomes")
	}
	for _, disposition := range []SettlementDisposition{SettlementPromote, SettlementCleanupAttempt} {
		decision := validSettlementDecision(HeadOutcomeUnknown, disposition)
		if err := decision.Validate(); !errors.Is(err, ErrInvalidSettlementDecision) {
			t.Fatalf("UNKNOWN + %q error = %v, want ErrInvalidSettlementDecision", disposition, err)
		}
		if decision.AuthorizesAttemptCleanup() {
			t.Fatalf("UNKNOWN + %q must never authorize attempt cleanup", disposition)
		}
	}
	retain := validSettlementDecision(HeadOutcomeUnknown, SettlementRetain)
	if err := retain.Validate(); err != nil {
		t.Fatalf("UNKNOWN + retain rejected: %v", err)
	}
	if retain.AuthorizesAttemptCleanup() {
		t.Fatal("UNKNOWN + retain must not authorize attempt cleanup")
	}
}

func TestSettlementDispositionsAreDistinctAndValid(t *testing.T) {
	dispositions := []SettlementDisposition{SettlementPromote, SettlementCleanupAttempt, SettlementRetain}
	seen := map[SettlementDisposition]bool{}
	for _, disposition := range dispositions {
		if !disposition.Valid() {
			t.Fatalf("declared disposition %q is not Valid()", disposition)
		}
		if seen[disposition] {
			t.Fatalf("disposition %q declared twice", disposition)
		}
		seen[disposition] = true
	}
	for _, disposition := range []SettlementDisposition{"", "garbage"} {
		if disposition.Valid() {
			t.Fatalf("%q must not be Valid()", disposition)
		}
	}
}

func validSettlementDecision(outcome HeadOutcome, disposition SettlementDisposition) SettlementDecision {
	return SettlementDecision{
		Attempt:     validAttempt(),
		Outcome:     outcome,
		Disposition: disposition,
	}
}

func TestSettlementDecisionRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		decision SettlementDecision
		want     error
	}{
		{decision: SettlementDecision{Outcome: HeadOutcomeApplied, Disposition: SettlementRetain}, want: ErrInvalidAttemptIdentity},
		{decision: validSettlementDecision("", SettlementRetain), want: ErrInvalidHeadOutcome},
		{decision: validSettlementDecision(HeadOutcomeApplied, ""), want: ErrInvalidSettlementDisposition},
		{decision: validSettlementDecision("garbage", SettlementRetain), want: ErrInvalidHeadOutcome},
		{decision: validSettlementDecision(HeadOutcomeApplied, "garbage"), want: ErrInvalidSettlementDisposition},
	}
	for _, tc := range cases {
		err := tc.decision.Validate()
		if !errors.Is(err, ErrInvalidSettlementDecision) || !errors.Is(err, tc.want) {
			t.Fatalf("decision %+v error = %v, want ErrInvalidSettlementDecision and %v", tc.decision, err, tc.want)
		}
		if tc.decision.AuthorizesAttemptCleanup() {
			t.Fatalf("invalid decision %+v must not authorize cleanup", tc.decision)
		}
	}
}

func TestSettlementDecisionKeepsTargetAndAttemptAxesSeparate(t *testing.T) {
	cases := []SettlementDecision{
		validSettlementDecision(HeadOutcomeApplied, SettlementPromote),
		validSettlementDecision(HeadOutcomeApplied, SettlementCleanupAttempt),
		validSettlementDecision(HeadOutcomeApplied, SettlementRetain),
		validSettlementDecision(HeadOutcomeKnownLoser, SettlementCleanupAttempt),
		validSettlementDecision(HeadOutcomeKnownLoser, SettlementRetain),
		validSettlementDecision(HeadOutcomeUnknown, SettlementRetain),
	}
	coordinator := NewPublicationCoordinator()
	for _, decision := range cases {
		if err := decision.Validate(); err != nil {
			t.Fatalf("decision %+v rejected: %v", decision, err)
		}
		if err := coordinator.ValidateSettlement(decision); err != nil {
			t.Fatalf("coordinator rejected decision %+v: %v", decision, err)
		}
	}

	// APPLIED + cleanup is the same-target shape: the target is canonical while
	// this writer's distinct attempt-local pub: state has cleanup authority.
	sameTarget := validSettlementDecision(HeadOutcomeApplied, SettlementCleanupAttempt)
	if !sameTarget.AuthorizesAttemptCleanup() {
		t.Fatal("same-target APPLIED decision must authorize exact attempt cleanup")
	}
}

func TestAppliedCleanupRequiresDistinctAttemptIdentity(t *testing.T) {
	applied := validSettlementDecision(HeadOutcomeApplied, SettlementCleanupAttempt)
	applied.Attempt.Attempt = AttemptID(applied.Attempt.TargetCommitID)
	if err := applied.Validate(); !errors.Is(err, ErrInvalidSettlementDecision) {
		t.Fatalf("APPLIED cleanup with attempt == target error = %v, want ErrInvalidSettlementDecision", err)
	}
	if applied.AuthorizesAttemptCleanup() {
		t.Fatal("APPLIED cleanup with attempt == target must not authorize cleanup")
	}

	knownLoser := validSettlementDecision(HeadOutcomeKnownLoser, SettlementCleanupAttempt)
	knownLoser.Attempt.Attempt = AttemptID(knownLoser.Attempt.TargetCommitID)
	if err := knownLoser.Validate(); err != nil {
		t.Fatalf("KNOWN_LOSER cleanup with attempt == target rejected: %v", err)
	}
	if !knownLoser.AuthorizesAttemptCleanup() {
		t.Fatal("KNOWN_LOSER cleanup with attempt == target must remain representable")
	}
}

func TestKnownLoserCannotPromote(t *testing.T) {
	decision := validSettlementDecision(HeadOutcomeKnownLoser, SettlementPromote)
	if err := decision.Validate(); !errors.Is(err, ErrInvalidSettlementDecision) {
		t.Fatalf("KNOWN_LOSER + promote error = %v, want ErrInvalidSettlementDecision", err)
	}
}
