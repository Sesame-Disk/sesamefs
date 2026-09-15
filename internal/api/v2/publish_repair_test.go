package v2

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

func TestSchedulePublishedBlockReferenceRepairDeduplicatesInFlightRepair(t *testing.T) {
	oldDelay := libraryHeadMutationRetryDelay
	oldMaxDelay := libraryHeadMutationRetryMaxDelay
	oldJitter := libraryHeadMutationRetryJitter
	oldRun := schedulePublishedBlockReferenceRepairRunFn
	oldSleep := schedulePublishedBlockReferenceRepairSleepFn
	t.Cleanup(func() {
		libraryHeadMutationRetryDelay = oldDelay
		libraryHeadMutationRetryMaxDelay = oldMaxDelay
		libraryHeadMutationRetryJitter = oldJitter
		schedulePublishedBlockReferenceRepairRunFn = oldRun
		schedulePublishedBlockReferenceRepairSleepFn = oldSleep
	})

	libraryHeadMutationRetryDelay = time.Millisecond
	libraryHeadMutationRetryMaxDelay = time.Millisecond
	libraryHeadMutationRetryJitter = 0
	var pending []func()
	var slept []time.Duration
	schedulePublishedBlockReferenceRepairRunFn = func(repair func()) {
		pending = append(pending, repair)
	}
	schedulePublishedBlockReferenceRepairSleepFn = func(delay time.Duration) {
		slept = append(slept, delay)
	}

	repairCalls := 0
	SchedulePublishedBlockReferenceRepair("repair-key", "test", func() error {
		repairCalls++
		return nil
	})
	SchedulePublishedBlockReferenceRepair("repair-key", "test", func() error {
		repairCalls += 100
		return nil
	})
	if len(pending) != 1 {
		t.Fatalf("scheduled repairs = %d, want 1", len(pending))
	}

	pending[0]()
	if repairCalls != 1 {
		t.Fatalf("repairCalls = %d, want 1", repairCalls)
	}
	if len(slept) != 1 || slept[0] != time.Millisecond {
		t.Fatalf("slept = %#v, want []time.Duration{time.Millisecond}", slept)
	}

	SchedulePublishedBlockReferenceRepair("repair-key", "test", func() error {
		repairCalls++
		return nil
	})
	if len(pending) != 2 {
		t.Fatalf("scheduled repairs after completion = %d, want 2", len(pending))
	}
	pending[1]()
}

func TestQueuePendingPublishedFileRepairs_RollsBackPartialInsertFailure(t *testing.T) {
	oldInsert := insertPublishedBlockReferenceRepairFn
	oldDelete := deletePublishedBlockReferenceRepairFn
	t.Cleanup(func() {
		insertPublishedBlockReferenceRepairFn = oldInsert
		deletePublishedBlockReferenceRepairFn = oldDelete
	})

	stageErr := errors.New("insert boom")
	cleanupErr := errors.New("cleanup boom")
	insertCalls := 0
	insertPublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		insertCalls++
		if insertCalls == 2 {
			return stageErr
		}
		return nil
	}
	deleteCalls := 0
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		deleteCalls++
		if deleteCalls == 2 {
			return cleanupErr
		}
		return nil
	}

	pending := []*pendingPublishedFile{
		{fsID: "fs-1", externalBlockIDs: []string{"block-1"}, internalBlockIDs: []string{"staged-1"}},
		{fsID: "fs-2", externalBlockIDs: []string{"block-2"}, internalBlockIDs: []string{"staged-2"}},
	}
	err := queuePendingPublishedFileRepairs(nil, "org-1", "repo-1", "commit-1", pending)
	if !errors.Is(err, stageErr) {
		t.Fatalf("queuePendingPublishedFileRepairs() error = %v, want stage error %v", err, stageErr)
	}
	if !errors.Is(err, cleanupErr) {
		t.Fatalf("queuePendingPublishedFileRepairs() error = %v, want cleanup error %v", err, cleanupErr)
	}
	if insertCalls != 2 {
		t.Fatalf("insertCalls = %d, want 2", insertCalls)
	}
	if deleteCalls != 2 {
		t.Fatalf("deleteCalls = %d, want 2", deleteCalls)
	}
}

func TestCleanupFailedPublishArtifacts_DeletesCommitAndDedupesAttemptRefs(t *testing.T) {
	oldDeleteCommit := cleanupFailedPublishDeleteCommitFn
	oldRemoveRefs := cleanupFailedPublishRemoveAttemptReferencesFn
	t.Cleanup(func() {
		cleanupFailedPublishDeleteCommitFn = oldDeleteCommit
		cleanupFailedPublishRemoveAttemptReferencesFn = oldRemoveRefs
	})

	commitDeletes := 0
	cleanupFailedPublishDeleteCommitFn = func(database *db.DB, repoID, commitID string) error {
		commitDeletes++
		if repoID != "repo-1" || commitID != "commit-losing" {
			t.Fatalf("delete commit args = %s/%s, want repo-1/commit-losing", repoID, commitID)
		}
		return nil
	}
	removeRefsCalls := 0
	cleanupFailedPublishRemoveAttemptReferencesFn = func(database *db.DB, orgID, attemptID string, blockIDs []string) error {
		removeRefsCalls++
		if orgID != "org-1" || attemptID != "attempt-1" {
			t.Fatalf("remove refs args = %s/%s, want org-1/attempt-1", orgID, attemptID)
		}
		if len(blockIDs) != 1 || blockIDs[0] != "queued-block-1" {
			t.Fatalf("remove refs blockIDs = %#v, want []string{\"queued-block-1\"}", blockIDs)
		}
		return nil
	}

	err := CleanupFailedPublishArtifacts(&db.DB{}, "org-1", "repo-1", "attempt-1", "commit-losing", []string{"fs-live", "fs-zombie"}, []string{"queued-block-1", "queued-block-1"})
	if err != nil {
		t.Fatalf("CleanupFailedPublishArtifacts() error = %v, want nil", err)
	}
	if commitDeletes != 1 {
		t.Fatalf("commitDeletes = %d, want 1", commitDeletes)
	}
	if removeRefsCalls != 1 {
		t.Fatalf("removeRefsCalls = %d, want 1", removeRefsCalls)
	}
}

func TestCleanupFailedPublishArtifacts_ReturnsCommitDeleteErrorAfterRemovingRefs(t *testing.T) {
	oldDeleteCommit := cleanupFailedPublishDeleteCommitFn
	oldRemoveRefs := cleanupFailedPublishRemoveAttemptReferencesFn
	t.Cleanup(func() {
		cleanupFailedPublishDeleteCommitFn = oldDeleteCommit
		cleanupFailedPublishRemoveAttemptReferencesFn = oldRemoveRefs
	})

	commitErr := errors.New("commit delete failed")
	cleanupFailedPublishDeleteCommitFn = func(database *db.DB, repoID, commitID string) error {
		return commitErr
	}
	removeRefsCalls := 0
	cleanupFailedPublishRemoveAttemptReferencesFn = func(database *db.DB, orgID, attemptID string, blockIDs []string) error {
		removeRefsCalls++
		return nil
	}

	err := CleanupFailedPublishArtifacts(&db.DB{}, "org-1", "repo-1", "attempt-1", "commit-losing", []string{"fs-zombie"}, []string{"queued-block-1"})
	if !errors.Is(err, commitErr) {
		t.Fatalf("CleanupFailedPublishArtifacts() error = %v, want commitErr %v", err, commitErr)
	}
	if removeRefsCalls != 1 {
		t.Fatalf("removeRefsCalls = %d, want 1", removeRefsCalls)
	}
}

func TestCleanupFailedPublishAttempt_PreservesPendingOwnersWhenArtifactCleanupFails(t *testing.T) {
	oldDeleteCommit := cleanupFailedPublishDeleteCommitFn
	oldRelease := releasePendingPublishedFileOwnersFn
	t.Cleanup(func() {
		cleanupFailedPublishDeleteCommitFn = oldDeleteCommit
		releasePendingPublishedFileOwnersFn = oldRelease
	})

	commitErr := errors.New("commit delete failed")
	cleanupFailedPublishDeleteCommitFn = func(database *db.DB, repoID, commitID string) error {
		return commitErr
	}
	releaseCalls := 0
	releasePendingPublishedFileOwnersFn = func(database *db.DB, repoID string, pendingFiles []*pendingPublishedFile) error {
		releaseCalls++
		return nil
	}

	err := CleanupFailedPublishAttempt(&db.DB{}, "org-1", "repo-1", "commit-1", "commit-1", []*pendingPublishedFile{{
		fsID:             "fs-1",
		cleanupOwnerID:   "owner-1",
		cleanupCreatedAt: time.Now().UTC(),
	}})
	if !errors.Is(err, commitErr) {
		t.Fatalf("CleanupFailedPublishAttempt() error = %v, want commitErr %v", err, commitErr)
	}
	if releaseCalls != 0 {
		t.Fatalf("releaseCalls = %d, want 0", releaseCalls)
	}
}

func TestClearPendingPublishedFileOwners_DeletesOwnerWithoutReachabilityChecks(t *testing.T) {
	oldDeletePendingOwner := cleanupFailedPublishDeletePendingOwnerFn
	oldOwnerExists := cleanupFailedPublishPendingOwnerExistsFn
	oldReachable := cleanupFailedPublishFSObjectReachableFn
	t.Cleanup(func() {
		cleanupFailedPublishDeletePendingOwnerFn = oldDeletePendingOwner
		cleanupFailedPublishPendingOwnerExistsFn = oldOwnerExists
		cleanupFailedPublishFSObjectReachableFn = oldReachable
	})

	deleteCalls := 0
	cleanupFailedPublishDeletePendingOwnerFn = func(database *db.DB, repoID, fsID, ownerID string, createdAt time.Time) error {
		deleteCalls++
		return nil
	}
	cleanupFailedPublishPendingOwnerExistsFn = func(database *db.DB, repoID, fsID string) (bool, error) {
		t.Fatal("clearPendingPublishedFileOwners should not check remaining owners")
		return false, nil
	}
	cleanupFailedPublishFSObjectReachableFn = func(database *db.DB, repoID, fsID string) (bool, error) {
		t.Fatal("clearPendingPublishedFileOwners should not check reachability")
		return false, nil
	}

	err := clearPendingPublishedFileOwners(&db.DB{}, "repo-1", []*pendingPublishedFile{{
		fsID:             "fs-1",
		cleanupOwnerID:   "owner-1",
		cleanupCreatedAt: time.Now().UTC(),
	}})
	if err != nil {
		t.Fatalf("clearPendingPublishedFileOwners() error = %v, want nil", err)
	}
	if deleteCalls != 1 {
		t.Fatalf("deleteCalls = %d, want 1", deleteCalls)
	}
}

func TestCleanupFailedPublishAttempt_ReleasesOwnerWithoutDeletingSharedFSObject(t *testing.T) {
	oldDeletePendingOwner := cleanupFailedPublishDeletePendingOwnerFn
	oldOwnerExists := cleanupFailedPublishPendingOwnerExistsFn
	oldReachable := cleanupFailedPublishFSObjectReachableFn
	oldDeleteFSObject := cleanupFailedPublishDeleteFSObjectFn
	t.Cleanup(func() {
		cleanupFailedPublishDeletePendingOwnerFn = oldDeletePendingOwner
		cleanupFailedPublishPendingOwnerExistsFn = oldOwnerExists
		cleanupFailedPublishFSObjectReachableFn = oldReachable
		cleanupFailedPublishDeleteFSObjectFn = oldDeleteFSObject
	})

	deletedOwners := 0
	cleanupFailedPublishDeletePendingOwnerFn = func(database *db.DB, repoID, fsID, ownerID string, createdAt time.Time) error {
		deletedOwners++
		if repoID != "repo-1" || fsID != "fs-1" || ownerID != "owner-1" {
			t.Fatalf("delete pending owner args = %s/%s/%s, want repo-1/fs-1/owner-1", repoID, fsID, ownerID)
		}
		return nil
	}
	cleanupFailedPublishPendingOwnerExistsFn = func(database *db.DB, repoID, fsID string) (bool, error) {
		t.Fatal("release should not check owner existence before deciding whether to delete a shared fs_object")
		return false, nil
	}
	cleanupFailedPublishFSObjectReachableFn = func(database *db.DB, repoID, fsID string) (bool, error) {
		t.Fatal("release should not check reachability before deciding whether to delete a shared fs_object")
		return false, nil
	}
	cleanupFailedPublishDeleteFSObjectFn = func(database *db.DB, repoID, fsID string) error {
		t.Fatal("release should not delete content-addressed fs_object rows")
		return nil
	}

	err := CleanupFailedPublishAttempt(&db.DB{}, "", "repo-1", "", "", []*pendingPublishedFile{{
		fsID:             "fs-1",
		cleanupOwnerID:   "owner-1",
		cleanupCreatedAt: time.Date(2026, time.June, 1, 10, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("CleanupFailedPublishAttempt() error = %v, want nil", err)
	}
	if deletedOwners != 1 {
		t.Fatalf("deletedOwners = %d, want 1", deletedOwners)
	}
}

func TestFailedPublishFSObjectReachableFromRoot_IgnoresMissingUnrelatedNodes(t *testing.T) {
	oldLoad := failedPublishReachabilityLoadFSObjectFn
	t.Cleanup(func() {
		failedPublishReachabilityLoadFSObjectFn = oldLoad
	})

	failedPublishReachabilityLoadFSObjectFn = func(database *db.DB, repoID, fsID string) (string, string, error) {
		if repoID != "repo-1" {
			t.Fatalf("repoID = %q, want repo-1", repoID)
		}
		switch fsID {
		case "root-fs":
			return "dir", `[{"name":"target","id":"target-fs"},{"name":"missing","id":"missing-fs"}]`, nil
		case "missing-fs":
			return "", "", gocql.ErrNotFound
		default:
			t.Fatalf("unexpected fsID lookup %q", fsID)
			return "", "", nil
		}
	}

	reachable, err := failedPublishFSObjectReachableFromRoot(&db.DB{}, "repo-1", "target-fs", "root-fs", map[string]bool{})
	if err != nil {
		t.Fatalf("failedPublishFSObjectReachableFromRoot() error = %v, want nil", err)
	}
	if !reachable {
		t.Fatal("failedPublishFSObjectReachableFromRoot() = false, want true when target remains reachable past a missing sibling")
	}
}

func TestRunPendingPublishedFSObjectOwnerSweep_ReleasesOnlyStaleOwners(t *testing.T) {
	oldNow := pendingPublishedFSObjectOwnerNowFn
	oldList := listPendingPublishedFSObjectOwnersByDayFn
	oldLoad := loadPendingPublishedFSObjectOwnerFn
	oldCleanup := cleanupPendingPublishedFileOwnerAttemptFn
	t.Cleanup(func() {
		pendingPublishedFSObjectOwnerNowFn = oldNow
		listPendingPublishedFSObjectOwnersByDayFn = oldList
		loadPendingPublishedFSObjectOwnerFn = oldLoad
		cleanupPendingPublishedFileOwnerAttemptFn = oldCleanup
	})

	now := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	pendingPublishedFSObjectOwnerNowFn = func() time.Time { return now }
	listPendingPublishedFSObjectOwnersByDayFn = func(database *db.DB, day time.Time, bucket int) ([]db.PendingPublishedFSObjectOwner, error) {
		if !day.Equal(db.GCProjectionUTCDate(now)) || bucket != 0 {
			return nil, nil
		}
		return []db.PendingPublishedFSObjectOwner{
			{RepoID: "repo-1", FSID: "fs-stale", OwnerID: "owner-stale", CreatedAt: now.Add(-pendingPublishedFSObjectOwnerStaleAfter - time.Minute), OrgID: "org-1", AttemptID: "commit-stale", BlockIDs: []string{"queued-block-1"}},
			{RepoID: "repo-1", FSID: "fs-fresh", OwnerID: "owner-fresh", CreatedAt: now.Add(-time.Hour)},
		}, nil
	}
	loadPendingPublishedFSObjectOwnerFn = func(database *db.DB, repoID, fsID, ownerID string) (db.PendingPublishedFSObjectOwner, error) {
		return db.PendingPublishedFSObjectOwner{}, nil
	}
	var released []string
	cleanupPendingPublishedFileOwnerAttemptFn = func(database *db.DB, repoID string, pending *pendingPublishedFile) error {
		if pending.cleanupOrgID != "org-1" || pending.cleanupAttemptID != "commit-stale" {
			t.Fatalf("pending cleanup metadata = %s/%s, want org-1/commit-stale", pending.cleanupOrgID, pending.cleanupAttemptID)
		}
		if len(pending.internalBlockIDs) != 1 || pending.internalBlockIDs[0] != "queued-block-1" {
			t.Fatalf("pending.internalBlockIDs = %#v, want []string{\"queued-block-1\"}", pending.internalBlockIDs)
		}
		released = append(released, repoID+":"+pending.fsID+":"+pending.cleanupOwnerID)
		return nil
	}

	err := runPendingPublishedFSObjectOwnerSweep(&db.DB{})
	if err != nil {
		t.Fatalf("runPendingPublishedFSObjectOwnerSweep() error = %v, want nil", err)
	}
	if len(released) != 1 || released[0] != "repo-1:fs-stale:owner-stale" {
		t.Fatalf("released = %#v, want []string{\"repo-1:fs-stale:owner-stale\"}", released)
	}
}

func TestRunPendingPublishedFSObjectOwnerSweep_HydratesMissingMetadataFromPrimaryRow(t *testing.T) {
	oldNow := pendingPublishedFSObjectOwnerNowFn
	oldList := listPendingPublishedFSObjectOwnersByDayFn
	oldLoad := loadPendingPublishedFSObjectOwnerFn
	oldCleanup := cleanupPendingPublishedFileOwnerAttemptFn
	t.Cleanup(func() {
		pendingPublishedFSObjectOwnerNowFn = oldNow
		listPendingPublishedFSObjectOwnersByDayFn = oldList
		loadPendingPublishedFSObjectOwnerFn = oldLoad
		cleanupPendingPublishedFileOwnerAttemptFn = oldCleanup
	})

	now := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	pendingPublishedFSObjectOwnerNowFn = func() time.Time { return now }
	listPendingPublishedFSObjectOwnersByDayFn = func(database *db.DB, day time.Time, bucket int) ([]db.PendingPublishedFSObjectOwner, error) {
		if !day.Equal(db.GCProjectionUTCDate(now)) || bucket != 0 {
			return nil, nil
		}
		return []db.PendingPublishedFSObjectOwner{{
			RepoID:    "repo-1",
			FSID:      "fs-stale",
			OwnerID:   "owner-stale",
			CreatedAt: now.Add(-pendingPublishedFSObjectOwnerStaleAfter - time.Minute),
		}}, nil
	}
	hydrated := 0
	loadPendingPublishedFSObjectOwnerFn = func(database *db.DB, repoID, fsID, ownerID string) (db.PendingPublishedFSObjectOwner, error) {
		hydrated++
		if repoID != "repo-1" || fsID != "fs-stale" || ownerID != "owner-stale" {
			t.Fatalf("hydrate args = %s/%s/%s, want repo-1/fs-stale/owner-stale", repoID, fsID, ownerID)
		}
		return db.PendingPublishedFSObjectOwner{
			RepoID:    repoID,
			FSID:      fsID,
			OwnerID:   ownerID,
			CreatedAt: now.Add(-pendingPublishedFSObjectOwnerStaleAfter - time.Minute),
			OrgID:     "org-1",
			AttemptID: "commit-stale",
			BlockIDs:  []string{"queued-block-1", "queued-block-2"},
		}, nil
	}
	cleanupPendingPublishedFileOwnerAttemptFn = func(database *db.DB, repoID string, pending *pendingPublishedFile) error {
		if repoID != "repo-1" {
			t.Fatalf("cleanup repoID = %s, want repo-1", repoID)
		}
		if pending.cleanupOrgID != "org-1" || pending.cleanupAttemptID != "commit-stale" {
			t.Fatalf("pending cleanup metadata = %s/%s, want org-1/commit-stale", pending.cleanupOrgID, pending.cleanupAttemptID)
		}
		if !reflect.DeepEqual(pending.internalBlockIDs, []string{"queued-block-1", "queued-block-2"}) {
			t.Fatalf("pending.internalBlockIDs = %#v, want hydrated block ids", pending.internalBlockIDs)
		}
		return nil
	}

	if err := runPendingPublishedFSObjectOwnerSweep(&db.DB{}); err != nil {
		t.Fatalf("runPendingPublishedFSObjectOwnerSweep() error = %v, want nil", err)
	}
	if hydrated != 1 {
		t.Fatalf("hydrated = %d, want 1", hydrated)
	}
}

func TestRunPendingPublishedFSObjectOwnerSweep_DeletesDanglingProjectionWhenPrimaryRowMissing(t *testing.T) {
	oldNow := pendingPublishedFSObjectOwnerNowFn
	oldList := listPendingPublishedFSObjectOwnersByDayFn
	oldLoad := loadPendingPublishedFSObjectOwnerFn
	oldDeletePendingOwner := cleanupFailedPublishDeletePendingOwnerFn
	oldCleanup := cleanupPendingPublishedFileOwnerAttemptFn
	t.Cleanup(func() {
		pendingPublishedFSObjectOwnerNowFn = oldNow
		listPendingPublishedFSObjectOwnersByDayFn = oldList
		loadPendingPublishedFSObjectOwnerFn = oldLoad
		cleanupFailedPublishDeletePendingOwnerFn = oldDeletePendingOwner
		cleanupPendingPublishedFileOwnerAttemptFn = oldCleanup
	})

	now := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	pendingPublishedFSObjectOwnerNowFn = func() time.Time { return now }
	listPendingPublishedFSObjectOwnersByDayFn = func(database *db.DB, day time.Time, bucket int) ([]db.PendingPublishedFSObjectOwner, error) {
		if !day.Equal(db.GCProjectionUTCDate(now)) || bucket != 0 {
			return nil, nil
		}
		return []db.PendingPublishedFSObjectOwner{{
			RepoID:    "repo-1",
			FSID:      "fs-stale",
			OwnerID:   "owner-stale",
			CreatedAt: now.Add(-pendingPublishedFSObjectOwnerStaleAfter - time.Minute),
		}}, nil
	}
	loadPendingPublishedFSObjectOwnerFn = func(database *db.DB, repoID, fsID, ownerID string) (db.PendingPublishedFSObjectOwner, error) {
		return db.PendingPublishedFSObjectOwner{}, gocql.ErrNotFound
	}
	deleted := 0
	cleanupFailedPublishDeletePendingOwnerFn = func(database *db.DB, repoID, fsID, ownerID string, createdAt time.Time) error {
		deleted++
		if repoID != "repo-1" || fsID != "fs-stale" || ownerID != "owner-stale" {
			t.Fatalf("delete args = %s/%s/%s, want repo-1/fs-stale/owner-stale", repoID, fsID, ownerID)
		}
		if !createdAt.Equal(now.Add(-pendingPublishedFSObjectOwnerStaleAfter - time.Minute)) {
			t.Fatalf("delete createdAt = %v, want projection timestamp", createdAt)
		}
		return nil
	}
	cleanupPendingPublishedFileOwnerAttemptFn = func(database *db.DB, repoID string, pending *pendingPublishedFile) error {
		t.Fatal("cleanupPendingPublishedFileOwnerAttemptFn must not run for dangling projections")
		return nil
	}

	if err := runPendingPublishedFSObjectOwnerSweep(&db.DB{}); err != nil {
		t.Fatalf("runPendingPublishedFSObjectOwnerSweep() error = %v, want nil", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
}

func TestCleanupPendingPublishedFileOwnerAttempt_PromotesReachableCommitBeforeClearingOwner(t *testing.T) {
	oldReachable := cleanupPendingPublishedFileAttemptCommitReachableFn
	oldLoad := loadPublishedBlockReferenceRepairPendingFileFn
	oldPromote := publishedBlockReferenceRepairPromoteFn
	oldClear := clearPendingPublishedFileOwnerFn
	oldDeleteCommit := cleanupFailedPublishDeleteCommitFn
	oldRemoveAttemptRefs := cleanupFailedPublishRemoveAttemptReferencesFn
	t.Cleanup(func() {
		cleanupPendingPublishedFileAttemptCommitReachableFn = oldReachable
		loadPublishedBlockReferenceRepairPendingFileFn = oldLoad
		publishedBlockReferenceRepairPromoteFn = oldPromote
		clearPendingPublishedFileOwnerFn = oldClear
		cleanupFailedPublishDeleteCommitFn = oldDeleteCommit
		cleanupFailedPublishRemoveAttemptReferencesFn = oldRemoveAttemptRefs
	})

	reachabilityChecks := 0
	cleanupPendingPublishedFileAttemptCommitReachableFn = func(database *db.DB, orgID, repoID, commitID string) (publishedBlockReferenceRepairCommitOutcome, error) {
		reachabilityChecks++
		if repoID != "repo-1" || commitID != "commit-1" {
			t.Fatalf("reachability args = %s/%s, want repo-1/commit-1", repoID, commitID)
		}
		return publishedBlockReferenceRepairCommitReachable, nil
	}
	loaded := 0
	loadPublishedBlockReferenceRepairPendingFileFn = func(database *db.DB, repoID, fsID string) (*pendingPublishedFile, error) {
		loaded++
		if repoID != "repo-1" || fsID != "fs-1" {
			t.Fatalf("load args = %s/%s, want repo-1/fs-1", repoID, fsID)
		}
		return &pendingPublishedFile{fsID: fsID, externalBlockIDs: []string{"block-ext-1"}}, nil
	}
	promoted := 0
	publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
		promoted++
		if helper == nil {
			t.Fatal("promote helper must be initialized")
		}
		if orgID != "org-1" || repoID != "repo-1" || commitID != "commit-1" {
			t.Fatalf("promote args = %s/%s/%s, want org-1/repo-1/commit-1", orgID, repoID, commitID)
		}
		if pending == nil || pending.fsID != "fs-1" {
			t.Fatalf("promote pending fsID = %#v, want fs-1", pending)
		}
		if !reflect.DeepEqual(pending.externalBlockIDs, []string{"block-ext-1"}) {
			t.Fatalf("promote externalBlockIDs = %#v, want []string{\"block-ext-1\"}", pending.externalBlockIDs)
		}
		if !reflect.DeepEqual(pending.internalBlockIDs, []string{"block-int-1"}) {
			t.Fatalf("promote internalBlockIDs = %#v, want []string{\"block-int-1\"}", pending.internalBlockIDs)
		}
		return nil
	}
	clearedOwners := 0
	clearPendingPublishedFileOwnerFn = func(database *db.DB, repoID string, pending *pendingPublishedFile) error {
		clearedOwners++
		if repoID != "repo-1" || pending == nil || pending.fsID != "fs-1" || pending.cleanupOwnerID != "owner-1" {
			t.Fatalf("clear owner args = %s/%#v, want repo-1 owner fs-1/owner-1", repoID, pending)
		}
		return nil
	}
	cleanupFailedPublishDeleteCommitFn = func(database *db.DB, repoID, commitID string) error {
		t.Fatal("published commit cleanup must not run for reachable commits")
		return nil
	}
	cleanupFailedPublishRemoveAttemptReferencesFn = func(database *db.DB, orgID, attemptID string, blockIDs []string) error {
		t.Fatal("publish-attempt ref cleanup must not run for reachable commits")
		return nil
	}

	err := cleanupPendingPublishedFileOwnerAttempt(&db.DB{}, "repo-1", &pendingPublishedFile{
		fsID:             "fs-1",
		cleanupOwnerID:   "owner-1",
		cleanupCreatedAt: time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC),
		cleanupOrgID:     "org-1",
		cleanupAttemptID: "commit-1",
		internalBlockIDs: []string{"block-int-1"},
	})
	if err != nil {
		t.Fatalf("cleanupPendingPublishedFileOwnerAttempt() error = %v, want nil", err)
	}
	if reachabilityChecks != 1 {
		t.Fatalf("reachabilityChecks = %d, want 1", reachabilityChecks)
	}
	if loaded != 1 {
		t.Fatalf("loaded = %d, want 1", loaded)
	}
	if promoted != 1 {
		t.Fatalf("promoted = %d, want 1", promoted)
	}
	if clearedOwners != 1 {
		t.Fatalf("clearedOwners = %d, want 1", clearedOwners)
	}
}

func TestCleanupPendingPublishedFileOwnerAttempt_FailsClosedWithoutAttemptMetadata(t *testing.T) {
	oldReachable := cleanupPendingPublishedFileAttemptCommitReachableFn
	oldDeletePendingOwner := cleanupFailedPublishDeletePendingOwnerFn
	oldDeleteCommit := cleanupFailedPublishDeleteCommitFn
	t.Cleanup(func() {
		cleanupPendingPublishedFileAttemptCommitReachableFn = oldReachable
		cleanupFailedPublishDeletePendingOwnerFn = oldDeletePendingOwner
		cleanupFailedPublishDeleteCommitFn = oldDeleteCommit
	})

	reachabilityChecks := 0
	cleanupPendingPublishedFileAttemptCommitReachableFn = func(database *db.DB, orgID, repoID, commitID string) (publishedBlockReferenceRepairCommitOutcome, error) {
		reachabilityChecks++
		return publishedBlockReferenceRepairCommitUnknown, nil
	}
	clearedOwners := 0
	cleanupFailedPublishDeletePendingOwnerFn = func(database *db.DB, repoID, fsID, ownerID string, createdAt time.Time) error {
		clearedOwners++
		return nil
	}
	deleteCommitCalls := 0
	cleanupFailedPublishDeleteCommitFn = func(database *db.DB, repoID, commitID string) error {
		deleteCommitCalls++
		return nil
	}

	err := cleanupPendingPublishedFileOwnerAttempt(&db.DB{}, "repo-1", &pendingPublishedFile{
		fsID:             "fs-1",
		cleanupOwnerID:   "owner-1",
		cleanupCreatedAt: time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC),
		cleanupAttemptID: "   ",
	})
	if err == nil || !strings.Contains(err.Error(), "missing cleanup attempt metadata") {
		t.Fatalf("cleanupPendingPublishedFileOwnerAttempt() error = %v, want missing cleanup attempt metadata", err)
	}
	if reachabilityChecks != 0 {
		t.Fatalf("reachabilityChecks = %d, want 0", reachabilityChecks)
	}
	if clearedOwners != 0 {
		t.Fatalf("clearedOwners = %d, want 0", clearedOwners)
	}
	if deleteCommitCalls != 0 {
		t.Fatalf("deleteCommitCalls = %d, want 0", deleteCommitCalls)
	}
}

func TestRepairPublishedFSObjectBlockReferenceRepair_PromotesReachableCommit(t *testing.T) {
	oldClassify := publishedBlockReferenceRepairClassifyFn
	oldLoad := loadPublishedBlockReferenceRepairPendingFileFn
	oldPromote := publishedBlockReferenceRepairPromoteFn
	oldDelete := deletePublishedBlockReferenceRepairFn
	oldRenew := renewPublishedBlockReferenceRepairLivenessFn
	oldRemove := removePublishedBlockReferenceRepairOwnedLivenessFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairClassifyFn = oldClassify
		loadPublishedBlockReferenceRepairPendingFileFn = oldLoad
		publishedBlockReferenceRepairPromoteFn = oldPromote
		deletePublishedBlockReferenceRepairFn = oldDelete
		renewPublishedBlockReferenceRepairLivenessFn = oldRenew
		removePublishedBlockReferenceRepairOwnedLivenessFn = oldRemove
	})

	publishedBlockReferenceRepairClassifyFn = func(database *db.DB, repair *publishedBlockReferenceRepair) (publishedBlockReferenceRepairCommitOutcome, error) {
		return publishedBlockReferenceRepairCommitReachable, nil
	}
	promoteCalls := 0
	events := make([]string, 0, 4)
	// The pre-classify renewal is the only pub: write of the visit; settlement
	// itself must not renew again (it removes the identity instead).
	renewPublishedBlockReferenceRepairLivenessFn = func(database *db.DB, repair *publishedBlockReferenceRepair) error {
		events = append(events, "renew")
		if promoteCalls != 0 {
			t.Fatal("reachable settlement must not renew attempt-local pub: liveness after promotion")
		}
		return nil
	}
	loadPublishedBlockReferenceRepairPendingFileFn = func(database *db.DB, repoID, fsID string) (*pendingPublishedFile, error) {
		return &pendingPublishedFile{fsID: fsID, externalBlockIDs: []string{"fs-block-1"}}, nil
	}
	publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
		promoteCalls++
		events = append(events, "promote")
		if orgID != "org-1" || repoID != "repo-1" || commitID != "commit-1" {
			t.Fatalf("promote args = %s/%s/%s, want org-1/repo-1/commit-1", orgID, repoID, commitID)
		}
		if len(pending.internalBlockIDs) != 1 || pending.internalBlockIDs[0] != "queued-block-1" {
			t.Fatalf("pending.internalBlockIDs = %#v, want []string{\"queued-block-1\"}", pending.internalBlockIDs)
		}
		if len(pending.externalBlockIDs) != 1 || pending.externalBlockIDs[0] != "fs-block-1" {
			t.Fatalf("pending.externalBlockIDs = %#v, want []string{\"fs-block-1\"}", pending.externalBlockIDs)
		}
		return nil
	}
	removePublishedBlockReferenceRepairOwnedLivenessFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		events = append(events, "remove-owned-pub")
		if repair.OrgID != "org-1" || repair.CommitID != "commit-1" || repair.FSID != "fs-1" {
			t.Fatalf("remove-owned-pub repair = %#v, want org-1/commit-1/fs-1", repair)
		}
		if publishedBlockReferenceRepairLivenessAttemptID(repair) == repair.CommitID {
			t.Fatal("reachable settlement must not treat commitID as the repair-owned pub identity")
		}
		if !reflect.DeepEqual(repair.StagedBlockIDs, []string{"queued-block-1"}) {
			t.Fatalf("remove-owned-pub blockIDs = %#v, want []string{\"queued-block-1\"}", repair.StagedBlockIDs)
		}
		return nil
	}
	deleteCalls := 0
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		deleteCalls++
		events = append(events, "delete")
		if repair.RepoID != "repo-1" || repair.CommitID != "commit-1" || repair.FSID != "fs-1" {
			t.Fatalf("delete repair = %#v, want repo-1/commit-1/fs-1", repair)
		}
		return nil
	}

	err := RepairPublishedFSObjectBlockReferenceRepair(nil, "org-1", "repo-1", "commit-1", "fs-1", []string{"queued-block-1"})
	if err != nil {
		t.Fatalf("RepairPublishedFSObjectBlockReferenceRepair() error = %v, want nil", err)
	}
	if promoteCalls != 1 {
		t.Fatalf("promoteCalls = %d, want 1", promoteCalls)
	}
	if deleteCalls != 1 {
		t.Fatalf("deleteCalls = %d, want 1", deleteCalls)
	}
	if !reflect.DeepEqual(events, []string{"renew", "promote", "remove-owned-pub", "delete"}) {
		t.Fatalf("repair settlement order = %#v, want renew, promote, remove-owned-pub, delete", events)
	}
}

