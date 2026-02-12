package v2

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// UploadLinkHandler handles upload link API requests
type UploadLinkHandler struct {
	db        *db.DB
	serverURL string
}

// NewUploadLinkHandler creates a new UploadLinkHandler
func NewUploadLinkHandler(database *db.DB, serverURL string) *UploadLinkHandler {
	return &UploadLinkHandler{db: database, serverURL: serverURL}
}

// UploadLinkResponse represents an upload link in API response
type UploadLinkResponse struct {
	Token      string `json:"token"`
	RepoID     string `json:"repo_id"`
	RepoName   string `json:"repo_name"`
	Path       string `json:"path"`
	ObjName    string `json:"obj_name"`
	IsExpired  bool   `json:"is_expired"`
	CTime      string `json:"ctime"`
	ExpireDate string `json:"expire_date,omitempty"`
	UserEmail  string `json:"username"`
	LinkURL    string `json:"link,omitempty"`
	IsOwner    bool   `json:"is_owner"`
}

// RegisterUploadLinkRoutes registers upload link routes
func RegisterUploadLinkRoutes(rg *gin.RouterGroup, database *db.DB, serverURL string) *UploadLinkHandler {
	h := NewUploadLinkHandler(database, serverURL)

	uploadLinks := rg.Group("/upload-links")
	{
		uploadLinks.GET("", h.ListUploadLinks)
		uploadLinks.GET("/", h.ListUploadLinks)
		uploadLinks.POST("", h.CreateUploadLink)
		uploadLinks.POST("/", h.CreateUploadLink)
		uploadLinks.DELETE("/:token", h.DeleteUploadLink)
		uploadLinks.DELETE("/:token/", h.DeleteUploadLink)
	}

	// Repo-specific upload links
	repoUploadLinks := rg.Group("/repos/:repo_id/upload-links")
	{
		repoUploadLinks.GET("", h.ListRepoUploadLinks)
		repoUploadLinks.GET("/", h.ListRepoUploadLinks)
	}

	return h
}

// ListUploadLinks returns upload links for the authenticated user
// Implements: GET /api/v2.1/upload-links/?repo_id=xxx
func (h *UploadLinkHandler) ListUploadLinks(c *gin.Context) {
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")
	repoIDFilter := c.Query("repo_id")

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

	iter := h.db.Session().Query(`
		SELECT upload_token, library_id, file_path, expires_at, created_at
		FROM upload_links_by_creator
		WHERE org_id = ? AND created_by = ?
	`, orgUUID, userUUID).Iter()

	var links []UploadLinkResponse
	var token, libID, filePath string
	var expiresAt *time.Time
	var createdAt time.Time

	// Get user email
	var userEmail string
	if err := h.db.Session().Query(`SELECT email FROM users WHERE org_id = ? AND user_id = ?`, orgUUID, userUUID).Scan(&userEmail); err != nil || userEmail == "" {
		userEmail = userID
	}

	libNameCache := map[string]string{}

	for iter.Scan(&token, &libID, &filePath, &expiresAt, &createdAt) {
		if repoIDFilter != "" && libID != repoIDFilter {
			continue
		}

		isExpired := false
		expireDate := ""
		if expiresAt != nil && !expiresAt.IsZero() {
			isExpired = expiresAt.Before(time.Now())
			expireDate = expiresAt.Format(time.RFC3339)
		}

		repoName, ok := libNameCache[libID]
		if !ok {
			libUUID, parseErr := gocql.ParseUUID(libID)
			if parseErr == nil {
				h.db.Session().Query(`SELECT name FROM libraries WHERE org_id = ? AND library_id = ?`, orgUUID, libUUID).Scan(&repoName)
			}
			if repoName == "" {
				repoName = "Unknown Library"
			}
			libNameCache[libID] = repoName
		}

		objName := filePath
		if idx := strings.LastIndex(filePath, "/"); idx >= 0 && idx < len(filePath)-1 {
			objName = filePath[idx+1:]
		}
		if filePath == "/" {
			objName = repoName
		}

		links = append(links, UploadLinkResponse{
			Token:      token,
			RepoID:     libID,
			RepoName:   repoName,
			Path:       filePath,
			ObjName:    objName,
			IsExpired:  isExpired,
			CTime:      createdAt.Format(time.RFC3339),
			ExpireDate: expireDate,
			UserEmail:  userEmail,
			LinkURL:    fmt.Sprintf("%s/u/d/%s", getBrowserURL(c, h.serverURL), token),
			IsOwner:    true,
		})
	}

	if err := iter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to list upload links: %v", err)})
		return
	}

	if links == nil {
		links = []UploadLinkResponse{}
	}

	c.JSON(http.StatusOK, links)
}

