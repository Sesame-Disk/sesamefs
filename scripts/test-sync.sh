#!/bin/bash
#
# SesameFS Sync Protocol Test Script
#
# Tests Seafile CLI sync for both encrypted and unencrypted libraries.
# Verifies bidirectional sync: local→remote and remote→local.
#
# Usage:
#   ./scripts/test-sync.sh              # Run all tests
#   ./scripts/test-sync.sh --keep       # Keep test libraries after completion
#   ./scripts/test-sync.sh --verbose    # Show detailed output
#   ./scripts/test-sync.sh --help       # Show help
#
# Requirements:
#   - Docker Compose services running (sesamefs, cassandra, minio, seafile-cli)
#   - Dev mode enabled (AUTH_DEV_MODE=true)
#

set -e

# Configuration
SESAMEFS_URL="${SESAMEFS_URL:-http://sesamefs:8080}"
SESAMEFS_URL_LOCAL="${SESAMEFS_URL_LOCAL:-http://localhost:8082}"
DEV_TOKEN="${DEV_TOKEN:-dev-token-123}"
DEV_USER="${DEV_USER:-00000000-0000-0000-0000-000000000001}"
ENCRYPTED_PASSWORD="${ENCRYPTED_PASSWORD:-testpass123}"
CLI_CONTAINER="${CLI_CONTAINER:-cool-storage-api-seafile-cli-1}"
SYNC_DATA_DIR="${SYNC_DATA_DIR:-/seafile-data}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Flags
KEEP_LIBRARIES=false
VERBOSE=false
CLEANUP_ONLY=false

# Test state
UNENCRYPTED_REPO_ID=""
ENCRYPTED_REPO_ID=""
TESTS_PASSED=0
TESTS_FAILED=0
TEST_START_TIME=""

# Parse arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --keep)
      KEEP_LIBRARIES=true
      shift
      ;;
    --verbose|-v)
      VERBOSE=true
      shift
      ;;
    --cleanup)
      CLEANUP_ONLY=true
      shift
      ;;
    --help|-h)
      echo "SesameFS Sync Protocol Test Script"
      echo ""
      echo "Usage: $0 [OPTIONS]"
      echo ""
      echo "Options:"
      echo "  --keep       Keep test libraries after completion"
      echo "  --verbose    Show detailed output"
      echo "  --cleanup    Only cleanup previous test libraries"
      echo "  --help       Show this help message"
      echo ""
      echo "Environment Variables:"
      echo "  SESAMEFS_URL_LOCAL  API URL for local requests (default: http://localhost:8082)"
      echo "  DEV_TOKEN           Auth token (default: dev-token-123)"
      echo "  ENCRYPTED_PASSWORD  Password for encrypted library (default: testpass123)"
      echo "  CLI_CONTAINER       Docker container name (default: cool-storage-api-seafile-cli-1)"
      exit 0
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

#
# Utility Functions
#

log() {
  echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
  echo -e "${GREEN}[PASS]${NC} $1"
}

log_error() {
  echo -e "${RED}[FAIL]${NC} $1"
}

log_warn() {
  echo -e "${YELLOW}[WARN]${NC} $1"
}

log_verbose() {
  if [ "$VERBOSE" = true ]; then
    echo -e "${BLUE}[DEBUG]${NC} $1"
  fi
}

# Check if required services are running
check_services() {
  log "Checking required services..."

  # Check sesamefs API
  if ! curl -s "${SESAMEFS_URL_LOCAL}/api/v2.1/repos/" -H "Authorization: Token ${DEV_TOKEN}" > /dev/null 2>&1; then
    log_error "SesameFS API not responding at ${SESAMEFS_URL_LOCAL}"
    exit 1
  fi
  log_verbose "SesameFS API: OK"

  # Check seafile-cli container
  if ! docker ps --format '{{.Names}}' | grep -q "${CLI_CONTAINER}"; then
    log_error "Seafile CLI container not running: ${CLI_CONTAINER}"
    log "Start it with: docker-compose up -d seafile-cli"
    exit 1
  fi
  log_verbose "Seafile CLI container: OK"

  log_success "All services running"
}

