import React, { useState, useEffect } from 'react';
import BottomSheet from '../ui/BottomSheet';

interface RenameGroupSheetProps {
  isOpen: boolean;
  onClose: () => void;
  currentName: string;
  onRename: (newName: string) => Promise<void>;
}

export default function RenameGroupSheet({ isOpen, onClose, currentName, onRename }: RenameGroupSheetProps) {
  const [name, setName] = useState(currentName);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (isOpen) {
      setName(currentName);
      setError('');
    }
  }, [isOpen, currentName]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = name.trim();
    if (!trimmed || trimmed === currentName) return;

    setLoading(true);
    setError('');
    try {
      await onRename(trimmed);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to rename group');
    } finally {
      setLoading(false);
    }
  };

  return (
    <BottomSheet isOpen={isOpen} onClose={onClose} title="Rename Group">
      <form onSubmit={handleSubmit}>
        <input
          type="text"
          placeholder="Group name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="w-full border border-gray-300 rounded-lg px-4 py-3 min-h-[44px] text-base focus:outline-none focus:ring-2 focus:ring-primary mb-3"
          autoFocus
        />
        {error && <p role="alert" className="text-red-500 text-sm mb-3">{error}</p>}
        <button
          type="submit"
          disabled={loading || !name.trim() || name.trim() === currentName}
          className="w-full bg-primary-button text-white rounded-lg py-3 min-h-[44px] font-medium disabled:opacity-50"
        >
          {loading ? 'Renaming...' : 'Rename'}
        </button>
      </form>
    </BottomSheet>
  );
}
