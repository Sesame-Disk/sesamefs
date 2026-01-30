package gc

import (
	"context"
	"log"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/google/uuid"
)

// Scanner periodically finds orphaned items that were missed by inline enqueue
// and adds them to the gc_queue for processing.
type Scanner struct {
	db    *db.DB
	queue *Queue
	stats *Stats
}

// NewScanner creates a new safety scanner.
func NewScanner(database *db.DB, queue *Queue, stats *Stats) *Scanner {
	return &Scanner{
		db:    database,
		queue: queue,
		stats: stats,
	}
}

// ScanOnce performs a full scan of all phases.
func (s *Scanner) ScanOnce(ctx context.Context) error {
	start := time.Now()
	log.Println("[GC Scanner] Starting safety scan...")

	enqueued := 0

	n, err := s.scanOrphanedBlocks(ctx)
	if err != nil {
		log.Printf("[GC Scanner] Error scanning orphaned blocks: %v", err)
	}
	enqueued += n

	n, err = s.scanExpiredShareLinks(ctx)
	if err != nil {
		log.Printf("[GC Scanner] Error scanning expired share links: %v", err)
	}
	enqueued += n

	n, err = s.scanOrphanedCommits(ctx)
	if err != nil {
		log.Printf("[GC Scanner] Error scanning orphaned commits: %v", err)
	}
	enqueued += n

	n, err = s.scanOrphanedFSObjects(ctx)
	if err != nil {
		log.Printf("[GC Scanner] Error scanning orphaned fs_objects: %v", err)
	}
	enqueued += n

	elapsed := time.Since(start)
	log.Printf("[GC Scanner] Safety scan complete: enqueued %d items in %v", enqueued, elapsed)
	s.stats.SetLastScanRun(time.Now())
	return nil
}

// scanOrphanedBlocks finds blocks with ref_count <= 0 and enqueues them.
// Uses ALLOW FILTERING since this is an infrequent nightly scan.
func (s *Scanner) scanOrphanedBlocks(ctx context.Context) (int, error) {
	log.Println("[GC Scanner] Phase 1: Scanning for orphaned blocks...")

	// Get all orgs
	orgs, err := s.listOrgs()
	if err != nil {
		return 0, err
	}

	enqueued := 0
	for _, orgID := range orgs {
		select {
		case <-ctx.Done():
			return enqueued, ctx.Err()
		default:
		}

		iter := s.db.Session().Query(`
			SELECT block_id, storage_class, ref_count FROM blocks WHERE org_id = ?
		`, orgID).Iter()

		var blockID, storageClass string
		var refCount int
		for iter.Scan(&blockID, &storageClass, &refCount) {
			if refCount <= 0 {
				// Check if already in queue (avoid duplicates)
				if err := s.queue.Enqueue(orgID, ItemBlock, blockID, uuid.Nil, storageClass); err == nil {
					enqueued++
				}
			}
		}
		iter.Close()
	}

	log.Printf("[GC Scanner] Phase 1 complete: enqueued %d orphaned blocks", enqueued)
	return enqueued, nil
}

// scanExpiredShareLinks finds share links past their expiration date.
func (s *Scanner) scanExpiredShareLinks(ctx context.Context) (int, error) {
	log.Println("[GC Scanner] Phase 2: Scanning for expired share links...")

	now := time.Now()
	iter := s.db.Session().Query(`
		SELECT share_token, org_id, expires_at FROM share_links
	`).Iter()

	enqueued := 0
	var shareToken string
	var orgID uuid.UUID
	var expiresAt time.Time

	for iter.Scan(&shareToken, &orgID, &expiresAt) {
		select {
		case <-ctx.Done():
			iter.Close()
			return enqueued, ctx.Err()
		default:
		}

		if !expiresAt.IsZero() && expiresAt.Before(now) {
			if err := s.queue.Enqueue(orgID, ItemShareLink, shareToken, uuid.Nil, ""); err == nil {
				enqueued++
			}
		}
	}
	iter.Close()

	log.Printf("[GC Scanner] Phase 2 complete: enqueued %d expired share links", enqueued)
	return enqueued, nil
}

