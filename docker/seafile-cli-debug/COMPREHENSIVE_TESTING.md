# Comprehensive Sync Protocol Testing Framework

This framework tests the complete Seafile sync protocol by creating files on-the-fly, syncing them with the real desktop client, and comparing behavior between stock Seafile and SesameFS.

## Features

✅ **Automated File Creation** - Creates test files via API on both servers
✅ **Real Desktop Client** - Uses seaf-cli (official Seafile client) for sync
✅ **Traffic Capture** - mitmproxy captures ALL HTTP requests/responses
✅ **Protocol Comparison** - Compares field types, response formats, etc.
✅ **Multiple Scenarios** - Tests various file sizes, folder structures, etc.
✅ **Detailed Reports** - Generates comprehensive reports with findings

## Quick Start

### 1. Ensure Local Server is Running

```bash
cd /Users/abel/Documents/Code-Experiments/cool-storage-api
docker-compose up -d sesamefs

# Verify
curl http://localhost:8080/api2/ping
```

### 2. Run Quick Test (Small Files Only)

```bash
cd docker/seafile-cli-debug
./run-comprehensive-with-proxy.sh --quick
```

This runs ~3-4 scenarios with small files in 2-3 minutes.

### 3. Run Full Test Suite

```bash
./run-comprehensive-with-proxy.sh --test-all
```

This runs ALL 7 scenarios including large files (50MB+). Takes 10-15 minutes.

### 4. Run Specific Scenario

```bash
# List available scenarios
./run-comprehensive-with-proxy.sh --list-scenarios

# Run specific one
./run-comprehensive-with-proxy.sh --test-scenario nested_folders
```

## Test Scenarios

| Scenario | Files | Size | Description |
|----------|-------|------|-------------|
| `single_small_file` | 1 | <1 MB | Single text file in root |
| `multiple_small_files` | 10 | ~1 MB | Multiple small files in root |
| `nested_folders` | 5 | <1 MB | Files in nested folder structure |
| `medium_files` | 3 | 9 MB | Medium files (1-5 MB each) |
| `large_file` | 1 | 50 MB | Single large file |
| `many_tiny_files` | 50 | <1 MB | Many tiny files (1KB each) |
| `mixed_content` | 8 | 2 MB | Mix of sizes and folder structures |

## What Gets Tested

### For Each Scenario:

**On Remote Server (Stock Seafile):**
1. Create library via API
2. Upload test files via API (including nested folders)
3. Sync library with seaf-cli desktop client
4. Verify all files synced correctly
5. Verify file content (SHA-256 hash comparison)
6. Capture all HTTP traffic

**On Local Server (SesameFS):**
1-6. Same steps as remote

**Comparison:**
- Sync success/failure
- Files synced vs. expected
- File content verification
- Protocol traffic differences
- Field types, response formats

## Output Files

### Test Results

```
docker/seafile-cli-debug/test-results/
└── test_report_YYYYMMDD_HHMMSS.txt
```

Contains:
- Summary (total scenarios, matching, differing)
- Per-scenario results (files synced, verified, errors)
- Comparison details

### Traffic Captures

```
docker/seafile-cli-debug/captures/
├── remote/
│   ├── nested_folders_remote_traffic.mitm
│   ├── nested_folders_remote_traffic.har
│   └── ...
└── local/
    ├── nested_folders_local_traffic.mitm
    ├── nested_folders_local_traffic.har
    └── ...
```

- `.mitm` files: mitmproxy format (can be viewed with mitmweb)
- `.har` files: HAR format (can be viewed in browser DevTools or HAR viewers)

## Analyzing Traffic Captures

### View with mitmweb (Recommended)

```bash
# Install mitmproxy if not already
pip install mitmproxy

# View captures interactively
mitmweb -r docker/seafile-cli-debug/captures/remote/nested_folders_remote_traffic.mitm
```

Opens browser at http://127.0.0.1:8081 with interactive HTTP traffic viewer.

### View HAR Files

1. Open browser DevTools (F12)
2. Go to Network tab
3. Right-click → "Import HAR file..."
4. Select `.har` file from captures/

### Extract Specific Requests

```bash
cd docker/seafile-cli-debug/captures

# List all requests
mitmdump -nr remote/nested_folders_remote_traffic.mitm

# Filter by endpoint
mitmdump -nr remote/nested_folders_remote_traffic.mitm | grep "/api2/repos"

# Export specific request to JSON
mitmdump -nr remote/nested_folders_remote_traffic.mitm --set flow_detail=3 | jq '.'
```

## Common Use Cases

### Test a Specific Bug Fix

```bash
# Before fix
./run-comprehensive-with-proxy.sh --test-scenario nested_folders

# Make your code changes in SesameFS
# Restart server: docker-compose restart sesamefs

# After fix
./run-comprehensive-with-proxy.sh --test-scenario nested_folders

# Compare results
```

### Find Field Type Mismatches

```bash
# Run test
./run-comprehensive-with-proxy.sh --test-scenario single_small_file

# View captured traffic
mitmweb -r captures/remote/single_small_file_remote_traffic.mitm &
mitmweb -r captures/local/single_small_file_local_traffic.mitm &

# Compare same endpoint in both
# Look for differences in:
# - Field types (int vs string, etc.)
# - Missing/extra fields
# - Response format
```

### Test Large File Handling

