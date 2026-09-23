package db

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Sesame-Disk/sesamefs/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

const (
	testMappingOrgID    = "11111111-2222-3333-4444-555555555555"
	testMappingExternal = "0123456789abcdef0123456789abcdef01234567"
)

var (
	testMappingA = strings.Repeat("a", 64)
	testMappingB = strings.Repeat("b", 64)
)

func testMappingIdentity(t *testing.T) blockMappingIdentity {
	t.Helper()
	identity, err := canonicalBlockMappingIdentity(testMappingOrgID, PlainBlockRepresentationID, testMappingExternal)
	if err != nil {
		t.Fatalf("canonical mapping identity: %v", err)
	}
	return identity
}

// fakeMappingPorts records every port call so tests can assert what a
// promotion did NOT do (no proof, no claim) as well as what it returned.
type fakeMappingPorts struct {
	stored       *BlockMappingAuthorityClaim
	readErr      error
	settle       *BlockMappingAuthorityClaim
	settleErr    error
	candidate    string
	candidateOK  bool
	candidateErr error
	proveErr     error
	claimOutcome IdentityClaimOutcome
	claimStored  *BlockMappingAuthorityClaim
	claimErr     error

	authorityReads int
	candidateReads int
	proofs         int
	claims         []blockMappingProvenance
}

func (f *fakeMappingPorts) ports() blockMappingPromotionPorts {
	return blockMappingPromotionPorts{
		readAuthority: func(_ context.Context, identity blockMappingIdentity) (BlockMappingAuthorityClaim, bool, error) {
			f.authorityReads++
			if f.authorityReads > 1 {
				if f.settleErr != nil {
					return BlockMappingAuthorityClaim{}, false, f.settleErr
				}
				if f.settle != nil {
					return *f.settle, true, nil
				}
				return BlockMappingAuthorityClaim{}, false, nil
			}
			if f.readErr != nil {
				return BlockMappingAuthorityClaim{}, false, f.readErr
			}
			if f.stored != nil {
				return *f.stored, true, nil
			}
			return BlockMappingAuthorityClaim{}, false, nil
		},
		readCandidate: func(context.Context, blockMappingIdentity) (string, bool, error) {
			f.candidateReads++
			return f.candidate, f.candidateOK, f.candidateErr
		},
		prove: func(_ context.Context, identity blockMappingIdentity, candidate string) (blockMappingProvenance, error) {
			f.proofs++
			if f.proveErr != nil {
				return blockMappingProvenance{}, f.proveErr
			}
			return blockMappingProvenance{identity: identity, internalID: candidate, evidence: BlockMappingEvidencePhysicalBytesV1}, nil
		},
		claim: func(_ context.Context, proof blockMappingProvenance) (IdentityClaimOutcome, *BlockMappingAuthorityClaim, error) {
			f.claims = append(f.claims, proof)
			return f.claimOutcome, f.claimStored, f.claimErr
		},
	}
}

func storedMappingClaim(internalID string) *BlockMappingAuthorityClaim {
	return &BlockMappingAuthorityClaim{InternalID: internalID, ContractVersion: SupportedBlockMappingAuthorityContract, Evidence: BlockMappingEvidencePhysicalBytesV1}
}

func TestPromoteBlockMappingAuthorityClaimsOnlyProvedCandidate(t *testing.T) {
	fake := &fakeMappingPorts{candidate: testMappingA, candidateOK: true, claimOutcome: IdentityClaimEstablished}
	result, err := promoteBlockMappingAuthority(context.Background(), fake.ports(), testMappingIdentity(t))
	if err != nil || result.Outcome != BlockMappingAuthorityPromoted || result.Authority != testMappingA {
		t.Fatalf("absent claim + valid evidence = %+v, %v; want promoted to A", result, err)
	}
	if fake.proofs != 1 || len(fake.claims) != 1 || fake.claims[0].internalID != testMappingA || fake.claims[0].evidence != BlockMappingEvidencePhysicalBytesV1 {
		t.Fatalf("promotion claimed %+v after %d proofs; want exactly the proved candidate", fake.claims, fake.proofs)
	}
	if id, ok := result.AuthoritativeInternalID(); !ok || id != testMappingA {
		t.Fatalf("promoted result is not consumable: %q %t", id, ok)
	}
}

