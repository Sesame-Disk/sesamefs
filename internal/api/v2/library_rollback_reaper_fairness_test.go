package v2

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

func restoreLibraryRollbackScanHooks(t *testing.T) {
	t.Helper()
	list := listLibraryRollbackPendingAfterFn
	recoverFn := recoverPendingLibraryRollbackFn
	t.Cleanup(func() {
		listLibraryRollbackPendingAfterFn = list
		recoverPendingLibraryRollbackFn = recoverFn
	})
}

func TestRecoverPendingLibraryRollbacksDoesNotStarveLaterMarkerInSameBucket(t *testing.T) {
	restoreLibraryRollbackScanHooks(t)

	failCount := libraryRollbackRecoveryMaxPerSweep
	total := failCount + 1
	rows := make([]db.LibraryRollbackPending, 0, total)
	for i := 0; i < total; i++ {
		rows = append(rows, db.LibraryRollbackPending{
			RecoveryBucket: 0,
			OrgID:          fmt.Sprintf("org-%04d", i),
			LibraryID:      fmt.Sprintf("lib-%04d", i),
		})
	}
	later := rows[failCount]
	installLibraryRollbackPendingListStub(t, rows)

	attempts := map[string]int{}
	recoverPendingLibraryRollbackFn = func(_ *gocql.Session, pending db.LibraryRollbackPending) error {
		attempts[pending.LibraryID]++
		if pending.LibraryID == later.LibraryID {
			return nil
		}
		return errors.New("persistent recovery failure")
	}

	ctx := context.Background()
	state, err := recoverPendingLibraryRollbacksFrom(ctx, nil, libraryRollbackRecoveryState{})
	if err == nil {
		t.Fatal("first sweep must surface the persistent prefix failures")
	}
	if attempts[later.LibraryID] != 0 {
		t.Fatal("first bounded sweep must spend its budget on the failing prefix, not the later marker")
	}
	if got := attempts[rows[0].LibraryID]; got != 1 {
		t.Fatalf("first sweep attempts for prefix[0] = %d, want 1", got)
	}

	if !libraryRollbackRecoveryEventuallyAttempts(t, ctx, state, later.LibraryID, attempts) {
		t.Fatalf("later recoverable marker %s was starved by %d persistent failures", later.LibraryID, failCount)
	}
}

func TestRecoverPendingLibraryRollbacksDoesNotStarveLaterBucket(t *testing.T) {
	restoreLibraryRollbackScanHooks(t)

	failCount := libraryRollbackRecoveryMaxPerSweep
	var rows []db.LibraryRollbackPending
	for i := 0; i < failCount; i++ {
		rows = append(rows, db.LibraryRollbackPending{
			RecoveryBucket: 0,
			OrgID:          fmt.Sprintf("org-%04d", i),
			LibraryID:      fmt.Sprintf("lib-%04d", i),
		})
	}
	later := db.LibraryRollbackPending{
		RecoveryBucket: 1,
		OrgID:          "org-later",
		LibraryID:      "lib-later",
	}
	rows = append(rows, later)
	installLibraryRollbackPendingListStub(t, rows)

	attempts := map[string]int{}
	recoverPendingLibraryRollbackFn = func(_ *gocql.Session, pending db.LibraryRollbackPending) error {
		attempts[pending.LibraryID]++
		if pending.LibraryID == later.LibraryID {
			return nil
		}
		return errors.New("persistent recovery failure")
	}

	ctx := context.Background()
	state, err := recoverPendingLibraryRollbacksFrom(ctx, nil, libraryRollbackRecoveryState{})
	if err == nil {
		t.Fatal("first sweep must surface the persistent bucket-0 failures")
	}
	if attempts[later.LibraryID] != 0 {
		t.Fatal("first sweep exhausted in bucket 0 must not have reached bucket 1")
	}

	if !libraryRollbackRecoveryEventuallyAttempts(t, ctx, state, later.LibraryID, attempts) {
		t.Fatal("recoverable marker in bucket 1 was starved by a failing prefix in bucket 0")
	}
}

func installLibraryRollbackPendingListStub(t *testing.T, rows []db.LibraryRollbackPending) {
	t.Helper()
	listLibraryRollbackPendingAfterFn = func(_ *gocql.Session, bucket int, afterOrgID, afterLibraryID string, pageSize int) ([]db.LibraryRollbackPending, error) {
		var out []db.LibraryRollbackPending
		for _, row := range rows {
			if row.RecoveryBucket != bucket {
				continue
			}
			if !libraryRollbackPendingClusteringAfter(row, afterOrgID, afterLibraryID) {
				continue
			}
			out = append(out, row)
			if len(out) >= pageSize {
				break
			}
		}
		return out, nil
	}
}

func libraryRollbackPendingClusteringAfter(row db.LibraryRollbackPending, afterOrgID, afterLibraryID string) bool {
	if afterOrgID == "" && afterLibraryID == "" {
		return true
	}
	if row.OrgID > afterOrgID {
		return true
	}
	return row.OrgID == afterOrgID && row.LibraryID > afterLibraryID
}

func libraryRollbackRecoveryEventuallyAttempts(t *testing.T, ctx context.Context, state libraryRollbackRecoveryState, libraryID string, attempts map[string]int) bool {
	t.Helper()
	// 32 start-bucket rotations plus a same-bucket continuation is enough to
	// walk the ring; stay well under that so a fairness regression fails fast.
	const maxSweeps = 40
	for i := 0; i < maxSweeps; i++ {
		var err error
		state, err = recoverPendingLibraryRollbacksFrom(ctx, nil, state)
		if attempts[libraryID] > 0 {
			return true
		}
		if err == nil {
			t.Fatalf("sweep %d: prefix failures disappeared before the later marker was attempted", i+2)
		}
	}
	return false
}
