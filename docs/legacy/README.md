# Legacy Documentation

This folder contains outdated or superseded documentation kept for historical reference.

**Last Updated**: 2026-02-01

---

## Files in This Folder

### PROTOCOL-COMPARISON-SUMMARY-2024-12-29.md

**Original Name**: `PROTOCOL-COMPARISON-SUMMARY.md`
**Date**: 2024-12-29
**Status**: ❌ Outdated

**Why moved to legacy**:
- Describes old testing framework using mitmproxy
- Replaced by:
  - `docker/seafile-cli-debug/run-sync-comparison.sh` (API-level comparison)
  - `docker/seafile-cli-debug/run-real-client-sync.sh` (real client test)
  - `docs/SEAFILE-SYNC-PROTOCOL-RFC.md` (formal specification)

**Historical value**: Shows early approach to protocol verification

---

### SEAFILE-IMPLEMENTATION-GUIDE-2024-12-29.md

**Original Name**: `docs/SEAFILE-IMPLEMENTATION-GUIDE.md`
**Date**: 2024-12-29
**Status**: ❌ Outdated / Redundant

**Why moved to legacy**:
- Content superseded by:
  - `docs/SEAFILE-SYNC-PROTOCOL-RFC.md` (formal specification with test vectors)
  - `docs/SEAFILE-SYNC-PROTOCOL.md` (quick reference for developers)
  - `docs/DECISIONS.md` (debugging workflow, protocol-driven development)

**What was useful**:
- Debugging workflow → Moved to `DECISIONS.md`
- Client log locations → Moved to `CLAUDE.md`
- Reference server info → Moved to `CURRENT_WORK.md`

**Historical value**: Shows evolution of understanding Seafile protocol

---

### PERMISSION-ROLLOUT-PLAN.md

**Date**: 2026-01-24
**Status**: ❌ Completed

**Why moved to legacy**:
- Permission middleware rollout is fully complete
- All endpoints have proper permission checks
- Superseded by actual implementation in `internal/middleware/permissions.go`

---

### PERMISSION-ROLLOUT-COMPLETE.md

**Date**: 2026-01-24
**Status**: ❌ Completed

**Why moved to legacy**:
- Completion report for permission rollout — historical record only
- Current permission state tracked in `docs/IMPLEMENTATION_STATUS.md`

---

### SYNC_BUG_MULTIFILE_20260116.md

**Date**: 2026-01-16
**Status**: ❌ Resolved

**Why moved to legacy**:
- Bug investigation for multi-file sync issue — fully resolved
- Fix documented in codebase and CHANGELOG

---

### SEARCH_AND_OPTIMIZATION_PLAN.md

**Date**: 2026-01-22
**Status**: ❌ Outdated

**Why moved to legacy**:
- Search is now implemented via Cassandra SASI indexes
- Original plan recommended Elasticsearch which was not pursued
- Upload/download optimizations described are partially implemented

---

## Files NOT Moved (Still Active)

### SEAFILE-SYNC-AUTH.md
**Status**: ✅ Active (future work)
**Reason**: Covers SSO/2FA authentication which we plan to implement
**Location**: `docs/SEAFILE-SYNC-AUTH.md`

### MIGRATION-FROM-SEAFILE.md
**Status**: ✅ Active (future work)
**Reason**: Migration strategy document for future use
**Location**: `docs/MIGRATION-FROM-SEAFILE.md`

### MULTIREGION-TESTING.md
**Status**: ✅ Active (future work)
**Reason**: Multi-region testing guide for when we implement it
**Location**: `docs/MULTIREGION-TESTING.md`

---

## When to Move Documents to Legacy

Move a document to legacy when:

1. **Replaced by newer documentation**
   - Example: Implementation guide → RFC specification
   - Keep if still referenced or complementary

2. **Describes deprecated approach**
   - Example: Old testing framework → New automated tests
   - Keep if historical context valuable

3. **Outdated by implementation changes**
   - Example: Planned architecture that changed
   - Keep if shows decision-making process

4. **Redundant with other docs**
   - Example: Multiple guides for same feature
   - Consolidate into single source of truth

**DO NOT move**:
- Active implementation plans (even if not yet implemented)
- Reference materials for future features
- Documents still linked from active docs

---

## Naming Convention

When moving to legacy:
```
{ORIGINAL_NAME}-{YYYY-MM-DD}.md
```

Example:
- Original: `PROTOCOL-COMPARISON-SUMMARY.md`
- Legacy: `PROTOCOL-COMPARISON-SUMMARY-2024-12-29.md`

Use the **last modified date** or **date replaced**, whichever is more meaningful.

---

## Cleaning Up Legacy

**Review quarterly** (every 3 months):
- Files older than 1 year → Consider archiving (compress + delete)
- Files with no historical value → Delete
- Files with historical value → Keep

**Archive process**:
```bash
# Compress old legacy files
tar czf legacy-archive-2026-Q1.tar.gz *.md
# Move to archive folder
mkdir -p ../archive
mv legacy-archive-2026-Q1.tar.gz ../archive/
# Delete originals
rm *.md
```