func TestRepairPublishedFSObjectBlockReferenceRepair_RetainsUnknownOutcomeAfterLeaseExpiry(t *testing.T) {
	oldClassify := publishedBlockReferenceRepairClassifyFn
	oldHead := publishedBlockReferenceRepairHeadCommitFn
	oldParent := publishedBlockReferenceRepairCommitParentFn
	oldLoad := loadPublishedBlockReferenceRepairPendingFileFn
	oldPromote := publishedBlockReferenceRepairPromoteFn
	oldDelete := deletePublishedBlockReferenceRepairFn
	oldNow := publishedBlockReferenceRepairNowFn
	oldRenew := renewPublishedBlockReferenceRepairLivenessFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairClassifyFn = oldClassify
		publishedBlockReferenceRepairHeadCommitFn = oldHead
		publishedBlockReferenceRepairCommitParentFn = oldParent
		loadPublishedBlockReferenceRepairPendingFileFn = oldLoad
		publishedBlockReferenceRepairPromoteFn = oldPromote
		deletePublishedBlockReferenceRepairFn = oldDelete
		publishedBlockReferenceRepairNowFn = oldNow
		renewPublishedBlockReferenceRepairLivenessFn = oldRenew
	})

	now := time.Date(2026, time.May, 29, 12, 0, 0, 0, time.UTC)
	renewed := 0
	renewPublishedBlockReferenceRepairLivenessFn = func(database *db.DB, repair *publishedBlockReferenceRepair) error {
		renewed++
		if repair.CommitID != "commit-1" || !reflect.DeepEqual(repair.StagedBlockIDs, []string{"queued-block-1"}) {
			t.Fatalf("renew args = %#v, want commit-1 / queued-block-1", repair)
		}
		return nil
	}
	publishedBlockReferenceRepairClassifyFn = func(database *db.DB, repair *publishedBlockReferenceRepair) (publishedBlockReferenceRepairCommitOutcome, error) {
		return publishedBlockReferenceRepairCommitUnknown, nil
	}
	publishedBlockReferenceRepairNowFn = func() time.Time {
		return now
	}
	publishedBlockReferenceRepairHeadCommitFn = func(ctx context.Context, database *db.DB, orgID, repoID string) (string, error) {
		t.Fatal("head lookup should not be repeated after the outcome hook returns UNKNOWN")
		return "", nil
	}
	publishedBlockReferenceRepairCommitParentFn = func(ctx context.Context, database *db.DB, repoID, commitID string) (string, error) {
		t.Fatal("parent lookup should not be repeated after the outcome hook returns UNKNOWN")
		return "", nil
	}
	loadPublishedBlockReferenceRepairPendingFileFn = func(database *db.DB, repoID, fsID string) (*pendingPublishedFile, error) {
		t.Fatal("fs_object lookup should not run for unknown publication")
		return nil, nil
	}
	publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
		t.Fatal("promote should not run for unknown publication")
		return nil
	}
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		t.Fatal("repair row should not be deleted for unknown publication")
		return nil
	}

	repair := publishedBlockReferenceRepair{
		Bucket:         publishedBlockReferenceRepairBucket("org-1", "repo-1", "commit-1", "fs-1"),
		OrgID:          "org-1",
		RepoID:         "repo-1",
		CommitID:       "commit-1",
		FSID:           "fs-1",
		StagedBlockIDs: []string{"queued-block-1"},
		CreatedAt:      now.Add(-10 * time.Minute),
		LeaseExpiresAt: now.Add(-time.Minute),
	}
	err := repairPublishedBlockReferenceRepair(nil, repair)
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("repairPublishedBlockReferenceRepair() error = %v, want unknown-publication retention error", err)
	}
	if renewed != 1 {
		t.Fatalf("unresolved repair renewals = %d, want 1", renewed)
	}
}

func TestPublishedBlockReferenceRepairRetryDelayIsCappedAndAgeBased(t *testing.T) {
	now := time.Date(2026, time.May, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		createdAt time.Time
		want      time.Duration
	}{
		{name: "young row uses base", createdAt: now.Add(-time.Minute), want: publishedBlockReferenceRepairRetryBase},
		{name: "older row backs off with age", createdAt: now.Add(-time.Hour), want: time.Hour},
		{name: "very old row is capped", createdAt: now.Add(-48 * time.Hour), want: publishedBlockReferenceRepairRetryMax},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := publishedBlockReferenceRepairRetryDelay(now, tt.createdAt); got != tt.want {
				t.Fatalf("retry delay = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestRunPublishedBlockReferenceRepairSweepUsesAdvisoryRetrySchedule(t *testing.T) {
	oldNow := publishedBlockReferenceRepairNowFn
	oldList := listPublishedBlockReferenceRepairsForBucketFn
	oldClassify := publishedBlockReferenceRepairClassifyFn
	oldSchedule := schedulePublishedBlockReferenceRepairRetryFn
	oldLoad := loadPublishedBlockReferenceRepairFn
	oldAuthority := loadPublishedBlockReferenceRepairAuthorityFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairNowFn = oldNow
		listPublishedBlockReferenceRepairsForBucketFn = oldList
		publishedBlockReferenceRepairClassifyFn = oldClassify
		schedulePublishedBlockReferenceRepairRetryFn = oldSchedule
		loadPublishedBlockReferenceRepairFn = oldLoad
		loadPublishedBlockReferenceRepairAuthorityFn = oldAuthority
	})

	now := time.Date(2026, time.May, 29, 12, 0, 0, 0, time.UTC)
	publishedBlockReferenceRepairNowFn = func() time.Time { return now }
	repair := publishedBlockReferenceRepair{
		Bucket:         0,
		OrgID:          "org-1",
		RepoID:         "repo-1",
		CommitID:       "commit-1",
		FSID:           "fs-1",
		StagedBlockIDs: []string{"block-1"},
		CreatedAt:      now.Add(-time.Hour),
		LeaseExpiresAt: now.Add(time.Hour),
	}
	listPublishedBlockReferenceRepairsForBucketFn = func(database *db.DB, bucket int) ([]publishedBlockReferenceRepair, error) {
		if bucket == 0 {
			return []publishedBlockReferenceRepair{repair}, nil
		}
		return nil, nil
	}
	loadPublishedBlockReferenceRepairFn = func(database *db.DB, got publishedBlockReferenceRepair) (publishedBlockReferenceRepair, error) {
		return repair, nil
	}
	loadPublishedBlockReferenceRepairAuthorityFn = loadPublishedBlockReferenceRepairFn
	reachableCalls := 0
	publishedBlockReferenceRepairClassifyFn = func(database *db.DB, repair *publishedBlockReferenceRepair) (publishedBlockReferenceRepairCommitOutcome, error) {
		reachableCalls++
		return publishedBlockReferenceRepairCommitUnknown, nil
	}
	scheduled := 0
	var nextRetryAt time.Time
	schedulePublishedBlockReferenceRepairRetryFn = func(database *db.DB, got publishedBlockReferenceRepair, retryAt time.Time) error {
		scheduled++
		nextRetryAt = retryAt
		if got.RepoID != repair.RepoID || got.CommitID != repair.CommitID || got.FSID != repair.FSID {
			t.Fatalf("scheduled repair = %#v, want %#v", got, repair)
		}
		return nil
	}

	if err := runPublishedBlockReferenceRepairSweep(&db.DB{}); err != nil {
		t.Fatalf("future advisory lease made sweep fail: %v", err)
	}
	if reachableCalls != 0 || scheduled != 0 {
		t.Fatalf("future advisory retry was used: reachableCalls=%d scheduled=%d", reachableCalls, scheduled)
	}

	repair.LeaseExpiresAt = now.Add(-time.Second)
	if err := runPublishedBlockReferenceRepairSweep(&db.DB{}); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("due unknown repair error = %v, want unknown retention error", err)
	}
	if reachableCalls != 1 || scheduled != 1 {
		t.Fatalf("due unknown repair calls = reachable %d scheduled %d, want 1/1", reachableCalls, scheduled)
	}
	if !nextRetryAt.After(now) {
		t.Fatalf("nextRetryAt = %s, want future advisory retry", nextRetryAt)
	}
}

func TestShouldRunPendingPublishedFSObjectOwnerSweepUsesFifteenMinuteCadence(t *testing.T) {
	now := time.Date(2026, time.May, 29, 12, 0, 0, 0, time.UTC)
	if !shouldRunPendingPublishedFSObjectOwnerSweep(time.Time{}, now) {
		t.Fatal("initial owner sweep must run")
	}
	if shouldRunPendingPublishedFSObjectOwnerSweep(now, now.Add(14*time.Minute+59*time.Second)) {
		t.Fatal("owner sweep ran before its advisory cadence")
	}
	if !shouldRunPendingPublishedFSObjectOwnerSweep(now, now.Add(pendingPublishedFSObjectOwnerSweepInterval)) {
		t.Fatal("owner sweep did not run at its advisory cadence")
	}
}

func TestClassifyPublishedBlockReferenceRepairCommitOutcome(t *testing.T) {
	tests := []struct {
		name         string
		commitID     string
		headCommitID string
		parents      map[string]string
		want         publishedBlockReferenceRepairCommitOutcome
		wantErr      bool
	}{
		{
			name:         "head commit is reachable",
			commitID:     "c3",
			headCommitID: "c3",
			parents:      map[string]string{"c3": "c2", "c2": "c1", "c1": ""},
			want:         publishedBlockReferenceRepairCommitReachable,
		},
		{
			name:         "ancestor is reachable",
			commitID:     "c1",
			headCommitID: "c3",
			parents:      map[string]string{"c3": "c2", "c2": "c1", "c1": ""},
			want:         publishedBlockReferenceRepairCommitReachable,
		},
		{
			name:         "head stayed at expected parent remains unknown",
			commitID:     "c2",
			headCommitID: "c1",
			parents:      map[string]string{"c2": "c1", "c1": ""},
			want:         publishedBlockReferenceRepairCommitUnknown,
		},
		{
			name:         "concurrent winner remains unknown",
			commitID:     "c2",
			headCommitID: "winner",
			parents:      map[string]string{"c2": "c1", "winner": "c1", "c1": ""},
			want:         publishedBlockReferenceRepairCommitUnknown,
		},
		{
			name:         "unrelated head is unknown",
			commitID:     "c2",
			headCommitID: "other",
			parents:      map[string]string{"c2": "c1", "other": "other-root", "other-root": ""},
			want:         publishedBlockReferenceRepairCommitUnknown,
		},
		{
			name:         "incomplete ancestry is unknown",
			commitID:     "c2",
			headCommitID: "other",
			parents:      map[string]string{"c2": "c1"},
			want:         publishedBlockReferenceRepairCommitUnknown,
			wantErr:      true,
		},
		{
			name:         "target row missing is unknown",
			commitID:     "c3",
			headCommitID: "c3",
			parents:      map[string]string{},
			want:         publishedBlockReferenceRepairCommitUnknown,
			wantErr:      true,
		},
		{
			name:         "cycle is unknown",
			commitID:     "target",
			headCommitID: "c3",
			parents:      map[string]string{"c3": "c2", "c2": "c3"},
			want:         publishedBlockReferenceRepairCommitUnknown,
			wantErr:      true,
		},
		{
			name:         "target self-cycle is unknown",
			commitID:     "c3",
			headCommitID: "c3",
			parents:      map[string]string{"c3": "c3"},
			want:         publishedBlockReferenceRepairCommitUnknown,
			wantErr:      true,
		},
		{
			name:         "malformed whitespace parent is unknown",
			commitID:     "c3",
			headCommitID: "c3",
			parents:      map[string]string{"c3": " \t"},
			want:         publishedBlockReferenceRepairCommitUnknown,
			wantErr:      true,
		},
		{
			name:     "empty head is unknown",
			commitID: "c2",
			parents:  map[string]string{"c2": "c1", "c1": ""},
			want:     publishedBlockReferenceRepairCommitUnknown,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outcome, err := classifyPublishedBlockReferenceRepairCommitOutcome(context.Background(), tt.commitID, tt.headCommitID, func(ctx context.Context, commitID string) (string, error) {
				parent, ok := tt.parents[commitID]
				if !ok {
					return "", gocql.ErrNotFound
				}
				return parent, nil
			})
			if outcome != tt.want {
				t.Fatalf("outcome = %v, want %v", outcome, tt.want)
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestClassifyPublishedCommitReachabilityBoundsWorkWithoutFalseNegatives(t *testing.T) {
	parents := make(map[string]string, publishedCommitReachabilityMaxNodes+1)
	for i := 0; i <= publishedCommitReachabilityMaxNodes; i++ {
		current := fmt.Sprintf("c-%d", i)
		if i == publishedCommitReachabilityMaxNodes {
			parents[current] = ""
		} else {
			parents[current] = fmt.Sprintf("c-%d", i+1)
		}
	}
	lookupCalls := 0
	lookup := func(ctx context.Context, commitID string) (string, error) {
		lookupCalls++
		parent, ok := parents[commitID]
		if !ok {
			return "", gocql.ErrNotFound
		}
		return parent, nil
	}

	outcome, err := classifyPublishedCommitReachability(context.Background(), fmt.Sprintf("c-%d", publishedCommitReachabilityMaxNodes-2), "c-0", publishedCommitReachabilityMaxNodes, lookup)
	if err != nil || outcome != publishedBlockReferenceRepairCommitReachable {
		t.Fatalf("target in node %d = (%v, %v), want REACHABLE", publishedCommitReachabilityMaxNodes-1, outcome, err)
	}
	if lookupCalls != publishedCommitReachabilityMaxNodes-1 {
		t.Fatalf("lookup calls for 1023-node target = %d, want %d", lookupCalls, publishedCommitReachabilityMaxNodes-1)
	}

	lookupCalls = 0
	outcome, err = classifyPublishedCommitReachability(context.Background(), fmt.Sprintf("c-%d", publishedCommitReachabilityMaxNodes-1), "c-0", publishedCommitReachabilityMaxNodes, lookup)
	if err != nil || outcome != publishedBlockReferenceRepairCommitReachable {
		t.Fatalf("target in node %d = (%v, %v), want REACHABLE", publishedCommitReachabilityMaxNodes, outcome, err)
	}
	if lookupCalls != publishedCommitReachabilityMaxNodes {
		t.Fatalf("lookup calls for 1024-node target = %d, want %d", lookupCalls, publishedCommitReachabilityMaxNodes)
	}

	for _, depth := range []int{publishedCommitReachabilityMaxNodes, publishedCommitReachabilityMaxNodes + 1} {
		lookupCalls = 0
		outcome, err = classifyPublishedCommitReachability(context.Background(), fmt.Sprintf("c-%d", depth), "c-0", publishedCommitReachabilityMaxNodes, lookup)
		if outcome != publishedBlockReferenceRepairCommitUnknown || err == nil || !strings.Contains(err.Error(), "limit") {
			t.Fatalf("target beyond bound at depth %d = (%v, %v), want UNKNOWN limit error", depth, outcome, err)
		}
		if lookupCalls != publishedCommitReachabilityMaxNodes {
			t.Fatalf("lookup calls for depth %d = %d, want bound %d", depth, lookupCalls, publishedCommitReachabilityMaxNodes)
		}
	}
}

func TestWalkPublishedCommitReachabilityTimeoutAdvancesToNextUnread(t *testing.T) {
	parents := map[string]string{"c-0": "c-1", "c-1": "c-2", "c-2": "c-3", "c-3": "c-4"}
	ctx, cancel := context.WithCancel(context.Background())
	lookups := 0
	progress, err := walkPublishedCommitReachability(ctx, "target", "c-0", publishedCommitReachabilityMaxNodes, func(ctx context.Context, commitID string) (string, error) {
		lookups++
		if lookups > 3 {
			cancel()
			return "", ctx.Err()
		}
		parent, ok := parents[commitID]
		if !ok {
			return "", gocql.ErrNotFound
		}
		return parent, nil
	})
	if progress.Outcome != publishedBlockReferenceRepairCommitUnknown || !errors.Is(err, context.Canceled) {
		t.Fatalf("timeout walk = (%v, %v), want UNKNOWN/canceled", progress.Outcome, err)
	}
	if progress.NextCursor != "c-3" {
		t.Fatalf("timeout NextCursor = %q, want next unread c-3", progress.NextCursor)
	}
}

func TestWalkPublishedCommitReachabilityParentErrorAdvancesToUnread(t *testing.T) {
	parents := map[string]string{"c-0": "c-1", "c-1": "c-2"}
	progress, err := walkPublishedCommitReachability(context.Background(), "target", "c-0", publishedCommitReachabilityMaxNodes, func(ctx context.Context, commitID string) (string, error) {
		parent, ok := parents[commitID]
		if !ok {
			return "", gocql.ErrNotFound
		}
		return parent, nil
	})
	if progress.Outcome != publishedBlockReferenceRepairCommitUnknown || err == nil {
		t.Fatalf("parent error walk = (%v, %v), want UNKNOWN/error", progress.Outcome, err)
	}
	if progress.NextCursor != "c-2" {
		t.Fatalf("parent-error NextCursor = %q, want unread/failing c-2", progress.NextCursor)
	}
}

func TestWalkPublishedCommitReachabilityFirstNodeErrorDoesNotAdvance(t *testing.T) {
	progress, err := walkPublishedCommitReachability(context.Background(), "target", "c-0", publishedCommitReachabilityMaxNodes, func(ctx context.Context, commitID string) (string, error) {
		return "", gocql.ErrNotFound
	})
	if progress.Outcome != publishedBlockReferenceRepairCommitUnknown || err == nil {
		t.Fatalf("first-node error = (%v, %v), want UNKNOWN/error", progress.Outcome, err)
	}
	if progress.NextCursor != "" {
		t.Fatalf("first-node error NextCursor = %q, want empty", progress.NextCursor)
	}
}

func TestClassifyPublishedCommitReachabilityCancelledIsUnknown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	lookupCalls := 0
	outcome, err := classifyPublishedCommitReachability(ctx, "target", "head", publishedCommitReachabilityMaxNodes, func(ctx context.Context, commitID string) (string, error) {
		lookupCalls++
		return "", nil
	})
	if outcome != publishedBlockReferenceRepairCommitUnknown || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled classification = (%v, %v), want UNKNOWN/context.Canceled", outcome, err)
	}
	if lookupCalls != 0 {
		t.Fatalf("cancelled classification issued %d parent lookups, want 0", lookupCalls)
	}
}

func TestClassifyPublishedCommitReachabilityExpiredIsUnknown(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	outcome, err := classifyPublishedCommitReachability(ctx, "target", "head", publishedCommitReachabilityMaxNodes, func(ctx context.Context, commitID string) (string, error) {
		t.Fatal("expired classification issued a parent lookup")
		return "", nil
	})
	if outcome != publishedBlockReferenceRepairCommitUnknown || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired classification = (%v, %v), want UNKNOWN/context.DeadlineExceeded", outcome, err)
	}
}

func TestClassifyPublishedCommitReachabilityRetryCanBecomeReachable(t *testing.T) {
	parents := map[string]string{"head": ""}
	lookup := func(ctx context.Context, commitID string) (string, error) {
		parent, ok := parents[commitID]
		if !ok {
			return "", gocql.ErrNotFound
		}
		return parent, nil
	}

	outcome, err := classifyPublishedCommitReachability(context.Background(), "target", "head", publishedCommitReachabilityMaxNodes, lookup)
	if err != nil || outcome != publishedBlockReferenceRepairCommitUnknown {
		t.Fatalf("first observation = (%v, %v), want UNKNOWN without target", outcome, err)
	}
	parents["head"] = "target"
	parents["target"] = ""
	outcome, err = classifyPublishedCommitReachability(context.Background(), "target", "head", publishedCommitReachabilityMaxNodes, lookup)
	if err != nil || outcome != publishedBlockReferenceRepairCommitReachable {
		t.Fatalf("retry observation = (%v, %v), want REACHABLE after target becomes visible", outcome, err)
	}
}

func TestClassifyPublishedBlockReferenceRepairHeadErrorIsUnknown(t *testing.T) {
	oldHead := publishedBlockReferenceRepairHeadCommitFn
	oldParent := publishedBlockReferenceRepairCommitParentFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairHeadCommitFn = oldHead
		publishedBlockReferenceRepairCommitParentFn = oldParent
	})
	wantErr := errors.New("head unavailable")
	publishedBlockReferenceRepairHeadCommitFn = func(ctx context.Context, database *db.DB, orgID, repoID string) (string, error) {
		return "", wantErr
	}
	publishedBlockReferenceRepairCommitParentFn = func(ctx context.Context, database *db.DB, repoID, commitID string) (string, error) {
		t.Fatal("parent lookup must not run after a HEAD read error")
		return "", nil
	}

	outcome, err := classifyPublishedBlockReferenceRepairCommitFromStore(&db.DB{}, "org-1", "repo-1", "target")
	if outcome != publishedBlockReferenceRepairCommitUnknown || !errors.Is(err, wantErr) {
		t.Fatalf("HEAD error classification = (%v, %v), want UNKNOWN/head error", outcome, err)
	}
}

func TestDefinitelyNotReachableWithoutDurableAuthorityRetainsRepair(t *testing.T) {
	promoteCalls := 0
	deleteCalls := 0
	oldPromote := publishedBlockReferenceRepairPromoteFn
	oldDelete := deletePublishedBlockReferenceRepairFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairPromoteFn = oldPromote
		deletePublishedBlockReferenceRepairFn = oldDelete
	})
	publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
		promoteCalls++
		return nil
	}
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		deleteCalls++
		return nil
	}

	repair := newPublishedBlockReferenceRepair("org-1", "repo-1", "commit-1", "fs-1", []string{"block-1"})
	err := settlePublishedBlockReferenceRepair(nil, repair, publishedBlockReferenceRepairCommitDefinitelyNotReachable, nil)
	if err == nil || !strings.Contains(err.Error(), "no durable cleanup authority") {
		t.Fatalf("definitely-not-reachable settlement error = %v, want conservative retention", err)
	}
	if promoteCalls != 0 || deleteCalls != 0 {
		t.Fatalf("definitely-not-reachable changed state: promote=%d delete=%d", promoteCalls, deleteCalls)
	}
}

