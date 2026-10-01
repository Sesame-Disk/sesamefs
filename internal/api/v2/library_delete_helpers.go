package v2

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gcpkg "github.com/Sesame-Disk/sesamefs/internal/gc"
	"github.com/Sesame-Disk/sesamefs/internal/metrics"
	"github.com/Sesame-Disk/sesamefs/internal/middleware"
	"github.com/Sesame-Disk/sesamefs/internal/traffic"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type trashLibraryCandidate struct {
	OrgID        string
	LibraryID    string
	StorageClass string
	// DeletedAt is the library's original trash time. The permanent-delete marker
	// preserves it (rather than resetting to now) so a library_cascade already queued
	// under that identity stays deduplicated — see hardDeleteLibraryRowsFn.
	DeletedAt time.Time
}

var (
	resolveDeleteBlockRepresentationFn = func(database *dbpkg.DB, orgID, libraryID string) (string, error) {
		return dbpkg.ResolveBlockRepresentationIDForDelete(database.Session(), orgID, libraryID)
	}
	cleanupLibraryLinksForDeleteFn = func(database *dbpkg.DB, orgID, libraryID string) error {
		return cleanupLibraryLinks(database, orgID, libraryID, nil)
	}
	cleanupLibraryLinksGuardedForDeleteFn = func(database *dbpkg.DB, orgID, libraryID string, beforeMutation func() error) error {
		return cleanupLibraryLinks(database, orgID, libraryID, beforeMutation)
	}
	readPermanentDeleteLibraryStateFn = func(database *dbpkg.DB, orgID, libraryID string, _ time.Time) (dbpkg.LibraryState, error) {
		return dbpkg.ReadLibraryState(database.Session(), orgID, libraryID)
	}
	acquireLibraryHardDeleteLockLeaseFn = func(database *dbpkg.DB, libraryID, leaseToken uuid.UUID) (bool, error) {
		return gcpkg.AcquireLibraryHardDeleteLockLease(database.Session(), libraryID, leaseToken)
	}
	renewLibraryHardDeleteLockLeaseFn = func(database *dbpkg.DB, libraryID, leaseToken uuid.UUID) (bool, error) {
		return gcpkg.RenewLibraryHardDeleteLockLease(database.Session(), libraryID, leaseToken)
	}
	releaseLibraryHardDeleteLockLeaseFn = func(database *dbpkg.DB, libraryID, leaseToken uuid.UUID) error {
		return gcpkg.ReleaseLibraryHardDeleteLockLease(database.Session(), libraryID, leaseToken)
	}
	hardDeleteLibraryRowsFn = func(database *dbpkg.DB, orgID, libraryID, storageClass, blockRepresentationID string, deletedAt time.Time) error {
		// Continuation anchor: the generation's soft-delete marker exists before the
		// canonical row can disappear, so a completion that fails below is always
		// rediscoverable (resumeCommittedPermanentDelete, single and bulk). It repeats
		// the soft delete's own marker write at the generation's lifecycle timestamp,
		// so it can never outlive a restore (whose marker removal is stamped later).
		if err := database.Session().Query(`
			INSERT INTO deleted_libraries (library_id, org_id, deleted_at, storage_class, block_representation_id)
			VALUES (?, ?, ?, ?, ?) USING TIMESTAMP ?`,
			libraryID, orgID, deletedAt, storageClass, blockRepresentationID, dbpkg.LibraryLifecycleWriteTimestamp(deletedAt)).Exec(); err != nil {
			return errors.Join(errHardDeleteLibraryBatchExec, fmt.Errorf("write permanent-delete continuation marker: %w", err))
		}
		// The completion batch is built first because the admin read-model delete keys
		// are read from the canonical row, which the fenced delete removes.
		batch := database.Session().Batch(gocql.LoggedBatch).WithTimestamp(permanentDeleteCompletionTimestamp(deletedAt))
		if err := addPermanentDeleteCompletionQueries(database, batch, orgID, libraryID, storageClass, blockRepresentationID, deletedAt); err != nil {
			return err
		}

		// The fenced lifecycle mutation (ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01): the
		// canonical row is deleted only while it is still trashed under the generation this
		// owner verified. The lease renewal before this call does not fence it: an owner can
		// pause after renewing, lose the lease to a restore, and resume here. Generations are
		// unique per library (lifecycle_at), so a later generation never matches. Each
		// LWT attempt first records its own durable continuation (with its attempt
		// id): the lifecycle reaper resumes the completion if this process dies after
		// the canonical delete, and fences the attempt if it dies before.
		var intents []dbpkg.LibraryLifecyclePending
		outcome, err := dbpkg.DeleteTrashedLibraryGenerationWithIntent(database.Session(), orgID, libraryID, deletedAt,
			func(previous dbpkg.LibraryLifecycleState) error {
				intent := dbpkg.NewLibraryLifecycleAttempt(dbpkg.LibraryLifecyclePending{
					OrgID: orgID, LibraryID: libraryID, Operation: dbpkg.LibraryLifecycleOpPermanentDelete,
					TargetAt: deletedAt, PrevLifecycleAt: previous.LifecycleAt, PrevDeletedAt: deletedAt,
				}, uuid.NewString())
				if err := dbpkg.InsertLibraryLifecyclePending(database.Session(), intent); err != nil {
					return errors.Join(errHardDeleteLibraryBatchExec, err)
				}
				intents = append(intents, intent)
				return beforeLibraryLifecycleTransitionFn("permanent-delete", libraryID)
			})
		if err != nil {
			// An unknown outcome keeps its continuation for the reaper.
			return errors.Join(errHardDeleteLibraryLifecycle, err)
		}
		switch outcome {
		case dbpkg.LibraryLifecycleApplied:
		case dbpkg.LibraryLifecycleTargetAbsent:
			// The row is already gone: a delete of this generation committed earlier
			// (possibly from another datacenter) and may still be incomplete. Finish it
			// rather than reporting a conflict.
			_, _, resumed, err := resumeCommittedPermanentDelete(database, orgID, libraryID)
			if err != nil {
				return err
			}
			clearLibraryLifecycleIntents(database, intents)
			if !resumed {
				return errPermanentDeleteCandidateStale
			}
			return nil
		default:
			clearLibraryLifecycleIntents(database, intents)
			return errPermanentDeleteCandidateStale
		}
		// Completion writes run only after this generation's canonical row is gone. A
		// hard delete is terminal, so they cannot touch a newer generation and are
		// idempotent. If they fail, the reaper or a repeated permanent delete (single
		// or bulk) completes them (resumeCommittedPermanentDelete).
		if err := dbpkg.ExecLibraryLifecycleCompletionFn(batch); err != nil {
			return errors.Join(errHardDeleteLibraryBatchExec, err)
		}
		clearLibraryLifecycleIntents(database, intents)
		return nil
	}
	cleanupAllLibraryTagsForDeleteFn       = CleanupAllLibraryTags
	deleteLibraryStorageCounterForDeleteFn = traffic.DeleteLibraryStorageCounter
	runAsyncLibraryDeleteSideEffectFn      = func(fn func()) { go fn() }
)

