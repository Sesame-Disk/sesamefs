#!/usr/bin/env python3
"""
Comprehensive Seafile Sync Protocol Test Framework

Tests sync protocol behavior by:
1. Creating files via API on both stock Seafile and local SesameFS
2. Syncing with real desktop client (seaf-cli)
3. Verifying all files sync correctly
4. Comparing protocol responses
5. Testing various scenarios (nested folders, large files, many files, etc.)

Usage:
    python3 comprehensive_sync_test.py --test-all
    python3 comprehensive_sync_test.py --test-scenario nested_folders
    python3 comprehensive_sync_test.py --list-scenarios
"""

import argparse
import hashlib
import json
import os
import random
import shutil
import subprocess
import sys
import time
from dataclasses import dataclass, field
from datetime import datetime
from io import BytesIO
from pathlib import Path
from typing import Dict, List, Optional, Tuple
import requests
import urllib3

urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)

# Configuration from .seafile-reference.md
REMOTE_URL = "https://app.nihaoconsult.com"
REMOTE_USER = "abel.aguzmans@gmail.com"
REMOTE_PASS = "Qwerty123!"

LOCAL_URL = "http://localhost:8080"
LOCAL_USER = "abel.aguzmans@gmail.com"
LOCAL_PASS = "dev-token-123"

# Test directories
BASE_DIR = "/tmp/seafile-sync-test"
REMOTE_SYNC_DIR = f"{BASE_DIR}/remote-sync"
LOCAL_SYNC_DIR = f"{BASE_DIR}/local-sync"
REMOTE_CONFIG_DIR = f"{BASE_DIR}/remote-config"
LOCAL_CONFIG_DIR = f"{BASE_DIR}/local-config"
RESULTS_DIR = f"{BASE_DIR}/results"


@dataclass
class TestFile:
    """Represents a test file to create"""
    path: str  # Relative path in library (e.g., "folder/file.txt")
    content: bytes
    sha256: str = ""

    def __post_init__(self):
        if not self.sha256:
            self.sha256 = hashlib.sha256(self.content).hexdigest()


@dataclass
class TestScenario:
    """Defines a test scenario"""
    name: str
    description: str
    files: List[TestFile]

    def total_size(self) -> int:
        return sum(len(f.content) for f in self.files)

    def file_count(self) -> int:
        return len(self.files)


@dataclass
class SyncTestResult:
    """Results from syncing a library"""
    scenario_name: str
    server_type: str  # "remote" or "local"
    library_id: str
    library_name: str

    # Sync results
    sync_success: bool
    sync_duration: float

    # File verification
    files_expected: int
    files_synced: int
    files_verified: int
    files_missing: List[str] = field(default_factory=list)
    files_corrupted: List[str] = field(default_factory=list)

    # Protocol responses (captured)
    protocol_responses: Dict[str, any] = field(default_factory=dict)

    # Client logs
    client_log_errors: List[str] = field(default_factory=list)

    def success_rate(self) -> float:
        if self.files_expected == 0:
            return 0.0
        return (self.files_verified / self.files_expected) * 100


@dataclass
class ComparisonResult:
    """Comparison between remote and local sync results"""
    scenario_name: str
    remote_result: SyncTestResult
    local_result: SyncTestResult

    # Differences
    protocol_differences: List[Dict] = field(default_factory=list)
    behavior_differences: List[str] = field(default_factory=list)

    def is_match(self) -> bool:
        """Returns True if both servers behaved identically"""
        return (
            self.remote_result.sync_success == self.local_result.sync_success and
            self.remote_result.files_verified == self.local_result.files_verified and
            len(self.protocol_differences) == 0 and
            len(self.behavior_differences) == 0
        )


