# Testing Guide

This document describes how to run tests, test coverage, and testing infrastructure.

**Last updated: 2026-01-29**

---

## Quick Start

```bash
# Run API integration tests (default)
./scripts/test.sh

# Run specific test category
./scripts/test.sh api          # API integration tests
./scripts/test.sh go           # Go unit tests
./scripts/test.sh sync         # Sync protocol tests (requires seafile-cli)
./scripts/test.sh multiregion  # Multi-region tests (requires multi-region stack)

# Run all applicable tests
./scripts/test.sh all

# List available tests
./scripts/test.sh --list

# Quick mode (skip long-running tests)
./scripts/test.sh api --quick
```

---

## Unified Test Runner

The `./scripts/test.sh` script is the main entry point for all tests.

### Test Categories

| Category | Description | Requirements |
|----------|-------------|--------------|
| `api` | API integration tests (permissions, file ops, batch, etc.) | Backend running |
| `oidc` | OIDC authentication tests (config, login, logout, sessions) | Backend running |
| `sync` | Seafile CLI sync protocol tests | Backend + seafile-cli container |
| `multiregion` | Multi-region connectivity, routing tests | Multi-region stack |
| `failover` | Failover scenarios with large files | Multi-region + host docker |
| `go` | Go unit tests | Go 1.25+ or Docker |
| `frontend` | Frontend React tests | Node.js + npm |
| `all` | Run all applicable tests | Auto-detects available services |

### Options

| Option | Description |
|--------|-------------|
| `--quick` | Skip long-running tests (encrypted library, failover) |
| `--verbose` | Show detailed output |
| `--list` | List available tests without running |
| `--help` | Show help message |

---

## Test Categories in Detail

### 1. API Integration Tests (`api`)

Requires: Backend running (`docker compose up -d`)

```bash
./scripts/test.sh api
./scripts/test.sh api --quick  # Skip encrypted library tests
```

**Test Suites:**
| Suite | Script | Tests | Description |
|-------|--------|-------|-------------|
| Permission System | test-permissions.sh | 24 | Role hierarchy (admin > user > readonly > guest) |
| File Operations | test-file-operations.sh | 16 | Create, rename, move, copy, delete files/dirs |
| Batch Operations | test-batch-operations.sh | 19 | Batch move/copy, async tasks, error handling |
| Library Settings | test-library-settings.sh | 5 | History limit, auto-delete, API tokens |
| Encrypted Library | test-encrypted-library-security.sh | 14 | Access control, unlock flow |

**Individual Scripts:**
```bash
./scripts/test-permissions.sh
./scripts/test-file-operations.sh
./scripts/test-batch-operations.sh
./scripts/test-library-settings.sh
./scripts/test-encrypted-library-security.sh
```

### 2. OIDC Authentication Tests (`oidc`)

Requires: Backend running (`docker compose up -d`)

```bash
./scripts/test.sh oidc
./scripts/test.sh oidc --quick    # Skip tests requiring OIDC provider
./scripts/test.sh oidc --verbose  # Show request/response details
```

**Test Coverage:**
| Test Group | Tests | Description |
|------------|-------|-------------|
| Configuration | 4 | OIDC config endpoint, enabled status, secret exposure |
| Login URL | 5 | Authorization URL generation, parameters, PKCE |
| Callback | 3 | Code exchange, validation errors, JSON parsing |
| Logout URL | 4 | Single Logout URL, parameters, redirect handling |
| Session | 4 | Session info, token validation, logout |
| Trailing Slash | 4 | Endpoint compatibility with/without trailing slash |

**Individual Script:**
```bash
./scripts/test-oidc.sh
./scripts/test-oidc.sh --quick    # Skip provider-dependent tests
./scripts/test-oidc.sh --verbose  # Show detailed output
```

**Go Unit Tests (internal/auth):**
```bash
go test ./internal/auth/... -v
```

| Test File | Tests | Coverage |
|-----------|-------|----------|
| `session_test.go` | 12 | Session creation, validation, JWT, cleanup |
| `oidc_test.go` | 26 | Discovery, auth URL, state, logout, role mapping, parseIDToken (8 tests) |

### 3. Go Unit Tests (`go`)

Requires: Go 1.25+ or Docker

```bash
./scripts/test.sh go
```

