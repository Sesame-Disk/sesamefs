// Package auth provides authentication functionality for SesameFS
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/config"
	"github.com/Sesame-Disk/sesamefs/internal/db"
	"github.com/google/uuid"
)

// OIDCClient handles OIDC authentication flows
type OIDCClient struct {
	config   *config.OIDCConfig
	db       *db.DB
	sessions *SessionManager

	// Cached OIDC discovery document
	discoveryMu sync.RWMutex
	discovery   *OIDCDiscovery
	discoveryAt time.Time

	// PKCE state storage (state -> code_verifier)
	stateMu sync.RWMutex
	states  map[string]*AuthState
}

// OIDCDiscovery represents the OIDC discovery document
type OIDCDiscovery struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	UserInfoEndpoint      string   `json:"userinfo_endpoint"`
	JwksURI               string   `json:"jwks_uri"`
	ScopesSupported       []string `json:"scopes_supported"`
	ClaimsSupported       []string `json:"claims_supported"`
	EndSessionEndpoint    string   `json:"end_session_endpoint"`
}

// AuthState holds the state for an ongoing authorization request
type AuthState struct {
	State        string
	Nonce        string
	CodeVerifier string // For PKCE
	RedirectURI  string
	CreatedAt    time.Time
	ReturnURL    string // Where to redirect after successful auth
}

// TokenResponse represents the OIDC token endpoint response
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// IDTokenClaims represents the claims in an OIDC ID token
type IDTokenClaims struct {
	// Standard OIDC claims
	Issuer    string `json:"iss"`
	Subject   string `json:"sub"`
	Audience  string `json:"aud"`
	ExpiresAt int64  `json:"exp"`
	IssuedAt  int64  `json:"iat"`
	Nonce     string `json:"nonce,omitempty"`

	// Profile claims
	Name              string `json:"name,omitempty"`
	GivenName         string `json:"given_name,omitempty"`
	FamilyName        string `json:"family_name,omitempty"`
	PreferredUsername string `json:"preferred_username,omitempty"`
	Picture           string `json:"picture,omitempty"`

	// Email claims
	Email         string `json:"email,omitempty"`
	EmailVerified bool   `json:"email_verified,omitempty"`

	// Custom claims (will be extracted dynamically)
	Extra map[string]interface{} `json:"-"`
}

// UserInfo represents the user information from OIDC
type UserInfo struct {
	Subject       string   `json:"sub"`
	Email         string   `json:"email"`
	EmailVerified bool     `json:"email_verified"`
	Name          string   `json:"name"`
	Picture       string   `json:"picture"`
	Locale        string   `json:"locale"`
	OrgID         string   `json:"org_id,omitempty"`  // Extracted from custom claim
	Roles         []string `json:"roles,omitempty"`   // Extracted from custom claim
}

// AuthResult represents the result of a successful authentication
type AuthResult struct {
	UserID       string
	OrgID        string
	Email        string
	Name         string
	Role         string
	SessionToken string
	ExpiresAt    time.Time
	IsNewUser    bool
}

// NewOIDCClient creates a new OIDC client
func NewOIDCClient(cfg *config.OIDCConfig, database *db.DB, sessions *SessionManager) *OIDCClient {
	return &OIDCClient{
		config:   cfg,
		db:       database,
		sessions: sessions,
		states:   make(map[string]*AuthState),
	}
}

// IsEnabled returns whether OIDC authentication is enabled
func (c *OIDCClient) IsEnabled() bool {
	return c.config.Enabled && c.config.Issuer != "" && c.config.ClientID != ""
}

