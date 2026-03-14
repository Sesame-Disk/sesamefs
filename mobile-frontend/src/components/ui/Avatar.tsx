import React, { useState } from 'react';

interface AvatarProps {
  name: string;
  src?: string;
  size?: 'sm' | 'md' | 'lg';
}

const sizeClasses: Record<string, string> = {
  sm: 'w-8 h-8 text-xs',
  md: 'w-10 h-10 text-sm',
  lg: 'w-14 h-14 text-lg',
};

function getInitials(name: string): string {
  return name
    .split(' ')
    .map((part) => part[0])
    .filter(Boolean)
    .slice(0, 2)
    .join('')
    .toUpperCase();
}

/** Returns true if the URL points to the backend's default placeholder avatar. */
function isDefaultAvatar(url: string): boolean {
  return /\/media\/avatars\/default\b/.test(url);
}

export default function Avatar({ name, src, size = 'md' }: AvatarProps) {
  const initials = getInitials(name);
  const [imgError, setImgError] = useState(false);

  const showImage = src && !imgError && !isDefaultAvatar(src);

  if (showImage) {
    return (
      <img
        src={src}
        alt={name}
        className={`${sizeClasses[size]} rounded-full object-cover`}
        data-testid="avatar"
        onError={() => setImgError(true)}
      />
    );
  }

  return (
    <div
      className={`${sizeClasses[size]} rounded-full bg-primary/10 text-primary font-semibold flex items-center justify-center`}
      data-testid="avatar"
      aria-label={name}
    >
      {initials}
    </div>
  );
}
