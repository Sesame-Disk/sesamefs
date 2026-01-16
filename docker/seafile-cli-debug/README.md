# Seafile Protocol Testing & Comparison Framework

This directory contains tools to capture, analyze, and compare Seafile sync protocol implementations between a reference Seafile server and your local SesameFS server.

## Quick Start

```bash
# 1. Start your local SesameFS server
go run cmd/sesamefs/main.go

# 2. Run protocol comparison
cd docker/seafile-cli-debug
./run-comparison.sh compare
```

This will:
- Test encrypted library operations on both servers
- Capture all HTTP traffic via mitmproxy
- Generate detailed diff reports showing any protocol mismatches

## Prerequisites

- **Docker** and **docker-compose** installed
- **Local SesameFS server** running on `http://localhost:8080`
- **Remote server credentials** (configured in `.seafile-reference.md`)

## What It Tests

### Encrypted Library Operations

The comparison framework tests the complete encrypted library lifecycle:

1. **Create Encrypted Library** (`POST /api2/repos/`)
   - Compares: `encrypted`, `enc_version`, `magic`, `random_key` fields
   - Validates: PBKDF2 key derivation parameters

2. **Set/Verify Password** (`POST /api/v2.1/repos/{id}/set-password/`)
   - Compares: Password verification logic
   - Validates: Magic computation (repo_id + password)

3. **Get Download Info** (`GET /api2/repos/{id}/download-info/`)
   - Compares: Sync token generation
   - Validates: Encryption metadata consistency

4. **Get HEAD Commit** (`GET /seafhttp/repo/{id}/commit/HEAD`)
   - Compares: Response structure and field types
   - Validates: `is_corrupted` field type (must be integer, not boolean)

5. **Get Full Commit** (`GET /seafhttp/repo/{id}/commit/{commit_id}`)
   - Compares: Encryption fields in commit object
   - Validates: `encrypted`, `enc_version`, `magic`, `key` fields

## Directory Structure

```
docker/seafile-cli-debug/
├── README.md                    # This file
├── Dockerfile                   # Container with seaf-cli + mitmproxy
├── docker-compose.yml           # Container orchestration
├── run-comparison.sh            # Main entry point script
├── scripts/
│   ├── capture_addon.py         # mitmproxy addon for traffic capture
│   ├── generate_docs.py         # Generate protocol docs from captures
│   ├── seaf-debug.sh            # Manual capture utility
│   └── compare_protocol.py      # Automated comparison tool
└── captures/                    # Output directory for captures
    └── comparison_YYYYMMDD_HHMMSS/
        ├── COMPARISON_REPORT.md # Human-readable diff report
        ├── diffs.json           # Machine-readable diffs
        └── session_*/           # Raw captured traffic (JSON)
```

## Usage

### Run Full Comparison (Recommended)

```bash
./run-comparison.sh compare
```

This runs the complete test suite and generates a report.

### Build Container Only

```bash
./run-comparison.sh build
```

### Interactive Debugging

```bash
./run-comparison.sh shell
```

Inside the container, you can run:

```bash
# Manual operation capture
seaf-debug.sh capture-commits

# Run comparison manually
python3 /usr/local/bin/compare_protocol.py --test all

# View captures
ls -la /captures
```

### Capture Single Operation

```bash
./run-comparison.sh capture capture-pack-fs
```

Available capture commands (from `seaf-debug.sh`):
- `capture-protocol` - Protocol version
- `capture-account` - Account info
- `capture-list` - Library listing
- `capture-commits` - Commit operations
- `capture-pack-fs` - pack-fs binary format
- `capture-check-fs` - check-fs endpoint
- `capture-check-blocks` - check-blocks endpoint

### Clean Old Captures

```bash
./run-comparison.sh clean
```

## Understanding the Output

### COMPARISON_REPORT.md

The report shows:

1. **Summary** - Total number of differences found
2. **Per-Test Details**:
   - Endpoint tested
   - List of specific differences
   - Side-by-side comparison of remote vs local responses

Example diff:

```markdown
### POST /api2/repos/ (encrypted)

**Differences:**
- enc_version: Type mismatch - remote=int, local=str
- magic: Value mismatch - remote=64 hex chars, local=empty

**Remote Response:**
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "eee7b4a7a2539f2e4fb1c88a40121077..."
}

**Local Response:**
{
  "encrypted": true,
  "enc_version": "2",
  "magic": ""
}
```

### diffs.json

Machine-readable format for automated analysis:

```json
[
  {
    "test": "Create Encrypted Library",
    "endpoint": "POST /api2/repos/",
    "timestamp": "2026-01-16T10:30:00",
    "differences": ["enc_version: Type mismatch..."],
    "remote": {...},
    "local": {...}
  }
]
```

### Raw Traffic Captures

Each session directory contains JSON files with complete HTTP request/response data:

```
session_20260116_103000/
├── 0001_POST_api2_auth-token.json
├── 0002_POST_api2_repos.json
├── 0003_GET_api2_repos_{id}_download-info.json
└── session_summary.json
```

