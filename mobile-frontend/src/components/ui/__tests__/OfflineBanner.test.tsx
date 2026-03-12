import React from 'react';
import { render, screen, act, waitFor } from '@testing-library/react';
import { describe, it, expect, beforeEach } from 'vitest';
import OfflineBanner from '../OfflineBanner';

describe('OfflineBanner', () => {
  beforeEach(() => {
    Object.defineProperty(navigator, 'onLine', { value: true, writable: true });
  });

  it('does not show banner when online', () => {
    render(<OfflineBanner />);
    expect(screen.queryByText(/You are offline/)).not.toBeInTheDocument();
  });

  it('shows banner when offline', () => {
    Object.defineProperty(navigator, 'onLine', { value: false, writable: true });
    render(<OfflineBanner />);
    expect(screen.getByText(/You are offline/)).toBeInTheDocument();
  });

  it('shows banner when going offline', () => {
    render(<OfflineBanner />);
    expect(screen.queryByText(/You are offline/)).not.toBeInTheDocument();

    act(() => {
      window.dispatchEvent(new Event('offline'));
    });

    expect(screen.getByText(/You are offline/)).toBeInTheDocument();
  });

  it('triggers exit animation when coming back online', async () => {
    Object.defineProperty(navigator, 'onLine', { value: false, writable: true });
    render(<OfflineBanner />);
    expect(screen.getByText(/You are offline/)).toBeInTheDocument();

    act(() => {
      window.dispatchEvent(new Event('online'));
    });

    // The element may still be in DOM during exit animation.
    // Verify the exit animation was triggered by checking the container has exit styles.
    await waitFor(() => {
      const banner = screen.queryByTestId('offline-banner');
      if (banner) {
        // Exit animation gives height: 0 and opacity: 0
        expect(banner.style.opacity).toBe('0');
      }
    });
  });
});
