package v2

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/middleware"
	"github.com/Sesame-Disk/sesamefs/internal/models"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// ShareLinkHandler handles share link API requests
type ShareLinkHandler struct {
	db             *db.DB
	serverURL      string
	permMiddleware *middleware.PermissionMiddleware
}

// NewShareLinkHandler creates a new ShareLinkHandler
func NewShareLinkHandler(database *db.DB, serverURL string, permMiddleware *middleware.PermissionMiddleware) *ShareLinkHandler {
	return &ShareLinkHandler{db: database, serverURL: serverURL, permMiddleware: permMiddleware}
}

// ShareLink represents a share link in API response
type ShareLink struct {
	Token       string `json:"token"`
	RepoID      string `json:"repo_id"`
	RepoName    string `json:"repo_name"`
	Path        string `json:"path"`
	IsDir       bool   `json:"is_dir"`
	IsExpired   bool   `json:"is_expired"`
	ObjID       string `json:"obj_id,omitempty"`
	ObjName     string `json:"obj_name"`
	ViewCount   int    `json:"view_cnt"`
	CTime       string `json:"ctime"`
	ExpireDate  string `json:"expire_date,omitempty"`
	CanEdit     bool   `json:"can_edit"`
	CanDownload bool   `json:"can_download"`
	Permissions Perms  `json:"permissions"`
	UserEmail   string `json:"username"`
	CreatorName string `json:"creator_name"`
	LinkURL     string `json:"link,omitempty"`
	IsOwner     bool   `json:"is_owner"`
	Password    string `json:"password,omitempty"`
	HasPassword bool   `json:"has_password"`
}

// Perms represents permission settings for share links
type Perms struct {
	CanEdit     bool `json:"can_edit"`
	CanDownload bool `json:"can_download"`
	CanUpload   bool `json:"can_upload"`
}

// RegisterShareLinkRoutes registers share link routes
func RegisterShareLinkRoutes(rg *gin.RouterGroup, database *db.DB, serverURL string, permMiddleware *middleware.PermissionMiddleware) *ShareLinkHandler {
	h := NewShareLinkHandler(database, serverURL, permMiddleware)

	shareLinks := rg.Group("/share-links")
	{
		shareLinks.GET("", h.ListShareLinks)
		shareLinks.GET("/", h.ListShareLinks)
		shareLinks.POST("", h.CreateShareLink)
		shareLinks.POST("/", h.CreateShareLink)
		shareLinks.PUT("/:token", h.UpdateShareLink)
		shareLinks.PUT("/:token/", h.UpdateShareLink)
		shareLinks.DELETE("", h.BatchDeleteShareLinks)
		shareLinks.DELETE("/", h.BatchDeleteShareLinks)
		shareLinks.DELETE("/:token", h.DeleteShareLink)
		shareLinks.DELETE("/:token/", h.DeleteShareLink)
	}

	// Multi-share-links: Seafile frontend uses this endpoint for creating share links
	// that can be accessed multiple times. Functionally the same as regular share links.
	multiShareLinks := rg.Group("/multi-share-links")
	{
		multiShareLinks.GET("", h.ListShareLinks)
		multiShareLinks.GET("/", h.ListShareLinks)
		multiShareLinks.POST("", h.CreateShareLink)
		multiShareLinks.POST("/", h.CreateShareLink)
		multiShareLinks.POST("/batch", h.BatchCreateShareLinks)
		multiShareLinks.POST("/batch/", h.BatchCreateShareLinks)
		multiShareLinks.PUT("/:token", h.UpdateShareLink)
		multiShareLinks.PUT("/:token/", h.UpdateShareLink)
		multiShareLinks.DELETE("/:token", h.DeleteShareLink)
		multiShareLinks.DELETE("/:token/", h.DeleteShareLink)
	}

	// Repo-specific share links (used by frontend file detail panel)
	repoShareLinks := rg.Group("/repos/:repo_id/share-links")
	{
		repoShareLinks.GET("", h.ListRepoShareLinks)
		repoShareLinks.GET("/", h.ListRepoShareLinks)
	}

	return h
}

