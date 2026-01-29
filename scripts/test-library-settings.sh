#!/bin/bash
#
# End-to-end test script for library settings endpoints
# Tests: History limit, Auto-delete, API tokens, Library transfer, Permission enforcement
#
# Prerequisites:
# - SesameFS backend running on localhost:8082
# - Dev tokens available (dev-token-admin, dev-token-user, dev-token-readonly)
#
# Usage: ./test-library-settings.sh [admin_token]

set -e

# Configuration
ADMIN_TOKEN="${1:-dev-token-admin}"
USER_TOKEN="dev-token-user"
READONLY_TOKEN="dev-token-readonly"
BASE_URL="${SESAMEFS_URL:-http://localhost:8082}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Counters
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0
declare -a FAILED_TEST_NAMES

echo "==================================================="
echo "Library Settings End-to-End Tests"
echo "==================================================="
echo "Base URL: $BASE_URL"
echo ""

# ============================================
# Helpers
# ============================================

pass() {
    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    PASSED_TESTS=$((PASSED_TESTS + 1))
    echo -e "${GREEN}✓ PASS${NC}: $1"
}

fail() {
    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    FAILED_TESTS=$((FAILED_TESTS + 1))
    FAILED_TEST_NAMES+=("$1")
    echo -e "${RED}✗ FAIL${NC}: $1"
}

info() { echo -e "${YELLOW}→${NC} $1"; }
section() { echo -e "\n${BLUE}=== $1 ===${NC}\n"; }

api_get() {
    curl -s -w "\n%{http_code}" -H "Authorization: Token $1" "$BASE_URL$2"
}

api_put() {
    curl -s -w "\n%{http_code}" -X PUT \
        -H "Authorization: Token $1" \
        -H "Content-Type: application/json" \
        -d "$3" "$BASE_URL$2"
}

api_post() {
    curl -s -w "\n%{http_code}" -X POST \
        -H "Authorization: Token $1" \
        -H "Content-Type: application/json" \
        -d "$3" "$BASE_URL$2"
}

api_delete() {
    curl -s -w "\n%{http_code}" -X DELETE \
        -H "Authorization: Token $1" "$BASE_URL$2"
}

# Extract status code (last line) and body (everything else)
get_status() { echo "$1" | tail -1; }
get_body() { echo "$1" | head -n -1; }

# Check response status
check_status() {
    local response="$1"
    local expected_status="$2"
    local description="$3"

    local status=$(get_status "$response")
    local body=$(get_body "$response")

    if [ "$status" = "$expected_status" ]; then
        pass "$description (status: $status)"
        return 0
    else
        fail "$description (expected $expected_status, got $status)"
        echo "    Response: $body"
        return 1
    fi
}

# Check JSON field value
check_json_field() {
    local body="$1"
    local field="$2"
    local expected="$3"
    local description="$4"

    local actual=$(echo "$body" | jq -r "$field")
    if [ "$actual" = "$expected" ]; then
        pass "$description ($field=$actual)"
    else
        fail "$description (expected $field=$expected, got $actual)"
    fi
}

# ============================================
# Setup: Create test library
# ============================================

section "Setup: Creating test library"

TIMESTAMP=$(date +%s)
LIB_NAME="test-libsettings-${TIMESTAMP}"

response=$(api_post "$ADMIN_TOKEN" "/api/v2.1/repos/" "{\"repo_name\": \"$LIB_NAME\"}")
status=$(get_status "$response")
body=$(get_body "$response")

if [ "$status" != "200" ] && [ "$status" != "201" ]; then
    echo "Failed to create test library (status: $status)"
    echo "$body"
    exit 1
fi

REPO_ID=$(echo "$body" | jq -r '.repo_id // .id')
if [ -z "$REPO_ID" ] || [ "$REPO_ID" = "null" ]; then
    echo "Failed to get repo_id from response"
    echo "$body"
    exit 1
fi

info "Created test library: $LIB_NAME ($REPO_ID)"

# Cleanup on exit
cleanup() {
    info "Cleaning up test library..."
    api_delete "$ADMIN_TOKEN" "/api/v2.1/repos/${REPO_ID}/" > /dev/null 2>&1 || true
}
trap cleanup EXIT

# ============================================
# Test 1: History Limit
# ============================================

section "Test: History Limit (GET/PUT /api2/repos/:id/history-limit/)"

