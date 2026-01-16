#!/usr/bin/env python3
"""
Seafile Protocol Comparison Tool for Encrypted Libraries

Runs identical operations against both reference Seafile server and local SesameFS,
captures all traffic, and generates detailed diff reports.

Usage:
    python3 compare_protocol.py --test <test_name>
    python3 compare_protocol.py --test all
"""

import os
import sys
import json
import subprocess
import time
import argparse
import hashlib
from datetime import datetime
from pathlib import Path
from typing import Dict, List, Tuple, Optional

# Configuration
REMOTE_SERVER = os.environ.get("REMOTE_SERVER", "https://app.nihaoconsult.com")
LOCAL_SERVER = os.environ.get("LOCAL_SERVER", "http://host.docker.internal:8080")
REMOTE_USER = os.environ.get("REMOTE_USER", "abel.aguzmans@gmail.com")
REMOTE_PASS = os.environ.get("REMOTE_PASS", "Qwerty123!")
LOCAL_USER = os.environ.get("LOCAL_USER", "test@example.com")
LOCAL_PASS = os.environ.get("LOCAL_PASS", "testpass")

CAPTURE_DIR = os.environ.get("CAPTURE_DIR", "/captures")
PROXY_PORT = 8888

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

class ProxyCapture:
    """Manages mitmproxy for capturing traffic"""

    def __init__(self, session_name: str):
        self.session_name = session_name
        self.session_dir = Path(CAPTURE_DIR) / f"compare_{session_name}_{datetime.now().strftime('%Y%m%d_%H%M%S')}"
        self.session_dir.mkdir(parents=True, exist_ok=True)
        self.pid_file = self.session_dir / "mitm.pid"
        self.proc = None

    def start(self):
        """Start mitmproxy"""
        log_info(f"Starting mitmproxy for session: {self.session_name}")

        # Create addon script that saves to our session dir
        addon_script = self.session_dir / "addon.py"
        addon_script.write_text(f"""
import os
os.environ['CAPTURE_DIR'] = '{self.session_dir}'
exec(open('/usr/local/bin/capture_addon.py').read())
""")

        cmd = [
            "python3", "-m", "mitmproxy.tools.mitmdump",
            "--listen-port", str(PROXY_PORT),
            "--set", f"confdir=/mitmproxy",
            "-s", str(addon_script),
            "--ssl-insecure",
            "--showhost"
        ]

        log_file = self.session_dir / "mitm.log"
        with open(log_file, 'w') as f:
            self.proc = subprocess.Popen(cmd, stdout=f, stderr=subprocess.STDOUT)

        self.pid_file.write_text(str(self.proc.pid))
        time.sleep(2)
        log_info(f"mitmproxy started (PID: {self.proc.pid})")

    def stop(self):
        """Stop mitmproxy"""
        if self.proc:
            log_info("Stopping mitmproxy...")
            self.proc.terminate()
            try:
                self.proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.proc.kill()

    def get_captures(self) -> List[Dict]:
        """Load all captured requests from this session"""
        captures = []
        for json_file in sorted(self.session_dir.glob("*.json")):
            if "session_summary" in json_file.name:
                continue
            try:
                with open(json_file) as f:
                    captures.append(json.load(f))
            except Exception as e:
                log_warn(f"Failed to load {json_file}: {e}")
        return captures


