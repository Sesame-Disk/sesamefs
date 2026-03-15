import React, { useState } from 'react';
import { Search, X } from 'lucide-react';
import BottomSheet from '../ui/BottomSheet';
import { searchUsers } from '../../lib/api';
import type { SearchedUser } from '../../lib/api';

interface AddMemberSheetProps {
  isOpen: boolean;
  onClose: () => void;
  onAdd: (emails: string[]) => Promise<void>;
}

export default function AddMemberSheet({ isOpen, onClose, onAdd }: AddMemberSheetProps) {
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<SearchedUser[]>([]);
  const [selected, setSelected] = useState<SearchedUser[]>([]);
  const [loading, setLoading] = useState(false);
  const [searching, setSearching] = useState(false);
  const [error, setError] = useState('');

  const handleSearch = async (q: string) => {
    setQuery(q);
    if (q.trim().length < 1) {
      setResults([]);
      return;
    }
    setSearching(true);
    try {
      const users = await searchUsers(q.trim());
      setResults(users);
    } catch {
      setResults([]);
    } finally {
      setSearching(false);
    }
  };

  const toggleUser = (user: SearchedUser) => {
    setSelected((prev) => {
      const exists = prev.find((u) => u.email === user.email);
      return exists ? prev.filter((u) => u.email !== user.email) : [...prev, user];
    });
  };

  const handleAdd = async () => {
    if (selected.length === 0) return;
    setLoading(true);
    setError('');
    try {
      await onAdd(selected.map((u) => u.email));
      setQuery('');
      setResults([]);
      setSelected([]);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add members');
    } finally {
      setLoading(false);
    }
  };

  return (
    <BottomSheet isOpen={isOpen} onClose={onClose} title="Add Members">
      <div className="relative mb-3">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
        <input
          type="text"
          placeholder="Search users..."
          value={query}
          onChange={(e) => handleSearch(e.target.value)}
          className="w-full border border-gray-300 rounded-lg pl-10 pr-4 py-3 min-h-[44px] text-base focus:outline-none focus:ring-2 focus:ring-primary"
          autoFocus
        />
      </div>

      {selected.length > 0 && (
        <div className="flex flex-wrap gap-2 mb-3">
          {selected.map((user) => (
            <span
              key={user.email}
              className="inline-flex items-center gap-1 bg-primary/10 text-primary text-sm px-2 py-1 rounded-full"
            >
              {user.name}
              <button onClick={() => toggleUser(user)} className="p-0.5">
                <X className="w-3 h-3" />
              </button>
            </span>
          ))}
        </div>
      )}

      {searching && <p className="text-sm text-gray-400 mb-2">Searching...</p>}
      {results.length > 0 && (
        <div className="max-h-48 overflow-y-auto border border-gray-200 rounded-lg mb-3">
          {results.map((user) => {
            const isSelected = selected.some((u) => u.email === user.email);
            return (
              <button
                key={user.email}
                onClick={() => toggleUser(user)}
                className={`flex items-center gap-3 w-full px-3 py-2 min-h-[44px] text-left ${
                  isSelected ? 'bg-primary/10' : 'hover:bg-gray-50'
                }`}
              >
                {user.avatar_url ? (
                  <img src={user.avatar_url} alt="" className="w-8 h-8 rounded-full" />
                ) : (
                  <div className="w-8 h-8 rounded-full bg-gray-200" />
                )}
                <div className="flex-1 min-w-0">
                  <div className="text-sm font-medium truncate">{user.name}</div>
                  <div className="text-xs text-gray-500 truncate">{user.email}</div>
                </div>
                {isSelected && (
                  <span className="text-primary text-sm font-medium">Selected</span>
                )}
              </button>
            );
          })}
        </div>
      )}

      {error && <p role="alert" className="text-red-500 text-sm mb-3">{error}</p>}
      <button
        onClick={handleAdd}
        disabled={loading || selected.length === 0}
        className="w-full bg-primary-button text-white rounded-lg py-3 min-h-[44px] font-medium disabled:opacity-50"
      >
        {loading ? 'Adding...' : `Add ${selected.length > 0 ? `(${selected.length})` : ''}`}
      </button>
    </BottomSheet>
  );
}
