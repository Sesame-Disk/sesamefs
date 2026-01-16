# Seafile Encrypted Library Sync Protocol - Comprehensive Test Report

*Generated: 2026-01-16T02:05:06.811236*

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
  - Remote: `239c867811ca2fe0a78a1e57c5c1423af2634e9a80c44c54cb4ab18f2055fb20`
  - Local: `7bc8569b24b5232463e0b7aa72dee1b4275c45a6f6a60fb8a6295ebb59680e0b`
- **random_key**: value_mismatch
  - Remote: `ec1a8232bf4732293c71de3fe2195df8e1d00237686d4a2672611e1ccd48967074665ea9f224208352ae2548551e22ba`
  - Local: `fc9a91349a87988e22e8d2f55d1c09a1ed21afa4d9af2a6c8fb14eb33bdb1ebcf1cd0a3d1f5dff822c3a7a972f908aa9`

<details><summary>Remote Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "239c867811ca2fe0a78a1e57c5c1423af2634e9a80c44c54cb4ab18f2055fb20",
  "random_key": "ec1a8232bf4732293c71de3fe2195df8e1d00237686d4a2672611e1ccd48967074665ea9f224208352ae2548551e22ba",
  "salt": ""
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "7bc8569b24b5232463e0b7aa72dee1b4275c45a6f6a60fb8a6295ebb59680e0b",
  "random_key": "fc9a91349a87988e22e8d2f55d1c09a1ed21afa4d9af2a6c8fb14eb33bdb1ebcf1cd0a3d1f5dff822c3a7a972f908aa9",
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

- **encrypted**: type_mismatch
  - Remote: `int`
  - Local: `bool`
- **is_corrupted**: missing_in_remote
- **mtime_relative**: missing_in_local
- **random_key**: value_mismatch
  - Remote: `ec1a8232bf4732293c71de3fe2195df8e1d00237686d4a2672611e1ccd48967074665ea9f224208352ae2548551e22ba`
  - Local: `fc9a91349a87988e22e8d2f55d1c09a1ed21afa4d9af2a6c8fb14eb33bdb1ebcf1cd0a3d1f5dff822c3a7a972f908aa9`
- **magic**: value_mismatch
  - Remote: `239c867811ca2fe0a78a1e57c5c1423af2634e9a80c44c54cb4ab18f2055fb20`
  - Local: `7bc8569b24b5232463e0b7aa72dee1b4275c45a6f6a60fb8a6295ebb59680e0b`
- **salt**: missing_in_local
- **repo_size_formatted**: missing_in_local

<details><summary>Remote Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "239c867811ca2fe0a78a1e57c5c1423af2634e9a80c44c54cb4ab18f2055fb20",
  "random_key": "ec1a8232bf4732293c71de3fe2195df8e1d00237686d4a2672611e1ccd48967074665ea9f224208352ae2548551e22ba",
  "salt": ""
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "encrypted": true,
  "enc_version": 2,
  "magic": "7bc8569b24b5232463e0b7aa72dee1b4275c45a6f6a60fb8a6295ebb59680e0b",
  "random_key": "fc9a91349a87988e22e8d2f55d1c09a1ed21afa4d9af2a6c8fb14eb33bdb1ebcf1cd0a3d1f5dff822c3a7a972f908aa9",
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
  "head_commit_id": "afd159b3994dcfbd0a43ef09a2033b4763254744"
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "head_commit_id": "2cf3ccf013ecbb71e1539655e5413d7d37e943dd",
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
  - Remote: `239c867811ca2fe0a78a1e57c5c1423af2634e9a80c44c54cb4ab18f2055fb20`
  - Local: `7bc8569b24b5232463e0b7aa72dee1b4275c45a6f6a60fb8a6295ebb59680e0b`
- **key**: value_mismatch
  - Remote: `ec1a8232bf4732293c71de3fe2195df8e1d00237686d4a2672611e1ccd48967074665ea9f224208352ae2548551e22ba`
  - Local: `fc9a91349a87988e22e8d2f55d1c09a1ed21afa4d9af2a6c8fb14eb33bdb1ebcf1cd0a3d1f5dff822c3a7a972f908aa9`

<details><summary>Remote Response</summary>

```json
{
  "encrypted": "true",
  "enc_version": 2,
  "magic": "239c867811ca2fe0a78a1e57c5c1423af2634e9a80c44c54cb4ab18f2055fb20",
  "key": "ec1a8232bf4732293c71de3fe2195df8e1d00237686d4a2672611e1ccd48967074665ea9f224208352ae2548551e22ba"
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "encrypted": "true",
  "enc_version": 2,
  "magic": "7bc8569b24b5232463e0b7aa72dee1b4275c45a6f6a60fb8a6295ebb59680e0b",
  "key": "fc9a91349a87988e22e8d2f55d1c09a1ed21afa4d9af2a6c8fb14eb33bdb1ebcf1cd0a3d1f5dff822c3a7a972f908aa9"
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
