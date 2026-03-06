package v2

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// GroupHandler handles group-related API requests
type GroupHandler struct {
	db *db.DB
}

// NewGroupHandler creates a new GroupHandler
func NewGroupHandler(database *db.DB) *GroupHandler {
	return &GroupHandler{db: database}
}

// RegisterGroupRoutes registers group routes
func RegisterGroupRoutes(rg *gin.RouterGroup, database *db.DB) *GroupHandler {
	h := NewGroupHandler(database)

	groups := rg.Group("/groups")
	{
		groups.GET("", h.ListGroups)
		groups.GET("/", h.ListGroups)
		groups.POST("", h.CreateGroup)
		groups.POST("/", h.CreateGroup)
		groups.GET("/:group_id", h.GetGroup)
		groups.GET("/:group_id/", h.GetGroup)
		groups.PUT("/:group_id", h.UpdateGroup)
		groups.PUT("/:group_id/", h.UpdateGroup)
		groups.DELETE("/:group_id", h.DeleteGroup)
		groups.DELETE("/:group_id/", h.DeleteGroup)

		// Group libraries
		groups.GET("/:group_id/libraries", h.ListGroupLibraries)
		groups.GET("/:group_id/libraries/", h.ListGroupLibraries)

		// Group-owned libraries
		groups.POST("/:group_id/group-owned-libraries", h.CreateGroupOwnedLibrary)
		groups.POST("/:group_id/group-owned-libraries/", h.CreateGroupOwnedLibrary)

		// Group members
		groups.GET("/:group_id/members", h.ListGroupMembers)
		groups.GET("/:group_id/members/", h.ListGroupMembers)
		groups.POST("/:group_id/members", h.AddGroupMember)
		groups.POST("/:group_id/members/", h.AddGroupMember)
		groups.POST("/:group_id/members/bulk", h.BulkAddGroupMembers)
		groups.POST("/:group_id/members/bulk/", h.BulkAddGroupMembers)
		groups.POST("/:group_id/members/import", h.ImportGroupMembersViaFile)
		groups.POST("/:group_id/members/import/", h.ImportGroupMembersViaFile)
		groups.DELETE("/:group_id/members/:user_email", h.RemoveGroupMember)
		groups.DELETE("/:group_id/members/:user_email/", h.RemoveGroupMember)
		groups.PUT("/:group_id/members/:user_email", h.SetGroupAdmin)
		groups.PUT("/:group_id/members/:user_email/", h.SetGroupAdmin)
	}

	return h
}

// RegisterShareableGroupRoutes registers the shareable-groups endpoint.
// Returns all groups the user can share with (same as their groups).
// GET /api/v2.1/shareable-groups/
func RegisterShareableGroupRoutes(rg *gin.RouterGroup, database *db.DB) {
	h := NewGroupHandler(database)
	rg.GET("/shareable-groups", h.ListShareableGroups)
	rg.GET("/shareable-groups/", h.ListShareableGroups)
}

// GroupResponse represents a group in API response
type GroupResponse struct {
	GroupID     string   `json:"id"`
	GroupName   string   `json:"name"`
	Creator     string   `json:"creator"`
	CreatorID   string   `json:"creator_id"`
	CreatedAt   string   `json:"created_at"`
	Parent      int      `json:"parent"` // 0 for top-level groups
	Admins      []string `json:"admins"` // List of admin emails
	MemberCount int      `json:"member_count,omitempty"`
}

// GroupMemberResponse represents a group member in API response
type GroupMemberResponse struct {
	Email     string `json:"email"`
	Name      string `json:"name"`
	UserID    string `json:"user_id"`
	Role      string `json:"role"` // "Owner", "Admin", "Member"
	AddedAt   string `json:"added_at"`
	AvatarURL string `json:"avatar_url"`
}

// capitalizeRole converts a lowercase role ("owner", "admin", "member") to the
// Title-cased form expected by the frontend ("Owner", "Admin", "Member").
func capitalizeRole(role string) string {
	if role == "" {
		return role
	}
	return strings.ToUpper(role[:1]) + role[1:]
}

// CreateGroupRequest represents the request for creating a group
type CreateGroupRequest struct {
	GroupName string `json:"name" form:"name" binding:"required"`
}

// UpdateGroupRequest represents the request for updating a group
type UpdateGroupRequest struct {
	GroupName string `json:"name" form:"name"`
}

