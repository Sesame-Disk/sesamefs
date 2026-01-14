# SesameFS - Project Context for Claude

## What is SesameFS?

A Seafile-compatible cloud storage API with modern internals (Go, Cassandra, S3).

## Critical Constraints

1. **Seafile desktop/mobile client chunking cannot be changed** - compiled into apps (Rabin CDC, 256KB-4MB, SHA-1)
2. **SHA-1→SHA-256 translation for sync protocol only** - Desktop/mobile clients use `/seafhttp/` with SHA-1 block IDs; server translates to SHA-256 for storage. Web frontend uses REST API with server-side SHA-256 chunking.
3. **Block size for web/API**: 2-256MB (server-controlled, adaptive FastCDC)
4. **SpillBuffer threshold**: 16MB (memory below, temp file above)
5. **Encryption: Weak→Strong translation** - Seafile clients use weak PBKDF2 (1K iterations); we validate with PBKDF2 for compat but store Argon2id for security. Server-side envelope encryption adds protection layer.

### Upload Paths

| Client | Protocol | Chunking | Block Hash |
|--------|----------|----------|------------|
| Desktop/Mobile | `/seafhttp/` (sync) | Client-side Rabin CDC | SHA-1 → translated to SHA-256 |
| Web Frontend | REST API | Server-side FastCDC | SHA-256 (no translation) |
| API clients | REST API | Server-side FastCDC | SHA-256 (no translation) |

## Key Code Locations

| Feature | File |
|---------|------|
| Seafile sync protocol | `internal/api/sync.go` |
| File upload/download | `internal/api/seafhttp.go` |
| S3 storage backend | `internal/storage/s3.go` |
| Block storage | `internal/storage/blocks.go` |
| Multi-backend manager | `internal/storage/storage.go` |
| FastCDC chunking | `internal/chunker/fastcdc.go` |
| Adaptive chunking | `internal/chunker/adaptive.go` |
| Database schema | `internal/db/db.go` |
| API v2 handlers | `internal/api/v2/*.go` |
| Configuration | `internal/config/config.go` |
| Encryption/Key derivation | `internal/crypto/crypto.go` |
| Library password endpoints | `internal/api/v2/encryption.go` |

## Documentation

| Document | Contents |
|----------|----------|
| [README.md](README.md) | Quick start, features overview, roadmap |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Design decisions, storage architecture, database schema |
| [docs/API-REFERENCE.md](docs/API-REFERENCE.md) | API endpoints, implementation status, compatibility |
| [docs/DATABASE-GUIDE.md](docs/DATABASE-GUIDE.md) | Cassandra tables, examples, consistency |
| [docs/FRONTEND.md](docs/FRONTEND.md) | React frontend: setup, patterns, Docker, troubleshooting |
| [docs/TESTING.md](docs/TESTING.md) | Test coverage, benchmarks, running tests |
| [docs/SYNC-TESTING.md](docs/SYNC-TESTING.md) | Seafile sync protocol testing with containerized seaf-cli |
| [docs/TECHNICAL-DEBT.md](docs/TECHNICAL-DEBT.md) | Known issues, migration plans, modal pattern fixes |
| [docs/LICENSING.md](docs/LICENSING.md) | Legal considerations for Seafile compatibility |
| [docs/ENCRYPTION.md](docs/ENCRYPTION.md) | Encrypted libraries, key derivation, Seafile compat, security |

## External References

| Resource | URL |
|----------|-----|
| Seafile API Docs (New) | https://seafile-api.readme.io/ |
| Seafile Manual - API Index | https://manual.seafile.com/latest/develop/web_api_v2.1/ |
| Seafile Server Source (upload-file.c) | https://github.com/haiwen/seafile-server/blob/master/server/upload-file.c |
| seafile-js (frontend API client) | https://github.com/haiwen/seafile-js |
| Seafile Client (resumable upload) | https://github.com/haiwen/seafile-client/blob/master/src/filebrowser/reliable-upload.cpp |

## Understanding the Seafile Desktop Sync Client

### Source Code Locations

The Seafile sync protocol is undocumented. To understand how it works, you must read the source code:

| Component | Repository | Key Files |
|-----------|------------|-----------|
| **Seafile Daemon** (sync logic) | https://github.com/haiwen/seafile | `daemon/sync-mgr.c`, `daemon/http-tx-mgr.c`, `daemon/clone-mgr.c` |
| **Common Libraries** | https://github.com/haiwen/seafile | `common/fs-mgr.c`, `common/diff-simple.c`, `common/commit-mgr.c` |
| **Seafile Server** (reference impl) | https://github.com/haiwen/seafile-server | `fileserver/pack-dir.c`, `fileserver/obj-backend-fs.c` |
| **Desktop Client UI** | https://github.com/haiwen/seafile-client | `src/daemon-mgr.cpp`, `src/repo-service.cpp` |

