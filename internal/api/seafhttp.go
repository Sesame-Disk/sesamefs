package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/crypto"
	v2 "github.com/Sesame-Disk/sesamefs/internal/api/v2"
	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	"github.com/gin-gonic/gin"
)

// TokenType represents the type of access token
type TokenType string

const (
	TokenTypeUpload   TokenType = "upload"
	TokenTypeDownload TokenType = "download"
)

// AccessToken represents a temporary access token for file operations
type AccessToken struct {
	Token     string
	Type      TokenType
	OrgID     string
	RepoID    string
	Path      string    // File path for downloads, parent dir for uploads
	UserID    string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// TokenStore is the interface for token operations (can be in-memory or Cassandra-backed)
type TokenStore interface {
	CreateUploadToken(orgID, repoID, path, userID string) (string, error)
	CreateDownloadToken(orgID, repoID, path, userID string) (string, error)
	GetToken(tokenStr string, expectedType TokenType) (*AccessToken, bool)
	DeleteToken(tokenStr string) error
}

// TokenManager manages temporary access tokens for file operations
type TokenManager struct {
	tokens   map[string]*AccessToken
	mu       sync.RWMutex
	tokenTTL time.Duration
}

// NewTokenManager creates a new token manager with the specified TTL
func NewTokenManager(tokenTTL time.Duration) *TokenManager {
	if tokenTTL <= 0 {
		tokenTTL = DefaultTokenTTL
	}
	tm := &TokenManager{
		tokens:   make(map[string]*AccessToken),
		tokenTTL: tokenTTL,
	}
	// Start cleanup goroutine
	go tm.cleanup()
	return tm
}

// DefaultTokenTTL is the default time-to-live for tokens
const DefaultTokenTTL = 1 * time.Hour

// CreateToken creates a new access token
func (tm *TokenManager) CreateToken(tokenType TokenType, orgID, repoID, path, userID string, ttl time.Duration) (*AccessToken, error) {
	// Generate random token
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return nil, err
	}
	tokenStr := hex.EncodeToString(bytes)

	token := &AccessToken{
		Token:     tokenStr,
		Type:      tokenType,
		OrgID:     orgID,
		RepoID:    repoID,
		Path:      path,
		UserID:    userID,
		ExpiresAt: time.Now().Add(ttl),
		CreatedAt: time.Now(),
	}

	tm.mu.Lock()
	tm.tokens[tokenStr] = token
	tm.mu.Unlock()

	return token, nil
}

// CreateUploadToken creates an upload token (implements TokenCreator interface)
func (tm *TokenManager) CreateUploadToken(orgID, repoID, path, userID string) (string, error) {
	token, err := tm.CreateToken(TokenTypeUpload, orgID, repoID, path, userID, tm.tokenTTL)
	if err != nil {
		return "", err
	}
	return token.Token, nil
}

// CreateDownloadToken creates a download token (implements TokenCreator interface)
func (tm *TokenManager) CreateDownloadToken(orgID, repoID, path, userID string) (string, error) {
	token, err := tm.CreateToken(TokenTypeDownload, orgID, repoID, path, userID, tm.tokenTTL)
	if err != nil {
		return "", err
	}
	return token.Token, nil
}

// GetToken retrieves and validates a token
func (tm *TokenManager) GetToken(tokenStr string, expectedType TokenType) (*AccessToken, bool) {
	tm.mu.RLock()
	token, exists := tm.tokens[tokenStr]
	tm.mu.RUnlock()

	if !exists {
		return nil, false
	}

	// Check if expired
	if time.Now().After(token.ExpiresAt) {
		tm.DeleteToken(tokenStr)
		return nil, false
	}

	// Check type
	if token.Type != expectedType {
		return nil, false
	}

	return token, true
}

// DeleteToken removes a token
func (tm *TokenManager) DeleteToken(tokenStr string) error {
	tm.mu.Lock()
	delete(tm.tokens, tokenStr)
	tm.mu.Unlock()
	return nil
}

// cleanup periodically removes expired tokens
func (tm *TokenManager) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		tm.mu.Lock()
		now := time.Now()
		for token, at := range tm.tokens {
			if now.After(at.ExpiresAt) {
				delete(tm.tokens, token)
			}
		}
		tm.mu.Unlock()
	}
}

// Ensure TokenManager implements TokenStore
var _ TokenStore = (*TokenManager)(nil)

// ChunkUpload tracks an ongoing chunked upload
type ChunkUpload struct {
	Token       string
	Filename    string
	ParentDir   string
	TotalSize   int64
	TempFile    *os.File
	TempPath    string
	ReceivedEnd int64 // Track the highest byte received
	mu          sync.Mutex
}

// ChunkManager manages chunked uploads
type ChunkManager struct {
	uploads map[string]*ChunkUpload // keyed by "token:filename"
	mu      sync.RWMutex
	tempDir string
}