// ListShareLinks returns share links for a file or all share links
// Implements: GET /api/v2.1/share-links/?repo_id=xxx&path=/xxx
func (h *ShareLinkHandler) ListShareLinks(c *gin.Context) {
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")
	repoIDFilter := c.Query("repo_id")
	pathFilter := c.Query("path")

	// Parse UUIDs - convert to gocql.UUID for Cassandra
	orgUUID, err := gocql.ParseUUID(orgID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org_id"})
		return
	}
	userUUID, err := gocql.ParseUUID(userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user_id"})
		return
	}

	// Query all share links for this user (Cassandra requires filtering by partition key only)
	query := `
		SELECT share_token, library_id, file_path, permission, expires_at, download_count, max_downloads, created_at, has_password
		FROM share_links_by_creator
		WHERE org_id = ? AND created_by = ?
	`

	iter := h.db.Session().Query(query, orgUUID, userUUID).Iter()

	var links []ShareLink
	var token, libID, filePath, permission string
	var expiresAt *time.Time
	var downloadCount int
	var maxDownloads *int
	var createdAt time.Time
	var hasPassword bool

	// Get user email and name once (using gocql UUIDs)
	var userEmail, userName string
	if err := h.db.Session().Query(`SELECT email, name FROM users WHERE org_id = ? AND user_id = ?`, orgUUID, userUUID).Scan(&userEmail, &userName); err != nil || userEmail == "" {
		userEmail = userID // Fallback to ID if email not found
	}
	if userName == "" {
		userName = userEmail
	}

	for iter.Scan(&token, &libID, &filePath, &permission, &expiresAt, &downloadCount, &maxDownloads, &createdAt, &hasPassword) {
		// Client-side filtering by repo_id and path (if specified)
		if repoIDFilter != "" && libID != repoIDFilter {
			continue
		}
		if pathFilter != "" && filePath != pathFilter {
			continue
		}

		isExpired := false
		if expiresAt != nil && time.Now().After(*expiresAt) {
			isExpired = true
		}

		expireDate := ""
		if expiresAt != nil {
			expireDate = expiresAt.Format(time.RFC3339)
		}

		// Get library name (requires org_id + library_id for partition key)
		var repoName string
		libUUID, parseErr := gocql.ParseUUID(libID)
		if parseErr == nil {
			h.db.Session().Query(`SELECT name FROM libraries WHERE org_id = ? AND library_id = ?`, orgUUID, libUUID).Scan(&repoName)
		}
		if repoName == "" {
			repoName = "Unknown Library"
		}

		// Determine permissions (handle both string and legacy JSON format)
		var canEdit, canDownload, canUpload bool
		if strings.HasPrefix(permission, "{") {
			// Legacy JSON format stored in DB: parse it
			var perms struct {
				CanEdit     bool `json:"can_edit"`
				CanDownload bool `json:"can_download"`
				CanUpload   bool `json:"can_upload"`
			}
			if err := json.Unmarshal([]byte(permission), &perms); err == nil {
				canEdit = perms.CanEdit
				canDownload = perms.CanDownload
				canUpload = perms.CanUpload
			}
		} else {
			canEdit = permission == "edit" || permission == "upload"
			canDownload = permission == "download" || permission == "preview_download" || permission == "edit"
			canUpload = permission == "upload" || permission == "edit"
		}

		// Derive obj_name from path (just the last component)
		objName := filePath
		if filePath == "/" {
			objName = repoName
		} else if idx := strings.LastIndex(strings.TrimSuffix(filePath, "/"), "/"); idx >= 0 {
			objName = strings.TrimSuffix(filePath, "/")[idx+1:]
		}

		links = append(links, ShareLink{
			Token:       token,
			RepoID:      libID,
			RepoName:    repoName,
			Path:        filePath,
			IsDir:       filePath == "/" || strings.HasSuffix(filePath, "/"),
			IsExpired:   isExpired,
			ObjName:     objName,
			ViewCount:   downloadCount,
			CTime:       createdAt.Format(time.RFC3339),
			ExpireDate:  expireDate,
			CanEdit:     canEdit,
			CanDownload: canDownload,
			Permissions: Perms{
				CanEdit:     canEdit,
				CanDownload: canDownload,
				CanUpload:   canUpload,
			},
			UserEmail:   userEmail,
			CreatorName: userName,
			LinkURL:     fmt.Sprintf("%s/d/%s", getBrowserURL(c, h.serverURL), token),
			IsOwner:     true,
			HasPassword: hasPassword,
		})
	}

	if err := iter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to list share links: %v", err)})
		return
	}

	if links == nil {
		links = []ShareLink{}
	}

	// In-memory pagination
	if pageStr := c.Query("page"); pageStr != "" {
		page, _ := strconv.Atoi(pageStr)
		perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "25"))
		if page < 1 {
			page = 1
		}
		if perPage < 1 {
			perPage = 25
		}
		start := (page - 1) * perPage
		if start >= len(links) {
			links = []ShareLink{}
		} else {
			end := start + perPage
			if end > len(links) {
				end = len(links)
			}
			links = links[start:end]
		}
	}

	c.JSON(http.StatusOK, links)
}

