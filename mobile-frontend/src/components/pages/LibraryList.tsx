import React, { useState, useEffect, useCallback, useMemo } from 'react';
import { Library, Plus, ArrowUpDown, ChevronDown } from 'lucide-react';
import { listRepos, leaveShareRepo, getAccountInfo } from '../../lib/api';
import type { Repo } from '../../lib/models';
import { bytesToSize } from '../../lib/models';
import { cacheRepos, getCachedRepos } from '../../lib/offlineDb';
import LibraryCard from '../libraries/LibraryCard';
import NewLibrarySheet from '../libraries/NewLibrarySheet';
import LibraryContextMenu from '../libraries/LibraryContextMenu';
import RenameLibrarySheet from '../library/RenameLibrarySheet';
import DeleteLibrarySheet from '../library/DeleteLibrarySheet';
import TransferLibrarySheet from '../library/TransferLibrarySheet';
import LibraryDetailsSheet from '../library/LibraryDetailsSheet';
import BottomSheet from '../ui/BottomSheet';
import SkeletonList from '../ui/SkeletonList';
import { ContentCrossfade } from '../ui/SkeletonList';
import EmptyState from '../ui/EmptyState';
import FAB from '../ui/FAB';
import { AnimatedListItem } from '../ui/AnimatedList';
import PullToRefreshContainer from '../ui/PullToRefreshContainer';
import { AnimatePresence } from 'framer-motion';
import { getSortPreference, setSortPreference } from '../../lib/sortPreference';
import type { SortField, SortDirection } from '../../lib/sortPreference';

const sortOptions: { field: SortField; label: string }[] = [
  { field: 'name', label: 'Name' },
  { field: 'date', label: 'Last Modified' },
  { field: 'size', label: 'Size' },
];

function sortRepos(repos: Repo[], field: SortField, direction: SortDirection): Repo[] {
  const sorted = [...repos];
  sorted.sort((a, b) => {
    let cmp = 0;
    switch (field) {
      case 'name':
        cmp = a.repo_name.localeCompare(b.repo_name);
        break;
      case 'date':
        cmp = new Date(a.last_modified).getTime() - new Date(b.last_modified).getTime();
        break;
      case 'size':
        cmp = a.size - b.size;
        break;
    }
    return direction === 'asc' ? cmp : -cmp;
  });
  return sorted;
}

