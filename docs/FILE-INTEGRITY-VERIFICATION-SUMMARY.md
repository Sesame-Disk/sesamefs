# File Integrity Verification - Quick Reference

**Last Updated**: 2026-01-27
**For Full Details**: See [FILE-INTEGRITY-VERIFICATION.md](FILE-INTEGRITY-VERIFICATION.md)

---

## Quick Answers for Customers

### Q: How do I verify my uploaded files are not corrupted?

**Option 1: Fast Verification (Recommended)**
```python
# After upload, store the file ID
upload_response = upload_file('document.pdf')
file_id = upload_response['id']  # e.g., "2b95c8d2a2447e4216a443f7fa66897d863cea4d"

# Later: Verify file hasn't changed
file_detail = get_file_detail(repo_id, '/document.pdf')
if file_detail['id'] == file_id:
    print("✅ File is intact (content unchanged)")
else:
    print("❌ File has been modified or corrupted")
```

**Option 2: Cryptographic Verification (Download Required)**
```python
import hashlib

# Before upload: Calculate hash
with open('document.pdf', 'rb') as f:
    original_hash = hashlib.sha256(f.read()).hexdigest()

# After upload: Download and verify
downloaded = download_file(repo_id, '/document.pdf')
downloaded_hash = hashlib.sha256(downloaded).hexdigest()

if downloaded_hash == original_hash:
    print("✅ File verified cryptographically")
else:
    print("❌ File corrupted")
```

---

### Q: Does Seafile API provide MD5/SHA-1/SHA-256 checksums?

**Answer**: ❌ No, Seafile does not expose raw file content checksums.

**What's Available**:
- ✅ File `id` (content-addressed, deterministic)
- ✅ File `size` (bytes)
- ❌ No MD5, SHA-1, SHA-256 of file content

**Alternative**: Download the file and calculate the hash client-side.

---

### Q: What is the `id` field in upload responses?

**Answer**: The `id` is a **SHA-1 hash of the file object structure**, not the raw file content.

**File Object Structure**:
```json
{
  "version": 1,
  "type": 1,
  "block_ids": ["ab5339062c6c9c87b32482cccd4cbf3401cab062"],
  "size": 44
}
```

The `id` is calculated as: `SHA-1(JSON(file_object))`

**Key Properties**:
- ✅ **Deterministic**: Same content → Same ID
- ✅ **Content-addressed**: Different content → Different ID
- ✅ **Structural integrity**: Verifies all blocks are correctly assembled

---

### Q: Is the `id` reliable for integrity verification?

**Answer**: ✅ **Yes**, for most use cases.

**Test Results** (against Seafile v11.0.16):
- Same file uploaded twice → Same ID ✅
- Same content, different filename → Same ID ✅
- Different content → Different ID ✅
- Downloaded file matches original → SHA-256 verified ✅

**When to Use File ID**:
- Fast verification without downloading
- Detecting if file content has changed
- Verifying chunked uploads assembled correctly

**When to Use Download + Hash**:
- Need cryptographic verification (SHA-256/SHA-512)
- Compliance requirements (specific hash algorithms)
- Periodic deep audits

---

### Q: How do I verify large file uploads (1GB+)?

**Recommended Approach**:

1. **During Upload**: Store file ID
2. **Immediate Check**: Verify file size matches
3. **Routine Checks**: Use file ID (fast, no download)
4. **Periodic Audits**: Download + hash (e.g., monthly)

**Example**:
```python
# Upload large file
upload_response = upload_large_file('video.mp4')
expected_file_id = upload_response['id']
expected_size = upload_response['size']

# Store in database
db.store({
    'path': '/video.mp4',
    'file_id': expected_file_id,
    'size': expected_size,
    'uploaded_at': datetime.now()
})

# Daily: Quick check (no download)
file_detail = get_file_detail(repo_id, '/video.mp4')
assert file_detail['id'] == expected_file_id
assert file_detail['size'] == expected_size

# Monthly: Deep check (download + hash)
if should_do_monthly_audit():
    downloaded = download_file(repo_id, '/video.mp4')
    actual_hash = sha256(downloaded)
    assert actual_hash == expected_hash
```

---

## Implementation Examples

### Python Script

