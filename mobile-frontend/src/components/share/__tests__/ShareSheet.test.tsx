import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import ShareSheet from '../ShareSheet';

// Mock the API module
vi.mock('../../../lib/api', () => ({
  listShareLinks: vi.fn().mockResolvedValue([]),
  createShareLink: vi.fn().mockResolvedValue({
    token: 'abc123',
    link: 'https://example.com/s/abc123',
    repo_id: 'repo-1',
    path: '/test.pdf',
    is_dir: false,
    is_expired: false,
    expire_date: null,
    permissions: { can_edit: false, can_download: true },
    ctime: '2026-03-10T10:00:00Z',
    view_cnt: 0,
  }),
  deleteShareLink: vi.fn().mockResolvedValue(undefined),
  listRepoShareItems: vi.fn().mockResolvedValue([]),
  listRepoGroupShares: vi.fn().mockResolvedValue([]),
  listGroups: vi.fn().mockResolvedValue([]),
  searchUsers: vi.fn().mockResolvedValue([]),
  shareToUser: vi.fn().mockResolvedValue(undefined),
  shareToGroup: vi.fn().mockResolvedValue(undefined),
  removeUserShare: vi.fn().mockResolvedValue(undefined),
  removeGroupShare: vi.fn().mockResolvedValue(undefined),
}));

// Mock qrcode.react
vi.mock('qrcode.react', () => ({
  QRCodeSVG: ({ value }: { value: string }) => <div data-testid="qr-code-svg">{value}</div>,
}));

// Mock share utility
vi.mock('../../../lib/share', () => ({
  shareFile: vi.fn().mockResolvedValue(true),
}));

const defaultProps = {
  isOpen: true,
  onClose: vi.fn(),
  repoId: 'repo-1',
  path: '/',
  isDir: false,
  itemName: 'test.pdf',
  onToast: vi.fn(),
};

describe('ShareSheet', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders both tabs', async () => {
    render(<ShareSheet {...defaultProps} />);
    expect(screen.getByTestId('tab-share-link')).toBeInTheDocument();
    expect(screen.getByTestId('tab-internal-share')).toBeInTheDocument();
  });

  it('shows Share Link tab by default', async () => {
    render(<ShareSheet {...defaultProps} />);
    const linkTab = screen.getByTestId('tab-share-link');
    expect(linkTab.className).toContain('text-primary');
  });

  it('switches to Internal Share tab on click', async () => {
    render(<ShareSheet {...defaultProps} />);
    fireEvent.click(screen.getByTestId('tab-internal-share'));
    const internalTab = screen.getByTestId('tab-internal-share');
    expect(internalTab.className).toContain('text-primary');
  });

  it('shows generate link button when no link exists', async () => {
    render(<ShareSheet {...defaultProps} />);
    await waitFor(() => {
      expect(screen.getByTestId('generate-link-btn')).toBeInTheDocument();
    });
  });

  it('generates a share link and shows QR code', async () => {
    const { createShareLink } = await import('../../../lib/api');

    render(<ShareSheet {...defaultProps} />);

    await waitFor(() => {
      expect(screen.getByTestId('generate-link-btn')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('generate-link-btn'));

    await waitFor(() => {
      expect(createShareLink).toHaveBeenCalledWith('repo-1', '/test.pdf', expect.any(Object));
    });

    await waitFor(() => {
      expect(screen.getByTestId('qr-code')).toBeInTheDocument();
    });
  });

  it('copies link to clipboard', async () => {
    const { listShareLinks } = await import('../../../lib/api');
    (listShareLinks as ReturnType<typeof vi.fn>).mockResolvedValue([{
      token: 'abc123',
      link: 'https://example.com/s/abc123',
      repo_id: 'repo-1',
      path: '/test.pdf',
      is_dir: false,
      is_expired: false,
      expire_date: null,
      permissions: { can_edit: false, can_download: true },
      ctime: '2026-03-10T10:00:00Z',
      view_cnt: 5,
    }]);

    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      writable: true,
      configurable: true,
    });

    render(<ShareSheet {...defaultProps} />);

    await waitFor(() => {
      expect(screen.getByTestId('share-link-url')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByLabelText('Copy link'));

    await waitFor(() => {
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('https://example.com/s/abc123');
    });
  });

  it('does not render when isOpen is false', () => {
    const { container } = render(<ShareSheet {...defaultProps} isOpen={false} />);
    expect(container.innerHTML).toBe('');
  });
});
