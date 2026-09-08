#!/usr/bin/env bash
# W2 Sync PutBlock -> HEAD publication continuity — deliberate mutation matrix.
# Mirrors scripts/w2-post-head-mutation-validation.sh's structure exactly:
# one file, one backup-per-mutation, perl -0pi regex mutation, expect the
# named unit test to go RED with the exact assertion message, restore, repeat.
set -uo pipefail
cd "$(dirname "$0")/.."

SYNC=internal/api/sync.go
BACKUPS=()

green() { printf '\033[32m%s\033[0m\n' "$*"; }
red() { printf '\033[31m%s\033[0m\n' "$*" >&2; }

restore() {
  local f
  for f in "${BACKUPS[@]:-}"; do
    if [ -n "$f" ] && [ -f "$f.w2syncbak" ]; then mv -f "$f.w2syncbak" "$f"; fi
  done
  BACKUPS=()
}
fail() { red "FAILED: $*"; restore; exit 1; }
trap restore EXIT INT TERM

mutate() {
  local f="$1" expr="$2"
  cp "$f" "$f.w2syncbak"
  BACKUPS+=("$f")
  perl -0pi -e "$expr" "$f"
  cmp -s "$f" "$f.w2syncbak" && fail "mutation did not apply to $f"
}

expect_red() {
  local pattern="$1" needle="$2" what="$3" out status
  out="$(go test ./internal/api -count=1 -run "$pattern" 2>&1)"
  status=$?
  [ $status -eq 0 ] && { printf '%s\n' "$out" | tail -20 >&2; fail "$what stayed green"; }
  printf '%s\n' "$out" | grep -qF "$needle" || { printf '%s\n' "$out" | tail -30 >&2; fail "$what missed assertion: $needle"; }
  green "  RED as required: $what"
}

m_remove_own_liveness_barrier() {
  mutate "$SYNC" 's#if err := h\.ensureSyncCommitBlockOwnLiveness\(orgID, repoID, placements\); err != nil \{\s*return fmt\.Errorf\("renew own liveness: %w", err\)\s*\}\s*return h\.validateSyncCommitBlockPublicationFences\(orgID, placements\)\s*\}#return h.validateSyncCommitBlockPublicationFences(orgID, placements)\n}#s'
  expect_red '^TestEnsureSyncCommitBlockPublicationReadiness_LivenessRenewedBeforeFenceValidated$' 'want [renew:b1 validate:b1]' 'M1 remove own-liveness barrier'
  restore
}

m_move_liveness_after_validation() {
  mutate "$SYNC" 's#if err := h\.ensureSyncCommitBlockOwnLiveness\(orgID, repoID, placements\); err != nil \{\s*return fmt\.Errorf\("renew own liveness: %w", err\)\s*\}\s*return h\.validateSyncCommitBlockPublicationFences\(orgID, placements\)\s*\}#if err := h.validateSyncCommitBlockPublicationFences(orgID, placements); err != nil {\n\t\treturn err\n\t}\n\treturn h.ensureSyncCommitBlockOwnLiveness(orgID, repoID, placements)\n}#s'
  expect_red '^TestEnsureSyncCommitBlockPublicationReadiness_LivenessRenewedBeforeFenceValidated$' 'want [renew:b1 validate:b1]' 'M2 move liveness after exact-placement validation'
  restore
}

m_remove_final_exact_p_validation() {
  mutate "$SYNC" 's#if err := h\.ensureSyncCommitBlockOwnLiveness\(orgID, repoID, placements\); err != nil \{\s*return fmt\.Errorf\("renew own liveness: %w", err\)\s*\}\s*return h\.validateSyncCommitBlockPublicationFences\(orgID, placements\)\s*\}#if err := h.ensureSyncCommitBlockOwnLiveness(orgID, repoID, placements); err != nil {\n\t\treturn fmt.Errorf("renew own liveness: %w", err)\n\t}\n\treturn nil\n}#s'
  expect_red '^TestEnsureSyncCommitBlockPublicationReadiness_LivenessRenewedBeforeFenceValidated$' 'want [renew:b1 validate:b1]' 'M3 remove final exact-P validation'
  restore
}

m_bypass_provenance_scope_gate() {
  mutate "$SYNC" "s#if hasProvenance\[i\] \{#if hasProvenance[i] || !hasProvenance[i] {#"
  expect_red '^TestSyncCommitProvenancedBlockIDs_OnlyBlocksWithExistingUpReferencePass$' 'want [has-putblock]' 'M4 invert own-liveness provenance scope gate'
  restore
}

m_weaken_placement_fail_closed() {
  mutate "$SYNC" 's#if probe\.Decision != db\.BlockReuseReusable \{#if false {#'
  expect_red '^TestResolveSyncCommitBlockPlacements_NonReusableFailsClosed$' 'want wrapping v2.ErrBlockDeleteInProgress' 'M5 weaken placement fail-closed check'
  restore
}

m_weaken_exact_p_fence() {
  mutate "$SYNC" 's#if outcome != db\.BlockRepairAuthorityAuthorized \{#if false {#'
  expect_red '^TestValidateSyncCommitBlockPublicationFences_RejectsAnyNonAuthorizedOutcome$' 'want error, got nil' 'M6 weaken exact-placement fence to accept any outcome'
  restore
}