// NewChunkManager creates a new chunk manager
func NewChunkManager() *ChunkManager {
	tempDir := os.TempDir()
	return &ChunkManager{
		uploads: make(map[string]*ChunkUpload),
		tempDir: tempDir,
	}
}

// Global chunk manager instance
var chunkManager = NewChunkManager()

// GetOrCreateUpload gets or creates a chunk upload tracker
func (cm *ChunkManager) GetOrCreateUpload(token, filename, parentDir string, totalSize int64) (*ChunkUpload, error) {
	key := token + ":" + filename
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if upload, exists := cm.uploads[key]; exists {
		return upload, nil
	}

	// Create temp file
	tempPath := filepath.Join(cm.tempDir, fmt.Sprintf("sesamefs_upload_%s_%s", token, sanitizeFilename(filename)))
	tempFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}

	// Pre-allocate the file to total size (for seeking)
	if totalSize > 0 {
		if err := tempFile.Truncate(totalSize); err != nil {
			tempFile.Close()
			os.Remove(tempPath)
			return nil, fmt.Errorf("failed to pre-allocate temp file: %w", err)
		}
	}

	upload := &ChunkUpload{
		Token:       token,
		Filename:    filename,
		ParentDir:   parentDir,
		TotalSize:   totalSize,
		TempFile:    tempFile,
		TempPath:    tempPath,
		ReceivedEnd: -1,
	}
	cm.uploads[key] = upload
	log.Printf("[ChunkManager] Created upload tracker: %s, totalSize=%d", key, totalSize)
	return upload, nil
}

// WriteChunk writes a chunk to the correct position in the temp file
func (cu *ChunkUpload) WriteChunk(data []byte, start, end int64) error {
	cu.mu.Lock()
	defer cu.mu.Unlock()

	// Seek to the start position
	if _, err := cu.TempFile.Seek(start, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek: %w", err)
	}

	// Write the data
	if _, err := cu.TempFile.Write(data); err != nil {
		return fmt.Errorf("failed to write chunk: %w", err)
	}

	// Update received end marker
	if end > cu.ReceivedEnd {
		cu.ReceivedEnd = end
	}

	log.Printf("[ChunkUpload] Wrote chunk: start=%d, end=%d, received_end=%d, total=%d",
		start, end, cu.ReceivedEnd, cu.TotalSize)
	return nil
}

// IsComplete checks if all chunks have been received
func (cu *ChunkUpload) IsComplete() bool {
	return cu.ReceivedEnd >= cu.TotalSize-1
}

// GetContent reads the complete file content
func (cu *ChunkUpload) GetContent() ([]byte, error) {
	cu.mu.Lock()
	defer cu.mu.Unlock()

	if _, err := cu.TempFile.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(cu.TempFile)
}

// Cleanup removes the temp file
func (cu *ChunkUpload) Cleanup() error {
	cu.mu.Lock()
	defer cu.mu.Unlock()

	if cu.TempFile != nil {
		cu.TempFile.Close()
	}
	return os.Remove(cu.TempPath)
}

// CleanupUpload removes an upload from tracking
func (cm *ChunkManager) CleanupUpload(token, filename string) {
	key := token + ":" + filename
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if upload, exists := cm.uploads[key]; exists {
		upload.Cleanup()
		delete(cm.uploads, key)
		log.Printf("[ChunkManager] Cleaned up upload: %s", key)
	}
}

// sanitizeFilename makes a filename safe for temp file naming
func sanitizeFilename(filename string) string {
	// Replace unsafe characters with underscore
	reg := regexp.MustCompile(`[^a-zA-Z0-9._-]`)
	return reg.ReplaceAllString(filename, "_")
}

// parseContentRange parses Content-Range header
// Format: bytes start-end/total
// Returns: start, end, total, ok
func parseContentRange(header string) (int64, int64, int64, bool) {
	if header == "" {
		return 0, 0, 0, false
	}

	// Format: bytes start-end/total
	var start, end, total int64
	n, err := fmt.Sscanf(header, "bytes %d-%d/%d", &start, &end, &total)
	if err != nil || n != 3 {
		log.Printf("[parseContentRange] Failed to parse: %s, err=%v", header, err)
		return 0, 0, 0, false
	}
	return start, end, total, true
}

// SeafHTTPHandler handles Seafile-compatible file operations
type SeafHTTPHandler struct {
	storage        *storage.S3Store
	storageManager *storage.Manager
	db             *db.DB
	tokenStore     TokenStore
}

// NewSeafHTTPHandler creates a new SeafHTTP handler
func NewSeafHTTPHandler(s3Store *storage.S3Store, storageManager *storage.Manager, database *db.DB, tokenStore TokenStore) *SeafHTTPHandler {
	return &SeafHTTPHandler{
		storage:        s3Store,
		storageManager: storageManager,
		db:             database,
		tokenStore:     tokenStore,
	}
}

