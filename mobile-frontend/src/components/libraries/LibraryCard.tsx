import React from 'react';
import { Library, Lock } from 'lucide-react';
import type { Repo } from '../../lib/models';
import { bytesToSize } from '../../lib/models';

interface LibraryCardProps {
  repo: Repo;
  onTap: (repo: Repo) => void;
}

export default function LibraryCard({ repo, onTap }: LibraryCardProps) {
  return (
    <button
      onClick={() => onTap(repo)}
      className="w-full flex items-center gap-3 px-4 py-3 min-h-[44px] hover:bg-gray-50 active:bg-gray-50 text-left"
    >
      <Library className="w-5 h-5 text-primary flex-shrink-0" />
      <div className="flex-1 min-w-0">
        <p className="text-text text-base truncate">{repo.repo_name}</p>
        <p className="text-gray-400 text-xs">{bytesToSize(repo.size)}</p>
      </div>
      {repo.encrypted && <Lock className="w-4 h-4 text-gray-400 flex-shrink-0" />}
    </button>
  );
}
