# Current Work - SesameFS

**Last Updated**: 2026-02-24
**Session**: Session 53 — Admin Trash Libraries: 405 Fix + Cleanup Handler + Orphan Data Docs

**📏 File Size Rule**: Keep this file under **500 lines** unless unavoidable. Move detailed content to:
- `docs/KNOWN_ISSUES.md` - Detailed bug tracking
- `docs/CHANGELOG.md` - Session history
- `docs/IMPLEMENTATION_STATUS.md` - Component status
- Other appropriate documentation files

---

## 🚀 NEW SESSION? START HERE

**PROJECT STATUS**: ~80% production ready (see `docs/IMPLEMENTATION_STATUS.md`)

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
2. **Backend API**: ~98% complete - OIDC ✅, GC ✅, Library Settings ✅, Monitoring ✅, Departments ✅, Admin Panel (groups/users) ✅, OIDC Group/Dept Sync ✅, Tag cascade ✅, Admin Link Management ✅, Upload Links ✅
3. **Frontend UI**: ~85% complete (all modals migrated, About modal rebranded, File History UI ✅, History Download ✅, Snapshot View ✅, Restore from History ✅, permission UI ~60%, ~51 ModalPortal wrappers to clean up, folder icons ✅)
4. **All tests passing**: 18 test suites (all green), 345+ bash integration + 26 Go integration + 138 frontend + 55 GC unit + 267 api/v2+middleware tests + 29 admin panel + 17 file history + 28 file preview + 10 search tests
5. **Active Bugs**: 0 open (all 5 resolved in Session 32)

### Step 2: Before Making ANY Code Changes
- ✅ Check `docs/IMPLEMENTATION_STATUS.md` - Is component 🔒 FROZEN?
- ✅ If FROZEN → DO NOT MODIFY without explicit user approval
- ✅ If ✅ COMPLETE → Modify with caution, verify tests pass
- ✅ If 🟡 PARTIAL / ❌ TODO → Safe to actively develop

### Step 3: At End of Session - Update Documentation
**📋 MANDATORY: Run [docs/SESSION_CHECKLIST.md](docs/SESSION_CHECKLIST.md)**

---

## Last Session Summary ✅

**Date**: 2026-02-24
**Focus**: Admin Trash Libraries: 405 Fix + Full Cleanup Handler + Orphan Data Documentation

### Completed This Session (Session 53)

#### Bug Fix: `DELETE /admin/trash-libraries/` → 405 ✅

**Problem**: Superadmin clicking "Clean Trash" got a 405 — `DELETE` method not registered, no handler existed.

**Fixes**:
1. Added `DELETE /admin/trash-libraries/` + `DELETE /admin/trash-libraries` to router (`admin.go:134-135`)
2. Implemented `AdminCleanTrashLibraries` handler with full cleanup chain:
   - Scans all soft-deleted libs per org in one pass
   - Calls `getLibraryEnqueuer().EnqueueLibraryDeletion(...)` async (GC hook)
   - Calls `CleanupAllLibraryTags(h.db, lib.libID)` async
   - Hard-deletes via `gocql.LoggedBatch` on `libraries` + `libraries_by_id`
   - Superadmin cleans all orgs; org admin cleans their org only
   - Returns `{"success": true, "cleaned": N}`
3. Added doc comment to `PermanentDeleteRepo` documenting what is and isn't cleaned

#### Orphaned Data Gap — Identified, Documented, Planned ⚠️

**Problem**: Both `PermanentDeleteRepo` and `AdminCleanTrashLibraries` leave orphaned rows in `shares`, `share_links`, `share_links_by_creator`, `upload_links`, `upload_links_by_creator` after permanent library deletion. No crash, but DB bloat.

**Documentation added**:
- `docs/TECHNICAL-DEBT.md` § 9 — full implementation plan
- `docs/KNOWN_ISSUES.md` `ISSUE-GC-ORPHANS-01` — issue tracking
- `docs/ADMIN-FEATURES.md` — known gap note
- `docs/ENDPOINT-REGISTRY.md` — DELETE endpoint documented
- `internal/db/db.go` — comments on affected tables
- `internal/api/v2/deleted_libraries.go` — doc comment on `PermanentDeleteRepo`

**Files changed**: `admin.go`, `deleted_libraries.go`, `db.go`, `TECHNICAL-DEBT.md`, `KNOWN_ISSUES.md`, `ADMIN-FEATURES.md`, `ENDPOINT-REGISTRY.md`, `CHANGELOG.md`

