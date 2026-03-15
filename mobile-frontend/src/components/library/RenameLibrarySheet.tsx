import React, { useState, useEffect, useRef } from 'react';
import BottomSheet from '../ui/BottomSheet';
import { renameRepo } from '../../lib/api';
import type { Repo } from '../../lib/models';

interface RenameLibrarySheetProps {
  isOpen: boolean;
  onClose: () => void;
  repo: Repo | null;
  onRenamed: () => void;
}

export default function RenameLibrarySheet({ isOpen, onClose, repo, onRenamed }: RenameLibrarySheetProps) {
  const [name, setName] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (isOpen && repo) {
      setName(repo.repo_name);
      setError('');
      setTimeout(() => inputRef.current?.select(), 100);
    }
  }, [isOpen, repo]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!repo) return;

    const trimmed = name.trim();
    if (!trimmed) {
      setError('Name cannot be empty');
      return;
    }
    if (trimmed === repo.repo_name) {
      setError('Name must be different');
      return;
    }

    setSubmitting(true);
    try {
      await renameRepo(repo.repo_id, trimmed);
      onRenamed();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to rename library');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <BottomSheet isOpen={isOpen} onClose={onClose} title="Rename Library">
      <form onSubmit={handleSubmit} className="flex flex-col gap-4">
        <div>
          <label htmlFor="rename-input" className="block text-sm font-medium text-text dark:text-dark-text mb-1">
            Name
          </label>
          <input
            ref={inputRef}
            id="rename-input"
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="w-full px-3 py-2 border border-gray-300 dark:border-dark-border rounded-lg bg-white dark:bg-dark-surface text-text dark:text-dark-text min-h-[44px]"
            autoFocus
          />
        </div>

        {error && <p className="text-red-500 text-sm" role="alert">{error}</p>}

        <button
          type="submit"
          disabled={submitting}
          className="w-full py-3 bg-primary text-white rounded-lg font-medium min-h-[44px] disabled:opacity-50"
        >
          {submitting ? 'Renaming...' : 'Rename'}
        </button>
      </form>
    </BottomSheet>
  );
}
