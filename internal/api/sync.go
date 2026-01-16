package api

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	"github.com/gin-gonic/gin"
)

// SyncTokenCreator interface for creating sync tokens
type SyncTokenCreator interface {
	CreateDownloadToken(orgID, repoID, path, userID string) (string, error)
}

// SyncHandler handles Seafile sync protocol operations
// These endpoints are used by the Seafile Desktop client for file synchronization
type SyncHandler struct {
	db             *db.DB
	storage        *storage.S3Store    // Legacy single store
	blockStore     *storage.BlockStore // Legacy single block store
	storageManager *storage.Manager    // Multi-backend storage manager
	tokenCreator   SyncTokenCreator    // Token creator for download-info
}

// NewSyncHandler creates a new sync protocol handler
func NewSyncHandler(database *db.DB, s3Store *storage.S3Store, blockStore *storage.BlockStore, storageManager *storage.Manager) *SyncHandler {
	return &SyncHandler{
		db:             database,
		storage:        s3Store,
		blockStore:     blockStore,
		storageManager: storageManager,
	}
}

// SetTokenCreator sets the token creator for download-info endpoint
func (h *SyncHandler) SetTokenCreator(tc SyncTokenCreator) {
	h.tokenCreator = tc
}

// formatSizeSeafile formats bytes in Seafile's format with non-breaking space
// Examples: "0 bytes" (with \xa0), "1.5 KB"
func formatSizeSeafile(bytes int64) string {
	const nbsp = "\u00a0" // Non-breaking space (U+00A0)
	if bytes == 0 {
		return "0" + nbsp + "bytes"
	}
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d"+nbsp+"bytes", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f"+nbsp+"%cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// formatRelativeTimeHTML formats time as Seafile's HTML time tag
// Example: <time datetime="2026-01-16T02:00:27" is="relative-time" title="Fri, 16 Jan 2026 02:00:27 +0000" >4 seconds ago</time>
func formatRelativeTimeHTML(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	var relativeStr string
	if diff < time.Minute {
		seconds := int(diff.Seconds())
		if seconds <= 1 {
			relativeStr = "1 second ago"
		} else {
			relativeStr = fmt.Sprintf("%d seconds ago", seconds)
		}
	} else if diff < time.Hour {
		minutes := int(diff.Minutes())
		if minutes == 1 {
			relativeStr = "1 minute ago"
		} else {
			relativeStr = fmt.Sprintf("%d minutes ago", minutes)
		}
	} else if diff < 24*time.Hour {
		hours := int(diff.Hours())
		if hours == 1 {
			relativeStr = "1 hour ago"
		} else {
			relativeStr = fmt.Sprintf("%d hours ago", hours)
		}
	} else {
		days := int(diff.Hours() / 24)
		if days == 1 {
			relativeStr = "1 day ago"
		} else {
			relativeStr = fmt.Sprintf("%d days ago", days)
		}
	}

	// Format datetime in ISO 8601
	datetime := t.UTC().Format("2006-01-02T15:04:05")
	// Format title in RFC 1123
	title := t.UTC().Format("Mon, 02 Jan 2006 15:04:05 -0700")

	return fmt.Sprintf("<time datetime=\"%s\" is=\"relative-time\" title=\"%s\" >%s</time>",
		datetime, title, relativeStr)
}

// RegisterSyncRoutes registers the sync protocol routes
func (h *SyncHandler) RegisterSyncRoutes(router *gin.Engine, authMiddleware gin.HandlerFunc) {
	// Protocol version endpoint (no auth required)
	router.GET("/seafhttp/protocol-version", h.GetProtocolVersion)

	// Multi-repo head commits endpoint (for checking multiple repos at once)
	router.POST("/seafhttp/repo/head-commits-multi", authMiddleware, h.GetHeadCommitsMulti)

	// Sync protocol routes under /seafhttp/repo/
	repo := router.Group("/seafhttp/repo/:repo_id")
	repo.Use(authMiddleware)
	{
		// Commit operations
		repo.GET("/commit/HEAD", h.GetHeadCommit)
		repo.GET("/commit/:commit_id", h.GetCommit)
		repo.PUT("/commit/:commit_id", h.PutCommit)

		// Block operations
		repo.GET("/block/:block_id", h.GetBlock)
		repo.PUT("/block/:block_id", h.PutBlock)
		repo.POST("/check-blocks", h.CheckBlocks)
		repo.POST("/check-blocks/", h.CheckBlocks)

		// Filesystem operations
		repo.GET("/fs-id-list", h.GetFSIDList)
		repo.GET("/fs-id-list/", h.GetFSIDList)
		repo.GET("/fs/:fs_id", h.GetFSObject)
		repo.POST("/pack-fs", h.PackFS)
		repo.POST("/pack-fs/", h.PackFS)
		repo.POST("/recv-fs", h.RecvFS)
		repo.POST("/recv-fs/", h.RecvFS)
		repo.POST("/check-fs", h.CheckFS)
		repo.POST("/check-fs/", h.CheckFS)

		// Permission and quota
		repo.GET("/permission-check", h.PermissionCheck)
		repo.GET("/permission-check/", h.PermissionCheck)
		repo.GET("/quota-check", h.QuotaCheck)
		repo.GET("/quota-check/", h.QuotaCheck)

		// Update branch (for committing changes)
		repo.POST("/update-branch", h.UpdateBranch)
		repo.POST("/update-branch/", h.UpdateBranch)

		// Download info (for encrypted libraries)
		repo.GET("/download-info", h.GetDownloadInfo)
		repo.GET("/download-info/", h.GetDownloadInfo)
	}
}

// GetProtocolVersion returns the sync protocol version
// GET /seafhttp/protocol-version
func (h *SyncHandler) GetProtocolVersion(c *gin.Context) {
	// Seafile protocol version 2 is the current version used by desktop clients
	c.JSON(http.StatusOK, gin.H{
		"version": 2,
	})
}

// Commit represents a Seafile commit object
type Commit struct {
	CommitID       string  `json:"commit_id"`
	RepoID         string  `json:"repo_id"`
	RootID         string  `json:"root_id"`                    // Root FS object ID
	ParentID       *string `json:"parent_id"`                  // Parent commit ID (null for first commit)
	SecondParentID *string `json:"second_parent_id"`           // For merge commits (null if none)
	Description    string  `json:"description"`
	Creator        string  `json:"creator"`
	CreatorName    string  `json:"creator_name"`
	Ctime          int64   `json:"ctime"`                      // Creation time (Unix timestamp)
	Version        int     `json:"version"`                    // Commit version (currently 1)
	RepoName       string  `json:"repo_name,omitempty"`        // Repository name
	RepoDesc       string  `json:"repo_desc"`                  // Repository description (always included, even when empty)
	RepoCategory   *string `json:"repo_category"`              // Repository category (null)
	NoLocalHistory int     `json:"no_local_history,omitempty"` // 1 = no local history (only if set)
	Encrypted      string  `json:"encrypted,omitempty"`        // "true" as string, not bool (Seafile compat)
	EncVersion     int     `json:"enc_version,omitempty"`
	Magic          string  `json:"magic,omitempty"`
	Key            string  `json:"key,omitempty"`              // Seafile uses "key" not "random_key" in commit
}

// FSObject represents a Seafile filesystem object (file or directory)
type FSObject struct {
	Type     int       `json:"type"`              // 1 = file, 3 = directory
	ID       string    `json:"id"`                // SHA-1 hash of contents
	Name     string    `json:"name,omitempty"`
	Mode     int       `json:"mode,omitempty"`    // Unix file mode
	Mtime    int64     `json:"mtime,omitempty"`   // Modification time
	Size     int64     `json:"size,omitempty"`    // File size
	BlockIDs []string  `json:"block_ids,omitempty"` // Block IDs for files
	Entries  *[]FSEntry `json:"dirents,omitempty"`  // Directory entries (pointer to distinguish nil from empty)
}

// FSEntry represents a directory entry
// CRITICAL: Field order MUST be alphabetical to match Seafile JSON format.
// Seafile uses alphabetical key ordering in JSON which affects fs_id hash computation.
type FSEntry struct {
	ID       string `json:"id"`       // FS object ID
	Mode     int    `json:"mode"`     // Unix file mode (33188 = regular file, 16384 = directory)
	Modifier string `json:"modifier,omitempty"`
	Mtime    int64  `json:"mtime"`
	Name     string `json:"name"`
	Size     int64  `json:"size,omitempty"`
}

// CorrectedFSObject holds the computed fs_id and properly-formed JSON for an fs_object
type CorrectedFSObject struct {
	ComputedFSID  string // SHA-1 of properly ordered JSON
	StoredFSID    string // Original fs_id from database
	CorrectedJSON []byte // JSON with alphabetical keys and corrected child ids
}

// computeCorrectedObject recursively computes the correct fs_id for an fs_object
// It handles directories by first computing children's correct fs_ids and using those in dirents
// Returns nil if object not found
func (h *SyncHandler) computeCorrectedObject(repoID, storedFSID string, cache map[string]*CorrectedFSObject) *CorrectedFSObject {
	// Check cache first
	if cached, ok := cache[storedFSID]; ok {
		return cached
	}

	// Query the fs_object
	var fsType string
	var size int64
	var entriesJSON string
	var blockIDs []string
	err := h.db.Session().Query(`
		SELECT obj_type, size_bytes, dir_entries, block_ids FROM fs_objects WHERE library_id = ? AND fs_id = ?
	`, repoID, storedFSID).Scan(&fsType, &size, &entriesJSON, &blockIDs)

	if err != nil {
		return nil
	}

	var jsonObj map[string]interface{}

	if fsType == "dir" {
		// Parse entries and recursively compute children's correct fs_ids
		var dirents []map[string]interface{}
		if entriesJSON != "" && entriesJSON != "[]" {
			var entries []FSEntry
			if err := json.Unmarshal([]byte(entriesJSON), &entries); err != nil {
				return nil
			}
			for _, entry := range entries {
				// Recursively compute child's correct fs_id
				childCorrect := h.computeCorrectedObject(repoID, entry.ID, cache)
				childID := entry.ID // Default to stored if child not found
				if childCorrect != nil {
					childID = childCorrect.ComputedFSID
				}

				dirent := map[string]interface{}{
					"id":    childID, // Use COMPUTED child id
					"mode":  entry.Mode,
					"mtime": entry.Mtime,
					"name":  entry.Name,
				}
				if entry.Modifier != "" {
					dirent["modifier"] = entry.Modifier
				}
				if entry.Size > 0 {
					dirent["size"] = entry.Size
				}
				dirents = append(dirents, dirent)
			}
		} else {
			dirents = []map[string]interface{}{}
		}
		jsonObj = map[string]interface{}{
			"dirents": dirents,
			"type":    3,
			"version": 1,
		}
	} else {
		// File: no children to fix
		jsonObj = map[string]interface{}{
			"block_ids": blockIDs,
			"size":      size,
			"type":      1,
			"version":   1,
		}
	}

	// Serialize and compute hash
	jsonBytes, err := json.Marshal(jsonObj)
	if err != nil {
		return nil
	}
	computedHash := sha1.Sum(jsonBytes)
	computedFSID := hex.EncodeToString(computedHash[:])

	result := &CorrectedFSObject{
		ComputedFSID:  computedFSID,
		StoredFSID:    storedFSID,
		CorrectedJSON: jsonBytes,
	}

	// Cache result
	cache[storedFSID] = result

	return result
}

// buildFSIDMapping builds a complete mapping of computed→stored fs_ids for a repo tree
// Starting from a root stored fs_id, recursively computes all correct fs_ids
func (h *SyncHandler) buildFSIDMapping(repoID, rootStoredFSID string) (computedToStored map[string]string, storedToCorrected map[string]*CorrectedFSObject) {
	computedToStored = make(map[string]string)
	storedToCorrected = make(map[string]*CorrectedFSObject)

	// Recursively compute all objects starting from root
	h.collectCorrectedObjects(repoID, rootStoredFSID, storedToCorrected)

	// Build the reverse mapping
	for storedID, corrected := range storedToCorrected {
		computedToStored[corrected.ComputedFSID] = storedID
	}

	return
}

// collectCorrectedObjects recursively collects all corrected fs_objects
func (h *SyncHandler) collectCorrectedObjects(repoID, storedFSID string, cache map[string]*CorrectedFSObject) {
	if storedFSID == "" || len(storedFSID) != 40 {
		return
	}
	if _, ok := cache[storedFSID]; ok {
		return // Already processed
	}

	// Compute this object (will recurse into children)
	h.computeCorrectedObject(repoID, storedFSID, cache)
}

// GetHeadCommit returns the HEAD commit for a repository
// GET /seafhttp/repo/:repo_id/commit/HEAD
func (h *SyncHandler) GetHeadCommit(c *gin.Context) {
	repoID := c.Param("repo_id")
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	// Check if database is available
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not available"})
		return
	}

	// Get head commit from database
	var headCommitID string
	err := h.db.Session().Query(`
		SELECT head_commit_id FROM libraries
		WHERE org_id = ? AND library_id = ?
	`, orgID, repoID).Scan(&headCommitID)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "library not found"})
		return
	}

	// If no head commit exists, create an initial commit
	if headCommitID == "" {
		headCommitID, err = h.createInitialCommit(repoID, orgID, userID)
		if err != nil {
			// Log error but return empty - client can handle this
			c.JSON(http.StatusOK, gin.H{"is_corrupted": 0, "head_commit_id": ""})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"is_corrupted":   0, // Seafile uses integer 0, not boolean false
		"head_commit_id": headCommitID,
	})
}

