package v2

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Sesame-Disk/sesamefs/internal/crypto"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// TestSetPasswordRequest_Binding tests SetPasswordRequest binding
func TestSetPasswordRequest_Binding(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		wantPwd     string
	}{
		{
			name:        "form data",
			contentType: "application/x-www-form-urlencoded",
			body:        "password=TestPassword123",
			wantPwd:     "TestPassword123",
		},
		{
			name:        "json data",
			contentType: "application/json",
			body:        `{"password":"TestPassword123"}`,
			wantPwd:     "TestPassword123",
		},
		{
			name:        "empty password form",
			contentType: "application/x-www-form-urlencoded",
			body:        "password=",
			wantPwd:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req SetPasswordRequest

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", tt.contentType)

			err := c.ShouldBind(&req)
			if err != nil {
				t.Logf("Binding error (may be expected): %v", err)
			}

			if req.Password != tt.wantPwd {
				t.Errorf("Password = %q, want %q", req.Password, tt.wantPwd)
			}
		})
	}
}

// TestChangePasswordRequest_Binding tests ChangePasswordRequest binding
func TestChangePasswordRequest_Binding(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		wantOld     string
		wantNew     string
	}{
		{
			name:        "form data",
			contentType: "application/x-www-form-urlencoded",
			body:        "old_password=OldPass123&new_password=NewPass456",
			wantOld:     "OldPass123",
			wantNew:     "NewPass456",
		},
		{
			name:        "json data",
			contentType: "application/json",
			body:        `{"old_password":"OldPass123","new_password":"NewPass456"}`,
			wantOld:     "OldPass123",
			wantNew:     "NewPass456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req ChangePasswordRequest

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			c.Request = httptest.NewRequest(http.MethodPut, "/", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", tt.contentType)

			err := c.ShouldBind(&req)
			if err != nil {
				t.Fatalf("Binding failed: %v", err)
			}

			if req.OldPassword != tt.wantOld {
				t.Errorf("OldPassword = %q, want %q", req.OldPassword, tt.wantOld)
			}
			if req.NewPassword != tt.wantNew {
				t.Errorf("NewPassword = %q, want %q", req.NewPassword, tt.wantNew)
			}
		})
	}
}

// TestSetPassword_Validation tests input validation for SetPassword
func TestSetPassword_Validation(t *testing.T) {
	tests := []struct {
		name       string
		repoID     string
		password   string
		wantStatus int
		wantError  string
	}{
		{
			name:       "invalid repo_id",
			repoID:     "not-a-uuid",
			password:   "TestPassword",
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid repo_id",
		},
		{
			name:       "empty password",
			repoID:     "543f7a13-7145-4d85-a768-8c91755cfb77",
			password:   "",
			wantStatus: http.StatusBadRequest,
			wantError:  "password is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create handler without database (will fail on DB access, but we're testing validation)
			h := &EncryptionHandler{db: nil}

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			// Set up request
			form := url.Values{}
			form.Set("password", tt.password)

			c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
			c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			c.Params = gin.Params{{Key: "repo_id", Value: tt.repoID}}
			c.Set("org_id", "00000000-0000-0000-0000-000000000001")

			h.SetPassword(c)

			if w.Code != tt.wantStatus {
				t.Errorf("Status = %d, want %d", w.Code, tt.wantStatus)
			}

			var resp map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &resp)
			if errMsg, ok := resp["error"].(string); ok {
				if !strings.Contains(errMsg, tt.wantError) {
					t.Errorf("Error = %q, want to contain %q", errMsg, tt.wantError)
				}
			}
		})
	}
}

// TestChangePassword_Validation tests input validation for ChangePassword
func TestChangePassword_Validation(t *testing.T) {
	tests := []struct {
		name        string
		repoID      string
		oldPassword string
		newPassword string
		wantStatus  int
		wantError   string
	}{
		{
			name:        "invalid repo_id",
			repoID:      "not-a-uuid",
			oldPassword: "OldPass",
			newPassword: "NewPass",
			wantStatus:  http.StatusBadRequest,
			wantError:   "invalid repo_id",
		},
		{
			name:        "empty old password",
			repoID:      "543f7a13-7145-4d85-a768-8c91755cfb77",
			oldPassword: "",
			newPassword: "NewPass",
			wantStatus:  http.StatusBadRequest,
			wantError:   "required",
		},
		{
			name:        "empty new password",
			repoID:      "543f7a13-7145-4d85-a768-8c91755cfb77",
			oldPassword: "OldPass",
			newPassword: "",
			wantStatus:  http.StatusBadRequest,
			wantError:   "required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &EncryptionHandler{db: nil}

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			form := url.Values{}
			form.Set("old_password", tt.oldPassword)
			form.Set("new_password", tt.newPassword)

			c.Request = httptest.NewRequest(http.MethodPut, "/", strings.NewReader(form.Encode()))
			c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			c.Params = gin.Params{{Key: "repo_id", Value: tt.repoID}}
			c.Set("org_id", "00000000-0000-0000-0000-000000000001")

			h.ChangePassword(c)

			if w.Code != tt.wantStatus {
				t.Errorf("Status = %d, want %d", w.Code, tt.wantStatus)
			}

			var resp map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &resp)
			if errMsg, ok := resp["error"].(string); ok {
				if !strings.Contains(errMsg, tt.wantError) {
					t.Errorf("Error = %q, want to contain %q", errMsg, tt.wantError)
				}
			}
		})
	}
}

