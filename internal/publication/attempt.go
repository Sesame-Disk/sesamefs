package publication

import (
	"errors"
	"fmt"
)

// AttemptID is the identity of one publication attempt: the key behind the
// attempt-local pub: referrer (db.BlockReferrerForPublishAttempt) and the
// pending-owner rows that the settlement of exactly this attempt may touch.
type AttemptID string

// ErrInvalidAttemptIdentity is returned by AttemptIdentity.Validate when a
// required component is missing.
var ErrInvalidAttemptIdentity = errors.New("publication attempt identity is incomplete")

// AttemptIdentity names one publication attempt explicitly. Attempt and
// TargetCommitID are distinct concepts even though the v2, SeafHTTP, and
// OnlyOffice funnels currently use the new commit id for both: Sync mints a
// fresh UUID for its pub: identity and never reuses the target commit id
// (syncCommitBlockDelta.publishAttemptID), because a Sync commit id is shared
// by every writer that publishes the same target. Keeping the two fields apart
// is what lets a KNOWN_LOSER cleanup stay attributable to this attempt only.
type AttemptIdentity struct {
	// OrgID and RepoID scope the attempt; block references and HEAD are both
	// org- and library-scoped.
	OrgID  string
	RepoID string
	// Attempt is the pub: referrer identity of this attempt.
	Attempt AttemptID
	// TargetCommitID is the commit the HEAD CAS tries to make canonical.
	TargetCommitID string
	// ExpectedHead is the HEAD the CAS is conditioned on. It is empty only for
	// the initial-HEAD publish (FSHelper.InitializeLibraryHeadIfUnset), which
	// conditions on head_commit_id = null instead.
	ExpectedHead string
}

// Validate rejects an identity that cannot name an attempt. ExpectedHead may
// be empty (initial HEAD); every other component is required.
func (a AttemptIdentity) Validate() error {
	switch {
	case a.OrgID == "":
		return fmt.Errorf("%w: org id is empty", ErrInvalidAttemptIdentity)
	case a.RepoID == "":
		return fmt.Errorf("%w: repo id is empty", ErrInvalidAttemptIdentity)
	case a.Attempt == "":
		return fmt.Errorf("%w: attempt id is empty", ErrInvalidAttemptIdentity)
	case a.TargetCommitID == "":
		return fmt.Errorf("%w: target commit id is empty", ErrInvalidAttemptIdentity)
	}
	return nil
}
