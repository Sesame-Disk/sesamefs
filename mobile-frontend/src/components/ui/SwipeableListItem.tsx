import React, { useRef, useState } from 'react';
import { motion } from 'framer-motion';
import type { PanInfo } from 'framer-motion';

interface SwipeAction {
  icon: React.ReactNode;
  label: string;
  color: string;
  onClick: () => void;
}

interface SwipeableListItemProps {
  children: React.ReactNode;
  leftActions?: SwipeAction[];
  rightActions?: SwipeAction[];
  threshold?: number;
}

export default function SwipeableListItem({
  children,
  leftActions = [],
  rightActions = [],
  threshold = 80,
}: SwipeableListItemProps) {
  const [offset, setOffset] = useState(0);
  const constraintRef = useRef<HTMLDivElement>(null);

  const handleDragEnd = (_: unknown, info: PanInfo) => {
    if (info.offset.x > threshold && leftActions.length > 0) {
      setOffset(threshold);
    } else if (info.offset.x < -threshold && rightActions.length > 0) {
      setOffset(-threshold);
    } else {
      setOffset(0);
    }
  };

  const handleActionClick = (action: SwipeAction) => {
    action.onClick();
    setOffset(0);
  };

  return (
    <div className="relative overflow-hidden" data-testid="swipeable-list-item" ref={constraintRef}>
      {leftActions.length > 0 && (
        <div className="absolute inset-y-0 left-0 flex items-center" data-testid="left-actions">
          {leftActions.map((action) => (
            <button
              key={action.label}
              className="h-full px-4 flex items-center gap-1 text-white text-xs font-medium min-h-[44px]"
              style={{ backgroundColor: action.color }}
              onClick={() => handleActionClick(action)}
            >
              {action.icon}
              <span>{action.label}</span>
            </button>
          ))}
        </div>
      )}

      {rightActions.length > 0 && (
        <div className="absolute inset-y-0 right-0 flex items-center" data-testid="right-actions">
          {rightActions.map((action) => (
            <button
              key={action.label}
              className="h-full px-4 flex items-center gap-1 text-white text-xs font-medium min-h-[44px]"
              style={{ backgroundColor: action.color }}
              onClick={() => handleActionClick(action)}
            >
              {action.icon}
              <span>{action.label}</span>
            </button>
          ))}
        </div>
      )}

      <motion.div
        className="relative z-10 bg-white dark:bg-dark-surface"
        drag="x"
        dragConstraints={constraintRef}
        dragElastic={0.1}
        onDragEnd={handleDragEnd}
        animate={{ x: offset }}
        transition={{ type: 'spring', damping: 25, stiffness: 300 }}
        data-testid="swipeable-content"
      >
        {children}
      </motion.div>
    </div>
  );
}
