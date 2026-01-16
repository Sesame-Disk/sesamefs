# Getting Started with Protocol Comparison

This guide will walk you through running your first protocol comparison test.

## Prerequisites Check

Before starting, verify you have:

1. ✅ **Docker installed**
   ```bash
   docker --version
   ```

2. ✅ **Local SesameFS server running**
   ```bash
   # In the main project directory
   go run cmd/sesamefs/main.go

   # Verify it's accessible
   curl http://localhost:8080/api2/server-info/
   ```

3. ✅ **Test user created on local server**
   ```bash
   # You may need to create a test user first
   # Check your SesameFS documentation for user creation
   ```

## Quick Start (5 minutes)

### Step 1: Navigate to the testing directory

```bash
cd /Users/abel/Documents/Code-Experiments/cool-storage-api/docker/seafile-cli-debug
```

### Step 2: Run the comparison

```bash
./run-comparison.sh compare
```

This will:
1. Build the Docker container (first time only, ~2 minutes)
2. Test encrypted library operations on both servers
3. Generate a comparison report

### Step 3: Review the results

The script will output a preview of the comparison report. Look for:

```
✓ No differences found! Protocols match perfectly.
```

OR

```
✗ Found 5 differences
Review: captures/comparison_YYYYMMDD_HHMMSS/COMPARISON_REPORT.md
```

### Step 4: View detailed report

```bash
# Find the latest comparison session
ls -lt captures/

# View the report (replace with actual timestamp)
cat captures/comparison_20260116_123456/COMPARISON_REPORT.md
```

## What's Being Tested?

The comparison tests these encrypted library operations:

### 1. Protocol Version
- **Endpoint**: `GET /seafhttp/protocol-version`
- **Validates**: Server advertises correct protocol version (should be 2)

### 2. Create Encrypted Library
- **Endpoint**: `POST /api2/repos/` with `passwd` parameter
- **Validates**:
  - `encrypted` field (1 or true)
  - `enc_version` (should be 2)
  - `magic` (64 hex chars - PBKDF2 hash)
  - `random_key` (96 hex chars - encrypted file key)

### 3. Set/Verify Password
- **Endpoint**: `POST /api/v2.1/repos/{id}/set-password/`
- **Validates**: Password verification logic matches Seafile

### 4. Get Download Info (Sync Token)
- **Endpoint**: `GET /api2/repos/{id}/download-info/`
- **Validates**: Encryption metadata consistency

### 5. Get HEAD Commit
- **Endpoint**: `GET /seafhttp/repo/{id}/commit/HEAD`
- **Validates**:
  - `is_corrupted` field type (must be 0, not false)
  - `head_commit_id` field present

### 6. Get Full Commit
- **Endpoint**: `GET /seafhttp/repo/{id}/commit/{commit_id}`
- **Validates**:
  - `encrypted` field ("true" for encrypted libs)
  - `enc_version`, `magic`, `key` fields present

### 7. FS-ID-List
- **Endpoint**: `GET /seafhttp/repo/{id}/fs-id-list/?server-head={commit}`
- **Validates**: Returns JSON array of fs_ids

### 8. Pack-FS Binary Format
- **Endpoint**: `POST /seafhttp/repo/{id}/pack-fs/`
- **Validates**:
  - Binary format: `[40-byte ID][4-byte size BE][zlib data]`
  - Zlib compression works correctly
  - Decompressed JSON is valid

### 9. Check-FS
- **Endpoint**: `POST /seafhttp/repo/{id}/check-fs`
- **Validates**:
  - Accepts JSON array input
  - Returns JSON array of missing fs_ids
  - Returns `[]` for existing fs_ids

## Common Issues & Solutions

### Issue: "Local server is NOT running"

**Solution:**
```bash
# Terminal 1: Start local server
cd /Users/abel/Documents/Code-Experiments/cool-storage-api
go run cmd/sesamefs/main.go

# Terminal 2: Run comparison
cd docker/seafile-cli-debug
./run-comparison.sh compare
```

### Issue: "Failed to authenticate to local server"

**Solution:** Create a test user on your local server

```bash
# Use your local server's user creation endpoint
# Example (adjust to your API):
curl -X POST http://localhost:8080/api2/auth-token/ \
  -d "username=test@example.com" \
  -d "password=testpass"

# If this fails, you need to create the user first
# Check your SesameFS admin endpoints
```

### Issue: Differences found in encryption fields

**Common causes:**

1. **`enc_version` type mismatch** - Should be integer `2`, not string `"2"`
2. **`magic` computation wrong** - Must use `repo_id + password` for input
3. **`random_key` encryption wrong** - Must use `password` only (not `repo_id + password`)
4. **`is_corrupted` type** - Must be integer `0`, not boolean `false`

**Fix:**
1. Review `COMPARISON_REPORT.md` to see exact differences
2. Update your local implementation in `internal/api/v2/encryption.go` or `internal/api/sync.go`
3. Re-run comparison to verify fix

