#!/usr/bin/env python3
"""
Test the complete sync flow that the Seafile desktop client uses
This simulates what the client does when syncing a library
"""

import requests
import json
import zlib
import hashlib
import sys
from datetime import datetime

LOCAL_SERVER = "http://host.docker.internal:8080"
LOCAL_USER = "abel.aguzmans@gmail.com"
LOCAL_PASS = "dev-token-123"

def log(msg):
    print(f"[{datetime.now().strftime('%H:%M:%S')}] {msg}")

def authenticate(server_url, username, password):
    """Authenticate and get token"""
    url = f"{server_url}/api2/auth-token/"
    data = {"username": username, "password": password}
    resp = requests.post(url, data=data)
    resp.raise_for_status()
    return resp.json()["token"]

def get_download_info(server_url, token, repo_id):
    """Get download-info (first thing client does)"""
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
    # Handle both plain text "2" and JSON {"version":2}
    try:
        return int(resp.text.strip())
    except ValueError:
        return resp.json()["version"]

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

def get_fs_id_list(server_url, token, repo_id, server_head):
    """Get fs-id-list"""
    url = f"{server_url}/seafhttp/repo/{repo_id}/fs-id-list/"
    headers = {"Authorization": f"Token {token}"}
    params = {"server-head": server_head}
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

def verify_fs_object(fs_id, compressed_data):
    """Decompress and verify fs_object"""
    try:
        decompressed = zlib.decompress(compressed_data)
        fs_json = json.loads(decompressed.decode('utf-8'))

        # Compute SHA-1 hash
        computed_hash = hashlib.sha1(decompressed).hexdigest()

        return {
            "verified": computed_hash == fs_id,
            "computed_hash": computed_hash,
            "expected_hash": fs_id,
            "json": fs_json
        }
    except Exception as e:
        return {
            "verified": False,
            "error": str(e)
        }

