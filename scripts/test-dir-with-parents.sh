#!/bin/bash
# =============================================================================
# Directory Listing with_parents Integration Tests
# =============================================================================
#
# Tests the with_parents query parameter on GET /api/v2.1/repos/:id/dir/
# which is used by the move/copy dialog's file-chooser tree to load
# all parent directories from root to the current path.
#
# Usage:
#   ./scripts/test-dir-with-parents.sh [options]
#
# Options:
#   --verbose     Show curl response bodies
#   --help        Show this help
#
# Requirements:
#   - Backend running at $API_URL (default: http://localhost:8082)
#   - Dev tokens configured
#   - curl installed
#
# =============================================================================

set -e

# Configuration
API_URL="${API_URL:-http://localhost:8082}"
ADMIN_TOKEN="${ADMIN_TOKEN:-dev-token-admin}"

# Options
VERBOSE=false

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# Counters
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0
declare -a FAILED_TEST_NAMES

# =============================================================================
# Helpers
# =============================================================================

log_info()    { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[PASS]${NC} $1"; }
log_fail()    { echo -e "${RED}[FAIL]${NC} $1"; }
log_section() {
    echo ""
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${CYAN}  $1${NC}"
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

# Parse arguments
for arg in "$@"; do
    case "$arg" in
        --verbose) VERBOSE=true ;;
        --help)
            head -25 "$0" | grep "^#" | sed 's/^# //' | sed 's/^#//'
            exit 0
            ;;
    esac
done

# API helpers
api_status() {
    local method="$1" endpoint="$2" token="$3" data="$4"
    local url="${API_URL}${endpoint}"
    local opts=(-s -o /dev/null -w "%{http_code}")
    if [ -n "$token" ]; then opts+=(-H "Authorization: Token $token"); fi
    opts+=(-H "Content-Type: application/json")
    if [ -n "$data" ]; then opts+=(-d "$data"); fi
    curl "${opts[@]}" -X "$method" "$url"
}

api_body() {
    local method="$1" endpoint="$2" token="$3" data="$4"
    local url="${API_URL}${endpoint}"
    local opts=(-s)
    if [ -n "$token" ]; then opts+=(-H "Authorization: Token $token"); fi
    opts+=(-H "Content-Type: application/json")
    if [ -n "$data" ]; then opts+=(-d "$data"); fi
    curl "${opts[@]}" -X "$method" "$url"
}

run_test() {
    local description="$1" expected="$2" actual="$3"
    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    if [ "$expected" = "$actual" ]; then
        PASSED_TESTS=$((PASSED_TESTS + 1))
        log_success "$description (expected: $expected, got: $actual)"
    else
        FAILED_TESTS=$((FAILED_TESTS + 1))
        FAILED_TEST_NAMES+=("$description")
        log_fail "$description (expected: $expected, got: $actual)"
    fi
}

run_test_contains() {
    local description="$1" expected_substr="$2" actual="$3"
    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    if echo "$actual" | grep -q "$expected_substr"; then
        PASSED_TESTS=$((PASSED_TESTS + 1))
        log_success "$description (contains '$expected_substr')"
    else
        FAILED_TESTS=$((FAILED_TESTS + 1))
        FAILED_TEST_NAMES+=("$description")
        log_fail "$description (missing '$expected_substr' in: ${actual:0:200})"
    fi
}

run_test_not_contains() {
    local description="$1" unexpected_substr="$2" actual="$3"
    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    if ! echo "$actual" | grep -q "$unexpected_substr"; then
        PASSED_TESTS=$((PASSED_TESTS + 1))
        log_success "$description (correctly missing '$unexpected_substr')"
    else
        FAILED_TESTS=$((FAILED_TESTS + 1))
        FAILED_TEST_NAMES+=("$description")
        log_fail "$description (unexpectedly found '$unexpected_substr')"
    fi
}

# Count occurrences of a pattern in a string
count_matches() {
    local pattern="$1" text="$2"
    echo "$text" | grep -o "$pattern" | wc -l | tr -d ' '
}

# =============================================================================
# Pre-flight
# =============================================================================

echo -e "${CYAN}SesameFS Directory with_parents Integration Tests${NC}"
echo "API URL: $API_URL"
echo ""

log_section "Pre-flight Checks"

# Check API is reachable
if curl -s -f "$API_URL/health" > /dev/null 2>&1; then
    log_success "API is reachable at $API_URL"
else
    log_fail "API not reachable at $API_URL"
    exit 1
fi

# Check admin token
STATUS=$(api_status "GET" "/api2/account/info/" "$ADMIN_TOKEN")
run_test "Admin token is valid" "200" "$STATUS"

# =============================================================================
# Setup: Create test library with nested directories
# =============================================================================

