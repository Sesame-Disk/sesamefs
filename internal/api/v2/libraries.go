package v2

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/config"
	"github.com/Sesame-Disk/sesamefs/internal/crypto"
	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/models"
	"github.com/Sesame-Disk/sesamefs/internal/storage"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// LibraryTokenCreator is an interface for creating sync tokens
type LibraryTokenCreator interface {
	CreateDownloadToken(orgID, repoID, path, userID string) (string, error)
}

// LibraryHandler handles library-related API requests
type LibraryHandler struct {
	db           *db.DB
	config       *config.Config
	tokenCreator LibraryTokenCreator
}

// RegisterLibraryRoutes registers library routes
func RegisterLibraryRoutes(rg *gin.RouterGroup, database *db.DB, cfg *config.Config) {
	RegisterLibraryRoutesWithToken(rg, database, cfg, nil)
}

// RegisterLibraryRoutesWithToken registers library routes with token creator
func RegisterLibraryRoutesWithToken(rg *gin.RouterGroup, database *db.DB, cfg *config.Config, tokenCreator LibraryTokenCreator) {
	h := &LibraryHandler{db: database, config: cfg, tokenCreator: tokenCreator}

	repos := rg.Group("/repos")
	{
		repos.GET("", h.ListLibraries)
		repos.POST("", h.CreateLibrary)
		repos.GET("/:repo_id", h.GetLibrary)
		repos.GET("/:repo_id/", h.GetLibrary)
		repos.PUT("/:repo_id", h.UpdateLibrary)
		repos.PUT("/:repo_id/", h.UpdateLibrary)
		repos.POST("/:repo_id", h.LibraryOperation) // handles op=rename
		repos.POST("/:repo_id/", h.LibraryOperation)
		repos.DELETE("/:repo_id", h.DeleteLibrary)
		repos.DELETE("/:repo_id/", h.DeleteLibrary)
		repos.POST("/:repo_id/storage-class", h.ChangeStorageClass)
	}
}

// RegisterV21LibraryRoutes registers v2.1 library routes with Seahub-compatible response format
func RegisterV21LibraryRoutes(rg *gin.RouterGroup, database *db.DB, cfg *config.Config, tokenCreator LibraryTokenCreator, s3Store *storage.S3Store, blockStore *storage.BlockStore, serverURL string) {
	h := &LibraryHandler{db: database, config: cfg, tokenCreator: tokenCreator}
	fh := &FileHandler{db: database, config: cfg, serverURL: serverURL}
	eh := NewEncryptionHandler(database)

	// Pass storage and blockStore for Office file template creation
	if s3Store != nil {
		fh.storage = s3Store
	}
	if blockStore != nil {
		fh.blockStore = blockStore
	}

	repos := rg.Group("/repos")
	{
		repos.GET("", h.ListLibrariesV21)
		repos.GET("/:repo_id", h.GetLibraryV21)
		repos.DELETE("/:repo_id", h.DeleteLibrary)
		repos.DELETE("/:repo_id/", h.DeleteLibrary)
		repos.GET("/:repo_id/dir", fh.ListDirectoryV21)
		repos.GET("/:repo_id/dir/", fh.ListDirectoryV21)

		// Encrypted library password endpoints
		repos.POST("/:repo_id/set-password", eh.SetPassword)
		repos.POST("/:repo_id/set-password/", eh.SetPassword)
		repos.PUT("/:repo_id/set-password", eh.ChangePassword)
		repos.PUT("/:repo_id/set-password/", eh.ChangePassword)

		// File operations (CRUD)
		repos.GET("/:repo_id/file", fh.GetFileInfo)
		repos.GET("/:repo_id/file/", fh.GetFileInfo)
		repos.DELETE("/:repo_id/file", fh.DeleteFile)
		repos.DELETE("/:repo_id/file/", fh.DeleteFile)
		repos.POST("/:repo_id/file", fh.FileOperation)  // rename, create
		repos.POST("/:repo_id/file/", fh.FileOperation) // rename, create
		repos.PUT("/:repo_id/file", fh.LockFile)        // lock, unlock
		repos.PUT("/:repo_id/file/", fh.LockFile)       // lock, unlock
		repos.GET("/:repo_id/file/detail", fh.GetFileDetail)
		repos.GET("/:repo_id/file/detail/", fh.GetFileDetail)

		// Directory operations
		repos.DELETE("/:repo_id/dir", fh.DeleteDirectory)
		repos.DELETE("/:repo_id/dir/", fh.DeleteDirectory)
		repos.POST("/:repo_id/dir", fh.DirectoryOperation)  // mkdir, rename
		repos.POST("/:repo_id/dir/", fh.DirectoryOperation) // mkdir, rename

		// Move/Copy operations
		repos.POST("/:repo_id/file/move", fh.MoveFile)
		repos.POST("/:repo_id/file/move/", fh.MoveFile)
		repos.POST("/:repo_id/file/copy", fh.CopyFile)
		repos.POST("/:repo_id/file/copy/", fh.CopyFile)

		// Resumable upload support
		repos.GET("/:repo_id/file-uploaded-bytes", fh.GetFileUploadedBytes)
		repos.GET("/:repo_id/file-uploaded-bytes/", fh.GetFileUploadedBytes)

		// Share info endpoint (stub - returns empty shares)
		repos.GET("/:repo_id/share-info", h.GetRepoFolderShareInfo)
		repos.GET("/:repo_id/share-info/", h.GetRepoFolderShareInfo)
	}
}

