package middleware

import (
	"net/http"
	"strings"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// isNotFound checks if a Cassandra error indicates no rows were found.
func isNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not found")
}

// PermissionMiddleware handles permission checking
type PermissionMiddleware struct {
	db *db.DB
}

// NewPermissionMiddleware creates a new permission middleware
func NewPermissionMiddleware(database *db.DB) *PermissionMiddleware {
	return &PermissionMiddleware{db: database}
}

// OrganizationRole represents user roles at org level
type OrganizationRole string

const (
	RoleSuperAdmin OrganizationRole = "superadmin"
	RoleAdmin      OrganizationRole = "admin"
	RoleUser       OrganizationRole = "user"
	RoleReadOnly   OrganizationRole = "readonly"
	RoleGuest      OrganizationRole = "guest"
)

// PlatformOrgID is the well-known UUID for the platform-level organization.
// Superadmin users must belong to this org.
const PlatformOrgID = "00000000-0000-0000-0000-000000000000"

// LibraryPermission represents library access permissions
type LibraryPermission string

const (
	PermissionOwner LibraryPermission = "owner"
	PermissionRW    LibraryPermission = "rw"
	PermissionR     LibraryPermission = "r"
	PermissionNone  LibraryPermission = ""
)

// GroupRole represents roles within a group
type GroupRole string

const (
	GroupRoleOwner  GroupRole = "owner"
	GroupRoleAdmin  GroupRole = "admin"
	GroupRoleMember GroupRole = "member"
)

// RequireAuth ensures user is authenticated
// This should be applied to all protected routes
func (m *PermissionMiddleware) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		orgID := c.GetString("org_id")

		if userID == "" || orgID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireOrgRole checks if user has required organization role
// Usage: RequireOrgRole(RoleAdmin) - requires admin role
func (m *PermissionMiddleware) RequireOrgRole(requiredRole OrganizationRole) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		orgID := c.GetString("org_id")

		if userID == "" || orgID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			c.Abort()
			return
		}

		// Get user's role in the organization
		role, err := m.GetUserOrgRole(orgID, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permissions"})
			c.Abort()
			return
		}

		// Check if user has required role
		if !m.hasRequiredOrgRole(role, requiredRole) {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}

		// Store role in context for later use
		c.Set("user_org_role", role)
		c.Next()
	}
}

// RequireLibraryPermission checks if user has required permission for a library
// Usage: RequireLibraryPermission("repo_id", PermissionRW)
func (m *PermissionMiddleware) RequireLibraryPermission(paramName string, requiredPerm LibraryPermission) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		orgID := c.GetString("org_id")
		repoID := c.Param(paramName)

		if repoID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "library_id required"})
			c.Abort()
			return
		}

		// Check if this is a repo API token (scoped to a specific library)
		if isRepoToken, _ := c.Get("repo_api_token"); isRepoToken == true {
			tokenRepoID := c.GetString("repo_api_token_repo_id")
			tokenPerm := c.GetString("repo_api_token_permission")

			// Token must be for this specific library
			if tokenRepoID != repoID {
				c.JSON(http.StatusForbidden, gin.H{"error": "API token does not have access to this library"})
				c.Abort()
				return
			}

			// Map token permission to library permission
			var libPerm LibraryPermission
			switch tokenPerm {
			case "rw":
				libPerm = PermissionRW
			default:
				libPerm = PermissionR
			}

			if !m.hasRequiredLibraryPermission(libPerm, requiredPerm) {
				c.JSON(http.StatusForbidden, gin.H{"error": "API token has insufficient permissions"})
				c.Abort()
				return
			}

			c.Set("library_permission", libPerm)
			c.Next()
			return
		}

		// Get user's permission for this library
		permission, err := m.GetLibraryPermission(orgID, userID, repoID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check library permissions"})
			c.Abort()
			return
		}

		// Check if user has required permission
		if !m.hasRequiredLibraryPermission(permission, requiredPerm) {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient library permissions"})
			c.Abort()
			return
		}

		// Store permission in context
		c.Set("library_permission", permission)
		c.Next()
	}
}

