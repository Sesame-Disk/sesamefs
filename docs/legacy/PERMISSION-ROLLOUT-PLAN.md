# Comprehensive Permission Rollout - Implementation Plan

**Created**: 2026-01-24
**Status**: 📋 READY TO IMPLEMENT
**Priority**: 🔴 CRITICAL - Blocking production readiness
**Estimated Time**: 2-3 days

---

## 🎯 Goal

Apply permission middleware systematically to ALL API endpoints to enforce:
- Users can only see their own libraries (unless explicitly shared)
- Encrypted libraries cannot be shared
- File operations respect library permissions (owner/rw/r)
- Organization roles enforced (admin/user/readonly/guest)

---

## 🔴 Critical Issues Discovered (Manual Testing 2026-01-24)

### Issue 1: All Users See All Libraries
- **Bug**: `user@sesamefs.local` can see libraries owned by `admin@sesamefs.local`
- **Expected**: Users only see own libraries + explicitly shared libraries
- **Root Cause**: `ListLibraries` has NO permission filtering
- **File**: `internal/api/v2/libraries.go` - `ListLibraries()` function

### Issue 2: Encrypted Libraries Shareable
- **Policy**: Password-encrypted libraries CANNOT be shared
- **Status**: NOT ENFORCED in backend
- **Impact**: Security vulnerability

### Issue 3: Readonly User Can Write to Other Users' Encrypted Files
- **Bug**: `readonly@sesamefs.local` edited Word docx in encrypted library owned by admin
- **Expected**: readonly = read-only, and NO access to others' libraries
- **Root Cause**: File upload/edit endpoints have NO permission checks

### Issue 4: Guest Can Access and Corrupt User's Library
- **Bug**: `guest@sesamefs.local` accessed `user@sesamefs.local`'s library
- **Actions**: Browsed files, created file, original files disappeared
- **Expected**: guest has ZERO access to other users' libraries
- **Severity**: CRITICAL - Data loss + zero isolation

---

## 📋 Implementation Plan

### Phase 1: Library Access Control (Day 1)

#### 1.1 Fix `ListLibraries` - Filter by Ownership + Shares
**File**: `internal/api/v2/libraries.go`

**Current behavior**: Returns ALL libraries in organization

**New behavior**: Return only libraries where user has access:
```sql
-- Owned libraries
SELECT * FROM libraries WHERE owner_id = ?

UNION

-- Directly shared libraries
SELECT l.* FROM libraries l
JOIN library_shares ls ON l.repo_id = ls.repo_id
WHERE ls.user_id = ? AND ls.org_id = ?

UNION

-- Group-shared libraries
SELECT l.* FROM libraries l
JOIN library_shares ls ON l.repo_id = ls.repo_id
JOIN group_members gm ON ls.group_id = gm.group_id
WHERE gm.user_id = ? AND ls.org_id = ?
```

**Implementation steps**:
1. Add helper method to `PermissionMiddleware`: `GetUserLibraries(orgID, userID) []LibraryPermission`
2. Modify `ListLibraries` to call helper instead of querying all libraries
3. Return libraries with permission level (owner/rw/r)

**Testing**:
- Login as user@ → Should see ONLY user@'s libraries
- Share library with user@ → Should now see shared library
- Unshare → Should disappear from list

---

#### 1.2 Add Permission Check to `GetLibrary`
**File**: `internal/api/v2/libraries.go`

**Current behavior**: Returns library info for any repo_id

**New behavior**: Check if user has read permission BEFORE returning info

```go
func (h *LibraryHandler) GetLibrary(c *gin.Context) {
    repoID := c.Param("repo_id")
    orgID := c.GetString("org_id")
    userID := c.GetString("user_id")

    // PERMISSION CHECK: User must have at least read access
    hasAccess, err := h.permMiddleware.HasLibraryAccess(orgID, userID, repoID, middleware.PermissionRead)
    if err != nil {
        c.JSON(500, gin.H{"error": "failed to check permissions"})
        return
    }

    if !hasAccess {
        c.JSON(403, gin.H{"error": "you do not have access to this library"})
        return
    }

    // Continue with existing logic...
}
```

**Testing**:
- Direct URL to another user's library → Should get 403
- URL to own library → Should work
- URL to shared library → Should work

---

#### 1.3 Add Permission Check to Directory Listing
**File**: `internal/api/v2/files.go` - `ListDirectory` function

**Current behavior**: Lists directory contents for any repo_id

**New behavior**: Check library access before listing

