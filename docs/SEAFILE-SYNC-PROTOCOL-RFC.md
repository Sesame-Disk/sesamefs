# Seafile Desktop Client Sync Protocol - RFC Specification

**Status**: Draft Standard
**Version**: 2
**Date**: 2026-01-16
**Verification**: Production Seafile server (app.nihaoconsult.com)

---

## Abstract

This document specifies the Seafile Desktop Client Synchronization Protocol version 2, enabling bidirectional file synchronization between Seafile clients and servers. The protocol supports encrypted and unencrypted libraries, content-defined chunking, and delta synchronization.

---

## 1. Introduction

### 1.1 Purpose

This specification defines the complete Seafile sync protocol to enable independent implementations of Seafile-compatible servers and clients.

### 1.2 Requirements Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in RFC 2119.

### 1.3 Terminology

- **Library**: A synchronized repository containing files and directories
- **Commit**: An immutable snapshot of library state at a point in time
- **FS Object**: A filesystem object (directory or file metadata)
- **Block**: A content-addressed chunk of file data
- **Sync Token**: A session token for sync protocol operations
- **Magic**: A password verification hash for encrypted libraries
- **Random Key**: An encrypted symmetric key for file content encryption

---

## 2. Protocol Architecture

### 2.1 Protocol Layers

```
┌─────────────────────────────────────┐
│   Application (Desktop Client)      │
├─────────────────────────────────────┤
│   Sync Protocol (this spec)         │
├─────────────────────────────────────┤
│   HTTP/1.1                          │
├─────────────────────────────────────┤
│   TCP                               │
└─────────────────────────────────────┘
```

### 2.2 URL Namespace

Implementations MUST support two URL namespaces:

1. **REST API**: `/api2/`, `/api/v2.1/` - Library management, authentication
2. **Sync Protocol**: `/seafhttp/` - Synchronization operations

### 2.3 Authentication

#### 2.3.1 API Token Authentication

REST API endpoints MUST accept:
```
Authorization: Token {api_token}
```

Where `{api_token}` is obtained via `POST /api2/auth-token/`.

#### 2.3.2 Sync Token Authentication

Sync protocol endpoints MUST accept:
```
Seafile-Repo-Token: {sync_token}
```

Where `{sync_token}` is obtained from `GET /api2/repos/{id}/download-info/` response field `token`.

Implementations MUST NOT accept API tokens in place of sync tokens for `/seafhttp/` endpoints.

---

## 3. Data Types

### 3.1 Primitive Types

| Type | Description | Format | Example |
|------|-------------|--------|---------|
| `sha1` | SHA-1 hash | 40 lowercase hex digits | `"4702e8382675a4a062cb49cc5dc175c093f7effe"` |
| `uuid` | Library ID | UUID v4 format | `"256b7b88-d9cf-44d1-ba46-a5bb0bf0ebf7"` |
| `timestamp` | Unix timestamp | 64-bit unsigned integer | `1768543179` |
| `hex64` | 64 hex digits | Lowercase hex string | `"eee7b4a7..."` (password hash) |
| `hex96` | 96 hex digits | Lowercase hex string | `"406ec194..."` (encrypted key) |

### 3.2 Field Type Requirements

Implementations MUST use exact JSON types as specified. Type mismatches WILL cause client failures.

**Critical Type Mappings:**

| Context | Field | Type | Invalid Types |
|---------|-------|------|---------------|
| download-info | `encrypted` | integer (0, 1) | boolean, string |
| download-info | `salt` | string ("") | null |
| commit object | `encrypted` | string ("true") | integer, boolean |
| commit object | `is_corrupted` | integer (0, 1) | boolean |
| fs-id-list | response | JSON array | newline-separated text |

---

## 4. Protocol Flow

### 4.1 Sync State Machine

```
IDLE
  │
  ├─→ INIT_SYNC
  │     │
  │     ├─→ GET_SYNC_TOKEN (download-info)
  │     │
  │     ├─→ GET_HEAD_COMMIT
  │     │
  │     ├─→ GET_COMMIT_OBJECT
  │     │
  │     ├─→ DOWNLOAD_FS_METADATA
  │     │     ├─→ GET_FS_ID_LIST
  │     │     └─→ PACK_FS
  │     │
  │     ├─→ DOWNLOAD_BLOCKS
  │     │     ├─→ CHECK_BLOCKS
  │     │     └─→ GET_BLOCK (for each missing)
  │     │
  │     └─→ SYNCED
  │
  └─→ ERROR_RETRY (exponential backoff)
        └─→ IDLE (after delay)
```

### 4.2 Mandatory Sequence

