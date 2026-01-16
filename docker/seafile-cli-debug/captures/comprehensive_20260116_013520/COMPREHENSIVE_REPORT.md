# Seafile Encrypted Library Sync Protocol - Comprehensive Test Report

*Generated: 2026-01-16T01:35:25.497175*

**Total Tests:** 12
**Passed:** 5 ✓
**Failed:** 7 ✗

## ⚠️ Issues Found

The following tests failed. Fix these issues to achieve full compatibility.

## Test Results

### ✓ PASS: Authentication

**Endpoint:** `POST /api2/auth-token/`

**Notes:**
- Both servers returned valid tokens

---

### ✓ PASS: Protocol Version

**Endpoint:** `GET /seafhttp/protocol-version`

**Notes:**
- Should return {"version": 2}

<details><summary>Remote Response</summary>

```json
{
  "version": 2
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "version": 2
}
```
</details>

---

### ✓ PASS: Server Info

**Endpoint:** `GET /api2/server-info/`

**Notes:**
- encrypted_library_version should be 2 for both servers

<details><summary>Remote Response</summary>

```json
{
  "version": "11.0.16",
  "encrypted_library_version": 2,
  "desktop-custom-brand": "NiHao Cloud",
  "features": [
    "seafile-basic",
    "seafile-pro",
    "file-search"
  ]
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "enable_encrypted_library": true,
  "enable_repo_history_setting": true,
  "enable_reset_encrypted_repo_password": false,
  "encrypted_library_version": 2,
  "version": "10.0.0"
}
```
</details>

---

### ✗ FAIL: Create Encrypted Library

**Endpoint:** `POST /api2/repos/ (with passwd parameter)`

**Notes:**
- CRITICAL: encrypted must be integer 1, not boolean true
- CRITICAL: enc_version must be integer 2, not string '2'
- magic must be 64 hex characters (PBKDF2 hash)
- random_key must be 96 hex characters (encrypted file key)
- Client sends only passwd parameter, NOT encrypted parameter

**Differences:**

- **encrypted**: type_mismatch
  - Remote: `int: 1`
  - Local: `bool: False`
- **magic**: value_mismatch
  - Remote: `a574c91a7cd1534a40a397b2d80ca6bbdc0acffc36360f0dce6c4f5af108df96`
  - Local: ``
- **random_key**: value_mismatch
  - Remote: `bccaf3ca7f3d232e65ca26bc84ac6520db35503526ba2281eaab0a8eac8978949c1fb29a05ef25179d158e1e55663e7e`
  - Local: ``

<details><summary>Remote Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "a574c91a7cd1534a40a397b2d80ca6bbdc0acffc36360f0dce6c4f5af108df96",
  "random_key": "bccaf3ca7f3d232e65ca26bc84ac6520db35503526ba2281eaab0a8eac8978949c1fb29a05ef25179d158e1e55663e7e",
  "salt": ""
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "encrypted": false,
  "enc_version": 0,
  "magic": "",
  "random_key": "",
  "salt": ""
}
```
</details>

---

### ✗ FAIL: Set Password (Correct)

**Endpoint:** `POST /api/v2.1/repos/{id}/set-password/`

**Notes:**
- Verifies PBKDF2 magic computation is correct
- Input: repo_id + password
- Should return {"success": true}

**Differences:**

- **success**: missing_in_local
- **error**: missing_in_remote

<details><summary>Remote Response</summary>

```json
{
  "success": true
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "error": "library is not encrypted"
}
```
</details>

---

### ✗ FAIL: Set Password (Wrong)

**Endpoint:** `POST /api/v2.1/repos/{id}/set-password/`

**Notes:**
- Should return error for wrong password
- {"error_msg": "Wrong password"}

**Differences:**

- **error_msg**: value_mismatch
  - Remote: `Wrong password`
  - Local: `None`

<details><summary>Remote Response</summary>

```json
{
  "error_msg": "Wrong password"
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "error": "library is not encrypted"
}
```
</details>

---

### ✗ FAIL: Download Info

**Endpoint:** `GET /api2/repos/{id}/download-info/`

**Notes:**
- Returns sync token for /seafhttp/ operations
- Encryption metadata should match library creation

**Differences:**

- **repo_size_formatted**: missing_in_local
- **salt**: missing_in_local
- **magic**: missing_in_local
- **enc_version**: missing_in_local
- **random_key**: missing_in_local
- **mtime_relative**: missing_in_local
- **is_corrupted**: missing_in_remote
- **encrypted**: type_mismatch
  - Remote: `int`
  - Local: `bool`

<details><summary>Remote Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "a574c91a7cd1534a40a397b2d80ca6bbdc0acffc36360f0dce6c4f5af108df96",
  "random_key": "bccaf3ca7f3d232e65ca26bc84ac6520db35503526ba2281eaab0a8eac8978949c1fb29a05ef25179d158e1e55663e7e",
  "salt": ""
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "encrypted": false,
  "enc_version": null,
  "magic": null,
  "random_key": null,
  "salt": null
}
```
</details>

---

### ✓ PASS: Commit HEAD

**Endpoint:** `GET /seafhttp/repo/{id}/commit/HEAD`

**Notes:**
- CRITICAL: is_corrupted must be integer 0, not boolean false
- Must include head_commit_id field

<details><summary>Remote Response</summary>

```json
{
  "is_corrupted": 0,
  "head_commit_id": "a53396b4b92a5349fcb5455c4fb2df3aecfa907a"
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "head_commit_id": "f1786164e725ed4222427d9e85020d2b88a3fb90",
  "is_corrupted": 0
}
```
</details>

---

### ✗ FAIL: Full Commit Object

**Endpoint:** `GET /seafhttp/repo/{id}/commit/{commit_id}`

**Notes:**
- For encrypted libraries: encrypted='true' (string!)
- enc_version should be integer 2
- magic and key should match library metadata

**Differences:**

- **key**: missing_in_local
- **magic**: missing_in_local
- **no_local_history**: missing_in_local
- **enc_version**: missing_in_local
- **encrypted**: missing_in_local

<details><summary>Remote Response</summary>

```json
{
  "encrypted": "true",
  "enc_version": 2,
  "magic": "a574c91a7cd1534a40a397b2d80ca6bbdc0acffc36360f0dce6c4f5af108df96",
  "key": "bccaf3ca7f3d232e65ca26bc84ac6520db35503526ba2281eaab0a8eac8978949c1fb29a05ef25179d158e1e55663e7e"
}
```
</details>

<details><summary>Local Response</summary>

```json
{}
```
</details>

---

### ✓ PASS: FS-ID-List

**Endpoint:** `GET /seafhttp/repo/{id}/fs-id-list/`

**Notes:**
- CRITICAL: Must return JSON array
- Should include all FS IDs (directories and files)
- For new library: should contain at least root directory fs_id

<details><summary>Remote Response</summary>

```json
{
  "type": "list",
  "count": 0
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "type": "list",
  "count": 1
}
```
</details>

---

### ✗ FAIL: Pack-FS Binary Format

**Endpoint:** `POST /seafhttp/repo/{id}/pack-fs/`

**Notes:**
- No FS IDs available for testing

---

### ✗ FAIL: Check-FS Endpoint

**Endpoint:** `POST /seafhttp/repo/{id}/check-fs`

**Notes:**
- No FS IDs available

---
