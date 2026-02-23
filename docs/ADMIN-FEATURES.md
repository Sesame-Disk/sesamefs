# Admin Features — Library Management, Link Management, Audit Logs

**Last Updated**: 2026-02-23
**Status**: Library Management ✅ DONE, Link Management ✅ DONE, Sharing Stubs ✅ DONE, Org Management ✅ DONE, Audit Logs pending

---

## Overview

Three admin feature areas needed for production. The OIDC provider manages users, groups, departments, and tenants — so the SesameFS admin panel focuses on **storage-specific management** that only SesameFS can provide.

| Feature | Backend | Frontend | Database | Priority |
|---------|---------|----------|----------|----------|
| Admin Library Management | ✅ Complete (2026-02-12) | ✅ Exists | ✅ Exists | DONE |
| Admin Share Link & Upload Link Management | ✅ Complete (2026-02-12) | ✅ Exists | ✅ Exists | DONE |
| Admin Organization Management | ✅ Complete (2026-02-23) | ✅ Exists | ✅ Exists | DONE |
| Audit Logs | 🟡 Stub only | ✅ Exists (unused) | ❌ Missing | MEDIUM |

---

## 0. Superadmin Bootstrap — ✅ IMPLEMENTED (2026-02-23)

### The Problem

`RequireSuperAdmin()` middleware requires BOTH:
1. `role == "superadmin"`
2. `org_id == "00000000-0000-0000-0000-000000000000"` (platform org)

OIDC users are provisioned into their tenant org, not the platform org. Even with `OIDC_DEFAULT_ROLE=superadmin`, they get 403 on org management endpoints.

### Solution A: FIRST_SUPERADMIN_EMAIL (recommended for fresh deploys)

Set in `.env` **before first deploy**:
```bash
FIRST_SUPERADMIN_EMAIL=you@yourdomain.com
```

On first startup, the seed creates a superadmin in the platform org with this email.
When the user logs in via OIDC, they are matched by email and enter as superadmin.
The seed only runs once — changing this value after the first deploy has no effect.

### Solution B: make-superadmin.sh (for existing deploys)

**File**: `scripts/make-superadmin.sh`

Directly writes to Cassandra to place a user in the platform org with superadmin role.

```bash
# Dev/test (docker-compose.yaml):
./scripts/make-superadmin.sh your@email.com "Your Name"

# Production (docker-compose.prod.yml):
./scripts/make-superadmin.sh -f docker-compose.prod.yml your@email.com "Your Name"
```

Run from the project root. The script uses `docker compose exec cassandra cqlsh` internally — no DB credentials needed unless Cassandra auth is enabled (pass `--username`/`--password` or set `CASSANDRA_USERNAME`/`CASSANDRA_PASSWORD` env vars).

**What it does:**
1. Looks up user by email in `users_by_email` (reuses existing user_id if found)
2. Upserts user record in platform org with `role=superadmin`, unlimited quota
3. Updates `users_by_email` to map email → platform org
4. Updates `users_by_oidc` to map OIDC subject → platform org
5. Invalidates existing sessions so new role takes effect on next login

### Organization Management Endpoints

| Method | Endpoint | Handler | Auth Required |
|--------|----------|---------|---------------|
| GET | `/admin/organizations/` | `ListOrganizations` | admin or superadmin (read-only for admin) |
| POST | `/admin/organizations/` | `CreateOrganization` | **superadmin only** |
| GET | `/admin/organizations/:org_id/` | `GetOrganization` | admin or superadmin |
| PUT | `/admin/organizations/:org_id/` | `UpdateOrganization` | **superadmin only** |
| DELETE | `/admin/organizations/:org_id/` | `DeactivateOrganization` | **superadmin only** |
| GET | `/admin/organizations/:org_id/users/` | `ListOrgUsers` | admin or superadmin |
| POST | `/admin/organizations/:org_id/users/` | `AdminAddOrgUser` | admin or superadmin |
| PUT | `/admin/organizations/:org_id/users/:email/` | `AdminUpdateOrgUser` | admin or superadmin |
| DELETE | `/admin/organizations/:org_id/users/:email/` | `AdminDeleteOrgUser` | admin or superadmin |