Clients MUST follow this sequence for initial sync:

1. `GET /api2/repos/{id}/download-info/` → obtain sync_token
2. `GET /seafhttp/repo/{id}/commit/HEAD` → obtain head_commit_id
3. `GET /seafhttp/repo/{id}/commit/{commit_id}` → obtain root_id
4. `GET /seafhttp/repo/{id}/fs-id-list/?server-head={commit_id}` → obtain fs_ids
5. `POST /seafhttp/repo/{id}/pack-fs` → download FS objects
6. `POST /seafhttp/repo/{id}/check-blocks` → identify missing blocks
7. `GET /seafhttp/repo/{id}/block/{block_id}` → download each missing block

---

## 5. Core Endpoints

### 5.1 Protocol Version

**Endpoint:** `GET /seafhttp/protocol-version`

**Request:**
```http
GET /seafhttp/protocol-version HTTP/1.1
Host: example.com
```

**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/json

{"version": 2}
```

**Schema:**
```
version: integer (REQUIRED)
  Range: [2, 2]
  Description: Protocol version number
```

Implementations MUST return version 2. Clients MAY reject version != 2.

---

### 5.2 Download Info

**Endpoint:** `GET /api2/repos/{repo_id}/download-info/`

**Request:**
```http
GET /api2/repos/256b7b88-d9cf-44d1-ba46-a5bb0bf0ebf7/download-info/ HTTP/1.1
Host: example.com
Authorization: Token {api_token}
```

**Response (Encrypted Library):**
```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "token": "25979dd59d1b4ea95aa1c86028db4b0ad7498e61",
  "repo_id": "256b7b88-d9cf-44d1-ba46-a5bb0bf0ebf7",
  "repo_name": "Encrypted Library",
  "repo_desc": "",
  "repo_size": 0,
  "repo_size_formatted": "0\u00a0bytes",
  "repo_version": 1,
  "mtime": 1768543179,
  "mtime_relative": "<time datetime=\"2026-01-16T05:52:59\" is=\"relative-time\" title=\"Thu, 16 Jan 2026 05:52:59 +0000\" >1 minute ago</time>",
  "encrypted": 1,
  "enc_version": 2,
  "salt": "",
  "magic": "eee7b4a7a2539f2e4fb1c88a40121077152a5dc2c223f607f8b5e0838affde61",
  "random_key": "406ec194a0d0f7985b831b040034d829a4c68fbd354982f58101d0b6edd0232efed903cd1c404d0a66892473be968f19",
  "head_commit_id": "0342dc93ceca5acc782059a13347ef249055ea12",
  "permission": "rw",
  "relay_id": "localhost",
  "relay_addr": "localhost",
  "relay_port": "8080",
  "email": "user@example.com"
}
```

**Schema:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `token` | string | REQUIRED | Sync token for `/seafhttp/` endpoints |
| `repo_id` | uuid | REQUIRED | Library identifier |
| `repo_name` | string | REQUIRED | Library display name |
| `repo_desc` | string | REQUIRED | Library description (MAY be empty string) |
| `repo_size` | integer | REQUIRED | Total size in bytes (unsigned 64-bit) |
| `repo_size_formatted` | string | REQUIRED | Human-readable size with U+00A0 non-breaking space |
| `repo_version` | integer | REQUIRED | Repository format version (MUST be 1) |
| `mtime` | timestamp | REQUIRED | Last modification time (Unix seconds) |
| `mtime_relative` | string | REQUIRED | HTML time element (see 5.2.1) |
| `encrypted` | integer | REQUIRED | 0 = unencrypted, 1 = encrypted (NOT boolean) |
| `enc_version` | integer | REQUIRED | Encryption version: 0, 1, or 2 |
| `salt` | string | REQUIRED | Empty string "" for enc_version 2 (NOT null) |
| `magic` | hex64 | encrypted only | Password verification hash |
| `random_key` | hex96 | encrypted only | Encrypted file key |
| `head_commit_id` | sha1 | REQUIRED | Current HEAD commit |
| `permission` | string | REQUIRED | "r" or "rw" |
| `relay_id` | string | REQUIRED | Server identifier |
| `relay_addr` | string | REQUIRED | Server hostname/IP |
| `relay_port` | string | REQUIRED | Server port |
| `email` | string | REQUIRED | User email |

#### 5.2.1 HTML Time Element Format

The `mtime_relative` field MUST be an HTML time element:
```html
<time datetime="{iso8601}" is="relative-time" title="{rfc2822}" >{relative}</time>
```

Where:
- `{iso8601}`: ISO 8601 UTC format `YYYY-MM-DDTHH:MM:SS`
- `{rfc2822}`: RFC 2822 format `Day, DD Mon YYYY HH:MM:SS +0000`
- `{relative}`: Human-readable relative time (e.g., "2 hours ago")

**Errors:**

| Code | Condition | Response |
|------|-----------|----------|
| 401 | Invalid/missing token | `{"error": "Invalid token"}` |
| 403 | Library encrypted and locked | `{"error": "Library is encrypted"}` |
| 404 | Library not found | `{"error": "Library not found"}` |

---

### 5.3 Commit HEAD

**Endpoint:** `GET /seafhttp/repo/{repo_id}/commit/HEAD`

**Request:**
```http
GET /seafhttp/repo/256b7b88-d9cf-44d1-ba46-a5bb0bf0ebf7/commit/HEAD HTTP/1.1
Host: example.com
Seafile-Repo-Token: 25979dd59d1b4ea95aa1c86028db4b0ad7498e61
```

**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "is_corrupted": 0,
  "head_commit_id": "0342dc93ceca5acc782059a13347ef249055ea12"
}
```

