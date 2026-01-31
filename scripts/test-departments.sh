#!/bin/bash
# =============================================================================
# Department CRUD Integration Tests for SesameFS
# =============================================================================
#
# Tests the department management endpoints (hierarchical groups for org admins).
#
# Flow:
#   1. List departments (should start empty)
#   2. Create a root department
#   3. Create a child department (nested)
#   4. Get department with members and sub-departments
#   5. Get department with ancestor breadcrumbs
#   6. Update (rename) a department
#   7. List user departments
#   8. Delete child, then parent
#   9. Verify deletion
#
# Also tests:
#   - Search user endpoint
#   - Multi-share-links alias
#   - Copy/move progress alias
#   - File download URL generation
#
# Usage:
#   ./scripts/test-departments.sh [options]
#
# Options:
#   --quick       Skip cleanup (leave test data for inspection)
#   --verbose     Show curl response bodies
#   --help        Show this help
#
# Requirements:
#   - Backend running at $API_URL (default: http://localhost:8082)
#   - Dev tokens configured
#   - curl, jq installed
#
# =============================================================================

set -e

# Configuration
API_URL="${API_URL:-http://localhost:8082}"
ADMIN_TOKEN="${ADMIN_TOKEN:-dev-token-admin}"

# Options
QUICK_MODE=false
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

run_test() {
    local test_name="$1"
    local expected="$2"
    local actual="$3"

    TOTAL_TESTS=$((TOTAL_TESTS + 1))

    if [ "$expected" == "$actual" ]; then
        log_success "$test_name (expected: $expected, got: $actual)"
        PASSED_TESTS=$((PASSED_TESTS + 1))
    else
        log_fail "$test_name (expected: $expected, got: $actual)"
        FAILED_TESTS=$((FAILED_TESTS + 1))
        FAILED_TEST_NAMES+=("$test_name")
    fi
}

run_test_contains() {
    local test_name="$1"
    local needle="$2"
    local haystack="$3"

    TOTAL_TESTS=$((TOTAL_TESTS + 1))

    if echo "$haystack" | grep -q "$needle"; then
        log_success "$test_name (contains '$needle')"
        PASSED_TESTS=$((PASSED_TESTS + 1))
    else
        log_fail "$test_name (missing '$needle')"
        FAILED_TESTS=$((FAILED_TESTS + 1))
        FAILED_TEST_NAMES+=("$test_name")
    fi
}

# Returns HTTP status code only
api_status() {
    local method="$1"
    local endpoint="$2"
    local token="$3"
    local data="$4"

    local url="${API_URL}${endpoint}"
    local opts=(-s -o /dev/null -w "%{http_code}")

    if [ -n "$token" ]; then
        opts+=(-H "Authorization: Token $token")
    fi

    opts+=(-H "Content-Type: application/json")

    if [ -n "$data" ]; then
        opts+=(-d "$data")
    fi

    curl "${opts[@]}" -X "$method" "$url"
}

# Returns response body
api_body() {
    local method="$1"
    local endpoint="$2"
    local token="$3"
    local data="$4"

    local url="${API_URL}${endpoint}"
    local opts=(-s)

    if [ -n "$token" ]; then
        opts+=(-H "Authorization: Token $token")
    fi

    opts+=(-H "Content-Type: application/json")

    if [ -n "$data" ]; then
        opts+=(-d "$data")
    fi

    local body
    body=$(curl "${opts[@]}" -X "$method" "$url")

    if [ "$VERBOSE" = true ]; then
        echo -e "${YELLOW}[RESP]${NC} $method $endpoint" >&2
        echo "$body" | jq . 2>/dev/null >&2 || echo "$body" >&2
    fi

    echo "$body"
}

# Returns both body and status in a single curl call (avoids duplicate POSTs)
# Usage: local result=$(api_call METHOD ENDPOINT TOKEN [DATA])
#        local body=$(echo "$result" | head -n -1)
#        local status=$(echo "$result" | tail -1)
api_call() {
    local method="$1"
    local endpoint="$2"
    local token="$3"
    local data="$4"

    local url="${API_URL}${endpoint}"
    local opts=(-s -w "\n%{http_code}")

    if [ -n "$token" ]; then
        opts+=(-H "Authorization: Token $token")
    fi

    opts+=(-H "Content-Type: application/json")

    if [ -n "$data" ]; then
        opts+=(-d "$data")
    fi

    curl "${opts[@]}" -X "$method" "$url"
}

# Parse options
for arg in "$@"; do
    case $arg in
        --quick)   QUICK_MODE=true ;;
        --verbose) VERBOSE=true ;;
        --help)
            head -40 "$0" | tail -35
            exit 0
            ;;
    esac
done

# =============================================================================
# Pre-flight
# =============================================================================

