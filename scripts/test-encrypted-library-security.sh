#!/bin/bash
#
# Test script for encrypted library security (Task #1)
# This script verifies that encrypted libraries are inaccessible without a password
#
# Prerequisites:
# - SesameFS backend running on localhost:8082
# - At least one encrypted library exists
#
# Usage: ./test-encrypted-library-security.sh [token] [encrypted_repo_id]

set -e

TOKEN="${1:-dev-token-admin}"
BASE_URL="${SESAMEFS_URL:-http://localhost:8082}"
CREATED_REPO_ID=""

# Cleanup function - delete test library on exit
cleanup() {
    if [ -n "$CREATED_REPO_ID" ]; then
        curl -s -X DELETE "${BASE_URL}/api2/repos/${CREATED_REPO_ID}/" \
            -H "Authorization: Token ${TOKEN}" > /dev/null 2>&1 || true
    fi
}
trap cleanup EXIT

echo "==================================================="
echo "Encrypted Library Security Test"
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

# Test function
test_endpoint() {
    local method="$1"
    local endpoint="$2"
    local expected_status="$3"
    local description="$4"
    local data="$5"

    info "Testing: $description"

    if [ -n "$data" ]; then
        response=$(curl -s -w "\n%{http_code}" -X "$method" \
            -H "Authorization: Token $TOKEN" \
            -H "Content-Type: application/json" \
            -d "$data" \
            "$BASE_URL$endpoint")
    else
        response=$(curl -s -w "\n%{http_code}" -X "$method" \
            -H "Authorization: Token $TOKEN" \
            "$BASE_URL$endpoint")
    fi

    status=$(echo "$response" | tail -1)
    body=$(echo "$response" | head -n -1)

    if [ "$status" = "$expected_status" ]; then
        pass "$description (got $status)"
    else
        fail "$description (expected $expected_status, got $status)"
        echo "    Response: $body"
    fi
}

# Step 1: Check if we have an encrypted library from command line
ENCRYPTED_REPO_ID="${2:-}"

if [ -z "$ENCRYPTED_REPO_ID" ]; then
    info "Step 1: Creating an encrypted library for testing..."

    # Create encrypted library with unique name
    TIMESTAMP=$(date +%s)
    create_response=$(curl -s -X POST \
        -H "Authorization: Token $TOKEN" \
        -H "Content-Type: application/json" \
        -d "{\"repo_name\":\"encrypted-test-${TIMESTAMP}\",\"encrypted\":true,\"passwd\":\"testpass123\"}" \
        "$BASE_URL/api/v2.1/repos/")

    ENCRYPTED_REPO_ID=$(echo "$create_response" | grep -o '"repo_id":"[^"]*"' | cut -d'"' -f4)

    if [ -z "$ENCRYPTED_REPO_ID" ] || [ "$ENCRYPTED_REPO_ID" = "null" ]; then
        fail "Could not create encrypted library"
        echo "Response: $create_response"
        exit 1
    fi

    CREATED_REPO_ID="$ENCRYPTED_REPO_ID"
    pass "Created encrypted library: $ENCRYPTED_REPO_ID"
else
    info "Step 1: Using provided encrypted library: $ENCRYPTED_REPO_ID"
fi

echo ""
echo "==================================================="
echo "Testing with repo: $ENCRYPTED_REPO_ID"
echo "==================================================="
echo ""

# Step 2: Test file operations WITHOUT unlocking (should fail with 403)
echo "--- Testing Access WITHOUT Password (should return 403) ---"
echo ""

# List directory
test_endpoint "GET" "/api2/repos/$ENCRYPTED_REPO_ID/dir/?p=/" "403" \
    "List directory (should be blocked)"

# Get file info
test_endpoint "GET" "/api2/repos/$ENCRYPTED_REPO_ID/file/?p=/test.txt" "403" \
    "Get file info (should be blocked)"

# Get file detail
test_endpoint "GET" "/api2/repos/$ENCRYPTED_REPO_ID/file/detail/?p=/test.txt" "403" \
    "Get file detail (should be blocked)"

# Get download link
test_endpoint "GET" "/api2/repos/$ENCRYPTED_REPO_ID/file/download-link/?p=/test.txt" "403" \
    "Get download link (should be blocked)"

