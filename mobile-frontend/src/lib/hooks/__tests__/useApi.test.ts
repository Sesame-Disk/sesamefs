/// <reference types="vitest/globals" />
import { renderHook, act } from '@testing-library/react';
import { useApi } from '../useApi';

describe('useApi', () => {
  it('initializes with null data and no loading', () => {
    const fn = vi.fn();
    const { result } = renderHook(() => useApi(fn));
    expect(result.current.data).toBeNull();
    expect(result.current.loading).toBe(false);
    expect(result.current.error).toBeNull();
  });

  it('sets data on successful call', async () => {
    const fn = vi.fn().mockResolvedValue({ id: 1, name: 'test' });
    const { result } = renderHook(() => useApi(fn));

    await act(async () => {
      await result.current.execute('arg1', 'arg2');
    });

    expect(fn).toHaveBeenCalledWith('arg1', 'arg2');
    expect(result.current.data).toEqual({ id: 1, name: 'test' });
    expect(result.current.loading).toBe(false);
    expect(result.current.error).toBeNull();
  });

  it('sets error on failed call', async () => {
    const fn = vi.fn().mockRejectedValue(new Error('Network error'));
    const { result } = renderHook(() => useApi(fn));

    await act(async () => {
      await expect(result.current.execute()).rejects.toThrow('Network error');
    });

    expect(result.current.data).toBeNull();
    expect(result.current.loading).toBe(false);
    expect(result.current.error).toBe('Network error');
  });

  it('reset clears state', async () => {
    const fn = vi.fn().mockResolvedValue('data');
    const { result } = renderHook(() => useApi(fn));

    await act(async () => {
      await result.current.execute();
    });
    expect(result.current.data).toBe('data');

    act(() => {
      result.current.reset();
    });

    expect(result.current.data).toBeNull();
    expect(result.current.loading).toBe(false);
    expect(result.current.error).toBeNull();
  });

  it('handles non-Error thrown values', async () => {
    const fn = vi.fn().mockRejectedValue('string error');
    const { result } = renderHook(() => useApi(fn));

    await act(async () => {
      await expect(result.current.execute()).rejects.toBe('string error');
    });

    expect(result.current.error).toBe('API call failed');
  });
});