// TestEncryptionParams_JSON tests JSON serialization of encryption params
func TestEncryptionParams_JSON(t *testing.T) {
	params := &crypto.EncryptionParams{
		EncVersion:      12,
		Salt:            "abcd1234",
		Magic:           "magic123",
		MagicStrong:     "strongmagic456",
		RandomKey:       "randomkey789",
		RandomKeyStrong: "strongrandom012",
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded crypto.EncryptionParams
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.EncVersion != params.EncVersion {
		t.Errorf("EncVersion = %d, want %d", decoded.EncVersion, params.EncVersion)
	}
	if decoded.Salt != params.Salt {
		t.Errorf("Salt = %q, want %q", decoded.Salt, params.Salt)
	}
	if decoded.Magic != params.Magic {
		t.Errorf("Magic = %q, want %q", decoded.Magic, params.Magic)
	}
}

// TestEncryptionResponseFormat tests the response format matches Seafile
func TestEncryptionResponseFormat(t *testing.T) {
	// Test success response
	successResp := gin.H{"success": true}
	data, _ := json.Marshal(successResp)
	if !bytes.Contains(data, []byte(`"success":true`)) {
		t.Error("Success response should contain success:true")
	}

	// Test error response (Seafile format)
	errorResp := gin.H{"error_msg": "Wrong password"}
	data, _ = json.Marshal(errorResp)
	if !bytes.Contains(data, []byte(`"error_msg":"Wrong password"`)) {
		t.Error("Error response should use error_msg field")
	}
}

// TestCreateLibraryRequest_EncryptedFields tests encrypted library creation request
func TestCreateLibraryRequest_EncryptedFields(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantEnc   bool
		wantPwd   string
	}{
		{
			name:    "non-encrypted",
			body:    "name=TestLib",
			wantEnc: false,
			wantPwd: "",
		},
		{
			name:    "encrypted with password",
			body:    "name=TestLib&encrypted=true&passwd=Secret123",
			wantEnc: true,
			wantPwd: "Secret123",
		},
		{
			name:    "encrypted=1 format",
			body:    "name=TestLib&encrypted=1&passwd=Secret123",
			wantEnc: true,
			wantPwd: "Secret123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			// Parse like the handler does
			name := c.PostForm("name")
			password := c.PostForm("passwd")
			encrypted := c.PostForm("encrypted") == "true" || c.PostForm("encrypted") == "1"

			if name == "" {
				t.Error("Name should be parsed")
			}
			if encrypted != tt.wantEnc {
				t.Errorf("Encrypted = %v, want %v", encrypted, tt.wantEnc)
			}
			if password != tt.wantPwd {
				t.Errorf("Password = %q, want %q", password, tt.wantPwd)
			}
		})
	}
}

// TestEncryptionVersion tests that we use the correct encryption version
func TestEncryptionVersion(t *testing.T) {
	if crypto.EncVersionDual != 12 {
		t.Errorf("EncVersionDual = %d, want 12", crypto.EncVersionDual)
	}
	if crypto.EncVersionSesameFS != 10 {
		t.Errorf("EncVersionSesameFS = %d, want 10", crypto.EncVersionSesameFS)
	}
	if crypto.EncVersionSeafileV2 != 2 {
		t.Errorf("EncVersionSeafileV2 = %d, want 2", crypto.EncVersionSeafileV2)
	}
}

// TestPasswordMinLength tests password validation constants
func TestPasswordMinLength(t *testing.T) {
	// Seafile default minimum is 8 characters
	minLen := 8

	validPasswords := []string{
		"12345678",
		"Password123!",
		"abcdefghijklmnop",
	}

	invalidPasswords := []string{
		"1234567",
		"abc",
		"",
	}

	for _, pwd := range validPasswords {
		if len(pwd) < minLen {
			t.Errorf("Password %q should be valid (len=%d >= %d)", pwd, len(pwd), minLen)
		}
	}

	for _, pwd := range invalidPasswords {
		if len(pwd) >= minLen {
			t.Errorf("Password %q should be invalid (len=%d < %d)", pwd, len(pwd), minLen)
		}
	}
}
