#!/bin/bash
#
# SesameFS Unified Test Runner
#
# This is the main entry point for running all types of tests.
# It consolidates all test scripts and provides a unified interface.
#
# Usage:
#   ./scripts/test.sh [category] [options]
#
# Categories:
#   api           Run API integration tests (permissions, file-ops, batch, etc.)
#   oidc          Run OIDC authentication tests (config, login, logout, sessions)
#   sync          Run Seafile sync protocol tests (requires seafile-cli)
#   multiregion   Run multi-region tests (requires multi-region setup)
#   failover      Run failover tests (requires multi-region setup)
#   go            Run Go unit tests
#   frontend      Run frontend tests
#   all           Run all applicable tests
#
# Options:
#   --quick       Run quick tests only (skip long-running tests)
#   --verbose     Show detailed output
#   --list        List available tests without running
#   --help        Show this help message
#
# Examples:
#   ./scripts/test.sh                    # Run API tests (default)
#   ./scripts/test.sh api                # Run API integration tests
#   ./scripts/test.sh api --quick        # Run quick API tests only
#   ./scripts/test.sh sync               # Run sync protocol tests
#   ./scripts/test.sh go                 # Run Go unit tests
#   ./scripts/test.sh all                # Run all tests
#
# Requirements by category:
#   api         - Backend running (docker compose up -d)
#   sync        - Backend + seafile-cli container
#   multiregion - Multi-region stack (./scripts/bootstrap.sh multiregion)
#   failover    - Multi-region stack + host docker access
#   go          - Go 1.25+ or Docker
#   frontend    - Node.js + npm
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Counters
TOTAL_SUITES=0
PASSED_SUITES=0
FAILED_SUITES=0

# Options
QUICK_MODE=false
VERBOSE=false
LIST_ONLY=false

# Parse arguments
CATEGORY=""
for arg in "$@"; do
    case "$arg" in
        --quick)
            QUICK_MODE=true
            ;;
        --verbose|-v)
            VERBOSE=true
            ;;
        --list)
            LIST_ONLY=true
            ;;
        --help|-h)
            head -50 "$0" | grep "^#" | sed 's/^# //' | sed 's/^#//'
            exit 0
            ;;
        -*)
            # Unknown flag, ignore
            ;;
        *)
            # First non-flag argument is the category
            if [ -z "$CATEGORY" ]; then
                CATEGORY="$arg"
            fi
            ;;
    esac
done

# Default category
CATEGORY="${CATEGORY:-api}"

# Helper functions
log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[PASS]${NC} $1"; }
log_error() { echo -e "${RED}[FAIL]${NC} $1"; }
log_warning() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_section() { echo -e "\n${CYAN}=== $1 ===${NC}\n"; }

# Check if a service is available
check_backend() {
    local url="${SESAMEFS_URL:-http://localhost:8080}"
    if curl -s -f "$url/health" > /dev/null 2>&1; then
        return 0
    fi
    return 1
}

check_seafile_cli() {
    local container="${CLI_CONTAINER:-cool-storage-api-seafile-cli-1}"
    if docker ps --format '{{.Names}}' 2>/dev/null | grep -q "$container"; then
        return 0
    fi
    return 1
}

check_multiregion() {
    if curl -s -f "http://localhost:8080/ping" > /dev/null 2>&1; then
        # Check if nginx is the load balancer (multi-region mode)
        if docker ps --format '{{.Names}}' 2>/dev/null | grep -q "nginx"; then
            return 0
        fi
    fi
    return 1
}

check_go() {
    if command -v go &> /dev/null; then
        return 0
    fi
    return 1
}

check_node() {
    if command -v npm &> /dev/null; then
        return 0
    fi
    return 1
}

# Run a test suite
run_suite() {
    local name="$1"
    local script="$2"
    shift 2
    local args="$@"

    TOTAL_SUITES=$((TOTAL_SUITES + 1))

    if [ "$LIST_ONLY" = true ]; then
        echo "  - $name ($script)"
        return 0
    fi

    log_section "Running: $name"

    if [ -f "$SCRIPT_DIR/$script" ]; then
        if bash "$SCRIPT_DIR/$script" $args; then
            PASSED_SUITES=$((PASSED_SUITES + 1))
            log_success "$name completed"
            return 0
        else
            FAILED_SUITES=$((FAILED_SUITES + 1))
            log_error "$name failed"
            return 1
        fi
    else
        log_error "Script not found: $script"
        FAILED_SUITES=$((FAILED_SUITES + 1))
        return 1
    fi
}

