import React, { useState, useRef, useCallback } from 'react';
import { X, Download, Share2, ChevronLeft, ChevronRight } from 'lucide-react';
import { shareFile, downloadFile } from '../../lib/share';

interface ImageViewerProps {
  url: string;
  fileName: string;
  onClose: () => void;
  siblingImages?: { name: string; url: string }[];
  onNavigate?: (index: number) => void;
  currentIndex?: number;
  onToast?: (msg: string) => void;
}

export default function ImageViewer({
  url,
  fileName,
  onClose,
  siblingImages = [],
  onNavigate,
  currentIndex = 0,
  onToast,
}: ImageViewerProps) {
  const [scale, setScale] = useState(1);
  const [translate, setTranslate] = useState({ x: 0, y: 0 });
  const lastDistance = useRef<number | null>(null);
  const lastCenter = useRef<{ x: number; y: number } | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  const resetTransform = useCallback(() => {
    setScale(1);
    setTranslate({ x: 0, y: 0 });
  }, []);

  const handleTouchMove = useCallback((e: React.TouchEvent) => {
    if (e.touches.length === 2) {
      e.preventDefault();
      const dx = e.touches[0].clientX - e.touches[1].clientX;
      const dy = e.touches[0].clientY - e.touches[1].clientY;
      const distance = Math.sqrt(dx * dx + dy * dy);

      if (lastDistance.current !== null) {
        const delta = distance / lastDistance.current;
        setScale(s => Math.min(Math.max(s * delta, 0.5), 5));
      }
      lastDistance.current = distance;

      const cx = (e.touches[0].clientX + e.touches[1].clientX) / 2;
      const cy = (e.touches[0].clientY + e.touches[1].clientY) / 2;
      if (lastCenter.current) {
        setTranslate(t => ({
          x: t.x + cx - lastCenter.current!.x,
          y: t.y + cy - lastCenter.current!.y,
        }));
      }
      lastCenter.current = { x: cx, y: cy };
    }
  }, []);

  const handleTouchEnd = useCallback(() => {
    lastDistance.current = null;
    lastCenter.current = null;
  }, []);

  const handleDoubleClick = useCallback(() => {
    if (scale !== 1) {
      resetTransform();
    } else {
      setScale(2);
    }
  }, [scale, resetTransform]);

  const handlePrev = () => {
    if (onNavigate && currentIndex > 0) {
      resetTransform();
      onNavigate(currentIndex - 1);
    }
  };

  const handleNext = () => {
    if (onNavigate && currentIndex < siblingImages.length - 1) {
      resetTransform();
      onNavigate(currentIndex + 1);
    }
  };

  const handleDownload = () => downloadFile(url, fileName);

  const handleShare = async () => {
    const shared = await shareFile(fileName, url);
    if (shared && onToast) onToast('Link copied');
  };

  return (
    <div className="fixed inset-0 z-50 bg-black flex flex-col" data-testid="image-viewer">
      {/* Top bar */}
      <div className="flex items-center justify-between p-2 z-10">
        <button
          onClick={onClose}
          className="min-h-[44px] min-w-[44px] flex items-center justify-center text-white"
          aria-label="Close"
          data-testid="close-button"
        >
          <X className="w-6 h-6" />
        </button>
        <p className="text-white text-sm truncate mx-2 flex-1 text-center">{fileName}</p>
        <div className="flex gap-1">
          <button
            onClick={handleShare}
            className="min-h-[44px] min-w-[44px] flex items-center justify-center text-white"
            aria-label="Share"
          >
            <Share2 className="w-5 h-5" />
          </button>
          <button
            onClick={handleDownload}
            className="min-h-[44px] min-w-[44px] flex items-center justify-center text-white"
            aria-label="Download"
          >
            <Download className="w-5 h-5" />
          </button>
        </div>
      </div>

      {/* Image area */}
      <div
        ref={containerRef}
        className="flex-1 flex items-center justify-center overflow-hidden relative"
        onTouchMove={handleTouchMove}
        onTouchEnd={handleTouchEnd}
        onDoubleClick={handleDoubleClick}
      >
        <img
          src={url}
          alt={fileName}
          className="max-w-full max-h-full object-contain select-none"
          style={{
            transform: `translate(${translate.x}px, ${translate.y}px) scale(${scale})`,
            transition: lastDistance.current !== null ? 'none' : 'transform 0.2s',
          }}
          draggable={false}
        />

        {/* Nav arrows */}
        {siblingImages.length > 1 && currentIndex > 0 && (
          <button
            onClick={handlePrev}
            className="absolute left-2 top-1/2 -translate-y-1/2 min-h-[44px] min-w-[44px] flex items-center justify-center text-white/70 hover:text-white"
            aria-label="Previous image"
          >
            <ChevronLeft className="w-8 h-8" />
          </button>
        )}
        {siblingImages.length > 1 && currentIndex < siblingImages.length - 1 && (
          <button
            onClick={handleNext}
            className="absolute right-2 top-1/2 -translate-y-1/2 min-h-[44px] min-w-[44px] flex items-center justify-center text-white/70 hover:text-white"
            aria-label="Next image"
          >
            <ChevronRight className="w-8 h-8" />
          </button>
        )}
      </div>

      {/* Image counter */}
      {siblingImages.length > 1 && (
        <div className="text-center text-white/60 text-xs pb-4">
          {currentIndex + 1} / {siblingImages.length}
        </div>
      )}
    </div>
  );
}
