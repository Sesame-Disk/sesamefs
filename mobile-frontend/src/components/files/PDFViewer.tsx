import React from 'react';
import { X, Download, ExternalLink } from 'lucide-react';
import { downloadFile } from '../../lib/share';

interface PDFViewerProps {
  url: string;
  fileName: string;
  onClose: () => void;
}

export default function PDFViewer({ url, fileName, onClose }: PDFViewerProps) {
  return (
    <div className="fixed inset-0 z-50 bg-white flex flex-col" data-testid="pdf-viewer">
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
        <div className="flex gap-1">
          <button
            onClick={() => window.open(url, '_blank')}
            className="min-h-[44px] min-w-[44px] flex items-center justify-center text-gray-600"
            aria-label="Open in new tab"
          >
            <ExternalLink className="w-5 h-5" />
          </button>
          <button
            onClick={() => downloadFile(url, fileName)}
            className="min-h-[44px] min-w-[44px] flex items-center justify-center text-gray-600"
            aria-label="Download"
          >
            <Download className="w-5 h-5" />
          </button>
        </div>
      </div>

      {/* PDF embed with fallback */}
      <div className="flex-1 flex flex-col">
        <iframe
          src={url}
          className="flex-1 w-full border-0"
          title={fileName}
          data-testid="pdf-iframe"
        />
        {/* Fallback for browsers that don't support iframe PDF */}
        <noscript>
          <div className="flex flex-col items-center justify-center p-8 text-center">
            <p className="text-gray-500 mb-4">PDF preview is not available.</p>
            <button
              onClick={() => downloadFile(url, fileName)}
              className="bg-primary text-white px-6 py-3 rounded-lg text-base font-medium"
            >
              Download PDF
            </button>
          </div>
        </noscript>
      </div>
    </div>
  );
}
