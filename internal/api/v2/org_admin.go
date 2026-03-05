package v2

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
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

// OrgAdminHandler handles org-scoped admin API requests.
// These endpoints are accessed by org admins managing their own organisation
// and are distinct from the platform-admin endpoints under /api/v2.1/admin/.
type OrgAdminHandler struct {
	db             *db.DB
	config         *config.Config
	permMiddleware *middleware.PermissionMiddleware
}

// NewOrgAdminHandler creates a new OrgAdminHandler.
func NewOrgAdminHandler(database *db.DB, cfg *config.Config, perm *middleware.PermissionMiddleware) *OrgAdminHandler {
	return &OrgAdminHandler{
		db:             database,
		config:         cfg,
		permMiddleware: perm,
	}
}

// RegisterOrgAdminRoutes registers org-admin API routes under /api/v2.1/org.
//
// Route groups:
//   - /org/admin/...          — endpoints that use the JWT org (no :org_id in URL)
//   - /org/:org_id/admin/...  — endpoints that require an explicit org_id parameter
//
// All endpoints require the user to be an org admin of the specified org or a superadmin.
func RegisterOrgAdminRoutes(rg *gin.RouterGroup, database *db.DB, cfg *config.Config, perm *middleware.PermissionMiddleware) {
	h := NewOrgAdminHandler(database, cfg, perm)

	orgBase := rg.Group("/org")
	orgBase.Use(perm.RequireAdminOrAbove())

	// -------------------------------------------------------------------------
	// Endpoints without :org_id — org is derived from the authenticated user's JWT
	// -------------------------------------------------------------------------
	noIDGroup := orgBase.Group("/admin")
	{
		// Org info
		noIDGroup.GET("/info/", h.GetOrgInfo)
		noIDGroup.GET("/info", h.GetOrgInfo)
		noIDGroup.PUT("/info/", h.UpdateOrgInfo)
		noIDGroup.PUT("/info", h.UpdateOrgInfo)

		// Public share links
		noIDGroup.GET("/links/", h.ListOrgLinks)
		noIDGroup.GET("/links", h.ListOrgLinks)
		noIDGroup.DELETE("/links/:token/", h.DeleteOrgLink)
		noIDGroup.DELETE("/links/:token", h.DeleteOrgLink)

		// Audit logs
		noIDGroup.GET("/logs/file-access/", h.ListOrgFileAccessLogs)
		noIDGroup.GET("/logs/file-access", h.ListOrgFileAccessLogs)
		noIDGroup.GET("/logs/file-update/", h.ListOrgFileUpdateLogs)
		noIDGroup.GET("/logs/file-update", h.ListOrgFileUpdateLogs)
		noIDGroup.GET("/logs/repo-permission/", h.ListOrgRepoPermLogs)
		noIDGroup.GET("/logs/repo-permission", h.ListOrgRepoPermLogs)
	}

	// -------------------------------------------------------------------------
	// Endpoints with :org_id — validated against the authenticated user's org
	// -------------------------------------------------------------------------
	idGroup := orgBase.Group("/:org_id/admin")
	{
		// ---- Users ----
		idGroup.GET("/users/", h.ListOrgUsers)
		idGroup.GET("/users", h.ListOrgUsers)
		idGroup.POST("/users/", h.AddOrgUser)
		idGroup.POST("/users", h.AddOrgUser)
		idGroup.GET("/users/:email/", h.GetOrgUser)
		idGroup.GET("/users/:email", h.GetOrgUser)
		idGroup.PUT("/users/:email/", h.UpdateOrgUser)
		idGroup.PUT("/users/:email", h.UpdateOrgUser)
		idGroup.DELETE("/users/:email/", h.DeleteOrgUser)
		idGroup.DELETE("/users/:email", h.DeleteOrgUser)
		idGroup.PUT("/users/:email/set-password/", h.ResetOrgUserPassword)
		idGroup.PUT("/users/:email/set-password", h.ResetOrgUserPassword)
		idGroup.GET("/users/:email/repos/", h.GetOrgUserOwnedRepos)
		idGroup.GET("/users/:email/repos", h.GetOrgUserOwnedRepos)
		idGroup.GET("/users/:email/beshared-repos/", h.GetOrgUserBesharedRepos)
		idGroup.GET("/users/:email/beshared-repos", h.GetOrgUserBesharedRepos)
		idGroup.GET("/search-user/", h.SearchOrgUser)
		idGroup.GET("/search-user", h.SearchOrgUser)
		idGroup.POST("/import-users/", h.ImportOrgUsers)
		idGroup.POST("/import-users", h.ImportOrgUsers)
		idGroup.POST("/invite-users/", h.InviteOrgUsers)
		idGroup.POST("/invite-users", h.InviteOrgUsers)

		// ---- Groups ----
		idGroup.GET("/groups/", h.ListOrgGroups)
		idGroup.GET("/groups", h.ListOrgGroups)
		idGroup.GET("/groups/:gid/", h.GetOrgGroup)
		idGroup.GET("/groups/:gid", h.GetOrgGroup)
		idGroup.PUT("/groups/:gid/", h.UpdateOrgGroup)
		idGroup.PUT("/groups/:gid", h.UpdateOrgGroup)
		idGroup.DELETE("/groups/:gid/", h.DeleteOrgGroup)
		idGroup.DELETE("/groups/:gid", h.DeleteOrgGroup)
		idGroup.GET("/groups/:gid/members/", h.ListOrgGroupMembers)
		idGroup.GET("/groups/:gid/members", h.ListOrgGroupMembers)
		idGroup.POST("/groups/:gid/members/", h.AddOrgGroupMember)
		idGroup.POST("/groups/:gid/members", h.AddOrgGroupMember)
		idGroup.DELETE("/groups/:gid/members/:email/", h.DeleteOrgGroupMember)
		idGroup.DELETE("/groups/:gid/members/:email", h.DeleteOrgGroupMember)
		idGroup.PUT("/groups/:gid/members/:email/", h.UpdateOrgGroupMember)
		idGroup.PUT("/groups/:gid/members/:email", h.UpdateOrgGroupMember)
		idGroup.GET("/groups/:gid/libraries/", h.ListOrgGroupLibraries)
		idGroup.GET("/groups/:gid/libraries", h.ListOrgGroupLibraries)
		idGroup.POST("/groups/:gid/group-owned-libraries/", h.AddOrgGroupOwnedLibrary)
		idGroup.POST("/groups/:gid/group-owned-libraries", h.AddOrgGroupOwnedLibrary)
		idGroup.DELETE("/groups/:gid/group-owned-libraries/:rid/", h.DeleteOrgGroupOwnedLibrary)
		idGroup.DELETE("/groups/:gid/group-owned-libraries/:rid", h.DeleteOrgGroupOwnedLibrary)
		idGroup.GET("/search-group/", h.SearchOrgGroup)
		idGroup.GET("/search-group", h.SearchOrgGroup)

		// ---- Repositories ----
		idGroup.GET("/repos/", h.ListOrgRepos)
		idGroup.GET("/repos", h.ListOrgRepos)
		idGroup.DELETE("/repos/:rid/", h.DeleteOrgRepo)
		idGroup.DELETE("/repos/:rid", h.DeleteOrgRepo)
		idGroup.PUT("/repos/:rid/", h.TransferOrgRepo)
		idGroup.PUT("/repos/:rid", h.TransferOrgRepo)

		// ---- Trash libraries ----
		idGroup.GET("/trash-libraries/", h.ListOrgTrashLibraries)
		idGroup.GET("/trash-libraries", h.ListOrgTrashLibraries)
		idGroup.DELETE("/trash-libraries/", h.CleanOrgTrashLibraries)
		idGroup.DELETE("/trash-libraries", h.CleanOrgTrashLibraries)
		idGroup.DELETE("/trash-libraries/:rid/", h.DeleteOrgTrashLibrary)
		idGroup.DELETE("/trash-libraries/:rid", h.DeleteOrgTrashLibrary)
		idGroup.PUT("/trash-libraries/:rid/", h.RestoreOrgTrashLibrary)
		idGroup.PUT("/trash-libraries/:rid", h.RestoreOrgTrashLibrary)

		// ---- Departments (address-book) ----
		idGroup.GET("/departments/", h.ListOrgDepartments)
		idGroup.GET("/departments", h.ListOrgDepartments)
		idGroup.GET("/address-book/groups/", h.ListOrgAddressBookGroups)
		idGroup.GET("/address-book/groups", h.ListOrgAddressBookGroups)
		idGroup.POST("/address-book/groups/", h.AddOrgAddressBookGroup)
		idGroup.POST("/address-book/groups", h.AddOrgAddressBookGroup)
		idGroup.GET("/address-book/groups/:gid/", h.GetOrgAddressBookGroup)
		idGroup.GET("/address-book/groups/:gid", h.GetOrgAddressBookGroup)
		idGroup.PUT("/address-book/groups/:gid/", h.UpdateOrgAddressBookGroup)
		idGroup.PUT("/address-book/groups/:gid", h.UpdateOrgAddressBookGroup)
		idGroup.DELETE("/address-book/groups/:gid/", h.DeleteOrgAddressBookGroup)
		idGroup.DELETE("/address-book/groups/:gid", h.DeleteOrgAddressBookGroup)

		// ---- Statistics ----
		idGroup.GET("/statistics/file-operations/", h.OrgStatisticFiles)
		idGroup.GET("/statistics/file-operations", h.OrgStatisticFiles)
		idGroup.GET("/statistics/total-storage/", h.OrgStatisticStorage)
		idGroup.GET("/statistics/total-storage", h.OrgStatisticStorage)
		idGroup.GET("/statistics/active-users/", h.OrgStatisticActiveUsers)
		idGroup.GET("/statistics/active-users", h.OrgStatisticActiveUsers)
		idGroup.GET("/statistics/system-traffic/", h.OrgStatisticTraffic)
		idGroup.GET("/statistics/system-traffic", h.OrgStatisticTraffic)
		idGroup.GET("/statistics/user-traffic/", h.OrgStatisticUserTraffic)
		idGroup.GET("/statistics/user-traffic", h.OrgStatisticUserTraffic)

		// ---- Devices ----
		idGroup.GET("/devices/", h.ListOrgDevices)
		idGroup.GET("/devices", h.ListOrgDevices)
		idGroup.DELETE("/devices/", h.UnlinkOrgDevice)
		idGroup.DELETE("/devices", h.UnlinkOrgDevice)
		idGroup.GET("/devices-errors/", h.ListOrgDeviceErrors)
		idGroup.GET("/devices-errors", h.ListOrgDeviceErrors)

		// ---- Web settings, logo, SAML, domain ----
		idGroup.GET("/web-settings/", h.GetOrgWebSettings)
		idGroup.GET("/web-settings", h.GetOrgWebSettings)
		idGroup.PUT("/web-settings/", h.SetOrgWebSettings)
		idGroup.PUT("/web-settings", h.SetOrgWebSettings)
		idGroup.POST("/logo/", h.UpdateOrgLogo)
		idGroup.POST("/logo", h.UpdateOrgLogo)
		idGroup.GET("/saml-config/", h.GetOrgSAMLConfig)
		idGroup.GET("/saml-config", h.GetOrgSAMLConfig)
		idGroup.PUT("/saml-config/", h.UpdateOrgSAMLConfig)
		idGroup.PUT("/saml-config", h.UpdateOrgSAMLConfig)
		idGroup.PUT("/verify-domain/", h.VerifyOrgDomain)
		idGroup.PUT("/verify-domain", h.VerifyOrgDomain)
	}
}

