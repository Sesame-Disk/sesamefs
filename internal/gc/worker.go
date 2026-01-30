package gc

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	"github.com/google/uuid"
)

// Worker drains the gc_queue and deletes items from S3 and the database.
type Worker struct {
	db             *db.DB
	storageManager *storage.Manager
	queue          *Queue
	batchSize      int
	gracePeriod    time.Duration
	dryRun         bool
	stats          *Stats
}

// NewWorker creates a new GC worker.
func NewWorker(database *db.DB, storageManager *storage.Manager, queue *Queue, batchSize int, gracePeriod time.Duration, dryRun bool, stats *Stats) *Worker {
	return &Worker{
		db:             database,
		storageManager: storageManager,
		queue:          queue,
		batchSize:      batchSize,
		gracePeriod:    gracePeriod,
		dryRun:         dryRun,
		stats:          stats,
	}
}

// ProcessOnce runs a single pass of the worker: find orgs with queued items,
// dequeue a batch for each, and process them.
func (w *Worker) ProcessOnce(ctx context.Context) (int, error) {
	orgs, err := w.queue.ListOrgsWithQueuedItems()
	if err != nil {
		return 0, fmt.Errorf("failed to list orgs: %w", err)
	}

	totalProcessed := 0
	for _, orgID := range orgs {
		select {
		case <-ctx.Done():
			return totalProcessed, ctx.Err()
		default:
		}

		n, err := w.processOrg(ctx, orgID)
		if err != nil {
			log.Printf("[GC Worker] Error processing org %s: %v", orgID, err)
			continue
		}
		totalProcessed += n
	}

	return totalProcessed, nil
}

func (w *Worker) processOrg(ctx context.Context, orgID uuid.UUID) (int, error) {
	items, err := w.queue.DequeueBatch(orgID, w.batchSize, w.gracePeriod)
	if err != nil {
		return 0, err
	}

	processed := 0
	for _, item := range items {
		select {
		case <-ctx.Done():
			return processed, ctx.Err()
		default:
		}

		if err := w.processItem(ctx, item); err != nil {
			log.Printf("[GC Worker] Failed to process item %s/%s (type=%s): %v",
				item.OrgID, item.ItemID, item.ItemType, err)

			// Increment retry count; if too many retries, let TTL clean it up
			if item.RetryCount < 5 {
				w.queue.IncrementRetry(item.OrgID, item.QueuedAt, item.ItemType, item.ItemID, item.RetryCount)
			}
			continue
		}

		// Remove from queue
		if err := w.queue.Complete(item.OrgID, item.QueuedAt, item.ItemType, item.ItemID); err != nil {
			log.Printf("[GC Worker] Failed to complete item %s/%s: %v",
				item.OrgID, item.ItemID, err)
		}

		processed++
	}

	return processed, nil
}

func (w *Worker) processItem(ctx context.Context, item QueueItem) error {
	switch item.ItemType {
	case ItemBlock:
		return w.processBlock(ctx, item)
	case ItemCommit:
		return w.processCommit(ctx, item)
	case ItemFSObject:
		return w.processFSObject(ctx, item)
	case ItemBlockMapping:
		return w.processBlockMapping(ctx, item)
	case ItemShareLink:
		return w.processShareLink(ctx, item)
	default:
		return fmt.Errorf("unknown item type: %s", item.ItemType)
	}
}

