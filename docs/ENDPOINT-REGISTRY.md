# API Endpoint Registry

**Last Updated**: 2026-02-12
**Purpose**: Prevent route conflicts and provide quick lookup for endpoint locations

## How to Use This Registry

### Before Implementing a New Endpoint:
1. **Search this file** for the route pattern (e.g., `/repos/:repo_id/file/`)
2. **Verify with grep**: `grep -r "route-pattern" internal/api`
3. **If exists**: Modify existing handler instead of creating new one
4. **If new**: Add entry to this registry after implementation

### Registry Format:
```
### Route Pattern
**Handler**: FunctionName
**File**: path/to/file.go:line
**Registration**: where route is registered
**Purpose**: what it does
**Added**: YYYY-MM-DD
```

---

## Library Endpoints

### GET /api/v2.1/repos/
**Handler**: `LibraryHandler.ListLibrariesV21`
**File**: `internal/api/v2/libraries.go:672`
**Registration**: `internal/api/v2/libraries.go:74`
**Purpose**: List all libraries for authenticated user (v2.1 format)
**Added**: 2024-12-01

### GET /api2/repos/
**Handler**: `LibraryHandler.ListLibraries`
**File**: `internal/api/v2/libraries.go:121`
**Registration**: `internal/api/v2/files.go:177`
**Purpose**: List all libraries (api2 format for desktop client)
**Added**: 2024-12-01

### GET /api/v2.1/repos/:repo_id
**Handler**: `LibraryHandler.GetLibraryV21`
**File**: `internal/api/v2/libraries.go:1005`
**Registration**: `internal/api/v2/libraries.go:75`
**Purpose**: Get library details (v2.1 format)
**Added**: 2024-12-01

### POST /api/v2.1/repos/
**Handler**: `LibraryHandler.CreateLibrary`
**File**: `internal/api/v2/libraries.go:~200`
**Registration**: `internal/api/v2/files.go:~183`
**Purpose**: Create new library
**Added**: 2024-12-01

### DELETE /api/v2.1/repos/:repo_id/
**Handler**: `LibraryHandler.DeleteLibrary`
**File**: `internal/api/v2/libraries.go`
**Registration**: `internal/api/v2/libraries.go:76-77`
**Purpose**: Delete library
**Added**: 2024-12-01

### POST /api/v2.1/repos/:repo_id/?op=rename
**Handler**: `LibraryHandler.LibraryOperation`
**File**: `internal/api/v2/libraries.go`
**Registration**: `internal/api/v2/files.go`
**Purpose**: Rename library
**Added**: 2024-12-01

---

## Trash (Recycle Bin) Endpoints

### GET /api/v2.1/repos/:repo_id/trash/
**Handler**: `TrashHandler.GetRepoFolderTrash`
**File**: `internal/api/v2/trash.go`
**Registration**: `internal/api/v2/trash.go:RegisterTrashRoutes`
**Purpose**: List deleted files/folders in a library (walks commit history)
**Added**: 2026-02-05

### DELETE /api/v2.1/repos/:repo_id/trash/
**Handler**: `TrashHandler.CleanRepoTrash`
**File**: `internal/api/v2/trash.go`
**Purpose**: Clean/empty trash (acknowledge request, actual pruning by GC)
**Added**: 2026-02-05

### POST /api/v2.1/repos/:repo_id/file/restore/
**Handler**: `TrashHandler.RestoreTrashItem`
**File**: `internal/api/v2/trash.go`
**Purpose**: Restore a deleted file from trash
**Added**: 2026-02-05

### POST /api/v2.1/repos/:repo_id/dir/restore/
**Handler**: `TrashHandler.RestoreTrashItem`
**File**: `internal/api/v2/trash.go`
**Purpose**: Restore a deleted folder from trash
**Added**: 2026-02-05

### GET /api/v2.1/repos/:repo_id/commit/:commit_id/dir/
**Handler**: `TrashHandler.ListCommitDir`
**File**: `internal/api/v2/trash.go`
**Purpose**: Browse directory at a specific commit (for viewing deleted folder contents)
**Added**: 2026-02-05

---

## Deleted Library (Library Recycle Bin) Endpoints

### GET /api/v2.1/deleted-repos/
**Handler**: `DeletedLibraryHandler.ListDeletedRepos`
**File**: `internal/api/v2/deleted_libraries.go`
**Registration**: `internal/api/v2/deleted_libraries.go:RegisterDeletedLibraryRoutes`
**Purpose**: List soft-deleted libraries for current user
**Added**: 2026-02-05

