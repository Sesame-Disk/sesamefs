package v2

// admin_extra.go — Additional admin panel endpoints (sysinfo, statistics, devices,
// web-settings, logs, share links, notifications, institutions, invitations, org
// user management, search organizations).
//
// These are stub implementations returning realistic empty/default data matching
// the response format expected by the Seahub-compatible frontend.

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/gin-gonic/gin"
)

// ============================================================================
// System Info — GET /admin/sysinfo/
// ============================================================================

func (h *AdminHandler) AdminGetSysInfo(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	// Count users
	var usersCount int
	iter := h.db.Session().Query(`SELECT user_id FROM users`).Iter()
	var dummy string
	for iter.Scan(&dummy) {
		usersCount++
	}
	iter.Close()

	// Count libraries
	var reposCount int
	iter = h.db.Session().Query(`SELECT library_id FROM libraries`).Iter()
	for iter.Scan(&dummy) {
		reposCount++
	}
	iter.Close()

	// Count groups
	var groupsCount int
	iter = h.db.Session().Query(`SELECT group_id FROM user_groups`).Iter()
	for iter.Scan(&dummy) {
		groupsCount++
	}
	iter.Close()

	// Count organizations
	var orgCount int
	iter = h.db.Session().Query(`SELECT org_id FROM organizations`).Iter()
	for iter.Scan(&dummy) {
		orgCount++
	}
	iter.Close()

	c.JSON(http.StatusOK, gin.H{
		"users_count":                     usersCount,
		"active_users_count":              usersCount, // Approximate
		"repos_count":                     reposCount,
		"total_files_count":               0, // Would require scanning all libraries
		"groups_count":                    groupsCount,
		"org_count":                       orgCount,
		"multi_tenancy_enabled":           true,
		"is_pro":                          true,
		"with_license":                    true,
		"license_expiration":              "2030-12-31",
		"license_mode":                    "subscription",
		"license_maxusers":                1000,
		"license_to":                      "SesameFS",
		"total_storage":                   int64(0),
		"total_devices_count":             0,
		"current_connected_devices_count": 0,
	})
}

// ============================================================================
// Statistics — GET /admin/statistics/{type}/
// ============================================================================

func (h *AdminHandler) AdminStatisticFiles(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	// Return empty statistics array with realistic date range
	stats := h.generateDateRange(c)
	result := make([]gin.H, len(stats))
	for i, dt := range stats {
		result[i] = gin.H{
			"datetime": dt,
			"added":    0,
			"deleted":  0,
			"modified": 0,
			"visited":  0,
		}
	}
	c.JSON(http.StatusOK, result)
}

func (h *AdminHandler) AdminStatisticStorage(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	stats := h.generateDateRange(c)
	result := make([]gin.H, len(stats))
	for i, dt := range stats {
		result[i] = gin.H{
			"datetime":      dt,
			"total_storage": int64(0),
		}
	}
	c.JSON(http.StatusOK, result)
}

func (h *AdminHandler) AdminStatisticActiveUsers(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	stats := h.generateDateRange(c)
	result := make([]gin.H, len(stats))
	for i, dt := range stats {
		result[i] = gin.H{
			"datetime": dt,
			"count":    0,
		}
	}
	c.JSON(http.StatusOK, result)
}

func (h *AdminHandler) AdminStatisticTraffic(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	stats := h.generateDateRange(c)
	result := make([]gin.H, len(stats))
	for i, dt := range stats {
		result[i] = gin.H{
			"datetime":           dt,
			"web-file-upload":    int64(0),
			"web-file-download":  int64(0),
			"sync-file-upload":   int64(0),
			"sync-file-download": int64(0),
			"link-file-upload":   int64(0),
			"link-file-download": int64(0),
		}
	}
	c.JSON(http.StatusOK, result)
}

