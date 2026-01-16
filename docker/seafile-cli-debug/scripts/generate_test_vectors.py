#!/usr/bin/env python3
"""
Generate test vectors for RFC specification
"""

import hashlib
import json
from Crypto.Cipher import AES
from Crypto.Util.Padding import pad
import binascii

def pbkdf2_sha256(password, salt, iterations, key_length):
    """PBKDF2-HMAC-SHA256"""
    return hashlib.pbkdf2_hmac('sha256', password, salt, iterations, key_length)

def compute_magic(repo_id, password):
    """Compute magic for password verification"""
    salt = bytes([0xda, 0x90, 0x45, 0xc3, 0x06, 0xc7, 0xcc, 0x26])

    # Input is repo_id + password
    input_data = (repo_id + password).encode('utf-8')

    # Derive key (32 bytes)
    key = pbkdf2_sha256(input_data, salt, 1000, 32)

    # Derive IV (16 bytes) using key as input
    iv = pbkdf2_sha256(key, salt, 10, 16)

    # Magic is hex(key) + hex(iv)
    magic = binascii.hexlify(key).decode('ascii') + binascii.hexlify(iv).decode('ascii')

    return magic, binascii.hexlify(key).decode('ascii'), binascii.hexlify(iv).decode('ascii')

def compute_fs_id(fs_object):
    """Compute fs_id (SHA-1 hash of JSON)"""
    # Serialize JSON with no spaces, sorted keys
    json_str = json.dumps(fs_object, separators=(',', ':'), sort_keys=True)
    json_bytes = json_str.encode('utf-8')

    # Compute SHA-1
    fs_id = hashlib.sha1(json_bytes).hexdigest()

    return fs_id, json_str

# Test Vector 1: PBKDF2 Magic
print("=" * 70)
print("TEST VECTOR 1: PBKDF2 Magic Derivation")
print("=" * 70)

repo_id = "00000000-0000-0000-0000-000000000000"
password = "password"

magic, key_hex, iv_hex = compute_magic(repo_id, password)

print(f"Input:")
print(f"  repo_id: \"{repo_id}\"")
print(f"  password: \"{password}\"")
print(f"  salt: {{0xda, 0x90, 0x45, 0xc3, 0x06, 0xc7, 0xcc, 0x26}}")
print(f"  iterations: 1000")
print()
print(f"Output:")
print(f"  key (32 bytes): {key_hex}")
print(f"  iv (16 bytes):  {iv_hex}")
print(f"  magic (64 hex): {magic}")
print()

# Test Vector 2: Empty Directory FS ID
print("=" * 70)
print("TEST VECTOR 2: FS ID Computation (Empty Directory)")
print("=" * 70)

empty_dir = {
    "dirents": [],
    "type": 3,
    "version": 1
}

fs_id, json_str = compute_fs_id(empty_dir)

print(f"Input JSON:")
print(json_str)
print()
print(f"Output:")
print(f"  fs_id (SHA-1): {fs_id}")
print()

# Test Vector 3: Directory with Single File
print("=" * 70)
print("TEST VECTOR 3: FS ID Computation (Directory with File)")
print("=" * 70)

dir_with_file = {
    "dirents": [
        {
            "id": "534d4ba7a4a21939cf5bb4db7962d74e4f2b483a",
            "mode": 33188,
            "modifier": "user@example.com",
            "mtime": 1768543179,
            "name": "test.txt",
            "size": 100
        }
    ],
    "type": 3,
    "version": 1
}

fs_id2, json_str2 = compute_fs_id(dir_with_file)

print(f"Input JSON:")
print(json_str2)
print()
print(f"Output:")
print(f"  fs_id (SHA-1): {fs_id2}")
print()

# Test Vector 4: File FS Object
print("=" * 70)
print("TEST VECTOR 4: FS ID Computation (File)")
print("=" * 70)

file_obj = {
    "block_ids": ["9bc34549d565d9505b287de0cd20ac77be1d3f2c"],
    "size": 100,
    "type": 1,
    "version": 1
}

fs_id3, json_str3 = compute_fs_id(file_obj)

print(f"Input JSON:")
print(json_str3)
print()
print(f"Output:")
print(f"  fs_id (SHA-1): {fs_id3}")
print()

# Summary
print("=" * 70)
print("SUMMARY - Copy these to RFC Section 11 (Test Vectors)")
print("=" * 70)
print()
print("11.1 PBKDF2 Test Vector")
print(f"  Expected key: {key_hex}")
print(f"  Expected iv:  {iv_hex}")
print(f"  Expected magic: {magic}")
print()
print("11.2 FS ID Test Vectors")
print(f"  Empty directory: {fs_id}")
print(f"  Directory with file: {fs_id2}")
print(f"  File object: {fs_id3}")
