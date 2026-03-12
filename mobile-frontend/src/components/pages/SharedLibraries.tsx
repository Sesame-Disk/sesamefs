import React, { useState, useEffect, useCallback } from 'react';
import { Share2, Library, Lock } from 'lucide-react';
import { listSharedRepos, listBeSharedRepos } from '../../lib/api';
import type { SharedRepo } from '../../lib/api';
import { formatDate } from '../../lib/models';
import SkeletonList from '../ui/SkeletonList';
import { ContentCrossfade } from '../ui/SkeletonList';
import EmptyState from '../ui/EmptyState';

type Tab = 'with-me' | 'by-me';

function permissionLabel(perm: string): string {
  if (perm === 'rw') return 'Read-Write';
  if (perm === 'r') return 'Read-Only';
  return perm;
}

function permissionColor(perm: string): string {
  if (perm === 'rw') return 'text-green-600 dark:text-green-400';
  return 'text-gray-500 dark:text-gray-400';
}

export default function SharedLibraries() {
  const [activeTab, setActiveTab] = useState<Tab>('with-me');
  const [sharedWithMe, setSharedWithMe] = useState<SharedRepo[]>([]);
  const [sharedByMe, setSharedByMe] = useState<SharedRepo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [refreshing, setRefreshing] = useState(false);

  const fetchRepos = useCallback(async (tab: Tab) => {
    try {
      if (tab === 'with-me') {
        const data = await listBeSharedRepos();
        setSharedWithMe(data);
      } else {
        const data = await listSharedRepos();
        setSharedByMe(data);
      }
      setError('');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load shared libraries');
    }
  }, []);

  useEffect(() => {
    setLoading(true);
    fetchRepos(activeTab).finally(() => setLoading(false));
  }, [activeTab, fetchRepos]);

  const handleRefresh = async () => {
    setRefreshing(true);
    await fetchRepos(activeTab);
    setRefreshing(false);
  };

  const repos = activeTab === 'with-me' ? sharedWithMe : sharedByMe;

  if (error && repos.length === 0) {
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
        <h1 className="text-xl font-semibold text-text dark:text-dark-text">Shared Libraries</h1>
        <button
          onClick={handleRefresh}
          disabled={refreshing}
          className="text-sm text-primary font-medium min-h-[44px]"
        >
          {refreshing ? 'Refreshing...' : 'Refresh'}
        </button>
      </div>

      <div className="flex border-b border-gray-200 dark:border-dark-border px-4">
        <button
          onClick={() => setActiveTab('with-me')}
          className={`flex-1 py-3 text-sm font-medium text-center border-b-2 transition-colors ${
            activeTab === 'with-me'
              ? 'border-primary text-primary'
              : 'border-transparent text-gray-500 dark:text-gray-400'
          }`}
        >
          Shared with me
        </button>
        <button
          onClick={() => setActiveTab('by-me')}
          className={`flex-1 py-3 text-sm font-medium text-center border-b-2 transition-colors ${
            activeTab === 'by-me'
              ? 'border-primary text-primary'
              : 'border-transparent text-gray-500 dark:text-gray-400'
          }`}
        >
          Shared by me
        </button>
      </div>

      <ContentCrossfade
        isLoading={loading}
        skeleton={<SkeletonList variant="file" count={5} />}
      >
        {repos.length === 0 ? (
          <EmptyState
            icon={<Share2 className="w-12 h-12" />}
            title={
              activeTab === 'with-me'
                ? 'No libraries shared with you'
                : "You haven't shared any libraries"
            }
            description={
              activeTab === 'with-me'
                ? 'Libraries shared by others will appear here.'
                : 'Share a library to collaborate with others.'
            }
          />
        ) : (
          <div className="flex flex-col pb-20">
            {repos.map((repo) => (
              <div
                key={`${repo.repo_id}-${repo.user}`}
                className="flex items-center gap-3 px-4 py-3 min-h-[56px] border-b border-gray-100 dark:border-dark-border"
              >
                <div className="relative flex-shrink-0">
                  <Library className="w-10 h-10 text-blue-500" />
                  {repo.encrypted ? (
                    <Lock className="w-3.5 h-3.5 text-yellow-600 absolute -bottom-0.5 -right-0.5" />
                  ) : null}
                </div>
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium text-text dark:text-dark-text truncate">
                    {repo.repo_name}
                  </p>
                  <p className="text-xs text-gray-500 dark:text-gray-400 truncate">
                    {activeTab === 'with-me' ? `From ${repo.user}` : `To ${repo.user}`}
                  </p>
                  <div className="flex items-center gap-2">
                    <span className={`text-xs font-medium ${permissionColor(repo.permission)}`}>
                      {permissionLabel(repo.permission)}
                    </span>
                    {repo.last_modified ? (
                      <span className="text-xs text-gray-400 dark:text-gray-500">
                        &middot; {formatDate(repo.last_modified)}
                      </span>
                    ) : null}
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </ContentCrossfade>
    </div>
  );
}
