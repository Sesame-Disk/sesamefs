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

// setupBatchRouter creates a test router with batch operation handler (nil DB)
func setupBatchRouter() (*gin.Engine, *BatchOperationHandler) {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(func(c *gin.Context) {
		c.Set("org_id", "00000000-0000-0000-0000-000000000001")
		c.Set("user_id", "00000000-0000-0000-0000-000000000001")
		c.Next()
	})

	h := &BatchOperationHandler{
		db:             nil,
		config:         nil,
		permMiddleware: nil, // nil = skip permission checks
		tasks:          &TaskStore{tasks: make(map[string]*AsyncTask)},
	}

	return r, h
}

func TestSyncBatchMove_InvalidJSON(t *testing.T) {
	r, h := setupBatchRouter()
	r.POST("/api/v2.1/repos/sync-batch-move-item/", h.SyncBatchMove)

	req, _ := http.NewRequest("POST", "/api/v2.1/repos/sync-batch-move-item/", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestSyncBatchMove_MissingSrcRepoID(t *testing.T) {
	r, h := setupBatchRouter()
	r.POST("/api/v2.1/repos/sync-batch-move-item/", h.SyncBatchMove)

	body := BatchRequest{
		SrcRepoID:    "",
		DstRepoID:    "dst-repo",
		SrcParentDir: "/",
		DstParentDir: "/",
		SrcDirents:   []string{"file.txt"},
	}
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "/api/v2.1/repos/sync-batch-move-item/", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "src_repo_id is required" {
		t.Errorf("error = %v, want 'src_repo_id is required'", resp["error"])
	}
}

func TestSyncBatchMove_MissingDstRepoID(t *testing.T) {
	r, h := setupBatchRouter()
	r.POST("/api/v2.1/repos/sync-batch-move-item/", h.SyncBatchMove)

	body := BatchRequest{
		SrcRepoID:    "src-repo",
		DstRepoID:    "",
		SrcParentDir: "/",
		DstParentDir: "/",
		SrcDirents:   []string{"file.txt"},
	}
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "/api/v2.1/repos/sync-batch-move-item/", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "dst_repo_id is required" {
		t.Errorf("error = %v, want 'dst_repo_id is required'", resp["error"])
	}
}

func TestSyncBatchMove_EmptyDirents(t *testing.T) {
	r, h := setupBatchRouter()
	r.POST("/api/v2.1/repos/sync-batch-move-item/", h.SyncBatchMove)

	body := BatchRequest{
		SrcRepoID:    "src-repo",
		DstRepoID:    "dst-repo",
		SrcParentDir: "/",
		DstParentDir: "/",
		SrcDirents:   []string{},
	}
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "/api/v2.1/repos/sync-batch-move-item/", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "src_dirents is required" {
		t.Errorf("error = %v, want 'src_dirents is required'", resp["error"])
	}
}

func TestSyncBatchCopy_InvalidJSON(t *testing.T) {
	r, h := setupBatchRouter()
	r.POST("/api/v2.1/repos/sync-batch-copy-item/", h.SyncBatchCopy)

	req, _ := http.NewRequest("POST", "/api/v2.1/repos/sync-batch-copy-item/", bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestGetTaskProgress_MissingTaskID(t *testing.T) {
	r, h := setupBatchRouter()
	r.GET("/api/v2.1/copy-move-task/", h.GetTaskProgress)

	req, _ := http.NewRequest("GET", "/api/v2.1/copy-move-task/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "task_id is required" {
		t.Errorf("error = %v, want 'task_id is required'", resp["error"])
	}
}

func TestGetTaskProgress_NotFound(t *testing.T) {
	r, h := setupBatchRouter()
	r.GET("/api/v2.1/copy-move-task/", h.GetTaskProgress)

	req, _ := http.NewRequest("GET", "/api/v2.1/copy-move-task/?task_id=nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestGetTaskProgress_ExistingTask(t *testing.T) {
	r, h := setupBatchRouter()
	r.GET("/api/v2.1/copy-move-task/", h.GetTaskProgress)

	// Add a task to the store
	h.tasks.mu.Lock()
	h.tasks.tasks["test-task-123"] = &AsyncTask{
		ID:     "test-task-123",
		Type:   "copy",
		Status: "done",
		Total:  5,
		Done:   4,
		Failed: 1,
	}
	h.tasks.mu.Unlock()

	req, _ := http.NewRequest("GET", "/api/v2.1/copy-move-task/?task_id=test-task-123", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["task_id"] != "test-task-123" {
		t.Errorf("task_id = %v, want test-task-123", resp["task_id"])
	}
	if resp["done"] != true {
		t.Errorf("done = %v, want true", resp["done"])
	}
	if resp["total"] != float64(5) {
		t.Errorf("total = %v, want 5", resp["total"])
	}
	if resp["successful"] != float64(4) {
		t.Errorf("successful = %v, want 4", resp["successful"])
	}
	if resp["failed"] != float64(1) {
		t.Errorf("failed = %v, want 1", resp["failed"])
	}
}

func TestGetTaskProgress_ProcessingTask(t *testing.T) {
	r, h := setupBatchRouter()
	r.GET("/api/v2.1/copy-move-task/", h.GetTaskProgress)

	h.tasks.mu.Lock()
	h.tasks.tasks["in-progress"] = &AsyncTask{
		ID:     "in-progress",
		Type:   "move",
		Status: "processing",
		Total:  10,
		Done:   3,
		Failed: 0,
	}
	h.tasks.mu.Unlock()

	req, _ := http.NewRequest("GET", "/api/v2.1/copy-move-task/?task_id=in-progress", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	// Processing task should have done=false
	if resp["done"] != false {
		t.Errorf("done = %v, want false (task still processing)", resp["done"])
	}
}

func TestBatchRequest_JSONBinding(t *testing.T) {
	tests := []struct {
		name       string
		body       map[string]interface{}
		wantStatus int
	}{
		{
			name: "valid request",
			body: map[string]interface{}{
				"src_repo_id":    "repo-1",
				"dst_repo_id":    "repo-2",
				"src_parent_dir": "/",
				"dst_parent_dir": "/target",
				"src_dirents":    []string{"file.txt", "photo.jpg"},
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "empty body",
			body:       map[string]interface{}{},
			wantStatus: http.StatusOK, // JSON binding succeeds with zero values
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.POST("/test", func(c *gin.Context) {
				var req BatchRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, req)
			})

			jsonBody, _ := json.Marshal(tt.body)
			req, _ := http.NewRequest("POST", "/test", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d, body = %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestRegisterBatchOperationRoutes(t *testing.T) {
	r := gin.New()
	rg := r.Group("/api/v2.1")
	RegisterBatchOperationRoutes(rg, nil, nil)

	routes := []struct {
		method string
		path   string
	}{
		{"POST", "/api/v2.1/repos/sync-batch-move-item/"},
		{"POST", "/api/v2.1/repos/sync-batch-copy-item/"},
		{"POST", "/api/v2.1/repos/async-batch-move-item/"},
		{"POST", "/api/v2.1/repos/async-batch-copy-item/"},
		{"GET", "/api/v2.1/copy-move-task/"},
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

func TestTaskStore_ConcurrentAccess(t *testing.T) {
	store := &TaskStore{tasks: make(map[string]*AsyncTask)}

	// Write
	store.mu.Lock()
	store.tasks["task-1"] = &AsyncTask{ID: "task-1", Status: "processing"}
	store.mu.Unlock()

	// Read
	store.mu.RLock()
	task, exists := store.tasks["task-1"]
	store.mu.RUnlock()

	if !exists {
		t.Fatal("task not found")
	}
	if task.ID != "task-1" {
		t.Errorf("task ID = %s, want task-1", task.ID)
	}
}

func TestAsyncTask_JSONFormat(t *testing.T) {
	task := AsyncTask{
		ID:           "task-abc",
		Type:         "copy",
		Status:       "done",
		Total:        10,
		Done:         8,
		Failed:       2,
		FailedReason: "permission denied",
	}

	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("failed to marshal AsyncTask: %v", err)
	}

	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)

	if decoded["task_id"] != "task-abc" {
		t.Errorf("task_id = %v, want task-abc", decoded["task_id"])
	}
	if decoded["type"] != "copy" {
		t.Errorf("type = %v, want copy", decoded["type"])
	}
	if decoded["status"] != "done" {
		t.Errorf("status = %v, want done", decoded["status"])
	}
	if decoded["failed_reason"] != "permission denied" {
		t.Errorf("failed_reason = %v, want 'permission denied'", decoded["failed_reason"])
	}
}