func TestPromoteBlockMappingAuthorityReturnsExistingClaimWithoutReadingMutable(t *testing.T) {
	for _, test := range []struct {
		name      string
		candidate string
	}{
		{name: "same mutable value", candidate: testMappingA},
		{name: "diverged mutable value", candidate: testMappingB},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeMappingPorts{stored: storedMappingClaim(testMappingA), candidate: test.candidate, candidateOK: true}
			result, err := promoteBlockMappingAuthority(context.Background(), fake.ports(), testMappingIdentity(t))
			if err != nil || result.Outcome != BlockMappingAuthorityAlreadyAuthoritative || result.Authority != testMappingA {
				t.Fatalf("existing claim = %+v, %v; want already-authoritative A", result, err)
			}
			if fake.candidateReads != 0 || fake.proofs != 0 || len(fake.claims) != 0 {
				t.Fatalf("existing authority consulted mutable/proof/claim: candidate=%d proofs=%d claims=%d", fake.candidateReads, fake.proofs, len(fake.claims))
			}
		})
	}
}

// M18: a delayed or concurrent ordinary mapping write that proves B cannot
// replace an authority that already fixed A.
func TestBlockMappingAuthorityConflictKeepsDurableWinner(t *testing.T) {
	fake := &fakeMappingPorts{candidate: testMappingB, candidateOK: true, claimOutcome: IdentityClaimConflict, claimStored: storedMappingClaim(testMappingA)}
	result, err := promoteBlockMappingAuthority(context.Background(), fake.ports(), testMappingIdentity(t))
	if err != nil || result.Outcome != BlockMappingAuthorityConflict {
		t.Fatalf("losing promotion = %+v, %v; want conflict", result, err)
	}
	if id, ok := result.AuthoritativeInternalID(); !ok || id != testMappingA || result.Candidate != testMappingB {
		t.Fatalf("losing promotion must resolve the durable winner A, not the proved candidate B: authority=%q ok=%t candidate=%q", id, ok, result.Candidate)
	}
}

// M19: every replica agreeing on the mutable row is convergence, not proof.
func TestBlockMappingConvergenceIsNotProvenance(t *testing.T) {
	for _, proveErr := range []error{errBlockMappingEvidenceMismatch, errBlockMappingEvidenceAbsent} {
		fake := &fakeMappingPorts{candidate: testMappingA, candidateOK: true, proveErr: proveErr, claimOutcome: IdentityClaimEstablished}
		result, err := promoteBlockMappingAuthority(context.Background(), fake.ports(), testMappingIdentity(t))
		if result.Outcome != BlockMappingAuthorityUnproven || !errors.Is(err, proveErr) {
			t.Fatalf("converged mutable mapping without provenance must stay UNPROVEN: %+v, %v", result, err)
		}
		if len(fake.claims) != 0 {
			t.Fatalf("converged mutable mapping without provenance was promoted: %+v", fake.claims)
		}
		if _, ok := result.AuthoritativeInternalID(); ok {
			t.Fatal("unproven promotion produced a consumable authority")
		}
	}
}

