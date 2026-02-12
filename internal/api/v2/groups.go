package v2

import (
	"log/slog"
	"net/http"
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

		// Group members
		groups.GET("/:group_id/members", h.ListGroupMembers)
		groups.GET("/:group_id/members/", h.ListGroupMembers)
		groups.POST("/:group_id/members", h.AddGroupMember)
		groups.POST("/:group_id/members/", h.AddGroupMember)
		groups.DELETE("/:group_id/members/:user_email", h.RemoveGroupMember)
		groups.DELETE("/:group_id/members/:user_email/", h.RemoveGroupMember)
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
	GroupID    string `json:"id"`
	GroupName  string `json:"name"`
	Creator    string `json:"creator"`
	CreatorID  string `json:"creator_id"`
	CreatedAt  string `json:"created_at"`
	Parent     int    `json:"parent"`     // 0 for top-level groups
	Admins     []string `json:"admins"`   // List of admin emails
	MemberCount int    `json:"member_count,omitempty"`
}

// GroupMemberResponse represents a group member in API response
type GroupMemberResponse struct {
	Email     string `json:"email"`
	Name      string `json:"name"`
	UserID    string `json:"user_id"`
	Role      string `json:"role"`      // "owner", "admin", "member"
	AddedAt   string `json:"added_at"`
	AvatarURL string `json:"avatar_url"`
}

// CreateGroupRequest represents the request for creating a group
type CreateGroupRequest struct {
	GroupName string `json:"group_name" form:"group_name" binding:"required"`
}

// UpdateGroupRequest represents the request for updating a group
type UpdateGroupRequest struct {
	GroupName string `json:"group_name" form:"group_name"`
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
		if err := h.db.Session().Query(`SELECT email FROM users WHERE user_id = ?`, creatorID).Scan(&creatorEmail); err != nil {
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
	h.db.Session().Query(`SELECT email FROM users WHERE user_id = ?`, userID).Scan(&creatorEmail)

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
	h.db.Session().Query(`SELECT email FROM users WHERE user_id = ?`, creatorID).Scan(&creatorEmail)

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
		// Get user details
		var email, name string
		h.db.Session().Query(`SELECT email, name FROM users WHERE user_id = ?`, userID).Scan(&email, &name)

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
