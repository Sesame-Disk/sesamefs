#!/usr/bin/env bash
# PC-D1B.3 Mapping Authority source mutations. Each protocol-incorrect edit
# must turn its targeted contract RED with that contract's own diagnostic.
# All Go tests run inside Docker; sources are restored on every exit.
set -uo pipefail
cd "$(dirname "$0")/.."

TEST_IMAGE=${PCD1B3_MUTATION_IMAGE:-golang:1.25.12-trixie}
RUNNER="sesamefs-pcd1b3-mutation-runner-$$"
PRIMITIVE=internal/db/block_mapping_authority.go
CERTIFIER=internal/db/library_continuity_certifier.go
WRITERS=internal/db/block_references.go
BACKUP_SUFFIX=".pcd1b3bak.$$"
MUTATED=""

green() { echo "RED as required: $*"; }
fail() { echo "FAILED: $*" >&2; restore; exit 1; }

restore() {
    if [ -n "$MUTATED" ] && [ -f "$MUTATED$BACKUP_SUFFIX" ]; then
        mv -f "$MUTATED$BACKUP_SUFFIX" "$MUTATED"
    fi
    MUTATED=""
}

cleanup() {
    restore
    docker rm -f "$RUNNER" >/dev/null 2>&1 || true
}

trap cleanup EXIT INT TERM

mutate() {
    local target="$1" expression="$2"
    restore
    cp "$target" "$target$BACKUP_SUFFIX"
    MUTATED="$target"
    perl -0pi -e "$expression" "$target"
    cmp -s "$target" "$target$BACKUP_SUFFIX" && fail "mutation of $target did not apply"
}

expect_red() {
    local label="$1" diagnostic="$2" test_pattern="$3" out status
    out="$(docker exec "$RUNNER" go test ./internal/db -count=1 -run "$test_pattern" 2>&1)"
    status=$?
    echo "$out"
    if [ "$status" -eq 0 ]; then
        fail "$label stayed green"
    fi
    if [[ "$out" != *"$diagnostic"* ]]; then
        fail "$label did not trip its targeted contract assertion: $diagnostic"
    fi
    green "$label"
}

# M18a: a post-authority mutable mapping that disagrees is ignored instead of
# refused, so a witness could protect A while ordinary readers resolve B.
m18a_certifier_ignores_mutable_divergence() {
    mutate "$CERTIFIER" 's/validateCanonicalBlockMapping\(authoritativeID, mutableID, mutableFound\)/validateCanonicalBlockMapping(authoritativeID, mutableID, false && mutableFound)/'
    expect_red "M18a post-authority mutable divergence accepted" "a diverged mutable mapping must be identity_conflict and never resolve" '^TestContinuityWalkerRejectsMutableMappingDivergingFromAuthority$'
}

# M18d: the pre-witness mapping recheck is dropped, so a mutable write landing
# during certification can be witnessed.
m18d_drop_pre_witness_mapping_recheck() {
    mutate "$CERTIFIER" 's/if err := revalidateContinuityMappingAuthority\(ctx, mappingAuthority, orgID, representationID, dependencies\.sha1Mappings\); err != nil \{/if err := error(nil); err != nil {/'
    expect_red "M18d pre-witness mapping recheck removed" "mapping authority must be rechecked after final metadata revalidation and before the witness" '^TestCertifierRechecksMappingAuthorityBeforeWitness$'
}

# M18b: a promotion that lost the claim race reports its own candidate.
m18b_conflict_reports_candidate() {
    mutate "$PRIMITIVE" 's/Outcome: BlockMappingAuthorityConflict, Authority: stored\.InternalID, Candidate: candidate\}/Outcome: BlockMappingAuthorityConflict, Authority: candidate, Candidate: candidate}/'
    expect_red "M18b losing promotion rewrites authority" "losing promotion must resolve the durable winner A" '^TestBlockMappingAuthorityConflictKeepsDurableWinner$'
}

# M18c: an existing claim is ignored and the mutable mapping is re-promoted.
m18c_existing_claim_ignored() {
    mutate "$PRIMITIVE" 's/return settledBlockMappingClaim\(&stored, ""\)/_ = stored/'
    expect_red "M18c existing authority re-derived from the mutable mapping" "want already-authoritative A" '^TestPromoteBlockMappingAuthorityReturnsExistingClaimWithoutReadingMutable$'
}

# M19a: promotion claims the converged mutable candidate without provenance.
m19a_promote_without_provenance() {
    mutate "$PRIMITIVE" 's/proof, err := ports\.prove\(ctx, identity, candidate\)/proof, err := blockMappingProvenance{identity: identity, internalID: candidate, evidence: BlockMappingEvidencePhysicalBytesV1}, error(nil)/'
    expect_red "M19a convergence accepted as provenance" "converged mutable mapping without provenance must stay UNPROVEN" '^TestBlockMappingConvergenceIsNotProvenance$'
}

# M19b: provenance accepts bytes that only match the SHA-256 candidate.
m19b_sha256_only_provenance() {
    mutate "$PRIMITIVE" 's/if contentSHA1 != identity\.externalID \{/if false \&\& contentSHA1 != identity.externalID {/'
    expect_red "M19b SHA-256-only provenance" "stored bytes that only match the SHA-256 candidate were accepted as provenance" '^TestBlockMappingProvenanceRequiresBothContentDigests$'
}

