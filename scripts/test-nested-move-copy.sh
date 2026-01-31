#!/bin/bash
# =============================================================================
# Nested Move/Copy Integration Tests for SesameFS
# =============================================================================
#
# Tests move and copy operations with nested folder hierarchies at various depths.
# These are regression tests for the tree-rebuild logic (RebuildPathToRoot) that
# must correctly update ancestor fs_objects when moving/copying into or out of
# deeply nested directories.
#
# Test Scenarios:
#   1.  Move file: root → depth-1
#   2.  Move file: root → depth-2
#   3.  Move file: root → depth-3
#   4.  Move file: depth-2 → depth-2 (sibling)
#   5.  Move file: depth-3 → root
#   6.  Move folder: root → depth-2
#   7.  Move folder with contents: depth-1 → depth-2
#   8.  Move multiple items in one batch: root → depth-2
#   9.  Copy file: root → depth-1
#  10.  Copy file: root → depth-2
#  11.  Copy file: root → depth-3
#  12.  Copy file: depth-2 → depth-2 (sibling)
#  13.  Copy file: depth-3 → root
#  14.  Copy folder: root → depth-2
#  15.  Copy folder with contents: depth-1 → depth-2
#  16.  Copy multiple items in one batch: root → depth-2
#  17.  Verify tree integrity after chained moves (multiple sequential moves)
#  18.  Verify tree integrity after chained copies
#  19.  Move into freshly-created deep path (depth-4)
#  20.  Copy into freshly-created deep path (depth-4)
#
# Usage:
#   ./scripts/test-nested-move-copy.sh [options]
#
# Options:
#   --quick       Skip slow deep-path tests
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
        log_fail "$test_name (missing '$needle' in response)"
        FAILED_TESTS=$((FAILED_TESTS + 1))
        FAILED_TEST_NAMES+=("$test_name")
        if [ "$VERBOSE" = true ]; then
            echo -e "  ${YELLOW}Response:${NC} $haystack" | head -5
        fi
    fi
}

run_test_not_contains() {
    local test_name="$1"
    local needle="$2"
    local haystack="$3"

    TOTAL_TESTS=$((TOTAL_TESTS + 1))

    if ! echo "$haystack" | grep -q "$needle"; then
        log_success "$test_name (correctly missing '$needle')"
        PASSED_TESTS=$((PASSED_TESTS + 1))
    else
        log_fail "$test_name (unexpectedly found '$needle')"
        FAILED_TESTS=$((FAILED_TESTS + 1))
        FAILED_TEST_NAMES+=("$test_name")
    fi
}

# Returns HTTP status code only
api_status() {
    local method="$1" endpoint="$2" token="$3" data="$4"
    local url="${API_URL}${endpoint}"
    local opts=(-s -o /dev/null -w "%{http_code}")
    if [ -n "$token" ]; then opts+=(-H "Authorization: Token $token"); fi
    opts+=(-H "Content-Type: application/json")
    if [ -n "$data" ]; then opts+=(-d "$data"); fi
    curl "${opts[@]}" -X "$method" "$url"
}

# Returns response body
api_body() {
    local method="$1" endpoint="$2" token="$3" data="$4"
    local url="${API_URL}${endpoint}"
    local opts=(-s)
    if [ -n "$token" ]; then opts+=(-H "Authorization: Token $token"); fi
    opts+=(-H "Content-Type: application/json")
    if [ -n "$data" ]; then opts+=(-d "$data"); fi
    local body
    body=$(curl "${opts[@]}" -X "$method" "$url")
    if [ "$VERBOSE" = true ]; then
        echo -e "${YELLOW}[RESP]${NC} $method $endpoint" >&2
        echo "$body" | jq . 2>/dev/null >&2 || echo "$body" >&2
    fi
    echo "$body"
}

# Create a library, return repo_id
create_library() {
    local name="$1"
    local body
    body=$(api_body "POST" "/api/v2.1/repos/" "$ADMIN_TOKEN" "{\"repo_name\":\"$name\"}")
    echo "$body" | jq -r '.repo_id // empty'
}

# Create a directory (mkdir)
create_dir() {
    local repo_id="$1" path="$2"
    api_status "POST" "/api/v2.1/repos/${repo_id}/dir/?p=${path}" "$ADMIN_TOKEN" '{}' > /dev/null
}

