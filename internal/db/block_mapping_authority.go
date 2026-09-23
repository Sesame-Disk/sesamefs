package db

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/metrics"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

// PC-D1B.3 Mapping Authority. `block_id_mappings` is an operational, mutable
// projection: the desktop sync upload accepts a client-asserted SHA-1, GC
// mapping cleanup can delete a row that a later write re-creates, and every
// replica agreeing on a row proves only convergence. None of that is
// provenance. This file owns the separate write-once authority that the
// certified-baseline certifier consumes for SHA-1-only file dependencies.
//
// Acquisition is cold-path only. Ordinary upload writers keep their plain
// read-before-write mapping and never run this LWT; a claim is established
// only by PromoteBlockMappingAuthority after independent provenance.
// The decision is docs/PC-D1B-METADATA-IDENTITY-AUTHORITY.md (PC-D1B.3).

// SupportedBlockMappingAuthorityContract is persisted with every claim. A
// claim is only comparable with a value produced under the same contract.
const SupportedBlockMappingAuthorityContract = "V1"

// BlockMappingEvidencePhysicalBytesV1 is the only provenance rule: the stored
// bytes of the canonical block hash to both the claimed internal SHA-256 and
// the external SHA-1. Content addressing makes that self-certifying, so it is
// independent of the mutable mapping row and of any replica agreement.
const BlockMappingEvidencePhysicalBytesV1 = "physical_bytes_v1"

// BlockMappingAuthorityClaim is one stored mapping claim.
type BlockMappingAuthorityClaim struct {
	OrgID            string
	RepresentationID string
	ExternalID       string
	InternalID       string
	ContractVersion  string
	Evidence         string
	CreatedAt        time.Time
}

// BlockMappingAuthorityOutcome classifies a promotion attempt.
type BlockMappingAuthorityOutcome uint8

const (
	// BlockMappingAuthorityUnknown: an ambiguous claim could not be settled.
	BlockMappingAuthorityUnknown BlockMappingAuthorityOutcome = iota
	// BlockMappingAuthorityPromoted: this attempt established the claim.
	BlockMappingAuthorityPromoted
	// BlockMappingAuthorityAlreadyAuthoritative: a claim already existed, or an
	// identical claim won the race.
	BlockMappingAuthorityAlreadyAuthoritative
	// BlockMappingAuthorityConflict: the durable claim differs from what this
	// attempt proved, or the stored claim is unreadable. Authority carries the
	// winner when it is a valid claim; the proved candidate never replaces it.
	BlockMappingAuthorityConflict
	// BlockMappingAuthorityUnproven: no claim exists and no independent
	// provenance proves a candidate. Convergence alone lands here.
	BlockMappingAuthorityUnproven
	// BlockMappingAuthorityUnavailable: the authority, the candidate or the
	// evidence could not be read, so nothing was decided.
	BlockMappingAuthorityUnavailable
)

func (o BlockMappingAuthorityOutcome) String() string {
	switch o {
	case BlockMappingAuthorityPromoted:
		return "promoted"
	case BlockMappingAuthorityAlreadyAuthoritative:
		return "already_authoritative"
	case BlockMappingAuthorityConflict:
		return "conflict"
	case BlockMappingAuthorityUnproven:
		return "unproven"
	case BlockMappingAuthorityUnavailable:
		return "unavailable"
	default:
		return "unknown"
	}
}

// BlockMappingPromotionResult is the classified result of a promotion.
type BlockMappingPromotionResult struct {
	Outcome BlockMappingAuthorityOutcome
	// Authority is the durable canonical SHA-256 when one is known.
	Authority string
	// Candidate is the mutable-mapping value this attempt examined, if any. It
	// is diagnostic only and is never consumable.
	Candidate string
}

// AuthoritativeInternalID returns the durable canonical SHA-256 a consumer may
// use. Only a valid stored or established claim qualifies.
func (r BlockMappingPromotionResult) AuthoritativeInternalID() (string, bool) {
	switch r.Outcome {
	case BlockMappingAuthorityPromoted, BlockMappingAuthorityAlreadyAuthoritative, BlockMappingAuthorityConflict:
		if IsSHA256BlockID(r.Authority) && r.Authority == strings.ToLower(r.Authority) {
			return r.Authority, true
		}
	}
	return "", false
}

var (
	ErrInvalidBlockMappingAuthorityInput = errors.New("invalid block mapping authority input")

	errBlockMappingEvidenceAbsent   = errors.New("block mapping provenance is absent")
	errBlockMappingEvidenceMismatch = errors.New("block mapping provenance does not prove the candidate")
)

