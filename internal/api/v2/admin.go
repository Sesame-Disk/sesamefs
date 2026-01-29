package v2

import (
	"fmt"
	"net/http"
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

		// User management (superadmin or tenant admin for own org)
		admin.GET("/users/:user_id/", h.GetUser)
		admin.GET("/users/:user_id", h.GetUser)
		admin.PUT("/users/:user_id/", h.UpdateUser)
		admin.PUT("/users/:user_id", h.UpdateUser)
		admin.DELETE("/users/:user_id/", h.DeactivateUser)
		admin.DELETE("/users/:user_id", h.DeactivateUser)
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

// GetUser returns details for a single user.
// GET /admin/users/:user_id/
func (h *AdminHandler) GetUser(c *gin.Context) {
	targetUserID := c.Param("user_id")
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")

	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	// Try to find the user - we need their org_id
	// For superadmin, search across all orgs; for tenant admin, search own org
	var email, name, role, userOrgID string
	var quotaBytes, usedBytes int64
	var createdAt time.Time

	if callerOrgID == middleware.PlatformOrgID {
		// Superadmin: search by scanning users_by_email or iterate
		// For simplicity, try the caller's known orgs or do a lookup
		// We'll use users_by_email if available, but user_id lookup requires org_id
		// This is a limitation of the Cassandra schema - we'd need a secondary index
		// For now, return 501 if org is not known
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
func (h *AdminHandler) UpdateUser(c *gin.Context) {
	targetUserID := c.Param("user_id")
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
func (h *AdminHandler) DeactivateUser(c *gin.Context) {
	targetUserID := c.Param("user_id")
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