func TestPromoteBlockMappingAuthorityFailsClosed(t *testing.T) {
	unavailable := errors.New("cassandra unavailable")
	for _, test := range []struct {
		name   string
		fake   fakeMappingPorts
		want   BlockMappingAuthorityOutcome
		claims int
	}{
		{name: "authority read unavailable", fake: fakeMappingPorts{readErr: unavailable}, want: BlockMappingAuthorityUnavailable},
		{name: "no mutable candidate", fake: fakeMappingPorts{}, want: BlockMappingAuthorityUnproven},
		{name: "candidate read unavailable", fake: fakeMappingPorts{candidateErr: unavailable}, want: BlockMappingAuthorityUnavailable},
		{name: "evidence storage unavailable", fake: fakeMappingPorts{candidate: testMappingA, candidateOK: true, proveErr: unavailable}, want: BlockMappingAuthorityUnavailable},
		{name: "malformed stored claim", fake: fakeMappingPorts{stored: &BlockMappingAuthorityClaim{InternalID: "not-a-sha256", ContractVersion: SupportedBlockMappingAuthorityContract}}, want: BlockMappingAuthorityConflict},
		{name: "unsupported stored contract", fake: fakeMappingPorts{stored: &BlockMappingAuthorityClaim{InternalID: testMappingA, ContractVersion: "V0"}}, want: BlockMappingAuthorityConflict},
		{name: "ambiguous claim settled absent", fake: fakeMappingPorts{candidate: testMappingA, candidateOK: true, claimErr: unavailable}, want: BlockMappingAuthorityUnavailable, claims: 1},
		{name: "ambiguous claim unsettled", fake: fakeMappingPorts{candidate: testMappingA, candidateOK: true, claimErr: unavailable, settleErr: unavailable}, want: BlockMappingAuthorityUnknown, claims: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := test.fake
			result, _ := promoteBlockMappingAuthority(context.Background(), fake.ports(), testMappingIdentity(t))
			if result.Outcome != test.want {
				t.Fatalf("outcome = %s, want %s", result.Outcome, test.want)
			}
			if _, ok := result.AuthoritativeInternalID(); ok {
				t.Fatalf("%s produced a consumable authority: %+v", test.name, result)
			}
			if len(fake.claims) != test.claims {
				t.Fatalf("claims = %d, want %d", len(fake.claims), test.claims)
			}
		})
	}
}

func TestPromoteBlockMappingAuthoritySettlesAmbiguousClaim(t *testing.T) {
	unavailable := errors.New("write timeout")
	same := &fakeMappingPorts{candidate: testMappingA, candidateOK: true, claimErr: unavailable, settle: storedMappingClaim(testMappingA)}
	if result, _ := promoteBlockMappingAuthority(context.Background(), same.ports(), testMappingIdentity(t)); result.Outcome != BlockMappingAuthorityAlreadyAuthoritative || result.Authority != testMappingA {
		t.Fatalf("ambiguous claim settled to same value = %+v; want already-authoritative A", result)
	}
	other := &fakeMappingPorts{candidate: testMappingB, candidateOK: true, claimErr: unavailable, settle: storedMappingClaim(testMappingA)}
	if result, _ := promoteBlockMappingAuthority(context.Background(), other.ports(), testMappingIdentity(t)); result.Outcome != BlockMappingAuthorityConflict || result.Authority != testMappingA {
		t.Fatalf("ambiguous claim settled to another value = %+v; want conflict resolving A", result)
	}
}

func TestBlockMappingProvenanceRequiresBothContentDigests(t *testing.T) {
	content := []byte("pc-d1b3 mapping provenance")
	sha1Sum, sha256Sum := sha1.Sum(content), sha256.Sum256(content)
	external, internal := hex.EncodeToString(sha1Sum[:]), hex.EncodeToString(sha256Sum[:])
	identity, err := canonicalBlockMappingIdentity(testMappingOrgID, PlainBlockRepresentationID, external)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	proof, err := blockMappingProvenanceFromDigests(identity, internal, external, internal)
	if err != nil || proof.internalID != internal || proof.identity != identity || proof.evidence != BlockMappingEvidencePhysicalBytesV1 {
		t.Fatalf("matching content digests = %+v, %v", proof, err)
	}
	otherSHA1 := strings.Repeat("f", 40)
	if _, err := blockMappingProvenanceFromDigests(identity, internal, otherSHA1, internal); !errors.Is(err, errBlockMappingEvidenceMismatch) {
		t.Fatalf("stored bytes that only match the SHA-256 candidate were accepted as provenance: %v", err)
	}
	if _, err := blockMappingProvenanceFromDigests(identity, internal, external, testMappingB); !errors.Is(err, errBlockMappingEvidenceMismatch) {
		t.Fatalf("stored bytes that do not hash to the candidate were accepted: %v", err)
	}
}

