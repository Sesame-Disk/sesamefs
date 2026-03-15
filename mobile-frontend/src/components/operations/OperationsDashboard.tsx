import React, { useState, useEffect } from 'react';
import {
  X, Upload, Share2, CheckCircle, AlertCircle, Loader, Pause, Play,
  RefreshCw, Trash2,
} from 'lucide-react';
import { uploadManager, formatSpeed, formatETA, formatSize, type UploadFile } from '../../lib/upload';
import { shareQueueManager, type ShareResult } from '../../lib/shareQueue';
import type { QueueStats } from '../../lib/operationQueue';

interface OperationsDashboardProps {
  isOpen: boolean;
  onClose: () => void;
}

type Tab = 'uploads' | 'shares';

export default function OperationsDashboard({ isOpen, onClose }: OperationsDashboardProps) {
  const [tab, setTab] = useState<Tab>('uploads');
  const [uploadQueue, setUploadQueue] = useState<UploadFile[]>([]);
  const [shareStats, setShareStats] = useState<QueueStats>({ queued: 0, processing: 0, completed: 0, failed: 0, total: 0 });
  const [shareResults, setShareResults] = useState<ShareResult[]>([]);

  useEffect(() => {
    if (!isOpen) return;

    setUploadQueue(uploadManager.getQueue());
    const unsubUpload = uploadManager.subscribe(() => {
      setUploadQueue(uploadManager.getQueue());
    });

    const updateShareStats = async () => {
      setShareStats(await shareQueueManager.getStats());
      setShareResults(shareQueueManager.getResults());
    };
    updateShareStats();
    const unsubShare = shareQueueManager.subscribe(() => updateShareStats());

    return () => {
      unsubUpload();
      unsubShare();
    };
  }, [isOpen]);

  if (!isOpen) return null;

  const uploadStats = uploadManager.getStats();

  return (
    <div className="fixed inset-0 z-50 flex items-end">
      <div className="fixed inset-0 bg-black/30" onClick={onClose} />
      <div className="relative w-full bg-white dark:bg-dark-surface rounded-t-2xl shadow-xl max-h-[80vh] flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-gray-100 dark:border-dark-border">
          <h3 className="text-lg font-medium text-text dark:text-dark-text">Operations</h3>
          <button onClick={onClose} className="min-h-[44px] min-w-[44px] flex items-center justify-center">
            <X className="w-5 h-5 text-gray-400" />
          </button>
        </div>

        {/* Tabs */}
        <div className="flex border-b border-gray-100 dark:border-dark-border">
          <button
            onClick={() => setTab('uploads')}
            className={`flex-1 flex items-center justify-center gap-2 py-3 text-sm font-medium border-b-2 ${
              tab === 'uploads'
                ? 'border-primary text-primary'
                : 'border-transparent text-gray-500'
            }`}
          >
            <Upload className="w-4 h-4" />
            Uploads
            {(uploadStats.uploading + uploadStats.queued + uploadStats.paused) > 0 && (
              <span className="bg-primary/10 text-primary text-xs px-1.5 py-0.5 rounded-full">
                {uploadStats.uploading + uploadStats.queued + uploadStats.paused}
              </span>
            )}
          </button>
          <button
            onClick={() => setTab('shares')}
            className={`flex-1 flex items-center justify-center gap-2 py-3 text-sm font-medium border-b-2 ${
              tab === 'shares'
                ? 'border-primary text-primary'
                : 'border-transparent text-gray-500'
            }`}
          >
            <Share2 className="w-4 h-4" />
            Shares
            {(shareStats.queued + shareStats.processing) > 0 && (
              <span className="bg-primary/10 text-primary text-xs px-1.5 py-0.5 rounded-full">
                {shareStats.queued + shareStats.processing}
              </span>
            )}
          </button>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-auto">
          {tab === 'uploads' && (
            <UploadTab queue={uploadQueue} stats={uploadStats} />
          )}
          {tab === 'shares' && (
            <ShareTab stats={shareStats} results={shareResults} />
          )}
        </div>
      </div>
    </div>
  );
}

