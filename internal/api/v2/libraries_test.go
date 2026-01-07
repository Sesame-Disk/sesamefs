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

// TestFormatSize tests the human-readable size formatter
func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		// Bytes
		{0, "0 B"},
		{1, "1 B"},
		{512, "512 B"},
		{1023, "1023 B"},

		// Kilobytes
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{2048, "2.0 KB"},
		{10240, "10.0 KB"},
		{1048575, "1024.0 KB"}, // Just under 1 MB

		// Megabytes
		{1048576, "1.0 MB"},
		{1572864, "1.5 MB"},
		{10485760, "10.0 MB"},
		{104857600, "100.0 MB"},
		{1073741823, "1024.0 MB"}, // Just under 1 GB

		// Gigabytes
		{1073741824, "1.0 GB"},
		{1610612736, "1.5 GB"},
		{10737418240, "10.0 GB"},
		{107374182400, "100.0 GB"},

		// Terabytes
		{1099511627776, "1.0 TB"},
		{1649267441664, "1.5 TB"},
		{10995116277760, "10.0 TB"},

		// Petabytes
		{1125899906842624, "1.0 PB"},

		// Exabytes
		{1152921504606846976, "1.0 EB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatSize(tt.bytes)
			if result != tt.expected {
				t.Errorf("formatSize(%d) = %q, want %q", tt.bytes, result, tt.expected)
			}
		})
	}
}

// TestFormatSizeEdgeCases tests edge cases for size formatting
func TestFormatSizeEdgeCases(t *testing.T) {
	// Test boundary between units
	t.Run("KB boundary", func(t *testing.T) {
		below := formatSize(1023)
		at := formatSize(1024)

		if below != "1023 B" {
			t.Errorf("1023 bytes should be '1023 B', got %q", below)
		}
		if at != "1.0 KB" {
			t.Errorf("1024 bytes should be '1.0 KB', got %q", at)
		}
	})

	t.Run("MB boundary", func(t *testing.T) {
		below := formatSize(1048575)
		at := formatSize(1048576)

		if below != "1024.0 KB" {
			t.Errorf("1048575 bytes should be '1024.0 KB', got %q", below)
		}
		if at != "1.0 MB" {
			t.Errorf("1048576 bytes should be '1.0 MB', got %q", at)
		}
	})

	t.Run("GB boundary", func(t *testing.T) {
		below := formatSize(1073741823)
		at := formatSize(1073741824)

		if below != "1024.0 MB" {
			t.Errorf("1073741823 bytes should be '1024.0 MB', got %q", below)
		}
		if at != "1.0 GB" {
			t.Errorf("1073741824 bytes should be '1.0 GB', got %q", at)
		}
	})
}

// TestFormatSizeRealistic tests realistic file sizes
func TestFormatSizeRealistic(t *testing.T) {
	tests := []struct {
		name     string
		bytes    int64
		expected string
	}{
		{"small text file", 1500, "1.5 KB"},
		{"word document", 52428, "51.2 KB"},
		{"photo", 3145728, "3.0 MB"},
		{"video clip", 157286400, "150.0 MB"},
		{"movie file", 4718592000, "4.4 GB"},
		{"backup archive", 53687091200, "50.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatSize(tt.bytes)
			if result != tt.expected {
				t.Errorf("formatSize(%d) = %q, want %q", tt.bytes, result, tt.expected)
			}
		})
	}
}

// setupLibraryTestRouter creates a test router for library tests
func setupLibraryTestRouter(orgID, userID string) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if orgID != "" {
			c.Set("org_id", orgID)
		}
		if userID != "" {
			c.Set("user_id", userID)
		}
		c.Next()
	})
	return r
}