// Match the GC worker's hard-delete lease heartbeat cadence so API-side permanent deletes stay
// fresh under the same stale-takeover model during long link cleanup.
var permanentDeleteLeaseHeartbeatInterval = 30 * time.Minute

type permanentDeleteLeaseHeartbeat struct {
	stopCh    chan struct{}
	closedCh  chan struct{}
	closeOnce sync.Once

	mu  sync.Mutex
	err error
}

func startPermanentDeleteLeaseHeartbeat(target string, renew func() (bool, error)) *permanentDeleteLeaseHeartbeat {
	heartbeat := &permanentDeleteLeaseHeartbeat{
		stopCh:   make(chan struct{}),
		closedCh: make(chan struct{}),
	}
	go func() {
		defer close(heartbeat.closedCh)
		ticker := time.NewTicker(permanentDeleteLeaseHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeat.stopCh:
				return
			case <-ticker.C:
				owned, err := renew()
				if err != nil {
					heartbeat.setErr(fmt.Errorf("renew library hard-delete lock for %s during permanent delete: %w", target, err))
					return
				}
				if !owned {
					heartbeat.setErr(errPermanentDeleteInProgress)
					return
				}
			}
		}
	}()
	return heartbeat
}

func (h *permanentDeleteLeaseHeartbeat) setErr(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.err == nil {
		h.err = err
	}
}

func (h *permanentDeleteLeaseHeartbeat) Check() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.err
}

func (h *permanentDeleteLeaseHeartbeat) Close() {
	h.closeOnce.Do(func() {
		close(h.stopCh)
		<-h.closedCh
	})
}

