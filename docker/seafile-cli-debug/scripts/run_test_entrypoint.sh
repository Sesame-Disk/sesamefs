#!/bin/bash
# Entrypoint script for running comprehensive tests inside container

set -e

# Install dependencies
pip3 install -q requests urllib3 2>&1 | grep -v 'WARNING' || true

# Run the test with all passed arguments
# Using the simpler comprehensive_sync_test.py for now
exec python3 /scripts/comprehensive_sync_test.py "$@"
