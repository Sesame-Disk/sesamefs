package v2

import (
	"net/http"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// FileShareHandler handles file/folder sharing to users and groups
type FileShareHandler struct {
	db *db.DB
}

// NewFileShareHandler creates a new FileShareHandler
func NewFileShareHandler(database *db.DB) *FileShareHandler {
	return &FileShareHandler{db: database}
}

// lookupUserIDByEmail resolves an email address to a user_id.
// It first checks the optimised users_by_email index.  If the row is missing
// (e.g. the user was created before the dual-write was introduced) it falls
// back to scanning the users table scoped to the given org, then backfills
// users_by_email so that subsequent lookups are fast.
func (h *FileShareHandler) lookupUserIDByEmail(orgID, email string) (string, error) {
	var userID string
	if err := h.db.Session().Query(`
		SELECT user_id FROM users_by_email WHERE email = ?
	`, email).Scan(&userID); err == nil {
		return userID, nil
	}

	// Fallback: scan within the org partition (bounded, safe with ALLOW FILTERING)
	if err := h.db.Session().Query(`
		SELECT user_id FROM users WHERE org_id = ? AND email = ? ALLOW FILTERING
	`, orgID, email).Scan(&userID); err != nil {
		return "", err
	}

	// Backfill the index so future lookups skip the slow path
	_ = h.db.Session().Query(`
		INSERT INTO users_by_email (email, user_id, org_id) VALUES (?, ?, ?)
	`, email, userID, orgID).Exec()

	return userID, nil
}

// RegisterFileShareRoutes registers file share routes
func RegisterFileShareRoutes(rg *gin.RouterGroup, database *db.DB) *FileShareHandler {
	h := NewFileShareHandler(database)

	// Shared repos endpoints
	rg.GET("/shared-repos/", h.ListSharedRepos)
	rg.GET("/beshared-repos/", h.ListBeSharedRepos)

	// Note: Other routes are registered in the libraries.go file under /repos/:repo_id/dir/shared_items
	// This file provides the handler implementations

	return h
}

// ShareResponse represents a share in API response
type ShareResponse struct {
	ShareID      string     `json:"share_id"`
	ShareType    string     `json:"share_type"` // "user" or "group"
	RepoID       string     `json:"repo_id"`
	Path         string     `json:"path"`
	Permission   string     `json:"permission"`
	IsAdmin      bool       `json:"is_admin"`
	ShareTo      string     `json:"share_to"`       // email (user identifier) or group_id
	ShareToName  string     `json:"share_to_name"`  // display name
	SharedBy     string     `json:"shared_by"`      // email
	SharedByName string     `json:"shared_by_name"` // display name
	CreatedAt    string     `json:"ctime"`          // RFC3339 format
	ExpiresAt    *string    `json:"expire_date"`    // RFC3339 format
	UserInfo     *UserInfo  `json:"user_info,omitempty"`
	GroupInfo    *GroupInfo `json:"group_info,omitempty"`
}

// UserInfo represents user information in share response
// Note: In Seahub, "name" is the user identifier (email). The frontend uses
// user_info.name as the username param in update/delete API calls.
type UserInfo struct {
	Name     string `json:"name"`          // email (user identifier, used by frontend for API calls)
	Nickname string `json:"nickname"`      // display name
	Email    string `json:"contact_email"` // contact email
	Avatar   string `json:"avatar_url"`
}

// GroupInfo represents group information in share response
type GroupInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListSharedItems returns list of shares for a file/folder
// GET /api2/repos/:repo_id/dir/shared_items/?p={path}&share_type={user|group}
func (h *FileShareHandler) ListSharedItems(c *gin.Context) {
	repoID := c.Param("repo_id")
	path := c.Query("p")
	shareType := c.Query("share_type") // "user" or "group"

	if repoID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_id is required"})
		return
	}

	if path == "" {
		path = "/"
	}

	// Validate repo exists
	repoUUID, err := uuid.Parse(repoID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid repo_id"})
		return
	}

	// Check if database is available
	if h.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database not available"})
		return
	}

	// Query shares for this library and path
	// Note: We need to create a lookup table for efficient querying by library_id and path
	// For now, we'll query all shares for the library and filter in Go
	// Use .String() for UUID params - gocql can't marshal google/uuid.UUID directly
	iter := h.db.Session().Query(`
		SELECT share_id, shared_by, shared_to, shared_to_type, permission, created_at, expires_at
		FROM shares WHERE library_id = ?
	`, repoUUID.String()).Iter()

	var shares []ShareResponse
	var shareID, sharedBy, sharedTo, sharedToType, permission string
	var createdAt time.Time
	var expiresAt *time.Time

	for iter.Scan(&shareID, &sharedBy, &sharedTo, &sharedToType, &permission, &createdAt, &expiresAt) {
		// Filter by share_type if specified
		if shareType != "" && shareType != sharedToType {
			continue
		}

		// Get shared_by user info (users table requires org_id in WHERE clause)
		var sharedByName, sharedByEmail string
		// First get org_id from library
		var libOrgID string
		h.db.Session().Query(`SELECT org_id FROM libraries_by_id WHERE library_id = ?`, repoUUID.String()).Scan(&libOrgID)

		h.db.Session().Query(`SELECT name, email FROM users WHERE org_id = ? AND user_id = ?`, libOrgID, sharedBy).Scan(&sharedByName, &sharedByEmail)

		share := ShareResponse{
			ShareID:      shareID,
			ShareType:    sharedToType,
			RepoID:       repoID,
			Path:         path,
			Permission:   permission,
			ShareTo:      sharedTo,
			SharedBy:     sharedByEmail,
			SharedByName: sharedByName,
			CreatedAt:    createdAt.Format(time.RFC3339),
		}

		if expiresAt != nil {
			expStr := expiresAt.Format(time.RFC3339)
			share.ExpiresAt = &expStr
		}

		// Get shared_to info
		if sharedToType == "user" {
			var userName, userEmail string
			h.db.Session().Query(`SELECT name, email FROM users WHERE org_id = ? AND user_id = ?`, libOrgID, sharedTo).Scan(&userName, &userEmail)
			// Seahub convention: share_to = email (user identifier)
			share.ShareTo = userEmail
			share.ShareToName = userEmail
			share.UserInfo = &UserInfo{
				Name:     userEmail, // email = user identifier (frontend uses this for update/delete calls)
				Nickname: userName,  // display name shown in UI
				Email:    userEmail, // contact email
				Avatar:   "",
			}
		} else if sharedToType == "group" {
			// Get group info from groups table
			var groupName string
			h.db.Session().Query(`SELECT name FROM groups WHERE org_id = ? AND group_id = ?`, libOrgID, sharedTo).Scan(&groupName)
			if groupName == "" {
				groupName = "Group " + sharedTo
			}
			share.ShareToName = groupName
			share.GroupInfo = &GroupInfo{
				ID:   sharedTo,
				Name: groupName,
			}
		}

		shares = append(shares, share)
	}

	if err := iter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list shares"})
		return
	}

	if shares == nil {
		shares = []ShareResponse{}
	}

	c.JSON(http.StatusOK, shares)
}

