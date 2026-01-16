# Implementation Status - SesameFS

**Last Updated**: 2026-01-16

---

## Status Legend

| Symbol | Meaning | Stability | Safe to Modify? |
|--------|---------|-----------|-----------------|
| 🔒 **FROZEN** | Protocol-verified, desktop client tested, DO NOT MODIFY | **STABLE** | ❌ No - only with user approval |
| ✅ **COMPLETE** | Implemented, basic testing done | Mostly stable | ⚠️ Caution - review tests first |
| 🟡 **PARTIAL** | Stub exists or incomplete implementation | **UNSTABLE** | ✅ Yes - active development |
| ❌ **TODO** | Not started | N/A | ✅ Yes - greenfield |

---

## Core Components

| Component | Status | Stability | Protocol Tested | Last Verified | Notes |
|-----------|--------|-----------|-----------------|---------------|-------|
| **Sync Protocol (Desktop Client)** | 🔒 FROZEN | **STABLE** | ✅ Yes | 2026-01-16 | Both comparison + real client tests pass |
| **Encrypted Libraries (PBKDF2)** | 🔒 FROZEN | **STABLE** | ✅ Yes | 2026-01-13 | Test vectors verified |
| **File Block Encryption (AES-256-CBC)** | ✅ COMPLETE | Mostly stable | ⚠️ Partial | 2026-01-09 | Works with desktop client |
| **Block Storage (S3)** | ✅ COMPLETE | Mostly stable | ⚠️ Partial | 2026-01-09 | SHA-1→SHA-256 mapping working |
| **Block ID Mapping (SHA-1→SHA-256)** | ✅ COMPLETE | Mostly stable | ✅ Yes | 2026-01-09 | Desktop client uploads/downloads work |
| **File Upload (REST API)** | ✅ COMPLETE | Mostly stable | ❌ No | 2026-01-08 | Works but not protocol-verified |
| **File Download (REST API)** | ✅ COMPLETE | Mostly stable | ❌ No | 2026-01-08 | Works but not protocol-verified |
| **Directory Listing** | ✅ COMPLETE | Mostly stable | ❌ No | 2026-01-08 | Frontend integration works |
| **Library CRUD** | ✅ COMPLETE | Mostly stable | ⚠️ Partial | 2026-01-08 | Create/delete/list working |
| **Starred Files** | ✅ COMPLETE | Mostly stable | ❌ No | 2026-01-08 | Fixed Cassandra query issue |
| **OnlyOffice Integration** | 🟡 PARTIAL | **UNSTABLE** | ❌ No | 2026-01-08 | Opens files but config needs tuning |
| **Frontend (React)** | 🟡 PARTIAL | **UNSTABLE** | N/A | 2026-01-08 | Library list works, ~100 modals broken |
| **User Management** | 🟡 PARTIAL | **UNSTABLE** | ❌ No | - | Dev tokens only, no OIDC |
| **Sharing System** | 🟡 PARTIAL | **UNSTABLE** | ❌ No | - | UI complete, backend stub only |
| **Version History** | ❌ TODO | N/A | ❌ No | - | Not started |
| **Multi-Region Replication** | ❌ TODO | N/A | ❌ No | - | Not started |

---

## Protocol Endpoints (Seafile Compatibility)

### Sync Protocol (`/seafhttp/`) - 🔒 FROZEN

**DO NOT MODIFY** these endpoints without explicit user request or desktop client breakage.

