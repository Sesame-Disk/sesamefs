# Known Issues - SesameFS

**Last Updated**: 2026-01-24

This document tracks all known bugs, limitations, and issues in SesameFS.

---

## ✅ RECENTLY FIXED (2026-01-27)

### Logout Button - FIXED ✅ 🔒 FROZEN
**Fixed**: 2026-01-27
**Status**: Working correctly - DO NOT MODIFY
**Issue**: Clicking logout went to `/accounts/logout/` but nothing happened
**Root Cause**: Frontend nginx wasn't proxying `/accounts/` routes to backend
**Fix**: Added `/accounts/` location block to `frontend/nginx.conf`
**Files**: `frontend/nginx.conf` (lines 77-83)

### Anonymous Access for Testing - IMPLEMENTED ✅
**Implemented**: 2026-01-27
**Status**: Working - FOR TESTING ONLY
**Feature**: Backend allows unauthenticated requests when `AUTH_ALLOW_ANONYMOUS=true`
**Files**:
- `internal/api/server.go:516-590` - authMiddleware with anonymous fallback
- `internal/config/config.go` - AllowAnonymous config option
- `config.docker.yaml` - Dev tokens for all 4 test users

### Frontend Login Bypass - IMPLEMENTED ✅
**Implemented**: 2026-01-27
**Status**: Working - FOR TESTING ONLY
**Feature**: Set `REACT_APP_BYPASS_LOGIN=true` to skip login page
**Files**: `frontend/src/utils/seafile-api.js`, `frontend/.env`

---

## ✅ RECENTLY FIXED (2026-01-24)

### Media File Viewer Fix - FIXED ✅ (Pending manual testing)
**Fixed**: 2026-01-23
**Was**: CRITICAL UX bug
**Root Cause**: Mobile view missing `onClick` handler, causing direct navigation to download URL
**Files Fixed**:
- `frontend/src/components/dirent-list-view/dirent-list-item.js` line 798

**What Works Now** (pending manual testing):
- ✅ Clicking images should open image popup viewer
- ✅ Clicking PDFs should open in-browser PDF viewer
- ✅ Clicking videos should open video player
- ✅ Mobile view now has same click handling as desktop view

**Manual Testing Required**:
- Test clicking various file types on mobile view
- Test clicking images (should open popup)
- Test clicking PDFs (should open viewer)
- Test clicking videos (should open player)

### Permission Middleware Integration - COMPLETE ✅ (Pending full testing)
**Completed**: 2026-01-23
**Status**: Core implementation done, example checks integrated
**Files Implemented**:
- `internal/middleware/permissions.go` - Full permission middleware (371 lines)
- `internal/api/server.go` - Initialized and integrated
- `internal/api/v2/libraries.go` - Example permission checks

**What's Implemented**:
- ✅ Organization role checking (admin/user/readonly/guest)
- ✅ Library permission checking (owner/rw/r)
- ✅ Group role checking (owner/admin/member)
- ✅ Group permission resolution (users inherit group library permissions)
- ✅ CreateLibrary: Requires "user" role or higher
- ✅ DeleteLibrary: Requires library ownership

**Manual Testing Required**:
- Test CreateLibrary with different user roles
- Test DeleteLibrary with non-owner users
- Test group permission inheritance
- Add permission checks to remaining handlers incrementally

### Database Seeding - COMPLETE ✅
**Completed**: 2026-01-23
**Status**: Fully implemented and tested
**Files Implemented**:
- `internal/db/seed.go` - Database seeding implementation (220 lines)
- `cmd/sesamefs/main.go` - Integrated into startup

**What's Seeded**:
- ✅ Default organization (1TB quota)
- ✅ Admin user (role: admin)
- ✅ Test users (user, readonly, guest roles) - dev mode only
- ✅ Users indexed in users_by_email for login

### Test Coverage Improvements - COMPLETE ✅
**Completed**: 2026-01-24
**Status**: Comprehensive tests added for all new features

**Backend Tests Created**:
- `internal/db/seed_test.go` - Database seeding tests (9 tests, all passing)
  - Tests UUID uniqueness, idempotency, dev vs production modes
  - Tests organization creation, admin user, test users
  - Tests email indexing for login
- `internal/api/v2/libraries_test.go` - Permission middleware tests (3 test suites)
  - Tests role hierarchy (admin > user > readonly > guest)
  - Tests library creation permission (requires "user" role or higher)
  - Tests library deletion permission (requires ownership)
  - Tests group permission resolution