// ListLibraries returns all libraries for the authenticated user
// This endpoint uses the api2 format expected by Seafile desktop client
// (id, name, mtime) rather than the v2.1 web UI format (repo_id, repo_name, last_modified)
func (h *LibraryHandler) ListLibraries(c *gin.Context) {
	orgID := c.GetString("org_id")
	if orgID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing org_id"})
		return
	}

	if _, err := uuid.Parse(orgID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org_id"})
		return
	}

	// Query libraries from database (use string for UUID binding)
	// Include encryption fields (enc_version, magic, random_key, salt) for encrypted libraries
	iter := h.db.Session().Query(`
		SELECT library_id, owner_id, name, description, encrypted,
			   storage_class, size_bytes, file_count, head_commit_id, created_at, updated_at,
			   enc_version, magic, random_key, salt
		FROM libraries WHERE org_id = ?
	`, orgID).Iter()

	var libraries []gin.H
	var libID, ownerID string
	var name, description, storageClass string
	var headCommitID string
	var encrypted bool
	var sizeBytes, fileCount int64
	var createdAt, updatedAt time.Time
	var encVersion int
	var magic, randomKey, salt string

	for iter.Scan(
		&libID, &ownerID, &name, &description,
		&encrypted, &storageClass, &sizeBytes,
		&fileCount, &headCommitID, &createdAt, &updatedAt,
		&encVersion, &magic, &randomKey, &salt,
	) {
		ownerEmail := ownerID + "@sesamefs.local"

		// Seafile desktop client expects these specific field names:
		// - id (not repo_id)
		// - name (not repo_name)
		// - mtime (not last_modified)
		// - owner (not owner_email)
		// - desc (not description)
		//
		// CRITICAL field formats (verified against stock Seafile):
		// - root: empty string "" (not "0000...000")
		// - salt: always present (empty string "" for unencrypted)
		// - modifier_email, modifier_contact_email, modifier_name: required by desktop client
		lib := gin.H{
			"type":                   "repo",
			"id":                     libID,
			"name":                   name,
			"desc":                   description,
			"owner":                  ownerEmail,
			"owner_name":             strings.Split(ownerEmail, "@")[0],
			"owner_contact_email":    ownerEmail,
			"modifier_email":         ownerEmail, // Desktop client requires these
			"modifier_contact_email": ownerEmail,
			"modifier_name":          strings.Split(ownerEmail, "@")[0],
			"mtime":                  updatedAt.Unix(),
			"mtime_relative":         "", // Optional human-readable time
			"encrypted":              encrypted,
			"permission":             "rw",
			"virtual":                false,
			"root":                   "", // CRITICAL: empty string (stock Seafile format)
			"head_commit_id":         headCommitID,
			"version":                1,
			"size":                   sizeBytes,
			"size_formatted":         formatSize(sizeBytes),
			"salt":                   "", // CRITICAL: always present (stock Seafile format)
			"file_count":             fileCount,
			"storage_id":             storageClass,
			"storage_name":           storageClass,
		}

		// Add encryption fields for encrypted libraries
		// Client needs these to prompt for password
		if encrypted {
			// Return enc_version 2 for Seafile client compatibility (we store 12 for dual-mode)
			lib["enc_version"] = 2
			lib["magic"] = magic
			lib["random_key"] = randomKey
			// Override salt with actual value for encrypted libraries
			if salt != "" {
				lib["salt"] = salt
			}
		}

		libraries = append(libraries, lib)
	}

	if err := iter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list libraries", "details": err.Error()})
		return
	}

	// Return empty array instead of null
	if libraries == nil {
		libraries = []gin.H{}
	}

	c.JSON(http.StatusOK, libraries)
}

