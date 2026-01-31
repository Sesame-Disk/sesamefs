package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type mockDB struct {
	err error
}

func (m *mockDB) Ping(ctx context.Context) error { return m.err }

type mockStorage struct {
	err error
}

func (m *mockStorage) HeadBucket(ctx context.Context) error { return m.err }

func setupRouter(checker *Checker) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/health", checker.HandleLiveness)
	r.GET("/ready", checker.HandleReadiness)
	return r
}

func TestLiveness(t *testing.T) {
	checker := NewChecker(nil, nil, 3*time.Second, "test-1.0")
	r := setupRouter(checker)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["status"] != "healthy" {
		t.Fatalf("expected status healthy, got %v", body["status"])
	}
	if body["version"] != "test-1.0" {
		t.Fatalf("expected version test-1.0, got %v", body["version"])
	}
}

func TestReadinessAllOK(t *testing.T) {
	checker := NewChecker(&mockDB{}, &mockStorage{}, 3*time.Second, "test")
	r := setupRouter(checker)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/ready", nil)
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["status"] != "ready" {
		t.Fatalf("expected status ready, got %v", body["status"])
	}
}

func TestReadinessDBDown(t *testing.T) {
	checker := NewChecker(&mockDB{err: errors.New("connection refused")}, &mockStorage{}, 3*time.Second, "test")
	r := setupRouter(checker)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/ready", nil)
	r.ServeHTTP(w, req)

	if w.Code != 503 {
		t.Fatalf("expected 503, got %d", w.Code)
	}

	var body map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["status"] != "not_ready" {
		t.Fatalf("expected status not_ready, got %v", body["status"])
	}
}

func TestReadinessStorageDown(t *testing.T) {
	checker := NewChecker(&mockDB{}, &mockStorage{err: errors.New("bucket not found")}, 3*time.Second, "test")
	r := setupRouter(checker)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/ready", nil)
	r.ServeHTTP(w, req)

	if w.Code != 503 {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestReadinessNoDeps(t *testing.T) {
	// When no dependencies are configured, readiness should pass
	checker := NewChecker(nil, nil, 3*time.Second, "test")
	r := setupRouter(checker)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/ready", nil)
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200 when no deps, got %d", w.Code)
	}
}
