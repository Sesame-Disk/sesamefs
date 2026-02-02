# Admin Features — Library Management, Link Management, Audit Logs

**Last Updated**: 2026-02-02
**Status**: Specification — implementation pending

---

## Overview

Three admin feature areas needed for production. The OIDC provider manages users, groups, departments, and tenants — so the SesameFS admin panel focuses on **storage-specific management** that only SesameFS can provide.

| Feature | Backend | Frontend | Database | Priority |
|---------|---------|----------|----------|----------|
| Admin Library Management | ❌ Missing | ✅ Exists (unused) | ✅ Exists | HIGH |
| Admin Share Link Management | ❌ Missing | ✅ Exists (unused) | ✅ Exists (share) / ❌ Missing (upload) | HIGH |
| Audit Logs | 🟡 Stub only | ✅ Exists (unused) | ❌ Missing | MEDIUM |

---

## 1. Admin Library Management

### What Exists

- **Database**: `libraries` table with SASI index on `name` for search, `libraries_by_id` lookup table
- **User endpoints**: Full CRUD at `/api2/repos/` and `/api/v2.1/repos/` (permission-scoped to owner)
- **Frontend pages**: `frontend/src/pages/sys-admin/repos/` — `all-repos.js`, `search-repos.js`, `repos.js`, `trash-repos.js`, `dir-view.js`
- **seafile-js methods**: `sysAdminListAllRepos`, `sysAdminSearchRepos`, `sysAdminDeleteRepo`, `sysAdminTransferRepo`, `sysAdminCreateRepo`, `sysAdminGetRepoHistorySetting`, `sysAdminUpdateRepoHistorySetting`, `sysAdminListRepoSharedItems`, `sysAdminListRepoDirents`

### What's Missing — Backend Endpoints

| Method | Endpoint | Handler | seafile-js method | Notes |
|--------|----------|---------|-------------------|-------|
| GET | `/admin/libraries/` | `ListAllLibraries` | `sysAdminListAllRepos` | `?page=&per_page=&order_by=` |
| GET | `/admin/search-libraries/` | `SearchLibraries` | `sysAdminSearchRepos` | `?name_or_id=&page=&per_page=` |
| DELETE | `/admin/libraries/:library_id/` | `AdminDeleteLibrary` | `sysAdminDeleteRepo` | Admin privilege bypass |
| PUT | `/admin/libraries/:library_id/transfer/` | `AdminTransferLibrary` | `sysAdminTransferRepo` | FormData: `owner` (email) |
| POST | `/admin/libraries/` | `AdminCreateLibrary` | `sysAdminCreateRepo` | FormData: `name`, `owner` |
| GET | `/admin/libraries/:library_id/` | `AdminGetLibrary` | — | Library details |
| GET | `/admin/libraries/:library_id/dirents/` | `AdminListLibraryDirents` | `sysAdminListRepoDirents` | Browse library as admin |
| GET | `/admin/libraries/:library_id/history-setting/` | `AdminGetHistorySetting` | `sysAdminGetRepoHistorySetting` | |
| PUT | `/admin/libraries/:library_id/history-setting/` | `AdminUpdateHistorySetting` | `sysAdminUpdateRepoHistorySetting` | FormData: `keep_days` |
| GET | `/admin/libraries/:library_id/shared-items/` | `AdminListSharedItems` | `sysAdminListRepoSharedItems` | `?share_type=user|group` |

### Response Formats

**ListAllLibraries**:
```json
{
  "repos": [
    {
      "id": "uuid",
      "name": "Library Name",
      "owner": "user@example.com",
      "owner_name": "User Name",
      "size": 1234567,
      "file_count": 42,
      "encrypted": false,
      "created_at": "2026-01-01T00:00:00Z",
      "updated_at": "2026-01-15T00:00:00Z"
    }
  ],
  "page_info": {
    "has_next_page": true,
    "current_page": 1
  }
}
```