# Create a file with content via the create operation
create_file() {
    local repo_id="$1" path="$2"
    api_status "POST" "/api/v2.1/repos/${repo_id}/file/?p=${path}&operation=create" "$ADMIN_TOKEN" > /dev/null
}

# List directory contents
list_dir() {
    local repo_id="$1" path="$2"
    api_body "GET" "/api/v2.1/repos/${repo_id}/dir/?p=${path}" "$ADMIN_TOKEN"
}

# Sync batch move
batch_move() {
    local src_repo="$1" src_dir="$2" dst_repo="$3" dst_dir="$4"
    shift 4
    # Remaining args are dirent names
    local dirents=""
    for d in "$@"; do
        if [ -n "$dirents" ]; then dirents="${dirents},"; fi
        dirents="${dirents}\"${d}\""
    done
    api_status "POST" "/api/v2.1/repos/sync-batch-move-item/" "$ADMIN_TOKEN" \
        "{\"src_repo_id\":\"${src_repo}\",\"src_parent_dir\":\"${src_dir}\",\"dst_repo_id\":\"${dst_repo}\",\"dst_parent_dir\":\"${dst_dir}\",\"src_dirents\":[${dirents}]}"
}

# Sync batch copy
batch_copy() {
    local src_repo="$1" src_dir="$2" dst_repo="$3" dst_dir="$4"
    shift 4
    local dirents=""
    for d in "$@"; do
        if [ -n "$dirents" ]; then dirents="${dirents},"; fi
        dirents="${dirents}\"${d}\""
    done
    api_status "POST" "/api/v2.1/repos/sync-batch-copy-item/" "$ADMIN_TOKEN" \
        "{\"src_repo_id\":\"${src_repo}\",\"src_parent_dir\":\"${src_dir}\",\"dst_repo_id\":\"${dst_repo}\",\"dst_parent_dir\":\"${dst_dir}\",\"src_dirents\":[${dirents}]}"
}

# Delete a library
delete_library() {
    local repo_id="$1"
    api_status "DELETE" "/api/v2.1/repos/${repo_id}/" "$ADMIN_TOKEN" > /dev/null
}

# Parse options
for arg in "$@"; do
    case $arg in
        --quick)   QUICK_MODE=true ;;
        --verbose) VERBOSE=true ;;
        --help)
            head -45 "$0" | tail -40
            exit 0
            ;;
    esac
done

# =============================================================================
# Pre-flight
# =============================================================================

preflight() {
    log_section "Pre-flight Checks"
    if ! curl -s -o /dev/null -w "%{http_code}" "${API_URL}/api2/ping/" | grep -q "200"; then
        log_fail "API not reachable at ${API_URL}"
        echo "  Start the backend: docker compose up -d sesamefs"
        exit 1
    fi
    log_success "API is reachable at ${API_URL}"

    if ! command -v jq &> /dev/null; then
        log_fail "jq is required but not installed"
        exit 1
    fi

    local status=$(api_status "GET" "/api2/account/info/" "$ADMIN_TOKEN")
    run_test "Admin token is valid" "200" "$status"
}

# =============================================================================
# Setup: Create test library with nested structure
# =============================================================================

REPO_ID=""