# Get upload link
test_endpoint "GET" "/api2/repos/$ENCRYPTED_REPO_ID/upload-link/?p=/" "403" \
    "Get upload link (should be blocked)"

# V2.1 directory list
test_endpoint "GET" "/api/v2.1/repos/$ENCRYPTED_REPO_ID/dir/?p=/" "403" \
    "V2.1 List directory (should be blocked)"

# File history
test_endpoint "GET" "/api/v2.1/repos/$ENCRYPTED_REPO_ID/file/new_history/?path=/test.txt" "403" \
    "File history (should be blocked)"

# Create directory (should be blocked)
test_endpoint "POST" "/api2/repos/$ENCRYPTED_REPO_ID/dir/?p=/newdir" "403" \
    "Create directory (should be blocked)"

# Create file (should be blocked)
test_endpoint "POST" "/api2/repos/$ENCRYPTED_REPO_ID/file/?p=/newfile.txt&operation=create" "403" \
    "Create file (should be blocked)"

echo ""
echo "--- Testing Unlock Flow ---"
echo ""

# Step 3: Unlock the library with password
info "Unlocking library with password..."
unlock_response=$(curl -s -w "\n%{http_code}" -X POST \
    -H "Authorization: Token $TOKEN" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    -d "password=testpass123" \
    "$BASE_URL/api/v2.1/repos/$ENCRYPTED_REPO_ID/set-password/")

unlock_status=$(echo "$unlock_response" | tail -1)
unlock_body=$(echo "$unlock_response" | head -n -1)

if [ "$unlock_status" = "200" ]; then
    pass "Library unlocked successfully"

    echo ""
    echo "--- Testing Access WITH Password (should return 200) ---"
    echo ""

    # Now test the same endpoints - they should work
    test_endpoint "GET" "/api2/repos/$ENCRYPTED_REPO_ID/dir/?p=/" "200" \
        "List directory (should work after unlock)"

    test_endpoint "GET" "/api/v2.1/repos/$ENCRYPTED_REPO_ID/dir/?p=/" "200" \
        "V2.1 List directory (should work after unlock)"

    # Note: File endpoints may return 404 if no files exist, which is fine
    info "Get file endpoints may return 404 if no files exist (that's OK)"

else
    fail "Failed to unlock library: $unlock_body (status: $unlock_status)"
    echo ""
    echo "Note: If the library is not encrypted or password is wrong, this test will fail."
    echo "Create an encrypted library with password 'testpass123' and try again."
fi

echo ""
echo "--- Testing Library Deletion ---"
echo ""

# Step 4: Test that we can delete the encrypted library
if [ -n "$CREATED_REPO_ID" ]; then
    info "Deleting encrypted library: $CREATED_REPO_ID"
    delete_response=$(curl -s -w "\n%{http_code}" -X DELETE \
        -H "Authorization: Token $TOKEN" \
        "$BASE_URL/api2/repos/$CREATED_REPO_ID/")

    delete_status=$(echo "$delete_response" | tail -1)

    if [ "$delete_status" = "200" ] || [ "$delete_status" = "204" ]; then
        pass "Encrypted library deleted successfully"

        # Verify it's gone
        verify_response=$(curl -s -w "\n%{http_code}" -X GET \
            -H "Authorization: Token $TOKEN" \
            "$BASE_URL/api2/repos/$CREATED_REPO_ID/")
        verify_status=$(echo "$verify_response" | tail -1)

        if [ "$verify_status" = "404" ] || [ "$verify_status" = "403" ]; then
            pass "Deleted library is inaccessible (got $verify_status)"
        else
            fail "Deleted library still accessible (got $verify_status, expected 403 or 404)"
        fi

        # Mark as cleaned so trap doesn't try again
        CREATED_REPO_ID=""
    else
        fail "Failed to delete encrypted library (got $delete_status)"
    fi
fi

echo ""
echo "==================================================="
echo "Test Complete"
echo "==================================================="
echo ""
echo "Summary:"
echo "- Encrypted libraries should return 403 for all file operations"
echo "- After unlock (set-password), operations should work normally"
echo "- Unlock session expires after 1 hour"
echo "- Library deletion works and is verified"