// AddMemberRequest represents the request for adding a member to a group
type AddMemberRequest struct {
	Email string `json:"email" form:"email" binding:"required"`
	Role  string `json:"role" form:"role"` // "admin" or "member"
}

// ListGroups returns list of groups for the authenticated user
// GET /api/v2.1/groups/
func (h *GroupHandler) ListGroups(c *gin.Context) {
	userID := c.GetString("user_id")
	orgID := c.GetString("org_id")

	// Check if database is available
	if h.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database not available"})
		return
	}

	userUUID, err := uuid.Parse(userID)
	if err != nil {
		slog.Warn("ListGroups: invalid user_id", "user_id", userID, "error", err)
		c.JSON(http.StatusOK, []GroupResponse{})
		return
	}
	orgUUID, err := uuid.Parse(orgID)
	if err != nil {
		slog.Warn("ListGroups: invalid org_id", "org_id", orgID, "error", err)
		c.JSON(http.StatusOK, []GroupResponse{})
		return
	}

	// Query groups this user is a member of using lookup table
	// Use .String() for UUID params - gocql can't marshal google/uuid.UUID directly
	iter := h.db.Session().Query(`
		SELECT group_id, group_name, role, added_at
		FROM groups_by_member
		WHERE org_id = ? AND user_id = ?
	`, orgUUID.String(), userUUID.String()).Iter()

	var groups []GroupResponse
	var groupID, groupName, role string
	var addedAt time.Time

	for iter.Scan(&groupID, &groupName, &role, &addedAt) {
		// Get group details including creator
		var creatorID string
		var createdAt time.Time
		if err := h.db.Session().Query(`
			SELECT creator_id, created_at FROM groups WHERE org_id = ? AND group_id = ?
		`, orgUUID.String(), groupID).Scan(&creatorID, &createdAt); err != nil {
			slog.Warn("ListGroups: failed to get group details", "group_id", groupID, "error", err)
			continue
		}

		// Get creator email
		var creatorEmail string
		if err := h.db.Session().Query(`SELECT email FROM users WHERE org_id = ? AND user_id = ?`, orgUUID.String(), creatorID).Scan(&creatorEmail); err != nil {
			slog.Warn("ListGroups: failed to get creator email", "creator_id", creatorID, "error", err)
		}

		// Count members
		var memberCount int
		if err := h.db.Session().Query(`SELECT COUNT(*) FROM group_members WHERE group_id = ?`, groupID).Scan(&memberCount); err != nil {
			slog.Warn("ListGroups: failed to count members", "group_id", groupID, "error", err)
		}

		groups = append(groups, GroupResponse{
			GroupID:     groupID,
			GroupName:   groupName,
			Creator:     creatorEmail,
			CreatorID:   creatorID,
			CreatedAt:   createdAt.Format(time.RFC3339),
			Parent:      0,
			Admins:      []string{creatorEmail},
			MemberCount: memberCount,
		})
	}

	if err := iter.Close(); err != nil {
		slog.Error("ListGroups: failed to close iterator", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list groups"})
		return
	}

	if groups == nil {
		groups = []GroupResponse{}
	}

	c.JSON(http.StatusOK, groups)
}

// CreateGroup creates a new group
// POST /api/v2.1/groups/
func (h *GroupHandler) CreateGroup(c *gin.Context) {
	var req CreateGroupRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID := c.GetString("user_id")
	orgID := c.GetString("org_id")

	if req.GroupName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "group_name is required"})
		return
	}

	// Check if database is available
	if h.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database not available"})
		return
	}

	userUUID, _ := uuid.Parse(userID)
	orgUUID, _ := uuid.Parse(orgID)
	groupUUID := uuid.New()
	now := time.Now()

	// Insert into groups table (is_department=false for user-created groups)
	// Use .String() for UUID params - gocql can't marshal google/uuid.UUID directly
	if err := h.db.Session().Query(`
		INSERT INTO groups (org_id, group_id, name, creator_id, is_department, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, orgUUID.String(), groupUUID.String(), req.GroupName, userUUID.String(), false, now, now).Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create group"})
		return
	}

	// Add creator as owner in group_members
	if err := h.db.Session().Query(`
		INSERT INTO group_members (group_id, user_id, role, added_at)
		VALUES (?, ?, ?, ?)
	`, groupUUID.String(), userUUID.String(), "owner", now).Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add creator as member"})
		return
	}

	// Add to lookup table
	if err := h.db.Session().Query(`
		INSERT INTO groups_by_member (org_id, user_id, group_id, group_name, role, added_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, orgUUID.String(), userUUID.String(), groupUUID.String(), req.GroupName, "owner", now).Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update lookup table"})
		return
	}

	// Get creator email
	var creatorEmail string
	h.db.Session().Query(`SELECT email FROM users WHERE org_id = ? AND user_id = ?`, orgUUID.String(), userID).Scan(&creatorEmail)

	c.JSON(http.StatusCreated, GroupResponse{
		GroupID:     groupUUID.String(),
		GroupName:   req.GroupName,
		Creator:     creatorEmail,
		CreatorID:   userID,
		CreatedAt:   now.Format(time.RFC3339),
		Parent:      0,
		Admins:      []string{creatorEmail},
		MemberCount: 1,
	})
}

