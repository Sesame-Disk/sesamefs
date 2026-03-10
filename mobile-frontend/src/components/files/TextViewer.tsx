import React, { useState, useEffect } from 'react';
import { X, Download, Copy } from 'lucide-react';
import { downloadFile } from '../../lib/share';
import { getFileExtension } from '../../lib/utils';

interface TextViewerProps {
  url: string;
  fileName: string;
  onClose: () => void;
  onToast?: (msg: string) => void;
}

export default function TextViewer({ url, fileName, onClose, onToast }: TextViewerProps) {
  const [content, setContent] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const ext = getFileExtension(fileName);
  const isMarkdown = ext === 'md';

  useEffect(() => {
    fetch(url)
      .then(res => {
        if (!res.ok) throw new Error('Failed to load file');
        return res.text();
      })
      .then(text => {
        setContent(text);
        setLoading(false);
      })
      .catch(err => {
        setError(err.message);
        setLoading(false);
      });
  }, [url]);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(content);
      onToast?.('Copied to clipboard');
    } catch {
      onToast?.('Failed to copy');
    }
  };

  return (
    <div className="fixed inset-0 z-50 bg-white flex flex-col" data-testid="text-viewer">
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
            onClick={handleCopy}
            className="min-h-[44px] min-w-[44px] flex items-center justify-center text-gray-600"
            aria-label="Copy"
          >
            <Copy className="w-5 h-5" />
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

      {/* Content */}
      <div className="flex-1 overflow-auto p-4">
        {loading && <p className="text-center text-gray-500 py-8">Loading...</p>}
        {error && <p className="text-center text-red-500 py-4">{error}</p>}
        {!loading && !error && (
          isMarkdown ? (
            <div className="prose prose-sm max-w-none" data-testid="markdown-content">
              <pre className="whitespace-pre-wrap font-sans text-sm leading-relaxed">{content}</pre>
            </div>
          ) : (
            <pre className="whitespace-pre-wrap font-mono text-sm leading-relaxed text-text" data-testid="text-content">
              {content}
            </pre>
          )
        )}
      </div>
    </div>
  );
}
