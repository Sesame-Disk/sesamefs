# Known Issues - SesameFS

**Last Updated**: 2026-01-23

This document tracks all known bugs, limitations, and issues in SesameFS.

---

## 🔴 CRITICAL REGRESSIONS (Was Working, Now Broken)

### Share Modal Completely Broken
**Severity**: CRITICAL
**Status**: Broken as of 2026-01-23
**Impact**: Users cannot create/view share links - core collaboration feature down

**Symptoms**:
- Share dialog shows infinite loading spinner, completely unusable
- Error: `GET /api/v2.1/share-links/?repo_id={id}` returns **500 Internal Server Error**

**Was Working**: Yes - sharing system was marked ✅ COMPLETE on 2026-01-22
**Broke When**: Unknown - between last verification and 2026-01-23

**User Report**: 2026-01-23 - "the share modal used to work, but we also had a regression there"

**Files**:
- Backend: `internal/api/v2/share_links.go` - 500 error source
- Frontend: `src/components/dialog/share-dialog.js` - stuck in loading state

**Next Step**: Check backend logs for panic/error causing 500

---

## 🔴 CRITICAL UX BUGS

### Media Files Download Instead of Opening Viewer
**Severity**: HIGH
**Status**: Broken
**Impact**: Major UX regression - users expect inline preview

**Symptoms**:
- Clicking viewable media files (images, videos, PDFs) downloads instead of opening viewer
- Expected: Should open in-browser viewer/preview
- Current: Forces download for all file types

**User Report**: 2026-01-23 - "clicking on file of viewable media should not try to download"

**Related Issues**: Viewers may exist but aren't being triggered

**Files**:
- `src/components/dirent-list-view/dirent-list-item.js` - file click handlers

**Sub-Issues**:
- PDF viewer not working - falls back to download
- Image viewer not working - falls back to download
- Video viewer not working - falls back to download

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
