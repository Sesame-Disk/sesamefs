import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import TagPicker from '../TagPicker';

vi.mock('../../../lib/api', () => ({
  listRepoTags: vi.fn(),
  createRepoTag: vi.fn(),
  deleteRepoTag: vi.fn(),
  listFileTags: vi.fn(),
  addFileTag: vi.fn(),
  deleteFileTag: vi.fn(),
}));

import { listRepoTags, createRepoTag, listFileTags, addFileTag, deleteFileTag } from '../../../lib/api';

const mockRepoTags = [
  { id: 1, repo_id: 'repo-1', name: 'Important', color: '#F44336', tag_id: 1 },
  { id: 2, repo_id: 'repo-1', name: 'Review', color: '#2196F3', tag_id: 2 },
];

const mockFileTags = [
  { id: 10, repo_tag_id: 1, name: 'Important', color: '#F44336', file_tag_id: 10 },
];

describe('TagPicker', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    (listRepoTags as ReturnType<typeof vi.fn>).mockResolvedValue(mockRepoTags);
    (listFileTags as ReturnType<typeof vi.fn>).mockResolvedValue(mockFileTags);
    (addFileTag as ReturnType<typeof vi.fn>).mockResolvedValue({ id: 11, repo_tag_id: 2, name: 'Review', color: '#2196F3', file_tag_id: 11 });
    (deleteFileTag as ReturnType<typeof vi.fn>).mockResolvedValue(undefined);
    (createRepoTag as ReturnType<typeof vi.fn>).mockResolvedValue({ id: 3, repo_id: 'repo-1', name: 'New Tag', color: '#4CAF50', tag_id: 3 });
  });

  it('loads and displays repo tags', async () => {
    render(
      <TagPicker isOpen={true} onClose={vi.fn()} repoId="repo-1" filePath="/test.txt" />
    );
    await waitFor(() => {
      expect(screen.getByText('Important')).toBeInTheDocument();
      expect(screen.getByText('Review')).toBeInTheDocument();
    });
  });

  it('shows checked state for assigned tags', async () => {
    render(
      <TagPicker isOpen={true} onClose={vi.fn()} repoId="repo-1" filePath="/test.txt" />
    );
    await waitFor(() => {
      expect(screen.getByTestId('tag-toggle-1')).toBeInTheDocument();
    });
  });

  it('toggles tag off when clicking assigned tag', async () => {
    render(
      <TagPicker isOpen={true} onClose={vi.fn()} repoId="repo-1" filePath="/test.txt" />
    );
    await waitFor(() => {
      expect(screen.getByTestId('tag-toggle-1')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('tag-toggle-1'));
    await waitFor(() => {
      expect(deleteFileTag).toHaveBeenCalledWith('repo-1', 10);
    });
  });

  it('toggles tag on when clicking unassigned tag', async () => {
    render(
      <TagPicker isOpen={true} onClose={vi.fn()} repoId="repo-1" filePath="/test.txt" />
    );
    await waitFor(() => {
      expect(screen.getByTestId('tag-toggle-2')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('tag-toggle-2'));
    await waitFor(() => {
      expect(addFileTag).toHaveBeenCalledWith('repo-1', '/test.txt', 2);
    });
  });

  it('shows create tag form when Create New Tag is clicked', async () => {
    render(
      <TagPicker isOpen={true} onClose={vi.fn()} repoId="repo-1" filePath="/test.txt" />
    );
    await waitFor(() => {
      expect(screen.getByTestId('create-new-tag')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('create-new-tag'));
    expect(screen.getByTestId('new-tag-name')).toBeInTheDocument();
  });

  it('creates a new tag and assigns it', async () => {
    render(
      <TagPicker isOpen={true} onClose={vi.fn()} repoId="repo-1" filePath="/test.txt" />
    );
    await waitFor(() => {
      expect(screen.getByTestId('create-new-tag')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('create-new-tag'));
    fireEvent.change(screen.getByTestId('new-tag-name'), { target: { value: 'New Tag' } });
    fireEvent.click(screen.getByTestId('save-new-tag'));
    await waitFor(() => {
      expect(createRepoTag).toHaveBeenCalledWith('repo-1', 'New Tag', '#F44336');
      expect(addFileTag).toHaveBeenCalled();
    });
  });
});
