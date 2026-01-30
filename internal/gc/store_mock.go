package gc

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MockStore is an in-memory implementation of GCStore for testing.
type MockStore struct {
	mu sync.RWMutex

	// gc_queue items keyed by orgID
	queue map[uuid.UUID][]QueueItem

	// blocks keyed by "orgID:blockID"
	blocks map[string]*mockBlock

	// block_id_mappings keyed by "orgID:externalID"
	mappings map[string]string // externalID -> internalID

	// commits keyed by "libraryID:commitID"
	commits map[string]*mockCommit

	// fs_objects keyed by "libraryID:fsID"
	fsObjects map[string]*mockFSObject

	// libraries keyed by libraryID
	libraries map[uuid.UUID]*mockLibrary

	// organizations
	organizations []uuid.UUID

	// share_links keyed by shareToken
	shareLinks map[string]*mockShareLink
}

type mockBlock struct {
	OrgID        uuid.UUID
	BlockID      string
	StorageClass string
	RefCount     int
}

type mockCommit struct {
	LibraryID uuid.UUID
	CommitID  string
	RootFSID  string
}

type mockFSObject struct {
	LibraryID uuid.UUID
	FSID      string
	ObjType   string
	BlockIDs  []string
}

type mockLibrary struct {
	OrgID        uuid.UUID
	LibraryID    uuid.UUID
	StorageClass string
}

type mockShareLink struct {
	ShareToken string
	OrgID      uuid.UUID
	ExpiresAt  time.Time
}

// NewMockStore creates a new in-memory mock store.
func NewMockStore() *MockStore {
	return &MockStore{
		queue:         make(map[uuid.UUID][]QueueItem),
		blocks:        make(map[string]*mockBlock),
		mappings:      make(map[string]string),
		commits:       make(map[string]*mockCommit),
		fsObjects:     make(map[string]*mockFSObject),
		libraries:     make(map[uuid.UUID]*mockLibrary),
		shareLinks:    make(map[string]*mockShareLink),
		organizations: nil,
	}
}

// --- Test helpers for seeding data ---

func (m *MockStore) AddOrganization(orgID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.organizations = append(m.organizations, orgID)
}

func (m *MockStore) AddBlock(orgID uuid.UUID, blockID, storageClass string, refCount int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fmt.Sprintf("%s:%s", orgID, blockID)
	m.blocks[key] = &mockBlock{
		OrgID:        orgID,
		BlockID:      blockID,
		StorageClass: storageClass,
		RefCount:     refCount,
	}
}

func (m *MockStore) AddBlockMapping(orgID uuid.UUID, externalID, internalID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fmt.Sprintf("%s:%s", orgID, externalID)
	m.mappings[key] = internalID
}

func (m *MockStore) AddCommit(libraryID uuid.UUID, commitID, rootFSID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fmt.Sprintf("%s:%s", libraryID, commitID)
	m.commits[key] = &mockCommit{
		LibraryID: libraryID,
		CommitID:  commitID,
		RootFSID:  rootFSID,
	}
}

func (m *MockStore) AddFSObject(libraryID uuid.UUID, fsID, objType string, blockIDs []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fmt.Sprintf("%s:%s", libraryID, fsID)
	m.fsObjects[key] = &mockFSObject{
		LibraryID: libraryID,
		FSID:      fsID,
		ObjType:   objType,
		BlockIDs:  blockIDs,
	}
}

func (m *MockStore) AddLibrary(orgID, libraryID uuid.UUID, storageClass string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.libraries[libraryID] = &mockLibrary{
		OrgID:        orgID,
		LibraryID:    libraryID,
		StorageClass: storageClass,
	}
}

func (m *MockStore) AddShareLink(shareToken string, orgID uuid.UUID, expiresAt time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shareLinks[shareToken] = &mockShareLink{
		ShareToken: shareToken,
		OrgID:      orgID,
		ExpiresAt:  expiresAt,
	}
}

// GetBlock returns a block for test assertions.
func (m *MockStore) GetBlock(orgID uuid.UUID, blockID string) *mockBlock {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.blocks[fmt.Sprintf("%s:%s", orgID, blockID)]
}

// GetCommit returns a commit for test assertions.
func (m *MockStore) GetCommit(libraryID uuid.UUID, commitID string) *mockCommit {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.commits[fmt.Sprintf("%s:%s", libraryID, commitID)]
}

// GetFSObj returns an fs_object for test assertions.
func (m *MockStore) GetFSObj(libraryID uuid.UUID, fsID string) *mockFSObject {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.fsObjects[fmt.Sprintf("%s:%s", libraryID, fsID)]
}

// QueueLen returns the total number of items in the queue.
func (m *MockStore) QueueLen() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total := 0
	for _, items := range m.queue {
		total += len(items)
	}
	return total
}