// GetDiscovery fetches and caches the OIDC discovery document
func (c *OIDCClient) GetDiscovery(ctx context.Context) (*OIDCDiscovery, error) {
	c.discoveryMu.RLock()
	if c.discovery != nil && time.Since(c.discoveryAt) < 1*time.Hour {
		d := c.discovery
		c.discoveryMu.RUnlock()
		return d, nil
	}
	c.discoveryMu.RUnlock()

	// Fetch discovery document
	discoveryURL := strings.TrimSuffix(c.config.Issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, "GET", discoveryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch discovery document: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("discovery endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var discovery OIDCDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return nil, fmt.Errorf("failed to parse discovery document: %w", err)
	}

	// Cache the discovery document
	c.discoveryMu.Lock()
	c.discovery = &discovery
	c.discoveryAt = time.Now()
	c.discoveryMu.Unlock()

	return &discovery, nil
}

// GetAuthorizationURL returns the URL to redirect users to for authentication
func (c *OIDCClient) GetAuthorizationURL(ctx context.Context, redirectURI, returnURL string) (string, error) {
	// Validate redirect URI
	if !c.isValidRedirectURI(redirectURI) {
		return "", fmt.Errorf("invalid redirect URI: %s", redirectURI)
	}

	// Get discovery document
	discovery, err := c.GetDiscovery(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get discovery: %w", err)
	}

	// Generate state and nonce
	state, err := generateRandomString(32)
	if err != nil {
		return "", fmt.Errorf("failed to generate state: %w", err)
	}
	nonce, err := generateRandomString(32)
	if err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Generate PKCE code verifier and challenge
	var codeVerifier, codeChallenge string
	if c.config.RequirePKCE {
		codeVerifier, err = generateRandomString(64)
		if err != nil {
			return "", fmt.Errorf("failed to generate code verifier: %w", err)
		}
		codeChallenge = generateCodeChallenge(codeVerifier)
	}

	// Store state for validation
	authState := &AuthState{
		State:        state,
		Nonce:        nonce,
		CodeVerifier: codeVerifier,
		RedirectURI:  redirectURI,
		CreatedAt:    time.Now(),
		ReturnURL:    returnURL,
	}
	c.storeState(state, authState)

	// Build authorization URL
	authURL, err := url.Parse(discovery.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("invalid authorization endpoint: %w", err)
	}

	params := url.Values{}
	params.Set("client_id", c.config.ClientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", redirectURI)
	params.Set("state", state)
	params.Set("nonce", nonce)
	params.Set("scope", strings.Join(c.config.Scopes, " "))

	if c.config.RequirePKCE {
		params.Set("code_challenge", codeChallenge)
		params.Set("code_challenge_method", "S256")
	}

	authURL.RawQuery = params.Encode()
	return authURL.String(), nil
}

// ExchangeCode exchanges an authorization code for tokens
func (c *OIDCClient) ExchangeCode(ctx context.Context, code, state, redirectURI string) (*AuthResult, error) {
	// Validate and consume state
	authState, err := c.consumeState(state)
	if err != nil {
		return nil, fmt.Errorf("invalid state: %w", err)
	}

	// Verify redirect URI matches
	if authState.RedirectURI != redirectURI {
		return nil, fmt.Errorf("redirect URI mismatch")
	}

	// Get discovery document
	discovery, err := c.GetDiscovery(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get discovery: %w", err)
	}

	// Build token request
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", c.config.ClientID)
	data.Set("client_secret", c.config.ClientSecret)
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)

	if authState.CodeVerifier != "" {
		data.Set("code_verifier", authState.CodeVerifier)
	}

	// Exchange code for tokens
	req, err := http.NewRequestWithContext(ctx, "POST", discovery.TokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	// Parse ID token to get user claims
	claims, err := c.parseIDToken(tokenResp.IDToken, authState.Nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ID token: %w", err)
	}

	// Get additional user info if needed
	userInfo, err := c.getUserInfo(ctx, tokenResp.AccessToken)
	if err != nil {
		// Log but don't fail - we have basic info from ID token
		fmt.Printf("Warning: failed to get userinfo: %v\n", err)
	}

	// Merge userinfo with ID token claims
	if userInfo != nil {
		if claims.Email == "" {
			claims.Email = userInfo.Email
		}
		if claims.Name == "" {
			claims.Name = userInfo.Name
		}
	}

	// Provision user (find existing or create new)
	result, err := c.provisionUser(ctx, claims, userInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to provision user: %w", err)
	}

	// Create session
	session, err := c.sessions.CreateSession(result.UserID, result.OrgID, result.Email, result.Role)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	result.SessionToken = session.Token
	result.ExpiresAt = session.ExpiresAt

	return result, nil
}

// parseIDToken parses and validates an ID token (basic validation without full JWT verification)
// Note: In production, you should use a proper JWT library with key verification
func (c *OIDCClient) parseIDToken(idToken, expectedNonce string) (*IDTokenClaims, error) {
	if idToken == "" {
		return nil, fmt.Errorf("empty ID token")
	}

	// Split JWT into parts
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format")
	}

	// Decode payload (middle part)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWT payload: %w", err)
	}

	// Parse claims
	var claims IDTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse JWT claims: %w", err)
	}

	// Also parse into a map for custom claims
	var rawClaims map[string]interface{}
	if err := json.Unmarshal(payload, &rawClaims); err != nil {
		return nil, fmt.Errorf("failed to parse raw claims: %w", err)
	}
	claims.Extra = rawClaims

	// Validate issuer
	expectedIssuer := strings.TrimSuffix(c.config.Issuer, "/")
	actualIssuer := strings.TrimSuffix(claims.Issuer, "/")
	if actualIssuer != expectedIssuer {
		return nil, fmt.Errorf("issuer mismatch: expected %s, got %s", expectedIssuer, actualIssuer)
	}

	// Validate expiration with clock skew tolerance
	now := time.Now().Unix()
	clockSkew := int64(c.config.AllowedClockSkew.Seconds())
	if claims.ExpiresAt < now-clockSkew {
		return nil, fmt.Errorf("token has expired")
	}

	// Validate nonce if provided
	if expectedNonce != "" && claims.Nonce != expectedNonce {
		return nil, fmt.Errorf("nonce mismatch")
	}

	return &claims, nil
}

