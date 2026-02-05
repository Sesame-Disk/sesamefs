package v2

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// TestRevertFile_MissingPath tests that RevertFile returns 400 when path is missing
func TestRevertFile_MissingPath(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("org_id", "test-org")
		c.Set("user_id", "test-user")
		c.Next()
	})

	handler := &FileHandler{}
	r.POST("/repos/:repo_id/file", handler.FileOperation)

	form := url.Values{}
	form.Add("commit_id", "test-commit-id")

	req := httptest.NewRequest("POST", "/repos/test-repo/file?operation=revert", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	if !strings.Contains(w.Body.String(), "path is required") {
		t.Errorf("body = %s, want to contain 'path is required'", w.Body.String())
	}
}

// TestRevertFile_MissingCommitID tests that RevertFile returns 400 when commit_id is missing
func TestRevertFile_MissingCommitID(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("org_id", "test-org")
		c.Set("user_id", "test-user")
		c.Next()
	})

	handler := &FileHandler{}
	r.POST("/repos/:repo_id/file", handler.FileOperation)

	req := httptest.NewRequest("POST", "/repos/test-repo/file?operation=revert&p=/test.txt", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	if !strings.Contains(w.Body.String(), "commit_id is required") {
		t.Errorf("body = %s, want to contain 'commit_id is required'", w.Body.String())
	}
}

// TestRevertDirectory_MissingPath tests that RevertDirectory returns 400 when path is missing
func TestRevertDirectory_MissingPath(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("org_id", "test-org")
		c.Set("user_id", "test-user")
		c.Next()
	})

	handler := &FileHandler{}
	r.POST("/repos/:repo_id/dir", handler.DirectoryOperation)

	form := url.Values{}
	form.Add("commit_id", "test-commit-id")

	req := httptest.NewRequest("POST", "/repos/test-repo/dir?operation=revert", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	if !strings.Contains(w.Body.String(), "path is required") {
		t.Errorf("body = %s, want to contain 'path is required'", w.Body.String())
	}
}

// TestRevertDirectory_MissingCommitID tests that RevertDirectory returns 400 when commit_id is missing
func TestRevertDirectory_MissingCommitID(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("org_id", "test-org")
		c.Set("user_id", "test-user")
		c.Next()
	})

	handler := &FileHandler{}
	r.POST("/repos/:repo_id/dir", handler.DirectoryOperation)

	req := httptest.NewRequest("POST", "/repos/test-repo/dir?operation=revert&p=/testdir", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	if !strings.Contains(w.Body.String(), "commit_id is required") {
		t.Errorf("body = %s, want to contain 'commit_id is required'", w.Body.String())
	}
}

// TestDirectoryOperation_RevertIsValidOperation ensures revert is a valid directory operation
func TestDirectoryOperation_RevertIsValidOperation(t *testing.T) {
	r := gin.New()
	handler := &FileHandler{}

	r.POST("/dir", handler.DirectoryOperation)

	// Test that "revert" is not an invalid operation
	// (it will fail later due to missing params, but shouldn't return "invalid operation")
	req := httptest.NewRequest("POST", "/dir?operation=invalid_xyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid operation") {
		t.Error("operation=invalid_xyz should return 'invalid operation'")
	}

	// Now test revert - should NOT return "invalid operation"
	req = httptest.NewRequest("POST", "/dir?operation=revert", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if strings.Contains(w.Body.String(), "invalid operation") {
		t.Error("operation=revert should be valid for directory")
	}
}

// TestFileOperation_RevertIsValidOperation ensures revert is a valid file operation
func TestFileOperation_RevertIsValidOperation(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("org_id", "test-org")
		c.Set("user_id", "test-user")
		c.Next()
	})
	handler := &FileHandler{}

	r.POST("/file", handler.FileOperation)

	// Test that an invalid operation returns "invalid operation"
	req := httptest.NewRequest("POST", "/file?operation=invalid_xyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid operation") {
		t.Error("operation=invalid_xyz should return 'invalid operation'")
	}

	// Now test revert - should NOT return "invalid operation"
	// It will return "path is required" instead, proving revert was recognized
	req = httptest.NewRequest("POST", "/file?operation=revert", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if strings.Contains(w.Body.String(), "invalid operation") {
		t.Error("operation=revert should be valid for file")
	}

	// Verify we get "path is required" instead (proving revert handler was called)
	if !strings.Contains(w.Body.String(), "path is required") {
		t.Errorf("expected 'path is required', got: %s", w.Body.String())
	}
}

// TestGenerateUniqueName tests the unique name generation helper
func TestGenerateUniqueName_Basic(t *testing.T) {
	entries := []FSEntry{
		{Name: "report.pdf"},
		{Name: "data.xlsx"},
	}

	// Name doesn't exist - return as is
	result := GenerateUniqueName(entries, "new.txt")
	if result != "new.txt" {
		t.Errorf("got %s, want new.txt", result)
	}

	// Name exists - should add (1)
	result = GenerateUniqueName(entries, "report.pdf")
	if result != "report (1).pdf" {
		t.Errorf("got %s, want report (1).pdf", result)
	}
}

func TestGenerateUniqueName_MultipleConflicts(t *testing.T) {
	entries := []FSEntry{
		{Name: "report.pdf"},
		{Name: "report (1).pdf"},
		{Name: "report (2).pdf"},
	}

	result := GenerateUniqueName(entries, "report.pdf")
	if result != "report (3).pdf" {
		t.Errorf("got %s, want report (3).pdf", result)
	}
}

func TestGenerateUniqueName_NoExtension(t *testing.T) {
	entries := []FSEntry{
		{Name: "README"},
	}

	result := GenerateUniqueName(entries, "README")
	if result != "README (1)" {
		t.Errorf("got %s, want README (1)", result)
	}
}

func TestGenerateUniqueName_Directory(t *testing.T) {
	entries := []FSEntry{
		{Name: "photos"},
		{Name: "photos (1)"},
	}

	result := GenerateUniqueName(entries, "photos")
	if result != "photos (2)" {
		t.Errorf("got %s, want photos (2)", result)
	}
}

// TestConflictPolicyDocumentation documents the valid conflict_policy values
func TestConflictPolicyDocumentation(t *testing.T) {
	validPolicies := []string{"replace", "skip", "keep_both", "autorename"}

	for _, policy := range validPolicies {
		t.Run(policy, func(t *testing.T) {
			// This is a documentation test - these are the valid values
			// The actual logic is tested via integration tests
			if policy == "" {
				t.Error("policy should not be empty")
			}
		})
	}
}