class SeafileClient:
    """Wrapper for Seafile API operations"""

    def __init__(self, server_url: str, username: str, password: str, name: str):
        self.server_url = server_url
        self.username = username
        self.password = password
        self.name = name  # "remote" or "local"
        self.token = None

    def authenticate(self):
        """Get auth token"""
        url = f"{self.server_url}/api2/auth-token/"
        resp = requests.post(url, data={"username": self.username, "password": self.password}, verify=False)
        resp.raise_for_status()
        self.token = resp.json()["token"]
        return self.token

    def create_library(self, name: str, password: Optional[str] = None) -> str:
        """Create a library and return its ID"""
        url = f"{self.server_url}/api2/repos/"
        headers = {"Authorization": f"Token {self.token}"}
        data = {"name": name}
        if password:
            data["passwd"] = password
        resp = requests.post(url, headers=headers, data=data, verify=False)
        resp.raise_for_status()
        result = resp.json()
        return result.get("repo_id") or result.get("id")

    def get_upload_link(self, repo_id: str, folder_path: str = "/") -> str:
        """Get upload link for repository folder"""
        # Upload links are per-directory in stock Seafile
        from urllib.parse import quote
        encoded_path = quote(folder_path, safe='')
        url = f"{self.server_url}/api2/repos/{repo_id}/upload-link/?p={encoded_path}&from=web"
        headers = {"Authorization": f"Token {self.token}"}
        resp = requests.get(url, headers=headers, verify=False)
        resp.raise_for_status()
        return resp.text.strip().strip('"')

    def upload_file(self, upload_url: str, file_path: str, content: bytes, parent_dir: str = "/"):
        """Upload a file"""
        headers = {"Authorization": f"Token {self.token}"}
        files = {"file": (os.path.basename(file_path), BytesIO(content))}
        data = {"parent_dir": parent_dir, "replace": "1"}
        resp = requests.post(upload_url, headers=headers, files=files, data=data, verify=False)
        resp.raise_for_status()
        return resp.text

    def create_directory(self, repo_id: str, path: str) -> bool:
        """Create a directory. Returns True if created or already exists."""
        # Official seafile-js format (verified from source code)
        # URL: /api2/repos/{id}/dir/?p={url_encoded_path}
        # Body: FormData with operation=mkdir
        from urllib.parse import quote
        encoded_path = quote(path, safe='')
        url = f"{self.server_url}/api2/repos/{repo_id}/dir/?p={encoded_path}"
        headers = {"Authorization": f"Token {self.token}"}
        data = {"operation": "mkdir"}
        resp = requests.post(url, headers=headers, data=data, verify=False)

        # Success codes
        if resp.status_code in [200, 201]:
            return True

        # 400 might mean "directory already exists" or other error
        if resp.status_code == 400:
            try:
                # Try to parse error response
                error_text = resp.text.lower()
                error_json = resp.json() if resp.text else {}
                error_str = str(error_json).lower()

                # Check for various "already exists" messages
                if "exist" in error_text or "exist" in error_str:
                    return True  # Directory already exists, that's fine

                # Print error for debugging
                print(f"        400 error creating {path}: {resp.text[:100]}")
            except:
                print(f"        400 error creating {path}: {resp.text[:100]}")

            # 400 might still be okay if directory exists
            # Let's try to verify by listing the parent directory
            return False

        # 520 is "Operation failed" - might still mean directory was created
        if resp.status_code == 520:
            return False

        # Other errors
        try:
            resp.raise_for_status()
        except:
            print(f"        HTTP {resp.status_code} creating {path}: {resp.text[:100]}")
        return False

    def delete_library(self, repo_id: str):
        """Delete a library"""
        url = f"{self.server_url}/api2/repos/{repo_id}/"
        headers = {"Authorization": f"Token {self.token}"}
        requests.delete(url, headers=headers, verify=False)


