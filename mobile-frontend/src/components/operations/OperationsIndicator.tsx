import React, { useState, useEffect } from 'react';
import { ArrowUpDown } from 'lucide-react';
import { uploadManager } from '../../lib/upload';
import { shareQueueManager } from '../../lib/shareQueue';
import OperationsDashboard from './OperationsDashboard';

/**
 * Floating action button that shows active operations count.
 * Renders globally - shows only when there are active uploads or share operations.
 */
export default function OperationsIndicator() {
  const [dashboardOpen, setDashboardOpen] = useState(false);
  const [activeCount, setActiveCount] = useState(0);

  useEffect(() => {
    const update = async () => {
      const uploadStats = uploadManager.getStats();
      const shareStats = await shareQueueManager.getStats();
      setActiveCount(
        uploadStats.uploading + uploadStats.queued + uploadStats.paused +
        shareStats.queued + shareStats.processing
      );
    };

    update();

    const unsubUpload = uploadManager.subscribe(() => update());
    const unsubShare = shareQueueManager.subscribe(() => update());

    // Poll every 2s as fallback (events might not fire for share queue init)
    const interval = setInterval(update, 2000);

    return () => {
      unsubUpload();
      unsubShare();
      clearInterval(interval);
    };
  }, []);

  if (activeCount === 0) return null;

  return (
    <>
      <button
        onClick={() => setDashboardOpen(true)}
        className="fixed bottom-20 right-4 z-30 w-12 h-12 bg-primary text-white rounded-full shadow-lg flex items-center justify-center animate-pulse-slow"
        aria-label={`${activeCount} active operations`}
        data-testid="operations-indicator"
      >
        <ArrowUpDown className="w-5 h-5" />
        <span className="absolute -top-1 -right-1 bg-red-500 text-white text-[10px] font-bold rounded-full w-5 h-5 flex items-center justify-center">
          {activeCount > 99 ? '99+' : activeCount}
        </span>
      </button>

      <OperationsDashboard
        isOpen={dashboardOpen}
        onClose={() => setDashboardOpen(false)}
      />
    </>
  );
}
