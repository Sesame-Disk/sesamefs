import React from 'react';
import { File, Folder, Star } from 'lucide-react';
import BottomSheet from '../ui/BottomSheet';
import { bytesToSize, formatDate } from '../../lib/models';
import type { Dirent } from '../../lib/models';

interface FileDetailsSheetProps {
  isOpen: boolean;
  onClose: () => void;
  dirent: Dirent | null;
  path: string;
}

function DetailRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between py-2 border-b border-gray-100">
      <span className="text-gray-500 text-sm">{label}</span>
      <span className="text-text text-sm font-medium text-right max-w-[60%] break-all">{value}</span>
    </div>
  );
}

export default function FileDetailsSheet({ isOpen, onClose, dirent, path }: FileDetailsSheetProps) {
  if (!dirent) return null;

  const fullPath = path === '/' ? `/${dirent.name}` : `${path}/${dirent.name}`;
  const Icon = dirent.type === 'dir' ? Folder : File;

  return (
    <BottomSheet isOpen={isOpen} onClose={onClose} title="Details">
      <div className="flex items-center gap-3 mb-4">
        <Icon className="w-8 h-8 text-primary flex-shrink-0" />
        <div className="min-w-0">
          <p className="text-text font-medium truncate">{dirent.name}</p>
          <p className="text-gray-500 text-sm">{dirent.type === 'dir' ? 'Folder' : 'File'}</p>
        </div>
        {dirent.starred && <Star className="w-5 h-5 text-yellow-500 fill-yellow-500 ml-auto flex-shrink-0" />}
      </div>
      <div className="flex flex-col">
        {dirent.type === 'file' && (
          <DetailRow label="Size" value={bytesToSize(dirent.size)} />
        )}
        <DetailRow label="Last modified" value={formatDate(dirent.mtime)} />
        <DetailRow label="Path" value={fullPath} />
        {dirent.modifier_name && (
          <DetailRow label="Modified by" value={dirent.modifier_name} />
        )}
      </div>
    </BottomSheet>
  );
}