| Endpoint | Status | Stability | Last Verified | Critical Details |
|----------|--------|-----------|---------------|------------------|
| `GET /seafhttp/protocol-version` | 🔒 FROZEN | **STABLE** | 2026-01-16 | Returns `{"version": 2}` |
| `GET /seafhttp/repo/:id/permission-check/` | 🔒 FROZEN | **STABLE** | 2026-01-16 | Returns empty 200 OK |
| `GET /seafhttp/repo/:id/quota-check/` | 🔒 FROZEN | **STABLE** | 2026-01-16 | Returns quota info |
| `GET /seafhttp/repo/:id/commit/HEAD` | 🔒 FROZEN | **STABLE** | 2026-01-16 | `is_corrupted` MUST be integer 0 |
| `PUT /seafhttp/repo/:id/commit/HEAD` | 🔒 FROZEN | **STABLE** | 2026-01-16 | Update HEAD pointer |
| `GET /seafhttp/repo/:id/commit/:cid` | 🔒 FROZEN | **STABLE** | 2026-01-16 | `encrypted: "true"` (string), NO `no_local_history` |
| `PUT /seafhttp/repo/:id/commit/:cid` | 🔒 FROZEN | **STABLE** | 2026-01-16 | Store commit object |
| `GET /seafhttp/repo/:id/fs-id-list/` | 🔒 FROZEN | **STABLE** | 2026-01-16 | Returns JSON array (NOT newline-separated) |
| `GET /seafhttp/repo/:id/fs/:fsid` | 🔒 FROZEN | **STABLE** | 2026-01-16 | Get single FS object |
| `POST /seafhttp/repo/:id/pack-fs` | 🔒 FROZEN | **STABLE** | 2026-01-16 | Binary: 40-byte ID + 4-byte size (BE) + zlib |
| `POST /seafhttp/repo/:id/recv-fs` | 🔒 FROZEN | **STABLE** | 2026-01-16 | Receive FS objects (binary format) |
| `POST /seafhttp/repo/:id/check-fs` | 🔒 FROZEN | **STABLE** | 2026-01-16 | JSON array input/output |
| `POST /seafhttp/repo/:id/check-blocks` | 🔒 FROZEN | **STABLE** | 2026-01-16 | JSON array input/output |
| `GET /seafhttp/repo/:id/block/:bid` | ✅ COMPLETE | Mostly stable | 2026-01-09 | SHA-1→SHA-256 mapping works |
| `PUT /seafhttp/repo/:id/block/:bid` | ✅ COMPLETE | Mostly stable | 2026-01-09 | Block upload works |
| `POST /seafhttp/repo/head-commits-multi` | 🔒 FROZEN | **STABLE** | 2026-01-16 | Multi-repo HEAD check |

**Protocol Requirements** (from `docs/SEAFILE-SYNC-PROTOCOL-RFC.md`):
- Authentication: `Seafile-Repo-Token` header (NOT `Authorization`)
- fs-id-list: MUST return JSON array
- Commit objects: MUST omit `no_local_history` field
- Field types: See Critical Format Requirements table below

### REST API - Libraries (`/api2/repos/`, `/api/v2.1/repos/`)

| Endpoint | Status | Stability | Notes |
|----------|--------|-----------|-------|
| `GET /api2/repos/` | ✅ COMPLETE | Mostly stable | List libraries |
| `POST /api2/repos/` | ✅ COMPLETE | Mostly stable | Create library (supports `passwd` param) |
| `GET /api2/repos/:id/` | ✅ COMPLETE | Mostly stable | Get library info |
| `POST /api2/repos/:id/?op=rename` | ✅ COMPLETE | Mostly stable | Rename library |
| `DELETE /api2/repos/:id/` | ✅ COMPLETE | Mostly stable | Delete library |
| `GET /api2/repos/:id/download-info/` | 🔒 FROZEN | **STABLE** | Sync token for desktop client |
| `GET /api/v2.1/repos/` | ✅ COMPLETE | Mostly stable | v2.1 API variant |
| `DELETE /api/v2.1/repos/:id/` | ✅ COMPLETE | Mostly stable | v2.1 delete variant |

### REST API - File Operations

| Endpoint | Status | Stability | Notes |
|----------|--------|-----------|-------|
| `GET /api2/repos/:id/dir/` | ✅ COMPLETE | Mostly stable | List directory contents |
| `POST /api2/repos/:id/dir/` | 🟡 PARTIAL | **UNSTABLE** | Create directory (stub) |
| `DELETE /api2/repos/:id/dir/` | 🟡 PARTIAL | **UNSTABLE** | Delete directory (stub) |
| `GET /api2/repos/:id/file/` | 🟡 PARTIAL | **UNSTABLE** | Get file info (stub) |
| `DELETE /api2/repos/:id/file/` | 🟡 PARTIAL | **UNSTABLE** | Delete file (stub) |
| `PUT /api2/repos/:id/file/` | ✅ COMPLETE | Mostly stable | Lock/unlock file |
| `GET /api2/repos/:id/upload-link/` | ✅ COMPLETE | Mostly stable | Get upload URL |
| `POST /seafhttp/upload-api/:token` | ✅ COMPLETE | Mostly stable | Upload file (multipart) |
| `GET /api2/repos/:id/file/download-link` | ✅ COMPLETE | Mostly stable | Get download URL |
| `GET /seafhttp/files/:token/:filename` | ✅ COMPLETE | Mostly stable | Download file |
| `POST /api2/repos/:id/file/move/` | 🟡 PARTIAL | **UNSTABLE** | Move file (stub) |
| `POST /api2/repos/:id/file/copy/` | 🟡 PARTIAL | **UNSTABLE** | Copy file (stub) |
| `GET /api/v2.1/repos/:id/dir/` | ✅ COMPLETE | Mostly stable | v2.1 list directory |
| `GET /api/v2.1/repos/:id/file/?p=:path` | ❌ TODO | N/A | Get file metadata (needed for "View on Cloud") |

