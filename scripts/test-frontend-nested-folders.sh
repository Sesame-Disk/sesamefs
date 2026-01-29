#!/bin/bash
#
# Frontend-focused integration tests for nested folder operations.
#
# Tests the v2.1 API endpoints that the web frontend uses for directory
# browsing, file upload, and folder creation. This validates that nested
# folder operations work correctly from the frontend's perspective.
#
# The frontend uses:
#   - GET  /api/v2.1/repos/:id/dir/?p=/path     (ListDirectoryV21 - directory browsing)
#   - POST /api2/repos/:id/dir/                  (CreateDirectory via seafile-js)
#   - GET  /api2/repos/:id/upload-link/           (Get upload URL)
#   - POST <upload-url>                           (Upload file)
#   - GET  /api/v2.1/repos/:id/file/?p=/path     (File detail)
#   - DELETE /api2/repos/:id/file/?p=/path        (Delete file)
#   - DELETE /api2/repos/:id/dir/?p=/path         (Delete directory)
#   - POST /api/v2.1/repos/sync-batch-move-item/       (Batch move)
#   - POST /api/v2.1/repos/sync-batch-copy-item/       (Batch copy)
#
# Usage:
#   ./scripts/test-frontend-nested-folders.sh           # Run all tests
#   ./scripts/test-frontend-nested-folders.sh --verbose  # Show detailed output
#

set +e

# Configuration
SESAMEFS_URL="${SESAMEFS_URL:-http://localhost:8080}"
DEV_TOKEN="${DEV_TOKEN:-dev-token-admin}"
VERBOSE=false

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

TESTS_PASSED=0
TESTS_FAILED=0

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --verbose|-v) VERBOSE=true; shift ;;
        --help|-h)
            echo "Usage: $0 [--verbose]"
            exit 0
            ;;
        *) echo "Unknown option: $1"; exit 1 ;;
    esac
done

log_verbose() {
    if [ "$VERBOSE" = true ]; then
        echo -e "${BLUE}[DEBUG]${NC} $1"
    fi
}
log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[PASS]${NC} $1"; ((TESTS_PASSED++)); }
log_fail() { echo -e "${RED}[FAIL]${NC} $1"; ((TESTS_FAILED++)); }

# API helpers - use v2.1 endpoints (what the frontend uses)
v21_get() {
    local endpoint="$1"
    timeout 30 curl -s -w "\n%{http_code}" "${SESAMEFS_URL}${endpoint}" \
        -H "Authorization: Token ${DEV_TOKEN}" \
        -H "Accept: application/json" 2>/dev/null
}

api2_post_form() {
    local endpoint="$1"
    shift
    timeout 30 curl -s -w "\n%{http_code}" -X POST "${SESAMEFS_URL}${endpoint}" \
        -H "Authorization: Token ${DEV_TOKEN}" \
        "$@" 2>/dev/null
}

api2_get() {
    local endpoint="$1"
    timeout 30 curl -s -w "\n%{http_code}" "${SESAMEFS_URL}${endpoint}" \
        -H "Authorization: Token ${DEV_TOKEN}" \
        -H "Accept: application/json" 2>/dev/null
}

api2_delete() {
    local endpoint="$1"
    timeout 10 curl -s -w "\n%{http_code}" -X DELETE "${SESAMEFS_URL}${endpoint}" \
        -H "Authorization: Token ${DEV_TOKEN}" 2>/dev/null
}

api2_post_json() {
    local endpoint="$1"
    local data="$2"
    timeout 30 curl -s -w "\n%{http_code}" -X POST "${SESAMEFS_URL}${endpoint}" \
        -H "Authorization: Token ${DEV_TOKEN}" \
        -H "Content-Type: application/json" \
        -H "Accept: application/json" \
        -d "$data" 2>/dev/null
}

get_http_code() { echo "$1" | tail -n1; }
get_body() { echo "$1" | sed '$d'; }

create_test_library() {
    local name="$1"
    local response
    response=$(api2_post_json "/api2/repos/" "{\"name\": \"$name\"}")
    local code=$(get_http_code "$response")
    local body=$(get_body "$response")
    if [ "$code" = "200" ] || [ "$code" = "201" ]; then
        echo "$body" | grep -o '"repo_id":"[^"]*"' | cut -d'"' -f4
    else
        echo ""
    fi
}