// RegisterSeafHTTPRoutes registers the seafhttp routes
func (h *SeafHTTPHandler) RegisterSeafHTTPRoutes(router *gin.Engine) {
	seafhttp := router.Group("/seafhttp")
	{
		// Upload endpoint - receives files and stores them in S3
		seafhttp.POST("/upload-api/:token", h.HandleUpload)

		// Download endpoint - streams files from S3
		seafhttp.GET("/files/:token/*filepath", h.HandleDownload)
	}
}

// HandleUpload handles file uploads via the upload token
// Supports both single-shot uploads and chunked/resumable uploads (via Content-Range header)
func (h *SeafHTTPHandler) HandleUpload(c *gin.Context) {
	tokenStr := c.Param("token")

	// Validate token
	token, valid := h.tokenStore.GetToken(tokenStr, TokenTypeUpload)
	if !valid {
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid or expired upload token"})
		return
	}

	// Check if storage is available
	if h.storage == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage not available"})
		return
	}

	// Get the file from the request
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	defer file.Close()

	// Get optional parameters
	parentDir := c.DefaultPostForm("parent_dir", token.Path)
	relativePath := c.PostForm("relative_path")
	replace := c.DefaultPostForm("replace", "0")
	retJSON := c.Query("ret-json") == "1" || c.PostForm("ret-json") == "1"

	_ = replace // TODO: Handle replace logic

	filename := header.Filename

	// Handle relative_path for folder uploads (e.g., "my-folder/subfolder/file.txt")
	// The relative_path contains the full path relative to the upload target
	if relativePath != "" {
		// Directory markers are when relative_path ends with "/" AND the header filename
		// matches the directory name (or is empty). This distinguishes from actual files
		// in directories where relative_path is the directory and header.Filename is the file.
		if strings.HasSuffix(relativePath, "/") {
			dirName := strings.TrimSuffix(relativePath, "/")
			dirBaseName := filepath.Base(dirName)

			// If the header filename matches the directory name or is the same as relative_path,
			// this is a directory marker, not a real file
			if filename == dirBaseName || filename == relativePath || filename == "" {
				log.Printf("[HandleUpload] Skipping directory marker: %s (filename=%s)", relativePath, filename)
				// Return response in the same format as regular uploads so frontend can parse it
				if retJSON {
					c.JSON(http.StatusOK, []gin.H{
						{
							"name": dirBaseName,
							"id":   "", // Directory markers don't have a real ID
							"size": "0",
						},
					})
				} else {
					c.String(http.StatusOK, "")
				}
				return
			}

			// This is a real file inside a directory - relative_path is the directory,
			// header.Filename is the actual file
			log.Printf("[HandleUpload] File in directory: relativePath=%s, filename=%s", relativePath, filename)
			parentDir = filepath.Join(parentDir, dirName)
			// filename stays as header.Filename
		} else {
			// relative_path contains the full path including filename
			// Extract directory from relative path (everything before the filename)
			relDir := filepath.Dir(relativePath)
			if relDir != "." && relDir != "" {
				// Combine parent_dir with the relative directory
				parentDir = filepath.Join(parentDir, relDir)
			}
			// Use the filename from relative_path (may differ from header.Filename)
			filename = filepath.Base(relativePath)
		}
	}

	// Clean the path to ensure it starts with /
	if !strings.HasPrefix(parentDir, "/") {
		parentDir = "/" + parentDir
	}
	parentDir = filepath.Clean(parentDir)

	log.Printf("[HandleUpload] relativePath=%s, parentDir=%s, filename=%s", relativePath, parentDir, filename)

	filePath := filepath.Join(parentDir, filename)
	storageKey := fmt.Sprintf("%s/%s%s", token.OrgID, token.RepoID, filePath)

	// Check for Content-Range header (chunked upload)
	contentRange := c.GetHeader("Content-Range")
	start, end, total, isChunked := parseContentRange(contentRange)

	log.Printf("[HandleUpload] Token=%s, File=%s, ContentRange=%s, isChunked=%v",
		tokenStr, filename, contentRange, isChunked)

	// Read chunk/file content
	chunkData, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read file"})
		return
	}

	var content []byte
	var finalSize int64

	if isChunked {
		// Chunked upload: accumulate chunks in temp file
		upload, err := chunkManager.GetOrCreateUpload(tokenStr, filename, parentDir, total)
		if err != nil {
			log.Printf("[HandleUpload] Failed to create upload tracker: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to initialize upload"})
			return
		}

		// Write this chunk to the temp file
		if err := upload.WriteChunk(chunkData, start, end); err != nil {
			log.Printf("[HandleUpload] Failed to write chunk: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to write chunk"})
			return
		}

		// Check if upload is complete
		if !upload.IsComplete() {
			// More chunks expected - return success but don't finalize
			log.Printf("[HandleUpload] Chunk received, waiting for more: %d/%d", end+1, total)
			c.JSON(http.StatusOK, gin.H{
				"success": true,
			})
			return
		}

		// All chunks received - read the complete file
		log.Printf("[HandleUpload] All chunks received, finalizing upload")
		content, err = upload.GetContent()
		if err != nil {
			log.Printf("[HandleUpload] Failed to get content: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to assemble file"})
			return
		}
		finalSize = total

		// Cleanup the temp file
		chunkManager.CleanupUpload(tokenStr, filename)
	} else {
		// Single-shot upload: use the content directly
		content = chunkData
		finalSize = int64(len(content))
	}

	// Generate file ID based on content hash (SHA-1 for Seafile compatibility)
	sha1Hash := sha1.Sum(content)
	fileID := hex.EncodeToString(sha1Hash[:])

	// Check if library is encrypted and encrypt content before storage
	var encrypted bool
	var storedContent = content
	err = h.db.Session().Query(`
		SELECT encrypted FROM libraries WHERE org_id = ? AND library_id = ?
	`, token.OrgID, token.RepoID).Scan(&encrypted)
	if err != nil {
		log.Printf("[HandleUpload] Failed to check encryption status: %v", err)
		// Continue without encryption
	}

	if encrypted {
		// Get the file key from the decrypt session
		fileKey := v2.GetDecryptSessions().GetFileKey(token.UserID, token.RepoID)
		if fileKey == nil {
			log.Printf("[HandleUpload] Library is encrypted but not unlocked for user %s", token.UserID)
			c.JSON(http.StatusForbidden, gin.H{"error": "library is encrypted and not unlocked"})
			return
		}
		// Encrypt the content
		encryptedContent, err := crypto.EncryptBlock(content, fileKey)
		if err != nil {
			log.Printf("[HandleUpload] Failed to encrypt content: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encrypt content"})
			return
		}
		log.Printf("[HandleUpload] Encrypted content for library %s (original: %d bytes, encrypted: %d bytes)",
			token.RepoID, len(content), len(encryptedContent))
		storedContent = encryptedContent
	}

	// Compute SHA-256 hash of the content to be stored
	sha256Hash := sha256.Sum256(storedContent)
	sha256ID := hex.EncodeToString(sha256Hash[:])

	// Store as a block using BlockStore for proper sync protocol compatibility
	ctx := context.Background()
	blockStore, _, err := h.storageManager.GetHealthyBlockStore("")
	if err != nil {
		log.Printf("[HandleUpload] Failed to get block store: %v, falling back to S3", err)
		// Fall back to direct S3 storage
		_, err = h.storage.Put(c.Request.Context(), storageKey, newBytesReader(storedContent), int64(len(storedContent)))
		if err != nil {
			log.Printf("[HandleUpload] Failed to upload to S3: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to upload file"})
			return
		}
	} else {
		// Store block using BlockStore (with SHA-256 hash)
		blockData := &storage.BlockData{
			Hash: sha256ID,
			Data: storedContent,
			Size: int64(len(storedContent)),
		}
		_, err = blockStore.PutBlockData(ctx, blockData)
		if err != nil {
			log.Printf("[HandleUpload] Failed to store block: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store block"})
			return
		}
		log.Printf("[HandleUpload] Stored block %s (SHA-256: %s)", fileID[:16], sha256ID[:16])
	}

	// Create SHA-1 → SHA-256 mapping for sync protocol compatibility
	err = h.db.Session().Query(`
		INSERT INTO block_id_mappings (org_id, external_id, internal_id) VALUES (?, ?, ?)
	`, token.OrgID, fileID, sha256ID).Exec()
	if err != nil {
		log.Printf("[HandleUpload] Warning: failed to create block mapping: %v", err)
		// Continue - the mapping is for optimization, not critical
	} else {
		log.Printf("[HandleUpload] Created block mapping: %s → %s", fileID[:16], sha256ID[:16])
	}

	log.Printf("[HandleUpload] File uploaded to S3, updating filesystem metadata...")

	// Update filesystem metadata: create fs_object entries and commit
	commitID, err := h.commitUploadedFile(token.OrgID, token.RepoID, token.UserID, parentDir, filename, fileID, content, finalSize)
	if err != nil {
		log.Printf("[HandleUpload] Failed to update filesystem: %v", err)
		// File is in S3 but not in filesystem - this is a problem but we'll return success
		// since the file data is safe. A future reconciliation process could fix this.
	} else {
		log.Printf("[HandleUpload] Filesystem updated, commit=%s", commitID)
	}

	log.Printf("[HandleUpload] Upload complete: file=%s, size=%d, id=%s", filename, finalSize, fileID[:16])

	// Return response based on ret-json parameter
	if retJSON {
		c.JSON(http.StatusOK, []gin.H{
			{
				"name": filename,
				"id":   fileID,
				"size": strconv.FormatInt(finalSize, 10),
			},
		})
	} else {
		// Return just the file ID as plain text (Seafile compatible)
		c.String(http.StatusOK, fileID)
	}
}