func TestPublishedBlockReferenceRepairAuthorityReadsAreColdAndExplicit(t *testing.T) {
	raw, err := os.ReadFile("publish_repair.go")
	if err != nil {
		t.Fatalf("read publish_repair.go: %v", err)
	}
	source := strings.ReplaceAll(string(raw), "\r\n", "\n")
	headStart := strings.Index(source, "var publishedBlockReferenceRepairHeadCommitFn")
	parentStart := strings.Index(source, "var publishedBlockReferenceRepairCommitParentFn")
	if headStart < 0 || parentStart <= headStart {
		t.Fatal("could not locate canonical repair HEAD lookup")
	}
	headSource := source[headStart:parentStart]
	if !strings.Contains(headSource, "FROM libraries WHERE org_id = ? AND library_id = ?") {
		t.Fatal("repair HEAD lookup must use the canonical org-scoped libraries row")
	}
	if !strings.Contains(headSource, ".Consistency(gocql.Serial)") {
		t.Fatal("repair HEAD lookup must settle the canonical HEAD in the SERIAL domain")
	}
	if !strings.Contains(headSource, ".WithContext(ctx)") {
		t.Fatal("repair HEAD lookup must share the bounded classification context")
	}
	parentEnd := strings.Index(source[parentStart:], "func classifyPublishedBlockReferenceRepairCommitOutcome")
	if parentEnd < 0 {
		t.Fatal("could not locate repair parent lookup boundary")
	}
	parentSource := source[parentStart : parentStart+parentEnd]
	if !strings.Contains(parentSource, ".Consistency(gocql.EachQuorum)") {
		t.Fatal("repair ancestry lookup must use EachQuorum in the cold path")
	}
	authorityStart := strings.Index(source, "var loadPublishedBlockReferenceRepairAuthorityFn")
	goneStart := strings.Index(source, "func publishedBlockReferenceRepairGoneForCleanup")
	if authorityStart < 0 || goneStart <= authorityStart {
		t.Fatal("could not locate the cleanup authority read")
	}
	authoritySource := source[authorityStart:goneStart]
	if !strings.Contains(authoritySource, "FROM published_block_reference_repairs") || !strings.Contains(authoritySource, "Consistency(gocql.EachQuorum)") {
		t.Fatal("the repair-row absence that authorizes removing repair-owned liveness must be read at EachQuorum, never the session LOCAL_QUORUM")
	}
	sweepStart := strings.Index(source, "func sweepPublishedBlockReferenceRepairLivenessCleanupsGated")
	if sweepStart < 0 {
		t.Fatal("could not locate the cleanup-intent sweep")
	}
	sweepSource := source[sweepStart:]
	if sweepEnd := strings.Index(sweepSource[1:], "\nfunc "); sweepEnd >= 0 {
		sweepSource = sweepSource[:sweepEnd+1]
	}
	if !strings.Contains(sweepSource, "publishedBlockReferenceRepairLivenessCleanupFreezable(intent, now)") || !strings.Contains(sweepSource, "freezePublishedBlockReferenceRepairLivenessCleanupFn(database, intent)") {
		t.Fatal("the cleanup-intent sweep must claim an expired PREPARING intent through the exact-lease freeze CAS before treating it as finished: an expired lease read from a listing is not cleanup authority (the fan-out is renewable)")
	}
	if strings.Contains(sweepSource, "!now.Before(intent.LivenessLeaseExpiresAt)") || strings.Contains(sweepSource, "LivenessLeaseExpiresAt.Before(now)") {
		t.Fatal("the sweep must not decide consumption from the observed lease and the clock; only the freeze CAS decides")
	}
	freezeStart := strings.Index(source, "var freezePublishedBlockReferenceRepairLivenessCleanupFn")
	freezeEnd := strings.Index(source, "var armPublishedBlockReferenceRepairLivenessCleanupFn")
	if freezeStart < 0 || freezeEnd <= freezeStart {
		t.Fatal("could not locate the freeze primitive")
	}
	freezeSource := source[freezeStart:freezeEnd]
	if !strings.Contains(freezeSource, "SET armed = true") || !strings.Contains(freezeSource, "IF armed = false AND lease_expires_at = ?") || !strings.Contains(freezeSource, "MapScanCAS") || !strings.Contains(freezeSource, "SerialConsistency(gocql.Serial)") {
		t.Fatal("the freeze must be a SERIAL LWT on the exact observed lease of a PREPARING intent, so it is mutually exclusive with the producer's EXTEND (same exact lease) and ARM (armed = false)")
	}
	deciderStart := strings.Index(source, "func publishedBlockReferenceRepairGoneForCleanup")
	deciderEnd := strings.Index(source[deciderStart:], "\nfunc ")
	if deciderStart < 0 || deciderEnd < 0 {
		t.Fatal("could not locate the gone decider")
	}
	deciderSource := source[deciderStart : deciderStart+deciderEnd]
	localAt := strings.Index(deciderSource, "publishedBlockReferenceRepairStillPending(database, repair)")
	authorityAt := strings.Index(deciderSource, "loadPublishedBlockReferenceRepairAuthorityFn(database, repair)")
	if localAt < 0 || authorityAt < localAt {
		t.Fatal("the gone decider must use the session read only to retain and escalate every local absence to the EachQuorum authority read")
	}
	if strings.Contains(source, "GoneAfterLocalObservation") {
		t.Fatal("a local observation must never be a destructive decider: the coordinator may have been in another DC")
	}
	insertStart := strings.Index(source, "var insertPublishedBlockReferenceRepairLivenessCleanupFn")
	deleteStart := strings.Index(source, "var deletePublishedBlockReferenceRepairLivenessCleanupFn")
	listStart := strings.Index(source, "var listPublishedBlockReferenceRepairLivenessCleanupsForBucketFn")
	if insertStart < 0 || deleteStart <= insertStart || listStart <= deleteStart {
		t.Fatal("could not locate the cleanup intent primitives")
	}
	insertSource := source[insertStart:deleteStart]
	deleteSource := source[deleteStart:listStart]
	if strings.Contains(insertSource, "USING TTL") {
		t.Fatal("the cleanup intent must not carry a TTL: the renewal fan-out it precedes is not time-bounded, so no margin can guarantee it outlives the pin")
	}
	if !strings.Contains(insertSource, "producer_token") || !strings.Contains(insertSource, "armed, lease_expires_at") || !strings.Contains(deleteSource, "AND producer_token = ?") {
		t.Fatal("the cleanup intent must be written PREPARING with a lease and keyed and deleted per producer token so no other producer can delete a visit's witness")
	}
	insertOnly := source[insertStart:strings.Index(source, "var freezePublishedBlockReferenceRepairLivenessCleanupFn")]
	if !strings.Contains(insertOnly, "IF NOT EXISTS") || !strings.Contains(insertOnly, "MapScanCAS") || !strings.Contains(insertOnly, "SerialConsistency(gocql.Serial)") {
		t.Fatal("the cleanup intent INSERT must be an IF NOT EXISTS LWT: an ordinary INSERT's wall-clock timestamp can shadow the cells a later Paxos ballot (EXTEND/ARM/FREEZE) committed, so a freeze could report applied while the stored row still says PREPARING")
	}
	if !strings.Contains(deleteSource, "IF EXISTS") || !strings.Contains(deleteSource, "MapScanCAS") || !strings.Contains(deleteSource, "SerialConsistency(gocql.Serial)") {
		t.Fatal("the terminal cleanup-intent DELETE must be a SERIAL IF EXISTS LWT: an ordinary DELETE is ordered by the coordinator clock, not by the Paxos ballots that committed ARM/FREEZE/retire, and can be shadowed so the witness lingers")
	}
	retireStart := strings.Index(source, "var retirePublishedBlockReferenceRepairLivenessCleanupFn")
	refenceStart := strings.Index(source, "var refencePublishedBlockReferenceRepairLivenessCleanupFn")
	if retireStart < 0 || refenceStart <= retireStart || deleteStart <= refenceStart {
		t.Fatal("could not locate the retire/re-fence primitives")
	}
	retireSource := source[retireStart:refenceStart]
	if !strings.Contains(retireSource, "SET consumed_at = ?, refenced_at = ?") || !strings.Contains(retireSource, "IF armed = true AND consumed_at = null") || !strings.Contains(retireSource, "MapScanCAS") || strings.Contains(retireSource, "DELETE FROM") {
		t.Fatal("retiring a witness must be a conditional LWT to CONSUMED (armed, not yet consumed; never a phantom row), not a DELETE: the tombstone it leaves is purged after gc_grace and the sweep must keep re-fencing")
	}
	if !strings.Contains(sweepSource, "retirePublishedBlockReferenceRepairLivenessCleanupFn(database, intent, now)") || !strings.Contains(sweepSource, "refencePublishedBlockReferenceRepairLivenessCleanupFn(database, intent, now)") || !strings.Contains(sweepSource, "publishedBlockReferenceRepairLivenessConsumedRetention") || !strings.Contains(sweepSource, "publishedBlockReferenceRepairLivenessRefenceInterval") {
		t.Fatal("the sweep must retire consumed witnesses, re-fence them on the interval and delete them only after the retention")
	}
	finalFenceAt := strings.Index(sweepSource, "// The physical fence, EACH_QUORUM: on any error nothing below runs")
	terminalDeleteAt := strings.Index(sweepSource, "after its final fence")
	if finalFenceAt < 0 || terminalDeleteAt < finalFenceAt {
		t.Fatal("the terminal delete of a retired witness must come after a mandatory final physical fence: the witness is the last cleanup root and a late pin may sit under a tombstone gc_grace already purged")
	}
	armStart := strings.Index(source, "var armPublishedBlockReferenceRepairLivenessCleanupFn")
	extendStart := strings.Index(source, "var extendPublishedBlockReferenceRepairLivenessCleanupFn")
	if armStart < 0 || extendStart <= armStart {
		t.Fatal("could not locate the ARM/extend primitives")
	}
	armSource := source[armStart:extendStart]
	extendSource := source[extendStart:deleteStart]
	if !strings.Contains(armSource, "IF armed = false") || !strings.Contains(armSource, "MapScanCAS") || !strings.Contains(armSource, "SET armed = true, staged_block_ids = ?, lease_expires_at = ?") {
		t.Fatal("ARM must be a conditional LWT on the PREPARING intent that carries the cleanup payload: an unconditional UPDATE resurrects a consumed witness and can expose armed = true without its blocks")
	}
	if !strings.Contains(extendSource, "IF armed = false AND lease_expires_at = ?") || !strings.Contains(extendSource, "MapScanCAS") {
		t.Fatal("lease extension must be a conditional LWT on the exact lease the producer holds")
	}
	renewStart := strings.Index(source, "var renewPublishedBlockReferenceRepairLivenessFn")
	renewEnd := strings.Index(source[renewStart:], "\n}\n")
	if renewStart < 0 || renewEnd < 0 {
		t.Fatal("could not locate the fan-out primitive")
	}
	renewSource := source[renewStart : renewStart+renewEnd]
	if !strings.Contains(renewSource, "writePublishedBlockReferenceRepairLivenessPinFn(") || !strings.Contains(renewSource, "publishedBlockReferenceRepairLeaseTimestamp(repair.LivenessLeaseExpiresAt)") || !strings.Contains(renewSource, "extendPublishedBlockReferenceRepairLivenessCleanupFn(") {
		t.Fatal("every pin must be written with the producer lease as its timestamp and the lease must be renewed inside the fan-out")
	}
	ownedPubStart := strings.Index(source, "var removePublishedBlockReferenceRepairOwnedPubFn")
	if ownedPubStart < 0 || !strings.Contains(source[ownedPubStart:ownedPubStart+900], "db.RemovePublishAttemptReferencesAt(") {
		t.Fatal("repair-owned pin removal must tombstone at the producer lease timestamp so a late write of that producer is shadowed")
	}
	if !strings.Contains(sweepSource, "has no block payload") {
		t.Fatal("the sweep must never consume an intent without its block payload")
	}
	if !strings.Contains(parentSource, ".WithContext(ctx)") {
		t.Fatal("repair ancestry lookup must share the bounded classification context")
	}
	if publishedCommitReachabilityMaxNodes != 1024 || publishedCommitReachabilityTimeout != 30*time.Second {
		t.Fatalf("repair reachability bounds = nodes:%d timeout:%s, want 1024/30s", publishedCommitReachabilityMaxNodes, publishedCommitReachabilityTimeout)
	}
	reanchorStart := strings.Index(source, "func reanchorPublishedBlockReferenceRepairAfterCleanGenesis")
	if reanchorStart < 0 {
		t.Fatal("could not locate clean-genesis re-anchor")
	}
	reanchorEnd := strings.Index(source[reanchorStart:], "\nfunc ")
	if reanchorEnd < 0 {
		t.Fatal("could not bound clean-genesis re-anchor")
	}
	reanchorSource := source[reanchorStart : reanchorStart+reanchorEnd]
	if !strings.Contains(reanchorSource, "walkPublishedCommitReachability") {
		t.Fatal("clean-genesis re-anchor may walk a second 1024-node chunk in the same 30s visit")
	}
	if !strings.Contains(reanchorSource, "ReachabilityAnchorExhausted") {
		t.Fatal("re-anchor CAS loser must not walk a snapshot already marked exhausted")
	}
}

func TestPublishedBlockReferenceRepairSettlementUsesOrdinaryWrites(t *testing.T) {
	raw, err := os.ReadFile("publish_repair.go")
	if err != nil {
		t.Fatalf("read publish_repair.go: %v", err)
	}
	source := string(raw)
	insertStart := strings.Index(source, "var insertPublishedBlockReferenceRepairFn")
	deleteStart := strings.Index(source, "var deletePublishedBlockReferenceRepairFn")
	if insertStart < 0 || deleteStart <= insertStart {
		t.Fatal("could not locate settlement insert helper")
	}
	insertSource := source[insertStart:deleteStart]
	if !strings.Contains(insertSource, "INSERT INTO published_block_reference_repairs") || !strings.Contains(insertSource, "Exec()") {
		t.Fatal("ordinary INSERT must use Exec()")
	}
	if strings.Contains(insertSource, "reachability_anchor_head_commit_id") || strings.Contains(insertSource, "reachability_cursor_commit_id") || strings.Contains(insertSource, "reachability_anchor_exhausted") {
		t.Fatal("ordinary INSERT must not write reachability progress columns")
	}
	if strings.Contains(insertSource, "IF NOT EXISTS") || strings.Contains(insertSource, "ScanCAS") || strings.Contains(insertSource, "MapScanCAS") || strings.Contains(insertSource, "SerialConsistency(gocql.Serial)") {
		t.Fatal("ordinary INSERT must not enter the repair row's Paxos protocol")
	}

	deleteEnd := strings.Index(source, "// schedulePublishedBlockReferenceRepairRetryFn")
	if deleteStart < 0 || deleteEnd <= deleteStart {
		t.Fatal("could not locate settlement delete helper")
	}
	deleteSource := source[deleteStart:deleteEnd]
	if !strings.Contains(deleteSource, "DELETE FROM published_block_reference_repairs") || !strings.Contains(deleteSource, "Exec()") {
		t.Fatal("settlement must use an ordinary delete")
	}
	if strings.Contains(deleteSource, "IF EXISTS") || strings.Contains(deleteSource, "MapScanCAS") || strings.Contains(deleteSource, "SerialConsistency(gocql.Serial)") {
		t.Fatal("settlement must not enter the repair row's Paxos protocol")
	}

	retryStart := strings.Index(source, "var schedulePublishedBlockReferenceRepairRetryFn")
	retryEnd := strings.Index(source, "var listPublishedBlockReferenceRepairsForBucketFn")
	if retryStart < 0 || retryEnd <= retryStart {
		t.Fatal("could not locate retry scheduler helper")
	}
	retrySource := source[retryStart:retryEnd]
	if strings.Contains(retrySource, "published_block_reference_repairs") || strings.Contains(retrySource, "MapScanCAS") || strings.Contains(retrySource, "SerialConsistency(gocql.Serial)") {
		t.Fatal("retry backoff must not mutate the durable repair row or enter Paxos")
	}
	if !strings.Contains(retrySource, "publishedBlockReferenceRepairNextRetryAt.Store") {
		t.Fatal("retry backoff must remain process-local")
	}
}

func TestSchedulePublishedBlockReferenceRepairRetryUsesProcessLocalState(t *testing.T) {
	repair := publishedBlockReferenceRepair{RepoID: "repo-1", CommitID: "commit-1", FSID: "fs-1"}
	key := publishedBlockReferenceRepairRetryKey(repair)
	publishedBlockReferenceRepairNextRetryAt.Delete(key)
	t.Cleanup(func() { publishedBlockReferenceRepairNextRetryAt.Delete(key) })

	nextRetryAt := time.Date(2026, time.May, 29, 12, 5, 0, 0, time.UTC)
	if err := schedulePublishedBlockReferenceRepairRetryFn(&db.DB{}, repair, nextRetryAt); err != nil {
		t.Fatalf("schedule retry = %v", err)
	}
	got, ok := publishedBlockReferenceRepairNextRetryAt.Load(key)
	if !ok || !got.(time.Time).Equal(nextRetryAt) {
		t.Fatalf("local retry state = %#v, want %s", got, nextRetryAt)
	}
}

func TestRunPublishedBlockReferenceRepairSweepPrunesExpiredRetryHintsForMissingRows(t *testing.T) {
	oldNow := publishedBlockReferenceRepairNowFn
	oldList := listPublishedBlockReferenceRepairsForBucketFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairNowFn = oldNow
		listPublishedBlockReferenceRepairsForBucketFn = oldList
	})

	now := time.Date(2026, time.May, 29, 12, 0, 0, 0, time.UTC)
	publishedBlockReferenceRepairNowFn = func() time.Time { return now }
	listPublishedBlockReferenceRepairsForBucketFn = func(database *db.DB, bucket int) ([]publishedBlockReferenceRepair, error) {
		return nil, nil
	}
	repair := publishedBlockReferenceRepair{OrgID: "org-1", RepoID: "repo-1", CommitID: "commit-1", FSID: "fs-1"}
	key := publishedBlockReferenceRepairRetryKey(repair)
	publishedBlockReferenceRepairNextRetryAt.Store(key, now.Add(-time.Second))
	t.Cleanup(func() { publishedBlockReferenceRepairNextRetryAt.Delete(key) })

	if err := runPublishedBlockReferenceRepairSweep(&db.DB{}); err != nil {
		t.Fatalf("empty repair sweep = %v, want nil", err)
	}
	if got, ok := publishedBlockReferenceRepairNextRetryAt.Load(key); ok {
		t.Fatalf("local retry state remains after pruning expired hint: %v", got)
	}
}

func TestPublishedBlockReferenceRepairNeverUsesLeaseExpiryAsCleanupAuthority(t *testing.T) {
	raw, err := os.ReadFile("publish_repair.go")
	if err != nil {
		t.Fatalf("read publish_repair.go: %v", err)
	}
	source := string(raw)
	start := strings.Index(source, "func repairPublishedBlockReferenceRepair")
	end := strings.Index(source[start:], "func runPendingPublishedFSObjectOwnerSweep")
	if start < 0 || end < 0 {
		t.Fatal("could not locate queued repair settlement function")
	}
	settlementSource := source[start : start+end]
	if strings.Contains(settlementSource, "LeaseExpiresAt") || strings.Contains(settlementSource, "publishedBlockReferenceRepairPreCASLease") || strings.Contains(settlementSource, "ShouldDeferCleanup") {
		t.Fatal("lease age must never decide queued repair cleanup")
	}
	if strings.Contains(settlementSource, "publishedBlockReferenceRepairCommitDefinitelyNotPublished") || strings.Contains(settlementSource, "CleanupFailedPublishArtifacts") || !strings.Contains(settlementSource, "default:") || !strings.Contains(settlementSource, "retain queued repair") {
		t.Fatal("queued repair must be fail-closed and retain every non-reachable outcome")
	}
}

func TestPublishedBlockReferenceRepairProgressUsesMonotonicCAS(t *testing.T) {
	raw, err := os.ReadFile("publish_repair.go")
	if err != nil {
		t.Fatalf("read publish_repair.go: %v", err)
	}
	source := string(raw)
	anchorStart := strings.Index(source, "var persistPublishedBlockReferenceRepairAnchorFn")
	advanceStart := strings.Index(source, "var advancePublishedBlockReferenceRepairCursorFn")
	markStart := strings.Index(source, "var markPublishedBlockReferenceRepairAnchorExhaustedFn")
	replaceStart := strings.Index(source, "var replacePublishedBlockReferenceRepairAnchorFn")
	renewStart := strings.Index(source, "var renewPublishedBlockReferenceRepairLivenessFn")
	if anchorStart < 0 || advanceStart <= anchorStart || markStart <= advanceStart || replaceStart <= markStart || renewStart <= replaceStart {
		t.Fatal("could not locate monotonic reachability progress helpers")
	}
	anchorSource := source[anchorStart:advanceStart]
	advanceSource := source[advanceStart:markStart]
	markSource := source[markStart:replaceStart]
	replaceSource := source[replaceStart:renewStart]
	for _, body := range []string{anchorSource, advanceSource, markSource, replaceSource} {
		if !strings.Contains(body, "MapScanCAS") || !strings.Contains(body, "SerialConsistency(gocql.Serial)") {
			t.Fatal("reachability progress must use a SERIAL LWT")
		}
		if strings.Contains(body, "DELETE FROM published_block_reference_repairs") {
			t.Fatal("reachability progress LWT must not delete the repair row")
		}
	}
	if !strings.Contains(anchorSource, "IF created_at = ? AND reachability_anchor_head_commit_id = null") {
		t.Fatal("anchor persist must bind the loaded created_at generation")
	}
	if strings.Contains(anchorSource, "created_at != null") {
		t.Fatal("anchor persist must not treat mere row existence as generation")
	}
	if !strings.Contains(anchorSource, "reachability_anchor_exhausted = false") {
		t.Fatal("anchor persist must clear genesis exhaustion on a new snapshot")
	}
	if !strings.Contains(advanceSource, "IF created_at = ? AND reachability_anchor_head_commit_id = ? AND reachability_cursor_commit_id = ? AND reachability_anchor_exhausted != true") {
		t.Fatal("cursor advance must CAS against the loaded generation and expected snapshot")
	}
	if strings.Contains(advanceSource, "created_at != null") {
		t.Fatal("cursor advance must not treat mere row existence as generation")
	}
	if !strings.Contains(markSource, "SET reachability_anchor_exhausted = true") {
		t.Fatal("genesis exhaustion must persist reachability_anchor_exhausted")
	}
	if !strings.Contains(markSource, "IF created_at = ? AND reachability_anchor_head_commit_id = ? AND reachability_cursor_commit_id = ? AND reachability_anchor_exhausted != true") {
		t.Fatal("genesis exhaustion must CAS against the loaded generation and unexhausted snapshot")
	}
	if strings.Contains(markSource, "created_at != null") {
		t.Fatal("genesis exhaustion must not treat mere row existence as generation")
	}
	if !strings.Contains(replaceSource, "IF created_at = ? AND reachability_anchor_head_commit_id = ? AND reachability_cursor_commit_id = ? AND reachability_anchor_exhausted = true") {
		t.Fatal("genesis re-anchor must CAS against the loaded generation and exhausted snapshot")
	}
	if strings.Contains(replaceSource, "created_at != null") {
		t.Fatal("genesis re-anchor must not treat mere row existence as generation")
	}
	if !strings.Contains(replaceSource, "SET reachability_anchor_head_commit_id = ?, reachability_cursor_commit_id = ?, reachability_anchor_exhausted = false") {
		t.Fatal("genesis re-anchor must replace the exhausted snapshot and clear exhaustion")
	}
}

func TestPublishedBlockReferenceRepairLivenessIdentityIsPerRepairRow(t *testing.T) {
	a := publishedBlockReferenceRepair{RepoID: "repo-1", CommitID: "commit-1", FSID: "fs-a"}
	b := publishedBlockReferenceRepair{RepoID: "repo-1", CommitID: "commit-1", FSID: "fs-b"}
	if publishedBlockReferenceRepairLivenessAttemptID(a) == publishedBlockReferenceRepairLivenessAttemptID(b) {
		t.Fatal("sibling repairs of the same commit must not share pub: identity")
	}
	if publishedBlockReferenceRepairLivenessAttemptID(a) == a.CommitID {
		t.Fatal("repair liveness must not reuse the commit-scoped v2 attempt id")
	}

	raw, err := os.ReadFile("publish_repair.go")
	if err != nil {
		t.Fatalf("read publish_repair.go: %v", err)
	}
	source := string(raw)
	renewStart := strings.Index(source, "var renewPublishedBlockReferenceRepairLivenessFn")
	ifPendingStart := strings.Index(source, "func renewPublishedBlockReferenceRepairLivenessIfPending")
	removeStart := strings.Index(source, "var removePublishedBlockReferenceRepairOwnedLivenessFn")
	retryKeyStart := strings.Index(source, "func publishedBlockReferenceRepairRetryKey")
	if renewStart < 0 || ifPendingStart <= renewStart || removeStart < 0 || retryKeyStart <= removeStart {
		t.Fatal("could not locate repair-owned pub identity helpers")
	}
	renewSource := source[renewStart:ifPendingStart]
	parentStart := strings.Index(source, "func publishedBlockReferenceRepairParentLookup")
	if parentStart <= ifPendingStart {
		t.Fatal("could not locate renew-if-pending body")
	}
	ifPendingSource := source[ifPendingStart:parentStart]
	removeSource := source[removeStart:retryKeyStart]
	if !strings.Contains(renewSource, "publishedBlockReferenceRepairLivenessAttemptID(*repair)") {
		t.Fatal("renewal must use the per-repair pub identity")
	}
	if strings.Contains(renewSource, "repair.RepoID, repair.CommitID, repair.StagedBlockIDs") {
		t.Fatal("renewal must not write pub:<commitID>")
	}
	if !strings.Contains(ifPendingSource, "publishedBlockReferenceRepairLivenessAttemptID(repair)") {
		t.Fatal("renew-after-row-gone compensation must use the per-repair pub identity")
	}
	if strings.Contains(ifPendingSource, "repair.OrgID, repair.CommitID, repair.StagedBlockIDs") {
		t.Fatal("compensation must not delete the commit-scoped v2 attempt")
	}
	if !strings.Contains(removeSource, "removePublishedBlockReferenceRepairOwnedPubFn(database, repair)") {
		t.Fatal("eager repair-owned cleanup must go through the producer-timestamped per-repair removal")
	}
	ownedPubStart := strings.Index(source, "var removePublishedBlockReferenceRepairOwnedPubFn")
	if ownedPubStart < 0 || !strings.Contains(source[ownedPubStart:ownedPubStart+1200], "publishedBlockReferenceRepairLivenessAttemptID(repair)") {
		t.Fatal("eager repair-owned cleanup must use the per-repair pub identity")
	}
}

func TestSettleReachableRepairDoesNotRemoveSiblingRepairPubIdentity(t *testing.T) {
	oldPromote := publishedBlockReferenceRepairPromoteFn
	oldPending := loadPublishedBlockReferenceRepairPendingFileFn
	oldDelete := deletePublishedBlockReferenceRepairFn
	oldRemoveAttempt := cleanupFailedPublishRemoveAttemptReferencesFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairPromoteFn = oldPromote
		loadPublishedBlockReferenceRepairPendingFileFn = oldPending
		deletePublishedBlockReferenceRepairFn = oldDelete
		cleanupFailedPublishRemoveAttemptReferencesFn = oldRemoveAttempt
	})
	publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
		return nil
	}
	loadPublishedBlockReferenceRepairPendingFileFn = func(database *db.DB, repoID, fsID string) (*pendingPublishedFile, error) {
		return &pendingPublishedFile{fsID: fsID}, nil
	}
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		return nil
	}
	var removed []string
	cleanupFailedPublishRemoveAttemptReferencesFn = func(database *db.DB, orgID, attemptID string, blockIDs []string) error {
		removed = append(removed, attemptID)
		return nil
	}

	a := publishedBlockReferenceRepair{
		OrgID:          "org-1",
		RepoID:         "repo-1",
		CommitID:       "commit-1",
		FSID:           "fs-a",
		StagedBlockIDs: []string{"shared-x", "only-a"},
	}
	b := a
	b.FSID = "fs-b"
	b.StagedBlockIDs = []string{"shared-x", "only-b"}
	if err := settlePublishedBlockReferenceRepair(nil, a, publishedBlockReferenceRepairCommitReachable, nil); err != nil {
		t.Fatalf("settle A = %v", err)
	}
	sibling := publishedBlockReferenceRepairLivenessAttemptID(b)
	for _, id := range removed {
		if id == sibling {
			t.Fatal("settling A removed B's repair-owned pub identity")
		}
		if id == a.CommitID {
			t.Fatal("settling A removed the commit-scoped v2 attempt identity via the repair-owned path")
		}
	}
	if len(removed) != 1 || removed[0] != publishedBlockReferenceRepairLivenessAttemptID(a) {
		t.Fatalf("removed = %#v, want only A's repair identity", removed)
	}
}

func TestPersistPublishedBlockReferenceRepairAnchorRequiresLoadedGeneration(t *testing.T) {
	_, err := persistPublishedBlockReferenceRepairAnchorFn(&db.DB{}, publishedBlockReferenceRepair{
		OrgID:    "org-1",
		RepoID:   "repo-1",
		CommitID: "c-1",
		FSID:     "fs-1",
	}, "head-1")
	if err == nil || !strings.Contains(err.Error(), "created_at") {
		t.Fatalf("anchor persist = %v, want loaded created_at", err)
	}
}

type publishedRepairProgressMemory struct {
	mu               sync.Mutex
	createdAt        time.Time
	anchor           string
	cursor           string
	exhausted        bool
	persistCalls     int
	advanceCalls     int
	markCalls        int
	replaceCalls     int
	loadCalls        int
	failPersist      bool
	missing          bool
	missingAfterLoad int
	missingOnCASMiss bool
}

func (m *publishedRepairProgressMemory) matchesGeneration(repair publishedBlockReferenceRepair) bool {
	if m.createdAt.IsZero() {
		return true
	}
	return repair.CreatedAt.Equal(m.createdAt)
}

func (m *publishedRepairProgressMemory) persistAnchor(database *db.DB, repair publishedBlockReferenceRepair, anchorCommitID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.persistCalls++
	if m.failPersist {
		return false, fmt.Errorf("anchor persist crashed")
	}
	if m.missing {
		return false, nil
	}
	if !m.matchesGeneration(repair) {
		return false, nil
	}
	if m.anchor != "" {
		return false, nil
	}
	m.anchor = strings.TrimSpace(anchorCommitID)
	if m.cursor == "" {
		m.cursor = m.anchor
	}
	m.exhausted = false
	return true, nil
}

func (m *publishedRepairProgressMemory) advanceCursor(database *db.DB, repair publishedBlockReferenceRepair, expectedCursor, nextCursor string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.advanceCalls++
	if m.missingOnCASMiss {
		m.missing = true
		return false, nil
	}
	if !m.matchesGeneration(repair) {
		return false, nil
	}
	if m.anchor != strings.TrimSpace(repair.ReachabilityAnchorHeadCommitID) {
		return false, nil
	}
	if m.cursor != strings.TrimSpace(expectedCursor) {
		return false, nil
	}
	if m.exhausted {
		return false, nil
	}
	m.cursor = strings.TrimSpace(nextCursor)
	return true, nil
}

func (m *publishedRepairProgressMemory) markExhausted(database *db.DB, repair publishedBlockReferenceRepair, expectedCursor string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.markCalls++
	if m.missingOnCASMiss {
		m.missing = true
		return false, nil
	}
	if !m.matchesGeneration(repair) {
		return false, nil
	}
	if m.anchor != strings.TrimSpace(repair.ReachabilityAnchorHeadCommitID) {
		return false, nil
	}
	if m.cursor != strings.TrimSpace(expectedCursor) {
		return false, nil
	}
	if m.exhausted {
		return false, nil
	}
	m.exhausted = true
	return true, nil
}

