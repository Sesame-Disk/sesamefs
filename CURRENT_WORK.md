# Current Work - SesameFS

**Last Updated**: 2026-02-02
**Session**: Session 21 — GC TTL Enforcement, Groups Fix, Nav Cleanup, Admin Panel Research

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

**Date**: 2026-02-02
**Focus**: GC TTL Enforcement, Groups Fix, Nav Cleanup, Admin Panel Research

### Completed This Session (Session 21)

#### GC Scanner Phase 5: Version TTL Enforcement ✅
- Implemented `scanExpiredVersions()` — walks HEAD chain, enqueues expired non-HEAD commits
- Added store methods: `ListLibrariesWithVersionTTL()`, `ListCommitsWithTimestamps()`, `DeleteShareLink()`
- Fixed `processShareLink()` to actually delete share links (was only logging)
- 4 new unit tests, all 13 scanner tests pass

#### Groups 500 Error Fix ✅
- Root cause: `google/uuid.UUID` passed directly to gocql (must use `.String()`)
- Fixed ALL 7 group handlers

#### "Shared with me" Filter Fix ✅
- `ListLibrariesV21` now respects `type` query parameter

#### Nav Item Cleanup ✅
- Hidden: Published Libraries, Linked Devices, Share Admin (all had 404 backend errors)
- Added stub endpoints returning empty arrays to prevent console errors

#### Admin Panel Research (See "PRIORITY 1" below) 🔍
- Full exploration of sys-admin frontend, backend admin endpoints, and Seafile admin model
- Key decision needed: OIDC-managed vs SesameFS-managed groups/departments

### Previous Sessions (18-20)

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

### 🔴 PRIORITY 1: Admin Panel — Groups, Departments, Users (DECISION NEEDED)

**Status**: Research complete, implementation pending a design decision
**Research Date**: 2026-02-02

The admin panel is the biggest remaining feature gap. The Seafile frontend has a full sys-admin panel at `/sys/` with management pages for users, groups, departments, organizations, libraries, etc. All the React components exist in `frontend/src/pages/sys-admin/` but are **not wired up** (the webpack config only includes the `app` chunk in `index.html`; `sysAdmin` is a separate entry point designed for Django).

#### The Decision: Where Do Groups & Departments Live?

Since our OIDC provider is the source of truth for **tenants** (organizations) and **users** (auto-provisioned on login), the question is: should it also manage **groups** and **departments**, or should SesameFS manage those directly?

**Option A: OIDC Provider Manages Groups & Departments (Recommended)**

The OIDC provider emits group/department membership as claims in the ID token. SesameFS syncs on login.

| Aspect | Detail |
|--------|--------|
| **How it works** | OIDC token includes claims like `groups: ["engineering", "design"]` and `departments: ["eng/backend"]`. On login, SesameFS syncs group/dept membership from claims. |
| **OIDC provider needs** | Custom claims for `groups` (flat list) and `departments` (hierarchical paths or IDs). Provider manages group CRUD, membership. |
| **SesameFS admin panel** | Read-only view of groups/departments (synced from OIDC). Admins manage via the OIDC provider's admin UI. |
| **Pros** | Single source of truth. No dual management. Groups/depts consistent across all apps using the same OIDC provider. Aligns with existing tenant/user pattern. |
| **Cons** | Requires OIDC provider to support group management UI + custom claims. Users can't self-create ad-hoc groups in SesameFS. |
| **SesameFS work** | Add claim parsing for groups/departments on login. Sync membership to local DB. Admin panel is view-only. |

**Option B: SesameFS Manages Groups & Departments Locally**

Groups and departments are managed entirely within SesameFS via the admin panel and user UI.

| Aspect | Detail |
|--------|--------|
| **How it works** | Admins create groups/departments in SesameFS admin panel. Users create ad-hoc groups via the main UI. No OIDC involvement. |
| **OIDC provider needs** | Nothing beyond current tenant/user/role claims. |
| **SesameFS admin panel** | Full CRUD for groups, departments, members. Must implement ~25 admin API endpoints. |
| **Pros** | Self-contained. No OIDC provider changes needed. Users can create their own groups. |
| **Cons** | Groups/departments not synced with corporate directory. Dual management if other apps use the same OIDC provider for groups. |
| **SesameFS work** | Implement admin group/dept endpoints, wire up sys-admin frontend, set `window.sysadmin` config. |

