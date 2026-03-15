import React from 'react';
import { FolderOpen, Share2, Info, Pencil, Trash2, ArrowRightLeft, LogOut } from 'lucide-react';
import BottomSheet from '../ui/BottomSheet';
import type { Repo } from '../../lib/models';

type ActionKey = 'onOpen' | 'onShare' | 'onRename' | 'onDelete' | 'onTransfer' | 'onLeave' | 'onDetails';

interface ActionDef {
  key: string;
  label: string;
  icon: React.ComponentType<{ size?: number }>;
  action: ActionKey;
  ownerOnly?: boolean;
  sharedOnly?: boolean;
  className?: string;
}

const allActions: ActionDef[] = [
  { key: 'open', label: 'Open', icon: FolderOpen, action: 'onOpen' },
  { key: 'share', label: 'Share', icon: Share2, action: 'onShare' },
  { key: 'rename', label: 'Rename', icon: Pencil, action: 'onRename', ownerOnly: true },
  { key: 'transfer', label: 'Transfer', icon: ArrowRightLeft, action: 'onTransfer', ownerOnly: true },
  { key: 'details', label: 'Details', icon: Info, action: 'onDetails' },
  { key: 'leave', label: 'Leave Share', icon: LogOut, action: 'onLeave', sharedOnly: true },
  { key: 'delete', label: 'Delete', icon: Trash2, action: 'onDelete', ownerOnly: true, className: 'text-red-500' },
];

interface LibraryContextMenuProps {
  isOpen: boolean;
  onClose: () => void;
  repo: Repo | null;
  isOwner: boolean;
  onOpen: (repo: Repo) => void;
  onShare: (repo: Repo) => void;
  onRename: (repo: Repo) => void;
  onDelete: (repo: Repo) => void;
  onTransfer: (repo: Repo) => void;
  onLeave: (repo: Repo) => void;
  onDetails: (repo: Repo) => void;
}

export default function LibraryContextMenu({
  isOpen,
  onClose,
  repo,
  isOwner,
  onOpen,
  onShare,
  onRename,
  onDelete,
  onTransfer,
  onLeave,
  onDetails,
}: LibraryContextMenuProps) {
  const handlers: Record<ActionKey, (repo: Repo) => void> = {
    onOpen, onShare, onRename, onDelete, onTransfer, onLeave, onDetails,
  };

  const visibleActions = allActions.filter((a) => {
    if (a.ownerOnly && !isOwner) return false;
    if (a.sharedOnly && isOwner) return false;
    return true;
  });

  return (
    <BottomSheet isOpen={isOpen} onClose={onClose} title={repo?.repo_name}>
      <div className="flex flex-col">
        {visibleActions.map(({ key, label, icon: Icon, action, className }) => (
          <button
            key={key}
            onClick={() => {
              if (repo) handlers[action](repo);
              onClose();
            }}
            className={`flex items-center gap-3 px-2 py-3 min-h-[44px] hover:bg-gray-50 dark:hover:bg-dark-border rounded-lg ${
              className || 'text-text dark:text-dark-text'
            }`}
          >
            <Icon size={20} />
            <span className="text-sm">{label}</span>
          </button>
        ))}
      </div>
    </BottomSheet>
  );
}