setup_library() {
    log_section "Setup: Creating test library with nested structure"

    local ts=$(date +%s)
    REPO_ID=$(create_library "nested-move-copy-test-${ts}")

    if [ -z "$REPO_ID" ]; then
        log_fail "Could not create test library"
        exit 1
    fi
    log_info "Created library: $REPO_ID"

    # Build folder tree:
    #   /a/
    #   /a/b/
    #   /a/b/c/
    #   /a/b/c/d/         (depth 4)
    #   /x/
    #   /x/y/
    #   /x/y/z/
    create_dir "$REPO_ID" "/a"
    create_dir "$REPO_ID" "/a/b"
    create_dir "$REPO_ID" "/a/b/c"
    create_dir "$REPO_ID" "/a/b/c/d"
    create_dir "$REPO_ID" "/x"
    create_dir "$REPO_ID" "/x/y"
    create_dir "$REPO_ID" "/x/y/z"

    # Create test files at various locations
    create_file "$REPO_ID" "/root-file1.md"
    create_file "$REPO_ID" "/root-file2.md"
    create_file "$REPO_ID" "/root-file3.md"
    create_file "$REPO_ID" "/root-file4.md"
    create_file "$REPO_ID" "/root-file5.md"
    create_file "$REPO_ID" "/root-file6.md"
    create_file "$REPO_ID" "/a/file-a.md"
    create_file "$REPO_ID" "/a/b/file-ab.md"
    create_file "$REPO_ID" "/a/b/c/file-abc.md"
    create_file "$REPO_ID" "/x/y/file-xy.md"
    create_file "$REPO_ID" "/x/y/z/file-xyz.md"

    # Create folders that will be moved/copied
    create_dir "$REPO_ID" "/folder-to-move"
    create_file "$REPO_ID" "/folder-to-move/inside.md"
    create_dir "$REPO_ID" "/folder-to-copy"
    create_file "$REPO_ID" "/folder-to-copy/inside.md"
    create_dir "$REPO_ID" "/a/sub-to-move"
    create_file "$REPO_ID" "/a/sub-to-move/nested-inside.md"
    create_dir "$REPO_ID" "/a/sub-to-copy"
    create_file "$REPO_ID" "/a/sub-to-copy/nested-inside.md"

    # Multiple items for batch test
    create_file "$REPO_ID" "/batch1.md"
    create_file "$REPO_ID" "/batch2.md"
    create_file "$REPO_ID" "/batch3.md"
    create_file "$REPO_ID" "/batch-c1.md"
    create_file "$REPO_ID" "/batch-c2.md"
    create_file "$REPO_ID" "/batch-c3.md"

    sleep 1
    log_info "Setup complete — folder tree created"
}

# =============================================================================
# MOVE TESTS
# =============================================================================

test_move_root_to_depth1() {
    log_section "1. Move file: root → depth-1 (/a/)"

    local status=$(batch_move "$REPO_ID" "/" "$REPO_ID" "/a" "root-file1.md")
    run_test "Move root→depth-1 returns 200" "200" "$status"

    local body=$(list_dir "$REPO_ID" "/a")
    run_test_contains "File appears in /a/" '"name":"root-file1.md"' "$body"

    body=$(list_dir "$REPO_ID" "/")
    run_test_not_contains "File removed from root" '"name":"root-file1.md"' "$body"
}

test_move_root_to_depth2() {
    log_section "2. Move file: root → depth-2 (/a/b/)"

    local status=$(batch_move "$REPO_ID" "/" "$REPO_ID" "/a/b" "root-file2.md")
    run_test "Move root→depth-2 returns 200" "200" "$status"

    local body=$(list_dir "$REPO_ID" "/a/b")
    run_test_contains "File appears in /a/b/" '"name":"root-file2.md"' "$body"

    body=$(list_dir "$REPO_ID" "/")
    run_test_not_contains "File removed from root" '"name":"root-file2.md"' "$body"

    # Verify ancestors are intact
    body=$(list_dir "$REPO_ID" "/a")
    run_test_contains "Parent /a/ still has /b/" '"name":"b"' "$body"
}

test_move_root_to_depth3() {
    log_section "3. Move file: root → depth-3 (/a/b/c/)"

    local status=$(batch_move "$REPO_ID" "/" "$REPO_ID" "/a/b/c" "root-file3.md")
    run_test "Move root→depth-3 returns 200" "200" "$status"

    local body=$(list_dir "$REPO_ID" "/a/b/c")
    run_test_contains "File appears in /a/b/c/" '"name":"root-file3.md"' "$body"

    # Verify entire ancestor chain is intact
    body=$(list_dir "$REPO_ID" "/")
    run_test_contains "Root still has /a/" '"name":"a"' "$body"

    body=$(list_dir "$REPO_ID" "/a")
    run_test_contains "/a/ still has /b/" '"name":"b"' "$body"

    body=$(list_dir "$REPO_ID" "/a/b")
    run_test_contains "/a/b/ still has /c/" '"name":"c"' "$body"
}