// formatSize returns a human-readable size string
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// CreateLibraryRequest represents the request body for creating a library
type CreateLibraryRequest struct {
	Name        string `json:"name" form:"name"`
	Description string `json:"description" form:"desc"` // Seafile uses "desc" in form
	Encrypted   bool   `json:"encrypted" form:"encrypted"`
	Password    string `json:"passwd,omitempty" form:"passwd"` // Seafile uses "passwd" everywhere
}

// CreateLibrary creates a new library
func (h *LibraryHandler) CreateLibrary(c *gin.Context) {
	var req CreateLibraryRequest

	// Try JSON first, then fall back to form data (Seafile desktop uses form data)
	contentType := c.GetHeader("Content-Type")
	if strings.Contains(contentType, "application/json") {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	} else {
		// Form data (application/x-www-form-urlencoded)
		req.Name = c.PostForm("name")
		req.Description = c.PostForm("desc")
		req.Password = c.PostForm("passwd")
		// CRITICAL: Seafile clients don't send 'encrypted' param, only 'passwd'
		// If passwd is provided, library should be encrypted
		req.Encrypted = c.PostForm("encrypted") == "true" || c.PostForm("encrypted") == "1" || req.Password != ""
	}

	// Validate required field
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}

	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	log.Printf("[CreateLibrary] Creating library: name=%q, encrypted=%v, orgID=%q, userID=%q", req.Name, req.Encrypted, orgID, userID)

	// Check if a library with this name already exists for this user
	var existingName string
	iter := h.db.Session().Query(`
		SELECT name FROM libraries WHERE org_id = ? AND owner_id = ? ALLOW FILTERING
	`, orgID, userID).Iter()
	for iter.Scan(&existingName) {
		log.Printf("[CreateLibrary] Found existing library: %q (comparing with %q)", existingName, req.Name)
		if existingName == req.Name {
			iter.Close()
			log.Printf("[CreateLibrary] Conflict: library with name %q already exists", req.Name)
			c.JSON(http.StatusConflict, gin.H{"error": "a library with this name already exists"})
			return
		}
	}
	iter.Close()

	orgUUID, _ := uuid.Parse(orgID)
	userUUID, _ := uuid.Parse(userID)
	newLibID := uuid.New()

	now := time.Now()
	library := models.Library{
		LibraryID:      newLibID,
		OrgID:          orgUUID,
		OwnerID:        userUUID,
		Name:           req.Name,
		Description:    req.Description,
		Encrypted:      req.Encrypted,
		StorageClass:   h.config.Storage.DefaultClass,
		SizeBytes:      0,
		FileCount:      0,
		VersionTTLDays: h.config.Versioning.DefaultTTLDays,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	// Create empty root directory fs_object
	// Seafile uses a specific format for empty directories - the fs_id is the SHA-1 hash
	// of the serialized directory content. For an empty dir, we use a well-known empty dir hash.
	emptyDirEntries := "[]" // Empty JSON array for directory entries
	emptyDirData := fmt.Sprintf("%d\n%s", 1, emptyDirEntries) // version + entries
	emptyDirHash := sha1.Sum([]byte(emptyDirData))
	rootFSID := hex.EncodeToString(emptyDirHash[:])

	// Store empty root directory in fs_objects
	if err := h.db.Session().Query(`
		INSERT INTO fs_objects (library_id, fs_id, obj_type, obj_name, dir_entries, mtime)
		VALUES (?, ?, ?, ?, ?, ?)
	`, newLibID.String(), rootFSID, "dir", "", emptyDirEntries, now.Unix()).Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create root directory", "details": err.Error()})
		return
	}

	// Generate initial commit ID (SHA-1 hash of repo creation data)
	commitData := fmt.Sprintf("%s:%s:%d", newLibID.String(), req.Name, now.UnixNano())
	commitHash := sha1.Sum([]byte(commitData))
	headCommitID := hex.EncodeToString(commitHash[:])

	// Generate encryption params if library is encrypted
	var encParams *crypto.EncryptionParams
	if req.Encrypted {
		if req.Password == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "password is required for encrypted library"})
			return
		}
		var err error
		encParams, err = crypto.CreateEncryptedLibrary(req.Password, newLibID.String())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create encryption params"})
			return
		}
	}

	// Insert into database with head_commit_id and encryption params
	if req.Encrypted && encParams != nil {
		if err := h.db.Session().Query(`
			INSERT INTO libraries (
				org_id, library_id, owner_id, name, description, encrypted,
				enc_version, salt, magic, random_key, magic_strong, random_key_strong,
				storage_class, size_bytes, file_count, version_ttl_days,
				head_commit_id, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, orgID, newLibID.String(), userID, library.Name,
			library.Description, library.Encrypted,
			encParams.EncVersion, encParams.Salt, encParams.Magic, encParams.RandomKey,
			encParams.MagicStrong, encParams.RandomKeyStrong,
			library.StorageClass, library.SizeBytes, library.FileCount, library.VersionTTLDays,
			headCommitID, library.CreatedAt, library.UpdatedAt,
		).Exec(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create library", "details": err.Error()})
			return
		}
	} else {
		if err := h.db.Session().Query(`
			INSERT INTO libraries (
				org_id, library_id, owner_id, name, description, encrypted,
				storage_class, size_bytes, file_count, version_ttl_days,
				head_commit_id, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, orgID, newLibID.String(), userID, library.Name,
			library.Description, library.Encrypted, library.StorageClass,
			library.SizeBytes, library.FileCount, library.VersionTTLDays,
			headCommitID, library.CreatedAt, library.UpdatedAt,
		).Exec(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create library", "details": err.Error()})
			return
		}
	}

	// Create initial commit record with root_fs_id pointing to empty root directory
	if err := h.db.Session().Query(`
		INSERT INTO commits (library_id, commit_id, root_fs_id, creator_id, description, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, newLibID.String(), headCommitID, rootFSID, userID, "Initial commit", now).Exec(); err != nil {
		// Non-fatal - library was created
	}

	// Get user email for response
	userEmail := c.GetString("user_email")
	if userEmail == "" {
		userEmail = userID + "@sesamefs.local"
	}

	// Generate sync token if token creator is available
	syncToken := ""
	if h.tokenCreator != nil {
		token, err := h.tokenCreator.CreateDownloadToken(orgID, newLibID.String(), "/", userID)
		if err == nil {
			syncToken = token
		}
	}

	// Get server port for relay info
	serverPort := "8080"
	if h.config != nil && h.config.Server.Port != "" {
		serverPort = strings.TrimPrefix(h.config.Server.Port, ":")
	}

	// Return Seafile-compatible response (HTTP 200, not 201)
	// This format matches what Seafile returns and includes sync info
	response := gin.H{
		"relay_id":            "localhost",
		"relay_addr":          "localhost",
		"relay_port":          serverPort,
		"email":               userEmail,
		"token":               syncToken,
		"repo_id":             newLibID.String(),
		"repo_name":           req.Name,
		"repo_desc":           req.Description,
		"repo_size":           0,
		"repo_size_formatted": formatSizeSeafile(0),
		"mtime":               now.Unix(),
		"mtime_relative":      formatRelativeTimeHTML(now),
		"encrypted":           false,
		"enc_version":         0,
		"salt":                "",
		"magic":               "",
		"random_key":          "",
		"repo_version":        1,
		"head_commit_id":      headCommitID,
		"permission":          "rw",
	}

	// Set encrypted fields if library is encrypted
	// Translate enc_version for Seafile desktop client compatibility
	if req.Encrypted && encParams != nil {
		response["encrypted"] = 1 // Seafile uses 1 for encrypted (not true)
		// Translate enc_version 12 (dual-mode) to 2 for Seafile client
		clientEncVersion := encParams.EncVersion
		if clientEncVersion == 12 || clientEncVersion == 10 {
			clientEncVersion = 2
		}
		response["enc_version"] = clientEncVersion
		// CRITICAL: For Seafile v2, salt must be empty string (uses static hardcoded salt)
		// Don't expose internal Argon2id salt to Seafile clients
		response["salt"] = ""
		response["magic"] = encParams.Magic
		response["random_key"] = encParams.RandomKey
	}

	c.JSON(http.StatusOK, response)
}

// GetLibrary returns a single library by ID
// This endpoint uses the api2 format expected by Seafile desktop client
func (h *LibraryHandler) GetLibrary(c *gin.Context) {
	repoID := c.Param("repo_id")
	orgID := c.GetString("org_id")

	if _, err := uuid.Parse(repoID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid repo_id"})
		return
	}

	var libID, ownerID string
	var name, description, storageClass string
	var headCommitID string
	var encrypted bool
	var encVersion int
	var salt, magic, randomKey string
	var sizeBytes, fileCount int64
	var versionTTLDays int
	var createdAt, updatedAt time.Time

	if err := h.db.Session().Query(`
		SELECT library_id, owner_id, name, description, encrypted,
			   enc_version, salt, magic, random_key,
			   storage_class, size_bytes, file_count, version_ttl_days,
			   head_commit_id, created_at, updated_at
		FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, repoID).Scan(
		&libID, &ownerID, &name, &description,
		&encrypted, &encVersion, &salt, &magic, &randomKey,
		&storageClass, &sizeBytes,
		&fileCount, &versionTTLDays, &headCommitID, &createdAt, &updatedAt,
	); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "library not found"})
		return
	}

	ownerEmail := ownerID + "@sesamefs.local"

	// Return api2 format for Seafile desktop client compatibility
	response := gin.H{
		"id":                  libID,
		"name":                name,
		"desc":                description,
		"owner":               ownerEmail,
		"owner_email":         ownerEmail, // Used by share dialog
		"owner_name":          strings.Split(ownerEmail, "@")[0],
		"owner_contact_email": ownerEmail,
		"mtime":               updatedAt.Unix(),
		"mtime_relative":      "",
		"encrypted":           encrypted,
		"permission":          "rw",
		"virtual":             false,
		"root":                "0000000000000000000000000000000000000000",
		"head_commit_id":      headCommitID,
		"version":             1,
		"type":                "repo",
		"size":                sizeBytes,
		"size_formatted":      formatSize(sizeBytes),
		"file_count":          fileCount,
		"storage_id":          storageClass,
		"storage_name":        storageClass,
	}

	// Add encryption fields if library is encrypted
	// Translate enc_version for Seafile desktop client compatibility
	if encrypted {
		clientEncVersion := encVersion
		if encVersion == 12 || encVersion == 10 {
			clientEncVersion = 2
		}
		response["enc_version"] = clientEncVersion
		response["salt"] = salt
		response["magic"] = magic
		response["random_key"] = randomKey
	}

	c.JSON(http.StatusOK, response)
}

