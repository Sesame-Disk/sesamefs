package gc

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
)

// Worker drains the gc_queue and deletes items from S3 and the database.
type Worker struct {
	store          GCStore
	storage        StorageProvider
	queue          *Queue
	batchSize      int
	gracePeriod    time.Duration
	dryRun         bool
	stats          *Stats
}

// NewWorker creates a new GC worker.
func NewWorker(store GCStore, storage StorageProvider, queue *Queue, batchSize int, gracePeriod time.Duration, dryRun bool, stats *Stats) *Worker {
	return &Worker{
		store:       store,
		storage:     storage,
		queue:       queue,
		batchSize:   batchSize,
		gracePeriod: gracePeriod,
		dryRun:      dryRun,
		stats:       stats,
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
	refCount, err := w.store.GetBlockRefCount(item.OrgID, item.ItemID)
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

	if w.storage != nil {
		blockStore, err := w.storage.GetBlockStore(storageClass)
		if err != nil {
			return fmt.Errorf("failed to get block store for class %s: %w", storageClass, err)
		}
		if err := blockStore.DeleteBlock(ctx, item.ItemID); err != nil {
			return fmt.Errorf("failed to delete block from S3: %w", err)
		}
	}

	// Delete block record from DB
	if err := w.store.DeleteBlock(item.OrgID, item.ItemID); err != nil {
		return fmt.Errorf("failed to delete block record: %w", err)
	}

	// Delete block_id_mappings where internal_id matches this block
	mappings, err := w.store.ListBlockMappings(item.OrgID)
	if err == nil {
		for _, mapping := range mappings {
			if mapping.InternalID == item.ItemID {
				w.store.DeleteBlockMapping(item.OrgID, mapping.ExternalID)
			}
		}
	}

	w.stats.IncrBlocksDeleted()
	log.Printf("[GC Worker] Deleted block %s", item.ItemID)
	return nil
}

func (w *Worker) processCommit(ctx context.Context, item QueueItem) error {
	if w.dryRun {
		log.Printf("[GC Worker] DRY RUN: Would delete commit %s from library %s", item.ItemID, item.LibraryID)
		return nil
	}

	if err := w.store.DeleteCommit(item.LibraryID, item.ItemID); err != nil {
		return fmt.Errorf("failed to delete commit: %w", err)
	}

	log.Printf("[GC Worker] Deleted commit %s", item.ItemID)
	return nil
}

func (w *Worker) processFSObject(ctx context.Context, item QueueItem) error {
	// Get the fs_object to find its block_ids
	fsObj, err := w.store.GetFSObject(item.LibraryID, item.ItemID)
	if err != nil {
		// Already deleted
		log.Printf("[GC Worker] FS object %s not found (may already be deleted)", item.ItemID)
		return nil
	}

	// If it's a file with blocks, decrement ref counts and enqueue blocks that hit 0
	if len(fsObj.BlockIDs) > 0 {
		zeroRefBlocks := w.decrementAndFindZeroRef(item.OrgID, fsObj.BlockIDs)
		storageClass, _ := w.store.GetLibraryStorageClass(item.OrgID, item.LibraryID)

		for _, blockID := range zeroRefBlocks {
			w.queue.Enqueue(item.OrgID, ItemBlock, blockID, item.LibraryID, storageClass)
		}
	}

	if w.dryRun {
		log.Printf("[GC Worker] DRY RUN: Would delete fs_object %s from library %s", item.ItemID, item.LibraryID)
		return nil
	}

	// Delete the fs_object
	if err := w.store.DeleteFSObject(item.LibraryID, item.ItemID); err != nil {
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

	if err := w.store.DeleteBlockMapping(item.OrgID, item.ItemID); err != nil {
		return fmt.Errorf("failed to delete block mapping: %w", err)
	}

	return nil
}

func (w *Worker) processShareLink(ctx context.Context, item QueueItem) error {
	if w.dryRun {
		log.Printf("[GC Worker] DRY RUN: Would delete share link %s", item.ItemID)
		return nil
	}

	// For share links, the item_id is the share_token
	// We don't have a dedicated store method for share link deletion via GC,
	// but the share link cleanup is handled by the scanner phase
	log.Printf("[GC Worker] Processed share link %s", item.ItemID)
	return nil
}

// decrementAndFindZeroRef decrements ref_count for blocks and returns those that hit 0.
func (w *Worker) decrementAndFindZeroRef(orgID uuid.UUID, blockIDs []string) []string {
	var zeroRef []string
	for _, blockID := range blockIDs {
		if err := w.store.DecrementBlockRefCount(orgID, blockID); err != nil {
			continue
		}

		refCount, err := w.store.GetBlockRefCount(orgID, blockID)
		if err != nil {
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
	commits, err := w.store.ListCommitsForLibrary(libraryID)
	if err != nil {
		return fmt.Errorf("failed to list commits for library %s: %w", libraryID, err)
	}
	for _, c := range commits {
		w.queue.Enqueue(orgID, ItemCommit, c.CommitID, libraryID, "")
	}

	// Enqueue all fs_objects and their blocks
	fsObjects, err := w.store.ListFSObjectsForLibrary(libraryID)
	if err != nil {
		return fmt.Errorf("failed to list fs_objects for library %s: %w", libraryID, err)
	}
	for _, obj := range fsObjects {
		w.queue.Enqueue(orgID, ItemFSObject, obj.FSID, libraryID, "")
		for _, blockID := range obj.BlockIDs {
			w.queue.Enqueue(orgID, ItemBlock, blockID, libraryID, storageClass)
		}
	}

	log.Printf("[GC Worker] Enqueued library %s contents for deletion", libraryID)
	return nil
}