def main():
    # Get repo_id from command line or use test0032
    if len(sys.argv) > 1:
        repo_id = sys.argv[1]
    else:
        # Find an encrypted library to test
        log("No repo_id provided, listing libraries...")
        token = authenticate(LOCAL_SERVER, LOCAL_USER, LOCAL_PASS)

        resp = requests.get(f"{LOCAL_SERVER}/api2/repos", headers={"Authorization": f"Token {token}"})
        repos = resp.json()

        encrypted_repos = [r for r in repos if r.get("encrypted") == True or r.get("encrypted") == 1]

        if not encrypted_repos:
            print("No encrypted libraries found. Please create one first.")
            return 1

        repo_id = encrypted_repos[0]["id"]
        log(f"Testing with encrypted library: {encrypted_repos[0]['name']} ({repo_id})")

    print("=" * 70)
    print("DESKTOP CLIENT SYNC FLOW SIMULATION")
    print("=" * 70)
    print(f"Repository: {repo_id}")
    print()

    issues = []

    try:
        # Step 1: Authenticate
        log("Step 1: Authenticating...")
        token = authenticate(LOCAL_SERVER, LOCAL_USER, LOCAL_PASS)
        log("✓ Authenticated")

        # Step 2: Get download-info
        print("\n" + "=" * 70)
        print("STEP 2: Get download-info")
        print("=" * 70)

        download_info = get_download_info(LOCAL_SERVER, token, repo_id)
        print(json.dumps(download_info, indent=2))

        # Verify critical fields
        required_fields = ["encrypted", "enc_version", "magic", "random_key", "head_commit_id", "token"]
        for field in required_fields:
            if field not in download_info:
                issue = f"Missing field in download-info: {field}"
                issues.append(issue)
                print(f"  ✗ {issue}")

        if download_info.get("encrypted") not in [1, 2, "1", "2"]:
            issue = f"Library not marked as encrypted: {download_info.get('encrypted')}"
            issues.append(issue)
            print(f"  ✗ {issue}")

        # Step 3: Get protocol version
        print("\n" + "=" * 70)
        print("STEP 3: Get protocol version")
        print("=" * 70)

        protocol_version = get_protocol_version(LOCAL_SERVER, token)
        print(f"Protocol version: {protocol_version}")

        if protocol_version != 2:
            issue = f"Unexpected protocol version: {protocol_version} (expected 2)"
            issues.append(issue)
            print(f"  ✗ {issue}")

        # Step 4: Get HEAD commit
        print("\n" + "=" * 70)
        print("STEP 4: Get HEAD commit")
        print("=" * 70)

        head_response = get_commit_head(LOCAL_SERVER, token, repo_id)
        print(json.dumps(head_response, indent=2))

        if "head_commit_id" not in head_response:
            issue = "Missing head_commit_id in HEAD response"
            issues.append(issue)
            print(f"  ✗ {issue}")
            return 1

        commit_id = head_response["head_commit_id"]
        log(f"✓ HEAD commit: {commit_id}")

        # Step 5: Get commit object
        print("\n" + "=" * 70)
        print("STEP 5: Get commit object")
        print("=" * 70)

        commit = get_commit(LOCAL_SERVER, token, repo_id, commit_id)
        print(json.dumps(commit, indent=2))

        root_fs_id = commit.get("root_id")
        if not root_fs_id:
            issue = "Missing root_id in commit"
            issues.append(issue)
            print(f"  ✗ {issue}")
            return 1

        # Verify encryption fields in commit
        enc_fields = {
            "encrypted": commit.get("encrypted") == "true",
            "enc_version": commit.get("enc_version") == 2,
            "magic": len(commit.get("magic", "")) == 64,
            "key": len(commit.get("key", "")) > 0,
            "no_local_history": commit.get("no_local_history") == 1,
        }

        for field, valid in enc_fields.items():
            if not valid:
                issue = f"Invalid {field} in commit: {commit.get(field)}"
                issues.append(issue)
                print(f"  ✗ {issue}")

        # Step 6: Get fs-id-list
        print("\n" + "=" * 70)
        print("STEP 6: Get fs-id-list")
        print("=" * 70)

        fs_ids = get_fs_id_list(LOCAL_SERVER, token, repo_id, commit_id)
        print(f"Response: {fs_ids}\n")
        print(f"Parsed {len(fs_ids)} fs_ids:")
        for fs_id in fs_ids:
            print(f"  - {fs_id}")

        # CRITICAL: Verify root_fs_id is in the list
        if root_fs_id not in fs_ids:
            issue = f"ROOT FS ID NOT IN LIST: {root_fs_id}"
            issues.append(issue)
            print(f"\n  ✗ {issue}")
            print("  This is why the client shows 'Error when indexing'!")
        else:
            print(f"\n  ✓ Root fs_id {root_fs_id} is in list")

        # Step 7: Get pack-fs
        print("\n" + "=" * 70)
        print("STEP 7: Get pack-fs for all fs_ids")
        print("=" * 70)

        pack_data = pack_fs(LOCAL_SERVER, token, repo_id, fs_ids)
        print(f"Received {len(pack_data)} bytes\n")

        # Parse pack-fs
        offset = 0
        pack_fs_objects = {}

        while offset < len(pack_data):
            if offset + 44 > len(pack_data):
                issue = f"Incomplete pack-fs data at offset {offset}"
                issues.append(issue)
                print(f"  ✗ {issue}")
                break

            fs_id = pack_data[offset:offset+40].decode('ascii')
            size = int.from_bytes(pack_data[offset+40:offset+44], 'big')

            if offset + 44 + size > len(pack_data):
                issue = f"pack-fs size mismatch at offset {offset}"
                issues.append(issue)
                print(f"  ✗ {issue}")
                break

            compressed = pack_data[offset+44:offset+44+size]
            result = verify_fs_object(fs_id, compressed)

            if not result["verified"]:
                issue = f"FS ID HASH MISMATCH: {fs_id}"
                issues.append(issue)
                print(f"  ✗ {issue}")
                if "error" in result:
                    print(f"     Error: {result['error']}")
                else:
                    print(f"     Expected: {result['expected_hash']}")
                    print(f"     Computed: {result['computed_hash']}")
            else:
                print(f"  ✓ {fs_id[:16]}... verified")
                print(f"     Type: {result['json'].get('type')}, Version: {result['json'].get('version')}")

                # Check for required fields
                if result['json'].get('type') == 3:  # Directory
                    dirents = result['json'].get('dirents', [])
                    print(f"     Dirents: {len(dirents)} entries")
                    for i, dirent in enumerate(dirents):
                        required = ["id", "mode", "modifier", "mtime", "name", "size"]
                        missing = [f for f in required if f not in dirent]
                        if missing:
                            issue = f"Dirent {i} missing fields: {missing}"
                            issues.append(issue)
                            print(f"       ✗ {issue}")

            pack_fs_objects[fs_id] = result
            offset += 44 + size

        # Verify all fs_ids were returned
        missing = set(fs_ids) - set(pack_fs_objects.keys())
        if missing:
            issue = f"pack-fs missing {len(missing)} fs_ids: {list(missing)[:3]}..."
            issues.append(issue)
            print(f"\n  ✗ {issue}")

        # Summary
        print("\n" + "=" * 70)
        print("SUMMARY")
        print("=" * 70)

        if issues:
            print(f"\n✗ FOUND {len(issues)} ISSUE(S):\n")
            for i, issue in enumerate(issues, 1):
                print(f"{i}. {issue}")
            print("\nThese issues prevent the desktop client from syncing.")
            return 1
        else:
            print("\n✓ ALL CHECKS PASSED!")
            print("\nThe server is returning correct sync protocol responses.")
            print("\nIf the desktop client still shows errors:")
            print("  1. Delete old test libraries (test0030, test0031) - they have wrong fs_ids")
            print("  2. Clear client cache: rm -rf ~/Seafile/.seafile-data/storage/")
            print("  3. Restart the Seafile desktop client")
            print("  4. Test with a fresh library created after today's fixes")
            return 0

    except Exception as e:
        log(f"✗ ERROR: {e}")
        import traceback
        traceback.print_exc()
        return 1

if __name__ == "__main__":
    sys.exit(main())