**Coverage by Package (Updated 2026-01-29):**
| Package | Test Files | Coverage | Notes |
|---------|-----------|----------|-------|
| `internal/config` | 1 | 88.0% | Config loading, validation |
| `internal/chunker` | 3 | 78.7% | FastCDC + Adaptive chunking + integration |
| `internal/crypto` | 3 | 69.1% | Encryption, key derivation, Seafile compat |
| `internal/auth` | 2 | ~75% | OIDC (incl. parseIDToken), sessions, JWT |
| `internal/storage` | 4 | 46.6% | S3, blocks, SpillBuffer, manager |
| `internal/api/v2` | 17 | ~20% | REST handlers, admin, fileview, auth |
| `internal/api` | 4 | 13.0% | Sync protocol, SeafHTTP, hostname |
| `internal/middleware` | 1 | ~15% | Permission middleware (hierarchy + gin HTTP) |
| `internal/db` | 1 | 0% | Seed tests only; DB operations require Cassandra |

**Running Manually:**
```bash
# If Go is installed locally
go test ./... -short -cover

# Using Docker (if Go not installed)
docker build -t sesamefs-gotest -f - . << 'EOF'
FROM golang:1.25-alpine
RUN apk add --no-cache git
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
CMD ["go", "test", "./...", "-short", "-cover"]
EOF
docker run --rm sesamefs-gotest
```

### 4. Sync Protocol Tests (`sync`)

Requires: Backend + seafile-cli container

```bash
# Start seafile-cli container
docker compose up -d seafile-cli

# Run sync tests
./scripts/test.sh sync

# Or run directly with options
./scripts/test-sync.sh
./scripts/test-sync.sh --verbose
./scripts/test-sync.sh --keep      # Keep test libraries after
./scripts/test-sync.sh --cleanup   # Only cleanup previous tests
```

**Tests Included:**
- Unencrypted: Remote → Local sync
- Unencrypted: Multiple files sync
- Unencrypted: File modification sync
- Unencrypted: Subdirectory sync
- Unencrypted: Large file (1.5MB) sync
- Encrypted: Remote → Local sync
- Encrypted: Large file (64KB) sync
- Encrypted: Binary file sync
- Encrypted: File modification sync

### 5. Multi-Region Tests (`multiregion`)

Requires: Multi-region stack (`./scripts/bootstrap.sh multiregion`)

```bash
# Start multi-region stack
./scripts/bootstrap.sh multiregion

# Run tests
./scripts/test.sh multiregion

# Or run specific tests
./scripts/test-multiregion.sh connectivity
./scripts/test-multiregion.sh upload
./scripts/test-multiregion.sh routing
./scripts/test-multiregion.sh failover
./scripts/test-multiregion.sh all
```

**Prerequisites:**
Add to `/etc/hosts`:
```
127.0.0.1 us.sesamefs.local eu.sesamefs.local sesamefs.local
```

### 6. Failover Tests (`failover`)

Requires: Multi-region stack + host docker access (cannot run in container)

```bash
./scripts/test.sh failover

# Or run specific scenarios
./scripts/test-failover.sh setup       # Create test files
./scripts/test-failover.sh upload      # Test 1GB upload
./scripts/test-failover.sh download    # Stop server mid-download
./scripts/test-failover.sh upload-fail # Stop server mid-upload
./scripts/test-failover.sh recovery    # Verify after restart
./scripts/test-failover.sh cleanup     # Clean up
./scripts/test-failover.sh all         # All scenarios
```

**Container-Based Runner:**
```bash
./scripts/run-tests.sh multiregion all
./scripts/run-tests.sh failover all
```

### 7. Frontend Tests (`frontend`)

Requires: Node.js + npm

```bash
./scripts/test.sh frontend

# Or run directly
cd frontend
npm test                         # Watch mode
npm test -- --watchAll=false     # Single run
npm test -- --coverage           # With coverage
```

**Test Files:**
| File | Tests |
|------|-------|
| `src/models/__tests__/dirent.test.js` | 5 tests - Dirent model |
| `src/utils/__tests__/utils.test.js` | 50 tests - Utility functions |

---

## Environment Bootstrap

### Development Mode (Single Instance)

```bash
./scripts/bootstrap.sh dev
# or just
./scripts/bootstrap.sh

# With clean start
./scripts/bootstrap.sh dev --clean

# Stop
./scripts/bootstrap.sh --down

# Show status
./scripts/bootstrap.sh --status
```

**Services:**
- SesameFS: http://localhost:8082
- MinIO Console: http://localhost:9001 (minioadmin/minioadmin)

### Multi-Region Mode