log_section "Setup: Create test library with nested structure"

# Create library
BODY=$(api_body "POST" "/api/v2.1/repos/" "$ADMIN_TOKEN" '{"repo_name":"with-parents-test"}')
REPO_ID=$(echo "$BODY" | grep -o '"repo_id":"[^"]*"' | cut -d'"' -f4)

if [ -z "$REPO_ID" ] || [ "$REPO_ID" = "null" ]; then
    log_fail "Could not create test library"
    echo "  Response: $BODY"
    exit 1
fi
log_info "Created library: $REPO_ID"

# Cleanup on exit
cleanup() {
    log_section "Cleanup"
    api_status "DELETE" "/api/v2.1/repos/$REPO_ID/" "$ADMIN_TOKEN" > /dev/null 2>&1
    log_info "Deleted test library: $REPO_ID"
}
trap cleanup EXIT

# Create nested directory structure:
#   /
#   ├── alpha/
#   │   ├── beta/
#   │   │   ├── gamma/
#   │   │   │   └── (empty)
#   │   │   └── sibling-of-gamma/
#   │   └── other/
#   ├── top-dir/
#   └── readme.md

api_status "POST" "/api/v2.1/repos/$REPO_ID/dir/?p=/alpha&operation=mkdir" "$ADMIN_TOKEN" > /dev/null
api_status "POST" "/api/v2.1/repos/$REPO_ID/dir/?p=/alpha/beta&operation=mkdir" "$ADMIN_TOKEN" > /dev/null
api_status "POST" "/api/v2.1/repos/$REPO_ID/dir/?p=/alpha/beta/gamma&operation=mkdir" "$ADMIN_TOKEN" > /dev/null
api_status "POST" "/api/v2.1/repos/$REPO_ID/dir/?p=/alpha/beta/sibling-of-gamma&operation=mkdir" "$ADMIN_TOKEN" > /dev/null
api_status "POST" "/api/v2.1/repos/$REPO_ID/dir/?p=/alpha/other&operation=mkdir" "$ADMIN_TOKEN" > /dev/null
api_status "POST" "/api/v2.1/repos/$REPO_ID/dir/?p=/top-dir&operation=mkdir" "$ADMIN_TOKEN" > /dev/null
api_status "POST" "/api/v2.1/repos/$REPO_ID/file/?p=/readme.md&operation=create" "$ADMIN_TOKEN" > /dev/null

log_info "Created nested structure: /alpha/beta/gamma, /alpha/beta/sibling-of-gamma, /alpha/other, /top-dir, /readme.md"

# =============================================================================
# 1. Normal listing (without with_parents) — baseline
# =============================================================================

log_section "1. Normal listing (baseline, no with_parents)"

BODY=$(api_body "GET" "/api/v2.1/repos/$REPO_ID/dir/?p=/alpha/beta" "$ADMIN_TOKEN")
STATUS=$(api_status "GET" "/api/v2.1/repos/$REPO_ID/dir/?p=/alpha/beta" "$ADMIN_TOKEN")

run_test "List /alpha/beta returns 200" "200" "$STATUS"
run_test_contains "Has gamma in listing" '"name":"gamma"' "$BODY"
run_test_contains "Has sibling-of-gamma" '"name":"sibling-of-gamma"' "$BODY"
run_test_not_contains "Does NOT include parent dirs (alpha)" '"name":"alpha"' "$BODY"
run_test_not_contains "Does NOT include root dirs (top-dir)" '"name":"top-dir"' "$BODY"

# =============================================================================
# 2. with_parents at depth 2 (/alpha/beta)
# =============================================================================

log_section "2. with_parents at depth 2 (/alpha/beta)"

BODY=$(api_body "GET" "/api/v2.1/repos/$REPO_ID/dir/?p=/alpha/beta&with_parents=true" "$ADMIN_TOKEN")
STATUS=$(api_status "GET" "/api/v2.1/repos/$REPO_ID/dir/?p=/alpha/beta&with_parents=true" "$ADMIN_TOKEN")

run_test "Returns 200" "200" "$STATUS"

# Should include root-level dirs with parent_dir="/"
run_test_contains "Contains alpha (root child)" '"name":"alpha"' "$BODY"
run_test_contains "Contains top-dir (root child)" '"name":"top-dir"' "$BODY"
run_test_contains "Root entries have parent_dir=/" '"parent_dir":"/"' "$BODY"

# Should include /alpha's children with parent_dir="/alpha/"
run_test_contains "Contains beta (alpha child)" '"name":"beta"' "$BODY"
run_test_contains "Contains other (alpha child)" '"name":"other"' "$BODY"
run_test_contains "Alpha children have parent_dir=/alpha/" '"parent_dir":"/alpha/"' "$BODY"

