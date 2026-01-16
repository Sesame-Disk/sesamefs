#!/usr/bin/env python3
"""
Complete encrypted library sync protocol comparison test

Creates fresh encrypted libraries on both remote (stock Seafile) and local server,
uploads files, and compares all sync protocol responses.
"""

import requests
import json
import zlib
import hashlib
import sys
import os
from datetime import datetime

# Servers
REMOTE_SERVER = "https://app.nihaoconsult.com"
LOCAL_SERVER = "http://host.docker.internal:8080"

# Credentials (from .seafile-reference.md)
REMOTE_USER = "abel.aguzmans@gmail.com"
REMOTE_PASS = "Qwerty123!"
LOCAL_USER = "abel.aguzmans@gmail.com"
LOCAL_PASS = "dev-token-123"

# Test config
TIMESTAMP = datetime.now().strftime("%Y%m%d_%H%M%S")
LIBRARY_NAME = f"sync_test_{TIMESTAMP}"
LIBRARY_PASSWORD = "test123"
FILE_NAME = "test_document.txt"
FILE_CONTENT = b"Hello from sync test! This is test content for encrypted library sync verification."

def log(msg):
    """Print with timestamp"""
    print(f"[{datetime.now().strftime('%H:%M:%S')}] {msg}")

def section(title):
    """Print section header"""
    print("\n" + "=" * 70)
    print(title)
    print("=" * 70)

def authenticate(server_url, username, password):
    """Authenticate and get token"""
    url = f"{server_url}/api2/auth-token/"
    data = {"username": username, "password": password}
    resp = requests.post(url, data=data)
    resp.raise_for_status()
    return resp.json()["token"]

def create_encrypted_library(server_url, token, name, password):
    """Create encrypted library"""
    url = f"{server_url}/api2/repos/"
    headers = {"Authorization": f"Token {token}"}
    data = {"name": name, "passwd": password}
    resp = requests.post(url, data=data, headers=headers)
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

def get_protocol_version(server_url, token):
    """Get protocol version"""
    url = f"{server_url}/seafhttp/protocol-version"
    headers = {"Authorization": f"Token {token}"}
    resp = requests.get(url, headers=headers)
    resp.raise_for_status()
    return resp.json()["version"]

def get_commit_head(server_url, token, repo_id):
    """Get HEAD commit"""
    url = f"{server_url}/seafhttp/repo/{repo_id}/commit/HEAD"
    # Try Seafile-Repo-Token header (desktop client method)
    headers = {"Seafile-Repo-Token": token}
    resp = requests.get(url, headers=headers)
    if resp.status_code != 200:
        print(f"ERROR: GET {url} returned {resp.status_code}")
        print(f"Response: {resp.text}")
    resp.raise_for_status()
    return resp.json()

def get_commit(server_url, token, repo_id, commit_id):
    """Get commit object"""
    url = f"{server_url}/seafhttp/repo/{repo_id}/commit/{commit_id}"
    headers = {"Seafile-Repo-Token": token}
    resp = requests.get(url, headers=headers)
    resp.raise_for_status()
    return resp.json()

def get_fs_id_list(server_url, token, repo_id, server_head):
    """Get fs-id-list"""
    url = f"{server_url}/seafhttp/repo/{repo_id}/fs-id-list/"
    headers = {"Seafile-Repo-Token": token}
    params = {"server-head": server_head}
    resp = requests.get(url, headers=headers, params=params)
    resp.raise_for_status()
    # Stock Seafile returns JSON array
    return resp.json()

