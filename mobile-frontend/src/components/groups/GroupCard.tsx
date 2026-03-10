import React from 'react';
import { Users } from 'lucide-react';
import type { Group } from '../../lib/api';

interface GroupCardProps {
  group: Group;
}

export default function GroupCard({ group }: GroupCardProps) {
  return (
    <a
      href={`/groups/${group.id}`}
      className="flex items-center gap-3 bg-white rounded-lg px-4 py-3 shadow-sm active:bg-gray-50 dark:bg-dark-surface dark:border-dark-border dark:text-dark-text"
    >
      <div className="flex items-center justify-center w-10 h-10 rounded-full bg-primary/10">
        <Users className="w-5 h-5 text-primary" />
      </div>
      <div className="flex-1 min-w-0">
        <div className="font-medium text-text truncate">{group.name}</div>
        <div className="text-sm text-gray-500">
          {group.member_count} {group.member_count === 1 ? 'member' : 'members'}
        </div>
      </div>
      <div className="text-xs text-gray-400 truncate max-w-[120px]">{group.owner}</div>
    </a>
  );
}
