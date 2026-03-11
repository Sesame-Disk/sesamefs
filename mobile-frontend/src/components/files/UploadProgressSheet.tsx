import React, { useState, useEffect } from 'react';
import { X, CheckCircle, AlertCircle, XCircle, Loader } from 'lucide-react';
import { uploadManager, type UploadFile } from '../../lib/upload';

interface UploadProgressSheetProps {
  isOpen: boolean;
  onClose: () => void;
}

export default function UploadProgressSheet({ isOpen, onClose }: UploadProgressSheetProps) {
  const [queue, setQueue] = useState<UploadFile[]>([]);

  useEffect(() => {
    setQueue(uploadManager.getQueue());
    const unsubscribe = uploadManager.subscribe(() => {
      setQueue(uploadManager.getQueue());
    });
    return unsubscribe;
  }, []);

  if (!isOpen) return null;

  const uploading = queue.filter(f => f.status === 'uploading');
  const queued = queue.filter(f => f.status === 'queued');
  const completed = queue.filter(f => f.status === 'completed');
  const failed = queue.filter(f => f.status === 'failed');
  const hasActive = uploading.length > 0 || queued.length > 0;

  const statusIcon = (file: UploadFile) => {
    switch (file.status) {
      case 'uploading': return <Loader className="w-4 h-4 text-primary animate-spin" />;
      case 'completed': return <CheckCircle className="w-4 h-4 text-green-500" />;
      case 'failed': return <AlertCircle className="w-4 h-4 text-red-500" />;
      case 'cancelled': return <XCircle className="w-4 h-4 text-gray-400" />;
      default: return <Loader className="w-4 h-4 text-gray-300" />;
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-end" data-testid="upload-progress-sheet">
      <div className="fixed inset-0 bg-black/30" onClick={onClose} />
      <div className="relative w-full bg-white rounded-t-2xl shadow-xl max-h-[70vh] flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-gray-100">
          <div>
            <h3 className="text-lg font-medium text-text">Uploads</h3>
            <p className="text-xs text-gray-500">
              {uploading.length > 0 && `${uploading.length} uploading`}
              {queued.length > 0 && `${uploading.length > 0 ? ', ' : ''}${queued.length} queued`}
              {completed.length > 0 && ` · ${completed.length} done`}
              {failed.length > 0 && ` · ${failed.length} failed`}
            </p>
          </div>
          <div className="flex items-center gap-2">
            {hasActive && (
              <button
                onClick={() => uploadManager.cancelAll()}
                className="text-sm text-red-500 px-3 py-1"
                data-testid="cancel-all-btn"
              >
                Cancel All
              </button>
            )}
            {!hasActive && queue.length > 0 && (
              <button
                onClick={() => uploadManager.clearCompleted()}
                className="text-sm text-primary px-3 py-1"
                data-testid="clear-btn"
              >
                Clear
              </button>
            )}
            <button onClick={onClose} className="min-h-[44px] min-w-[44px] flex items-center justify-center">
              <X className="w-5 h-5 text-gray-400" />
            </button>
          </div>
        </div>

        {/* File list */}
        <div className="flex-1 overflow-auto">
          {queue.length === 0 && (
            <p className="text-center text-gray-400 py-8">No uploads</p>
          )}
          {queue.map(file => (
            <div key={file.id} className="flex items-center gap-3 px-4 py-3 border-b border-gray-50">
              {statusIcon(file)}
              <div className="flex-1 min-w-0">
                <p className="text-sm text-text truncate">{file.file.name}</p>
                {file.status === 'uploading' && (
                  <div className="mt-1 h-1.5 bg-gray-100 rounded-full overflow-hidden">
                    <div
                      className="h-full bg-primary rounded-full transition-all duration-300"
                      style={{ width: `${file.progress}%` }}
                    />
                  </div>
                )}
                {file.status === 'failed' && file.error && (
                  <p className="text-xs text-red-500 mt-0.5">{file.error}</p>
                )}
              </div>
              {(file.status === 'uploading' || file.status === 'queued') && (
                <button
                  onClick={() => uploadManager.cancelFile(file.id)}
                  className="min-h-[44px] min-w-[44px] flex items-center justify-center"
                  aria-label={`Cancel upload ${file.file.name}`}
                  data-testid={`cancel-${file.id}`}
                >
                  <X className="w-4 h-4 text-gray-400" />
                </button>
              )}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
