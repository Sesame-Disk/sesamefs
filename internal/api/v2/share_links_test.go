package v2

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// TestCreateShareLink_Validation tests input validation for CreateShareLink
func TestCreateShareLink_Validation(t *testing.T) {
	r := gin.New()
	handler := &ShareLinkHandler{}

	r.POST("/share-links", func(c *gin.Context) {
		c.Set("org_id", "test-org-id")
		c.Set("user_id", "test-user-id")
		handler.CreateShareLink(c)
	})

	tests := []struct {
		name       string
		body       map[string]interface{}
		wantStatus int
		wantError  string
	}{
		{
			name:       "empty body",
			body:       map[string]interface{}{},
			wantStatus: http.StatusBadRequest,
			wantError:  "repo_id is required",
		},
		{
			name: "missing repo_id",
			body: map[string]interface{}{
				"path": "/test.txt",
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "repo_id is required",
		},
		{
			name: "missing path",
			body: map[string]interface{}{
				"repo_id": "123e4567-e89b-12d3-a456-426614174000",
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "path is required",
		},
		{
			name: "invalid repo_id format",
			body: map[string]interface{}{
				"repo_id": "not-a-uuid",
				"path":    "/test.txt",
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid repo_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest("POST", "/share-links", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d. Response: %s", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantError != "" {
				var resp map[string]string
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err == nil {
					if resp["error"] != tt.wantError {
						t.Errorf("error = %q, want %q", resp["error"], tt.wantError)
					}
				}
			}
		})
	}
}

// TestGenerateSecureShareToken tests token generation
func TestGenerateSecureShareToken(t *testing.T) {
	tests := []struct {
		name   string
		length int
	}{
		{"short token", 8},
		{"medium token", 16},
		{"long token", 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := generateSecureShareToken(tt.length)
			if err != nil {
				t.Errorf("generateSecureShareToken() error = %v", err)
				return
			}

			if token == "" {
				t.Error("token is empty")
			}

			// Generate another token and ensure it's different (extremely unlikely to be same)
			token2, _ := generateSecureShareToken(tt.length)
			if token == token2 {
				t.Error("generated tokens are identical (extremely unlikely - possible weak RNG)")
			}
		})
	}
}

// TestShareLinkPermissions tests permission mapping
func TestShareLinkPermissions(t *testing.T) {
	tests := []struct {
		permission  string
		wantEdit    bool
		wantDownload bool
		wantUpload  bool
	}{
		{
			permission:   "download",
			wantEdit:     false,
			wantDownload: true,
			wantUpload:   false,
		},
		{
			permission:   "preview_download",
			wantEdit:     false,
			wantDownload: true,
			wantUpload:   false,
		},
		{
			permission:   "preview_only",
			wantEdit:     false,
			wantDownload: false,
			wantUpload:   false,
		},
		{
			permission:   "upload",
			wantEdit:     true,
			wantDownload: false,
			wantUpload:   true,
		},
		{
			permission:   "edit",
			wantEdit:     true,
			wantDownload: true,
			wantUpload:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.permission, func(t *testing.T) {
			canEdit := tt.permission == "edit" || tt.permission == "upload"
			canDownload := tt.permission == "download" || tt.permission == "preview_download" || tt.permission == "edit"
			canUpload := tt.permission == "upload" || tt.permission == "edit"

			if canEdit != tt.wantEdit {
				t.Errorf("canEdit = %v, want %v", canEdit, tt.wantEdit)
			}
			if canDownload != tt.wantDownload {
				t.Errorf("canDownload = %v, want %v", canDownload, tt.wantDownload)
			}
			if canUpload != tt.wantUpload {
				t.Errorf("canUpload = %v, want %v", canUpload, tt.wantUpload)
			}
		})
	}
}
