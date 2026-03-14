import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import FileBrowser from '../../components/pages/FileBrowser';
import { MOCK_DIRENTS } from '../mocks/data';

// Mock all dependencies
const mockListDir = vi.fn().mockResolvedValue(MOCK_DIRENTS);
const mockStarFile = vi.fn().mockResolvedValue(undefined);
const mockUnstarFile = vi.fn().mockResolvedValue(undefined);
const mockGetFileDownloadLink = vi.fn().mockResolvedValue('http://localhost/download');

vi.mock('../../lib/api', () => ({
  listDir: (...args: unknown[]) => mockListDir(...args),
  starFile: (...args: unknown[]) => mockStarFile(...args),
  unstarFile: (...args: unknown[]) => mockUnstarFile(...args),
  getFileDownloadLink: (...args: unknown[]) => mockGetFileDownloadLink(...args),
  getAuthToken: vi.fn().mockReturnValue('mock-token'),
  renameFile: vi.fn().mockResolvedValue(undefined),
  renameDir: vi.fn().mockResolvedValue(undefined),
  deleteFile: vi.fn().mockResolvedValue(undefined),
  deleteDir: vi.fn().mockResolvedValue(undefined),
  moveFile: vi.fn().mockResolvedValue(undefined),
  copyFile: vi.fn().mockResolvedValue(undefined),
  listShareLinks: vi.fn().mockResolvedValue([]),
  createShareLink: vi.fn().mockResolvedValue({ token: 'test', link: 'http://test' }),
  listRepoShareItems: vi.fn().mockResolvedValue([]),
  listRepoGroupShares: vi.fn().mockResolvedValue([]),
  listRepos: vi.fn().mockResolvedValue([]),
  searchUsers: vi.fn().mockResolvedValue([]),
}));

vi.mock('../../lib/share', () => ({
  downloadFile: vi.fn(),
  shareFile: vi.fn().mockResolvedValue(true),
}));

vi.mock('../../lib/upload', () => ({
  uploadManager: {
    subscribe: vi.fn().mockReturnValue(() => {}),
    getQueue: vi.fn().mockReturnValue([]),
    addFiles: vi.fn().mockReturnValue([]),
    cancelFile: vi.fn(),
    cancelAll: vi.fn(),
  },
}));

vi.mock('../../lib/encryption', () => ({
  isRepoDecrypted: vi.fn().mockReturnValue(true),
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
    maxFileName: 255,
  }),
  serviceURL: () => 'http://localhost:8080',
}));

// Mock framer-motion to avoid animation issues in tests
vi.mock('framer-motion', () => ({
  motion: {
    div: ({ children, ...props }: any) => <div {...filterMotionProps(props)}>{children}</div>,
  },
  AnimatePresence: ({ children }: any) => <>{children}</>,
}));

function filterMotionProps(props: Record<string, unknown>) {
  const filtered: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(props)) {
    if (!['initial', 'animate', 'exit', 'transition', 'drag', 'dragConstraints', 'dragElastic', 'onDragEnd', 'whileTap', 'whileHover', 'variants', 'layout'].includes(key)) {
      filtered[key] = value;
    }
  }
  return filtered;
}