// getUserInfo fetches user information from the userinfo endpoint
func (c *OIDCClient) getUserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	discovery, err := c.GetDiscovery(ctx)
	if err != nil {
		return nil, err
	}

	if discovery.UserInfoEndpoint == "" {
		return nil, nil // No userinfo endpoint
	}

	req, err := http.NewRequestWithContext(ctx, "GET", discovery.UserInfoEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo endpoint returned %d", resp.StatusCode)
	}

	var userInfo UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, err
	}

	return &userInfo, nil
}

// provisionUser finds or creates a user based on OIDC claims
func (c *OIDCClient) provisionUser(ctx context.Context, claims *IDTokenClaims, userInfo *UserInfo) (*AuthResult, error) {
	// Extract org ID from custom claim
	orgID := c.extractOrgID(claims, userInfo)
	if orgID == "" {
		if c.config.DefaultOrgID != "" {
			orgID = c.config.DefaultOrgID
		} else {
			// Create a deterministic org ID from the OIDC subject
			orgID = uuid.NewSHA1(uuid.NameSpaceURL, []byte(c.config.Issuer+"/orgs/default")).String()
		}
	}

	// Extract roles from custom claim
	roles := c.extractRoles(claims, userInfo)
	role := c.config.DefaultRole
	if len(roles) > 0 {
		role = c.mapOIDCRole(roles[0])
	}

	// Auto-provision organization if it doesn't exist
	if c.config.AutoProvision && orgID != "" {
		var existingOrgID string
		orgErr := c.db.Session().Query(`
			SELECT org_id FROM organizations WHERE org_id = ?
		`, orgID).Scan(&existingOrgID)
		if orgErr != nil {
			// Org doesn't exist - create it
			orgName := c.config.DefaultOrgName
			if orgName == "" {
				orgName = "Auto-provisioned Organization"
			}
			now := time.Now()
			createErr := c.db.Session().Query(`
				INSERT INTO organizations (org_id, name, settings, storage_quota, storage_used, chunking_polynomial, storage_config, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			`, orgID, orgName,
				map[string]string{"theme": "default", "features": "all"},
				int64(1099511627776), int64(0), int64(17592186044415),
				map[string]string{"default_backend": "s3"},
				now,
			).Exec()
			if createErr != nil {
				fmt.Printf("Warning: failed to auto-provision org %s: %v\n", orgID, createErr)
				// Continue - the org might have been created concurrently
			}
		}
	}

	// Look up user by OIDC subject
	var userID string
	err := c.db.Session().Query(`
		SELECT user_id FROM users_by_oidc
		WHERE oidc_issuer = ? AND oidc_sub = ?
	`, c.config.Issuer, claims.Subject).Scan(&userID)

	isNewUser := false
	if err != nil {
		// User not found - provision new user if enabled
		if !c.config.AutoProvision {
			return nil, fmt.Errorf("user not found and auto-provisioning is disabled")
		}

		isNewUser = true
		userID = uuid.New().String()

		// Determine email and name
		email := claims.Email
		if email == "" && userInfo != nil {
			email = userInfo.Email
		}
		if email == "" {
			email = claims.Subject + "@" + strings.TrimPrefix(c.config.Issuer, "https://")
		}

		name := claims.Name
		if name == "" {
			name = claims.PreferredUsername
		}
		if name == "" && userInfo != nil {
			name = userInfo.Name
		}
		if name == "" {
			name = email
		}

		// Create user record
		if err := c.createUser(ctx, userID, orgID, email, name, role, claims.Subject); err != nil {
			return nil, fmt.Errorf("failed to create user: %w", err)
		}
	} else {
		// Existing user re-login: sync role from OIDC (OIDC is source of truth)
		var dbRole string
		roleErr := c.db.Session().Query(`
			SELECT role FROM users WHERE org_id = ? AND user_id = ?
		`, orgID, userID).Scan(&dbRole)
		if roleErr == nil && dbRole != role {
			if updateErr := c.db.Session().Query(`
				UPDATE users SET role = ? WHERE org_id = ? AND user_id = ?
			`, role, orgID, userID).Exec(); updateErr != nil {
				fmt.Printf("Warning: failed to sync role from OIDC: %v\n", updateErr)
			}
		}
	}

	// Sync group memberships from OIDC claims
	if c.config.SyncGroupsOnLogin {
		groups := c.extractGroups(claims, userInfo)
		if len(groups) > 0 {
			if syncErr := c.syncGroupMembership(ctx, orgID, userID, claims.Email, groups); syncErr != nil {
				fmt.Printf("Warning: failed to sync group memberships: %v\n", syncErr)
			}
		}
	}

	// Sync department memberships from OIDC claims
	if c.config.SyncDeptsOnLogin {
		depts := c.extractDepartments(claims, userInfo)
		if len(depts) > 0 {
			if syncErr := c.syncDepartmentMembership(ctx, orgID, userID, claims.Email, depts); syncErr != nil {
				fmt.Printf("Warning: failed to sync department memberships: %v\n", syncErr)
			}
		}
	}

	// Get user details
	var email, name string
	err = c.db.Session().Query(`
		SELECT email, name FROM users
		WHERE org_id = ? AND user_id = ?
	`, orgID, userID).Scan(&email, &name)
	if err != nil {
		// Use claims data as fallback
		email = claims.Email
		name = claims.Name
	}

	return &AuthResult{
		UserID:    userID,
		OrgID:     orgID,
		Email:     email,
		Name:      name,
		Role:      role,
		IsNewUser: isNewUser,
	}, nil
}

