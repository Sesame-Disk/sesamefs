import React, { useState, useEffect } from 'react';
import { Users, Trash2 } from 'lucide-react';
import {
  listRepoShareItems,
  listRepoGroupShares,
  listGroups,
  shareToUser,
  shareToGroup,
  removeUserShare,
  removeGroupShare,
} from '../../lib/api';
import type { ShareItem, GroupShareItem, Group, SearchedUser } from '../../lib/api';
import UserPicker from './UserPicker';

interface InternalShareTabProps {
  repoId: string;
  path: string;
  onToast: (msg: string) => void;
}

export default function InternalShareTab({ repoId, path, onToast }: InternalShareTabProps) {
  const [userShares, setUserShares] = useState<ShareItem[]>([]);
  const [groupShares, setGroupShares] = useState<GroupShareItem[]>([]);
  const [groups, setGroups] = useState<Group[]>([]);
  const [loading, setLoading] = useState(true);
  const [sharing, setSharing] = useState(false);

  // New share state
  const [selectedUsers, setSelectedUsers] = useState<SearchedUser[]>([]);
  const [selectedGroupId, setSelectedGroupId] = useState<number | null>(null);
  const [permission, setPermission] = useState('r');

  useEffect(() => {
    loadShares();
  }, [repoId, path]);

  const loadShares = async () => {
    setLoading(true);
    try {
      const [users, groupItems, allGroups] = await Promise.all([
        listRepoShareItems(repoId, path),
        listRepoGroupShares(repoId, path),
        listGroups(),
      ]);
      setUserShares(users);
      setGroupShares(groupItems);
      setGroups(allGroups);
    } catch {
      onToast('Failed to load shares');
    } finally {
      setLoading(false);
    }
  };

  const handleShareUsers = async () => {
    if (selectedUsers.length === 0) return;
    setSharing(true);
    try {
      await Promise.all(
        selectedUsers.map(u => shareToUser(repoId, path, u.email, permission))
      );
      setSelectedUsers([]);
      onToast('Shared successfully');
      loadShares();
    } catch (err) {
      onToast(err instanceof Error ? err.message : 'Failed to share');
    } finally {
      setSharing(false);
    }
  };

  const handleShareGroup = async () => {
    if (selectedGroupId === null) return;
    setSharing(true);
    try {
      await shareToGroup(repoId, path, selectedGroupId, permission);
      setSelectedGroupId(null);
      onToast('Shared to group');
      loadShares();
    } catch (err) {
      onToast(err instanceof Error ? err.message : 'Failed to share to group');
    } finally {
      setSharing(false);
    }
  };

  const handleRemoveUser = async (email: string) => {
    try {
      await removeUserShare(repoId, path, email);
      setUserShares(prev => prev.filter(s => s.user_email !== email));
      onToast('Share removed');
    } catch {
      onToast('Failed to remove share');
    }
  };

  const handleRemoveGroup = async (groupId: number) => {
    try {
      await removeGroupShare(repoId, path, groupId);
      setGroupShares(prev => prev.filter(s => s.group_id !== groupId));
      onToast('Group share removed');
    } catch {
      onToast('Failed to remove group share');
    }
  };

  if (loading) {
    return <p className="text-center text-gray-400 py-8">Loading...</p>;
  }

  return (
    <div className="space-y-5">
      {/* Share with users */}
      <div>
        <h3 className="text-sm font-medium text-text mb-2">Share with users</h3>
        <UserPicker
          selectedUsers={selectedUsers}
          onSelect={user => setSelectedUsers(prev => [...prev, user])}
          onRemove={email => setSelectedUsers(prev => prev.filter(u => u.email !== email))}
        />
      </div>

      {/* Permission select */}
      <div>
        <label htmlFor="permission-select" className="text-sm text-gray-500 block mb-1">Permission</label>
        <select
          id="permission-select"
          value={permission}
          onChange={e => setPermission(e.target.value)}
          className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm"
        >
          <option value="r">Read-Only</option>
          <option value="rw">Read-Write</option>
        </select>
      </div>

      {/* Share user button */}
      {selectedUsers.length > 0 && (
        <button
          onClick={handleShareUsers}
          disabled={sharing}
          className="w-full bg-primary-button text-white py-2 rounded-lg min-h-[44px] disabled:opacity-50"
        >
          {sharing ? 'Sharing...' : `Share with ${selectedUsers.length} user${selectedUsers.length > 1 ? 's' : ''}`}
        </button>
      )}

      {/* Share with group */}
      {groups.length > 0 && (
        <div>
          <h3 className="text-sm font-medium text-text mb-2">Share with group</h3>
          <div className="flex gap-2">
            <select
              value={selectedGroupId ?? ''}
              onChange={e => setSelectedGroupId(e.target.value ? Number(e.target.value) : null)}
              className="flex-1 px-3 py-2 border border-gray-200 rounded-lg text-sm"
              aria-label="Select group"
            >
              <option value="">Select a group...</option>
              {groups.map(g => (
                <option key={g.id} value={g.id}>{g.name}</option>
              ))}
            </select>
            <button
              onClick={handleShareGroup}
              disabled={selectedGroupId === null || sharing}
              className="px-4 py-2 bg-primary-button text-white rounded-lg min-h-[44px] disabled:opacity-50"
            >
              Share
            </button>
          </div>
        </div>
      )}

      {/* Current user shares */}
      {userShares.length > 0 && (
        <div>
          <h3 className="text-sm font-medium text-text mb-2">Shared with users</h3>
          <div className="space-y-1">
            {userShares.map(share => (
              <div key={share.user_email} className="flex items-center gap-3 px-3 py-2 bg-gray-50 rounded-lg">
                <img src={share.avatar_url} alt="" className="w-8 h-8 rounded-full bg-gray-200" />
                <div className="flex-1 min-w-0">
                  <p className="text-sm text-text truncate">{share.user_name}</p>
                  <p className="text-xs text-gray-400">{share.permission === 'rw' ? 'Read-Write' : 'Read-Only'}</p>
                </div>
                <button
                  onClick={() => handleRemoveUser(share.user_email)}
                  className="min-h-[36px] min-w-[36px] flex items-center justify-center text-red-400"
                  aria-label={`Remove ${share.user_name}`}
                >
                  <Trash2 className="w-4 h-4" />
                </button>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Current group shares */}
      {groupShares.length > 0 && (
        <div>
          <h3 className="text-sm font-medium text-text mb-2">Shared with groups</h3>
          <div className="space-y-1">
            {groupShares.map(share => (
              <div key={share.group_id} className="flex items-center gap-3 px-3 py-2 bg-gray-50 rounded-lg">
                <Users className="w-5 h-5 text-gray-400" />
                <div className="flex-1 min-w-0">
                  <p className="text-sm text-text truncate">{share.group_name}</p>
                  <p className="text-xs text-gray-400">{share.permission === 'rw' ? 'Read-Write' : 'Read-Only'}</p>
                </div>
                <button
                  onClick={() => handleRemoveGroup(share.group_id)}
                  className="min-h-[36px] min-w-[36px] flex items-center justify-center text-red-400"
                  aria-label={`Remove ${share.group_name}`}
                >
                  <Trash2 className="w-4 h-4" />
                </button>
              </div>
            ))}
          </div>
        </div>
      )}

      {userShares.length === 0 && groupShares.length === 0 && (
        <p className="text-center text-gray-400 text-sm py-4">Not shared with anyone yet</p>
      )}
    </div>
  );
}