```python
#!/usr/bin/env python3
"""
Seafile File Integrity Verification Script
"""
import hashlib
import requests
import json

class SeafileIntegrityChecker:
    def __init__(self, server_url, auth_token):
        self.server_url = server_url
        self.headers = {'Authorization': f'Token {auth_token}'}

    def upload_with_verification(self, repo_id, file_path, parent_dir='/'):
        """Upload file and store integrity info"""
        # Calculate original hash
        with open(file_path, 'rb') as f:
            original_hash = hashlib.sha256(f.read()).hexdigest()

        # Get upload link
        upload_link = self._get_upload_link(repo_id)

        # Upload file
        with open(file_path, 'rb') as f:
            files = {'file': f}
            data = {
                'filename': os.path.basename(file_path),
                'parent_dir': parent_dir
            }
            response = requests.post(upload_link, files=files, data=data)
            response.raise_for_status()

        upload_info = response.json()[0]

        return {
            'file_id': upload_info['id'],
            'size': upload_info['size'],
            'name': upload_info['name'],
            'sha256': original_hash,
            'uploaded_at': datetime.now().isoformat()
        }

    def quick_verify(self, repo_id, file_path, expected_file_id):
        """Fast verification using file ID (no download)"""
        file_detail = self._get_file_detail(repo_id, file_path)
        return file_detail['id'] == expected_file_id

    def deep_verify(self, repo_id, file_path, expected_hash):
        """Full verification by downloading and hashing"""
        download_link = self._get_download_link(repo_id, file_path)
        response = requests.get(download_link)
        response.raise_for_status()

        actual_hash = hashlib.sha256(response.content).hexdigest()
        return actual_hash == expected_hash

    def _get_upload_link(self, repo_id):
        url = f'{self.server_url}/api2/repos/{repo_id}/upload-link/'
        response = requests.get(url, headers=self.headers)
        return response.json()

    def _get_file_detail(self, repo_id, file_path):
        url = f'{self.server_url}/api2/repos/{repo_id}/file/detail/'
        response = requests.get(url, headers=self.headers, params={'p': file_path})
        return response.json()

    def _get_download_link(self, repo_id, file_path):
        url = f'{self.server_url}/api2/repos/{repo_id}/file/'
        response = requests.get(url, headers=self.headers, params={'p': file_path})
        return response.json()

# Usage
checker = SeafileIntegrityChecker('https://app.nihaoconsult.com', 'your-token')

# Upload with verification
integrity_info = checker.upload_with_verification('repo-id', '/path/to/file.pdf')
print(f"Uploaded: {integrity_info['file_id']}")

# Quick check
is_intact = checker.quick_verify('repo-id', '/file.pdf', integrity_info['file_id'])
print(f"Quick check: {'✅ PASS' if is_intact else '❌ FAIL'}")

# Deep check
is_verified = checker.deep_verify('repo-id', '/file.pdf', integrity_info['sha256'])
print(f"Deep check: {'✅ PASS' if is_verified else '❌ FAIL'}")
```

---

### Bash Script

```bash
#!/bin/bash
# Seafile File Integrity Verification

SERVER="https://app.nihaoconsult.com"
TOKEN="your-auth-token"
REPO_ID="your-repo-id"
FILE_PATH="/test.pdf"

# Upload file and get file ID
echo "Uploading file..."
UPLOAD_LINK=$(curl -s -H "Authorization: Token $TOKEN" \
  "$SERVER/api2/repos/$REPO_ID/upload-link/")

UPLOAD_RESPONSE=$(curl -s -F file=@test.pdf \
  -F filename=test.pdf \
  -F parent_dir=/ \
  "$UPLOAD_LINK")

FILE_ID=$(echo "$UPLOAD_RESPONSE" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['id'])")
echo "File ID: $FILE_ID"

# Calculate original hash
ORIGINAL_HASH=$(sha256sum test.pdf | awk '{print $1}')
echo "Original SHA-256: $ORIGINAL_HASH"

# Quick verification: Check file ID
echo "Quick verification..."
CURRENT_FILE_ID=$(curl -s -H "Authorization: Token $TOKEN" \
  "$SERVER/api2/repos/$REPO_ID/file/detail/?p=$FILE_PATH" | \
  python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")

if [ "$FILE_ID" = "$CURRENT_FILE_ID" ]; then
  echo "✅ Quick check PASSED (file ID match)"
else
  echo "❌ Quick check FAILED (file ID mismatch)"
  exit 1
fi

# Deep verification: Download and hash
echo "Deep verification..."
DOWNLOAD_LINK=$(curl -s -H "Authorization: Token $TOKEN" \
  "$SERVER/api2/repos/$REPO_ID/file/?p=$FILE_PATH")

DOWNLOADED_HASH=$(curl -s "$DOWNLOAD_LINK" | sha256sum | awk '{print $1}')

if [ "$ORIGINAL_HASH" = "$DOWNLOADED_HASH" ]; then
  echo "✅ Deep check PASSED (SHA-256 match)"
else
  echo "❌ Deep check FAILED (SHA-256 mismatch)"
  exit 1
fi

echo "✅ All verification checks passed!"
```

---

## For SesameFS Developers

### Current Implementation

SesameFS correctly implements Seafile-compatible file IDs in `internal/api/v2/fs_helpers.go`:

```go
func (h *FSHelper) CreateFileFSObject(repoID, name string, size int64, blockIDs []string) (string, error) {
    fsContent := map[string]interface{}{
        "version":   1,
        "type":      1,
        "block_ids": blockIDs,
        "size":      size,
    }
    fsContentJSON, _ := json.Marshal(fsContent)
    hash := sha1.Sum(fsContentJSON)
    fsID := hex.EncodeToString(hash[:])
    return fsID, nil
}
```

### Recommended Enhancements

1. **Add content hash storage** (optional, non-breaking)
2. **Add custom checksum endpoint** (optional, non-breaking)
3. **Add verification to upload response** (optional, may break clients)

See [FILE-INTEGRITY-VERIFICATION.md](FILE-INTEGRITY-VERIFICATION.md) → "SesameFS Implementation" for details.

---

## References

- **Full Guide**: [FILE-INTEGRITY-VERIFICATION.md](FILE-INTEGRITY-VERIFICATION.md)
- **Seafile Data Model**: https://manual.seafile.com/11.0/develop/data_model/
- **Tested Against**: Seafile Server v11.0.16 (app.nihaoconsult.com)
- **Test Date**: 2026-01-27