// RequireLibraryOwner checks if user is the owner of a library
func (m *PermissionMiddleware) RequireLibraryOwner(paramName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		orgID := c.GetString("org_id")
		repoID := c.Param(paramName)

		if repoID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "library_id required"})
			c.Abort()
			return
		}

		isOwner, err := m.IsLibraryOwner(orgID, userID, repoID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check ownership"})
			c.Abort()
			return
		}

		if !isOwner {
			c.JSON(http.StatusForbidden, gin.H{"error": "only library owner can perform this action"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireGroupRole checks if user has required role in a group
func (m *PermissionMiddleware) RequireGroupRole(paramName string, requiredRole GroupRole) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		groupID := c.Param(paramName)

		if groupID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "group_id required"})
			c.Abort()
			return
		}

		role, err := m.GetGroupRole(groupID, userID)
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "not a group member"})
			c.Abort()
			return
		}

		if !m.hasRequiredGroupRole(role, requiredRole) {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient group permissions"})
			c.Abort()
			return
		}

		c.Set("group_role", role)
		c.Next()
	}
}

// GetUserOrgRole retrieves user's role in an organization
func (m *PermissionMiddleware) GetUserOrgRole(orgID, userID string) (OrganizationRole, error) {
	var role string
	err := m.db.Session().Query(`
		SELECT role FROM users WHERE org_id = ? AND user_id = ?
	`, orgID, userID).Scan(&role)

	if err != nil {
		return RoleGuest, err
	}

	return OrganizationRole(role), nil
}

// GetLibraryPermission retrieves user's permission for a library
func (m *PermissionMiddleware) GetLibraryPermission(orgID, userID, repoID string) (LibraryPermission, error) {
	// Check if user is admin/superadmin - they have full access to all libraries
	role, err := m.GetUserOrgRole(orgID, userID)
	if err == nil && (role == RoleSuperAdmin || role == RoleAdmin) {
		return PermissionOwner, nil
	}

	// Check if user is the owner
	var ownerIDStr string
	err = m.db.Session().Query(`
		SELECT owner_id FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, repoID).Scan(&ownerIDStr)

	if err != nil {
		if isNotFound(err) {
			return PermissionNone, nil // Library doesn't exist
		}
		return PermissionNone, err
	}

	if ownerIDStr == userID {
		return PermissionOwner, nil
	}

	// Check if library is shared with user
	var permission string
	err = m.db.Session().Query(`
		SELECT permission FROM shares
		WHERE library_id = ? AND shared_to = ? AND shared_to_type = 'user'
	`, repoID, userID).Scan(&permission)

	if err == nil {
		return LibraryPermission(permission), nil
	}

	// Check if library is shared with user's groups
	// Get all groups this user is a member of
	groupIter := m.db.Session().Query(`
		SELECT group_id FROM groups_by_member
		WHERE org_id = ? AND user_id = ?
	`, orgID, userID).Iter()

	var groupIDStr string
	var highestPermission LibraryPermission = PermissionNone

	for groupIter.Scan(&groupIDStr) {
		// Check if library is shared with this group
		var groupPermission string
		err := m.db.Session().Query(`
			SELECT permission FROM shares
			WHERE library_id = ? AND shared_to = ? AND shared_to_type = 'group'
		`, repoID, groupIDStr).Scan(&groupPermission)

		if err == nil {
			// Convert string permission to LibraryPermission type
			perm := LibraryPermission(groupPermission)
			// Keep the highest permission level found
			if m.hasRequiredLibraryPermission(perm, highestPermission) {
				highestPermission = perm
			}
		}
	}

	if err := groupIter.Close(); err != nil {
		// Log error but continue - return what we found so far
	}

	if highestPermission != PermissionNone {
		return highestPermission, nil
	}

	return PermissionNone, nil
}

// IsLibraryOwner checks if user is the owner of a library
func (m *PermissionMiddleware) IsLibraryOwner(orgID, userID, repoID string) (bool, error) {
	var ownerIDStr string
	err := m.db.Session().Query(`
		SELECT owner_id FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgID, repoID).Scan(&ownerIDStr)

	if err != nil {
		if isNotFound(err) {
			return false, nil // Library doesn't exist
		}
		return false, err
	}

	return ownerIDStr == userID, nil
}

// GetGroupRole retrieves user's role in a group
func (m *PermissionMiddleware) GetGroupRole(groupID, userID string) (GroupRole, error) {
	var role string
	err := m.db.Session().Query(`
		SELECT role FROM group_members WHERE group_id = ? AND user_id = ?
	`, groupID, userID).Scan(&role)

	if err != nil {
		return "", err
	}

	return GroupRole(role), nil
}

