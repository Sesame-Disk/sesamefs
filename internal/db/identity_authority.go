package db

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

// PC-D1B metadata identity authority. A complete `commits` / `fs_objects` row
// is not proof that it is the authoritative version of that identity, so
// baseline certification needs durable provenance that is independent of the
// row it describes. This file owns the write-once primitive; PR #231 consumes
// it only through the typed gateway, while certifier, mapping-promotion and
// productive witness behavior remain separate stages.
// The decision is docs/PC-D1B-METADATA-IDENTITY-AUTHORITY.md and the finding is
// ISSUE-PCD1B-METADATA-IDENTITY-AUTHORITY-01.

// SupportedIdentityDigestVersion is persisted with every claim. The digest
// encoding below is part of the contract: changing what a digest covers, or how
// it is encoded, requires a new version rather than a silent recomputation,
// because an existing claim can only ever be compared against a digest produced
// the same way.
const SupportedIdentityDigestVersion = "V1"

// IdentityAuthorityReadConsistency is the read side of the claim. It is a
// SERIAL read in the same global domain the claim is written in, so a reader
// observes an in-progress Paxos round instead of a replica that has not learned
// it yet. Like LibraryHeadSerialConsistency it is pinned explicitly and never
// derived from database.serial_consistency / CASSANDRA_SERIAL_CONSISTENCY.
const IdentityAuthorityReadConsistency = gocql.Serial

// IdentityKind namespaces a claim inside one library. Files and directories
// share fs_objects' Cassandra identity, so their subtype belongs in the digest
// rather than in this claim-key namespace.
type IdentityKind string

const (
	IdentityKindCommit   IdentityKind = "commit"
	IdentityKindFSObject IdentityKind = "fs_object"
)

// IdentityClaimOutcome is the non-error result of a claim attempt. Unknown is
// never provenance: an ambiguous LWT leaves the identity unproven, exactly as
// an ambiguous witness CAS never implies certification.
type IdentityClaimOutcome uint8

const (
	IdentityClaimUnknown IdentityClaimOutcome = iota
	// IdentityClaimEstablished: this attempt wrote the first claim.
	IdentityClaimEstablished
	// IdentityClaimIdempotent: a claim already existed with the same version
	// and digest. A retry of the same identity is not a conflict.
	IdentityClaimIdempotent
	// IdentityClaimConflict: a claim already existed with a different version
	// or digest. The stored row and the claimed projection disagree, which is
	// the case the certifier must refuse.
	IdentityClaimConflict
)

func (o IdentityClaimOutcome) String() string {
	switch o {
	case IdentityClaimEstablished:
		return "established"
	case IdentityClaimIdempotent:
		return "idempotent"
	case IdentityClaimConflict:
		return "conflict"
	default:
		return "unknown"
	}
}

// IdentityAuthorityClaim is one stored claim.
type IdentityAuthorityClaim struct {
	LibraryID     string
	Kind          IdentityKind
	IdentityID    string
	DigestVersion string
	Digest        string
	CreatedAt     time.Time
}

// IdentityClaimResult carries the outcome plus whatever claim was already
// stored, so a caller can report which digest it lost to without a second read
// that could observe a different state.
type IdentityClaimResult struct {
	Outcome IdentityClaimOutcome
	Stored  *IdentityAuthorityClaim
}

var (
	ErrInvalidIdentityAuthorityInput = errors.New("invalid identity authority input")
	ErrUnsupportedIdentityDigest     = errors.New("unsupported identity digest version")
)

func validIdentityKind(kind IdentityKind) bool {
	switch kind {
	case IdentityKindCommit, IdentityKindFSObject:
		return true
	default:
		return false
	}
}

func validateIdentityAuthorityInput(libraryID string, kind IdentityKind, identityID, digestVersion, digest string) error {
	if strings.TrimSpace(libraryID) == "" {
		return fmt.Errorf("%w: library id is required", ErrInvalidIdentityAuthorityInput)
	}
	if _, err := canonicalIdentityUUID(libraryID); err != nil {
		return err
	}
	if !validIdentityKind(kind) {
		return fmt.Errorf("%w: unknown identity kind %q", ErrInvalidIdentityAuthorityInput, kind)
	}
	if strings.TrimSpace(identityID) == "" {
		return fmt.Errorf("%w: identity id is required", ErrInvalidIdentityAuthorityInput)
	}
	if digestVersion != SupportedIdentityDigestVersion {
		return fmt.Errorf("%w: %q", ErrUnsupportedIdentityDigest, digestVersion)
	}
	if !isHexN(digest, 64) || digest != strings.ToLower(digest) {
		return fmt.Errorf("%w: digest must be 64 lowercase hex characters", ErrInvalidIdentityAuthorityInput)
	}
	return nil
}