class ServerClient:
    """Client for making requests to a Seafile-compatible server"""

    def __init__(self, base_url: str, username: str, password: str, use_proxy: bool = False):
        self.base_url = base_url.rstrip('/')
        self.username = username
        self.password = password
        self.use_proxy = use_proxy
        self.api_token = None

    def _curl(self, *args, return_json=True, return_headers=False, check_status=True):
        """Execute curl command"""
        cmd = ["curl", "-s"]

        if self.use_proxy:
            cmd.extend(["--proxy", f"http://127.0.0.1:{PROXY_PORT}", "--proxy-insecure", "-k"])

        if return_headers:
            cmd.append("-i")

        cmd.extend(args)

        result = subprocess.run(cmd, capture_output=True, text=True)

        if result.returncode != 0:
            log_error(f"curl failed: {result.stderr}")
            return None

        output = result.stdout

        if return_headers:
            # Split headers and body
            parts = output.split('\r\n\r\n', 1)
            headers = parts[0]
            body = parts[1] if len(parts) > 1 else ''

            if check_status and 'HTTP/' in headers:
                status_line = headers.split('\r\n')[0]
                if ' 4' in status_line or ' 5' in status_line:
                    log_error(f"HTTP error: {status_line}")
                    log_error(f"Body: {body[:200]}")

            if return_json:
                try:
                    return json.loads(body) if body else None
                except:
                    return body
            return body

        if return_json:
            try:
                return json.loads(output) if output else None
            except:
                return output
        return output

    def get_auth_token(self) -> Optional[str]:
        """Get API token"""
        log_info(f"Authenticating to {self.base_url}...")

        result = self._curl(
            "-X", "POST",
            f"{self.base_url}/api2/auth-token/",
            "--data-urlencode", f"username={self.username}",
            "--data-urlencode", f"password={self.password}"
        )

        if result and isinstance(result, dict):
            self.api_token = result.get('token')
            log_info(f"Got token: {self.api_token[:20]}...")
            return self.api_token

        log_error(f"Failed to get token: {result}")
        return None

    def create_encrypted_library(self, name: str, lib_password: str) -> Optional[Dict]:
        """Create an encrypted library"""
        log_info(f"Creating encrypted library '{name}'...")

        result = self._curl(
            "-X", "POST",
            "-H", f"Authorization: Token {self.api_token}",
            f"{self.base_url}/api2/repos/",
            "--data-urlencode", f"name={name}",
            "--data-urlencode", "desc=Test encrypted library",
            "--data-urlencode", f"passwd={lib_password}"
        )

        if result and isinstance(result, dict) and 'repo_id' in result:
            log_info(f"Created library: {result['repo_id']}")
            return result

        log_error(f"Failed to create library: {result}")
        return None

    def set_library_password(self, repo_id: str, lib_password: str) -> bool:
        """Set/verify password for encrypted library"""
        log_info(f"Setting password for library {repo_id}...")

        result = self._curl(
            "-X", "POST",
            "-H", f"Authorization: Token {self.api_token}",
            "-H", "Content-Type: application/json",
            f"{self.base_url}/api/v2.1/repos/{repo_id}/set-password/",
            "-d", json.dumps({"password": lib_password})
        )

        if result and isinstance(result, dict):
            success = result.get('success', False)
            log_info(f"Password set: {success}")
            return success

        log_error(f"Failed to set password: {result}")
        return False

    def get_download_info(self, repo_id: str) -> Optional[Dict]:
        """Get sync token and library metadata"""
        log_info(f"Getting download-info for {repo_id}...")

        result = self._curl(
            "-H", f"Authorization: Token {self.api_token}",
            f"{self.base_url}/api2/repos/{repo_id}/download-info/"
        )

        return result

    def get_commit_head(self, repo_id: str, sync_token: str) -> Optional[Dict]:
        """Get HEAD commit"""
        log_info(f"Getting HEAD commit...")

        result = self._curl(
            "-H", f"Seafile-Repo-Token: {sync_token}",
            f"{self.base_url}/seafhttp/repo/{repo_id}/commit/HEAD"
        )

        return result

    def get_commit(self, repo_id: str, commit_id: str, sync_token: str) -> Optional[Dict]:
        """Get specific commit"""
        log_info(f"Getting commit {commit_id[:12]}...")

        result = self._curl(
            "-H", f"Seafile-Repo-Token: {sync_token}",
            f"{self.base_url}/seafhttp/repo/{repo_id}/commit/{commit_id}"
        )

        return result

    def delete_library(self, repo_id: str) -> bool:
        """Delete library"""
        log_info(f"Deleting library {repo_id}...")

        result = self._curl(
            "-X", "DELETE",
            "-H", f"Authorization: Token {self.api_token}",
            f"{self.base_url}/api2/repos/{repo_id}/",
            return_json=False
        )

        return "success" in result.lower() if result else False


