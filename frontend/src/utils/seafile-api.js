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

// Logout - clear token
function logout() {
  localStorage.removeItem(TOKEN_KEY);
  // Redirect to login
  window.location.href = '/login/';
}

// Get stored token
function getToken() {
  return localStorage.getItem(TOKEN_KEY);
}

// Initialize on load
initAPI();

export { seafileAPI, isAuthenticated, login, logout, getToken, initAPI };
