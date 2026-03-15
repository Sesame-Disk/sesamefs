import React, { useState, useEffect, useCallback } from 'react';
import { Users, BookOpen, ArrowLeft, Shield, Crown, User, UserPlus, Trash2, ShieldCheck, ShieldOff } from 'lucide-react';
import { listGroups, listGroupRepos, listGroupMembers, getAccountInfo, addGroupMembers, deleteGroupMember, setGroupAdmin } from '../../lib/api';
import type { Group, GroupRepo, GroupMember } from '../../lib/api';
import BottomSheet from '../ui/BottomSheet';
import AddMemberSheet from '../groups/AddMemberSheet';

interface GroupDetailProps {
  groupId?: string;
}

type Tab = 'libraries' | 'members';

export default function GroupDetail({ groupId }: GroupDetailProps) {
  const [group, setGroup] = useState<Group | null>(null);
  const [repos, setRepos] = useState<GroupRepo[]>([]);
  const [members, setMembers] = useState<GroupMember[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [activeTab, setActiveTab] = useState<Tab>('libraries');
  const [currentUserEmail, setCurrentUserEmail] = useState('');

  // Member management state
  const [addMemberOpen, setAddMemberOpen] = useState(false);
  const [confirmRemoveOpen, setConfirmRemoveOpen] = useState(false);
  const [targetMember, setTargetMember] = useState<GroupMember | null>(null);
  const [memberActionLoading, setMemberActionLoading] = useState(false);
  const [memberActionError, setMemberActionError] = useState('');

  useEffect(() => {
    getAccountInfo().then((info) => setCurrentUserEmail(info.email)).catch(() => {});
  }, []);

  const fetchData = useCallback(async () => {
    if (!groupId) return;
    try {
      const [groups, repoData, memberData] = await Promise.all([
        listGroups(),
        listGroupRepos(groupId),
        listGroupMembers(groupId),
      ]);
      const found = groups.find((g) => String(g.id) === groupId);
      if (!found) {
        setError('Group not found');
        return;
      }
      setGroup(found);
      setRepos(repoData);
      setMembers(memberData);
      setError('');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load group');
    }
  }, [groupId]);

  useEffect(() => {
    fetchData().finally(() => setLoading(false));
  }, [fetchData]);

  const currentMember = members.find((m) => m.email === currentUserEmail);
  const isOwner = group?.owner === currentUserEmail;
  const isAdmin = currentMember?.role === 'admin';
  const canManageMembers = isOwner || isAdmin;

  const handleAddMembers = async (emails: string[]) => {
    if (!groupId) return;
    await addGroupMembers(Number(groupId), emails);
    await fetchData();
  };

  const handleRemoveMember = async () => {
    if (!groupId || !targetMember) return;
    setMemberActionLoading(true);
    setMemberActionError('');
    try {
      await deleteGroupMember(Number(groupId), targetMember.email);
      setConfirmRemoveOpen(false);
      setTargetMember(null);
      await fetchData();
    } catch (err) {
      setMemberActionError(err instanceof Error ? err.message : 'Failed to remove member');
    } finally {
      setMemberActionLoading(false);
    }
  };

  const handleToggleAdmin = async (member: GroupMember) => {
    if (!groupId) return;
    const newIsAdmin = member.role !== 'admin';
    await setGroupAdmin(Number(groupId), member.email, newIsAdmin);
    await fetchData();
  };

  if (!groupId) {
    return (
      <div className="flex flex-col items-center justify-center p-8 text-center">
        <Users className="w-12 h-12 text-gray-300 mb-4" />
        <p className="text-gray-500">No group selected</p>
      </div>
    );
  }

  if (loading) {
    return (
      <div className="flex flex-col gap-4 p-4">
        <div className="animate-pulse">
          <div className="h-6 bg-gray-200 rounded w-1/2 mb-2" />
          <div className="h-4 bg-gray-200 rounded w-1/3 mb-4" />
          <div className="h-10 bg-gray-200 rounded mb-4" />
          <div className="h-16 bg-gray-200 rounded mb-2" />
          <div className="h-16 bg-gray-200 rounded mb-2" />
          <div className="h-16 bg-gray-200 rounded" />
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex flex-col items-center justify-center p-8 text-center">
        <p role="alert" className="text-red-500 mb-4">{error}</p>
        <button
          onClick={() => {
            setLoading(true);
            setError('');
            fetchData().finally(() => setLoading(false));
          }}
          className="text-primary font-medium min-h-[44px]"
        >
          Retry
        </button>
      </div>
    );
  }

  if (!group) return null;

  return (
    <div className="flex flex-col h-full">
      {/* Back button */}
      <div className="px-4 pt-2">
        <a
          href="/groups"
          className="inline-flex items-center gap-1 text-primary text-sm font-medium min-h-[44px]"
        >
          <ArrowLeft className="w-4 h-4" />
          Groups
        </a>
      </div>

      {/* Group header */}
      <div className="px-4 py-3">
        <div className="flex items-center gap-3 mb-2">
          <div className="flex items-center justify-center w-12 h-12 rounded-full bg-primary/10">
            <Users className="w-6 h-6 text-primary" />
          </div>
          <div className="flex-1 min-w-0">
            <h1 className="text-xl font-semibold text-text truncate">{group.name}</h1>
            <p className="text-sm text-gray-500">
              {group.member_count} {group.member_count === 1 ? 'member' : 'members'} · Owner: {group.owner}
            </p>
          </div>
        </div>
      </div>

      {/* Tabs */}
      <div className="flex border-b border-gray-200 dark:border-dark-border px-4">
        <button
          onClick={() => setActiveTab('libraries')}
          className={`flex items-center gap-1.5 px-4 py-2 text-sm font-medium border-b-2 min-h-[44px] ${
            activeTab === 'libraries'
              ? 'border-primary text-primary'
              : 'border-transparent text-gray-500'
          }`}
        >
          <BookOpen className="w-4 h-4" />
          Libraries ({repos.length})
        </button>
        <button
          onClick={() => setActiveTab('members')}
          className={`flex items-center gap-1.5 px-4 py-2 text-sm font-medium border-b-2 min-h-[44px] ${
            activeTab === 'members'
              ? 'border-primary text-primary'
              : 'border-transparent text-gray-500'
          }`}
        >
          <Users className="w-4 h-4" />
          Members ({members.length})
        </button>
      </div>

      {/* Tab content */}
      <div className="flex-1 overflow-y-auto px-4 py-3 pb-20">
        {activeTab === 'libraries' && (
          <>
            {repos.length === 0 ? (
              <div className="flex flex-col items-center justify-center p-8 text-center">
                <BookOpen className="w-10 h-10 text-gray-300 mb-3" />
                <p className="text-gray-500">No libraries in this group</p>
              </div>
            ) : (
              <div className="flex flex-col gap-2">
                {repos.map((repo) => (
                  <div
                    key={repo.repo_id}
                    className="flex items-center gap-3 bg-white rounded-lg px-4 py-3 shadow-sm dark:bg-dark-surface dark:border-dark-border"
                  >
                    <BookOpen className="w-5 h-5 text-primary flex-shrink-0" />
                    <div className="flex-1 min-w-0">
                      <div className="font-medium text-text truncate">{repo.repo_name}</div>
                      <div className="text-xs text-gray-500">
                        {repo.owner_name} · {repo.permission}
                        {repo.encrypted && ' · Encrypted'}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </>
        )}

        {activeTab === 'members' && (
          <>
            {members.length === 0 ? (
              <div className="flex flex-col items-center justify-center p-8 text-center">
                <Users className="w-10 h-10 text-gray-300 mb-3" />
                <p className="text-gray-500">No members</p>
              </div>
            ) : (
              <div className="flex flex-col gap-2">
                {members.map((member) => (
                  <div
                    key={member.email}
                    className="flex items-center gap-3 bg-white rounded-lg px-4 py-3 shadow-sm dark:bg-dark-surface dark:border-dark-border"
                  >
                    {member.avatar_url ? (
                      <img
                        src={member.avatar_url}
                        alt=""
                        className="w-8 h-8 rounded-full"
                      />
                    ) : (
                      <User className="w-8 h-8 text-gray-400" />
                    )}
                    <div className="flex-1 min-w-0">
                      <div className="font-medium text-text truncate">{member.name}</div>
                      <div className="text-xs text-gray-500 truncate">{member.email}</div>
                    </div>
                    <RoleBadge role={member.role} />
                    {canManageMembers && member.role !== 'owner' && member.email !== currentUserEmail && (
                      <div className="flex items-center gap-1">
                        {isOwner && (
                          <button
                            onClick={() => handleToggleAdmin(member)}
                            className="p-2 min-h-[44px] min-w-[44px] flex items-center justify-center text-gray-400 hover:text-primary"
                            aria-label={member.role === 'admin' ? 'Remove Admin' : 'Set as Admin'}
                            title={member.role === 'admin' ? 'Remove Admin' : 'Set as Admin'}
                          >
                            {member.role === 'admin' ? (
                              <ShieldOff className="w-4 h-4" />
                            ) : (
                              <ShieldCheck className="w-4 h-4" />
                            )}
                          </button>
                        )}
                        <button
                          onClick={() => {
                            setTargetMember(member);
                            setConfirmRemoveOpen(true);
                            setMemberActionError('');
                          }}
                          className="p-2 min-h-[44px] min-w-[44px] flex items-center justify-center text-gray-400 hover:text-red-500"
                          aria-label="Remove Member"
                          title="Remove Member"
                        >
                          <Trash2 className="w-4 h-4" />
                        </button>
                      </div>
                    )}
                  </div>
                ))}
              </div>
            )}
          </>
        )}
      </div>

      {/* Add Member FAB - only on members tab */}
      {activeTab === 'members' && canManageMembers && (
        <button
          onClick={() => setAddMemberOpen(true)}
          className="fixed bottom-20 right-4 w-14 h-14 bg-primary-button text-white rounded-full shadow-lg flex items-center justify-center z-40 active:bg-primary-hover"
          aria-label="Add Member"
        >
          <UserPlus className="w-6 h-6" />
        </button>
      )}

      <AddMemberSheet
        isOpen={addMemberOpen}
        onClose={() => setAddMemberOpen(false)}
        onAdd={handleAddMembers}
      />

      <BottomSheet
        isOpen={confirmRemoveOpen}
        onClose={() => setConfirmRemoveOpen(false)}
        title="Remove Member"
      >
        <p className="text-gray-600 mb-4">
          Remove <strong>{targetMember?.name}</strong> from this group?
        </p>
        {memberActionError && <p role="alert" className="text-red-500 text-sm mb-3">{memberActionError}</p>}
        <div className="flex gap-3">
          <button
            onClick={() => setConfirmRemoveOpen(false)}
            disabled={memberActionLoading}
            className="flex-1 border border-gray-300 rounded-lg py-3 min-h-[44px] font-medium"
          >
            Cancel
          </button>
          <button
            onClick={handleRemoveMember}
            disabled={memberActionLoading}
            className="flex-1 bg-red-500 text-white rounded-lg py-3 min-h-[44px] font-medium disabled:opacity-50"
          >
            {memberActionLoading ? 'Removing...' : 'Remove'}
          </button>
        </div>
      </BottomSheet>
    </div>
  );
}

function RoleBadge({ role }: { role: string }) {
  const lower = role.toLowerCase();
  if (lower === 'owner') {
    return (
      <span className="inline-flex items-center gap-1 text-xs font-medium px-2 py-1 rounded-full bg-amber-100 text-amber-700">
        <Crown className="w-3 h-3" />
        Owner
      </span>
    );
  }
  if (lower === 'admin') {
    return (
      <span className="inline-flex items-center gap-1 text-xs font-medium px-2 py-1 rounded-full bg-blue-100 text-blue-700">
        <Shield className="w-3 h-3" />
        Admin
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1 text-xs font-medium px-2 py-1 rounded-full bg-gray-100 text-gray-600">
      <User className="w-3 h-3" />
      Member
    </span>
  );
}
