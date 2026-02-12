import { SeafileAPI } from 'seafile-js';
import { siteRoot, serviceURL } from './constants';

const TOKEN_KEY = 'sesamefs_auth_token';

// Login bypass for testing - set REACT_APP_BYPASS_LOGIN=true to skip login
// When enabled, uses 'dev-token-admin' which the backend accepts in dev mode
const BYPASS_LOGIN = process.env.REACT_APP_BYPASS_LOGIN === 'true';
const BYPASS_TOKEN = 'dev-token-admin'; // Default admin token for testing

let seafileAPI = new SeafileAPI();

// Initialize with token from localStorage if available
function initAPI() {
  let token = localStorage.getItem(TOKEN_KEY);
  const server = serviceURL || window.location.origin;

  // If bypass is enabled and no token stored, use the bypass token
  if (BYPASS_LOGIN && !token) {
    token = BYPASS_TOKEN;
    localStorage.setItem(TOKEN_KEY, token);
    console.log('[SesameFS] Login bypass enabled - using dev-token-admin');
  }

  if (token) {
    // Token-based authentication for SesameFS
    seafileAPI.init({ server, token });
  } else {
    // Initialize without auth for anonymous access (e.g. share link views)
    // Use initForSeahubUsage which creates this.req without requiring a token
    seafileAPI.initForSeahubUsage({ siteRoot: server + '/', xcsrfHeaders: '' });
  }
}

// Check if user is authenticated
function isAuthenticated() {
  // If bypass enabled, always return true (initAPI will set the token)
  if (BYPASS_LOGIN) {
    return true;
  }
  return !!localStorage.getItem(TOKEN_KEY);
}

// Login and store token
async function login(username, password) {
  const server = serviceURL || window.location.origin;

  // Call the auth-token endpoint
  const response = await fetch(`${server}/api2/auth-token/`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/x-www-form-urlencoded',
    },
    body: new URLSearchParams({
      username: username,
      password: password,
    }),
  });

  if (!response.ok) {
    const error = await response.json().catch(() => ({}));
    // Handle non_field_errors as either string or array (Seafile compatibility)
    let errorMsg = 'Login failed';
    if (error.non_field_errors) {
      errorMsg = Array.isArray(error.non_field_errors)
        ? error.non_field_errors[0]
        : error.non_field_errors;
    } else if (error.detail) {
      errorMsg = error.detail;
    } else if (error.error) {
      errorMsg = error.error;
    }
    throw new Error(errorMsg);
  }

  const data = await response.json();

  if (data.token) {
    localStorage.setItem(TOKEN_KEY, data.token);
    // Reinitialize API with the new token
    seafileAPI.init({ server, token: data.token });
    return data;
  }

  throw new Error('No token received');
}

// Logout - clear token and redirect to OIDC logout if available
async function logout() {
  const server = serviceURL || window.location.origin;

  try {
    // Try to get the OIDC logout URL for single logout
    const response = await fetch(server + '/api/v2.1/auth/oidc/logout/');
    if (response.ok) {
      const data = await response.json();
      // Clear local token first
      localStorage.removeItem(TOKEN_KEY);

      if (data.logout_url) {
        // Redirect to OIDC provider's logout endpoint for single logout
        // This will clear the SSO session and redirect back to our login page
        window.location.href = data.logout_url;
        return;
      }
    }
  } catch (err) {
    // If OIDC logout fails, fall back to local logout
    console.log('[SesameFS] OIDC logout not available, using local logout');
  }

  // Fallback: just clear local token and redirect to login
  localStorage.removeItem(TOKEN_KEY);
  window.location.href = '/login/';
}

// Get stored token
function getToken() {
  return localStorage.getItem(TOKEN_KEY);
}

// Set auth token (used after OIDC login)
function setAuthToken(token) {
  const server = serviceURL || window.location.origin;
  localStorage.setItem(TOKEN_KEY, token);
  seafileAPI.init({ server, token });
}

