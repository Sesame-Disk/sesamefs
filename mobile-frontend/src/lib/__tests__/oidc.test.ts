import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { getOIDCLoginURL, exchangeOIDCCode, isDevBypass } from '../oidc';
import { getAuthToken } from '../api';

// Mock serviceURL to return a stable base URL
vi.mock('../config', () => ({
  serviceURL: () => 'http://localhost:8080',
}));

describe('isDevBypass', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('returns false when dev_bypass is not set', () => {
    expect(isDevBypass()).toBe(false);
  });

  it('returns true when dev_bypass is "1"', () => {
    localStorage.setItem('dev_bypass', '1');
    expect(isDevBypass()).toBe(true);
  });

  it('returns false when dev_bypass is any other value', () => {
    localStorage.setItem('dev_bypass', '0');
    expect(isDevBypass()).toBe(false);
  });
});

describe('getOIDCLoginURL', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('fetches the authorization URL from the OIDC discovery endpoint', async () => {
    const authUrl = 'https://idp.example.com/authorize?client_id=abc&redirect_uri=...';
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ authorization_url: authUrl }),
    }));

    const result = await getOIDCLoginURL();

    expect(result).toBe(authUrl);
    expect(fetch).toHaveBeenCalledWith('http://localhost:8080/oauth/authorize/');
  });

  it('throws when the discovery endpoint fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
    }));

    await expect(getOIDCLoginURL()).rejects.toThrow('Failed to get SSO login URL');
  });
});

describe('exchangeOIDCCode', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('exchanges auth code for a token and stores it', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ token: 'oidc-token-xyz' }),
    });
    vi.stubGlobal('fetch', fetchMock);

    const token = await exchangeOIDCCode('auth-code-123', 'state-abc');

    expect(token).toBe('oidc-token-xyz');
    expect(getAuthToken()).toBe('oidc-token-xyz');

    // Verify correct endpoint and payload
    expect(fetchMock).toHaveBeenCalledWith(
      'http://localhost:8080/oauth/callback/',
      expect.objectContaining({
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ code: 'auth-code-123', state: 'state-abc' }),
      }),
    );
  });

  it('throws when token exchange fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 401,
    }));

    await expect(exchangeOIDCCode('bad-code', 'state')).rejects.toThrow('SSO authentication failed');
  });

  it('does not store token on failure', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 401,
    }));

    try {
      await exchangeOIDCCode('bad-code', 'state');
    } catch {
      // expected
    }

    expect(getAuthToken()).toBeNull();
  });
});
