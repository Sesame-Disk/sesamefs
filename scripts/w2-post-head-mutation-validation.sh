#!/usr/bin/env bash
# Unit mutation evidence for the W2 CreateFileFromBlocks post-HEAD repair slice.
set -uo pipefail
cd "$(dirname "$0")/.."

REPAIR=internal/api/v2/publish_repair.go
FILE_FROM_BLOCKS=internal/api/v2/file_from_blocks.go
BACKUPS=()
green() { printf '\033[32m%s\033[0m\n' "$*"; }
red() { printf '\033[31m%s\033[0m\n' "$*" >&2; }
restore() {
  local f
  for f in "${BACKUPS[@]:-}"; do
    if [ -n "$f" ] && [ -f "$f.w2bak" ]; then mv -f "$f.w2bak" "$f"; fi
  done
  BACKUPS=()
}
fail() { red "FAILED: $*"; restore; exit 1; }
trap restore EXIT INT TERM
mutate() {
  local f="$1" expr="$2"
  cp "$f" "$f.w2bak"
  BACKUPS+=("$f")
  perl -0pi -e "$expr" "$f"
  cmp -s "$f" "$f.w2bak" && fail "mutation did not apply to $f"
}
expect_red() {
  local pattern="$1" needle="$2" what="$3" out status
  out="$(go test ./internal/api/v2 -count=1 -run "$pattern" 2>&1)"
  status=$?
  [ $status -eq 0 ] && { printf '%s\n' "$out" | tail -20 >&2; fail "$what stayed green"; }
  printf '%s\n' "$out" | grep -qF "$needle" || { printf '%s\n' "$out" | tail -30 >&2; fail "$what missed assertion: $needle"; }
  green "  RED as required: $what"
}