### Key Source Files to Study

| File | Purpose |
|------|---------|
| `daemon/sync-mgr.c` | Main sync state machine (synchronized → committing → uploading/downloading → error) |
| `daemon/http-tx-mgr.c` | HTTP transfer manager - handles pack-fs, fs-id-list, block downloads |
| `common/fs-mgr.c` | FS object management - **line 1605 shows zlib decompression**, line 1787 shows read failures |
| `common/diff-simple.c` | Diff computation between commits - line 240 shows "Failed to find dir" errors |
| `daemon/clone-mgr.c` | Initial clone/sync of a new library |

### Client Log Location

```bash
# macOS
~/.ccnet/logs/seafile.log

# Linux
~/.ccnet/logs/seafile.log

# Windows
C:\Users\<username>\ccnet\logs\seafile.log
```

### Client Local Storage Structure

```bash
~/Seafile/.seafile-data/
├── repo.db                 # SQLite - repo state, properties, branches
├── storage/
│   ├── commits/<repo_id>/  # Commit objects (JSON files)
│   ├── fs/<repo_id>/       # FS objects (zlib compressed on disk)
│   └── blocks/<repo_id>/   # Content blocks
```

### Key Database Tables (repo.db)

```sql
-- Repo properties including local-head, remote-head, server-url
SELECT * FROM RepoProperty WHERE repo_id = '<repo_id>';

-- Branch info
SELECT * FROM RepoBranch WHERE repo_id = '<repo_id>';

-- Check which server a repo syncs to
SELECT repo_id, value FROM RepoProperty WHERE key = 'server-url';
```

### Sync State Machine

```
synchronized → committing → [uploading|downloading] → synchronized
     ↓                              ↓
   error ←──────────────────────────┘
```

**State transitions logged as**: `sync-mgr.c(607): Repo 'xxx' sync state transition from 'X' to 'Y'`

### Common Error Messages and Root Causes

| Error Message | Source File | Root Cause |
|---------------|-------------|------------|
| `Failed to inflate` | `common/fs-mgr.c:1605` | pack-fs returned uncompressed data (must be zlib compressed) |
| `Failed to decompress dir object` | `common/fs-mgr.c:1605` | Same as above - fs object not zlib compressed |
| `Failed to find dir X in repo Y` | `common/diff-simple.c:240` | fs_id not in fs-id-list response, or fs object not stored locally |
| `Failed to read dir` | `common/fs-mgr.c:1787` | fs object file missing or corrupted on client disk |
| `Error when indexing` | `sync-mgr.c:646` | Client can't build directory tree - usually missing fs objects |

### Debugging Sync Issues

1. **Check client logs** for specific error messages
2. **Identify the fs_id** causing the failure
3. **Test server endpoints manually**:
   ```bash
   # Get HEAD commit
   curl -H "Authorization: Token $TOKEN" \
     "http://localhost:8080/seafhttp/repo/$REPO_ID/commit/HEAD"

   # Get fs-id-list
   curl -H "Authorization: Token $TOKEN" \
     "http://localhost:8080/seafhttp/repo/$REPO_ID/fs-id-list/?server-head=$COMMIT_ID"

   # Get pack-fs (POST with fs_ids)
   curl -X POST -H "Authorization: Token $TOKEN" \
     -d '["fs_id_1", "fs_id_2"]' \
     "http://localhost:8080/seafhttp/repo/$REPO_ID/pack-fs" -o /tmp/packfs.bin

   # Verify pack-fs format (should be: 40-byte ID + 4-byte size BE + zlib data)
   xxd /tmp/packfs.bin | head -10
   ```
4. **Verify fs_id hash**: SHA1 of JSON content must match fs_id
5. **Check local client storage** if fs objects are corrupted

### Forcing a Fresh Sync

If client is stuck, clean local state:

```bash
# Remove corrupted commits and fs objects for a repo
rm -rf ~/Seafile/.seafile-data/storage/commits/<repo_id>
rm -rf ~/Seafile/.seafile-data/storage/fs/<repo_id>

# Reset local-head in database to force re-download
sqlite3 ~/Seafile/.seafile-data/repo.db \
  "UPDATE RepoProperty SET value='0000000000000000000000000000000000000000' WHERE repo_id='<repo_id>' AND key='local-head';"

# IMPORTANT: Restart Seafile client - it caches state in memory
```

### Critical Protocol Details

