import React from 'react';

interface TagBadgeProps {
  name: string;
  color: string;
  size?: 'small' | 'normal';
  onClick?: () => void;
}

export default function TagBadge({ name, color, size = 'normal', onClick }: TagBadgeProps) {
  const isSmall = size === 'small';

  return (
    <span
      role="status"
      onClick={onClick}
      className={`inline-flex items-center rounded-full font-medium text-white truncate ${
        isSmall ? 'px-1.5 py-0.5 text-[10px] max-w-[80px]' : 'px-2.5 py-1 text-xs max-w-[120px]'
      } ${onClick ? 'cursor-pointer' : ''}`}
      style={{ backgroundColor: color }}
      title={name}
      data-testid="tag-badge"
    >
      {name}
    </span>
  );
}
