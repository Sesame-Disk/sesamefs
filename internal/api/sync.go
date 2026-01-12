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

	v2 "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	"github.com/Sesame-Disk/sesamefs/internal/crypto"
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
	RepoDesc       string  `json:"repo_desc,omitempty"`        // Repository description
	RepoCategory   *string `json:"repo_category"`              // Repository category (null)
	NoLocalHistory int     `json:"no_local_history,omitempty"` // 1 = no local history
	Encrypted      bool    `json:"encrypted,omitempty"`
	EncVersion     int     `json:"enc_version,omitempty"`
	Magic          string  `json:"magic,omitempty"`
	RandomKey      string  `json:"random_key,omitempty"`
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
type FSEntry struct {
	Name     string `json:"name"`
	ID       string `json:"id"`       // FS object ID
	Mode     int    `json:"mode"`     // Unix file mode (33188 = regular file, 16384 = directory)
	Mtime    int64  `json:"mtime"`
	Size     int64  `json:"size,omitempty"`
	Modifier string `json:"modifier,omitempty"`
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
			c.JSON(http.StatusOK, gin.H{"is_corrupted": false, "head_commit_id": ""})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"is_corrupted":   false,
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

	// Get library info for repo_name and repo_desc
	var repoName, repoDesc string
	h.db.Session().Query(`
		SELECT name, description FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, repoID).Scan(&repoName, &repoDesc)

	commit.RepoID = repoID
	commit.RootID = rootID
	commit.Description = description
	// Seafile uses 40 zeros for creator ID format
	commit.Creator = strings.Repeat("0", 40)
	commit.CreatorName = creator + "@sesamefs.local"
	commit.Ctime = ctime.Unix()
	commit.Version = 1 // Seafile commit format version 1
	commit.RepoName = repoName
	commit.RepoDesc = repoDesc
	commit.NoLocalHistory = 1

	// Set pointer fields - null if empty, pointer to value otherwise
	if parentID == "" {
		commit.ParentID = nil
	} else {
		commit.ParentID = &parentID
	}
	commit.SecondParentID = nil // Always null for now
	commit.RepoCategory = nil   // Always null

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

	// Check if this is an encrypted library - if so, we may need to encrypt the block
	// for Seafile clients (blocks uploaded via web may be stored unencrypted)
	repoID := c.Param("repo_id")
	userID := c.GetString("user_id")

	log.Printf("GetBlock: checking encryption for repo=%s, user=%s, block=%s", repoID, userID, externalID)

	if h.db != nil && repoID != "" {
		var encrypted bool
		err := h.db.Session().Query(`
			SELECT encrypted FROM libraries WHERE library_id = ? ALLOW FILTERING
		`, repoID).Scan(&encrypted)

		log.Printf("GetBlock: library encrypted=%v, err=%v", encrypted, err)

		if err == nil && encrypted {
			// Library is encrypted - check if block needs encryption
			// Seafile clients expect encrypted blocks; web uploads may be unencrypted
			isUnenc := isUnencryptedBlock(data)
			previewLen := 16
			if len(data) < previewLen {
				previewLen = len(data)
			}
			log.Printf("GetBlock: block isUnencrypted=%v (first bytes: %x)", isUnenc, data[:previewLen])
			if isUnenc {
				// Block appears to be unencrypted - encrypt it for the client
				fileKey := v2.GetDecryptSessions().GetFileKey(userID, repoID)
				if fileKey != nil {
					encryptedData, err := crypto.EncryptBlock(data, fileKey)
					if err == nil {
						log.Printf("GetBlock: encrypted unencrypted block %s for repo %s (original: %d, encrypted: %d bytes)",
							externalID, repoID, len(data), len(encryptedData))
						data = encryptedData
					} else {
						log.Printf("GetBlock: failed to encrypt block %s: %v", externalID, err)
					}
				} else {
					log.Printf("GetBlock: library %s is encrypted but no file key in session for user %s", repoID, userID)
				}
			}
		}
	}

	// Update last accessed time (if DB available)
	if h.db != nil {
		_ = h.db.Session().Query(`
			UPDATE blocks SET last_accessed = ? WHERE org_id = ? AND block_id = ?
		`, time.Now(), orgID, internalID).Exec()
	}

	c.Data(http.StatusOK, "application/octet-stream", data)
}

// isUnencryptedBlock checks if a block appears to be unencrypted
// Encrypted blocks should look like random data; unencrypted files have recognizable headers
func isUnencryptedBlock(data []byte) bool {
	if len(data) < 16 {
		return false // Too small to tell
	}

	// Check for common file signatures that indicate unencrypted data
	// ZIP/Office formats (docx, xlsx, pptx, etc.)
	if bytes.HasPrefix(data, []byte("PK")) {
		return true
	}
	// PDF
	if bytes.HasPrefix(data, []byte("%PDF")) {
		return true
	}
	// PNG
	if bytes.HasPrefix(data, []byte{0x89, 0x50, 0x4E, 0x47}) {
		return true
	}
	// JPEG
	if bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}) {
		return true
	}
	// GIF
	if bytes.HasPrefix(data, []byte("GIF8")) {
		return true
	}
	// Plain text (high ASCII ratio suggests text)
	asciiCount := 0
	checkLen := 256
	if len(data) < checkLen {
		checkLen = len(data)
	}
	for i := 0; i < checkLen; i++ {
		if data[i] >= 0x20 && data[i] < 0x7F || data[i] == '\n' || data[i] == '\r' || data[i] == '\t' {
			asciiCount++
		}
	}
	// If >80% appears to be printable ASCII, likely unencrypted text
	if float64(asciiCount)/float64(checkLen) > 0.8 {
		return true
	}

	return false
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

	// Parse as newline-separated block IDs (Seafile format)
	externalIDs := strings.Split(strings.TrimSpace(string(body)), "\n")

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
	var needed []string
	for _, extID := range externalIDs {
		if extID == "" {
			continue
		}
		internalID := externalToInternal[extID]
		if !existMap[internalID] {
			needed = append(needed, extID)
		}
	}

	// Return as newline-separated list
	c.String(http.StatusOK, strings.Join(needed, "\n"))
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

	// Return as JSON array (Seafile format)
	c.JSON(http.StatusOK, fsIDs)
}

// collectFSIDs recursively collects all fs_ids from a directory tree
func (h *SyncHandler) collectFSIDs(repoID, fsID string, dirOnly bool, fsIDs *[]string) {
	if fsID == "" || len(fsID) != 40 {
		return
	}

	// Add this fs_id to the list
	*fsIDs = append(*fsIDs, fsID)

	// Query the fs_object to see if it's a directory
	var fsType string
	var entriesJSON string
	err := h.db.Session().Query(`
		SELECT obj_type, dir_entries FROM fs_objects WHERE library_id = ? AND fs_id = ?
	`, repoID, fsID).Scan(&fsType, &entriesJSON)

	if err != nil {
		return // Object not found, skip
	}

	if fsType != "dir" {
		return // Files don't have children
	}

	// Parse directory entries and recursively collect their fs_ids
	if entriesJSON == "" || entriesJSON == "[]" {
		return
	}

	var entries []struct {
		ID   string `json:"id"`
		Mode int    `json:"mode"`
	}
	if err := json.Unmarshal([]byte(entriesJSON), &entries); err != nil {
		return
	}

	for _, entry := range entries {
		if entry.ID == "" || len(entry.ID) != 40 {
			continue
		}

		// Check if this is a directory (mode & 0040000 != 0) or file
		isDir := (entry.Mode & 0040000) != 0

		if dirOnly && !isDir {
			continue // Skip files if dir-only requested
		}

		// Recursively collect this entry's fs_id
		h.collectFSIDs(repoID, entry.ID, dirOnly, fsIDs)
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
func (h *SyncHandler) PackFS(c *gin.Context) {
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
			log.Printf("pack-fs: failed to parse JSON array: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON array"})
			return
		}
	} else {
		// Newline-separated format
		fsIDs = strings.Split(bodyStr, "\n")
	}

	// Build binary response
	var buf bytes.Buffer

	for _, fsID := range fsIDs {
		if fsID == "" || len(fsID) != 40 {
			continue
		}

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
			log.Printf("pack-fs: object %s not found: %v", fsID, err)
			continue // Skip missing objects
		}

		// Build JSON object that matches Seafile's format
		// CRITICAL: The JSON bytes MUST match exactly what was used to compute fs_id hash
		// CRITICAL: Must use map[string]interface{} which serializes keys alphabetically.
		// Using a struct would change field order and produce different hash.
		var jsonObj map[string]interface{}

		if fsType == "dir" {
			// Directory format: {"dirents":[...],"type":3,"version":1} (alphabetical)
			// Use json.RawMessage to preserve exact byte ordering of dirents
			var rawDirents json.RawMessage
			if entriesJSON != "" && entriesJSON != "[]" {
				rawDirents = json.RawMessage(entriesJSON)
			} else {
				rawDirents = json.RawMessage("[]")
			}
			jsonObj = map[string]interface{}{
				"version": 1,
				"type":    3, // SEAF_METADATA_TYPE_DIR
				"dirents": rawDirents,
			}
		} else {
			// File format: {"block_ids":[...],"size":N,"type":1,"version":1} (alphabetical)
			jsonObj = map[string]interface{}{
				"version":   1,
				"type":      1, // SEAF_METADATA_TYPE_FILE
				"block_ids": blockIDs,
				"size":      size,
			}
		}

		// Serialize to JSON (map produces alphabetical key order)
		jsonBytes, err := json.Marshal(jsonObj)
		if err != nil {
			log.Printf("pack-fs: failed to marshal object %s: %v", fsID, err)
			continue
		}

		// DEBUG: Log what we're sending and verify hash
		computedHash := sha1.Sum(jsonBytes)
		computedFSID := hex.EncodeToString(computedHash[:])
		log.Printf("pack-fs DEBUG: fs_id=%s, computed_hash=%s, match=%v, json=%s",
			fsID, computedFSID, fsID == computedFSID, string(jsonBytes))

		// Compress with zlib - Seafile server stores fs objects compressed,
		// so pack-fs sends compressed data. Client stores as-is and decompresses when reading.
		var compressed bytes.Buffer
		zlibWriter := zlib.NewWriter(&compressed)
		zlibWriter.Write(jsonBytes)
		zlibWriter.Close()

		// Write object ID (40-byte hex string, no newline)
		buf.WriteString(fsID)

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

		// Parse JSON
		var obj map[string]interface{}
		if err := json.Unmarshal(jsonData, &obj); err != nil {
			log.Printf("recv-fs: failed to parse JSON for %s: %v", fsID, err)
			continue
		}

		// Extract type (1=file, 3=dir)
		objType := 0
		if t, ok := obj["type"].(float64); ok {
			objType = int(t)
		}

		fsType := "dir"
		var size int64
		var blockIDs []string
		var entriesJSON string = "[]"

		if objType == 1 {
			// File object
			fsType = "file"
			if s, ok := obj["size"].(float64); ok {
				size = int64(s)
			}
			if bids, ok := obj["block_ids"].([]interface{}); ok {
				for _, bid := range bids {
					if bidStr, ok := bid.(string); ok {
						blockIDs = append(blockIDs, bidStr)
					}
				}
			}
		} else if objType == 3 {
			// Directory object
			if dirents, ok := obj["dirents"].([]interface{}); ok {
				direntBytes, _ := json.Marshal(dirents)
				entriesJSON = string(direntBytes)
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

	fsIDs := strings.Split(strings.TrimSpace(string(body)), "\n")

	// Check which FS objects exist
	var needed []string
	for _, fsID := range fsIDs {
		if fsID == "" {
			continue
		}

		var exists string
		err := h.db.Session().Query(`
			SELECT fs_id FROM fs_objects WHERE library_id = ? AND fs_id = ? LIMIT 1
		`, repoID, fsID).Scan(&exists)

		if err != nil {
			needed = append(needed, fsID)
		}
	}

	c.String(http.StatusOK, strings.Join(needed, "\n"))
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

	// Build response in Seafile format
	response := gin.H{
		"relay_id":       "localhost",
		"relay_addr":     "localhost",
		"relay_port":     "8080",
		"email":          userID + "@sesamefs.local",
		"token":          token,
		"repo_id":        repoID,
		"repo_name":      name,
		"repo_desc":      description,
		"repo_size":      sizeBytes,
		"repo_version":   1, // Standard Seafile repo version
		"mtime":          updatedAt.Unix(),
		"encrypted":      encrypted,
		"permission":     "rw",
		"head_commit_id": headCommitID,
		"is_corrupted":   false,
	}

	// Add encryption fields if encrypted
	// Translate enc_version for Seafile desktop client compatibility
	if encrypted {
		clientEncVersion := encVersion
		if encVersion == 12 || encVersion == 10 {
			clientEncVersion = 2
		}
		response["enc_version"] = clientEncVersion
		response["magic"] = magic
		response["random_key"] = randomKey
	}

	c.JSON(http.StatusOK, response)
}
