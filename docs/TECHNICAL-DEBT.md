# Technical Debt & Migration Plan

This document tracks known technical debt and provides actionable plans for addressing each issue while the system remains in use.

---

## 1. Multi-Host ServiceURL (EASY FIX)

### Problem
The frontend has `serviceURL: 'http://localhost:8080'` hardcoded in `public/index.html`, which breaks multi-tenant deployments where different hostnames (us.sesamefs.com, eu.sesamefs.com) point to the same system.

### Good News
The code **already supports this**! In `src/utils/seafile-api.js:11`:
```javascript
const server = serviceURL || window.location.origin;
```

If `serviceURL` is empty/null/undefined, it automatically uses `window.location.origin`.

### Solution (Immediate - 5 minutes)

**Option A: Use window.location.origin (Recommended)**

Edit `frontend/public/index.html`:
```javascript
// BEFORE:
serviceURL: window.SESAMEFS_API_URL,

// AFTER:
serviceURL: '', // Uses window.location.origin automatically
```

This makes the frontend work on ANY hostname without configuration.

**Option B: Runtime Detection**

Edit `frontend/public/index.html`:
```javascript
serviceURL: window.SESAMEFS_API_URL || window.location.origin,
```

This allows override via environment variable but falls back to current origin.

### Result
- `https://us.sesamefs.com` → API calls go to `https://us.sesamefs.com/api/...`
- `https://eu.sesamefs.com` → API calls go to `https://eu.sesamefs.com/api/...`
- `http://localhost:3000` → API calls go to `http://localhost:3000/api/...` (dev mode needs proxy)

### Dev Mode Consideration
For local development where frontend runs on port 3001 and backend on 8080:
- Keep using `window.SESAMEFS_API_URL` environment variable
- Or configure webpack proxy in `package.json`:
```json
"proxy": "http://localhost:8080"
```

---

## 2. Modal Pattern (MEDIUM EFFORT)

### Problem
~100 dialog components use reactstrap `<Modal>` which doesn't render inside Seafile's `ModalPortal` wrapper due to double-portal issues.

### Impact
- Dialogs that are CURRENTLY BROKEN won't show
- Only 3 dialogs have been fixed so far (delete-repo, create-repo, batch-delete-repo)

### Dialogs Already Fixed
| File | Status |
|------|--------|
| `delete-repo-dialog.js` | ✅ Fixed |
| `create-repo-dialog.js` | ✅ Fixed |
| `batch-delete-repo-dialog.js` | ✅ Fixed |

### Dialogs Still Broken (Need Migration)
| File | Status | Notes |
|------|--------|-------|
| `delete-folder-dialog.js` | ❌ Uses reactstrap Modal | Previously documented as fixed incorrectly |
| `create-folder-dialog.js` | ❌ Uses reactstrap Modal | High priority |
| `create-file-dialog.js` | ❌ Uses reactstrap Modal | High priority |
| `rename-dialog.js` | ❌ Uses reactstrap Modal | High priority |
| `rename-dirent.js` | ❌ Uses reactstrap Modal | High priority |

### Dialogs That Need Fixing (Priority Order)

**High Priority (Core Functionality)**
| Dialog | Used For |
|--------|----------|
| `create-folder-dialog.js` | Create new folder |
| `create-file-dialog.js` | Create new file |
| `rename-dialog.js` | Rename file/folder |
| `rename-dirent.js` | Rename dirent |
| `share-dialog.js` | Share file/folder |
| `copy-dirent-dialog.js` | Copy files |
| `move-dirent-dialog.js` | Move files |
| `lib-decrypt-dialog.js` | Decrypt encrypted library |

**Medium Priority (Useful Features)**
| Dialog | Used For |
|--------|----------|
| `create-group-dialog.js` | Create group |
| `share-repo-dialog.js` | Share library |
| `internal-link-dialog.js` | Get internal link |
| `lib-history-setting-dialog.js` | Library history settings |
| `change-repo-password-dialog.js` | Change library password |

**Low Priority (Admin/Org Features)**
- All `org-*.js` dialogs
- All `sysadmin-dialog/*.js` dialogs

### Migration Script

