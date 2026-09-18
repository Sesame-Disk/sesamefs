package v2

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// The metrics are process-global counters/gauges; tests read deltas for
// counters and absolute values for gauges the sweep sets.

func sweepRowsCount(outcome string) float64 {
	return testutil.ToFloat64(metrics.PublishRepairSweepRowsTotal.WithLabelValues(outcome))
}

func visitsCount(outcome string) float64 {
	return testutil.ToFloat64(metrics.PublishRepairVisitsTotal.WithLabelValues(outcome))
}

// One sweep over rows in every scheduling state: the sweep counts each row
// by what it did with it, reports the pending backlog (residue excluded)
// and the oldest pending age, and stamps both the start and the complete
// timestamps because every bucket listed.
func TestRunPublishedBlockReferenceRepairSweepReportsBacklogAndOutcomes(t *testing.T) {
	oldNow := publishedBlockReferenceRepairNowFn
	oldList := listPublishedBlockReferenceRepairsForBucketFn
	oldClassify := publishedBlockReferenceRepairClassifyFn
	oldSchedule := schedulePublishedBlockReferenceRepairRetryFn
	oldLoad := loadPublishedBlockReferenceRepairFn
	oldRenew := renewPublishedBlockReferenceRepairLivenessFn
	oldReap := reapPublishedBlockReferenceRepairProgressOnlyRowFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairNowFn = oldNow
		listPublishedBlockReferenceRepairsForBucketFn = oldList
		publishedBlockReferenceRepairClassifyFn = oldClassify
		schedulePublishedBlockReferenceRepairRetryFn = oldSchedule
		loadPublishedBlockReferenceRepairFn = oldLoad
		renewPublishedBlockReferenceRepairLivenessFn = oldRenew
		reapPublishedBlockReferenceRepairProgressOnlyRowFn = oldReap
		publishedBlockReferenceRepairNextRetryAt.Range(func(key, _ any) bool {
			publishedBlockReferenceRepairNextRetryAt.Delete(key)
			return true
		})
	})

	now := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)
	publishedBlockReferenceRepairNowFn = func() time.Time { return now }
	mk := func(fsID string, age time.Duration, lease time.Time) publishedBlockReferenceRepair {
		return publishedBlockReferenceRepair{
			Bucket: 0, OrgID: "org-1", RepoID: "repo-1", CommitID: "commit-1", FSID: fsID,
			StagedBlockIDs: []string{"block-1"}, CreatedAt: now.Add(-age), LeaseExpiresAt: lease,
		}
	}
	due := mk("fs-due", 3*time.Hour, now.Add(-time.Second))
	leased := mk("fs-leased", 2*time.Hour, now.Add(time.Hour))
	young := mk("fs-young", 10*time.Second, time.Time{})
	hinted := mk("fs-hinted", 90*time.Minute, now.Add(-time.Second))
	residue := publishedBlockReferenceRepair{Bucket: 1, OrgID: "org-1", RepoID: "repo-1", CommitID: "commit-1", FSID: "fs-residue", ReachabilityAnchorHeadCommitID: "head"}
	publishedBlockReferenceRepairNextRetryAt.Store(publishedBlockReferenceRepairRetryKey(hinted), now.Add(time.Hour))

	listPublishedBlockReferenceRepairsForBucketFn = func(database *db.DB, bucket int) ([]publishedBlockReferenceRepair, error) {
		switch bucket {
		case 0:
			return []publishedBlockReferenceRepair{due, leased, young, hinted}, nil
		case 1:
			return []publishedBlockReferenceRepair{residue}, nil
		}
		return nil, nil
	}
	loadPublishedBlockReferenceRepairFn = func(database *db.DB, got publishedBlockReferenceRepair) (publishedBlockReferenceRepair, error) {
		return due, nil
	}
	publishedBlockReferenceRepairClassifyFn = func(database *db.DB, repair *publishedBlockReferenceRepair) (publishedBlockReferenceRepairCommitOutcome, error) {
		return publishedBlockReferenceRepairCommitUnknown, nil
	}
	renewPublishedBlockReferenceRepairLivenessFn = func(database *db.DB, repair publishedBlockReferenceRepair) error { return nil }
	schedulePublishedBlockReferenceRepairRetryFn = func(database *db.DB, repair publishedBlockReferenceRepair, retryAt time.Time) error { return nil }
	reapPublishedBlockReferenceRepairProgressOnlyRowFn = func(database *db.DB, repair publishedBlockReferenceRepair) (bool, error) { return true, nil }

	before := map[string]float64{}
	for _, o := range []string{"visited", "skipped_retry_hint", "skipped_young", "skipped_lease", "residue_reaped", "residue_reap_failed"} {
		before[o] = sweepRowsCount(o)
	}
	retainedBefore := visitsCount("retained")

	err := runPublishedBlockReferenceRepairSweep(&db.DB{})
	if err == nil || !strings.Contains(err.Error(), "retain queued repair") {
		t.Fatalf("sweep = %v, want the due row's UNKNOWN retention error", err)
	}

	want := map[string]float64{"visited": 1, "skipped_retry_hint": 1, "skipped_young": 1, "skipped_lease": 1, "residue_reaped": 1, "residue_reap_failed": 0}
	for o, delta := range want {
		if got := sweepRowsCount(o) - before[o]; got != delta {
			t.Fatalf("publish_repair_sweep_rows_total{outcome=%q} delta = %v, want %v", o, got, delta)
		}
	}
	if got := visitsCount("retained") - retainedBefore; got != 1 {
		t.Fatalf("publish_repair_visits_total{retained} delta = %v, want 1", got)
	}
	if got := testutil.ToFloat64(metrics.PublishRepairPendingRows); got != 4 {
		t.Fatalf("publish_repair_pending_rows = %v, want 4 (residue excluded)", got)
	}
	if got := testutil.ToFloat64(metrics.PublishRepairOldestPendingAge); got != (3 * time.Hour).Seconds() {
		t.Fatalf("publish_repair_oldest_pending_age_seconds = %v, want %v", got, (3 * time.Hour).Seconds())
	}
	if got := testutil.ToFloat64(metrics.PublishRepairLastSweepStart); got != float64(now.Unix()) {
		t.Fatalf("publish_repair_last_sweep_started_timestamp_seconds = %v, want %v", got, now.Unix())
	}
	if got := testutil.ToFloat64(metrics.PublishRepairLastCompleteSweep); got != float64(now.Unix()) {
		t.Fatalf("publish_repair_last_complete_sweep_timestamp_seconds = %v, want %v: every bucket listed", got, now.Unix())
	}
}