func (w *Worker) processBlock(ctx context.Context, item QueueItem) error {
	// Re-verify ref_count is still 0 before deleting
	var refCount int
	err := w.db.Session().Query(`
		SELECT ref_count FROM blocks WHERE org_id = ? AND block_id = ?
	`, item.OrgID, item.ItemID).Scan(&refCount)
	if err != nil {
		// Block may already be deleted
		log.Printf("[GC Worker] Block %s not found (may already be deleted): %v", item.ItemID, err)
		return nil
	}

	if refCount > 0 {
		// Block was re-referenced during grace period, skip deletion
		log.Printf("[GC Worker] Block %s ref_count=%d, skipping deletion", item.ItemID, refCount)
		return nil
	}

	if w.dryRun {
		log.Printf("[GC Worker] DRY RUN: Would delete block %s from S3 and DB", item.ItemID)
		return nil
	}

	// Delete from S3
	storageClass := item.StorageClass
	if storageClass == "" {
		storageClass = "hot"
	}

	if w.storageManager != nil {
		blockStore, err := w.storageManager.GetBlockStore(storageClass)
		if err != nil {
			return fmt.Errorf("failed to get block store for class %s: %w", storageClass, err)
		}
		if err := blockStore.DeleteBlock(ctx, item.ItemID); err != nil {
			return fmt.Errorf("failed to delete block from S3: %w", err)
		}
	}

	// Delete block record from DB
	if err := w.db.Session().Query(`
		DELETE FROM blocks WHERE org_id = ? AND block_id = ?
	`, item.OrgID, item.ItemID).Exec(); err != nil {
		return fmt.Errorf("failed to delete block record: %w", err)
	}

	// Delete block_id_mappings where internal_id matches this block
	// (mappings are keyed by org_id + external_id, so we need to scan)
	// For efficiency, we query the mapping by internal_id pattern
	// This is best-effort; the scanner will catch any missed mappings
	iter := w.db.Session().Query(`
		SELECT external_id FROM block_id_mappings WHERE org_id = ?
	`, item.OrgID).Iter()
	var extID string
	for iter.Scan(&extID) {
		var internalID string
		w.db.Session().Query(`
			SELECT internal_id FROM block_id_mappings WHERE org_id = ? AND external_id = ?
		`, item.OrgID, extID).Scan(&internalID)
		if internalID == item.ItemID {
			w.db.Session().Query(`
				DELETE FROM block_id_mappings WHERE org_id = ? AND external_id = ?
			`, item.OrgID, extID).Exec()
		}
	}
	iter.Close()

	w.stats.IncrBlocksDeleted()
	log.Printf("[GC Worker] Deleted block %s", item.ItemID)
	return nil
}

func (w *Worker) processCommit(ctx context.Context, item QueueItem) error {
	if w.dryRun {
		log.Printf("[GC Worker] DRY RUN: Would delete commit %s from library %s", item.ItemID, item.LibraryID)
		return nil
	}

	if err := w.db.Session().Query(`
		DELETE FROM commits WHERE library_id = ? AND commit_id = ?
	`, item.LibraryID, item.ItemID).Exec(); err != nil {
		return fmt.Errorf("failed to delete commit: %w", err)
	}

	log.Printf("[GC Worker] Deleted commit %s", item.ItemID)
	return nil
}

func (w *Worker) processFSObject(ctx context.Context, item QueueItem) error {
	// Get the fs_object to find its block_ids
	var blockIDsList []string
	var objType string
	err := w.db.Session().Query(`
		SELECT obj_type, block_ids FROM fs_objects WHERE library_id = ? AND fs_id = ?
	`, item.LibraryID, item.ItemID).Scan(&objType, &blockIDsList)
	if err != nil {
		// Already deleted
		log.Printf("[GC Worker] FS object %s not found (may already be deleted)", item.ItemID)
		return nil
	}

	// If it's a file with blocks, decrement ref counts and enqueue blocks that hit 0
	if len(blockIDsList) > 0 {
		zeroRefBlocks := w.decrementAndFindZeroRef(item.OrgID, blockIDsList)
		// Get storage class for block deletion
		var storageClass string
		w.db.Session().Query(`
			SELECT storage_class FROM libraries WHERE org_id = ? AND library_id = ?
		`, item.OrgID, item.LibraryID).Scan(&storageClass)

		for _, blockID := range zeroRefBlocks {
			w.queue.Enqueue(item.OrgID, ItemBlock, blockID, item.LibraryID, storageClass)
		}
	}

	if w.dryRun {
		log.Printf("[GC Worker] DRY RUN: Would delete fs_object %s from library %s", item.ItemID, item.LibraryID)
		return nil
	}

	// Delete the fs_object
	if err := w.db.Session().Query(`
		DELETE FROM fs_objects WHERE library_id = ? AND fs_id = ?
	`, item.LibraryID, item.ItemID).Exec(); err != nil {
		return fmt.Errorf("failed to delete fs_object: %w", err)
	}

	log.Printf("[GC Worker] Deleted fs_object %s", item.ItemID)
	return nil
}