test_move_depth2_to_depth2() {
    log_section "4. Move file: depth-2 → depth-2 (/a/b/ → /x/y/)"

    local status=$(batch_move "$REPO_ID" "/a/b" "$REPO_ID" "/x/y" "file-ab.md")
    run_test "Move depth-2→depth-2 returns 200" "200" "$status"

    local body=$(list_dir "$REPO_ID" "/x/y")
    run_test_contains "File appears in /x/y/" '"name":"file-ab.md"' "$body"

    body=$(list_dir "$REPO_ID" "/a/b")
    run_test_not_contains "File removed from /a/b/" '"name":"file-ab.md"' "$body"

    # Verify both ancestor chains intact
    body=$(list_dir "$REPO_ID" "/a")
    run_test_contains "/a/ still has /b/" '"name":"b"' "$body"

    body=$(list_dir "$REPO_ID" "/x")
    run_test_contains "/x/ still has /y/" '"name":"y"' "$body"
}

test_move_depth3_to_root() {
    log_section "5. Move file: depth-3 → root (/a/b/c/ → /)"

    local status=$(batch_move "$REPO_ID" "/a/b/c" "$REPO_ID" "/" "file-abc.md")
    run_test "Move depth-3→root returns 200" "200" "$status"

    local body=$(list_dir "$REPO_ID" "/")
    run_test_contains "File appears in root" '"name":"file-abc.md"' "$body"

    body=$(list_dir "$REPO_ID" "/a/b/c")
    run_test_not_contains "File removed from /a/b/c/" '"name":"file-abc.md"' "$body"
}

test_move_folder_root_to_depth2() {
    log_section "6. Move folder: root → depth-2 (/folder-to-move → /x/y/)"

    local status=$(batch_move "$REPO_ID" "/" "$REPO_ID" "/x/y" "folder-to-move")
    run_test "Move folder root→depth-2 returns 200" "200" "$status"

    local body=$(list_dir "$REPO_ID" "/x/y")
    run_test_contains "Folder appears in /x/y/" '"name":"folder-to-move"' "$body"

    # Verify folder contents survived the move
    body=$(list_dir "$REPO_ID" "/x/y/folder-to-move")
    run_test_contains "Folder contents intact after move" '"name":"inside.md"' "$body"

    body=$(list_dir "$REPO_ID" "/")
    run_test_not_contains "Folder removed from root" '"name":"folder-to-move"' "$body"
}

test_move_folder_depth1_to_depth2() {
    log_section "7. Move folder with contents: depth-1 → depth-2 (/a/sub-to-move → /x/y/)"

    local status=$(batch_move "$REPO_ID" "/a" "$REPO_ID" "/x/y" "sub-to-move")
    run_test "Move folder depth-1→depth-2 returns 200" "200" "$status"

    local body=$(list_dir "$REPO_ID" "/x/y")
    run_test_contains "Folder appears in /x/y/" '"name":"sub-to-move"' "$body"

    # Verify nested contents survived
    body=$(list_dir "$REPO_ID" "/x/y/sub-to-move")
    run_test_contains "Nested contents intact after move" '"name":"nested-inside.md"' "$body"

    body=$(list_dir "$REPO_ID" "/a")
    run_test_not_contains "Folder removed from /a/" '"name":"sub-to-move"' "$body"
}

test_move_multiple_batch() {
    log_section "8. Move multiple items in batch: root → depth-2 (/a/b/)"

    local status=$(batch_move "$REPO_ID" "/" "$REPO_ID" "/a/b" "batch1.md" "batch2.md" "batch3.md")
    run_test "Batch move (3 items) returns 200" "200" "$status"

    local body=$(list_dir "$REPO_ID" "/a/b")
    run_test_contains "batch1.md in /a/b/" '"name":"batch1.md"' "$body"
    run_test_contains "batch2.md in /a/b/" '"name":"batch2.md"' "$body"
    run_test_contains "batch3.md in /a/b/" '"name":"batch3.md"' "$body"

    body=$(list_dir "$REPO_ID" "/")
    run_test_not_contains "batch1.md removed from root" '"name":"batch1.md"' "$body"
    run_test_not_contains "batch2.md removed from root" '"name":"batch2.md"' "$body"
    run_test_not_contains "batch3.md removed from root" '"name":"batch3.md"' "$body"
}

# =============================================================================
# COPY TESTS
# =============================================================================

test_copy_root_to_depth1() {
    log_section "9. Copy file: root → depth-1 (/a/)"

    local status=$(batch_copy "$REPO_ID" "/" "$REPO_ID" "/a" "root-file4.md")
    run_test "Copy root→depth-1 returns 200" "200" "$status"

    local body=$(list_dir "$REPO_ID" "/a")
    run_test_contains "Copy appears in /a/" '"name":"root-file4.md"' "$body"

    body=$(list_dir "$REPO_ID" "/")
    run_test_contains "Original still in root" '"name":"root-file4.md"' "$body"
}

