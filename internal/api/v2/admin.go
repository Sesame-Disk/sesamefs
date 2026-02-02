package v2

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/config"
	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/Sesame-Disk/sesamefs/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AdminHandler handles platform admin API requests
type AdminHandler struct {
	db             *db.DB
	config         *config.Config
	permMiddleware *middleware.PermissionMiddleware
}

// NewAdminHandler creates a new AdminHandler
func NewAdminHandler(database *db.DB, cfg *config.Config, perm *middleware.PermissionMiddleware) *AdminHandler {
	return &AdminHandler{
		db:             database,
		config:         cfg,
		permMiddleware: perm,
	}
}

// RegisterAdminRoutes registers admin API routes under the given router group.
// All org CRUD endpoints require superadmin. User management allows tenant admin for own org.
func RegisterAdminRoutes(rg *gin.RouterGroup, database *db.DB, cfg *config.Config, perm *middleware.PermissionMiddleware) {
	h := NewAdminHandler(database, cfg, perm)

	admin := rg.Group("/admin")
	{
		// Organization management (superadmin only)
		superadminOnly := admin.Group("", perm.RequireSuperAdmin())
		{
			superadminOnly.GET("/organizations/", h.ListOrganizations)
			superadminOnly.GET("/organizations", h.ListOrganizations)
			superadminOnly.POST("/organizations/", h.CreateOrganization)
			superadminOnly.POST("/organizations", h.CreateOrganization)
			superadminOnly.GET("/organizations/:org_id/", h.GetOrganization)
			superadminOnly.GET("/organizations/:org_id", h.GetOrganization)
			superadminOnly.PUT("/organizations/:org_id/", h.UpdateOrganization)
			superadminOnly.PUT("/organizations/:org_id", h.UpdateOrganization)
			superadminOnly.DELETE("/organizations/:org_id/", h.DeactivateOrganization)
			superadminOnly.DELETE("/organizations/:org_id", h.DeactivateOrganization)
		}

		// User listing per org (superadmin or tenant admin for own org)
		admin.GET("/organizations/:org_id/users/", h.ListOrgUsers)
		admin.GET("/organizations/:org_id/users", h.ListOrgUsers)

		// User management — register a middleware on the /users group that
		// intercepts all requests and dispatches based on path.
		admin.Any("/users", h.adminUsersHandler)
		admin.Any("/users/*path", h.adminUsersHandler)

		// Admin group management (admin or above)
		admin.GET("/groups/", h.ListAllGroups)
		admin.GET("/groups", h.ListAllGroups)
		admin.POST("/groups/", h.AdminCreateGroup)
		admin.POST("/groups", h.AdminCreateGroup)
		admin.DELETE("/groups/:group_id/", h.AdminDeleteGroup)
		admin.DELETE("/groups/:group_id", h.AdminDeleteGroup)
		admin.PUT("/groups/:group_id/", h.AdminTransferGroup)
		admin.PUT("/groups/:group_id", h.AdminTransferGroup)
		admin.GET("/groups/:group_id/members/", h.AdminListGroupMembers)
		admin.GET("/groups/:group_id/members", h.AdminListGroupMembers)
		admin.POST("/groups/:group_id/members/", h.AdminAddGroupMember)
		admin.POST("/groups/:group_id/members", h.AdminAddGroupMember)
		admin.DELETE("/groups/:group_id/members/:email/", h.AdminRemoveGroupMember)
		admin.DELETE("/groups/:group_id/members/:email", h.AdminRemoveGroupMember)
		admin.GET("/groups/:group_id/libraries/", h.AdminListGroupLibraries)
		admin.GET("/groups/:group_id/libraries", h.AdminListGroupLibraries)
		admin.GET("/search-group/", h.SearchGroups)
		admin.GET("/search-group", h.SearchGroups)

		// Admin user search & admin listing
		admin.GET("/search-user/", h.SearchUsers)
		admin.GET("/search-user", h.SearchUsers)
		admin.GET("/admins/", h.ListAdminUsers)
		admin.GET("/admins", h.ListAdminUsers)
	}
}

// ListOrganizations returns all organizations (superadmin only, enforced by middleware)
// GET /admin/organizations/
func (h *AdminHandler) ListOrganizations(c *gin.Context) {
	type orgResponse struct {
		OrgID        string            `json:"org_id"`
		Name         string            `json:"name"`
		StorageQuota int64             `json:"storage_quota"`
		StorageUsed  int64             `json:"storage_used"`
		Settings     map[string]string `json:"settings,omitempty"`
		CreatedAt    time.Time         `json:"created_at"`
	}

	iter := h.db.Session().Query(`
		SELECT org_id, name, storage_quota, storage_used, settings, created_at
		FROM organizations
	`).Iter()

	var orgs []orgResponse
	var orgID, name string
	var storageQuota, storageUsed int64
	var settings map[string]string
	var createdAt time.Time

	for iter.Scan(&orgID, &name, &storageQuota, &storageUsed, &settings, &createdAt) {
		orgs = append(orgs, orgResponse{
			OrgID:        orgID,
			Name:         name,
			StorageQuota: storageQuota,
			StorageUsed:  storageUsed,
			Settings:     settings,
			CreatedAt:    createdAt,
		})
	}
	if err := iter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list organizations"})
		return
	}

	if orgs == nil {
		orgs = []orgResponse{}
	}

	c.JSON(http.StatusOK, gin.H{"organizations": orgs})
}

