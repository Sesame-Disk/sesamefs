import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import SSOCallback from '../SSOCallback';

vi.mock('../../../lib/oidc', () => ({
  exchangeOIDCCode: vi.fn(),
}));

import { exchangeOIDCCode } from '../../../lib/oidc';

const mockExchangeOIDCCode = vi.mocked(exchangeOIDCCode);

describe('SSOCallback', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    Object.defineProperty(window, 'location', {
      value: { href: '', search: '' },
      writable: true,
    });
  });

  it('calls exchangeOIDCCode with URL params', async () => {
    Object.defineProperty(window, 'location', {
      value: { href: '', search: '?code=abc123&state=xyz789' },
      writable: true,
    });
    mockExchangeOIDCCode.mockResolvedValue('token');

    render(<SSOCallback />);

    await waitFor(() => {
      expect(mockExchangeOIDCCode).toHaveBeenCalledWith('abc123', 'xyz789');
    });
  });

  it('redirects to /libraries/ on success', async () => {
    Object.defineProperty(window, 'location', {
      value: { href: '', search: '?code=abc&state=xyz' },
      writable: true,
    });
    mockExchangeOIDCCode.mockResolvedValue('token');

    render(<SSOCallback />);

    await waitFor(() => {
      expect(window.location.href).toBe('/libraries/');
    });
  });

  it('shows error on failure', async () => {
    Object.defineProperty(window, 'location', {
      value: { href: '', search: '?code=bad&state=xyz' },
      writable: true,
    });
    mockExchangeOIDCCode.mockRejectedValue(new Error('Exchange failed'));

    render(<SSOCallback />);

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Exchange failed');
    });
  });

  it('redirects to login if code/state missing', () => {
    Object.defineProperty(window, 'location', {
      value: { href: '', search: '' },
      writable: true,
    });

    render(<SSOCallback />);

    expect(window.location.href).toBe('/login/?error=sso_failed');
  });

  it('shows loading spinner while processing', () => {
    Object.defineProperty(window, 'location', {
      value: { href: '', search: '?code=abc&state=xyz' },
      writable: true,
    });
    mockExchangeOIDCCode.mockReturnValue(new Promise(() => {})); // never resolves

    render(<SSOCallback />);

    expect(screen.getByText('Completing sign in...')).toBeInTheDocument();
  });
});
