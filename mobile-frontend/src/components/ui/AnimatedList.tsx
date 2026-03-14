import React from 'react';
import { AnimatePresence, motion } from 'framer-motion';

interface AnimatedListItemProps {
  children: React.ReactNode;
  itemKey: string;
  index?: number;
}

export function AnimatedListItem({ children, itemKey, index = 0 }: AnimatedListItemProps) {
  return (
    <motion.div
      key={itemKey}
      layout
      initial={{ opacity: 0, y: -20 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, x: -200 }}
      transition={{
        layout: { type: 'spring', damping: 25, stiffness: 300 },
        opacity: { duration: 0.2, delay: index * 0.05 },
        y: { duration: 0.2, delay: index * 0.05 },
      }}
      data-testid="animated-list-item"
    >
      {children}
    </motion.div>
  );
}

interface AnimatedListProps {
  children: React.ReactNode;
}

export default function AnimatedList({ children }: AnimatedListProps) {
  return (
    <AnimatePresence mode="popLayout">
      {children}
    </AnimatePresence>
  );
}
