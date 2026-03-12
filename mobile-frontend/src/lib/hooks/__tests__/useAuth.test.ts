/// <reference types="vitest/globals" />
import { renderHook, act } from '@testing-library/react';
import { useAuth } from '../useAuth';
import * as api from '../../api';

vi.mock('../../api', () => ({
  getAuthToken: vi.fn(),
  setAuthToken: vi.fn(),
  clearAuthToken: vi.fn(),
  login: vi.fn(),
}));

const mockApi = api as unknown as {
  getAuthToken: ReturnType<typeof vi.fn>;
  setAuthToken: ReturnType<typeof vi.fn>;
  clearAuthToken: ReturnType<typeof vi.fn>;
  login: ReturnType<typeof vi.fn>;
};

beforeEach(() => {
  vi.clearAllMocks();
  mockApi.getAuthToken.mockReturnValue(null);
});

describe('useAuth', () => {
  it('initializes as unauthenticated when no token', () => {
    const { result } = renderHook(() => useAuth());
    expect(result.current.isAuthenticated).toBe(false);
    expect(result.current.token).toBeNull();
    expect(result.current.loading).toBe(false);
    expect(result.current.error).toBeNull();
  });

  it('initializes as authenticated when token exists', () => {
    mockApi.getAuthToken.mockReturnValue('existing-token');
    const { result } = renderHook(() => useAuth());
    expect(result.current.isAuthenticated).toBe(true);
    expect(result.current.token).toBe('existing-token');
  });

  it('login sets token on success', async () => {
    mockApi.login.mockResolvedValue('new-token');
    const { result } = renderHook(() => useAuth());

    await act(async () => {
      await result.current.login('user@test.com', 'pass123');
    });

    expect(result.current.isAuthenticated).toBe(true);
    expect(result.current.token).toBe('new-token');
    expect(result.current.loading).toBe(false);
    expect(result.current.error).toBeNull();
  });

  it('login sets error on failure', async () => {
    mockApi.login.mockRejectedValue(new Error('Invalid credentials'));
    const { result } = renderHook(() => useAuth());

    await act(async () => {
      await expect(result.current.login('bad@test.com', 'wrong')).rejects.toThrow('Invalid credentials');
    });

    expect(result.current.isAuthenticated).toBe(false);
    expect(result.current.error).toBe('Invalid credentials');
    expect(result.current.loading).toBe(false);
  });

  it('logout clears auth state', () => {
    mockApi.getAuthToken.mockReturnValue('token');
    const { result } = renderHook(() => useAuth());

    act(() => {
      result.current.logout();
    });

    expect(result.current.isAuthenticated).toBe(false);
    expect(result.current.token).toBeNull();
    expect(mockApi.clearAuthToken).toHaveBeenCalled();
  });

  it('checkAuth returns current auth status', () => {
    const { result } = renderHook(() => useAuth());

    mockApi.getAuthToken.mockReturnValue('refreshed-token');
    let isAuth = false;
    act(() => {
      isAuth = result.current.checkAuth();
    });

    expect(isAuth).toBe(true);
    expect(result.current.isAuthenticated).toBe(true);
    expect(result.current.token).toBe('refreshed-token');
  });
});
