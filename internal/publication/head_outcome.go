package publication

import "errors"

// HeadOutcome is the common tri-state classification of one HEAD CAS attempt
// (PC-0 §2, §9). It is the vocabulary the coordinator will settle on; it is
// not persisted anywhere and no productive classifier produces it yet.
//
// Today's classifiers map onto it as follows (documentation only — none of
// these call sites is migrated in PC-1, and where the mapping would change a
// classification the change is a separate PR, see PC-0 §15 "HEAD classify
// split"):
//
//	v2 FSHelper.UpdateLibraryHead
//	  nil                               → HeadOutcomeApplied
//	  ErrLibraryHeadConflict            → HeadOutcomeKnownLoser
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
//	  HEAD == target                    → not a loser of the target: another
//	                                      writer published the same commit;
//	                                      only this attempt's pub: is cleaned
//	  syncHeadConflictError, divergent  → HeadOutcomeKnownLoser
//	v2 FSHelper.InitializeLibraryHeadIfUnset (InitialHeadOutcome)
//	  InitialHeadApplied                → HeadOutcomeApplied
//	  InitialHeadAlreadyInitialized     → HeadOutcomeKnownLoser
//	  InitialHeadUnknown                → HeadOutcomeUnknown
type HeadOutcome string

const (
	// HeadOutcomeApplied: the CAS is known to have made the target commit the
	// canonical HEAD.
	HeadOutcomeApplied HeadOutcome = "applied"
	// HeadOutcomeKnownLoser: the CAS returned applied=false with a definite
	// current HEAD, so the target commit never became HEAD. This is the only
	// outcome that authorizes cleanup attributable to the attempt.
	HeadOutcomeKnownLoser HeadOutcome = "known-loser"
	// HeadOutcomeUnknown: the CAS may have applied; confirmation failed or was
	// never attempted. UNKNOWN is not KNOWN_LOSER: it retains pub: and repair
	// and never authorizes destructive cleanup.
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

// AuthorizesAttemptCleanup reports whether the outcome permits destructive
// cleanup attributable to the attempt (removing its pub: references, its
// pending-owner rows, its repair row). Only a demonstrated KNOWN_LOSER does;
// APPLIED settles by promotion and UNKNOWN must retain (PC-0 §6 PUBL-4/5).
func (o HeadOutcome) AuthorizesAttemptCleanup() bool {
	return o == HeadOutcomeKnownLoser
}