// generateDateRange parses start/end params and returns date strings spaced by group_by.
func (h *AdminHandler) generateDateRange(c *gin.Context) []string {
	startStr := c.Query("start")
	endStr := c.Query("end")
	groupBy := c.DefaultQuery("group_by", "day")

	now := time.Now()
	end := now
	start := now.AddDate(0, 0, -7)

	if startStr != "" {
		if t, err := time.Parse("2006-01-02", startStr); err == nil {
			start = t
		}
	}
	if endStr != "" {
		if t, err := time.Parse("2006-01-02", endStr); err == nil {
			end = t
		}
	}

	var dates []string
	for d := start; !d.After(end); {
		dates = append(dates, d.Format("2006-01-02T00:00:00+00:00"))
		switch groupBy {
		case "month":
			d = d.AddDate(0, 1, 0)
		case "week":
			d = d.AddDate(0, 0, 7)
		default: // day
			d = d.AddDate(0, 0, 1)
		}
	}
	if len(dates) == 0 {
		dates = []string{now.Format("2006-01-02T00:00:00+00:00")}
	}
	return dates
}

// ============================================================================
// Devices — GET /admin/devices/ , GET /admin/device-errors/
// ============================================================================

func (h *AdminHandler) AdminListDevices(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"devices": []gin.H{},
		"page_info": gin.H{
			"current_page":  1,
			"has_next_page": false,
		},
	})
}

func (h *AdminHandler) AdminListDeviceErrors(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"device_errors": []gin.H{},
		"page_info": gin.H{
			"current_page":  1,
			"has_next_page": false,
		},
	})
}

func (h *AdminHandler) AdminClearDeviceErrors(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ============================================================================
// Web Settings — GET/PUT /admin/web-settings/
// ============================================================================

func (h *AdminHandler) AdminGetWebSettings(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	serviceURL := h.config.Server.Port
	if strings.HasPrefix(serviceURL, ":") {
		serviceURL = "http://localhost" + serviceURL
	}

	c.JSON(http.StatusOK, gin.H{
		"SERVICE_URL":                        serviceURL,
		"FILE_SERVER_ROOT":                   serviceURL + "/seafhttp",
		"SITE_TITLE":                         "SesameFS",
		"SITE_NAME":                          "SesameFS",
		"ENABLE_BRANDING_CSS":                false,
		"CUSTOM_CSS":                         "",
		"ENABLE_SIGNUP":                      false,
		"ACTIVATE_AFTER_REGISTRATION":        true,
		"REGISTRATION_SEND_MAIL":             false,
		"LOGIN_REMEMBER_DAYS":                7,
		"LOGIN_ATTEMPT_LIMIT":                5,
		"FREEZE_USER_ON_LOGIN_FAILED":        false,
		"ENABLE_SHARE_TO_ALL_GROUPS":         true,
		"USER_STRONG_PASSWORD_REQUIRED":      false,
		"FORCE_PASSWORD_CHANGE":              false,
		"USER_PASSWORD_MIN_LENGTH":           6,
		"USER_PASSWORD_STRENGTH_LEVEL":       1,
		"ENABLE_TWO_FACTOR_AUTH":             false,
		"ENABLE_REPO_HISTORY_SETTING":        true,
		"ENABLE_ENCRYPTED_LIBRARY":           true,
		"REPO_PASSWORD_MIN_LENGTH":           8,
		"SHARE_LINK_FORCE_USE_PASSWORD":      false,
		"SHARE_LINK_PASSWORD_MIN_LENGTH":     8,
		"SHARE_LINK_PASSWORD_STRENGTH_LEVEL": 1,
		"ENABLE_USER_CLEAN_TRASH":            true,
		"TEXT_PREVIEW_EXT":                   "ac,am,bat,c,cc,cmake,cpp,cs,css,diff,el,go,h,html,htm,java,js,json,less,make,md,org,php,pl,properties,py,rb,scala,script,sh,sql,txt,text,tex,vi,vim,xhtml,xml,log,csv,groovy,rst,patch,yml,yaml",
		"DISABLE_SYNC_WITH_ANY_FOLDER":       false,
	})
}

func (h *AdminHandler) AdminSetWebSettings(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}
	// Accept the setting but don't persist (stub)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ============================================================================
// Logo/Favicon/Login BG uploads — stubs
// ============================================================================

func (h *AdminHandler) AdminUpdateLogo(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{"logo_path": "/media/custom/logo.png"})
}

func (h *AdminHandler) AdminUpdateFavicon(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{"favicon_path": "/media/custom/favicon.ico"})
}