// CreateOrganization creates a new organization (superadmin only)
// POST /admin/organizations/
func (h *AdminHandler) CreateOrganization(c *gin.Context) {
	var req struct {
		Name         string `json:"name" binding:"required"`
		StorageQuota int64  `json:"storage_quota"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}

	orgID := uuid.New()
	now := time.Now()

	if req.StorageQuota == 0 {
		req.StorageQuota = 1099511627776 // 1TB default
	}

	settings := map[string]string{
		"theme":    "default",
		"features": "all",
	}
	storageConfig := map[string]string{
		"default_backend": "s3",
	}

	err := h.db.Session().Query(`
		INSERT INTO organizations (
			org_id, name, settings, storage_quota, storage_used,
			chunking_polynomial, storage_config, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		orgID.String(), req.Name, settings,
		req.StorageQuota, int64(0), int64(17592186044415),
		storageConfig, now,
	).Exec()

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create organization"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"org_id":        orgID.String(),
		"name":          req.Name,
		"storage_quota": req.StorageQuota,
		"created_at":    now,
	})
}

// GetOrganization returns details for a single organization (superadmin only)
// GET /admin/organizations/:org_id/
func (h *AdminHandler) GetOrganization(c *gin.Context) {
	orgID := c.Param("org_id")

	var name string
	var storageQuota, storageUsed int64
	var settings map[string]string
	var createdAt time.Time

	err := h.db.Session().Query(`
		SELECT name, storage_quota, storage_used, settings, created_at
		FROM organizations WHERE org_id = ?
	`, orgID).Scan(&name, &storageQuota, &storageUsed, &settings, &createdAt)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "organization not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"org_id":        orgID,
		"name":          name,
		"storage_quota": storageQuota,
		"storage_used":  storageUsed,
		"settings":      settings,
		"created_at":    createdAt,
	})
}