### Previous Session (Session 52) — Retrocompat Fix: Pre-Index Users

#### Admin Panel `/sys/users/` Not Showing All Users — FIXED ✅

**Problem**: The admin panel `/sys/users/` page either showed no users or only platform-org admins.

**Root causes**:
1. **Frontend missing API functions**: `sysAdminListUsers()` and `sysAdminListAdmins()` were called in React components but never defined in `seafile-api.js` — calls failed silently
2. **Backend multi-org**: `ListAllUsers`, `ListAdminUsers`, `SearchUsers` only queried `WHERE org_id = ?` using the caller's org. Superadmin in platform org → only saw platform users (admins). Tenant users invisible.
3. **`users_by_email` gap**: OIDC `createUser()` and `AdminAddOrgUser` wrote to `users` but not `users_by_email`. DELETE/GET by email → 404 for OIDC-provisioned users.

**Fixes**:
- Added 13 `sysAdmin*` user management functions to `frontend/src/utils/seafile-api.js`
- `ListAllUsers`, `ListAdminUsers`, `SearchUsers` now query ALL orgs for superadmin (same pattern as `AdminListAllLibraries`)
- `ListAdminUsers` response key changed from `"data"` to `"admin_user_list"` (matches frontend model)
- OIDC `createUser()` and `AdminAddOrgUser` now dual-write to `users_by_email`

**Files changed**: `admin.go`, `oidc.go`, `admin_extra.go`, `seafile-api.js`

### Previous Session (Session 45) — Superadmin Script + CreateOrganization Fix

#### make-superadmin.sh — New Script ✅

**Problem**: Users authenticated via OIDC end up in a tenant org, not the platform org
(`00000000-0000-0000-0000-000000000000`). `RequireSuperAdmin()` middleware checks both
the `superadmin` role AND the platform org_id, so they got 403 on org management endpoints.

**Fix**: New script `scripts/make-superadmin.sh <email> [name]` that:
- Looks up user by email in `users_by_email`; reuses their user_id if found, or generates a new UUID
- Upserts user record in platform org with `role=superadmin` and unlimited quota
- Updates `users_by_email` to map email → platform org
- Invalidates existing sessions so the new role takes effect on next login
- Works via `docker compose exec cassandra cqlsh` (pass `--host` for direct access)

**Usage**: `./scripts/make-superadmin.sh your@email.com "Your Name"`

**OIDC note**: If OIDC provisioning re-assigns the user to their tenant org on re-login,
configure `OIDC_PLATFORM_ORG_CLAIM_VALUE` in `.env` so the provider sends the matching
claim. See `docs/OIDC.md`.

#### CreateOrganization API — seafile-js Compatibility Fix ✅

**Problem**: Frontend `sysAdminAddOrg(orgName, ownerEmail, password)` (seafile-js method)
sends FormData with `org_name`, `owner_email`, `password` but backend only accepted
JSON `{ "name": "..." }` → mismatch caused bad-request errors even with correct auth.

**Fix** (`internal/api/v2/admin.go`):
- Handler now auto-detects content type (FormData → form values, else → JSON)
- Accepts `org_name` (seafile-js) or `name` (our format) — tries `org_name` first
- Accepts `owner_email`: creates an admin user in the new org (dual-write to `users` +
  `users_by_email` with IF NOT EXISTS to avoid overwriting existing OIDC sessions)
- Accepts `password` (ignored — OIDC-only system)
- Response includes `creator_email`, `creator_name`, `users_count` (1 if owner created)

### Previous Session (Session 44) — Desktop Client File Browser & Upload Fixes

### Completed This Session (Session 44)

#### Desktop File Browser Broken — Missing `oid` Header — FIXED ✅

**Problem**: Seafile desktop file browser showed "Fallo al obtener información de archivos" for all libraries despite server returning 200.

**Root cause**: `ListDirectory` (`GET /api2/repos/:id/dir/`) didn't set `oid` and `dir_perm` response headers. Seafile Qt client reads these via `rawHeader()` and treats response as invalid without them.

**Fix**: Added `c.Header("oid", currentFSID)` and `c.Header("dir_perm", "rw")` to all success paths.

#### Upload/Download Fails — "Protocol ttps/ttp is unknown" — FIXED ✅

**Problem**: File upload and download from desktop file browser failed. Client logs: `Protocol "ttps" is unknown` (prod) / `Protocol "ttp" is unknown` (local).