def pack_fs(server_url, token, repo_id, fs_ids):
    """Get pack-fs"""
    url = f"{server_url}/seafhttp/repo/{repo_id}/pack-fs"
    headers = {
        "Seafile-Repo-Token": token,
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

def parse_pack_fs(pack_data):
    """Parse pack-fs binary data into dict of fs_objects"""
    offset = 0
    fs_objects = {}

    while offset < len(pack_data):
        if offset + 44 > len(pack_data):
            break

        fs_id = pack_data[offset:offset+40].decode('ascii')
        size = int.from_bytes(pack_data[offset+40:offset+44], 'big')

        if offset + 44 + size > len(pack_data):
            break

        compressed = pack_data[offset+44:offset+44+size]
        decompressed = zlib.decompress(compressed)
        fs_json = json.loads(decompressed.decode('utf-8'))

        fs_objects[fs_id] = {
            "json": fs_json,
            "hash": hashlib.sha1(decompressed).hexdigest()
        }

        offset += 44 + size

    return fs_objects

def compare_values(remote_val, local_val, path=""):
    """Compare two values and return differences"""
    diffs = []

    # Skip time-dependent and server-specific fields
    skip_fields = ["mtime", "ctime", "mtime_relative", "token", "head_commit_id",
                   "commit_id", "root_id", "parent_id", "creator_name", "creator",
                   "description", "repo_id", "email", "relay_addr", "relay_id",
                   "relay_port", "repo_name", "magic", "random_key", "key", "salt"]

    field_name = path.split(".")[-1] if "." in path else path
    if field_name in skip_fields:
        return diffs

    if type(remote_val) != type(local_val):
        diffs.append({
            "path": path,
            "issue": "type_mismatch",
            "remote": f"{type(remote_val).__name__}: {remote_val}",
            "local": f"{type(local_val).__name__}: {local_val}"
        })
    elif isinstance(remote_val, dict):
        all_keys = set(remote_val.keys()) | set(local_val.keys())
        for key in all_keys:
            new_path = f"{path}.{key}" if path else key
            if key not in remote_val:
                diffs.append({
                    "path": new_path,
                    "issue": "missing_in_remote",
                    "local": local_val[key]
                })
            elif key not in local_val:
                diffs.append({
                    "path": new_path,
                    "issue": "missing_in_local",
                    "remote": remote_val[key]
                })
            else:
                diffs.extend(compare_values(remote_val[key], local_val[key], new_path))
    elif isinstance(remote_val, list):
        if len(remote_val) != len(local_val):
            diffs.append({
                "path": path,
                "issue": "length_mismatch",
                "remote": len(remote_val),
                "local": len(local_val)
            })
        else:
            for i, (r, l) in enumerate(zip(remote_val, local_val)):
                diffs.extend(compare_values(r, l, f"{path}[{i}]"))
    elif remote_val != local_val:
        diffs.append({
            "path": path,
            "issue": "value_mismatch",
            "remote": remote_val,
            "local": local_val
        })

    return diffs

def main():
    print("=" * 70)
    print("ENCRYPTED LIBRARY SYNC PROTOCOL COMPARISON")
    print("=" * 70)
    print(f"Library: {LIBRARY_NAME}")
    print(f"Password: {LIBRARY_PASSWORD}")
    print(f"File: {FILE_NAME} ({len(FILE_CONTENT)} bytes)")
    print()

    remote_repo_id = None
    local_repo_id = None
    all_diffs = []

    try:
        # Step 1: Authenticate to both servers
        section("STEP 1: Authentication")
        log("Authenticating to remote server...")
        remote_token = authenticate(REMOTE_SERVER, REMOTE_USER, REMOTE_PASS)
        log(f"✓ Remote authenticated")

        log("Authenticating to local server...")
        local_token = authenticate(LOCAL_SERVER, LOCAL_USER, LOCAL_PASS)
        log(f"✓ Local authenticated")

        # Step 2: Create encrypted libraries
        section("STEP 2: Create Encrypted Libraries")
        log(f"Creating encrypted library on remote server...")
        remote_lib = create_encrypted_library(REMOTE_SERVER, remote_token, LIBRARY_NAME, LIBRARY_PASSWORD)
        remote_repo_id = remote_lib["repo_id"]
        log(f"✓ Remote library: {remote_repo_id}")

        log(f"Creating encrypted library on local server...")
        local_lib = create_encrypted_library(LOCAL_SERVER, local_token, LIBRARY_NAME, LIBRARY_PASSWORD)
        local_repo_id = local_lib["repo_id"]
        log(f"✓ Local library: {local_repo_id}")

        # Step 3: Unlock libraries
        section("STEP 3: Unlock Libraries")
        log("Unlocking remote library...")
        set_password(REMOTE_SERVER, remote_token, remote_repo_id, LIBRARY_PASSWORD)
        log("✓ Remote unlocked")

        log("Unlocking local library...")
        set_password(LOCAL_SERVER, local_token, local_repo_id, LIBRARY_PASSWORD)
        log("✓ Local unlocked")

        # Step 4: Upload files
        section("STEP 4: Upload Files")
        log(f"Uploading '{FILE_NAME}' to remote...")
        remote_upload_link = get_upload_link(REMOTE_SERVER, remote_token, remote_repo_id)
        upload_file(remote_upload_link, remote_token, FILE_NAME, FILE_CONTENT)
        log("✓ Remote file uploaded")

        log(f"Uploading '{FILE_NAME}' to local...")
        local_upload_link = get_upload_link(LOCAL_SERVER, local_token, local_repo_id)
        upload_file(local_upload_link, local_token, FILE_NAME, FILE_CONTENT)
        log("✓ Local file uploaded")

        # Wait for commits to be created (encrypted libraries may take longer)
        import time
        log("Waiting 5 seconds for commits to be created...")
        time.sleep(5)

        # Step 5: Compare download-info
        section("STEP 5: Compare download-info")
        remote_info = get_download_info(REMOTE_SERVER, remote_token, remote_repo_id)
        local_info = get_download_info(LOCAL_SERVER, local_token, local_repo_id)

        # CRITICAL: For encrypted libraries, use the sync token from download-info
        remote_sync_token = remote_info.get("token", remote_token)
        local_sync_token = local_info.get("token", local_token)

        log(f"Using remote sync token: {remote_sync_token[:20]}...")
        log(f"Using local sync token: {local_sync_token[:20]}...")

        diffs = compare_values(remote_info, local_info)
        if diffs:
            print(f"Found {len(diffs)} difference(s):")
            for diff in diffs:
                print(f"  {diff['path']}: {diff['issue']}")
                if 'remote' in diff:
                    print(f"    Remote: {diff['remote']}")
                if 'local' in diff:
                    print(f"    Local: {diff['local']}")
            all_diffs.extend(diffs)
        else:
            log("✓ download-info matches")

        # Step 6: Compare protocol version
        section("STEP 6: Compare Protocol Version")
        remote_version = get_protocol_version(REMOTE_SERVER, remote_sync_token)
        local_version = get_protocol_version(LOCAL_SERVER, local_sync_token)
        print(f"Remote: {remote_version}, Local: {local_version}")
        if remote_version != local_version:
            all_diffs.append({"path": "protocol_version", "issue": "mismatch", "remote": remote_version, "local": local_version})

        # Step 7: Get and compare commits
        section("STEP 7: Compare Commit Objects")
        remote_head = get_commit_head(REMOTE_SERVER, remote_sync_token, remote_repo_id)
        local_head = get_commit_head(LOCAL_SERVER, local_sync_token, local_repo_id)

        remote_commit_id = remote_head["head_commit_id"]
        local_commit_id = local_head["head_commit_id"]

        remote_commit = get_commit(REMOTE_SERVER, remote_sync_token, remote_repo_id, remote_commit_id)
        local_commit = get_commit(LOCAL_SERVER, local_sync_token, local_repo_id, local_commit_id)

        print("\nRemote commit:")
        print(json.dumps(remote_commit, indent=2))
        print("\nLocal commit:")
        print(json.dumps(local_commit, indent=2))

        diffs = compare_values(remote_commit, local_commit, "commit")
        if diffs:
            print(f"\nFound {len(diffs)} difference(s):")
            for diff in diffs:
                print(f"  {diff['path']}: {diff['issue']}")
            all_diffs.extend(diffs)
        else:
            log("\n✓ Commit objects match")

        # Step 8: Compare fs-id-list
        section("STEP 8: Compare fs-id-list")
        remote_fs_ids = get_fs_id_list(REMOTE_SERVER, remote_sync_token, remote_repo_id, remote_commit_id)
        local_fs_ids = get_fs_id_list(LOCAL_SERVER, local_sync_token, local_repo_id, local_commit_id)

        print(f"Remote fs_ids ({len(remote_fs_ids)}): {remote_fs_ids}")
        print(f"Local fs_ids ({len(local_fs_ids)}): {local_fs_ids}")

        if set(remote_fs_ids) != set(local_fs_ids):
            only_remote = set(remote_fs_ids) - set(local_fs_ids)
            only_local = set(local_fs_ids) - set(remote_fs_ids)
            if only_remote:
                print(f"  Only in remote: {only_remote}")
            if only_local:
                print(f"  Only in local: {only_local}")
            all_diffs.append({"path": "fs_id_list", "issue": "mismatch"})
        else:
            log("✓ fs-id-list matches")

        # Step 9: Compare pack-fs
        section("STEP 9: Compare pack-fs")
        remote_pack = pack_fs(REMOTE_SERVER, remote_sync_token, remote_repo_id, remote_fs_ids)
        local_pack = pack_fs(LOCAL_SERVER, local_sync_token, local_repo_id, local_fs_ids)

        remote_objects = parse_pack_fs(remote_pack)
        local_objects = parse_pack_fs(local_pack)

        print(f"Remote pack-fs: {len(remote_objects)} objects")
        print(f"Local pack-fs: {len(local_objects)} objects")

        for fs_id in remote_fs_ids:
            if fs_id not in remote_objects:
                print(f"  ✗ Remote missing fs_id: {fs_id}")
                all_diffs.append({"path": f"pack_fs.{fs_id}", "issue": "missing_in_remote"})
                continue
            if fs_id not in local_objects:
                print(f"  ✗ Local missing fs_id: {fs_id}")
                all_diffs.append({"path": f"pack_fs.{fs_id}", "issue": "missing_in_local"})
                continue

            remote_obj = remote_objects[fs_id]
            local_obj = local_objects[fs_id]

            print(f"\nFS Object: {fs_id[:16]}...")
            print(f"  Remote JSON: {json.dumps(remote_obj['json'], sort_keys=True)}")
            print(f"  Local JSON:  {json.dumps(local_obj['json'], sort_keys=True)}")

            diffs = compare_values(remote_obj['json'], local_obj['json'], f"pack_fs.{fs_id[:16]}")
            if diffs:
                for diff in diffs:
                    print(f"  ✗ {diff['path']}: {diff['issue']}")
                all_diffs.extend(diffs)
            else:
                print(f"  ✓ Match")

        # Summary
        section("SUMMARY")
        if all_diffs:
            print(f"\n✗ FOUND {len(all_diffs)} DIFFERENCE(S):\n")
            for i, diff in enumerate(all_diffs, 1):
                print(f"{i}. {diff['path']}: {diff['issue']}")
                if 'remote' in diff:
                    print(f"   Remote: {diff['remote']}")
                if 'local' in diff:
                    print(f"   Local: {diff['local']}")
            return 1
        else:
            print("\n✓ ALL PROTOCOL RESPONSES MATCH!")
            print("\nThe local server returns identical sync protocol responses")
            print("to the stock Seafile server.")
            return 0

    except Exception as e:
        log(f"✗ ERROR: {e}")
        import traceback
        traceback.print_exc()
        return 1

    finally:
        # Cleanup
        if remote_repo_id:
            log("\nCleaning up remote library...")
            delete_library(REMOTE_SERVER, remote_token, remote_repo_id)
        if local_repo_id:
            log("Cleaning up local library...")
            delete_library(LOCAL_SERVER, local_token, local_repo_id)
        log("Cleanup complete")

if __name__ == "__main__":
    sys.exit(main())