preflight() {
    log_section "Pre-flight Checks"

    # Check API reachable
    if ! curl -s -o /dev/null -w "%{http_code}" "${API_URL}/api2/ping/" | grep -q "200"; then
        log_fail "API not reachable at ${API_URL}"
        echo "  Start the backend: docker compose up -d sesamefs"
        exit 1
    fi
    log_success "API is reachable at ${API_URL}"

    # Check jq
    if ! command -v jq &> /dev/null; then
        log_fail "jq is required but not installed"
        exit 1
    fi

    # Verify admin token works
    local status=$(api_status "GET" "/api2/account/info/" "$ADMIN_TOKEN")
    run_test "Admin token is valid" "200" "$status"
}

# =============================================================================
# Test 1: List Departments (initially empty)
# =============================================================================

test_list_departments_empty() {
    log_section "1. List Departments (initially empty)"

    local body=$(api_body "GET" "/api/v2.1/admin/address-book/groups/" "$ADMIN_TOKEN")
    local status=$(api_status "GET" "/api/v2.1/admin/address-book/groups/" "$ADMIN_TOKEN")
    run_test "List departments returns 200" "200" "$status"

    local count=$(echo "$body" | jq '.data | length')
    log_info "Current department count: $count"
}

# =============================================================================
# Test 2: Create Root Department
# =============================================================================

ROOT_DEPT_ID=""
CHILD_DEPT_ID=""

test_create_root_department() {
    log_section "2. Create Root Department"

    local result=$(api_call "POST" "/api/v2.1/admin/address-book/groups/" "$ADMIN_TOKEN" \
        '{"name":"Engineering"}')
    local body=$(echo "$result" | head -n -1)
    local status=$(echo "$result" | tail -1)
    run_test "Create root department returns 201" "201" "$status"

    ROOT_DEPT_ID=$(echo "$body" | jq -r '.id // empty')
    if [ -n "$ROOT_DEPT_ID" ]; then
        log_success "Created root department: $ROOT_DEPT_ID"
    else
        log_fail "No department ID in response"
        FAILED_TESTS=$((FAILED_TESTS + 1))
    fi

    run_test_contains "Response has name" '"name":"Engineering"' "$body"
}

# =============================================================================
# Test 3: Create Child Department
# =============================================================================

test_create_child_department() {
    log_section "3. Create Child Department (nested)"

    if [ -z "$ROOT_DEPT_ID" ]; then
        log_info "Skipping — no root department created"
        return
    fi

    local result=$(api_call "POST" "/api/v2.1/admin/address-book/groups/" "$ADMIN_TOKEN" \
        "{\"name\":\"Backend Team\",\"parent_group\":\"$ROOT_DEPT_ID\"}")
    local body=$(echo "$result" | head -n -1)
    local status=$(echo "$result" | tail -1)
    run_test "Create child department returns 201" "201" "$status"

    CHILD_DEPT_ID=$(echo "$body" | jq -r '.id // empty')
    if [ -n "$CHILD_DEPT_ID" ]; then
        log_success "Created child department: $CHILD_DEPT_ID"
    else
        log_fail "No department ID in response"
    fi

    run_test_contains "Response has parent_group_id" "\"parent_group_id\":\"$ROOT_DEPT_ID\"" "$body"
}

# =============================================================================
# Test 4: Create Department — Validation
# =============================================================================

test_create_department_validation() {
    log_section "4. Create Department — Validation"

    # Missing name
    local status=$(api_status "POST" "/api/v2.1/admin/address-book/groups/" "$ADMIN_TOKEN" \
        '{"name":""}')
    run_test "Empty name returns 400" "400" "$status"

    # Invalid parent UUID
    status=$(api_status "POST" "/api/v2.1/admin/address-book/groups/" "$ADMIN_TOKEN" \
        '{"name":"Test","parent_group":"not-a-uuid"}')
    run_test "Invalid parent UUID returns 400" "400" "$status"
}

# =============================================================================
# Test 5: Get Department (with members + sub-departments)
# =============================================================================

test_get_department() {
    log_section "5. Get Department (with members + sub-departments)"

    if [ -z "$ROOT_DEPT_ID" ]; then
        log_info "Skipping — no root department"
        return
    fi

    local body=$(api_body "GET" "/api/v2.1/admin/address-book/groups/${ROOT_DEPT_ID}/" "$ADMIN_TOKEN")
    local status=$(api_status "GET" "/api/v2.1/admin/address-book/groups/${ROOT_DEPT_ID}/" "$ADMIN_TOKEN")
    run_test "Get department returns 200" "200" "$status"

    run_test_contains "Response has name" '"name":"Engineering"' "$body"
    run_test_contains "Response has members array" '"members"' "$body"
    run_test_contains "Response has groups array" '"groups"' "$body"

    # Check sub-department appears
    if [ -n "$CHILD_DEPT_ID" ]; then
        run_test_contains "Sub-department listed in groups" "Backend Team" "$body"
    fi
}