// Initialize on load
initAPI();

// ============================================================================
// OIDC API methods - for SSO authentication
// These use fetch directly because they're called before user is authenticated
// ============================================================================

// Get OIDC configuration (public endpoint)
seafileAPI.getOIDCConfig = async function () {
  const server = this.server || serviceURL || window.location.origin;
  const url = server + '/api/v2.1/auth/oidc/config/';
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error('Failed to get OIDC config');
  }
  return { data: await response.json() };
};

// Get OIDC login URL
seafileAPI.getOIDCLoginURL = async function (redirectURI, returnURL) {
  const server = this.server || serviceURL || window.location.origin;
  let url = server + '/api/v2.1/auth/oidc/login/';
  const params = new URLSearchParams();
  if (redirectURI) params.set('redirect_uri', redirectURI);
  if (returnURL) params.set('return_url', returnURL);
  if (params.toString()) url += '?' + params.toString();
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error('Failed to get OIDC login URL');
  }
  return { data: await response.json() };
};

// Exchange OIDC authorization code for tokens
seafileAPI.exchangeOIDCCode = async function (code, state, redirectURI) {
  const server = this.server || serviceURL || window.location.origin;
  const url = server + '/api/v2.1/auth/oidc/callback/';
  const response = await fetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ code, state, redirect_uri: redirectURI }),
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({}));
    throw { response: { data: error } };
  }
  return { data: await response.json() };
};

// Get OIDC logout URL for single logout
seafileAPI.getOIDCLogoutURL = async function (postLogoutRedirectURI) {
  const server = this.server || serviceURL || window.location.origin;
  let url = server + '/api/v2.1/auth/oidc/logout/';
  if (postLogoutRedirectURI) {
    url += '?post_logout_redirect_uri=' + encodeURIComponent(postLogoutRedirectURI);
  }
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error('Failed to get OIDC logout URL');
  }
  return { data: await response.json() };
};

// ============================================================================
// Tag API methods - not in upstream seafile-js, added for SesameFS
// ============================================================================

// List all tags for a repository
seafileAPI.listRepoTags = function (repoID) {
  const url = this.server + '/api/v2.1/repos/' + repoID + '/repo-tags/';
  return this.req.get(url);
};

// Create a new tag in a repository
seafileAPI.createRepoTag = function (repoID, name, color) {
  const url = this.server + '/api/v2.1/repos/' + repoID + '/repo-tags/';
  const data = { name, color };
  return this.req.post(url, data);
};

// Update a tag
seafileAPI.updateRepoTag = function (repoID, tagID, name, color) {
  const url = this.server + '/api/v2.1/repos/' + repoID + '/repo-tags/' + tagID + '/';
  const data = { name, color };
  return this.req.put(url, data);
};

// Delete a tag
seafileAPI.deleteRepoTag = function (repoID, tagID) {
  const url = this.server + '/api/v2.1/repos/' + repoID + '/repo-tags/' + tagID + '/';
  return this.req.delete(url);
};

// Get tags for a specific file
seafileAPI.getFileTags = function (repoID, filePath) {
  const url = this.server + '/api/v2.1/repos/' + repoID + '/file-tags/?file_path=' + encodeURIComponent(filePath);
  return this.req.get(url);
};

// Add a tag to a file
seafileAPI.addFileTag = function (repoID, filePath, repoTagID) {
  const url = this.server + '/api/v2.1/repos/' + repoID + '/file-tags/';
  const data = { file_path: filePath, repo_tag_id: repoTagID };
  return this.req.post(url, data);
};

// Remove a tag from a file
seafileAPI.deleteFileTag = function (repoID, fileTagID) {
  const url = this.server + '/api/v2.1/repos/' + repoID + '/file-tags/' + fileTagID + '/';
  return this.req.delete(url);
};

// List all files with a specific tag
seafileAPI.listTaggedFiles = function (repoID, tagID) {
  const url = this.server + '/api/v2.1/repos/' + repoID + '/tagged-files/' + tagID + '/';
  return this.req.get(url);
};