// GetGroup returns group details
// GET /api/v2.1/groups/:group_id/
func (h *GroupHandler) GetGroup(c *gin.Context) {
	groupID := c.Param("group_id")
	orgID := c.GetString("org_id")

	if groupID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "group_id is required"})
		return
	}

	groupUUID, err := uuid.Parse(groupID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group_id"})
		return
	}

	// Check if database is available
	if h.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database not available"})
		return
	}

	orgUUID, _ := uuid.Parse(orgID)

	// Get group details
	var name string
	var creatorID string
	var createdAt time.Time

	if err := h.db.Session().Query(`
		SELECT name, creator_id, created_at FROM groups WHERE org_id = ? AND group_id = ?
	`, orgUUID.String(), groupUUID.String()).Scan(&name, &creatorID, &createdAt); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
		return
	}

	// Get creator email
	var creatorEmail string
	h.db.Session().Query(`SELECT email FROM users WHERE org_id = ? AND user_id = ?`, orgUUID.String(), creatorID).Scan(&creatorEmail)

	// Count members
	var memberCount int
	h.db.Session().Query(`SELECT COUNT(*) FROM group_members WHERE group_id = ?`, groupUUID.String()).Scan(&memberCount)

	c.JSON(http.StatusOK, GroupResponse{
		GroupID:     groupID,
		GroupName:   name,
		Creator:     creatorEmail,
		CreatorID:   creatorID,
		CreatedAt:   createdAt.Format(time.RFC3339),
		Parent:      0,
		Admins:      []string{creatorEmail},
		MemberCount: memberCount,
	})
}

