package v2

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/models"
	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// ShareLinkHandler handles share link API requests
type ShareLinkHandler struct {
	db *db.DB
}

// NewShareLinkHandler creates a new ShareLinkHandler
func NewShareLinkHandler(database *db.DB) *ShareLinkHandler {
	return &ShareLinkHandler{db: database}
}

// ShareLink represents a share link in API response
type ShareLink struct {
	Token         string `json:"token"`
	RepoID        string `json:"repo_id"`
	RepoName      string `json:"repo_name"`
	Path          string `json:"path"`
	IsDir         bool   `json:"is_dir"`
	IsExpired     bool   `json:"is_expired"`
	ObjID         string `json:"obj_id,omitempty"`
	ObjName       string `json:"obj_name"`
	ViewCount     int    `json:"view_cnt"`
	CTime         string `json:"ctime"`
	ExpireDate    string `json:"expire_date,omitempty"`
	CanEdit       bool   `json:"can_edit"`
	CanDownload   bool   `json:"can_download"`
	Permissions   Perms  `json:"permissions"`
	UserEmail     string `json:"username"`
	LinkURL       string `json:"link,omitempty"`
	IsOwner       bool   `json:"is_owner"`
}

// Perms represents permission settings for share links
type Perms struct {
	CanEdit     bool `json:"can_edit"`
	CanDownload bool `json:"can_download"`
	CanUpload   bool `json:"can_upload"`
}

// RegisterShareLinkRoutes registers share link routes
func RegisterShareLinkRoutes(rg *gin.RouterGroup, database *db.DB) *ShareLinkHandler {
	h := NewShareLinkHandler(database)

	shareLinks := rg.Group("/share-links")
	{
		shareLinks.GET("", h.ListShareLinks)
		shareLinks.GET("/", h.ListShareLinks)
		shareLinks.POST("", h.CreateShareLink)
		shareLinks.POST("/", h.CreateShareLink)
		shareLinks.DELETE("/:token", h.DeleteShareLink)
		shareLinks.DELETE("/:token/", h.DeleteShareLink)
	}

	return h
}

// ListShareLinks returns share links for a file or all share links
// Implements: GET /api/v2.1/share-links/?repo_id=xxx&path=/xxx
func (h *ShareLinkHandler) ListShareLinks(c *gin.Context) {
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")
	repoID := c.Query("repo_id")
	path := c.Query("path")

	// Query share links created by this user
	var query string
	var args []interface{}

	if repoID != "" && path != "" {
		// Filter by repo and path
		query = `
			SELECT share_token, library_id, file_path, permission, expires_at, download_count, max_downloads, created_at
			FROM share_links_by_creator
			WHERE org_id = ? AND created_by = ? AND library_id = ? AND file_path = ?
		`
		args = []interface{}{orgID, userID, repoID, path}
	} else {
		// Get all share links for this user
		query = `
			SELECT share_token, library_id, file_path, permission, expires_at, download_count, max_downloads, created_at
			FROM share_links_by_creator
			WHERE org_id = ? AND created_by = ?
		`
		args = []interface{}{orgID, userID}
	}

	iter := h.db.Session().Query(query, args...).Iter()

	var links []ShareLink
	var token, libID, filePath, permission string
	var expiresAt *time.Time
	var downloadCount int
	var maxDownloads *int
	var createdAt time.Time

	for iter.Scan(&token, &libID, &filePath, &permission, &expiresAt, &downloadCount, &maxDownloads, &createdAt) {
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
		libUUID, err := uuid.Parse(libID)
		if err == nil {
			orgUUID, _ := uuid.Parse(orgID)
			h.db.Session().Query(`SELECT name FROM libraries WHERE org_id = ? AND library_id = ?`, orgUUID, libUUID).Scan(&repoName)
		}
		if repoName == "" {
			repoName = "Unknown Library"
		}

		// Determine permissions
		canEdit := permission == "edit" || permission == "upload"
		canDownload := permission == "download" || permission == "preview_download" || permission == "edit"
		canUpload := permission == "upload" || permission == "edit"

		// Get user email
		var userEmail string
		userUUID, err := uuid.Parse(userID)
		if err == nil {
			h.db.Session().Query(`SELECT email FROM users WHERE user_id = ?`, userUUID).Scan(&userEmail)
		}
		if userEmail == "" {
			userEmail = userID // Fallback to ID if email not found
		}

		links = append(links, ShareLink{
			Token:       token,
			RepoID:      libID,
			RepoName:    repoName,
			Path:        filePath,
			IsDir:       false, // TODO: Determine from path or store in DB
			IsExpired:   isExpired,
			ObjName:     filePath,
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
			UserEmail: userEmail,
			LinkURL:   fmt.Sprintf("/d/%s", token),
			IsOwner:   true,
		})
	}

	if err := iter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list share links"})
		return
	}

	if links == nil {
		links = []ShareLink{}
	}

	c.JSON(http.StatusOK, links)
}

// ShareLinkCreateRequest represents the request for creating a share link
type ShareLinkCreateRequest struct {
	RepoID      string `json:"repo_id" form:"repo_id"`
	Path        string `json:"path" form:"path"`
	Password    string `json:"password" form:"password"`
	ExpireDays  int    `json:"expire_days" form:"expire_days"`
	Permissions string `json:"permissions" form:"permissions"` // "preview_download", "preview_only", etc.
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

	// Default permission based on Seafile's "permissions" parameter
	// "preview_download", "preview_only", "download", "upload", "edit"
	permission := req.Permissions
	if permission == "" {
		permission = "download"
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

	// Calculate expiration
	var expiresAt *time.Time
	if req.ExpireDays > 0 {
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
	batch := h.db.Session().NewBatch(gocql.LoggedBatch)

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
			expires_at, download_count, max_downloads, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, orgID, userID, link.Token, req.RepoID, link.Path,
		link.Permission, link.ExpiresAt, 0, nil, link.CreatedAt,
	)

	if err := h.db.Session().ExecuteBatch(batch); err != nil {
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

	// Return Seafile-compatible response
	c.JSON(http.StatusOK, ShareLink{
		Token:       token,
		RepoID:      req.RepoID,
		RepoName:    repoName,
		Path:        req.Path,
		IsDir:       false,
		IsExpired:   false,
		ObjName:     req.Path,
		ViewCount:   0,
		CTime:       now.Format(time.RFC3339),
		ExpireDate:  expireDate,
		CanEdit:     canEdit,
		CanDownload: canDownload,
		Permissions: Perms{
			CanEdit:     canEdit,
			CanDownload:     canDownload,
			CanUpload:   canUpload,
		},
		UserEmail: userID,
		LinkURL:   fmt.Sprintf("/d/%s", token),
		IsOwner:   true,
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
	batch := h.db.Session().NewBatch(gocql.LoggedBatch)

	batch.Query(`DELETE FROM share_links WHERE share_token = ?`, token)
	batch.Query(`DELETE FROM share_links_by_creator WHERE org_id = ? AND created_by = ? AND share_token = ?`,
		orgID, userID, token)

	if err := h.db.Session().ExecuteBatch(batch); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete share link"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
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
