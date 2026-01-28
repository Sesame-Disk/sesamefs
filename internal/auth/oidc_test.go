package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/config"
)

// mockOIDCProvider creates a mock OIDC provider for testing
func mockOIDCProvider(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()

	// Discovery endpoint
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		discovery := OIDCDiscovery{
			Issuer:                "http://localhost",
			AuthorizationEndpoint: "http://localhost/authorize",
			TokenEndpoint:         "http://localhost/token",
			UserInfoEndpoint:      "http://localhost/userinfo",
			JwksURI:               "http://localhost/.well-known/jwks.json",
			EndSessionEndpoint:    "http://localhost/logout",
			ScopesSupported:       []string{"openid", "profile", "email"},
			ClaimsSupported:       []string{"sub", "email", "name"},
		}
		json.NewEncoder(w).Encode(discovery)
	})

	// Token endpoint
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Check for authorization code
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		code := r.FormValue("code")
		if code == "" || code == "invalid_code" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}

		// Return mock tokens
		// Note: This is a simplified mock - real JWT would need proper signing
		resp := TokenResponse{
			AccessToken: "mock-access-token",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
			IDToken:     createMockIDToken(t),
		}
		json.NewEncoder(w).Encode(resp)
	})

	// UserInfo endpoint
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		userInfo := UserInfo{
			Subject:       "user-12345",
			Email:         "test@example.com",
			EmailVerified: true,
			Name:          "Test User",
		}
		json.NewEncoder(w).Encode(userInfo)
	})

	// Logout endpoint
	mux.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		redirectURI := r.URL.Query().Get("post_logout_redirect_uri")
		if redirectURI != "" {
			http.Redirect(w, r, redirectURI, http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	return httptest.NewServer(mux)
}

// createMockIDToken creates a mock ID token for testing
// Note: This is a simplified mock without proper JWT signing
func createMockIDToken(t *testing.T) string {
	// Create a simple base64-encoded JWT-like structure for testing
	// In real tests, you'd use a proper JWT library
	header := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"
	payload := "eyJpc3MiOiJodHRwOi8vbG9jYWxob3N0Iiwic3ViIjoidXNlci0xMjM0NSIsImF1ZCI6InRlc3QtY2xpZW50IiwiZXhwIjo5OTk5OTk5OTk5LCJpYXQiOjE2MDAwMDAwMDAsImVtYWlsIjoidGVzdEBleGFtcGxlLmNvbSIsIm5hbWUiOiJUZXN0IFVzZXIifQ"
	signature := "mock-signature"
	return header + "." + payload + "." + signature
}

// TestOIDCClient_IsEnabled tests the IsEnabled method
func TestOIDCClient_IsEnabled(t *testing.T) {
	tests := []struct {
		name   string
		config *config.OIDCConfig
		want   bool
	}{
		{
			name: "enabled with all required fields",
			config: &config.OIDCConfig{
				Enabled:  true,
				Issuer:   "https://example.com",
				ClientID: "test-client",
			},
			want: true,
		},
		{
			name: "disabled explicitly",
			config: &config.OIDCConfig{
				Enabled:  false,
				Issuer:   "https://example.com",
				ClientID: "test-client",
			},
			want: false,
		},
		{
			name: "missing issuer",
			config: &config.OIDCConfig{
				Enabled:  true,
				Issuer:   "",
				ClientID: "test-client",
			},
			want: false,
		},
		{
			name: "missing client ID",
			config: &config.OIDCConfig{
				Enabled:  true,
				Issuer:   "https://example.com",
				ClientID: "",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &OIDCClient{config: tt.config}
			if got := c.IsEnabled(); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestOIDCClient_GetDiscovery tests OIDC discovery document fetching
func TestOIDCClient_GetDiscovery(t *testing.T) {
	server := mockOIDCProvider(t)
	defer server.Close()

	cfg := &config.OIDCConfig{
		Enabled:  true,
		Issuer:   server.URL,
		ClientID: "test-client",
	}

	client := &OIDCClient{
		config: cfg,
		states: make(map[string]*AuthState),
	}

	t.Run("fetch discovery document", func(t *testing.T) {
		discovery, err := client.GetDiscovery(context.Background())
		if err != nil {
			t.Fatalf("GetDiscovery() error = %v", err)
		}

		if discovery.AuthorizationEndpoint != "http://localhost/authorize" {
			t.Errorf("AuthorizationEndpoint = %v", discovery.AuthorizationEndpoint)
		}
		if discovery.TokenEndpoint != "http://localhost/token" {
			t.Errorf("TokenEndpoint = %v", discovery.TokenEndpoint)
		}
		if discovery.EndSessionEndpoint != "http://localhost/logout" {
			t.Errorf("EndSessionEndpoint = %v", discovery.EndSessionEndpoint)
		}
	})

	t.Run("cache discovery document", func(t *testing.T) {
		// First call
		d1, _ := client.GetDiscovery(context.Background())

		// Should return cached version
		d2, _ := client.GetDiscovery(context.Background())

		if d1 != d2 {
			t.Error("Second call should return cached discovery")
		}
	})
}

// TestOIDCClient_GetDiscovery_InvalidIssuer tests error handling for invalid issuer
func TestOIDCClient_GetDiscovery_InvalidIssuer(t *testing.T) {
	cfg := &config.OIDCConfig{
		Enabled:  true,
		Issuer:   "http://invalid-issuer-that-does-not-exist.local",
		ClientID: "test-client",
	}

	client := &OIDCClient{
		config: cfg,
		states: make(map[string]*AuthState),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := client.GetDiscovery(ctx)
	if err == nil {
		t.Error("GetDiscovery() should fail for invalid issuer")
	}
}

// TestOIDCClient_ValidateRedirectURI tests redirect URI validation
func TestOIDCClient_ValidateRedirectURI(t *testing.T) {
	tests := []struct {
		name         string
		allowedURIs  []string
		testURI      string
		shouldBeValid bool
	}{
		{
			name:          "valid URI in list",
			allowedURIs:   []string{"http://localhost:3000/sso", "https://example.com/sso"},
			testURI:       "http://localhost:3000/sso",
			shouldBeValid: true,
		},
		{
			name:          "URI not in list",
			allowedURIs:   []string{"http://localhost:3000/sso"},
			testURI:       "http://attacker.com/sso",
			shouldBeValid: false,
		},
		{
			name:          "empty allowed list allows all",
			allowedURIs:   []string{},
			testURI:       "http://anything.com/callback",
			shouldBeValid: true,
		},
		{
			name:          "case sensitive match",
			allowedURIs:   []string{"http://localhost:3000/sso"},
			testURI:       "http://localhost:3000/SSO",
			shouldBeValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &OIDCClient{
				config: &config.OIDCConfig{
					RedirectURIs: tt.allowedURIs,
				},
			}

			got := client.isValidRedirectURI(tt.testURI)
			if got != tt.shouldBeValid {
				t.Errorf("isValidRedirectURI(%q) = %v, want %v", tt.testURI, got, tt.shouldBeValid)
			}
		})
	}
}

// TestOIDCClient_GetAuthorizationURL tests authorization URL generation
func TestOIDCClient_GetAuthorizationURL(t *testing.T) {
	server := mockOIDCProvider(t)
	defer server.Close()

	cfg := &config.OIDCConfig{
		Enabled:      true,
		Issuer:       server.URL,
		ClientID:     "test-client",
		RedirectURIs: []string{"http://localhost:3000/sso"},
		Scopes:       []string{"openid", "profile", "email"},
	}

	client := &OIDCClient{
		config: cfg,
		states: make(map[string]*AuthState),
	}

	t.Run("generate valid authorization URL", func(t *testing.T) {
		url, err := client.GetAuthorizationURL(context.Background(), "http://localhost:3000/sso", "/dashboard")
		if err != nil {
			t.Fatalf("GetAuthorizationURL() error = %v", err)
		}

		// Check URL contains required parameters
		if !strings.Contains(url, "client_id=test-client") {
			t.Error("URL should contain client_id")
		}
		if !strings.Contains(url, "response_type=code") {
			t.Error("URL should contain response_type=code")
		}
		if !strings.Contains(url, "redirect_uri=") {
			t.Error("URL should contain redirect_uri")
		}
		if !strings.Contains(url, "state=") {
			t.Error("URL should contain state")
		}
		if !strings.Contains(url, "scope=") {
			t.Error("URL should contain scope")
		}
	})

	t.Run("reject invalid redirect URI", func(t *testing.T) {
		_, err := client.GetAuthorizationURL(context.Background(), "http://attacker.com/callback", "/")
		if err == nil {
			t.Error("GetAuthorizationURL() should reject invalid redirect URI")
		}
	})

	t.Run("generate URL with PKCE", func(t *testing.T) {
		pkceClient := &OIDCClient{
			config: &config.OIDCConfig{
				Enabled:      true,
				Issuer:       server.URL,
				ClientID:     "test-client",
				RedirectURIs: []string{"http://localhost:3000/sso"},
				Scopes:       []string{"openid"},
				RequirePKCE:  true,
			},
			states: make(map[string]*AuthState),
		}

		url, err := pkceClient.GetAuthorizationURL(context.Background(), "http://localhost:3000/sso", "/")
		if err != nil {
			t.Fatalf("GetAuthorizationURL() error = %v", err)
		}

		if !strings.Contains(url, "code_challenge=") {
			t.Error("URL should contain code_challenge for PKCE")
		}
		if !strings.Contains(url, "code_challenge_method=S256") {
			t.Error("URL should contain code_challenge_method=S256")
		}
	})
}

// TestOIDCClient_StateManagement tests state storage and consumption
func TestOIDCClient_StateManagement(t *testing.T) {
	client := &OIDCClient{
		config: &config.OIDCConfig{},
		states: make(map[string]*AuthState),
	}

	t.Run("store and consume state", func(t *testing.T) {
		state := &AuthState{
			State:       "test-state-123",
			Nonce:       "test-nonce",
			RedirectURI: "http://localhost:3000/sso",
			CreatedAt:   time.Now(),
		}

		client.storeState("test-state-123", state)

		// Consume the state
		consumed, err := client.consumeState("test-state-123")
		if err != nil {
			t.Errorf("consumeState() error = %v", err)
		}
		if consumed.Nonce != "test-nonce" {
			t.Error("Consumed state should match stored state")
		}

		// Second consumption should fail
		_, err = client.consumeState("test-state-123")
		if err == nil {
			t.Error("Second consumeState() should fail")
		}
	})

	t.Run("expired state", func(t *testing.T) {
		expiredState := &AuthState{
			State:     "expired-state",
			CreatedAt: time.Now().Add(-15 * time.Minute), // 15 minutes old
		}
		client.storeState("expired-state", expiredState)

		_, err := client.consumeState("expired-state")
		if err == nil {
			t.Error("consumeState() should fail for expired state")
		}
	})

	t.Run("nonexistent state", func(t *testing.T) {
		_, err := client.consumeState("nonexistent-state")
		if err == nil {
			t.Error("consumeState() should fail for nonexistent state")
		}
	})
}

// TestOIDCClient_GetLogoutURL tests logout URL generation
func TestOIDCClient_GetLogoutURL(t *testing.T) {
	server := mockOIDCProvider(t)
	defer server.Close()

	cfg := &config.OIDCConfig{
		Enabled:  true,
		Issuer:   server.URL,
		ClientID: "test-client",
	}

	client := &OIDCClient{
		config: cfg,
		states: make(map[string]*AuthState),
	}

	t.Run("generate logout URL with post_logout_redirect_uri", func(t *testing.T) {
		logoutURL, err := client.GetLogoutURL(context.Background(), "", "http://localhost:3000/login/")
		if err != nil {
			t.Fatalf("GetLogoutURL() error = %v", err)
		}

		if !strings.Contains(logoutURL, "client_id=test-client") {
			t.Error("Logout URL should contain client_id")
		}
		if !strings.Contains(logoutURL, "post_logout_redirect_uri=") {
			t.Error("Logout URL should contain post_logout_redirect_uri")
		}
	})

	t.Run("generate logout URL with id_token_hint", func(t *testing.T) {
		logoutURL, err := client.GetLogoutURL(context.Background(), "mock-id-token", "http://localhost:3000/login/")
		if err != nil {
			t.Fatalf("GetLogoutURL() error = %v", err)
		}

		if !strings.Contains(logoutURL, "id_token_hint=mock-id-token") {
			t.Error("Logout URL should contain id_token_hint")
		}
	})
}

// TestOIDCClient_MapOIDCRole tests role mapping
func TestOIDCClient_MapOIDCRole(t *testing.T) {
	client := &OIDCClient{
		config: &config.OIDCConfig{
			DefaultRole: "user",
		},
	}

	tests := []struct {
		oidcRole     string
		expectedRole string
	}{
		{"admin", "admin"},
		{"administrator", "admin"},
		{"Admin", "admin"},
		{"user", "user"},
		{"member", "user"},
		{"readonly", "readonly"},
		{"read-only", "readonly"},
		{"viewer", "readonly"},
		{"guest", "guest"},
		{"unknown-role", "user"}, // Falls back to default
	}

	for _, tt := range tests {
		t.Run(tt.oidcRole, func(t *testing.T) {
			got := client.mapOIDCRole(tt.oidcRole)
			if got != tt.expectedRole {
				t.Errorf("mapOIDCRole(%q) = %q, want %q", tt.oidcRole, got, tt.expectedRole)
			}
		})
	}
}

// TestGenerateRandomString tests random string generation
func TestGenerateRandomString(t *testing.T) {
	t.Run("generates correct length", func(t *testing.T) {
		s, err := generateRandomString(32)
		if err != nil {
			t.Fatalf("generateRandomString() error = %v", err)
		}
		if len(s) != 32 {
			t.Errorf("len(s) = %d, want 32", len(s))
		}
	})

	t.Run("generates unique strings", func(t *testing.T) {
		s1, _ := generateRandomString(32)
		s2, _ := generateRandomString(32)
		if s1 == s2 {
			t.Error("Generated strings should be unique")
		}
	})
}

// TestGenerateCodeChallenge tests PKCE code challenge generation
func TestGenerateCodeChallenge(t *testing.T) {
	t.Run("generates valid code challenge", func(t *testing.T) {
		verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
		challenge := generateCodeChallenge(verifier)

		// Challenge should be URL-safe base64 encoded
		if strings.Contains(challenge, "+") || strings.Contains(challenge, "/") {
			t.Error("Challenge should be URL-safe base64")
		}

		// Should not be empty
		if challenge == "" {
			t.Error("Challenge should not be empty")
		}
	})

	t.Run("same verifier produces same challenge", func(t *testing.T) {
		verifier := "test-verifier-12345"
		c1 := generateCodeChallenge(verifier)
		c2 := generateCodeChallenge(verifier)
		if c1 != c2 {
			t.Error("Same verifier should produce same challenge")
		}
	})

	t.Run("different verifiers produce different challenges", func(t *testing.T) {
		c1 := generateCodeChallenge("verifier-1")
		c2 := generateCodeChallenge("verifier-2")
		if c1 == c2 {
			t.Error("Different verifiers should produce different challenges")
		}
	})
}

// TestOIDCClient_ExtractOrgID tests org ID extraction from claims
func TestOIDCClient_ExtractOrgID(t *testing.T) {
	tests := []struct {
		name      string
		orgClaim  string
		claims    *IDTokenClaims
		userInfo  *UserInfo
		wantOrgID string
	}{
		{
			name:     "extract from ID token extra claims",
			orgClaim: "tenant_id",
			claims: &IDTokenClaims{
				Extra: map[string]interface{}{
					"tenant_id": "org-12345",
				},
			},
			wantOrgID: "org-12345",
		},
		{
			name:     "extract from userinfo",
			orgClaim: "org_id",
			claims:   &IDTokenClaims{Extra: map[string]interface{}{}},
			userInfo: &UserInfo{
				OrgID: "org-from-userinfo",
			},
			wantOrgID: "org-from-userinfo",
		},
		{
			name:      "no org claim configured",
			orgClaim:  "",
			claims:    &IDTokenClaims{Extra: map[string]interface{}{"tenant_id": "org-12345"}},
			wantOrgID: "",
		},
		{
			name:      "org claim missing from token",
			orgClaim:  "missing_claim",
			claims:    &IDTokenClaims{Extra: map[string]interface{}{}},
			wantOrgID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &OIDCClient{
				config: &config.OIDCConfig{
					OrgClaim: tt.orgClaim,
				},
			}

			got := client.extractOrgID(tt.claims, tt.userInfo)
			if got != tt.wantOrgID {
				t.Errorf("extractOrgID() = %q, want %q", got, tt.wantOrgID)
			}
		})
	}
}

// TestOIDCClient_ExtractRoles tests roles extraction from claims
func TestOIDCClient_ExtractRoles(t *testing.T) {
	tests := []struct {
		name       string
		rolesClaim string
		claims     *IDTokenClaims
		userInfo   *UserInfo
		wantRoles  []string
	}{
		{
			name:       "extract array of roles from claims",
			rolesClaim: "roles",
			claims: &IDTokenClaims{
				Extra: map[string]interface{}{
					"roles": []interface{}{"admin", "user"},
				},
			},
			wantRoles: []string{"admin", "user"},
		},
		{
			name:       "extract single role string from claims",
			rolesClaim: "role",
			claims: &IDTokenClaims{
				Extra: map[string]interface{}{
					"role": "admin",
				},
			},
			wantRoles: []string{"admin"},
		},
		{
			name:       "extract from userinfo",
			rolesClaim: "roles",
			claims:     &IDTokenClaims{Extra: map[string]interface{}{}},
			userInfo: &UserInfo{
				Roles: []string{"user", "viewer"},
			},
			wantRoles: []string{"user", "viewer"},
		},
		{
			name:       "no roles claim configured",
			rolesClaim: "",
			claims:     &IDTokenClaims{Extra: map[string]interface{}{"roles": []interface{}{"admin"}}},
			wantRoles:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &OIDCClient{
				config: &config.OIDCConfig{
					RolesClaim: tt.rolesClaim,
				},
			}

			got := client.extractRoles(tt.claims, tt.userInfo)

			if len(got) != len(tt.wantRoles) {
				t.Errorf("extractRoles() returned %d roles, want %d", len(got), len(tt.wantRoles))
				return
			}

			for i, role := range got {
				if role != tt.wantRoles[i] {
					t.Errorf("role[%d] = %q, want %q", i, role, tt.wantRoles[i])
				}
			}
		})
	}
}
