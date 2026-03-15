import React, { useState, useEffect, useCallback, useRef } from 'react';
import { History, RotateCcw, Download, Eye, ChevronLeft } from 'lucide-react';
import { listFileHistory, getFileRevision, revertFile } from '../../lib/api';
import { bytesToSize, formatDate } from '../../lib/models';
import type { FileHistoryRecord } from '../../lib/models';
import SwipeableListItem from '../ui/SwipeableListItem';
import EmptyState from '../ui/EmptyState';
import BottomSheet from '../ui/BottomSheet';

interface FileHistoryProps {
  repoId?: string;
  path?: string;
  fileName?: string;
}

const PER_PAGE = 25;

export default function FileHistory({ repoId, path, fileName }: FileHistoryProps) {
  const [records, setRecords] = useState<FileHistoryRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState('');
  const [toast, setToast] = useState('');
  const [page, setPage] = useState(1);
  const [hasMore, setHasMore] = useState(false);
  const [totalCount, setTotalCount] = useState(0);

  // Context menu
  const [contextRecord, setContextRecord] = useState<FileHistoryRecord | null>(null);
  const [contextMenuOpen, setContextMenuOpen] = useState(false);

  // Restore confirmation
  const [restoreConfirmOpen, setRestoreConfirmOpen] = useState(false);
  const [restoreTarget, setRestoreTarget] = useState<FileHistoryRecord | null>(null);
  const [restoring, setRestoring] = useState(false);

  const sentinelRef = useRef<HTMLDivElement>(null);

  const showToast = (msg: string) => {
    setToast(msg);
    setTimeout(() => setToast(''), 3000);
  };

  const loadHistory = useCallback(async (pageNum: number, append: boolean = false) => {
    if (!repoId || !path) return;
    if (pageNum === 1) setLoading(true);
    else setLoadingMore(true);
    setError('');

    try {
      const result = await listFileHistory(repoId, path, pageNum, PER_PAGE);
      const items = result.data || [];
      const total = result.total_count || items.length;
      setTotalCount(total);
      setHasMore(total > PER_PAGE * pageNum);
      setRecords(prev => append ? [...prev, ...items] : items);
      setPage(pageNum);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load history');
    } finally {
      setLoading(false);
      setLoadingMore(false);
    }
  }, [repoId, path]);

  useEffect(() => {
    loadHistory(1);
  }, [loadHistory]);

  // Infinite scroll via IntersectionObserver
  useEffect(() => {
    if (!sentinelRef.current || !hasMore || loadingMore) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && hasMore && !loadingMore) {
          loadHistory(page + 1, true);
        }
      },
      { threshold: 0.1 },
    );
    observer.observe(sentinelRef.current);
    return () => observer.disconnect();
  }, [hasMore, loadingMore, page, loadHistory]);

  const handleRefresh = () => {
    loadHistory(1);
  };

  const handleView = async (record: FileHistoryRecord) => {
    if (!repoId || !path) return;
    try {
      const url = await getFileRevision(repoId, record.commit_id, path);
      window.open(url, '_blank');
    } catch {
      showToast('Failed to load revision');
    }
  };

  const handleDownload = async (record: FileHistoryRecord) => {
    if (!repoId || !path) return;
    try {
      const url = await getFileRevision(repoId, record.commit_id, path);
      const a = document.createElement('a');
      a.href = url;
      a.download = fileName || path.split('/').pop() || 'file';
      a.click();
    } catch {
      showToast('Failed to download revision');
    }
  };

  const handleRestoreConfirm = (record: FileHistoryRecord) => {
    setRestoreTarget(record);
    setRestoreConfirmOpen(true);
  };

  const handleRestore = async () => {
    if (!repoId || !path || !restoreTarget) return;
    setRestoring(true);
    try {
      await revertFile(repoId, restoreTarget.commit_id, path);
      showToast('File restored successfully');
      setRestoreConfirmOpen(false);
      setRestoreTarget(null);
      loadHistory(1);
    } catch {
      showToast('Failed to restore file');
    } finally {
      setRestoring(false);
    }
  };

  const handleLongPress = (record: FileHistoryRecord) => {
    setContextRecord(record);
    setContextMenuOpen(true);
  };

  const handleContextAction = (action: () => void) => {
    setContextMenuOpen(false);
    action();
  };

  if (!repoId || !path) {
    return (
      <EmptyState
        icon={<History className="w-12 h-12" />}
        title="No file selected"
        description="Select a file to view its history"
      />
    );
  }

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="flex items-center gap-2 px-4 py-3 border-b border-gray-100">
        <button
          onClick={() => window.history.back()}
          className="min-h-[44px] min-w-[44px] flex items-center justify-center"
          aria-label="Go back"
        >
          <ChevronLeft className="w-5 h-5 text-gray-600" />
        </button>
        <div className="flex-1 min-w-0">
          <h1 className="text-base font-semibold text-text truncate">{fileName || path.split('/').pop()}</h1>
          <p className="text-xs text-gray-500">Version History{totalCount > 0 ? ` (${totalCount})` : ''}</p>
        </div>
        <button
          onClick={handleRefresh}
          className="min-h-[44px] min-w-[44px] flex items-center justify-center"
          aria-label="Refresh"
          data-testid="refresh-button"
        >
          <RotateCcw className="w-5 h-5 text-gray-500" />
        </button>
      </div>

      {/* Loading */}
      {loading && <p className="text-center text-gray-500 py-8">Loading history...</p>}

      {/* Error */}
      {error && <p className="text-center text-red-500 py-4" data-testid="error-message">{error}</p>}

      {/* Empty state */}
      {!loading && !error && records.length === 0 && (
        <EmptyState
          icon={<History className="w-12 h-12" />}
          title="No history available"
          description="This file has no previous versions"
        />
      )}

      {/* History list */}
      {!loading && records.length > 0 && (
        <div className="flex-1 overflow-auto" data-testid="history-list">
          {records.map((record, index) => {
            const isCurrent = index === 0;
            const size = record.rev_file_size ?? record.size ?? 0;
            const creator = record.creator_name || record.creator_email || 'Unknown';

            return (
              <SwipeableListItem
                key={`${record.commit_id}-${index}`}
                rightActions={
                  !isCurrent
                    ? [
                        {
                          icon: <RotateCcw className="w-4 h-4" />,
                          label: 'Restore',
                          color: '#3b82f6',
                          onClick: () => handleRestoreConfirm(record),
                        },
                      ]
                    : []
                }
              >
                <HistoryRecordItem
                  record={record}
                  isCurrent={isCurrent}
                  creator={creator}
                  size={size}
                  onTap={() => handleView(record)}
                  onLongPress={() => handleLongPress(record)}
                />
              </SwipeableListItem>
            );
          })}

          {/* Sentinel for infinite scroll */}
          {hasMore && (
            <div ref={sentinelRef} className="py-4 text-center" data-testid="load-more-sentinel">
              {loadingMore && <p className="text-gray-500 text-sm">Loading more...</p>}
            </div>
          )}

          {!hasMore && records.length > 0 && (
            <p className="text-center text-gray-400 text-sm py-4">No more versions</p>
          )}
        </div>
      )}

      {/* Toast */}
      {toast && (
        <div className="fixed bottom-20 left-1/2 -translate-x-1/2 bg-gray-800 text-white px-4 py-2 rounded-lg text-sm z-50" data-testid="toast">
          {toast}
        </div>
      )}

      {/* Context menu (long press) */}
      <BottomSheet
        isOpen={contextMenuOpen}
        onClose={() => setContextMenuOpen(false)}
        title={`Version from ${contextRecord ? new Date(contextRecord.ctime * 1000).toLocaleString() : ''}`}
      >
        <div className="flex flex-col -mx-6 -mb-6">
          <button
            onClick={() => handleContextAction(() => contextRecord && handleView(contextRecord))}
            className="flex items-center gap-3 w-full px-4 py-3 min-h-[44px] text-left hover:bg-gray-50 text-text"
          >
            <Eye className="w-5 h-5" />
            <span className="text-base">View</span>
          </button>
          <button
            onClick={() => handleContextAction(() => contextRecord && handleDownload(contextRecord))}
            className="flex items-center gap-3 w-full px-4 py-3 min-h-[44px] text-left hover:bg-gray-50 text-text"
          >
            <Download className="w-5 h-5" />
            <span className="text-base">Download</span>
          </button>
          {contextRecord && records.indexOf(contextRecord) !== 0 && (
            <button
              onClick={() => handleContextAction(() => contextRecord && handleRestoreConfirm(contextRecord))}
              className="flex items-center gap-3 w-full px-4 py-3 min-h-[44px] text-left hover:bg-gray-50 text-text"
            >
              <RotateCcw className="w-5 h-5" />
              <span className="text-base">Restore</span>
            </button>
          )}
        </div>
      </BottomSheet>

      {/* Restore confirmation dialog */}
      <BottomSheet
        isOpen={restoreConfirmOpen}
        onClose={() => { setRestoreConfirmOpen(false); setRestoreTarget(null); }}
        title="Restore Version"
      >
        <p className="text-sm text-gray-600 mb-4">
          This will replace the current file with this version
          {restoreTarget ? ` from ${new Date(restoreTarget.ctime * 1000).toLocaleString()}` : ''}.
        </p>
        <div className="flex gap-3">
          <button
            onClick={() => { setRestoreConfirmOpen(false); setRestoreTarget(null); }}
            className="flex-1 py-3 min-h-[44px] rounded-lg border border-gray-300 text-text font-medium"
            disabled={restoring}
          >
            Cancel
          </button>
          <button
            onClick={handleRestore}
            className="flex-1 py-3 min-h-[44px] rounded-lg bg-primary text-white font-medium disabled:opacity-50"
            disabled={restoring}
            data-testid="confirm-restore"
          >
            {restoring ? 'Restoring...' : 'Restore'}
          </button>
        </div>
      </BottomSheet>
    </div>
  );
}

