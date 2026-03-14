import { serviceURL } from './config';
import { setAuthToken } from './api';

export function isDevBypass(): boolean {
  try {
    return localStorage.getItem('dev_bypass') === '1';
  } catch {
    return false;
  }
}

export async function getOIDCLoginURL(): Promise<string> {
  const res = await fetch(`${serviceURL()}/oauth/authorize/`);
  if (!res.ok) {
    throw new Error('Failed to get SSO login URL');
  }
  const data = await res.json();
  return data.authorization_url as string;
}

export async function exchangeOIDCCode(code: string, state: string): Promise<string> {
  const res = await fetch(`${serviceURL()}/oauth/callback/`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ code, state }),
  });

  if (!res.ok) {
    throw new Error('SSO authentication failed');
  }

  const data = await res.json();
  const token: string = data.token;
  setAuthToken(token);
  return token;
}

export { setAuthToken };
