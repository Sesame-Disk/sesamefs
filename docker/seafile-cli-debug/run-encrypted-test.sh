#!/bin/bash

# Test Encrypted Library File Sync
# This reproduces the user's exact procedure

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# Check if local server is running
echo "Checking if local server is running..."
if ! curl -s http://localhost:8080/api2/ping/ > /dev/null 2>&1; then
    echo "ERROR: Local server not running at http://localhost:8080"
    echo "Start it with: go run cmd/sesamefs/main.go"
    exit 1
fi

# Run the test
echo "Running encrypted library file sync test..."
docker run --rm \
    --network host \
    -v "$SCRIPT_DIR/scripts:/scripts:ro" \
    -v "$SCRIPT_DIR/captures:/captures" \
    -e REMOTE_SERVER="https://app.nihaoconsult.com" \
    -e LOCAL_SERVER="http://host.docker.internal:8080" \
    -e REMOTE_USER="abel.aguzmans@gmail.com" \
    -e REMOTE_PASS="$SEAFILE_REMOTE_PASS" \
    -e LOCAL_USER="admin@sesamefs.local" \
    -e LOCAL_PASS="dev-token-123" \
    -e CAPTURE_DIR="/captures" \
    python:3.11-slim \
    bash -c "
        pip install -q requests &&
        python3 /scripts/test_encrypted_file_sync.py
    "

# Show results
LATEST_DIR=$(ls -td "$SCRIPT_DIR/captures/encrypted_sync_"* 2>/dev/null | head -1)
if [ -n "$LATEST_DIR" ]; then
    echo ""
    echo "Results saved to: $LATEST_DIR"
    echo ""

    # Show test results summary
    if [ -f "$LATEST_DIR/test_results.json" ]; then
        echo "Test Results:"
        cat "$LATEST_DIR/test_results.json" | python3 -m json.tool
    fi
fi
