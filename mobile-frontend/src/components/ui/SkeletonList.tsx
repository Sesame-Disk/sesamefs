import React from 'react';
import { motion, AnimatePresence } from 'framer-motion';

function ShimmerBlock({ className }: { className?: string }) {
  return (
    <div className={`relative overflow-hidden rounded bg-gray-200 ${className ?? ''}`}>
      <motion.div
        className="absolute inset-0 bg-gradient-to-r from-transparent via-white/60 to-transparent"
        animate={{ x: ['-100%', '100%'] }}
        transition={{ repeat: Infinity, duration: 1.5, ease: 'linear' }}
      />
    </div>
  );
}

function LibrarySkeletonCard() {
  return (
    <div className="flex items-center gap-3 p-4" data-testid="skeleton-library">
      <ShimmerBlock className="w-12 h-12 rounded-lg" />
      <div className="flex-1 space-y-2">
        <ShimmerBlock className="h-4 w-3/4" />
        <ShimmerBlock className="h-3 w-1/2" />
      </div>
    </div>
  );
}

function FileSkeletonCard() {
  return (
    <div className="flex items-center gap-3 p-4" data-testid="skeleton-file">
      <ShimmerBlock className="w-10 h-10 rounded" />
      <div className="flex-1 space-y-2">
        <ShimmerBlock className="h-4 w-2/3" />
        <ShimmerBlock className="h-3 w-1/3" />
      </div>
    </div>
  );
}

function ActivitySkeletonCard() {
  return (
    <div className="flex items-start gap-3 p-4" data-testid="skeleton-activity">
      <ShimmerBlock className="w-8 h-8 rounded-full" />
      <div className="flex-1 space-y-2">
        <ShimmerBlock className="h-4 w-full" />
        <ShimmerBlock className="h-3 w-2/3" />
        <ShimmerBlock className="h-3 w-1/4" />
      </div>
    </div>
  );
}

interface SkeletonListProps {
  variant?: 'library' | 'file' | 'activity';
  count?: number;
}

const variantComponents: Record<string, React.FC> = {
  library: LibrarySkeletonCard,
  file: FileSkeletonCard,
  activity: ActivitySkeletonCard,
};

interface ContentCrossfadeProps {
  isLoading: boolean;
  skeleton: React.ReactNode;
  children: React.ReactNode;
}

export function ContentCrossfade({ isLoading, skeleton, children }: ContentCrossfadeProps) {
  return (
    <AnimatePresence mode="wait">
      {isLoading ? (
        <motion.div
          key="skeleton"
          initial={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={{ duration: 0.2 }}
        >
          {skeleton}
        </motion.div>
      ) : (
        <motion.div
          key="content"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.2 }}
        >
          {children}
        </motion.div>
      )}
    </AnimatePresence>
  );
}

export { LibrarySkeletonCard as LibrarySkeleton };
export { FileSkeletonCard as FileSkeleton };
export { ActivitySkeletonCard as ActivitySkeleton };

export default function SkeletonList({ variant = 'library', count = 5 }: SkeletonListProps) {
  const Component = variantComponents[variant];
  return (
    <div data-testid="skeleton-list">
      {Array.from({ length: count }, (_, i) => (
        <Component key={i} />
      ))}
    </div>
  );
}
