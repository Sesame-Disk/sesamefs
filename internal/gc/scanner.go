package gc

import (
	"context"
	"log"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/metrics"
	"github.com/google/uuid"
)

// Scanner periodically finds orphaned items that were missed by inline enqueue
// and adds them to the gc_queue for processing.
type Scanner struct {
	store GCStore
	queue *Queue
	stats *Stats
}

// NewScanner creates a new safety scanner.
func NewScanner(store GCStore, queue *Queue, stats *Stats) *Scanner {
	return &Scanner{
		store: store,
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

	n, err = s.scanExpiredVersions(ctx)
	if err != nil {
		log.Printf("[GC Scanner] Error scanning expired versions: %v", err)
	}
	enqueued += n

	n, err = s.scanAutoDeleteExpiredObjects(ctx)
	if err != nil {
		log.Printf("[GC Scanner] Error scanning auto-delete expired objects: %v", err)
	}
	enqueued += n

	elapsed := time.Since(start)
	log.Printf("[GC Scanner] Safety scan complete: enqueued %d items in %v", enqueued, elapsed)
	s.stats.SetLastScanRun(time.Now())
	return nil
}

// scanOrphanedBlocks finds blocks with ref_count <= 0 and enqueues them.
func (s *Scanner) scanOrphanedBlocks(ctx context.Context) (int, error) {
	log.Println("[GC Scanner] Phase 1: Scanning for orphaned blocks...")

	orgs, err := s.store.ListOrganizations()
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

		blocks, err := s.store.ListBlocksForOrg(orgID)
		if err != nil {
			continue
		}

		for _, b := range blocks {
			if b.RefCount <= 0 {
				if err := s.queue.Enqueue(orgID, ItemBlock, b.BlockID, uuid.Nil, b.StorageClass); err == nil {
					enqueued++
				}
			}
		}
	}

	log.Printf("[GC Scanner] Phase 1 complete: enqueued %d orphaned blocks", enqueued)
	metrics.GCItemsEnqueuedTotal.WithLabelValues("orphaned_blocks").Add(float64(enqueued))
	metrics.GCScannerLastPhaseRun.WithLabelValues("orphaned_blocks").SetToCurrentTime()
	return enqueued, nil
}

// scanExpiredShareLinks finds share links past their expiration date.
func (s *Scanner) scanExpiredShareLinks(ctx context.Context) (int, error) {
	log.Println("[GC Scanner] Phase 2: Scanning for expired share links...")

	now := time.Now()
	links, err := s.store.ListShareLinks()
	if err != nil {
		return 0, err
	}

	enqueued := 0
	for _, link := range links {
		select {
		case <-ctx.Done():
			return enqueued, ctx.Err()
		default:
		}

		if !link.ExpiresAt.IsZero() && link.ExpiresAt.Before(now) {
			if err := s.queue.Enqueue(link.OrgID, ItemShareLink, link.ShareToken, uuid.Nil, ""); err == nil {
				enqueued++
			}
		}
	}

	log.Printf("[GC Scanner] Phase 2 complete: enqueued %d expired share links", enqueued)
	metrics.GCItemsEnqueuedTotal.WithLabelValues("expired_links").Add(float64(enqueued))
	metrics.GCScannerLastPhaseRun.WithLabelValues("expired_links").SetToCurrentTime()
	return enqueued, nil
}

