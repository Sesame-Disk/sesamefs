//go:build integration

package v2

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/google/uuid"
)

// SettlePublishedBlockReferenceRepairForIntegration invokes the production
// settlement path with an explicitly selected classifier outcome. It lets the
// evidence suite model an ambiguous CAS confirmation result without replacing
// a process-wide classifier that a live repair worker may call concurrently.
func SettlePublishedBlockReferenceRepairForIntegration(database *db.DB, orgID, repoID, commitID, fsID string, stagedBlockIDs []string, outcome string, injectedErr error) error {
	var outcomeValue publishedBlockReferenceRepairCommitOutcome
	switch strings.ToLower(strings.TrimSpace(outcome)) {
	case "reachable":
		outcomeValue = publishedBlockReferenceRepairCommitReachable
	case "unknown":
		outcomeValue = publishedBlockReferenceRepairCommitUnknown
	case "definitely_not_reachable":
		outcomeValue = publishedBlockReferenceRepairCommitDefinitelyNotReachable
	default:
		return fmt.Errorf("unknown integration repair outcome %q", outcome)
	}
	return settlePublishedBlockReferenceRepair(database, newPublishedBlockReferenceRepair(orgID, repoID, commitID, fsID, stagedBlockIDs), outcomeValue, injectedErr)
}

// PublishedBlockReferenceRepairCommitOutcomeForIntegration runs the production
// cold-path classifier without exposing its internal outcome type to integration
// packages. It is used by the standalone 3-DC evidence leg.
func PublishedBlockReferenceRepairCommitOutcomeForIntegration(database *db.DB, orgID, repoID, commitID string) (string, error) {
	outcome, err := publishedBlockReferenceRepairCommitReachableFn(database, orgID, repoID, commitID)
	switch outcome {
	case publishedBlockReferenceRepairCommitReachable:
		return "reachable", err
	case publishedBlockReferenceRepairCommitDefinitelyNotReachable:
		return "definitely_not_reachable", err
	case publishedBlockReferenceRepairCommitNoLongerPending:
		return "no_longer_pending", err
	default:
		return "unknown", err
	}
}

// PublishedBlockReferenceRepairProgressForIntegration returns the durable
// resumable-walk snapshot. It is used by the R31 convergence evidence to prove
// retries continue from the persisted cursor rather than a later live HEAD.
func PublishedBlockReferenceRepairProgressForIntegration(database *db.DB, orgID, repoID, commitID, fsID string) (anchorHeadCommitID, cursorCommitID string, err error) {
	repair := newPublishedBlockReferenceRepair(orgID, repoID, commitID, fsID, nil)
	loaded, err := loadPublishedBlockReferenceRepairFn(database, repair)
	if err != nil {
		return "", "", err
	}
	return loaded.ReachabilityAnchorHeadCommitID, loaded.ReachabilityCursorCommitID, nil
}

func PublishedCommitReachabilityMaxNodesForIntegration() int {
	return publishedCommitReachabilityMaxNodes
}

// PublishedBlockReferenceRepairLivenessReferrerForIntegration is the pub:
// identity owned by one repair row. It is not pub:<commitID>.
func PublishedBlockReferenceRepairLivenessReferrerForIntegration(repoID, commitID, fsID string) string {
	return db.BlockReferrerForPublishAttempt(publishedBlockReferenceRepairLivenessAttemptID(publishedBlockReferenceRepair{
		RepoID:   repoID,
		CommitID: commitID,
		FSID:     fsID,
	}))
}

// ClassifyPublishedBlockReferenceRepairResumableForIntegration runs the
// production resumable classifier (SERIAL anchor + cursor walk) without
// settling. 3-DC evidence uses this so a missing dummy fs_object cannot
// masquerade as a reachability failure.
func ClassifyPublishedBlockReferenceRepairResumableForIntegration(database *db.DB, orgID, repoID, commitID, fsID string) (string, error) {
	repair := newPublishedBlockReferenceRepair(orgID, repoID, commitID, fsID, nil)
	hydrated, err := hydratePublishedBlockReferenceRepair(database, repair)
	if errors.Is(err, errPublishedBlockReferenceRepairGone) {
		return "no_longer_pending", nil
	}
	if err != nil {
		return "unknown", err
	}
	outcome, err := classifyPublishedBlockReferenceRepairCommitResumable(database, &hydrated)
	switch outcome {
	case publishedBlockReferenceRepairCommitReachable:
		return "reachable", err
	case publishedBlockReferenceRepairCommitDefinitelyNotReachable:
		return "definitely_not_reachable", err
	case publishedBlockReferenceRepairCommitNoLongerPending:
		return "no_longer_pending", err
	default:
		return "unknown", err
	}
}

