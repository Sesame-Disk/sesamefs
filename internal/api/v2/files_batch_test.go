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

// TestBatchDeleteItems_Validation tests input validation for BatchDeleteItems
func TestBatchDeleteItems_Validation(t *testing.T) {
	r := gin.New()
	handler := &FileHandler{}

	r.DELETE("/batch-delete", handler.BatchDeleteItems)

	tests := []struct {
		name       string
		body       interface{}
		wantStatus int
		wantError  string
	}{
		{
			name:       "empty body",
			body:       nil,
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid request body",
		},
		{
			name:       "missing repo_id",
			body:       BatchDeleteRequest{RepoID: "", ParentDir: "/", Dirents: []string{"file.txt"}},
			wantStatus: http.StatusBadRequest,
			wantError:  "repo_id is required",
		},
		{
			name:       "empty dirents",
			body:       BatchDeleteRequest{RepoID: "test-repo", ParentDir: "/", Dirents: []string{}},
			wantStatus: http.StatusBadRequest,
			wantError:  "dirents is required",
		},
		{
			name:       "nil dirents",
			body:       map[string]interface{}{"repo_id": "test-repo", "parent_dir": "/"},
			wantStatus: http.StatusBadRequest,
			wantError:  "dirents is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			if tt.body != nil {
				body, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest("DELETE", "/batch-delete", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			var resp map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err == nil {
				if resp["error"] != tt.wantError {
					t.Errorf("error = %q, want %q", resp["error"], tt.wantError)
				}
			}
		})
	}
}