### PUT /api/v2.1/repos/deleted/:repo_id/
**Handler**: `DeletedLibraryHandler.RestoreDeletedRepo`
**File**: `internal/api/v2/deleted_libraries.go`
**Purpose**: Restore a soft-deleted library
**Added**: 2026-02-05

### DELETE /api/v2.1/repos/deleted/:repo_id/
**Handler**: `DeletedLibraryHandler.PermanentDeleteRepo`
**File**: `internal/api/v2/deleted_libraries.go`
**Purpose**: Permanently delete a soft-deleted library (enqueues GC)
**Added**: 2026-02-05

---

## File Endpoints

### GET /api/v2.1/repos/:repo_id/file/?p=/path
**Handler**: `FileHandler.GetFileInfo`
**File**: `internal/api/v2/files.go:1021`
**Registration**: `internal/api/v2/libraries.go:88-89`
**Purpose**: Get file metadata including `view_url` for "View on Cloud" feature
**Response Fields**: `id`, `type`, `name`, `size`, `mtime`, `permission`, `starred`, `repo_id`, `repo_name`, `parent_dir`, `view_url`
**Added**: 2024-12-01
**Updated**: 2026-01-18 (added view_url field)

### GET /api/v2.1/repos/:repo_id/file/detail/?p=/path
**Handler**: `FileHandler.GetFileDetail`
**File**: `internal/api/v2/files.go:1082`
**Registration**: `internal/api/v2/libraries.go:96-97`
**Purpose**: Get detailed file information (includes modifier info)
**Added**: 2024-12-01

### GET /api/v2.1/repos/:repo_id/file/history/?p=/path
**Handler**: `FileHandler.GetFileHistoryV21`
**File**: `internal/api/v2/files.go`
**Registration**: `internal/api/server.go:378-381`
**Purpose**: Get file revision history
**Added**: 2025-01-01

### DELETE /api/v2.1/repos/:repo_id/file/?p=/path
**Handler**: `FileHandler.DeleteFile`
**File**: `internal/api/v2/files.go:1152`
**Registration**: `internal/api/v2/libraries.go:90-91`
**Purpose**: Delete file
**Added**: 2024-12-01

### POST /api/v2.1/repos/:repo_id/file/?p=/path&operation=rename
**Handler**: `FileHandler.FileOperation`
**File**: `internal/api/v2/files.go:637`
**Registration**: `internal/api/v2/libraries.go:92-93`
**Purpose**: Rename, create, or revert file
**Query Params**: `operation=rename|create|revert`
**Added**: 2024-12-01, revert added 2026-02-05

### PUT /api/v2.1/repos/:repo_id/file/?p=/path
**Handler**: `FileHandler.LockFile`
**File**: `internal/api/v2/files.go:2183`
**Registration**: `internal/api/v2/libraries.go:94-95`
**Purpose**: Lock/unlock file
**Query Params**: `operation=lock|unlock`
**Added**: 2024-12-01

### POST /api/v2.1/repos/:repo_id/file/move/?p=/path
**Handler**: `FileHandler.MoveFile`
**File**: `internal/api/v2/files.go:1260`
**Registration**: `internal/api/v2/libraries.go:106-107`
**Purpose**: Move file to different location
**Added**: 2024-12-01

### POST /api/v2.1/repos/:repo_id/file/copy/?p=/path
**Handler**: `FileHandler.CopyFile`
**File**: `internal/api/v2/files.go:1498`
**Registration**: `internal/api/v2/libraries.go:108-109`
**Purpose**: Copy file
**Added**: 2024-12-01

### POST /api/v2.1/repos/:repo_id/zip-task/?p=/path
**Handler**: `FileHandler.CreateZipTask`
**File**: `internal/api/v2/files.go:3893`
**Registration**: `internal/api/v2/libraries.go:176-177`
**Purpose**: Create ZIP download task for a directory (authenticated users)
**Response**: `{"zip_token": "..."}`
**Query Params**: `p` (path to directory/file)
**Added**: 2026-02-13

---

## Directory Endpoints

### GET /api/v2.1/repos/:repo_id/dir/?p=/path
**Handler**: `FileHandler.ListDirectoryV21`
**File**: `internal/api/v2/files.go:1924`
**Registration**: `internal/api/v2/libraries.go:78-79`
**Purpose**: List directory contents
**Added**: 2024-12-01