**Schema:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `is_corrupted` | integer | REQUIRED | 0 = valid, 1 = corrupted (NOT boolean) |
| `head_commit_id` | sha1 | REQUIRED | Current HEAD commit SHA-1 |

**Errors:**

| Code | Condition |
|------|-----------|
| 400 | Missing/invalid sync token |
| 404 | Library not found |

---

### 5.4 Commit Object

**Endpoint:** `GET /seafhttp/repo/{repo_id}/commit/{commit_id}`

**Request:**
```http
GET /seafhttp/repo/256b7b88-d9cf-44d1-ba46-a5bb0bf0ebf7/commit/0342dc93ceca5acc782059a13347ef249055ea12 HTTP/1.1
Host: example.com
Seafile-Repo-Token: 25979dd59d1b4ea95aa1c86028db4b0ad7498e61
```

**Response (Encrypted Library):**
```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "commit_id": "0342dc93ceca5acc782059a13347ef249055ea12",
  "repo_id": "256b7b88-d9cf-44d1-ba46-a5bb0bf0ebf7",
  "root_id": "87199e6b76b84e56eef6b572bffdaa1067556489",
  "creator_name": "user@example.com",
  "creator": "0000000000000000000000000000000000000000",
  "description": "Added file.txt",
  "ctime": 1768543179,
  "parent_id": "8125065ad7abe077c1b1dcfbb4f491eb41987415",
  "second_parent_id": null,
  "repo_name": "Encrypted Library",
  "repo_desc": "",
  "repo_category": "",
  "encrypted": "true",
  "enc_version": 2,
  "magic": "eee7b4a7a2539f2e4fb1c88a40121077152a5dc2c223f607f8b5e0838affde61",
  "key": "406ec194a0d0f7985b831b040034d829a4c68fbd354982f58101d0b6edd0232efed903cd1c404d0a66892473be968f19",
  "version": 1
}
```

**Schema:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `commit_id` | sha1 | REQUIRED | Commit identifier |
| `repo_id` | uuid | REQUIRED | Library identifier |
| `root_id` | sha1 | REQUIRED | Root directory FS object ID |
| `creator_name` | string | REQUIRED | Commit author email/name |
| `creator` | string | REQUIRED | 40 zero characters for client commits |
| `description` | string | REQUIRED | Commit message |
| `ctime` | timestamp | REQUIRED | Commit creation time |
| `parent_id` | sha1\|null | REQUIRED | Previous commit (null for initial) |
| `second_parent_id` | sha1\|null | REQUIRED | Second parent for merge (usually null) |
| `repo_name` | string | REQUIRED | Library name |
| `repo_desc` | string | REQUIRED | Library description (MUST be "", NOT null) |
| `repo_category` | string | REQUIRED | Library category (MUST be "", NOT null) |
| `version` | integer | REQUIRED | Commit format version (MUST be 1) |
| `encrypted` | string | encrypted only | MUST be string "true" (NOT integer 1) |
| `enc_version` | integer | encrypted only | Encryption version (1 or 2) |
| `magic` | hex64 | encrypted only | Password verification hash |
| `key` | hex96 | encrypted only | Encrypted file key (same as random_key) |

**Critical Requirements:**

1. Field `encrypted` MUST be string `"true"` in commits (different from download-info integer!)
2. Fields `repo_desc` and `repo_category` MUST be empty string `""`, NOT null
3. Implementations MUST NOT include field `no_local_history` (verified absent in production)

**Response (Unencrypted Library):**

Omit fields: `encrypted`, `enc_version`, `magic`, `key`

---

