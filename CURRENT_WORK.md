# Current Work - SesameFS

**Last Updated**: 2026-01-29
**Session**: Fix Nested File/Directory Creation Bug + Comprehensive Tests

**📏 File Size Rule**: Keep this file under **500 lines** unless unavoidable. Move detailed content to:
- `docs/KNOWN_ISSUES.md` - Detailed bug tracking
- `docs/CHANGELOG.md` - Session history
- `docs/IMPLEMENTATION_STATUS.md` - Component status
- Other appropriate documentation files

---

## 🚀 NEW SESSION? START HERE

**PROJECT STATUS**: ~55% production ready (see `docs/IMPLEMENTATION_STATUS.md`)

**🔴 PRODUCTION BLOCKERS** (Must complete before deploy):
1. ~~**OIDC Authentication**~~ - ✅ **COMPLETE** (Phase 1 - Basic Login)
2. **Garbage Collection** - Architecture in `docs/ARCHITECTURE.md:381-417`, not started
3. **Monitoring/Health Checks** - Not started

**Then review**:
1. **"What's Next"** → Top priorities (work on #1 unless user specifies)
2. **"Frozen Components"** → What NOT to touch (breaks desktop clients)
3. **"Critical Context"** → Essential facts to remember

### Quick Context
1. **Sync Protocol**: 100% complete, 🔒 FROZEN
2. **Backend API**: ~92% complete (missing: GC) - OIDC ✅ DONE, Library Settings ✅ DONE
3. **Frontend UI**: ~65% complete (~90 modal dialogs need fixing, permission UI ~60%)
4. **All tests passing**: 94 integration + 138 frontend tests

### Step 2: Before Making ANY Code Changes
- ✅ Check `docs/IMPLEMENTATION_STATUS.md` - Is component 🔒 FROZEN?
- ✅ If FROZEN → DO NOT MODIFY without explicit user approval
- ✅ If ✅ COMPLETE → Modify with caution, verify tests pass
- ✅ If 🟡 PARTIAL / ❌ TODO → Safe to actively develop

### Step 3: At End of Session - Update Documentation
**📋 MANDATORY: Run [docs/SESSION_CHECKLIST.md](docs/SESSION_CHECKLIST.md)**

---

## Last Session Summary ✅

**Date**: 2026-01-29
**Focus**: Fix "Folder does not exist" bug + Expanded nested folder tests

### Completed This Session (Sessions 7-8)

- ✅ **Fixed "Folder does not exist" bug in CreateDirectory** - depth 3+ corrupted root_fs_id
  - Root cause: Manual grandparent rebuild logic used stale HEAD traversal
  - Fix: Single `RebuildPathToRoot(result, newGrandparentFSID)` call using original traversal
  - Fixed in both `files.go` and `batch_operations.go` (source + destination)
- ✅ **Fixed "Folder does not exist" bug in CreateFile** - creating a file (e.g. Word doc) in any subfolder corrupted the tree
  - Root cause: `CreateFile` called `RebuildPathToRoot(result, newParentFSID)` directly without grandparent handling
  - When parentPath != "/", the function returned the modified subfolder as root instead of updating root to point to the new subfolder
  - Fix: Added same if/else grandparent rebuild pattern used by `CreateDirectory`
  - File: `internal/api/v2/files.go` (CreateFile function)
- ✅ **Comprehensive integration test suite** - 94 integration tests across 4 suites
  - `test-nested-folders.sh`: 15→30 tests (added CreateFile v2.1 tests 16-20)
  - `test-frontend-nested-folders.sh`: NEW, 25 tests (v2.1 API endpoints)
  - `test-batch-operations.sh`: 19 tests
  - `test-file-operations.sh`: all pass
  - Go unit tests: 7 algorithm tests for RebuildPathToRoot
- ✅ **Added both test scripts to `test-all.sh`** master runner

### Completed Session 6

- ✅ **Library Settings Backend** - History limits, API tokens, auto-delete, library transfer
- ✅ **Frontend Permission UI** - Owner/admin checks on menu operations
- ✅ **Frontend .env.example** - Documented all build-time variables
- ✅ **Fixed logout** - Removed REACT_APP_BYPASS_LOGIN from Docker build

### Completed Session 5

- ✅ **OIDC Authentication - Phase 1 Complete**
  - Supports multiple redirect URIs for different environments
  - PKCE enabled by default for security

- ✅ **Files Created/Modified**
  - `internal/auth/oidc.go` - OIDC client, discovery, code exchange
  - `internal/auth/session.go` - Session management
  - `internal/api/v2/auth.go` - OIDC API endpoints
  - `internal/config/config.go` - Expanded OIDCConfig
  - `internal/api/server.go` - Auth routes, middleware update
  - `internal/db/db.go` - Sessions table migration
  - `frontend/src/pages/sso/index.js` - SSO callback page
  - `frontend/src/pages/login/index.js` - Added SSO button
  - `frontend/src/utils/seafile-api.js` - OIDC API methods
  - `frontend/public/favicon.png` - Added favicon

### Previous Session (Session 4 - 2026-01-28)

- ✅ **Comprehensive Project Review**
  - Evaluated all documentation and codebase
  - Identified production blockers (OIDC, GC, monitoring)
  - Assessed completeness: ~55% production ready

### Previous Session (Session 3 - 2026-01-28)

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

### 🔴 PRIORITY 1: Garbage Collection (Production Blocker)

**Status**: NOT STARTED - Architecture documented
**Documentation**: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) lines 381-417