```go
func (h *FileHandler) ListDirectory(c *gin.Context) {
    repoID := c.Query("repo_id")
    path := c.Query("path")
    orgID := c.GetString("org_id")
    userID := c.GetString("user_id")

    // PERMISSION CHECK
    hasAccess, err := h.permMiddleware.HasLibraryAccess(orgID, userID, repoID, middleware.PermissionRead)
    if err != nil || !hasAccess {
        c.JSON(403, gin.H{"error": "access denied"})
        return
    }

    // Continue...
}
```

**Testing**:
- Browse another user's library → Should get 403
- Browse own library → Should work

---

### Phase 2: File Operations (Day 2)

#### 2.1 File Upload Permission Check
**Files**:
- `internal/api/seafhttp.go` - `HandleUpload`
- `internal/api/v2/files.go` - File creation endpoints

**Check required**: Write permission (owner or rw)

```go
// In HandleUpload - after validating token, before accepting file
hasWrite, err := h.permMiddleware.HasLibraryAccess(orgID, userID, repoID, middleware.PermissionWrite)
if err != nil || !hasWrite {
    c.JSON(403, gin.H{"error": "no write permission"})
    return
}
```

**Testing**:
- readonly@ tries to upload → 403
- guest@ tries to upload → 403
- user@ uploads to own library → Works
- user@ uploads to library shared with rw → Works

---

#### 2.2 File Edit Permission Check
**Files**:
- `internal/api/v2/onlyoffice.go` - OnlyOffice save callback
- `internal/api/v2/seadoc.go` - Seadoc operations (if implemented)

**Check required**: Write permission

**Testing**:
- readonly@ edits Word doc in own library → Should fail (readonly role)
- user@ edits Word doc in own library → Works
- user@ edits in rw-shared library → Works
- user@ edits in r-shared library → Fails

---

#### 2.3 File Delete/Rename/Move Permission Check
**File**: `internal/api/v2/files.go`

**Endpoints to protect**:
- `DELETE /api/v2.1/repos/{repo_id}/file/`
- `POST /api/v2.1/repos/{repo_id}/file/?op=rename`
- `POST /api/v2.1/repos/{repo_id}/file/?op=move`

**Check required**: Write permission

---

#### 2.4 File Download Permission Check
**File**: `internal/api/seafhttp.go` - `HandleDownload`

**Current**: Token-based access (token from download-info endpoint)

**Enhancement**: Verify user still has read access when download token is used

**Note**: This is lower priority since download-info already requires access to library

---

### Phase 3: Encrypted Library Policy (Day 2-3)

#### 3.1 Enforce No-Sharing for Encrypted Libraries
**File**: `internal/api/v2/file_shares.go` - Share creation endpoints

```go
func (h *ShareHandler) CreateShare(c *gin.Context) {
    repoID := c.Param("repo_id")

    // Check if library is encrypted
    var encrypted int
    err := h.db.Session().Query(`
        SELECT encrypted FROM libraries WHERE repo_id = ?
    `, repoID).Scan(&encrypted)

    if encrypted > 0 {
        c.JSON(403, gin.H{
            "error": "Cannot share encrypted libraries. Move files to a non-encrypted library to share them."
        })
        return
    }

    // Continue with share creation...
}
```

**Testing**:
- Try to share encrypted library → Get clear error message
- Try to share normal library → Works

---

### Phase 4: Helper Methods (Throughout)

Add to `internal/middleware/permissions.go`:

```go
// HasLibraryAccess checks if user has at least the specified permission level
func (m *PermissionMiddleware) HasLibraryAccess(
    orgID, userID, repoID uuid.UUID,
    requiredPermission Permission,
) (bool, error) {
    // 1. Check if user is library owner
    // 2. Check direct library shares
    // 3. Check group library shares
    // 4. Return highest permission found
    // 5. Compare against requiredPermission
}

// GetUserLibraries returns all libraries user has access to with permission levels
func (m *PermissionMiddleware) GetUserLibraries(
    orgID, userID uuid.UUID,
) ([]LibraryWithPermission, error) {
    // Return owned + shared libraries
}

// GetLibraryPermission returns user's permission level for a specific library
func (m *PermissionMiddleware) GetLibraryPermission(
    orgID, userID, repoID uuid.UUID,
) (Permission, error) {
    // Returns: owner, rw, r, or none
}
```

---

## 🧪 Testing Strategy

### Automated Tests to Create

**File**: `internal/middleware/permissions_test.go`

```go
func TestHasLibraryAccess_Owner(t *testing.T)
func TestHasLibraryAccess_DirectShare_RW(t *testing.T)
func TestHasLibraryAccess_DirectShare_R(t *testing.T)
func TestHasLibraryAccess_GroupShare(t *testing.T)
func TestHasLibraryAccess_NoAccess(t *testing.T)
func TestGetUserLibraries_FiltersCorrectly(t *testing.T)
func TestEncryptedLibrarySharing_Blocked(t *testing.T)
```

