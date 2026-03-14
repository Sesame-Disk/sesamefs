import React, { useState, useEffect, useCallback } from 'react';
import { Users, Plus } from 'lucide-react';
import { listGroups } from '../../lib/api';
import type { Group } from '../../lib/api';
import GroupCard from '../groups/GroupCard';
import NewGroupDialog from '../groups/NewGroupDialog';

export default function GroupList() {
  const [groups, setGroups] = useState<Group[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [showNewGroup, setShowNewGroup] = useState(false);
  const [refreshing, setRefreshing] = useState(false);

  const fetchGroups = useCallback(async () => {
    try {
      const data = await listGroups();
      setGroups(data);
      setError('');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load groups');
    }
  }, []);

  useEffect(() => {
    fetchGroups().finally(() => setLoading(false));
  }, [fetchGroups]);

  const handleRefresh = async () => {
    setRefreshing(true);
    await fetchGroups();
    setRefreshing(false);
  };

  const handleCreated = () => {
    fetchGroups();
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center p-8">
        <div className="text-gray-400">Loading...</div>
      </div>
    );
  }

  if (error) {
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
      {/* Pull to refresh button */}
      <div className="px-4 pt-2 pb-1">
        <button
          onClick={handleRefresh}
          disabled={refreshing}
          className="text-sm text-primary font-medium min-h-[44px]"
        >
          {refreshing ? 'Refreshing...' : 'Refresh'}
        </button>
      </div>

      {groups.length === 0 ? (
        <div className="flex flex-col items-center justify-center p-8 text-center flex-1">
          <Users className="w-12 h-12 text-gray-300 mb-4" />
          <p className="text-gray-500">No groups</p>
        </div>
      ) : (
        <div className="flex flex-col gap-2 px-4 pb-20">
          {groups.map((group) => (
            <GroupCard key={group.id} group={group} />
          ))}
        </div>
      )}

      {/* FAB */}
      <button
        onClick={() => setShowNewGroup(true)}
        className="fixed bottom-20 right-4 w-14 h-14 bg-primary-button text-white rounded-full shadow-lg flex items-center justify-center z-40 active:bg-primary-hover"
        aria-label="Create New Group"
      >
        <Plus className="w-6 h-6" />
      </button>

      <NewGroupDialog
        open={showNewGroup}
        onClose={() => setShowNewGroup(false)}
        onCreated={handleCreated}
      />
    </div>
  );
}
