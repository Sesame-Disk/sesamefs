package gc

import (
	"testing"

	"github.com/google/uuid"
)

func TestItemType_Constants(t *testing.T) {
	// Verify item type string values match the database schema expectations
	tests := []struct {
		itemType ItemType
		want     string
	}{
		{ItemBlock, "block"},
		{ItemCommit, "commit"},
		{ItemFSObject, "fs_object"},
		{ItemBlockMapping, "block_mapping"},
		{ItemShareLink, "share_link"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if string(tt.itemType) != tt.want {
				t.Errorf("ItemType = %q, want %q", string(tt.itemType), tt.want)
			}
		})
	}
}

func TestQueueItem_Fields(t *testing.T) {
	orgID := uuid.New()
	libID := uuid.New()

	item := QueueItem{
		OrgID:        orgID,
		ItemType:     ItemBlock,
		ItemID:       "abc123def456",
		LibraryID:    libID,
		StorageClass: "hot",
		RetryCount:   0,
	}

	if item.OrgID != orgID {
		t.Errorf("OrgID = %v, want %v", item.OrgID, orgID)
	}
	if item.ItemType != ItemBlock {
		t.Errorf("ItemType = %v, want %v", item.ItemType, ItemBlock)
	}
	if item.ItemID != "abc123def456" {
		t.Errorf("ItemID = %v, want abc123def456", item.ItemID)
	}
	if item.LibraryID != libID {
		t.Errorf("LibraryID = %v, want %v", item.LibraryID, libID)
	}
	if item.StorageClass != "hot" {
		t.Errorf("StorageClass = %v, want hot", item.StorageClass)
	}
	if item.RetryCount != 0 {
		t.Errorf("RetryCount = %v, want 0", item.RetryCount)
	}
}

func TestNewQueue_NilDB(t *testing.T) {
	// NewQueue should not panic with nil DB
	q := NewQueue(nil)
	if q == nil {
		t.Fatal("NewQueue(nil) returned nil")
	}
	if q.db != nil {
		t.Error("db should be nil when created with nil")
	}
}

// Integration tests below require a Cassandra database connection.
// They are skipped when no DB is available (standard test mode).

func TestQueue_Enqueue_Integration(t *testing.T) {
	t.Skip("requires Cassandra database connection")

	// When DB is available, this would test:
	// 1. Enqueue an item
	// 2. DequeueBatch retrieves it
	// 3. Complete removes it
	// 4. DequeueBatch returns empty
}

func TestQueue_DequeueBatch_GracePeriod_Integration(t *testing.T) {
	t.Skip("requires Cassandra database connection")

	// When DB is available, this would test:
	// 1. Enqueue an item at time.Now()
	// 2. DequeueBatch with 1h grace period returns empty (item too new)
	// 3. DequeueBatch with 0s grace period returns the item
}

func TestQueue_IncrementRetry_Integration(t *testing.T) {
	t.Skip("requires Cassandra database connection")

	// When DB is available, this would test:
	// 1. Enqueue item with retry_count=0
	// 2. IncrementRetry → retry_count=1
	// 3. IncrementRetry → retry_count=2
}

func TestQueue_ListOrgsWithQueuedItems_Integration(t *testing.T) {
	t.Skip("requires Cassandra database connection")

	// When DB is available, this would test:
	// 1. Enqueue items for org1 and org2
	// 2. ListOrgsWithQueuedItems returns both
	// 3. Complete all items for org1
	// 4. ListOrgsWithQueuedItems returns only org2
}

func TestQueue_GetQueueSize_Integration(t *testing.T) {
	t.Skip("requires Cassandra database connection")

	// When DB is available, this would test:
	// 1. GetQueueSize for empty org returns 0
	// 2. Enqueue 3 items
	// 3. GetQueueSize returns 3
	// 4. Complete 1 item
	// 5. GetQueueSize returns 2
}
