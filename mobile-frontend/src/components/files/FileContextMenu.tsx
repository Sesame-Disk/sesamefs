import React from 'react';
import { Star, Share2, Pencil, Copy, FolderInput, Download, Info, Trash2, History, Tag, Lock, Unlock } from 'lucide-react';
import BottomSheet from '../ui/BottomSheet';
import type { Dirent } from '../../lib/models';

interface FileContextMenuProps {
  isOpen: boolean;
  onClose: () => void;
  dirent: Dirent | null;
  repoId: string;
  path: string;
  onStar: () => void;
  onShare: () => void;
  onRename: () => void;
  onCopy: () => void;
  onMove: () => void;
  onDownload: () => void;
  onDetails: () => void;
  onHistory: () => void;
  onTags: () => void;
  onDelete: () => void;
  onLock?: () => void;
  onUnlock?: () => void;
}

interface MenuItemProps {
  icon: React.ReactNode;
  label: string;
  onClick: () => void;
  danger?: boolean;
  disabled?: boolean;
}

function MenuItem({ icon, label, onClick, danger, disabled }: MenuItemProps) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className={`flex items-center gap-3 w-full px-4 py-3 min-h-[44px] text-left hover:bg-gray-50 ${danger ? 'text-red-500' : 'text-text'} ${disabled ? 'opacity-50 cursor-not-allowed' : ''}`}
    >
      {icon}
      <span className="text-base">{label}</span>
    </button>
  );
}

export default function FileContextMenu({
  isOpen,
  onClose,
  dirent,
  onStar,
  onShare,
  onRename,
  onCopy,
  onMove,
  onDownload,
  onDetails,
  onHistory,
  onTags,
  onDelete,
  onLock,
  onUnlock,
}: FileContextMenuProps) {
  if (!dirent) return null;

  const handleAction = (action: () => void) => {
    onClose();
    action();
  };

  const isFile = dirent.type === 'file';
  const hasWritePerm = dirent.permission === 'rw';
  const isLocked = !!dirent.is_locked;
  const lockedByMe = !!dirent.locked_by_me;

  return (
    <BottomSheet isOpen={isOpen} onClose={onClose} title={dirent.name}>
      <div className="flex flex-col -mx-6 -mb-6">
        <MenuItem
          icon={<Star className="w-5 h-5" />}
          label={dirent.starred ? 'Unstar' : 'Star'}
          onClick={() => handleAction(onStar)}
        />
        <MenuItem
          icon={<Share2 className="w-5 h-5" />}
          label="Share"
          onClick={() => handleAction(onShare)}
        />
        <MenuItem
          icon={<Pencil className="w-5 h-5" />}
          label="Rename"
          onClick={() => handleAction(onRename)}
        />
        <MenuItem
          icon={<Copy className="w-5 h-5" />}
          label="Copy"
          onClick={() => handleAction(onCopy)}
        />
        <MenuItem
          icon={<FolderInput className="w-5 h-5" />}
          label="Move"
          onClick={() => handleAction(onMove)}
        />
        <MenuItem
          icon={<Download className="w-5 h-5" />}
          label="Download"
          onClick={() => handleAction(onDownload)}
        />
        <MenuItem
          icon={<Info className="w-5 h-5" />}
          label="Details"
          onClick={() => handleAction(onDetails)}
        />
        {isFile && (
          <MenuItem
            icon={<History className="w-5 h-5" />}
            label="History"
            onClick={() => handleAction(onHistory)}
          />
        )}
        {isFile && (
          <MenuItem
            icon={<Tag className="w-5 h-5" />}
            label="Tags"
            onClick={() => handleAction(onTags)}
          />
        )}
        {isFile && hasWritePerm && !isLocked && onLock && (
          <MenuItem
            icon={<Lock className="w-5 h-5" />}
            label="Lock"
            onClick={() => handleAction(onLock)}
          />
        )}
        {isFile && isLocked && lockedByMe && onUnlock && (
          <MenuItem
            icon={<Unlock className="w-5 h-5" />}
            label="Unlock"
            onClick={() => handleAction(onUnlock)}
          />
        )}
        {isFile && isLocked && !lockedByMe && (
          <MenuItem
            icon={<Lock className="w-5 h-5" />}
            label={`Locked by ${dirent.lock_owner_name || 'another user'}`}
            onClick={() => {}}
            disabled
          />
        )}
        <MenuItem
          icon={<Trash2 className="w-5 h-5" />}
          label="Delete"
          onClick={() => handleAction(onDelete)}
          danger
        />
      </div>
    </BottomSheet>
  );
}
