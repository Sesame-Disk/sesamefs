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
MUTATED=()
STACK_PROJECT="sesamefs-pcd1b3-mutation-stack-$$"
STACK_STARTED=0

green() { echo "RED as required: $*"; }
fail() { echo "FAILED: $*" >&2; restore; exit 1; }

restore() {
	for target in "${MUTATED[@]}"; do
		if [ -f "$target$BACKUP_SUFFIX" ]; then
			mv -f "$target$BACKUP_SUFFIX" "$target"
		fi
	done
	MUTATED=()
}

cleanup() {
	restore
	if [ "$STACK_STARTED" -eq 1 ]; then
		docker compose -p "$STACK_PROJECT" down --volumes --remove-orphans --rmi local >/dev/null 2>&1 || true
	fi
	docker rm -f "$RUNNER" >/dev/null 2>&1 || true
}

trap cleanup EXIT INT TERM

mutate_many() {
	restore
	while [ "$#" -gt 0 ]; do
		local target="$1" expression="$2"
		cp "$target" "$target$BACKUP_SUFFIX"
		MUTATED+=("$target")
		perl -0pi -e "$expression" "$target"
		cmp -s "$target" "$target$BACKUP_SUFFIX" && fail "mutation of $target did not apply"
		shift 2
	done
}

