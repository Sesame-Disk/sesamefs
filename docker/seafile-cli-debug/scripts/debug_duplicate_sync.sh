#!/bin/bash
# Debug script to verify what our server returns for duplicate files

set -e

# Use local server
SERVER_URL="http://host.docker.internal:8080"
# Dev credentials
EMAIL="test@example.com"
PASSWORD="anything"  # Dev mode accepts any password

echo "=== Testing Duplicate Files Sync Protocol on Local Server ==="
echo

# Step 1: Get auth token
echo "Step 1: Authenticating..."
TOKEN_RESPONSE=$(curl -s -X POST "${SERVER_URL}/api2/auth-token/" \
  -d "username=${EMAIL}" \
  -d "password=${PASSWORD}")
echo "Auth response: $TOKEN_RESPONSE"

TOKEN=$(echo "$TOKEN_RESPONSE" | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])" 2>/dev/null || echo "")
if [ -z "$TOKEN" ]; then
    echo "ERROR: Failed to get auth token"
    exit 1
fi
echo "✓ Got auth token: ${TOKEN:0:20}..."
echo

# Step 2: Create test library
echo "Step 2: Creating test library..."
REPO_RESPONSE=$(curl -s -X POST "${SERVER_URL}/api/v2.1/repos/" \
  -H "Authorization: Token ${TOKEN}" \
  -d "name=Debug Duplicate Files $(date +%s)")

echo "Library response: $REPO_RESPONSE"
REPO_ID=$(echo "$REPO_RESPONSE" | python3 -c "import sys,json; print(json.load(sys.stdin)['repo_id'])" 2>/dev/null || echo "")
if [ -z "$REPO_ID" ]; then
    echo "ERROR: Failed to create library"
    exit 1
fi
echo "✓ Created test library: ${REPO_ID}"
echo

# Step 3: Create test content
echo "Step 3: Creating test content..."
TEST_FILE="/tmp/test-duplicate-content-$$.txt"
echo "This is identical content for testing" > "$TEST_FILE"
CONTENT_SHA1=$(sha1sum "$TEST_FILE" | cut -d' ' -f1)
CONTENT_SIZE=$(wc -c < "$TEST_FILE")
echo "  Content SHA-1: ${CONTENT_SHA1}"
echo "  Content size: ${CONTENT_SIZE} bytes"
echo

# Step 4: Get upload link
echo "Step 4: Getting upload link..."
UPLOAD_URL=$(curl -s "${SERVER_URL}/api2/repos/${REPO_ID}/upload-link/?p=/" \
  -H "Authorization: Token ${TOKEN}" | python3 -c "import sys,json; print(json.load(sys.stdin).strip('\"'))")
echo "✓ Upload URL: ${UPLOAD_URL}"
echo

# Step 5: Upload first file
echo "Step 5: Uploading duplicate-file-1.txt..."
UPLOAD1=$(curl -s -X POST "${UPLOAD_URL}?ret-json=1" \
  -H "Authorization: Token ${TOKEN}" \
  -F "file=@${TEST_FILE}" \
  -F "filename=duplicate-file-1.txt" \
  -F "parent_dir=/" \
  -F "replace=0")
echo "Upload 1 response: $UPLOAD1"
FILE1_ID=$(echo "$UPLOAD1" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['id'])" 2>/dev/null || echo "")
echo "✓ File 1 fs_id: ${FILE1_ID}"
sleep 1

# Step 6: Upload second file (identical content, different name)
echo
echo "Step 6: Uploading duplicate-file-2.txt..."
UPLOAD2=$(curl -s -X POST "${UPLOAD_URL}?ret-json=1" \
  -H "Authorization: Token ${TOKEN}" \
  -F "file=@${TEST_FILE}" \
  -F "filename=duplicate-file-2.txt" \
  -F "parent_dir=/" \
  -F "replace=0")
echo "Upload 2 response: $UPLOAD2"
FILE2_ID=$(echo "$UPLOAD2" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['id'])" 2>/dev/null || echo "")
echo "✓ File 2 fs_id: ${FILE2_ID}"
echo

# Check if fs_ids match
if [ "$FILE1_ID" = "$FILE2_ID" ]; then
    echo "✓ Both files have SAME fs_id: $FILE1_ID"
    echo "  This is expected - same content → same fs_id"
else
    echo "✗ Files have DIFFERENT fs_ids!"
    echo "  File 1: $FILE1_ID"
    echo "  File 2: $FILE2_ID"
    echo "  This is WRONG!"
fi
echo

# Step 7: Check directory listing (API v2.1 - web interface)
echo "Step 7: Checking web API directory listing..."
DIR_LIST=$(curl -s "${SERVER_URL}/api/v2.1/repos/${REPO_ID}/dir/?p=/" \
  -H "Authorization: Token ${TOKEN}")
