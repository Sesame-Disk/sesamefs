import React from 'react';
import { bytesToSize } from '../../lib/models';

interface StorageUsageBarProps {
  used: number;
  total: number;
}

export default function StorageUsageBar({ used, total }: StorageUsageBarProps) {
  const percentage = total > 0 ? Math.min((used / total) * 100, 100) : 0;
  const isHigh = percentage > 80;

  return (
    <div data-testid="storage-usage-bar">
      <div className="flex justify-between text-xs mb-1">
        <span className="text-gray-500 dark:text-gray-400">
          {bytesToSize(used)} of {bytesToSize(total)} used
        </span>
        <span className={`font-medium ${isHigh ? 'text-red-500' : 'text-gray-500 dark:text-gray-400'}`}>
          {percentage.toFixed(0)}%
        </span>
      </div>
      <div className="w-full h-2 bg-gray-200 dark:bg-dark-border rounded-full overflow-hidden">
        <div
          className={`h-full rounded-full transition-all ${isHigh ? 'bg-red-500' : 'bg-primary'}`}
          style={{ width: `${percentage}%` }}
          data-testid="storage-bar-fill"
        />
      </div>
    </div>
  );
}