### DeactivateOrganization — ⚠️ INCOMPLETE (2026-02-23)

**Current behavior:** The `DELETE /admin/organizations/:org_id/` endpoint does NOT delete the organization from the database. It only sets `settings['status'] = 'deactivated'` (soft-deactivation via map column update).

**Known issues:**
1. `ListOrganizations` does NOT filter out deactivated orgs — they still appear in the list
2. Deactivated orgs are still fully functional (users can still log in, access libraries, etc.)
3. No `deleted_at` / `deleted_by` columns like the library soft-delete pattern
4. No cascade handling (users, libraries, shares of the deactivated org remain active)

**TODO — Choose one approach:**
- **Option A (Hard delete):** Change to `DELETE FROM organizations WHERE org_id = ?` + cascade cleanup of related data (users, libraries, shares, etc.)
- **Option B (Proper soft-delete):** Add `deleted_at` / `deleted_by` columns (matching library pattern), filter deactivated orgs from all queries, and block login for users of deactivated orgs

---

### CreateOrganization — seafile-js Compatibility (2026-02-23)

The frontend `sysAdminAddOrg(orgName, ownerEmail, password)` (seafile-js) sends FormData.
Backend now accepts both formats:

**FormData** (seafile-js native):
```
org_name=Acme Corp
owner_email=alice@acme.com
password=ignored  ← accepted but not used (OIDC-only system)
```

**JSON** (direct API calls):
```json
{ "name": "Acme Corp", "owner_email": "alice@acme.com", "storage_quota": 1099511627776 }
```

If `owner_email` is provided, an admin user is created in the new org (dual-write to
`users` + `users_by_email` with `IF NOT EXISTS` to avoid overwriting existing OIDC sessions).

---

## 1. Admin Library Management — ✅ IMPLEMENTED (2026-02-12)

### Implementation

- **File**: `internal/api/v2/admin.go` — Phase 3 section (after user/group admin endpoints)
- **Frontend API**: `frontend/src/utils/seafile-api.js` — all `sysAdmin*Repo*` methods wired
- **Frontend pages**: `frontend/src/pages/sys-admin/repos/` — `all-repos.js`, `search-repos.js`, `repos.js`, `trash-repos.js`, `dir-view.js`

### Endpoints Implemented

| Method | Endpoint | Handler | Status |
|--------|----------|---------|--------|
| GET | `/admin/libraries/` | `AdminListAllLibraries` | ✅ `?page=&per_page=&order_by=` |
| GET | `/admin/search-libraries/` | `AdminSearchLibraries` | ✅ `?name_or_id=&page=&per_page=` |
| GET | `/admin/libraries/:library_id/` | `AdminGetLibrary` | ✅ |
| POST | `/admin/libraries/` | `AdminCreateLibrary` | ✅ JSON `{name, owner}` |
| DELETE | `/admin/libraries/:library_id/` | `AdminDeleteLibrary` | ✅ Soft-delete |
| PUT | `/admin/libraries/:library_id/transfer/` | `AdminTransferLibrary` | ✅ JSON `{owner}` |
| GET | `/admin/libraries/:library_id/dirents/` | `AdminListDirents` | ✅ `?path=` |
| GET | `/admin/libraries/:library_id/history-setting/` | `AdminGetHistorySetting` | ✅ |
| PUT | `/admin/libraries/:library_id/history-setting/` | `AdminUpdateHistorySetting` | ✅ JSON `{keep_days}` |
| GET | `/admin/libraries/:library_id/shared-items/` | `AdminListSharedItems` | ✅ `?share_type=user\|group` |
| GET | `/admin/trash-libraries/` | `AdminListTrashLibraries` | ✅ `?page=&per_page=&owner=` |