# GET default history limit
response=$(api_get "$ADMIN_TOKEN" "/api2/repos/$REPO_ID/history-limit/")
check_status "$response" "200" "GET history limit - default" || true
body=$(get_body "$response")
# Default depends on server config (default_ttl_days=90 by default)
default_keep=$(echo "$body" | jq -r '.keep_days')
if [ "$default_keep" != "null" ] && [ "$default_keep" -ge -1 ] 2>/dev/null; then
    pass "Default keep_days is a valid number ($default_keep)"
else
    fail "Default keep_days should be a number, got: $default_keep"
fi

# PUT set history limit to 30 days
response=$(api_put "$ADMIN_TOKEN" "/api2/repos/$REPO_ID/history-limit/" '{"keep_days": 30}')
check_status "$response" "200" "PUT history limit to 30 days" || true
body=$(get_body "$response")
check_json_field "$body" ".keep_days" "30" "Response should confirm keep_days=30"

# GET verify history limit was persisted
response=$(api_get "$ADMIN_TOKEN" "/api2/repos/$REPO_ID/history-limit/")
body=$(get_body "$response")
check_json_field "$body" ".keep_days" "30" "GET should return persisted keep_days=30"

# PUT set to 0 (keep none)
response=$(api_put "$ADMIN_TOKEN" "/api2/repos/$REPO_ID/history-limit/" '{"keep_days": 0}')
check_status "$response" "200" "PUT history limit to 0 (keep none)" || true

# PUT set back to -1 (keep all)
response=$(api_put "$ADMIN_TOKEN" "/api2/repos/$REPO_ID/history-limit/" '{"keep_days": -1}')
check_status "$response" "200" "PUT history limit to -1 (keep all)" || true
body=$(get_body "$response")
check_json_field "$body" ".keep_days" "-1" "Response should confirm keep_days=-1"

# PUT invalid value
response=$(api_put "$ADMIN_TOKEN" "/api2/repos/$REPO_ID/history-limit/" '{"keep_days": -5}')
check_status "$response" "400" "PUT invalid keep_days should return 400" || true

# Permission test: non-owner should be denied
response=$(api_put "$READONLY_TOKEN" "/api2/repos/$REPO_ID/history-limit/" '{"keep_days": 30}')
status=$(get_status "$response")
if [ "$status" = "403" ]; then
    pass "PUT history limit as non-owner returns 403"
else
    fail "PUT history limit as non-owner should return 403, got $status"
fi

# ============================================
# Test 2: Auto-Delete Settings
# ============================================

section "Test: Auto-Delete (GET/PUT /api/v2.1/repos/:id/auto-delete/)"

# GET default auto-delete
response=$(api_get "$ADMIN_TOKEN" "/api/v2.1/repos/$REPO_ID/auto-delete/")
check_status "$response" "200" "GET auto-delete - default" || true
body=$(get_body "$response")
check_json_field "$body" ".auto_delete_days" "0" "Default auto_delete_days should be 0 (disabled)"

# PUT set auto-delete to 90 days
response=$(api_put "$ADMIN_TOKEN" "/api/v2.1/repos/$REPO_ID/auto-delete/" '{"auto_delete_days": 90}')
check_status "$response" "200" "PUT auto-delete to 90 days" || true
body=$(get_body "$response")
check_json_field "$body" ".auto_delete_days" "90" "Response should confirm auto_delete_days=90"

# GET verify auto-delete was persisted
response=$(api_get "$ADMIN_TOKEN" "/api/v2.1/repos/$REPO_ID/auto-delete/")
body=$(get_body "$response")
check_json_field "$body" ".auto_delete_days" "90" "GET should return persisted auto_delete_days=90"

# PUT disable auto-delete
response=$(api_put "$ADMIN_TOKEN" "/api/v2.1/repos/$REPO_ID/auto-delete/" '{"auto_delete_days": 0}')
check_status "$response" "200" "PUT auto-delete to 0 (disabled)" || true

# PUT invalid value
response=$(api_put "$ADMIN_TOKEN" "/api/v2.1/repos/$REPO_ID/auto-delete/" '{"auto_delete_days": -1}')
check_status "$response" "400" "PUT invalid auto_delete_days should return 400" || true

# Permission test: non-owner should be denied
response=$(api_put "$READONLY_TOKEN" "/api/v2.1/repos/$REPO_ID/auto-delete/" '{"auto_delete_days": 90}')
status=$(get_status "$response")
if [ "$status" = "403" ]; then
    pass "PUT auto-delete as non-owner returns 403"
else
    fail "PUT auto-delete as non-owner should return 403, got $status"
fi

# ============================================
# Test 3: Repo API Tokens
# ============================================