### DELETE /api/v2.1/repos/:repo_id/dir/?p=/path
**Handler**: `FileHandler.DeleteDirectory`
**File**: `internal/api/v2/files.go:927`
**Registration**: `internal/api/v2/libraries.go:100-101`
**Purpose**: Delete directory
**Added**: 2024-12-01

### POST /api/v2.1/repos/:repo_id/dir/?p=/path&operation=mkdir
**Handler**: `FileHandler.DirectoryOperation`
**File**: `internal/api/v2/files.go:404`
**Registration**: `internal/api/v2/libraries.go:102-103`
**Purpose**: Create, rename, or revert directory
**Query Params**: `operation=mkdir|rename|revert`
**Added**: 2024-12-01, revert added 2026-02-05

---

## Upload/Download Endpoints

### GET /api/v2.1/repos/:repo_id/file/download-link/?p=/path
**Handler**: `FileHandler.GetDownloadLink`
**File**: `internal/api/v2/files.go:1658`
**Registration**: `internal/api/v2/files.go`
**Purpose**: Get download link for file
**Added**: 2024-12-01

### GET /api/v2.1/repos/:repo_id/upload-link/?p=/path
**Handler**: `FileHandler.GetUploadLink`
**File**: `internal/api/v2/files.go:1698`
**Registration**: `internal/api/v2/files.go`
**Purpose**: Get upload link for file
**Added**: 2024-12-01

### GET /api/v2.1/repos/:repo_id/file-uploaded-bytes/
**Handler**: `FileHandler.GetFileUploadedBytes`
**File**: `internal/api/v2/files.go`
**Registration**: `internal/api/v2/libraries.go:112-113`
**Purpose**: Get resumable upload progress
**Added**: 2024-12-01

---

## Encryption Endpoints

### POST /api/v2.1/repos/:repo_id/set-password/
**Handler**: `EncryptionHandler.SetPassword`
**File**: `internal/api/v2/encryption.go:28`
**Registration**: `internal/api/v2/libraries.go:82-83`
**Purpose**: Verify library password (unlock encrypted library)
**Added**: 2026-01-08

### PUT /api/v2.1/repos/:repo_id/set-password/
**Handler**: `EncryptionHandler.ChangePassword`
**File**: `internal/api/v2/encryption.go:97`
**Registration**: `internal/api/v2/libraries.go:84-85`
**Purpose**: Change library password
**Added**: 2026-01-08

---

## OnlyOffice Integration

### GET /api/v2.1/repos/:repo_id/onlyoffice/?p=/path
**Handler**: `OnlyOfficeHandler.GetEditorConfig`
**File**: `internal/api/v2/onlyoffice.go`
**Registration**: `internal/api/v2/onlyoffice.go`
**Purpose**: Get OnlyOffice editor configuration
**Added**: 2025-01-01

### POST /api/v2.1/repos/:repo_id/onlyoffice/callback/
**Handler**: `OnlyOfficeHandler.Callback`
**File**: `internal/api/v2/onlyoffice.go`
**Registration**: `internal/api/v2/onlyoffice.go`
**Purpose**: Handle OnlyOffice save callback
**Added**: 2025-01-01

---

## Starred Items Endpoints

### GET /api/v2.1/starred-items/
**Handler**: `StarredHandler.ListStarredItemsV21`
**File**: `internal/api/v2/starred.go`
**Registration**: `internal/api/v2/starred.go`
**Purpose**: List starred files/folders
**Added**: 2024-12-01

### POST /api/v2.1/starred-items/
**Handler**: `StarredHandler.StarFile`
**File**: `internal/api/v2/starred.go`
**Registration**: `internal/api/v2/starred.go`
**Purpose**: Star a file/folder
**Added**: 2024-12-01

### DELETE /api/v2.1/starred-items/
**Handler**: `StarredHandler.UnstarFile`
**File**: `internal/api/v2/starred.go`
**Registration**: `internal/api/v2/starred.go`
**Purpose**: Unstar a file/folder
**Added**: 2024-12-01

---

## Share Link Endpoints

### GET /api/v2.1/repos/:repo_id/share-links/
**Handler**: `ShareLinkHandler.ListShareLinks`
**File**: `internal/api/v2/share_links.go`
**Registration**: `internal/api/v2/share_links.go`
**Purpose**: List share links for file/folder
**Added**: 2025-01-01

