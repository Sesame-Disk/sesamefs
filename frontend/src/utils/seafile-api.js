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
    // Initialize without auth - will need to login
    seafileAPI.init({ server });
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
seafileAPI.getOIDCConfig = async function() {
  const server = this.server || serviceURL || window.location.origin;
  const url = server + '/api/v2.1/auth/oidc/config/';
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error('Failed to get OIDC config');
  }
  return { data: await response.json() };
};

// Get OIDC login URL
seafileAPI.getOIDCLoginURL = async function(redirectURI, returnURL) {
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
seafileAPI.exchangeOIDCCode = async function(code, state, redirectURI) {
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
seafileAPI.getOIDCLogoutURL = async function(postLogoutRedirectURI) {
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
seafileAPI.listRepoTags = function(repoID) {
  const url = this.server + '/api/v2.1/repos/' + repoID + '/repo-tags/';
  return this.req.get(url);
};

// Create a new tag in a repository
seafileAPI.createRepoTag = function(repoID, name, color) {
  const url = this.server + '/api/v2.1/repos/' + repoID + '/repo-tags/';
  const data = { name, color };
  return this.req.post(url, data);
};

// Update a tag
seafileAPI.updateRepoTag = function(repoID, tagID, name, color) {
  const url = this.server + '/api/v2.1/repos/' + repoID + '/repo-tags/' + tagID + '/';
  const data = { name, color };
  return this.req.put(url, data);
};

// Delete a tag
seafileAPI.deleteRepoTag = function(repoID, tagID) {
  const url = this.server + '/api/v2.1/repos/' + repoID + '/repo-tags/' + tagID + '/';
  return this.req.delete(url);
};

// Get tags for a specific file
seafileAPI.getFileTags = function(repoID, filePath) {
  const url = this.server + '/api/v2.1/repos/' + repoID + '/file-tags/?file_path=' + encodeURIComponent(filePath);
  return this.req.get(url);
};

// Add a tag to a file
seafileAPI.addFileTag = function(repoID, filePath, repoTagID) {
  const url = this.server + '/api/v2.1/repos/' + repoID + '/file-tags/';
  const data = { file_path: filePath, repo_tag_id: repoTagID };
  return this.req.post(url, data);
};

// Remove a tag from a file
seafileAPI.deleteFileTag = function(repoID, fileTagID) {
  const url = this.server + '/api/v2.1/repos/' + repoID + '/file-tags/' + fileTagID + '/';
  return this.req.delete(url);
};

// List all files with a specific tag
seafileAPI.listTaggedFiles = function(repoID, tagID) {
  const url = this.server + '/api/v2.1/repos/' + repoID + '/tagged-files/' + tagID + '/';
  return this.req.get(url);
};

// List tagged files for share link
seafileAPI.getShareLinkTaggedFiles = function(shareLinkToken, tagID) {
  const url = this.server + '/api/v2.1/share-links/' + shareLinkToken + '/tagged-files/' + tagID + '/';
  return this.req.get(url);
};

export { seafileAPI, isAuthenticated, login, logout, getToken, setAuthToken, initAPI };
