# Current Work - SesameFS

**Last Updated**: 2026-02-03
**Session**: Session 26 — Share Links Fix (Full URLs + Repo-Specific Endpoint)

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
3. **Frontend UI**: ~82% complete (all modals migrated, About modal rebranded, File History UI ✅, History Download ✅, permission UI ~60%, ~51 ModalPortal wrappers to clean up)
4. **All tests passing**: 307+ bash integration + 26 Go integration + 138 frontend + 55 GC unit + 267 api/v2+middleware tests + 29 admin panel + 17 file history tests

### Step 2: Before Making ANY Code Changes
- ✅ Check `docs/IMPLEMENTATION_STATUS.md` - Is component 🔒 FROZEN?
- ✅ If FROZEN → DO NOT MODIFY without explicit user approval
- ✅ If ✅ COMPLETE → Modify with caution, verify tests pass
- ✅ If 🟡 PARTIAL / ❌ TODO → Safe to actively develop

### Step 3: At End of Session - Update Documentation
**📋 MANDATORY: Run [docs/SESSION_CHECKLIST.md](docs/SESSION_CHECKLIST.md)**

---

## Last Session Summary ✅

**Date**: 2026-02-03
**Focus**: Share Links Fix (Full URLs + Repo-Specific Endpoint)

### Completed This Session (Session 26)

#### Share Links Bug Fix ✅
- **Problem**: Share links showed relative paths (`/d/token`) instead of full URLs; repo-specific share link list returned empty (stub); user reported "two entries" appearing in UI
- **Root cause**: Three issues — (1) `LinkURL` in `share_links.go` used `fmt.Sprintf("/d/%s", token)` without server URL prefix, (2) `/api/v2.1/repos/:repo_id/share-links/` was a stub returning `[]`, (3) frontend admin panel called `listRepoShareLinks` which hit the stub
- **Backend fixes**:
  - Added `serverURL` field to `ShareLinkHandler`, updated `RegisterShareLinkRoutes` signature to accept it
  - Changed `LinkURL` in both `ListShareLinks` and `CreateShareLink` to use `getBrowserURL(c, h.serverURL)` for full URLs
  - Implemented `ListRepoShareLinks` handler that sets `repo_id` query param from URL path and delegates to `ListShareLinks`
  - Registered repo-specific routes (`/repos/:repo_id/share-links/`) in `RegisterShareLinkRoutes`
  - Removed stub `handleEmptyRepoShareLinks` from `server.go`
- **Tests**: 2 new unit tests (`TestListRepoShareLinks_SetsRepoIDQueryParam`, `TestShareLinkURL_IncludesServerURL`)
- **Files modified**: `internal/api/v2/share_links.go`, `internal/api/v2/share_links_test.go`, `internal/api/server.go`

#### Crypto Unit Test Coverage ✅
- Pushed `internal/crypto/` from **69.6% → 90.8%** coverage
- Added 25 targeted tests in `internal/crypto/coverage_test.go` covering all uncovered functions

#### Download URL Fix ✅
- **Problem**: Frontend downloads went to `localhost:3000` (nginx) instead of backend
- **Fix**: `getBrowserURL()` now prefers configured `SERVER_URL`/`FILE_SERVER_ROOT` over request Host header
- Added `FILE_SERVER_ROOT` env var support in `server.go`
- Updated `departments_test.go` for new behavior

#### Upload/Download Integration Tests ✅
- 7 new integration tests simulating full frontend upload/download flow
- Tests: round-trip upload→download, upload link URL, download link URL, overwrite, permission enforcement

### Previous Session (Session 24)

- **Go Integration Test Framework**: 14 test functions, test.sh enhancements, chunker fix
- **File History UI** (Session 23): Detail sidebar History tab, 17 integration tests

### Previous Sessions (18-22)

- **Session 26**: Share links fix — full URLs, repo-specific endpoint, removed stub
- **Session 25**: History download fix, crypto test coverage, download URL fix
- **Session 22**: Admin Panel (16 endpoints) + OIDC Group/Dept Sync
- **Session 21**: GC TTL enforcement, groups 500 fix, nav cleanup
- **Session 20**: Cross-repo conflict fix, move+autorename fix, 7 new integration tests
- **Session 19**: Copy/Move conflict resolution
- **Session 18**: Repo API token write permission fix, move/copy dialog tree fix

### Earlier Sessions (See docs/CHANGELOG.md for details)

- **Sessions 15-17**: Departments, About modal, route fixes, nested move/copy tests
- **Sessions 12-14**: GC system, Monitoring, Health Checks
- **Sessions 1-11**: Modals, tags, permissions, OIDC, library settings, OnlyOffice, test coverage

---

## What's Next (Priority Order) 🎯

### 🔴 PRIORITY 1: Admin Library Management

**Status**: ❌ Backend endpoints missing — database and frontend exist
**Details**: [docs/ADMIN-FEATURES.md](docs/ADMIN-FEATURES.md) § 1

Need ~10 endpoints in `internal/api/v2/admin.go`: list all libraries, search, delete, transfer ownership, create, get info, browse contents, history settings, shared items. Frontend pages exist at `frontend/src/pages/sys-admin/repos/`. Database `libraries` table has SASI search index ready.

### 🔴 PRIORITY 2: Admin Share Link & Upload Link Management

**Status**: Share links ❌ admin endpoints missing; Upload links ❌ entire feature missing
**Details**: [docs/ADMIN-FEATURES.md](docs/ADMIN-FEATURES.md) § 2

- **Admin share links**: 2 endpoints (list all, delete any) — `share_links` table exists
- **Upload links**: Entirely new feature — needs DB tables (`upload_links`, `upload_links_by_creator`), user endpoints (CRUD), and admin endpoints (list, delete). Frontend pages exist.

### 🟡 PRIORITY 3: Audit Logs

**Status**: 🟡 Console-only stub exists, no persistence or API
**Details**: [docs/ADMIN-FEATURES.md](docs/ADMIN-FEATURES.md) § 3

Needs 5 new Cassandra tables (login logs, file access logs, file update logs, permission audit logs, activities feed), a new `internal/api/v2/audit.go` handler file, and logging integration across ~15 existing handlers. Existing middleware at `internal/middleware/audit.go` defines action types but only prints to console. Frontend pages exist at `frontend/src/pages/sys-admin/logs-page/`.

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
| **Admin Library Management** | ❌ TODO | ~10 endpoints. See [ADMIN-FEATURES.md](docs/ADMIN-FEATURES.md) § 1 |
| **Admin Link Management** | ❌ TODO | Share + upload links. See [ADMIN-FEATURES.md](docs/ADMIN-FEATURES.md) § 2 |
| **Audit Logs** | ❌ TODO | 5 tables, ~5 endpoints, ~15 handler integrations. See [ADMIN-FEATURES.md](docs/ADMIN-FEATURES.md) § 3 |
| **File History UI** | ✅ DONE | Detail sidebar History tab + full-page view. 17 integration tests. |
| **GC TTL Enforcement** | ✅ DONE | Scanner Phase 5 — version_ttl_days + share link deletion |
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
- `internal/crypto/crypto.go` - PBKDF2 implementation
- `internal/api/sync.go` (lines 949-952, 125-130, 1405-1492) - Protocol formats
- `internal/api/v2/encryption.go` - Password endpoints

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
./scripts/test.sh api              # Bash integration tests (307+ assertions)
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
