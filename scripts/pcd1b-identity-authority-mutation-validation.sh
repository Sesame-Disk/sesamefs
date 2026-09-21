#!/usr/bin/env bash
# PC-D1B identity-authority source mutations. Each protocol-incorrect edit must
# make the primitive's own contract tests RED. No application stack is required.
#
# Runs `go test` locally by default. Set PCD1B_MUTATION_IMAGE to a gotest image
# to run inside Docker instead (the repository's usual path when the host cannot
# execute test binaries).
#
# Mutations are matched with \r?\n-tolerant patterns because the working tree
# may be CRLF on Windows checkouts.
set -uo pipefail
cd "$(dirname "$0")/.."

TARGET=internal/db/identity_authority.go
PROD_TARGET=internal/api/v2/library_rollback.go
INVENTORY_TARGET=internal/db/pcd1b_identity_writer_inventory_test.go
TEST_IMAGE=${PCD1B_MUTATION_IMAGE:-}
BACKUP="$TARGET.pcd1bbak"
PROD_BACKUP="$PROD_TARGET.pcd1bbak"
INVENTORY_BACKUP="$INVENTORY_TARGET.pcd1bbak"

green() { echo "RED as required: $*"; }
fail() { echo "FAILED: $*" >&2; restore; exit 1; }
restore() {
	[ -f "$BACKUP" ] && mv -f "$BACKUP" "$TARGET"
	[ -f "$PROD_BACKUP" ] && mv -f "$PROD_BACKUP" "$PROD_TARGET"
	[ -f "$INVENTORY_BACKUP" ] && mv -f "$INVENTORY_BACKUP" "$INVENTORY_TARGET"
	return 0
}
trap restore EXIT INT TERM

mutate_file() {
	local file="$1" backup="$2" expr="$3"
	restore
	cp "$file" "$backup"
	perl -0pi -e "$expr" "$file"
	cmp -s "$file" "$backup" && fail "mutation did not apply to $file"
	return 0
}
mutate() { mutate_file "$TARGET" "$BACKUP" "$1"; }
mutate_prod() { mutate_file "$PROD_TARGET" "$PROD_BACKUP" "$1"; }
mutate_inventory() { mutate_file "$INVENTORY_TARGET" "$INVENTORY_BACKUP" "$1"; }

run_tests() {
	local pattern="$1"
	if [ -n "$TEST_IMAGE" ]; then
		docker run --rm --name sesamefs-pcd1b-mutation-test -v "$(pwd):/build" -w /build "$TEST_IMAGE" go test ./internal/db -count=1 -run "$pattern" 2>&1
	else
		go test ./internal/db -count=1 -run "$pattern" 2>&1
	fi
}

expect_red() {
	local pattern="$1" label="$2" needle="${3:-}" out status
	out="$(run_tests "$pattern")"
	status=$?
	if [ "$status" -eq 0 ]; then
		echo "$out"
		fail "$label stayed green"
	fi
	if [ -n "$needle" ] && ! grep -q -- "$needle" <<<"$out"; then
		echo "$out"
		fail "$label went red for the wrong reason (expected: $needle)"
	fi
	echo "$out" | grep -E "^(--- FAIL|FAIL|ok|.*_test.go:[0-9]+:)" | head -8
	green "$label"
}

# --- M17: the file digest must bind the canonical SHA-256 list ---------------
m17_drop_canonical_list() {
	mutate 's/list\(normalizeIdentityBlockIDs\(canonicalSHA256IDs\)\)\.\r?\n/list(nil).\n/'
	expect_red '^TestFileIdentityDigestBindsCanonicalBlockIDs$' "M17: canonical SHA-256 list dropped from the file digest" "canonical list is not bound"
}

m17_drop_length_prefix() {
	mutate 's/(\t\tvar length \[\]byte\r?\n\t\tlength = appendUint64\(length, uint64\(len\(field\)\)\)\r?\n\t\th\.Write\(length\)\r?\n)/\t\t_ = field\n/'
	expect_red '^TestIdentityDigestFieldBoundariesAreUnambiguous$' "M17b: field length prefix removed" "not length-delimited"
}