# ==========================================================================
# API Tests - Basic integration tests requiring only backend
# ==========================================================================
run_api_tests() {
    log_section "API Integration Tests"

    if ! check_backend; then
        log_error "Backend not available at ${SESAMEFS_URL:-http://localhost:8080}"
        echo ""
        echo "Start the backend with:"
        echo "  docker compose up -d"
        echo ""
        return 1
    fi

    log_success "Backend is available"

    # Run test suites
    run_suite "Permission System" "test-permissions.sh" || true
    run_suite "File Operations" "test-file-operations.sh" || true
    run_suite "Batch Operations" "test-batch-operations.sh" || true
    run_suite "Library Settings" "test-library-settings.sh" || true
    run_suite "Nested Folders" "test-nested-folders.sh --quick" || true

    if [ "$QUICK_MODE" = false ]; then
        run_suite "Encrypted Library Security" "test-encrypted-library-security.sh" || true
    else
        log_info "Skipping encrypted library tests (--quick mode)"
    fi
}

# ==========================================================================
# Sync Tests - Seafile CLI sync protocol tests
# ==========================================================================
run_sync_tests() {
    log_section "Sync Protocol Tests"

    if ! check_backend; then
        log_error "Backend not available"
        return 1
    fi

    if ! check_seafile_cli; then
        log_warning "Seafile CLI container not running"
        echo ""
        echo "Start seafile-cli with:"
        echo "  docker compose up -d seafile-cli"
        echo ""
        echo "Or skip sync tests with: ./scripts/test.sh api"
        return 1
    fi

    log_success "Seafile CLI container is available"

    local args=""
    [ "$VERBOSE" = true ] && args="--verbose"

    run_suite "Sync Protocol" "test-sync.sh" $args || true
}

# ==========================================================================
# Multi-Region Tests
# ==========================================================================
run_multiregion_tests() {
    log_section "Multi-Region Tests"

    if ! check_multiregion; then
        log_warning "Multi-region stack not running"
        echo ""
        echo "Start multi-region with:"
        echo "  ./scripts/bootstrap.sh multiregion"
        echo ""
        return 1
    fi

    log_success "Multi-region stack is available"

    run_suite "Multi-Region Connectivity" "test-multiregion.sh" "connectivity" || true
    run_suite "Multi-Region Upload" "test-multiregion.sh" "upload" || true
    run_suite "Multi-Region Routing" "test-multiregion.sh" "routing" || true
}

# ==========================================================================
# Failover Tests
# ==========================================================================
run_failover_tests() {
    log_section "Failover Tests"

    if ! check_multiregion; then
        log_warning "Multi-region stack not running"
        return 1
    fi

    # Check if running in container (failover tests need host docker access)
    if [ -f /.dockerenv ] || grep -q docker /proc/1/cgroup 2>/dev/null; then
        log_warning "Running in container - failover tests require host execution"
        echo ""
        echo "Run failover tests from host:"
        echo "  ./scripts/test-failover.sh all"
        return 0
    fi

    run_suite "Failover Setup" "test-failover.sh" "setup" || true
    run_suite "Failover Upload" "test-failover.sh" "upload" || true

    if [ "$QUICK_MODE" = false ]; then
        run_suite "Download Failover" "test-failover.sh" "download" || true
        run_suite "Upload Failover" "test-failover.sh" "upload-fail" || true
        run_suite "Recovery" "test-failover.sh" "recovery" || true
    fi

    run_suite "Failover Cleanup" "test-failover.sh" "cleanup" || true
}

# ==========================================================================
# OIDC Authentication Tests
# ==========================================================================
run_oidc_tests() {
    log_section "OIDC Authentication Tests"

    if ! check_backend; then
        log_error "Backend not available at ${SESAMEFS_URL:-http://localhost:8080}"
        return 1
    fi

    log_success "Backend is available"

    local args=""
    [ "$QUICK_MODE" = true ] && args="--quick"
    [ "$VERBOSE" = true ] && args="$args --verbose"

    run_suite "OIDC Authentication" "test-oidc.sh" $args || true
}

# ==========================================================================
# Go Unit Tests
# ==========================================================================
run_go_tests() {
    log_section "Go Unit Tests"

    if check_go; then
        log_info "Running Go tests locally..."
        cd "$PROJECT_DIR"
        if go test ./... -short -cover; then
            PASSED_SUITES=$((PASSED_SUITES + 1))
            log_success "Go tests passed"
        else
            FAILED_SUITES=$((FAILED_SUITES + 1))
            log_error "Go tests failed"
        fi
        TOTAL_SUITES=$((TOTAL_SUITES + 1))
    else
        log_info "Go not installed locally, using Docker..."

        # Build and run tests in Docker
        docker build -t sesamefs-gotest -f - "$PROJECT_DIR" << 'EOF'
FROM golang:1.25-alpine
RUN apk add --no-cache git
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
CMD ["go", "test", "./...", "-short", "-cover"]
EOF

        TOTAL_SUITES=$((TOTAL_SUITES + 1))
        if docker run --rm sesamefs-gotest; then
            PASSED_SUITES=$((PASSED_SUITES + 1))
            log_success "Go tests passed (Docker)"
        else
            FAILED_SUITES=$((FAILED_SUITES + 1))
            log_error "Go tests failed (Docker)"
        fi
    fi
}

