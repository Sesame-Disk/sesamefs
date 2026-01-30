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

**Coverage by Package (Updated 2026-01-29, Session 11):**
| Package | Test Files | Coverage | Notes |
|---------|-----------|----------|-------|
| `internal/config` | 1 | 88.0% | Config loading, validation |
| `internal/chunker` | 3 | 78.7% | FastCDC + Adaptive chunking + integration |
| `internal/crypto` | 3 | 69.1% | Encryption, key derivation, Seafile compat |
| `internal/auth` | 2 | ~75% | OIDC (incl. parseIDToken), sessions, JWT |
| `internal/storage` | 4 | 46.6% | S3, blocks, SpillBuffer, manager |
| `internal/api/v2` | 23 | ~35% | REST handlers + 6 new test files (search, batch, blocks, restore, library settings) |
| `internal/api` | 4 | 13.0% | Sync protocol, SeafHTTP, hostname |
| `internal/middleware` | 2 | ~30% | Permission middleware + audit middleware |
| `internal/gc` | 4 | ~40% | GC service, queue, worker, scanner (unit + integration stubs) |
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

## Garbage Collection (GC) Tests

### Overview

The GC system has 7 test files covering the service orchestrator, queue, worker, scanner, adapters, and hooks.

```bash
# Run all GC tests
go test ./internal/gc/... -v

# Run adapter tests
go test ./internal/api/... -v -run TestGC

# Run hooks tests
go test ./internal/api/v2/... -v -run TestGC
go test ./internal/api/v2/... -v -run TestSetGCHooks
```

### Test Files

| File | Tests | Type | What's Tested |
|------|-------|------|---------------|
| `internal/gc/gc_test.go` | 10 | Unit | Stats (atomic counters, concurrency), GCStatus formatting, Service creation, config propagation, SetDryRun, disabled service, trigger channels |
| `internal/gc/queue_test.go` | 10 | Unit + Integration stubs | ItemType constants, QueueItem fields, NewQueue, 5 integration stubs (enqueue, grace period, retry, list orgs, queue size) |
| `internal/gc/worker_test.go` | 10 | Unit + Integration stubs | NewWorker, dry run config, type conversion, nil UUID, 7 integration stubs (block ref_count, dry run, fs_object cascade, commit, retry, library contents, empty queue) |
| `internal/gc/scanner_test.go` | 7 | Unit + Integration stubs | NewScanner, 6 integration stubs (orphaned blocks, expired share links, orphaned commits/fs_objects, full scan, empty DB) |
| `internal/api/gc_adapter_test.go` | 7 | Unit | Invalid UUIDs, empty inputs, interface compliance, nil service, config defaults |
| `internal/api/v2/gc_hooks_test.go` | 8 | Unit | Set/get hooks, nil defaults, concurrent access, interface compile-time check, mock call recording |

### Unit Tests (Always Run)

These tests do not require a database and run in standard `go test`:

- **Stats concurrency**: 100 goroutines incrementing `BlocksDeleted` atomically
- **Service lifecycle**: Create, configure, trigger channels (non-blocking), disabled mode
- **Config propagation**: `DryRun`, `BatchSize`, `GracePeriod` flow from config to worker
- **Hooks thread safety**: Concurrent `SetGCHooks` + `getBlockEnqueuer` calls
- **Adapter UUID validation**: Invalid UUIDs log errors instead of panicking

### Integration Tests (Require Cassandra + S3)

All integration tests are marked with `t.Skip("requires Cassandra database connection")` and will be skipped in standard test runs. Each stub documents exactly what it would test:

| Test | Scenario |
|------|----------|
| `Queue_Enqueue_Integration` | Enqueue → DequeueBatch → Complete → empty |
| `Queue_DequeueBatch_GracePeriod_Integration` | Grace period filtering (1h vs 0s) |
| `Queue_IncrementRetry_Integration` | Retry counter increments correctly |
| `Worker_ProcessBlock_RefCountPositive_Integration` | Block with ref_count>0 is spared |
| `Worker_ProcessBlock_RefCountZero_Integration` | Block with ref_count=0 deleted from S3+DB |
| `Worker_ProcessBlock_DryRun_Integration` | Dry run mode doesn't delete |
| `Worker_ProcessFSObject_CascadeBlocks_Integration` | FS object deletion cascades to block enqueue |
| `Worker_EnqueueLibraryContents_Integration` | Library deletion enqueues all commits+fs_objects+blocks |
| `Scanner_ScanOrphanedBlocks_Integration` | Finds blocks with ref_count<=0 |
| `Scanner_ScanExpiredShareLinks_Integration` | Finds share links past expiry |
| `Scanner_ScanOrphanedCommits_Integration` | Finds commits for deleted libraries |
| `Scanner_ScanOrphanedFSObjects_Integration` | Finds fs_objects for deleted libraries |