class ProtocolComparator:
    """Compares protocol implementations between two servers"""

    def __init__(self, output_dir: Path):
        self.output_dir = output_dir
        self.output_dir.mkdir(parents=True, exist_ok=True)
        self.diffs = []

    def compare_json_responses(self, remote: Dict, local: Dict, context: str) -> List[str]:
        """Compare two JSON responses and return list of differences"""
        differences = []

        def compare_recursive(r, l, path=""):
            if type(r) != type(l):
                differences.append(f"{path}: Type mismatch - remote={type(r).__name__}, local={type(l).__name__}")
                return

            if isinstance(r, dict):
                all_keys = set(r.keys()) | set(l.keys())
                for key in all_keys:
                    new_path = f"{path}.{key}" if path else key

                    if key not in r:
                        differences.append(f"{new_path}: Missing in remote")
                    elif key not in l:
                        differences.append(f"{new_path}: Missing in local")
                    else:
                        compare_recursive(r[key], l[key], new_path)

            elif isinstance(r, list):
                if len(r) != len(l):
                    differences.append(f"{path}: Array length mismatch - remote={len(r)}, local={len(l)}")
                else:
                    for i, (r_item, l_item) in enumerate(zip(r, l)):
                        compare_recursive(r_item, l_item, f"{path}[{i}]")

            else:
                # Leaf value - compare with special handling for known differences
                if r != l:
                    # Ignore token/ID differences
                    if 'token' not in path.lower() and 'id' not in path.lower() and 'time' not in path.lower():
                        differences.append(f"{path}: Value mismatch - remote={repr(r)}, local={repr(l)}")

        compare_recursive(remote, local)

        if differences:
            log_diff(f"{context}: Found {len(differences)} differences")
            for diff in differences:
                log_diff(f"  - {diff}")
        else:
            log_info(f"{context}: Responses match ✓")

        return differences

    def record_diff(self, test_name: str, endpoint: str, remote_data: any, local_data: any, diffs: List[str]):
        """Record a diff for reporting"""
        self.diffs.append({
            "test": test_name,
            "endpoint": endpoint,
            "timestamp": datetime.now().isoformat(),
            "remote": remote_data,
            "local": local_data,
            "differences": diffs
        })

    def generate_report(self) -> str:
        """Generate markdown diff report"""
        if not self.diffs:
            return "# Protocol Comparison Report\n\n**No differences found!** ✓\n"

        md = ["# Seafile Protocol Comparison Report", ""]
        md.append(f"*Generated: {datetime.now().isoformat()}*")
        md.append("")
        md.append(f"**Total Issues Found:** {len(self.diffs)}")
        md.append("")

        # Group by test
        by_test = {}
        for diff in self.diffs:
            test = diff['test']
            if test not in by_test:
                by_test[test] = []
            by_test[test].append(diff)

        # Table of contents
        md.append("## Table of Contents")
        md.append("")
        for test in sorted(by_test.keys()):
            md.append(f"- [{test}](#{test.lower().replace(' ', '-')})")
        md.append("")

        # Details for each test
        for test in sorted(by_test.keys()):
            md.append(f"## {test}")
            md.append("")

            for diff in by_test[test]:
                md.append(f"### {diff['endpoint']}")
                md.append("")
                md.append(f"**Timestamp:** {diff['timestamp']}")
                md.append("")

                if diff['differences']:
                    md.append("**Differences:**")
                    md.append("")
                    for d in diff['differences']:
                        md.append(f"- {d}")
                    md.append("")

                md.append("**Remote Response:**")
                md.append("```json")
                md.append(json.dumps(diff['remote'], indent=2))
                md.append("```")
                md.append("")

                md.append("**Local Response:**")
                md.append("```json")
                md.append(json.dumps(diff['local'], indent=2))
                md.append("```")
                md.append("")
                md.append("---")
                md.append("")

        return "\n".join(md)

    def save_report(self):
        """Save report to file"""
        report = self.generate_report()
        report_file = self.output_dir / "COMPARISON_REPORT.md"
        report_file.write_text(report)
        log_info(f"Report saved to: {report_file}")

        # Also save raw diff data
        diff_file = self.output_dir / "diffs.json"
        diff_file.write_text(json.dumps(self.diffs, indent=2))

        return report_file


