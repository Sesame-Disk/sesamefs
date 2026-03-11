import { useState, useCallback } from 'react';
import { login as apiLogin, getAuthToken, clearAuthToken } from '../api';

export interface AuthState {
  isAuthenticated: boolean;
  token: string | null;
  loading: boolean;
  error: string | null;
}

export function useAuth() {
  const [state, setState] = useState<AuthState>(() => {
    const token = getAuthToken();
    return {
      isAuthenticated: !!token,
      token,
      loading: false,
      error: null,
    };
  });

  const login = useCallback(async (email: string, password: string) => {
    setState(prev => ({ ...prev, loading: true, error: null }));
    try {
      const token = await apiLogin(email, password);
      setState({ isAuthenticated: true, token, loading: false, error: null });
      return token;
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Login failed';
      setState(prev => ({ ...prev, loading: false, error: message }));
      throw err;
    }
  }, []);

  const logout = useCallback(() => {
    clearAuthToken();
    setState({ isAuthenticated: false, token: null, loading: false, error: null });
  }, []);

  const checkAuth = useCallback(() => {
    const token = getAuthToken();
    setState(prev => ({
      ...prev,
      isAuthenticated: !!token,
      token,
    }));
    return !!token;
  }, []);

  return { ...state, login, logout, checkAuth };
}