delete_test_library() {
    local repo_id="$1"
    timeout 10 curl -s -X DELETE "${SESAMEFS_URL}/api2/repos/${repo_id}/" \
        -H "Authorization: Token ${DEV_TOKEN}" > /dev/null 2>&1 || true
}

create_directory() {
    local repo_id="$1"
    local dir_path="$2"
    local response
    response=$(api2_post_form "/api2/repos/${repo_id}/dir/" \
        -F "p=${dir_path}" \
        -F "operation=mkdir")
    local code=$(get_http_code "$response")
    log_verbose "Create dir $dir_path: HTTP $code"
    [ "$code" = "200" ] || [ "$code" = "201" ]
}

upload_file() {
    local repo_id="$1"
    local dir_path="$2"
    local filename="$3"
    local content="$4"
    local tmpfile=$(mktemp)
    echo -n "$content" > "$tmpfile"

    local response
    response=$(api2_get "/api2/repos/${repo_id}/upload-link/?p=${dir_path}")
    local code=$(get_http_code "$response")
    local body=$(get_body "$response")
    if [ "$code" != "200" ]; then
        rm -f "$tmpfile"
        return 1
    fi

    local upload_url=$(echo "$body" | tr -d '"')
    response=$(curl -s -w "\n%{http_code}" -X POST "$upload_url" \
        -H "Authorization: Token ${DEV_TOKEN}" \
        -F "file=@${tmpfile};filename=${filename}" \
        -F "parent_dir=${dir_path}" \
        -F "relative_path=")
    code=$(get_http_code "$response")
    rm -f "$tmpfile"
    [ "$code" = "200" ] || [ "$code" = "201" ]
}

# v2.1 directory listing (what the frontend calls)
v21_list_directory() {
    local repo_id="$1"
    local dir_path="$2"
    local response
    response=$(v21_get "/api/v2.1/repos/${repo_id}/dir/?p=${dir_path}")
    local code=$(get_http_code "$response")
    local body=$(get_body "$response")
    if [ "$code" = "200" ]; then
        echo "$body"
    else
        log_verbose "v2.1 list $dir_path: HTTP $code - $body"
        echo ""
    fi
}

echo "==================================================="
echo "  Frontend API - Nested Folder Integration Tests"
echo "==================================================="
echo ""
echo "Target: ${SESAMEFS_URL}"
echo "Tests v2.1 API endpoints used by the web frontend"
echo ""

# Check API availability
response=$(curl -s -o /dev/null -w "%{http_code}" "${SESAMEFS_URL}/ping")
if [ "$response" != "200" ]; then
    echo -e "${RED}ERROR: API not available at ${SESAMEFS_URL}${NC}"
    exit 1
fi

# ============================================================================
# Test 1: v2.1 dir listing returns correct structure
# ============================================================================
echo "--- Test 1: v2.1 Directory Listing Response Format ---"

