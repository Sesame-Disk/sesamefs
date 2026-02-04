package v2

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sesame-Disk/sesamefs/internal/config"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// mockTokenCreator implements TokenCreator interface for testing
type mockTokenCreator struct{}

func (m *mockTokenCreator) CreateUploadToken(orgID, repoID, path, userID string) (string, error) {
	return "mock-upload-token-" + repoID, nil
}

func (m *mockTokenCreator) CreateDownloadToken(orgID, repoID, path, userID string) (string, error) {
	return "mock-download-token-" + repoID, nil
}

// TestErrorPageHTML tests the error page HTML generator
func TestErrorPageHTML(t *testing.T) {
	tests := []struct {
		title    string
		message  string
		expected []string
	}{
		{
			title:   "File Not Found",
			message: "The requested file could not be found.",
			expected: []string{
				"<title>File Not Found - SesameFS</title>",
				"<h1>File Not Found</h1>",
				"<p>The requested file could not be found.</p>",
			},
		},
		{
			title:   "Authentication Required",
			message: "Please provide a valid authentication token.",
			expected: []string{
				"<title>Authentication Required - SesameFS</title>",
				"<h1>Authentication Required</h1>",
				"<p>Please provide a valid authentication token.</p>",
			},
		},
		{
			title:   "Internal Error",
			message: "Something went wrong.",
			expected: []string{
				"<!DOCTYPE html>",
				"error-container",
				"#c0392b", // Error color
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			result := errorPageHTML(tt.title, tt.message)

			for _, exp := range tt.expected {
				if !strings.Contains(result, exp) {
					t.Errorf("errorPageHTML(%q, %q) missing expected content: %q", tt.title, tt.message, exp)
				}
			}
		})
	}
}

// TestOnlyOfficeEditorHTML tests the OnlyOffice editor HTML generator
func TestOnlyOfficeEditorHTML(t *testing.T) {
	config := OnlyOfficeConfig{
		Document: OnlyOfficeDocument{
			FileType: "docx",
			Key:      "abc123def456",
			Title:    "test-document.docx",
			URL:      "https://example.com/download/test.docx",
			Permissions: &OnlyOfficePermissions{
				Edit:      true,
				Download:  true,
				Print:     true,
				Copy:      true,
				Review:    true,
				Comment:   true,
				FillForms: true,
			},
		},
		DocumentType: "word",
		EditorConfig: OnlyOfficeEditorConfig{
			CallbackURL: "https://example.com/callback",
			Mode:        "edit",
			User: OnlyOfficeUser{
				ID:   "user-123",
				Name: "Test User",
			},
			Customization: &OnlyOfficeCustomization{
				Forcesave:  true,
				SubmitForm: false,
			},
		},
		Token: "jwt-token-here",
	}

	result := onlyOfficeEditorHTML("https://office.example.com/api.js", config, "test-document.docx")

	// Check for required elements
	// Note: json.Marshal produces compact JSON without spaces after colons
	expected := []string{
		"<!DOCTYPE html>",
		"<title>test-document.docx - SesameFS</title>",
		`<script src="https://office.example.com/api.js"></script>`,
		`"fileType":"docx"`,
		`"key":"abc123def456"`,
		`"title":"test-document.docx"`,
		"example.com", // URL is escaped, just check domain
		"test.docx",   // And filename
		`"documentType":"word"`,
		`"mode":"edit"`,
		`"id":"user-123"`,
		`"name":"Test User"`,
		`"token":"jwt-token-here"`,
		"DocsAPI.DocEditor",
		"editor-container",
		"loading-spinner",
	}

	for _, exp := range expected {
		if !strings.Contains(result, exp) {
			t.Errorf("onlyOfficeEditorHTML missing expected content: %q", exp)
		}
	}
}

