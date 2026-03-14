import React, { useState, useEffect } from 'react';
import BottomSheet from '../ui/BottomSheet';
import { getSortPreference, setSortPreference } from '../../lib/sortPreference';
import type { SortField } from '../../lib/sortPreference';

interface SortPreferenceSheetProps {
  isOpen: boolean;
  onClose: () => void;
}

const sortOptions: { field: SortField; label: string }[] = [
  { field: 'name', label: 'Name' },
  { field: 'date', label: 'Last Modified' },
  { field: 'size', label: 'Size' },
];

export default function SortPreferenceSheet({ isOpen, onClose }: SortPreferenceSheetProps) {
  const [current, setCurrent] = useState(getSortPreference());

  useEffect(() => {
    if (isOpen) setCurrent(getSortPreference());
  }, [isOpen]);

  const handleSelect = (field: SortField) => {
    const newDirection = field === current.field && current.direction === 'asc' ? 'desc' : 'asc';
    const pref = { field, direction: newDirection as 'asc' | 'desc' };
    setSortPreference(pref);
    setCurrent(pref);
    onClose();
  };

  return (
    <BottomSheet isOpen={isOpen} onClose={onClose} title="Sort Preference">
      <div className="flex flex-col">
        {sortOptions.map(({ field, label }) => (
          <button
            key={field}
            onClick={() => handleSelect(field)}
            className={`flex items-center justify-between px-2 py-3 min-h-[44px] rounded-lg ${
              current.field === field ? 'text-primary bg-primary/5' : 'text-text dark:text-dark-text'
            }`}
          >
            <span className="text-sm">{label}</span>
            {current.field === field && (
              <span className="text-xs font-medium">
                {current.direction === 'asc' ? 'Ascending' : 'Descending'}
              </span>
            )}
          </button>
        ))}
      </div>
    </BottomSheet>
  );
}
