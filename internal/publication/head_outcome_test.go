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
		if outcome.AuthorizesAttemptCleanup() {
			t.Fatalf("%q must not authorize attempt cleanup", outcome)
		}
	}
}

func TestOnlyKnownLoserAuthorizesAttemptCleanup(t *testing.T) {
	if !HeadOutcomeKnownLoser.AuthorizesAttemptCleanup() {
		t.Fatal("KNOWN_LOSER must authorize exact attempt cleanup")
	}
	if HeadOutcomeApplied.AuthorizesAttemptCleanup() {
		t.Fatal("APPLIED must settle by promotion, not attempt cleanup")
	}
}

// TestHeadOutcomeUnknownNeverAuthorizesAttemptCleanup is the PUBL-4/PUBL-5
// rule: UNKNOWN != KNOWN_LOSER, and UNKNOWN never authorizes destructive
// cleanup attributable to the attempt.
func TestHeadOutcomeUnknownNeverAuthorizesAttemptCleanup(t *testing.T) {
	if HeadOutcomeUnknown == HeadOutcomeKnownLoser {
		t.Fatal("UNKNOWN and KNOWN_LOSER must be distinct outcomes")
	}
	if HeadOutcomeUnknown.AuthorizesAttemptCleanup() {
		t.Fatal("UNKNOWN must never authorize attempt cleanup")
	}
	disposition, err := DispositionFor(HeadOutcomeUnknown)
	if err != nil {
		t.Fatalf("UNKNOWN must have a disposition: %v", err)
	}
	if disposition != SettlementRetain {
		t.Fatalf("UNKNOWN must retain, got %q", disposition)
	}
}

func TestDispositionForMapsEveryOutcome(t *testing.T) {
	cases := map[HeadOutcome]SettlementDisposition{
		HeadOutcomeApplied:    SettlementPromote,
		HeadOutcomeKnownLoser: SettlementCleanupAttempt,
		HeadOutcomeUnknown:    SettlementRetain,
	}
	coordinator := NewPublicationCoordinator()
	for outcome, want := range cases {
		got, err := DispositionFor(outcome)
		if err != nil {
			t.Fatalf("DispositionFor(%q): %v", outcome, err)
		}
		if got != want {
			t.Fatalf("DispositionFor(%q) = %q, want %q", outcome, got, want)
		}
		viaCoordinator, err := coordinator.SettlementFor(outcome)
		if err != nil || viaCoordinator != want {
			t.Fatalf("SettlementFor(%q) = (%q, %v), want (%q, nil)", outcome, viaCoordinator, err, want)
		}
	}
	if SettlementCleanupAttempt == SettlementRetain || SettlementPromote == SettlementRetain || SettlementPromote == SettlementCleanupAttempt {
		t.Fatal("settlement dispositions must be distinct")
	}
}

func TestDispositionForRejectsInvalidOutcome(t *testing.T) {
	for _, outcome := range []HeadOutcome{"", "garbage"} {
		disposition, err := DispositionFor(outcome)
		if !errors.Is(err, ErrInvalidHeadOutcome) {
			t.Fatalf("DispositionFor(%q) error = %v, want ErrInvalidHeadOutcome", outcome, err)
		}
		if disposition != "" {
			t.Fatalf("DispositionFor(%q) must not return a disposition, got %q", outcome, disposition)
		}
		if _, err := NewPublicationCoordinator().SettlementFor(outcome); !errors.Is(err, ErrInvalidHeadOutcome) {
			t.Fatalf("SettlementFor(%q) error = %v, want ErrInvalidHeadOutcome", outcome, err)
		}
	}
}
