# Seafile Client Authentication

**Version**: 2
**Status**: Supplement to [SEAFILE-SYNC-PROTOCOL.md](SEAFILE-SYNC-PROTOCOL.md)

This document covers advanced authentication methods for Seafile desktop/mobile clients including SSO, Two-Factor Authentication, and API token management.

---

## Table of Contents

1. [Basic Authentication](#basic-authentication)
2. [SSO Authentication](#sso-authentication)
3. [Two-Factor Authentication](#two-factor-authentication)
4. [API Token Authentication](#api-token-authentication)
5. [CLI Authentication Options](#cli-authentication-options)
6. [Database Schema](#database-schema)
7. [Implementation Checklist](#implementation-checklist)

---

## Basic Authentication

### POST /api2/auth-token/

Standard username/password authentication (covered in main protocol doc).

```
POST /api2/auth-token/
Content-Type: application/x-www-form-urlencoded

username=user%40example.com&password=secret
```

**Response:**
```json
{"token": "113219421eef29cebe842dd8801ec1243eeb460e"}
```

### Desktop Client Quirks (Verified 2026-02-17, Seafile 9.0.16 Windows)

The Seafile desktop client has several quirks when calling this endpoint:

1. **Defensive TrimSpace**: The server applies `TrimSpace()` to username and password as a defensive measure against trailing whitespace in form data.

2. **Both Content-Types**: While the spec says `application/x-www-form-urlencoded`, some client versions may send `application/json` with `{"username":"...","password":"..."}`. The server should support both.

3. **`head-commits-multi` has no auth**: The desktop client calls `POST /seafhttp/repo/head-commits-multi` every ~30 seconds with NO auth headers at all — no `Authorization`, no `Seafile-Repo-Token`. This is a multi-repo polling endpoint that only returns commit hashes. The server must handle this gracefully (either allow unauthenticated access or provide an anonymous fallback).

---

## SSO Authentication

For servers using Single Sign-On (OIDC), the desktop client uses a **pending token + polling**
flow (compatible with seahub's `ClientSSOToken` mechanism). No authentication token is required
to initiate the flow.

### Step 1: Create Pending Token

```
POST /api2/client-sso-link
(no Authorization header required)
```

**Response:**
```json
{
  "link": "https://server/oauth/login/?token=<40-char-hex-token>",
  "token": "<40-char-hex-token>"
}
```

The client parses the pending token from the link URL (`?token=`) — SeaDrive/Seafile desktop
requires this param name to start polling. Opens `link` in the system browser and simultaneously
begins polling `GET /api2/client-sso-link/<token>`.

### Step 2: Poll for Completion

```
GET /api2/client-sso-link/<sso_token>
(no Authorization header required — the token itself is the credential)
```

**Response (pending):**
```json
{"status": "pending"}
```

**Response (complete):**
```json
{
  "status": "success",
  "email": "user@example.com",
  "apiToken": "session-token"
}
```

The client polls every 1-2 seconds until `status == "success"`. Tokens expire after 15 minutes.

### SSO Flow Diagram

```
┌──────────────────────────────────────────────────────────────────────┐
│                  DESKTOP CLIENT SSO AUTHENTICATION FLOW               │
├──────────────────────────────────────────────────────────────────────┤
│                                                                       │
│  Client                     Server                  Browser           │
│    │                          │                        │              │
│    │─POST /client-sso-link───▶│                        │              │
│    │◀──{link: "…?sso_token=T"}│                        │              │
│    │                          │                        │              │
│    │──────────────────────Open link (with sso_token)──▶│              │
│    │                          │                        │              │
│    │                          │◀──Redirect to OIDC─────│              │
│    │                          │   provider             │              │
│    │                          │                        │──User logs in│
│    │                          │◀──callback?code=xxx────│              │
│    │                          │   (sso_token in state) │              │
│    │                          │                        │              │
│    │                          │──Mark T as success     │              │
│    │                          │──Redirect to seafile://client-login/──▶│
│    │                          │                        │              │
│    │──GET /client-sso-link/T─▶│                        │              │
│    │◀──{status: "pending"}────│                        │              │
│    │                          │                        │              │
│    │   ... poll every 2s ...  │                        │              │
│    │                          │                        │              │
│    │──GET /client-sso-link/T─▶│                        │              │
│    │◀──{status: "success",    │                        │              │
│    │    apiToken: "xxx"}──────│                        │              │
│    │                          │                        │              │
└──────────────────────────────────────────────────────────────────────┘
```

---

## Two-Factor Authentication

When 2FA is enabled for a user, the standard `/api2/auth-token/` endpoint returns an error requiring a TOTP code.

### 2FA Error Response

```json
{"non_field_errors": ["Two factor auth token is missing."]}
```

### Providing 2FA Code

Add the `X-SEAFILE-OTP` header with the 6-digit TOTP code:

```
POST /api2/auth-token/
Content-Type: application/x-www-form-urlencoded
X-SEAFILE-OTP: 123456

username=user%40example.com&password=secret
```

### TOTP Implementation

- **Algorithm**: TOTP (RFC 6238)
- **Digits**: 6
- **Period**: 30 seconds
- **Hash**: SHA-1
- **Window**: Allow ±1 period for clock drift

### 2FA Database Schema

```cql
CREATE TABLE sesamefs.user_2fa (
    user_id UUID PRIMARY KEY,
    totp_secret TEXT,                   -- Base32 encoded secret
    backup_codes SET<TEXT>,             -- Hashed backup codes
    enabled_at TIMESTAMP
);
```

---

## API Token Authentication

For SSO environments or automated scripts, users can generate long-lived API tokens from the web interface.

### Using API Tokens

Instead of username/password:

```bash
# CLI with API token
seaf-cli download -l <library-id> -s <server-url> -d <directory> \
    -u <username> -T <api-token>
```

### Token Management Endpoints

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api2/auth-token/` | POST | Generate token (with -T flag) |
| Web UI Profile Page | - | Generate/revoke tokens |

### Library-Specific Tokens (Optional)

Per-library API tokens for fine-grained access:

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/v2.1/repos/{id}/repo-api-tokens/` | GET | List library tokens |
| `/api/v2.1/repos/{id}/repo-api-tokens/` | POST | Create library token |
| `/api/v2.1/repos/{id}/repo-api-tokens/{token}` | DELETE | Delete token |

---

## CLI Authentication Options

The `seaf-cli` tool supports multiple authentication methods:

### Password Authentication
```bash
seaf-cli download -l <library-id> -s <server-url> -d <directory> \
    -u <username> -p <password>
```

### API Token Authentication (for SSO)
```bash
# Get token from profile page in web interface
seaf-cli download -l <library-id> -s <server-url> -d <directory> \
    -u <username> -T <api-token>
```

### Two-Factor Authentication
```bash
seaf-cli download -l <library-id> -s <server-url> -d <directory> \
    -u <username> -p <password> -a <2fa-code>
```

### Authentication Flags

| Flag | Description |
|------|-------------|
| `-u, --username` | User email |
| `-p, --password` | User password |
| `-T, --token` | API token (instead of password) |
| `-a, --tfa` | Two-factor auth code |

---

## Database Schema

### Users by OIDC (for SSO)

```cql
CREATE TABLE sesamefs.users_by_oidc (
    oidc_issuer TEXT,
    oidc_sub TEXT,
    user_id UUID,
    org_id UUID,
    PRIMARY KEY ((oidc_issuer, oidc_sub))
);
```

### Two-Factor Authentication

```cql
CREATE TABLE sesamefs.user_2fa (
    user_id UUID PRIMARY KEY,
    totp_secret TEXT,                   -- Base32 encoded TOTP secret
    backup_codes SET<TEXT>,             -- Hashed backup codes
    enabled_at TIMESTAMP
);
```

---

## Implementation Checklist

### Two-Factor Authentication

| Feature | Implementation | Status |
|---------|----------------|--------|
| Accept `X-SEAFILE-OTP` header | `/api2/auth-token/` | |
| Validate TOTP codes | 6-digit, 30-second window | |
| Return 2FA error | `{"non_field_errors":["Two factor auth token is missing."]}` | |
| Per-user 2FA secret storage | Database table for TOTP secrets | |
| Backup codes | One-time use recovery codes | |

### SSO Support

| Feature | Implementation | Status |
|---------|----------------|--------|
| `POST /api2/client-sso-link` | Create pending token, return browser URL | ✅ Implemented |
| `GET /api2/client-sso-link/:token` | Poll for SSO completion | ✅ Implemented |
| OIDC user lookup | `users_by_oidc` table | ✅ Implemented |
| SAML integration | Not planned | — |

### API Token Management

| Feature | Implementation | Status |
|---------|----------------|--------|
| Generate API tokens | Profile page in web UI | |
| Token endpoint | `POST /api2/auth-token/` with `-T` flag | |
| Long-lived tokens | Separate from session tokens | |
| Token revocation | Web UI or API endpoint | |
| Library-specific tokens | `/api/v2.1/repos/{id}/repo-api-tokens/` | |

---

## Error Catalog

| Error Code | HTTP Status | Response | Solution |
|------------|-------------|----------|----------|
| `2FA_REQUIRED` | 401 | `{"non_field_errors":["Two factor auth token is missing."]}` | Add `X-SEAFILE-OTP` header |
| `2FA_INVALID` | 401 | `{"non_field_errors":["Invalid two factor auth token."]}` | Check TOTP code |
| `SSO_REQUIRED` | 401 | `{"non_field_errors":["SSO authentication required."]}` | Use SSO flow |
| `TOKEN_INVALID` | 401 | `{"detail":"Invalid token."}` | Re-authenticate |
| `TOKEN_EXPIRED` | 401 | `{"detail":"Token has expired."}` | Re-authenticate |

---

## References

- [SEAFILE-SYNC-PROTOCOL.md](SEAFILE-SYNC-PROTOCOL.md) - Main sync protocol documentation
- [ENCRYPTION.md](ENCRYPTION.md) - Encrypted library password handling
- RFC 6238 - TOTP: Time-Based One-Time Password Algorithm
- RFC 4226 - HOTP: HMAC-Based One-Time Password Algorithm
