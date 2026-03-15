import React, { useState, useEffect } from 'react';
import { X, Link2, Users, UserPlus, CheckCircle, AlertCircle, Loader, Copy, Check } from 'lucide-react';
import { shareQueueManager, type ShareTask, type ShareResult } from '../../lib/shareQueue';
import type { QueueStats } from '../../lib/operationQueue';
import { searchUsers, listGroups, type SearchedUser, type Group } from '../../lib/api';
import type { Dirent } from '../../lib/models';

interface BatchShareSheetProps {
  isOpen: boolean;
  onClose: () => void;
  items: Dirent[];
  repoId: string;
  currentPath: string;
}

type ShareMode = 'link' | 'user' | 'group';
type Step = 'configure' | 'progress';

let batchIdCounter = 0;

export default function BatchShareSheet({
  isOpen,
  onClose,
  items,
  repoId,
  currentPath,
}: BatchShareSheetProps) {
  const [step, setStep] = useState<Step>('configure');
  const [mode, setMode] = useState<ShareMode>('link');
  const [permission, setPermission] = useState('r');

  // User share state
  const [userQuery, setUserQuery] = useState('');
  const [searchResults, setSearchResults] = useState<SearchedUser[]>([]);
  const [selectedUser, setSelectedUser] = useState<SearchedUser | null>(null);
  const [searching, setSearching] = useState(false);

  // Group share state
  const [groups, setGroups] = useState<Group[]>([]);
  const [selectedGroup, setSelectedGroup] = useState<Group | null>(null);
  const [loadingGroups, setLoadingGroups] = useState(false);

  // Progress state
  const [stats, setStats] = useState<QueueStats>({ queued: 0, processing: 0, completed: 0, failed: 0, total: 0 });
  const [results, setResults] = useState<ShareResult[]>([]);
  const [copiedLink, setCopiedLink] = useState<string | null>(null);

  useEffect(() => {
    if (!isOpen) {
      setStep('configure');
      setMode('link');
      setPermission('r');
      setSelectedUser(null);
      setSelectedGroup(null);
      setUserQuery('');
      return;
    }

    // Load groups when opened
    setLoadingGroups(true);
    listGroups().then(setGroups).catch(() => {}).finally(() => setLoadingGroups(false));
  }, [isOpen]);

  // User search
  useEffect(() => {
    if (userQuery.length < 2) { setSearchResults([]); return; }
    const timeout = setTimeout(async () => {
      setSearching(true);
      try {
        const users = await searchUsers(userQuery);
        setSearchResults(users);
      } catch {}
      setSearching(false);
    }, 300);
    return () => clearTimeout(timeout);
  }, [userQuery]);

  // Subscribe to share queue events
  useEffect(() => {
    if (step !== 'progress') return;

    const updateStats = async () => {
      const s = await shareQueueManager.getStats();
      setStats(s);
      setResults(shareQueueManager.getResults());
    };

    const unsub = shareQueueManager.subscribe(() => {
      updateStats();
    });
    updateStats();

    return unsub;
  }, [step]);

  if (!isOpen) return null;

  const startBatch = async () => {
    const batchPrefix = `batch-${Date.now()}-${++batchIdCounter}`;
    const tasks: ShareTask[] = items.map((item, i) => {
      const itemPath = currentPath === '/' ? `/${item.name}` : `${currentPath}/${item.name}`;
      return {
        id: `${batchPrefix}-${i}`,
        repoId,
        path: itemPath,
        fileName: item.name,
        shareType: mode,
        permission,
        userEmail: mode === 'user' ? selectedUser?.email : undefined,
        groupId: mode === 'group' ? selectedGroup?.id : undefined,
      };
    });

    setStep('progress');
    await shareQueueManager.addTasks(tasks);
  };

  const canStart = mode === 'link' ||
    (mode === 'user' && selectedUser) ||
    (mode === 'group' && selectedGroup);

  const copyLink = async (link: string) => {
    try {
      await navigator.clipboard.writeText(link);
      setCopiedLink(link);
      setTimeout(() => setCopiedLink(null), 2000);
    } catch {}
  };

  const isDone = stats.total > 0 && stats.queued === 0 && stats.processing === 0;

  return (
    <div className="fixed inset-0 z-50 flex items-end">
      <div className="fixed inset-0 bg-black/30" onClick={onClose} />
      <div className="relative w-full bg-white dark:bg-dark-surface rounded-t-2xl shadow-xl max-h-[80vh] flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-gray-100 dark:border-dark-border">
          <h3 className="text-lg font-medium text-text dark:text-dark-text">
            {step === 'configure' ? `Share ${items.length} items` : 'Sharing Progress'}
          </h3>
          <button onClick={onClose} className="min-h-[44px] min-w-[44px] flex items-center justify-center">
            <X className="w-5 h-5 text-gray-400" />
          </button>
        </div>

        {step === 'configure' && (
          <div className="flex-1 overflow-auto p-4">
            {/* Share mode tabs */}
            <div className="flex gap-2 mb-4">
              {([
                { mode: 'link' as ShareMode, icon: Link2, label: 'Share Links' },
                { mode: 'user' as ShareMode, icon: UserPlus, label: 'Share to User' },
                { mode: 'group' as ShareMode, icon: Users, label: 'Share to Group' },
              ]).map(({ mode: m, icon: Icon, label }) => (
                <button
                  key={m}
                  onClick={() => setMode(m)}
                  className={`flex-1 flex flex-col items-center gap-1 py-3 rounded-lg border ${
                    mode === m
                      ? 'border-primary bg-primary/5 text-primary'
                      : 'border-gray-200 dark:border-dark-border text-gray-500'
                  }`}
                >
                  <Icon className="w-5 h-5" />
                  <span className="text-xs">{label}</span>
                </button>
              ))}
            </div>

            {/* Permission selector */}
            <div className="mb-4">
              <label className="text-sm text-gray-500 dark:text-gray-400 mb-1 block">Permission</label>
              <div className="flex gap-2">
                <button
                  onClick={() => setPermission('r')}
                  className={`flex-1 py-2 rounded-lg border text-sm ${
                    permission === 'r'
                      ? 'border-primary bg-primary/5 text-primary'
                      : 'border-gray-200 dark:border-dark-border text-gray-500'
                  }`}
                >
                  Read Only
                </button>
                <button
                  onClick={() => setPermission('rw')}
                  className={`flex-1 py-2 rounded-lg border text-sm ${
                    permission === 'rw'
                      ? 'border-primary bg-primary/5 text-primary'
                      : 'border-gray-200 dark:border-dark-border text-gray-500'
                  }`}
                >
                  Read & Write
                </button>
              </div>
            </div>

            {/* User search */}
            {mode === 'user' && (
              <div className="mb-4">
                <label className="text-sm text-gray-500 dark:text-gray-400 mb-1 block">Share with user</label>
                <input
                  type="text"
                  placeholder="Search users..."
                  value={userQuery}
                  onChange={e => setUserQuery(e.target.value)}
                  className="w-full px-3 py-2 border border-gray-200 dark:border-dark-border rounded-lg text-sm bg-white dark:bg-dark-surface text-text dark:text-dark-text"
                />
                {searching && <p className="text-xs text-gray-400 mt-1">Searching...</p>}
                {searchResults.length > 0 && (
                  <div className="mt-2 border border-gray-200 dark:border-dark-border rounded-lg overflow-hidden max-h-40 overflow-y-auto">
                    {searchResults.map(user => (
                      <button
                        key={user.email}
                        onClick={() => { setSelectedUser(user); setUserQuery(user.name || user.email); setSearchResults([]); }}
                        className={`w-full text-left px-3 py-2 text-sm border-b last:border-b-0 border-gray-100 dark:border-dark-border ${
                          selectedUser?.email === user.email ? 'bg-primary/5 text-primary' : 'text-text dark:text-dark-text'
                        }`}
                      >
                        <span className="font-medium">{user.name}</span>
                        <span className="text-gray-400 ml-2">{user.email}</span>
                      </button>
                    ))}
                  </div>
                )}
                {selectedUser && (
                  <p className="text-xs text-primary mt-1">Selected: {selectedUser.name} ({selectedUser.email})</p>
                )}
              </div>
            )}

            {/* Group selector */}
            {mode === 'group' && (
              <div className="mb-4">
                <label className="text-sm text-gray-500 dark:text-gray-400 mb-1 block">Share with group</label>
                {loadingGroups ? (
                  <p className="text-xs text-gray-400">Loading groups...</p>
                ) : (
                  <div className="border border-gray-200 dark:border-dark-border rounded-lg overflow-hidden max-h-40 overflow-y-auto">
                    {groups.map(group => (
                      <button
                        key={group.id}
                        onClick={() => setSelectedGroup(group)}
                        className={`w-full text-left px-3 py-2 text-sm border-b last:border-b-0 border-gray-100 dark:border-dark-border ${
                          selectedGroup?.id === group.id ? 'bg-primary/5 text-primary' : 'text-text dark:text-dark-text'
                        }`}
                      >
                        {group.name}
                        <span className="text-gray-400 ml-2">({group.member_count} members)</span>
                      </button>
                    ))}
                    {groups.length === 0 && (
                      <p className="text-xs text-gray-400 p-3">No groups available</p>
                    )}
                  </div>
                )}
              </div>
            )}

            {/* Items summary */}
            <div className="mb-4">
              <label className="text-sm text-gray-500 dark:text-gray-400 mb-1 block">Items to share ({items.length})</label>
              <div className="border border-gray-200 dark:border-dark-border rounded-lg max-h-32 overflow-y-auto">
                {items.map(item => (
                  <div key={item.name} className="px-3 py-1.5 text-sm text-text dark:text-dark-text border-b last:border-b-0 border-gray-50 dark:border-dark-border truncate">
                    {item.name}
                  </div>
                ))}
              </div>
            </div>

            {/* Start button */}
            <button
              onClick={startBatch}
              disabled={!canStart}
              className="w-full py-3 bg-primary text-white rounded-lg font-medium disabled:opacity-50 min-h-[44px]"
            >
              Share {items.length} Items
            </button>
          </div>
        )}

        {step === 'progress' && (
          <div className="flex-1 overflow-auto">
            {/* Progress summary */}
            <div className="px-4 py-3 bg-gray-50 dark:bg-dark-bg">
              <div className="flex justify-between text-sm mb-2">
                <span className="text-gray-500">Progress</span>
                <span className="text-text dark:text-dark-text font-medium">
                  {stats.completed + stats.failed} / {stats.total}
                </span>
              </div>
              <div className="h-2 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden">
                <div
                  className="h-full bg-primary rounded-full transition-all duration-300"
                  style={{ width: stats.total > 0 ? `${((stats.completed + stats.failed) / stats.total) * 100}%` : '0%' }}
                />
              </div>
              {stats.failed > 0 && (
                <p className="text-xs text-red-500 mt-1">{stats.failed} failed</p>
              )}
            </div>

            {/* Results list */}
            {results.map(result => (
              <div key={result.taskId} className="flex items-center gap-3 px-4 py-3 border-b border-gray-50 dark:border-dark-border">
                {result.success ? (
                  <CheckCircle className="w-4 h-4 text-green-500 shrink-0" />
                ) : (
                  <AlertCircle className="w-4 h-4 text-red-500 shrink-0" />
                )}
                <div className="flex-1 min-w-0">
                  <p className="text-sm text-text dark:text-dark-text truncate">{result.fileName}</p>
                  {result.error && <p className="text-xs text-red-500">{result.error}</p>}
                </div>
                {result.shareLink && (
                  <button
                    onClick={() => copyLink(result.shareLink!)}
                    className="min-h-[44px] min-w-[44px] flex items-center justify-center"
                  >
                    {copiedLink === result.shareLink ? (
                      <Check className="w-4 h-4 text-green-500" />
                    ) : (
                      <Copy className="w-4 h-4 text-gray-400" />
                    )}
                  </button>
                )}
              </div>
            ))}

            {/* Pending items */}
            {shareQueueManager.getItems()
              .filter(item => item.status === 'queued' || item.status === 'processing')
              .map(item => (
                <div key={item.id} className="flex items-center gap-3 px-4 py-3 border-b border-gray-50 dark:border-dark-border">
                  <Loader className="w-4 h-4 text-gray-300 animate-spin shrink-0" />
                  <p className="text-sm text-gray-400 truncate flex-1">{(item as any).fileName}</p>
                </div>
              ))}

            {/* Done actions */}
            {isDone && (
              <div className="p-4">
                {stats.failed > 0 && (
                  <button
                    onClick={() => shareQueueManager.retryAllFailed()}
                    className="w-full py-3 border border-primary text-primary rounded-lg font-medium mb-2 min-h-[44px]"
                  >
                    Retry Failed ({stats.failed})
                  </button>
                )}
                <button
                  onClick={() => { shareQueueManager.clear(); onClose(); }}
                  className="w-full py-3 bg-primary text-white rounded-lg font-medium min-h-[44px]"
                >
                  Done
                </button>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
