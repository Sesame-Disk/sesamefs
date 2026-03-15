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

const mockSharedFolders = [
  {
    repo_id: 'repo-1',
    repo_name: 'My Library',
    path: '/documents/shared',
    folder_name: 'shared',
    share_type: 'personal' as const,
    share_permission: 'rw',
    user_email: 'bob@example.com',
    user_name: 'Bob',
    contact_email: 'bob@example.com',
  },
  {
    repo_id: 'repo-2',
    repo_name: 'Work Files',
    path: '/projects',
    folder_name: 'projects',
    share_type: 'group' as const,
    share_permission: 'r',
    group_id: 5,
    group_name: 'Engineering',
  },
];

const mockListAllShareLinks = vi.fn();
const mockListAllUploadLinks = vi.fn();
const mockDeleteShareLink = vi.fn();
const mockDeleteUploadLink = vi.fn();
const mockListSharedFolders = vi.fn();
const mockUnshareFolder = vi.fn();

vi.mock('../../../lib/api', () => ({
  listAllShareLinks: (...args: unknown[]) => mockListAllShareLinks(...args),
  listAllUploadLinks: (...args: unknown[]) => mockListAllUploadLinks(...args),
  deleteShareLink: (...args: unknown[]) => mockDeleteShareLink(...args),
  deleteUploadLink: (...args: unknown[]) => mockDeleteUploadLink(...args),
  listSharedFolders: (...args: unknown[]) => mockListSharedFolders(...args),
  unshareFolder: (...args: unknown[]) => mockUnshareFolder(...args),
}));

describe('ShareAdmin', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListSharedFolders.mockResolvedValue([]);
  });

  it('renders all three tabs', async () => {
    mockListAllShareLinks.mockResolvedValue(mockShareLinks);
    mockListAllUploadLinks.mockResolvedValue(mockUploadLinks);
    render(<ShareAdmin />);
    await waitFor(() => {
      expect(screen.getByText('Share Links')).toBeInTheDocument();
    });
    expect(screen.getByText('Upload Links')).toBeInTheDocument();
    expect(screen.getByText('Folders')).toBeInTheDocument();
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
    mockListSharedFolders.mockRejectedValue(new Error('Network error'));
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

  // Shared Folders tab tests

  it('switches to shared folders tab and lists shared folders', async () => {
    mockListAllShareLinks.mockResolvedValue([]);
    mockListAllUploadLinks.mockResolvedValue([]);
    mockListSharedFolders.mockResolvedValue(mockSharedFolders);
    render(<ShareAdmin />);

    await waitFor(() => {
      expect(screen.getByTestId('tab-shared-folders')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('tab-shared-folders'));

    await waitFor(() => {
      expect(screen.getByText('shared')).toBeInTheDocument();
    });
    expect(screen.getByText('projects')).toBeInTheDocument();
    expect(screen.getByText(/Bob/)).toBeInTheDocument();
    expect(screen.getByText(/Engineering/)).toBeInTheDocument();
    expect(screen.getByText(/Read-Write/)).toBeInTheDocument();
    expect(screen.getByText(/Read-Only/)).toBeInTheDocument();
  });

  it('shows empty state for shared folders', async () => {
    mockListAllShareLinks.mockResolvedValue([]);
    mockListAllUploadLinks.mockResolvedValue([]);
    mockListSharedFolders.mockResolvedValue([]);
    render(<ShareAdmin />);

    await waitFor(() => {
      expect(screen.getByTestId('tab-shared-folders')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('tab-shared-folders'));

    await waitFor(() => {
      expect(screen.getByText('No shared folders')).toBeInTheDocument();
    });
  });

  it('unshares a folder with user via swipe action', async () => {
    mockListAllShareLinks.mockResolvedValue([]);
    mockListAllUploadLinks.mockResolvedValue([]);
    mockListSharedFolders.mockResolvedValue(mockSharedFolders);
    mockUnshareFolder.mockResolvedValue(undefined);
    render(<ShareAdmin />);

    await waitFor(() => {
      expect(screen.getByTestId('tab-shared-folders')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('tab-shared-folders'));

    await waitFor(() => {
      expect(screen.getByText('shared')).toBeInTheDocument();
    });

    // Click Unshare action on the first folder
    const unshareButtons = screen.getAllByText('Unshare');
    fireEvent.click(unshareButtons[0]);

    // Confirmation dialog should appear
    await waitFor(() => {
      expect(screen.getByText('Unshare Folder')).toBeInTheDocument();
    });
    expect(screen.getByText(/Are you sure you want to unshare "shared"/)).toBeInTheDocument();

    // Confirm unshare
    fireEvent.click(screen.getByTestId('confirm-unshare-btn'));

    await waitFor(() => {
      expect(mockUnshareFolder).toHaveBeenCalledWith('repo-1', '/documents/shared', 'user', 'bob@example.com');
    });

    await waitFor(() => {
      expect(screen.queryByText(/Shared to Bob/)).not.toBeInTheDocument();
    });
  });

  it('unshares a folder with group', async () => {
    mockListAllShareLinks.mockResolvedValue([]);
    mockListAllUploadLinks.mockResolvedValue([]);
    mockListSharedFolders.mockResolvedValue([mockSharedFolders[1]]);
    mockUnshareFolder.mockResolvedValue(undefined);
    render(<ShareAdmin />);

    await waitFor(() => {
      expect(screen.getByTestId('tab-shared-folders')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('tab-shared-folders'));

    await waitFor(() => {
      expect(screen.getByText('projects')).toBeInTheDocument();
    });

    const unshareButtons = screen.getAllByText('Unshare');
    fireEvent.click(unshareButtons[0]);

    await waitFor(() => {
      expect(screen.getByText('Unshare Folder')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('confirm-unshare-btn'));

    await waitFor(() => {
      expect(mockUnshareFolder).toHaveBeenCalledWith('repo-2', '/projects', 'group', '5');
    });
  });

  it('cancels unshare folder confirmation', async () => {
    mockListAllShareLinks.mockResolvedValue([]);
    mockListAllUploadLinks.mockResolvedValue([]);
    mockListSharedFolders.mockResolvedValue(mockSharedFolders);
    render(<ShareAdmin />);

    await waitFor(() => {
      expect(screen.getByTestId('tab-shared-folders')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('tab-shared-folders'));

    await waitFor(() => {
      expect(screen.getByText('shared')).toBeInTheDocument();
    });

    const unshareButtons = screen.getAllByText('Unshare');
    fireEvent.click(unshareButtons[0]);

    await waitFor(() => {
      expect(screen.getByText('Unshare Folder')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Cancel'));

    await waitFor(() => {
      expect(screen.queryByText('Unshare Folder')).not.toBeInTheDocument();
    });
    // Folder should still be there
    expect(screen.getByText('shared')).toBeInTheDocument();
  });

  it('shows repo name and path for shared folders', async () => {
    mockListAllShareLinks.mockResolvedValue([]);
    mockListAllUploadLinks.mockResolvedValue([]);
    mockListSharedFolders.mockResolvedValue(mockSharedFolders);
    render(<ShareAdmin />);

    await waitFor(() => {
      expect(screen.getByTestId('tab-shared-folders')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('tab-shared-folders'));

    await waitFor(() => {
      expect(screen.getByText('My Library /documents/shared')).toBeInTheDocument();
    });
    expect(screen.getByText('Work Files /projects')).toBeInTheDocument();
  });
});