// UpdateLibraryRequest represents the request body for updating a library
type UpdateLibraryRequest struct {
	Name           *string `json:"name,omitempty"`
	Description    *string `json:"description,omitempty"`
	VersionTTLDays *int    `json:"version_ttl_days,omitempty"`
}

// UpdateLibrary updates a library's properties
func (h *LibraryHandler) UpdateLibrary(c *gin.Context) {
	repoID := c.Param("repo_id")
	orgID := c.GetString("org_id")

	var req UpdateLibraryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Build dynamic update query
	updates := []string{}
	values := []interface{}{}

	if req.Name != nil {
		updates = append(updates, "name = ?")
		values = append(values, *req.Name)
	}
	if req.Description != nil {
		updates = append(updates, "description = ?")
		values = append(values, *req.Description)
	}
	if req.VersionTTLDays != nil {
		if *req.VersionTTLDays < h.config.Versioning.MinTTLDays && *req.VersionTTLDays != 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "version_ttl_days must be 0 (forever) or >= min_ttl_days",
			})
			return
		}
		updates = append(updates, "version_ttl_days = ?")
		values = append(values, *req.VersionTTLDays)
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no updates provided"})
		return
	}

	updates = append(updates, "updated_at = ?")
	values = append(values, time.Now())
	values = append(values, orgID, repoID) // Use strings for UUIDs

	query := "UPDATE libraries SET "
	for i, u := range updates {
		if i > 0 {
			query += ", "
		}
		query += u
	}
	query += " WHERE org_id = ? AND library_id = ?"

	if err := h.db.Session().Query(query, values...).Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update library"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// DeleteLibrary deletes a library