func (m *publishedRepairProgressMemory) replaceAnchor(database *db.DB, repair publishedBlockReferenceRepair, expectedCursor, nextHEAD string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.replaceCalls++
	if m.missingOnCASMiss {
		m.missing = true
		return false, nil
	}
	if !m.matchesGeneration(repair) {
		return false, nil
	}
	if m.anchor != strings.TrimSpace(repair.ReachabilityAnchorHeadCommitID) {
		return false, nil
	}
	if m.cursor != strings.TrimSpace(expectedCursor) {
		return false, nil
	}
	if !m.exhausted {
		return false, nil
	}
	nextHEAD = strings.TrimSpace(nextHEAD)
	m.anchor = nextHEAD
	m.cursor = nextHEAD
	m.exhausted = false
	return true, nil
}

func (m *publishedRepairProgressMemory) load(database *db.DB, repair publishedBlockReferenceRepair) (publishedBlockReferenceRepair, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadCalls++
	if m.missing {
		return publishedBlockReferenceRepair{}, gocql.ErrNotFound
	}
	if m.missingAfterLoad > 0 && m.loadCalls > m.missingAfterLoad {
		m.missing = true
		return publishedBlockReferenceRepair{}, gocql.ErrNotFound
	}
	loaded := repair
	loaded.ReachabilityAnchorHeadCommitID = m.anchor
	loaded.ReachabilityCursorCommitID = m.cursor
	loaded.ReachabilityAnchorExhausted = m.exhausted
	return loaded, nil
}

func (m *publishedRepairProgressMemory) snapshot() (anchor, cursor string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.anchor, m.cursor
}

func (m *publishedRepairProgressMemory) exhaustedSnapshot() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.exhausted
}

func (m *publishedRepairProgressMemory) replaceCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.replaceCalls
}

func TestPublishedRepairProgressMemoryRejectsStaleGenerationAfterRequeue(t *testing.T) {
	genA := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	genB := genA.Add(time.Second)
	memory := &publishedRepairProgressMemory{createdAt: genB}
	stale := publishedBlockReferenceRepair{CreatedAt: genA}
	applied, err := memory.persistAnchor(nil, stale, "h1")
	if err != nil || applied {
		t.Fatalf("stale generation CAS applied=%v err=%v, want miss", applied, err)
	}
}

func linearPublishedCommitParents(depth int) map[string]string {
	parents := make(map[string]string, depth+1)
	for i := 0; i <= depth; i++ {
		current := fmt.Sprintf("c-%d", i)
		if i == depth {
			parents[current] = ""
		} else {
			parents[current] = fmt.Sprintf("c-%d", i+1)
		}
	}
	return parents
}

func installPublishedRepairResumableHooks(t *testing.T, memory *publishedRepairProgressMemory, liveHEAD string, parents map[string]string) *atomic.Int32 {
	t.Helper()
	var headCalls atomic.Int32
	oldHead := publishedBlockReferenceRepairHeadCommitFn
	oldParent := publishedBlockReferenceRepairCommitParentFn
	oldPersist := persistPublishedBlockReferenceRepairAnchorFn
	oldAdvance := advancePublishedBlockReferenceRepairCursorFn
	oldMark := markPublishedBlockReferenceRepairAnchorExhaustedFn
	oldReplace := replacePublishedBlockReferenceRepairAnchorFn
	oldLoad := loadPublishedBlockReferenceRepairFn
	oldAuthority := loadPublishedBlockReferenceRepairAuthorityFn
	oldRenew := renewPublishedBlockReferenceRepairLivenessFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairHeadCommitFn = oldHead
		publishedBlockReferenceRepairCommitParentFn = oldParent
		persistPublishedBlockReferenceRepairAnchorFn = oldPersist
		advancePublishedBlockReferenceRepairCursorFn = oldAdvance
		markPublishedBlockReferenceRepairAnchorExhaustedFn = oldMark
		replacePublishedBlockReferenceRepairAnchorFn = oldReplace
		loadPublishedBlockReferenceRepairFn = oldLoad
		loadPublishedBlockReferenceRepairAuthorityFn = oldAuthority
		renewPublishedBlockReferenceRepairLivenessFn = oldRenew
	})
	publishedBlockReferenceRepairHeadCommitFn = func(ctx context.Context, database *db.DB, orgID, repoID string) (string, error) {
		headCalls.Add(1)
		return liveHEAD, nil
	}
	publishedBlockReferenceRepairCommitParentFn = func(ctx context.Context, database *db.DB, repoID, commitID string) (string, error) {
		parent, ok := parents[commitID]
		if !ok {
			return "", gocql.ErrNotFound
		}
		return parent, nil
	}
	persistPublishedBlockReferenceRepairAnchorFn = memory.persistAnchor
	advancePublishedBlockReferenceRepairCursorFn = memory.advanceCursor
	markPublishedBlockReferenceRepairAnchorExhaustedFn = memory.markExhausted
	replacePublishedBlockReferenceRepairAnchorFn = memory.replaceAnchor
	loadPublishedBlockReferenceRepairFn = memory.load
	loadPublishedBlockReferenceRepairAuthorityFn = memory.load
	renewPublishedBlockReferenceRepairLivenessFn = func(database *db.DB, repair *publishedBlockReferenceRepair) error {
		return nil
	}
	oldOwnedPub := removePublishedBlockReferenceRepairOwnedPubFn
	oldArm := armPublishedBlockReferenceRepairLivenessCleanupFn
	t.Cleanup(func() {
		removePublishedBlockReferenceRepairOwnedPubFn = oldOwnedPub
		armPublishedBlockReferenceRepairLivenessCleanupFn = oldArm
	})
	// Route the timestamped removal through the legacy recorder these tests
	// observe, and let ARM apply without a session.
	removePublishedBlockReferenceRepairOwnedPubFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		return cleanupFailedPublishRemoveAttemptReferencesFn(database, repair.OrgID, publishedBlockReferenceRepairLivenessAttemptID(repair), repair.StagedBlockIDs)
	}
	armPublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, repair publishedBlockReferenceRepair) (bool, error) {
		return true, nil
	}
	return &headCalls
}

func newTestPublishedBlockReferenceRepair(target string) publishedBlockReferenceRepair {
	return publishedBlockReferenceRepair{
		Bucket:         1,
		OrgID:          "org-1",
		RepoID:         "repo-1",
		CommitID:       target,
		FSID:           "fs-1",
		StagedBlockIDs: []string{"block-1"},
	}
}

func TestClassifyPublishedBlockReferenceRepairResumableConvergesPastBound(t *testing.T) {
	memory := &publishedRepairProgressMemory{}
	parents := linearPublishedCommitParents(publishedCommitReachabilityMaxNodes + 2)
	headCalls := installPublishedRepairResumableHooks(t, memory, "c-0", parents)
	target := fmt.Sprintf("c-%d", publishedCommitReachabilityMaxNodes)

	oldPromote := publishedBlockReferenceRepairPromoteFn
	oldDelete := deletePublishedBlockReferenceRepairFn
	oldPending := loadPublishedBlockReferenceRepairPendingFileFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairPromoteFn = oldPromote
		deletePublishedBlockReferenceRepairFn = oldDelete
		loadPublishedBlockReferenceRepairPendingFileFn = oldPending
	})
	promoted := 0
	publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
		promoted++
		return nil
	}
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		return nil
	}
	loadPublishedBlockReferenceRepairPendingFileFn = func(database *db.DB, repoID, fsID string) (*pendingPublishedFile, error) {
		return &pendingPublishedFile{fsID: fsID}, nil
	}

	repair := newTestPublishedBlockReferenceRepair(target)
	err := repairPublishedBlockReferenceRepair(nil, repair)
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("first pass = %v, want UNKNOWN limit error", err)
	}
	_, cursor := memory.snapshot()
	if cursor != target {
		t.Fatalf("first-pass cursor = %q, want %q", cursor, target)
	}
	if headCalls.Load() != 1 || promoted != 0 {
		t.Fatalf("first pass headCalls=%d promoted=%d, want 1/0", headCalls.Load(), promoted)
	}

	err = repairPublishedBlockReferenceRepair(nil, newTestPublishedBlockReferenceRepair(target))
	if err != nil {
		t.Fatalf("second pass = %v, want REACHABLE settlement", err)
	}
	if headCalls.Load() != 1 {
		t.Fatalf("second pass re-read live HEAD (%d calls)", headCalls.Load())
	}
	if promoted != 1 {
		t.Fatalf("promoted = %d, want 1", promoted)
	}
}

func TestClassifyPublishedBlockReferenceRepairResumableIgnoresMovingHEAD(t *testing.T) {
	memory := &publishedRepairProgressMemory{}
	parents := linearPublishedCommitParents(publishedCommitReachabilityMaxNodes*2 + 8)
	liveHEAD := "c-0"
	headCalls := installPublishedRepairResumableHooks(t, memory, liveHEAD, parents)
	publishedBlockReferenceRepairHeadCommitFn = func(ctx context.Context, database *db.DB, orgID, repoID string) (string, error) {
		headCalls.Add(1)
		return liveHEAD, nil
	}
	target := fmt.Sprintf("c-%d", publishedCommitReachabilityMaxNodes*2+4)
	repair := newTestPublishedBlockReferenceRepair(target)

	if err := repairPublishedBlockReferenceRepair(nil, repair); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("first pass = %v, want UNKNOWN limit", err)
	}
	liveHEAD = "c-moved-thousands"
	parents["c-moved-thousands"] = "c-moved-thousands-1"
	for i := 1; i < 3000; i++ {
		parents[fmt.Sprintf("c-moved-thousands-%d", i)] = fmt.Sprintf("c-moved-thousands-%d", i+1)
	}

	if err := repairPublishedBlockReferenceRepair(nil, newTestPublishedBlockReferenceRepair(target)); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("second pass = %v, want continued UNKNOWN from cursor", err)
	}
	if headCalls.Load() != 1 {
		t.Fatalf("moving HEAD was re-observed: headCalls=%d", headCalls.Load())
	}
	_, cursor := memory.snapshot()
	if cursor == liveHEAD || strings.HasPrefix(cursor, "c-moved") {
		t.Fatalf("cursor reset to live HEAD %q", cursor)
	}

	oldPromote := publishedBlockReferenceRepairPromoteFn
	oldDelete := deletePublishedBlockReferenceRepairFn
	oldPending := loadPublishedBlockReferenceRepairPendingFileFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairPromoteFn = oldPromote
		deletePublishedBlockReferenceRepairFn = oldDelete
		loadPublishedBlockReferenceRepairPendingFileFn = oldPending
	})
	publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
		return nil
	}
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		return nil
	}
	loadPublishedBlockReferenceRepairPendingFileFn = func(database *db.DB, repoID, fsID string) (*pendingPublishedFile, error) {
		return &pendingPublishedFile{fsID: fsID}, nil
	}
	if err := repairPublishedBlockReferenceRepair(nil, newTestPublishedBlockReferenceRepair(target)); err != nil {
		t.Fatalf("third pass = %v, want REACHABLE under a moved HEAD", err)
	}
	if headCalls.Load() != 1 {
		t.Fatalf("reachable pass re-read live HEAD (%d calls)", headCalls.Load())
	}
}

func TestClassifyPublishedBlockReferenceRepairResumablePreHEADAnchorCanReanchorAfterPublish(t *testing.T) {
	memory := &publishedRepairProgressMemory{}
	parents := map[string]string{"h0": ""}
	liveHEAD := "h0"
	headCalls := installPublishedRepairResumableHooks(t, memory, liveHEAD, parents)
	publishedBlockReferenceRepairHeadCommitFn = func(ctx context.Context, database *db.DB, orgID, repoID string) (string, error) {
		headCalls.Add(1)
		return liveHEAD, nil
	}
	oldPromote := publishedBlockReferenceRepairPromoteFn
	oldDelete := deletePublishedBlockReferenceRepairFn
	oldPending := loadPublishedBlockReferenceRepairPendingFileFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairPromoteFn = oldPromote
		deletePublishedBlockReferenceRepairFn = oldDelete
		loadPublishedBlockReferenceRepairPendingFileFn = oldPending
	})
	promoted := 0
	publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
		promoted++
		return nil
	}
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		return nil
	}
	loadPublishedBlockReferenceRepairPendingFileFn = func(database *db.DB, repoID, fsID string) (*pendingPublishedFile, error) {
		return &pendingPublishedFile{fsID: fsID}, nil
	}

	target := "t"
	err := repairPublishedBlockReferenceRepair(nil, newTestPublishedBlockReferenceRepair(target))
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("pre-HEAD repair = %v, want UNKNOWN retain", err)
	}
	anchor, cursor := memory.snapshot()
	if anchor != "h0" || cursor != "h0" {
		t.Fatalf("pre-HEAD snapshot = %q/%q, want h0/h0", anchor, cursor)
	}
	if memory.replaceCount() != 0 {
		t.Fatalf("pre-HEAD replaceCalls = %d, want 0", memory.replaceCount())
	}
	if promoted != 0 {
		t.Fatalf("pre-HEAD promoted = %d, want 0", promoted)
	}
	headsAfterPreHEAD := headCalls.Load()

	liveHEAD = target
	parents[target] = "h0"
	if err := repairPublishedBlockReferenceRepair(nil, newTestPublishedBlockReferenceRepair(target)); err != nil {
		t.Fatalf("recovery after publish = %v, want REACHABLE after re-anchor", err)
	}
	anchor, cursor = memory.snapshot()
	if anchor != target || cursor != target {
		t.Fatalf("recovery snapshot = %q/%q, want t/t", anchor, cursor)
	}
	if memory.replaceCount() != 1 {
		t.Fatalf("recovery replaceCalls = %d, want 1", memory.replaceCount())
	}
	if promoted != 1 {
		t.Fatalf("recovery promoted = %d, want 1", promoted)
	}
	if headCalls.Load() <= headsAfterPreHEAD {
		t.Fatalf("recovery did not re-observe SERIAL HEAD after genesis: headCalls=%d first=%d", headCalls.Load(), headsAfterPreHEAD)
	}
}

func TestClassifyPublishedBlockReferenceRepairResumableGenesisExhaustionSurvivesHEADDeadline(t *testing.T) {
	memory := &publishedRepairProgressMemory{anchor: "h0", cursor: "c-near"}
	parents := map[string]string{"c-near": "", "t": "h0"}
	liveHEAD := "h0"
	failHEAD := true
	parentReads := map[string]int{}
	headCalls := installPublishedRepairResumableHooks(t, memory, liveHEAD, parents)
	publishedBlockReferenceRepairHeadCommitFn = func(ctx context.Context, database *db.DB, orgID, repoID string) (string, error) {
		headCalls.Add(1)
		if failHEAD {
			return "", fmt.Errorf("lookup canonical HEAD for repo %s: %w", repoID, context.DeadlineExceeded)
		}
		return liveHEAD, nil
	}
	publishedBlockReferenceRepairCommitParentFn = func(ctx context.Context, database *db.DB, repoID, commitID string) (string, error) {
		parentReads[commitID]++
		parent, ok := parents[commitID]
		if !ok {
			return "", gocql.ErrNotFound
		}
		return parent, nil
	}

	oldPromote := publishedBlockReferenceRepairPromoteFn
	oldDelete := deletePublishedBlockReferenceRepairFn
	oldPending := loadPublishedBlockReferenceRepairPendingFileFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairPromoteFn = oldPromote
		deletePublishedBlockReferenceRepairFn = oldDelete
		loadPublishedBlockReferenceRepairPendingFileFn = oldPending
	})
	promoted := 0
	publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
		promoted++
		return nil
	}
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		return nil
	}
	loadPublishedBlockReferenceRepairPendingFileFn = func(database *db.DB, repoID, fsID string) (*pendingPublishedFile, error) {
		return &pendingPublishedFile{fsID: fsID}, nil
	}

	repair := publishedBlockReferenceRepair{
		Bucket:                         1,
		OrgID:                          "org-1",
		RepoID:                         "repo-1",
		CommitID:                       "t",
		FSID:                           "fs-1",
		StagedBlockIDs:                 []string{"block-1"},
		ReachabilityAnchorHeadCommitID: "h0",
		ReachabilityCursorCommitID:     "c-near",
	}
	err := repairPublishedBlockReferenceRepair(nil, repair)
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("HEAD deadline after genesis = %v, want UNKNOWN deadline", err)
	}
	if !memory.exhaustedSnapshot() {
		t.Fatal("clean genesis must persist exhausted progress before the HEAD re-read")
	}
	if parentReads["c-near"] != 1 {
		t.Fatalf("first-pass parentReads[c-near]=%d, want 1", parentReads["c-near"])
	}
	_, cursor := memory.snapshot()
	if cursor != "c-near" {
		t.Fatalf("cursor = %q, want c-near on the exhausted snapshot", cursor)
	}
	if promoted != 0 {
		t.Fatalf("deadline promoted = %d, want 0", promoted)
	}
	readsAfterDeadline := parentReads["c-near"]

	failHEAD = false
	liveHEAD = "t"
	if err := repairPublishedBlockReferenceRepair(nil, publishedBlockReferenceRepair{
		Bucket:                         1,
		OrgID:                          "org-1",
		RepoID:                         "repo-1",
		CommitID:                       "t",
		FSID:                           "fs-1",
		StagedBlockIDs:                 []string{"block-1"},
		ReachabilityAnchorHeadCommitID: "h0",
		ReachabilityCursorCommitID:     "c-near",
	}); err != nil {
		t.Fatalf("recovery after durable genesis = %v, want REACHABLE after re-anchor", err)
	}
	if parentReads["c-near"] != readsAfterDeadline {
		t.Fatalf("retry replayed exhausted snapshot: parent reads of c-near = %d, want %d", parentReads["c-near"], readsAfterDeadline)
	}
	if parentReads["t"] == 0 {
		t.Fatal("re-anchor walk did not observe the new HEAD")
	}
	if promoted != 1 {
		t.Fatalf("recovery promoted = %d, want 1", promoted)
	}
}

func TestClassifyPublishedBlockReferenceRepairResumableRestartContinuesFromCursor(t *testing.T) {
	memory := &publishedRepairProgressMemory{}
	parents := linearPublishedCommitParents(publishedCommitReachabilityMaxNodes + 2)
	headCalls := installPublishedRepairResumableHooks(t, memory, "c-0", parents)
	target := fmt.Sprintf("c-%d", publishedCommitReachabilityMaxNodes)
	if err := repairPublishedBlockReferenceRepair(nil, newTestPublishedBlockReferenceRepair(target)); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("first chunk = %v, want UNKNOWN limit", err)
	}
	restarted := newTestPublishedBlockReferenceRepair(target)
	if restarted.ReachabilityCursorCommitID != "" || restarted.ReachabilityAnchorHeadCommitID != "" {
		t.Fatal("restart struct must not carry process-local progress")
	}
	oldPromote := publishedBlockReferenceRepairPromoteFn
	oldDelete := deletePublishedBlockReferenceRepairFn
	oldPending := loadPublishedBlockReferenceRepairPendingFileFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairPromoteFn = oldPromote
		deletePublishedBlockReferenceRepairFn = oldDelete
		loadPublishedBlockReferenceRepairPendingFileFn = oldPending
	})
	publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
		return nil
	}
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		return nil
	}
	loadPublishedBlockReferenceRepairPendingFileFn = func(database *db.DB, repoID, fsID string) (*pendingPublishedFile, error) {
		return &pendingPublishedFile{fsID: fsID}, nil
	}
	if err := repairPublishedBlockReferenceRepair(nil, restarted); err != nil {
		t.Fatalf("restarted chunk = %v, want REACHABLE from durable cursor", err)
	}
	if headCalls.Load() != 1 {
		t.Fatalf("restart re-read HEAD (%d calls)", headCalls.Load())
	}
}

func TestClassifyPublishedBlockReferenceRepairResumableCrashBeforeAnchorPersist(t *testing.T) {
	memory := &publishedRepairProgressMemory{failPersist: true}
	parents := linearPublishedCommitParents(4)
	headCalls := installPublishedRepairResumableHooks(t, memory, "c-0", parents)
	err := repairPublishedBlockReferenceRepair(nil, newTestPublishedBlockReferenceRepair("c-2"))
	if err == nil || !strings.Contains(err.Error(), "anchor persist crashed") {
		t.Fatalf("crash before persist = %v, want persist error", err)
	}
	anchor, cursor := memory.snapshot()
	if anchor != "" || cursor != "" {
		t.Fatalf("progress leaked after failed persist: anchor=%q cursor=%q", anchor, cursor)
	}
	memory.failPersist = false
	oldPromote := publishedBlockReferenceRepairPromoteFn
	oldDelete := deletePublishedBlockReferenceRepairFn
	oldPending := loadPublishedBlockReferenceRepairPendingFileFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairPromoteFn = oldPromote
		deletePublishedBlockReferenceRepairFn = oldDelete
		loadPublishedBlockReferenceRepairPendingFileFn = oldPending
	})
	publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
		return nil
	}
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		return nil
	}
	loadPublishedBlockReferenceRepairPendingFileFn = func(database *db.DB, repoID, fsID string) (*pendingPublishedFile, error) {
		return &pendingPublishedFile{fsID: fsID}, nil
	}
	if err := repairPublishedBlockReferenceRepair(nil, newTestPublishedBlockReferenceRepair("c-2")); err != nil {
		t.Fatalf("retry after persist crash = %v, want REACHABLE", err)
	}
	if headCalls.Load() != 2 {
		t.Fatalf("safe retry after missing persist must re-read HEAD, got %d", headCalls.Load())
	}
}

func TestClassifyPublishedBlockReferenceRepairResumableCrashAfterCursorPersist(t *testing.T) {
	memory := &publishedRepairProgressMemory{}
	parents := linearPublishedCommitParents(publishedCommitReachabilityMaxNodes + 2)
	headCalls := installPublishedRepairResumableHooks(t, memory, "c-0", parents)
	target := fmt.Sprintf("c-%d", publishedCommitReachabilityMaxNodes)
	if err := repairPublishedBlockReferenceRepair(nil, newTestPublishedBlockReferenceRepair(target)); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("first chunk = %v, want persisted UNKNOWN", err)
	}
	if memory.advanceCalls != 1 {
		t.Fatalf("advanceCalls = %d, want 1", memory.advanceCalls)
	}
	oldPromote := publishedBlockReferenceRepairPromoteFn
	oldDelete := deletePublishedBlockReferenceRepairFn
	oldPending := loadPublishedBlockReferenceRepairPendingFileFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairPromoteFn = oldPromote
		deletePublishedBlockReferenceRepairFn = oldDelete
		loadPublishedBlockReferenceRepairPendingFileFn = oldPending
	})
	publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
		return nil
	}
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		return nil
	}
	loadPublishedBlockReferenceRepairPendingFileFn = func(database *db.DB, repoID, fsID string) (*pendingPublishedFile, error) {
		return &pendingPublishedFile{fsID: fsID}, nil
	}
	if err := repairPublishedBlockReferenceRepair(nil, newTestPublishedBlockReferenceRepair(target)); err != nil {
		t.Fatalf("retry after cursor persist = %v, want REACHABLE", err)
	}
	if headCalls.Load() != 1 {
		t.Fatalf("retry after cursor persist re-read HEAD (%d calls)", headCalls.Load())
	}
}

func TestClassifyPublishedBlockReferenceRepairResumableParentErrorAdvancesToUnread(t *testing.T) {
	memory := &publishedRepairProgressMemory{anchor: "c-0", cursor: "c-0"}
	parents := map[string]string{"c-0": "c-1"}
	installPublishedRepairResumableHooks(t, memory, "other", parents)
	deleteCalls := 0
	oldDelete := deletePublishedBlockReferenceRepairFn
	oldPromote := publishedBlockReferenceRepairPromoteFn
	t.Cleanup(func() {
		deletePublishedBlockReferenceRepairFn = oldDelete
		publishedBlockReferenceRepairPromoteFn = oldPromote
	})
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		deleteCalls++
		return nil
	}
	publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
		t.Fatal("parent error must not promote")
		return nil
	}
	err := repairPublishedBlockReferenceRepair(nil, publishedBlockReferenceRepair{
		Bucket:                         1,
		OrgID:                          "org-1",
		RepoID:                         "repo-1",
		CommitID:                       "target",
		FSID:                           "fs-1",
		StagedBlockIDs:                 []string{"block-1"},
		ReachabilityAnchorHeadCommitID: "c-0",
		ReachabilityCursorCommitID:     "c-0",
	})
	if err == nil || !strings.Contains(err.Error(), "lookup parent") {
		t.Fatalf("parent error = %v, want UNKNOWN retain", err)
	}
	_, cursor := memory.snapshot()
	if cursor != "c-1" {
		t.Fatalf("cursor = %q, want unread/failing c-1", cursor)
	}
	if deleteCalls != 0 {
		t.Fatalf("parent error deleted repair (%d)", deleteCalls)
	}
}

func TestClassifyPublishedBlockReferenceRepairResumableCycleAndMalformedRetain(t *testing.T) {
	for _, tt := range []struct {
		name    string
		parents map[string]string
		needle  string
	}{
		{name: "cycle", parents: map[string]string{"c-0": "c-1", "c-1": "c-0"}, needle: "cycle"},
		{name: "malformed", parents: map[string]string{"c-0": " \t"}, needle: "malformed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			memory := &publishedRepairProgressMemory{anchor: "c-0", cursor: "c-0"}
			installPublishedRepairResumableHooks(t, memory, "ignored-head", tt.parents)
			deleteCalls := 0
			oldDelete := deletePublishedBlockReferenceRepairFn
			t.Cleanup(func() { deletePublishedBlockReferenceRepairFn = oldDelete })
			deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
				deleteCalls++
				return nil
			}
			err := repairPublishedBlockReferenceRepair(nil, publishedBlockReferenceRepair{
				Bucket:                         1,
				OrgID:                          "org-1",
				RepoID:                         "repo-1",
				CommitID:                       "target",
				FSID:                           "fs-1",
				StagedBlockIDs:                 []string{"block-1"},
				ReachabilityAnchorHeadCommitID: "c-0",
				ReachabilityCursorCommitID:     "c-0",
			})
			if err == nil || !strings.Contains(err.Error(), tt.needle) {
				t.Fatalf("error = %v, want %s", err, tt.needle)
			}
			_, cursor := memory.snapshot()
			if cursor != "c-0" {
				t.Fatalf("cursor advanced on %s: %q", tt.name, cursor)
			}
			if deleteCalls != 0 {
				t.Fatalf("%s deleted repair", tt.name)
			}
		})
	}
}

func TestClassifyPublishedBlockReferenceRepairResumableRootIsNotNegativeAuthority(t *testing.T) {
	memory := &publishedRepairProgressMemory{anchor: "other", cursor: "other"}
	parents := map[string]string{"other": "other-root", "other-root": ""}
	installPublishedRepairResumableHooks(t, memory, "other", parents)
	deleteCalls := 0
	oldDelete := deletePublishedBlockReferenceRepairFn
	t.Cleanup(func() { deletePublishedBlockReferenceRepairFn = oldDelete })
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		deleteCalls++
		return nil
	}
	repair := publishedBlockReferenceRepair{
		Bucket:                         1,
		OrgID:                          "org-1",
		RepoID:                         "repo-1",
		CommitID:                       "target",
		FSID:                           "fs-1",
		StagedBlockIDs:                 []string{"block-1"},
		ReachabilityAnchorHeadCommitID: "other",
		ReachabilityCursorCommitID:     "other",
	}
	outcome, err := classifyPublishedBlockReferenceRepairCommitResumable(nil, &repair)
	if outcome != publishedBlockReferenceRepairCommitUnknown || err != nil {
		t.Fatalf("root without target = (%v, %v), want UNKNOWN/nil", outcome, err)
	}
	if outcome == publishedBlockReferenceRepairCommitDefinitelyNotReachable {
		t.Fatal("root without target must not become DEFINITELY_NOT_REACHABLE")
	}
	err = repairPublishedBlockReferenceRepair(nil, repair)
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("root settlement = %v, want retain", err)
	}
	if deleteCalls != 0 {
		t.Fatal("root without target deleted the repair")
	}
}

func TestClassifyPublishedBlockReferenceRepairResumableConcurrentWorkersDoNotRegress(t *testing.T) {
	memory := &publishedRepairProgressMemory{}
	parents := linearPublishedCommitParents(publishedCommitReachabilityMaxNodes + 2)
	installPublishedRepairResumableHooks(t, memory, "c-0", parents)
	target := fmt.Sprintf("c-%d", publishedCommitReachabilityMaxNodes)
	oldPromote := publishedBlockReferenceRepairPromoteFn
	oldDelete := deletePublishedBlockReferenceRepairFn
	oldPending := loadPublishedBlockReferenceRepairPendingFileFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairPromoteFn = oldPromote
		deletePublishedBlockReferenceRepairFn = oldDelete
		loadPublishedBlockReferenceRepairPendingFileFn = oldPending
	})
	var promoted atomic.Int32
	publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
		promoted.Add(1)
		return nil
	}
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		return nil
	}
	loadPublishedBlockReferenceRepairPendingFileFn = func(database *db.DB, repoID, fsID string) (*pendingPublishedFile, error) {
		return &pendingPublishedFile{fsID: fsID}, nil
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- repairPublishedBlockReferenceRepair(nil, newTestPublishedBlockReferenceRepair(target))
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err == nil {
			continue
		}
		if !strings.Contains(err.Error(), "limit") {
			t.Fatalf("concurrent first chunk = %v, want UNKNOWN limit or true REACHABLE", err)
		}
	}
	_, cursor := memory.snapshot()
	if cursor != target && promoted.Load() == 0 {
		t.Fatalf("concurrent cursor = %q promoted=%d, want monotonic progress", cursor, promoted.Load())
	}
}