func canonicalIdentityUUID(value string) (string, error) {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return "", fmt.Errorf("%w: invalid UUID value: %v", ErrInvalidIdentityAuthorityInput, err)
	}
	return parsed.String(), nil
}

// --- canonical semantic projection -----------------------------------------

// identityDigestEncoder builds the exact bytes a digest is taken over. Every
// field is length-prefixed and the field count is written first, so no two
// different projections can encode to the same bytes by shifting a boundary
// between adjacent fields.
type identityDigestEncoder struct {
	kind   IdentityKind
	fields [][]byte
}

func (e *identityDigestEncoder) str(value string) *identityDigestEncoder {
	e.fields = append(e.fields, []byte(value))
	return e
}

func (e *identityDigestEncoder) int64(value int64) *identityDigestEncoder {
	return e.str(strconv.FormatInt(value, 10))
}

// list encodes an ordered list as one field. Order is part of the identity: a
// file's block list is not a set.
func (e *identityDigestEncoder) list(values []string) *identityDigestEncoder {
	var buf []byte
	buf = appendUint64(buf, uint64(len(values)))
	for _, value := range values {
		buf = appendUint64(buf, uint64(len(value)))
		buf = append(buf, value...)
	}
	e.fields = append(e.fields, buf)
	return e
}

func (e *identityDigestEncoder) sum() string {
	h := sha256.New()
	h.Write([]byte(SupportedIdentityDigestVersion))
	h.Write([]byte{0})
	h.Write([]byte(e.kind))
	h.Write([]byte{0})
	var header []byte
	header = appendUint64(header, uint64(len(e.fields)))
	h.Write(header)
	for _, field := range e.fields {
		var length []byte
		length = appendUint64(length, uint64(len(field)))
		h.Write(length)
		h.Write(field)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func appendUint64(dst []byte, value uint64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], value)
	return append(dst, buf[:]...)
}

func newIdentityDigest(kind IdentityKind) *identityDigestEncoder {
	return &identityDigestEncoder{kind: kind}
}

// CommitIdentityDigest binds the complete immutable commits projection read by
// history, ancestry, trash and retention paths. Cassandra stores timestamp
// values at millisecond precision, so createdAt is encoded as the exact
// persisted Unix-millisecond value. UUID fields are parsed and re-encoded
// canonically; text fields are encoded as supplied so trimming identity data
// cannot merge distinct stored projections.
func CommitIdentityDigest(libraryID, commitID, parentID, rootFSID, creatorID, description string, createdAt time.Time) (string, error) {
	canonicalLibraryID, err := canonicalIdentityUUID(libraryID)
	if err != nil {
		return "", err
	}
	canonicalCreatorID, err := canonicalIdentityUUID(creatorID)
	if err != nil {
		return "", fmt.Errorf("invalid creator id: %w", err)
	}
	return newIdentityDigest(IdentityKindCommit).
		str(canonicalLibraryID).
		str(commitID).
		str(parentID).
		str(rootFSID).
		str(canonicalCreatorID).
		str(description).
		int64(createdAt.UnixMilli()).
		sum(), nil
}

// DirectoryIdentityDigest binds the exact directory entries traversal consumes.
// The raw stored string is used, not a re-marshaled form: fs ids are content
// addresses over exact bytes, so re-encoding the entries would change the very
// thing being attested. The fixed subtype is part of the fs_object projection,
// not the claim key.
func DirectoryIdentityDigest(libraryID, fsID, directoryEntries string) (string, error) {
	canonicalLibraryID, err := canonicalIdentityUUID(libraryID)
	if err != nil {
		return "", err
	}
	return newIdentityDigest(IdentityKindFSObject).
		str(canonicalLibraryID).
		str(fsID).
		str("dir").
		str(directoryEntries).
		sum(), nil
}

// FileIdentityDigest binds object type, size, the ordered logical Seafile SHA-1
// ids AND the ordered canonical SHA-256 ids whenever the row carries them.
//
// Both lists are required inputs. fs_id is derived from the Seafile SHA-1
// representation, so two complete rows can agree on fs_id, object type, size
// and logical list while naming different canonical block_ids. A digest over
// the logical list alone would let both satisfy one claim, and the physical
// dependency could then change underneath a settled witness.
func FileIdentityDigest(libraryID, fsID string, sizeBytes int64, logicalSHA1IDs, canonicalSHA256IDs []string) (string, error) {
	canonicalLibraryID, err := canonicalIdentityUUID(libraryID)
	if err != nil {
		return "", err
	}
	return newIdentityDigest(IdentityKindFSObject).
		str(canonicalLibraryID).
		str(fsID).
		str("file").
		int64(sizeBytes).
		list(normalizeIdentityBlockIDs(logicalSHA1IDs)).
		list(normalizeIdentityBlockIDs(canonicalSHA256IDs)).
		sum(), nil
}

