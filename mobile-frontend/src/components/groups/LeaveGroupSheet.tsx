import React, { useState } from 'react';
import BottomSheet from '../ui/BottomSheet';

interface LeaveGroupSheetProps {
  isOpen: boolean;
  onClose: () => void;
  groupName: string;
  onLeave: () => Promise<void>;
}

export default function LeaveGroupSheet({ isOpen, onClose, groupName, onLeave }: LeaveGroupSheetProps) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const handleLeave = async () => {
    setLoading(true);
    setError('');
    try {
      await onLeave();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to leave group');
    } finally {
      setLoading(false);
    }
  };

  return (
    <BottomSheet isOpen={isOpen} onClose={onClose} title="Leave Group">
      <p className="text-gray-600 mb-4">
        Are you sure you want to leave <strong>{groupName}</strong>?
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
          onClick={handleLeave}
          disabled={loading}
          className="flex-1 bg-red-500 text-white rounded-lg py-3 min-h-[44px] font-medium disabled:opacity-50"
        >
          {loading ? 'Leaving...' : 'Leave'}
        </button>
      </div>
    </BottomSheet>
  );
}