**Root cause**: Three functions (`GetUploadLink`, `GetDownloadLink`, `getFileDownloadURL`) used `c.String()` (plain text). Client expects JSON-quoted string (`"https://..."`), strips first/last char (quotes). Without quotes → stripped `h` → `ttps://` or `ttp://`.

**Fix**: Changed all three to `c.JSON(http.StatusOK, url)` which wraps the string in JSON quotes.

#### `head-commits-multi` Trailing Slash 502 — FIXED ✅

**Problem**: Client sends `POST /seafhttp/repo/head-commits-multi/` (with slash), 502 response.

**Fix**: Added trailing-slash duplicate route in `sync.go`.

**Files changed**: `internal/api/v2/files.go`, `internal/api/sync.go`

#### Previous Session (Session 34) — Sharing Endpoints Bug Fixes ✅

**Verified complete and correct** from Session 33:
- ✅ `upload_links` + `upload_links_by_creator` tables exist in `db.go`
- ✅ All 6 admin endpoints registered and implemented in `admin_extra.go`
- ✅ User CRUD endpoints in `upload_links.go`
- ✅ Frontend `sysAdmin*` methods wired in `seafile-api.js`
- ✅ No UUID marshaling issues (all use `.String()` correctly)
- ✅ Dual-delete with `gocql.LoggedBatch` for consistency
- ✅ Proper caching (libNameCache, userEmailCache) to avoid repeated queries

### Previous Session (Session 33)

#### Admin Share Link & Upload Link Management — ALL 13 ENDPOINTS ✅

**Share link admin fixes** (`internal/api/v2/admin_extra.go`):
- Fixed `AdminListShareLinks` — corrected column names (`share_token`, `library_id`, `created_by`), added repo_name resolution via `libraries` table, creator email/name lookup with caching, `order_by`/`direction` sorting
- Fixed `AdminDeleteShareLink` — added dual-delete from both `share_links` + `share_links_by_creator` using `gocql.LoggedBatch`

**Upload links — full new feature**:
- Created DB tables: `upload_links` + `upload_links_by_creator` (`internal/db/db.go`)
- Created `internal/api/v2/upload_links.go` — `ListUploadLinks`, `CreateUploadLink`, `DeleteUploadLink`, `ListRepoUploadLinks`
- Implemented admin handlers: `AdminListUploadLinks`, `AdminDeleteUploadLink`

**Per-user link endpoints** (admin):
- `AdminListUserShareLinks` — queries `share_links_by_creator` by email→user_id
- `AdminListUserUploadLinks` — queries `upload_links_by_creator` by email→user_id

**Frontend API** (`frontend/src/utils/seafile-api.js`):
- Added 6 `sysAdmin*` methods for link management

**Route registration**: `internal/api/server.go` — added `RegisterUploadLinkRoutes`

### Previous Session (Session 32)

#### Bug Fix Sprint — ALL 5 BUGS RESOLVED ✅

1. **OnlyOffice toolbar greyed out** — Root cause: `generateDocKey()` rotated key every 60s via `time.Now().Unix()/60`. Removed timestamp, added `compactToolbar`/`compactHeader`, added JWT `exp` (8h). Files: `internal/api/v2/onlyoffice.go`

2. **Missing folder icon variants** — Created 6 PNGs: `folder-read-only-{24,192}`, `folder-shared-out-{24,192}`, `folder-read-only-shared-out-{24,192}`. Files: `frontend/public/static/img/`

3. **Role hierarchy maps duplicated** — Verified already resolved: all 3 files delegate to `middleware.HasRequiredOrgRole()`. No changes needed.

4. **Admin Panel not loading** — Verified already working in Docker: `/sys/` returns HTTP 200, sysadmin.html served. No changes needed.

5. **Tagged files list shows deleted files** — Already fixed by job-001 (TraverseToPath filtering). Enhanced with cascade tag cleanup.

#### Tag Management Enhancement ✅
- Added `MoveFileTagsByPath()` — migrates file tags when files are renamed
- Added `MoveFileTagsByPrefix()` — migrates all tags under a directory when renamed
- Added `CleanupAllLibraryTags()` — removes all 6 tag tables' data when library permanently deleted
- Wired into: `RenameFile`, `RenameDirectory`, `PermanentDeleteRepo`
- Files: `internal/api/v2/tags.go`, `internal/api/v2/files.go`, `internal/api/v2/deleted_libraries.go`
- Live-tested: tag migration on rename confirmed working

