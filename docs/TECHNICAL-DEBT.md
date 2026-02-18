# Technical Debt & Migration Plan

This document tracks known technical debt and provides actionable plans for addressing each issue while the system remains in use.

---

## 1. Multi-Host ServiceURL — ✅ FIXED (2026-02-09)

### Status
The frontend now uses `window.location.origin` by default for API calls, enabling multi-tenant deployments where different hostnames (us.sesamefs.com, eu.sesamefs.com) route to the same system.

### What Was Done
- `frontend/public/index.html`: `serviceURL` set to `window.SESAMEFS_API_URL || ''` (empty by default)
- `frontend/src/utils/seafile-api.js`: Fallback `const server = serviceURL || window.location.origin` handles the empty case
- `docker-compose.yaml`: `SESAMEFS_API_URL=` is empty for production builds
- `docker-entrypoint.sh`: Only injects `SESAMEFS_API_URL` when explicitly set at container runtime
- No hardcoded `localhost` references remain in `frontend/src/`

### Result
- `https://us.sesamefs.com` → API calls go to `https://us.sesamefs.com/api/...`
- `https://eu.sesamefs.com` → API calls go to `https://eu.sesamefs.com/api/...`
- Dev mode: Set `SESAMEFS_API_URL` env var or use nginx proxy (frontend port 3001 → backend port 8082)

---

## 2. Modal Pattern — ✅ MIGRATION COMPLETE (2026-01-30)

### Status
All 122 modal dialog components have been migrated from reactstrap `<Modal>` to plain Bootstrap modal classes. Zero dialog files import `Modal` from reactstrap.

### Remaining Cleanup: ModalPortal Wrapper Removal
~51 parent components still wrap already-fixed dialog components in `<ModalPortal>`. This is harmless (dialogs render correctly) but unnecessary. Remove wrappers opportunistically when touching these files.

**Before** (unnecessary wrapper):
```jsx
{this.state.isDialogOpen && (
  <ModalPortal>
    <SomeDialog toggle={this.toggle} />
  </ModalPortal>
)}
```

**After** (direct render):
```jsx
{this.state.isDialogOpen && (
  <SomeDialog toggle={this.toggle} />
)}
```

Parent components with `<ModalPortal>` wrappers are in:
- `components/dirent-list-view/`
- `components/dirent-grid-view/`
- `components/toolbar/`
- `components/user-settings/`
- `pages/sys-admin/`
- `pages/org-admin/`
- `pages/groups/`
- `pages/my-libs/`
- `pages/wikis/`

---

## 3. seafile-js Hardcoded Paths (NO ACTION NEEDED)

### Problem
The `seafile-js` npm package has hardcoded API paths that cannot be changed without forking.

### Impact
- Backend MUST implement exact Seafile API paths
- Cannot use custom API prefixes

### Solution
**This is acceptable** - we're building a Seafile-compatible API, so matching their paths is intentional.

### Documented Constraints
The backend must implement these exact paths (from seafile-js):
| Method | Path |
|--------|------|
| listRepos | `GET /api/v2.1/repos/` |
| deleteRepo | `DELETE /api/v2.1/repos/:id/` |
| listDir | `GET /api/v2.1/repos/:id/dir/?p=:path` |
| lockfile | `PUT /api/v2.1/repos/:id/file/?p=:path` |
| etc. | See docs/API-REFERENCE.md |

---

## 4. Test Coverage (ONGOING)

### Current State
| Package | Coverage | Target |
|---------|----------|--------|
| `internal/config` | 92.5% | Maintain |
| `internal/chunker` | 79.2% | Maintain |
| `internal/storage` | 46.6% | 60% |
| `internal/api` | 17.5% | 40% |
| `internal/api/v2` | 16.3% | 40% |

### Strategy: Test As You Fix

When fixing a bug or adding a feature:
1. Write a test that reproduces the issue
2. Fix the issue
3. Verify test passes
4. Commit both together

### High-Value Tests to Add

**1. API Handler Tests** (`internal/api/v2/*_test.go`)
```go
// Test request validation
func TestCreateLibrary_EmptyName(t *testing.T) {
    // Should return 400 Bad Request
}

// Test authorization
func TestDeleteLibrary_NotOwner(t *testing.T) {
    // Should return 403 Forbidden
}
```