// UpdateOrganization updates an organization (superadmin only)
// PUT /admin/organizations/:org_id/
func (h *AdminHandler) UpdateOrganization(c *gin.Context) {
	orgID := c.Param("org_id")

	var req struct {
		Name         *string `json:"name"`
		StorageQuota *int64  `json:"storage_quota"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	// Verify org exists
	var existingName string
	err := h.db.Session().Query(`
		SELECT name FROM organizations WHERE org_id = ?
	`, orgID).Scan(&existingName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "organization not found"})
		return
	}

	if req.Name != nil {
		if err := h.db.Session().Query(`
			UPDATE organizations SET name = ? WHERE org_id = ?
		`, *req.Name, orgID).Exec(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update organization"})
			return
		}
	}

	if req.StorageQuota != nil {
		if err := h.db.Session().Query(`
			UPDATE organizations SET storage_quota = ? WHERE org_id = ?
		`, *req.StorageQuota, orgID).Exec(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update organization"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// DeactivateOrganization deactivates an organization (superadmin only)
// DELETE /admin/organizations/:org_id/
func (h *AdminHandler) DeactivateOrganization(c *gin.Context) {
	orgID := c.Param("org_id")

	// Don't allow deactivating the platform org
	if orgID == middleware.PlatformOrgID {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot deactivate platform organization"})
		return
	}

	// Verify org exists
	var name string
	err := h.db.Session().Query(`
		SELECT name FROM organizations WHERE org_id = ?
	`, orgID).Scan(&name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "organization not found"})
		return
	}

	// Mark org as deactivated by setting a setting
	if err := h.db.Session().Query(`
		UPDATE organizations SET settings['status'] = ? WHERE org_id = ?
	`, "deactivated", orgID).Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to deactivate organization"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ListOrgUsers lists users in an organization.
// Superadmin can list any org. Tenant admin can list their own org.
// GET /admin/organizations/:org_id/users/
func (h *AdminHandler) ListOrgUsers(c *gin.Context) {
	targetOrgID := c.Param("org_id")
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")

	// Check permissions: superadmin can access any org, admin can access own org
	if callerOrgID != middleware.PlatformOrgID {
		// Not a platform user — must be admin of the target org
		if callerOrgID != targetOrgID {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}
		role, err := h.permMiddleware.GetUserOrgRole(callerOrgID, callerUserID)
		if err != nil || !isAdminOrAbove(role) {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}
	} else {
		// Platform user — must be superadmin
		role, err := h.permMiddleware.GetUserOrgRole(callerOrgID, callerUserID)
		if err != nil || role != middleware.RoleSuperAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}
	}

	type userResponse struct {
		UserID     string    `json:"user_id"`
		Email      string    `json:"email"`
		Name       string    `json:"name"`
		Role       string    `json:"role"`
		QuotaBytes int64     `json:"quota_bytes"`
		UsedBytes  int64     `json:"used_bytes"`
		CreatedAt  time.Time `json:"created_at"`
	}

	iter := h.db.Session().Query(`
		SELECT user_id, email, name, role, quota_bytes, used_bytes, created_at
		FROM users WHERE org_id = ?
	`, targetOrgID).Iter()

	var users []userResponse
	var userID, email, name, role string
	var quotaBytes, usedBytes int64
	var createdAt time.Time

	for iter.Scan(&userID, &email, &name, &role, &quotaBytes, &usedBytes, &createdAt) {
		users = append(users, userResponse{
			UserID:     userID,
			Email:      email,
			Name:       name,
			Role:       role,
			QuotaBytes: quotaBytes,
			UsedBytes:  usedBytes,
			CreatedAt:  createdAt,
		})
	}
	if err := iter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list users"})
		return
	}

	if users == nil {
		users = []userResponse{}
	}

	c.JSON(http.StatusOK, gin.H{"users": users})
}

// adminUsersHandler dispatches /admin/users and /admin/users/* requests.
// Gin's httprouter can't register both /users/ (static) and /users/:param
// at the same level, so we use Any() + wildcard and dispatch manually.
func (h *AdminHandler) adminUsersHandler(c *gin.Context) {
	path := strings.Trim(c.Param("path"), "/")

	switch c.Request.Method {
	case "GET":
		if path == "" {
			h.ListAllUsers(c)
		} else {
			c.Set("resolved_user_param", path)
			h.GetUser(c)
		}
	case "POST":
		if path == "" {
			h.AdminCreateUser(c)
		} else {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		}
	case "PUT":
		if path == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "user identifier required"})
		} else {
			c.Set("resolved_user_param", path)
			h.UpdateUser(c)
		}
	case "DELETE":
		if path == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "user identifier required"})
		} else {
			c.Set("resolved_user_param", path)
			h.DeactivateUser(c)
		}
	default:
		c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "method not allowed"})
	}
}

// getResolvedUserParam returns the user identifier from either the wildcard
// router's resolved param or the legacy :user_id param.
func (h *AdminHandler) getResolvedUserParam(c *gin.Context) string {
	if v, exists := c.Get("resolved_user_param"); exists {
		return v.(string)
	}
	return c.Param("user_id")
}

// GetUser returns details for a single user.
// GET /admin/users/:user_id/
// If :user_id contains an @ sign, it's treated as an email lookup (seafile-js compatible).
func (h *AdminHandler) GetUser(c *gin.Context) {
	targetUserID := h.getResolvedUserParam(c)

	// Dispatch to email-based handler if the param looks like an email
	if strings.Contains(targetUserID, "@") {
		h.GetUserByEmail(c, targetUserID)
		return
	}

	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")

	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	// Try to find the user - we need their org_id
	var email, name, role, userOrgID string
	var quotaBytes, usedBytes int64
	var createdAt time.Time

	if callerOrgID == middleware.PlatformOrgID {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "use /admin/organizations/:org_id/users/ to list users by org"})
		return
	}

	// Tenant admin: look up in own org
	err := h.db.Session().Query(`
		SELECT email, name, role, quota_bytes, used_bytes, created_at
		FROM users WHERE org_id = ? AND user_id = ?
	`, callerOrgID, targetUserID).Scan(&email, &name, &role, &quotaBytes, &usedBytes, &createdAt)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	userOrgID = callerOrgID

	c.JSON(http.StatusOK, gin.H{
		"user_id":     targetUserID,
		"org_id":      userOrgID,
		"email":       email,
		"name":        name,
		"role":        role,
		"quota_bytes": quotaBytes,
		"used_bytes":  usedBytes,
		"created_at":  createdAt,
	})
}

// UpdateUser updates a user's role, status, or quota.
// PUT /admin/users/:user_id/
// If :user_id contains an @ sign, it's treated as an email lookup (seafile-js compatible).
func (h *AdminHandler) UpdateUser(c *gin.Context) {
	targetUserID := h.getResolvedUserParam(c)

	// Dispatch to email-based handler if the param looks like an email
	if strings.Contains(targetUserID, "@") {
		h.UpdateUserByEmail(c, targetUserID)
		return
	}

	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")

	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	var req struct {
		Role       *string `json:"role"`
		QuotaBytes *int64  `json:"quota_bytes"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	// Tenant admin can only update users in their own org
	orgID := callerOrgID

	// Validate role if provided
	if req.Role != nil {
		validRoles := map[string]bool{"admin": true, "user": true, "readonly": true, "guest": true}
		// Only superadmin can assign superadmin role
		if *req.Role == "superadmin" {
			role, _ := h.permMiddleware.GetUserOrgRole(callerOrgID, callerUserID)
			if role != middleware.RoleSuperAdmin {
				c.JSON(http.StatusForbidden, gin.H{"error": "only superadmin can assign superadmin role"})
				return
			}
			validRoles["superadmin"] = true
		}
		if !validRoles[*req.Role] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role"})
			return
		}
		if err := h.db.Session().Query(`
			UPDATE users SET role = ? WHERE org_id = ? AND user_id = ?
		`, *req.Role, orgID, targetUserID).Exec(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update user"})
			return
		}
	}

	if req.QuotaBytes != nil {
		if err := h.db.Session().Query(`
			UPDATE users SET quota_bytes = ? WHERE org_id = ? AND user_id = ?
		`, *req.QuotaBytes, orgID, targetUserID).Exec(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update user"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// DeactivateUser deactivates a user.
// DELETE /admin/users/:user_id/
// If :user_id contains an @ sign, it's treated as an email lookup (seafile-js compatible).
func (h *AdminHandler) DeactivateUser(c *gin.Context) {
	targetUserID := h.getResolvedUserParam(c)

	// Dispatch to email-based handler if the param looks like an email
	if strings.Contains(targetUserID, "@") {
		h.DeleteUserByEmail(c, targetUserID)
		return
	}

	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")

	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	// Don't allow deactivating yourself
	if targetUserID == callerUserID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot deactivate your own account"})
		return
	}

	orgID := callerOrgID

	// Set role to "deactivated" to disable the user
	if err := h.db.Session().Query(`
		UPDATE users SET role = ? WHERE org_id = ? AND user_id = ?
	`, "deactivated", orgID, targetUserID).Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to deactivate user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// requireAdminAccess checks that the caller is superadmin or tenant admin.
// Returns a non-nil error (and writes the response) if not authorized.
func (h *AdminHandler) requireAdminAccess(c *gin.Context, callerOrgID, callerUserID string) error {
	role, err := h.permMiddleware.GetUserOrgRole(callerOrgID, callerUserID)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		c.Abort()
		return err
	}
	if !isAdminOrAbove(role) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		c.Abort()
		return fmt.Errorf("insufficient permissions: role %s", role)
	}
	return nil
}

// isAdminOrAbove returns true if the role is admin or superadmin
func isAdminOrAbove(role middleware.OrganizationRole) bool {
	return role == middleware.RoleAdmin || role == middleware.RoleSuperAdmin
}

// lookupUserByEmail finds a user's user_id and org_id by email via the users_by_email table.
func (h *AdminHandler) lookupUserByEmail(email string) (userID, orgID string, err error) {
	err = h.db.Session().Query(`
		SELECT user_id, org_id FROM users_by_email WHERE email = ?
	`, email).Scan(&userID, &orgID)
	return
}

// =============================================================================
// Phase 1: Admin Group Endpoints
// =============================================================================

// adminGroupResponse matches the seafile-js expected group object format.
type adminGroupResponse struct {
	ID            string `json:"id"`
	GroupName     string `json:"group_name"`
	Owner         string `json:"owner"`
	CreatedAt     string `json:"created_at"`
	MemberCount   int    `json:"member_count"`
	ParentGroupID int    `json:"parent_group_id"`
}

// ListAllGroups returns all groups in the caller's org with pagination.
// GET /admin/groups/?page=N&per_page=N
func (h *AdminHandler) ListAllGroups(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")

	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "25"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 25
	}

	// For superadmin, list all orgs' groups; for tenant admin, list own org
	orgID := callerOrgID

	iter := h.db.Session().Query(`
		SELECT group_id, name, creator_id, created_at FROM groups WHERE org_id = ?
	`, orgID).Iter()

	var allGroups []adminGroupResponse
	var groupID, name, creatorID string
	var createdAt time.Time

	for iter.Scan(&groupID, &name, &creatorID, &createdAt) {
		// Get creator email
		var ownerEmail string
		h.db.Session().Query(`
			SELECT email FROM users WHERE org_id = ? AND user_id = ?
		`, orgID, creatorID).Scan(&ownerEmail)

		// Count members
		var memberCount int
		h.db.Session().Query(`SELECT COUNT(*) FROM group_members WHERE group_id = ?`, groupID).Scan(&memberCount)

		allGroups = append(allGroups, adminGroupResponse{
			ID:            groupID,
			GroupName:     name,
			Owner:         ownerEmail,
			CreatedAt:     createdAt.Format(time.RFC3339),
			MemberCount:   memberCount,
			ParentGroupID: 0,
		})
	}
	if err := iter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list groups"})
		return
	}

	// Paginate
	total := len(allGroups)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	pageGroups := allGroups[start:end]
	if pageGroups == nil {
		pageGroups = []adminGroupResponse{}
	}

	c.JSON(http.StatusOK, gin.H{
		"groups": pageGroups,
		"page_info": gin.H{
			"has_next_page": end < total,
			"current_page":  page,
		},
	})
}

