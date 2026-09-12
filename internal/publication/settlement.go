package publication

import (
	"errors"
	"fmt"
)

// SettlementDisposition says what an adapter has independently established
// may happen to this attempt's staged state. It is deliberately not derivable
// from HeadOutcome alone: another writer can publish the same target while
// this attempt still owns distinct pub: state that should be cleaned.
type SettlementDisposition string

const (
	// SettlementPromote permits this attempt's pub: state to be promoted to fs:.
	SettlementPromote SettlementDisposition = "promote"
	// SettlementCleanupAttempt permits cleanup only of resources proven
	// exclusive to this attempt. It never by itself authorizes deleting
	// TargetCommitID or shared repair state; those need separate evidence.
	SettlementCleanupAttempt SettlementDisposition = "cleanup-attempt"
	// SettlementRetain withholds promotion and cleanup authority so durable
	// state remains available for later settlement in any datacenter.
	SettlementRetain SettlementDisposition = "retain"
)

// ErrInvalidSettlementDisposition identifies an unset or unknown disposition.
var ErrInvalidSettlementDisposition = errors.New("invalid publication settlement disposition")

// ErrInvalidSettlementDecision identifies a disposition incompatible with
// what is known about the target HEAD outcome.
var ErrInvalidSettlementDecision = errors.New("invalid publication settlement decision")

// Valid reports whether d is one of the three declared dispositions.
func (d SettlementDisposition) Valid() bool {
	switch d {
	case SettlementPromote, SettlementCleanupAttempt, SettlementRetain:
		return true
	default:
		return false
	}
}

// SettlementDecision binds an explicit disposition to one exact attempt while
// keeping the target outcome and attempt disposition as separate dimensions.
// The adapter supplies both from funnel-specific evidence; this package only
// rejects incomplete identities and combinations that violate fail-closed
// protocol rules.
type SettlementDecision struct {
	Attempt     AttemptIdentity
	Outcome     HeadOutcome
	Disposition SettlementDisposition
}

// Validate enforces universal safety without inventing a 1:1 mapping. UNKNOWN
// can only retain, and a target known to have lost cannot promote. APPLIED may
// promote, retain, or clean distinct attempt-local state (Sync same-target).
func (d SettlementDecision) Validate() error {
	if err := d.Attempt.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidSettlementDecision, err)
	}
	if !d.Outcome.Valid() {
		return fmt.Errorf("%w: %w %q", ErrInvalidSettlementDecision, ErrInvalidHeadOutcome, d.Outcome)
	}
	if !d.Disposition.Valid() {
		return fmt.Errorf("%w: %w %q", ErrInvalidSettlementDecision, ErrInvalidSettlementDisposition, d.Disposition)
	}
	if d.Outcome == HeadOutcomeUnknown && d.Disposition != SettlementRetain {
		return fmt.Errorf("%w: unknown HEAD outcome requires retain, got %q", ErrInvalidSettlementDecision, d.Disposition)
	}
	if d.Outcome == HeadOutcomeKnownLoser && d.Disposition == SettlementPromote {
		return fmt.Errorf("%w: known-loser HEAD outcome cannot promote", ErrInvalidSettlementDecision)
	}
	return nil
}

// AuthorizesAttemptCleanup reports explicit, validated authority to clean only
// attempt-exclusive resources. In particular, UNKNOWN always returns false.
func (d SettlementDecision) AuthorizesAttemptCleanup() bool {
	return d.Validate() == nil && d.Disposition == SettlementCleanupAttempt
}