// CreateShare creates new share(s) to users or groups
// PUT /api2/repos/:repo_id/dir/shared_items/?p={path}
// Form data: share_type, permission, username[] or group_id[]
func (h *FileShareHandler) CreateShare(c *gin.Context) {
	repoID := c.Param("repo_id")
	path := c.Query("p")
	userID := c.GetString("user_id")

	if repoID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_id is required"})
		return
	}

	if path == "" {
		path = "/"
	}

	shareType := c.PostForm("share_type")  // "user" or "group"
	permission := c.PostForm("permission") // "r", "rw"

	if shareType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "share_type is required"})
		return
	}

	if permission == "" {
		permission = "r"
	}

	// Validate repo exists
	repoUUID, err := uuid.Parse(repoID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid repo_id"})
		return
	}

	// ========================================================================
	// PERMISSION CHECK: Block sharing of encrypted libraries (security policy)
	// ========================================================================
	// Get library info to check if it's encrypted
	var libOrgID string
	var encrypted bool
	err = h.db.Session().Query(`
		SELECT org_id, encrypted FROM libraries_by_id WHERE library_id = ?
	`, repoUUID.String()).Scan(&libOrgID, &encrypted)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "library not found"})
		return
	}

	// Prevent sharing encrypted libraries (security policy)
	if encrypted {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Cannot share encrypted libraries. Encrypted libraries cannot be shared for security reasons. Please move files to a non-encrypted library to share them.",
		})
		return
	}

	// Validate share_type-specific parameters before DB access
	if shareType == "user" {
		usernames := c.PostFormArray("username")
		if len(usernames) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "username is required"})
			return
		}
	} else if shareType == "group" {
		groupIDs := c.PostFormArray("group_id")
		if len(groupIDs) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "group_id is required"})
			return
		}
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid share_type"})
		return
	}

	// Check if database is available
	if h.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database not available"})
		return
	}

	var successItems []gin.H
	var failedItems []gin.H
	now := time.Now()

	// Pre-load existing shares for this library to check for duplicates
	existingShares := make(map[string]string) // shared_to+type -> share_id
	dedupIter := h.db.Session().Query(`
		SELECT share_id, shared_to, shared_to_type FROM shares WHERE library_id = ?
	`, repoUUID.String()).Iter()
	var existShareID, existSharedTo, existSharedToType string
	for dedupIter.Scan(&existShareID, &existSharedTo, &existSharedToType) {
		existingShares[existSharedTo+":"+existSharedToType] = existShareID
	}
	dedupIter.Close()

	if shareType == "user" {
		// Share to user(s)
		usernames := c.PostFormArray("username")

		for _, username := range usernames {
			// Get user by email (with fallback for pre-index users)
			sharedToUserID, lookupErr := h.lookupUserIDByEmail(libOrgID, username)
			if lookupErr != nil {
				failedItems = append(failedItems, gin.H{
					"email":     username,
					"error_msg": "user not found",
				})
				continue
			}

			// Get display name
			var userName string
			h.db.Session().Query(`SELECT name FROM users WHERE org_id = ? AND user_id = ?`, libOrgID, sharedToUserID).Scan(&userName)

			// Check for duplicate: if already shared to this user, update permission instead
			if existShareID, exists := existingShares[sharedToUserID+":user"]; exists {
				// Update existing share permission
				h.db.Session().Query(`
					UPDATE shares SET permission = ? WHERE library_id = ? AND share_id = ?
				`, permission, repoUUID.String(), existShareID).Exec()

				successItems = append(successItems, gin.H{
					"user_info":  gin.H{"name": username, "nickname": userName, "contact_email": username, "avatar_url": ""},
					"share_type": "user",
					"permission": permission,
					"is_admin":   false,
				})
				continue
			}

			shareIDUUID := uuid.New()

			// Insert share into database
			if err := h.db.Session().Query(`
				INSERT INTO shares (
					library_id, share_id, shared_by, shared_to, shared_to_type,
					permission, created_at, expires_at
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			`, repoUUID.String(), shareIDUUID.String(), userID, sharedToUserID, "user", permission, now, nil).Exec(); err != nil {
				failedItems = append(failedItems, gin.H{
					"email":     username,
					"error_msg": "failed to create share",
				})
				continue
			}

			successItems = append(successItems, gin.H{
				"user_info":  gin.H{"name": username, "nickname": userName, "contact_email": username, "avatar_url": ""},
				"share_type": "user",
				"permission": permission,
				"is_admin":   false,
			})
		}
	} else if shareType == "group" {
		// Share to group(s)
		groupIDs := c.PostFormArray("group_id")
		if len(groupIDs) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "group_id is required"})
			return
		}

		for _, groupID := range groupIDs {
			groupUUID, err := uuid.Parse(groupID)
			if err != nil {
				failedItems = append(failedItems, gin.H{
					"group_id":  groupID,
					"error_msg": "invalid group_id",
				})
				continue
			}

			// Get group name
			var groupName string
			h.db.Session().Query(`SELECT name FROM groups WHERE org_id = ? AND group_id = ?`, libOrgID, groupUUID.String()).Scan(&groupName)

			// Check for duplicate
			if existShareID, exists := existingShares[groupUUID.String()+":group"]; exists {
				h.db.Session().Query(`
					UPDATE shares SET permission = ? WHERE library_id = ? AND share_id = ?
				`, permission, repoUUID.String(), existShareID).Exec()

				successItems = append(successItems, gin.H{
					"group_info": gin.H{"id": groupUUID.String(), "name": groupName},
					"share_type": "group",
					"permission": permission,
					"is_admin":   false,
				})
				continue
			}

			shareIDUUID := uuid.New()

			// Insert share into database
			if err := h.db.Session().Query(`
				INSERT INTO shares (
					library_id, share_id, shared_by, shared_to, shared_to_type,
					permission, created_at, expires_at
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			`, repoUUID.String(), shareIDUUID.String(), userID, groupUUID.String(), "group", permission, now, nil).Exec(); err != nil {
				failedItems = append(failedItems, gin.H{
					"group_id":  groupID,
					"error_msg": "failed to create share",
				})
				continue
			}

			successItems = append(successItems, gin.H{
				"group_info": gin.H{"id": groupUUID.String(), "name": groupName},
				"share_type": "group",
				"permission": permission,
				"is_admin":   false,
			})
		}
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid share_type"})
		return
	}

	if successItems == nil {
		successItems = []gin.H{}
	}
	if failedItems == nil {
		failedItems = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{"success": successItems, "failed": failedItems})
}