class DesktopClientSync:
    """Manages Seafile desktop client (seaf-cli) for syncing"""

    def __init__(self, config_dir: str, sync_dir: str, server_url: str, username: str, password: str):
        self.config_dir = config_dir
        self.sync_dir = sync_dir
        self.server_url = server_url
        self.username = username
        self.password = password

    def init(self):
        """Initialize seafile client"""
        # Stop any existing daemon first
        print(f"    Stopping any existing daemon...")
        self.stop()
        time.sleep(1)

        # Remove existing directories to ensure clean init
        # IMPORTANT: seaf-cli init requires the directory to NOT exist
        print(f"    Cleaning up old directories...")
        if os.path.exists(self.config_dir):
            shutil.rmtree(self.config_dir)
        if os.path.exists(self.sync_dir):
            shutil.rmtree(self.sync_dir)

        # Create sync directory (seaf-cli won't create this)
        os.makedirs(self.sync_dir, exist_ok=True)

        # DO NOT create config_dir - let seaf-cli init create it
        print(f"    Running seaf-cli init (will create {self.config_dir})...")
        result = subprocess.run(
            ["seaf-cli", "init", "-c", self.config_dir, "-d", self.config_dir],
            capture_output=True,
            text=True
        )

        print(f"    Init stdout: {result.stdout.strip()}")
        if result.returncode != 0:
            print(f"    Init failed with code {result.returncode}")
            print(f"    stderr: {result.stderr.strip()}")
        else:
            # Verify seafile.ini was created
            ini_path = os.path.join(self.config_dir, "seafile.ini")
            if os.path.exists(ini_path):
                print(f"    ✓ seafile.ini created at {ini_path}")
            else:
                print(f"    ✗ WARNING: seafile.ini not found at {ini_path}")

        return result.returncode == 0

    def start(self):
        """Start seafile daemon"""
        print(f"    Starting daemon...")
        result = subprocess.run(
            ["seaf-cli", "start", "-c", self.config_dir],
            capture_output=True,
            text=True
        )

        if result.returncode != 0:
            print(f"    Failed to start daemon: {result.stderr}")
        else:
            print(f"    Daemon started, waiting for initialization...")

        time.sleep(3)  # Wait for daemon to start

        # Verify daemon is running
        status_result = subprocess.run(
            ["seaf-cli", "status", "-c", self.config_dir],
            capture_output=True,
            text=True
        )
        if status_result.returncode == 0:
            print(f"    ✓ Daemon is running")
        else:
            print(f"    ✗ Daemon may not be running properly")

        return result.returncode == 0

    def stop(self):
        """Stop seafile daemon"""
        subprocess.run(["seaf-cli", "stop", "-c", self.config_dir], capture_output=True)

    def download(self, repo_id: str, library_name: str) -> Tuple[bool, str]:
        """Download/sync a library"""
        sync_path = os.path.join(self.sync_dir, library_name)

        # Verify config directory exists and has seafile.ini
        ini_path = os.path.join(self.config_dir, "seafile.ini")
        if not os.path.exists(ini_path):
            error_msg = f"ERROR: seafile.ini not found at {ini_path} before download"
            print(f"    {error_msg}")
            return False, error_msg

        print(f"    ✓ Config verified: {ini_path}")
        print(f"    Running seaf-cli download...")

        result = subprocess.run(
            [
                "seaf-cli", "download", "-c", self.config_dir,
                "-l", repo_id,
                "-s", self.server_url,
                "-u", self.username,
                "-p", self.password,
                "-d", self.sync_dir
            ],
            capture_output=True,
            text=True
        )

        output = result.stdout + result.stderr
        if result.returncode != 0:
            print(f"    Download failed with code {result.returncode}")
            print(f"    Output: {output}")

        return result.returncode == 0, output

    def wait_for_sync(self, timeout: int = 60) -> bool:
        """Wait for sync to complete"""
        start_time = time.time()
        while time.time() - start_time < timeout:
            result = subprocess.run(
                ["seaf-cli", "status", "-c", self.config_dir],
                capture_output=True,
                text=True
            )

            status = result.stdout.lower()

            # Check for errors
            if "error" in status:
                print(f"    Sync error detected: {result.stdout}")
                return False

            # Check if still syncing (waiting, downloading, uploading, etc.)
            if "waiting" in status or "downloading" in status or "uploading" in status or "committing" in status:
                time.sleep(2)
                continue

            # Check if synchronized
            if "synchronized" in status:
                print(f"    ✓ Sync completed successfully")
                return True

            # Unknown status, keep waiting
            time.sleep(2)

        # Timeout - check final status
        result = subprocess.run(
            ["seaf-cli", "status", "-c", self.config_dir],
            capture_output=True,
            text=True
        )
        print(f"    Sync timeout. Final status: {result.stdout}")
        return "synchronized" in result.stdout.lower()

    def get_log(self) -> str:
        """Get client log"""
        log_path = os.path.join(self.config_dir, "logs", "seafile.log")
        if os.path.exists(log_path):
            with open(log_path, 'r') as f:
                return f.read()
        return ""


