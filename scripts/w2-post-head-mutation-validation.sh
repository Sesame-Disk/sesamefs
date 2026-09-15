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
  mutate "$REPAIR" 's/Outcome:\s+publishedBlockReferenceRepairCommitUnknown,\n\s+NextCursor: currentCommitID/Outcome:    publishedBlockReferenceRepairCommitDefinitelyNotReachable,\n\t\tNextCursor: currentCommitID/'
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
  mutate "$REPAIR" 's/\t\t\tif err := deletePublishedBlockReferenceRepairLivenessCleanupFn\(database, repair\); err != nil \{\r?\n\t\t\t\treturn fmt\.Errorf\("delete repair-owned liveness cleanup intent for fs_object %s: %w", repair\.FSID, err\)\r?\n\t\t\t\}\r?\n//'
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
  mutate "$REPAIR" 's/(DELETE FROM published_repair_liveness_cleanups\r?\n\t\t)WHERE bucket = \? AND org_id = \? AND repo_id = \? AND commit_id = \? AND fs_id = \? AND producer_token = \?\r?\n\t`, repair\.Bucket, repair\.OrgID, repair\.RepoID, repair\.CommitID, repair\.FSID, repair\.LivenessToken\)\.Exec\(\)/$1WHERE bucket = ? AND org_id = ? AND repo_id = ? AND commit_id = ? AND fs_id = ?\n\t`, repair.Bucket, repair.OrgID, repair.RepoID, repair.CommitID, repair.FSID).Exec()/'
  expect_red 'TestPublishedBlockReferenceRepairAuthorityReadsAreColdAndExplicit' 'per producer token' 'M18: one producer deletes every witness of the identity, including another producer still able to write'
  restore
}
m_cleanup_decides_absence_with_session_read() {
  mutate "$REPAIR" 's/(\tif pending \{\r?\n\t\treturn false, nil\r?\n\t\}\r?\n)(\tloaded, err := loadPublishedBlockReferenceRepairAuthorityFn\(database, repair\)\r?\n)/$1\tif database != nil {\n\t\treturn true, nil\n\t}\n$2/'
  expect_red 'TestPublishedBlockReferenceRepairSweepProcessesLivenessCleanupIntents' 'blind intent: removed=1' 'M19: a local absence removes liveness without the EACH_QUORUM escalation'
  restore
}
m_sweep_ignores_producer_fence() {
  mutate "$REPAIR" 's/\t\t\tif !publishedBlockReferenceRepairLivenessCleanupConsumable\(intent, now\) \{\r?\n\t\t\t\tcontinue\r?\n\t\t\t\}\r?\n/\t\t\tif false \&\& !publishedBlockReferenceRepairLivenessCleanupConsumable(intent, now) {\n\t\t\t\tcontinue\n\t\t\t}\n/'
  expect_red 'TestPublishedBlockReferenceRepairSweepProcessesLivenessCleanupIntents' 'its producer may still be writing pins under a live lease' 'M20: the sweep consumes a witness whose producer may still write pins'
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
  mutate "$REPAIR" 's/\t\t\tif len\(db\.NormalizeBlockIDs\(intent\.StagedBlockIDs\)\) == 0 \{/\t\t\tif false \&\& len(db.NormalizeBlockIDs(intent.StagedBlockIDs)) == 0 {/'
  expect_red 'TestPublishedBlockReferenceRepairSweepProcessesLivenessCleanupIntents' 'hollow intent: removed=1 deleted=1' 'M26: the sweep deletes an armed witness whose block payload a replica has not received'
  restore
}
m_sweep_never_compacts_finished_producers() {
  mutate "$REPAIR" 's/\t\t\t\tif !intent\.LivenessArmed \|\| intent\.LivenessToken == keep\.LivenessToken \{/\t\t\t\tif true || !intent.LivenessArmed || intent.LivenessToken == keep.LivenessToken {/'
  expect_red 'TestPublishedBlockReferenceRepairSweepProcessesLivenessCleanupIntents' 'compaction deleted the older finished producer 0 times' 'M27: finished producers of a pending identity accumulate one witness per visit forever'
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

MUTATIONS=(m_lease_expiry_cleans_unknown m_unrelated_head_is_declared_not_published m_repair_row_deleted_before_settlement m_head_read_is_weak m_parent_read_is_local_only m_reachability_ignores_ancestry m_ancestry_limit_becomes_negative m_parent_error_becomes_negative m_ancestry_skips_parent m_hot_path_pays_serial_per_block m_cleanup_uses_the_wrong_attempt_identity m_settlement_delete_is_conditional m_settlement_insert_uses_serial_consistency m_retry_backoff_removes_process_local_state m_retry_hint_prune_is_missing m_retry_reanchors_to_live_head m_root_becomes_negative m_insert_writes_cursor_columns m_pre_classify_renewal_removed m_renewal_moved_below_classifier m_classify_continues_after_renewal_error m_renewal_skips_still_pending_before_write m_renewal_skips_still_pending_after_write m_renewal_compensation_removed m_renewal_compensation_uses_commit_identity m_unknown_renews_twice_per_visit m_post_classify_compensation_removed m_partial_renewal_failure_skips_compensation m_reachable_settlement_failure_skips_gone_check m_cleanup_intent_not_written_before_pub m_sweep_ignores_cleanup_intents m_cleanup_intent_write_failure_ignored m_positive_settlement_keeps_cleanup_intent m_cleanup_authority_read_is_local m_cleanup_intent_has_ttl m_cleanup_intent_delete_ignores_token m_cleanup_decides_absence_with_session_read m_sweep_ignores_producer_fence m_fanout_writes_pins_without_lease_timestamp m_visit_skips_arm m_fanout_never_renews_lease m_cleanup_tombstone_ignores_producer_lease m_arm_is_unconditional m_sweep_consumes_hollow_intent m_sweep_never_compacts_finished_producers m_timeout_drops_partial_progress m_missing_row_is_reachable m_genesis_does_not_reanchor m_genesis_exhaustion_not_durable m_repair_liveness_uses_commit_id m_progress_cas_ignores_generation m_reanchor_loser_replays_exhausted m_residue_reaper_removed m_residue_reaper_unconditional m_residue_reaper_deletes_whole_row m_hydrate_trusts_listed_cells_on_residue m_reanchor_head_budget_unbounded m_resume_forgets_anchor_seed)
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
