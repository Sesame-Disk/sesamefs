#!/bin/bash
# Wrapper script to run comprehensive sync protocol tests
#
# Usage:
#   ./run-comprehensive-sync-test.sh --quick          # Quick test (small files only)
#   ./run-comprehensive-sync-test.sh --test-all       # Full test suite
#   ./run-comprehensive-sync-test.sh --list-scenarios # List available tests
#   ./run-comprehensive-sync-test.sh --test-scenario nested_folders  # Run specific test

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

echo "Running Comprehensive Seafile Sync Protocol Tests"
echo "=================================================="
echo ""

# Ensure local server is running
if ! curl -s http://localhost:8080/api2/ping > /dev/null 2>&1; then
    echo "⚠️  WARNING: Local server (localhost:8080) is not responding"
    echo "   Make sure SesameFS is running with: docker-compose up -d sesamefs"
    echo ""
    read -p "Continue anyway? (y/n) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

# Run tests in Docker container
docker run --rm --network host \
    -v "$SCRIPT_DIR/scripts:/scripts:ro" \
    -v /tmp:/tmp \
    -e SSL_CERT_FILE= \
    -e REQUESTS_CA_BUNDLE= \
    cool-storage-api-seafile-cli \
    bash -c "pip3 install -q requests urllib3 2>&1 && python3 /scripts/comprehensive_sync_test.py $@"

# Copy results to local directory
if [ -d /tmp/seafile-sync-test/results ]; then
    mkdir -p "$SCRIPT_DIR/test-results"
    cp /tmp/seafile-sync-test/results/* "$SCRIPT_DIR/test-results/" 2>/dev/null || true
    echo ""
    echo "Results copied to: $SCRIPT_DIR/test-results/"
    ls -lh "$SCRIPT_DIR/test-results/" | tail -5
fi