# =============================================================================
# Test 6: Get Department with Ancestors
# =============================================================================

test_get_department_ancestors() {
    log_section "6. Get Department with Ancestor Breadcrumbs"

    if [ -z "$CHILD_DEPT_ID" ]; then
        log_info "Skipping — no child department"
        return
    fi

    local body=$(api_body "GET" "/api/v2.1/admin/address-book/groups/${CHILD_DEPT_ID}/?return_ancestors=true" "$ADMIN_TOKEN")
    local status=$(api_status "GET" "/api/v2.1/admin/address-book/groups/${CHILD_DEPT_ID}/?return_ancestors=true" "$ADMIN_TOKEN")
    run_test "Get child department with ancestors returns 200" "200" "$status"

    run_test_contains "Response has ancestor_groups" '"ancestor_groups"' "$body"
    run_test_contains "Ancestor includes parent name" "Engineering" "$body"
}

# =============================================================================
# Test 7: Update Department
# =============================================================================

test_update_department() {
    log_section "7. Update (Rename) Department"

    if [ -z "$ROOT_DEPT_ID" ]; then
        log_info "Skipping — no root department"
        return
    fi

    local status=$(api_status "PUT" "/api/v2.1/admin/address-book/groups/${ROOT_DEPT_ID}/" "$ADMIN_TOKEN" \
        '{"name":"Engineering Renamed"}')
    run_test "Update department returns 200" "200" "$status"

    # Small delay for Cassandra consistency
    sleep 1

    # Verify rename
    local body=$(api_body "GET" "/api/v2.1/admin/address-book/groups/${ROOT_DEPT_ID}/" "$ADMIN_TOKEN")
    run_test_contains "Department renamed" "Engineering Renamed" "$body"

    # Validation: empty name
    status=$(api_status "PUT" "/api/v2.1/admin/address-book/groups/${ROOT_DEPT_ID}/" "$ADMIN_TOKEN" \
        '{"name":""}')
    run_test "Update with empty name returns 400" "400" "$status"
}

# =============================================================================
# Test 8: List User Departments
# =============================================================================

test_list_user_departments() {
    log_section "8. List User Departments"

    local status=$(api_status "GET" "/api/v2.1/departments/" "$ADMIN_TOKEN")
    run_test "List user departments returns 200" "200" "$status"
}

# =============================================================================
# Test 9: Delete Departments
# =============================================================================

test_delete_departments() {
    log_section "9. Delete Departments"

    if [ -z "$ROOT_DEPT_ID" ]; then
        log_info "Skipping — no departments to delete"
        return
    fi

    # Try deleting parent with children (should fail)
    if [ -n "$CHILD_DEPT_ID" ]; then
        local status=$(api_status "DELETE" "/api/v2.1/admin/address-book/groups/${ROOT_DEPT_ID}/" "$ADMIN_TOKEN")
        run_test "Delete parent with children returns 409" "409" "$status"

        # Delete child first
        status=$(api_status "DELETE" "/api/v2.1/admin/address-book/groups/${CHILD_DEPT_ID}/" "$ADMIN_TOKEN")
        run_test "Delete child department returns 200" "200" "$status"

        # Wait for Cassandra consistency (tombstones need to propagate)
        sleep 3
    fi

    # Now delete root
    local status=$(api_status "DELETE" "/api/v2.1/admin/address-book/groups/${ROOT_DEPT_ID}/" "$ADMIN_TOKEN")
    run_test "Delete root department returns 200" "200" "$status"

    # Verify deleted
    status=$(api_status "GET" "/api/v2.1/admin/address-book/groups/${ROOT_DEPT_ID}/" "$ADMIN_TOKEN")
    run_test "Deleted department returns 404" "404" "$status"

    # Invalid group_id
    status=$(api_status "DELETE" "/api/v2.1/admin/address-book/groups/not-a-uuid/" "$ADMIN_TOKEN")
    run_test "Delete with invalid UUID returns 400" "400" "$status"
}

# =============================================================================
# Test 10: Search User Endpoint
# =============================================================================

test_search_user() {
    log_section "10. Search User Endpoint"

    local status=$(api_status "GET" "/api/v2.1/search-user?q=test" "$ADMIN_TOKEN")
    run_test "Search user returns 200" "200" "$status"

    local body=$(api_body "GET" "/api/v2.1/search-user?q=test" "$ADMIN_TOKEN")
    run_test_contains "Response has users array" '"users"' "$body"
}

# =============================================================================
# Test 11: Multi-Share-Links Alias
# =============================================================================

