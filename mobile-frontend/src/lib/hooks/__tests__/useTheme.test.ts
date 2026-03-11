/// <reference types="vitest/globals" />
import { renderHook, act } from '@testing-library/react';
import { useTheme } from '../useTheme';

// Mock matchMedia
function createMockMatchMedia(matches: boolean) {
  const listeners: Array<(e: MediaQueryListEvent) => void> = [];
  return {
    matches,
    addEventListener: (_: string, fn: (e: MediaQueryListEvent) => void) => listeners.push(fn),
    removeEventListener: (_: string, fn: (e: MediaQueryListEvent) => void) => {
      const idx = listeners.indexOf(fn);
      if (idx >= 0) listeners.splice(idx, 1);
    },
    trigger(newMatches: boolean) {
      listeners.forEach(fn => fn({ matches: newMatches } as MediaQueryListEvent));
    },
  };
}

let mockMql: ReturnType<typeof createMockMatchMedia>;

beforeEach(() => {
  localStorage.clear();
  document.documentElement.classList.remove('dark');

  // Add meta theme-color
  let meta = document.querySelector('meta[name="theme-color"]');
  if (!meta) {
    meta = document.createElement('meta');
    meta.setAttribute('name', 'theme-color');
    meta.setAttribute('content', '#eb8205');
    document.head.appendChild(meta);
  }

  mockMql = createMockMatchMedia(false);
  window.matchMedia = vi.fn().mockReturnValue(mockMql);
});

describe('useTheme', () => {
  it('defaults to system preference', () => {
    const { result } = renderHook(() => useTheme());
    expect(result.current.theme).toBe('system');
    expect(result.current.isDark).toBe(false);
  });

  it('defaults to dark when system prefers dark', () => {
    mockMql = createMockMatchMedia(true);
    window.matchMedia = vi.fn().mockReturnValue(mockMql);

    const { result } = renderHook(() => useTheme());
    expect(result.current.theme).toBe('system');
    expect(result.current.isDark).toBe(true);
    expect(document.documentElement.classList.contains('dark')).toBe(true);
  });

  it('manual override persists to localStorage', () => {
    const { result } = renderHook(() => useTheme());

    act(() => {
      result.current.setTheme('dark');
    });

    expect(result.current.theme).toBe('dark');
    expect(result.current.isDark).toBe(true);
    expect(localStorage.getItem('theme-preference')).toBe('dark');
    expect(document.documentElement.classList.contains('dark')).toBe(true);
  });

  it('reads persisted preference from localStorage', () => {
    localStorage.setItem('theme-preference', 'dark');
    const { result } = renderHook(() => useTheme());
    expect(result.current.theme).toBe('dark');
    expect(result.current.isDark).toBe(true);
  });

  it('applies dark class correctly and updates meta theme-color', () => {
    const { result } = renderHook(() => useTheme());

    act(() => {
      result.current.setTheme('dark');
    });
    expect(document.documentElement.classList.contains('dark')).toBe(true);
    expect(document.querySelector('meta[name="theme-color"]')?.getAttribute('content')).toBe('#1a1a1a');

    act(() => {
      result.current.setTheme('light');
    });
    expect(document.documentElement.classList.contains('dark')).toBe(false);
    expect(document.querySelector('meta[name="theme-color"]')?.getAttribute('content')).toBe('#eb8205');
  });

  it('responds to system preference changes in system mode', () => {
    const { result } = renderHook(() => useTheme());
    expect(result.current.theme).toBe('system');

    // Simulate system going dark
    act(() => {
      mockMql.trigger(true);
    });
    expect(document.documentElement.classList.contains('dark')).toBe(true);
  });
});