section "Test: API Tokens (GET/POST/PUT/DELETE /api/v2.1/repos/:id/repo-api-tokens/)"

# GET empty token list
response=$(api_get "$ADMIN_TOKEN" "/api/v2.1/repos/$REPO_ID/repo-api-tokens/")
check_status "$response" "200" "GET API tokens - initially empty" || true
body=$(get_body "$response")
token_count=$(echo "$body" | jq '.repo_api_tokens | length')
check_json_field "$body" ".repo_api_tokens | length" "0" "Initial token list should be empty"

# POST create token
response=$(api_post "$ADMIN_TOKEN" "/api/v2.1/repos/$REPO_ID/repo-api-tokens/" '{"app_name": "test-app", "permission": "rw"}')
check_status "$response" "200" "POST create API token" || true
body=$(get_body "$response")
check_json_field "$body" ".app_name" "test-app" "Created token should have correct app_name"
check_json_field "$body" ".permission" "rw" "Created token should have correct permission"
API_TOKEN_VALUE=$(echo "$body" | jq -r '.api_token')
if [ -n "$API_TOKEN_VALUE" ] && [ "$API_TOKEN_VALUE" != "null" ] && [ ${#API_TOKEN_VALUE} -eq 64 ]; then
    pass "Generated API token is 64-char hex string"
else
    fail "Generated API token should be 64-char hex string, got: $API_TOKEN_VALUE"
fi

# POST create second token
response=$(api_post "$ADMIN_TOKEN" "/api/v2.1/repos/$REPO_ID/repo-api-tokens/" '{"app_name": "test-app-2", "permission": "r"}')
check_status "$response" "200" "POST create second API token with read-only permission" || true

# POST duplicate app_name should fail
response=$(api_post "$ADMIN_TOKEN" "/api/v2.1/repos/$REPO_ID/repo-api-tokens/" '{"app_name": "test-app", "permission": "rw"}')
check_status "$response" "409" "POST duplicate app_name returns 409 conflict" || true

# POST missing app_name should fail
response=$(api_post "$ADMIN_TOKEN" "/api/v2.1/repos/$REPO_ID/repo-api-tokens/" '{"permission": "rw"}')
check_status "$response" "400" "POST missing app_name returns 400" || true

# POST invalid permission should fail
response=$(api_post "$ADMIN_TOKEN" "/api/v2.1/repos/$REPO_ID/repo-api-tokens/" '{"app_name": "bad-perm", "permission": "admin"}')
check_status "$response" "400" "POST invalid permission returns 400" || true

# GET verify token list
response=$(api_get "$ADMIN_TOKEN" "/api/v2.1/repos/$REPO_ID/repo-api-tokens/")
body=$(get_body "$response")
token_count=$(echo "$body" | jq '.repo_api_tokens | length')
if [ "$token_count" = "2" ]; then
    pass "GET API tokens returns 2 tokens"
else
    fail "GET API tokens should return 2 tokens, got $token_count"
fi

# PUT update token permission
response=$(api_put "$ADMIN_TOKEN" "/api/v2.1/repos/$REPO_ID/repo-api-tokens/test-app/" '{"permission": "r"}')
check_status "$response" "200" "PUT update token permission to read-only" || true

# DELETE token
response=$(api_delete "$ADMIN_TOKEN" "/api/v2.1/repos/$REPO_ID/repo-api-tokens/test-app-2/")
check_status "$response" "200" "DELETE API token" || true

# Verify deletion
response=$(api_get "$ADMIN_TOKEN" "/api/v2.1/repos/$REPO_ID/repo-api-tokens/")
body=$(get_body "$response")
token_count=$(echo "$body" | jq '.repo_api_tokens | length')
if [ "$token_count" = "1" ]; then
    pass "After deletion, token list has 1 token"
else
    fail "After deletion, token list should have 1 token, got $token_count"
fi

# Permission test: non-owner should be denied
response=$(api_get "$READONLY_TOKEN" "/api/v2.1/repos/$REPO_ID/repo-api-tokens/")
status=$(get_status "$response")
if [ "$status" = "403" ]; then
    pass "GET API tokens as non-owner returns 403"
else
    fail "GET API tokens as non-owner should return 403, got $status"
fi

response=$(api_post "$READONLY_TOKEN" "/api/v2.1/repos/$REPO_ID/repo-api-tokens/" '{"app_name": "hack", "permission": "rw"}')
status=$(get_status "$response")
if [ "$status" = "403" ]; then
    pass "POST API token as non-owner returns 403"
else
    fail "POST API token as non-owner should return 403, got $status"
fi

# ============================================
# Test 4: Library Permission Fields
# ============================================

section "Test: Permission fields in library detail response"

# Owner should get correct permissions
response=$(api_get "$ADMIN_TOKEN" "/api/v2.1/repos/$REPO_ID")
body=$(get_body "$response")
check_json_field "$body" ".permission" "rw" "Owner permission should be 'rw'"
is_admin=$(echo "$body" | jq -r '.is_admin')
if [ "$is_admin" = "true" ]; then
    pass "Owner is_admin should be true"
else
    fail "Owner is_admin should be true, got $is_admin"
fi

# Non-owner/non-shared user should get 403
response=$(api_get "$READONLY_TOKEN" "/api/v2.1/repos/$REPO_ID")
status=$(get_status "$response")
if [ "$status" = "403" ]; then
    pass "Non-shared user cannot access library detail"
else
    fail "Non-shared user should get 403, got $status"
fi

# ============================================
# Test 5: Library Transfer
# ============================================

section "Test: Library Transfer (PUT /api2/repos/:id/owner/)"

# First, get the user's email
user_info=$(curl -s -H "Authorization: Token $USER_TOKEN" "$BASE_URL/api2/account/info/")
USER_EMAIL=$(echo "$user_info" | jq -r '.email')
info "User email: $USER_EMAIL"

if [ -n "$USER_EMAIL" ] && [ "$USER_EMAIL" != "null" ]; then
    # Transfer library to user
    response=$(api_put "$ADMIN_TOKEN" "/api2/repos/$REPO_ID/owner/" "{\"owner\": \"$USER_EMAIL\"}")
    check_status "$response" "200" "Transfer library to user" || true

    # Verify new owner can access the library
    response=$(api_get "$USER_TOKEN" "/api/v2.1/repos/$REPO_ID")
    check_status "$response" "200" "New owner can access transferred library" || true
    body=$(get_body "$response")
    # owner_email may be user's email or UUID-based email depending on system
    new_owner_email=$(echo "$body" | jq -r '.owner_email')
    if [ "$new_owner_email" = "$USER_EMAIL" ] || echo "$new_owner_email" | grep -q "000000000002"; then
        pass "Library owner_email changed to new owner ($new_owner_email)"
    else
        fail "Library owner_email should reference new owner, got $new_owner_email"
    fi

    # Verify old owner can no longer modify settings
    response=$(api_put "$ADMIN_TOKEN" "/api/v2.1/repos/$REPO_ID/auto-delete/" '{"auto_delete_days": 30}')
    status=$(get_status "$response")
    if [ "$status" = "403" ]; then
        pass "Old owner cannot modify transferred library settings"
    else
        fail "Old owner should get 403 on transferred library, got $status"
    fi

    # Transfer back for cleanup
    response=$(api_put "$USER_TOKEN" "/api2/repos/$REPO_ID/owner/" "{\"owner\": \"admin@sesamefs.local\"}")
    if [ "$(get_status "$response")" = "200" ]; then
        info "Transferred library back for cleanup"
    fi
else
    info "Skipping transfer tests - could not determine user email"
fi

# Transfer to non-existent user should fail
response=$(api_put "$ADMIN_TOKEN" "/api2/repos/$REPO_ID/owner/" '{"owner": "nonexistent@example.com"}')
check_status "$response" "404" "Transfer to non-existent user returns 404" || true

# Non-owner cannot transfer
response=$(api_put "$READONLY_TOKEN" "/api2/repos/$REPO_ID/owner/" "{\"owner\": \"$USER_EMAIL\"}")
status=$(get_status "$response")
if [ "$status" = "403" ]; then
    pass "Non-owner cannot transfer library"
else
    fail "Non-owner transfer should return 403, got $status"
fi

# ============================================
# Summary
# ============================================

echo ""
echo "==================================================="
echo "Library Settings Test Results"
echo "==================================================="
echo -e "Total:  $TOTAL_TESTS"
echo -e "Passed: ${GREEN}$PASSED_TESTS${NC}"
echo -e "Failed: ${RED}$FAILED_TESTS${NC}"

if [ $FAILED_TESTS -gt 0 ]; then
    echo ""
    echo "Failed tests:"
    for name in "${FAILED_TEST_NAMES[@]}"; do
        echo -e "  ${RED}✗${NC} $name"
    done
    echo ""
    exit 1
fi

echo ""
echo -e "${GREEN}All tests passed!${NC}"
echo ""
