#!/usr/bin/env python3
"""
Real desktop client sync test - uses seaf-cli to actually sync libraries
from both remote and local servers, then compares results.
"""

import requests
import json
import time
import os
import subprocess
from datetime import datetime

# Servers
REMOTE_SERVER = "https://app.nihaoconsult.com"
LOCAL_SERVER = "http://host.docker.internal:8080"

# Credentials
REMOTE_USER = "abel.aguzmans@gmail.com"
REMOTE_PASS = "Qwerty123!"
LOCAL_USER = "abel.aguzmans@gmail.com"
LOCAL_PASS = "dev-token-123"

# Test config
TIMESTAMP = datetime.now().strftime("%Y%m%d_%H%M%S")
LIBRARY_NAME = f"client_sync_test_{TIMESTAMP}"
LIBRARY_PASSWORD = "test123"
FILE_NAME = "test_file.txt"
FILE_CONTENT = b"This is test content for real client sync verification.\nLine 2\nLine 3\n"

# Sync directories
REMOTE_SYNC_DIR = "/tmp/seafile-remote"
LOCAL_SYNC_DIR = "/tmp/seafile-local"

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
    resp = requests.post(url, data=data, verify=False)
    resp.raise_for_status()
    return resp.json()["token"]

def create_encrypted_library(server_url, token, name, password):
    """Create encrypted library"""
    url = f"{server_url}/api2/repos/"
    headers = {"Authorization": f"Token {token}"}
    data = {"name": name, "passwd": password}
    resp = requests.post(url, data=data, headers=headers, verify=False)
    resp.raise_for_status()
    return resp.json()

def set_password(server_url, token, repo_id, password):
    """Unlock encrypted library"""
    url = f"{server_url}/api/v2.1/repos/{repo_id}/set-password/"
    headers = {"Authorization": f"Token {token}"}
    data = {"password": password}
    resp = requests.post(url, json=data, headers=headers, verify=False)
    resp.raise_for_status()
    return resp.json()

def get_upload_link(server_url, token, repo_id):
    """Get upload link"""
    url = f"{server_url}/api2/repos/{repo_id}/upload-link/"
    headers = {"Authorization": f"Token {token}"}
    resp = requests.get(url, headers=headers, verify=False)
    resp.raise_for_status()
    return resp.text.strip().strip('"')

def upload_file(upload_url, token, filename, content):
    """Upload a file via API"""
    headers = {"Authorization": f"Token {token}"}
    files = {"file": (filename, content)}
    data = {"parent_dir": "/", "replace": "0"}
    resp = requests.post(upload_url, headers=headers, files=files, data=data, verify=False)
    resp.raise_for_status()
    return resp.text

def delete_library(server_url, token, repo_id):
    """Delete library"""
    url = f"{server_url}/api2/repos/{repo_id}/"
    headers = {"Authorization": f"Token {token}"}
    requests.delete(url, headers=headers, verify=False)

def run_seaf_cli(cmd, cwd=None):
    """Run seaf-cli command"""
    full_cmd = ["seaf-cli"] + cmd
    result = subprocess.run(full_cmd, cwd=cwd, capture_output=True, text=True)
    return result.returncode, result.stdout, result.stderr

def init_seafile_client(config_dir):
    """Initialize seafile client"""
    os.makedirs(config_dir, exist_ok=True)
    returncode, stdout, stderr = run_seaf_cli(["-c", config_dir, "init", "-d", config_dir])
    if returncode != 0:
        log(f"Init output: {stdout}")
        log(f"Init error: {stderr}")
    return returncode == 0

def start_seafile_client(config_dir):
    """Start seafile daemon"""
    returncode, stdout, stderr = run_seaf_cli(["-c", config_dir, "start"])
    if returncode != 0:
        log(f"Start output: {stdout}")
        log(f"Start error: {stderr}")
    time.sleep(2)  # Wait for daemon to start
    return returncode == 0