func (h *AdminHandler) AdminUpdateLoginBG(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{"login_bg_image_path": "/media/custom/login-bg.jpg"})
}

// ============================================================================
// Search Organizations — GET /admin/search-organization/
// ============================================================================

func (h *AdminHandler) AdminSearchOrganizations(c *gin.Context) {
	query := strings.ToLower(c.Query("query"))

	iter := h.db.Session().Query(`
		SELECT org_id, name, storage_quota, storage_used, created_at
		FROM organizations
	`).Iter()

	var orgs []gin.H
	var orgID, name string
	var storageQuota, storageUsed int64
	var createdAt time.Time

	for iter.Scan(&orgID, &name, &storageQuota, &storageUsed, &createdAt) {
		if query != "" && !strings.Contains(strings.ToLower(name), query) {
			continue
		}
		// Count users in this org
		usersCount := h.countOrgUsers(orgID)
		orgs = append(orgs, gin.H{
			"org_id":        orgID,
			"org_name":      name,
			"creator_email": "",
			"creator_name":  "",
			"role":          "default",
			"quota_usage":   storageUsed,
			"quota":         storageQuota,
			"ctime":         createdAt.Format(time.RFC3339),
			"users_count":   usersCount,
		})
	}
	iter.Close()

	if orgs == nil {
		orgs = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{
		"organizations": orgs,
		"total_count":   len(orgs),
	})
}

// ============================================================================
// Organization User Management (POST, PUT, DELETE)
// ============================================================================