// createInitialCommit creates the first commit for an empty repository
func (h *SyncHandler) createInitialCommit(repoID, orgID, userID string) (string, error) {
	now := time.Now()

	// Create empty root directory FS object
	// The ID is a hash - for empty dir, use a deterministic ID
	rootID := fmt.Sprintf("%040x", 0) // 40 zeros = empty root

	// Store the empty root FS object
	err := h.db.Session().Query(`
		INSERT INTO fs_objects (library_id, fs_id, obj_type, obj_name, dir_entries, size_bytes, mtime)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, repoID, rootID, "dir", "", "[]", 0, now.Unix()).Exec()
	if err != nil {
		return "", fmt.Errorf("failed to create root fs object: %w", err)
	}

	// Create initial commit
	// Commit ID is a hash of the content - use deterministic ID for initial (40 chars like SHA-1)
	commitID := sha1Hex(fmt.Sprintf("%s-%s-%d", repoID, rootID, now.Unix()))

	err = h.db.Session().Query(`
		INSERT INTO commits (library_id, commit_id, parent_id, root_fs_id, creator_id, description, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, repoID, commitID, "", rootID, userID, "Initial commit", now).Exec()
	if err != nil {
		return "", fmt.Errorf("failed to create initial commit: %w", err)
	}

	// Update library's head_commit_id
	err = h.db.Session().Query(`
		UPDATE libraries SET head_commit_id = ?, root_commit_id = ?, updated_at = ?
		WHERE org_id = ? AND library_id = ?
	`, commitID, commitID, now, orgID, repoID).Exec()
	if err != nil {
		return "", fmt.Errorf("failed to update library head: %w", err)
	}

	return commitID, nil
}

// sha1Hex returns the SHA1 hash of a string as hex (40 chars, Seafile compatible)
func sha1Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	// Return only first 40 chars to match Seafile's SHA-1 format
	return hex.EncodeToString(h[:20])
}

