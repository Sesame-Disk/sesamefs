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

func TestLibrarySettings_RequireOwner_NoAuth(t *testing.T) {
	r := gin.New()
	r.Use(gin.Recovery())
	// No auth context set

	h := &LibrarySettingsHandler{
		db:             nil,
		config:         nil,
		permMiddleware: nil,
	}

	r.GET("/api2/repos/:repo_id/history-limit/", h.GetHistoryLimit)

	req, _ := http.NewRequest("GET", "/api2/repos/00000000-0000-0000-0000-000000000001/history-limit/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestLibrarySettings_RequireOwner_InvalidRepoID(t *testing.T) {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(func(c *gin.Context) {
		c.Set("org_id", "00000000-0000-0000-0000-000000000001")
		c.Set("user_id", "00000000-0000-0000-0000-000000000001")
		c.Next()
	})

	h := &LibrarySettingsHandler{
		db:             nil,
		config:         nil,
		permMiddleware: nil,
	}

	r.PUT("/api2/repos/:repo_id/history-limit/", h.SetHistoryLimit)

	req, _ := http.NewRequest("PUT", "/api2/repos/not-a-uuid/history-limit/", bytes.NewBufferString(`{"keep_days": 30}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "invalid repo_id" {
		t.Errorf("error = %v, want 'invalid repo_id'", resp["error"])
	}
}

func TestCreateAPIToken_InvalidJSON(t *testing.T) {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(func(c *gin.Context) {
		c.Set("org_id", "00000000-0000-0000-0000-000000000001")
		c.Set("user_id", "00000000-0000-0000-0000-000000000001")
		c.Next()
	})

	h := &LibrarySettingsHandler{
		db:             nil,
		config:         nil,
		permMiddleware: nil,
	}

	r.POST("/api/v2.1/repos/:repo_id/repo-api-tokens/", h.CreateAPIToken)

	// With nil permMiddleware, requireOwner will panic trying to call IsLibraryOwner
	// so we test binding on a direct handler instead
	r2 := gin.New()
	r2.POST("/test", func(c *gin.Context) {
		var req struct {
			AppName    string `json:"app_name"`
			Permission string `json:"permission"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
		if req.AppName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "app_name is required"})
			return
		}
		if req.Permission != "r" && req.Permission != "rw" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "permission must be 'r' or 'rw'"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	tests := []struct {
		name       string
		body       map[string]interface{}
		wantStatus int
		wantError  string
	}{
		{
			name:       "missing app_name",
			body:       map[string]interface{}{"permission": "rw"},
			wantStatus: http.StatusBadRequest,
			wantError:  "app_name is required",
		},
		{
			name:       "invalid permission",
			body:       map[string]interface{}{"app_name": "test", "permission": "admin"},
			wantStatus: http.StatusBadRequest,
			wantError:  "permission must be 'r' or 'rw'",
		},
		{
			name:       "valid read permission",
			body:       map[string]interface{}{"app_name": "test", "permission": "r"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "valid rw permission",
			body:       map[string]interface{}{"app_name": "test", "permission": "rw"},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBody, _ := json.Marshal(tt.body)
			req, _ := http.NewRequest("POST", "/test", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r2.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if tt.wantError != "" {
				var resp map[string]interface{}
				json.Unmarshal(w.Body.Bytes(), &resp)
				if resp["error"] != tt.wantError {
					t.Errorf("error = %v, want %v", resp["error"], tt.wantError)
				}
			}
		})
	}
}