class EncryptedLibraryTest:
    """Test suite for encrypted library operations"""

    def __init__(self, remote_client: ServerClient, local_client: ServerClient, comparator: ProtocolComparator):
        self.remote = remote_client
        self.local = local_client
        self.comparator = comparator
        self.lib_password = "SecureLibPassword123!"
        self.test_file_content = b"This is test content for encrypted library validation"

    def test_create_encrypted_library(self) -> Tuple[Optional[str], Optional[str]]:
        """Test: Create encrypted library on both servers"""
        log_section("TEST: Create Encrypted Library")

        lib_name = f"EncTest_{int(time.time())}"

        # Remote
        remote_capture = ProxyCapture("remote_create_enc_lib")
        remote_capture.start()
        self.remote.use_proxy = True

        remote_result = self.remote.create_encrypted_library(lib_name, self.lib_password)

        remote_capture.stop()
        self.remote.use_proxy = False

        # Local
        local_capture = ProxyCapture("local_create_enc_lib")
        local_capture.start()
        self.local.use_proxy = True

        local_result = self.local.create_encrypted_library(lib_name, self.lib_password)

        local_capture.stop()
        self.local.use_proxy = False

        # Compare
        if remote_result and local_result:
            # Compare encryption metadata
            fields_to_compare = ['encrypted', 'enc_version', 'magic', 'random_key']
            remote_enc = {k: remote_result.get(k) for k in fields_to_compare}
            local_enc = {k: local_result.get(k) for k in fields_to_compare}

            diffs = self.comparator.compare_json_responses(
                remote_enc, local_enc,
                "Create Encrypted Library - Encryption Fields"
            )

            if diffs:
                self.comparator.record_diff(
                    "Create Encrypted Library",
                    "POST /api2/repos/ (encrypted)",
                    remote_result,
                    local_result,
                    diffs
                )

            return remote_result.get('repo_id'), local_result.get('repo_id')

        log_error("Failed to create encrypted library on one or both servers")
        return None, None

    def test_set_password(self, remote_repo_id: str, local_repo_id: str):
        """Test: Set/verify library password"""
        log_section("TEST: Set/Verify Library Password")

        # Remote
        remote_capture = ProxyCapture("remote_set_password")
        remote_capture.start()
        self.remote.use_proxy = True

        remote_success = self.remote.set_library_password(remote_repo_id, self.lib_password)

        remote_capture.stop()
        self.remote.use_proxy = False

        # Local
        local_capture = ProxyCapture("local_set_password")
        local_capture.start()
        self.local.use_proxy = True

        local_success = self.local.set_library_password(local_repo_id, self.lib_password)

        local_capture.stop()
        self.local.use_proxy = False

        if remote_success != local_success:
            self.comparator.record_diff(
                "Set Library Password",
                "POST /api/v2.1/repos/{id}/set-password/",
                {"success": remote_success},
                {"success": local_success},
                [f"Password verification result mismatch: remote={remote_success}, local={local_success}"]
            )

    def test_download_info(self, remote_repo_id: str, local_repo_id: str) -> Tuple[Optional[Dict], Optional[Dict]]:
        """Test: Get download-info (sync token)"""
        log_section("TEST: Get Download Info")

        # Remote
        remote_capture = ProxyCapture("remote_download_info")
        remote_capture.start()
        self.remote.use_proxy = True

        remote_info = self.remote.get_download_info(remote_repo_id)

        remote_capture.stop()
        self.remote.use_proxy = False

        # Local
        local_capture = ProxyCapture("local_download_info")
        local_capture.start()
        self.local.use_proxy = True

        local_info = self.local.get_download_info(local_repo_id)

        local_capture.stop()
        self.local.use_proxy = False

        # Compare encryption metadata
        if remote_info and local_info:
            fields = ['encrypted', 'enc_version', 'magic', 'random_key', 'salt']
            remote_meta = {k: remote_info.get(k) for k in fields}
            local_meta = {k: local_info.get(k) for k in fields}

            diffs = self.comparator.compare_json_responses(
                remote_meta, local_meta,
                "Download Info - Encryption Metadata"
            )

            if diffs:
                self.comparator.record_diff(
                    "Download Info",
                    f"GET /api2/repos/{remote_repo_id}/download-info/",
                    remote_info,
                    local_info,
                    diffs
                )

        return remote_info, local_info

    def test_commit_head(self, remote_repo_id: str, local_repo_id: str,
                        remote_token: str, local_token: str):
        """Test: Get HEAD commit"""
        log_section("TEST: Get HEAD Commit")

        # Remote
        remote_capture = ProxyCapture("remote_commit_head")
        remote_capture.start()
        self.remote.use_proxy = True

        remote_head = self.remote.get_commit_head(remote_repo_id, remote_token)

        remote_capture.stop()
        self.remote.use_proxy = False

        # Local
        local_capture = ProxyCapture("local_commit_head")
        local_capture.start()
        self.local.use_proxy = True

        local_head = self.local.get_commit_head(local_repo_id, local_token)

        local_capture.stop()
        self.local.use_proxy = False

        # Compare structure (not commit IDs)
        if remote_head and local_head:
            # Check field existence and types
            remote_fields = {k: type(v).__name__ for k, v in remote_head.items()}
            local_fields = {k: type(v).__name__ for k, v in local_head.items()}

            diffs = self.comparator.compare_json_responses(
                remote_fields, local_fields,
                "Commit HEAD - Field Types"
            )

            if diffs:
                self.comparator.record_diff(
                    "Get HEAD Commit",
                    f"GET /seafhttp/repo/{remote_repo_id}/commit/HEAD",
                    remote_head,
                    local_head,
                    diffs
                )

        return remote_head, local_head

    def test_full_commit(self, remote_repo_id: str, local_repo_id: str,
                        remote_token: str, local_token: str,
                        remote_commit_id: str, local_commit_id: str):
        """Test: Get full commit object"""
        log_section("TEST: Get Full Commit Object")

        # Remote
        remote_capture = ProxyCapture("remote_full_commit")
        remote_capture.start()
        self.remote.use_proxy = True

        remote_commit = self.remote.get_commit(remote_repo_id, remote_commit_id, remote_token)

        remote_capture.stop()
        self.remote.use_proxy = False

        # Local
        local_capture = ProxyCapture("local_full_commit")
        local_capture.start()
        self.local.use_proxy = True

        local_commit = self.local.get_commit(local_repo_id, local_commit_id, local_token)

        local_capture.stop()
        self.local.use_proxy = False

        # Compare encryption fields
        if remote_commit and local_commit:
            enc_fields = ['encrypted', 'enc_version', 'magic', 'key']
            remote_enc = {k: remote_commit.get(k) for k in enc_fields if k in remote_commit}
            local_enc = {k: local_commit.get(k) for k in enc_fields if k in local_commit}

            diffs = self.comparator.compare_json_responses(
                remote_enc, local_enc,
                "Full Commit - Encryption Fields"
            )

            if diffs:
                self.comparator.record_diff(
                    "Get Full Commit",
                    f"GET /seafhttp/repo/{remote_repo_id}/commit/{remote_commit_id}",
                    remote_commit,
                    local_commit,
                    diffs
                )

    def test_fs_id_list(self, remote_repo_id: str, local_repo_id: str,
                       remote_token: str, local_token: str,
                       remote_commit_id: str, local_commit_id: str):
        """Test: Get fs-id-list"""
        log_section("TEST: Get FS-ID-List")

        # Remote
        remote_capture = ProxyCapture("remote_fs_id_list")
        remote_capture.start()
        self.remote.use_proxy = True

        remote_list = self.remote._curl(
            "-H", f"Seafile-Repo-Token: {remote_token}",
            f"{self.remote.base_url}/seafhttp/repo/{remote_repo_id}/fs-id-list/?server-head={remote_commit_id}"
        )

        remote_capture.stop()
        self.remote.use_proxy = False

        # Local
        local_capture = ProxyCapture("local_fs_id_list")
        local_capture.start()
        self.local.use_proxy = True

        local_list = self.local._curl(
            "-H", f"Seafile-Repo-Token: {local_token}",
            f"{self.local.base_url}/seafhttp/repo/{local_repo_id}/fs-id-list/?server-head={local_commit_id}"
        )

        local_capture.stop()
        self.local.use_proxy = False

        # Compare response types (both should be JSON arrays)
        if remote_list and local_list:
            remote_is_array = isinstance(remote_list, list)
            local_is_array = isinstance(local_list, list)

            if remote_is_array != local_is_array:
                self.comparator.record_diff(
                    "Get FS-ID-List",
                    f"GET /seafhttp/repo/{remote_repo_id}/fs-id-list/",
                    {"type": "list" if remote_is_array else type(remote_list).__name__, "data": remote_list},
                    {"type": "list" if local_is_array else type(local_list).__name__, "data": local_list},
                    [f"Response type mismatch: remote={'array' if remote_is_array else 'not array'}, local={'array' if local_is_array else 'not array'}"]
                )

        return remote_list, local_list

    def test_pack_fs_format(self, remote_repo_id: str, local_repo_id: str,
                           remote_token: str, local_token: str,
                           fs_ids: List[str]):
        """Test: pack-fs binary format"""
        log_section("TEST: Pack-FS Binary Format")

        if not fs_ids:
            log_warn("No fs_ids to test pack-fs")
            return

        test_fs_id = fs_ids[0] if isinstance(fs_ids, list) else fs_ids

        # Remote
        remote_capture = ProxyCapture("remote_pack_fs")
        remote_capture.start()
        self.remote.use_proxy = True

        remote_data = self.remote._curl(
            "-X", "POST",
            "-H", f"Seafile-Repo-Token: {remote_token}",
            "-H", "Content-Type: application/json",
            f"{self.remote.base_url}/seafhttp/repo/{remote_repo_id}/pack-fs/",
            "-d", json.dumps([test_fs_id]),
            return_json=False
        )

        remote_capture.stop()
        self.remote.use_proxy = False

        # Local
        local_capture = ProxyCapture("local_pack_fs")
        local_capture.start()
        self.local.use_proxy = True

        local_data = self.local._curl(
            "-X", "POST",
            "-H", f"Seafile-Repo-Token: {local_token}",
            "-H", "Content-Type: application/json",
            f"{self.local.base_url}/seafhttp/repo/{local_repo_id}/pack-fs/",
            "-d", json.dumps([test_fs_id]),
            return_json=False
        )

        local_capture.stop()
        self.local.use_proxy = False

        # Analyze binary format
        def analyze_pack_fs(data):
            if not data or len(data) < 44:
                return {"error": "Data too short"}

            try:
                # First entry
                fs_id = data[:40]
                size_bytes = data[40:44]
                size = int.from_bytes(size_bytes, 'big') if isinstance(size_bytes, bytes) else 0

                # Try to decompress
                import zlib
                if len(data) >= 44 + size:
                    compressed = data[44:44+size]
                    try:
                        decompressed = zlib.decompress(compressed)
                        return {
                            "fs_id": fs_id if isinstance(fs_id, str) else fs_id.decode('ascii', errors='replace'),
                            "compressed_size": size,
                            "decompressed_size": len(decompressed),
                            "format": "valid_pack_fs"
                        }
                    except Exception as e:
                        return {
                            "fs_id": fs_id if isinstance(fs_id, str) else "invalid",
                            "compressed_size": size,
                            "error": f"decompression_failed: {e}",
                            "format": "invalid_compression"
                        }

                return {"error": "incomplete_data"}
            except Exception as e:
                return {"error": f"parse_failed: {e}"}

        remote_analysis = analyze_pack_fs(remote_data.encode('latin1') if isinstance(remote_data, str) else remote_data)
        local_analysis = analyze_pack_fs(local_data.encode('latin1') if isinstance(local_data, str) else local_data)

        # Compare formats
        diffs = []
        if remote_analysis.get('format') != local_analysis.get('format'):
            diffs.append(f"Format mismatch: remote={remote_analysis.get('format')}, local={local_analysis.get('format')}")

        if 'error' in remote_analysis or 'error' in local_analysis:
            if remote_analysis.get('error') != local_analysis.get('error'):
                diffs.append(f"Error mismatch: remote={remote_analysis.get('error')}, local={local_analysis.get('error')}")

        if diffs:
            self.comparator.record_diff(
                "Pack-FS Binary Format",
                f"POST /seafhttp/repo/{remote_repo_id}/pack-fs/",
                remote_analysis,
                local_analysis,
                diffs
            )

    def test_check_fs(self, remote_repo_id: str, local_repo_id: str,
                     remote_token: str, local_token: str,
                     test_fs_id: str):
        """Test: check-fs endpoint"""
        log_section("TEST: Check-FS Endpoint")

        # Test with existing fs_id (should return empty array)
        # Remote
        remote_capture = ProxyCapture("remote_check_fs_exists")
        remote_capture.start()
        self.remote.use_proxy = True

        remote_result = self.remote._curl(
            "-X", "POST",
            "-H", f"Seafile-Repo-Token: {remote_token}",
            "-H", "Content-Type: application/json",
            f"{self.remote.base_url}/seafhttp/repo/{remote_repo_id}/check-fs",
            "-d", json.dumps([test_fs_id])
        )

        remote_capture.stop()
        self.remote.use_proxy = False

        # Local
        local_capture = ProxyCapture("local_check_fs_exists")
        local_capture.start()
        self.local.use_proxy = True

        local_result = self.local._curl(
            "-X", "POST",
            "-H", f"Seafile-Repo-Token: {local_token}",
            "-H", "Content-Type: application/json",
            f"{self.local.base_url}/seafhttp/repo/{local_repo_id}/check-fs",
            "-d", json.dumps([test_fs_id])
        )

        local_capture.stop()
        self.local.use_proxy = False

        # Compare
        if remote_result != local_result:
            self.comparator.record_diff(
                "Check-FS Endpoint",
                f"POST /seafhttp/repo/{remote_repo_id}/check-fs",
                remote_result,
                local_result,
                [f"Response mismatch: remote={remote_result}, local={local_result}"]
            )

    def test_protocol_version(self):
        """Test: Protocol version endpoint"""
        log_section("TEST: Protocol Version")

        # Remote
        remote_capture = ProxyCapture("remote_protocol_version")
        remote_capture.start()
        self.remote.use_proxy = True

        remote_version = self.remote._curl(
            f"{self.remote.base_url}/seafhttp/protocol-version"
        )

        remote_capture.stop()
        self.remote.use_proxy = False

        # Local
        local_capture = ProxyCapture("local_protocol_version")
        local_capture.start()
        self.local.use_proxy = True

        local_version = self.local._curl(
            f"{self.local.base_url}/seafhttp/protocol-version"
        )

        local_capture.stop()
        self.local.use_proxy = False

        # Compare
        if remote_version and local_version:
            diffs = self.comparator.compare_json_responses(
                remote_version, local_version,
                "Protocol Version"
            )

            if diffs:
                self.comparator.record_diff(
                    "Protocol Version",
                    "GET /seafhttp/protocol-version",
                    remote_version,
                    local_version,
                    diffs
                )

    def cleanup(self, remote_repo_id: Optional[str], local_repo_id: Optional[str]):
        """Cleanup: Delete test libraries"""
        log_section("CLEANUP: Deleting Test Libraries")

        if remote_repo_id:
            self.remote.delete_library(remote_repo_id)

        if local_repo_id:
            self.local.delete_library(local_repo_id)

    def run_all_tests(self):
        """Run complete test suite"""
        log_section("ENCRYPTED LIBRARY PROTOCOL COMPARISON")
        log_info(f"Remote: {self.remote.base_url}")
        log_info(f"Local: {self.local.base_url}")

        # Test 0: Protocol version
        self.test_protocol_version()

        # Authenticate
        if not self.remote.get_auth_token():
            log_error("Failed to authenticate to remote server")
            return False

        if not self.local.get_auth_token():
            log_error("Failed to authenticate to local server")
            return False

        remote_repo_id = None
        local_repo_id = None

        try:
            # Test 1: Create encrypted library
            remote_repo_id, local_repo_id = self.test_create_encrypted_library()

            if not remote_repo_id or not local_repo_id:
                log_error("Failed to create libraries, aborting tests")
                return False

            # Test 2: Set password
            self.test_set_password(remote_repo_id, local_repo_id)

            # Test 3: Download info
            remote_info, local_info = self.test_download_info(remote_repo_id, local_repo_id)

            if not remote_info or not local_info:
                log_error("Failed to get download info")
                return False

            remote_sync_token = remote_info.get('token')
            local_sync_token = local_info.get('token')

            # Test 4: HEAD commit
            remote_head, local_head = self.test_commit_head(
                remote_repo_id, local_repo_id,
                remote_sync_token, local_sync_token
            )

            if remote_head and local_head:
                remote_commit_id = remote_head.get('head_commit_id')
                local_commit_id = local_head.get('head_commit_id')

                # Test 5: Full commit
                if remote_commit_id and local_commit_id:
                    self.test_full_commit(
                        remote_repo_id, local_repo_id,
                        remote_sync_token, local_sync_token,
                        remote_commit_id, local_commit_id
                    )

                    # Test 6: fs-id-list
                    remote_fs_list, local_fs_list = self.test_fs_id_list(
                        remote_repo_id, local_repo_id,
                        remote_sync_token, local_sync_token,
                        remote_commit_id, local_commit_id
                    )

                    # Test 7: pack-fs format
                    if remote_fs_list and isinstance(remote_fs_list, list) and len(remote_fs_list) > 0:
                        self.test_pack_fs_format(
                            remote_repo_id, local_repo_id,
                            remote_sync_token, local_sync_token,
                            remote_fs_list
                        )

                        # Test 8: check-fs
                        self.test_check_fs(
                            remote_repo_id, local_repo_id,
                            remote_sync_token, local_sync_token,
                            remote_fs_list[0]
                        )

        finally:
            # Cleanup
            self.cleanup(remote_repo_id, local_repo_id)

        return True