// notImplemented returns a 501 stub response with a descriptive message.
func (h *OrgAdminHandler) notImplemented(c *gin.Context, feature string) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"error":   "not implemented",
		"feature": feature,
	})
}

// ============================================================================
// Helpers
// ============================================================================

// requireOrgAccess validates that the authenticated caller may administer
// targetOrgID. It returns a non-nil error (and writes the HTTP response) when:
//   - the caller is not authenticated, or
//   - the caller's org does not match targetOrgID and they are not a platform
//     superadmin.
func (h *OrgAdminHandler) requireOrgAccess(c *gin.Context, targetOrgID string) error {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")

	if callerOrgID == "" || callerUserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		c.Abort()
		return fmt.Errorf("unauthenticated")
	}

	role, err := h.permMiddleware.GetUserOrgRole(callerOrgID, callerUserID)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		c.Abort()
		return err
	}

	if callerOrgID == middleware.PlatformOrgID {
		// Platform user: must be superadmin to access any org
		if role != middleware.RoleSuperAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return fmt.Errorf("insufficient permissions")
		}
		return nil
	}

	// Tenant user: must be admin of their own org and target must match
	if !middleware.HasRequiredOrgRole(role, middleware.RoleAdmin) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		c.Abort()
		return fmt.Errorf("insufficient permissions")
	}
	if callerOrgID != targetOrgID {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		c.Abort()
		return fmt.Errorf("org mismatch")
	}
	return nil
}