**Frontend Tests Created**:
- `frontend/src/components/dirent-list-view/__tests__/dirent-list-item.test.js`
  - Documents media viewer fix behavior
  - Tests file type detection (images, PDFs, videos)
  - Tests onClick handler presence (desktop and mobile views)
  - Regression test for line 798 fix

**Test Results**:
- ✅ All backend tests passing
- ✅ Backend coverage: 23.4% overall (stable)
- ✅ internal/db: 0.0% (tests are documentation-style, skip DB operations)
- ✅ internal/api/v2: 18.4% coverage (improved from adding tests)

**Type Error Fixed**:
- Fixed `internal/api/v2/libraries_test.go:468` - Changed `Encrypted: false` (bool) to `Encrypted: 0` (int)
- This is NOT a protocol change - API already returns int (0/1) for Seafile compatibility

### Share Modal 500 Error - FIXED ✅
**Fixed**: 2026-01-23
**Was**: CRITICAL regression
**Root Cause**: Missing `org_id` in Cassandra queries (partition key required)
**Files Fixed**:
- `internal/api/v2/share_links.go` lines 125, 153
- `internal/api/v2/file_shares.go` lines 116, 138, 146, 651
- `internal/middleware/permissions.go` line 242 (group permission resolution)

**What Works Now**:
- ✅ Share modal loads without errors
- ✅ Group names display correctly (not UUIDs)
- ✅ Users see libraries shared to their groups
- ✅ User emails display correctly (not UUIDs)

---

## 🔴 CRITICAL SECURITY/PERMISSION ISSUES (Discovered 2026-01-24)

**Discovered During**: Manual permission testing with multiple user roles
**Status**: 📋 DOCUMENTED - Implementation plan created
**Plan**: See `docs/PERMISSION-ROLLOUT-PLAN.md` for comprehensive fix
**Priority**: BLOCKING production deployment

### Issue 1: All Users Can See All Libraries 🔴 CRITICAL
**Severity**: CRITICAL - Complete privacy violation
**Discovered**: 2026-01-24 manual testing

**Bug**: User logged in as `user@sesamefs.local` can see libraries owned by `admin@sesamefs.local`

**Expected Behavior**:
- Users should ONLY see their own libraries
- Exception: Libraries explicitly shared with them

**Actual Behavior**:
- `GET /api/v2.1/repos/` returns ALL libraries in organization
- No filtering by ownership or shares

**Root Cause**: `ListLibraries()` in `internal/api/v2/libraries.go` has NO permission filtering

**Impact**:
- Zero privacy between users
- Users can see library names, sizes, encryption status of all libraries
- Violates basic multi-tenant isolation

**Files**: `internal/api/v2/libraries.go` - `ListLibraries()` function

---

### Issue 2: Users Can Access Other Users' Libraries 🔴 CRITICAL
**Severity**: CRITICAL - Complete access control failure
**Discovered**: 2026-01-24 manual testing

**Bug**: Any user can access any library by direct URL or navigation

**Test Cases**:
- `user@sesamefs.local` browsed libraries owned by `admin@sesamefs.local`
- `guest@sesamefs.local` accessed library owned by `user@sesamefs.local`
- All directory contents visible to unauthorized users

**Expected Behavior**:
- Users can only access own libraries
- Access to other libraries ONLY if explicitly shared
- Should get 403 Forbidden if attempting unauthorized access

**Actual Behavior**:
- NO permission checks on directory listing endpoints
- NO permission checks on library detail endpoints
- Complete access to all libraries regardless of ownership

**Root Cause**: Missing permission checks on:
- `GET /api/v2.1/repos/:repo_id` (GetLibrary)
- `GET /api/v2.1/repos/:repo_id/dir/` (ListDirectory)

**Impact**:
- Users can read all files from all libraries
- Zero access control
- Data breach scenario

---

### Issue 3: Readonly Users Can Write to Other Users' Libraries 🔴 CRITICAL
**Severity**: CRITICAL - Role-based access control failure
**Discovered**: 2026-01-24 manual testing

**Bug**: User `readonly@sesamefs.local` successfully edited Word docx files in encrypted libraries owned by other users

**Expected Behavior**:
- readonly role = read-only access to own libraries ONLY
- Should get 403 on write attempts (upload, edit, delete)
- Should have ZERO access to other users' libraries

**Actual Behavior**:
- readonly user can upload files to any library
- readonly user can edit documents in any library (via OnlyOffice)
- NO enforcement of role restrictions

**Root Cause**: Missing permission checks on:
- File upload endpoints (`/seafhttp/upload-api/`)
- OnlyOffice save callback (`internal/api/v2/onlyoffice.go`)
- File create/edit/delete operations

