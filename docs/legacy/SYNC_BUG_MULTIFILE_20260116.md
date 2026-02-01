# Multi-File Library Sync Bug - Diagnosis & Fix

**Date**: 2026-01-16  
**Severity**: CRITICAL  
**Status**: RESOLVED ✅  
**Component**: Seafile Sync Protocol - `check-fs` endpoint  

---

## Executive Summary

Desktop client sync was failing for libraries with many files. Client showed "synced" status but **zero files appeared locally**. Root cause: `check-fs` endpoint incorrectly reported ALL FS objects as missing when they actually existed on the server.

### Impact

- ✅ **FIXED**: Desktop/mobile clients can now sync libraries with multiple files
- ✅ **VERIFIED**: Tested with 770MB library containing 18 files  
- ✅ **STABLE**: All Seafile client logs show clean sync without errors

---

## Symptoms Observed

### User Report
Library `xxxTxT005` (770MB, 18+ files):
- Desktop client shows "synced"
- **Zero files appear in local sync directory**
- No error messages visible to user

### Client Log Analysis
```
[01/16/26 08:29:41] Failed to find dir e9bdd660186953528f6be65aa337e9d8375e2a0a
[01/16/26 08:29:42] change-set.c(234): Failed to find root dir
[01/16/26 08:29:42] sync-mgr.c(961): Failed to commit to repo xxxTxT005
[01/16/26 08:29:42] sync-mgr.c(617): Repo sync state transition to 'error': 'Error when indexing'
```

Client successfully downloaded FS objects but **failed to save them locally**.

---

## Root Cause

### The Bug

`check-fs` endpoint was querying database with **COMPUTED** FS IDs (what clients send), but database stores **ORIGINAL** (stored) FS IDs. Result: ALL FS objects reported as missing.

### Code Location

**File**: `internal/api/sync.go`  
**Function**: `CheckFS()` (line 1405)  
**Issue**: Missing FS ID mapping (computed → stored)

### Broken Code (Before Fix)

```go
for _, fsID := range fsIDs {
    var exists string
    err := h.db.Session().Query(`
        SELECT fs_id FROM fs_objects WHERE library_id = ? AND fs_id = ? LIMIT 1
    `, repoID, fsID).Scan(&exists)  // ← BUG: checking for COMPUTED ID
    
    if err != nil {
        missing = append(missing, fsID)  // Reports ALL as missing!
    }
}
```

---

## Technical Background

### FS ID Dual-ID System

SesameFS uses two types of FS IDs:

| Type | Definition | Usage |
|------|------------|-------|
| **Stored FS ID** | SHA-1 of original uploaded JSON | Database storage key |
| **Computed FS ID** | SHA-1 of corrected JSON (ordered keys, fixed child IDs) | Sync protocol responses |

### Why This Exists

- Web uploads create objects with **stored IDs**
- Desktop clients expect **computed IDs** (Seafile protocol requirement)
- Server must map between them during sync

### Previous Implementations

Mapping already implemented for:
- ✅ `fs-id-list` endpoint
- ✅ `pack-fs` endpoint

But **NOT** for `check-fs` endpoint ← **This was the bug!**

---

## Protocol Comparison Analysis

### Test Setup