```bash
./scripts/bootstrap.sh multiregion

# With clean start
./scripts/bootstrap.sh multiregion --clean

# Stop
./scripts/bootstrap.sh multiregion --down
```

**Services:**
- Load Balancer: http://localhost:8082
- USA Endpoint: http://us.sesamefs.local:8080
- EU Endpoint: http://eu.sesamefs.local:8080
- MinIO Console: http://localhost:9001

---

## Test Scripts Reference

| Script | Purpose | Requirements |
|--------|---------|--------------|
| `test.sh` | **Unified test runner** | Varies by category |
| `test-permissions.sh` | Permission system tests | Backend |
| `test-file-operations.sh` | File/dir CRUD tests | Backend |
| `test-batch-operations.sh` | Batch move/copy tests | Backend |
| `test-library-settings.sh` | Library settings API | Backend |
| `test-encrypted-library-security.sh` | Encrypted lib access | Backend |
| `test-sync.sh` | Seafile sync protocol | Backend + seafile-cli |
| `test-multiregion.sh` | Multi-region tests | Multi-region stack |
| `test-failover.sh` | Failover scenarios | Multi-region + host docker |
| `run-tests.sh` | Container-based runner | Multi-region stack |
| `bootstrap.sh` | Environment setup | Docker |
| `bootstrap-multiregion.sh` | Legacy multi-region setup | Docker |

---

## Benchmarks

### FastCDC Chunking Performance

| Benchmark | Throughput | Notes |
|-----------|------------|-------|
| `BenchmarkFastCDC_ChunkAll` | **45.87 MB/s** | 256MB file, 16MB chunks |
| `BenchmarkFastCDC_2MB_Chunks` | **48.77 MB/s** | 256MB file, 2MB chunks |
| `BenchmarkFastCDC_16MB_Chunks` | **59.68 MB/s** | 256MB file, 16MB chunks |

### Running Benchmarks

```bash
go test -bench=. -benchtime=3s ./internal/chunker/
go test -bench=. -benchmem ./internal/chunker/
```

---

## Test Infrastructure

### Authentication Tokens

| Token | User Role | Use Case |
|-------|-----------|----------|
| `dev-token-admin` | Admin | Full access |
| `dev-token-user` | User | Standard access |
| `dev-token-readonly` | Readonly | Read-only access |
| `dev-token-123` | Default dev | Legacy tests |

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SESAMEFS_URL` | `http://localhost:8082` | Backend URL (host-mapped port) |
| `DEV_TOKEN` | dev-token-123 | Auth token |
| `CLI_CONTAINER` | cool-storage-api-seafile-cli-1 | Seafile CLI container |
| `ENCRYPTED_PASSWORD` | testpass123 | Encrypted library password |

---

## Mock Implementations

### TokenStore Interface
```go
type TokenStore interface {
    CreateUploadToken(orgID, repoID, path, userID string) string
    CreateDownloadToken(orgID, repoID, path, userID string) string
    GetToken(token string, tokenType string) (*TokenInfo, error)
    DeleteToken(token string) error
}
```
Has: `TokenManager` (in-memory) and `MockTokenStore` (for tests)

### Store Interface
```go
type Store interface {
    Put(ctx context.Context, blockID string, data io.Reader, size int64) (string, error)
    Get(ctx context.Context, storageKey string) (io.ReadCloser, error)
    Delete(ctx context.Context, storageKey string) error
    Exists(ctx context.Context, storageKey string) (bool, error)
}
```
Has: `mockStore` (in `manager_test.go`)

---

## Known Issues

### Tests Requiring Database

Some tests are skipped because they require a real database connection:
- `TestHandleAccountInfo` - Needs DB session
- `TestAccountInfoTotalSpace` - Needs DB session
- `TestCreateShare` - Needs DB session

These are tested via integration tests instead.

### Tests Updated in 2026-01-29 (Session 10)

- Rewrote `admin_test.go` — replaced logic-reimplementation tests with real gin HTTP handler tests
- Added middleware gin handler tests to `permissions_test.go` — RequireAuth, RequireSuperAdmin, RequireOrgRole
- Added 8 `parseIDToken` direct tests to `oidc_test.go` — valid/expired/issuer/nonce/format/custom claims
- Fixed pre-existing compile errors in `fileview_test.go` — `h.fileViewAuthMiddleware()` → `fileViewAuthWrapper()`
- Fixed `TestRegisterFileViewRoutes` — passed real auth middleware instead of nil
- Fixed all test scripts to use port 8082 (host-mapped port)
- Fixed `test.sh` nested folders invocation (script name vs args split)
- Removed legacy `test-all.sh` (replaced by unified `test.sh`)

