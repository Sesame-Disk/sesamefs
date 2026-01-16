# Current Work - SesameFS

**Last Updated**: 2026-01-16 (Session b3b3a1ec)
**Last Worked By**: Claude Sonnet 4.5

---

## What Was Just Completed ✅

### Seafile Sync Protocol Fixed (2026-01-16)
- ✅ Fixed fs-id-list endpoint to return JSON array (was incorrectly returning newline-separated text)
- ✅ Removed `no_local_history` field from commit objects (stock Seafile doesn't include it)
- ✅ Fixed `repo_desc` and `repo_category` to be empty strings `""` (not null)
- ✅ Fixed `is_corrupted` to be integer `0` (not boolean `false`)
- ✅ Created automated protocol comparison test (`./run-sync-comparison.sh`)
- ✅ Created real desktop client sync test (`./run-real-client-sync.sh`)
- ✅ Both tests passing - protocol matches stock Seafile exactly

### RFC Specification Created (2026-01-16)
- ✅ Created formal RFC-style specification (docs/SEAFILE-SYNC-PROTOCOL-RFC.md)
- ✅ Generated and verified PBKDF2 test vectors
- ✅ Generated and verified FS ID computation test vectors
- ✅ Complete technical specification suitable for independent implementations

### Documentation Cleanup (2026-01-16)
- ✅ Reduced SEAFILE-SYNC-PROTOCOL.md from 3,299 lines to 433 lines (87% reduction)
- ✅ Removed speculative/unverified content
- ✅ Kept only verified, essential information

---

## What's Next (Priority Order) 🎯

### 1. Documentation Structure Setup (IN PROGRESS)
- 🔄 Create IMPLEMENTATION_STATUS.md (component stability matrix)
- 🔄 Create DECISIONS.md (protocol-driven workflow)
- 🔄 Update CLAUDE.md to reference new structure
- 🔄 Clean up legacy documentation files

### 2. Desktop Client "View on Cloud" Feature
**Status**: Not implemented
**Priority**: Medium
**User Impact**: Desktop client right-click → "View on Cloud" doesn't work

**What needs to be done**:
- Implement `GET /api/v2.1/repos/{repo_id}/file/?p={path}` endpoint
- Should return `view_url` field pointing to web UI file viewer
- Test with Seafile desktop client

**Files to modify**:
- `internal/api/v2/files.go` - Add new endpoint

### 3. Frontend Modal Dialog Migration
**Status**: ~100 files need fixing
**Priority**: Low (doesn't affect desktop clients)
**User Impact**: Some frontend dialogs don't render properly

**What needs to be done**:
- Replace reactstrap Modal with plain Bootstrap modal classes
- See CLAUDE.md for correct pattern
- Test each dialog after conversion

**Files to fix**: See `docs/FRONTEND.md` for complete list

### 4. OnlyOffice Configuration Tuning
**Status**: Opens files but toolbar sometimes greyed out
**Priority**: Medium
**User Impact**: Document editing works but UX could be better

**What needs to be done**:
- Review OnlyOffice config in `internal/api/v2/onlyoffice.go`
- Test save/close cycle thoroughly
- Verify callbacks work correctly

---

## Known Issues 🐛

### Critical (Affects Desktop Clients)
- None currently - sync protocol working correctly

### High (Affects Web Users)
- Desktop client "View on Cloud" not working (missing endpoint)

### Medium (UI/UX Issues)
- ~100 frontend modal dialogs need reactstrap Modal fix
- OnlyOffice toolbar sometimes greyed out
- Some frontend icons loading slowly (potential CDN vs local issue)

### Low (Future Enhancements)
- Sharing system backend incomplete (UI complete, backend stub only)
- Version history not implemented
- OIDC authentication not implemented (dev tokens only)

---

## Frozen/Stable Components 🔒

**DO NOT MODIFY these without explicit user request or desktop client breakage:**

### Code Files
- `internal/crypto/crypto.go` - PBKDF2 implementation verified against stock Seafile
- `internal/api/sync.go` lines 949-952 - fs-id-list format (JSON array)
- `internal/api/sync.go` lines 125-130 - commit object format (no `no_local_history`)
- `internal/api/v2/encryption.go` - set-password/change-password endpoints

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
- `docs/SEAFILE-SYNC-PROTOCOL-RFC.md` - Added verified test vectors (Section 11)
- (Previous session: `internal/api/sync.go` - Fixed fs-id-list and commit formats)

### Testing Locations
- Protocol comparison: `docker/seafile-cli-debug/run-sync-comparison.sh`
- Real client test: `docker/seafile-cli-debug/run-real-client-sync.sh`
- Test vector generation: `docker/seafile-cli-debug/scripts/generate_test_vectors.py`

### Reference Servers
- **Stock Seafile** (protocol reference): https://app.nihaoconsult.com
  - Credentials: See `.seafile-reference.md`
  - Use for protocol comparison testing
- **Local dev**: http://localhost:8080
  - Test implementation

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