// TestOnlyOfficeEditorHTMLWithoutToken tests HTML generation without JWT token
func TestOnlyOfficeEditorHTMLWithoutToken(t *testing.T) {
	config := OnlyOfficeConfig{
		Document: OnlyOfficeDocument{
			FileType: "xlsx",
			Key:      "spreadsheet-key",
			Title:    "data.xlsx",
			URL:      "https://example.com/data.xlsx",
			Permissions: &OnlyOfficePermissions{
				Edit:      false,
				Download:  true,
				Print:     true,
				Copy:      true,
				Review:    false,
				Comment:   false,
				FillForms: true,
			},
		},
		DocumentType: "cell",
		EditorConfig: OnlyOfficeEditorConfig{
			CallbackURL: "https://example.com/callback",
			Mode:        "view",
			User: OnlyOfficeUser{
				ID:   "viewer-456",
				Name: "Viewer User",
			},
			Customization: &OnlyOfficeCustomization{
				Forcesave:  true,
				SubmitForm: false,
			},
		},
		// Token is empty - should not include token in config
	}

	result := onlyOfficeEditorHTML("https://office.example.com/api.js", config, "data.xlsx")

	// Should NOT contain token field when token is empty
	if strings.Contains(result, `"token":`) {
		t.Error("onlyOfficeEditorHTML should not include token field when token is empty")
	}

	// Should still contain other required fields (json.Marshal compact format)
	if !strings.Contains(result, `"mode":"view"`) {
		t.Error("onlyOfficeEditorHTML missing mode field")
	}

	if !strings.Contains(result, `"documentType":"cell"`) {
		t.Error("onlyOfficeEditorHTML missing documentType field")
	}
}

// TestIsOnlyOfficeFile tests the file extension checker
func TestIsOnlyOfficeFile(t *testing.T) {
	h := &FileViewHandler{
		config: &config.Config{
			OnlyOffice: config.OnlyOfficeConfig{
				ViewExtensions: []string{"doc", "docx", "xls", "xlsx", "ppt", "pptx", "odt", "pdf"},
				EditExtensions: []string{"docx", "xlsx", "pptx"},
			},
		},
	}

	tests := []struct {
		ext      string
		expected bool
	}{
		{"docx", true},
		{"xlsx", true},
		{"pptx", true},
		{"pdf", true},
		{"doc", true},
		{"odt", true},
		{"txt", false},
		{"jpg", false},
		{"png", false},
		{"go", false},
		{"", false},
		{"DOCX", false}, // Case sensitive - ext should be lowercased before calling
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			result := h.isOnlyOfficeFile(tt.ext)
			if result != tt.expected {
				t.Errorf("isOnlyOfficeFile(%q) = %v, want %v", tt.ext, result, tt.expected)
			}
		})
	}
}

// devTokenAuthMiddleware is a simple dev-mode auth middleware for testing.
// It validates tokens against a list of dev token entries.
func devTokenAuthMiddleware(tokens []config.DevTokenEntry) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		token := ""
		if strings.HasPrefix(auth, "Token ") {
			token = strings.TrimPrefix(auth, "Token ")
		} else if strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimPrefix(auth, "Bearer ")
		}
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			c.Abort()
			return
		}
		for _, entry := range tokens {
			if entry.Token == token {
				c.Set("user_id", entry.UserID)
				c.Set("org_id", entry.OrgID)
				c.Next()
				return
			}
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		c.Abort()
	}
}

