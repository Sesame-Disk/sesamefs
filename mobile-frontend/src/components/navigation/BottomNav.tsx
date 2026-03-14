import React from 'react';
import { Library, Share2, Users, Star, MoreHorizontal } from 'lucide-react';

interface BottomNavProps {
  currentPath: string;
}

const tabs = [
  { label: 'Libraries', icon: Library, href: '/libraries/' },
  { label: 'Shared', icon: Share2, href: '/shared/' },
  { label: 'Groups', icon: Users, href: '/groups/' },
  { label: 'Starred', icon: Star, href: '/starred/' },
  { label: 'More', icon: MoreHorizontal, href: '/more/' },
] as const;

export default function BottomNav({ currentPath }: BottomNavProps) {
  return (
    <nav
      className="fixed bottom-0 left-0 right-0 z-50 flex h-16 items-center justify-around border-t border-gray-200 bg-white shadow-lg dark:bg-dark-surface dark:border-dark-border"
      style={{ paddingBottom: 'env(safe-area-inset-bottom)' }}
    >
      {tabs.map(({ label, icon: Icon, href }) => {
        const isActive = currentPath.startsWith(href);
        return (
          <a
            key={href}
            href={href}
            className={`flex min-h-[44px] min-w-[44px] flex-col items-center justify-center gap-0.5 ${
              isActive ? 'text-[var(--color-primary)]' : 'text-gray-400'
            }`}
          >
            <Icon size={24} fill={isActive ? 'currentColor' : 'none'} />
            <span className="text-xs">{label}</span>
          </a>
        );
      })}
    </nav>
  );
}
