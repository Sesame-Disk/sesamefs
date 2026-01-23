# Changelog - SesameFS

Session-by-session development history for SesameFS.

**Format**: Each session includes completion date, major features, files changed.

**Note**: For detailed git history, use `git log --oneline --graph`. This file tracks high-level session summaries.

---

## 2026-01-23 - Frontend Modal Close Icon Fix, Browser Cache Debugging

**Session Type**: Debugging, Documentation
**Worked By**: Claude Sonnet 4.5

### Completed
- ✅ **lib-decrypt-dialog Close Button Fixed**
  - Issue: Close button showed square □ instead of × icon
  - Root Cause: Browser cache serving old JavaScript despite correct source code
  - Solution: Code was correct, created standalone test page to verify
  - Test Page Created: `frontend/public/test-decrypt-modal.html`
  - Files: `frontend/src/components/dialog/lib-decrypt-dialog.js:72-74`

- ✅ **Frontend Testing Methodology Documented**
  - Created comprehensive browser cache debugging guide
  - Documented standalone HTML test page approach for frontend fixes
  - Added cache clearing methods and verification techniques
  - Files: `CLAUDE.md`, `CURRENT_WORK.md`

- ✅ **Frozen Working Frontend Components**
  - Documented components that are working and should not be modified without approval
  - Library list view, starred items, file download functionality
  - Files: `CURRENT_WORK.md`

- ✅ **Audited and Documented Pending Issues**
  - Discovered critical regression: Share modal broken with 500 error (was working 2026-01-22)
  - Documented file viewer regression (downloads instead of preview)
  - Documented missing library advanced settings (History, API Token, Auto Deletion)
  - Files: `CURRENT_WORK.md`

### Files Modified
**Frontend**:
- `frontend/src/components/dialog/lib-decrypt-dialog.js` - Close button verified
- `frontend/public/test-decrypt-modal.html` - **NEW** Standalone test page

**Documentation**:
- `CURRENT_WORK.md` - Updated with debugging guide, frozen components, new issues
- `CLAUDE.md` - Added "Browser Cache Issues & Testing Methodology" section

---

## 2026-01-22 - Cassandra SASI Search, Encrypted Library Fix, Build Optimizations

**Session Type**: Major Feature, Bug Fixes, Infrastructure
**Worked By**: Claude Sonnet 4.5

### Completed

#### Cassandra SASI Search Implementation ⭐ MAJOR
- ✅ Full search backend with Cassandra SASI indexes
- ✅ Added SASI indexes to `fs_objects.obj_name` and `libraries.name` for case-insensitive search
- ✅ Implemented `internal/api/v2/search.go` with full search functionality
- ✅ Registered routes in `internal/api/server.go`
- **Features**:
  - Search libraries by name: `GET /api/v2.1/search/?q=query&type=repo`
  - Search files/folders: `GET /api/v2.1/search/?q=query&repo_id=xxx&type=file`
  - Case-insensitive CONTAINS matching
  - Filter by repo_id, type (file/dir/repo)
- **Zero new dependencies** - Uses existing Cassandra
- **Performance**: Fast for most queries, may need pagination for very large datasets

#### Encrypted Library Sharing Fix 🐛 CRITICAL BUG FIX
- ✅ Frontend warning now displays correctly
- **Root Cause**: Backend returned `encrypted: true` (boolean), frontend expected `encrypted: 1` (integer)
- **Fix**: Changed `V21Library.Encrypted` from `bool` to `int` in all library endpoints
- **Files**: `internal/api/v2/libraries.go` (GetLibrary, ListLibraries, ListLibrariesV21)
- **Result**: Share dialog now shows "Cannot share encrypted library" warning instead of infinite loading spinner

#### Permission Middleware System ⭐ MAJOR
- ✅ Complete permission middleware implementation
- Created `internal/middleware/permissions.go` - Full permission checking system
- Organization-level roles (admin, user, readonly, guest)
- Library-level permissions (owner, rw, r)
- Group-level roles (owner, admin, member)
- Hierarchical permission model with proper inheritance
- ✅ Audit logging system (`internal/middleware/audit.go`)
- ✅ Complete documentation (`internal/middleware/README.md`)
- ✅ Ready for integration - Next step: Apply to routes in server.go

#### Build System Fixes
- ✅ **Removed Elasticsearch Dependency**
  - Removed Elasticsearch service from `docker-compose.yaml` (saves 2GB RAM)
  - Removed `ELASTICSEARCH_URL` environment variable
  - Cleaned up go.mod with `go mod tidy`
- ✅ **Frontend Build Memory Fix**
  - Added `NODE_OPTIONS=--max_old_space_size=4096` to `frontend/Dockerfile`
  - Gives Node.js 4GB memory instead of default ~1.5GB

#### Frontend UI Fixes
- ✅ Encrypted library sharing policy - Frontend enforcement complete
- ✅ Backend build fixes - Search module import errors corrected

#### OnlyOffice Integration Frozen
- ✅ STATUS: OnlyOffice document editing now 🔒 FROZEN
- ✅ Configuration simplified, toolbar working correctly

### Files Modified

**Database**:
- `internal/db/db.go` - Added SASI search indexes for fs_objects and libraries

**Backend**:
- `internal/api/v2/search.go` - Complete rewrite with full search implementation
- `internal/api/v2/libraries.go` - Fixed encrypted field type (bool → int)
- `internal/api/server.go` - Registered search routes
- `internal/middleware/permissions.go` - **NEW** Permission middleware
- `internal/middleware/audit.go` - **NEW** Audit logging
- `internal/middleware/README.md` - **NEW** Middleware documentation
- `go.mod` / `go.sum` - Cleaned up after Elasticsearch removal

