#!/usr/bin/env python3
"""
Comprehensive Multi-File Library Sync Protocol Comparison

Compares sync protocol behavior between stock Seafile and SesameFS
for libraries with multiple files to diagnose sync issues.

Output format similar to SEAFILE-SYNC-PROTOCOL-RFC.md for documentation.
"""
import json
import requests
import sys
import zlib
import hashlib
from datetime import datetime
from urllib.parse import urlparse
import urllib3
urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)

# Configuration
REMOTE_URL = "https://app.nihaoconsult.com"
REMOTE_USER = "abel.aguzmans@gmail.com"
REMOTE_PASS = "Qwerty123!"

LOCAL_URL = "http://localhost:8080"
LOCAL_USER = "abel.aguzmans@gmail.com"
LOCAL_PASS = "dev-token-123"

# Find libraries with "xxxTxT" in the name (user created test libraries)
LIBRARY_NAME_PATTERN = "xxxTxT"

class ProtocolComparison:
    def __init__(self, name, remote_url, remote_user, remote_pass, local_url, local_user, local_pass):
        self.name = name
        self.remote_url = remote_url
        self.remote_user = remote_user
        self.remote_pass = remote_pass
        self.local_url = local_url
        self.local_user = local_user
        self.local_pass = local_pass

        self.remote_token = None
        self.local_token = None
        self.remote_repo_id = None
        self.local_repo_id = None
        self.remote_sync_token = None
        self.local_sync_token = None
        self.remote_head_commit = None
        self.local_head_commit = None

        self.differences = []
        self.report = []

    def log(self, message, indent=0):
        """Log message with indentation"""
        prefix = "  " * indent
        print(f"{prefix}{message}")
        self.report.append(f"{prefix}{message}")

    def section(self, title):
        """Print section header"""
        separator = "=" * 80
        self.log("")
        self.log(separator)
        self.log(title)
        self.log(separator)

    def subsection(self, title):
        """Print subsection header"""
        self.log("")
        self.log(f"--- {title} ---")

    def diff_found(self, endpoint, field, remote_val, local_val, severity="MEDIUM"):
        """Record a difference"""
        diff = {
            "endpoint": endpoint,
            "field": field,
            "remote": remote_val,
            "local": local_val,
            "severity": severity
        }
        self.differences.append(diff)
        self.log(f"❌ DIFFERENCE: {field}", indent=1)
        self.log(f"Remote: {remote_val}", indent=2)
        self.log(f"Local:  {local_val}", indent=2)
        self.log(f"Severity: {severity}", indent=2)

    def match_found(self, field, value):
        """Record a match"""
        self.log(f"✓ {field}: {value}", indent=1)

    def authenticate(self, server_url, username, password):
        """Authenticate and get token"""
        url = f"{server_url}/api2/auth-token/"
        data = {"username": username, "password": password}
        resp = requests.post(url, data=data, verify=False)
        resp.raise_for_status()
        return resp.json()["token"]

    def find_library(self, server_url, token, pattern):
        """Find library by name pattern"""
        url = f"{server_url}/api2/repos/"
        headers = {"Authorization": f"Token {token}"}
        resp = requests.get(url, headers=headers, verify=False)
        resp.raise_for_status()
        repos = resp.json()

        for repo in repos:
            if pattern in repo.get("name", ""):
                return repo["id"], repo["name"]
        return None, None

    def compare_json_response(self, endpoint, remote_resp, local_resp, ignore_fields=None):
        """Compare JSON responses field by field"""
        ignore_fields = ignore_fields or []

        # Check for missing/extra fields
        remote_keys = set(remote_resp.keys())
        local_keys = set(local_resp.keys())

        missing_in_local = remote_keys - local_keys - set(ignore_fields)
        extra_in_local = local_keys - remote_keys - set(ignore_fields)

        if missing_in_local:
            self.diff_found(endpoint, "missing_fields", list(missing_in_local), "N/A", "HIGH")
        if extra_in_local:
            self.diff_found(endpoint, "extra_fields", "N/A", list(extra_in_local), "LOW")

        # Compare common fields
        common_keys = (remote_keys & local_keys) - set(ignore_fields)
        for key in sorted(common_keys):
            remote_val = remote_resp[key]
            local_val = local_resp[key]

            # Skip obviously different values (tokens, timestamps, etc.)
            if key in ['token', 'relay_addr', 'relay_port', 'mtime', 'mtime_relative',
                      'commit_id', 'parent_id', 'ctime', 'creator_name', 'repo_id',
                      'root_id', 'description']:
                self.match_found(f"{key} (type)", f"{type(remote_val).__name__} == {type(local_val).__name__}")
                continue

            if remote_val != local_val:
                # Check if it's a type mismatch
                if type(remote_val) != type(local_val):
                    self.diff_found(endpoint, f"{key} (type)",
                                  f"{type(remote_val).__name__}: {remote_val}",
                                  f"{type(local_val).__name__}: {local_val}", "HIGH")
                else:
                    self.diff_found(endpoint, key, remote_val, local_val)
            else:
                self.match_found(key, remote_val)

    def compare_binary_response(self, endpoint, remote_data, local_data):
        """Compare binary responses (pack-fs format)"""
        self.log(f"Remote binary size: {len(remote_data)} bytes", indent=1)
        self.log(f"Local binary size: {len(local_data)} bytes", indent=1)

        if len(remote_data) != len(local_data):
            self.diff_found(endpoint, "binary_size", len(remote_data), len(local_data), "HIGH")

        # Parse pack-fs format
        def parse_pack_fs(data):
            """Parse pack-fs binary format"""
            objects = []
            offset = 0
            while offset < len(data):
                if offset + 44 > len(data):
                    break

                fs_id = data[offset:offset+40].decode('ascii')
                size = int.from_bytes(data[offset+40:offset+44], 'big')
                compressed = data[offset+44:offset+44+size]

                try:
                    decompressed = zlib.decompress(compressed)
                    obj = json.loads(decompressed.decode('utf-8'))
                    computed_hash = hashlib.sha1(decompressed).hexdigest()
                    objects.append({
                        'fs_id': fs_id,
                        'size': size,
                        'computed_hash': computed_hash,
                        'hash_match': fs_id == computed_hash,
                        'object': obj
                    })
                except Exception as e:
                    self.log(f"Failed to parse object at offset {offset}: {e}", indent=2)

                offset += 44 + size

            return objects

        remote_objects = parse_pack_fs(remote_data)
        local_objects = parse_pack_fs(local_data)

        self.log(f"Remote objects count: {len(remote_objects)}", indent=1)
        self.log(f"Local objects count: {len(local_objects)}", indent=1)

        if len(remote_objects) != len(local_objects):
            self.diff_found(endpoint, "object_count", len(remote_objects), len(local_objects), "HIGH")

        # Compare each object
        for i, (remote_obj, local_obj) in enumerate(zip(remote_objects, local_objects)):
            self.subsection(f"Object {i+1}: {remote_obj['fs_id'][:8]}...")

            if remote_obj['fs_id'] != local_obj['fs_id']:
                self.diff_found(f"{endpoint}[{i}]", "fs_id", remote_obj['fs_id'], local_obj['fs_id'], "CRITICAL")

            if not remote_obj['hash_match']:
                self.log(f"⚠️  Remote hash mismatch: {remote_obj['fs_id']} != {remote_obj['computed_hash']}", indent=1)
            if not local_obj['hash_match']:
                self.log(f"⚠️  Local hash mismatch: {local_obj['fs_id']} != {local_obj['computed_hash']}", indent=1)

            # Compare object content
            remote_content = remote_obj['object']
            local_content = local_obj['object']

            if remote_content != local_content:
                self.log(f"Object content differs", indent=1)
                # Compare specific fields
                self.compare_json_response(f"{endpoint}[{i}]", remote_content, local_content)

        return remote_objects, local_objects

    def run(self):
        """Run comprehensive protocol comparison"""
        self.section("MULTI-FILE LIBRARY SYNC PROTOCOL COMPARISON")
        self.log(f"Test: {self.name}")
        self.log(f"Date: {datetime.now().isoformat()}")
        self.log(f"Remote: {self.remote_url}")
        self.log(f"Local:  {self.local_url}")

        try:
            # Step 1: Authenticate
            self.section("STEP 1: Authentication")
            self.subsection("Remote Authentication")
            self.remote_token = self.authenticate(self.remote_url, self.remote_user, self.remote_pass)
            self.log(f"Token: {self.remote_token[:20]}...", indent=1)

            self.subsection("Local Authentication")
            self.local_token = self.authenticate(self.local_url, self.local_user, self.local_pass)
            self.log(f"Token: {self.local_token[:20]}...", indent=1)

            # Step 2: Find libraries
            self.section("STEP 2: Find Test Libraries")
            self.subsection("Remote Library")
            self.remote_repo_id, remote_name = self.find_library(self.remote_url, self.remote_token, LIBRARY_NAME_PATTERN)
            if not self.remote_repo_id:
                self.log(f"❌ No library found with pattern '{LIBRARY_NAME_PATTERN}'", indent=1)
                return 1
            self.log(f"Found: {remote_name}", indent=1)
            self.log(f"ID: {self.remote_repo_id}", indent=1)

            self.subsection("Local Library")
            self.local_repo_id, local_name = self.find_library(self.local_url, self.local_token, LIBRARY_NAME_PATTERN)
            if not self.local_repo_id:
                self.log(f"❌ No library found with pattern '{LIBRARY_NAME_PATTERN}'", indent=1)
                return 1
            self.log(f"Found: {local_name}", indent=1)
            self.log(f"ID: {self.local_repo_id}", indent=1)

            # Step 3: Get download-info
            self.section("STEP 3: GET /api2/repos/{id}/download-info/")

            self.subsection("Remote Response")
            url = f"{self.remote_url}/api2/repos/{self.remote_repo_id}/download-info/"
            headers = {"Authorization": f"Token {self.remote_token}"}
            resp = requests.get(url, headers=headers, verify=False)
            resp.raise_for_status()
            remote_dl_info = resp.json()
            self.log(json.dumps(remote_dl_info, indent=2), indent=1)
            self.remote_sync_token = remote_dl_info['token']
            self.remote_head_commit = remote_dl_info['head_commit_id']

            self.subsection("Local Response")
            url = f"{self.local_url}/api2/repos/{self.local_repo_id}/download-info/"
            headers = {"Authorization": f"Token {self.local_token}"}
            resp = requests.get(url, headers=headers, verify=False)
            resp.raise_for_status()
            local_dl_info = resp.json()
            self.log(json.dumps(local_dl_info, indent=2), indent=1)
            self.local_sync_token = local_dl_info['token']
            self.local_head_commit = local_dl_info['head_commit_id']

            self.subsection("Comparison")
            self.compare_json_response("download-info", remote_dl_info, local_dl_info)

            # Step 4: Get HEAD commit
            self.section("STEP 4: GET /seafhttp/repo/{id}/commit/HEAD")

            self.subsection("Remote Response")
            url = f"{self.remote_url}/seafhttp/repo/{self.remote_repo_id}/commit/HEAD"
            headers = {"Seafile-Repo-Token": self.remote_sync_token}
            resp = requests.get(url, headers=headers, verify=False)
            resp.raise_for_status()
            remote_commit = resp.json()
            self.log(json.dumps(remote_commit, indent=2), indent=1)

            self.subsection("Local Response")
            url = f"{self.local_url}/seafhttp/repo/{self.local_repo_id}/commit/HEAD"
            headers = {"Seafile-Repo-Token": self.local_sync_token}
            resp = requests.get(url, headers=headers, verify=False)
            resp.raise_for_status()
            local_commit = resp.json()
            self.log(json.dumps(local_commit, indent=2), indent=1)

            self.subsection("Comparison")
            self.compare_json_response("commit/HEAD", remote_commit, local_commit)

            # Step 5: Get fs-id-list
            self.section("STEP 5: GET /seafhttp/repo/{id}/fs-id-list/")

            self.subsection("Remote Response")
            url = f"{self.remote_url}/seafhttp/repo/{self.remote_repo_id}/fs-id-list/?server-head={self.remote_head_commit}"
            headers = {"Seafile-Repo-Token": self.remote_sync_token}
            resp = requests.get(url, headers=headers, verify=False)
            resp.raise_for_status()
            remote_fs_ids = resp.json()
            self.log(f"Count: {len(remote_fs_ids)}", indent=1)
            self.log(f"First 10: {remote_fs_ids[:10]}", indent=1)
            self.log(f"Type: {type(remote_fs_ids)}", indent=1)

            self.subsection("Local Response")
            url = f"{self.local_url}/seafhttp/repo/{self.local_repo_id}/fs-id-list/?server-head={self.local_head_commit}"
            headers = {"Seafile-Repo-Token": self.local_sync_token}
            resp = requests.get(url, headers=headers, verify=False)
            resp.raise_for_status()
            local_fs_ids = resp.json()
            self.log(f"Count: {len(local_fs_ids)}", indent=1)
            self.log(f"First 10: {local_fs_ids[:10]}", indent=1)
            self.log(f"Type: {type(local_fs_ids)}", indent=1)

            self.subsection("Comparison")
            if len(remote_fs_ids) != len(local_fs_ids):
                self.diff_found("fs-id-list", "count", len(remote_fs_ids), len(local_fs_ids), "HIGH")
            else:
                self.match_found("count", len(remote_fs_ids))

            # Check if all IDs match
            remote_set = set(remote_fs_ids)
            local_set = set(local_fs_ids)
            missing_in_local = remote_set - local_set
            extra_in_local = local_set - remote_set

            if missing_in_local:
                self.diff_found("fs-id-list", "missing_ids", list(missing_in_local), "N/A", "CRITICAL")
            if extra_in_local:
                self.diff_found("fs-id-list", "extra_ids", "N/A", list(extra_in_local), "CRITICAL")
            if not missing_in_local and not extra_in_local:
                self.match_found("all_ids", "All FS IDs match")

            # Step 6: Get full commit object (if HEAD didn't include root_id)
            if 'root_id' not in remote_commit:
                self.section("STEP 6: GET /seafhttp/repo/{id}/commit/{commit_id} (full commit)")

                self.subsection("Remote Response")
                url = f"{self.remote_url}/seafhttp/repo/{self.remote_repo_id}/commit/{self.remote_head_commit}"
                headers = {"Seafile-Repo-Token": self.remote_sync_token}
                resp = requests.get(url, headers=headers, verify=False)
                resp.raise_for_status()
                remote_full_commit = resp.json()
                self.log(json.dumps(remote_full_commit, indent=2), indent=1)

                self.subsection("Local Response")
                url = f"{self.local_url}/seafhttp/repo/{self.local_repo_id}/commit/{self.local_head_commit}"
                headers = {"Seafile-Repo-Token": self.local_sync_token}
                resp = requests.get(url, headers=headers, verify=False)
                resp.raise_for_status()
                local_full_commit = resp.json()
                self.log(json.dumps(local_full_commit, indent=2), indent=1)

                self.subsection("Comparison")
                self.compare_json_response("commit/{id}", remote_full_commit, local_full_commit)

                root_id_remote = remote_full_commit['root_id']
                root_id_local = local_full_commit['root_id']
            else:
                root_id_remote = remote_commit['root_id']
                root_id_local = local_commit['root_id']

            # Step 7: Get pack-fs for root directory
            self.section("STEP 7: POST /seafhttp/repo/{id}/pack-fs/ (root dir)")

            self.subsection("Remote Response")
            url = f"{self.remote_url}/seafhttp/repo/{self.remote_repo_id}/pack-fs/"
            headers = {"Seafile-Repo-Token": self.remote_sync_token, "Content-Type": "application/json"}
            data = json.dumps([root_id_remote])
            resp = requests.post(url, headers=headers, data=data, verify=False)
            resp.raise_for_status()
            remote_pack_fs = resp.content
            self.log(f"HTTP Status: {resp.status_code}", indent=1)
            self.log(f"Content-Type: {resp.headers.get('Content-Type', 'N/A')}", indent=1)
            self.log(f"Size: {len(remote_pack_fs)} bytes", indent=1)

            self.subsection("Local Response")
            url = f"{self.local_url}/seafhttp/repo/{self.local_repo_id}/pack-fs/"
            headers = {"Seafile-Repo-Token": self.local_sync_token, "Content-Type": "application/json"}
            data = json.dumps([root_id_local])
            resp = requests.post(url, headers=headers, data=data, verify=False)
            resp.raise_for_status()
            local_pack_fs = resp.content
            self.log(f"HTTP Status: {resp.status_code}", indent=1)
            self.log(f"Content-Type: {resp.headers.get('Content-Type', 'N/A')}", indent=1)
            self.log(f"Size: {len(local_pack_fs)} bytes", indent=1)

            self.subsection("Binary Comparison")
            remote_objs, local_objs = self.compare_binary_response("pack-fs", remote_pack_fs, local_pack_fs)

            # Step 8: Check-fs for all IDs
            self.section("STEP 8: POST /seafhttp/repo/{id}/check-fs")

            self.subsection("Remote Response")
            url = f"{self.remote_url}/seafhttp/repo/{self.remote_repo_id}/check-fs"
            headers = {"Seafile-Repo-Token": self.remote_sync_token, "Content-Type": "application/json"}
            data = json.dumps(remote_fs_ids)
            resp = requests.post(url, headers=headers, data=data, verify=False)
            resp.raise_for_status()
            remote_check_fs = resp.json()
            self.log(f"Missing IDs: {remote_check_fs}", indent=1)
            self.log(f"Count: {len(remote_check_fs)}", indent=1)

            self.subsection("Local Response")
            url = f"{self.local_url}/seafhttp/repo/{self.local_repo_id}/check-fs"
            headers = {"Seafile-Repo-Token": self.local_sync_token, "Content-Type": "application/json"}
            data = json.dumps(local_fs_ids)
            resp = requests.post(url, headers=headers, data=data, verify=False)
            resp.raise_for_status()
            local_check_fs = resp.json()
            self.log(f"Missing IDs: {local_check_fs}", indent=1)
            self.log(f"Count: {len(local_check_fs)}", indent=1)

            self.subsection("Comparison")
            if remote_check_fs != local_check_fs:
                self.diff_found("check-fs", "missing_ids", remote_check_fs, local_check_fs, "CRITICAL")
            else:
                self.match_found("check-fs", "Both return same missing IDs")

            # Summary
            self.section("SUMMARY")
            self.log(f"Total differences found: {len(self.differences)}")

            if self.differences:
                self.subsection("Critical Differences")
                critical = [d for d in self.differences if d['severity'] == 'CRITICAL']
                if critical:
                    for d in critical:
                        self.log(f"• {d['endpoint']}: {d['field']}", indent=1)
                        self.log(f"  Remote: {d['remote']}", indent=2)
                        self.log(f"  Local: {d['local']}", indent=2)

                self.subsection("High Priority Differences")
                high = [d for d in self.differences if d['severity'] == 'HIGH']
                if high:
                    for d in high:
                        self.log(f"• {d['endpoint']}: {d['field']}", indent=1)
                        self.log(f"  Remote: {d['remote']}", indent=2)
                        self.log(f"  Local: {d['local']}", indent=2)

            else:
                self.log("✓ No significant differences found!", indent=1)
                self.log("Protocol responses match stock Seafile behavior.", indent=1)

            # Save report
            report_path = f"/tmp/sync_protocol_comparison_{datetime.now().strftime('%Y%m%d_%H%M%S')}.txt"
            with open(report_path, 'w') as f:
                f.write('\n'.join(self.report))
            self.log(f"\nReport saved to: {report_path}")

            return 0 if not critical else 1

        except Exception as e:
            self.log(f"\n❌ ERROR: {e}")
            import traceback
            traceback.print_exc()
            return 1

def main():
    comparison = ProtocolComparison(
        "Multi-File Library Sync Test",
        REMOTE_URL, REMOTE_USER, REMOTE_PASS,
        LOCAL_URL, LOCAL_USER, LOCAL_PASS
    )
    return comparison.run()

if __name__ == "__main__":
    sys.exit(main())