// List tagged files for share link
seafileAPI.getShareLinkTaggedFiles = function (shareLinkToken, tagID) {
  const url = this.server + '/api/v2.1/share-links/' + shareLinkToken + '/tagged-files/' + tagID + '/';
  return this.req.get(url);
};

// Copy/move with conflict resolution policy
seafileAPI.copyDirWithPolicy = function (repoID, dstRepoID, dstPath, srcDir, dirents, conflictPolicy) {
  let paths = Array.isArray(dirents) ? dirents : [dirents];
  let url = this.server;
  url += repoID === dstRepoID ? '/api/v2.1/repos/sync-batch-copy-item/' : '/api/v2.1/repos/async-batch-copy-item/';
  let data = {
    'src_repo_id': repoID,
    'src_parent_dir': srcDir,
    'dst_repo_id': dstRepoID,
    'dst_parent_dir': dstPath,
    'src_dirents': paths,
    'conflict_policy': conflictPolicy,
  };
  return this._sendPostRequest(url, data, { headers: { 'Content-Type': 'application/json' } });
};

seafileAPI.moveDirWithPolicy = function (repoID, dstRepoID, dstPath, srcDir, dirents, conflictPolicy) {
  let paths = Array.isArray(dirents) ? dirents : [dirents];
  let url = this.server;
  url += repoID === dstRepoID ? '/api/v2.1/repos/sync-batch-move-item/' : '/api/v2.1/repos/async-batch-move-item/';
  let data = {
    'src_repo_id': repoID,
    'src_parent_dir': srcDir,
    'dst_repo_id': dstRepoID,
    'dst_parent_dir': dstPath,
    'src_dirents': paths,
    'conflict_policy': conflictPolicy,
  };
  return this._sendPostRequest(url, data, { headers: { 'Content-Type': 'application/json' } });
};

// ============================================================================
// File/Folder Trash (Recycle Bin) API methods
// ============================================================================

seafileAPI.getRepoFolderTrash = function (repoID, path, scanStat) {
  let url = this.server + '/api/v2.1/repos/' + repoID + '/trash/';
  const params = new URLSearchParams();
  if (path) params.set('parent_dir', path);
  if (scanStat) params.set('scan_stat', scanStat);
  if (params.toString()) url += '?' + params.toString();
  return this.req.get(url);
};

seafileAPI.deleteRepoTrash = function (repoID, keepDays) {
  let url = this.server + '/api/v2.1/repos/' + repoID + '/trash/';
  if (keepDays !== undefined) url += '?keep_days=' + keepDays;
  return this.req.delete(url);
};

seafileAPI.restoreFile = function (repoID, commitID, path) {
  let url = this.server + '/api/v2.1/repos/' + repoID + '/file/restore/';
  let data = new FormData();
  data.append('commit_id', commitID);
  data.append('p', path);
  return this.req.post(url, data);
};

seafileAPI.restoreFolder = function (repoID, commitID, path) {
  let url = this.server + '/api/v2.1/repos/' + repoID + '/dir/restore/';
  let data = new FormData();
  data.append('commit_id', commitID);
  data.append('p', path);
  return this.req.post(url, data);
};

seafileAPI.listCommitDir = function (repoID, commitID, path) {
  let url = this.server + '/api/v2.1/repos/' + repoID + '/commit/' + commitID + '/dir/';
  if (path) url += '?p=' + encodeURIComponent(path);
  return this.req.get(url);
};

// ============================================================================
// Deleted Libraries (Library Recycle Bin) API methods
// ============================================================================

seafileAPI.listDeletedRepo = function () {
  let url = this.server + '/api/v2.1/deleted-repos/';
  return this.req.get(url);
};

seafileAPI.restoreDeletedRepo = function (repoID) {
  let url = this.server + '/api/v2.1/repos/deleted/' + repoID + '/';
  return this.req.put(url);
};