### Key Design Decisions

- **No permission filter**: Admin sees ALL libraries in their org; superadmin sees ALL libraries across all orgs
- **Library lookup**: `libraries` table (partitioned by `org_id`) for listing, `libraries_by_id` for single lookups
- **Transfer**: Dual-write to `libraries` + `libraries_by_id` tables
- **Search**: Application-level case-insensitive substring match + ID prefix match
- **Delete**: Soft-delete via `deleted_at` / `deleted_by` columns (same pattern as user delete)
- **JSON + FormData**: Create and transfer endpoints accept both content types

---

## 2. Admin Share Link & Upload Link Management — ✅ IMPLEMENTED (2026-02-12)

### Implementation

- **Admin share/upload handlers**: `internal/api/v2/admin_extra.go` — Fixed AdminListShareLinks (correct column names, repo_name resolution, creator info, sorting), AdminDeleteShareLink (dual-delete from both tables), implemented AdminListUploadLinks, AdminDeleteUploadLink, AdminListUserShareLinks, AdminListUserUploadLinks
- **User upload link CRUD**: `internal/api/v2/upload_links.go` — NEW file with ListUploadLinks, CreateUploadLink, DeleteUploadLink, ListRepoUploadLinks
- **Database**: `internal/db/db.go` — Added `upload_links` + `upload_links_by_creator` tables with migration
- **Route registration**: `internal/api/server.go` — Added `RegisterUploadLinkRoutes`
- **Frontend API**: `frontend/src/utils/seafile-api.js` — Added 6 sysAdmin methods

### What Was Built

#### Admin Share Link Endpoints — ✅ DONE

| Method | Endpoint | Handler | seafile-js method | Status |
|--------|----------|---------|-------------------|--------|
| GET | `/admin/share-links/` | `AdminListShareLinks` | `sysAdminListShareLinks` | ✅ `?page=&per_page=&order_by=&direction=` |
| DELETE | `/admin/share-links/:token/` | `AdminDeleteShareLink` | `sysAdminDeleteShareLink` | ✅ Dual-delete from share_links + share_links_by_creator |

#### Upload Links — Full Feature (User + Admin) — ✅ DONE

**Database tables created** (in `internal/db/db.go`):
```cql
CREATE TABLE IF NOT EXISTS upload_links (
    upload_token TEXT PRIMARY KEY,
    org_id UUID,
    library_id UUID,
    file_path TEXT,
    created_by UUID,
    password_hash TEXT,
    expires_at TIMESTAMP,
    created_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS upload_links_by_creator (
    org_id UUID,
    created_by UUID,
    upload_token TEXT,
    library_id UUID,
    file_path TEXT,
    expires_at TIMESTAMP,
    created_at TIMESTAMP,
    PRIMARY KEY ((org_id, created_by), upload_token)
);
```

**User endpoints** (file `internal/api/v2/upload_links.go`):

| Method | Endpoint | Handler | Status |
|--------|----------|---------|--------|
| GET | `/api/v2.1/upload-links/` | `ListUploadLinks` | ✅ User's own upload links (optional `?repo_id=` filter) |
| POST | `/api/v2.1/upload-links/` | `CreateUploadLink` | ✅ Creates token, dual-writes, optional password+expiry |
| DELETE | `/api/v2.1/upload-links/:token/` | `DeleteUploadLink` | ✅ Verifies ownership, dual-deletes |
| GET | `/api/v2.1/repos/:repo_id/upload-links/` | `ListRepoUploadLinks` | ✅ List upload links for specific repo |

**Admin endpoints** (in `internal/api/v2/admin_extra.go`):

| Method | Endpoint | Handler | seafile-js method | Status |
|--------|----------|---------|-------------------|--------|
| GET | `/admin/upload-links/` | `AdminListUploadLinks` | `sysAdminListAllUploadLinks` | ✅ |
| DELETE | `/admin/upload-links/:token/` | `AdminDeleteUploadLink` | `sysAdminDeleteUploadLink` | ✅ |

