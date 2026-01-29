# Roles & Permissions - SesameFS

**Last Updated**: 2026-01-29
**Status**: IMPLEMENTED — All phases complete (superadmin role, admin API, OIDC org provisioning, role sync)

---

## Overview

SesameFS uses a **three-layer permission model**:

1. **Platform level** — Super admins manage all tenants (cross-org)
2. **Organization level** — Tenant admins and users operate within their org
3. **Resource level** — Library permissions, group roles, share links

Users are provisioned exclusively through the **OIDC provider**. SesameFS does not create users directly — it auto-provisions on first login and syncs roles from OIDC claims.

---

## Role Hierarchy

```
superadmin          ← Platform-level (dedicated platform org)
  └── admin         ← Tenant-level (org-scoped)
      └── user      ← Regular tenant member
          └── readonly  ← View-only tenant member
              └── guest     ← Limited to shared content only
```

### Role Definitions

| Role | Scope | Description |
|------|-------|-------------|
| `superadmin` | **Platform** | Manages all tenants. Belongs to the platform org (`00000000-0000-0000-0000-000000000000`). Can list/create/deactivate organizations, view any tenant's data, impersonate tenant admins for support. Cannot directly access tenant files — must switch into tenant context. |
| `admin` | **Tenant** | Full control within their organization. Can manage users (view, deactivate), create/delete libraries, manage groups, configure org settings, view audit logs. Cannot access other tenants. |
| `user` | **Tenant** | Regular member. Can create libraries, upload/download files, create groups, share content with other members. Standard quota applies. |
| `readonly` | **Tenant** | View-only access. Can browse libraries shared with them, download files, but cannot upload, create libraries, share, or modify content. |
| `guest` | **Tenant** | Most restricted. Can only access content explicitly shared with them. Cannot browse org libraries, create anything, or share with others. Intended for external collaborators. |

### Hierarchy Value Map (for code)

```go
roleHierarchy := map[OrganizationRole]int{
    RoleSuperAdmin: 4,  // NEW
    RoleAdmin:      3,
    RoleUser:       2,
    RoleReadOnly:   1,
    RoleGuest:      0,
}
```

---

## Platform Org (Super Admin)

Super admins use a **dedicated platform organization**:

| Property | Value |
|----------|-------|
| **Org ID** | `00000000-0000-0000-0000-000000000000` |
| **Org Name** | `SesameFS Platform` |
| **Purpose** | Houses super admin users only |

### Design Rationale

- **Clean isolation**: Super admins don't "leak" into tenant data. They exist outside the tenant model.
- **No special-case boolean**: Avoids `is_super_admin` flag scattered through the codebase. The role field handles it.
- **Existing schema works**: The `users` table already partitions by `(org_id, user_id)`. Platform org is just another org_id.
- **Auditable**: All super admin actions can be filtered by platform org_id.

### Cross-Tenant Access Pattern

Super admins access tenant data by providing a **target org_id** in API requests:

```
GET /api/v2/admin/organizations/                        # List all orgs
GET /api/v2/admin/organizations/{org_id}/               # View org details
GET /api/v2/admin/organizations/{org_id}/users/         # List org users
GET /api/v2/admin/organizations/{org_id}/libraries/     # List org libraries
PUT /api/v2/admin/organizations/{org_id}/               # Update org settings
DELETE /api/v2/admin/organizations/{org_id}/             # Deactivate org
```

Super admins **cannot** use regular `/api/v2/repos/` endpoints (they have no tenant context). They must use the `/api/v2/admin/` prefix which explicitly requires a target org.

---

## Permission Matrix

### Organization-Level Operations

| Operation | superadmin | admin | user | readonly | guest |
|-----------|:----------:|:-----:|:----:|:--------:|:-----:|
| List all organizations | ✅ | ❌ | ❌ | ❌ | ❌ |
| Create organization | ✅ | ❌ | ❌ | ❌ | ❌ |
| Deactivate organization | ✅ | ❌ | ❌ | ❌ | ❌ |
| View org settings | ✅ | ✅ | ❌ | ❌ | ❌ |
| Modify org settings | ✅ | ✅ | ❌ | ❌ | ❌ |
| List org users | ✅ | ✅ | ❌ | ❌ | ❌ |
| Deactivate org user | ✅ | ✅ | ❌ | ❌ | ❌ |
| Change user role (within org) | ✅ | ✅ | ❌ | ❌ | ❌ |
| View org audit log | ✅ | ✅ | ❌ | ❌ | ❌ |

