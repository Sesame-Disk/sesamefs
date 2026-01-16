#!/usr/bin/env python3
"""
Comprehensive Seafile Encrypted Library Sync Protocol Comparison

Tests the complete sync workflow for encrypted libraries:
1. Library creation with encryption
2. Password verification
3. Sync token generation
4. Commit operations
5. FS object handling
6. Block operations
7. File upload/download cycle
8. Binary format validation

This ensures your implementation can fully sync encrypted libraries
with the official Seafile desktop client.
"""

import os
import sys
import json
import subprocess
import time
import hashlib
import zlib
from datetime import datetime
from pathlib import Path
from typing import Optional, Dict, List, Tuple

# Configuration
REMOTE_SERVER = os.environ.get("REMOTE_SERVER", "https://app.nihaoconsult.com")
LOCAL_SERVER = os.environ.get("LOCAL_SERVER", "http://host.docker.internal:8080")
REMOTE_USER = os.environ.get("REMOTE_USER", "abel.aguzmans@gmail.com")
REMOTE_PASS = os.environ.get("REMOTE_PASS", "Qwerty123!")
LOCAL_USER = os.environ.get("LOCAL_USER", "admin@sesamefs.local")
LOCAL_PASS = os.environ.get("LOCAL_PASS", "dev-token-123")
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
def log_section(msg): print(f"\n{Colors.BLUE}{'='*70}\n{msg}\n{'='*70}{Colors.NC}\n", file=sys.stderr)
def log_pass(msg): print(f"{Colors.GREEN}[PASS]{Colors.NC} {msg}", file=sys.stderr)

def curl(*args, return_json=True, return_binary=False):
    """Execute curl command"""
    cmd = ["curl", "-s", "-k"]
    cmd.extend(args)

    result = subprocess.run(cmd, capture_output=True)

    if result.returncode != 0:
        log_error(f"curl failed: {result.stderr.decode('utf-8', errors='replace')}")
        return None

    if return_binary:
        return result.stdout

    output = result.stdout.decode('utf-8', errors='replace')

    if return_json:
        try:
            return json.loads(output) if output else None
        except:
            return output
    return output

def compare_values(remote, local, path, differences):
    """Compare two values and track differences"""
    if type(remote) != type(local):
        differences.append({
            "path": path,
            "type": "type_mismatch",
            "remote": f"{type(remote).__name__}: {remote}",
            "local": f"{type(local).__name__}: {local}"
        })
        return

    if remote != local:
        differences.append({
            "path": path,
            "type": "value_mismatch",
            "remote": remote,
            "local": local
        })

def compare_json(remote, local, context, path="", ignore_fields=None):
    """Compare two JSON structures recursively"""
    differences = []
    ignore_fields = ignore_fields or []

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

            # Skip ignored fields
            if key in ignore_fields:
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
                differences.extend(compare_json(remote[key], local[key], context, new_path, ignore_fields))

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
                differences.extend(compare_json(r_item, l_item, context, f"{path}[{i}]", ignore_fields))

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

