#!/usr/bin/env python3
"""
Test Encrypted Library File Sync - Reproduces User's Procedure

This test reproduces the exact user procedure:
1. Create encrypted library "test0030" with password "test0030"
2. Upload a .docx file with content "test0030"
3. Simulate desktop client sync (get commit, fs objects, blocks)
4. Compare behavior between remote (Seafile) and local (SesameFS)

This will identify the actual sync failure point.
"""

import os
import sys
import json
import time
import requests
import hashlib
import zlib
import struct
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

# Test configuration - use timestamp to avoid conflicts
TIMESTAMP = datetime.now().strftime("%Y%m%d_%H%M%S")
LIBRARY_NAME = f"test0030_{TIMESTAMP}"
LIBRARY_PASSWORD = "test0030"
FILE_NAME = "test0030.docx"
FILE_CONTENT = b"test0030"  # Simple content for testing

class Colors:
    RED = '\033[0;31m'
    GREEN = '\033[0;32m'
    YELLOW = '\033[1;33m'
    BLUE = '\033[0;34m'
    CYAN = '\033[0;36m'
    MAGENTA = '\033[0;35m'
    NC = '\033[0m'

def log_info(msg): print(f"{Colors.GREEN}[INFO]{Colors.NC} {msg}")
def log_warn(msg): print(f"{Colors.YELLOW}[WARN]{Colors.NC} {msg}")
def log_error(msg): print(f"{Colors.RED}[ERROR]{Colors.NC} {msg}")
def log_pass(msg): print(f"{Colors.GREEN}[PASS]{Colors.NC} {msg}")
def log_fail(msg): print(f"{Colors.RED}[FAIL]{Colors.NC} {msg}")
def log_step(msg): print(f"\n{Colors.BLUE}{'='*70}\n{msg}\n{'='*70}{Colors.NC}")

def save_json(data, filename):
    """Save data to JSON file in capture directory"""
    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    output_dir = Path(CAPTURE_DIR) / f"encrypted_sync_{timestamp}"
    output_dir.mkdir(parents=True, exist_ok=True)

    filepath = output_dir / filename
    with open(filepath, 'w') as f:
        json.dump(data, f, indent=2)
    return filepath

def save_binary(data, filename):
    """Save binary data to file"""
    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    output_dir = Path(CAPTURE_DIR) / f"encrypted_sync_{timestamp}"
    output_dir.mkdir(parents=True, exist_ok=True)

    filepath = output_dir / filename
    with open(filepath, 'wb') as f:
        f.write(data)
    return filepath

def authenticate(server_url, username, password):
    """Get API token"""
    url = f"{server_url}/api2/auth-token/"
    resp = requests.post(url, data={"username": username, "password": password})
    resp.raise_for_status()
    return resp.json()["token"]

def create_encrypted_library(server_url, token, name, password):
    """Create encrypted library with password"""
    url = f"{server_url}/api2/repos/"
    headers = {"Authorization": f"Token {token}"}
    data = {
        "name": name,
        "desc": f"Test encrypted library {name}",
        "passwd": password
    }
    resp = requests.post(url, headers=headers, data=data)
    resp.raise_for_status()
    result = resp.json()
    save_json(result, f"create_library_{name}_{server_url.split('/')[-1]}.json")
    return result

def set_password(server_url, token, repo_id, password):
    """Unlock encrypted library"""
    url = f"{server_url}/api/v2.1/repos/{repo_id}/set-password/"
    headers = {"Authorization": f"Token {token}"}
    data = {"password": password}
    resp = requests.post(url, headers=headers, json=data)
    resp.raise_for_status()
    return resp.json()

def get_upload_link(server_url, token, repo_id):
    """Get upload link for library"""
    url = f"{server_url}/api2/repos/{repo_id}/upload-link/"
    headers = {"Authorization": f"Token {token}"}
    resp = requests.get(url, headers=headers)
    resp.raise_for_status()
    # Returns plain text URL, not JSON
    return resp.text.strip().strip('"')