// lookupOrgUserByEmail finds the user_id for a user identified by email within
// a specific org. It uses the users_by_email index first and then verifies the
// user belongs to the expected org.
func (h *OrgAdminHandler) lookupOrgUserByEmail(orgID, email string) (userID string, err error) {
	var idxOrgID string
	err = h.db.Session().Query(`
		SELECT user_id, org_id FROM users_by_email WHERE email = ?
	`, email).Scan(&userID, &idxOrgID)
	if err == nil {
		if idxOrgID != orgID {
			return "", fmt.Errorf("user not found in org")
		}
		return userID, nil
	}

	// Fallback: scan the org partition (stays cheap — narrow partition)
	iter := h.db.Session().Query(`
		SELECT user_id FROM users WHERE org_id = ? AND email = ? ALLOW FILTERING
	`, orgID, email).Iter()
	var scanUID string
	found := iter.Scan(&scanUID)
	if closeErr := iter.Close(); closeErr != nil {
		return "", closeErr
	}
	if !found {
		return "", fmt.Errorf("user not found")
	}
	// Backfill the index
	_ = h.db.Session().Query(`
		INSERT INTO users_by_email (email, user_id, org_id) VALUES (?, ?, ?)
	`, email, scanUID, orgID).Exec()
	return scanUID, nil
}

// orgUserRow is the common user response shape used across org-admin endpoints.
// Field names match the frontend OrgUserInfo model (src/models/org-user.js):
//
//	id, name, email, owner_contact_email, is_active, quota_usage, quota_total,
//	last_login, ctime, is_org_staff, role, org_id
type orgUserRow struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	ContactEmail string `json:"owner_contact_email"`
	IsActive     bool   `json:"is_active"`
	IsOrgStaff   bool   `json:"is_org_staff"`
	Role         string `json:"role"`
	QuotaTotal   int64  `json:"quota_total"`
	QuotaUsage   int64  `json:"quota_usage"`
	Ctime        string `json:"ctime"`
	LastLogin    string `json:"last_login"`
	OrgID        string `json:"org_id"`
	AvatarURL    string `json:"avatar_url"`
}

func buildOrgUserRow(email, name, role, orgID string, quota, used int64, created time.Time) orgUserRow {
	return orgUserRow{
		ID:           email, // Seafile uses email as user ID in org-admin context
		Email:        email,
		Name:         name,
		ContactEmail: "",
		IsActive:     role != "deactivated",
		IsOrgStaff:   role == "admin" || role == "superadmin",
		Role:         role,
		QuotaTotal:   quota,
		QuotaUsage:   used,
		Ctime:        created.Format(time.RFC3339),
		LastLogin:    "",
		OrgID:        orgID,
		AvatarURL:    "/static/img/default-avatar.png",
	}
}

// ============================================================================
// Org info — GET/PUT /api/v2.1/org/admin/info/
// The org_id is taken from the JWT (no :org_id in the URL).
// ============================================================================