### Manual Test Scenarios

Create test script: `test-permissions.md`

1. **User Isolation Test**
   - Login as user@, should NOT see admin@'s libraries
   - Create library as user@, admin@ should NOT see it

2. **Permission Level Test**
   - Share library with "r" permission
   - Try to upload → Should fail
   - Share with "rw" permission
   - Try to upload → Should work

3. **Encrypted Library Test**
   - Create encrypted library
   - Try to share → Should get error
   - Decrypt library, try to share → Should work

4. **Role-Based Test**
   - readonly@ should only read own libraries
   - guest@ should only read own libraries
   - Neither should see other users' libraries

---

## 📊 Progress Tracking

### Checklist

**Phase 1: Library Access Control**
- [ ] Add `HasLibraryAccess()` helper method
- [ ] Add `GetUserLibraries()` helper method
- [ ] Fix `ListLibraries` to filter by access
- [ ] Add permission check to `GetLibrary`
- [ ] Add permission check to directory listing
- [ ] Test: User isolation (users can't see each other's libraries)

**Phase 2: File Operations**
- [ ] Add permission check to file upload (seafhttp.go)
- [ ] Add permission check to OnlyOffice edit
- [ ] Add permission check to file delete
- [ ] Add permission check to file rename
- [ ] Add permission check to file move
- [ ] Test: readonly cannot write
- [ ] Test: guest cannot write
- [ ] Test: rw share allows write

**Phase 3: Encrypted Library Policy**
- [ ] Block share creation for encrypted libraries
- [ ] Add clear error message to frontend
- [ ] Test: Cannot share encrypted library
- [ ] Test: Can share after decrypting

**Phase 4: Testing & Documentation**
- [ ] Create automated tests
- [ ] Run full manual test suite
- [ ] Update API-REFERENCE.md with permission requirements
- [ ] Update KNOWN_ISSUES.md (mark issues as fixed)
- [ ] Update CURRENT_WORK.md

---

## 🚨 Critical Reminders

### DO NOT Break Frozen Components
- ✅ Sync protocol (`internal/api/sync.go`) - DO NOT MODIFY
- ✅ File download (`internal/api/seafhttp.go` download path) - Already working
- ✅ Encryption (`internal/crypto/crypto.go`) - Already working

### Add Permissions WITHOUT Changing Protocol
- Permission checks are BUSINESS LOGIC, not protocol changes
- Desktop clients continue using same endpoints
- Permission failures return 403 Forbidden (standard HTTP)

### Test After Each Phase
- Don't implement all 3 phases then test
- Test Phase 1 completely before starting Phase 2
- Easier to debug issues if tested incrementally

---

## 📚 Reference

### Existing Permission Middleware Location
- **File**: `internal/middleware/permissions.go`
- **Documentation**: `internal/middleware/README.md`
- **Already implemented**: Role hierarchy, group resolution

### Database Schema
- **libraries**: `owner_id`, `encrypted`
- **library_shares**: `repo_id`, `user_id`, `group_id`, `permission`
- **group_members**: `group_id`, `user_id`, `role`
- **users**: `user_id`, `role` (org-level role)

### Permission Levels
```go
const (
    PermissionNone  Permission = ""
    PermissionRead  Permission = "r"
    PermissionWrite Permission = "rw"
    PermissionOwner Permission = "owner"
)
```

---

## 🎯 Success Criteria

**When complete, ALL of these should be true:**

✅ User A cannot see User B's libraries in list
✅ User A cannot access User B's library by direct URL
✅ User A cannot browse User B's library directories
✅ User A cannot upload files to User B's library
✅ User A cannot edit files in User B's library
✅ readonly role cannot write to ANY library (even their own)
✅ guest role cannot write to ANY library (even their own)
✅ Encrypted libraries cannot be shared
✅ Sharing works correctly for non-encrypted libraries
✅ Group-based permissions work (users inherit group library access)

**Zero regressions:**
✅ Desktop client sync still works
✅ Existing file download functionality intact
✅ Encrypted library unlock/decrypt still works

---

## Related Documents

- [ENGINEERING-PRINCIPLES.md](ENGINEERING-PRINCIPLES.md) - Why we do comprehensive solutions
- [KNOWN_ISSUES.md](KNOWN_ISSUES.md) - Issues this plan addresses
- [CURRENT_WORK.md](../CURRENT_WORK.md) - Session priorities
- [internal/middleware/README.md](../internal/middleware/README.md) - Permission middleware docs