// createUser creates a new user record in the database
func (c *OIDCClient) createUser(ctx context.Context, userID, orgID, email, name, role, oidcSub string) error {
	now := time.Now()

	// Create OIDC mapping
	if err := c.db.Session().Query(`
		INSERT INTO users_by_oidc (oidc_issuer, oidc_sub, user_id, org_id)
		VALUES (?, ?, ?, ?)
	`, c.config.Issuer, oidcSub, userID, orgID).Exec(); err != nil {
		return fmt.Errorf("failed to create OIDC mapping: %w", err)
	}

	// Create user record
	if err := c.db.Session().Query(`
		INSERT INTO users (org_id, user_id, email, name, role, quota_bytes, used_bytes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, orgID, userID, email, name, role, int64(-2), int64(0), now).Exec(); err != nil {
		return fmt.Errorf("failed to create user record: %w", err)
	}

	return nil
}

// extractOrgID extracts the organization ID from OIDC claims.
// If the org claim value matches PlatformOrgClaimValue, returns PlatformOrgID.
func (c *OIDCClient) extractOrgID(claims *IDTokenClaims, userInfo *UserInfo) string {
	if c.config.OrgClaim == "" {
		return ""
	}

	var orgClaimValue string

	// Check ID token extra claims
	if claims.Extra != nil {
		if v, ok := claims.Extra[c.config.OrgClaim].(string); ok {
			orgClaimValue = v
		}
	}

	// Check userinfo as fallback
	if orgClaimValue == "" && userInfo != nil && userInfo.OrgID != "" {
		orgClaimValue = userInfo.OrgID
	}

	if orgClaimValue == "" {
		return ""
	}

	// Map platform org claim value to platform org UUID
	if c.config.PlatformOrgClaimValue != "" && orgClaimValue == c.config.PlatformOrgClaimValue {
		return c.config.PlatformOrgID
	}

	return orgClaimValue
}

// extractRoles extracts roles from OIDC claims
func (c *OIDCClient) extractRoles(claims *IDTokenClaims, userInfo *UserInfo) []string {
	if c.config.RolesClaim == "" {
		return nil
	}

	// Check ID token extra claims
	if claims.Extra != nil {
		if roles, ok := claims.Extra[c.config.RolesClaim].([]interface{}); ok {
			result := make([]string, 0, len(roles))
			for _, r := range roles {
				if s, ok := r.(string); ok {
					result = append(result, s)
				}
			}
			return result
		}
		if role, ok := claims.Extra[c.config.RolesClaim].(string); ok {
			return []string{role}
		}
	}

	// Check userinfo
	if userInfo != nil && len(userInfo.Roles) > 0 {
		return userInfo.Roles
	}

	return nil
}

// mapOIDCRole maps an OIDC role to a SesameFS role
func (c *OIDCClient) mapOIDCRole(oidcRole string) string {
	switch strings.ToLower(oidcRole) {
	case "superadmin", "super_admin", "platform_admin":
		return "superadmin"
	case "admin", "administrator":
		return "admin"
	case "user", "member":
		return "user"
	case "readonly", "read-only", "viewer":
		return "readonly"
	case "guest":
		return "guest"
	default:
		return c.config.DefaultRole
	}
}

// isValidRedirectURI checks if a redirect URI is in the allowed list
func (c *OIDCClient) isValidRedirectURI(uri string) bool {
	if len(c.config.RedirectURIs) == 0 {
		return true // No restrictions
	}
	for _, allowed := range c.config.RedirectURIs {
		if uri == allowed {
			return true
		}
	}
	return false
}

// storeState stores an auth state for later validation
func (c *OIDCClient) storeState(state string, authState *AuthState) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()

	// Clean up old states (older than 10 minutes)
	cutoff := time.Now().Add(-10 * time.Minute)
	for s, as := range c.states {
		if as.CreatedAt.Before(cutoff) {
			delete(c.states, s)
		}
	}

	c.states[state] = authState
}

// consumeState retrieves and removes an auth state
func (c *OIDCClient) consumeState(state string) (*AuthState, error) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()

	authState, ok := c.states[state]
	if !ok {
		return nil, fmt.Errorf("state not found")
	}

	// Check if state has expired (10 minutes)
	if time.Since(authState.CreatedAt) > 10*time.Minute {
		delete(c.states, state)
		return nil, fmt.Errorf("state has expired")
	}

	delete(c.states, state)
	return authState, nil
}

// GetLogoutURL returns the URL to redirect users to for logout
func (c *OIDCClient) GetLogoutURL(ctx context.Context, idToken, postLogoutRedirectURI string) (string, error) {
	discovery, err := c.GetDiscovery(ctx)
	if err != nil {
		return "", err
	}

	if discovery.EndSessionEndpoint == "" {
		return "", nil // No logout endpoint
	}

	logoutURL, err := url.Parse(discovery.EndSessionEndpoint)
	if err != nil {
		return "", err
	}

	params := url.Values{}
	params.Set("client_id", c.config.ClientID)
	if idToken != "" {
		params.Set("id_token_hint", idToken)
	}
	if postLogoutRedirectURI != "" {
		params.Set("post_logout_redirect_uri", postLogoutRedirectURI)
	}

	logoutURL.RawQuery = params.Encode()
	return logoutURL.String(), nil
}

// Helper functions

// generateRandomString generates a cryptographically secure random string
func generateRandomString(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b)[:length], nil
}

// generateCodeChallenge generates a PKCE code challenge from a code verifier
func generateCodeChallenge(verifier string) string {
	// S256: BASE64URL(SHA256(ASCII(code_verifier)))
	h := sha256Sum([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// sha256Sum computes SHA-256 hash
func sha256Sum(data []byte) [32]byte {
	return sha256.Sum256(data)
}

// =============================================================================
// OIDC Group & Department Claim Sync
// =============================================================================

// GroupClaim represents a group membership from OIDC claims.
type GroupClaim struct {
	ID   string `json:"id"`   // External group ID
	Name string `json:"name"` // Group display name
}

// DepartmentClaim represents a department membership from OIDC claims.
type DepartmentClaim struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parent_id,omitempty"`
}

// extractGroups extracts group claims from the ID token or userinfo.
// Supports both array-of-strings and array-of-objects formats.
func (c *OIDCClient) extractGroups(claims *IDTokenClaims, userInfo *UserInfo) []GroupClaim {
	if c.config.GroupsClaim == "" {
		return nil
	}

	raw := c.getClaimValue(claims, c.config.GroupsClaim)
	if raw == nil {
		return nil
	}

	return parseGroupClaims(raw)
}

// extractDepartments extracts department claims from the ID token or userinfo.
// Supports both array-of-strings and array-of-objects formats.
func (c *OIDCClient) extractDepartments(claims *IDTokenClaims, userInfo *UserInfo) []DepartmentClaim {
	if c.config.DepartmentsClaim == "" {
		return nil
	}

	raw := c.getClaimValue(claims, c.config.DepartmentsClaim)
	if raw == nil {
		return nil
	}

	return parseDepartmentClaims(raw)
}

// getClaimValue retrieves a claim value from the ID token extra claims.
func (c *OIDCClient) getClaimValue(claims *IDTokenClaims, claimName string) interface{} {
	if claims.Extra != nil {
		if v, ok := claims.Extra[claimName]; ok {
			return v
		}
	}
	return nil
}

// parseGroupClaims parses a raw claim value into GroupClaim slice.
// Supports: ["group1", "group2"] or [{"id": "abc", "name": "Engineering"}]
func parseGroupClaims(raw interface{}) []GroupClaim {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}

	var groups []GroupClaim
	for _, item := range arr {
		switch v := item.(type) {
		case string:
			groups = append(groups, GroupClaim{ID: v, Name: v})
		case map[string]interface{}:
			gc := GroupClaim{}
			if id, ok := v["id"].(string); ok {
				gc.ID = id
			}
			if name, ok := v["name"].(string); ok {
				gc.Name = name
			}
			if gc.ID == "" && gc.Name != "" {
				gc.ID = gc.Name
			}
			if gc.ID != "" {
				if gc.Name == "" {
					gc.Name = gc.ID
				}
				groups = append(groups, gc)
			}
		}
	}
	return groups
}

