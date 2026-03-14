import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import ShareSheet from '../../components/share/ShareSheet';
import { MOCK_SHARE_LINKS, MOCK_SHARE_ITEMS } from '../mocks/data';

const mockListShareLinks = vi.fn().mockResolvedValue(MOCK_SHARE_LINKS);
const mockCreateShareLink = vi.fn().mockResolvedValue(MOCK_SHARE_LINKS[0]);
const mockDeleteShareLink = vi.fn().mockResolvedValue(undefined);
const mockListRepoShareItems = vi.fn().mockResolvedValue(MOCK_SHARE_ITEMS);
const mockListRepoGroupShares = vi.fn().mockResolvedValue([]);
const mockShareToUser = vi.fn().mockResolvedValue(undefined);
const mockSearchUsers = vi.fn().mockResolvedValue([
  { email: 'alice@example.com', name: 'Alice Smith', avatar_url: '/avatar2.png' },
]);

vi.mock('../../lib/api', () => ({
  listShareLinks: (...args: unknown[]) => mockListShareLinks(...args),
  createShareLink: (...args: unknown[]) => mockCreateShareLink(...args),
  deleteShareLink: (...args: unknown[]) => mockDeleteShareLink(...args),
  listRepoShareItems: (...args: unknown[]) => mockListRepoShareItems(...args),
  listRepoGroupShares: (...args: unknown[]) => mockListRepoGroupShares(...args),
  shareToUser: (...args: unknown[]) => mockShareToUser(...args),
  removeUserShare: vi.fn().mockResolvedValue(undefined),
  removeGroupShare: vi.fn().mockResolvedValue(undefined),
  searchUsers: (...args: unknown[]) => mockSearchUsers(...args),
  getAuthToken: vi.fn().mockReturnValue('mock-token'),
  listGroups: vi.fn().mockResolvedValue([]),
}));

vi.mock('../../lib/share', () => ({
  shareFile: vi.fn().mockResolvedValue(true),
  downloadFile: vi.fn(),
}));

vi.mock('../../lib/config', () => ({
  getConfig: () => ({
    serviceURL: 'http://localhost:8080',
    siteRoot: '/',
    mediaUrl: '/static/',
  }),
  getPageOptions: () => ({
    canShareRepo: true,
    canGenerateShareLink: true,
  }),
  serviceURL: () => 'http://localhost:8080',
}));

vi.mock('framer-motion', () => ({
  motion: {
    div: ({ children, ...props }: any) => {
      const filtered: Record<string, unknown> = {};
      for (const [k, v] of Object.entries(props)) {
        if (!['initial', 'animate', 'exit', 'transition', 'drag', 'dragConstraints', 'dragElastic', 'onDragEnd', 'whileTap', 'whileHover', 'variants', 'layout'].includes(k)) {
          filtered[k] = v;
        }
      }
      return <div {...filtered}>{children}</div>;
    },
  },
  AnimatePresence: ({ children }: any) => <>{children}</>,
}));

