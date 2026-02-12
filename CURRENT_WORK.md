# Current Work - SesameFS

**Last Updated**: 2026-02-06
**Session**: Session 31 — Search Bug Fix

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
2. **Backend API**: ~98% complete - OIDC ✅, GC ✅, Library Settings ✅, Monitoring ✅, Departments ✅, Admin Panel (groups/users) ✅, OIDC Group/Dept Sync ✅
3. **Frontend UI**: ~85% complete (all modals migrated, About modal rebranded, File History UI ✅, History Download ✅, Snapshot View ✅, Restore from History ✅, permission UI ~60%, ~51 ModalPortal wrappers to clean up)
4. **All tests passing**: 18 test suites (all green), 345+ bash integration + 26 Go integration + 138 frontend + 55 GC unit + 267 api/v2+middleware tests + 29 admin panel + 17 file history + 28 file preview + 10 search tests

### Step 2: Before Making ANY Code Changes
- ✅ Check `docs/IMPLEMENTATION_STATUS.md` - Is component 🔒 FROZEN?
- ✅ If FROZEN → DO NOT MODIFY without explicit user approval
- ✅ If ✅ COMPLETE → Modify with caution, verify tests pass
- ✅ If 🟡 PARTIAL / ❌ TODO → Safe to actively develop

### Step 3: At End of Session - Update Documentation
**📋 MANDATORY: Run [docs/SESSION_CHECKLIST.md](docs/SESSION_CHECKLIST.md)**

---

## Last Session Summary ✅

**Date**: 2026-02-06
**Focus**: Search Bug Fix — "No Results Matching" even with existing files

### Completed This Session (Session 31)

#### Search Bug Fix ✅
**Problem**: Search for "test" returned empty results despite files named "test.docx" existing.
**Root cause**: Two issues:
1. `obj_name` field in `fs_objects` table was never populated during Seafile sync (stored as "")
2. SASI indexes disabled in Cassandra 5.x — queries failed silently