### 5.5 FS ID List

**Endpoint:** `GET /seafhttp/repo/{repo_id}/fs-id-list/`

**Request:**
```http
GET /seafhttp/repo/256b7b88-d9cf-44d1-ba46-a5bb0bf0ebf7/fs-id-list/?server-head=0342dc93ceca5acc782059a13347ef249055ea12 HTTP/1.1
Host: example.com
Seafile-Repo-Token: 25979dd59d1b4ea95aa1c86028db4b0ad7498e61
```

**Query Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `server-head` | sha1 | REQUIRED | Commit ID to enumerate |

**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/json

["87199e6b76b84e56eef6b572bffdaa1067556489","534d4ba7a4a21939cf5bb4db7962d74e4f2b483a"]
```

**Schema:**
```
response: array of sha1
  Description: All FS object IDs in the commit tree
  Order: Unspecified (clients MUST NOT depend on order)
```

**Requirements:**

1. Response MUST be JSON array of strings
2. Response MUST NOT be newline-separated text
3. Array MUST include root_id from commit object
4. Array MUST include all directories and files recursively

---

### 5.6 Pack FS

**Endpoint:** `POST /seafhttp/repo/{repo_id}/pack-fs`

**Request:**
```http
POST /seafhttp/repo/256b7b88-d9cf-44d1-ba46-a5bb0bf0ebf7/pack-fs HTTP/1.1
Host: example.com
Seafile-Repo-Token: 25979dd59d1b4ea95aa1c86028db4b0ad7498e61
Content-Type: application/json

["87199e6b76b84e56eef6b572bffdaa1067556489","534d4ba7a4a21939cf5bb4db7962d74e4f2b483a"]
```

**Request Body Schema:**
```
array of sha1
  Description: FS object IDs to retrieve
```

**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/octet-stream

{binary pack-fs data}
```

#### 5.6.1 Pack-FS Binary Format

```abnf
pack-fs-data = *fs-entry

fs-entry = fs-id size compressed-json

fs-id = 40HEXDIG
  ; SHA-1 hash of decompressed JSON in lowercase hex

size = 4OCTET
  ; Unsigned 32-bit big-endian integer
  ; Value: length of compressed-json in bytes

compressed-json = *OCTET
  ; zlib-compressed UTF-8 encoded JSON
```

**Example:**
```
Offset 0-39:   "87199e6b76b84e56eef6b572bffdaa1067556489"  (40 bytes)
Offset 40-43:  0x00 0x00 0x00 0x5C                          (92 bytes)
Offset 44-135: <92 bytes of zlib compressed JSON>
Offset 136-175: "534d4ba7a4a21939cf5bb4db7962d74e4f2b483a" (next entry)
...
```

#### 5.6.2 FS Object JSON Schema

**Directory (type 3):**
```json
{
  "dirents": [
    {
      "id": "534d4ba7a4a21939cf5bb4db7962d74e4f2b483a",
      "mode": 33188,
      "modifier": "user@example.com",
      "mtime": 1768543179,
      "name": "file.txt",
      "size": 8
    }
  ],
  "type": 3,
  "version": 1
}
```

**Directory Schema:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `dirents` | array | REQUIRED | Directory entries |
| `type` | integer | REQUIRED | MUST be 3 for directories |
| `version` | integer | REQUIRED | MUST be 1 |

**Directory Entry Schema:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | sha1 | REQUIRED | FS object ID of entry |
| `mode` | integer | REQUIRED | Unix file mode (33188=file, 16384=dir) |
| `modifier` | string | REQUIRED | Last modifier email |
| `mtime` | timestamp | REQUIRED | Modification time (Unix seconds) |
| `name` | string | REQUIRED | Entry name (no path separators) |
| `size` | integer | REQUIRED | Size in bytes (0 for directories) |

**Critical Requirements:**

1. Directory entry fields MUST appear in alphabetical order: `id`, `mode`, `modifier`, `mtime`, `name`, `size`
2. Field `modifier` MUST be present (omission causes fs_id hash mismatch)
3. JSON MUST be serialized without spaces after separators: `{"type":3}` not `{"type": 3}`

**File (type 1):**
```json
{
  "block_ids": ["9bc34549d565d9505b287de0cd20ac77be1d3f2c"],
  "size": 8,
  "type": 1,
  "version": 1
}
```

**File Schema:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `block_ids` | array of sha1 | REQUIRED | Content block identifiers (in order) |
| `size` | integer | REQUIRED | Total file size in bytes (original, not encrypted) |
| `type` | integer | REQUIRED | MUST be 1 for files |
| `version` | integer | REQUIRED | MUST be 1 |