// ============================================================================
// Admin Library Management API methods
// ============================================================================

// Admin: list all libraries (paginated, sortable)
seafileAPI.sysAdminListAllRepos = function (page, perPage, sortBy) {
  let url = this.server + '/api/v2.1/admin/libraries/';
  const params = new URLSearchParams();
  if (page) params.set('page', page);
  if (perPage) params.set('per_page', perPage);
  if (sortBy) params.set('order_by', sortBy);
  if (params.toString()) url += '?' + params.toString();
  return this.req.get(url);
};

// Admin: search libraries by name or ID
seafileAPI.sysAdminSearchRepos = function (name, page, perPage) {
  let url = this.server + '/api/v2.1/admin/search-libraries/';
  const params = new URLSearchParams();
  if (name) params.set('name_or_id', name);
  if (page) params.set('page', page);
  if (perPage) params.set('per_page', perPage);
  if (params.toString()) url += '?' + params.toString();
  return this.req.get(url);
};

// Admin: create a new library
seafileAPI.sysAdminCreateRepo = function (repoName, owner) {
  let url = this.server + '/api/v2.1/admin/libraries/';
  let data = { name: repoName, owner: owner };
  return this.req.post(url, data);
};

// Admin: get library info
seafileAPI.sysAdminGetRepoInfo = function (repoID) {
  let url = this.server + '/api/v2.1/admin/libraries/' + repoID + '/';
  return this.req.get(url);
};

// Admin: delete a library (soft-delete)
seafileAPI.sysAdminDeleteRepo = function (repoID) {
  let url = this.server + '/api/v2.1/admin/libraries/' + repoID + '/';
  return this.req.delete(url);
};

// Admin: transfer library ownership
seafileAPI.sysAdminTransferRepo = function (repoID, email) {
  let url = this.server + '/api/v2.1/admin/libraries/' + repoID + '/transfer/';
  let data = { owner: email };
  return this.req.put(url, data);
};

// Admin: list libraries by owner email
seafileAPI.sysAdminListReposByOwner = function (email) {
  let url = this.server + '/api/v2.1/admin/libraries/?owner=' + encodeURIComponent(email);
  return this.req.get(url);
};

// Admin: list libraries for a specific org
seafileAPI.sysAdminListOrgRepos = function (orgID) {
  let url = this.server + '/api/v2.1/admin/libraries/?org_id=' + encodeURIComponent(orgID);
  return this.req.get(url);
};

// Admin: get system repo info (stub — SesameFS doesn't use a system repo)
seafileAPI.sysAdminGetSystemRepoInfo = function () {
  return Promise.resolve({ data: { name: 'System', id: '', encrypted: false, file_count: 0, size: 0 } });
};

// Admin: list directory entries in a library
seafileAPI.sysAdminListRepoDirents = function (repoID, path) {
  let url = this.server + '/api/v2.1/admin/libraries/' + repoID + '/dirents/';
  if (path) url += '?path=' + encodeURIComponent(path);
  return this.req.get(url);
};

// Admin: create folder in library (via existing dir API with admin auth)
seafileAPI.sysAdminCreateSysRepoFolder = function (repoID, path, folderName) {
  let dirPath = path.endsWith('/') ? path + folderName : path + '/' + folderName;
  let url = this.server + '/api2/repos/' + repoID + '/dir/?p=' + encodeURIComponent(dirPath);
  let form = new FormData();
  form.append('operation', 'mkdir');
  return this.req.post(url, form);
};

// Admin: delete a dirent (file or folder) in a library
seafileAPI.sysAdminDeleteRepoDirent = function (repoID, path) {
  let url = this.server + '/api/v2.1/repos/' + repoID + '/file/?p=' + encodeURIComponent(path);
  return this.req.delete(url);
};