### REST API - Encrypted Libraries

| Endpoint | Status | Stability | Notes |
|----------|--------|-----------|-------|
| `POST /api/v2.1/repos/:id/set-password/` | 🔒 FROZEN | **STABLE** | Unlock library (verify password) |
| `PUT /api/v2.1/repos/:id/set-password/` | 🔒 FROZEN | **STABLE** | Change library password |

### REST API - Sharing

| Endpoint | Status | Stability | Notes |
|----------|--------|-----------|-------|
| `GET /api/v2.1/share-links/` | 🟡 PARTIAL | **UNSTABLE** | List share links (stub) |
| `POST /api/v2.1/share-links/` | 🟡 PARTIAL | **UNSTABLE** | Create share link (stub) |
| `DELETE /api/v2.1/share-links/:token/` | 🟡 PARTIAL | **UNSTABLE** | Delete share link (stub) |
| Share to users/groups | ❌ TODO | N/A | Not started |

### REST API - Starred Files

| Endpoint | Status | Stability | Notes |
|----------|--------|-----------|-------|
| `GET /api2/starredfiles/` | ✅ COMPLETE | Mostly stable | List starred files |
| `POST /api2/starredfiles/` | ✅ COMPLETE | Mostly stable | Star file |
| `DELETE /api2/starredfiles/` | ✅ COMPLETE | Mostly stable | Unstar file |
| `GET /api/v2.1/starred-items/` | ✅ COMPLETE | Mostly stable | v2.1 variant (fixed 2026-01-08) |

### REST API - OnlyOffice

| Endpoint | Status | Stability | Notes |
|----------|--------|-----------|-------|
| `GET /api/v2.1/onlyoffice/editor/:id` | 🟡 PARTIAL | **UNSTABLE** | Get editor config |
| `POST /api/v2.1/onlyoffice/callback/` | 🟡 PARTIAL | **UNSTABLE** | OnlyOffice save callback |

### REST API - Authentication

| Endpoint | Status | Stability | Notes |
|----------|--------|-----------|-------|
| `POST /api2/auth-token/` | ✅ COMPLETE | Mostly stable | Login (dev mode only) |
| `GET /api2/account/info/` | ✅ COMPLETE | Mostly stable | User info |
| `GET /api2/server-info/` | ✅ COMPLETE | Mostly stable | Server capabilities |
| OIDC integration | ❌ TODO | N/A | Not started |

---

## Critical Format Requirements

**These MUST match exactly or desktop client breaks:**

| Endpoint | Field | Required Type | ❌ Wrong Type |
|----------|-------|---------------|---------------|
| `GET /api2/repos/{id}/download-info/` | `encrypted` | integer `0` or `1` | boolean |
| `GET /api2/repos/{id}/download-info/` | `salt` | string `""` | null |
| `GET /seafhttp/repo/{id}/commit/HEAD` | `is_corrupted` | integer `0` | boolean |
| `GET /seafhttp/repo/{id}/commit/{id}` | `encrypted` | string `"true"` | integer or boolean |
| `GET /seafhttp/repo/{id}/commit/{id}` | `repo_desc` | string `""` | null |
| `GET /seafhttp/repo/{id}/commit/{id}` | `repo_category` | string `""` | null |
| `GET /seafhttp/repo/{id}/commit/{id}` | `no_local_history` | **MUST OMIT** | any value breaks client |
| `GET /seafhttp/repo/{id}/fs-id-list/` | response | JSON array `[]` | newline-separated text |

---

## Encryption Components

| Component | Status | Stability | Verified Against | Notes |
|-----------|--------|-----------|------------------|-------|
| **PBKDF2 Key Derivation (Magic)** | 🔒 FROZEN | **STABLE** | Stock Seafile | Input: repo_id + password |
| **PBKDF2 Key Derivation (Random Key)** | 🔒 FROZEN | **STABLE** | Stock Seafile | Input: password ONLY |
| **Static Salt (enc_version 2)** | 🔒 FROZEN | **STABLE** | Stock Seafile | `{0xda,0x90,0x45,0xc3,0x06,0xc7,0xcc,0x26}` |
| **File Block Encryption (AES-256-CBC)** | ✅ COMPLETE | Mostly stable | Desktop client | Format: [16-byte IV][encrypted content] |
| **Decrypt Session Manager** | ✅ COMPLETE | Mostly stable | Manual testing | 1-hour TTL, in-memory |
| Argon2id (server-side storage) | ✅ COMPLETE | Mostly stable | Unit tests | 300× slower brute-force vs PBKDF2 |

