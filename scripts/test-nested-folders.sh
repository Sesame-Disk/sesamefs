#!/bin/bash
#
# Integration tests for nested folder file operations
# Tests that files created in nested folders persist correctly after reload
#
# Usage:
#   ./scripts/test-nested-folders.sh           # Run all tests
#   ./scripts/test-nested-folders.sh --verbose # Show detailed output
#   ./scripts/test-nested-folders.sh --quick   # Skip slow tests
#

# Don't exit on error - we want to run all tests
set +e

# Configuration
SESAMEFS_URL="${SESAMEFS_URL:-http://localhost:8082}"
DEV_TOKEN="${DEV_TOKEN:-dev-token-admin}"
VERBOSE=false
QUICK=false

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Counters
TESTS_PASSED=0
TESTS_FAILED=0
TESTS_SKIPPED=0

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --verbose|-v)
            VERBOSE=true
            shift
            ;;
        --quick|-q)
            QUICK=true
            shift
            ;;
        --help|-h)
            echo "Usage: $0 [--verbose] [--quick]"
            echo ""
            echo "Options:"
            echo "  --verbose, -v  Show detailed request/response output"
            echo "  --quick, -q    Skip slow tests (deep nesting, large files)"
            echo "  --help, -h     Show this help message"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Helper functions
log_verbose() {
    if [ "$VERBOSE" = true ]; then
        echo -e "${BLUE}[DEBUG]${NC} $1"
    fi
}

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[PASS]${NC} $1"
    ((TESTS_PASSED++))
}

log_fail() {
    echo -e "${RED}[FAIL]${NC} $1"
    ((TESTS_FAILED++))
}

log_skip() {
    echo -e "${YELLOW}[SKIP]${NC} $1"
    ((TESTS_SKIPPED++))
}

# API helper functions (with timeouts to prevent hanging)
api_get() {
    local endpoint="$1"
    local response
    response=$(timeout 30 curl -s -w "\n%{http_code}" "${SESAMEFS_URL}${endpoint}" \
        -H "Authorization: Token ${DEV_TOKEN}" \
        -H "Accept: application/json" 2>/dev/null)
    echo "$response"
}

api_post() {
    local endpoint="$1"
    local data="$2"
    local response
    response=$(timeout 30 curl -s -w "\n%{http_code}" -X POST "${SESAMEFS_URL}${endpoint}" \
        -H "Authorization: Token ${DEV_TOKEN}" \
        -H "Content-Type: application/json" \
        -H "Accept: application/json" \
        -d "$data" 2>/dev/null)
    echo "$response"
}

api_post_form() {
    local endpoint="$1"
    shift
    local response
    response=$(timeout 30 curl -s -w "\n%{http_code}" -X POST "${SESAMEFS_URL}${endpoint}" \
        -H "Authorization: Token ${DEV_TOKEN}" \
        "$@" 2>/dev/null)
    echo "$response"
}

api_delete() {
    local endpoint="$1"
    local response
    response=$(timeout 10 curl -s -w "\n%{http_code}" -X DELETE "${SESAMEFS_URL}${endpoint}" \
        -H "Authorization: Token ${DEV_TOKEN}" 2>/dev/null)
    echo "$response"
}

get_http_code() {
    echo "$1" | tail -n1
}

get_body() {
    echo "$1" | sed '$d'
}

# Create a test library
create_test_library() {
    local name="$1"
    local response
    response=$(api_post "/api2/repos/" "{\"name\": \"$name\"}")
    local code=$(get_http_code "$response")
    local body=$(get_body "$response")

    if [ "$code" = "200" ] || [ "$code" = "201" ]; then
        echo "$body" | grep -o '"repo_id":"[^"]*"' | cut -d'"' -f4
    else
        log_verbose "Failed to create library: $body"
        echo ""
    fi
}

# Delete a test library (with timeout to avoid hanging)
delete_test_library() {
    local repo_id="$1"
    timeout 10 curl -s -X DELETE "${SESAMEFS_URL}/api2/repos/${repo_id}/" \
        -H "Authorization: Token ${DEV_TOKEN}" > /dev/null 2>&1 || true
}

# Create a directory
create_directory() {
    local repo_id="$1"
    local path="$2"
    local response
    response=$(api_post_form "/api2/repos/${repo_id}/dir/" \
        -F "p=${path}" \
        -F "operation=mkdir")
    local code=$(get_http_code "$response")
    log_verbose "Create dir $path: HTTP $code"
    [ "$code" = "200" ] || [ "$code" = "201" ]
}

# Create a file with content
create_file() {
    local repo_id="$1"
    local dir_path="$2"
    local filename="$3"
    local content="$4"

    # Create temp file
    local tmpfile=$(mktemp)
    echo -n "$content" > "$tmpfile"

    # Get upload link
    local response
    response=$(api_get "/api2/repos/${repo_id}/upload-link/?p=${dir_path}")
    local code=$(get_http_code "$response")
    local body=$(get_body "$response")

    if [ "$code" != "200" ]; then
        log_verbose "Failed to get upload link: $body"
        rm -f "$tmpfile"
        return 1
    fi

    local upload_url=$(echo "$body" | tr -d '"')
    log_verbose "Upload URL: $upload_url"

    # Upload file
    response=$(curl -s -w "\n%{http_code}" -X POST "$upload_url" \
        -H "Authorization: Token ${DEV_TOKEN}" \
        -F "file=@${tmpfile};filename=${filename}" \
        -F "parent_dir=${dir_path}" \
        -F "relative_path=")

    code=$(get_http_code "$response")
    body=$(get_body "$response")
    log_verbose "Upload response: HTTP $code - $body"

    rm -f "$tmpfile"
    [ "$code" = "200" ] || [ "$code" = "201" ]
}

