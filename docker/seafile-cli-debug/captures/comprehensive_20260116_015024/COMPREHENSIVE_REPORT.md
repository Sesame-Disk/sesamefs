# Seafile Encrypted Library Sync Protocol - Comprehensive Test Report

*Generated: 2026-01-16T01:50:30.103438*

**Total Tests:** 12
**Passed:** 7 ✓
**Failed:** 5 ✗

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

- **magic**: value_mismatch
  - Remote: `85652a45d3598e6570d3b69f265ee249d3808da9552c9f6f3aa50b11d97d400e`
  - Local: `bd4388b7231091a333efe05f9173d67f0dc8c5a359c11e2753985b9eeff15240`
- **random_key**: value_mismatch
  - Remote: `d3c3198620e0c9ffed16e1118ee97bb683912bc412ce95f493878ce8cd6bc5779db0cdf27285ff82f6977996d3ea5ac2`
  - Local: `dc57d19a49351ba106f9934b5e04fbdd51a53f3d0e8838b9e924d5e96df741b1ac7deb55d02a943143de1dfa33948f03`

<details><summary>Remote Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "85652a45d3598e6570d3b69f265ee249d3808da9552c9f6f3aa50b11d97d400e",
  "random_key": "d3c3198620e0c9ffed16e1118ee97bb683912bc412ce95f493878ce8cd6bc5779db0cdf27285ff82f6977996d3ea5ac2",
  "salt": ""
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "bd4388b7231091a333efe05f9173d67f0dc8c5a359c11e2753985b9eeff15240",
  "random_key": "dc57d19a49351ba106f9934b5e04fbdd51a53f3d0e8838b9e924d5e96df741b1ac7deb55d02a943143de1dfa33948f03",
  "salt": ""
}
```
</details>

---

### ✓ PASS: Set Password (Correct)

**Endpoint:** `POST /api/v2.1/repos/{id}/set-password/`

**Notes:**
- Verifies PBKDF2 magic computation is correct
- Input: repo_id + password
- Should return {"success": true}

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
  "success": true
}
```
</details>

---

### ✓ PASS: Set Password (Wrong)

**Endpoint:** `POST /api/v2.1/repos/{id}/set-password/`

**Notes:**
- Should return error for wrong password
- {"error_msg": "Wrong password"}

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
  "error_msg": "Wrong password"
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

- **salt**: missing_in_local
- **magic**: value_mismatch
  - Remote: `85652a45d3598e6570d3b69f265ee249d3808da9552c9f6f3aa50b11d97d400e`
  - Local: `bd4388b7231091a333efe05f9173d67f0dc8c5a359c11e2753985b9eeff15240`
- **is_corrupted**: missing_in_remote
- **repo_size_formatted**: missing_in_local
- **random_key**: value_mismatch
  - Remote: `d3c3198620e0c9ffed16e1118ee97bb683912bc412ce95f493878ce8cd6bc5779db0cdf27285ff82f6977996d3ea5ac2`
  - Local: `dc57d19a49351ba106f9934b5e04fbdd51a53f3d0e8838b9e924d5e96df741b1ac7deb55d02a943143de1dfa33948f03`
- **mtime_relative**: missing_in_local
- **encrypted**: type_mismatch
  - Remote: `int`
  - Local: `bool`

<details><summary>Remote Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "85652a45d3598e6570d3b69f265ee249d3808da9552c9f6f3aa50b11d97d400e",
  "random_key": "d3c3198620e0c9ffed16e1118ee97bb683912bc412ce95f493878ce8cd6bc5779db0cdf27285ff82f6977996d3ea5ac2",
  "salt": ""
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "encrypted": true,
  "enc_version": 2,
  "magic": "bd4388b7231091a333efe05f9173d67f0dc8c5a359c11e2753985b9eeff15240",
  "random_key": "dc57d19a49351ba106f9934b5e04fbdd51a53f3d0e8838b9e924d5e96df741b1ac7deb55d02a943143de1dfa33948f03",
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
  "head_commit_id": "fdc0d33fad634784047d4653ba22a5cfc1fb92c2"
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "head_commit_id": "89ea40be50332735bea516e73b6ea50a6dcedf7e",
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

- **key**: value_mismatch
  - Remote: `d3c3198620e0c9ffed16e1118ee97bb683912bc412ce95f493878ce8cd6bc5779db0cdf27285ff82f6977996d3ea5ac2`
  - Local: `dc57d19a49351ba106f9934b5e04fbdd51a53f3d0e8838b9e924d5e96df741b1ac7deb55d02a943143de1dfa33948f03`
- **no_local_history**: missing_in_local
- **magic**: value_mismatch
  - Remote: `85652a45d3598e6570d3b69f265ee249d3808da9552c9f6f3aa50b11d97d400e`
  - Local: `bd4388b7231091a333efe05f9173d67f0dc8c5a359c11e2753985b9eeff15240`

<details><summary>Remote Response</summary>

```json
{
  "encrypted": "true",
  "enc_version": 2,
  "magic": "85652a45d3598e6570d3b69f265ee249d3808da9552c9f6f3aa50b11d97d400e",
  "key": "d3c3198620e0c9ffed16e1118ee97bb683912bc412ce95f493878ce8cd6bc5779db0cdf27285ff82f6977996d3ea5ac2"
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "encrypted": "true",
  "enc_version": 2,
  "magic": "bd4388b7231091a333efe05f9173d67f0dc8c5a359c11e2753985b9eeff15240",
  "key": "dc57d19a49351ba106f9934b5e04fbdd51a53f3d0e8838b9e924d5e96df741b1ac7deb55d02a943143de1dfa33948f03"
}
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
