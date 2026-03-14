import React from 'react';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import ShareAdmin from '../ShareAdmin';

const mockShareLinks = [
  {
    token: 'share-token-1',
    link: 'https://example.com/f/share-token-1/',
    repo_id: 'repo-1',
    path: '/docs/readme.md',
    is_dir: false,
    is_expired: false,
    expire_date: null,
    permissions: { can_edit: false, can_download: true },
    ctime: '2026-01-15T10:00:00Z',
    view_cnt: 42,
  },
  {
    token: 'share-token-2',
    link: 'https://example.com/f/share-token-2/',
    repo_id: 'repo-2',
    path: '/photos/',
    is_dir: true,
    is_expired: false,
    expire_date: null,
    permissions: { can_edit: false, can_download: true },
    ctime: '2026-02-10T12:00:00Z',
    view_cnt: 10,
  },
];

const mockUploadLinks = [
  {
    token: 'upload-token-1',
    link: 'https://example.com/u/upload-token-1/',
    repo_id: 'repo-1',
    path: '/uploads/',
    ctime: '2026-01-20T08:00:00Z',
    username: 'user@example.com',
    view_cnt: 5,
  },
];

const mockListAllShareLinks = vi.fn();
const mockListAllUploadLinks = vi.fn();
const mockDeleteShareLink = vi.fn();
const mockDeleteUploadLink = vi.fn();

vi.mock('../../../lib/api', () => ({
  listAllShareLinks: (...args: unknown[]) => mockListAllShareLinks(...args),
  listAllUploadLinks: (...args: unknown[]) => mockListAllUploadLinks(...args),
  deleteShareLink: (...args: unknown[]) => mockDeleteShareLink(...args),
  deleteUploadLink: (...args: unknown[]) => mockDeleteUploadLink(...args),
}));

describe('ShareAdmin', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders both tabs', async () => {
    mockListAllShareLinks.mockResolvedValue(mockShareLinks);
    mockListAllUploadLinks.mockResolvedValue(mockUploadLinks);
    render(<ShareAdmin />);
    await waitFor(() => {
      expect(screen.getByText('Share Links')).toBeInTheDocument();
    });
    expect(screen.getByText('Upload Links')).toBeInTheDocument();
  });

  it('lists share links', async () => {
    mockListAllShareLinks.mockResolvedValue(mockShareLinks);
    mockListAllUploadLinks.mockResolvedValue(mockUploadLinks);
    render(<ShareAdmin />);
    await waitFor(() => {
      expect(screen.getByText('readme.md')).toBeInTheDocument();
    });
    expect(screen.getByText('photos')).toBeInTheDocument();
    expect(screen.getByText(/42 views/)).toBeInTheDocument();
  });

  it('switches to upload links tab', async () => {
    mockListAllShareLinks.mockResolvedValue(mockShareLinks);
    mockListAllUploadLinks.mockResolvedValue(mockUploadLinks);
    render(<ShareAdmin />);
    await waitFor(() => {
      expect(screen.getByText('readme.md')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Upload Links'));
    expect(screen.getByText('uploads')).toBeInTheDocument();
    expect(screen.getByText(/5 views/)).toBeInTheDocument();
  });

  it('shows delete confirmation and deletes share link', async () => {
    mockListAllShareLinks.mockResolvedValue(mockShareLinks);
    mockListAllUploadLinks.mockResolvedValue([]);
    mockDeleteShareLink.mockResolvedValue(undefined);
    render(<ShareAdmin />);
    await waitFor(() => {
      expect(screen.getByText('readme.md')).toBeInTheDocument();
    });

    // Click delete action from swipeable list
    const deleteButtons = screen.getAllByText('Delete');
    fireEvent.click(deleteButtons[0]);

    // Confirmation dialog should appear
    await waitFor(() => {
      expect(screen.getByText('Delete Link')).toBeInTheDocument();
    });
    expect(screen.getByText(/Are you sure/)).toBeInTheDocument();

    // Confirm deletion
    const confirmBtn = screen.getAllByText('Delete').find(
      (el) => el.closest('.bg-red-500')
    );
    fireEvent.click(confirmBtn!);

    await waitFor(() => {
      expect(screen.queryByText('readme.md')).not.toBeInTheDocument();
    });
    expect(mockDeleteShareLink).toHaveBeenCalledWith('share-token-1');
  });

  it('shows empty state for share links', async () => {
    mockListAllShareLinks.mockResolvedValue([]);
    mockListAllUploadLinks.mockResolvedValue([]);
    render(<ShareAdmin />);
    await waitFor(() => {
      expect(screen.getByText('No share links')).toBeInTheDocument();
    });
  });

  it('shows empty state for upload links', async () => {
    mockListAllShareLinks.mockResolvedValue([]);
    mockListAllUploadLinks.mockResolvedValue([]);
    render(<ShareAdmin />);
    await waitFor(() => {
      expect(screen.getByText('No share links')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Upload Links'));
    await waitFor(() => {
      expect(screen.getByText('No upload links')).toBeInTheDocument();
    });
  });

  it('shows error state with retry', async () => {
    mockListAllShareLinks.mockRejectedValue(new Error('Network error'));
    mockListAllUploadLinks.mockRejectedValue(new Error('Network error'));
    render(<ShareAdmin />);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Network error');
    });
    expect(screen.getByText('Retry')).toBeInTheDocument();
  });

  it('cancels delete confirmation', async () => {
    mockListAllShareLinks.mockResolvedValue(mockShareLinks);
    mockListAllUploadLinks.mockResolvedValue([]);
    render(<ShareAdmin />);
    await waitFor(() => {
      expect(screen.getByText('readme.md')).toBeInTheDocument();
    });

    const deleteButtons = screen.getAllByText('Delete');
    fireEvent.click(deleteButtons[0]);

    await waitFor(() => {
      expect(screen.getByText('Delete Link')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Cancel'));
    await waitFor(() => {
      expect(screen.queryByText('Delete Link')).not.toBeInTheDocument();
    });
    // Item should still be there
    expect(screen.getByText('readme.md')).toBeInTheDocument();
  });
});
