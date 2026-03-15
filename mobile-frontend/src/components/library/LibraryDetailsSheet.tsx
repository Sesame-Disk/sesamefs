import React from 'react';
import BottomSheet from '../ui/BottomSheet';
import { bytesToSize } from '../../lib/models';
import type { Repo } from '../../lib/models';

interface LibraryDetailsSheetProps {
  isOpen: boolean;
  onClose: () => void;
  repo: Repo | null;
}

export default function LibraryDetailsSheet({ isOpen, onClose, repo }: LibraryDetailsSheetProps) {
  if (!repo) return null;

  const details = [
    { label: 'Name', value: repo.repo_name },
    { label: 'Owner', value: repo.owner_name || repo.owner_email },
    { label: 'Size', value: bytesToSize(repo.size) },
    { label: 'Encrypted', value: repo.encrypted ? 'Yes' : 'No' },
    { label: 'Last Modified', value: new Date(repo.last_modified).toLocaleString() },
  ];

  return (
    <BottomSheet isOpen={isOpen} onClose={onClose} title="Library Details">
      <div className="flex flex-col gap-3">
        {details.map(({ label, value }) => (
          <div key={label} className="flex justify-between items-center py-2 border-b border-gray-100 dark:border-dark-border last:border-0">
            <span className="text-sm text-gray-500 dark:text-gray-400">{label}</span>
            <span className="text-sm font-medium text-text dark:text-dark-text text-right max-w-[60%] truncate">{value}</span>
          </div>
        ))}
      </div>
    </BottomSheet>
  );
}