#### 5.6.3 FS ID Hash Computation

The `fs_id` is computed as:
```
fs_id = SHA-1(UTF-8(JSON.stringify(fs_object)))
```

Where:
1. JSON MUST be serialized with keys in alphabetical order
2. JSON MUST use compact format: no whitespace after `:` or `,`
3. JSON MUST be UTF-8 encoded
4. SHA-1 hash MUST be lowercase hexadecimal

**Test Vector:**

Given directory entry:
```json
{"id":"534d4ba7a4a21939cf5bb4db7962d74e4f2b483a","mode":33188,"modifier":"user@example.com","mtime":1768543179,"name":"file.txt","size":8}
```

Expected fs_id: (computed via SHA-1 of above JSON bytes)

---

### 5.7 Check FS

**Endpoint:** `POST /seafhttp/repo/{repo_id}/check-fs`

**Request:**
```http
POST /seafhttp/repo/256b7b88-d9cf-44d1-ba46-a5bb0bf0ebf7/check-fs HTTP/1.1
Host: example.com
Seafile-Repo-Token: 25979dd59d1b4ea95aa1c86028db4b0ad7498e61
Content-Type: application/json

["87199e6b76b84e56eef6b572bffdaa1067556489","534d4ba7a4a21939cf5bb4db7962d74e4f2b483a"]
```

**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/json

["missing_fs_id_1"]
```

**Schema:**
```
request: array of sha1
  Description: FS IDs to check

response: array of sha1
  Description: FS IDs that do NOT exist on server
  Empty array: All requested IDs exist
```

---

### 5.8 Check Blocks

**Endpoint:** `POST /seafhttp/repo/{repo_id}/check-blocks`

**Request:**
```http
POST /seafhttp/repo/256b7b88-d9cf-44d1-ba46-a5bb0bf0ebf7/check-blocks HTTP/1.1
Host: example.com
Seafile-Repo-Token: 25979dd59d1b4ea95aa1c86028db4b0ad7498e61
Content-Type: application/json

["9bc34549d565d9505b287de0cd20ac77be1d3f2c","a7f2b1c8e3d94f5a6b8c9d0e1f2a3b4c5d6e7f8a"]
```

**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/json

["a7f2b1c8e3d94f5a6b8c9d0e1f2a3b4c5d6e7f8a"]
```

**Schema:**
```
request: array of sha1
  Description: Block IDs to check

response: array of sha1
  Description: Block IDs that need to be uploaded
  Empty array: All blocks exist on server
```

---

### 5.9 Get Block

**Endpoint:** `GET /seafhttp/repo/{repo_id}/block/{block_id}`

**Request:**
```http
GET /seafhttp/repo/256b7b88-d9cf-44d1-ba46-a5bb0bf0ebf7/block/9bc34549d565d9505b287de0cd20ac77be1d3f2c HTTP/1.1
Host: example.com
Seafile-Repo-Token: 25979dd59d1b4ea95aa1c86028db4b0ad7498e61
```

**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/octet-stream

{binary block data}
```

**Block Format (Encrypted Library):**
```abnf
encrypted-block = iv encrypted-data

iv = 16OCTET
  ; Random initialization vector

encrypted-data = *OCTET
  ; AES-256-CBC encrypted content with PKCS#7 padding
```

**Block Format (Unencrypted Library):**
```abnf
unencrypted-block = *OCTET
  ; Raw file content (subset of file)
```

**Errors:**

| Code | Condition |
|------|-----------|
| 404 | Block not found |

---

### 5.10 Put Block

**Endpoint:** `PUT /seafhttp/repo/{repo_id}/block/{block_id}`

**Request:**
```http
PUT /seafhttp/repo/256b7b88-d9cf-44d1-ba46-a5bb0bf0ebf7/block/9bc34549d565d9505b287de0cd20ac77be1d3f2c HTTP/1.1
Host: example.com
Seafile-Repo-Token: 25979dd59d1b4ea95aa1c86028db4b0ad7498e61
Content-Type: application/octet-stream

{binary block data}
```

**Request Body:**

For encrypted libraries: encrypted-block (see 5.9)
For unencrypted libraries: raw content

**Response:**
```http
HTTP/1.1 200 OK
```

**Errors:**

| Code | Condition |
|------|-----------|
| 403 | Read-only library |
| 413 | Block too large |
| 507 | Insufficient storage |

---

### 5.11 Head Commits Multi

**Endpoint:** `POST /seafhttp/repo/head-commits-multi/`

**Purpose:** Batch check HEAD commit IDs for multiple repositories.

**Request:**
```http
POST /seafhttp/repo/head-commits-multi/ HTTP/1.1
Host: example.com
Authorization: Token {api_token}
Content-Type: application/json

