#!/usr/bin/env bash
# PC-D1B.1 source mutations. Each protocol-incorrect edit must make the
# certifier safety contract RED. All Go tests run inside Docker.
set -uo pipefail
cd "$(dirname "$0")/.."

TARGET=internal/db/library_continuity_certifier.go
TEST_IMAGE=${PCD1B1_MUTATION_IMAGE:-golang:1.25.12-trixie}
BACKUP="$TARGET.pcd1b1bak"
RUNNER="sesamefs-pcd1b1-mutation-runner-$$"

green() { echo "RED as required: $*"; }
fail() { echo "FAILED: $*" >&2; restore; exit 1; }

restore() {
    if [ -f "$BACKUP" ]; then
        mv -f "$BACKUP" "$TARGET"
    fi
}

cleanup() {
    restore
    docker rm -f "$RUNNER" >/dev/null 2>&1 || true
}

trap cleanup EXIT INT TERM

mutate() {
    restore
    cp "$TARGET" "$BACKUP"
    perl -0pi -e "$1" "$TARGET"
    cmp -s "$TARGET" "$BACKUP" && fail "mutation did not apply"
}

expect_red() {
    local label="$1" diagnostic="$2" out status
    out="$(docker exec "$RUNNER" go test ./internal/db -count=1 -run '^TestCertifierOrdersLivenessRevalidationAndWitness$' 2>&1)"
    status=$?
    if [ "$status" -eq 0 ]; then
        echo "$out"
        fail "$label stayed green"
    fi
    echo "$out"
    if [[ "$out" != *"$diagnostic"* ]]; then
        fail "$label did not trip its targeted contract assertion: $diagnostic"
    fi
    green "$label"
}

# M1: accepting ValidatePhysicalLocator would certify a legacy deterministic key.
m1_reject_legacy_locator() {
    mutate 's/ValidateMintedPhysicalLocator/ValidatePhysicalLocator/'
    expect_red "M1 legacy deterministic locator acceptance" "certifier does not explicitly reject deterministic legacy locators"
}

# M2: skipping the reachable-tree walk cannot produce a complete certificate.
m2_walk_complete_tree() {
    mutate 's/dependencies, err := db\.walkContinuityTree\([^\n]+\)/_ = representationID; _ = rootFSID; dependencies, err := continuityDependencies{}, nil/'
    expect_red "M2 complete reachable-tree walk" "certifier must walk the complete reachable tree from the observed commit root"
}

# M3: an ordinary/eventual existence read is not permanent EACH_QUORUM liveness.
m3_require_permanent_liveness() {
    mutate 's/BlockReferencePermanentExistsEachQuorumContext\(ctx, orgID, blockID, referrer, libraryID\)/BlockReferenceExistsEachQuorumContext(ctx, orgID, blockID, referrer)/g'
    expect_red "M3 permanent liveness proof" "certifier must prove permanent EACH_QUORUM liveness before write, after write, and before witness"
}

# M4: a TTL bridge is not the non-expiring authority required by the witness.
m4_reject_ttl_liveness() {
    mutate 's/libraryID, 0\)/libraryID, ProvisionalBlockReferenceTTLSeconds)/'
    expect_red "M4 non-expiring liveness" "certifier must establish non-expiring liveness, not a TTL pin"
}

# M5: both liveness guards must fail closed on a missing permanent row.
m5_keep_liveness_guards() {
    mutate 's/if !permanent \{/if permanent {/g'
    expect_red "M5 liveness visibility guards" "certifier must fail closed for both post-write and pre-witness liveness checks"
}

# M6: only an explicitly authorized physical revalidation may continue.
m6_keep_physical_authority() {
    mutate 's/case BlockRepairAuthorityAuthorized:/case BlockRepairAuthorityUnknown:/'
    expect_red "M6 physical authority classification" "certifier must accept only an explicitly authorized physical revalidation"
}

# M7: the final witness CAS is the only certification authority.
m7_keep_witness_cas() {
    mutate 's/CommitLibraryContinuityWitnessContext\(ctx, db\.Session\(\),/CommitLibraryContinuityWitness(db.Session(),/'
    expect_red "M7 final witness CAS" "certification sequence is incomplete:"
}

# M8: ambiguous CAS results must settle through the authoritative read.
m8_keep_witness_settlement() {
    mutate 's/settlement, settleErr := settleLibraryContinuityWitnessContext\(ctx, db, orgID, libraryID, observedHead\)/settlement, settleErr := libraryContinuityWitnessSettlement{}, casErr/'
    expect_red "M8 ambiguous witness settlement" "certifier has no ambiguous-witness settlement path"
}

# M9: both transport errors and explicit UNKNOWN CAS outcomes need settlement.
m9_settle_unknown_cas() {
    mutate 's/ \|\| cas\.Outcome == LibraryContinuityCASUnknown//'
    expect_red "M9 UNKNOWN CAS settlement gate" "certifier must settle both transport errors and explicit UNKNOWN CAS outcomes"
}

# M10: an authoritative settled witness is a positive certification result.
m10_report_settled_certified() {
    mutate 's/LibraryBaselineCertificationCertified, LibraryBaselineReasonWitnessSettled/LibraryBaselineCertificationNotCertified, LibraryBaselineReasonWitnessSettled/'
    expect_red "M10 settled witness outcome" "settled authoritative witness must be reported as certified"
}

ALL_MUTATIONS=(
    m1_reject_legacy_locator
    m2_walk_complete_tree
    m3_require_permanent_liveness
    m4_reject_ttl_liveness
    m5_keep_liveness_guards
    m6_keep_physical_authority
    m7_keep_witness_cas
    m8_keep_witness_settlement
    m9_settle_unknown_cas
    m10_report_settled_certified
)

if [ "${1:-}" = "--list" ]; then
    printf '%s\n' "${ALL_MUTATIONS[@]}"
    exit 0
fi

docker run -d --name "$RUNNER" -v "$(pwd):/build" -w /build "$TEST_IMAGE" sleep 3600 >/dev/null
for _ in $(seq 1 30); do
    if docker exec "$RUNNER" go version >/dev/null 2>&1; then
        break
    fi
    sleep 1
done
docker exec "$RUNNER" go version >/dev/null || fail "Docker mutation runner did not start"

# Docker runner remains active for the full suite or the selected mutation below.

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
echo "PC-D1B.1 mutations are red (10/10)"