// TestDeleteLibraryValidation tests DeleteLibrary input validation
// Note: Tests that require database access are skipped when db is nil
func TestDeleteLibraryValidation(t *testing.T) {
	tests := []struct {
		name       string
		orgID      string
		userID     string
		repoID     string
		wantStatus int
		wantError  string
	}{
		{
			name:       "missing org_id returns 400",
			orgID:      "",
			userID:     "00000000-0000-0000-0000-000000000001",
			repoID:     "00000000-0000-0000-0000-000000000001",
			wantStatus: http.StatusBadRequest,
			wantError:  "missing org_id",
		},
		{
			name:       "empty repo_id returns 400",
			orgID:      "00000000-0000-0000-0000-000000000001",
			userID:     "00000000-0000-0000-0000-000000000001",
			repoID:     "",
			wantStatus: http.StatusNotFound, // gin returns 404 for empty param
			wantError:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := setupLibraryTestRouter(tt.orgID, tt.userID)

			h := &LibraryHandler{
				db:     nil, // No database - tests validation only
				config: nil,
			}

			r.DELETE("/api2/repos/:repo_id", h.DeleteLibrary)

			req, _ := http.NewRequest("DELETE", "/api2/repos/"+tt.repoID, nil)
			req.Header.Set("Authorization", "Token test-token")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantError != "" {
				var resp map[string]interface{}
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err == nil {
					if errMsg, ok := resp["error"].(string); ok {
						if errMsg != tt.wantError {
							t.Errorf("error = %q, want %q", errMsg, tt.wantError)
						}
					}
				}
			}
		})
	}
}

// TestListLibrariesValidation tests ListLibraries input validation
func TestListLibrariesValidation(t *testing.T) {
	tests := []struct {
		name       string
		orgID      string
		wantStatus int
		wantError  string
	}{
		{
			name:       "missing org_id returns 400",
			orgID:      "",
			wantStatus: http.StatusBadRequest,
			wantError:  "missing org_id",
		},
		{
			name:       "invalid org_id returns 400",
			orgID:      "not-a-uuid",
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid org_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := setupLibraryTestRouter(tt.orgID, "")

			h := &LibraryHandler{
				db:     nil,
				config: nil,
			}

			r.GET("/api2/repos", h.ListLibraries)

			req, _ := http.NewRequest("GET", "/api2/repos", nil)
			req.Header.Set("Authorization", "Token test-token")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			var resp map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err == nil {
				if errMsg, ok := resp["error"].(string); ok && tt.wantError != "" {
					if errMsg != tt.wantError {
						t.Errorf("error = %q, want %q", errMsg, tt.wantError)
					}
				}
			}
		})
	}
}