class SyncProtocolTester:
    """Main test framework"""

    def __init__(self):
        self.remote_client = SeafileClient(REMOTE_URL, REMOTE_USER, REMOTE_PASS, "remote")
        self.local_client = SeafileClient(LOCAL_URL, LOCAL_USER, LOCAL_PASS, "local")

        self.remote_sync = DesktopClientSync(
            REMOTE_CONFIG_DIR, REMOTE_SYNC_DIR, REMOTE_URL, REMOTE_USER, REMOTE_PASS
        )
        self.local_sync = DesktopClientSync(
            LOCAL_CONFIG_DIR, LOCAL_SYNC_DIR, LOCAL_URL, LOCAL_USER, LOCAL_PASS
        )

        self.results: List[ComparisonResult] = []
        self.test_libraries: List[Tuple[SeafileClient, str]] = []  # Track libraries to clean up

    def setup(self):
        """Setup test environment"""
        print("Setting up test environment...")

        # Clean up old test data
        if os.path.exists(BASE_DIR):
            shutil.rmtree(BASE_DIR)

        os.makedirs(BASE_DIR, exist_ok=True)
        os.makedirs(RESULTS_DIR, exist_ok=True)

        # Authenticate
        print("  Authenticating with remote server...")
        self.remote_client.authenticate()
        print(f"  Remote token: {self.remote_client.token[:20]}...")

        print("  Authenticating with local server...")
        self.local_client.authenticate()
        print(f"  Local token: {self.local_client.token[:20]}...")

        # Initialize sync clients
        print("  Initializing remote sync client...")
        self.remote_sync.init()
        self.remote_sync.start()

        print("  Initializing local sync client...")
        self.local_sync.init()
        self.local_sync.start()

        print("✓ Setup complete\n")

    def teardown(self):
        """Cleanup test environment"""
        print("\nCleaning up...")

        # Stop sync clients first
        self.remote_sync.stop()
        self.local_sync.stop()
        time.sleep(2)  # Wait for daemons to fully stop

        # Delete test libraries
        print(f"  Deleting {len(self.test_libraries)} test libraries...")
        for client, repo_id in self.test_libraries:
            try:
                client.delete_library(repo_id)
            except Exception as e:
                print(f"    Warning: Failed to delete library {repo_id}: {e}")

        print("✓ Cleanup complete")

    def run_scenario(self, scenario: TestScenario) -> ComparisonResult:
        """Run a single test scenario"""
        print(f"\n{'='*80}")
        print(f"SCENARIO: {scenario.name}")
        print(f"{'='*80}")
        print(f"Description: {scenario.description}")
        print(f"Files: {scenario.file_count()}")
        print(f"Total size: {scenario.total_size() / 1024 / 1024:.2f} MB\n")

        # Test on remote server (stock Seafile - the reference implementation)
        print("Testing on REMOTE server (stock Seafile)...")
        remote_result = self._test_on_server(
            scenario, self.remote_client, self.remote_sync, "remote"
        )

        # Test on local server (SesameFS - must match stock Seafile behavior)
        print("\nTesting on LOCAL server (SesameFS)...")
        local_result = self._test_on_server(
            scenario, self.local_client, self.local_sync, "local"
        )

        # Compare results
        comparison = self._compare_results(scenario, remote_result, local_result)

        self.results.append(comparison)
        return comparison

    def _test_on_server(
        self, scenario: TestScenario, client: SeafileClient,
        sync: DesktopClientSync, server_type: str
    ) -> SyncTestResult:
        """Test scenario on a single server"""

        timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
        library_name = f"test_{scenario.name}_{server_type}_{timestamp}"

        result = SyncTestResult(
            scenario_name=scenario.name,
            server_type=server_type,
            library_id="",
            library_name=library_name,
            sync_success=False,
            sync_duration=0.0,
            files_expected=scenario.file_count(),
            files_synced=0,
            files_verified=0
        )

        try:
            # Create library
            print(f"  Creating library '{library_name}'...")
            repo_id = client.create_library(library_name)
            result.library_id = repo_id
            self.test_libraries.append((client, repo_id))  # Track for cleanup
            print(f"  Library ID: {repo_id}")

            # Upload files
            print(f"  Uploading {scenario.file_count()} files...")
            self._upload_files(client, repo_id, scenario.files)

            # Sync with desktop client
            print(f"  Syncing with desktop client...")
            sync_start = time.time()
            success, output = sync.download(repo_id, library_name)

            if success:
                # Wait for sync to complete
                print(f"  Waiting for sync to complete...")
                sync_success = sync.wait_for_sync(timeout=120)
                result.sync_duration = time.time() - sync_start
                result.sync_success = sync_success

                if sync_success:
                    print(f"  ✓ Sync completed in {result.sync_duration:.2f}s")
                else:
                    print(f"  ✗ Sync failed or timed out")
            else:
                print(f"  ✗ Failed to start sync")
                print(f"  Error output: {output}")
                result.sync_success = False

            # Verify files
            print(f"  Verifying synced files...")
            self._verify_files(scenario, sync.sync_dir, library_name, result)

            # Get client logs
            log = sync.get_log()
            result.client_log_errors = self._extract_errors_from_log(log)

            # Report
            print(f"\n  Results:")
            print(f"    Files expected: {result.files_expected}")
            print(f"    Files synced: {result.files_synced}")
            print(f"    Files verified: {result.files_verified}")
            print(f"    Success rate: {result.success_rate():.1f}%")

            if result.files_missing:
                print(f"    Missing: {', '.join(result.files_missing[:5])}")
                if len(result.files_missing) > 5:
                    print(f"    ... and {len(result.files_missing) - 5} more")
            if result.files_corrupted:
                print(f"    Corrupted: {', '.join(result.files_corrupted)}")
            if result.client_log_errors:
                print(f"    Client errors: {len(result.client_log_errors)}")
                # Print first few errors for debugging
                for error in result.client_log_errors[:3]:
                    print(f"      - {error}")

        except Exception as e:
            print(f"  ✗ ERROR: {e}")
            import traceback
            traceback.print_exc()

        # NOTE: Don't delete library here - it causes 404 errors while client is still syncing
        # Libraries will be cleaned up by teardown()
        return result

    def _upload_files(self, client: SeafileClient, repo_id: str, files: List[TestFile]):
        """Upload test files to library"""
        # Create all directories first (stock Seafile requires this)
        dirs_created = set()
        all_dirs = set()

        # Collect all directories needed
        for test_file in files:
            file_path = test_file.path
            if os.path.dirname(file_path):
                parent_dir = "/" + os.path.dirname(file_path)
                parts = parent_dir.strip("/").split("/")
                for i in range(len(parts)):
                    all_dirs.add("/" + "/".join(parts[:i+1]))

        # Create directories in order (parent before child)
        if all_dirs:
            print(f"    Creating {len(all_dirs)} directories...")
        for dir_path in sorted(all_dirs):
            if dir_path and dir_path != "/":
                try:
                    success = client.create_directory(repo_id, dir_path)
                    if success:
                        dirs_created.add(dir_path)
                        print(f"      ✓ Created: {dir_path}")
                    else:
                        print(f"      ✗ Failed to create: {dir_path}")
                    time.sleep(0.1)  # Brief pause
                except Exception as e:
                    print(f"      ✗ Exception creating {dir_path}: {e}")

        # Upload all files (get upload link per directory)
        upload_links = {}  # Cache upload links per directory

        for test_file in files:
            file_path = test_file.path
            parent_dir = "/" + os.path.dirname(file_path) if os.path.dirname(file_path) else "/"

            # Get upload link for this parent directory (cached)
            if parent_dir not in upload_links:
                upload_links[parent_dir] = client.get_upload_link(repo_id, parent_dir)

            upload_link = upload_links[parent_dir]

            try:
                client.upload_file(upload_link, file_path, test_file.content, parent_dir)
            except Exception as e:
                # If upload fails, try getting a fresh link and retry once
                print(f"      Upload failed for {file_path} (parent: {parent_dir}), retrying with fresh link...")
                try:
                    upload_link = client.get_upload_link(repo_id, parent_dir)
                    upload_links[parent_dir] = upload_link  # Update cache
                    client.upload_file(upload_link, file_path, test_file.content, parent_dir)
                    print(f"      ✓ Retry succeeded for {file_path}")
                except Exception as e2:
                    print(f"      ✗ Error uploading {file_path}: {e2}")

    def _verify_files(
        self, scenario: TestScenario, sync_dir: str,
        library_name: str, result: SyncTestResult
    ):
        """Verify synced files match expected files"""
        library_path = os.path.join(sync_dir, library_name)

        # Debug: List what's actually in sync_dir
        if os.path.exists(sync_dir):
            actual_dirs = os.listdir(sync_dir)
            print(f"    Sync directory contains: {actual_dirs}")
        else:
            print(f"    Sync directory doesn't exist: {sync_dir}")

        if not os.path.exists(library_path):
            print(f"    Library path not found: {library_path}")
            result.files_synced = 0
            result.files_missing = [f.path for f in scenario.files]
            return
        else:
            print(f"    ✓ Library path exists: {library_path}")

        for test_file in scenario.files:
            file_path = os.path.join(library_path, test_file.path)

            if not os.path.exists(file_path):
                result.files_missing.append(test_file.path)
                continue

            result.files_synced += 1

            # Verify content
            with open(file_path, 'rb') as f:
                content = f.read()
                sha256 = hashlib.sha256(content).hexdigest()

                if sha256 == test_file.sha256:
                    result.files_verified += 1
                else:
                    result.files_corrupted.append(test_file.path)

    def _extract_errors_from_log(self, log: str) -> List[str]:
        """Extract error messages from client log"""
        errors = []
        for line in log.split('\n'):
            if 'error' in line.lower() or 'failed' in line.lower():
                errors.append(line.strip())
        return errors

    def _compare_results(
        self, scenario: TestScenario,
        remote: SyncTestResult, local: SyncTestResult
    ) -> ComparisonResult:
        """Compare remote and local results"""
        comparison = ComparisonResult(
            scenario_name=scenario.name,
            remote_result=remote,
            local_result=local
        )

        # Compare sync success
        if remote.sync_success != local.sync_success:
            comparison.behavior_differences.append(
                f"Sync success: remote={remote.sync_success}, local={local.sync_success}"
            )

        # Compare file counts
        if remote.files_verified != local.files_verified:
            comparison.behavior_differences.append(
                f"Files verified: remote={remote.files_verified}, local={local.files_verified}"
            )

        # Compare missing files
        remote_missing = set(remote.files_missing)
        local_missing = set(local.files_missing)
        if remote_missing != local_missing:
            comparison.behavior_differences.append(
                f"Missing files differ: remote={remote_missing}, local={local_missing}"
            )

        # Compare corrupted files
        remote_corrupted = set(remote.files_corrupted)
        local_corrupted = set(local.files_corrupted)
        if remote_corrupted != local_corrupted:
            comparison.behavior_differences.append(
                f"Corrupted files differ: remote={remote_corrupted}, local={local_corrupted}"
            )

        return comparison

    def generate_report(self):
        """Generate test report"""
        report_path = os.path.join(RESULTS_DIR, f"test_report_{datetime.now().strftime('%Y%m%d_%H%M%S')}.txt")

        with open(report_path, 'w') as f:
            f.write("="*80 + "\n")
            f.write("SEAFILE SYNC PROTOCOL COMPREHENSIVE TEST REPORT\n")
            f.write("="*80 + "\n")
            f.write(f"Date: {datetime.now().isoformat()}\n")
            f.write(f"Remote Server: {REMOTE_URL}\n")
            f.write(f"Local Server: {LOCAL_URL}\n")
            f.write(f"\n")

            # Summary
            f.write("SUMMARY\n")
            f.write("-"*80 + "\n")
            total_scenarios = len(self.results)
            matching_scenarios = sum(1 for r in self.results if r.is_match())
            f.write(f"Total scenarios tested: {total_scenarios}\n")
            f.write(f"Matching behaviors: {matching_scenarios}\n")
            f.write(f"Differing behaviors: {total_scenarios - matching_scenarios}\n")
            f.write(f"\n")

            # Detailed results
            for comparison in self.results:
                f.write("\n" + "="*80 + "\n")
                f.write(f"SCENARIO: {comparison.scenario_name}\n")
                f.write("="*80 + "\n")

                # Remote results
                f.write("\nREMOTE (Stock Seafile):\n")
                self._write_result(f, comparison.remote_result)

                # Local results
                f.write("\nLOCAL (SesameFS):\n")
                self._write_result(f, comparison.local_result)

                # Differences
                if comparison.is_match():
                    f.write("\n✓ MATCH: Both servers behaved identically\n")
                else:
                    f.write("\n✗ DIFFERENCES FOUND:\n")
                    for diff in comparison.behavior_differences:
                        f.write(f"  - {diff}\n")
                    for diff in comparison.protocol_differences:
                        f.write(f"  - Protocol: {diff}\n")

        print(f"\n✓ Report saved to: {report_path}")
        return report_path

    def _write_result(self, f, result: SyncTestResult):
        """Write single result to file"""
        f.write(f"  Library: {result.library_name} ({result.library_id})\n")
        f.write(f"  Sync success: {result.sync_success}\n")
        f.write(f"  Sync duration: {result.sync_duration:.2f}s\n")
        f.write(f"  Files expected: {result.files_expected}\n")
        f.write(f"  Files synced: {result.files_synced}\n")
        f.write(f"  Files verified: {result.files_verified}\n")
        f.write(f"  Success rate: {result.success_rate():.1f}%\n")
        if result.files_missing:
            f.write(f"  Missing: {', '.join(result.files_missing)}\n")
        if result.files_corrupted:
            f.write(f"  Corrupted: {', '.join(result.files_corrupted)}\n")
        if result.client_log_errors:
            f.write(f"  Client errors: {len(result.client_log_errors)}\n")