REPO_ID=$(create_test_library "fe-test-1-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 1: Could not create library"
else
    create_directory "$REPO_ID" "/docs"
    upload_file "$REPO_ID" "/" "readme.txt" "Hello"
    sleep 1

    body=$(v21_list_directory "$REPO_ID" "/")
    log_verbose "v2.1 response: $body"

    # Frontend expects: { "user_perm": "...", "dir_id": "...", "dirent_list": [...] }
    if echo "$body" | grep -q '"user_perm"'; then
        log_success "Test 1: Response has user_perm field"
    else
        log_fail "Test 1: Missing user_perm field"
    fi

    if echo "$body" | grep -q '"dir_id"'; then
        log_success "Test 1: Response has dir_id field"
    else
        log_fail "Test 1: Missing dir_id field"
    fi

    if echo "$body" | grep -q '"dirent_list"'; then
        log_success "Test 1: Response has dirent_list field"
    else
        log_fail "Test 1: Missing dirent_list field"
    fi

    # Check dirent fields
    if echo "$body" | grep -q '"type":"dir"'; then
        log_success "Test 1: Directory dirent has type=dir"
    else
        log_fail "Test 1: Missing directory dirent or wrong type"
    fi

    if echo "$body" | grep -q '"type":"file"'; then
        log_success "Test 1: File dirent has type=file"
    else
        log_fail "Test 1: Missing file dirent or wrong type"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 2: v2.1 nested directory browsing (3 levels deep)
# The frontend navigates by calling ListDirectoryV21 at each level.
# ============================================================================
echo "--- Test 2: v2.1 Nested Directory Browsing (3 Levels) ---"

REPO_ID=$(create_test_library "fe-test-2-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 2: Could not create library"
else
    # Simulate user creating nested folders via UI
    create_directory "$REPO_ID" "/projects"
    create_directory "$REPO_ID" "/projects/frontend"
    create_directory "$REPO_ID" "/projects/frontend/src"
    upload_file "$REPO_ID" "/projects/frontend/src" "app.js" "const App = () => {};"
    upload_file "$REPO_ID" "/projects/frontend" "package.json" "{}"
    upload_file "$REPO_ID" "/projects" "README.md" "# Projects"
    sleep 1

    # Frontend navigation: user clicks root → projects → frontend → src
    # Each click triggers v2.1 ListDirectoryV21
    ok=true

    # Level 0: root
    body=$(v21_list_directory "$REPO_ID" "/")
    if echo "$body" | grep -q '"name":"projects"'; then
        log_verbose "Root shows 'projects' folder"
    else
        log_fail "Test 2: Root listing missing 'projects'"
        ok=false
    fi

    # Level 1: /projects
    body=$(v21_list_directory "$REPO_ID" "/projects")
    if echo "$body" | grep -q '"name":"frontend"' && echo "$body" | grep -q '"name":"README.md"'; then
        log_verbose "/projects shows 'frontend' dir and README.md"
    else
        log_fail "Test 2: /projects listing incomplete"
        log_verbose "Body: $body"
        ok=false
    fi

    # Level 2: /projects/frontend
    body=$(v21_list_directory "$REPO_ID" "/projects/frontend")
    if echo "$body" | grep -q '"name":"src"' && echo "$body" | grep -q '"name":"package.json"'; then
        log_verbose "/projects/frontend shows 'src' dir and package.json"
    else
        log_fail "Test 2: /projects/frontend listing incomplete"
        log_verbose "Body: $body"
        ok=false
    fi

    # Level 3: /projects/frontend/src
    body=$(v21_list_directory "$REPO_ID" "/projects/frontend/src")
    if echo "$body" | grep -q '"name":"app.js"'; then
        log_verbose "/projects/frontend/src shows app.js"
    else
        log_fail "Test 2: /projects/frontend/src listing missing app.js"
        log_verbose "Body: $body"
        ok=false
    fi

    if [ "$ok" = true ]; then
        log_success "Test 2: v2.1 nested browsing works at all 4 levels"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 3: Deep nesting via v2.1 (5 levels - the regression scenario)
# ============================================================================
echo "--- Test 3: v2.1 Deep Nesting (5 Levels) ---"

REPO_ID=$(create_test_library "fe-test-3-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 3: Could not create library"
else
    create_directory "$REPO_ID" "/d1"
    create_directory "$REPO_ID" "/d1/d2"
    create_directory "$REPO_ID" "/d1/d2/d3"
    create_directory "$REPO_ID" "/d1/d2/d3/d4"
    create_directory "$REPO_ID" "/d1/d2/d3/d4/d5"
    upload_file "$REPO_ID" "/d1/d2/d3/d4/d5" "deep.txt" "deep content"
    sleep 1

    # Navigate all the way down using v2.1 (simulating frontend clicks)
    all_ok=true
    for check_path in "/" "/d1" "/d1/d2" "/d1/d2/d3" "/d1/d2/d3/d4" "/d1/d2/d3/d4/d5"; do
        body=$(v21_list_directory "$REPO_ID" "$check_path")
        if [ -z "$body" ] || echo "$body" | grep -q '"error"'; then
            log_fail "Test 3: v2.1 listing failed at $check_path"
            log_verbose "Body: $body"
            all_ok=false
            break
        fi
        log_verbose "v2.1 listing OK at $check_path"
    done

    if [ "$all_ok" = true ]; then
        # Verify file at deepest level
        body=$(v21_list_directory "$REPO_ID" "/d1/d2/d3/d4/d5")
        if echo "$body" | grep -q '"name":"deep.txt"'; then
            log_success "Test 3: v2.1 browsing works at 5 levels deep"
        else
            log_fail "Test 3: File missing at 5 levels deep"
        fi
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 4: Create folder, upload file, navigate back up (frontend workflow)
# User creates nested structure, then navigates back to parent folders.
# Tests that parent dir_id references remain valid after child operations.
# ============================================================================
echo "--- Test 4: Create-Upload-Navigate-Back Workflow ---"

REPO_ID=$(create_test_library "fe-test-4-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 4: Could not create library"
else
    # Step 1: User creates folder via UI
    create_directory "$REPO_ID" "/photos"
    create_directory "$REPO_ID" "/photos/2024"
    create_directory "$REPO_ID" "/photos/2024/vacation"

    # Step 2: User uploads files into the deep folder
    upload_file "$REPO_ID" "/photos/2024/vacation" "beach.jpg" "fake-jpeg-data-1"
    upload_file "$REPO_ID" "/photos/2024/vacation" "sunset.jpg" "fake-jpeg-data-2"
    sleep 1

    # Step 3: User navigates back up (clicking breadcrumbs in frontend)
    ok=true

    # Check deepest level
    body=$(v21_list_directory "$REPO_ID" "/photos/2024/vacation")
    if ! echo "$body" | grep -q '"name":"beach.jpg"' || ! echo "$body" | grep -q '"name":"sunset.jpg"'; then
        log_fail "Test 4: Files missing in /photos/2024/vacation"
        ok=false
    fi

    # Navigate up to /photos/2024
    body=$(v21_list_directory "$REPO_ID" "/photos/2024")
    if ! echo "$body" | grep -q '"name":"vacation"'; then
        log_fail "Test 4: 'vacation' folder missing in /photos/2024"
        ok=false
    fi

    # Navigate up to /photos
    body=$(v21_list_directory "$REPO_ID" "/photos")
    if ! echo "$body" | grep -q '"name":"2024"'; then
        log_fail "Test 4: '2024' folder missing in /photos"
        ok=false
    fi

    # Navigate up to root
    body=$(v21_list_directory "$REPO_ID" "/")
    if ! echo "$body" | grep -q '"name":"photos"'; then
        log_fail "Test 4: 'photos' folder missing in root"
        ok=false
    fi

    if [ "$ok" = true ]; then
        log_success "Test 4: Create-upload-navigate-back workflow works correctly"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 5: Concurrent folder operations (simulates rapid UI clicks)
# Create multiple sibling folders quickly, then verify all are visible.
# ============================================================================
echo "--- Test 5: Rapid Sibling Folder Creation ---"

REPO_ID=$(create_test_library "fe-test-5-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 5: Could not create library"
else
    create_directory "$REPO_ID" "/workspace"
    create_directory "$REPO_ID" "/workspace/project-alpha"
    create_directory "$REPO_ID" "/workspace/project-beta"
    create_directory "$REPO_ID" "/workspace/project-gamma"
    create_directory "$REPO_ID" "/workspace/project-delta"

    # Each project gets a file
    upload_file "$REPO_ID" "/workspace/project-alpha" "main.py" "print('alpha')"
    upload_file "$REPO_ID" "/workspace/project-beta" "main.py" "print('beta')"
    upload_file "$REPO_ID" "/workspace/project-gamma" "main.py" "print('gamma')"
    upload_file "$REPO_ID" "/workspace/project-delta" "main.py" "print('delta')"
    sleep 1

    # Frontend lists /workspace - should show all 4 projects
    body=$(v21_list_directory "$REPO_ID" "/workspace")
    items_found=0
    for proj in "project-alpha" "project-beta" "project-gamma" "project-delta"; do
        if echo "$body" | grep -q "\"name\":\"$proj\""; then
            ((items_found++))
        else
            log_verbose "Missing: $proj"
        fi
    done

    if [ "$items_found" -eq 4 ]; then
        log_success "Test 5: All 4 sibling projects visible in v2.1 listing"
    else
        log_fail "Test 5: Only $items_found/4 sibling projects visible"
        log_verbose "Body: $body"
    fi

    # Verify each project's file
    files_found=0
    for proj in "project-alpha" "project-beta" "project-gamma" "project-delta"; do
        body=$(v21_list_directory "$REPO_ID" "/workspace/$proj")
        if echo "$body" | grep -q '"name":"main.py"'; then
            ((files_found++))
        fi
    done

    if [ "$files_found" -eq 4 ]; then
        log_success "Test 5: All 4 project files accessible via v2.1"
    else
        log_fail "Test 5: Only $files_found/4 project files found"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 6: File operations in nested dirs (delete, then verify parent intact)
# ============================================================================
echo "--- Test 6: File Delete in Deeply Nested Dir via v2.1 ---"

REPO_ID=$(create_test_library "fe-test-6-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 6: Could not create library"
else
    create_directory "$REPO_ID" "/a"
    create_directory "$REPO_ID" "/a/b"
    create_directory "$REPO_ID" "/a/b/c"
    upload_file "$REPO_ID" "/a/b/c" "keep.txt" "keep"
    upload_file "$REPO_ID" "/a/b/c" "delete.txt" "delete"
    sleep 1

    # Delete file
    response=$(api2_delete "/api2/repos/${REPO_ID}/file/?p=/a/b/c/delete.txt")
    del_code=$(get_http_code "$response")

    if [ "$del_code" = "200" ]; then
        sleep 1

        # v2.1: verify via frontend's listing endpoint
        body=$(v21_list_directory "$REPO_ID" "/a/b/c")
        if echo "$body" | grep -q '"name":"keep.txt"' && ! echo "$body" | grep -q '"name":"delete.txt"'; then
            log_success "Test 6: File deleted, sibling preserved (v2.1 verified)"
        else
            log_fail "Test 6: Unexpected listing after delete"
            log_verbose "Body: $body"
        fi

        # Navigate up to verify parent structure via v2.1
        body=$(v21_list_directory "$REPO_ID" "/a/b")
        if echo "$body" | grep -q '"name":"c"'; then
            log_success "Test 6: Parent dir intact after nested delete (v2.1 verified)"
        else
            log_fail "Test 6: Parent dir corrupted after delete"
        fi

        body=$(v21_list_directory "$REPO_ID" "/a")
        if echo "$body" | grep -q '"name":"b"'; then
            log_success "Test 6: Grandparent dir intact (v2.1 verified)"
        else
            log_fail "Test 6: Grandparent dir corrupted"
        fi
    else
        log_fail "Test 6: Delete failed (HTTP $del_code)"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 7: Batch move into nested directory (frontend Move dialog)
# ============================================================================
echo "--- Test 7: Batch Move Into Nested Dir ---"

REPO_ID=$(create_test_library "fe-test-7-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 7: Could not create library"
else
    # Create structure: /source/file.txt and /dest/nested/
    create_directory "$REPO_ID" "/source"
    upload_file "$REPO_ID" "/source" "moveme.txt" "move this content"
    create_directory "$REPO_ID" "/dest"
    create_directory "$REPO_ID" "/dest/nested"
    sleep 1

    # Batch move (v2.1 endpoint used by frontend)
    response=$(api2_post_json "/api/v2.1/repos/sync-batch-move-item/" \
        "{\"src_repo_id\":\"$REPO_ID\",\"src_parent_dir\":\"/source\",\"src_dirents\":[\"moveme.txt\"],\"dst_repo_id\":\"$REPO_ID\",\"dst_parent_dir\":\"/dest/nested\"}")
    move_code=$(get_http_code "$response")
    log_verbose "Batch move: HTTP $move_code"

    if [ "$move_code" = "200" ]; then
        sleep 1

        # Verify file in destination via v2.1
        body=$(v21_list_directory "$REPO_ID" "/dest/nested")
        if echo "$body" | grep -q '"name":"moveme.txt"'; then
            log_success "Test 7: File moved into nested dir (v2.1 verified)"
        else
            log_fail "Test 7: Moved file not found in destination"
            log_verbose "Body: $body"
        fi

        # Verify file removed from source
        body=$(v21_list_directory "$REPO_ID" "/source")
        if ! echo "$body" | grep -q '"name":"moveme.txt"'; then
            log_success "Test 7: File removed from source after move"
        else
            log_fail "Test 7: File still in source after move"
        fi
    else
        log_fail "Test 7: Batch move failed (HTTP $move_code)"
        log_verbose "Response: $(get_body "$response")"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 8: Batch copy into nested directory (frontend Copy dialog)
# ============================================================================
echo "--- Test 8: Batch Copy Into Nested Dir ---"

REPO_ID=$(create_test_library "fe-test-8-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 8: Could not create library"
else
    create_directory "$REPO_ID" "/original"
    upload_file "$REPO_ID" "/original" "copyme.txt" "copy this content"
    create_directory "$REPO_ID" "/backup"
    create_directory "$REPO_ID" "/backup/archive"
    sleep 1

    # Batch copy (v2.1 endpoint used by frontend)
    response=$(api2_post_json "/api/v2.1/repos/sync-batch-copy-item/" \
        "{\"src_repo_id\":\"$REPO_ID\",\"src_parent_dir\":\"/original\",\"src_dirents\":[\"copyme.txt\"],\"dst_repo_id\":\"$REPO_ID\",\"dst_parent_dir\":\"/backup/archive\"}")
    copy_code=$(get_http_code "$response")
    log_verbose "Batch copy: HTTP $copy_code"

    if [ "$copy_code" = "200" ]; then
        sleep 1

        # Verify copy in destination
        body=$(v21_list_directory "$REPO_ID" "/backup/archive")
        if echo "$body" | grep -q '"name":"copyme.txt"'; then
            log_success "Test 8: File copied into nested dir (v2.1 verified)"
        else
            log_fail "Test 8: Copied file not found in destination"
            log_verbose "Body: $body"
        fi

        # Verify original still exists
        body=$(v21_list_directory "$REPO_ID" "/original")
        if echo "$body" | grep -q '"name":"copyme.txt"'; then
            log_success "Test 8: Original file preserved after copy"
        else
            log_fail "Test 8: Original file disappeared after copy"
        fi
    else
        log_fail "Test 8: Batch copy failed (HTTP $copy_code)"
        log_verbose "Response: $(get_body "$response")"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 9: Delete folder in nested structure, verify sibling intact
# ============================================================================
echo "--- Test 9: Delete Nested Folder (Frontend Delete Dialog) ---"

REPO_ID=$(create_test_library "fe-test-9-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 9: Could not create library"
else
    create_directory "$REPO_ID" "/shared"
    create_directory "$REPO_ID" "/shared/docs"
    create_directory "$REPO_ID" "/shared/images"
    upload_file "$REPO_ID" "/shared/docs" "report.txt" "quarterly report"
    upload_file "$REPO_ID" "/shared/images" "logo.png" "fake-png-data"
    sleep 1

    # Delete /shared/images folder
    response=$(api2_delete "/api2/repos/${REPO_ID}/dir/?p=/shared/images")
    del_code=$(get_http_code "$response")

    if [ "$del_code" = "200" ]; then
        sleep 1

        # Verify /shared/docs still exists with its file
        body=$(v21_list_directory "$REPO_ID" "/shared")
        if echo "$body" | grep -q '"name":"docs"' && ! echo "$body" | grep -q '"name":"images"'; then
            log_success "Test 9: Sibling folder preserved, deleted folder gone (v2.1)"
        else
            log_fail "Test 9: Unexpected parent listing after folder delete"
            log_verbose "Body: $body"
        fi

        body=$(v21_list_directory "$REPO_ID" "/shared/docs")
        if echo "$body" | grep -q '"name":"report.txt"'; then
            log_success "Test 9: Sibling folder contents intact (v2.1)"
        else
            log_fail "Test 9: Sibling folder contents lost"
        fi
    else
        log_fail "Test 9: Folder delete failed (HTTP $del_code)"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 10: Dirent fields contain required frontend data
# The frontend JS models expect specific fields in each dirent.
# ============================================================================
echo "--- Test 10: Dirent Fields for Frontend Models ---"

REPO_ID=$(create_test_library "fe-test-10-$(date +%s)")
if [ -z "$REPO_ID" ]; then
    log_fail "Test 10: Could not create library"
else
    create_directory "$REPO_ID" "/testdir"
    upload_file "$REPO_ID" "/" "testfile.txt" "test content"
    sleep 1

    body=$(v21_list_directory "$REPO_ID" "/")
    log_verbose "Dirent response: $body"

    # Check that file dirents have the fields the frontend Dirent model expects
    checks_ok=0

    # The frontend expects: id, name, type, size, mtime, permission
    echo "$body" | grep -q '"id"' && ((checks_ok++))
    echo "$body" | grep -q '"name"' && ((checks_ok++))
    echo "$body" | grep -q '"type"' && ((checks_ok++))
    echo "$body" | grep -q '"mtime"' && ((checks_ok++))
    echo "$body" | grep -q '"permission"' && ((checks_ok++))

    if [ "$checks_ok" -eq 5 ]; then
        log_success "Test 10: All required dirent fields present (id, name, type, mtime, permission)"
    else
        log_fail "Test 10: Missing $((5-checks_ok)) required dirent fields"
    fi

    # Check dir_id is non-empty (frontend uses it for caching)
    if echo "$body" | grep -q '"dir_id":"[a-f0-9]'; then
        log_success "Test 10: dir_id is a valid hash (frontend cache key)"
    else
        log_fail "Test 10: dir_id missing or invalid"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Test 11: Create File in Nested Folder (Frontend CreateFile Dialog Regression)
# ============================================================================
# This is the exact user-reported bug: Create folder → Create Word doc inside
# → "Folder does not exist" error when navigating back to the folder.
# The frontend calls POST /api/v2.1/repos/:id/file/?p=/folder/file.docx
# with form data operation=create.
echo "--- Test 11: CreateFile in Nested Folder (Frontend Dialog Regression) ---"
REPO_ID=$(create_test_library "fe-test-11-$(date +%s)")
if [ -n "$REPO_ID" ]; then
    # Create folder /projects
    create_directory "$REPO_ID" "/projects"

    # Create a docx file inside /projects (mimics frontend CreateFile dialog)
    response=$(api2_post_form "/api/v2.1/repos/$REPO_ID/file/?p=/projects/report.docx" \
        -F "operation=create")
    status=$(get_http_code "$response")

    if [ "$status" = "201" ]; then
        # THE KEY TEST: Navigate back to /projects via v2.1 (what frontend does)
        response=$(v21_get "/api/v2.1/repos/$REPO_ID/dir/?p=/projects")
        body=$(get_body "$response")
        if echo "$body" | grep -q '"report.docx"'; then
            log_success "Test 11: File visible in nested folder after CreateFile (depth 1)"
        else
            log_fail "Test 11: Folder does not show file after CreateFile"
        fi

        # Verify root also works
        response=$(v21_get "/api/v2.1/repos/$REPO_ID/dir/?p=/")
        root_body=$(get_body "$response")
        if echo "$root_body" | grep -q '"projects"'; then
            log_success "Test 11: Root still lists folder after nested CreateFile"
        else
            log_fail "Test 11: Root broken after nested CreateFile"
        fi
    else
        log_fail "Test 11: CreateFile returned status $status (expected 201)"
    fi

    # Deeper test: /a/b/c/doc.docx
    create_directory "$REPO_ID" "/a"
    create_directory "$REPO_ID" "/a/b"
    create_directory "$REPO_ID" "/a/b/c"

    api2_post_form "/api/v2.1/repos/$REPO_ID/file/?p=/a/b/c/deep.docx" \
        -F "operation=create" > /dev/null

    response=$(v21_get "/api/v2.1/repos/$REPO_ID/dir/?p=/a/b/c")
    body=$(get_body "$response")
    if echo "$body" | grep -q '"deep.docx"'; then
        log_success "Test 11: File visible at depth 4 after CreateFile"
    else
        log_fail "Test 11: Folder does not show file at depth 4"
    fi

    # Verify all ancestor levels
    ok=true
    for p in "/a/b" "/a" "/"; do
        response=$(v21_get "/api/v2.1/repos/$REPO_ID/dir/?p=$p")
        check_body=$(get_body "$response")
        if echo "$check_body" | grep -q '"error"'; then
            ok=false
            log_fail "Test 11: Ancestor $p broken after deep CreateFile"
            break
        fi
    done
    if $ok; then
        log_success "Test 11: All ancestors intact after deep CreateFile"
    fi

    delete_test_library "$REPO_ID"
fi
echo ""

# ============================================================================
# Summary
# ============================================================================
echo "==================================================="
echo "  Frontend API Test Summary"
echo "==================================================="
echo ""
echo -e "  ${GREEN}Passed:${NC}  $TESTS_PASSED"
echo -e "  ${RED}Failed:${NC}  $TESTS_FAILED"
echo ""

TOTAL=$((TESTS_PASSED + TESTS_FAILED))
if [ "$TESTS_FAILED" -eq 0 ]; then
    echo -e "${GREEN}All $TOTAL tests passed!${NC}"
    exit 0
else
    echo -e "${RED}$TESTS_FAILED of $TOTAL tests failed${NC}"
    exit 1
fi
