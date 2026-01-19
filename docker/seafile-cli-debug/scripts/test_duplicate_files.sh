#!/bin/bash
# Test how stock Seafile handles identical files with different names

set -e

SERVER_URL="https://app.nihaoconsult.com"
EMAIL="abel.aguzmans@gmail.com"
PASSWORD="Qwerty123!"

echo "=== Testing Duplicate Files on Stock Seafile ==="
echo

# Step 1: Get auth token
echo "Step 1: Authenticating..."
TOKEN=$(curl -s -X POST "${SERVER_URL}/api2/auth-token/" \
  -d "username=${EMAIL}" \
  -d "password=${PASSWORD}" | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")

echo "✓ Got auth token: ${TOKEN:0:20}..."
echo

# Step 2: Create test library
echo "Step 2: Creating test library..."
REPO_RESPONSE=$(curl -s -X POST "${SERVER_URL}/api2/repos/" \
  -H "Authorization: Token ${TOKEN}" \
  -d "name=Duplicate Files Test $(date +%s)" \
  -d "desc=Testing identical files with different names")

REPO_ID=$(echo "$REPO_RESPONSE" | python3 -c "import sys,json; print(json.load(sys.stdin)['repo_id'])")
echo "✓ Created test library: ${REPO_ID}"
echo

# Step 3: Create test file content
echo "Step 3: Creating test file content..."
TEST_FILE="/tmp/test-content-$$.txt"
python3 -c "print('This is test content. ' * 1000)" > "$TEST_FILE"
FILE_SIZE=$(wc -c < "$TEST_FILE")
FILE_HASH=$(sha1sum "$TEST_FILE" | cut -d' ' -f1)
echo "  Content SHA1: ${FILE_HASH}"
echo "  Content size: ${FILE_SIZE} bytes"
echo

# Step 4: Get upload link
echo "Step 4: Getting upload link..."
UPLOAD_URL=$(curl -s "${SERVER_URL}/api2/repos/${REPO_ID}/upload-link/" \
  -H "Authorization: Token ${TOKEN}" | python3 -c "import sys,json; print(json.load(sys.stdin).strip('\"'))")
echo "✓ Got upload link: ${UPLOAD_URL}"
echo

# Step 5: Upload first file
echo "Step 5: Uploading test-file-original.txt..."
curl -s -X POST "${UPLOAD_URL}?ret-json=1" \
  -H "Authorization: Token ${TOKEN}" \
  -F "file=@${TEST_FILE}" \
  -F "filename=test-file-original.txt" \
  -F "parent_dir=/" \
  -F "replace=0" > /dev/null
echo "✓ Uploaded: test-file-original.txt"
sleep 1

# Step 6: Upload second file (identical content, different name)
echo "Uploading test-file-copy.txt..."
curl -s -X POST "${UPLOAD_URL}?ret-json=1" \
  -H "Authorization: Token ${TOKEN}" \
  -F "file=@${TEST_FILE}" \
  -F "filename=test-file-copy.txt" \
  -F "parent_dir=/" \
  -F "replace=0" > /dev/null
echo "✓ Uploaded: test-file-copy.txt"
echo

# Step 7: List files to verify
echo "Step 6: Verifying files on server..."
FILES_JSON=$(curl -s "${SERVER_URL}/api2/repos/${REPO_ID}/dir/?p=/" \
  -H "Authorization: Token ${TOKEN}")

echo "Files on server:"
echo "$FILES_JSON" | python3 -c "
import sys, json
files = json.load(sys.stdin)
for f in files:
    print(f\"  - {f['name']} ({f['size']} bytes, id: {f['id']})\")

# Check if IDs match
if len(files) >= 2:
    file1_id = files[0]['id']
    file2_id = files[1]['id']
    print(f\"\nFile IDs match: {file1_id == file2_id}\")
    if file1_id == file2_id:
        print(\"✓ Stock Seafile deduplicates content (same block ID)\")
"
echo

# Clean up
rm -f "$TEST_FILE"

echo "=== Manual Desktop Client Test ==="
echo
echo "Repository ID: ${REPO_ID}"
echo "Server: ${SERVER_URL}"
echo
echo "TO TEST:"
echo "1. Open your Seafile desktop client"
echo "2. Add this library (use the repo ID above)"
echo "3. Wait for sync to complete"
echo "4. Check if BOTH files are present in the synced folder:"
echo "   - test-file-original.txt"
echo "   - test-file-copy.txt"
echo
echo "If both files are downloaded despite having identical content,"
echo "then our server has a bug and needs fixing."
echo

# Save repo ID for later cleanup
echo "${REPO_ID}" > /tmp/test-repo-id.txt
echo "Repo ID saved to /tmp/test-repo-id.txt for later cleanup"