func (h *LibraryHandler) DeleteLibrary(c *gin.Context) {
	repoID := c.Param("repo_id")
	orgID := c.GetString("org_id")

	// Validate inputs
	if orgID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing org_id"})
		return
	}

	if repoID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing repo_id"})
		return
	}

	// Verify library exists before deleting
	var libID string
	err := h.db.Session().Query(`
		SELECT library_id FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, repoID).Scan(&libID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "library not found"})
		return
	}

	// TODO: Delete all files, blocks, commits, etc.
	// For now, just delete the library record

	if err := h.db.Session().Query(`
		DELETE FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, repoID).Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete library"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// LibraryOperation handles POST operations on a library based on 'op' query parameter
// Implements Seafile API: POST /api2/repos/:repo_id/?op=rename
func (h *LibraryHandler) LibraryOperation(c *gin.Context) {
	op := c.Query("op")

	switch op {
	case "rename":
		h.RenameLibrary(c)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported operation: " + op})
	}
}

// RenameLibraryRequest represents the request body for renaming a library
type RenameLibraryRequest struct {
	RepoName string `json:"repo_name" form:"repo_name"`
}

// RenameLibrary renames a library
// Implements Seafile API: POST /api2/repos/:repo_id/?op=rename
func (h *LibraryHandler) RenameLibrary(c *gin.Context) {
	repoID := c.Param("repo_id")
	orgID := c.GetString("org_id")

	var req RenameLibraryRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.RepoName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_name is required"})
		return
	}

	if err := h.db.Session().Query(`
		UPDATE libraries SET name = ?, updated_at = ?
		WHERE org_id = ? AND library_id = ?
	`, req.RepoName, time.Now(), orgID, repoID).Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to rename library"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ChangeStorageClassRequest represents the request body for changing storage class