### Previous Sessions (31 and earlier — see docs/CHANGELOG.md)

#### Search Bug Fix ✅
**Problem**: Search for "test" returned empty results despite files named "test.docx" existing.
**Root cause**: `obj_name` never populated during Seafile sync + SASI disabled in Cassandra 5.x
**Result**: Search now returns libraries and files with correct paths. 20/20 API suites pass.

### Previous Sessions (30 and earlier — see docs/CHANGELOG.md)

- **Session 30**: Snapshot View, RevertFile/RevertDirectory with conflict handling, frontend conflict dialog
- **Session 29**: Search 404, Tag deletion 500, Trash endpoints, Library Recycle Bin
- **Session 28**: GC Prometheus metrics, Raw preview fix, Image lightbox fix, History dedup
- **Session 27**: File preview tests, freeze candidate analysis
- **Sessions 22-26**: Admin Panel, OIDC sync, File History UI, crypto coverage, share links
- **Sessions 12-21**: GC, Monitoring, Departments, modal migration, move/copy fixes
- **Sessions 1-11**: Core API, tags, permissions, OIDC, library settings, OnlyOffice

---

## What's Next (Priority Order) 🎯

### ✅ ~~PRIORITY 1: Admin Library Management~~ — DONE (2026-02-12)

**Status**: ✅ Complete — 12 endpoints implemented in `internal/api/v2/admin.go`
**Details**: [docs/ADMIN-FEATURES.md](docs/ADMIN-FEATURES.md) § 1

All admin library endpoints implemented: list, search, get, create, delete, transfer, browse dirents, history settings, shared items, trash libraries. Frontend `seafile-api.js` methods already wired.

### ✅ ~~PRIORITY 2: Admin Share Link & Upload Link Management~~ — DONE (2026-02-12)

**Status**: ✅ Complete — 13 endpoints across 5 files
**Details**: [docs/ADMIN-FEATURES.md](docs/ADMIN-FEATURES.md) § 2

Admin share link list/delete fixed; upload links full feature (DB tables, user CRUD, admin list/delete); per-user link endpoints; frontend API methods added.

### 🔴 PRIORITY 3: Audit Logs & Activity Logs — PRIORITIZE NEXT

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
| **Admin Link Management** | ✅ DONE | Share + upload links. 13 endpoints. See [ADMIN-FEATURES.md](docs/ADMIN-FEATURES.md) § 2 |
| **Org Delete (DeactivateOrg)** | ⚠️ INCOMPLETE | Soft-deactivate only (`settings['status']`), no filtering in list, no cascade. See [ADMIN-FEATURES.md](docs/ADMIN-FEATURES.md) § DeactivateOrganization |
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
| File Tags | ✅ COMPLETE | 2026-02-12 (cascade+rename) |
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

### Code Files - Web Downloads (Updated 2026-02-16)
- `internal/api/seafhttp.go` - `streamFileFromBlocks()` (primary download path — prefetch pipeline, 4MB buffers)
- `internal/api/seafhttp.go` - `HandleDownload()` (token validation, 4MB streaming buffer)
- `internal/api/seafhttp.go` - `addFileToZip()` (ZIP Store method, batch block resolve, 4MB buffers)
- `internal/api/seafhttp.go` - `resolveBlockIDs()` (batch Cassandra IN queries, 100/batch)
- `internal/api/v2/fileview.go` - `ServeRawFile()` / `DownloadHistoricFile()` (batch resolve + 4MB buffers)
- `internal/api/v2/sharelink_view.go` - Share link raw file streaming (batch resolve + 4MB buffers)
- `internal/storage/s3.go` - Custom HTTP transport (64 conn/host, 128KB read buffers)
- ⚠️ `getFileFromBlocks()` is DEPRECATED — kept only for upload metadata path

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

### 📊 Current State (Updated 2026-02-12)
- **Sync Protocol**: 100% working, desktop clients fully compatible 🔒 FROZEN
- **Backend API**: ~98% implemented — OIDC ✅, GC ✅, Library Settings ✅, OnlyOffice ✅, Tags cascade ✅
- **Frontend UI**: ~83% functional (all modals migrated, folder icons ✅, ~51 ModalPortal wrappers to clean up)
- **Production Ready**: All production blockers complete — OIDC ✅, GC ✅, Monitoring ✅
- **Active Bugs**: 0 open (all 5 resolved Session 32)

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