// SearchGroups searches groups by name.
// GET /admin/search-group/?query=name
func (h *AdminHandler) SearchGroups(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")

	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	query := strings.ToLower(c.Query("query"))
	if query == "" {
		c.JSON(http.StatusOK, gin.H{"groups": []adminGroupResponse{}})
		return
	}

	orgID := callerOrgID

	iter := h.db.Session().Query(`
		SELECT group_id, name, creator_id, created_at FROM groups WHERE org_id = ?
	`, orgID).Iter()

	var results []adminGroupResponse
	var groupID, name, creatorID string
	var createdAt time.Time

	for iter.Scan(&groupID, &name, &creatorID, &createdAt) {
		if !strings.Contains(strings.ToLower(name), query) {
			continue
		}

		var ownerEmail string
		h.db.Session().Query(`
			SELECT email FROM users WHERE org_id = ? AND user_id = ?
		`, orgID, creatorID).Scan(&ownerEmail)

		var memberCount int
		h.db.Session().Query(`SELECT COUNT(*) FROM group_members WHERE group_id = ?`, groupID).Scan(&memberCount)

		results = append(results, adminGroupResponse{
			ID:            groupID,
			GroupName:     name,
			Owner:         ownerEmail,
			CreatedAt:     createdAt.Format(time.RFC3339),
			MemberCount:   memberCount,
			ParentGroupID: 0,
		})
	}
	iter.Close()

	if results == nil {
		results = []adminGroupResponse{}
	}

	c.JSON(http.StatusOK, gin.H{"groups": results})
}

