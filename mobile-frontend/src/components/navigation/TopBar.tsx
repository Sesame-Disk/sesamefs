import React, { useState, useEffect, useCallback } from 'react';
import { Search, Bell } from 'lucide-react';
import { getUnseenNotificationCount } from '../../lib/api';

interface TopBarProps {
  title: string;
  avatarUrl?: string;
  userName?: string;
}

export default function TopBar({ title, avatarUrl, userName }: TopBarProps) {
  const initials = userName
    ? userName.charAt(0).toUpperCase()
    : '?';

  const [unseenCount, setUnseenCount] = useState(0);

  const fetchCount = useCallback(async () => {
    try {
      const count = await getUnseenNotificationCount();
      setUnseenCount(count);
    } catch {
      // ignore errors for badge count
    }
  }, []);

  useEffect(() => {
    fetchCount();
    const interval = setInterval(fetchCount, 60000);
    const handleFocus = () => fetchCount();
    window.addEventListener('focus', handleFocus);
    return () => {
      clearInterval(interval);
      window.removeEventListener('focus', handleFocus);
    };
  }, [fetchCount]);

  return (
    <header
      className="fixed top-0 left-0 right-0 z-50 flex h-14 items-center justify-between border-b border-gray-200 bg-white px-4 shadow-sm dark:bg-dark-surface dark:border-dark-border dark:text-dark-text"
      style={{ paddingTop: 'env(safe-area-inset-top)' }}
    >
      <h1 className="text-lg font-medium truncate">{title}</h1>
      <div className="flex items-center gap-2">
        <a
          href="/search/"
          className="flex h-10 w-10 items-center justify-center rounded-full"
          aria-label="Search"
        >
          <Search size={22} />
        </a>
        <a
          href="/notifications/"
          className="relative flex h-10 w-10 items-center justify-center rounded-full"
          aria-label="Notifications"
          data-testid="notification-bell"
        >
          <Bell size={22} />
          {unseenCount > 0 && (
            <span
              data-testid="notification-badge"
              className="absolute top-1 right-1 flex items-center justify-center min-w-[18px] h-[18px] rounded-full bg-red-500 text-white text-[10px] font-bold px-1"
            >
              {unseenCount > 99 ? '99+' : unseenCount}
            </span>
          )}
        </a>
        <a
          href="/more/"
          className="flex h-8 w-8 items-center justify-center rounded-full bg-gray-200 overflow-hidden"
          aria-label="Profile"
        >
          {avatarUrl ? (
            <img src={avatarUrl} alt="" className="h-full w-full object-cover" />
          ) : (
            <span className="text-sm font-medium text-gray-600">{initials}</span>
          )}
        </a>
      </div>
    </header>
  );
}