// Admin: get download URL for a file in a library
seafileAPI.sysAdminGetRepoFileDownloadURL = function (repoID, path) {
  let url = this.server + '/api2/repos/' + repoID + '/file/?p=' + encodeURIComponent(path) + '&reuse=1';
  return this.req.get(url);
};

// Admin: get upload URL for system repo (stub)
seafileAPI.sydAdminGetSysRepoItemUploadURL = function (path) {
  return Promise.resolve({ data: { upload_link: '' } });
};

// Admin: get library history setting
seafileAPI.sysAdminGetRepoHistorySetting = function (repoID) {
  let url = this.server + '/api/v2.1/admin/libraries/' + repoID + '/history-setting/';
  return this.req.get(url);
};

// Admin: update library history setting
seafileAPI.sysAdminUpdateRepoHistorySetting = function (repoID, days) {
  let url = this.server + '/api/v2.1/admin/libraries/' + repoID + '/history-setting/';
  let data = { keep_days: days };
  return this.req.put(url, data);
};

// Admin: list shared items for a library
seafileAPI.sysAdminListRepoSharedItems = function (repoID, shareType) {
  let url = this.server + '/api/v2.1/admin/libraries/' + repoID + '/shared-items/';
  if (shareType) url += '?share_type=' + shareType;
  return this.req.get(url);
};

// Admin: add shared item to a library (uses standard share API)
seafileAPI.sysAdminAddRepoSharedItem = function (repoID, shareType, shareToList, permission) {
  let url = this.server + '/api2/repos/' + repoID + '/dir/shared_items/?p=/';
  let form = new FormData();
  form.append('share_type', shareType);
  form.append('permission', permission);
  if (shareType === 'user') {
    shareToList.forEach(item => form.append('username', item));
  } else {
    shareToList.forEach(item => form.append('group_id', item));
  }
  return this.req.put(url, form);
};

// Admin: delete shared item from a library
seafileAPI.sysAdminDeleteRepoSharedItem = function (repoID, shareType, shareToID) {
  let url = this.server + '/api2/repos/' + repoID + '/dir/shared_items/?p=/&share_type=' + shareType;
  if (shareType === 'user') {
    url += '&username=' + encodeURIComponent(shareToID);
  } else {
    url += '&group_id=' + shareToID;
  }
  return this.req.delete(url);
};

// Admin: update shared item permission
seafileAPI.sysAdminUpdateRepoSharedItemPermission = function (repoID, shareType, shareToID, permission) {
  let url = this.server + '/api2/repos/' + repoID + '/dir/shared_items/?p=/&share_type=' + shareType;
  if (shareType === 'user') {
    url += '&username=' + encodeURIComponent(shareToID);
  } else {
    url += '&group_id=' + shareToID;
  }
  let form = new FormData();
  form.append('permission', permission);
  return this.req.post(url, form);
};

// Admin: list group libraries
seafileAPI.sysAdminListGroupRepos = function (groupID) {
  let url = this.server + '/api/v2.1/admin/groups/' + groupID + '/libraries/';
  return this.req.get(url);
};

// Admin: unshare repo from group
seafileAPI.sysAdminUnshareRepoFromGroup = function (groupID, repoID) {
  let url = this.server + '/api/v2.1/groups/' + groupID + '/libraries/' + repoID + '/';
  return this.req.delete(url);
};

// Admin: list repos shared to a user
seafileAPI.sysAdminListShareInRepos = function (email) {
  let url = this.server + '/api/v2.1/admin/libraries/?shared_to=' + encodeURIComponent(email);
  return this.req.get(url);
};

// Admin: add library in department group
seafileAPI.sysAdminAddRepoInDepartment = function (groupID, repoName) {
  let url = this.server + '/api/v2.1/groups/' + groupID + '/group-owned-libraries/';
  let form = new FormData();
  form.append('name', repoName);
  form.append('permission', 'rw');
  return this.req.post(url, form);
};