// AdminAddOrgUser creates a user in an organization
// POST /admin/organizations/:org_id/users/
func (h *AdminHandler) AdminAddOrgUser(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	targetOrgID := c.Param("org_id")

	var req struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email is required"})
		return
	}

	if req.Email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email is required"})
		return
	}

	// Create user via the existing admin create user logic
	userID := generateUserID()
	now := time.Now()
	role := "user"

	if req.Name == "" {
		req.Name = strings.Split(req.Email, "@")[0]
	}

	err := h.db.Session().Query(`
		INSERT INTO users (org_id, user_id, email, name, role, quota_bytes, used_bytes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, targetOrgID, userID, req.Email, req.Name, role, int64(-2), int64(0), now).Exec()

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
		return
	}

	// Create email lookup
	h.db.Session().Query(`
		INSERT INTO users_by_email (email, user_id, org_id)
		VALUES (?, ?, ?)
	`, req.Email, userID, targetOrgID).Exec()

	c.JSON(http.StatusCreated, gin.H{
		"email":        req.Email,
		"name":         req.Name,
		"active":       true,
		"is_org_staff": false,
		"quota_usage":  0,
		"quota_total":  -2,
		"create_time":  now.Format(time.RFC3339),
		"last_login":   "",
		"org_id":       targetOrgID,
	})
}

// AdminUpdateOrgUser updates a user in an organization
// PUT /admin/organizations/:org_id/users/:email/
func (h *AdminHandler) AdminUpdateOrgUser(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	targetOrgID := c.Param("org_id")
	email := c.Param("email")

	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Find user by email in org
	var userID, name, role string
	var quotaBytes, usedBytes int64
	var createdAt time.Time
	found := false

	iter := h.db.Session().Query(`
		SELECT user_id, email, name, role, quota_bytes, used_bytes, created_at
		FROM users WHERE org_id = ?
	`, targetOrgID).Iter()

	var scanEmail, scanName, scanRole, scanUID string
	var scanQuota, scanUsed int64
	var scanCreated time.Time
	for iter.Scan(&scanUID, &scanEmail, &scanName, &scanRole, &scanQuota, &scanUsed, &scanCreated) {
		if scanEmail == email {
			userID = scanUID
			name = scanName
			role = scanRole
			quotaBytes = scanQuota
			usedBytes = scanUsed
			createdAt = scanCreated
			found = true
			break
		}
	}
	iter.Close()

	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	// Apply updates
	if v, ok := req["active"]; ok {
		if active, ok := v.(bool); ok && !active {
			role = "deactivated"
		} else {
			if role == "deactivated" {
				role = "user"
			}
		}
		h.db.Session().Query(`UPDATE users SET role = ? WHERE org_id = ? AND user_id = ?`,
			role, targetOrgID, userID).Exec()
	}
	if v, ok := req["is_org_staff"]; ok {
		if isStaff, ok := v.(bool); ok && isStaff {
			role = "admin"
		} else if role == "admin" {
			role = "user"
		}
		h.db.Session().Query(`UPDATE users SET role = ? WHERE org_id = ? AND user_id = ?`,
			role, targetOrgID, userID).Exec()
	}
	if v, ok := req["name"]; ok {
		if n, ok := v.(string); ok {
			name = n
			h.db.Session().Query(`UPDATE users SET name = ? WHERE org_id = ? AND user_id = ?`,
				name, targetOrgID, userID).Exec()
		}
	}
	if v, ok := req["quota_total"]; ok {
		if q, ok := v.(float64); ok {
			quotaBytes = int64(q)
			h.db.Session().Query(`UPDATE users SET quota_bytes = ? WHERE org_id = ? AND user_id = ?`,
				quotaBytes, targetOrgID, userID).Exec()
		}
	}

	isActive := role != "deactivated"
	isOrgStaff := role == "admin" || role == "superadmin"

	c.JSON(http.StatusOK, gin.H{
		"email":        email,
		"name":         name,
		"active":       isActive,
		"is_org_staff": isOrgStaff,
		"quota_usage":  usedBytes,
		"quota_total":  quotaBytes,
		"create_time":  createdAt.Format(time.RFC3339),
		"last_login":   "",
		"org_id":       targetOrgID,
	})
}

// AdminDeleteOrgUser removes a user from an organization
// DELETE /admin/organizations/:org_id/users/:email/
func (h *AdminHandler) AdminDeleteOrgUser(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	targetOrgID := c.Param("org_id")
	email := c.Param("email")

	// Find user by email in org
	iter := h.db.Session().Query(`
		SELECT user_id, email FROM users WHERE org_id = ?
	`, targetOrgID).Iter()

	var scanUID, scanEmail string
	var foundUID string
	for iter.Scan(&scanUID, &scanEmail) {
		if scanEmail == email {
			foundUID = scanUID
			break
		}
	}
	iter.Close()

	if foundUID == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	// Mark as deactivated rather than hard delete
	h.db.Session().Query(`UPDATE users SET role = ? WHERE org_id = ? AND user_id = ?`,
		"deactivated", targetOrgID, foundUID).Exec()

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// AdminListOrgGroups lists groups in an organization
// GET /admin/organizations/:org_id/groups/
func (h *AdminHandler) AdminListOrgGroups(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	targetOrgID := c.Param("org_id")

	iter := h.db.Session().Query(`
		SELECT group_id, group_name, owner_id, created_at
		FROM user_groups WHERE org_id = ?
	`, targetOrgID).Iter()

	var groups []gin.H
	var groupID, groupName, ownerID string
	var createdAt time.Time

	for iter.Scan(&groupID, &groupName, &ownerID, &createdAt) {
		ownerEmail := h.resolveOwnerEmail(targetOrgID, ownerID)
		groups = append(groups, gin.H{
			"id":           groupID,
			"name":         groupName,
			"owner":        ownerEmail,
			"created_at":   createdAt.Format(time.RFC3339),
			"member_count": 0,
		})
	}
	iter.Close()

	if groups == nil {
		groups = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{"groups": groups})
}

// ============================================================================
// Logs — GET /admin/logs/*
// ============================================================================

func (h *AdminHandler) AdminListLoginLogs(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"login_log_list": []gin.H{},
		"total_count":    0,
	})
}

func (h *AdminHandler) AdminListFileAccessLogs(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"file_access_log_list": []gin.H{},
		"total_count":          0,
	})
}

func (h *AdminHandler) AdminListFileUpdateLogs(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"file_update_log_list": []gin.H{},
		"total_count":          0,
	})
}

func (h *AdminHandler) AdminListSharePermissionLogs(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"share_permission_log_list": []gin.H{},
		"total_count":               0,
	})
}

func (h *AdminHandler) AdminListAdminLogs(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"admin_operation_log_list": []gin.H{},
		"total_count":              0,
	})
}

func (h *AdminHandler) AdminListAdminLoginLogs(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"admin_login_log_list": []gin.H{},
		"total_count":          0,
	})
}

// ============================================================================
// Share Links — GET /admin/share-links/ , DELETE /admin/share-links/:token/
// ============================================================================

func (h *AdminHandler) AdminListShareLinks(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	// Query all share links from the database (correct column names)
	var links []gin.H
	iter := h.db.Session().Query(`SELECT share_token, library_id, file_path, created_by, org_id, permission, expires_at, download_count, created_at FROM share_links`).Iter()
	var token, libID, filePath, createdBy, orgID, permission string
	var expiresAt *time.Time
	var downloadCount int
	var createdAt time.Time

	// Cache for library names and user emails to avoid repeated lookups
	libNameCache := map[string]string{}
	userEmailCache := map[string]string{}

	for iter.Scan(&token, &libID, &filePath, &createdBy, &orgID, &permission, &expiresAt, &downloadCount, &createdAt) {
		// Extract file/folder name from path
		objName := filePath
		if idx := strings.LastIndex(filePath, "/"); idx >= 0 && idx < len(filePath)-1 {
			objName = filePath[idx+1:]
		}

		isExpired := false
		expireDateStr := ""
		if expiresAt != nil && !expiresAt.IsZero() {
			isExpired = expiresAt.Before(time.Now())
			expireDateStr = expiresAt.Format("2006-01-02T15:04:05+00:00")
		}

		// Resolve library name (cached) — use libraries table which has the name column
		repoName, ok := libNameCache[libID]
		if !ok {
			h.db.Session().Query(`SELECT name FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, libID).Scan(&repoName)
			if repoName == "" {
				repoName = "Unknown Library"
			}
			libNameCache[libID] = repoName
		}

		// Resolve creator email (cached)
		creatorEmail, ok := userEmailCache[createdBy]
		if !ok {
			h.db.Session().Query(`SELECT email FROM users WHERE org_id = ? AND user_id = ?`, orgID, createdBy).Scan(&creatorEmail)
			if creatorEmail == "" {
				creatorEmail = createdBy
			}
			userEmailCache[createdBy] = creatorEmail
		}

		// Resolve creator name
		creatorName := creatorEmail
		var name string
		h.db.Session().Query(`SELECT name FROM users WHERE org_id = ? AND user_id = ?`, orgID, createdBy).Scan(&name)
		if name != "" {
			creatorName = name
		}

		links = append(links, gin.H{
			"obj_name":      objName,
			"token":         token,
			"repo_id":       libID,
			"repo_name":     repoName,
			"path":          filePath,
			"creator_email": creatorEmail,
			"creator_name":  creatorName,
			"ctime":         createdAt.Format(time.RFC3339),
			"view_cnt":      downloadCount,
			"expire_date":   expireDateStr,
			"is_expired":    isExpired,
			"permissions":   gin.H{"can_download": permission == "download" || permission == "preview_download" || permission == "edit", "can_edit": permission == "edit"},
		})
	}
	if err := iter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list share links"})
		return
	}

	if links == nil {
		links = []gin.H{}
	}

	// Sorting support
	sortBy := c.DefaultQuery("order_by", "")
	direction := c.DefaultQuery("direction", "asc")
	if sortBy != "" {
		sort.Slice(links, func(i, j int) bool {
			var vi, vj string
			switch sortBy {
			case "ctime":
				vi, _ = links[i]["ctime"].(string)
				vj, _ = links[j]["ctime"].(string)
			case "creator":
				vi, _ = links[i]["creator_email"].(string)
				vj, _ = links[j]["creator_email"].(string)
			case "name":
				vi, _ = links[i]["obj_name"].(string)
				vj, _ = links[j]["obj_name"].(string)
			default:
				vi, _ = links[i]["ctime"].(string)
				vj, _ = links[j]["ctime"].(string)
			}
			if direction == "desc" {
				return vi > vj
			}
			return vi < vj
		})
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "25"))
	total := len(links)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}

	c.JSON(http.StatusOK, gin.H{
		"share_link_list": links[start:end],
		"count":           total,
	})
}