### Running Integration Tests

When a Cassandra and S3 environment is available:

```bash
# Start services
docker compose up -d cassandra minio

# Run with integration tests enabled (remove t.Skip lines)
go test ./internal/gc/... -v -count=1

# Or use the API integration test scripts
./scripts/test.sh api
```

### Manual GC Verification

After deploying, verify GC via the admin API:

```bash
# Check GC status
curl -H "Authorization: Token dev-token-admin" \
  http://localhost:8082/api/v2.1/admin/gc/status

# Trigger worker run
curl -X POST -H "Authorization: Token dev-token-admin" \
  -H "Content-Type: application/json" \
  -d '{"type":"worker"}' \
  http://localhost:8082/api/v2.1/admin/gc/run

# Trigger scanner run (dry run)
curl -X POST -H "Authorization: Token dev-token-admin" \
  -H "Content-Type: application/json" \
  -d '{"type":"scanner","dry_run":true}' \
  http://localhost:8082/api/v2.1/admin/gc/run
```

### End-to-End GC Test Scenario

Manual test flow to verify the full GC pipeline:

1. **Upload** a file to a library
2. **Verify** block exists in S3 (`blocks/` prefix)
3. **Delete** the file via API
4. **Check** `gc_queue` has a block entry (after ref_count hits 0)
5. **Wait** for grace period (1h default, or trigger worker manually)
6. **Verify** block deleted from S3 and `blocks` table
7. **Delete** the library
8. **Verify** all commits and fs_objects enqueued and eventually cleaned up

---

## Known Issues

### Tests Requiring Database

Some tests are skipped because they require a real database connection:
- `TestHandleAccountInfo` - Needs DB session (unconditional skip)
- `TestAccountInfoTotalSpace` - Needs DB session (unconditional skip)
- `TestCreateShare_Integration` - Needs DB for encrypted library check (unconditional skip)
- `TestCLIChunkingDemo` - Manual demo requiring `CHUNKING_DEMO=1` env var
- `TestQueue_*_Integration` - GC queue operations require Cassandra
- `TestWorker_*_Integration` - GC worker processing requires Cassandra + S3
- `TestScanner_*_Integration` - GC scanner phases require Cassandra

These are tested via integration tests instead.

### Tests Updated in 2026-01-30 (GC Implementation)

- **New: `internal/gc/gc_test.go`** — 10 tests for GC service (Stats, lifecycle, config, dry run, triggers)
- **New: `internal/gc/queue_test.go`** — 10 tests for GC queue (item types, fields, integration stubs)
- **New: `internal/gc/worker_test.go`** — 10 tests for GC worker (creation, type conversion, integration stubs)
- **New: `internal/gc/scanner_test.go`** — 7 tests for GC scanner (creation, integration stubs)
- **New: `internal/api/gc_adapter_test.go`** — 7 tests for GC adapters (UUID validation, interfaces, nil safety)
- **New: `internal/api/v2/gc_hooks_test.go`** — 8 tests for GC hooks (set/get, thread safety, mock recording)
- **Updated: `docs/TESTING.md`** — Added comprehensive GC testing section

### Tests Updated in 2026-01-29 (Session 11)

