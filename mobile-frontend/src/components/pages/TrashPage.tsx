import React, { useState, useEffect, useCallback } from 'react';
import { Trash2, File, Folder, RotateCcw, AlertTriangle, RefreshCw } from 'lucide-react';
import { listTrash, restoreTrashItem, cleanTrash } from '../../lib/api';
import { bytesToSize } from '../../lib/models';
import type { TrashItem } from '../../lib/models';
import SwipeableListItem from '../ui/SwipeableListItem';

interface TrashPageProps {
  repoId?: string;
  repoName?: string;
}

export default function TrashPage({ repoId, repoName }: TrashPageProps) {
  const [items, setItems] = useState<TrashItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [toast, setToast] = useState('');
  const [confirmClean, setConfirmClean] = useState(false);
  const [scanStat, setScanStat] = useState<string | null>(null);
  const [hasMore, setHasMore] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);

  const showToast = (msg: string) => {
    setToast(msg);
    setTimeout(() => setToast(''), 3000);
  };

  const fetchTrash = useCallback(async (append = false, cursor?: string) => {
    if (!repoId) return;
    if (!append) setLoading(true);
    setError('');
    try {
      const result = await listTrash(repoId, '/', cursor);
      if (!result.data.length && result.more && result.scan_stat) {
        // Skip empty pages (same pattern as web frontend)
        await fetchTrash(append, result.scan_stat);
        return;
      }
      setItems(prev => append ? [...prev, ...result.data] : result.data);
      setScanStat(result.scan_stat);
      setHasMore(result.more);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load trash');
    } finally {
      setLoading(false);
      setLoadingMore(false);
    }
  }, [repoId]);

  useEffect(() => {
    fetchTrash();
  }, [fetchTrash]);

  const handleRefresh = () => {
    setItems([]);
    setScanStat(null);
    fetchTrash();
  };

  const handleLoadMore = () => {
    if (!scanStat) return;
    setLoadingMore(true);
    fetchTrash(true, scanStat);
  };

  const handleRestore = async (item: TrashItem) => {
    if (!repoId) return;
    const path = item.parent_dir + item.obj_name;
    try {
      await restoreTrashItem(repoId, item.commit_id, path, item.is_dir);
      setItems(prev => prev.filter(i => i !== item));
      showToast('Item restored');
    } catch {
      showToast('Failed to restore item');
    }
  };

  const handleCleanTrash = async () => {
    if (!repoId) return;
    setConfirmClean(false);
    try {
      await cleanTrash(repoId);
      setItems([]);
      setHasMore(false);
      setScanStat(null);
      showToast('Trash cleaned');
    } catch {
      showToast('Failed to clean trash');
    }
  };

  if (!repoId) {
    return (
      <div className="flex flex-col items-center justify-center p-8 text-center">
        <Trash2 className="w-12 h-12 text-gray-300 mb-4" />
        <p className="text-gray-500">No library selected</p>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-gray-100">
        <h1 className="text-lg font-semibold text-text truncate">
          {repoName ? `${repoName} - Trash` : 'Trash'}
        </h1>
        <div className="flex items-center gap-2">
          <button
            onClick={handleRefresh}
            className="min-h-[44px] min-w-[44px] flex items-center justify-center"
            aria-label="Refresh"
          >
            <RefreshCw className="w-5 h-5 text-gray-500" />
          </button>
          {items.length > 0 && (
            <button
              onClick={() => setConfirmClean(true)}
              className="text-red-500 text-sm font-medium min-h-[44px] px-2"
              data-testid="clean-trash-btn"
            >
              Clean Trash
            </button>
          )}
        </div>
      </div>

      {/* Loading skeleton */}
      {loading && (
        <div className="px-4 py-3 space-y-4" data-testid="trash-skeleton">
          {[1, 2, 3].map(i => (
            <div key={i} className="flex items-center gap-3 animate-pulse">
              <div className="w-5 h-5 bg-gray-200 rounded" />
              <div className="flex-1 space-y-2">
                <div className="h-4 bg-gray-200 rounded w-3/4" />
                <div className="h-3 bg-gray-100 rounded w-1/2" />
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Error state */}
      {error && !loading && (
        <div className="flex flex-col items-center justify-center p-8 text-center" role="alert">
          <AlertTriangle className="w-12 h-12 text-red-300 mb-4" />
          <p className="text-red-500 mb-4">{error}</p>
          <button
            onClick={handleRefresh}
            className="bg-primary text-white px-4 py-2 rounded-lg text-sm"
          >
            Retry
          </button>
        </div>
      )}

      {/* Empty state */}
      {!loading && !error && items.length === 0 && (
        <div className="flex flex-col items-center justify-center p-8 text-center">
          <Trash2 className="w-12 h-12 text-gray-300 mb-4" />
          <p className="text-gray-500">No deleted items</p>
        </div>
      )}

      {/* Item list */}
      {!loading && !error && items.length > 0 && (
        <div className="flex-1 overflow-auto">
          {items.map((item, index) => {
            const Icon = item.is_dir ? Folder : File;
            const deletedDate = new Date(item.deleted_time).toLocaleDateString();
            return (
              <SwipeableListItem
                key={`${item.commit_id}-${item.obj_name}-${index}`}
                rightActions={[
                  {
                    icon: <RotateCcw className="w-4 h-4" />,
                    label: 'Restore',
                    color: '#22c55e',
                    onClick: () => handleRestore(item),
                  },
                ]}
              >
                <div
                  className="flex items-center gap-3 px-4 py-3 min-h-[44px]"
                  data-testid="trash-item"
                >
                  <Icon className="w-5 h-5 text-primary flex-shrink-0" />
                  <div className="flex-1 min-w-0">
                    <p className="text-text text-base truncate">{item.obj_name}</p>
                    <p className="text-gray-400 text-xs truncate">
                      {item.parent_dir} &middot; {deletedDate}
                      {!item.is_dir && item.size > 0 ? ` \u00b7 ${bytesToSize(item.size)}` : ''}
                    </p>
                  </div>
                </div>
              </SwipeableListItem>
            );
          })}
          {hasMore && (
            <button
              onClick={handleLoadMore}
              disabled={loadingMore}
              className="w-full py-3 text-primary text-sm font-medium border-t border-gray-100"
            >
              {loadingMore ? 'Loading...' : 'Load more'}
            </button>
          )}
        </div>
      )}

      {/* Toast */}
      {toast && (
        <div className="fixed bottom-20 left-1/2 -translate-x-1/2 bg-gray-800 text-white px-4 py-2 rounded-lg text-sm z-50">
          {toast}
        </div>
      )}

      {/* Clean trash confirmation */}
      {confirmClean && (
        <div className="fixed inset-0 z-50 flex items-end justify-center bg-black/50" data-testid="clean-confirm-dialog">
          <div className="bg-white dark:bg-dark-surface w-full max-w-lg rounded-t-2xl p-6 pb-8">
            <h2 className="text-lg font-semibold text-text mb-2">Clean Trash</h2>
            <p className="text-gray-500 text-sm mb-6">
              Are you sure you want to permanently delete all items in the trash? This action cannot be undone.
            </p>
            <div className="flex gap-3">
              <button
                onClick={() => setConfirmClean(false)}
                className="flex-1 py-3 rounded-lg border border-gray-200 text-text font-medium"
              >
                Cancel
              </button>
              <button
                onClick={handleCleanTrash}
                className="flex-1 py-3 rounded-lg bg-red-500 text-white font-medium"
                data-testid="confirm-clean-btn"
              >
                Delete All
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