// TestFileViewAuthWrapper tests the fileViewAuthWrapper function
func TestFileViewAuthWrapper(t *testing.T) {
	devTokens := []config.DevTokenEntry{
		{Token: "valid-token-123", UserID: "user-1", OrgID: "org-1"},
		{Token: "admin-token-456", UserID: "admin", OrgID: "org-admin"},
	}

	serverAuth := devTokenAuthMiddleware(devTokens)

	tests := []struct {
		name           string
		authHeader     string
		queryToken     string
		expectedStatus int
		expectUserID   string
		expectOrgID    string
	}{
		{
			name:           "valid token in header",
			authHeader:     "Token valid-token-123",
			expectedStatus: http.StatusOK,
			expectUserID:   "user-1",
			expectOrgID:    "org-1",
		},
		{
			name:           "valid bearer token in header",
			authHeader:     "Bearer valid-token-123",
			expectedStatus: http.StatusOK,
			expectUserID:   "user-1",
			expectOrgID:    "org-1",
		},
		{
			name:           "valid token in query param",
			queryToken:     "admin-token-456",
			expectedStatus: http.StatusOK,
			expectUserID:   "admin",
			expectOrgID:    "org-admin",
		},
		{
			name:           "no token provided",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "invalid token",
			authHeader:     "Token invalid-token",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "header takes precedence over query",
			authHeader:     "Token valid-token-123",
			queryToken:     "admin-token-456",
			expectedStatus: http.StatusOK,
			expectUserID:   "user-1", // From header token
			expectOrgID:    "org-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(fileViewAuthWrapper(serverAuth))
			r.GET("/test", func(c *gin.Context) {
				userID := c.GetString("user_id")
				orgID := c.GetString("org_id")
				c.JSON(http.StatusOK, gin.H{"user_id": userID, "org_id": orgID})
			})

			path := "/test"
			if tt.queryToken != "" {
				path += "?token=" + tt.queryToken
			}

			req, _ := http.NewRequest("GET", path, nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.expectedStatus)
			}

			if tt.expectedStatus == http.StatusOK {
				body := w.Body.String()
				if tt.expectUserID != "" && !strings.Contains(body, tt.expectUserID) {
					t.Errorf("response missing expected user_id: %s", tt.expectUserID)
				}
				if tt.expectOrgID != "" && !strings.Contains(body, tt.expectOrgID) {
					t.Errorf("response missing expected org_id: %s", tt.expectOrgID)
				}
			}
		})
	}
}

// TestFileViewAuthWrapperQueryParamPromotion tests that query param tokens
// get promoted to Authorization header before reaching the server auth middleware
func TestFileViewAuthWrapperQueryParamPromotion(t *testing.T) {
	// Use a serverAuth that rejects all requests to verify the wrapper promotes the token
	rejectAll := func(c *gin.Context) {
		// Check that the Authorization header was set from query param
		auth := c.GetHeader("Authorization")
		if auth == "Token my-query-token" {
			c.Set("user_id", "promoted-user")
			c.Next()
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no auth"})
		c.Abort()
	}

	r := gin.New()
	r.Use(fileViewAuthWrapper(rejectAll))
	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"user_id": c.GetString("user_id")})
	})

	req, _ := http.NewRequest("GET", "/test?token=my-query-token", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// TestViewFileRedirectsNonOfficeFiles tests that non-office files redirect to download
func TestViewFileRedirectsNonOfficeFiles(t *testing.T) {
	h := &FileViewHandler{
		config: &config.Config{
			OnlyOffice: config.OnlyOfficeConfig{
				Enabled:        true,
				ViewExtensions: []string{"docx", "xlsx", "pptx"},
			},
		},
		serverURL:    "http://localhost:8080",
		tokenCreator: &mockTokenCreator{},
	}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", "test-user")
		c.Set("org_id", "test-org")
		c.Next()
	})
	r.GET("/lib/:repo_id/file/*filepath", h.ViewFile)

	tests := []struct {
		name         string
		filepath     string
		expectStatus int
		expectRedirect bool
	}{
		{
			name:           "dmg file redirects",
			filepath:       "/test.dmg",
			expectStatus:   http.StatusFound,
			expectRedirect: true,
		},
		{
			name:           "zip file redirects",
			filepath:       "/archive.zip",
			expectStatus:   http.StatusFound,
			expectRedirect: true,
		},
		{
			name:           "png file redirects",
			filepath:       "/image.png",
			expectStatus:   http.StatusFound,
			expectRedirect: true,
		},
		{
			name:           "txt file redirects",
			filepath:       "/readme.txt",
			expectStatus:   http.StatusFound,
			expectRedirect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/lib/repo-123/file"+tt.filepath, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.expectStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.expectStatus)
			}

			if tt.expectRedirect {
				location := w.Header().Get("Location")
				// Redirects to seafhttp download endpoint with token
				if !strings.Contains(location, "/seafhttp/files/") {
					t.Errorf("redirect location = %q, expected seafhttp download URL", location)
				}
			}
		})
	}
}