| Aspect | Requirement |
|--------|-------------|
| **pack-fs format** | `[40-byte hex fs_id][4-byte size BE][zlib-compressed JSON]` |
| **fs_id computation** | SHA-1 of JSON with alphabetically-ordered keys (use `map[string]interface{}` in Go) |
| **fs-id-list** | Must return ALL fs_ids recursively (directories AND files/seafile objects) |
| **fs object storage** | Seafile server stores compressed; client stores compressed; pack-fs sends compressed |

---

## Pending Features / Known Issues

### Seafile Desktop Client: "View on Cloud" Not Working
**Status**: Not implemented
**Issue**: When right-clicking a file in the Seafile desktop client and selecting "View on Cloud", the feature doesn't work. This option should open the file in the web browser using the cloud's file viewer.
**Root Cause**: The Seafile client sends a request to get the web URL for the file, but SesameFS doesn't implement this endpoint yet.
**Required Endpoints** (to be implemented):
- `GET /api/v2.1/repos/{repo_id}/file/?p={path}` - should return `view_url` field
- Or custom endpoint that returns the web view URL
**Priority**: Medium - affects desktop client user experience

### Encrypted Library File Content Encryption
**Status**: ✅ Implemented (2026-01-09)
**What works**:
- Creating encrypted libraries with strong password protection
- Verifying passwords (set-password endpoint)
- Changing passwords (change-password endpoint)
- File content encryption/decryption for all upload paths
- SHA-1→SHA-256 block ID mapping for Seafile client compatibility

---

## CRITICAL: Block ID Mapping System

### Why This Exists
Seafile desktop/mobile clients use **SHA-1** block IDs (40 chars), but we store blocks with **SHA-256** hashes (64 chars) for security. The `block_id_mappings` table translates between them.

### Database Table: `block_id_mappings`
```sql
CREATE TABLE sesamefs.block_id_mappings (
    org_id UUID,
    external_id TEXT,      -- SHA-1 (40 chars) - what Seafile clients use
    internal_id TEXT,      -- SHA-256 (64 chars) - how we store blocks
    created_at TIMESTAMP,
    PRIMARY KEY (org_id, external_id)
);
```

### Block Storage Flow

**Upload (seafhttp.go, onlyoffice.go):**
```
1. Receive file content
2. If encrypted library: encrypt content with file key (AES-256-CBC)
3. Compute SHA-1 hash of ORIGINAL content → external_id (for fs_object)
4. Compute SHA-256 hash of STORED content → internal_id (for storage)
5. Store block to S3 using internal_id
6. INSERT INTO block_id_mappings (org_id, external_id, internal_id)
7. Create fs_object with external_id in block_ids array
```

**Download (sync.go GetBlock):**
```
1. Receive request for external_id (SHA-1)
2. SELECT internal_id FROM block_id_mappings WHERE external_id = ?
3. Fetch block from S3 using internal_id
4. Return encrypted block to client (client decrypts locally)
```

### Code Locations for Block Mapping

| Operation | File | Function |
|-----------|------|----------|
| Create mapping (upload) | `internal/api/seafhttp.go` | `HandleUpload()` |
| Create mapping (OnlyOffice) | `internal/api/v2/onlyoffice.go` | `saveEditedDocument()` |
| Resolve mapping (download) | `internal/api/sync.go` | `GetBlock()` |

### ⚠️ IMPORTANT: Always Use Correct Table/Column Names
- Table: `block_id_mappings` (NOT `block_mapping`)
- Columns: `external_id`, `internal_id` (NOT `sha1_id`, `sha256_id`)
- Query pattern: `SELECT internal_id FROM block_id_mappings WHERE org_id = ? AND external_id = ?`

---

## CRITICAL: Encrypted Library Flow

### Decrypt Session Management
File keys for unlocked libraries are stored in memory (`DecryptSessionManager`):
- Key: `userID:repoID`
- Value: `{UnlockedAt, FileKey}`
- TTL: 1 hour

**Code:** `internal/api/v2/encryption.go`

### File Encryption Format (AES-256-CBC)
```
[16-byte IV][encrypted content with PKCS7 padding]
```

**Code:** `internal/crypto/crypto.go` - `EncryptBlock()`, `DecryptBlock()`

### ⚠️ CRITICAL: PBKDF2 Key Derivation (Fixed 2026-01-13)
Seafile uses **TWO separate PBKDF2 calls** for key/IV derivation:
```go
// CORRECT - two separate PBKDF2 calls
key := pbkdf2.Key(input, salt, 1000, 32, sha256.New)  // 1000 iterations for key
iv := pbkdf2.Key(key, salt, 10, 16, sha256.New)       // 10 iterations for IV, using KEY as input
```

