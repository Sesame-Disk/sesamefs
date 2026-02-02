package middleware

import (
	"encoding/json"
	"fmt"
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

// --- isNotFound tests ---

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"not found error", fmt.Errorf("not found"), true},
		{"wrapped not found", fmt.Errorf("gocql: not found"), true},
		{"unrelated error", fmt.Errorf("connection refused"), false},
		{"empty error", fmt.Errorf(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNotFound(tt.err)
			if result != tt.expected {
				t.Errorf("isNotFound(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

// --- Group role hierarchy tests ---

func TestGroupRoleHierarchy(t *testing.T) {
	tests := []struct {
		name     string
		userRole GroupRole
		required GroupRole
		expected bool
	}{
		{"owner satisfies owner", GroupRoleOwner, GroupRoleOwner, true},
		{"owner satisfies admin", GroupRoleOwner, GroupRoleAdmin, true},
		{"owner satisfies member", GroupRoleOwner, GroupRoleMember, true},
		{"admin satisfies admin", GroupRoleAdmin, GroupRoleAdmin, true},
		{"admin satisfies member", GroupRoleAdmin, GroupRoleMember, true},
		{"admin does not satisfy owner", GroupRoleAdmin, GroupRoleOwner, false},
		{"member satisfies member", GroupRoleMember, GroupRoleMember, true},
		{"member does not satisfy admin", GroupRoleMember, GroupRoleAdmin, false},
		{"member does not satisfy owner", GroupRoleMember, GroupRoleOwner, false},
	}

	pm := setupTestDB(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pm.hasRequiredGroupRole(tt.userRole, tt.required)
			if result != tt.expected {
				t.Errorf("hasRequiredGroupRole(%s, %s) = %v, want %v",
					tt.userRole, tt.required, result, tt.expected)
			}
		})
	}
}

// --- Group role constants test ---

func TestGroupRoleConstants(t *testing.T) {
	if GroupRoleOwner != "owner" {
		t.Errorf("GroupRoleOwner = %q, want %q", GroupRoleOwner, "owner")
	}
	if GroupRoleAdmin != "admin" {
		t.Errorf("GroupRoleAdmin = %q, want %q", GroupRoleAdmin, "admin")
	}
	if GroupRoleMember != "member" {
		t.Errorf("GroupRoleMember = %q, want %q", GroupRoleMember, "member")
	}
}

// --- Org role constants test ---

func TestOrgRoleConstants(t *testing.T) {
	if RoleAdmin != "admin" {
		t.Errorf("RoleAdmin = %q, want %q", RoleAdmin, "admin")
	}
	if RoleUser != "user" {
		t.Errorf("RoleUser = %q, want %q", RoleUser, "user")
	}
	if RoleReadOnly != "readonly" {
		t.Errorf("RoleReadOnly = %q, want %q", RoleReadOnly, "readonly")
	}
	if RoleGuest != "guest" {
		t.Errorf("RoleGuest = %q, want %q", RoleGuest, "guest")
	}
}

// --- RequireLibraryPermission tests with repo API token ---

func TestRequireLibraryPermission_MissingRepoID(t *testing.T) {
	pm := NewPermissionMiddleware(nil)
	mw := pm.RequireLibraryPermission("repo_id", PermissionR)

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

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestRequireLibraryPermission_RepoApiToken_Matching(t *testing.T) {
	pm := NewPermissionMiddleware(nil)
	mw := pm.RequireLibraryPermission("repo_id", PermissionR)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", "user-1")
		c.Set("org_id", "org-1")
		c.Set("repo_api_token", true)
		c.Set("repo_api_token_repo_id", "test-repo")
		c.Set("repo_api_token_permission", "rw")
		c.Next()
	})
	r.Use(mw)
	r.GET("/lib/:repo_id/test", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/lib/test-repo/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestRequireLibraryPermission_RepoApiToken_WrongRepo(t *testing.T) {
	pm := NewPermissionMiddleware(nil)
	mw := pm.RequireLibraryPermission("repo_id", PermissionR)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", "user-1")
		c.Set("org_id", "org-1")
		c.Set("repo_api_token", true)
		c.Set("repo_api_token_repo_id", "other-repo")
		c.Set("repo_api_token_permission", "rw")
		c.Next()
	})
	r.Use(mw)
	r.GET("/lib/:repo_id/test", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/lib/test-repo/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestRequireLibraryPermission_RepoApiToken_InsufficientPerm(t *testing.T) {
	pm := NewPermissionMiddleware(nil)
	mw := pm.RequireLibraryPermission("repo_id", PermissionRW)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", "user-1")
		c.Set("org_id", "org-1")
		c.Set("repo_api_token", true)
		c.Set("repo_api_token_repo_id", "test-repo")
		c.Set("repo_api_token_permission", "r") // only read, requesting rw
		c.Next()
	})
	r.Use(mw)
	r.GET("/lib/:repo_id/test", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/lib/test-repo/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

// --- RequireLibraryOwner tests ---

func TestRequireLibraryOwner_MissingRepoID(t *testing.T) {
	pm := NewPermissionMiddleware(nil)
	mw := pm.RequireLibraryOwner("repo_id")

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

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// --- RequireGroupRole tests ---

func TestRequireGroupRole_MissingGroupID(t *testing.T) {
	pm := NewPermissionMiddleware(nil)
	mw := pm.RequireGroupRole("group_id", GroupRoleMember)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", "user-1")
		c.Next()
	})
	r.Use(mw)
	r.GET("/test", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// --- HasLibraryAccessCtx tests (repo API token path) ---

func TestHasLibraryAccessCtx_RepoApiToken_Matching(t *testing.T) {
	pm := NewPermissionMiddleware(nil)

	r := gin.New()
	var hasAccess bool
	r.GET("/test", func(c *gin.Context) {
		c.Set("repo_api_token", true)
		c.Set("repo_api_token_repo_id", "repo-1")
		c.Set("repo_api_token_permission", "rw")

		result, err := pm.HasLibraryAccessCtx(c, "org-1", "user-1", "repo-1", PermissionR)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		hasAccess = result
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if !hasAccess {
		t.Error("expected access to be granted for matching repo token")
	}
}

func TestHasLibraryAccessCtx_RepoApiToken_WrongRepo(t *testing.T) {
	pm := NewPermissionMiddleware(nil)

	r := gin.New()
	var hasAccess bool
	r.GET("/test", func(c *gin.Context) {
		c.Set("repo_api_token", true)
		c.Set("repo_api_token_repo_id", "repo-1")
		c.Set("repo_api_token_permission", "rw")

		result, err := pm.HasLibraryAccessCtx(c, "org-1", "user-1", "repo-2", PermissionR)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		hasAccess = result
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if hasAccess {
		t.Error("expected access to be denied for wrong repo")
	}
}

// --- RequireAdminOrAbove tests ---

func TestRequireAdminOrAbove_RejectsEmptyAuth(t *testing.T) {
	pm := NewPermissionMiddleware(nil)
	mw := pm.RequireAdminOrAbove()

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

// --- Unknown role tests ---

func TestUnknownOrgRole_TreatedAsLowest(t *testing.T) {
	pm := setupTestDB(t)
	// Unknown role gets 0 in the hierarchy (same as guest)
	if !pm.hasRequiredOrgRole(OrganizationRole("unknown"), RoleGuest) {
		t.Error("unknown role should be treated as lowest level (same as guest)")
	}
	if pm.hasRequiredOrgRole(OrganizationRole("unknown"), RoleUser) {
		t.Error("unknown role should not satisfy user requirement")
	}
}

func TestUnknownLibraryPermission_DeniesAccess(t *testing.T) {
	pm := setupTestDB(t)
	if pm.hasRequiredLibraryPermission(LibraryPermission("unknown"), PermissionR) {
		t.Error("unknown permission should not satisfy any requirement")
	}
}

func TestUnknownGroupRole_DeniesAccess(t *testing.T) {
	pm := setupTestDB(t)
	if pm.hasRequiredGroupRole(GroupRole("unknown"), GroupRoleMember) {
		t.Error("unknown group role should not satisfy any requirement")
	}
}
