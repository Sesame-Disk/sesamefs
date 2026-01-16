#!/usr/bin/env python3
"""
Simple test to verify repo_size is updated after file upload
"""

import requests
import json
import time
import sys

LOCAL_SERVER = "http://host.docker.internal:8080"
LOCAL_USER = "admin@sesamefs.local"
LOCAL_PASS = "dev-token-123"

def authenticate(server_url, username, password):
    """Authenticate and get token"""
    url = f"{server_url}/api2/auth-token/"
    data = {"username": username, "password": password}
    resp = requests.post(url, data=data)
    resp.raise_for_status()
    return resp.json()["token"]

def create_library(server_url, token, name):
    """Create a new library"""
    url = f"{server_url}/api2/repos/"
    headers = {"Authorization": f"Token {token}"}
    data = {"name": name, "desc": "Test library for size check"}
    resp = requests.post(url, data=data, headers=headers)
    resp.raise_for_status()
    return resp.json()

def get_library_info(server_url, token, repo_id):
    """Get library information"""
    url = f"{server_url}/api2/repos/{repo_id}/"
    headers = {"Authorization": f"Token {token}"}
    resp = requests.get(url, headers=headers)
    resp.raise_for_status()
    return resp.json()

def get_upload_link(server_url, token, repo_id):
    """Get upload link for library"""
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

def delete_library(server_url, token, repo_id):
    """Delete a library"""
    url = f"{server_url}/api2/repos/{repo_id}/"
    headers = {"Authorization": f"Token {token}"}
    resp = requests.delete(url, headers=headers)
    # Don't raise for status - deletion might fail if already deleted

def main():
    print("=" * 70)
    print("REPO_SIZE UPDATE TEST")
    print("=" * 70)

    lib_name = f"size_test_{int(time.time())}"
    repo_id = None

    try:
        # Authenticate
        print("\nStep 1: Authenticating...")
        token = authenticate(LOCAL_SERVER, LOCAL_USER, LOCAL_PASS)
        print(f"✓ Authenticated successfully")

        # Create library
        print(f"\nStep 2: Creating library '{lib_name}'...")
        lib = create_library(LOCAL_SERVER, token, lib_name)
        repo_id = lib["repo_id"]
        print(f"✓ Library created: {repo_id}")

        # Check initial size
        print("\nStep 3: Checking initial library size...")
        lib_info = get_library_info(LOCAL_SERVER, token, repo_id)
        initial_size = lib_info.get("size", 0)
        print(f"✓ Initial size: {initial_size} bytes")

        if initial_size != 0:
            print(f"✗ ERROR: Initial size should be 0, got {initial_size}")
            return 1

        # Upload file
        print("\nStep 4: Uploading 8-byte file...")
        upload_link = get_upload_link(LOCAL_SERVER, token, repo_id)
        file_content = b"test0030"  # 8 bytes
        upload_file(upload_link, token, "test.txt", file_content)
        print(f"✓ File uploaded (8 bytes)")

        # Wait a moment for size to update
        time.sleep(1)

        # Check final size
        print("\nStep 5: Checking library size after upload...")
        lib_info = get_library_info(LOCAL_SERVER, token, repo_id)
        final_size = lib_info.get("size", 0)
        print(f"✓ Final size: {final_size} bytes")

        # Verify size
        print("\nStep 6: Verifying size update...")
        if final_size == 8:
            print("✓ SUCCESS: Library size correctly updated to 8 bytes")
            return 0
        else:
            print(f"✗ FAILED: Expected size 8, got {final_size}")
            print(f"\nLibrary info: {json.dumps(lib_info, indent=2)}")
            return 1

    except Exception as e:
        print(f"\n✗ ERROR: {e}")
        import traceback
        traceback.print_exc()
        return 1
    finally:
        # Cleanup
        if repo_id:
            print(f"\nCleaning up: deleting library {repo_id}...")
            delete_library(LOCAL_SERVER, token, repo_id)
            print("✓ Cleanup complete")

if __name__ == "__main__":
    sys.exit(main())
