package gc

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewWorker(t *testing.T) {
	stats := &Stats{}
	q := NewQueue(nil)

	w := NewWorker(nil, nil, q, 100, 1*time.Hour, false, stats)

	if w == nil {
		t.Fatal("NewWorker returned nil")
	}
	if w.batchSize != 100 {
		t.Errorf("batchSize = %d, want 100", w.batchSize)
	}
	if w.gracePeriod != 1*time.Hour {
		t.Errorf("gracePeriod = %v, want 1h", w.gracePeriod)
	}
	if w.dryRun {
		t.Error("dryRun should be false")
	}
}

func TestNewWorker_DryRun(t *testing.T) {
	stats := &Stats{}
	q := NewQueue(nil)

	w := NewWorker(nil, nil, q, 50, 30*time.Minute, true, stats)

	if !w.dryRun {
		t.Error("dryRun should be true when passed true")
	}
	if w.batchSize != 50 {
		t.Errorf("batchSize = %d, want 50", w.batchSize)
	}
}

func TestWorker_ProcessItem_UnknownType(t *testing.T) {
	t.Skip("requires Cassandra database connection")

	// When DB is available, this would test:
	// Worker.processItem with an unknown ItemType returns an error
}

// Integration tests below require a Cassandra database connection and S3/Minio.

func TestWorker_ProcessBlock_RefCountPositive_Integration(t *testing.T) {
	t.Skip("requires Cassandra database and S3 connection")

	// When environment is available, this would test:
	// 1. Create a block with ref_count=2
	// 2. Enqueue it as a block GC item
	// 3. Worker processes it → should skip (ref_count > 0)
	// 4. Block still exists in DB and S3
}

func TestWorker_ProcessBlock_RefCountZero_Integration(t *testing.T) {
	t.Skip("requires Cassandra database and S3 connection")

	// When environment is available, this would test:
	// 1. Create a block with ref_count=0
	// 2. Enqueue it as a block GC item
	// 3. Worker processes it → should delete from S3 and DB
	// 4. Block no longer exists
}

func TestWorker_ProcessBlock_DryRun_Integration(t *testing.T) {
	t.Skip("requires Cassandra database and S3 connection")

	// When environment is available, this would test:
	// 1. Create a block with ref_count=0
	// 2. Worker in dryRun mode processes it
	// 3. Block still exists (not deleted in dry run)
}

func TestWorker_ProcessFSObject_CascadeBlocks_Integration(t *testing.T) {
	t.Skip("requires Cassandra database connection")

	// When environment is available, this would test:
	// 1. Create fs_object (file) with 3 blocks
	// 2. Each block has ref_count=1
	// 3. Worker processes the fs_object
	// 4. Block ref_counts decremented to 0
	// 5. 3 new block items enqueued in gc_queue
}

func TestWorker_ProcessCommit_Integration(t *testing.T) {
	t.Skip("requires Cassandra database connection")

	// When environment is available, this would test:
	// 1. Create a commit record
	// 2. Enqueue it as a commit GC item
	// 3. Worker processes it → commit deleted
}

func TestWorker_RetryOnFailure_Integration(t *testing.T) {
	t.Skip("requires Cassandra database connection")

	// When environment is available, this would test:
	// 1. Enqueue an item
	// 2. Make processing fail (e.g., S3 is down)
	// 3. Verify retry_count incremented
	// 4. Item remains in queue for re-processing
}

func TestWorker_EnqueueLibraryContents_Integration(t *testing.T) {
	t.Skip("requires Cassandra database connection")

	// When environment is available, this would test:
	// 1. Create a library with 2 commits, 3 fs_objects, 5 blocks
	// 2. Call EnqueueLibraryContents
	// 3. Verify gc_queue contains items for all commits, fs_objects, blocks
}

func TestWorker_ProcessOnce_EmptyQueue_Integration(t *testing.T) {
	t.Skip("requires Cassandra database connection")

	// When environment is available, this would test:
	// 1. Empty gc_queue
	// 2. ProcessOnce returns 0 processed, no error
}

// Unit test for QueueItem type conversion
func TestQueueItem_TypeConversion(t *testing.T) {
	// Verify ItemType string→enum conversion (used in DequeueBatch)
	tests := []struct {
		str  string
		want ItemType
	}{
		{"block", ItemBlock},
		{"commit", ItemCommit},
		{"fs_object", ItemFSObject},
		{"block_mapping", ItemBlockMapping},
		{"share_link", ItemShareLink},
	}

	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			got := ItemType(tt.str)
			if got != tt.want {
				t.Errorf("ItemType(%q) = %v, want %v", tt.str, got, tt.want)
			}
		})
	}
}

// Unit test for QueueItem with uuid.Nil
func TestQueueItem_NilUUID(t *testing.T) {
	item := QueueItem{
		OrgID:     uuid.Nil,
		LibraryID: uuid.Nil,
		ItemType:  ItemBlock,
		ItemID:    "test-block-id",
	}

	if item.OrgID != uuid.Nil {
		t.Error("OrgID should be uuid.Nil")
	}
	if item.LibraryID != uuid.Nil {
		t.Error("LibraryID should be uuid.Nil")
	}
}