func TestReanchorPublishedBlockReferenceRepairDoesNotReplayExhaustedLoserSnapshot(t *testing.T) {
	memory := &publishedRepairProgressMemory{anchor: "h1", cursor: "h1", exhausted: true}
	parents := map[string]string{
		"h0":        "",
		"h1":        "h1-parent",
		"h1-parent": "",
		"h2":        "target",
		"target":    "",
	}
	parentReads := map[string]int{}
	installPublishedRepairResumableHooks(t, memory, "h2", parents)
	publishedBlockReferenceRepairCommitParentFn = func(ctx context.Context, database *db.DB, repoID, commitID string) (string, error) {
		parentReads[commitID]++
		parent, ok := parents[commitID]
		if !ok {
			return "", gocql.ErrNotFound
		}
		return parent, nil
	}

	repair := newTestPublishedBlockReferenceRepair("target")
	repair.ReachabilityAnchorHeadCommitID = "h0"
	repair.ReachabilityCursorCommitID = "h0"
	repair.ReachabilityAnchorExhausted = true

	outcome, err := classifyPublishedBlockReferenceRepairCommitResumable(nil, &repair)
	if err != nil {
		t.Fatalf("re-anchor loser = %v, want REACHABLE on the newer HEAD", err)
	}
	if parentReads["h1"] != 0 || parentReads["h1-parent"] != 0 {
		t.Fatalf("re-anchor loser replayed exhausted snapshot: h1=%d h1-parent=%d", parentReads["h1"], parentReads["h1-parent"])
	}
	if outcome != publishedBlockReferenceRepairCommitReachable {
		t.Fatalf("re-anchor loser outcome = %v, want REACHABLE after replacing the exhausted winner snapshot", outcome)
	}
	if parentReads["h2"] == 0 || parentReads["target"] == 0 {
		t.Fatalf("re-anchor loser did not walk the live HEAD: h2=%d target=%d", parentReads["h2"], parentReads["target"])
	}
	anchor, _ := memory.snapshot()
	if anchor != "h2" {
		t.Fatalf("durable anchor = %q, want h2", anchor)
	}
}

func TestRepairPublishedBlockReferenceRepairRenewFailureRetainsRow(t *testing.T) {
	oldClassify := publishedBlockReferenceRepairClassifyFn
	oldRenew := renewPublishedBlockReferenceRepairLivenessFn
	oldDelete := deletePublishedBlockReferenceRepairFn
	oldPromote := publishedBlockReferenceRepairPromoteFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairClassifyFn = oldClassify
		renewPublishedBlockReferenceRepairLivenessFn = oldRenew
		deletePublishedBlockReferenceRepairFn = oldDelete
		publishedBlockReferenceRepairPromoteFn = oldPromote
	})
	classifyCalls := 0
	publishedBlockReferenceRepairClassifyFn = func(database *db.DB, repair *publishedBlockReferenceRepair) (publishedBlockReferenceRepairCommitOutcome, error) {
		classifyCalls++
		return publishedBlockReferenceRepairCommitUnknown, nil
	}
	renewPublishedBlockReferenceRepairLivenessFn = func(database *db.DB, repair *publishedBlockReferenceRepair) error {
		return fmt.Errorf("renew failed")
	}
	deleteCalls := 0
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		deleteCalls++
		return nil
	}
	publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
		t.Fatal("renew failure must not promote")
		return nil
	}
	err := repairPublishedBlockReferenceRepair(nil, newTestPublishedBlockReferenceRepair("commit-1"))
	if err == nil || !strings.Contains(err.Error(), "renew failed") {
		t.Fatalf("renew failure = %v, want the renewal error retained for retry", err)
	}
	if classifyCalls != 0 {
		t.Fatalf("classifyCalls = %d, want 0: the walk must not start without the pre-classify renewal", classifyCalls)
	}
	if deleteCalls != 0 {
		t.Fatal("renew failure deleted the repair row")
	}
}

func TestHydratePublishedBlockReferenceRepairMissingRowIsGone(t *testing.T) {
	oldLoad := loadPublishedBlockReferenceRepairFn
	t.Cleanup(func() { loadPublishedBlockReferenceRepairFn = oldLoad })
	loadPublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) (publishedBlockReferenceRepair, error) {
		return publishedBlockReferenceRepair{}, gocql.ErrNotFound
	}
	_, err := hydratePublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1"))
	if !errors.Is(err, errPublishedBlockReferenceRepairGone) {
		t.Fatalf("hydrate missing row = %v, want gone", err)
	}
}

func TestRepairPublishedBlockReferenceRepairMissingRowBeforeHydrateIsNoOp(t *testing.T) {
	memory := &publishedRepairProgressMemory{missing: true}
	headCalls := installPublishedRepairResumableHooks(t, memory, "c-0", linearPublishedCommitParents(4))
	renewCalls := 0
	promoteCalls := 0
	oldRenew := renewPublishedBlockReferenceRepairLivenessFn
	oldPromote := publishedBlockReferenceRepairPromoteFn
	oldDelete := deletePublishedBlockReferenceRepairFn
	t.Cleanup(func() {
		renewPublishedBlockReferenceRepairLivenessFn = oldRenew
		publishedBlockReferenceRepairPromoteFn = oldPromote
		deletePublishedBlockReferenceRepairFn = oldDelete
	})
	renewPublishedBlockReferenceRepairLivenessFn = func(database *db.DB, repair *publishedBlockReferenceRepair) error {
		renewCalls++
		return nil
	}
	publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
		promoteCalls++
		return nil
	}
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		t.Fatal("missing repair row must not delete")
		return nil
	}
	if err := repairPublishedBlockReferenceRepair(nil, newTestPublishedBlockReferenceRepair("c-2")); err != nil {
		t.Fatalf("missing-row hydrate = %v, want nil no-op", err)
	}
	if headCalls.Load() != 0 || renewCalls != 0 || promoteCalls != 0 {
		t.Fatalf("missing-row work continued: head=%d renew=%d promote=%d", headCalls.Load(), renewCalls, promoteCalls)
	}
}

func TestClassifyPublishedBlockReferenceRepairCASMissOnGoneRowIsNotReachable(t *testing.T) {
	memory := &publishedRepairProgressMemory{missingOnCASMiss: true}
	parents := linearPublishedCommitParents(publishedCommitReachabilityMaxNodes + 2)
	installPublishedRepairResumableHooks(t, memory, "c-0", parents)
	target := fmt.Sprintf("c-%d", publishedCommitReachabilityMaxNodes)
	promoteCalls := 0
	renewCalls := 0
	removeCalls := 0
	oldPromote := publishedBlockReferenceRepairPromoteFn
	oldDelete := deletePublishedBlockReferenceRepairFn
	oldPending := loadPublishedBlockReferenceRepairPendingFileFn
	oldRenew := renewPublishedBlockReferenceRepairLivenessFn
	oldRemove := cleanupFailedPublishRemoveAttemptReferencesFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairPromoteFn = oldPromote
		deletePublishedBlockReferenceRepairFn = oldDelete
		loadPublishedBlockReferenceRepairPendingFileFn = oldPending
		renewPublishedBlockReferenceRepairLivenessFn = oldRenew
		cleanupFailedPublishRemoveAttemptReferencesFn = oldRemove
	})
	publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
		promoteCalls++
		return nil
	}
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		return nil
	}
	loadPublishedBlockReferenceRepairPendingFileFn = func(database *db.DB, repoID, fsID string) (*pendingPublishedFile, error) {
		return &pendingPublishedFile{fsID: fsID}, nil
	}
	renewPublishedBlockReferenceRepairLivenessFn = func(database *db.DB, repair *publishedBlockReferenceRepair) error {
		renewCalls++
		return nil
	}
	cleanupFailedPublishRemoveAttemptReferencesFn = func(database *db.DB, orgID, attemptID string, blockIDs []string) error {
		removeCalls++
		if attemptID != publishedBlockReferenceRepairLivenessAttemptID(newTestPublishedBlockReferenceRepair(target)) {
			t.Fatalf("removed %q, want the per-repair identity", attemptID)
		}
		return nil
	}
	if err := repairPublishedBlockReferenceRepair(nil, newTestPublishedBlockReferenceRepair(target)); err != nil {
		t.Fatalf("CAS miss on gone row = %v, want nil no-op", err)
	}
	if promoteCalls != 0 {
		t.Fatalf("gone row was treated as REACHABLE: promote=%d", promoteCalls)
	}
	// The row was live through hydrate and both StillPending reads, so the
	// single pre-classify renewal is correct. It vanished during the walk
	// (an ordinary writer settlement deletes only the row), so the pin this
	// visit wrote must not be left ownerless until its TTL.
	if renewCalls != 1 {
		t.Fatalf("renewCalls = %d, want exactly the pre-classify renewal", renewCalls)
	}
	if removeCalls != 1 {
		t.Fatalf("removeCalls = %d, want the pub: written before the walk removed once the row was gone", removeCalls)
	}
}

func TestRepairPublishedBlockReferenceRepairGoneBeforeRenewDoesNotRenewOrWalk(t *testing.T) {
	// hydrate is the only load that sees the row; the StillPending read that
	// guards the pre-classify renewal finds it settled.
	memory := &publishedRepairProgressMemory{anchor: "other", cursor: "other", missingAfterLoad: 1}
	parents := map[string]string{"other": "other-root", "other-root": ""}
	headCalls := installPublishedRepairResumableHooks(t, memory, "other", parents)
	renewCalls := 0
	oldRenew := renewPublishedBlockReferenceRepairLivenessFn
	oldDelete := deletePublishedBlockReferenceRepairFn
	t.Cleanup(func() {
		renewPublishedBlockReferenceRepairLivenessFn = oldRenew
		deletePublishedBlockReferenceRepairFn = oldDelete
	})
	renewPublishedBlockReferenceRepairLivenessFn = func(database *db.DB, repair *publishedBlockReferenceRepair) error {
		renewCalls++
		return nil
	}
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		t.Fatal("gone row must not delete")
		return nil
	}
	err := repairPublishedBlockReferenceRepair(nil, publishedBlockReferenceRepair{
		Bucket:                         1,
		OrgID:                          "org-1",
		RepoID:                         "repo-1",
		CommitID:                       "target",
		FSID:                           "fs-1",
		StagedBlockIDs:                 []string{"block-1"},
		ReachabilityAnchorHeadCommitID: "other",
		ReachabilityCursorCommitID:     "other",
	})
	if err != nil {
		t.Fatalf("gone before renewal = %v, want nil no-op", err)
	}
	if renewCalls != 0 {
		t.Fatalf("renewed pub: after row disappeared (%d)", renewCalls)
	}
	if headCalls.Load() != 0 || memory.markCalls != 0 || memory.replaceCalls != 0 {
		t.Fatalf("walk ran for a settled row: head=%d mark=%d replace=%d", headCalls.Load(), memory.markCalls, memory.replaceCalls)
	}
}

func TestRepairPublishedBlockReferenceRepairCompensatesOrphanPubAfterGoneRace(t *testing.T) {
	// hydrate and the StillPending read before the pub write see the row; the
	// confirmation read after AddPublishAttemptReferences does not.
	memory := &publishedRepairProgressMemory{anchor: "other", cursor: "other", missingAfterLoad: 2}
	parents := map[string]string{"other": "other-root", "other-root": ""}
	installPublishedRepairResumableHooks(t, memory, "other", parents)
	renewCalls := 0
	removeCalls := 0
	oldRenew := renewPublishedBlockReferenceRepairLivenessFn
	oldRemove := cleanupFailedPublishRemoveAttemptReferencesFn
	t.Cleanup(func() {
		renewPublishedBlockReferenceRepairLivenessFn = oldRenew
		cleanupFailedPublishRemoveAttemptReferencesFn = oldRemove
	})
	renewPublishedBlockReferenceRepairLivenessFn = func(database *db.DB, repair *publishedBlockReferenceRepair) error {
		renewCalls++
		return nil
	}
	cleanupFailedPublishRemoveAttemptReferencesFn = func(database *db.DB, orgID, attemptID string, blockIDs []string) error {
		removeCalls++
		if attemptID != publishedBlockReferenceRepairLivenessAttemptID(publishedBlockReferenceRepair{
			RepoID:   "repo-1",
			CommitID: "target",
			FSID:     "fs-1",
		}) {
			t.Fatalf("compensate attemptID = %q, want per-repair identity", attemptID)
		}
		if len(blockIDs) != 1 || blockIDs[0] != "block-1" {
			t.Fatalf("compensate blockIDs = %#v", blockIDs)
		}
		return nil
	}
	err := repairPublishedBlockReferenceRepair(nil, publishedBlockReferenceRepair{
		Bucket:                         1,
		OrgID:                          "org-1",
		RepoID:                         "repo-1",
		CommitID:                       "target",
		FSID:                           "fs-1",
		StagedBlockIDs:                 []string{"block-1"},
		ReachabilityAnchorHeadCommitID: "other",
		ReachabilityCursorCommitID:     "other",
	})
	if err != nil {
		t.Fatalf("compensate race = %v, want nil", err)
	}
	if renewCalls != 1 || removeCalls != 1 {
		t.Fatalf("compensate race renew=%d remove=%d, want 1/1", renewCalls, removeCalls)
	}
	if memory.markCalls != 0 || memory.replaceCalls != 0 {
		t.Fatalf("walk ran after the row was gone: mark=%d replace=%d", memory.markCalls, memory.replaceCalls)
	}
}

func TestSettlePublishedBlockReferenceRepairNoLongerPendingIsNoOp(t *testing.T) {
	promoteCalls := 0
	deleteCalls := 0
	oldPromote := publishedBlockReferenceRepairPromoteFn
	oldDelete := deletePublishedBlockReferenceRepairFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairPromoteFn = oldPromote
		deletePublishedBlockReferenceRepairFn = oldDelete
	})
	publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
		promoteCalls++
		return nil
	}
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		deleteCalls++
		return nil
	}
	repair := newPublishedBlockReferenceRepair("org-1", "repo-1", "commit-1", "fs-1", []string{"block-1"})
	if err := settlePublishedBlockReferenceRepair(nil, repair, publishedBlockReferenceRepairCommitNoLongerPending, nil); err != nil {
		t.Fatalf("no-longer-pending settlement = %v, want nil", err)
	}
	if err := settlePublishedBlockReferenceRepair(nil, repair, publishedBlockReferenceRepairCommitUnknown, errPublishedBlockReferenceRepairGone); err != nil {
		t.Fatalf("gone-error settlement = %v, want nil", err)
	}
	if promoteCalls != 0 || deleteCalls != 0 {
		t.Fatalf("no-longer-pending changed state: promote=%d delete=%d", promoteCalls, deleteCalls)
	}
}

func TestRunPublishedBlockReferenceRepairSweepReapsProgressOnlyResidue(t *testing.T) {
	oldNow := publishedBlockReferenceRepairNowFn
	oldList := listPublishedBlockReferenceRepairsForBucketFn
	oldClassify := publishedBlockReferenceRepairClassifyFn
	oldLoad := loadPublishedBlockReferenceRepairFn
	oldAuthority := loadPublishedBlockReferenceRepairAuthorityFn
	oldReap := reapPublishedBlockReferenceRepairProgressOnlyRowFn
	oldDelete := deletePublishedBlockReferenceRepairFn
	oldSchedule := schedulePublishedBlockReferenceRepairRetryFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairNowFn = oldNow
		listPublishedBlockReferenceRepairsForBucketFn = oldList
		publishedBlockReferenceRepairClassifyFn = oldClassify
		loadPublishedBlockReferenceRepairFn = oldLoad
		loadPublishedBlockReferenceRepairAuthorityFn = oldAuthority
		reapPublishedBlockReferenceRepairProgressOnlyRowFn = oldReap
		deletePublishedBlockReferenceRepairFn = oldDelete
		schedulePublishedBlockReferenceRepairRetryFn = oldSchedule
	})
	schedulePublishedBlockReferenceRepairRetryFn = func(database *db.DB, repair publishedBlockReferenceRepair, retryAt time.Time) error {
		return nil
	}

	now := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
	publishedBlockReferenceRepairNowFn = func() time.Time { return now }
	// Residue of a progress LWT that raced the ordinary settlement DELETE:
	// only the primary key and reachability cells survive.
	residue := publishedBlockReferenceRepair{
		Bucket:                         0,
		OrgID:                          "org-1",
		RepoID:                         "repo-1",
		CommitID:                       "commit-zombie",
		FSID:                           "fs-zombie",
		ReachabilityAnchorHeadCommitID: "h-old",
		ReachabilityCursorCommitID:     "c-old",
	}
	legit := publishedBlockReferenceRepair{
		Bucket:         0,
		OrgID:          "org-1",
		RepoID:         "repo-1",
		CommitID:       "commit-live",
		FSID:           "fs-live",
		StagedBlockIDs: []string{"block-1"},
		CreatedAt:      now.Add(-time.Hour),
		LeaseExpiresAt: now.Add(-time.Minute),
	}
	listPublishedBlockReferenceRepairsForBucketFn = func(database *db.DB, bucket int) ([]publishedBlockReferenceRepair, error) {
		if bucket == 0 {
			return []publishedBlockReferenceRepair{residue, legit}, nil
		}
		return nil, nil
	}
	loadPublishedBlockReferenceRepairFn = func(database *db.DB, got publishedBlockReferenceRepair) (publishedBlockReferenceRepair, error) {
		if got.FSID == residue.FSID {
			t.Fatalf("progress-only residue was hydrated as actionable work: %#v", got)
		}
		return legit, nil
	}
	loadPublishedBlockReferenceRepairAuthorityFn = loadPublishedBlockReferenceRepairFn
	classified := []string{}
	publishedBlockReferenceRepairClassifyFn = func(database *db.DB, repair *publishedBlockReferenceRepair) (publishedBlockReferenceRepairCommitOutcome, error) {
		classified = append(classified, repair.FSID)
		return publishedBlockReferenceRepairCommitUnknown, nil
	}
	reaped := []publishedBlockReferenceRepair{}
	reapPublishedBlockReferenceRepairProgressOnlyRowFn = func(database *db.DB, repair publishedBlockReferenceRepair) (bool, error) {
		reaped = append(reaped, repair)
		return true, nil
	}
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		t.Fatalf("ordinary settlement delete must not be used for residue or unknown rows: %#v", repair)
		return nil
	}

	err := runPublishedBlockReferenceRepairSweep(&db.DB{})
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("sweep error = %v, want the legit row's unknown retention only", err)
	}
	if len(reaped) != 1 || reaped[0].FSID != residue.FSID || reaped[0].CommitID != residue.CommitID || reaped[0].Bucket != residue.Bucket {
		t.Fatalf("progress-only residue was not reaped exactly once: %#v", reaped)
	}
	if strings.TrimSpace(reaped[0].ReachabilityAnchorHeadCommitID) == "" && strings.TrimSpace(reaped[0].ReachabilityCursorCommitID) == "" {
		t.Fatal("reaper was handed a row without reachability cells; a listed row must carry at least one live cell")
	}
	if len(classified) != 1 || classified[0] != legit.FSID {
		t.Fatalf("classified = %v, want only the legit row", classified)
	}
	if publishedBlockReferenceRepairIsProgressOnly(legit) {
		t.Fatal("a queued row with ordinary cells must never be treated as residue")
	}
	for _, partial := range []publishedBlockReferenceRepair{
		{CreatedAt: now},
		{LeaseExpiresAt: now},
		{StagedBlockIDs: []string{"block-1"}},
	} {
		if publishedBlockReferenceRepairIsProgressOnly(partial) {
			t.Fatalf("any surviving ordinary cell must veto residue classification: %#v", partial)
		}
	}
}

func TestReapPublishedBlockReferenceRepairProgressOnlyRowIsConditionalAndSerial(t *testing.T) {
	raw, err := os.ReadFile("publish_repair.go")
	if err != nil {
		t.Fatalf("read publish_repair.go: %v", err)
	}
	source := string(raw)
	// The multi-line CQL assertion below is anchored on LF; a core.autocrlf=true
	// Windows checkout materializes this file as CRLF and Dockerfile.gotest
	// copies the working tree verbatim, so normalize before matching.
	source = strings.ReplaceAll(source, "\r\n", "\n")
	start := strings.Index(source, "var reapPublishedBlockReferenceRepairProgressOnlyRowFn")
	end := strings.Index(source, "var listPendingPublishedFSObjectOwnersByDayFn")
	if start < 0 || end <= start {
		t.Fatal("could not locate the progress-only residue reaper")
	}
	body := source[start:end]
	if strings.Contains(body, "DELETE FROM published_block_reference_repairs") {
		t.Fatal("residue reaper must never delete the whole repair row: a row tombstone with the later ballot timestamp can shadow an ordinary requeue INSERT the Paxos quorum had not yet seen")
	}
	if !strings.Contains(body, "DELETE reachability_anchor_head_commit_id, reachability_cursor_commit_id, reachability_anchor_exhausted\n\t\tFROM published_block_reference_repairs") {
		t.Fatal("residue reaper must tombstone only the reachability cells")
	}
	if !strings.Contains(body, "IF created_at = null AND lease_expires_at = null") {
		t.Fatal("residue reaper must be conditioned on the ordinary queue cells still being absent so a concurrent requeue INSERT is never shadowed")
	}
	if !strings.Contains(body, "SerialConsistency(gocql.Serial)") || !strings.Contains(body, "MapScanCAS") {
		t.Fatal("residue reaper must run as a SERIAL LWT")
	}
	if strings.Contains(body, "Exec()") {
		t.Fatal("residue reaper must not be an ordinary unconditional delete")
	}
	sweepStart := strings.Index(source, "func runPublishedBlockReferenceRepairSweep")
	sweepEnd := strings.Index(source, "func StartPublishedBlockReferenceRepairer")
	if sweepStart < 0 || sweepEnd <= sweepStart {
		t.Fatal("could not locate the repair sweep")
	}
	sweep := source[sweepStart:sweepEnd]
	if !strings.Contains(sweep, "publishedBlockReferenceRepairIsProgressOnly(repair)") || !strings.Contains(sweep, "reapPublishedBlockReferenceRepairProgressOnlyRowFn(database, repair)") {
		t.Fatal("the sweep must reap progress-only residue instead of listing it forever")
	}
}

func TestReanchorPublishedBlockReferenceRepairHeadObservationsAreBudgeted(t *testing.T) {
	// Every re-anchor CAS loses to a concurrent winner that has already
	// exhausted a strictly newer snapshot. Without a budget this loop would
	// re-read SERIAL HEAD until the 30s context expired.
	memory := &publishedRepairProgressMemory{anchor: "h1", cursor: "h1", exhausted: true}
	parents := map[string]string{"target": ""}
	parentReads := 0
	headCalls := installPublishedRepairResumableHooks(t, memory, "h-live", parents)
	publishedBlockReferenceRepairCommitParentFn = func(ctx context.Context, database *db.DB, repoID, commitID string) (string, error) {
		parentReads++
		return "", gocql.ErrNotFound
	}
	generation := 1
	replacePublishedBlockReferenceRepairAnchorFn = func(database *db.DB, repair publishedBlockReferenceRepair, expectedCursor, nextHEAD string) (bool, error) {
		generation++
		// Fail fast instead of spinning until the 30s context (or go test's
		// own deadline) if the budget is ever removed.
		if int(headCalls.Load()) > publishedCommitReachabilityMaxHeadObservations {
			t.Fatalf("re-anchor loser kept re-reading SERIAL HEAD: %d observations, want exactly the per-visit budget %d", headCalls.Load(), publishedCommitReachabilityMaxHeadObservations)
		}
		memory.mu.Lock()
		memory.anchor = fmt.Sprintf("h%d", generation)
		memory.cursor = memory.anchor
		memory.exhausted = true
		memory.mu.Unlock()
		return false, nil
	}

	repair := newTestPublishedBlockReferenceRepair("target")
	repair.ReachabilityAnchorHeadCommitID = "h0"
	repair.ReachabilityCursorCommitID = "h0"
	repair.ReachabilityAnchorExhausted = true

	outcome, err := classifyPublishedBlockReferenceRepairCommitResumable(nil, &repair)
	if err != nil || outcome != publishedBlockReferenceRepairCommitUnknown {
		t.Fatalf("budget-exhausted loser = %v/%v, want UNKNOWN/nil", outcome, err)
	}
	if got := int(headCalls.Load()); got != publishedCommitReachabilityMaxHeadObservations {
		t.Fatalf("SERIAL HEAD observations = %d, want exactly the per-visit budget %d", got, publishedCommitReachabilityMaxHeadObservations)
	}
	if parentReads != 0 {
		t.Fatalf("loser walked %d parents without owning a snapshot", parentReads)
	}
	anchor, _ := memory.snapshot()
	if !repair.ReachabilityAnchorExhausted || repair.ReachabilityAnchorHeadCommitID != anchor {
		t.Fatalf("loser did not carry the durable newer exhausted snapshot %q: %#v", anchor, repair)
	}

	// The anchor-creating read spends budget too: a fresh row that clean-walks
	// to genesis gets exactly one re-anchor attempt in the same visit.
	fresh := &publishedRepairProgressMemory{}
	freshParents := map[string]string{"h-a": "", "h-b": "target", "target": ""}
	liveHEAD := "h-a"
	freshHeads := installPublishedRepairResumableHooks(t, fresh, liveHEAD, freshParents)
	publishedBlockReferenceRepairHeadCommitFn = func(ctx context.Context, database *db.DB, orgID, repoID string) (string, error) {
		freshHeads.Add(1)
		// HEAD moves after the anchor was created, so genesis re-anchors.
		if freshHeads.Load() > 1 {
			return "h-b", nil
		}
		return liveHEAD, nil
	}
	freshRepair := newTestPublishedBlockReferenceRepair("target")
	outcome, err = classifyPublishedBlockReferenceRepairCommitResumable(nil, &freshRepair)
	if err != nil || outcome != publishedBlockReferenceRepairCommitReachable {
		t.Fatalf("fresh row = %v/%v, want REACHABLE after one re-anchor", outcome, err)
	}
	if got := int(freshHeads.Load()); got != publishedCommitReachabilityMaxHeadObservations {
		t.Fatalf("fresh-row SERIAL HEAD observations = %d, want %d (anchor create + one re-anchor)", got, publishedCommitReachabilityMaxHeadObservations)
	}
}

func TestClassifyPublishedBlockReferenceRepairResumableDetectsCycleThroughAnchoredHEAD(t *testing.T) {
	// A resumed chunk starts with an empty visited set. If the ancestry leads
	// back to the anchored HEAD, HEAD would be its own ancestor: that is a
	// cycle, not progress, and the cursor must not rotate through it.
	// The cycle is longer than one chunk: h -> p1 -> ... -> p1100 -> h. A
	// previous chunk walked h..p1023 and left the cursor at p1024, so the
	// in-chunk visited set of the resumed walk never contains h by itself.
	cycleLen := publishedCommitReachabilityMaxNodes + 76
	parents := map[string]string{"h": "p1"}
	for i := 1; i <= cycleLen; i++ {
		next := fmt.Sprintf("p%d", i+1)
		if i == cycleLen {
			next = "h"
		}
		parents[fmt.Sprintf("p%d", i)] = next
	}
	resumeCursor := fmt.Sprintf("p%d", publishedCommitReachabilityMaxNodes)
	memory := &publishedRepairProgressMemory{anchor: "h", cursor: resumeCursor}
	headCalls := installPublishedRepairResumableHooks(t, memory, "h", parents)

	repair := newTestPublishedBlockReferenceRepair("target")
	repair.ReachabilityAnchorHeadCommitID = "h"
	repair.ReachabilityCursorCommitID = resumeCursor
	outcome, err := classifyPublishedBlockReferenceRepairCommitResumable(nil, &repair)
	if outcome != publishedBlockReferenceRepairCommitUnknown || err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("resumed walk into anchored HEAD = %v/%v, want UNKNOWN cycle error", outcome, err)
	}
	anchor, cursor := memory.snapshot()
	if anchor != "h" || cursor != resumeCursor {
		t.Fatalf("cycle rotated durable progress to %q/%q, want h/%s", anchor, cursor, resumeCursor)
	}
	if headCalls.Load() != 0 {
		t.Fatalf("cycle re-observed HEAD %d times", headCalls.Load())
	}

	// Control: the first chunk of a snapshot starts at HEAD and must not
	// report itself as a cycle.
	first := &publishedRepairProgressMemory{anchor: "h", cursor: "h"}
	firstParents := map[string]string{"h": "target", "target": ""}
	installPublishedRepairResumableHooks(t, first, "h", firstParents)
	firstRepair := newTestPublishedBlockReferenceRepair("target")
	firstRepair.ReachabilityAnchorHeadCommitID = "h"
	firstRepair.ReachabilityCursorCommitID = "h"
	outcome, err = classifyPublishedBlockReferenceRepairCommitResumable(nil, &firstRepair)
	if err != nil || outcome != publishedBlockReferenceRepairCommitReachable {
		t.Fatalf("first chunk from HEAD = %v/%v, want REACHABLE", outcome, err)
	}
	if seeds := publishedBlockReferenceRepairWalkSeeds(firstRepair); len(seeds) != 0 {
		t.Fatalf("first chunk seeded %v, want nothing", seeds)
	}
}