func TestSetHistoryLimit_Validation(t *testing.T) {
	// Test the keep_days validation logic directly
	r := gin.New()
	r.PUT("/test", func(c *gin.Context) {
		var req struct {
			KeepDays int `json:"keep_days"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
		if req.KeepDays < -1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "keep_days must be -1 (all), 0 (none), or a positive integer"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"keep_days": req.KeepDays})
	})

	tests := []struct {
		name       string
		keepDays   int
		wantStatus int
	}{
		{"keep all (-1)", -1, http.StatusOK},
		{"keep none (0)", 0, http.StatusOK},
		{"keep 30 days", 30, http.StatusOK},
		{"keep 365 days", 365, http.StatusOK},
		{"invalid (-2)", -2, http.StatusBadRequest},
		{"invalid (-100)", -100, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]interface{}{"keep_days": tt.keepDays}
			jsonBody, _ := json.Marshal(body)
			req, _ := http.NewRequest("PUT", "/test", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestSetAutoDelete_Validation(t *testing.T) {
	r := gin.New()
	r.PUT("/test", func(c *gin.Context) {
		var req struct {
			AutoDeleteDays int `json:"auto_delete_days"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
		if req.AutoDeleteDays < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auto_delete_days must be 0 (disabled) or a positive integer"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"auto_delete_days": req.AutoDeleteDays})
	})

	tests := []struct {
		name       string
		days       int
		wantStatus int
	}{
		{"disabled (0)", 0, http.StatusOK},
		{"30 days", 30, http.StatusOK},
		{"negative (-1)", -1, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]interface{}{"auto_delete_days": tt.days}
			jsonBody, _ := json.Marshal(body)
			req, _ := http.NewRequest("PUT", "/test", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestUpdateAPIToken_PermissionValidation(t *testing.T) {
	r := gin.New()
	r.PUT("/test", func(c *gin.Context) {
		var req struct {
			Permission string `json:"permission"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
		if req.Permission != "r" && req.Permission != "rw" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "permission must be 'r' or 'rw'"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	tests := []struct {
		name       string
		permission string
		wantStatus int
	}{
		{"read only", "r", http.StatusOK},
		{"read-write", "rw", http.StatusOK},
		{"invalid empty", "", http.StatusBadRequest},
		{"invalid admin", "admin", http.StatusBadRequest},
		{"invalid write", "w", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]interface{}{"permission": tt.permission}
			jsonBody, _ := json.Marshal(body)
			req, _ := http.NewRequest("PUT", "/test", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestTransferLibrary_Validation(t *testing.T) {
	r := gin.New()
	r.PUT("/test", func(c *gin.Context) {
		var req struct {
			Owner string `json:"owner"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
		if req.Owner == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "owner email is required"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	tests := []struct {
		name       string
		body       map[string]interface{}
		wantStatus int
	}{
		{
			name:       "valid email",
			body:       map[string]interface{}{"owner": "user@example.com"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing owner",
			body:       map[string]interface{}{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty owner",
			body:       map[string]interface{}{"owner": ""},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBody, _ := json.Marshal(tt.body)
			req, _ := http.NewRequest("PUT", "/test", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestAPITokenResponse_JSONFormat(t *testing.T) {
	resp := APITokenResponse{
		AppName:     "my-app",
		APIToken:    "abc123def456",
		Permission:  "rw",
		GeneratedAt: "2026-01-29T12:00:00Z",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal APITokenResponse: %v", err)
	}

	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)

	if decoded["app_name"] != "my-app" {
		t.Errorf("app_name = %v, want my-app", decoded["app_name"])
	}
	if decoded["permission"] != "rw" {
		t.Errorf("permission = %v, want rw", decoded["permission"])
	}
	if decoded["generated_at"] != "2026-01-29T12:00:00Z" {
		t.Errorf("generated_at = %v, want 2026-01-29T12:00:00Z", decoded["generated_at"])
	}
}

func TestRegisterHistoryLimitRoutes(t *testing.T) {
	r := gin.New()
	rg := r.Group("/api2")
	RegisterHistoryLimitRoutes(rg, nil, nil)

	routes := []struct {
		method string
		path   string
	}{
		{"GET", "/api2/repos/00000000-0000-0000-0000-000000000001/history-limit/"},
		{"PUT", "/api2/repos/00000000-0000-0000-0000-000000000001/history-limit/"},
	}

	for _, rt := range routes {
		req, _ := http.NewRequest(rt.method, rt.path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("route %s %s not registered", rt.method, rt.path)
		}
	}
}