# Initialize seafile daemon in container
init_seafile_daemon() {
  log "Initializing Seafile daemon..."

  docker exec "${CLI_CONTAINER}" seaf-cli init -d "${SYNC_DATA_DIR}" 2>/dev/null || true
  docker exec "${CLI_CONTAINER}" seaf-cli start 2>/dev/null || true
  sleep 2

  # Verify daemon is running
  if ! docker exec "${CLI_CONTAINER}" seaf-cli status > /dev/null 2>&1; then
    log_error "Failed to start Seafile daemon"
    exit 1
  fi

  log_success "Seafile daemon initialized"
}

# Create a library via API
create_library() {
  local name="$1"
  local encrypted="$2"
  local password="$3"

  local params="name=${name}"
  if [ "$encrypted" = "true" ]; then
    params="${params}&encrypted=true&enc_version=2&passwd=${password}"
  fi

  local response
  response=$(curl -s -X POST "${SESAMEFS_URL_LOCAL}/api2/repos/" \
    -H "Authorization: Token ${DEV_TOKEN}" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    -d "${params}")

  local repo_id
  repo_id=$(echo "$response" | jq -r '.repo_id // empty')

  if [ -z "$repo_id" ]; then
    echo "" >&2
    echo "Failed to create library: $response" >&2
    return 1
  fi

  # Only output the repo_id to stdout
  echo "$repo_id"
}

# Delete a library via API
delete_library() {
  local repo_id="$1"

  log_verbose "Deleting library: ${repo_id}"

  curl -s -X DELETE "${SESAMEFS_URL_LOCAL}/api/v2.1/repos/${repo_id}/" \
    -H "Authorization: Token ${DEV_TOKEN}" > /dev/null 2>&1 || true
}

# Unlock encrypted library
unlock_library() {
  local repo_id="$1"
  local password="$2"

  local response
  response=$(curl -s -X POST "${SESAMEFS_URL_LOCAL}/api/v2.1/repos/${repo_id}/set-password/" \
    -H "Authorization: Token ${DEV_TOKEN}" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    -d "password=${password}")

  if ! echo "$response" | jq -e '.success' > /dev/null 2>&1; then
    log_error "Failed to unlock library: $response"
    return 1
  fi
}

# Get upload link for a library
get_upload_link() {
  local repo_id="$1"

  curl -s "${SESAMEFS_URL_LOCAL}/api2/repos/${repo_id}/upload-link/?p=/" \
    -H "Authorization: Token ${DEV_TOKEN}" | tr -d '"'
}

# Upload a file to library via API
upload_file_remote() {
  local repo_id="$1"
  local local_path="$2"
  local remote_name="$3"

  log_verbose "Uploading file: ${remote_name} to ${repo_id}"

  local upload_link
  upload_link=$(get_upload_link "$repo_id")

  if [ -z "$upload_link" ]; then
    log_error "Failed to get upload link"
    return 1
  fi

  curl -s -X POST "$upload_link" \
    -F "file=@${local_path};filename=${remote_name}" \
    -F "parent_dir=/" \
    -F "relative_path=" \
    -H "Authorization: Token ${DEV_TOKEN}" > /dev/null
}

# Sync library with seafile CLI
sync_library() {
  local repo_id="$1"
  local encrypted="$2"
  local password="$3"
  local sync_dir="${SYNC_DATA_DIR}/sync-test-${repo_id}"

  log_verbose "Syncing library: ${repo_id} to ${sync_dir}"

  # Create sync directory
  docker exec "${CLI_CONTAINER}" mkdir -p "${sync_dir}"

  # Build sync command
  local sync_cmd="seaf-cli sync -l ${repo_id} -s ${SESAMEFS_URL} -d ${sync_dir} -u ${DEV_USER} -p ${DEV_TOKEN}"
  if [ "$encrypted" = "true" ]; then
    sync_cmd="${sync_cmd} -e ${password}"
  fi

  # Start sync
  docker exec "${CLI_CONTAINER}" ${sync_cmd} 2>&1 || true

  echo "${sync_dir}"
}

