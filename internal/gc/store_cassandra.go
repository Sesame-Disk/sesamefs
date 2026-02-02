package gc

import (
	"fmt"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	"github.com/google/uuid"
)

// CassandraStore implements GCStore using a Cassandra database.
type CassandraStore struct {
	db *db.DB
}

// NewCassandraStore creates a new CassandraStore.
func NewCassandraStore(database *db.DB) *CassandraStore {
	return &CassandraStore{db: database}
}

func (s *CassandraStore) EnqueueItem(orgID uuid.UUID, queuedAt time.Time, itemType ItemType, itemID string, libraryID uuid.UUID, storageClass string, retryCount int) error {
	return s.db.Session().Query(`
		INSERT INTO gc_queue (org_id, queued_at, item_type, item_id, library_id, storage_class, retry_count)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, orgID, queuedAt, string(itemType), itemID, libraryID, storageClass, retryCount).Exec()
}

func (s *CassandraStore) DequeueBatch(orgID uuid.UUID, batchSize int, cutoff time.Time) ([]QueueItem, error) {
	iter := s.db.Session().Query(`
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
		item = QueueItem{}
	}

	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("failed to dequeue batch: %w", err)
	}
	return items, nil
}

func (s *CassandraStore) CompleteItem(orgID uuid.UUID, queuedAt time.Time, itemType ItemType, itemID string) error {
	return s.db.Session().Query(`
		DELETE FROM gc_queue
		WHERE org_id = ? AND queued_at = ? AND item_type = ? AND item_id = ?
	`, orgID, queuedAt, string(itemType), itemID).Exec()
}

func (s *CassandraStore) UpdateRetryCount(orgID uuid.UUID, queuedAt time.Time, itemType ItemType, itemID string, retryCount int) error {
	return s.db.Session().Query(`
		UPDATE gc_queue SET retry_count = ?
		WHERE org_id = ? AND queued_at = ? AND item_type = ? AND item_id = ?
	`, retryCount, orgID, queuedAt, string(itemType), itemID).Exec()
}

func (s *CassandraStore) GetQueueSize(orgID uuid.UUID) (int, error) {
	var count int
	err := s.db.Session().Query(`
		SELECT COUNT(*) FROM gc_queue WHERE org_id = ?
	`, orgID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get queue size: %w", err)
	}
	return count, nil
}

func (s *CassandraStore) GetTotalQueueSize() (int, error) {
	var count int
	err := s.db.Session().Query(`SELECT COUNT(*) FROM gc_queue`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get total queue size: %w", err)
	}
	return count, nil
}

