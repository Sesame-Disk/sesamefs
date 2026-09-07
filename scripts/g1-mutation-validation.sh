#!/usr/bin/env bash
#
# G1 mutation evidence. Each mutation removes one exact-identity, durable-root,
# or fail-closed invariant and must make its focused test red.
#
#   ./scripts/g1-mutation-validation.sh          # run every mutation
#   ./scripts/g1-mutation-validation.sh <name>   # run one mutation
#   ./scripts/g1-mutation-validation.sh --list   # list mutations
#
# Unit-level only: no Cassandra, MinIO, or application stack is required.

set -uo pipefail
cd "$(dirname "$0")/.."

MIGRATION=internal/db/migrations/021_gc_s3_orphan_exact_identity.cql
STORE=internal/gc/store_cassandra.go
MOCK=internal/gc/store_mock.go
WORKER=internal/gc/worker.go

green() { printf '\033[32m%s\033[0m\n' "$*"; }
red() { printf '\033[31m%s\033[0m\n' "$*" >&2; }

BACKUPS=()
restore() {
  local f
  for f in "${BACKUPS[@]:-}"; do
    if [ -n "$f" ] && [ -f "$f.g1bak" ]; then mv -f "$f.g1bak" "$f"; fi
  done
  BACKUPS=()
}
fail() { red "FAILED: $*"; restore; exit 1; }
trap restore EXIT INT TERM

mutate() {
  local f="$1" expr="$2"
  cp "$f" "$f.g1bak"
  BACKUPS+=("$f")
  perl -0pi -e "$expr" "$f"
  if cmp -s "$f" "$f.g1bak"; then
    fail "mutation did not apply to $f"
  fi
}

expect_red() {
  local pattern="$1" needle="$2" what="$3" out status
  out="$(go test ./internal/gc -count=1 -run "$pattern" 2>&1)"
  status=$?
  if [ $status -eq 0 ]; then
    printf '%s\n' "$out" >&2
    fail "$what: the suite stayed green"
  fi
  if [ -n "$needle" ] && ! printf '%s\n' "$out" | grep -qF "$needle"; then
    printf '%s\n' "$out" >&2
    fail "$what: failed without the expected assertion: $needle"
  fi
  green "  RED as required: $what"
}

m_exact_canonical_identity() {
  mutate "$MIGRATION" 's{PRIMARY KEY \(\(org_id, block_id\), storage_class, storage_key, gc_claim_id, gc_claimed_at\)}{PRIMARY KEY ((org_id, block_id), storage_class, gc_claim_id, gc_claimed_at)}'
  out="$(go test ./internal/db -count=1 -run TestR26MigrationDeclaresTheExactIdentityKeys 2>&1)"
  [ $? -ne 0 ] || fail 'canonical orphan key mutation stayed green'
  green '  RED as required: canonical orphan identity loses storage_key'
  restore
}

m_exact_projection_identity() {
  mutate "$MIGRATION" 's{PRIMARY KEY \(\(first_seen_day, bucket\), first_seen_at, org_id, block_id,\s+storage_class, storage_key, gc_claim_id, gc_claimed_at\)}{PRIMARY KEY ((first_seen_day, bucket), first_seen_at, org_id, block_id)}'
  out="$(go test ./internal/db -count=1 -run TestR26MigrationDeclaresTheExactIdentityKeys 2>&1)"
  [ $? -ne 0 ] || fail 'discovery identity mutation stayed green'
  green '  RED as required: discovery identity loses P,D'
  restore
}

m_recovery_root_required() {
  mutate "$MIGRATION" 's{CREATE TABLE IF NOT EXISTS gc_s3_orphan_recovery_roots}{CREATE TABLE IF NOT EXISTS gc_s3_orphan_recovery_roots_removed}'
  out="$(go test ./internal/db -count=1 -run TestG1MigrationDeclaresExactOrphanRecoveryRoot 2>&1)"
  [ $? -ne 0 ] || fail 'recovery-root migration mutation stayed green'
  green '  RED as required: durable recovery root disappears'
  restore
}

