#!/usr/bin/env bash
#
# G3 mutation evidence: prove that processBlock's gate before canonical
# retirement, and FinalizeBlockDelete's own exact-(P,D) predicate, actually
# matter — removing any one of them must turn a G3 test RED.
#
#   ./scripts/g3-canonical-retirement-mutation-validation.sh          # run every mutation
#   ./scripts/g3-canonical-retirement-mutation-validation.sh <name>   # run one (see --list)
#   ./scripts/g3-canonical-retirement-mutation-validation.sh --list   # names only
#
# G1/G2 mutation evidence remains scripts/p4b-authority-mutation-validation.sh
# and scripts/x1-nonoverlap-mutation-validation.sh. This script is unit-level
# only (MockStore): no Cassandra, MinIO, or application stack is required.

set -uo pipefail
cd "$(dirname "$0")/.."

MOCK=internal/gc/store_mock.go
WORKER=internal/gc/worker.go

green() { printf '\033[32m%s\033[0m\n' "$*"; }
red() { printf '\033[31m%s\033[0m\n' "$*" >&2; }

BACKUPS=()
restore() {
  local f
  for f in "${BACKUPS[@]:-}"; do
    if [ -n "$f" ] && [ -f "$f.g3bak" ]; then mv -f "$f.g3bak" "$f"; fi
  done
  BACKUPS=()
}
fail() { red "FAILED: $*"; restore; exit 1; }
trap restore EXIT INT TERM

mutate() {
  local f="$1" expr="$2" before
  if [ ! -f "$f.g3bak" ]; then
    cp "$f" "$f.g3bak"
    BACKUPS+=("$f")
  fi
  before="$(mktemp)"
  cp "$f" "$before"
  perl -0pi -e "$expr" "$f"
  if cmp -s "$f" "$before"; then
    rm -f "$before"
    fail "mutation did not apply to $f"
  fi
  rm -f "$before"
}

expect_red() {
  local pattern="$1" needle="$2" what="$3" out status
  out="$(go test ./internal/gc -count=1 -run "$pattern" 2>&1)"
  status=$?
  if [ $status -eq 0 ]; then
    printf '%s\n' "$out" | tail -15 >&2
    fail "$what: the suite stayed green"
  fi
  if ! printf '%s\n' "$out" | grep -qF "$needle"; then
    printf '%s\n' "$out" | tail -25 >&2
    fail "$what: failed without the expected G3 assertion: $needle"
  fi
  green "  RED as required: $what"
}

# --- processBlock gate mutations -------------------------------------------
#
# FinalizeBlockDelete's own CAS only consults blocks.gc_orphan_handoff, which
# is already true as soon as CommitBlockDeleteOrphanHandoff applies -- BEFORE
# the orphan is promoted to COMMITTED. So the gate in processBlock (never
# reaching finalizeAfterCommittedHandoff on a non-success Promote outcome) is
# the ONLY thing standing between "D committed" and "orphan actually
# COMMITTED, confirmed exactly". Removing it lets Finalize retire blocks(L)
# while the orphan is still merely PREPARED or ambiguous.

m_ambiguous_promotion_reaches_finalize() {
  # Move Ambiguous into the success case list, so a Promote outcome that never
  # actually confirmed exact COMMITTED still falls through to Finalize.
  # StartBlockDeleteOrphanCreated/SameAuthority also label an unrelated switch
  # in classifyPreparedBlockDeleteOutcome earlier in the file, so anchor on
  # processBlock's own trailing comment to mutate the right one.
  mutate "$WORKER" 's{case StartBlockDeleteOrphanCreated, StartBlockDeleteOrphanSameAuthority:\s+// Exact committed blocks\(P,D\) authority}{case StartBlockDeleteOrphanCreated, StartBlockDeleteOrphanSameAuthority, StartBlockDeleteOrphanAmbiguous:\n\t\t// Exact committed blocks(P,D) authority}'
  mutate "$WORKER" 's{case StartBlockDeleteOrphanAmbiguous, StartBlockDeleteOrphanProjectionUnconfirmed:\s+w\.recordDestructiveBlocked\(destructivePathBlock\)\s+return blockDeleteCommittedPendingError\{ItemID: item\.ItemID, Err: promotion\.Cause\}}{case StartBlockDeleteOrphanProjectionUnconfirmed:\n\t\tw.recordDestructiveBlocked(destructivePathBlock)\n\t\treturn blockDeleteCommittedPendingError{ItemID: item.ItemID, Err: promotion.Cause}}'
  expect_red 'TestG3PromoteAmbiguousNeverReachesFinalize' 'canonical row is gone' \
    'an ambiguous Promote outcome falls through to finalizeAfterCommittedHandoff'
  restore
}

