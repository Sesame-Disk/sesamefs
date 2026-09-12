package publication

import "errors"

// HeadOutcome is the common tri-state knowledge about the target commit after
// a HEAD publication attempt (PC-0 §2, §9). It describes the target, not the
// raw CAS applied bit and not cleanup authority for this attempt. It is not
// persisted anywhere and no productive classifier produces it in PC-1.
//
// Today's classifiers map onto it as follows (documentation only — none of
// these call sites is migrated in PC-1, and where the mapping would change a
// classification the change is a separate PR, see PC-0 §15 "HEAD classify
// split"):
//
//	v2 FSHelper.UpdateLibraryHead
//	  nil                               → HeadOutcomeApplied
//	  ErrLibraryHeadConflict, found
//	  HEAD == target                    → HeadOutcomeApplied
//	  ErrLibraryHeadConflict, divergent → HeadOutcomeKnownLoser
//	  ErrLibraryHeadPublicationUnknown  → HeadOutcomeUnknown
//	  ambiguous CAS + confirm shows a
//	  different HEAD                    → today a plain wrapped failure with no
//	                                      cleanup; strictly HeadOutcomeUnknown
//	                                      (the commit may have applied and been
//	                                      succeeded). Not unified here.
//	Sync SyncHandler.updateLibraryHeadWithStats
//	  nil / errSyncHeadPostCAS /
//	  errSyncHeadRepairPending          → HeadOutcomeApplied (HEAD did apply)
//	  errSyncHeadCASUncertain           → HeadOutcomeUnknown (any CAS error,
//	                                      no confirmation read)
//	  syncHeadConflictError, current
//	  HEAD == target                    → HeadOutcomeApplied; another writer
//	                                      published the same commit, while this
//	                                      attempt's pub: may still be cleaned
//	  syncHeadConflictError, divergent  → HeadOutcomeKnownLoser
//	v2 FSHelper.InitializeLibraryHeadIfUnset (InitialHeadOutcome)
//	  InitialHeadApplied                → HeadOutcomeApplied
//	  InitialHeadAlreadyInitialized,
//	  adopted HEAD == target            → HeadOutcomeApplied
//	  InitialHeadAlreadyInitialized,
//	  adopted HEAD differs              → HeadOutcomeKnownLoser
//	  InitialHeadUnknown                → HeadOutcomeUnknown
type HeadOutcome string

const (
	// HeadOutcomeApplied means the target commit is known to be canonical. This
	// attempt may have applied the CAS, or another writer may have published the
	// same target.
	HeadOutcomeApplied HeadOutcome = "applied"
	// HeadOutcomeKnownLoser means a definitive publication result observed a
	// different canonical HEAD, so this target lost that publication decision.
	// It does not by itself authorize cleanup of any resource.
	HeadOutcomeKnownLoser HeadOutcome = "known-loser"
	// HeadOutcomeUnknown means the target may have become canonical;
	// confirmation failed or was never attempted. UNKNOWN is not KNOWN_LOSER
	// and cannot be paired with a destructive settlement disposition.
	HeadOutcomeUnknown HeadOutcome = "unknown"
)

// ErrInvalidHeadOutcome is returned when a value outside the three declared
// outcomes reaches a rule that needs one. The zero value "" is invalid on
// purpose: an unset outcome must never be read as a decision.
var ErrInvalidHeadOutcome = errors.New("invalid publication head outcome")

// Valid reports whether o is one of the three declared outcomes.
func (o HeadOutcome) Valid() bool {
	switch o {
	case HeadOutcomeApplied, HeadOutcomeKnownLoser, HeadOutcomeUnknown:
		return true
	default:
		return false
	}
}