// GetCommit returns a specific commit object
// GET /seafhttp/repo/:repo_id/commit/:commit_id
func (h *SyncHandler) GetCommit(c *gin.Context) {
	repoID := c.Param("repo_id")
	commitID := c.Param("commit_id")
	orgID := c.GetString("org_id")

	// Query commit from database
	var commit Commit
	var parentID, rootID, description, creator string
	var ctime time.Time

	err := h.db.Session().Query(`
		SELECT commit_id, parent_id, root_fs_id, description, creator_id, created_at
		FROM commits WHERE library_id = ? AND commit_id = ?
	`, repoID, commitID).Scan(
		&commit.CommitID, &parentID, &rootID, &description, &creator, &ctime,
	)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "commit not found"})
		return
	}

	// Get library info for repo_name, repo_desc, and encryption info
	var repoName, repoDesc string
	var encrypted bool
	var encVersion int
	var magic, randomKey string
	h.db.Session().Query(`
		SELECT name, description, encrypted, enc_version, magic, random_key
		FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, repoID).Scan(&repoName, &repoDesc, &encrypted, &encVersion, &magic, &randomKey)

	commit.RepoID = repoID

	// Compute the corrected root_id (with proper JSON key ordering)
	// This ensures the commit's root_id matches what fs-id-list and pack-fs return
	if rootID != "" && rootID != strings.Repeat("0", 40) {
		cache := make(map[string]*CorrectedFSObject)
		corrected := h.computeCorrectedObject(repoID, rootID, cache)
		if corrected != nil {
			commit.RootID = corrected.ComputedFSID
		} else {
			commit.RootID = rootID // Fallback to stored if computation fails
		}
	} else {
		commit.RootID = rootID
	}

	commit.Description = description
	// Seafile uses 40 zeros for creator ID format
	commit.Creator = strings.Repeat("0", 40)
	commit.CreatorName = creator + "@sesamefs.local"
	commit.Ctime = ctime.Unix()
	commit.Version = 1 // Seafile commit format version 1
	commit.RepoName = repoName
	commit.RepoDesc = "" // Seafile returns empty string in commit objects

	// Add encryption fields if library is encrypted
	if encrypted {
		commit.Encrypted = "true" // Seafile uses string "true" not boolean
		// Return enc_version 2 for Seafile client compatibility (we store 12 for dual-mode)
		commit.EncVersion = 2
		commit.Magic = magic
		commit.Key = randomKey // Seafile uses "key" in commit response
		// NOTE: no_local_history is NOT included by stock Seafile server
	}

	// Set pointer fields - null if empty, pointer to value otherwise
	if parentID == "" {
		commit.ParentID = nil
	} else {
		commit.ParentID = &parentID
	}
	commit.SecondParentID = nil // Always null for now

	// CRITICAL: Seafile returns empty string for repo_category, not null
	emptyCategory := ""
	commit.RepoCategory = &emptyCategory

	// Return commit as JSON
	c.JSON(http.StatusOK, commit)
}

// PutCommit stores a new commit object or updates the HEAD pointer
// PUT /seafhttp/repo/:repo_id/commit/:commit_id
// PUT /seafhttp/repo/:repo_id/commit/HEAD?head=<commit_id> (update HEAD pointer)
func (h *SyncHandler) PutCommit(c *gin.Context) {
	repoID := c.Param("repo_id")
	commitID := c.Param("commit_id")
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	// Special case: PUT /commit/HEAD?head=<commit_id> updates the HEAD pointer
	if commitID == "HEAD" {
		headCommitID := c.Query("head")
		if headCommitID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing head parameter"})
			return
		}

		log.Printf("PutCommit HEAD: updating repo %s head to %s", repoID, headCommitID)

		// Update library head
		now := time.Now()
		err := h.db.Session().Query(`
			UPDATE libraries SET head_commit_id = ?, updated_at = ?
			WHERE org_id = ? AND library_id = ?
		`, headCommitID, now, orgID, repoID).Exec()

		if err != nil {
			log.Printf("PutCommit HEAD: failed to update head: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update head"})
			return
		}

		c.Status(http.StatusOK)
		return
	}

	// Read commit data from body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	var commit Commit
	if err := json.Unmarshal(body, &commit); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid commit format"})
		return
	}

	// Verify commit ID matches
	if commit.CommitID != "" && commit.CommitID != commitID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "commit ID mismatch"})
		return
	}

	// Store commit in database
	now := time.Now()
	err = h.db.Session().Query(`
		INSERT INTO commits (library_id, commit_id, parent_id, root_fs_id, creator_id, description, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, repoID, commitID, commit.ParentID, commit.RootID, userID, commit.Description, now).Exec()

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store commit"})
		return
	}

	// Update library head
	err = h.db.Session().Query(`
		UPDATE libraries SET head_commit_id = ?, updated_at = ?
		WHERE org_id = ? AND library_id = ?
	`, commitID, now, orgID, repoID).Exec()

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update head"})
		return
	}

	c.Status(http.StatusOK)
}

