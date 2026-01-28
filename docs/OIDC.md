# OIDC Integration - SesameFS

**Last Updated**: 2026-01-28
**Status**: HIGH PRIORITY - Required for Production

---

## Overview

SesameFS will use OIDC (OpenID Connect) for user authentication and tenant/organization management. The OIDC provider will be the source of truth for:

- User accounts (creation, deletion, profile data)
- Organization/tenant management
- User roles and permissions
- Multi-tenant isolation

---

## OIDC Provider Details

### Test Environment

| Setting | Value |
|---------|-------|
| **Provider URL** | https://t-accounts.sesamedisk.com/ |
| **Client ID** | 657640 |
| **Client Secret** | `070101ea014c91cb749221a354c49f68e6000c8c40ff11d2119599dc` |
| **Redirect URI (dev)** | http://localhost:3000/sso |

### Discovery Endpoint

The OIDC discovery document should be available at:
```
https://t-accounts.sesamedisk.com/.well-known/openid-configuration
```

### Redirect URIs

The redirect URI (`/sso` endpoint) must be configurable to support multiple environments:

| Environment | Redirect URI |
|-------------|--------------|
| Development | http://localhost:3000/sso |
| Staging | https://staging.sesamefs.com/sso |
| Production | https://app.sesamefs.com/sso |

**Important**: The OIDC provider must have ALL allowed redirect URIs registered. The backend configuration should accept a list of allowed URIs for validation.

---

## Implementation Plan

### Phase 1: Basic OIDC Login

**Goal**: Replace dev token authentication with OIDC login flow

1. **Add OIDC Configuration**
   - Add OIDC settings to `internal/config/config.go`
   - Environment variables: `OIDC_ISSUER_URL`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`, `OIDC_REDIRECT_URIS`
   - Support multiple redirect URIs (comma-separated list)

2. **Implement OIDC Authentication Flow**

   **Frontend (`/sso` page)**:
   - Create `frontend/src/pages/sso/sso.js` - SSO callback handler
   - Extract `code` and `state` from URL params
   - Send code to backend API for token exchange
   - Store returned session token
   - Redirect to dashboard on success

   **Backend endpoints**:
   - `GET /api/v2.1/auth/oidc/login` - Returns OIDC authorization URL
   - `POST /api/v2.1/auth/oidc/callback` - Exchanges code for tokens, creates session
   - Validate redirect_uri is in allowed list before processing

3. **Redirect URI Validation**
   ```go
   // Validate redirect_uri is in allowed list
   func (h *OIDCHandler) validateRedirectURI(uri string) bool {
       for _, allowed := range h.config.RedirectURIs {
           if uri == allowed {
               return true
           }
       }
       return false
   }
   ```

4. **Database Schema** (already exists)
   ```sql
   CREATE TABLE sesamefs.users_by_oidc (
       oidc_issuer TEXT,
       oidc_sub TEXT,
       user_id UUID,
       org_id UUID,
       PRIMARY KEY ((oidc_issuer), oidc_sub)
   );
   ```

### Phase 2: Organization/Tenant Mapping

**Goal**: Map OIDC claims to SesameFS organizations

1. **OIDC Claims to Use**
   - `sub` - Unique user identifier
   - `email` - User email
   - `name` - Display name
   - `org_id` or custom claim - Organization/tenant identifier
   - `roles` or custom claim - User roles within organization

2. **Auto-Provisioning**
   - First login creates user record if not exists
   - Organization is created or mapped from OIDC claims
   - User assigned to organization based on OIDC tenant claim

### Phase 3: Role Synchronization

**Goal**: Sync roles from OIDC provider to SesameFS

1. **Role Mapping**
   | OIDC Role | SesameFS Role |
   |-----------|---------------|
   | `admin` | admin |
   | `user` | user |
   | `readonly` | readonly |
   | `guest` | guest |

2. **Periodic Sync**
   - Refresh token flow updates user roles
   - Optional webhook for real-time role changes

---

## Files to Create/Modify

### Backend

| File | Purpose |
|------|---------|
| `internal/auth/oidc.go` | OIDC authentication logic, token exchange |
| `internal/auth/session.go` | Session management (JWT creation) |
| `internal/config/config.go` | OIDC configuration options (issuer, client_id, redirect_uris) |
| `internal/api/server.go` | Register OIDC routes |
| `internal/api/v2/auth.go` | OIDC endpoint handlers |

### Frontend

| File | Purpose |
|------|---------|
| `frontend/src/pages/sso/sso.js` | SSO callback page (`/sso` route) |
| `frontend/src/pages/sso/index.js` | Page export |
| `frontend/src/utils/auth.js` | OIDC login redirect helper |
| `frontend/src/app.js` | Add `/sso` route |

---

## Configuration Example

```yaml
# config.yaml
auth:
  type: oidc
  oidc:
    issuer_url: https://t-accounts.sesamedisk.com/
    client_id: "657640"
    client_secret: "${OIDC_CLIENT_SECRET}"

    # Allowed redirect URIs - supports multiple for different environments
    # All URIs must also be registered with the OIDC provider
    redirect_uris:
      - http://localhost:3000/sso          # Development
      - https://staging.sesamefs.com/sso   # Staging
      - https://app.sesamefs.com/sso       # Production

    scopes:
      - openid
      - profile
      - email

    # Optional: custom claims for org/role mapping
    org_claim: "tenant_id"
    roles_claim: "roles"
