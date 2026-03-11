import React from 'react';
import {
  Folder,
  Image,
  Video,
  FileText,
  FileCode,
  File,
  Music,
  Archive,
} from 'lucide-react';

type FileType = 'folder' | 'image' | 'video' | 'pdf' | 'doc' | 'code' | 'audio' | 'archive' | 'generic';

interface FileIconProps {
  type: FileType;
  size?: number;
  className?: string;
}

const iconMap: Record<FileType, React.ElementType> = {
  folder: Folder,
  image: Image,
  video: Video,
  pdf: FileText,
  doc: FileText,
  code: FileCode,
  audio: Music,
  archive: Archive,
  generic: File,
};

const colorMap: Record<FileType, string> = {
  folder: 'text-yellow-500',
  image: 'text-green-500',
  video: 'text-purple-500',
  pdf: 'text-red-500',
  doc: 'text-blue-500',
  code: 'text-gray-600',
  audio: 'text-pink-500',
  archive: 'text-orange-500',
  generic: 'text-gray-400',
};

export default function FileIcon({ type, size = 20, className = '' }: FileIconProps) {
  const Icon = iconMap[type] || iconMap.generic;
  const color = colorMap[type] || colorMap.generic;

  return (
    <span data-testid="file-icon" data-file-type={type}>
      <Icon size={size} className={`${color} ${className}`} />
    </span>
  );
}
