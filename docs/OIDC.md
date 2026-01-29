# OIDC Integration - SesameFS

**Last Updated**: 2026-01-29
**Status**: IMPLEMENTED - All Phases Complete (OIDC Login + Role Sync + Org Provisioning + Admin API)

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
| **Provider URL** | https://t-accounts.some-host.com/openid |
| **Client ID** | `someID` |
| **Client Secret** | `some-secret` |
| **Redirect URI (dev)** | http://localhost:3000/sso |

### Discovery Endpoint

The OIDC discovery document should be available at:
```
https://t-accounts.sesamedisk.com/openid/.well-known/openid-configuration
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

### Phase 1: Basic OIDC Login - COMPLETE

**Goal**: Replace dev token authentication with OIDC login flow
**Status**: Implemented 2026-01-28

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
   - `GET /api/v2.1/auth/oidc/config` - Returns public OIDC configuration
   - `GET /api/v2.1/auth/oidc/login` - Returns OIDC authorization URL
   - `POST /api/v2.1/auth/oidc/callback` - Exchanges code for tokens, creates session
   - `GET /api/v2.1/auth/oidc/logout` - Returns OIDC logout URL (Single Logout)
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

### Phase 2: Organization/Tenant Mapping — COMPLETE

- OIDC claims map to SesameFS organizations via configurable `org_claim`
- Auto-provisioning creates orgs on first login if they don't exist
- Platform org claim value maps superadmin users to the platform org

### Phase 3: Role Synchronization — COMPLETE

- 5-tier role hierarchy mapped from OIDC claims
- Role sync on re-login (OIDC is source of truth)
- Admin API endpoints for org/user management

See [ROLES-AND-PERMISSIONS.md](ROLES-AND-PERMISSIONS.md) for full details.

---

## OIDC Provider Requirements (What to Implement)

This section documents what the OIDC provider must emit in its tokens and how SesameFS consumes those claims. Use this as the integration spec.

### Required ID Token / UserInfo Claims

The OIDC provider must include these claims in the **ID token** (preferred) or make them available via the **userinfo endpoint**:

| Claim | Type | Required | Description |
|-------|------|----------|-------------|
| `sub` | string | **Yes** | Unique, stable user identifier. Used as the OIDC-to-SesameFS user mapping key. |
| `email` | string | **Yes** | User's email address. Used for display and as fallback identifier. |
| `name` | string | Recommended | Display name. Falls back to `preferred_username`, then email. |
| `preferred_username` | string | Optional | Used as display name fallback if `name` is absent. |
| `email_verified` | boolean | Optional | Whether email is verified. |
| **`<org_claim>`** | string | **Yes** for multi-tenant | Organization/tenant identifier. Claim name is configurable (default: `tenant_id`). See "Org Claim" below. |
| **`<roles_claim>`** | string or string[] | **Yes** for role mapping | User's role. Claim name is configurable (default: `roles`). See "Role Claim" below. |

### Org Claim (`org_claim`)

The org claim tells SesameFS which tenant/organization the user belongs to.

**Config**: `org_claim: "tenant_id"` (the claim name in the ID token)

**Value format**: The claim value is used directly as the org UUID in SesameFS, **except** when it matches the `platform_org_claim_value` — in that case it maps to the platform org UUID.

| Org Claim Value | SesameFS Behavior |
|-----------------|-------------------|
| A UUID string (e.g., `"a1b2c3d4-..."`) | Used directly as `org_id`. If org doesn't exist and `auto_provision=true`, it's created automatically. |
| The platform claim value (e.g., `"platform"`) | Maps to platform org `00000000-0000-0000-0000-000000000000`. User must also have a superadmin role. |
| Empty / missing | Falls back to `default_org_id` config, or generates a deterministic UUID from the issuer URL. |

**Example ID token payload**:
```json
{
  "sub": "user-uuid-1234",
  "email": "alice@acme.com",
  "name": "Alice Smith",
  "tenant_id": "550e8400-e29b-41d4-a716-446655440000",
  "roles": ["admin"]
}
```

**Example for a superadmin**:
```json
{
  "sub": "superadmin-uuid-5678",
  "email": "admin@sesamefs.com",
  "name": "Platform Admin",
  "tenant_id": "platform",
  "roles": ["superadmin"]
}
```

### Role Claim (`roles_claim`)

The role claim tells SesameFS what role to assign the user.

**Config**: `roles_claim: "roles"` (the claim name in the ID token)

**Accepted formats**:
- **String array**: `"roles": ["admin", "user"]` — first element is used
- **Single string**: `"roles": "admin"` — used directly

**Role mapping** (case-insensitive):

| OIDC Provider Emits | SesameFS Maps To | Access Level |
|---------------------|------------------|--------------|
| `superadmin`, `super_admin`, `platform_admin`, `SUPERADMIN` | `superadmin` | Full platform access (requires platform org claim) |
| `admin`, `administrator` | `admin` | Full tenant management |
| `user`, `member` | `user` | Standard member (create libraries, upload, share) |
| `readonly`, `read-only`, `viewer` | `readonly` | View-only (browse, download, no create/upload) |
| `guest`, `external` | `guest` | Shared content only |
| *(anything else)* | Config `default_role` (default: `user`) | Fallback |

**Important**: A `superadmin` role is only effective when the user's org claim resolves to the platform org. A user with `superadmin` role in a regular tenant org is treated as a regular superadmin but cannot access platform admin endpoints (the `RequireSuperAdmin` middleware checks both role AND org).

### Provisioning Logic (What Happens on Login)

```
User authenticates with OIDC provider
        │
        ▼