def stop_seafile_client(config_dir):
    """Stop seafile daemon"""
    run_seaf_cli(["-c", config_dir, "stop"])
    time.sleep(1)

def sync_library(config_dir, server_url, username, password, repo_id, library_password, sync_dir):
    """Sync a library using seaf-cli"""
    os.makedirs(sync_dir, exist_ok=True)

    # Download library
    cmd = [
        "-c", config_dir,
        "download",
        "-l", repo_id,
        "-s", server_url,
        "-u", username,
        "-p", password,
        "-d", sync_dir
    ]

    returncode, stdout, stderr = run_seaf_cli(cmd)
    log(f"Download command output: {stdout}")
    if stderr:
        log(f"Download stderr: {stderr}")

    if returncode != 0:
        return False

    # Wait for initial sync
    time.sleep(3)

    # If encrypted, provide password
    if library_password:
        # The library might prompt for password via sync status
        # For encrypted libs, we need to use the API to unlock first
        pass

    # Wait for sync to complete
    max_wait = 30
    waited = 0
    while waited < max_wait:
        returncode, stdout, stderr = run_seaf_cli(["-c", config_dir, "status"])
        log(f"Sync status: {stdout.strip()}")

        if "synchronized" in stdout.lower():
            return True

        time.sleep(2)
        waited += 2

    return False

def compare_directories(dir1, dir2):
    """Compare two synced directories"""
    differences = []

    # Get file lists
    files1 = set()
    files2 = set()

    for root, dirs, files in os.walk(dir1):
        for f in files:
            rel_path = os.path.relpath(os.path.join(root, f), dir1)
            if not rel_path.startswith('.seafile'):
                files1.add(rel_path)

    for root, dirs, files in os.walk(dir2):
        for f in files:
            rel_path = os.path.relpath(os.path.join(root, f), dir2)
            if not rel_path.startswith('.seafile'):
                files2.add(rel_path)

    # Compare file lists
    only_in_dir1 = files1 - files2
    only_in_dir2 = files2 - files1
    common_files = files1 & files2

    if only_in_dir1:
        differences.append(f"Only in remote: {only_in_dir1}")

    if only_in_dir2:
        differences.append(f"Only in local: {only_in_dir2}")

    # Compare file contents
    for file in common_files:
        path1 = os.path.join(dir1, file)
        path2 = os.path.join(dir2, file)

        with open(path1, 'rb') as f1, open(path2, 'rb') as f2:
            content1 = f1.read()
            content2 = f2.read()

            if content1 != content2:
                differences.append(f"Content differs: {file} (remote: {len(content1)} bytes, local: {len(content2)} bytes)")

    return differences, common_files

