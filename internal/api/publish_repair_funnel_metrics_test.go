package api

import (
	"errors"
	"testing"

	v2 "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func reconciliationFailures(funnel string) float64 {
	return testutil.ToFloat64(metrics.PublishRepairPostHeadReconciliationFailuresTotal.WithLabelValues(funnel))
}

// Sync hands one repair key per fs_object to the scheduler, but the
// publication-level event is counted once per funnel invocation: one commit
// with three fs_objects is one event and three schedules. (This is the site
// where the original xN-per-fs_object bug lived.)
func TestScheduleSyncCommitBlockReferenceRepairsCountsOneEventPerInvocation(t *testing.T) {
	oldSchedule := publishRepairScheduleFn
	t.Cleanup(func() { publishRepairScheduleFn = oldSchedule })
	scheduled := 0
	publishRepairScheduleFn = func(database *db.DB, orgID, repoID, commitID, fsID, label string, stagedBlockIDs []string) {
		scheduled++
	}
	label := "SyncFunnelMetricsTest"
	before := reconciliationFailures(label)
	scheduleSyncCommitBlockReferenceRepairs(nil, "org-1", "repo-1", "commit-1", map[string][]string{
		"fs-a": {"b1"}, "fs-b": {"b2"}, "fs-c": {"b3"},
	}, label)
	if scheduled != 3 {
		t.Fatalf("scheduler calls = %d, want 3 (one per fs_object)", scheduled)
	}
	if got := reconciliationFailures(label) - before; got != 1 {
		t.Fatalf("post_head_reconciliation_failures{%s} delta = %v, want 1 for one invocation with three fs_objects", label, got)
	}
	// A retry of the same commit is another invocation and another event:
	// the counter is defined per handoff event, not per distinct publication.
	scheduleSyncCommitBlockReferenceRepairs(nil, "org-1", "repo-1", "commit-1", map[string][]string{"fs-a": {"b1"}}, label)
	if got := reconciliationFailures(label) - before; got != 2 {
		t.Fatalf("post_head_reconciliation_failures{%s} delta after a retry = %v, want 2 (events, not distinct publications)", label, got)
	}
}

// SeafHTTP counts the event exactly when its request-local promotion fails
// and schedules the repair once; a successful promotion counts nothing.
func TestFinalizeSeafHTTPPublishedBlockReferencesCountsReconciliationFailure(t *testing.T) {
	oldPromote := promoteSeafHTTPPublishAttemptReferencesFn
	oldSchedule := schedulePublishedFSObjectBlockReferenceRepairFn
	oldClear := clearPublishedFSObjectBlockReferenceRepairFn
	t.Cleanup(func() {
		promoteSeafHTTPPublishAttemptReferencesFn = oldPromote
		schedulePublishedFSObjectBlockReferenceRepairFn = oldSchedule
		clearPublishedFSObjectBlockReferenceRepairFn = oldClear
	})
	scheduled := 0
	schedulePublishedFSObjectBlockReferenceRepairFn = func(database *db.DB, orgID, repoID, commitID, fsID, label string, stagedBlockIDs []string) {
		scheduled++
	}
	clearPublishedFSObjectBlockReferenceRepairFn = func(database *db.DB, orgID, repoID, commitID, fsID string) error { return nil }
	label := "SeafHTTPFunnelMetricsTest"
	before := reconciliationFailures(label)

	promoteSeafHTTPPublishAttemptReferencesFn = func(database *db.DB, orgID, attemptID string, blockIDs []string, registerPermanent func() error) error {
		return nil
	}
	finalizeSeafHTTPPublishedBlockReferences(&v2.FSHelper{}, nil, "org-1", "repo-1", "commit-1", "fs-1", label, []string{"sha1-a"}, []string{"sha256-a"})
	if got := reconciliationFailures(label) - before; got != 0 || scheduled != 0 {
		t.Fatalf("after a successful promotion: events delta = %v, schedules = %d, want 0/0", got, scheduled)
	}

	promoteSeafHTTPPublishAttemptReferencesFn = func(database *db.DB, orgID, attemptID string, blockIDs []string, registerPermanent func() error) error {
		return errors.New("promotion failed after HEAD")
	}
	finalizeSeafHTTPPublishedBlockReferences(&v2.FSHelper{}, nil, "org-1", "repo-1", "commit-1", "fs-1", label, []string{"sha1-a"}, []string{"sha256-a"})
	if got := reconciliationFailures(label) - before; got != 1 {
		t.Fatalf("post_head_reconciliation_failures{%s} delta = %v, want 1", label, got)
	}
	if scheduled != 1 {
		t.Fatalf("schedules = %d, want 1", scheduled)
	}
}