type blockMappingIdentity struct {
	orgID            string
	representationID string
	externalID       string
}

// canonicalBlockMappingIdentity accepts exactly the identity shape of
// block_id_mappings. The external id must already be the canonical lowercase
// SHA-1: normalizing here could claim a different key than the caller asked.
func canonicalBlockMappingIdentity(orgID, representationID, externalID string) (blockMappingIdentity, error) {
	canonicalOrgID, err := canonicalIdentityUUID(orgID)
	if err != nil {
		return blockMappingIdentity{}, fmt.Errorf("%w: org id: %v", ErrInvalidBlockMappingAuthorityInput, err)
	}
	if representationID != strings.TrimSpace(representationID) || !IsCanonicalBlockRepresentationID(representationID) {
		return blockMappingIdentity{}, fmt.Errorf("%w: representation id %q is not canonical", ErrInvalidBlockMappingAuthorityInput, representationID)
	}
	if !IsSHA1BlockID(externalID) || externalID != strings.ToLower(externalID) {
		return blockMappingIdentity{}, fmt.Errorf("%w: external id %q is not a canonical SHA-1", ErrInvalidBlockMappingAuthorityInput, externalID)
	}
	return blockMappingIdentity{orgID: canonicalOrgID, representationID: representationID, externalID: externalID}, nil
}

// blockMappingProvenance is proof that internalID is the canonical SHA-256 of
// identity.externalID. Only proveBlockMappingCandidate constructs one.
type blockMappingProvenance struct {
	identity   blockMappingIdentity
	internalID string
	evidence   string
}

func validStoredBlockMappingClaim(claim *BlockMappingAuthorityClaim) bool {
	return claim != nil &&
		claim.ContractVersion == SupportedBlockMappingAuthorityContract &&
		IsSHA256BlockID(claim.InternalID) &&
		claim.InternalID == strings.ToLower(claim.InternalID)
}

// --- durable primitive ------------------------------------------------------

// claimBlockMappingAuthority writes the write-once, no-TTL claim under the
// protocol's global SERIAL domain. SerialConsistency is pinned explicitly and
// never inherited from database.serial_consistency: a LOCAL_SERIAL domain would
// let two datacenters each establish a different mapping for one identity.
// It is unexported and takes provenance, so no caller can claim a value that
// the promotion has not proved.
func claimBlockMappingAuthority(ctx context.Context, session *gocql.Session, proof blockMappingProvenance) (IdentityClaimOutcome, *BlockMappingAuthorityClaim, error) {
	if session == nil {
		return IdentityClaimUnknown, nil, fmt.Errorf("%w: nil Cassandra session", ErrInvalidBlockMappingAuthorityInput)
	}
	if proof.evidence == "" || !IsSHA256BlockID(proof.internalID) {
		return IdentityClaimUnknown, nil, fmt.Errorf("%w: claim requires proved provenance", ErrInvalidBlockMappingAuthorityInput)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return IdentityClaimUnknown, nil, err
	}
	existing := map[string]interface{}{}
	applied, err := session.Query(`
		INSERT INTO block_mapping_authority_claims (org_id, representation_id, external_id, internal_id, contract_version, evidence, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?) IF NOT EXISTS
	`, proof.identity.orgID, proof.identity.representationID, proof.identity.externalID, proof.internalID,
		SupportedBlockMappingAuthorityContract, proof.evidence, canonicalIdentityCreatedAt(time.Now())).
		WithContext(ctx).
		SerialConsistency(LibraryHeadSerialConsistency).
		MapScanCAS(existing)
	if err != nil {
		return IdentityClaimUnknown, nil, fmt.Errorf("claim block mapping authority %s: %w", proof.identity.externalID, err)
	}
	if applied {
		return IdentityClaimEstablished, nil, nil
	}
	stored := blockMappingClaimFromCAS(proof.identity, existing)
	if validStoredBlockMappingClaim(stored) && stored.InternalID == proof.internalID {
		return IdentityClaimIdempotent, stored, nil
	}
	return IdentityClaimConflict, stored, nil
}

