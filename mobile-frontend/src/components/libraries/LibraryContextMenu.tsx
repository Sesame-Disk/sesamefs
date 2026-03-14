import React from 'react';
import { FolderOpen, Share2, Info } from 'lucide-react';
import BottomSheet from '../ui/BottomSheet';
import type { Repo } from '../../lib/models';

interface LibraryContextMenuProps {
  isOpen: boolean;
  onClose: () => void;
  repo: Repo | null;
  onOpen: (repo: Repo) => void;
  onShare: (repo: Repo) => void;
  onDetails: (repo: Repo) => void;
}

const actions = [
  { key: 'open', label: 'Open', icon: FolderOpen, action: 'onOpen' as const },
  { key: 'share', label: 'Share', icon: Share2, action: 'onShare' as const },
  { key: 'details', label: 'Details', icon: Info, action: 'onDetails' as const },
];

export default function LibraryContextMenu({
  isOpen,
  onClose,
  repo,
  onOpen,
  onShare,
  onDetails,
}: LibraryContextMenuProps) {
  const handlers = { onOpen, onShare, onDetails };

  return (
    <BottomSheet isOpen={isOpen} onClose={onClose} title={repo?.repo_name}>
      <div className="flex flex-col">
        {actions.map(({ key, label, icon: Icon, action }) => (
          <button
            key={key}
            onClick={() => {
              if (repo) handlers[action](repo);
              onClose();
            }}
            className="flex items-center gap-3 px-2 py-3 min-h-[44px] text-text dark:text-dark-text hover:bg-gray-50 dark:hover:bg-dark-border rounded-lg"
          >
            <Icon size={20} />
            <span className="text-sm">{label}</span>
          </button>
        ))}
      </div>
    </BottomSheet>
  );
}