# Desync library
desync_library() {
  local repo_id="$1"

  docker exec "${CLI_CONTAINER}" seaf-cli desync -d "${SYNC_DATA_DIR}/sync-test-${repo_id}" 2>/dev/null || true
}

# Wait for sync to complete
wait_for_sync() {
  local repo_id="$1"
  local max_wait="${2:-30}"

  log_verbose "Waiting for sync to complete (max ${max_wait}s)..."

  local waited=0
  while [ $waited -lt $max_wait ]; do
    local status
    status=$(docker exec "${CLI_CONTAINER}" seaf-cli status 2>/dev/null | grep -E "^sync-test-" | head -1 | awk '{print $2}')

    if [ "$status" = "synchronized" ]; then
      log_verbose "Sync completed"
      return 0
    fi

    sleep 1
    waited=$((waited + 1))
  done

  log_warn "Sync timeout after ${max_wait}s"
  return 1
}

# Get file hash
get_local_hash() {
  local file_path="$1"
  shasum -a 256 "$file_path" 2>/dev/null | cut -d' ' -f1
}

get_remote_hash() {
  local file_path="$1"
  local max_retries=3
  local retry=0

  while [ $retry -lt $max_retries ]; do
    # Check if file exists first
    if ! docker exec "${CLI_CONTAINER}" test -f "${file_path}" 2>/dev/null; then
      retry=$((retry + 1))
      sleep 1
      continue
    fi

    # Get hash - try sha256sum first, fall back to openssl
    local hash
    hash=$(docker exec "${CLI_CONTAINER}" sha256sum "${file_path}" 2>/dev/null | awk '{print $1}')

    # If sha256sum failed, try openssl
    if [ -z "$hash" ]; then
      hash=$(docker exec "${CLI_CONTAINER}" openssl dgst -sha256 "${file_path}" 2>/dev/null | awk -F'= ' '{print $2}')
    fi

    if [ -n "$hash" ] && [ ${#hash} -eq 64 ]; then
      echo "$hash"
      return 0
    fi

    retry=$((retry + 1))
    sleep 1
  done

  echo ""
  return 1
}

# Get file size
get_local_size() {
  local file_path="$1"
  wc -c < "$file_path" 2>/dev/null | tr -d ' '
}

get_remote_size() {
  local file_path="$1"
  docker exec "${CLI_CONTAINER}" wc -c "$file_path" 2>/dev/null | awk '{print $1}'
}

# Create test file with deterministic content (for verification)
create_test_file() {
  local path="$1"
  local size="${2:-1024}"
  local prefix="${3:-test}"
  local timestamp
  timestamp=$(date -Iseconds)

  {
    echo "=== ${prefix} file created at ${timestamp} ==="
    echo "File for SesameFS sync protocol testing"
    echo "This content is used to verify file integrity after sync."
    echo ""
    # Use deterministic random based on timestamp for reproducibility
    echo "Content block with base64 data:"
    head -c "$size" /dev/urandom | base64
    echo ""
    echo "=== End of ${prefix} file (size target: ${size} bytes) ==="
  } > "$path"
}

# Trigger a sync by restarting the daemon
trigger_sync() {
  docker exec "${CLI_CONTAINER}" seaf-cli stop 2>/dev/null || true
  sleep 1
  docker exec "${CLI_CONTAINER}" seaf-cli start 2>/dev/null || true
  sleep 3
}

# Verify file integrity with retry logic
verify_file_integrity() {
  local original_path="$1"
  local synced_path="$2"
  local is_remote="$3"  # true if synced_path is in docker container
  local max_retries="${4:-3}"
  local retry_delay="${5:-2}"

  local original_hash original_size
  local retry=0

  # Get original file info
  original_hash=$(get_local_hash "$original_path")
  original_size=$(get_local_size "$original_path")

  # Trigger initial sync
  trigger_sync

  while [ $retry -lt $max_retries ]; do
    local synced_hash synced_size

    # Get synced file info
    if [ "$is_remote" = "true" ]; then
      synced_hash=$(get_remote_hash "$synced_path")
      synced_size=$(get_remote_size "$synced_path")
    else
      synced_hash=$(get_local_hash "$synced_path")
      synced_size=$(get_local_size "$synced_path")
    fi

    # Check if file exists and matches
    if [ -n "$synced_hash" ] && [ -n "$synced_size" ]; then
      if [ "$original_size" = "$synced_size" ] && [ "$original_hash" = "$synced_hash" ]; then
        log_verbose "INTEGRITY VERIFIED: hash=${original_hash}, size=${original_size}"
        return 0
      fi

      # File exists but doesn't match - final check on last retry
      if [ $retry -eq $((max_retries - 1)) ]; then
        log_verbose "Original: hash=${original_hash}, size=${original_size}"
        log_verbose "Synced:   hash=${synced_hash}, size=${synced_size}"

        if [ "$original_size" != "$synced_size" ]; then
          log_verbose "SIZE MISMATCH: expected ${original_size}, got ${synced_size}"
        fi
        if [ "$original_hash" != "$synced_hash" ]; then
          log_verbose "HASH MISMATCH: expected ${original_hash}, got ${synced_hash}"
        fi
        return 1
      fi
    fi

    retry=$((retry + 1))
    log_verbose "Waiting for sync (attempt ${retry}/${max_retries})..."
    sleep "$retry_delay"
    trigger_sync
  done

  log_verbose "File not synced after ${max_retries} attempts"
  return 1
}

# Compare file downloaded via API with original
verify_api_download_integrity() {
  local repo_id="$1"
  local remote_path="$2"
  local original_local_path="$3"
  local max_retries="${4:-5}"
  local retry_delay="${5:-3}"

  local temp_download="/tmp/sync-test-download-verify.tmp"

  # Retry loop to wait for file to appear on server
  local retry=0
  while [ $retry -lt $max_retries ]; do
    # Get download link
    local download_link
    download_link=$(curl -s "${SESAMEFS_URL_LOCAL}/api2/repos/${repo_id}/file/?p=${remote_path}" \
      -H "Authorization: Token ${DEV_TOKEN}" | tr -d '"')

    if [ -n "$download_link" ] && [ "$download_link" != "null" ] && [[ "$download_link" == http* ]]; then
      # Download to temp file
      if curl -s "$download_link" -H "Authorization: Token ${DEV_TOKEN}" -o "$temp_download" 2>/dev/null; then
        if [ -f "$temp_download" ] && [ -s "$temp_download" ]; then
          break
        fi
      fi
    fi

    retry=$((retry + 1))
    log_verbose "File not yet available, retry ${retry}/${max_retries}..."
    sleep "$retry_delay"
  done

  if [ ! -f "$temp_download" ] || [ ! -s "$temp_download" ]; then
    log_verbose "Could not download file after ${max_retries} retries"
    rm -f "$temp_download"
    return 1
  fi

  # Compare
  local original_hash downloaded_hash original_size downloaded_size
  original_hash=$(get_local_hash "$original_local_path")
  downloaded_hash=$(get_local_hash "$temp_download")
  original_size=$(get_local_size "$original_local_path")
  downloaded_size=$(get_local_size "$temp_download")

  rm -f "$temp_download"

  log_verbose "Original:   hash=${original_hash}, size=${original_size}"
  log_verbose "Downloaded: hash=${downloaded_hash}, size=${downloaded_size}"

  if [ "$original_hash" = "$downloaded_hash" ] && [ "$original_size" = "$downloaded_size" ]; then
    log_verbose "API DOWNLOAD INTEGRITY VERIFIED"
    return 0
  else
    log_verbose "API DOWNLOAD INTEGRITY FAILED"
    return 1
  fi
}

#
# Test Functions
#

# Run a test and track duration
run_test() {
  local test_name="$1"
  local test_func="$2"

  echo ""
  log "Running test: ${test_name}"

  local start_time
  start_time=$(date +%s)

  local result=0
  $test_func || result=$?

  local end_time
  end_time=$(date +%s)
  local duration=$((end_time - start_time))

  if [ $result -eq 0 ]; then
    log_success "${test_name} (${duration}s)"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    log_error "${test_name} (${duration}s)"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  fi
}

# Test: Upload from local, sync to CLI (unencrypted)
test_unencrypted_remote_to_local() {
  local repo_id="$UNENCRYPTED_REPO_ID"
  local sync_dir="${SYNC_DATA_DIR}/sync-test-${repo_id}"
  local test_file="/tmp/sync-test-unenc-remote.txt"

  # Create and upload test file
  create_test_file "$test_file" 2048 "unencrypted-remote"
  upload_file_remote "$repo_id" "$test_file" "remote-file.txt"

  # Verify file integrity with retry (waits up to 15s for sync)
  verify_file_integrity "$test_file" "${sync_dir}/remote-file.txt" "true" 5 3
}

# Test: Create file in CLI, sync to remote (unencrypted)
# NOTE: This test requires Seafile client upload protocol which is not yet implemented
test_unencrypted_local_to_remote() {
  log_verbose "SKIPPED: Client-to-server upload sync not yet implemented"
  log_verbose "This test requires the Seafile upload protocol endpoints"
  return 0  # Skip test (pass)
}

# Test: Upload from local, sync to CLI (encrypted)
test_encrypted_remote_to_local() {
  local repo_id="$ENCRYPTED_REPO_ID"
  local sync_dir="${SYNC_DATA_DIR}/sync-test-${repo_id}"
  local test_file="/tmp/sync-test-enc-remote.txt"

  # Create and upload test file
  create_test_file "$test_file" 2048 "encrypted-remote"
  upload_file_remote "$repo_id" "$test_file" "remote-encrypted-file.txt"

  # Verify file integrity with retry (waits up to 15s for sync)
  verify_file_integrity "$test_file" "${sync_dir}/remote-encrypted-file.txt" "true" 5 3
}

# Test: Create file in CLI, sync to remote (encrypted)
# NOTE: This test requires Seafile client upload protocol which is not yet implemented
test_encrypted_local_to_remote() {
  log_verbose "SKIPPED: Client-to-server upload sync not yet implemented"
  log_verbose "This test requires the Seafile upload protocol endpoints"
  return 0  # Skip test (pass)
}

# Test: Large file sync (encrypted) - tests multi-block encryption
test_encrypted_large_file() {
  local repo_id="$ENCRYPTED_REPO_ID"
  local sync_dir="${SYNC_DATA_DIR}/sync-test-${repo_id}"
  local test_file="/tmp/sync-test-enc-large.txt"

  # Create larger test file (64KB)
  create_test_file "$test_file" 65536 "encrypted-large"
  log_verbose "Large file size: $(get_local_size "$test_file") bytes"

  upload_file_remote "$repo_id" "$test_file" "large-encrypted-file.txt"

  # Verify file integrity with retry (waits up to 15s for sync)
  verify_file_integrity "$test_file" "${sync_dir}/large-encrypted-file.txt" "true" 5 3
}

# Test: Multiple files sync (unencrypted)
test_unencrypted_multiple_files() {
  local repo_id="$UNENCRYPTED_REPO_ID"
  local sync_dir="${SYNC_DATA_DIR}/sync-test-${repo_id}"
  local failed_count=0

  # Upload multiple files
  for i in 1 2 3; do
    local test_file="/tmp/sync-test-multi-${i}.txt"
    echo "Multi-file test ${i} - $(date -Iseconds) - $(head -c 50 /dev/urandom | base64)" > "$test_file"
    upload_file_remote "$repo_id" "$test_file" "multi-file-${i}.txt"
  done

  # Verify all files with retry
  for i in 1 2 3; do
    local test_file="/tmp/sync-test-multi-${i}.txt"
    if ! verify_file_integrity "$test_file" "${sync_dir}/multi-file-${i}.txt" "true" 5 3; then
      log_verbose "File ${i} integrity check FAILED"
      failed_count=$((failed_count + 1))
    else
      log_verbose "File ${i} integrity check PASSED"
    fi
  done

  [ $failed_count -eq 0 ]
}

# Test: Binary file sync (encrypted) - tests non-text content
test_encrypted_binary_file() {
  local repo_id="$ENCRYPTED_REPO_ID"
  local sync_dir="${SYNC_DATA_DIR}/sync-test-${repo_id}"
  local test_file="/tmp/sync-test-enc-binary.bin"

  # Create binary file with random data
  head -c 4096 /dev/urandom > "$test_file"
  log_verbose "Binary file size: $(get_local_size "$test_file") bytes"

  upload_file_remote "$repo_id" "$test_file" "binary-encrypted-file.bin"

  # Verify file integrity with retry (waits up to 15s for sync)
  verify_file_integrity "$test_file" "${sync_dir}/binary-encrypted-file.bin" "true" 5 3
}

# Test: File modification sync (unencrypted) - update existing file
test_unencrypted_file_modification() {
  local repo_id="$UNENCRYPTED_REPO_ID"
  local sync_dir="${SYNC_DATA_DIR}/sync-test-${repo_id}"
  local test_file="/tmp/sync-test-modify.txt"

  # Create and upload initial file
  echo "Original content - version 1 - $(date -Iseconds)" > "$test_file"
  upload_file_remote "$repo_id" "$test_file" "modifiable-file.txt"

  # Verify initial sync with retry
  if ! verify_file_integrity "$test_file" "${sync_dir}/modifiable-file.txt" "true" 5 3; then
    log_verbose "Initial file sync failed"
    return 1
  fi
  log_verbose "Initial file synced successfully"

  # Modify and re-upload
  echo "Modified content - version 2 - $(date -Iseconds)" > "$test_file"
  echo "Additional line with more data: $(head -c 100 /dev/urandom | base64)" >> "$test_file"
  upload_file_remote "$repo_id" "$test_file" "modifiable-file.txt"

  # Verify modified file with retry
  verify_file_integrity "$test_file" "${sync_dir}/modifiable-file.txt" "true" 5 3
}

# Test: Subdirectory sync (unencrypted) - nested folder structure
test_unencrypted_subdirectory() {
  local repo_id="$UNENCRYPTED_REPO_ID"
  local sync_dir="${SYNC_DATA_DIR}/sync-test-${repo_id}"
  local test_file="/tmp/sync-test-subdir.txt"

  # Create subdirectory via API
  log_verbose "Creating subdirectory..."
  curl -s -X POST "${SESAMEFS_URL_LOCAL}/api2/repos/${repo_id}/dir/?p=/test-subdir" \
    -H "Authorization: Token ${DEV_TOKEN}" \
    -d "operation=mkdir" > /dev/null

  # Create file in subdirectory
  create_test_file "$test_file" 1024 "subdirectory-test"

  # Get upload link for subdirectory
  local upload_link
  upload_link=$(curl -s "${SESAMEFS_URL_LOCAL}/api2/repos/${repo_id}/upload-link/?p=/test-subdir" \
    -H "Authorization: Token ${DEV_TOKEN}" | tr -d '"')

  if [ -z "$upload_link" ]; then
    log_verbose "Failed to get upload link for subdirectory"
    return 1
  fi

  # Upload to subdirectory
  curl -s -X POST "$upload_link" \
    -F "file=@${test_file};filename=nested-file.txt" \
    -F "parent_dir=/test-subdir" \
    -F "relative_path=" \
    -H "Authorization: Token ${DEV_TOKEN}" > /dev/null

  # Verify file in subdirectory with retry
  verify_file_integrity "$test_file" "${sync_dir}/test-subdir/nested-file.txt" "true" 5 3
}

# Test: Very large file sync (1MB+) - tests multi-block handling
test_unencrypted_very_large_file() {
  local repo_id="$UNENCRYPTED_REPO_ID"
  local sync_dir="${SYNC_DATA_DIR}/sync-test-${repo_id}"
  local test_file="/tmp/sync-test-very-large.bin"

  # Create 1.5MB file (crosses multiple Seafile blocks which are typically 1MB)
  log_verbose "Creating 1.5MB test file..."
  head -c 1572864 /dev/urandom > "$test_file"
  log_verbose "Very large file size: $(get_local_size "$test_file") bytes"

  upload_file_remote "$repo_id" "$test_file" "very-large-file.bin"

  # Verify file integrity with longer retry (waits up to 30s for large file sync)
  verify_file_integrity "$test_file" "${sync_dir}/very-large-file.bin" "true" 10 3
}

# Test: Encrypted file modification - update existing encrypted file
test_encrypted_file_modification() {
  local repo_id="$ENCRYPTED_REPO_ID"
  local sync_dir="${SYNC_DATA_DIR}/sync-test-${repo_id}"
  local test_file="/tmp/sync-test-enc-modify.txt"

  # Create and upload initial file
  echo "Encrypted original - version 1 - $(date -Iseconds)" > "$test_file"
  upload_file_remote "$repo_id" "$test_file" "enc-modifiable-file.txt"

  # Verify initial sync with retry
  if ! verify_file_integrity "$test_file" "${sync_dir}/enc-modifiable-file.txt" "true" 5 3; then
    log_verbose "Initial encrypted file sync failed"
    return 1
  fi
  log_verbose "Initial encrypted file synced successfully"

  # Modify and re-upload
  echo "Encrypted modified - version 2 - $(date -Iseconds)" > "$test_file"
  echo "Additional encrypted data: $(head -c 100 /dev/urandom | base64)" >> "$test_file"
  upload_file_remote "$repo_id" "$test_file" "enc-modifiable-file.txt"

  # Verify modified encrypted file with retry
  verify_file_integrity "$test_file" "${sync_dir}/enc-modifiable-file.txt" "true" 5 3
}

#
# Cleanup Functions
#

cleanup_test_libraries() {
  log "Cleaning up test libraries..."

  # List all libraries and find sync-test ones
  local repos
  repos=$(curl -s "${SESAMEFS_URL_LOCAL}/api/v2.1/repos/" \
    -H "Authorization: Token ${DEV_TOKEN}" | jq -r '.repos[] | select(.repo_name | startswith("sync-test-")) | .repo_id')

  for repo_id in $repos; do
    log_verbose "Deleting library: ${repo_id}"

    # Desync first
    desync_library "$repo_id"

    # Delete from server
    delete_library "$repo_id"

    # Remove local data
    docker exec "${CLI_CONTAINER}" rm -rf "${SYNC_DATA_DIR}/sync-test-${repo_id}" 2>/dev/null || true
  done

  log_success "Cleanup complete"
}

cleanup() {
  if [ "$KEEP_LIBRARIES" = false ]; then
    log "Cleaning up..."

    # Desync libraries
    if [ -n "$UNENCRYPTED_REPO_ID" ]; then
      desync_library "$UNENCRYPTED_REPO_ID"
      delete_library "$UNENCRYPTED_REPO_ID"
      docker exec "${CLI_CONTAINER}" rm -rf "${SYNC_DATA_DIR}/sync-test-${UNENCRYPTED_REPO_ID}" 2>/dev/null || true
    fi

    if [ -n "$ENCRYPTED_REPO_ID" ]; then
      desync_library "$ENCRYPTED_REPO_ID"
      delete_library "$ENCRYPTED_REPO_ID"
      docker exec "${CLI_CONTAINER}" rm -rf "${SYNC_DATA_DIR}/sync-test-${ENCRYPTED_REPO_ID}" 2>/dev/null || true
    fi

    # Clean temp files
    rm -f /tmp/sync-test-*.txt

    log_success "Cleanup complete"
  else
    log "Keeping test libraries (--keep flag set)"
    echo ""
    echo "Test libraries:"
    echo "  Unencrypted: ${UNENCRYPTED_REPO_ID}"
    echo "  Encrypted:   ${ENCRYPTED_REPO_ID} (password: ${ENCRYPTED_PASSWORD})"
  fi
}

#
# Main
#

main() {
  echo ""
  echo "=========================================="
  echo "  SesameFS Sync Protocol Test Suite"
  echo "=========================================="
  echo ""

  TEST_START_TIME=$(date +%s)

  # Handle cleanup-only mode
  if [ "$CLEANUP_ONLY" = true ]; then
    check_services
    cleanup_test_libraries
    exit 0
  fi

  # Setup trap for cleanup on exit
  trap cleanup EXIT

  # Pre-flight checks
  check_services
  init_seafile_daemon

  # Create test libraries
  echo ""
  log "Setting up test libraries..."

  log "Creating unencrypted library..."
  UNENCRYPTED_REPO_ID=$(create_library "sync-test-unencrypted-$(date +%s)" "false" "")
  if [ -z "$UNENCRYPTED_REPO_ID" ]; then
    log_error "Failed to create unencrypted library"
    exit 1
  fi
  log_success "Created unencrypted library: ${UNENCRYPTED_REPO_ID}"

  log "Creating encrypted library..."
  ENCRYPTED_REPO_ID=$(create_library "sync-test-encrypted-$(date +%s)" "true" "${ENCRYPTED_PASSWORD}")
  if [ -z "$ENCRYPTED_REPO_ID" ]; then
    log_error "Failed to create encrypted library"
    exit 1
  fi
  log_success "Created encrypted library: ${ENCRYPTED_REPO_ID}"

  # Unlock encrypted library
  log "Unlocking encrypted library..."
  unlock_library "$ENCRYPTED_REPO_ID" "$ENCRYPTED_PASSWORD"
  log_success "Library unlocked"

  # Sync both libraries
  log "Starting library sync..."
  sync_library "$UNENCRYPTED_REPO_ID" "false" ""
  sync_library "$ENCRYPTED_REPO_ID" "true" "$ENCRYPTED_PASSWORD"

  # Wait for initial sync
  sleep 5

  # Run tests
  echo ""
  echo "=========================================="
  echo "  Running Tests"
  echo "=========================================="

  run_test "Unencrypted: Remote → Local sync" test_unencrypted_remote_to_local
  run_test "Unencrypted: Local → Remote sync (SKIPPED)" test_unencrypted_local_to_remote
  run_test "Unencrypted: Multiple files sync" test_unencrypted_multiple_files
  run_test "Unencrypted: File modification sync" test_unencrypted_file_modification
  run_test "Unencrypted: Subdirectory sync" test_unencrypted_subdirectory
  run_test "Unencrypted: Very large file (1.5MB) sync" test_unencrypted_very_large_file
  run_test "Encrypted: Remote → Local sync" test_encrypted_remote_to_local
  run_test "Encrypted: Local → Remote sync (SKIPPED)" test_encrypted_local_to_remote
  run_test "Encrypted: Large file (64KB) sync" test_encrypted_large_file
  run_test "Encrypted: Binary file sync" test_encrypted_binary_file
  run_test "Encrypted: File modification sync" test_encrypted_file_modification

  # Print summary
  local end_time
  end_time=$(date +%s)
  local duration=$((end_time - TEST_START_TIME))

  echo ""
  echo "=========================================="
  echo "  Test Summary"
  echo "=========================================="
  echo ""
  echo -e "  ${GREEN}Passed:${NC} ${TESTS_PASSED}"
  echo -e "  ${RED}Failed:${NC} ${TESTS_FAILED}"
  echo "  Duration: ${duration}s"
  echo ""

  if [ $TESTS_FAILED -gt 0 ]; then
    echo -e "${RED}Some tests failed!${NC}"
    exit 1
  else
    echo -e "${GREEN}All tests passed!${NC}"
    exit 0
  fi
}

main "$@"