// TestViewFileOnlyOfficeDisabled tests behavior when OnlyOffice is disabled
func TestViewFileOnlyOfficeDisabled(t *testing.T) {
	h := &FileViewHandler{
		config: &config.Config{
			OnlyOffice: config.OnlyOfficeConfig{
				Enabled:        false, // Disabled
				ViewExtensions: []string{"docx", "xlsx"},
			},
		},
		serverURL:    "http://localhost:8080",
		tokenCreator: &mockTokenCreator{},
	}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", "test-user")
		c.Set("org_id", "test-org")
		c.Next()
	})
	r.GET("/lib/:repo_id/file/*filepath", h.ViewFile)

	// Even docx files should redirect when OnlyOffice is disabled
	req, _ := http.NewRequest("GET", "/lib/repo-123/file/document.docx", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("with OnlyOffice disabled, docx should redirect, got status %d", w.Code)
	}

	location := w.Header().Get("Location")
	if !strings.Contains(location, "/seafhttp/files/") {
		t.Errorf("expected redirect to seafhttp download, got %q", location)
	}
}

// TestViewFilePathNormalization tests that file paths are normalized correctly
func TestViewFilePathNormalization(t *testing.T) {
	h := &FileViewHandler{
		config: &config.Config{
			OnlyOffice: config.OnlyOfficeConfig{
				Enabled:        false, // Disabled to just test path handling
				ViewExtensions: []string{},
			},
		},
		serverURL:    "http://localhost:8080",
		tokenCreator: &mockTokenCreator{},
	}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", "test-user")
		c.Set("org_id", "test-org")
		c.Next()
	})
	r.GET("/lib/:repo_id/file/*filepath", h.ViewFile)

	tests := []struct {
		name           string
		requestPath    string
		expectFilename string
	}{
		{
			name:           "path with leading slash",
			requestPath:    "/lib/repo-123/file/docs/file.txt",
			expectFilename: "file.txt",
		},
		{
			name:           "path in subdirectory",
			requestPath:    "/lib/repo-123/file/nested/deep/file.pdf",
			expectFilename: "file.pdf",
		},
		{
			name:           "root file",
			requestPath:    "/lib/repo-123/file/root.txt",
			expectFilename: "root.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", tt.requestPath, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			// Check redirect URL contains the filename
			location := w.Header().Get("Location")
			if !strings.Contains(location, "/"+tt.expectFilename) {
				t.Errorf("redirect URL = %q, expected to contain /%s", location, tt.expectFilename)
			}
		})
	}
}

// TestRegisterFileViewRoutes tests that routes are registered correctly
func TestRegisterFileViewRoutes(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{
			DevMode: true,
			DevTokens: []config.DevTokenEntry{
				{Token: "test-token", UserID: "user", OrgID: "org"},
			},
		},
		OnlyOffice: config.OnlyOfficeConfig{
			Enabled: false,
		},
	}

	r := gin.New()

	// Register routes with mock token creator and dev auth middleware
	devAuth := devTokenAuthMiddleware(cfg.Auth.DevTokens)
	RegisterFileViewRoutes(r, nil, cfg, nil, nil, &mockTokenCreator{}, "http://localhost:8082", devAuth)

	// Test that the route exists
	req, _ := http.NewRequest("GET", "/lib/repo-123/file/test.txt?token=test-token", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Should get a redirect (not 404), proving route is registered
	if w.Code == http.StatusNotFound {
		t.Error("route /lib/:repo_id/file/*filepath not registered")
	}

	// With valid token, should redirect to download
	if w.Code != http.StatusFound {
		t.Errorf("expected redirect (302), got %d", w.Code)
	}
}

