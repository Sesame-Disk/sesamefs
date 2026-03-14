import { useRef, useCallback, useState } from 'react';

export interface SwipeActionOptions {
  thresholdLeft?: number;
  thresholdRight?: number;
  onSwipeLeft?: () => void;
  onSwipeRight?: () => void;
}

export function useSwipeAction({
  thresholdLeft = 100,
  thresholdRight = 100,
  onSwipeLeft,
  onSwipeRight,
}: SwipeActionOptions = {}) {
  const [offsetX, setOffsetX] = useState(0);
  const [swiping, setSwiping] = useState(false);
  const startX = useRef(0);
  const startY = useRef(0);
  const isTracking = useRef(false);

  const onTouchStart = useCallback((e: React.TouchEvent) => {
    startX.current = e.touches[0].clientX;
    startY.current = e.touches[0].clientY;
    isTracking.current = true;
    setSwiping(true);
  }, []);

  const onTouchMove = useCallback((e: React.TouchEvent) => {
    if (!isTracking.current) return;
    const deltaX = e.touches[0].clientX - startX.current;
    const deltaY = e.touches[0].clientY - startY.current;

    // If vertical movement is dominant, stop tracking horizontal swipe
    if (Math.abs(deltaY) > Math.abs(deltaX) && offsetX === 0) {
      isTracking.current = false;
      setSwiping(false);
      setOffsetX(0);
      return;
    }

    setOffsetX(deltaX);
  }, [offsetX]);

  const onTouchEnd = useCallback(() => {
    if (!isTracking.current) return;
    isTracking.current = false;
    setSwiping(false);

    if (offsetX < -thresholdLeft && onSwipeLeft) {
      onSwipeLeft();
    } else if (offsetX > thresholdRight && onSwipeRight) {
      onSwipeRight();
    }

    setOffsetX(0);
  }, [offsetX, thresholdLeft, thresholdRight, onSwipeLeft, onSwipeRight]);

  return {
    offsetX,
    swiping,
    handlers: { onTouchStart, onTouchMove, onTouchEnd },
  };
}