// AdminCreateGroup creates a new group as admin.
// POST /admin/groups/ (FormData: group_name, group_owner)
func (h *AdminHandler) AdminCreateGroup(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")

	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	groupName := c.Request.FormValue("group_name")
	groupOwnerEmail := c.Request.FormValue("group_owner")

	if groupName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "group_name is required"})
		return
	}

	orgID := callerOrgID

	// Resolve owner: use provided email or fallback to caller
	var ownerID, ownerEmail string
	if groupOwnerEmail != "" {
		var ownerOrgID string
		var err error
		ownerID, ownerOrgID, err = h.lookupUserByEmail(groupOwnerEmail)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "group_owner not found"})
			return
		}
		if ownerOrgID != orgID {
			c.JSON(http.StatusBadRequest, gin.H{"error": "group_owner must be in the same organization"})
			return
		}
		ownerEmail = groupOwnerEmail
	} else {
		ownerID = callerUserID
		h.db.Session().Query(`
			SELECT email FROM users WHERE org_id = ? AND user_id = ?
		`, orgID, callerUserID).Scan(&ownerEmail)
	}

	groupUUID := uuid.New()
	now := time.Now()

	// Insert group
	if err := h.db.Session().Query(`
		INSERT INTO groups (org_id, group_id, name, creator_id, is_department, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, orgID, groupUUID.String(), groupName, ownerID, false, now, now).Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create group"})
		return
	}

	// Add owner as member
	if err := h.db.Session().Query(`
		INSERT INTO group_members (group_id, user_id, role, added_at)
		VALUES (?, ?, ?, ?)
	`, groupUUID.String(), ownerID, "owner", now).Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add owner as member"})
		return
	}

	// Add to lookup table
	h.db.Session().Query(`
		INSERT INTO groups_by_member (org_id, user_id, group_id, group_name, role, added_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, orgID, ownerID, groupUUID.String(), groupName, "owner", now).Exec()

	c.JSON(http.StatusCreated, adminGroupResponse{
		ID:            groupUUID.String(),
		GroupName:     groupName,
		Owner:         ownerEmail,
		CreatedAt:     now.Format(time.RFC3339),
		MemberCount:   1,
		ParentGroupID: 0,
	})
}

