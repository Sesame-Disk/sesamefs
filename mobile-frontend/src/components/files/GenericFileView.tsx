import React from 'react';
import { X, Download, Share2, FileIcon } from 'lucide-react';
import { shareFile, downloadFile } from '../../lib/share';
import { bytesToSize } from '../../lib/models';

interface GenericFileViewProps {
  url: string;
  fileName: string;
  fileSize: number;
  onClose: () => void;
  onToast?: (msg: string) => void;
}

export default function GenericFileView({ url, fileName, fileSize, onClose, onToast }: GenericFileViewProps) {
  const handleShare = async () => {
    const shared = await shareFile(fileName, url);
    if (shared) onToast?.('Link copied');
  };

  return (
    <div className="fixed inset-0 z-50 bg-white flex flex-col" data-testid="generic-file-view">
      {/* Top bar */}
      <div className="flex items-center justify-between p-2 border-b border-gray-200">
        <button
          onClick={onClose}
          className="min-h-[44px] min-w-[44px] flex items-center justify-center text-gray-600"
          aria-label="Close"
        >
          <X className="w-6 h-6" />
        </button>
        <p className="text-text text-sm truncate mx-2 flex-1 text-center font-medium">{fileName}</p>
        <div className="w-[44px]" />
      </div>

      {/* File info */}
      <div className="flex-1 flex flex-col items-center justify-center p-8 text-center">
        <FileIcon className="w-20 h-20 text-gray-300 mb-6" />
        <h2 className="text-lg font-medium text-text mb-2 break-all">{fileName}</h2>
        <p className="text-gray-500 text-sm mb-8">{bytesToSize(fileSize)}</p>

        <div className="flex gap-3">
          <button
            onClick={() => downloadFile(url, fileName)}
            className="bg-primary text-white px-6 py-3 rounded-lg text-base font-medium flex items-center gap-2"
          >
            <Download className="w-5 h-5" />
            Download
          </button>
          <button
            onClick={handleShare}
            className="border border-gray-300 text-text px-6 py-3 rounded-lg text-base font-medium flex items-center gap-2"
          >
            <Share2 className="w-5 h-5" />
            Share
          </button>
        </div>
      </div>
    </div>
  );
}
