import React, { useEffect } from 'react';
import { motion, useAnimation, PanInfo } from 'framer-motion';

interface ToastProps {
  message: string;
  type?: 'success' | 'error' | 'info';
  visible: boolean;
  onDismiss: () => void;
  duration?: number;
}

const typeStyles: Record<string, string> = {
  success: 'bg-green-600',
  error: 'bg-red-600',
  info: 'bg-gray-800',
};

export default function Toast({
  message,
  type = 'info',
  visible,
  onDismiss,
  duration = 3000,
}: ToastProps) {
  const controls = useAnimation();

  useEffect(() => {
    if (visible) {
      controls.start({ y: 0, opacity: 1 });
      const timer = setTimeout(() => {
        controls.start({ y: 100, opacity: 0 }).then(onDismiss);
      }, duration);
      return () => clearTimeout(timer);
    } else {
      controls.start({ y: 100, opacity: 0 });
    }
  }, [visible, duration, controls, onDismiss]);

  const handleDragEnd = (_: unknown, info: PanInfo) => {
    if (info.offset.y > 50 || info.velocity.y > 200) {
      controls.start({ y: 200, opacity: 0 }).then(onDismiss);
    } else {
      controls.start({ y: 0, opacity: 1 });
    }
  };

  if (!visible) return null;

  return (
    <motion.div
      className={`fixed bottom-24 left-4 right-4 z-50 mx-auto max-w-lg rounded-lg px-4 py-3 text-white shadow-lg ${typeStyles[type]}`}
      initial={{ y: 100, opacity: 0 }}
      animate={controls}
      drag="y"
      dragConstraints={{ top: 0 }}
      dragElastic={0.3}
      onDragEnd={handleDragEnd}
      data-testid="toast"
      role="alert"
    >
      <p className="text-sm font-medium text-center">{message}</p>
    </motion.div>
  );
}