// GetBlock retrieves a block by ID
// GET /seafhttp/repo/:repo_id/block/:block_id
// Supports both SHA-1 (40 chars, Seafile legacy) and SHA-256 (64 chars, new clients)
func (h *SyncHandler) GetBlock(c *gin.Context) {
	externalID := c.Param("block_id")
	orgID := c.GetString("org_id")

	// Determine internal ID based on external ID length
	var internalID string
	isLegacySHA1 := len(externalID) == 40

	if h.db != nil && isLegacySHA1 {
		// SHA-1: look up internal SHA-256 ID from mapping
		err := h.db.Session().Query(`
			SELECT internal_id FROM block_id_mappings WHERE org_id = ? AND external_id = ?
		`, orgID, externalID).Scan(&internalID)

		if err != nil || internalID == "" {
			// Fallback: maybe this is an old block stored with SHA-1 directly
			// Try using the external ID as the internal ID
			internalID = externalID
			log.Printf("GetBlock: no mapping found for %s, using as-is\n", externalID)
		} else {
			log.Printf("GetBlock: resolved %s → %s\n", externalID, internalID)
		}
	} else {
		// SHA-256 or no DB: use external ID directly
		internalID = externalID
	}

	// Look up storage class from database
	var storageClass string
	if h.db != nil {
		err := h.db.Session().Query(`
			SELECT storage_class FROM blocks WHERE org_id = ? AND block_id = ?
		`, orgID, internalID).Scan(&storageClass)

		if err != nil || storageClass == "" {
			storageClass = "hot"
		}
	} else {
		storageClass = "hot"
	}

	// Get the appropriate BlockStore using StorageManager
	var blockStore *storage.BlockStore
	var err error

	if h.storageManager != nil {
		blockStore, err = h.storageManager.GetBlockStore(storageClass)
		if err != nil {
			log.Printf("GetBlock: storage class %s not found: %v, trying default\n", storageClass, err)
			blockStore, _, err = h.storageManager.GetHealthyBlockStore(h.storageManager.ResolveStorageClass("", "", "hot"))
			if err != nil {
				blockStore = h.blockStore
			}
		}
	} else {
		blockStore = h.blockStore
	}

	if blockStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "block storage not available"})
		return
	}

	// Retrieve block from storage using internal ID
	data, err := blockStore.GetBlock(c.Request.Context(), internalID)
	if err != nil {
		log.Printf("GetBlock: block %s (internal: %s) not found in %s: %v\n",
			externalID, internalID, storageClass, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "block not found"})
		return
	}

	// NOTE: For encrypted libraries, blocks are stored encrypted:
	// - Sync protocol: Client encrypts blocks locally before upload, server stores as-is
	// - Web uploads: Server encrypts blocks before storage
	// In both cases, blocks are returned as-is - NO re-encryption needed.
	// The client will decrypt using its locally-derived file key.

	// Update last accessed time (if DB available)
	if h.db != nil {
		_ = h.db.Session().Query(`
			UPDATE blocks SET last_accessed = ? WHERE org_id = ? AND block_id = ?
		`, time.Now(), orgID, internalID).Exec()
	}

	c.Data(http.StatusOK, "application/octet-stream", data)
}


// PutBlock stores a block
// PUT /seafhttp/repo/:repo_id/block/:block_id
// Supports both SHA-1 (40 chars, Seafile legacy) and SHA-256 (64 chars, new clients)
// Internally always stores blocks using SHA-256 for consistency
func (h *SyncHandler) PutBlock(c *gin.Context) {
	externalID := c.Param("block_id")
	orgID := c.GetString("org_id")
	hashType := c.DefaultQuery("hash_type", "") // Optional: "sha256" for new clients

	log.Printf("PutBlock: externalID=%s, len=%d\n", externalID, len(externalID))

	// Read block data
	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		log.Printf("PutBlock: failed to read body: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read block data"})
		return
	}

	log.Printf("PutBlock: received %d bytes for block %s\n", len(data), externalID)

	// Always compute SHA-256 as the internal storage ID
	sha256Hash := sha256.Sum256(data)
	internalID := hex.EncodeToString(sha256Hash[:])

	// Determine if this is a legacy SHA-1 ID or new SHA-256 ID
	isLegacySHA1 := len(externalID) == 40 && hashType != "sha256"
	isDirectSHA256 := len(externalID) == 64 || hashType == "sha256"

	// Verify hash for SHA-256 clients
	if isDirectSHA256 && externalID != internalID {
		log.Printf("PutBlock: SHA-256 hash mismatch, expected %s got %s\n", externalID, internalID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "block hash mismatch"})
		return
	}

	// Resolve storage class based on request hostname
	hostname := c.Request.Host
	if colonIdx := strings.Index(hostname, ":"); colonIdx > 0 {
		hostname = hostname[:colonIdx] // Strip port
	}

	// Get the appropriate BlockStore using StorageManager with failover
	var blockStore *storage.BlockStore
	var storageClass string

	if h.storageManager != nil {
		preferredClass := h.storageManager.ResolveStorageClass(hostname, "", "hot")
		blockStore, storageClass, err = h.storageManager.GetHealthyBlockStore(preferredClass)
		if err != nil {
			log.Printf("PutBlock: failed to get healthy backend: %v, falling back to legacy\n", err)
			blockStore = h.blockStore
			storageClass = "hot"
		}
	} else {
		blockStore = h.blockStore
		storageClass = "hot"
	}

	if blockStore == nil {
		log.Printf("PutBlock: block storage not available\n")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "block storage not available"})
		return
	}

	log.Printf("PutBlock: storing block external=%s internal=%s in storage class %s\n",
		externalID, internalID, storageClass)

	// Store block using internal SHA-256 ID
	blockData := &storage.BlockData{
		Data: data,
		Hash: internalID, // Always use SHA-256 for storage
	}

	_, err = blockStore.PutBlockData(c.Request.Context(), blockData)
	if err != nil {
		log.Printf("PutBlock: failed to store in backend: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store block"})
		return
	}

	// Store block metadata and mapping (if DB available)
	if h.db != nil {
		now := time.Now()

		// Store block metadata using internal ID
		_ = h.db.Session().Query(`
			INSERT INTO blocks (org_id, block_id, size_bytes, storage_class, ref_count, created_at, last_accessed)
			VALUES (?, ?, ?, ?, 1, ?, ?)
		`, orgID, internalID, len(data), storageClass, now, now).Exec()

		// If legacy SHA-1 client, store mapping external→internal
		if isLegacySHA1 {
			_ = h.db.Session().Query(`
				INSERT INTO block_id_mappings (org_id, external_id, internal_id, created_at)
				VALUES (?, ?, ?, ?)
			`, orgID, externalID, internalID, now).Exec()
			log.Printf("PutBlock: stored mapping %s → %s\n", externalID, internalID)
		}
	}

	c.Status(http.StatusOK)
}

