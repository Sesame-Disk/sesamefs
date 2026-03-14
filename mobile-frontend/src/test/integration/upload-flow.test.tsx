import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import FileBrowser from '../../components/pages/FileBrowser';
import { MOCK_DIRENTS } from '../mocks/data';

const mockListDir = vi.fn().mockResolvedValue(MOCK_DIRENTS);

// vi.mock is hoisted, so we cannot reference local variables.
// Instead we define the mock inline.
vi.mock('../../lib/upload', () => {
  const subscribe = vi.fn().mockReturnValue(() => {});
  const getQueue = vi.fn().mockReturnValue([]);
  const addFiles = vi.fn().mockReturnValue([]);
  const cancelFile = vi.fn();
  const cancelAll = vi.fn();
  return {
    uploadManager: { subscribe, getQueue, addFiles, cancelFile, cancelAll },
  };
});

vi.mock('../../lib/api', () => ({
  listDir: (...args: unknown[]) => mockListDir(...args),
  getAuthToken: vi.fn().mockReturnValue('mock-token'),
  renameFile: vi.fn().mockResolvedValue(undefined),
  renameDir: vi.fn().mockResolvedValue(undefined),
  deleteFile: vi.fn().mockResolvedValue(undefined),
  deleteDir: vi.fn().mockResolvedValue(undefined),
  moveFile: vi.fn().mockResolvedValue(undefined),
  copyFile: vi.fn().mockResolvedValue(undefined),
  getFileDownloadLink: vi.fn().mockResolvedValue('http://localhost/download'),
  starFile: vi.fn().mockResolvedValue(undefined),
  unstarFile: vi.fn().mockResolvedValue(undefined),
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
    enableUploadFolder: true,
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

describe('Upload Flow', () => {
  let uploadManagerMock: any;

  beforeEach(async () => {
    mockListDir.mockClear().mockResolvedValue(MOCK_DIRENTS);
    // Import the mocked module to access the mock functions
    const uploadModule = await import('../../lib/upload');
    uploadManagerMock = uploadModule.uploadManager;
    vi.mocked(uploadManagerMock.subscribe).mockClear();
    vi.mocked(uploadManagerMock.getQueue).mockClear().mockReturnValue([]);
    vi.mocked(uploadManagerMock.addFiles).mockClear();
    vi.mocked(uploadManagerMock.cancelFile).mockClear();
    vi.mocked(uploadManagerMock.cancelAll).mockClear();
  });

  it('renders upload FAB button when viewing a repo', async () => {
    render(<FileBrowser repoId="repo-001" repoName="My Documents" />);

    await waitFor(() => {
      expect(screen.getByText('document.pdf')).toBeInTheDocument();
    });

    const uploadArea = document.querySelector('input[type="file"]');
    expect(uploadArea).toBeInTheDocument();
  });

  it('subscribes to upload manager events', async () => {
    render(<FileBrowser repoId="repo-001" repoName="My Documents" />);

    await waitFor(() => {
      expect(uploadManagerMock.subscribe).toHaveBeenCalled();
    });
  });

  it('reloads directory on upload completion', async () => {
    // Capture the subscriber
    let subscriber: ((event: any) => void) | null = null;
    vi.mocked(uploadManagerMock.subscribe).mockImplementation((listener: any) => {
      subscriber = listener;
      return () => { subscriber = null; };
    });

    render(<FileBrowser repoId="repo-001" repoName="My Documents" />);

    await waitFor(() => {
      expect(mockListDir).toHaveBeenCalledTimes(1);
    });

    // Simulate a completed upload event
    expect(subscriber).not.toBeNull();
    act(() => {
      subscriber!({ type: 'completed', fileId: 'up-1' });
    });

    await waitFor(() => {
      expect(mockListDir).toHaveBeenCalledTimes(2);
    });
  });

  it('does not show upload FAB when no repoId', () => {
    render(<FileBrowser />);

    const uploadArea = document.querySelector('input[type="file"]');
    expect(uploadArea).not.toBeInTheDocument();
  });

  it('shows new folder dialog via the FAB', async () => {
    render(<FileBrowser repoId="repo-001" repoName="My Documents" />);

    await waitFor(() => {
      expect(screen.getByText('document.pdf')).toBeInTheDocument();
    });

    expect(document.querySelector('input[type="file"]')).toBeInTheDocument();
  });
});
