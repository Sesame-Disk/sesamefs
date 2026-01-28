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
SESAMEFS_URL="${SESAMEFS_URL:-http://localhost:8080}"
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
# ============================================================================
echo "--- Test 5: Files with Spaces in Path ---"

REPO_ID=$(create_test_library "test-nested-5-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 5: Could not create test library"
else
    log_verbose "Created library: $REPO_ID"

    # URL encode spaces
    if create_directory "$REPO_ID" "/my%20folder" && \
       create_directory "$REPO_ID" "/my%20folder/sub%20folder"; then
        log_verbose "Created /my folder/sub folder"

        if create_file "$REPO_ID" "/my%20folder/sub%20folder" "my file.txt" "Content with spaces"; then
            sleep 1
            listing=$(list_directory "$REPO_ID" "/my%20folder/sub%20folder")
            if file_exists_in_listing "$listing" "my file.txt"; then
                log_success "Test 5: File with spaces in path persists"
            else
                log_fail "Test 5: File with spaces in path disappeared"
                log_verbose "Listing: $listing"
            fi
        else
            log_fail "Test 5: Could not create file with spaces"
        fi
    else
        log_fail "Test 5: Could not create folders with spaces"
    fi

    delete_test_library "$REPO_ID"
fi
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