["repo-id-1", "repo-id-2", "repo-id-3"]
```

**Request Schema:**
```
Type: JSON Array of UUID strings
Format: ["uuid1", "uuid2", ...]
Constraints:
  - Array MUST NOT be empty
  - Each UUID MUST be valid (8-4-4-4-12 hex format)
```

**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "repo-id-1": "commit-id-1",
  "repo-id-2": "commit-id-2"
}
```

**Response Schema:**
```
Type: JSON Object
Keys: repo_id (string, UUID format)
Values: head_commit_id (string, 40-char hex)

Notes:
  - Repos not found are omitted from response
  - Repos without HEAD commit are omitted
  - Empty object {} if no repos found
```

**Verified:** 2026-01-18 against production Seafile (app.nihaoconsult.com)

**Purpose:** Desktop client uses this endpoint to efficiently check if local HEAD matches remote HEAD for multiple libraries before initiating sync. If HEAD matches, no sync needed.

---

### 5.12 Permission Check

**Endpoint:** `GET /seafhttp/repo/{repo_id}/permission-check/`

**Purpose:** Verify user permissions and record client peer info for audit/tracking.

**Request:**
```http
GET /seafhttp/repo/eafc83e1-e62c-464a-8a87-94f2ec8d4fde/permission-check/?op=download&client_id=2e2d673229247a4eb20a60b9d053f1a7f36bfbee&client_name=MyMacBook.local HTTP/1.1
Host: example.com
Seafile-Repo-Token: {sync_token}
```

**Query Parameters:**
```
op: string (REQUIRED)
  Values: "download" | "upload"
  Description: Operation type being permission-checked

client_id: string (OPTIONAL)
  Format: 40-character hex string (SHA-1)
  Description: Unique client device identifier

client_name: string (OPTIONAL)
  Description: Human-readable client name (hostname)

client_ver: string (OPTIONAL)
  Description: Client version string
```

**Response:**
```http
HTTP/1.1 200 OK
Content-Length: 0
```

**Response Schema:**
```
Status: 200 OK
Body: Empty (Content-Length: 0)

Success: Empty 200 response
Errors:
  - 400 Bad Request: Invalid op parameter
  - 403 Forbidden: Permission denied
  - 404 Not Found: Repository not found
  - 500 Internal Server Error: Server error
```

**Verified:** 2026-01-18 against production Seafile (app.nihaoconsult.com)

**Purpose:**
1. Validate user has permission for requested operation
2. Record client peer info (IP, client_id, client_name, version) for audit
3. Track download/upload operations for analytics
4. Enforce repo access controls (corrupted, deleted repos return errors)

**Implementation Notes:**
- MUST validate `op` parameter (only "download" or "upload" allowed)
- MUST validate `client_id` is exactly 40 characters if provided
- SHOULD record client peer info in audit log
- SHOULD update client peer info if already exists
- Response body MUST be empty (not null, not JSON)

---

## 6. Encryption

### 6.1 Encryption Version 2 (REQUIRED)

Implementations MUST support encryption version 2. Support for version 1 is OPTIONAL.

### 6.2 Password Derivation (PBKDF2-SHA256)

#### 6.2.1 Magic Derivation

Magic is a password verification hash computed as:

```
Input: repo_id (UUID string) + password (UTF-8 string)
Salt: {0xda, 0x90, 0x45, 0xc3, 0x06, 0xc7, 0xcc, 0x26}
Iterations: 1000

key = PBKDF2-HMAC-SHA256(input, salt, iterations, 32)
iv = PBKDF2-HMAC-SHA256(key, salt, 10, 16)
magic = lowercase_hex(key) + lowercase_hex(iv)
```

**Output:**
- Length: 64 hexadecimal characters (32 bytes key + 16 bytes IV)
- Encoding: Lowercase hex string

**Test Vector:**
```
repo_id: "256b7b88-d9cf-44d1-ba46-a5bb0bf0ebf7"
password: "test123"
salt: {0xda, 0x90, 0x45, 0xc3, 0x06, 0xc7, 0xcc, 0x26}

Expected magic: [implementation to compute]
```

#### 6.2.2 Random Key Derivation

Random key is an encrypted symmetric key:

```
Input: password (UTF-8 string only, NOT repo_id + password)
Salt: {0xda, 0x90, 0x45, 0xc3, 0x06, 0xc7, 0xcc, 0x26}
Iterations: 1000

encKey = PBKDF2-HMAC-SHA256(password, salt, 1000, 32)
encIV = PBKDF2-HMAC-SHA256(encKey, salt, 10, 16)

secretKey = random 48 bytes
randomKey = AES-256-CBC-Encrypt(secretKey, encKey, encIV)
randomKey_hex = lowercase_hex(randomKey)
```