// ShareLinkCreateRequest represents the request for creating a share link
type ShareLinkCreateRequest struct {
	RepoID         string `json:"repo_id" form:"repo_id"`
	Path           string `json:"path" form:"path"`
	Password       string `json:"password" form:"password"`
	ExpireDays     int    `json:"expire_days" form:"expire_days"`
	ExpirationTime string `json:"expiration_time" form:"expiration_time"`
	Permissions    string `json:"permissions" form:"permissions"` // "preview_download", "preview_only", etc.
}

// CreateShareLink creates a new share link
// Implements: POST /api/v2.1/share-links/
func (h *ShareLinkHandler) CreateShareLink(c *gin.Context) {
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	var req ShareLinkCreateRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.RepoID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_id is required"})
		return
	}

	if req.Path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
		return
	}

	// Validate repo exists and user has access
	repoUUID, err := uuid.Parse(req.RepoID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid repo_id"})
		return
	}

	// PERMISSION CHECK: User must have at least read access to the library
	if h.permMiddleware != nil {
		hasAccess, err := h.permMiddleware.HasLibraryAccess(orgID, userID, req.RepoID, middleware.PermissionR)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permissions"})
			return
		}
		if !hasAccess {
			c.JSON(http.StatusForbidden, gin.H{"error": "you do not have access to this library"})
			return
		}
	}

	// CUSTOM PERMISSION CHECK: download_external_link flag
	if h.permMiddleware != nil && !h.permMiddleware.RequirePermFlagForRepo(c, req.RepoID, "download_external_link") {
		c.JSON(http.StatusForbidden, gin.H{"error": "generating share links is not allowed by your permission"})
		return
	}

	// Default permission based on Seafile's "permissions" parameter
	// "preview_download", "preview_only", "download", "upload", "edit"
	// The frontend may send a JSON object like {"can_edit":false,"can_download":true,"can_upload":false}
	// or a simple string like "preview_download". Parse JSON if present.
	permission := req.Permissions
	if permission == "" {
		permission = "download"
	} else if strings.HasPrefix(permission, "{") {
		// Parse JSON permissions object and convert to canonical string
		var perms struct {
			CanEdit     bool `json:"can_edit"`
			CanDownload bool `json:"can_download"`
			CanUpload   bool `json:"can_upload"`
		}
		if err := json.Unmarshal([]byte(permission), &perms); err == nil {
			if perms.CanEdit {
				permission = "edit"
			} else if perms.CanUpload && perms.CanDownload {
				permission = "upload"
			} else if perms.CanUpload {
				permission = "upload"
			} else if perms.CanDownload {
				permission = "preview_download"
			} else {
				permission = "preview_only"
			}
		}
	}

	// Generate secure token
	token, err := generateSecureShareToken(16)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	// Hash password if provided
	var passwordHash string
	if req.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
			return
		}
		passwordHash = string(hash)
	}

	// Calculate expiration - support both expiration_time (ISO string) and expire_days (int)
	var expiresAt *time.Time
	if req.ExpirationTime != "" {
		if t, err := time.Parse(time.RFC3339, req.ExpirationTime); err == nil {
			expiresAt = &t
		} else if t, err := time.Parse("2006-01-02", req.ExpirationTime); err == nil {
			expiresAt = &t
		}
	} else if req.ExpireDays > 0 {
		exp := time.Now().AddDate(0, 0, req.ExpireDays)
		expiresAt = &exp
	}

	now := time.Now()
	orgUUID, _ := uuid.Parse(orgID)
	userUUID, _ := uuid.Parse(userID)

	link := models.ShareLink{
		Token:        token,
		OrgID:        orgUUID,
		LibraryID:    repoUUID,
		Path:         req.Path,
		CreatedBy:    userUUID,
		Permission:   permission,
		PasswordHash: passwordHash,
		ExpiresAt:    expiresAt,
		CreatedAt:    now,
	}

	// Insert into database with dual-write pattern
	batch := h.db.Session().Batch(gocql.LoggedBatch)

	// Main table
	batch.Query(`
		INSERT INTO share_links (
			share_token, org_id, library_id, file_path, created_by, permission,
			password_hash, expires_at, download_count, max_downloads, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, link.Token, orgID, req.RepoID, link.Path, userID,
		link.Permission, link.PasswordHash, link.ExpiresAt, 0, nil, link.CreatedAt,
	)

	// Lookup table for querying by creator
	batch.Query(`
		INSERT INTO share_links_by_creator (
			org_id, created_by, share_token, library_id, file_path, permission,
			expires_at, download_count, max_downloads, created_at, has_password
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, orgID, userID, link.Token, req.RepoID, link.Path,
		link.Permission, link.ExpiresAt, 0, nil, link.CreatedAt, req.Password != "",
	)

	if err := batch.Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create share link"})
		return
	}

	// Get library name for response
	var repoName string
	h.db.Session().Query(`SELECT name FROM libraries WHERE library_id = ?`, req.RepoID).Scan(&repoName)
	if repoName == "" {
		repoName = "Unknown Library"
	}

	// Determine permissions
	canEdit := permission == "edit" || permission == "upload"
	canDownload := permission == "download" || permission == "preview_download" || permission == "edit"
	canUpload := permission == "upload" || permission == "edit"

	expireDate := ""
	if expiresAt != nil {
		expireDate = expiresAt.Format(time.RFC3339)
	}

	// Derive obj_name from path
	createObjName := req.Path
	if req.Path == "/" {
		createObjName = repoName
	} else if idx := strings.LastIndex(strings.TrimSuffix(req.Path, "/"), "/"); idx >= 0 {
		createObjName = strings.TrimSuffix(req.Path, "/")[idx+1:]
	}

	// Get creator name for response
	orgUUIDCreate, _ := gocql.ParseUUID(orgID)
	userUUIDCreate, _ := gocql.ParseUUID(userID)
	var createUserEmail, createUserName string
	if err := h.db.Session().Query(`SELECT email, name FROM users WHERE org_id = ? AND user_id = ?`, orgUUIDCreate, userUUIDCreate).Scan(&createUserEmail, &createUserName); err != nil || createUserEmail == "" {
		createUserEmail = userID
	}
	if createUserName == "" {
		createUserName = createUserEmail
	}

	// Return Seafile-compatible response
	c.JSON(http.StatusOK, ShareLink{
		Token:       token,
		RepoID:      req.RepoID,
		RepoName:    repoName,
		Path:        req.Path,
		IsDir:       req.Path == "/" || strings.HasSuffix(req.Path, "/"),
		IsExpired:   false,
		ObjName:     createObjName,
		ViewCount:   0,
		CTime:       now.Format(time.RFC3339),
		ExpireDate:  expireDate,
		CanEdit:     canEdit,
		CanDownload: canDownload,
		Permissions: Perms{
			CanEdit:     canEdit,
			CanDownload: canDownload,
			CanUpload:   canUpload,
		},
		UserEmail:   createUserEmail,
		CreatorName: createUserName,
		LinkURL:     fmt.Sprintf("%s/d/%s", getBrowserURL(c, h.serverURL), token),
		IsOwner:     true,
		Password:    req.Password,
		HasPassword: req.Password != "",
	})
}