func (h *AdminHandler) AdminDeleteShareLink(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	token := c.Param("token")

	// Read the link first to get created_by + org_id for dual-delete
	var createdBy, orgID string
	if err := h.db.Session().Query(`SELECT created_by, org_id FROM share_links WHERE share_token = ?`, token).Scan(&createdBy, &orgID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "share link not found"})
		return
	}

	// Dual-delete from both tables (same pattern as user DeleteShareLink)
	batch := h.db.Session().Batch(gocql.LoggedBatch)
	batch.Query(`DELETE FROM share_links WHERE share_token = ?`, token)
	batch.Query(`DELETE FROM share_links_by_creator WHERE org_id = ? AND created_by = ? AND share_token = ?`,
		orgID, createdBy, token)

	if err := batch.Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete share link"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ============================================================================
// Upload Links — GET /admin/upload-links/ , DELETE /admin/upload-links/:token/
// ============================================================================

func (h *AdminHandler) AdminListUploadLinks(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	// Query all upload links
	var links []gin.H
	iter := h.db.Session().Query(`SELECT upload_token, library_id, file_path, created_by, org_id, expires_at, created_at FROM upload_links`).Iter()
	var token, libID, filePath, createdBy, orgID string
	var expiresAt *time.Time
	var createdAt time.Time

	libNameCache := map[string]string{}
	userEmailCache := map[string]string{}

	for iter.Scan(&token, &libID, &filePath, &createdBy, &orgID, &expiresAt, &createdAt) {
		objName := filePath
		if idx := strings.LastIndex(filePath, "/"); idx >= 0 && idx < len(filePath)-1 {
			objName = filePath[idx+1:]
		}

		isExpired := false
		expireDateStr := ""
		if expiresAt != nil && !expiresAt.IsZero() {
			isExpired = expiresAt.Before(time.Now())
			expireDateStr = expiresAt.Format("2006-01-02T15:04:05+00:00")
		}

		repoName, ok := libNameCache[libID]
		if !ok {
			h.db.Session().Query(`SELECT name FROM libraries WHERE org_id = ? AND library_id = ?`, orgID, libID).Scan(&repoName)
			if repoName == "" {
				repoName = "Unknown Library"
			}
			libNameCache[libID] = repoName
		}

		creatorEmail, ok := userEmailCache[createdBy]
		if !ok {
			h.db.Session().Query(`SELECT email FROM users WHERE org_id = ? AND user_id = ?`, orgID, createdBy).Scan(&creatorEmail)
			if creatorEmail == "" {
				creatorEmail = createdBy
			}
			userEmailCache[createdBy] = creatorEmail
		}

		creatorName := creatorEmail
		var name string
		h.db.Session().Query(`SELECT name FROM users WHERE org_id = ? AND user_id = ?`, orgID, createdBy).Scan(&name)
		if name != "" {
			creatorName = name
		}

		links = append(links, gin.H{
			"obj_name":      objName,
			"path":          filePath,
			"token":         token,
			"repo_id":       libID,
			"repo_name":     repoName,
			"creator_email": creatorEmail,
			"creator_name":  creatorName,
			"ctime":         createdAt.Format(time.RFC3339),
			"view_cnt":      0,
			"expire_date":   expireDateStr,
			"is_expired":    isExpired,
		})
	}
	if err := iter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list upload links"})
		return
	}

	if links == nil {
		links = []gin.H{}
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "25"))
	total := len(links)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}

	c.JSON(http.StatusOK, gin.H{
		"upload_link_list": links[start:end],
		"count":            total,
	})
}

