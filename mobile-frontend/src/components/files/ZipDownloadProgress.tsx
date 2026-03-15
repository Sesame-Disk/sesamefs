import React, { useState, useEffect, useRef, useCallback } from 'react';
import { X, Download, AlertCircle, Loader2 } from 'lucide-react';
import { queryZipProgress, cancelZipTask } from '../../lib/api';
import { fileServerRoot } from '../../lib/config';
import { AnimatePresence, motion } from 'framer-motion';

interface ZipDownloadProgressProps {
  isOpen: boolean;
  zipToken: string | null;
  onClose: () => void;
  onError: (msg: string) => void;
}

export default function ZipDownloadProgress({
  isOpen,
  zipToken,
  onClose,
  onError,
}: ZipDownloadProgressProps) {
  const [progress, setProgress] = useState<string | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const cleanup = useCallback(() => {
    if (intervalRef.current) {
      clearInterval(intervalRef.current);
      intervalRef.current = null;
    }
  }, []);

  const pollProgress = useCallback(async () => {
    if (!zipToken) return;
    try {
      const data = await queryZipProgress(zipToken);
      if (data.failed === 1) {
        cleanup();
        let errorMsg: string;
        switch (data.failed_reason) {
          case 'size too large':
            errorMsg = 'Failed to download. The total size of the files exceeded the limit.';
            break;
          case 'internal error':
            errorMsg = 'Internal Server Error';
            break;
          default:
            errorMsg = 'Failed to create zip file';
        }
        setError(errorMsg);
        setLoading(false);
        return;
      }

      const pct = data.total === 0 ? '100%' : (data.zipped / data.total * 100).toFixed(1) + '%';
      setProgress(pct);
      setLoading(false);

      if (data.total === data.zipped) {
        cleanup();
        onClose();
        // Trigger browser download
        location.href = `${fileServerRoot()}zip/${zipToken}`;
      }
    } catch {
      cleanup();
      setError('Failed to check download progress');
      setLoading(false);
    }
  }, [zipToken, cleanup, onClose]);

  useEffect(() => {
    if (!isOpen || !zipToken) return;

    setLoading(true);
    setError('');
    setProgress(null);

    // Initial poll immediately
    pollProgress();
    // Then poll every 1 second
    intervalRef.current = setInterval(pollProgress, 1000);

    return cleanup;
  }, [isOpen, zipToken, pollProgress, cleanup]);

  const handleCancel = async () => {
    cleanup();
    if (zipToken && progress && progress !== '100%') {
      try {
        await cancelZipTask(zipToken);
      } catch {
        // ignore cancel errors
      }
    }
    onClose();
  };

  const handleClose = () => {
    if (progress && progress !== '100%') {
      handleCancel();
    } else {
      cleanup();
      onClose();
    }
  };

  return (
    <AnimatePresence>
      {isOpen && (
        <motion.div
          className="fixed inset-0 z-50 flex items-center justify-center"
          initial={{ backgroundColor: 'rgba(0,0,0,0)' }}
          animate={{ backgroundColor: 'rgba(0,0,0,0.4)' }}
          exit={{ backgroundColor: 'rgba(0,0,0,0)' }}
          transition={{ duration: 0.2 }}
          onClick={(e) => { if (e.target === e.currentTarget) handleClose(); }}
          data-testid="zip-download-backdrop"
        >
          <motion.div
            className="w-[90%] max-w-sm bg-white dark:bg-dark-surface rounded-2xl p-6 shadow-xl"
            role="dialog"
            aria-label="Download"
            initial={{ scale: 0.9, opacity: 0 }}
            animate={{ scale: 1, opacity: 1 }}
            exit={{ scale: 0.9, opacity: 0 }}
            transition={{ duration: 0.2 }}
          >
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-lg font-semibold text-text dark:text-dark-text flex items-center gap-2">
                <Download className="w-5 h-5" />
                Download
              </h3>
              <button
                onClick={handleClose}
                className="min-h-[44px] min-w-[44px] flex items-center justify-center text-gray-400"
                aria-label="Close"
              >
                <X className="w-5 h-5" />
              </button>
            </div>

            {loading && (
              <div className="flex flex-col items-center py-6" data-testid="zip-loading">
                <Loader2 className="w-8 h-8 text-primary animate-spin" />
                <p className="text-sm text-gray-500 mt-3">Preparing download...</p>
              </div>
            )}

            {error && (
              <div className="flex flex-col items-center py-6" data-testid="zip-error">
                <AlertCircle className="w-10 h-10 text-red-400" />
                <p className="text-sm text-red-500 mt-3 text-center">{error}</p>
              </div>
            )}

            {!loading && !error && progress && (
              <div className="py-4" data-testid="zip-progress">
                <div className="w-full bg-gray-200 dark:bg-gray-700 rounded-full h-2 mb-3">
                  <div
                    className="bg-primary h-2 rounded-full transition-all duration-300"
                    style={{ width: progress }}
                  />
                </div>
                <p className="text-sm text-gray-500 text-center">
                  Packaging... {progress}
                </p>
              </div>
            )}

            {!loading && !error && (
              <button
                onClick={handleCancel}
                className="w-full mt-2 py-2.5 text-sm text-red-500 border border-red-200 rounded-lg min-h-[44px]"
                data-testid="zip-cancel-btn"
              >
                Cancel
              </button>
            )}
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>
  );
}