func blockMappingClaimFromCAS(identity blockMappingIdentity, state map[string]interface{}) *BlockMappingAuthorityClaim {
	if len(state) == 0 {
		return nil
	}
	claim := &BlockMappingAuthorityClaim{OrgID: identity.orgID, RepresentationID: identity.representationID, ExternalID: identity.externalID}
	if value, ok := state["internal_id"].(string); ok {
		claim.InternalID = value
	}
	if value, ok := state["contract_version"].(string); ok {
		claim.ContractVersion = value
	}
	if value, ok := state["evidence"].(string); ok {
		claim.Evidence = value
	}
	if value, ok := state["created_at"].(time.Time); ok {
		claim.CreatedAt = value
	}
	if claim.InternalID == "" && claim.ContractVersion == "" {
		return nil
	}
	return claim
}

// ReadBlockMappingAuthority reads one claim with a SERIAL read in the same
// global domain it is written in. ok == false is a known-absent claim.
func ReadBlockMappingAuthority(ctx context.Context, session *gocql.Session, orgID, representationID, externalID string) (BlockMappingAuthorityClaim, bool, error) {
	identity, err := canonicalBlockMappingIdentity(orgID, representationID, externalID)
	if err != nil {
		return BlockMappingAuthorityClaim{}, false, err
	}
	return readBlockMappingAuthority(ctx, session, identity)
}

func readBlockMappingAuthority(ctx context.Context, session *gocql.Session, identity blockMappingIdentity) (BlockMappingAuthorityClaim, bool, error) {
	claim := BlockMappingAuthorityClaim{OrgID: identity.orgID, RepresentationID: identity.representationID, ExternalID: identity.externalID}
	if session == nil {
		return claim, false, fmt.Errorf("%w: nil Cassandra session", ErrInvalidBlockMappingAuthorityInput)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return claim, false, err
	}
	err := session.Query(`
		SELECT internal_id, contract_version, evidence, created_at
		FROM block_mapping_authority_claims
		WHERE org_id = ? AND representation_id = ? AND external_id = ?
	`, identity.orgID, identity.representationID, identity.externalID).
		WithContext(ctx).
		Consistency(IdentityAuthorityReadConsistency).
		Scan(&claim.InternalID, &claim.ContractVersion, &claim.Evidence, &claim.CreatedAt)
	if err != nil {
		if errors.Is(err, gocql.ErrNotFound) {
			return claim, false, nil
		}
		return claim, false, fmt.Errorf("read block mapping authority %s: %w", identity.externalID, err)
	}
	return claim, true, nil
}

// --- provenance -------------------------------------------------------------

// proveBlockMappingCandidate proves candidate from the canonical block's
// stored bytes. The mutable mapping only nominates the candidate; the proof is
// that the bytes hash to BOTH the candidate SHA-256 and the external SHA-1.
// Encrypted-library mappings whose SHA-1 covers plaintext cannot be proved
// without the library key and stay unproven.
func proveBlockMappingCandidate(ctx context.Context, database *DB, storageManager *storage.Manager, identity blockMappingIdentity, candidate string) (blockMappingProvenance, error) {
	if !IsSHA256BlockID(candidate) || candidate != strings.ToLower(candidate) {
		return blockMappingProvenance{}, fmt.Errorf("%w: candidate %q is not a canonical SHA-256", errBlockMappingEvidenceMismatch, candidate)
	}
	row, found, err := readBlockRepairAuthorityContextFn(ctx, database, identity.orgID, candidate, BlockAuthorityStrong)
	if err != nil {
		return blockMappingProvenance{}, fmt.Errorf("read canonical block %s: %w", candidate, err)
	}
	if !found || !row.StorageClassPresent || !row.StorageKeyPresent || strings.TrimSpace(row.StorageKey) == "" {
		return blockMappingProvenance{}, fmt.Errorf("%w: canonical block %s has no stored location", errBlockMappingEvidenceAbsent, candidate)
	}
	if storageManager == nil {
		return blockMappingProvenance{}, fmt.Errorf("storage manager unavailable for class %s", row.StorageClass)
	}
	store, err := storageManager.GetBlockStoreForOrg(identity.orgID, row.StorageClass)
	if err != nil {
		return blockMappingProvenance{}, fmt.Errorf("resolve storage class %s: %w", row.StorageClass, err)
	}
	if err := store.ValidatePhysicalLocator(candidate, row.StorageKey); err != nil {
		return blockMappingProvenance{}, fmt.Errorf("%w: canonical block %s locator: %v", errBlockMappingEvidenceAbsent, candidate, err)
	}
	exists, err := store.ObjectExists(ctx, row.StorageKey)
	if err != nil {
		return blockMappingProvenance{}, fmt.Errorf("check canonical block %s bytes: %w", candidate, err)
	}
	if !exists {
		return blockMappingProvenance{}, fmt.Errorf("%w: canonical block %s bytes are missing", errBlockMappingEvidenceAbsent, candidate)
	}
	reader, err := store.GetBlockReaderByStorageKey(ctx, row.StorageKey)
	if err != nil {
		return blockMappingProvenance{}, fmt.Errorf("read canonical block %s bytes: %w", candidate, err)
	}
	defer reader.Close()
	sha1Hash, sha256Hash := sha1.New(), sha256.New()
	if _, err := io.Copy(io.MultiWriter(sha1Hash, sha256Hash), reader); err != nil {
		return blockMappingProvenance{}, fmt.Errorf("hash canonical block %s bytes: %w", candidate, err)
	}
	return blockMappingProvenanceFromDigests(identity, candidate, hex.EncodeToString(sha1Hash.Sum(nil)), hex.EncodeToString(sha256Hash.Sum(nil)))
}

