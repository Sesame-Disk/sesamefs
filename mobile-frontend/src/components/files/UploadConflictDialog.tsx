import React, { useState } from 'react';
import { AlertTriangle, X } from 'lucide-react';

export type ConflictResolution = 'replace' | 'rename' | 'skip';

interface UploadConflictDialogProps {
  isOpen: boolean;
  onClose: () => void;
  fileName: string;
  remainingCount: number;
  onResolve: (resolution: ConflictResolution, applyToAll: boolean) => void;
}

export default function UploadConflictDialog({
  isOpen,
  onClose,
  fileName,
  remainingCount,
  onResolve,
}: UploadConflictDialogProps) {
  const [applyToAll, setApplyToAll] = useState(false);

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4" data-testid="conflict-dialog">
      <div className="fixed inset-0 bg-black/30" onClick={onClose} />
      <div className="relative bg-white rounded-2xl shadow-xl w-full max-w-sm p-6">
        <button
          onClick={onClose}
          className="absolute top-3 right-3 min-h-[44px] min-w-[44px] flex items-center justify-center"
        >
          <X className="w-5 h-5 text-gray-400" />
        </button>

        <div className="flex items-center gap-3 mb-4">
          <AlertTriangle className="w-6 h-6 text-amber-500" />
          <h3 className="text-lg font-medium text-text">File Exists</h3>
        </div>

        <p className="text-sm text-gray-600 mb-6">
          A file named <strong>{fileName}</strong> already exists in this location.
        </p>

        {remainingCount > 0 && (
          <label className="flex items-center gap-2 mb-4 text-sm text-gray-600">
            <input
              type="checkbox"
              checked={applyToAll}
              onChange={(e) => setApplyToAll(e.target.checked)}
              className="w-4 h-4"
              data-testid="apply-to-all"
            />
            Apply to remaining {remainingCount} file{remainingCount > 1 ? 's' : ''}
          </label>
        )}

        <div className="flex flex-col gap-2">
          <button
            onClick={() => onResolve('replace', applyToAll)}
            className="w-full py-3 bg-primary text-white rounded-lg font-medium min-h-[44px]"
            data-testid="resolve-replace"
          >
            Replace
          </button>
          <button
            onClick={() => onResolve('rename', applyToAll)}
            className="w-full py-3 bg-gray-100 text-text rounded-lg font-medium min-h-[44px]"
            data-testid="resolve-rename"
          >
            Keep Both (Rename)
          </button>
          <button
            onClick={() => onResolve('skip', applyToAll)}
            className="w-full py-3 text-gray-500 rounded-lg font-medium min-h-[44px]"
            data-testid="resolve-skip"
          >
            Skip
          </button>
        </div>
      </div>
    </div>
  );
}