// RunPublishedBlockReferenceRepairSweepForIntegration runs one production
// discovery sweep synchronously. The evidence suite uses it to prove the
// progress-only residue reaper against real Cassandra LWT semantics.
func RunPublishedBlockReferenceRepairSweepForIntegration(database *db.DB) error {
	return runPublishedBlockReferenceRepairSweep(database)
}

// ReapPublishedBlockReferenceRepairProgressOnlyRowForIntegration runs the
// conditional residue delete against one explicit repair identity and reports
// whether the LWT applied. A queued row must make it not apply.
func ReapPublishedBlockReferenceRepairProgressOnlyRowForIntegration(database *db.DB, orgID, repoID, commitID, fsID string) (bool, error) {
	return reapPublishedBlockReferenceRepairProgressOnlyRowFn(database, newPublishedBlockReferenceRepair(orgID, repoID, commitID, fsID, nil))
}

// PublishedBlockReferenceRepairBucketForIntegration exposes the discovery
// bucket so evidence can seed a residue row at the exact primary key the
// sweep lists.
func PublishedBlockReferenceRepairBucketForIntegration(orgID, repoID, commitID, fsID string) int {
	return newPublishedBlockReferenceRepair(orgID, repoID, commitID, fsID, nil).Bucket
}

// RepairPublishedFSObjectBlockReferenceRepairGatedForIntegration runs one
// production repair visit whose classifier is held at beforeClassify for this
// identity only. Evidence uses it to observe Cassandra while the bounded
// ancestry walk has not started yet and prove the repair-owned
// pub:<repo:commit:fsID> is already visible
// (ISSUE-PUBLISH-REPAIR-RENEWAL-AFTER-CLASSIFY-01). The process-wide
// classifier variable is not swapped, so a live worker in the same process
// is unaffected.
func RepairPublishedFSObjectBlockReferenceRepairGatedForIntegration(database *db.DB, orgID, repoID, commitID, fsID string, stagedBlockIDs []string, beforeClassify func()) error {
	gated := func(database *db.DB, repair *publishedBlockReferenceRepair) (publishedBlockReferenceRepairCommitOutcome, error) {
		if beforeClassify != nil {
			beforeClassify()
		}
		return publishedBlockReferenceRepairClassifyFn(database, repair)
	}
	return repairPublishedBlockReferenceRepairWithClassifier(database, newPublishedBlockReferenceRepair(orgID, repoID, commitID, fsID, stagedBlockIDs), gated)
}

// RecordPublishedBlockReferenceRepairLivenessCleanupForIntegration writes the
// write-ahead cleanup intent through the production primitive. Evidence seeds
// the exact durable state a visit leaves behind when its process is lost
// between the pub: write and a successful compensation.
func RecordPublishedBlockReferenceRepairLivenessCleanupForIntegration(database *db.DB, orgID, repoID, commitID, fsID string, stagedBlockIDs []string) error {
	repair := newPublishedBlockReferenceRepair(orgID, repoID, commitID, fsID, stagedBlockIDs)
	repair.LivenessToken = uuid.NewString()
	repair.LivenessLeaseExpiresAt = publishedBlockReferenceRepairLeaseInstant(publishedBlockReferenceRepairNowFn().Add(publishedBlockReferenceRepairLivenessLease))
	if err := insertPublishedBlockReferenceRepairLivenessCleanupFn(database, repair); err != nil {
		return err
	}
	armed, err := armPublishedBlockReferenceRepairLivenessCleanupFn(database, repair)
	if err != nil {
		return err
	}
	if !armed {
		return fmt.Errorf("seeded cleanup intent could not be armed")
	}
	return nil
}