SesameFS receives ID token + userinfo
        │
        ├─ Extract org_claim value
        │   ├─ Matches platform_org_claim_value? → org_id = "00000000-...-000000000000"
        │   ├─ Valid UUID string? → org_id = that UUID
        │   └─ Empty? → org_id = default_org_id or deterministic UUID
        │
        ├─ Extract roles_claim value
        │   └─ Map first role via mapOIDCRole() → SesameFS role string
        │
        ├─ Does org exist in DB?
        │   ├─ No + auto_provision=true → CREATE org (name from config or "Auto-provisioned Organization", 1TB quota, S3 backend)
        │   └─ Yes → continue
        │
        ├─ Does user exist? (lookup via users_by_oidc table: oidc_issuer + oidc_sub)
        │   ├─ No + auto_provision=true → CREATE user (new UUID, email from claims, role from OIDC)
        │   └─ Yes → ROLE SYNC: if DB role ≠ OIDC role, UPDATE role in DB (OIDC is source of truth)
        │
        └─ Create session (JWT) → return to frontend
```

### Role Sync on Re-Login

Every time an existing user logs in, SesameFS compares the role from the OIDC token with the role stored in the database. If they differ, the DB is updated to match the OIDC claim. This means:

- **Promotions** (e.g., user → admin): Take effect on next login
- **Demotions** (e.g., admin → readonly): Take effect on next login
- **OIDC provider is the single source of truth** for roles
- The admin API can also deactivate users (sets role to `deactivated`), but OIDC re-login will overwrite this — so deactivation should also be done at the OIDC provider level

### What the OIDC Provider Must Support

| Requirement | Detail |
|-------------|--------|
| **OpenID Connect Discovery** | `/.well-known/openid-configuration` at the issuer URL |
| **Authorization Code Flow** | Standard OIDC auth code flow with `response_type=code` |
| **PKCE support** | Optional but recommended. Enabled via `require_pkce: true` in SesameFS config. |
| **Token endpoint** | Must accept `grant_type=authorization_code` with `client_id`, `client_secret`, `code`, `redirect_uri` |
| **ID token** | JWT with at minimum: `sub`, `email`, `iss`, `exp`, `iat` |
| **Custom claims** | Must include the org claim and roles claim in the ID token or userinfo response |
| **UserInfo endpoint** | Optional fallback. If ID token doesn't contain email/name, SesameFS fetches from userinfo. |
| **End Session endpoint** | Optional. If present in discovery doc, used for single logout. |
| **Stable `sub` claim** | The `sub` value must never change for a given user. It's the primary key for user mapping. |

---

## Admin API Endpoints (For Management UIs / OIDC Provider Webhooks)

These endpoints are available at `/api/v2.1/admin/` and allow managing tenants and users programmatically. The OIDC provider (or an admin dashboard) can call these to pre-create orgs, list users, etc.

### Authentication

All admin endpoints require a valid session token or dev token:
```
Authorization: Token <session_token>
```

### Organization Management (Superadmin Only)

All org endpoints require the caller to be a **superadmin in the platform org**.

#### List Organizations
```
GET /api/v2.1/admin/organizations/
```
**Response** `200`:
```json
{
  "organizations": [
    {
      "org_id": "550e8400-e29b-41d4-a716-446655440000",
      "name": "Acme Corp",
      "storage_quota": 1099511627776,
      "storage_used": 52428800,
      "settings": {"theme": "default", "features": "all"},
      "created_at": "2026-01-29T10:00:00Z"
    }
  ]
}
```

#### Create Organization
```
POST /api/v2.1/admin/organizations/
Content-Type: application/json