type ChangeStorageClassRequest struct {
	StorageClass string `json:"storage_class" binding:"required"`
}

// ChangeStorageClass changes a library's storage class
func (h *LibraryHandler) ChangeStorageClass(c *gin.Context) {
	repoID := c.Param("repo_id")
	orgID := c.GetString("org_id")

	var req ChangeStorageClassRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate storage class
	if _, ok := h.config.Storage.Backends[req.StorageClass]; !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid storage class"})
		return
	}

	if err := h.db.Session().Query(`
		UPDATE libraries SET storage_class = ?, updated_at = ?
		WHERE org_id = ? AND library_id = ?
	`, req.StorageClass, time.Now(), orgID, repoID).Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update storage class"})
		return
	}

	// TODO: Trigger background job to migrate blocks to new storage class

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GetRepoFolderShareInfo returns share information for a folder in a repository
// GET /api/v2.1/repos/:repo_id/share-info?path=/folder
// For now, this is a stub that returns empty shares
func (h *LibraryHandler) GetRepoFolderShareInfo(c *gin.Context) {
	// Return empty share info - folder is not shared to anyone
	// Full implementation would query the shares table
	c.JSON(http.StatusOK, gin.H{
		"shared_user_emails": []string{},
		"shared_group_ids":   []int{},
	})
}

