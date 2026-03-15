import React, { useState } from 'react';
import BottomSheet from '../ui/BottomSheet';
import { deleteRepo } from '../../lib/api';
import type { Repo } from '../../lib/models';

interface DeleteLibrarySheetProps {
  isOpen: boolean;
  onClose: () => void;
  repo: Repo | null;
  onDeleted: () => void;
}

export default function DeleteLibrarySheet({ isOpen, onClose, repo, onDeleted }: DeleteLibrarySheetProps) {
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  const handleDelete = async () => {
    if (!repo) return;

    setSubmitting(true);
    setError('');
    try {
      await deleteRepo(repo.repo_id);
      onDeleted();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete library');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <BottomSheet isOpen={isOpen} onClose={onClose} title="Delete Library">
      <div className="flex flex-col gap-4">
        <p className="text-sm text-text dark:text-dark-text">
          Are you sure you want to delete{' '}
          <span className="font-semibold">{repo?.repo_name}</span>?
        </p>
        <p className="text-sm text-red-500">
          This action is permanent and cannot be undone. All files in this library will be deleted.
        </p>

        {error && <p className="text-red-500 text-sm" role="alert">{error}</p>}

        <div className="flex gap-3">
          <button
            onClick={onClose}
            className="flex-1 py-3 border border-gray-300 dark:border-dark-border text-text dark:text-dark-text rounded-lg font-medium min-h-[44px]"
          >
            Cancel
          </button>
          <button
            onClick={handleDelete}
            disabled={submitting}
            className="flex-1 py-3 bg-red-600 text-white rounded-lg font-medium min-h-[44px] disabled:opacity-50"
          >
            {submitting ? 'Deleting...' : 'Delete'}
          </button>
        </div>
      </div>
    </BottomSheet>
  );
}
