# Current Work - SesameFS

**Last Updated**: 2026-01-20 (Session: File Download Fix)
**Last Worked By**: Claude Sonnet 4.5

---

## 🚀 NEW SESSION? START HERE

**You are an AI assistant starting a new session.** Read this section first (5 min):

### Step 1: Understand Current State (Read sections below in order)
1. **"What Was Just Completed"** → Know what was done last session
2. **"What's Next"** → Understand priorities (work on #1 unless user specifies)
3. **"Frozen Components"** → Know what NOT to touch (breaks desktop clients)
4. **"Context for Next Session"** → Critical facts to remember

### Step 2: Before Making ANY Code Changes
- ✅ Check `docs/IMPLEMENTATION_STATUS.md` - Is component 🔒 FROZEN?
- ✅ If FROZEN → DO NOT MODIFY without explicit user approval
- ✅ If ✅ COMPLETE → Modify with caution, check existing tests
- ✅ If 🟡 PARTIAL / ❌ TODO → Safe to actively develop

### Step 3: Follow Protocol-Driven Workflow
- ✅ See `docs/DECISIONS.md` for 6-step protocol verification process
- ✅ Stock Seafile (app.nihaoconsult.com) is ALWAYS the reference
- ✅ Test sync protocol changes with `./run-sync-comparison.sh` and `./run-real-client-sync.sh`

### Step 4: At End of Session - Update Documentation
**📋 MANDATORY: Run the [Session Checklist](docs/SESSION_CHECKLIST.md)**

Quick checklist:
- [ ] Update `CURRENT_WORK.md` (completed items, priorities, files modified)
- [ ] Update `docs/IMPLEMENTATION_STATUS.md` (if component status changed)
- [ ] Update `docs/API-REFERENCE.md` (if endpoints added/changed)
- [ ] Update `docs/SEAFILE-SYNC-PROTOCOL.md` (if sync protocol changed)
- [ ] Update `CLAUDE.md` (if frozen components or critical constraints added)
- [ ] Update all "Last Verified: YYYY-MM-DD" dates to current date
- [ ] Update timestamp and session ID at top of this file

**See [docs/SESSION_CHECKLIST.md](docs/SESSION_CHECKLIST.md) for complete checklist with all documentation update requirements.**

**Ready? Continue reading below for details.**

---

## What Was Just Completed ✅

### File Download Bug Fixed (2026-01-20 - This Session)
- ✅ **FIXED**: Web file downloads returning 404 "file not found"
- ✅ **ROOT CAUSE**: JSON string matching bug in `findEntryInDir()` - search pattern didn't account for JSON spacing
- ✅ **SOLUTION**: Replaced string matching with proper `json.Unmarshal()` parsing
- ✅ **FILES MODIFIED**: `internal/api/seafhttp.go:1253-1317`, `1034-1189`
- ✅ **STATUS**: File download feature now 🔒 FROZEN (working correctly)
- ✅ **SAFETY**: Only affects web downloads, does NOT touch sync protocol

### Recent Completed Work (Previous Sessions)
For details on previous sessions, see:
- 2026-01-19: Frontend feature audit, duplicate file sync bug fix → See git log
- 2026-01-18: "View on Cloud" feature, desktop re-sync fix → See git log
- 2026-01-17: Comprehensive sync protocol test framework → See `docker/seafile-cli-debug/COMPREHENSIVE_TESTING.md`
- 2026-01-16: Session continuity system, sync protocol fixes → See `docs/IMPLEMENTATION_STATUS.md`

---

## What's Next (Priority Order) 🎯

### STRATEGIC APPROACH: Frontend-Driven Development
**Goal**: Complete product ready to replace Seafile in production
**Method**: Let frontend dictate backend priorities (many backend features are stubs)
**Timeline**: ASAP but thorough - no rushing, do it right

---

### Phase 1: Complete Missing Backend (Highest Impact) - Weeks 1-4

#### 1.1 Sharing System Backend ⭐ HIGHEST PRIORITY
**Status**: UI complete, backend stubbed
**User Impact**: CRITICAL - core collaboration feature
**Effort**: 3-5 days
**What needs doing**:
- Implement share to users backend (`POST /api/v2.1/file-shares/`)
- Implement share to groups backend
- Implement share links CRUD (view/edit/upload links)
- Implement permissions management (read/write/admin)
- Add share notification system
- Test with existing frontend UI

**Frontend files ready**:
- `share-dialog.js` - Main sharing dialog ✅
- `share-to-user.js` - User sharing UI ✅
- `share-to-group.js` - Group sharing UI ✅
- `share-link-panel/` - Link management UI ✅
- `generate-upload-link.js` - Upload link creation ✅

#### 1.2 File Operations Backend (Core CRUD) ⭐ HIGH PRIORITY
**Status**: UI working, backend partially implemented
**User Impact**: HIGH - basic file management
**Effort**: 2-3 days
**What needs doing**:
- Complete create file/folder endpoints
- Complete delete file/folder endpoints
- Complete rename file/folder endpoints
- Complete move/copy file endpoints
- Test all operations with frontend

**Frontend files ready**:
- `create-file-dialog.js` ✅
- `create-folder-dialog.js` ✅
- `rename-dirent.js` ✅
- `copy-dirent-dialog.js` ✅
- `move-dirent-dialog.js` ✅

#### 1.3 Groups Backend ⭐ HIGH PRIORITY
**Status**: UI complete, backend stubbed
**User Impact**: MEDIUM-HIGH - team collaboration
**Effort**: 2-3 days
**What needs doing**:
- Implement create/rename/delete group
- Implement add/remove members
- Implement group permissions
- Implement group-owned repos
- Test with frontend

**Frontend files ready**:
- `create-group-dialog.js` ✅
- `rename-group-dialog.js` ✅
- `dismiss-group-dialog.js` ✅
- `list-and-add-group-members.js` ✅

#### 1.4 File Tags Backend
**Status**: UI complete, backend stubbed
**User Impact**: MEDIUM - organization feature
**Effort**: 1-2 days
**What needs doing**:
- Implement create/edit/delete tags
- Implement tag file/folder
- Implement list files by tag
- Test with frontend

#### 1.5 Search Backend
**Status**: Basic implementation exists, needs completion
**User Impact**: HIGH - content discovery
**Effort**: 3-5 days
**What needs doing**:
- Complete full-text search implementation
- Add file type filters
- Add date range filters
- Optimize search performance
- Test with frontend

---

### Phase 2: Frontend Polish - Weeks 3-5 (Parallel to Phase 1)

#### 2.1 Modal Dialog Migration (Incremental)
**Status**: 79/158 dialogs broken (reactstrap Modal issue)
**Priority**: Fix on demand + automate high-priority ones
**User Impact**: MEDIUM - some dialogs don't show

**Approach**: Don't fix all 79 at once - too much effort for ROI
**Strategy**:
1. Fix top 10-15 user-facing dialogs (2-3 days)
   - Group management (create, rename, leave, dismiss)
   - Share link dialogs (view, generate)
   - Invitation dialogs (invite, revoke)
   - Tag dialogs (create, edit)
2. Write automation script for remaining dialogs (1 day)
3. Run script on admin dialogs (low priority)

**Pattern to apply**: See `delete-repo-dialog.js` for working example
- Remove `reactstrap Modal` imports
- Use plain Bootstrap modal classes
- Handle focus with componentDidMount instead of onOpened

#### 2.2 Icon & Asset Audit
**Status**: Some file type icons return 404
**Priority**: MEDIUM - visual polish
**Effort**: 1 day
**What needs doing**:
- Audit missing icons in `frontend/public/static/img/`
- Add fallback icons for missing types
- Test HiDPI icon loading
- Fix any broken image paths

#### 2.3 OnlyOffice Configuration Tuning
**Status**: Working but toolbar sometimes greyed out
**Priority**: MEDIUM - document editing UX
**Effort**: 1-2 days
**What needs doing**:
- Simplify OnlyOffice config (match Seahub minimal approach)
- Fix toolbar grayed-out issue
- Test save/close cycle thoroughly
- Document working configuration

---

### Phase 3: Production Readiness - Week 6

#### 3.1 Authentication & Security
**Status**: Only dev tokens implemented
**Priority**: CRITICAL for production
**Effort**: 1 week
**What needs doing**:
- Implement OIDC/OAuth integration
- Add multi-factor authentication (optional)
- Add session management
- Add password change functionality
- Security audit

#### 3.2 Error Handling & Monitoring
**Status**: Basic error handling
**Priority**: HIGH for production
**Effort**: 3-5 days
**What needs doing**:
- Add comprehensive error handling
- Add logging (structured logs)
- Add metrics/monitoring (Prometheus?)
- Add health check endpoints
- Add alerting

#### 3.3 Documentation & Deployment
**Status**: Partial
**Priority**: HIGH for production
**Effort**: 3-5 days
**What needs doing**:
- User documentation
- Admin documentation
- Deployment guide (production-ready)
- Backup/restore procedures
- Migration guide (from Seafile)

---

### 🚨 IMMEDIATE PRIORITY: Fix Remaining Critical Issues

**BEFORE starting new features, fix these broken items:**

#### Week 1: Critical Bug Fixes (2-3 days)
1. **Fix lib-decrypt-dialog close button** (2 hours)
   - Add X close button to top-right corner
   - Test dialog can be dismissed

2. **Fix share dialog for encrypted libraries** (4 hours)
   - Disable sharing UI for encrypted libraries
   - Show message: "Move files to non-encrypted library to share"
   - Fix Internal Link loading spinner hang

3. **Implement move/copy backend** (1-2 days)
   - Add move file endpoint (currently 405)
   - Add copy file endpoint (currently 405)
   - Test with frontend dialogs

#### Week 2-3: Complete File Operations Backend
Then continue with file operations, sharing, groups as planned in Phase 1

---

## Known Issues 🐛

### 🔥 CRITICAL BUGS (Broken Recently - Fix ASAP)

#### Frontend Dialog Issues
1. ❌ **lib-decrypt-dialog.js - Missing close button** (Image #2)
   - Password dialog has no X close button in top-right
   - Only shows a square/checkbox in top-left
   - File: `frontend/src/components/dialog/lib-decrypt-dialog.js`
   - **Priority**: HIGH - User can't close dialog except by unlocking

2. ❌ **share-dialog.js - Internal Link tab stuck loading** (Image #4)
   - Share dialog "Internal Link" tab shows infinite loading spinner
   - Happens on encrypted libraries (test0033.docx)
   - File: `frontend/src/components/dialog/share-dialog.js`
   - **Priority**: HIGH - Sharing doesn't work

3. ⚠️ **Encrypted libraries should NOT allow sharing** (Image #6 vs #4)
   - Share dialog should be disabled for user-encrypted libraries
   - Show message: "Move files to non-encrypted library to share"
   - Only allow sharing on libraries without custom password encryption
   - **Priority**: MEDIUM - Business logic issue

#### File Operations Broken
4. ❌ **Move file returns 405 Method Not Allowed** (Image #7, #8)
   - Move dialog appears correctly
   - Submit triggers `async-batch-move-item` endpoint
   - Returns HTTP 405 - Backend not implemented
   - File: Backend handler missing
   - **Priority**: HIGH - Core file operation

5. ❌ **Copy file returns 405** (mentioned, not shown)
   - Same issue as move - backend not implemented
   - **Priority**: HIGH - Core file operation

#### File Viewing Not Implemented
6. ⚠️ **PDF viewer not working**
   - Users can't preview PDF files
   - Falls back to download
   - **Priority**: MEDIUM - UX issue

7. ⚠️ **Image viewer not working**
   - Users can't preview images (PNG, JPG, etc.)
   - Falls back to download
   - **Priority**: MEDIUM - UX issue

8. ⚠️ **Thumbnails not implemented**
    - No image thumbnails in file list
    - Grid view has no previews
    - **Priority**: MEDIUM - Visual polish

9. ⚠️ **User avatars not implemented**
    - No profile pictures for users
    - Generic icon shown
    - **Priority**: LOW - Visual polish

---

### Critical (Blocks Production Use)
- ❌ **OIDC/OAuth not implemented** - Only dev token authentication works
- ❌ **Sharing backend stubbed** - Users can't share files/folders (UI exists)
- ❌ **File operations incomplete** - Create/delete/rename/copy/move partially stubbed

### High (Affects Web Users)
- ⚠️ **79/158 modal dialogs broken** - reactstrap Modal rendering issue
- ⚠️ **Groups backend stubbed** - Can't create/manage groups (UI exists)
- ⚠️ **File tags backend stubbed** - Can't organize with tags (UI exists)
- ⚠️ **Search incomplete** - Basic search exists but needs completion

### Medium (UI/UX Issues)
- ⚠️ Some file type icons return 404 (need icon audit)
- ⚠️ OnlyOffice toolbar sometimes greyed out (config needs tuning)
- ⚠️ Admin features mostly stubbed (org admin, system admin)

### Low (Future Enhancements)
- Multi-factor authentication not implemented
- Activity logs/notifications stubbed
- AI search not implemented
- SeaTable integration not started
- Wiki features partially stubbed

### ✅ What's Working Well (Keep Frozen)
- Sync protocol: 100% desktop client compatible (14/14 endpoints) 🔒 FROZEN
- Encrypted libraries: PBKDF2 + Argon2id working perfectly 🔒 FROZEN
- File upload: Multipart upload, progress tracking ✅
- File download: Web downloads via token system 🔒 FROZEN (2026-01-20)
- Library management: Create, delete, rename, star, encrypt ✅
- File locking: Lock/unlock working ✅
- Starred files: Star/unstar files and libraries ✅
- Version history: Backend + UI working ✅
- OnlyOffice: Document editing functional (needs config tuning) ✅

---

## Frozen/Stable Components 🔒

### ⚠️ CRITICAL: Sync Code FROZEN (2026-01-19)
**User directive**: DO NOT MODIFY sync code without explicit approval first
**Reason**: Sync protocol is working perfectly with desktop clients
**Impact**: Any changes could break desktop/mobile client sync

**If sync code needs changes**: Ask user for approval before making changes

---

**DO NOT MODIFY these without explicit user request or desktop client breakage:**

### Code Files - Sync Protocol
- `internal/crypto/crypto.go` - PBKDF2 implementation verified against stock Seafile
- `internal/api/sync.go` lines 949-952 - fs-id-list format (JSON array)
- `internal/api/sync.go` lines 125-130 - commit object format (no `no_local_history`)
- `internal/api/sync.go` lines 1405-1492 - check-fs endpoint with FS ID mapping (CRITICAL for sync)
- `internal/api/v2/encryption.go` - set-password/change-password endpoints

### Code Files - Web Downloads (FROZEN 2026-01-20)
- `internal/api/seafhttp.go:1253-1317` - `findEntryInDir()` function (JSON parsing for file lookup)
- `internal/api/seafhttp.go:1034-1189` - `getFileFromBlocks()` function (block-based file retrieval)
- `internal/api/seafhttp.go:963-1030` - `HandleDownload()` function (download token validation)

### Protocol Behaviors
- fs-id-list returns JSON array (NOT newline-separated text)
- Commit objects OMIT `no_local_history` field
- `encrypted` field type: integer in download-info, string in commits
- `is_corrupted` field type: integer 0 (NOT boolean false)
- `Seafile-Repo-Token` header for `/seafhttp/` authentication
- pack-fs binary format: 40-byte ID + 4-byte size (BE) + zlib-compressed JSON

### Documentation
- `docs/SEAFILE-SYNC-PROTOCOL-RFC.md` - Formal specification with test vectors
- `docs/ENCRYPTION.md` - Encryption implementation guide

**Why frozen?**
- Desktop client sync tested and working
- Protocol comparison verified against stock Seafile
- Test vectors generated and verified
- Breaking these = breaking all desktop/mobile clients

---

## Context for Next Session 📝

### 🎯 Project Goal & Business Context
**Mission**: Build complete Seafile replacement ready for production use
**Target Users**: Global cloud storage users, especially those needing China access
**Deployment Plan**: Run parallel to Seafile, migrate new users to SesameFS
**Timeline**: ASAP but thorough - "want it soon, do it right"
**Success Metric**: Can objectively replace Seafile in production

### 📊 Current State Summary
- **Sync Protocol**: 100% working, desktop clients fully compatible 🔒 FROZEN
- **Backend API**: ~60% implemented, 30% stubbed, 10% not started
- **Frontend UI**: ~60% functional, 79/158 dialogs broken (modal issue)
- **Production Ready**: NO - missing OIDC, sharing backend, file operations
- **Test Coverage**: 25% (11 API test files)

### 🚀 Strategic Direction
**Approach**: Frontend-driven development
- Let frontend dictate backend priorities (many features have UI but no backend)
- Complete missing backend endpoints as highest priority
- Fix frontend issues in parallel
- Focus on production readiness: OIDC, monitoring, error handling

### ⭐ Next Immediate Steps (Start Here)
1. **Implement Sharing System Backend** (3-5 days)
   - Share to users/groups
   - Share links (view/edit/upload)
   - Permissions management
   - UI already complete, just needs backend

2. **Complete File Operations Backend** (2-3 days)
   - Create/delete/rename/copy/move
   - UI already working, backend partially stubbed

3. **Fix High-Priority Modal Dialogs** (2-3 days)
   - Group management dialogs
   - Share link dialogs
   - Invitation dialogs
   - ~10-15 files to fix first

4. **Implement Groups Backend** (2-3 days)
   - Create/manage groups
   - Add/remove members
   - Group permissions
   - UI already complete

### Critical Facts to Remember

**Protocol Development**:
- Stock Seafile (app.nihaoconsult.com) is ALWAYS the reference for sync protocol
- Use `./run-sync-comparison.sh` to verify protocol changes before considering them done
- Use `./run-real-client-sync.sh` to test with actual seaf-cli desktop client
- Protocol bugs = broken desktop clients = critical severity

**Authentication**:
- REST API (`/api2/`, `/api/v2.1/`): Use `Authorization: Token {api_token}` header
- Sync protocol (`/seafhttp/`): Use `Seafile-Repo-Token: {sync_token}` header
- Sync token comes from `GET /api2/repos/{id}/download-info/` response

**Encryption**:
- Magic computation: input = `repo_id + password`
- Random key encryption: input = `password` ONLY (NOT repo_id + password)
- PBKDF2 uses 1000 iterations for key, 10 for IV
- Static salt for enc_version 2: `{0xda, 0x90, 0x45, 0xc3, 0x06, 0xc7, 0xcc, 0x26}`

**Frontend Development**:
- Modal dialogs MUST use plain Bootstrap classes, NOT reactstrap Modal components
- Reason: ModalPortal wrapper causes double-portal issue with reactstrap Modal
- See CLAUDE.md for correct modal pattern
- Icons use HiDPI logic: requests 24px or 192px based on screen

**Block Storage**:
- Block ID mapping: SHA-1 (external/client) → SHA-256 (internal/storage)
- Table: `block_id_mappings` (columns: `external_id`, `internal_id`)
- Desktop clients use SHA-1, server stores SHA-256

### Files Modified This Session
- `internal/api/seafhttp.go:1253-1317`, `1034-1189` - Fixed `findEntryInDir()` JSON parsing bug
- `CURRENT_WORK.md` - Trimmed to focus on current priorities

---

## Quick Commands Reference

```bash
# Protocol verification (MUST PASS before freezing protocol changes)
cd docker/seafile-cli-debug
./run-sync-comparison.sh          # API-level protocol comparison
./run-real-client-sync.sh          # Real desktop client sync test

# Generate test vectors (for RFC documentation)
cd docker/seafile-cli-debug
docker run --rm -v "$(pwd)/scripts:/scripts:ro" \
  cool-storage-api-seafile-cli python3 /scripts/generate_test_vectors.py

# Run server
docker-compose up -d sesamefs

# Rebuild after code changes
docker-compose build sesamefs && docker-compose up -d sesamefs

# Frontend development
cd frontend
npm install
npm start  # Runs on port 3001

# Frontend Docker rebuild (if changes don't appear)
docker-compose stop frontend && docker-compose rm -f frontend
docker rmi cool-storage-api-frontend
docker-compose build --no-cache frontend
docker-compose up -d frontend

# Run tests
go test ./...
go test ./... -coverprofile=coverage.out
```

---

## Session Handoff Checklist

Before ending a session, update this file with:
- [ ] What was completed (move from "What's Next" to "What Was Just Completed")
- [ ] What's next (update priorities)
- [ ] New known issues discovered
- [ ] Files modified
- [ ] Components frozen (if any)
- [ ] Update last updated timestamp and session ID
