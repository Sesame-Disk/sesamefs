package db

import (
	"context"
	"errors"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

// ErrLibraryDeleted indicates the canonical libraries row still exists but the
// library has been soft-deleted and must be treated as unavailable for live
// reads and writes.
var ErrLibraryDeleted = errors.New("library deleted")

// LibraryState captures the canonical fields that write/read fences need in
// order to treat soft-deleted libraries as unavailable.
type LibraryState struct {
	OrgID                 string
	LibraryID             string
	OwnerID               string
	Name                  string
	Encrypted             bool
	BlockRepresentationID string
	HeadCommitID          string
	// ContinuityCertifiedHeadCommitID and ContinuityContractVersion are the
	// canonical PC-D1A witness. A nil value means that no witness was
	// persisted; callers must still require equality with HeadCommitID and the
	// supported contract version before treating a library as certified.
	ContinuityCertifiedHeadCommitID *string
	ContinuityContractVersion       *string
	StorageClass                    string
	DeletedAt                       *time.Time
}

// ContinuityWitnessValidFor reports whether the canonical row proves exactly
// the current HEAD under the requested supported contract. It deliberately
// fails closed for an empty HEAD, a missing witness, a stale witness, or a
// contract version the caller does not support.
func (s LibraryState) ContinuityWitnessValidFor(contractVersion string) bool {
	return s.HeadCommitID != "" &&
		contractVersion == SupportedContinuityContractVersion &&
		s.ContinuityCertifiedHeadCommitID != nil &&
		*s.ContinuityCertifiedHeadCommitID == s.HeadCommitID &&
		s.ContinuityContractVersion != nil &&
		*s.ContinuityContractVersion == contractVersion
}

// ReadLibraryState loads the canonical libraries row for a known org/library
// pair, including deleted_at so callers can distinguish live vs soft-deleted.
func ReadLibraryState(session *gocql.Session, orgID, libraryID string) (LibraryState, error) {
	return ReadLibraryStateContext(context.Background(), session, orgID, libraryID)
}

// ReadLibraryStateContext is ReadLibraryState bound to ctx. Request-scoped
// callers use it so metadata work stops when the client disconnects or its
// preparation deadline expires.
func ReadLibraryStateContext(ctx context.Context, session *gocql.Session, orgID, libraryID string) (LibraryState, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	state := LibraryState{
		OrgID:     orgID,
		LibraryID: libraryID,
	}

	var continuityCertifiedHeadCommitID *string
	var continuityContractVersion *string
	var deletedAt time.Time
	if err := session.Query(`
		SELECT owner_id, name, encrypted, block_representation_id, head_commit_id,
			continuity_certified_head_commit_id, continuity_contract_version,
			storage_class, deleted_at
		FROM libraries
		WHERE org_id = ? AND library_id = ?
	`, orgID, libraryID).WithContext(ctx).Scan(
		&state.OwnerID,
		&state.Name,
		&state.Encrypted,
		&state.BlockRepresentationID,
		&state.HeadCommitID,
		&continuityCertifiedHeadCommitID,
		&continuityContractVersion,
		&state.StorageClass,
		&deletedAt,
	); err != nil {
		return LibraryState{}, err
	}
	state.ContinuityCertifiedHeadCommitID = continuityCertifiedHeadCommitID
	state.ContinuityContractVersion = continuityContractVersion

	if !deletedAt.IsZero() {
		deletedCopy := deletedAt
		state.DeletedAt = &deletedCopy
	}

	return state, nil
}

// ReadLiveLibraryState returns the canonical library row only when the library
// is still live. Soft-deleted libraries are reported via ErrLibraryDeleted.
func ReadLiveLibraryState(session *gocql.Session, orgID, libraryID string) (LibraryState, error) {
	return ReadLiveLibraryStateContext(context.Background(), session, orgID, libraryID)
}

// ReadLiveLibraryStateContext is ReadLiveLibraryState bound to ctx.
func ReadLiveLibraryStateContext(ctx context.Context, session *gocql.Session, orgID, libraryID string) (LibraryState, error) {
	state, err := ReadLibraryStateContext(ctx, session, orgID, libraryID)
	if err != nil {
		return LibraryState{}, err
	}
	if state.DeletedAt != nil {
		return LibraryState{}, ErrLibraryDeleted
	}
	return state, nil
}

// ResolveLiveLibraryStateByID resolves the org partition through libraries_by_id
// and then returns the canonical live library row.
func ResolveLiveLibraryStateByID(session *gocql.Session, libraryID string) (LibraryState, error) {
	var orgID string
	if err := session.Query(`
		SELECT org_id FROM libraries_by_id WHERE library_id = ?
	`, libraryID).Scan(&orgID); err != nil {
		return LibraryState{}, err
	}

	return ReadLiveLibraryState(session, orgID, libraryID)
}
