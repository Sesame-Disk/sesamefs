# Permission Middleware

Centralized permission checking and audit logging for SesameFS.

## Features

- **Organization-level role checking** (admin, user, readonly, guest)
- **Library-level permission checking** (owner, rw, r)
- **Group-level role checking** (owner, admin, member)
- **Audit logging** for all permission-related events
- **Hierarchical permission model** with proper inheritance

## Usage Examples

### 1. Basic Authentication

```go
// Require user to be authenticated
router.Use(permMiddleware.RequireAuth())
```

### 2. Organization Role Checking

```go
// Require admin role
router.DELETE("/api/v2.1/admin/users/:id",
    permMiddleware.RequireOrgRole(middleware.RoleAdmin),
    handler.DeleteUser,
)

// Require at least user role (blocks readonly and guest)
router.POST("/api/v2.1/repos/",
    permMiddleware.RequireOrgRole(middleware.RoleUser),
    handler.CreateLibrary,
)
```

### 3. Library Permission Checking

```go
// Require read permission
router.GET("/api/v2.1/repos/:repo_id/dir/",
    permMiddleware.RequireLibraryPermission("repo_id", middleware.PermissionR),
    handler.ListDirectory,
)

// Require write permission
router.POST("/api/v2.1/repos/:repo_id/file/",
    permMiddleware.RequireLibraryPermission("repo_id", middleware.PermissionRW),
    handler.UploadFile,
)

// Require owner permission
router.DELETE("/api/v2.1/repos/:repo_id/",
    permMiddleware.RequireLibraryOwner("repo_id"),
    handler.DeleteLibrary,
)
```

### 4. Group Role Checking

```go
// Require group admin or owner role
router.DELETE("/api/v2.1/groups/:group_id/members/:email/",
    permMiddleware.RequireGroupRole("group_id", middleware.GroupRoleAdmin),
    handler.RemoveGroupMember,
)

// Require group owner only
router.DELETE("/api/v2.1/groups/:group_id/",
    permMiddleware.RequireGroupRole("group_id", middleware.GroupRoleOwner),
    handler.DeleteGroup,
)
```

### 5. Manual Permission Checking

```go
// In your handler function
func (h *Handler) SomeAction(c *gin.Context) {
    userID := c.GetString("user_id")
    orgID := c.GetString("org_id")
    repoID := c.Param("repo_id")

    // Check if user can modify library
    canModify, err := h.permMiddleware.CanModifyLibrary(orgID, userID, repoID)
    if err != nil {
        c.JSON(500, gin.H{"error": "failed to check permissions"})
        return
    }

    if !canModify {
        c.JSON(403, gin.H{"error": "insufficient permissions"})
        return
    }

    // Proceed with action...
}
```

### 6. Audit Logging

```go
// Initialize audit logger
auditLogger := middleware.NewAuditLogger(db)

// Add audit middleware to log all requests
router.Use(auditLogger.AuditMiddleware())

// Manual audit logging
auditLogger.LogAudit(c, middleware.ActionLibraryShare, "library", repoID, true, map[string]interface{}{
    "shared_with": targetUserID,
    "permission": "rw",
})

// Log access denied
auditLogger.LogAccessDenied(c, "library", repoID, "user not owner")

// Log permission change
auditLogger.LogPermissionChange(c, "library", repoID, "r", "rw")
```

## Permission Hierarchies

### Organization Roles
```
admin > user > readonly > guest
```

- **admin**: Full access to organization, can manage users and settings
- **user**: Can create libraries, upload files, share content
- **readonly**: Can only read content, cannot modify
- **guest**: Limited access to shared content only

### Library Permissions
```
owner > rw > r > none
```

- **owner**: Full control, can delete library, manage shares
- **rw**: Read and write access, can modify files
- **r**: Read-only access
- **none**: No access

### Group Roles
```
owner > admin > member
```

- **owner**: Created the group, can delete group, manage all members
- **admin**: Can add/remove members (except owner)
- **member**: Regular member with no management privileges

## Audit Events

All permission-related actions are logged with:
- Timestamp
- Organization ID
- User ID
- Action type
- Resource (type and ID)
- IP address
- User agent
- Success/failure
- Additional details

## Integration with Existing Code

### Update Router Setup

```go
// In internal/api/server.go or similar
permMiddleware := middleware.NewPermissionMiddleware(db)
auditLogger := middleware.NewAuditLogger(db)

// Apply audit logging globally
router.Use(auditLogger.AuditMiddleware())

// Protected routes
api := router.Group("/api/v2.1")
api.Use(permMiddleware.RequireAuth())

// Admin routes
admin := api.Group("/admin")
admin.Use(permMiddleware.RequireOrgRole(middleware.RoleAdmin))
```

### Migrating Existing Handlers

**Before:**
```go
func (h *GroupHandler) DeleteGroup(c *gin.Context) {
    groupID := c.Param("group_id")
    userID := c.GetString("user_id")

    // Inline permission check
    var role string
    if err := h.db.Query("SELECT role...").Scan(&role); err != nil || role != "owner" {
        c.JSON(403, gin.H{"error": "permission denied"})
        return
    }

    // Delete group...
}
```

**After:**
```go
// Route registration
router.DELETE("/groups/:group_id",
    permMiddleware.RequireGroupRole("group_id", middleware.GroupRoleOwner),
    handler.DeleteGroup,
)

func (h *GroupHandler) DeleteGroup(c *gin.Context) {
    groupID := c.Param("group_id")
    // Permission already checked by middleware
    // Delete group...
}
```

## TODO: Future Enhancements

1. **Database audit table**: Create `audit_logs` table in Cassandra
2. **SIEM integration**: Send audit events to security monitoring system
3. **Elasticsearch integration**: Index audit logs for analysis
4. **Rate limiting**: Add rate limiting per user/org
5. **Suspicious activity detection**: Alert on unusual patterns
6. **Permission delegation**: Allow users to delegate permissions temporarily
7. **API key permissions**: Support API keys with scoped permissions

## Testing

```bash
# Run tests
go test ./internal/middleware/...

# Test specific middleware
go test ./internal/middleware -run TestRequireOrgRole
```

## Security Considerations

1. **Always check permissions server-side**: Never trust client-side checks
2. **Use middleware for routes**: Prevents bypassing permission checks
3. **Log all access denials**: Helps detect attack patterns
4. **Review audit logs regularly**: Monitor for suspicious activity
5. **Principle of least privilege**: Grant minimum required permissions
6. **Regular permission audits**: Review user permissions quarterly
