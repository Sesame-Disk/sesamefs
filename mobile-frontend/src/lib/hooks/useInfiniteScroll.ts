import { useRef, useEffect, useState, useCallback } from 'react';

export interface InfiniteScrollOptions {
  onLoadMore: () => void | Promise<void>;
  hasMore: boolean;
  rootMargin?: string;
  threshold?: number;
}

export function useInfiniteScroll({
  onLoadMore,
  hasMore,
  rootMargin = '200px',
  threshold = 0,
}: InfiniteScrollOptions) {
  const sentinelRef = useRef<HTMLElement | null>(null);
  const [loading, setLoading] = useState(false);
  const [page, setPage] = useState(1);

  const loadMore = useCallback(async () => {
    if (loading || !hasMore) return;
    setLoading(true);
    try {
      await onLoadMore();
      setPage(prev => prev + 1);
    } finally {
      setLoading(false);
    }
  }, [loading, hasMore, onLoadMore]);

  useEffect(() => {
    const el = sentinelRef.current;
    if (!el || !hasMore) return;

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting) {
          loadMore();
        }
      },
      { rootMargin, threshold },
    );

    observer.observe(el);
    return () => observer.disconnect();
  }, [hasMore, loadMore, rootMargin, threshold]);

  const reset = useCallback(() => {
    setPage(1);
    setLoading(false);
  }, []);

  return { sentinelRef, loading, page, reset };
}