### POST /api/v2.1/repos/:repo_id/share-links/
**Handler**: `ShareLinkHandler.CreateShareLink`
**File**: `internal/api/v2/share_links.go`
**Registration**: `internal/api/v2/share_links.go`
**Purpose**: Create share link
**Added**: 2025-01-01

### DELETE /api/v2.1/repos/:repo_id/share-links/:link_id/
**Handler**: `ShareLinkHandler.DeleteShareLink`
**File**: `internal/api/v2/share_links.go`
**Registration**: `internal/api/v2/share_links.go`
**Purpose**: Delete share link
**Added**: 2025-01-01

---

## Batch Operations

### DELETE /api/v2.1/repos/batch-delete-item/
**Handler**: `FileHandler.BatchDeleteItems`
**File**: `internal/api/v2/files.go`
**Registration**: `internal/api/server.go:374-375`
**Purpose**: Delete multiple files/folders
**Added**: 2024-12-01

### POST /api/v2.1/repos/sync-batch-move-item/
**Handler**: `BatchOperationHandler.SyncBatchMove`
**File**: `internal/api/v2/batch_operations.go`
**Registration**: `internal/api/server.go`
**Purpose**: Synchronous batch move (same repo)
**Added**: 2026-01-27

### POST /api/v2.1/repos/sync-batch-copy-item/
**Handler**: `BatchOperationHandler.SyncBatchCopy`
**File**: `internal/api/v2/batch_operations.go`
**Registration**: `internal/api/server.go`
**Purpose**: Synchronous batch copy (same repo)
**Added**: 2026-01-27

### POST /api/v2.1/repos/async-batch-move-item/
**Handler**: `BatchOperationHandler.AsyncBatchMove`
**File**: `internal/api/v2/batch_operations.go`
**Registration**: `internal/api/server.go`
**Purpose**: Asynchronous batch move (cross repo), returns task_id
**Added**: 2026-01-27

### POST /api/v2.1/repos/async-batch-copy-item/
**Handler**: `BatchOperationHandler.AsyncBatchCopy`
**File**: `internal/api/v2/batch_operations.go`
**Registration**: `internal/api/server.go`
**Purpose**: Asynchronous batch copy (cross repo), returns task_id
**Added**: 2026-01-27

### GET /api/v2.1/copy-move-task/
**Handler**: `BatchOperationHandler.GetTaskProgress`
**File**: `internal/api/v2/batch_operations.go`
**Registration**: `internal/api/server.go`
**Purpose**: Query progress of async move/copy task
**Query Params**: `task_id` (required)
**Added**: 2026-01-27

---

## Sync Protocol Endpoints (/seafhttp/)

These endpoints are used by Seafile desktop/mobile clients for sync. **DO NOT MODIFY without protocol testing.**

### GET /seafhttp/repo/:repo_id/commit/HEAD
**Handler**: `SyncHandler.GetHeadCommit`
**File**: `internal/api/sync.go`
**Purpose**: Get HEAD commit for repository
**Status**: 🔒 FROZEN (desktop client compatibility)

### GET /seafhttp/repo/:repo_id/commit/:commit_id
**Handler**: `SyncHandler.GetCommit`
**File**: `internal/api/sync.go`
**Purpose**: Get specific commit object
**Status**: 🔒 FROZEN

### PUT /seafhttp/repo/:repo_id/commit/:commit_id
**Handler**: `SyncHandler.PutCommit`
**File**: `internal/api/sync.go`
**Purpose**: Upload commit object
**Status**: 🔒 FROZEN

### GET /seafhttp/repo/:repo_id/block/:block_id
**Handler**: `SyncHandler.GetBlock`
**File**: `internal/api/sync.go`
**Purpose**: Download content block
**Status**: 🔒 FROZEN

### PUT /seafhttp/repo/:repo_id/block/:block_id
**Handler**: `SyncHandler.PutBlock`
**File**: `internal/api/sync.go`
**Purpose**: Upload content block
**Status**: 🔒 FROZEN

### POST /seafhttp/repo/:repo_id/check-blocks/
**Handler**: `SyncHandler.CheckBlocks`
**File**: `internal/api/sync.go:601`
**Purpose**: Check which blocks exist on server
**Input**: JSON array of block IDs
**Output**: JSON array of missing block IDs
**Status**: 🔒 FROZEN

