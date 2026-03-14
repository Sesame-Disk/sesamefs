/// <reference types="vitest/globals" />
import { renderHook, act } from '@testing-library/react';
import { useSwipeAction } from '../useSwipeAction';

function createReactTouchEvent(clientX: number, clientY = 0) {
  return {
    touches: [{ clientX, clientY }],
  } as unknown as React.TouchEvent;
}

describe('useSwipeAction', () => {
  it('returns initial state', () => {
    const { result } = renderHook(() => useSwipeAction());
    expect(result.current.offsetX).toBe(0);
    expect(result.current.swiping).toBe(false);
    expect(result.current.handlers).toBeDefined();
  });

  it('tracks horizontal swipe offset', () => {
    const { result } = renderHook(() => useSwipeAction());

    act(() => {
      result.current.handlers.onTouchStart(createReactTouchEvent(200));
    });
    expect(result.current.swiping).toBe(true);

    act(() => {
      result.current.handlers.onTouchMove(createReactTouchEvent(100));
    });
    expect(result.current.offsetX).toBe(-100);
  });

  it('triggers onSwipeLeft when threshold exceeded', () => {
    const onSwipeLeft = vi.fn();
    const { result } = renderHook(() =>
      useSwipeAction({ thresholdLeft: 80, onSwipeLeft }),
    );

    act(() => {
      result.current.handlers.onTouchStart(createReactTouchEvent(200));
      result.current.handlers.onTouchMove(createReactTouchEvent(50));
    });

    act(() => {
      result.current.handlers.onTouchEnd();
    });

    expect(onSwipeLeft).toHaveBeenCalled();
  });

  it('triggers onSwipeRight when threshold exceeded', () => {
    const onSwipeRight = vi.fn();
    const { result } = renderHook(() =>
      useSwipeAction({ thresholdRight: 80, onSwipeRight }),
    );

    act(() => {
      result.current.handlers.onTouchStart(createReactTouchEvent(50));
      result.current.handlers.onTouchMove(createReactTouchEvent(200));
    });

    act(() => {
      result.current.handlers.onTouchEnd();
    });

    expect(onSwipeRight).toHaveBeenCalled();
  });

  it('does not trigger swipe below threshold', () => {
    const onSwipeLeft = vi.fn();
    const { result } = renderHook(() =>
      useSwipeAction({ thresholdLeft: 100, onSwipeLeft }),
    );

    act(() => {
      result.current.handlers.onTouchStart(createReactTouchEvent(200));
      result.current.handlers.onTouchMove(createReactTouchEvent(150));
    });

    act(() => {
      result.current.handlers.onTouchEnd();
    });

    expect(onSwipeLeft).not.toHaveBeenCalled();
  });

  it('resets offset after touch end', () => {
    const { result } = renderHook(() => useSwipeAction());

    act(() => {
      result.current.handlers.onTouchStart(createReactTouchEvent(200));
      result.current.handlers.onTouchMove(createReactTouchEvent(100));
    });

    act(() => {
      result.current.handlers.onTouchEnd();
    });

    expect(result.current.offsetX).toBe(0);
    expect(result.current.swiping).toBe(false);
  });
});