m_finalize_runs_unconditionally_after_promote() {
  # Empty out every non-success case body in the Promote-outcome switch (all
  # three read identically), so every outcome falls through to Finalize.
  mutate "$WORKER" 's{return blockDeleteCommittedPendingError\{ItemID: item\.ItemID, Err: promotion\.Cause\}}{}g'
  expect_red 'TestG3PromoteAmbiguousNeverReachesFinalize' 'canonical row is gone' \
    'processBlock reaches Finalize regardless of the Promote outcome'
  restore
}

# --- FinalizeBlockDelete exact-(P,D) mutations (MockStore only, matching what
# the G3 unit tests exercise; the equivalent Cassandra CQL predicate is not
# text-mutated here — it is covered by real-cluster integration evidence
# instead, see each mutation's own comment) ---------------------------------

m_finalize_ignores_exact_p() {
  # Mock-only: this is what the G3 unit tests exercise. The equivalent
  # Cassandra CQL predicate (storage_class/storage_key in Finalize's IF
  # clause) is covered by real-cluster integration evidence instead of a
  # text mutation here.
  mutate "$MOCK" 's{if \(BlockDeleteTarget\{StorageClass: b\.StorageClass, StorageKey: b\.StorageKey\}\) != proposed\.Target \{\s+return BlockDeleteFinalizeResult\{\s+Outcome: BlockDeleteNotAuthority,\s+Cause:   fmt\.Errorf\("block delete finalize not applied for %s", blockID\),\s+\}, fmt\.Errorf\("block delete finalize not applied for %s", blockID\)\s+\}}{}'
  expect_red 'TestG3FinalizeExactPMismatchFailsClosed' 'want refusal' \
    'FinalizeBlockDelete ignores the exact physical target P'
  restore
}

m_finalize_ignores_exact_d() {
  # Use a non-brace delimiter: the replacement text itself opens an unbalanced
  # "{", which a brace-delimited s{}{} cannot parse.
  mutate "$MOCK" 's#if b\.GCState != db\.BlockGCStateDeleting \|\| b\.GCClaimID != proposed\.ClaimID \{#if false {#'
  mutate "$MOCK" 's#if b\.GCClaimedAt == nil \|\| !b\.GCClaimedAt\.Equal\(proposed\.ClaimedAt\) \{#if false {#'
  expect_red 'TestG3FinalizeExactDMismatchFailsClosed' 'want refusal' \
    'FinalizeBlockDelete ignores the exact delete authority D (claim id / claimed_at)'
  restore
}

m_finalize_drops_committed_check() {
  mutate "$MOCK" 's{if !orphanHandoffCommitted\(b\.GCOrphanHandoff\) \{\s+return BlockDeleteFinalizeResult\{\s+Outcome: BlockDeleteNotAuthority,\s+Cause:   fmt\.Errorf\("block delete finalize not applied for %s", blockID\),\s+\}, fmt\.Errorf\("block delete finalize not applied for %s", blockID\)\s+\}}{}'
  expect_red 'TestG3PreparedAloneNeverAuthorizesFinalize' 'want refusal' \
    'FinalizeBlockDelete no longer requires the committed handoff bit'
  restore
}

# --- G3-3: Finalize must not consume continuation authority -----------------

m_finalize_deletes_orphan_after_success() {
  mutate "$WORKER" 's{(canonical row retired after committed handoff; orphan %s remains COMMITTED and durable for the physical-delete continuation", item\.ItemID, authority\.ClaimID\))\s+(if err := w\.settleFinalizedBlockCandidate\(item, candidate\); err != nil \{)}{$1\n\t\t_ = w.store.DeleteS3Orphan(item.OrgID, item.ItemID, authority, time.Time{})\n\t\t$2}'
  expect_red 'TestG3FinalizeSurvivesOrphanRecoveryAuthority' 'want exactly one surviving COMMITTED authority' \
    'a successful Finalize deletes the surviving orphan/recovery authority'
  restore
}

MUTATIONS=(
  m_ambiguous_promotion_reaches_finalize
  m_finalize_runs_unconditionally_after_promote
  m_finalize_ignores_exact_p
  m_finalize_ignores_exact_d
  m_finalize_drops_committed_check
  m_finalize_deletes_orphan_after_success
)

if [ "${1:-}" = "--list" ]; then
  printf '%s\n' "${MUTATIONS[@]}"
  exit 0
fi

printf 'Baseline (unmutated) must be green...\n'
if ! go test ./internal/gc -count=1 -run 'TestG3' >/dev/null 2>&1; then
  fail 'the unmutated G3 suite is already red'
fi
green '  baseline green'

if [ $# -gt 0 ]; then
  requested="$1"
  if ! printf '%s\n' "${MUTATIONS[@]}" | grep -qxF "$requested"; then
    fail "unknown mutation '$requested'; run with --list to see valid names"
  fi
  MUTATIONS=("$requested")
fi

for mutation in "${MUTATIONS[@]}"; do
  printf '\n%s\n' "$mutation"
  if ! "$mutation"; then
    fail "$mutation exited non-zero"
  fi
done

green "G3 canonical-retirement mutations: all ${#MUTATIONS[@]} RED as required"