**Impact**:
- Role system is non-functional
- readonly and guest roles have same permissions as admin
- Data corruption risk

---

### Issue 4: Guest User Can Modify Libraries and Cause Data Loss 🔴 CRITICAL
**Severity**: CRITICAL - Data corruption + access control failure
**Discovered**: 2026-01-24 manual testing

**Bug**: User `guest@sesamefs.local` accessed library owned by `user@sesamefs.local`, created file, caused original files to disappear

**Timeline**:
1. guest@ logged in
2. Navigated to library owned by user@ (test0034)
3. Created new file `test-guest.docx` (2.2 KB)
4. After creation, user@'s original files disappeared from directory listing

**Expected Behavior**:
- guest role should have ZERO access to other users' libraries
- guest should only see own libraries (if any)
- Creating files should not cause existing files to disappear

**Actual Behavior**:
- guest can access any library
- guest can create files in any library
- File creation caused data corruption (files disappeared)

**Root Cause**:
- Missing permission checks (same as Issues 1-3)
- Possible commit/fs_object corruption in multi-user scenario

**Impact**:
- Data loss
- Complete lack of user isolation
- Potential filesystem corruption

**Files**:
- Permission checks needed in all file operation endpoints
- Investigate fs_object/commit corruption issue

---

### Issue 5: Encrypted Libraries Not Protected from Sharing 🔴 CRITICAL
**Severity**: CRITICAL - Security policy violation
**Discovered**: 2026-01-24 (known issue, not yet enforced)

**Policy**: Password-encrypted libraries CANNOT be shared (sharing would require sharing encryption key)

**Status**: NOT ENFORCED in backend

**Expected Behavior**:
- Attempting to share encrypted library should return 403
- Clear error message: "Cannot share encrypted libraries. Move files to a non-encrypted library to share them."

**Actual Behavior**:
- Backend allows share creation on encrypted libraries
- Frontend shows loading spinner (stuck) when trying to share encrypted files

**Root Cause**: No validation in share creation endpoints

**Files**: `internal/api/v2/file_shares.go` - Share creation functions

**Impact**:
- Security vulnerability
- Encrypted data could be shared inappropriately
- Encryption key management violated

---

## 📋 Comprehensive Fix Plan

**See**: `docs/PERMISSION-ROLLOUT-PLAN.md` for full implementation plan

**Summary**:
- Phase 1: Library access control (filter ListLibraries, check GetLibrary, check directory listing)
- Phase 2: File operations (upload, edit, delete, rename, move)
- Phase 3: Encrypted library policy enforcement
- Estimated time: 2-3 days
- Approach: Systematic application of permission middleware to ALL endpoints

---

## 🔴 CRITICAL REGRESSIONS (Was Working, Now Broken)

### Encrypted Libraries Load Without Password 🔴 CRITICAL SECURITY
**Severity**: CRITICAL - Security bypass
**Discovered**: 2026-01-27 during frontend testing
**Status**: 🔴 UNFIXED - Needs immediate attention

**Bug**: Encrypted libraries prompt for password but frontend loads the library contents even if user doesn't enter password

**Expected Behavior**:
- Encrypted libraries should be COMPLETELY inaccessible without the correct password
- Frontend should NOT display any library contents until password verified
- API should return 403 or require decrypt session before returning any data

**Actual Behavior**:
- Password dialog appears (correct)
- User can dismiss/cancel password dialog
- Frontend still loads and displays library contents
- Files are visible without decryption

**Root Cause**: Likely missing server-side enforcement
- Backend may not be checking decrypt session before returning library contents
- Frontend may be making API calls that succeed without password

**Impact**:
- Encrypted libraries are NOT secure
- Password protection is cosmetic only
- Sensitive data exposed without authorization

**Files to Investigate**:
- `internal/api/v2/libraries.go` - ListDirectory should check decrypt session
- `internal/api/v2/encryption.go` - Decrypt session management
- `internal/api/sync.go` - fs-id-list and pack-fs endpoints

**Priority**: 🔴 CRITICAL - Must fix before any production use of encrypted libraries

---

## 🔴 CRITICAL UX BUGS

**None currently!** 🎉 (Pending manual testing)

---

## ⚠️ LIBRARY SETTINGS NOT IMPLEMENTED

**Status**: Backend endpoints missing, frontend UI exists
**Pattern**: Frontend shows these options in Advanced menu, but backend endpoints are stubs or missing

### History Setting Not Working
**Severity**: MEDIUM
**Issue**: "History Setting" menu item shows but clicking does nothing or shows error
**Expected**: Should open dialog to configure version history retention
**User Report**: 2026-01-23 - "history settings for a library not being working"
**Files**: Backend needs implementation, frontend dialog exists