// A sweep that could not list one bucket did not observe the backlog: the
// start timestamp advances, the complete timestamp and the backlog gauges
// do not, so a health gate reading the complete timestamp sees the gap.
func TestRunPublishedBlockReferenceRepairSweepListingErrorWithholdsCompleteTimestamp(t *testing.T) {
	oldNow := publishedBlockReferenceRepairNowFn
	oldList := listPublishedBlockReferenceRepairsForBucketFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairNowFn = oldNow
		listPublishedBlockReferenceRepairsForBucketFn = oldList
	})
	metrics.PublishRepairLastCompleteSweep.Set(1_000)
	metrics.PublishRepairPendingRows.Set(7)
	metrics.PublishRepairOldestPendingAge.Set(42)

	now := time.Date(2026, time.September, 18, 13, 0, 0, 0, time.UTC)
	publishedBlockReferenceRepairNowFn = func() time.Time { return now }
	listPublishedBlockReferenceRepairsForBucketFn = func(database *db.DB, bucket int) ([]publishedBlockReferenceRepair, error) {
		if bucket == 5 {
			return nil, fmt.Errorf("bucket 5 unavailable")
		}
		return nil, nil
	}
	if err := runPublishedBlockReferenceRepairSweep(&db.DB{}); err == nil || !strings.Contains(err.Error(), "bucket 5") {
		t.Fatalf("sweep = %v, want the listing error surfaced", err)
	}
	if got := testutil.ToFloat64(metrics.PublishRepairLastSweepStart); got != float64(now.Unix()) {
		t.Fatalf("last_sweep_started = %v, want %v", got, now.Unix())
	}
	if got := testutil.ToFloat64(metrics.PublishRepairLastCompleteSweep); got != 1_000 {
		t.Fatalf("last_complete_sweep = %v, want unchanged 1000: a sweep with a listing error is not a complete observation", got)
	}
	if got := testutil.ToFloat64(metrics.PublishRepairPendingRows); got != 7 {
		t.Fatalf("pending_rows = %v, want unchanged 7", got)
	}
	if got := testutil.ToFloat64(metrics.PublishRepairOldestPendingAge); got != 42 {
		t.Fatalf("oldest_pending_age = %v, want unchanged 42", got)
	}
}