// Sub-component for a history record row
interface HistoryRecordItemProps {
  record: FileHistoryRecord;
  isCurrent: boolean;
  creator: string;
  size: number;
  onTap: () => void;
  onLongPress: () => void;
}

function HistoryRecordItem({ record, isCurrent, creator, size, onTap, onLongPress }: HistoryRecordItemProps) {
  const longPressTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const didLongPress = useRef(false);

  const handlePointerDown = () => {
    didLongPress.current = false;
    longPressTimer.current = setTimeout(() => {
      didLongPress.current = true;
      onLongPress();
    }, 500);
  };

  const handlePointerUp = () => {
    if (longPressTimer.current) {
      clearTimeout(longPressTimer.current);
      longPressTimer.current = null;
    }
  };

  const handleClick = () => {
    if (!didLongPress.current) {
      onTap();
    }
  };

  return (
    <div
      className="flex items-start gap-3 px-4 py-3 min-h-[44px] hover:bg-gray-50 cursor-pointer border-b border-gray-50"
      onClick={handleClick}
      onPointerDown={handlePointerDown}
      onPointerUp={handlePointerUp}
      onPointerLeave={handlePointerUp}
      data-testid="history-record"
    >
      <div className="flex-shrink-0 mt-1">
        <History className="w-5 h-5 text-primary" />
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <p className="text-sm font-medium text-text">
            {new Date(record.ctime * 1000).toLocaleString()}
          </p>
          {isCurrent && (
            <span className="text-xs bg-primary/10 text-primary px-2 py-0.5 rounded-full">
              Current
            </span>
          )}
        </div>
        <p className="text-xs text-gray-500 mt-0.5">{creator}</p>
        <div className="flex items-center gap-3 mt-1">
          <span className="text-xs text-gray-400">{bytesToSize(size)}</span>
          {record.description && (
            <span className="text-xs text-gray-400 truncate">{record.description}</span>
          )}
        </div>
      </div>
    </div>
  );
}
