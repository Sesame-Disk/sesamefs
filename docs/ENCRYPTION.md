# SesameFS Encryption Architecture

This document describes how SesameFS handles encrypted libraries, with a focus on security improvements over Seafile while maintaining client compatibility.

## Overview

SesameFS implements a **dual-mode encryption system**:
- **Strong mode** (web/API): Argon2id key derivation, server-side encryption
- **Compat mode** (Seafile clients): PBKDF2 for client compatibility + additional server-side protection

This is similar to our SHA-1→SHA-256 block hash translation for sync protocol.

## Why Seafile's Encryption is Weak

### Seafile v2 Parameters (Current Default)

| Parameter | Value | Problem |
|-----------|-------|---------|
| Algorithm | PBKDF2-HMAC-SHA256 | OK, but iterations matter |
| Iterations | 1,000 | **Critical**: GPU can test 10M+ passwords/sec |
| Salt | Fixed 8-byte static | **Critical**: Rainbow tables work across ALL libraries |
| Key | 32 bytes | OK |

**Attack scenario**: With a 1,000-iteration PBKDF2 and fixed salt, an attacker with the encrypted library metadata can brute-force a typical password in hours, not years.

### Seafile v12 Improvements (Optional)

Seafile 12 added optional Argon2id support, but:
- Not the default
- Only works with new clients (>= 9.0.9)
- Still uses weak defaults for compatibility

## SesameFS Solution: Translation Layer

Just like we translate SHA-1→SHA-256 for block storage, we translate weak→strong for encryption:

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Password Input                               │
└─────────────────────────────┬───────────────────────────────────────┘
                              │
          ┌───────────────────┴───────────────────┐
          ▼                                       ▼
┌─────────────────────┐               ┌─────────────────────────┐
│   Seafile Client    │               │      Web/API Client     │
│   (Sync Protocol)   │               │                         │
└─────────┬───────────┘               └────────────┬────────────┘
          │                                        │
          ▼                                        ▼
┌─────────────────────┐               ┌─────────────────────────┐
│  PBKDF2 (1K iter)   │               │  Argon2id               │
│  (client compat)    │               │  t=3, m=64MB, p=4       │
└─────────┬───────────┘               └────────────┬────────────┘
          │                                        │
          │    ┌──────────────────────┐           │
          └───►│  Server Translation  │◄──────────┘
               │  Layer               │
               └──────────┬───────────┘
                          │
                          ▼
               ┌──────────────────────┐
               │  Storage             │
               │  - magic_compat      │ ← For Seafile client validation
               │  - magic_strong      │ ← For web/API validation
               │  - random_key_compat │ ← For Seafile client key exchange
               │  - random_key_strong │ ← For server-side encryption
               │  - enc_version       │
               │  - salt (32 bytes)   │ ← Per-library random salt
               └──────────────────────┘
```

## Security Properties

### What We CAN Protect

| Attack Vector | Protection |
|---------------|------------|
| Server breach (DB dump) | Strong Argon2id makes brute-force infeasible |
| Network interception | HTTPS + password never stored plaintext |
| Brute force attempts | Rate limiting (5 attempts/min) + audit logging |
| Rainbow tables | Per-library random 32-byte salt |
| Timing attacks | Constant-time password comparison |

### What We CANNOT Change (Seafile Client Limitation)

Seafile desktop/mobile clients perform **client-side encryption** using their weak key derivation. We cannot change:
- How the client derives the file encryption key
- The encryption of file content (happens before upload)

**However**, for files uploaded via Seafile client, we add:
1. Server-side envelope encryption (AES-256-GCM with strong key)
2. S3 SSE (if configured)

This means even if an attacker cracks the weak Seafile password, they still need to break our server-side encryption layer.

## Database Schema

```sql
-- Existing columns (Seafile compatibility)
encrypted BOOLEAN,
enc_version INT,
magic TEXT,           -- PBKDF2-derived (Seafile compat)
random_key TEXT,      -- Encrypted file key (Seafile compat)

-- New columns (SesameFS strong encryption)
salt TEXT,            -- 32-byte random, hex-encoded (per-library)
magic_strong TEXT,    -- Argon2id-derived verification token
random_key_strong TEXT, -- File key encrypted with strong derivation
enc_kdf_algo TEXT,    -- 'pbkdf2_sha256' or 'argon2id'
enc_kdf_params TEXT,  -- JSON: {"t":3,"m":65536,"p":4} or {"iterations":1000}
```

## Encryption Versions

| enc_version | Description | KDF | Notes |
|-------------|-------------|-----|-------|
| 2 | Seafile standard | PBKDF2 1K iterations | Fixed salt, weak |
| 4 | Seafile strong | PBKDF2 configurable | Per-repo salt |
| 10 | SesameFS default | Argon2id | Strong, web-only |
| 12 | SesameFS + compat | Argon2id + PBKDF2 | Dual-mode |

## API Endpoints

### Create Encrypted Library

```
POST /api/v2.1/repos/
POST /api2/repos/

Parameters:
  name: string (required)
  encrypted: true
  passwd: string (required for encrypted)

Response:
  {
    "repo_id": "uuid",
    "encrypted": true,
    "enc_version": 12,       // SesameFS dual-mode
    "salt": "hex...",        // 32-byte random
    "magic": "hex...",       // PBKDF2 (client compat)
    "random_key": "hex..."   // Encrypted file key (client compat)
  }
