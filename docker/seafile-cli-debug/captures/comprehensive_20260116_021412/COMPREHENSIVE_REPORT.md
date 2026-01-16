# Seafile Encrypted Library Sync Protocol - Comprehensive Test Report

*Generated: 2026-01-16T02:14:17.837953*

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
  - Remote: `e97fcce576d743259ede3bfa15ad31b2591f0ad90718cd71014cd4f3a14acb3f`
  - Local: `c19f010d2f70b6c2db930aa9bc7646a697f283dec2621239a21b7eb561547abe`
- **random_key**: value_mismatch
  - Remote: `d672a0e41fce11908e8c136bd17c8491deaa3ed69ffcd96016aa9308e242f7ef26d878c39f663c43449f55a631912cb2`
  - Local: `f5720c1d4bc2910e738a60a3543deab6f38a989abf6b99075f69af1a275df946375ceb45dead41727ea63a20353c680a`

<details><summary>Remote Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "e97fcce576d743259ede3bfa15ad31b2591f0ad90718cd71014cd4f3a14acb3f",
  "random_key": "d672a0e41fce11908e8c136bd17c8491deaa3ed69ffcd96016aa9308e242f7ef26d878c39f663c43449f55a631912cb2",
  "salt": ""
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "c19f010d2f70b6c2db930aa9bc7646a697f283dec2621239a21b7eb561547abe",
  "random_key": "f5720c1d4bc2910e738a60a3543deab6f38a989abf6b99075f69af1a275df946375ceb45dead41727ea63a20353c680a",
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

- **random_key**: value_mismatch
  - Remote: `d672a0e41fce11908e8c136bd17c8491deaa3ed69ffcd96016aa9308e242f7ef26d878c39f663c43449f55a631912cb2`
  - Local: `f5720c1d4bc2910e738a60a3543deab6f38a989abf6b99075f69af1a275df946375ceb45dead41727ea63a20353c680a`
- **magic**: value_mismatch
  - Remote: `e97fcce576d743259ede3bfa15ad31b2591f0ad90718cd71014cd4f3a14acb3f`
  - Local: `c19f010d2f70b6c2db930aa9bc7646a697f283dec2621239a21b7eb561547abe`
- **mtime_relative**: value_mismatch
  - Remote: `<time datetime="2026-01-16T02:14:12" is="relative-time" title="Fri, 16 Jan 2026 02:14:12 +0000" >4 seconds ago</time>`
  - Local: `<time datetime="2026-01-16T02:14:16" is="relative-time" title="Fri, 16 Jan 2026 02:14:16 +0000" >1 second ago</time>`
- **repo_size_formatted**: value_mismatch
  - Remote: `0 bytes`
  - Local: `0 bytes`

<details><summary>Remote Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "e97fcce576d743259ede3bfa15ad31b2591f0ad90718cd71014cd4f3a14acb3f",
  "random_key": "d672a0e41fce11908e8c136bd17c8491deaa3ed69ffcd96016aa9308e242f7ef26d878c39f663c43449f55a631912cb2",
  "salt": ""
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "c19f010d2f70b6c2db930aa9bc7646a697f283dec2621239a21b7eb561547abe",
  "random_key": "f5720c1d4bc2910e738a60a3543deab6f38a989abf6b99075f69af1a275df946375ceb45dead41727ea63a20353c680a",
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
  "head_commit_id": "40623406c739d29d429255860ae68bd39a6ff8ad"
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "head_commit_id": "e31a8626b71b9b0d17ba0a56456f2a2c23613b6b",
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
  - Remote: `e97fcce576d743259ede3bfa15ad31b2591f0ad90718cd71014cd4f3a14acb3f`
  - Local: `c19f010d2f70b6c2db930aa9bc7646a697f283dec2621239a21b7eb561547abe`
- **key**: value_mismatch
  - Remote: `d672a0e41fce11908e8c136bd17c8491deaa3ed69ffcd96016aa9308e242f7ef26d878c39f663c43449f55a631912cb2`
  - Local: `f5720c1d4bc2910e738a60a3543deab6f38a989abf6b99075f69af1a275df946375ceb45dead41727ea63a20353c680a`

<details><summary>Remote Response</summary>

```json
{
  "encrypted": "true",
  "enc_version": 2,
  "magic": "e97fcce576d743259ede3bfa15ad31b2591f0ad90718cd71014cd4f3a14acb3f",
  "key": "d672a0e41fce11908e8c136bd17c8491deaa3ed69ffcd96016aa9308e242f7ef26d878c39f663c43449f55a631912cb2"
}
```
</details>

<details><summary>Local Response</summary>

```json
{
  "encrypted": "true",
  "enc_version": 2,
  "magic": "c19f010d2f70b6c2db930aa9bc7646a697f283dec2621239a21b7eb561547abe",
  "key": "f5720c1d4bc2910e738a60a3543deab6f38a989abf6b99075f69af1a275df946375ceb45dead41727ea63a20353c680a"
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
