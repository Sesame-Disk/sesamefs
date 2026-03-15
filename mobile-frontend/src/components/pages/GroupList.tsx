import React, { useState, useEffect, useCallback } from 'react';
import { Users, Plus } from 'lucide-react';
import { listGroups, listGroupMembers, getAccountInfo, renameGroup, deleteGroup, transferGroup, quitGroup } from '../../lib/api';
import type { Group } from '../../lib/api';
import GroupCard from '../groups/GroupCard';
import NewGroupDialog from '../groups/NewGroupDialog';
import GroupContextMenu from '../groups/GroupContextMenu';
import RenameGroupSheet from '../groups/RenameGroupSheet';
import DeleteGroupSheet from '../groups/DeleteGroupSheet';
import TransferGroupSheet from '../groups/TransferGroupSheet';
import LeaveGroupSheet from '../groups/LeaveGroupSheet';

export default function GroupList() {
  const [groups, setGroups] = useState<Group[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [showNewGroup, setShowNewGroup] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [currentUserEmail, setCurrentUserEmail] = useState('');

  // Context menu state
  const [contextGroup, setContextGroup] = useState<Group | null>(null);
  const [contextMenuOpen, setContextMenuOpen] = useState(false);
  const [isOwner, setIsOwner] = useState(false);
  const [isAdmin, setIsAdmin] = useState(false);

  // Sheet states
  const [renameOpen, setRenameOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [transferOpen, setTransferOpen] = useState(false);
  const [leaveOpen, setLeaveOpen] = useState(false);

  useEffect(() => {
    getAccountInfo().then((info) => setCurrentUserEmail(info.email)).catch(() => {});
  }, []);

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

  const handleLongPress = useCallback(async (group: Group) => {
    setContextGroup(group);
    const ownerMatch = group.owner === currentUserEmail;
    setIsOwner(ownerMatch);

    if (!ownerMatch) {
      try {
        const members = await listGroupMembers(String(group.id));
        const me = members.find((m) => m.email === currentUserEmail);
        setIsAdmin(me?.role === 'admin');
      } catch {
        setIsAdmin(false);
      }
    } else {
      setIsAdmin(false);
    }

    setContextMenuOpen(true);
  }, [currentUserEmail]);

  const handleRename = async (newName: string) => {
    if (!contextGroup) return;
    await renameGroup(contextGroup.id, newName);
    await fetchGroups();
  };

  const handleDelete = async () => {
    if (!contextGroup) return;
    await deleteGroup(contextGroup.id);
    await fetchGroups();
  };

  const handleTransfer = async (email: string) => {
    if (!contextGroup) return;
    await transferGroup(contextGroup.id, email);
    await fetchGroups();
  };

  const handleLeave = async () => {
    if (!contextGroup) return;
    await quitGroup(contextGroup.id);
    await fetchGroups();
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
            <GroupCard key={group.id} group={group} onLongPress={handleLongPress} />
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

      <GroupContextMenu
        isOpen={contextMenuOpen}
        onClose={() => setContextMenuOpen(false)}
        group={contextGroup}
        isOwner={isOwner}
        isAdmin={isAdmin}
        onOpen={() => {
          if (contextGroup) window.location.href = `/groups/${contextGroup.id}`;
        }}
        onRename={() => setRenameOpen(true)}
        onTransfer={() => setTransferOpen(true)}
        onDelete={() => setDeleteOpen(true)}
        onLeave={() => setLeaveOpen(true)}
      />

      <RenameGroupSheet
        isOpen={renameOpen}
        onClose={() => setRenameOpen(false)}
        currentName={contextGroup?.name ?? ''}
        onRename={handleRename}
      />

      <DeleteGroupSheet
        isOpen={deleteOpen}
        onClose={() => setDeleteOpen(false)}
        groupName={contextGroup?.name ?? ''}
        onDelete={handleDelete}
      />

      <TransferGroupSheet
        isOpen={transferOpen}
        onClose={() => setTransferOpen(false)}
        groupName={contextGroup?.name ?? ''}
        onTransfer={handleTransfer}
      />

      <LeaveGroupSheet
        isOpen={leaveOpen}
        onClose={() => setLeaveOpen(false)}
        groupName={contextGroup?.name ?? ''}
        onLeave={handleLeave}
      />
    </div>
  );
}
