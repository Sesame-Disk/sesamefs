#!/usr/bin/env bash
#
# G2 mutation evidence: prove the PREPARED -> COMMITTED handoff and its
# recovery/cleanup boundaries fail closed when their load-bearing predicates
# are removed.
#
#   ./scripts/g2-mutation-validation.sh          # run every mutation
#   ./scripts/g2-mutation-validation.sh <name>   # run one mutation
#   ./scripts/g2-mutation-validation.sh --list   # names only
#
# Unit-level only: no Cassandra, MinIO, or application stack is required. The
# real Abort-vs-Commit race is covered by the integration test
# TestG2AbortAndCommitRaceAtRealCassandra when P4B evidence is enabled.

set -uo pipefail
cd "$(dirname "$0")/.."

STORE=internal/gc/store_cassandra.go
MOCK=internal/gc/store_mock.go
WORKER=internal/gc/worker.go

green() { printf '\033[32m%s\033[0m\n' "$*"; }
red() { printf '\033[31m%s\033[0m\n' "$*" >&2; }

BACKUPS=()
restore() {
  local file
  for file in "${BACKUPS[@]:-}"; do
    if [ -n "$file" ] && [ -f "$file.g2bak" ]; then
      mv -f "$file.g2bak" "$file"
    fi
  done
  BACKUPS=()
}
fail() { red "FAILED: $*"; restore; exit 1; }
trap restore EXIT INT TERM

mutate() {
  local file="$1" expression="$2" before
  if [ ! -f "$file.g2bak" ]; then
    cp "$file" "$file.g2bak"
    BACKUPS+=("$file")
  fi
  before="$(mktemp)"
  cp "$file" "$before"
  perl -0pi -e "$expression" "$file"
  if cmp -s "$file" "$before"; then
    rm -f "$before"
    fail "mutation did not apply to $file"
  fi
  rm -f "$before"
}

expect_red() {
  local pattern="$1" needle="$2" what="$3" output status
  output="$(go test ./internal/gc -count=1 -run "$pattern" 2>&1)"
  status=$?
  if [ "$status" -eq 0 ]; then
    fail "$what: the suite stayed green"
  fi
  if ! printf '%s\n' "$output" | grep -qF "$needle"; then
    fail "$what: failed without the expected G2 assertion: $needle"
  fi
  green "  RED as required: $what"
}

m_abort_drops_handoff_predicate() {
  mutate "$STORE" 's{(func \(s \*CassandraStore\) AbortBlockDeleteHandoff.*?gc_claimed_at = \?)[[:space:]]+AND gc_orphan_handoff = null}{$1}s'
  expect_red 'TestG2AbortSourceRequiresExactUncommittedOwner' 'gc_orphan_handoff = null' \
    'Abort CAS drops the uncommitted-handoff predicate'
  restore
}

m_not_owner_skips_exact_state_classification() {
  mutate "$WORKER" 's{switch strings\.TrimSpace\(exact\.RecoveryState\)}{switch ""}'
  expect_red 'TestG2NotOwnerRecoveryClassifiesExactState' 'switch strings.TrimSpace(exact.RecoveryState)' \
    'NotOwner recovery skips exact PREPARED/COMMITTED classification'
  restore
}

m_prepared_skips_irreversible_commit() {
  mutate "$WORKER" 's/if !alreadyCommitted \{(\s+prepared :=)/if false {$1/'
  expect_red 'TestG2ProcessBlockStopsAtCommittedHandoff' 'canonical block is not left at committed handoff' \
    'PREPARED is treated as irreversible without committing D'
  restore
}

m_promote_drops_handoff_guard() {
  mutate "$MOCK" 's{(!stored\.sameAuthority\(proposed\) \|\| row\.GCState != db\.BlockGCStateDeleting) \|\| !orphanHandoffCommitted\(row\.GCOrphanHandoff\)}{$1}'
  expect_red 'TestG2PromoteRequiresCommittedHandoff' 'promotion without committed handoff' \
    'promotion accepts a PREPARED block without committed D'
  restore
}

m_cleanup_drops_claim_identity() {
  mutate "$STORE" 's{(func \(s \*CassandraStore\) DeletePreparedBlockDeleteOrphan.*?)(?=// StartBlockDeleteOrphan)}{my $body=$1; $body =~ s/ AND gc_claim_id = \? AND gc_claimed_at = \?//g; $body}se'
  expect_red 'TestG2DeletePreparedUsesExactRecoveryIdentity' 'gc_claim_id = ?' \
    'PREPARED cleanup drops the exact D identity'
  restore
}

m_cleanup_touches_sibling_authority() {
  mutate "$MOCK" 's|(func \(m \*MockStore\) DeletePreparedBlockDeleteOrphan.*?)(?=func \(m \*MockStore\) StartBlockDeleteOrphan)|my $body=$1; $body =~ s/delete\(m\.s3Orphans, key\)/for candidate := range m.s3Orphans { if candidate.OrgID == orgID { if candidate.BlockID == blockID { delete(m.s3Orphans, candidate) } } }/g; $body|se'
  expect_red 'TestG2RecoveryCleansOnlyD1WhenD2SharesPhysicalTarget' 'D2 PREPARED was touched while cleaning D1' \
    'PREPARED cleanup touches a sibling D2 authority'
  restore
}

m_root_only_cleanup_deletes_absent_canonical() {
	mutate "$WORKER" 's{(case StartBlockDeleteOrphanNotPublished:)}{$1\n\t\t\t\t\t_ = w.store.DeletePreparedBlockDeleteOrphan(root.OrgID, root.BlockID, root.Authority)}'
	expect_red 'TestG2RecoveryRetainsRootAcrossLatePreparedProducer' 'late-producer root =' \
		'root-only cleanup deletes a root before a late PREPARED publication'
	  restore
}

MUTATIONS=(
  m_abort_drops_handoff_predicate
  m_not_owner_skips_exact_state_classification
  m_prepared_skips_irreversible_commit
  m_promote_drops_handoff_guard
  m_cleanup_drops_claim_identity
  m_cleanup_touches_sibling_authority
  m_root_only_cleanup_deletes_absent_canonical
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

if [ "$#" -gt 0 ]; then
  MUTATIONS=("$1")
fi

for mutation in "${MUTATIONS[@]}"; do
  printf '\n%s\n' "$mutation"
  "$mutation"
done

green "G2 mutations: all ${#MUTATIONS[@]} RED as required"