// AdminDeleteGroup deletes a group.
// DELETE /admin/groups/:group_id/
func (h *AdminHandler) AdminDeleteGroup(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")

	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	groupID := c.Param("group_id")
	orgID := callerOrgID

	// Verify group exists
	var name string
	if err := h.db.Session().Query(`
		SELECT name FROM groups WHERE org_id = ? AND group_id = ?
	`, orgID, groupID).Scan(&name); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
		return
	}

	// Delete group
	h.db.Session().Query(`DELETE FROM groups WHERE org_id = ? AND group_id = ?`, orgID, groupID).Exec()

	// Delete all members
	h.db.Session().Query(`DELETE FROM group_members WHERE group_id = ?`, groupID).Exec()

	// Clean up lookup table entries for all members in this group in this org
	// Note: Can't efficiently delete from groups_by_member without knowing user_ids,
	// but the Cassandra partition is (org_id, user_id) so we'd need to scan.
	// For now, we delete what we can - the lookup entries become stale but harmless.

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// AdminTransferGroup transfers group ownership.
// PUT /admin/groups/:group_id/ (FormData: new_owner)
func (h *AdminHandler) AdminTransferGroup(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")

	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	groupID := c.Param("group_id")
	newOwnerEmail := c.Request.FormValue("new_owner")
	orgID := callerOrgID

	if newOwnerEmail == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "new_owner is required"})
		return
	}

	// Verify group exists
	var creatorID string
	if err := h.db.Session().Query(`
		SELECT creator_id FROM groups WHERE org_id = ? AND group_id = ?
	`, orgID, groupID).Scan(&creatorID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
		return
	}

	// Resolve new owner
	newOwnerID, newOwnerOrgID, err := h.lookupUserByEmail(newOwnerEmail)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "new_owner not found"})
		return
	}
	if newOwnerOrgID != orgID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "new_owner must be in the same organization"})
		return
	}

	now := time.Now()

	// Update group creator
	h.db.Session().Query(`
		UPDATE groups SET creator_id = ?, updated_at = ? WHERE org_id = ? AND group_id = ?
	`, newOwnerID, now, orgID, groupID).Exec()

	// Demote old owner to member in group_members
	h.db.Session().Query(`
		UPDATE group_members SET role = ? WHERE group_id = ? AND user_id = ?
	`, "member", groupID, creatorID).Exec()

	// Add new owner as owner (upsert)
	h.db.Session().Query(`
		INSERT INTO group_members (group_id, user_id, role, added_at)
		VALUES (?, ?, ?, ?)
	`, groupID, newOwnerID, "owner", now).Exec()

	// Update lookup tables
	var groupName string
	h.db.Session().Query(`SELECT name FROM groups WHERE org_id = ? AND group_id = ?`, orgID, groupID).Scan(&groupName)

	h.db.Session().Query(`
		INSERT INTO groups_by_member (org_id, user_id, group_id, group_name, role, added_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, orgID, newOwnerID, groupID, groupName, "owner", now).Exec()

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// AdminListGroupMembers lists members of a group.
// GET /admin/groups/:group_id/members/
func (h *AdminHandler) AdminListGroupMembers(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")

	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	groupID := c.Param("group_id")
	orgID := callerOrgID

	// Verify group exists
	var name string
	if err := h.db.Session().Query(`
		SELECT name FROM groups WHERE org_id = ? AND group_id = ?
	`, orgID, groupID).Scan(&name); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
		return
	}

	iter := h.db.Session().Query(`
		SELECT user_id, role, added_at FROM group_members WHERE group_id = ?
	`, groupID).Iter()

	type memberResponse struct {
		Email     string `json:"email"`
		Name      string `json:"name"`
		Role      string `json:"role"`
		AvatarURL string `json:"avatar_url"`
	}

	var members []memberResponse
	var userID, role string
	var addedAt time.Time

	for iter.Scan(&userID, &role, &addedAt) {
		var email, uname string
		h.db.Session().Query(`
			SELECT email, name FROM users WHERE org_id = ? AND user_id = ?
		`, orgID, userID).Scan(&email, &uname)

		members = append(members, memberResponse{
			Email:     email,
			Name:      uname,
			Role:      role,
			AvatarURL: "",
		})
	}
	iter.Close()

	if members == nil {
		members = []memberResponse{}
	}

	c.JSON(http.StatusOK, members)
}

// AdminAddGroupMember adds a member to a group.
// POST /admin/groups/:group_id/members/ (FormData: email)
func (h *AdminHandler) AdminAddGroupMember(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")

	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	groupID := c.Param("group_id")
	email := c.Request.FormValue("email")
	orgID := callerOrgID

	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email is required"})
		return
	}

	// Verify group exists
	var groupName string
	if err := h.db.Session().Query(`
		SELECT name FROM groups WHERE org_id = ? AND group_id = ?
	`, orgID, groupID).Scan(&groupName); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
		return
	}

	// Resolve user by email
	memberID, memberOrgID, err := h.lookupUserByEmail(email)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if memberOrgID != orgID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must be in the same organization"})
		return
	}

	now := time.Now()

	// Add to group_members
	if err := h.db.Session().Query(`
		INSERT INTO group_members (group_id, user_id, role, added_at)
		VALUES (?, ?, ?, ?)
	`, groupID, memberID, "member", now).Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add member"})
		return
	}

	// Add to lookup table
	h.db.Session().Query(`
		INSERT INTO groups_by_member (org_id, user_id, group_id, group_name, role, added_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, orgID, memberID, groupID, groupName, "member", now).Exec()

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// AdminRemoveGroupMember removes a member from a group.
// DELETE /admin/groups/:group_id/members/:email/
func (h *AdminHandler) AdminRemoveGroupMember(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")

	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	groupID := c.Param("group_id")
	email := c.Param("email")
	orgID := callerOrgID

	// Verify group exists
	if err := h.db.Session().Query(`
		SELECT name FROM groups WHERE org_id = ? AND group_id = ?
	`, orgID, groupID).Scan(new(string)); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
		return
	}

	// Resolve user by email
	memberID, _, err := h.lookupUserByEmail(email)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	// Delete from group_members
	h.db.Session().Query(`
		DELETE FROM group_members WHERE group_id = ? AND user_id = ?
	`, groupID, memberID).Exec()

	// Delete from lookup table
	h.db.Session().Query(`
		DELETE FROM groups_by_member WHERE org_id = ? AND user_id = ? AND group_id = ?
	`, orgID, memberID, groupID).Exec()

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// AdminListGroupLibraries lists libraries shared with a group.
// GET /admin/groups/:group_id/libraries/
func (h *AdminHandler) AdminListGroupLibraries(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")

	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	// No group-to-library sharing table exists yet; return empty list.
	c.JSON(http.StatusOK, gin.H{"libraries": []interface{}{}})
}

// =============================================================================
// Phase 2: Admin User Endpoints (email-based, seafile-js compatible)
// =============================================================================

// adminUserResponse matches the seafile-js expected user object format.
type adminUserResponse struct {
	Email      string `json:"email"`
	Name       string `json:"name"`
	IsActive   bool   `json:"is_active"`
	IsStaff    bool   `json:"is_staff"`
	Role       string `json:"role"`
	QuotaTotal int64  `json:"quota_total"`
	QuotaUsage int64  `json:"quota_usage"`
	CreateTime string `json:"create_time"`
	LastLogin  string `json:"last_login"`
}

func makeAdminUserResponse(email, name, role string, quotaBytes, usedBytes int64, createdAt time.Time) adminUserResponse {
	return adminUserResponse{
		Email:      email,
		Name:       name,
		IsActive:   role != "deactivated",
		IsStaff:    role == "admin" || role == "superadmin",
		Role:       role,
		QuotaTotal: quotaBytes,
		QuotaUsage: usedBytes,
		CreateTime: createdAt.Format(time.RFC3339),
		LastLogin:  "", // Not tracked currently
	}
}

// ListAllUsers lists all users in the caller's org with pagination.
// GET /admin/users/?page=N&per_page=N
func (h *AdminHandler) ListAllUsers(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")

	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "25"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 25
	}

	orgID := callerOrgID

	iter := h.db.Session().Query(`
		SELECT user_id, email, name, role, quota_bytes, used_bytes, created_at
		FROM users WHERE org_id = ?
	`, orgID).Iter()

	var allUsers []adminUserResponse
	var userID, email, name, role string
	var quotaBytes, usedBytes int64
	var createdAt time.Time

	for iter.Scan(&userID, &email, &name, &role, &quotaBytes, &usedBytes, &createdAt) {
		allUsers = append(allUsers, makeAdminUserResponse(email, name, role, quotaBytes, usedBytes, createdAt))
	}
	if err := iter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list users"})
		return
	}

	// Paginate
	total := len(allUsers)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	pageUsers := allUsers[start:end]
	if pageUsers == nil {
		pageUsers = []adminUserResponse{}
	}

	c.JSON(http.StatusOK, gin.H{
		"data":        pageUsers,
		"total_count": total,
	})
}

