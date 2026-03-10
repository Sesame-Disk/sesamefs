import React, { useState, useEffect } from 'react';
import { login } from '../../lib/api';
import { getOIDCLoginURL, isDevBypass } from '../../lib/oidc';
import { getConfig } from '../../lib/config';

export default function LoginForm() {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [showPassword, setShowPassword] = useState(false);

  const config = getConfig();
  const params = new URLSearchParams(window.location.search);
  const expired = params.get('expired') === '1';
  const ssoError = params.get('error') === 'sso_failed';

  useEffect(() => {
    if (isDevBypass()) {
      window.location.href = '/libraries/';
    }
  }, []);

  if (isDevBypass()) {
    return null;
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setLoading(true);

    try {
      await login(email, password);
      window.location.href = '/libraries/';
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed');
    } finally {
      setLoading(false);
    }
  };

  const handleSSO = async () => {
    setError('');
    setLoading(true);

    try {
      const url = await getOIDCLoginURL();
      window.location.href = url;
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to start SSO');
      setLoading(false);
    }
  };

  return (
    <div className="flex flex-col items-center w-full">
      <img
        src={`${config.mediaUrl}${config.logoPath}`}
        alt={config.siteTitle}
        className="max-w-[200px] mb-4"
      />
      <h1 className="text-xl font-medium text-text mb-6">{config.siteTitle}</h1>

      {expired && (
        <p className="text-red-600 text-sm mb-4 text-center">
          Session expired, please log in again
        </p>
      )}

      {ssoError && (
        <p className="text-red-600 text-sm mb-4 text-center">
          SSO login failed, please try again
        </p>
      )}

      {error && (
        <p className="text-red-600 text-sm mb-4 text-center" role="alert">
          {error}
        </p>
      )}

      <form onSubmit={handleSubmit} className="w-full space-y-4">
        <input
          type="email"
          placeholder="Email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          autoComplete="username"
          required
          className="w-full min-h-[44px] px-4 py-3 rounded-lg border border-border focus:outline-none focus:ring-2 focus:ring-primary"
        />

        <div className="relative">
          <input
            type={showPassword ? 'text' : 'password'}
            placeholder="Password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            required
            className="w-full min-h-[44px] px-4 py-3 rounded-lg border border-border focus:outline-none focus:ring-2 focus:ring-primary pr-16"
          />
          <button
            type="button"
            onClick={() => setShowPassword(!showPassword)}
            className="absolute right-3 top-1/2 -translate-y-1/2 text-sm text-gray-500 min-h-[44px] min-w-[44px] flex items-center justify-center"
          >
            {showPassword ? 'Hide' : 'Show'}
          </button>
        </div>

        <button
          type="submit"
          disabled={loading}
          className="w-full h-12 bg-primary-button text-white rounded-lg font-medium hover:bg-primary-hover disabled:opacity-50 transition-colors"
        >
          {loading ? 'Logging in...' : 'Log In'}
        </button>
      </form>

      <div className="w-full flex items-center my-6">
        <div className="flex-1 border-t border-border" />
        <span className="px-4 text-sm text-gray-400">or</span>
        <div className="flex-1 border-t border-border" />
      </div>

      <button
        onClick={handleSSO}
        disabled={loading}
        className="w-full h-12 border border-border rounded-lg font-medium text-text hover:bg-gray-50 disabled:opacity-50 transition-colors"
      >
        Sign in with SSO
      </button>
    </div>
  );
}