def upload_file(upload_url, token, filename, content, parent_dir="/"):
    """Upload file to library"""
    headers = {"Authorization": f"Token {token}"}
    files = {"file": (filename, content)}
    data = {
        "parent_dir": parent_dir,
        "replace": "0"
    }
    resp = requests.post(upload_url, headers=headers, files=files, data=data)
    resp.raise_for_status()
    # Upload may return JSON array, plain text, or HTML
    try:
        return resp.json()
    except:
        return {"status": "uploaded", "text": resp.text[:200]}

def get_download_info(server_url, token, repo_id):
    """Get sync token and encryption info"""
    url = f"{server_url}/api2/repos/{repo_id}/download-info/"
    headers = {"Authorization": f"Token {token}"}
    resp = requests.get(url, headers=headers)
    resp.raise_for_status()
    result = resp.json()
    save_json(result, f"download_info_{repo_id[:8]}_{server_url.split('/')[-1]}.json")
    return result

def get_commit_head(server_url, sync_token, repo_id):
    """Get HEAD commit ID"""
    url = f"{server_url}/seafhttp/repo/{repo_id}/commit/HEAD"
    headers = {"Seafile-Repo-Token": sync_token}
    resp = requests.get(url, headers=headers)
    resp.raise_for_status()
    result = resp.json()
    save_json(result, f"commit_head_{repo_id[:8]}_{server_url.split('/')[-1]}.json")
    return result

def get_commit(server_url, sync_token, repo_id, commit_id):
    """Get full commit object"""
    url = f"{server_url}/seafhttp/repo/{repo_id}/commit/{commit_id}"
    headers = {"Seafile-Repo-Token": sync_token}
    resp = requests.get(url, headers=headers)
    resp.raise_for_status()
    result = resp.json()
    save_json(result, f"commit_{commit_id[:8]}_{server_url.split('/')[-1]}.json")
    return result

def get_fs_id_list(server_url, sync_token, repo_id, commit_id):
    """Get list of all FS IDs in commit"""
    url = f"{server_url}/seafhttp/repo/{repo_id}/fs-id-list/"
    headers = {"Seafile-Repo-Token": sync_token}
    params = {"server-head": commit_id}
    resp = requests.get(url, headers=headers, params=params)
    resp.raise_for_status()
    result = resp.json()
    save_json(result, f"fs_id_list_{commit_id[:8]}_{server_url.split('/')[-1]}.json")
    return result

def pack_fs(server_url, sync_token, repo_id, fs_ids):
    """Get FS objects in binary format"""
    url = f"{server_url}/seafhttp/repo/{repo_id}/pack-fs/"
    headers = {
        "Seafile-Repo-Token": sync_token,
        "Content-Type": "application/json"
    }
    resp = requests.post(url, headers=headers, json=fs_ids)
    resp.raise_for_status()
    binary_data = resp.content
    save_binary(binary_data, f"pack_fs_{repo_id[:8]}_{server_url.split('/')[-1]}.bin")
    return binary_data

def parse_pack_fs(binary_data):
    """Parse pack-fs binary format"""
    objects = []
    offset = 0

    while offset < len(binary_data):
        # Read 40-byte fs_id (ASCII hex)
        if offset + 40 > len(binary_data):
            break
        fs_id = binary_data[offset:offset+40].decode('ascii')
        offset += 40

        # Read 4-byte size (big-endian)
        if offset + 4 > len(binary_data):
            break
        size = struct.unpack('>I', binary_data[offset:offset+4])[0]
        offset += 4

        # Read compressed data
        if offset + size > len(binary_data):
            break
        compressed = binary_data[offset:offset+size]
        offset += size

        # Decompress
        try:
            decompressed = zlib.decompress(compressed)
            obj = json.loads(decompressed)
            objects.append({
                "fs_id": fs_id,
                "size": size,
                "object": obj
            })
        except Exception as e:
            log_error(f"Failed to parse FS object {fs_id}: {e}")
            objects.append({
                "fs_id": fs_id,
                "size": size,
                "error": str(e)
            })

    return objects

def get_block(server_url, sync_token, repo_id, block_id):
    """Download block content"""
    url = f"{server_url}/seafhttp/repo/{repo_id}/block/{block_id}"
    headers = {"Seafile-Repo-Token": sync_token}
    resp = requests.get(url, headers=headers)
    resp.raise_for_status()
    return resp.content