test_copy_root_to_depth2() {
    log_section "10. Copy file: root → depth-2 (/a/b/)"

    local status=$(batch_copy "$REPO_ID" "/" "$REPO_ID" "/a/b" "root-file5.md")
    run_test "Copy root→depth-2 returns 200" "200" "$status"

    local body=$(list_dir "$REPO_ID" "/a/b")
    run_test_contains "Copy appears in /a/b/" '"name":"root-file5.md"' "$body"

    body=$(list_dir "$REPO_ID" "/")
    run_test_contains "Original still in root" '"name":"root-file5.md"' "$body"

    # Verify ancestors intact
    body=$(list_dir "$REPO_ID" "/a")
    run_test_contains "Parent /a/ still has /b/" '"name":"b"' "$body"
}

test_copy_root_to_depth3() {
    log_section "11. Copy file: root → depth-3 (/x/y/z/)"

    local status=$(batch_copy "$REPO_ID" "/" "$REPO_ID" "/x/y/z" "root-file6.md")
    run_test "Copy root→depth-3 returns 200" "200" "$status"

    local body=$(list_dir "$REPO_ID" "/x/y/z")
    run_test_contains "Copy appears in /x/y/z/" '"name":"root-file6.md"' "$body"

    body=$(list_dir "$REPO_ID" "/")
    run_test_contains "Original still in root" '"name":"root-file6.md"' "$body"

    # Verify entire ancestor chain
    body=$(list_dir "$REPO_ID" "/x")
    run_test_contains "/x/ still has /y/" '"name":"y"' "$body"

    body=$(list_dir "$REPO_ID" "/x/y")
    run_test_contains "/x/y/ still has /z/" '"name":"z"' "$body"
}

test_copy_depth2_to_depth2() {
    log_section "12. Copy file: depth-2 → depth-2 (/x/y/ → /a/b/)"

    local status=$(batch_copy "$REPO_ID" "/x/y" "$REPO_ID" "/a/b" "file-xy.md")
    run_test "Copy depth-2→depth-2 returns 200" "200" "$status"

    local body=$(list_dir "$REPO_ID" "/a/b")
    run_test_contains "Copy appears in /a/b/" '"name":"file-xy.md"' "$body"

    body=$(list_dir "$REPO_ID" "/x/y")
    run_test_contains "Original still in /x/y/" '"name":"file-xy.md"' "$body"
}

test_copy_depth3_to_root() {
    log_section "13. Copy file: depth-3 → root (/x/y/z/ → /)"

    local status=$(batch_copy "$REPO_ID" "/x/y/z" "$REPO_ID" "/" "file-xyz.md")
    run_test "Copy depth-3→root returns 200" "200" "$status"

    local body=$(list_dir "$REPO_ID" "/")
    run_test_contains "Copy appears in root" '"name":"file-xyz.md"' "$body"

    body=$(list_dir "$REPO_ID" "/x/y/z")
    run_test_contains "Original still in /x/y/z/" '"name":"file-xyz.md"' "$body"
}

test_copy_folder_root_to_depth2() {
    log_section "14. Copy folder: root → depth-2 (/folder-to-copy → /x/y/)"

    local status=$(batch_copy "$REPO_ID" "/" "$REPO_ID" "/x/y" "folder-to-copy")
    run_test "Copy folder root→depth-2 returns 200" "200" "$status"

    local body=$(list_dir "$REPO_ID" "/x/y")
    run_test_contains "Folder copy appears in /x/y/" '"name":"folder-to-copy"' "$body"

    # Verify folder contents in copy
    body=$(list_dir "$REPO_ID" "/x/y/folder-to-copy")
    run_test_contains "Copied folder contents intact" '"name":"inside.md"' "$body"

    # Verify original still exists
    body=$(list_dir "$REPO_ID" "/")
    run_test_contains "Original folder still in root" '"name":"folder-to-copy"' "$body"
}

