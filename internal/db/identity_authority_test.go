package db

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testCommitIdentityDigest(libraryID, commitID, parentID, rootFSID string) string {
	return CommitIdentityDigest(libraryID, commitID, parentID, rootFSID, "creator", "description", time.UnixMilli(1_700_000_000_123))
}

// PC-D1B identity-authority contracts. These are the primitive's own layer:
// every assertion here is about the claim and the digest, with no certifier and
// no witness in the picture, so the primitive can satisfy its merge contract
// without a consumer that has not landed yet.
//
// M16 (claim lifecycle across a delete and re-create) and M17 (what the file
// digest binds) are the two mutations this file and the integration legs own.

func identityAuthoritySource(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "identity_authority.go"))
	if err != nil {
		t.Fatalf("read identity_authority.go: %v", err)
	}
	return string(raw)
}

// The claim must pin the canonical global SERIAL domain explicitly. Inheriting
// database.serial_consistency / CASSANDRA_SERIAL_CONSISTENCY may yield
// LOCAL_SERIAL, whose per-DC Paxos domain gives no cross-DC
// no-conflicting-writer guarantee.
func TestIdentityAuthorityPinsGlobalSerialExplicitly(t *testing.T) {
	source := identityAuthoritySource(t)
	for _, needle := range []string{
		"INSERT INTO identity_authority_claims",
		"IF NOT EXISTS",
		"SerialConsistency(LibraryHeadSerialConsistency)",
		"Consistency(IdentityAuthorityReadConsistency)",
	} {
		if !strings.Contains(source, needle) {
			t.Errorf("identity authority primitive is missing %q", needle)
		}
	}
	if strings.Contains(source, "LocalSerial") {
		t.Error("identity authority must never use LOCAL_SERIAL")
	}
	if strings.Contains(source, "serial_consistency") && !strings.Contains(source, "never inherited") && !strings.Contains(source, "never derived") {
		t.Error("identity authority must state that it does not inherit the configured serial consistency")
	}
	if got := strings.Count(source, "MapScanCAS("); got != 1 {
		t.Errorf("identity authority MapScanCAS count=%d, want exactly 1", got)
	}
	if strings.Contains(source, "USING TTL") || strings.Contains(source, ".TTL(") {
		t.Error("an identity authority claim must not carry a TTL; it outlives its source row on purpose")
	}
}

// A claim is write-once. Nothing in the primitive may update or delete one:
// the claim deliberately survives deletion of its source row so a re-created
// key cannot mint fresh provenance.
func TestIdentityAuthorityClaimIsWriteOnce(t *testing.T) {
	source := identityAuthoritySource(t)
	for _, forbidden := range []string{
		"UPDATE identity_authority_claims",
		"DELETE FROM identity_authority_claims",
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("identity authority primitive must not contain %q: retiring a claim is a separate fenced protocol (ISSUE-PCD1B-AUTHORITY-CLAIM-RETIREMENT-01)", forbidden)
		}
	}
}