// ─── Upload Tab ─────────────────────────────────────────────────────

function UploadTab({ queue, stats }: {
  queue: UploadFile[];
  stats: ReturnType<typeof uploadManager.getStats>;
}) {
  const hasActive = stats.uploading > 0 || stats.queued > 0;
  const hasPaused = stats.paused > 0;

  return (
    <div>
      {/* Action bar */}
      {(hasActive || hasPaused || stats.failed > 0) && (
        <div className="flex items-center gap-2 px-4 py-2 bg-gray-50 dark:bg-dark-bg">
          {hasActive && (
            <button onClick={() => uploadManager.pauseAll()} className="text-xs text-amber-600 px-2 py-1 rounded border border-amber-200">
              Pause All
            </button>
          )}
          {hasPaused && !hasActive && (
            <button onClick={() => uploadManager.resumeAll()} className="text-xs text-primary px-2 py-1 rounded border border-primary/20">
              Resume All
            </button>
          )}
          {stats.failed > 0 && (
            <button onClick={() => uploadManager.retryAllFailed()} className="text-xs text-primary px-2 py-1 rounded border border-primary/20">
              Retry Failed
            </button>
          )}
          {(hasActive || hasPaused) && (
            <button onClick={() => uploadManager.cancelAll()} className="text-xs text-red-500 px-2 py-1 rounded border border-red-200">
              Cancel All
            </button>
          )}
          {!hasActive && !hasPaused && queue.length > 0 && (
            <button onClick={() => uploadManager.clearCompleted()} className="text-xs text-gray-500 px-2 py-1 rounded border border-gray-200">
              Clear
            </button>
          )}
          {stats.totalSpeed > 0 && (
            <span className="text-xs text-gray-400 ml-auto">{formatSpeed(stats.totalSpeed)}</span>
          )}
        </div>
      )}

      {queue.length === 0 && (
        <p className="text-center text-gray-400 py-8 text-sm">No uploads</p>
      )}

      {queue.map(file => (
        <div key={file.id} className="flex items-center gap-3 px-4 py-2.5 border-b border-gray-50 dark:border-dark-border">
          <StatusIcon status={file.status} />
          <div className="flex-1 min-w-0">
            <p className="text-sm text-text dark:text-dark-text truncate">{file.file.name}</p>
            {(file.status === 'uploading' || file.status === 'paused') && (
              <>
                <div className="mt-1 h-1 bg-gray-100 dark:bg-gray-700 rounded-full overflow-hidden">
                  <div
                    className={`h-full rounded-full transition-all ${file.status === 'paused' ? 'bg-amber-400' : 'bg-primary'}`}
                    style={{ width: `${file.progress}%` }}
                  />
                </div>
                <p className="text-[10px] text-gray-400 mt-0.5">
                  {formatSize(file.bytesUploaded)} / {formatSize(file.totalBytes)}
                  {file.speed > 0 && ` · ${formatSpeed(file.speed)}`}
                  {file.eta > 0 && ` · ${formatETA(file.eta)}`}
                </p>
              </>
            )}
            {file.status === 'failed' && file.error && (
              <p className="text-[10px] text-red-500">{file.error}</p>
            )}
          </div>
          <UploadActions file={file} />
        </div>
      ))}
    </div>
  );
}

function UploadActions({ file }: { file: UploadFile }) {
  if (file.status === 'uploading') {
    return (
      <div className="flex">
        <button onClick={() => uploadManager.pauseFile(file.id)} className="p-2">
          <Pause className="w-4 h-4 text-gray-400" />
        </button>
        <button onClick={() => uploadManager.cancelFile(file.id)} className="p-2">
          <X className="w-4 h-4 text-gray-400" />
        </button>
      </div>
    );
  }
  if (file.status === 'paused') {
    return (
      <div className="flex">
        <button onClick={() => uploadManager.resumeFile(file.id)} className="p-2">
          <Play className="w-4 h-4 text-primary" />
        </button>
        <button onClick={() => uploadManager.cancelFile(file.id)} className="p-2">
          <X className="w-4 h-4 text-gray-400" />
        </button>
      </div>
    );
  }
  if (file.status === 'failed') {
    return (
      <button onClick={() => uploadManager.retryFile(file.id)} className="p-2">
        <RefreshCw className="w-4 h-4 text-primary" />
      </button>
    );
  }
  if (file.status === 'queued') {
    return (
      <button onClick={() => uploadManager.cancelFile(file.id)} className="p-2">
        <X className="w-4 h-4 text-gray-400" />
      </button>
    );
  }
  return null;
}

