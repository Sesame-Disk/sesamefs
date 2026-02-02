# Current Work - SesameFS

**Last Updated**: 2026-02-02
**Session**: Session 23 — File History UI (Detail Sidebar History Tab)

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
3. **Frontend UI**: ~82% complete (all modals migrated, About modal rebranded, File History UI ✅, permission UI ~60%, ~51 ModalPortal wrappers to clean up)
4. **All tests passing**: 307+ integration + 138 frontend + 55 GC unit + 261 api/v2+middleware tests + 29 admin panel + 17 file history tests

### Step 2: Before Making ANY Code Changes
- ✅ Check `docs/IMPLEMENTATION_STATUS.md` - Is component 🔒 FROZEN?
- ✅ If FROZEN → DO NOT MODIFY without explicit user approval
- ✅ If ✅ COMPLETE → Modify with caution, verify tests pass
- ✅ If 🟡 PARTIAL / ❌ TODO → Safe to actively develop

### Step 3: At End of Session - Update Documentation
**📋 MANDATORY: Run [docs/SESSION_CHECKLIST.md](docs/SESSION_CHECKLIST.md)**

---

## Last Session Summary ✅

**Date**: 2026-02-02
**Focus**: File History UI — Detail Sidebar History Tab

### Completed This Session (Session 23)

#### File History UI — Detail Sidebar History Tab ✅
- Added **Info | History** tab bar to file detail sidebar (`DirentDetail` component)
- Created `FileHistoryPanel` component — compact revision list with relative time, modifier, size
- Each revision has dropdown menu: **Restore** (skipped for current version) + **Download**
- Scroll-based pagination (loads more on scroll)
- Restore calls `seafileAPI.revertFile()`, shows toast, reloads list
- "View all history" link navigates to full-page history view
- Tabs only shown for files (directories keep current layout)
- Tab state responds to `direntDetailPanelTab` prop changes
- **Files**: `frontend/src/components/dirent-detail/dirent-details.js`, `frontend/src/components/dirent-detail/file-history-panel.js`, `frontend/src/css/dirent-detail.css`

#### Integration Tests ✅
- Created `scripts/test-file-history.sh` — 17 assertions, all passing
- Tests: both API endpoints (api2 + v2.1), pagination, non-existent file, directory history, revert, readonly permission enforcement
- Registered in `scripts/test.sh` test runner

### Previous Session (Session 22)

- **Admin Panel**: 16 admin endpoints (groups/users) + OIDC group/dept sync
- **OIDC Group/Dept Sync**: Claims extraction, sync on login, full sync mode

### Previous Sessions (18-21)

- **Session 21**: GC TTL enforcement, groups 500 fix, nav cleanup, admin panel research
- **Session 20**: Cross-repo conflict fix, move+autorename source removal fix, 7 new integration tests
- **Session 19**: Copy/Move conflict resolution (`conflict_policy` field, 409 pre-flight, `CopyMoveConflictDialog`)
- **Session 18**: Repo API token write permission fix, move/copy dialog tree fix, department test fix

### Earlier Sessions (See docs/CHANGELOG.md for details)

- **Sessions 15-17**: Departments, About modal, route fixes, nested move/copy tests
- **Session 14**: Monitoring, Health Checks, Structured Logging
- **Sessions 12-13**: Garbage Collection System
- **Sessions 7-11**: OnlyOffice, nested folder fixes, test coverage
- **Sessions 1-6**: Modals, tags, permissions, OIDC, library settings

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

### 📋 PRIORITY 4: Frontend Cleanup (Lower)

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
