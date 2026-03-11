import React, { useRef, useState } from 'react';
import { Upload, Camera, FolderUp, Plus, FolderPlus, FileText, X } from 'lucide-react';
import { uploadManager } from '../../lib/upload';

interface UploadButtonProps {
  repoId: string;
  path: string;
  onUploadStarted?: () => void;
  onNewFolder?: () => void;
  onNewFile?: () => void;
}

export default function UploadButton({ repoId, path, onUploadStarted, onNewFolder, onNewFile }: UploadButtonProps) {
  const [menuOpen, setMenuOpen] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const cameraInputRef = useRef<HTMLInputElement>(null);
  const folderInputRef = useRef<HTMLInputElement>(null);

  const handleFilePick = () => {
    fileInputRef.current?.click();
    setMenuOpen(false);
  };

  const handleCamera = () => {
    cameraInputRef.current?.click();
    setMenuOpen(false);
  };

  const handleFolderPick = () => {
    folderInputRef.current?.click();
    setMenuOpen(false);
  };

  const handleFilesSelected = (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files;
    if (!files || files.length === 0) return;
    uploadManager.addFiles(Array.from(files), repoId, path);
    onUploadStarted?.();
    e.target.value = '';
  };

  return (
    <>
      {/* FAB */}
      <button
        onClick={() => setMenuOpen(!menuOpen)}
        className="fixed bottom-20 right-4 w-14 h-14 bg-primary text-white rounded-full shadow-lg flex items-center justify-center z-40"
        aria-label={menuOpen ? 'Close menu' : 'Add new'}
        data-testid="upload-fab"
      >
        {menuOpen ? <X className="w-6 h-6" /> : <Plus className="w-6 h-6" />}
      </button>

      {/* Menu overlay */}
      {menuOpen && (
        <>
          <div
            className="fixed inset-0 bg-black/30 z-30"
            onClick={() => setMenuOpen(false)}
            data-testid="upload-overlay"
          />
          <div className="fixed bottom-36 right-4 bg-white rounded-xl shadow-xl z-40 w-56 py-2" data-testid="upload-menu">
            <button
              onClick={handleFilePick}
              className="flex items-center gap-3 px-4 py-3 w-full hover:bg-gray-50 min-h-[44px]"
              data-testid="upload-files-btn"
            >
              <Upload className="w-5 h-5 text-primary" />
              <span className="text-text">Upload Files</span>
            </button>
            <button
              onClick={handleCamera}
              className="flex items-center gap-3 px-4 py-3 w-full hover:bg-gray-50 min-h-[44px]"
              data-testid="upload-camera-btn"
            >
              <Camera className="w-5 h-5 text-primary" />
              <span className="text-text">Take Photo</span>
            </button>
            <button
              onClick={handleFolderPick}
              className="flex items-center gap-3 px-4 py-3 w-full hover:bg-gray-50 min-h-[44px]"
              data-testid="upload-folder-btn"
            >
              <FolderUp className="w-5 h-5 text-primary" />
              <span className="text-text">Upload Folder</span>
            </button>
            <div className="border-t border-gray-100 my-1" />
            <button
              onClick={() => { setMenuOpen(false); onNewFolder?.(); }}
              className="flex items-center gap-3 px-4 py-3 w-full hover:bg-gray-50 min-h-[44px]"
              data-testid="new-folder-btn"
            >
              <FolderPlus className="w-5 h-5 text-primary" />
              <span className="text-text">New Folder</span>
            </button>
            <button
              onClick={() => { setMenuOpen(false); onNewFile?.(); }}
              className="flex items-center gap-3 px-4 py-3 w-full hover:bg-gray-50 min-h-[44px]"
              data-testid="new-file-btn"
            >
              <FileText className="w-5 h-5 text-primary" />
              <span className="text-text">New File</span>
            </button>
          </div>
        </>
      )}

      {/* Hidden file inputs */}
      <input
        ref={fileInputRef}
        type="file"
        multiple
        className="hidden"
        onChange={handleFilesSelected}
        data-testid="file-input"
      />
      <input
        ref={cameraInputRef}
        type="file"
        accept="image/*"
        capture="environment"
        className="hidden"
        onChange={handleFilesSelected}
        data-testid="camera-input"
      />
      <input
        ref={folderInputRef}
        type="file"
        {...{ webkitdirectory: '', directory: '' } as any}
        multiple
        className="hidden"
        onChange={handleFilesSelected}
        data-testid="folder-input"
      />
    </>
  );
}