# --- M16: claim lifecycle at the claim layer ----------------------------------
m16_ttl_on_claim() {
	mutate 's/VALUES \(\?, \?, \?, \?, \?, \?\) IF NOT EXISTS/VALUES (?, ?, ?, ?, ?, ?) IF NOT EXISTS USING TTL 3600/'
	expect_red '^TestIdentityAuthorityPinsGlobalSerialExplicitly$' "M16a: claim given a TTL" "must not carry a TTL"
}

m16_delete_path() {
	mutate 's/(func classifyIdentityClaim)/\/\/ DELETE FROM identity_authority_claims WHERE library_id = ?\n$1/'
	expect_red '^TestIdentityAuthorityClaimIsWriteOnce$' "M16b: a delete path added to the primitive" "must not contain"
}

m16_conflict_collapsed() {
	mutate 's/if stored != nil && stored\.DigestVersion == digestVersion && stored\.Digest == digest \{\r?\n\t\treturn IdentityClaimIdempotent/if stored != nil {\n\t\treturn IdentityClaimIdempotent/'
	expect_red '^TestClassifyIdentityClaimRefusesDifferentDigest$' "M16c: a different digest classified as an idempotent retry" "want conflict"
}

m16_nil_readback_as_fresh() {
	mutate 's/\treturn IdentityClaimConflict\r?\n\}/\tif stored == nil {\n\t\treturn IdentityClaimEstablished\n\t}\n\treturn IdentityClaimConflict\n}/'
	expect_red '^TestClassifyIdentityClaimRefusesDifferentDigest$' "M16d: missing read-back treated as a fresh first claim" "never a fresh first claim"
}

# --- SERIAL domain ------------------------------------------------------------
m_serial_local() {
	mutate 's/SerialConsistency\(LibraryHeadSerialConsistency\)/SerialConsistency(gocql.LocalSerial)/'
	expect_red '^TestIdentityAuthorityPinsGlobalSerialExplicitly$' "claim demoted to LOCAL_SERIAL" "LOCAL_SERIAL"
}

# --- no-bypass inventory ------------------------------------------------------
m_inventory_new_writer() {
	mutate_prod 's/(func cleanupRolledBackLibraryDerivedState)/var pcd1bMutationLeak = "INSERT INTO commits (library_id) VALUES (?)"\n\n$1/'
	expect_red '^TestIdentityWritersAreInventoried$' "inventory: an uninventoried commits writer added to production" "unlisted commits/fs_objects writer"
}

m_inventory_entry_dropped() {
	mutate_inventory 's/\t\{path: "internal\/gc\/store_cassandra\.go", decl: "CassandraStore\.DeleteFSObject", shape: identityWriteDelete,\r?\n\t\tnote: "same cascade"\},\r?\n//'
	expect_red '^TestIdentityWritersAreInventoried$' "inventory: a real deleter removed from the inventory" "unlisted commits/fs_objects writer"
}

m_inventory_shape_drift() {
	mutate_inventory 's/decl: "SyncHandler\.PutCommit", shape: identityWriteInsertLWT/decl: "SyncHandler.PutCommit", shape: identityWriteInsert/'
	expect_red '^TestIdentityWriterShapesAreFrozen$' "inventory: PutCommit recorded as a plain insert although it is an LWT" "statement shape changed"
}

# --- scope guard --------------------------------------------------------------
m_premature_consumer() {
	mutate_prod 's/(func cleanupRolledBackLibraryDerivedState)/var _ = ClaimIdentityAuthority\n\n$1/'
	expect_red '^TestIdentityAuthorityHasNoProductionConsumerYet$' "scope: a production consumer of the primitive appeared" "production consumers"
}

echo "== baseline must be GREEN =="
restore
if ! run_tests 'TestIdentity|TestFileIdentity|TestClaimIdentity|TestClassifyIdentity' >/dev/null 2>&1; then
	fail "baseline is not green; fix the tests before validating mutations"
fi
echo "baseline green"

m17_drop_canonical_list
m17_drop_length_prefix
m16_ttl_on_claim
m16_delete_path
m16_conflict_collapsed
m16_nil_readback_as_fresh
m_serial_local
m_inventory_new_writer
m_inventory_entry_dropped
m_inventory_shape_drift
m_premature_consumer

restore
echo "== all PC-D1B identity-authority mutations RED as required =="
