#!/usr/bin/env bash
# Unit mutation evidence for the W2 CreateFileFromBlocks post-HEAD repair slice.
set -uo pipefail
cd "$(dirname "$0")/.."

REPAIR=internal/api/v2/publish_repair.go
FILE_FROM_BLOCKS=internal/api/v2/file_from_blocks.go
BLOCK_REFS=internal/db/block_references.go
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
  # Normalize only the disposable mutation copy; Windows worktrees may be CRLF.
  perl -0pi -e 's/\r\n/\n/g' "$f"
  perl -0pi -e "$expr" "$f"
  cmp -s "$f" "$f.w2bak" && fail "mutation did not apply to $f"
}
expect_red_pkg() {
  local pkg="$1" pattern="$2" needle="$3" what="$4" out status
  out="$(go test "$pkg" -count=1 -run "$pattern" 2>&1)"
  status=$?
  [ $status -eq 0 ] && { printf '%s\n' "$out" | tail -20 >&2; fail "$what stayed green"; }
  printf '%s\n' "$out" | grep -qF "$needle" || { printf '%s\n' "$out" | tail -30 >&2; fail "$what missed assertion: $needle"; }
  green "  RED as required: $what"
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
  mutate "$REPAIR" 's/publishedCommitReachabilityWalk\{Outcome: publishedBlockReferenceRepairCommitUnknown\}, nil/publishedCommitReachabilityWalk{Outcome: publishedBlockReferenceRepairCommitReachable}, nil/'
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
  mutate "$REPAIR" 's/Outcome:\s+publishedBlockReferenceRepairCommitUnknown,\r?\n\s+NextCursor: currentCommitID/Outcome:    publishedBlockReferenceRepairCommitDefinitelyNotReachable,\n\t\tNextCursor: currentCommitID/'
  expect_red 'TestClassifyPublishedCommitReachabilityBoundsWorkWithoutFalseNegatives' 'want UNKNOWN limit error' 'ancestry limit as negative authority'
  restore
}
m_parent_error_becomes_negative() {
  mutate "$REPAIR" 's/return publishedCommitReachabilityUnknownProgress\(startCommitID, currentCommitID\), fmt.Errorf\("lookup parent/return publishedCommitReachabilityWalk{Outcome: publishedBlockReferenceRepairCommitDefinitelyNotReachable}, fmt.Errorf("lookup parent/'
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
m_retry_reanchors_to_live_head() {
  mutate "$REPAIR" 's/if strings.TrimSpace\(repair.ReachabilityAnchorHeadCommitID\) == "" \{/if true {/'
  expect_red 'TestClassifyPublishedBlockReferenceRepairResumableIgnoresMovingHEAD' 'moving HEAD was re-observed' 'retry re-anchors to live HEAD'
  restore
}
m_root_becomes_negative() {
  mutate "$REPAIR" 's/publishedCommitReachabilityWalk\{Outcome: publishedBlockReferenceRepairCommitUnknown\}, nil/publishedCommitReachabilityWalk{Outcome: publishedBlockReferenceRepairCommitDefinitelyNotReachable}, nil/'
  expect_red 'TestClassifyPublishedBlockReferenceRepairResumableRootIsNotNegativeAuthority' 'want UNKNOWN/nil' 'root as negative authority'
  restore
}
m_insert_writes_cursor_columns() {
  mutate "$REPAIR" 's{INSERT INTO published_block_reference_repairs \(bucket, org_id, repo_id, commit_id, fs_id, staged_block_ids, created_at, lease_expires_at\)}{INSERT INTO published_block_reference_repairs (bucket, org_id, repo_id, commit_id, fs_id, staged_block_ids, created_at, lease_expires_at, reachability_anchor_head_commit_id, reachability_cursor_commit_id)}'
  expect_red 'TestPublishedBlockReferenceRepairSettlementUsesOrdinaryWrites' 'ordinary INSERT must not write reachability progress columns' 'queue insert writes cursor columns'
  restore
}
m_pre_classify_renewal_removed() {
  mutate "$REPAIR" 's/\tif renewErr := renewPublishedBlockReferenceRepairLivenessIfPending\(database, &repair\); renewErr != nil \{\r?\n\t\tif errors\.Is\(renewErr, errPublishedBlockReferenceRepairGone\) \{\r?\n\t\t\treturn nil\r?\n\t\t\}\r?\n\t\treturn fmt\.Errorf\("renew publish-attempt liveness for fs_object %s before classification: %w", repair\.FSID, renewErr\)\r?\n\t\}\r?\n//'
  expect_red 'TestRepairPublishedBlockReferenceRepairRenewsLivenessBeforeClassify' 'want both renew and classify' 'M1: pre-classify renewal removed'
  restore
}
m_renewal_moved_below_classifier() {
  mutate "$REPAIR" 's/(\tif renewErr := renewPublishedBlockReferenceRepairLivenessIfPending\(database, &repair\); renewErr != nil \{\r?\n\t\tif errors\.Is\(renewErr, errPublishedBlockReferenceRepairGone\) \{\r?\n\t\t\treturn nil\r?\n\t\t\}\r?\n\t\treturn fmt\.Errorf\("renew publish-attempt liveness for fs_object %s before classification: %w", repair\.FSID, renewErr\)\r?\n\t\}\r?\n)(\tcommitOutcome, classifyErr := classify\(database, &repair\)\r?\n)/$2$1/'
  expect_red 'TestRepairPublishedBlockReferenceRepairRenewsLivenessBeforeClassify' 'want renew before classify' 'M2: renewal moved below the classifier'
  restore
}
m_classify_continues_after_renewal_error() {
  mutate "$REPAIR" 's/\t\treturn fmt\.Errorf\("renew publish-attempt liveness for fs_object %s before classification: %w", repair\.FSID, renewErr\)\r?\n/\t\tlog.Printf("renew failed, classifying anyway: %v", renewErr)\n/'
  expect_red 'TestRepairPublishedBlockReferenceRepairRenewFailureDoesNotClassify' 'classifyCalls = 1, want 0' 'M3: classifier runs after a failed renewal'
  restore
}
m_renewal_skips_still_pending_before_write() {
  mutate "$REPAIR" 's/\tif !pending \{\r?\n\t\treturn errPublishedBlockReferenceRepairGone\r?\n\t\}\r?\n/\tif false \&\& !pending {\n\t\treturn errPublishedBlockReferenceRepairGone\n\t}\n/'
  expect_red 'TestRepairPublishedBlockReferenceRepairRowGoneBeforeRenewIsTerminalNoOp' 'renewCalls = 1, want 0' 'M4: pub: written without the pre-write StillPending read'
  restore
}
m_renewal_skips_still_pending_after_write() {
  mutate "$REPAIR" 's/\tif !isGone \{\r?\n\t\treturn false, nil\r?\n\t\}\r?\n/\tif true || !isGone {\n\t\treturn false, nil\n\t}\n/'
  expect_red 'TestRepairPublishedBlockReferenceRepairRowGoneAfterRenewCompensatesExactPub' 'classify=1 promote=1 delete=1, want all 0' 'M5: post-write StillPending confirmation skipped'
  restore
}
m_renewal_compensation_removed() {
  mutate "$REPAIR" 's/\tif err := removePublishedBlockReferenceRepairOwnedPubFn\(database, repair\); err != nil \{\r?\n\t\treturn true, fmt\.Errorf\([^\n]*\)\r?\n\t\}\r?\n//'
  expect_red 'TestRepairPublishedBlockReferenceRepairRowGoneAfterRenewCompensatesExactPub' 'removeCalls = 0, want exactly one compensation' 'M6: ownerless pub: left when the row vanished after the write'
  restore
}
m_renewal_compensation_uses_commit_identity() {
  mutate "$REPAIR" 's/\treturn db\.RemovePublishAttemptReferencesAt\(database, repair\.OrgID, publishedBlockReferenceRepairLivenessAttemptID\(repair\), repair\.StagedBlockIDs, publishedBlockReferenceRepairLeaseTimestamp\(repair\.LivenessLeaseExpiresAt\)\)/\treturn db.RemovePublishAttemptReferencesAt(database, repair.OrgID, repair.CommitID, repair.StagedBlockIDs, publishedBlockReferenceRepairLeaseTimestamp(repair.LivenessLeaseExpiresAt))/'
  expect_red 'TestPublishedBlockReferenceRepairLivenessIdentityIsPerRepairRow$' 'compensation must not delete the commit-scoped v2 attempt' 'M7: compensation deletes the commit-scoped pub: shared by sibling repairs'
  restore
}
m_unknown_renews_twice_per_visit() {
  mutate "$REPAIR" 's/\tcase publishedBlockReferenceRepairCommitUnknown:\r?\n\t\treturn fmt\.Errorf\("publication outcome for fs_object %s commit %s is unknown; retain queued repair"/\tcase publishedBlockReferenceRepairCommitUnknown:\n\t\t_ = renewPublishedBlockReferenceRepairLivenessIfPending(database, \&repair)\n\t\treturn fmt.Errorf("publication outcome for fs_object %s commit %s is unknown; retain queued repair"/'
  expect_red 'TestRepairPublishedBlockReferenceRepairUnknownRenewsOncePerVisit' 'renewCalls = 2, want exactly 1' 'M8: UNKNOWN renews a second time in the same visit'
  restore
}
m_post_classify_compensation_removed() {
  mutate "$REPAIR" 's/(\t\/\/ already written\.\r?\n)\tgone, compensateErr := compensatePublishedBlockReferenceRepairLivenessIfGone\(database, repair\)\r?\n/$1\tgone, compensateErr := false, error(nil)\n/'
  expect_red 'TestRepairPublishedBlockReferenceRepairRowClearedDuringClassifyRemovesOwnPub' 'removeCalls = 0, want exactly one removal' 'M9: pub: written before the walk left ownerless when the row is cleared during the walk'
  restore
}
m_partial_renewal_failure_skips_compensation() {
  mutate "$REPAIR" 's/(\trenewErr := renewPublishedBlockReferenceRepairLivenessFn\(database, repair\)\r?\n)/$1\tif renewErr != nil {\n\t\treturn renewErr\n\t}\n/'
  expect_red 'TestRepairPublishedBlockReferenceRepairPartialRenewalFailureCompensatesWhenRowGone' 'want nil: the row is gone, the partial refs were removed' 'M10: partial renewal fan-out failure returns without the gone-check'
  restore
}
m_reachable_settlement_failure_skips_gone_check() {
  mutate "$REPAIR" 's/\tif settleErr == nil && classifyErr == nil && commitOutcome == publishedBlockReferenceRepairCommitReachable \{\r?\n\t\treturn nil\r?\n\t\}\r?\n/\tif classifyErr == nil \&\& commitOutcome == publishedBlockReferenceRepairCommitReachable {\n\t\treturn settleErr\n\t}\n/'
  expect_red 'TestRepairPublishedBlockReferenceRepairReachableSettlementFailureAfterClearRemovesOwnPub' 'want one removal of the per-repair identity after the failed settlement found the row gone' 'M11: REACHABLE settlement failure returns without the gone-check'
  restore
}
m_cleanup_intent_not_written_before_pub() {
  mutate "$REPAIR" 's/\tif err := insertPublishedBlockReferenceRepairLivenessCleanupFn\(database, \*repair\); err != nil \{\r?\n\t\treturn fmt\.Errorf\("record repair-owned liveness cleanup intent for fs_object %s: %w", repair\.FSID, err\)\r?\n\t\}\r?\n//'
  expect_red 'TestRepairPublishedBlockReferenceRepairWritesCleanupIntentBeforeRenewingPub' 'pin written without a PREPARING witness for its token' 'M12: pin written without a durable cleanup intent'
  restore
}
m_sweep_ignores_cleanup_intents() {
  mutate "$REPAIR" 's/if err := sweepPublishedBlockReferenceRepairLivenessCleanups\(database, bucket\); err != nil && firstErr == nil \{/if err := error(nil); err != nil \&\& firstErr == nil {/'
  expect_red 'TestPublishedBlockReferenceRepairSweepProcessesLivenessCleanupIntents' 'orphan intent: removed=0 deleted=0, want 1/1' 'M13: sweep never processes leftover cleanup intents'
  restore
}
m_cleanup_intent_write_failure_ignored() {
  mutate "$REPAIR" 's/\t\treturn fmt\.Errorf\("record repair-owned liveness cleanup intent for fs_object %s: %w", repair\.FSID, err\)\r?\n/\t\tlog.Printf("cleanup intent not recorded, renewing anyway: %v", err)\n/'
  expect_red 'TestRepairPublishedBlockReferenceRepairCleanupIntentWriteFailureWritesNoPub' 'pin written without a PREPARING witness for its token' 'M14: pin written although its cleanup intent was not recorded'
  restore
}
m_positive_settlement_keeps_cleanup_intent() {
  mutate "$REPAIR" 's/\t\t\tif _, err := retirePublishedBlockReferenceRepairLivenessCleanupFn\(database, repair, publishedBlockReferenceRepairNowFn\(\)\.UTC\(\)\); err != nil \{\r?\n\t\t\t\treturn fmt\.Errorf\("retire repair-owned liveness cleanup intent for fs_object %s: %w", repair\.FSID, err\)\r?\n\t\t\t\}\r?\n//'
  expect_red 'TestRepairPublishedBlockReferenceRepairReachableOrderIsRenewClassifyPromoteCleanupDelete' 'visit order =' 'M15: positive settlement leaves its cleanup intent behind'
  restore
}
m_cleanup_authority_read_is_local() {
  mutate "$REPAIR" 's/(FROM published_block_reference_repairs\r?\n\t\tWHERE bucket = \? AND org_id = \? AND repo_id = \? AND commit_id = \? AND fs_id = \?\r?\n\t`, repair\.Bucket, repair\.OrgID, repair\.RepoID, repair\.CommitID, repair\.FSID\)\.\r?\n\t\t)Consistency\(gocql\.EachQuorum\)\./$1Consistency(gocql.LocalQuorum)./'
  expect_red 'TestPublishedBlockReferenceRepairAuthorityReadsAreColdAndExplicit' 'must be read at EachQuorum, never the session LOCAL_QUORUM' 'M16: cleanup absence decided by a LOCAL_QUORUM read'
  restore
}
m_cleanup_intent_has_ttl() {
  mutate "$REPAIR" 's/(INSERT INTO published_repair_liveness_cleanups \(bucket, org_id, repo_id, commit_id, fs_id, producer_token, staged_block_ids, created_at, armed, lease_expires_at\)\r?\n\t\tVALUES \(\?, \?, \?, \?, \?, \?, \?, \?, false, \?\))/$1 USING TTL 3110400/'
  expect_red 'TestPublishedBlockReferenceRepairAuthorityReadsAreColdAndExplicit' 'must not carry a TTL' 'M17: cleanup intent expires on a clock the fan-out does not respect'
  restore
}
m_cleanup_intent_delete_ignores_token() {
  mutate "$REPAIR" 's/(DELETE FROM published_repair_liveness_cleanups\r?\n\t\t)WHERE bucket = \? AND org_id = \? AND repo_id = \? AND commit_id = \? AND fs_id = \? AND producer_token = \?\r?\n(\t\tIF EXISTS\r?\n\t`, repair\.Bucket, repair\.OrgID, repair\.RepoID, repair\.CommitID, repair\.FSID)(, repair\.LivenessToken)\)/$1WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?\n$2)/'
  expect_red 'TestPublishedBlockReferenceRepairAuthorityReadsAreColdAndExplicit' 'per producer token' 'M18: one producer deletes every witness of the identity, including another producer still able to write'
  restore
}
m_cleanup_decides_absence_with_session_read() {
  mutate "$REPAIR" 's/(\tif pending \{\r?\n\t\treturn false, nil\r?\n\t\}\r?\n)(\tloaded, err := loadPublishedBlockReferenceRepairAuthorityFn\(database, repair\)\r?\n)/$1\tif database != nil {\n\t\treturn true, nil\n\t}\n$2/'
  expect_red 'TestPublishedBlockReferenceRepairSweepProcessesLivenessCleanupIntents' 'blind intent: removed=1' 'M19: a local absence removes liveness without the EACH_QUORUM escalation'
  restore
}
m_sweep_ignores_producer_fence() {
  mutate "$REPAIR" 's/\t\t\tif !publishedBlockReferenceRepairLivenessCleanupFreezable\(intent, now\) \{\r?\n\t\t\t\tcontinue\r?\n\t\t\t\}\r?\n/\t\t\tif false \&\& !publishedBlockReferenceRepairLivenessCleanupFreezable(intent, now) {\n\t\t\t\tcontinue\n\t\t\t}\n/'
  expect_red 'TestPublishedBlockReferenceRepairSweepProcessesLivenessCleanupIntents' 'preparing intent under a live lease was frozen' 'M20: the sweep claims a witness whose producer is still inside its lease'
  restore
}
m_fanout_writes_pins_without_lease_timestamp() {
  mutate "$REPAIR" 's/writePublishedBlockReferenceRepairLivenessPinFn\(database, repair\.OrgID, repair\.RepoID, attemptID, blockID, publishedBlockReferenceRepairLeaseTimestamp\(repair\.LivenessLeaseExpiresAt\)\)/writePublishedBlockReferenceRepairLivenessPinFn(database, repair.OrgID, repair.RepoID, attemptID, blockID, publishedBlockReferenceRepairNowFn().UnixMicro())/'
  expect_red 'TestRenewPublishedBlockReferenceRepairLivenessFanOutRenewsLeaseAndTimestampsPins' 'want the lease current at that write' 'M21: pins written at wall-clock time instead of the producer lease, so a late write outlives its cleanup tombstone'
  restore
}
m_visit_skips_arm() {
  mutate "$REPAIR" 's/\tarmed, err := armPublishedBlockReferenceRepairLivenessCleanupFn\(database, \*repair\)\r?\n/\tarmed, err := true, error(nil)\n/'
  expect_red 'TestRepairPublishedBlockReferenceRepairProducerFenceLeaseAndArm' 'want intent < renew < arm < classify' 'M22: the producer never arms its witness (only the lease would ever release it)'
  restore
}
m_fanout_never_renews_lease() {
  mutate "$REPAIR" 's/\t\tif !now\.Add\(2 \* publishedBlockReferenceRepairLivenessLeaseSkew\)\.Before\(repair\.LivenessLeaseExpiresAt\) \{/\t\tif false \&\& !now.Add(2 * publishedBlockReferenceRepairLivenessLeaseSkew).Before(repair.LivenessLeaseExpiresAt) {/'
  expect_red 'TestRenewPublishedBlockReferenceRepairLivenessFanOutRenewsLeaseAndTimestampsPins' 'lease extensions = ' 'M23: a fan-out longer than one lease never renews it (pins would carry a stale timestamp and the sweep could consume the witness mid fan-out)'
  restore
}
m_cleanup_tombstone_ignores_producer_lease() {
  mutate "$REPAIR" 's/\treturn db\.RemovePublishAttemptReferencesAt\(database, repair\.OrgID, publishedBlockReferenceRepairLivenessAttemptID\(repair\), repair\.StagedBlockIDs, publishedBlockReferenceRepairLeaseTimestamp\(repair\.LivenessLeaseExpiresAt\)\)/\treturn cleanupFailedPublishRemoveAttemptReferencesFn(database, repair.OrgID, publishedBlockReferenceRepairLivenessAttemptID(repair), repair.StagedBlockIDs)/'
  expect_red 'TestPublishedBlockReferenceRepairAuthorityReadsAreColdAndExplicit' 'must tombstone at the producer lease timestamp' 'M24: cleanup tombstones at wall-clock time, so a paused producer whose pin lands later revives it'
  restore
}
m_arm_is_unconditional() {
  mutate "$REPAIR" 's/\t\tWHERE bucket = \? AND org_id = \? AND repo_id = \? AND commit_id = \? AND fs_id = \? AND producer_token = \?\r?\n\t\tIF armed = false\r?\n\t`, repair\.StagedBlockIDs/\t\tWHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ? AND producer_token = ?\n\t\tIF EXISTS\n\t`, repair.StagedBlockIDs/'
  expect_red 'TestPublishedBlockReferenceRepairAuthorityReadsAreColdAndExplicit' 'ARM must be a conditional LWT on the PREPARING intent' 'M25: ARM no longer conditioned on the PREPARING state'
  restore
}
m_sweep_consumes_hollow_intent() {
  mutate "$REPAIR" 's/if !gone \{.*\K\t\t\tif len\(db\.NormalizeBlockIDs\(intent\.StagedBlockIDs\)\) == 0 \{/\t\t\tif false \&\& len(db.NormalizeBlockIDs(intent.StagedBlockIDs)) == 0 \{/s'
  expect_red 'TestPublishedBlockReferenceRepairSweepProcessesLivenessCleanupIntents' 'hollow intent: removed=1 deleted=1' 'M26: the sweep deletes an armed witness whose block payload a replica has not received'
  restore
}
m_sweep_consumes_expired_lease_without_freeze() {
  mutate "$REPAIR" 's/\t\t\tfrozen, err := freezePublishedBlockReferenceRepairLivenessCleanupFn\(database, intent\)\r?\n/\t\t\tfrozen, err := true, error(nil)\n/'
  expect_red 'TestPublishedBlockReferenceRepairSweepProcessesLivenessCleanupIntents' 'stale preparing intent: frozen=0' 'M28: an expired lease read from a listing is consumed without the exact-lease freeze (stale snapshot vs a producer that extended L1->L2)'
  restore
}
m_freeze_ignores_observed_lease() {
  mutate "$REPAIR" 's/(\t\tWHERE bucket = \? AND org_id = \? AND repo_id = \? AND commit_id = \? AND fs_id = \? AND producer_token = \?\r?\n\t\t)IF armed = false AND lease_expires_at = \?\r?\n(\t`, intent\.Bucket, intent\.OrgID, intent\.RepoID, intent\.CommitID, intent\.FSID, intent\.LivenessToken, intent\.LivenessLeaseExpiresAt\.UTC\(\)\))/$1IF armed = false\n$2/'
  expect_red 'TestPublishedBlockReferenceRepairAuthorityReadsAreColdAndExplicit' 'the freeze must be a SERIAL LWT on the exact observed lease' 'M28b: the freeze is not conditioned on the exact observed lease, so it can beat an EXTEND that already applied'
  restore
}
m_fenced_producer_keeps_writing() {
  mutate "$REPAIR" 's/\t\t\tif !applied \{\r?\n\t\t\t\treturn errPublishedBlockReferenceRepairLivenessWitnessLost\r?\n\t\t\t\}\r?\n\t\t\trepair\.LivenessLeaseExpiresAt = nextLease\r?\n/\t\t\t_ = applied\n\t\t\trepair.LivenessLeaseExpiresAt = nextLease\n/'
  expect_red 'TestPublishedBlockReferenceRepairFreezeWinsAgainstProducerExtendAndArm' 'pins written after the freeze' 'M29: a producer whose EXTEND lost to the freeze keeps writing pins under a newer lease'
  restore
}
m_fenced_producer_removes_pins_of_pending_row() {
  mutate "$REPAIR" 's/\treturn fmt\.Errorf\("repair-owned liveness producer for fs_object %s was fenced while its repair row is still pending; retain for a fresh producer: %w", repair\.FSID, fenceErr\)\r?\n/\tif err := removePublishedBlockReferenceRepairOwnedPubFn(database, *repair); err != nil {\n\t\treturn errors.Join(fenceErr, err)\n\t}\n\treturn errPublishedBlockReferenceRepairGone\n/'
  expect_red 'TestRepairPublishedBlockReferenceRepairProducerFenceLeaseAndArm' 'a fenced producer must not take its liveness away' 'M29b: a fenced producer of a PENDING row removes the pins its frozen witness still covers'
  restore
}
m_sweep_never_freezes_abandoned_producers_of_pending_rows() {
  mutate "$REPAIR" 's/\t\tvar finished \[\]publishedBlockReferenceRepair\r?\n\t\tfor _, intent := range group \{\r?\n\t\t\tif intent\.LivenessArmed \{\r?\n\t\t\t\tfinished = append\(finished, intent\)\r?\n\t\t\t\tcontinue\r?\n\t\t\t\}\r?\n/\t\tvar finished []publishedBlockReferenceRepair\n\t\tfor _, intent := range group {\n\t\t\tif intent.LivenessArmed {\n\t\t\t\tfinished = append(finished, intent)\n\t\t\t\tcontinue\n\t\t\t}\n\t\t\tif !gone {\n\t\t\t\tcontinue\n\t\t\t}\n/'
  expect_red 'TestPublishedBlockReferenceRepairSweepBoundsAbandonedPreparingProducers' 'want all 11 expired PREPARING producers claimed' 'M30: abandoned PREPARING producers of a pending identity are never claimed and accumulate forever'
  restore
}
m_pending_partial_fence_reintroduced() {
  mutate "$REPAIR" 's{(\t\t\t// durable full-coverage proof exists\.\r?\n)\t\t\tcontinue}{$1\t\t\tif err := removePublishedBlockReferenceRepairOwnedPubFn(database, finished[0]); err != nil {\n\t\t\t\treturn err\n\t\t\t}\n\t\t\tcontinue}'
  expect_red 'TestPublishedBlockReferenceRepairSweepRetainsHigherLeaseFrozenPartialProducer' 'want no physical fence while the identity is pending' 'M39: a pending higher-lease partial witness must not fence a complete lower-lease witness'
  restore
}
m_pending_retention_continue_removed() {
  mutate "$REPAIR" 's{(\t\t\t// durable full-coverage proof exists\.\r?\n)\t\t\tcontinue}{$1\t\t\t// mutation: fall through to the terminal cleanup path}'
  expect_red 'TestPublishedBlockReferenceRepairSweepRetainsArmedPartialProducer' 'want no physical fence while the identity is pending' 'M40: an armed partial witness must remain retained while the identity is pending'
  restore
}
m_intent_insert_is_ordinary() {
  mutate "$REPAIR" 's/\t\tVALUES \(\?, \?, \?, \?, \?, \?, \?, \?, false, \?\)\r?\n\t\tIF NOT EXISTS\r?\n/\t\tVALUES (?, ?, ?, ?, ?, ?, ?, ?, false, ?)\n/'
  expect_red 'TestPublishedBlockReferenceRepairAuthorityReadsAreColdAndExplicit' 'must be an IF NOT EXISTS LWT' 'M31: the cleanup intent INSERT leaves the Paxos state machine its EXTEND/ARM/FREEZE transitions live in'
  restore
}
m_intent_terminal_delete_is_ordinary() {
  mutate "$REPAIR" 's/\t_, err := database\.Session\(\)\.Query\(`\r?\n\t\tDELETE FROM published_repair_liveness_cleanups\r?\n\t\tWHERE bucket = \? AND org_id = \? AND repo_id = \? AND commit_id = \? AND fs_id = \? AND producer_token = \?\r?\n\t\tIF EXISTS\r?\n\t`, repair\.Bucket, repair\.OrgID, repair\.RepoID, repair\.CommitID, repair\.FSID, repair\.LivenessToken\)\.\r?\n\t\tSerialConsistency\(gocql\.Serial\)\.\r?\n\t\tMapScanCAS\(map\[string\]interface\{\}\{\}\)\r?\n\treturn err/\treturn database.Session().Query(`\n\t\tDELETE FROM published_repair_liveness_cleanups\n\t\tWHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ? AND producer_token = ?\n\t`, repair.Bucket, repair.OrgID, repair.RepoID, repair.CommitID, repair.FSID, repair.LivenessToken).Exec()/'
  expect_red 'TestPublishedBlockReferenceRepairAuthorityReadsAreColdAndExplicit' 'terminal cleanup-intent DELETE must be a SERIAL IF EXISTS LWT' 'M32: the terminal cleanup-intent DELETE leaves the Paxos state machine (ordinary DELETE ordered by the coordinator clock)'
  restore
}
m_sweep_deletes_instead_of_retiring() {
  mutate "$REPAIR" 's/\t\t\tif _, err := retirePublishedBlockReferenceRepairLivenessCleanupFn\(database, intent, now\); err != nil \{\r?\n\t\t\t\treport\(intent, fmt\.Errorf\("retire repair-owned liveness cleanup intent: %w", err\)\)\r?\n\t\t\t\}/\t\t\tif err := deletePublishedBlockReferenceRepairLivenessCleanupFn(database, intent); err != nil {\n\t\t\t\treport(intent, fmt.Errorf("delete repair-owned liveness cleanup intent: %w", err))\n\t\t\t}/'
  expect_red 'TestPublishedBlockReferenceRepairSweepProcessesLivenessCleanupIntents' 'must be retired so it keeps re-fencing past gc_grace' 'M33: a consumed witness is deleted outright, so its tombstone is the last fence and gc_grace purges it while the producer may still write'
  restore
}
m_sweep_never_refences_retired_witnesses() {
  mutate "$REPAIR" 's/\t\t\tif !retentionOver && now\.Before\(intent\.LivenessRefencedAt\.Add\(publishedBlockReferenceRepairLivenessRefenceInterval\)\) \{/\t\t\tif !retentionOver {/'
  expect_red 'TestPublishedBlockReferenceRepairSweepRefencesRetiredWitnesses' 'want one re-fence at the producer lease' 'M34: retired witnesses are never re-fenced, so the fence horizon is gc_grace instead of the pin TTL'
  restore
}
m_retired_witness_never_expires() {
  mutate "$REPAIR" 's/\t\t\tretentionOver := !now\.Before\(intent\.LivenessConsumedAt\.Add\(publishedBlockReferenceRepairLivenessConsumedRetention\)\)/\t\t\tretentionOver := false/'
  expect_red 'TestPublishedBlockReferenceRepairSweepRefencesRetiredWitnesses' 'want the final fence at the producer lease and then exactly one delete' 'M34b: retired witnesses are never deleted after the retention (unbounded durable state)'
  restore
}
m_terminal_retention_skips_final_fence() {
  mutate "$REPAIR" 's/(\t\t\tif len\(db\.NormalizeBlockIDs\(intent\.StagedBlockIDs\)\) == 0 \|\| intent\.LivenessLeaseExpiresAt\.IsZero\(\) \{\r?\n\t\t\t\treport\(intent, fmt\.Errorf\("retired cleanup intent has no block payload or lease; cannot re-fence"\)\))/\t\t\tif retentionOver {\n\t\t\t\tif err := deletePublishedBlockReferenceRepairLivenessCleanupFn(database, intent); err != nil {\n\t\t\t\t\treport(intent, err)\n\t\t\t\t}\n\t\t\t\tcontinue\n\t\t\t}\n$1/'
  expect_red 'TestPublishedBlockReferenceRepairSweepRefencesRetiredWitnesses' 'want the fence attempted, the witness retained' 'M35: retention expiry deletes the witness without a final physical fence (a late pin under a purged tombstone is never cleaned)'
  restore
}
m_fence_tombstone_is_local_quorum() {
  mutate "$BLOCK_REFS" 's/(\t`, timestampMicros, orgID, blockID, referrer\)\.Consistency\()PublishAttemptReferenceFenceConsistency(\)\.Exec\(\))/$1BlockReferenceWriteConsistency$2/'
  expect_red_pkg ./internal/db 'TestRemovePublishAttemptReferenceAtBindsTheFenceConsistency' 'must call .Consistency(PublishAttemptReferenceFenceConsistency)' 'M36: the fence tombstone is acknowledged at LOCAL_QUORUM only, so refenced_at advances while another DC may still accept and serve the pin'
  restore
}
m_consumed_refence_ignores_pending_requeue() {
  mutate "$REPAIR" 's/\t\treturn identity \+ ":" \+ token/\t\treturn identity/'
  expect_red 'TestPublishedBlockReferenceRepairConsumedWitnessIsolatedFromPendingRequeue' 'different producer tokens must produce different physical pub identities' 'M37: producer-token identity collapsed to the shared pub referrer, so an old producer fence removed a requeued producer pin'
  restore
}
m_timeout_drops_partial_progress() {
  mutate "$REPAIR" 's/if nextUnreadCommitID != "" && nextUnreadCommitID != startCommitID \{\n\t\twalk.NextCursor = nextUnreadCommitID\n\t\}//'
  expect_red 'TestWalkPublishedCommitReachabilityTimeoutAdvancesToNextUnread' 'want next unread' 'timeout drops partial ancestry progress'
  restore
}
m_missing_row_is_reachable() {
  mutate "$REPAIR" 's/return publishedBlockReferenceRepairCommitNoLongerPending, errPublishedBlockReferenceRepairGone/return publishedBlockReferenceRepairCommitReachable, nil/g'
  expect_red 'TestClassifyPublishedBlockReferenceRepairCASMissOnGoneRowIsNotReachable' 'gone row was treated as REACHABLE' 'missing repair row as REACHABLE'
  restore
}
m_genesis_does_not_reanchor() {
  mutate "$REPAIR" 's/func publishedBlockReferenceRepairWalkExhaustedToGenesis\(progress publishedCommitReachabilityWalk, err error\) bool \{\n\treturn err == nil &&\n\t\tprogress.Outcome == publishedBlockReferenceRepairCommitUnknown &&\n\t\tstrings.TrimSpace\(progress.NextCursor\) == ""\n\}/func publishedBlockReferenceRepairWalkExhaustedToGenesis(progress publishedCommitReachabilityWalk, err error) bool {\n\treturn false\n}/'
  expect_red 'TestClassifyPublishedBlockReferenceRepairResumablePreHEADAnchorCanReanchorAfterPublish' 'want REACHABLE after re-anchor' 'pre-HEAD genesis does not re-anchor to a later HEAD'
  restore
}
m_genesis_exhaustion_not_durable() {
  mutate "$REPAIR" 's/if publishedBlockReferenceRepairWalkExhaustedToGenesis\(progress, err\) \{\n\t\tif persistErr := persistPublishedBlockReferenceRepairGenesisExhaustion\(database, repair\); persistErr != nil \{\n\t\t\tif errors.Is\(persistErr, errPublishedBlockReferenceRepairGone\) \{\n\t\t\t\treturn publishedBlockReferenceRepairGoneClassification\(\)\n\t\t\t\}\n\t\t\treturn publishedBlockReferenceRepairCommitUnknown, persistErr\n\t\t\}\n\t\treturn reanchorPublishedBlockReferenceRepairAfterCleanGenesis\(ctx, database, repair, headObservationBudget\)\n\t\}/if publishedBlockReferenceRepairWalkExhaustedToGenesis(progress, err) {\n\t\treturn reanchorPublishedBlockReferenceRepairAfterCleanGenesis(ctx, database, repair, headObservationBudget)\n\t}/'
  expect_red 'TestClassifyPublishedBlockReferenceRepairResumableGenesisExhaustionSurvivesHEADDeadline' 'clean genesis must persist exhausted progress before the HEAD re-read' 'clean genesis exhaustion is not durable before HEAD re-read'
  restore
}
m_repair_liveness_uses_commit_id() {
  mutate "$REPAIR" 's/publishedBlockReferenceRepairLivenessAttemptID\(\*?repair\)/repair.CommitID/g'
  expect_red 'TestPublishedBlockReferenceRepairLivenessIdentityIsPerRepairRow$' 'renewal must use the per-repair pub identity' 'repair liveness reuses commit-scoped pub identity'
  restore
}
m_progress_cas_ignores_generation() {
  mutate "$REPAIR" 's/IF created_at = \?/IF created_at != null/g'
  expect_red 'TestPublishedBlockReferenceRepairProgressUsesMonotonicCAS$' 'must bind the loaded created_at generation' 'progress LWT ignores loaded created_at generation'
  restore
}
m_reanchor_loser_replays_exhausted() {
  mutate "$REPAIR" 's/(budget still allows another SERIAL HEAD observation\.\n\t\t\t)continue\n/$1break\n/'
  expect_red 'TestReanchorPublishedBlockReferenceRepairDoesNotReplayExhaustedLoserSnapshot' 're-anchor loser replayed exhausted snapshot' 're-anchor CAS loser replays an exhausted snapshot'
  restore
}

m_residue_reaper_removed() {
  mutate "$REPAIR" 's/if publishedBlockReferenceRepairIsProgressOnly\(repair\) \{/if false \&\& publishedBlockReferenceRepairIsProgressOnly(repair) {/'
  expect_red 'TestRunPublishedBlockReferenceRepairSweepReapsProgressOnlyResidue' 'progress-only residue was not reaped exactly once' 'sweep lists progress-only residue forever'
  restore
}
m_residue_reaper_unconditional() {
  mutate "$REPAIR" 's/\t\tIF created_at = null AND lease_expires_at = null\n//'
  expect_red 'TestReapPublishedBlockReferenceRepairProgressOnlyRowIsConditionalAndSerial' 'residue reaper must be conditioned on the ordinary queue cells' 'residue reaper shadows a concurrent requeue'
  restore
}
m_residue_reaper_deletes_whole_row() {
  mutate "$REPAIR" 's/DELETE reachability_anchor_head_commit_id, reachability_cursor_commit_id, reachability_anchor_exhausted\n\t\tFROM published_block_reference_repairs/DELETE FROM published_block_reference_repairs/'
  expect_red 'TestReapPublishedBlockReferenceRepairProgressOnlyRowIsConditionalAndSerial' 'must never delete the whole repair row' 'residue reaper tombstones the whole row and can shadow an ordinary requeue'
  restore
}
m_hydrate_trusts_listed_cells_on_residue() {
  mutate "$REPAIR" 's/\tif publishedBlockReferenceRepairIsProgressOnly\(loaded\) \{\n\t\treturn publishedBlockReferenceRepair\{\}, errPublishedBlockReferenceRepairGone\n\t\}\n//'
  expect_red 'TestRepairPublishedBlockReferenceRepairListedLiveThenLoadedResidueIsNoOp' 'progress-only residue' 'listed-live repair keeps acting after it became residue'
  restore
}
m_reanchor_head_budget_unbounded() {
  mutate "$REPAIR" 's/if headObservationBudget <= 0 \{/if false {/'
  expect_red 'TestReanchorPublishedBlockReferenceRepairHeadObservationsAreBudgeted' 'want exactly the per-visit budget' 're-anchor loser re-reads SERIAL HEAD without bound'
  restore
}
m_resume_forgets_anchor_seed() {
  mutate "$REPAIR" 's/publishedBlockReferenceRepairWalkSeeds\(\*repair\)/nil/g'
  expect_red 'TestClassifyPublishedBlockReferenceRepairResumableDetectsCycleThroughAnchoredHEAD' 'want UNKNOWN cycle error' 'resumed chunk rotates through a cycle across chunks'
  restore
}

m_liveness_producer_budget_removed() {
  mutate "$REPAIR" 's/\tif repair\.LivenessProducerCount >= publishedBlockReferenceRepairMaxLivenessProducers \{\r?\n\t\treturn false, errPublishedBlockReferenceRepairLivenessProducerBudgetExhausted\r?\n\t\}/\tif false \&\& repair.LivenessProducerCount >= publishedBlockReferenceRepairMaxLivenessProducers {\n\t\treturn false, errPublishedBlockReferenceRepairLivenessProducerBudgetExhausted\n\t}/'
  expect_red 'TestRenewPublishedBlockReferenceRepairLivenessProducerBudgetExhaustionRetainsWithoutNewIntent' 'want producer-budget exhaustion' 'M41: durable producer budget removed'
  restore
}
MUTATIONS=(m_lease_expiry_cleans_unknown m_unrelated_head_is_declared_not_published m_repair_row_deleted_before_settlement m_head_read_is_weak m_parent_read_is_local_only m_reachability_ignores_ancestry m_ancestry_limit_becomes_negative m_parent_error_becomes_negative m_ancestry_skips_parent m_hot_path_pays_serial_per_block m_cleanup_uses_the_wrong_attempt_identity m_settlement_delete_is_conditional m_settlement_insert_uses_serial_consistency m_retry_backoff_removes_process_local_state m_retry_hint_prune_is_missing m_retry_reanchors_to_live_head m_root_becomes_negative m_insert_writes_cursor_columns m_pre_classify_renewal_removed m_renewal_moved_below_classifier m_classify_continues_after_renewal_error m_renewal_skips_still_pending_before_write m_renewal_skips_still_pending_after_write m_renewal_compensation_removed m_renewal_compensation_uses_commit_identity m_unknown_renews_twice_per_visit m_post_classify_compensation_removed m_partial_renewal_failure_skips_compensation m_reachable_settlement_failure_skips_gone_check m_cleanup_intent_not_written_before_pub m_sweep_ignores_cleanup_intents m_cleanup_intent_write_failure_ignored m_positive_settlement_keeps_cleanup_intent m_cleanup_authority_read_is_local m_cleanup_intent_has_ttl m_cleanup_intent_delete_ignores_token m_cleanup_decides_absence_with_session_read m_sweep_ignores_producer_fence m_fanout_writes_pins_without_lease_timestamp m_visit_skips_arm m_fanout_never_renews_lease m_cleanup_tombstone_ignores_producer_lease m_arm_is_unconditional m_sweep_consumes_hollow_intent m_sweep_consumes_expired_lease_without_freeze m_freeze_ignores_observed_lease m_fenced_producer_keeps_writing m_fenced_producer_removes_pins_of_pending_row m_sweep_never_freezes_abandoned_producers_of_pending_rows m_pending_partial_fence_reintroduced m_pending_retention_continue_removed m_intent_insert_is_ordinary m_intent_terminal_delete_is_ordinary m_sweep_deletes_instead_of_retiring m_sweep_never_refences_retired_witnesses m_retired_witness_never_expires m_terminal_retention_skips_final_fence m_fence_tombstone_is_local_quorum m_consumed_refence_ignores_pending_requeue m_timeout_drops_partial_progress m_missing_row_is_reachable m_genesis_does_not_reanchor m_genesis_exhaustion_not_durable m_repair_liveness_uses_commit_id m_progress_cas_ignores_generation m_reanchor_loser_replays_exhausted m_residue_reaper_removed m_residue_reaper_unconditional m_residue_reaper_deletes_whole_row m_hydrate_trusts_listed_cells_on_residue m_reanchor_head_budget_unbounded m_resume_forgets_anchor_seed m_liveness_producer_budget_removed)
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
