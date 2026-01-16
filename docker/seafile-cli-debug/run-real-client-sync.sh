#!/bin/bash
# Run real desktop client sync test

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

echo "Running real desktop client sync test..."
echo "This will:"
echo "  1. Create encrypted libraries on both servers via API"
echo "  2. Upload files via API"
echo "  3. Sync both libraries using seaf-cli (real desktop client)"
echo "  4. Compare synced files"
echo ""

docker run --rm --network host \
  -v "$SCRIPT_DIR/scripts:/scripts:ro" \
  -v /tmp:/tmp \
  cool-storage-api-seafile-cli \
  bash -c "pip install -q requests urllib3 && python3 /scripts/test_real_client_sync.py"