func TestCanonicalBlockMappingIdentityIsExact(t *testing.T) {
	for _, test := range []struct {
		name, org, representation, external string
	}{
		{name: "non-uuid org", org: "org", representation: PlainBlockRepresentationID, external: testMappingExternal},
		{name: "unknown representation", org: testMappingOrgID, representation: "bogus", external: testMappingExternal},
		{name: "uppercase external", org: testMappingOrgID, representation: PlainBlockRepresentationID, external: strings.ToUpper(testMappingExternal)},
		{name: "padded external", org: testMappingOrgID, representation: PlainBlockRepresentationID, external: " " + testMappingExternal},
		{name: "sha256 external", org: testMappingOrgID, representation: PlainBlockRepresentationID, external: testMappingA},
	} {
		if _, err := canonicalBlockMappingIdentity(test.org, test.representation, test.external); !errors.Is(err, ErrInvalidBlockMappingAuthorityInput) {
			t.Fatalf("%s accepted as a mapping identity: %v", test.name, err)
		}
	}
}

// --- certifier consumption --------------------------------------------------

type fakeContinuityMappingAuthority struct {
	results    map[string]BlockMappingPromotionResult
	errs       map[string]error
	mutable    map[string]string
	mutableErr error
	promotes   int
}

func (f *fakeContinuityMappingAuthority) promote(_ context.Context, _, _, externalID string) (BlockMappingPromotionResult, error) {
	f.promotes++
	return f.results[externalID], f.errs[externalID]
}

func (f *fakeContinuityMappingAuthority) readMutable(_ context.Context, _, _, externalID string) (string, bool, error) {
	if f.mutableErr != nil {
		return "", false, f.mutableErr
	}
	value, ok := f.mutable[externalID]
	return value, ok, nil
}

func newMappingTestWalker(authority continuityMappingAuthority) *continuityTreeWalker {
	return &continuityTreeWalker{
		ctx:              context.Background(),
		orgID:            testMappingOrgID,
		representation:   PlainBlockRepresentationID,
		mappingAuthority: authority,
	}
}

func TestContinuityWalkerResolvesSHA1OnlyThroughMappingAuthority(t *testing.T) {
	other := strings.Repeat("e", 40)
	authority := &fakeContinuityMappingAuthority{results: map[string]BlockMappingPromotionResult{
		testMappingExternal: {Outcome: BlockMappingAuthorityPromoted, Authority: testMappingA},
		other:               {Outcome: BlockMappingAuthorityAlreadyAuthoritative, Authority: testMappingB},
	}}
	resolved, err := newMappingTestWalker(authority).resolveBlockIDs([]string{testMappingExternal, other, testMappingExternal}, nil)
	if err != nil {
		t.Fatalf("SHA1-only with authority: %v", err)
	}
	if strings.Join(resolved, ",") != strings.Join([]string{testMappingA, testMappingB, testMappingA}, ",") {
		t.Fatalf("SHA1-only resolved to %v; want ordered authority values", resolved)
	}
	if authority.promotes != 2 {
		t.Fatalf("repeated SHA-1 dependency promoted %d times, want once per identity", authority.promotes)
	}
}

// M18: after authority(M)=A, an ordinary mapping write B can neither become the
// resolution nor be witnessed: ordinary readers still resolve through the
// mutable row, so disagreement fails closed exactly like a paired mapping.
func TestContinuityWalkerRejectsMutableMappingDivergingFromAuthority(t *testing.T) {
	for _, outcome := range []BlockMappingAuthorityOutcome{BlockMappingAuthorityAlreadyAuthoritative, BlockMappingAuthorityConflict} {
		authority := &fakeContinuityMappingAuthority{
			results: map[string]BlockMappingPromotionResult{testMappingExternal: {Outcome: outcome, Authority: testMappingA, Candidate: testMappingB}},
			mutable: map[string]string{testMappingExternal: testMappingB},
		}
		before := testutil.ToFloat64(metrics.LibraryContinuityMappingAuthorityDivergenceTotal)
		resolved, err := newMappingTestWalker(authority).resolveBlockIDs([]string{testMappingExternal}, nil)
		if resolved != nil || !errors.Is(err, errContinuityIdentityConflict) {
			t.Fatalf("authority A with mutable B resolved %v, %v; a diverged mutable mapping must be identity_conflict and never resolve", resolved, err)
		}
		if after := testutil.ToFloat64(metrics.LibraryContinuityMappingAuthorityDivergenceTotal); after != before+1 {
			t.Fatalf("mutable/authority divergence was not reported: before=%v after=%v", before, after)
		}
	}
}

