import React, { useState } from 'react';
import { createGroup } from '../../lib/api';

interface NewGroupDialogProps {
  open: boolean;
  onClose: () => void;
  onCreated: () => void;
}

export default function NewGroupDialog({ open, onClose, onCreated }: NewGroupDialogProps) {
  const [name, setName] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  if (!open) return null;

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) return;

    setLoading(true);
    setError('');
    try {
      await createGroup(trimmed);
      setName('');
      onCreated();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create group');
    } finally {
      setLoading(false);
    }
  };

  const handleBackdropClick = (e: React.MouseEvent) => {
    if (e.target === e.currentTarget) onClose();
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-end justify-center bg-black/40"
      onClick={handleBackdropClick}
    >
      <div className="w-full max-w-lg bg-white rounded-t-2xl p-6 animate-slide-up">
        <div className="w-10 h-1 bg-gray-300 rounded-full mx-auto mb-4" />
        <h2 className="text-lg font-semibold text-text mb-4">Create New Group</h2>
        <form onSubmit={handleCreate}>
          <input
            type="text"
            placeholder="Group name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="w-full border border-gray-300 rounded-lg px-4 py-3 min-h-[44px] text-base focus:outline-none focus:ring-2 focus:ring-primary mb-3"
            autoFocus
          />
          {error && (
            <p role="alert" className="text-red-500 text-sm mb-3">{error}</p>
          )}
          <button
            type="submit"
            disabled={loading || !name.trim()}
            className="w-full bg-primary-button text-white rounded-lg py-3 min-h-[44px] font-medium disabled:opacity-50"
          >
            {loading ? 'Creating...' : 'Create'}
          </button>
        </form>
      </div>
    </div>
  );
}
