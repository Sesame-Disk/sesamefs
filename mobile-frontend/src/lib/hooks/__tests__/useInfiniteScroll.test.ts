/// <reference types="vitest/globals" />
import { renderHook, act } from '@testing-library/react';
import { useInfiniteScroll } from '../useInfiniteScroll';

// Mock IntersectionObserver
let observerCallback: IntersectionObserverCallback;
const mockObserve = vi.fn();
const mockDisconnect = vi.fn();

class MockIntersectionObserver {
  constructor(callback: IntersectionObserverCallback) {
    observerCallback = callback;
  }
  observe = mockObserve;
  disconnect = mockDisconnect;
  unobserve = vi.fn();
  root = null;
  rootMargin = '';
  thresholds = [] as number[];
  takeRecords = () => [] as IntersectionObserverEntry[];
}

beforeEach(() => {
  vi.clearAllMocks();
  global.IntersectionObserver = MockIntersectionObserver as unknown as typeof IntersectionObserver;
});

describe('useInfiniteScroll', () => {
  it('returns initial state', () => {
    const { result } = renderHook(() =>
      useInfiniteScroll({ onLoadMore: vi.fn(), hasMore: true }),
    );
    expect(result.current.loading).toBe(false);
    expect(result.current.page).toBe(1);
  });

  it('observes sentinel element when hasMore is true', () => {
    const div = document.createElement('div');
    const onLoadMore = vi.fn();

    renderHook(() => {
      const hook = useInfiniteScroll({ onLoadMore, hasMore: true });
      (hook.sentinelRef as React.MutableRefObject<HTMLElement>).current = div;
      return hook;
    });

    expect(mockObserve).toHaveBeenCalledWith(div);
  });

  it('calls onLoadMore when sentinel intersects', async () => {
    const onLoadMore = vi.fn().mockResolvedValue(undefined);
    const div = document.createElement('div');

    renderHook(() => {
      const hook = useInfiniteScroll({ onLoadMore, hasMore: true });
      (hook.sentinelRef as React.MutableRefObject<HTMLElement>).current = div;
      return hook;
    });

    await act(async () => {
      observerCallback(
        [{ isIntersecting: true } as IntersectionObserverEntry],
        {} as IntersectionObserver,
      );
    });

    expect(onLoadMore).toHaveBeenCalled();
  });

  it('does not call onLoadMore when not intersecting', async () => {
    const onLoadMore = vi.fn();
    const div = document.createElement('div');

    renderHook(() => {
      const hook = useInfiniteScroll({ onLoadMore, hasMore: true });
      (hook.sentinelRef as React.MutableRefObject<HTMLElement>).current = div;
      return hook;
    });

    await act(async () => {
      observerCallback(
        [{ isIntersecting: false } as IntersectionObserverEntry],
        {} as IntersectionObserver,
      );
    });

    expect(onLoadMore).not.toHaveBeenCalled();
  });

  it('reset sets page back to 1', async () => {
    const onLoadMore = vi.fn().mockResolvedValue(undefined);
    const div = document.createElement('div');

    const { result } = renderHook(() => {
      const hook = useInfiniteScroll({ onLoadMore, hasMore: true });
      (hook.sentinelRef as React.MutableRefObject<HTMLElement>).current = div;
      return hook;
    });

    await act(async () => {
      observerCallback(
        [{ isIntersecting: true } as IntersectionObserverEntry],
        {} as IntersectionObserver,
      );
    });

    expect(result.current.page).toBe(2);

    act(() => {
      result.current.reset();
    });

    expect(result.current.page).toBe(1);
  });
});
