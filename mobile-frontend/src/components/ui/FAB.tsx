import React, { useState } from 'react';
import { AnimatePresence, motion } from 'framer-motion';
import { Plus } from 'lucide-react';

interface FABAction {
  icon: React.ReactNode;
  label: string;
  onClick: () => void;
}

interface FABProps {
  actions: FABAction[];
}

export default function FAB({ actions }: FABProps) {
  const [isExpanded, setIsExpanded] = useState(false);

  const toggle = () => setIsExpanded((prev) => !prev);

  return (
    <div className="fixed bottom-20 right-4 z-40 flex flex-col items-end gap-2">
      <AnimatePresence>
        {isExpanded && (
          <>
            <motion.div
              className="fixed inset-0 bg-black/20 z-30"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              transition={{ duration: 0.2 }}
              onClick={toggle}
              data-testid="fab-backdrop"
            />
            {actions.map((action, index) => (
              <motion.button
                key={action.label}
                className="relative z-40 flex items-center gap-2 bg-white rounded-full shadow-lg px-4 py-3 min-h-[44px]"
                initial={{ opacity: 0, scale: 0.3, y: 20 }}
                animate={{ opacity: 1, scale: 1, y: 0 }}
                exit={{ opacity: 0, scale: 0.3, y: 20 }}
                transition={{
                  delay: (actions.length - 1 - index) * 0.05,
                  type: 'spring',
                  damping: 20,
                  stiffness: 300,
                }}
                onClick={() => {
                  action.onClick();
                  setIsExpanded(false);
                }}
                data-testid={`fab-action-${index}`}
              >
                {action.icon}
                <span className="text-sm font-medium">{action.label}</span>
              </motion.button>
            ))}
          </>
        )}
      </AnimatePresence>

      <motion.button
        className="relative z-40 w-14 h-14 rounded-full bg-primary text-white shadow-lg flex items-center justify-center"
        onClick={toggle}
        animate={{ rotate: isExpanded ? 45 : 0 }}
        transition={{ duration: 0.2 }}
        aria-label={isExpanded ? 'Close menu' : 'Open menu'}
        data-testid="fab-toggle"
      >
        <Plus size={24} />
      </motion.button>
    </div>
  );
}