// parseDepartmentClaims parses a raw claim value into DepartmentClaim slice.
// Supports: ["dept1", "dept2"] or [{"id": "abc", "name": "Engineering", "parent_id": "xyz"}]
func parseDepartmentClaims(raw interface{}) []DepartmentClaim {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}

	var depts []DepartmentClaim
	for _, item := range arr {
		switch v := item.(type) {
		case string:
			depts = append(depts, DepartmentClaim{ID: v, Name: v})
		case map[string]interface{}:
			dc := DepartmentClaim{}
			if id, ok := v["id"].(string); ok {
				dc.ID = id
			}
			if name, ok := v["name"].(string); ok {
				dc.Name = name
			}
			if pid, ok := v["parent_id"].(string); ok {
				dc.ParentID = pid
			}
			if dc.ID == "" && dc.Name != "" {
				dc.ID = dc.Name
			}
			if dc.ID != "" {
				if dc.Name == "" {
					dc.Name = dc.ID
				}
				depts = append(depts, dc)
			}
		}
	}
	return depts
}

// orgNamespace is a UUID namespace for generating deterministic group UUIDs from OIDC claims.
var orgNamespace = uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8") // URL namespace

// syncGroupMembership syncs a user's group memberships from OIDC claims.
func (c *OIDCClient) syncGroupMembership(ctx context.Context, orgID, userID, email string, groups []GroupClaim) error {
	now := time.Now()
	claimedGroupIDs := make(map[string]bool)

	for _, g := range groups {
		// Generate deterministic UUID from external group ID
		groupUUID := uuid.NewSHA1(orgNamespace, []byte(orgID+":group:"+g.ID))
		groupIDStr := groupUUID.String()
		claimedGroupIDs[groupIDStr] = true

		// Upsert group (INSERT IF NOT EXISTS equivalent - Cassandra uses LWT)
		c.db.Session().Query(`
			INSERT INTO groups (org_id, group_id, name, creator_id, is_department, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?) IF NOT EXISTS
		`, orgID, groupIDStr, g.Name, userID, false, now, now).Exec()

		// Add user to group_members (upsert)
		c.db.Session().Query(`
			INSERT INTO group_members (group_id, user_id, role, added_at)
			VALUES (?, ?, ?, ?)
		`, groupIDStr, userID, "member", now).Exec()

		// Add to lookup table (upsert)
		c.db.Session().Query(`
			INSERT INTO groups_by_member (org_id, user_id, group_id, group_name, role, added_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, orgID, userID, groupIDStr, g.Name, "member", now).Exec()
	}

	// Full sync: remove from groups not in claims
	if c.config.FullSyncGroups {
		iter := c.db.Session().Query(`
			SELECT group_id FROM groups_by_member WHERE org_id = ? AND user_id = ?
		`, orgID, userID).Iter()

		var existingGroupID string
		for iter.Scan(&existingGroupID) {
			if !claimedGroupIDs[existingGroupID] {
				// Check if this group was OIDC-synced (deterministic UUID pattern)
				// Remove membership
				c.db.Session().Query(`
					DELETE FROM group_members WHERE group_id = ? AND user_id = ?
				`, existingGroupID, userID).Exec()
				c.db.Session().Query(`
					DELETE FROM groups_by_member WHERE org_id = ? AND user_id = ? AND group_id = ?
				`, orgID, userID, existingGroupID).Exec()
			}
		}
		iter.Close()
	}

	return nil
}

// syncDepartmentMembership syncs a user's department memberships from OIDC claims.
func (c *OIDCClient) syncDepartmentMembership(ctx context.Context, orgID, userID, email string, depts []DepartmentClaim) error {
	now := time.Now()
	claimedDeptIDs := make(map[string]bool)

	for _, d := range depts {
		// Generate deterministic UUID from external department ID
		deptUUID := uuid.NewSHA1(orgNamespace, []byte(orgID+":dept:"+d.ID))
		deptIDStr := deptUUID.String()
		claimedDeptIDs[deptIDStr] = true

		// Resolve parent group ID if specified
		var parentGroupIDStr string
		if d.ParentID != "" {
			parentUUID := uuid.NewSHA1(orgNamespace, []byte(orgID+":dept:"+d.ParentID))
			parentGroupIDStr = parentUUID.String()
		}

		// Upsert department as a group with is_department=true
		if parentGroupIDStr != "" {
			c.db.Session().Query(`
				INSERT INTO groups (org_id, group_id, name, creator_id, parent_group_id, is_department, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?) IF NOT EXISTS
			`, orgID, deptIDStr, d.Name, userID, parentGroupIDStr, true, now, now).Exec()
		} else {
			c.db.Session().Query(`
				INSERT INTO groups (org_id, group_id, name, creator_id, is_department, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?) IF NOT EXISTS
			`, orgID, deptIDStr, d.Name, userID, true, now, now).Exec()
		}

		// Add user to group_members
		c.db.Session().Query(`
			INSERT INTO group_members (group_id, user_id, role, added_at)
			VALUES (?, ?, ?, ?)
		`, deptIDStr, userID, "member", now).Exec()

		// Add to lookup table
		c.db.Session().Query(`
			INSERT INTO groups_by_member (org_id, user_id, group_id, group_name, role, added_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, orgID, userID, deptIDStr, d.Name, "member", now).Exec()
	}

	// Full sync: remove from departments not in claims
	if c.config.FullSyncDepts {
		iter := c.db.Session().Query(`
			SELECT group_id FROM groups_by_member WHERE org_id = ? AND user_id = ?
		`, orgID, userID).Iter()

		var existingGroupID string
		for iter.Scan(&existingGroupID) {
			if !claimedDeptIDs[existingGroupID] {
				// Check if this is a department before removing
				var isDept bool
				if err := c.db.Session().Query(`
					SELECT is_department FROM groups WHERE org_id = ? AND group_id = ?
				`, orgID, existingGroupID).Scan(&isDept); err == nil && isDept {
					c.db.Session().Query(`
						DELETE FROM group_members WHERE group_id = ? AND user_id = ?
					`, existingGroupID, userID).Exec()
					c.db.Session().Query(`
						DELETE FROM groups_by_member WHERE org_id = ? AND user_id = ? AND group_id = ?
					`, orgID, userID, existingGroupID).Exec()
				}
			}
		}
		iter.Close()
	}

	return nil
}