// TestBatchDeleteRequest_Binding tests JSON binding for BatchDeleteRequest
func TestBatchDeleteRequest_Binding(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		want    BatchDeleteRequest
		wantErr bool
	}{
		{
			name: "valid request",
			json: `{"repo_id":"abc-123","parent_dir":"/folder","dirents":["file1.txt","file2.txt"]}`,
			want: BatchDeleteRequest{
				RepoID:    "abc-123",
				ParentDir: "/folder",
				Dirents:   []string{"file1.txt", "file2.txt"},
			},
			wantErr: false,
		},
		{
			name: "missing parent_dir defaults to empty",
			json: `{"repo_id":"abc-123","dirents":["file.txt"]}`,
			want: BatchDeleteRequest{
				RepoID:    "abc-123",
				ParentDir: "",
				Dirents:   []string{"file.txt"},
			},
			wantErr: false,
		},
		{
			name:    "invalid json",
			json:    `{invalid}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req BatchDeleteRequest
			err := json.Unmarshal([]byte(tt.json), &req)

			if (err != nil) != tt.wantErr {
				t.Errorf("Unmarshal error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if req.RepoID != tt.want.RepoID {
					t.Errorf("RepoID = %q, want %q", req.RepoID, tt.want.RepoID)
				}
				if req.ParentDir != tt.want.ParentDir {
					t.Errorf("ParentDir = %q, want %q", req.ParentDir, tt.want.ParentDir)
				}
				if len(req.Dirents) != len(tt.want.Dirents) {
					t.Errorf("Dirents len = %d, want %d", len(req.Dirents), len(tt.want.Dirents))
				}
			}
		})
	}
}

// TestGeneratePathID_Batch tests the generatePathID function with various inputs
func TestGeneratePathID_Batch(t *testing.T) {
	tests := []struct {
		orgID  string
		repoID string
		path   string
	}{
		{"org-1", "abc-123", "/"},
		{"org-1", "abc-123", "/folder"},
		{"org-1", "abc-123", "/folder/subfolder"},
		{"org-2", "def-456", "/"},
	}

	for _, tt := range tests {
		t.Run(tt.orgID+"/"+tt.repoID+tt.path, func(t *testing.T) {
			id := generatePathID(tt.orgID, tt.repoID, tt.path)

			// Should be non-empty
			if id == "" {
				t.Error("generatePathID returned empty string")
			}

			// Should be deterministic
			id2 := generatePathID(tt.orgID, tt.repoID, tt.path)
			if id != id2 {
				t.Errorf("generatePathID not deterministic: %q != %q", id, id2)
			}

			// Different inputs should produce different outputs
			different := generatePathID(tt.orgID+"x", tt.repoID, tt.path)
			if id == different {
				t.Error("Different inputs produced same path ID")
			}
		})
	}
}

// TestDirentStruct_JSONMarshal tests the Dirent JSON marshaling
func TestDirentStruct_JSONMarshal(t *testing.T) {
	d := Dirent{
		Type:       "file",
		ID:         "abc123",
		Name:       "test.txt",
		MTime:      1234567890,
		Permission: "rw",
		ParentDir:  "/",
		Size:       1024,
		IsLocked:   true,
		LockOwner:  "user@example.com",
		LockTime:   1234567890,
	}

	data, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Check field names match Seafile API
	expectedFields := []string{"type", "id", "name", "mtime", "permission", "parent_dir", "size", "is_locked", "lock_owner", "lock_time"}
	for _, field := range expectedFields {
		if _, ok := m[field]; !ok {
			t.Errorf("Missing field %q in JSON output", field)
		}
	}
}

// TestDirentStruct_AllFields tests all Dirent fields serialize correctly
func TestDirentStruct_AllFields(t *testing.T) {
	d := Dirent{
		ID:              "abc123",
		Name:            "document.pdf",
		Type:            "file",
		Size:            2048,
		MTime:           1234567890,
		Permission:      "rw",
		ParentDir:       "/documents",
		Starred:         true,
		IsLocked:        true,
		LockOwner:       "admin@example.com",
		LockOwnerName:   "Admin User",
		LockTime:        1234567800,
		LockedByMe:      false,
		ModifierEmail:   "user@example.com",
		ModifierName:    "Test User",
	}

	data, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// Verify it can be unmarshaled back
	var d2 Dirent
	if err := json.Unmarshal(data, &d2); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if d.ID != d2.ID {
		t.Errorf("ID mismatch: %q != %q", d.ID, d2.ID)
	}
	if d.Name != d2.Name {
		t.Errorf("Name mismatch: %q != %q", d.Name, d2.Name)
	}
	if d.IsLocked != d2.IsLocked {
		t.Errorf("IsLocked mismatch: %v != %v", d.IsLocked, d2.IsLocked)
	}
}

// TestCreateDirectoryRequest_RootPath tests that creating root directory is rejected
func TestCreateDirectoryRequest_RootPath(t *testing.T) {
	r := gin.New()
	handler := &FileHandler{}

	r.POST("/dir/:repo_id", func(c *gin.Context) {
		c.Set("org_id", "test-org")
		c.Set("user_id", "test-user")
		handler.CreateDirectory(c)
	})

	// Creating root "/" should be rejected before DB access
	req := httptest.NewRequest("POST", "/dir/test-repo?p=/", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d for root path", w.Code, http.StatusBadRequest)
	}
}

// TestMoveFileRequest_EmptyBody tests MoveFile with empty body
func TestMoveFileRequest_EmptyBody(t *testing.T) {
	r := gin.New()
	handler := &FileHandler{}

	r.POST("/move", func(c *gin.Context) {
		c.Set("org_id", "test-org")
		c.Set("user_id", "test-user")
		handler.MoveFile(c)
	})

	// Empty body should fail validation before DB access
	req := httptest.NewRequest("POST", "/move", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d for empty body", w.Code, http.StatusBadRequest)
	}
}

// TestCopyFileRequest_EmptyBody tests CopyFile with empty body
func TestCopyFileRequest_EmptyBody(t *testing.T) {
	r := gin.New()
	handler := &FileHandler{}

	r.POST("/copy", func(c *gin.Context) {
		c.Set("org_id", "test-org")
		c.Set("user_id", "test-user")
		handler.CopyFile(c)
	})

	// Empty body should fail validation before DB access
	req := httptest.NewRequest("POST", "/copy", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d for empty body", w.Code, http.StatusBadRequest)
	}
}

// TestMoveFileRequest_Binding tests JSON binding for move request
func TestMoveFileRequest_Binding(t *testing.T) {
	type MoveRequest struct {
		SrcRepoID   string `json:"src_repo_id"`
		SrcDir      string `json:"src_dir"`
		SrcFilename string `json:"src_filename"`
		DstRepoID   string `json:"dst_repo_id"`
		DstDir      string `json:"dst_dir"`
	}

	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{
			name:    "valid request",
			json:    `{"src_repo_id":"repo1","src_dir":"/","src_filename":"test.txt","dst_repo_id":"repo2","dst_dir":"/dest"}`,
			wantErr: false,
		},
		{
			name:    "invalid json",
			json:    `{invalid}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req MoveRequest
			err := json.Unmarshal([]byte(tt.json), &req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Unmarshal error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
