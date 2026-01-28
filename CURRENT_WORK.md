# Current Work - SesameFS

**Last Updated**: 2026-01-28
**Session**: Frontend Permission UI Improvements

**📏 File Size Rule**: Keep this file under **500 lines** unless unavoidable. Move detailed content to:
- `docs/KNOWN_ISSUES.md` - Detailed bug tracking
- `docs/CHANGELOG.md` - Session history
- `docs/IMPLEMENTATION_STATUS.md` - Component status
- Other appropriate documentation files

---

## 🚀 NEW SESSION? START HERE

**NEXT SESSION PRIORITY**: 🟡 Frontend Permission UI Completion

**Then review**:
1. **"What's Next"** → Top priorities (work on #1 unless user specifies)
2. **"Frozen Components"** → What NOT to touch (breaks desktop clients)
3. **"Critical Context"** → Essential facts to remember

### Quick Context
1. **Test infrastructure**: Unified test runner created (`./scripts/test.sh`)
2. **Batch move/copy**: Backend 100% complete, ready for frontend integration
3. **Permission system**: Backend 100% complete, Frontend ~70% complete (toolbars done)
4. **All tests passing**: 78 integration + Go unit tests

### Step 2: Before Making ANY Code Changes
- ✅ Check `docs/IMPLEMENTATION_STATUS.md` - Is component 🔒 FROZEN?
- ✅ If FROZEN → DO NOT MODIFY without explicit user approval
- ✅ If ✅ COMPLETE → Modify with caution, verify tests pass
- ✅ If 🟡 PARTIAL / ❌ TODO → Safe to actively develop

### Step 3: At End of Session - Update Documentation
**📋 MANDATORY: Run [docs/SESSION_CHECKLIST.md](docs/SESSION_CHECKLIST.md)**

---

## Last Session Summary ✅

**Date**: 2026-01-28
**Focus**: Modal Pattern Fixes for Library Menu Dialogs

### Completed This Session (Session 3)

- ✅ **Fixed 15 Modal Dialogs (Modal Pattern Fix)**
  - **Issue**: Dialogs using reactstrap Modal inside ModalPortal don't render
  - **Root Cause**: reactstrap Modal creates its own portal, breaks inside ModalPortal
  - **Fix**: Converted all affected dialogs to plain Bootstrap modal classes
  - **Files Fixed**:
    - `frontend/src/components/dialog/transfer-dialog.js`
    - `frontend/src/components/dialog/lib-history-setting-dialog.js`
    - `frontend/src/components/dialog/reset-encrypted-repo-password-dialog.js`
    - `frontend/src/components/dialog/label-repo-state-dialog.js`
    - `frontend/src/components/dialog/lib-sub-folder-permission-dialog.js`
    - `frontend/src/components/dialog/repo-api-token-dialog.js`
    - `frontend/src/components/dialog/repo-seatable-integration-dialog.js`
    - `frontend/src/components/dialog/lib-old-files-auto-del-dialog.js`
    - `frontend/src/components/dialog/edit-filetag-dialog.js`
    - `frontend/src/components/dialog/create-tag-dialog.js`
  - **Already fixed (previous sessions)**:
    - `delete-repo-dialog.js`, `change-repo-password-dialog.js`, `share-dialog.js`
    - `repo-share-admin-dialog.js`, `list-taggedfiles-dialog.js`

- ✅ **Frontend Build Verified**
  - All changes compile without errors
  - npm run build succeeds

- ✅ **Frontend Tests Significantly Expanded**
  - **Before**: 4 test files, 105 tests
  - **After**: 6 test files, 138 tests (+33 tests)
  - New test files created:
    - `frontend/src/components/dialog/__tests__/modal-pattern.test.js` - Modal pattern verification
    - `frontend/src/utils/__tests__/seafile-api-tags.test.js` - Tag API methods verification
    - `frontend/src/pages/__tests__/permission-checks.test.js` - Permission system verification
  - Fixed existing tests:
    - Fixed mocks in `dirent-list-item.test.js` (added bytesToSize, isSdocFile)
    - Removed broken @testing-library/react imports
  - All tests pass: `./scripts/test.sh frontend`

### Previous Session (Session 2 - 2026-01-28)

- ✅ **Fixed Create Repo Tag 500 Error** - Replaced Cassandra LWT with simple SELECT/INSERT
- ✅ **Fixed Share Admin Dialog Not Opening** - Modal pattern fix
- ✅ **Fixed Tagged Files Dialog Not Opening** - Modal pattern fix
- ✅ **Added Tag API Methods** - 9 methods added to seafile-api.js
- ✅ **Fixed Change Password Menu** - Only shows for encrypted libraries

### Previous Session (Session 1 - 2026-01-28)

- ✅ **Added Global Permission Checks to Frontend Components**
- ✅ **Fixed File Tags 500 Error** - Counter batch separation
- ✅ **Fixed Copy/Move Dialog Empty Library List** - apiPermission() helper
- ✅ **Fixed Tagged Files Feature** - Backend + Frontend API methods

### Previous Session (2026-01-28 - Test Infrastructure)

- ✅ Fixed batch move/copy operations (TraverseToPath bug)
- ✅ Fixed nested directory creation
- ✅ Improved test scripts (unique names, cleanup)
- ✅ Created test-batch-operations.sh (19 tests)

### Batch Operations API

**Sync Move** (same repo):
```bash
curl -X POST "http://localhost:8080/api/v2.1/repos/sync-batch-move-item/" \
  -H "Authorization: Token dev-token-admin" \
  -d '{"src_repo_id":"...", "src_parent_dir":"/", "dst_repo_id":"...", "dst_parent_dir":"/dest", "src_dirents":["folder1"]}'
# Response: {"success":true}
```

**Async Move** (cross repo, returns task_id):
```bash
curl -X POST "http://localhost:8080/api/v2.1/repos/async-batch-move-item/" \
  ...
# Response: {"task_id":"uuid-xxx"}

curl "http://localhost:8080/api/v2.1/copy-move-task/?task_id=uuid-xxx"
# Response: {"done":true,"successful":1,"failed":0,"total":1}
```

---

## What's Next (Priority Order) 🎯

### 🟡 PRIORITY 1: Complete Frontend Permission UI

**Status**: ~70% complete - Toolbars and main views done, some edge cases remain

**Remaining UI Elements to Hide/Disable for Readonly/Guest**:

| Feature | Location | Status |
|---------|----------|--------|
| Upload button | `dir-operation-toolbar.js` | ✅ DONE |
| New folder button | `dir-operation-toolbar.js` | ✅ DONE |
| Delete file/folder | `utils.js` (getFolderOperationList/getFileOperationList) | ✅ DONE |
| Rename file/folder | `utils.js` (operation lists) | ✅ DONE |
| Move button | `multiple-dir-operation-toolbar.js` | ✅ DONE |
| Share library button | `lib-content-view.js` | ✅ DONE |
| New Group button | `groups-toolbar.js` | ✅ DONE (was already done) |
| Drag & drop upload | `lib-content-view.js` | ✅ DONE |
| Copy button (readonly) | `multiple-dir-operation-toolbar.js` | ✅ DONE |

**Implementation Pattern**:
```javascript
// Check permission dynamically (not from import)
const userCanWrite = window.app.pageOptions.canAddRepo;
{userCanWrite && <UploadButton ... />}
```

---

### ✅ PRIORITY 2: File Operations Backend - COMPLETE

**Status**: ✅ All batch operations implemented
- `POST /api/v2.1/repos/sync-batch-move-item/` ✅
- `POST /api/v2.1/repos/sync-batch-copy-item/` ✅
- `POST /api/v2.1/repos/async-batch-move-item/` ✅
- `POST /api/v2.1/repos/async-batch-copy-item/` ✅
- `GET /api/v2.1/copy-move-task/?task_id=xxx` ✅

**Frontend Ready**: `move-dirent-dialog.js`, `copy-dirent-dialog.js` exist

---

### 🔴 PRIORITY 2: OIDC Integration (Production Critical)

**Status**: NOT STARTED - Design documented
**Documentation**: [docs/OIDC.md](docs/OIDC.md)

**OIDC Provider** (Test Environment):
- **URL**: https://t-accounts.sesamedisk.com/
- **Client ID**: 657640
- **Client Secret**: See `.reference.md`

**Purpose**: Replace dev token authentication with real OIDC login
- User authentication and provisioning
- Organization/tenant management
- Role synchronization

**Implementation Phases**:
1. Basic OIDC login flow (login redirect, callback, session)
2. Organization/tenant mapping from OIDC claims
3. Role synchronization from OIDC provider

**Files to Create**:
- `internal/auth/oidc.go` - OIDC authentication logic
- `internal/auth/session.go` - Session management

---

### 🟡 PRIORITY 3: Library Advanced Settings

**Status**: ❌ Backend NOT IMPLEMENTED, frontend dialogs now work after modal fixes

| Feature | Frontend | Backend | Endpoint |
|---------|----------|---------|----------|
| Watch/Unwatch | ✅ Works | ❌ 404 | `POST /api/v2.1/monitored-repos/` |
| History Setting | ✅ Works | ❌ Missing | `GET/PUT /api/v2.1/repos/{id}/history-limit/` |
| API Token | ✅ Works | ❌ Missing | `GET/POST /api/v2.1/repos/{id}/repo-api-tokens/` |
| Auto Deletion | ✅ Works | ❌ Missing | `GET/PUT /api/v2.1/repos/{id}/auto-delete/` |
| Library Transfer | ✅ Works | ❌ Missing | `PUT /api2/repos/{id}/owner/` |

**See**: [docs/KNOWN_ISSUES.md](docs/KNOWN_ISSUES.md) → "LIBRARY SETTINGS NOT IMPLEMENTED"

---

### 📋 Full Roadmap

See **Strategic Roadmap** section below for complete feature list.

---

## Strategic Roadmap

### Phase 1: Complete Missing Backend (Weeks 1-4)

**1.1 File Operations Backend** ✅ COMPLETE
- ~~Complete move/copy file endpoints~~ ✅ Done 2026-01-27
- **Frontend Ready**: All operation dialogs exist ✅

**1.2 Sharing System Backend** ⭐ HIGH
- Implement share to users backend (`POST /api/v2.1/file-shares/`)
- Implement share to groups backend
- Implement share links CRUD
- **Frontend Ready**: All sharing dialogs exist ✅

**1.3 Groups Backend** ⭐ MEDIUM-HIGH
- Implement create/rename/delete group
- Implement add/remove members
- **Frontend Ready**: All group dialogs exist ✅

**1.4 Library Ownership Features** ⭐ MEDIUM
- Library transfer endpoint (`PUT /api2/repos/{repo_id}/owner/`)
- Multiple owners / group ownership support
- **See**: [docs/KNOWN_ISSUES.md](docs/KNOWN_ISSUES.md) for design requirements

### Phase 2: Production Readiness (Week 5+)

**2.1 OIDC Authentication** - 🔥 CRITICAL for production
- OIDC login flow with test provider (https://t-accounts.sesamedisk.com/)
- User/organization provisioning from OIDC claims
- Role synchronization
- Session management (JWT/cookies)
- **Documentation**: [docs/OIDC.md](docs/OIDC.md)
- **Priority**: HIGH - Required for real users

**2.2 Garbage Collection** - 🔥 CRITICAL for production
- Block GC worker (delete ref_count=0 blocks)
- Commit cleanup (version_ttl_days)
- Expired share link cleanup
- **Priority**: Must implement before production

**2.3 Additional Authentication Features** - MEDIUM
- Password change functionality (if supported by OIDC)
- Session timeout/refresh

**2.3 Error Handling & Monitoring** - HIGH
- Structured logging
- Metrics/monitoring (Prometheus)
- Health check endpoints

---

## Frozen/Stable Components 🔒

### ⚠️ CRITICAL: Sync Code FROZEN (2026-01-19)
**User directive**: DO NOT MODIFY sync code without explicit approval

### Code Files - Sync Protocol 🔒
- `internal/crypto/crypto.go` - PBKDF2 implementation
- `internal/api/sync.go` (lines 949-952, 125-130, 1405-1492) - Protocol formats
- `internal/api/v2/encryption.go` - Password endpoints

### Code Files - Web Downloads 🔒 (Frozen 2026-01-20)
- `internal/api/seafhttp.go:1253-1317` - `findEntryInDir()` (file lookup)
- `internal/api/seafhttp.go:1034-1189` - `getFileFromBlocks()` (block retrieval)
- `internal/api/seafhttp.go:963-1030` - `HandleDownload()` (token validation)

### Frontend Components 🔒 (Frozen 2026-01-23)
- `frontend/src/pages/my-libs/` - Library list view
- `frontend/src/pages/starred/` - Starred files & libraries
- `frontend/src/components/dirent-list-view/` - File download functionality

### Protocol Behaviors 🔒
- fs-id-list: JSON array (NOT newline-separated)
- Commit objects: OMIT `no_local_history` field
- `encrypted` field: integer in download-info, string in commits
- `is_corrupted` field: integer 0 (NOT boolean)
- `/seafhttp/` auth: `Seafile-Repo-Token` header (NOT `Authorization`)

---

## Critical Context for Next Session 📝

### 🎯 Project Goal
**Mission**: Build complete Seafile replacement ready for production
**Target Users**: Global cloud storage, especially needing China access
**Timeline**: ASAP but thorough - "want it soon, do it right"

### 📊 Current State
- **Sync Protocol**: 100% working, desktop clients fully compatible 🔒 FROZEN
- **Backend API**: ~85% implemented (permissions complete, batch operations complete!)
- **Frontend UI**: ~65% functional (permissions started)
- **Production Ready**: NO - missing OIDC, GC, monitoring

### Critical Facts to Remember

**Permissions System** (UPDATED 2026-01-27):
- Backend: ✅ 100% COMPLETE - All endpoints check permissions
- Frontend: 🟡 ~30% - "New Library" button done, many features remain
- API returns: `can_add_repo`, `can_share_repo`, `can_add_group`, etc.
- Check `window.app.pageOptions.canAddRepo` in render methods

**User Roles**:
- `admin` → Full access, `is_staff: true`
- `user` → Can create libraries, share, upload
- `readonly` → View only, no write operations
- `guest` → Most restricted, view only

**Test Users** (password: `password` for all):
- `admin@sesamefs.local` (token: `dev-token-admin`)
- `user@sesamefs.local` (token: `dev-token-user`)
- `readonly@sesamefs.local` (token: `dev-token-readonly`)
- `guest@sesamefs.local` (token: `dev-token-guest`)

---

## Documentation Map 📚

### Session Continuity (Read First Every Session)
- **[CURRENT_WORK.md](CURRENT_WORK.md)** - This file - Session state, priorities
- **[docs/KNOWN_ISSUES.md](docs/KNOWN_ISSUES.md)** - Detailed bug tracking
- **[docs/CHANGELOG.md](docs/CHANGELOG.md)** - Session history
- **[docs/IMPLEMENTATION_STATUS.md](docs/IMPLEMENTATION_STATUS.md)** - Component stability matrix

### Protocol & Sync (🔒 Reference Implementation)
- **[docs/SEAFILE-SYNC-PROTOCOL-RFC.md](docs/SEAFILE-SYNC-PROTOCOL-RFC.md)** - Formal RFC with test vectors 🔒
- **[docs/ENCRYPTION.md](docs/ENCRYPTION.md)** - Encrypted libraries, PBKDF2, Argon2id

### Implementation Guides
- **[docs/API-REFERENCE.md](docs/API-REFERENCE.md)** - API endpoints, implementation status
- **[docs/ENDPOINT-REGISTRY.md](docs/ENDPOINT-REGISTRY.md)** - ⚠️ CHECK BEFORE ADDING ENDPOINTS
- **[docs/FRONTEND.md](docs/FRONTEND.md)** - React frontend patterns, modal fixes
- **[CLAUDE.md](CLAUDE.md)** - Complete project context for AI assistant

---

## Quick Commands

```bash
# Run server
docker compose up -d sesamefs frontend

# Rebuild after changes
docker compose build --no-cache sesamefs frontend && docker compose up -d

# Test API with different users
curl -H "Authorization: Token dev-token-admin" http://localhost:8080/api2/account/info/
curl -H "Authorization: Token dev-token-readonly" http://localhost:8080/api2/account/info/

# Run tests
go test ./...
```

---

## End of Session Checklist

**📋 See [docs/SESSION_CHECKLIST.md](docs/SESSION_CHECKLIST.md) for complete checklist**

Quick reminders:
- [x] Update `CURRENT_WORK.md` (what was done, next priorities)
- [x] Update `docs/KNOWN_ISSUES.md` (bugs fixed/discovered)
- [x] Update `docs/CHANGELOG.md` (add session entry)
- [x] Keep `CURRENT_WORK.md` under 500 lines