// UpdateSharePermission updates permission for a share
// POST /api2/repos/:repo_id/dir/shared_items/?p={path}&share_type={type}&username={user} or &group_id={id}
// Form data: permission
func (h *FileShareHandler) UpdateSharePermission(c *gin.Context) {
	repoID := c.Param("repo_id")
	path := c.Query("p")
	shareType := c.Query("share_type")
	username := c.Query("username")
	groupID := c.Query("group_id")
	permission := c.PostForm("permission")

	if repoID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_id is required"})
		return
	}

	if path == "" {
		path = "/"
	}

	if permission == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "permission is required"})
		return
	}

	repoUUID, err := uuid.Parse(repoID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid repo_id"})
		return
	}

	// Validate share-type-specific parameters
	if username == "" && groupID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username or group_id is required"})
		return
	}

	// Check if database is available
	if h.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database not available"})
		return
	}

	// Resolve org_id for this library (needed for email fallback lookup)
	var updateLibOrgID string
	h.db.Session().Query(`
		SELECT org_id FROM libraries_by_id WHERE library_id = ?
	`, repoUUID.String()).Scan(&updateLibOrgID)

	// Find the share to update
	var sharedToID string
	if shareType == "user" && username != "" {
		// Get user ID by email (with fallback for pre-index users)
		var lookupErr error
		sharedToID, lookupErr = h.lookupUserIDByEmail(updateLibOrgID, username)
		if lookupErr != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
	} else if shareType == "group" && groupID != "" {
		sharedToID = groupID
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username or group_id is required"})
		return
	}

	// Update the share permission
	// Note: We need to find the share_id first since we can't update by library_id + shared_to
	// For now, we'll scan all shares for this library
	iter := h.db.Session().Query(`
		SELECT share_id, shared_to, shared_to_type
		FROM shares WHERE library_id = ?
	`, repoUUID.String()).Iter()

	var foundShareID string
	var shareIDStr, sharedTo, sharedToType string
	for iter.Scan(&shareIDStr, &sharedTo, &sharedToType) {
		if sharedTo == sharedToID && sharedToType == shareType {
			foundShareID = shareIDStr
			break
		}
	}
	if err := iter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query shares"})
		return
	}

	if foundShareID == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "share not found"})
		return
	}

	shareIDUUID, err := uuid.Parse(foundShareID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid share_id in database"})
		return
	}

	// Update permission
	if err := h.db.Session().Query(`
		UPDATE shares SET permission = ? WHERE library_id = ? AND share_id = ?
	`, permission, repoUUID.String(), shareIDUUID.String()).Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update share"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// DeleteShare deletes a share