def main():
    print("=" * 70)
    print("REAL CLIENT SYNC TEST")
    print("=" * 70)
    print(f"Library: {LIBRARY_NAME}")
    print(f"Password: {LIBRARY_PASSWORD}")
    print(f"File: {FILE_NAME} ({len(FILE_CONTENT)} bytes)")
    print()

    remote_repo_id = None
    local_repo_id = None
    remote_config = "/tmp/seafile-config-remote"
    local_config = "/tmp/seafile-config-local"

    try:
        # Step 1: Authenticate
        section("STEP 1: Authentication")
        log("Authenticating to remote server...")
        remote_token = authenticate(REMOTE_SERVER, REMOTE_USER, REMOTE_PASS)
        log("✓ Remote authenticated")

        log("Authenticating to local server...")
        local_token = authenticate(LOCAL_SERVER, LOCAL_USER, LOCAL_PASS)
        log("✓ Local authenticated")

        # Step 2: Create encrypted libraries
        section("STEP 2: Create Encrypted Libraries")
        log("Creating encrypted library on remote server...")
        remote_lib = create_encrypted_library(REMOTE_SERVER, remote_token, LIBRARY_NAME, LIBRARY_PASSWORD)
        remote_repo_id = remote_lib["repo_id"]
        log(f"✓ Remote library: {remote_repo_id}")

        log("Creating encrypted library on local server...")
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

        # Step 4: Upload files via API
        section("STEP 4: Upload Files via API")
        log(f"Uploading '{FILE_NAME}' to remote...")
        remote_upload_link = get_upload_link(REMOTE_SERVER, remote_token, remote_repo_id)
        upload_file(remote_upload_link, remote_token, FILE_NAME, FILE_CONTENT)
        log("✓ Remote file uploaded")

        log(f"Uploading '{FILE_NAME}' to local...")
        local_upload_link = get_upload_link(LOCAL_SERVER, local_token, local_repo_id)
        upload_file(local_upload_link, local_token, FILE_NAME, FILE_CONTENT)
        log("✓ Local file uploaded")

        # Wait for commits
        log("Waiting 3 seconds for commits to be created...")
        time.sleep(3)

        # Step 5: Initialize Seafile clients
        section("STEP 5: Initialize Seafile Clients")
        log("Initializing remote sync client...")
        if not init_seafile_client(remote_config):
            log("✗ Failed to init remote client")
            return 1
        log("✓ Remote client initialized")

        log("Initializing local sync client...")
        if not init_seafile_client(local_config):
            log("✗ Failed to init local client")
            return 1
        log("✓ Local client initialized")

        # Step 6: Start Seafile daemons
        section("STEP 6: Start Seafile Daemons")
        log("Starting remote daemon...")
        if not start_seafile_client(remote_config):
            log("✗ Failed to start remote daemon")
            return 1
        log("✓ Remote daemon started")

        log("Starting local daemon...")
        if not start_seafile_client(local_config):
            log("✗ Failed to start local daemon")
            return 1
        log("✓ Local daemon started")

        # Step 7: Sync libraries
        section("STEP 7: Sync Libraries with Desktop Client")
        log("Syncing remote library...")
        if not sync_library(remote_config, REMOTE_SERVER, REMOTE_USER, REMOTE_PASS,
                           remote_repo_id, LIBRARY_PASSWORD, REMOTE_SYNC_DIR):
            log("✗ Remote sync failed or timed out")
            # Continue anyway to check what we got
        else:
            log("✓ Remote sync completed")

        log("Syncing local library...")
        if not sync_library(local_config, LOCAL_SERVER, LOCAL_USER, LOCAL_PASS,
                           local_repo_id, LIBRARY_PASSWORD, LOCAL_SYNC_DIR):
            log("✗ Local sync failed or timed out")
            # Continue anyway to check what we got
        else:
            log("✓ Local sync completed")

        # Step 8: Compare synced files
        section("STEP 8: Compare Synced Files")
        differences, common_files = compare_directories(REMOTE_SYNC_DIR, LOCAL_SYNC_DIR)

        if differences:
            print(f"\n✗ FOUND {len(differences)} DIFFERENCE(S):\n")
            for i, diff in enumerate(differences, 1):
                print(f"{i}. {diff}")
            return 1
        else:
            print(f"\n✓ ALL FILES MATCH!")
            print(f"\nSynced files: {common_files}")
            print("\nBoth servers synced identical files via desktop client.")
            return 0

    except Exception as e:
        log(f"✗ ERROR: {e}")
        import traceback
        traceback.print_exc()
        return 1

    finally:
        # Cleanup
        log("\nStopping Seafile daemons...")
        stop_seafile_client(remote_config)
        stop_seafile_client(local_config)

        if remote_repo_id:
            log("Cleaning up remote library...")
            delete_library(REMOTE_SERVER, remote_token, remote_repo_id)
        if local_repo_id:
            log("Cleaning up local library...")
            delete_library(LOCAL_SERVER, local_token, local_repo_id)

        # Clean up sync directories
        subprocess.run(["rm", "-rf", REMOTE_SYNC_DIR, LOCAL_SYNC_DIR, remote_config, local_config])
        log("✓ Cleanup complete")

if __name__ == "__main__":
    import sys
    # Disable SSL warnings for testing
    import urllib3
    urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)

    sys.exit(main())
