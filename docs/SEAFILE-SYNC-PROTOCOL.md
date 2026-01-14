# Seafile Desktop Client Sync Protocol

**Version**: 2 (as of Seafile 11.x)
**Status**: Reverse-Engineered from Production Server
**Date**: 2026-01-14

This document describes the complete Seafile desktop/mobile client synchronization protocol as observed from captured network traffic. This specification enables third-party implementations of a Seafile-compatible sync server.

## Table of Contents

### Core Protocol
1. [Overview](#overview)
2. [Database Schema](#database-schema) *(table mapping and additional tables)*
3. [Authentication](#authentication)
4. [Server Information](#server-information)
5. [Library Management](#library-management)
6. [File Operations](#file-operations)
7. [Sync Protocol Flow](#sync-protocol-flow)
8. [Commit Objects](#commit-objects)
9. [FS Objects](#fs-objects)
10. [Block Operations](#block-operations)
11. [Upload Protocol](#upload-protocol)
12. [Download Protocol](#download-protocol)
13. [Encrypted Libraries](#encrypted-libraries)

### GUI Client Features
14. [Virtual Repos (Selective Folder Sync)](#virtual-repos-selective-folder-sync)
15. [Thumbnails](#thumbnails)
16. [File Activities](#file-activities)
17. [File Search](#file-search)
18. [Folder Sharing](#folder-sharing)
19. [Client Session Management](#client-session-management)

### Reference
20. [Binary Format Specifications](#binary-format-specifications)
21. [Error Handling](#error-handling)
22. [CLI Client Reference](#cli-client-reference)
23. [File Exclusion Patterns](#file-exclusion-patterns)
24. [Implementation Checklist](#implementation-checklist)
25. [Complete Workflow Examples](#complete-workflow-examples)
26. [Conflict Resolution](#conflict-resolution)
27. [Security Specifications](#security-specifications)
28. [Testing Guidance](#testing-guidance)
29. [Error Catalog](#error-catalog)

---

## Overview

The Seafile sync protocol uses two types of authentication:
1. **API Token** (`Authorization: Token xxx`) - Used for REST API calls (`/api2/`, `/api/v2.1/`)
2. **Sync Token** (`Seafile-Repo-Token: xxx`) - Used for sync operations (`/seafhttp/`)

### Protocol Version

```
GET /seafhttp/protocol-version
```

**Response:**
```json
{"version": 2}
```

The current protocol version is 2. Clients should verify compatibility before proceeding.

### Sync Flow Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          SEAFILE SYNC PROTOCOL                               │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  1. AUTHENTICATION                                                           │
│     POST /api2/auth-token/ → API Token                                       │
│     GET /api2/repos/{id}/download-info/ → Sync Token                         │
│                                                                              │
│  2. DISCOVER CHANGES                                                         │
│     GET /seafhttp/repo/{id}/commit/HEAD → head_commit_id                     │
│     Compare with local head → if different, sync needed                      │
│                                                                              │
│  3. DOWNLOAD (Server → Client)                                               │
│     GET /commit/{id} → root_id, parent_id                                    │
│     GET /fs-id-list/ → [all fs_ids in commit]                                │
│     POST /check-fs → [missing fs_ids]                                        │
│     POST /pack-fs → binary stream of FS objects                              │
│     Parse FS objects → find file block_ids                                   │
│     POST /check-blocks → [missing block_ids]                                 │
│     GET /block/{id} → raw block content                                      │
│     Assemble blocks → reconstruct files                                      │
│                                                                              │
│  4. UPLOAD (Client → Server)                                                 │
│     Chunk files → blocks (Rabin CDC, 256KB-4MB)                              │
│     POST /check-blocks → [blocks server needs]                               │
│     PUT /block/{id} → upload each missing block                              │
│     Create FS objects for files/directories                                  │
│     POST /check-fs → [fs objects server needs]                               │
│     POST /recv-fs → upload FS objects                                        │
│     Create commit object                                                     │
│     PUT /commit/{id} → upload commit                                         │
│     POST /update-branch → advance HEAD pointer                               │
│                                                                              │
│  5. CONFLICT RESOLUTION                                                      │
│     If HEAD changed during upload → merge required                           │
│     Client downloads new changes, merges locally, re-uploads                 │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Database Schema

> **Authoritative Schema**: See [ARCHITECTURE.md](ARCHITECTURE.md) for the complete Cassandra database schema with ER diagrams, partition key design, and implementation details.

This section maps sync protocol concepts to the existing SesameFS database tables and identifies additional tables needed for full GUI/CLI client support.

### Existing Tables (from ARCHITECTURE.md)

The sync protocol uses these tables that already exist in SesameFS:

| Protocol Concept | Cassandra Table | Key Fields |
|------------------|-----------------|------------|
| Multi-tenancy | `organizations` | `org_id`, `name`, `storage_quota` |
| User accounts | `users` | `(org_id, user_id)`, `email`, `name` |
| User lookup | `users_by_email` | `email` → `user_id`, `org_id` |
| OIDC auth | `users_by_oidc` | `(oidc_issuer, oidc_sub)` → `user_id` |
| Libraries/repos | `libraries` | `(org_id, library_id)`, encryption fields, `head_commit_id` |
| Version history | `commits` | `(library_id, commit_id)`, `root_fs_id`, `parent_id` |
| Directory tree | `fs_objects` | `(library_id, fs_id)`, `obj_type`, `dir_entries`, `block_ids` |
| Block storage | `blocks` | `(org_id, block_id)`, `storage_class`, `storage_key`, `ref_count` |
| SHA-1→SHA-256 | `block_id_mappings` | `(org_id, external_id)` → `internal_id` |
| Share links | `share_links` | `share_token`, `library_id`, `file_path`, `permission` |
| Library shares | `shares` | `(library_id, share_id)`, `shared_to`, `permission` |
| User favorites | `starred_files` | `(user_id, repo_id, path)` |
| File locking | `locked_files` | `(repo_id, path)`, `locked_by` |
| Auth tokens | `access_tokens` | `token`, `token_type`, `user_id`, `repo_id` |

### Additional Tables for Full Sync Support

These tables are **not yet implemented** but are required for complete GUI/CLI client compatibility.

> **Note**: For 2FA/SSO database tables, see [SEAFILE-SYNC-AUTH.md](SEAFILE-SYNC-AUTH.md).

```cql
-- Virtual Repos (selective folder sync for desktop GUI)
-- Allows syncing a subfolder as its own "library"
CREATE TABLE sesamefs.virtual_repos (
    org_id UUID,
    virtual_repo_id UUID,
    origin_repo_id UUID,                -- Parent library
    origin_path TEXT,                   -- Path within parent (e.g., "/Documents/Project")
    name TEXT,
    owner_id UUID,
    created_at TIMESTAMP,
    PRIMARY KEY ((org_id), virtual_repo_id)
);
-- Secondary index for lookups by parent
CREATE INDEX ON sesamefs.virtual_repos (origin_repo_id);

-- File Activities (audit log for GUI activity feed)
CREATE TABLE sesamefs.activities (
    org_id UUID,
    user_id UUID,
    activity_id TIMEUUID,               -- Time-based for ordering
    library_id UUID,
    commit_id TEXT,
    op_type TEXT,                       -- 'create', 'update', 'delete', 'rename', 'move'
    path TEXT,
    old_path TEXT,                      -- For rename/move operations
    file_size BIGINT,
    PRIMARY KEY ((org_id, user_id), activity_id)
) WITH CLUSTERING ORDER BY (activity_id DESC);

-- Groups (for group-based sharing)
CREATE TABLE sesamefs.groups (
    org_id UUID,
    group_id UUID,
    name TEXT,
    owner_id UUID,
    created_at TIMESTAMP,
    PRIMARY KEY ((org_id), group_id)
);

-- Group Members
CREATE TABLE sesamefs.group_members (
    group_id UUID,
    user_id UUID,
    is_admin BOOLEAN,
    joined_at TIMESTAMP,
    PRIMARY KEY ((group_id), user_id)
);

-- Trash (soft delete with auto-purge)
CREATE TABLE sesamefs.trash (
    org_id UUID,
    library_id UUID,
    trash_id TIMEUUID,
    path TEXT,
    fs_id TEXT,
    deleted_by UUID,
    expires_at TIMESTAMP,               -- Auto-purge after this time
    PRIMARY KEY ((org_id, library_id), trash_id)
) WITH CLUSTERING ORDER BY (trash_id DESC);
```

### Token Types in `access_tokens`

The existing `access_tokens` table handles multiple token types:

| `token_type` | Purpose | Fields Used |
|--------------|---------|-------------|
| `api` | REST API authentication | `user_id`, `org_id` |
| `sync` | Seafile sync protocol (`/seafhttp/`) | `user_id`, `repo_id`, `file_path` |
| `upload` | Upload link tokens | `repo_id`, `file_path` |
| `download` | Download link tokens | `repo_id`, `file_path` |

### Encryption Fields in `libraries`

The `libraries` table already supports encrypted libraries:

| Field | Description |
|-------|-------------|
| `encrypted` | Boolean flag |
| `enc_version` | Encryption version (2 = AES-256-CBC) |
| `magic` | Password verification hash (PBKDF2 or Argon2id) |
| `random_key` | Encrypted file key |
| `salt` | For key derivation (version 2 only) |

See [ENCRYPTION.md](ENCRYPTION.md) for detailed encryption implementation.

---

## Authentication

> **Advanced Auth**: For SSO, Two-Factor Authentication, and API token management, see [SEAFILE-SYNC-AUTH.md](SEAFILE-SYNC-AUTH.md).

### POST /api2/auth-token/

Obtain an API authentication token.

**Request:**
```
POST /api2/auth-token/
Content-Type: application/x-www-form-urlencoded

username=user%40example.com&password=secret
```

**Important:** URL-encode special characters in username and password.

**Response (200 OK):**
```json
{"token": "113219421eef29cebe842dd8801ec1243eeb460e"}
```

**Response (400 Bad Request):**
```json
{"non_field_errors": ["Unable to login with provided credentials."]}
```

The token is a 40-character hex string. Use it in subsequent requests as:
```
Authorization: Token 113219421eef29cebe842dd8801ec1243eeb460e
```

---

## Server Information

### GET /api2/server-info/

Get server version and capabilities.

**Request:**
```
GET /api2/server-info/
Authorization: Token {api_token}
```

**Response:**
```json
{
  "version": "11.0.16",
  "encrypted_library_version": 2,
  "desktop-custom-brand": "Seafile Server",
  "features": [
    "seafile-basic",
    "seafile-pro",
    "file-search"
  ]
}
```

| Field | Description |
|-------|-------------|
| `version` | Server version string |
| `encrypted_library_version` | Maximum supported encryption version (1 or 2) |
| `desktop-custom-brand` | Custom branding for desktop client |
| `features` | List of enabled features |

### GET /api2/account/info/

Get current user account information.

**Response:**
```json
{
  "email": "user@example.com",
  "name": "User Name",
  "usage": 56,
  "total": -2,
  "is_staff": false,
  "avatar_url": "https://server/media/avatars/default.png"
}
```

Note: `total: -2` indicates unlimited quota.

---

## Library Management

### Create Plain Library

**POST /api2/repos/**

Create a new unencrypted library.

**Request:**
```
POST /api2/repos/
Authorization: Token {api_token}
Content-Type: application/x-www-form-urlencoded

name=My+New+Library&desc=Optional+description
```

**Response (200 OK):**
```json
{
  "relay_id": "44e8f253849ad910dc142247227c8ece8ec0f971",
  "relay_addr": "127.0.0.1",
  "relay_port": "80",
  "email": "user@example.com",
  "token": "fbdca90d49812f49f8d11918f392c59eb285b958",
  "repo_id": "a9093631-e08b-453f-9b48-2c7fa305d910",
  "repo_name": "My New Library",
  "repo_desc": "Optional description",
  "repo_size": 0,
  "mtime": 1768362468,
  "encrypted": "",
  "enc_version": 0,
  "salt": "",
  "magic": "",
  "random_key": "",
  "repo_version": 1,
  "head_commit_id": "2ad45059e5d92976cc00092aee64b92fccc0b582",
  "permission": "rw"
}
```

**Note:** The response is the same format as `download-info`, including a sync token.

### Create Encrypted Library

**POST /api2/repos/**

Create a new encrypted library by adding the `passwd` field.

**Request:**
```
POST /api2/repos/
Authorization: Token {api_token}
Content-Type: application/x-www-form-urlencoded

name=Secret+Library&desc=Encrypted+library&passwd=MySecretPassword
```

**Response (200 OK):**
```json
{
  "repo_id": "af53e8f5-c8d6-4882-bcf2-1bf42b7148d0",
  "repo_name": "Secret Library",
  "token": "defe425ce7e42786422ccb71a4ee4136ac7a9792",
  "encrypted": 1,
  "enc_version": 2,
  "salt": "",
  "magic": "e50f0baf1edbf198da89587310162178359a6e02ee4411b62764f60656686679",
  "random_key": "1b42dec5e218c16913978cc655f37dea73a596bbfc94d262a3666dab73f8484a78fdfaa86d22e4d099165a60fbaf62ee",
  "head_commit_id": "ce7594eafe87cd8657467f121287a28eca68c725",
  ...
}
```

**Important Fields for Encrypted Libraries:**

| Field | Description |
|-------|-------------|
| `encrypted` | 1 = encrypted library |
| `enc_version` | 2 = AES-256-CBC encryption |
| `magic` | 64 hex chars - PBKDF2 hash for password verification |
| `random_key` | 96 hex chars - Encrypted file encryption key |

### Rename Library

**POST /api2/repos/{repo_id}/?op=rename**

**Request:**
```
POST /api2/repos/{repo_id}/?op=rename
Authorization: Token {api_token}
Content-Type: application/x-www-form-urlencoded

repo_name=New+Library+Name
```

**Response:**
```
"success"
```

### Delete Library

**DELETE /api2/repos/{repo_id}/**

**Request:**
```
DELETE /api2/repos/{repo_id}/
Authorization: Token {api_token}
```

**Response:**
```
"success"
```

### List Libraries

#### GET /api2/repos/

List all accessible libraries (old format).

**Response:**
```json
[
  {
    "type": "repo",
    "id": "aa89cce8-f2f1-44ad-98f0-1ea87ca6a3ed",
    "name": "My Library",
    "owner": "user@example.com",
    "owner_name": "User Name",
    "permission": "rw",
    "encrypted": false,
    "size": 233016217,
    "mtime": 1767686979,
    "head_commit_id": "268281145ad162113359a04cefe1bf806fb8a5c9",
    "version": 1
  }
]
```

### GET /api/v2.1/repos/

List all accessible libraries (new format, recommended).

**Response:**
```json
{
  "repos": [
    {
      "type": "mine",
      "repo_id": "aa89cce8-f2f1-44ad-98f0-1ea87ca6a3ed",
      "repo_name": "My Library",
      "owner_email": "user@example.com",
      "permission": "rw",
      "encrypted": false,
      "size": 233016217,
      "last_modified": "2026-01-06T08:09:39+00:00",
      "status": "normal"
    }
  ]
}
```

| Type | Description |
|------|-------------|
| `mine` | Libraries owned by the user |
| `shared` | Libraries shared to the user |
| `group` | Group libraries |
| `public` | Organization-wide libraries |

---

## File Operations

### Upload Files

#### Step 1: Get Upload URL

**GET /api2/repos/{repo_id}/upload-link/**

```
GET /api2/repos/{repo_id}/upload-link/?p=/
Authorization: Token {api_token}
```

**Response:**
```json
"https://server/seafhttp/upload-api/04a1d0f5-58e1-4448-ba49-9bf261472504"
```

The `p` parameter specifies the destination directory (default: `/` for root).

#### Step 2: Upload File (Multipart)

**POST {upload_url}**

```
POST /seafhttp/upload-api/04a1d0f5-58e1-4448-ba49-9bf261472504
Content-Type: multipart/form-data; boundary=----WebKitFormBoundary...

------WebKitFormBoundary...
Content-Disposition: form-data; name="parent_dir"

/
------WebKitFormBoundary...
Content-Disposition: form-data; name="relative_path"


------WebKitFormBoundary...
Content-Disposition: form-data; name="file"; filename="test.txt"
Content-Type: text/plain

[file content]
------WebKitFormBoundary...--
```

**Response (200 OK):**
```json
[
  {
    "id": "b42a2769de0e5066cc20b91bf18e1eac0de53b15",
    "name": "test.txt",
    "size": 141
  }
]
```

**Important Form Fields:**

| Field | Required | Description |
|-------|----------|-------------|
| `parent_dir` | Yes | Destination directory path |
| `relative_path` | No | Subdirectory within parent_dir |
| `file` | Yes | File content (can be multiple) |
| `replace` | No | Set to `1` to replace existing file |

### Update Existing Files

Use `update-link` instead of `upload-link`:

**GET /api2/repos/{repo_id}/update-link/**

```
GET /api2/repos/{repo_id}/update-link/
Authorization: Token {api_token}
```

The upload process is the same as above, but uses the update URL.

### Download Files

#### Step 1: Get Download URL

**GET /api2/repos/{repo_id}/file/**

```
GET /api2/repos/{repo_id}/file/?p=/path/to/file.txt
Authorization: Token {api_token}
```

**Response:**
```json
"https://server/seafhttp/files/2e8c30e4-41fe-4f8b-9e8c-3a8c30e441fe/file.txt"
```

#### Step 2: Download File Content

**GET {download_url}**

```
GET /seafhttp/files/2e8c30e4-41fe-4f8b-9e8c-3a8c30e441fe/file.txt
```

**Response:** Raw file content.

For encrypted libraries, the server returns the encrypted content. The client must decrypt it locally using the file key.

### Delete Files

**DELETE /api2/repos/{repo_id}/file/**

```
DELETE /api2/repos/{repo_id}/file/?p=/path/to/file.txt
Authorization: Token {api_token}
```

**Response (200 OK):**
```json
"success"
```

### Create Directory

**POST /api2/repos/{repo_id}/dir/**

```
POST /api2/repos/{repo_id}/dir/?p=/new-folder
Authorization: Token {api_token}
Content-Type: application/x-www-form-urlencoded

operation=mkdir
```

**Response (201 Created):**
```json
"success"
```

### Rename File/Directory

**POST /api2/repos/{repo_id}/file/**

```
POST /api2/repos/{repo_id}/file/?p=/old-name.txt
Authorization: Token {api_token}
Content-Type: application/x-www-form-urlencoded

operation=rename&newname=new-name.txt
```

**Response:**
```json
"success"
```

### Move File/Directory

**POST /api2/repos/{repo_id}/file/**

```
POST /api2/repos/{repo_id}/file/?p=/source/file.txt
Authorization: Token {api_token}
Content-Type: application/x-www-form-urlencoded

operation=move&dst_repo={dest_repo_id}&dst_dir=/destination/
```

### Copy File/Directory

**POST /api2/repos/{repo_id}/file/**

```
POST /api2/repos/{repo_id}/file/?p=/source/file.txt
Authorization: Token {api_token}
Content-Type: application/x-www-form-urlencoded

operation=copy&dst_repo={dest_repo_id}&dst_dir=/destination/
```

### List Directory Contents

**GET /api2/repos/{repo_id}/dir/**

```
GET /api2/repos/{repo_id}/dir/?p=/
Authorization: Token {api_token}
```

**Response:**
```json
[
  {
    "id": "0000000000000000000000000000000000000000",
    "type": "dir",
    "name": "Documents",
    "mtime": 1768362500
  },
  {
    "id": "b42a2769de0e5066cc20b91bf18e1eac0de53b15",
    "type": "file",
    "name": "test.txt",
    "size": 141,
    "mtime": 1768362468,
    "modifier_email": "user@example.com"
  }
]
```

---

## Sync Protocol Flow

### Complete Sync Sequence

```
1. GET /api2/repos/{repo_id}/download-info/     → Get sync token + metadata
2. GET /seafhttp/repo/{repo_id}/commit/HEAD     → Get current HEAD commit ID
3. GET /seafhttp/repo/{repo_id}/commit/{id}     → Get full commit object
4. GET /seafhttp/repo/{repo_id}/fs-id-list/     → List all FS object IDs
5. POST /seafhttp/repo/{repo_id}/check-fs       → Check which FS objects exist locally
6. POST /seafhttp/repo/{repo_id}/pack-fs/       → Download missing FS objects
7. POST /seafhttp/repo/{repo_id}/check-blocks   → Check which blocks exist locally
8. GET /seafhttp/repo/{repo_id}/block/{id}      → Download missing blocks
```

### GET /api2/repos/{repo_id}/download-info/

**CRITICAL**: This endpoint returns the sync token needed for all `/seafhttp/` operations.

**Request:**
```
GET /api2/repos/{repo_id}/download-info/
Authorization: Token {api_token}
```

**Response (Unencrypted Library):**
```json
{
  "relay_id": "44e8f253849ad910dc142247227c8ece8ec0f971",
  "relay_addr": "127.0.0.1",
  "relay_port": "80",
  "email": "user@example.com",
  "token": "efd0d5332806f712bd5095e52d814ad254105654",
  "repo_id": "aa89cce8-f2f1-44ad-98f0-1ea87ca6a3ed",
  "repo_name": "My Library",
  "repo_size": 233016217,
  "encrypted": "",
  "enc_version": 0,
  "salt": "",
  "magic": "",
  "random_key": "",
  "repo_version": 1,
  "head_commit_id": "268281145ad162113359a04cefe1bf806fb8a5c9",
  "permission": "rw"
}
```

**Response (Encrypted Library):**
```json
{
  "token": "25979dd59d1b4ea95aa1c86028db4b0ad7498e61",
  "repo_id": "256b7b88-d9cf-44d1-ba46-a5bb0bf0ebf7",
  "encrypted": 1,
  "enc_version": 2,
  "salt": "",
  "magic": "eee7b4a7a2539f2e4fb1c88a40121077152a5dc2c223f607f8b5e0838affde61",
  "random_key": "406ec194a0d0f7985b831b040034d829a4c68fbd354982f58101d0b6edd0232efed903cd1c404d0a66892473be968f19",
  ...
}
```

| Field | Description |
|-------|-------------|
| `token` | **Sync token** - Use as `Seafile-Repo-Token` header |
| `encrypted` | 0 or "" for unencrypted, 1 for encrypted |
| `enc_version` | Encryption version (0, 1, or 2) |
| `salt` | Salt for key derivation (empty for enc_version 2) |
| `magic` | Password verification hash (64 hex chars) |
| `random_key` | Encrypted file key (96 hex chars) |

---

## Commit Objects

### GET /seafhttp/repo/{repo_id}/commit/HEAD

Get the HEAD commit reference.

**Request:**
```
GET /seafhttp/repo/{repo_id}/commit/HEAD
Seafile-Repo-Token: {sync_token}
```

**Response:**
```json
{
  "is_corrupted": 0,
  "head_commit_id": "268281145ad162113359a04cefe1bf806fb8a5c9"
}
```

**Important:** This does NOT return the full commit object, only a reference.

### GET /seafhttp/repo/{repo_id}/commit/{commit_id}

Get a specific commit object.

**Request:**
```
GET /seafhttp/repo/{repo_id}/commit/{commit_id}
Seafile-Repo-Token: {sync_token}
```

**Response (Unencrypted):**
```json
{
  "commit_id": "268281145ad162113359a04cefe1bf806fb8a5c9",
  "root_id": "4702e8382675a4a062cb49cc5dc175c093f7effe",
  "repo_id": "aa89cce8-f2f1-44ad-98f0-1ea87ca6a3ed",
  "creator_name": "user@example.com",
  "creator": "0000000000000000000000000000000000000000",
  "description": "Added directory \"xxx\"",
  "ctime": 1767686979,
  "parent_id": "af5122051d71617ff7a2536a60067e90d725da62",
  "second_parent_id": null,
  "repo_name": "My Library",
  "repo_desc": "",
  "repo_category": null,
  "version": 1
}
```

**Response (Encrypted):**
```json
{
  "commit_id": "0342dc93ceca5acc782059a13347ef249055ea12",
  "root_id": "87199e6b76b84e56eef6b572bffdaa1067556489",
  "repo_id": "256b7b88-d9cf-44d1-ba46-a5bb0bf0ebf7",
  "creator": "0000000000000000000000000000000000000000",
  "description": "Added \"file.txt\".",
  "ctime": 1768225934,
  "parent_id": "8125065ad7abe077c1b1dcfbb4f491eb41987415",
  "second_parent_id": null,
  "encrypted": "true",
  "enc_version": 2,
  "magic": "eee7b4a7a2539f2e4fb1c88a40121077152a5dc2c223f607f8b5e0838affde61",
  "key": "406ec194a0d0f7985b831b040034d829a4c68fbd354982f58101d0b6edd0232efed903cd1c404d0a66892473be968f19",
  "version": 1
}
```

| Field | Description |
|-------|-------------|
| `commit_id` | SHA-1 hash of the commit (40 hex chars) |
| `root_id` | FS ID of the root directory |
| `parent_id` | Previous commit ID (null for first commit) |
| `second_parent_id` | For merge commits |
| `creator` | Always 40 zeros for client commits |
| `ctime` | Unix timestamp of commit creation |
| `description` | Human-readable change description |
| `version` | Always 1 |
| `encrypted` | "true" for encrypted libraries |
| `enc_version` | 1 or 2 |
| `magic` | Password verification hash |
| `key` | Encrypted file key (same as random_key) |

---

## FS Objects

FS Objects represent the file system structure. There are two types:
- **Directory (SeafDir)** - type: 3
- **File (Seafile)** - type: 1

### GET /seafhttp/repo/{repo_id}/fs-id-list/

Get list of all FS object IDs in the repository.

**Request:**
```
GET /seafhttp/repo/{repo_id}/fs-id-list/?server-head={commit_id}
Seafile-Repo-Token: {sync_token}
```

**Response:**
```json
[
  "4702e8382675a4a062cb49cc5dc175c093f7effe",
  "95957597b0480add4f18d2c5e5a57905cd645f54"
]
```

Returns a flat JSON array of all FS IDs (both directories and files) that need to be synced.

### POST /seafhttp/repo/{repo_id}/check-fs

Check which FS objects the client already has.

**Request:**
```
POST /seafhttp/repo/{repo_id}/check-fs
Seafile-Repo-Token: {sync_token}
Content-Type: application/json

["4702e8382675a4a062cb49cc5dc175c093f7effe", "95957597b0480add4f18d2c5e5a57905cd645f54"]
```

**Response:**
```json
[]
```

Returns JSON array of FS IDs that **do NOT exist** on the server. Empty array (`[]`) means all requested IDs exist on server. The client should upload any returned IDs via `recv-fs`.

### POST /seafhttp/repo/{repo_id}/pack-fs/

Download multiple FS objects in a single request.

**Request:**
```
POST /seafhttp/repo/{repo_id}/pack-fs/
Seafile-Repo-Token: {sync_token}
Content-Type: application/json

["4702e8382675a4a062cb49cc5dc175c093f7effe"]
```

**Response:** Binary data (see [Pack-FS Binary Format](#pack-fs-binary-format))

### Directory Object (SeafDir)

```json
{
  "type": 3,
  "version": 1,
  "dirents": [
    {
      "id": "0000000000000000000000000000000000000000",
      "mode": 16384,
      "mtime": 1767686976,
      "name": "subdirectory"
    },
    {
      "id": "95957597b0480add4f18d2c5e5a57905cd645f54",
      "mode": 33188,
      "modifier": "user@example.com",
      "mtime": 1751512773,
      "name": "file.zip",
      "size": 233016217
    }
  ]
}
```

| Field | Description |
|-------|-------------|
| `type` | Always 3 for directories |
| `version` | Always 1 |
| `dirents` | Array of directory entries |

**Dirent Fields:**

| Field | Description |
|-------|-------------|
| `id` | FS ID of child (40 hex chars, 40 zeros for empty directory) |
| `mode` | Unix file mode (16384 = directory, 33188 = regular file) |
| `modifier` | Last modifier email (only for files, omitted if empty) |
| `mtime` | Unix timestamp |
| `name` | File/directory name |
| `size` | File size (only for files, omitted for directories) |

**CRITICAL: JSON Key Order**

Dirent fields MUST be serialized in **alphabetical order** (`id`, `mode`, `modifier`, `mtime`, `name`, `size`). This is essential because the `fs_id` is computed as SHA-1 of the JSON content. Different key ordering produces different hashes, causing sync failures.

### File Object (Seafile)

```json
{
  "type": 1,
  "version": 1,
  "size": 233016217,
  "block_ids": [
    "b871434458b83f7ecee7d47069caa5c516c8143d",
    "9b9a81eaa9253100a6d2e0ed5cc6f5f5feb26008",
    "e178f4ee8454ccbdce7857a04267e09167d75a93"
  ]
}
```

| Field | Description |
|-------|-------------|
| `type` | Always 1 for files |
| `version` | Always 1 |
| `size` | Total file size in bytes |
| `block_ids` | Ordered array of block SHA-1 IDs |

---

## Block Operations

### POST /seafhttp/repo/{repo_id}/check-blocks

Check which blocks the client already has.

**Request:**
```
POST /seafhttp/repo/{repo_id}/check-blocks
Seafile-Repo-Token: {sync_token}
Content-Type: application/json

["b871434458b83f7ecee7d47069caa5c516c8143d", "0000000000000000000000000000000000000000"]
```

**Response:**
```json
["0000000000000000000000000000000000000000"]
```

Returns array of block IDs that do NOT exist on the server (client needs to send these).

### GET /seafhttp/repo/{repo_id}/block/{block_id}

Download a specific block.

**Request:**
```
GET /seafhttp/repo/{repo_id}/block/{block_id}
Seafile-Repo-Token: {sync_token}
```

**Response:** Raw binary block data.

For encrypted libraries, the block is encrypted with the file key using AES-256-CBC. The client decrypts it locally.

### GET /seafhttp/repo/{repo_id}/permission-check/

Check read/write permissions.

**Request:**
```
GET /seafhttp/repo/{repo_id}/permission-check/?op=download
Seafile-Repo-Token: {sync_token}
```

or

```
GET /seafhttp/repo/{repo_id}/permission-check/?op=upload
Seafile-Repo-Token: {sync_token}
```

**Response:** Empty body with HTTP 200 on success, HTTP 403 on failure.

---

## Upload Protocol (Sync Client)

The sync client uploads files by:
1. Splitting files into blocks using Rabin CDC (256KB-4MB chunks)
2. Computing SHA-1 hash of each block
3. Checking which blocks exist on server via `check-blocks`
4. Uploading only missing blocks
5. Creating FS objects (Seafile objects) referencing the block IDs
6. Creating a commit with the new root directory

### Block Upload

**POST /seafhttp/repo/{repo_id}/recv-fs/HEAD**

Upload block data to server.

**Request:**
```
POST /seafhttp/repo/{repo_id}/recv-fs/HEAD
Seafile-Repo-Token: {sync_token}
Content-Type: application/octet-stream

[raw block data]
```

### Commit Upload

After uploading all blocks and FS objects, client commits changes:

**POST /seafhttp/repo/{repo_id}/commit/HEAD/?head={current_head}**

---

## Download Protocol

The download protocol supports two modes:
1. **Web API Download** - Single file via download URL (see File Operations)
2. **Sync Protocol Download** - Block-by-block via `/block/{id}` endpoint

### Sync Client Download Flow

```
1. Get HEAD commit
2. Get commit object (contains root_id)
3. Get fs-id-list for current commit
4. Download FS objects via pack-fs
5. Parse FS objects to find file's block_ids
6. Download each block via /block/{id}
7. Concatenate blocks to reconstruct file
8. For encrypted: decrypt each block with file key
```

### Content Verification

**Block ID is SHA-1 of Content:**

For unencrypted libraries:
```python
import hashlib
block_content = download_block(block_id)
computed_id = hashlib.sha1(block_content).hexdigest()
assert computed_id == block_id, "Block integrity check failed"
```

For encrypted libraries (Seafile v2 - uses derived IV):
```python
# Block ID is SHA-1 of PLAINTEXT (original) content
# Server stores encrypted content (ciphertext only, no IV)
encrypted_block = download_block(block_id)
# Decrypt using derived IV (same for all blocks in library)
decrypted_block = AES_256_CBC_Decrypt(encrypted_block, file_key, derived_iv)
# Remove PKCS7 padding
decrypted_block = remove_pkcs7_padding(decrypted_block)
computed_id = hashlib.sha1(decrypted_block).hexdigest()
assert computed_id == block_id, "Block integrity check failed"
```

### File Reconstruction

```python
def reconstruct_file(fs_object, repo_id, sync_token, file_key=None, file_iv=None):
    """
    Reconstruct a file from its FS object.

    Args:
        fs_object: The Seafile object (type: 1) containing block_ids
        file_key: 32-byte AES key for encrypted libraries (None for plain)
        file_iv: 16-byte derived IV for encrypted libraries (Seafile v2)
    """
    file_content = b''

    for block_id in fs_object['block_ids']:
        # Download block
        block_data = download_block(repo_id, block_id, sync_token)

        if file_key:
            # Encrypted library: decrypt block using DERIVED IV
            # Seafile v2: ciphertext only, no prepended IV
            cipher = AES.new(file_key, AES.MODE_CBC, file_iv)
            decrypted = cipher.decrypt(block_data)
            block_data = remove_pkcs7_padding(decrypted)

        file_content += block_data

    # Verify total size matches fs_object['size']
    assert len(file_content) == fs_object['size']

    return file_content
```

---

## Encrypted Libraries

### Encryption Versions

| Version | Key Derivation | Block Encryption | Salt |
|---------|---------------|-----------------|------|
| 1 | PBKDF2-HMAC-SHA256 (1000 iter) | AES-128-CBC | Random per library |
| 2 | PBKDF2-HMAC-SHA256 (1000 iter) | AES-256-CBC | Static 8-byte salt |

### Encryption Parameters

For enc_version 2, use this static salt:
```
0xda, 0x90, 0x45, 0xc3, 0x06, 0xc7, 0xcc, 0x26
```

### Key Derivation (enc_version 2)

```python
# Password verification (magic)
input = repo_id + password
key = PBKDF2(input, salt, 1000, 32, SHA256)
iv = PBKDF2(key, salt, 10, 16, SHA256)
magic = hex(key)  # Compare with server's magic

# File key derivation (for decrypting random_key)
input = password  # Just password, NOT repo_id + password
enc_key = PBKDF2(input, salt, 1000, 32, SHA256)
enc_iv = PBKDF2(enc_key, salt, 10, 16, SHA256)
file_key = AES_256_CBC_Decrypt(hex_decode(random_key), enc_key, enc_iv)
```

**CRITICAL:** Magic uses `repo_id + password` but random_key decryption uses just `password`.

### Block Encryption

**IMPORTANT: Seafile v2 uses DERIVED IV, not random IV per block.**

For enc_version 2, blocks are encrypted with a derived IV (same IV for all blocks in the library):
```
[AES-256-CBC encrypted content with PKCS7 padding]
```

The IV is derived from the file key using a second PBKDF2 call:
```python
file_key = decrypt_random_key(password)  # 32-byte secret key
derived_iv = PBKDF2(file_key, static_salt, 10, 16, SHA256)  # 16-byte IV

# Each block encrypted with same key and IV
ciphertext = AES_256_CBC_Encrypt(plaintext, file_key, derived_iv)
```

**Note:** This differs from standard AES-CBC which uses random IV per block. Seafile v2's
approach reuses the IV across all blocks in a library. This is less secure but required
for compatibility with Seafile clients.

### Password Verification

**POST /api/v2.1/repos/{repo_id}/set-password/**

**Request:**
```
POST /api/v2.1/repos/{repo_id}/set-password/
Authorization: Token {api_token}
Content-Type: application/json

{"password": "user_password"}
```

**Response (Success):**
```json
{"success": true}
```

**Response (Wrong Password):**
```json
{"error_msg": "Wrong password"}
```

---

## Virtual Repos (Selective Folder Sync)

Virtual repos (also called sub-libraries) enable selective folder sync in the GUI client. When a user right-clicks a folder in the cloud file browser and selects "Sync this folder", a virtual repo is created.

### Concept

A **virtual repo** is a view into a subfolder of a parent library:
- Shares the same underlying data (fs objects, blocks) as the parent
- Has its own commit history
- Can be synced independently
- Changes sync back to the parent library

### GET /api2/repos/{repo_id}/dir/sub_repo/

Get or create a virtual repo for a subfolder.

**Request:**
```
GET /api2/repos/{repo_id}/dir/sub_repo/?p=/Documents/Project&name=Project
Authorization: Token {api_token}
```

| Parameter | Required | Description |
|-----------|----------|-------------|
| `p` | Yes | Path to the folder within the parent library |
| `name` | Yes | Name for the virtual repo (usually folder name) |

**Response (200 OK):**
```json
{
  "id": "a3f5c8d2-1234-5678-9abc-def012345678",
  "name": "Project",
  "origin_repo_id": "parent-library-uuid",
  "origin_path": "/Documents/Project"
}
```

**Response Fields:**

| Field | Description |
|-------|-------------|
| `id` | UUID of the virtual repo (use this for sync operations) |
| `name` | Display name of the virtual repo |
| `origin_repo_id` | UUID of the parent library |
| `origin_path` | Path within parent library |

### Virtual Repo Sync Flow

Once a virtual repo is created, the client syncs it like a regular library:

```
1. GET /api2/repos/{virtual_repo_id}/download-info/
2. Use returned sync token for /seafhttp/ operations
3. Sync proceeds normally using the virtual repo ID
4. Changes are reflected in the parent library at origin_path
```

**Database Schema:** See `virtual_repos` table in [Database Schema](#database-schema) section.

### List Virtual Repos

**GET /api2/virtual-repos/**

List all virtual repos for the current user.

**Response:**
```json
[
  {
    "id": "virtual-repo-uuid",
    "name": "Project",
    "origin_repo_id": "parent-repo-uuid",
    "origin_repo_name": "My Documents",
    "origin_path": "/Documents/Project"
  }
]
```

---

## Thumbnails

The GUI client displays thumbnails for images in the cloud file browser.

### GET /api2/repos/{repo_id}/thumbnail/

Generate a thumbnail for an image file.

**Request:**
```
GET /api2/repos/{repo_id}/thumbnail/?p=/photos/image.jpg&size=128
Authorization: Token {api_token}
```

| Parameter | Required | Description |
|-----------|----------|-------------|
| `p` | Yes | Path to the image file |
| `size` | No | Thumbnail size in pixels (default: 48) |

**Supported Sizes:** 48, 128, 256, 480

**Response:** JPEG image data with `Content-Type: image/jpeg`

**Supported Formats:** JPEG, PNG, GIF, WEBP, BMP, TIFF

### Implementation Notes

1. Generate thumbnails on-demand
2. Cache generated thumbnails (key: `{repo_id}:{path}:{size}`)
3. For encrypted libraries, decrypt file first, then generate thumbnail
4. Return 400 for unsupported file types
5. Return 404 if file doesn't exist

### Thumbnail URL in File Listings

When listing directories, include thumbnail URLs for images:

```json
{
  "name": "photo.jpg",
  "type": "file",
  "size": 1234567,
  "mtime": 1234567890,
  "encoded_thumbnail_src": "/api2/repos/{repo_id}/thumbnail/?p=/photo.jpg&size=48"
}
```

---

## File Activities

The activities endpoint returns recent file changes across all libraries.

### GET /api2/events/

Get recent file activity events.

**Request:**
```
GET /api2/events/?start=0&limit=25
Authorization: Token {api_token}
```

| Parameter | Description |
|-----------|-------------|
| `start` | Offset for pagination (default: 0) |
| `limit` | Number of events to return (default: 25, max: 100) |

**Response:**
```json
{
  "events": [
    {
      "repo_id": "library-uuid",
      "repo_name": "My Library",
      "commit_id": "abc123...",
      "path": "/documents/report.docx",
      "name": "report.docx",
      "op_type": "edit",
      "op_user": "user@example.com",
      "time": 1234567890
    },
    {
      "repo_id": "library-uuid",
      "repo_name": "My Library",
      "path": "/images/",
      "name": "photo.jpg",
      "op_type": "create",
      "op_user": "user@example.com",
      "time": 1234567800
    }
  ],
  "more": true
}
```

**Operation Types:**

| op_type | Description |
|---------|-------------|
| `create` | File/folder created |
| `delete` | File/folder deleted |
| `edit` | File modified |
| `rename` | File/folder renamed |
| `move` | File/folder moved |
| `recover` | File recovered from trash |

### GET /api/v2.1/activities/

Alternative v2.1 endpoint with similar functionality.

**Request:**
```
GET /api/v2.1/activities/?page=1&per_page=25
Authorization: Token {api_token}
```

---

## File Search

Search for files across libraries.

### GET /api2/search/

Search for files by name or content.

**Request:**
```
GET /api2/search/?q=report&search_repo=all
Authorization: Token {api_token}
```

| Parameter | Description |
|-----------|-------------|
| `q` | Search query (required) |
| `search_repo` | `all` or specific repo_id |
| `search_path` | Path prefix to search within |
| `search_ftypes` | File types: `all`, `document`, `image`, `video`, `audio` |
| `page` | Page number (default: 1) |
| `per_page` | Results per page (default: 10) |
| `obj_type` | `file` or `dir` (default: both) |

**Response:**
```json
{
  "total": 42,
  "results": [
    {
      "repo_id": "library-uuid",
      "repo_name": "My Library",
      "name": "report-2024.docx",
      "fullpath": "/documents/reports/report-2024.docx",
      "size": 45678,
      "mtime": 1234567890,
      "is_dir": false,
      "content_highlight": "...quarterly <b>report</b> shows..."
    }
  ],
  "has_more": true
}
```

### Search Implementation Notes

1. **Basic search:** Match filename patterns (case-insensitive)
2. **Full-text search:** Optional, requires search index (Elasticsearch)
3. **Encrypted libraries:** Only search filenames, not content
4. **Permission filtering:** Only return files user has access to

---

## Folder Sharing

Share folders within a library to users or groups.

### GET /api2/repos/{repo_id}/dir/shared_items/

List sharing settings for a folder.

**Request:**
```
GET /api2/repos/{repo_id}/dir/shared_items/?p=/shared-folder&share_type=user
Authorization: Token {api_token}
```

| Parameter | Description |
|-----------|-------------|
| `p` | Folder path |
| `share_type` | `user` or `group` |

**Response (user shares):**
```json
[
  {
    "user_email": "colleague@example.com",
    "user_name": "John Doe",
    "permission": "rw"
  }
]
```

**Response (group shares):**
```json
[
  {
    "group_id": 123,
    "group_name": "Engineering",
    "permission": "r"
  }
]
```

### PUT /api2/repos/{repo_id}/dir/shared_items/

Share a folder with users or groups.

**Request (share to user):**
```
PUT /api2/repos/{repo_id}/dir/shared_items/?p=/shared-folder
Authorization: Token {api_token}
Content-Type: application/x-www-form-urlencoded

share_type=user&username=colleague@example.com&permission=rw
```

**Request (share to group):**
```
PUT /api2/repos/{repo_id}/dir/shared_items/?p=/shared-folder
Authorization: Token {api_token}
Content-Type: application/x-www-form-urlencoded

share_type=group&group_id=123&permission=r
```

**Permissions:**
- `r` - Read only
- `rw` - Read and write

**Response (200 OK):**
```json
{"success": true}
```

### POST /api2/repos/{repo_id}/dir/shared_items/

Update sharing permission.

**Request:**
```
POST /api2/repos/{repo_id}/dir/shared_items/?p=/shared-folder
Authorization: Token {api_token}
Content-Type: application/x-www-form-urlencoded

share_type=user&username=colleague@example.com&permission=r
```

### DELETE /api2/repos/{repo_id}/dir/shared_items/

Remove sharing.

**Request:**
```
DELETE /api2/repos/{repo_id}/dir/shared_items/?p=/shared-folder&share_type=user&username=colleague@example.com
Authorization: Token {api_token}
```

---

## Client Session Management

Endpoints for managing client sessions, web login, and device tracking.

### GET /api2/client-login/

Generate a one-time token for web browser login from desktop client.

**Request:**
```
GET /api2/client-login/
Authorization: Token {api_token}
```

**Response:**
```json
{
  "token": "one-time-login-token"
}
```

The client opens: `https://server/client-login/?token={token}`

### POST /api2/logout-device/

Log out the current device and invalidate the token.

**Request:**
```
POST /api2/logout-device/
Authorization: Token {api_token}
```

**Response:**
```json
{"success": true}
```

### GET /api2/unseen_messages/

Get count of unseen notifications.

**Request:**
```
GET /api2/unseen_messages/
Authorization: Token {api_token}
```

**Response:**
```json
{
  "unseen_count": 5
}
```

### GET /api2/default-repo/

Get the user's default library.

**Request:**
```
GET /api2/default-repo/
Authorization: Token {api_token}
```

**Response:**
```json
{
  "repo_id": "default-library-uuid",
  "exists": true
}
```

If no default repo exists, `exists` is `false`.

### POST /api2/default-repo/

Create a default library for the user.

**Request:**
```
POST /api2/default-repo/
Authorization: Token {api_token}
```

**Response:**
```json
{
  "repo_id": "new-default-library-uuid"
}
```

### GET /api2/repo-tokens/

Get sync tokens for multiple repositories at once (for batch sync initialization).

**Request:**
```
GET /api2/repo-tokens/?repos=uuid1,uuid2,uuid3
Authorization: Token {api_token}
```

**Response:**
```json
{
  "uuid1": "sync-token-1",
  "uuid2": "sync-token-2",
  "uuid3": "sync-token-3"
}
```

### GET /api2/repo_history_changes/

Get detailed changes in a commit.

**Request:**
```
GET /api2/repo_history_changes/?repo_id={repo_id}&commit_id={commit_id}
Authorization: Token {api_token}
```

**Response:**
```json
{
  "added": [
    {"path": "/new-file.txt", "size": 1234}
  ],
  "deleted": [
    {"path": "/removed-file.txt"}
  ],
  "modified": [
    {"path": "/changed-file.txt", "size": 5678}
  ],
  "renamed": [
    {"old_path": "/old-name.txt", "new_path": "/new-name.txt"}
  ]
}
```

---

## Binary Format Specifications

### Pack-FS Binary Format

The `/pack-fs/` endpoint returns a binary stream containing multiple FS objects:

```
┌──────────────────────────────────────────────────────────────────────┐
│  Entry 1                                                              │
├───────────────────┬──────────────┬───────────────────────────────────┤
│ FS ID (40 bytes)  │ Size (4 BE)  │ Zlib Compressed JSON              │
│ ASCII hex string  │ Big-endian   │ (size bytes)                      │
├───────────────────┴──────────────┴───────────────────────────────────┤
│  Entry 2                                                              │
├───────────────────┬──────────────┬───────────────────────────────────┤
│ FS ID (40 bytes)  │ Size (4 BE)  │ Zlib Compressed JSON              │
└───────────────────┴──────────────┴───────────────────────────────────┘
```

| Field | Size | Description |
|-------|------|-------------|
| FS ID | 40 bytes | ASCII hex string (SHA-1 of uncompressed JSON) |
| Size | 4 bytes | Big-endian uint32, size of compressed data |
| Data | {Size} bytes | Zlib-compressed JSON (deflate with header) |

### Parsing Example (Python)

```python
import zlib
import json

def parse_pack_fs(data):
    entries = []
    offset = 0

    while offset + 44 <= len(data):
        fs_id = data[offset:offset+40].decode('ascii')
        size = int.from_bytes(data[offset+40:offset+44], 'big')

        compressed = data[offset+44:offset+44+size]
        decompressed = zlib.decompress(compressed)
        obj = json.loads(decompressed.decode('utf-8'))

        entries.append({
            'fs_id': fs_id,
            'object': obj
        })

        offset += 44 + size

    return entries
```

### Generating Pack-FS (Server)

```python
import zlib
import json

def create_pack_fs(fs_objects):
    """
    fs_objects: list of (fs_id, object_dict) tuples
    """
    output = b''

    for fs_id, obj in fs_objects:
        json_data = json.dumps(obj, separators=(',', ':')).encode('utf-8')
        compressed = zlib.compress(json_data)

        output += fs_id.encode('ascii')  # 40 bytes
        output += len(compressed).to_bytes(4, 'big')  # 4 bytes
        output += compressed

    return output
```

### FS ID Computation

The FS ID is the SHA-1 hash of the JSON content with:
- Keys alphabetically sorted
- Minimal whitespace (no spaces after `:` or `,`)

```python
import hashlib
import json

def compute_fs_id(obj):
    # Ensure consistent key ordering
    json_str = json.dumps(obj, sort_keys=True, separators=(',', ':'))
    return hashlib.sha1(json_str.encode('utf-8')).hexdigest()
```

---

## Error Handling

### HTTP Status Codes

| Code | Meaning |
|------|---------|
| 200 | Success |
| 400 | Bad request (missing/invalid parameters) |
| 401 | Unauthorized (invalid token) |
| 403 | Forbidden (no permission) |
| 404 | Not found (library/commit/block doesn't exist) |
| 500 | Server error |

### Common Error Responses

```json
{"detail": "Invalid token header. No credentials provided."}
```

```json
{"error_msg": "Wrong password"}
```

```
Invalid server-head parameter.
```

---

## CLI Client Reference

The `seaf-cli` command-line tool provides all sync functionality for headless servers.

### Available Commands

| Command | Description |
|---------|-------------|
| `init` | Initialize config directory |
| `start` | Start seafile daemon |
| `stop` | Stop seafile daemon |
| `list` | List local libraries |
| `list-remote` | List remote libraries |
| `status` | Show syncing status |
| `download` | Download a library by ID |
| `download-by-name` | Download a library by name |
| `sync` | Sync a library with existing folder |
| `desync` | Stop syncing a library |
| `create` | Create a new library |
| `config` | Configure client settings |

### Authentication

```bash
seaf-cli download -l <library-id> -s <server-url> -d <directory> \
    -u <username> -p <password>
```

> **Advanced Auth**: For SSO, 2FA, and API token options, see [SEAFILE-SYNC-AUTH.md](SEAFILE-SYNC-AUTH.md).

### Encrypted Library Operations

**Download Encrypted Library:**
```bash
seaf-cli download -l <library-id> -s <server-url> -d <directory> \
    -u <username> -p <password> -e <library-password>
```

**Sync Encrypted Library:**
```bash
seaf-cli sync -l <library-id> -s <server-url> -d <existing-folder> \
    -u <username> -p <password> -e <library-password>
```

**Create Encrypted Library:**
```bash
seaf-cli create -n "Library Name" -s <server-url> \
    -u <username> -p <password> -e <library-password>
```

### Configuration Options

```bash
# View a config value
seaf-cli config -k <key>

# Set a config value
seaf-cli config -k <key> -v <value>
```

| Key | Description | Example Value |
|-----|-------------|---------------|
| `disable_verify_certificate` | Skip SSL certificate verification | `true` |
| `upload_limit` | Upload speed limit (bytes/sec) | `1000000` |
| `download_limit` | Download speed limit (bytes/sec) | `1000000` |

### Status States

| State | Description |
|-------|-------------|
| `synchronized` | Library is fully synced |
| `committing` | Client is creating a commit |
| `initializing` | Initial sync in progress |
| `downloading` | Downloading changes from server |
| `uploading` | Uploading changes to server |
| `error` | Sync error occurred |

### Command Details

**seaf-cli download:**
```
-l, --library     Library ID (required)
-L, --libraryname Library name (for download-by-name)
-s, --server      Server URL (required)
-d, --dir         Parent directory for library
-u, --username    Username/email (required)
-p, --password    Account password
-T, --token       API token (alternative to password)
-a, --tfa         Two-factor authentication code
-e, --libpasswd   Library encryption password
-c, --confdir     Config directory (default: ~/.ccnet)
```

**seaf-cli sync:**
```
-l, --library     Library ID (required)
-s, --server      Server URL (required)
-d, --folder      Existing local folder to sync (required)
-u, --username    Username/email (required)
-p, --password    Account password
-T, --token       API token
-a, --tfa         Two-factor authentication code
-e, --libpasswd   Library encryption password
```

**seaf-cli create:**
```
-n, --name        Library name (required)
-t, --desc        Library description
-e, --libpasswd   Library encryption password (makes library encrypted)
-s, --server      Server URL (required)
-u, --username    Username/email (required)
-p, --password    Account password
-T, --token       API token
-a, --tfa         Two-factor authentication code
```

### Limitations

**Subfolder Sync:** The CLI client does NOT support syncing individual subfolders within a library. This feature is only available in the GUI client. The `sync` and `download` commands operate at the full library level.

**Workaround:** Previously, users could obtain a sublibrary ID from the GUI and use it with seaf-cli, but this workaround no longer works reliably in recent versions.

---

## File Exclusion Patterns

### seafile-ignore.txt

Create a `seafile-ignore.txt` file in the root of a library to exclude files/folders from syncing.

**Location:** `<library-root>/seafile-ignore.txt`

### Pattern Syntax

```
# Comments start with #

# Exclude specific file
secret.txt

# Exclude directory (note trailing slash)
temp/

# Exclude by extension (recursive)
*.log

# Exclude pattern in specific directory
build/*.o

# Exclude all .git directories
.git/

# Exclude node_modules anywhere
node_modules/

# Wildcard patterns
*.tmp
*~
.DS_Store
Thumbs.db
```

### Pattern Rules

| Pattern | Matches |
|---------|---------|
| `foo` | Only file or symlink named "foo" |
| `foo/` | Only directory named "foo" (and contents) |
| `foo*` | Files/directories starting with "foo" |
| `*.ext` | All files with extension ".ext" |
| `dir/*.ext` | Files with ".ext" in "dir" and subdirectories |

### Important Notes

1. **Client-side only:** Files can still be created via the web interface and will sync TO the client, but local changes to excluded files won't sync back.

2. **No server-side enforcement:** The server doesn't enforce these patterns - they only affect the sync client.

3. **Applied at sync time:** Changes to `seafile-ignore.txt` take effect on the next sync operation.

### Default Exclusions

The client automatically excludes:
- `seafile-ignore.txt` itself
- Files starting with `~$` (Office temp files)
- Files ending with `.seaf` or `.~`
- System files: `.DS_Store`, `Thumbs.db`, `desktop.ini`

---

## Client State Machine

The Seafile desktop client uses this state machine for sync:

```
synchronized → committing → [uploading|downloading] → synchronized
     ↓                              ↓
   error ←──────────────────────────┘
```

### Client Log Messages

| Message | Cause |
|---------|-------|
| `Failed to inflate` | pack-fs data not zlib compressed |
| `Failed to decompress dir object` | Same as above |
| `Failed to find dir X in repo Y` | fs_id not in fs-id-list response |
| `Failed to read dir` | FS object corrupted locally |
| `Error when indexing` | Missing FS objects |

---

## Implementation Checklist

### Core Sync Protocol (Required for Desktop Client)

| Endpoint | Method | Purpose | Status |
|----------|--------|---------|--------|
| `/seafhttp/protocol-version` | GET | Return `{"version": 2}` | |
| `/api2/auth-token/` | POST | Username/password authentication | |
| `/api2/server-info/` | GET | Server version and capabilities | |
| `/api2/account/info/` | GET | Current user info | |
| `/api2/repos/` | GET | List libraries | |
| `/api2/repos/` | POST | Create library | |
| `/api2/repos/{id}/` | DELETE | Delete library | |
| `/api2/repos/{id}/?op=rename` | POST | Rename library | |
| `/api2/repos/{id}/download-info/` | GET | Get sync token + encryption info | |
| `/seafhttp/repo/{id}/commit/HEAD` | GET | Get HEAD commit reference | |
| `/seafhttp/repo/{id}/commit/{id}` | GET | Get full commit object | |
| `/seafhttp/repo/{id}/commit/{id}` | PUT | Create new commit | |
| `/seafhttp/repo/{id}/fs-id-list/` | GET | List all FS object IDs | |
| `/seafhttp/repo/{id}/check-fs` | POST | Check which FS objects exist | |
| `/seafhttp/repo/{id}/pack-fs/` | POST | Download FS objects (binary format) | |
| `/seafhttp/repo/{id}/recv-fs` | POST | Upload FS objects | |
| `/seafhttp/repo/{id}/check-blocks` | POST | Check which blocks exist | |
| `/seafhttp/repo/{id}/block/{id}` | GET | Download block | |
| `/seafhttp/repo/{id}/block/{id}` | PUT | Upload block | |
| `/seafhttp/repo/{id}/permission-check/` | GET | Check read/write permission | |
| `/seafhttp/repo/{id}/quota-check/` | GET | Check storage quota | |
| `/seafhttp/repo/{id}/update-branch` | POST | Update branch pointer | |

### File Operations (Required for Web/API Uploads)

| Endpoint | Method | Purpose | Status |
|----------|--------|---------|--------|
| `/api2/repos/{id}/upload-link/` | GET | Get upload URL | |
| `/api2/repos/{id}/update-link/` | GET | Get update URL | |
| `/seafhttp/upload-api/{token}` | POST | Multipart file upload | |
| `/api2/repos/{id}/file/` | GET | Get download URL | |
| `/seafhttp/files/{token}/{path}` | GET | Download file content | |
| `/api2/repos/{id}/file/` | DELETE | Delete file | |
| `/api2/repos/{id}/file/?operation=rename` | POST | Rename file | |
| `/api2/repos/{id}/file/?operation=move` | POST | Move file | |
| `/api2/repos/{id}/file/?operation=copy` | POST | Copy file | |
| `/api2/repos/{id}/dir/` | GET | List directory | |
| `/api2/repos/{id}/dir/?operation=mkdir` | POST | Create directory | |

### Encrypted Library Support

| Endpoint | Method | Purpose | Status |
|----------|--------|---------|--------|
| `/api/v2.1/repos/{id}/set-password/` | POST | Unlock encrypted library | |
| `/api/v2.1/repos/{id}/set-password/` | PUT | Change library password | |
| `download-info` response | - | Include `magic`, `random_key`, `enc_version` | |
| `commit` response | - | Include `encrypted`, `magic`, `key` fields | |
| Block encryption | - | AES-256-CBC with IV prefix | |

### Advanced Authentication (Optional)

> See [SEAFILE-SYNC-AUTH.md](SEAFILE-SYNC-AUTH.md) for SSO, 2FA, and API token implementations.

### GUI Client Features (Required for Desktop GUI)

#### Virtual Repos / Selective Folder Sync

| Endpoint | Method | Purpose | Status |
|----------|--------|---------|--------|
| `/api2/repos/{id}/dir/sub_repo/` | GET | Create/get virtual repo for folder sync | |
| `/api2/virtual-repos/` | GET | List user's virtual repos | |

#### Thumbnails

| Endpoint | Method | Purpose | Status |
|----------|--------|---------|--------|
| `/api2/repos/{id}/thumbnail/` | GET | Generate image thumbnail | |

#### File Activities

| Endpoint | Method | Purpose | Status |
|----------|--------|---------|--------|
| `/api2/events/` | GET | Get recent file changes | |
| `/api/v2.1/activities/` | GET | Get activities (v2.1 format) | |

#### File Search

| Endpoint | Method | Purpose | Status |
|----------|--------|---------|--------|
| `/api2/search/` | GET | Search files by name/content | |

#### Folder Sharing

| Endpoint | Method | Purpose | Status |
|----------|--------|---------|--------|
| `/api2/repos/{id}/dir/shared_items/` | GET | List folder shares | |
| `/api2/repos/{id}/dir/shared_items/` | PUT | Create folder share | |
| `/api2/repos/{id}/dir/shared_items/` | POST | Update share permission | |
| `/api2/repos/{id}/dir/shared_items/` | DELETE | Remove folder share | |

#### Client Session Management

| Endpoint | Method | Purpose | Status |
|----------|--------|---------|--------|
| `/api2/client-login/` | GET | Generate web login token | |
| `/api2/logout-device/` | POST | Logout and invalidate token | |
| `/api2/unseen_messages/` | GET | Get notification count | |
| `/api2/default-repo/` | GET | Get user's default library | |
| `/api2/default-repo/` | POST | Create default library | |
| `/api2/repo-tokens/` | GET | Batch get sync tokens | |
| `/api2/repo_history_changes/` | GET | Get commit change details | |

#### Groups (Required for Folder Sharing)

| Endpoint | Method | Purpose | Status |
|----------|--------|---------|--------|
| `/api2/groups/` | GET | List user's groups | |
| `/api2/groupandcontacts/` | GET | List groups and contacts | |
| `/api2/search-user/` | GET | Search users for sharing | |

#### User Avatars

| Endpoint | Method | Purpose | Status |
|----------|--------|---------|--------|
| `/api2/avatars/user/{email}/resized/{size}/` | GET | Get user avatar image | |

### Response Format Requirements

**`/api2/auth-token/` Response:**
```json
{"token": "40-char-hex-string"}
```

**`/api2/repos/` List Response:**
```json
[
  {
    "type": "repo",
    "id": "uuid",
    "name": "Library Name",
    "owner": "user@example.com",
    "permission": "rw",
    "encrypted": false,
    "size": 12345,
    "mtime": 1234567890,
    "head_commit_id": "40-char-hex"
  }
]
```

**`/api2/repos/` Create Response (with sync token):**
```json
{
  "repo_id": "uuid",
  "repo_name": "Library Name",
  "token": "40-char-sync-token",
  "encrypted": "",
  "enc_version": 0,
  "magic": "",
  "random_key": "",
  "head_commit_id": "40-char-hex",
  "permission": "rw"
}
```

**Encrypted Library Create Response:**
```json
{
  "repo_id": "uuid",
  "token": "40-char-sync-token",
  "encrypted": 1,
  "enc_version": 2,
  "magic": "64-char-hex",
  "random_key": "96-char-hex",
  ...
}
```

### Critical Implementation Notes

1. **Username is actually email**: The CLI uses `-u username` but expects email address format
2. **URL encoding**: Passwords with special characters must be URL-encoded
3. **Trailing slashes**: Support both `/api2/repos/` and `/api2/repos` (Seafile clients inconsistent)
4. **pack-fs format**: Must be `[40-byte hex ID][4-byte BE size][zlib data]` - NOT raw JSON
5. **FS ID computation**: SHA-1 of JSON with alphabetically sorted keys
6. **Block encryption**: Format is `[16-byte IV][AES-256-CBC ciphertext with PKCS7 padding]`
7. **PBKDF2 key derivation**: Magic uses `repo_id + password`, random_key uses `password` only

---

## Complete Workflow Examples

### Example 1: Create Plain Library and Upload File

```bash
# Step 1: Authenticate
TOKEN=$(curl -s -X POST "https://server/api2/auth-token/" \
  -d "username=user@example.com&password=secret" | jq -r '.token')

# Step 2: Create library
REPO_INFO=$(curl -s -X POST "https://server/api2/repos/" \
  -H "Authorization: Token $TOKEN" \
  -d "name=My+Library&desc=Test+library")
REPO_ID=$(echo $REPO_INFO | jq -r '.repo_id')
SYNC_TOKEN=$(echo $REPO_INFO | jq -r '.token')

# Step 3: Get upload URL
UPLOAD_URL=$(curl -s "https://server/api2/repos/$REPO_ID/upload-link/" \
  -H "Authorization: Token $TOKEN" | tr -d '"')

# Step 4: Upload file
curl -s -X POST "$UPLOAD_URL" \
  -F "parent_dir=/" \
  -F "file=@/path/to/local/file.txt"

# Step 5: List files to verify
curl -s "https://server/api2/repos/$REPO_ID/dir/?p=/" \
  -H "Authorization: Token $TOKEN"
```

### Example 2: Create Encrypted Library Workflow

```bash
# Step 1: Authenticate
TOKEN=$(curl -s -X POST "https://server/api2/auth-token/" \
  -d "username=user@example.com&password=secret" | jq -r '.token')

# Step 2: Create encrypted library
REPO_INFO=$(curl -s -X POST "https://server/api2/repos/" \
  -H "Authorization: Token $TOKEN" \
  -d "name=Secret+Library&passwd=MySecretPassword")

REPO_ID=$(echo $REPO_INFO | jq -r '.repo_id')
MAGIC=$(echo $REPO_INFO | jq -r '.magic')
RANDOM_KEY=$(echo $REPO_INFO | jq -r '.random_key')

echo "Created encrypted library: $REPO_ID"
echo "Magic: $MAGIC"
echo "Random Key: $RANDOM_KEY"

# Step 3: Unlock library (verify password - required before upload/download)
curl -s -X POST "https://server/api/v2.1/repos/$REPO_ID/set-password/" \
  -H "Authorization: Token $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"password": "MySecretPassword"}'

# Step 4: Upload file (server will encrypt)
UPLOAD_URL=$(curl -s "https://server/api2/repos/$REPO_ID/upload-link/" \
  -H "Authorization: Token $TOKEN" | tr -d '"')

curl -s -X POST "$UPLOAD_URL" \
  -F "parent_dir=/" \
  -F "file=@/path/to/secret-file.txt"

# Step 5: Download file (server will return encrypted content)
DOWNLOAD_URL=$(curl -s "https://server/api2/repos/$REPO_ID/file/?p=/secret-file.txt" \
  -H "Authorization: Token $TOKEN" | tr -d '"')

# The downloaded content is encrypted; client must decrypt with file_key
curl -s "$DOWNLOAD_URL" -o /tmp/encrypted-download.bin
```

### Example 3: Decrypt File Key and Blocks (Python)

```python
import hashlib
from Crypto.Cipher import AES
from Crypto.Protocol.KDF import PBKDF2

# Constants for enc_version 2
STATIC_SALT = bytes([0xda, 0x90, 0x45, 0xc3, 0x06, 0xc7, 0xcc, 0x26])

def derive_file_key(password, random_key_hex):
    """
    Derive the file encryption key from password and encrypted random_key.

    Args:
        password: User's password (str)
        random_key_hex: 96-char hex string from download-info

    Returns:
        32-byte file key for AES-256-CBC
    """
    # Derive encryption key from password only (NOT repo_id + password)
    enc_key = PBKDF2(
        password.encode('utf-8'),
        STATIC_SALT,
        dkLen=32,
        count=1000,
        hmac_hash_module=hashlib.sha256
    )

    # Derive IV using the key as input
    enc_iv = PBKDF2(
        enc_key,
        STATIC_SALT,
        dkLen=16,
        count=10,
        hmac_hash_module=hashlib.sha256
    )

    # Decrypt the random_key to get the file key
    encrypted_key = bytes.fromhex(random_key_hex)
    cipher = AES.new(enc_key, AES.MODE_CBC, enc_iv)
    file_key = cipher.decrypt(encrypted_key)

    # Remove PKCS7 padding
    pad_len = file_key[-1]
    file_key = file_key[:-pad_len]

    return file_key  # 32 bytes

def decrypt_block(encrypted_block, file_key):
    """
    Decrypt a block downloaded from an encrypted library.

    Args:
        encrypted_block: Raw bytes from /block/{id} endpoint
        file_key: 32-byte key from derive_file_key()

    Returns:
        Decrypted block content
    """
    # Format: [16-byte IV][AES-256-CBC encrypted content]
    iv = encrypted_block[:16]
    ciphertext = encrypted_block[16:]

    cipher = AES.new(file_key, AES.MODE_CBC, iv)
    plaintext = cipher.decrypt(ciphertext)

    # Remove PKCS7 padding
    pad_len = plaintext[-1]
    return plaintext[:-pad_len]

def verify_password(password, repo_id, magic_hex):
    """
    Verify password by computing magic and comparing.

    Args:
        password: User's password
        repo_id: Repository UUID (with hyphens)
        magic_hex: 64-char magic from download-info

    Returns:
        True if password is correct
    """
    # Magic uses repo_id + password as input
    input_data = (repo_id + password).encode('utf-8')

    computed_key = PBKDF2(
        input_data,
        STATIC_SALT,
        dkLen=32,
        count=1000,
        hmac_hash_module=hashlib.sha256
    )

    computed_magic = computed_key.hex()
    return computed_magic == magic_hex

# Usage Example
if __name__ == "__main__":
    # Values from download-info response
    repo_id = "af53e8f5-c8d6-4882-bcf2-1bf42b7148d0"
    password = "MySecretPassword"
    magic = "e50f0baf1edbf198..."  # 64 hex chars
    random_key = "1b42dec5e218c169..."  # 96 hex chars

    # Verify password
    if verify_password(password, repo_id, magic):
        print("Password correct!")

        # Get file key
        file_key = derive_file_key(password, random_key)
        print(f"File key: {file_key.hex()}")

        # Decrypt a downloaded block
        with open("/tmp/encrypted-block.bin", "rb") as f:
            encrypted = f.read()

        decrypted = decrypt_block(encrypted, file_key)
        print(f"Decrypted content: {decrypted}")
```

### Example 4: Full Sync Download Flow (Python)

```python
import requests
import zlib
import json
import hashlib

def sync_download_library(server_url, api_token, repo_id, password=None):
    """
    Complete sync download of a library.
    """
    headers = {"Authorization": f"Token {api_token}"}

    # Step 1: Get download-info (includes sync token)
    resp = requests.get(
        f"{server_url}/api2/repos/{repo_id}/download-info/",
        headers=headers
    )
    info = resp.json()
    sync_token = info['token']
    is_encrypted = info.get('encrypted') == 1

    sync_headers = {"Seafile-Repo-Token": sync_token}

    # Step 2: Get file key if encrypted
    file_key = None
    if is_encrypted and password:
        file_key = derive_file_key(password, info['random_key'])

    # Step 3: Get HEAD commit
    resp = requests.get(
        f"{server_url}/seafhttp/repo/{repo_id}/commit/HEAD",
        headers=sync_headers
    )
    head = resp.json()
    commit_id = head['head_commit_id']

    # Step 4: Get commit object
    resp = requests.get(
        f"{server_url}/seafhttp/repo/{repo_id}/commit/{commit_id}",
        headers=sync_headers
    )
    commit = resp.json()
    root_id = commit['root_id']

    # Step 5: Get all FS IDs
    resp = requests.get(
        f"{server_url}/seafhttp/repo/{repo_id}/fs-id-list/",
        headers=sync_headers,
        params={"server-head": commit_id}
    )
    fs_ids = resp.json()

    # Step 6: Download FS objects via pack-fs
    resp = requests.post(
        f"{server_url}/seafhttp/repo/{repo_id}/pack-fs/",
        headers=sync_headers,
        json=fs_ids
    )

    # Parse pack-fs binary format
    fs_objects = {}
    data = resp.content
    offset = 0
    while offset + 44 <= len(data):
        fs_id = data[offset:offset+40].decode('ascii')
        size = int.from_bytes(data[offset+40:offset+44], 'big')
        compressed = data[offset+44:offset+44+size]
        decompressed = zlib.decompress(compressed)
        fs_objects[fs_id] = json.loads(decompressed)
        offset += 44 + size

    # Step 7: Walk directory tree and download files
    def walk_dir(dir_id, path=""):
        dir_obj = fs_objects.get(dir_id)
        if not dir_obj or dir_obj.get('type') != 3:
            return

        for entry in dir_obj.get('dirents', []):
            entry_path = f"{path}/{entry['name']}"

            if entry['mode'] == 16384:  # Directory
                walk_dir(entry['id'], entry_path)
            else:  # File
                file_obj = fs_objects.get(entry['id'])
                if file_obj and file_obj.get('type') == 1:
                    download_file(entry_path, file_obj, file_key)

    def download_file(path, file_obj, file_key):
        print(f"Downloading: {path}")
        content = b''

        for block_id in file_obj['block_ids']:
            resp = requests.get(
                f"{server_url}/seafhttp/repo/{repo_id}/block/{block_id}",
                headers=sync_headers
            )
            block_data = resp.content

            if file_key:
                block_data = decrypt_block(block_data, file_key)

            # Verify block integrity
            computed_id = hashlib.sha1(block_data).hexdigest()
            assert computed_id == block_id, f"Block {block_id} integrity failed"

            content += block_data

        assert len(content) == file_obj['size'], "Size mismatch"
        return content

    # Start recursive download from root
    walk_dir(root_id)

# Usage
sync_download_library(
    "https://server",
    "113219421eef29cebe842dd8801ec1243eeb460e",
    "aa89cce8-f2f1-44ad-98f0-1ea87ca6a3ed",
    password=None  # Set for encrypted libraries
)
```

### Example 5: Content Verification Test

This example demonstrates verifying that downloaded content matches the original:

```bash
#!/bin/bash
# verify-content.sh - Verify file integrity through upload/download cycle

SERVER="https://server"
TOKEN="your-api-token"
REPO_ID="your-repo-id"
TEST_FILE="/tmp/test-verification.txt"

# Create test file with known content
echo "Test content at $(date)" > "$TEST_FILE"
ORIGINAL_HASH=$(sha256sum "$TEST_FILE" | cut -d' ' -f1)
echo "Original SHA256: $ORIGINAL_HASH"

# Upload
UPLOAD_URL=$(curl -s "$SERVER/api2/repos/$REPO_ID/upload-link/" \
  -H "Authorization: Token $TOKEN" | tr -d '"')
curl -s -X POST "$UPLOAD_URL" -F "parent_dir=/" -F "file=@$TEST_FILE"

# Download
DOWNLOAD_URL=$(curl -s "$SERVER/api2/repos/$REPO_ID/file/?p=/test-verification.txt" \
  -H "Authorization: Token $TOKEN" | tr -d '"')
curl -s "$DOWNLOAD_URL" -o /tmp/downloaded-verification.txt

# Verify
DOWNLOADED_HASH=$(sha256sum /tmp/downloaded-verification.txt | cut -d' ' -f1)
echo "Downloaded SHA256: $DOWNLOADED_HASH"

if [ "$ORIGINAL_HASH" = "$DOWNLOADED_HASH" ]; then
    echo "✓ Content verification PASSED"
else
    echo "✗ Content verification FAILED"
    diff "$TEST_FILE" /tmp/downloaded-verification.txt
fi
```

---

## Conflict Resolution

### When Conflicts Occur

Conflicts happen when:
1. Client A and Client B both modify the same file offline
2. Client uploads while server HEAD has changed (another client committed)
3. Network interruption during upload

### Conflict Detection

During upload, if `POST /update-branch` fails with "HEAD changed", the client must:

```
1. Download new server HEAD
2. Compare local changes with server changes
3. Merge or create conflict files
4. Create new commit with merged result
5. Retry upload
```

### Conflict Resolution Strategy

**File-level conflicts (same file modified):**
```
If file A modified locally AND file A modified on server:
  - Keep server version as "filename.txt"
  - Rename local version to "filename (SFConflict user@email 2026-01-14-12-30-45).txt"
  - User manually resolves
```

**Directory conflicts:**
```
If directory deleted locally but contains new server files:
  - Restore directory with server files
  - Mark as conflict for user review
```

### Merge Commit Structure

```json
{
  "commit_id": "abc123...",
  "root_id": "merged-root-fs-id",
  "parent_id": "local-commit-id",
  "second_parent_id": "server-commit-id",
  "creator": "user@example.com",
  "description": "Merged changes",
  "ctime": 1234567890
}
```

### Conflict File Naming

Pattern: `{original_name} (SFConflict {user} {timestamp}).{ext}`

Example: `report.docx (SFConflict john@example.com 2026-01-14-15-30-45).docx`

---

## Security Specifications

### Authentication Security

**Token Generation:**
```
- Length: 40 characters hex (160 bits entropy)
- Algorithm: cryptographically secure random bytes
- Storage: SHA-256 hash in database (never store plaintext)
- Expiration: Configurable (default: never for API tokens, 24h for sync tokens)
```

**Password Requirements:**
```
- Minimum length: 8 characters
- Hashing: Argon2id (memory: 64MB, iterations: 3, parallelism: 4)
- Alternative: bcrypt (cost factor: 12)
- Never store plaintext passwords
```

**Brute Force Protection:**
```
- Rate limit: 5 failed attempts per 15 minutes per IP
- Account lockout: 30 minutes after 10 failed attempts
- CAPTCHA: After 3 failed attempts
```

### Input Validation

**Path Traversal Prevention:**
```go
// Reject paths containing:
// - ".." (parent directory)
// - Absolute paths starting with "/"
// - Null bytes
// - Control characters

func validatePath(path string) error {
    if strings.Contains(path, "..") {
        return errors.New("invalid path: contains ..")
    }
    if strings.HasPrefix(path, "/") && path != "/" {
        return errors.New("invalid path: absolute paths not allowed")
    }
    if strings.ContainsAny(path, "\x00") {
        return errors.New("invalid path: contains null byte")
    }
    return nil
}
```

**Filename Validation:**
```
- Max length: 255 bytes
- Forbidden characters: / \ : * ? " < > | (Windows compatibility)
- Forbidden names: CON, PRN, AUX, NUL, COM1-9, LPT1-9 (Windows reserved)
- Unicode normalization: NFC
```

**Request Size Limits:**
```
- Max request body: 100MB (for file uploads)
- Max URL length: 8KB
- Max header size: 16KB
- Max JSON depth: 20 levels
```

### HTTPS Requirements

```
- TLS 1.2 minimum (TLS 1.3 recommended)
- Strong cipher suites only
- HSTS header: Strict-Transport-Security: max-age=31536000
- Certificate validation required for production
```

### CSRF Protection

For web UI endpoints (not API):
```
- CSRF token in session
- Validate on all state-changing requests (POST, PUT, DELETE)
- SameSite cookie attribute: Strict
```

### Content Security

```
- Content-Type validation on upload
- Virus scanning recommended for uploaded files
- Executable file restrictions (configurable)
```

---

## Testing Guidance

### Test Vectors

#### Empty Directory FS Object

```json
{
  "type": 3,
  "version": 1,
  "dirents": []
}
```
**fs_id:** `SHA-1("3\n1\n[]")` = `0000000000000000000000000000000000000000` (special empty dir)

#### File FS Object (Seafile Object)

```json
{
  "type": 1,
  "version": 1,
  "size": 13,
  "block_ids": ["aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d"]
}
```
Content: `"Hello, World!"`
Block ID: `SHA-1("Hello, World!")` = `aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d`

#### Encrypted Block

```
Plaintext: "Secret data"
Password: "test123"
Repo ID: "550e8400-e29b-41d4-a716-446655440000"

Salt (v2): da9045c306c7cc26
PBKDF2 iterations: 1000

File key derivation:
  input = "test123"
  key = PBKDF2-SHA256(input, salt, 1000, 32)
  iv = PBKDF2-SHA256(key, salt, 10, 16)

Block format: [16-byte random IV][AES-256-CBC(plaintext, file_key, random_iv)]
```

### Interoperability Tests

**Test 1: Create Library via API**
```bash
# Should return repo_id and sync token
curl -X POST "https://server/api2/repos/" \
  -H "Authorization: Token $TOKEN" \
  -d "name=TestLib"
```

**Test 2: Sync with Official Seafile Client**
```bash
# Download and sync
seaf-cli download -l $REPO_ID -s https://server -u user@test.com -p password
seaf-cli status  # Should show "synchronized"
```

**Test 3: Encrypted Library Round-Trip**
```bash
# Create encrypted library
curl -X POST "https://server/api2/repos/" \
  -H "Authorization: Token $TOKEN" \
  -d "name=EncryptedLib&passwd=secret123"

# Unlock
curl -X POST "https://server/api/v2.1/repos/$REPO_ID/set-password/" \
  -H "Authorization: Token $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"password": "secret123"}'

# Upload file
# Download file
# Verify content matches
```

**Test 4: Conflict Resolution**
```bash
# Client A: Modify file, go offline
# Client B: Modify same file, sync
# Client A: Come online, sync
# Verify: Conflict file created with proper naming
```

### Performance Benchmarks

| Operation | Target Latency | Notes |
|-----------|---------------|-------|
| `GET /commit/HEAD` | < 50ms | Simple DB lookup |
| `POST /check-blocks` (1000 IDs) | < 200ms | Batch query |
| `GET /block/{id}` (1MB) | < 500ms | Depends on storage |
| `POST /pack-fs` (100 objects) | < 300ms | Zlib compression |
| Full sync (1000 files, 100MB) | < 60s | Network dependent |

### Validation Checklist

- [ ] Official Seafile desktop client can sync libraries
- [ ] Official Seafile mobile app can browse/download files
- [ ] Encrypted libraries work with correct password
- [ ] Wrong password returns proper error
- [ ] File conflicts create conflict files (not data loss)
- [ ] Large files (>1GB) sync successfully
- [ ] Unicode filenames preserved correctly
- [ ] File permissions (read-only libraries) enforced
- [ ] Deleted files go to trash
- [ ] Quota limits enforced

---

## Error Catalog

### HTTP Status Codes

| Code | Meaning | When Used |
|------|---------|-----------|
| 200 | Success | All successful operations |
| 201 | Created | Library/directory created |
| 400 | Bad Request | Invalid parameters, malformed JSON |
| 401 | Unauthorized | Missing or invalid token |
| 403 | Forbidden | No permission, library locked, quota exceeded |
| 404 | Not Found | Library/file/block doesn't exist |
| 409 | Conflict | Name already exists, concurrent modification |
| 413 | Payload Too Large | File exceeds size limit |
| 500 | Server Error | Database error, storage failure |
| 503 | Service Unavailable | Maintenance mode, overloaded |

### Error Response Format

```json
{
  "error_msg": "Human-readable error message",
  "error_code": "MACHINE_READABLE_CODE"
}
```

### Common Errors

| Error Code | HTTP | Message | Resolution |
|------------|------|---------|------------|
| `INVALID_TOKEN` | 401 | "Invalid token" | Re-authenticate |
| `TOKEN_EXPIRED` | 401 | "Token expired" | Re-authenticate |
| `PERMISSION_DENIED` | 403 | "Permission denied" | Check library permissions |
| `LIBRARY_ENCRYPTED` | 403 | "Library is encrypted" | Call set-password first |
| `WRONG_PASSWORD` | 400 | "Wrong password" | Correct password needed |
| `QUOTA_EXCEEDED` | 403 | "Storage quota exceeded" | Delete files or upgrade |
| `FILE_LOCKED` | 403 | "File is locked by another user" | Wait or break lock |
| `NAME_EXISTS` | 409 | "A library with this name already exists" | Use different name |
| `REPO_NOT_FOUND` | 404 | "Library not found" | Check repo_id |
| `FILE_NOT_FOUND` | 404 | "File not found" | Check file path |
| `BLOCK_MISSING` | 404 | "Block not found" | Re-upload file |
| `HEAD_CHANGED` | 409 | "Repository HEAD has changed" | Merge and retry |

---

## References

- Seafile Server Source: https://github.com/haiwen/seafile-server
- Seafile Daemon Source: https://github.com/haiwen/seafile
- Seafile Desktop Client: https://github.com/haiwen/seafile-client

### Key Source Files

| File | Purpose |
|------|---------|
| `daemon/sync-mgr.c` | Sync state machine |
| `daemon/http-tx-mgr.c` | HTTP transfer manager |
| `common/fs-mgr.c` | FS object handling (line 1605: zlib decompression) |
| `fileserver/pack-dir.c` | pack-fs implementation |