```bash
# Test 50MB file
./run-comprehensive-with-proxy.sh --test-scenario large_file

# Check if:
# - Chunking works correctly
# - Progress tracking works
# - Blocks are verified
# - Resume works (if interrupted)
```

### Test Many Files Performance

```bash
# Test 50 tiny files
./run-comprehensive-with-proxy.sh --test-scenario many_tiny_files

# Measure:
# - How long does sync take?
# - Are there any rate limits hit?
# - Does check-fs handle many IDs correctly?
```

## Understanding Results

### Success Criteria

```
✓ ALL TESTS PASSED - Protocol behaviors match!
```

This means:
- All files synced on both servers
- All file content verified (SHA-256 matches)
- Sync success/failure same on both servers
- No client errors in logs

### When Tests Fail

```
✗ TESTS FAILED - Review reports for differences
```

Check test report for:

```
SCENARIO: nested_folders
====================

REMOTE:
  Success: True
  Files: 5/5

LOCAL:
  Success: False
  Files: 3/5

✗ DIFFERENCES:
  - Sync success: remote=True, local=False
  - Files verified: remote=5, local=3
  - Missing files differ: remote=set(), local={'folder1/subfolder/deep.txt', 'folder2/another.txt'}
```

**Diagnosis**:
1. Local server failed to sync 2 files
2. Check client logs: `cat /tmp/seafile-sync-test/local-config/logs/seafile.log`
3. Look for errors related to those file paths
4. Review captured traffic for those upload/download requests

## Advanced Usage

### Modify Test Scenarios

Edit `scripts/comprehensive_sync_test.py`:

```python
def create_test_scenarios() -> List[TestScenario]:
    scenarios = []

    # Add your custom scenario
    scenarios.append(TestScenario(
        name="my_custom_test",
        description="Test specific edge case",
        files=[
            TestFile("special/file.dat", b"specific content"),
            # ... more files
        ]
    ))

    return scenarios
```

Then run:
```bash
./run-comprehensive-with-proxy.sh --test-scenario my_custom_test
```

### Debug Failed Sync

```bash
# Run test
./run-comprehensive-with-proxy.sh --test-scenario nested_folders

# If fails, check logs
tail -100 /tmp/seafile-sync-test/local-config/logs/seafile.log

# Look for:
# - "Failed to find dir"
# - "Failed to decompress"
# - "Error when indexing"
# - HTTP errors (404, 500, etc.)
```

### Compare Protocol Responses

```bash
# Run test
./run-comprehensive-with-proxy.sh --test-scenario single_small_file

# Extract specific endpoint from both
cd docker/seafile-cli-debug/captures

# Get download-info from remote
mitmdump -nr remote/single_small_file_remote_traffic.mitm | \
    grep "download-info" | \
    jq '.'

# Get download-info from local
mitmdump -nr local/single_small_file_local_traffic.mitm | \
    grep "download-info" | \
    jq '.'

# Compare field by field
```

## Troubleshooting

### Issue: "Local server not responding"

**Solution:**
```bash
cd /Users/abel/Documents/Code-Experiments/cool-storage-api
docker-compose up -d sesamefs
docker-compose logs -f sesamefs
```

Wait for "SesameFS dev starting on port :8080"

### Issue: "Authentication failed"

**Solution:** Check credentials in `.seafile-reference.md` are correct and user exists on both servers.

### Issue: mitmproxy not capturing traffic

**Solution:**
1. Check mitmproxy is installed in container: `docker run ... mitmdump --version`
2. Check proxy port is not already in use: `lsof -i :8888`
3. Review container logs for mitmproxy errors

### Issue: Files not syncing

**Symptoms:** Sync shows success but files missing

**Solution:**
1. Check client logs: `/tmp/seafile-sync-test/{remote|local}-config/logs/seafile.log`
2. Look for specific error messages
3. Run same scenario against stock Seafile only to verify client works:
   ```bash
   # Temporarily modify LOCAL_URL in script to point to stock Seafile
   # Or comment out local server test
   ```

## Integration with CI/CD

This framework can be integrated into CI/CD pipelines:

```yaml
# .github/workflows/sync-protocol-test.yml
name: Sync Protocol Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2

      - name: Start SesameFS
        run: docker-compose up -d sesamefs

      - name: Run sync protocol tests
        run: |
          cd docker/seafile-cli-debug
          ./run-comprehensive-with-proxy.sh --quick

      - name: Upload results
        uses: actions/upload-artifact@v2
        with:
          name: sync-test-results
          path: docker/seafile-cli-debug/test-results/
```

## Performance Benchmarking

The framework can also be used for performance testing:

```bash
# Run full suite and time it
time ./run-comprehensive-with-proxy.sh --test-all

# Check sync duration in report
grep "Sync duration" docker/seafile-cli-debug/test-results/test_report_*.txt
```

Example output:
```
Remote: Sync duration: 2.34s
Local:  Sync duration: 2.41s
```

## Next Steps

After tests pass:
1. ✅ Protocol compatibility verified
2. Test with real Seafile desktop client on macOS/Windows/Linux
3. Test encrypted libraries
4. Test version history
5. Test sharing features
6. Performance optimization if needed

## Related Documentation

- **Protocol Spec**: `../../docs/SEAFILE-SYNC-PROTOCOL-RFC.md`
- **Encryption**: `../../docs/ENCRYPTION.md`
- **Bug Reports**: `../../docs/SYNC_BUG_MULTIFILE_20260116.md`
- **Getting Started**: `./GETTING_STARTED.md`
