import React, { useState } from 'react';
import BottomSheet from '../ui/BottomSheet';
import ShareLinkTab from './ShareLinkTab';
import InternalShareTab from './InternalShareTab';

interface ShareSheetProps {
  isOpen: boolean;
  onClose: () => void;
  repoId: string;
  path: string;
  isDir: boolean;
  itemName: string;
  onToast: (msg: string) => void;
}

type TabKey = 'link' | 'internal';

export default function ShareSheet({ isOpen, onClose, repoId, path, isDir, itemName, onToast }: ShareSheetProps) {
  const [activeTab, setActiveTab] = useState<TabKey>('link');

  const fullPath = path === '/' ? `/${itemName}` : `${path}/${itemName}`;

  return (
    <BottomSheet isOpen={isOpen} onClose={onClose} title={`Share: ${itemName}`} fullScreen>
      <div className="flex flex-col h-full">
        {/* Tab bar */}
        <div className="flex border-b border-gray-200">
          <button
            onClick={() => setActiveTab('link')}
            className={`flex-1 py-3 text-sm font-medium min-h-[44px] ${
              activeTab === 'link'
                ? 'text-primary border-b-2 border-primary'
                : 'text-gray-500'
            }`}
            data-testid="tab-share-link"
          >
            Share Link
          </button>
          <button
            onClick={() => setActiveTab('internal')}
            className={`flex-1 py-3 text-sm font-medium min-h-[44px] ${
              activeTab === 'internal'
                ? 'text-primary border-b-2 border-primary'
                : 'text-gray-500'
            }`}
            data-testid="tab-internal-share"
          >
            Internal Share
          </button>
        </div>

        {/* Tab content */}
        <div className="flex-1 overflow-auto p-4">
          {activeTab === 'link' && (
            <ShareLinkTab
              repoId={repoId}
              path={fullPath}
              itemName={itemName}
              onToast={onToast}
            />
          )}
          {activeTab === 'internal' && (
            <InternalShareTab
              repoId={repoId}
              path={fullPath}
              onToast={onToast}
            />
          )}
        </div>
      </div>
    </BottomSheet>
  );
}