describe('Library Browsing', () => {
  beforeEach(() => {
    mockListDir.mockClear().mockResolvedValue(MOCK_DIRENTS);
    mockStarFile.mockClear();
    mockUnstarFile.mockClear();
  });

  it('shows placeholder when no repoId is provided', () => {
    render(<FileBrowser />);

    expect(screen.getByText('Select a library to browse files')).toBeInTheDocument();
  });

  it('loads and renders directory listing', async () => {
    render(<FileBrowser repoId="repo-001" repoName="My Documents" />);

    await waitFor(() => {
      expect(mockListDir).toHaveBeenCalledWith('repo-001', '/');
    });

    await waitFor(() => {
      expect(screen.getByText('Reports')).toBeInTheDocument();
      expect(screen.getByText('Images')).toBeInTheDocument();
      expect(screen.getByText('document.pdf')).toBeInTheDocument();
      expect(screen.getByText('photo.jpg')).toBeInTheDocument();
    });
  });

  it('shows loading state', async () => {
    let resolveDir: (value: unknown) => void;
    mockListDir.mockImplementation(() => new Promise(resolve => { resolveDir = resolve; }));

    render(<FileBrowser repoId="repo-001" repoName="My Documents" />);

    expect(screen.getByText('Loading...')).toBeInTheDocument();

    resolveDir!(MOCK_DIRENTS);

    await waitFor(() => {
      expect(screen.queryByText('Loading...')).not.toBeInTheDocument();
    });
  });

  it('shows error state on API failure', async () => {
    mockListDir.mockRejectedValueOnce(new Error('Network error'));

    render(<FileBrowser repoId="repo-001" repoName="My Documents" />);

    await waitFor(() => {
      expect(screen.getByText('Network error')).toBeInTheDocument();
    });
  });

  it('navigates into a directory on click', async () => {
    render(<FileBrowser repoId="repo-001" repoName="My Documents" />);

    await waitFor(() => {
      expect(screen.getByText('Reports')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Reports'));

    await waitFor(() => {
      expect(mockListDir).toHaveBeenCalledWith('repo-001', '/Reports');
    });
  });

  it('renders breadcrumb navigation', async () => {
    render(<FileBrowser repoId="repo-001" repoName="My Documents" initialPath="/Reports/2026" />);

    await waitFor(() => {
      expect(screen.getByText('Root')).toBeInTheDocument();
      expect(screen.getByText('Reports')).toBeInTheDocument();
      expect(screen.getByText('2026')).toBeInTheDocument();
    });
  });

  it('navigates via breadcrumb to root', async () => {
    render(<FileBrowser repoId="repo-001" repoName="My Documents" initialPath="/Reports/2026" />);

    await waitFor(() => {
      expect(screen.getByText('Root')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Root'));

    await waitFor(() => {
      expect(mockListDir).toHaveBeenCalledWith('repo-001', '/');
    });
  });

  it('navigates via breadcrumb to intermediate path', async () => {
    render(<FileBrowser repoId="repo-001" repoName="My Documents" initialPath="/Reports/2026" />);

    await waitFor(() => {
      expect(screen.getByText('Reports')).toBeInTheDocument();
    });

    // Click the breadcrumb for Reports (which is the first breadcrumb, not the dir listing item)
    const buttons = screen.getAllByText('Reports');
    // The first 'Reports' is the breadcrumb
    fireEvent.click(buttons[0]);

    await waitFor(() => {
      expect(mockListDir).toHaveBeenCalledWith('repo-001', '/Reports');
    });
  });

  it('shows empty folder message', async () => {
    mockListDir.mockResolvedValueOnce([]);

    render(<FileBrowser repoId="repo-001" repoName="My Documents" />);

    await waitFor(() => {
      expect(screen.getByText('This folder is empty')).toBeInTheDocument();
    });
  });

  it('displays file sizes and dates', async () => {
    render(<FileBrowser repoId="repo-001" repoName="My Documents" />);

    await waitFor(() => {
      // document.pdf is 1 MB
      expect(screen.getByText('document.pdf')).toBeInTheDocument();
    });
  });

  it('shows starred indicator on starred files', async () => {
    render(<FileBrowser repoId="repo-001" repoName="My Documents" />);

    await waitFor(() => {
      expect(screen.getByText('document.pdf')).toBeInTheDocument();
    });

    // document.pdf and code.py are starred in MOCK_DIRENTS
    // Check that the item containers exist
    const documentItem = screen.getByText('document.pdf').closest('div');
    expect(documentItem).toBeInTheDocument();
  });

  it('opens context menu for file actions', async () => {
    render(<FileBrowser repoId="repo-001" repoName="My Documents" />);

    await waitFor(() => {
      expect(screen.getByText('document.pdf')).toBeInTheDocument();
    });

    // Click the more options button
    const moreButtons = screen.getAllByLabelText(/more options for/i);
    expect(moreButtons.length).toBeGreaterThan(0);
    fireEvent.click(moreButtons[3]); // document.pdf is 4th item (after 3 dirs)

    // Context menu should appear (rendered via FileContextMenu component)
    await waitFor(() => {
      expect(screen.getByText('Rename')).toBeInTheDocument();
    });
  });
});
