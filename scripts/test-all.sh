#!/bin/bash
#
# Master test runner - runs all test scripts
#
# Prerequisites:
# - SesameFS backend running on localhost:8080
# - Cassandra running and initialized
# - MinIO (S3) running
#
# Usage: ./test-all.sh [--quick]
#
# Options:
#   --quick   Run quick tests only (skip long-running tests)

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE_URL="${SESAMEFS_URL:-http://localhost:8080}"

echo "==================================================="
echo "SesameFS Test Suite"
echo "==================================================="
echo "Base URL: $BASE_URL"
echo "Script dir: $SCRIPT_DIR"
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

pass() { echo -e "${GREEN}✓ PASS${NC}: $1"; }
fail() { echo -e "${RED}✗ FAIL${NC}: $1"; }
info() { echo -e "${YELLOW}→${NC} $1"; }
section() { echo -e "\n${BLUE}=== $1 ===${NC}\n"; }

QUICK_MODE=false
if [ "$1" = "--quick" ]; then
    QUICK_MODE=true
    info "Running in quick mode"
fi

# Track results
TOTAL=0
PASSED=0
FAILED=0

run_test() {
    local name="$1"
    local script="$2"
    shift 2
    local args="$@"

    TOTAL=$((TOTAL + 1))
    section "Running: $name"

    if [ -f "$SCRIPT_DIR/$script" ]; then
        if bash "$SCRIPT_DIR/$script" $args; then
            PASSED=$((PASSED + 1))
            pass "$name completed"
        else
            FAILED=$((FAILED + 1))
            fail "$name failed"
        fi
    else
        fail "Script not found: $script"
        FAILED=$((FAILED + 1))
    fi
}

# Pre-flight check
section "Pre-flight Check"
info "Checking backend availability..."
if curl -s -f "$BASE_URL/health" > /dev/null 2>&1; then
    pass "Backend is healthy"
else
    fail "Backend not responding at $BASE_URL"
    echo ""
    echo "Please ensure:"
    echo "  1. Docker containers are running: docker compose up -d"
    echo "  2. Backend is accessible at $BASE_URL"
    echo ""
    exit 1
fi

# API connectivity test
info "Testing API authentication..."
response=$(curl -s -w "\n%{http_code}" -H "Authorization: Token dev-token-admin" "$BASE_URL/api/v2.1/repos/")
status=$(echo "$response" | tail -1)
if [ "$status" = "200" ]; then
    pass "API authentication working"
else
    fail "API authentication failed (status: $status)"
    echo "Response: $(echo "$response" | head -n -1)"
fi

# Run test suites
section "Test Suite Execution"

# 1. Permission tests
run_test "Permission System" "test-permissions.sh"

# 2. File operations tests
run_test "File Operations" "test-file-operations.sh"

# 3. Batch operations tests (move/copy)
run_test "Batch Operations" "test-batch-operations.sh"

# 4. Library settings tests
run_test "Library Settings" "test-library-settings.sh"

# 5. Encrypted library security (if not quick mode)
if [ "$QUICK_MODE" = false ]; then
    run_test "Encrypted Library Security" "test-encrypted-library-security.sh" "dev-token-admin"
fi

# Print summary
echo ""
echo "==================================================="
echo "Test Summary"
echo "==================================================="
echo -e "Total:  $TOTAL"
echo -e "Passed: ${GREEN}$PASSED${NC}"
echo -e "Failed: ${RED}$FAILED${NC}"
echo ""

if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}All tests passed!${NC}"
    exit 0
else
    echo -e "${RED}Some tests failed.${NC}"
    exit 1
fi