test_copy_folder_depth1_to_depth2() {
    log_section "15. Copy folder with contents: depth-1 → depth-2 (/a/sub-to-copy → /x/y/)"

    local status=$(batch_copy "$REPO_ID" "/a" "$REPO_ID" "/x/y" "sub-to-copy")
    run_test "Copy folder depth-1→depth-2 returns 200" "200" "$status"

    local body=$(list_dir "$REPO_ID" "/x/y")
    run_test_contains "Folder copy appears in /x/y/" '"name":"sub-to-copy"' "$body"

    # Verify nested contents in copy
    body=$(list_dir "$REPO_ID" "/x/y/sub-to-copy")
    run_test_contains "Copied nested contents intact" '"name":"nested-inside.md"' "$body"

    # Verify original still exists
    body=$(list_dir "$REPO_ID" "/a")
    run_test_contains "Original folder still in /a/" '"name":"sub-to-copy"' "$body"
}

test_copy_multiple_batch() {
    log_section "16. Copy multiple items in batch: root → depth-2 (/a/b/)"

    local status=$(batch_copy "$REPO_ID" "/" "$REPO_ID" "/a/b" "batch-c1.md" "batch-c2.md" "batch-c3.md")
    run_test "Batch copy (3 items) returns 200" "200" "$status"

    local body=$(list_dir "$REPO_ID" "/a/b")
    run_test_contains "batch-c1.md in /a/b/" '"name":"batch-c1.md"' "$body"
    run_test_contains "batch-c2.md in /a/b/" '"name":"batch-c2.md"' "$body"
    run_test_contains "batch-c3.md in /a/b/" '"name":"batch-c3.md"' "$body"

    # Originals still in root
    body=$(list_dir "$REPO_ID" "/")
    run_test_contains "batch-c1.md still in root" '"name":"batch-c1.md"' "$body"
    run_test_contains "batch-c2.md still in root" '"name":"batch-c2.md"' "$body"
    run_test_contains "batch-c3.md still in root" '"name":"batch-c3.md"' "$body"
}

# =============================================================================
# CHAINED OPERATIONS (sequential moves/copies that stress tree rebuild)
# =============================================================================

test_chained_moves() {
    log_section "17. Chained moves — multiple sequential moves in same tree"

    # Move file-a.md from /a/ to /a/b/c/ then back to /a/
    # This stresses the rebuild logic with rapid ancestor updates

    local status=$(batch_move "$REPO_ID" "/a" "$REPO_ID" "/a/b/c" "file-a.md")
    run_test "Chain move 1: /a/ → /a/b/c/ returns 200" "200" "$status"

    local body=$(list_dir "$REPO_ID" "/a/b/c")
    run_test_contains "File arrived in /a/b/c/" '"name":"file-a.md"' "$body"

    # Move it back
    status=$(batch_move "$REPO_ID" "/a/b/c" "$REPO_ID" "/a" "file-a.md")
    run_test "Chain move 2: /a/b/c/ → /a/ returns 200" "200" "$status"

    body=$(list_dir "$REPO_ID" "/a")
    run_test_contains "File back in /a/" '"name":"file-a.md"' "$body"

    # Verify full tree integrity — all directories still navigable
    body=$(list_dir "$REPO_ID" "/a/b")
    run_test_contains "After chained moves: /a/b/ has /c/" '"name":"c"' "$body"

    body=$(list_dir "$REPO_ID" "/a/b/c")
    run_test_contains "After chained moves: /a/b/c/ has /d/" '"name":"d"' "$body"
}

test_chained_copies() {
    log_section "18. Chained copies — copy cascading through nested dirs"

    # Copy file-a.md from /a/ → /a/b/, then /a/b/ → /a/b/c/
    local status=$(batch_copy "$REPO_ID" "/a" "$REPO_ID" "/a/b" "file-a.md")
    run_test "Chain copy 1: /a/ → /a/b/ returns 200" "200" "$status"

    status=$(batch_copy "$REPO_ID" "/a/b" "$REPO_ID" "/a/b/c" "file-a.md")
    run_test "Chain copy 2: /a/b/ → /a/b/c/ returns 200" "200" "$status"

    # All three locations should have the file
    local body=$(list_dir "$REPO_ID" "/a")
    run_test_contains "Original still in /a/" '"name":"file-a.md"' "$body"

    body=$(list_dir "$REPO_ID" "/a/b")
    run_test_contains "Copy in /a/b/" '"name":"file-a.md"' "$body"

    body=$(list_dir "$REPO_ID" "/a/b/c")
    run_test_contains "Copy in /a/b/c/" '"name":"file-a.md"' "$body"
}

