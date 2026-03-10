import React from 'react';
import { Activity } from 'lucide-react';

export default function ActivityFeed() {
  return (
    <div className="flex flex-col items-center justify-center p-8 text-center">
      <Activity className="w-12 h-12 text-gray-300 mb-4" />
      <h1 className="text-xl font-medium text-text">Activity Feed</h1>
      <p className="text-gray-500 mt-2">Coming soon</p>
    </div>
  );
}