// normalizeIdentityBlockIDs applies the same trim/lowercase normalization the
// rest of the block plumbing uses, so a claim is not decided by incidental
// casing or padding. It preserves order and does NOT deduplicate: a repeated
// block is a repeated dependency.
func normalizeIdentityBlockIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(ids))
	for _, id := range ids {
		normalized = append(normalized, NormalizeBlockID(id))
	}
	return normalized
}

// --- claim primitive --------------------------------------------------------

// ClaimIdentityAuthority makes the durable, write-once, no-TTL claim for one
// identity under the protocol's canonical global SERIAL domain.
//
// SerialConsistency is pinned to LibraryHeadSerialConsistency explicitly and is
// never inherited from database.serial_consistency / CASSANDRA_SERIAL_CONSISTENCY:
// a supported multi-DC deployment may legitimately set those to LOCAL_SERIAL,
// and a per-DC Paxos domain gives no cross-DC no-conflicting-writer guarantee,
// which is the whole point of the claim.
//
// Outcomes: Established on first write, Idempotent when the stored claim
// matches, Conflict when it does not, Unknown on any driver error including an
// ambiguous CAS. Unknown is not provenance and must fail closed at the caller.
func ClaimIdentityAuthority(ctx context.Context, session *gocql.Session, libraryID string, kind IdentityKind, identityID, digestVersion, digest string) (IdentityClaimResult, error) {
	return ClaimIdentityAuthorityAt(ctx, session, libraryID, kind, identityID, digestVersion, digest, canonicalIdentityCreatedAt(time.Now()))
}

// ClaimIdentityAuthorityAt is the timestamp-explicit form used by the
// production gateway. For commits the same millisecond timestamp is part of
// the digest, stored in the claim and materialized into commits.created_at.
// Keeping this parameter explicit prevents the primitive from silently
// generating a different timestamp than the projection it protects.
func ClaimIdentityAuthorityAt(ctx context.Context, session *gocql.Session, libraryID string, kind IdentityKind, identityID, digestVersion, digest string, createdAt time.Time) (IdentityClaimResult, error) {
	if session == nil {
		return IdentityClaimResult{Outcome: IdentityClaimUnknown}, fmt.Errorf("%w: nil Cassandra session", ErrInvalidIdentityAuthorityInput)
	}
	canonicalLibraryID, err := canonicalIdentityUUID(libraryID)
	if err != nil {
		return IdentityClaimResult{Outcome: IdentityClaimUnknown}, err
	}
	if err := validateIdentityAuthorityInput(canonicalLibraryID, kind, identityID, digestVersion, digest); err != nil {
		return IdentityClaimResult{Outcome: IdentityClaimUnknown}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return IdentityClaimResult{Outcome: IdentityClaimUnknown}, err
	}

	createdAt = canonicalIdentityCreatedAt(createdAt)

	existing := map[string]interface{}{}
	applied, err := session.Query(`
		INSERT INTO identity_authority_claims (library_id, identity_kind, identity_id, digest_version, digest, created_at)
		VALUES (?, ?, ?, ?, ?, ?) IF NOT EXISTS
	`, canonicalLibraryID, string(kind), identityID, digestVersion, digest, createdAt).
		WithContext(ctx).
		SerialConsistency(LibraryHeadSerialConsistency).
		MapScanCAS(existing)
	if err != nil {
		return IdentityClaimResult{Outcome: IdentityClaimUnknown}, fmt.Errorf("claim identity authority %s/%s: %w", kind, identityID, err)
	}
	if applied {
		return IdentityClaimResult{Outcome: IdentityClaimEstablished}, nil
	}

	stored := identityClaimFromCAS(canonicalLibraryID, kind, identityID, existing)
	return IdentityClaimResult{Outcome: classifyIdentityClaim(stored, digestVersion, digest), Stored: stored}, nil
}

// classifyIdentityClaim decides what a non-applied claim means. Only an exact
// match on both version and digest is a retry; everything else, including a
// stored claim that could not be read back, is a conflict. A missing read-back
// is not treated as "probably the same": the caller asked to claim a digest and
// something else already owns the key.
// canonicalIdentityCreatedAt matches Cassandra timestamp precision and is
// shared by commit digest, claim row and source row.
func canonicalIdentityCreatedAt(value time.Time) time.Time {
	return time.UnixMilli(value.UnixMilli()).UTC()
}

func classifyIdentityClaim(stored *IdentityAuthorityClaim, digestVersion, digest string) IdentityClaimOutcome {
	if stored != nil && stored.DigestVersion == digestVersion && stored.Digest == digest {
		return IdentityClaimIdempotent
	}
	return IdentityClaimConflict
}