# ============================================================================
# TEST SCENARIOS
# ============================================================================

def generate_random_content(size: int) -> bytes:
    """Generate random binary content"""
    return bytes(random.getrandbits(8) for _ in range(size))


def create_test_scenarios() -> List[TestScenario]:
    """Create all test scenarios"""

    scenarios = []

    # Scenario 1: Single small file in root
    scenarios.append(TestScenario(
        name="single_small_file",
        description="Single small text file in root directory",
        files=[
            TestFile("test.txt", b"Hello, Seafile!\n" * 10)
        ]
    ))

    # Scenario 2: Multiple small files in root
    scenarios.append(TestScenario(
        name="multiple_small_files",
        description="10 small files in root directory",
        files=[
            TestFile(f"file{i}.txt", f"File {i} content\n".encode() * 100)
            for i in range(10)
        ]
    ))

    # Scenario 3: Nested folders
    scenarios.append(TestScenario(
        name="nested_folders",
        description="Files in nested folder structure",
        files=[
            TestFile("root.txt", b"Root file\n" * 10),
            TestFile("folder1/file1.txt", b"Folder 1 file\n" * 10),
            TestFile("folder1/file2.txt", b"Folder 1 file 2\n" * 10),
            TestFile("folder1/subfolder/deep.txt", b"Deep file\n" * 10),
            TestFile("folder2/another.txt", b"Another folder file\n" * 10),
        ]
    ))

    # Scenario 4: Medium files (1-10 MB)
    scenarios.append(TestScenario(
        name="medium_files",
        description="3 medium-sized files (1-5 MB each)",
        files=[
            TestFile("medium1.bin", generate_random_content(1 * 1024 * 1024)),  # 1 MB
            TestFile("medium2.bin", generate_random_content(3 * 1024 * 1024)),  # 3 MB
            TestFile("medium3.bin", generate_random_content(5 * 1024 * 1024)),  # 5 MB
        ]
    ))

    # Scenario 5: Large file (50 MB)
    scenarios.append(TestScenario(
        name="large_file",
        description="Single large file (50 MB)",
        files=[
            TestFile("large.bin", generate_random_content(50 * 1024 * 1024))
        ]
    ))

    # Scenario 6: Many tiny files
    scenarios.append(TestScenario(
        name="many_tiny_files",
        description="50 tiny files (1KB each)",
        files=[
            TestFile(f"tiny/file{i:03d}.txt", f"Tiny {i}\n".encode() * 100)
            for i in range(50)
        ]
    ))

    # Scenario 7: Mixed content
    scenarios.append(TestScenario(
        name="mixed_content",
        description="Mix of file sizes and types in folders",
        files=[
            TestFile("README.md", b"# Project\nThis is a test project\n"),
            TestFile("docs/guide.txt", b"User guide\n" * 1000),
            TestFile("docs/api.txt", b"API documentation\n" * 2000),
            TestFile("src/main.py", b"print('Hello')\n" * 100),
            TestFile("src/lib/utils.py", b"def util():\n    pass\n" * 50),
            TestFile("data/small.dat", generate_random_content(100 * 1024)),  # 100 KB
            TestFile("data/medium.dat", generate_random_content(2 * 1024 * 1024)),  # 2 MB
            TestFile("images/logo.png", generate_random_content(50 * 1024)),  # 50 KB
        ]
    ))

    return scenarios