// UpdateGroup updates group details (rename)
// PUT /api/v2.1/groups/:group_id/
func (h *GroupHandler) UpdateGroup(c *gin.Context) {
	groupID := c.Param("group_id")
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	var req UpdateGroupRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.GroupName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "group_name is required"})
		return
	}

	groupUUID, err := uuid.Parse(groupID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group_id"})
		return
	}

	// Check if database is available
	if h.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database not available"})
		return
	}

	orgUUID, _ := uuid.Parse(orgID)

	// Verify user is owner or admin
	var role string
	if err := h.db.Session().Query(`
		SELECT role FROM group_members WHERE group_id = ? AND user_id = ?
	`, groupUUID.String(), userID).Scan(&role); err != nil || (role != "owner" && role != "admin") {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied"})
		return
	}

	// Update group name
	if err := h.db.Session().Query(`
		UPDATE groups SET name = ?, updated_at = ? WHERE org_id = ? AND group_id = ?
	`, req.GroupName, time.Now(), orgUUID.String(), groupUUID.String()).Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update group"})
		return
	}

	// Update lookup table for all members
	if err := h.db.Session().Query(`
		UPDATE groups_by_member SET group_name = ? WHERE org_id = ? AND group_id = ?
	`, req.GroupName, orgUUID.String(), groupUUID.String()).Exec(); err != nil {
		// Log error but don't fail the request
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// DeleteGroup deletes a group
// DELETE /api/v2.1/groups/:group_id/
func (h *GroupHandler) DeleteGroup(c *gin.Context) {
	groupID := c.Param("group_id")
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	groupUUID, err := uuid.Parse(groupID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group_id"})
		return
	}

	// Check if database is available
	if h.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database not available"})
		return
	}

	orgUUID, _ := uuid.Parse(orgID)
	userUUID, _ := uuid.Parse(userID)

	// Verify user is owner
	var role string
	if err := h.db.Session().Query(`
		SELECT role FROM group_members WHERE group_id = ? AND user_id = ?
	`, groupUUID.String(), userUUID.String()).Scan(&role); err != nil || role != "owner" {
		c.JSON(http.StatusForbidden, gin.H{"error": "only group owner can delete the group"})
		return
	}

	// Delete from groups table
	if err := h.db.Session().Query(`
		DELETE FROM groups WHERE org_id = ? AND group_id = ?
	`, orgUUID.String(), groupUUID.String()).Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete group"})
		return
	}

	// Delete all members
	if err := h.db.Session().Query(`
		DELETE FROM group_members WHERE group_id = ?
	`, groupUUID.String()).Exec(); err != nil {
		// Log error but continue
	}

	// Delete from lookup table
	if err := h.db.Session().Query(`
		DELETE FROM groups_by_member WHERE org_id = ? AND group_id = ?
	`, orgUUID.String(), groupUUID.String()).Exec(); err != nil {
		// Log error but continue
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ListGroupMembers returns list of group members
// GET /api/v2.1/groups/:group_id/members/
func (h *GroupHandler) ListGroupMembers(c *gin.Context) {
	groupID := c.Param("group_id")
	orgID := c.GetString("org_id")

	groupUUID, err := uuid.Parse(groupID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group_id"})
		return
	}

	// Check if database is available
	if h.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database not available"})
		return
	}

	// Query group members
	iter := h.db.Session().Query(`
		SELECT user_id, role, added_at FROM group_members WHERE group_id = ?
	`, groupUUID.String()).Iter()

	var members []GroupMemberResponse
	var userID, role string
	var addedAt time.Time

	for iter.Scan(&userID, &role, &addedAt) {
		// Get user details (org_id is the partition key - must be included)
		var email, name string
		h.db.Session().Query(`SELECT email, name FROM users WHERE org_id = ? AND user_id = ?`, orgID, userID).Scan(&email, &name)

		members = append(members, GroupMemberResponse{
			Email:     email,
			Name:      name,
			UserID:    userID,
			Role:      role,
			AddedAt:   addedAt.Format(time.RFC3339),
			AvatarURL: "",
		})
	}

	if err := iter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list members"})
		return
	}

	if members == nil {
		members = []GroupMemberResponse{}
	}

	c.JSON(http.StatusOK, members)
}