func identityClaimFromCAS(libraryID string, kind IdentityKind, identityID string, state map[string]interface{}) *IdentityAuthorityClaim {
	if len(state) == 0 {
		return nil
	}
	claim := &IdentityAuthorityClaim{LibraryID: libraryID, Kind: kind, IdentityID: identityID}
	if version, ok := state["digest_version"].(string); ok {
		claim.DigestVersion = version
	}
	if digest, ok := state["digest"].(string); ok {
		claim.Digest = digest
	}
	if createdAt, ok := state["created_at"].(time.Time); ok {
		claim.CreatedAt = createdAt
	}
	if claim.DigestVersion == "" && claim.Digest == "" {
		return nil
	}
	return claim
}

// ReadIdentityAuthority resolves one stored claim. ok == false means no claim
// exists, which is UNPROVEN and never "probably fine": the claim deliberately
// survives deletion of its source row, so an absent claim with a present row
// means the row was never claimed, not that its claim expired.
//
// The read is SERIAL so it observes any in-progress claim's Paxos state rather
// than a stale replica, in the same domain the claim is written in.
func ReadIdentityAuthority(ctx context.Context, session *gocql.Session, libraryID string, kind IdentityKind, identityID string) (IdentityAuthorityClaim, bool, error) {
	claim := IdentityAuthorityClaim{LibraryID: libraryID, Kind: kind, IdentityID: identityID}
	if session == nil {
		return claim, false, fmt.Errorf("%w: nil Cassandra session", ErrInvalidIdentityAuthorityInput)
	}
	canonicalLibraryID, err := canonicalIdentityUUID(libraryID)
	if err != nil {
		return claim, false, err
	}
	if !validIdentityKind(kind) || strings.TrimSpace(identityID) == "" {
		return claim, false, fmt.Errorf("%w: library id, kind and identity id are required", ErrInvalidIdentityAuthorityInput)
	}
	claim.LibraryID = canonicalLibraryID
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return claim, false, err
	}

	err = session.Query(`
		SELECT digest_version, digest, created_at
		FROM identity_authority_claims
		WHERE library_id = ? AND identity_kind = ? AND identity_id = ?
	`, canonicalLibraryID, string(kind), identityID).
		WithContext(ctx).
		Consistency(IdentityAuthorityReadConsistency).
		Scan(&claim.DigestVersion, &claim.Digest, &claim.CreatedAt)
	if err != nil {
		if errors.Is(err, gocql.ErrNotFound) {
			return claim, false, nil
		}
		return claim, false, fmt.Errorf("read identity authority %s/%s: %w", kind, identityID, err)
	}
	return claim, true, nil
}

// IdentityVerificationOutcome keeps a known-absent claim distinct from an
// unavailable or ambiguous authority read. Only Verified is positive proof.
type IdentityVerificationOutcome uint8

const (
	IdentityVerificationUnknown IdentityVerificationOutcome = iota
	IdentityVerificationVerified
	IdentityVerificationUnproven
	IdentityVerificationConflict
)

func (o IdentityVerificationOutcome) String() string {
	switch o {
	case IdentityVerificationVerified:
		return "verified"
	case IdentityVerificationUnproven:
		return "unproven"
	case IdentityVerificationConflict:
		return "conflict"
	default:
		return "unknown"
	}
}

func classifyIdentityVerification(claim *IdentityAuthorityClaim, found bool, digestVersion, digest string) IdentityVerificationOutcome {
	if !found {
		return IdentityVerificationUnproven
	}
	if claim != nil && claim.DigestVersion == digestVersion && claim.Digest == digest {
		return IdentityVerificationVerified
	}
	return IdentityVerificationConflict
}

// VerifyIdentityAuthority compares a recomputed projection digest against the
// stored claim. A known-absent claim is Unproven; an unavailable or ambiguous
// authority read is Unknown; a mismatch is Conflict. Neither absence nor
// mismatch may be repaired here.
func VerifyIdentityAuthority(ctx context.Context, session *gocql.Session, libraryID string, kind IdentityKind, identityID, digestVersion, digest string) (IdentityVerificationOutcome, error) {
	if err := validateIdentityAuthorityInput(libraryID, kind, identityID, digestVersion, digest); err != nil {
		return IdentityVerificationUnknown, err
	}
	claim, found, err := ReadIdentityAuthority(ctx, session, libraryID, kind, identityID)
	if err != nil {
		return IdentityVerificationUnknown, err
	}
	if !found {
		return IdentityVerificationUnproven, nil
	}
	return classifyIdentityVerification(&claim, true, digestVersion, digest), nil
}
