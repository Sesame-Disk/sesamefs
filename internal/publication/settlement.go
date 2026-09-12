package publication

import "fmt"

// SettlementDisposition is what the protocol does with an attempt's staged
// state once its HEAD outcome is classified (PC-0 §10 kernel). It names the
// disposition; executing it (promoting pub: to fs:, clearing or retaining the
// repair row, exact attempt cleanup) stays with today's funnel-specific
// settlement helpers until a funnel is migrated.
type SettlementDisposition string

const (
	// SettlementPromote: APPLIED → promote pub: to fs: and clear the repair
	// intent once promotion succeeds.
	SettlementPromote SettlementDisposition = "promote"
	// SettlementCleanupAttempt: KNOWN_LOSER → exact cleanup of this attempt's
	// staged state, and only then.
	SettlementCleanupAttempt SettlementDisposition = "cleanup-attempt"
	// SettlementRetain: UNKNOWN → retain pub: and the durable repair intent so
	// another process, in any datacenter, can settle later.
	SettlementRetain SettlementDisposition = "retain"
)

// DispositionFor is the pure rule mapping a HEAD outcome to its settlement.
// An invalid outcome is an error, never a disposition: the caller must not be
// able to obtain cleanup authority from an unset or unrecognized value.
func DispositionFor(outcome HeadOutcome) (SettlementDisposition, error) {
	switch outcome {
	case HeadOutcomeApplied:
		return SettlementPromote, nil
	case HeadOutcomeKnownLoser:
		return SettlementCleanupAttempt, nil
	case HeadOutcomeUnknown:
		return SettlementRetain, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidHeadOutcome, string(outcome))
	}
}
