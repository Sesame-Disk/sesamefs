import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import FolderPicker from '../FolderPicker';
import type { Dirent } from '../../../lib/models';

vi.mock('../../../lib/api', () => ({
  listRepos: vi.fn(),
  listDir: vi.fn(),
  moveFile: vi.fn(),
  copyFile: vi.fn(),
  moveDir: vi.fn(),
  copyDir: vi.fn(),
}));

import { listRepos, listDir, moveFile } from '../../../lib/api';

const mockRepos = [
  { repo_id: 'repo-1', repo_name: 'My Library', size: 0, permission: 'rw', owner_email: 'user@test.com', owner_name: 'User', encrypted: false, last_modified: '' },
  { repo_id: 'repo-2', repo_name: 'Shared Docs', size: 0, permission: 'r', owner_email: 'other@test.com', owner_name: 'Other', encrypted: false, last_modified: '' },
];

const mockDirs: Dirent[] = [
  { id: 'dir-1', type: 'dir', name: 'Documents', size: 0, mtime: 1700000000, permission: 'rw' },
  { id: 'dir-2', type: 'dir', name: 'Photos', size: 0, mtime: 1700000000, permission: 'rw' },
];

const mockItem: Dirent = {
  id: 'file-1',
  type: 'file',
  name: 'report.pdf',
  size: 2048,
  mtime: 1700000000,
  permission: 'rw',
};

const defaultProps = {
  isOpen: true,
  onClose: vi.fn(),
  items: [mockItem],
  srcRepoId: 'repo-1',
  srcPath: '/',
  mode: 'move' as const,
  onSuccess: vi.fn(),
};

describe('FolderPicker', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    (listRepos as ReturnType<typeof vi.fn>).mockResolvedValue(mockRepos);
    (listDir as ReturnType<typeof vi.fn>).mockResolvedValue(mockDirs);
  });

  it('renders library list', async () => {
    render(<FolderPicker {...defaultProps} />);
    await waitFor(() => {
      expect(screen.getByText('My Library')).toBeInTheDocument();
      expect(screen.getByText('Shared Docs')).toBeInTheDocument();
    });
  });

  it('navigates into a library and shows folders', async () => {
    render(<FolderPicker {...defaultProps} />);
    await waitFor(() => {
      expect(screen.getByText('My Library')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText('My Library'));
    await waitFor(() => {
      expect(screen.getByText('Documents')).toBeInTheDocument();
      expect(screen.getByText('Photos')).toBeInTheDocument();
    });
  });

  it('shows move button when in repo', async () => {
    render(<FolderPicker {...defaultProps} />);
    await waitFor(() => {
      expect(screen.getByText('My Library')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText('My Library'));
    await waitFor(() => {
      expect(screen.getByText('Move here')).toBeInTheDocument();
    });
  });

  it('shows copy button in copy mode', async () => {
    render(<FolderPicker {...defaultProps} mode="copy" />);
    await waitFor(() => {
      expect(screen.getByText('My Library')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText('My Library'));
    await waitFor(() => {
      expect(screen.getByText('Copy here')).toBeInTheDocument();
    });
  });

  it('executes move when button is clicked', async () => {
    (moveFile as ReturnType<typeof vi.fn>).mockResolvedValue(undefined);
    render(<FolderPicker {...defaultProps} />);
    await waitFor(() => {
      expect(screen.getByText('My Library')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText('My Library'));
    await waitFor(() => {
      expect(screen.getByText('Move here')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText('Move here'));
    await waitFor(() => {
      expect(moveFile).toHaveBeenCalledWith('repo-1', '/report.pdf', 'repo-1', '/');
    });
  });
});
