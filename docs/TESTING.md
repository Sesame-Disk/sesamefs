# Testing Guide

This document describes how to run tests, test coverage, and testing infrastructure.

**Last updated: 2026-01-28**

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

# Legacy master runner (still works)
./scripts/test-all.sh
./scripts/test-all.sh --quick
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
| `oidc_test.go` | 18 | Discovery, auth URL, state, logout, role mapping |

### 3. Go Unit Tests (`go`)

Requires: Go 1.25+ or Docker

```bash
./scripts/test.sh go
```

**Coverage by Package:**
| Package | Coverage | Notes |
|---------|----------|-------|
| `internal/config` | 88.0% | Config loading, validation |
| `internal/chunker` | 78.7% | FastCDC + Adaptive chunking |
| `internal/crypto` | 69.1% | Encryption, key derivation |
| `internal/auth` | ~70% | **NEW** OIDC, session management, JWT |
| `internal/storage` | 46.6% | S3, blocks, SpillBuffer |
| `internal/api/v2` | 16.1% | REST API handlers (incl. auth) |
| `internal/api` | 13.0% | Sync protocol, SeafHTTP |
| `internal/middleware` | 2.5% | Permission middleware |
| `internal/db` | 0% | Requires Cassandra |

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
- SesameFS: http://localhost:8080
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
- Load Balancer: http://localhost:8080
- USA Endpoint: http://us.sesamefs.local:8080
- EU Endpoint: http://eu.sesamefs.local:8080
- MinIO Console: http://localhost:9001

---

## Test Scripts Reference

| Script | Purpose | Requirements |
|--------|---------|--------------|
| `test.sh` | **Unified test runner** | Varies by category |
| `test-all.sh` | Legacy API test runner | Backend |
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
| `SESAMEFS_URL` | http://localhost:8080 | Backend URL |
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

### Tests Updated in 2026-01-28

- Fixed `NewSeafHTTPHandler` test signature (added `permMiddleware` parameter)
- Fixed `middleware.Permission` → `middleware.LibraryPermission` type
- Fixed test scripts to use unique library names (prevents 409 conflicts)
- Created unified test runner (`test.sh`)

---

## Future Improvements

1. **Database mock interface** - Abstract DB operations for unit testing
2. **Test containers** - Use testcontainers-go for automatic Docker management
3. **E2E tests** - Full API tests with real Seafile client compatibility
4. **Frontend component tests** - Add tests for modal dialogs, share components
