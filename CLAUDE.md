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
| [docs/LICENSING.md](docs/LICENSING.md) | Legal considerations for Seafile compatibility |

## External References

| Resource | URL |
|----------|-----|
| Seafile API Docs (New) | https://seafile-api.readme.io/ |
| Seafile Manual - API Index | https://manual.seafile.com/latest/develop/web_api_v2.1/ |
| Seafile Server Source (upload-file.c) | https://github.com/haiwen/seafile-server/blob/master/server/upload-file.c |
| seafile-js (frontend API client) | https://github.com/haiwen/seafile-js |
| Seafile Client (resumable upload) | https://github.com/haiwen/seafile-client/blob/master/src/filebrowser/reliable-upload.cpp |

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
    serviceURL: 'http://localhost:8080',  // Backend API
    mediaUrl: '/static/',                  // Icons/assets base
    siteRoot: '/',                         // App root
    username: 'user@sesamefs.local',       // Logged in user
  }
};
```
**Constants file**: `src/utils/constants.js` exports these values.

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

### Frontend Debugging Checklist

| Symptom | Check First | Likely Fix |
|---------|-------------|------------|
| Icons not loading | Network tab for 404s | Add missing icon file, hard refresh |
| API call fails | Network tab request/response | Check backend returns exact format |
| Button click does nothing | Console for errors | Check handler is bound, state flows |
| Changes not appearing | Docker build time (<10s = cached) | `docker-compose build --no-cache frontend` |
| "Invalid token" | Request headers | Must be `Token xyz` not `Bearer xyz` |
| Component not updating | React DevTools state | Check callback updates parent state |
| **Modal not visible** | Console shows "mounted" | **Remove `ModalPortal` wrapper** - reactstrap Modal handles portaling internally |

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
