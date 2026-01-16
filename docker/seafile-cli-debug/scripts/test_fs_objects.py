#!/usr/bin/env python3
"""
Test fs-id-list and pack-fs endpoints to verify they return correct data
This is what the desktop client uses to download files
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

def create_library(server_url, token, name):
    """Create a library"""
    url = f"{server_url}/api2/repos/"
    headers = {"Authorization": f"Token {token}"}
    data = {"name": name, "desc": ""}
    resp = requests.post(url, data=data, headers=headers)
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

def verify_fs_object_hash(fs_json, expected_id):
    """Verify that SHA-1 of fs_object JSON matches expected fs_id"""
    # Seafile computes fs_id as SHA-1 of JSON with alphabetically sorted keys
    # Python's json.dumps with sort_keys=True should match
    json_bytes = json.dumps(fs_json, separators=(',', ':'), sort_keys=True).encode('utf-8')
    computed_id = hashlib.sha1(json_bytes).hexdigest()

    if computed_id == expected_id:
        return True, computed_id
    else:
        # Try without spaces
        json_bytes_no_space = json.dumps(fs_json, separators=(',', ':'), sort_keys=True, ensure_ascii=False).encode('utf-8')
        computed_id_no_space = hashlib.sha1(json_bytes_no_space).hexdigest()

        if computed_id_no_space == expected_id:
            return True, computed_id_no_space
        else:
            return False, computed_id

def main():
    print("=" * 70)
    print("FS OBJECT VERIFICATION TEST")
    print("=" * 70)

    repo_id = None
    errors = []

    try:
        # Authenticate
        log("Authenticating...")
        token = authenticate(LOCAL_SERVER, LOCAL_USER, LOCAL_PASS)
        log("✓ Authenticated")

        # Create library
        log("Creating test library...")
        lib = create_library(LOCAL_SERVER, token, "fs_test")
        repo_id = lib["repo_id"]
        log(f"✓ Library created: {repo_id}")

        # Upload a file
        log("Uploading test file...")
        upload_link = get_upload_link(LOCAL_SERVER, token, repo_id)
        upload_file(upload_link, token, "test.txt", b"test1234")
        log("✓ File uploaded (8 bytes)")

        # Get HEAD commit
        log("\nGetting HEAD commit...")
        head_response = get_commit_head(LOCAL_SERVER, token, repo_id)
        commit_id = head_response["head_commit_id"]
        log(f"✓ HEAD commit: {commit_id}")

        # Get commit object
        log("\nGetting commit object...")
        commit = get_commit(LOCAL_SERVER, token, repo_id, commit_id)
        root_fs_id = commit["root_id"]
        log(f"✓ Root fs_id: {root_fs_id}")
        print(f"Commit: {json.dumps(commit, indent=2)}")

        # Get fs-id-list
        log("\nGetting fs-id-list...")
        fs_list_text = get_fs_id_list(LOCAL_SERVER, token, repo_id, commit_id)
        fs_ids = [line.strip() for line in fs_list_text.strip().split('\n') if line.strip()]
        log(f"✓ fs-id-list returned {len(fs_ids)} fs_ids")
        print(f"fs_ids: {fs_ids}")

        # Verify root_fs_id is in the list
        if root_fs_id not in fs_ids:
            error = f"✗ ERROR: root_fs_id {root_fs_id} NOT in fs-id-list!"
            errors.append(error)
            print(f"\n{error}")
            print("This is why the desktop client can't find the directory!")
        else:
            print(f"\n✓ root_fs_id {root_fs_id} is in fs-id-list")

        # Get pack-fs for all fs_ids
        log(f"\nGetting pack-fs for {len(fs_ids)} fs_ids...")
        pack_data = pack_fs(LOCAL_SERVER, token, repo_id, fs_ids)
        log(f"✓ pack-fs returned {len(pack_data)} bytes")

        # Parse pack-fs response
        log("\nParsing pack-fs response...")
        offset = 0
        fs_objects = {}

        while offset < len(pack_data):
            if offset + 44 > len(pack_data):
                error = f"✗ ERROR: Incomplete pack-fs data at offset {offset}"
                errors.append(error)
                print(error)
                break

            # Read fs_id (40 bytes)
            fs_id = pack_data[offset:offset+40].decode('ascii')

            # Read size (4 bytes big-endian)
            size = int.from_bytes(pack_data[offset+40:offset+44], 'big')

            if offset + 44 + size > len(pack_data):
                error = f"✗ ERROR: pack-fs claims size {size} but only {len(pack_data) - offset - 44} bytes remaining"
                errors.append(error)
                print(error)
                break

            # Read compressed data
            compressed_data = pack_data[offset+44:offset+44+size]

            try:
                # Decompress
                decompressed = zlib.decompress(compressed_data)
                fs_json = json.loads(decompressed.decode('utf-8'))

                # Verify hash
                hash_ok, computed_id = verify_fs_object_hash(fs_json, fs_id)
                if not hash_ok:
                    error = f"✗ ERROR: fs_id hash mismatch!"
                    errors.append(error)
                    print(f"\n{error}")
                    print(f"  Expected: {fs_id}")
                    print(f"  Computed: {computed_id}")
                    print(f"  JSON: {json.dumps(fs_json, separators=(',', ':'), sort_keys=True)}")
                else:
                    print(f"\n✓ fs_id {fs_id[:16]}... hash verified")

                print(f"  Type: {fs_json.get('type')}, Size: {size} bytes compressed")
                print(f"  Content: {json.dumps(fs_json, indent=2)}")

                fs_objects[fs_id] = fs_json

            except zlib.error as e:
                error = f"✗ ERROR: Failed to decompress fs_id {fs_id}: {e}"
                errors.append(error)
                print(f"\n{error}")

            offset += 44 + size

        log(f"\nParsed {len(fs_objects)} FS objects from pack-fs")

        # Verify all requested fs_ids were returned
        missing = set(fs_ids) - set(fs_objects.keys())
        if missing:
            error = f"✗ ERROR: pack-fs did not return all requested fs_ids!"
            errors.append(error)
            print(f"\n{error}")
            print(f"  Missing: {missing}")
        else:
            print("\n✓ All requested fs_ids were returned in pack-fs")

        # Summary
        print("\n" + "=" * 70)
        print("TEST SUMMARY")
        print("=" * 70)

        if errors:
            print(f"\n✗ FAILED: {len(errors)} error(s) found:\n")
            for error in errors:
                print(f"  - {error}")
            print("\nThese errors explain why the desktop client shows:")
            print("  - 'Error when indexing' (can't find FS objects)")
            print("  - Empty folder (can't download directory structure)")
            return 1
        else:
            print("\n✓ SUCCESS: All FS objects are correct")
            print("  - fs-id-list contains root_fs_id ✓")
            print("  - pack-fs returns all requested fs_ids ✓")
            print("  - All fs_id hashes match ✓")
            print("  - All data is properly zlib compressed ✓")
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