// CheckBlocksRequest represents the request to check which blocks exist
type CheckBlocksRequest struct {
	BlockIDs []string `json:"block_ids"`
}

// CheckBlocks checks which blocks already exist (for deduplication)
// POST /seafhttp/repo/:repo_id/check-blocks
// Supports both SHA-1 (40 chars, Seafile legacy) and SHA-256 (64 chars, new clients)
// Translates SHA-1 external IDs to internal SHA-256 IDs for storage lookup
func (h *SyncHandler) CheckBlocks(c *gin.Context) {
	orgID := c.GetString("org_id")

	// Read block IDs from body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	// Parse the body - can be JSON array or newline-separated
	var externalIDs []string
	bodyStr := strings.TrimSpace(string(body))
	if strings.HasPrefix(bodyStr, "[") {
		// JSON array format
		if err := json.Unmarshal(body, &externalIDs); err != nil {
			log.Printf("check-blocks: failed to parse JSON array: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON array"})
			return
		}
	} else {
		// Newline-separated format
		externalIDs = strings.Split(bodyStr, "\n")
	}

	// Build mapping from external IDs to internal IDs
	// For SHA-1 IDs (40 chars), look up the internal SHA-256 from mapping table
	// For SHA-256 IDs (64 chars), use directly
	externalToInternal := make(map[string]string)
	var internalIDs []string

	for _, extID := range externalIDs {
		if extID == "" {
			continue
		}

		var internalID string
		isLegacySHA1 := len(extID) == 40

		if h.db != nil && isLegacySHA1 {
			// SHA-1: look up internal SHA-256 ID from mapping
			err := h.db.Session().Query(`
				SELECT internal_id FROM block_id_mappings WHERE org_id = ? AND external_id = ?
			`, orgID, extID).Scan(&internalID)

			if err != nil || internalID == "" {
				// No mapping found - this block hasn't been uploaded yet
				// or it's an old block stored with SHA-1 directly
				internalID = extID
			}
		} else {
			// SHA-256 or no DB: use external ID directly
			internalID = extID
		}

		externalToInternal[extID] = internalID
		internalIDs = append(internalIDs, internalID)
	}

	// Resolve storage class based on request hostname
	hostname := c.Request.Host
	if colonIdx := strings.Index(hostname, ":"); colonIdx > 0 {
		hostname = hostname[:colonIdx] // Strip port
	}

	// Get the appropriate BlockStore using StorageManager with failover
	var blockStore *storage.BlockStore

	if h.storageManager != nil {
		preferredClass := h.storageManager.ResolveStorageClass(hostname, "", "hot")
		blockStore, _, err = h.storageManager.GetHealthyBlockStore(preferredClass)
		if err != nil {
			log.Printf("CheckBlocks: failed to get healthy backend: %v, falling back to legacy\n", err)
			blockStore = h.blockStore
		}
	} else {
		blockStore = h.blockStore
	}

	if blockStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "block storage not available"})
		return
	}

	// Check which blocks exist using internal IDs
	existMap, err := blockStore.CheckBlocksParallel(c.Request.Context(), internalIDs, 10)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check blocks"})
		return
	}

	// Return list of missing blocks using external IDs (client expects these)
	// Initialize as empty slice so JSON serializes as [] not null
	needed := make([]string, 0)
	for _, extID := range externalIDs {
		if extID == "" {
			continue
		}
		internalID := externalToInternal[extID]
		if !existMap[internalID] {
			needed = append(needed, extID)
		}
	}

	// Return as JSON array (Seafile format)
	c.JSON(http.StatusOK, needed)
}

// GetFSIDList returns the list of FS object IDs for sync
// GET /seafhttp/repo/:repo_id/fs-id-list
// Must return ALL fs_ids recursively: directories AND files (seafile objects)
func (h *SyncHandler) GetFSIDList(c *gin.Context) {
	repoID := c.Param("repo_id")
	serverHead := c.Query("server-head")
	clientHead := c.Query("client-head")
	dirOnly := c.Query("dir-only") == "1"

	_ = clientHead // Used for incremental sync

	// Get FS object IDs by traversing from server head commit
	// Initialize as empty slice (not nil) so JSON serializes as [] not null
	fsIDs := make([]string, 0)

	if serverHead != "" {
		// Query root FS ID from commit
		var rootFSID string
		err := h.db.Session().Query(`
			SELECT root_fs_id FROM commits WHERE library_id = ? AND commit_id = ?
		`, repoID, serverHead).Scan(&rootFSID)

		if err == nil && rootFSID != "" && rootFSID != strings.Repeat("0", 40) {
			// Recursively collect all fs_ids starting from root
			h.collectFSIDs(repoID, rootFSID, dirOnly, &fsIDs)
		}
	}

	// Return as JSON array (matches stock Seafile server)
	c.JSON(http.StatusOK, fsIDs)
}

// collectFSIDs recursively collects all fs_ids from a directory tree
// CRITICAL: Must return COMPUTED fs_ids (from properly-ordered JSON with corrected child IDs)
// CRITICAL: Must return parent (root) FIRST, then children (breadth-first order)
func (h *SyncHandler) collectFSIDs(repoID, storedFSID string, dirOnly bool, fsIDs *[]string) {
	if storedFSID == "" || len(storedFSID) != 40 {
		return
	}

	// Build a cache of all corrected objects starting from this root
	// This properly handles the recursive dependency where parent fs_ids depend on children's computed fs_ids
	cache := make(map[string]*CorrectedFSObject)
	added := make(map[string]bool) // Track which IDs have been added to avoid duplicates
	h.collectCorrectedObjectsWithFilter(repoID, storedFSID, dirOnly, cache, fsIDs, added)
}

