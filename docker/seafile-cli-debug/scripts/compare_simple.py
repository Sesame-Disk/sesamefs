#!/usr/bin/env python3
"""
Simple Seafile Protocol Comparison (without proxy)
Direct comparison of responses for speed and reliability.
"""

import os
import sys
import json
import subprocess
import time
from datetime import datetime
from pathlib import Path

# Configuration
REMOTE_SERVER = os.environ.get("REMOTE_SERVER", "https://app.nihaoconsult.com")
LOCAL_SERVER = os.environ.get("LOCAL_SERVER", "http://host.docker.internal:8080")
REMOTE_USER = os.environ.get("REMOTE_USER", "abel.aguzmans@gmail.com")
REMOTE_PASS = os.environ.get("REMOTE_PASS", "Qwerty123!")
LOCAL_USER = os.environ.get("LOCAL_USER", "test@example.com")
LOCAL_PASS = os.environ.get("LOCAL_PASS", "testpass")

CAPTURE_DIR = os.environ.get("CAPTURE_DIR", "/captures")

class Colors:
    RED = '\033[0;31m'
    GREEN = '\033[0;32m'
    YELLOW = '\033[1;33m'
    BLUE = '\033[0;34m'
    CYAN = '\033[0;36m'
    MAGENTA = '\033[0;35m'
    NC = '\033[0m'

def log_info(msg): print(f"{Colors.GREEN}[INFO]{Colors.NC} {msg}", file=sys.stderr)
def log_warn(msg): print(f"{Colors.YELLOW}[WARN]{Colors.NC} {msg}", file=sys.stderr)
def log_error(msg): print(f"{Colors.RED}[ERROR]{Colors.NC} {msg}", file=sys.stderr)
def log_diff(msg): print(f"{Colors.MAGENTA}[DIFF]{Colors.NC} {msg}", file=sys.stderr)
def log_section(msg): print(f"\n{Colors.BLUE}{'='*60}\n{msg}\n{'='*60}{Colors.NC}\n", file=sys.stderr)

def curl(*args, return_json=True):
    """Execute curl command"""
    cmd = ["curl", "-s", "-k"]  # -k to ignore SSL cert issues
    cmd.extend(args)

    result = subprocess.run(cmd, capture_output=True, text=True)

    if result.returncode != 0:
        log_error(f"curl failed: {result.stderr}")
        return None

    output = result.stdout

    if return_json:
        try:
            return json.loads(output) if output else None
        except:
            return output
    return output

def compare_json(remote, local, context, path=""):
    """Compare two JSON structures recursively"""
    differences = []

    if type(remote) != type(local):
        differences.append({
            "path": path or "root",
            "type": "type_mismatch",
            "remote": type(remote).__name__,
            "local": type(local).__name__
        })
        return differences

    if isinstance(remote, dict):
        all_keys = set(remote.keys()) | set(local.keys())
        for key in all_keys:
            new_path = f"{path}.{key}" if path else key

            # Skip comparison for expected differences
            if key in ['token', 'repo_id', 'commit_id', 'ctime', 'mtime', 'head_commit_id', 'root_id', 'parent_id']:
                continue

            if key not in remote:
                differences.append({
                    "path": new_path,
                    "type": "missing_in_remote",
                    "local_value": local[key]
                })
            elif key not in local:
                differences.append({
                    "path": new_path,
                    "type": "missing_in_local",
                    "remote_value": remote[key]
                })
            else:
                differences.extend(compare_json(remote[key], local[key], context, new_path))

    elif isinstance(remote, list):
        if len(remote) != len(local):
            differences.append({
                "path": path,
                "type": "array_length_mismatch",
                "remote": len(remote),
                "local": len(local)
            })
        else:
            for i, (r_item, l_item) in enumerate(zip(remote, local)):
                differences.extend(compare_json(r_item, l_item, context, f"{path}[{i}]"))

    else:
        # Leaf value
        if remote != local:
            differences.append({
                "path": path,
                "type": "value_mismatch",
                "remote": remote,
                "local": local
            })

    return differences