func TestContinuityWalkerAcceptsAgreeingOrAbsentMutableMapping(t *testing.T) {
	for name, mutable := range map[string]map[string]string{
		"agreeing": {testMappingExternal: testMappingA},
		"absent":   {},
	} {
		authority := &fakeContinuityMappingAuthority{
			results: map[string]BlockMappingPromotionResult{testMappingExternal: {Outcome: BlockMappingAuthorityAlreadyAuthoritative, Authority: testMappingA}},
			mutable: mutable,
		}
		walker := newMappingTestWalker(authority)
		resolved, err := walker.resolveBlockIDs([]string{testMappingExternal}, nil)
		if err != nil || len(resolved) != 1 || resolved[0] != testMappingA {
			t.Fatalf("%s mutable mapping resolved %v, %v; want authority A", name, resolved, err)
		}
		if walker.mappingResolved[testMappingExternal] != testMappingA {
			t.Fatalf("%s mapping was not recorded for final revalidation: %v", name, walker.mappingResolved)
		}
	}
	unavailable := &fakeContinuityMappingAuthority{
		results:    map[string]BlockMappingPromotionResult{testMappingExternal: {Outcome: BlockMappingAuthorityAlreadyAuthoritative, Authority: testMappingA}},
		mutableErr: errors.New("mapping read timeout"),
	}
	_, err := newMappingTestWalker(unavailable).resolveBlockIDs([]string{testMappingExternal}, nil)
	if outcome, reason := classifyContinuityDependencyError(err); outcome != LibraryBaselineCertificationUnknown || reason != LibraryBaselineReasonIdentityAuthorityUnavailable {
		t.Fatalf("unreadable mutable mapping = %s/%s (%v), want UNKNOWN/identity_authority_unavailable", outcome, reason, err)
	}
}

// M18 temporal leg: a mutable write that lands after the walk but before the
// witness is still refused.
func TestRevalidateContinuityMappingAuthorityBeforeWitness(t *testing.T) {
	mappings := map[string]string{testMappingExternal: testMappingA}
	if err := revalidateContinuityMappingAuthority(context.Background(), nil, testMappingOrgID, PlainBlockRepresentationID, nil); err != nil {
		t.Fatalf("no SHA-1-only dependencies needs no mapping recheck: %v", err)
	}
	diverged := &fakeContinuityMappingAuthority{mutable: map[string]string{testMappingExternal: testMappingB}}
	err := revalidateContinuityMappingAuthority(context.Background(), diverged, testMappingOrgID, PlainBlockRepresentationID, mappings)
	if outcome, reason := classifyContinuityDependencyError(err); outcome != LibraryBaselineCertificationNotCertified || reason != LibraryBaselineReasonIdentityConflict {
		t.Fatalf("mutable write landing before witness = %s/%s (%v), want NOT_CERTIFIED/identity_conflict", outcome, reason, err)
	}
	unreadable := &fakeContinuityMappingAuthority{mutableErr: errors.New("timeout")}
	err = revalidateContinuityMappingAuthority(context.Background(), unreadable, testMappingOrgID, PlainBlockRepresentationID, mappings)
	if outcome, _ := classifyContinuityDependencyError(err); outcome != LibraryBaselineCertificationUnknown {
		t.Fatalf("unreadable mutable mapping before witness = %s (%v), want UNKNOWN", outcome, err)
	}
	err = revalidateContinuityMappingAuthority(context.Background(), nil, testMappingOrgID, PlainBlockRepresentationID, mappings)
	if outcome, _ := classifyContinuityDependencyError(err); outcome != LibraryBaselineCertificationUnknown {
		t.Fatalf("missing mapping authority before witness = %s (%v), want UNKNOWN", outcome, err)
	}
	agreeing := &fakeContinuityMappingAuthority{mutable: map[string]string{testMappingExternal: testMappingA}}
	if err := revalidateContinuityMappingAuthority(context.Background(), agreeing, testMappingOrgID, PlainBlockRepresentationID, mappings); err != nil {
		t.Fatalf("agreeing mutable mapping before witness: %v", err)
	}
}

