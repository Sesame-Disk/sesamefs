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
// projection written by plain read-before-write INSERTs with no LWT: the
// desktop sync upload accepts a client-asserted SHA-1, two same-key writers can
// both read "absent" and the later INSERT wins, a delayed or in-flight mutation
// (including a hint) can land after any read, and every replica agreeing on a
// row proves only convergence. None of that is provenance. This file owns the
// separate write-once authority the certified-baseline certifier consumes for
// SHA-1-only file dependencies, and the projection freeze that makes stale
// ordinary writes inert once that authority is consumable.
//
// Acquisition is cold-path only. Ordinary upload writers keep their plain
// read-before-write mapping and never run this LWT; a claim is established
// only by PromoteBlockMappingAuthority after independent provenance.
// The decision is docs/PC-D1B-METADATA-IDENTITY-AUTHORITY.md (PC-D1B.3).

// SupportedBlockMappingAuthorityContract is persisted with every claim. A
// claim is only comparable with a value produced under the same contract.
const SupportedBlockMappingAuthorityContract = "V1"

// BlockMappingEvidencePhysicalBytesV1 is the only provenance rule: the stored
// bytes of the canonical block, in the claimed representation, hash to both the
// claimed internal SHA-256 and the external SHA-1. Content addressing makes
// that self-certifying, so it is independent of the mutable mapping row and of
// any replica agreement.
const BlockMappingEvidencePhysicalBytesV1 = "physical_bytes_v1"

// BlockMappingProjectionFrozenTimestamp is the write timestamp (microseconds)
// the promotion uses to freeze the mutable block_id_mappings row to the
// authority. Ordinary writers use wall-clock timestamps, orders of magnitude
// below it, so under last-write-wins every ordinary write - earlier, in flight,
// delayed or replayed as a hint - is inert against the frozen cell. Production
// never deletes block_id_mappings rows (R11a), so nothing needs to supersede it.
const BlockMappingProjectionFrozenTimestamp int64 = 1 << 62

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
	// attempt proved, or the stored claim is unusable. Authority carries the
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

// BlockMappingProjectionState is the temporal half of a promotion: whether the
// ordinary block_id_mappings row that readers resolve through is frozen to the
// authority, so no stale ordinary write can later make it resolve elsewhere.
type BlockMappingProjectionState uint8

const (
	// BlockMappingProjectionUnknown: not attempted, or not verifiable.
	BlockMappingProjectionUnknown BlockMappingProjectionState = iota
	// BlockMappingProjectionFrozen: the row resolves to the authority at the
	// frozen timestamp, so ordinary writes cannot supersede it.
	BlockMappingProjectionFrozen
	// BlockMappingProjectionDiverged: the row already resolves elsewhere. It is
	// never overwritten from here; the mapping stays non-consumable.
	BlockMappingProjectionDiverged
)