func (w *Worker) processBlockMapping(ctx context.Context, item QueueItem) error {
	if w.dryRun {
		log.Printf("[GC Worker] DRY RUN: Would delete block mapping %s", item.ItemID)
		return nil
	}

	if err := w.db.Session().Query(`
		DELETE FROM block_id_mappings WHERE org_id = ? AND external_id = ?
	`, item.OrgID, item.ItemID).Exec(); err != nil {
		return fmt.Errorf("failed to delete block mapping: %w", err)
	}

	return nil
}

func (w *Worker) processShareLink(ctx context.Context, item QueueItem) error {
	if w.dryRun {
		log.Printf("[GC Worker] DRY RUN: Would delete share link %s", item.ItemID)
		return nil
	}

	// Delete from main table
	if err := w.db.Session().Query(`
		DELETE FROM share_links WHERE share_token = ?
	`, item.ItemID).Exec(); err != nil {
		return fmt.Errorf("failed to delete share link: %w", err)
	}

	// Best-effort cleanup of share_links_by_creator
	// We need the creator to delete from the lookup table
	// Since the main record is gone, we can't look it up - the scanner will handle orphans

	return nil
}

// decrementAndFindZeroRef decrements ref_count for blocks and returns those that hit 0.
func (w *Worker) decrementAndFindZeroRef(orgID uuid.UUID, blockIDs []string) []string {
	var zeroRef []string
	for _, blockID := range blockIDs {
		// Decrement ref_count
		if err := w.db.Session().Query(`
			UPDATE blocks SET ref_count = ref_count - 1, last_accessed = ?
			WHERE org_id = ? AND block_id = ?
		`, time.Now(), orgID, blockID).Exec(); err != nil {
			continue
		}

		// Check if ref_count is now 0
		var refCount int
		if err := w.db.Session().Query(`
			SELECT ref_count FROM blocks WHERE org_id = ? AND block_id = ?
		`, orgID, blockID).Scan(&refCount); err != nil {
			continue
		}

		if refCount <= 0 {
			zeroRef = append(zeroRef, blockID)
		}
	}
	return zeroRef
}

// EnqueueLibraryContents enqueues all commits, fs_objects, and blocks for a deleted library.
func (w *Worker) EnqueueLibraryContents(orgID, libraryID uuid.UUID, storageClass string) error {
	// Enqueue all commits for this library
	commitIter := w.db.Session().Query(`
		SELECT commit_id, root_fs_id FROM commits WHERE library_id = ?
	`, libraryID).Iter()

	var commitID, rootFSID string
	fsIDSet := make(map[string]bool) // track unique fs_ids
	for commitIter.Scan(&commitID, &rootFSID) {
		w.queue.Enqueue(orgID, ItemCommit, commitID, libraryID, "")
		if rootFSID != "" {
			fsIDSet[rootFSID] = true
		}
	}
	commitIter.Close()

	// Enqueue all fs_objects for this library
	fsIter := w.db.Session().Query(`
		SELECT fs_id, block_ids FROM fs_objects WHERE library_id = ?
	`, libraryID).Iter()

	var fsID string
	var blockIDs []string
	for fsIter.Scan(&fsID, &blockIDs) {
		w.queue.Enqueue(orgID, ItemFSObject, fsID, libraryID, "")

		// Also enqueue blocks directly (they'll be re-verified before deletion)
		for _, blockID := range blockIDs {
			w.queue.Enqueue(orgID, ItemBlock, blockID, libraryID, storageClass)
		}
	}
	fsIter.Close()

	log.Printf("[GC Worker] Enqueued library %s contents for deletion", libraryID)
	return nil
}

