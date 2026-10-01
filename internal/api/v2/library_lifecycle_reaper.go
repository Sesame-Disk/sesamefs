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
// library_lifecycle_pending and finishes a row only once its transition can no
// longer change: the derived state is rebuilt from the canonical row, a
// permanent delete is resumed, and a storage reconciliation request is
// recorded after the canonical state is final.
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
// row whose transition can still apply is kept; a failed finish keeps the row
// for the next sweep.
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
			if err := finishLibraryLifecyclePending(database, row); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// finishLibraryLifecyclePending finishes one transition once the canonical
// lifecycle has moved past its precondition (its LWT can no longer commit):
// whether it applied or was superseded, rebuilding the derived state of the
// current canonical row and requesting a storage reconciliation leave both
// consistent with that row.
func finishLibraryLifecyclePending(database *dbpkg.DB, pending dbpkg.LibraryLifecyclePending) error {
	state, err := dbpkg.ReadLibraryLifecycleSerial(database.Session(), pending.OrgID, pending.LibraryID)
	if err != nil {
		return fmt.Errorf("read library %s/%s for its pending %s: %w", pending.OrgID, pending.LibraryID, pending.Operation, err)
	}
	if pending.CanStillApply(state) {
		return nil
	}
	if state.Present {
		if err := repairLibraryLifecycleDerivedState(database, pending.OrgID, pending.LibraryID); err != nil {
			return err
		}
	} else if _, _, _, err := resumeCommittedPermanentDelete(database, pending.OrgID, pending.LibraryID); err != nil {
		return err
	}
	if pending.Operation != dbpkg.LibraryLifecycleOpPermanentDelete {
		if err := requestStorageReconciliation(database, pending.OrgID, pending.OwnerID, time.Now().UTC()); err != nil {
			return err
		}
	}
	return dbpkg.DeleteLibraryLifecyclePending(database.Session(), pending)
}