**CRITICAL: Different Input for Magic vs Random Key Encryption:**
```go
// Magic (password verification) uses repo_id + password:
magicKey, _ := DeriveKeyPBKDF2(password, repoID, salt, version)  // input = repo_id + password
magic := hex.EncodeToString(magicKey)

// Random key encryption uses PASSWORD ONLY:
encKey, encIV := DeriveEncryptionKeyPBKDF2(password, salt, version)  // input = password only
randomKey := AES_CBC_Encrypt(secretKey, encKey, encIV)
```

**Static salt for enc_version 2:** `{0xda, 0x90, 0x45, 0xc3, 0x06, 0xc7, 0xcc, 0x26}`

**Code:** `internal/crypto/crypto.go` - `DeriveKeyPBKDF2()`, `DeriveEncryptionKeyPBKDF2()`

### Upload to Encrypted Library
```
1. Check if library is encrypted (SELECT encrypted FROM libraries)
2. Get file key from session: GetDecryptSessions().GetFileKey(userID, repoID)
3. If no file key → return 403 "library is encrypted and not unlocked"
4. Encrypt content: crypto.EncryptBlock(content, fileKey)
5. Store encrypted block with SHA-1→SHA-256 mapping
6. fs_object stores ORIGINAL file size (not encrypted size)
```

### API Response: `lib_need_decrypt`
The `lib_need_decrypt` field in library API responses tells frontend if password dialog is needed:
- `true`: Library is encrypted AND not unlocked for this user
- `false`: Library is not encrypted OR already unlocked

**Code:** `internal/api/v2/libraries.go` line ~880

---

## Recent Changes (2026-01-14)

### Sync Protocol Compatibility Fixes
**Issue**: Seafile desktop client sync was failing due to response format mismatches
**Root Cause**: Several API endpoints had incorrect JSON field types and ordering

**Fixes Applied**:

1. **`is_corrupted` field type** - Changed from `false` (boolean) to `0` (integer)
   - Files: `internal/api/sync.go:186,192,1277`, `internal/api/v2/files.go:1815`

2. **Commit object format** - Removed unconditional `no_local_history`, always include `repo_desc`
   - File: `internal/api/sync.go:125,289`

3. **FSEntry struct field order** - Changed to alphabetical order for correct fs_id hash computation
   - **Before**: `name`, `id`, `mode`, `mtime`, `size`, `modifier`
   - **After**: `id`, `mode`, `modifier`, `mtime`, `name`, `size`
   - Files: `internal/api/sync.go:149-156`, `internal/api/v2/files.go:58-67`
   - **Impact**: Critical - fs_id is SHA-1 of JSON with alphabetically sorted keys

4. **check-fs endpoint** - Now accepts JSON array input, returns JSON array output
   - File: `internal/api/sync.go:1099-1145`

5. **check-blocks endpoint** - Now accepts JSON array input, returns JSON array output
   - File: `internal/api/sync.go:601-706`

**Verification**: All endpoints now match reference Seafile server (app.nihaoconsult.com) responses

---

## Recent Changes (2026-01-08)

### Encrypted Library Support
**Feature**: Full encrypted library password management with strong security
**Implementation**:
- Created `internal/crypto/crypto.go` with dual-mode encryption:
  - **Argon2id** (strong, 64MB memory, 3 iterations) for web/API clients
  - **PBKDF2** (1000 iterations) for Seafile desktop/mobile client compatibility
- Added `POST /api/v2.1/repos/{id}/set-password/` - verify password (unlock library)
- Added `PUT /api/v2.1/repos/{id}/set-password/` - change password
- Database columns: `salt`, `magic_strong`, `random_key_strong`
- Fixed modal dialogs: `lib-decrypt-dialog.js`, `change-repo-password-dialog.js`
**Security**: 300× slower brute-force compared to Seafile's default PBKDF2
**Files**: `internal/crypto/crypto.go`, `internal/api/v2/encryption.go`, `internal/api/v2/libraries.go`
**Docs**: [docs/ENCRYPTION.md](docs/ENCRYPTION.md)

### Library Starring Fix
**Issue**: Starred libraries weren't persisting after page refresh
**Root Cause**: Cassandra query in `ListLibrariesV21` was invalid - couldn't filter by `path` without also filtering by `repo_id` (clustering key order)
**Fix**: Query all starred items for user, filter by `path="/"` in Go code
```go
// Query all starred files for user, filter for libraries (path="/")
starIter := h.db.Session().Query(`
    SELECT repo_id, path FROM starred_files WHERE user_id = ?
`, userID).Iter()
for starIter.Scan(&starredRepoID, &starredPath) {
    if starredPath == "/" {
        starredLibs[starredRepoID] = true
    }
}
```
**File**: `internal/api/v2/libraries.go:678-693`