// UploadLinkCreateRequest represents the request for creating an upload link
type UploadLinkCreateRequest struct {
	RepoID     string `json:"repo_id" form:"repo_id"`
	Path       string `json:"path" form:"path"`
	Password   string `json:"password" form:"password"`
	ExpireDays int    `json:"expire_days" form:"expire_days"`
}

// CreateUploadLink creates a new upload link
// Implements: POST /api/v2.1/upload-links/
func (h *UploadLinkHandler) CreateUploadLink(c *gin.Context) {
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	var req UploadLinkCreateRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.RepoID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_id is required"})
		return
	}

	if req.Path == "" {
		req.Path = "/"
	}

	// Validate repo exists
	if _, err := uuid.Parse(req.RepoID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid repo_id"})
		return
	}

	// Generate secure token
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)

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

	// Dual-write to both tables
	batch := h.db.Session().NewBatch(gocql.LoggedBatch)

	batch.Query(`
		INSERT INTO upload_links (
			upload_token, org_id, library_id, file_path, created_by,
			password_hash, expires_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, token, orgID, req.RepoID, req.Path, userID,
		passwordHash, expiresAt, now)

	batch.Query(`
		INSERT INTO upload_links_by_creator (
			org_id, created_by, upload_token, library_id, file_path,
			expires_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, orgID, userID, token, req.RepoID, req.Path,
		expiresAt, now)

	if err := h.db.Session().ExecuteBatch(batch); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create upload link"})
		return
	}

	// Get library name for response
	var repoName string
	h.db.Session().Query(`SELECT name FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, req.RepoID).Scan(&repoName)
	if repoName == "" {
		repoName = "Unknown Library"
	}

	objName := req.Path
	if req.Path == "/" {
		objName = repoName
	} else if idx := strings.LastIndex(req.Path, "/"); idx >= 0 && idx < len(req.Path)-1 {
		objName = req.Path[idx+1:]
	}

	expireDate := ""
	if expiresAt != nil {
		expireDate = expiresAt.Format(time.RFC3339)
	}

	c.JSON(http.StatusOK, UploadLinkResponse{
		Token:      token,
		RepoID:     req.RepoID,
		RepoName:   repoName,
		Path:       req.Path,
		ObjName:    objName,
		IsExpired:  false,
		CTime:      now.Format(time.RFC3339),
		ExpireDate: expireDate,
		UserEmail:  userID,
		LinkURL:    fmt.Sprintf("%s/u/d/%s", getBrowserURL(c, h.serverURL), token),
		IsOwner:    true,
	})
}

// DeleteUploadLink deletes an upload link
// Implements: DELETE /api/v2.1/upload-links/:token/
func (h *UploadLinkHandler) DeleteUploadLink(c *gin.Context) {
	token := c.Param("token")
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	// Verify ownership
	var createdBy string
	if err := h.db.Session().Query(`
		SELECT created_by FROM upload_links WHERE upload_token = ?
	`, token).Scan(&createdBy); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "upload link not found"})
		return
	}

	if createdBy != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "not authorized to delete this upload link"})
		return
	}

	// Dual-delete from both tables
	batch := h.db.Session().NewBatch(gocql.LoggedBatch)
	batch.Query(`DELETE FROM upload_links WHERE upload_token = ?`, token)
	batch.Query(`DELETE FROM upload_links_by_creator WHERE org_id = ? AND created_by = ? AND upload_token = ?`,
		orgID, userID, token)

	if err := h.db.Session().ExecuteBatch(batch); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete upload link"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ListRepoUploadLinks returns upload links for a specific repo
// Implements: GET /api/v2.1/repos/:repo_id/upload-links/
func (h *UploadLinkHandler) ListRepoUploadLinks(c *gin.Context) {
	repoID := c.Param("repo_id")
	c.Request.URL.RawQuery = fmt.Sprintf("repo_id=%s&%s", repoID, c.Request.URL.RawQuery)
	h.ListUploadLinks(c)
}
