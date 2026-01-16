# Seafile Encrypted Library Sync Protocol - Comprehensive Test Report

*Generated: 2026-01-16T02:17:39.348865*

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
  - Remote: `7e356b3bc69861e4eb55e89d43fdbdeafe8dab443b4e8d5ae6053bef6f14775d`
  - Local: `b2314ae2de63957e0c591b152de3e96608a4857473646a7cb5de94df7dd83419`
- **random_key**: value_mismatch
  - Remote: `c617ab5b29a5c4292b77a8aa00490ac842463d64bece8666a51d8080f54ec111ef9f386b7055870fe46590a3b619f353`
  - Local: `ca4cd7b9e95dd67e7234ddf09429731f180aa39069de35ec73036c7e505fbe25e891097fc128f19df50f6c2508f53236`

<details><summary>Remote Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "7e356b3bc69861e4eb55e89d43fdbdeafe8dab443b4e8d5ae6053bef6f14775d",
  "random_key": "c617ab5b29a5c4292b77a8aa00490ac842463d64bece8666a51d8080f54ec111ef9f386b7055870fe46590a3b619f353",
  "salt": ""
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "b2314ae2de63957e0c591b152de3e96608a4857473646a7cb5de94df7dd83419",
  "random_key": "ca4cd7b9e95dd67e7234ddf09429731f180aa39069de35ec73036c7e505fbe25e891097fc128f19df50f6c2508f53236",
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

- **magic**: value_mismatch
  - Remote: `7e356b3bc69861e4eb55e89d43fdbdeafe8dab443b4e8d5ae6053bef6f14775d`
  - Local: `b2314ae2de63957e0c591b152de3e96608a4857473646a7cb5de94df7dd83419`
- **mtime_relative**: value_mismatch
  - Remote: `<time datetime="2026-01-16T02:17:35" is="relative-time" title="Fri, 16 Jan 2026 02:17:35 +0000" >3 seconds ago</time>`
  - Local: `<time datetime="2026-01-16T02:17:37" is="relative-time" title="Fri, 16 Jan 2026 02:17:37 +0000" >1 second ago</time>`
- **random_key**: value_mismatch
  - Remote: `c617ab5b29a5c4292b77a8aa00490ac842463d64bece8666a51d8080f54ec111ef9f386b7055870fe46590a3b619f353`
  - Local: `ca4cd7b9e95dd67e7234ddf09429731f180aa39069de35ec73036c7e505fbe25e891097fc128f19df50f6c2508f53236`

<details><summary>Remote Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "7e356b3bc69861e4eb55e89d43fdbdeafe8dab443b4e8d5ae6053bef6f14775d",
  "random_key": "c617ab5b29a5c4292b77a8aa00490ac842463d64bece8666a51d8080f54ec111ef9f386b7055870fe46590a3b619f353",
  "salt": ""
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "b2314ae2de63957e0c591b152de3e96608a4857473646a7cb5de94df7dd83419",
  "random_key": "ca4cd7b9e95dd67e7234ddf09429731f180aa39069de35ec73036c7e505fbe25e891097fc128f19df50f6c2508f53236",
  "salt": ""
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
  "head_commit_id": "bf4da203a096410c944b1b6683b8c22617b96921"
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "head_commit_id": "dbbb85c08a88bd62958984f6caa4e475a4898381",
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

- **magic**: value_mismatch
  - Remote: `7e356b3bc69861e4eb55e89d43fdbdeafe8dab443b4e8d5ae6053bef6f14775d`
  - Local: `b2314ae2de63957e0c591b152de3e96608a4857473646a7cb5de94df7dd83419`
- **key**: value_mismatch
  - Remote: `c617ab5b29a5c4292b77a8aa00490ac842463d64bece8666a51d8080f54ec111ef9f386b7055870fe46590a3b619f353`
  - Local: `ca4cd7b9e95dd67e7234ddf09429731f180aa39069de35ec73036c7e505fbe25e891097fc128f19df50f6c2508f53236`

<details><summary>Remote Response</summary>

```json
{
  "encrypted": "true",
  "enc_version": 2,
  "magic": "7e356b3bc69861e4eb55e89d43fdbdeafe8dab443b4e8d5ae6053bef6f14775d",
  "key": "c617ab5b29a5c4292b77a8aa00490ac842463d64bece8666a51d8080f54ec111ef9f386b7055870fe46590a3b619f353"
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "encrypted": "true",
  "enc_version": 2,
  "magic": "b2314ae2de63957e0c591b152de3e96608a4857473646a7cb5de94df7dd83419",
  "key": "ca4cd7b9e95dd67e7234ddf09429731f180aa39069de35ec73036c7e505fbe25e891097fc128f19df50f6c2508f53236"
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