# =============================================================================
# DEEP PATH TESTS (depth 4+)
# =============================================================================

test_move_into_depth4() {
    log_section "19. Move into depth-4 (/a/b/c/d/)"

    if [ "$QUICK_MODE" = true ]; then
        log_info "Skipping (--quick mode)"
        return
    fi

    # Create a file at root to move deep
    create_file "$REPO_ID" "/deep-move.md"
    sleep 1

    local status=$(batch_move "$REPO_ID" "/" "$REPO_ID" "/a/b/c/d" "deep-move.md")
    run_test "Move root→depth-4 returns 200" "200" "$status"

    local body=$(list_dir "$REPO_ID" "/a/b/c/d")
    run_test_contains "File appears in /a/b/c/d/" '"name":"deep-move.md"' "$body"

    # Verify full chain intact
    body=$(list_dir "$REPO_ID" "/a")
    run_test_contains "After deep move: /a/ has /b/" '"name":"b"' "$body"

    body=$(list_dir "$REPO_ID" "/a/b")
    run_test_contains "After deep move: /a/b/ has /c/" '"name":"c"' "$body"

    body=$(list_dir "$REPO_ID" "/a/b/c")
    run_test_contains "After deep move: /a/b/c/ has /d/" '"name":"d"' "$body"
}

test_copy_into_depth4() {
    log_section "20. Copy into depth-4 (/a/b/c/d/)"

    if [ "$QUICK_MODE" = true ]; then
        log_info "Skipping (--quick mode)"
        return
    fi

    create_file "$REPO_ID" "/deep-copy.md"
    sleep 1

    local status=$(batch_copy "$REPO_ID" "/" "$REPO_ID" "/a/b/c/d" "deep-copy.md")
    run_test "Copy root→depth-4 returns 200" "200" "$status"

    local body=$(list_dir "$REPO_ID" "/a/b/c/d")
    run_test_contains "Copy appears in /a/b/c/d/" '"name":"deep-copy.md"' "$body"

    body=$(list_dir "$REPO_ID" "/")
    run_test_contains "Original still in root" '"name":"deep-copy.md"' "$body"

    # Verify full chain intact
    body=$(list_dir "$REPO_ID" "/a")
    run_test_contains "After deep copy: /a/ has /b/" '"name":"b"' "$body"

    body=$(list_dir "$REPO_ID" "/a/b")
    run_test_contains "After deep copy: /a/b/ has /c/" '"name":"c"' "$body"

    body=$(list_dir "$REPO_ID" "/a/b/c")
    run_test_contains "After deep copy: /a/b/c/ has /d/" '"name":"d"' "$body"
}

# =============================================================================
# Cleanup
# =============================================================================

cleanup() {
    log_section "Cleanup"
    if [ -n "$REPO_ID" ]; then
        delete_library "$REPO_ID"
        log_info "Deleted test library: $REPO_ID"
    fi
}

# =============================================================================
# Summary
# =============================================================================

print_summary() {
    echo ""
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${CYAN}  Nested Move/Copy Test Summary${NC}"
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

main() {
    echo -e "${CYAN}SesameFS Nested Move/Copy Integration Tests${NC}"
    echo -e "API URL: ${API_URL}"
    echo ""

    preflight
    setup_library

    # Move tests (1-8)
    test_move_root_to_depth1
    test_move_root_to_depth2
    test_move_root_to_depth3
    test_move_depth2_to_depth2
    test_move_depth3_to_root
    test_move_folder_root_to_depth2
    test_move_folder_depth1_to_depth2
    test_move_multiple_batch

    # Copy tests (9-16)
    test_copy_root_to_depth1
    test_copy_root_to_depth2
    test_copy_root_to_depth3
    test_copy_depth2_to_depth2
    test_copy_depth3_to_root
    test_copy_folder_root_to_depth2
    test_copy_folder_depth1_to_depth2
    test_copy_multiple_batch

    # Chained operations (17-18)
    test_chained_moves
    test_chained_copies

    # Deep path tests (19-20)
    test_move_into_depth4
    test_copy_into_depth4

    # Cleanup
    if [ "$QUICK_MODE" = false ]; then
        cleanup
    else
        log_info "Skipping cleanup (--quick mode) — library: $REPO_ID"
    fi

    print_summary

    [ $FAILED_TESTS -eq 0 ]
}

main