func (s *CassandraStore) ListOrgsWithQueuedItems() ([]uuid.UUID, error) {
	iter := s.db.Session().Query(`SELECT DISTINCT org_id FROM gc_queue`).Iter()
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

func (s *CassandraStore) GetBlockRefCount(orgID uuid.UUID, blockID string) (int, error) {
	var refCount int
	err := s.db.Session().Query(`
		SELECT ref_count FROM blocks WHERE org_id = ? AND block_id = ?
	`, orgID, blockID).Scan(&refCount)
	return refCount, err
}

func (s *CassandraStore) DeleteBlock(orgID uuid.UUID, blockID string) error {
	return s.db.Session().Query(`
		DELETE FROM blocks WHERE org_id = ? AND block_id = ?
	`, orgID, blockID).Exec()
}

func (s *CassandraStore) DecrementBlockRefCount(orgID uuid.UUID, blockID string) error {
	return s.db.Session().Query(`
		UPDATE blocks SET ref_count = ref_count - 1, last_accessed = ?
		WHERE org_id = ? AND block_id = ?
	`, time.Now(), orgID, blockID).Exec()
}

func (s *CassandraStore) ListBlockMappings(orgID uuid.UUID) ([]BlockMapping, error) {
	iter := s.db.Session().Query(`
		SELECT external_id, internal_id FROM block_id_mappings WHERE org_id = ?
	`, orgID).Iter()

	var mappings []BlockMapping
	var extID, intID string
	for iter.Scan(&extID, &intID) {
		mappings = append(mappings, BlockMapping{ExternalID: extID, InternalID: intID})
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return mappings, nil
}

func (s *CassandraStore) DeleteBlockMapping(orgID uuid.UUID, externalID string) error {
	return s.db.Session().Query(`
		DELETE FROM block_id_mappings WHERE org_id = ? AND external_id = ?
	`, orgID, externalID).Exec()
}

func (s *CassandraStore) DeleteCommit(libraryID uuid.UUID, commitID string) error {
	return s.db.Session().Query(`
		DELETE FROM commits WHERE library_id = ? AND commit_id = ?
	`, libraryID, commitID).Exec()
}

func (s *CassandraStore) GetFSObject(libraryID uuid.UUID, fsID string) (FSObjectInfo, error) {
	var info FSObjectInfo
	var blockIDs []string
	err := s.db.Session().Query(`
		SELECT fs_id, obj_type, block_ids FROM fs_objects WHERE library_id = ? AND fs_id = ?
	`, libraryID, fsID).Scan(&info.FSID, &info.ObjType, &blockIDs)
	if err != nil {
		return FSObjectInfo{}, err
	}
	info.BlockIDs = blockIDs
	return info, nil
}

func (s *CassandraStore) DeleteFSObject(libraryID uuid.UUID, fsID string) error {
	return s.db.Session().Query(`
		DELETE FROM fs_objects WHERE library_id = ? AND fs_id = ?
	`, libraryID, fsID).Exec()
}

func (s *CassandraStore) GetLibraryStorageClass(orgID, libraryID uuid.UUID) (string, error) {
	var storageClass string
	err := s.db.Session().Query(`
		SELECT storage_class FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, libraryID).Scan(&storageClass)
	return storageClass, err
}

func (s *CassandraStore) ListCommitsForLibrary(libraryID uuid.UUID) ([]CommitInfo, error) {
	iter := s.db.Session().Query(`
		SELECT commit_id, root_fs_id FROM commits WHERE library_id = ?
	`, libraryID).Iter()

	var commits []CommitInfo
	var commitID, rootFSID string
	for iter.Scan(&commitID, &rootFSID) {
		commits = append(commits, CommitInfo{CommitID: commitID, RootFSID: rootFSID})
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return commits, nil
}

func (s *CassandraStore) ListFSObjectsForLibrary(libraryID uuid.UUID) ([]FSObjectInfo, error) {
	iter := s.db.Session().Query(`
		SELECT fs_id, obj_type, block_ids FROM fs_objects WHERE library_id = ?
	`, libraryID).Iter()

	var objects []FSObjectInfo
	var fsID, objType string
	var blockIDs []string
	for iter.Scan(&fsID, &objType, &blockIDs) {
		objects = append(objects, FSObjectInfo{FSID: fsID, ObjType: objType, BlockIDs: blockIDs})
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return objects, nil
}

func (s *CassandraStore) ListOrganizations() ([]uuid.UUID, error) {
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

func (s *CassandraStore) ListBlocksForOrg(orgID uuid.UUID) ([]BlockInfo, error) {
	iter := s.db.Session().Query(`
		SELECT block_id, storage_class, ref_count FROM blocks WHERE org_id = ?
	`, orgID).Iter()

	var blocks []BlockInfo
	var blockID, storageClass string
	var refCount int
	for iter.Scan(&blockID, &storageClass, &refCount) {
		blocks = append(blocks, BlockInfo{BlockID: blockID, StorageClass: storageClass, RefCount: refCount})
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return blocks, nil
}

func (s *CassandraStore) ListShareLinks() ([]ShareLinkInfo, error) {
	iter := s.db.Session().Query(`
		SELECT share_token, org_id, expires_at FROM share_links
	`).Iter()

	var links []ShareLinkInfo
	var shareToken string
	var orgID uuid.UUID
	var expiresAt time.Time
	for iter.Scan(&shareToken, &orgID, &expiresAt) {
		links = append(links, ShareLinkInfo{ShareToken: shareToken, OrgID: orgID, ExpiresAt: expiresAt})
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return links, nil
}

func (s *CassandraStore) ListDistinctCommitLibraries() ([]uuid.UUID, error) {
	iter := s.db.Session().Query(`SELECT DISTINCT library_id FROM commits`).Iter()
	var ids []uuid.UUID
	var id uuid.UUID
	for iter.Scan(&id) {
		ids = append(ids, id)
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *CassandraStore) ListDistinctFSObjectLibraries() ([]uuid.UUID, error) {
	iter := s.db.Session().Query(`SELECT DISTINCT library_id FROM fs_objects`).Iter()
	var ids []uuid.UUID
	var id uuid.UUID
	for iter.Scan(&id) {
		ids = append(ids, id)
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *CassandraStore) LibraryExists(libraryID uuid.UUID) (bool, error) {
	var existingLibID uuid.UUID
	err := s.db.Session().Query(`
		SELECT library_id FROM libraries_by_id WHERE library_id = ?
	`, libraryID).Scan(&existingLibID)
	if err != nil {
		return false, nil // Not found
	}
	return true, nil
}

func (s *CassandraStore) FindOrgForLibrary(libraryID uuid.UUID) (uuid.UUID, error) {
	var orgID uuid.UUID
	err := s.db.Session().Query(`
		SELECT org_id FROM libraries_by_id WHERE library_id = ?
	`, libraryID).Scan(&orgID)
	if err != nil {
		return uuid.Nil, err
	}
	return orgID, nil
}

func (s *CassandraStore) ListCommitIDsForLibrary(libraryID uuid.UUID) ([]string, error) {
	iter := s.db.Session().Query(`
		SELECT commit_id FROM commits WHERE library_id = ?
	`, libraryID).Iter()
	var ids []string
	var id string
	for iter.Scan(&id) {
		ids = append(ids, id)
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *CassandraStore) ListFSObjectIDsForLibrary(libraryID uuid.UUID) ([]string, error) {
	iter := s.db.Session().Query(`
		SELECT fs_id FROM fs_objects WHERE library_id = ?
	`, libraryID).Iter()
	var ids []string
	var id string
	for iter.Scan(&id) {
		ids = append(ids, id)
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *CassandraStore) ListLibrariesWithVersionTTL() ([]LibraryTTLInfo, error) {
	iter := s.db.Session().Query(`
		SELECT org_id, library_id, head_commit_id, version_ttl_days FROM libraries
	`).Iter()

	var results []LibraryTTLInfo
	var orgID, libraryID uuid.UUID
	var headCommitID string
	var versionTTLDays int
	for iter.Scan(&orgID, &libraryID, &headCommitID, &versionTTLDays) {
		if versionTTLDays > 0 {
			results = append(results, LibraryTTLInfo{
				OrgID:          orgID,
				LibraryID:      libraryID,
				HeadCommitID:   headCommitID,
				VersionTTLDays: versionTTLDays,
			})
		}
	}
	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("failed to list libraries with version TTL: %w", err)
	}
	return results, nil
}

func (s *CassandraStore) ListCommitsWithTimestamps(libraryID uuid.UUID) ([]CommitWithTimestamp, error) {
	iter := s.db.Session().Query(`
		SELECT commit_id, parent_id, created_at FROM commits WHERE library_id = ?
	`, libraryID).Iter()

	var commits []CommitWithTimestamp
	var commitID, parentID string
	var createdAt time.Time
	for iter.Scan(&commitID, &parentID, &createdAt) {
		commits = append(commits, CommitWithTimestamp{
			CommitID:  commitID,
			ParentID:  parentID,
			CreatedAt: createdAt,
		})
	}
	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("failed to list commits with timestamps: %w", err)
	}
	return commits, nil
}

func (s *CassandraStore) DeleteShareLink(shareToken string) error {
	return s.db.Session().Query(`
		DELETE FROM share_links WHERE share_token = ?
	`, shareToken).Exec()
}

// StorageManagerAdapter wraps a *storage.Manager to implement StorageProvider.
type StorageManagerAdapter struct {
	manager *storage.Manager
}

// NewStorageManagerAdapter wraps a *storage.Manager as a StorageProvider.
func NewStorageManagerAdapter(manager *storage.Manager) *StorageManagerAdapter {
	return &StorageManagerAdapter{manager: manager}
}

func (a *StorageManagerAdapter) GetBlockStore(storageClass string) (BlockStoreDeleter, error) {
	bs, err := a.manager.GetBlockStore(storageClass)
	if err != nil {
		return nil, err
	}
	return bs, nil
}
