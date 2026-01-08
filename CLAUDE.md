# SesameFS - Project Context for Claude

## What is SesameFS?

A Seafile-compatible cloud storage API with modern internals (Go, Cassandra, S3).

## Critical Constraints

1. **Seafile desktop/mobile client chunking cannot be changed** - compiled into apps (Rabin CDC, 256KB-4MB, SHA-1)
2. **SHA-1→SHA-256 translation for sync protocol only** - Desktop/mobile clients use `/seafhttp/` with SHA-1 block IDs; server translates to SHA-256 for storage. Web frontend uses REST API with server-side SHA-256 chunking.
3. **Block size for web/API**: 2-256MB (server-controlled, adaptive FastCDC)
4. **SpillBuffer threshold**: 16MB (memory below, temp file above)

### Upload Paths

| Client | Protocol | Chunking | Block Hash |
|--------|----------|----------|------------|
| Desktop/Mobile | `/seafhttp/` (sync) | Client-side Rabin CDC | SHA-1 → translated to SHA-256 |
| Web Frontend | REST API | Server-side FastCDC | SHA-256 (no translation) |
| API clients | REST API | Server-side FastCDC | SHA-256 (no translation) |

## Key Code Locations

| Feature | File |
|---------|------|
| Seafile sync protocol | `internal/api/sync.go` |
| File upload/download | `internal/api/seafhttp.go` |
| S3 storage backend | `internal/storage/s3.go` |
| Block storage | `internal/storage/blocks.go` |
| Multi-backend manager | `internal/storage/storage.go` |
| FastCDC chunking | `internal/chunker/fastcdc.go` |
| Adaptive chunking | `internal/chunker/adaptive.go` |
| Database schema | `internal/db/db.go` |
| API v2 handlers | `internal/api/v2/*.go` |
| Configuration | `internal/config/config.go` |

## Documentation

| Document | Contents |
|----------|----------|
| [README.md](README.md) | Quick start, features overview, roadmap |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Design decisions, storage architecture, database schema |
| [docs/API-REFERENCE.md](docs/API-REFERENCE.md) | API endpoints, implementation status, compatibility |
| [docs/DATABASE-GUIDE.md](docs/DATABASE-GUIDE.md) | Cassandra tables, examples, consistency |
| [docs/FRONTEND.md](docs/FRONTEND.md) | React frontend: setup, patterns, Docker, troubleshooting |
| [docs/TESTING.md](docs/TESTING.md) | Test coverage, benchmarks, running tests |
| [docs/TECHNICAL-DEBT.md](docs/TECHNICAL-DEBT.md) | Known issues, migration plans, modal pattern fixes |
| [docs/LICENSING.md](docs/LICENSING.md) | Legal considerations for Seafile compatibility |

## External References

| Resource | URL |
|----------|-----|
| Seafile API Docs (New) | https://seafile-api.readme.io/ |
| Seafile Manual - API Index | https://manual.seafile.com/latest/develop/web_api_v2.1/ |
| Seafile Server Source (upload-file.c) | https://github.com/haiwen/seafile-server/blob/master/server/upload-file.c |
| seafile-js (frontend API client) | https://github.com/haiwen/seafile-js |
| Seafile Client (resumable upload) | https://github.com/haiwen/seafile-client/blob/master/src/filebrowser/reliable-upload.cpp |

## Recent Changes (2026-01-08)

### Library Starring Fix
**Issue**: Starred libraries weren't persisting after page refresh
**Root Cause**: Cassandra query in `ListLibrariesV21` was invalid - couldn't filter by `path` without also filtering by `repo_id` (clustering key order)
**Fix**: Query all starred items for user, filter by `path="/"` in Go code
```go
// Query all starred files for user, filter for libraries (path="/")
starIter := h.db.Session().Query(`
    SELECT repo_id, path FROM starred_files WHERE user_id = ?
`, userID).Iter()
for starIter.Scan(&starredRepoID, &starredPath) {
    if starredPath == "/" {
        starredLibs[starredRepoID] = true
    }
}
```
**File**: `internal/api/v2/libraries.go:678-693`

### OnlyOffice Simplified Config
**Issue**: OnlyOffice documents opened in view-only mode (toolbar grayed out)
**Fix**: Simplified OnlyOffice config to match Seahub's minimal approach:
- Reduced customization to only `forcesave` and `submitForm`
- Added `fillForms: true` to permissions
- URL translation for Docker networking (`localhost:8088` → `onlyoffice:80`)
**Files**: `internal/api/v2/onlyoffice.go`, `internal/config/config.go`