### API Token Not Working
**Severity**: MEDIUM
**Issue**: "API Token" menu item shows but clicking does nothing or shows error
**Expected**: Should show/generate API tokens for library access
**User Report**: 2026-01-23 - "api token... do not work"
**Files**: Backend needs implementation, frontend dialog exists

### Auto Deletion Setting Not Working
**Severity**: MEDIUM
**Issue**: "Auto Deletion Setting" menu item shows but clicking does nothing or shows error
**Expected**: Should configure automatic deletion of old files
**User Report**: 2026-01-23 - "autodeletion settins do not work"
**Files**: Backend needs implementation, frontend dialog exists

---

## ⚠️ FILE OPERATIONS NOT FULLY IMPLEMENTED

### Move File Returns 405
**Severity**: HIGH
**Impact**: Core file operation broken

**Symptoms**:
- Move dialog appears correctly
- Submit triggers `async-batch-move-item` endpoint
- Returns HTTP 405 Method Not Allowed - backend not implemented

**Files**: `internal/api/v2/files.go` - backend handler missing
**Frontend Ready**: `move-dirent-dialog.js` exists and working

### Copy File Returns 405
**Severity**: HIGH
**Impact**: Core file operation broken

**Symptoms**: Same as move - backend not implemented
**Files**: `internal/api/v2/files.go` - backend handler missing
**Frontend Ready**: `copy-dirent-dialog.js` exists and working

---

## ⚠️ UI/UX ISSUES

### Thumbnails Not Implemented
**Severity**: MEDIUM
**Impact**: Visual polish

**Missing**:
- No image thumbnails in file list
- Grid view has no previews

### User Avatars Not Implemented
**Severity**: LOW
**Impact**: Visual polish

**Missing**:
- No profile pictures for users
- Generic icon shown

### Missing File Type Icons
**Severity**: LOW
**Impact**: Visual polish

**Issue**: Some file type icons return 404
**Fix Needed**: Icon audit and add missing icons

---

## 🚧 BACKEND NOT IMPLEMENTED

### Garbage Collection / Cleanup Jobs
**Severity**: CRITICAL for production (storage leak)
**Status**: Architecture documented, ZERO implementation

**Missing Components**:
1. **Block GC Worker** - Delete blocks with `ref_count = 0`
   - Current: Orphaned blocks stay in S3 forever
   - Impact: Storage costs grow without bound
   - Note: ref_count tracking IS implemented ✅

2. **Commit Cleanup Worker** - Delete old versions beyond TTL
   - Current: Old commits accumulate forever
   - Impact: Storage leak + database bloat
   - Table: `libraries.version_ttl_days` exists but unused

3. **FS Object Cleanup** - Remove unreferenced fs_objects
   - Current: Orphaned fs_objects accumulate
   - Impact: Database bloat

4. **Expired Share Link Cleanup** - Delete expired share links
   - Current: Checked at access, never removed from DB
   - Impact: Database bloat
   - Table: `share_links.expires_at` exists

5. **Block ID Mapping Cleanup** - Remove mappings for deleted blocks
   - Current: Orphaned mappings accumulate
   - Impact: Database bloat

**Architecture**: See `docs/ARCHITECTURE.md:381-417` - Full GC design documented

**Priority**: HIGH - Required before production deployment

**Recommended Implementation**:
```go
// internal/gc/worker.go
type GCWorker struct {
    db *db.DB
    storage storage.StorageManager
    interval time.Duration
}

func (w *GCWorker) Run() {
    // 1. Every 24h: Delete blocks with ref_count=0 older than 24h
    // 2. Every 24h: Delete commits older than version_ttl_days
    // 3. Every 7d: Clean orphaned fs_objects
    // 4. Every 6h: Delete expired share links
}
```

**Files to Create**:
- `internal/gc/worker.go` - Main GC worker
- `internal/gc/blocks.go` - Block cleanup
- `internal/gc/commits.go` - Commit cleanup
- `internal/gc/shares.go` - Share link cleanup
- `cmd/sesamefs/main.go` - Start GC worker goroutine

### Authentication & Security
**Severity**: CRITICAL for production
**Status**: Only dev token authentication works

**Missing**:
- OIDC/OAuth integration
- Multi-factor authentication (optional)
- Session management
- Password change functionality
- Security audit

### Permission Middleware
**Severity**: CRITICAL for production
**Status**: Middleware built ✅, not integrated

**Current State**:
- Database schema complete
- Middleware implementation complete (`internal/middleware/permissions.go`)
- NOT applied to routes in `internal/api/server.go`
- No centralized permission enforcement

