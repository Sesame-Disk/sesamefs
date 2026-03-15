import React, { useState } from 'react';
import BottomSheet from '../ui/BottomSheet';

interface DeleteGroupSheetProps {
  isOpen: boolean;
  onClose: () => void;
  groupName: string;
  onDelete: () => Promise<void>;
}

export default function DeleteGroupSheet({ isOpen, onClose, groupName, onDelete }: DeleteGroupSheetProps) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const handleDelete = async () => {
    setLoading(true);
    setError('');
    try {
      await onDelete();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete group');
    } finally {
      setLoading(false);
    }
  };

  return (
    <BottomSheet isOpen={isOpen} onClose={onClose} title="Delete Group">
      <p className="text-gray-600 mb-4">
        Are you sure you want to delete <strong>{groupName}</strong>? This action cannot be undone.
      </p>
      {error && <p role="alert" className="text-red-500 text-sm mb-3">{error}</p>}
      <div className="flex gap-3">
        <button
          onClick={onClose}
          disabled={loading}
          className="flex-1 border border-gray-300 rounded-lg py-3 min-h-[44px] font-medium"
        >
          Cancel
        </button>
        <button
          onClick={handleDelete}
          disabled={loading}
          className="flex-1 bg-red-500 text-white rounded-lg py-3 min-h-[44px] font-medium disabled:opacity-50"
        >
          {loading ? 'Deleting...' : 'Delete'}
        </button>
      </div>
    </BottomSheet>
  );
}