**Option C: Hybrid — OIDC for Departments, SesameFS for Groups**

Departments (organizational structure) synced from OIDC. Groups (ad-hoc collaboration) managed locally in SesameFS.

| Aspect | Detail |
|--------|--------|
| **How it works** | Departments come from OIDC claims (e.g., `department: "Engineering/Backend"`). Groups are user-created in SesameFS. |
| **Pros** | Org structure stays in directory. Users still get flexible collaboration groups. Best of both worlds. |
| **Cons** | Two different management models. More complex to explain to admins. |
| **SesameFS work** | OIDC department sync + local group management + admin panel for both. |

#### Recommendation

**Option A is cleanest** if you're building the OIDC provider anyway — it keeps one source of truth and avoids dual management. The OIDC provider would need:
1. Group CRUD API + admin UI
2. Department hierarchy management
3. Custom claims: `groups` (array of group names/IDs), `department` (string or path)
4. SesameFS parses these on login and syncs to local `groups`/`group_members` tables

**Option C is most pragmatic** if you want quick results — departments from OIDC (since they mirror corporate structure), but groups are lightweight and users expect to create them ad-hoc in the storage app.

#### What's Needed Regardless of Decision

No matter which option, we need to:
1. **Wire up the admin panel frontend** — serve the `sysAdmin` webpack chunk at `/sys/`
2. **Set `window.sysadmin` config** — the frontend reads admin permissions from this
3. **Implement admin user list/search endpoints** — frontend calls `GET /admin/users/`, `GET /admin/search-user/`
4. **Implement admin group list endpoint** — even if read-only, frontend needs `GET /admin/groups/`

#### Backend Gap Analysis (Frontend Expects vs Backend Has)

| Frontend API Call | Endpoint | Backend Status |
|-------------------|----------|----------------|
| `sysAdminListUsers()` | `GET /admin/users/` | ❌ Missing (have per-org only) |
| `sysAdminSearchUsers()` | `GET /admin/search-user/` | ❌ Missing |
| `sysAdminAddUser()` | `POST /admin/users/` | ❌ Missing (OIDC auto-provision) |
| `sysAdminGetUser()` | `GET /admin/users/:email/` | 🟡 Partial (501 for superadmin) |
| `sysAdminUpdateUser()` | `PUT /admin/users/:email/` | 🟡 Partial (role/quota only) |
| `sysAdminListAllGroups()` | `GET /admin/groups/` | ❌ Missing |
| `sysAdminCreateNewGroup()` | `POST /admin/groups/` | ❌ Missing (user-facing exists) |
| `sysAdminDismissGroupByID()` | `DELETE /admin/groups/:id/` | ❌ Missing |
| `sysAdminListGroupMembers()` | `GET /admin/groups/:id/members/` | ❌ Missing |
| `sysAdminListAllDepartments()` | `GET /admin/address-book/groups/` | ✅ Exists |
| `sysAdminGetDepartmentInfo()` | `GET /admin/address-book/groups/:id/` | ✅ Exists |
| `sysAdminAddNewDepartment()` | `POST /admin/address-book/groups/` | ✅ Exists |
| `sysAdminListOrgs()` | `GET /admin/organizations/` | ✅ Exists |
| `sysAdminAddOrg()` | `POST /admin/organizations/` | ✅ Exists |

---

### 🟡 PRIORITY 2: Frontend ModalPortal Wrapper Cleanup

**Status**: All 122 dialog components migrated. ~51 parent components still use unnecessary `<ModalPortal>` wrappers.
Harmless cleanup — dialogs already render correctly.

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
| **Admin Panel (Groups/Users/Depts)** | 🔴 DECISION NEEDED | See PRIORITY 1 above — OIDC-managed vs local |
| **GC TTL Enforcement** | ✅ DONE | Scanner Phase 5 — version_ttl_days + share link deletion |
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