// blockMappingProvenanceFromDigests is the provenance rule itself: both
// content digests must match. A SHA-256 match alone only proves that the
// candidate block exists, not that it is the content the external SHA-1 names.
func blockMappingProvenanceFromDigests(identity blockMappingIdentity, candidate, contentSHA1, contentSHA256 string) (blockMappingProvenance, error) {
	if contentSHA256 != candidate {
		return blockMappingProvenance{}, fmt.Errorf("%w: stored bytes hash to SHA-256 %s, not %s", errBlockMappingEvidenceMismatch, contentSHA256, candidate)
	}
	if contentSHA1 != identity.externalID {
		return blockMappingProvenance{}, fmt.Errorf("%w: stored bytes hash to SHA-1 %s, not %s", errBlockMappingEvidenceMismatch, contentSHA1, identity.externalID)
	}
	return blockMappingProvenance{identity: identity, internalID: candidate, evidence: BlockMappingEvidencePhysicalBytesV1}, nil
}

// --- promotion ----------------------------------------------------------------

type blockMappingPromotionPorts struct {
	readAuthority func(context.Context, blockMappingIdentity) (BlockMappingAuthorityClaim, bool, error)
	readCandidate func(context.Context, blockMappingIdentity) (string, bool, error)
	prove         func(context.Context, blockMappingIdentity, string) (blockMappingProvenance, error)
	claim         func(context.Context, blockMappingProvenance) (IdentityClaimOutcome, *BlockMappingAuthorityClaim, error)
}

