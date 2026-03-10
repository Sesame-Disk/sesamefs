import React from 'react';
import { Users } from 'lucide-react';

interface GroupDetailProps {
  groupId?: string;
}

export default function GroupDetail({ groupId: _groupId }: GroupDetailProps) {
  return (
    <div className="flex flex-col items-center justify-center p-8 text-center">
      <Users className="w-12 h-12 text-gray-300 mb-4" />
      <h1 className="text-xl font-medium text-text">Group Detail</h1>
      <p className="text-gray-500 mt-2">Coming soon</p>
    </div>
  );
}