**Fixes implemented**:
- `internal/api/sync.go` — After storing a directory, parse `dir_entries` and update child `obj_name` fields
- `internal/api/sync.go` — Added `updateFullPaths()` helper that runs async after commit to populate `full_path` for all entries
- `internal/api/sync.go` — Called from `PostCommit`, `PutCommit HEAD`, and `UpdateBranch` handlers
- `internal/api/seafhttp.go` — REST API uploads now set `full_path` directly when storing file fs_objects
- `internal/api/v2/search.go` — Changed to in-memory filtering (Cassandra 5 SAI doesn't support wildcard LIKE)
- `cmd/sesamefs/main.go` — Added `backfill-search-index` CLI command for existing data
- `internal/db/db.go` — Added migration for `full_path` column; documented SASI deprecation in Cassandra 5.x
- Fixed UUID type marshaling errors (use strings instead of google/uuid.UUID with gocql)
- `scripts/test-search.sh` — New integration test suite (10 tests) for search path verification

**Result**: Search now returns libraries and files with correct paths. Both backfill (for existing data), live sync (Seafile protocol), and REST API uploads populate `full_path`. Test suite passes 20/20 API suites.

### Previous Session (Session 30)

#### Snapshot View Page — ✅
- Created `frontend/src/pages/repo-snapshot/index.js` — SPA-compatible snapshot view
- Added route `/repo/:repoID/snapshot/?commit_id=...` to `app.js`
- Displays commit description, time, author at top
- Navigate through folders within the snapshot
- Breadcrumb path navigation
- "Restore Library" button (reverts entire library to snapshot)

#### RevertFile with Conflict Handling ✅
- Updated `internal/api/v2/files.go` — `RevertFile` function
- Added `conflict_policy` parameter: `replace`, `skip`, `keep_both`/`autorename`
- Same content → returns "file already has the same content"
- Different content + no policy → returns HTTP 409 with `conflicting_items`
- Uses `GenerateUniqueName()` for "keep_both" (e.g., "file (1).pdf")

#### RevertDirectory — NEW ✅
- Added `RevertDirectory` function to `internal/api/v2/files.go`
- Added "revert" case to `DirectoryOperation` switch
- Same conflict handling as RevertFile

#### Frontend Conflict Dialog ✅
- 3 options: Skip, Keep Both, Replace
- Shows filename and explains the conflict
- Visual feedback: restored items show green ✓ badge and "Restored" text
- "File is already up to date" message when content matches

#### API Methods ✅
- Added `revertFile(repoID, path, commitID, conflictPolicy)` to seafile-api.js
- Added `revertFolder(repoID, path, commitID, conflictPolicy)` to seafile-api.js
- Added `revertRepo(repoID, commitID)` to seafile-api.js

#### Backend Unit Tests ✅
- Created `internal/api/v2/revert_test.go`
- Tests: RevertFile/RevertDirectory missing params (path, commit_id)
- Tests: operation=revert is valid for file and directory operations
- Tests: GenerateUniqueName basic, multiple conflicts, no extension, directories

#### Other Fixes (from conversation start) ✅
- Trash page recursive scanning for subdirectory deletions
- History page layout matching standard library view
- About dialog branding: "SesameFS by Sesame Disk LLC"
- Share link repo-tags 404 fix

### Previous Session (Session 29)

- **Search 404 Fix**: Route registered under `/api2/`
- **Tag Deletion 500 Fix**: Counter table DELETE separated
- **Tags # URL Fix**: preventDefault + hash fragment stripping
- **File/Folder Trash**: 5 new endpoints + frontend API
- **Library Recycle Bin**: Soft-delete + 3 endpoints + 7 frontend API methods
- **File Expiry Countdown**: `expires_at` field in directory listing

### Previous Session (Session 28)

- **GC Prometheus Metrics**: Removed unused metric, wired gc_queue_size, added 10 new metrics
- **Raw File Preview 500 Fix**: Column name `size` → `size_bytes` in fileview.go
- **Image Lightbox aria-hidden Fix**: Disabled react-modal body aria-hidden
- **File History Deduplication Fix**: Deduplicate by RevFileID

### Previous Sessions (27 and earlier)

- **File Preview Tests**: 28 integration tests, Go unit test fixes
- **Freeze Candidate Analysis**: `internal/crypto` identified as strongest candidate

### Previous Sessions (18-26)

- **Session 26**: Share links fix, crypto coverage 90.8%, download URL fix
- **Session 25**: History download fix, crypto test coverage
- **Session 24**: Go Integration Test Framework, chunker fix
- **Session 23**: File History UI, Release Criteria doc
- **Session 22**: Admin Panel (16 endpoints) + OIDC Group/Dept Sync
- **Sessions 18-21**: GC TTL, groups fix, conflict resolution, move/copy fixes

### Earlier Sessions (See docs/CHANGELOG.md for details)

- **Sessions 15-17**: Departments, About modal, route fixes, nested move/copy tests
- **Sessions 12-14**: GC system, Monitoring, Health Checks
- **Sessions 1-11**: Modals, tags, permissions, OIDC, library settings, OnlyOffice, test coverage

---

## What's Next (Priority Order) 🎯

### ✅ ~~PRIORITY 1: Admin Library Management~~ — DONE (2026-02-12)

**Status**: ✅ Complete — 12 endpoints implemented in `internal/api/v2/admin.go`
**Details**: [docs/ADMIN-FEATURES.md](docs/ADMIN-FEATURES.md) § 1

All admin library endpoints implemented: list, search, get, create, delete, transfer, browse dirents, history settings, shared items, trash libraries. Frontend `seafile-api.js` methods already wired.

### 🔴 PRIORITY 2: Admin Share Link & Upload Link Management

**Status**: Share links ❌ admin endpoints missing; Upload links ❌ entire feature missing
**Details**: [docs/ADMIN-FEATURES.md](docs/ADMIN-FEATURES.md) § 2

- **Admin share links**: 2 endpoints (list all, delete any) — `share_links` table exists
- **Upload links**: Entirely new feature — needs DB tables (`upload_links`, `upload_links_by_creator`), user endpoints (CRUD), and admin endpoints (list, delete). Frontend pages exist.

### 🔴 PRIORITY 3: Audit Logs & Activity Logs — PRIORITIZE SOON

**Status**: 🟡 Console-only stub exists, no persistence or API
**Details**: [docs/ADMIN-FEATURES.md](docs/ADMIN-FEATURES.md) § 3

**Two related systems need implementation:**

1. **Audit Logs** (admin-facing): Login logs, file access logs, file update logs, permission audit logs. Needed for compliance and admin visibility. Frontend pages exist at `frontend/src/pages/sys-admin/logs-page/` and `frontend/src/pages/org-admin/org-logs-*.js`.

2. **Activity Feed** (user-facing): The `/api/v2.1/activities/` endpoint currently returns stub `{"events": []}`. The dashboard activities feed and file activity panels depend on this. Frontend components exist (`frontend/src/pages/dashboard/activity-item.js`, `frontend/src/models/activity.js`).

**What exists today:**
- `internal/middleware/audit.go` — 13 action types defined, `AuditEvent` struct, console-only logging, 8 unit tests
- Frontend UI components for both admin logs and user activity feed

**What's needed:**
- 5 new Cassandra tables (login_logs, file_access_logs, file_update_logs, permission_audit_logs, activities) with 90-day TTL
- New `internal/api/v2/audit.go` handler file (~5 endpoints)
- Async DB write integration (buffered channel pattern) across ~15 existing handlers
- Wire up frontend pages to real API endpoints

### ~~🟡 PRIORITY 4: File History UI Wiring~~ — ✅ COMPLETE (Session 23)

Detail sidebar now has Info | History tabs for files. Full-page history also works. Integration tests: 17 assertions passing.

### 📋 PRIORITY 4: Test Coverage Improvement

**Status**: Go integration test framework built (Session 24), coverage gaps identified

**Current unit test coverage** (from `go test -cover`):
| Package | Coverage | Lines | Priority |
|---------|----------|-------|----------|
| `internal/crypto` | 90.8% | ~600 | ✅ ABOVE THRESHOLD (was 69.6%) |
| `internal/api/v2` | 20.5% | 14,136 | HIGH — biggest codebase, most untested |
| `internal/api` | 19.1% | 4,769 | HIGH — sync protocol edge cases |
| `internal/db` | 0% | 1,139 | MEDIUM — all DB access only via integration |
| `internal/middleware` | 42.1% | 752 | MEDIUM — permission logic |
| `internal/storage` | 46.4% | 1,561 | MEDIUM — S3/block edge cases |
| `internal/templates` | 0% | 327 | LOW — email rendering |
| `internal/logging` | 0% | 66 | LOW — instrumentation |
| `internal/metrics` | 0% | 111 | LOW — instrumentation |

**Next steps** (in priority order):
1. **Add more Go integration tests** — share links, admin endpoints, groups, batch ops (parallels existing bash tests)
2. **DB interface mock** — define `Store` interface for `internal/db`, implement mock, unlock unit tests for all handlers
3. **API v2 handler unit tests** — error paths, validation edge cases in `files.go` (3,564 lines), `admin.go` (1,462 lines)
4. **Concurrent access tests** — race detector integration tests for simultaneous uploads/downloads
5. **testcontainers-go** — real Cassandra in CI for `internal/db` unit tests

**Frontend Testing Strategy** (7 test files currently, need expansion):
- Current: `utils.test.js`, `dirent.test.js`, `modal-pattern.test.js`, `seafile-api-tags.test.js`, `seafile-api-oidc.test.js`, `permission-checks.test.js`, `dirent-list-item.test.js`
- **Metrics to track**: Component coverage (% of components with tests), critical path coverage (login→upload→share flow), API mock coverage
- **Priority areas**: Dialog components (conflict dialogs, restore dialogs), API integration layer, permission-based UI visibility
- **Tools**: Jest + React Testing Library (already configured), consider adding Cypress for E2E

### 📋 PRIORITY 5: Frontend Cleanup (Lower)

- **ModalPortal Wrapper Cleanup** — ~51 parent components have unnecessary `<ModalPortal>` wrappers (harmless, cosmetic)
- **Frontend Permission UI** — ~60% complete, readonly/guest users still see some buttons they can't use

---

## Strategic Roadmap

### Phase 1: Production Blockers 🔴 — ALL COMPLETE ✅

| Item | Status | Notes |
|------|--------|-------|
| **OIDC Authentication** | ✅ DONE | Phase 1 complete |
| **Garbage Collection** | ✅ DONE | Queue worker + scanner + admin API |
| **Health Checks/Monitoring** | ✅ DONE | `/health`, `/ready`, `/metrics`, slog logging |

### Phase 2: Core Feature Completion

| Item | Status | Notes |
|------|--------|-------|
| **Admin Panel (Groups/Users)** | ✅ DONE | Option A (OIDC-managed). 16 endpoints + OIDC sync. 29 tests. |
| **Admin Library Management** | ✅ DONE | 12 endpoints in admin.go. See [ADMIN-FEATURES.md](docs/ADMIN-FEATURES.md) § 1 |
| **Admin Link Management** | ❌ TODO | Share + upload links. See [ADMIN-FEATURES.md](docs/ADMIN-FEATURES.md) § 2 |
| **Audit Logs** | ❌ TODO | 5 tables, ~5 endpoints, ~15 handler integrations. See [ADMIN-FEATURES.md](docs/ADMIN-FEATURES.md) § 3 |
| **File History UI** | ✅ DONE | Detail sidebar History tab + full-page view. 17 integration tests. |
| **GC TTL Enforcement** | ✅ DONE | Scanner Phase 5 (version_ttl_days) + Phase 6 (auto_delete_days) + share link deletion |
| **Frontend Modal Migration** | ✅ 122/122 | All done; ~51 ModalPortal wrappers to clean up |
| **Library Settings Backend** | ✅ DONE | History, API tokens, auto-delete, transfer |
| **Department Management** | ✅ DONE | Admin CRUD + hierarchy, 29 integration tests |
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
| Admin Panel (Groups/Users) | ✅ COMPLETE | 2026-02-02 |
| OIDC Group/Dept Sync | ✅ COMPLETE | 2026-02-02 |
| File Tags | ✅ COMPLETE | 2026-01-22 |
| Permission Middleware | ✅ COMPLETE | 2026-01-27 |
| OnlyOffice Integration | ✅ 🔒 FROZEN | 2026-01-29 |
| Search | ✅ COMPLETE | 2026-01-22 |

### Phase 4: Future Features (Lower Priority)

| Item | Priority | Notes |
|------|----------|-------|
| Thumbnails | LOW | Visual polish |
| File Comments | LOW | Collaboration feature |
| Watch/Unwatch | LOW | Needs notification system |
| Multi-region Replication | LOW | Future scaling |

---

## Frozen/Stable Components 🔒

**Freeze procedure**: See [docs/RELEASE-CRITERIA.md](docs/RELEASE-CRITERIA.md) for the formal stability rules and Component Test Map. Components need ≥ 80% Go coverage, ≥ 90% integration endpoint coverage, zero open bugs, and 3 clean sessions in 🟢 RELEASE-CANDIDATE before reaching 🔒 FROZEN.

### ⚠️ CRITICAL: Sync Code FROZEN (2026-01-19)
**User directive**: DO NOT MODIFY sync code without explicit approval

### Code Files - Sync Protocol 🔒
- `internal/api/sync.go` (lines 949-952, 125-130, 1405-1492) - Protocol formats
- `internal/api/v2/encryption.go` - Password endpoints

### Code Files - Crypto 🔒 (Frozen 2026-02-04)
- `internal/crypto/crypto.go` - PBKDF2, Argon2id, AES-256-CBC (90.8% unit test coverage, 39 tests)

### Code Files - Monitoring/Health 🔒 (Updated 2026-02-04)
- `internal/health/health.go` - Liveness and readiness probes 🔒
- `internal/metrics/metrics.go` - Prometheus metric definitions (GC metrics expanded Session 28)
- `internal/metrics/middleware.go` - Request metrics middleware 🔒
- `internal/logging/logging.go` - Structured logging setup 🔒

### Code Files - OnlyOffice 🔒 (Frozen 2026-01-29)
- `internal/api/v2/fileview.go` - File view auth wrapper + OnlyOffice editor HTML (json.Marshal config). Note: History download handler added (Session 25) — OnlyOffice code paths unchanged.
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

### 📊 Current State (Updated 2026-02-04)
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
./scripts/test.sh api              # Bash integration tests (335+ assertions)
./scripts/test.sh go               # Go unit tests
./scripts/test.sh go-integration   # Go integration tests (requires backend)
./scripts/test.sh all              # Everything
./scripts/test.sh api --quick      # Skip slow tests
```

---

## End of Session Checklist

**📋 See [docs/SESSION_CHECKLIST.md](docs/SESSION_CHECKLIST.md) for complete checklist**

Quick reminders:
- [x] Update `CURRENT_WORK.md` (what was done, next priorities)
- [x] Update `docs/KNOWN_ISSUES.md` (bugs fixed/discovered)
- [x] Update `docs/CHANGELOG.md` (add session entry)
- [x] Keep `CURRENT_WORK.md` under 500 lines