m_repair_row_queue_clears_shared_rows() {
  mutate "$SYNC" 's#return fmt\.Errorf\("queue durable publish repair for fs_object %s: %w", fsID, err\)#_ = publishRepairClearFn(database, orgID, repoID, commitID, fsID)\n\t\t\treturn fmt.Errorf("queue durable publish repair for fs_object %s: %w", fsID, err)#'
  expect_red '^TestQueueSyncCommitBlockReferenceRepairs_PartialFailureRetainsSharedRows$' 'want no shared repair rows removed after ambiguous queue failure' 'M7 repair-row queue clears a shared row on ambiguous failure'
  restore
}

m_finalize_never_clears_on_success() {
  mutate "$SYNC" 's#if err := clearSyncCommitBlockReferenceRepairsFn\(h\.db, orgID, repoID, targetCommitID, canonicalByFile\); err != nil \{\s*log\.Printf\("\[%s\] WARNING: published repo=%s commit=%s but failed to clear queued publish repair: %v", label, repoID, targetCommitID, err\)\s*\}\s*return nil\s*\}#return nil\n}#s'
  expect_red '^TestFinalizeSyncCommitBlockDeltaAndSettleRepairIntent_ClearsOnSuccess$' 'want [fs-r25] cleared on successful finalize' 'M8 finalize wrapper never clears the repair row on success'
  restore
}

m_unknown_failure_performs_cleanup() {
  mutate "$SYNC" 's#if err := h\.finalizeSyncCommitBlockDelta\(orgID, repoID, targetCommitID, delta\); err != nil \{\s*scheduleSyncCommitBlockReferenceRepairs\(h\.db, orgID, repoID, targetCommitID, canonicalByFile, label\)\s*return err\s*\}#if err := h.finalizeSyncCommitBlockDelta(orgID, repoID, targetCommitID, delta); err != nil {\n\t\t_ = clearSyncCommitBlockReferenceRepairsFn(h.db, orgID, repoID, targetCommitID, canonicalByFile)\n\t\tscheduleSyncCommitBlockReferenceRepairs(h.db, orgID, repoID, targetCommitID, canonicalByFile, label)\n\t\treturn err\n\t}#s'
  expect_red '^TestFinalizeSyncCommitBlockDeltaAndSettleRepairIntent_SchedulesOnFailureNeverClears$' 'clear was called' 'M9 a failed/unknown finalize outcome performs repair-row cleanup'
  restore
}

m_cross_file_block_id_leakage() {
  mutate "$SYNC" 's#for _, raw := range file\.blockIDs \{#for _, raw := range union {#'
  expect_red '^TestResolveSyncCommitAddedFilesCanonical_PerFileAssociationNoCrossFileLeakage$' 'fs-1 canonical = [a b c], want [a b]' 'M10 canonical resolution leaks blocks across files'
  restore
}

m_auto_merge_queues_before_readiness() {
  mutate "$SYNC" 's#if err := h\.ensureSyncCommitBlockPublicationReadiness\(orgID, repoID, canonicalByFile\); err != nil \{\s*return err\s*\}\s*if err := queueSyncCommitBlockReferenceRepairsFn\(h\.db, orgID, repoID, commitID, canonicalByFile\); err != nil \{\s*return \&syncAutoMergeRepairQueueError\{err: err\}\s*\}#if err := queueSyncCommitBlockReferenceRepairsFn(h.db, orgID, repoID, commitID, canonicalByFile); err != nil {\n\t\treturn \&syncAutoMergeRepairQueueError{err: err}\n\t}\n\tif err := h.ensureSyncCommitBlockPublicationReadiness(orgID, repoID, canonicalByFile); err != nil {\n\t\treturn err\n\t}#s'
  expect_red '^TestAutoMergeSyncPublicationReadinessPrecedesRepairQueue$' 'queue was called' 'M11 auto-merge queues repair before readiness'
  restore
}

MUTATIONS=(
  m_remove_own_liveness_barrier
  m_move_liveness_after_validation
  m_remove_final_exact_p_validation
  m_bypass_provenance_scope_gate
  m_weaken_placement_fail_closed
  m_weaken_exact_p_fence
  m_repair_row_queue_clears_shared_rows
  m_finalize_never_clears_on_success
  m_unknown_failure_performs_cleanup
  m_cross_file_block_id_leakage
  m_auto_merge_queues_before_readiness
)

if [ "${1:-}" = "--list" ]; then
  printf '%s\n' "${MUTATIONS[@]}"
  exit 0
fi

printf 'Baseline (unmutated) must be green...\n'
go test ./internal/api -count=1 >/dev/null 2>&1 || fail 'the unmutated internal/api suite is already red'
green '  baseline green'

if [ $# -gt 0 ]; then
MUTATIONS=("$1")
fi

count=0
for mutation in "${MUTATIONS[@]}"; do
  printf '\n%s\n' "$mutation"
  declare -F "$mutation" >/dev/null || fail "unknown mutation $mutation"
  "$mutation"
  count=$((count + 1))
done

restore
green "All $count W2 Sync PutBlock->HEAD mutations produced the expected red."