// DELETE /api2/repos/:repo_id/dir/shared_items/?p={path}&share_type={type}&username={user} or &group_id={id}
func (h *FileShareHandler) DeleteShare(c *gin.Context) {
	repoID := c.Param("repo_id")
	path := c.Query("p")
	shareType := c.Query("share_type")
	username := c.Query("username")
	groupID := c.Query("group_id")

	if repoID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_id is required"})
		return
	}

	if path == "" {
		path = "/"
	}

	repoUUID, err := uuid.Parse(repoID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid repo_id"})
		return
	}

	// Validate share-type-specific parameters
	if username == "" && groupID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username or group_id is required"})
		return
	}

	// Check if database is available
	if h.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database not available"})
		return
	}

	// Resolve org_id for this library (needed for email fallback lookup)
	var deleteLibOrgID string
	h.db.Session().Query(`
		SELECT org_id FROM libraries_by_id WHERE library_id = ?
	`, repoUUID.String()).Scan(&deleteLibOrgID)

	// Find the share to delete
	var sharedToID string
	if shareType == "user" && username != "" {
		// Get user ID by email (with fallback for pre-index users)
		var lookupErr error
		sharedToID, lookupErr = h.lookupUserIDByEmail(deleteLibOrgID, username)
		if lookupErr != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
	} else if shareType == "group" && groupID != "" {
		sharedToID = groupID
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username or group_id is required"})
		return
	}

	// Find and delete the share
	iter := h.db.Session().Query(`
		SELECT share_id, shared_to, shared_to_type
		FROM shares WHERE library_id = ?
	`, repoUUID.String()).Iter()

	var foundShareID string
	var shareIDStr, sharedTo, sharedToType string
	for iter.Scan(&shareIDStr, &sharedTo, &sharedToType) {
		if sharedTo == sharedToID && sharedToType == shareType {
			foundShareID = shareIDStr
			break
		}
	}
	if err := iter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query shares"})
		return
	}

	if foundShareID == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "share not found"})
		return
	}

	shareIDUUID, err := uuid.Parse(foundShareID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid share_id in database"})
		return
	}

	// Delete share
	if err := h.db.Session().Query(`
		DELETE FROM shares WHERE library_id = ? AND share_id = ?
	`, repoUUID.String(), shareIDUUID.String()).Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete share"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// LibraryShareInfo represents library share information