# ==========================================================================
# Frontend Tests
# ==========================================================================
run_frontend_tests() {
    log_section "Frontend Tests"

    if ! check_node; then
        log_warning "Node.js/npm not available"
        echo ""
        echo "Install Node.js to run frontend tests, or run in Docker:"
        echo "  docker run --rm -v $PROJECT_DIR/frontend:/app -w /app node:20 npm test -- --watchAll=false"
        return 1
    fi

    cd "$PROJECT_DIR/frontend"

    if [ ! -d "node_modules" ]; then
        log_info "Installing frontend dependencies..."
        npm install
    fi

    TOTAL_SUITES=$((TOTAL_SUITES + 1))
    if npm test -- --watchAll=false; then
        PASSED_SUITES=$((PASSED_SUITES + 1))
        log_success "Frontend tests passed"
    else
        FAILED_SUITES=$((FAILED_SUITES + 1))
        log_error "Frontend tests failed"
    fi
}

# ==========================================================================
# List Available Tests
# ==========================================================================
list_tests() {
    echo ""
    echo "Available Test Categories"
    echo "========================="
    echo ""

    echo "api - API Integration Tests (requires: backend)"
    LIST_ONLY=true
    echo "  - Permission System (test-permissions.sh)"
    echo "  - File Operations (test-file-operations.sh)"
    echo "  - Batch Operations (test-batch-operations.sh)"
    echo "  - Library Settings (test-library-settings.sh)"
    echo "  - Encrypted Library Security (test-encrypted-library-security.sh)"
    echo ""

    echo "oidc - OIDC Authentication Tests (requires: backend)"
    echo "  - OIDC Configuration"
    echo "  - Login URL Generation"
    echo "  - Callback Handling"
    echo "  - Logout (Single Logout)"
    echo "  - Session Management"
    echo ""

    echo "sync - Sync Protocol Tests (requires: backend + seafile-cli)"
    echo "  - Sync Protocol (test-sync.sh)"
    echo ""

    echo "multiregion - Multi-Region Tests (requires: multi-region stack)"
    echo "  - Connectivity (test-multiregion.sh connectivity)"
    echo "  - Upload (test-multiregion.sh upload)"
    echo "  - Routing (test-multiregion.sh routing)"
    echo ""

    echo "failover - Failover Tests (requires: multi-region stack + host docker)"
    echo "  - Setup, Upload, Download Failover, Recovery"
    echo ""

    echo "go - Go Unit Tests (requires: Go 1.25+ or Docker)"
    echo "  - All packages in internal/"
    echo ""

    echo "frontend - Frontend Tests (requires: Node.js)"
    echo "  - React component tests"
    echo ""

    echo "all - Run all applicable tests"
    echo ""
}

# ==========================================================================
# Main
# ==========================================================================
main() {
    echo ""
    echo "=========================================="
    echo "SesameFS Test Runner"
    echo "=========================================="
    echo ""

    if [ "$LIST_ONLY" = true ] || [ "$CATEGORY" = "list" ]; then
        list_tests
        exit 0
    fi

    local start_time=$(date +%s)

    case "$CATEGORY" in
        api|integration)
            run_api_tests
            ;;
        oidc|auth)
            run_oidc_tests
            ;;
        sync)
            run_sync_tests
            ;;
        multiregion|multi)
            run_multiregion_tests
            ;;
        failover)
            run_failover_tests
            ;;
        go|unit)
            run_go_tests
            ;;
        frontend|fe)
            run_frontend_tests
            ;;
        all)
            run_api_tests
            run_oidc_tests
            run_go_tests
            # Only run these if their prerequisites are met
            if check_seafile_cli; then
                run_sync_tests
            else
                log_info "Skipping sync tests (seafile-cli not available)"
            fi
            if check_multiregion; then
                run_multiregion_tests
            else
                log_info "Skipping multiregion tests (stack not running)"
            fi
            if check_node; then
                run_frontend_tests
            else
                log_info "Skipping frontend tests (Node.js not available)"
            fi
            ;;
        *)
            log_error "Unknown category: $CATEGORY"
            echo ""
            echo "Run './scripts/test.sh --help' for usage information"
            echo "Run './scripts/test.sh --list' to see available tests"
            exit 1
            ;;
    esac

    local end_time=$(date +%s)
    local duration=$((end_time - start_time))

    # Print summary
    echo ""
    echo "=========================================="
    echo "Test Summary"
    echo "=========================================="
    echo ""
    echo "Total suites:  $TOTAL_SUITES"
    echo -e "Passed:        ${GREEN}$PASSED_SUITES${NC}"
    echo -e "Failed:        ${RED}$FAILED_SUITES${NC}"
    echo "Duration:      ${duration}s"
    echo ""

    if [ $FAILED_SUITES -eq 0 ]; then
        echo -e "${GREEN}All tests passed!${NC}"
        exit 0
    else
        echo -e "${RED}Some tests failed.${NC}"
        exit 1
    fi
}

main "$@"