// ─── Share Tab ──────────────────────────────────────────────────────

function ShareTab({ stats, results }: { stats: QueueStats; results: ShareResult[] }) {
  const isDone = stats.total > 0 && stats.queued === 0 && stats.processing === 0;

  return (
    <div>
      {stats.total > 0 && (
        <div className="px-4 py-3 bg-gray-50 dark:bg-dark-bg">
          <div className="flex justify-between text-sm mb-1">
            <span className="text-gray-500">Progress</span>
            <span className="text-text dark:text-dark-text">{stats.completed + stats.failed} / {stats.total}</span>
          </div>
          <div className="h-1.5 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden">
            <div
              className="h-full bg-primary rounded-full transition-all"
              style={{ width: `${((stats.completed + stats.failed) / stats.total) * 100}%` }}
            />
          </div>
          {stats.failed > 0 && isDone && (
            <button
              onClick={() => shareQueueManager.retryAllFailed()}
              className="text-xs text-primary mt-2"
            >
              Retry {stats.failed} failed
            </button>
          )}
        </div>
      )}

      {stats.total === 0 && (
        <p className="text-center text-gray-400 py-8 text-sm">No share operations</p>
      )}

      {results.map(r => (
        <div key={r.taskId} className="flex items-center gap-3 px-4 py-2.5 border-b border-gray-50 dark:border-dark-border">
          {r.success ? (
            <CheckCircle className="w-4 h-4 text-green-500 shrink-0" />
          ) : (
            <AlertCircle className="w-4 h-4 text-red-500 shrink-0" />
          )}
          <div className="flex-1 min-w-0">
            <p className="text-sm text-text dark:text-dark-text truncate">{r.fileName}</p>
            {r.error && <p className="text-[10px] text-red-500">{r.error}</p>}
            {r.shareLink && <p className="text-[10px] text-primary truncate">{r.shareLink}</p>}
          </div>
        </div>
      ))}

      {/* Pending items */}
      {shareQueueManager.getItems()
        .filter(item => item.status === 'queued' || item.status === 'processing')
        .map(item => (
          <div key={item.id} className="flex items-center gap-3 px-4 py-2.5 border-b border-gray-50 dark:border-dark-border">
            <Loader className="w-4 h-4 text-gray-300 animate-spin shrink-0" />
            <p className="text-sm text-gray-400 truncate">{(item as any).fileName}</p>
          </div>
        ))}

      {isDone && stats.total > 0 && (
        <div className="p-4">
          <button
            onClick={() => shareQueueManager.clear()}
            className="w-full py-2 text-sm text-gray-500 border border-gray-200 dark:border-dark-border rounded-lg"
          >
            Clear Results
          </button>
        </div>
      )}
    </div>
  );
}

// ─── Shared ─────────────────────────────────────────────────────────

function StatusIcon({ status }: { status: string }) {
  switch (status) {
    case 'uploading': return <Loader className="w-4 h-4 text-primary animate-spin shrink-0" />;
    case 'completed': return <CheckCircle className="w-4 h-4 text-green-500 shrink-0" />;
    case 'failed': return <AlertCircle className="w-4 h-4 text-red-500 shrink-0" />;
    case 'cancelled': return <Trash2 className="w-4 h-4 text-gray-400 shrink-0" />;
    case 'paused': return <Pause className="w-4 h-4 text-amber-500 shrink-0" />;
    default: return <Loader className="w-4 h-4 text-gray-300 shrink-0" />;
  }
}