- **Fixed 4 pre-existing test failures** — `TestGetSessionInfo` (nil cache), `TestOnlyOfficeEditorHTML*` (JSON format mismatch)
- **New: `search_test.go`** — 6 tests for search handler validation (missing/empty query, missing org_id, routes)
- **New: `batch_operations_test.go`** — 15 tests for batch operations (invalid JSON, missing fields, task progress CRUD, routes)
- **New: `library_settings_test.go`** — 11 tests for library settings (auth, invalid UUID, API token permissions, history limits, routes)
- **New: `restore_test.go`** — 5 tests for restore handler (missing path, invalid job_id, request binding, routes)
- **New: `blocks_test.go`** — 13 tests for block handler (hash validation, empty/too many hashes, nil store, upload, routes)
- **New: `audit_test.go`** — 9 tests for audit middleware (all HTTP methods, GET success/error, LogAudit/LogAccessDenied/LogPermissionChange)
- **Enabled `TestCreateShare_Validation`** — split from skipped `TestCreateShare`, runs validation paths without DB

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

50 test files across 10 packages (~300+ passing tests across api/v2, gc, middleware, auth, etc.). Coverage is strong in crypto/chunker/config/auth. API handler coverage significantly improved in Session 11.

### Pre-Existing Test Failures — ✅ ALL FIXED (Session 11)

All 4 previously failing tests are now fixed:
- ~~`TestGetSessionInfo`~~ — Fixed: use `auth.NewSessionManager()` instead of `&auth.SessionManager{}`
- ~~`TestOnlyOfficeEditorHTML`~~ — Fixed: match `json.Marshal` compact format (no spaces after colons)
- ~~`TestOnlyOfficeEditorHTMLWithoutToken`~~ — Fixed: same
- ~~`TestOnlyOfficeEditorHTMLCustomizations`~~ — Fixed: `submitForm` omitted by `omitempty` when false

### Priority 1: ✅ DONE — Previously Untested Handler Files

All 6 handler files + audit middleware now have tests (Session 11):

| File | Test File | Tests Added | Coverage |
|------|-----------|-------------|----------|
| `api/v2/search.go` | `search_test.go` | 6 | Missing query, empty query, missing org_id, JSON format, constructor, routes |
| `api/v2/batch_operations.go` | `batch_operations_test.go` | 15 | Invalid JSON, missing fields, task progress (CRUD), JSON binding, routes, TaskStore |
| `api/v2/library_settings.go` | `library_settings_test.go` | 11 | Auth middleware, invalid UUID, API token validation, history limit, auto-delete, transfer, routes |
| `api/v2/restore.go` | `restore_test.go` | 5 | Missing path, invalid job_id, missing body, routes, request binding |
| `api/v2/blocks.go` | `blocks_test.go` | 13 | Invalid JSON, empty/too many hashes, nil blockstore, invalid hash, upload, response formats, routes |
| `middleware/audit.go` | `audit_test.go` | 9 | All HTTP methods, GET success/error, LogAudit no-org, LogAccessDenied, LogPermissionChange, constants |
| `api/v2/file_shares.go` | `file_shares_test.go` | 2 (new) | Split `TestCreateShare` → validation tests run without DB |
| `db/tokens.go` | — | — | Still requires Cassandra (Priority 3) |

### Priority 2: Partially Tested Files (Missing Handler Coverage)

| File | What's Tested | What's Missing |
|------|--------------|----------------|
| `api/v2/files.go` (3060 lines) | Batch, CRUD, lock | UploadFile, GetDownloadLink, CopyFile, MoveFile, GetFileRevisions |
| `api/sync.go` | Data structures, protocol format | Handler functions: GetHeadCommit, PutCommit, GetBlock, PutBlock, PackFS, RecvFS |
| `api/v2/libraries.go` (1085 lines) | Permission checks, list | CreateLibrary end-to-end, UpdateLibrary, DeleteLibrary |

### Priority 3: Infrastructure Improvements

| Improvement | Impact | Effort | Status |
|------------|--------|--------|--------|
| **DB interface mock** | Unlocks unit tests for all handlers with DB deps | High — define interface, implement mock, refactor handlers | Not started |
| ~~**Fix 4 pre-existing test failures**~~ | ~~Clean CI output~~ | ~~Low~~ | ✅ **DONE** (Session 11) |
| **Test containers (testcontainers-go)** | Real DB integration tests in CI | Medium — Docker-in-Docker setup | Not started |
| **Frontend E2E tests (Playwright)** | Full UI workflow coverage | High — framework setup + test authoring | Not started |
| **Frontend component tests** | Modal dialogs, share components | Medium — need to resolve @testing-library/react ESM issues | Not started |
