package gc

import (
	"fmt"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/google/uuid"
)

// ItemType identifies the kind of object in the GC queue
type ItemType string

const (
	ItemBlock        ItemType = "block"
	ItemCommit       ItemType = "commit"
	ItemFSObject     ItemType = "fs_object"
	ItemBlockMapping ItemType = "block_mapping"
	ItemShareLink    ItemType = "share_link"
)

// QueueItem represents a single item pending garbage collection
type QueueItem struct {
	OrgID        uuid.UUID
	QueuedAt     time.Time
	ItemType     ItemType
	ItemID       string
	LibraryID    uuid.UUID
	StorageClass string
	RetryCount   int
}

// Queue provides operations for the gc_queue Cassandra table
type Queue struct {
	db *db.DB
}

// NewQueue creates a new Queue instance
func NewQueue(database *db.DB) *Queue {
	return &Queue{db: database}
}

// Enqueue inserts an item into the gc_queue for later deletion.
func (q *Queue) Enqueue(orgID uuid.UUID, itemType ItemType, itemID string, libraryID uuid.UUID, storageClass string) error {
	return q.db.Session().Query(`
		INSERT INTO gc_queue (org_id, queued_at, item_type, item_id, library_id, storage_class, retry_count)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, orgID, time.Now(), string(itemType), itemID, libraryID, storageClass, 0).Exec()
}

// DequeueBatch retrieves the oldest items from the queue for a given org
// that are older than minAge (grace period). Returns up to batchSize items.
func (q *Queue) DequeueBatch(orgID uuid.UUID, batchSize int, minAge time.Duration) ([]QueueItem, error) {
	cutoff := time.Now().Add(-minAge)

	iter := q.db.Session().Query(`
		SELECT org_id, queued_at, item_type, item_id, library_id, storage_class, retry_count
		FROM gc_queue
		WHERE org_id = ? AND queued_at < ?
		LIMIT ?
	`, orgID, cutoff, batchSize).Iter()

	var items []QueueItem
	var item QueueItem
	var itemTypeStr string

	for iter.Scan(&item.OrgID, &item.QueuedAt, &itemTypeStr, &item.ItemID,
		&item.LibraryID, &item.StorageClass, &item.RetryCount) {
		item.ItemType = ItemType(itemTypeStr)
		items = append(items, item)
		item = QueueItem{} // reset for next scan
	}

	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("failed to dequeue batch: %w", err)
	}

	return items, nil
}

// Complete removes a processed item from the gc_queue.
func (q *Queue) Complete(orgID uuid.UUID, queuedAt time.Time, itemType ItemType, itemID string) error {
	return q.db.Session().Query(`
		DELETE FROM gc_queue
		WHERE org_id = ? AND queued_at = ? AND item_type = ? AND item_id = ?
	`, orgID, queuedAt, string(itemType), itemID).Exec()
}

// IncrementRetry updates the retry count for a failed item.
// Since retry_count is part of a non-PK column, we can update in place.
func (q *Queue) IncrementRetry(orgID uuid.UUID, queuedAt time.Time, itemType ItemType, itemID string, currentRetry int) error {
	return q.db.Session().Query(`
		UPDATE gc_queue SET retry_count = ?
		WHERE org_id = ? AND queued_at = ? AND item_type = ? AND item_id = ?
	`, currentRetry+1, orgID, queuedAt, string(itemType), itemID).Exec()
}

// GetQueueSize returns the approximate number of items in the queue for an org.
func (q *Queue) GetQueueSize(orgID uuid.UUID) (int, error) {
	var count int
	err := q.db.Session().Query(`
		SELECT COUNT(*) FROM gc_queue WHERE org_id = ?
	`, orgID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get queue size: %w", err)
	}
	return count, nil
}

// GetTotalQueueSize returns the approximate total number of items across all orgs.
// Note: This does a full table scan and should only be used for admin status endpoints.
func (q *Queue) GetTotalQueueSize() (int, error) {
	var count int
	err := q.db.Session().Query(`SELECT COUNT(*) FROM gc_queue`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get total queue size: %w", err)
	}
	return count, nil
}

// ListOrgsWithQueuedItems returns org_ids that have items in the gc_queue.
// Used by the worker to find orgs to process.
func (q *Queue) ListOrgsWithQueuedItems() ([]uuid.UUID, error) {
	iter := q.db.Session().Query(`SELECT DISTINCT org_id FROM gc_queue`).Iter()
	var orgs []uuid.UUID
	var orgID uuid.UUID
	for iter.Scan(&orgID) {
		orgs = append(orgs, orgID)
	}
	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("failed to list orgs: %w", err)
	}
	return orgs, nil
}