// DeleteShareLink deletes a share link
// Implements: DELETE /api/v2.1/share-links/:token/
func (h *ShareLinkHandler) DeleteShareLink(c *gin.Context) {
	token := c.Param("token")
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	// Verify ownership before deleting
	var createdBy, libID, filePath string
	if err := h.db.Session().Query(`
		SELECT created_by, library_id, file_path FROM share_links WHERE share_token = ?
	`, token).Scan(&createdBy, &libID, &filePath); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "share link not found"})
		return
	}

	if createdBy != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "not authorized to delete this share link"})
		return
	}

	// Delete from both tables
	batch := h.db.Session().Batch(gocql.LoggedBatch)

	batch.Query(`DELETE FROM share_links WHERE share_token = ?`, token)
	batch.Query(`DELETE FROM share_links_by_creator WHERE org_id = ? AND created_by = ? AND share_token = ?`,
		orgID, userID, token)

	if err := batch.Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete share link"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ListRepoShareLinks returns share links for a specific repo.
// Implements: GET /api/v2.1/repos/:repo_id/share-links/
// This filters the user's share links by the repo_id from the URL path.
func (h *ShareLinkHandler) ListRepoShareLinks(c *gin.Context) {
	repoID := c.Param("repo_id")
	// Set repo_id as query param so ListShareLinks can filter by it
	c.Request.URL.RawQuery = fmt.Sprintf("repo_id=%s&%s", repoID, c.Request.URL.RawQuery)
	h.ListShareLinks(c)
}