**Docker & Build**:
- `docker-compose.yaml` - Removed Elasticsearch service
- `frontend/Dockerfile` - Increased Node.js memory to 4GB

**Frontend**:
- `frontend/src/components/dialog/internal-link.js` - Encrypted library warning
- `frontend/src/components/dialog/share-dialog.js` - Pass repoEncrypted prop
- `frontend/src/components/dialog/lib-decrypt-dialog.js` - Bootstrap 4 close button
- `frontend/public/static/img/lock.svg` - **NEW** Lock icon

**Documentation**:
- `CURRENT_WORK.md` - Updated with search, encrypted library fix, build optimizations

---

## 2026-01-22 Earlier - Sharing System, Groups, File Tags

**Session Type**: Major Features
**Worked By**: Claude Sonnet 4.5

### Completed
- ✅ Sharing system backend - Share to users/groups, share links, permissions
- ✅ Groups management - Complete CRUD for groups and members
- ✅ File tags - Repository tags and file tagging

---

## 2026-01-19 - Frontend Feature Audit, Duplicate File Sync Bug Fix

**Session Type**: Bug Fix, Audit
**Summary**: Fixed duplicate file sync bug, comprehensive frontend feature audit

See git log for details.

---

## 2026-01-18 - "View on Cloud" Feature, Desktop Re-sync Fix

**Session Type**: Feature, Bug Fix
**Summary**: Implemented "View on Cloud" desktop client feature, fixed desktop re-sync issues

See git log for details.

---

## 2026-01-17 - Comprehensive Sync Protocol Test Framework

**Session Type**: Testing Infrastructure
**Summary**: Created comprehensive sync protocol test framework with 7 test scenarios

**Documentation**: See `docker/seafile-cli-debug/COMPREHENSIVE_TESTING.md`

See git log for details.

---

## 2026-01-16 - Session Continuity System, Sync Protocol Fixes

**Session Type**: Infrastructure, Bug Fixes
**Summary**: Created session continuity documentation system, multiple sync protocol compatibility fixes

**Documentation**: See `docs/IMPLEMENTATION_STATUS.md`

### Sync Protocol Compatibility Fixes
- Fixed `is_corrupted` field type (boolean → integer 0)
- Fixed commit object format (removed unconditional `no_local_history`)
- Fixed FSEntry struct field order (alphabetical for correct fs_id hash)
- Fixed check-fs endpoint (JSON array input/output)
- Fixed check-blocks endpoint (JSON array input/output)

**Verification**: All endpoints now match reference Seafile server (app.nihaoconsult.com)

See git log for details.

---

## 2026-01-14 - Major Sync Protocol Compatibility Fixes

**Session Type**: Bug Fixes
**Summary**: Multiple critical sync protocol fixes for desktop client compatibility

See git log and CURRENT_WORK.md archives for details.

---

## 2026-01-13 - PBKDF2 Key Derivation Fix

**Session Type**: Critical Bug Fix
**Summary**: Fixed PBKDF2 encryption - Seafile uses TWO separate PBKDF2 calls

**Critical Fix**: Different input for magic vs random key encryption
- Magic: Uses `repo_id + password`
- Random key: Uses `password` ONLY

See git log for details.

---

## 2026-01-09 - Encrypted Library File Content Encryption

**Session Type**: Major Feature
**Summary**: Full file content encryption for encrypted libraries

**Features**:
- Creating encrypted libraries with strong password protection
- Verifying passwords (set-password endpoint)
- Changing passwords (change-password endpoint)
- File content encryption/decryption for all upload paths
- SHA-1→SHA-256 block ID mapping for Seafile client compatibility

See git log for details.

---

## 2026-01-08 - Encrypted Library Password Management

**Session Type**: Major Feature
**Summary**: Full encrypted library password management with strong security

**Implementation**:
- Created `internal/crypto/crypto.go` with dual-mode encryption
- Argon2id (strong) for web/API clients
- PBKDF2 (1000 iterations) for Seafile desktop/mobile compatibility
- Added set-password and change-password endpoints
- Database columns: `salt`, `magic_strong`, `random_key_strong`
- Fixed modal dialogs: `lib-decrypt-dialog.js`, `change-repo-password-dialog.js`

**Security**: 300× slower brute-force compared to Seafile's default PBKDF2

**Files**: `internal/crypto/crypto.go`, `internal/api/v2/encryption.go`, `internal/api/v2/libraries.go`

**Documentation**: See `docs/ENCRYPTION.md`

### Library Starring Fix
- Fixed starred libraries not persisting after page refresh
- Root cause: Invalid Cassandra query filtering
- Fix: Query all starred items, filter by `path="/"` in Go code
- File: `internal/api/v2/libraries.go:678-693`

### OnlyOffice Simplified Config
- Fixed OnlyOffice documents opening in view-only mode
- Simplified config to match Seahub's minimal approach
- Files: `internal/api/v2/onlyoffice.go`, `internal/config/config.go`

### Multi-host Frontend Support
- Empty `serviceURL` config uses `window.location.origin` automatically
- File: `frontend/public/index.html`

### Modal Dialog Fixes
- Fixed dialogs to use plain Bootstrap modal classes
- `rename-dialog.js`, `rename-dirent.js`

See git log for details.

---

## Earlier Sessions

For sessions before 2026-01-08, see git log:

```bash
git log --oneline --graph --all
```

Key early milestones:
- Seafile sync protocol implementation (2025-12-xx)
- Cassandra database schema (2025-12-xx)
- S3 storage backend (2025-12-xx)
- React frontend integration (2025-12-xx)
- Docker compose setup (2025-12-xx)