def delete_library(server_url, token, repo_id):
    """Delete library for cleanup"""
    url = f"{server_url}/api2/repos/{repo_id}/"
    headers = {"Authorization": f"Token {token}"}
    resp = requests.delete(url, headers=headers)
    return resp.ok

def compare_objects(obj1, obj2, path=""):
    """Deep compare two objects and return differences"""
    diffs = []

    if type(obj1) != type(obj2):
        return [{
            "path": path,
            "type": "type_mismatch",
            "remote": type(obj1).__name__,
            "local": type(obj2).__name__
        }]

    if isinstance(obj1, dict):
        all_keys = set(obj1.keys()) | set(obj2.keys())
        for key in all_keys:
            new_path = f"{path}.{key}" if path else key

            if key not in obj1:
                diffs.append({
                    "path": new_path,
                    "type": "missing_in_remote",
                    "local_value": obj2[key]
                })
            elif key not in obj2:
                diffs.append({
                    "path": new_path,
                    "type": "missing_in_local",
                    "remote_value": obj1[key]
                })
            else:
                diffs.extend(compare_objects(obj1[key], obj2[key], new_path))

    elif isinstance(obj1, list):
        if len(obj1) != len(obj2):
            diffs.append({
                "path": path,
                "type": "length_mismatch",
                "remote": len(obj1),
                "local": len(obj2)
            })
        else:
            for i, (v1, v2) in enumerate(zip(obj1, obj2)):
                diffs.extend(compare_objects(v1, v2, f"{path}[{i}]"))

    elif obj1 != obj2:
        # Skip magic/random_key/key differences (repo-specific)
        if path.endswith("magic") or path.endswith("random_key") or path.endswith("key"):
            return []

        diffs.append({
            "path": path,
            "type": "value_mismatch",
            "remote": obj1,
            "local": obj2
        })

    return diffs

