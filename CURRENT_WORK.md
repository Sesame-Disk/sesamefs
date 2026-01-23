# Current Work - SesameFS

**Last Updated**: 2026-01-23
**Session**: Documentation Cleanup & Organization

**📏 File Size Rule**: Keep this file under **500 lines** unless unavoidable. Move detailed content to:
- `docs/KNOWN_ISSUES.md` - Detailed bug tracking
- `docs/CHANGELOG.md` - Session history
- `docs/IMPLEMENTATION_STATUS.md` - Component status
- Other appropriate documentation files

---

## 🚀 NEW SESSION? START HERE

**You are an AI assistant starting a new session.** Read this section first (5 min):

### Step 1: Understand Current State
1. **"What's Next"** → Top priorities (work on #1 unless user specifies)
2. **"Frozen Components"** → What NOT to touch (breaks desktop clients)
3. **"Critical Context"** → Essential facts to remember

### Step 2: Before Making ANY Code Changes
- ✅ Check `docs/IMPLEMENTATION_STATUS.md` - Is component 🔒 FROZEN?
- ✅ If FROZEN → DO NOT MODIFY without explicit user approval
- ✅ If ✅ COMPLETE → Modify with caution, verify tests pass
- ✅ If 🟡 PARTIAL / ❌ TODO → Safe to actively develop

### Step 3: Check Known Issues
- ✅ Read `docs/KNOWN_ISSUES.md` - Current bugs and regressions
- ✅ Prioritize CRITICAL regressions first

### Step 4: Follow Protocol-Driven Workflow
- ✅ See `docs/DECISIONS.md` for 6-step protocol verification process
- ✅ Stock Seafile (app.nihaoconsult.com) is ALWAYS the reference
- ✅ Test sync protocol changes with `./run-sync-comparison.sh` and `./run-real-client-sync.sh`

### Step 5: At End of Session - Update Documentation
**📋 MANDATORY: Run [docs/SESSION_CHECKLIST.md](docs/SESSION_CHECKLIST.md)**

Quick checklist:
- [ ] Update `CURRENT_WORK.md` (what was done, next priorities)
- [ ] Update `docs/KNOWN_ISSUES.md` (bugs fixed/discovered)
- [ ] Update `docs/CHANGELOG.md` (add session entry)
- [ ] Update `docs/IMPLEMENTATION_STATUS.md` (if component status changed)
- [ ] Update `docs/API-REFERENCE.md` (if endpoints added/changed)
- [ ] Keep `CURRENT_WORK.md` under 500 lines (move content to appropriate docs)
- [ ] Update all "Last Verified: YYYY-MM-DD" dates

---

## Last Session Summary ✅

**Date**: 2026-01-23
**Focus**: Documentation cleanup, frontend debugging methodology

### Completed
- ✅ Created `docs/KNOWN_ISSUES.md` - Detailed bug tracking (moved from CURRENT_WORK.md)
- ✅ Created `docs/CHANGELOG.md` - Session history (moved from CURRENT_WORK.md)
- ✅ Refactored `CURRENT_WORK.md` - Reduced from 822 → ~350 lines
- ✅ Documented frontend browser cache debugging methodology
- ✅ Fixed lib-decrypt-dialog close button (browser cache issue)
- ✅ Froze working frontend components (library list, starred items, download)

### Discovered
- 🔴 **CRITICAL REGRESSION**: Share modal broken with 500 error (was working 2026-01-22)
- ⚠️ Media files download instead of opening viewer (UX regression)
- ⚠️ Library advanced settings missing backend (History, API Token, Auto Deletion)

**Full details**: See `docs/CHANGELOG.md` and `docs/KNOWN_ISSUES.md`

---

## What's Next (Priority Order) 🎯

### 🚨 CRITICAL REGRESSIONS (Must Fix First)

**1. Share Modal Broken** (2-4 hours) - 🔥 HIGHEST PRIORITY
- **Issue**: `GET /api/v2.1/share-links/?repo_id={id}` returns 500 error
- **Impact**: Sharing completely broken, was working yesterday
- **Files**: `internal/api/v2/share_links.go`
- **Details**: See `docs/KNOWN_ISSUES.md` → "Share Modal Completely Broken"

**2. Media File Viewer Not Working** (4-6 hours) - 🔥 HIGH PRIORITY
- **Issue**: Clicking viewable files downloads instead of opening viewer
- **Impact**: Major UX regression - users expect inline preview
- **Files**: `src/components/dirent-list-view/dirent-list-item.js`
- **Details**: See `docs/KNOWN_ISSUES.md` → "Media Files Download Instead of Opening Viewer"

### ⚡ Production-Critical Backend

**3. Permission Middleware Integration** (2-4 hours)
- **Status**: Middleware built ✅, needs integration into routes
- **Files**: `internal/api/server.go`, `internal/api/v2/*.go`
- **Note**: See `internal/middleware/README.md` for usage guide

**4. File Operations Backend** (1-2 days)
- **Issue**: Move/copy endpoints return 405
- **Files**: `internal/api/v2/files.go`
- **Frontend Ready**: `move-dirent-dialog.js`, `copy-dirent-dialog.js` exist

**5. Library Advanced Settings Backend** (1-2 days)
- **Missing**: History Setting, API Token, Auto Deletion Setting
- **Files**: `internal/api/v2/libraries.go`
- **Frontend Ready**: Dialogs exist, just need backend

### 📋 Complete Feature List

**For complete prioritized roadmap, see**:
- Phase 1: Backend completion (sharing, groups, tags) - See Section below
- Phase 2: Frontend polish (modal fixes, icons) - See `docs/TECHNICAL-DEBT.md`
- Phase 3: Production readiness (OIDC, monitoring, docs) - See Section below

---

## Strategic Roadmap

### Phase 1: Complete Missing Backend (Weeks 1-4)

**1.1 Sharing System Backend** ⭐ HIGH
- Implement share to users backend (`POST /api/v2.1/file-shares/`)
- Implement share to groups backend
- Implement share links CRUD (view/edit/upload links)
- Implement permissions management (read/write/admin)
- **Frontend Ready**: All sharing dialogs exist ✅

**1.2 File Operations Backend** ⭐ HIGH
- Complete create/delete/rename file/folder endpoints
- Complete move/copy file endpoints
- **Frontend Ready**: All operation dialogs exist ✅

**1.3 Groups Backend** ⭐ MEDIUM-HIGH
- Implement create/rename/delete group
- Implement add/remove members
- Implement group permissions
- **Frontend Ready**: All group dialogs exist ✅

**1.4 File Tags Backend** - MEDIUM
- Implement create/edit/delete tags
- Implement tag file/folder
- **Frontend Ready**: Tag dialogs exist ✅

**1.5 Search Backend** ✅ COMPLETE (2026-01-22)
- Cassandra SASI indexes implemented
- Search libraries/files by name
- Filter by repo_id, type

### Phase 2: Frontend Polish (Weeks 3-5, Parallel to Phase 1)

**2.1 Modal Dialog Migration** (Incremental)
- Fix top 10-15 user-facing dialogs (2-3 days)
- Pattern: Replace reactstrap Modal with plain Bootstrap classes
- See `delete-repo-dialog.js` for working example
- **List**: See `docs/TECHNICAL-DEBT.md` or grep for broken modals

**2.2 Icon & Asset Audit** (1 day)
- Audit missing icons in `frontend/public/static/img/`
- Add fallback icons for missing types
- Fix any broken image paths

### Phase 3: Production Readiness (Week 6+)

**3.1 Authentication & Security** - CRITICAL for production (1 week)
- Implement OIDC/OAuth integration
- Add session management
- Add password change functionality
- Security audit

**3.2 Error Handling & Monitoring** - HIGH for production (3-5 days)
- Add comprehensive error handling
- Add structured logging
- Add metrics/monitoring (Prometheus?)
- Add health check endpoints

**3.3 Documentation & Deployment** - HIGH for production (3-5 days)
- User documentation
- Admin documentation
- Production deployment guide
- Backup/restore procedures
- Migration guide (from Seafile)

---

## Frozen/Stable Components 🔒

### ⚠️ CRITICAL: Sync Code FROZEN (2026-01-19)
**User directive**: DO NOT MODIFY sync code without explicit approval
**Reason**: Sync protocol working perfectly with desktop clients
**Impact**: Changes could break desktop/mobile client sync

### Code Files - Sync Protocol 🔒
- `internal/crypto/crypto.go` - PBKDF2 implementation
- `internal/api/sync.go` (lines 949-952, 125-130, 1405-1492) - Protocol formats
- `internal/api/v2/encryption.go` - Password endpoints

### Code Files - Web Downloads 🔒 (Frozen 2026-01-20)
- `internal/api/seafhttp.go:1253-1317` - `findEntryInDir()` (file lookup)
- `internal/api/seafhttp.go:1034-1189` - `getFileFromBlocks()` (block retrieval)
- `internal/api/seafhttp.go:963-1030` - `HandleDownload()` (token validation)

### Frontend Components 🔒 (Frozen 2026-01-23)
**User directive**: These work correctly - DO NOT MODIFY without approval
- `frontend/src/pages/my-libs/` - Library list view
- `frontend/src/pages/starred/` - Starred files & libraries
- `frontend/src/components/dirent-list-view/` - File download functionality

### Protocol Behaviors 🔒
- fs-id-list: JSON array (NOT newline-separated)
- Commit objects: OMIT `no_local_history` field
- `encrypted` field: integer in download-info, string in commits
- `is_corrupted` field: integer 0 (NOT boolean)
- `/seafhttp/` auth: `Seafile-Repo-Token` header (NOT `Authorization`)
- pack-fs format: 40-byte ID + 4-byte size (BE) + zlib-compressed JSON

### Documentation 🔒
- `docs/SEAFILE-SYNC-PROTOCOL-RFC.md` - Formal specification with test vectors
- `docs/ENCRYPTION.md` - Encryption implementation guide

**Why frozen?** Desktop client sync tested and working. Breaking these = breaking all clients.

---

## Critical Context for Next Session 📝

### 🎯 Project Goal
**Mission**: Build complete Seafile replacement ready for production
**Target Users**: Global cloud storage, especially needing China access
**Timeline**: ASAP but thorough - "want it soon, do it right"
**Success Metric**: Can objectively replace Seafile in production

### 📊 Current State
- **Sync Protocol**: 100% working, desktop clients fully compatible 🔒 FROZEN
- **Backend API**: ~75% implemented, 20% stubbed, 5% not started
- **Frontend UI**: ~60% functional, ~100 dialogs broken (modal issue)
- **Production Ready**: NO - missing OIDC, permissions middleware, monitoring
- **Test Coverage**: ~40%

### 🚀 Strategic Approach
**Frontend-driven development**: Let frontend dictate backend priorities (many features have UI but no backend)

### Critical Facts to Remember

**Protocol Development**:
- Stock Seafile (app.nihaoconsult.com) is ALWAYS the reference
- Use `./run-sync-comparison.sh` to verify protocol changes
- Use `./run-real-client-sync.sh` to test with seaf-cli
- Protocol bugs = broken desktop clients = critical severity

**Authentication**:
- REST API (`/api2/`, `/api/v2.1/`): `Authorization: Token {api_token}`
- Sync protocol (`/seafhttp/`): `Seafile-Repo-Token: {sync_token}`
- Sync token from: `GET /api2/repos/{id}/download-info/`

**Encryption (PBKDF2)**:
- Magic computation: input = `repo_id + password`
- Random key encryption: input = `password` ONLY
- PBKDF2: 1000 iterations for key, 10 for IV
- Static salt for enc_version 2: `{0xda, 0x90, 0x45, 0xc3, 0x06, 0xc7, 0xcc, 0x26}`
- **Details**: See `docs/ENCRYPTION.md`

**Frontend Development**:
- Modal dialogs: Use plain Bootstrap classes, NOT reactstrap Modal
- Reason: ModalPortal wrapper causes double-portal issue
- **Pattern**: See `CLAUDE.md` → "Frontend Critical Context" → "Modal Pattern"
- **Browser Cache**: Test fixes with standalone HTML first, see `CLAUDE.md` → "Browser Cache Issues"

**Block Storage**:
- Block ID mapping: SHA-1 (external/client) → SHA-256 (internal/storage)
- Table: `block_id_mappings` (columns: `external_id`, `internal_id`)
- Desktop clients use SHA-1, server stores SHA-256

**Permissions System**:
- Database schema: ✅ COMPLETE
- Middleware: ✅ BUILT (see `internal/middleware/`)
- Integration: ❌ NOT APPLIED to routes yet
- **Priority**: MEDIUM-HIGH for production

**Encrypted Library Sharing Policy**:
- **POLICY**: Password-encrypted libraries CANNOT be shared
- **Reason**: Would require sharing encryption key, breaking security
- **Status**: ❌ NOT ENFORCED yet
- **Priority**: HIGH

---

## Documentation Map 📚

### Session Continuity (Read First Every Session)
- **[CURRENT_WORK.md](CURRENT_WORK.md)** - This file - Session state, priorities
- **[docs/KNOWN_ISSUES.md](docs/KNOWN_ISSUES.md)** - Detailed bug tracking
- **[docs/CHANGELOG.md](docs/CHANGELOG.md)** - Session history
- **[docs/SESSION_CHECKLIST.md](docs/SESSION_CHECKLIST.md)** - End-of-session checklist
- **[docs/IMPLEMENTATION_STATUS.md](docs/IMPLEMENTATION_STATUS.md)** - Component stability matrix
- **[docs/DECISIONS.md](docs/DECISIONS.md)** - Protocol-driven workflow, architecture decisions

### Protocol & Sync (🔒 Reference Implementation)
- **[docs/SEAFILE-SYNC-PROTOCOL-RFC.md](docs/SEAFILE-SYNC-PROTOCOL-RFC.md)** - Formal RFC with test vectors 🔒
- **[docs/SEAFILE-SYNC-PROTOCOL.md](docs/SEAFILE-SYNC-PROTOCOL.md)** - Quick reference
- **[docs/SYNC-TESTING.md](docs/SYNC-TESTING.md)** - Protocol testing with seaf-cli
- **[docker/seafile-cli-debug/COMPREHENSIVE_TESTING.md](docker/seafile-cli-debug/COMPREHENSIVE_TESTING.md)** - 7 test scenarios
- **[docs/ENCRYPTION.md](docs/ENCRYPTION.md)** - Encrypted libraries, PBKDF2, Argon2id

### Implementation Guides
- **[README.md](README.md)** - Quick start, features overview
- **[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)** - Design decisions, storage architecture
- **[docs/API-REFERENCE.md](docs/API-REFERENCE.md)** - API endpoints, implementation status
- **[docs/ENDPOINT-REGISTRY.md](docs/ENDPOINT-REGISTRY.md)** - ⚠️ CHECK BEFORE ADDING ENDPOINTS
- **[docs/DATABASE-GUIDE.md](docs/DATABASE-GUIDE.md)** - Cassandra tables, queries
- **[docs/FRONTEND.md](docs/FRONTEND.md)** - React frontend patterns, modal fixes
- **[docs/TESTING.md](docs/TESTING.md)** - Test coverage, benchmarks
- **[docs/TECHNICAL-DEBT.md](docs/TECHNICAL-DEBT.md)** - Known issues, modal pattern fixes
- **[CLAUDE.md](CLAUDE.md)** - Complete project context for AI assistant

### Other
- **[docs/LICENSING.md](docs/LICENSING.md)** - Legal considerations

---

## Quick Commands

**See [CLAUDE.md](CLAUDE.md) for complete command reference.**

```bash
# Protocol verification (MUST PASS before freezing protocol changes)
cd docker/seafile-cli-debug
./run-sync-comparison.sh          # API-level protocol comparison
./run-real-client-sync.sh          # Real desktop client sync test

# Run server
docker-compose up -d sesamefs

# Frontend development
cd frontend && npm install && npm start  # Runs on port 3001

# Frontend Docker rebuild (if changes don't appear)
docker-compose build --no-cache frontend && docker-compose up -d frontend

# Run tests
go test ./...
```

---

## End of Session Checklist

**📋 See [docs/SESSION_CHECKLIST.md](docs/SESSION_CHECKLIST.md) for complete checklist**

Quick reminders:
- [ ] Update `CURRENT_WORK.md` (what was done, next priorities)
- [ ] Update `docs/KNOWN_ISSUES.md` (bugs fixed/discovered)
- [ ] Update `docs/CHANGELOG.md` (add session entry)
- [ ] Keep `CURRENT_WORK.md` under 500 lines
- [ ] Update timestamps and "Last Verified" dates