// generateSecureShareToken generates a URL-safe random token
func generateSecureShareToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	// Base64 encodes to ~4/3 the original length, return without padding
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// UpdateShareLink updates a share link's permissions and/or expiration
// Implements: PUT /api/v2.1/share-links/:token/
// seafile-js sends: permissions (JSON string), expiration_time (ISO date)
func (h *ShareLinkHandler) UpdateShareLink(c *gin.Context) {
	token := c.Param("token")
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	// Look up existing share link to verify ownership
	var createdBy, libID, filePath, currentPermission string
	var currentExpiresAt *time.Time
	var downloadCount int
	var maxDownloads *int
	var createdAt time.Time

	if err := h.db.Session().Query(`
		SELECT created_by, library_id, file_path, permission, expires_at, download_count, max_downloads, created_at
		FROM share_links WHERE share_token = ?
	`, token).Scan(&createdBy, &libID, &filePath, &currentPermission, &currentExpiresAt, &downloadCount, &maxDownloads, &createdAt); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "share link not found"})
		return
	}

	if createdBy != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "not authorized to update this share link"})
		return
	}

	// Parse update fields from form data
	permissionsJSON := c.PostForm("permissions")
	expirationTime := c.PostForm("expiration_time")
	newPasswordPlain := c.PostForm("password") // "" = no change, "__remove__" = remove password

	newPermission := currentPermission
	newExpiresAt := currentExpiresAt

	// Fetch current password hash so we can include has_password in response
	var currentPasswordHash string
	h.db.Session().Query(`SELECT password_hash FROM share_links WHERE share_token = ?`, token).Scan(&currentPasswordHash)

	// Parse permissions JSON: {"can_edit":false,"can_download":true,"can_upload":false}
	if permissionsJSON != "" {
		var perms Perms
		if err := json.Unmarshal([]byte(permissionsJSON), &perms); err == nil {
			// Map permissions struct back to permission string
			if perms.CanEdit {
				newPermission = "edit"
			} else if perms.CanUpload {
				newPermission = "upload"
			} else if perms.CanDownload {
				newPermission = "preview_download"
			} else {
				newPermission = "preview_only"
			}
		}
	}

	// Parse expiration time
	if expirationTime != "" {
		// Try RFC3339 format first, then other common formats
		if t, err := time.Parse(time.RFC3339, expirationTime); err == nil {
			newExpiresAt = &t
		} else if t, err := time.Parse("2006-01-02", expirationTime); err == nil {
			newExpiresAt = &t
		}
	}

	orgUUID, _ := gocql.ParseUUID(orgID)
	userUUID, _ := gocql.ParseUUID(userID)

	// Handle password change
	newPasswordHash := currentPasswordHash
	returnedPassword := "" // only returned when a new password is set
	if newPasswordPlain == "__remove__" {
		newPasswordHash = ""
	} else if newPasswordPlain != "" {
		hashed, err := bcrypt.GenerateFromPassword([]byte(newPasswordPlain), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
			return
		}
		newPasswordHash = string(hashed)
		returnedPassword = newPasswordPlain
	}

	// Update both tables (dual-write)
	batch := h.db.Session().Batch(gocql.LoggedBatch)

	batch.Query(`
		UPDATE share_links SET permission = ?, expires_at = ?, password_hash = ? WHERE share_token = ?
	`, newPermission, newExpiresAt, newPasswordHash, token)

	batch.Query(`
		UPDATE share_links_by_creator SET permission = ?, expires_at = ?, has_password = ?
		WHERE org_id = ? AND created_by = ? AND share_token = ?
	`, newPermission, newExpiresAt, newPasswordHash != "", orgUUID, userUUID, token)

	if err := batch.Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to update share link: %v", err)})
		return
	}

	// Build response
	var repoName string
	libUUID, _ := gocql.ParseUUID(libID)
	h.db.Session().Query(`SELECT name FROM libraries WHERE org_id = ? AND library_id = ?`, orgUUID, libUUID).Scan(&repoName)
	if repoName == "" {
		repoName = "Unknown Library"
	}

	// Get user email and name
	var userEmail, updateUserName string
	if err := h.db.Session().Query(`SELECT email, name FROM users WHERE org_id = ? AND user_id = ?`, orgUUID, userUUID).Scan(&userEmail, &updateUserName); err != nil || userEmail == "" {
		userEmail = userID
	}
	if updateUserName == "" {
		updateUserName = userEmail
	}

	isExpired := false
	if newExpiresAt != nil && time.Now().After(*newExpiresAt) {
		isExpired = true
	}

	expireDate := ""
	if newExpiresAt != nil {
		expireDate = newExpiresAt.Format(time.RFC3339)
	}

	canEdit := newPermission == "edit" || newPermission == "upload"
	canDownload := newPermission == "download" || newPermission == "preview_download" || newPermission == "edit"
	canUpload := newPermission == "upload" || newPermission == "edit"

	// Derive obj_name from path
	updateObjName := filePath
	if filePath == "/" {
		updateObjName = repoName
	} else if idx := strings.LastIndex(strings.TrimSuffix(filePath, "/"), "/"); idx >= 0 {
		updateObjName = strings.TrimSuffix(filePath, "/")[idx+1:]
	}

	c.JSON(http.StatusOK, ShareLink{
		Token:       token,
		RepoID:      libID,
		RepoName:    repoName,
		Path:        filePath,
		IsDir:       filePath == "/" || strings.HasSuffix(filePath, "/"),
		IsExpired:   isExpired,
		ObjName:     updateObjName,
		ViewCount:   downloadCount,
		CTime:       createdAt.Format(time.RFC3339),
		ExpireDate:  expireDate,
		CanEdit:     canEdit,
		CanDownload: canDownload,
		Permissions: Perms{
			CanEdit:     canEdit,
			CanDownload: canDownload,
			CanUpload:   canUpload,
		},
		UserEmail:   userEmail,
		CreatorName: updateUserName,
		LinkURL:     fmt.Sprintf("%s/d/%s", getBrowserURL(c, h.serverURL), token),
		IsOwner:     true,
		Password:    returnedPassword,
		HasPassword: newPasswordHash != "",
	})
}