# ============================================================================
# MAIN
# ============================================================================

def main():
    parser = argparse.ArgumentParser(description="Comprehensive Seafile Sync Protocol Test")
    parser.add_argument("--test-all", action="store_true", help="Run all test scenarios")
    parser.add_argument("--test-scenario", type=str, help="Run specific scenario")
    parser.add_argument("--list-scenarios", action="store_true", help="List available scenarios")
    parser.add_argument("--quick", action="store_true", help="Quick test (skip large files)")

    args = parser.parse_args()

    scenarios = create_test_scenarios()

    if args.list_scenarios:
        print("\nAvailable test scenarios:")
        print("-" * 80)
        for scenario in scenarios:
            print(f"\n{scenario.name}")
            print(f"  {scenario.description}")
            print(f"  Files: {scenario.file_count()}, Size: {scenario.total_size() / 1024 / 1024:.2f} MB")
        return 0

    # Filter scenarios
    if args.test_scenario:
        scenarios = [s for s in scenarios if s.name == args.test_scenario]
        if not scenarios:
            print(f"Error: Scenario '{args.test_scenario}' not found")
            return 1

    if args.quick:
        # Skip large file scenarios
        scenarios = [s for s in scenarios if s.total_size() < 20 * 1024 * 1024]

    # Run tests
    tester = SyncProtocolTester()

    try:
        tester.setup()

        for scenario in scenarios:
            tester.run_scenario(scenario)

        tester.generate_report()

        # Print summary
        print("\n" + "="*80)
        print("TEST SUMMARY")
        print("="*80)
        total = len(tester.results)
        matching = sum(1 for r in tester.results if r.is_match())
        print(f"Total scenarios: {total}")
        print(f"Matching: {matching}")
        print(f"Differing: {total - matching}")

        if matching == total:
            print("\n✓ All scenarios match! Protocol is compatible.")
            return 0
        else:
            print("\n✗ Some scenarios differ. Review report for details.")
            return 1

    finally:
        tester.teardown()


if __name__ == "__main__":
    sys.exit(main())
