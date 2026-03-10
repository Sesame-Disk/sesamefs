import React, { useState, useEffect, useRef } from 'react';
import { Search, X } from 'lucide-react';
import { searchUsers } from '../../lib/api';
import type { SearchedUser } from '../../lib/api';

interface UserPickerProps {
  selectedUsers: SearchedUser[];
  onSelect: (user: SearchedUser) => void;
  onRemove: (email: string) => void;
}

export default function UserPicker({ selectedUsers, onSelect, onRemove }: UserPickerProps) {
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<SearchedUser[]>([]);
  const [loading, setLoading] = useState(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current);

    if (query.length < 2) {
      setResults([]);
      return;
    }

    debounceRef.current = setTimeout(async () => {
      setLoading(true);
      try {
        const users = await searchUsers(query);
        const selectedEmails = new Set(selectedUsers.map(u => u.email));
        setResults(users.filter(u => !selectedEmails.has(u.email)));
      } catch {
        setResults([]);
      } finally {
        setLoading(false);
      }
    }, 300);

    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, [query, selectedUsers]);

  const handleSelect = (user: SearchedUser) => {
    onSelect(user);
    setQuery('');
    setResults([]);
  };

  return (
    <div>
      {/* Selected users as chips */}
      {selectedUsers.length > 0 && (
        <div className="flex flex-wrap gap-2 mb-3" data-testid="selected-chips">
          {selectedUsers.map(user => (
            <span
              key={user.email}
              className="inline-flex items-center gap-1 bg-primary/10 text-primary text-sm px-3 py-1 rounded-full"
            >
              {user.name || user.email}
              <button
                onClick={() => onRemove(user.email)}
                className="min-h-[24px] min-w-[24px] flex items-center justify-center"
                aria-label={`Remove ${user.name || user.email}`}
              >
                <X className="w-3 h-3" />
              </button>
            </span>
          ))}
        </div>
      )}

      {/* Search input */}
      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
        <input
          type="text"
          value={query}
          onChange={e => setQuery(e.target.value)}
          placeholder="Search users by name or email..."
          className="w-full pl-10 pr-4 py-2 border border-gray-200 rounded-lg text-sm focus:outline-none focus:border-primary"
          aria-label="Search users"
        />
      </div>

      {/* Results */}
      {loading && <p className="text-gray-400 text-sm py-2">Searching...</p>}
      {results.length > 0 && (
        <div className="mt-2 border border-gray-100 rounded-lg max-h-48 overflow-auto">
          {results.map(user => (
            <button
              key={user.email}
              onClick={() => handleSelect(user)}
              className="flex items-center gap-3 w-full px-3 py-2 min-h-[44px] text-left hover:bg-gray-50"
            >
              <img
                src={user.avatar_url}
                alt=""
                className="w-8 h-8 rounded-full bg-gray-200"
              />
              <div className="min-w-0">
                <p className="text-sm text-text truncate">{user.name}</p>
                <p className="text-xs text-gray-400 truncate">{user.email}</p>
              </div>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
