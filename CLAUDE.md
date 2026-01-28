# SesameFS - Project Context for Claude

**📏 File Size Rule**: Keep this file under **500 lines**. Move detailed content to specific documentation files and reference them with links.

## What is SesameFS?

A Seafile-compatible cloud storage API with modern internals (Go, Cassandra, S3).

---

## 🔥 READ THESE FIRST - Session Continuity

### If Starting New Session (Read in 5 minutes)

**Step 1:** Read [CURRENT_WORK.md](CURRENT_WORK.md) - See "🚀 NEW SESSION? START HERE" box
- What was completed last session
- What to work on next (priority order)
- What's frozen (DO NOT TOUCH)

**Step 2:** Check [docs/IMPLEMENTATION_STATUS.md](docs/IMPLEMENTATION_STATUS.md) before changing ANY code
- 🔒 FROZEN = Desktop client breaks if modified
- ✅ COMPLETE = Modify with caution
- 🟡 PARTIAL = Safe to develop
- ❌ TODO = Greenfield

**Step 3:** When modifying sync protocol, follow [docs/DECISIONS.md](docs/DECISIONS.md) workflow
- Stock Seafile (app.nihaoconsult.com) is the reference
- Test with `./run-sync-comparison.sh` and `./run-real-client-sync.sh`
- Freeze only after both tests pass