### Implementation Notes

- **File**: `internal/api/v2/admin.go` — add handlers alongside existing group/user admin endpoints
- **Key difference from user endpoints**: No permission filter — admin sees ALL libraries in their org; superadmin sees ALL libraries across all orgs
- **Library lookup**: Use `libraries` table (partitioned by `org_id`) for listing, `libraries_by_id` for single lookups
- **Transfer**: Update `owner_id` in both `libraries` and `libraries_by_id` tables (dual-write)
- **Search**: Use existing SASI index on `name` field — `WHERE name LIKE '%query%'`

---

## 2. Admin Share Link & Upload Link Management

### What Exists

**Share Links (download links)**:
- **Database**: `share_links` (by token) + `share_links_by_creator` (by org+user) tables
- **User endpoints**: Full CRUD at `/api/v2.1/share-links/` (scoped to creator)
- **Frontend pages**: `frontend/src/pages/sys-admin/links/share-links.js`
- **seafile-js methods**: `sysAdminListShareLinks`, `sysAdminDeleteShareLink`

**Upload Links**:
- **Database**: ❌ No `upload_links` table exists
- **User endpoints**: ❌ Not implemented
- **Frontend pages**: `frontend/src/pages/sys-admin/links/upload-links.js`
- **seafile-js methods**: `sysAdminListAllUploadLinks`, `sysAdminDeleteUploadLink`

### What's Missing — Backend Endpoints

#### Admin Share Link Endpoints

| Method | Endpoint | Handler | seafile-js method | Notes |
|--------|----------|---------|-------------------|-------|
| GET | `/admin/share-links/` | `AdminListShareLinks` | `sysAdminListShareLinks` | `?page=&per_page=&sort_by=&sort_order=` |
| DELETE | `/admin/share-links/:token/` | `AdminDeleteShareLink` | `sysAdminDeleteShareLink` | Admin privilege — delete any link |

#### Upload Links — Full Feature (User + Admin)

**Database tables needed**:
```cql
CREATE TABLE upload_links (
    upload_token TEXT PRIMARY KEY,
    org_id UUID,
    library_id UUID,
    file_path TEXT,
    created_by UUID,
    creator_email TEXT,
    password_hash TEXT,
    expires_at TIMESTAMP,
    created_at TIMESTAMP
);

CREATE TABLE upload_links_by_creator (
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

**User endpoints** (new file `internal/api/v2/upload_links.go`):

| Method | Endpoint | Handler | Notes |
|--------|----------|---------|-------|
| GET | `/api/v2.1/upload-links/` | `ListUploadLinks` | User's own upload links |
| POST | `/api/v2.1/upload-links/` | `CreateUploadLink` | Create upload link for a folder |
| DELETE | `/api/v2.1/upload-links/:token/` | `DeleteUploadLink` | Delete own upload link |

**Admin endpoints**:

| Method | Endpoint | Handler | seafile-js method |
|--------|----------|---------|-------------------|
| GET | `/admin/upload-links/` | `AdminListUploadLinks` | `sysAdminListAllUploadLinks` |
| DELETE | `/admin/upload-links/:token/` | `AdminDeleteUploadLink` | `sysAdminDeleteUploadLink` |

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

- **Admin share link listing**: Must query `share_links` table (not `share_links_by_creator`) to see all links. For Cassandra, this means a full table scan — use pagination and consider adding a `share_links_by_org` table if performance is a concern.
- **Upload links**: Entirely new feature — needs DB tables, user endpoints, admin endpoints, and integration with the existing upload flow (the upload handler at `/seafhttp/upload-api/:token` needs to also accept upload link tokens).

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

1. **Admin Library Management** — Highest value, database already exists, just needs endpoints
2. **Admin Share Link Management** — Share links exist, upload links need new tables
3. **Audit Logs** — Largest scope (5 tables, integration across ~15 handlers), but medium priority

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
