# Current Work - SesameFS

**Last Updated**: 2026-02-01
**Session**: Session 20 — Copy/Move Conflict Bug Fixes (Cross-Repo + Autorename)

**📏 File Size Rule**: Keep this file under **500 lines** unless unavoidable. Move detailed content to:
- `docs/KNOWN_ISSUES.md` - Detailed bug tracking
- `docs/CHANGELOG.md` - Session history
- `docs/IMPLEMENTATION_STATUS.md` - Component status
- Other appropriate documentation files

---

## 🚀 NEW SESSION? START HERE

**PROJECT STATUS**: ~75% production ready (see `docs/IMPLEMENTATION_STATUS.md`)

**🔴 PRODUCTION BLOCKERS** (Must complete before deploy):
1. ~~**OIDC Authentication**~~ - ✅ **COMPLETE** (Phase 1 - Basic Login)
2. ~~**Garbage Collection**~~ - ✅ **COMPLETE** (Queue worker + safety scanner + admin API)
3. ~~**Monitoring/Health Checks**~~ - ✅ **COMPLETE** (Structured logging, `/health`, `/ready`, `/metrics`)

**Then review**:
1. **"What's Next"** → Top priorities (work on #1 unless user specifies)
2. **"Frozen Components"** → What NOT to touch (breaks desktop clients)
3. **"Critical Context"** → Essential facts to remember

### Quick Context
1. **Sync Protocol**: 100% complete, 🔒 FROZEN
2. **Backend API**: ~97% complete - OIDC ✅, GC ✅, Library Settings ✅, Monitoring ✅, Departments ✅
3. **Frontend UI**: ~80% complete (all modals migrated, About modal rebranded, permission UI ~60%, ~51 ModalPortal wrappers to clean up)
4. **All tests passing**: 290+ integration + 138 frontend + 55 GC unit + 261 api/v2+middleware tests

### Step 2: Before Making ANY Code Changes
- ✅ Check `docs/IMPLEMENTATION_STATUS.md` - Is component 🔒 FROZEN?
- ✅ If FROZEN → DO NOT MODIFY without explicit user approval
- ✅ If ✅ COMPLETE → Modify with caution, verify tests pass
- ✅ If 🟡 PARTIAL / ❌ TODO → Safe to actively develop

### Step 3: At End of Session - Update Documentation
**📋 MANDATORY: Run [docs/SESSION_CHECKLIST.md](docs/SESSION_CHECKLIST.md)**

---

## Last Session Summary ✅

**Date**: 2026-02-01
**Focus**: Copy/Move Conflict Resolution Bug Fixes

### Completed This Session (Session 20)

#### Cross-Repo Conflict Resolution Bug Fix ✅
- **Bug**: Async (cross-repo) batch copy/move skipped pre-flight conflict check — returned 200 with task_id, then task silently failed. Frontend showed "interface error" for cross-library copies with same-name files.
- **Fix**: Moved pre-flight conflict check BEFORE the `if async` branch so it applies to both sync and async paths
- **File**: `internal/api/v2/batch_operations.go`

#### Move+Autorename Source Removal Bug Fix ✅
- **Bug**: When moving with `conflict_policy=autorename`, the source file was not removed because `RemoveEntryFromList` used the renamed name (e.g., `file (1).md`) instead of the original name (`file.md`)
- **Fix**: Added `originalItemName` variable to preserve name before autorename; source removal and commit description use original name
- **File**: `internal/api/v2/batch_operations.go`

#### New Integration Tests (7 tests, 29-35) ✅
- Test 29: Cross-repo copy with same-name file returns 409
- Test 30: Cross-repo conflict response includes `error`/`conflicting_items`
- Test 31: Cross-repo copy with `replace` policy works
- Test 32: Cross-repo copy with `autorename` policy works
- Test 33: Cross-repo nested path conflict returns 409
- Test 34: Move with autorename correctly removes source file
- Test 35: Copy from nested path to root — conflict + replace + autorename
- **All 137 tests pass** (scripts/test-nested-move-copy.sh)
- **File**: `scripts/test-nested-move-copy.sh` — added cross-repo helpers, second test library, 7 new test functions

### Previous Session (Session 19)

#### Copy/Move Conflict Resolution ✅
- **Backend**: Added `conflict_policy` field to `BatchRequest`, `MoveFileRequest`, `CopyFileRequest`
- **Policies**: `"replace"` (overwrite), `"autorename"` (keep both → `file (1).ext`), `"skip"` (silently skip)
- **409 Response**: No policy + conflict → HTTP 409 with `{"error":"conflict","conflicting_items":["file.txt"]}`
- **Frontend**: New `CopyMoveConflictDialog` (Replace / Keep Both / Cancel), integrated into `lib-content-view.js`

#### Groups 500 Error Fix ✅
- **Bug**: `GET /api/v2.1/groups/?with_repos=0` returned 500 due to unhandled UUID parse errors
- **Fix**: Added proper error handling for UUID parsing and inner queries with `slog.Warn` logging

#### Auto-Delete Documentation ✅
- **Updated**: `docs/KNOWN_ISSUES.md` — noted `auto_delete_days`/`version_ttl_days` stored but not enforced by GC

### Previous Session (Session 18)

#### Repo API Token Write Permission Fix ✅

- ✅ **`internal/api/v2/files.go`** — Fixed `requireWritePermission()` to check repo API token permissions before org-level role check
- **Bug**: Read-only repo API tokens could create directories (returned 201 instead of 403)
- **Fix**: Added repo API token check at top of `requireWritePermission()`
- ✅ **`scripts/test-repo-api-tokens.sh`** — Made executable, registered in `test.sh`, all 37 tests passing

#### Move/Copy Dialog Tree Fix ✅

- ✅ **`internal/api/v2/files.go`** — Added `with_parents=true` support to `ListDirectoryV21`
- **Bug**: Frontend move/copy dialog crashed with `TypeError: Cannot read properties of null (reading 'path')` because tree-builder couldn't find intermediate nodes
- **Fix**: When `with_parents=true`, traverse from root to target path collecting directory entries at each ancestor level with correct `parent_dir` format (trailing slash)
- ✅ **`scripts/test-dir-with-parents.sh`** — NEW, 52 integration tests across 10 sections, all passing

#### Department Test Double-POST Fix ✅

- ✅ **`scripts/test-departments.sh`** — Fixed ghost duplicate department bug caused by separate `api_body()`/`api_status()` calls
- **Fix**: Added `api_call()` helper for single-request body+status capture; added `cleanup_stale_departments()` at test start
- **Result**: All 29 department tests passing

#### Duplicate Name Rejection Tests ✅

- ✅ **`scripts/test-nested-move-copy.sh`** — Extended to 137 tests (35 test sections) covering nested ops, conflict detection, conflict policies, cross-repo conflicts, autorename source removal

#### All 12 API Test Suites Pass — 0 Failures

### Completed Previous Session (Session 17)

### Completed Previous Session (Sessions 15-17)

- ✅ **Department Management API** — Full CRUD with hierarchy, 29 integration tests
- ✅ **About Modal Branding** — "SesameFS by Sesame Disk LLC", v0.0.1
- ✅ **SSO/HTTPS Investigation** — Documented desktop client HTTPS requirement
- ✅ **Nested Move/Copy Tests** — 91 tests across 20 sections (now 103 with Session 18 additions)
- ✅ **Route fixes** — search-user, multi-share-links, copy-move-progress aliases
- ✅ **File download URL fix** — `getBrowserURL()` helper for browser-reachable URLs

### Earlier Sessions (See docs/CHANGELOG.md for details)

- **Session 14**: Monitoring, Health Checks, Structured Logging
- **Sessions 12-13**: Garbage Collection System (55 unit + 21 integration tests)
- **Sessions 10-11**: Test coverage improvements (60+ new unit tests, port fixes)
- **Sessions 7-9**: OnlyOffice fix 🔒, nested folder corruption fixes, 94 integration tests
- **Session 6**: Library Settings Backend, Frontend Permission UI
- **Session 5**: OIDC Authentication Phase 1
- **Sessions 1-4**: Modal fixes (15 dialogs), tag system, permission checks, project review

### Batch Operations API

**Sync Move** (same repo):
```bash
curl -X POST "http://localhost:8082/api/v2.1/repos/sync-batch-move-item/" \
  -H "Authorization: Token dev-token-admin" \
  -d '{"src_repo_id":"...", "src_parent_dir":"/", "dst_repo_id":"...", "dst_parent_dir":"/dest", "src_dirents":["folder1"]}'
# Response: {"success":true}
```

**Async Move** (cross repo, returns task_id):
```bash
curl -X POST "http://localhost:8082/api/v2.1/repos/async-batch-move-item/" \
  ...
# Response: {"task_id":"uuid-xxx"}

curl "http://localhost:8082/api/v2.1/copy-move-task/?task_id=uuid-xxx"
# Response: {"done":true,"successful":1,"failed":0,"total":1}
```

---

## What's Next (Priority Order) 🎯

### ✅ COMPLETED: Garbage Collection

**Status**: ✅ COMPLETE (2026-01-30)
**Files**: `internal/gc/` — gc.go, queue.go, worker.go, scanner.go, store.go, store_mock.go, store_cassandra.go, gc_hooks.go, gc_adapter.go
**Tests**: 55 Go unit tests + 21 bash integration tests, all passing
**Admin API**: `GET /api/v2.1/admin/gc/status`, `POST /api/v2.1/admin/gc/run`

---

### ✅ COMPLETED: Monitoring/Health Checks

**Status**: ✅ COMPLETE (2026-01-30)
**Files**: `internal/logging/`, `internal/health/`, `internal/metrics/`
**Endpoints**: `GET /health` (liveness), `GET /ready` (readiness), `GET /metrics` (Prometheus)
**Features**: Structured slog logging (JSON prod / text dev), request metrics middleware

---

### 🟡 PRIORITY 1: Frontend ModalPortal Wrapper Cleanup

**Status**: ✅ All 122 dialog components migrated. ~51 parent components still use unnecessary `<ModalPortal>` wrappers.
**Documentation**: [docs/FRONTEND.md](docs/FRONTEND.md) → "Dialogs and Modals"

All dialog components now use plain Bootstrap modal classes. The remaining work is removing
`<ModalPortal>` wrappers from parent components — this is harmless cleanup (dialogs already render correctly).

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
| **Garbage Collection** | ✅ DONE | Queue worker + scanner + admin API |
| **Health Checks/Monitoring** | ✅ DONE | `/health`, `/ready`, `/metrics`, slog logging |

### Phase 2: Core Feature Completion

| Item | Status | Notes |
|------|--------|-------|
| **Frontend Modal Migration** | ✅ 122/122 | All done; ~51 ModalPortal wrappers to clean up |
| **Library Settings Backend** | ✅ DONE | History, API tokens, auto-delete, transfer |
| **Department Management** | ✅ DONE | Admin CRUD + hierarchy, 29 integration tests |
| **About Modal Branding** | ✅ DONE | SesameFS v0.0.1 by Sesame Disk LLC |
| **Frontend Permission UI** | 🟡 ~60% | Hide/disable based on role |

### Phase 3: Already Complete ✅

| Item | Status | Completed |
|------|--------|-----------|
| Sync Protocol | ✅ 🔒 FROZEN | 2026-01-16 |
| File Operations Backend | ✅ COMPLETE | 2026-01-27 |
| Batch Move/Copy | ✅ COMPLETE | 2026-01-27 |
| Sharing System | ✅ COMPLETE | 2026-01-22 |
| Groups Management | ✅ COMPLETE | 2026-01-22 |
| Department Management | ✅ COMPLETE | 2026-01-31 |
| File Tags | ✅ COMPLETE | 2026-01-22 |
| Permission Middleware | ✅ COMPLETE | 2026-01-27 |
| OnlyOffice Integration | ✅ 🔒 FROZEN | 2026-01-29 |
| Search | ✅ COMPLETE | 2026-01-22 |

### Phase 4: Future Features (Lower Priority)

| Item | Priority | Notes |
|------|----------|-------|
| Version History — Remaining Gaps | MEDIUM | See details below |
| Thumbnails | LOW | Visual polish |
| File Comments | LOW | Collaboration feature |
| Activity Logs | LOW | Audit trail |
| Watch/Unwatch | LOW | Needs notification system |
| Multi-region Replication | LOW | Future scaling |

#### Version History — Status Update (2026-02-01)

**Core feature is ✅ COMPLETE**: File history listing, revision download, file revert, history limit settings, pagination, encryption support — all working end-to-end (backend + frontend + seafile-js).

**Remaining gaps** (enhancements, not blockers):
1. **Library-wide commit history** — No `GET /api2/repo-history/:id/` endpoint. Users can see per-file history but not "all changes to this library." Medium effort.
2. **Diff view between versions** — Frontend has infrastructure but backend has no diff endpoint (`/api2/repos/:id/file/diff/`). Medium effort (needs text diff algorithm).
3. **History TTL enforcement** — `version_ttl_days` stored but GC doesn't delete old commits. Same gap as `auto_delete_days`. Medium effort (extend GC scanner).
4. **Directory revert** — Implementation exists (`POST /dir/?operation=revert`) but untested. Low effort (just needs testing).

---

## Frozen/Stable Components 🔒

### ⚠️ CRITICAL: Sync Code FROZEN (2026-01-19)
**User directive**: DO NOT MODIFY sync code without explicit approval

### Code Files - Sync Protocol 🔒
- `internal/crypto/crypto.go` - PBKDF2 implementation
- `internal/api/sync.go` (lines 949-952, 125-130, 1405-1492) - Protocol formats
- `internal/api/v2/encryption.go` - Password endpoints

### Code Files - OnlyOffice 🔒 (Frozen 2026-01-29)
- `internal/api/v2/fileview.go` - File view auth wrapper + OnlyOffice editor HTML (json.Marshal config)
- `internal/api/v2/onlyoffice.go` - OnlyOffice API endpoint + JWT signing + editor callback

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

### 📊 Current State (Updated 2026-02-01)
- **Sync Protocol**: 100% working, desktop clients fully compatible 🔒 FROZEN
- **Backend API**: ~97% implemented — OIDC ✅, GC ✅, Library Settings ✅, OnlyOffice ✅
- **Frontend UI**: ~80% functional (all modals migrated, ~51 ModalPortal wrappers to clean up)
- **Production Ready**: All production blockers complete — OIDC ✅, GC ✅, Monitoring ✅

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
curl -H "Authorization: Token dev-token-admin" http://localhost:8082/api2/account/info/
curl -H "Authorization: Token dev-token-readonly" http://localhost:8082/api2/account/info/

# Run tests (ALWAYS use test.sh)
./scripts/test.sh api          # All integration tests (222+ assertions)
./scripts/test.sh go           # Go unit tests
./scripts/test.sh all          # Everything
./scripts/test.sh api --quick  # Skip slow tests
```

---

## End of Session Checklist

**📋 See [docs/SESSION_CHECKLIST.md](docs/SESSION_CHECKLIST.md) for complete checklist**

Quick reminders:
- [x] Update `CURRENT_WORK.md` (what was done, next priorities)
- [x] Update `docs/KNOWN_ISSUES.md` (bugs fixed/discovered)
- [x] Update `docs/CHANGELOG.md` (add session entry)
- [x] Keep `CURRENT_WORK.md` under 500 lines
