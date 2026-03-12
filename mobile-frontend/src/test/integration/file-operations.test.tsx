import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import FileBrowser from '../../components/pages/FileBrowser';
import { MOCK_DIRENTS } from '../mocks/data';

const mockListDir = vi.fn().mockResolvedValue(MOCK_DIRENTS);
const mockRenameFile = vi.fn().mockResolvedValue(undefined);
const mockDeleteFile = vi.fn().mockResolvedValue(undefined);
const mockDeleteDir = vi.fn().mockResolvedValue(undefined);
const mockMoveFile = vi.fn().mockResolvedValue(undefined);
const mockStarFile = vi.fn().mockResolvedValue(undefined);
const mockUnstarFile = vi.fn().mockResolvedValue(undefined);

vi.mock('../../lib/api', () => ({
  listDir: (...args: unknown[]) => mockListDir(...args),
  renameFile: (...args: unknown[]) => mockRenameFile(...args),
  renameDir: vi.fn().mockResolvedValue(undefined),
  deleteFile: (...args: unknown[]) => mockDeleteFile(...args),
  deleteDir: (...args: unknown[]) => mockDeleteDir(...args),
  moveFile: (...args: unknown[]) => mockMoveFile(...args),
  moveDir: vi.fn().mockResolvedValue(undefined),
  copyFile: vi.fn().mockResolvedValue(undefined),
  copyDir: vi.fn().mockResolvedValue(undefined),
  getFileDownloadLink: vi.fn().mockResolvedValue('http://localhost/download'),
  starFile: (...args: unknown[]) => mockStarFile(...args),
  unstarFile: (...args: unknown[]) => mockUnstarFile(...args),
  getAuthToken: vi.fn().mockReturnValue('mock-token'),
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

describe('File Operations', () => {
  beforeEach(() => {
    mockListDir.mockClear().mockResolvedValue(MOCK_DIRENTS);
    mockRenameFile.mockClear().mockResolvedValue(undefined);
    mockDeleteFile.mockClear().mockResolvedValue(undefined);
    mockDeleteDir.mockClear().mockResolvedValue(undefined);
    mockMoveFile.mockClear().mockResolvedValue(undefined);
    mockStarFile.mockClear().mockResolvedValue(undefined);
    mockUnstarFile.mockClear().mockResolvedValue(undefined);
  });

  async function renderAndWait() {
    render(<FileBrowser repoId="repo-001" repoName="My Documents" />);
    await waitFor(() => {
      expect(screen.getByText('document.pdf')).toBeInTheDocument();
    });
  }

  async function openContextMenuForFile(fileName: string) {
    const btn = screen.getByLabelText(`More options for ${fileName}`);
    fireEvent.click(btn);
  }

  it('opens rename dialog and renames a file', async () => {
    await renderAndWait();
    openContextMenuForFile('document.pdf');

    await waitFor(() => {
      expect(screen.getByText('Rename')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Rename'));

    await waitFor(() => {
      expect(screen.getByLabelText('New name')).toBeInTheDocument();
    });

    const input = screen.getByLabelText('New name');
    fireEvent.change(input, { target: { value: 'renamed-doc.pdf' } });
    fireEvent.click(screen.getByText('Save'));

    await waitFor(() => {
      expect(mockRenameFile).toHaveBeenCalledWith('repo-001', '/document.pdf', 'renamed-doc.pdf');
    });
  });

  it('opens delete confirmation and deletes a file', async () => {
    await renderAndWait();
    openContextMenuForFile('notes.txt');

    await waitFor(() => {
      // Context menu items appear
      const deleteButtons = screen.getAllByText('Delete');
      expect(deleteButtons.length).toBeGreaterThan(0);
    });

    // Click Delete in context menu
    const deleteButtons = screen.getAllByText('Delete');
    fireEvent.click(deleteButtons[0]);

    await waitFor(() => {
      expect(screen.getByText(/Delete "notes.txt"\?/)).toBeInTheDocument();
    });

    // Click the confirm Delete button in the dialog
    const confirmDeleteBtns = screen.getAllByText('Delete');
    const confirmBtn = confirmDeleteBtns[confirmDeleteBtns.length - 1];
    fireEvent.click(confirmBtn);

    await waitFor(() => {
      expect(mockDeleteFile).toHaveBeenCalledWith('repo-001', '/notes.txt');
    });
  });

  it('unstars a starred file via context menu', async () => {
    await renderAndWait();
    openContextMenuForFile('document.pdf');

    await waitFor(() => {
      expect(screen.getByText('Unstar')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Unstar'));

    await waitFor(() => {
      expect(mockUnstarFile).toHaveBeenCalledWith('repo-001', '/document.pdf');
    });
  });

  it('stars an unstarred file via context menu', async () => {
    await renderAndWait();
    openContextMenuForFile('photo.jpg');

    await waitFor(() => {
      expect(screen.getByText('Star')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Star'));

    await waitFor(() => {
      expect(mockStarFile).toHaveBeenCalledWith('repo-001', '/photo.jpg');
    });
  });

  it('activates multi-select mode on long press', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });

    render(<FileBrowser repoId="repo-001" repoName="My Documents" />);

    await vi.waitFor(() => {
      expect(screen.getByText('document.pdf')).toBeInTheDocument();
    });

    const item = screen.getByText('document.pdf').closest('div[class*="flex items-center"]')!;
    fireEvent.pointerDown(item);
    vi.advanceTimersByTime(600);
    fireEvent.pointerUp(item);

    await vi.waitFor(() => {
      const checkboxes = screen.getAllByRole('checkbox');
      expect(checkboxes.length).toBeGreaterThan(0);
    });

    vi.useRealTimers();
  });

  it('selects multiple items in multi-select mode', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });

    render(<FileBrowser repoId="repo-001" repoName="My Documents" />);

    await vi.waitFor(() => {
      expect(screen.getByText('document.pdf')).toBeInTheDocument();
    });

    // Activate multi-select via long press
    const firstItem = screen.getByText('document.pdf').closest('div[class*="flex items-center"]')!;
    fireEvent.pointerDown(firstItem);
    vi.advanceTimersByTime(600);
    fireEvent.pointerUp(firstItem);

    await vi.waitFor(() => {
      expect(screen.getAllByRole('checkbox').length).toBeGreaterThan(0);
    });

    // Tap another item to select it
    fireEvent.click(screen.getByText('photo.jpg'));

    await vi.waitFor(() => {
      const checkedBoxes = screen.getAllByRole('checkbox').filter((cb: HTMLElement) => (cb as HTMLInputElement).checked);
      expect(checkedBoxes.length).toBe(2);
    });

    vi.useRealTimers();
  });

  it('shows move option in context menu', async () => {
    await renderAndWait();
    openContextMenuForFile('notes.txt');

    await waitFor(() => {
      expect(screen.getByText('Move')).toBeInTheDocument();
      expect(screen.getByText('Copy')).toBeInTheDocument();
    });
  });

  it('shows details option in context menu', async () => {
    await renderAndWait();
    openContextMenuForFile('notes.txt');

    await waitFor(() => {
      expect(screen.getByText('Details')).toBeInTheDocument();
    });
  });

  it('shows download option in context menu', async () => {
    await renderAndWait();
    openContextMenuForFile('photo.jpg');

    await waitFor(() => {
      expect(screen.getByText('Download')).toBeInTheDocument();
    });
  });
});
