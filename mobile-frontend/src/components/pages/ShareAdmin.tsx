import React, { useState, useEffect, useCallback } from 'react';
import { Link2, Upload, Trash2, Folder } from 'lucide-react';
import {
  listAllShareLinks,
  listAllUploadLinks,
  listSharedFolders,
  deleteShareLink,
  deleteUploadLink,
  unshareFolder,
} from '../../lib/api';
import type { ShareLink, UploadLink, SharedFolder } from '../../lib/api';
import { formatDate } from '../../lib/models';
import SwipeableListItem from '../ui/SwipeableListItem';
import SkeletonList from '../ui/SkeletonList';
import { ContentCrossfade } from '../ui/SkeletonList';
import EmptyState from '../ui/EmptyState';

type Tab = 'share' | 'upload' | 'folders';

export default function ShareAdmin() {
  const [activeTab, setActiveTab] = useState<Tab>('share');
  const [shareLinks, setShareLinks] = useState<ShareLink[]>([]);
  const [uploadLinks, setUploadLinks] = useState<UploadLink[]>([]);
  const [sharedFolders, setSharedFolders] = useState<SharedFolder[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [refreshing, setRefreshing] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState<{ type: Tab; token: string } | null>(null);
  const [unshareConfirm, setUnshareConfirm] = useState<SharedFolder | null>(null);

  const fetchData = useCallback(async () => {
    try {
      const [shares, uploads, folders] = await Promise.all([
        listAllShareLinks(),
        listAllUploadLinks(),
        listSharedFolders(),
      ]);
      setShareLinks(shares);
      setUploadLinks(uploads);
      setSharedFolders(folders);
      setError('');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load links');
    }
  }, []);

  useEffect(() => {
    fetchData().finally(() => setLoading(false));
  }, [fetchData]);

  const handleRefresh = async () => {
    setRefreshing(true);
    await fetchData();
    setRefreshing(false);
  };

  const handleDeleteShareLink = async (token: string) => {
    try {
      await deleteShareLink(token);
      setShareLinks((prev) => prev.filter((l) => l.token !== token));
    } catch {
      setError('Failed to delete share link');
    }
    setDeleteConfirm(null);
  };

  const handleDeleteUploadLink = async (token: string) => {
    try {
      await deleteUploadLink(token);
      setUploadLinks((prev) => prev.filter((l) => l.token !== token));
    } catch {
      setError('Failed to delete upload link');
    }
    setDeleteConfirm(null);
  };

  const handleUnshareFolder = async (folder: SharedFolder) => {
    try {
      const shareType = folder.share_type === 'personal' ? 'user' : 'group';
      const shareToId = shareType === 'user'
        ? (folder.user_email || '')
        : String(folder.group_id || '');
      await unshareFolder(folder.repo_id, folder.path, shareType, shareToId);
      setSharedFolders((prev) => prev.filter((f) => f !== folder));
    } catch {
      setError('Failed to unshare folder');
    }
    setUnshareConfirm(null);
  };

  const confirmDelete = () => {
    if (!deleteConfirm) return;
    if (deleteConfirm.type === 'share') {
      handleDeleteShareLink(deleteConfirm.token);
    } else {
      handleDeleteUploadLink(deleteConfirm.token);
    }
  };

  const getFileName = (path: string) => {
    const parts = path.replace(/\/$/, '').split('/');
    return parts[parts.length - 1] || path;
  };

  if (error && shareLinks.length === 0 && uploadLinks.length === 0 && sharedFolders.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center p-8 text-center">
        <p role="alert" className="text-red-500 mb-4">{error}</p>
        <button
          onClick={handleRefresh}
          className="text-primary font-medium min-h-[44px]"
        >
          Retry
        </button>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full">
      <div className="px-4 pt-2 pb-1 flex items-center justify-between">
        <h1 className="text-xl font-semibold text-text dark:text-dark-text">My Shares</h1>
        <button
          onClick={handleRefresh}
          disabled={refreshing}
          className="text-sm text-primary font-medium min-h-[44px]"
        >
          {refreshing ? 'Refreshing...' : 'Refresh'}
        </button>
      </div>

      {/* Tabs */}
      <div className="flex border-b border-gray-200 dark:border-dark-border px-4">
        <button
          onClick={() => setActiveTab('share')}
          className={`flex-1 py-3 text-sm font-medium text-center min-h-[44px] border-b-2 ${
            activeTab === 'share'
              ? 'border-primary text-primary'
              : 'border-transparent text-gray-500 dark:text-gray-400'
          }`}
        >
          Share Links
        </button>
        <button
          onClick={() => setActiveTab('upload')}
          className={`flex-1 py-3 text-sm font-medium text-center min-h-[44px] border-b-2 ${
            activeTab === 'upload'
              ? 'border-primary text-primary'
              : 'border-transparent text-gray-500 dark:text-gray-400'
          }`}
        >
          Upload Links
        </button>
        <button
          onClick={() => setActiveTab('folders')}
          className={`flex-1 py-3 text-sm font-medium text-center min-h-[44px] border-b-2 ${
            activeTab === 'folders'
              ? 'border-primary text-primary'
              : 'border-transparent text-gray-500 dark:text-gray-400'
          }`}
          data-testid="tab-shared-folders"
        >
          Folders
        </button>
      </div>

      <ContentCrossfade
        isLoading={loading}
        skeleton={<SkeletonList variant="file" count={5} />}
      >
        {activeTab === 'share' ? (
          shareLinks.length === 0 ? (
            <EmptyState
              icon={<Link2 className="w-12 h-12" />}
              title="No share links"
              description="Create share links from the file browser to share files with others."
            />
          ) : (
            <div className="flex flex-col pb-20">
              {shareLinks.map((link) => (
                <SwipeableListItem
                  key={link.token}
                  rightActions={[
                    {
                      icon: <Trash2 className="w-5 h-5" />,
                      label: 'Delete',
                      color: '#ef4444',
                      onClick: () => setDeleteConfirm({ type: 'share', token: link.token }),
                    },
                  ]}
                >
                  <div className="flex items-center gap-3 px-4 py-3 min-h-[56px] border-b border-gray-100 dark:border-dark-border">
                    <Link2 className="w-10 h-10 text-primary flex-shrink-0" />
                    <div className="flex-1 min-w-0">
                      <p className="text-sm font-medium text-text dark:text-dark-text truncate">
                        {getFileName(link.path)}
                      </p>
                      <p className="text-xs text-gray-500 dark:text-gray-400 truncate">
                        {link.path}
                      </p>
                      <p className="text-xs text-gray-400 dark:text-gray-500">
                        Created {formatDate(new Date(link.ctime).getTime() / 1000)} &middot; {link.view_cnt} views
                      </p>
                    </div>
                  </div>
                </SwipeableListItem>
              ))}
            </div>
          )
        ) : activeTab === 'upload' ? (
          uploadLinks.length === 0 ? (
            <EmptyState
              icon={<Upload className="w-12 h-12" />}
              title="No upload links"
              description="Create upload links to let others upload files to your libraries."
            />
          ) : (
            <div className="flex flex-col pb-20">
              {uploadLinks.map((link) => (
                <SwipeableListItem
                  key={link.token}
                  rightActions={[
                    {
                      icon: <Trash2 className="w-5 h-5" />,
                      label: 'Delete',
                      color: '#ef4444',
                      onClick: () => setDeleteConfirm({ type: 'upload', token: link.token }),
                    },
                  ]}
                >
                  <div className="flex items-center gap-3 px-4 py-3 min-h-[56px] border-b border-gray-100 dark:border-dark-border">
                    <Upload className="w-10 h-10 text-green-500 flex-shrink-0" />
                    <div className="flex-1 min-w-0">
                      <p className="text-sm font-medium text-text dark:text-dark-text truncate">
                        {getFileName(link.path)}
                      </p>
                      <p className="text-xs text-gray-500 dark:text-gray-400 truncate">
                        {link.path}
                      </p>
                      <p className="text-xs text-gray-400 dark:text-gray-500">
                        Created {formatDate(new Date(link.ctime).getTime() / 1000)} &middot; {link.view_cnt} views
                      </p>
                    </div>
                  </div>
                </SwipeableListItem>
              ))}
            </div>
          )
        ) : (
          sharedFolders.length === 0 ? (
            <EmptyState
              icon={<Folder className="w-12 h-12" />}
              title="No shared folders"
              description="Share folders from the file browser to collaborate with others."
            />
          ) : (
            <div className="flex flex-col pb-20" data-testid="shared-folders-list">
              {sharedFolders.map((folder, index) => {
                const key = `${folder.repo_id}-${folder.path}-${folder.share_type}-${folder.share_type === 'personal' ? folder.user_email : folder.group_id}`;
                const shareTo = folder.share_type === 'personal'
                  ? folder.user_name || folder.user_email || ''
                  : folder.group_name || '';
                return (
                  <SwipeableListItem
                    key={key}
                    rightActions={[
                      {
                        icon: <Trash2 className="w-5 h-5" />,
                        label: 'Unshare',
                        color: '#ef4444',
                        onClick: () => setUnshareConfirm(folder),
                      },
                    ]}
                  >
                    <div className="flex items-center gap-3 px-4 py-3 min-h-[56px] border-b border-gray-100 dark:border-dark-border">
                      <Folder className="w-10 h-10 text-primary flex-shrink-0" />
                      <div className="flex-1 min-w-0">
                        <p className="text-sm font-medium text-text dark:text-dark-text truncate">
                          {folder.folder_name}
                        </p>
                        <p className="text-xs text-gray-500 dark:text-gray-400 truncate">
                          {folder.repo_name} {folder.path}
                        </p>
                        <p className="text-xs text-gray-400 dark:text-gray-500">
                          Shared to {shareTo} &middot; {folder.share_permission === 'rw' ? 'Read-Write' : 'Read-Only'}
                        </p>
                      </div>
                    </div>
                  </SwipeableListItem>
                );
              })}
            </div>
          )
        )}
      </ContentCrossfade>

      {/* Delete confirmation dialog */}
      {deleteConfirm && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
          <div className="bg-white dark:bg-dark-surface rounded-lg p-6 max-w-sm w-full shadow-xl">
            <h3 className="text-lg font-semibold text-text dark:text-dark-text mb-2">
              Delete Link
            </h3>
            <p className="text-sm text-gray-500 dark:text-gray-400 mb-6">
              Are you sure you want to delete this {deleteConfirm.type === 'share' ? 'share' : 'upload'} link? This action cannot be undone.
            </p>
            <div className="flex gap-3 justify-end">
              <button
                onClick={() => setDeleteConfirm(null)}
                className="px-4 py-2 text-sm font-medium text-gray-600 dark:text-gray-400 min-h-[44px]"
              >
                Cancel
              </button>
              <button
                onClick={confirmDelete}
                className="px-4 py-2 text-sm font-medium text-white bg-red-500 rounded-lg min-h-[44px]"
              >
                Delete
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Unshare folder confirmation dialog */}
      {unshareConfirm && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
          <div className="bg-white dark:bg-dark-surface rounded-lg p-6 max-w-sm w-full shadow-xl">
            <h3 className="text-lg font-semibold text-text dark:text-dark-text mb-2">
              Unshare Folder
            </h3>
            <p className="text-sm text-gray-500 dark:text-gray-400 mb-6">
              Are you sure you want to unshare "{unshareConfirm.folder_name}"? The shared user or group will lose access.
            </p>
            <div className="flex gap-3 justify-end">
              <button
                onClick={() => setUnshareConfirm(null)}
                className="px-4 py-2 text-sm font-medium text-gray-600 dark:text-gray-400 min-h-[44px]"
              >
                Cancel
              </button>
              <button
                onClick={() => handleUnshareFolder(unshareConfirm)}
                className="px-4 py-2 text-sm font-medium text-white bg-red-500 rounded-lg min-h-[44px]"
                data-testid="confirm-unshare-btn"
              >
                Unshare
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