export default function LibraryList() {
  const [repos, setRepos] = useState<Repo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [sortField, setSortField] = useState<SortField>('name');
  const [sortDirection, setSortDirection] = useState<SortDirection>('asc');
  const [sortSheetOpen, setSortSheetOpen] = useState(false);
  const [newLibOpen, setNewLibOpen] = useState(false);
  const [contextRepo, setContextRepo] = useState<Repo | null>(null);
  const [contextMenuOpen, setContextMenuOpen] = useState(false);
  const [showingCached, setShowingCached] = useState(false);
  const [currentUserEmail, setCurrentUserEmail] = useState('');

  // Sheet states
  const [renameOpen, setRenameOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [transferOpen, setTransferOpen] = useState(false);
  const [detailsOpen, setDetailsOpen] = useState(false);

  // Load sort preference and user info on mount
  useEffect(() => {
    const pref = getSortPreference();
    setSortField(pref.field);
    setSortDirection(pref.direction);
    getAccountInfo().then((info) => setCurrentUserEmail(info.email)).catch(() => {});
  }, []);

  const fetchRepos = useCallback(async () => {
    try {
      const data = await listRepos();
      setRepos(data);
      setError('');
      setShowingCached(false);
      cacheRepos(data).catch(() => {});
    } catch (err) {
      // Try offline fallback
      try {
        const cached = await getCachedRepos();
        if (cached && cached.length > 0) {
          setRepos(cached);
          setShowingCached(true);
          setError('');
          return;
        }
      } catch {
        // ignore cache errors
      }
      setError(err instanceof Error ? err.message : 'Failed to load libraries');
    }
  }, []);

  useEffect(() => {
    fetchRepos().finally(() => setLoading(false));
  }, [fetchRepos]);

  const handleRefresh = async () => {
    await fetchRepos();
  };

  const sortedRepos = useMemo(
    () => sortRepos(repos, sortField, sortDirection),
    [repos, sortField, sortDirection],
  );

  const handleSortSelect = (field: SortField) => {
    const newDirection = field === sortField && sortDirection === 'asc' ? 'desc' : 'asc';
    setSortField(field);
    setSortDirection(newDirection);
    setSortPreference({ field, direction: newDirection });
    setSortSheetOpen(false);
  };

  const handleTap = (repo: Repo) => {
    window.location.href = `/libraries/${repo.repo_id}/`;
  };

  const handleLongPress = (repo: Repo) => {
    setContextRepo(repo);
    setContextMenuOpen(true);
  };

  const handleContextOpen = (repo: Repo) => {
    window.location.href = `/libraries/${repo.repo_id}/`;
  };

  const handleContextShare = (repo: Repo) => {
    if (navigator.share) {
      navigator.share({ title: repo.repo_name, url: `/libraries/${repo.repo_id}/` }).catch(() => {});
    }
  };

  const handleContextRename = (repo: Repo) => {
    setContextRepo(repo);
    setRenameOpen(true);
  };

  const handleContextDelete = (repo: Repo) => {
    setContextRepo(repo);
    setDeleteOpen(true);
  };

  const handleContextTransfer = (repo: Repo) => {
    setContextRepo(repo);
    setTransferOpen(true);
  };

  const handleContextLeave = async (repo: Repo) => {
    try {
      await leaveShareRepo(repo.repo_id);
      await fetchRepos();
    } catch {
      // Could show a toast, but for now silently fail
    }
  };

  const handleContextDetails = (repo: Repo) => {
    setContextRepo(repo);
    setDetailsOpen(true);
  };

  const isOwner = contextRepo ? contextRepo.owner_email === currentUserEmail : false;

  const currentSortLabel = sortOptions.find((o) => o.field === sortField)?.label ?? 'Name';

  if (error && repos.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center p-8 text-center">
        <p role="alert" className="text-red-500 mb-4">{error}</p>
        <button
          onClick={() => { setLoading(true); fetchRepos().finally(() => setLoading(false)); }}
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
        <h1 className="text-xl font-semibold text-text dark:text-dark-text">My Libraries</h1>
      </div>

      {showingCached && (
        <div className="mx-4 mt-1 px-3 py-1 bg-amber-100 dark:bg-amber-900/30 text-amber-800 dark:text-amber-200 text-xs rounded" data-testid="cached-indicator">
          Showing cached data
        </div>
      )}

      {/* Sort indicator bar */}
      {!loading && repos.length > 0 && (
        <button
          onClick={() => setSortSheetOpen(true)}
          className="flex items-center gap-1 px-4 py-2 text-sm text-gray-500 dark:text-gray-400"
          data-testid="sort-button"
        >
          <ArrowUpDown size={14} />
          <span>{currentSortLabel}</span>
          <ChevronDown size={14} />
          <span className="text-xs">({sortDirection === 'asc' ? '↑' : '↓'})</span>
        </button>
      )}

      <ContentCrossfade
        isLoading={loading}
        skeleton={<SkeletonList variant="library" count={6} />}
      >
        {repos.length === 0 ? (
          <EmptyState
            icon={<Library className="w-12 h-12" />}
            title="No libraries yet"
            description="Create a library to start storing your files."
            action={{ label: 'Create a library', onClick: () => setNewLibOpen(true) }}
          />
        ) : (
          <PullToRefreshContainer onRefresh={handleRefresh}>
            <div className="flex flex-col pb-20">
              <AnimatePresence mode="popLayout">
                {sortedRepos.map((repo, index) => (
                  <AnimatedListItem key={repo.repo_id} itemKey={repo.repo_id} index={index}>
                    <LibraryCard
                      repo={repo}
                      onTap={handleTap}
                      onLongPress={handleLongPress}
                    />
                  </AnimatedListItem>
                ))}
              </AnimatePresence>
            </div>
          </PullToRefreshContainer>
        )}
      </ContentCrossfade>

      {/* FAB */}
      <FAB
        actions={[
          {
            icon: <Plus size={20} />,
            label: 'New Library',
            onClick: () => setNewLibOpen(true),
          },
        ]}
      />

      {/* Sort bottom sheet */}
      <BottomSheet isOpen={sortSheetOpen} onClose={() => setSortSheetOpen(false)} title="Sort by">
        <div className="flex flex-col">
          {sortOptions.map(({ field, label }) => (
            <button
              key={field}
              onClick={() => handleSortSelect(field)}
              className={`flex items-center justify-between px-2 py-3 min-h-[44px] rounded-lg ${
                sortField === field ? 'text-primary bg-primary/5' : 'text-text dark:text-dark-text'
              }`}
            >
              <span className="text-sm">{label}</span>
              {sortField === field && (
                <span className="text-xs font-medium">
                  {sortDirection === 'asc' ? 'Ascending' : 'Descending'}
                </span>
              )}
            </button>
          ))}
        </div>
      </BottomSheet>

      {/* New Library sheet */}
      <NewLibrarySheet
        isOpen={newLibOpen}
        onClose={() => setNewLibOpen(false)}
        onCreated={handleRefresh}
      />

      {/* Context menu */}
      <LibraryContextMenu
        isOpen={contextMenuOpen}
        onClose={() => setContextMenuOpen(false)}
        repo={contextRepo}
        isOwner={isOwner}
        onOpen={handleContextOpen}
        onShare={handleContextShare}
        onRename={handleContextRename}
        onDelete={handleContextDelete}
        onTransfer={handleContextTransfer}
        onLeave={handleContextLeave}
        onDetails={handleContextDetails}
      />

      {/* Rename sheet */}
      <RenameLibrarySheet
        isOpen={renameOpen}
        onClose={() => setRenameOpen(false)}
        repo={contextRepo}
        onRenamed={handleRefresh}
      />

      {/* Delete sheet */}
      <DeleteLibrarySheet
        isOpen={deleteOpen}
        onClose={() => setDeleteOpen(false)}
        repo={contextRepo}
        onDeleted={handleRefresh}
      />

      {/* Transfer sheet */}
      <TransferLibrarySheet
        isOpen={transferOpen}
        onClose={() => setTransferOpen(false)}
        repo={contextRepo}
        onTransferred={handleRefresh}
      />

      {/* Details sheet */}
      <LibraryDetailsSheet
        isOpen={detailsOpen}
        onClose={() => setDetailsOpen(false)}
        repo={contextRepo}
      />
    </div>
  );
}