func (s BlockMappingProjectionState) String() string {
	switch s {
	case BlockMappingProjectionFrozen:
		return "frozen"
	case BlockMappingProjectionDiverged:
		return "diverged"
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
	// Projection is the state of the ordinary mapping row after the attempt.
	Projection BlockMappingProjectionState
}

// claimedInternalID returns the durable canonical SHA-256 of a valid stored
// or established claim, whether or not the projection is frozen yet.
func (r BlockMappingPromotionResult) claimedInternalID() (string, bool) {
	switch r.Outcome {
	case BlockMappingAuthorityPromoted, BlockMappingAuthorityAlreadyAuthoritative, BlockMappingAuthorityConflict:
		if IsSHA256BlockID(r.Authority) && r.Authority == strings.ToLower(r.Authority) {
			return r.Authority, true
		}
	}
	return "", false
}

// ConsumableInternalID returns the canonical SHA-256 a consumer may depend on:
// a valid durable claim (semantic provenance) whose ordinary projection is
// frozen to it (temporal authority). Either half alone is not enough.
func (r BlockMappingPromotionResult) ConsumableInternalID() (string, bool) {
	authority, claimed := r.claimedInternalID()
	if !claimed || r.Projection != BlockMappingProjectionFrozen {
		return "", false
	}
	return authority, true
}

var (
	ErrInvalidBlockMappingAuthorityInput = errors.New("invalid block mapping authority input")

	errBlockMappingEvidenceAbsent     = errors.New("block mapping provenance is absent")
	errBlockMappingEvidenceMismatch   = errors.New("block mapping provenance does not prove the candidate")
	errBlockMappingProjectionDiverged = errors.New("block mapping projection diverges from its authority")
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

// validStoredBlockMappingClaim accepts only a claim this code can have
// written: the supported contract, the only supported evidence rule and a
// canonical SHA-256. Anything else is unusable, never "probably fine".
func validStoredBlockMappingClaim(claim *BlockMappingAuthorityClaim) bool {
	return claim != nil &&
		claim.ContractVersion == SupportedBlockMappingAuthorityContract &&
		claim.Evidence == BlockMappingEvidencePhysicalBytesV1 &&
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

// --- projection freeze (temporal authority) ---------------------------------

type blockMappingProjectionRow struct {
	internalID string
	writeTime  int64
	found      bool
}

// ReadBlockMappingProjection reads the ordinary mapping row and whether it is
// frozen, at EACH_QUORUM so every datacenter's copy is covered.
func ReadBlockMappingProjection(ctx context.Context, session *gocql.Session, orgID, representationID, externalID string) (internalID string, frozen, found bool, err error) {
	identity, err := canonicalBlockMappingIdentity(orgID, representationID, externalID)
	if err != nil {
		return "", false, false, err
	}
	row, err := readBlockMappingProjection(ctx, session, identity)
	if err != nil || !row.found {
		return "", false, false, err
	}
	return NormalizeBlockID(row.internalID), row.writeTime == BlockMappingProjectionFrozenTimestamp, true, nil
}

func readBlockMappingProjection(ctx context.Context, session *gocql.Session, identity blockMappingIdentity) (blockMappingProjectionRow, error) {
	var row blockMappingProjectionRow
	if session == nil {
		return row, fmt.Errorf("%w: nil Cassandra session", ErrInvalidBlockMappingAuthorityInput)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	err := session.Query(`
		SELECT internal_id, WRITETIME(internal_id) FROM block_id_mappings
		WHERE org_id = ? AND representation_id = ? AND external_id = ?
	`, identity.orgID, identity.representationID, identity.externalID).
		WithContext(ctx).
		Consistency(gocql.EachQuorum).
		Scan(&row.internalID, &row.writeTime)
	if err != nil {
		if errors.Is(err, gocql.ErrNotFound) {
			return blockMappingProjectionRow{}, nil
		}
		return blockMappingProjectionRow{}, fmt.Errorf("read mapping projection %s: %w", identity.externalID, err)
	}
	row.found = true
	return row, nil
}

// blockMappingProjectionDecision is the freeze rule. A row that already
// resolves elsewhere is Diverged and is never overwritten: the authority does
// not repair the projection. An absent or agreeing row that is not yet frozen
// needs the freeze write.
func blockMappingProjectionDecision(row blockMappingProjectionRow, authority string) (state BlockMappingProjectionState, needsFreeze bool) {
	if row.found && NormalizeBlockID(row.internalID) != authority {
		return BlockMappingProjectionDiverged, false
	}
	if row.found && row.writeTime == BlockMappingProjectionFrozenTimestamp {
		return BlockMappingProjectionFrozen, false
	}
	return BlockMappingProjectionUnknown, true
}

// freezeBlockMappingProjection is the temporal cutover of a promotion. It
// rewrites the ordinary row to the authority at the frozen timestamp, which
// neutralizes every pre-fence, in-flight or delayed ordinary mutation without
// any writer cooperating and without Paxos on the upload path. A stale write
// that lands between the decision read and the freeze write is overwritten by
// the freeze; one that landed before the read is observed and fails closed.
func freezeBlockMappingProjection(ctx context.Context, session *gocql.Session, identity blockMappingIdentity, authority string) (BlockMappingProjectionState, error) {
	row, err := readBlockMappingProjection(ctx, session, identity)
	if err != nil {
		return BlockMappingProjectionUnknown, err
	}
	state, needsFreeze := blockMappingProjectionDecision(row, authority)
	if state == BlockMappingProjectionDiverged {
		return state, fmt.Errorf("%w: %s resolves to %s, authority is %s", errBlockMappingProjectionDiverged, identity.externalID, NormalizeBlockID(row.internalID), authority)
	}
	if !needsFreeze {
		return state, nil
	}
	if err := session.Query(`
		UPDATE block_id_mappings USING TIMESTAMP ? SET internal_id = ?
		WHERE org_id = ? AND representation_id = ? AND external_id = ?
	`, BlockMappingProjectionFrozenTimestamp, authority, identity.orgID, identity.representationID, identity.externalID).
		WithContext(ctx).
		Consistency(gocql.EachQuorum).
		Exec(); err != nil {
		return BlockMappingProjectionUnknown, fmt.Errorf("freeze mapping projection %s: %w", identity.externalID, err)
	}
	after, err := readBlockMappingProjection(ctx, session, identity)
	if err != nil {
		return BlockMappingProjectionUnknown, err
	}
	state, needsFreeze = blockMappingProjectionDecision(after, authority)
	switch {
	case state == BlockMappingProjectionDiverged:
		return state, fmt.Errorf("%w: %s resolves to %s after freeze", errBlockMappingProjectionDiverged, identity.externalID, NormalizeBlockID(after.internalID))
	case needsFreeze:
		return BlockMappingProjectionUnknown, fmt.Errorf("mapping projection %s freeze is not visible at EACH_QUORUM", identity.externalID)
	}
	return state, nil
}

// --- provenance -------------------------------------------------------------

// proveBlockMappingCandidate proves candidate from the canonical block's
// stored bytes. The mutable mapping only nominates the candidate; the proof is
// that the block belongs to the claimed representation and its bytes hash to
// BOTH the candidate SHA-256 and the external SHA-1. Encrypted-library
// mappings whose SHA-1 covers plaintext cannot be proved without the library
// key and stay unproven.
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
	return blockMappingProvenanceFromEvidence(identity, candidate, row.RepresentationID, hex.EncodeToString(sha1Hash.Sum(nil)), hex.EncodeToString(sha256Hash.Sum(nil)))
}

// blockMappingProvenanceFromEvidence is the provenance rule itself. The block
// must belong to the representation being claimed, and both content digests
// must match. A hash relation proved from another representation's block does
// not prove this identity, and a SHA-256 match alone only proves that the
// candidate block exists, not that it is the content the external SHA-1 names.
func blockMappingProvenanceFromEvidence(identity blockMappingIdentity, candidate, blockRepresentationID, contentSHA1, contentSHA256 string) (blockMappingProvenance, error) {
	if blockRepresentationID != identity.representationID {
		return blockMappingProvenance{}, fmt.Errorf("%w: canonical block %s belongs to representation %q, not %q", errBlockMappingEvidenceMismatch, candidate, blockRepresentationID, identity.representationID)
	}
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
	freeze        func(context.Context, blockMappingIdentity, string) (BlockMappingProjectionState, error)
}

// PromoteBlockMappingAuthority is the cold-path acquisition of one mapping:
// semantic provenance (the write-once claim) followed by temporal authority
// (freezing the ordinary projection to the claim). It returns the existing
// claim when there is one, claims only a candidate that independent provenance
// proved, and never repairs a claim or a diverged projection.
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
		freeze: func(ctx context.Context, identity blockMappingIdentity, authority string) (BlockMappingProjectionState, error) {
			return freezeBlockMappingProjection(ctx, db.Session(), identity, authority)
		},
	}
}

func promoteBlockMappingAuthority(ctx context.Context, ports blockMappingPromotionPorts, identity blockMappingIdentity) (BlockMappingPromotionResult, error) {
	result, err := acquireBlockMappingClaim(ctx, ports, identity)
	authority, claimed := result.claimedInternalID()
	if !claimed {
		return result, err
	}
	state, freezeErr := ports.freeze(ctx, identity, authority)
	result.Projection = state
	return result, errors.Join(err, freezeErr)
}

func acquireBlockMappingClaim(ctx context.Context, ports blockMappingPromotionPorts, identity blockMappingIdentity) (BlockMappingPromotionResult, error) {
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
		return BlockMappingPromotionResult{Outcome: BlockMappingAuthorityConflict, Candidate: candidate}, fmt.Errorf("stored block mapping authority is malformed or uses an unsupported contract or evidence")
	}
	if candidate != "" && stored.InternalID != candidate {
		return BlockMappingPromotionResult{Outcome: BlockMappingAuthorityConflict, Authority: stored.InternalID, Candidate: candidate}, nil
	}
	return BlockMappingPromotionResult{Outcome: BlockMappingAuthorityAlreadyAuthoritative, Authority: stored.InternalID, Candidate: candidate}, nil
}