mutate() {
	mutate_many "$@"
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

# M18a: a claim is consumed without a frozen projection, so a stale ordinary
# write could still make readers resolve elsewhere after the witness.
m18a_consume_without_frozen_projection() {
    mutate "$PRIMITIVE" 's/if !claimed \|\| r\.Projection != BlockMappingProjectionFrozen \{/if !claimed {/'
    expect_red "M18a consume authority without a frozen projection" "a claim without a frozen projection must never resolve" '^TestContinuityWalkerRequiresFrozenProjection$'
}

# M18e: promotion skips the projection freeze (temporal authority).
m18e_promotion_skips_freeze() {
    mutate "$PRIMITIVE" 's/state, freezeErr := ports\.freeze\(ctx, identity, authority\)/_ = authority; state, freezeErr := BlockMappingProjectionFrozen, error(nil)/'
    expect_red "M18e promotion without projection freeze" "promotion must freeze the ordinary projection to the claimed authority" '^TestPromoteBlockMappingAuthorityClaimsOnlyProvedCandidateThenFreezes$'
}

# M18f: the freeze overwrites a projection that already diverged.
m18f_freeze_repairs_divergence() {
    mutate "$PRIMITIVE" 's/if row\.found && NormalizeBlockID\(row\.internalID\) != authority \{/if false \&\& row.found \&\& NormalizeBlockID(row.internalID) != authority {/'
    expect_red "M18f freeze repairs a diverged projection" "a diverged projection must never be overwritten" '^TestBlockMappingProjectionDecisionNeverRepairsDivergence$'
}

# M18g: the pre-witness recheck accepts an agreeing but unfrozen projection.
m18g_recheck_accepts_unfrozen() {
    mutate "$CERTIFIER" 's/if !found \|\| !frozen \|\| NormalizeBlockID\(mappedID\) != authoritativeID \{/if !found || NormalizeBlockID(mappedID) != authoritativeID {/'
    expect_red "M18g pre-witness recheck accepts an unfrozen projection" "unfrozen same before witness" '^TestRevalidateContinuityMappingAuthorityBeforeWitness$'
}

# R1: promotion accepts hash-valid bytes from another representation.
r1_promotion_ignores_representation() {
    mutate "$PRIMITIVE" 's/if blockRepresentationID != identity\.representationID \{/if false \&\& blockRepresentationID != identity.representationID {/'
    expect_red "R1 cross-representation provenance" "proved a plain:v1 mapping" '^TestBlockMappingProvenanceBindsRepresentation$'
}

# R2: consumption ignores the representation of the named canonical block.
r2_consumption_ignores_representation() {
    mutate "$CERTIFIER" 's/if blockRepresentationID != representationID \{/if false \&\& blockRepresentationID != representationID {/'
    expect_red "R2 consumed claim outside the library representation" "mapped block in another representation resolved" '^TestContinuityWalkerBindsMappedBlockRepresentation$'
}

# E1: stored claims with unsupported evidence are accepted.
e1_accept_unsupported_evidence() {
    mutate "$PRIMITIVE" 's/claim\.Evidence == BlockMappingEvidencePhysicalBytesV1 &&//'
    expect_red "E1 unsupported claim evidence accepted" "was accepted as usable authority" '^TestStoredBlockMappingClaimRequiresSupportedEvidence$'
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

# M20: productive mapping resolution falls back to a potentially stale ONE
# replica after the EACH_QUORUM temporal freeze.
m20_projection_reader_one() {
    mutate "$PRIMITIVE" 's/BlockMappingProjectionReadConsistency = gocql\.LocalQuorum/BlockMappingProjectionReadConsistency = gocql.One/'
    expect_red "M20 productive mapping reader uses ONE" "productive block mapping consistency = ONE, want LOCAL_QUORUM" '^TestBlockMappingProjectionReadsPinLocalQuorum$'
}

# M21: an ambiguous CAS result from the strong metadata read is not retried.
m21_no_ambiguous_serial_retry() {
    mutate "$PRIMITIVE" 's/blockMappingSerialReadRetryLimit = 2/blockMappingSerialReadRetryLimit = 0/'
    expect_red "M21 strong SERIAL read does not retry ambiguous CAS" "strong SERIAL metadata read =" '^TestProveBlockMappingCandidateRetriesAmbiguousCASOnStrongRead$'
}

# T2/H3: an ordinary writer supplies a timestamp above the authority freeze.
t2_ordinary_writer_supersedes_freeze() {
    mutate "$WRITERS" 's/(INSERT INTO block_id_mappings \([^)]*\) VALUES \(\?, \?, \?, \?, \?\))/$1 USING TIMESTAMP ?/; s/, createdAt\)\.Exec\(\)/, createdAt, BlockMappingProjectionFrozenTimestamp + 1).Exec()/'
    expect_red "T2/H3 ordinary mapping writer timestamp exceeds the freeze" "ordinary block_id_mappings INSERT must not specify USING TIMESTAMP" '^TestBlockMappingMutationsAreRepositoryWideInventoried$'
}

# DEL1: a productive mapping DELETE bypasses the R11a mutation contract.
del1_productive_mapping_delete() {
    mutate "$WRITERS" 's/\z/\nconst blockMappingAuthorityDeleteMutation = "DELETE FROM block_id_mappings WHERE org_id = ?"\n/'
    expect_red "DEL1 production mapping DELETE" "production DELETE from block_id_mappings is prohibited by R11a" '^TestBlockMappingMutationsAreRepositoryWideInventoried$'
}

# H3: a production caller bypasses provenance/claim promotion by invoking the
# projection-freeze primitive directly.
a1_external_freeze_caller() {
    mutate "$WRITERS" 's/\z/\nfunc pcd1b3ExternalFreezeCallerMutation(ctx context.Context, session *gocql.Session, identity blockMappingIdentity, authority string) { _, _ = freezeBlockMappingProjection(ctx, session, identity, authority) }\n/'
    expect_red "A1 external projection-freeze caller" "references freezeBlockMappingProjection" '^TestBlockMappingAuthorityAcquisitionIsColdPathOnly$'
}

# T3: a dominant-timestamp UPDATE is assembled by a helper from local,
# concatenated strings and then passed through a Query variable.
t3_dynamic_timestamp_mapping_update() {
    mutate "$WRITERS" 's/\z/\nfunc pcd1b3DynamicTimestampUpdateCQLMutation() string { query := "UPDATE block_id_" + "mappings USING TIMESTAMP ? SET internal_id = ? WHERE org_id = ? AND representation_id = ? AND external_id = ?"; return query }\nfunc pcd1b3DynamicTimestampUpdateMutation(session *gocql.Session) { query := pcd1b3DynamicTimestampUpdateCQLMutation(); session.Query(query, BlockMappingProjectionFrozenTimestamp, "id", "org", "plain:v1", "sha1") }\n/'
    expect_red "T3 dynamically assembled dominant-timestamp UPDATE" "only freezeBlockMappingProjection may use explicit-timestamp block_id_mappings UPDATE" '^TestBlockMappingMutationsAreRepositoryWideInventoried$'
}

# T4: an unresolved runtime table suffix must not make a dynamic Query escape
# the mapping mutation inventory.
t4_unresolved_mapping_query_argument() {
    mutate "$WRITERS" 's/\z/\nfunc pcd1b3UnknownMappingUpdateCQLMutation(table string) string { return "UPDATE block_id_" + table + " USING TIMESTAMP ? SET internal_id = ? WHERE org_id = ? AND representation_id = ? AND external_id = ?" }\nfunc pcd1b3UnknownMappingUpdateMutation(session *gocql.Session, table string) { query := pcd1b3UnknownMappingUpdateCQLMutation(table); session.Query(query, BlockMappingProjectionFrozenTimestamp, "id", "org", "plain:v1", "sha1") }\n/'
    expect_red "T4 unresolved dynamic mapping Query argument" "unresolved/dynamic block_id_mappings Query argument" '^TestBlockMappingMutationsAreRepositoryWideInventoried$'
}

# D2: strings.Join assembles a production DELETE across literals.
d2_dynamic_mapping_delete() {
    mutate "$WRITERS" 's/\z/\nfunc pcd1b3DynamicDeleteCQLMutation() string { return strings.Join([]string{"DELETE FROM block_id_", "mappings WHERE org_id = ? AND representation_id = ? AND external_id = ?"}, "") }\nfunc pcd1b3DynamicDeleteMutation(session *gocql.Session) { query := pcd1b3DynamicDeleteCQLMutation(); session.Query(query, "org", "plain:v1", "sha1") }\n/'
    expect_red "D2 dynamically assembled mapping DELETE" "production DELETE from block_id_mappings is prohibited by R11a" '^TestBlockMappingMutationsAreRepositoryWideInventoried$'
}

# R3: every mapping SELECT must be in the reader inventory, even if it has no
# pinned consistency floor.
r3_unclassified_mapping_select() {
	mutate "$WRITERS" 's/\z/\nfunc pcd1b3UnclassifiedMappingReaderMutation(session *gocql.Session) { session.Query("SELECT internal_id FROM block_id_mappings WHERE org_id = ? AND representation_id = ? AND external_id = ?", "org", "plain:v1", "sha1").Scan(new(string)) }\n/'
	expect_red "R3 unclassified mapping SELECT" "unclassified production block_id_mappings SELECT" '^TestBlockMappingMutationsAreRepositoryWideInventoried$'
}

# T5: the Query call must resolve q at that point in statement order, before a
# later harmless assignment overwrites the local's final value.
t5_query_before_harmless_reassignment() {
	mutate "$WRITERS" 's/\z/\nfunc pcd1b3QueryBeforeHarmlessReassignmentMutation(session *gocql.Session) { q := "DELETE FROM block_id_mappings WHERE org_id = ? AND representation_id = ? AND external_id = ?"; session.Query(q, "org", "plain:v1", "sha1").Exec(); q = "SELECT now() FROM system.local" }\n/'
	expect_red "T5 dangerous Query followed by harmless reassignment" "production DELETE from block_id_mappings is prohibited by R11a" '^TestBlockMappingMutationsAreRepositoryWideInventoried$'
}

# T6: a known mapping CQL argument must flow into a same-package generic helper
# that performs the Query.
t6_mapping_cql_through_generic_helper() {
	mutate "$WRITERS" 's/\z/\nfunc pcd1b3GenericCQLHelperMutation(session *gocql.Session, q string) { session.Query(q).Exec() }\nfunc pcd1b3GenericCQLHelperCallerMutation(session *gocql.Session) { pcd1b3GenericCQLHelperMutation(session, "DELETE FROM block_id_mappings WHERE org_id = ? AND representation_id = ? AND external_id = ?") }\n/'
	expect_red "T6 mapping CQL passed through a generic helper argument" "production DELETE from block_id_mappings is prohibited by R11a" '^TestBlockMappingMutationsAreRepositoryWideInventoried$'
}

# T7: runtime table identity leaves an unresolved Query argument and must fail
# closed even though no full block_id_mappings token can be reconstructed.
t7_runtime_mapping_table_identity() {
	mutate "$WRITERS" 's/\z/\nfunc pcd1b3RuntimeTableIdentityMutation(session *gocql.Session, table string) { q := "SELECT internal_id FROM " + table + " WHERE org_id = ?"; session.Query(q, "org") }\n/'
	expect_red "T7 unresolved runtime table identity" "unresolved/dynamic block_id_mappings Query argument" '^TestBlockMappingMutationsAreRepositoryWideInventoried$'
}

# R4: LOCAL_QUORUM on an unrelated query must not satisfy the mapping reader's
# query-chain consistency contract.
r4_unrelated_query_has_local_quorum() {
	mutate "$WRITERS" 's/Consistency\(BlockMappingProjectionReadConsistency\)\.//; s/(func \(db \*DB\) GetBlockIDMappingContext\([^\n]*\) \{\n)/$1\t_ = db.Session().Query("SELECT x FROM something_else").Consistency(BlockMappingProjectionReadConsistency)\n/'
	expect_red "R4 unrelated query supplies LOCAL_QUORUM" "block mapping SELECT consistency=, want BlockMappingProjectionReadConsistency" '^TestBlockMappingMutationsAreRepositoryWideInventoried$'
}

# A2: a function-value alias in the allowed primitive file cannot be called
# from elsewhere to bypass provenance and claim acquisition.
a2_aliased_freeze_caller() {
	mutate_many "$PRIMITIVE" 's/\z/\nvar pcd1b3RawFreezeAliasMutation = freezeBlockMappingProjection\n/' "$WRITERS" 's/\z/\nfunc pcd1b3ExternalAliasedFreezeCallerMutation(ctx context.Context, session *gocql.Session, identity blockMappingIdentity, authority string) { _, _ = pcd1b3RawFreezeAliasMutation(ctx, session, identity, authority) }\n/'
	expect_red "A2 alias of projection-freeze primitive" "freezeBlockMappingProjection may not be taken as a function value or aliased" '^TestBlockMappingAuthorityAcquisitionIsColdPathOnly$'
}

# T1 (behavioral, real Cassandra + MinIO, uses a private Compose project):
# replace the dominant-timestamp freeze with an ordinary rewrite. An ordinary
# write between the final recheck and the witness CAS then reaches readers
# after the witness, which the real reproducer must catch.
t1_freeze_without_dominant_timestamp() {
	mutate "$PRIMITIVE" 's/UPDATE block_id_mappings USING TIMESTAMP \? SET internal_id = \?/UPDATE block_id_mappings SET internal_id = ?/; s/`, BlockMappingProjectionFrozenTimestamp, authority, /`, authority, /; s/row\.writeTime == BlockMappingProjectionFrozenTimestamp/row.writeTime > 0/g'
	local out status
	STACK_STARTED=1
	out="$(SESAMEFS_HOST_PORT=0 CASSANDRA_HOST_PORT=0 MINIO_API_HOST_PORT=0 MINIO_CONSOLE_HOST_PORT=0 FRONTEND_HOST_PORT=0 ONLYOFFICE_HOST_PORT=0 \
		docker compose -p "$STACK_PROJECT" --profile test run --rm --build \
        -e SESAMEFS_REQUIRE_X1_NONOVERLAP_CHARACTERIZATION=0 \
        -e SESAMEFS_REQUIRE_BORROWEDFS_OWN_LIVENESS_EVIDENCE=0 \
        go-integration-test go test -tags integration -count=1 ./internal/integration/ \
        -run '^TestBlockMappingAuthorityCertifierRealCassandra$/ordinary_write_between_final_recheck_and_witness_CAS_is_inert$' 2>&1)"
    status=$?
    echo "$out" | grep -E -- "--- (PASS|FAIL)|_test.go:[0-9]+:" || true
    if [ "$status" -eq 0 ]; then
        fail "T1 fence-less freeze stayed green on real Cassandra"
    fi
    if [[ "$out" != *"want frozen"* ]]; then
        fail "T1 did not trip the post-witness projection assertion"
    fi
    green "T1 fence-less freeze lets a pre-CAS ordinary write reach readers after the witness"
}

ALL_MUTATIONS=(
    m18a_consume_without_frozen_projection
    m18d_drop_pre_witness_mapping_recheck
    m18e_promotion_skips_freeze
    m18f_freeze_repairs_divergence
    m18g_recheck_accepts_unfrozen
    m18b_conflict_reports_candidate
    m18c_existing_claim_ignored
    m19a_promote_without_provenance
    m19b_sha256_only_provenance
    s1_claim_local_serial
    s2_read_local_serial
    h1_upload_writer_lwt
    h2_upload_writer_promotes
    i1_claim_retirement
    r1_promotion_ignores_representation
    r2_consumption_ignores_representation
    e1_accept_unsupported_evidence
    m20_projection_reader_one
    m21_no_ambiguous_serial_retry
    t2_ordinary_writer_supersedes_freeze
    del1_productive_mapping_delete
    a1_external_freeze_caller
    t3_dynamic_timestamp_mapping_update
    t4_unresolved_mapping_query_argument
    d2_dynamic_mapping_delete
    r3_unclassified_mapping_select
    t5_query_before_harmless_reassignment
    t6_mapping_cql_through_generic_helper
    t7_runtime_mapping_table_identity
    r4_unrelated_query_has_local_quorum
    a2_aliased_freeze_caller
)

WITH_INTEGRATION=0
if [ "${1:-}" = "--with-integration" ]; then
    WITH_INTEGRATION=1
    shift
fi

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
baseline="$(docker exec "$RUNNER" go test ./internal/db -count=1 -run '^(TestContinuityWalkerRequiresFrozenProjection|TestCertifierRechecksMappingAuthorityBeforeWitness|TestPromoteBlockMappingAuthorityClaimsOnlyProvedCandidateThenFreezes|TestBlockMappingProjectionDecisionNeverRepairsDivergence|TestRevalidateContinuityMappingAuthorityBeforeWitness|TestBlockMappingAuthorityConflictKeepsDurableWinner|TestPromoteBlockMappingAuthorityReturnsExistingClaimWithoutReadingMutable|TestBlockMappingConvergenceIsNotProvenance|TestBlockMappingProvenanceRequiresBothContentDigests|TestBlockMappingProvenanceBindsRepresentation|TestContinuityWalkerBindsMappedBlockRepresentation|TestStoredBlockMappingClaimRequiresSupportedEvidence|TestBlockMappingAuthorityPinsGlobalSerial|TestBlockMappingAuthorityAcquisitionIsColdPathOnly|TestBlockMappingAuthorityClaimsAreImmutableRepositoryWide|TestBlockMappingProjectionReadsPinLocalQuorum|TestBlockMappingAuthoritySerialReadRetriesOnlyAmbiguousCAS|TestProveBlockMappingCandidateRetriesAmbiguousCASOnStrongRead|TestBlockMappingMutationsAreRepositoryWideInventoried)$' 2>&1)" || {
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
TOTAL=${#ALL_MUTATIONS[@]}
if [ "$WITH_INTEGRATION" -eq 1 ]; then
    t1_freeze_without_dominant_timestamp
    restore
    TOTAL=$((TOTAL + 1))
fi
echo "PC-D1B.3 M18/M19, representation, evidence, SERIAL, hot-path and immutability contract legs are red (${TOTAL}/${TOTAL})"