echo "Directory listing response:"
echo "$DIR_LIST" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    if 'dirent_list' in data:
        for f in data['dirent_list']:
            print(f\"  - {f['name']} (type: {f['type']}, size: {f.get('size', 'N/A')})\")
    else:
        print(data)
except:
    print('Failed to parse response')
"
echo

# Step 8: Get HEAD commit for sync protocol
echo "Step 8: Getting HEAD commit..."
HEAD_COMMIT=$(curl -s "${SERVER_URL}/seafhttp/repo/${REPO_ID}/commit/HEAD" \
  -H "Authorization: Token ${TOKEN}")
echo "HEAD commit response (first 200 chars):"
echo "$HEAD_COMMIT" | head -c 200
echo
COMMIT_ID=$(echo "$HEAD_COMMIT" | python3 -c "
import sys, json
data = json.loads(sys.stdin.read())
print(data['commit_id'])
")
ROOT_FS_ID=$(echo "$HEAD_COMMIT" | python3 -c "
import sys, json
data = json.loads(sys.stdin.read())
print(data['root_id'])
")
echo "  Commit ID: $COMMIT_ID"
echo "  Root fs_id: $ROOT_FS_ID"
echo

# Step 9: Get fs-id-list
echo "Step 9: Getting fs-id-list..."
FS_ID_LIST=$(curl -s "${SERVER_URL}/seafhttp/repo/${REPO_ID}/fs-id-list/?server-head=${COMMIT_ID}" \
  -H "Authorization: Token ${TOKEN}")
echo "fs-id-list response:"
echo "$FS_ID_LIST" | python3 -c "
import sys, json
try:
    ids = json.load(sys.stdin)
    print(f'  Total fs_ids: {len(ids)}')
    for i, fs_id in enumerate(ids):
        print(f'  [{i}] {fs_id}')
except Exception as e:
    print(f'Failed to parse: {e}')
    print(sys.stdin.read())
"
echo

# Count how many times the file fs_id appears
echo "Checking if file fs_id appears in fs-id-list..."
echo "$FS_ID_LIST" | python3 -c "
import sys, json
try:
    ids = json.load(sys.stdin)
    file_id = '$FILE1_ID'
    count = ids.count(file_id)
    print(f'  File fs_id {file_id} appears {count} time(s) in fs-id-list')
    if count == 0:
        print('  ✗ ERROR: File fs_id NOT in fs-id-list!')
    elif count == 1:
        print('  ✓ Appears once (deduplicated)')
    else:
        print('  ✗ Appears multiple times (should be deduplicated)')
except:
    pass
"
echo

# Step 10: Get pack-fs for root directory
echo "Step 10: Getting pack-fs for root directory..."
PACK_FS=$(curl -s -X POST "${SERVER_URL}/seafhttp/repo/${REPO_ID}/pack-fs" \
  -H "Authorization: Token ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d "[\"$ROOT_FS_ID\"]" \
  --output /tmp/pack-fs-$$.bin)

echo "  Saved to /tmp/pack-fs-$$.bin"
echo "  Size: $(wc -c < /tmp/pack-fs-$$.bin) bytes"
echo

# Parse pack-fs binary format
echo "Parsing pack-fs response..."
python3 <<'PYEOF'
import sys
import zlib
import json

filename = "/tmp/pack-fs-" + sys.argv[1] + ".bin"
with open(filename, 'rb') as f:
    data = f.read()

print(f"Total size: {len(data)} bytes")
print()

offset = 0
obj_num = 0
while offset < len(data):
    obj_num += 1
    # Read 40-byte hex fs_id
    if len(data) - offset < 44:
        print(f"Not enough data for object {obj_num}")
        break

    fs_id = data[offset:offset+40].decode('ascii')
    offset += 40

    # Read 4-byte size (big endian)
    size_bytes = data[offset:offset+4]
    size = int.from_bytes(size_bytes, 'big')
    offset += 4

    # Read compressed content
    compressed = data[offset:offset+size]
    offset += size

    # Decompress and parse JSON
    try:
        decompressed = zlib.decompress(compressed)
        obj = json.loads(decompressed)

        print(f"Object {obj_num}: fs_id={fs_id}")
        print(f"  Type: {obj.get('type')} (3=dir, 1=file)")

        if obj.get('type') == 3:
            dirents = obj.get('dirents', [])
            print(f"  Dirents count: {len(dirents)}")
            for dirent in dirents:
                print(f"    - name: {dirent.get('name')}, id: {dirent.get('id')}, mode: {dirent.get('mode')}")
        elif obj.get('type') == 1:
            print(f"  Size: {obj.get('size')}")
            print(f"  Block IDs: {obj.get('block_ids')}")
        print()
    except Exception as e:
        print(f"  Error parsing object: {e}")
        print()

PYEOF
python3 -c "pass" $$

# Cleanup
rm -f "$TEST_FILE"
rm -f /tmp/pack-fs-$$.bin

echo "=== Summary ==="
echo
echo "Repository ID: ${REPO_ID}"
echo "Both files uploaded: duplicate-file-1.txt, duplicate-file-2.txt"
echo "fs_ids: ${FILE1_ID} (file1), ${FILE2_ID} (file2)"
echo
echo "EXPECTED BEHAVIOR:"
echo "1. Web API should show BOTH files in directory listing ✓"
echo "2. fs-id-list should include file fs_id ONCE (deduplicated)"
echo "3. pack-fs root directory should have BOTH dirents entries"
echo "4. Desktop client should download BOTH files using the same fs_object"
echo
echo "Check the output above to verify if all steps are correct."