m_lease_expiry_cleans_unknown() {
  mutate "$REPAIR" 's{case publishedBlockReferenceRepairCommitUnknown:\s+return fmt\.Errorf\("publication outcome for fs_object %s commit %s is unknown; retain queued repair", repair\.FSID, repair\.CommitID\)}{case publishedBlockReferenceRepairCommitUnknown:\n\t\treturn nil}'
  expect_red 'TestRepairPublishedFSObjectBlockReferenceRepair_RetainsUnknownOutcomeAfterLeaseExpiry' 'want unknown-publication retention error' 'lease expiry cleanup authority'
  restore
}
m_unrelated_head_is_declared_not_published() {
  mutate "$REPAIR" 's{\treturn publishedBlockReferenceRepairCommitUnknown, nil\r?\n}{\treturn publishedBlockReferenceRepairCommitReachable, nil\n}'
  expect_red 'TestClassifyPublishedBlockReferenceRepairCommitOutcome' 'outcome = 1, want 0' 'unknown publication classification'
  restore
}
m_repair_row_deleted_before_settlement() {
  mutate "$REPAIR" 's{(\tif classifyErr != nil \{\r?\n\t\treturn classifyErr\r?\n\t\}\r?\n)}{$1\t_ = deletePublishedBlockReferenceRepairFn(database, repair)\n}'
  expect_red 'TestRepairPublishedFSObjectBlockReferenceRepair_RetainsUnknownOutcomeAfterLeaseExpiry' 'repair row should not be deleted for unknown publication' 'repair-row deletion before settlement'
  restore
}
m_head_read_is_weak() {
  mutate "$REPAIR" 's{\.Consistency\(gocql\.Serial\)}{}'
  expect_red 'TestPublishedBlockReferenceRepairAuthorityReadsAreColdAndExplicit' 'must settle the canonical HEAD in the SERIAL domain' 'weak repair HEAD read'
  restore
}
m_parent_read_is_local_only() {
  mutate "$REPAIR" 's{\.Consistency\(gocql\.EachQuorum\)}{.Consistency(gocql.LocalQuorum)}'
  expect_red 'TestPublishedBlockReferenceRepairAuthorityReadsAreColdAndExplicit' 'must use EachQuorum' 'local-only repair ancestry read'
  restore
}
m_reachability_ignores_ancestry() {
  mutate "$REPAIR" 's{return classifyPublishedCommitReachability\(ctx, commitID, headCommitID, publishedCommitReachabilityMaxNodes, parentLookup\)}{return publishedBlockReferenceRepairCommitUnknown, nil}'
  expect_red 'TestClassifyPublishedBlockReferenceRepairCommitOutcome' 'outcome = 0, want 1' 'HEAD-only reachability classification'
  restore
}
m_ancestry_limit_becomes_negative() {
  mutate "$REPAIR" 's{return publishedBlockReferenceRepairCommitUnknown, fmt\.Errorf\("commit ancestry walk reached %d-node limit}{return publishedBlockReferenceRepairCommitDefinitelyNotReachable, fmt.Errorf("commit ancestry walk reached %d-node limit}'
  expect_red 'TestClassifyPublishedCommitReachabilityBoundsWorkWithoutFalseNegatives' 'want UNKNOWN limit error' 'ancestry limit as negative authority'
  restore
}
m_parent_error_becomes_negative() {
  mutate "$REPAIR" 's{return publishedBlockReferenceRepairCommitUnknown, fmt\.Errorf\("lookup parent for commit %s: %w", currentCommitID, err\)}{return publishedBlockReferenceRepairCommitDefinitelyNotReachable, fmt.Errorf("lookup parent for commit %s: %w", currentCommitID, err)}'
  expect_red 'TestClassifyPublishedBlockReferenceRepairCommitOutcome' 'outcome = 2, want 0' 'parent read error as negative authority'
  restore
}
m_ancestry_skips_parent() {
  mutate "$REPAIR" 's{currentCommitID = parentCommitID}{currentCommitID = ""}'
  expect_red 'TestClassifyPublishedBlockReferenceRepairCommitOutcome' 'outcome = 0, want 1' 'skipped ancestry parent'
  restore
}
m_hot_path_pays_serial_per_block() {
  mutate "$FILE_FROM_BLOCKS" 's{outcome, err := validateBorrowedFSPublicationAuthorityFn}{outcome, err := validateBlockRepairAuthorityFn}'
  expect_red 'TestValidateCommitBlockPublicationFencesStaysOffSerialAuthority' 'must call validateBorrowedFSPublicationAuthorityFn' 'SERIAL authority on CreateFileFromBlocks hot path'
  restore
}
m_cleanup_uses_the_wrong_attempt_identity() {
  mutate "$REPAIR" 's{cleanupFailedPublishRemoveAttemptReferencesFn\(database, orgID, attemptID, blockIDs\)}{cleanupFailedPublishRemoveAttemptReferencesFn(database, orgID, commitID, blockIDs)}'
  expect_red 'TestCleanupFailedPublishArtifacts_DeletesCommitAndDedupesAttemptRefs' 'remove refs args' 'wrong loser cleanup identity'
  restore
}
m_settlement_delete_is_conditional() {
  mutate "$REPAIR" 's{(WHERE bucket = \? AND org_id = \? AND repo_id = \? AND commit_id = \? AND fs_id = \?\r?\n\s*)(`, repair\.Bucket, repair\.OrgID, repair\.RepoID, repair\.CommitID, repair\.FSID\))}{$1IF EXISTS\n$2}'
  expect_red 'TestPublishedBlockReferenceRepairSettlementUsesOrdinaryWrites' 'settlement must not enter the repair row' 'conditional settlement delete'
  restore
}
m_settlement_insert_uses_serial_consistency() {
  mutate "$REPAIR" 's{repair\.LeaseExpiresAt\)\.Exec\(\)}{repair.LeaseExpiresAt).SerialConsistency(gocql.Serial).Exec()}'
  expect_red 'TestPublishedBlockReferenceRepairSettlementUsesOrdinaryWrites' 'ordinary INSERT' 'serial-consistency settlement insert'
  restore
}
m_retry_backoff_removes_process_local_state() {
  mutate "$REPAIR" 's{publishedBlockReferenceRepairNextRetryAt\.Store\(publishedBlockReferenceRepairRetryKey\(repair\), nextRetryAt\.UTC\(\)\)}{publishedBlockReferenceRepairNextRetryAt.Delete(publishedBlockReferenceRepairRetryKey(repair))}'
  expect_red 'TestSchedulePublishedBlockReferenceRepairRetryUsesProcessLocalState' 'local retry state =' 'removing process-local retry state'
  restore
}
m_retry_hint_prune_is_missing() {
  mutate "$REPAIR" 's{\tprunePublishedBlockReferenceRepairRetryHints\(now\)\r?\n}{\t// retry hint pruning disabled by mutation\n}'
  expect_red 'TestRunPublishedBlockReferenceRepairSweepPrunesExpiredRetryHintsForMissingRows' 'local retry state remains after pruning expired hint' 'expired retry-hint pruning'
  restore
}

MUTATIONS=(m_lease_expiry_cleans_unknown m_unrelated_head_is_declared_not_published m_repair_row_deleted_before_settlement m_head_read_is_weak m_parent_read_is_local_only m_reachability_ignores_ancestry m_ancestry_limit_becomes_negative m_parent_error_becomes_negative m_ancestry_skips_parent m_hot_path_pays_serial_per_block m_cleanup_uses_the_wrong_attempt_identity m_settlement_delete_is_conditional m_settlement_insert_uses_serial_consistency m_retry_backoff_removes_process_local_state m_retry_hint_prune_is_missing)
if [ "${1:-}" = "--list" ]; then printf '%s\n' "${MUTATIONS[@]}"; exit 0; fi
printf 'Baseline (unmutated) must be green...\n'
go test ./internal/api/v2 -count=1 >/dev/null 2>&1 || fail 'the unmutated internal/api/v2 suite is already red'
green '  baseline green'
if [ $# -gt 0 ]; then MUTATIONS=("$1"); fi
for mutation in "${MUTATIONS[@]}"; do
  printf '\n%s\n' "$mutation"
  declare -F "$mutation" >/dev/null || fail "unknown mutation $mutation"
  "$mutation"
done
restore
green "All W2 post-HEAD mutations produced the expected red."
