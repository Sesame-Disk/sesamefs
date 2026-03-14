import { useState, useEffect, useCallback } from 'react';

export type ThemeOption = 'light' | 'dark' | 'system';

const STORAGE_KEY = 'theme-preference';

function getSystemPrefersDark(): boolean {
  return typeof window !== 'undefined' &&
    window.matchMedia('(prefers-color-scheme: dark)').matches;
}

function applyTheme(isDark: boolean) {
  if (isDark) {
    document.documentElement.classList.add('dark');
  } else {
    document.documentElement.classList.remove('dark');
  }

  const metaThemeColor = document.querySelector('meta[name="theme-color"]');
  if (metaThemeColor) {
    metaThemeColor.setAttribute('content', isDark ? '#1a1a1a' : '#eb8205');
  }
}

export function useTheme() {
  const [theme, setThemeState] = useState<ThemeOption>(() => {
    if (typeof window === 'undefined') return 'system';
    return (localStorage.getItem(STORAGE_KEY) as ThemeOption) || 'system';
  });

  const isDark = theme === 'dark' || (theme === 'system' && getSystemPrefersDark());

  const setTheme = useCallback((newTheme: ThemeOption) => {
    setThemeState(newTheme);
    localStorage.setItem(STORAGE_KEY, newTheme);
  }, []);

  useEffect(() => {
    applyTheme(isDark);
  }, [isDark]);

  useEffect(() => {
    if (theme !== 'system') return;

    const mql = window.matchMedia('(prefers-color-scheme: dark)');
    const handler = (e: MediaQueryListEvent) => {
      applyTheme(e.matches);
    };
    mql.addEventListener('change', handler);
    return () => mql.removeEventListener('change', handler);
  }, [theme]);

  return { theme, setTheme, isDark };
}