// collectCorrectedObjectsWithFilter recursively collects corrected fs_ids with dir-only filter support
// IMPORTANT: Returns parent (root) FIRST, then children (breadth-first order)
// This matches Seafile server behavior and ensures client can build directory tree in order
func (h *SyncHandler) collectCorrectedObjectsWithFilter(repoID, storedFSID string, dirOnly bool, cache map[string]*CorrectedFSObject, fsIDs *[]string, added map[string]bool) {
	if storedFSID == "" || len(storedFSID) != 40 {
		return
	}

	// Query the object type first
	var fsType string
	var entriesJSON string
	err := h.db.Session().Query(`
		SELECT obj_type, dir_entries FROM fs_objects WHERE library_id = ? AND fs_id = ?
	`, repoID, storedFSID).Scan(&fsType, &entriesJSON)

	if err != nil {
		return
	}

	// Parse entries for directories
	var entries []FSEntry
	if fsType == "dir" && entriesJSON != "" && entriesJSON != "[]" {
		json.Unmarshal([]byte(entriesJSON), &entries)
	}

	// First, recursively compute children so their IDs are in cache (needed for parent's dirent IDs)
	// This doesn't add them to fsIDs yet
	for _, entry := range entries {
		if entry.ID == "" || len(entry.ID) != 40 {
			continue
		}
		isDir := (entry.Mode & 0040000) != 0
		if dirOnly && !isDir {
			continue
		}
		h.computeCorrectedObject(repoID, entry.ID, cache)
	}

	// Now compute this object's correct fs_id (children are already in cache)
	corrected := h.computeCorrectedObject(repoID, storedFSID, cache)
	if corrected != nil && !added[corrected.ComputedFSID] {
		// Add THIS object (parent) FIRST
		*fsIDs = append(*fsIDs, corrected.ComputedFSID)
		added[corrected.ComputedFSID] = true
	}

	// Then add children AFTER parent
	for _, entry := range entries {
		if entry.ID == "" || len(entry.ID) != 40 {
			continue
		}
		isDir := (entry.Mode & 0040000) != 0
		if dirOnly && !isDir {
			continue
		}

		// Add this child's computed ID
		if childCorrected, ok := cache[entry.ID]; ok && !added[childCorrected.ComputedFSID] {
			*fsIDs = append(*fsIDs, childCorrected.ComputedFSID)
			added[childCorrected.ComputedFSID] = true
		}

		// Recursively collect grandchildren
		h.collectCorrectedObjectsWithFilter(repoID, entry.ID, dirOnly, cache, fsIDs, added)
	}
}

// GetFSObject retrieves a filesystem object
// GET /seafhttp/repo/:repo_id/fs/:fs_id
// Returns zlib-compressed JSON in Seafile format:
// - For dirs: {"version": 1, "type": 3, "dirents": [...]}
// - For files: {"version": 1, "type": 1, "block_ids": [...], "size": N}
func (h *SyncHandler) GetFSObject(c *gin.Context) {
	repoID := c.Param("repo_id")
	fsID := c.Param("fs_id")

	// Query FS object from database
	var fsType string
	var name string
	var size int64
	var mtime int64
	var entriesJSON string
	var blockIDs []string

	err := h.db.Session().Query(`
		SELECT obj_type, obj_name, size_bytes, mtime, dir_entries, block_ids
		FROM fs_objects WHERE library_id = ? AND fs_id = ?
	`, repoID, fsID).Scan(&fsType, &name, &size, &mtime, &entriesJSON, &blockIDs)

	if err != nil {
		log.Printf("[GetFSObject] fs_object %s not found in repo %s: %v", fsID, repoID, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "fs object not found"})
		return
	}

	// Build JSON object matching Seafile's exact format
	var jsonObj interface{}

	if fsType == "dir" {
		// Directory format: {"version": 1, "type": 3, "dirents": [...]}
		var dirents []map[string]interface{}
		if entriesJSON != "" && entriesJSON != "[]" {
			if err := json.Unmarshal([]byte(entriesJSON), &dirents); err != nil {
				log.Printf("[GetFSObject] failed to parse dirents for %s: %v", fsID, err)
				dirents = []map[string]interface{}{}
			}
		} else {
			dirents = []map[string]interface{}{}
		}
		jsonObj = map[string]interface{}{
			"version": 1,
			"type":    3, // SEAF_METADATA_TYPE_DIR
			"dirents": dirents,
		}
	} else {
		// File format: {"version": 1, "type": 1, "block_ids": [...], "size": N}
		jsonObj = map[string]interface{}{
			"version":   1,
			"type":      1, // SEAF_METADATA_TYPE_FILE
			"block_ids": blockIDs,
			"size":      size,
		}
	}

	// Serialize to JSON
	jsonBytes, err := json.Marshal(jsonObj)
	if err != nil {
		log.Printf("[GetFSObject] failed to marshal fs_object %s: %v", fsID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to serialize object"})
		return
	}

	// Compress with zlib (Seafile client expects zlib-compressed data)
	var compressed bytes.Buffer
	zlibWriter := zlib.NewWriter(&compressed)
	zlibWriter.Write(jsonBytes)
	zlibWriter.Close()

	log.Printf("[GetFSObject] Returning fs_object %s (type=%s, compressed=%d bytes)", fsID, fsType, compressed.Len())

	c.Data(http.StatusOK, "application/octet-stream", compressed.Bytes())
}

