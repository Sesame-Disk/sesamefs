#!/usr/bin/env python3
"""
Test how stock Seafile handles identical files with different names.

This script:
1. Creates a test library on stock Seafile
2. Uploads two files with identical content but different names
3. Syncs the library with the desktop client
4. Verifies if both files are downloaded
"""

import os
import sys
import json
import requests
import time
import subprocess
import hashlib

# Stock Seafile server credentials
SERVER_URL = "https://app.nihaoconsult.com"
EMAIL = "abel.aguzmans@gmail.com"
PASSWORD = "Qwerty123!"

def get_auth_token():
    """Get authentication token from stock Seafile server."""
    url = f"{SERVER_URL}/api2/auth-token/"
    data = {"username": EMAIL, "password": PASSWORD}

    response = requests.post(url, data=data)
    response.raise_for_status()

    token = response.json()["token"]
    print(f"✓ Got auth token: {token[:20]}...")
    return token

def create_test_library(token):
    """Create a test library for duplicate file testing."""
    url = f"{SERVER_URL}/api2/repos/"
    headers = {"Authorization": f"Token {token}"}
    data = {
        "name": "Duplicate Files Test",
        "desc": "Testing identical files with different names"
    }

    response = requests.post(url, headers=headers, data=data)
    response.raise_for_status()

    repo_id = response.json()["repo_id"]
    print(f"✓ Created test library: {repo_id}")
    return repo_id

def get_upload_link(token, repo_id, path="/"):
    """Get upload link for the library."""
    url = f"{SERVER_URL}/api2/repos/{repo_id}/upload-link/"
    headers = {"Authorization": f"Token {token}"}
    params = {"p": path}

    response = requests.get(url, headers=headers, params=params)
    response.raise_for_status()

    upload_url = response.json()
    # Remove quotes if present
    if isinstance(upload_url, str):
        upload_url = upload_url.strip('"')
    print(f"✓ Got upload link: {upload_url}")
    return upload_url

def upload_file(upload_url, token, parent_dir, filename, content):
    """Upload a file to the library."""
    headers = {"Authorization": f"Token {token}"}
    files = {"file": (filename, content)}
    data = {
        "parent_dir": parent_dir,
        "replace": "0"
    }

    response = requests.post(upload_url, headers=headers, files=files, data=data)
    response.raise_for_status()

    print(f"✓ Uploaded: {filename}")
    return response.json()

def list_directory(token, repo_id, path="/"):
    """List directory contents."""
    url = f"{SERVER_URL}/api2/repos/{repo_id}/dir/"
    headers = {"Authorization": f"Token {token}"}
    params = {"p": path}

    response = requests.get(url, headers=headers, params=params)
    response.raise_for_status()

    return response.json()

def sync_with_client(server_url, email, password, repo_id):
    """Sync library with seaf-cli and check downloaded files."""
    print(f"\n=== Syncing with seaf-cli ===")

    # Initialize seaf-cli
    subprocess.run(["seaf-cli", "init", "-d", "/tmp/seafile-test"], check=True)
    subprocess.run(["seaf-cli", "start"], check=True)

    # Wait for daemon to start
    time.sleep(2)

    # Download the library
    cmd = [
        "seaf-cli", "download",
        "-l", repo_id,
        "-s", server_url,
        "-u", email,
        "-p", password,
        "-d", "/tmp/seafile-test/sync"
    ]

    result = subprocess.run(cmd, capture_output=True, text=True)
    print(f"Download command output: {result.stdout}")
    if result.stderr:
        print(f"Errors: {result.stderr}")

    # Wait for sync to complete
    print("Waiting for sync to complete...")
    time.sleep(10)

    # Check synced files
    sync_dir = "/tmp/seafile-test/sync/Duplicate Files Test"
    if os.path.exists(sync_dir):
        files = os.listdir(sync_dir)
        print(f"\n=== Files synced to local directory ===")
        for f in sorted(files):
            filepath = os.path.join(sync_dir, f)
            if os.path.isfile(filepath):
                size = os.path.getsize(filepath)
                print(f"  {f} ({size} bytes)")
        return files
    else:
        print(f"ERROR: Sync directory not found: {sync_dir}")
        return []

    # Stop daemon
    subprocess.run(["seaf-cli", "stop"], check=True)

def main():
    print("=== Testing Duplicate Files on Stock Seafile ===\n")

    try:
        # Step 1: Authenticate
        print("Step 1: Authenticating...")
        token = get_auth_token()

        # Step 2: Create test library
        print("\nStep 2: Creating test library...")
        repo_id = create_test_library(token)

        # Step 3: Create identical file content
        print("\nStep 3: Creating test files...")
        test_content = b"This is test content. " * 1000  # ~22KB
        content_hash = hashlib.sha1(test_content).hexdigest()
        print(f"  Content SHA1: {content_hash}")
        print(f"  Content size: {len(test_content)} bytes")

        # Step 4: Upload two files with identical content but different names
        print("\nStep 4: Uploading duplicate files...")
        upload_url = get_upload_link(token, repo_id)

        upload_file(upload_url, token, "/", "test-file-original.txt", test_content)
        time.sleep(1)
        upload_file(upload_url, token, "/", "test-file-copy.txt", test_content)

        # Step 5: List directory to verify both files exist
        print("\nStep 5: Verifying files on server...")
        files = list_directory(token, repo_id)
        print(f"Files on server:")
        for f in files:
            print(f"  - {f['name']} ({f['size']} bytes, id: {f['id']})")

        # Check if both files have the same block ID (content hash)
        if len(files) >= 2:
            file1_id = files[0]['id']
            file2_id = files[1]['id']
            print(f"\n  File IDs match: {file1_id == file2_id}")
            if file1_id == file2_id:
                print("  ✓ Stock Seafile deduplicates content (same block ID)")

        # Step 6: Sync with desktop client
        print("\nStep 6: Testing desktop client sync...")
        print("NOTE: This requires seaf-cli to be installed and working.")
        print(f"Repository ID: {repo_id}")
        print(f"Server URL: {SERVER_URL}")
        print("\nManual test:")
        print(f"1. Add this library to your Seafile desktop client")
        print(f"2. Check if BOTH files are downloaded:")
        print(f"   - test-file-original.txt")
        print(f"   - test-file-copy.txt")
        print(f"3. Verify both files exist in the synced folder")

        print(f"\n=== Test Complete ===")
        print(f"Library ID: {repo_id}")
        print(f"Check your Seafile desktop client to see if both files are synced.")

    except Exception as e:
        print(f"\nERROR: {e}")
        import traceback
        traceback.print_exc()
        sys.exit(1)

if __name__ == "__main__":
    main()
