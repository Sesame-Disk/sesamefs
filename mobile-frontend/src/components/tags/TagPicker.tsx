import React, { useState, useEffect, useCallback } from 'react';
import { Plus, Trash2, Check } from 'lucide-react';
import BottomSheet from '../ui/BottomSheet';
import TagBadge from './TagBadge';
import { listRepoTags, createRepoTag, deleteRepoTag, listFileTags, addFileTag, deleteFileTag } from '../../lib/api';
import type { RepoTag, FileTag } from '../../lib/models';

const TAG_COLORS = [
  '#F44336', '#E91E63', '#9C27B0', '#673AB7',
  '#3F51B5', '#2196F3', '#009688', '#4CAF50',
  '#FF9800', '#795548',
];

interface TagPickerProps {
  isOpen: boolean;
  onClose: () => void;
  repoId: string;
  filePath: string;
  onToast?: (msg: string) => void;
}

export default function TagPicker({ isOpen, onClose, repoId, filePath, onToast }: TagPickerProps) {
  const [repoTags, setRepoTags] = useState<RepoTag[]>([]);
  const [fileTags, setFileTags] = useState<FileTag[]>([]);
  const [loading, setLoading] = useState(false);
  const [creating, setCreating] = useState(false);
  const [newTagName, setNewTagName] = useState('');
  const [newTagColor, setNewTagColor] = useState(TAG_COLORS[0]);
  const [deleteConfirmId, setDeleteConfirmId] = useState<number | null>(null);

  const loadTags = useCallback(async () => {
    if (!repoId || !filePath) return;
    setLoading(true);
    try {
      const [rt, ft] = await Promise.all([
        listRepoTags(repoId),
        listFileTags(repoId, filePath),
      ]);
      setRepoTags(rt);
      setFileTags(ft);
    } catch {
      onToast?.('Failed to load tags');
    } finally {
      setLoading(false);
    }
  }, [repoId, filePath, onToast]);

  useEffect(() => {
    if (isOpen) {
      loadTags();
      setCreating(false);
      setNewTagName('');
      setNewTagColor(TAG_COLORS[0]);
      setDeleteConfirmId(null);
    }
  }, [isOpen, loadTags]);

  const isTagAssigned = (repoTagId: number): FileTag | undefined => {
    return fileTags.find(ft => ft.repo_tag_id === repoTagId);
  };

  const handleToggleTag = async (repoTag: RepoTag) => {
    const existing = isTagAssigned(repoTag.id);
    try {
      if (existing) {
        await deleteFileTag(repoId, existing.file_tag_id);
        onToast?.(`Removed tag "${repoTag.name}"`);
      } else {
        await addFileTag(repoId, filePath, repoTag.id);
        onToast?.(`Added tag "${repoTag.name}"`);
      }
      await loadTags();
    } catch {
      onToast?.('Failed to update tag');
    }
  };

  const handleCreateTag = async () => {
    if (!newTagName.trim()) return;
    try {
      const newTag = await createRepoTag(repoId, newTagName.trim(), newTagColor);
      await addFileTag(repoId, filePath, newTag.id);
      onToast?.(`Created tag "${newTag.name}"`);
      setCreating(false);
      setNewTagName('');
      setNewTagColor(TAG_COLORS[0]);
      await loadTags();
    } catch (err) {
      onToast?.(err instanceof Error ? err.message : 'Failed to create tag');
    }
  };

  const handleDeleteRepoTag = async (tagId: number) => {
    try {
      await deleteRepoTag(repoId, tagId);
      onToast?.('Tag deleted from all files');
      setDeleteConfirmId(null);
      await loadTags();
    } catch {
      onToast?.('Failed to delete tag');
    }
  };

  return (
    <BottomSheet isOpen={isOpen} onClose={onClose} title="Tags">
      <div className="flex flex-col gap-2 -mx-6 -mb-6">
        {loading && <p className="text-center text-gray-500 py-4">Loading...</p>}

        {!loading && !creating && (
          <>
            {repoTags.length === 0 && (
              <p className="text-center text-gray-400 py-4 text-sm">No tags yet. Create one below.</p>
            )}

            {repoTags.map(tag => {
              const assigned = isTagAssigned(tag.id);
              return (
                <div key={tag.id} className="flex items-center gap-3 px-4 py-3 min-h-[44px]">
                  <button
                    onClick={() => handleToggleTag(tag)}
                    className="flex items-center gap-3 flex-1 min-w-0"
                    data-testid={`tag-toggle-${tag.id}`}
                  >
                    <div
                      className={`w-5 h-5 rounded border-2 flex items-center justify-center flex-shrink-0 ${
                        assigned ? 'border-primary bg-primary' : 'border-gray-300'
                      }`}
                    >
                      {assigned && <Check className="w-3 h-3 text-white" />}
                    </div>
                    <TagBadge name={tag.name} color={tag.color} />
                  </button>

                  {deleteConfirmId === tag.id ? (
                    <div className="flex items-center gap-1 flex-shrink-0">
                      <button
                        onClick={() => handleDeleteRepoTag(tag.id)}
                        className="text-red-500 text-xs font-medium px-2 py-1 min-h-[32px]"
                      >
                        Confirm
                      </button>
                      <button
                        onClick={() => setDeleteConfirmId(null)}
                        className="text-gray-500 text-xs px-2 py-1 min-h-[32px]"
                      >
                        Cancel
                      </button>
                    </div>
                  ) : (
                    <button
                      onClick={() => setDeleteConfirmId(tag.id)}
                      className="min-h-[44px] min-w-[44px] flex items-center justify-center flex-shrink-0"
                      aria-label={`Delete tag ${tag.name}`}
                    >
                      <Trash2 className="w-4 h-4 text-gray-400" />
                    </button>
                  )}
                </div>
              );
            })}

            <button
              onClick={() => setCreating(true)}
              className="flex items-center gap-3 px-4 py-3 min-h-[44px] text-primary hover:bg-gray-50"
              data-testid="create-new-tag"
            >
              <Plus className="w-5 h-5" />
              <span className="text-base">Create New Tag</span>
            </button>
          </>
        )}

        {!loading && creating && (
          <div className="px-4 py-3 flex flex-col gap-3">
            <input
              type="text"
              value={newTagName}
              onChange={e => setNewTagName(e.target.value)}
              placeholder="Tag name"
              className="w-full border border-gray-300 rounded-lg px-3 py-2 text-base focus:outline-none focus:ring-2 focus:ring-primary"
              autoFocus
              data-testid="new-tag-name"
            />
            <div className="flex flex-wrap gap-2">
              {TAG_COLORS.map(color => (
                <button
                  key={color}
                  onClick={() => setNewTagColor(color)}
                  className={`w-8 h-8 rounded-full border-2 ${
                    newTagColor === color ? 'border-gray-800 scale-110' : 'border-transparent'
                  }`}
                  style={{ backgroundColor: color }}
                  aria-label={`Color ${color}`}
                  data-testid={`color-${color}`}
                />
              ))}
            </div>
            <div className="flex items-center gap-2 mt-1">
              <TagBadge name={newTagName || 'Preview'} color={newTagColor} />
            </div>
            <div className="flex gap-2">
              <button
                onClick={() => setCreating(false)}
                className="flex-1 py-2 text-gray-500 border border-gray-300 rounded-lg"
              >
                Cancel
              </button>
              <button
                onClick={handleCreateTag}
                disabled={!newTagName.trim()}
                className="flex-1 py-2 text-white bg-primary rounded-lg disabled:opacity-50"
                data-testid="save-new-tag"
              >
                Create
              </button>
            </div>
          </div>
        )}
      </div>
    </BottomSheet>
  );
}