// PackFS packs multiple FS objects into a single response
// POST /seafhttp/repo/:repo_id/pack-fs
// Returns binary packed format that Seafile client expects:
// For each object: 40-byte hex ID + object size (4 bytes BE) + zlib-compressed JSON
// NOTE: Seafile server stores fs objects compressed, so pack-fs sends compressed data.
// Client stores as-is and decompresses when reading.
// CRITICAL: Client sends COMPUTED fs_ids (from fs-id-list), we must map them to stored IDs
func (h *SyncHandler) PackFS(c *gin.Context) {
	repoID := c.Param("repo_id")

	// Read FS IDs from body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	// Parse the body - can be JSON array or newline-separated
	var requestedFSIDs []string
	bodyStr := strings.TrimSpace(string(body))
	if strings.HasPrefix(bodyStr, "[") {
		// JSON array format
		if err := json.Unmarshal(body, &requestedFSIDs); err != nil {
			log.Printf("pack-fs: failed to parse JSON array: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON array"})
			return
		}
	} else {
		// Newline-separated format
		requestedFSIDs = strings.Split(bodyStr, "\n")
	}

	// Build the computed→stored mapping for this repo
	// First, get the HEAD commit's root_fs_id
	orgID := c.GetString("org_id")
	var headCommitID string
	err = h.db.Session().Query(`
		SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, repoID).Scan(&headCommitID)
	if err != nil {
		log.Printf("pack-fs: failed to get HEAD commit for repo %s (org %s): %v", repoID, orgID, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "repo not found"})
		return
	}

	var rootFSID string
	err = h.db.Session().Query(`
		SELECT root_fs_id FROM commits WHERE library_id = ? AND commit_id = ?
	`, repoID, headCommitID).Scan(&rootFSID)
	if err != nil || rootFSID == "" || rootFSID == strings.Repeat("0", 40) {
		log.Printf("pack-fs: failed to get root_fs_id for commit %s: %v", headCommitID, err)
		// Try to proceed without mapping - maybe objects exist
		rootFSID = ""
	}

	// Build the mapping by computing all corrected fs_ids
	computedToStored := make(map[string]string)
	storedToCorrected := make(map[string]*CorrectedFSObject)
	if rootFSID != "" {
		computedToStored, storedToCorrected = h.buildFSIDMapping(repoID, rootFSID)
	}

	// Build binary response
	var buf bytes.Buffer

	for _, requestedFSID := range requestedFSIDs {
		if requestedFSID == "" || len(requestedFSID) != 40 {
			continue
		}

		// Try to find the object using the mapping (computed→stored)
		storedFSID, hasMapping := computedToStored[requestedFSID]
		if !hasMapping {
			// Fallback: maybe the requested ID is already a stored ID (for compatibility)
			storedFSID = requestedFSID
		}

		// Check if we have pre-computed corrected data
		corrected, hasCorrected := storedToCorrected[storedFSID]

		var jsonBytes []byte
		var computedFSID string

		if hasCorrected {
			// Use the pre-computed corrected JSON (with proper child IDs)
			jsonBytes = corrected.CorrectedJSON
			computedFSID = corrected.ComputedFSID
		} else {
			// Fallback: query and compute on-the-fly (may have wrong child IDs)
			var fsType string
			var size int64
			var entriesJSON string
			var blockIDs []string

			err := h.db.Session().Query(`
				SELECT obj_type, size_bytes, dir_entries, block_ids
				FROM fs_objects WHERE library_id = ? AND fs_id = ?
			`, repoID, storedFSID).Scan(&fsType, &size, &entriesJSON, &blockIDs)

			if err != nil {
				log.Printf("pack-fs: object %s (stored: %s) not found: %v", requestedFSID, storedFSID, err)
				continue
			}

			// Build JSON
			var jsonObj map[string]interface{}
			if fsType == "dir" {
				var dirents []map[string]interface{}
				if entriesJSON != "" && entriesJSON != "[]" {
					var entries []FSEntry
					if err := json.Unmarshal([]byte(entriesJSON), &entries); err == nil {
						for _, entry := range entries {
							// Try to get corrected child ID from mapping
							childComputedID := entry.ID
							if childCorrected, ok := storedToCorrected[entry.ID]; ok {
								childComputedID = childCorrected.ComputedFSID
							}
							dirent := map[string]interface{}{
								"id":    childComputedID,
								"mode":  entry.Mode,
								"mtime": entry.Mtime,
								"name":  entry.Name,
							}
							if entry.Modifier != "" {
								dirent["modifier"] = entry.Modifier
							}
							if entry.Size > 0 {
								dirent["size"] = entry.Size
							}
							dirents = append(dirents, dirent)
						}
					}
				} else {
					dirents = []map[string]interface{}{}
				}
				jsonObj = map[string]interface{}{
					"dirents": dirents,
					"type":    3,
					"version": 1,
				}
			} else {
				jsonObj = map[string]interface{}{
					"block_ids": blockIDs,
					"size":      size,
					"type":      1,
					"version":   1,
				}
			}

			jsonBytes, err = json.Marshal(jsonObj)
			if err != nil {
				log.Printf("pack-fs: failed to marshal object %s: %v", storedFSID, err)
				continue
			}
			computedHash := sha1.Sum(jsonBytes)
			computedFSID = hex.EncodeToString(computedHash[:])
		}

		// Compress with zlib
		var compressed bytes.Buffer
		zlibWriter := zlib.NewWriter(&compressed)
		zlibWriter.Write(jsonBytes)
		zlibWriter.Close()

		// Write the COMPUTED fs_id - this matches what client expects
		buf.WriteString(computedFSID)

		// Write object size (4 bytes, network byte order)
		binary.Write(&buf, binary.BigEndian, uint32(compressed.Len()))

		// Write zlib-compressed content
		buf.Write(compressed.Bytes())
	}

	c.Data(http.StatusOK, "application/octet-stream", buf.Bytes())
}

// RecvFS receives and stores FS objects from client
// POST /seafhttp/repo/:repo_id/recv-fs
// Seafile sends packed FS objects in binary format:
// For each object: 40-byte hex ID + 4-byte size (BE) + zlib-compressed JSON
func (h *SyncHandler) RecvFS(c *gin.Context) {
	repoID := c.Param("repo_id")

	// Read FS objects from body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	if len(body) < 44 { // At least 40 (ID) + 4 (size)
		c.JSON(http.StatusBadRequest, gin.H{"error": "body too short"})
		return
	}

	// Parse packed FS objects
	// Format: each object is [40-char hex ID][4-byte size][zlib-compressed JSON]
	offset := 0
	objectsStored := 0

	for offset+44 <= len(body) {
		// Read 40-char hex FS ID
		fsID := string(body[offset : offset+40])
		offset += 40

		// Read 4-byte size (big-endian)
		objSize := binary.BigEndian.Uint32(body[offset : offset+4])
		offset += 4

		// Read the compressed object data
		if offset+int(objSize) > len(body) {
			log.Printf("recv-fs: truncated object data for %s", fsID)
			break
		}
		compressedData := body[offset : offset+int(objSize)]
		offset += int(objSize)

		// Decompress with zlib
		zlibReader, err := zlib.NewReader(bytes.NewReader(compressedData))
		if err != nil {
			log.Printf("recv-fs: failed to create zlib reader for %s: %v", fsID, err)
			continue
		}
		jsonData, err := io.ReadAll(zlibReader)
		zlibReader.Close()
		if err != nil {
			log.Printf("recv-fs: failed to decompress object %s: %v", fsID, err)
			continue
		}

		// CRITICAL: We must preserve the EXACT JSON bytes for dirents because
		// the fs_id is the SHA1 hash of the exact JSON content. Re-marshaling
		// would change the key order and break hash verification.
		//
		// Use json.RawMessage to extract the dirents without re-marshaling.
		var rawObj struct {
			Type     int               `json:"type"`
			Version  int               `json:"version"`
			Dirents  json.RawMessage   `json:"dirents,omitempty"`
			BlockIDs []string          `json:"block_ids,omitempty"`
			Size     int64             `json:"size,omitempty"`
		}
		if err := json.Unmarshal(jsonData, &rawObj); err != nil {
			log.Printf("recv-fs: failed to parse JSON for %s: %v", fsID, err)
			continue
		}

		fsType := "dir"
		var size int64
		var blockIDs []string
		var entriesJSON string = "[]"

		if rawObj.Type == 1 {
			// File object
			fsType = "file"
			size = rawObj.Size
			blockIDs = rawObj.BlockIDs
		} else if rawObj.Type == 3 {
			// Directory object - preserve exact bytes of dirents
			if len(rawObj.Dirents) > 0 {
				entriesJSON = string(rawObj.Dirents)
			}
		}

		now := time.Now().Unix()

		err = h.db.Session().Query(`
			INSERT INTO fs_objects (library_id, fs_id, obj_type, obj_name, size_bytes, mtime, dir_entries, block_ids)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, repoID, fsID, fsType, "", size, now, entriesJSON, blockIDs).Exec()

		if err != nil {
			log.Printf("recv-fs: Failed to store object %s: %v", fsID, err)
		} else {
			objectsStored++
		}
	}

	log.Printf("recv-fs: Stored %d objects for repo %s", objectsStored, repoID)
	c.Status(http.StatusOK)
}

