# Testing Guide: Comprehensive Sync Protocol Framework

**Last Updated**: 2026-01-17

## Quick Start

### Prerequisites

1. **Ensure SesameFS is running:**
```bash
cd /Users/abel/Documents/Code-Experiments/cool-storage-api
docker-compose up -d sesamefs

# Verify it's responding
curl http://localhost:8080/api2/ping
```

2. **Navigate to test directory:**
```bash
cd docker/seafile-cli-debug
```

### Run Tests

**Quick Test (Recommended for development):**
```bash
./run-comprehensive-with-proxy.sh --quick
```
- Tests 4 scenarios with small files
- Runs in ~3 minutes
- Good for verifying basic sync functionality

**Full Test Suite:**
```bash
./run-comprehensive-with-proxy.sh --test-all
```
- Tests all 7 scenarios including large files (50MB)
- Runs in ~15 minutes
- Comprehensive protocol verification

**Single Scenario Test:**
```bash
# List available scenarios
./run-comprehensive-with-proxy.sh --list-scenarios

# Test specific scenario
./run-comprehensive-with-proxy.sh --test-scenario nested_folders
./run-comprehensive-with-proxy.sh --test-scenario medium_files
```

---

## Understanding Test Results

### Success Output

```
================================================================================
SCENARIO: nested_folders
================================================================================
Description: Files in nested folder structure
Files: 5
Total size: 0.00 MB

Testing on LOCAL server (SesameFS)...
  Creating library 'test_nested_folders_local_20260117_045958'...
  Library ID: 1687f001-6fe4-4a1e-9cd2-625e18508eb6
  Uploading 5 files...
    Creating 3 directories...
      ✓ Created: /folder1
      ✓ Created: /folder1/subfolder
      ✓ Created: /folder2
    Upload link: http://localhost:8080/seafhttp/upload-api/...
  Syncing with desktop client...
    ✓ Config verified: /tmp/seafile-sync-test/local-config/seafile.ini
    Running seaf-cli download...
  Waiting for sync to complete...
    ✓ Sync completed successfully
  ✓ Sync completed in 2.34s
  Verifying synced files...
    Sync directory contains: ['test_nested_folders_local_20260117_045958']
    ✓ Library path exists: /tmp/seafile-sync-test/local-sync/test_nested_folders_local_20260117_045958

  Results:
    Files expected: 5
    Files synced: 5
    Files verified: 5
    Success rate: 100.0%

================================================================================================
✓ ALL TESTS PASSED - Protocol behaviors match!
================================================================================================
```

**What this means:**
- ✅ All files uploaded successfully
- ✅ Desktop client synced successfully
- ✅ All file contents verified (SHA-256 hash match)
- ✅ SesameFS protocol is compatible with Seafile

### Failure Output

```
  Results:
    Files expected: 5
    Files synced: 3
    Files verified: 3
    Success rate: 60.0%
    Missing: folder1/file1.txt, folder2/another.txt
    Client errors: 2
      - [01/17/26 03:59:48] http-tx-mgr.c(4149): Bad response code for POST http://localhost:8080/seafhttp/repo/329d86e9/pack-fs/: 404.
      - [01/17/26 03:59:48] http-tx-mgr.c(4599): Failed to get fs objects for repo 329d86e9

================================================================================================
✗ TESTS FAILED - Review reports for differences
================================================================================================
```

**What to do:**
1. Check client errors for specific endpoint failures
2. Review server logs: `docker-compose logs sesamefs | grep ERROR`
3. Check test report: `cat test-results/test_report_*.txt`
4. Review captured traffic (if mitmproxy enabled)

---

## Test Scenarios

| Scenario | Files | Size | Description | Tests |
|----------|-------|------|-------------|-------|
| `single_small_file` | 1 | <1 KB | Single text file in root | Basic sync |
| `multiple_small_files` | 10 | ~10 KB | Multiple files in root | Multi-file handling |
| `nested_folders` | 5 | <1 KB | Files in nested folders | Directory structure sync |
| `medium_files` | 3 | 9 MB | Files 1-5 MB each | Chunking, block storage |
| `large_file` | 1 | 50 MB | Single large file | Large file handling, progress |
| `many_tiny_files` | 50 | 50 KB | Many 1KB files | Performance, many files |
| `mixed_content` | 8 | 2 MB | Mix of sizes/folders | Real-world scenarios |

---

## Debugging Failed Tests

### Check Server Logs

```bash
# View live logs
docker-compose logs -f sesamefs

# Search for errors
docker-compose logs sesamefs | grep -i "error\|failed\|404"

# Check specific endpoint
docker-compose logs sesamefs | grep "pack-fs"
```

### Check Client Logs

```bash
# View client log (after test completes)
docker run --rm --network host -v /tmp:/tmp cool-storage-api-seafile-cli \
  cat /tmp/seafile-sync-test/local-config/logs/seafile.log | tail -100
```

### Common Issues

**Issue: "Failed to create directory"**
- **Cause**: Directory creation API not working
- **Check**: `docker-compose logs sesamefs | grep "POST.*dir"`
- **Fix**: Verify `POST /api2/repos/{id}/dir/?p={path}&operation=mkdir` endpoint

