//go:build integration

package integration

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

const (
	blockMappingAuthorityEvidenceEnv            = "SESAMEFS_REQUIRE_BLOCK_MAPPING_AUTHORITY_EVIDENCE"
	blockMappingAuthorityUnavailableEvidenceEnv = "SESAMEFS_REQUIRE_BLOCK_MAPPING_AUTHORITY_UNAVAILABLE_EVIDENCE"
	blockMappingAuthorityUnavailablePhaseEnv    = "SESAMEFS_BLOCK_MAPPING_AUTHORITY_UNAVAILABLE_PHASE"
	blockMappingAuthorityUnavailableRunIDEnv    = "SESAMEFS_BLOCK_MAPPING_AUTHORITY_UNAVAILABLE_RUN_ID"
)

var (
	blockMappingAuthorityEvidence            bool
	blockMappingAuthorityUnavailableEvidence bool
)

// runConcurrently starts every call behind one barrier so the proposals
// genuinely contend for the same Paxos partition.
func runConcurrently(calls ...func()) {
	var ready, done sync.WaitGroup
	start := make(chan struct{})
	for _, call := range calls {
		ready.Add(1)
		done.Add(1)
		go func(call func()) {
			defer done.Done()
			ready.Done()
			<-start
			call()
		}(call)
	}
	ready.Wait()
	close(start)
	done.Wait()
}