// RequireSuperAdmin checks that the user has the superadmin role AND belongs to
// the platform org. This prevents privilege escalation by setting the role string
// on a regular org user.
func (m *PermissionMiddleware) RequireSuperAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		orgID := c.GetString("org_id")

		if userID == "" || orgID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			c.Abort()
			return
		}

		// Must belong to platform org
		if orgID != PlatformOrgID {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}

		// Get user's role
		role, err := m.GetUserOrgRole(orgID, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check permissions"})
			c.Abort()
			return
		}

		if role != RoleSuperAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}

		c.Set("user_org_role", role)
		c.Next()
	}
}

// RequireAdminOrAbove checks that the user has at least admin role (admin or superadmin).
func (m *PermissionMiddleware) RequireAdminOrAbove() gin.HandlerFunc {
	return m.RequireOrgRole(RoleAdmin)
}

// hasRequiredOrgRole checks if user's role meets requirement
func (m *PermissionMiddleware) hasRequiredOrgRole(userRole, requiredRole OrganizationRole) bool {
	// Role hierarchy: superadmin > admin > user > readonly > guest
	roleHierarchy := map[OrganizationRole]int{
		RoleSuperAdmin: 4,
		RoleAdmin:      3,
		RoleUser:       2,
		RoleReadOnly:   1,
		RoleGuest:      0,
	}

	return roleHierarchy[userRole] >= roleHierarchy[requiredRole]
}

// hasRequiredLibraryPermission checks if user's permission meets requirement
func (m *PermissionMiddleware) hasRequiredLibraryPermission(userPerm, requiredPerm LibraryPermission) bool {
	// Permission hierarchy: owner > rw > r
	permHierarchy := map[LibraryPermission]int{
		PermissionOwner: 3,
		PermissionRW:    2,
		PermissionR:     1,
		PermissionNone:  0,
	}

	return permHierarchy[userPerm] >= permHierarchy[requiredPerm]
}

// hasRequiredGroupRole checks if user's group role meets requirement
func (m *PermissionMiddleware) hasRequiredGroupRole(userRole, requiredRole GroupRole) bool {
	// Role hierarchy: owner > admin > member
	roleHierarchy := map[GroupRole]int{
		GroupRoleOwner:  3,
		GroupRoleAdmin:  2,
		GroupRoleMember: 1,
	}

	return roleHierarchy[userRole] >= roleHierarchy[requiredRole]
}

// CanModifyLibrary checks if user can modify a library (owner or rw permission)
func (m *PermissionMiddleware) CanModifyLibrary(orgID, userID, repoID string) (bool, error) {
	permission, err := m.GetLibraryPermission(orgID, userID, repoID)
	if err != nil {
		return false, err
	}

	return permission == PermissionOwner || permission == PermissionRW, nil
}

// CanReadLibrary checks if user can read a library
func (m *PermissionMiddleware) CanReadLibrary(orgID, userID, repoID string) (bool, error) {
	permission, err := m.GetLibraryPermission(orgID, userID, repoID)
	if err != nil {
		return false, err
	}

	return permission != PermissionNone, nil
}

// HasLibraryAccess checks if user has at least the specified permission level for a library
// This is the main method used for permission checks throughout the API
func (m *PermissionMiddleware) HasLibraryAccess(orgID, userID, repoID string, requiredPermission LibraryPermission) (bool, error) {
	// Get user's permission level for this library
	permission, err := m.GetLibraryPermission(orgID, userID, repoID)
	if err != nil {
		return false, err
	}

	// Check if user has at least the required permission level
	return m.hasRequiredLibraryPermission(permission, requiredPermission), nil
}