**2. Integration Tests** (with mock DB)
```go
// Test full flow with mocked dependencies
func TestUploadDownloadRoundtrip(t *testing.T) {
    // Upload file, verify stored, download, verify contents
}
```

**3. Frontend Tests** (`frontend/src/**/__tests__/`)
```javascript
// Test API client error handling
describe('seafile-api', () => {
    it('handles 401 by redirecting to login', async () => {
        // ...
    });
});
```

### CI Integration
Add to `.github/workflows/test.yml`:
```yaml
- name: Check coverage threshold
  run: |
    go test ./... -coverprofile=coverage.out
    COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')
    if (( $(echo "$COVERAGE < 25" | bc -l) )); then
      echo "Coverage $COVERAGE% is below threshold 25%"
      exit 1
    fi
```

---

## 5. Frontend Features Pending

### Authentication & Session
| Feature | Status | Notes |
|---------|--------|-------|
| Logout button | ✅ Working | `/accounts/logout/` clears localStorage |
| Session management | ⚠️ Basic | Dev tokens only, no OIDC yet |

### Notifications
| Feature | Status | Notes |
|---------|--------|-------|
| `/api/v2.1/notifications/` | ⚠️ Stub | Returns empty array |
| Real-time notifications | ❌ Not implemented | Would need WebSocket or polling |
| Activity feed | ❌ Not implemented | `/api2/events/` not implemented |

### Sharing Features
| Feature | Status | Notes |
|---------|--------|-------|
| "Shared with me" page | ⚠️ Shows own libs | Needs filter by `type: "shared"` |
| Share dialog | ⚠️ Modal shows | Backend share endpoints are stubs |
| Move/Copy dialogs | ⚠️ Modal shows | Backend move/copy partially implemented |
| Groups | ⚠️ Stub | `/api/v2.1/groups/` returns empty |

### File Viewer
| Feature | Status | Notes |
|---------|--------|-------|
| OnlyOffice (docx, xlsx, pptx) | ✅ Working | Full editing support, auth token handling fixed (2026-02-12) |
| New Office file creation | ✅ Working | Creates with valid template (not 0 bytes) |
| Images (jpg, png, etc.) | ✅ Working | Inline preview via `/lib/:id/file/*path`, raw serving via `/repo/:id/raw/*path` |
| PDF viewer | ✅ Working | Inline `<embed>` preview implemented (2026-02-12) |
| Video/Audio player | ✅ Working | Inline HTML5 video/audio players implemented (2026-02-12) |
| Text file viewer | ✅ Working | Code-highlighted text preview with syntax support (2026-02-12) |
| Thumbnails | ❌ Not implemented | `/thumbnail/` endpoint missing |

### Library Settings Dialogs
| Dialog | Status | Notes |
|--------|--------|-------|
| History settings | ✅ Complete | Full CRUD implemented |
| Auto-delete settings | ✅ Complete | Full CRUD implemented |
| API tokens | ✅ Complete | Full CRUD implemented |
| Transfer ownership | ✅ Complete | Backend implemented |

---

## 6. Programmatic Auth Gap — ⚠️ BLOCKS PROD (2026-02-18)

### Problem

In production (`dev_mode=false`, OIDC-only), there is **no way to get an API token
programmatically** — without a browser. This blocks two critical scenarios:

1. **Seafile desktop/mobile client login** — the client calls
   `POST /api2/auth-token/` with username+password to get a session token.
   In prod this endpoint always returns `401` (there is a `// TODO` in the code).

2. **Programmatic/admin access** — scripts, CI pipelines, the `seaf-cli` tool,
   or any API consumer that can't open a browser has no way to authenticate.

### Root Cause

`internal/api/server.go` — `handleAuthToken` function:

```go
// In dev mode: checks dev tokens by username match → works
if s.config.Auth.DevMode {
    // ... matches dev tokens ...
}

// In prod: TODO, falls through to 401
// TODO: Implement OIDC password grant or redirect to OIDC flow
c.JSON(http.StatusUnauthorized, gin.H{
    "non_field_errors": "Unable to login with provided credentials.",
})
```

