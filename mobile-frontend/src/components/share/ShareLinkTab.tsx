import React, { useState, useEffect } from 'react';
import { Copy, Trash2, ExternalLink, ChevronDown, ChevronUp } from 'lucide-react';
import { QRCodeSVG } from 'qrcode.react';
import {
  listShareLinks,
  createShareLink,
  deleteShareLink,
} from '../../lib/api';
import type { ShareLink, ShareLinkOptions } from '../../lib/api';
import { shareFile } from '../../lib/share';

interface ShareLinkTabProps {
  repoId: string;
  path: string;
  itemName: string;
  onToast: (msg: string) => void;
}

export default function ShareLinkTab({ repoId, path, itemName, onToast }: ShareLinkTabProps) {
  const [link, setLink] = useState<ShareLink | null>(null);
  const [loading, setLoading] = useState(true);
  const [generating, setGenerating] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [showOptions, setShowOptions] = useState(false);

  // Options state
  const [usePassword, setUsePassword] = useState(false);
  const [password, setPassword] = useState('');
  const [useExpiry, setUseExpiry] = useState(false);
  const [expiryDate, setExpiryDate] = useState('');
  const [canEdit, setCanEdit] = useState(false);
  const [canDownload, setCanDownload] = useState(true);

  useEffect(() => {
    loadLink();
  }, [repoId, path]);

  const loadLink = async () => {
    setLoading(true);
    try {
      const links = await listShareLinks(repoId, path);
      setLink(links.length > 0 ? links[0] : null);
    } catch {
      // No existing link
      setLink(null);
    } finally {
      setLoading(false);
    }
  };

  const handleGenerate = async () => {
    setGenerating(true);
    try {
      const options: ShareLinkOptions = {};
      if (usePassword && password) options.password = password;
      if (useExpiry && expiryDate) {
        const now = new Date();
        const expiry = new Date(expiryDate);
        const diffDays = Math.ceil((expiry.getTime() - now.getTime()) / (1000 * 60 * 60 * 24));
        if (diffDays > 0) options.expire_days = diffDays;
      }
      options.permissions = { can_edit: canEdit, can_download: canDownload };

      const newLink = await createShareLink(repoId, path, options);
      setLink(newLink);
      onToast('Share link created');
    } catch (err) {
      onToast(err instanceof Error ? err.message : 'Failed to create link');
    } finally {
      setGenerating(false);
    }
  };

  const handleCopy = async () => {
    if (!link) return;
    try {
      await navigator.clipboard.writeText(link.link);
      onToast('Link copied to clipboard');
    } catch {
      onToast('Failed to copy link');
    }
  };

  const handleShare = async () => {
    if (!link) return;
    await shareFile(itemName, link.link);
  };

  const handleDelete = async () => {
    if (!link) return;
    try {
      await deleteShareLink(link.token);
      setLink(null);
      setConfirmDelete(false);
      onToast('Share link deleted');
    } catch {
      onToast('Failed to delete link');
    }
  };

  if (loading) {
    return <p className="text-center text-gray-400 py-8">Loading...</p>;
  }

  // Show existing link
  if (link) {
    return (
      <div className="space-y-4">
        {/* Link display */}
        <div className="flex items-center gap-2 bg-gray-50 p-3 rounded-lg">
          <p className="flex-1 text-sm text-text truncate" data-testid="share-link-url">{link.link}</p>
          <button
            onClick={handleCopy}
            className="min-h-[44px] min-w-[44px] flex items-center justify-center text-primary"
            aria-label="Copy link"
          >
            <Copy className="w-5 h-5" />
          </button>
        </div>

        {/* QR Code */}
        <div className="flex justify-center py-2" data-testid="qr-code">
          <QRCodeSVG value={link.link} size={160} />
        </div>

        {/* Link info */}
        <div className="text-xs text-gray-400 space-y-1">
          <p>Views: {link.view_cnt}</p>
          {link.expire_date && <p>Expires: {new Date(link.expire_date).toLocaleDateString()}</p>}
          {link.permissions && (
            <p>
              Permissions: {link.permissions.can_edit ? 'Edit' : 'View only'}
              {link.permissions.can_download ? ', Download' : ''}
            </p>
          )}
        </div>

        {/* Actions */}
        <div className="flex gap-2">
          <button
            onClick={handleShare}
            className="flex-1 flex items-center justify-center gap-2 bg-primary-button text-white py-2 rounded-lg min-h-[44px]"
          >
            <ExternalLink className="w-4 h-4" />
            Share
          </button>
          <button
            onClick={handleCopy}
            className="flex-1 flex items-center justify-center gap-2 border border-gray-200 py-2 rounded-lg min-h-[44px] text-text"
          >
            <Copy className="w-4 h-4" />
            Copy
          </button>
        </div>

        {/* Delete */}
        {confirmDelete ? (
          <div className="flex items-center gap-2 p-3 bg-red-50 rounded-lg">
            <p className="flex-1 text-sm text-red-600">Delete this share link?</p>
            <button
              onClick={handleDelete}
              className="px-3 py-1 bg-red-500 text-white text-sm rounded min-h-[36px]"
            >
              Delete
            </button>
            <button
              onClick={() => setConfirmDelete(false)}
              className="px-3 py-1 border border-gray-200 text-sm rounded min-h-[36px]"
            >
              Cancel
            </button>
          </div>
        ) : (
          <button
            onClick={() => setConfirmDelete(true)}
            className="flex items-center justify-center gap-2 w-full text-red-500 py-2 min-h-[44px]"
          >
            <Trash2 className="w-4 h-4" />
            Delete Link
          </button>
        )}
      </div>
    );
  }

  // No link - show generate form
  return (
    <div className="space-y-4">
      <p className="text-gray-500 text-sm">No share link exists for this item. Generate one to share externally.</p>

      {/* Options toggle */}
      <button
        onClick={() => setShowOptions(!showOptions)}
        className="flex items-center gap-2 text-sm text-primary min-h-[36px]"
      >
        {showOptions ? <ChevronUp className="w-4 h-4" /> : <ChevronDown className="w-4 h-4" />}
        Link Options
      </button>

      {showOptions && (
        <div className="space-y-3 p-3 bg-gray-50 rounded-lg">
          {/* Password */}
          <div className="flex items-center gap-2">
            <input
              type="checkbox"
              id="use-password"
              checked={usePassword}
              onChange={e => setUsePassword(e.target.checked)}
              className="w-4 h-4"
            />
            <label htmlFor="use-password" className="text-sm text-text">Password protection</label>
          </div>
          {usePassword && (
            <input
              type="password"
              value={password}
              onChange={e => setPassword(e.target.value)}
              placeholder="Enter password"
              className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm"
              aria-label="Share link password"
            />
          )}

          {/* Expiration */}
          <div className="flex items-center gap-2">
            <input
              type="checkbox"
              id="use-expiry"
              checked={useExpiry}
              onChange={e => setUseExpiry(e.target.checked)}
              className="w-4 h-4"
            />
            <label htmlFor="use-expiry" className="text-sm text-text">Expiration date</label>
          </div>
          {useExpiry && (
            <input
              type="date"
              value={expiryDate}
              onChange={e => setExpiryDate(e.target.value)}
              className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm"
              aria-label="Expiration date"
            />
          )}

          {/* Permissions */}
          <div className="space-y-2">
            <p className="text-sm text-text font-medium">Permissions</p>
            <div className="flex items-center gap-2">
              <input
                type="checkbox"
                id="can-edit"
                checked={canEdit}
                onChange={e => setCanEdit(e.target.checked)}
                className="w-4 h-4"
              />
              <label htmlFor="can-edit" className="text-sm text-text">Can edit</label>
            </div>
            <div className="flex items-center gap-2">
              <input
                type="checkbox"
                id="can-download"
                checked={canDownload}
                onChange={e => setCanDownload(e.target.checked)}
                className="w-4 h-4"
              />
              <label htmlFor="can-download" className="text-sm text-text">Can download</label>
            </div>
          </div>
        </div>
      )}

      {/* Generate button */}
      <button
        onClick={handleGenerate}
        disabled={generating}
        className="w-full bg-primary-button text-white py-3 rounded-lg min-h-[44px] disabled:opacity-50"
        data-testid="generate-link-btn"
      >
        {generating ? 'Generating...' : 'Generate Link'}
      </button>
    </div>
  );
}