func TestRepairPublishedBlockReferenceRepairListedLiveThenLoadedResidueIsNoOp(t *testing.T) {
	// The sweep listed a live repair; another worker settled it and a late
	// progress LWT left a progress-only residue before this worker hydrated.
	// The loaded row is authoritative: no classify, no pub: renewal, no
	// promote from the stale listed copy.
	oldLoad := loadPublishedBlockReferenceRepairFn
	oldClassify := publishedBlockReferenceRepairClassifyFn
	oldRenew := renewPublishedBlockReferenceRepairLivenessFn
	oldPromote := publishedBlockReferenceRepairPromoteFn
	oldDelete := deletePublishedBlockReferenceRepairFn
	oldRemove := removePublishedBlockReferenceRepairOwnedLivenessFn
	t.Cleanup(func() {
		loadPublishedBlockReferenceRepairFn = oldLoad
		publishedBlockReferenceRepairClassifyFn = oldClassify
		renewPublishedBlockReferenceRepairLivenessFn = oldRenew
		publishedBlockReferenceRepairPromoteFn = oldPromote
		deletePublishedBlockReferenceRepairFn = oldDelete
		removePublishedBlockReferenceRepairOwnedLivenessFn = oldRemove
	})
	now := time.Date(2026, time.September, 14, 9, 0, 0, 0, time.UTC)
	listed := publishedBlockReferenceRepair{
		Bucket:         3,
		OrgID:          "org-1",
		RepoID:         "repo-1",
		CommitID:       "commit-settled",
		FSID:           "fs-settled",
		StagedBlockIDs: []string{"block-1", "block-2"},
		CreatedAt:      now.Add(-time.Hour),
		LeaseExpiresAt: now.Add(-time.Minute),
	}
	residue := publishedBlockReferenceRepair{
		Bucket:                         listed.Bucket,
		OrgID:                          listed.OrgID,
		RepoID:                         listed.RepoID,
		CommitID:                       listed.CommitID,
		FSID:                           listed.FSID,
		ReachabilityAnchorHeadCommitID: "h-late",
		ReachabilityCursorCommitID:     "c-late",
	}
	loads := 0
	loadPublishedBlockReferenceRepairFn = func(database *db.DB, got publishedBlockReferenceRepair) (publishedBlockReferenceRepair, error) {
		loads++
		return residue, nil
	}
	publishedBlockReferenceRepairClassifyFn = func(database *db.DB, repair *publishedBlockReferenceRepair) (publishedBlockReferenceRepairCommitOutcome, error) {
		t.Fatalf("progress-only residue was classified as a live repair: %#v", *repair)
		return publishedBlockReferenceRepairCommitUnknown, nil
	}
	renewPublishedBlockReferenceRepairLivenessFn = func(database *db.DB, repair *publishedBlockReferenceRepair) error {
		t.Fatalf("progress-only residue renewed pub: for stale staged blocks %v", repair.StagedBlockIDs)
		return nil
	}
	publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
		t.Fatal("progress-only residue was promoted from the stale listed copy")
		return nil
	}
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		t.Fatal("hydrate of a residue must not settle-delete")
		return nil
	}
	removePublishedBlockReferenceRepairOwnedLivenessFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		t.Fatal("hydrate of a residue must not remove pub:")
		return nil
	}

	if err := repairPublishedBlockReferenceRepair(&db.DB{}, listed); err != nil {
		t.Fatalf("listed-live then loaded-residue = %v, want no-op", err)
	}
	if loads == 0 {
		t.Fatal("repair did not re-read the durable row before acting on the listed copy")
	}
	pending, err := publishedBlockReferenceRepairStillPending(&db.DB{}, listed)
	if err != nil || pending {
		t.Fatalf("StillPending on residue = %v/%v, want false/nil", pending, err)
	}
	hydrated, err := hydratePublishedBlockReferenceRepair(&db.DB{}, listed)
	if !errors.Is(err, errPublishedBlockReferenceRepairGone) || len(hydrated.StagedBlockIDs) != 0 {
		t.Fatalf("hydrate of residue = %#v/%v, want gone with no stale cells", hydrated, err)
	}

	// The loaded ordinary cells are authoritative even for a live row: a
	// request-supplied or listed copy must not outlive what Cassandra holds.
	live := residue
	live.StagedBlockIDs = []string{"block-9"}
	live.CreatedAt = now.Add(-2 * time.Hour)
	live.LeaseExpiresAt = now.Add(-2 * time.Minute)
	merged := mergePublishedBlockReferenceRepairProgress(listed, live)
	if len(merged.StagedBlockIDs) != 1 || merged.StagedBlockIDs[0] != "block-9" || !merged.CreatedAt.Equal(live.CreatedAt) || !merged.LeaseExpiresAt.Equal(live.LeaseExpiresAt) {
		t.Fatalf("merge kept stale listed cells over the loaded row: %#v", merged)
	}
}

func TestClassifyPublishedBlockReferenceRepairCASMissOnResidueIsGoneAndDoesNotRenew(t *testing.T) {
	// The row became residue between hydrate and the anchor CAS. The
	// CAS-miss reload must classify as no-longer-pending, not walk the
	// residue's cursor, and the caller must not renew pub:.
	memory := &publishedRepairProgressMemory{}
	parents := map[string]string{"h": "target", "target": ""}
	headCalls := installPublishedRepairResumableHooks(t, memory, "h", parents)
	parentReads := 0
	publishedBlockReferenceRepairCommitParentFn = func(ctx context.Context, database *db.DB, repoID, commitID string) (string, error) {
		parentReads++
		return parents[commitID], nil
	}
	persistPublishedBlockReferenceRepairAnchorFn = func(database *db.DB, repair publishedBlockReferenceRepair, anchorCommitID string) (bool, error) {
		return false, nil
	}
	loadPublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) (publishedBlockReferenceRepair, error) {
		return publishedBlockReferenceRepair{
			Bucket: repair.Bucket, OrgID: repair.OrgID, RepoID: repair.RepoID, CommitID: repair.CommitID, FSID: repair.FSID,
			ReachabilityAnchorHeadCommitID: "h-late",
			ReachabilityCursorCommitID:     "c-late",
		}, nil
	}
	renewed := 0
	renewPublishedBlockReferenceRepairLivenessFn = func(database *db.DB, repair *publishedBlockReferenceRepair) error {
		renewed++
		return nil
	}

	repair := newTestPublishedBlockReferenceRepair("target")
	outcome, err := classifyPublishedBlockReferenceRepairCommitResumable(nil, &repair)
	if outcome != publishedBlockReferenceRepairCommitNoLongerPending || !errors.Is(err, errPublishedBlockReferenceRepairGone) {
		t.Fatalf("CAS miss on residue = %v/%v, want NoLongerPending/gone", outcome, err)
	}
	if parentReads != 0 || headCalls.Load() != 1 {
		t.Fatalf("residue cursor was walked: parentReads=%d headCalls=%d", parentReads, headCalls.Load())
	}
	if err := repairPublishedBlockReferenceRepair(nil, newTestPublishedBlockReferenceRepair("target")); err != nil {
		t.Fatalf("repair on residue = %v, want no-op", err)
	}
	if renewed != 0 {
		t.Fatalf("residue renewed pub: %d times", renewed)
	}
}

// repairVisitOrderHooks instruments one repair visit so tests can assert the
// order of liveness renewal, classification, and settlement
// (ISSUE-PUBLISH-REPAIR-RENEWAL-AFTER-CLASSIFY-01). The database is a non-nil
// handle so a load miss is honoured as Gone rather than tolerated as "no
// database"; loads answers hydrate and the StillPending reads in order and
// its last answer repeats afterwards (a cleared row stays cleared).
type repairVisitOrderHooks struct {
	events        []string
	loadCalls     int
	loads         []bool
	renewCalls    int
	classifyCalls int
	promoteCalls  int
	removeCalls   int
	deleteCalls   int
	intentInserts int
	intentDeletes int
	removedIDs    []string
	removedBlocks [][]string
	armCalls      int
	// intents is the durable witness store keyed by fsID + producer token,
	// with the producer fence (armed) as value.
	intents map[string]bool
	// lastLease is the producer lease the renew hook observed.
	lastLease time.Time
}

func intentKey(repair publishedBlockReferenceRepair) string {
	return repair.FSID + "@" + repair.LivenessToken
}

func installRepairVisitOrderHooks(t *testing.T, liveLoads []bool, outcome publishedBlockReferenceRepairCommitOutcome, classifyErr error) *repairVisitOrderHooks {
	t.Helper()
	hooks := &repairVisitOrderHooks{loads: liveLoads, intents: map[string]bool{}}
	oldLoad := loadPublishedBlockReferenceRepairFn
	oldAuthority := loadPublishedBlockReferenceRepairAuthorityFn
	oldRenew := renewPublishedBlockReferenceRepairLivenessFn
	oldClassify := publishedBlockReferenceRepairClassifyFn
	oldPending := loadPublishedBlockReferenceRepairPendingFileFn
	oldPromote := publishedBlockReferenceRepairPromoteFn
	oldRemove := cleanupFailedPublishRemoveAttemptReferencesFn
	oldDelete := deletePublishedBlockReferenceRepairFn
	oldInsertIntent := insertPublishedBlockReferenceRepairLivenessCleanupFn
	oldArmIntent := armPublishedBlockReferenceRepairLivenessCleanupFn
	oldDeleteIntent := deletePublishedBlockReferenceRepairLivenessCleanupFn
	oldRetireIntent := retirePublishedBlockReferenceRepairLivenessCleanupFn
	oldOwnedPub := removePublishedBlockReferenceRepairOwnedPubFn
	oldExtend := extendPublishedBlockReferenceRepairLivenessCleanupFn
	t.Cleanup(func() {
		retirePublishedBlockReferenceRepairLivenessCleanupFn = oldRetireIntent
		removePublishedBlockReferenceRepairOwnedPubFn = oldOwnedPub
		extendPublishedBlockReferenceRepairLivenessCleanupFn = oldExtend
		loadPublishedBlockReferenceRepairFn = oldLoad
		loadPublishedBlockReferenceRepairAuthorityFn = oldAuthority
		renewPublishedBlockReferenceRepairLivenessFn = oldRenew
		publishedBlockReferenceRepairClassifyFn = oldClassify
		loadPublishedBlockReferenceRepairPendingFileFn = oldPending
		publishedBlockReferenceRepairPromoteFn = oldPromote
		cleanupFailedPublishRemoveAttemptReferencesFn = oldRemove
		deletePublishedBlockReferenceRepairFn = oldDelete
		insertPublishedBlockReferenceRepairLivenessCleanupFn = oldInsertIntent
		armPublishedBlockReferenceRepairLivenessCleanupFn = oldArmIntent
		deletePublishedBlockReferenceRepairLivenessCleanupFn = oldDeleteIntent
	})
	insertPublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		hooks.intentInserts++
		if repair.LivenessToken == "" {
			t.Fatal("cleanup intent written without a producer token")
		}
		if repair.LivenessArmed {
			t.Fatal("cleanup intent written already armed: the producer fence must start PREPARING")
		}
		hooks.intents[intentKey(repair)] = false
		hooks.events = append(hooks.events, "intent")
		return nil
	}
	armPublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, repair publishedBlockReferenceRepair) (bool, error) {
		hooks.armCalls++
		if _, ok := hooks.intents[intentKey(repair)]; !ok {
			// Consumed by the sweep: a conditional ARM does not apply.
			hooks.events = append(hooks.events, "arm-not-applied")
			return false, nil
		}
		hooks.intents[intentKey(repair)] = true
		hooks.events = append(hooks.events, "arm")
		return true, nil
	}
	extendPublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, repair publishedBlockReferenceRepair, nextLease time.Time) (bool, error) {
		return true, nil
	}
	removePublishedBlockReferenceRepairOwnedPubFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		return cleanupFailedPublishRemoveAttemptReferencesFn(database, repair.OrgID, publishedBlockReferenceRepairLivenessAttemptID(repair), repair.StagedBlockIDs)
	}
	// A visit never deletes a witness outright: it RETIRES its own token to
	// CONSUMED (the sweep re-fences and finally deletes it). The store drops
	// the key so a later ARM of that token cannot apply, exactly like the
	// production LWT (armed = true).
	retirePublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, repair publishedBlockReferenceRepair, now time.Time) (bool, error) {
		hooks.intentDeletes++
		delete(hooks.intents, intentKey(repair))
		hooks.events = append(hooks.events, "retire-intent")
		return true, nil
	}
	deletePublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		t.Fatalf("a visit deleted a witness outright (token %s): only the sweep deletes, after the re-fence retention", repair.LivenessToken)
		return nil
	}
	load := func(database *db.DB, repair publishedBlockReferenceRepair) (publishedBlockReferenceRepair, error) {
		hooks.loadCalls++
		hooks.events = append(hooks.events, "load")
		live := true
		if hooks.loadCalls <= len(hooks.loads) {
			live = hooks.loads[hooks.loadCalls-1]
		} else if len(hooks.loads) > 0 {
			live = hooks.loads[len(hooks.loads)-1]
		}
		if !live {
			return publishedBlockReferenceRepair{}, gocql.ErrNotFound
		}
		return repair, nil
	}
	loadPublishedBlockReferenceRepairFn = load
	loadPublishedBlockReferenceRepairAuthorityFn = load
	renewPublishedBlockReferenceRepairLivenessFn = func(database *db.DB, repair *publishedBlockReferenceRepair) error {
		hooks.renewCalls++
		hooks.lastLease = repair.LivenessLeaseExpiresAt
		if armed, ok := hooks.intents[intentKey(*repair)]; !ok || armed {
			t.Fatalf("pin written without a PREPARING witness for its token (present=%v armed=%v)", ok, armed)
		}
		hooks.events = append(hooks.events, "renew")
		return nil
	}
	publishedBlockReferenceRepairClassifyFn = func(database *db.DB, repair *publishedBlockReferenceRepair) (publishedBlockReferenceRepairCommitOutcome, error) {
		hooks.classifyCalls++
		hooks.events = append(hooks.events, "classify")
		return outcome, classifyErr
	}
	loadPublishedBlockReferenceRepairPendingFileFn = func(database *db.DB, repoID, fsID string) (*pendingPublishedFile, error) {
		return &pendingPublishedFile{fsID: fsID}, nil
	}
	publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
		hooks.promoteCalls++
		hooks.events = append(hooks.events, "promote")
		return nil
	}
	cleanupFailedPublishRemoveAttemptReferencesFn = func(database *db.DB, orgID, attemptID string, blockIDs []string) error {
		hooks.removeCalls++
		hooks.removedIDs = append(hooks.removedIDs, attemptID)
		hooks.removedBlocks = append(hooks.removedBlocks, append([]string(nil), blockIDs...))
		hooks.events = append(hooks.events, "remove-owned-pub")
		return nil
	}
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		hooks.deleteCalls++
		hooks.events = append(hooks.events, "delete")
		return nil
	}
	return hooks
}

func (h *repairVisitOrderHooks) without(event string) []string {
	kept := make([]string, 0, len(h.events))
	for _, e := range h.events {
		if e != event {
			kept = append(kept, e)
		}
	}
	return kept
}

func (h *repairVisitOrderHooks) index(event string) int {
	for i, e := range h.events {
		if e == event {
			return i
		}
	}
	return -1
}

func TestRepairPublishedBlockReferenceRepairRenewsLivenessBeforeClassify(t *testing.T) {
	hooks := installRepairVisitOrderHooks(t, nil, publishedBlockReferenceRepairCommitUnknown, nil)
	err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1"))
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("visit = %v, want UNKNOWN retention", err)
	}
	renewAt, classifyAt := hooks.index("renew"), hooks.index("classify")
	if renewAt < 0 || classifyAt < 0 {
		t.Fatalf("events = %v, want both renew and classify", hooks.events)
	}
	if renewAt > classifyAt {
		t.Fatalf("events = %v, want renew before classify: the bounded classifier must not run on an unrenewed pub:", hooks.events)
	}
	if hooks.renewCalls != 1 || hooks.classifyCalls != 1 {
		t.Fatalf("renew=%d classify=%d, want exactly 1/1", hooks.renewCalls, hooks.classifyCalls)
	}
}

func TestRepairPublishedBlockReferenceRepairRenewFailureDoesNotClassify(t *testing.T) {
	hooks := installRepairVisitOrderHooks(t, nil, publishedBlockReferenceRepairCommitReachable, nil)
	renewPublishedBlockReferenceRepairLivenessFn = func(database *db.DB, repair *publishedBlockReferenceRepair) error {
		hooks.renewCalls++
		hooks.events = append(hooks.events, "renew")
		return fmt.Errorf("renew failed")
	}
	err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1"))
	if hooks.classifyCalls != 0 {
		t.Fatalf("classifyCalls = %d, want 0: a failed pre-classify renewal must fail closed before the ancestry walk", hooks.classifyCalls)
	}
	if err == nil || !strings.Contains(err.Error(), "renew failed") {
		t.Fatalf("visit = %v, want the renewal error for retry", err)
	}
	if hooks.promoteCalls != 0 || hooks.deleteCalls != 0 || hooks.removeCalls != 0 {
		t.Fatalf("promote=%d delete=%d remove=%d, want 0/0/0 (repair remains)", hooks.promoteCalls, hooks.deleteCalls, hooks.removeCalls)
	}
}

func TestRepairPublishedBlockReferenceRepairRowGoneBeforeRenewIsTerminalNoOp(t *testing.T) {
	// hydrate sees the row; the StillPending read before the pub write does not.
	hooks := installRepairVisitOrderHooks(t, []bool{true, false}, publishedBlockReferenceRepairCommitReachable, nil)
	err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1"))
	if err != nil {
		t.Fatalf("visit = %v, want nil terminal no-op", err)
	}
	if hooks.renewCalls != 0 {
		t.Fatalf("renewCalls = %d, want 0: no pub write for a row that settled before the renewal", hooks.renewCalls)
	}
	if hooks.classifyCalls != 0 || hooks.promoteCalls != 0 || hooks.deleteCalls != 0 || hooks.removeCalls != 0 {
		t.Fatalf("classify=%d promote=%d delete=%d remove=%d, want all 0", hooks.classifyCalls, hooks.promoteCalls, hooks.deleteCalls, hooks.removeCalls)
	}
}

func TestRepairPublishedBlockReferenceRepairRowGoneAfterRenewCompensatesExactPub(t *testing.T) {
	// hydrate and the first StillPending read see the row; the confirmation
	// read after AddPublishAttemptReferences does not.
	hooks := installRepairVisitOrderHooks(t, []bool{true, true, false}, publishedBlockReferenceRepairCommitReachable, nil)
	repair := newTestPublishedBlockReferenceRepair("commit-1")
	err := repairPublishedBlockReferenceRepair(&db.DB{}, repair)
	if err != nil {
		t.Fatalf("visit = %v, want nil (gone handled as terminal)", err)
	}
	if hooks.renewCalls != 1 {
		t.Fatalf("renewCalls = %d, want 1", hooks.renewCalls)
	}
	if hooks.removeCalls != 1 {
		t.Fatalf("removeCalls = %d, want exactly one compensation of the pub: just written", hooks.removeCalls)
	}
	want := publishedBlockReferenceRepairLivenessAttemptID(repair)
	if hooks.removedIDs[0] != want {
		t.Fatalf("compensated attempt = %q, want per-repair identity %q", hooks.removedIDs[0], want)
	}
	if hooks.removedIDs[0] == repair.CommitID {
		t.Fatal("compensation removed the commit-scoped pub:<commitID>, which sibling repairs share")
	}
	if !reflect.DeepEqual(hooks.removedBlocks[0], repair.StagedBlockIDs) {
		t.Fatalf("compensated blocks = %#v, want %#v", hooks.removedBlocks[0], repair.StagedBlockIDs)
	}
	if hooks.classifyCalls != 0 || hooks.promoteCalls != 0 || hooks.deleteCalls != 0 {
		t.Fatalf("classify=%d promote=%d delete=%d, want all 0", hooks.classifyCalls, hooks.promoteCalls, hooks.deleteCalls)
	}
}

func TestRepairPublishedBlockReferenceRepairUnknownRenewsOncePerVisit(t *testing.T) {
	hooks := installRepairVisitOrderHooks(t, nil, publishedBlockReferenceRepairCommitUnknown, nil)
	err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1"))
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("visit = %v, want UNKNOWN retention", err)
	}
	if hooks.renewCalls != 1 {
		t.Fatalf("renewCalls = %d, want exactly 1: the pre-classify renewal already protects the retained row", hooks.renewCalls)
	}
	// The row is still pending after the walk: it keeps its pin.
	if hooks.deleteCalls != 0 || hooks.promoteCalls != 0 || hooks.removeCalls != 0 {
		t.Fatalf("delete=%d promote=%d remove=%d, want 0/0/0", hooks.deleteCalls, hooks.promoteCalls, hooks.removeCalls)
	}
}

func TestRepairPublishedBlockReferenceRepairClassifierErrorRetainsRenewedRow(t *testing.T) {
	walkErr := fmt.Errorf("lookup parent for commit c-7: %w", context.DeadlineExceeded)
	hooks := installRepairVisitOrderHooks(t, nil, publishedBlockReferenceRepairCommitUnknown, walkErr)
	err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("visit = %v, want the classifier error surfaced for retry", err)
	}
	if hooks.renewCalls != 1 {
		t.Fatalf("renewCalls = %d, want exactly 1", hooks.renewCalls)
	}
	if hooks.deleteCalls != 0 || hooks.promoteCalls != 0 || hooks.removeCalls != 0 {
		t.Fatalf("delete=%d promote=%d remove=%d, want 0/0/0: a classifier error is never cleanup authority", hooks.deleteCalls, hooks.promoteCalls, hooks.removeCalls)
	}
}

func TestRepairPublishedBlockReferenceRepairReachableOrderIsRenewClassifyPromoteCleanupDelete(t *testing.T) {
	hooks := installRepairVisitOrderHooks(t, nil, publishedBlockReferenceRepairCommitReachable, nil)
	if err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1")); err != nil {
		t.Fatalf("visit = %v, want REACHABLE settlement", err)
	}
	want := []string{"intent", "renew", "arm", "classify", "promote", "remove-owned-pub", "retire-intent", "delete"}
	if got := hooks.without("load"); !reflect.DeepEqual(got, want) {
		t.Fatalf("visit order = %v, want %v", got, want)
	}
}

func TestRepairPublishedBlockReferenceRepairReachableSettlementFailureKeepsSingleRenewal(t *testing.T) {
	hooks := installRepairVisitOrderHooks(t, nil, publishedBlockReferenceRepairCommitReachable, nil)
	publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
		hooks.promoteCalls++
		hooks.events = append(hooks.events, "promote")
		return fmt.Errorf("promote boom")
	}
	err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1"))
	if err == nil || !strings.Contains(err.Error(), "promote boom") {
		t.Fatalf("visit = %v, want the settlement error", err)
	}
	if hooks.renewCalls != 1 {
		t.Fatalf("renewCalls = %d, want exactly 1: the pre-renewed pub: already protects the retry, no reflex second write", hooks.renewCalls)
	}
	if hooks.deleteCalls != 0 || hooks.removeCalls != 0 {
		t.Fatalf("delete=%d remove=%d, want 0/0 (repair and its liveness retained)", hooks.deleteCalls, hooks.removeCalls)
	}
}

// TestRepairPublishedBlockReferenceRepairLivenessSurvivesClassifierPastPriorExpiry
// models the exact defect closed by ISSUE-PUBLISH-REPAIR-RENEWAL-AFTER-CLASSIFY-01
// with a deterministic clock: the visit starts while a prior pub: is still
// valid but close to expiry, and the bounded classifier runs long enough to
// cross that expiry. Renewing first keeps a valid pin through the walk;
// renewing afterwards leaves a zero-ref interval while the walk runs. It does
// not model discovery after expiry (ISSUE-PUBLISH-REPAIR-DISCOVERY-SCALE-01).
func TestRepairPublishedBlockReferenceRepairLivenessSurvivesClassifierPastPriorExpiry(t *testing.T) {
	hooks := installRepairVisitOrderHooks(t, nil, publishedBlockReferenceRepairCommitUnknown, nil)
	start := time.Date(2026, time.September, 14, 12, 0, 0, 0, time.UTC)
	clock := start
	priorExpiry := start.Add(10 * time.Second)
	pubExpiresAt := priorExpiry
	renewPublishedBlockReferenceRepairLivenessFn = func(database *db.DB, repair *publishedBlockReferenceRepair) error {
		hooks.renewCalls++
		pubExpiresAt = clock.Add(35 * 24 * time.Hour)
		return nil
	}
	zeroRefObserved := false
	publishedBlockReferenceRepairClassifyFn = func(database *db.DB, repair *publishedBlockReferenceRepair) (publishedBlockReferenceRepairCommitOutcome, error) {
		hooks.classifyCalls++
		clock = clock.Add(publishedCommitReachabilityTimeout)
		if !clock.After(priorExpiry) {
			t.Fatal("model did not cross the prior expiry during the walk; the test proves nothing")
		}
		if !pubExpiresAt.After(clock) {
			zeroRefObserved = true
		}
		return publishedBlockReferenceRepairCommitUnknown, nil
	}
	err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1"))
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("visit = %v, want UNKNOWN retention", err)
	}
	if zeroRefObserved {
		t.Fatal("pub: expired while the classifier was still walking: liveness must be renewed before the bounded classifier, not after it")
	}
	if !pubExpiresAt.After(clock) {
		t.Fatalf("pub: expires %s, clock %s: the retained row must still be pinned after the visit", pubExpiresAt, clock)
	}
	if hooks.renewCalls != 1 || hooks.classifyCalls != 1 {
		t.Fatalf("renew=%d classify=%d, want 1/1", hooks.renewCalls, hooks.classifyCalls)
	}
}

// A writer's ordinary settlement (ClearPublishedFSObjectBlockReferenceRepair)
// deletes only the repair row. Because this visit writes pub: before the
// walk, a clear that lands during the walk would otherwise leave that pin
// ownerless until its TTL — a window main did not have. The visit must remove
// exactly its own identity when it learns the row is gone.
func TestRepairPublishedBlockReferenceRepairRowClearedDuringClassifyRemovesOwnPub(t *testing.T) {
	// hydrate, pre-write and post-write reads see the row; the read after the
	// classifier reported Gone does not.
	hooks := installRepairVisitOrderHooks(t, []bool{true, true, true, false}, publishedBlockReferenceRepairCommitNoLongerPending, errPublishedBlockReferenceRepairGone)
	repair := newTestPublishedBlockReferenceRepair("commit-1")
	if err := repairPublishedBlockReferenceRepair(&db.DB{}, repair); err != nil {
		t.Fatalf("visit = %v, want nil after compensating", err)
	}
	if hooks.renewCalls != 1 || hooks.classifyCalls != 1 {
		t.Fatalf("renew=%d classify=%d, want 1/1", hooks.renewCalls, hooks.classifyCalls)
	}
	if hooks.removeCalls != 1 {
		t.Fatalf("removeCalls = %d, want exactly one removal of the pub: this visit wrote before the walk", hooks.removeCalls)
	}
	if want := publishedBlockReferenceRepairLivenessAttemptID(repair); hooks.removedIDs[0] != want || hooks.removedIDs[0] == repair.CommitID {
		t.Fatalf("removed %q, want per-repair identity %q", hooks.removedIDs[0], want)
	}
	if hooks.promoteCalls != 0 || hooks.deleteCalls != 0 {
		t.Fatalf("promote=%d delete=%d, want 0/0: row absence is not positive reachability", hooks.promoteCalls, hooks.deleteCalls)
	}
}

func TestRepairPublishedBlockReferenceRepairRowClearedDuringUnknownWalkRemovesOwnPub(t *testing.T) {
	// The walk timed out before any CAS could observe the delete, so the
	// classifier says UNKNOWN; the post-walk read finds the row gone.
	hooks := installRepairVisitOrderHooks(t, []bool{true, true, true, false}, publishedBlockReferenceRepairCommitUnknown, context.DeadlineExceeded)
	if err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1")); err != nil {
		t.Fatalf("visit = %v, want nil: a cleared row is terminal, not a retained UNKNOWN", err)
	}
	if hooks.removeCalls != 1 {
		t.Fatalf("removeCalls = %d, want 1", hooks.removeCalls)
	}
	if hooks.promoteCalls != 0 || hooks.deleteCalls != 0 {
		t.Fatalf("promote=%d delete=%d, want 0/0", hooks.promoteCalls, hooks.deleteCalls)
	}
}

func TestRepairPublishedBlockReferenceRepairRequeuedRowAfterGoneIsNotCompensated(t *testing.T) {
	// The classifier observed Gone, but by the time this visit re-reads, the
	// same identity has been requeued. That row owns the pin now; removing it
	// would steal a later generation's liveness.
	hooks := installRepairVisitOrderHooks(t, nil, publishedBlockReferenceRepairCommitNoLongerPending, errPublishedBlockReferenceRepairGone)
	if err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1")); err != nil {
		t.Fatalf("visit = %v, want nil no-op", err)
	}
	if hooks.removeCalls != 0 {
		t.Fatalf("removeCalls = %d, want 0: a pending row keeps its pin", hooks.removeCalls)
	}
	if hooks.promoteCalls != 0 || hooks.deleteCalls != 0 {
		t.Fatalf("promote=%d delete=%d, want 0/0", hooks.promoteCalls, hooks.deleteCalls)
	}
}

// AddPublishAttemptReferences is a sequential per-block fan-out, so a
// renewal error may have written some refs already. If the row is gone by
// then, those refs must be removed like any other lost race.
func TestRepairPublishedBlockReferenceRepairPartialRenewalFailureCompensatesWhenRowGone(t *testing.T) {
	hooks := installRepairVisitOrderHooks(t, []bool{true, true, false}, publishedBlockReferenceRepairCommitReachable, nil)
	renewPublishedBlockReferenceRepairLivenessFn = func(database *db.DB, repair *publishedBlockReferenceRepair) error {
		hooks.renewCalls++
		return fmt.Errorf("block 3 of 5: write timeout")
	}
	repair := newTestPublishedBlockReferenceRepair("commit-1")
	if err := repairPublishedBlockReferenceRepair(&db.DB{}, repair); err != nil {
		t.Fatalf("visit = %v, want nil: the row is gone, the partial refs were removed", err)
	}
	if hooks.removeCalls != 1 || hooks.removedIDs[0] != publishedBlockReferenceRepairLivenessAttemptID(repair) {
		t.Fatalf("remove calls=%d ids=%v, want one removal of the per-repair identity", hooks.removeCalls, hooks.removedIDs)
	}
	if hooks.classifyCalls != 0 || hooks.promoteCalls != 0 || hooks.deleteCalls != 0 {
		t.Fatalf("classify=%d promote=%d delete=%d, want all 0", hooks.classifyCalls, hooks.promoteCalls, hooks.deleteCalls)
	}
}