{
  "name": "Acme Corp",
  "storage_quota": 1099511627776
}
```
- `name` (string, **required**): Organization display name
- `storage_quota` (int64, optional): Storage quota in bytes. Default: 1TB (1099511627776)

**Response** `201`:
```json
{
  "org_id": "generated-uuid",
  "name": "Acme Corp",
  "storage_quota": 1099511627776,
  "created_at": "2026-01-29T10:00:00Z"
}
```

#### Get Organization
```
GET /api/v2.1/admin/organizations/:org_id/
```
**Response** `200`:
```json
{
  "org_id": "550e8400-...",
  "name": "Acme Corp",
  "storage_quota": 1099511627776,
  "storage_used": 52428800,
  "settings": {"theme": "default", "features": "all"},
  "created_at": "2026-01-29T10:00:00Z"
}
```

#### Update Organization
```
PUT /api/v2.1/admin/organizations/:org_id/
Content-Type: application/json

{
  "name": "Acme Corp (Renamed)",
  "storage_quota": 2199023255552
}
```
Both fields are optional. Only provided fields are updated.

**Response** `200`: `{"success": true}`

#### Deactivate Organization
```
DELETE /api/v2.1/admin/organizations/:org_id/
```
Sets `settings['status'] = 'deactivated'` on the org. The platform org (`00000000-0000-0000-0000-000000000000`) cannot be deactivated.

**Response** `200`: `{"success": true}`
**Response** `403`: `{"error": "cannot deactivate platform organization"}`

### User Management (Superadmin or Tenant Admin)

User endpoints allow **superadmin** (any org) or **tenant admin** (own org only).

#### List Org Users
```
GET /api/v2.1/admin/organizations/:org_id/users/
```
**Permission**: Superadmin for any org, tenant admin for own org only.

**Response** `200`:
```json
{
  "users": [
    {
      "user_id": "a1b2c3d4-...",
      "email": "alice@acme.com",
      "name": "Alice Smith",
      "role": "admin",
      "quota_bytes": -2,
      "used_bytes": 1048576,
      "created_at": "2026-01-29T10:00:00Z"
    }
  ]
}
```
Note: `quota_bytes = -2` means "use org default quota".

#### Get User
```
GET /api/v2.1/admin/users/:user_id/
```
**Limitation**: Superadmin gets `501 Not Implemented` (Cassandra schema requires org_id for user lookup — use the org users list endpoint instead). Tenant admin can look up users in their own org.

#### Update User
```
PUT /api/v2.1/admin/users/:user_id/
Content-Type: application/json