// AddGroupMember adds a member to a group
// POST /api/v2.1/groups/:group_id/members/
func (h *GroupHandler) AddGroupMember(c *gin.Context) {
	groupID := c.Param("group_id")
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	var req AddMemberRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email is required"})
		return
	}

	// Default role
	if req.Role == "" {
		req.Role = "member"
	}

	groupUUID, err := uuid.Parse(groupID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group_id"})
		return
	}

	// Check if database is available
	if h.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database not available"})
		return
	}

	orgUUID, _ := uuid.Parse(orgID)

	// Verify user is owner or admin
	var role string
	if err := h.db.Session().Query(`
		SELECT role FROM group_members WHERE group_id = ? AND user_id = ?
	`, groupUUID.String(), userID).Scan(&role); err != nil || (role != "owner" && role != "admin") {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied"})
		return
	}

	// Get user ID by email
	var newMemberID string
	if err := h.db.Session().Query(`
		SELECT user_id FROM users_by_email WHERE email = ?
	`, req.Email).Scan(&newMemberID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	now := time.Now()

	// Add to group_members
	if err := h.db.Session().Query(`
		INSERT INTO group_members (group_id, user_id, role, added_at)
		VALUES (?, ?, ?, ?)
	`, groupUUID.String(), newMemberID, req.Role, now).Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add member"})
		return
	}

	// Get group name
	var groupName string
	h.db.Session().Query(`SELECT name FROM groups WHERE org_id = ? AND group_id = ?`, orgUUID.String(), groupUUID.String()).Scan(&groupName)

	// Add to lookup table
	if err := h.db.Session().Query(`
		INSERT INTO groups_by_member (org_id, user_id, group_id, group_name, role, added_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, orgUUID.String(), newMemberID, groupUUID.String(), groupName, req.Role, now).Exec(); err != nil {
		// Log error but don't fail
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// RemoveGroupMember removes a member from a group
// DELETE /api/v2.1/groups/:group_id/members/:user_email/
func (h *GroupHandler) RemoveGroupMember(c *gin.Context) {
	groupID := c.Param("group_id")
	userEmail := c.Param("user_email")
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	groupUUID, err := uuid.Parse(groupID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group_id"})
		return
	}

	// Check if database is available
	if h.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database not available"})
		return
	}

	orgUUID, _ := uuid.Parse(orgID)
	userUUID, _ := uuid.Parse(userID)

	// Verify user is owner or admin
	var role string
	if err := h.db.Session().Query(`
		SELECT role FROM group_members WHERE group_id = ? AND user_id = ?
	`, groupUUID.String(), userUUID.String()).Scan(&role); err != nil || (role != "owner" && role != "admin") {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied"})
		return
	}

	// Get member ID by email
	var memberID string
	if err := h.db.Session().Query(`
		SELECT user_id FROM users_by_email WHERE email = ?
	`, userEmail).Scan(&memberID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	// Don't allow removing the owner
	var memberRole string
	if err := h.db.Session().Query(`
		SELECT role FROM group_members WHERE group_id = ? AND user_id = ?
	`, groupUUID.String(), memberID).Scan(&memberRole); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "member not found"})
		return
	}

	if memberRole == "owner" {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot remove group owner"})
		return
	}

	// Remove from group_members
	if err := h.db.Session().Query(`
		DELETE FROM group_members WHERE group_id = ? AND user_id = ?
	`, groupUUID.String(), memberID).Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to remove member"})
		return
	}

	// Remove from lookup table
	if err := h.db.Session().Query(`
		DELETE FROM groups_by_member WHERE org_id = ? AND user_id = ? AND group_id = ?
	`, orgUUID.String(), memberID, groupUUID.String()).Exec(); err != nil {
		// Log error but don't fail
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ListGroupLibraries returns all libraries shared with a group.
// The caller must be a member of the group.
// GET /api/v2.1/groups/:group_id/libraries/
func (h *GroupHandler) ListGroupLibraries(c *gin.Context) {
	groupID := c.Param("group_id")
	userID := c.GetString("user_id")
	orgID := c.GetString("org_id")

	groupUUID, err := uuid.Parse(groupID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group_id"})
		return
	}

	if h.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database not available"})
		return
	}

	// Verify caller is a member of the group
	var role string
	if err := h.db.Session().Query(`
		SELECT role FROM group_members WHERE group_id = ? AND user_id = ?
	`, groupUUID.String(), userID).Scan(&role); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this group"})
		return
	}

	// Iterate org libraries and find those shared with this group
	libIter := h.db.Session().Query(`
		SELECT library_id, owner_id, name, encrypted, size_bytes, updated_at, storage_class
		FROM libraries WHERE org_id = ?
	`, orgID).Iter()

	type groupRepo struct {
		RepoID               string `json:"repo_id"`
		RepoName             string `json:"repo_name"`
		Permission           string `json:"permission"`
		Size                 int64  `json:"size"`
		OwnerEmail           string `json:"owner_email"`
		OwnerContactEmail    string `json:"owner_contact_email"`
		OwnerName            string `json:"owner_name"`
		Encrypted            bool   `json:"encrypted"`
		LastModified         int64  `json:"last_modified"`
		ModifierEmail        string `json:"modifier_email"`
		ModifierContactEmail string `json:"modifier_contact_email"`
		ModifierName         string `json:"modifier_name"`
		Type                 string `json:"type"`
		Starred              bool   `json:"starred"`
		Monitored            bool   `json:"monitored"`
		StorageName          string `json:"storage_name"`
	}

	var repos []groupRepo
	var libID, ownerID, libName, storageClass string
	var encrypted bool
	var sizeBytes int64
	var updatedAt time.Time

	for libIter.Scan(&libID, &ownerID, &libName, &encrypted, &sizeBytes, &updatedAt, &storageClass) {
		// Check if this library has a group share to groupID
		var perm string
		if err := h.db.Session().Query(`
			SELECT permission FROM shares
			WHERE library_id = ? AND shared_to = ? ALLOW FILTERING
		`, libID, groupUUID.String()).Scan(&perm); err != nil {
			continue // not shared with this group
		}

		// Resolve owner email
		var ownerEmail string
		h.db.Session().Query(`SELECT email FROM users WHERE org_id = ? AND user_id = ?`, orgID, ownerID).Scan(&ownerEmail)
		if ownerEmail == "" {
			ownerEmail = ownerID
		}
		ownerName := strings.Split(ownerEmail, "@")[0]

		apiPerm := "rw"
		if perm == "r" {
			apiPerm = "r"
		}

		repos = append(repos, groupRepo{
			RepoID:               libID,
			RepoName:             libName,
			Permission:           apiPerm,
			Size:                 sizeBytes,
			OwnerEmail:           ownerEmail,
			OwnerContactEmail:    ownerEmail,
			OwnerName:            ownerName,
			Encrypted:            encrypted,
			LastModified:         updatedAt.Unix(),
			ModifierEmail:        ownerEmail,
			ModifierContactEmail: ownerEmail,
			ModifierName:         ownerName,
			Type:                 "repo",
			StorageName:          storageClass,
		})
	}
	libIter.Close()

	if repos == nil {
		repos = []groupRepo{}
	}

	c.JSON(http.StatusOK, repos)
}

// CreateGroupOwnedLibrary creates a new library owned by the group.
// POST /api/v2.1/groups/:group_id/group-owned-libraries/
// FormData: name, passwd (optional), permission
func (h *GroupHandler) CreateGroupOwnedLibrary(c *gin.Context) {
	groupID := c.Param("group_id")
	userID := c.GetString("user_id")
	orgID := c.GetString("org_id")

	groupUUID, err := uuid.Parse(groupID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group_id"})
		return
	}

	if h.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database not available"})
		return
	}

	// Verify caller is an owner or admin of the group
	var role string
	if err := h.db.Session().Query(`
		SELECT role FROM group_members WHERE group_id = ? AND user_id = ?
	`, groupUUID.String(), userID).Scan(&role); err != nil || (role != "owner" && role != "admin") {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied"})
		return
	}

	// Resolve group name
	var groupName string
	h.db.Session().Query(`SELECT name FROM groups WHERE org_id = ? AND group_id = ?`,
		orgID, groupUUID.String()).Scan(&groupName)

	repoName := c.PostForm("name")
	if repoName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}

	newLibID := uuid.New().String()
	now := time.Now()

	// Create library (owned by the requesting user on behalf of the group)
	h.db.Session().Query(`
		INSERT INTO libraries (org_id, library_id, owner_id, name, encrypted, size_bytes, file_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, orgID, newLibID, userID, repoName, false, int64(0), int64(0), now, now).Exec()

	h.db.Session().Query(`
		INSERT INTO libraries_by_id (library_id, org_id, owner_id, encrypted)
		VALUES (?, ?, ?, ?)
	`, newLibID, orgID, userID, false).Exec()

	// Share the library with the group
	shareID := uuid.New().String()
	h.db.Session().Query(`
		INSERT INTO shares (library_id, share_id, shared_by, shared_to, shared_to_type, permission, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, newLibID, shareID, userID, groupUUID.String(), "group", "rw", now).Exec()

	// Resolve caller email for the response's `owner` field
	var ownerEmail string
	h.db.Session().Query(`SELECT email FROM users WHERE org_id = ? AND user_id = ?`, orgID, userID).Scan(&ownerEmail)

	c.JSON(http.StatusOK, gin.H{
		"id":         newLibID,
		"name":       repoName,
		"group_name": groupName,
		"owner":      ownerEmail,
		"permission": "rw",
		"mtime":      now.Unix(),
		"size":       0,
		"encrypted":  false,
	})
}

// BulkAddGroupMembers adds multiple members to a group by comma-separated emails.
// POST /api/v2.1/groups/:group_id/members/bulk/
// FormData: emails (comma-separated)
func (h *GroupHandler) BulkAddGroupMembers(c *gin.Context) {
	groupID := c.Param("group_id")
	userID := c.GetString("user_id")
	orgID := c.GetString("org_id")

	groupUUID, err := uuid.Parse(groupID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group_id"})
		return
	}

	if h.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database not available"})
		return
	}

	// Verify caller is an owner or admin of the group
	var role string
	if err := h.db.Session().Query(`
		SELECT role FROM group_members WHERE group_id = ? AND user_id = ?
	`, groupUUID.String(), userID).Scan(&role); err != nil || (role != "owner" && role != "admin") {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied"})
		return
	}

	emailsRaw := c.PostForm("emails")
	if emailsRaw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "emails is required"})
		return
	}

	// Get group name once for lookup inserts
	var groupName string
	h.db.Session().Query(`SELECT name FROM groups WHERE org_id = ? AND group_id = ?`,
		orgID, groupUUID.String()).Scan(&groupName)

	type failedItem struct {
		Email    string `json:"email"`
		ErrorMsg string `json:"error_msg"`
	}

	var failed []failedItem
	var success []GroupMemberResponse
	now := time.Now()

	for _, email := range strings.Split(emailsRaw, ",") {
		email = strings.TrimSpace(email)
		if email == "" {
			continue
		}

		var memberID string
		if err := h.db.Session().Query(`
			SELECT user_id FROM users_by_email WHERE email = ?
		`, email).Scan(&memberID); err != nil {
			failed = append(failed, failedItem{Email: email, ErrorMsg: "user not found"})
			continue
		}

		h.db.Session().Query(`
			INSERT INTO group_members (group_id, user_id, role, added_at)
			VALUES (?, ?, ?, ?)
		`, groupUUID.String(), memberID, "member", now).Exec()

		h.db.Session().Query(`
			INSERT INTO groups_by_member (org_id, user_id, group_id, group_name, role, added_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, orgID, memberID, groupUUID.String(), groupName, "member", now).Exec()

		// Resolve user details for the response object
		var memberName string
		h.db.Session().Query(`SELECT name FROM users WHERE org_id = ? AND user_id = ?`, orgID, memberID).Scan(&memberName)

		success = append(success, GroupMemberResponse{
			Email:     email,
			Name:      memberName,
			UserID:    memberID,
			Role:      "Member",
			AddedAt:   now.Format(time.RFC3339),
			AvatarURL: "",
		})
	}

	if failed == nil {
		failed = []failedItem{}
	}
	if success == nil {
		success = []GroupMemberResponse{}
	}

	c.JSON(http.StatusOK, gin.H{"success": success, "failed": failed})
}

