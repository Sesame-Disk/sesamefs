#!/usr/bin/env python3
"""
Compare encrypted library sync protocol between stock Seafile and SesameFS
This reproduces the exact user scenario: create encrypted library, upload file, try to sync
"""

import requests
import json
import time
import sys
import os
from datetime import datetime

# Servers
REMOTE_SERVER = "https://app.nihaoconsult.com"
LOCAL_SERVER = "http://host.docker.internal:8080"

# Credentials
REMOTE_USER = os.environ.get("REMOTE_USER", "abel.aguzmans@gmail.com")
REMOTE_PASS = os.environ.get("REMOTE_PASS", "")
LOCAL_USER = "admin@sesamefs.local"
LOCAL_PASS = "dev-token-123"

# Test config
TIMESTAMP = datetime.now().strftime("%Y%m%d_%H%M%S")
LIBRARY_NAME = f"sync_test_{TIMESTAMP}"
LIBRARY_PASSWORD = "test123"
FILE_NAME = "test.docx"
FILE_CONTENT = b"test1234"  # 8 bytes

CAPTURE_DIR = "/captures"

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
    data = {
        "name": name,
        "passwd": password,
    }
    resp = requests.post(url, json=data, headers=headers)
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
    return resp.text

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

def compare_json(remote_data, local_data, path=""):
    """Compare two JSON objects and return differences"""
    differences = []

    # Check for type mismatch
    if type(remote_data) != type(local_data):
        differences.append({
            "path": path,
            "type": "type_mismatch",
            "remote": str(type(remote_data).__name__),
            "local": str(type(local_data).__name__)
        })
        return differences

    # Compare dictionaries
    if isinstance(remote_data, dict):
        all_keys = set(remote_data.keys()) | set(local_data.keys())
        for key in all_keys:
            new_path = f"{path}.{key}" if path else key

            if key not in remote_data:
                differences.append({
                    "path": new_path,
                    "type": "missing_in_remote",
                    "local": local_data[key]
                })
            elif key not in local_data:
                differences.append({
                    "path": new_path,
                    "type": "missing_in_local",
                    "remote": remote_data[key]
                })
            else:
                differences.extend(compare_json(remote_data[key], local_data[key], new_path))

    # Compare lists
    elif isinstance(remote_data, list):
        if len(remote_data) != len(local_data):
            differences.append({
                "path": path,
                "type": "length_mismatch",
                "remote": len(remote_data),
                "local": len(local_data)
            })
        for i, (remote_item, local_item) in enumerate(zip(remote_data, local_data)):
            differences.extend(compare_json(remote_item, local_item, f"{path}[{i}]"))

    # Compare values
    else:
        # Skip time-dependent fields
        skip_fields = ["mtime", "ctime", "mtime_relative", "token", "head_commit_id",
                       "commit_id", "root_id", "parent_id", "creator_name", "creator",
                       "description", "repo_id", "email"]
        if path.split(".")[-1] not in skip_fields:
            if remote_data != local_data:
                differences.append({
                    "path": path,
                    "type": "value_mismatch",
                    "remote": remote_data,
                    "local": local_data
                })

    return differences