func (h *AdminHandler) AdminDeleteUploadLink(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	token := c.Param("token")

	// Read the link first to get created_by + org_id for dual-delete
	var createdBy, orgID string
	if err := h.db.Session().Query(`SELECT created_by, org_id FROM upload_links WHERE upload_token = ?`, token).Scan(&createdBy, &orgID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "upload link not found"})
		return
	}

	batch := h.db.Session().Batch(gocql.LoggedBatch)
	batch.Query(`DELETE FROM upload_links WHERE upload_token = ?`, token)
	batch.Query(`DELETE FROM upload_links_by_creator WHERE org_id = ? AND created_by = ? AND upload_token = ?`,
		orgID, createdBy, token)

	if err := batch.Exec(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete upload link"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ============================================================================
// System Notifications — GET/POST/PUT/DELETE /admin/sys-notifications/
// ============================================================================

func (h *AdminHandler) AdminListSysNotifications(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"notifications": []gin.H{},
	})
}

func (h *AdminHandler) AdminAddSysNotification(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	var req struct {
		Msg string `json:"msg"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "msg is required"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"notification": gin.H{
			"id":         1,
			"msg":        req.Msg,
			"is_current": false,
		},
	})
}

func (h *AdminHandler) AdminUpdateSysNotification(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *AdminHandler) AdminDeleteSysNotification(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ============================================================================
// Institutions — GET/POST/PUT/DELETE /admin/institutions/
// ============================================================================

func (h *AdminHandler) AdminListInstitutions(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"institution_list": []gin.H{},
		"total_count":      0,
	})
}

func (h *AdminHandler) AdminAddInstitution(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"id":    1,
		"name":  req.Name,
		"ctime": time.Now().Format(time.RFC3339),
	})
}

func (h *AdminHandler) AdminGetInstitution(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}
	id := c.Param("id")
	c.JSON(http.StatusOK, gin.H{
		"id":   id,
		"name": "Institution",
	})
}

func (h *AdminHandler) AdminUpdateInstitution(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *AdminHandler) AdminDeleteInstitution(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ============================================================================
// Invitations — GET/DELETE /admin/invitations/
// ============================================================================

func (h *AdminHandler) AdminListInvitations(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"invitation_list": []gin.H{},
		"total_count":     0,
	})
}

func (h *AdminHandler) AdminDeleteInvitation(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ============================================================================
// License upload — POST /admin/license/
// ============================================================================

func (h *AdminHandler) AdminUploadLicense(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"with_license":       true,
		"license_expiration": "2030-12-31",
		"license_mode":       "subscription",
		"license_maxusers":   1000,
		"license_to":         "SesameFS",
	})
}

// ============================================================================
// User share/upload links — GET /admin/users/:email/share-links/
// ============================================================================

func (h *AdminHandler) AdminListUserShareLinks(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	email := c.GetString("resolved_user_param")
	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email is required"})
		return
	}

	// Look up user by email to get user_id
	var targetUserID string
	var targetOrgID string
	if err := h.db.Session().Query(`SELECT user_id, org_id FROM users_by_email WHERE email = ?`, email).Scan(&targetUserID, &targetOrgID); err != nil {
		c.JSON(http.StatusOK, gin.H{"share_link_list": []gin.H{}, "count": 0})
		return
	}

	// Query share links by creator
	var links []gin.H
	iter := h.db.Session().Query(`
		SELECT share_token, library_id, file_path, permission, expires_at, download_count, created_at
		FROM share_links_by_creator WHERE org_id = ? AND created_by = ?`,
		targetOrgID, targetUserID).Iter()

	var token, libID, filePath, permission string
	var expiresAt *time.Time
	var downloadCount int
	var createdAt time.Time

	libNameCache := map[string]string{}

	for iter.Scan(&token, &libID, &filePath, &permission, &expiresAt, &downloadCount, &createdAt) {
		objName := filePath
		if idx := strings.LastIndex(filePath, "/"); idx >= 0 && idx < len(filePath)-1 {
			objName = filePath[idx+1:]
		}

		isExpired := false
		expireDateStr := ""
		if expiresAt != nil && !expiresAt.IsZero() {
			isExpired = expiresAt.Before(time.Now())
			expireDateStr = expiresAt.Format("2006-01-02T15:04:05+00:00")
		}

		repoName, ok := libNameCache[libID]
		if !ok {
			h.db.Session().Query(`SELECT name FROM libraries WHERE org_id = ? AND library_id = ?`, targetOrgID, libID).Scan(&repoName)
			if repoName == "" {
				repoName = "Unknown Library"
			}
			libNameCache[libID] = repoName
		}

		links = append(links, gin.H{
			"obj_name":      objName,
			"token":         token,
			"repo_id":       libID,
			"repo_name":     repoName,
			"path":          filePath,
			"creator_email": email,
			"creator_name":  email,
			"ctime":         createdAt.Format(time.RFC3339),
			"view_cnt":      downloadCount,
			"expire_date":   expireDateStr,
			"is_expired":    isExpired,
		})
	}
	iter.Close()

	if links == nil {
		links = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{
		"share_link_list": links,
		"count":           len(links),
	})
}

func (h *AdminHandler) AdminListUserUploadLinks(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	email := c.GetString("resolved_user_param")
	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email is required"})
		return
	}

	// Look up user by email to get user_id
	var targetUserID string
	var targetOrgID string
	if err := h.db.Session().Query(`SELECT user_id, org_id FROM users_by_email WHERE email = ?`, email).Scan(&targetUserID, &targetOrgID); err != nil {
		c.JSON(http.StatusOK, gin.H{"upload_link_list": []gin.H{}, "count": 0})
		return
	}

	// Query upload links by creator
	var links []gin.H
	iter := h.db.Session().Query(`
		SELECT upload_token, library_id, file_path, expires_at, created_at
		FROM upload_links_by_creator WHERE org_id = ? AND created_by = ?`,
		targetOrgID, targetUserID).Iter()

	var token, libID, filePath string
	var expiresAt *time.Time
	var createdAt time.Time

	libNameCache := map[string]string{}

	for iter.Scan(&token, &libID, &filePath, &expiresAt, &createdAt) {
		objName := filePath
		if idx := strings.LastIndex(filePath, "/"); idx >= 0 && idx < len(filePath)-1 {
			objName = filePath[idx+1:]
		}

		isExpired := false
		expireDateStr := ""
		if expiresAt != nil && !expiresAt.IsZero() {
			isExpired = expiresAt.Before(time.Now())
			expireDateStr = expiresAt.Format("2006-01-02T15:04:05+00:00")
		}

		repoName, ok := libNameCache[libID]
		if !ok {
			h.db.Session().Query(`SELECT name FROM libraries WHERE org_id = ? AND library_id = ?`, targetOrgID, libID).Scan(&repoName)
			if repoName == "" {
				repoName = "Unknown Library"
			}
			libNameCache[libID] = repoName
		}

		links = append(links, gin.H{
			"obj_name":      objName,
			"path":          filePath,
			"token":         token,
			"repo_id":       libID,
			"repo_name":     repoName,
			"creator_email": email,
			"creator_name":  email,
			"ctime":         createdAt.Format(time.RFC3339),
			"view_cnt":      0,
			"expire_date":   expireDateStr,
			"is_expired":    isExpired,
		})
	}
	iter.Close()

	if links == nil {
		links = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{
		"upload_link_list": links,
		"count":            len(links),
	})
}

// AdminListUserGroups returns groups that a user belongs to
// GET /admin/users/:email/groups/ (dispatched via adminUsersHandler)
func (h *AdminHandler) AdminListUserGroups(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"group_list": []gin.H{},
	})
}

// ============================================================================
// User group member role update — PUT /admin/groups/:group_id/members/:email/
// ============================================================================

func (h *AdminHandler) AdminUpdateGroupMemberRole(c *gin.Context) {
	callerOrgID := c.GetString("org_id")
	callerUserID := c.GetString("user_id")
	if err := h.requireAdminAccess(c, callerOrgID, callerUserID); err != nil {
		return
	}

	// Stub: accept and return success
	var req struct {
		IsAdmin bool `json:"is_admin"`
	}
	c.ShouldBindJSON(&req)

	role := "Member"
	if req.IsAdmin {
		role = "Admin"
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"is_admin": req.IsAdmin,
		"role":     role,
	})
}

// ============================================================================
// Helpers
// ============================================================================

func (h *AdminHandler) countOrgUsers(orgID string) int {
	count := 0
	iter := h.db.Session().Query(`SELECT user_id FROM users WHERE org_id = ?`, orgID).Iter()
	var dummy string
	for iter.Scan(&dummy) {
		count++
	}
	iter.Close()
	return count
}

func (h *AdminHandler) countOrgLibraries(orgID string) int {
	count := 0
	iter := h.db.Session().Query(`SELECT library_id FROM libraries WHERE org_id = ?`, orgID).Iter()
	var dummy string
	for iter.Scan(&dummy) {
		count++
	}
	iter.Close()
	return count
}

func generateUserID() string {
	return "u-" + time.Now().Format("20060102150405") + "-" + strconv.FormatInt(time.Now().UnixNano()%10000, 10)
}