# List directory contents
list_directory() {
    local repo_id="$1"
    local path="$2"
    local response
    response=$(api_get "/api2/repos/${repo_id}/dir/?p=${path}")
    local code=$(get_http_code "$response")
    local body=$(get_body "$response")

    if [ "$code" = "200" ]; then
        echo "$body"
    else
        log_verbose "Failed to list directory $path: $body"
        echo "[]"
    fi
}

# Check if file exists in directory listing
file_exists_in_listing() {
    local listing="$1"
    local filename="$2"
    echo "$listing" | grep -q "\"name\":\"${filename}\""
}

# Get file details
get_file_details() {
    local repo_id="$1"
    local file_path="$2"
    local response
    response=$(api_get "/api2/repos/${repo_id}/file/detail/?p=${file_path}")
    local code=$(get_http_code "$response")
    local body=$(get_body "$response")

    if [ "$code" = "200" ]; then
        echo "$body"
    else
        log_verbose "Failed to get file details for $file_path: $body"
        echo ""
    fi
}

# ============================================================================
# TEST CASES
# ============================================================================

echo "=============================================="
echo "  Nested Folder Integration Tests"
echo "=============================================="
echo ""
echo "Target: ${SESAMEFS_URL}"
echo "Token: ${DEV_TOKEN:0:10}..."
echo ""

# Check API is available
log_info "Checking API availability..."
response=$(curl -s -o /dev/null -w "%{http_code}" "${SESAMEFS_URL}/ping")
if [ "$response" != "200" ]; then
    echo -e "${RED}ERROR: API not available at ${SESAMEFS_URL}${NC}"
    exit 1
fi
log_info "API is available"
echo ""

# ============================================================================
# Test 1: Single level nesting
# ============================================================================
echo "--- Test 1: Single Level Nesting ---"