### Tests Updated in 2026-01-28

- Fixed `NewSeafHTTPHandler` test signature (added `permMiddleware` parameter)
- Fixed `middleware.Permission` → `middleware.LibraryPermission` type
- Fixed test scripts to use unique library names (prevents 409 conflicts)
- Created unified test runner (`test.sh`)

---

## Test Coverage Improvement Plan

**Last Updated**: 2026-01-29

### Current State

37 test files across 9 packages. Coverage is strong in crypto/chunker/config/auth but weak in API handlers and middleware.

### Pre-Existing Test Failures (Not Regressions)

These 4 tests in `internal/api/v2/` fail due to nil-pointer dereferences in tests that don't set up required dependencies:
- `TestGetSessionInfo` — `auth_test.go` creates `SessionManager` with nil config
- `TestOnlyOfficeEditorHTML` — `fileview_test.go` tests template rendering with nil config fields
- `TestOnlyOfficeEditorHTMLWithoutToken` — same
- `TestOnlyOfficeEditorHTMLCustomizations` — same

These need either mock configs or `gin.Recovery()` middleware added to the test routers.

### Priority 1: Untested Handler Files (High-Value Gaps)

| File | Lines | What's Missing | Difficulty |
|------|-------|---------------|------------|
| `api/v2/batch_operations.go` | 457 | SyncBatchMove, SyncBatchCopy, AsyncBatch*, GetTaskProgress | Medium — needs DB mock or gin context setup |
| `api/v2/library_settings.go` | 434 | History limit, auto-delete, API tokens, transfer | Medium — CRUD handlers with DB dependency |
| `api/v2/restore.go` | 233 | InitiateRestore, GetRestoreStatus, ListRestoreJobs | Medium — S3 restore lifecycle |
| `api/v2/search.go` | 186 | Search libraries/files by name | Easy — input validation, query building |
| `api/v2/blocks.go` | 278 | CheckBlocks, UploadBlock, DownloadBlock | Medium — storage layer integration |
| `middleware/audit.go` | 150 | LogAudit, AuditMiddleware | Easy — test log output format |
| `db/tokens.go` | 170 | TokenStore CRUD | Hard — requires Cassandra |

**Recommended approach**: Test input validation, JSON binding, and error paths (no DB needed). Use `gin.Recovery()` + nil DB to verify code reaches the DB call without crashing early.

### Priority 2: Partially Tested Files (Missing Handler Coverage)

| File | What's Tested | What's Missing |
|------|--------------|----------------|
| `api/v2/files.go` (3060 lines) | Batch, CRUD, lock | UploadFile, GetDownloadLink, CopyFile, MoveFile, GetFileRevisions |
| `api/sync.go` | Data structures, protocol format | Handler functions: GetHeadCommit, PutCommit, GetBlock, PutBlock, PackFS, RecvFS |
| `api/v2/libraries.go` (1085 lines) | Permission checks, list | CreateLibrary end-to-end, UpdateLibrary, DeleteLibrary |

### Priority 3: Infrastructure Improvements

| Improvement | Impact | Effort |
|------------|--------|--------|
| **DB interface mock** | Unlocks unit tests for all handlers with DB deps | High — define interface, implement mock, refactor handlers |
| **Fix 4 pre-existing test failures** | Clean CI output | Low — add nil checks or mock configs |
| **Test containers (testcontainers-go)** | Real DB integration tests in CI | Medium — Docker-in-Docker setup |
| **Frontend E2E tests (Playwright)** | Full UI workflow coverage | High — framework setup + test authoring |
| **Frontend component tests** | Modal dialogs, share components | Medium — need to resolve @testing-library/react ESM issues |

### Quick Wins (Can Do Without DB)

These tests can be written today using gin test contexts with no database:

1. **`search.go`** — test input validation (empty query, missing params → 400)
2. **`audit.go`** — test middleware sets audit context values
3. **`batch_operations.go`** — test JSON binding validation (missing fields → 400)
4. **`library_settings.go`** — test owner-only middleware rejection (non-owner → 403)
5. **`restore.go`** — test missing repo_id params → 400
6. **Fix 4 pre-existing test failures** — add `gin.Recovery()` or nil-safe config to existing tests