def test_protocol_version():
    """Test protocol version"""
    log_section("TEST: Protocol Version")

    remote_ver = curl(f"{REMOTE_SERVER}/seafhttp/protocol-version")
    local_ver = curl(f"{LOCAL_SERVER}/seafhttp/protocol-version")

    diffs = compare_json(remote_ver, local_ver, "Protocol Version")

    if diffs:
        log_diff(f"Found {len(diffs)} differences:")
        for d in diffs:
            log_diff(f"  {d}")
        return [{"test": "Protocol Version", "endpoint": "/seafhttp/protocol-version", "diffs": diffs, "remote": remote_ver, "local": local_ver}]

    log_info("Protocol Version: OK ✓")
    return []

def test_create_encrypted_library():
    """Test creating encrypted library"""
    log_section("TEST: Create Encrypted Library")

    # Get tokens
    log_info("Getting auth tokens...")

    remote_auth = curl(
        "-X", "POST",
        f"{REMOTE_SERVER}/api2/auth-token/",
        "--data-urlencode", f"username={REMOTE_USER}",
        "--data-urlencode", f"password={REMOTE_PASS}"
    )

    local_auth = curl(
        "-X", "POST",
        f"{LOCAL_SERVER}/api2/auth-token/",
        "--data-urlencode", f"username={LOCAL_USER}",
        "--data-urlencode", f"password={LOCAL_PASS}"
    )

    if not remote_auth or not local_auth:
        log_error("Failed to get auth tokens")
        return [{"test": "Authentication", "error": "Failed to authenticate"}]

    remote_token = remote_auth.get('token')
    local_token = local_auth.get('token')

    log_info(f"Remote token: {remote_token[:20]}...")
    log_info(f"Local token: {local_token[:20]}...")

    # Create encrypted libraries
    lib_name = f"EncTest_{int(time.time())}"
    lib_password = "TestPassword123!"

    log_info(f"Creating encrypted library '{lib_name}'...")

    remote_lib = curl(
        "-X", "POST",
        "-H", f"Authorization: Token {remote_token}",
        f"{REMOTE_SERVER}/api2/repos/",
        "--data-urlencode", f"name={lib_name}",
        "--data-urlencode", "desc=Test encrypted library",
        "--data-urlencode", f"passwd={lib_password}"
    )

    local_lib = curl(
        "-X", "POST",
        "-H", f"Authorization: Token {local_token}",
        f"{LOCAL_SERVER}/api2/repos/",
        "--data-urlencode", f"name={lib_name}_local",
        "--data-urlencode", "desc=Test encrypted library",
        "--data-urlencode", f"passwd={lib_password}"
    )

    all_diffs = []

    # Compare encryption fields
    if remote_lib and local_lib:
        fields = ['encrypted', 'enc_version', 'magic', 'random_key', 'salt']
        remote_enc = {k: remote_lib.get(k) for k in fields}
        local_enc = {k: local_lib.get(k) for k in fields}

        log_info(f"Remote encryption fields: {json.dumps(remote_enc, indent=2)}")
        log_info(f"Local encryption fields: {json.dumps(local_enc, indent=2)}")

        diffs = compare_json(remote_enc, local_enc, "Create Encrypted Library")

        if diffs:
            log_diff(f"Found {len(diffs)} differences in encryption fields:")
            for d in diffs:
                log_diff(f"  {d}")
            all_diffs.append({
                "test": "Create Encrypted Library",
                "endpoint": "POST /api2/repos/",
                "diffs": diffs,
                "remote": remote_enc,
                "local": local_enc
            })
        else:
            log_info("Encryption fields: OK ✓")

        # Cleanup
        if remote_lib.get('repo_id'):
            curl("-X", "DELETE", "-H", f"Authorization: Token {remote_token}",
                 f"{REMOTE_SERVER}/api2/repos/{remote_lib['repo_id']}/", return_json=False)

        if local_lib.get('repo_id'):
            curl("-X", "DELETE", "-H", f"Authorization: Token {local_token}",
                 f"{LOCAL_SERVER}/api2/repos/{local_lib['repo_id']}/", return_json=False)

        return all_diffs

    log_error("Failed to create libraries")
    return [{"test": "Create Encrypted Library", "error": "Failed to create one or both libraries"}]