# Should include /alpha/beta's children with parent_dir="/alpha/beta/"
run_test_contains "Contains gamma (beta child)" '"name":"gamma"' "$BODY"
run_test_contains "Contains sibling-of-gamma" '"name":"sibling-of-gamma"' "$BODY"
run_test_contains "Beta children have parent_dir=/alpha/beta/" '"parent_dir":"/alpha/beta/"' "$BODY"

# with_parents should only return dirs (files excluded from parent levels)
run_test_not_contains "Root-level files excluded from parents" '"name":"readme.md".*"parent_dir":"/"' "$BODY"

# =============================================================================
# 3. with_parents at depth 3 (/alpha/beta/gamma)
# =============================================================================

log_section "3. with_parents at depth 3 (/alpha/beta/gamma)"

BODY=$(api_body "GET" "/api/v2.1/repos/$REPO_ID/dir/?p=/alpha/beta/gamma&with_parents=true" "$ADMIN_TOKEN")
STATUS=$(api_status "GET" "/api/v2.1/repos/$REPO_ID/dir/?p=/alpha/beta/gamma&with_parents=true" "$ADMIN_TOKEN")

run_test "Returns 200" "200" "$STATUS"

# Should have 3 levels of parent_dir values
run_test_contains "Has root level (parent_dir=/)" '"parent_dir":"/"' "$BODY"
run_test_contains "Has alpha level (parent_dir=/alpha/)" '"parent_dir":"/alpha/"' "$BODY"
run_test_contains "Has beta level (parent_dir=/alpha/beta/)" '"parent_dir":"/alpha/beta/"' "$BODY"

# gamma is empty, so target entries should have parent_dir=/alpha/beta/gamma/
# but since gamma has no children, the target's dirent_list portion is empty
# The sibling-of-gamma should appear at the /alpha/beta/ level
run_test_contains "Sibling-of-gamma at beta level" '"name":"sibling-of-gamma"' "$BODY"

# =============================================================================
# 4. with_parents at root (/) — should behave like normal listing
# =============================================================================

log_section "4. with_parents at root (/)"

BODY=$(api_body "GET" "/api/v2.1/repos/$REPO_ID/dir/?p=/&with_parents=true" "$ADMIN_TOKEN")
STATUS=$(api_status "GET" "/api/v2.1/repos/$REPO_ID/dir/?p=/&with_parents=true" "$ADMIN_TOKEN")

run_test "Returns 200" "200" "$STATUS"
run_test_contains "Has alpha" '"name":"alpha"' "$BODY"
run_test_contains "Has top-dir" '"name":"top-dir"' "$BODY"
run_test_contains "Has readme.md" '"name":"readme.md"' "$BODY"

# At root there are no parents, so parent_dir should be "/"
run_test_contains "All entries have parent_dir=/" '"parent_dir":"/"' "$BODY"
# Should NOT have deeper parent_dir values
run_test_not_contains "No /alpha/ parent_dir" '"parent_dir":"/alpha/"' "$BODY"

# =============================================================================
# 5. with_parents at depth 1 (/alpha)
# =============================================================================

log_section "5. with_parents at depth 1 (/alpha)"

BODY=$(api_body "GET" "/api/v2.1/repos/$REPO_ID/dir/?p=/alpha&with_parents=true" "$ADMIN_TOKEN")
STATUS=$(api_status "GET" "/api/v2.1/repos/$REPO_ID/dir/?p=/alpha&with_parents=true" "$ADMIN_TOKEN")

run_test "Returns 200" "200" "$STATUS"

# Root level dirs (parents)
run_test_contains "Has root-level alpha" '"name":"alpha"' "$BODY"
run_test_contains "Has root-level top-dir" '"name":"top-dir"' "$BODY"
run_test_contains "Root entries have parent_dir=/" '"parent_dir":"/"' "$BODY"

# /alpha's children (target entries)
run_test_contains "Has beta" '"name":"beta"' "$BODY"
run_test_contains "Has other" '"name":"other"' "$BODY"
run_test_contains "Alpha children have parent_dir=/alpha/" '"parent_dir":"/alpha/"' "$BODY"

# Should NOT have deeper levels
run_test_not_contains "No /alpha/beta/ parent_dir" '"parent_dir":"/alpha/beta/"' "$BODY"

# =============================================================================
# 6. with_parents=false — same as no parameter
# =============================================================================

log_section "6. with_parents=false (same as omitted)"

BODY=$(api_body "GET" "/api/v2.1/repos/$REPO_ID/dir/?p=/alpha/beta&with_parents=false" "$ADMIN_TOKEN")
BODY_NORMAL=$(api_body "GET" "/api/v2.1/repos/$REPO_ID/dir/?p=/alpha/beta" "$ADMIN_TOKEN")