// QueueItems returns all queue items for an org.
func (m *MockStore) QueueItems(orgID uuid.UUID) []QueueItem {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]QueueItem{}, m.queue[orgID]...)
}

// --- GCStore interface implementation ---

func (m *MockStore) EnqueueItem(orgID uuid.UUID, queuedAt time.Time, itemType ItemType, itemID string, libraryID uuid.UUID, storageClass string, retryCount int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item := QueueItem{
		OrgID:        orgID,
		QueuedAt:     queuedAt,
		ItemType:     itemType,
		ItemID:       itemID,
		LibraryID:    libraryID,
		StorageClass: storageClass,
		RetryCount:   retryCount,
	}
	m.queue[orgID] = append(m.queue[orgID], item)
	return nil
}

func (m *MockStore) DequeueBatch(orgID uuid.UUID, batchSize int, cutoff time.Time) ([]QueueItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	items := m.queue[orgID]
	// Sort by QueuedAt ASC
	sort.Slice(items, func(i, j int) bool {
		return items[i].QueuedAt.Before(items[j].QueuedAt)
	})

	var result []QueueItem
	for _, item := range items {
		if item.QueuedAt.Before(cutoff) {
			result = append(result, item)
			if len(result) >= batchSize {
				break
			}
		}
	}
	return result, nil
}

func (m *MockStore) CompleteItem(orgID uuid.UUID, queuedAt time.Time, itemType ItemType, itemID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	items := m.queue[orgID]
	for i, item := range items {
		if item.QueuedAt.Equal(queuedAt) && item.ItemType == itemType && item.ItemID == itemID {
			m.queue[orgID] = append(items[:i], items[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *MockStore) UpdateRetryCount(orgID uuid.UUID, queuedAt time.Time, itemType ItemType, itemID string, retryCount int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, item := range m.queue[orgID] {
		if item.QueuedAt.Equal(queuedAt) && item.ItemType == itemType && item.ItemID == itemID {
			m.queue[orgID][i].RetryCount = retryCount
			return nil
		}
	}
	return nil
}

func (m *MockStore) GetQueueSize(orgID uuid.UUID) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.queue[orgID]), nil
}

func (m *MockStore) GetTotalQueueSize() (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total := 0
	for _, items := range m.queue {
		total += len(items)
	}
	return total, nil
}

func (m *MockStore) ListOrgsWithQueuedItems() ([]uuid.UUID, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var orgs []uuid.UUID
	for orgID, items := range m.queue {
		if len(items) > 0 {
			orgs = append(orgs, orgID)
		}
	}
	return orgs, nil
}

func (m *MockStore) GetBlockRefCount(orgID uuid.UUID, blockID string) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", orgID, blockID)
	b, ok := m.blocks[key]
	if !ok {
		return 0, fmt.Errorf("block not found: %s", blockID)
	}
	return b.RefCount, nil
}

func (m *MockStore) DeleteBlock(orgID uuid.UUID, blockID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("%s:%s", orgID, blockID)
	delete(m.blocks, key)
	return nil
}

func (m *MockStore) DecrementBlockRefCount(orgID uuid.UUID, blockID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("%s:%s", orgID, blockID)
	b, ok := m.blocks[key]
	if !ok {
		return fmt.Errorf("block not found: %s", blockID)
	}
	b.RefCount--
	return nil
}

func (m *MockStore) ListBlockMappings(orgID uuid.UUID) ([]BlockMapping, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	prefix := fmt.Sprintf("%s:", orgID)
	var mappings []BlockMapping
	for key, internalID := range m.mappings {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			extID := key[len(prefix):]
			mappings = append(mappings, BlockMapping{ExternalID: extID, InternalID: internalID})
		}
	}
	return mappings, nil
}

func (m *MockStore) DeleteBlockMapping(orgID uuid.UUID, externalID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("%s:%s", orgID, externalID)
	delete(m.mappings, key)
	return nil
}

func (m *MockStore) DeleteCommit(libraryID uuid.UUID, commitID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("%s:%s", libraryID, commitID)
	delete(m.commits, key)
	return nil
}

func (m *MockStore) GetFSObject(libraryID uuid.UUID, fsID string) (FSObjectInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", libraryID, fsID)
	obj, ok := m.fsObjects[key]
	if !ok {
		return FSObjectInfo{}, fmt.Errorf("fs_object not found: %s", fsID)
	}
	return FSObjectInfo{
		FSID:     obj.FSID,
		ObjType:  obj.ObjType,
		BlockIDs: obj.BlockIDs,
	}, nil
}