def test_server_info():
    """Test server info"""
    log_section("TEST: Server Info")

    remote_info = curl(f"{REMOTE_SERVER}/api2/server-info/")
    local_info = curl(f"{LOCAL_SERVER}/api2/server-info/")

    # Compare encryption-related fields only
    fields = ['encrypted_library_version', 'enable_encrypted_library']
    remote_enc = {k: remote_info.get(k) for k in fields if k in remote_info}
    local_enc = {k: local_info.get(k) for k in fields if k in local_info}

    diffs = compare_json(remote_enc, local_enc, "Server Info")

    if diffs:
        log_diff(f"Found {len(diffs)} differences:")
        for d in diffs:
            log_diff(f"  {d}")
        return [{"test": "Server Info", "endpoint": "/api2/server-info/", "diffs": diffs, "remote": remote_enc, "local": local_enc}]

    log_info("Server Info: OK ✓")
    return []

def main():
    log_section("SEAFILE PROTOCOL COMPARISON (SIMPLE MODE)")
    log_info(f"Remote: {REMOTE_SERVER}")
    log_info(f"Local: {LOCAL_SERVER}")

    all_diffs = []

    # Run tests
    all_diffs.extend(test_server_info())
    all_diffs.extend(test_protocol_version())
    all_diffs.extend(test_create_encrypted_library())

    # Generate report
    output_dir = Path(CAPTURE_DIR) / f"comparison_{datetime.now().strftime('%Y%m%d_%H%M%S')}"
    output_dir.mkdir(parents=True, exist_ok=True)

    report_file = output_dir / "COMPARISON_REPORT.md"

    if all_diffs:
        # Generate markdown report
        md = ["# Seafile Protocol Comparison Report", ""]
        md.append(f"*Generated: {datetime.now().isoformat()}*")
        md.append("")
        md.append(f"**Total Issues Found:** {len(all_diffs)}")
        md.append("")

        for diff_set in all_diffs:
            md.append(f"## {diff_set['test']}")
            md.append("")

            if 'error' in diff_set:
                md.append(f"**Error:** {diff_set['error']}")
                md.append("")
                continue

            md.append(f"**Endpoint:** `{diff_set.get('endpoint', 'N/A')}`")
            md.append("")

            if diff_set.get('diffs'):
                md.append("**Differences:**")
                md.append("")
                for d in diff_set['diffs']:
                    md.append(f"- **{d['path']}**: {d['type']}")
                    if 'remote' in d:
                        md.append(f"  - Remote: `{d['remote']}`")
                    if 'local' in d:
                        md.append(f"  - Local: `{d['local']}`")
                md.append("")

            md.append("**Remote Response:**")
            md.append("```json")
            md.append(json.dumps(diff_set.get('remote', {}), indent=2))
            md.append("```")
            md.append("")

            md.append("**Local Response:**")
            md.append("```json")
            md.append(json.dumps(diff_set.get('local', {}), indent=2))
            md.append("```")
            md.append("")
            md.append("---")
            md.append("")

        report_file.write_text("\n".join(md))

        # Save JSON
        json_file = output_dir / "diffs.json"
        json_file.write_text(json.dumps(all_diffs, indent=2))

        log_section("COMPARISON COMPLETE")
        log_warn(f"Found {len(all_diffs)} differences")
        log_info(f"Report: {report_file}")

        # Print summary
        print("\n" + "="*60)
        print("DIFFERENCES FOUND:")
        print("="*60)
        for diff_set in all_diffs:
            print(f"\n{diff_set['test']}:")
            if 'error' in diff_set:
                print(f"  Error: {diff_set['error']}")
            elif diff_set.get('diffs'):
                for d in diff_set['diffs']:
                    print(f"  - {d['path']}: {d['type']}")
                    if 'remote' in d and 'local' in d:
                        print(f"    Remote: {d['remote']}, Local: {d['local']}")

        return 1
    else:
        report_file.write_text("# Seafile Protocol Comparison Report\n\n**No differences found!** ✓\n")

        log_section("COMPARISON COMPLETE")
        log_info("✓ No differences found! Protocols match perfectly.")

        return 0

if __name__ == "__main__":
    sys.exit(main())
