//go:build integration

package v2

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Sesame-Disk/sesamefs/internal/db"
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
