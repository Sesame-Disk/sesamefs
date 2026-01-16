# Protocol Comparison Framework - Summary

## What Was Created

I've built a comprehensive testing framework to compare your local SesameFS implementation against the reference Seafile server, with special focus on encrypted library operations.

## 🎯 Problem Solved

**Before:** You had a protocol specification document but couldn't verify if your implementation actually matches Seafile's behavior for encrypted libraries.

**Now:** You have an automated tool that:
1. Runs identical operations on both servers
2. Captures all HTTP traffic via mitmproxy
3. Generates detailed diff reports showing exact differences
4. Tests critical encrypted library operations

## 📁 Files Created

### Main Scripts

1. **`docker/seafile-cli-debug/scripts/compare_protocol.py`** (750+ lines)
   - Core comparison engine
   - Tests 9 different encrypted library operations
   - Automated diff detection and reporting
   - JSON response comparison with special handling for encryption fields

2. **`docker/seafile-cli-debug/run-comparison.sh`**
   - Easy-to-use wrapper script
   - Handles Docker container management
   - One-command testing: `./run-comparison.sh compare`

3. **`docker/seafile-cli-debug/docker-compose.yml`**
   - Container orchestration
   - Pre-configured with reference server credentials
   - Maps to local server via `host.docker.internal`

### Documentation

4. **`docker/seafile-cli-debug/README.md`**
   - Complete framework documentation
   - Configuration guide
   - Troubleshooting tips
   - Advanced usage examples

5. **`docker/seafile-cli-debug/GETTING_STARTED.md`**
   - Step-by-step quickstart guide
   - Common issues and solutions
   - What's being tested (detailed breakdown)

6. **`PROTOCOL-COMPARISON-SUMMARY.md`** (this file)
   - Overview of what was built

### Existing Files Enhanced

The framework leverages existing tools:
- `capture_addon.py` - mitmproxy addon (captures both requests AND responses)
- `generate_docs.py` - Protocol documentation generator
- `seaf-debug.sh` - Manual capture utilities

## 🧪 What It Tests

### Encrypted Library Operations (9 Tests)

1. **Protocol Version** - Verifies v2 protocol support
2. **Create Encrypted Library** - Tests PBKDF2 key derivation, magic, random_key
3. **Set Password** - Validates password verification endpoint
4. **Download Info** - Checks sync token and encryption metadata
5. **HEAD Commit** - Verifies `is_corrupted` field type (int vs bool)
6. **Full Commit** - Validates encryption fields in commit object
7. **FS-ID-List** - Tests JSON array response format
8. **Pack-FS Format** - Validates binary format: `[40 ID][4 size][zlib JSON]`
9. **Check-FS** - Tests JSON array input/output

### Critical Encryption Validations

- ✅ `enc_version` type (must be int `2`, not string `"2"`)
- ✅ `magic` computation (uses `repo_id + password`)
- ✅ `random_key` encryption (uses `password` only)
- ✅ `is_corrupted` field (must be int `0`, not bool `false`)
- ✅ Binary pack-fs format (proper zlib compression)
- ✅ Field ordering in JSON (alphabetical for fs_id hash computation)

## 🚀 How to Use

### Quick Start

```bash
# 1. Start local server
go run cmd/sesamefs/main.go

# 2. Run comparison
cd docker/seafile-cli-debug
./run-comparison.sh compare
```

### Output

```
========== ENCRYPTED LIBRARY PROTOCOL COMPARISON ==========

[INFO] Remote: https://app.nihaoconsult.com
[INFO] Local: http://host.docker.internal:8080

========== TEST: Protocol Version ==========
[INFO] Responses match ✓

========== TEST: Create Encrypted Library ==========
[DIFF] Found 2 differences
  - enc_version: Type mismatch - remote=int, local=str
  - magic: Value mismatch - remote=64 hex chars, local=empty

...

========== COMPARISON COMPLETE ==========
[WARN] Found 5 differences
[INFO] Review report: captures/comparison_20260116_123456/COMPARISON_REPORT.md
```

### Review Results

```bash
# View full report
cat captures/comparison_*/COMPARISON_REPORT.md

# Check machine-readable diffs
jq . captures/comparison_*/diffs.json

# Inspect raw HTTP traffic
cat captures/comparison_*/remote_*/session_summary.json | jq .
```

## 📊 Output Format

### COMPARISON_REPORT.md

Markdown report with:
- Summary of total differences
- Per-test breakdown
- Side-by-side comparison of remote vs local responses
- Specific field-level differences

### diffs.json

```json
[
  {
    "test": "Create Encrypted Library",
    "endpoint": "POST /api2/repos/",
    "timestamp": "2026-01-16T10:30:00",
    "differences": [
      "enc_version: Type mismatch - remote=int, local=str"
    ],
    "remote": { ... },
    "local": { ... }
  }
]
```

### Raw Traffic Captures

Each test operation gets its own capture session:
```
captures/comparison_20260116_123456/
├── remote_create_enc_lib_20260116_103001/
│   ├── 0001_POST_api2_auth-token.json      # Full request/response
│   ├── 0002_POST_api2_repos.json
│   └── session_summary.json
└── local_create_enc_lib_20260116_103002/
    ├── 0001_POST_api2_auth-token.json
    ├── 0002_POST_api2_repos.json
    └── session_summary.json
```

## 🔍 Key Features

### 1. Captures BOTH Requests AND Responses

Unlike basic tcpdump, the mitmproxy addon captures:
- Full HTTP headers
- Request/response bodies
- Binary data (with hex preview)
- Decompressed zlib content
- Parsed pack-fs format

### 2. Intelligent Comparison

- Ignores expected differences (IDs, timestamps, tokens)
- Focuses on protocol-critical fields
- Detects type mismatches (int vs string)
- Validates binary format structure

