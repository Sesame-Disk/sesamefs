# Seafile Encrypted Library Sync Protocol - Comprehensive Test Report

*Generated: 2026-01-16T01:45:27.738247*

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
  - Remote: `c92951b943957488752b3a845d234ee2d4d66c6b3ac44866e7e80a690ee250c0`
  - Local: `b5ee9c4cf777a803069c30ace902d329fc7da1de9987d14879b64f1488eb1732`
- **random_key**: value_mismatch
  - Remote: `8d1b00d46d4867e456b3845a170a4b5fc2effac0da531d73096f0a43f47966a5730ebfcd430501237bbfaa7a53203688`
  - Local: `c87a3374b49c8a488a231f727f0d1a2005648ecea83ef03bef6e0d7b2995fcf53fe74c84b9303fd16f710f9290c90411`
- **salt**: value_mismatch
  - Remote: ``
  - Local: `e0c9e35b81784509d18fd13bf2f34ae180a11ad0b2d3f0b3807b91132ad0fa5d`

<details><summary>Remote Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "c92951b943957488752b3a845d234ee2d4d66c6b3ac44866e7e80a690ee250c0",
  "random_key": "8d1b00d46d4867e456b3845a170a4b5fc2effac0da531d73096f0a43f47966a5730ebfcd430501237bbfaa7a53203688",
  "salt": ""
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "b5ee9c4cf777a803069c30ace902d329fc7da1de9987d14879b64f1488eb1732",
  "random_key": "c87a3374b49c8a488a231f727f0d1a2005648ecea83ef03bef6e0d7b2995fcf53fe74c84b9303fd16f710f9290c90411",
  "salt": "e0c9e35b81784509d18fd13bf2f34ae180a11ad0b2d3f0b3807b91132ad0fa5d"
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

- **encrypted**: type_mismatch
  - Remote: `int`
  - Local: `bool`
- **salt**: missing_in_local
- **mtime_relative**: missing_in_local
- **repo_size_formatted**: missing_in_local
- **magic**: value_mismatch
  - Remote: `c92951b943957488752b3a845d234ee2d4d66c6b3ac44866e7e80a690ee250c0`
  - Local: `b5ee9c4cf777a803069c30ace902d329fc7da1de9987d14879b64f1488eb1732`
- **random_key**: value_mismatch
  - Remote: `8d1b00d46d4867e456b3845a170a4b5fc2effac0da531d73096f0a43f47966a5730ebfcd430501237bbfaa7a53203688`
  - Local: `c87a3374b49c8a488a231f727f0d1a2005648ecea83ef03bef6e0d7b2995fcf53fe74c84b9303fd16f710f9290c90411`
- **is_corrupted**: missing_in_remote

<details><summary>Remote Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "c92951b943957488752b3a845d234ee2d4d66c6b3ac44866e7e80a690ee250c0",
  "random_key": "8d1b00d46d4867e456b3845a170a4b5fc2effac0da531d73096f0a43f47966a5730ebfcd430501237bbfaa7a53203688",
  "salt": ""
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "encrypted": true,
  "enc_version": 2,
  "magic": "b5ee9c4cf777a803069c30ace902d329fc7da1de9987d14879b64f1488eb1732",
  "random_key": "c87a3374b49c8a488a231f727f0d1a2005648ecea83ef03bef6e0d7b2995fcf53fe74c84b9303fd16f710f9290c90411",
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
  "head_commit_id": "70ec6075954df1213355528622d475ec4c74e44e"
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "head_commit_id": "aa5125afd0da4799f61a38c0f152dbd062f2845d",
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
  - Remote: `c92951b943957488752b3a845d234ee2d4d66c6b3ac44866e7e80a690ee250c0`
  - Local: `b5ee9c4cf777a803069c30ace902d329fc7da1de9987d14879b64f1488eb1732`
- **key**: value_mismatch
  - Remote: `8d1b00d46d4867e456b3845a170a4b5fc2effac0da531d73096f0a43f47966a5730ebfcd430501237bbfaa7a53203688`
  - Local: `c87a3374b49c8a488a231f727f0d1a2005648ecea83ef03bef6e0d7b2995fcf53fe74c84b9303fd16f710f9290c90411`
- **no_local_history**: missing_in_local

<details><summary>Remote Response</summary>

```json
{
  "encrypted": "true",
  "enc_version": 2,
  "magic": "c92951b943957488752b3a845d234ee2d4d66c6b3ac44866e7e80a690ee250c0",
  "key": "8d1b00d46d4867e456b3845a170a4b5fc2effac0da531d73096f0a43f47966a5730ebfcd430501237bbfaa7a53203688"
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "encrypted": "true",
  "enc_version": 2,
  "magic": "b5ee9c4cf777a803069c30ace902d329fc7da1de9987d14879b64f1488eb1732",
  "key": "c87a3374b49c8a488a231f727f0d1a2005648ecea83ef03bef6e0d7b2995fcf53fe74c84b9303fd16f710f9290c90411"
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
