package middleware

import (
	"log"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AuditAction represents types of auditable actions
type AuditAction string

const (
	ActionLibraryCreate    AuditAction = "library.create"
	ActionLibraryDelete    AuditAction = "library.delete"
	ActionLibraryShare     AuditAction = "library.share"
	ActionLibraryUnshare   AuditAction = "library.unshare"
	ActionFileUpload       AuditAction = "file.upload"
	ActionFileDownload     AuditAction = "file.download"
	ActionFileDelete       AuditAction = "file.delete"
	ActionGroupCreate      AuditAction = "group.create"
	ActionGroupDelete      AuditAction = "group.delete"
	ActionGroupAddMember   AuditAction = "group.add_member"
	ActionGroupRemoveMember AuditAction = "group.remove_member"
	ActionPermissionChange AuditAction = "permission.change"
	ActionAccessDenied     AuditAction = "access.denied"
)

// AuditEvent represents an audit log entry
type AuditEvent struct {
	EventID     uuid.UUID              `json:"event_id"`
	Timestamp   time.Time              `json:"timestamp"`
	OrgID       uuid.UUID              `json:"org_id"`
	UserID      uuid.UUID              `json:"user_id"`
	Action      AuditAction            `json:"action"`
	ResourceType string                `json:"resource_type"` // library, file, group, etc.
	ResourceID  string                 `json:"resource_id"`
	IPAddress   string                 `json:"ip_address"`
	UserAgent   string                 `json:"user_agent"`
	Success     bool                   `json:"success"`
	Details     map[string]interface{} `json:"details,omitempty"`
}

// AuditLogger handles audit logging
type AuditLogger struct {
	db *db.DB
}

// NewAuditLogger creates a new audit logger
func NewAuditLogger(database *db.DB) *AuditLogger {
	return &AuditLogger{db: database}
}

// LogAudit logs an audit event
func (a *AuditLogger) LogAudit(c *gin.Context, action AuditAction, resourceType, resourceID string, success bool, details map[string]interface{}) {
	orgID := c.GetString("org_id")
	userID := c.GetString("user_id")

	if orgID == "" || userID == "" {
		// Can't log without org/user context
		return
	}

	orgUUID, _ := uuid.Parse(orgID)
	userUUID, _ := uuid.Parse(userID)

	event := AuditEvent{
		EventID:      uuid.New(),
		Timestamp:    time.Now(),
		OrgID:        orgUUID,
		UserID:       userUUID,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		IPAddress:    c.ClientIP(),
		UserAgent:    c.GetHeader("User-Agent"),
		Success:      success,
		Details:      details,
	}

	// Log to console (in production, this would go to a proper audit log system)
	log.Printf("[AUDIT] org=%s user=%s action=%s resource=%s/%s success=%v ip=%s",
		orgID, userID, action, resourceType, resourceID, success, event.IPAddress)

	// TODO: Store in audit_logs table when we create it
	// For now, we'll just log to console
	// In production, you'd want:
	// - Store in Cassandra audit_logs table
	// - Send to SIEM system
	// - Stream to Elasticsearch for analysis
	// - Alert on suspicious patterns
}

// LogAccessDenied logs an access denied event
func (a *AuditLogger) LogAccessDenied(c *gin.Context, resourceType, resourceID, reason string) {
	a.LogAudit(c, ActionAccessDenied, resourceType, resourceID, false, map[string]interface{}{
		"reason": reason,
	})
}

// LogPermissionChange logs a permission change event
func (a *AuditLogger) LogPermissionChange(c *gin.Context, resourceType, resourceID, oldPerm, newPerm string) {
	a.LogAudit(c, ActionPermissionChange, resourceType, resourceID, true, map[string]interface{}{
		"old_permission": oldPerm,
		"new_permission": newPerm,
	})
}

// AuditMiddleware creates a middleware that logs all requests
func (a *AuditLogger) AuditMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Record start time
		start := time.Now()

		// Process request
		c.Next()

		// Log after request completes
		duration := time.Since(start)
		statusCode := c.Writer.Status()

		// Only log significant actions (not GET requests for public resources)
		if c.Request.Method != "GET" || statusCode >= 400 {
			details := map[string]interface{}{
				"method":      c.Request.Method,
				"path":        c.Request.URL.Path,
				"status_code": statusCode,
				"duration_ms": duration.Milliseconds(),
			}

			// Determine action based on method and path
			var action AuditAction
			switch c.Request.Method {
			case "POST":
				action = "resource.create"
			case "PUT", "PATCH":
				action = "resource.update"
			case "DELETE":
				action = "resource.delete"
			default:
				action = "resource.access"
			}

			a.LogAudit(c, AuditAction(action), "api", c.Request.URL.Path, statusCode < 400, details)
		}
	}
}