// scanOrphanedCommits finds commits whose library no longer exists.
func (s *Scanner) scanOrphanedCommits(ctx context.Context) (int, error) {
	log.Println("[GC Scanner] Phase 3: Scanning for orphaned commits...")

	libraryIDs, err := s.store.ListDistinctCommitLibraries()
	if err != nil {
		return 0, err
	}

	enqueued := 0
	for _, libID := range libraryIDs {
		select {
		case <-ctx.Done():
			return enqueued, ctx.Err()
		default:
		}

		exists, err := s.store.LibraryExists(libID)
		if err != nil || exists {
			continue
		}

		// Library doesn't exist - enqueue all its commits
		orgID, err := s.store.FindOrgForLibrary(libID)
		if err != nil || orgID == uuid.Nil {
			continue
		}

		commitIDs, err := s.store.ListCommitIDsForLibrary(libID)
		if err != nil {
			continue
		}
		for _, commitID := range commitIDs {
			if err := s.queue.Enqueue(orgID, ItemCommit, commitID, libID, ""); err == nil {
				enqueued++
			}
		}
	}

	log.Printf("[GC Scanner] Phase 3 complete: enqueued %d orphaned commits", enqueued)
	metrics.GCItemsEnqueuedTotal.WithLabelValues("orphaned_commits").Add(float64(enqueued))
	metrics.GCScannerLastPhaseRun.WithLabelValues("orphaned_commits").SetToCurrentTime()
	return enqueued, nil
}

// scanOrphanedFSObjects finds fs_objects whose library no longer exists.
func (s *Scanner) scanOrphanedFSObjects(ctx context.Context) (int, error) {
	log.Println("[GC Scanner] Phase 4: Scanning for orphaned fs_objects...")

	libraryIDs, err := s.store.ListDistinctFSObjectLibraries()
	if err != nil {
		return 0, err
	}

	enqueued := 0
	for _, libID := range libraryIDs {
		select {
		case <-ctx.Done():
			return enqueued, ctx.Err()
		default:
		}

		exists, err := s.store.LibraryExists(libID)
		if err != nil || exists {
			continue
		}

		orgID, err := s.store.FindOrgForLibrary(libID)
		if err != nil || orgID == uuid.Nil {
			continue
		}

		fsIDs, err := s.store.ListFSObjectIDsForLibrary(libID)
		if err != nil {
			continue
		}
		for _, fsID := range fsIDs {
			if err := s.queue.Enqueue(orgID, ItemFSObject, fsID, libID, ""); err == nil {
				enqueued++
			}
		}
	}

	log.Printf("[GC Scanner] Phase 4 complete: enqueued %d orphaned fs_objects", enqueued)
	metrics.GCItemsEnqueuedTotal.WithLabelValues("orphaned_fs_objects").Add(float64(enqueued))
	metrics.GCScannerLastPhaseRun.WithLabelValues("orphaned_fs_objects").SetToCurrentTime()
	return enqueued, nil
}

// scanExpiredVersions finds commits older than the library's version_ttl_days
// that are NOT in the HEAD commit chain, and enqueues them for deletion.
func (s *Scanner) scanExpiredVersions(ctx context.Context) (int, error) {
	log.Println("[GC Scanner] Phase 5: Scanning for expired versions...")

	libs, err := s.store.ListLibrariesWithVersionTTL()
	if err != nil {
		return 0, err
	}

	enqueued := 0
	for _, lib := range libs {
		select {
		case <-ctx.Done():
			return enqueued, ctx.Err()
		default:
		}

		commits, err := s.store.ListCommitsWithTimestamps(lib.LibraryID)
		if err != nil {
			log.Printf("[GC Scanner] Phase 5: failed to list commits for library %s: %v", lib.LibraryID, err)
			continue
		}

		// Build a lookup map for walking the parent chain
		commitMap := make(map[string]CommitWithTimestamp, len(commits))
		for _, c := range commits {
			commitMap[c.CommitID] = c
		}

		// Walk HEAD chain to build the keep set
		keepSet := make(map[string]bool)
		current := lib.HeadCommitID
		for current != "" {
			if keepSet[current] {
				break // cycle protection
			}
			keepSet[current] = true
			if c, ok := commitMap[current]; ok {
				current = c.ParentID
			} else {
				break
			}
		}

		// Find expired commits not in keep set
		cutoff := time.Now().AddDate(0, 0, -lib.VersionTTLDays)
		for _, c := range commits {
			if keepSet[c.CommitID] {
				continue
			}
			if c.CreatedAt.Before(cutoff) {
				if err := s.queue.Enqueue(lib.OrgID, ItemCommit, c.CommitID, lib.LibraryID, ""); err == nil {
					enqueued++
				}
			}
		}
	}

	log.Printf("[GC Scanner] Phase 5 complete: enqueued %d expired version commits", enqueued)
	metrics.GCItemsEnqueuedTotal.WithLabelValues("expired_versions").Add(float64(enqueued))
	metrics.GCScannerLastPhaseRun.WithLabelValues("expired_versions").SetToCurrentTime()
	return enqueued, nil
}