def main():
    parser = argparse.ArgumentParser(description="Compare Seafile protocol implementations")
    parser.add_argument('--test', default='all', help='Test name to run (all, create, password, sync)')
    parser.add_argument('--remote', default=REMOTE_SERVER, help='Remote server URL')
    parser.add_argument('--local', default=LOCAL_SERVER, help='Local server URL')
    parser.add_argument('--output', default=None, help='Output directory for reports')

    args = parser.parse_args()

    # Setup output directory
    if args.output:
        output_dir = Path(args.output)
    else:
        output_dir = Path(CAPTURE_DIR) / f"comparison_{datetime.now().strftime('%Y%m%d_%H%M%S')}"

    output_dir.mkdir(parents=True, exist_ok=True)
    log_info(f"Output directory: {output_dir}")

    # Create clients
    remote_client = ServerClient(args.remote, REMOTE_USER, REMOTE_PASS)
    local_client = ServerClient(args.local, LOCAL_USER, LOCAL_PASS)

    # Create comparator
    comparator = ProtocolComparator(output_dir)

    # Run tests
    test_suite = EncryptedLibraryTest(remote_client, local_client, comparator)
    success = test_suite.run_all_tests()

    # Generate report
    comparator.save_report()

    # Summary
    log_section("COMPARISON COMPLETE")
    if comparator.diffs:
        log_warn(f"Found {len(comparator.diffs)} differences")
        log_info(f"Review report: {output_dir / 'COMPARISON_REPORT.md'}")
        return 1
    else:
        log_info("No differences found! ✓")
        return 0


if __name__ == "__main__":
    sys.exit(main())