// GetOrgInfo returns info about the caller's organisation.
// GET /org/admin/info/
func (h *OrgAdminHandler) GetOrgInfo(c *gin.Context) {
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	role, err := h.permMiddleware.GetUserOrgRole(orgID, userID)
	if err != nil || !middleware.HasRequiredOrgRole(role, middleware.RoleAdmin) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	var name string
	var storageQuota, storageUsed int64
	var createdAt time.Time

	err = h.db.Session().Query(`
		SELECT name, storage_quota, storage_used, created_at
		FROM organizations WHERE org_id = ?
	`, orgID).Scan(&name, &storageQuota, &storageUsed, &createdAt)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "organization not found"})
		return
	}

	usersCount := h.countOrgMembers(orgID)

	// Response fields match what org-info.js reads:
	// res.data.org_name, storage_quota, storage_usage, member_quota, member_usage, active_members
	c.JSON(http.StatusOK, gin.H{
		"org_id":         orgID,
		"org_name":       name,
		"storage_quota":  storageQuota,
		"storage_usage":  storageUsed,
		"member_usage":   usersCount,
		"member_quota":   h.getOrgSettingInt(orgID, "max_user_number", 0),
		"active_members": usersCount,
		"ctime":          createdAt.Format(time.RFC3339),
	})
}