```

### Environment Variables

```bash
# Required
OIDC_ISSUER_URL=https://t-accounts.sesamedisk.com/
OIDC_CLIENT_ID=657640
OIDC_CLIENT_SECRET=070101ea014c91cb749221a354c49f68e6000c8c40ff11d2119599dc

# Redirect URIs (comma-separated for multiple)
OIDC_REDIRECT_URIS=http://localhost:3000/sso,https://staging.sesamefs.com/sso,https://app.sesamefs.com/sso
```

---

## Testing

### Manual Testing

```bash
# 1. Start authorization flow (redirects to OIDC provider login page)
open "https://t-accounts.sesamedisk.com/authorize?client_id=657640&response_type=code&scope=openid%20profile%20email&redirect_uri=http://localhost:3000/sso"

# 2. After login, user is redirected to http://localhost:3000/sso?code=AUTHORIZATION_CODE
# 3. Frontend sends code to backend, which exchanges it for tokens:
curl -X POST "https://t-accounts.sesamedisk.com/oauth/token" \
  -d "grant_type=authorization_code" \
  -d "client_id=657640" \
  -d "client_secret=070101ea014c91cb749221a354c49f68e6000c8c40ff11d2119599dc" \
  -d "code=AUTHORIZATION_CODE" \
  -d "redirect_uri=http://localhost:3000/sso"

# 4. Verify ID token claims
```

### SSO Endpoint Flow

```
1. User clicks "Login" → Frontend redirects to OIDC provider
2. User authenticates with OIDC provider
3. OIDC provider redirects to: http://localhost:3000/sso?code=xxx&state=yyy
4. Frontend /sso page extracts code, sends to backend API
5. Backend exchanges code for tokens, creates session
6. Backend returns session token to frontend
7. Frontend stores token, redirects to dashboard
```

### Integration Tests

Create `scripts/test-oidc.sh` to automate:
- Discovery document fetch
- Token endpoint availability
- User info endpoint
- Full login flow simulation

---

## Security Considerations

1. **Client Secret Storage**
   - Store in environment variable, NOT in code
   - Use secrets manager in production

2. **Token Storage**
   - Use HttpOnly cookies for session
   - Implement PKCE for added security

3. **Logout**
   - Implement single logout (SLO)
   - Clear local session AND OIDC session

---

## Dependencies

- Go OIDC library: `github.com/coreos/go-oidc/v3`
- OAuth2 library: `golang.org/x/oauth2`

---

## Related Documentation

- [ARCHITECTURE.md](ARCHITECTURE.md) - Overall system architecture
- [CURRENT_WORK.md](../CURRENT_WORK.md) - Current priorities
- [API-REFERENCE.md](API-REFERENCE.md) - API endpoints