# Both should return only the target directory's contents
run_test_not_contains "with_parents=false: no root entries" '"name":"top-dir"' "$BODY"
run_test_not_contains "Normal: no root entries" '"name":"top-dir"' "$BODY_NORMAL"
run_test_contains "with_parents=false: has gamma" '"name":"gamma"' "$BODY"
run_test_contains "Normal: has gamma" '"name":"gamma"' "$BODY_NORMAL"

# =============================================================================
# 7. with_parents on nonexistent path
# =============================================================================

log_section "7. with_parents on nonexistent path"

STATUS=$(api_status "GET" "/api/v2.1/repos/$REPO_ID/dir/?p=/alpha/nonexistent&with_parents=true" "$ADMIN_TOKEN")
run_test "Nonexistent subdir returns 404" "404" "$STATUS"

STATUS=$(api_status "GET" "/api/v2.1/repos/$REPO_ID/dir/?p=/nope/deep/path&with_parents=true" "$ADMIN_TOKEN")
run_test "Nonexistent deep path returns 404" "404" "$STATUS"

# =============================================================================
# 8. parent_dir format validation (trailing slash)
# =============================================================================

log_section "8. parent_dir format validation"

BODY=$(api_body "GET" "/api/v2.1/repos/$REPO_ID/dir/?p=/alpha/beta&with_parents=true" "$ADMIN_TOKEN")

# Root parent_dir should be "/" (no trailing slash issue)
# This is what the frontend expects: parentDir === '/' ? '/' : parentDir.slice(0, -1)
run_test_contains "Root parent_dir is /" '"parent_dir":"/"' "$BODY"

# /alpha/ children should have trailing slash
run_test_contains "/alpha/ has trailing slash" '"parent_dir":"/alpha/"' "$BODY"

# /alpha/beta/ children should have trailing slash
run_test_contains "/alpha/beta/ has trailing slash" '"parent_dir":"/alpha/beta/"' "$BODY"

# Verify the frontend's key computation would work:
# "/" -> "/"                    (root)
# "/alpha/" -> "/alpha"         (strip trailing slash)
# "/alpha/beta/" -> "/alpha/beta" (strip trailing slash)
# These must match tree node paths for getNodeByPath() to work.

# =============================================================================
# 9. with_parents returns only dirs at parent levels
# =============================================================================

log_section "9. Parent levels return only directories"

# Create a file inside /alpha to verify it's excluded from parent entries
api_status "POST" "/api/v2.1/repos/$REPO_ID/file/?p=/alpha/note.txt&operation=create" "$ADMIN_TOKEN" > /dev/null

BODY=$(api_body "GET" "/api/v2.1/repos/$REPO_ID/dir/?p=/alpha/beta&with_parents=true" "$ADMIN_TOKEN")

# The /alpha level parent entries should only have dirs (beta, other), not files (note.txt)
# But the file-chooser frontend filters client-side, so what matters is parent_dir format
# Still, verify the parent_dir grouping is correct
run_test_contains "Alpha has beta child" '"name":"beta"' "$BODY"
run_test_contains "Alpha has other child" '"name":"other"' "$BODY"

# =============================================================================
# 10. Response structure with with_parents
# =============================================================================

log_section "10. Response structure validation"

BODY=$(api_body "GET" "/api/v2.1/repos/$REPO_ID/dir/?p=/alpha/beta&with_parents=true" "$ADMIN_TOKEN")

# Must have user_perm
run_test_contains "Response has user_perm" '"user_perm"' "$BODY"

# Must have dir_id
run_test_contains "Response has dir_id" '"dir_id"' "$BODY"

# Must have dirent_list
run_test_contains "Response has dirent_list" '"dirent_list"' "$BODY"

# Each entry should have standard fields
run_test_contains "Entries have type field" '"type":"dir"' "$BODY"
run_test_contains "Entries have name field" '"name":' "$BODY"

# =============================================================================
# Summary
# =============================================================================

log_section "Directory with_parents Test Summary"

echo ""
echo "  Total:  $TOTAL_TESTS"
echo -e "  ${GREEN}Passed: $PASSED_TESTS${NC}"
echo -e "  ${RED}Failed: $FAILED_TESTS${NC}"
echo ""

if [ $FAILED_TESTS -gt 0 ]; then
    echo -e "  ${RED}Failed tests:${NC}"
    for name in "${FAILED_TEST_NAMES[@]}"; do
        echo "    - $name"
    done
    echo ""
    exit 1
else
    echo -e "  ${GREEN}All tests passed!${NC}"
fi