### What Exists Today

| Method | Status | Notes |
|---|---|---|
| `POST /api2/auth-token/` username+password | ❌ Broken in prod | TODO in server.go |
| OIDC browser flow (`/api/v2.1/auth/oidc/login`) | ✅ Works | Browser only |
| Library-scoped API tokens | ✅ Works | Requires browser login first; single-library scope |
| Personal Access Tokens (user-level) | ❌ Not implemented | - |
| OIDC Device Flow (RFC 8628) | ❌ Not implemented | Best fit for CLI tools |
| OIDC Client Credentials grant | ❌ Not implemented | For service-to-service |

### Impact

- Seafile desktop client **cannot log in** in production → sync is broken
- `seaf-cli` **cannot authenticate** without dev tokens
- Admin scripts **cannot automate** API calls
- Users **cannot get tokens** without a browser

### Solutions (pick one or combine)

**Option A — OIDC Device Authorization Flow (RFC 8628)** ← Recommended

The cleanest long-term solution for CLI tools and headless clients:
1. Client calls `POST /api2/auth-token/` → server responds with a device code + URL
2. User opens the URL in a browser, approves
3. Client polls until approved → gets session token

Requires the OIDC provider (`accounts.sesamedisk.com`) to support Device Flow.

**Option B — Personal Access Tokens (PATs)**

Admin/users generate long-lived tokens via the web UI or admin API.
The token is a random string stored in Cassandra, scoped to the user (not a library).

- Implementation: ~200 lines in a new `internal/api/v2/access_tokens.go`
- Endpoints: `POST/GET/DELETE /api/v2.1/user/access-tokens/`
- Storage: new `personal_access_tokens` Cassandra table

**Option C — Allow OIDC-issued tokens in `/api2/auth-token/`**

After the user completes browser OIDC login, generate a longer-lived token they
can copy and use for CLI/API access. Simpler than PATs, less elegant.

### Workaround for Current Testing Phase

Keep `AUTH_DEV_MODE=true` with specific dev tokens while testing in prod.
This unblocks desktop client and CLI testing at the cost of real OIDC auth.

```bash
# In .env — temporary workaround while PATs / Device Flow are not implemented:
AUTH_DEV_MODE=true
AUTH_ALLOW_ANONYMOUS=false
# dev tokens defined in config.prod.yaml → auth.dev_tokens
```

### Priority

**High** — blocks any non-browser client (desktop sync, CLI, automation).
Must be resolved before promoting to general availability.

---

## Summary: Action Items

### Immediate (This Week)
- [x] **Fix serviceURL** - Changed to use `window.location.origin` ✅ (2026-02-09)
- [ ] **Document modal pattern** in CLAUDE.md (done)

### High Priority — Blocks Production
- [ ] **Programmatic auth gap** (Section 6) — Seafile clients and CLI cannot auth in OIDC-only mode.
  Implement Personal Access Tokens (PATs) or OIDC Device Flow.
  Workaround: keep `AUTH_DEV_MODE=true` with specific dev tokens during testing phase.

### Short-Term (As Encountered)
- [ ] Fix dialogs as users report issues
- [ ] Add tests when fixing bugs

### Long-Term (Backlog)
- [ ] Migrate all ~100 dialogs (can be automated with script)
- [ ] Increase test coverage to 40%
- [ ] Consider forking seafile-js for any customization needs
- [ ] OIDC JWT signature verification (Section 6)

---

## Monitoring Technical Debt

### Commands
```bash
# Count remaining ModalPortal wrappers in parent components
grep -rl "ModalPortal" frontend/src/ | wc -l

# Check test coverage
go test ./... -coverprofile=coverage.out && go tool cover -func=coverage.out | grep total

# Find hardcoded URLs
grep -r "localhost:8080" frontend/src/
```

### Metrics to Track
| Metric | Current | Target | How to Check |
|--------|---------|--------|--------------|
| Broken dialogs | 0 ✅ | 0 | All 122 migrated (2026-01-30) |
| Test coverage | 25% | 40% | go test -cover |
| Hardcoded URLs | 0 ✅ | 0 | grep localhost |

---

*Last updated: 2026-02-09*