**Output:**
- Length: 96 hexadecimal characters (48 bytes encrypted)
- Encoding: Lowercase hex string

**Critical:**
- Input for random_key is password ONLY (different from magic!)
- Secret key is random 48 bytes (32 for file key + 16 for file IV)

### 6.3 File Key Derivation

```
randomKey_bytes = hex_decode(random_key)
secretKey = AES-256-CBC-Decrypt(randomKey_bytes, encKey, encIV)

fileKey = secretKey[0:32]   # First 32 bytes
fileIV = secretKey[32:48]   # Last 16 bytes
```

### 6.4 Block Encryption

Encrypted blocks MUST use AES-256-CBC with PKCS#7 padding:

```abnf
encrypted-block = random-iv encrypted-content

random-iv = 16OCTET
  ; Random initialization vector (MUST be unique per block)

encrypted-content = AES-256-CBC(content, fileKey, random-iv) with PKCS#7
  ; Padding: PKCS#7 to 16-byte boundary
```

**Decryption:**
```
iv = encrypted_block[0:16]
ciphertext = encrypted_block[16:]
plaintext = AES-256-CBC-Decrypt(ciphertext, fileKey, iv)
content = PKCS7-Unpad(plaintext)
```

---

## 7. Library Creation

### 7.1 Create Encrypted Library

**Endpoint:** `POST /api2/repos/`

**Request:**
```http
POST /api2/repos/ HTTP/1.1
Host: example.com
Authorization: Token {api_token}
Content-Type: application/x-www-form-urlencoded

name=My+Library&passwd=test123
```

**Request Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | REQUIRED | Library display name |
| `desc` | string | OPTIONAL | Library description |
| `passwd` | string | OPTIONAL | Password (presence creates encrypted library) |

**Critical Requirement:**

Implementations MUST detect presence of `passwd` parameter to create encrypted library. Clients do NOT send `encrypted=true` parameter.

**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "repo_id": "256b7b88-d9cf-44d1-ba46-a5bb0bf0ebf7",
  "repo_name": "My Library",
  "encrypted": 1,
  "enc_version": 2,
  "magic": "eee7b4a7a2539f2e4fb1c88a40121077152a5dc2c223f607f8b5e0838affde61",
  "random_key": "406ec194a0d0f7985b831b040034d829a4c68fbd354982f58101d0b6edd0232efed903cd1c404d0a66892473be968f19"
}
```

**Response Schema:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `repo_id` | uuid | REQUIRED | Generated library ID |
| `repo_name` | string | REQUIRED | Library name |
| `encrypted` | integer | REQUIRED | MUST be integer 1 for encrypted |
| `enc_version` | integer | encrypted | MUST be 2 |
| `magic` | hex64 | encrypted | Computed magic (see 6.2.1) |
| `random_key` | hex96 | encrypted | Generated random_key (see 6.2.2) |

---

## 8. Error Responses

### 8.1 Error Format

Errors SHOULD use JSON format:
```json
{
  "error": "ERROR_CODE",
  "error_msg": "Human-readable message"
}
```

### 8.2 HTTP Status Codes

| Code | Usage |
|------|-------|
| 200 | Success |
| 400 | Bad request (invalid parameters, malformed JSON) |
| 401 | Unauthorized (missing/invalid auth token) |
| 403 | Forbidden (insufficient permissions, library locked) |
| 404 | Not found (library, commit, block not found) |
| 413 | Payload too large |
| 500 | Internal server error |
| 507 | Insufficient storage |

### 8.3 Common Error Scenarios

| Error | HTTP | error | error_msg |
|-------|------|-------|-----------|
| Invalid token | 401 | `AUTH_FAILED` | "Invalid token" |
| Library locked | 403 | `LIBRARY_ENCRYPTED` | "Library is encrypted" |
| Library not found | 404 | `LIBRARY_NOT_FOUND` | "Library not found" |
| Block not found | 404 | `BLOCK_NOT_FOUND` | "Block not found" |
| Read-only library | 403 | `PERMISSION_DENIED` | "Read-only library" |

---

## 9. Security Considerations

### 9.1 Encryption Strength

**Warning:** PBKDF2 with 1000 iterations is weak by modern standards (2026). Implementations SHOULD internally use stronger key derivation (e.g., Argon2id) but MUST maintain PBKDF2 compatibility for client authentication.

### 9.2 Block ID Collision

SHA-1 is deprecated for cryptographic use due to collision attacks. However, the protocol requires SHA-1 for block_id and fs_id. Implementations MUST use SHA-1 for compatibility but MAY internally use SHA-256 for storage with a mapping layer.

### 9.3 Transport Security

Implementations MUST support TLS 1.2 or later. Implementations SHOULD reject connections using TLS 1.0 or 1.1.

### 9.4 Authentication Token Storage

Sync tokens SHOULD have limited lifetime (e.g., 1 hour). Implementations SHOULD NOT store sync tokens in persistent storage.

---

## 10. Conformance Requirements

### 10.1 Server Conformance

A conforming Seafile sync server implementation MUST:

1. Implement all endpoints in sections 5.1-5.10
2. Return exact JSON types specified (integer vs string vs boolean)
3. Support encryption version 2 (section 6)
4. Use `Seafile-Repo-Token` header for `/seafhttp/` authentication
5. Return fs-id-list as JSON array (not newline-separated)
6. Compress FS objects with zlib in pack-fs responses
7. Include `modifier` field in directory entries
8. Use empty string `""` (not null) for `repo_desc` and `repo_category`
9. Omit `no_local_history` field from commit objects
10. Pass protocol comparison tests: `./run-sync-comparison.sh`

### 10.2 Client Conformance

A conforming Seafile sync client implementation MUST:

1. Follow sync sequence in section 4.2
2. Use `Seafile-Repo-Token` header (not `Authorization`) for `/seafhttp/`
3. Accept both JSON types for `encrypted` field (integer in download-info, string in commits)
4. Parse fs-id-list as JSON array
5. Decompress FS objects from pack-fs using zlib
6. Verify fs_id matches SHA-1 of decompressed JSON
7. Support encryption version 2

---

## 11. Test Vectors

### 11.1 PBKDF2 Test Vector

```
Input (magic):
  repo_id: "00000000-0000-0000-0000-000000000000"
  password: "password"
  salt: {0xda, 0x90, 0x45, 0xc3, 0x06, 0xc7, 0xcc, 0x26}
  iterations: 1000

