package gc

import (
	"testing"
)

func TestNewScanner(t *testing.T) {
	stats := &Stats{}
	q := NewQueue(nil)

	s := NewScanner(nil, q, stats)

	if s == nil {
		t.Fatal("NewScanner returned nil")
	}
	if s.queue != q {
		t.Error("scanner queue should match provided queue")
	}
	if s.stats != stats {
		t.Error("scanner stats should match provided stats")
	}
}

// Integration tests below require a Cassandra database connection.

func TestScanner_ScanOrphanedBlocks_Integration(t *testing.T) {
	t.Skip("requires Cassandra database connection")

	// When DB is available, this would test:
	// 1. Create org with 3 blocks: ref_count=0, ref_count=1, ref_count=0
	// 2. Run scanOrphanedBlocks
	// 3. Verify 2 items enqueued (the two with ref_count=0)
	// 4. Run again → should not double-enqueue (idempotent via gc_queue PK)
}

func TestScanner_ScanExpiredShareLinks_Integration(t *testing.T) {
	t.Skip("requires Cassandra database connection")

	// When DB is available, this would test:
	// 1. Create 3 share links: expired, not expired, no expiry (permanent)
	// 2. Run scanExpiredShareLinks
	// 3. Verify only 1 item enqueued (the expired one)
}

func TestScanner_ScanOrphanedCommits_Integration(t *testing.T) {
	t.Skip("requires Cassandra database connection")

	// When DB is available, this would test:
	// 1. Create library A with 2 commits
	// 2. Create library B with 1 commit
	// 3. Delete library B record (but leave commits)
	// 4. Run scanOrphanedCommits
	// 5. Verify 1 commit enqueued (library B's commit)
	// 6. Library A's commits not enqueued
}

func TestScanner_ScanOrphanedFSObjects_Integration(t *testing.T) {
	t.Skip("requires Cassandra database connection")

	// When DB is available, this would test:
	// 1. Create library A with 3 fs_objects
	// 2. Create library B with 2 fs_objects
	// 3. Delete library B record (but leave fs_objects)
	// 4. Run scanOrphanedFSObjects
	// 5. Verify 2 items enqueued (library B's fs_objects)
}

func TestScanner_ScanOnce_Integration(t *testing.T) {
	t.Skip("requires Cassandra database connection")

	// When DB is available, this would test the full scan pipeline:
	// 1. Set up: orphaned blocks + expired share links + orphaned commits
	// 2. Run ScanOnce
	// 3. Verify all orphaned items enqueued
	// 4. Verify stats.LastScanRun updated
}

func TestScanner_ScanOnce_EmptyDB_Integration(t *testing.T) {
	t.Skip("requires Cassandra database connection")

	// When DB is available, this would test:
	// 1. Empty database (no blocks, commits, share links)
	// 2. Run ScanOnce
	// 3. Verify 0 items enqueued, no errors
}
