import React, { useState, useEffect, useCallback } from 'react';
import { Star, StarOff, File, Folder } from 'lucide-react';
import { listStarredFiles, unstarFile } from '../../lib/api';
import type { StarredFile } from '../../lib/api';
import { bytesToSize, formatDate } from '../../lib/models';
import SwipeableListItem from '../ui/SwipeableListItem';
import SkeletonList from '../ui/SkeletonList';
import { ContentCrossfade } from '../ui/SkeletonList';
import EmptyState from '../ui/EmptyState';

export default function StarredFiles() {
  const [files, setFiles] = useState<StarredFile[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [refreshing, setRefreshing] = useState(false);

  const fetchStarred = useCallback(async () => {
    try {
      const data = await listStarredFiles();
      setFiles(data);
      setError('');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load starred files');
    }
  }, []);

  useEffect(() => {
    fetchStarred().finally(() => setLoading(false));
  }, [fetchStarred]);

  const handleRefresh = async () => {
    setRefreshing(true);
    await fetchStarred();
    setRefreshing(false);
  };

  const handleUnstar = async (file: StarredFile) => {
    try {
      await unstarFile(file.repo_id, file.path);
      setFiles((prev) => prev.filter((f) => !(f.repo_id === file.repo_id && f.path === file.path)));
    } catch {
      setError('Failed to unstar file');
    }
  };

  if (error && files.length === 0) {
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
        <h1 className="text-xl font-semibold text-text dark:text-dark-text">Starred Files</h1>
        <button
          onClick={handleRefresh}
          disabled={refreshing}
          className="text-sm text-primary font-medium min-h-[44px]"
        >
          {refreshing ? 'Refreshing...' : 'Refresh'}
        </button>
      </div>

      <ContentCrossfade
        isLoading={loading}
        skeleton={<SkeletonList variant="file" count={5} />}
      >
        {files.length === 0 ? (
          <EmptyState
            icon={<Star className="w-12 h-12" />}
            title="No starred files yet"
            description="Star important files for quick access. Swipe left on a file in the file browser and tap Star."
          />
        ) : (
          <div className="flex flex-col pb-20">
            {files.map((file) => (
              <SwipeableListItem
                key={`${file.repo_id}:${file.path}`}
                rightActions={[
                  {
                    icon: <StarOff className="w-5 h-5" />,
                    label: 'Unstar',
                    color: '#f59e0b',
                    onClick: () => handleUnstar(file),
                  },
                ]}
              >
                <a
                  href={`/libraries/${file.repo_id}${file.is_dir ? file.path : file.path.substring(0, file.path.lastIndexOf('/') + 1)}`}
                  className="flex items-center gap-3 px-4 py-3 min-h-[56px] border-b border-gray-100 dark:border-dark-border"
                >
                  {file.is_dir ? (
                    <Folder className="w-10 h-10 text-yellow-500 fill-yellow-100 flex-shrink-0" />
                  ) : (
                    <File className="w-10 h-10 text-gray-400 flex-shrink-0" />
                  )}
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium text-text dark:text-dark-text truncate">
                      {file.obj_name}
                    </p>
                    <p className="text-xs text-gray-500 dark:text-gray-400 truncate">
                      {file.repo_name} &middot; {file.path}
                    </p>
                    <p className="text-xs text-gray-400 dark:text-gray-500">
                      {bytesToSize(file.size)} &middot; {formatDate(file.mtime)}
                    </p>
                  </div>
                  <Star className="w-4 h-4 text-yellow-500 fill-yellow-500 flex-shrink-0" />
                </a>
              </SwipeableListItem>
            ))}
          </div>
        )}
      </ContentCrossfade>
    </div>
  );
}