// Visit outcomes: nil → ok; the ordinary UNKNOWN retention → retained;
// a renewal failure, even when joined with the retention error, → failed.
func TestPublishedBlockReferenceRepairVisitOutcomeClassification(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, "ok"},
		{"retained", fmt.Errorf("publication outcome is unknown; retain queued repair: %w", errPublishedBlockReferenceRepairRetained), "retained"},
		{"renewal failed alone", fmt.Errorf("renew: %w", errPublishedBlockReferenceRepairRenewalFailed), "failed"},
		{"renewal failed joined with retained", errors.Join(fmt.Errorf("renew: %w", errPublishedBlockReferenceRepairRenewalFailed), fmt.Errorf("retain: %w", errPublishedBlockReferenceRepairRetained)), "failed"},
		{"other error", errors.New("hydrate: timeout"), "failed"},
	}
	for _, tc := range cases {
		if got := publishedBlockReferenceRepairVisitOutcome(tc.err); got != tc.want {
			t.Fatalf("%s: outcome = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// A full visit whose durable renewal fails counts one renewal failure and a
// failed visit, not a retained one, because the row was left without the
// renewal main relies on.
func TestRepairPublishedBlockReferenceRepairRenewalFailureCountsAsFailedVisit(t *testing.T) {
	oldClassify := publishedBlockReferenceRepairClassifyFn
	oldLoad := loadPublishedBlockReferenceRepairFn
	oldRenew := renewPublishedBlockReferenceRepairLivenessFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairClassifyFn = oldClassify
		loadPublishedBlockReferenceRepairFn = oldLoad
		renewPublishedBlockReferenceRepairLivenessFn = oldRenew
	})
	repair := newTestPublishedBlockReferenceRepair("commit-1")
	loadPublishedBlockReferenceRepairFn = func(database *db.DB, got publishedBlockReferenceRepair) (publishedBlockReferenceRepair, error) {
		return repair, nil
	}
	publishedBlockReferenceRepairClassifyFn = func(database *db.DB, repair *publishedBlockReferenceRepair) (publishedBlockReferenceRepairCommitOutcome, error) {
		return publishedBlockReferenceRepairCommitUnknown, nil
	}
	renewPublishedBlockReferenceRepairLivenessFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		return errors.New("write unavailable at block-2")
	}
	failedBefore, retainedBefore := visitsCount("failed"), visitsCount("retained")
	renewalsBefore := testutil.ToFloat64(metrics.PublishRepairRenewalFailuresTotal)

	err := repairPublishedBlockReferenceRepair(&db.DB{}, repair)
	if err == nil || !strings.Contains(err.Error(), "write unavailable") || !strings.Contains(err.Error(), "retain queued repair") {
		t.Fatalf("visit = %v, want the renewal error joined with the retention error, as main returns", err)
	}
	if got := testutil.ToFloat64(metrics.PublishRepairRenewalFailuresTotal) - renewalsBefore; got != 1 {
		t.Fatalf("publish_repair_renewal_failures_total delta = %v, want 1", got)
	}
	if got := visitsCount("retained") - retainedBefore; got != 0 {
		t.Fatalf("visits{retained} delta = %v, want 0: a visit that could not renew is not a retained visit", got)
	}
	if got := visitsCount("failed") - failedBefore; got != 1 {
		t.Fatalf("visits{failed} delta = %v, want 1", got)
	}
}

// Every scheduled background repair is a post-HEAD promotion failure of the
// funnel that scheduled it; its one-shot outcome is counted too.
func TestSchedulePublishedBlockReferenceRepairCountsPostHeadPromotionFailures(t *testing.T) {
	oldRun := schedulePublishedBlockReferenceRepairRunFn
	oldSleep := schedulePublishedBlockReferenceRepairSleepFn
	t.Cleanup(func() {
		schedulePublishedBlockReferenceRepairRunFn = oldRun
		schedulePublishedBlockReferenceRepairSleepFn = oldSleep
	})
	var pending []func()
	schedulePublishedBlockReferenceRepairRunFn = func(repair func()) { pending = append(pending, repair) }
	schedulePublishedBlockReferenceRepairSleepFn = func(time.Duration) {}

	funnel := "MetricsTestFunnel"
	failuresBefore := testutil.ToFloat64(metrics.PublishRepairPostHeadPromotionFailuresTotal.WithLabelValues(funnel))
	okBefore := testutil.ToFloat64(metrics.PublishRepairImmediateRepairsTotal.WithLabelValues("ok"))
	failedBefore := testutil.ToFloat64(metrics.PublishRepairImmediateRepairsTotal.WithLabelValues("failed"))

	SchedulePublishedBlockReferenceRepair("metrics-key-1", funnel, func() error { return nil })
	SchedulePublishedBlockReferenceRepair("metrics-key-1", funnel, func() error { return nil }) // deduplicated, still a failure event
	SchedulePublishedBlockReferenceRepair("metrics-key-2", funnel, func() error { return errors.New("still failing") })
	if got := testutil.ToFloat64(metrics.PublishRepairPostHeadPromotionFailuresTotal.WithLabelValues(funnel)) - failuresBefore; got != 3 {
		t.Fatalf("post_head_promotion_failures{%s} delta = %v, want 3 (every schedule call is a promotion failure, deduplicated or not)", funnel, got)
	}
	for _, run := range pending {
		run()
	}
	if got := testutil.ToFloat64(metrics.PublishRepairImmediateRepairsTotal.WithLabelValues("ok")) - okBefore; got != 1 {
		t.Fatalf("immediate_repairs{ok} delta = %v, want 1", got)
	}
	if got := testutil.ToFloat64(metrics.PublishRepairImmediateRepairsTotal.WithLabelValues("failed")) - failedBefore; got != 1 {
		t.Fatalf("immediate_repairs{failed} delta = %v, want 1", got)
	}
}
