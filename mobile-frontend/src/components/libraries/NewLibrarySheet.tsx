import React, { useState } from 'react';
import BottomSheet from '../ui/BottomSheet';
import { createRepo } from '../../lib/api';

interface NewLibrarySheetProps {
  isOpen: boolean;
  onClose: () => void;
  onCreated: () => void;
}

export default function NewLibrarySheet({ isOpen, onClose, onCreated }: NewLibrarySheetProps) {
  const [name, setName] = useState('');
  const [encrypted, setEncrypted] = useState(false);
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const reset = () => {
    setName('');
    setEncrypted(false);
    setPassword('');
    setConfirmPassword('');
    setError('');
  };

  const handleClose = () => {
    reset();
    onClose();
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');

    const trimmedName = name.trim();
    if (!trimmedName) {
      setError('Library name is required');
      return;
    }

    if (encrypted) {
      if (!password) {
        setError('Password is required for encrypted libraries');
        return;
      }
      if (password !== confirmPassword) {
        setError('Passwords do not match');
        return;
      }
    }

    setSubmitting(true);
    try {
      await createRepo(trimmedName, encrypted, encrypted ? password : undefined);
      reset();
      onCreated();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create library');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <BottomSheet isOpen={isOpen} onClose={handleClose} title="New Library">
      <form onSubmit={handleSubmit} className="flex flex-col gap-4">
        <div>
          <label htmlFor="lib-name" className="block text-sm font-medium text-text dark:text-dark-text mb-1">
            Name
          </label>
          <input
            id="lib-name"
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Library name"
            className="w-full px-3 py-2 border border-gray-300 dark:border-dark-border rounded-lg bg-white dark:bg-dark-surface text-text dark:text-dark-text min-h-[44px]"
            autoFocus
          />
        </div>

        <label className="flex items-center gap-3 min-h-[44px] cursor-pointer">
          <input
            type="checkbox"
            checked={encrypted}
            onChange={(e) => setEncrypted(e.target.checked)}
            className="w-5 h-5"
          />
          <span className="text-sm text-text dark:text-dark-text">Encrypt this library</span>
        </label>

        {encrypted && (
          <>
            <div>
              <label htmlFor="lib-password" className="block text-sm font-medium text-text dark:text-dark-text mb-1">
                Password
              </label>
              <input
                id="lib-password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="Enter password"
                className="w-full px-3 py-2 border border-gray-300 dark:border-dark-border rounded-lg bg-white dark:bg-dark-surface text-text dark:text-dark-text min-h-[44px]"
              />
            </div>
            <div>
              <label htmlFor="lib-confirm-password" className="block text-sm font-medium text-text dark:text-dark-text mb-1">
                Confirm Password
              </label>
              <input
                id="lib-confirm-password"
                type="password"
                value={confirmPassword}
                onChange={(e) => setConfirmPassword(e.target.value)}
                placeholder="Confirm password"
                className="w-full px-3 py-2 border border-gray-300 dark:border-dark-border rounded-lg bg-white dark:bg-dark-surface text-text dark:text-dark-text min-h-[44px]"
              />
            </div>
          </>
        )}

        {error && <p className="text-red-500 text-sm" role="alert">{error}</p>}

        <button
          type="submit"
          disabled={submitting}
          className="w-full py-3 bg-primary text-white rounded-lg font-medium min-h-[44px] disabled:opacity-50"
        >
          {submitting ? 'Creating...' : 'Create Library'}
        </button>
      </form>
    </BottomSheet>
  );
}
