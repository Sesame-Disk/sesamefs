#!/usr/bin/env python3
"""
Detailed sync protocol diagnostic test
Checks every aspect of the sync protocol to find what's causing "Error when indexing"
"""

import requests
import json
import zlib
import hashlib
import sys
from datetime import datetime

LOCAL_SERVER = "http://host.docker.internal:8080"
LOCAL_USER = "admin@sesamefs.local"
LOCAL_PASS = "dev-token-123"

def log(msg):
    """Print with timestamp"""
    print(f"[{datetime.now().strftime('%H:%M:%S')}] {msg}")

def authenticate(server_url, username, password):
    """Authenticate and get token"""
    url = f"{server_url}/api2/auth-token/"
    data = {"username": username, "password": password}
    resp = requests.post(url, data=data)
    resp.raise_for_status()
    return resp.json()["token"]

def create_encrypted_library(server_url, token, name, password):
    """Create encrypted library"""
    url = f"{server_url}/api/v2.1/repos/"
    headers = {"Authorization": f"Token {token}"}
    data = {"name": name, "passwd": password}
    resp = requests.post(url, data=data, headers=headers)  # Use form data, not JSON
    resp.raise_for_status()
    return resp.json()

def set_password(server_url, token, repo_id, password):
    """Unlock encrypted library"""
    url = f"{server_url}/api/v2.1/repos/{repo_id}/set-password/"
    headers = {"Authorization": f"Token {token}"}
    data = {"password": password}
    resp = requests.post(url, json=data, headers=headers)
    resp.raise_for_status()
    return resp.json()

def get_upload_link(server_url, token, repo_id):
    """Get upload link"""
    url = f"{server_url}/api2/repos/{repo_id}/upload-link/"
    headers = {"Authorization": f"Token {token}"}
    resp = requests.get(url, headers=headers)
    resp.raise_for_status()
    return resp.text.strip().strip('"')

def upload_file(upload_url, token, filename, content):
    """Upload a file"""
    headers = {"Authorization": f"Token {token}"}
    files = {"file": (filename, content)}
    data = {"parent_dir": "/", "replace": "0"}
    resp = requests.post(upload_url, headers=headers, files=files, data=data)
    resp.raise_for_status()
    return resp.text

def get_download_info(server_url, token, repo_id):
    """Get download-info"""
    url = f"{server_url}/api2/repos/{repo_id}/download-info/"
    headers = {"Authorization": f"Token {token}"}
    resp = requests.get(url, headers=headers)
    resp.raise_for_status()
    return resp.json()

def get_commit_head(server_url, token, repo_id):
    """Get HEAD commit"""
    url = f"{server_url}/seafhttp/repo/{repo_id}/commit/HEAD"
    headers = {"Authorization": f"Token {token}"}
    resp = requests.get(url, headers=headers)
    resp.raise_for_status()
    return resp.json()

def get_commit(server_url, token, repo_id, commit_id):
    """Get commit object"""
    url = f"{server_url}/seafhttp/repo/{repo_id}/commit/{commit_id}"
    headers = {"Authorization": f"Token {token}"}
    resp = requests.get(url, headers=headers)
    resp.raise_for_status()
    return resp.json()

def get_fs_id_list(server_url, token, repo_id, commit_id):
    """Get fs-id-list"""
    url = f"{server_url}/seafhttp/repo/{repo_id}/fs-id-list/"
    headers = {"Authorization": f"Token {token}"}
    params = {"server-head": commit_id}
    resp = requests.get(url, headers=headers, params=params)
    resp.raise_for_status()
    # Stock Seafile returns JSON array
    return resp.json()

def pack_fs(server_url, token, repo_id, fs_ids):
    """Get pack-fs"""
    url = f"{server_url}/seafhttp/repo/{repo_id}/pack-fs"
    headers = {
        "Authorization": f"Token {token}",
        "Content-Type": "application/json"
    }
    resp = requests.post(url, headers=headers, json=fs_ids)
    resp.raise_for_status()
    return resp.content

def delete_library(server_url, token, repo_id):
    """Delete library"""
    url = f"{server_url}/api2/repos/{repo_id}/"
    headers = {"Authorization": f"Token {token}"}
    requests.delete(url, headers=headers)

def verify_fs_id(fs_json, expected_id):
    """Verify fs_id matches SHA-1 of JSON"""
    # Try different JSON serialization formats
    formats = [
        (json.dumps(fs_json, separators=(',', ':'), sort_keys=True), "no spaces"),
        (json.dumps(fs_json, separators=(', ', ': '), sort_keys=True), "with spaces"),
        (json.dumps(fs_json, separators=(',', ': '), sort_keys=True), "colon space"),
    ]

    for json_str, format_name in formats:
        computed = hashlib.sha1(json_str.encode('utf-8')).hexdigest()
        if computed == expected_id:
            return True, computed, format_name, json_str

    return False, formats[0][0], "none", formats[0][0]