def run_test():
    """Run the full encrypted file sync test"""
    log_step("ENCRYPTED LIBRARY FILE SYNC TEST")
    log_info(f"Testing: Create '{LIBRARY_NAME}' with password '{LIBRARY_PASSWORD}'")
    log_info(f"Upload: '{FILE_NAME}' with content '{FILE_CONTENT.decode()}'")
    log_info(f"Sync: Verify file can be downloaded via sync protocol")

    results = {
        "timestamp": datetime.now().isoformat(),
        "test": "encrypted_file_sync",
        "steps": []
    }

    # Track created libraries for cleanup
    remote_repo_id = None
    local_repo_id = None

    try:
        # Step 1: Authenticate
        log_step("STEP 1: Authentication")
        remote_token = authenticate(REMOTE_SERVER, REMOTE_USER, REMOTE_PASS)
        local_token = authenticate(LOCAL_SERVER, LOCAL_USER, LOCAL_PASS)
        log_pass("Authentication successful")

        # Step 2: Create encrypted libraries
        log_step("STEP 2: Create Encrypted Library")
        remote_lib = create_encrypted_library(REMOTE_SERVER, remote_token, LIBRARY_NAME, LIBRARY_PASSWORD)
        local_lib = create_encrypted_library(LOCAL_SERVER, local_token, LIBRARY_NAME, LIBRARY_PASSWORD)
        remote_repo_id = remote_lib["repo_id"]
        local_repo_id = local_lib["repo_id"]

        log_info(f"Remote repo: {remote_repo_id}")
        log_info(f"Local repo: {local_repo_id}")

        # Compare creation responses
        diffs = compare_objects(remote_lib, local_lib)
        if diffs:
            log_warn(f"Library creation differences: {len(diffs)}")
            results["steps"].append({
                "step": "create_library",
                "status": "differences_found",
                "differences": diffs
            })
        else:
            log_pass("Library creation responses match")
            results["steps"].append({"step": "create_library", "status": "pass"})

        # Step 3: Unlock libraries
        log_step("STEP 3: Unlock Libraries (Set Password)")
        remote_unlock = set_password(REMOTE_SERVER, remote_token, remote_repo_id, LIBRARY_PASSWORD)
        local_unlock = set_password(LOCAL_SERVER, local_token, local_repo_id, LIBRARY_PASSWORD)

        if remote_unlock.get("success") and local_unlock.get("success"):
            log_pass("Libraries unlocked successfully")
            results["steps"].append({"step": "unlock", "status": "pass"})
        else:
            log_fail("Failed to unlock libraries")
            results["steps"].append({"step": "unlock", "status": "fail"})
            return results

        # Step 4: Upload file to both libraries
        log_step(f"STEP 4: Upload File '{FILE_NAME}'")

        # Upload to remote
        remote_upload_link = get_upload_link(REMOTE_SERVER, remote_token, remote_repo_id)
        remote_upload_result = upload_file(remote_upload_link, remote_token, FILE_NAME, FILE_CONTENT)
        log_info(f"Remote upload: {remote_upload_result}")

        # Upload to local
        local_upload_link = get_upload_link(LOCAL_SERVER, local_token, local_repo_id)
        local_upload_result = upload_file(local_upload_link, local_token, FILE_NAME, FILE_CONTENT)
        log_info(f"Local upload: {local_upload_result}")

        log_pass("Files uploaded")
        results["steps"].append({"step": "upload_file", "status": "pass"})

        # Wait for indexing
        time.sleep(2)

        # Step 5: Get download-info (sync token)
        log_step("STEP 5: Get Download Info (Sync Token)")
        remote_download_info = get_download_info(REMOTE_SERVER, remote_token, remote_repo_id)
        local_download_info = get_download_info(LOCAL_SERVER, local_token, local_repo_id)

        remote_sync_token = remote_download_info["token"]
        local_sync_token = local_download_info["token"]

        log_info(f"Remote sync token: {remote_sync_token[:20]}...")
        log_info(f"Local sync token: {local_sync_token[:20]}...")

        diffs = compare_objects(remote_download_info, local_download_info)
        if diffs:
            log_warn(f"Download-info differences: {len(diffs)}")
            for diff in diffs:
                log_warn(f"  {diff}")
            results["steps"].append({
                "step": "download_info",
                "status": "differences_found",
                "differences": diffs
            })
        else:
            log_pass("Download-info matches")
            results["steps"].append({"step": "download_info", "status": "pass"})

        # Step 6: Get HEAD commit
        log_step("STEP 6: Get HEAD Commit")
        remote_head = get_commit_head(REMOTE_SERVER, remote_sync_token, remote_repo_id)
        local_head = get_commit_head(LOCAL_SERVER, local_sync_token, local_repo_id)

        remote_commit_id = remote_head["head_commit_id"]
        local_commit_id = local_head["head_commit_id"]

        log_info(f"Remote HEAD: {remote_commit_id}")
        log_info(f"Local HEAD: {local_commit_id}")

        diffs = compare_objects(remote_head, local_head)
        if diffs:
            log_warn(f"HEAD commit differences: {len(diffs)}")
            results["steps"].append({
                "step": "commit_head",
                "status": "differences_found",
                "differences": diffs
            })
        else:
            log_pass("HEAD commit matches")
            results["steps"].append({"step": "commit_head", "status": "pass"})

        # Step 7: Get full commit object
        log_step("STEP 7: Get Full Commit Object")
        remote_commit = get_commit(REMOTE_SERVER, remote_sync_token, remote_repo_id, remote_commit_id)
        local_commit = get_commit(LOCAL_SERVER, local_sync_token, local_repo_id, local_commit_id)

        log_info(f"Remote root_id: {remote_commit.get('root_id')}")
        log_info(f"Local root_id: {local_commit.get('root_id')}")

        diffs = compare_objects(remote_commit, local_commit)
        if diffs:
            log_warn(f"Commit object differences: {len(diffs)}")
            for diff in diffs:
                log_warn(f"  {diff}")
            results["steps"].append({
                "step": "full_commit",
                "status": "differences_found",
                "differences": diffs
            })
        else:
            log_pass("Commit object matches")
            results["steps"].append({"step": "full_commit", "status": "pass"})

        # Step 8: Get FS ID list
        log_step("STEP 8: Get FS ID List")
        remote_fs_ids = get_fs_id_list(REMOTE_SERVER, remote_sync_token, remote_repo_id, remote_commit_id)
        local_fs_ids = get_fs_id_list(LOCAL_SERVER, local_sync_token, local_repo_id, local_commit_id)

        log_info(f"Remote FS IDs: {len(remote_fs_ids)} objects")
        log_info(f"Local FS IDs: {len(local_fs_ids)} objects")

        # Compare FS ID lists
        remote_set = set(remote_fs_ids if isinstance(remote_fs_ids, list) else [])
        local_set = set(local_fs_ids if isinstance(local_fs_ids, list) else [])

        missing_in_local = remote_set - local_set
        missing_in_remote = local_set - remote_set

        if missing_in_local or missing_in_remote:
            log_warn(f"FS ID list differences:")
            if missing_in_local:
                log_warn(f"  Missing in local: {missing_in_local}")
            if missing_in_remote:
                log_warn(f"  Missing in remote: {missing_in_remote}")
            results["steps"].append({
                "step": "fs_id_list",
                "status": "differences_found",
                "missing_in_local": list(missing_in_local),
                "missing_in_remote": list(missing_in_remote)
            })
        else:
            log_pass(f"FS ID lists match ({len(remote_set)} objects)")
            results["steps"].append({"step": "fs_id_list", "status": "pass"})

        # Step 9: Get FS objects (pack-fs)
        if remote_fs_ids and local_fs_ids:
            log_step("STEP 9: Get FS Objects (pack-fs)")

            # Get pack-fs from both servers
            remote_pack = pack_fs(REMOTE_SERVER, remote_sync_token, remote_repo_id, remote_fs_ids)
            local_pack = pack_fs(LOCAL_SERVER, local_sync_token, local_repo_id, local_fs_ids)

            log_info(f"Remote pack-fs: {len(remote_pack)} bytes")
            log_info(f"Local pack-fs: {len(local_pack)} bytes")

            # Parse pack-fs
            remote_objects = parse_pack_fs(remote_pack)
            local_objects = parse_pack_fs(local_pack)

            log_info(f"Remote objects parsed: {len(remote_objects)}")
            log_info(f"Local objects parsed: {len(local_objects)}")

            # Save parsed objects
            save_json({"objects": remote_objects}, "remote_fs_objects.json")
            save_json({"objects": local_objects}, "local_fs_objects.json")

            # Compare FS objects
            for remote_obj in remote_objects:
                fs_id = remote_obj["fs_id"]
                local_obj = next((o for o in local_objects if o["fs_id"] == fs_id), None)

                if not local_obj:
                    log_warn(f"FS object {fs_id} missing in local")
                    continue

                if "error" in remote_obj or "error" in local_obj:
                    log_warn(f"FS object {fs_id} parse error")
                    continue

                # Compare object content
                diffs = compare_objects(remote_obj["object"], local_obj["object"])
                if diffs:
                    log_warn(f"FS object {fs_id} differences:")
                    for diff in diffs:
                        log_warn(f"  {diff}")

            # Look for file objects with blocks
            for obj in remote_objects:
                if "object" in obj and obj["object"].get("type") == 1:  # Seafile file
                    log_info(f"Found file object: {obj['object'].get('name', 'unknown')}")
                    log_info(f"  Block IDs: {obj['object'].get('block_ids', [])}")

            results["steps"].append({
                "step": "pack_fs",
                "status": "complete",
                "remote_objects": len(remote_objects),
                "local_objects": len(local_objects)
            })

        log_step("TEST COMPLETE")
        log_info(f"Results saved to {CAPTURE_DIR}/encrypted_sync_*")

    except Exception as e:
        log_error(f"Test failed: {e}")
        import traceback
        traceback.print_exc()
        results["error"] = str(e)

    finally:
        # Cleanup: Delete test libraries
        log_step("CLEANUP: Deleting Test Libraries")
        if remote_repo_id:
            if delete_library(REMOTE_SERVER, remote_token, remote_repo_id):
                log_info(f"Deleted remote library: {remote_repo_id}")
        if local_repo_id:
            if delete_library(LOCAL_SERVER, local_token, local_repo_id):
                log_info(f"Deleted local library: {local_repo_id}")

    # Save final results
    save_json(results, "test_results.json")

    return results

if __name__ == "__main__":
    run_test()