// TestOnlyOfficeEditorHTMLCustomizations tests that customization options are present
func TestOnlyOfficeEditorHTMLCustomizations(t *testing.T) {
	config := OnlyOfficeConfig{
		Document: OnlyOfficeDocument{
			FileType: "docx",
			Key:      "key123",
			Title:    "doc.docx",
			URL:      "http://example.com/doc.docx",
			Permissions: &OnlyOfficePermissions{
				Edit:      true,
				Download:  true,
				Print:     true,
				Copy:      true,
				Review:    true,
				Comment:   true,
				FillForms: true,
			},
		},
		DocumentType: "word",
		EditorConfig: OnlyOfficeEditorConfig{
			CallbackURL: "http://example.com/callback",
			Mode:        "edit",
			User:        OnlyOfficeUser{ID: "1", Name: "User"},
			Customization: &OnlyOfficeCustomization{
				Forcesave:  true,
				SubmitForm: false,
			},
		},
	}

	result := onlyOfficeEditorHTML("http://office/api.js", config, "doc.docx")

	// Debug: print relevant part of result
	if strings.Contains(result, "Template Error") {
		t.Fatalf("Template error: %s", result)
	}

	// Check for simplified customization options (Seahub-compatible minimal config)
	// json.Marshal produces compact JSON without spaces
	if !strings.Contains(result, `"forcesave":`) || !strings.Contains(result, "true") {
		t.Error("onlyOfficeEditorHTML missing forcesave: true customization")
	}
	// submitForm is omitempty and false in this test, so it won't appear in JSON output
	// Verify forcesave is present instead (it's true so always serialized)
	if !strings.Contains(result, `"forcesave":true`) {
		t.Error("onlyOfficeEditorHTML missing forcesave customization value")
	}

	// Also verify basic editor config is present (json.Marshal compact format)
	if !strings.Contains(result, `"mode":"edit"`) {
		t.Error("onlyOfficeEditorHTML missing mode field")
	}
	if !strings.Contains(result, `"documentType":"word"`) {
		t.Error("onlyOfficeEditorHTML missing documentType field")
	}
}

// TestOnlyOfficeEditorHTMLLoadingState tests that loading state is present
func TestOnlyOfficeEditorHTMLLoadingState(t *testing.T) {
	config := OnlyOfficeConfig{
		Document: OnlyOfficeDocument{
			FileType: "xlsx",
			Key:      "key",
			Title:    "sheet.xlsx",
			URL:      "http://example.com/sheet.xlsx",
			Permissions: &OnlyOfficePermissions{
				Edit: false, Download: true, Print: true, Copy: true, FillForms: true,
			},
		},
		DocumentType: "cell",
		EditorConfig: OnlyOfficeEditorConfig{
			CallbackURL:   "http://example.com/callback",
			Mode:          "view",
			User:          OnlyOfficeUser{ID: "1", Name: "User"},
			Customization: &OnlyOfficeCustomization{Forcesave: true},
		},
	}

	result := onlyOfficeEditorHTML("http://office/api.js", config, "sheet.xlsx")

	// Check for loading state elements
	loadingElements := []string{
		"loading-spinner",
		"Loading document...",
		"@keyframes spin",
	}

	for _, elem := range loadingElements {
		if !strings.Contains(result, elem) {
			t.Errorf("onlyOfficeEditorHTML missing loading element: %s", elem)
		}
	}
}

// TestDownloadHistoricFileMissingObjID tests that missing obj_id returns 400
func TestDownloadHistoricFileMissingObjID(t *testing.T) {
	h := &FileViewHandler{
		config: &config.Config{},
	}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", "test-user")
		c.Set("org_id", "test-org")
		c.Next()
	})
	r.GET("/repo/:repo_id/history/download", h.DownloadHistoricFile)

	req, _ := http.NewRequest("GET", "/repo/repo-123/history/download?p=/test.txt", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing obj_id, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Missing obj_id") {
		t.Error("response should mention missing obj_id")
	}
}

// TestDownloadHistoricFileInvalidPath tests that invalid path returns 400
func TestDownloadHistoricFileInvalidPath(t *testing.T) {
	h := &FileViewHandler{
		config: &config.Config{},
	}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", "test-user")
		c.Set("org_id", "test-org")
		c.Next()
	})
	r.GET("/repo/:repo_id/history/download", h.DownloadHistoricFile)

	// p=/ results in filepath.Base returning "/"
	req, _ := http.NewRequest("GET", "/repo/repo-123/history/download?obj_id=abc123&p=/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid path, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Invalid file path") {
		t.Error("response should mention invalid file path")
	}
}