func TestCertifierRechecksMappingAuthorityBeforeWitness(t *testing.T) {
	sourceBytes, err := os.ReadFile("library_continuity_certifier.go")
	if err != nil {
		t.Fatalf("read certifier source: %v", err)
	}
	source := string(sourceBytes)
	finalFSObject := strings.Index(source, "currentProjection, verifyErr := readContinuityFSObjectProjectionContext")
	recheck := strings.Index(source, "revalidateContinuityMappingAuthority(ctx, mappingAuthority, orgID, representationID, dependencies.sha1Mappings)")
	witness := strings.Index(source, "cas, casErr := CommitLibraryContinuityWitnessContext")
	if finalFSObject < 0 || recheck < 0 || witness < 0 || !(finalFSObject < recheck && recheck < witness) {
		t.Fatalf("mapping authority must be rechecked after final metadata revalidation and before the witness: fs=%d mapping=%d witness=%d", finalFSObject, recheck, witness)
	}
}

func TestContinuityWalkerMapsMappingAuthorityFailuresFailClosed(t *testing.T) {
	for _, test := range []struct {
		result  BlockMappingPromotionResult
		outcome LibraryBaselineCertificationOutcome
		reason  LibraryBaselineCertificationReason
	}{
		{BlockMappingPromotionResult{Outcome: BlockMappingAuthorityUnproven, Candidate: testMappingA}, LibraryBaselineCertificationNotCertified, LibraryBaselineReasonIdentityUnproven},
		{BlockMappingPromotionResult{Outcome: BlockMappingAuthorityConflict}, LibraryBaselineCertificationNotCertified, LibraryBaselineReasonIdentityConflict},
		{BlockMappingPromotionResult{Outcome: BlockMappingAuthorityUnavailable}, LibraryBaselineCertificationUnknown, LibraryBaselineReasonIdentityAuthorityUnavailable},
		{BlockMappingPromotionResult{Outcome: BlockMappingAuthorityUnknown, Authority: testMappingA}, LibraryBaselineCertificationUnknown, LibraryBaselineReasonIdentityAuthorityUnavailable},
	} {
		authority := &fakeContinuityMappingAuthority{results: map[string]BlockMappingPromotionResult{testMappingExternal: test.result}, mutable: map[string]string{testMappingExternal: testMappingA}}
		_, err := newMappingTestWalker(authority).resolveBlockIDs([]string{testMappingExternal}, nil)
		if outcome, reason := classifyContinuityDependencyError(err); outcome != test.outcome || reason != test.reason {
			t.Fatalf("mapping authority %s = %s/%s (%v), want %s/%s", test.result.Outcome, outcome, reason, err, test.outcome, test.reason)
		}
	}
}

func TestContinuityWalkerPairedFilesDoNotUseMappingAuthority(t *testing.T) {
	authority := &fakeContinuityMappingAuthority{results: map[string]BlockMappingPromotionResult{testMappingExternal: {Outcome: BlockMappingAuthorityPromoted, Authority: testMappingB}}}
	resolved, err := newMappingTestWalker(authority).resolveBlockIDs([]string{testMappingA}, []string{testMappingExternal})
	if err != nil || len(resolved) != 1 || resolved[0] != testMappingA {
		t.Fatalf("paired file resolved %v, %v; want its claim-bound canonical A", resolved, err)
	}
	if authority.promotes != 0 {
		t.Fatalf("paired file consulted mapping authority %d times", authority.promotes)
	}
}
