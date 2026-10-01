package v2

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
)

const libraryLifecycleReaperInterval = 30 * time.Second

// LibraryLifecycleReaper is the Server-owned, GC-independent finisher of
// library lifecycle transitions (soft delete, restore, permanent delete) whose
// process died between the canonical transition and its completion
// (ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01). It scans
// library_lifecycle_pending at global QUORUM and finishes an attempt only once
// it can no longer apply: the derived state is rebuilt from the canonical row, a
// permanent delete is resumed, and a storage reconciliation request is
// recorded after the canonical state is final. An attempt that can still apply
// past LibraryLifecycleAttemptAbandonAfter is abandoned (its producer died
// before its LWT, or the LWT failed without applying): it is fenced first
// (dbpkg.FenceLibraryLifecycleAttempt), so its row is retired without ever
// being dropped while its LWT could still commit.
type LibraryLifecycleReaper struct {
	database *dbpkg.DB
	cancel   context.CancelFunc
	done     chan struct{}
}

// StartLibraryLifecycleReaper begins an immediate sweep and then a periodic
// one. Independent of GC_ENABLED. A nil database is a no-op.
func StartLibraryLifecycleReaper(database *dbpkg.DB) *LibraryLifecycleReaper {
	if database == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &LibraryLifecycleReaper{database: database, cancel: cancel, done: make(chan struct{})}
	go r.loop(ctx)
	return r
}

// StopWithContext cancels the sweep and waits until it exits or ctx is done.
func (r *LibraryLifecycleReaper) StopWithContext(ctx context.Context) {
	if r == nil {
		return
	}
	r.cancel()
	if ctx == nil {
		<-r.done
		return
	}
	select {
	case <-r.done:
	case <-ctx.Done():
	}
}

func (r *LibraryLifecycleReaper) loop(ctx context.Context) {
	defer close(r.done)
	ticker := time.NewTicker(libraryLifecycleReaperInterval)
	defer ticker.Stop()
	for {
		if err := RecoverPendingLibraryLifecycles(ctx, r.database); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("[libraryLifecycle] recovery deferred: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RecoverPendingLibraryLifecycles runs one sweep over every recovery bucket. A
// row whose attempt can still apply is kept until it is abandoned, then fenced;
// a failed finish keeps the row for the next sweep. A bucket that cannot be
// read at global QUORUM fails the sweep (retry later), never reads as empty.
func RecoverPendingLibraryLifecycles(ctx context.Context, database *dbpkg.DB) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var firstErr error
	for bucket := 0; bucket < dbpkg.GCDiscoveryBucketCount; bucket++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := dbpkg.ListLibraryLifecyclePending(database.Session(), bucket)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, row := range rows {
			if err := finishLibraryLifecyclePending(database, row, time.Now().UTC()); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// finishLibraryLifecyclePending finishes one attempt once the canonical
// lifecycle has moved past its precondition (its LWT can no longer commit),
// fencing it first if it is abandoned: whether it applied, was superseded or
// never ran, rebuilding the derived state of the current canonical row and
// requesting a storage reconciliation leave both consistent with that row.
func finishLibraryLifecyclePending(database *dbpkg.DB, pending dbpkg.LibraryLifecyclePending, now time.Time) error {
	state, err := dbpkg.ReadLibraryLifecycleSerial(database.Session(), pending.OrgID, pending.LibraryID)
	if err != nil {
		return fmt.Errorf("read library %s/%s for its pending %s: %w", pending.OrgID, pending.LibraryID, pending.Operation, err)
	}
	if pending.CanStillApply(state) {
		if !pending.Abandoned(now) {
			return nil // possibly in flight
		}
		if state, err = dbpkg.FenceLibraryLifecycleAttempt(database.Session(), pending, now); err != nil {
			return err
		}
		if pending.CanStillApply(state) {
			return nil // the fence did not settle it; the next sweep retries
		}
	}
	if state.Present {
		if err := repairLibraryLifecycleDerivedState(database, pending.OrgID, pending.LibraryID); err != nil {
			return err
		}
	} else if _, _, _, err := resumeCommittedPermanentDelete(database, pending.OrgID, pending.LibraryID); err != nil {
		return err
	}
	if pending.Operation != dbpkg.LibraryLifecycleOpPermanentDelete {
		if err := requestStorageReconciliation(database, pending.OrgID, pending.OwnerID, now); err != nil {
			return err
		}
	}
	return dbpkg.DeleteLibraryLifecyclePending(database.Session(), pending)
}
