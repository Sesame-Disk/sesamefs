# Current Work - SesameFS

**Last Updated**: 2026-01-27
**Session**: Batch Move/Copy Operations Backend

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
1. **Batch move/copy**: Backend 100% complete, ready for frontend integration
2. **Permission system**: Backend 100% complete, Frontend ~30% complete
3. **Encrypted libraries**: Now properly blocked until password entered

### Step 2: Before Making ANY Code Changes
- ✅ Check `docs/IMPLEMENTATION_STATUS.md` - Is component 🔒 FROZEN?
- ✅ If FROZEN → DO NOT MODIFY without explicit user approval
- ✅ If ✅ COMPLETE → Modify with caution, verify tests pass
- ✅ If 🟡 PARTIAL / ❌ TODO → Safe to actively develop

### Step 3: At End of Session - Update Documentation
**📋 MANDATORY: Run [docs/SESSION_CHECKLIST.md](docs/SESSION_CHECKLIST.md)**

---

## Last Session Summary ✅

**Date**: 2026-01-27 (Session 2)
**Focus**: Testing & Bug Fixes

### Completed This Session

- ✅ **Fixed Batch Move/Copy Bug** - Items now properly move/copy
  - Fixed bug where destination directory check used parent's entries instead of target's
  - Fixed nested path handling when removing items from source directory during move
  - **Files**: `internal/api/v2/batch_operations.go:126-139, 187-209`

- ✅ **Fixed Nested Directory Creation Bug**
  - CreateDirectory now correctly places items inside parent directories (not at root)
  - Same TraverseToPath issue - was using parent's entries instead of target directory's
  - **Files**: `internal/api/v2/files.go` CreateDirectory function

- ✅ **Test Scripts Improved** - All 5 test suites now pass
  - Fixed test-permissions.sh: Use timestamps for unique library names
  - Fixed test-file-operations.sh: Parse `repo_id` correctly, create fresh library each run
  - Fixed test-library-settings.sh: Same repo_id parsing fix
  - Fixed test-encrypted-library-security.sh: Auto-create encrypted library for testing
  - Created new **test-batch-operations.sh**: 19 tests for batch move/copy
  - Updated test-all.sh to include batch operations tests
  - **Files**: All scripts in `/scripts/` directory

- ✅ **Integration Test Results**
  | Test Suite | Tests | Result |
  |------------|-------|--------|
  | Permission System | 24 | ✅ PASS |
  | File Operations | 16 | ✅ PASS |
  | Batch Operations | 19 | ✅ PASS |
  | Library Settings | 5 | ✅ PASS |
  | Encrypted Library Security | 14 | ✅ PASS |
  | **Total** | **78** | **✅ ALL PASS** |

### Previous Session (2026-01-27 Session 1)

- ✅ Batch Move/Copy Operations Backend - all 4 endpoints implemented
- ✅ Library Creation v2.1 API - POST /api/v2.1/repos/
- ✅ Backend Permission Checks - All write operations protected
- ✅ Permission Tests - 24 scenarios verified

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

**Status**: ~30% complete - Core done, many features still need permission checks

**Remaining UI Elements to Hide/Disable for Readonly/Guest**:

| Feature | Location | Status |
|---------|----------|--------|
| Upload button | `lib-content-toolbar.js` | ❌ TODO |
| New folder button | `lib-content-toolbar.js` | ❌ TODO |
| Delete file/folder | `dirent-menu.js`, context menus | ❌ TODO |
| Rename file/folder | Context menus | ❌ TODO |
| Move button | Toolbar, context menus | ❌ TODO |
| Share library button | `repo-info.js`, menus | ❌ TODO |
| New Group button | `groups-view.js` | ❌ TODO |
| Drag & drop upload | `lib-content-view.js` | ❌ TODO |

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

### 🟢 PRIORITY 2: Library Advanced Settings

**Status**: Backend stubs, frontend dialogs exist
**Effort**: 1-2 days

**Missing**:
- History Setting endpoint
- API Token generation
- Auto Deletion Setting

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

### Phase 2: Production Readiness (Week 5+)

**2.1 Garbage Collection** - 🔥 CRITICAL for production
- Block GC worker (delete ref_count=0 blocks)
- Commit cleanup (version_ttl_days)
- Expired share link cleanup
- **Priority**: Must implement before production

**2.2 Authentication & Security** - CRITICAL
- OIDC/OAuth integration
- Session management
- Password change functionality

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