def main():
    print("=" * 70)
    print("ENCRYPTED LIBRARY SYNC PROTOCOL COMPARISON")
    print("=" * 70)
    print(f"Test: {LIBRARY_NAME}")
    print(f"Password: {LIBRARY_PASSWORD}")
    print(f"File: {FILE_NAME} ({len(FILE_CONTENT)} bytes)")
    print()

    remote_repo_id = None
    local_repo_id = None
    results = {
        "timestamp": datetime.now().isoformat(),
        "test": "encrypted_sync_comparison",
        "steps": []
    }

    try:
        # Authenticate to both servers
        log("Authenticating to remote server...")
        remote_token = authenticate(REMOTE_SERVER, REMOTE_USER, REMOTE_PASS)
        log("✓ Remote authenticated")

        log("Authenticating to local server...")
        local_token = authenticate(LOCAL_SERVER, LOCAL_USER, LOCAL_PASS)
        log("✓ Local authenticated")

        # Create encrypted libraries on both servers
        log(f"Creating encrypted library '{LIBRARY_NAME}' on remote...")
        remote_lib = create_encrypted_library(REMOTE_SERVER, remote_token, LIBRARY_NAME, LIBRARY_PASSWORD)
        remote_repo_id = remote_lib["repo_id"]
        log(f"✓ Remote library: {remote_repo_id}")

        log(f"Creating encrypted library '{LIBRARY_NAME}' on local...")
        local_lib = create_encrypted_library(LOCAL_SERVER, local_token, LIBRARY_NAME, LIBRARY_PASSWORD)
        local_repo_id = local_lib["repo_id"]
        log(f"✓ Local library: {local_repo_id}")

        # Unlock libraries
        log("Unlocking remote library...")
        set_password(REMOTE_SERVER, remote_token, remote_repo_id, LIBRARY_PASSWORD)
        log("✓ Remote unlocked")

        log("Unlocking local library...")
        set_password(LOCAL_SERVER, local_token, local_repo_id, LIBRARY_PASSWORD)
        log("✓ Local unlocked")

        # Upload files
        log(f"Uploading '{FILE_NAME}' to remote...")
        remote_upload_link = get_upload_link(REMOTE_SERVER, remote_token, remote_repo_id)
        upload_file(remote_upload_link, remote_token, FILE_NAME, FILE_CONTENT)
        log("✓ Remote file uploaded")

        log(f"Uploading '{FILE_NAME}' to local...")
        local_upload_link = get_upload_link(LOCAL_SERVER, local_token, local_repo_id)
        upload_file(local_upload_link, local_token, FILE_NAME, FILE_CONTENT)
        log("✓ Local file uploaded")

        time.sleep(1)  # Wait for size to update

        # Compare download-info
        print("\n" + "=" * 70)
        print("STEP 1: Comparing download-info")
        print("=" * 70)

        remote_download_info = get_download_info(REMOTE_SERVER, remote_token, remote_repo_id)
        local_download_info = get_download_info(LOCAL_SERVER, local_token, local_repo_id)

        print("\nRemote download-info:")
        print(json.dumps(remote_download_info, indent=2))
        print("\nLocal download-info:")
        print(json.dumps(local_download_info, indent=2))

        diff = compare_json(remote_download_info, local_download_info)
        if diff:
            print(f"\n✗ Found {len(diff)} difference(s):")
            for d in diff:
                print(f"  - {d['path']}: {d['type']}")
                if 'remote' in d:
                    print(f"    Remote: {d['remote']}")
                if 'local' in d:
                    print(f"    Local: {d['local']}")
            results["steps"].append({"step": "download_info", "status": "differences_found", "differences": diff})
        else:
            print("\n✓ download-info matches")
            results["steps"].append({"step": "download_info", "status": "match"})

        # Compare commit HEAD
        print("\n" + "=" * 70)
        print("STEP 2: Comparing commit HEAD")
        print("=" * 70)

        remote_head = get_commit_head(REMOTE_SERVER, remote_token, remote_repo_id)
        local_head = get_commit_head(LOCAL_SERVER, local_token, local_repo_id)

        print(f"\nRemote HEAD: {json.dumps(remote_head, indent=2)}")
        print(f"Local HEAD: {json.dumps(local_head, indent=2)}")

        remote_commit_id = remote_head["head_commit_id"]
        local_commit_id = local_head["head_commit_id"]

        # Compare commit objects
        print("\n" + "=" * 70)
        print("STEP 3: Comparing commit objects")
        print("=" * 70)

        remote_commit = get_commit(REMOTE_SERVER, remote_token, remote_repo_id, remote_commit_id)
        local_commit = get_commit(LOCAL_SERVER, local_token, local_repo_id, local_commit_id)

        print("\nRemote commit:")
        print(json.dumps(remote_commit, indent=2))
        print("\nLocal commit:")
        print(json.dumps(local_commit, indent=2))

        diff = compare_json(remote_commit, local_commit)
        if diff:
            print(f"\n✗ Found {len(diff)} difference(s):")
            for d in diff:
                print(f"  - {d['path']}: {d['type']}")
                if 'remote' in d:
                    print(f"    Remote: {d['remote']}")
                if 'local' in d:
                    print(f"    Local: {d['local']}")
            results["steps"].append({"step": "commit", "status": "differences_found", "differences": diff})
        else:
            print("\n✓ commit object matches")
            results["steps"].append({"step": "commit", "status": "match"})

        # Compare fs-id-list
        print("\n" + "=" * 70)
        print("STEP 4: Comparing fs-id-list")
        print("=" * 70)

        remote_fs_list = get_fs_id_list(REMOTE_SERVER, remote_token, remote_repo_id, remote_commit_id)
        local_fs_list = get_fs_id_list(LOCAL_SERVER, local_token, local_repo_id, local_commit_id)

        print(f"\nRemote fs-id-list ({len(remote_fs_list)} chars):")
        print(remote_fs_list[:500] + ("..." if len(remote_fs_list) > 500 else ""))
        print(f"\nLocal fs-id-list ({len(local_fs_list)} chars):")
        print(local_fs_list[:500] + ("..." if len(local_fs_list) > 500 else ""))

        # Parse fs-id-list (newline separated)
        remote_fs_ids = [line.strip() for line in remote_fs_list.strip().split('\n') if line.strip()]
        local_fs_ids = [line.strip() for line in local_fs_list.strip().split('\n') if line.strip()]

        print(f"\nRemote has {len(remote_fs_ids)} fs_ids")
        print(f"Local has {len(local_fs_ids)} fs_ids")

        if set(remote_fs_ids) != set(local_fs_ids):
            print("\n✗ fs-id-list MISMATCH:")
            only_remote = set(remote_fs_ids) - set(local_fs_ids)
            only_local = set(local_fs_ids) - set(remote_fs_ids)
            if only_remote:
                print(f"  Only in remote: {only_remote}")
            if only_local:
                print(f"  Only in local: {only_local}")
            results["steps"].append({
                "step": "fs_id_list",
                "status": "mismatch",
                "remote_count": len(remote_fs_ids),
                "local_count": len(local_fs_ids),
                "only_remote": list(only_remote),
                "only_local": list(only_local)
            })
        else:
            print("\n✓ fs-id-list matches")
            results["steps"].append({"step": "fs_id_list", "status": "match", "count": len(remote_fs_ids)})

        # Compare pack-fs for root fs_id
        print("\n" + "=" * 70)
        print("STEP 5: Comparing pack-fs (root FS object)")
        print("=" * 70)

        root_fs_id = remote_commit["root_id"]
        print(f"\nRoot fs_id: {root_fs_id}")

        remote_pack = pack_fs(REMOTE_SERVER, remote_token, remote_repo_id, [root_fs_id])
        local_pack = pack_fs(LOCAL_SERVER, local_token, local_repo_id, [root_fs_id])

        print(f"\nRemote pack-fs size: {len(remote_pack)} bytes")
        print(f"Local pack-fs size: {len(local_pack)} bytes")

        # Parse pack-fs format: [40-byte ID][4-byte size BE][zlib data]
        if len(remote_pack) >= 44 and len(local_pack) >= 44:
            import zlib

            remote_id = remote_pack[:40].decode('ascii')
            local_id = local_pack[:40].decode('ascii')

            remote_size = int.from_bytes(remote_pack[40:44], 'big')
            local_size = int.from_bytes(local_pack[40:44], 'big')

            print(f"\nRemote: ID={remote_id}, size={remote_size}")
            print(f"Local: ID={local_id}, size={local_size}")

            try:
                remote_data = zlib.decompress(remote_pack[44:])
                remote_json = json.loads(remote_data.decode('utf-8'))
                print(f"\nRemote FS object:")
                print(json.dumps(remote_json, indent=2))
            except Exception as e:
                print(f"\n✗ Failed to decompress remote pack-fs: {e}")
                remote_json = None

            try:
                local_data = zlib.decompress(local_pack[44:])
                local_json = json.loads(local_data.decode('utf-8'))
                print(f"\nLocal FS object:")
                print(json.dumps(local_json, indent=2))
            except Exception as e:
                print(f"\n✗ Failed to decompress local pack-fs: {e}")
                local_json = None

            if remote_json and local_json:
                diff = compare_json(remote_json, local_json)
                if diff:
                    print(f"\n✗ Found {len(diff)} difference(s) in FS object:")
                    for d in diff:
                        print(f"  - {d['path']}: {d['type']}")
                        if 'remote' in d:
                            print(f"    Remote: {d['remote']}")
                        if 'local' in d:
                            print(f"    Local: {d['local']}")
                    results["steps"].append({"step": "pack_fs", "status": "differences_found", "differences": diff})
                else:
                    print("\n✓ pack-fs FS object matches")
                    results["steps"].append({"step": "pack_fs", "status": "match"})
        else:
            print("\n✗ pack-fs response too small")
            results["steps"].append({"step": "pack_fs", "status": "error", "error": "response too small"})

        # Summary
        print("\n" + "=" * 70)
        print("TEST SUMMARY")
        print("=" * 70)

        issues = [s for s in results["steps"] if s["status"] not in ["match"]]
        if issues:
            print(f"\n✗ Found {len(issues)} issue(s):")
            for issue in issues:
                print(f"  - {issue['step']}: {issue['status']}")
        else:
            print("\n✓ All protocol responses match!")

        return 0 if not issues else 1

    except Exception as e:
        log(f"✗ ERROR: {e}")
        import traceback
        traceback.print_exc()
        results["error"] = str(e)
        return 1

    finally:
        # Save results
        if CAPTURE_DIR:
            os.makedirs(CAPTURE_DIR, exist_ok=True)
            result_file = f"{CAPTURE_DIR}/sync_comparison_{TIMESTAMP}.json"
            with open(result_file, 'w') as f:
                json.dump(results, f, indent=2)
            log(f"Results saved to: {result_file}")

        # Cleanup
        if remote_repo_id:
            log("Cleaning up remote library...")
            delete_library(REMOTE_SERVER, remote_token, remote_repo_id)
        if local_repo_id:
            log("Cleaning up local library...")
            delete_library(LOCAL_SERVER, local_token, local_repo_id)
        log("Cleanup complete")

if __name__ == "__main__":
    sys.exit(main())