**Components Missing**:
- Block GC Worker (delete ref_count=0 blocks older than 24h)
- Commit Cleanup (delete versions beyond TTL)
- FS Object Cleanup
- Expired Share Link Cleanup

**Files to Create**:
- `internal/gc/worker.go` - Main GC worker
- `internal/gc/blocks.go` - Block cleanup logic

---

### 🔴 PRIORITY 2: Monitoring/Health Checks (Production Blocker)

**Status**: NOT STARTED

**Missing**:
- `GET /health` - Load balancer health check
- `GET /metrics` - Prometheus metrics
- Structured JSON logging
- Error alerting hooks

---

### 🟡 PRIORITY 3: Frontend Modal Migration

**Status**: 🟡 15 fixed, ~90 remaining
**Documentation**: [docs/FRONTEND.md](docs/FRONTEND.md) → "Modal Pattern"

Many dialogs use reactstrap Modal inside ModalPortal which doesn't render.
Fix: Convert to plain Bootstrap modal classes.

---

### ✅ COMPLETED: Batch Operations Backend

All batch operations implemented and working:
- `POST /api/v2.1/repos/sync-batch-move-item/` ✅
- `POST /api/v2.1/repos/sync-batch-copy-item/` ✅
- `POST /api/v2.1/repos/async-batch-move-item/` ✅
- `POST /api/v2.1/repos/async-batch-copy-item/` ✅
- `GET /api/v2.1/copy-move-task/?task_id=xxx` ✅

---

### 📋 Full Roadmap

See **Strategic Roadmap** section below for complete feature list.

---

## Strategic Roadmap

### Phase 1: Production Blockers 🔴 (Must Complete First)

| Item | Status | Notes |
|------|--------|-------|
| **OIDC Authentication** | ✅ DONE | Phase 1 complete |
| **Garbage Collection** | ❌ TODO | Architecture in `docs/ARCHITECTURE.md` |
| **Health Checks/Monitoring** | ❌ TODO | `/health`, `/metrics`, logging |

### Phase 2: Core Feature Completion

| Item | Status | Notes |
|------|--------|-------|
| **Frontend Modal Migration** | 🟡 15/~100 | Bulk migration possible |
| **Library Settings Backend** | ✅ DONE | History, API tokens, auto-delete, transfer |
| **Frontend Permission UI** | 🟡 ~60% | Hide/disable based on role |

### Phase 3: Already Complete ✅

| Item | Status | Completed |
|------|--------|-----------|
| Sync Protocol | ✅ 🔒 FROZEN | 2026-01-16 |
| File Operations Backend | ✅ COMPLETE | 2026-01-27 |
| Batch Move/Copy | ✅ COMPLETE | 2026-01-27 |
| Sharing System | ✅ COMPLETE | 2026-01-22 |
| Groups Management | ✅ COMPLETE | 2026-01-22 |
| File Tags | ✅ COMPLETE | 2026-01-22 |
| Permission Middleware | ✅ COMPLETE | 2026-01-27 |
| OnlyOffice Integration | ✅ 🔒 FROZEN | 2026-01-22 |
| Search | ✅ COMPLETE | 2026-01-22 |

### Phase 4: Future Features (Lower Priority)

| Item | Priority | Notes |
|------|----------|-------|
| Version History UI | MEDIUM | Backend commits exist |
| Thumbnails | LOW | Visual polish |
| File Comments | LOW | Collaboration feature |
| Activity Logs | LOW | Audit trail |
| Watch/Unwatch | LOW | Needs notification system |
| Multi-region Replication | LOW | Future scaling |

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

### 📊 Current State (Updated 2026-01-28)
- **Sync Protocol**: 100% working, desktop clients fully compatible 🔒 FROZEN
- **Backend API**: ~85% implemented (missing: OIDC, GC, library settings)
- **Frontend UI**: ~65% functional (~90 modal dialogs need fixing)
- **Production Ready**: NO - missing OIDC, GC, monitoring (see "Production Blockers" in `docs/IMPLEMENTATION_STATUS.md`)

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