### Multi-host Frontend Support
**Issue**: Frontend hardcoded to single backend URL
**Fix**: Empty `serviceURL` config uses `window.location.origin` automatically
```javascript
window.app.config.serviceURL = '';  // Uses window.location.origin
```
**File**: `frontend/public/index.html`

### Modal Dialog Fixes
Fixed dialogs to use plain Bootstrap modal classes instead of reactstrap Modal:
- `rename-dialog.js` - Rename (wiki context)
- `rename-dirent.js` - Rename file/folder

## Quick Commands

```bash
# Run tests
go test ./...

# Run with coverage
go test ./... -coverprofile=coverage.out

# Start dev server
go run cmd/sesamefs/main.go

# Docker compose
docker-compose up -d
```

## Frontend Development

**Full guide**: [docs/FRONTEND.md](docs/FRONTEND.md) - Complete setup, patterns, Docker, troubleshooting

### Quick Reference

```bash
# Docker build caching fix (if changes don't appear)
docker-compose stop frontend && docker-compose rm -f frontend && docker rmi cool-storage-api-frontend
docker-compose build --no-cache frontend
docker-compose up -d frontend

# Local dev (faster iteration)
cd frontend && npm install && npm start  # runs on port 3001
```

### Key Files

| File | Purpose |
|------|---------|
| `frontend/src/models/dirent.js` | Parses API response (is_locked, file_tags, etc.) |
| `frontend/src/components/dirent-list-view/` | Directory listing, file rows, lock icons |
| `frontend/src/components/dialog/` | Modal dialogs (share, rename, tags) |
| `frontend/src/utils/seafile-api.js` | API client wrapper |
| `frontend/src/css/dirent-list-item.css` | File row styling, lock icon positioning |

### Adding New File Properties

1. **Backend**: Add to `Dirent` struct in `internal/api/v2/files.go`
2. **Frontend model**: Parse in `src/models/dirent.js` constructor
3. **Component**: Render: `{dirent.property && <Component/>}`

---

## Frontend Critical Context

> **Source of Truth**: This section consolidates key info from [docs/FRONTEND.md](docs/FRONTEND.md)

### Architecture & Data Flow
```
User Action → Component Handler → seafile-api.js → Backend API
                                        ↓
Component Render ← React State ← Dirent Model ← API Response
```

### Global Configuration (CHECK FIRST)
Frontend reads from `window.app.config` in `public/index.html`:
```javascript
window.app = {
  config: {
    serviceURL: '',  // Empty = use window.location.origin (multi-host support)
    mediaUrl: '/static/',                  // Icons/assets base
    siteRoot: '/',                         // App root
    fileServerRoot: window.location.origin + '/seafhttp',  // File server
  }
};
```
**Constants file**: `src/utils/constants.js` exports these values.

**Multi-host deployment**: `serviceURL` is empty by default. The `seafile-api.js` client uses `window.location.origin` when serviceURL is empty, allowing the same frontend build to work on us.sesamefs.com, eu.sesamefs.com, etc.

For local dev with different ports (frontend on 3001, backend on 8080):
```javascript
window.SESAMEFS_API_URL = 'http://localhost:8080';
```

### Icon Path Patterns (COMMON ISSUE SOURCE)
| Asset | URL Pattern | Files Needed |
|-------|-------------|--------------|
| Folder | `{mediaUrl}img/folder-{24\|192}.png` | `folder-24.png`, `folder-192.png` |
| Folder (read-only) | `{mediaUrl}img/folder-read-only-{24\|192}.png` | Same with `-read-only` |
| File types | `{mediaUrl}img/file/{24\|192}/{ext}.png` | `pdf.png`, `excel.png`, etc. |
| Libraries | `{mediaUrl}img/lib/{24\|48\|256}/{type}.png` | `lib.png`, `lib-readonly.png` |
| Lock overlay | `{mediaUrl}img/file-locked-32.png` | Single file |

**HiDPI Logic** (`utils.js`): `isHiDPI() ? 48 : 24` → then `size > 24 ? 192 : 24`
- Normal: requests 24px icons
- Retina: requests 192px icons (48→192 mapping)

### API Client: seafile-js (CANNOT MODIFY)
The `seafile-js` npm package has **hardcoded paths**. Backend MUST match:

| Frontend Call | HTTP Request |
|---------------|--------------|
| `seafileAPI.deleteRepo(id)` | `DELETE /api/v2.1/repos/{id}/` |
| `seafileAPI.listRepos()` | `GET /api/v2.1/repos/` |
| `seafileAPI.renameRepo(id, name)` | `POST /api2/repos/{id}/?op=rename` |
| `seafileAPI.listDir(repoId, path)` | `GET /api/v2.1/repos/{id}/dir/?p={path}` |
| `seafileAPI.lockfile(repoId, path)` | `PUT /api/v2.1/repos/{id}/file/?p={path}` + `operation=lock` |

### Token Authentication
```javascript
// Stored in localStorage
const TOKEN_KEY = 'sesamefs_auth_token';

// All API requests use:
headers: { 'Authorization': 'Token ' + token }  // NOT "Bearer"
```

### Component Data Flow Example: Delete Library
```
1. User clicks trash icon on library row
   → MylibRepoListItem.onDeleteToggle()
   → sets state.isDeleteDialogShow = true

2. DeleteRepoDialog renders (modal)
   → componentDidMount fetches share info

3. User clicks Delete button
   → DeleteRepoDialog.onDeleteRepo()
   → calls this.props.onDeleteRepo(repo)

4. Parent handler executes
   → MylibRepoListItem.onDeleteRepo(repo)
   → seafileAPI.deleteRepo(repo.repo_id)

5. On success:
   → this.props.onDeleteRepo(repo) notifies grandparent
   → list re-renders without deleted item
```

### Required Backend Response Formats

**Library List** (`GET /api/v2.1/repos/`):
```json
{ "repos": [{ "repo_id": "uuid", "repo_name": "str", "type": "mine", "permission": "rw" }] }
```

**Directory List** (`GET /api/v2.1/repos/{id}/dir/?p=/`):
```json
{ "dirent_list": [{ "name": "str", "type": "file|dir", "mtime": 123, "permission": "rw" }] }
```

**Delete Success** (`DELETE /api/v2.1/repos/{id}/`):
```json
{ "success": true }
```

### Modal Pattern (CRITICAL - Common Bug Source)

**Problem**: Seafile frontend uses `ModalPortal` wrapper for dialog rendering. The reactstrap `<Modal>` component does NOT render properly inside `ModalPortal` because reactstrap Modal creates its own portal, resulting in double-portal issues.

**Solution**: Use plain Bootstrap modal classes instead of reactstrap Modal:

```jsx
// ❌ BROKEN - reactstrap Modal inside ModalPortal
import { Modal, ModalHeader, ModalBody, ModalFooter } from 'reactstrap';
<Modal isOpen={true} toggle={this.toggle}>
  <ModalHeader>Title</ModalHeader>
  <ModalBody>Content</ModalBody>
  <ModalFooter>Buttons</ModalFooter>
</Modal>

// ✅ WORKING - plain Bootstrap modal classes
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

**Files already fixed** (using plain Bootstrap modal classes):

| Dialog | Purpose | Status |
|--------|---------|--------|
| `delete-repo-dialog.js` | Library deletion | ✅ Fixed |
| `create-repo-dialog.js` | New library creation | ✅ Fixed |
| `batch-delete-repo-dialog.js` | Batch library deletion | ✅ Fixed |
| `delete-folder-dialog.js` | Folder deletion | ✅ Fixed |
| `create-folder-dialog.js` | Create folder | ✅ Fixed |
| `create-file-dialog.js` | Create file | ✅ Fixed |
| `rename-dialog.js` | Rename (wiki context) | ✅ Fixed |
| `rename-dirent.js` | Rename file/folder | ✅ Fixed |
| `share-dialog.js` | Share files/folders | ✅ Fixed |
| `copy-dirent-dialog.js` | Copy file/folder | ✅ Fixed |
| `move-dirent-dialog.js` | Move file/folder | ✅ Fixed |

**Note**: You can still use reactstrap `Button`, `Form`, `Input`, `Alert` etc. inside the modal body - just not the Modal wrapper components.

---

## Pending Modal Dialog Fixes

> **IMPORTANT**: ~100+ dialog files still use reactstrap Modal and need migration.
> Run this to find them: `grep -l "import.*Modal.*from 'reactstrap'" frontend/src/components/dialog/**/*.js`

### Priority 1: User-Facing Dialogs (Fix First)
These dialogs are encountered by regular users:

| Dialog | Purpose | File |
|--------|---------|------|
| `lib-decrypt-dialog.js` | Decrypt encrypted library | `dialog/` |
| `change-repo-password-dialog.js` | Change library password | `dialog/` |
| `create-group-dialog.js` | Create new group | `dialog/` |
| `rename-group-dialog.js` | Rename group | `dialog/` |
| `leave-group-dialog.js` | Leave a group | `dialog/` |
| `dismiss-group-dialog.js` | Dismiss/delete group | `dialog/` |
| `transfer-group-dialog.js` | Transfer group ownership | `dialog/` |
| `manage-members-dialog.js` | Manage group members | `dialog/` |
| `create-tag-dialog.js` | Create file tag | `dialog/` |
| `edit-filetag-dialog.js` | Edit file tags | `dialog/` |
| `list-taggedfiles-dialog.js` | List files with tag | `dialog/` |
| `invite-people-dialog.js` | Invite users | `dialog/` |
| `about-dialog.js` | About dialog | `dialog/` |
| `clean-trash.js` | Clean trash | `dialog/` |
| `confirm-restore-repo.js` | Restore deleted repo | `dialog/` |
| `upload-remind-dialog.js` | Upload reminder | `dialog/` |
| `zip-download-dialog.js` | ZIP download progress | `dialog/` |
| `search-file-dialog.js` | Search files | `dialog/` |
| `internal-link-dialog.js` | Internal link | `dialog/` |

### Priority 2: Share & Link Dialogs
| Dialog | Purpose |
|--------|---------|
| `share-repo-dialog.js` | Share repository |
| `share-admin-link.js` | Admin share link |
| `view-link-dialog.js` | View share link |
| `generate-upload-link.js` | Generate upload link |
| `share-to-user.js` | Share to user |
| `share-to-group.js` | Share to group |
| `share-to-invite-people.js` | Share invite |
| `share-to-other-server.js` | OCM sharing |
| `repo-share-admin-dialog.js` | Share admin |

### Priority 3: Library Settings Dialogs
| Dialog | Purpose |
|--------|---------|
| `lib-history-setting-dialog.js` | History settings |
| `lib-old-files-auto-del-dialog.js` | Auto-delete old files |
| `lib-sub-folder-permission-dialog.js` | Subfolder permissions |
| `lib-sub-folder-set-user-permission-dialog.js` | User folder perms |
| `lib-sub-folder-set-group-permission-dialog.js` | Group folder perms |
| `reset-encrypted-repo-password-dialog.js` | Reset encrypted password |
| `repo-api-token-dialog.js` | API token management |
| `repo-seatable-integration-dialog.js` | SeaTable integration |
| `label-repo-state-dialog.js` | Label repo state |
| `edit-repo-commit-labels.js` | Edit commit labels |

### Priority 4: Organization Admin Dialogs (`dialog/`)
| Dialog | Purpose |
|--------|---------|
| `org-add-user-dialog.js` | Add org user |
| `org-add-member-dialog.js` | Add org member |
| `org-add-admin-dialog.js` | Add org admin |
| `org-add-department-dialog.js` | Add department |
| `org-add-repo-dialog.js` | Add org repo |
| `org-delete-member-dialog.js` | Delete member |
| `org-delete-department-dialog.js` | Delete department |
| `org-delete-repo-dialog.js` | Delete org repo |
| `org-rename-department-dialog.js` | Rename department |
| `org-set-group-quota-dialog.js` | Set group quota |
| `org-import-users-dialog.js` | Import users |
| `org-admin-invite-user-dialog.js` | Invite user |
| `org-admin-invite-user-via-weixin-dialog.js` | WeChat invite |
| `org-logs-file-update-detail.js` | File update logs |
| `set-org-user-quota.js` | Set user quota |
| `set-org-user-name.js` | Set user name |
| `set-org-user-contact-email.js` | Set contact email |

### Priority 5: System Admin Dialogs (`dialog/sysadmin-dialog/`)
All files in `frontend/src/components/dialog/sysadmin-dialog/` need fixing:
- `sysadmin-add-user-dialog.js`
- `sysadmin-delete-member-dialog.js`
- `sysadmin-delete-repo-dialog.js`
- `sysadmin-create-repo-dialog.js`
- `sysadmin-share-dialog.js`
- `sysadmin-import-user-dialog.js`
- `sysadmin-logs-export-excel-dialog.js`
- ... and ~15 more files

### Priority 6: Other/Rare Dialogs
| Dialog | Purpose |
|--------|---------|
| `wiki-delete-dialog.js` | Delete wiki |
| `wiki-select-dialog.js` | Select wiki |
| `new-wiki-dialog.js` | New wiki |
| `import-members-dialog.js` | Import members |
| `import-dingtalk-department-dialog.js` | DingTalk import |
| `import-work-weixin-department-dialog.js` | WeChat Work import |
| `confirm-disconnect-dingtalk.js` | Disconnect DingTalk |
| `confirm-disconnect-wechat.js` | Disconnect WeChat |
| `confirm-delete-account.js` | Delete account |
| `confirm-unlink-device.js` | Unlink device |
| `confirm-apply-folder-properties-dialog.js` | Apply folder props |
| `terms-editor-dialog.js` | Terms editor |
| `terms-preview-dialog.js` | Terms preview |
| `guide-for-new-dialog.js` | New user guide |
| `add-abuse-report-dialog.js` | Abuse report |
| `invitation-revoke-dialog.js` | Revoke invitation |
| `set-webdav-password.js` | Set WebDAV password |
| `reset-webdav-password.js` | Reset WebDAV password |
| `remove-webdav-password.js` | Remove WebDAV password |
| `copy-move-dirent-progress-dialog.js` | Copy/move progress |
| `insert-file-dialog.js` | Insert file |
| `insert-repo-image-dialog.js` | Insert repo image |
| `list-created-files-dialog.js` | List created files |
| `save-shared-file-dialog.js` | Save shared file |
| `save-shared-dir-dialog.js` | Save shared dir |
| `transfer-dialog.js` | Transfer ownership |
| `common-operation-confirmation-dialog.js` | Generic confirm |
| `create-department-repo-dialog.js` | Dept repo |
| `extra-attributes-dialog/index.js` | Extra attributes |

### How to Fix a Dialog

1. **Change import** - Remove Modal components:
```jsx
// Before
import { Button, Modal, ModalHeader, ModalBody, ModalFooter, Alert } from 'reactstrap';
// After
import { Button, Alert } from 'reactstrap';
```

2. **Replace render** - Use plain Bootstrap classes:
```jsx
// Before
<Modal isOpen={true} toggle={this.toggle}>
  <ModalHeader toggle={this.toggle}>Title</ModalHeader>
  <ModalBody>Content</ModalBody>
  <ModalFooter>
    <Button onClick={this.toggle}>Cancel</Button>
  </ModalFooter>