m_no_orphan_ttl() {
  mutate "$MIGRATION" 's{default_time_to_live = 0}{default_time_to_live = 7776000}'
  expect_red 'TestS3OrphanMigrationHasNoExpirySchedule' 'must have no expiry schedule' \
    'pending orphan identity regains a TTL'
  restore
}

m_root_before_canonical() {
  mutate "$STORE" 's{rootFirstSeenAt := now}{rootFirstSeenAt := now\n\t// INSERT INTO gc_s3_orphans before root would violate G1}'
  expect_red 'TestG1SourceContractsKeepRootBeforeCanonicalAndSettlementBounded' 'recovery root must publish before canonical insert' \
    'canonical orphan publication moves before the recovery root'
  restore
}

m_prepared_fail_closed() {
	mutate "$STORE" 's{state != ""}{false}'
  expect_red 'TestP4B_CanonicalVisibilityClassification|TestG1PreparedRecoveryStateIsRetainedWithoutPhysicalDelete' 'PREPARED' \
    'PREPARED becomes SameAuthority'
  restore
}

m_terminal_settles_projection_first() {
  mutate "$WORKER" 's{w\.terminateThenDeleteS3Orphan\(root\.OrgID, root\.BlockID, root\.FirstSeenAt, committedBlockDeleteAuthority\(root\.Authority\)\)}{w.store.DeleteS3OrphanRecoveryRoot(root.OrgID, root.BlockID, root.Authority)}'
  expect_red 'TestG1TerminalLifecycleCleansRootAfterCanonicalLoss|TestG1TerminalRootSettlementIsPageBoundedAndExact' 'projection' \
    'terminal root cleanup skips exact discovery settlement'
  restore
}

m_root_first_seen_is_stable() {
  mutate "$MOCK" 's{rootFirstSeenAt = existing.FirstSeenAt}{_ = existing}'
  expect_red 'TestG1RepeatedPublicationIsIdempotentForExactIdentity' 'stable' \
    'replayed root overwrites first_seen_at'
  restore
}

m_root_settlement_is_page_bounded() {
  mutate "$WORKER" 's{cleaned := 0}{cleaned := 0\n\t// settledRoots would violate bounded root settlement}'
  expect_red 'TestG1SourceContractsKeepRootBeforeCanonicalAndSettlementBounded' 'must not accumulate a bucket' \
    'root settlement accumulates all terminal roots'
  restore
}

m_root_does_not_rewind_history() {
  mutate "$WORKER" 's{cleaned := 0}{cleaned := 0\n\t// earliestRootCanonical rewind would violate bounded scheduling}'
  expect_red 'TestG1SourceContractsKeepRootBeforeCanonicalAndSettlementBounded' 'must not accumulate a bucket' \
    'root settlement reintroduces historical rewind'
  restore
}

MUTATIONS=(
  m_exact_canonical_identity
  m_exact_projection_identity
  m_recovery_root_required
  m_no_orphan_ttl
  m_root_before_canonical
  m_prepared_fail_closed
  m_terminal_settles_projection_first
  m_root_first_seen_is_stable
  m_root_settlement_is_page_bounded
  m_root_does_not_rewind_history
)

if [ "${1:-}" = "--list" ]; then
  printf '%s\n' "${MUTATIONS[@]}"
  exit 0
fi

printf 'Baseline (unmutated) must be green...\n'
if ! go test ./internal/gc -count=1 >/dev/null 2>&1; then
  fail 'the unmutated internal/gc suite is already red'
fi
green '  baseline green'

if [ $# -gt 0 ]; then
  MUTATIONS=("$1")
fi

for mutation in "${MUTATIONS[@]}"; do
  printf '\n%s\n' "$mutation"
  "$mutation"
done

restore
green "All ${#MUTATIONS[@]} G1 mutation(s) produced the expected red."
