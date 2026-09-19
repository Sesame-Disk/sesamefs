#!/usr/bin/env bash
# PC-D1A source mutations. Each protocol-incorrect edit must make the DB
# contract tests RED. Tests run in Docker; no application stack is required.
set -uo pipefail
cd "$(dirname "$0")/.."

TARGET=internal/db/library_continuity.go
STATE_TARGET=internal/db/library_state.go
TEST_IMAGE=${PCD1A_MUTATION_IMAGE:-sesamefs-pcd1a-gotest}
BACKUP="$TARGET.pcd1abak"
STATE_BACKUP="$STATE_TARGET.pcd1abak"

green() { echo "RED as required: $*"; }
fail() { echo "FAILED: $*" >&2; restore; exit 1; }
restore() {
	if [ -f "$BACKUP" ]; then
		mv -f "$BACKUP" "$TARGET"
	fi
	if [ -f "$STATE_BACKUP" ]; then
		mv -f "$STATE_BACKUP" "$STATE_TARGET"
	fi
}
trap restore EXIT INT TERM

mutate() {
	restore
	cp "$TARGET" "$BACKUP"
	perl -0pi -e "$1" "$TARGET"
	cmp -s "$TARGET" "$BACKUP" && fail "mutation did not apply"
}

mutate_state() {
	restore
	cp "$STATE_TARGET" "$STATE_BACKUP"
	perl -0pi -e "$1" "$STATE_TARGET"
	cmp -s "$STATE_TARGET" "$STATE_BACKUP" && fail "state mutation did not apply"
}

expect_red() {
	local pattern="$1" label="$2" out status
	out="$(docker run --rm --name sesamefs-pcd1a-mutation-test -v "$(pwd):/build" -w /build "$TEST_IMAGE" go test ./internal/db -count=1 -run "$pattern" 2>&1)"
	status=$?
	if [ "$status" -eq 0 ]; then
		echo "$out"
		fail "$label stayed green"
	fi
	echo "$out"
	green "$label"
}

m_baseline_if() {
	mutate 's/IF head_commit_id = \?/IF EXISTS/'
	expect_red '^TestLibraryContinuityAuthorityCQLContracts$' "baseline witness loses HEAD fence"
}

m_baseline_serial() {
	mutate 's/SerialConsistency\(LibraryHeadSerialConsistency\)/SerialConsistency(gocql.LocalSerial)/'
	expect_red '^TestLibraryContinuityAuthorityCQLContracts$' "baseline witness loses global SERIAL"
}

m_atomic_if() {
	mutate 's/\n\t\tIF head_commit_id = \?\n\t\tAND continuity_certified_head_commit_id = \?/\n\t\tAND continuity_certified_head_commit_id = \?/'
	expect_red '^TestLibraryContinuityAuthorityCQLContracts$' "atomic advance loses the HEAD predecessor"
}

m_atomic_serial() {
	mutate 's/SerialConsistency\(LibraryHeadSerialConsistency\)(?![\s\S]*SerialConsistency\(LibraryHeadSerialConsistency\))/SerialConsistency(gocql.LocalSerial)/'
	expect_red '^TestLibraryContinuityAuthorityCQLContracts$' "atomic advance loses global SERIAL"
}

m_atomic_set_certified() {
	mutate 's/SET head_commit_id = \?, continuity_certified_head_commit_id = \?, continuity_contract_version = \?/SET head_commit_id = ?, continuity_contract_version = ?/'
	expect_red '^TestLibraryContinuityAuthorityCQLContracts$' "atomic advance stops writing the certified HEAD"
}

m_atomic_set_version() {
	mutate 's/SET head_commit_id = \?, continuity_certified_head_commit_id = \?, continuity_contract_version = \?/SET head_commit_id = ?, continuity_certified_head_commit_id = ?/'
	expect_red '^TestLibraryContinuityAuthorityCQLContracts$' "atomic advance stops writing the contract version"
}

m_atomic_certified_predicate() {
	mutate 's/\n\t\tAND continuity_certified_head_commit_id = \?//'
	expect_red '^TestLibraryContinuityAuthorityCQLContracts$' "atomic advance loses certified-head predecessor"
}

m_atomic_version_predicate() {
	mutate 's/\n\t\tAND continuity_contract_version = \?//'
	expect_red '^TestLibraryContinuityAuthorityCQLContracts$' "atomic advance loses contract predecessor"
}

m_witness_deleted_lifecycle() {
	mutate_state 's/s\.DeletedAt == nil/true/'
	expect_red '^TestContinuityWitnessValidity$' "witness validity ignores deleted lifecycle"
}

m_witness_validity_stale() {
	mutate_state 's/== s\.HeadCommitID/!= s.HeadCommitID/'
	expect_red '^TestContinuityWitnessValidity$' "witness validity accepts stale HEAD"
}

m_baseline_deleted_guard() {
	mutate 's/\n\t\tIF head_commit_id = \?\n\t\tAND deleted_at = null/\n\t\tIF head_commit_id = \?/'
	expect_red '^TestLibraryContinuityAuthorityCQLContracts$' "baseline witness accepts a deleted library"
}

m_frontier_deleted_guard() {
	mutate 's/\n\t\tAND continuity_contract_version = \?\n\t\tAND deleted_at = null/\n\t\tAND continuity_contract_version = \?/'
	expect_red '^TestLibraryContinuityAuthorityCQLContracts$' "frontier advance accepts a deleted library"
}

m_accept_unsupported_version() {
	mutate 's/if contractVersion != SupportedContinuityContractVersion/if false/'
	expect_red '^TestValidateLibraryContinuityInput$|^TestContinuityWitnessValidity$' "unsupported contract becomes accepted"
}

ALL_MUTATIONS=(
	m_baseline_if
	m_baseline_serial
	m_atomic_if
	m_atomic_serial
	m_atomic_set_certified
	m_atomic_set_version
	m_atomic_certified_predicate
	m_atomic_version_predicate
	m_witness_deleted_lifecycle
	m_witness_validity_stale
	m_baseline_deleted_guard
	m_frontier_deleted_guard
	m_accept_unsupported_version
)

if [ "${1:-}" = "--list" ]; then
	printf '%s\n' "${ALL_MUTATIONS[@]}"
	exit 0
fi

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
echo "PC-D1A mutations are red (${#ALL_MUTATIONS[@]}/${#ALL_MUTATIONS[@]})"
