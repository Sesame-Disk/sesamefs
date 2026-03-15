import React from 'react';
import { ExternalLink, Pencil, ArrowRightLeft, Trash2, LogOut } from 'lucide-react';
import BottomSheet from '../ui/BottomSheet';
import type { Group } from '../../lib/api';

interface GroupContextMenuProps {
  isOpen: boolean;
  onClose: () => void;
  group: Group | null;
  isOwner: boolean;
  isAdmin: boolean;
  onOpen: () => void;
  onRename: () => void;
  onTransfer: () => void;
  onDelete: () => void;
  onLeave: () => void;
}

function MenuItem({ icon, label, onClick, danger }: { icon: React.ReactNode; label: string; onClick: () => void; danger?: boolean }) {
  return (
    <button
      onClick={onClick}
      className={`flex items-center gap-3 w-full px-4 py-3 min-h-[44px] text-left hover:bg-gray-50 ${danger ? 'text-red-500' : 'text-text'}`}
    >
      {icon}
      <span className="text-base">{label}</span>
    </button>
  );
}

export default function GroupContextMenu({
  isOpen,
  onClose,
  group,
  isOwner,
  isAdmin,
  onOpen,
  onRename,
  onTransfer,
  onDelete,
  onLeave,
}: GroupContextMenuProps) {
  if (!group) return null;

  const handleAction = (action: () => void) => {
    onClose();
    action();
  };

  return (
    <BottomSheet isOpen={isOpen} onClose={onClose} title={group.name}>
      <div className="flex flex-col -mx-6 -mb-6">
        <MenuItem
          icon={<ExternalLink className="w-5 h-5" />}
          label="Open"
          onClick={() => handleAction(onOpen)}
        />
        {(isOwner || isAdmin) && (
          <MenuItem
            icon={<Pencil className="w-5 h-5" />}
            label="Rename"
            onClick={() => handleAction(onRename)}
          />
        )}
        {isOwner && (
          <MenuItem
            icon={<ArrowRightLeft className="w-5 h-5" />}
            label="Transfer"
            onClick={() => handleAction(onTransfer)}
          />
        )}
        {isOwner && (
          <MenuItem
            icon={<Trash2 className="w-5 h-5" />}
            label="Delete"
            onClick={() => handleAction(onDelete)}
            danger
          />
        )}
        {!isOwner && (
          <MenuItem
            icon={<LogOut className="w-5 h-5" />}
            label="Leave"
            onClick={() => handleAction(onLeave)}
            danger
          />
        )}
      </div>
    </BottomSheet>
  );
}