Created `compare_multifile_sync.py` to compare responses between:
- **Stock Seafile** (https://app.nihaoconsult.com)
- **SesameFS** (http://localhost:8080)

### Critical Difference Found

```
Endpoint: POST /seafhttp/repo/{id}/check-fs
Request:  ["fs_id_1", "fs_id_2", ... "fs_id_18"]  (18 FS IDs)

Stock Seafile Response: []  ← Empty = all objects exist
SesameFS Response:      ["fs_id_1", "fs_id_2", ... "fs_id_18"]  ← All missing!
```

**Severity**: CRITICAL - This breaks sync completely.

---

## Solution Implemented

### Fix Overview

Apply the same FS ID mapping used in `pack-fs` to `check-fs`:

1. Build computed→stored ID mapping from HEAD commit
2. For each requested ID: map computed → stored  
3. Check if **stored** ID exists in database
4. Return computed IDs that are missing

### Code Changes

```go
func (h *SyncHandler) CheckFS(c *gin.Context) {
    // ... parse request ...
    
    // ADDED: Build FS ID mapping
    var headCommitID, rootFSID string
    h.db.Session().Query(`SELECT head_commit_id FROM libraries...`).Scan(&headCommitID)
    h.db.Session().Query(`SELECT root_fs_id FROM commits...`).Scan(&rootFSID)
    
    computedToStored, _ := h.buildFSIDMapping(repoID, rootFSID)
    
    // FIXED: Map computed → stored before checking database
    missing := make([]string, 0)
    for _, computedFSID := range fsIDs {
        // Map to stored ID
        storedFSID, hasMapping := computedToStored[computedFSID]
        if !hasMapping {
            storedFSID = computedFSID  // Fallback
        }
        
        // Check if STORED ID exists (NOT computed ID)
        var exists string
        err := h.db.Session().Query(`
            SELECT fs_id FROM fs_objects WHERE library_id = ? AND fs_id = ? LIMIT 1
        `, repoID, storedFSID).Scan(&exists)  // ← FIXED
        
        if err != nil {
            missing = append(missing, computedFSID)  // Return computed ID
        }
    }
    
    c.JSON(http.StatusOK, missing)
}
```

---

## Verification & Testing

### Test Library

- **Name**: xxxTxT005
- **ID**: 01920c46-b74b-4802-ad7c-db66732423ab
- **Size**: 770.8MB
- **Files**: 18 files (ZIPs, DMGs, PDFs, logs)

### Test Client

- **Tool**: seaf-cli 7.0.10 (official Seafile desktop client)
- **Platform**: Debian Bullseye (Docker container)

### Results

| Metric | Before Fix | After Fix |
|--------|-----------|-----------|
| `check-fs` response | `[... ALL 18 IDs ...]` | `[]` (empty) |
| Sync status | `"error - Error when indexing"` | `"synchronized"` |
| Files synced | 0 | 18 files (1.1GB) ✅ |
| Errors in log | Yes - "Failed to find dir" | None ✅ |

### Files Successfully Synced

```bash
11-30-2025 INCURVE 2025 Backup1.CAB            9.8M
11-30-2025 IPM BACKUP for 20251.CAB           35M
Aiden Caleb Cabasag Guzman Sanchez 0.pdf      4.9M
Bright Beginnings Immigration (1).pdf         17M
Bright Beginnings Immigration (2).pdf         13M
Bright Beginnings Immigration (3).pdf         5.2M
Bright Beginnings Immigration (4).pdf         3.0M
Bright Beginnings Immigration.docx            4.2M
Bright Beginnings Immigration.pdf             18M
Untitled document.pdf                         3.6M
bt-mouse-full.log                            128M
bt-mouse.log                                 186M
cover1.png                                    3.1M
scene1.png                                    2.9M
seadrive.log                                  11M
seafile-client-9.0.13 (1).dmg               198M
seafile-client-9.0.13.dmg                   198M
seafile-nihaocloud-master.zip               207M
```

**Total**: 18 files, 1.1GB - **ALL PRESENT** ✅

### Client Log (After Fix)

```
[01/16/26 12:45:49] Transfer repo: init → check → commit → fs → data → finished
[01/16/26 12:46:01] Repo 'xxxTxT005' sync state transition from 'synchronized' to 'committing'
[01/16/26 12:46:01] All events are processed for repo xxxTxT005
```

**No errors!** Clean, successful sync. ✅

---

## Additional Protocol Findings

### Low-Priority Differences

These don't break sync but should be fixed for 100% compatibility:

**1. `encrypted` field type in `download-info`**
- Stock: `""` (empty string)
- Us: `0` (integer)

**2. Missing encryption fields for non-encrypted repos**
- Stock includes: `enc_version`, `salt`, `magic`, `random_key` (all empty)
- We omit them

**3. Missing `encrypted` field in commit objects**
- Stock: `"encrypted": "false"` (string)
- We omit this field

**Recommendation**: Address in future for perfect compatibility.

---

## Related Systems

### `buildFSIDMapping()` Function

Shared by `pack-fs` and `check-fs`:

```
Input:  repoID, rootFSID
Output: map[computedFSID]storedFSID

Process:
1. Recursively traverse FS tree from root
2. For each object:
   - Load stored JSON from DB
   - Compute corrected JSON (ordered keys, fixed child IDs)
   - Calculate SHA-1 = computed FS ID
   - Map: computedFSID → storedFSID
3. Return mapping
```

**Performance**: Built once per request, O(n) where n = number of FS objects.

### Endpoints Using FS ID Mapping

| Endpoint | Uses Mapping | Status |
|----------|-------------|--------|
| `fs-id-list` | ✅ | Working |
| `pack-fs` | ✅ | Working |
| `check-fs` | ✅ **FIXED** | **Working** |
| `recv-fs` | N/A | Working |

---

## Future Improvements

1. **Cache FS ID mapping** across requests (in-memory cache with TTL)
2. **Fix encrypted field types** to match stock Seafile exactly
3. **Add missing encryption fields** for non-encrypted repos
4. **Add `encrypted` field to commit objects** (string "true"/"false")
5. **Performance**: Consider pre-computing mappings during upload

---

## References

- **Comparison Script**: `docker/seafile-cli-debug/scripts/compare_multifile_sync.py`
- **Comparison Output**: `/tmp/sync_protocol_comparison_20260116_124055.txt`
- **Code Fix**: `internal/api/sync.go` lines 1405-1492
- **Test**: `docker/seafile-cli-debug/scripts/test_xxxTxT005_sync.py`

---

## Conclusion

The multi-file library sync bug was caused by the `check-fs` endpoint checking for computed FS IDs when the database stores original (stored) FS IDs. The fix applies the same FS ID mapping logic already used in `pack-fs`, ensuring the correct IDs are checked in the database.

**Status**: ✅ RESOLVED  
**Date**: 2026-01-16  
**Verification**: Tested with real desktop client, 770MB library (18 files), clean sync  
**Confidence Level**: HIGH
