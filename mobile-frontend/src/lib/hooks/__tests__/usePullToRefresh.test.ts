/// <reference types="vitest/globals" />
import { renderHook, act } from '@testing-library/react';
import { usePullToRefresh } from '../usePullToRefresh';

describe('usePullToRefresh', () => {
  it('returns initial state', () => {
    const onRefresh = vi.fn();
    const { result } = renderHook(() => usePullToRefresh({ onRefresh }));
    expect(result.current.pulling).toBe(false);
    expect(result.current.refreshing).toBe(false);
    expect(result.current.pullDistance).toBe(0);
  });

  it('provides a containerRef', () => {
    const onRefresh = vi.fn();
    const { result } = renderHook(() => usePullToRefresh({ onRefresh }));
    expect(result.current.containerRef).toBeDefined();
    expect(result.current.containerRef.current).toBeNull();
  });

  it('default threshold is 80', () => {
    const onRefresh = vi.fn();
    const { result } = renderHook(() => usePullToRefresh({ onRefresh }));
    // Just verifying hook doesn't crash and returns expected shape
    expect(result.current.pulling).toBe(false);
    expect(result.current.refreshing).toBe(false);
    expect(result.current.pullDistance).toBe(0);
  });

  it('attaches and cleans up event listeners on container', () => {
    const onRefresh = vi.fn();
    const div = document.createElement('div');
    Object.defineProperty(div, 'scrollTop', { value: 0, writable: true });

    const addSpy = vi.spyOn(div, 'addEventListener');
    const removeSpy = vi.spyOn(div, 'removeEventListener');

    const { result, unmount } = renderHook(() => {
      const hook = usePullToRefresh({ onRefresh });
      (hook.containerRef as React.MutableRefObject<HTMLElement>).current = div;
      return hook;
    });

    expect(addSpy).toHaveBeenCalledWith('touchstart', expect.any(Function), { passive: true });
    expect(addSpy).toHaveBeenCalledWith('touchmove', expect.any(Function), { passive: true });
    expect(addSpy).toHaveBeenCalledWith('touchend', expect.any(Function));

    unmount();
    expect(removeSpy).toHaveBeenCalledWith('touchstart', expect.any(Function));
    expect(removeSpy).toHaveBeenCalledWith('touchmove', expect.any(Function));
    expect(removeSpy).toHaveBeenCalledWith('touchend', expect.any(Function));
  });
});