// isHexString checks if bytes are valid hex characters
func isHexString(b []byte) bool {
	for _, c := range b {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// CheckFS checks which FS objects already exist
// POST /seafhttp/repo/:repo_id/check-fs
func (h *SyncHandler) CheckFS(c *gin.Context) {
	repoID := c.Param("repo_id")

	// Read FS IDs from body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	// Parse the body - can be JSON array or newline-separated
	var fsIDs []string
	bodyStr := strings.TrimSpace(string(body))
	if strings.HasPrefix(bodyStr, "[") {
		// JSON array format
		if err := json.Unmarshal(body, &fsIDs); err != nil {
			log.Printf("check-fs: failed to parse JSON array: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON array"})
			return
		}
	} else {
		// Newline-separated format
		fsIDs = strings.Split(bodyStr, "\n")
	}

	// Check which FS objects DON'T exist on server
	// Returns array of IDs that the server doesn't have
	// Initialize as empty slice so JSON serializes as [] not null
	missing := make([]string, 0)
	for _, fsID := range fsIDs {
		if fsID == "" || len(fsID) != 40 {
			continue
		}

		var exists string
		err := h.db.Session().Query(`
			SELECT fs_id FROM fs_objects WHERE library_id = ? AND fs_id = ? LIMIT 1
		`, repoID, fsID).Scan(&exists)

		if err != nil {
			// FS object doesn't exist on server
			missing = append(missing, fsID)
		}
	}

	// Return as JSON array (Seafile format)
	c.JSON(http.StatusOK, missing)
}

// PermissionCheck checks user permissions for the repository
// GET /seafhttp/repo/:repo_id/permission-check
func (h *SyncHandler) PermissionCheck(c *gin.Context) {
	// Real Seafile returns empty body with 200 OK for success
	// The permission is already validated by auth middleware
	// TODO: Implement proper permission checking and return 403 if denied
	c.Status(http.StatusOK)
}

// QuotaCheck checks if user has enough quota for upload
// GET /seafhttp/repo/:repo_id/quota-check
func (h *SyncHandler) QuotaCheck(c *gin.Context) {
	// For now, return unlimited quota
	// TODO: Implement proper quota checking
	c.JSON(http.StatusOK, gin.H{
		"has_quota": true,
	})
}

// GetHeadCommitsMulti returns head commits for multiple repositories at once
// POST /seafhttp/repo/head-commits-multi
func (h *SyncHandler) GetHeadCommitsMulti(c *gin.Context) {
	orgID := c.GetString("org_id")

	// Read repo IDs from body (newline separated)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	repoIDs := strings.Split(strings.TrimSpace(string(body)), "\n")

	// Build response map of repo_id -> head_commit_id
	result := make(map[string]string)

	for _, repoID := range repoIDs {
		if repoID == "" {
			continue
		}

		var headCommitID string
		err := h.db.Session().Query(`
			SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?
		`, orgID, repoID).Scan(&headCommitID)

		if err == nil && headCommitID != "" {
			result[repoID] = headCommitID
		}
	}

	c.JSON(http.StatusOK, result)
}

// UpdateBranch updates the head commit of a repository branch
// POST /seafhttp/repo/:repo_id/update-branch
func (h *SyncHandler) UpdateBranch(c *gin.Context) {
	repoID := c.Param("repo_id")
	orgID := c.GetString("org_id")

	// Get new head commit from query params
	newHead := c.Query("head")
	if newHead == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing head parameter"})
		return
	}

	// Verify the commit exists
	var commitID string
	err := h.db.Session().Query(`
		SELECT commit_id FROM commits WHERE library_id = ? AND commit_id = ?
	`, repoID, newHead).Scan(&commitID)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "commit not found"})
		return
	}

	// Update library head
	err = h.db.Session().Query(`
		UPDATE libraries SET head_commit_id = ?, updated_at = ?
		WHERE org_id = ? AND library_id = ?
	`, newHead, time.Now(), orgID, repoID).Exec()

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update branch"})
		return
	}

	// Return empty body with 200 OK (Seafile format)
	c.Status(http.StatusOK)
}

// GetDownloadInfo returns repository sync information for desktop client
// GET /seafhttp/repo/:repo_id/download-info
func (h *SyncHandler) GetDownloadInfo(c *gin.Context) {
	repoID := c.Param("repo_id")
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	// Get library info from database
	var libID, ownerID, name, description, headCommitID string
	var encrypted bool
	var encVersion int
	var magic, randomKey string
	var sizeBytes int64
	var updatedAt time.Time

	err := h.db.Session().Query(`
		SELECT library_id, owner_id, name, description, encrypted, enc_version,
		       magic, random_key, head_commit_id, size_bytes, updated_at
		FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, repoID).Scan(
		&libID, &ownerID, &name, &description, &encrypted, &encVersion,
		&magic, &randomKey, &headCommitID, &sizeBytes, &updatedAt,
	)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "library not found"})
		return
	}

	// Generate a sync token if we have a token creator
	token := ""
	if h.tokenCreator != nil {
		token, _ = h.tokenCreator.CreateDownloadToken(orgID, repoID, "/", userID)
	}

	// Format repo size in Seafile's human-readable format
	repoSizeFormatted := formatSizeSeafile(sizeBytes)

	// Format mtime as relative time HTML (Seafile format)
	mtimeRelative := formatRelativeTimeHTML(updatedAt)

	// Build response in Seafile format
	// Convert encrypted bool to int (Seafile uses 1/0, not true/false in download-info)
	encryptedInt := 0
	if encrypted {
		encryptedInt = 1
	}
	response := gin.H{
		"relay_id":            "localhost",
		"relay_addr":          "localhost",
		"relay_port":          "8080",
		"email":               userID + "@sesamefs.local",
		"token":               token,
		"repo_id":             repoID,
		"repo_name":           name,
		"repo_desc":           "", // Seafile returns empty string in download-info
		"repo_size":           sizeBytes,
		"repo_size_formatted": repoSizeFormatted,
		"repo_version":        1, // Standard Seafile repo version
		"mtime":               updatedAt.Unix(),
		"mtime_relative":      mtimeRelative,
		"encrypted":           encryptedInt, // Seafile uses int (1/0), not bool
		"permission":          "rw",
		"head_commit_id":      headCommitID,
		// NOTE: is_corrupted is NOT included in download-info, only in commit/HEAD
	}

	// Add encryption fields if encrypted
	// Translate enc_version for Seafile desktop client compatibility
	if encrypted {
		clientEncVersion := encVersion
		if encVersion == 12 || encVersion == 10 {
			clientEncVersion = 2
		}
		response["enc_version"] = clientEncVersion
		// CRITICAL: For Seafile v2, salt must be empty string (not null)
		response["salt"] = ""
		response["magic"] = magic
		response["random_key"] = randomKey
	}

	c.JSON(http.StatusOK, response)
}
