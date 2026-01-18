#!/usr/bin/env python3
"""
Test sync for library xxxTxT005 to diagnose missing files issue
"""
import os
import subprocess
import time

REPO_ID = "01920c46-b74b-4802-ad7c-db66732423ab"
SERVER_URL = "http://localhost:8080"
USERNAME = "abel.aguzmans@gmail.com"
PASSWORD = "dev-token-123"
CONFIG_DIR = "/tmp/seaf-test-config"
SYNC_DIR = "/tmp/seaf-test-sync"

def run_cmd(cmd):
    """Run command and return output"""
    result = subprocess.run(cmd, shell=True, capture_output=True, text=True)
    return result.returncode, result.stdout, result.stderr

def main():
    print("=" * 70)
    print("Testing sync for library xxxTxT005")
    print("=" * 70)

    # Clean up
    print("\n1. Cleaning up old data...")
    run_cmd(f"rm -rf {CONFIG_DIR} {SYNC_DIR}")
    os.makedirs(CONFIG_DIR, exist_ok=True)
    os.makedirs(SYNC_DIR, exist_ok=True)

    # Initialize
    print("2. Initializing seaf-cli...")
    rc, out, err = run_cmd(f"seaf-cli init -c {CONFIG_DIR} -d {CONFIG_DIR}")
    if rc != 0:
        print(f"   Init failed: {err}")
        return 1
    print("   ✓ Initialized")

    # Start daemon
    print("3. Starting seafile daemon...")
    rc, out, err = run_cmd(f"seaf-cli start -c {CONFIG_DIR}")
    if rc != 0:
        print(f"   Start failed: {err}")
        return 1
    time.sleep(2)
    print("   ✓ Daemon started")

    # Sync library
    print(f"4. Syncing library {REPO_ID}...")
    sync_cmd = f"seaf-cli download -c {CONFIG_DIR} -l {REPO_ID} -s {SERVER_URL} -u {USERNAME} -p {PASSWORD} -d {SYNC_DIR}"
    rc, out, err = run_cmd(sync_cmd)
    print(f"   Return code: {rc}")
    print(f"   Output: {out}")
    if err:
        print(f"   Error: {err}")

    # Wait for sync
    print("5. Waiting for sync to complete...")
    time.sleep(10)

    # Check sync status
    print("6. Checking sync status...")
    rc, out, err = run_cmd(f"seaf-cli status -c {CONFIG_DIR}")
    print(f"   Status: {out}")

    # Check synced files
    print("7. Checking synced files...")
    rc, out, err = run_cmd(f"ls -lah {SYNC_DIR}")
    print(f"   Files in sync dir:\n{out}")

    # Show logs
    print("\n" + "=" * 70)
    print("SEAFILE CLIENT LOG (last 100 lines)")
    print("=" * 70)
    rc, out, err = run_cmd(f"tail -100 {CONFIG_DIR}/logs/seafile.log")
    print(out)

    # Cleanup
    print("\n8. Stopping daemon...")
    run_cmd(f"seaf-cli stop -c {CONFIG_DIR}")

    return 0

if __name__ == "__main__":
    exit(main())