// TestDownloadHistoricFileNoDatabase tests behavior when db is nil (panics are caught)
func TestDownloadHistoricFileDefaultPath(t *testing.T) {
	// When p is empty, it defaults to "/" which has invalid Base
	h := &FileViewHandler{
		config: &config.Config{},
	}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", "test-user")
		c.Set("org_id", "test-org")
		c.Next()
	})
	r.GET("/repo/:repo_id/history/download", h.DownloadHistoricFile)

	// No p parameter - defaults to "/" which has Base "/"
	req, _ := http.NewRequest("GET", "/repo/repo-123/history/download?obj_id=abc123", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for default path /, got %d", w.Code)
	}
}

// TestRegisterFileViewRoutesIncludesHistoryDownload tests the history download route is registered
func TestRegisterFileViewRoutesIncludesHistoryDownload(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{
			DevMode: true,
			DevTokens: []config.DevTokenEntry{
				{Token: "test-token", UserID: "user", OrgID: "org"},
			},
		},
		OnlyOffice: config.OnlyOfficeConfig{Enabled: false},
	}

	r := gin.New()
	r.Use(gin.Recovery()) // Recover from panics due to nil db
	devAuth := devTokenAuthMiddleware(cfg.Auth.DevTokens)
	RegisterFileViewRoutes(r, nil, cfg, nil, nil, &mockTokenCreator{}, "http://localhost:8082", devAuth)

	// Test that /repo/:repo_id/history/download route exists (missing obj_id → 400)
	req, _ := http.NewRequest("GET", "/repo/repo-123/history/download?p=/test.txt&token=test-token", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Missing obj_id should return 400, not 404
	if w.Code == http.StatusNotFound {
		t.Error("route /repo/:repo_id/history/download not registered")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing obj_id, got %d", w.Code)
	}

	// Test that /repo/:repo_id/raw/*filepath still works (will panic at DB, but not 404)
	req2, _ := http.NewRequest("GET", "/repo/repo-123/raw/test.txt?token=test-token", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code == http.StatusNotFound {
		t.Error("route /repo/:repo_id/raw/*filepath broken after adding history route")
	}
}

// TestDownloadHistoricFileNoStorageManager tests behavior when storageManager is nil
func TestDownloadHistoricFileNoStorageManager(t *testing.T) {
	h := &FileViewHandler{
		config:         &config.Config{},
		storageManager: nil, // No storage manager
	}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", "test-user")
		c.Set("org_id", "test-org")
		c.Next()
	})
	r.GET("/repo/:repo_id/history/download", h.DownloadHistoricFile)

	// Missing obj_id should still return 400 before reaching storage
	req, _ := http.NewRequest("GET", "/repo/repo-123/history/download", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing obj_id, got %d", w.Code)
	}
}

// TestOnlyOfficeEditorHTMLErrorHandling tests that error handling is present
func TestOnlyOfficeEditorHTMLErrorHandling(t *testing.T) {
	config := OnlyOfficeConfig{
		Document: OnlyOfficeDocument{
			FileType: "pptx",
			Key:      "key",
			Title:    "slides.pptx",
			URL:      "http://example.com/slides.pptx",
			Permissions: &OnlyOfficePermissions{
				Edit: true, Download: true, Print: true, Copy: true, FillForms: true,
			},
		},
		DocumentType: "slide",
		EditorConfig: OnlyOfficeEditorConfig{
			CallbackURL:   "http://example.com/callback",
			Mode:          "edit",
			User:          OnlyOfficeUser{ID: "1", Name: "User"},
			Customization: &OnlyOfficeCustomization{Forcesave: true},
		},
	}

	result := onlyOfficeEditorHTML("http://office/api.js", config, "slides.pptx")

	// Check for error handling in JavaScript
	errorHandling := []string{
		"catch (e)",
		"console.error",
		"Failed to load editor",
	}

	for _, elem := range errorHandling {
		if !strings.Contains(result, elem) {
			t.Errorf("onlyOfficeEditorHTML missing error handling: %s", elem)
		}
	}
}