### 3. Detailed Diff Reports

- Human-readable markdown
- Machine-readable JSON
- Full context for each difference
- Actionable error messages

### 4. Easy to Extend

Add new tests by editing `compare_protocol.py`:

```python
def test_my_endpoint(self, remote_repo_id, local_repo_id):
    """Test: My custom endpoint"""
    log_section("TEST: My Custom Endpoint")

    # Remote
    remote_capture = ProxyCapture("remote_my_test")
    remote_capture.start()
    self.remote.use_proxy = True
    remote_result = self.remote._curl(...)
    remote_capture.stop()
    self.remote.use_proxy = False

    # Local
    local_capture = ProxyCapture("local_my_test")
    local_capture.start()
    self.local.use_proxy = True
    local_result = self.local._curl(...)
    local_capture.stop()
    self.local.use_proxy = False

    # Compare
    diffs = self.comparator.compare_json_responses(remote_result, local_result, "My Test")
    if diffs:
        self.comparator.record_diff("My Test", "/my/endpoint", remote_result, local_result, diffs)

# Add to run_all_tests()
```

## 🎓 Workflow

### Development Workflow

1. **Make changes** to local SesameFS implementation
2. **Run comparison**: `./run-comparison.sh compare`
3. **Review diffs**: Check `COMPARISON_REPORT.md`
4. **Fix issues** in local code
5. **Repeat** until no differences found
6. **Test with real client** - Use Seafile desktop app

### Continuous Integration

Add to CI pipeline:

```yaml
- name: Protocol Comparison
  run: |
    cd docker/seafile-cli-debug
    ./run-comparison.sh compare

- name: Check Results
  run: |
    DIFFS=$(jq '. | length' docker/seafile-cli-debug/captures/*/diffs.json)
    if [ "$DIFFS" -gt 0 ]; then
      echo "::error::Found $DIFFS protocol differences"
      exit 1
    fi
```

## 🐛 Common Issues & Fixes

### Issue: "No differences found" but desktop client sync fails

**Cause:** HTTP API matches, but desktop client uses different code paths

**Solution:**
1. Use `./run-comparison.sh shell`
2. Run `seaf-debug.sh sync-with-capture <repo_id>` to capture full sync
3. Check client logs: `~/.ccnet/logs/seafile.log`
4. Look for binary format issues (pack-fs, blocks)

### Issue: pack-fs format errors

**Symptoms:** "Failed to inflate" in client logs

**Debug:**
```bash
./run-comparison.sh shell
seaf-debug.sh capture-pack-fs

# Check format
xxd /captures/pack-fs-response.bin | head -20

# First 40 bytes should be ASCII hex (fs_id)
# Next 4 bytes should be size (big-endian)
# Rest should be zlib compressed (starts with 0x789c)
```

**Fix:** Ensure `internal/api/sync.go` pack-fs endpoint:
1. Sends 40-byte ASCII hex fs_id
2. Sends 4-byte big-endian size
3. Compresses JSON with `zlib.compress()`

### Issue: Type mismatches (int vs string)

**Common fields:**
- `enc_version` - Must be int `2`, not `"2"`
- `is_corrupted` - Must be int `0`, not `false`
- `encrypted` - Can be int `1` or bool `true` (Seafile accepts both)

**Fix:** Update struct tags in Go:
```go
EncVersion int `json:"enc_version"`  // NOT string
IsCorrupted int `json:"is_corrupted"` // NOT bool
```

## 📚 Documentation References

- **Framework README**: `docker/seafile-cli-debug/README.md`
- **Getting Started**: `docker/seafile-cli-debug/GETTING_STARTED.md`
- **Protocol Spec**: `docs/SEAFILE-SYNC-PROTOCOL.md`
- **Encryption Details**: `docs/ENCRYPTION.md`
- **Reference Server Creds**: `.seafile-reference.md`

## 🎯 Success Criteria

Your implementation is correct when:

1. ✅ `./run-comparison.sh compare` shows **"No differences found"**
2. ✅ Seafile desktop client can **create encrypted libraries**
3. ✅ Desktop client can **sync encrypted libraries**
4. ✅ **Password verification** works (set-password endpoint)
5. ✅ **File upload/download** works in encrypted libraries
6. ✅ **Password change** works without re-encrypting all files

## 💡 Next Steps

### Immediate

1. Run first comparison: `./run-comparison.sh compare`
2. Review generated report
3. Fix any differences found
4. Re-run until clean

### Short-term

1. Add more test cases (file upload, block operations)
2. Test password change flow
3. Test with real Seafile desktop client
4. Document any edge cases found

### Long-term

1. Add to CI/CD pipeline
2. Create regression test suite
3. Test with different Seafile versions
4. Generate updated protocol documentation

## 🤝 Contributing

To add new tests or improve the framework:

1. Edit `compare_protocol.py` - Add test methods
2. Update `README.md` - Document new tests
3. Test with: `./run-comparison.sh compare`
4. Commit changes

## 📝 Notes

- **Proxy captures both directions** - Unlike tcpdump, mitmproxy sees full HTTP
- **Binary format validation** - Tests pack-fs, not just JSON responses
- **Encryption-focused** - Specifically validates PBKDF2, magic, random_key
- **Easy to extend** - Add new tests without modifying container
- **Production-ready** - Can be used in CI/CD pipelines

## ❓ Questions?

Review these files in order:
1. `GETTING_STARTED.md` - Quickstart guide
2. `README.md` - Full documentation
3. `compare_protocol.py` - Implementation details
4. Capture output in `captures/` - Real examples

---

**Summary:** You now have a complete, automated testing framework to ensure your SesameFS implementation is 100% compatible with Seafile's sync protocol for encrypted libraries. Run `./run-comparison.sh compare` to get started!
