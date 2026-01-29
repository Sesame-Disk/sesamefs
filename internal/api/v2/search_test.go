package v2

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestSearch_MissingQuery(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("org_id", "00000000-0000-0000-0000-000000000001")
		c.Set("user_id", "00000000-0000-0000-0000-000000000001")
		c.Next()
	})

	h := NewSearchHandler(nil)
	r.GET("/api/v2.1/search/", h.Search)

	req, _ := http.NewRequest("GET", "/api/v2.1/search/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "query parameter 'q' is required" {
		t.Errorf("error = %v, want 'query parameter q is required'", resp["error"])
	}
}

func TestSearch_MissingOrgID(t *testing.T) {
	r := gin.New()
	// No org_id set in context

	h := NewSearchHandler(nil)
	r.GET("/api/v2.1/search/", h.Search)

	req, _ := http.NewRequest("GET", "/api/v2.1/search/?q=test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("org_id", "00000000-0000-0000-0000-000000000001")
		c.Set("user_id", "00000000-0000-0000-0000-000000000001")
		c.Next()
	})

	h := NewSearchHandler(nil)
	r.GET("/api/v2.1/search/", h.Search)

	req, _ := http.NewRequest("GET", "/api/v2.1/search/?q=", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestSearchResult_JSONFormat(t *testing.T) {
	result := SearchResult{
		RepoID:   "abc-123",
		RepoName: "My Library",
		Name:     "test.txt",
		Path:     "/docs/test.txt",
		Type:     "file",
		Size:     1024,
		Mtime:    1234567890,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal SearchResult: %v", err)
	}

	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)

	if decoded["repo_id"] != "abc-123" {
		t.Errorf("repo_id = %v, want abc-123", decoded["repo_id"])
	}
	if decoded["type"] != "file" {
		t.Errorf("type = %v, want file", decoded["type"])
	}
	if decoded["name"] != "test.txt" {
		t.Errorf("name = %v, want test.txt", decoded["name"])
	}
}

func TestNewSearchHandler(t *testing.T) {
	h := NewSearchHandler(nil)
	if h == nil {
		t.Fatal("NewSearchHandler returned nil")
	}
	if h.db != nil {
		t.Error("expected nil db")
	}
}

func TestRegisterSearchRoutes(t *testing.T) {
	r := gin.New()
	rg := r.Group("/api/v2.1")
	RegisterSearchRoutes(rg, nil)

	// Verify routes are registered by making requests
	paths := []string{"/api/v2.1/search", "/api/v2.1/search/"}
	for _, path := range paths {
		req, _ := http.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		// Should not be 404 (route exists), will be 400 (missing q param)
		if w.Code == http.StatusNotFound {
			t.Errorf("route %s not registered", path)
		}
	}
}