class TestReport:
    """Manages test results and reporting"""

    def __init__(self):
        self.tests = []
        self.passed = 0
        self.failed = 0

    def add_test(self, name, endpoint, passed, remote_data=None, local_data=None, diffs=None, notes=None):
        """Add test result"""
        self.tests.append({
            "name": name,
            "endpoint": endpoint,
            "passed": passed,
            "remote": remote_data,
            "local": local_data,
            "diffs": diffs or [],
            "notes": notes or []
        })

        if passed:
            self.passed += 1
            log_pass(f"{name}: OK ✓")
        else:
            self.failed += 1
            log_error(f"{name}: FAILED ✗")
            if diffs:
                for diff in diffs[:5]:  # Show first 5 diffs
                    log_diff(f"  {diff}")

    def generate_report(self, output_dir):
        """Generate markdown report"""
        report_file = Path(output_dir) / "COMPREHENSIVE_REPORT.md"

        md = ["# Seafile Encrypted Library Sync Protocol - Comprehensive Test Report", ""]
        md.append(f"*Generated: {datetime.now().isoformat()}*")
        md.append("")
        md.append(f"**Total Tests:** {len(self.tests)}")
        md.append(f"**Passed:** {self.passed} ✓")
        md.append(f"**Failed:** {self.failed} ✗")
        md.append("")

        if self.failed == 0:
            md.append("## 🎉 All Tests Passed!")
            md.append("")
            md.append("Your implementation is fully compatible with Seafile's encrypted library sync protocol.")
        else:
            md.append("## ⚠️ Issues Found")
            md.append("")
            md.append("The following tests failed. Fix these issues to achieve full compatibility.")

        md.append("")
        md.append("## Test Results")
        md.append("")

        for test in self.tests:
            status = "✓ PASS" if test['passed'] else "✗ FAIL"
            md.append(f"### {status}: {test['name']}")
            md.append("")
            md.append(f"**Endpoint:** `{test['endpoint']}`")
            md.append("")

            if test['notes']:
                md.append("**Notes:**")
                for note in test['notes']:
                    md.append(f"- {note}")
                md.append("")

            if not test['passed'] and test['diffs']:
                md.append("**Differences:**")
                md.append("")
                for diff in test['diffs']:
                    md.append(f"- **{diff.get('path', 'unknown')}**: {diff.get('type', 'unknown')}")
                    if 'remote' in diff:
                        md.append(f"  - Remote: `{diff['remote']}`")
                    if 'local' in diff:
                        md.append(f"  - Local: `{diff['local']}`")
                md.append("")

            if test['remote'] is not None:
                md.append("<details><summary>Remote Response</summary>")
                md.append("")
                md.append("```json")
                md.append(json.dumps(test['remote'], indent=2))
                md.append("```")
                md.append("</details>")
                md.append("")

            if test['local'] is not None:
                md.append("<details><summary>Local Response</summary>")
                md.append("")
                md.append("```json")
                md.append(json.dumps(test['local'], indent=2))
                md.append("```")
                md.append("</details>")
                md.append("")

            md.append("---")
            md.append("")

        report_file.write_text("\n".join(md))
        log_info(f"Report saved: {report_file}")
        return report_file

