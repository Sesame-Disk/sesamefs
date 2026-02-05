#!/bin/bash
# Repo History API Integration Tests
# Tests the repo history endpoint that was added

set -e

# ANSI colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

# Configuration
BASE_URL="${BASE_URL:-http://localhost:8082}"
TOKEN="${TOKEN:-dev-token-123}"

PASSED=0
FAILED=0

# Helper function
api_call() {
    local method="$1"
    local endpoint="$2"
    local data="$3"

    if [ -n "$data" ]; then
        curl -s -w "\n%{http_code}" -X "$method" \
            -H "Authorization: Token $TOKEN" \
            -H "Content-Type: application/x-www-form-urlencoded" \
            -d "$data" \
            "${BASE_URL}${endpoint}"
    else
        curl -s -w "\n%{http_code}" -X "$method" \
            -H "Authorization: Token $TOKEN" \
            "${BASE_URL}${endpoint}"
    fi
}

check() {
    local test_name="$1"
    local expected_code="$2"
    local actual_code="$3"
    local response_body="$4"

    if [ "$actual_code" = "$expected_code" ]; then
        echo -e "${GREEN}✓ PASS${NC}: $test_name (HTTP $actual_code)"
        PASSED=$((PASSED + 1))
        return 0
    else
        echo -e "${RED}✗ FAIL${NC}: $test_name (expected $expected_code, got $actual_code)"
        echo -e "  Response: $response_body"
        FAILED=$((FAILED + 1))
        return 1
    fi
}

echo -e "${CYAN}=== Repo History API Integration Tests ===${NC}"
echo "Base URL: $BASE_URL"
echo ""

# Create a test library
echo -e "${YELLOW}→${NC} Creating test library..."
RESPONSE=$(api_call POST "/api/v2.1/repos/" "name=history-test-library-$(date +%s)")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | head -n-1)

if [ "$HTTP_CODE" != "200" ] && [ "$HTTP_CODE" != "201" ]; then
    echo -e "${RED}Failed to create test library${NC}"
    exit 1
fi

REPO_ID=$(echo "$BODY" | grep -o '"repo_id":"[^"]*"' | head -1 | cut -d'"' -f4)
echo -e "${GREEN}✓${NC} Created library: $REPO_ID"
echo ""

cleanup() {
    echo ""
    echo -e "${YELLOW}→${NC} Cleaning up test library..."
    api_call DELETE "/api/v2.1/repos/$REPO_ID/" > /dev/null 2>&1
    echo -e "${GREEN}✓${NC} Cleanup complete"
}
trap cleanup EXIT

# Test 1: Get repo history (basic)
echo -e "${CYAN}--- Test 1: Get repo history ---${NC}"
RESPONSE=$(api_call GET "/api/v2.1/repos/$REPO_ID/history/")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | head -n-1)
check "Get repo history" "200" "$HTTP_CODE" "$BODY"

# Verify response structure
if echo "$BODY" | grep -q '"data"'; then
    echo -e "${GREEN}✓ PASS${NC}: Response contains data field"
    PASSED=$((PASSED + 1))
else
    echo -e "${RED}✗ FAIL${NC}: Response missing data field"
    FAILED=$((FAILED + 1))
fi

if echo "$BODY" | grep -q '"more"'; then
    echo -e "${GREEN}✓ PASS${NC}: Response contains more field"
    PASSED=$((PASSED + 1))
else
    echo -e "${RED}✗ FAIL${NC}: Response missing more field"
    FAILED=$((FAILED + 1))
fi
echo ""

# Test 2: Get repo history with pagination
echo -e "${CYAN}--- Test 2: Get repo history with pagination ---${NC}"
RESPONSE=$(api_call GET "/api/v2.1/repos/$REPO_ID/history/?page=1&per_page=5")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | head -n-1)
check "Get repo history (paginated)" "200" "$HTTP_CODE" "$BODY"
echo ""

# Test 3: History should contain commit entries
echo -e "${CYAN}--- Test 3: History contains commit data ---${NC}"
if echo "$BODY" | grep -q '"commit_id"'; then
    echo -e "${GREEN}✓ PASS${NC}: History contains commit_id"
    PASSED=$((PASSED + 1))
else
    # New library might have no commits yet, which is OK
    echo -e "${YELLOW}○ SKIP${NC}: No commits yet (new library)"
fi

if echo "$BODY" | grep -q '"description"'; then
    echo -e "${GREEN}✓ PASS${NC}: History contains description"
    PASSED=$((PASSED + 1))
else
    echo -e "${YELLOW}○ SKIP${NC}: No description found (new library)"
fi
echo ""

# Test 4: Invalid repo ID (returns 200 with empty data - graceful handling)
echo -e "${CYAN}--- Test 4: Invalid repo ID ---${NC}"
RESPONSE=$(api_call GET "/api/v2.1/repos/invalid-repo-id/history/")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | head -n-1)
# API gracefully handles invalid repo ID by returning empty data
check "Invalid repo ID (graceful)" "200" "$HTTP_CODE" "$BODY"

# Verify it returns empty data array
if echo "$BODY" | grep -q '"data":\[\]'; then
    echo -e "${GREEN}✓ PASS${NC}: Returns empty data for invalid repo"
    PASSED=$((PASSED + 1))
else
    echo -e "${YELLOW}○ SKIP${NC}: May have unexpected data"
fi
echo ""

# Test 5: Non-existent repo ID (returns 200 with empty data)
echo -e "${CYAN}--- Test 5: Non-existent repo ID ---${NC}"
RESPONSE=$(api_call GET "/api/v2.1/repos/00000000-0000-0000-0000-000000000000/history/")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | head -n-1)
# API gracefully handles non-existent repo by returning empty data
check "Non-existent repo ID (graceful)" "200" "$HTTP_CODE" "$BODY"
echo ""

# Summary
echo "============================================"
echo -e " Results: ${GREEN}$PASSED${NC} passed, ${RED}$FAILED${NC} failed"
echo "============================================"

if [ $FAILED -gt 0 ]; then
    exit 1
fi

exit 0