// Admin: delete library in department group
seafileAPI.sysAdminDeleteRepoInDepartment = function (groupID, repoID) {
  let url = this.server + '/api/v2.1/groups/' + groupID + '/group-owned-libraries/' + repoID + '/';
  return this.req.delete(url);
};

// ============================================================================
// Admin Trash Library API methods
// ============================================================================

// Admin: list all deleted libraries (paginated)
seafileAPI.sysAdminListTrashRepos = function (page, perPage) {
  let url = this.server + '/api/v2.1/admin/trash-libraries/';
  const params = new URLSearchParams();
  if (page) params.set('page', page);
  if (perPage) params.set('per_page', perPage);
  if (params.toString()) url += '?' + params.toString();
  return this.req.get(url);
};

// Admin: permanently delete a trashed library
seafileAPI.sysAdminDeleteTrashRepo = function (repoID) {
  let url = this.server + '/api/v2.1/repos/deleted/' + repoID + '/';
  return this.req.delete(url);
};

// Admin: restore a trashed library
seafileAPI.sysAdminRestoreTrashRepo = function (repoID) {
  let url = this.server + '/api/v2.1/repos/deleted/' + repoID + '/';
  return this.req.put(url);
};

// Admin: permanently delete ALL trashed libraries
seafileAPI.sysAdminCleanTrashRepos = function () {
  let url = this.server + '/api/v2.1/admin/trash-libraries/';
  return this.req.delete(url);
};

// Admin: search trashed libraries by owner
seafileAPI.sysAdminSearchTrashRepos = function (owner) {
  let url = this.server + '/api/v2.1/admin/trash-libraries/?owner=' + encodeURIComponent(owner);
  return this.req.get(url);
};

// ============================================================================
// Repository History API methods
// ============================================================================

seafileAPI.getRepoHistory = function (repoID, page, perPage) {
  let url = this.server + '/api/v2.1/repos/' + repoID + '/history/';
  const params = new URLSearchParams();
  if (page) params.set('page', page);
  if (perPage) params.set('per_page', perPage);
  if (params.toString()) url += '?' + params.toString();
  return this.req.get(url);
};

// Fallback for getRepoInfo if not provided by seafile-js
if (!seafileAPI.getRepoInfo) {
  seafileAPI.getRepoInfo = function (repoID) {
    let url = this.server + '/api/v2.1/repos/' + repoID + '/';
    return this.req.get(url);
  };
}

// ============================================================================
// Revert API methods (for restoring files/folders to a specific commit version)
// ============================================================================

// Revert a file to its state at a specific commit
// conflictPolicy: 'replace' | 'skip' | undefined (undefined = return conflict error)
seafileAPI.revertFile = function (repoID, path, commitID, conflictPolicy) {
  let url = this.server + '/api/v2.1/repos/' + repoID + '/file/?p=' + encodeURIComponent(path) + '&operation=revert';
  let data = new FormData();
  data.append('commit_id', commitID);
  if (conflictPolicy) {
    data.append('conflict_policy', conflictPolicy);
  }
  return this.req.post(url, data);
};

// Revert a folder to its state at a specific commit
// conflictPolicy: 'replace' | 'skip' | undefined (undefined = return conflict error)
seafileAPI.revertFolder = function (repoID, path, commitID, conflictPolicy) {
  let url = this.server + '/api/v2.1/repos/' + repoID + '/dir/?p=' + encodeURIComponent(path) + '&operation=revert';
  let data = new FormData();
  data.append('commit_id', commitID);
  if (conflictPolicy) {
    data.append('conflict_policy', conflictPolicy);
  }
  return this.req.post(url, data);
};

// Revert entire library to a specific commit
seafileAPI.revertRepo = function (repoID, commitID) {
  let url = this.server + '/api/v2.1/repos/' + repoID + '/?operation=revert';
  let data = new FormData();
  data.append('commit_id', commitID);
  return this.req.put(url, data);
};

export { seafileAPI, isAuthenticated, login, logout, getToken, setAuthToken, initAPI };
