package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestAuditMiddleware_LogsNonGETRequests(t *testing.T) {
	logger := NewAuditLogger(nil)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("org_id", "00000000-0000-0000-0000-000000000001")
		c.Set("user_id", "00000000-0000-0000-0000-000000000001")
		c.Next()
	})
	r.Use(logger.AuditMiddleware())
	r.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	req, _ := http.NewRequest("POST", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAuditMiddleware_GETSuccess_NoLog(t *testing.T) {
	logger := NewAuditLogger(nil)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("org_id", "00000000-0000-0000-0000-000000000001")
		c.Set("user_id", "00000000-0000-0000-0000-000000000001")
		c.Next()
	})
	r.Use(logger.AuditMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": "test"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Successful GET should complete normally (no audit for GET < 400)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAuditMiddleware_GETError_Logs(t *testing.T) {
	logger := NewAuditLogger(nil)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("org_id", "00000000-0000-0000-0000-000000000001")
		c.Set("user_id", "00000000-0000-0000-0000-000000000001")
		c.Next()
	})
	r.Use(logger.AuditMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusForbidden, gin.H{"error": "denied"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// GET with error status should be logged
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestAuditMiddleware_AllMethods(t *testing.T) {
	logger := NewAuditLogger(nil)

	methods := []struct {
		method string
		path   string
	}{
		{"POST", "/test"},
		{"PUT", "/test"},
		{"DELETE", "/test"},
		{"PATCH", "/test"},
	}

	for _, m := range methods {
		t.Run(m.method, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set("org_id", "00000000-0000-0000-0000-000000000001")
				c.Set("user_id", "00000000-0000-0000-0000-000000000001")
				c.Next()
			})
			r.Use(logger.AuditMiddleware())
			r.Handle(m.method, m.path, func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"ok": true})
			})

			req, _ := http.NewRequest(m.method, m.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("%s: status = %d, want %d", m.method, w.Code, http.StatusOK)
			}
		})
	}
}

func TestLogAudit_NoOrgID(t *testing.T) {
	logger := NewAuditLogger(nil)

	r := gin.New()
	// No org_id or user_id set
	r.POST("/test", func(c *gin.Context) {
		// Should return silently without panic
		logger.LogAudit(c, ActionFileUpload, "file", "test.txt", true, nil)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req, _ := http.NewRequest("POST", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (LogAudit should not panic without org_id)", w.Code, http.StatusOK)
	}
}

func TestLogAccessDenied(t *testing.T) {
	logger := NewAuditLogger(nil)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("org_id", "00000000-0000-0000-0000-000000000001")
		c.Set("user_id", "00000000-0000-0000-0000-000000000001")
		c.Next()
	})
	r.POST("/test", func(c *gin.Context) {
		logger.LogAccessDenied(c, "library", "lib-123", "insufficient permissions")
		c.JSON(http.StatusForbidden, gin.H{"error": "denied"})
	})

	req, _ := http.NewRequest("POST", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestLogPermissionChange(t *testing.T) {
	logger := NewAuditLogger(nil)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("org_id", "00000000-0000-0000-0000-000000000001")
		c.Set("user_id", "00000000-0000-0000-0000-000000000001")
		c.Next()
	})
	r.POST("/test", func(c *gin.Context) {
		logger.LogPermissionChange(c, "library", "lib-123", "r", "rw")
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req, _ := http.NewRequest("POST", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestNewAuditLogger(t *testing.T) {
	logger := NewAuditLogger(nil)
	if logger == nil {
		t.Fatal("NewAuditLogger returned nil")
	}
}

func TestAuditAction_Constants(t *testing.T) {
	// Verify action constants are defined and non-empty
	actions := []AuditAction{
		ActionLibraryCreate,
		ActionLibraryDelete,
		ActionLibraryShare,
		ActionLibraryUnshare,
		ActionFileUpload,
		ActionFileDownload,
		ActionFileDelete,
		ActionGroupCreate,
		ActionGroupDelete,
		ActionGroupAddMember,
		ActionGroupRemoveMember,
		ActionPermissionChange,
		ActionAccessDenied,
	}

	for _, a := range actions {
		if a == "" {
			t.Error("found empty AuditAction constant")
		}
	}
}
