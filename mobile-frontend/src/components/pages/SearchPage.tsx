import React from 'react';
import { Search } from 'lucide-react';

export default function SearchPage() {
  return (
    <div className="flex flex-col items-center justify-center p-8 text-center">
      <Search className="w-12 h-12 text-gray-300 mb-4" />
      <h1 className="text-xl font-medium text-text">Search</h1>
      <p className="text-gray-500 mt-2">Coming soon</p>
    </div>
  );
}
