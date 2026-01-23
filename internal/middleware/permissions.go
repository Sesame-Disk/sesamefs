package middleware

import (
	"net/http"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

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
	RoleAdmin    OrganizationRole = "admin"
	RoleUser     OrganizationRole = "user"
	RoleReadOnly OrganizationRole = "readonly"
	RoleGuest    OrganizationRole = "guest"
)

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
	orgUUID, _ := uuid.Parse(orgID)
	userUUID, _ := uuid.Parse(userID)
	repoUUID, _ := uuid.Parse(repoID)

	// Check if user is the owner
	var ownerID uuid.UUID
	err := m.db.Session().Query(`
		SELECT owner_id FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgUUID, repoUUID).Scan(&ownerID)

	if err != nil {
		return PermissionNone, err
	}

	if ownerID == userUUID {
		return PermissionOwner, nil
	}

	// Check if library is shared with user
	var permission string
	err = m.db.Session().Query(`
		SELECT permission FROM shares
		WHERE library_id = ? AND shared_to = ? AND shared_to_type = 'user'
	`, repoUUID, userUUID).Scan(&permission)

	if err == nil {
		return LibraryPermission(permission), nil
	}

	// Check if library is shared with user's groups
	// TODO: Implement group sharing check

	return PermissionNone, nil
}

// IsLibraryOwner checks if user is the owner of a library
func (m *PermissionMiddleware) IsLibraryOwner(orgID, userID, repoID string) (bool, error) {
	orgUUID, _ := uuid.Parse(orgID)
	userUUID, _ := uuid.Parse(userID)
	repoUUID, _ := uuid.Parse(repoID)

	var ownerID uuid.UUID
	err := m.db.Session().Query(`
		SELECT owner_id FROM libraries WHERE org_id = ? AND library_id = ?
	`, orgUUID, repoUUID).Scan(&ownerID)

	if err != nil {
		return false, err
	}

	return ownerID == userUUID, nil
}

// GetGroupRole retrieves user's role in a group
func (m *PermissionMiddleware) GetGroupRole(groupID, userID string) (GroupRole, error) {
	groupUUID, _ := uuid.Parse(groupID)
	userUUID, _ := uuid.Parse(userID)

	var role string
	err := m.db.Session().Query(`
		SELECT role FROM group_members WHERE group_id = ? AND user_id = ?
	`, groupUUID, userUUID).Scan(&role)

	if err != nil {
		return "", err
	}

	return GroupRole(role), nil
}

// hasRequiredOrgRole checks if user's role meets requirement
func (m *PermissionMiddleware) hasRequiredOrgRole(userRole, requiredRole OrganizationRole) bool {
	// Role hierarchy: admin > user > readonly > guest
	roleHierarchy := map[OrganizationRole]int{
		RoleAdmin:    3,
		RoleUser:     2,
		RoleReadOnly: 1,
		RoleGuest:    0,
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