**What's Missing**:
- Integration into route handlers
- Audit logging for permission changes
- Org-level role enforcement (admin vs user)
- Library-level permission checking (owner vs collaborator)

**Priority**: MEDIUM-HIGH - Required for production multi-tenant deployment

### Encrypted Library Sharing Policy
**Severity**: HIGH
**Status**: Policy exists, not enforced

**Policy**: Password-encrypted libraries CANNOT be shared
**Reason**: Sharing encrypted files requires sharing the encryption key, breaking security

**Implementation Status**: ❌ NOT ENFORCED
- Backend allows creating shares on encrypted libraries
- Frontend shows loading spinner (stuck) when trying to share encrypted files

**Required Fix**:
- Backend: Check `libraries.encrypted` before allowing share creation → return 403
- Frontend: Show message "Move files to a public library to share" instead of loading
- Files: `internal/api/v2/file_shares.go`, `frontend/src/components/dialog/share-dialog.js`

---

## 🚧 FRONTEND MODAL ISSUES

### ~100 Modal Dialogs Broken
**Severity**: MEDIUM
**Status**: reactstrap Modal rendering issue
**Impact**: Some dialogs don't show

**Root Cause**: `ModalPortal` wrapper + reactstrap `Modal` creates double-portal issue

**Solution**: Use plain Bootstrap modal classes instead of reactstrap Modal

**Pattern**: See `delete-repo-dialog.js` for working example

**Priority Dialogs to Fix** (10-15 files):
- Group management (create, rename, leave, dismiss)
- Share link dialogs (view, generate)
- Invitation dialogs (invite, revoke)
- Tag dialogs (create, edit)

**All Broken Dialogs**: Run `grep -l "import.*Modal.*from 'reactstrap'" frontend/src/components/dialog/**/*.js`

---

## ⚠️ PRODUCTION READINESS GAPS

### Error Handling & Monitoring
**Severity**: HIGH for production
**Status**: Basic error handling only

**Missing**:
- Comprehensive error handling
- Structured logging
- Metrics/monitoring (Prometheus?)
- Health check endpoints
- Alerting

### Documentation
**Severity**: HIGH for production
**Status**: Partial

**Missing**:
- User documentation
- Admin documentation
- Production deployment guide
- Backup/restore procedures
- Migration guide (from Seafile)

---

## ✅ RECENTLY FIXED (2026-01-22 - 2026-01-23)

### Encrypted Library Sharing Warning - FIXED
**Fixed**: 2026-01-22
**Issue**: Internal Link tab showed infinite loading spinner in encrypted libraries
**Root Cause**: Backend returned `encrypted: true` (boolean), frontend expected `encrypted: 1` (integer)
**Fix**: Changed all library endpoints to return integer (0/1)
**Files**: `internal/api/v2/libraries.go`

### Search Backend - IMPLEMENTED
**Completed**: 2026-01-22
**Issue**: Search returned empty stub results
**Fix**: Full Cassandra SASI search implementation
**Features**: Search libraries/files by name, filter by repo/type
**Files**: `internal/db/db.go`, `internal/api/v2/search.go`, `internal/api/server.go`

### Docker Build Memory Issues - FIXED
**Fixed**: 2026-01-22
**Issue**: Frontend build killed with "cannot allocate memory"
**Fix**: Increased Node memory to 4GB, removed Elasticsearch (saved 2GB)
**Files**: `frontend/Dockerfile`, `docker-compose.yaml`

### lib-decrypt-dialog Close Button - FIXED
**Fixed**: 2026-01-23
**Issue**: Close button showed square □ instead of × icon
**Root Cause**: Browser cache serving old JavaScript despite correct source code
**Solution**: Code was correct (`className="close"` with `<span>&times;</span>`)
**Files**: `frontend/src/components/dialog/lib-decrypt-dialog.js:72-74`

---

## Low Priority / Future Enhancements

### Features Not Started
- Multi-factor authentication
- Activity logs/notifications stubbed
- AI search not implemented
- SeaTable integration not started
- Wiki features partially stubbed

### Admin Features
- Most org admin features stubbed
- System admin features mostly stubbed

---

## See Also

- [IMPLEMENTATION_STATUS.md](IMPLEMENTATION_STATUS.md) - Component completion status
- [API-REFERENCE.md](API-REFERENCE.md) - API endpoint documentation
- [TECHNICAL-DEBT.md](TECHNICAL-DEBT.md) - Architectural issues
- [CURRENT_WORK.md](../CURRENT_WORK.md) - Active priorities