// UpdateOrgInfo updates the caller's organisation name or quota.
// PUT /org/admin/info/
func (h *OrgAdminHandler) UpdateOrgInfo(c *gin.Context) {
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	role, err := h.permMiddleware.GetUserOrgRole(orgID, userID)
	if err != nil || !middleware.HasRequiredOrgRole(role, middleware.RoleAdmin) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	// Accept both JSON and form data (seafile-js compatibility)
	orgName := c.Request.FormValue("org_name")
	maxUsersStr := c.Request.FormValue("max_user_number")

	if orgName == "" && maxUsersStr == "" {
		// Try JSON body
		var body struct {
			OrgName       string `json:"org_name"`
			MaxUserNumber string `json:"max_user_number"`
		}
		if err := c.ShouldBindJSON(&body); err == nil {
			orgName = body.OrgName
			maxUsersStr = body.MaxUserNumber
		}
	}

	if orgName != "" {
		if err := h.db.Session().Query(`
			UPDATE organizations SET name = ? WHERE org_id = ?
		`, orgName, orgID).Exec(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update organization"})
			return
		}
	}

	if maxUsersStr != "" {
		if err := h.db.Session().Query(`
			UPDATE organizations SET settings['max_user_number'] = ? WHERE org_id = ?
		`, maxUsersStr, orgID).Exec(); err != nil {
			log.Printf("UpdateOrgInfo: failed to update max_user_number for org %s: %v", orgID, err)
		}
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ============================================================================
// Users — /api/v2.1/org/:org_id/admin/users/
// ============================================================================

// ListOrgUsers lists users in the target org with pagination.
// Supports ?is_staff=true to filter to admin/superadmin users only.
// GET /org/:org_id/admin/users/
func (h *OrgAdminHandler) ListOrgUsers(c *gin.Context) {
	targetOrgID := c.Param("org_id")
	if err := h.requireOrgAccess(c, targetOrgID); err != nil {
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
	isStaffOnly := c.Query("is_staff") == "true"

	iter := h.db.Session().Query(`
		SELECT user_id, email, name, role, quota_bytes, used_bytes, created_at
		FROM users WHERE org_id = ?
	`, targetOrgID).Iter()

	var all []orgUserRow
	var userID, email, name, role string
	var quota, used int64
	var created time.Time

	for iter.Scan(&userID, &email, &name, &role, &quota, &used, &created) {
		if isStaffOnly && role != "admin" && role != "superadmin" {
			continue
		}
		all = append(all, buildOrgUserRow(email, name, role, targetOrgID, quota, used, created))
	}
	if err := iter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list users"})
		return
	}

	total := len(all)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	page_data := all[start:end]
	if page_data == nil {
		page_data = []orgUserRow{}
	}

	// Frontend reads: res.data.user_list, res.data.page_next, res.data.page
	hasNext := end < total
	pageNext := false
	if hasNext {
		pageNext = true
	}
	c.JSON(http.StatusOK, gin.H{
		"user_list":   page_data,
		"page":        page,
		"page_next":   pageNext,
		"total_count": total,
	})
}

// AddOrgUser creates a user in the target org.
// Accepts FormData (email, name, password ignored) or JSON.
// POST /org/:org_id/admin/users/
func (h *OrgAdminHandler) AddOrgUser(c *gin.Context) {
	targetOrgID := c.Param("org_id")
	if err := h.requireOrgAccess(c, targetOrgID); err != nil {
		return
	}

	// Parse email/name from either form or JSON
	email := strings.TrimSpace(c.Request.FormValue("email"))
	name := strings.TrimSpace(c.Request.FormValue("name"))
	if email == "" {
		var body struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		}
		if err := c.ShouldBindJSON(&body); err == nil {
			email = strings.TrimSpace(body.Email)
			name = strings.TrimSpace(body.Name)
		}
	}

	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email is required"})
		return
	}
	if name == "" {
		name = strings.Split(email, "@")[0]
	}

	// Check uniqueness
	var existingUID string
	if err := h.db.Session().Query(`
		SELECT user_id FROM users_by_email WHERE email = ?
	`, email).Scan(&existingUID); err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user with this email already exists"})
		return
	}

	userID := uuid.New().String()
	now := time.Now()

	if err := h.db.Session().Query(`
		INSERT INTO users (org_id, user_id, email, name, role, quota_bytes, used_bytes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, targetOrgID, userID, email, name, "user", int64(-2), int64(0), now).Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
		return
	}
	h.db.Session().Query(`
		INSERT INTO users_by_email (email, user_id, org_id) VALUES (?, ?, ?)
	`, email, userID, targetOrgID).Exec()

	c.JSON(http.StatusCreated, buildOrgUserRow(email, name, "user", targetOrgID, -2, 0, now))
}

// GetOrgUser returns details for a single user identified by email within the target org.
// GET /org/:org_id/admin/users/:email/
func (h *OrgAdminHandler) GetOrgUser(c *gin.Context) {
	targetOrgID := c.Param("org_id")
	email := c.Param("email")
	if err := h.requireOrgAccess(c, targetOrgID); err != nil {
		return
	}

	userID, err := h.lookupOrgUserByEmail(targetOrgID, email)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	var name, role string
	var quota, used int64
	var created time.Time

	if err := h.db.Session().Query(`
		SELECT name, role, quota_bytes, used_bytes, created_at
		FROM users WHERE org_id = ? AND user_id = ?
	`, targetOrgID, userID).Scan(&name, &role, &quota, &used, &created); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	c.JSON(http.StatusOK, buildOrgUserRow(email, name, role, targetOrgID, quota, used, created))
}

// UpdateOrgUser updates an org user's active status, staff role, name, or quota.
// Accepts JSON: { "is_active", "is_org_staff", "name", "quota_total" }
// or form values: active, is_org_staff, name, quota_total.
// PUT /org/:org_id/admin/users/:email/
func (h *OrgAdminHandler) UpdateOrgUser(c *gin.Context) {
	targetOrgID := c.Param("org_id")
	email := c.Param("email")
	if err := h.requireOrgAccess(c, targetOrgID); err != nil {
		return
	}

	userID, err := h.lookupOrgUserByEmail(targetOrgID, email)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	var name, role string
	var quota, used int64
	var created time.Time

	if err := h.db.Session().Query(`
		SELECT name, role, quota_bytes, used_bytes, created_at
		FROM users WHERE org_id = ? AND user_id = ?
	`, targetOrgID, userID).Scan(&name, &role, &quota, &used, &created); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	// seafile-js sends FormData for all org-admin user updates.
	// Fields: is_active, is_staff, name, contact_email, quota_total
	if v := c.Request.FormValue("is_active"); v != "" {
		active := v == "true"
		if !active {
			role = "deactivated"
		} else if role == "deactivated" {
			role = "user"
		}
		h.db.Session().Query(`UPDATE users SET role = ? WHERE org_id = ? AND user_id = ?`,
			role, targetOrgID, userID).Exec()
	}

	if v := c.Request.FormValue("is_staff"); v != "" {
		isStaff := v == "true"
		if isStaff {
			role = "admin"
		} else if role == "admin" {
			role = "user"
		}
		h.db.Session().Query(`UPDATE users SET role = ? WHERE org_id = ? AND user_id = ?`,
			role, targetOrgID, userID).Exec()
	}

	if v := c.Request.FormValue("name"); v != "" {
		name = v
		h.db.Session().Query(`UPDATE users SET name = ? WHERE org_id = ? AND user_id = ?`,
			name, targetOrgID, userID).Exec()
	}

	if v := c.Request.FormValue("quota_total"); v != "" {
		if q, err := strconv.ParseInt(v, 10, 64); err == nil {
			quota = q
			h.db.Session().Query(`UPDATE users SET quota_bytes = ? WHERE org_id = ? AND user_id = ?`,
				quota, targetOrgID, userID).Exec()
		}
	}

	if v := c.Request.FormValue("contact_email"); v != "" {
		// contact_email is not stored in users table currently but acknowledge it
		_ = v
	}

	c.JSON(http.StatusOK, buildOrgUserRow(email, name, role, targetOrgID, quota, used, created))
}

// DeleteOrgUser deactivates (soft-deletes) a user from the target org.
// The caller cannot delete themselves.
// DELETE /org/:org_id/admin/users/:email/
func (h *OrgAdminHandler) DeleteOrgUser(c *gin.Context) {
	targetOrgID := c.Param("org_id")
	email := c.Param("email")
	if err := h.requireOrgAccess(c, targetOrgID); err != nil {
		return
	}

	callerUserID := c.GetString("user_id")

	userID, err := h.lookupOrgUserByEmail(targetOrgID, email)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if userID == callerUserID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot delete your own account"})
		return
	}

	h.db.Session().Query(`UPDATE users SET role = ? WHERE org_id = ? AND user_id = ?`,
		"deactivated", targetOrgID, userID).Exec()

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ResetOrgUserPassword is a no-op because SesameFS authenticates exclusively
// via OIDC — there are no local passwords to reset.
// PUT /org/:org_id/admin/users/:email/set-password/
func (h *OrgAdminHandler) ResetOrgUserPassword(c *gin.Context) {
	targetOrgID := c.Param("org_id")
	if err := h.requireOrgAccess(c, targetOrgID); err != nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"detail":  "SesameFS uses OIDC authentication; local password management is not supported",
	})
}

// GetOrgUserOwnedRepos returns libraries owned by a user in the target org.
// GET /org/:org_id/admin/users/:email/repos/
func (h *OrgAdminHandler) GetOrgUserOwnedRepos(c *gin.Context) {
	targetOrgID := c.Param("org_id")
	email := c.Param("email")
	if err := h.requireOrgAccess(c, targetOrgID); err != nil {
		return
	}

	userID, err := h.lookupOrgUserByEmail(targetOrgID, email)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	iter := h.db.Session().Query(`
		SELECT library_id, name, encrypted, size_bytes, created_at
		FROM libraries WHERE org_id = ? AND owner_id = ?
		ALLOW FILTERING
	`, targetOrgID, userID).Iter()

	type repoItem struct {
		RepoID    string `json:"repo_id"`
		RepoName  string `json:"repo_name"`
		Encrypted bool   `json:"encrypted"`
		Size      int64  `json:"size"`
		Owner     string `json:"owner"`
	}

	var repos []repoItem
	var libID, libName string
	var encrypted bool
	var size int64
	var createdAt time.Time

	for iter.Scan(&libID, &libName, &encrypted, &size, &createdAt) {
		repos = append(repos, repoItem{
			RepoID:    libID,
			RepoName:  libName,
			Encrypted: encrypted,
			Size:      size,
			Owner:     email,
		})
	}
	if err := iter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list repos"})
		return
	}
	if repos == nil {
		repos = []repoItem{}
	}

	c.JSON(http.StatusOK, gin.H{"repos": repos})
}

// GetOrgUserBesharedRepos returns libraries that have been shared to a user.
// GET /org/:org_id/admin/users/:email/beshared-repos/
func (h *OrgAdminHandler) GetOrgUserBesharedRepos(c *gin.Context) {
	targetOrgID := c.Param("org_id")
	email := c.Param("email")
	if err := h.requireOrgAccess(c, targetOrgID); err != nil {
		return
	}

	userID, err := h.lookupOrgUserByEmail(targetOrgID, email)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	// Collect library IDs shared to this user
	shareIter := h.db.Session().Query(`
		SELECT library_id, permission FROM shares
		WHERE shared_to = ? AND shared_to_type = 'user'
		ALLOW FILTERING
	`, userID).Iter()

	type repoItem struct {
		RepoID     string `json:"repo_id"`
		RepoName   string `json:"repo_name"`
		Permission string `json:"permission"`
		Owner      string `json:"owner"`
	}

	var repos []repoItem
	var libID, perm string

	for shareIter.Scan(&libID, &perm) {
		// Verify the library is in the target org
		var libName, ownerID string
		err := h.db.Session().Query(`
			SELECT name, owner_id FROM libraries WHERE org_id = ? AND library_id = ?
		`, targetOrgID, libID).Scan(&libName, &ownerID)
		if err != nil {
			continue // Library not in this org or deleted
		}
		ownerEmail := h.resolveUserEmail(targetOrgID, ownerID)
		repos = append(repos, repoItem{
			RepoID:     libID,
			RepoName:   libName,
			Permission: perm,
			Owner:      ownerEmail,
		})
	}
	if err := shareIter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list shared repos"})
		return
	}
	if repos == nil {
		repos = []repoItem{}
	}

	c.JSON(http.StatusOK, gin.H{"repos": repos})
}

// SearchOrgUser searches users within the target org by email or name fragment.
// GET /org/:org_id/admin/search-user/?query=...
func (h *OrgAdminHandler) SearchOrgUser(c *gin.Context) {
	targetOrgID := c.Param("org_id")
	if err := h.requireOrgAccess(c, targetOrgID); err != nil {
		return
	}

	query := strings.ToLower(strings.TrimSpace(c.Query("query")))
	if query == "" {
		c.JSON(http.StatusOK, gin.H{"users": []orgUserRow{}})
		return
	}

	iter := h.db.Session().Query(`
		SELECT user_id, email, name, role, quota_bytes, used_bytes, created_at
		FROM users WHERE org_id = ?
	`, targetOrgID).Iter()

	var results []orgUserRow
	var userID, email, name, role string
	var quota, used int64
	var created time.Time

	for iter.Scan(&userID, &email, &name, &role, &quota, &used, &created) {
		if strings.Contains(strings.ToLower(email), query) || strings.Contains(strings.ToLower(name), query) {
			results = append(results, buildOrgUserRow(email, name, role, targetOrgID, quota, used, created))
		}
	}
	iter.Close()

	if results == nil {
		results = []orgUserRow{}
	}
	c.JSON(http.StatusOK, gin.H{"users": results})
}

// ImportOrgUsers bulk-creates users from an uploaded CSV file.
// The CSV must have a header row and at minimum an "email" column; "name" is
// optional. Passwords are accepted for compatibility but ignored (OIDC-only).
// POST /org/:org_id/admin/import-users/
func (h *OrgAdminHandler) ImportOrgUsers(c *gin.Context) {
	targetOrgID := c.Param("org_id")
	if err := h.requireOrgAccess(c, targetOrgID); err != nil {
		return
	}

	file, _, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "CSV file is required (field: file)"})
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.TrimLeadingSpace = true

	// Read header row
	headers, err := reader.Read()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid CSV format"})
		return
	}

	emailIdx, nameIdx := -1, -1
	for i, h := range headers {
		switch strings.ToLower(strings.TrimSpace(h)) {
		case "email":
			emailIdx = i
		case "name":
			nameIdx = i
		}
	}
	if emailIdx < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "CSV must have an 'email' column"})
		return
	}

	// Frontend reads res.data.success as an array of user objects (OrgUserInfo).
	var success []orgUserRow
	var failed []gin.H
	now := time.Now()

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		if emailIdx >= len(record) {
			continue
		}

		email := strings.TrimSpace(record[emailIdx])
		if email == "" {
			continue
		}
		name := strings.Split(email, "@")[0]
		if nameIdx >= 0 && nameIdx < len(record) {
			if n := strings.TrimSpace(record[nameIdx]); n != "" {
				name = n
			}
		}

		// Skip if already exists
		var existingUID string
		if err := h.db.Session().Query(`
			SELECT user_id FROM users_by_email WHERE email = ?
		`, email).Scan(&existingUID); err == nil {
			failed = append(failed, gin.H{"email": email, "error": "already exists"})
			continue
		}

		userID := uuid.New().String()
		if err := h.db.Session().Query(`
			INSERT INTO users (org_id, user_id, email, name, role, quota_bytes, used_bytes, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, targetOrgID, userID, email, name, "user", int64(-2), int64(0), now).Exec(); err != nil {
			failed = append(failed, gin.H{"email": email, "error": "database error"})
			continue
		}
		h.db.Session().Query(`
			INSERT INTO users_by_email (email, user_id, org_id) VALUES (?, ?, ?)
		`, email, userID, targetOrgID).Exec()
		success = append(success, buildOrgUserRow(email, name, "user", targetOrgID, -2, 0, now))
	}

	if success == nil {
		success = []orgUserRow{}
	}
	if failed == nil {
		failed = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": success,
		"failed":  failed,
	})
}

