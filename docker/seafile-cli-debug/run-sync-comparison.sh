#!/bin/bash
# Run complete encrypted library sync comparison test

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

echo "Running encrypted library sync comparison test..."
echo "This will:"
echo "  1. Create fresh encrypted libraries on both servers"
echo "  2. Upload files to both"
echo "  3. Compare all sync protocol responses"
echo ""

docker run --rm --network host \
  -v "$SCRIPT_DIR/scripts:/scripts:ro" \
  python:3.11-slim \
  bash -c "pip install -q requests && python3 /scripts/test_encrypted_sync_full.py"