// RecordPreparingPublishedBlockReferenceRepairLivenessCleanupForIntegration
// writes a cleanup intent that is still PREPARING with the given lease: the
// state a producer leaves while its pin fan-out is in flight. It returns the
// producer token so evidence can later drive that producer's EXTEND and ARM
// against a sweeper. Evidence uses it to prove the sweep does not consume
// such an intent before the lease, consumes it once it has won the
// exact-lease freeze, and never consumes it from a stale observation.
func RecordPreparingPublishedBlockReferenceRepairLivenessCleanupForIntegration(database *db.DB, orgID, repoID, commitID, fsID string, stagedBlockIDs []string, leaseExpiresAt time.Time) (string, error) {
	repair := newPublishedBlockReferenceRepair(orgID, repoID, commitID, fsID, stagedBlockIDs)
	repair.LivenessToken = uuid.NewString()
	repair.LivenessLeaseExpiresAt = publishedBlockReferenceRepairLeaseInstant(leaseExpiresAt)
	if err := insertPublishedBlockReferenceRepairLivenessCleanupFn(database, repair); err != nil {
		return "", err
	}
	return repair.LivenessToken, nil
}

func publishedBlockReferenceRepairProducerForIntegration(orgID, repoID, commitID, fsID string, stagedBlockIDs []string, token string, lease time.Time) publishedBlockReferenceRepair {
	repair := newPublishedBlockReferenceRepair(orgID, repoID, commitID, fsID, stagedBlockIDs)
	repair.LivenessToken = token
	repair.LivenessLeaseExpiresAt = publishedBlockReferenceRepairLeaseInstant(lease)
	return repair
}

// ExtendPublishedBlockReferenceRepairLivenessCleanupForIntegration is the
// producer's exact-lease EXTEND (observedLease -> nextLease) through the
// production LWT, for evidence that races it against a sweeper's freeze.
func ExtendPublishedBlockReferenceRepairLivenessCleanupForIntegration(database *db.DB, orgID, repoID, commitID, fsID string, stagedBlockIDs []string, token string, observedLease, nextLease time.Time) (bool, error) {
	repair := publishedBlockReferenceRepairProducerForIntegration(orgID, repoID, commitID, fsID, stagedBlockIDs, token, observedLease)
	return extendPublishedBlockReferenceRepairLivenessCleanupFn(database, repair, publishedBlockReferenceRepairLeaseInstant(nextLease))
}

// ArmPublishedBlockReferenceRepairLivenessCleanupForIntegration is the
// producer's conditional ARM through the production LWT.
func ArmPublishedBlockReferenceRepairLivenessCleanupForIntegration(database *db.DB, orgID, repoID, commitID, fsID string, stagedBlockIDs []string, token string, lease time.Time) (bool, error) {
	repair := publishedBlockReferenceRepairProducerForIntegration(orgID, repoID, commitID, fsID, stagedBlockIDs, token, lease)
	return armPublishedBlockReferenceRepairLivenessCleanupFn(database, repair)
}

// WritePublishedBlockReferenceRepairLivenessPinForIntegration writes one pin
// exactly as the production fan-out does: USING TIMESTAMP = the producer's
// lease.
func WritePublishedBlockReferenceRepairLivenessPinForIntegration(database *db.DB, orgID, repoID, commitID, fsID, blockID string, lease time.Time) error {
	repair := publishedBlockReferenceRepair{RepoID: repoID, CommitID: commitID, FSID: fsID}
	return writePublishedBlockReferenceRepairLivenessPinFn(database, orgID, repoID, publishedBlockReferenceRepairLivenessAttemptID(repair), blockID, publishedBlockReferenceRepairLeaseTimestamp(publishedBlockReferenceRepairLeaseInstant(lease)))
}

// PublishedBlockReferenceRepairLivenessCleanupStateForIntegration returns the
// durable state of one producer's intent (present, armed, lease) as the
// bucket listing reports it.
func PublishedBlockReferenceRepairLivenessCleanupStateForIntegration(database *db.DB, orgID, repoID, commitID, fsID, token string) (present, armed bool, lease time.Time, err error) {
	repair := newPublishedBlockReferenceRepair(orgID, repoID, commitID, fsID, nil)
	intents, err := listPublishedBlockReferenceRepairLivenessCleanupsForBucketFn(database, repair.Bucket)
	if err != nil {
		return false, false, time.Time{}, err
	}
	for _, intent := range intents {
		if intent.OrgID == repair.OrgID && intent.RepoID == repair.RepoID && intent.CommitID == repair.CommitID && intent.FSID == repair.FSID && intent.LivenessToken == token {
			return true, intent.LivenessArmed, intent.LivenessLeaseExpiresAt, nil
		}
	}
	return false, false, time.Time{}, nil
}

