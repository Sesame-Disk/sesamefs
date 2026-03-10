import React from 'react';
import { Share2, FolderInput, Copy, Trash2, CheckSquare, XSquare } from 'lucide-react';
import type { Dirent } from '../../lib/models';

interface MultiSelectBarProps {
  selectedItems: Dirent[];
  totalItems: number;
  onSelectAll: () => void;
  onDeselectAll: () => void;
  onShare: () => void;
  onMove: () => void;
  onCopy: () => void;
  onDelete: () => void;
}

export default function MultiSelectBar({
  selectedItems,
  totalItems,
  onSelectAll,
  onDeselectAll,
  onShare,
  onMove,
  onCopy,
  onDelete,
}: MultiSelectBarProps) {
  if (selectedItems.length === 0) return null;

  const allSelected = selectedItems.length === totalItems;

  return (
    <div className="fixed bottom-0 left-0 right-0 z-40 bg-white border-t border-gray-200 shadow-lg animate-slide-up">
      <div className="flex items-center justify-between px-4 py-2 border-b border-gray-100">
        <span className="text-sm font-medium text-text">{selectedItems.length} selected</span>
        <button
          onClick={allSelected ? onDeselectAll : onSelectAll}
          className="flex items-center gap-1 text-sm text-primary min-h-[36px]"
        >
          {allSelected ? (
            <><XSquare className="w-4 h-4" /> Deselect All</>
          ) : (
            <><CheckSquare className="w-4 h-4" /> Select All</>
          )}
        </button>
      </div>
      <div className="flex items-center justify-around py-2">
        <button onClick={onShare} className="flex flex-col items-center gap-1 min-h-[44px] min-w-[44px] p-1">
          <Share2 className="w-5 h-5 text-text" />
          <span className="text-xs text-gray-500">Share</span>
        </button>
        <button onClick={onMove} className="flex flex-col items-center gap-1 min-h-[44px] min-w-[44px] p-1">
          <FolderInput className="w-5 h-5 text-text" />
          <span className="text-xs text-gray-500">Move</span>
        </button>
        <button onClick={onCopy} className="flex flex-col items-center gap-1 min-h-[44px] min-w-[44px] p-1">
          <Copy className="w-5 h-5 text-text" />
          <span className="text-xs text-gray-500">Copy</span>
        </button>
        <button onClick={onDelete} className="flex flex-col items-center gap-1 min-h-[44px] min-w-[44px] p-1">
          <Trash2 className="w-5 h-5 text-red-500" />
          <span className="text-xs text-red-500">Delete</span>
        </button>
      </div>
    </div>
  );
}