func TestRepairPublishedBlockReferenceRepairPartialRenewalFailureRetainsWhenRowPending(t *testing.T) {
	hooks := installRepairVisitOrderHooks(t, nil, publishedBlockReferenceRepairCommitReachable, nil)
	renewPublishedBlockReferenceRepairLivenessFn = func(database *db.DB, repair *publishedBlockReferenceRepair) error {
		hooks.renewCalls++
		return fmt.Errorf("block 3 of 5: write timeout")
	}
	err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1"))
	if err == nil || !strings.Contains(err.Error(), "write timeout") {
		t.Fatalf("visit = %v, want the renewal error retained for retry", err)
	}
	if hooks.removeCalls != 0 {
		t.Fatalf("removeCalls = %d, want 0: a pending row keeps the refs already written; the next visit completes the fan-out", hooks.removeCalls)
	}
	if hooks.classifyCalls != 0 {
		t.Fatalf("classifyCalls = %d, want 0", hooks.classifyCalls)
	}
}

// REACHABLE takes the positive settlement path; if that settlement fails
// after a writer's ordinary clear already deleted the row, the pin this visit
// wrote before the walk must still be removed (main renewed only after a
// failed settlement and therefore never wrote it for a gone row).
func TestRepairPublishedBlockReferenceRepairReachableSettlementFailureAfterClearRemovesOwnPub(t *testing.T) {
	hooks := installRepairVisitOrderHooks(t, []bool{true, true, true, false}, publishedBlockReferenceRepairCommitReachable, nil)
	publishedBlockReferenceRepairPromoteFn = func(helper *FSHelper, orgID, repoID, commitID string, pending *pendingPublishedFile) error {
		hooks.promoteCalls++
		hooks.events = append(hooks.events, "promote")
		return fmt.Errorf("promote: fs_object write timeout")
	}
	repair := newTestPublishedBlockReferenceRepair("commit-1")
	err := repairPublishedBlockReferenceRepair(&db.DB{}, repair)
	if hooks.removeCalls != 1 || hooks.removedIDs[0] != publishedBlockReferenceRepairLivenessAttemptID(repair) {
		t.Fatalf("remove calls=%d ids=%v, want one removal of the per-repair identity after the failed settlement found the row gone", hooks.removeCalls, hooks.removedIDs)
	}
	if err != nil {
		t.Fatalf("visit = %v, want nil: the writer settled this row itself", err)
	}
	if hooks.deleteCalls != 0 || hooks.renewCalls != 1 {
		t.Fatalf("delete=%d renew=%d, want 0/1", hooks.deleteCalls, hooks.renewCalls)
	}
}

// The cleanup intent is the durable witness for the pin: it must exist before
// the pin does, so a clear + failed compensation + process loss is still
// rediscoverable by the sweep. If it cannot be written, no pin is written.
func TestRepairPublishedBlockReferenceRepairWritesCleanupIntentBeforeRenewingPub(t *testing.T) {
	hooks := installRepairVisitOrderHooks(t, nil, publishedBlockReferenceRepairCommitUnknown, nil)
	if err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1")); err == nil {
		t.Fatal("visit = nil, want UNKNOWN retention")
	}
	intentAt, renewAt := hooks.index("intent"), hooks.index("renew")
	if intentAt < 0 || renewAt < 0 || intentAt > renewAt {
		t.Fatalf("events = %v, want the cleanup intent written before the pub: write", hooks.events)
	}
	if hooks.intentInserts != 1 || hooks.intentDeletes != 0 {
		t.Fatalf("intent inserts=%d deletes=%d, want 1/0: a retained row keeps its intent", hooks.intentInserts, hooks.intentDeletes)
	}
}

func TestRepairPublishedBlockReferenceRepairCleanupIntentWriteFailureWritesNoPub(t *testing.T) {
	hooks := installRepairVisitOrderHooks(t, nil, publishedBlockReferenceRepairCommitReachable, nil)
	insertPublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		hooks.intentInserts++
		return fmt.Errorf("intent write timeout")
	}
	err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1"))
	if hooks.renewCalls != 0 {
		t.Fatalf("renewCalls = %d, want 0: never write a pin whose cleanup could not be recorded", hooks.renewCalls)
	}
	if err == nil || !strings.Contains(err.Error(), "intent write timeout") {
		t.Fatalf("visit = %v, want the intent error retained for retry", err)
	}
	if hooks.classifyCalls != 0 || hooks.promoteCalls != 0 || hooks.deleteCalls != 0 || hooks.removeCalls != 0 {
		t.Fatalf("classify=%d promote=%d delete=%d remove=%d, want all 0", hooks.classifyCalls, hooks.promoteCalls, hooks.deleteCalls, hooks.removeCalls)
	}
}

func TestRepairPublishedBlockReferenceRepairCompensationDeletesIntentAfterPin(t *testing.T) {
	hooks := installRepairVisitOrderHooks(t, []bool{true, true, true, false}, publishedBlockReferenceRepairCommitNoLongerPending, errPublishedBlockReferenceRepairGone)
	if err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1")); err != nil {
		t.Fatalf("visit = %v, want nil", err)
	}
	removeAt, intentAt := hooks.index("remove-owned-pub"), hooks.index("retire-intent")
	if removeAt < 0 || intentAt < 0 || intentAt < removeAt {
		t.Fatalf("events = %v, want the intent retired only after the pin was removed", hooks.events)
	}
}

// A failed gone-check read or a failed pin DELETE leaves the intent in place;
// nothing in the visit may delete it, because the sweep is its only retry.
func TestRepairPublishedBlockReferenceRepairFailedCompensationKeepsIntent(t *testing.T) {
	t.Run("gone-check read error", func(t *testing.T) {
		hooks := installRepairVisitOrderHooks(t, []bool{true, true, true}, publishedBlockReferenceRepairCommitNoLongerPending, errPublishedBlockReferenceRepairGone)
		loadPublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) (publishedBlockReferenceRepair, error) {
			hooks.loadCalls++
			if hooks.loadCalls > 3 {
				return publishedBlockReferenceRepair{}, fmt.Errorf("read timeout")
			}
			return repair, nil
		}
		err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1"))
		if err == nil || !strings.Contains(err.Error(), "read timeout") {
			t.Fatalf("visit = %v, want the read error surfaced", err)
		}
		if hooks.intentDeletes != 0 || hooks.removeCalls != 0 {
			t.Fatalf("intentDeletes=%d remove=%d, want 0/0: unknown row state must not delete the witness", hooks.intentDeletes, hooks.removeCalls)
		}
	})
	t.Run("pin delete error", func(t *testing.T) {
		hooks := installRepairVisitOrderHooks(t, []bool{true, true, true, false}, publishedBlockReferenceRepairCommitNoLongerPending, errPublishedBlockReferenceRepairGone)
		cleanupFailedPublishRemoveAttemptReferencesFn = func(database *db.DB, orgID, attemptID string, blockIDs []string) error {
			hooks.removeCalls++
			return fmt.Errorf("delete block_references: write timeout")
		}
		err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1"))
		if err == nil || !strings.Contains(err.Error(), "write timeout") {
			t.Fatalf("visit = %v, want the DELETE error surfaced", err)
		}
		if hooks.intentDeletes != 0 {
			t.Fatalf("intentDeletes = %d, want 0: the pin is still present, the sweep must find the intent", hooks.intentDeletes)
		}
	})
}

// The sweep is the durable retry: an intent whose repair row is gone has its
// pin removed and is deleted; an intent whose row is pending is left alone;
// a failed removal keeps the intent for the next sweep. It never touches
// repair rows.
func TestPublishedBlockReferenceRepairSweepProcessesLivenessCleanupIntents(t *testing.T) {
	oldList := listPublishedBlockReferenceRepairsForBucketFn
	oldListIntents := listPublishedBlockReferenceRepairLivenessCleanupsForBucketFn
	oldLoad := loadPublishedBlockReferenceRepairFn
	oldAuthority := loadPublishedBlockReferenceRepairAuthorityFn
	oldRemove := cleanupFailedPublishRemoveAttemptReferencesFn
	oldDeleteIntent := deletePublishedBlockReferenceRepairLivenessCleanupFn
	oldDelete := deletePublishedBlockReferenceRepairFn
	t.Cleanup(func() {
		listPublishedBlockReferenceRepairsForBucketFn = oldList
		listPublishedBlockReferenceRepairLivenessCleanupsForBucketFn = oldListIntents
		loadPublishedBlockReferenceRepairFn = oldLoad
		loadPublishedBlockReferenceRepairAuthorityFn = oldAuthority
		cleanupFailedPublishRemoveAttemptReferencesFn = oldRemove
		deletePublishedBlockReferenceRepairLivenessCleanupFn = oldDeleteIntent
		deletePublishedBlockReferenceRepairFn = oldDelete
	})
	// The session read may only RETAIN; every absence must be escalated.
	localReads := map[string]int{}
	loadPublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) (publishedBlockReferenceRepair, error) {
		localReads[repair.FSID]++
		if repair.FSID == "fs-owned" || repair.FSID == "fs-local" {
			return repair, nil
		}
		return publishedBlockReferenceRepair{}, gocql.ErrNotFound
	}
	listPublishedBlockReferenceRepairsForBucketFn = func(database *db.DB, bucket int) ([]publishedBlockReferenceRepair, error) {
		return nil, nil
	}
	deletePublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		t.Fatalf("intent processing deleted a repair row: %#v", repair)
		return nil
	}
	now := time.Date(2026, time.September, 15, 10, 0, 0, 0, time.UTC)
	oldNow := publishedBlockReferenceRepairNowFn
	t.Cleanup(func() { publishedBlockReferenceRepairNowFn = oldNow })
	publishedBlockReferenceRepairNowFn = func() time.Time { return now }
	armed := func(fs, block string) publishedBlockReferenceRepair {
		intent := newPublishedBlockReferenceRepair("org-1", "repo-1", "commit-"+fs, fs, []string{block})
		intent.LivenessToken = "token-" + fs
		intent.LivenessArmed = true
		intent.LivenessLeaseExpiresAt = now.Add(5 * time.Minute)
		return intent
	}
	orphan := armed("fs-orphan", "block-o")
	owned := armed("fs-owned", "block-w")
	flaky := armed("fs-flaky", "block-f")
	// blind: the repair row is not visible at EACH_QUORUM because a DC is
	// unavailable — absence is unknown, not proven.
	blind := armed("fs-blind", "block-b")
	// local: the session read still sees the row; no cross-DC read may be spent.
	local := armed("fs-local", "block-l")
	// preparing: the producer may still be writing pins under this token —
	// its lease is live — so the intent is not consumable even though the
	// row is gone; expired: the same, but the lease has passed.
	preparing := armed("fs-preparing", "block-p")
	preparing.LivenessArmed = false
	preparing.LivenessLeaseExpiresAt = now.Add(5 * time.Minute)
	expired := armed("fs-expired", "block-e")
	expired.LivenessArmed = false
	expired.LivenessLeaseExpiresAt = now.Add(-time.Second)
	// stale: listed as PREPARING past its lease like expired, but the
	// producer extended its lease after the listing — the exact-lease freeze
	// does not apply, and the stale observation must authorize nothing.
	stale := armed("fs-stale", "block-s")
	stale.LivenessArmed = false
	stale.LivenessLeaseExpiresAt = now.Add(-time.Second)
	// hollow: armed and row gone, but a replica that has not received the
	// payload yet — never consumable, never deleted.
	hollow := armed("fs-hollow", "block-h")
	hollow.StagedBlockIDs = nil
	// unleased: armed and row gone, but the lease cell is missing — the
	// removal could not be fenced to the producer; never consumed.
	unleased := armed("fs-unleased", "block-u")
	unleased.LivenessLeaseExpiresAt = time.Time{}
	// dupOld/dupNew: two finished producers of one pending identity; only the
	// one with the greatest lease is kept.
	dupOld := armed("fs-owned", "block-w")
	dupOld.LivenessToken = "token-owned-old"
	dupOld.LivenessLeaseExpiresAt = now.Add(-time.Hour)
	listPublishedBlockReferenceRepairLivenessCleanupsForBucketFn = func(database *db.DB, bucket int) ([]publishedBlockReferenceRepair, error) {
		var out []publishedBlockReferenceRepair
		for _, intent := range []publishedBlockReferenceRepair{orphan, owned, dupOld, flaky, blind, local, preparing, expired, stale, hollow, unleased} {
			if intent.Bucket == bucket {
				out = append(out, intent)
			}
		}
		return out, nil
	}
	oldFreeze := freezePublishedBlockReferenceRepairLivenessCleanupFn
	t.Cleanup(func() { freezePublishedBlockReferenceRepairLivenessCleanupFn = oldFreeze })
	frozen := map[string]int{}
	freezePublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, intent publishedBlockReferenceRepair) (bool, error) {
		frozen[intent.FSID]++
		if intent.LivenessArmed {
			t.Fatalf("freeze attempted on an already finished intent %s", intent.FSID)
		}
		if intent.FSID == preparing.FSID {
			t.Fatal("preparing intent under a live lease was frozen: a live producer is never claimed")
		}
		if !intent.LivenessLeaseExpiresAt.Equal(expired.LivenessLeaseExpiresAt) {
			t.Fatalf("freeze of %s conditioned on lease %s, want the exact observed lease", intent.FSID, intent.LivenessLeaseExpiresAt)
		}
		// The stale producer extended after the listing: its stored lease is
		// no longer the observed one.
		return intent.FSID != stale.FSID, nil
	}
	loadPublishedBlockReferenceRepairAuthorityFn = func(database *db.DB, repair publishedBlockReferenceRepair) (publishedBlockReferenceRepair, error) {
		switch repair.FSID {
		case owned.FSID:
			return owned, nil
		case local.FSID:
			t.Fatal("EACH_QUORUM read spent for an intent the local read already retains")
		case blind.FSID:
			return publishedBlockReferenceRepair{}, &gocql.RequestErrUnavailable{Consistency: gocql.EachQuorum, Required: 3, Alive: 2}
		}
		return publishedBlockReferenceRepair{}, gocql.ErrNotFound
	}
	removed := map[string]int{}
	cleanupFailedPublishRemoveAttemptReferencesFn = func(database *db.DB, orgID, attemptID string, blockIDs []string) error {
		removed[attemptID]++
		if attemptID == publishedBlockReferenceRepairLivenessAttemptID(flaky) {
			return fmt.Errorf("delete block_references: write timeout")
		}
		return nil
	}
	oldOwnedPub := removePublishedBlockReferenceRepairOwnedPubFn
	t.Cleanup(func() { removePublishedBlockReferenceRepairOwnedPubFn = oldOwnedPub })
	removePublishedBlockReferenceRepairOwnedPubFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		if repair.LivenessLeaseExpiresAt.IsZero() {
			t.Fatalf("sweep removed pins of %s without a producer lease timestamp", repair.FSID)
		}
		return cleanupFailedPublishRemoveAttemptReferencesFn(database, repair.OrgID, publishedBlockReferenceRepairLivenessAttemptID(repair), repair.StagedBlockIDs)
	}
	// Row gone: a finished witness is RETIRED (CONSUMED), never deleted; the
	// only outright delete of this sweep is the compaction of a pending
	// identity's finished producers (dupOld).
	deletedIntents := map[string]int{}
	deletedTokens := map[string]int{}
	oldRetire := retirePublishedBlockReferenceRepairLivenessCleanupFn
	t.Cleanup(func() { retirePublishedBlockReferenceRepairLivenessCleanupFn = oldRetire })
	retirePublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, repair publishedBlockReferenceRepair, at time.Time) (bool, error) {
		if !at.Equal(now) {
			t.Fatalf("retire of %s stamped %s, want the sweep clock %s", repair.FSID, at, now)
		}
		deletedIntents[repair.FSID]++
		deletedTokens[repair.LivenessToken]++
		return true, nil
	}
	deletePublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		if repair.LivenessToken != dupOld.LivenessToken {
			t.Fatalf("sweep deleted witness %s outright; a finished witness of a gone row must be retired so it keeps re-fencing past gc_grace", repair.LivenessToken)
		}
		deletedTokens[repair.LivenessToken]++
		return nil
	}

	err := runPublishedBlockReferenceRepairSweep(&db.DB{})
	if removed[publishedBlockReferenceRepairLivenessAttemptID(orphan)] != 1 || deletedIntents[orphan.FSID] != 1 {
		t.Fatalf("orphan intent: removed=%d deleted=%d, want 1/1", removed[publishedBlockReferenceRepairLivenessAttemptID(orphan)], deletedIntents[orphan.FSID])
	}
	if err == nil {
		t.Fatal("sweep = nil, want the flaky removal or the unavailable-DC read surfaced")
	}
	if removed[publishedBlockReferenceRepairLivenessAttemptID(owned)] != 0 || deletedTokens[owned.LivenessToken] != 0 {
		t.Fatalf("owned intent: removed=%d deleted=%d, want 0/0 (its pending row owns the pin)", removed[publishedBlockReferenceRepairLivenessAttemptID(owned)], deletedIntents[owned.FSID])
	}
	if removed[publishedBlockReferenceRepairLivenessAttemptID(flaky)] != 1 || deletedIntents[flaky.FSID] != 0 {
		t.Fatalf("flaky intent: removed=%d deleted=%d, want 1/0 (kept for the next sweep)", removed[publishedBlockReferenceRepairLivenessAttemptID(flaky)], deletedIntents[flaky.FSID])
	}
	if removed[publishedBlockReferenceRepairLivenessAttemptID(blind)] != 0 || deletedIntents[blind.FSID] != 0 {
		t.Fatalf("blind intent: removed=%d deleted=%d, want 0/0: an unavailable DC is not proof of absence", removed[publishedBlockReferenceRepairLivenessAttemptID(blind)], deletedIntents[blind.FSID])
	}
	if removed[publishedBlockReferenceRepairLivenessAttemptID(local)] != 0 || deletedIntents[local.FSID] != 0 {
		t.Fatalf("local intent: removed=%d deleted=%d, want 0/0 (session read retains)", removed[publishedBlockReferenceRepairLivenessAttemptID(local)], deletedIntents[local.FSID])
	}
	if removed[publishedBlockReferenceRepairLivenessAttemptID(preparing)] != 0 || deletedIntents[preparing.FSID] != 0 {
		t.Fatalf("preparing intent: removed=%d deleted=%d, want 0/0: its producer may still be writing pins under a live lease", removed[publishedBlockReferenceRepairLivenessAttemptID(preparing)], deletedIntents[preparing.FSID])
	}
	if removed[publishedBlockReferenceRepairLivenessAttemptID(hollow)] != 0 || deletedIntents[hollow.FSID] != 0 {
		t.Fatalf("hollow intent: removed=%d deleted=%d, want 0/0: armed without its payload is never consumed", removed[publishedBlockReferenceRepairLivenessAttemptID(hollow)], deletedIntents[hollow.FSID])
	}
	if deletedTokens[dupOld.LivenessToken] != 1 {
		t.Fatalf("compaction deleted the older finished producer %d times, want 1 (the newest lease shadows its pins)", deletedTokens[dupOld.LivenessToken])
	}
	_ = localReads
	if frozen[stale.FSID] != 1 || removed[publishedBlockReferenceRepairLivenessAttemptID(stale)] != 0 || deletedIntents[stale.FSID] != 0 {
		t.Fatalf("stale preparing intent: frozen=%d removed=%d deleted=%d, want 1/0/0: the producer extended after the listing, so the lost freeze authorizes nothing (no tombstone at the stale lease, witness kept)", frozen[stale.FSID], removed[publishedBlockReferenceRepairLivenessAttemptID(stale)], deletedIntents[stale.FSID])
	}
	if frozen[expired.FSID] != 1 || removed[publishedBlockReferenceRepairLivenessAttemptID(expired)] != 1 || deletedIntents[expired.FSID] != 1 {
		t.Fatalf("expired preparing intent: frozen=%d removed=%d deleted=%d, want 1/1/1: consumed only after winning the exact-lease freeze", frozen[expired.FSID], removed[publishedBlockReferenceRepairLivenessAttemptID(expired)], deletedIntents[expired.FSID])
	}
	if frozen[preparing.FSID] != 0 {
		t.Fatalf("preparing intent under a live lease was frozen %d times, want 0 (a live producer is never claimed)", frozen[preparing.FSID])
	}
	if removed[publishedBlockReferenceRepairLivenessAttemptID(unleased)] != 0 || deletedIntents[unleased.FSID] != 0 {
		t.Fatalf("unleased intent: removed=%d deleted=%d, want 0/0: without its lease the removal cannot be fenced to the producer", removed[publishedBlockReferenceRepairLivenessAttemptID(unleased)], deletedIntents[unleased.FSID])
	}
	if removed[orphan.CommitID] != 0 || removed[flaky.CommitID] != 0 {
		t.Fatal("sweep removed a commit-scoped pub: identity")
	}
}

// A cleanup holding one producer token must not be able to delete the
// witness another producer of the same identity wrote before its own pin —
// whether that other producer is a requeued visit or a concurrent visit of
// the very same repair row (same created_at). Deterministic interleaving:
// A reads Gone -> B visits, writes intent(B) and its pin -> A removes and
// deletes -> intent(B) must survive.
func TestRepairPublishedBlockReferenceRepairCleanupIntentIsProducerBound(t *testing.T) {
	hooks := installRepairVisitOrderHooks(t, nil, publishedBlockReferenceRepairCommitUnknown, nil)
	visitA := newTestPublishedBlockReferenceRepair("commit-1")
	visitA.CreatedAt = time.Date(2026, time.September, 14, 12, 0, 0, 0, time.UTC)
	visitA.LivenessToken = "token-A"
	visitA.LivenessArmed = true
	visitB := visitA // same row, same created_at: a concurrent visit
	visitB.LivenessToken = ""
	hooks.intents[intentKey(visitA)] = true // A armed its witness earlier

	loadPublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) (publishedBlockReferenceRepair, error) {
		if repair.LivenessToken == "token-A" {
			return publishedBlockReferenceRepair{}, gocql.ErrNotFound // A observes the row cleared
		}
		return visitB, nil // B observes the row pending
	}
	loadPublishedBlockReferenceRepairAuthorityFn = loadPublishedBlockReferenceRepairFn
	bRan := false
	cleanupFailedPublishRemoveAttemptReferencesFn = func(database *db.DB, orgID, attemptID string, blockIDs []string) error {
		hooks.removeCalls++
		if !bRan {
			// A has read Gone and is about to remove; B's whole visit lands here.
			bRan = true
			if err := repairPublishedBlockReferenceRepair(&db.DB{}, visitB); err == nil || !strings.Contains(err.Error(), "unknown") {
				t.Fatalf("B visit = %v, want UNKNOWN retention", err)
			}
		}
		return nil
	}

	gone, err := compensatePublishedBlockReferenceRepairLivenessIfGone(&db.DB{}, visitA)
	if err != nil || !gone {
		t.Fatalf("A compensation = (%v, %v), want gone without error", gone, err)
	}
	if _, present := hooks.intents[intentKey(visitA)]; present {
		t.Fatal("A did not delete its own witness")
	}
	survivors := 0
	for key, armed := range hooks.intents {
		if !strings.HasPrefix(key, visitA.FSID+"@") || key == intentKey(visitA) {
			continue
		}
		if !armed {
			t.Fatalf("B's witness %s is still preparing after B finished its fan-out", key)
		}
		survivors++
	}
	if survivors != 1 {
		t.Fatalf("witnesses of other producers surviving A's cleanup = %d, want B's exactly (A's cleanup deleted another producer's witness: its pin would be live without a durable cleanup intent)", survivors)
	}
	if hooks.renewCalls != 1 || hooks.armCalls != 1 {
		t.Fatalf("renew=%d arm=%d, want B's single renewal and arm", hooks.renewCalls, hooks.armCalls)
	}
}

// The producer fence: a visit writes its intent PREPARING with a lease,
// fences its fan-out to stop before that lease, and arms the intent after
// the fan-out. An arm failure fails closed before the walk.
func TestRepairPublishedBlockReferenceRepairProducerFenceLeaseAndArm(t *testing.T) {
	hooks := installRepairVisitOrderHooks(t, nil, publishedBlockReferenceRepairCommitUnknown, nil)
	now := time.Date(2026, time.September, 15, 9, 0, 0, 0, time.UTC)
	oldNow := publishedBlockReferenceRepairNowFn
	t.Cleanup(func() { publishedBlockReferenceRepairNowFn = oldNow })
	publishedBlockReferenceRepairNowFn = func() time.Time { return now }
	if err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1")); err == nil {
		t.Fatal("visit = nil, want UNKNOWN retention")
	}
	wantLease := now.Add(publishedBlockReferenceRepairLivenessLease)
	if !hooks.lastLease.Equal(wantLease) {
		t.Fatalf("producer lease = %s, want now + lease = %s", hooks.lastLease, wantLease)
	}
	intentAt, renewAt, armAt, classifyAt := hooks.index("intent"), hooks.index("renew"), hooks.index("arm"), hooks.index("classify")
	if !(intentAt >= 0 && intentAt < renewAt && renewAt < armAt && armAt < classifyAt) {
		t.Fatalf("events = %v, want intent < renew < arm < classify", hooks.events)
	}

	t.Run("arm failure fails closed", func(t *testing.T) {
		hooks := installRepairVisitOrderHooks(t, nil, publishedBlockReferenceRepairCommitReachable, nil)
		armPublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, repair publishedBlockReferenceRepair) (bool, error) {
			hooks.armCalls++
			return false, fmt.Errorf("arm write timeout")
		}
		err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1"))
		if hooks.classifyCalls != 0 || hooks.promoteCalls != 0 || hooks.deleteCalls != 0 {
			t.Fatalf("classify=%d promote=%d delete=%d, want all 0 after a failed arm", hooks.classifyCalls, hooks.promoteCalls, hooks.deleteCalls)
		}
		if err == nil || !strings.Contains(err.Error(), "arm write timeout") {
			t.Fatalf("visit = %v, want the arm error retained for retry (the lease still bounds the preparing intent)", err)
		}
		if hooks.intentDeletes != 0 {
			t.Fatal("a preparing intent whose arm failed must be left for the lease and the sweep")
		}
	})

	// A refused EXTEND or ARM fences the producer, but the fence alone says
	// nothing about the repair row: a sweeper freezes expired producers of
	// PENDING rows too (to compact them). The row decides: gone -> the
	// producer compensates its own pins and stops; pending -> its pins are
	// covered by the frozen witness and must stay (removing them would take
	// liveness away from a pending row), and the visit retains for a fresh
	// producer. Loads: hydrate, pre-write StillPending, then the gone-check's
	// local read and its EACH_QUORUM escalation.
	t.Run("witness lost mid fan-out with the row gone compensates and stops", func(t *testing.T) {
		hooks := installRepairVisitOrderHooks(t, []bool{true, true, false, false}, publishedBlockReferenceRepairCommitReachable, nil)
		renewPublishedBlockReferenceRepairLivenessFn = func(database *db.DB, repair *publishedBlockReferenceRepair) error {
			hooks.renewCalls++
			return errPublishedBlockReferenceRepairLivenessWitnessLost
		}
		err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1"))
		if err != nil {
			t.Fatalf("visit = %v, want nil: the sweep already owns this identity's cleanup", err)
		}
		if hooks.removeCalls != 1 {
			t.Fatalf("removeCalls = %d, want the producer's own compensation of the pins it wrote", hooks.removeCalls)
		}
		if hooks.armCalls != 0 || hooks.classifyCalls != 0 || hooks.promoteCalls != 0 {
			t.Fatalf("arm=%d classify=%d promote=%d, want all 0", hooks.armCalls, hooks.classifyCalls, hooks.promoteCalls)
		}
	})

	t.Run("witness lost mid fan-out with the row pending retains without removing pins", func(t *testing.T) {
		hooks := installRepairVisitOrderHooks(t, nil, publishedBlockReferenceRepairCommitReachable, nil)
		renewPublishedBlockReferenceRepairLivenessFn = func(database *db.DB, repair *publishedBlockReferenceRepair) error {
			hooks.renewCalls++
			// A sweeper froze this producer (its lease looked expired) while
			// the row is still pending.
			hooks.intents[intentKey(*repair)] = true
			return errPublishedBlockReferenceRepairLivenessWitnessLost
		}
		err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1"))
		if hooks.removeCalls != 0 || hooks.intentDeletes != 0 {
			t.Fatalf("remove=%d intentDeletes=%d, want 0/0: the frozen witness covers the pins of a pending row; a fenced producer must not take its liveness away", hooks.removeCalls, hooks.intentDeletes)
		}
		if err == nil || !errors.Is(err, errPublishedBlockReferenceRepairLivenessWitnessLost) {
			t.Fatalf("visit = %v, want the fence error retained for a fresh producer", err)
		}
		if hooks.armCalls != 0 || hooks.classifyCalls != 0 || hooks.promoteCalls != 0 || hooks.deleteCalls != 0 {
			t.Fatalf("arm=%d classify=%d promote=%d delete=%d, want all 0", hooks.armCalls, hooks.classifyCalls, hooks.promoteCalls, hooks.deleteCalls)
		}
	})

	t.Run("arm not applied with the row gone means the witness was consumed", func(t *testing.T) {
		hooks := installRepairVisitOrderHooks(t, []bool{true, true, false, false}, publishedBlockReferenceRepairCommitReachable, nil)
		renewPublishedBlockReferenceRepairLivenessFn = func(database *db.DB, repair *publishedBlockReferenceRepair) error {
			hooks.renewCalls++
			// The sweep consumed the witness (row gone, freeze won) while
			// the fan-out ran: it is absent when the conditional ARM runs.
			delete(hooks.intents, intentKey(*repair))
			return nil
		}
		err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1"))
		if err != nil {
			t.Fatalf("visit = %v, want nil terminal", err)
		}
		if hooks.index("arm-not-applied") < 0 {
			t.Fatalf("events = %v, want a conditional ARM that did not apply", hooks.events)
		}
		if hooks.removeCalls != 1 {
			t.Fatalf("removeCalls = %d, want the producer's compensation after its witness was consumed", hooks.removeCalls)
		}
		if hooks.intentInserts != 1 || hooks.armCalls != 1 {
			t.Fatal("a not-applied ARM must never write the intent back (no resurrection)")
		}
		if hooks.classifyCalls != 0 || hooks.promoteCalls != 0 {
			t.Fatalf("classify=%d promote=%d, want 0/0", hooks.classifyCalls, hooks.promoteCalls)
		}
	})

	t.Run("arm not applied with the row pending means the producer was frozen", func(t *testing.T) {
		hooks := installRepairVisitOrderHooks(t, nil, publishedBlockReferenceRepairCommitReachable, nil)
		armPublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, repair publishedBlockReferenceRepair) (bool, error) {
			hooks.armCalls++
			hooks.events = append(hooks.events, "arm-not-applied")
			return false, nil // frozen by a sweeper: armed is already true
		}
		err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1"))
		if hooks.renewCalls != 1 || hooks.removeCalls != 0 || hooks.intentDeletes != 0 || hooks.classifyCalls != 0 {
			t.Fatalf("renew=%d remove=%d intentDeletes=%d classify=%d, want 1/0/0/0: pins of a pending row stay under the frozen witness", hooks.renewCalls, hooks.removeCalls, hooks.intentDeletes, hooks.classifyCalls)
		}
		if err == nil || !errors.Is(err, errPublishedBlockReferenceRepairLivenessWitnessLost) {
			t.Fatalf("visit = %v, want the fence error retained", err)
		}
	})
}