**Issue: "pack-fs: 404"**
- **Cause**: FS objects not being stored or retrieved correctly
- **Check**: Server logs for "pack-fs" errors
- **Fix**: Verify `POST /seafhttp/repo/{id}/pack-fs` endpoint and FS ID mapping

**Issue: "Failed to get HEAD commit"**
- **Cause**: Library not properly initialized
- **Check**: `docker-compose logs sesamefs | grep commit`
- **Fix**: Verify library creation creates initial commit

**Issue: "Files synced but verification fails"**
- **Cause**: File content corrupted during upload/download
- **Check**: Compare SHA-256 hashes in test output
- **Fix**: Check block storage and encryption/decryption logic

---

## Advanced Usage

### Capture HTTP Traffic

The comprehensive test framework uses mitmproxy to capture all HTTP traffic between the desktop client and servers. Captures are saved in `captures/` directory.

**View captured traffic:**
```bash
# Install mitmproxy (if not installed)
pip install mitmproxy

# View captures in browser
cd captures/local
mitmweb -r nested_folders_local_traffic.mitm
# Opens http://127.0.0.1:8081
```

### Compare Protocol Responses

```bash
# Run test that captures traffic
./run-comprehensive-with-proxy.sh --test-scenario single_small_file

# Extract specific requests from captures
cd captures
mitmdump -nr local/single_small_file_local_traffic.mitm | grep "pack-fs"
mitmdump -nr remote/single_small_file_remote_traffic.mitm | grep "pack-fs"
```

### Custom Test Scenarios

Edit `scripts/comprehensive_sync_test.py` and add your scenario:

```python
def create_test_scenarios() -> List[TestScenario]:
    scenarios = []

    # Your custom scenario
    scenarios.append(TestScenario(
        name="my_custom_test",
        description="Test specific edge case",
        files=[
            TestFile("path/to/file.txt", b"content here"),
            TestFile("another/file.dat", b"more content"),
        ]
    ))

    return scenarios
```

Then run:
```bash
./run-comprehensive-with-proxy.sh --test-scenario my_custom_test
```

---

## Output Files

### Test Results

```
docker/seafile-cli-debug/test-results/
└── test_report_20260117_040000.txt
```

Contains:
- Summary (scenarios tested, pass/fail count)
- Per-scenario results (files synced, verified, errors)
- Comparison between remote and local servers

### Traffic Captures (if mitmproxy enabled)

```
docker/seafile-cli-debug/captures/
├── remote/
│   ├── scenario_name_remote_traffic.mitm  # mitmproxy format
│   └── scenario_name_remote_traffic.har   # HAR format
└── local/
    ├── scenario_name_local_traffic.mitm
    └── scenario_name_local_traffic.har
```

### Temporary Test Data

```
/tmp/seafile-sync-test/
├── remote-config/     # Remote server seaf-cli config
├── local-config/      # Local server seaf-cli config
├── remote-sync/       # Remote synced files
├── local-sync/        # Local synced files
└── results/           # Test reports
```

---

## Continuous Integration

### Add to CI Pipeline

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

      - name: Wait for server
        run: |
          for i in {1..30}; do
            curl -s http://localhost:8080/api2/ping && break
            sleep 2
          done

      - name: Run sync protocol tests
        run: |
          cd docker/seafile-cli-debug
          ./run-comprehensive-with-proxy.sh --quick

      - name: Upload test results
        if: always()
        uses: actions/upload-artifact@v2
        with:
          name: sync-test-results
          path: docker/seafile-cli-debug/test-results/
```

---

## Performance Benchmarking

Track sync performance over time:

```bash
# Run and time full suite
time ./run-comprehensive-with-proxy.sh --test-all

# Check individual scenario times
grep "Sync duration" test-results/test_report_*.txt
```

Example output:
```
Remote: Sync duration: 2.34s
Local:  Sync duration: 2.41s
```

---

## FAQ

**Q: Tests pass locally but fail in CI?**
- Check Docker networking (use `--network host`)
- Verify server has time to start (add wait loop)
- Check resource limits (CI may have less memory/CPU)

**Q: How do I test only large files?**
```bash
./run-comprehensive-with-proxy.sh --test-scenario large_file
```

**Q: Can I test against a different Seafile server?**
Edit `scripts/comprehensive_sync_test.py` and change:
```python
REMOTE_URL = "https://your-seafile-server.com"
REMOTE_USER = "your-email@example.com"
REMOTE_PASS = "your-password"
```

**Q: Tests are slow, can I speed them up?**
- Use `--quick` instead of `--test-all`
- Test single scenarios with `--test-scenario`
- Reduce file sizes in test scenarios

**Q: Where are desktop client logs?**
```bash
# Local server client logs
docker run --rm -v /tmp:/tmp cool-storage-api-seafile-cli \
  cat /tmp/seafile-sync-test/local-config/logs/seafile.log
```

---

## Support

For issues or questions:
1. Check this guide first
2. Review `COMPREHENSIVE_TESTING.md` for detailed documentation
3. Check existing issues in GitHub repo
4. Create new issue with test output and logs
