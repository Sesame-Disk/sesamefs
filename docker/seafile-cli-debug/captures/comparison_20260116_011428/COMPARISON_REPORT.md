# Seafile Protocol Comparison Report

*Generated: 2026-01-16T01:14:28.425285*

**Total Issues Found:** 2

## Server Info

**Endpoint:** `/api2/server-info/`

**Differences:**

- **enable_encrypted_library**: missing_in_remote

**Remote Response:**
```json
{
  "encrypted_library_version": 2
}
```

**Local Response:**
```json
{
  "encrypted_library_version": 2,
  "enable_encrypted_library": true
}
```

---

## Create Encrypted Library

**Endpoint:** `POST /api2/repos/`

**Differences:**

- **encrypted**: type_mismatch
  - Remote: `int`
  - Local: `bool`
- **magic**: value_mismatch
  - Remote: `5c483b563a1f109c750a9ba2730a73955725efaf389149e3dd6c1b75330aa9e2`
  - Local: ``
- **random_key**: value_mismatch
  - Remote: `c9609146aca005703e7f1fb56b9bcd27c0e27bfe4446ddb636d81d6bf5a223a75358e9d5ff8e9df4a248c081ab3af165`
  - Local: ``
- **enc_version**: value_mismatch
  - Remote: `2`
  - Local: `0`

**Remote Response:**
```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "5c483b563a1f109c750a9ba2730a73955725efaf389149e3dd6c1b75330aa9e2",
  "random_key": "c9609146aca005703e7f1fb56b9bcd27c0e27bfe4446ddb636d81d6bf5a223a75358e9d5ff8e9df4a248c081ab3af165",
  "salt": ""
}
```

**Local Response:**
```json
{
  "encrypted": false,
  "enc_version": 0,
  "magic": "",
  "random_key": "",
  "salt": ""
}
```

---