// M29 — the sweeper wins the exact-lease freeze: from then on the producer's
// EXTEND and ARM must both fail and it must write no pin under a newer lease.
// The intent store is shared between the sweep and the real fan-out
// primitive so the CAS semantics are exercised, not assumed.
func TestPublishedBlockReferenceRepairFreezeWinsAgainstProducerExtendAndArm(t *testing.T) {
	type intentRow struct {
		armed bool
		lease time.Time
	}
	store := map[string]*intentRow{}
	oldFreeze := freezePublishedBlockReferenceRepairLivenessCleanupFn
	oldExtend := extendPublishedBlockReferenceRepairLivenessCleanupFn
	oldArm := armPublishedBlockReferenceRepairLivenessCleanupFn
	oldWrite := writePublishedBlockReferenceRepairLivenessPinFn
	oldNow := publishedBlockReferenceRepairNowFn
	t.Cleanup(func() {
		freezePublishedBlockReferenceRepairLivenessCleanupFn = oldFreeze
		extendPublishedBlockReferenceRepairLivenessCleanupFn = oldExtend
		armPublishedBlockReferenceRepairLivenessCleanupFn = oldArm
		writePublishedBlockReferenceRepairLivenessPinFn = oldWrite
		publishedBlockReferenceRepairNowFn = oldNow
	})
	cas := func(token string, wantArmed bool, wantLease time.Time, apply func(*intentRow)) bool {
		row, ok := store[token]
		if !ok || row.armed != wantArmed || !row.lease.Equal(wantLease) {
			return false
		}
		apply(row)
		return true
	}
	freezePublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, intent publishedBlockReferenceRepair) (bool, error) {
		return cas(intent.LivenessToken, false, intent.LivenessLeaseExpiresAt, func(r *intentRow) { r.armed = true }), nil
	}
	extendPublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, repair publishedBlockReferenceRepair, next time.Time) (bool, error) {
		return cas(repair.LivenessToken, false, repair.LivenessLeaseExpiresAt, func(r *intentRow) { r.lease = next }), nil
	}
	armPublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, repair publishedBlockReferenceRepair) (bool, error) {
		row, ok := store[repair.LivenessToken]
		if !ok || row.armed {
			return false, nil
		}
		row.armed = true
		row.lease = repair.LivenessLeaseExpiresAt
		return true, nil
	}
	var pinTS []int64
	writePublishedBlockReferenceRepairLivenessPinFn = func(database *db.DB, orgID, repoID, attemptID, blockID string, ts int64) error {
		pinTS = append(pinTS, ts)
		return nil
	}
	start := time.Date(2026, time.September, 16, 9, 0, 0, 0, time.UTC)
	l1 := start.Add(publishedBlockReferenceRepairLivenessLease)
	visit := newTestPublishedBlockReferenceRepair("commit-1")
	producer := &visit
	producer.StagedBlockIDs = []string{"b1", "b2", "b3"}
	producer.LivenessToken = "token-frozen"
	producer.LivenessLeaseExpiresAt = l1
	store[producer.LivenessToken] = &intentRow{lease: l1}

	// The producer wrote b1 under L1 and stalled past its lease; a sweeper
	// whose listing shows PREPARING(L1) expired claims it.
	publishedBlockReferenceRepairNowFn = func() time.Time { return start }
	pinTS = nil
	frozen, err := freezePublishedBlockReferenceRepairLivenessCleanupFn(&db.DB{}, publishedBlockReferenceRepair{LivenessToken: producer.LivenessToken, LivenessLeaseExpiresAt: l1})
	if err != nil || !frozen {
		t.Fatalf("freeze = %v/%v, want applied on the exact observed lease", frozen, err)
	}
	// The producer resumes past L1: its EXTEND is the same exact-lease CAS
	// and must lose; it writes nothing under a newer lease.
	publishedBlockReferenceRepairNowFn = func() time.Time { return l1.Add(time.Minute) }
	err = renewPublishedBlockReferenceRepairLivenessFn(&db.DB{}, producer)
	if len(pinTS) != 0 {
		t.Fatalf("pins written after the freeze = %v, want none: a frozen producer must not write under a newer lease", pinTS)
	}
	if !errors.Is(err, errPublishedBlockReferenceRepairLivenessWitnessLost) {
		t.Fatalf("fan-out after the freeze = %v, want errPublishedBlockReferenceRepairLivenessWitnessLost", err)
	}
	if !producer.LivenessLeaseExpiresAt.Equal(l1) {
		t.Fatalf("producer lease after the refused EXTEND = %s, want L1 unchanged", producer.LivenessLeaseExpiresAt)
	}
	if applied, err := armPublishedBlockReferenceRepairLivenessCleanupFn(&db.DB{}, *producer); err != nil || applied {
		t.Fatalf("ARM after the freeze = %v/%v, want not applied", applied, err)
	}
	if row := store[producer.LivenessToken]; !row.armed || !row.lease.Equal(l1) {
		t.Fatalf("frozen intent = %+v, want armed at exactly L1 so a tombstone at L1 covers every pin of this producer", *row)
	}

	// M28 mirror on the same store: a sweeper holding a stale PREPARING(L1)
	// snapshot after the producer extended to L2 must lose its claim.
	store["token-live"] = &intentRow{lease: l1}
	live := &publishedBlockReferenceRepair{LivenessToken: "token-live", LivenessLeaseExpiresAt: l1, StagedBlockIDs: []string{"b1"}, FSID: "fs-live"}
	l2 := l1.Add(publishedBlockReferenceRepairLivenessLease)
	if applied, err := extendPublishedBlockReferenceRepairLivenessCleanupFn(&db.DB{}, *live, l2); err != nil || !applied {
		t.Fatalf("EXTEND L1->L2 = %v/%v, want applied", applied, err)
	}
	if frozen, err := freezePublishedBlockReferenceRepairLivenessCleanupFn(&db.DB{}, publishedBlockReferenceRepair{LivenessToken: "token-live", LivenessLeaseExpiresAt: l1}); err != nil || frozen {
		t.Fatalf("freeze on the stale L1 snapshot = %v/%v, want not applied: the producer holds L2", frozen, err)
	}
	if row := store["token-live"]; row.armed || !row.lease.Equal(l2) {
		t.Fatalf("live intent after the stale freeze = %+v, want PREPARING(L2) untouched", *row)
	}
}

// M30 — abandoned PREPARING producers of a PENDING identity do not accumulate:
// each expired one is frozen (claimed) and folded into the finished-producer
// compaction, the live one is preserved, and the kept witness holds the
// greatest lease so its eventual tombstone covers every prior producer's pins.
func TestPublishedBlockReferenceRepairSweepBoundsAbandonedPreparingProducers(t *testing.T) {
	oldList := listPublishedBlockReferenceRepairsForBucketFn
	oldListIntents := listPublishedBlockReferenceRepairLivenessCleanupsForBucketFn
	oldLoad := loadPublishedBlockReferenceRepairFn
	oldAuthority := loadPublishedBlockReferenceRepairAuthorityFn
	oldFreeze := freezePublishedBlockReferenceRepairLivenessCleanupFn
	oldDeleteIntent := deletePublishedBlockReferenceRepairLivenessCleanupFn
	oldOwnedPub := removePublishedBlockReferenceRepairOwnedPubFn
	oldNow := publishedBlockReferenceRepairNowFn
	t.Cleanup(func() {
		listPublishedBlockReferenceRepairsForBucketFn = oldList
		listPublishedBlockReferenceRepairLivenessCleanupsForBucketFn = oldListIntents
		loadPublishedBlockReferenceRepairFn = oldLoad
		loadPublishedBlockReferenceRepairAuthorityFn = oldAuthority
		freezePublishedBlockReferenceRepairLivenessCleanupFn = oldFreeze
		deletePublishedBlockReferenceRepairLivenessCleanupFn = oldDeleteIntent
		removePublishedBlockReferenceRepairOwnedPubFn = oldOwnedPub
		publishedBlockReferenceRepairNowFn = oldNow
	})
	now := time.Date(2026, time.September, 16, 10, 0, 0, 0, time.UTC)
	publishedBlockReferenceRepairNowFn = func() time.Time { return now }
	listPublishedBlockReferenceRepairsForBucketFn = func(database *db.DB, bucket int) ([]publishedBlockReferenceRepair, error) { return nil, nil }
	// The identity is PENDING: the session read retains, no authority read.
	loadPublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) (publishedBlockReferenceRepair, error) {
		return repair, nil
	}
	loadPublishedBlockReferenceRepairAuthorityFn = func(database *db.DB, repair publishedBlockReferenceRepair) (publishedBlockReferenceRepair, error) {
		t.Fatal("EACH_QUORUM read spent for a locally pending identity")
		return repair, nil
	}
	removePublishedBlockReferenceRepairOwnedPubFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		t.Fatalf("sweep removed pins of a pending identity (token %s)", repair.LivenessToken)
		return nil
	}
	base := newPublishedBlockReferenceRepair("org-1", "repo-1", "commit-1", "fs-1", []string{"block-1"})
	producer := func(token string, armed bool, lease time.Time) publishedBlockReferenceRepair {
		intent := base
		intent.LivenessToken = token
		intent.LivenessArmed = armed
		intent.LivenessLeaseExpiresAt = lease
		return intent
	}
	var intents []publishedBlockReferenceRepair
	// Ten producers abandoned mid fan-out over the last ten hours, leases
	// long expired, still PREPARING.
	for i := 1; i <= 10; i++ {
		intents = append(intents, producer(fmt.Sprintf("abandoned-%d", i), false, now.Add(-time.Duration(i)*time.Hour)))
	}
	// One finished producer (armed) with a lease that is not the greatest.
	intents = append(intents, producer("finished", true, now.Add(-30*time.Minute)))
	// The greatest lease belongs to an abandoned producer: after the freeze
	// it is the witness that covers every other one's pins.
	intents = append(intents, producer("abandoned-newest", false, now.Add(-time.Minute)))
	// One live producer still inside its lease: never claimed.
	intents = append(intents, producer("live", false, now.Add(5*time.Minute)))
	listPublishedBlockReferenceRepairLivenessCleanupsForBucketFn = func(database *db.DB, bucket int) ([]publishedBlockReferenceRepair, error) {
		if bucket != base.Bucket {
			return nil, nil
		}
		return intents, nil
	}
	frozen := map[string]bool{}
	freezePublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, intent publishedBlockReferenceRepair) (bool, error) {
		if intent.LivenessToken == "live" {
			t.Fatal("freeze attempted on a producer under a live lease")
		}
		frozen[intent.LivenessToken] = true
		return true, nil
	}
	deleted := map[string]bool{}
	deletePublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		deleted[repair.LivenessToken] = true
		return nil
	}
	if err := runPublishedBlockReferenceRepairSweep(&db.DB{}); err != nil {
		t.Fatalf("sweep = %v, want nil", err)
	}
	if len(frozen) != 11 {
		t.Fatalf("frozen = %v, want all 11 expired PREPARING producers claimed", frozen)
	}
	if deleted["live"] || deleted["abandoned-newest"] {
		t.Fatalf("deleted = %v: the live producer and the greatest-lease witness must survive", deleted)
	}
	if !deleted["finished"] {
		t.Fatal("the armed producer with a smaller lease was not compacted into the greatest-lease witness")
	}
	for i := 1; i <= 10; i++ {
		if !deleted[fmt.Sprintf("abandoned-%d", i)] {
			t.Fatalf("abandoned-%d was not compacted: expired PREPARING intents would accumulate forever while the repair is pending", i)
		}
	}
	// Bound: what survives is one finished witness (greatest lease) plus the
	// live producer — independent of how many producers were abandoned.
	survivors := 0
	for _, intent := range intents {
		if !deleted[intent.LivenessToken] {
			survivors++
		}
	}
	if survivors != 2 {
		t.Fatalf("survivors = %d, want 2 (greatest-lease witness + live producer)", survivors)
	}
}

// The real fan-out primitive: every pin carries the producer lease as its
// write timestamp, the lease is renewed through a conditional LWT before it
// runs out so an arbitrarily long fan-out converges, and a renewal that does
// not apply stops the producer without another write.
func TestRenewPublishedBlockReferenceRepairLivenessFanOutRenewsLeaseAndTimestampsPins(t *testing.T) {
	oldNow := publishedBlockReferenceRepairNowFn
	oldWrite := writePublishedBlockReferenceRepairLivenessPinFn
	oldExtend := extendPublishedBlockReferenceRepairLivenessCleanupFn
	t.Cleanup(func() {
		publishedBlockReferenceRepairNowFn = oldNow
		writePublishedBlockReferenceRepairLivenessPinFn = oldWrite
		extendPublishedBlockReferenceRepairLivenessCleanupFn = oldExtend
	})
	start := time.Date(2026, time.September, 16, 8, 0, 0, 0, time.UTC)
	clock := start
	publishedBlockReferenceRepairNowFn = func() time.Time { return clock }
	type write struct {
		block string
		ts    int64
	}
	var writes []write
	writePublishedBlockReferenceRepairLivenessPinFn = func(database *db.DB, orgID, repoID, attemptID, blockID string, ts int64) error {
		writes = append(writes, write{blockID, ts})
		clock = clock.Add(4 * time.Minute) // each per-block write costs 4 minutes
		return nil
	}
	var extensions []time.Time
	extendPublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, repair publishedBlockReferenceRepair, nextLease time.Time) (bool, error) {
		extensions = append(extensions, nextLease)
		return true, nil
	}
	visit := newTestPublishedBlockReferenceRepair("commit-1")
	repair := &visit
	repair.StagedBlockIDs = []string{"b1", "b2", "b3", "b4", "b5"}
	repair.LivenessToken = "token-1"
	repair.LivenessLeaseExpiresAt = start.Add(publishedBlockReferenceRepairLivenessLease)
	if err := renewPublishedBlockReferenceRepairLivenessFn(&db.DB{}, repair); err != nil {
		t.Fatalf("fan-out = %v, want convergence across leases", err)
	}
	if len(writes) != 5 {
		t.Fatalf("writes = %v, want all five blocks (a hard deadline would have cut the fan-out)", writes)
	}
	// 10-minute lease, 1-minute skew: renewal once less than 2 minutes remain.
	// t=0 (10 left), t=4 (6 left), t=8 (2 left -> renew to 18), t=12 (6 left),
	// t=16 (2 left -> renew to 26).
	if len(extensions) != 2 || !extensions[0].Equal(start.Add(18*time.Minute)) || !extensions[1].Equal(start.Add(26*time.Minute)) {
		t.Fatalf("lease extensions = %v, want [+18m, +26m]", extensions)
	}
	lease0 := publishedBlockReferenceRepairLeaseTimestamp(start.Add(10 * time.Minute))
	lease1 := publishedBlockReferenceRepairLeaseTimestamp(start.Add(18 * time.Minute))
	lease2 := publishedBlockReferenceRepairLeaseTimestamp(start.Add(26 * time.Minute))
	wantTS := []int64{lease0, lease0, lease1, lease1, lease2}
	for i, w := range writes {
		if w.ts != wantTS[i] {
			t.Fatalf("write %d (%s) timestamp = %d, want the lease current at that write %d", i, w.block, w.ts, wantTS[i])
		}
	}
	if !repair.LivenessLeaseExpiresAt.Equal(start.Add(26 * time.Minute)) {
		t.Fatalf("repair lease after fan-out = %s, want the last renewed lease", repair.LivenessLeaseExpiresAt)
	}

	// A renewal that does not apply (witness consumed) stops the producer
	// before the next write.
	writes = nil
	extensions = nil
	clock = start
	extendPublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, repair publishedBlockReferenceRepair, nextLease time.Time) (bool, error) {
		return false, nil
	}
	repair.LivenessLeaseExpiresAt = start.Add(publishedBlockReferenceRepairLivenessLease)
	err := renewPublishedBlockReferenceRepairLivenessFn(&db.DB{}, repair)
	if !errors.Is(err, errPublishedBlockReferenceRepairLivenessWitnessLost) {
		t.Fatalf("fan-out after a lost witness = %v, want errPublishedBlockReferenceRepairLivenessWitnessLost", err)
	}
	if len(writes) != 2 {
		t.Fatalf("writes after the lost witness = %v, want exactly the two written before the renewal was refused", writes)
	}
}

// A local absence is never destructive authority for a visit either: the
// visit's earlier observation may have been coordinated in another DC. Local
// NotFound must be escalated to the EACH_QUORUM authority read, and a row
// found there keeps the pin and the intent.
func TestRepairPublishedBlockReferenceRepairLocalAbsenceEscalatesToAuthority(t *testing.T) {
	t.Run("authority still sees the row", func(t *testing.T) {
		hooks := installRepairVisitOrderHooks(t, []bool{true, true, true, false}, publishedBlockReferenceRepairCommitNoLongerPending, errPublishedBlockReferenceRepairGone)
		authorityReads := 0
		loadPublishedBlockReferenceRepairAuthorityFn = func(database *db.DB, repair publishedBlockReferenceRepair) (publishedBlockReferenceRepair, error) {
			authorityReads++
			return repair, nil
		}
		if err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1")); err != nil {
			t.Fatalf("visit = %v, want nil (retained)", err)
		}
		if authorityReads != 1 {
			t.Fatalf("authorityReads = %d, want the local absence escalated exactly once", authorityReads)
		}
		if hooks.removeCalls != 0 || hooks.intentDeletes != 0 {
			t.Fatalf("remove=%d intentDeletes=%d, want 0/0: a row visible at EACH_QUORUM keeps its pin and witness", hooks.removeCalls, hooks.intentDeletes)
		}
	})
	t.Run("authority unavailable", func(t *testing.T) {
		hooks := installRepairVisitOrderHooks(t, []bool{true, true, true, false}, publishedBlockReferenceRepairCommitNoLongerPending, errPublishedBlockReferenceRepairGone)
		loadPublishedBlockReferenceRepairAuthorityFn = func(database *db.DB, repair publishedBlockReferenceRepair) (publishedBlockReferenceRepair, error) {
			return publishedBlockReferenceRepair{}, &gocql.RequestErrUnavailable{Consistency: gocql.EachQuorum, Required: 3, Alive: 2}
		}
		err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1"))
		if err == nil || !strings.Contains(err.Error(), "EACH_QUORUM") {
			t.Fatalf("visit = %v, want the authority failure surfaced", err)
		}
		if hooks.removeCalls != 0 || hooks.intentDeletes != 0 {
			t.Fatalf("remove=%d intentDeletes=%d, want 0/0 (fail closed)", hooks.removeCalls, hooks.intentDeletes)
		}
	})
	t.Run("local presence retains without a cross-DC read", func(t *testing.T) {
		hooks := installRepairVisitOrderHooks(t, nil, publishedBlockReferenceRepairCommitUnknown, nil)
		loadPublishedBlockReferenceRepairAuthorityFn = func(database *db.DB, repair publishedBlockReferenceRepair) (publishedBlockReferenceRepair, error) {
			t.Fatal("EACH_QUORUM read spent although the local read already retains")
			return repair, nil
		}
		if err := repairPublishedBlockReferenceRepair(&db.DB{}, newTestPublishedBlockReferenceRepair("commit-1")); err == nil {
			t.Fatal("visit = nil, want UNKNOWN retention")
		}
		if hooks.removeCalls != 0 {
			t.Fatalf("removeCalls = %d, want 0", hooks.removeCalls)
		}
	})
}

// The re-fence schedule must keep a live tombstone at the producer lease for
// the whole retention: two intervals must fit inside gc_grace_seconds of
// block_references (so a missed sweep tick cannot leave a purge window), and
// the retention must cover the pin TTL (a write can only escape the fence if
// its producer was suspended longer than the pin it would write lives).
func TestPublishedBlockReferenceRepairRefenceIntervalBeatsGCGrace(t *testing.T) {
	if 2*publishedBlockReferenceRepairLivenessRefenceInterval > publishedBlockReferenceRepairBlockReferencesGCGrace {
		t.Fatalf("refence interval %s: two intervals must fit inside block_references gc_grace %s", publishedBlockReferenceRepairLivenessRefenceInterval, publishedBlockReferenceRepairBlockReferencesGCGrace)
	}
	if publishedBlockReferenceRepairLivenessConsumedRetention < time.Duration(db.PublishAttemptReferenceTTLSeconds)*time.Second {
		t.Fatalf("consumed retention %s must cover the pub: pin TTL %ds", publishedBlockReferenceRepairLivenessConsumedRetention, db.PublishAttemptReferenceTTLSeconds)
	}
	if publishedBlockReferenceRepairBlockReferencesGCGrace != 10*24*time.Hour {
		t.Fatal("block_references gc_grace constant must track migration 026 (gc_grace_seconds = 864000); change both together")
	}
}

// M33/M34 — a retired (CONSUMED) witness is not forgotten: the sweep leaves
// it alone before its refence interval, re-tombstones its producer pins at
// the producer lease and records the round once the interval has passed,
// deletes it only after the retention, and never spends a gone check, a
// freeze or a compaction on it.
func TestPublishedBlockReferenceRepairSweepRefencesRetiredWitnesses(t *testing.T) {
	oldList := listPublishedBlockReferenceRepairsForBucketFn
	oldListIntents := listPublishedBlockReferenceRepairLivenessCleanupsForBucketFn
	oldLoad := loadPublishedBlockReferenceRepairFn
	oldAuthority := loadPublishedBlockReferenceRepairAuthorityFn
	oldFreeze := freezePublishedBlockReferenceRepairLivenessCleanupFn
	oldDeleteIntent := deletePublishedBlockReferenceRepairLivenessCleanupFn
	oldRetire := retirePublishedBlockReferenceRepairLivenessCleanupFn
	oldRefence := refencePublishedBlockReferenceRepairLivenessCleanupFn
	oldOwnedPub := removePublishedBlockReferenceRepairOwnedPubFn
	oldNow := publishedBlockReferenceRepairNowFn
	t.Cleanup(func() {
		listPublishedBlockReferenceRepairsForBucketFn = oldList
		listPublishedBlockReferenceRepairLivenessCleanupsForBucketFn = oldListIntents
		loadPublishedBlockReferenceRepairFn = oldLoad
		loadPublishedBlockReferenceRepairAuthorityFn = oldAuthority
		freezePublishedBlockReferenceRepairLivenessCleanupFn = oldFreeze
		deletePublishedBlockReferenceRepairLivenessCleanupFn = oldDeleteIntent
		retirePublishedBlockReferenceRepairLivenessCleanupFn = oldRetire
		refencePublishedBlockReferenceRepairLivenessCleanupFn = oldRefence
		removePublishedBlockReferenceRepairOwnedPubFn = oldOwnedPub
		publishedBlockReferenceRepairNowFn = oldNow
	})
	listPublishedBlockReferenceRepairsForBucketFn = func(database *db.DB, bucket int) ([]publishedBlockReferenceRepair, error) { return nil, nil }
	loadPublishedBlockReferenceRepairFn = func(database *db.DB, repair publishedBlockReferenceRepair) (publishedBlockReferenceRepair, error) {
		t.Fatal("gone check spent on an identity that only has a retired witness")
		return repair, nil
	}
	loadPublishedBlockReferenceRepairAuthorityFn = loadPublishedBlockReferenceRepairFn
	freezePublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, intent publishedBlockReferenceRepair) (bool, error) {
		t.Fatal("freeze attempted on a retired witness")
		return false, nil
	}
	retirePublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, repair publishedBlockReferenceRepair, now time.Time) (bool, error) {
		t.Fatal("retire attempted on an already retired witness")
		return false, nil
	}
	consumedAt := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	lease := consumedAt.Add(-time.Minute)
	retired := newPublishedBlockReferenceRepair("org-1", "repo-1", "commit-1", "fs-1", []string{"block-1"})
	retired.LivenessToken = "token-retired"
	retired.LivenessArmed = true
	retired.LivenessLeaseExpiresAt = lease
	retired.LivenessConsumedAt = consumedAt
	retired.LivenessRefencedAt = consumedAt
	listPublishedBlockReferenceRepairLivenessCleanupsForBucketFn = func(database *db.DB, bucket int) ([]publishedBlockReferenceRepair, error) {
		if bucket != retired.Bucket {
			return nil, nil
		}
		return []publishedBlockReferenceRepair{retired}, nil
	}
	var removedAt []int64
	var removeErr error
	removePublishedBlockReferenceRepairOwnedPubFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		if repair.LivenessToken != retired.LivenessToken {
			t.Fatalf("re-fence for token %s, want the retired witness", repair.LivenessToken)
		}
		removedAt = append(removedAt, publishedBlockReferenceRepairLeaseTimestamp(repair.LivenessLeaseExpiresAt))
		return removeErr
	}
	var refencedAt []time.Time
	refencePublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, intent publishedBlockReferenceRepair, now time.Time) (bool, error) {
		if !intent.LivenessRefencedAt.Equal(retired.LivenessRefencedAt) {
			t.Fatalf("re-fence conditioned on refenced_at %s, want the observed %s", intent.LivenessRefencedAt, retired.LivenessRefencedAt)
		}
		refencedAt = append(refencedAt, now)
		retired.LivenessRefencedAt = now
		return true, nil
	}
	deleted := 0
	deletePublishedBlockReferenceRepairLivenessCleanupFn = func(database *db.DB, repair publishedBlockReferenceRepair) error {
		deleted++
		return nil
	}
	sweepAt := func(now time.Time) error {
		t.Helper()
		publishedBlockReferenceRepairNowFn = func() time.Time { return now }
		return runPublishedBlockReferenceRepairSweep(&db.DB{})
	}
	// Before the interval: nothing.
	if err := sweepAt(consumedAt.Add(publishedBlockReferenceRepairLivenessRefenceInterval / 2)); err != nil {
		t.Fatalf("early sweep = %v, want nil", err)
	}
	if len(removedAt) != 0 || len(refencedAt) != 0 || deleted != 0 {
		t.Fatalf("early sweep touched the retired witness: removed=%v refenced=%v deleted=%d", removedAt, refencedAt, deleted)
	}
	// At the interval: re-tombstone at the producer lease, record the round.
	due := consumedAt.Add(publishedBlockReferenceRepairLivenessRefenceInterval)
	if err := sweepAt(due); err != nil {
		t.Fatalf("due sweep = %v, want nil", err)
	}
	if len(removedAt) != 1 || removedAt[0] != publishedBlockReferenceRepairLeaseTimestamp(lease) || len(refencedAt) != 1 || !refencedAt[0].Equal(due) || deleted != 0 {
		t.Fatalf("due sweep: removed=%v refenced=%v deleted=%d, want one re-fence at the producer lease recorded at the sweep clock (a tombstone purged after gc_grace no longer shadows a suspended producer write)", removedAt, refencedAt, deleted)
	}
	// A second round on the next interval; none in between.
	_ = sweepAt(due.Add(publishedBlockReferenceRepairLivenessRefenceInterval / 2))
	_ = sweepAt(due.Add(publishedBlockReferenceRepairLivenessRefenceInterval))
	if len(removedAt) != 2 || len(refencedAt) != 2 {
		t.Fatalf("second interval: removed=%v refenced=%v, want exactly two rounds", removedAt, refencedAt)
	}
	// After the retention: one MANDATORY final fence, then deleted. Sweeps may
	// have been absent for longer than gc_grace (here: since the last round),
	// so a late pin may sit under a purged tombstone; the witness is the last
	// cleanup root and must not vanish before that fence succeeded.
	removeErr = errors.New("EACH_QUORUM tombstone: dc-asia unavailable")
	failedErr := sweepAt(consumedAt.Add(publishedBlockReferenceRepairLivenessConsumedRetention))
	if deleted != 0 || len(removedAt) != 3 || len(refencedAt) != 2 {
		t.Fatalf("retention sweep with the final fence failing: deleted=%d removed=%v refenced=%v, want the fence attempted, the witness retained and refenced_at untouched", deleted, removedAt, refencedAt)
	}
	if failedErr == nil {
		t.Fatal("a failed final fence must surface as a sweep error")
	}
	removeErr = nil
	if err := sweepAt(consumedAt.Add(publishedBlockReferenceRepairLivenessConsumedRetention + time.Minute)); err != nil {
		t.Fatalf("retention sweep = %v, want nil", err)
	}
	if deleted != 1 || len(removedAt) != 4 || removedAt[3] != publishedBlockReferenceRepairLeaseTimestamp(lease) || len(refencedAt) != 2 {
		t.Fatalf("retention sweep: deleted=%d removed=%v refenced=%v, want the final fence at the producer lease and then exactly one delete", deleted, removedAt, refencedAt)
	}
}