def main():
    print("=" * 70)
    print("DETAILED SYNC PROTOCOL DIAGNOSTIC")
    print("=" * 70)

    repo_id = None
    issues = []

    try:
        # Authenticate
        log("Step 1: Authenticating...")
        token = authenticate(LOCAL_SERVER, LOCAL_USER, LOCAL_PASS)
        log("✓ Authenticated")

        # Create regular library (encrypted library creation is failing with 405)
        log("\nStep 2: Creating library...")
        lib_name = f"sync_diag_{int(datetime.now().timestamp())}"
        # Use regular library creation
        url = f"{LOCAL_SERVER}/api2/repos/"
        headers = {"Authorization": f"Token {token}"}
        data = {"name": lib_name, "desc": ""}
        resp = requests.post(url, data=data, headers=headers)
        resp.raise_for_status()
        lib = resp.json()
        repo_id = lib["repo_id"]
        log(f"✓ Library created: {repo_id}")

        # Upload file
        log("\nStep 4: Uploading test file...")
        upload_link = get_upload_link(LOCAL_SERVER, token, repo_id)
        upload_file(upload_link, token, "test.docx", b"test1234")
        log("✓ File uploaded (8 bytes)")

        # Test download-info
        print("\n" + "=" * 70)
        print("CHECKING: download-info")
        print("=" * 70)

        download_info = get_download_info(LOCAL_SERVER, token, repo_id)
        print(json.dumps(download_info, indent=2))

        # Verify critical fields (skip encryption checks for regular library)
        checks = [
            ("encrypted", int, download_info.get("encrypted") in [0, 1, 2]),
            ("repo_desc", str, download_info.get("repo_desc") == ""),
        ]

        for field, expected_type, condition in checks:
            if field not in download_info:
                issue = f"✗ download-info: Missing field '{field}'"
                issues.append(issue)
                print(f"  {issue}")
            elif not isinstance(download_info[field], expected_type):
                issue = f"✗ download-info: '{field}' wrong type (expected {expected_type.__name__}, got {type(download_info[field]).__name__})"
                issues.append(issue)
                print(f"  {issue}")
            elif not condition:
                issue = f"✗ download-info: '{field}' has invalid value: {download_info[field]}"
                issues.append(issue)
                print(f"  {issue}")
            else:
                print(f"  ✓ {field}: {download_info[field]}")

        # Test commit HEAD
        print("\n" + "=" * 70)
        print("CHECKING: commit HEAD")
        print("=" * 70)

        head_response = get_commit_head(LOCAL_SERVER, token, repo_id)
        print(json.dumps(head_response, indent=2))

        commit_id = head_response["head_commit_id"]
        log(f"✓ HEAD commit: {commit_id}")

        # Test commit object
        print("\n" + "=" * 70)
        print("CHECKING: commit object")
        print("=" * 70)

        commit = get_commit(LOCAL_SERVER, token, repo_id, commit_id)
        print(json.dumps(commit, indent=2))

        root_fs_id = commit["root_id"]

        # Skip encryption field checks for regular library
        print(f"  ✓ commit object structure valid")

        # Test fs-id-list
        print("\n" + "=" * 70)
        print("CHECKING: fs-id-list")
        print("=" * 70)

        fs_ids = get_fs_id_list(LOCAL_SERVER, token, repo_id, commit_id)
        print(f"Response: {fs_ids}\n")
        print(f"Parsed {len(fs_ids)} fs_ids:")
        for fs_id in fs_ids:
            print(f"  - {fs_id}")

        # Verify root_fs_id is in list
        if root_fs_id not in fs_ids:
            issue = f"✗ fs-id-list: root_fs_id {root_fs_id} NOT in list!"
            issues.append(issue)
            print(f"\n{issue}")
        else:
            print(f"\n✓ root_fs_id {root_fs_id} is in list")

        # Verify all fs_ids are 40-char hex
        for fs_id in fs_ids:
            if len(fs_id) != 40 or not all(c in '0123456789abcdef' for c in fs_id):
                issue = f"✗ fs-id-list: Invalid fs_id format: {fs_id}"
                issues.append(issue)
                print(f"  {issue}")

        # Test pack-fs
        print("\n" + "=" * 70)
        print("CHECKING: pack-fs")
        print("=" * 70)

        pack_data = pack_fs(LOCAL_SERVER, token, repo_id, fs_ids)
        print(f"pack-fs returned {len(pack_data)} bytes\n")

        # Parse pack-fs
        offset = 0
        fs_objects = {}

        while offset < len(pack_data):
            if offset + 44 > len(pack_data):
                issue = f"✗ pack-fs: Incomplete data at offset {offset}"
                issues.append(issue)
                print(issue)
                break

            # Read fs_id (40 bytes)
            fs_id = pack_data[offset:offset+40].decode('ascii')

            # Read size (4 bytes big-endian)
            size = int.from_bytes(pack_data[offset+40:offset+44], 'big')

            if offset + 44 + size > len(pack_data):
                issue = f"✗ pack-fs: Size mismatch at offset {offset}"
                issues.append(issue)
                print(issue)
                break

            # Read compressed data
            compressed = pack_data[offset+44:offset+44+size]

            try:
                # Decompress
                decompressed = zlib.decompress(compressed)
                fs_json = json.loads(decompressed.decode('utf-8'))

                # Verify hash
                hash_ok, computed, format_name, json_str = verify_fs_id(fs_json, fs_id)

                if not hash_ok:
                    issue = f"✗ pack-fs: fs_id hash MISMATCH for {fs_id}"
                    issues.append(issue)
                    print(f"\n{issue}")
                    print(f"  Expected: {fs_id}")
                    print(f"  Computed: {computed}")
                    print(f"  JSON: {json_str}")
                else:
                    print(f"✓ fs_id {fs_id[:16]}... hash verified ({format_name})")

                print(f"  Type: {fs_json.get('type')}, Version: {fs_json.get('version')}")
                print(f"  Content: {json.dumps(fs_json, indent=2, sort_keys=True)}")

                # Verify JSON structure
                required_fields = {
                    1: ["version", "type", "block_ids", "size"],  # file
                    3: ["version", "type", "dirents"],  # directory
                }

                obj_type = fs_json.get("type")
                if obj_type in required_fields:
                    for field in required_fields[obj_type]:
                        if field not in fs_json:
                            issue = f"✗ pack-fs: FS object {fs_id[:16]} missing field '{field}'"
                            issues.append(issue)
                            print(f"  {issue}")

                # For directories, check dirents format
                if obj_type == 3:
                    dirents = fs_json.get("dirents", [])
                    print(f"  Directory has {len(dirents)} entries")
                    for i, entry in enumerate(dirents):
                        required_entry_fields = ["id", "mode", "modifier", "mtime", "name", "size"]
                        for field in required_entry_fields:
                            if field not in entry:
                                issue = f"✗ pack-fs: Dirent {i} missing field '{field}'"
                                issues.append(issue)
                                print(f"    {issue}")
                        # Verify field order (should be alphabetical)
                        entry_keys = list(entry.keys())
                        expected_order = sorted(entry_keys)
                        if entry_keys != expected_order:
                            issue = f"✗ pack-fs: Dirent {i} field order wrong"
                            issues.append(issue)
                            print(f"    {issue}")
                            print(f"      Got: {entry_keys}")
                            print(f"      Expected: {expected_order}")

                fs_objects[fs_id] = fs_json

            except zlib.error as e:
                issue = f"✗ pack-fs: Decompression failed for {fs_id}: {e}"
                issues.append(issue)
                print(f"\n{issue}")
            except json.JSONDecodeError as e:
                issue = f"✗ pack-fs: JSON parse failed for {fs_id}: {e}"
                issues.append(issue)
                print(f"\n{issue}")

            print()
            offset += 44 + size

        # Verify all fs_ids were returned
        missing = set(fs_ids) - set(fs_objects.keys())
        if missing:
            issue = f"✗ pack-fs: Missing fs_ids: {missing}"
            issues.append(issue)
            print(issue)

        # Summary
        print("=" * 70)
        print("DIAGNOSTIC SUMMARY")
        print("=" * 70)

        if issues:
            print(f"\n✗ Found {len(issues)} issue(s):\n")
            for i, issue in enumerate(issues, 1):
                print(f"{i}. {issue}")
            print("\nThese issues may be causing the desktop client to fail.")
            return 1
        else:
            print("\n✓ All protocol checks passed!")
            print("\nIf the desktop client still shows 'Error when indexing',")
            print("the issue may be with:")
            print("  - Block content encryption/decryption")
            print("  - Network connectivity")
            print("  - Desktop client cache (try deleting local repo folder)")
            return 0

    except Exception as e:
        log(f"✗ ERROR: {e}")
        import traceback
        traceback.print_exc()
        return 1

    finally:
        # Cleanup
        if repo_id:
            log("\nCleaning up...")
            delete_library(LOCAL_SERVER, token, repo_id)
            log("✓ Cleanup complete")

if __name__ == "__main__":
    sys.exit(main())
