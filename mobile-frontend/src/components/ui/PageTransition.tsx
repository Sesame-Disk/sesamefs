import React from 'react';
import { AnimatePresence, motion } from 'framer-motion';

interface PageTransitionProps {
  children: React.ReactNode;
  pageKey: string;
  direction?: 'forward' | 'back';
}

const variants = {
  enter: (direction: 'forward' | 'back') => ({
    x: direction === 'forward' ? '100%' : '-100%',
    opacity: 0,
  }),
  center: {
    x: 0,
    opacity: 1,
  },
  exit: (direction: 'forward' | 'back') => ({
    x: direction === 'forward' ? '-100%' : '100%',
    opacity: 0,
  }),
};

export default function PageTransition({
  children,
  pageKey,
  direction = 'forward',
}: PageTransitionProps) {
  return (
    <AnimatePresence mode="wait" custom={direction}>
      <motion.div
        key={pageKey}
        custom={direction}
        variants={variants}
        initial="enter"
        animate="center"
        exit="exit"
        transition={{ duration: 0.2, ease: 'easeInOut' }}
        data-testid="page-transition"
      >
        {children}
      </motion.div>
    </AnimatePresence>
  );
}