// SweepPublishedBlockReferenceRepairLivenessCleanupsGatedAtForIntegration
// runs the production cleanup-intent sweep for one identity's bucket with the
// worker clock pinned to now and a hold between the listing and the
// decisions, so evidence can make the listing stale (the producer extends
// after the sweeper observed it) and prove the freeze refuses that stale
// authority.
func SweepPublishedBlockReferenceRepairLivenessCleanupsGatedAtForIntegration(database *db.DB, orgID, repoID, commitID, fsID string, now time.Time, afterList func()) error {
	previous := publishedBlockReferenceRepairNowFn
	publishedBlockReferenceRepairNowFn = func() time.Time { return now }
	defer func() { publishedBlockReferenceRepairNowFn = previous }()
	return sweepPublishedBlockReferenceRepairLivenessCleanupsGated(database, newPublishedBlockReferenceRepair(orgID, repoID, commitID, fsID, nil).Bucket, afterList)
}

// PublishedBlockReferenceRepairLivenessCleanupExistsForIntegration reports
// whether the cleanup intent row for one repair identity is present.
func PublishedBlockReferenceRepairLivenessCleanupExistsForIntegration(database *db.DB, orgID, repoID, commitID, fsID string) (bool, error) {
	repair := newPublishedBlockReferenceRepair(orgID, repoID, commitID, fsID, nil)
	intents, err := listPublishedBlockReferenceRepairLivenessCleanupsForBucketFn(database, repair.Bucket)
	if err != nil {
		return false, err
	}
	for _, intent := range intents {
		if intent.OrgID == repair.OrgID && intent.RepoID == repair.RepoID && intent.CommitID == repair.CommitID && intent.FSID == repair.FSID {
			return true, nil
		}
	}
	return false, nil
}

// SweepPublishedBlockReferenceRepairLivenessCleanupsForIntegration runs the
// production cleanup-intent sweep for the bucket of one identity only. The
// 3-DC evidence uses it so the blind-DC decision under test is the intent
// sweep and not the unrelated repair rows other legs left in the fixture.
func SweepPublishedBlockReferenceRepairLivenessCleanupsForIntegration(database *db.DB, orgID, repoID, commitID, fsID string) error {
	return sweepPublishedBlockReferenceRepairLivenessCleanups(database, newPublishedBlockReferenceRepair(orgID, repoID, commitID, fsID, nil).Bucket)
}

// PublishedBlockReferenceRepairLivenessAttemptIDForIntegration is the
// pub:<attempt> id (not the referrer) one repair row owns, for evidence that
// drives the timestamped removal primitive directly.
func PublishedBlockReferenceRepairLivenessAttemptIDForIntegration(repoID, commitID, fsID string) string {
	return publishedBlockReferenceRepairLivenessAttemptID(publishedBlockReferenceRepair{RepoID: repoID, CommitID: commitID, FSID: fsID})
}

// PublishedBlockReferenceRepairLeaseTimestampForIntegration is the write /
// tombstone timestamp a producer with the given lease uses.
func PublishedBlockReferenceRepairLeaseTimestampForIntegration(lease time.Time) int64 {
	return publishedBlockReferenceRepairLeaseTimestamp(lease)
}

// RunPublishedBlockReferenceRepairSweepAtForIntegration runs one production
// discovery sweep with the worker clock pinned to now, so evidence can make a
// producer lease expire without waiting for it.
func RunPublishedBlockReferenceRepairSweepAtForIntegration(database *db.DB, now time.Time) error {
	previous := publishedBlockReferenceRepairNowFn
	publishedBlockReferenceRepairNowFn = func() time.Time { return now }
	defer func() { publishedBlockReferenceRepairNowFn = previous }()
	return runPublishedBlockReferenceRepairSweep(database)
}
