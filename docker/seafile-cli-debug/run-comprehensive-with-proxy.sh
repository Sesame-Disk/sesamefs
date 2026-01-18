#!/bin/bash
# Run comprehensive sync tests with mitmproxy traffic capture
#
# This script:
# 1. Starts the test container with mitmproxy installed
# 2. Captures ALL HTTP traffic to/from both servers
# 3. Runs comprehensive sync protocol tests
# 4. Generates detailed reports with field types, response formats
# 5. Saves all captures for analysis
#
# Usage:
#   ./run-comprehensive-with-proxy.sh --quick
#   ./run-comprehensive-with-proxy.sh --test-all
#   ./run-comprehensive-with-proxy.sh --test-scenario nested_folders

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

echo "================================================================================================"
echo "Comprehensive Seafile Sync Protocol Test with Traffic Capture"
echo "================================================================================================"
echo ""

# Check if local server is running
if ! curl -s http://localhost:8080/api2/ping > /dev/null 2>&1; then
    echo "⚠️  WARNING: Local server (localhost:8080) is not responding"
    echo ""
    echo "Please start SesameFS server:"
    echo "  cd /Users/abel/Documents/Code-Experiments/cool-storage-api"
    echo "  docker-compose up -d sesamefs"
    echo ""
    read -p "Press Enter to continue anyway, or Ctrl+C to abort..."
    echo ""
fi

# Build container if needed
echo "Ensuring container is ready..."
docker build -t cool-storage-api-seafile-cli -f "$SCRIPT_DIR/Dockerfile" "$SCRIPT_DIR" > /dev/null 2>&1 || true

# Clean up old test data
echo "Cleaning up old test data..."
rm -rf /tmp/seafile-sync-test 2>/dev/null || true
mkdir -p "$SCRIPT_DIR/test-results"
mkdir -p "$SCRIPT_DIR/captures"

echo ""
echo "Starting comprehensive sync test..."
echo "This will:"
echo "  1. Create test files via API on BOTH servers"
echo "  2. Sync with desktop client (seaf-cli) on BOTH servers"
echo "  3. Capture ALL HTTP traffic with mitmproxy"
echo "  4. Compare results and generate detailed report"
echo ""

# Run tests in container using entrypoint script
docker run --rm --network host \
    -v "$SCRIPT_DIR/scripts:/scripts:ro" \
    -v /tmp:/tmp \
    -v "$SCRIPT_DIR/captures:/captures" \
    -e SSL_CERT_FILE= \
    -e REQUESTS_CA_BUNDLE= \
    cool-storage-api-seafile-cli \
    bash /scripts/run_test_entrypoint.sh "$@"

EXIT_CODE=$?

# Copy results
echo ""
echo "Copying results to local directory..."
if [ -d /tmp/seafile-sync-test/results ]; then
    cp -r /tmp/seafile-sync-test/results/* "$SCRIPT_DIR/test-results/" 2>/dev/null || true
fi

if [ -d /tmp/seafile-sync-test/captures ]; then
    cp -r /tmp/seafile-sync-test/captures/* "$SCRIPT_DIR/captures/" 2>/dev/null || true
fi

# Display results
echo ""
echo "================================================================================================"
echo "RESULTS"
echo "================================================================================================"

if [ -d "$SCRIPT_DIR/test-results" ] && [ "$(ls -A $SCRIPT_DIR/test-results 2>/dev/null)" ]; then
    echo "Test Reports:"
    ls -lht "$SCRIPT_DIR/test-results/" | head -5
    echo ""
    echo "Latest report:"
    LATEST_REPORT=$(ls -t "$SCRIPT_DIR/test-results"/*.txt 2>/dev/null | head -1)
    if [ -n "$LATEST_REPORT" ]; then
        cat "$LATEST_REPORT"
    fi
fi

echo ""
if [ -d "$SCRIPT_DIR/captures" ] && [ "$(ls -A $SCRIPT_DIR/captures 2>/dev/null)" ]; then
    echo "Traffic Captures:"
    echo "  Remote server: $SCRIPT_DIR/captures/remote/"
    echo "  Local server: $SCRIPT_DIR/captures/local/"
    echo ""
    echo "Captured files:"
    find "$SCRIPT_DIR/captures" -type f -name "*.mitm" -o -name "*.har" | head -10
fi

echo ""
echo "================================================================================================"
if [ $EXIT_CODE -eq 0 ]; then
    echo "✓ ALL TESTS PASSED - Protocol behaviors match!"
else
    echo "✗ TESTS FAILED - Review reports for differences"
fi
echo "================================================================================================"

exit $EXIT_CODE