### OnlyOffice Simplified Config
**Issue**: OnlyOffice documents opened in view-only mode (toolbar grayed out)
**Fix**: Simplified OnlyOffice config to match Seahub's minimal approach:
- Reduced customization to only `forcesave` and `submitForm`
- Added `fillForms: true` to permissions
- URL translation for Docker networking (`localhost:8088` → `onlyoffice:80`)
**Files**: `internal/api/v2/onlyoffice.go`, `internal/config/config.go`

### Multi-host Frontend Support
**Issue**: Frontend hardcoded to single backend URL
**Fix**: Empty `serviceURL` config uses `window.location.origin` automatically
```javascript
window.app.config.serviceURL = '';  // Uses window.location.origin
```
**File**: `frontend/public/index.html`

### Modal Dialog Fixes
Fixed dialogs to use plain Bootstrap modal classes instead of reactstrap Modal:
- `rename-dialog.js` - Rename (wiki context)
- `rename-dirent.js` - Rename file/folder

## Quick Commands

```bash
# Run tests
go test ./...

# Run with coverage
go test ./... -coverprofile=coverage.out

# Start dev server
go run cmd/sesamefs/main.go

# Docker compose
docker-compose up -d
```

## Frontend Development

**Full guide**: [docs/FRONTEND.md](docs/FRONTEND.md) - Complete setup, patterns, Docker, troubleshooting

### Quick Reference

```bash
# Docker build caching fix (if changes don't appear)
docker-compose stop frontend && docker-compose rm -f frontend && docker rmi cool-storage-api-frontend
docker-compose build --no-cache frontend
docker-compose up -d frontend

# Local dev (faster iteration)
cd frontend && npm install && npm start  # runs on port 3001
```

### Key Files

| File | Purpose |
|------|---------|
| `frontend/src/models/dirent.js` | Parses API response (is_locked, file_tags, etc.) |
| `frontend/src/components/dirent-list-view/` | Directory listing, file rows, lock icons |
| `frontend/src/components/dialog/` | Modal dialogs (share, rename, tags) |
| `frontend/src/utils/seafile-api.js` | API client wrapper |
| `frontend/src/css/dirent-list-item.css` | File row styling, lock icon positioning |

### Adding New File Properties

1. **Backend**: Add to `Dirent` struct in `internal/api/v2/files.go`
2. **Frontend model**: Parse in `src/models/dirent.js` constructor
3. **Component**: Render: `{dirent.property && <Component/>}`

---

## Frontend Critical Context

> **Source of Truth**: This section consolidates key info from [docs/FRONTEND.md](docs/FRONTEND.md)

### Architecture & Data Flow
```
User Action → Component Handler → seafile-api.js → Backend API
                                        ↓
Component Render ← React State ← Dirent Model ← API Response
```

### Global Configuration (CHECK FIRST)
Frontend reads from `window.app.config` in `public/index.html`:
```javascript
window.app = {
  config: {
    serviceURL: '',  // Empty = use window.location.origin (multi-host support)
    mediaUrl: '/static/',                  // Icons/assets base
    siteRoot: '/',                         // App root
    fileServerRoot: window.location.origin + '/seafhttp',  // File server
  }
};
```
**Constants file**: `src/utils/constants.js` exports these values.

**Multi-host deployment**: `serviceURL` is empty by default. The `seafile-api.js` client uses `window.location.origin` when serviceURL is empty, allowing the same frontend build to work on us.sesamefs.com, eu.sesamefs.com, etc.

For local dev with different ports (frontend on 3001, backend on 8080):
```javascript
window.SESAMEFS_API_URL = 'http://localhost:8080';
```

### Icon Path Patterns (COMMON ISSUE SOURCE)
| Asset | URL Pattern | Files Needed |
|-------|-------------|--------------|
| Folder | `{mediaUrl}img/folder-{24\|192}.png` | `folder-24.png`, `folder-192.png` |
| Folder (read-only) | `{mediaUrl}img/folder-read-only-{24\|192}.png` | Same with `-read-only` |
| File types | `{mediaUrl}img/file/{24\|192}/{ext}.png` | `pdf.png`, `excel.png`, etc. |
| Libraries | `{mediaUrl}img/lib/{24\|48\|256}/{type}.png` | `lib.png`, `lib-readonly.png` |
| Lock overlay | `{mediaUrl}img/file-locked-32.png` | Single file |

**HiDPI Logic** (`utils.js`): `isHiDPI() ? 48 : 24` → then `size > 24 ? 192 : 24`
- Normal: requests 24px icons
- Retina: requests 192px icons (48→192 mapping)

### API Client: seafile-js (CANNOT MODIFY)
The `seafile-js` npm package has **hardcoded paths**. Backend MUST match:

| Frontend Call | HTTP Request |
|---------------|--------------|
| `seafileAPI.deleteRepo(id)` | `DELETE /api/v2.1/repos/{id}/` |
| `seafileAPI.listRepos()` | `GET /api/v2.1/repos/` |
| `seafileAPI.renameRepo(id, name)` | `POST /api2/repos/{id}/?op=rename` |
| `seafileAPI.listDir(repoId, path)` | `GET /api/v2.1/repos/{id}/dir/?p={path}` |
| `seafileAPI.lockfile(repoId, path)` | `PUT /api/v2.1/repos/{id}/file/?p={path}` + `operation=lock` |

### Token Authentication
```javascript
// Stored in localStorage
const TOKEN_KEY = 'sesamefs_auth_token';

// All API requests use:
headers: { 'Authorization': 'Token ' + token }  // NOT "Bearer"
```

### Component Data Flow Example: Delete Library
```
1. User clicks trash icon on library row
   → MylibRepoListItem.onDeleteToggle()
   → sets state.isDeleteDialogShow = true

2. DeleteRepoDialog renders (modal)
   → componentDidMount fetches share info

3. User clicks Delete button
   → DeleteRepoDialog.onDeleteRepo()
   → calls this.props.onDeleteRepo(repo)

4. Parent handler executes
   → MylibRepoListItem.onDeleteRepo(repo)
   → seafileAPI.deleteRepo(repo.repo_id)

5. On success:
   → this.props.onDeleteRepo(repo) notifies grandparent
   → list re-renders without deleted item
```

### Required Backend Response Formats

**Library List** (`GET /api/v2.1/repos/`):
```json
{ "repos": [{ "repo_id": "uuid", "repo_name": "str", "type": "mine", "permission": "rw" }] }
```

**Directory List** (`GET /api/v2.1/repos/{id}/dir/?p=/`):
```json
{ "dirent_list": [{ "name": "str", "type": "file|dir", "mtime": 123, "permission": "rw" }] }
```

**Delete Success** (`DELETE /api/v2.1/repos/{id}/`):
```json
{ "success": true }
```

### Modal Pattern (CRITICAL - Common Bug Source)

**Problem**: Seafile frontend uses `ModalPortal` wrapper for dialog rendering. The reactstrap `<Modal>` component does NOT render properly inside `ModalPortal` because reactstrap Modal creates its own portal, resulting in double-portal issues.

**Solution**: Use plain Bootstrap modal classes instead of reactstrap Modal:

```jsx
// ❌ BROKEN - reactstrap Modal inside ModalPortal
import { Modal, ModalHeader, ModalBody, ModalFooter } from 'reactstrap';
<Modal isOpen={true} toggle={this.toggle}>
  <ModalHeader>Title</ModalHeader>
  <ModalBody>Content</ModalBody>
  <ModalFooter>Buttons</ModalFooter>
</Modal>

// ✅ WORKING - plain Bootstrap modal classes
<div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
  <div className="modal-dialog modal-dialog-centered">
    <div className="modal-content">
      <div className="modal-header">
        <h5 className="modal-title">Title</h5>
        <button type="button" className="btn-close" onClick={this.toggle} aria-label="Close"></button>
      </div>
      <div className="modal-body">Content</div>
      <div className="modal-footer">
        <Button color="secondary" onClick={this.toggle}>Cancel</Button>
        <Button color="primary" onClick={this.handleSubmit}>Submit</Button>
      </div>
    </div>
  </div>
</div>
```

**Files already fixed** (using plain Bootstrap modal classes):

| Dialog | Purpose | Status |
|--------|---------|--------|
| `delete-repo-dialog.js` | Library deletion | ✅ Fixed |
| `create-repo-dialog.js` | New library creation | ✅ Fixed |
| `batch-delete-repo-dialog.js` | Batch library deletion | ✅ Fixed |
| `delete-folder-dialog.js` | Folder deletion | ✅ Fixed |
| `create-folder-dialog.js` | Create folder | ✅ Fixed |
| `create-file-dialog.js` | Create file | ✅ Fixed |
| `rename-dialog.js` | Rename (wiki context) | ✅ Fixed |
| `rename-dirent.js` | Rename file/folder | ✅ Fixed |
| `share-dialog.js` | Share files/folders | ✅ Fixed |
| `copy-dirent-dialog.js` | Copy file/folder | ✅ Fixed |
| `move-dirent-dialog.js` | Move file/folder | ✅ Fixed |
| `lib-decrypt-dialog.js` | Decrypt encrypted library | ✅ Fixed |
| `change-repo-password-dialog.js` | Change library password | ✅ Fixed |

**Note**: You can still use reactstrap `Button`, `Form`, `Input`, `Alert` etc. inside the modal body - just not the Modal wrapper components.

