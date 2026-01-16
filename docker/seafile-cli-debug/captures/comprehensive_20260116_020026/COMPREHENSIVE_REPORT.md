# Seafile Encrypted Library Sync Protocol - Comprehensive Test Report

*Generated: 2026-01-16T02:00:31.929563*

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
  - Remote: `4921c5b809741d47f66bafcc49f4e6b8bc172e6384fba0b2337c9bb7aea14735`
  - Local: `2d4641ded69c403463c62d106ff6571e998d61cd6255c1098bfc400518c2a541`
- **random_key**: value_mismatch
  - Remote: `035b2fe4b015325bf90ca9753c9b05d34a41b97958978753a4c67e5626893ecd55d9d09672825e614cad91eb60dc79e1`
  - Local: `520ae3f67de56de1aec27eb5d37da4f8e443f7294f4e969e4fa940f625c712a28e4f5f0b6dddb04fc90764013182bd0e`

<details><summary>Remote Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "4921c5b809741d47f66bafcc49f4e6b8bc172e6384fba0b2337c9bb7aea14735",
  "random_key": "035b2fe4b015325bf90ca9753c9b05d34a41b97958978753a4c67e5626893ecd55d9d09672825e614cad91eb60dc79e1",
  "salt": ""
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "2d4641ded69c403463c62d106ff6571e998d61cd6255c1098bfc400518c2a541",
  "random_key": "520ae3f67de56de1aec27eb5d37da4f8e443f7294f4e969e4fa940f625c712a28e4f5f0b6dddb04fc90764013182bd0e",
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

- **mtime_relative**: missing_in_local
- **random_key**: value_mismatch
  - Remote: `035b2fe4b015325bf90ca9753c9b05d34a41b97958978753a4c67e5626893ecd55d9d09672825e614cad91eb60dc79e1`
  - Local: `520ae3f67de56de1aec27eb5d37da4f8e443f7294f4e969e4fa940f625c712a28e4f5f0b6dddb04fc90764013182bd0e`
- **magic**: value_mismatch
  - Remote: `4921c5b809741d47f66bafcc49f4e6b8bc172e6384fba0b2337c9bb7aea14735`
  - Local: `2d4641ded69c403463c62d106ff6571e998d61cd6255c1098bfc400518c2a541`
- **encrypted**: type_mismatch
  - Remote: `int`
  - Local: `bool`
- **repo_size_formatted**: missing_in_local
- **is_corrupted**: missing_in_remote
- **salt**: missing_in_local

<details><summary>Remote Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "4921c5b809741d47f66bafcc49f4e6b8bc172e6384fba0b2337c9bb7aea14735",
  "random_key": "035b2fe4b015325bf90ca9753c9b05d34a41b97958978753a4c67e5626893ecd55d9d09672825e614cad91eb60dc79e1",
  "salt": ""
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "encrypted": true,
  "enc_version": 2,
  "magic": "2d4641ded69c403463c62d106ff6571e998d61cd6255c1098bfc400518c2a541",
  "random_key": "520ae3f67de56de1aec27eb5d37da4f8e443f7294f4e969e4fa940f625c712a28e4f5f0b6dddb04fc90764013182bd0e",
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
  "head_commit_id": "3cdac9047df505736ac372b096b2dc08da74a210"
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "head_commit_id": "b1fc965d9b334ce5f744a2319299a1b8f60dea75",
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
  - Remote: `4921c5b809741d47f66bafcc49f4e6b8bc172e6384fba0b2337c9bb7aea14735`
  - Local: `2d4641ded69c403463c62d106ff6571e998d61cd6255c1098bfc400518c2a541`
- **no_local_history**: missing_in_local
- **key**: value_mismatch
  - Remote: `035b2fe4b015325bf90ca9753c9b05d34a41b97958978753a4c67e5626893ecd55d9d09672825e614cad91eb60dc79e1`
  - Local: `520ae3f67de56de1aec27eb5d37da4f8e443f7294f4e969e4fa940f625c712a28e4f5f0b6dddb04fc90764013182bd0e`

<details><summary>Remote Response</summary>

```json
{
  "encrypted": "true",
  "enc_version": 2,
  "magic": "4921c5b809741d47f66bafcc49f4e6b8bc172e6384fba0b2337c9bb7aea14735",
  "key": "035b2fe4b015325bf90ca9753c9b05d34a41b97958978753a4c67e5626893ecd55d9d09672825e614cad91eb60dc79e1"
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "encrypted": "true",
  "enc_version": 2,
  "magic": "2d4641ded69c403463c62d106ff6571e998d61cd6255c1098bfc400518c2a541",
  "key": "520ae3f67de56de1aec27eb5d37da4f8e443f7294f4e969e4fa940f625c712a28e4f5f0b6dddb04fc90764013182bd0e"
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
