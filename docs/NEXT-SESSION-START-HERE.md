# Next Session: Start Here 🚀

**Created**: 2026-01-24
**Priority**: 🔴 CRITICAL - Permission System Rollout

---

## 📋 Quick Summary

**What happened last session**:
- Implemented permission middleware core
- Added database seeding (4 test users)
- Did manual testing with different user roles
- **DISCOVERED**: Critical permission issues - all users can access all libraries

**What to do this session**:
- Fix permission system comprehensively (no quick fixes)
- Apply permission checks to ALL API endpoints systematically
- Follow detailed implementation plan

---

## 🎯 Your Mission

Implement **comprehensive permission rollout** following the plan in `docs/PERMISSION-ROLLOUT-PLAN.md`

**Estimated Time**: 2-3 days
**Priority**: BLOCKING production deployment

---

## 📖 Documents to Read (In Order)

### 1. Start Here (5 minutes)
This document - you're reading it now ✅

### 2. Engineering Principles (3 minutes)
**File**: `docs/ENGINEERING-PRINCIPLES.md`
**Why**: Understand why we're doing comprehensive solution instead of quick fixes
**Key Takeaway**: Early development is the BEST time to do things right

### 3. Implementation Plan (15 minutes)
**File**: `docs/PERMISSION-ROLLOUT-PLAN.md`
**Why**: This is your roadmap for the entire implementation
**Key Sections**:
- Critical Issues Discovered (understand what you're fixing)
- Phase 1: Library Access Control (start here)
- Phase 2: File Operations
- Phase 3: Encrypted Library Policy
- Testing Strategy

### 4. Known Issues (5 minutes)
**File**: `docs/KNOWN_ISSUES.md`
**Why**: See detailed descriptions of the 5 critical permission issues
**Section**: "🔴 CRITICAL SECURITY/PERMISSION ISSUES"

---

## 🚦 Step-by-Step Start

### Step 1: Verify Test Users Exist (2 minutes)

```bash
# Check database seeding worked
docker exec cool-storage-api-cassandra-1 cqlsh -e "SELECT email, role FROM sesamefs.users;"

# Expected output:
# admin@sesamefs.local       | admin
# user@sesamefs.local        | user
# readonly@sesamefs.local    | readonly
# guest@sesamefs.local       | guest
```

### Step 2: Start with Phase 1 - Library Access Control

**File to modify**: `internal/api/v2/libraries.go`

**Tasks** (in order):
1. Add helper method `HasLibraryAccess()` to permission middleware
2. Add helper method `GetUserLibraries()` to permission middleware
3. Fix `ListLibraries()` to filter by ownership + shares
4. Add permission check to `GetLibrary()`
5. Add permission check to directory listing in `files.go`

**See**: `docs/PERMISSION-ROLLOUT-PLAN.md` → "Phase 1" for code examples

### Step 3: Test After Phase 1

**Manual Test**:
1. Login as user@sesamefs.local
2. Check library list - should NOT see admin@'s libraries
3. Try to access admin@'s library by URL - should get 403
4. Create own library - should work
5. See own library in list - should work

**Automated Test**:
```bash
# Run permission tests
go test ./internal/middleware -v -run TestHasLibraryAccess
go test ./internal/api/v2 -v -run TestListLibraries
```

### Step 4: Continue with Phase 2 and 3

Follow the plan in `docs/PERMISSION-ROLLOUT-PLAN.md`

Test after each phase - don't implement everything then test

---

## 🧪 Test Users Reference

All passwords follow pattern: `{role}123`

| Email | Password | Role | Should Be Able To |
|-------|----------|------|-------------------|
| admin@sesamefs.local | admin123 | admin | Everything |
| user@sesamefs.local | user123 | user | Create/manage own libraries, write files |
| readonly@sesamefs.local | readonly123 | readonly | Only read own libraries |
| guest@sesamefs.local | guest123 | guest | Only read own libraries |

---

## ✅ Success Criteria

**When Phase 1 is complete:**
- [ ] user@ cannot see admin@'s libraries in list
- [ ] user@ gets 403 when accessing admin@'s library by URL
- [ ] user@ can still see own libraries
- [ ] Shared libraries appear in list (after sharing implemented)

**When Phase 2 is complete:**
- [ ] readonly@ cannot upload files to any library
- [ ] guest@ cannot upload files to any library
- [ ] readonly@ cannot edit files in OnlyOffice
- [ ] user@ can upload to own libraries
- [ ] user@ can upload to rw-shared libraries (when sharing works)

**When Phase 3 is complete:**
- [ ] Cannot create share on encrypted library
- [ ] Get clear error message when attempting to share encrypted library
- [ ] Can share normal (non-encrypted) libraries

**When ALL phases complete:**
- [ ] Zero regressions (desktop client sync still works)
- [ ] All manual tests pass
- [ ] All automated tests pass
- [ ] Documentation updated

---

## 🚨 Critical Reminders

### DO NOT Modify Frozen Components
- ❌ `internal/api/sync.go` - Sync protocol (FROZEN)
- ❌ `internal/api/seafhttp.go` - Download path (working)
- ❌ `internal/crypto/crypto.go` - Encryption (working)

### Permission Checks Are Business Logic
- ✅ Permission failures return 403 Forbidden (standard HTTP)
- ✅ Desktop clients continue using same endpoints
- ✅ No protocol changes required
- ✅ Just add validation before processing requests

### Test Incrementally
- ✅ Test after Phase 1 (library access)
- ✅ Test after Phase 2 (file operations)
- ✅ Test after Phase 3 (encrypted libraries)
- ❌ Don't implement all 3 phases then test

---

## 🎯 Expected Outcome

By end of session, you should have:

1. **Phase 1 Complete** (minimum)
   - Users can only see own libraries
   - Library access properly restricted
   - Automated and manual tests passing

2. **Phase 2 Complete** (ideal)
   - File operations respect permissions
   - readonly/guest cannot write
   - rw permissions work correctly

3. **Phase 3 Complete** (stretch goal)
   - Encrypted libraries cannot be shared
   - Clear error messages

4. **Documentation Updated**
   - CURRENT_WORK.md - Mark phases complete
   - KNOWN_ISSUES.md - Move issues to "Fixed" section
   - CHANGELOG.md - Add session entry

---

## 📚 Quick Reference Links

| Document | Purpose |
|----------|---------|
| [PERMISSION-ROLLOUT-PLAN.md](PERMISSION-ROLLOUT-PLAN.md) | Detailed implementation plan with code examples |
| [ENGINEERING-PRINCIPLES.md](ENGINEERING-PRINCIPLES.md) | Why comprehensive solution over quick fix |
| [KNOWN_ISSUES.md](KNOWN_ISSUES.md) | Issues being fixed |
| [CURRENT_WORK.md](../CURRENT_WORK.md) | Session state and priorities |
| [internal/middleware/README.md](../internal/middleware/README.md) | Permission middleware documentation |

---

## 🆘 If You Get Stuck

1. **Re-read the implementation plan** - It has code examples
2. **Check existing permission middleware** - `internal/middleware/permissions.go` already has role/group logic
3. **Look at working examples** - `CreateLibrary` and `DeleteLibrary` already have permission checks
4. **Test small pieces** - Don't implement everything at once
5. **Document blockers** - If you hit a wall, document it in CURRENT_WORK.md

---

## 🎉 Let's Build This Right

Remember the principle: **Better engineering now = faster development later**

You're not just fixing bugs - you're establishing the permission pattern that will be used for ALL future endpoints.

Do it comprehensively, do it well, and you'll only have to do it once.

**Good luck! 🚀**