describe('Share Flow', () => {
  const mockOnToast = vi.fn();
  const mockOnClose = vi.fn();

  const defaultProps = {
    isOpen: true,
    onClose: mockOnClose,
    repoId: 'repo-001',
    path: '/',
    isDir: false,
    itemName: 'document.pdf',
    onToast: mockOnToast,
  };

  beforeEach(() => {
    mockListShareLinks.mockClear().mockResolvedValue(MOCK_SHARE_LINKS);
    mockCreateShareLink.mockClear().mockResolvedValue(MOCK_SHARE_LINKS[0]);
    mockDeleteShareLink.mockClear().mockResolvedValue(undefined);
    mockListRepoShareItems.mockClear().mockResolvedValue(MOCK_SHARE_ITEMS);
    mockListRepoGroupShares.mockClear().mockResolvedValue([]);
    mockShareToUser.mockClear().mockResolvedValue(undefined);
    mockSearchUsers.mockClear().mockResolvedValue([
      { email: 'alice@example.com', name: 'Alice Smith', avatar_url: '/avatar2.png' },
    ]);
    mockOnToast.mockClear();
    mockOnClose.mockClear();
  });

  it('renders share sheet with tabs', async () => {
    render(<ShareSheet {...defaultProps} />);

    expect(screen.getByText('Share: document.pdf')).toBeInTheDocument();
    expect(screen.getByTestId('tab-share-link')).toBeInTheDocument();
    expect(screen.getByTestId('tab-internal-share')).toBeInTheDocument();
  });

  it('shows existing share link on the link tab', async () => {
    render(<ShareSheet {...defaultProps} />);

    await waitFor(() => {
      expect(mockListShareLinks).toHaveBeenCalledWith('repo-001', '/document.pdf');
    });

    await waitFor(() => {
      expect(screen.getByTestId('share-link-url')).toBeInTheDocument();
    });
  });

  it('shows QR code for existing share link', async () => {
    render(<ShareSheet {...defaultProps} />);

    await waitFor(() => {
      expect(screen.getByTestId('qr-code')).toBeInTheDocument();
    });
  });

  it('copies share link to clipboard', async () => {
    render(<ShareSheet {...defaultProps} />);

    await waitFor(() => {
      expect(screen.getByTestId('share-link-url')).toBeInTheDocument();
    });

    // Click copy button
    const copyBtn = screen.getByLabelText('Copy link');
    fireEvent.click(copyBtn);

    await waitFor(() => {
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith(MOCK_SHARE_LINKS[0].link);
    });

    await waitFor(() => {
      expect(mockOnToast).toHaveBeenCalledWith('Link copied to clipboard');
    });
  });

  it('generates a new share link when none exists', async () => {
    mockListShareLinks.mockResolvedValueOnce([]);

    render(<ShareSheet {...defaultProps} />);

    await waitFor(() => {
      expect(screen.getByTestId('generate-link-btn')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('generate-link-btn'));

    await waitFor(() => {
      expect(mockCreateShareLink).toHaveBeenCalledWith(
        'repo-001',
        '/document.pdf',
        expect.objectContaining({ permissions: { can_edit: false, can_download: true } })
      );
    });
  });

  it('switches to internal share tab', async () => {
    render(<ShareSheet {...defaultProps} />);

    fireEvent.click(screen.getByTestId('tab-internal-share'));

    await waitFor(() => {
      expect(mockListRepoShareItems).toHaveBeenCalled();
    });
  });

  it('shows link info (views, permissions)', async () => {
    render(<ShareSheet {...defaultProps} />);

    await waitFor(() => {
      expect(screen.getByText(/Views: 5/)).toBeInTheDocument();
    });
  });

  it('shows delete link option', async () => {
    render(<ShareSheet {...defaultProps} />);

    await waitFor(() => {
      expect(screen.getByText('Delete Link')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Delete Link'));

    await waitFor(() => {
      expect(screen.getByText('Delete this share link?')).toBeInTheDocument();
    });
  });

  it('deletes a share link after confirmation', async () => {
    render(<ShareSheet {...defaultProps} />);

    await waitFor(() => {
      expect(screen.getByText('Delete Link')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Delete Link'));

    await waitFor(() => {
      expect(screen.getByText('Delete this share link?')).toBeInTheDocument();
    });

    // Click the confirm delete button (not the "Delete Link" button)
    const deleteConfirmBtns = screen.getAllByText('Delete');
    fireEvent.click(deleteConfirmBtns[deleteConfirmBtns.length - 1]);

    await waitFor(() => {
      expect(mockDeleteShareLink).toHaveBeenCalledWith('abc123token');
    });
  });

  it('does not render when isOpen is false', () => {
    const { container } = render(<ShareSheet {...defaultProps} isOpen={false} />);

    // AnimatePresence should not render the sheet
    expect(screen.queryByText('Share: document.pdf')).not.toBeInTheDocument();
  });
});