var (
	errDeleteRepresentationUnresolved = errors.New("delete representation unresolved")
	errDeleteLibraryLinksCleanup      = errors.New("delete library links cleanup")
	errHardDeleteLibraryReadModel     = errors.New("hard delete library read model")
	errHardDeleteLibraryBatchExec     = errors.New("hard delete library batch exec")
	errHardDeleteLibraryLifecycle     = errors.New("hard delete library lifecycle transition")
	errPermanentDeleteCandidateStale  = errors.New("permanent delete candidate stale")
	errPermanentDeleteInProgress      = errors.New("permanent delete in progress")
)

// addPermanentDeleteCompletionQueries adds the writes that complete a permanent
// delete once its canonical row is gone: the admin read-model rows, the
// libraries_by_id lookup, the permanent-delete marker and a plain delete of the
// canonical row. The plain delete carries a client timestamp, so together with
// the LWT row tombstone (Paxos timestamp) it covers cells written under either
// clock.
func addPermanentDeleteCompletionQueries(database *dbpkg.DB, batch *gocql.Batch, orgID, libraryID, storageClass, blockRepresentationID string, deletedAt time.Time) error {
	if err := addDeleteAdminLibraryReadModelQueries(database, batch, orgID, libraryID); err != nil {
		return errors.Join(errHardDeleteLibraryReadModel, err)
	}
	batch.Query(`DELETE FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, libraryID)
	batch.Query(`DELETE FROM libraries_by_id WHERE library_id = ?`, libraryID)
	// This is a *permanent* delete. Two invariants:
	//   1. PRESERVE the original deleted_at (the library's trash time). Phase 13 dedups
	//      library_cascade by deleted_at; resetting it to now() would change the identity
	//      and let a cascade already queued under the old deleted_at be enqueued a second
	//      time. deletedAt is the authoritative libraries.deleted_at captured by the caller.
	//   2. Stamp purge_requested_at = now() so Phase 13 makes the library eligible on its
	//      next scan instead of waiting out the configured TrashRetentionDays. The cascade
	//      is still gated by the GC grace period before the worker processes it — reclamation
	//      happens on the order of the grace period, not the retention period.
	// See migration 012 / ISSUE-GC-ORG-TRASH-NO-CASCADE-01.
	batch.Query(`INSERT INTO deleted_libraries (library_id, org_id, deleted_at, storage_class, block_representation_id, purge_requested_at) VALUES (?, ?, ?, ?, ?, ?)`, libraryID, orgID, deletedAt, storageClass, blockRepresentationID, time.Now())
	return nil
}

// permanentDeleteCompletionTimestamp is the write timestamp of a permanent
// delete's completion: after the generation's own derived writes, and not before
// the client clock (the completion also deletes rows other writers stamp with it).
func permanentDeleteCompletionTimestamp(deletedAt time.Time) int64 {
	stamp := libraryLifecycleNow().UnixMicro()
	if floor := dbpkg.LibraryLifecycleWriteTimestamp(deletedAt) + 1; stamp < floor {
		stamp = floor
	}
	return stamp
}

// resumeCommittedPermanentDelete completes a permanent delete whose canonical
// row is already gone but whose completion writes failed. Its anchor is the
// generation's deleted_libraries marker, which the permanent delete writes
// before its canonical transition: a marker without purge request (or with the
// lookup row still present) for an absent canonical row. It is reached by
// repeating the permanent delete (single routes) and by the bulk cleanups, so
// it works with GC_ENABLED=false. It reports false, changing nothing, for any
// other state. A hard delete is terminal, so no lease is needed.
func resumeCommittedPermanentDelete(database *dbpkg.DB, orgID, libraryID string) (trashLibraryCandidate, string, bool, error) {
	var markerOrgID, storageClass, blockRepresentationID string
	var deletedAt, purgeRequestedAt time.Time
	// EACH_QUORUM: the marker may have been acknowledged in another datacenter; a
	// local miss must not read as "nothing to resume", and an unreachable DC fails
	// the resume (retry later) instead of reporting nothing to do.
	err := database.Session().Query(`SELECT org_id, deleted_at, storage_class, block_representation_id, purge_requested_at FROM deleted_libraries WHERE library_id = ?`,
		libraryID).Consistency(gocql.EachQuorum).Scan(&markerOrgID, &deletedAt, &storageClass, &blockRepresentationID, &purgeRequestedAt)
	if errors.Is(err, gocql.ErrNotFound) {
		return trashLibraryCandidate{}, "", false, nil
	}
	if err != nil {
		return trashLibraryCandidate{}, "", false, fmt.Errorf("read deleted library marker %s: %w", libraryID, err)
	}
	if markerOrgID != orgID || deletedAt.IsZero() {
		return trashLibraryCandidate{}, "", false, nil
	}
	if !purgeRequestedAt.IsZero() {
		var lookupOrgID string
		err = database.Session().Query(`SELECT org_id FROM libraries_by_id WHERE library_id = ?`, libraryID).Consistency(gocql.EachQuorum).Scan(&lookupOrgID)
		if errors.Is(err, gocql.ErrNotFound) {
			return trashLibraryCandidate{}, "", false, nil // completed
		}
		if err != nil {
			return trashLibraryCandidate{}, "", false, fmt.Errorf("read library lookup %s: %w", libraryID, err)
		}
	}
	state, err := dbpkg.ReadLibraryLifecycleSerial(database.Session(), orgID, libraryID)
	if err != nil {
		return trashLibraryCandidate{}, "", false, fmt.Errorf("read canonical library %s: %w", libraryID, err)
	}
	if state.Present {
		return trashLibraryCandidate{}, "", false, nil
	}
	batch := database.Session().Batch(gocql.LoggedBatch).WithTimestamp(permanentDeleteCompletionTimestamp(deletedAt))
	if err := addPermanentDeleteCompletionQueries(database, batch, orgID, libraryID, storageClass, blockRepresentationID, deletedAt); err != nil {
		return trashLibraryCandidate{}, "", false, err
	}
	if err := dbpkg.ExecLibraryLifecycleCompletionFn(batch); err != nil {
		return trashLibraryCandidate{}, "", false, errors.Join(errHardDeleteLibraryBatchExec, err)
	}
	return trashLibraryCandidate{OrgID: orgID, LibraryID: libraryID, StorageClass: storageClass, DeletedAt: deletedAt}, blockRepresentationID, true, nil
}

type resumedPermanentDelete struct {
	Candidate             trashLibraryCandidate
	BlockRepresentationID string
}

// resumeCommittedPermanentDeletes is the bulk counterpart of
// resumeCommittedPermanentDelete for the given orgs: the bulk cleanups list
// their candidates from canonical rows, which a half-committed permanent delete
// no longer has. Its discovery source is the permanent-delete continuations in
// library_lifecycle_pending, read at global QUORUM: every permanent delete
// records one before its canonical LWT, so a delete acknowledged in another
// datacenter is found, and a bucket that cannot be read fails closed (counted as
// failed) instead of reading as nothing to do. The deleted_libraries markers
// (one scan for all orgs) are a second, best-effort source. Every candidate is
// then resolved by resumeCommittedPermanentDelete's strong reads.
func resumeCommittedPermanentDeletes(database *dbpkg.DB, orgIDs []string) ([]resumedPermanentDelete, int) {
	wanted := make(map[string]bool, len(orgIDs))
	for _, orgID := range orgIDs {
		wanted[orgID] = true
	}
	failed := 0
	seen := map[[2]string]bool{}
	var candidates [][2]string
	pending, err := listPendingPermanentDeletesFn(database, wanted)
	if err != nil {
		log.Printf("[resumeCommittedPermanentDeletes] list permanent-delete continuations: %v", err)
		failed++
	}
	for _, pair := range pending {
		if !seen[pair] {
			seen[pair] = true
			candidates = append(candidates, pair)
		}
	}
	markers, err := listDeletedLibraryMarkersFn(database, wanted)
	if err != nil {
		log.Printf("[resumeCommittedPermanentDeletes] scan deleted_libraries: %v", err)
		failed++
	}
	for _, pair := range markers {
		if seen[pair] {
			continue
		}
		seen[pair] = true
		var present string
		if err := database.Session().Query(`SELECT library_id FROM libraries WHERE org_id = ? AND library_id = ?`, pair[0], pair[1]).Scan(&present); err == nil {
			continue // still canonical: the normal candidate path handles it
		}
		candidates = append(candidates, pair)
	}
	var resumed []resumedPermanentDelete
	for _, pair := range candidates {
		candidate, blockRepresentationID, ok, err := resumeCommittedPermanentDelete(database, pair[0], pair[1])
		if err != nil {
			log.Printf("[resumeCommittedPermanentDeletes] resume %s/%s: %v", pair[0], pair[1], err)
			failed++
			continue
		}
		if ok {
			resumed = append(resumed, resumedPermanentDelete{Candidate: candidate, BlockRepresentationID: blockRepresentationID})
		}
	}
	return resumed, failed
}

var (
	// listPendingPermanentDeletesFn returns the (org, library) pairs of the wanted
	// orgs that have a permanent-delete continuation, read at global QUORUM.
	listPendingPermanentDeletesFn = func(database *dbpkg.DB, wanted map[string]bool) ([][2]string, error) {
		var out [][2]string
		for bucket := 0; bucket < dbpkg.GCDiscoveryBucketCount; bucket++ {
			rows, err := dbpkg.ListLibraryLifecyclePending(database.Session(), bucket)
			if err != nil {
				return out, err
			}
			for _, row := range rows {
				if row.Operation == dbpkg.LibraryLifecycleOpPermanentDelete && wanted[row.OrgID] {
					out = append(out, [2]string{row.OrgID, row.LibraryID})
				}
			}
		}
		return out, nil
	}
	// listDeletedLibraryMarkersFn returns the (org, library) pairs of the wanted
	// orgs' deleted_libraries markers (session consistency). A variable so tests
	// can stand in for a datacenter whose local scan misses a marker.
	listDeletedLibraryMarkersFn = func(database *dbpkg.DB, wanted map[string]bool) ([][2]string, error) {
		var out [][2]string
		iter := database.Session().Query(`SELECT library_id, org_id FROM deleted_libraries`).Iter()
		var libraryID, orgID string
		for iter.Scan(&libraryID, &orgID) {
			if wanted[orgID] {
				out = append(out, [2]string{orgID, libraryID})
			}
		}
		return out, iter.Close()
	}
)

// readPermanentDeleteResumeOwner returns the owner recorded on the lookup row of
// a library whose canonical row is gone, for the permission check of a resumed
// permanent delete.
func readPermanentDeleteResumeOwner(database *dbpkg.DB, libraryID string) (orgID, ownerID string, err error) {
	err = database.Session().Query(`SELECT org_id, owner_id FROM libraries_by_id WHERE library_id = ?`, libraryID).Consistency(gocql.EachQuorum).Scan(&orgID, &ownerID)
	return orgID, ownerID, err
}

func enqueueLibraryCascadeBestEffort(libEnqueuer LibraryGCEnqueuer, orgID, repoID, blockRepresentationID, storageClass string, deletedAt time.Time) {
	if libEnqueuer == nil {
		return
	}
	// Immediately queue the durable library cascade so reclamation starts on the next worker
	// tick instead of waiting up to a full ScanInterval for Phase 13. This is deduplicated
	// against Phase 13 (identical deletedAt identity), so it is not a second producer; the
	// durable purge_requested_at marker recovers it if this fire-and-forget enqueue is lost.
	// See migration 012 / ISSUE-GC-ORG-TRASH-NO-CASCADE-01.
	runAsyncLibraryDeleteSideEffectFn(func() {
		libEnqueuer.EnqueueLibraryCascade(orgID, repoID, blockRepresentationID, storageClass, deletedAt)
	})
}

func permanentlyDeleteTrashedLibraryCandidate(database *dbpkg.DB, candidate trashLibraryCandidate, metricOp, logPrefix string, cleanupLinks bool) (string, error) {
	libraryUUID, err := uuid.Parse(candidate.LibraryID)
	if err != nil {
		return "", fmt.Errorf("parse library id %q: %w", candidate.LibraryID, err)
	}

	leaseToken := uuid.New()
	acquired, err := acquireLibraryHardDeleteLockLeaseFn(database, libraryUUID, leaseToken)
	if err != nil {
		return "", fmt.Errorf("acquire library hard-delete lock for %s/%s: %w", candidate.OrgID, candidate.LibraryID, err)
	}
	if !acquired {
		return "", errPermanentDeleteInProgress
	}
	defer func() {
		if err := releaseLibraryHardDeleteLockLeaseFn(database, libraryUUID, leaseToken); err != nil {
			log.Printf("[%s] failed to release hard-delete lock for %s/%s: %v", logPrefix, candidate.OrgID, candidate.LibraryID, err)
		}
	}()

	state, err := readPermanentDeleteLibraryStateFn(database, candidate.OrgID, candidate.LibraryID, candidate.DeletedAt)
	if errors.Is(err, gocql.ErrNotFound) {
		return "", errPermanentDeleteCandidateStale
	}
	if err != nil {
		return "", fmt.Errorf("read canonical library state for %s/%s: %w", candidate.OrgID, candidate.LibraryID, err)
	}
	if state.DeletedAt == nil || !state.DeletedAt.Equal(candidate.DeletedAt) {
		return "", errPermanentDeleteCandidateStale
	}

	blockRepresentationID, err := resolveDeleteBlockRepresentationFn(database, candidate.OrgID, candidate.LibraryID)
	if err != nil {
		metrics.LibraryDeleteRepresentationResolutionFailures.WithLabelValues(metricOp).Inc()
		log.Printf("[%s] refusing to hard-delete %s/%s: block representation unresolved: %v", logPrefix, candidate.OrgID, candidate.LibraryID, err)
		return "", errDeleteRepresentationUnresolved
	}

	var leaseHeartbeat *permanentDeleteLeaseHeartbeat
	if cleanupLinks {
		// Keep the shared hard-delete lease alive for the entire link-cleanup window so a
		// restore cannot steal a stale lease and revive the library after link rows were
		// already deleted but before the hard-delete batch runs.
		leaseHeartbeat = startPermanentDeleteLeaseHeartbeat(candidate.OrgID+"/"+candidate.LibraryID, func() (bool, error) {
			return renewLibraryHardDeleteLockLeaseFn(database, libraryUUID, leaseToken)
		})
		defer leaseHeartbeat.Close()
		if err := cleanupLibraryLinksGuardedForDeleteFn(database, candidate.OrgID, candidate.LibraryID, leaseHeartbeat.Check); err != nil {
			return "", errors.Join(errDeleteLibraryLinksCleanup, err)
		}
		leaseHeartbeat.Close()
		if err := leaseHeartbeat.Check(); err != nil {
			return "", err
		}
	}

	// Early exit only: this renewal does not fence the final mutation (the owner can
	// pause right after it). hardDeleteLibraryRowsFn fences on the deleted_at generation.
	owned, err := renewLibraryHardDeleteLockLeaseFn(database, libraryUUID, leaseToken)
	if err != nil {
		return "", fmt.Errorf("renew library hard-delete lock for %s/%s: %w", candidate.OrgID, candidate.LibraryID, err)
	}
	if !owned {
		return "", errPermanentDeleteInProgress
	}

	if err := hardDeleteLibraryRowsFn(database, candidate.OrgID, candidate.LibraryID, candidate.StorageClass, blockRepresentationID, candidate.DeletedAt); err != nil {
		return "", err
	}
	return blockRepresentationID, nil
}

func writePermanentDeletePreconditionError(c *gin.Context, err error) bool {
	if errors.Is(err, errPermanentDeleteCandidateStale) {
		c.JSON(http.StatusConflict, gin.H{"error": "library is no longer in trash"})
		return true
	}
	if errors.Is(err, errPermanentDeleteInProgress) {
		c.JSON(http.StatusConflict, gin.H{"error": "library permanent delete is already in progress"})
		return true
	}
	return false
}

func (h *DeletedLibraryHandler) permanentDeleteResolvedRepo(c *gin.Context, orgID, repoID, storageClass string, deletedAt time.Time) {
	blockRepresentationID, err := permanentlyDeleteTrashedLibraryCandidate(h.db, trashLibraryCandidate{
		OrgID:        orgID,
		LibraryID:    repoID,
		StorageClass: storageClass,
		DeletedAt:    deletedAt,
	}, "permanent_delete", "PermanentDeleteRepo", true)
	if writePermanentDeletePreconditionError(c, err) {
		return
	}
	if errors.Is(err, errDeleteRepresentationUnresolved) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to prepare library for permanent deletion"})
		return
	}
	if errors.Is(err, errDeleteLibraryLinksCleanup) {
		log.Printf("[PermanentDeleteRepo] Failed to clean share links for %s: %v", repoID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to clean share links"})
		return
	}
	if err != nil {
		if errors.Is(err, errHardDeleteLibraryReadModel) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to clean library read model"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete library"})
		return
	}
	h.finishPermanentDelete(c, orgID, repoID, storageClass, deletedAt, blockRepresentationID)
}

// resumePermanentDelete completes, on a repeated request, a permanent delete of
// repoID whose canonical row is already gone but whose completion writes failed
// (resumeCommittedPermanentDelete). It reports whether it answered the request.
func (h *DeletedLibraryHandler) resumePermanentDelete(c *gin.Context, orgID, repoID, userID string, callerRole middleware.OrganizationRole) bool {
	lookupOrgID, ownerID, err := readPermanentDeleteResumeOwner(h.db, repoID)
	if err != nil && !errors.Is(err, gocql.ErrNotFound) {
		// Cannot tell whether a committed delete is waiting: fail, do not answer 404.
		log.Printf("[PermanentDeleteRepo] cannot check for a committed permanent delete of %s/%s: %v", orgID, repoID, err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "library state unavailable, retry"})
		return true
	}
	if err != nil || lookupOrgID != orgID {
		return false
	}
	if ownerID != userID && !middleware.HasRequiredOrgRole(callerRole, middleware.RoleAdmin) {
		c.JSON(http.StatusForbidden, gin.H{"error": "only library owner or admin can permanently delete"})
		return true
	}
	candidate, blockRepresentationID, resumed, err := resumeCommittedPermanentDelete(h.db, orgID, repoID)
	if err != nil {
		log.Printf("[PermanentDeleteRepo] failed to resume permanent delete of %s/%s: %v", orgID, repoID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete library"})
		return true
	}
	if !resumed {
		return false
	}
	h.finishPermanentDelete(c, orgID, repoID, candidate.StorageClass, candidate.DeletedAt, blockRepresentationID)
	return true
}

// finishPermanentDelete runs the side effects that follow a committed permanent
// delete and answers the request.
func (h *DeletedLibraryHandler) finishPermanentDelete(c *gin.Context, orgID, repoID, storageClass string, deletedAt time.Time, blockRepresentationID string) {
	if h.libHandler != nil {
		enqueueLibraryCascadeBestEffort(h.libHandler.gcEnqueuer, orgID, repoID, blockRepresentationID, storageClass, deletedAt)
	}
	if err := deleteLibraryStorageCounterForDeleteFn(h.db, orgID, repoID); err != nil {
		log.Printf("failed to delete storage counter for permanently deleted library %s/%s: %v", orgID, repoID, err)
	}
	runAsyncLibraryDeleteSideEffectFn(func() {
		if err := cleanupAllLibraryTagsForDeleteFn(h.db, repoID); err != nil {
			log.Printf("failed to clean tag metadata for permanently deleted library %s: %v", repoID, err)
		}
	})
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *OrgAdminHandler) deleteResolvedTrashLibrary(c *gin.Context, targetOrgID, repoID, storageClass string, deletedAt time.Time) {
	blockRepresentationID, err := permanentlyDeleteTrashedLibraryCandidate(h.db, trashLibraryCandidate{
		OrgID:        targetOrgID,
		LibraryID:    repoID,
		StorageClass: storageClass,
		DeletedAt:    deletedAt,
	}, "org_delete_trash_library", "DeleteOrgTrashLibrary", true)
	if writePermanentDeletePreconditionError(c, err) {
		return
	}
	if errors.Is(err, errDeleteRepresentationUnresolved) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to prepare library for permanent deletion"})
		return
	}
	if errors.Is(err, errDeleteLibraryLinksCleanup) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to clean library links"})
		return
	}
	if err != nil {
		if errors.Is(err, errHardDeleteLibraryReadModel) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to clean library read model"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete library"})
		return
	}
	enqueueLibraryCascadeBestEffort(h.gcEnqueuer, targetOrgID, repoID, blockRepresentationID, storageClass, deletedAt)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// resumeOrgTrashLibraryDelete is the org-admin counterpart of
// DeletedLibraryHandler.resumePermanentDelete. It reports whether it answered
// the request.
func (h *OrgAdminHandler) resumeOrgTrashLibraryDelete(c *gin.Context, targetOrgID, repoID string) bool {
	candidate, blockRepresentationID, resumed, err := resumeCommittedPermanentDelete(h.db, targetOrgID, repoID)
	if err != nil {
		log.Printf("[DeleteOrgTrashLibrary] failed to resume permanent delete of %s/%s: %v", targetOrgID, repoID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete library"})
		return true
	}
	if !resumed {
		return false
	}
	enqueueLibraryCascadeBestEffort(h.gcEnqueuer, targetOrgID, repoID, blockRepresentationID, candidate.StorageClass, candidate.DeletedAt)
	c.JSON(http.StatusOK, gin.H{"success": true})
	return true
}

// processOrgTrashCandidates hard-deletes each candidate trashed library for the org-admin
// bulk clean-trash path and enqueues its durable, deduplicated library cascade — the same
// shared-writer wiring as the single-library org-admin delete (deleteResolvedTrashLibrary),
// run over an explicit candidate list. Splitting it from the org-wide SELECT lets the bulk
// loop be exercised with explicit candidates in tests without triggering org-wide side
// effects, and mirrors processAdminTrashCandidates so the two bulk paths cannot drift.
//
// Unlike the admin path it also cleans the library's share/upload links (org bulk always
// has). A per-library failure (unresolved block representation, link cleanup, or hard-delete)
// is counted and skipped so one bad library never strands the rest or aborts the whole batch;
// a skipped library keeps its live canonical row and is retried on the next clean.
func (h *OrgAdminHandler) processOrgTrashCandidates(candidates []trashLibraryCandidate, libEnqueuer LibraryGCEnqueuer) (cleaned, failed int) {
	for _, candidate := range candidates {
		blockRepresentationID, err := permanentlyDeleteTrashedLibraryCandidate(h.db, candidate, "org_clean_trash", "CleanOrgTrashLibraries", true)
		if errors.Is(err, errPermanentDeleteCandidateStale) {
			log.Printf("[CleanOrgTrashLibraries] skipping stale trash candidate %s/%s: canonical deleted_at no longer matches %s", candidate.OrgID, candidate.LibraryID, candidate.DeletedAt.Format(time.RFC3339Nano))
			failed++
			continue
		}
		if errors.Is(err, errPermanentDeleteInProgress) {
			log.Printf("[CleanOrgTrashLibraries] skipping %s/%s: permanent delete or restore already holds the hard-delete lease", candidate.OrgID, candidate.LibraryID)
			failed++
			continue
		}
		if errors.Is(err, errDeleteLibraryLinksCleanup) {
			log.Printf("[CleanOrgTrashLibraries] skipping %s/%s: failed to clean library links: %v", candidate.OrgID, candidate.LibraryID, err)
			failed++
			continue
		}
		if err != nil {
			log.Printf("[CleanOrgTrashLibraries] failed to delete library %s (org %s): %v", candidate.LibraryID, candidate.OrgID, err)
			failed++
			continue
		}
		enqueueLibraryCascadeBestEffort(libEnqueuer, candidate.OrgID, candidate.LibraryID, blockRepresentationID, candidate.StorageClass, candidate.DeletedAt)
		cleaned++
	}
	return cleaned, failed
}

func (h *AdminHandler) processAdminTrashCandidates(candidates []trashLibraryCandidate, libEnqueuer LibraryGCEnqueuer) (cleaned, failed int) {
	for _, candidate := range candidates {
		blockRepresentationID, err := permanentlyDeleteTrashedLibraryCandidate(h.db, candidate, "admin_clean_trash", "AdminCleanTrashLibraries", false)
		if errors.Is(err, errPermanentDeleteCandidateStale) {
			log.Printf("[AdminCleanTrashLibraries] skipping stale trash candidate %s/%s: canonical deleted_at no longer matches %s", candidate.OrgID, candidate.LibraryID, candidate.DeletedAt.Format(time.RFC3339Nano))
			failed++
			continue
		}
		if errors.Is(err, errPermanentDeleteInProgress) {
			log.Printf("[AdminCleanTrashLibraries] skipping %s/%s: permanent delete or restore already holds the hard-delete lease", candidate.OrgID, candidate.LibraryID)
			failed++
			continue
		}
		if err != nil {
			log.Printf("[AdminCleanTrashLibraries] failed to delete library %s (org %s): %v", candidate.LibraryID, candidate.OrgID, err)
			failed++
			continue
		}
		enqueueLibraryCascadeBestEffort(libEnqueuer, candidate.OrgID, candidate.LibraryID, blockRepresentationID, candidate.StorageClass, candidate.DeletedAt)
		runAsyncLibraryDeleteSideEffectFn(func() {
			if err := cleanupAllLibraryTagsForDeleteFn(h.db, candidate.LibraryID); err != nil {
				log.Printf("[AdminCleanTrashLibraries] failed to clean tag metadata for library %s: %v", candidate.LibraryID, err)
			}
		})
		cleaned++
	}
	return cleaned, failed
}
