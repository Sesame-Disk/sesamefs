package v2

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

const (
	libraryRollbackRecoveryPageSize    = 100
	libraryRollbackRecoveryMaxPerSweep = 256
	libraryRollbackReaperInterval      = 30 * time.Second
)

// listLibraryRollbackPendingAfterFn / recoverPendingLibraryRollbackFn are
// production helpers with test-replaceable vars so fairness tests can inject
// a prefix of persistent failures without minting 256 real libraries.
var (
	listLibraryRollbackPendingAfterFn = db.ListLibraryRollbackPendingAfter
	recoverPendingLibraryRollbackFn   = recoverPendingLibraryRollback
)

// libraryRollbackClusteringCursor is the exclusive resume point inside one
// recovery bucket: the next list is (org_id, library_id) > these keys.
// Empty keys mean the start of the partition.
type libraryRollbackClusteringCursor struct {
	orgID     string
	libraryID string
}

// libraryRollbackRecoveryState is the fair, bounded scan cursor. after[i] is
// the last processed clustering key in bucket i (so failures advance).
// startBucket rotates every completed sweep so a full, persistently failing
// prefix in one partition cannot deny the other 31 buckets service.
type libraryRollbackRecoveryState struct {
	startBucket int
	after       [db.GCDiscoveryBucketCount]libraryRollbackClusteringCursor
}

// LibraryRollbackReaper is a Server-owned, GC-independent scanner of
// library_rollback_pending. It re-runs the same HEAD LWT before any cleanup.
type LibraryRollbackReaper struct {
	session *gocql.Session
	cancel  context.CancelFunc
	done    chan struct{}

	mu     sync.Mutex
	cursor libraryRollbackRecoveryState
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
	r.sweep(ctx)
	ticker := time.NewTicker(libraryRollbackReaperInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.sweep(ctx)
		}
	}
}

func (r *LibraryRollbackReaper) sweep(ctx context.Context) {
	r.mu.Lock()
	state := r.cursor
	r.mu.Unlock()
	next, err := recoverPendingLibraryRollbacksFrom(ctx, r.session, state)
	r.mu.Lock()
	r.cursor = next
	r.mu.Unlock()
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("[library_rollback] recovery deferred/error: %v", err)
	}
}

// RecoverPendingLibraryRollbacks is the recovery seam: it enumerates durable
// markers from Cassandra and, for each one, re-enters deleteUnpublishedLibraryRow
// before any derived cleanup. Tests that inject a crash after the authority
// LWT must call this rather than the cleanup helper directly.
//
// One call is a single bounded sweep starting at bucket 0. The Server-owned
// reaper retains recoverPendingLibraryRollbacksFrom state across ticks so a
// persistently failing prefix cannot starve later markers.
func RecoverPendingLibraryRollbacks(ctx context.Context, session *gocql.Session) error {
	if session == nil {
		return nil
	}
	_, err := recoverPendingLibraryRollbacksFrom(ctx, session, libraryRollbackRecoveryState{})
	return err
}

// recoverPendingLibraryRollbacksFrom scans at most
// libraryRollbackRecoveryMaxPerSweep markers, always advancing the clustering
// cursor past a processed row (success or failure), wrapping a finished
// partition back to its start, and rotating the start bucket after each
// completed sweep.
func recoverPendingLibraryRollbacksFrom(ctx context.Context, session *gocql.Session, state libraryRollbackRecoveryState) (libraryRollbackRecoveryState, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if state.startBucket < 0 || state.startBucket >= db.GCDiscoveryBucketCount {
		state.startBucket = 0
	}
	processed := 0
	var firstErr error
	bucket := state.startBucket
	for visited := 0; visited < db.GCDiscoveryBucketCount && processed < libraryRollbackRecoveryMaxPerSweep; visited++ {
		if err := ctx.Err(); err != nil {
			return state, err
		}
		after := state.after[bucket]
		for processed < libraryRollbackRecoveryMaxPerSweep {
			if err := ctx.Err(); err != nil {
				return state, err
			}
			pageSize := libraryRollbackRecoveryPageSize
			if remaining := libraryRollbackRecoveryMaxPerSweep - processed; remaining < pageSize {
				pageSize = remaining
			}
			rows, err := listLibraryRollbackPendingAfterFn(session, bucket, after.orgID, after.libraryID, pageSize)
			if err != nil {
				log.Printf("[library_rollback] recovery deferred/error: list bucket=%d: %v", bucket, err)
				if firstErr == nil {
					firstErr = err
				}
				break
			}
			if len(rows) == 0 {
				// Partition exhausted from this cursor. Wrap so the next visit
				// retries from the start (failed rows stay durable).
				state.after[bucket] = libraryRollbackClusteringCursor{}
				break
			}
			for i := range rows {
				if err := ctx.Err(); err != nil {
					return state, err
				}
				row := rows[i]
				processed++
				if err := recoverPendingLibraryRollbackFn(session, row); err != nil {
					if firstErr == nil {
						firstErr = err
					}
				}
				after = libraryRollbackClusteringCursor{orgID: row.OrgID, libraryID: row.LibraryID}
				state.after[bucket] = after
				if processed >= libraryRollbackRecoveryMaxPerSweep {
					break
				}
			}
		}
		bucket = (bucket + 1) % db.GCDiscoveryBucketCount
	}
	if err := ctx.Err(); err != nil {
		return state, err
	}
	state.startBucket = (state.startBucket + 1) % db.GCDiscoveryBucketCount
	return state, firstErr
}

// recoverPendingLibraryRollback re-runs the same authority gate the request
// path uses. Finding a marker is not permission to cleanup.
func recoverPendingLibraryRollback(session *gocql.Session, pending db.LibraryRollbackPending) error {
	return applyAuthorizedLibraryRollbackCleanup(session, pending, true)
}