</Modal>

// After
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
      </div>
    </div>
  </div>
</div>
```

3. **Handle onOpened callback** - If dialog uses `onOpened` for focus:
```jsx
componentDidMount() {
  // Focus input after mount (replaces Modal's onOpened)
  setTimeout(() => {
    if (this.inputRef.current) {
      this.inputRef.current.focus();
    }
  }, 100);
}
```

4. **Test the dialog** - Verify it opens, closes, and submits correctly

### Frontend Debugging Checklist

| Symptom | Check First | Likely Fix |
|---------|-------------|------------|
| Icons not loading | Network tab for 404s | Add missing icon file, hard refresh |
| API call fails | Network tab request/response | Check backend returns exact format |
| Button click does nothing | Console for errors | Check handler is bound, state flows |
| Changes not appearing | Docker build time (<10s = cached) | `docker-compose build --no-cache frontend` |
| "Invalid token" | Request headers | Must be `Token xyz` not `Bearer xyz` |
| Component not updating | React DevTools state | Check callback updates parent state |
| **Modal not visible** | Check if using reactstrap Modal | **Use plain Bootstrap modal classes** (see pattern above) |

### Key Files Quick Reference

| What | File |
|------|------|
| API wrapper | `src/utils/seafile-api.js` |
| Config constants | `src/utils/constants.js` |
| Icon URL logic | `src/utils/utils.js` → `getDirentIcon()`, `getFolderIconUrl()` |
| File/folder model | `src/models/dirent.js` |
| Directory list | `src/components/dirent-list-view/dirent-list-item.js` |
| Library list | `src/pages/my-libs/mylib-repo-list-item.js` |
| Delete library dialog | `src/components/dialog/delete-repo-dialog.js` |
| Global config | `public/index.html` → `window.app.config` |
