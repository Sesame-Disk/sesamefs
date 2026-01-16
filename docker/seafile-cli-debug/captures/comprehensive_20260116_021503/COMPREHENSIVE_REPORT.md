# Seafile Encrypted Library Sync Protocol - Comprehensive Test Report

*Generated: 2026-01-16T02:15:05.987952*

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
  - Remote: `e5b17ee1b4813244533839b3ca366c85d06dcb82f267a61edaec5c35fdebd580`
  - Local: `e0057d571aece4584748b45ef29e0f7f4360f14038d279511eb444d728a23ca8`
- **random_key**: value_mismatch
  - Remote: `559e35e047dccc59e394a5996582c127e78c7d24ad213db0ab2885fbcca7fcb9f72e9ac22c1c23efb7397671140a5bfd`
  - Local: `0adf695deb87cfc4465951dbf1f1f449db012211ac22cb64790d6571916100a9c33c08e6b85be8af36f56a4258681225`

<details><summary>Remote Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "e5b17ee1b4813244533839b3ca366c85d06dcb82f267a61edaec5c35fdebd580",
  "random_key": "559e35e047dccc59e394a5996582c127e78c7d24ad213db0ab2885fbcca7fcb9f72e9ac22c1c23efb7397671140a5bfd",
  "salt": ""
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "e0057d571aece4584748b45ef29e0f7f4360f14038d279511eb444d728a23ca8",
  "random_key": "0adf695deb87cfc4465951dbf1f1f449db012211ac22cb64790d6571916100a9c33c08e6b85be8af36f56a4258681225",
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

- **repo_size_formatted**: value_mismatch
  - Remote: `0 bytes`
  - Local: `0 bytes`
- **random_key**: value_mismatch
  - Remote: `559e35e047dccc59e394a5996582c127e78c7d24ad213db0ab2885fbcca7fcb9f72e9ac22c1c23efb7397671140a5bfd`
  - Local: `0adf695deb87cfc4465951dbf1f1f449db012211ac22cb64790d6571916100a9c33c08e6b85be8af36f56a4258681225`
- **magic**: value_mismatch
  - Remote: `e5b17ee1b4813244533839b3ca366c85d06dcb82f267a61edaec5c35fdebd580`
  - Local: `e0057d571aece4584748b45ef29e0f7f4360f14038d279511eb444d728a23ca8`

<details><summary>Remote Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "e5b17ee1b4813244533839b3ca366c85d06dcb82f267a61edaec5c35fdebd580",
  "random_key": "559e35e047dccc59e394a5996582c127e78c7d24ad213db0ab2885fbcca7fcb9f72e9ac22c1c23efb7397671140a5bfd",
  "salt": ""
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "e0057d571aece4584748b45ef29e0f7f4360f14038d279511eb444d728a23ca8",
  "random_key": "0adf695deb87cfc4465951dbf1f1f449db012211ac22cb64790d6571916100a9c33c08e6b85be8af36f56a4258681225",
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
  "head_commit_id": "97db181ceea67dfd1aae3a348d1f47d1a8311812"
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "head_commit_id": "8ae53bd81d9f84c5169949ed04478e33ea7503b8",
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
  - Remote: `e5b17ee1b4813244533839b3ca366c85d06dcb82f267a61edaec5c35fdebd580`
  - Local: `e0057d571aece4584748b45ef29e0f7f4360f14038d279511eb444d728a23ca8`
- **key**: value_mismatch
  - Remote: `559e35e047dccc59e394a5996582c127e78c7d24ad213db0ab2885fbcca7fcb9f72e9ac22c1c23efb7397671140a5bfd`
  - Local: `0adf695deb87cfc4465951dbf1f1f449db012211ac22cb64790d6571916100a9c33c08e6b85be8af36f56a4258681225`

<details><summary>Remote Response</summary>

```json
{
  "encrypted": "true",
  "enc_version": 2,
  "magic": "e5b17ee1b4813244533839b3ca366c85d06dcb82f267a61edaec5c35fdebd580",
  "key": "559e35e047dccc59e394a5996582c127e78c7d24ad213db0ab2885fbcca7fcb9f72e9ac22c1c23efb7397671140a5bfd"
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "encrypted": "true",
  "enc_version": 2,
  "magic": "e0057d571aece4584748b45ef29e0f7f4360f14038d279511eb444d728a23ca8",
  "key": "0adf695deb87cfc4465951dbf1f1f449db012211ac22cb64790d6571916100a9c33c08e6b85be8af36f56a4258681225"
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
