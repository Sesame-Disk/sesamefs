import React, { useEffect, useState } from 'react';
import { exchangeOIDCCode } from '../../lib/oidc';

export default function SSOCallback() {
  const [error, setError] = useState('');

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const code = params.get('code');
    const state = params.get('state');

    if (!code || !state) {
      window.location.href = '/login/?error=sso_failed';
      return;
    }

    exchangeOIDCCode(code, state)
      .then(() => {
        window.location.href = '/libraries/';
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : 'SSO authentication failed');
      });
  }, []);

  if (error) {
    return (
      <div className="text-center p-6">
        <p className="text-red-600 mb-4" role="alert">{error}</p>
        <a href="/login/" className="text-primary hover:underline">
          Back to login
        </a>
      </div>
    );
  }

  return (
    <div className="flex flex-col items-center justify-center p-6">
      <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary mb-4" />
      <p className="text-gray-500">Completing sign in...</p>
    </div>
  );
}