#### Admin Per-User Link Endpoints — ✅ DONE

| Method | Endpoint | Handler | seafile-js method | Status |
|--------|----------|---------|-------------------|--------|
| GET | `/admin/users/:email/share-links/` | `AdminListUserShareLinks` | `sysAdminListShareLinksByUser` | ✅ Resolves email→user_id, queries share_links_by_creator |
| GET | `/admin/users/:email/upload-links/` | `AdminListUserUploadLinks` | `sysAdminListUploadLinksByUser` | ✅ Resolves email→user_id, queries upload_links_by_creator |

### Response Formats

**AdminListShareLinks**:
```json
{
  "share_link_list": [
    {
      "token": "abc123",
      "repo_id": "uuid",
      "repo_name": "Library Name",
      "path": "/path/to/file.pdf",
      "creator_email": "user@example.com",
      "creator_name": "User Name",
      "ctime": "2026-01-01T00:00:00Z",
      "expire_date": "2026-02-01T00:00:00Z",
      "is_expired": false,
      "view_cnt": 5,
      "permissions": {"can_download": true, "can_edit": false}
    }
  ],
  "page_info": {"has_next_page": false, "current_page": 1}
}
```

### Implementation Notes

- **Admin share link listing**: Queries `share_links` table (full scan) with application-level pagination. Resolves repo names from `libraries` table (not `libraries_by_id` which lacks `name` column). Caches user lookups to avoid N+1 queries.
- **Admin delete**: Uses read-first-then-batch-delete pattern — reads `created_by`+`org_id` from `share_links`/`upload_links`, then issues `gocql.LoggedBatch` to delete from both primary and lookup tables.
- **Upload links**: Full feature implemented — DB tables auto-created via migration, user CRUD with dual-write pattern, admin list/delete with same batch-delete pattern.
- **Future**: Upload handler at `/seafhttp/upload-api/:token` should also accept upload link tokens for anonymous file uploads via link.

---

## 3. Audit Logs

### What Exists

- **Middleware**: `internal/middleware/audit.go` — defines action types and basic structure, but **only logs to console** (no database persistence)
- **Stub endpoint**: `GET /activities` returns empty `{"events": []}` (in `internal/api/server.go:1173`)
- **Frontend pages**: Full UI in `frontend/src/pages/sys-admin/logs-page/` — `login-logs.js`, `file-access-logs.js`, `file-update-logs.js`, `share-permission-logs.js`
- **Dashboard**: `frontend/src/pages/dashboard/files-activities.js` — calls `seafileAPI.listActivities()`
- **seafile-js methods**: `sysAdminListLoginLogs`, `sysAdminListFileAccessLogs`, `sysAdminListFileUpdateLogs`, `sysAdminListSharePermissionLogs`, `sysAdminListAdminLogs`, `listActivities`
- **API-REFERENCE.md**: Documents planned `activities` table schema (line 627) but never implemented

### What's Missing — Everything

#### Database Tables

5 new Cassandra tables needed (all use TIMEUUID clustering for time-ordered queries):