type LibraryShareInfo struct {
	RepoID       string `json:"repo_id"`
	RepoName     string `json:"repo_name"`
	RepoDesc     string `json:"repo_desc"`
	Permission   string `json:"permission"`
	ShareType    string `json:"share_type"` // "personal" or "group"
	User         string `json:"user"`       // email of user who shared
	LastModified int64  `json:"last_modified"`
	IsVirtual    bool   `json:"is_virtual"`
	Encrypted    int    `json:"encrypted"` // 0 or 1
	EncVersion   int    `json:"enc_version,omitempty"`
	Magic        string `json:"magic,omitempty"`
	RandomKey    string `json:"random_key,omitempty"`
	Salt         string `json:"salt,omitempty"`
}

// ListSharedRepos returns list of libraries I have shared with others
// GET /api2/shared-repos/
func (h *FileShareHandler) ListSharedRepos(c *gin.Context) {
	userID := c.GetString("user_id")
	orgID := c.GetString("org_id")

	userUUID, err := uuid.Parse(userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user_id"})
		return
	}

	// Query all shares created by this user
	// Note: This scans all libraries and checks shares - we need a shares_by_creator lookup table
	// TODO: Create shares_by_creator lookup table for efficiency
	// For now, scan libraries owned by the user and get their shares
	libIter := h.db.Session().Query(`
		SELECT library_id FROM libraries WHERE org_id = ?
	`, orgID).Iter()

	sharedRepos := make(map[string]*LibraryShareInfo)
	var scanLibID string
	for libIter.Scan(&scanLibID) {
		scanLibUUID, parseErr := uuid.Parse(scanLibID)
		if parseErr != nil {
			continue
		}

		iter := h.db.Session().Query(`
			SELECT library_id, share_id, shared_to, shared_to_type, permission, shared_by
			FROM shares WHERE library_id = ?
		`, scanLibUUID).Iter()

		var libID, shareID, sharedTo, sharedToType, permission, sharedBy string
		for iter.Scan(&libID, &shareID, &sharedTo, &sharedToType, &permission, &sharedBy) {
			// Only include shares created by this user
			sharedByUUID, _ := uuid.Parse(sharedBy)
			if sharedByUUID != userUUID {
				continue
			}

			if _, exists := sharedRepos[libID]; !exists {
				var name, description string
				var encrypted bool
				var updatedAt time.Time
				var encVersion int
				var magic, randomKey, salt string

				if queryErr := h.db.Session().Query(`
					SELECT name, description, encrypted, updated_at, enc_version, magic, random_key, salt
					FROM libraries WHERE org_id = ? AND library_id = ?
				`, orgID, libID).Scan(&name, &description, &encrypted, &updatedAt, &encVersion, &magic, &randomKey, &salt); queryErr != nil {
					continue
				}

				encryptedInt := 0
				if encrypted {
					encryptedInt = 1
				}

				sharedRepos[libID] = &LibraryShareInfo{
					RepoID:       libID,
					RepoName:     name,
					RepoDesc:     description,
					Permission:   permission,
					ShareType:    "personal",
					LastModified: updatedAt.Unix(),
					IsVirtual:    false,
					Encrypted:    encryptedInt,
					EncVersion:   encVersion,
					Magic:        magic,
					RandomKey:    randomKey,
					Salt:         salt,
				}
			}
		}
		if closeErr := iter.Close(); closeErr != nil {
			continue
		}
	}
	if err := libIter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list libraries"})
		return
	}

	// Convert map to array
	var result []LibraryShareInfo
	for _, repo := range sharedRepos {
		result = append(result, *repo)
	}

	if result == nil {
		result = []LibraryShareInfo{}
	}

	c.JSON(http.StatusOK, result)
}