### GET /seafhttp/repo/:repo_id/fs-id-list/?server-head=xxx
**Handler**: `SyncHandler.GetFSIDList`
**File**: `internal/api/sync.go:949`
**Purpose**: Get list of all FS objects for commit
**Output**: JSON array of FS IDs
**Status**: 🔒 FROZEN (CRITICAL: must return JSON array, not newline-separated)

### POST /seafhttp/repo/:repo_id/check-fs/
**Handler**: `SyncHandler.CheckFS`
**File**: `internal/api/sync.go:1405`
**Purpose**: Check which FS objects exist on server
**Input**: JSON array of FS IDs
**Output**: JSON array of missing FS IDs
**Status**: 🔒 FROZEN (CRITICAL: includes FS ID mapping)

### POST /seafhttp/repo/:repo_id/pack-fs/
**Handler**: `SyncHandler.PackFS`
**File**: `internal/api/sync.go`
**Purpose**: Download multiple FS objects in pack format
**Input**: JSON array of FS IDs
**Output**: Binary pack (40-byte ID + 4-byte size BE + zlib-compressed JSON)
**Status**: 🔒 FROZEN

### POST /seafhttp/repo/:repo_id/recv-fs/
**Handler**: `SyncHandler.RecvFS`
**File**: `internal/api/sync.go`
**Purpose**: Upload FS objects
**Status**: 🔒 FROZEN

### POST /seafhttp/repo/head-commits-multi
**Handler**: `SyncHandler.GetHeadCommitsMulti`
**File**: `internal/api/sync.go:1519`
**Purpose**: Get HEAD commits for multiple repos efficiently
**Input**: JSON array of repo IDs
**Output**: JSON object `{"repo-id": "commit-id"}`
**Status**: 🔒 FROZEN (Fixed 2026-01-18)

### GET /seafhttp/repo/:repo_id/permission-check/
**Handler**: `SyncHandler.PermissionCheck`
**File**: `internal/api/sync.go`
**Purpose**: Check user permissions
**Output**: 200 OK with empty body (permission granted)
**Status**: 🔒 FROZEN

---

## Authentication Endpoints

### POST /api2/auth-token/
**Handler**: `AuthHandler.Login`
**File**: `internal/api/server.go`
**Purpose**: Username/password login (dev mode only)
**Added**: 2024-12-01

### GET /api/v2.1/auth/oidc/config/
**Handler**: `AuthHandler.GetOIDCConfig`
**File**: `internal/api/v2/auth.go:35`
**Registration**: `internal/api/server.go`
**Purpose**: Get public OIDC configuration (enabled status, provider URL)
**Added**: 2026-01-28

### GET /api/v2.1/auth/oidc/login/
**Handler**: `AuthHandler.GetOIDCLoginURL`
**File**: `internal/api/v2/auth.go:55`
**Registration**: `internal/api/server.go`
**Purpose**: Get OIDC authorization URL for SSO login redirect
**Query Params**: `redirect_uri`, `return_url`
**Added**: 2026-01-28

### POST /api/v2.1/auth/oidc/callback/
**Handler**: `AuthHandler.HandleOIDCCallback`
**File**: `internal/api/v2/auth.go:127`
**Registration**: `internal/api/server.go`
**Purpose**: Exchange authorization code for session token
**Request Body**: `{ code, state, redirect_uri }`
**Response**: `{ token, user: { email, name } }`
**Added**: 2026-01-28

### GET /api/v2.1/auth/oidc/logout/
**Handler**: `AuthHandler.GetOIDCLogoutURL`
**File**: `internal/api/v2/auth.go:232`
**Registration**: `internal/api/server.go`
**Purpose**: Get OIDC logout URL for Single Logout (SLO)
**Query Params**: `post_logout_redirect_uri` (optional, defaults to /login/)
**Response**: `{ logout_url, post_logout_redirect_uri, enabled }`
**Added**: 2026-01-28

---

## Monitoring & Health Endpoints

### GET /health
**Handler**: `health.Checker.HandleLiveness`
**File**: `internal/health/health.go`
**Registration**: `internal/api/server.go`
**Purpose**: Kubernetes liveness probe — returns 200 if process is alive (no dependency checks)
**Added**: 2026-01-30

### GET /ready
**Handler**: `health.Checker.HandleReadiness`
**File**: `internal/health/health.go`
**Registration**: `internal/api/server.go`
**Purpose**: Kubernetes readiness probe — checks Cassandra + S3 connectivity, returns 503 if down
**Added**: 2026-01-30