// TestGetLibraryValidation tests GetLibrary input validation
func TestGetLibraryValidation(t *testing.T) {
	tests := []struct {
		name       string
		orgID      string
		repoID     string
		wantStatus int
		wantError  string
	}{
		{
			name:       "invalid repo_id returns 400",
			orgID:      "00000000-0000-0000-0000-000000000001",
			repoID:     "not-a-uuid",
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid repo_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := setupLibraryTestRouter(tt.orgID, "")

			h := &LibraryHandler{
				db:     nil,
				config: nil,
			}

			r.GET("/api2/repos/:repo_id", h.GetLibrary)

			req, _ := http.NewRequest("GET", "/api2/repos/"+tt.repoID, nil)
			req.Header.Set("Authorization", "Token test-token")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

// TestCreateLibraryRequestBinding tests request binding for CreateLibrary
func TestCreateLibraryRequestBinding(t *testing.T) {
	r := gin.New()
	gin.SetMode(gin.TestMode)

	var receivedReq CreateLibraryRequest
	r.POST("/test", func(c *gin.Context) {
		if err := c.ShouldBindJSON(&receivedReq); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, receivedReq)
	})

	tests := []struct {
		name       string
		body       map[string]interface{}
		wantStatus int
		wantName   string
	}{
		{
			name:       "valid library creation",
			body:       map[string]interface{}{"name": "My Library", "description": "Test library"},
			wantStatus: http.StatusOK,
			wantName:   "My Library",
		},
		{
			name:       "encrypted library",
			body:       map[string]interface{}{"name": "Encrypted", "encrypted": true, "password": "secret"},
			wantStatus: http.StatusOK,
			wantName:   "Encrypted",
		},
		{
			name:       "minimal request",
			body:       map[string]interface{}{"name": "Minimal"},
			wantStatus: http.StatusOK,
			wantName:   "Minimal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBody, _ := json.Marshal(tt.body)
			req, _ := http.NewRequest("POST", "/test", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK {
				var resp CreateLibraryRequest
				json.Unmarshal(w.Body.Bytes(), &resp)
				if resp.Name != tt.wantName {
					t.Errorf("name = %q, want %q", resp.Name, tt.wantName)
				}
			}
		})
	}
}

// TestRenameLibraryRequestBinding tests request binding for RenameLibrary
func TestRenameLibraryRequestBinding(t *testing.T) {
	r := gin.New()
	gin.SetMode(gin.TestMode)

	var receivedReq RenameLibraryRequest
	r.POST("/test", func(c *gin.Context) {
		if err := c.ShouldBind(&receivedReq); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, receivedReq)
	})

	tests := []struct {
		name       string
		body       map[string]interface{}
		wantStatus int
	}{
		{
			name:       "valid rename",
			body:       map[string]interface{}{"repo_name": "New Name"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "empty name binds ok - validation in handler",
			body:       map[string]interface{}{"repo_name": ""},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBody, _ := json.Marshal(tt.body)
			req, _ := http.NewRequest("POST", "/test", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

// TestGetRepoFolderShareInfo tests the share info stub endpoint
func TestGetRepoFolderShareInfo(t *testing.T) {
	r := setupLibraryTestRouter("00000000-0000-0000-0000-000000000001", "")

	h := &LibraryHandler{
		db:     nil,
		config: nil,
	}

	r.GET("/api/v2.1/repos/:repo_id/share-info", h.GetRepoFolderShareInfo)

	req, _ := http.NewRequest("GET", "/api/v2.1/repos/test-repo/share-info?path=/", nil)
	req.Header.Set("Authorization", "Token test-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Should return empty arrays
	if emails, ok := resp["shared_user_emails"].([]interface{}); !ok || len(emails) != 0 {
		t.Errorf("shared_user_emails should be empty array")
	}
	if groups, ok := resp["shared_group_ids"].([]interface{}); !ok || len(groups) != 0 {
		t.Errorf("shared_group_ids should be empty array")
	}
}

// TestV21LibraryStruct tests V21Library JSON serialization
func TestV21LibraryStruct(t *testing.T) {
	lib := V21Library{
		Type:              "mine",
		RepoID:            "12345678-1234-1234-1234-123456789012",
		RepoName:          "Test Library",
		OwnerEmail:        "user@example.com",
		OwnerName:         "user",
		LastModified:      "2026-01-01T00:00:00Z",
		Size:              1024,
		Encrypted:         false,
		Permission:        "rw",
		Starred:           false,
		Monitored:         false,
		Status:            "normal",
	}

	data, err := json.Marshal(lib)
	if err != nil {
		t.Fatalf("failed to marshal V21Library: %v", err)
	}

	// Verify JSON field names match Seafile v2.1 API format
	expectedFields := []string{
		`"type":"mine"`,
		`"repo_id":"12345678-1234-1234-1234-123456789012"`,
		`"repo_name":"Test Library"`,
		`"owner_email":"user@example.com"`,
		`"last_modified":"2026-01-01T00:00:00Z"`,
		`"permission":"rw"`,
	}

	jsonStr := string(data)
	for _, field := range expectedFields {
		if !bytes.Contains(data, []byte(field)) {
			t.Errorf("JSON missing field: %s\nGot: %s", field, jsonStr)
		}
	}
}