---

## Pending Modal Dialog Fixes

> **IMPORTANT**: ~100+ dialog files still use reactstrap Modal and need migration.
> Run this to find them: `grep -l "import.*Modal.*from 'reactstrap'" frontend/src/components/dialog/**/*.js`

### Priority 1: User-Facing Dialogs (Fix First)
These dialogs are encountered by regular users:

| Dialog | Purpose | File |
|--------|---------|------|
| `create-group-dialog.js` | Create new group | `dialog/` |
| `rename-group-dialog.js` | Rename group | `dialog/` |
| `leave-group-dialog.js` | Leave a group | `dialog/` |
| `dismiss-group-dialog.js` | Dismiss/delete group | `dialog/` |
| `transfer-group-dialog.js` | Transfer group ownership | `dialog/` |
| `manage-members-dialog.js` | Manage group members | `dialog/` |
| `create-tag-dialog.js` | Create file tag | `dialog/` |
| `edit-filetag-dialog.js` | Edit file tags | `dialog/` |
| `list-taggedfiles-dialog.js` | List files with tag | `dialog/` |
| `invite-people-dialog.js` | Invite users | `dialog/` |
| `about-dialog.js` | About dialog | `dialog/` |
| `clean-trash.js` | Clean trash | `dialog/` |
| `confirm-restore-repo.js` | Restore deleted repo | `dialog/` |
| `upload-remind-dialog.js` | Upload reminder | `dialog/` |
| `zip-download-dialog.js` | ZIP download progress | `dialog/` |
| `search-file-dialog.js` | Search files | `dialog/` |
| `internal-link-dialog.js` | Internal link | `dialog/` |

### Priority 2: Share & Link Dialogs
| Dialog | Purpose |
|--------|---------|
| `share-repo-dialog.js` | Share repository |
| `share-admin-link.js` | Admin share link |
| `view-link-dialog.js` | View share link |
| `generate-upload-link.js` | Generate upload link |
| `share-to-user.js` | Share to user |
| `share-to-group.js` | Share to group |
| `share-to-invite-people.js` | Share invite |
| `share-to-other-server.js` | OCM sharing |
| `repo-share-admin-dialog.js` | Share admin |

### Priority 3: Library Settings Dialogs
| Dialog | Purpose |
|--------|---------|
| `lib-history-setting-dialog.js` | History settings |
| `lib-old-files-auto-del-dialog.js` | Auto-delete old files |
| `lib-sub-folder-permission-dialog.js` | Subfolder permissions |
| `lib-sub-folder-set-user-permission-dialog.js` | User folder perms |
| `lib-sub-folder-set-group-permission-dialog.js` | Group folder perms |
| `reset-encrypted-repo-password-dialog.js` | Reset encrypted password |
| `repo-api-token-dialog.js` | API token management |
| `repo-seatable-integration-dialog.js` | SeaTable integration |
| `label-repo-state-dialog.js` | Label repo state |
| `edit-repo-commit-labels.js` | Edit commit labels |

### Priority 4: Organization Admin Dialogs (`dialog/`)
| Dialog | Purpose |
|--------|---------|
| `org-add-user-dialog.js` | Add org user |
| `org-add-member-dialog.js` | Add org member |
| `org-add-admin-dialog.js` | Add org admin |
| `org-add-department-dialog.js` | Add department |
| `org-add-repo-dialog.js` | Add org repo |
| `org-delete-member-dialog.js` | Delete member |
| `org-delete-department-dialog.js` | Delete department |
| `org-delete-repo-dialog.js` | Delete org repo |
| `org-rename-department-dialog.js` | Rename department |
| `org-set-group-quota-dialog.js` | Set group quota |
| `org-import-users-dialog.js` | Import users |
| `org-admin-invite-user-dialog.js` | Invite user |
| `org-admin-invite-user-via-weixin-dialog.js` | WeChat invite |
| `org-logs-file-update-detail.js` | File update logs |
| `set-org-user-quota.js` | Set user quota |
| `set-org-user-name.js` | Set user name |
| `set-org-user-contact-email.js` | Set contact email |

### Priority 5: System Admin Dialogs (`dialog/sysadmin-dialog/`)
All files in `frontend/src/components/dialog/sysadmin-dialog/` need fixing:
- `sysadmin-add-user-dialog.js`
- `sysadmin-delete-member-dialog.js`
- `sysadmin-delete-repo-dialog.js`
- `sysadmin-create-repo-dialog.js`
- `sysadmin-share-dialog.js`
- `sysadmin-import-user-dialog.js`
- `sysadmin-logs-export-excel-dialog.js`
- ... and ~15 more files

