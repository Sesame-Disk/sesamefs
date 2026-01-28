#!/bin/bash
#
# Test script for library settings endpoints
# Tests: History limit, Auto-delete, API tokens (stub implementations)
#
# Prerequisites:
# - SesameFS backend running on localhost:8080
# - At least one library exists
#
# Usage: ./test-library-settings.sh [token] [repo_id]

set -e

TOKEN="${1:-dev-token-admin}"
REPO_ID="${2:-}"
BASE_URL="${SESAMEFS_URL:-http://localhost:8080}"

echo "==================================================="
echo "Library Settings Test"
echo "==================================================="
echo "Base URL: $BASE_URL"
echo "Token: $TOKEN"
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

pass() { echo -e "${GREEN}✓ PASS${NC}: $1"; }
fail() { echo -e "${RED}✗ FAIL${NC}: $1"; }
info() { echo -e "${YELLOW}→${NC} $1"; }

api_get() {
    curl -s -w "\n%{http_code}" -H "Authorization: Token $TOKEN" "$BASE_URL$1"
}

api_put() {
    curl -s -w "\n%{http_code}" -X PUT \
        -H "Authorization: Token $TOKEN" \
        -H "Content-Type: application/json" \
        -d "$2" "$BASE_URL$1"
}

check_response() {
    local response="$1"
    local expected_status="$2"
    local description="$3"

    status=$(echo "$response" | tail -1)
    body=$(echo "$response" | head -n -1)

    if [ "$status" = "$expected_status" ]; then
        pass "$description (status: $status)"
        echo "$body"
        return 0
    else
        fail "$description (expected $expected_status, got $status)"
        echo "    Response: $body"
        return 1
    fi
}

# Get a library if not provided
if [ -z "$REPO_ID" ]; then
    info "No repo_id provided, getting first available library..."
    libs_response=$(api_get "/api/v2.1/repos/?type=mine")
    # Our API uses repo_id, not id
    REPO_ID=$(echo "$libs_response" | head -n -1 | grep -o '"repo_id":"[^"]*"' | head -1 | cut -d'"' -f4)
fi

if [ -z "$REPO_ID" ]; then
    fail "No library found. Create one first."
    exit 1
fi

echo "Using library: $REPO_ID"
echo ""

echo "=== Test 1: GET History Limit ==="
response=$(api_get "/api2/repos/$REPO_ID/history-limit/")
check_response "$response" "200" "Get history limit"

echo ""
echo "=== Test 2: PUT History Limit ==="
info "Setting history limit to 30 days..."
response=$(api_put "/api2/repos/$REPO_ID/history-limit/" '{"keep_days": 30}')
check_response "$response" "200" "Set history limit" || true
info "Note: PUT may not persist (stub implementation)"

echo ""
echo "=== Test 3: GET Auto-Delete Settings ==="
response=$(api_get "/api/v2.1/repos/$REPO_ID/auto-delete/")
check_response "$response" "200" "Get auto-delete settings"

echo ""
echo "=== Test 4: PUT Auto-Delete Settings ==="
info "Setting auto-delete to 90 days..."
response=$(api_put "/api/v2.1/repos/$REPO_ID/auto-delete/" '{"auto_delete_days": 90}')
check_response "$response" "200" "Set auto-delete" || true
info "Note: PUT may not persist (stub implementation)"

echo ""
echo "=== Test 5: GET Repo API Tokens ==="
response=$(api_get "/api/v2.1/repos/$REPO_ID/repo-api-tokens/")
check_response "$response" "200" "Get repo API tokens"

echo ""
echo "==================================================="
echo "Library Settings Test Complete"
echo "==================================================="
echo ""
echo "Summary:"
echo "- All GET endpoints return default values (stub implementation)"
echo "- PUT endpoints accept requests but may not persist"
echo "- Full implementation would require database schema changes"
echo ""
echo "Default values returned:"
echo "  - history-limit: keep_days=-1 (keep all)"
echo "  - auto-delete: auto_delete_days=0 (disabled)"
echo "  - repo-api-tokens: [] (empty list)"
