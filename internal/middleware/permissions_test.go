package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// setupTestDB returns nil; these tests don't need a DB for hierarchy logic.
func setupTestDB(t *testing.T) *PermissionMiddleware {
	return NewPermissionMiddleware(nil)
}

// --- RequireAuth middleware tests ---

func TestRequireAuth_RejectsEmptyContext(t *testing.T) {
	pm := NewPermissionMiddleware(nil)
	mw := pm.RequireAuth()

	r := gin.New()
	r.Use(mw)
	r.GET("/test", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	var body map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["error"] != "authentication required" {
		t.Errorf("unexpected error: %v", body["error"])
	}
}

func TestRequireAuth_RejectsMissingOrgID(t *testing.T) {
	pm := NewPermissionMiddleware(nil)
	mw := pm.RequireAuth()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", "user-1")
		// org_id intentionally missing
		c.Next()
	})
	r.Use(mw)
	r.GET("/test", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRequireAuth_RejectsMissingUserID(t *testing.T) {
	pm := NewPermissionMiddleware(nil)
	mw := pm.RequireAuth()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("org_id", "org-1")
		// user_id intentionally missing
		c.Next()
	})
	r.Use(mw)
	r.GET("/test", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRequireAuth_AllowsAuthenticatedUser(t *testing.T) {
	pm := NewPermissionMiddleware(nil)
	mw := pm.RequireAuth()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", "user-1")
		c.Set("org_id", "org-1")
		c.Next()
	})
	r.Use(mw)
	r.GET("/test", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// --- RequireSuperAdmin middleware tests ---

func TestRequireSuperAdmin_RejectsNonPlatformOrg(t *testing.T) {
	pm := NewPermissionMiddleware(nil)
	mw := pm.RequireSuperAdmin()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", "user-1")
		c.Set("org_id", "00000000-0000-0000-0000-000000000001") // non-platform
		c.Next()
	})
	r.Use(mw)
	r.GET("/test", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestRequireSuperAdmin_RejectsEmptyAuth(t *testing.T) {
	pm := NewPermissionMiddleware(nil)
	mw := pm.RequireSuperAdmin()

	r := gin.New()
	r.Use(mw)
	r.GET("/test", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// --- RequireOrgRole middleware tests ---

func TestRequireOrgRole_RejectsEmptyAuth(t *testing.T) {
	pm := NewPermissionMiddleware(nil)
	mw := pm.RequireOrgRole(RoleUser)

	r := gin.New()
	r.Use(mw)
	r.GET("/test", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRequireOrgRole_RejectsMissingUserID(t *testing.T) {
	pm := NewPermissionMiddleware(nil)
	mw := pm.RequireOrgRole(RoleAdmin)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("org_id", "org-1")
		c.Next()
	})
	r.Use(mw)
	r.GET("/test", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// --- Permission hierarchy tests (existing, but calling real methods) ---

func TestHasLibraryAccess_Owner(t *testing.T) {
	pm := setupTestDB(t)

	if !pm.hasRequiredLibraryPermission(PermissionOwner, PermissionRW) {
		t.Error("Owner should have RW permission")
	}
	if !pm.hasRequiredLibraryPermission(PermissionOwner, PermissionR) {
		t.Error("Owner should have R permission")
	}
	if !pm.hasRequiredLibraryPermission(PermissionRW, PermissionR) {
		t.Error("RW permission should satisfy R requirement")
	}
	if pm.hasRequiredLibraryPermission(PermissionR, PermissionRW) {
		t.Error("R permission should not satisfy RW requirement")
	}
	if pm.hasRequiredLibraryPermission(PermissionNone, PermissionR) {
		t.Error("None permission should not satisfy R requirement")
	}
}

func TestHasRequiredOrgRole(t *testing.T) {
	pm := setupTestDB(t)

	if !pm.hasRequiredOrgRole(RoleSuperAdmin, RoleAdmin) {
		t.Error("Superadmin should have admin privileges")
	}
	if !pm.hasRequiredOrgRole(RoleSuperAdmin, RoleUser) {
		t.Error("Superadmin should have user privileges")
	}
	if !pm.hasRequiredOrgRole(RoleAdmin, RoleUser) {
		t.Error("Admin should have user privileges")
	}
	if pm.hasRequiredOrgRole(RoleAdmin, RoleSuperAdmin) {
		t.Error("Admin should not have superadmin privileges")
	}
	if !pm.hasRequiredOrgRole(RoleUser, RoleReadOnly) {
		t.Error("User should have readonly privileges")
	}
	if !pm.hasRequiredOrgRole(RoleReadOnly, RoleGuest) {
		t.Error("Readonly should have guest privileges")
	}
	if pm.hasRequiredOrgRole(RoleGuest, RoleUser) {
		t.Error("Guest should not have user privileges")
	}
	if pm.hasRequiredOrgRole(RoleReadOnly, RoleUser) {
		t.Error("Readonly should not have user privileges")
	}
}

func TestLibraryPermissionHierarchy(t *testing.T) {
	tests := []struct {
		name           string
		userPermission LibraryPermission
		requiredPerm   LibraryPermission
		expectedResult bool
	}{
		{"owner satisfies rw", PermissionOwner, PermissionRW, true},
		{"owner satisfies r", PermissionOwner, PermissionR, true},
		{"rw satisfies r", PermissionRW, PermissionR, true},
		{"rw satisfies rw", PermissionRW, PermissionRW, true},
		{"r does not satisfy rw", PermissionR, PermissionRW, false},
		{"r satisfies r", PermissionR, PermissionR, true},
		{"none does not satisfy r", PermissionNone, PermissionR, false},
		{"none does not satisfy rw", PermissionNone, PermissionRW, false},
		{"none does not satisfy owner", PermissionNone, PermissionOwner, false},
	}

	pm := setupTestDB(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pm.hasRequiredLibraryPermission(tt.userPermission, tt.requiredPerm)
			if result != tt.expectedResult {
				t.Errorf("hasRequiredLibraryPermission(%s, %s) = %v, want %v",
					tt.userPermission, tt.requiredPerm, result, tt.expectedResult)
			}
		})
	}
}

func TestOrgRoleHierarchy(t *testing.T) {
	tests := []struct {
		name           string
		userRole       OrganizationRole
		requiredRole   OrganizationRole
		expectedResult bool
	}{
		{"superadmin satisfies superadmin", RoleSuperAdmin, RoleSuperAdmin, true},
		{"superadmin satisfies admin", RoleSuperAdmin, RoleAdmin, true},
		{"superadmin satisfies user", RoleSuperAdmin, RoleUser, true},
		{"superadmin satisfies readonly", RoleSuperAdmin, RoleReadOnly, true},
		{"superadmin satisfies guest", RoleSuperAdmin, RoleGuest, true},
		{"admin does not satisfy superadmin", RoleAdmin, RoleSuperAdmin, false},
		{"admin satisfies admin", RoleAdmin, RoleAdmin, true},
		{"admin satisfies user", RoleAdmin, RoleUser, true},
		{"admin satisfies readonly", RoleAdmin, RoleReadOnly, true},
		{"admin satisfies guest", RoleAdmin, RoleGuest, true},
		{"user satisfies user", RoleUser, RoleUser, true},
		{"user satisfies readonly", RoleUser, RoleReadOnly, true},
		{"user satisfies guest", RoleUser, RoleGuest, true},
		{"user does not satisfy admin", RoleUser, RoleAdmin, false},
		{"user does not satisfy superadmin", RoleUser, RoleSuperAdmin, false},
		{"readonly satisfies readonly", RoleReadOnly, RoleReadOnly, true},
		{"readonly satisfies guest", RoleReadOnly, RoleGuest, true},
		{"readonly does not satisfy user", RoleReadOnly, RoleUser, false},
		{"guest satisfies guest", RoleGuest, RoleGuest, true},
		{"guest does not satisfy readonly", RoleGuest, RoleReadOnly, false},
		{"guest does not satisfy user", RoleGuest, RoleUser, false},
		{"guest does not satisfy admin", RoleGuest, RoleAdmin, false},
		{"guest does not satisfy superadmin", RoleGuest, RoleSuperAdmin, false},
	}

	pm := setupTestDB(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pm.hasRequiredOrgRole(tt.userRole, tt.requiredRole)
			if result != tt.expectedResult {
				t.Errorf("hasRequiredOrgRole(%s, %s) = %v, want %v",
					tt.userRole, tt.requiredRole, result, tt.expectedResult)
			}
		})
	}
}

func TestLibraryWithPermissionStruct(t *testing.T) {
	libID := uuid.New()
	lwp := LibraryWithPermission{
		LibraryID:  libID,
		Permission: PermissionRW,
	}

	if lwp.LibraryID != libID {
		t.Error("LibraryID not set correctly")
	}
	if lwp.Permission != PermissionRW {
		t.Error("Permission not set correctly")
	}
}

func TestPlatformOrgID(t *testing.T) {
	expected := "00000000-0000-0000-0000-000000000000"
	if PlatformOrgID != expected {
		t.Errorf("PlatformOrgID = %q, want %q", PlatformOrgID, expected)
	}
	defaultOrgID := "00000000-0000-0000-0000-000000000001"
	if PlatformOrgID == defaultOrgID {
		t.Error("PlatformOrgID must be different from default org ID")
	}
}

func TestSuperAdminConstant(t *testing.T) {
	if RoleSuperAdmin != "superadmin" {
		t.Errorf("RoleSuperAdmin = %q, want %q", RoleSuperAdmin, "superadmin")
	}
}