// TestBlockMappingAuthority3DC is the multi-DC evidence for PC-D1B.3. Client
// sessions default to LOCAL_SERIAL; every claim and read still decides in the
// global SERIAL domain.
func TestBlockMappingAuthority3DC(t *testing.T) {
	if os.Getenv(blockMappingAuthorityEvidenceEnv) != "1" {
		t.Skipf("%s is not set", blockMappingAuthorityEvidenceEnv)
	}
	endpoints := w2PostHead3DCEndpoints(t)
	na := w2PostHead3DCConnectSerial(t, "dc-na", endpoints, "LOCAL_SERIAL")
	eu := w2PostHead3DCConnectSerial(t, "dc-eu", endpoints, "LOCAL_SERIAL")
	asia := w2PostHead3DCConnectSerial(t, "dc-asia", endpoints, "LOCAL_SERIAL")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	orgID := uuid.NewString()
	storageManager, blockStore := newLibraryBaselineCertifierStorage(t, ctx, orgID, libraryBaselineCertifierMinIOEndpoint, true)

	t.Run("MAPPING-3DC-1 promotion in dc-eu is decided from dc-na", func(t *testing.T) {
		content := newMappingAuthorityContent("3dc-1 " + orgID)
		storeMappingAuthorityBlock(t, ctx, na, blockStore, orgID, content)
		writeMutableBlockMapping(t, na, orgID, content.external, content.internal)
		promoted, err := eu.PromoteBlockMappingAuthority(ctx, storageManager, orgID, dbpkg.PlainBlockRepresentationID, content.external)
		if err != nil || promoted.Outcome != dbpkg.BlockMappingAuthorityPromoted || promoted.Authority != content.internal {
			t.Fatalf("dc-eu promotion = %+v, %v; want promoted", promoted, err)
		}
		requireMappingAuthority(t, ctx, na, orgID, content.external, content.internal)
		requireMappingAuthority(t, ctx, asia, orgID, content.external, content.internal)
		requireFrozenProjection(t, ctx, na, orgID, content.external, content.internal)
		again, err := na.PromoteBlockMappingAuthority(ctx, storageManager, orgID, dbpkg.PlainBlockRepresentationID, content.external)
		if err != nil || again.Outcome != dbpkg.BlockMappingAuthorityAlreadyAuthoritative || again.Authority != content.internal {
			t.Fatalf("dc-na re-promotion = %+v, %v; want already-authoritative", again, err)
		}
		libraryID, head, _ := seedSHA1OnlyMappingLibrary(t, na, orgID, "3dc-1", content)
		result := na.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, head)
		if result.Outcome != dbpkg.LibraryBaselineCertificationCertified || result.UniqueBlocks != 1 {
			t.Fatalf("SHA1-only certification with dc-eu authority = %+v", result)
		}
		requireBaselineWitness(t, na, orgID, libraryID, head)
	})

	t.Run("MAPPING-3DC-1b concurrent same-value promotion converges", func(t *testing.T) {
		for iteration := 0; iteration < 8; iteration++ {
			content := newMappingAuthorityContent("3dc-same " + orgID + " " + uuid.NewString())
			storeMappingAuthorityBlock(t, ctx, na, blockStore, orgID, content)
			writeMutableBlockMapping(t, na, orgID, content.external, content.internal)
			var naResult, euResult dbpkg.BlockMappingPromotionResult
			var naErr, euErr error
			runConcurrently(
				func() {
					naResult, naErr = na.PromoteBlockMappingAuthority(ctx, storageManager, orgID, dbpkg.PlainBlockRepresentationID, content.external)
				},
				func() {
					euResult, euErr = eu.PromoteBlockMappingAuthority(ctx, storageManager, orgID, dbpkg.PlainBlockRepresentationID, content.external)
				},
			)
			if naErr != nil || euErr != nil {
				t.Fatalf("iteration %d concurrent same-value promotion errors: na=%v eu=%v", iteration, naErr, euErr)
			}
			promoted := 0
			for _, result := range []dbpkg.BlockMappingPromotionResult{naResult, euResult} {
				if authority, ok := result.ConsumableInternalID(); !ok || authority != content.internal {
					t.Fatalf("iteration %d same-value promotion result %+v", iteration, result)
				}
				if result.Outcome == dbpkg.BlockMappingAuthorityPromoted {
					promoted++
				}
			}
			if promoted > 1 {
				t.Fatalf("iteration %d: two datacenters both established the claim: na=%+v eu=%+v", iteration, naResult, euResult)
			}
			requireMappingAuthority(t, ctx, asia, orgID, content.external, content.internal)
		}
	})

	t.Run("MAPPING-3DC-2 conflicting proposals never split authority", func(t *testing.T) {
		valueA, valueB := strings.Repeat("a", 64), strings.Repeat("b", 64)
		for iteration := 0; iteration < 20; iteration++ {
			external := newMappingAuthorityContent("3dc-split " + uuid.NewString()).external
			var naOutcome, euOutcome dbpkg.IdentityClaimOutcome
			var naStored, euStored *dbpkg.BlockMappingAuthorityClaim
			var naErr, euErr error
			runConcurrently(
				func() {
					naOutcome, naStored, naErr = dbpkg.ClaimBlockMappingAuthorityForIntegration(ctx, na.Session(), orgID, dbpkg.PlainBlockRepresentationID, external, valueA, dbpkg.BlockMappingEvidenceIntegrationInjected)
				},
				func() {
					euOutcome, euStored, euErr = dbpkg.ClaimBlockMappingAuthorityForIntegration(ctx, eu.Session(), orgID, dbpkg.PlainBlockRepresentationID, external, valueB, dbpkg.BlockMappingEvidenceIntegrationInjected)
				},
			)
			if naErr != nil || euErr != nil {
				t.Fatalf("iteration %d conflicting claims errored: na=%v eu=%v", iteration, naErr, euErr)
			}
			var winner string
			switch {
			case naOutcome == dbpkg.IdentityClaimEstablished && euOutcome == dbpkg.IdentityClaimConflict && euStored != nil && euStored.InternalID == valueA:
				winner = valueA
			case euOutcome == dbpkg.IdentityClaimEstablished && naOutcome == dbpkg.IdentityClaimConflict && naStored != nil && naStored.InternalID == valueB:
				winner = valueB
			default:
				t.Fatalf("iteration %d split or undecided authority: na=%s/%+v eu=%s/%+v", iteration, naOutcome, naStored, euOutcome, euStored)
			}
			for _, database := range []*dbpkg.DB{na, eu, asia} {
				claim, found, err := dbpkg.ReadBlockMappingAuthority(ctx, database.Session(), orgID, dbpkg.PlainBlockRepresentationID, external)
				if err != nil || !found || claim.InternalID != winner {
					t.Fatalf("iteration %d SERIAL read = %+v found=%t err=%v; want winner %s", iteration, claim, found, err, winner)
				}
			}
		}
	})

	t.Run("MAPPING-3DC-4 ordinary writes after promotion are inert in every DC", func(t *testing.T) {
		authoritative := newMappingAuthorityContent("3dc-4 A " + orgID)
		mutable := newMappingAuthorityContent("3dc-4 B " + orgID)
		storeMappingAuthorityBlock(t, ctx, na, blockStore, orgID, authoritative)
		storeMappingAuthorityBlock(t, ctx, na, blockStore, orgID, mutable)
		writeMutableBlockMapping(t, na, orgID, authoritative.external, authoritative.internal)
		if result, err := na.PromoteBlockMappingAuthority(ctx, storageManager, orgID, dbpkg.PlainBlockRepresentationID, authoritative.external); err != nil || result.Outcome != dbpkg.BlockMappingAuthorityPromoted || result.Projection != dbpkg.BlockMappingProjectionFrozen {
			t.Fatalf("establish and freeze authority A: %+v, %v", result, err)
		}
		// An ordinary B write reaches every datacenter after the freeze.
		writeMutableBlockMapping(t, eu, orgID, authoritative.external, mutable.internal)
		for dc, database := range map[string]*dbpkg.DB{"dc-na": na, "dc-eu": eu, "dc-asia": asia} {
			var mapped string
			if err := database.Session().Query(`SELECT internal_id FROM block_id_mappings WHERE org_id = ? AND representation_id = ? AND external_id = ?`,
				orgID, dbpkg.PlainBlockRepresentationID, authoritative.external).Consistency(gocql.LocalOne).Scan(&mapped); err != nil || mapped != authoritative.internal {
				t.Fatalf("%s ordinary readers resolve %q, %v after an inert B write; want A", dc, mapped, err)
			}
			libraryID, head, fileFSID := seedSHA1OnlyMappingLibrary(t, na, orgID, "3dc-4-"+dc, authoritative)
			result := database.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, head)
			if result.Outcome != dbpkg.LibraryBaselineCertificationCertified || result.UniqueBlocks != 1 {
				t.Fatalf("%s certification after an inert B write = %+v", dc, result)
			}
			if !libraryReferenceExists(t, na, orgID, authoritative.internal, libraryID, fileFSID) || libraryReferenceExists(t, na, orgID, mutable.internal, libraryID, fileFSID) {
				t.Fatalf("%s certification did not resolve only the durable authority A", dc)
			}
			requireBaselineWitness(t, na, orgID, libraryID, head)
		}
		requireFrozenProjection(t, ctx, asia, orgID, authoritative.external, authoritative.internal)
	})

	t.Run("MAPPING-3DC-4b unfrozen divergence fails closed without repair", func(t *testing.T) {
		authoritative := newMappingAuthorityContent("3dc-4b A " + orgID)
		mutable := newMappingAuthorityContent("3dc-4b B " + orgID)
		storeMappingAuthorityBlock(t, ctx, na, blockStore, orgID, authoritative)
		storeMappingAuthorityBlock(t, ctx, na, blockStore, orgID, mutable)
		// The claim committed but its promotion stopped before the freeze, then
		// a stale ordinary writer landed B in every datacenter.
		if outcome, _, err := dbpkg.ClaimBlockMappingAuthorityForIntegration(ctx, na.Session(), orgID, dbpkg.PlainBlockRepresentationID, authoritative.external, authoritative.internal, dbpkg.BlockMappingEvidencePhysicalBytesV1); err != nil || outcome != dbpkg.IdentityClaimEstablished {
			t.Fatalf("establish unfrozen claim A: %s, %v", outcome, err)
		}
		writeMutableBlockMapping(t, eu, orgID, authoritative.external, mutable.internal)
		for dc, database := range map[string]*dbpkg.DB{"dc-na": na, "dc-eu": eu, "dc-asia": asia} {
			libraryID, head, fileFSID := seedSHA1OnlyMappingLibrary(t, na, orgID, "3dc-4b-"+dc, authoritative)
			result := database.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, head)
			if result.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || result.Reason != dbpkg.LibraryBaselineReasonIdentityConflict ||
				result.PermanentLivenessWrites != 0 || result.PhysicalRevalidations != 0 {
				t.Fatalf("%s certification with claim A and unfrozen B = %+v; want NOT_CERTIFIED/identity_conflict", dc, result)
			}
			if libraryReferenceExists(t, na, orgID, mutable.internal, libraryID, fileFSID) {
				t.Fatalf("%s certification consumed the mutable mapping B", dc)
			}
			requireNoBaselineWitness(t, na, orgID, libraryID)
		}
		requireMappingAuthority(t, ctx, asia, orgID, authoritative.external, authoritative.internal)
		mapped, frozen, found, err := dbpkg.ReadBlockMappingProjection(ctx, asia.Session(), orgID, dbpkg.PlainBlockRepresentationID, authoritative.external)
		if err != nil || !found || frozen || mapped != mutable.internal {
			t.Fatalf("diverged projection was repaired or frozen: %q frozen=%t found=%t err=%v", mapped, frozen, found, err)
		}
		// Only once the ordinary row agrees again does the mapping freeze and certify.
		writeMutableBlockMapping(t, asia, orgID, authoritative.external, authoritative.internal)
		libraryID, head, fileFSID := seedSHA1OnlyMappingLibrary(t, na, orgID, "3dc-4b-restored", authoritative)
		restored := eu.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, head)
		if restored.Outcome != dbpkg.LibraryBaselineCertificationCertified || !libraryReferenceExists(t, na, orgID, authoritative.internal, libraryID, fileFSID) {
			t.Fatalf("certification after the ordinary row agrees again = %+v; want CERTIFIED resolving A", restored)
		}
		requireFrozenProjection(t, ctx, na, orgID, authoritative.external, authoritative.internal)
		requireBaselineWitness(t, na, orgID, libraryID, head)
	})

	t.Run("MAPPING-3DC-5 converged mapping without provenance is not promoted", func(t *testing.T) {
		forged := newMappingAuthorityContent("3dc-5 forged " + orgID)
		stored := newMappingAuthorityContent("3dc-5 stored " + orgID)
		storeMappingAuthorityBlock(t, ctx, na, blockStore, orgID, stored)
		writeMutableBlockMapping(t, na, orgID, forged.external, stored.internal)
		for dc, database := range map[string]*dbpkg.DB{"dc-na": na, "dc-eu": eu, "dc-asia": asia} {
			var mapped string
			if err := database.Session().Query(`SELECT internal_id FROM block_id_mappings WHERE org_id = ? AND representation_id = ? AND external_id = ?`,
				orgID, dbpkg.PlainBlockRepresentationID, forged.external).Consistency(gocql.LocalOne).Scan(&mapped); err != nil || mapped != stored.internal {
				t.Fatalf("%s converged mutable mapping = %q, %v", dc, mapped, err)
			}
			result, err := database.PromoteBlockMappingAuthority(ctx, storageManager, orgID, dbpkg.PlainBlockRepresentationID, forged.external)
			if result.Outcome != dbpkg.BlockMappingAuthorityUnproven {
				t.Fatalf("%s promoted a converged mapping without provenance: %+v, %v", dc, result, err)
			}
		}
		requireNoMappingAuthority(t, ctx, asia, orgID, forged.external)
		libraryID, head, _ := seedSHA1OnlyMappingLibrary(t, na, orgID, "3dc-5", forged)
		result := eu.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, head)
		if result.Outcome != dbpkg.LibraryBaselineCertificationNotCertified || result.Reason != dbpkg.LibraryBaselineReasonIdentityUnproven ||
			result.PermanentLivenessWrites != 0 || result.PhysicalRevalidations != 0 {
			t.Fatalf("converged unprovable mapping certification = %+v; want identity_unproven before physical/liveness work", result)
		}
		requireNoBaselineWitness(t, na, orgID, libraryID)
	})

	blockMappingAuthorityEvidence = true
	t.Logf("GREEN: MAPPING-3DC-1/1b/2/4/4b/5 — cross-DC decidable promotion, one winner under concurrent same and conflicting proposals, ordinary writes after the freeze are inert in every DC, unfrozen divergence fails closed without repair, convergence is not provenance")
}

