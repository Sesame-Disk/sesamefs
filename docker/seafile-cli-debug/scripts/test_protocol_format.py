#!/usr/bin/env python3
"""
Test that all sync protocol endpoints return correct formats
"""

import requests
import json
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

def get_download_info(server_url, token, repo_id):
    """Get download-info for sync protocol"""
    url = f"{server_url}/api2/repos/{repo_id}/download-info/"
    headers = {"Authorization": f"Token {token}"}
    resp = requests.get(url, headers=headers)
    resp.raise_for_status()
    return resp.json()

def get_commit_head(server_url, token, repo_id):
    """Get HEAD commit ID"""
    url = f"{server_url}/seafhttp/repo/{repo_id}/commit/HEAD"
    headers = {"Authorization": f"Token {token}"}
    resp = requests.get(url, headers=headers)
    resp.raise_for_status()
    head_response = resp.json()
    return head_response["head_commit_id"]

def get_commit(server_url, token, repo_id, commit_id):
    """Get commit object"""
    url = f"{server_url}/seafhttp/repo/{repo_id}/commit/{commit_id}"
    headers = {"Authorization": f"Token {token}"}
    resp = requests.get(url, headers=headers)
    resp.raise_for_status()
    return resp.json()

def delete_library(server_url, token, repo_id):
    """Delete a library"""
    url = f"{server_url}/api2/repos/{repo_id}/"
    headers = {"Authorization": f"Token {token}"}
    requests.delete(url, headers=headers)

def check_field(obj, field, expected_type, expected_value=None, allow_empty=True):
    """Check if a field exists, has correct type, and optionally correct value"""
    if field not in obj:
        return f"✗ Missing field: {field}"

    value = obj[field]
    value_type = type(value).__name__

    if expected_type == "int":
        if not isinstance(value, int):
            return f"✗ {field}: expected int, got {value_type} ({value})"
    elif expected_type == "str":
        if not isinstance(value, str):
            return f"✗ {field}: expected str, got {value_type} ({value})"
    elif expected_type == "bool":
        if not isinstance(value, bool):
            return f"✗ {field}: expected bool, got {value_type} ({value})"

    if expected_value is not None:
        if value != expected_value:
            return f"✗ {field}: expected '{expected_value}', got '{value}'"

    if not allow_empty and expected_type == "str" and value == "":
        return f"✗ {field}: should not be empty"

    return None

def main():
    print("=" * 70)
    print("PROTOCOL FORMAT VERIFICATION TEST")
    print("=" * 70)

    repo_id = None
    errors = []

    try:
        # Authenticate
        print("\n[1/6] Authenticating...")
        token = authenticate(LOCAL_SERVER, LOCAL_USER, LOCAL_PASS)
        print("✓ Authenticated")

        # Create library
        print("\n[2/6] Creating test library...")
        lib = create_library(LOCAL_SERVER, token, "protocol_test")
        repo_id = lib["repo_id"]
        print(f"✓ Library created: {repo_id}")

        # Upload a file
        print("\n[3/6] Uploading test file...")
        upload_link = get_upload_link(LOCAL_SERVER, token, repo_id)
        upload_file(upload_link, token, "test.txt", b"test1234")
        print("✓ File uploaded (8 bytes)")

        # Test download-info endpoint
        print("\n[4/6] Testing download-info endpoint...")
        download_info = get_download_info(LOCAL_SERVER, token, repo_id)
        print(f"Download-info response: {json.dumps(download_info, indent=2)}")

        # Check critical fields
        checks = [
            check_field(download_info, "repo_id", "str", allow_empty=False),
            check_field(download_info, "repo_name", "str", allow_empty=False),
            check_field(download_info, "repo_desc", "str", expected_value=""),  # Must be empty string
            check_field(download_info, "repo_size", "int"),
            check_field(download_info, "encrypted", "int"),  # Seafile uses int 0/1, not boolean
            check_field(download_info, "relay_id", "str"),
            check_field(download_info, "token", "str", allow_empty=False),
            check_field(download_info, "relay_addr", "str"),
            check_field(download_info, "relay_port", "str"),
            check_field(download_info, "email", "str", allow_empty=False),
            # Note: random_key, enc_version, magic only present for encrypted libraries
        ]

        for check in checks:
            if check:
                errors.append(f"download-info: {check}")
                print(f"  {check}")

        if not errors:
            print("  ✓ All download-info fields correct")

        # Verify repo_size is 8
        if download_info.get("repo_size") != 8:
            error = f"download-info: repo_size should be 8, got {download_info.get('repo_size')}"
            errors.append(error)
            print(f"  ✗ {error}")
        else:
            print("  ✓ repo_size correctly shows 8 bytes")

        # Test commit endpoint
        print("\n[5/6] Testing commit endpoint...")
        commit_id = get_commit_head(LOCAL_SERVER, token, repo_id)
        print(f"HEAD commit: {commit_id}")

        commit = get_commit(LOCAL_SERVER, token, repo_id, commit_id)
        print(f"Commit response: {json.dumps(commit, indent=2)}")

        # Check commit fields
        commit_checks = [
            check_field(commit, "commit_id", "str", allow_empty=False),
            check_field(commit, "root_id", "str", allow_empty=False),
            check_field(commit, "repo_id", "str", allow_empty=False),
            check_field(commit, "creator_name", "str", allow_empty=False),
            check_field(commit, "creator", "str", allow_empty=False),
            check_field(commit, "description", "str", allow_empty=False),
            check_field(commit, "ctime", "int"),
            check_field(commit, "repo_name", "str", allow_empty=False),
            check_field(commit, "repo_desc", "str", expected_value=""),  # Must be empty string
            check_field(commit, "repo_category", "str", expected_value=""),  # Must be empty string
            check_field(commit, "version", "int", expected_value=1),
        ]

        for check in commit_checks:
            if check:
                errors.append(f"commit: {check}")
                print(f"  {check}")

        if not errors:
            print("  ✓ All commit fields correct")

        # Summary
        print("\n[6/6] Test Summary")
        print("=" * 70)
        if errors:
            print(f"\n✗ FAILED: {len(errors)} error(s) found:\n")
            for error in errors:
                print(f"  - {error}")
            return 1
        else:
            print("\n✓ SUCCESS: All protocol endpoints return correct formats")
            print("  - repo_desc: empty string ✓")
            print("  - repo_category: empty string ✓")
            print("  - repo_size: updates correctly ✓")
            print("  - Field types: all correct ✓")
            return 0

    except Exception as e:
        print(f"\n✗ ERROR: {e}")
        import traceback
        traceback.print_exc()
        return 1
    finally:
        # Cleanup
        if repo_id:
            print(f"\nCleaning up...")
            delete_library(LOCAL_SERVER, token, repo_id)
            print("✓ Cleanup complete")

if __name__ == "__main__":
    sys.exit(main())