// BatchDeleteShareLinks deletes multiple share links at once
// Implements: DELETE /api/v2.1/share-links/
func (h *ShareLinkHandler) BatchDeleteShareLinks(c *gin.Context) {
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	var req struct {
		Tokens []string `json:"tokens"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tokens array required"})
		return
	}

	success := make([]gin.H, 0)
	failed := make([]gin.H, 0)

	for _, tk := range req.Tokens {
		var createdBy string
		err := h.db.Session().Query(
			`SELECT created_by FROM share_links WHERE share_token = ?`, tk,
		).Scan(&createdBy)
		if err != nil {
			failed = append(failed, gin.H{"token": tk, "error_msg": "not found"})
			continue
		}
		if createdBy != userID {
			failed = append(failed, gin.H{"token": tk, "error_msg": "permission denied"})
			continue
		}

		batch := h.db.Session().Batch(gocql.LoggedBatch)
		batch.Query(`DELETE FROM share_links WHERE share_token = ?`, tk)
		batch.Query(`DELETE FROM share_links_by_creator WHERE org_id = ? AND created_by = ? AND share_token = ?`,
			orgID, userID, tk)
		if err := batch.Exec(); err != nil {
			failed = append(failed, gin.H{"token": tk, "error_msg": err.Error()})
			continue
		}
		success = append(success, gin.H{"token": tk})
	}

	c.JSON(http.StatusOK, gin.H{"success": success, "failed": failed})
}

// generateRandomPassword generates a cryptographically secure random password
func generateRandomPassword(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%"
	b := make([]byte, length)
	for i := range b {
		randBytes := make([]byte, 1)
		rand.Read(randBytes)
		b[i] = charset[int(randBytes[0])%len(charset)]
	}
	return string(b)
}

// BatchCreateShareLinks creates multiple share links at once
// Implements: POST /api/v2.1/multi-share-links/batch/
func (h *ShareLinkHandler) BatchCreateShareLinks(c *gin.Context) {
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	var req struct {
		RepoID               string `form:"repo_id"`
		Path                 string `form:"path"`
		Number               int    `form:"number"`
		AutoGeneratePassword bool   `form:"auto_generate_password"`
		ExpirationTime       string `form:"expiration_time"`
		Permissions          string `form:"permissions"`
	}
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.RepoID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_id is required"})
		return
	}
	if req.Number < 2 || req.Number > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "number must be between 2 and 200"})
		return
	}
	if req.Path == "" {
		req.Path = "/"
	}

	// Validate repo access
	repoUUID, err := uuid.Parse(req.RepoID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid repo_id"})
		return
	}
	_ = repoUUID

	if h.permMiddleware != nil {
		hasAccess, err := h.permMiddleware.HasLibraryAccess(orgID, userID, req.RepoID, middleware.PermissionR)
		if err != nil || !hasAccess {
			c.JSON(http.StatusForbidden, gin.H{"error": "you do not have access to this library"})
			return
		}
	}

	// Parse permission
	permission := req.Permissions
	if permission == "" {
		permission = "download"
	} else if strings.HasPrefix(permission, "{") {
		var perms struct {
			CanEdit     bool `json:"can_edit"`
			CanDownload bool `json:"can_download"`
			CanUpload   bool `json:"can_upload"`
		}
		if err := json.Unmarshal([]byte(permission), &perms); err == nil {
			if perms.CanEdit {
				permission = "edit"
			} else if perms.CanUpload && perms.CanDownload {
				permission = "upload"
			} else if perms.CanUpload {
				permission = "upload"
			} else if perms.CanDownload {
				permission = "preview_download"
			} else {
				permission = "preview_only"
			}
		}
	}

	// Parse expiration
	var expiresAt *time.Time
	if req.ExpirationTime != "" {
		if t, err := time.Parse(time.RFC3339, req.ExpirationTime); err == nil {
			expiresAt = &t
		} else if t, err := time.Parse("2006-01-02", req.ExpirationTime); err == nil {
			expiresAt = &t
		}
	}

	// Get library name
	var repoName string
	h.db.Session().Query(`SELECT name FROM libraries WHERE library_id = ?`, req.RepoID).Scan(&repoName)
	if repoName == "" {
		repoName = "Unknown Library"
	}

	// Get creator info
	orgUUID, _ := gocql.ParseUUID(orgID)
	userUUID, _ := gocql.ParseUUID(userID)
	var createUserEmail, createUserName string
	h.db.Session().Query(`SELECT email, name FROM users WHERE org_id = ? AND user_id = ?`, orgUUID, userUUID).Scan(&createUserEmail, &createUserName)
	if createUserEmail == "" {
		createUserEmail = userID
	}
	if createUserName == "" {
		createUserName = createUserEmail
	}

	canEdit := permission == "edit" || permission == "upload"
	canDownload := permission == "download" || permission == "preview_download" || permission == "edit"
	canUpload := permission == "upload" || permission == "edit"

	now := time.Now()
	expireDate := ""
	if expiresAt != nil {
		expireDate = expiresAt.Format(time.RFC3339)
	}

	objName := req.Path
	if req.Path == "/" {
		objName = repoName
	} else if idx := strings.LastIndex(strings.TrimSuffix(req.Path, "/"), "/"); idx >= 0 {
		objName = strings.TrimSuffix(req.Path, "/")[idx+1:]
	}

	var links []ShareLink

	for i := 0; i < req.Number; i++ {
		token, err := generateSecureShareToken(16)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
			return
		}

		var password, passwordHash string
		if req.AutoGeneratePassword {
			password = generateRandomPassword(10)
			hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
				return
			}
			passwordHash = string(hash)
		}

		batch := h.db.Session().Batch(gocql.LoggedBatch)
		batch.Query(`
			INSERT INTO share_links (
				share_token, org_id, library_id, file_path, created_by, permission,
				password_hash, expires_at, download_count, max_downloads, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, token, orgID, req.RepoID, req.Path, userID,
			permission, passwordHash, expiresAt, 0, nil, now)

		batch.Query(`
			INSERT INTO share_links_by_creator (
				org_id, created_by, share_token, library_id, file_path, permission,
				expires_at, download_count, max_downloads, created_at, has_password
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, orgID, userID, token, req.RepoID, req.Path,
			permission, expiresAt, 0, nil, now, password != "")

		if err := batch.Exec(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create share link"})
			return
		}

		links = append(links, ShareLink{
			Token:       token,
			RepoID:      req.RepoID,
			RepoName:    repoName,
			Path:        req.Path,
			IsDir:       req.Path == "/" || strings.HasSuffix(req.Path, "/"),
			IsExpired:   false,
			ObjName:     objName,
			ViewCount:   0,
			CTime:       now.Format(time.RFC3339),
			ExpireDate:  expireDate,
			CanEdit:     canEdit,
			CanDownload: canDownload,
			Permissions: Perms{
				CanEdit:     canEdit,
				CanDownload: canDownload,
				CanUpload:   canUpload,
			},
			UserEmail:   createUserEmail,
			CreatorName: createUserName,
			LinkURL:     fmt.Sprintf("%s/d/%s", getBrowserURL(c, h.serverURL), token),
			IsOwner:     true,
			Password:    password,
			HasPassword: password != "",
		})
	}

	c.JSON(http.StatusOK, links)
}
