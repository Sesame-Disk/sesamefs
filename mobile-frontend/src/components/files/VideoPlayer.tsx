import React from 'react';
import { X, Download } from 'lucide-react';
import { downloadFile } from '../../lib/share';
import { getVideoMimeType } from '../../lib/utils';

interface VideoPlayerProps {
  url: string;
  fileName: string;
  onClose: () => void;
}

export default function VideoPlayer({ url, fileName, onClose }: VideoPlayerProps) {
  const mimeType = getVideoMimeType(fileName);

  return (
    <div className="fixed inset-0 z-50 bg-black flex flex-col" data-testid="video-player">
      {/* Top bar */}
      <div className="flex items-center justify-between p-2">
        <button
          onClick={onClose}
          className="min-h-[44px] min-w-[44px] flex items-center justify-center text-white"
          aria-label="Close"
        >
          <X className="w-6 h-6" />
        </button>
        <p className="text-white text-sm truncate mx-2 flex-1 text-center">{fileName}</p>
        <button
          onClick={() => downloadFile(url, fileName)}
          className="min-h-[44px] min-w-[44px] flex items-center justify-center text-white"
          aria-label="Download"
        >
          <Download className="w-5 h-5" />
        </button>
      </div>

      {/* Video */}
      <div className="flex-1 flex items-center justify-center p-4">
        <video
          controls
          autoPlay
          className="max-w-full max-h-full"
          data-testid="video-element"
        >
          <source src={url} type={mimeType} />
          Your browser does not support video playback.
        </video>
      </div>
    </div>
  );
}