// ImportGroupMembersViaFile adds group members from an uploaded CSV/text file.
// POST /api/v2.1/groups/:group_id/members/import/
// multipart: file
func (h *GroupHandler) ImportGroupMembersViaFile(c *gin.Context) {
	groupID := c.Param("group_id")
	userID := c.GetString("user_id")
	orgID := c.GetString("org_id")

	groupUUID, err := uuid.Parse(groupID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group_id"})
		return
	}

	if h.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database not available"})
		return
	}

	// Verify caller is an owner or admin of the group
	var callerRole string
	if err := h.db.Session().Query(`
		SELECT role FROM group_members WHERE group_id = ? AND user_id = ?
	`, groupUUID.String(), userID).Scan(&callerRole); err != nil || (callerRole != "owner" && callerRole != "admin") {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied"})
		return
	}

	file, _, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	defer file.Close()

	var buf strings.Builder
	io.Copy(&buf, file) //nolint:errcheck

	var groupName string
	h.db.Session().Query(`SELECT name FROM groups WHERE org_id = ? AND group_id = ?`,
		orgID, groupUUID.String()).Scan(&groupName)

	type failedItem struct {
		Email    string `json:"email"`
		ErrorMsg string `json:"error_msg"`
	}

	var failed []failedItem
	var success []string
	now := time.Now()

	for _, line := range strings.Split(buf.String(), "\n") {
		email := strings.TrimSpace(line)
		if email == "" {
			continue
		}
		// Strip CSV quotes if any
		email = strings.Trim(email, `"`)
		email = strings.Trim(email, "'")
		if email == "" {
			continue
		}

		var memberID string
		if err := h.db.Session().Query(`
			SELECT user_id FROM users_by_email WHERE email = ?
		`, email).Scan(&memberID); err != nil {
			failed = append(failed, failedItem{Email: email, ErrorMsg: "user not found"})
			continue
		}

		h.db.Session().Query(`
			INSERT INTO group_members (group_id, user_id, role, added_at)
			VALUES (?, ?, ?, ?)
		`, groupUUID.String(), memberID, "member", now).Exec()

		h.db.Session().Query(`
			INSERT INTO groups_by_member (org_id, user_id, group_id, group_name, role, added_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, orgID, memberID, groupUUID.String(), groupName, "member", now).Exec()

		success = append(success, email)
	}

	if failed == nil {
		failed = []failedItem{}
	}
	if success == nil {
		success = []string{}
	}

	c.JSON(http.StatusOK, gin.H{"success": success, "failed": failed})
}

// SetGroupAdmin promotes or demotes a group member to/from admin role.
// PUT /api/v2.1/groups/:group_id/members/:user_email/
// Body (JSON): { "is_admin": "True" | "False" }
func (h *GroupHandler) SetGroupAdmin(c *gin.Context) {
	groupID := c.Param("group_id")
	userEmail := c.Param("user_email")
	orgID := c.GetString("org_id")
	callerID := c.GetString("user_id")

	groupUUID, err := uuid.Parse(groupID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group_id"})
		return
	}

	if h.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database not available"})
		return
	}

	// Verify caller is owner
	var callerRole string
	if err := h.db.Session().Query(`
		SELECT role FROM group_members WHERE group_id = ? AND user_id = ?
	`, groupUUID.String(), callerID).Scan(&callerRole); err != nil || callerRole != "owner" {
		c.JSON(http.StatusForbidden, gin.H{"error": "only group owner can change member roles"})
		return
	}

	// Resolve target member by email
	var memberID string
	if err := h.db.Session().Query(`
		SELECT user_id FROM users_by_email WHERE email = ?
	`, userEmail).Scan(&memberID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	// Determine new role from is_admin param
	var req struct {
		IsAdmin string `json:"is_admin" form:"is_admin"`
	}
	c.ShouldBind(&req) //nolint:errcheck
	newRole := "member"
	if strings.EqualFold(req.IsAdmin, "true") {
		newRole = "admin"
	}

	// Update group_members table
	if err := h.db.Session().Query(`
		UPDATE group_members SET role = ? WHERE group_id = ? AND user_id = ?
	`, newRole, groupUUID.String(), memberID).Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update member role"})
		return
	}

	// Update lookup table
	h.db.Session().Query(`
		UPDATE groups_by_member SET role = ? WHERE org_id = ? AND user_id = ? AND group_id = ?
	`, newRole, orgID, memberID, groupUUID.String()).Exec() //nolint:errcheck

	// Fetch member details for response
	var memberName string
	h.db.Session().Query(`SELECT name FROM users WHERE org_id = ? AND user_id = ?`, orgID, memberID).Scan(&memberName)

	// Fetch added_at for response
	var addedAt time.Time
	h.db.Session().Query(`SELECT added_at FROM group_members WHERE group_id = ? AND user_id = ?`,
		groupUUID.String(), memberID).Scan(&addedAt)

	c.JSON(http.StatusOK, GroupMemberResponse{
		Email:     userEmail,
		Name:      memberName,
		UserID:    memberID,
		Role:      capitalizeRole(newRole),
		AddedAt:   addedAt.Format(time.RFC3339),
		AvatarURL: "",
	})
}

// ListShareableGroups returns groups the authenticated user can share items with.
// This is the same as the user's groups but with the response format expected by
// the seafile-js shareableGroups() call.
// GET /api/v2.1/shareable-groups/
func (h *GroupHandler) ListShareableGroups(c *gin.Context) {
	userID := c.GetString("user_id")
	orgID := c.GetString("org_id")

	userUUID, err := uuid.Parse(userID)
	if err != nil {
		c.JSON(http.StatusOK, []interface{}{})
		return
	}
	orgUUID, err := uuid.Parse(orgID)
	if err != nil {
		c.JSON(http.StatusOK, []interface{}{})
		return
	}

	iter := h.db.Session().Query(`
		SELECT group_id, group_name
		FROM groups_by_member
		WHERE org_id = ? AND user_id = ?
	`, orgUUID.String(), userUUID.String()).Iter()

	type shareableGroup struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		ParentGroupID int    `json:"parent_group_id"`
	}

	var groups []shareableGroup
	var groupID, groupName string

	for iter.Scan(&groupID, &groupName) {
		groups = append(groups, shareableGroup{
			ID:            groupID,
			Name:          groupName,
			ParentGroupID: 0,
		})
	}

	if err := iter.Close(); err != nil {
		slog.Error("ListShareableGroups: failed to close iterator", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list groups"})
		return
	}

	if groups == nil {
		groups = []shareableGroup{}
	}

	c.JSON(http.StatusOK, groups)
}