**Step 4:** Before implementing ANY new API endpoint:
- ⚠️ **ALWAYS check [docs/ENDPOINT-REGISTRY.md](docs/ENDPOINT-REGISTRY.md)** first
- Run: `grep -r "route-pattern" internal/api` to verify route doesn't exist
- If route exists: Modify existing handler (DON'T create duplicate)
- If route is new: Implement AND add to registry

### Critical Rules (Never Violate)

**🔒 DO NOT MODIFY without explicit user approval**:
- `internal/crypto/crypto.go` (PBKDF2 implementation)
- `internal/api/sync.go` lines 949-952 (fs-id-list format)
- `internal/api/sync.go` lines 125-130, 500-509 (commit object format)
- Any endpoint marked 🔒 FROZEN in IMPLEMENTATION_STATUS.md

**⚠️ Protocol Requirements (Stock Seafile is reference)**:
- fs-id-list returns JSON array (NOT newline-separated text)
- Commit objects OMIT `no_local_history` field
- `encrypted` type: integer in download-info, string in commits
- `is_corrupted` type: integer 0 (NOT boolean false)
- `/seafhttp/` auth: `Seafile-Repo-Token` header (NOT `Authorization`)

**📝 During Session (IMPORTANT)**:
- **Log user-reported issues immediately** to `docs/KNOWN_ISSUES.md`
- When user reports a bug mid-conversation, add it to KNOWN_ISSUES even before fixing
- This ensures issues aren't forgotten if session is interrupted

**📝 End of Session (MANDATORY)**:
- **Run [docs/SESSION_CHECKLIST.md](docs/SESSION_CHECKLIST.md)** - Complete documentation update checklist
- Update `CURRENT_WORK.md` (move completed items, update priorities, list files modified)
  - **CRITICAL**: Keep `CURRENT_WORK.md` under **500 lines** unless unavoidable
  - **CRITICAL**: Keep `CLAUDE.md` under **500 lines** (current: 300 lines)
  - Move detailed content to: `docs/KNOWN_ISSUES.md`, `docs/CHANGELOG.md`, other appropriate docs
- Update all "Last Verified: YYYY-MM-DD" dates to current date

---

## Critical Constraints

1. **Seafile desktop/mobile client chunking cannot be changed** - compiled into apps (Rabin CDC, 256KB-4MB, SHA-1)
2. **SHA-1→SHA-256 translation for sync protocol only** - Desktop/mobile clients use `/seafhttp/` with SHA-1 block IDs; server translates to SHA-256 for storage. Web frontend uses REST API with server-side SHA-256 chunking.
3. **Block size for web/API**: 2-256MB (server-controlled, adaptive FastCDC)
4. **SpillBuffer threshold**: 16MB (memory below, temp file above)
5. **Encryption: Weak→Strong translation** - Seafile clients use weak PBKDF2 (1K iterations); we validate with PBKDF2 for compat but store Argon2id for security. Server-side envelope encryption adds protection layer.
6. **Desktop vs Web endpoints** - Desktop clients ONLY use `/seafhttp/` + `/api2/repos/` (library CRUD). Groups, sharing, settings are WEB UI ONLY and can be designed freely (we match Seafile for convenience).

### Upload Paths

| Client | Protocol | Chunking | Block Hash |
|--------|----------|----------|------------|
| Desktop/Mobile | `/seafhttp/` (sync) | Client-side Rabin CDC | SHA-1 → translated to SHA-256 |
| Web Frontend | REST API | Server-side FastCDC | SHA-256 (no translation) |
| API clients | REST API | Server-side FastCDC | SHA-256 (no translation) |

---

## Key Code Locations

| Feature | File |
|---------|------|
| Seafile sync protocol | `internal/api/sync.go` |
| File upload/download | `internal/api/seafhttp.go` |
| S3 storage backend | `internal/storage/s3.go` |
| Block storage | `internal/storage/blocks.go` |
| Multi-backend manager | `internal/storage/storage.go` |
| FastCDC chunking | `internal/chunker/fastcdc.go` |
| Adaptive chunking | `internal/chunker/adaptive.go` |
| Database schema | `internal/db/db.go` |
| API v2 handlers | `internal/api/v2/*.go` |
| Configuration | `internal/config/config.go` |
| Encryption/Key derivation | `internal/crypto/crypto.go` |
| Library password endpoints | `internal/api/v2/encryption.go` |
| Groups management | `internal/api/v2/groups.go` |
| File/folder sharing | `internal/api/v2/file_shares.go`, `internal/api/v2/libraries.go:122-129` |
| Share links (public URLs) | `internal/api/v2/share_links.go` |
| Permission middleware | `internal/middleware/permissions.go` (built, not integrated) |

---

## Documentation Structure

### 📋 Session Continuity (🔥 Read First)

| Document | Purpose | Update Frequency |
|----------|---------|------------------|
| [CURRENT_WORK.md](CURRENT_WORK.md) | Session state, priorities, frozen components (📏 KEEP UNDER 500 LINES) | **Every session** |
| [docs/KNOWN_ISSUES.md](docs/KNOWN_ISSUES.md) | Detailed bug tracking (regressions, critical bugs, UX issues) | **When bugs discovered/fixed** |
| [docs/CHANGELOG.md](docs/CHANGELOG.md) | Session-by-session development history | **Every session** |
| [docs/SESSION_CHECKLIST.md](docs/SESSION_CHECKLIST.md) | End-of-session documentation update checklist | **Run at end of every session** |
| [docs/IMPLEMENTATION_STATUS.md](docs/IMPLEMENTATION_STATUS.md) | Component stability matrix (frozen/complete/partial/todo) | Weekly or after major changes |
| [docs/DECISIONS.md](docs/DECISIONS.md) | Protocol-driven workflow, architecture decisions | When decisions made |

### 🔒 Protocol & Sync Reference

| Document | Purpose |
|----------|---------|
| [docs/SEAFILE-SYNC-PROTOCOL-RFC.md](docs/SEAFILE-SYNC-PROTOCOL-RFC.md) | Formal RFC specification with test vectors (🔒 FROZEN) |
| [docs/SEAFILE-SYNC-PROTOCOL.md](docs/SEAFILE-SYNC-PROTOCOL.md) | Quick reference for developers, debugging guide |
| [docs/SYNC-TESTING.md](docs/SYNC-TESTING.md) | Protocol testing with seaf-cli, client debugging, error messages |
| [docker/seafile-cli-debug/COMPREHENSIVE_TESTING.md](docker/seafile-cli-debug/COMPREHENSIVE_TESTING.md) | Comprehensive test framework (7 scenarios, mitmproxy) |

### 🔐 Security & Encryption

| Document | Purpose |
|----------|---------|
| [docs/ENCRYPTION.md](docs/ENCRYPTION.md) | Encrypted libraries, PBKDF2, Argon2id, block ID mapping, decrypt sessions |
| [docs/OIDC.md](docs/OIDC.md) | **OIDC authentication** - Provider details, implementation plan, config examples |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Design decisions, storage architecture, SHA-1→SHA-256 translation |

### 🎨 Frontend Development

| Document | Purpose |
|----------|---------|
| [docs/FRONTEND.md](docs/FRONTEND.md) | **Complete frontend guide** - setup, patterns, modal fixes, debugging, browser cache issues |
| [docs/TECHNICAL-DEBT.md](docs/TECHNICAL-DEBT.md) | Pending modal dialog fixes (~100+ files need migration) |

### 🛠️ Implementation Guides

| Document | Purpose |
|----------|---------|
| [README.md](README.md) | Quick start, features overview |
| [docs/API-REFERENCE.md](docs/API-REFERENCE.md) | API endpoints, implementation status |
| [docs/ENDPOINT-REGISTRY.md](docs/ENDPOINT-REGISTRY.md) | **⚠️ CHECK BEFORE ADDING ENDPOINTS** - Complete route registry to prevent conflicts |
| [docs/DATABASE-GUIDE.md](docs/DATABASE-GUIDE.md) | Cassandra tables, queries, examples |
| [docs/FILE-INTEGRITY-VERIFICATION.md](docs/FILE-INTEGRITY-VERIFICATION.md) | File integrity & checksum verification for chunked uploads |
| [docs/TESTING.md](docs/TESTING.md) | Test coverage, benchmarks |
| [docs/LICENSING.md](docs/LICENSING.md) | Legal considerations for Seafile compatibility |

---

## External References

| Resource | URL |
|----------|-----|
| Seafile API Docs (New) | https://seafile-api.readme.io/ |
| Seafile Manual - API Index | https://manual.seafile.com/latest/develop/web_api_v2.1/ |
| Seafile Server Source (upload-file.c) | https://github.com/haiwen/seafile-server/blob/master/server/upload-file.c |
| seafile-js (frontend API client) | https://github.com/haiwen/seafile-js |
| Seafile Client (resumable upload) | https://github.com/haiwen/seafile-client/blob/master/src/filebrowser/reliable-upload.cpp |
| Seafile Daemon (sync logic) | https://github.com/haiwen/seafile - `daemon/sync-mgr.c`, `daemon/http-tx-mgr.c` |

---

## Quick Commands

```bash
# Run tests
go test ./...

# Run with coverage
go test ./... -coverprofile=coverage.out

# Start dev server
go run cmd/sesamefs/main.go

# Docker compose
docker-compose up -d

# Frontend development
cd frontend && npm install && npm start  # runs on port 3001

# Test sync protocol
./run-sync-comparison.sh
./run-real-client-sync.sh
```

---

## Critical Technical Context

### Block ID Mapping (SHA-1 → SHA-256)

**Quick Reference**: Seafile clients use SHA-1 (40 chars), we store SHA-256 (64 chars) for security.

**Database Table**: `block_id_mappings (org_id, external_id, internal_id, created_at)`
- `external_id`: SHA-1 (what Seafile clients use)
- `internal_id`: SHA-256 (how we store blocks)

**Code Locations**:
- Upload mapping: `internal/api/seafhttp.go`, `internal/api/v2/onlyoffice.go`
- Download mapping: `internal/api/sync.go` → `GetBlock()`

**Full details**: See [docs/ENCRYPTION.md](docs/ENCRYPTION.md) and [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)

### Encrypted Library Flow

**Quick Reference**:
- File keys stored in memory (1-hour TTL)
- Encryption: AES-256-CBC with `[16-byte IV][encrypted content + PKCS7 padding]`
- PBKDF2 for Seafile compatibility, Argon2id for web clients
- Two PBKDF2 calls: key (1000 iterations) → IV (10 iterations, using key as input)

**Code Locations**:
- Decrypt sessions: `internal/api/v2/encryption.go`
- Crypto functions: `internal/crypto/crypto.go`
- Upload flow: `internal/api/seafhttp.go`, `internal/api/v2/onlyoffice.go`

**Full details**: See [docs/ENCRYPTION.md](docs/ENCRYPTION.md)

### Frontend Architecture

**Quick Reference**:
- **Modal Pattern**: Use plain Bootstrap modal classes (NOT reactstrap Modal inside ModalPortal)
- **Browser Cache**: Always test with standalone HTML first before claiming a fix doesn't work
- **API Client**: `seafile-js` has hardcoded paths - backend must match
- **Config**: `window.app.config` in `public/index.html` (serviceURL empty = multi-host support)
- **Token Auth**: `Authorization: Token xyz` (NOT Bearer)

**Code Locations**:
- API client: `frontend/src/utils/seafile-api.js`
- Config: `frontend/public/index.html` → `window.app.config`
- Dialogs: `frontend/src/components/dialog/`

**Full details**: See [docs/FRONTEND.md](docs/FRONTEND.md) - Complete guide with patterns, debugging, cache issues

### Sync Protocol Debugging

**Quick Reference**:
- Client logs: `~/.ccnet/logs/seafile.log` (macOS/Linux), `C:\Users\<username>\ccnet\logs\seafile.log` (Windows)
- Common errors: "Failed to inflate" = fs object not zlib compressed, "Failed to find dir" = fs_id missing from fs-id-list
- Force fresh sync: Delete `~/Seafile/.seafile-data/storage/{commits,fs}/<repo_id>` + reset local-head in repo.db

**Full details**: See [docs/SYNC-TESTING.md](docs/SYNC-TESTING.md) and [docs/SEAFILE-SYNC-PROTOCOL.md](docs/SEAFILE-SYNC-PROTOCOL.md)

---

## Common Gotchas

### Protocol Requirements (ALWAYS Follow)

| Requirement | Wrong ❌ | Correct ✅ |
|-------------|---------|-----------|
| fs-id-list format | Newline-separated text | JSON array `["id1", "id2"]` |
| Commit field | Include `no_local_history` | Omit `no_local_history` |
| `is_corrupted` type | `false` (boolean) | `0` (integer) |
| `encrypted` type (download-info) | String | Integer `1` or `0` |
| `/seafhttp/` auth header | `Authorization: Token xyz` | `Seafile-Repo-Token: xyz` |
| pack-fs format | Uncompressed JSON | `[40-byte ID][4-byte size BE][zlib JSON]` |
| fs_id computation | Unsorted keys | SHA-1 of JSON with **alphabetically sorted keys** |

### Frontend Common Errors

| Error | Cause | Solution |
|-------|-------|----------|
| Modal not visible | reactstrap Modal inside ModalPortal | Use plain Bootstrap modal classes |
| Close button shows □ | Browser cache serving old JS | Hard refresh (Cmd+Shift+R), or test with standalone HTML first |
| Icons not loading (404) | Missing icon files or wrong path | Check `frontend/public/static/img/` structure |
| API call fails | Wrong response format | Compare with [docs/API-REFERENCE.md](docs/API-REFERENCE.md) |

**Full debugging guide**: See [docs/FRONTEND.md](docs/FRONTEND.md) → "Browser Cache Issues & Testing Methodology"

---

## Where to Find Information

| Need to... | Check... |
|------------|----------|
| Start a new session | [CURRENT_WORK.md](CURRENT_WORK.md) → "🚀 NEW SESSION? START HERE" |
| Understand a sync error | [docs/SYNC-TESTING.md](docs/SYNC-TESTING.md) → "Common Error Messages" |
| Debug encrypted libraries | [docs/ENCRYPTION.md](docs/ENCRYPTION.md) → Full flow diagrams |
| Implement OIDC auth | [docs/OIDC.md](docs/OIDC.md) → Provider details, implementation plan |
| Verify file integrity/checksums | [docs/FILE-INTEGRITY-VERIFICATION.md](docs/FILE-INTEGRITY-VERIFICATION.md) → Complete guide with test results |
| Fix a frontend modal | [docs/FRONTEND.md](docs/FRONTEND.md) → "Modal Pattern" |
| Add a new API endpoint | [docs/ENDPOINT-REGISTRY.md](docs/ENDPOINT-REGISTRY.md) + [docs/API-REFERENCE.md](docs/API-REFERENCE.md) |
| Understand database schema | [docs/DATABASE-GUIDE.md](docs/DATABASE-GUIDE.md) |
| See what changed recently | [docs/CHANGELOG.md](docs/CHANGELOG.md) |
| Check for known bugs | [docs/KNOWN_ISSUES.md](docs/KNOWN_ISSUES.md) |
| Understand design decisions | [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) + [docs/DECISIONS.md](docs/DECISIONS.md) |

---

**Remember**: This file is kept intentionally brief. For detailed information, follow the links to specific documentation files above. When in doubt, check:
1. [CURRENT_WORK.md](CURRENT_WORK.md) - What's happening now
2. [docs/IMPLEMENTATION_STATUS.md](docs/IMPLEMENTATION_STATUS.md) - What's frozen vs safe to change
3. The specific docs linked above for your topic