// PromoteBlockMappingAuthority is the cold-path acquisition of one mapping
// claim. It returns the existing claim when there is one, and otherwise claims
// only a candidate that independent provenance proved. It never repairs or
// rewrites a claim from the mutable mapping.
func (db *DB) PromoteBlockMappingAuthority(ctx context.Context, storageManager *storage.Manager, orgID, representationID, externalID string) (BlockMappingPromotionResult, error) {
	identity, err := canonicalBlockMappingIdentity(orgID, representationID, externalID)
	if err != nil {
		return BlockMappingPromotionResult{Outcome: BlockMappingAuthorityUnproven}, err
	}
	if db == nil || db.Session() == nil {
		return BlockMappingPromotionResult{Outcome: BlockMappingAuthorityUnavailable}, fmt.Errorf("database session unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := promoteBlockMappingAuthority(ctx, db.blockMappingPromotionPorts(storageManager), identity)
	metrics.BlockMappingAuthorityPromotionsTotal.WithLabelValues(result.Outcome.String()).Inc()
	return result, err
}

func (db *DB) blockMappingPromotionPorts(storageManager *storage.Manager) blockMappingPromotionPorts {
	return blockMappingPromotionPorts{
		readAuthority: func(ctx context.Context, identity blockMappingIdentity) (BlockMappingAuthorityClaim, bool, error) {
			return readBlockMappingAuthority(ctx, db.Session(), identity)
		},
		readCandidate: func(ctx context.Context, identity blockMappingIdentity) (string, bool, error) {
			return db.GetBlockIDMappingContext(ctx, identity.orgID, identity.representationID, identity.externalID)
		},
		prove: func(ctx context.Context, identity blockMappingIdentity, candidate string) (blockMappingProvenance, error) {
			return proveBlockMappingCandidate(ctx, db, storageManager, identity, candidate)
		},
		claim: func(ctx context.Context, proof blockMappingProvenance) (IdentityClaimOutcome, *BlockMappingAuthorityClaim, error) {
			return claimBlockMappingAuthority(ctx, db.Session(), proof)
		},
	}
}

func promoteBlockMappingAuthority(ctx context.Context, ports blockMappingPromotionPorts, identity blockMappingIdentity) (BlockMappingPromotionResult, error) {
	if stored, found, err := ports.readAuthority(ctx, identity); err != nil {
		return BlockMappingPromotionResult{Outcome: BlockMappingAuthorityUnavailable}, err
	} else if found {
		return settledBlockMappingClaim(&stored, "")
	}

	rawCandidate, found, err := ports.readCandidate(ctx, identity)
	if err != nil {
		return BlockMappingPromotionResult{Outcome: BlockMappingAuthorityUnavailable}, fmt.Errorf("read mapping candidate %s: %w", identity.externalID, err)
	}
	if !found {
		return BlockMappingPromotionResult{Outcome: BlockMappingAuthorityUnproven}, fmt.Errorf("%w: no mapping candidate for %s", errBlockMappingEvidenceAbsent, identity.externalID)
	}
	candidate := NormalizeBlockID(rawCandidate)

	proof, err := ports.prove(ctx, identity, candidate)
	if err != nil {
		if errors.Is(err, errBlockMappingEvidenceAbsent) || errors.Is(err, errBlockMappingEvidenceMismatch) {
			return BlockMappingPromotionResult{Outcome: BlockMappingAuthorityUnproven, Candidate: candidate}, err
		}
		return BlockMappingPromotionResult{Outcome: BlockMappingAuthorityUnavailable, Candidate: candidate}, err
	}
	if proof.identity != identity || proof.internalID != candidate || proof.evidence == "" {
		return BlockMappingPromotionResult{Outcome: BlockMappingAuthorityUnproven, Candidate: candidate}, fmt.Errorf("%w: provenance does not bind %s to %s", errBlockMappingEvidenceMismatch, identity.externalID, candidate)
	}

	outcome, stored, claimErr := ports.claim(ctx, proof)
	switch {
	case claimErr != nil:
		// Settle the ambiguous LWT with a SERIAL read, which completes any
		// in-progress Paxos round. Absent after settlement means nothing was
		// established; an unreadable settlement stays Unknown.
		settledClaim, settledFound, settleErr := ports.readAuthority(ctx, identity)
		if settleErr != nil {
			return BlockMappingPromotionResult{Outcome: BlockMappingAuthorityUnknown, Candidate: candidate}, errors.Join(claimErr, settleErr)
		}
		if !settledFound {
			return BlockMappingPromotionResult{Outcome: BlockMappingAuthorityUnavailable, Candidate: candidate}, claimErr
		}
		return settledBlockMappingClaim(&settledClaim, candidate)
	case outcome == IdentityClaimEstablished:
		return BlockMappingPromotionResult{Outcome: BlockMappingAuthorityPromoted, Authority: candidate, Candidate: candidate}, nil
	case outcome == IdentityClaimIdempotent:
		return BlockMappingPromotionResult{Outcome: BlockMappingAuthorityAlreadyAuthoritative, Authority: candidate, Candidate: candidate}, nil
	case outcome == IdentityClaimConflict:
		return settledBlockMappingClaim(stored, candidate)
	default:
		return BlockMappingPromotionResult{Outcome: BlockMappingAuthorityUnknown, Candidate: candidate}, fmt.Errorf("unexpected block mapping claim outcome %s", outcome)
	}
}

// settledBlockMappingClaim classifies an existing claim. The stored value is
// the authority even when this attempt proved a different candidate: a later
// or concurrent proof never replaces it.
func settledBlockMappingClaim(stored *BlockMappingAuthorityClaim, candidate string) (BlockMappingPromotionResult, error) {
	if !validStoredBlockMappingClaim(stored) {
		return BlockMappingPromotionResult{Outcome: BlockMappingAuthorityConflict, Candidate: candidate}, fmt.Errorf("stored block mapping authority is malformed or uses an unsupported contract")
	}
	if candidate != "" && stored.InternalID != candidate {
		return BlockMappingPromotionResult{Outcome: BlockMappingAuthorityConflict, Authority: stored.InternalID, Candidate: candidate}, nil
	}
	return BlockMappingPromotionResult{Outcome: BlockMappingAuthorityAlreadyAuthoritative, Authority: stored.InternalID, Candidate: candidate}, nil
}
