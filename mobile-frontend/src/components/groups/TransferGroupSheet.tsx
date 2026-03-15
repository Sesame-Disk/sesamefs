import React, { useState } from 'react';
import { Search } from 'lucide-react';
import BottomSheet from '../ui/BottomSheet';
import { searchUsers } from '../../lib/api';
import type { SearchedUser } from '../../lib/api';

interface TransferGroupSheetProps {
  isOpen: boolean;
  onClose: () => void;
  groupName: string;
  onTransfer: (email: string) => Promise<void>;
}

export default function TransferGroupSheet({ isOpen, onClose, groupName, onTransfer }: TransferGroupSheetProps) {
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<SearchedUser[]>([]);
  const [selected, setSelected] = useState<SearchedUser | null>(null);
  const [loading, setLoading] = useState(false);
  const [searching, setSearching] = useState(false);
  const [error, setError] = useState('');

  const handleSearch = async (q: string) => {
    setQuery(q);
    setSelected(null);
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

  const handleTransfer = async () => {
    if (!selected) return;
    setLoading(true);
    setError('');
    try {
      await onTransfer(selected.email);
      setQuery('');
      setResults([]);
      setSelected(null);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to transfer group');
    } finally {
      setLoading(false);
    }
  };

  return (
    <BottomSheet isOpen={isOpen} onClose={onClose} title="Transfer Group">
      <p className="text-gray-600 mb-3">
        Transfer <strong>{groupName}</strong> to another user:
      </p>
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
      {searching && <p className="text-sm text-gray-400 mb-2">Searching...</p>}
      {results.length > 0 && (
        <div className="max-h-48 overflow-y-auto border border-gray-200 rounded-lg mb-3">
          {results.map((user) => (
            <button
              key={user.email}
              onClick={() => setSelected(user)}
              className={`flex items-center gap-3 w-full px-3 py-2 min-h-[44px] text-left ${
                selected?.email === user.email ? 'bg-primary/10' : 'hover:bg-gray-50'
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
            </button>
          ))}
        </div>
      )}
      {error && <p role="alert" className="text-red-500 text-sm mb-3">{error}</p>}
      <button
        onClick={handleTransfer}
        disabled={loading || !selected}
        className="w-full bg-primary-button text-white rounded-lg py-3 min-h-[44px] font-medium disabled:opacity-50"
      >
        {loading ? 'Transferring...' : 'Transfer'}
      </button>
    </BottomSheet>
  );
}