// commitUploadedFile updates the filesystem metadata after a file upload
func (h *SeafHTTPHandler) commitUploadedFile(orgID, repoID, userID, parentDir, filename, fileID string, content []byte, fileSize int64) (string, error) {
	// Get current head commit
	var headCommitID string
	err := h.db.Session().Query(`
		SELECT head_commit_id FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, repoID).Scan(&headCommitID)
	if err != nil {
		return "", fmt.Errorf("failed to get head commit: %w", err)
	}

	// Get root fs_id from head commit
	var rootFSID string
	err = h.db.Session().Query(`
		SELECT root_fs_id FROM commits WHERE library_id = ? AND commit_id = ?
	`, repoID, headCommitID).Scan(&rootFSID)
	if err != nil {
		return "", fmt.Errorf("failed to get root fs_id: %w", err)
	}

	log.Printf("[commitUploadedFile] headCommit=%s, rootFSID=%s, parentDir=%s, filename=%s",
		headCommitID, rootFSID, parentDir, filename)

	// Create fs_object for the file (single block for now)
	blockID := fileID // Use the file hash as block ID
	fileEntry := map[string]interface{}{
		"id":       fileID,
		"name":     filename,
		"mode":     33188, // Regular file (0100644)
		"mtime":    time.Now().Unix(),
		"size":     fileSize,
		"modifier": userID + "@sesamefs.local",
	}

	// Store file fs_object
	fileEntryJSON, _ := json.Marshal(fileEntry)
	err = h.db.Session().Query(`
		INSERT INTO fs_objects (library_id, fs_id, obj_type, obj_name, size_bytes, mtime, block_ids)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, repoID, fileID, "file", filename, fileSize, time.Now().Unix(), []string{blockID}).Exec()
	if err != nil {
		return "", fmt.Errorf("failed to create file fs_object: %w", err)
	}
	log.Printf("[commitUploadedFile] Created file fs_object: %s", fileID)
	_ = fileEntryJSON // unused but kept for potential future use

	// Navigate to parent directory and update its entries
	newRootFSID, err := h.addFileToDirectory(repoID, rootFSID, parentDir, filename, fileID, fileSize, userID)
	if err != nil {
		return "", fmt.Errorf("failed to add file to directory: %w", err)
	}

	// Create new commit
	description := fmt.Sprintf("Added or modified \"%s\".\n", filename)
	commitData := fmt.Sprintf("%s:%s:%s:%d", repoID, newRootFSID, description, time.Now().UnixNano())
	commitHash := sha1.Sum([]byte(commitData))
	newCommitID := hex.EncodeToString(commitHash[:])

	err = h.db.Session().Query(`
		INSERT INTO commits (library_id, commit_id, parent_id, root_fs_id, creator_id, description, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, repoID, newCommitID, headCommitID, newRootFSID, userID, description, time.Now()).Exec()
	if err != nil {
		return "", fmt.Errorf("failed to create commit: %w", err)
	}

	// Update library head
	err = h.db.Session().Query(`
		UPDATE libraries SET head_commit_id = ?, updated_at = ? WHERE org_id = ? AND library_id = ?
	`, newCommitID, time.Now(), orgID, repoID).Exec()
	if err != nil {
		return "", fmt.Errorf("failed to update library head: %w", err)
	}

	log.Printf("[commitUploadedFile] Created commit %s with root %s", newCommitID, newRootFSID)
	return newCommitID, nil
}

// addFileToDirectory adds a file entry to a directory, creating parent directories as needed
func (h *SeafHTTPHandler) addFileToDirectory(repoID, rootFSID, parentDir, filename, fileID string, fileSize int64, userID string) (string, error) {
	parentDir = strings.TrimSuffix(parentDir, "/")
	if parentDir == "" {
		parentDir = "/"
	}

	log.Printf("[addFileToDirectory] rootFSID=%s, parentDir=%s, filename=%s", rootFSID, parentDir, filename)

	// Get root directory entries
	var rootEntriesJSON string
	err := h.db.Session().Query(`
		SELECT dir_entries FROM fs_objects WHERE library_id = ? AND fs_id = ?
	`, repoID, rootFSID).Scan(&rootEntriesJSON)
	if err != nil {
		return "", fmt.Errorf("failed to get root entries: %w", err)
	}

	var rootEntries []map[string]interface{}
	if rootEntriesJSON != "" && rootEntriesJSON != "[]" {
		if err := json.Unmarshal([]byte(rootEntriesJSON), &rootEntries); err != nil {
			return "", fmt.Errorf("failed to parse root entries: %w", err)
		}
	}

	if parentDir == "/" {
		// Add file directly to root
		newEntry := map[string]interface{}{
			"id":       fileID,
			"name":     filename,
			"mode":     33188, // Regular file
			"mtime":    time.Now().Unix(),
			"size":     fileSize,
			"modifier": userID + "@sesamefs.local",
		}

		// Check if file already exists and update it, otherwise add new entry
		found := false
		for i, entry := range rootEntries {
			if entry["name"] == filename {
				rootEntries[i] = newEntry
				found = true
				break
			}
		}
		if !found {
			rootEntries = append(rootEntries, newEntry)
		}

		// Create new root fs_object
		return h.createDirectoryFSObject(repoID, rootEntries)
	}

	// Need to traverse and possibly create parent directories
	parts := strings.Split(strings.Trim(parentDir, "/"), "/")
	return h.traverseAndAddFile(repoID, rootFSID, rootEntries, parts, 0, filename, fileID, fileSize, userID)
}

// traverseAndAddFile recursively traverses/creates directories and adds a file
func (h *SeafHTTPHandler) traverseAndAddFile(repoID string, currentFSID string, entries []map[string]interface{}, pathParts []string, depth int, filename, fileID string, fileSize int64, userID string) (string, error) {
	if depth >= len(pathParts) {
		// We've reached the target directory, add the file
		newEntry := map[string]interface{}{
			"id":       fileID,
			"name":     filename,
			"mode":     33188,
			"mtime":    time.Now().Unix(),
			"size":     fileSize,
			"modifier": userID + "@sesamefs.local",
		}

		found := false
		for i, entry := range entries {
			if entry["name"] == filename {
				entries[i] = newEntry
				found = true
				break
			}
		}
		if !found {
			entries = append(entries, newEntry)
		}

		return h.createDirectoryFSObject(repoID, entries)
	}

	dirName := pathParts[depth]
	var childFSID string
	var childEntries []map[string]interface{}
	childIdx := -1

	// Look for existing directory
	for i, entry := range entries {
		if entry["name"] == dirName {
			childFSID = entry["id"].(string)
			childIdx = i

			// Get child directory entries
			var childEntriesJSON string
			err := h.db.Session().Query(`
				SELECT dir_entries FROM fs_objects WHERE library_id = ? AND fs_id = ?
			`, repoID, childFSID).Scan(&childEntriesJSON)
			if err != nil {
				return "", fmt.Errorf("failed to get child directory: %w", err)
			}
			if childEntriesJSON != "" && childEntriesJSON != "[]" {
				json.Unmarshal([]byte(childEntriesJSON), &childEntries)
			}
			break
		}
	}

	if childFSID == "" {
		// Create new directory
		childEntries = []map[string]interface{}{}
	}

	// Recursively process
	newChildFSID, err := h.traverseAndAddFile(repoID, childFSID, childEntries, pathParts, depth+1, filename, fileID, fileSize, userID)
	if err != nil {
		return "", err
	}

	// Update or add directory entry in current level
	dirEntry := map[string]interface{}{
		"id":       newChildFSID,
		"name":     dirName,
		"mode":     16384, // Directory (040000)
		"mtime":    time.Now().Unix(),
		"size":     0,
		"modifier": userID + "@sesamefs.local",
	}

	if childIdx >= 0 {
		entries[childIdx] = dirEntry
	} else {
		entries = append(entries, dirEntry)
	}

	return h.createDirectoryFSObject(repoID, entries)
}

// createDirectoryFSObject creates a new directory fs_object and returns its ID
func (h *SeafHTTPHandler) createDirectoryFSObject(repoID string, entries []map[string]interface{}) (string, error) {
	entriesJSON, err := json.Marshal(entries)
	if err != nil {
		return "", fmt.Errorf("failed to marshal entries: %w", err)
	}

	// Calculate fs_id as SHA-1 of serialized content
	dirData := fmt.Sprintf("%d\n%s", 1, string(entriesJSON))
	hash := sha1.Sum([]byte(dirData))
	fsID := hex.EncodeToString(hash[:])

	// Store in database
	err = h.db.Session().Query(`
		INSERT INTO fs_objects (library_id, fs_id, obj_type, dir_entries, mtime)
		VALUES (?, ?, ?, ?, ?)
	`, repoID, fsID, "dir", string(entriesJSON), time.Now().Unix()).Exec()
	if err != nil {
		return "", fmt.Errorf("failed to create directory fs_object: %w", err)
	}

	log.Printf("[createDirectoryFSObject] Created dir fs_object: %s with %d entries", fsID, len(entries))
	return fsID, nil
}

// HandleDownload handles file downloads via the download token
func (h *SeafHTTPHandler) HandleDownload(c *gin.Context) {
	tokenStr := c.Param("token")
	requestedPath := c.Param("filepath")

	log.Printf("[HandleDownload] Token: %s, RequestedPath: %s", tokenStr, requestedPath)

	// Validate token
	token, valid := h.tokenStore.GetToken(tokenStr, TokenTypeDownload)
	if !valid {
		log.Printf("[HandleDownload] Invalid token: %s", tokenStr)
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid or expired download token"})
		return
	}

	log.Printf("[HandleDownload] Token valid: OrgID=%s, RepoID=%s, Path=%s", token.OrgID, token.RepoID, token.Path)

	// Get filename from path
	filename := filepath.Base(token.Path)
	if requestedPath != "" && requestedPath != "/" {
		filename = filepath.Base(requestedPath)
	}

	// Try to get file content from block storage (content-addressed)
	// This is the normal flow for SesameFS files
	if h.db != nil && h.storageManager != nil {
		log.Printf("[HandleDownload] Attempting block-based file retrieval")
		content, err := h.getFileFromBlocks(c, token)
		if err == nil {
			log.Printf("[HandleDownload] Block-based retrieval SUCCESS, size=%d", len(content))
			c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
			c.Data(http.StatusOK, "application/octet-stream", content)
			return
		}
		log.Printf("[HandleDownload] Block-based retrieval FAILED: %v", err)
		// If block-based retrieval fails, fall back to direct S3 path-based retrieval
	} else {
		log.Printf("[HandleDownload] Block storage not available (db=%v, storageManager=%v)", h.db != nil, h.storageManager != nil)
	}

	// Fallback: Try direct S3 path-based retrieval (legacy)
	if h.storage == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage not available"})
		return
	}

	// Build the storage key
	storageKey := fmt.Sprintf("%s/%s%s", token.OrgID, token.RepoID, token.Path)

	// Get the file from S3
	reader, err := h.storage.Get(c.Request.Context(), storageKey)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}
	defer reader.Close()

	// Read content
	content, err := io.ReadAll(reader)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read file"})
		return
	}

	// Set headers for file download
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, "application/octet-stream", content)
}

// getFileFromBlocks retrieves a file by looking up its blocks and concatenating them
// If the library is encrypted, it decrypts the content before returning
func (h *SeafHTTPHandler) getFileFromBlocks(c *gin.Context, token *AccessToken) ([]byte, error) {
	ctx := c.Request.Context()

	// Check if library is encrypted and get file key
	var encrypted bool
	var fileKey []byte
	err := h.db.Session().Query(`
		SELECT encrypted FROM libraries WHERE org_id = ? AND library_id = ?
	`, token.OrgID, token.RepoID).Scan(&encrypted)
	if err == nil && encrypted {
		fileKey = v2.GetDecryptSessions().GetFileKey(token.UserID, token.RepoID)
		if fileKey == nil {
			return nil, fmt.Errorf("library is encrypted but not unlocked")
		}
		log.Printf("[getFileFromBlocks] Library is encrypted, will decrypt content")
	}

	// Get the library's head commit to find the root FS
	var headCommit string
	err = h.db.Session().Query(`
		SELECT head_commit_id FROM libraries
		WHERE org_id = ? AND library_id = ?
	`, token.OrgID, token.RepoID).Scan(&headCommit)
	if err != nil {
		return nil, fmt.Errorf("library not found: %w", err)
	}

	// Get the root FS ID from the commit
	var rootFSID string
	err = h.db.Session().Query(`
		SELECT root_fs_id FROM commits
		WHERE library_id = ? AND commit_id = ?
	`, token.RepoID, headCommit).Scan(&rootFSID)
	if err != nil {
		return nil, fmt.Errorf("commit not found: %w", err)
	}

	// Navigate to the target file through the directory structure
	filePath := token.Path
	if !strings.HasPrefix(filePath, "/") {
		filePath = "/" + filePath
	}

	// Split path into components
	pathParts := strings.Split(strings.Trim(filePath, "/"), "/")
	if len(pathParts) == 0 || (len(pathParts) == 1 && pathParts[0] == "") {
		return nil, fmt.Errorf("invalid file path")
	}

	currentFSID := rootFSID

	// Navigate to the file (all parts except the last are directories)
	for i := 0; i < len(pathParts)-1; i++ {
		dirName := pathParts[i]
		nextFSID, err := h.findEntryInDir(token.RepoID, currentFSID, dirName)
		if err != nil {
			return nil, fmt.Errorf("directory not found: %s: %w", dirName, err)
		}
		currentFSID = nextFSID
	}

	// Find the target file in the current directory
	targetName := pathParts[len(pathParts)-1]
	fileFSID, err := h.findEntryInDir(token.RepoID, currentFSID, targetName)
	if err != nil {
		return nil, fmt.Errorf("file not found: %s: %w", targetName, err)
	}

	// Get the file's block IDs
	var blockIDs []string
	err = h.db.Session().Query(`
		SELECT block_ids FROM fs_objects
		WHERE library_id = ? AND fs_id = ?
	`, token.RepoID, fileFSID).Scan(&blockIDs)
	if err != nil {
		return nil, fmt.Errorf("file metadata not found: %w", err)
	}

	// Get block store from storage manager
	blockStore, _, err := h.storageManager.GetHealthyBlockStore("")
	if err != nil {
		return nil, fmt.Errorf("block store not available: %w", err)
	}

	// Retrieve and concatenate blocks
	var content bytes.Buffer
	for _, blockID := range blockIDs {
		// Translate SHA-1 (40 chars) to SHA-256 (64 chars) if needed
		internalID := blockID
		if len(blockID) == 40 {
			// Look up internal SHA-256 ID from mapping
			var mappedID string
			err := h.db.Session().Query(`
				SELECT internal_id FROM block_id_mappings WHERE org_id = ? AND external_id = ?
			`, token.OrgID, blockID).Scan(&mappedID)
			if err == nil && mappedID != "" {
				log.Printf("[getFileFromBlocks] Resolved block %s → %s", blockID, mappedID)
				internalID = mappedID
			} else {
				log.Printf("[getFileFromBlocks] No mapping for block %s, using as-is", blockID)
			}
		}

		blockData, err := blockStore.GetBlock(ctx, internalID)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve block %s: %w", blockID, err)
		}

		// Decrypt block if library is encrypted
		if fileKey != nil {
			decryptedData, err := crypto.DecryptBlock(blockData, fileKey)
			if err != nil {
				log.Printf("[getFileFromBlocks] Failed to decrypt block %s: %v", blockID, err)
				return nil, fmt.Errorf("failed to decrypt block %s: %w", blockID, err)
			}
			log.Printf("[getFileFromBlocks] Decrypted block %s (%d -> %d bytes)", blockID[:16], len(blockData), len(decryptedData))
			blockData = decryptedData
		}

		content.Write(blockData)
	}

	return content.Bytes(), nil
}

// findEntryInDir finds an entry (file or directory) within a directory FS object
func (h *SeafHTTPHandler) findEntryInDir(repoID, dirFSID, entryName string) (string, error) {
	var dirEntries string
	err := h.db.Session().Query(`
		SELECT dir_entries FROM fs_objects
		WHERE library_id = ? AND fs_id = ?
	`, repoID, dirFSID).Scan(&dirEntries)
	if err != nil {
		return "", fmt.Errorf("directory not found: %w", err)
	}

	log.Printf("[findEntryInDir] Looking for entry '%s' in dir %s", entryName, dirFSID)
	log.Printf("[findEntryInDir] Dir entries length: %d", len(dirEntries))

	// Parse dir_entries - format is JSON array like [{"id":"...","mode":...,"modifier":"...","mtime":...,"name":"...","size":...},...]
	// Find the entry by name, then extract the entire JSON object to get the ID
	searchPattern := fmt.Sprintf(`"name":"%s"`, entryName)
	log.Printf("[findEntryInDir] Search pattern: %s", searchPattern)
	idx := strings.Index(dirEntries, searchPattern)
	log.Printf("[findEntryInDir] Pattern found at index: %d", idx)
	if idx == -1 {
		// Log a snippet of the dir_entries for debugging
		if len(dirEntries) > 500 {
			log.Printf("[findEntryInDir] Dir entries (first 500 chars): %s", dirEntries[:500])
		} else {
			log.Printf("[findEntryInDir] Dir entries: %s", dirEntries)
		}
		return "", fmt.Errorf("entry not found: %s", entryName)
	}

	// Find the start of the JSON object containing this entry (search backwards for '{')
	objectStart := -1
	for i := idx; i >= 0; i-- {
		if dirEntries[i] == '{' {
			objectStart = i
			break
		}
	}
	if objectStart == -1 {
		return "", fmt.Errorf("malformed dir entries: no object start for: %s", entryName)
	}

	// Find the end of the JSON object (search forward for '}')
	objectEnd := -1
	for i := idx; i < len(dirEntries); i++ {
		if dirEntries[i] == '}' {
			objectEnd = i + 1
			break
		}
	}
	if objectEnd == -1 {
		return "", fmt.Errorf("malformed dir entries: no object end for: %s", entryName)
	}

	// Extract just this entry's JSON object
	entryJSON := dirEntries[objectStart:objectEnd]
	log.Printf("[findEntryInDir] Extracted object: %s", entryJSON)

	// Find the "id" field within this object
	idPattern := `"id":"`
	idIdx := strings.Index(entryJSON, idPattern)
	if idIdx == -1 {
		return "", fmt.Errorf("entry ID not found for: %s", entryName)
	}

	// Extract the ID value
	idStart := idIdx + len(idPattern)
	idEnd := strings.Index(entryJSON[idStart:], `"`)
	if idEnd == -1 {
		return "", fmt.Errorf("malformed entry for: %s", entryName)
	}

	entryID := entryJSON[idStart : idStart+idEnd]
	log.Printf("[findEntryInDir] Found entry ID: %s", entryID)

	return entryID, nil
}

// Helper function to generate a file ID
func generateFileID(storageKey string) string {
	bytes := make([]byte, 20)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// bytesReader wraps []byte to implement io.Reader
type bytesReader struct {
	data []byte
	pos  int
}

func newBytesReader(data []byte) *bytesReader {
	return &bytesReader{data: data}
}

func (r *bytesReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