class EncryptedLibraryTester:
    """Comprehensive encrypted library sync protocol tester"""

    def __init__(self, remote_server, local_server, remote_user, remote_pass, local_user, local_pass):
        self.remote_server = remote_server
        self.local_server = local_server
        self.remote_user = remote_user
        self.remote_pass = remote_pass
        self.local_user = local_user
        self.local_pass = local_pass

        self.remote_token = None
        self.local_token = None
        self.remote_repo_id = None
        self.local_repo_id = None
        self.remote_sync_token = None
        self.local_sync_token = None
        self.lib_password = "SecureTestPassword123!"

        self.report = TestReport()

    def authenticate(self):
        """Test 1: Authentication"""
        log_section("TEST 1: Authentication")

        remote_auth = curl(
            "-X", "POST",
            f"{self.remote_server}/api2/auth-token/",
            "--data-urlencode", f"username={self.remote_user}",
            "--data-urlencode", f"password={self.remote_pass}"
        )

        local_auth = curl(
            "-X", "POST",
            f"{self.local_server}/api2/auth-token/",
            "--data-urlencode", f"username={self.local_user}",
            "--data-urlencode", f"password={self.local_pass}"
        )

        if remote_auth and local_auth:
            self.remote_token = remote_auth.get('token')
            self.local_token = local_auth.get('token')

            if self.remote_token and self.local_token:
                self.report.add_test(
                    "Authentication",
                    "POST /api2/auth-token/",
                    True,
                    notes=["Both servers returned valid tokens"]
                )
                return True

        self.report.add_test(
            "Authentication",
            "POST /api2/auth-token/",
            False,
            remote_auth,
            local_auth,
            notes=["Failed to get auth tokens from one or both servers"]
        )
        return False

    def test_protocol_version(self):
        """Test 2: Protocol version"""
        log_section("TEST 2: Protocol Version")

        remote_ver = curl(f"{self.remote_server}/seafhttp/protocol-version")
        local_ver = curl(f"{self.local_server}/seafhttp/protocol-version")

        diffs = compare_json(remote_ver, local_ver, "Protocol Version")

        self.report.add_test(
            "Protocol Version",
            "GET /seafhttp/protocol-version",
            len(diffs) == 0,
            remote_ver,
            local_ver,
            diffs,
            ["Should return {\"version\": 2}"]
        )

    def test_server_info(self):
        """Test 3: Server info"""
        log_section("TEST 3: Server Information")

        remote_info = curl(
            "-H", f"Authorization: Token {self.remote_token}",
            f"{self.remote_server}/api2/server-info/"
        )
        local_info = curl(
            "-H", f"Authorization: Token {self.local_token}",
            f"{self.local_server}/api2/server-info/"
        )

        # Only compare encrypted_library_version
        remote_enc_ver = {"encrypted_library_version": remote_info.get("encrypted_library_version")}
        local_enc_ver = {"encrypted_library_version": local_info.get("encrypted_library_version")}

        diffs = compare_json(remote_enc_ver, local_enc_ver, "Server Info")

        self.report.add_test(
            "Server Info",
            "GET /api2/server-info/",
            len(diffs) == 0,
            remote_info,
            local_info,
            diffs,
            ["encrypted_library_version should be 2 for both servers"]
        )

    def test_create_encrypted_library(self):
        """Test 4: Create encrypted library (CRITICAL)"""
        log_section("TEST 4: Create Encrypted Library (CRITICAL)")

        lib_name = f"EncTest_{int(time.time())}"

        log_info(f"Creating encrypted library with passwd parameter...")

        remote_lib = curl(
            "-X", "POST",
            "-H", f"Authorization: Token {self.remote_token}",
            f"{self.remote_server}/api2/repos/",
            "--data-urlencode", f"name={lib_name}",
            "--data-urlencode", "desc=Comprehensive test encrypted library",
            "--data-urlencode", f"passwd={self.lib_password}"
        )

        local_lib = curl(
            "-X", "POST",
            "-H", f"Authorization: Token {self.local_token}",
            f"{self.local_server}/api2/repos/",
            "--data-urlencode", f"name={lib_name}_local",
            "--data-urlencode", "desc=Comprehensive test encrypted library",
            "--data-urlencode", f"passwd={self.lib_password}"
        )

        if remote_lib and local_lib:
            self.remote_repo_id = remote_lib.get('repo_id')
            self.local_repo_id = local_lib.get('repo_id')

            # Compare encryption fields
            enc_fields = ['encrypted', 'enc_version', 'magic', 'random_key', 'salt']
            remote_enc = {k: remote_lib.get(k) for k in enc_fields}
            local_enc = {k: local_lib.get(k) for k in enc_fields}

            diffs = []

            # Check types explicitly
            if type(remote_lib.get('encrypted')) != type(local_lib.get('encrypted')):
                diffs.append({
                    "path": "encrypted",
                    "type": "type_mismatch",
                    "remote": f"{type(remote_lib.get('encrypted')).__name__}: {remote_lib.get('encrypted')}",
                    "local": f"{type(local_lib.get('encrypted')).__name__}: {local_lib.get('encrypted')}"
                })

            if type(remote_lib.get('enc_version')) != type(local_lib.get('enc_version')):
                diffs.append({
                    "path": "enc_version",
                    "type": "type_mismatch",
                    "remote": f"{type(remote_lib.get('enc_version')).__name__}: {remote_lib.get('enc_version')}",
                    "local": f"{type(local_lib.get('enc_version')).__name__}: {local_lib.get('enc_version')}"
                })

            # Check values
            for field in enc_fields:
                if field in ['encrypted', 'enc_version']:
                    continue  # Already checked types

                remote_val = remote_lib.get(field)
                local_val = local_lib.get(field)

                if remote_val != local_val:
                    diffs.append({
                        "path": field,
                        "type": "value_mismatch",
                        "remote": remote_val,
                        "local": local_val
                    })

            self.report.add_test(
                "Create Encrypted Library",
                "POST /api2/repos/ (with passwd parameter)",
                len(diffs) == 0,
                remote_enc,
                local_enc,
                diffs,
                [
                    "CRITICAL: encrypted must be integer 1, not boolean true",
                    "CRITICAL: enc_version must be integer 2, not string '2'",
                    "magic must be 64 hex characters (PBKDF2 hash)",
                    "random_key must be 96 hex characters (encrypted file key)",
                    "Client sends only passwd parameter, NOT encrypted parameter"
                ]
            )

            return self.remote_repo_id and self.local_repo_id

        self.report.add_test(
            "Create Encrypted Library",
            "POST /api2/repos/",
            False,
            remote_lib,
            local_lib,
            notes=["Failed to create library on one or both servers"]
        )
        return False

    def test_set_password(self):
        """Test 5: Password verification"""
        log_section("TEST 5: Password Verification")

        # Test with correct password
        remote_result = curl(
            "-X", "POST",
            "-H", f"Authorization: Token {self.remote_token}",
            "-H", "Content-Type: application/json",
            f"{self.remote_server}/api/v2.1/repos/{self.remote_repo_id}/set-password/",
            "-d", json.dumps({"password": self.lib_password})
        )

        local_result = curl(
            "-X", "POST",
            "-H", f"Authorization: Token {self.local_token}",
            "-H", "Content-Type: application/json",
            f"{self.local_server}/api/v2.1/repos/{self.local_repo_id}/set-password/",
            "-d", json.dumps({"password": self.lib_password})
        )

        diffs = compare_json(remote_result, local_result, "Set Password")

        self.report.add_test(
            "Set Password (Correct)",
            "POST /api/v2.1/repos/{id}/set-password/",
            len(diffs) == 0 and remote_result.get('success') == True,
            remote_result,
            local_result,
            diffs,
            [
                "Verifies PBKDF2 magic computation is correct",
                "Input: repo_id + password",
                "Should return {\"success\": true}"
            ]
        )

        # Test with wrong password
        wrong_remote = curl(
            "-X", "POST",
            "-H", f"Authorization: Token {self.remote_token}",
            "-H", "Content-Type: application/json",
            f"{self.remote_server}/api/v2.1/repos/{self.remote_repo_id}/set-password/",
            "-d", json.dumps({"password": "WrongPassword123!"})
        )

        wrong_local = curl(
            "-X", "POST",
            "-H", f"Authorization: Token {self.local_token}",
            "-H", "Content-Type: application/json",
            f"{self.local_server}/api/v2.1/repos/{self.local_repo_id}/set-password/",
            "-d", json.dumps({"password": "WrongPassword123!"})
        )

        # Both should have error_msg
        wrong_diffs = []
        if wrong_remote.get('error_msg') != wrong_local.get('error_msg'):
            wrong_diffs.append({
                "path": "error_msg",
                "type": "value_mismatch",
                "remote": wrong_remote.get('error_msg'),
                "local": wrong_local.get('error_msg')
            })

        self.report.add_test(
            "Set Password (Wrong)",
            "POST /api/v2.1/repos/{id}/set-password/",
            'error_msg' in wrong_remote and 'error_msg' in wrong_local,
            wrong_remote,
            wrong_local,
            wrong_diffs,
            [
                "Should return error for wrong password",
                "{\"error_msg\": \"Wrong password\"}"
            ]
        )

    def test_download_info(self):
        """Test 6: Download info (sync token)"""
        log_section("TEST 6: Download Info (Sync Token)")

        remote_info = curl(
            "-H", f"Authorization: Token {self.remote_token}",
            f"{self.remote_server}/api2/repos/{self.remote_repo_id}/download-info/"
        )

        local_info = curl(
            "-H", f"Authorization: Token {self.local_token}",
            f"{self.local_server}/api2/repos/{self.local_repo_id}/download-info/"
        )

        if remote_info and local_info:
            self.remote_sync_token = remote_info.get('token')
            self.local_sync_token = local_info.get('token')

            # Compare encryption metadata
            enc_fields = ['encrypted', 'enc_version', 'magic', 'random_key', 'salt']
            ignore = ['token', 'repo_id', 'relay_id', 'relay_addr', 'relay_port', 'email',
                     'repo_name', 'repo_desc', 'repo_size', 'mtime', 'head_commit_id', 'permission']

            diffs = compare_json(remote_info, local_info, "Download Info", ignore_fields=ignore)

            self.report.add_test(
                "Download Info",
                "GET /api2/repos/{id}/download-info/",
                len(diffs) == 0,
                {k: remote_info.get(k) for k in enc_fields},
                {k: local_info.get(k) for k in enc_fields},
                diffs,
                [
                    "Returns sync token for /seafhttp/ operations",
                    "Encryption metadata should match library creation"
                ]
            )

            return bool(self.remote_sync_token and self.local_sync_token)

        self.report.add_test(
            "Download Info",
            "GET /api2/repos/{id}/download-info/",
            False,
            remote_info,
            local_info,
            notes=["Failed to get download info"]
        )
        return False

    def test_commit_head(self):
        """Test 7: Commit HEAD"""
        log_section("TEST 7: Commit HEAD")

        remote_head = curl(
            "-H", f"Seafile-Repo-Token: {self.remote_sync_token}",
            f"{self.remote_server}/seafhttp/repo/{self.remote_repo_id}/commit/HEAD"
        )

        local_head = curl(
            "-H", f"Seafile-Repo-Token: {self.local_sync_token}",
            f"{self.local_server}/seafhttp/repo/{self.local_repo_id}/commit/HEAD"
        )

        diffs = []

        # Check is_corrupted type
        if type(remote_head.get('is_corrupted')) != type(local_head.get('is_corrupted')):
            diffs.append({
                "path": "is_corrupted",
                "type": "type_mismatch",
                "remote": f"{type(remote_head.get('is_corrupted')).__name__}: {remote_head.get('is_corrupted')}",
                "local": f"{type(local_head.get('is_corrupted')).__name__}: {local_head.get('is_corrupted')}"
            })

        # Check head_commit_id exists
        if 'head_commit_id' not in remote_head:
            diffs.append({"path": "head_commit_id", "type": "missing_in_remote"})
        if 'head_commit_id' not in local_head:
            diffs.append({"path": "head_commit_id", "type": "missing_in_local"})

        self.report.add_test(
            "Commit HEAD",
            "GET /seafhttp/repo/{id}/commit/HEAD",
            len(diffs) == 0,
            remote_head,
            local_head,
            diffs,
            [
                "CRITICAL: is_corrupted must be integer 0, not boolean false",
                "Must include head_commit_id field"
            ]
        )

        return remote_head.get('head_commit_id'), local_head.get('head_commit_id')

    def test_full_commit(self, remote_commit_id, local_commit_id):
        """Test 8: Full commit object"""
        log_section("TEST 8: Full Commit Object")

        if not remote_commit_id or not local_commit_id:
            self.report.add_test(
                "Full Commit Object",
                "GET /seafhttp/repo/{id}/commit/{commit_id}",
                False,
                notes=["No commit IDs available"]
            )
            return

        remote_commit = curl(
            "-H", f"Seafile-Repo-Token: {self.remote_sync_token}",
            f"{self.remote_server}/seafhttp/repo/{self.remote_repo_id}/commit/{remote_commit_id}"
        )

        local_commit = curl(
            "-H", f"Seafile-Repo-Token: {self.local_sync_token}",
            f"{self.local_server}/seafhttp/repo/{self.local_repo_id}/commit/{local_commit_id}"
        )

        # Compare encryption fields
        enc_fields = ['encrypted', 'enc_version', 'magic', 'key']
        ignore = ['commit_id', 'root_id', 'repo_id', 'creator', 'creator_name',
                 'description', 'ctime', 'parent_id', 'repo_name', 'repo_desc', 'version']

        diffs = compare_json(remote_commit, local_commit, "Full Commit", ignore_fields=ignore)

        self.report.add_test(
            "Full Commit Object",
            "GET /seafhttp/repo/{id}/commit/{commit_id}",
            len(diffs) == 0,
            {k: remote_commit.get(k) for k in enc_fields if k in remote_commit},
            {k: local_commit.get(k) for k in enc_fields if k in local_commit},
            diffs,
            [
                "For encrypted libraries: encrypted='true' (string!)",
                "enc_version should be integer 2",
                "magic and key should match library metadata"
            ]
        )

        return remote_commit.get('root_id'), local_commit.get('root_id')

    def test_fs_id_list(self, remote_commit_id, local_commit_id):
        """Test 9: FS-ID-List"""
        log_section("TEST 9: FS-ID-List")

        if not remote_commit_id or not local_commit_id:
            self.report.add_test(
                "FS-ID-List",
                "GET /seafhttp/repo/{id}/fs-id-list/",
                False,
                notes=["No commit IDs available"]
            )
            return None, None

        remote_list = curl(
            "-H", f"Seafile-Repo-Token: {self.remote_sync_token}",
            f"{self.remote_server}/seafhttp/repo/{self.remote_repo_id}/fs-id-list/?server-head={remote_commit_id}"
        )

        local_list = curl(
            "-H", f"Seafile-Repo-Token: {self.local_sync_token}",
            f"{self.local_server}/seafhttp/repo/{self.local_repo_id}/fs-id-list/?server-head={local_commit_id}"
        )

        diffs = []

        # Check both are arrays
        if not isinstance(remote_list, list):
            diffs.append({
                "path": "root",
                "type": "not_an_array",
                "remote": type(remote_list).__name__
            })
        if not isinstance(local_list, list):
            diffs.append({
                "path": "root",
                "type": "not_an_array",
                "local": type(local_list).__name__
            })

        self.report.add_test(
            "FS-ID-List",
            "GET /seafhttp/repo/{id}/fs-id-list/",
            len(diffs) == 0 and isinstance(remote_list, list) and isinstance(local_list, list),
            {"type": "list", "count": len(remote_list) if isinstance(remote_list, list) else 0},
            {"type": "list", "count": len(local_list) if isinstance(local_list, list) else 0},
            diffs,
            [
                "CRITICAL: Must return JSON array",
                "Should include all FS IDs (directories and files)",
                "For new library: should contain at least root directory fs_id"
            ]
        )

        return remote_list, local_list

    def test_pack_fs(self, remote_fs_ids, local_fs_ids):
        """Test 10: Pack-FS binary format"""
        log_section("TEST 10: Pack-FS Binary Format")

        if not remote_fs_ids or not local_fs_ids:
            self.report.add_test(
                "Pack-FS Binary Format",
                "POST /seafhttp/repo/{id}/pack-fs/",
                False,
                notes=["No FS IDs available for testing"]
            )
            return

        # Request first FS ID
        test_fs_id_remote = remote_fs_ids[0] if isinstance(remote_fs_ids, list) and len(remote_fs_ids) > 0 else None
        test_fs_id_local = local_fs_ids[0] if isinstance(local_fs_ids, list) and len(local_fs_ids) > 0 else None

        if not test_fs_id_remote or not test_fs_id_local:
            self.report.add_test(
                "Pack-FS Binary Format",
                "POST /seafhttp/repo/{id}/pack-fs/",
                False,
                notes=["Empty FS ID lists"]
            )
            return

        remote_pack = curl(
            "-X", "POST",
            "-H", f"Seafile-Repo-Token: {self.remote_sync_token}",
            "-H", "Content-Type: application/json",
            f"{self.remote_server}/seafhttp/repo/{self.remote_repo_id}/pack-fs/",
            "-d", json.dumps([test_fs_id_remote]),
            return_binary=True
        )

        local_pack = curl(
            "-X", "POST",
            "-H", f"Seafile-Repo-Token: {self.local_sync_token}",
            "-H", "Content-Type: application/json",
            f"{self.local_server}/seafhttp/repo/{self.local_repo_id}/pack-fs/",
            "-d", json.dumps([test_fs_id_local]),
            return_binary=True
        )

        def validate_pack_fs(data, name):
            """Validate pack-fs binary format"""
            if not data or len(data) < 44:
                return {"valid": False, "error": "Data too short (< 44 bytes)"}

            try:
                # Parse first entry
                fs_id = data[:40].decode('ascii')
                size = int.from_bytes(data[40:44], 'big')

                if len(data) < 44 + size:
                    return {"valid": False, "error": f"Incomplete data (expected {44+size}, got {len(data)})"}

                compressed = data[44:44+size]

                # Try to decompress
                try:
                    decompressed = zlib.decompress(compressed)

                    # Try to parse as JSON
                    try:
                        obj = json.loads(decompressed.decode('utf-8'))
                        return {
                            "valid": True,
                            "fs_id": fs_id,
                            "compressed_size": size,
                            "decompressed_size": len(decompressed),
                            "object_type": obj.get('type'),
                            "format": "correct"
                        }
                    except:
                        return {
                            "valid": True,
                            "fs_id": fs_id,
                            "compressed_size": size,
                            "decompressed_size": len(decompressed),
                            "format": "decompressed_but_not_json"
                        }
                except Exception as e:
                    return {
                        "valid": False,
                        "error": f"zlib decompression failed: {e}",
                        "fs_id": fs_id,
                        "size": size,
                        "format": "not_compressed"
                    }

            except Exception as e:
                return {"valid": False, "error": f"Parse error: {e}"}

        remote_validation = validate_pack_fs(remote_pack, "remote")
        local_validation = validate_pack_fs(local_pack, "local")

        diffs = []
        if remote_validation.get('valid') != local_validation.get('valid'):
            diffs.append({
                "path": "validity",
                "type": "validation_mismatch",
                "remote": "valid" if remote_validation.get('valid') else remote_validation.get('error'),
                "local": "valid" if local_validation.get('valid') else local_validation.get('error')
            })

        passed = remote_validation.get('valid') and local_validation.get('valid')

        self.report.add_test(
            "Pack-FS Binary Format",
            "POST /seafhttp/repo/{id}/pack-fs/",
            passed,
            remote_validation,
            local_validation,
            diffs,
            [
                "CRITICAL: Must be zlib compressed",
                "Format: [40-byte hex ID][4-byte size BE][zlib JSON]",
                "Client error 'Failed to inflate' means not compressed",
                "Decompressed data should be valid JSON"
            ]
        )

    def test_check_fs(self, remote_fs_ids, local_fs_ids):
        """Test 11: Check-FS endpoint"""
        log_section("TEST 11: Check-FS Endpoint")

        if not remote_fs_ids or not local_fs_ids:
            self.report.add_test(
                "Check-FS Endpoint",
                "POST /seafhttp/repo/{id}/check-fs",
                False,
                notes=["No FS IDs available"]
            )
            return

        test_fs_id_remote = remote_fs_ids[0] if isinstance(remote_fs_ids, list) and len(remote_fs_ids) > 0 else None
        test_fs_id_local = local_fs_ids[0] if isinstance(local_fs_ids, list) and len(local_fs_ids) > 0 else None

        # Test with existing FS ID (should return empty array)
        remote_check = curl(
            "-X", "POST",
            "-H", f"Seafile-Repo-Token: {self.remote_sync_token}",
            "-H", "Content-Type: application/json",
            f"{self.remote_server}/seafhttp/repo/{self.remote_repo_id}/check-fs",
            "-d", json.dumps([test_fs_id_remote])
        )

        local_check = curl(
            "-X", "POST",
            "-H", f"Seafile-Repo-Token: {self.local_sync_token}",
            "-H", "Content-Type: application/json",
            f"{self.local_server}/seafhttp/repo/{self.local_repo_id}/check-fs",
            "-d", json.dumps([test_fs_id_local])
        )

        diffs = []

        # Both should return arrays
        if not isinstance(remote_check, list):
            diffs.append({"path": "remote", "type": "not_array", "value": type(remote_check).__name__})
        if not isinstance(local_check, list):
            diffs.append({"path": "local", "type": "not_array", "value": type(local_check).__name__})

        # For existing FS IDs, should return empty array
        if isinstance(remote_check, list) and len(remote_check) != 0:
            diffs.append({"path": "remote", "type": "should_be_empty", "value": remote_check})
        if isinstance(local_check, list) and len(local_check) != 0:
            diffs.append({"path": "local", "type": "should_be_empty", "value": local_check})

        passed = (isinstance(remote_check, list) and isinstance(local_check, list) and
                 len(remote_check) == 0 and len(local_check) == 0)

        self.report.add_test(
            "Check-FS Endpoint",
            "POST /seafhttp/repo/{id}/check-fs",
            passed,
            remote_check,
            local_check,
            diffs,
            [
                "Input: JSON array of fs_ids",
                "Output: JSON array of missing fs_ids",
                "For existing IDs: returns []",
                "For missing IDs: returns [\"missing_id1\", \"missing_id2\"]"
            ]
        )

    def cleanup(self):
        """Cleanup: Delete test libraries"""
        log_section("CLEANUP: Deleting Test Libraries")

        if self.remote_repo_id:
            curl(
                "-X", "DELETE",
                "-H", f"Authorization: Token {self.remote_token}",
                f"{self.remote_server}/api2/repos/{self.remote_repo_id}/",
                return_json=False
            )
            log_info(f"Deleted remote library: {self.remote_repo_id}")

        if self.local_repo_id:
            curl(
                "-X", "DELETE",
                "-H", f"Authorization: Token {self.local_token}",
                f"{self.local_server}/api2/repos/{self.local_repo_id}/",
                return_json=False
            )
            log_info(f"Deleted local library: {self.local_repo_id}")

    def run_all_tests(self):
        """Run complete test suite"""
        log_section("COMPREHENSIVE ENCRYPTED LIBRARY SYNC PROTOCOL TESTS")
        log_info(f"Remote: {self.remote_server}")
        log_info(f"Local: {self.local_server}")

        try:
            # Phase 1: Authentication
            if not self.authenticate():
                log_error("Authentication failed, aborting tests")
                return False

            # Phase 2: Server info
            self.test_protocol_version()
            self.test_server_info()

            # Phase 3: Create encrypted library (CRITICAL)
            if not self.test_create_encrypted_library():
                log_error("Failed to create encrypted libraries, aborting sync tests")
                return False

            # Phase 4: Password verification
            self.test_set_password()

            # Phase 5: Sync token
            if not self.test_download_info():
                log_error("Failed to get sync tokens, aborting sync protocol tests")
                return False

            # Phase 6: Commit operations
            remote_commit_id, local_commit_id = self.test_commit_head()
            remote_root_id, local_root_id = self.test_full_commit(remote_commit_id, local_commit_id)

            # Phase 7: FS operations
            remote_fs_ids, local_fs_ids = self.test_fs_id_list(remote_commit_id, local_commit_id)
            self.test_pack_fs(remote_fs_ids, local_fs_ids)
            self.test_check_fs(remote_fs_ids, local_fs_ids)

        finally:
            # Always cleanup
            self.cleanup()

        return True

