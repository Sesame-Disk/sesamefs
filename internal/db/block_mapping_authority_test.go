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
// promotion did NOT do (no proof, no claim, no freeze) as well as what it
// returned.
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
	freezeState  *BlockMappingProjectionState
	freezeErr    error

	authorityReads int
	candidateReads int
	proofs         int
	claims         []blockMappingProvenance
	freezes        []string
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
		freeze: func(_ context.Context, _ blockMappingIdentity, authority string) (BlockMappingProjectionState, error) {
			f.freezes = append(f.freezes, authority)
			if f.freezeState != nil {
				return *f.freezeState, f.freezeErr
			}
			return BlockMappingProjectionFrozen, f.freezeErr
		},
	}
}

func storedMappingClaim(internalID string) *BlockMappingAuthorityClaim {
	return &BlockMappingAuthorityClaim{InternalID: internalID, ContractVersion: SupportedBlockMappingAuthorityContract, Evidence: BlockMappingEvidencePhysicalBytesV1}
}

func projectionState(state BlockMappingProjectionState) *BlockMappingProjectionState { return &state }

func TestPromoteBlockMappingAuthorityClaimsOnlyProvedCandidateThenFreezes(t *testing.T) {
	fake := &fakeMappingPorts{candidate: testMappingA, candidateOK: true, claimOutcome: IdentityClaimEstablished}
	result, err := promoteBlockMappingAuthority(context.Background(), fake.ports(), testMappingIdentity(t))
	if err != nil || result.Outcome != BlockMappingAuthorityPromoted || result.Authority != testMappingA {
		t.Fatalf("absent claim + valid evidence = %+v, %v; want promoted to A", result, err)
	}
	if fake.proofs != 1 || len(fake.claims) != 1 || fake.claims[0].internalID != testMappingA || fake.claims[0].evidence != BlockMappingEvidencePhysicalBytesV1 {
		t.Fatalf("promotion claimed %+v after %d proofs; want exactly the proved candidate", fake.claims, fake.proofs)
	}
	if len(fake.freezes) != 1 || fake.freezes[0] != testMappingA || result.Projection != BlockMappingProjectionFrozen {
		t.Fatalf("promotion must freeze the ordinary projection to the claimed authority: freezes=%v projection=%s", fake.freezes, result.Projection)
	}
	if id, ok := result.ConsumableInternalID(); !ok || id != testMappingA {
		t.Fatalf("promoted and frozen result is not consumable: %q %t", id, ok)
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
			if len(fake.freezes) != 1 || fake.freezes[0] != testMappingA {
				t.Fatalf("existing authority must still be frozen into the projection as A: %v", fake.freezes)
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
	if id, ok := result.ConsumableInternalID(); !ok || id != testMappingA || result.Candidate != testMappingB {
		t.Fatalf("losing promotion must resolve the durable winner A, not the proved candidate B: authority=%q ok=%t candidate=%q", id, ok, result.Candidate)
	}
	if len(fake.freezes) != 1 || fake.freezes[0] != testMappingA {
		t.Fatalf("losing promotion must freeze the durable winner A, never its own candidate: %v", fake.freezes)
	}
}

// M18 temporal half: a claim whose projection is not frozen, or already
// diverged, is never consumable.
func TestBlockMappingClaimWithoutFrozenProjectionIsNotConsumable(t *testing.T) {
	for _, state := range []BlockMappingProjectionState{BlockMappingProjectionDiverged, BlockMappingProjectionUnknown} {
		fake := &fakeMappingPorts{stored: storedMappingClaim(testMappingA), freezeState: projectionState(state), freezeErr: errors.New("projection not frozen")}
		result, err := promoteBlockMappingAuthority(context.Background(), fake.ports(), testMappingIdentity(t))
		if result.Projection != state || err == nil {
			t.Fatalf("freeze state %s = %+v, %v; want the state and its error surfaced", state, result, err)
		}
		if _, ok := result.ConsumableInternalID(); ok {
			t.Fatalf("a claim with a %s projection must not be consumable", state)
		}
		if id, claimed := result.claimedInternalID(); !claimed || id != testMappingA {
			t.Fatalf("the durable claim must survive an unfrozen projection unchanged: %q %t", id, claimed)
		}
	}
}

// M18 freeze rule: a projection that already resolves elsewhere is reported
// diverged and is never overwritten from the promotion path.
func TestBlockMappingProjectionDecisionNeverRepairsDivergence(t *testing.T) {
	for _, test := range []struct {
		name   string
		row    blockMappingProjectionRow
		state  BlockMappingProjectionState
		freeze bool
	}{
		{name: "absent", row: blockMappingProjectionRow{}, state: BlockMappingProjectionUnknown, freeze: true},
		{name: "agreeing unfrozen", row: blockMappingProjectionRow{found: true, internalID: testMappingA, writeTime: 1_700_000_000_000_000}, state: BlockMappingProjectionUnknown, freeze: true},
		{name: "agreeing frozen", row: blockMappingProjectionRow{found: true, internalID: testMappingA, writeTime: BlockMappingProjectionFrozenTimestamp}, state: BlockMappingProjectionFrozen},
		{name: "diverged unfrozen", row: blockMappingProjectionRow{found: true, internalID: testMappingB, writeTime: 1_700_000_000_000_000}, state: BlockMappingProjectionDiverged},
		{name: "diverged frozen", row: blockMappingProjectionRow{found: true, internalID: testMappingB, writeTime: BlockMappingProjectionFrozenTimestamp}, state: BlockMappingProjectionDiverged},
	} {
		state, freeze := blockMappingProjectionDecision(test.row, testMappingA)
		if state != test.state || freeze != test.freeze {
			t.Fatalf("%s projection decision = %s/%t, want %s/%t; a diverged projection must never be overwritten", test.name, state, freeze, test.state, test.freeze)
		}
	}
	if BlockMappingProjectionFrozenTimestamp <= 4_102_444_800_000_000 {
		t.Fatalf("frozen timestamp %d does not dominate wall-clock microseconds", BlockMappingProjectionFrozenTimestamp)
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
		if len(fake.claims) != 0 || len(fake.freezes) != 0 {
			t.Fatalf("converged mutable mapping without provenance was claimed or frozen: claims=%+v freezes=%v", fake.claims, fake.freezes)
		}
		if _, ok := result.ConsumableInternalID(); ok {
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
		{name: "malformed stored claim", fake: fakeMappingPorts{stored: &BlockMappingAuthorityClaim{InternalID: "not-a-sha256", ContractVersion: SupportedBlockMappingAuthorityContract, Evidence: BlockMappingEvidencePhysicalBytesV1}}, want: BlockMappingAuthorityConflict},
		{name: "unsupported stored contract", fake: fakeMappingPorts{stored: &BlockMappingAuthorityClaim{InternalID: testMappingA, ContractVersion: "V0", Evidence: BlockMappingEvidencePhysicalBytesV1}}, want: BlockMappingAuthorityConflict},
		{name: "ambiguous claim settled absent", fake: fakeMappingPorts{candidate: testMappingA, candidateOK: true, claimErr: unavailable}, want: BlockMappingAuthorityUnavailable, claims: 1},
		{name: "ambiguous claim unsettled", fake: fakeMappingPorts{candidate: testMappingA, candidateOK: true, claimErr: unavailable, settleErr: unavailable}, want: BlockMappingAuthorityUnknown, claims: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := test.fake
			result, _ := promoteBlockMappingAuthority(context.Background(), fake.ports(), testMappingIdentity(t))
			if result.Outcome != test.want {
				t.Fatalf("outcome = %s, want %s", result.Outcome, test.want)
			}
			if _, ok := result.ConsumableInternalID(); ok {
				t.Fatalf("%s produced a consumable authority: %+v", test.name, result)
			}
			if len(fake.claims) != test.claims || len(fake.freezes) != 0 {
				t.Fatalf("claims = %d (want %d), freezes = %v (want none)", len(fake.claims), test.claims, fake.freezes)
			}
		})
	}
}

// Stored evidence is part of the claim contract: only the one rule this code
// implements is usable authority.
func TestStoredBlockMappingClaimRequiresSupportedEvidence(t *testing.T) {
	for _, evidence := range []string{"", "integration_injected", "unsupported_future_rule", strings.ToUpper(BlockMappingEvidencePhysicalBytesV1)} {
		claim := storedMappingClaim(testMappingA)
		claim.Evidence = evidence
		if validStoredBlockMappingClaim(claim) {
			t.Fatalf("stored claim with evidence %q was accepted as usable authority", evidence)
		}
		fake := &fakeMappingPorts{stored: claim}
		result, err := promoteBlockMappingAuthority(context.Background(), fake.ports(), testMappingIdentity(t))
		if result.Outcome != BlockMappingAuthorityConflict || err == nil {
			t.Fatalf("stored claim with evidence %q = %+v, %v; want unusable conflict", evidence, result, err)
		}
		if _, ok := result.ConsumableInternalID(); ok || len(fake.freezes) != 0 {
			t.Fatalf("stored claim with evidence %q became consumable or froze the projection", evidence)
		}
	}
	if !validStoredBlockMappingClaim(storedMappingClaim(testMappingA)) {
		t.Fatal("a physical_bytes_v1 claim must be usable")
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
	proof, err := blockMappingProvenanceFromEvidence(identity, internal, PlainBlockRepresentationID, external, internal)
	if err != nil || proof.internalID != internal || proof.identity != identity || proof.evidence != BlockMappingEvidencePhysicalBytesV1 {
		t.Fatalf("matching content digests = %+v, %v", proof, err)
	}
	otherSHA1 := strings.Repeat("f", 40)
	if _, err := blockMappingProvenanceFromEvidence(identity, internal, PlainBlockRepresentationID, otherSHA1, internal); !errors.Is(err, errBlockMappingEvidenceMismatch) {
		t.Fatalf("stored bytes that only match the SHA-256 candidate were accepted as provenance: %v", err)
	}
	if _, err := blockMappingProvenanceFromEvidence(identity, internal, PlainBlockRepresentationID, external, testMappingB); !errors.Is(err, errBlockMappingEvidenceMismatch) {
		t.Fatalf("stored bytes that do not hash to the candidate were accepted: %v", err)
	}
}

// The hash relation alone does not prove the claimed identity: the canonical
// block must belong to the representation being claimed.
func TestBlockMappingProvenanceBindsRepresentation(t *testing.T) {
	content := []byte("pc-d1b3 representation binding")
	sha1Sum, sha256Sum := sha1.Sum(content), sha256.Sum256(content)
	external, internal := hex.EncodeToString(sha1Sum[:]), hex.EncodeToString(sha256Sum[:])
	identity, err := canonicalBlockMappingIdentity(testMappingOrgID, PlainBlockRepresentationID, external)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	for _, blockRepresentation := range []string{"library:22222222-3333-4444-5555-666666666666", ""} {
		if _, err := blockMappingProvenanceFromEvidence(identity, internal, blockRepresentation, external, internal); !errors.Is(err, errBlockMappingEvidenceMismatch) {
			t.Fatalf("block in representation %q proved a %s mapping: %v", blockRepresentation, PlainBlockRepresentationID, err)
		}
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

type fakeProjection struct {
	internalID string
	frozen     bool
}

type fakeContinuityMappingAuthority struct {
	results       map[string]BlockMappingPromotionResult
	errs          map[string]error
	projections   map[string]fakeProjection
	projectionErr error
	blockReps     map[string]string
	blockRepErr   error
	promotes      int
}

func (f *fakeContinuityMappingAuthority) promote(_ context.Context, _, _, externalID string) (BlockMappingPromotionResult, error) {
	f.promotes++
	return f.results[externalID], f.errs[externalID]
}

func (f *fakeContinuityMappingAuthority) readProjection(_ context.Context, _, _, externalID string) (string, bool, bool, error) {
	if f.projectionErr != nil {
		return "", false, false, f.projectionErr
	}
	projection, ok := f.projections[externalID]
	return projection.internalID, projection.frozen, ok, nil
}

func (f *fakeContinuityMappingAuthority) canonicalRepresentation(_ context.Context, _, blockID string) (string, bool, error) {
	if f.blockRepErr != nil {
		return "", false, f.blockRepErr
	}
	representation, ok := f.blockReps[blockID]
	if !ok && f.blockReps == nil {
		return PlainBlockRepresentationID, true, nil
	}
	return representation, ok, nil
}

func newMappingTestWalker(authority continuityMappingAuthority) *continuityTreeWalker {
	return &continuityTreeWalker{
		ctx:              context.Background(),
		orgID:            testMappingOrgID,
		representation:   PlainBlockRepresentationID,
		mappingAuthority: authority,
	}
}

func frozenPromotion(outcome BlockMappingAuthorityOutcome, authority string) BlockMappingPromotionResult {
	return BlockMappingPromotionResult{Outcome: outcome, Authority: authority, Projection: BlockMappingProjectionFrozen}
}

func TestContinuityWalkerResolvesSHA1OnlyThroughMappingAuthority(t *testing.T) {
	other := strings.Repeat("e", 40)
	authority := &fakeContinuityMappingAuthority{results: map[string]BlockMappingPromotionResult{
		testMappingExternal: frozenPromotion(BlockMappingAuthorityPromoted, testMappingA),
		other:               frozenPromotion(BlockMappingAuthorityAlreadyAuthoritative, testMappingB),
	}}
	walker := newMappingTestWalker(authority)
	resolved, err := walker.resolveBlockIDs([]string{testMappingExternal, other, testMappingExternal}, nil)
	if err != nil {
		t.Fatalf("SHA1-only with authority: %v", err)
	}
	if strings.Join(resolved, ",") != strings.Join([]string{testMappingA, testMappingB, testMappingA}, ",") {
		t.Fatalf("SHA1-only resolved to %v; want ordered authority values", resolved)
	}
	if authority.promotes != 2 {
		t.Fatalf("repeated SHA-1 dependency promoted %d times, want once per identity", authority.promotes)
	}
	if walker.mappingResolved[testMappingExternal] != testMappingA || walker.mappingResolved[other] != testMappingB {
		t.Fatalf("resolved mappings were not recorded for final revalidation: %v", walker.mappingResolved)
	}
}

// M18: a claim is consumed only with a frozen projection. A diverged
// projection is identity_conflict and never resolves; an unverifiable one is
// UNKNOWN.
func TestContinuityWalkerRequiresFrozenProjection(t *testing.T) {
	for _, test := range []struct {
		projection BlockMappingProjectionState
		outcome    LibraryBaselineCertificationOutcome
		reason     LibraryBaselineCertificationReason
	}{
		{BlockMappingProjectionDiverged, LibraryBaselineCertificationNotCertified, LibraryBaselineReasonIdentityConflict},
		{BlockMappingProjectionUnknown, LibraryBaselineCertificationUnknown, LibraryBaselineReasonIdentityAuthorityUnavailable},
	} {
		for _, outcome := range []BlockMappingAuthorityOutcome{BlockMappingAuthorityAlreadyAuthoritative, BlockMappingAuthorityConflict} {
			authority := &fakeContinuityMappingAuthority{results: map[string]BlockMappingPromotionResult{
				testMappingExternal: {Outcome: outcome, Authority: testMappingA, Candidate: testMappingB, Projection: test.projection},
			}}
			before := testutil.ToFloat64(metrics.LibraryContinuityMappingAuthorityDivergenceTotal)
			resolved, err := newMappingTestWalker(authority).resolveBlockIDs([]string{testMappingExternal}, nil)
			if resolved != nil {
				t.Fatalf("%s projection resolved %v; a claim without a frozen projection must never resolve", test.projection, resolved)
			}
			if gotOutcome, gotReason := classifyContinuityDependencyError(err); gotOutcome != test.outcome || gotReason != test.reason {
				t.Fatalf("%s projection = %s/%s (%v), want %s/%s", test.projection, gotOutcome, gotReason, err, test.outcome, test.reason)
			}
			after := testutil.ToFloat64(metrics.LibraryContinuityMappingAuthorityDivergenceTotal)
			if test.projection == BlockMappingProjectionDiverged && after != before+1 {
				t.Fatalf("diverged projection was not reported: before=%v after=%v", before, after)
			}
		}
	}
}

// The consumed claim must still name a canonical block in this library's
// representation domain.
func TestContinuityWalkerBindsMappedBlockRepresentation(t *testing.T) {
	authority := &fakeContinuityMappingAuthority{
		results:   map[string]BlockMappingPromotionResult{testMappingExternal: frozenPromotion(BlockMappingAuthorityAlreadyAuthoritative, testMappingA)},
		blockReps: map[string]string{testMappingA: "library:22222222-3333-4444-5555-666666666666"},
	}
	resolved, err := newMappingTestWalker(authority).resolveBlockIDs([]string{testMappingExternal}, nil)
	if resolved != nil || !errors.Is(err, errContinuityIdentityConflict) {
		t.Fatalf("mapped block in another representation resolved %v, %v; want identity_conflict", resolved, err)
	}
	authority.blockReps = map[string]string{testMappingA: ""}
	if _, err := newMappingTestWalker(authority).resolveBlockIDs([]string{testMappingExternal}, nil); !errors.Is(err, errContinuityIdentityConflict) {
		t.Fatalf("mapped block with an empty representation = %v; want identity_conflict", err)
	}
	authority.blockReps = map[string]string{}
	if resolved, err := newMappingTestWalker(authority).resolveBlockIDs([]string{testMappingExternal}, nil); err != nil || len(resolved) != 1 {
		t.Fatalf("a missing block row is left to the physical proof: %v, %v", resolved, err)
	}
	authority.blockRepErr = errors.New("timeout")
	if _, err := newMappingTestWalker(authority).resolveBlockIDs([]string{testMappingExternal}, nil); !errors.Is(err, errContinuityIdentityUnavailable) {
		t.Fatalf("unreadable block representation = %v; want UNKNOWN", err)
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
		{BlockMappingPromotionResult{Outcome: BlockMappingAuthorityUnknown, Authority: testMappingA, Projection: BlockMappingProjectionFrozen}, LibraryBaselineCertificationUnknown, LibraryBaselineReasonIdentityAuthorityUnavailable},
	} {
		authority := &fakeContinuityMappingAuthority{results: map[string]BlockMappingPromotionResult{testMappingExternal: test.result}}
		_, err := newMappingTestWalker(authority).resolveBlockIDs([]string{testMappingExternal}, nil)
		if outcome, reason := classifyContinuityDependencyError(err); outcome != test.outcome || reason != test.reason {
			t.Fatalf("mapping authority %s = %s/%s (%v), want %s/%s", test.result.Outcome, outcome, reason, err, test.outcome, test.reason)
		}
	}
}

// M18 temporal leg: immediately before the witness the projection must still
// be frozen to the authority and the block still in this representation.
func TestRevalidateContinuityMappingAuthorityBeforeWitness(t *testing.T) {
	mappings := map[string]string{testMappingExternal: testMappingA}
	if err := revalidateContinuityMappingAuthority(context.Background(), nil, testMappingOrgID, PlainBlockRepresentationID, nil); err != nil {
		t.Fatalf("no SHA-1-only dependencies needs no mapping recheck: %v", err)
	}
	for name, test := range map[string]struct {
		authority *fakeContinuityMappingAuthority
		outcome   LibraryBaselineCertificationOutcome
	}{
		"diverged value":     {&fakeContinuityMappingAuthority{projections: map[string]fakeProjection{testMappingExternal: {testMappingB, true}}}, LibraryBaselineCertificationNotCertified},
		"unfrozen same":      {&fakeContinuityMappingAuthority{projections: map[string]fakeProjection{testMappingExternal: {testMappingA, false}}}, LibraryBaselineCertificationNotCertified},
		"absent projection":  {&fakeContinuityMappingAuthority{projections: map[string]fakeProjection{}}, LibraryBaselineCertificationNotCertified},
		"other rep":          {&fakeContinuityMappingAuthority{projections: map[string]fakeProjection{testMappingExternal: {testMappingA, true}}, blockReps: map[string]string{testMappingA: "library:22222222-3333-4444-5555-666666666666"}}, LibraryBaselineCertificationNotCertified},
		"block row vanished": {&fakeContinuityMappingAuthority{projections: map[string]fakeProjection{testMappingExternal: {testMappingA, true}}, blockReps: map[string]string{}}, LibraryBaselineCertificationNotCertified},
		"projection timeout": {&fakeContinuityMappingAuthority{projectionErr: errors.New("timeout")}, LibraryBaselineCertificationUnknown},
	} {
		err := revalidateContinuityMappingAuthority(context.Background(), test.authority, testMappingOrgID, PlainBlockRepresentationID, mappings)
		if outcome, _ := classifyContinuityDependencyError(err); err == nil || outcome != test.outcome {
			t.Fatalf("%s before witness = %s (%v), want %s", name, outcome, err, test.outcome)
		}
	}
	if err := revalidateContinuityMappingAuthority(context.Background(), nil, testMappingOrgID, PlainBlockRepresentationID, mappings); err == nil {
		t.Fatal("missing mapping authority before witness must fail closed")
	}
	frozen := &fakeContinuityMappingAuthority{projections: map[string]fakeProjection{testMappingExternal: {testMappingA, true}}}
	if err := revalidateContinuityMappingAuthority(context.Background(), frozen, testMappingOrgID, PlainBlockRepresentationID, mappings); err != nil {
		t.Fatalf("frozen agreeing projection before witness: %v", err)
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

func TestContinuityWalkerPairedFilesDoNotUseMappingAuthority(t *testing.T) {
	authority := &fakeContinuityMappingAuthority{results: map[string]BlockMappingPromotionResult{testMappingExternal: frozenPromotion(BlockMappingAuthorityPromoted, testMappingB)}}
	resolved, err := newMappingTestWalker(authority).resolveBlockIDs([]string{testMappingA}, []string{testMappingExternal})
	if err != nil || len(resolved) != 1 || resolved[0] != testMappingA {
		t.Fatalf("paired file resolved %v, %v; want its claim-bound canonical A", resolved, err)
	}
	if authority.promotes != 0 {
		t.Fatalf("paired file consulted mapping authority %d times", authority.promotes)
	}
}