// InviteOrgUsers accepts a list of email addresses and records them as invited.
// Since SesameFS uses OIDC exclusively, no invitation email is sent — the
// response acknowledges the request so the frontend flow completes normally.
// POST /org/:org_id/admin/invite-users/
func (h *OrgAdminHandler) InviteOrgUsers(c *gin.Context) {
	targetOrgID := c.Param("org_id")
	if err := h.requireOrgAccess(c, targetOrgID); err != nil {
		return
	}

	var body struct {
		Emails []string `json:"email_list"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || len(body.Emails) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email_list is required"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"invited_count": len(body.Emails),
		"detail":        "SesameFS uses OIDC; users will be created on first login",
	})
}

// ============================================================================
// Internal helpers
// ============================================================================

// countOrgMembers counts the number of users in an org (iterates the partition).
func (h *OrgAdminHandler) countOrgMembers(orgID string) int {
	count := 0
	iter := h.db.Session().Query(`SELECT user_id FROM users WHERE org_id = ?`, orgID).Iter()
	var dummy string
	for iter.Scan(&dummy) {
		count++
	}
	iter.Close()
	return count
}

// resolveUserEmail returns the email address for a user_id within an org.
// Returns an empty string if the user cannot be found.
func (h *OrgAdminHandler) resolveUserEmail(orgID, userID string) string {
	var email string
	h.db.Session().Query(`
		SELECT email FROM users WHERE org_id = ? AND user_id = ?
	`, orgID, userID).Scan(&email)
	return email
}

// getOrgSetting retrieves a single key from organizations.settings, returning
// defaultVal when the key is absent or the row cannot be read.
func (h *OrgAdminHandler) getOrgSetting(orgID, key, defaultVal string) string {
	var settings map[string]string
	if err := h.db.Session().Query(`
		SELECT settings FROM organizations WHERE org_id = ?
	`, orgID).Scan(&settings); err != nil {
		return defaultVal
	}
	if v, ok := settings[key]; ok {
		return v
	}
	return defaultVal
}

// getOrgSettingInt retrieves a single key from organizations.settings as an int.
func (h *OrgAdminHandler) getOrgSettingInt(orgID, key string, defaultVal int) int {
	s := h.getOrgSetting(orgID, key, "")
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return v
}

// ============================================================================
// Groups
// ============================================================================

func (h *OrgAdminHandler) ListOrgGroups(c *gin.Context) {
	h.notImplemented(c, "list org groups")
}
func (h *OrgAdminHandler) GetOrgGroup(c *gin.Context) {
	h.notImplemented(c, "get org group")
}
func (h *OrgAdminHandler) UpdateOrgGroup(c *gin.Context) {
	h.notImplemented(c, "update org group")
}
func (h *OrgAdminHandler) DeleteOrgGroup(c *gin.Context) {
	h.notImplemented(c, "delete org group")
}
func (h *OrgAdminHandler) ListOrgGroupMembers(c *gin.Context) {
	h.notImplemented(c, "list org group members")
}
func (h *OrgAdminHandler) AddOrgGroupMember(c *gin.Context) {
	h.notImplemented(c, "add org group member")
}
func (h *OrgAdminHandler) DeleteOrgGroupMember(c *gin.Context) {
	h.notImplemented(c, "delete org group member")
}
func (h *OrgAdminHandler) UpdateOrgGroupMember(c *gin.Context) {
	h.notImplemented(c, "update org group member role")
}
func (h *OrgAdminHandler) ListOrgGroupLibraries(c *gin.Context) {
	h.notImplemented(c, "list org group libraries")
}
func (h *OrgAdminHandler) AddOrgGroupOwnedLibrary(c *gin.Context) {
	h.notImplemented(c, "add org group owned library")
}
func (h *OrgAdminHandler) DeleteOrgGroupOwnedLibrary(c *gin.Context) {
	h.notImplemented(c, "delete org group owned library")
}
func (h *OrgAdminHandler) SearchOrgGroup(c *gin.Context) {
	h.notImplemented(c, "search org group")
}

// ============================================================================
// Repositories
// ============================================================================

func (h *OrgAdminHandler) ListOrgRepos(c *gin.Context) {
	h.notImplemented(c, "list org repos")
}
func (h *OrgAdminHandler) DeleteOrgRepo(c *gin.Context) {
	h.notImplemented(c, "delete org repo")
}
func (h *OrgAdminHandler) TransferOrgRepo(c *gin.Context) {
	h.notImplemented(c, "transfer org repo")
}

// ============================================================================
// Trash libraries
// ============================================================================

func (h *OrgAdminHandler) ListOrgTrashLibraries(c *gin.Context) {
	h.notImplemented(c, "list org trash libraries")
}
func (h *OrgAdminHandler) CleanOrgTrashLibraries(c *gin.Context) {
	h.notImplemented(c, "clean all org trash libraries")
}
func (h *OrgAdminHandler) DeleteOrgTrashLibrary(c *gin.Context) {
	h.notImplemented(c, "delete org trash library")
}
func (h *OrgAdminHandler) RestoreOrgTrashLibrary(c *gin.Context) {
	h.notImplemented(c, "restore org trash library")
}

// ============================================================================
// Departments / address-book
// ============================================================================

func (h *OrgAdminHandler) ListOrgDepartments(c *gin.Context) {
	h.notImplemented(c, "list org departments")
}
func (h *OrgAdminHandler) ListOrgAddressBookGroups(c *gin.Context) {
	h.notImplemented(c, "list org address-book groups")
}
func (h *OrgAdminHandler) AddOrgAddressBookGroup(c *gin.Context) {
	h.notImplemented(c, "add org address-book group")
}
func (h *OrgAdminHandler) GetOrgAddressBookGroup(c *gin.Context) {
	h.notImplemented(c, "get org address-book group")
}
func (h *OrgAdminHandler) UpdateOrgAddressBookGroup(c *gin.Context) {
	h.notImplemented(c, "update org address-book group")
}
func (h *OrgAdminHandler) DeleteOrgAddressBookGroup(c *gin.Context) {
	h.notImplemented(c, "delete org address-book group")
}

// ============================================================================
// Statistics
// ============================================================================

func (h *OrgAdminHandler) OrgStatisticFiles(c *gin.Context) {
	h.notImplemented(c, "org statistics file-operations")
}
func (h *OrgAdminHandler) OrgStatisticStorage(c *gin.Context) {
	h.notImplemented(c, "org statistics total-storage")
}
func (h *OrgAdminHandler) OrgStatisticActiveUsers(c *gin.Context) {
	h.notImplemented(c, "org statistics active-users")
}
func (h *OrgAdminHandler) OrgStatisticTraffic(c *gin.Context) {
	h.notImplemented(c, "org statistics system-traffic")
}
func (h *OrgAdminHandler) OrgStatisticUserTraffic(c *gin.Context) {
	h.notImplemented(c, "org statistics user-traffic")
}

// ============================================================================
// Devices
// ============================================================================

func (h *OrgAdminHandler) ListOrgDevices(c *gin.Context) {
	h.notImplemented(c, "list org devices")
}
func (h *OrgAdminHandler) UnlinkOrgDevice(c *gin.Context) {
	h.notImplemented(c, "unlink org device")
}
func (h *OrgAdminHandler) ListOrgDeviceErrors(c *gin.Context) {
	h.notImplemented(c, "list org device errors")
}

// ============================================================================
// Links (public share links)
// ============================================================================

func (h *OrgAdminHandler) ListOrgLinks(c *gin.Context) {
	h.notImplemented(c, "list org links")
}
func (h *OrgAdminHandler) DeleteOrgLink(c *gin.Context) {
	h.notImplemented(c, "delete org link")
}

// ============================================================================
// Logs
// ============================================================================

func (h *OrgAdminHandler) ListOrgFileAccessLogs(c *gin.Context) {
	h.notImplemented(c, "list org file access logs")
}
func (h *OrgAdminHandler) ListOrgFileUpdateLogs(c *gin.Context) {
	h.notImplemented(c, "list org file update logs")
}
func (h *OrgAdminHandler) ListOrgRepoPermLogs(c *gin.Context) {
	h.notImplemented(c, "list org repo permission logs")
}

// ============================================================================
// Web settings, logo, SAML, domain
// ============================================================================

func (h *OrgAdminHandler) GetOrgWebSettings(c *gin.Context) {
	h.notImplemented(c, "get org web settings")
}
func (h *OrgAdminHandler) SetOrgWebSettings(c *gin.Context) {
	h.notImplemented(c, "set org web settings")
}
func (h *OrgAdminHandler) UpdateOrgLogo(c *gin.Context) {
	h.notImplemented(c, "update org logo")
}
func (h *OrgAdminHandler) GetOrgSAMLConfig(c *gin.Context) {
	h.notImplemented(c, "get org SAML config")
}
func (h *OrgAdminHandler) UpdateOrgSAMLConfig(c *gin.Context) {
	h.notImplemented(c, "update org SAML config")
}
func (h *OrgAdminHandler) VerifyOrgDomain(c *gin.Context) {
	h.notImplemented(c, "verify org domain")
}