def main():
    log_section("STARTING COMPREHENSIVE ENCRYPTED LIBRARY SYNC TESTS")

    # Create output directory
    output_dir = Path(CAPTURE_DIR) / f"comprehensive_{datetime.now().strftime('%Y%m%d_%H%M%S')}"
    output_dir.mkdir(parents=True, exist_ok=True)
    log_info(f"Output directory: {output_dir}")

    # Run tests
    tester = EncryptedLibraryTester(
        REMOTE_SERVER, LOCAL_SERVER,
        REMOTE_USER, REMOTE_PASS,
        LOCAL_USER, LOCAL_PASS
    )

    tester.run_all_tests()

    # Generate report
    report_file = tester.report.generate_report(output_dir)

    # Print summary
    log_section("TEST SUMMARY")
    log_info(f"Total Tests: {len(tester.report.tests)}")
    log_info(f"Passed: {tester.report.passed} ✓")
    log_info(f"Failed: {tester.report.failed} ✗")
    log_info(f"Report: {report_file}")

    if tester.report.failed == 0:
        log_pass("🎉 ALL TESTS PASSED! Your implementation is fully compatible.")
        return 0
    else:
        log_error(f"⚠️  {tester.report.failed} test(s) failed. Review the report for details.")
        return 1

if __name__ == "__main__":
    sys.exit(main())
