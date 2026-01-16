# Seafile Protocol Comparison Report

*Generated: 2026-01-16T01:10:11.483838*

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

- **enc_version**: value_mismatch
  - Remote: `2`
  - Local: `0`
- **random_key**: value_mismatch
  - Remote: `9f1b8938dbc1ac026627a51b02c27e32f1f9928bafa2f49930dd730b57f4874e7626a009de08aec70530f3a8bb4676ac`
  - Local: ``
- **encrypted**: type_mismatch
  - Remote: `int`
  - Local: `bool`
- **magic**: value_mismatch
  - Remote: `f61f4ddaa05eb435ac3124b5b8482e6f0527a2f188bc8d80dca189c0bea567fd`
  - Local: ``

**Remote Response:**
```json
{
  "encrypted": 1,
  "enc_version": 2,
  "magic": "f61f4ddaa05eb435ac3124b5b8482e6f0527a2f188bc8d80dca189c0bea567fd",
  "random_key": "9f1b8938dbc1ac026627a51b02c27e32f1f9928bafa2f49930dd730b57f4874e7626a009de08aec70530f3a8bb4676ac",
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
