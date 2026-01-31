package v2

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupDepartmentRouter() (*gin.Engine, *DepartmentHandler) {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(func(c *gin.Context) {
		c.Set("org_id", "00000000-0000-0000-0000-000000000001")
		c.Set("user_id", "00000000-0000-0000-0000-000000000001")
		c.Next()
	})

	h := NewDepartmentHandler(nil, nil)
	return r, h
}

func TestListDepartments_NilDB(t *testing.T) {
	r, h := setupDepartmentRouter()
	r.GET("/api/v2.1/admin/address-book/groups/", h.ListDepartments)

	req, _ := http.NewRequest("GET", "/api/v2.1/admin/address-book/groups/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatal("expected data field in response")
	}
	if len(data) != 0 {
		t.Errorf("expected empty data, got %d items", len(data))
	}
}

func TestListUserDepartments_NilDB(t *testing.T) {
	r, h := setupDepartmentRouter()
	r.GET("/api/v2.1/departments/", h.ListUserDepartments)

	req, _ := http.NewRequest("GET", "/api/v2.1/departments/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp []interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp) != 0 {
		t.Errorf("expected empty array, got %d items", len(resp))
	}
}

func TestCreateDepartment_MissingName(t *testing.T) {
	r, h := setupDepartmentRouter()
	r.POST("/api/v2.1/admin/address-book/groups/", h.CreateDepartment)

	req, _ := http.NewRequest("POST", "/api/v2.1/admin/address-book/groups/",
		strings.NewReader(`{"name":""}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCreateDepartment_InvalidParent(t *testing.T) {
	r, h := setupDepartmentRouter()
	r.POST("/api/v2.1/admin/address-book/groups/", h.CreateDepartment)

	req, _ := http.NewRequest("POST", "/api/v2.1/admin/address-book/groups/",
		strings.NewReader(`{"name":"Engineering","parent_group":"not-a-uuid"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestGetDepartment_InvalidGroupID(t *testing.T) {
	r, h := setupDepartmentRouter()
	r.GET("/api/v2.1/admin/address-book/groups/:group_id/", h.GetDepartment)

	req, _ := http.NewRequest("GET", "/api/v2.1/admin/address-book/groups/not-a-uuid/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestUpdateDepartment_MissingName(t *testing.T) {
	r, h := setupDepartmentRouter()
	r.PUT("/api/v2.1/admin/address-book/groups/:group_id/", h.UpdateDepartment)

	req, _ := http.NewRequest("PUT",
		"/api/v2.1/admin/address-book/groups/00000000-0000-0000-0000-000000000099/",
		strings.NewReader(`{"name":""}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestDeleteDepartment_InvalidGroupID(t *testing.T) {
	r, h := setupDepartmentRouter()
	r.DELETE("/api/v2.1/admin/address-book/groups/:group_id/", h.DeleteDepartment)

	req, _ := http.NewRequest("DELETE", "/api/v2.1/admin/address-book/groups/not-a-uuid/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestDepartmentResponse_JSON(t *testing.T) {
	resp := DepartmentResponse{
		ID:        "test-id",
		Name:      "Engineering",
		CreatedAt: "2026-01-31T00:00:00Z",
		Groups:    []DepartmentResponse{},
		Members:   []GroupMemberResponse{},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed map[string]interface{}
	json.Unmarshal(data, &parsed)

	if parsed["id"] != "test-id" {
		t.Errorf("id = %v, want test-id", parsed["id"])
	}
	if parsed["name"] != "Engineering" {
		t.Errorf("name = %v, want Engineering", parsed["name"])
	}
}

// TestGetBrowserURL tests the URL generation helper
func TestGetBrowserURL(t *testing.T) {
	tests := []struct {
		name        string
		host        string
		xProto      string
		fallbackURL string
		want        string
	}{
		{
			name:        "with X-Forwarded-Proto",
			host:        "localhost:3000",
			xProto:      "https",
			fallbackURL: "http://localhost:8082",
			want:        "https://localhost:3000",
		},
		{
			name:        "without proxy headers",
			host:        "localhost:3000",
			xProto:      "",
			fallbackURL: "http://localhost:8082",
			want:        "http://localhost:3000",
		},
		{
			name:        "no host uses fallback",
			host:        "",
			xProto:      "",
			fallbackURL: "http://localhost:8082",
			want:        "http://localhost:8082",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request, _ = http.NewRequest("GET", "/test", nil)
			if tt.host != "" {
				c.Request.Host = tt.host
			}
			if tt.xProto != "" {
				c.Request.Header.Set("X-Forwarded-Proto", tt.xProto)
			}

			got := getBrowserURL(c, tt.fallbackURL)
			if got != tt.want {
				t.Errorf("getBrowserURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