### Library Operations

| Operation | admin | user | readonly | guest |
|-----------|:-----:|:----:|:--------:|:-----:|
| Create library | ✅ | ✅ | ❌ | ❌ |
| Delete own library | ✅ | ✅ | ❌ | ❌ |
| Delete any org library | ✅ | ❌ | ❌ | ❌ |
| Upload files | ✅ | ✅ | ❌ | ❌ |
| Download files (own/shared) | ✅ | ✅ | ✅ | ✅* |
| Browse org libraries | ✅ | ✅ | ✅ | ❌ |
| Browse shared libraries | ✅ | ✅ | ✅ | ✅ |
| Share library with user/group | ✅ | ✅ | ❌ | ❌ |
| Create share link | ✅ | ✅ | ❌ | ❌ |
| Set library password (encrypted) | ✅ | ✅ | ❌ | ❌ |

\* Guest can only download from libraries explicitly shared with them.

### Group Operations

| Operation | admin | user | readonly | guest |
|-----------|:-----:|:----:|:--------:|:-----:|
| Create group | ✅ | ✅ | ❌ | ❌ |
| Join group (if allowed) | ✅ | ✅ | ✅ | ❌ |
| Manage group members | ✅ | owner/admin | ❌ | ❌ |
| Delete group | ✅ | owner only | ❌ | ❌ |

---

## OIDC Role Provisioning

### How Roles Flow from OIDC

```
OIDC Provider (source of truth)
    │
    │  Custom claim: "roles" (configurable via roles_claim)
    │  Custom claim: "tenant_id" (configurable via org_claim)
    │
    ▼
SesameFS OIDC Client (internal/auth/oidc.go)
    │
    │  extractRoles() → reads claim from ID token or userinfo
    │  mapOIDCRole()  → maps provider role string to SesameFS role
    │  extractOrgID() → determines which org the user belongs to
    │
    ▼
provisionUser()
    │
    ├─ User exists?  → Update role if OIDC role differs (role sync)
    └─ New user?     → Create user record with mapped role
         │
         ├─ Org exists?  → Assign to existing org
         └─ New org?     → Auto-create org (if AutoProvision enabled)
```

### OIDC Role Mapping

The OIDC provider sends role strings in a custom claim. SesameFS maps them:

| OIDC Claim Value | SesameFS Role | Notes |
|------------------|---------------|-------|
| `superadmin`, `super_admin`, `platform_admin` | `superadmin` | **Must also have platform org claim** |
| `admin`, `administrator`, `tenant_admin` | `admin` | Org-scoped admin |
| `user`, `member` | `user` | Regular member |
| `readonly`, `read-only`, `viewer` | `readonly` | View-only |
| `guest`, `external` | `guest` | Limited access |
| *(anything else)* | `DefaultRole` config value | Fallback |

### Super Admin Provisioning via OIDC

Super admins are provisioned the same way as other users, but with two requirements:

1. **Role claim** must map to `superadmin`
2. **Org claim** must resolve to the platform org ID (`00000000-0000-0000-0000-000000000000`)

The OIDC provider should be configured to assign the platform org claim value to super admin users. SesameFS config maps this:

```yaml
auth:
  oidc:
    org_claim: "tenant_id"
    roles_claim: "roles"
    platform_org_claim_value: "platform"  # NEW: when org claim = "platform", maps to platform org ID
```

### Organization Auto-Provisioning (IMPLEMENTED)

When an OIDC user logs in with an org claim value that doesn't match any existing organization:

1. SesameFS creates a new `organizations` record with defaults (1TB quota, S3 backend)
2. Assigns default settings (quota, storage config, chunking polynomial)
3. Users get the role from their OIDC claim (not auto-promoted)
4. Subsequent users join the existing org

**Config option**: `allowed_org_claims` restricts which org values are accepted (empty = allow all).

---

## Implementation Status

All phases are complete. Modified/created files:

| Component | File | Status |
|-----------|------|--------|
| Role constants (5-tier including `superadmin`) | `internal/middleware/permissions.go:24-30` | ✅ Complete |
| Role hierarchy check (levels 0-4) | `internal/middleware/permissions.go:354-366` | ✅ Complete |
| `RequireOrgRole()` middleware | `internal/middleware/permissions.go:74-104` | ✅ Complete |
| `RequireSuperAdmin()` middleware | `internal/middleware/permissions.go:312-347` | ✅ Complete |
| `RequireAdminOrAbove()` middleware | `internal/middleware/permissions.go:350-352` | ✅ Complete |
| Library permission system | `internal/middleware/permissions.go:106-424` | ✅ Complete |
| Group role system | `internal/middleware/permissions.go:171-391` | ✅ Complete |
| OIDC role extraction + superadmin mapping | `internal/auth/oidc.go:612-658` | ✅ Complete |
| OIDC org claim + platform org mapping | `internal/auth/oidc.go:581-610` | ✅ Complete |
| OIDC org auto-provisioning | `internal/auth/oidc.go:451-478` | ✅ Complete |
| OIDC role sync on re-login | `internal/auth/oidc.go:521-532` | ✅ Complete |
| Admin API endpoints (org + user CRUD) | `internal/api/v2/admin.go` | ✅ Complete |
| Admin API tests | `internal/api/v2/admin_test.go` | ✅ Complete |
| Platform org + superadmin seeding | `internal/db/seed.go` | ✅ Complete |
| Config: PlatformOrgID, PlatformOrgClaimValue, DevTokenEntry.Role | `internal/config/config.go` | ✅ Complete |
| Route registration + dev token role | `internal/api/server.go` | ✅ Complete |
| Integration tests | `scripts/test-admin-api.sh`, `scripts/test-permissions.sh` | ✅ Complete |

---

## Database Schema (No Changes Required)

The existing schema already supports the role model:

```sql
-- Users table: role field stores any role string
CREATE TABLE users (
    org_id UUID,
    user_id UUID,
    email TEXT,
    name TEXT,
    role TEXT,          -- "superadmin", "admin", "user", "readonly", "guest"
    ...
    PRIMARY KEY ((org_id), user_id)
);

-- Sessions: role cached for fast auth
CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    user_id UUID,
    org_id UUID,
    email TEXT,
    role TEXT,          -- Cached from users table
    ...
);

-- Organizations: no schema change needed
CREATE TABLE organizations (
    org_id UUID PRIMARY KEY,
    name TEXT,
    ...
);
```

The `role` column is `TEXT` — no migration needed to add `superadmin`. The platform org is just another row in `organizations`.

---

## Frontend Implications

The Seafile frontend uses `window.app.pageOptions` flags from the backend:

| Flag | Set When |
|------|----------|
| `canAddRepo` | role >= `user` |
| `canShareRepo` | role >= `user` |
| `canAddGroup` | role >= `user` |
| `canGenerateShareLink` | role >= `user` |
| `canInvitePeople` | role = `admin` |
| `isStaff` | role = `admin` or `superadmin` |
| `isPlatformAdmin` | role = `superadmin` (NEW) |

The super admin frontend experience will need a **separate admin panel** (or admin section) that shows the multi-tenant management UI. This is outside the regular Seafile frontend.

---

## Security Considerations

1. **Super admin cannot directly browse tenant files** — They use admin API endpoints, not regular file endpoints. This prevents accidental data exposure.
2. **OIDC is source of truth** — Roles cannot be changed through SesameFS API alone. The admin endpoints can deactivate users but not promote them. Role changes come from OIDC.
3. **Platform org isolation** — The platform org should never contain libraries or file data. It exists only for user records.
4. **Audit logging** — All admin API calls should be logged with the acting super admin's identity.
5. **Session role caching** — When a role changes via OIDC re-login, existing sessions still carry the old role until they expire. Consider invalidating sessions on role change.

---

## Related Documentation

- [OIDC.md](OIDC.md) — OIDC provider config, login flow, claim mapping
- [ARCHITECTURE.md](ARCHITECTURE.md) — Multi-tenancy model, org_id partitioning
- [ENDPOINT-REGISTRY.md](ENDPOINT-REGISTRY.md) — API route registry (add admin routes here)
- [IMPLEMENTATION_STATUS.md](IMPLEMENTATION_STATUS.md) — Component status tracking
- `internal/middleware/permissions.go` — Permission middleware code
- `internal/auth/oidc.go` — OIDC provisioning code
