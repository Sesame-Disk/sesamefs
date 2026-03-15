import React, { useState, useCallback } from 'react';
import BottomSheet from '../ui/BottomSheet';
import { searchUsers, transferRepo } from '../../lib/api';
import type { SearchedUser } from '../../lib/api';
import type { Repo } from '../../lib/models';

interface TransferLibrarySheetProps {
  isOpen: boolean;
  onClose: () => void;
  repo: Repo | null;
  onTransferred: () => void;
}

export default function TransferLibrarySheet({ isOpen, onClose, repo, onTransferred }: TransferLibrarySheetProps) {
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<SearchedUser[]>([]);
  const [selected, setSelected] = useState<SearchedUser | null>(null);
  const [searching, setSearching] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  const handleSearch = useCallback(async (q: string) => {
    setQuery(q);
    setSelected(null);
    if (q.trim().length < 2) {
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
  }, []);

  const handleTransfer = async () => {
    if (!repo || !selected) return;

    setSubmitting(true);
    setError('');
    try {
      await transferRepo(repo.repo_id, selected.email);
      onTransferred();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to transfer library');
    } finally {
      setSubmitting(false);
    }
  };

  const handleClose = () => {
    setQuery('');
    setResults([]);
    setSelected(null);
    setError('');
    onClose();
  };

  return (
    <BottomSheet isOpen={isOpen} onClose={handleClose} title="Transfer Library">
      <div className="flex flex-col gap-4">
        <p className="text-sm text-text dark:text-dark-text">
          Transfer <span className="font-semibold">{repo?.repo_name}</span> to another user.
        </p>

        <div>
          <label htmlFor="transfer-search" className="block text-sm font-medium text-text dark:text-dark-text mb-1">
            Search user
          </label>
          <input
            id="transfer-search"
            type="text"
            value={query}
            onChange={(e) => handleSearch(e.target.value)}
            placeholder="Type a name or email..."
            className="w-full px-3 py-2 border border-gray-300 dark:border-dark-border rounded-lg bg-white dark:bg-dark-surface text-text dark:text-dark-text min-h-[44px]"
            autoFocus
          />
        </div>

        {searching && <p className="text-sm text-gray-500">Searching...</p>}

        {results.length > 0 && (
          <div className="flex flex-col max-h-48 overflow-auto">
            {results.map((user) => (
              <button
                key={user.email}
                onClick={() => setSelected(user)}
                className={`flex items-center gap-3 px-2 py-3 min-h-[44px] rounded-lg text-left ${
                  selected?.email === user.email
                    ? 'bg-primary/10 text-primary'
                    : 'text-text dark:text-dark-text hover:bg-gray-50 dark:hover:bg-dark-border'
                }`}
              >
                <img src={user.avatar_url} alt="" className="w-8 h-8 rounded-full" />
                <div className="flex flex-col min-w-0">
                  <span className="text-sm font-medium truncate">{user.name}</span>
                  <span className="text-xs text-gray-500 truncate">{user.email}</span>
                </div>
              </button>
            ))}
          </div>
        )}

        {error && <p className="text-red-500 text-sm" role="alert">{error}</p>}

        <button
          onClick={handleTransfer}
          disabled={submitting || !selected}
          className="w-full py-3 bg-primary text-white rounded-lg font-medium min-h-[44px] disabled:opacity-50"
        >
          {submitting ? 'Transferring...' : selected ? `Transfer to ${selected.name}` : 'Select a user'}
        </button>
      </div>
    </BottomSheet>
  );
}