// V21Library represents a library in v2.1 API format
// This format uses different field names and ISO date format for Seahub frontend compatibility
type V21Library struct {
	Type                 string `json:"type"`
	RepoID               string `json:"repo_id"`
	RepoName             string `json:"repo_name"`
	OwnerEmail           string `json:"owner_email"`
	OwnerName            string `json:"owner_name"`
	OwnerContactEmail    string `json:"owner_contact_email"`
	LastModified         string `json:"last_modified"` // ISO 8601 format
	ModifierEmail        string `json:"modifier_email"`
	ModifierName         string `json:"modifier_name"`
	ModifierContactEmail string `json:"modifier_contact_email"`
	Size                 int64  `json:"size"`
	Encrypted            bool   `json:"encrypted"`
	LibNeedDecrypt       bool   `json:"lib_need_decrypt"`
	Permission           string `json:"permission"`
	Starred              bool   `json:"starred"`
	Monitored            bool   `json:"monitored"`
	Status               string `json:"status"`
	Salt                 string `json:"salt"`
	StorageName          string `json:"storage_name,omitempty"`
}

// V21LibraryResponse represents the v2.1 API response for listing libraries
type V21LibraryResponse struct {
	Repos []V21Library `json:"repos"`
}

// ListLibrariesV21 returns all libraries in v2.1 API format for Seahub frontend
func (h *LibraryHandler) ListLibrariesV21(c *gin.Context) {
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")
	if orgID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing org_id"})
		return
	}

	if _, err := uuid.Parse(orgID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org_id"})
		return
	}

	// Query starred libraries for this user (path="/" means the library itself)
	// Note: We query all starred items and filter for path="/" in Go because Cassandra's
	// primary key ((user_id), repo_id, path) doesn't allow filtering by path alone
	starredLibs := make(map[string]bool)
	if userID != "" {
		starIter := h.db.Session().Query(`
			SELECT repo_id, path FROM starred_files WHERE user_id = ?
		`, userID).Iter()
		var starredRepoID, starredPath string
		for starIter.Scan(&starredRepoID, &starredPath) {
			if starredPath == "/" {
				starredLibs[starredRepoID] = true
			}
		}
		starIter.Close()
	}

	// Query libraries from database
	iter := h.db.Session().Query(`
		SELECT library_id, owner_id, name, description, encrypted,
			   storage_class, size_bytes, file_count, created_at, updated_at
		FROM libraries WHERE org_id = ?
	`, orgID).Iter()

	var libraries []V21Library
	var libID, ownerID string
	var name, description, storageClass string
	var encrypted bool
	var sizeBytes, fileCount int64
	var createdAt, updatedAt time.Time

	for iter.Scan(
		&libID, &ownerID, &name, &description,
		&encrypted, &storageClass, &sizeBytes,
		&fileCount, &createdAt, &updatedAt,
	) {
		// Generate owner email
		ownerEmail := ownerID + "@sesamefs.local"

		// Determine library type (mine, shared, public)
		libType := "mine"
		if ownerID != userID {
			libType = "shared"
		}

		// Check if this library is starred
		isStarred := starredLibs[libID]

		// Check if encrypted library needs decryption (not yet unlocked by user)
		libNeedDecrypt := false
		if encrypted && userID != "" {
			libNeedDecrypt = !GetDecryptSessions().IsUnlocked(userID, libID)
		}

		libraries = append(libraries, V21Library{
			Type:                 libType,
			RepoID:               libID,
			RepoName:             name,
			OwnerEmail:           ownerEmail,
			OwnerName:            strings.Split(ownerEmail, "@")[0], // Extract name from email
			OwnerContactEmail:    ownerEmail,
			LastModified:         updatedAt.Format(time.RFC3339), // ISO 8601 format
			ModifierEmail:        ownerEmail,
			ModifierName:         strings.Split(ownerEmail, "@")[0],
			ModifierContactEmail: ownerEmail,
			Size:                 sizeBytes,
			Encrypted:            encrypted,
			LibNeedDecrypt:       libNeedDecrypt,
			Permission:           "rw", // TODO: Check actual permissions
			Starred:              isStarred,
			Monitored:            false,
			Status:               "normal",
			Salt:                 "",
			StorageName:          storageClass,
		})
	}

	if err := iter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list libraries", "details": err.Error()})
		return
	}

	// Return empty array instead of null
	if libraries == nil {
		libraries = []V21Library{}
	}

	c.JSON(http.StatusOK, V21LibraryResponse{Repos: libraries})
}

