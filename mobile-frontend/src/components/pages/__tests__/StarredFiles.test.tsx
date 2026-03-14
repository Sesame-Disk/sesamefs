import React from 'react';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import StarredFiles from '../StarredFiles';

const mockStarredFiles = [
  {
    repo_id: 'repo-1',
    repo_name: 'My Library',
    path: '/docs/readme.md',
    obj_name: 'readme.md',
    mtime: 1700000000,
    size: 1024,
    is_dir: false,
  },
  {
    repo_id: 'repo-2',
    repo_name: 'Work Files',
    path: '/projects/',
    obj_name: 'projects',
    mtime: 1700100000,
    size: 0,
    is_dir: true,
  },
];

const mockListStarredFiles = vi.fn();
const mockUnstarFile = vi.fn();

vi.mock('../../../lib/api', () => ({
  listStarredFiles: (...args: unknown[]) => mockListStarredFiles(...args),
  unstarFile: (...args: unknown[]) => mockUnstarFile(...args),
}));

describe('StarredFiles', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows loading skeleton initially', () => {
    mockListStarredFiles.mockReturnValue(new Promise(() => {}));
    render(<StarredFiles />);
    expect(screen.getByTestId('skeleton-list')).toBeInTheDocument();
  });

  it('renders starred files after loading', async () => {
    mockListStarredFiles.mockResolvedValue(mockStarredFiles);
    render(<StarredFiles />);
    await waitFor(() => {
      expect(screen.getByText('readme.md')).toBeInTheDocument();
    });
    expect(screen.getByText('projects')).toBeInTheDocument();
    expect(screen.getByText(/My Library/)).toBeInTheDocument();
    expect(screen.getByText(/Work Files/)).toBeInTheDocument();
  });

  it('shows empty state when no starred files', async () => {
    mockListStarredFiles.mockResolvedValue([]);
    render(<StarredFiles />);
    await waitFor(() => {
      expect(screen.getByText('No starred files yet')).toBeInTheDocument();
    });
    expect(screen.getByText(/Star important files/)).toBeInTheDocument();
  });

  it('shows error state with retry button', async () => {
    mockListStarredFiles.mockRejectedValue(new Error('Network error'));
    render(<StarredFiles />);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Network error');
    });
    expect(screen.getByText('Retry')).toBeInTheDocument();
  });

  it('retries loading on retry button click', async () => {
    mockListStarredFiles.mockRejectedValueOnce(new Error('Network error'));
    render(<StarredFiles />);
    await waitFor(() => {
      expect(screen.getByText('Retry')).toBeInTheDocument();
    });

    mockListStarredFiles.mockResolvedValue(mockStarredFiles);
    fireEvent.click(screen.getByText('Retry'));
    await waitFor(() => {
      expect(screen.getByText('readme.md')).toBeInTheDocument();
    });
  });

  it('removes file from list after unstar', async () => {
    mockListStarredFiles.mockResolvedValue(mockStarredFiles);
    mockUnstarFile.mockResolvedValue(undefined);
    render(<StarredFiles />);
    await waitFor(() => {
      expect(screen.getByText('readme.md')).toBeInTheDocument();
    });

    const unstarButtons = screen.getAllByText('Unstar');
    fireEvent.click(unstarButtons[0]);
    await waitFor(() => {
      expect(screen.queryByText('readme.md')).not.toBeInTheDocument();
    });
    expect(mockUnstarFile).toHaveBeenCalledWith('repo-1', '/docs/readme.md');
    expect(screen.getByText('projects')).toBeInTheDocument();
  });

  it('renders page title', async () => {
    mockListStarredFiles.mockResolvedValue([]);
    render(<StarredFiles />);
    await waitFor(() => {
      expect(screen.getByText('Starred Files')).toBeInTheDocument();
    });
  });

  it('shows refresh button', async () => {
    mockListStarredFiles.mockResolvedValue(mockStarredFiles);
    render(<StarredFiles />);
    await waitFor(() => {
      expect(screen.getByText('Refresh')).toBeInTheDocument();
    });
  });
});