// scanAutoDeleteExpiredObjects finds fs_objects that are not referenced by the
// HEAD commit tree or any recent commit tree (within auto_delete_days), and
// enqueues them for deletion.
func (s *Scanner) scanAutoDeleteExpiredObjects(ctx context.Context) (int, error) {
	log.Println("[GC Scanner] Phase 6: Scanning for auto-delete expired fs_objects...")

	libs, err := s.store.ListLibrariesWithAutoDelete()
	if err != nil {
		return 0, err
	}

	enqueued := 0
	for _, lib := range libs {
		select {
		case <-ctx.Done():
			return enqueued, ctx.Err()
		default:
		}

		commits, err := s.store.ListCommitsWithTimestamps(lib.LibraryID)
		if err != nil {
			log.Printf("[GC Scanner] Phase 6: failed to list commits for library %s: %v", lib.LibraryID, err)
			continue
		}

		// Build a lookup map for walking the parent chain
		commitMap := make(map[string]CommitWithTimestamp, len(commits))
		for _, c := range commits {
			commitMap[c.CommitID] = c
		}

		// Walk HEAD chain to build keepCommits
		keepCommits := make(map[string]bool)
		current := lib.HeadCommitID
		for current != "" {
			if keepCommits[current] {
				break // cycle protection
			}
			keepCommits[current] = true
			if c, ok := commitMap[current]; ok {
				current = c.ParentID
			} else {
				break
			}
		}

		// Add commits within auto_delete_days window to keepCommits
		cutoff := time.Now().AddDate(0, 0, -lib.AutoDeleteDays)
		for _, c := range commits {
			if !c.CreatedAt.Before(cutoff) {
				keepCommits[c.CommitID] = true
			}
		}

		// Walk filesystem trees of all keepCommits to build keepFSSet
		keepFSSet := make(map[string]bool)
		for commitID := range keepCommits {
			if c, ok := commitMap[commitID]; ok && c.RootFSID != "" {
				s.walkFSTree(lib.LibraryID, c.RootFSID, keepFSSet)
			}
		}

		// List all fs_object IDs for this library and enqueue orphans
		allFSIDs, err := s.store.ListFSObjectIDsForLibrary(lib.LibraryID)
		if err != nil {
			log.Printf("[GC Scanner] Phase 6: failed to list fs_objects for library %s: %v", lib.LibraryID, err)
			continue
		}

		for _, fsID := range allFSIDs {
			if !keepFSSet[fsID] {
				if err := s.queue.Enqueue(lib.OrgID, ItemFSObject, fsID, lib.LibraryID, ""); err == nil {
					enqueued++
				}
			}
		}
	}

	log.Printf("[GC Scanner] Phase 6 complete: enqueued %d auto-delete expired fs_objects", enqueued)
	metrics.GCItemsEnqueuedTotal.WithLabelValues("auto_delete").Add(float64(enqueued))
	metrics.GCScannerLastPhaseRun.WithLabelValues("auto_delete").SetToCurrentTime()
	return enqueued, nil
}

// walkFSTree recursively walks a filesystem tree starting from fsID,
// adding all visited fs_ids to the visited set.
func (s *Scanner) walkFSTree(libraryID uuid.UUID, fsID string, visited map[string]bool) {
	if fsID == "" || visited[fsID] {
		return
	}
	visited[fsID] = true

	obj, err := s.store.GetFSObject(libraryID, fsID)
	if err != nil {
		return
	}

	for _, childID := range obj.DirEntries {
		s.walkFSTree(libraryID, childID, visited)
	}
}