{
  "role": "readonly",
  "quota_bytes": 5368709120
}
```
- `role` (string, optional): New role. Valid values: `admin`, `user`, `readonly`, `guest`. Only superadmin can assign `superadmin`.
- `quota_bytes` (int64, optional): Per-user storage quota in bytes.

**Response** `200`: `{"success": true}`
**Response** `403`: `{"error": "only superadmin can assign superadmin role"}`

**Note**: Role changes via this endpoint will be overwritten on the user's next OIDC login (OIDC is source of truth). To permanently change a user's role, update it in the OIDC provider.

#### Deactivate User
```
DELETE /api/v2.1/admin/users/:user_id/
```
Sets user's role to `deactivated`. Cannot deactivate yourself.

**Response** `200`: `{"success": true}`
**Response** `400`: `{"error": "cannot deactivate your own account"}`

**Note**: Deactivated users will be re-activated on next OIDC login (role syncs from OIDC). To permanently deactivate, also disable the user in the OIDC provider.

---

## Files Created/Modified

### Backend — Phase 1 (OIDC Login)

| File | Purpose | Status |
|------|---------|--------|
| `internal/auth/oidc.go` | OIDC auth, token exchange, role mapping, org provisioning, role sync | CREATED + MODIFIED |
| `internal/auth/session.go` | Session management (JWT creation) | CREATED |
| `internal/config/config.go` | OIDC config + PlatformOrgID + DevTokenEntry.Role | MODIFIED |
| `internal/api/server.go` | Register OIDC + admin routes, dev token role context | MODIFIED |
| `internal/api/v2/auth.go` | OIDC endpoint handlers | CREATED |
| `internal/db/db.go` | Add sessions table migration | MODIFIED |

### Backend — Phase 2-4 (Roles, Admin API, Provisioning)

| File | Purpose | Status |
|------|---------|--------|
| `internal/middleware/permissions.go` | 5-tier role hierarchy, RequireSuperAdmin, PlatformOrgID | MODIFIED |
| `internal/api/v2/admin.go` | Admin API: org CRUD + user management | CREATED |
| `internal/api/v2/admin_test.go` | Admin API unit tests | CREATED |
| `internal/db/seed.go` | Platform org + superadmin user seeding | MODIFIED |
| `internal/models/models.go` | Role field comment update | MODIFIED |
| `internal/api/v2/libraries.go` | Fixed superadmin in role hierarchy | MODIFIED |
| `internal/api/v2/files.go` | Fixed superadmin in role hierarchy | MODIFIED |
| `internal/api/v2/batch_operations.go` | Fixed superadmin in role hierarchy | MODIFIED |
| `scripts/test-admin-api.sh` | Integration tests (56 assertions) | CREATED |
| `scripts/test-permissions.sh` | Superadmin permission tests | MODIFIED |

### Frontend (Phase 1)

| File | Purpose | Status |
|------|---------|--------|
| `frontend/src/pages/sso/index.js` | SSO callback page (`/sso` route) | CREATED |
| `frontend/src/pages/login/index.js` | Added SSO login button | MODIFIED |
| `frontend/src/utils/seafile-api.js` | OIDC API methods + setAuthToken | MODIFIED |
| `frontend/src/app.js` | Handle `/sso` route | MODIFIED |

---

## Configuration Example

```yaml
# config.yaml
auth:
  type: oidc
  oidc:
    issuer_url: https://t-accounts.sesamedisk.com/openid
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

    # Custom claims for org/role mapping
    org_claim: "tenant_id"              # Claim name containing org/tenant identifier
    roles_claim: "roles"                # Claim name containing user role(s)

    # Platform org configuration (for superadmin users)
    platform_org_id: "00000000-0000-0000-0000-000000000000"   # UUID for the platform org
    platform_org_claim_value: "platform"                       # When org_claim = this value, map to platform org

    # Auto-provisioning
    auto_provision: true                # Create orgs/users on first OIDC login
    default_role: "user"                # Fallback role when OIDC claim is unrecognized
    default_org_name: "New Organization" # Name for auto-provisioned orgs
```

### Environment Variables

```bash
# Required
OIDC_ISSUER_URL=https://t-accounts.sesamedisk.com/openid
OIDC_CLIENT_ID=657640
OIDC_CLIENT_SECRET=070101ea014c91cb749221a354c49f68e6000c8c40ff11d2119599dc

# Redirect URIs (comma-separated for multiple)
OIDC_REDIRECT_URIS=http://localhost:3000/sso,https://staging.sesamefs.com/sso,https://app.sesamefs.com/sso

# Platform org (optional, has defaults)
OIDC_PLATFORM_ORG_ID=00000000-0000-0000-0000-000000000000
OIDC_PLATFORM_ORG_CLAIM_VALUE=platform
```

---

## Testing

### Manual Testing

```bash
# 1. Start authorization flow (redirects to OIDC provider login page)
open "https://t-accounts.sesamedisk.com/openid/authorize?client_id=657640&response_type=code&scope=openid%20profile%20email&redirect_uri=http://localhost:3000/sso"

# 2. After login, user is redirected to http://localhost:3000/sso?code=AUTHORIZATION_CODE
# 3. Frontend sends code to backend, which exchanges it for tokens:
curl -X POST "https://t-accounts.sesamedisk.com/openid/token" \
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

3. **Logout** ✅ IMPLEMENTED
   - Single Logout (SLO) via OIDC `end_session_endpoint`
   - Clears local session AND redirects to OIDC provider logout
   - Provider logout endpoint: `https://t-accounts.sesamedisk.com/openid/end-session`

---

## Dependencies

- Go OIDC library: `github.com/coreos/go-oidc/v3`
- OAuth2 library: `golang.org/x/oauth2`

---

## Related Documentation

- [ROLES-AND-PERMISSIONS.md](ROLES-AND-PERMISSIONS.md) - Full role hierarchy, permission matrix, implementation status
- [ARCHITECTURE.md](ARCHITECTURE.md) - Overall system architecture
- [CURRENT_WORK.md](../CURRENT_WORK.md) - Current priorities
- [API-REFERENCE.md](API-REFERENCE.md) - API endpoints
- [ENDPOINT-REGISTRY.md](ENDPOINT-REGISTRY.md) - Complete route registry