### GET /metrics
**Handler**: `promhttp.Handler()`
**File**: (prometheus client library)
**Registration**: `internal/api/server.go`
**Purpose**: Prometheus metrics endpoint — request counts, durations, Go runtime stats
**Added**: 2026-01-30

### GET /ping
**Handler**: inline
**File**: `internal/api/server.go`
**Registration**: `internal/api/server.go`
**Purpose**: Simple ping endpoint — returns "pong"
**Added**: 2024-12-01

---

## Stub Endpoints (Return Empty Results)

These endpoints are required by Seafile clients but not fully implemented:

- GET /api/v2.1/notifications/
- GET /api/v2.1/repo-folder-share-info/
- GET /api/v2.1/departments/
- GET /api/v2.1/shared-repos/
- GET /api/v2.1/repos/:repo_id/auto-delete/
- PUT /api/v2.1/repos/:repo_id/auto-delete/
- GET /api/v2.1/repos/:repo_id/repo-api-tokens/

---

## File/Folder Sharing Endpoints

### GET /api/v2.1/shareable-groups/
**Handler**: `GroupHandler.ListShareableGroups`
**File**: `internal/api/v2/groups.go`
**Registration**: `RegisterShareableGroupRoutes` in server.go
**Purpose**: List groups user can share with (returns user's groups)
**Response**: `[{id, name, parent_group_id}]`
**Added**: 2026-02-12

### GET /api/v2.1/repos/:repo_id/custom-share-permissions/
**Handler**: `FileShareHandler.ListCustomSharePermissions`
**File**: `internal/api/v2/file_shares.go`
**Registration**: `RegisterV21LibraryRoutes` in libraries.go
**Purpose**: List custom share permissions (Seafile Pro feature, returns empty list)
**Response**: `{"permission_list": []}`
**Added**: 2026-02-12

### GET /api2/repos/:repo_id/dir/shared_items/
**Handler**: `FileShareHandler.ListSharedItems`
**File**: `internal/api/v2/file_shares.go`
**Registration**: `RegisterLibraryRoutesWithToken` in libraries.go
**Purpose**: List file/folder shares (user or group)
**Query Params**: `p` (path), `share_type` (user|group)
**Added**: 2026-02-12
**Note**: Also available under /api/v2.1/ prefix

### PUT /api2/repos/:repo_id/dir/shared_items/
**Handler**: `FileShareHandler.CreateShare`
**File**: `internal/api/v2/file_shares.go`
**Registration**: `RegisterLibraryRoutesWithToken` in libraries.go
**Purpose**: Create file/folder share
**Added**: 2026-02-12

### POST /api2/repos/:repo_id/dir/shared_items/
**Handler**: `FileShareHandler.UpdateSharePermission`
**File**: `internal/api/v2/file_shares.go`
**Registration**: `RegisterLibraryRoutesWithToken` in libraries.go
**Purpose**: Update share permission
**Added**: 2026-02-12

### DELETE /api2/repos/:repo_id/dir/shared_items/
**Handler**: `FileShareHandler.DeleteShare`
**File**: `internal/api/v2/file_shares.go`
**Registration**: `RegisterLibraryRoutesWithToken` in libraries.go
**Purpose**: Delete file/folder share
**Added**: 2026-02-12

### GET /api/v2.1/repos/:repo_id/share-info/
**Handler**: `LibraryHandler.GetRepoFolderShareInfo`
**File**: `internal/api/v2/libraries.go`
**Registration**: `RegisterV21LibraryRoutes` in libraries.go
**Purpose**: Get share info for library/folder (stub, returns empty shares)
**Added**: 2024-12-01

### GET /api/v2.1/groups/
**Handler**: `GroupHandler.ListGroups`
**File**: `internal/api/v2/groups.go`
**Registration**: `RegisterGroupRoutes` in server.go
**Purpose**: List groups user is member of
**Added**: 2026-01-01

---

## Quick Search Commands

```bash
# Find all route registrations
grep -rn "GET\|POST\|PUT\|DELETE\|PATCH" internal/api --include="*.go" | grep "repos"

# Find handler for specific route
grep -rn "GetFileInfo\|specific-handler-name" internal/api

# Find all routes in a file
grep -n "repos\." internal/api/v2/libraries.go

# Check if route exists before implementing
grep -r "/repos/:repo_id/your-route" internal/api
```

---

## Route Conflict Prevention Checklist

Before implementing a new endpoint:

- [ ] Search this registry for route pattern
- [ ] Run: `grep -r "route-pattern" internal/api`
- [ ] Check `internal/api/v2/libraries.go` RegisterV21LibraryRoutes
- [ ] Check `internal/api/server.go` route registrations
- [ ] If route exists: Modify existing handler (don't create duplicate)
- [ ] If route is new: Implement and add to this registry
- [ ] Test with: `docker-compose build sesamefs && docker-compose up -d sesamefs`
- [ ] Verify no panic about "handlers are already registered for path"

---

## Admin Library Management Endpoints

### GET /api/v2.1/admin/libraries/
**Handler**: `AdminHandler.AdminListAllLibraries`
**File**: `internal/api/v2/admin.go`
**Registration**: `RegisterAdminRoutes` in admin.go
**Purpose**: List all libraries (admin/superadmin). Params: `page`, `per_page`, `order_by`
**Added**: 2026-02-12

### GET /api/v2.1/admin/search-libraries/
**Handler**: `AdminHandler.AdminSearchLibraries`
**File**: `internal/api/v2/admin.go`
**Registration**: `RegisterAdminRoutes` in admin.go
**Purpose**: Search libraries by name or ID. Params: `name_or_id`, `page`, `per_page`
**Added**: 2026-02-12

### GET /api/v2.1/admin/libraries/:library_id/
**Handler**: `AdminHandler.AdminGetLibrary`
**File**: `internal/api/v2/admin.go`
**Registration**: `RegisterAdminRoutes` in admin.go
**Purpose**: Get single library details (admin privilege)
**Added**: 2026-02-12

### POST /api/v2.1/admin/libraries/
**Handler**: `AdminHandler.AdminCreateLibrary`
**File**: `internal/api/v2/admin.go`
**Registration**: `RegisterAdminRoutes` in admin.go
**Purpose**: Create library for any user. JSON body: `{name, owner}`
**Added**: 2026-02-12

### DELETE /api/v2.1/admin/libraries/:library_id/
**Handler**: `AdminHandler.AdminDeleteLibrary`
**File**: `internal/api/v2/admin.go`
**Registration**: `RegisterAdminRoutes` in admin.go
**Purpose**: Soft-delete any library (admin privilege, no owner check)
**Added**: 2026-02-12

### PUT /api/v2.1/admin/libraries/:library_id/transfer/
**Handler**: `AdminHandler.AdminTransferLibrary`
**File**: `internal/api/v2/admin.go`
**Registration**: `RegisterAdminRoutes` in admin.go
**Purpose**: Transfer library ownership. JSON body: `{owner}`
**Added**: 2026-02-12

### GET /api/v2.1/admin/libraries/:library_id/dirents/
**Handler**: `AdminHandler.AdminListDirents`
**File**: `internal/api/v2/admin.go`
**Registration**: `RegisterAdminRoutes` in admin.go
**Purpose**: Browse library directory contents as admin. Params: `path`
**Added**: 2026-02-12

### GET /api/v2.1/admin/libraries/:library_id/history-setting/
**Handler**: `AdminHandler.AdminGetHistorySetting`
**File**: `internal/api/v2/admin.go`
**Registration**: `RegisterAdminRoutes` in admin.go
**Purpose**: Get library version history setting (keep_days)
**Added**: 2026-02-12

### PUT /api/v2.1/admin/libraries/:library_id/history-setting/
**Handler**: `AdminHandler.AdminUpdateHistorySetting`
**File**: `internal/api/v2/admin.go`
**Registration**: `RegisterAdminRoutes` in admin.go
**Purpose**: Update library version history setting. JSON body: `{keep_days}`
**Added**: 2026-02-12

### GET /api/v2.1/admin/libraries/:library_id/shared-items/
**Handler**: `AdminHandler.AdminListSharedItems`
**File**: `internal/api/v2/admin.go`
**Registration**: `RegisterAdminRoutes` in admin.go
**Purpose**: List users and groups a library is shared with. Params: `share_type`
**Added**: 2026-02-12

### GET /api/v2.1/admin/trash-libraries/
**Handler**: `AdminHandler.AdminListTrashLibraries`
**File**: `internal/api/v2/admin.go`
**Registration**: `RegisterAdminRoutes` in admin.go
**Purpose**: List soft-deleted libraries. Params: `page`, `per_page`, `owner`
**Added**: 2026-02-12

---

## Upload Link Endpoints (User)

### GET /api/v2.1/upload-links/
**Handler**: `UploadLinkHandler.ListUploadLinks`
**File**: `internal/api/v2/upload_links.go`
**Registration**: `RegisterUploadLinkRoutes` in upload_links.go
**Purpose**: List user's own upload links. Optional `?repo_id=` filter
**Added**: 2026-02-12

### POST /api/v2.1/upload-links/
**Handler**: `UploadLinkHandler.CreateUploadLink`
**File**: `internal/api/v2/upload_links.go`
**Registration**: `RegisterUploadLinkRoutes` in upload_links.go
**Purpose**: Create upload link for a folder. JSON body: `{repo_id, path, password, expire_days}`
**Added**: 2026-02-12

### DELETE /api/v2.1/upload-links/:token/
**Handler**: `UploadLinkHandler.DeleteUploadLink`
**File**: `internal/api/v2/upload_links.go`
**Registration**: `RegisterUploadLinkRoutes` in upload_links.go
**Purpose**: Delete own upload link (verifies ownership, dual-deletes)
**Added**: 2026-02-12

### GET /api/v2.1/repos/:repo_id/upload-links/
**Handler**: `UploadLinkHandler.ListRepoUploadLinks`
**File**: `internal/api/v2/upload_links.go`
**Registration**: `RegisterUploadLinkRoutes` in upload_links.go
**Purpose**: List upload links for specific repo
**Added**: 2026-02-12

---

## Admin Link Management Endpoints

### GET /api/v2.1/admin/share-links/
**Handler**: `AdminHandler.AdminListShareLinks`
**File**: `internal/api/v2/admin_extra.go`
**Registration**: `RegisterAdminExtraRoutes` in admin_extra.go
**Purpose**: List all share links (admin). Params: `page`, `per_page`, `order_by`, `direction`
**Added**: 2026-02-12

### DELETE /api/v2.1/admin/share-links/:token/
**Handler**: `AdminHandler.AdminDeleteShareLink`
**File**: `internal/api/v2/admin_extra.go`
**Registration**: `RegisterAdminExtraRoutes` in admin_extra.go
**Purpose**: Delete any share link (admin privilege). Dual-deletes from share_links + share_links_by_creator
**Added**: 2026-02-12

### GET /api/v2.1/admin/upload-links/
**Handler**: `AdminHandler.AdminListUploadLinks`
**File**: `internal/api/v2/admin_extra.go`
**Registration**: `RegisterAdminExtraRoutes` in admin_extra.go
**Purpose**: List all upload links (admin). Params: `page`, `per_page`
**Added**: 2026-02-12

### DELETE /api/v2.1/admin/upload-links/:token/
**Handler**: `AdminHandler.AdminDeleteUploadLink`
**File**: `internal/api/v2/admin_extra.go`
**Registration**: `RegisterAdminExtraRoutes` in admin_extra.go
**Purpose**: Delete any upload link (admin privilege). Dual-deletes from upload_links + upload_links_by_creator
**Added**: 2026-02-12

### GET /api/v2.1/admin/users/:email/share-links/
**Handler**: `AdminHandler.AdminListUserShareLinks`
**File**: `internal/api/v2/admin_extra.go`
**Registration**: `RegisterAdminExtraRoutes` in admin_extra.go
**Purpose**: List share links created by specific user. Resolves email→user_id, queries share_links_by_creator
**Added**: 2026-02-12

### GET /api/v2.1/admin/users/:email/upload-links/
**Handler**: `AdminHandler.AdminListUserUploadLinks`
**File**: `internal/api/v2/admin_extra.go`
**Registration**: `RegisterAdminExtraRoutes` in admin_extra.go
**Purpose**: List upload links created by specific user. Resolves email→user_id, queries upload_links_by_creator
**Added**: 2026-02-12

---

## Update History

- **2026-02-12**: Added Admin Link Management endpoints (13 endpoints: share links, upload links, per-user links)
- **2026-02-12**: Added Admin Library Management endpoints (12 endpoints)
- **2026-01-30**: Added Monitoring & Health endpoints (/health, /ready, /metrics)
- **2026-01-28**: Added Authentication section with OIDC endpoints
- **2026-01-18**: Initial registry created, added view_url to GetFileInfo endpoint