Expected output:
  key (32 bytes): 7b936d1d48fdb41cfaf7706a170dc7f4acd62770a0bba34942f28db59d067ad3
  iv (16 bytes): 1311b21e41502122e215be585bef0295
  magic (64 hex chars): 7b936d1d48fdb41cfaf7706a170dc7f4acd62770a0bba34942f28db59d067ad31311b21e41502122e215be585bef0295
```

### 11.2 FS ID Test Vectors

**Test Vector 1: Empty Directory**

```
Input JSON:
{"dirents":[],"type":3,"version":1}

Expected fs_id: 34d733541351367f503319f3966888325f787d84
```

**Test Vector 2: Directory with File**

```
Input JSON:
{"dirents":[{"id":"534d4ba7a4a21939cf5bb4db7962d74e4f2b483a","mode":33188,"modifier":"user@example.com","mtime":1768543179,"name":"test.txt","size":100}],"type":3,"version":1}

Expected fs_id: bc7200ad1106816404aa90e1680411b383670cb0
```

**Test Vector 3: File Object**

```
Input JSON:
{"block_ids":["9bc34549d565d9505b287de0cd20ac77be1d3f2c"],"size":100,"type":1,"version":1}

Expected fs_id: 04be6ce556b15cdf52d523eda1a7434c807db1c2
```

**Note:** JSON is serialized with no spaces (`separators=(',',':')`), sorted keys, UTF-8 encoded before SHA-1 hashing.

---

## 12. References

### 12.1 Normative References

- **RFC 2119**: Key words for use in RFCs to Indicate Requirement Levels
- **RFC 7230**: HTTP/1.1 Message Syntax and Routing
- **RFC 8259**: The JavaScript Object Notation (JSON) Data Interchange Format

### 12.2 Informative References

- Production Seafile: https://app.nihaoconsult.com
- Seafile Source: https://github.com/haiwen/seafile
- Testing Framework: `docker/seafile-cli-debug/`

---

## Appendix A: Change Log

### Version 2 (2026-01-16)
- Initial RFC specification based on production server verification
- Verified against app.nihaoconsult.com
- Removed `no_local_history` field (absent in production)
- Confirmed JSON array format for fs-id-list
- Confirmed `Seafile-Repo-Token` header requirement

---

## Appendix B: Testing

Implementations SHOULD verify conformance using:

```bash
cd docker/seafile-cli-debug

# API-level protocol comparison
./run-sync-comparison.sh

# Real desktop client sync test
./run-real-client-sync.sh
```

Both tests MUST pass for full conformance.
