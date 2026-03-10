import React from 'react';
import { Sun, Moon, Monitor } from 'lucide-react';
import { useTheme } from '../../lib/hooks/useTheme';
import type { ThemeOption } from '../../lib/hooks/useTheme';

const themeOptions: { value: ThemeOption; label: string; icon: typeof Sun }[] = [
  { value: 'light', label: 'Light', icon: Sun },
  { value: 'dark', label: 'Dark', icon: Moon },
  { value: 'system', label: 'System', icon: Monitor },
];

export default function MorePage() {
  const { theme, setTheme } = useTheme();

  return (
    <div className="flex flex-col p-4 gap-6">
      <h1 className="text-xl font-medium text-text">Settings</h1>

      {/* Theme selector */}
      <div>
        <h2 className="text-sm font-medium text-gray-500 dark:text-gray-400 mb-2">Appearance</h2>
        <div className="bg-white dark:bg-dark-surface rounded-lg border border-gray-200 dark:border-dark-border overflow-hidden">
          {themeOptions.map(({ value, label, icon: Icon }) => (
            <button
              key={value}
              onClick={() => setTheme(value)}
              className={`w-full flex items-center gap-3 px-4 py-3 min-h-[44px] text-left border-b last:border-b-0 border-gray-200 dark:border-dark-border ${
                theme === value
                  ? 'text-primary bg-primary/5'
                  : 'text-text dark:text-dark-text'
              }`}
            >
              <Icon size={20} />
              <span className="flex-1">{label}</span>
              {theme === value && (
                <span className="text-primary text-sm font-medium">Active</span>
              )}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