# S1: the claim inherits a per-datacenter LOCAL_SERIAL domain.
s1_claim_local_serial() {
    mutate "$PRIMITIVE" 's/SerialConsistency\(LibraryHeadSerialConsistency\)/SerialConsistency(gocql.LocalSerial)/'
    expect_red "S1 LOCAL_SERIAL mapping claim" "mapping authority claim must pin global SERIAL explicitly" '^TestBlockMappingAuthorityPinsGlobalSerial$'
}

# S2: the authority read uses LOCAL_SERIAL.
s2_read_local_serial() {
    mutate "$PRIMITIVE" 's/Consistency\(IdentityAuthorityReadConsistency\)/Consistency(gocql.LocalSerial)/'
    expect_red "S2 LOCAL_SERIAL mapping authority read" "mapping authority read must use the global SERIAL read consistency" '^TestBlockMappingAuthorityPinsGlobalSerial$'
}

# H1: the upload mapping writer becomes a per-block LWT.
h1_upload_writer_lwt() {
    mutate "$WRITERS" 's/(INSERT INTO block_id_mappings \(org_id, representation_id, external_id, internal_id, created_at\) VALUES \(\?, \?, \?, \?, \?\))/$1 IF NOT EXISTS/'
    expect_red "H1 upload hot-path Paxos" "issues conditional/authority CQL" '^TestBlockMappingAuthorityAcquisitionIsColdPathOnly$'
}

# H2: the upload mapping writer acquires mapping authority.
h2_upload_writer_promotes() {
    mutate "$WRITERS" 's/(func \(db \*DB\) writeCheckedBlockIDMapping\([^)]*\) error \{)/$1\n\t_, _ = db.PromoteBlockMappingAuthority(context.Background(), nil, orgID, representationID, externalID)/'
    expect_red "H2 upload hot-path promotion" "references PromoteBlockMappingAuthority" '^TestBlockMappingAuthorityAcquisitionIsColdPathOnly$'
}

# I1: a production path deletes (retires) a mapping claim.
i1_claim_retirement() {
    mutate "$PRIMITIVE" 's/\z/\nconst blockMappingAuthorityRetireCQL = "DELETE FROM block_mapping_authority_claims WHERE org_id = ? AND representation_id = ? AND external_id = ?"\n/'
    expect_red "I1 mapping claim retirement" "unauthorized operation on block_mapping_authority_claims" '^TestBlockMappingAuthorityClaimsAreImmutableRepositoryWide$'
}

ALL_MUTATIONS=(
    m18a_certifier_ignores_mutable_divergence
    m18d_drop_pre_witness_mapping_recheck
    m18b_conflict_reports_candidate
    m18c_existing_claim_ignored
    m19a_promote_without_provenance
    m19b_sha256_only_provenance
    s1_claim_local_serial
    s2_read_local_serial
    h1_upload_writer_lwt
    h2_upload_writer_promotes
    i1_claim_retirement
)

if [ "${1:-}" = "--list" ]; then
    printf '%s\n' "${ALL_MUTATIONS[@]}"
    exit 0
fi

WORKSPACE_PATH="$(pwd)"
case "${OSTYPE:-}" in
    msys*|cygwin*)
        WORKSPACE_PATH="$(cygpath -m "$WORKSPACE_PATH")"
        ;;
esac

MSYS_NO_PATHCONV=1 docker run -d --name "$RUNNER" -v "$WORKSPACE_PATH:/build" -w /build "$TEST_IMAGE" sleep 3600 >/dev/null
for _ in $(seq 1 30); do
    if docker exec "$RUNNER" go version >/dev/null 2>&1; then
        break
    fi
    sleep 1
done
docker exec "$RUNNER" go version >/dev/null || fail "Docker mutation runner did not start"

# Baseline: every targeted contract must be green before any mutation.
baseline="$(docker exec "$RUNNER" go test ./internal/db -count=1 -run '^(TestContinuityWalkerRejectsMutableMappingDivergingFromAuthority|TestCertifierRechecksMappingAuthorityBeforeWitness|TestBlockMappingAuthorityConflictKeepsDurableWinner|TestPromoteBlockMappingAuthorityReturnsExistingClaimWithoutReadingMutable|TestBlockMappingConvergenceIsNotProvenance|TestBlockMappingProvenanceRequiresBothContentDigests|TestBlockMappingAuthorityPinsGlobalSerial|TestBlockMappingAuthorityAcquisitionIsColdPathOnly|TestBlockMappingAuthorityClaimsAreImmutableRepositoryWide)$' 2>&1)" || {
    echo "$baseline"
    fail "targeted contracts are not green before mutation"
}

if [ -n "${1:-}" ]; then
    for mutation in "${ALL_MUTATIONS[@]}"; do
        if [ "$mutation" = "$1" ]; then
            "$mutation"
            restore
            exit 0
        fi
    done
    fail "unknown mutation $1"
fi

for mutation in "${ALL_MUTATIONS[@]}"; do
    "$mutation"
done
restore
echo "PC-D1B.3 M18/M19, SERIAL, hot-path and immutability contract legs are red (${#ALL_MUTATIONS[@]}/${#ALL_MUTATIONS[@]})"