```cql
-- 1. Login logs
CREATE TABLE login_logs (
    org_id UUID,
    log_id TIMEUUID,
    user_id UUID,
    email TEXT,
    name TEXT,
    login_ip TEXT,
    user_agent TEXT,
    success BOOLEAN,
    created_at TIMESTAMP,
    PRIMARY KEY ((org_id), log_id)
) WITH CLUSTERING ORDER BY (log_id DESC)
  AND default_time_to_live = 7776000;  -- 90-day TTL

-- 2. File access logs (downloads, previews)
CREATE TABLE file_access_logs (
    org_id UUID,
    log_id TIMEUUID,
    user_id UUID,
    email TEXT,
    name TEXT,
    repo_id UUID,
    repo_name TEXT,
    file_path TEXT,
    event_type TEXT,  -- 'download', 'preview', 'view'
    ip TEXT,
    created_at TIMESTAMP,
    PRIMARY KEY ((org_id), log_id)
) WITH CLUSTERING ORDER BY (log_id DESC)
  AND default_time_to_live = 7776000;

-- 3. File update logs (create, edit, delete, move, rename)
CREATE TABLE file_update_logs (
    org_id UUID,
    log_id TIMEUUID,
    user_id UUID,
    email TEXT,
    name TEXT,
    repo_id UUID,
    repo_name TEXT,
    file_path TEXT,
    operation TEXT,  -- 'create', 'edit', 'delete', 'move', 'rename'
    detail TEXT,     -- e.g. "Renamed from old.txt to new.txt"
    created_at TIMESTAMP,
    PRIMARY KEY ((org_id), log_id)
) WITH CLUSTERING ORDER BY (log_id DESC)
  AND default_time_to_live = 7776000;

-- 4. Permission/share audit logs
CREATE TABLE permission_audit_logs (
    org_id UUID,
    log_id TIMEUUID,
    user_id UUID,
    email TEXT,
    name TEXT,
    repo_id UUID,
    repo_name TEXT,
    operation TEXT,  -- 'share-library', 'unshare-library', 'create-share-link', 'delete-share-link'
    target TEXT,     -- email, group name, or link token
    target_type TEXT, -- 'user', 'group', 'link'
    permission TEXT,  -- 'r', 'rw', 'admin'
    created_at TIMESTAMP,
    PRIMARY KEY ((org_id), log_id)
) WITH CLUSTERING ORDER BY (log_id DESC)
  AND default_time_to_live = 7776000;

-- 5. Activities feed (aggregated for dashboard)
CREATE TABLE activities (
    org_id UUID,
    activity_id TIMEUUID,
    user_id UUID,
    author_email TEXT,
    author_name TEXT,
    repo_id UUID,
    repo_name TEXT,
    path TEXT,
    obj_type TEXT,   -- 'file', 'dir', 'repo'
    op_type TEXT,    -- 'create', 'edit', 'delete', 'rename', 'move', 'recover'
    old_path TEXT,
    created_at TIMESTAMP,
    PRIMARY KEY ((org_id), activity_id)
) WITH CLUSTERING ORDER BY (activity_id DESC)
  AND default_time_to_live = 7776000;
```

#### Backend Endpoints

**Admin log endpoints** (new file `internal/api/v2/audit.go`):

| Method | Endpoint | Handler | seafile-js method | Notes |
|--------|----------|---------|-------------------|-------|
| GET | `/admin/logs/login/` | `ListLoginLogs` | `sysAdminListLoginLogs` | `?page=&per_page=` |
| GET | `/admin/logs/file-access/` | `ListFileAccessLogs` | `sysAdminListFileAccessLogs` | `?page=&per_page=&email=&repo_id=` |
| GET | `/admin/logs/file-update/` | `ListFileUpdateLogs` | `sysAdminListFileUpdateLogs` | `?page=&per_page=` |
| GET | `/admin/logs/share-permission/` | `ListPermissionAuditLogs` | `sysAdminListSharePermissionLogs` | `?page=&per_page=` |

**User activity endpoint** (replace stub in `server.go`):

| Method | Endpoint | Handler | seafile-js method | Notes |
|--------|----------|---------|-------------------|-------|
| GET | `/api/v2.1/activities/` | `ListActivities` | `listActivities` | `?page=&per_page=` — user's dashboard feed |

#### Logging Integration Points

Where to insert audit log writes in existing handlers:

| Event | File | Handler | Log Table |
|-------|------|---------|-----------|
| Login (OIDC) | `internal/auth/oidc.go` | `provisionUser()` | `login_logs` |
| Login (dev token) | `internal/middleware/auth.go` | auth middleware | `login_logs` |
| File download | `internal/api/seafhttp.go` | `HandleDownload()` | `file_access_logs` |
| File preview/view | `internal/api/v2/files.go` | `GetFileDetail()` | `file_access_logs` |
| File upload | `internal/api/seafhttp.go` | upload handler | `file_update_logs` + `activities` |
| File create | `internal/api/v2/files.go` | `CreateFile()` | `file_update_logs` + `activities` |
| File delete | `internal/api/v2/files.go` | `DeleteFile()` | `file_update_logs` + `activities` |
| File rename | `internal/api/v2/files.go` | `RenameFile()` | `file_update_logs` + `activities` |
| File move/copy | `internal/api/v2/files.go` | move/copy handlers | `file_update_logs` + `activities` |
| Share to user/group | `internal/api/v2/file_shares.go` | share handlers | `permission_audit_logs` |
| Create share link | `internal/api/v2/share_links.go` | `CreateShareLink()` | `permission_audit_logs` |
| Delete share link | `internal/api/v2/share_links.go` | `DeleteShareLink()` | `permission_audit_logs` |
| Library create | `internal/api/v2/libraries.go` | `CreateLibrary()` | `activities` |
| Library delete | `internal/api/v2/libraries.go` | `DeleteLibrary()` | `activities` |

#### Response Formats

**ListLoginLogs**:
```json
{
  "login_log_list": [
    {
      "email": "user@example.com",
      "name": "User Name",
      "login_ip": "192.168.1.1",
      "login_time": "2026-02-01T10:30:00Z",
      "login_success": true
    }
  ],
  "total_count": 150,
  "page_info": {"has_next_page": true, "current_page": 1}
}
```

**ListActivities** (dashboard feed):
```json
{
  "events": [
    {
      "author_email": "user@example.com",
      "author_name": "User Name",
      "author_contact_email": "user@example.com",
      "avatar_url": "",
      "repo_id": "uuid",
      "repo_name": "My Library",
      "obj_type": "file",
      "op_type": "edit",
      "path": "/documents/report.docx",
      "time": "2026-02-01T14:30:00Z"
    }
  ]
}
```

### Implementation Notes

- **Async writes**: Audit log writes should NOT block request handling. Use a buffered channel + goroutine worker to write logs asynchronously.
- **TTL**: All tables use 90-day TTL by default. Make configurable via `config.yaml`.
- **Update existing middleware**: `internal/middleware/audit.go` already defines action types — refactor to use DB persistence instead of console logging.
- **Performance**: TIMEUUID clustering gives natural time ordering. Cassandra handles high write throughput well.

---

## Implementation Order

1. ~~**Admin Library Management**~~ — ✅ DONE (2026-02-12)
2. ~~**Admin Share Link & Upload Link Management**~~ — ✅ DONE (2026-02-12)
3. **Audit Logs** — Largest scope (5 tables, integration across ~15 handlers), medium priority

---

## Related Documentation

- [IMPLEMENTATION_STATUS.md](IMPLEMENTATION_STATUS.md) — Component stability matrix
- [ENDPOINT-REGISTRY.md](ENDPOINT-REGISTRY.md) — Route registry (update when implementing)
- [API-REFERENCE.md](API-REFERENCE.md) — API documentation (update when implementing)
- [DATABASE-GUIDE.md](DATABASE-GUIDE.md) — Cassandra schema reference
- [ROLES-AND-PERMISSIONS.md](ROLES-AND-PERMISSIONS.md) — Admin role requirements
- [OIDC.md](OIDC.md) — OIDC manages users/groups/tenants; SesameFS admin handles storage
- Existing admin code: `internal/api/v2/admin.go` (group + user admin endpoints)
- Existing share links: `internal/api/v2/share_links.go`
- Existing audit middleware: `internal/middleware/audit.go`
- Frontend admin pages: `frontend/src/pages/sys-admin/`