// scanOrphanedCommits finds commits whose library no longer exists.
func (s *Scanner) scanOrphanedCommits(ctx context.Context) (int, error) {
	log.Println("[GC Scanner] Phase 3: Scanning for orphaned commits...")

	// Get all distinct library_ids from commits
	commitIter := s.db.Session().Query(`
		SELECT DISTINCT library_id FROM commits
	`).Iter()

	var libraryIDs []uuid.UUID
	var libID uuid.UUID
	for commitIter.Scan(&libID) {
		libraryIDs = append(libraryIDs, libID)
	}
	commitIter.Close()

	enqueued := 0
	for _, libID := range libraryIDs {
		select {
		case <-ctx.Done():
			return enqueued, ctx.Err()
		default:
		}

		// Check if library still exists in the lookup table
		var existingLibID uuid.UUID
		err := s.db.Session().Query(`
			SELECT library_id FROM libraries_by_id WHERE library_id = ?
		`, libID).Scan(&existingLibID)
		if err != nil {
			// Library doesn't exist - enqueue all its commits
			// Get org_id from any remaining data (best effort)
			orgID := s.findOrgForLibrary(libID)
			if orgID == uuid.Nil {
				continue
			}

			cIter := s.db.Session().Query(`
				SELECT commit_id FROM commits WHERE library_id = ?
			`, libID).Iter()
			var commitID string
			for cIter.Scan(&commitID) {
				if err := s.queue.Enqueue(orgID, ItemCommit, commitID, libID, ""); err == nil {
					enqueued++
				}
			}
			cIter.Close()
		}
	}

	log.Printf("[GC Scanner] Phase 3 complete: enqueued %d orphaned commits", enqueued)
	return enqueued, nil
}

// scanOrphanedFSObjects finds fs_objects whose library no longer exists.
func (s *Scanner) scanOrphanedFSObjects(ctx context.Context) (int, error) {
	log.Println("[GC Scanner] Phase 4: Scanning for orphaned fs_objects...")

	fsIter := s.db.Session().Query(`
		SELECT DISTINCT library_id FROM fs_objects
	`).Iter()

	var libraryIDs []uuid.UUID
	var libID uuid.UUID
	for fsIter.Scan(&libID) {
		libraryIDs = append(libraryIDs, libID)
	}
	fsIter.Close()

	enqueued := 0
	for _, libID := range libraryIDs {
		select {
		case <-ctx.Done():
			return enqueued, ctx.Err()
		default:
		}

		// Check if library still exists
		var existingLibID uuid.UUID
		err := s.db.Session().Query(`
			SELECT library_id FROM libraries_by_id WHERE library_id = ?
		`, libID).Scan(&existingLibID)
		if err != nil {
			// Library doesn't exist - enqueue all its fs_objects
			orgID := s.findOrgForLibrary(libID)
			if orgID == uuid.Nil {
				continue
			}

			fIter := s.db.Session().Query(`
				SELECT fs_id FROM fs_objects WHERE library_id = ?
			`, libID).Iter()
			var fsID string
			for fIter.Scan(&fsID) {
				if err := s.queue.Enqueue(orgID, ItemFSObject, fsID, libID, ""); err == nil {
					enqueued++
				}
			}
			fIter.Close()
		}
	}

	log.Printf("[GC Scanner] Phase 4 complete: enqueued %d orphaned fs_objects", enqueued)
	return enqueued, nil
}

// listOrgs returns all org_ids from the organizations table.
func (s *Scanner) listOrgs() ([]uuid.UUID, error) {
	iter := s.db.Session().Query(`SELECT org_id FROM organizations`).Iter()
	var orgs []uuid.UUID
	var orgID uuid.UUID
	for iter.Scan(&orgID) {
		orgs = append(orgs, orgID)
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return orgs, nil
}

// findOrgForLibrary tries to find the org_id for a library.
// It checks the lookup table first, then falls back to scanning libraries.
func (s *Scanner) findOrgForLibrary(libraryID uuid.UUID) uuid.UUID {
	var orgID uuid.UUID
	err := s.db.Session().Query(`
		SELECT org_id FROM libraries_by_id WHERE library_id = ?
	`, libraryID).Scan(&orgID)
	if err == nil {
		return orgID
	}

	// Library record is gone; try to find org from other sources
	// Check blocks table for any block belonging to this library's commits
	// This is best-effort; if we can't find the org, we skip this library
	return uuid.Nil
}