// M17's core property, provable without a certifier: fs_id is derived from the
// Seafile SHA-1 representation, so two complete rows can agree on fs_id, object
// type, size and the logical list while naming different canonical SHA-256
// block ids. If the canonical list is not inside the digest they collapse to
// one claim and the physical dependency can change underneath it.
func TestFileIdentityDigestBindsCanonicalBlockIDs(t *testing.T) {
	const (
		library = "11111111-1111-1111-1111-111111111111"
		fsID    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	logical := []string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	canonicalA := []string{strings.Repeat("a", 64)}
	canonicalB := []string{strings.Repeat("b", 64)}

	digestA := FileIdentityDigest(library, fsID, 1024, logical, canonicalA)
	digestB := FileIdentityDigest(library, fsID, 1024, logical, canonicalB)
	if digestA == digestB {
		t.Fatal("two rows with the same fs_id, type, size and logical list but different canonical SHA-256 lists produced the same authority digest; the canonical list is not bound")
	}

	// And the logical list still matters on its own.
	digestOtherLogical := FileIdentityDigest(library, fsID, 1024, []string{strings.Repeat("c", 40)}, canonicalA)
	if digestOtherLogical == digestA {
		t.Fatal("changing the logical SHA-1 list did not change the digest")
	}
}

// A file with no canonical list (the SHA-1-only shape storeSyncFSObject writes)
// must not collide with the same row once a canonical list exists.
func TestFileIdentityDigestSeparatesSHA1OnlyFromPaired(t *testing.T) {
	const (
		library = "11111111-1111-1111-1111-111111111111"
		fsID    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	logical := []string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	sha1Only := FileIdentityDigest(library, fsID, 7, logical, nil)
	paired := FileIdentityDigest(library, fsID, 7, logical, []string{strings.Repeat("a", 64)})
	if sha1Only == paired {
		t.Fatal("a SHA-1-only identity and the same identity with a canonical list share a digest")
	}
}

// Order is part of a file identity: a block list is not a set.
func TestFileIdentityDigestIsOrderSensitive(t *testing.T) {
	const (
		library = "11111111-1111-1111-1111-111111111111"
		fsID    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	one := strings.Repeat("a", 64)
	two := strings.Repeat("b", 64)
	forward := FileIdentityDigest(library, fsID, 2, nil, []string{one, two})
	reversed := FileIdentityDigest(library, fsID, 2, nil, []string{two, one})
	if forward == reversed {
		t.Fatal("reordering the canonical block list did not change the digest")
	}
}

// Length-prefixed encoding: no two different projections may encode to the same
// bytes by shifting a boundary between adjacent fields.
func TestIdentityDigestFieldBoundariesAreUnambiguous(t *testing.T) {
	left := testCommitIdentityDigest("lib", "ab", "c", "d")
	right := testCommitIdentityDigest("lib", "a", "bc", "d")
	if left == right {
		t.Fatal("adjacent commit fields are not length-delimited: 'ab'+'c' and 'a'+'bc' collide")
	}

	// A directory and a file share one fs_object key; their digest projections
	// stay distinct because each binds its persisted subtype.
	dir := DirectoryIdentityDigest("lib", "fs", "entries")
	file := FileIdentityDigest("lib", "fs", 0, []string{"entries"}, nil)
	if dir == file {
		t.Fatal("directory and file identities are not domain-separated")
	}
}

// Stored identity strings are exact. Block ids keep their explicitly defined
// trim/lowercase normalization, while an fs_id with different bytes is a
// different key and cannot share a claim.
func TestIdentityDigestPreservesStoredIdentityAndNormalizesBlockIDs(t *testing.T) {
	const library = "11111111-1111-1111-1111-111111111111"
	block := strings.Repeat("A", 40)
	withPadding := FileIdentityDigest(library, "fs", 3, []string{" " + block + " "}, nil)
	normalized := FileIdentityDigest(library, "fs", 3, []string{strings.ToLower(block)}, nil)
	if withPadding != normalized {
		t.Fatal("block id digest does not follow the established trim/lowercase normalization")
	}
	differentFSID := FileIdentityDigest(library, " fs ", 3, []string{strings.ToLower(block)}, nil)
	if differentFSID == normalized {
		t.Fatal("distinct stored fs_id values share a digest")
	}
	if len(normalized) != 64 {
		t.Fatalf("digest length=%d, want 64 hex characters", len(normalized))
	}
}

// V1 binds every immutable commits field consumed by history, ancestry,
// trash and retention readers, including timestamp at Cassandra's millisecond
// precision.
func TestCommitIdentityDigestBindsCompleteProjection(t *testing.T) {
	createdAt := time.UnixMilli(1_700_000_000_123)
	base := CommitIdentityDigest("lib", "commit", "parent", "root", "creator", "description", createdAt)
	cases := []struct {
		name string
		got  string
	}{
		{"library", CommitIdentityDigest(" lib", "commit", "parent", "root", "creator", "description", createdAt)},
		{"commit id", CommitIdentityDigest("lib", " commit", "parent", "root", "creator", "description", createdAt)},
		{"parent", CommitIdentityDigest("lib", "commit", " parent", "root", "creator", "description", createdAt)},
		{"root", CommitIdentityDigest("lib", "commit", "parent", " root", "creator", "description", createdAt)},
		{"creator", CommitIdentityDigest("lib", "commit", "parent", "root", " creator", "description", createdAt)},
		{"description", CommitIdentityDigest("lib", "commit", "parent", "root", "creator", " description", createdAt)},
		{"created_at", CommitIdentityDigest("lib", "commit", "parent", "root", "creator", "description", createdAt.Add(time.Millisecond))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got == base {
				t.Fatalf("changing %s did not change the commit identity digest", tc.name)
			}
		})
	}
	if samePersistedTime := CommitIdentityDigest("lib", "commit", "parent", "root", "creator", "description", createdAt.Add(400*time.Microsecond)); samePersistedTime != base {
		t.Fatal("sub-millisecond time noise changed a timestamp that Cassandra persists at millisecond precision")
	}
}

// A claim is refused before it reaches Cassandra unless its inputs are the
// supported version and a well-formed digest. Everything else is Unknown, which
// is not provenance.
func TestClaimIdentityAuthorityValidatesInput(t *testing.T) {
	good := testCommitIdentityDigest("lib", "commit", "", "root")
	cases := []struct {
		name          string
		libraryID     string
		kind          IdentityKind
		identityID    string
		digestVersion string
		digest        string
	}{
		{"empty library", "", IdentityKindCommit, "c", SupportedIdentityDigestVersion, good},
		{"unknown kind", "lib", IdentityKind("block"), "c", SupportedIdentityDigestVersion, good},
		{"empty identity", "lib", IdentityKindCommit, "  ", SupportedIdentityDigestVersion, good},
		{"unsupported version", "lib", IdentityKindCommit, "c", "V0", good},
		{"malformed digest", "lib", IdentityKindCommit, "c", SupportedIdentityDigestVersion, "not-a-digest"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateIdentityAuthorityInput(tc.libraryID, tc.kind, tc.identityID, tc.digestVersion, tc.digest); err == nil {
				t.Fatal("expected validation failure")
			}
			result, err := ClaimIdentityAuthority(context.Background(), nil, tc.libraryID, tc.kind, tc.identityID, tc.digestVersion, tc.digest)
			if err == nil {
				t.Fatal("expected an error with a nil session")
			}
			if result.Outcome != IdentityClaimUnknown {
				t.Fatalf("outcome=%v, want unknown", result.Outcome)
			}
		})
	}
}

// The CAS read-back decides idempotent vs conflict. A stored claim that matches
// is a retry; anything else is a conflict, including a version change.
func TestIdentityClaimFromCASClassification(t *testing.T) {
	digest := testCommitIdentityDigest("lib", "commit", "", "root")

	if claim := identityClaimFromCAS("lib", IdentityKindCommit, "commit", map[string]interface{}{}); claim != nil {
		t.Fatal("an empty CAS map must not produce a stored claim")
	}
	claim := identityClaimFromCAS("lib", IdentityKindCommit, "commit", map[string]interface{}{
		"digest_version": SupportedIdentityDigestVersion,
		"digest":         digest,
	})
	if claim == nil {
		t.Fatal("expected a stored claim")
	}
	if claim.Digest != digest || claim.DigestVersion != SupportedIdentityDigestVersion {
		t.Fatalf("stored claim not carried through: %+v", claim)
	}
}

// M16 at the claim layer: a re-created key with a different digest must be a
// conflict, never a fresh first claim and never a silent retry. Only an exact
// match on version and digest is idempotent.
func TestClassifyIdentityClaimRefusesDifferentDigest(t *testing.T) {
	first := testCommitIdentityDigest("lib", "commit", "", "root-a")
	second := testCommitIdentityDigest("lib", "commit", "", "root-b")
	stored := &IdentityAuthorityClaim{DigestVersion: SupportedIdentityDigestVersion, Digest: first}

	if got := classifyIdentityClaim(stored, SupportedIdentityDigestVersion, first); got != IdentityClaimIdempotent {
		t.Fatalf("identical retry classified as %v, want idempotent", got)
	}
	if got := classifyIdentityClaim(stored, SupportedIdentityDigestVersion, second); got != IdentityClaimConflict {
		t.Fatalf("re-created key with a different digest classified as %v, want conflict", got)
	}
	if got := classifyIdentityClaim(stored, "V0", first); got != IdentityClaimConflict {
		t.Fatalf("same digest under a different version classified as %v, want conflict", got)
	}
	if got := classifyIdentityClaim(nil, SupportedIdentityDigestVersion, first); got != IdentityClaimConflict {
		t.Fatalf("non-applied claim with no read-back classified as %v, want conflict (never a fresh first claim)", got)
	}
}

func TestClassifyIdentityVerificationSeparatesUnprovenFromUnknown(t *testing.T) {
	digest := testCommitIdentityDigest("lib", "commit", "", "root")
	claim := &IdentityAuthorityClaim{DigestVersion: SupportedIdentityDigestVersion, Digest: digest}
	cases := []struct {
		name    string
		claim   *IdentityAuthorityClaim
		found   bool
		version string
		digest  string
		want    IdentityVerificationOutcome
	}{
		{"absent", nil, false, SupportedIdentityDigestVersion, digest, IdentityVerificationUnproven},
		{"matches", claim, true, SupportedIdentityDigestVersion, digest, IdentityVerificationVerified},
		{"different digest", claim, true, SupportedIdentityDigestVersion, strings.Repeat("0", 64), IdentityVerificationConflict},
		{"different version", claim, true, "V0", digest, IdentityVerificationConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyIdentityVerification(tc.claim, tc.found, tc.version, tc.digest); got != tc.want {
				t.Fatalf("verification outcome=%v, want %v", got, tc.want)
			}
		})
	}
	if IdentityVerificationUnknown != 0 {
		t.Fatal("Unknown must be the zero value so an unavailable verification is never positive")
	}
}

func TestIdentityVerificationOutcomeNames(t *testing.T) {
	for outcome, want := range map[IdentityVerificationOutcome]string{
		IdentityVerificationUnknown:  "unknown",
		IdentityVerificationVerified: "verified",
		IdentityVerificationUnproven: "unproven",
		IdentityVerificationConflict: "conflict",
	} {
		if got := outcome.String(); got != want {
			t.Errorf("outcome %d = %q, want %q", outcome, got, want)
		}
	}
}

// Unknown must never be reported as a positive outcome, and its String form is
// part of the operator-visible contract.
func TestIdentityClaimOutcomeNames(t *testing.T) {
	for outcome, want := range map[IdentityClaimOutcome]string{
		IdentityClaimUnknown:     "unknown",
		IdentityClaimEstablished: "established",
		IdentityClaimIdempotent:  "idempotent",
		IdentityClaimConflict:    "conflict",
	} {
		if got := outcome.String(); got != want {
			t.Errorf("outcome %d = %q, want %q", outcome, got, want)
		}
	}
	if IdentityClaimUnknown != 0 {
		t.Error("Unknown must be the zero value so an uninitialized result is never positive")
	}
}
