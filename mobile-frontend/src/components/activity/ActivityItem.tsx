import React from 'react';
import type { Activity } from '../../lib/models';
import Avatar from '../ui/Avatar';

interface ActivityItemProps {
  activity: Activity;
}

const actionLabels: Record<string, string> = {
  create: 'Created',
  delete: 'Deleted',
  edit: 'Modified',
  rename: 'Renamed',
  move: 'Moved',
};

const actionPrepositions: Record<string, string> = {
  create: 'in',
  delete: 'from',
  edit: 'in',
  rename: 'in',
  move: 'in',
};

function formatRelativeTime(dateString: string): string {
  const date = new Date(dateString);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMins = Math.floor(diffMs / 60000);
  const diffHours = Math.floor(diffMs / 3600000);
  const diffDays = Math.floor(diffMs / 86400000);

  if (diffMins < 1) return 'just now';
  if (diffMins < 60) return `${diffMins}m ago`;
  if (diffHours < 24) return `${diffHours}h ago`;
  if (diffDays < 7) return `${diffDays}d ago`;
  return date.toLocaleDateString();
}

export function getDayKey(dateString: string): string {
  const date = new Date(dateString);
  return date.toLocaleDateString(undefined, {
    weekday: 'long',
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  });
}

export default function ActivityItem({ activity }: ActivityItemProps) {
  const action = actionLabels[activity.op_type] || activity.op_type;
  const preposition = actionPrepositions[activity.op_type] || 'in';

  const fileUrl = activity.op_type !== 'delete'
    ? `/libraries/${activity.repo_id}${activity.path}`
    : undefined;

  return (
    <div className="flex gap-3 px-4 py-3" data-testid="activity-item">
      <Avatar name={activity.author_name} src={activity.avatar_url} size="sm" />
      <div className="flex-1 min-w-0">
        <p className="text-sm text-text dark:text-dark-text">
          <span className="font-medium">{activity.author_name}</span>
          {' '}{action}{' '}
          {fileUrl ? (
            <a
              href={fileUrl}
              className="text-primary font-medium hover:underline"
              data-testid="activity-file-link"
            >
              {activity.name}
            </a>
          ) : (
            <span className="font-medium">{activity.name}</span>
          )}
          {' '}{preposition}{' '}
          <span className="text-gray-500">{activity.repo_name}</span>
        </p>
        <p className="text-xs text-gray-400 mt-0.5" data-testid="activity-time">
          {formatRelativeTime(activity.time)}
        </p>
      </div>
    </div>
  );
}