### Issue: pack-fs format errors

**Symptoms:**
- "decompression_failed" in report
- "Format mismatch" error

**Solution:**
1. Check binary format is correct:
   ```
   [40-byte hex fs_id][4-byte big-endian size][zlib-compressed JSON]
   ```

2. Verify zlib compression level (default level 6 works)

3. Ensure JSON is valid before compression

4. Test manually:
   ```bash
   ./run-comparison.sh shell

   # Inside container
   seaf-debug.sh capture-pack-fs
   xxd /captures/pack-fs-response.bin | head -20
   ```

## Understanding the Output Structure

### Directory Layout

```
captures/
└── comparison_20260116_123456/          # Session directory
    ├── COMPARISON_REPORT.md             # Human-readable diff report
    ├── diffs.json                        # Machine-readable diffs
    ├── remote_create_enc_lib_*/         # Remote server captures
    │   ├── 0001_POST_api2_auth-token.json
    │   ├── 0002_POST_api2_repos.json
    │   └── session_summary.json
    ├── local_create_enc_lib_*/          # Local server captures
    │   ├── 0001_POST_api2_auth-token.json
    │   ├── 0002_POST_api2_repos.json
    │   └── session_summary.json
    └── ... (more test captures)
```

### Report Format

```markdown
# Seafile Protocol Comparison Report

**Total Issues Found:** 3

## Create Encrypted Library

### POST /api2/repos/ (encrypted)

**Differences:**
- enc_version: Type mismatch - remote=int, local=str
- magic: Value mismatch - remote=64 hex chars, local=empty

**Remote Response:**
{ ... }

**Local Response:**
{ ... }
```

## Next Steps

1. **If no differences found:**
   - ✅ Your local implementation matches Seafile!
   - Test with real Seafile desktop client
   - Try syncing files to encrypted library

2. **If differences found:**
   - 📝 Review `COMPARISON_REPORT.md` carefully
   - 🔧 Fix issues in local implementation
   - ▶️ Re-run comparison: `./run-comparison.sh compare`
   - 🔁 Repeat until no differences

3. **Test with real client:**
   ```bash
   # On your Mac with Seafile client installed
   # Add library using your local server URL
   # Try to sync encrypted library
   # Check logs: ~/.ccnet/logs/seafile.log
   ```

4. **Update protocol documentation:**
   - Add findings to `docs/SEAFILE-SYNC-PROTOCOL.md`
   - Document encryption specifics in `docs/ENCRYPTION.md`

## Advanced Usage

### Test only specific operations

Edit `compare_protocol.py` and comment out tests you don't need:

```python
def run_all_tests(self):
    # Comment out tests you want to skip
    # self.test_protocol_version()

    self.test_create_encrypted_library()
    # self.test_set_password(...)
    # ...
```

### Add custom tests

```python
def test_my_custom_endpoint(self):
    """Test: My custom endpoint"""
    log_section("TEST: My Custom Endpoint")

    # Your test logic here
    pass

# Add to run_all_tests()
def run_all_tests(self):
    # ... existing tests ...
    self.test_my_custom_endpoint()
```

### Compare against different server

```bash
REMOTE_SERVER="https://other-seafile.com" \
REMOTE_USER="user@example.com" \
REMOTE_PASS="password" \
./run-comparison.sh compare
```

## Troubleshooting Tips

### Enable debug logging

```bash
./run-comparison.sh shell

# Inside container
export PYTHONUNBUFFERED=1
python3 -u /usr/local/bin/compare_protocol.py --test all 2>&1 | tee debug.log
```

### Inspect raw HTTP traffic

```bash
# After running comparison
cd captures/comparison_20260116_123456

# View all requests to remote server
cat remote_*/session_summary.json | jq .

# View specific request
cat remote_create_enc_lib_*/0002_POST_api2_repos.json | jq .
```

### Compare binary responses

```bash
./run-comparison.sh shell

# Inside container
# Get pack-fs from both servers and compare
seaf-debug.sh capture-pack-fs

# Compare hex dumps
xxd /captures/pack-fs-response.bin > /tmp/remote.hex
# ... capture from local server ...
diff /tmp/remote.hex /tmp/local.hex
```

## Getting Help

If you're stuck:

1. **Check captures:** Raw HTTP data in `captures/comparison_*/`
2. **Check container logs:** `docker logs seafile-protocol-compare`
3. **Run interactive shell:** `./run-comparison.sh shell`
4. **Review protocol docs:** `../../docs/SEAFILE-SYNC-PROTOCOL.md`

## Success Criteria

Your implementation is ready when:

- ✅ `./run-comparison.sh compare` shows "No differences found"
- ✅ Seafile desktop client can sync encrypted libraries
- ✅ Files upload/download correctly
- ✅ Password change works
- ✅ All encryption fields match Seafile format