// HasLibraryAccessCtx checks library access, with repo API token support.
// If the request was authenticated via a repo API token, it checks the token's
// scoped repo_id and permission instead of querying ownership/shares.
func (m *PermissionMiddleware) HasLibraryAccessCtx(c interface{ Get(string) (interface{}, bool); GetString(string) string }, orgID, userID, repoID string, requiredPermission LibraryPermission) (bool, error) {
	if isRepoToken, _ := c.Get("repo_api_token"); isRepoToken == true {
		tokenRepoID := c.GetString("repo_api_token_repo_id")
		tokenPerm := c.GetString("repo_api_token_permission")

		// Token must be for this specific library
		if tokenRepoID != repoID {
			return false, nil
		}

		var libPerm LibraryPermission
		switch tokenPerm {
		case "rw":
			libPerm = PermissionRW
		default:
			libPerm = PermissionR
		}

		return m.hasRequiredLibraryPermission(libPerm, requiredPermission), nil
	}

	return m.HasLibraryAccess(orgID, userID, repoID, requiredPermission)
}

// LibraryWithPermission represents a library along with the user's permission level
type LibraryWithPermission struct {
	LibraryID  uuid.UUID
	Permission LibraryPermission
}

// GetUserLibraries returns all libraries the user has access to (owned + shared)
// This is used for filtering library lists to show only accessible libraries
func (m *PermissionMiddleware) GetUserLibraries(orgID, userID string) ([]LibraryWithPermission, error) {
	librariesMap := make(map[uuid.UUID]LibraryPermission)

	// 1. Get all libraries for the org and filter by owner in application code
	// Note: We query all libraries for the org (indexed by org_id), then filter in Go
	// This avoids ALLOW FILTERING and is acceptable since we're already querying by partition key
	iter := m.db.Session().Query(`
		SELECT library_id, owner_id FROM libraries WHERE org_id = ?
	`, orgID).Iter()

	var libIDStr, ownerIDStr string
	for iter.Scan(&libIDStr, &ownerIDStr) {
		// Filter by owner_id in application code
		if ownerIDStr == userID {
			libID, _ := uuid.Parse(libIDStr)
			librariesMap[libID] = PermissionOwner
		}
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}

	// 2. Get all libraries directly shared with user
	// Note: ALLOW FILTERING used here - acceptable for shares table as it's typically small
	// TODO: Create shares_by_recipient table for better performance at scale
	iter = m.db.Session().Query(`
		SELECT library_id, permission FROM shares
		WHERE shared_to = ? AND shared_to_type = 'user'
		ALLOW FILTERING
	`, userID).Iter()

	var permission string
	for iter.Scan(&libIDStr, &permission) {
		libID, _ := uuid.Parse(libIDStr)
		// If user is owner, keep owner permission (highest)
		if existingPerm, exists := librariesMap[libID]; exists {
			if m.hasRequiredLibraryPermission(LibraryPermission(permission), existingPerm) {
				librariesMap[libID] = LibraryPermission(permission)
			}
		} else {
			librariesMap[libID] = LibraryPermission(permission)
		}
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}

	// 3. Get all groups user is a member of
	// Use groups_by_member table which is indexed by (org_id, user_id)
	groupIter := m.db.Session().Query(`
		SELECT group_id FROM groups_by_member WHERE org_id = ? AND user_id = ?
	`, orgID, userID).Iter()

	var groupIDStr string
	var groupIDs []string

	for groupIter.Scan(&groupIDStr) {
		groupIDs = append(groupIDs, groupIDStr)
	}
	if err := groupIter.Close(); err != nil {
		return nil, err
	}

	// 4. For each group, get libraries shared with that group
	for _, groupID := range groupIDs {
		// Note: ALLOW FILTERING used here - acceptable for shares table as it's typically small
		iter := m.db.Session().Query(`
			SELECT library_id, permission FROM shares
			WHERE shared_to = ? AND shared_to_type = 'group'
			ALLOW FILTERING
		`, groupID).Iter()

		for iter.Scan(&libIDStr, &permission) {
			libID, _ := uuid.Parse(libIDStr)
			// Keep highest permission level
			if existingPerm, exists := librariesMap[libID]; exists {
				if m.hasRequiredLibraryPermission(LibraryPermission(permission), existingPerm) {
					librariesMap[libID] = LibraryPermission(permission)
				}
			} else {
				librariesMap[libID] = LibraryPermission(permission)
			}
		}
		if err := iter.Close(); err != nil {
			return nil, err
		}
	}

	// Convert map to slice
	result := make([]LibraryWithPermission, 0, len(librariesMap))
	for libID, perm := range librariesMap {
		result = append(result, LibraryWithPermission{
			LibraryID:  libID,
			Permission: perm,
		})
	}

	return result, nil
}