Create `scripts/migrate-modals.sh`:
```bash
#!/bin/bash
# Migrate reactstrap Modal to Bootstrap modal classes

# Pattern to find files still using reactstrap Modal
grep -l "import.*Modal.*from 'reactstrap'" frontend/src/components/dialog/*.js | while read file; do
    echo "TODO: $file"
done
```

### Manual Migration Steps (Per Dialog)

1. **Update imports** - Remove Modal components:
```javascript
// BEFORE:
import { Button, Modal, ModalHeader, ModalBody, ModalFooter } from 'reactstrap';

// AFTER:
import { Button } from 'reactstrap';
```

2. **Update render()** - Use Bootstrap classes:
```jsx
// BEFORE:
<Modal isOpen={true} toggle={this.toggle}>
  <ModalHeader toggle={this.toggle}>Title</ModalHeader>
  <ModalBody>Content</ModalBody>
  <ModalFooter>
    <Button onClick={this.toggle}>Cancel</Button>
    <Button onClick={this.handleSubmit}>Submit</Button>
  </ModalFooter>
</Modal>

// AFTER:
<div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
  <div className="modal-dialog modal-dialog-centered">
    <div className="modal-content">
      <div className="modal-header">
        <h5 className="modal-title">Title</h5>
        <button type="button" className="btn-close" onClick={this.toggle} aria-label="Close"></button>
      </div>
      <div className="modal-body">Content</div>
      <div className="modal-footer">
        <Button color="secondary" onClick={this.toggle}>Cancel</Button>
        <Button color="primary" onClick={this.handleSubmit}>Submit</Button>
      </div>
    </div>
  </div>
</div>
```

### Incremental Approach
Fix dialogs as users encounter them:
1. User reports "X button doesn't work"
2. Check if dialog uses reactstrap Modal
3. Apply the fix pattern
4. Rebuild and test

### Automated Detection
Add to CI/CD or pre-commit hook:
```bash
# Warn about unfixed dialogs
BROKEN=$(grep -l "import.*Modal.*from 'reactstrap'" frontend/src/components/dialog/*.js | wc -l)
if [ "$BROKEN" -gt "0" ]; then
    echo "Warning: $BROKEN dialogs may not render correctly"
fi
```

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
| Logout button | ❌ Not working | Needs `/api2/auth/logout/` endpoint or frontend fix |
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
| OnlyOffice (docx, xlsx, pptx) | ✅ Working | Full editing support |
| Images (jpg, png, etc.) | ✅ Working | Via `/repo/:id/raw/*path` |
| PDF viewer | ❌ Not implemented | Falls back to download |
| Video/Audio player | ❌ Not implemented | Falls back to download |
| Thumbnails | ❌ Not implemented | `/thumbnail/` endpoint missing |

### Library Settings Dialogs
| Dialog | Status | Notes |
|--------|--------|-------|
| History settings | ⚠️ Stub | Returns default values |
| Auto-delete settings | ⚠️ Stub | Returns default values |
| API tokens | ⚠️ Stub | Returns empty list |
| Transfer ownership | ❌ Not implemented | Dialog shows but no backend |

---

## Summary: Action Items

### Immediate (This Week)
- [ ] **Fix serviceURL** - Change to use `window.location.origin` (5 min)
- [ ] **Document modal pattern** in CLAUDE.md (done)

### Short-Term (As Encountered)
- [ ] Fix dialogs as users report issues
- [ ] Add tests when fixing bugs

### Long-Term (Backlog)
- [ ] Migrate all ~100 dialogs (can be automated with script)
- [ ] Increase test coverage to 40%
- [ ] Consider forking seafile-js for any customization needs

---

## Monitoring Technical Debt

### Commands
```bash
# Count broken modals
grep -l "import.*Modal.*from 'reactstrap'" frontend/src/components/dialog/*.js | wc -l

# Check test coverage
go test ./... -coverprofile=coverage.out && go tool cover -func=coverage.out | grep total

# Find hardcoded URLs
grep -r "localhost:8080" frontend/src/
```

### Metrics to Track
| Metric | Current | Target | How to Check |
|--------|---------|--------|--------------|
| Broken dialogs | ~96 | 0 | grep for reactstrap Modal |
| Test coverage | 25% | 40% | go test -cover |
| Hardcoded URLs | 1 | 0 | grep localhost |

---

*Last updated: 2026-01-07*