// TestBlockMappingAuthorityUnavailable3DC is MAPPING-3DC-3. With dc-eu and
// dc-asia stopped, global SERIAL has no majority: the claim cannot be
// established, promotion is not consumable and certification is UNKNOWN. A
// LOCAL_SERIAL claim would still apply in dc-na, so this leg turns RED if the
// pinned domain regresses. The recover phase proves nothing was established
// during the outage and that the same library then certifies.
func TestBlockMappingAuthorityUnavailable3DC(t *testing.T) {
	phase := strings.TrimSpace(os.Getenv(blockMappingAuthorityUnavailablePhaseEnv))
	if phase == "" {
		t.Skipf("%s is not set", blockMappingAuthorityUnavailablePhaseEnv)
	}
	if phase != "prepare" && phase != "unavailable" && phase != "recover" {
		t.Fatalf("%s=%q, want prepare, unavailable or recover", blockMappingAuthorityUnavailablePhaseEnv, phase)
	}
	if phase != "prepare" && os.Getenv(blockMappingAuthorityUnavailableEvidenceEnv) != "1" {
		t.Fatalf("%s=1 is required for the %s phase", blockMappingAuthorityUnavailableEvidenceEnv, phase)
	}
	runID := strings.TrimSpace(os.Getenv(blockMappingAuthorityUnavailableRunIDEnv))
	namespace, err := uuid.Parse(runID)
	if err != nil {
		t.Fatalf("%s must be a UUID: %v", blockMappingAuthorityUnavailableRunIDEnv, err)
	}
	orgID := uuid.NewSHA1(namespace, []byte("pc-d1b3-mapping-unavailable-org")).String()
	content := newMappingAuthorityContent("unavailable " + runID)
	injected := newMappingAuthorityContent("unavailable injected " + runID)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	endpoints := w2PostHead3DCEndpoints(t)
	na := w2PostHead3DCConnectSerial(t, "dc-na", endpoints, "LOCAL_SERIAL")
	storageManager, blockStore := newLibraryBaselineCertifierStorage(t, ctx, orgID, libraryBaselineCertifierMinIOEndpoint, phase == "prepare")

	readLibraryMarker := func() (string, string) {
		var libraryID, head string
		if err := na.Session().Query(`SELECT library_id, head_commit_id FROM libraries WHERE org_id = ? LIMIT 1`, orgID).WithContext(ctx).Consistency(gocql.LocalOne).Scan(&libraryID, &head); err != nil {
			t.Fatalf("read prepared mapping-unavailable library: %v", err)
		}
		return libraryID, head
	}

	switch phase {
	case "prepare":
		storeMappingAuthorityBlock(t, ctx, na, blockStore, orgID, content)
		writeMutableBlockMapping(t, na, orgID, content.external, content.internal)
		seedSHA1OnlyMappingLibrary(t, na, orgID, "unavailable", content)
		requireNoMappingAuthority(t, ctx, na, orgID, content.external)
	case "unavailable":
		libraryID, head := readLibraryMarker()
		_, _, claimErr := dbpkg.ClaimBlockMappingAuthorityForIntegration(ctx, na.Session(), orgID, dbpkg.PlainBlockRepresentationID, injected.external, injected.internal, dbpkg.BlockMappingEvidenceIntegrationInjected)
		if claimErr == nil {
			t.Fatal("a mapping claim was established with dc-eu and dc-asia down; the claim is not in the global SERIAL domain")
		}
		promotion, err := na.PromoteBlockMappingAuthority(ctx, storageManager, orgID, dbpkg.PlainBlockRepresentationID, content.external)
		if _, ok := promotion.ConsumableInternalID(); ok || promotion.Outcome == dbpkg.BlockMappingAuthorityPromoted || promotion.Outcome == dbpkg.BlockMappingAuthorityUnproven {
			t.Fatalf("promotion without a global SERIAL majority = %+v, %v; want UNAVAILABLE/UNKNOWN", promotion, err)
		}
		result := na.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, head)
		if result.Outcome != dbpkg.LibraryBaselineCertificationUnknown || result.PermanentLivenessWrites != 0 || result.PhysicalRevalidations != 0 {
			t.Fatalf("certification without global SERIAL authority = %+v; want UNKNOWN before liveness/physical work", result)
		}
		var certifiedHead *string
		if err := na.Session().Query(`SELECT continuity_certified_head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, libraryID).WithContext(ctx).Consistency(gocql.LocalQuorum).Scan(&certifiedHead); err != nil {
			t.Fatalf("locally read mapping-unavailable witness: %v", err)
		}
		if certifiedHead != nil {
			t.Fatalf("authority outage created a witness %q", *certifiedHead)
		}
		blockMappingAuthorityUnavailableEvidence = true
		t.Logf("GREEN: MAPPING-3DC-3 outage — claim rejected, promotion %s, certification %s/%s, no witness", promotion.Outcome, result.Outcome, result.Reason)
	case "recover":
		libraryID, head := readLibraryMarker()
		requireNoMappingAuthority(t, ctx, na, orgID, injected.external)
		requireNoMappingAuthority(t, ctx, na, orgID, content.external)
		requireNoBaselineWitness(t, na, orgID, libraryID)
		result := na.CertifyLibraryBaseline(ctx, storageManager, orgID, libraryID, head)
		if result.Outcome != dbpkg.LibraryBaselineCertificationCertified || result.UniqueBlocks != 1 {
			t.Fatalf("certification after authority recovery = %+v", result)
		}
		requireMappingAuthority(t, ctx, na, orgID, content.external, content.internal)
		requireBaselineWitness(t, na, orgID, libraryID, head)
		blockMappingAuthorityUnavailableEvidence = true
		t.Logf("GREEN: MAPPING-3DC-3 recovery — nothing was claimed during the outage; promotion and certification succeed after recovery")
	}
}