```

### Unlock Encrypted Library (Set Password)

```
POST /api/v2.1/repos/{repo_id}/set-password/

Parameters:
  password: string

Response (success):
  {"success": true}

Response (wrong password):
  {"error_msg": "Wrong password"} (400)
```

### Change Password

```
PUT /api/v2.1/repos/{repo_id}/set-password/

Parameters:
  old_password: string
  new_password: string

Response (success):
  {"success": true}

Response (wrong password):
  {"error_msg": "Wrong password"} (400)
```

## Key Derivation Details

### Argon2id Parameters (SesameFS Strong Mode)

```go
params := argon2.IDParams{
    Time:    3,      // 3 iterations
    Memory:  65536,  // 64 MB
    Threads: 4,      // 4 parallel lanes
    KeyLen:  32,     // 256-bit key
}
```

**Cost analysis**:
- Single attempt: ~200ms on modern CPU
- GPU attack: ~100 attempts/sec (vs 10M/sec for PBKDF2)
- Brute-force 8-char password: centuries, not hours

### PBKDF2 Parameters (Seafile Compat Mode)

```go
// Seafile v2 compatibility (required for desktop/mobile clients)
iterations := 1000
salt := []byte{0xda, 0x90, 0x45, 0xc3, 0x06, 0xc7, 0xcc, 0x26}  // Fixed :(
key := pbkdf2.Key(password, salt, iterations, 32, sha256.New)
```

### Magic Token Computation

```go
// For password verification (Seafile compatible)
func computeMagic(repoID, password string, salt []byte, version int) string {
    input := []byte(repoID + password)
    key := deriveKey(input, salt, version)
    return hex.EncodeToString(key)
}

// Verify by comparing computed magic with stored magic
func verifyPassword(repoID, password, storedMagic string, salt []byte, version int) bool {
    computed := computeMagic(repoID, password, salt, version)
    return subtle.ConstantTimeCompare([]byte(computed), []byte(storedMagic)) == 1
}
```

### File Key Encryption

```go
// The random_key is the file encryption key, encrypted with the derived key
func encryptFileKey(fileKey []byte, derivedKey []byte) string {
    iv := derivedKey[16:32]  // Use second half as IV (Seafile convention)
    key := derivedKey[:16]   // First half as AES key

    block, _ := aes.NewCipher(key)
    encrypted := make([]byte, len(fileKey)+16)  // +16 for padding
    mode := cipher.NewCBCEncrypter(block, iv)
    mode.CryptBlocks(encrypted, pkcs7Pad(fileKey))

    return hex.EncodeToString(encrypted)
}
```

## Rate Limiting

To prevent brute-force attacks:

```go
// Per-user, per-library rate limit
const (
    MaxPasswordAttempts = 5
    RateLimitWindow     = time.Minute
)

// Logged for security audit
type PasswordAttempt struct {
    UserID    string
    LibraryID string
    Success   bool
    IP        string
    Timestamp time.Time
}
```

## Implementation Plan

### Phase 1: Backend Core (This PR)
- [ ] Add `internal/crypto/` package with Argon2id + PBKDF2
- [ ] Implement magic token generation/verification
- [ ] Implement file key encryption/decryption
- [ ] Add `set-password` endpoint (POST + PUT)
- [ ] Update library creation for encrypted libs
- [ ] Add rate limiting and audit logging

### Phase 2: Frontend Dialogs
- [ ] Fix `lib-decrypt-dialog.js` modal pattern
- [ ] Fix `change-repo-password-dialog.js` modal pattern

### Phase 3: Server-Side Envelope Encryption
- [ ] Add envelope encryption for Seafile client uploads
- [ ] Transparent decrypt on download
- [ ] Key rotation support

## Testing

### Create Encrypted Library
```bash
curl -X POST -H "Authorization: Token $TOKEN" \
  "http://localhost:8080/api2/repos/" \
  -d "name=SecretVault" \
  -d "encrypted=true" \
  -d "passwd=MySecurePassword123"
```

### Unlock Library
```bash
curl -X POST -H "Authorization: Token $TOKEN" \
  "http://localhost:8080/api/v2.1/repos/$REPO_ID/set-password/" \
  -d "password=MySecurePassword123"
```

### Change Password
```bash
curl -X PUT -H "Authorization: Token $TOKEN" \
  "http://localhost:8080/api/v2.1/repos/$REPO_ID/set-password/" \
  -d "old_password=MySecurePassword123" \
  -d "new_password=NewSecurePassword456"
```

## Security Audit Checklist

- [ ] Passwords never logged or stored plaintext
- [ ] Constant-time comparison for magic verification
- [ ] Rate limiting enforced server-side
- [ ] Audit log for all password attempts
- [ ] Random salt per library (not fixed)
- [ ] Argon2id for new libraries created via web
- [ ] Server-side encryption layer for Seafile client uploads

## References

- [Seafile Security Features](https://manual.seafile.com/11.0/security/security_features/)
- [Seafile Encrypted Library Wiki](https://github.com/haiwen/seadroid/wiki/How-does-an-encrypted-library-work%3F)
- [Argon2 RFC 9106](https://www.rfc-editor.org/rfc/rfc9106.html)
- [OWASP Password Storage Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html)