### Priority 6: Other/Rare Dialogs
| Dialog | Purpose |
|--------|---------|
| `wiki-delete-dialog.js` | Delete wiki |
| `wiki-select-dialog.js` | Select wiki |
| `new-wiki-dialog.js` | New wiki |
| `import-members-dialog.js` | Import members |
| `import-dingtalk-department-dialog.js` | DingTalk import |
| `import-work-weixin-department-dialog.js` | WeChat Work import |
| `confirm-disconnect-dingtalk.js` | Disconnect DingTalk |
| `confirm-disconnect-wechat.js` | Disconnect WeChat |
| `confirm-delete-account.js` | Delete account |
| `confirm-unlink-device.js` | Unlink device |
| `confirm-apply-folder-properties-dialog.js` | Apply folder props |
| `terms-editor-dialog.js` | Terms editor |
| `terms-preview-dialog.js` | Terms preview |
| `guide-for-new-dialog.js` | New user guide |
| `add-abuse-report-dialog.js` | Abuse report |
| `invitation-revoke-dialog.js` | Revoke invitation |
| `set-webdav-password.js` | Set WebDAV password |
| `reset-webdav-password.js` | Reset WebDAV password |
| `remove-webdav-password.js` | Remove WebDAV password |
| `copy-move-dirent-progress-dialog.js` | Copy/move progress |
| `insert-file-dialog.js` | Insert file |
| `insert-repo-image-dialog.js` | Insert repo image |
| `list-created-files-dialog.js` | List created files |
| `save-shared-file-dialog.js` | Save shared file |
| `save-shared-dir-dialog.js` | Save shared dir |
| `transfer-dialog.js` | Transfer ownership |
| `common-operation-confirmation-dialog.js` | Generic confirm |
| `create-department-repo-dialog.js` | Dept repo |
| `extra-attributes-dialog/index.js` | Extra attributes |

### How to Fix a Dialog

1. **Change import** - Remove Modal components:
```jsx
// Before
import { Button, Modal, ModalHeader, ModalBody, ModalFooter, Alert } from 'reactstrap';
// After
import { Button, Alert } from 'reactstrap';
```

2. **Replace render** - Use plain Bootstrap classes:
```jsx
// Before
<Modal isOpen={true} toggle={this.toggle}>
  <ModalHeader toggle={this.toggle}>Title</ModalHeader>
  <ModalBody>Content</ModalBody>
  <ModalFooter>
    <Button onClick={this.toggle}>Cancel</Button>
  </ModalFooter>
</Modal>

// After
<div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
  <div className="modal-dialog modal-dialog-centered">
    <div className="modal-content">
      <div className="modal-header">
        <h5 className="modal-title">Title</h5>
        <button type="button" className="btn-close" onClick={this.toggle} aria-label="Close"></button>
      </div>
      <div className="modal-body">Content</div>
      <div className="modal-footer">
        <Button color="secondary" onClick={this.toggle}>Cancel</Button>
      </div>
    </div>
  </div>
</div>
```

3. **Handle onOpened callback** - If dialog uses `onOpened` for focus:
```jsx
componentDidMount() {
  // Focus input after mount (replaces Modal's onOpened)
  setTimeout(() => {
    if (this.inputRef.current) {
      this.inputRef.current.focus();
    }
  }, 100);
}
```

4. **Test the dialog** - Verify it opens, closes, and submits correctly

### Frontend Debugging Checklist

| Symptom | Check First | Likely Fix |
|---------|-------------|------------|
| Icons not loading | Network tab for 404s | Add missing icon file, hard refresh |
| API call fails | Network tab request/response | Check backend returns exact format |
| Button click does nothing | Console for errors | Check handler is bound, state flows |
| Changes not appearing | Docker build time (<10s = cached) | `docker-compose build --no-cache frontend` |
| "Invalid token" | Request headers | Must be `Token xyz` not `Bearer xyz` |
| Component not updating | React DevTools state | Check callback updates parent state |
| **Modal not visible** | Check if using reactstrap Modal | **Use plain Bootstrap modal classes** (see pattern above) |

### Key Files Quick Reference

| What | File |
|------|------|
| API wrapper | `src/utils/seafile-api.js` |
| Config constants | `src/utils/constants.js` |
| Icon URL logic | `src/utils/utils.js` → `getDirentIcon()`, `getFolderIconUrl()` |
| File/folder model | `src/models/dirent.js` |
| Directory list | `src/components/dirent-list-view/dirent-list-item.js` |
| Library list | `src/pages/my-libs/mylib-repo-list-item.js` |
| Delete library dialog | `src/components/dialog/delete-repo-dialog.js` |
| Global config | `public/index.html` → `window.app.config` |
