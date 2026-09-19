package db

import (
	"errors"
	"fmt"
	"strings"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

// SupportedContinuityContractVersion is persisted in every PC-D1A witness.
const SupportedContinuityContractVersion = "V1"

type LibraryContinuityCASOutcome uint8

const (
	LibraryContinuityCASUnknown LibraryContinuityCASOutcome = iota
	LibraryContinuityCASNotApplied
	LibraryContinuityCASApplied
)

// LibraryContinuityCASResult is the non-error result of a continuity LWT.
// Unknown is never a positive certification result.
type LibraryContinuityCASResult struct {
	Outcome                      LibraryContinuityCASOutcome
	CurrentHeadCommitID          string
	CurrentCertifiedHeadCommitID *string
	CurrentContractVersion       *string
}

var (
	ErrInvalidLibraryContinuityInput = errors.New("invalid library continuity input")
	ErrUnsupportedContinuityContract = errors.New("unsupported library continuity contract")
)

func validateLibraryContinuityInput(orgID, libraryID, head, contractVersion string) error {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(libraryID) == "" || strings.TrimSpace(head) == "" {
		return fmt.Errorf("%w: org, library, and head are required", ErrInvalidLibraryContinuityInput)
	}
	if strings.TrimSpace(contractVersion) == "" {
		return fmt.Errorf("%w: contract version is required", ErrInvalidLibraryContinuityInput)
	}
	if contractVersion != SupportedContinuityContractVersion {
		return fmt.Errorf("%w: %q", ErrUnsupportedContinuityContract, contractVersion)
	}
	return nil
}

func classifyLibraryContinuityCAS(applied bool, err error) LibraryContinuityCASOutcome {
	if err != nil {
		return LibraryContinuityCASUnknown
	}
	if applied {
		return LibraryContinuityCASApplied
	}
	return LibraryContinuityCASNotApplied
}

func libraryContinuityCASResult(applied bool, err error, state map[string]interface{}) LibraryContinuityCASResult {
	result := LibraryContinuityCASResult{Outcome: classifyLibraryContinuityCAS(applied, err)}
	if err != nil {
		return result
	}
	if head, ok := state["head_commit_id"].(string); ok {
		result.CurrentHeadCommitID = head
	}
	if certified, ok := state["continuity_certified_head_commit_id"].(string); ok {
		certifiedCopy := certified
		result.CurrentCertifiedHeadCommitID = &certifiedCopy
	}
	if version, ok := state["continuity_contract_version"].(string); ok {
		versionCopy := version
		result.CurrentContractVersion = &versionCopy
	}
	return result
}

// CommitLibraryContinuityWitness persists the result of a completed PC-D1
// baseline-certification handshake. The caller MUST already have proved the
// full dependency-level contract for this HEAD. This LWT only fences that
// claim to the current HEAD; it does not prove exact-P, tree completeness, or
// GC authority.
//
// A predicate miss is NOT_APPLIED. Any Cassandra error, including a timeout
// or RequestErrCASWriteUnknown, is UNKNOWN and must not be treated as
// certification.
func CommitLibraryContinuityWitness(session *gocql.Session, orgID, libraryID, observedHead, contractVersion string) (LibraryContinuityCASResult, error) {
	if session == nil {
		return LibraryContinuityCASResult{Outcome: LibraryContinuityCASUnknown}, fmt.Errorf("%w: nil Cassandra session", ErrInvalidLibraryContinuityInput)
	}
	if err := validateLibraryContinuityInput(orgID, libraryID, observedHead, contractVersion); err != nil {
		return LibraryContinuityCASResult{Outcome: LibraryContinuityCASUnknown}, err
	}

	state := map[string]interface{}{}
	applied, err := session.Query(`
		UPDATE libraries
		SET continuity_certified_head_commit_id = ?, continuity_contract_version = ?
		WHERE org_id = ? AND library_id = ?
		IF head_commit_id = ?
		AND deleted_at = null
	`, observedHead, contractVersion, orgID, libraryID, observedHead).
		SerialConsistency(LibraryHeadSerialConsistency).
		MapScanCAS(state)
	result := libraryContinuityCASResult(applied, err, state)
	if err != nil {
		return result, fmt.Errorf("commit library continuity witness: %w", err)
	}
	return result, nil
}

// AdvanceLibraryCertifiedFrontier atomically advances an already-certified
// frontier. It permits only (H,H,V) -> (H',H',V) under one global-SERIAL LWT.
// It never falls back to a plain HEAD CAS when the predecessor witness is
// missing, stale, or on an unsupported contract.
//
// This is a DB authority primitive only; no current HEAD writer or productive
// publication funnel calls it. PC-D1B must complete the proof precondition
// before a future caller uses it.
func AdvanceLibraryCertifiedFrontier(session *gocql.Session, orgID, libraryID, observedHead, nextHead, contractVersion string) (LibraryContinuityCASResult, error) {
	if session == nil {
		return LibraryContinuityCASResult{Outcome: LibraryContinuityCASUnknown}, fmt.Errorf("%w: nil Cassandra session", ErrInvalidLibraryContinuityInput)
	}
	if err := validateLibraryContinuityInput(orgID, libraryID, observedHead, contractVersion); err != nil {
		return LibraryContinuityCASResult{Outcome: LibraryContinuityCASUnknown}, err
	}
	if strings.TrimSpace(nextHead) == "" {
		return LibraryContinuityCASResult{Outcome: LibraryContinuityCASUnknown}, fmt.Errorf("%w: next head is required", ErrInvalidLibraryContinuityInput)
	}

	state := map[string]interface{}{}
	applied, err := session.Query(`
		UPDATE libraries
		SET head_commit_id = ?, continuity_certified_head_commit_id = ?, continuity_contract_version = ?
		WHERE org_id = ? AND library_id = ?
		IF head_commit_id = ?
		AND continuity_certified_head_commit_id = ?
		AND continuity_contract_version = ?
		AND deleted_at = null
	`, nextHead, nextHead, contractVersion, orgID, libraryID, observedHead, observedHead, contractVersion).
		SerialConsistency(LibraryHeadSerialConsistency).
		MapScanCAS(state)
	result := libraryContinuityCASResult(applied, err, state)
	if err != nil {
		return result, fmt.Errorf("advance library certified frontier: %w", err)
	}
	return result, nil
}
