package v2

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

const (
	libraryRollbackRecoveryPageSize    = 100
	libraryRollbackRecoveryMaxPerSweep = 256
	libraryRollbackReaperInterval      = 30 * time.Second
)

// LibraryRollbackReaper is a Server-owned, GC-independent scanner of
// library_rollback_pending. It re-runs the same HEAD LWT before any cleanup.
type LibraryRollbackReaper struct {
	session *gocql.Session
	cancel  context.CancelFunc
	done    chan struct{}
}

// StartLibraryRollbackReaper begins an immediate sweep (so a restart recovers
// without waiting for the first tick) and then a bounded periodic scan.
// Independent of GC_ENABLED. A nil database is a no-op.
func StartLibraryRollbackReaper(database *db.DB) *LibraryRollbackReaper {
	if database == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &LibraryRollbackReaper{
		session: database.Session(),
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	go r.loop(ctx)
	return r
}

// Stop cancels the sweep and waits for the goroutine to exit.
func (r *LibraryRollbackReaper) Stop() {
	r.StopWithContext(context.Background())
}

// StopWithContext cancels the sweep and waits until it exits or ctx is done.
// Server.Shutdown passes its deadline so a stuck Cassandra page cannot hang
// process exit past the graceful-stop timeout.
func (r *LibraryRollbackReaper) StopWithContext(ctx context.Context) {
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

func (r *LibraryRollbackReaper) loop(ctx context.Context) {
	defer close(r.done)
	if err := RecoverPendingLibraryRollbacks(ctx, r.session); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("[library_rollback] recovery deferred/error: %v", err)
	}
	ticker := time.NewTicker(libraryRollbackReaperInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := RecoverPendingLibraryRollbacks(ctx, r.session); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("[library_rollback] recovery deferred/error: %v", err)
			}
		}
	}
}

// RecoverPendingLibraryRollbacks is the recovery seam: it enumerates durable
// markers from Cassandra and, for each one, re-enters deleteUnpublishedLibraryRow
// before any derived cleanup. Tests that inject a crash after the authority
// LWT must call this rather than the cleanup helper directly.
func RecoverPendingLibraryRollbacks(ctx context.Context, session *gocql.Session) error {
	if session == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	processed := 0
	var firstErr error
	for bucket := 0; bucket < db.GCDiscoveryBucketCount; bucket++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		var pageState []byte
		for {
			if processed >= libraryRollbackRecoveryMaxPerSweep {
				return firstErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			pageSize := libraryRollbackRecoveryPageSize
			if remaining := libraryRollbackRecoveryMaxPerSweep - processed; remaining < pageSize {
				pageSize = remaining
			}
			page, err := db.ListLibraryRollbackPending(session, bucket, pageState, pageSize)
			if err != nil {
				log.Printf("[library_rollback] recovery deferred/error: list bucket=%d: %v", bucket, err)
				if firstErr == nil {
					firstErr = err
				}
				break
			}
			for i := range page.Rows {
				if err := ctx.Err(); err != nil {
					return err
				}
				processed++
				if err := recoverPendingLibraryRollback(session, page.Rows[i]); err != nil {
					if firstErr == nil {
						firstErr = err
					}
				}
			}
			if len(page.PageState) == 0 {
				break
			}
			pageState = page.PageState
		}
	}
	return firstErr
}

// recoverPendingLibraryRollback re-runs the same authority gate the request
// path uses. Finding a marker is not permission to cleanup.
func recoverPendingLibraryRollback(session *gocql.Session, pending db.LibraryRollbackPending) error {
	return applyAuthorizedLibraryRollbackCleanup(session, pending, true)
}