test_multi_share_links() {
    log_section "11. Multi-Share-Links Route Alias"

    local status=$(api_status "GET" "/api/v2.1/multi-share-links/" "$ADMIN_TOKEN")
    # Should return 200 (empty list) or 400 (missing params), not 404
    if [ "$status" != "404" ]; then
        TOTAL_TESTS=$((TOTAL_TESTS + 1))
        log_success "Multi-share-links route exists (got $status, not 404)"
        PASSED_TESTS=$((PASSED_TESTS + 1))
    else
        TOTAL_TESTS=$((TOTAL_TESTS + 1))
        log_fail "Multi-share-links route missing (got 404)"
        FAILED_TESTS=$((FAILED_TESTS + 1))
        FAILED_TEST_NAMES+=("Multi-share-links route exists")
    fi
}

# =============================================================================
# Test 12: Copy/Move Progress Alias
# =============================================================================

test_copy_move_progress() {
    log_section "12. Copy/Move Progress Route Alias"

    # query-copy-move-progress should resolve to the handler (returns "task not found" for fake task)
    local body=$(api_body "GET" "/api/v2.1/query-copy-move-progress/?task_id=fake" "$ADMIN_TOKEN")
    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    if echo "$body" | grep -q "task not found"; then
        log_success "query-copy-move-progress route exists (handler returned 'task not found')"
        PASSED_TESTS=$((PASSED_TESTS + 1))
    elif echo "$body" | grep -q "unauthorized"; then
        log_fail "query-copy-move-progress route — auth failed"
        FAILED_TESTS=$((FAILED_TESTS + 1))
        FAILED_TEST_NAMES+=("query-copy-move-progress route exists")
    else
        log_fail "query-copy-move-progress route unexpected response: $body"
        FAILED_TESTS=$((FAILED_TESTS + 1))
        FAILED_TEST_NAMES+=("query-copy-move-progress route exists")
    fi
}

# =============================================================================
# Summary
# =============================================================================

print_summary() {
    echo ""
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${CYAN}  Test Summary${NC}"
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
    echo -e "  Total:  $TOTAL_TESTS"
    echo -e "  ${GREEN}Passed: $PASSED_TESTS${NC}"
    echo -e "  ${RED}Failed: $FAILED_TESTS${NC}"

    if [ ${#FAILED_TEST_NAMES[@]} -gt 0 ]; then
        echo ""
        echo -e "  ${RED}Failed tests:${NC}"
        for name in "${FAILED_TEST_NAMES[@]}"; do
            echo -e "    - $name"
        done
    fi

    echo ""
    if [ $FAILED_TESTS -eq 0 ]; then
        echo -e "  ${GREEN}All tests passed!${NC}"
    else
        echo -e "  ${RED}Some tests failed.${NC}"
    fi
    echo ""
}

# =============================================================================
# Run All Tests
# =============================================================================

cleanup_stale_departments() {
    log_section "Cleanup: Remove leftover departments from prior runs"

    local body=$(api_body "GET" "/api/v2.1/admin/address-book/groups/" "$ADMIN_TOKEN")
    local dept_ids=$(echo "$body" | jq -r '.data[]?.id // empty' 2>/dev/null)

    if [ -z "$dept_ids" ]; then
        log_info "No leftover departments found"
        return
    fi

    # Delete children first (departments with parent_group_id), then roots
    local roots=""
    local children=""
    for id in $dept_ids; do
        local dept=$(echo "$body" | jq -r ".data[] | select(.id==\"$id\")")
        local parent=$(echo "$dept" | jq -r '.parent_group_id // empty')
        if [ -n "$parent" ] && [ "$parent" != "null" ] && [ "$parent" != "" ]; then
            children="$children $id"
        else
            roots="$roots $id"
        fi
    done

    for id in $children; do
        api_status "DELETE" "/api/v2.1/admin/address-book/groups/${id}/" "$ADMIN_TOKEN" > /dev/null 2>&1
        log_info "Deleted leftover child department: $id"
    done
    for id in $roots; do
        api_status "DELETE" "/api/v2.1/admin/address-book/groups/${id}/" "$ADMIN_TOKEN" > /dev/null 2>&1
        log_info "Deleted leftover root department: $id"
    done
}

main() {
    echo -e "${CYAN}SesameFS Department & Session-15 Integration Tests${NC}"
    echo -e "API URL: ${API_URL}"
    echo ""

    preflight
    cleanup_stale_departments
    test_list_departments_empty
    test_create_root_department
    test_create_child_department
    test_create_department_validation
    test_get_department
    test_get_department_ancestors
    test_update_department
    test_list_user_departments

    if [ "$QUICK_MODE" = false ]; then
        test_delete_departments
    else
        log_info "Skipping cleanup (--quick mode)"
    fi

    test_search_user
    test_multi_share_links
    test_copy_move_progress

    print_summary

    [ $FAILED_TESTS -eq 0 ]
}

main
