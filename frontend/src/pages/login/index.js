import React, { useState, useEffect } from 'react';
import { login, seafileAPI } from '../../utils/seafile-api';
import { siteTitle, loginBGPath } from '../../utils/constants';
import './login.css';

function LoginPage() {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [ssoLoading, setSsoLoading] = useState(false);
  const [oidcEnabled, setOidcEnabled] = useState(false);

  // Check if OIDC is enabled
  useEffect(() => {
    // Show session expired message if redirected due to 401
    const params = new URLSearchParams(window.location.search);
    if (params.get('expired') === '1') {
      setError('Your session has expired. Please log in again.');
    }

    seafileAPI.getOIDCConfig()
      .then(resp => {
        if (resp.data && resp.data.enabled) {
          setOidcEnabled(true);
        }
      })
      .catch(() => {
        // OIDC not available, use password login only
        setOidcEnabled(false);
      });
  }, []);

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    setLoading(true);

    try {
      await login(email, password);
      // Redirect to home on success
      window.location.href = '/';
    } catch (err) {
      setError(err.message || 'Login failed. Please try again.');
    } finally {
      setLoading(false);
    }
  };

  const handleSSOLogin = async () => {
    setError('');
    setSsoLoading(true);

    try {
      // Store return URL for after SSO
      const returnURL = new URLSearchParams(window.location.search).get('next') || '/';
      localStorage.setItem('sso_return_url', returnURL);

      // Get OIDC login URL
      const redirectURI = window.location.origin + '/sso/';
      const resp = await seafileAPI.getOIDCLoginURL(redirectURI, returnURL);

      if (resp.data && resp.data.authorization_url) {
        // Redirect to OIDC provider
        window.location.href = resp.data.authorization_url;
      } else {
        throw new Error('Failed to get SSO login URL');
      }
    } catch (err) {
      console.error('SSO login error:', err);
      setError(err.response?.data?.error || err.message || 'SSO login failed. Please try again.');
      setSsoLoading(false);
    }
  };

  const bgStyle = loginBGPath ? { backgroundImage: `url(${loginBGPath})` } : {};

  return (
    <div className="login-page" style={bgStyle}>
      <div className="login-container">
        <div className="login-header">
          <h1>{siteTitle || 'SesameFS'}</h1>
        </div>

        {/* SSO Login Button */}
        {oidcEnabled && (
          <div className="sso-login" style={{ marginBottom: '1.5rem' }}>
            <button
              type="button"
              className="btn btn-outline-primary btn-block"
              onClick={handleSSOLogin}
              disabled={ssoLoading}
              style={{ width: '100%' }}
            >
              {ssoLoading ? 'Redirecting...' : 'Login with SSO'}
            </button>
            <div className="login-divider" style={{
              display: 'flex',
              alignItems: 'center',
              margin: '1.5rem 0',
              color: '#6c757d'
            }}>
              <hr style={{ flex: 1, borderColor: '#dee2e6' }} />
              <span style={{ padding: '0 1rem' }}>or</span>
              <hr style={{ flex: 1, borderColor: '#dee2e6' }} />
            </div>
          </div>
        )}

        <form className="login-form" onSubmit={handleSubmit}>
          {error && (
            <div className="login-error">
              {error}
            </div>
          )}

          <div className="form-group">
            <label htmlFor="email">Email</label>
            <input
              type="email"
              id="email"
              className="form-control"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="Enter your email"
              required
              autoFocus
            />
          </div>

          <div className="form-group">
            <label htmlFor="password">Password</label>
            <input
              type="password"
              id="password"
              className="form-control"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="Enter your password"
              required
            />
          </div>

          <button
            type="submit"
            className="btn btn-primary btn-block"
            disabled={loading}
          >
            {loading ? 'Logging in...' : 'Log In'}
          </button>
        </form>
      </div>
    </div>
  );
}

export default LoginPage;