// SearchUsers searches users by email or name.
// GET /admin/search-user/?query=...
func (h *AdminHandler) SearchUsers(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")

	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	query := strings.ToLower(c.Query("query"))
	if query == "" {
		c.JSON(http.StatusOK, gin.H{"users": []adminUserResponse{}})
		return
	}

	orgID := callerOrgID

	iter := h.db.Session().Query(`
		SELECT user_id, email, name, role, quota_bytes, used_bytes, created_at
		FROM users WHERE org_id = ?
	`, orgID).Iter()

	var results []adminUserResponse
	var userID, email, name, role string
	var quotaBytes, usedBytes int64
	var createdAt time.Time

	for iter.Scan(&userID, &email, &name, &role, &quotaBytes, &usedBytes, &createdAt) {
		if strings.Contains(strings.ToLower(email), query) || strings.Contains(strings.ToLower(name), query) {
			results = append(results, makeAdminUserResponse(email, name, role, quotaBytes, usedBytes, createdAt))
		}
	}
	iter.Close()

	if results == nil {
		results = []adminUserResponse{}
	}

	c.JSON(http.StatusOK, gin.H{"users": results})
}

// AdminCreateUser creates a new user via admin API.
// POST /admin/users/ (FormData: email, name, password)
func (h *AdminHandler) AdminCreateUser(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")

	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	email := c.Request.FormValue("email")
	name := c.Request.FormValue("name")

	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email is required"})
		return
	}
	if name == "" {
		name = email
	}

	orgID := callerOrgID

	// Check if user already exists
	var existingUserID string
	if err := h.db.Session().Query(`
		SELECT user_id FROM users_by_email WHERE email = ?
	`, email).Scan(&existingUserID); err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user with this email already exists"})
		return
	}

	userID := uuid.New().String()
	now := time.Now()

	// Create user record
	if err := h.db.Session().Query(`
		INSERT INTO users (org_id, user_id, email, name, role, quota_bytes, used_bytes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, orgID, userID, email, name, "user", int64(-2), int64(0), now).Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
		return
	}

	// Create email lookup
	h.db.Session().Query(`
		INSERT INTO users_by_email (email, user_id, org_id)
		VALUES (?, ?, ?)
	`, email, userID, orgID).Exec()

	c.JSON(http.StatusCreated, makeAdminUserResponse(email, name, "user", -2, 0, now))
}

// GetUserByEmail returns user details by email.
// GET /admin/users/:email/
// Note: This handler is reached when the :user_id param contains an @ sign (email).
// The existing GetUser handles UUID-based lookups.
func (h *AdminHandler) GetUserByEmail(c *gin.Context, email string) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")

	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	userID, userOrgID, err := h.lookupUserByEmail(email)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	var name, role string
	var quotaBytes, usedBytes int64
	var createdAt time.Time

	if err := h.db.Session().Query(`
		SELECT name, role, quota_bytes, used_bytes, created_at
		FROM users WHERE org_id = ? AND user_id = ?
	`, userOrgID, userID).Scan(&name, &role, &quotaBytes, &usedBytes, &createdAt); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	c.JSON(http.StatusOK, makeAdminUserResponse(email, name, role, quotaBytes, usedBytes, createdAt))
}

// UpdateUserByEmail updates a user by email.
// PUT /admin/users/:email/ (FormData: role, name, quota_total, is_active)
func (h *AdminHandler) UpdateUserByEmail(c *gin.Context, email string) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")

	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	userID, userOrgID, err := h.lookupUserByEmail(email)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	// Read form values
	newRole := c.Request.FormValue("role")
	newName := c.Request.FormValue("name")
	quotaStr := c.Request.FormValue("quota_total")
	isActiveStr := c.Request.FormValue("is_active")

	if newRole != "" {
		validRoles := map[string]bool{"admin": true, "user": true, "readonly": true, "guest": true}
		if newRole == "superadmin" {
			role, _ := h.permMiddleware.GetUserOrgRole(callerOrgID, callerUserID)
			if role != middleware.RoleSuperAdmin {
				c.JSON(http.StatusForbidden, gin.H{"error": "only superadmin can assign superadmin role"})
				return
			}
			validRoles["superadmin"] = true
		}
		if !validRoles[newRole] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role"})
			return
		}
		h.db.Session().Query(`
			UPDATE users SET role = ? WHERE org_id = ? AND user_id = ?
		`, newRole, userOrgID, userID).Exec()
	}

	if newName != "" {
		h.db.Session().Query(`
			UPDATE users SET name = ? WHERE org_id = ? AND user_id = ?
		`, newName, userOrgID, userID).Exec()
	}

	if quotaStr != "" {
		if quota, err := strconv.ParseInt(quotaStr, 10, 64); err == nil {
			h.db.Session().Query(`
				UPDATE users SET quota_bytes = ? WHERE org_id = ? AND user_id = ?
			`, quota, userOrgID, userID).Exec()
		}
	}

	if isActiveStr == "false" {
		h.db.Session().Query(`
			UPDATE users SET role = ? WHERE org_id = ? AND user_id = ?
		`, "deactivated", userOrgID, userID).Exec()
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// DeleteUserByEmail deactivates a user by email.
// DELETE /admin/users/:email/
func (h *AdminHandler) DeleteUserByEmail(c *gin.Context, email string) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")

	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	userID, userOrgID, err := h.lookupUserByEmail(email)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	// Don't allow deactivating yourself
	if userID == callerUserID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot deactivate your own account"})
		return
	}

	h.db.Session().Query(`
		UPDATE users SET role = ? WHERE org_id = ? AND user_id = ?
	`, "deactivated", userOrgID, userID).Exec()

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ListAdminUsers lists users with admin or superadmin role.
// GET /admin/admins/
func (h *AdminHandler) ListAdminUsers(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")

	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	orgID := callerOrgID

	iter := h.db.Session().Query(`
		SELECT user_id, email, name, role, quota_bytes, used_bytes, created_at
		FROM users WHERE org_id = ?
	`, orgID).Iter()

	var admins []adminUserResponse
	var userID, email, name, role string
	var quotaBytes, usedBytes int64
	var createdAt time.Time

	for iter.Scan(&userID, &email, &name, &role, &quotaBytes, &usedBytes, &createdAt) {
		if role == "admin" || role == "superadmin" {
			admins = append(admins, makeAdminUserResponse(email, name, role, quotaBytes, usedBytes, createdAt))
		}
	}
	iter.Close()

	if admins == nil {
		admins = []adminUserResponse{}
	}

	c.JSON(http.StatusOK, gin.H{"data": admins})
}