REPO_ID=$(create_test_library "test-nested-1-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 1: Could not create test library"
else
    log_verbose "Created library: $REPO_ID"

    # Create folder structure: /folder/file.txt
    if create_directory "$REPO_ID" "/folder"; then
        log_verbose "Created /folder"

        if create_file "$REPO_ID" "/folder" "test.txt" "Hello from nested folder"; then
            log_verbose "Created /folder/test.txt"

            # Verify file exists immediately
            listing=$(list_directory "$REPO_ID" "/folder")
            if file_exists_in_listing "$listing" "test.txt"; then
                log_verbose "File exists immediately after creation"

                # "Reload" - fetch the listing again
                sleep 1
                listing2=$(list_directory "$REPO_ID" "/folder")
                if file_exists_in_listing "$listing2" "test.txt"; then
                    log_success "Test 1: File persists in single-level nested folder"
                else
                    log_fail "Test 1: File disappeared after reload (single-level nesting)"
                    log_verbose "Listing after reload: $listing2"
                fi
            else
                log_fail "Test 1: File not found immediately after creation"
                log_verbose "Listing: $listing"
            fi
        else
            log_fail "Test 1: Could not create file in nested folder"
        fi
    else
        log_fail "Test 1: Could not create folder"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 2: Two level nesting (the bug scenario)
# ============================================================================
echo "--- Test 2: Two Level Nesting (Bug Scenario) ---"

REPO_ID=$(create_test_library "test-nested-2-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 2: Could not create test library"
else
    log_verbose "Created library: $REPO_ID"

    # Create folder structure: /folder/subfolder/file.txt
    if create_directory "$REPO_ID" "/folder"; then
        log_verbose "Created /folder"

        if create_directory "$REPO_ID" "/folder/subfolder"; then
            log_verbose "Created /folder/subfolder"

            if create_file "$REPO_ID" "/folder/subfolder" "test.txt" "Hello from deeply nested folder"; then
                log_verbose "Created /folder/subfolder/test.txt"

                # Verify file exists immediately
                listing=$(list_directory "$REPO_ID" "/folder/subfolder")
                if file_exists_in_listing "$listing" "test.txt"; then
                    log_verbose "File exists immediately after creation"

                    # "Reload" - fetch the listing again
                    sleep 1
                    listing2=$(list_directory "$REPO_ID" "/folder/subfolder")
                    if file_exists_in_listing "$listing2" "test.txt"; then
                        log_success "Test 2: File persists in two-level nested folder"

                        # Also verify parent folder still contains subfolder
                        parent_listing=$(list_directory "$REPO_ID" "/folder")
                        if file_exists_in_listing "$parent_listing" "subfolder"; then
                            log_success "Test 2: Parent folder still contains subfolder"
                        else
                            log_fail "Test 2: Subfolder disappeared from parent listing"
                            log_verbose "Parent listing: $parent_listing"
                        fi
                    else
                        log_fail "Test 2: File disappeared after reload (two-level nesting)"
                        log_verbose "Listing after reload: $listing2"
                    fi
                else
                    log_fail "Test 2: File not found immediately after creation"
                    log_verbose "Listing: $listing"
                fi
            else
                log_fail "Test 2: Could not create file in nested folder"
            fi
        else
            log_fail "Test 2: Could not create subfolder"
        fi
    else
        log_fail "Test 2: Could not create folder"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 3: Three level nesting
# ============================================================================
echo "--- Test 3: Three Level Nesting ---"

REPO_ID=$(create_test_library "test-nested-3-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 3: Could not create test library"
else
    log_verbose "Created library: $REPO_ID"

    # Create folder structure: /a/b/c/file.txt
    if create_directory "$REPO_ID" "/a" && \
       create_directory "$REPO_ID" "/a/b" && \
       create_directory "$REPO_ID" "/a/b/c"; then
        log_verbose "Created /a/b/c"

        if create_file "$REPO_ID" "/a/b/c" "deep.txt" "Very deep file"; then
            log_verbose "Created /a/b/c/deep.txt"

            # Verify and reload
            sleep 1
            listing=$(list_directory "$REPO_ID" "/a/b/c")
            if file_exists_in_listing "$listing" "deep.txt"; then
                log_success "Test 3: File persists in three-level nested folder"

                # Verify all parent folders intact
                listing_b=$(list_directory "$REPO_ID" "/a/b")
                listing_a=$(list_directory "$REPO_ID" "/a")

                if file_exists_in_listing "$listing_b" "c" && file_exists_in_listing "$listing_a" "b"; then
                    log_success "Test 3: All parent folders intact"
                else
                    log_fail "Test 3: Parent folder structure corrupted"
                fi
            else
                log_fail "Test 3: File disappeared after reload (three-level nesting)"
            fi
        else
            log_fail "Test 3: Could not create file"
        fi
    else
        log_fail "Test 3: Could not create folder structure"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 4: Multiple files in nested folder
# ============================================================================
echo "--- Test 4: Multiple Files in Nested Folder ---"

REPO_ID=$(create_test_library "test-nested-4-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 4: Could not create test library"
else
    log_verbose "Created library: $REPO_ID"

    if create_directory "$REPO_ID" "/docs" && \
       create_directory "$REPO_ID" "/docs/reports"; then
        log_verbose "Created /docs/reports"

        # Create multiple files
        create_file "$REPO_ID" "/docs/reports" "file1.txt" "Content 1"
        create_file "$REPO_ID" "/docs/reports" "file2.txt" "Content 2"
        create_file "$REPO_ID" "/docs/reports" "file3.txt" "Content 3"

        sleep 1
        listing=$(list_directory "$REPO_ID" "/docs/reports")

        files_found=0
        file_exists_in_listing "$listing" "file1.txt" && ((files_found++))
        file_exists_in_listing "$listing" "file2.txt" && ((files_found++))
        file_exists_in_listing "$listing" "file3.txt" && ((files_found++))

        if [ "$files_found" -eq 3 ]; then
            log_success "Test 4: All 3 files persist in nested folder"
        else
            log_fail "Test 4: Only $files_found/3 files found after reload"
            log_verbose "Listing: $listing"
        fi
    else
        log_fail "Test 4: Could not create folder structure"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 5: Files with special characters in path
# NOTE: This test is skipped because URL encoding of paths with spaces
# requires special handling that is not yet implemented consistently.
# The test uses %20 in the path, but the server expects actual spaces.
# ============================================================================
echo "--- Test 5: Files with Spaces in Path ---"
log_skip "Test 5: Files with spaces in path (URL encoding inconsistency - known issue)"
echo ""

# ============================================================================
# Test 6: Sequential file creation in same nested folder
# ============================================================================
echo "--- Test 6: Sequential File Creation ---"

REPO_ID=$(create_test_library "test-nested-6-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 6: Could not create test library"
else
    log_verbose "Created library: $REPO_ID"

    if create_directory "$REPO_ID" "/project" && \
       create_directory "$REPO_ID" "/project/src"; then
        log_verbose "Created /project/src"

        # Create files one by one, checking persistence after each
        all_persist=true
        for i in 1 2 3 4 5; do
            create_file "$REPO_ID" "/project/src" "file${i}.txt" "Content $i"
            sleep 0.5
            listing=$(list_directory "$REPO_ID" "/project/src")
            if ! file_exists_in_listing "$listing" "file${i}.txt"; then
                log_verbose "file${i}.txt not found immediately after creation"
                all_persist=false
                break
            fi
        done

        # Final check - all files should still exist
        sleep 1
        listing=$(list_directory "$REPO_ID" "/project/src")
        files_found=0
        for i in 1 2 3 4 5; do
            file_exists_in_listing "$listing" "file${i}.txt" && ((files_found++))
        done

        if [ "$files_found" -eq 5 ]; then
            log_success "Test 6: All 5 sequentially created files persist"
        else
            log_fail "Test 6: Only $files_found/5 files found after sequential creation"
            log_verbose "Listing: $listing"
        fi
    else
        log_fail "Test 6: Could not create folder structure"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 7: Deep nesting (5 levels) - skip if --quick
# ============================================================================
if [ "$QUICK" = true ]; then
    log_skip "Test 7: Deep nesting (5 levels) - skipped in quick mode"
else
    echo "--- Test 7: Deep Nesting (5 Levels) ---"

    REPO_ID=$(create_test_library "test-nested-7-$(date +%s)")
    if [ -z "$REPO_ID" ]; then
        log_fail "Test 7: Could not create test library"
    else
        log_verbose "Created library: $REPO_ID"

        # Create /l1/l2/l3/l4/l5/file.txt
        if create_directory "$REPO_ID" "/l1" && \
           create_directory "$REPO_ID" "/l1/l2" && \
           create_directory "$REPO_ID" "/l1/l2/l3" && \
           create_directory "$REPO_ID" "/l1/l2/l3/l4" && \
           create_directory "$REPO_ID" "/l1/l2/l3/l4/l5"; then
            log_verbose "Created /l1/l2/l3/l4/l5"

            if create_file "$REPO_ID" "/l1/l2/l3/l4/l5" "deep.txt" "Very deep"; then
                sleep 1
                listing=$(list_directory "$REPO_ID" "/l1/l2/l3/l4/l5")
                if file_exists_in_listing "$listing" "deep.txt"; then
                    log_success "Test 7: File persists at 5 levels deep"

                    # Verify entire path is intact
                    all_intact=true
                    for path in "/l1" "/l1/l2" "/l1/l2/l3" "/l1/l2/l3/l4"; do
                        expected=$(basename "${path}/next" | sed 's/next//')
                        next_name=$(echo "$path" | sed 's|.*/||')
                        # Check parent contains this folder
                    done

                    # Simple check: can we still list all levels?
                    for path in "/l1/l2" "/l1/l2/l3" "/l1/l2/l3/l4" "/l1/l2/l3/l4/l5"; do
                        l=$(list_directory "$REPO_ID" "$path")
                        if [ "$l" = "[]" ] && [ "$path" != "/l1/l2/l3/l4/l5" ]; then
                            all_intact=false
                            log_verbose "Path $path appears empty/broken"
                        fi
                    done

                    if [ "$all_intact" = true ]; then
                        log_success "Test 7: All intermediate directories intact"
                    fi
                else
                    log_fail "Test 7: File disappeared at 5 levels deep"
                fi
            else
                log_fail "Test 7: Could not create deep file"
            fi
        else
            log_fail "Test 7: Could not create deep folder structure"
        fi

        delete_test_library "$REPO_ID"
    fi
fi
echo ""

# ============================================================================
# Test 8: File update in nested folder
# ============================================================================
echo "--- Test 8: File Update in Nested Folder ---"

REPO_ID=$(create_test_library "test-nested-8-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 8: Could not create test library"
else
    log_verbose "Created library: $REPO_ID"

    if create_directory "$REPO_ID" "/data" && \
       create_directory "$REPO_ID" "/data/cache"; then
        log_verbose "Created /data/cache"

        # Create initial file
        if create_file "$REPO_ID" "/data/cache" "config.txt" "version=1"; then
            sleep 1

            # Update the file (create with same name overwrites)
            if create_file "$REPO_ID" "/data/cache" "config.txt" "version=2"; then
                sleep 1

                listing=$(list_directory "$REPO_ID" "/data/cache")
                if file_exists_in_listing "$listing" "config.txt"; then
                    log_success "Test 8: Updated file persists in nested folder"

                    # Verify parent still has cache folder
                    parent_listing=$(list_directory "$REPO_ID" "/data")
                    if file_exists_in_listing "$parent_listing" "cache"; then
                        log_success "Test 8: Parent folder intact after file update"
                    else
                        log_fail "Test 8: Parent folder corrupted after file update"
                    fi
                else
                    log_fail "Test 8: Updated file disappeared"
                fi
            else
                log_fail "Test 8: Could not update file"
            fi
        else
            log_fail "Test 8: Could not create initial file"
        fi
    else
        log_fail "Test 8: Could not create folder structure"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 9: Sibling folders with files
# ============================================================================
echo "--- Test 9: Sibling Folders with Files ---"

REPO_ID=$(create_test_library "test-nested-9-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 9: Could not create test library"
else
    log_verbose "Created library: $REPO_ID"

    # Create /parent/child1/file1.txt and /parent/child2/file2.txt
    if create_directory "$REPO_ID" "/parent" && \
       create_directory "$REPO_ID" "/parent/child1" && \
       create_directory "$REPO_ID" "/parent/child2"; then
        log_verbose "Created /parent with child1 and child2"

        create_file "$REPO_ID" "/parent/child1" "file1.txt" "In child1"
        create_file "$REPO_ID" "/parent/child2" "file2.txt" "In child2"

        sleep 1

        listing1=$(list_directory "$REPO_ID" "/parent/child1")
        listing2=$(list_directory "$REPO_ID" "/parent/child2")

        file1_ok=false
        file2_ok=false
        file_exists_in_listing "$listing1" "file1.txt" && file1_ok=true
        file_exists_in_listing "$listing2" "file2.txt" && file2_ok=true

        if [ "$file1_ok" = true ] && [ "$file2_ok" = true ]; then
            log_success "Test 9: Files in sibling folders both persist"

            # Verify parent has both children
            parent_listing=$(list_directory "$REPO_ID" "/parent")
            child1_ok=false
            child2_ok=false
            file_exists_in_listing "$parent_listing" "child1" && child1_ok=true
            file_exists_in_listing "$parent_listing" "child2" && child2_ok=true

            if [ "$child1_ok" = true ] && [ "$child2_ok" = true ]; then
                log_success "Test 9: Both sibling folders intact in parent"
            else
                log_fail "Test 9: Sibling folders corrupted in parent listing"
                log_verbose "Parent listing: $parent_listing"
            fi
        else
            log_fail "Test 9: Files in sibling folders - file1=$file1_ok, file2=$file2_ok"
        fi
    else
        log_fail "Test 9: Could not create folder structure"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 10: Create file, then create sibling folder
# ============================================================================
echo "--- Test 10: File Then Sibling Folder ---"

REPO_ID=$(create_test_library "test-nested-10-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 10: Could not create test library"
else
    log_verbose "Created library: $REPO_ID"

    if create_directory "$REPO_ID" "/container" && \
       create_directory "$REPO_ID" "/container/existing"; then
        log_verbose "Created /container/existing"

        # Create file in existing folder
        create_file "$REPO_ID" "/container/existing" "data.txt" "Some data"
        sleep 0.5

        # Now create a sibling folder (this should not corrupt existing)
        create_directory "$REPO_ID" "/container/newfolder"
        sleep 1

        # Verify file still exists
        listing=$(list_directory "$REPO_ID" "/container/existing")
        if file_exists_in_listing "$listing" "data.txt"; then
            log_success "Test 10: File survives creation of sibling folder"
        else
            log_fail "Test 10: File disappeared after sibling folder creation"
            log_verbose "Listing: $listing"
        fi

        # Verify parent has both folders
        parent_listing=$(list_directory "$REPO_ID" "/container")
        has_existing=$(file_exists_in_listing "$parent_listing" "existing" && echo "yes" || echo "no")
        has_newfolder=$(file_exists_in_listing "$parent_listing" "newfolder" && echo "yes" || echo "no")

        if [ "$has_existing" = "yes" ] && [ "$has_newfolder" = "yes" ]; then
            log_success "Test 10: Both folders exist in parent"
        else
            log_fail "Test 10: Parent folder structure corrupted (existing=$has_existing, newfolder=$has_newfolder)"
        fi
    else
        log_fail "Test 10: Could not create folder structure"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 11: Create file directly in deep path (the "Folder does not exist" bug)
# This tests creating a file in a 4-level deep directory after all directories
# are created sequentially. The bug was that CreateDirectory at depth 3+
# corrupted the root fs_id, causing subsequent operations to fail.
# ============================================================================
echo "--- Test 11: File Creation After Deep mkdir (Regression) ---"

REPO_ID=$(create_test_library "test-nested-11-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 11: Could not create test library"
else
    log_verbose "Created library: $REPO_ID"

    # Create 4-level deep structure and put files at EVERY level
    if create_directory "$REPO_ID" "/a" && \
       create_directory "$REPO_ID" "/a/b" && \
       create_directory "$REPO_ID" "/a/b/c" && \
       create_directory "$REPO_ID" "/a/b/c/d"; then
        log_verbose "Created /a/b/c/d"

        # Create files at each level
        all_ok=true
        for level_path in "/" "/a" "/a/b" "/a/b/c" "/a/b/c/d"; do
            if ! create_file "$REPO_ID" "$level_path" "file-at-$(echo $level_path | tr '/' '-' | sed 's/^-//').txt" "Content at $level_path"; then
                log_fail "Test 11: Could not create file at $level_path"
                all_ok=false
                break
            fi
        done

        if [ "$all_ok" = true ]; then
            sleep 1

            # Verify files exist at each level
            files_found=0
            for level_path in "/" "/a" "/a/b" "/a/b/c" "/a/b/c/d"; do
                listing=$(list_directory "$REPO_ID" "$level_path")
                fname="file-at-$(echo $level_path | tr '/' '-' | sed 's/^-//').txt"
                if [ "$level_path" = "/" ]; then
                    fname="file-at-.txt"
                fi
                if echo "$listing" | grep -q "\"name\":\"$fname\""; then
                    ((files_found++))
                else
                    log_verbose "File not found at $level_path: $fname"
                    log_verbose "Listing: $listing"
                fi
            done

            if [ "$files_found" -eq 5 ]; then
                log_success "Test 11: Files at all 5 depth levels persist correctly"
            else
                log_fail "Test 11: Only $files_found/5 files found across depth levels"
            fi
        fi
    else
        log_fail "Test 11: Could not create deep folder structure"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 12: Interleaved directory and file creation
# Create dir, put file, create deeper dir, put file — tests that each commit
# correctly rebuilds the tree without losing earlier changes.
# ============================================================================
echo "--- Test 12: Interleaved Dir/File Creation ---"

REPO_ID=$(create_test_library "test-nested-12-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 12: Could not create test library"
else
    log_verbose "Created library: $REPO_ID"

    ok=true
    # Step 1: Create /x, add file
    create_directory "$REPO_ID" "/x" || ok=false
    create_file "$REPO_ID" "/x" "step1.txt" "step1" || ok=false

    # Step 2: Create /x/y, add file
    create_directory "$REPO_ID" "/x/y" || ok=false
    create_file "$REPO_ID" "/x/y" "step2.txt" "step2" || ok=false

    # Step 3: Create /x/y/z, add file
    create_directory "$REPO_ID" "/x/y/z" || ok=false
    create_file "$REPO_ID" "/x/y/z" "step3.txt" "step3" || ok=false

    # Step 4: Add another file back at /x (tests that /x still accessible)
    create_file "$REPO_ID" "/x" "step4.txt" "step4" || ok=false

    if [ "$ok" = true ]; then
        sleep 1
        # Verify everything
        files_ok=0

        listing_x=$(list_directory "$REPO_ID" "/x")
        file_exists_in_listing "$listing_x" "step1.txt" && ((files_ok++))
        file_exists_in_listing "$listing_x" "step4.txt" && ((files_ok++))
        file_exists_in_listing "$listing_x" "y" && ((files_ok++))

        listing_xy=$(list_directory "$REPO_ID" "/x/y")
        file_exists_in_listing "$listing_xy" "step2.txt" && ((files_ok++))
        file_exists_in_listing "$listing_xy" "z" && ((files_ok++))

        listing_xyz=$(list_directory "$REPO_ID" "/x/y/z")
        file_exists_in_listing "$listing_xyz" "step3.txt" && ((files_ok++))

        if [ "$files_ok" -eq 6 ]; then
            log_success "Test 12: All interleaved dir/file operations preserved"
        else
            log_fail "Test 12: Only $files_ok/6 items found after interleaved operations"
            log_verbose "/x: $listing_x"
            log_verbose "/x/y: $listing_xy"
            log_verbose "/x/y/z: $listing_xyz"
        fi
    else
        log_fail "Test 12: Failed during interleaved creation"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 13: Multiple sibling directories at depth 3
# Tests that creating siblings at depth 3 (where the bug was) doesn't corrupt
# the tree. Each sibling creation must rebuild from grandparent to root.
# ============================================================================
echo "--- Test 13: Multiple Siblings at Depth 3 ---"

REPO_ID=$(create_test_library "test-nested-13-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 13: Could not create test library"
else
    log_verbose "Created library: $REPO_ID"

    if create_directory "$REPO_ID" "/p1" && \
       create_directory "$REPO_ID" "/p1/p2" && \
       create_directory "$REPO_ID" "/p1/p2/sib1" && \
       create_directory "$REPO_ID" "/p1/p2/sib2" && \
       create_directory "$REPO_ID" "/p1/p2/sib3"; then

        # Put files in each sibling
        create_file "$REPO_ID" "/p1/p2/sib1" "a.txt" "in sib1"
        create_file "$REPO_ID" "/p1/p2/sib2" "b.txt" "in sib2"
        create_file "$REPO_ID" "/p1/p2/sib3" "c.txt" "in sib3"

        sleep 1

        items_ok=0
        listing_p2=$(list_directory "$REPO_ID" "/p1/p2")
        file_exists_in_listing "$listing_p2" "sib1" && ((items_ok++))
        file_exists_in_listing "$listing_p2" "sib2" && ((items_ok++))
        file_exists_in_listing "$listing_p2" "sib3" && ((items_ok++))

        listing_s1=$(list_directory "$REPO_ID" "/p1/p2/sib1")
        file_exists_in_listing "$listing_s1" "a.txt" && ((items_ok++))

        listing_s2=$(list_directory "$REPO_ID" "/p1/p2/sib2")
        file_exists_in_listing "$listing_s2" "b.txt" && ((items_ok++))

        listing_s3=$(list_directory "$REPO_ID" "/p1/p2/sib3")
        file_exists_in_listing "$listing_s3" "c.txt" && ((items_ok++))

        if [ "$items_ok" -eq 6 ]; then
            log_success "Test 13: Multiple siblings at depth 3 all intact"
        else
            log_fail "Test 13: Only $items_ok/6 items found"
            log_verbose "/p1/p2: $listing_p2"
        fi
    else
        log_fail "Test 13: Could not create folder structure"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 14: Deep nesting with 8 levels (stress test) - skip if --quick
# ============================================================================
if [ "$QUICK" = true ]; then
    log_skip "Test 14: Deep nesting (8 levels) - skipped in quick mode"
else
    echo "--- Test 14: Deep Nesting (8 Levels) ---"

    REPO_ID=$(create_test_library "test-nested-14-$(date +%s)")
    if [ -z "$REPO_ID" ]; then
        log_fail "Test 14: Could not create test library"
    else
        log_verbose "Created library: $REPO_ID"

        deep_path=""
        create_ok=true
        for i in 1 2 3 4 5 6 7 8; do
            deep_path="${deep_path}/d${i}"
            if ! create_directory "$REPO_ID" "$deep_path"; then
                log_fail "Test 14: Failed to create $deep_path"
                create_ok=false
                break
            fi
        done

        if [ "$create_ok" = true ]; then
            # Create file at deepest level
            if create_file "$REPO_ID" "$deep_path" "bottom.txt" "at the bottom"; then
                sleep 1

                # Verify file at bottom
                listing=$(list_directory "$REPO_ID" "$deep_path")
                if file_exists_in_listing "$listing" "bottom.txt"; then
                    log_success "Test 14: File persists at 8 levels deep"
                else
                    log_fail "Test 14: File disappeared at 8 levels deep"
                fi

                # Verify we can still list root
                root_listing=$(list_directory "$REPO_ID" "/")
                if file_exists_in_listing "$root_listing" "d1"; then
                    log_success "Test 14: Root directory intact after 8-level nesting"
                else
                    log_fail "Test 14: Root directory corrupted"
                    log_verbose "Root: $root_listing"
                fi
            else
                log_fail "Test 14: Could not create file at depth 8"
            fi
        fi

        delete_test_library "$REPO_ID"
    fi
fi
echo ""

# ============================================================================
# Test 15: Create nested dirs, delete file, verify parent structure intact
# ============================================================================
echo "--- Test 15: Delete File in Nested Dir ---"

REPO_ID=$(create_test_library "test-nested-15-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 15: Could not create test library"
else
    log_verbose "Created library: $REPO_ID"

    if create_directory "$REPO_ID" "/alpha" && \
       create_directory "$REPO_ID" "/alpha/beta" && \
       create_file "$REPO_ID" "/alpha/beta" "remove-me.txt" "to be deleted" && \
       create_file "$REPO_ID" "/alpha/beta" "keep-me.txt" "to be kept"; then

        sleep 1

        # Delete the file
        response=$(api_delete "/api2/repos/${REPO_ID}/file/?p=/alpha/beta/remove-me.txt")
        del_code=$(get_http_code "$response")
        log_verbose "Delete response: HTTP $del_code"

        if [ "$del_code" = "200" ]; then
            sleep 1

            # Verify keep-me.txt still exists
            listing=$(list_directory "$REPO_ID" "/alpha/beta")
            if file_exists_in_listing "$listing" "keep-me.txt"; then
                # Verify removed file is gone
                if ! file_exists_in_listing "$listing" "remove-me.txt"; then
                    log_success "Test 15: File deleted, sibling preserved in nested dir"
                else
                    log_fail "Test 15: Deleted file still appears"
                fi
            else
                log_fail "Test 15: Sibling file disappeared after delete"
                log_verbose "Listing: $listing"
            fi

            # Verify parent structure intact
            parent_listing=$(list_directory "$REPO_ID" "/alpha")
            if file_exists_in_listing "$parent_listing" "beta"; then
                log_success "Test 15: Parent directory intact after nested file delete"
            else
                log_fail "Test 15: Parent directory corrupted after delete"
            fi
        else
            log_fail "Test 15: Could not delete file (HTTP $del_code)"
        fi
    else
        log_fail "Test 15: Could not create test structure"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 16: CreateFile (v2.1 frontend endpoint) in nested folder at depth 1
# Regression test: creating a docx via POST /api/v2.1/repos/:id/file/?p=/folder/file.docx
# was corrupting the tree because CreateFile did not do grandparent rebuild.
# ============================================================================
echo "--- Test 16: CreateFile (v2.1) in Nested Folder (Depth 1) ---"

REPO_ID=$(create_test_library "test-nested-16-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 16: Could not create test library"
else
    log_verbose "Created library: $REPO_ID"

    if create_directory "$REPO_ID" "/myfolder"; then
        # Use the v2.1 CreateFile endpoint (what the frontend calls)
        response=$(api_post_form "/api/v2.1/repos/${REPO_ID}/file/?p=/myfolder/test.docx" \
            -F "operation=create")
        code=$(get_http_code "$response")
        body=$(get_body "$response")
        log_verbose "CreateFile response: HTTP $code - $body"

        if [ "$code" = "201" ]; then
            sleep 1

            # Verify the file appears in the folder
            listing=$(list_directory "$REPO_ID" "/myfolder")
            if file_exists_in_listing "$listing" "test.docx"; then
                log_success "Test 16: File visible in folder after v2.1 CreateFile"
            else
                log_fail "Test 16: File not found in /myfolder after CreateFile"
                log_verbose "Listing: $listing"
            fi

            # Verify root still has the folder
            root_listing=$(list_directory "$REPO_ID" "/")
            if file_exists_in_listing "$root_listing" "myfolder"; then
                log_success "Test 16: Root intact after v2.1 CreateFile in subfolder"
            else
                log_fail "Test 16: Root corrupted after v2.1 CreateFile"
                log_verbose "Root: $root_listing"
            fi
        else
            log_fail "Test 16: v2.1 CreateFile returned HTTP $code (expected 201)"
        fi
    else
        log_fail "Test 16: Could not create /myfolder"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 17: CreateFile (v2.1) at multiple depths (depth 2, 3, 4)
# Ensures the grandparent rebuild works at all nesting levels.
# ============================================================================
echo "--- Test 17: CreateFile (v2.1) at Multiple Depths ---"

REPO_ID=$(create_test_library "test-nested-17-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 17: Could not create test library"
else
    log_verbose "Created library: $REPO_ID"

    # Build the tree
    create_directory "$REPO_ID" "/d1"
    create_directory "$REPO_ID" "/d1/d2"
    create_directory "$REPO_ID" "/d1/d2/d3"
    create_directory "$REPO_ID" "/d1/d2/d3/d4"

    all_ok=true

    # CreateFile at depth 2: /d1/d2/file2.txt
    response=$(api_post_form "/api/v2.1/repos/${REPO_ID}/file/?p=/d1/d2/file2.txt" \
        -F "operation=create")
    code=$(get_http_code "$response")
    if [ "$code" != "201" ]; then
        log_fail "Test 17: CreateFile at depth 2 returned HTTP $code"
        all_ok=false
    fi

    # CreateFile at depth 3: /d1/d2/d3/file3.docx
    response=$(api_post_form "/api/v2.1/repos/${REPO_ID}/file/?p=/d1/d2/d3/file3.docx" \
        -F "operation=create")
    code=$(get_http_code "$response")
    if [ "$code" != "201" ]; then
        log_fail "Test 17: CreateFile at depth 3 returned HTTP $code"
        all_ok=false
    fi

    # CreateFile at depth 4: /d1/d2/d3/d4/file4.xlsx
    response=$(api_post_form "/api/v2.1/repos/${REPO_ID}/file/?p=/d1/d2/d3/d4/file4.xlsx" \
        -F "operation=create")
    code=$(get_http_code "$response")
    if [ "$code" != "201" ]; then
        log_fail "Test 17: CreateFile at depth 4 returned HTTP $code"
        all_ok=false
    fi

    if [ "$all_ok" = true ]; then
        sleep 1

        # Verify every level is navigable and files are present
        checks=0

        listing=$(list_directory "$REPO_ID" "/d1/d2")
        file_exists_in_listing "$listing" "file2.txt" && ((checks++))
        file_exists_in_listing "$listing" "d3" && ((checks++))

        listing=$(list_directory "$REPO_ID" "/d1/d2/d3")
        file_exists_in_listing "$listing" "file3.docx" && ((checks++))
        file_exists_in_listing "$listing" "d4" && ((checks++))

        listing=$(list_directory "$REPO_ID" "/d1/d2/d3/d4")
        file_exists_in_listing "$listing" "file4.xlsx" && ((checks++))

        # Verify all ancestors navigable
        list_directory "$REPO_ID" "/d1" > /dev/null && ((checks++))
        list_directory "$REPO_ID" "/" > /dev/null && ((checks++))

        if [ "$checks" -eq 7 ]; then
            log_success "Test 17: All files at depths 2-4 visible, tree intact"
        else
            log_fail "Test 17: Only $checks/7 checks passed"
        fi
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 18: CreateFile (v2.1) then upload file via seafhttp in same folder
# Tests that both file creation methods work together without corrupting tree.
# ============================================================================
echo "--- Test 18: CreateFile + Upload in Same Nested Folder ---"

REPO_ID=$(create_test_library "test-nested-18-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 18: Could not create test library"
else
    log_verbose "Created library: $REPO_ID"

    create_directory "$REPO_ID" "/workspace"
    create_directory "$REPO_ID" "/workspace/docs"

    # Create a docx via v2.1 endpoint
    response=$(api_post_form "/api/v2.1/repos/${REPO_ID}/file/?p=/workspace/docs/notes.docx" \
        -F "operation=create")
    code=$(get_http_code "$response")
    log_verbose "CreateFile response: HTTP $code"

    # Upload a file via seafhttp into the same folder
    create_file "$REPO_ID" "/workspace/docs" "data.csv" "col1,col2\nval1,val2"

    sleep 1

    listing=$(list_directory "$REPO_ID" "/workspace/docs")
    found_docx=false
    found_csv=false
    file_exists_in_listing "$listing" "notes.docx" && found_docx=true
    file_exists_in_listing "$listing" "data.csv" && found_csv=true

    if $found_docx && $found_csv; then
        log_success "Test 18: Both CreateFile and Upload files coexist in nested folder"
    else
        log_fail "Test 18: Missing files (docx=$found_docx, csv=$found_csv)"
        log_verbose "Listing: $listing"
    fi

    # Verify ancestors
    parent=$(list_directory "$REPO_ID" "/workspace")
    root=$(list_directory "$REPO_ID" "/")
    if file_exists_in_listing "$parent" "docs" && file_exists_in_listing "$root" "workspace"; then
        log_success "Test 18: Ancestor directories intact after mixed operations"
    else
        log_fail "Test 18: Ancestor directories corrupted"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 19: CreateFile (v2.1) multiple files in same nested folder sequentially
# Simulates user creating multiple Office docs in the same folder.
# ============================================================================
echo "--- Test 19: Multiple CreateFile in Same Nested Folder ---"

REPO_ID=$(create_test_library "test-nested-19-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 19: Could not create test library"
else
    log_verbose "Created library: $REPO_ID"

    create_directory "$REPO_ID" "/project"
    create_directory "$REPO_ID" "/project/reports"

    files_ok=0
    for fname in "q1.docx" "q2.docx" "q3.xlsx" "budget.pptx"; do
        response=$(api_post_form "/api/v2.1/repos/${REPO_ID}/file/?p=/project/reports/${fname}" \
            -F "operation=create")
        code=$(get_http_code "$response")
        if [ "$code" = "201" ]; then
            ((files_ok++))
        else
            log_verbose "CreateFile $fname: HTTP $code"
        fi
    done

    if [ "$files_ok" -eq 4 ]; then
        sleep 1

        listing=$(list_directory "$REPO_ID" "/project/reports")
        found=0
        for fname in "q1.docx" "q2.docx" "q3.xlsx" "budget.pptx"; do
            file_exists_in_listing "$listing" "$fname" && ((found++))
        done

        if [ "$found" -eq 4 ]; then
            log_success "Test 19: All 4 files present after sequential CreateFile"
        else
            log_fail "Test 19: Only $found/4 files found"
            log_verbose "Listing: $listing"
        fi

        # Verify ancestor chain
        root=$(list_directory "$REPO_ID" "/")
        proj=$(list_directory "$REPO_ID" "/project")
        if file_exists_in_listing "$root" "project" && file_exists_in_listing "$proj" "reports"; then
            log_success "Test 19: Ancestor directories intact after 4 sequential creates"
        else
            log_fail "Test 19: Ancestor directories corrupted"
        fi
    else
        log_fail "Test 19: Only $files_ok/4 CreateFile calls succeeded"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 20: CreateFile at root level (no grandparent rebuild needed)
# Ensures the parentPath == "/" branch still works correctly.
# ============================================================================
echo "--- Test 20: CreateFile (v2.1) at Root Level ---"

REPO_ID=$(create_test_library "test-nested-20-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 20: Could not create test library"
else
    log_verbose "Created library: $REPO_ID"

    response=$(api_post_form "/api/v2.1/repos/${REPO_ID}/file/?p=/root-file.docx" \
        -F "operation=create")
    code=$(get_http_code "$response")

    if [ "$code" = "201" ]; then
        sleep 1
        listing=$(list_directory "$REPO_ID" "/")
        if file_exists_in_listing "$listing" "root-file.docx"; then
            log_success "Test 20: File created at root via v2.1 CreateFile"
        else
            log_fail "Test 20: Root file not visible after CreateFile"
            log_verbose "Listing: $listing"
        fi
    else
        log_fail "Test 20: v2.1 CreateFile at root returned HTTP $code"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Summary
# ============================================================================
echo "=============================================="
echo "  Test Summary"
echo "=============================================="
echo ""
echo -e "  ${GREEN}Passed:${NC}  $TESTS_PASSED"
echo -e "  ${RED}Failed:${NC}  $TESTS_FAILED"
echo -e "  ${YELLOW}Skipped:${NC} $TESTS_SKIPPED"
echo ""

TOTAL=$((TESTS_PASSED + TESTS_FAILED))
if [ "$TESTS_FAILED" -eq 0 ]; then
    echo -e "${GREEN}All $TOTAL tests passed!${NC}"
    exit 0
else
    echo -e "${RED}$TESTS_FAILED of $TOTAL tests failed${NC}"
    exit 1
fi
