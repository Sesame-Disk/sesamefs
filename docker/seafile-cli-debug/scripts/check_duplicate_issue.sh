#!/bin/bash
# Simple script to check what our server returns for sync protocol

# Use the existing library that has the problem
# User should provide REPO_ID and TOKEN

if [ -z "$1" ] || [ -z "$2" ]; then
    echo "Usage: $0 <REPO_ID> <AUTH_TOKEN>"
    echo
    echo "Example:"
    echo "  $0 'abc123...' 'your-auth-token'"
    exit 1
fi

REPO_ID="$1"
TOKEN="$2"
SERVER_URL="http://localhost:8080"

echo "=== Checking Sync Protocol for Duplicate Files Issue ==="
echo "Repository: $REPO_ID"
echo

# Get HEAD commit
echo "[1] Getting HEAD commit..."
HEAD_RESPONSE=$(curl -s "${SERVER_URL}/seafhttp/repo/${REPO_ID}/commit/HEAD" \
  -H "Authorization: Token ${TOKEN}")

echo "$HEAD_RESPONSE" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    print(f\"  Commit ID: {data['commit_id']}\")
    print(f\"  Root fs_id: {data['root_id']}\")
    print(f\"  Description: {data.get('desc', 'N/A')}\")
except Exception as e:
    print(f'Error: {e}')
    print(sys.stdin.read())
" || exit 1

ROOT_FS_ID=$(echo "$HEAD_RESPONSE" | python3 -c "import sys,json; print(json.load(sys.stdin)['root_id'])")
COMMIT_ID=$(echo "$HEAD_RESPONSE" | python3 -c "import sys,json; print(json.load(sys.stdin)['commit_id'])")
echo

# Get fs-id-list
echo "[2] Getting fs-id-list..."
FS_LIST=$(curl -s "${SERVER_URL}/seafhttp/repo/${REPO_ID}/fs-id-list/?server-head=${COMMIT_ID}" \
  -H "Authorization: Token ${TOKEN}")

echo "$FS_LIST" | python3 -c "
import sys, json
try:
    ids = json.load(sys.stdin)
    print(f'  Total unique fs_ids: {len(ids)}')
    print(f'  First 5 fs_ids:')
    for i, fs_id in enumerate(ids[:5]):
        print(f'    [{i}] {fs_id}')
    if len(ids) > 5:
        print(f'    ... ({len(ids) - 5} more)')
except Exception as e:
    print(f'Error parsing fs-id-list: {e}')
"
echo

# Get pack-fs for root directory to see dirents
echo "[3] Getting pack-fs for root directory..."
echo "  Requesting fs_id: $ROOT_FS_ID"

curl -s -X POST "${SERVER_URL}/seafhttp/repo/${REPO_ID}/pack-fs" \
  -H "Authorization: Token ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d "[\"$ROOT_FS_ID\"]" \
  --output /tmp/pack-fs-root.bin

echo "  Saved to /tmp/pack-fs-root.bin ($(wc -c < /tmp/pack-fs-root.bin) bytes)"
echo

echo "[4] Parsing root directory object..."
python3 <<'PYEOF'
import zlib
import json

with open('/tmp/pack-fs-root.bin', 'rb') as f:
    data = f.read()

if len(data) < 44:
    print("  Error: Response too short")
    exit(1)

# Read fs_id (40 bytes)
fs_id = data[0:40].decode('ascii')
print(f"  fs_id: {fs_id}")

# Read size (4 bytes BE)
size = int.from_bytes(data[40:44], 'big')
print(f"  Compressed size: {size} bytes")

# Read and decompress content
compressed = data[44:44+size]
try:
    decompressed = zlib.decompress(compressed)
    obj = json.loads(decompressed)

    print(f"  Type: {obj.get('type')} (3=dir, 1=file)")

    if obj.get('type') == 3:
        dirents = obj.get('dirents', [])
        print(f"  Total dirents: {len(dirents)}")
        print()
        print("  Files/folders in root:")

        # Track fs_ids to detect duplicates
        fs_id_counts = {}
        for dirent in dirents:
            dirent_id = dirent.get('id', 'N/A')
            name = dirent.get('name', 'N/A')
            mode = dirent.get('mode', 0)
            size = dirent.get('size', 0)
            is_dir = (mode & 0o40000) != 0
            type_str = 'DIR' if is_dir else 'FILE'

            print(f"    [{type_str}] {name}")
            print(f"        fs_id: {dirent_id}")
            print(f"        size: {size} bytes")
            print(f"        mode: {oct(mode)}")

            # Count fs_id occurrences
            if dirent_id in fs_id_counts:
                fs_id_counts[dirent_id].append(name)
            else:
                fs_id_counts[dirent_id] = [name]

        print()
        print("  Duplicate content detection:")
        has_duplicates = False
        for fs_id, names in fs_id_counts.items():
            if len(names) > 1:
                print(f"    fs_id {fs_id} is used by {len(names)} files:")
                for name in names:
                    print(f"      - {name}")
                has_duplicates = True

        if not has_duplicates:
            print("    No duplicate content detected (each file has unique fs_id)")
    else:
        print(f"  Unexpected type: {obj.get('type')}")
        print(f"  Object: {obj}")

except Exception as e:
    print(f"  Error decompressing/parsing: {e}")
    import traceback
    traceback.print_exc()

PYEOF

rm -f /tmp/pack-fs-root.bin

echo
echo "=== Analysis ==="
echo
echo "If the root directory's dirents array shows:"
echo "  - BOTH files with DIFFERENT names → Web interface will show both ✓"
echo "  - But SAME fs_id → They share content (deduplication)"
echo
echo "The desktop client SHOULD download both files even with same fs_id."
echo "If it doesn't, the bug is in our pack-fs or fs-id-list response format."
