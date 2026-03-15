import React, { useState, useEffect } from 'react';
import { X, CheckCircle, AlertCircle, XCircle, Loader, Pause, Play, RefreshCw } from 'lucide-react';
import { uploadManager, formatSpeed, formatETA, formatSize, type UploadFile } from '../../lib/upload';

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

  const stats = uploadManager.getStats();
  const hasActive = stats.uploading > 0 || stats.queued > 0;
  const hasPaused = stats.paused > 0;
  const hasFinished = !hasActive && !hasPaused && queue.length > 0;

  const statusIcon = (file: UploadFile) => {
    switch (file.status) {
      case 'uploading': return <Loader className="w-4 h-4 text-primary animate-spin" />;
      case 'completed': return <CheckCircle className="w-4 h-4 text-green-500" />;
      case 'failed': return <AlertCircle className="w-4 h-4 text-red-500" />;
      case 'cancelled': return <XCircle className="w-4 h-4 text-gray-400" />;
      case 'paused': return <Pause className="w-4 h-4 text-amber-500" />;
      default: return <Loader className="w-4 h-4 text-gray-300" />;
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-end" data-testid="upload-progress-sheet">
      <div className="fixed inset-0 bg-black/30" onClick={onClose} />
      <div className="relative w-full bg-white dark:bg-dark-surface rounded-t-2xl shadow-xl max-h-[70vh] flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-gray-100 dark:border-dark-border">
          <div>
            <h3 className="text-lg font-medium text-text dark:text-dark-text">Uploads</h3>
            <p className="text-xs text-gray-500">
              {stats.uploading > 0 && `${stats.uploading} uploading`}
              {stats.queued > 0 && `${stats.uploading > 0 ? ', ' : ''}${stats.queued} queued`}
              {stats.paused > 0 && ` · ${stats.paused} paused`}
              {stats.completed > 0 && ` · ${stats.completed} done`}
              {stats.failed > 0 && ` · ${stats.failed} failed`}
              {stats.totalSpeed > 0 && ` · ${formatSpeed(stats.totalSpeed)}`}
            </p>
          </div>
          <div className="flex items-center gap-1">
            {hasActive && (
              <button
                onClick={() => uploadManager.pauseAll()}
                className="text-sm text-amber-600 px-2 py-1 min-h-[36px]"
                data-testid="pause-all-btn"
              >
                Pause All
              </button>
            )}
            {hasPaused && !hasActive && (
              <button
                onClick={() => uploadManager.resumeAll()}
                className="text-sm text-primary px-2 py-1 min-h-[36px]"
                data-testid="resume-all-btn"
              >
                Resume All
              </button>
            )}
            {(hasActive || hasPaused) && (
              <button
                onClick={() => uploadManager.cancelAll()}
                className="text-sm text-red-500 px-2 py-1 min-h-[36px]"
                data-testid="cancel-all-btn"
              >
                Cancel All
              </button>
            )}
            {hasFinished && (
              <button
                onClick={() => uploadManager.clearCompleted()}
                className="text-sm text-primary px-2 py-1 min-h-[36px]"
                data-testid="clear-btn"
              >
                Clear
              </button>
            )}
            {stats.failed > 0 && (
              <button
                onClick={() => uploadManager.retryAllFailed()}
                className="text-sm text-primary px-2 py-1 min-h-[36px]"
                data-testid="retry-all-btn"
              >
                Retry All
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
            <div key={file.id} className="flex items-center gap-3 px-4 py-3 border-b border-gray-50 dark:border-dark-border">
              {statusIcon(file)}
              <div className="flex-1 min-w-0">
                <p className="text-sm text-text dark:text-dark-text truncate">{file.file.name}</p>
                {(file.status === 'uploading' || file.status === 'paused') && (
                  <>
                    <div className="mt-1 h-1.5 bg-gray-100 dark:bg-gray-700 rounded-full overflow-hidden">
                      <div
                        className={`h-full rounded-full transition-all duration-300 ${
                          file.status === 'paused' ? 'bg-amber-400' : 'bg-primary'
                        }`}
                        style={{ width: `${file.progress}%` }}
                      />
                    </div>
                    <div className="flex items-center gap-2 mt-0.5">
                      <span className="text-[10px] text-gray-400">
                        {formatSize(file.bytesUploaded)} / {formatSize(file.totalBytes)}
                      </span>
                      {file.chunksTotal > 1 && (
                        <span className="text-[10px] text-gray-400">
                          · chunk {file.chunksCompleted}/{file.chunksTotal}
                        </span>
                      )}
                      {file.status === 'uploading' && file.speed > 0 && (
                        <span className="text-[10px] text-gray-400">
                          · {formatSpeed(file.speed)}
                        </span>
                      )}
                      {file.status === 'uploading' && file.eta > 0 && (
                        <span className="text-[10px] text-gray-400">
                          · {formatETA(file.eta)} left
                        </span>
                      )}
                    </div>
                  </>
                )}
                {file.status === 'failed' && file.error && (
                  <p className="text-xs text-red-500 mt-0.5">{file.error}</p>
                )}
              </div>
              {/* Action buttons */}
              <div className="flex items-center">
                {file.status === 'uploading' && (
                  <button
                    onClick={() => uploadManager.pauseFile(file.id)}
                    className="min-h-[44px] min-w-[44px] flex items-center justify-center"
                    aria-label={`Pause upload ${file.file.name}`}
                  >
                    <Pause className="w-4 h-4 text-gray-400" />
                  </button>
                )}
                {file.status === 'paused' && (
                  <button
                    onClick={() => uploadManager.resumeFile(file.id)}
                    className="min-h-[44px] min-w-[44px] flex items-center justify-center"
                    aria-label={`Resume upload ${file.file.name}`}
                  >
                    <Play className="w-4 h-4 text-primary" />
                  </button>
                )}
                {file.status === 'failed' && (
                  <button
                    onClick={() => uploadManager.retryFile(file.id)}
                    className="min-h-[44px] min-w-[44px] flex items-center justify-center"
                    aria-label={`Retry upload ${file.file.name}`}
                  >
                    <RefreshCw className="w-4 h-4 text-primary" />
                  </button>
                )}
                {(file.status === 'uploading' || file.status === 'queued' || file.status === 'paused') && (
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
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