**Test Vectors**: See `docs/SEAFILE-SYNC-PROTOCOL-RFC.md` Section 11

---

## Frontend Components

| Component | Status | Stability | Notes |
|-----------|--------|-----------|-------|
| Library List | ✅ COMPLETE | Mostly stable | Delete, rename, star working |
| Directory Browser | ✅ COMPLETE | Mostly stable | File list, navigation works |
| File Upload (Drag & Drop) | ✅ COMPLETE | Mostly stable | Works with backend |
| File Download | ✅ COMPLETE | Mostly stable | Works with backend |
| Encrypted Library Unlock Dialog | ✅ COMPLETE | Mostly stable | Fixed 2026-01-08 (uses Bootstrap modal) |
| Library Password Change Dialog | ✅ COMPLETE | Mostly stable | Fixed 2026-01-08 (uses Bootstrap modal) |
| Delete Library Dialog | ✅ COMPLETE | Mostly stable | Fixed (uses Bootstrap modal) |
| Create Library Dialog | ✅ COMPLETE | Mostly stable | Fixed (uses Bootstrap modal) |
| Rename File/Folder Dialog | ✅ COMPLETE | Mostly stable | Fixed (uses Bootstrap modal) |
| Share Dialog | ✅ COMPLETE | Mostly stable | UI complete, backend stub |
| **~100 Other Modal Dialogs** | 🟡 PARTIAL | **UNSTABLE** | Need reactstrap Modal → Bootstrap migration |
| OnlyOffice Editor Integration | 🟡 PARTIAL | **UNSTABLE** | Opens but toolbar sometimes greyed |
| Icon Loading | 🟡 PARTIAL | **UNSTABLE** | Some 404s, needs audit |

**Frontend Critical Issue**: ~100 dialog files still use `reactstrap Modal` which doesn't render inside `ModalPortal`. Must use plain Bootstrap modal classes instead. See `docs/FRONTEND.md` for complete list and pattern.

---

## Database Schema (Cassandra)

| Table | Status | Notes |
|-------|--------|-------|
| `organizations` | ✅ COMPLETE | Org metadata |
| `users` | ✅ COMPLETE | User accounts |
| `user_sessions` | ✅ COMPLETE | Auth sessions |
| `libraries` | ✅ COMPLETE | Repository metadata |
| `fs_objects` | ✅ COMPLETE | File/directory metadata |
| `blocks` | ✅ COMPLETE | Block metadata |
| `block_id_mappings` | ✅ COMPLETE | SHA-1 → SHA-256 translation |
| `commits` | ✅ COMPLETE | Commit history |
| `starred_files` | ✅ COMPLETE | User starred items |
| `file_locks` | ✅ COMPLETE | File lock state |
| `share_links` | ✅ COMPLETE | Share link metadata |
| `library_shares` | ✅ COMPLETE | Library sharing |
| `file_shares` | ✅ COMPLETE | File/folder sharing |
| All other tables | ✅ COMPLETE | See `docs/DATABASE-GUIDE.md` |

---

## Code Freeze Rules

### 🔒 FROZEN Components - DO NOT MODIFY Without:

1. **User explicitly requests change** (documented in session)
2. **Desktop client sync breaks** (verified with `run-real-client-sync.sh`)
3. **Protocol comparison fails** (verified with `run-sync-comparison.sh`)

**Frozen Files**:
- `internal/crypto/crypto.go` - PBKDF2 implementation
- `internal/api/sync.go` lines 949-952 - fs-id-list format
- `internal/api/sync.go` lines 125-130 - commit object format
- `internal/api/sync.go` lines 500-509 - encryption fields in commit
- `internal/api/v2/encryption.go` - set-password/change-password endpoints

**Frozen Behaviors**:
- fs-id-list returns JSON array (not newline-separated)
- Commit objects omit `no_local_history` field
- `encrypted` field type: integer in download-info, string in commits
- `is_corrupted` field type: integer 0 (not boolean false)
- `Seafile-Repo-Token` header for `/seafhttp/` authentication
- pack-fs binary format: 40-byte ID + 4-byte size (BE) + zlib-compressed JSON
- PBKDF2 inputs: magic uses `repo_id+password`, random_key uses `password` only

### ✅ COMPLETE Components - Modify With Caution:

**Before modifying**:
1. Review existing tests
2. Understand current behavior
3. Add tests for new functionality
4. Check if changes affect desktop client (even if not frozen)

### 🟡 PARTIAL / ❌ TODO - Safe to Modify:

- Active development area
- No stability guarantees yet
- Experiment freely

---

## Testing Requirements for Stability Promotion

| Promotion | Requirements |
|-----------|-------------|
| ❌ TODO → 🟡 PARTIAL | Basic implementation complete, compiles |
| 🟡 PARTIAL → ✅ COMPLETE | Unit tests passing, manual testing done, documented |
| ✅ COMPLETE → 🔒 FROZEN | Desktop client tested OR protocol comparison verified, test vectors generated (if crypto) |

**Verification Commands**:
```bash
# Protocol comparison (for sync endpoints)
cd docker/seafile-cli-debug && ./run-sync-comparison.sh

# Real desktop client test (for sync endpoints)
cd docker/seafile-cli-debug && ./run-real-client-sync.sh

# Unit tests (for all components)
go test ./...
```

---

## Next Priorities (Protocol-Driven Order)

### Priority 1: Desktop Client Compatibility

1. **"View on Cloud" feature** (missing endpoint)
   - Endpoint: `GET /api/v2.1/repos/{id}/file/?p={path}` → return `view_url`
   - User impact: Right-click in desktop client → View on Cloud doesn't work
   - Status: ❌ TODO

### Priority 2: Web UI Polish

2. **Frontend modal dialog migration** (~100 files)
   - Replace `reactstrap Modal` with plain Bootstrap classes
   - See `docs/FRONTEND.md` for complete list and pattern
   - Status: 🟡 PARTIAL (15 fixed, ~100 remaining)

3. **OnlyOffice configuration tuning**
   - Fix toolbar greyed out issue
   - Test save/close cycle thoroughly
   - Status: 🟡 PARTIAL

### Priority 3: User-Facing Features

4. **Sharing system backend implementation**
   - Frontend UI complete, backend stub only
   - High user-facing priority
   - Status: 🟡 PARTIAL

5. **File operations (delete, rename, move, copy)**
   - Basic CRUD for files/folders
   - Status: 🟡 PARTIAL (stubs exist)

6. **Version history**
   - Commit history viewing
   - File revert to previous version
   - Status: ❌ TODO

### Priority 4: Authentication & Security

7. **OIDC authentication**
   - Replace dev tokens with proper OAuth/OIDC
   - Status: ❌ TODO

---

## Documentation Status

| Document | Status | Purpose |
|----------|--------|---------|
| `CURRENT_WORK.md` | ✅ COMPLETE | Session-to-session tracking (updated every session) |
| `docs/IMPLEMENTATION_STATUS.md` | ✅ COMPLETE | This file - component stability matrix |
| `docs/DECISIONS.md` | ✅ COMPLETE | Protocol-driven development workflow |
| `docs/SEAFILE-SYNC-PROTOCOL-RFC.md` | 🔒 FROZEN | Formal protocol specification with test vectors |
| `docs/SEAFILE-SYNC-PROTOCOL.md` | ✅ COMPLETE | Quick reference for developers |
| `docs/ENCRYPTION.md` | ✅ COMPLETE | Encryption implementation guide |
| `docs/FRONTEND.md` | ✅ COMPLETE | Frontend patterns, modal dialog fixes, debugging |
| `docs/ARCHITECTURE.md` | ✅ COMPLETE | Design decisions, storage architecture |
| `docs/DATABASE-GUIDE.md` | ✅ COMPLETE | Cassandra schema, examples |
| `docs/API-REFERENCE.md` | ✅ COMPLETE | API endpoints, implementation status |
| `docs/TESTING.md` | ✅ COMPLETE | Test coverage, benchmarks |
| `docs/SYNC-TESTING.md` | ✅ COMPLETE | Protocol testing with seaf-cli |
| `CLAUDE.md` | ✅ COMPLETE | AI assistant context |
| `README.md` | ✅ COMPLETE | Quick start, features |

---

## Metrics

**Last Updated**: 2026-01-16

| Metric | Value | Notes |
|--------|-------|-------|
| Sync Protocol Endpoints | 13/13 (100%) | All frozen ✅ |
| REST API Endpoints (Core) | ~30/50 (60%) | Basic CRUD complete |
| Frontend Components | ~60% complete | Modal dialogs need work |
| Desktop Client Compatibility | ✅ Working | Both tests passing |
| Test Coverage (Go) | ~40% | Need more unit tests |
| Documentation Coverage | ~90% | Most areas documented |

**Stability Breakdown**:
- 🔒 FROZEN: ~20 components (sync protocol, encryption)
- ✅ COMPLETE: ~30 components (basic features)
- 🟡 PARTIAL: ~40 components (active development)
- ❌ TODO: ~20 components (not started)