// GetLibraryV21 returns a single library in v2.1 API format
func (h *LibraryHandler) GetLibraryV21(c *gin.Context) {
	repoID := c.Param("repo_id")
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	if _, err := uuid.Parse(repoID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid repo_id"})
		return
	}

	var libID, ownerID string
	var name, description, storageClass string
	var encrypted bool
	var sizeBytes, fileCount int64
	var headCommitID string
	var createdAt, updatedAt time.Time

	if err := h.db.Session().Query(`
		SELECT library_id, owner_id, name, description, encrypted,
			   storage_class, size_bytes, file_count, head_commit_id,
			   created_at, updated_at
		FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, repoID).Scan(
		&libID, &ownerID, &name, &description,
		&encrypted, &storageClass, &sizeBytes,
		&fileCount, &headCommitID, &createdAt, &updatedAt,
	); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "library not found"})
		return
	}

	// Generate owner email
	ownerEmail := ownerID + "@sesamefs.local"

	// Check if this library is starred by the user
	isStarred := false
	if userID != "" {
		var starredAt time.Time
		err := h.db.Session().Query(`
			SELECT starred_at FROM starred_files WHERE user_id = ? AND repo_id = ? AND path = ?
		`, userID, libID, "/").Scan(&starredAt)
		isStarred = (err == nil)
	}

	// Check if encrypted library needs decryption
	libNeedDecrypt := false
	if encrypted && userID != "" {
		// Check if user has unlocked this library
		libNeedDecrypt = !GetDecryptSessions().IsUnlocked(userID, libID)
	}

	// Return v2.1 format response (matches Seafile's /api/v2.1/repos/:id/ format)
	response := gin.H{
		"repo_id":             libID,
		"repo_name":           name,
		"owner_email":         ownerEmail,
		"owner_name":          strings.Split(ownerEmail, "@")[0],
		"owner_contact_email": ownerEmail,
		"size":                sizeBytes,
		"encrypted":           encrypted,
		"file_count":          fileCount,
		"permission":          "rw",
		"no_quota":            true,
		"is_admin":            true,
		"is_virtual":          false,
		"has_been_shared_out": false,
		"lib_need_decrypt":    libNeedDecrypt,
		"last_modified":       updatedAt.Format(time.RFC3339),
		"status":              "normal",
		"starred":             isStarred,
	}

	c.JSON(http.StatusOK, response)
}