func (m *MockStore) DeleteFSObject(libraryID uuid.UUID, fsID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("%s:%s", libraryID, fsID)
	delete(m.fsObjects, key)
	return nil
}

func (m *MockStore) GetLibraryStorageClass(orgID, libraryID uuid.UUID) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	lib, ok := m.libraries[libraryID]
	if !ok {
		return "", fmt.Errorf("library not found: %s", libraryID)
	}
	return lib.StorageClass, nil
}

func (m *MockStore) ListCommitsForLibrary(libraryID uuid.UUID) ([]CommitInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	prefix := fmt.Sprintf("%s:", libraryID)
	var commits []CommitInfo
	for key, c := range m.commits {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			commits = append(commits, CommitInfo{CommitID: c.CommitID, RootFSID: c.RootFSID})
		}
	}
	return commits, nil
}

func (m *MockStore) ListFSObjectsForLibrary(libraryID uuid.UUID) ([]FSObjectInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	prefix := fmt.Sprintf("%s:", libraryID)
	var objects []FSObjectInfo
	for key, obj := range m.fsObjects {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			objects = append(objects, FSObjectInfo{
				FSID:     obj.FSID,
				ObjType:  obj.ObjType,
				BlockIDs: obj.BlockIDs,
			})
		}
	}
	return objects, nil
}

func (m *MockStore) ListOrganizations() ([]uuid.UUID, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]uuid.UUID{}, m.organizations...), nil
}

func (m *MockStore) ListBlocksForOrg(orgID uuid.UUID) ([]BlockInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	prefix := fmt.Sprintf("%s:", orgID)
	var blocks []BlockInfo
	for key, b := range m.blocks {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			blocks = append(blocks, BlockInfo{
				BlockID:      b.BlockID,
				StorageClass: b.StorageClass,
				RefCount:     b.RefCount,
			})
		}
	}
	return blocks, nil
}

func (m *MockStore) ListShareLinks() ([]ShareLinkInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var links []ShareLinkInfo
	for _, sl := range m.shareLinks {
		links = append(links, ShareLinkInfo{
			ShareToken: sl.ShareToken,
			OrgID:      sl.OrgID,
			ExpiresAt:  sl.ExpiresAt,
		})
	}
	return links, nil
}

func (m *MockStore) ListDistinctCommitLibraries() ([]uuid.UUID, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	seen := make(map[uuid.UUID]bool)
	for _, c := range m.commits {
		seen[c.LibraryID] = true
	}

	var result []uuid.UUID
	for id := range seen {
		result = append(result, id)
	}
	return result, nil
}

func (m *MockStore) ListDistinctFSObjectLibraries() ([]uuid.UUID, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	seen := make(map[uuid.UUID]bool)
	for _, obj := range m.fsObjects {
		seen[obj.LibraryID] = true
	}

	var result []uuid.UUID
	for id := range seen {
		result = append(result, id)
	}
	return result, nil
}

func (m *MockStore) LibraryExists(libraryID uuid.UUID) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.libraries[libraryID]
	return ok, nil
}

func (m *MockStore) FindOrgForLibrary(libraryID uuid.UUID) (uuid.UUID, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	lib, ok := m.libraries[libraryID]
	if !ok {
		return uuid.Nil, fmt.Errorf("library not found: %s", libraryID)
	}
	return lib.OrgID, nil
}

func (m *MockStore) ListCommitIDsForLibrary(libraryID uuid.UUID) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	prefix := fmt.Sprintf("%s:", libraryID)
	var ids []string
	for key, c := range m.commits {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			ids = append(ids, c.CommitID)
		}
	}
	return ids, nil
}

func (m *MockStore) ListFSObjectIDsForLibrary(libraryID uuid.UUID) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	prefix := fmt.Sprintf("%s:", libraryID)
	var ids []string
	for key, obj := range m.fsObjects {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			ids = append(ids, obj.FSID)
		}
	}
	return ids, nil
}

// MockStorageProvider implements StorageProvider for testing.
type MockStorageProvider struct {
	mu          sync.Mutex
	DeletedKeys []string
}

func (p *MockStorageProvider) GetBlockStore(storageClass string) (BlockStoreDeleter, error) {
	return &mockBlockDeleter{provider: p}, nil
}

// DeletedBlocks returns the list of block IDs that were deleted.
func (p *MockStorageProvider) DeletedBlocks() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string{}, p.DeletedKeys...)
}

type mockBlockDeleter struct {
	provider *MockStorageProvider
}

func (d *mockBlockDeleter) DeleteBlock(ctx context.Context, blockID string) error {
	d.provider.mu.Lock()
	defer d.provider.mu.Unlock()
	d.provider.DeletedKeys = append(d.provider.DeletedKeys, blockID)
	return nil
}