## Configuration

### Environment Variables

Set these before running `run-comparison.sh`:

```bash
export REMOTE_SERVER="https://app.nihaoconsult.com"
export REMOTE_USER="your-email@example.com"
export REMOTE_PASS="your-password"

export LOCAL_SERVER="http://localhost:8080"
export LOCAL_USER="test@example.com"
export LOCAL_PASS="testpass"
```

Or edit `docker-compose.yml` to change defaults.

### Testing Against Different Servers

To test against a different remote server:

```bash
REMOTE_SERVER="https://other-seafile.com" ./run-comparison.sh compare
```

To test against local server on different port:

```bash
LOCAL_SERVER="http://localhost:9090" ./run-comparison.sh compare
```

## Troubleshooting

### "Local server is NOT running"

Start SesameFS:

```bash
cd /Users/abel/Documents/Code-Experiments/cool-storage-api
go run cmd/sesamefs/main.go
```

Verify it's accessible:

```bash
curl http://localhost:8080/api2/server-info/
```

### "Failed to authenticate"

Check credentials in `.seafile-reference.md` or environment variables.

For local server, ensure you've created a test user:

```bash
# Inside SesameFS (add user creation endpoint if needed)
curl -X POST http://localhost:8080/api/v2.1/admin/users/ \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"testpass"}'
```

### "No differences found" but sync still fails

The comparison tool tests HTTP API responses, not the full sync flow. If desktop client sync fails, check:

1. **Client logs** (`~/.ccnet/logs/seafile.log`)
2. **Binary format issues** - Run `./run-comparison.sh capture capture-pack-fs` and check pack-fs output
3. **Block encryption** - Ensure blocks are encrypted with correct IV derivation

### Debugging Binary Formats

pack-fs uses a specific binary format:

```
[40-byte hex fs_id][4-byte size big-endian][zlib-compressed JSON]
```

To debug pack-fs responses:

```bash
./run-comparison.sh shell

# Inside container
seaf-debug.sh capture-pack-fs

# Check output
xxd /captures/pack-fs-response.bin | head -20
```

## Advanced Usage

### Custom Test Script

Create your own test in the container:

```bash
./run-comparison.sh shell

# Inside container
cat > /tmp/my_test.sh <<'EOF'
#!/bin/bash
TOKEN=$(seaf-debug.sh get-token)
REPO_ID="your-repo-id"

# Test specific endpoint
curl -H "Authorization: Token $TOKEN" \
  "http://host.docker.internal:8080/api2/repos/$REPO_ID/download-info/"
EOF

chmod +x /tmp/my_test.sh
/tmp/my_test.sh
```

### Comparing Specific Fields

Modify `compare_protocol.py` to add custom comparisons:

```python
# In EncryptedLibraryTest class
def test_custom_field(self):
    # Your custom test logic
    pass

# Add to run_all_tests()
self.test_custom_field()
```

### Exporting Protocol Documentation

After capturing traffic, generate docs:

```bash
./run-comparison.sh shell

# Inside container
python3 /usr/local/bin/generate_docs.py

# Output: /captures/SEAFILE-PROTOCOL.md
```

## Integration with CI/CD

Add to your test pipeline:

```yaml
# .github/workflows/protocol-test.yml
- name: Run Protocol Comparison
  run: |
    docker-compose -f docker/seafile-cli-debug/docker-compose.yml \
      run --rm seafile-compare \
      python3 /usr/local/bin/compare_protocol.py --test all

- name: Check for Differences
  run: |
    if [ -f docker/seafile-cli-debug/captures/*/diffs.json ]; then
      DIFF_COUNT=$(jq '. | length' docker/seafile-cli-debug/captures/*/diffs.json)
      if [ "$DIFF_COUNT" -gt 0 ]; then
        echo "::error::Found $DIFF_COUNT protocol differences"
        exit 1
      fi
    fi
```

## Next Steps

After running comparisons:

1. **Review COMPARISON_REPORT.md** - Identify specific protocol mismatches
2. **Fix local implementation** - Update SesameFS to match reference behavior
3. **Re-run comparison** - Verify fixes eliminated differences
4. **Test with real client** - Use Seafile desktop client to test full sync
5. **Update protocol docs** - Document findings in `docs/SEAFILE-SYNC-PROTOCOL.md`

## Related Documentation

- [SEAFILE-SYNC-PROTOCOL.md](../../docs/SEAFILE-SYNC-PROTOCOL.md) - Protocol specification
- [ENCRYPTION.md](../../docs/ENCRYPTION.md) - Encryption implementation details
- [.seafile-reference.md](../../.seafile-reference.md) - Reference server credentials
- [TESTING.md](../../docs/TESTING.md) - General testing guide

## Support

If you encounter issues:

1. Check `captures/comparison_*/COMPARISON_REPORT.md` for detailed diffs
2. Review `captures/session_*/session_summary.json` for raw traffic
3. Compare binary formats with `xxd` for pack-fs issues
4. Check client logs at `~/.ccnet/logs/seafile.log` for sync failures