// ListBeSharedRepos returns list of libraries shared with me
// GET /api2/beshared-repos/
func (h *FileShareHandler) ListBeSharedRepos(c *gin.Context) {
	userID := c.GetString("user_id")
	orgID := c.GetString("org_id")

	userUUID, err := uuid.Parse(userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user_id"})
		return
	}

	orgUUID, err := uuid.Parse(orgID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org_id"})
		return
	}

	// Query all shares where this user is the recipient (direct or via group)
	// Note: This requires scanning all shares - we need a lookup table
	// TODO: Create shares_by_recipient lookup table

	// Get user's group IDs for group share resolution
	var userGroupIDs []uuid.UUID
	groupIter := h.db.Session().Query(`
		SELECT group_id FROM groups_by_member WHERE org_id = ? AND user_id = ?
	`, orgID, userID).Iter()
	var gidStr string
	for groupIter.Scan(&gidStr) {
		if gid, parseErr := uuid.Parse(gidStr); parseErr == nil {
			userGroupIDs = append(userGroupIDs, gid)
		}
	}
	groupIter.Close()

	beSharedRepos := make(map[string]*LibraryShareInfo)

	// Helper to add a library share to results
	addLibShare := func(libIDStr string, sharedBy, permission, sharedToType string) {
		var name, description string
		var encrypted bool
		var updatedAt time.Time
		var encVersion int
		var magic, randomKey, salt string

		if queryErr := h.db.Session().Query(`
			SELECT name, description, encrypted, updated_at, enc_version, magic, random_key, salt
			FROM libraries WHERE org_id = ? AND library_id = ?
		`, orgID, libIDStr).Scan(&name, &description, &encrypted, &updatedAt, &encVersion, &magic, &randomKey, &salt); queryErr != nil {
			return
		}

		var sharedByEmail string
		sharedByUUID, _ := uuid.Parse(sharedBy)
		h.db.Session().Query(`SELECT email FROM users WHERE org_id = ? AND user_id = ?`, orgUUID, sharedByUUID).Scan(&sharedByEmail)

		encryptedInt := 0
		if encrypted {
			encryptedInt = 1
		}

		beSharedRepos[libIDStr] = &LibraryShareInfo{
			RepoID:       libIDStr,
			RepoName:     name,
			RepoDesc:     description,
			Permission:   permission,
			ShareType:    sharedToType,
			User:         sharedByEmail,
			LastModified: updatedAt.Unix(),
			IsVirtual:    false,
			Encrypted:    encryptedInt,
			EncVersion:   encVersion,
			Magic:        magic,
			RandomKey:    randomKey,
			Salt:         salt,
		}
	}

	// For now, we'll have to scan all libraries and check if user has shares
	// This is very inefficient and should be optimized with a lookup table
	libIter := h.db.Session().Query(`
		SELECT library_id FROM libraries WHERE org_id = ?
	`, orgID).Iter()

	var libIDStr string
	for libIter.Scan(&libIDStr) {
		libUUID, parseErr := uuid.Parse(libIDStr)
		if parseErr != nil {
			continue
		}

		// Check direct shares to user
		shareIter := h.db.Session().Query(`
			SELECT share_id, shared_by, permission, shared_to_type
			FROM shares WHERE library_id = ? AND shared_to = ?
		`, libUUID, userUUID).Iter()

		var shareID, sharedBy, permission, sharedToType string
		for shareIter.Scan(&shareID, &sharedBy, &permission, &sharedToType) {
			addLibShare(libIDStr, sharedBy, permission, sharedToType)
		}
		if closeErr := shareIter.Close(); closeErr != nil {
			continue
		}

		// Check group shares (skip if already found via direct share)
		if _, exists := beSharedRepos[libIDStr]; !exists {
			for _, groupID := range userGroupIDs {
				groupShareIter := h.db.Session().Query(`
					SELECT share_id, shared_by, permission, shared_to_type
					FROM shares WHERE library_id = ? AND shared_to = ?
				`, libUUID, groupID).Iter()

				for groupShareIter.Scan(&shareID, &sharedBy, &permission, &sharedToType) {
					addLibShare(libIDStr, sharedBy, permission, "group")
				}
				groupShareIter.Close()

				if _, exists := beSharedRepos[libIDStr]; exists {
					break
				}
			}
		}
	}
	if err := libIter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list libraries"})
		return
	}

	// Convert map to array
	var result []LibraryShareInfo
	for _, repo := range beSharedRepos {
		result = append(result, *repo)
	}

	if result == nil {
		result = []LibraryShareInfo{}
	}

	c.JSON(http.StatusOK, result)
}

// ListCustomSharePermissions returns custom share permissions for a library.
// Stub — returns empty list. Custom share permissions are a Seafile Pro feature
// that allows defining granular per-library permission sets. Not needed for SesameFS.
// GET /api/v2.1/repos/:repo_id/custom-share-permissions/
func (h *FileShareHandler) ListCustomSharePermissions(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"permission_list": []interface{}{},
	})
}
