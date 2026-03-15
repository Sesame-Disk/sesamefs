import React from 'react';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import TrashPage from '../TrashPage';

const mockTrashItems = [
  {
    obj_name: 'document.pdf',
    obj_id: 'abc123',
    parent_dir: '/docs/',
    size: 2048,
    deleted_time: '2026-03-10T10:00:00Z',
    commit_id: 'commit-1',
    is_dir: false,
  },
  {
    obj_name: 'old-folder',
    obj_id: 'def456',
    parent_dir: '/',
    size: 0,
    deleted_time: '2026-03-09T08:00:00Z',
    commit_id: 'commit-2',
    is_dir: true,
  },
];

const mockListTrash = vi.fn();
const mockRestoreTrashItem = vi.fn();
const mockCleanTrash = vi.fn();

vi.mock('../../../lib/api', () => ({
  listTrash: (...args: unknown[]) => mockListTrash(...args),
  restoreTrashItem: (...args: unknown[]) => mockRestoreTrashItem(...args),
  cleanTrash: (...args: unknown[]) => mockCleanTrash(...args),
}));

vi.mock('../../../lib/models', () => ({
  bytesToSize: (bytes: number) => `${bytes} B`,
}));

vi.mock('../../ui/SwipeableListItem', () => ({
  default: ({ children, rightActions }: { children: React.ReactNode; rightActions?: { onClick: () => void; label: string }[] }) => (
    <div data-testid="swipeable-item">
      {children}
      {rightActions?.map(a => (
        <button key={a.label} onClick={a.onClick} data-testid={`action-${a.label.toLowerCase()}`}>
          {a.label}
        </button>
      ))}
    </div>
  ),
}));

describe('TrashPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows loading skeleton initially', () => {
    mockListTrash.mockReturnValue(new Promise(() => {}));
    render(<TrashPage repoId="repo-1" repoName="My Library" />);
    expect(screen.getByTestId('trash-skeleton')).toBeInTheDocument();
  });

  it('renders trash items after loading', async () => {
    mockListTrash.mockResolvedValue({ data: mockTrashItems, more: false, scan_stat: null });
    render(<TrashPage repoId="repo-1" repoName="My Library" />);
    await waitFor(() => {
      expect(screen.getByText('document.pdf')).toBeInTheDocument();
    });
    expect(screen.getByText('old-folder')).toBeInTheDocument();
  });

  it('shows empty state when no trash items', async () => {
    mockListTrash.mockResolvedValue({ data: [], more: false, scan_stat: null });
    render(<TrashPage repoId="repo-1" repoName="My Library" />);
    await waitFor(() => {
      expect(screen.getByText('No deleted items')).toBeInTheDocument();
    });
  });

  it('shows error state with retry', async () => {
    mockListTrash.mockRejectedValue(new Error('Network error'));
    render(<TrashPage repoId="repo-1" repoName="My Library" />);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Network error');
    });
    expect(screen.getByText('Retry')).toBeInTheDocument();
  });

  it('retries loading on retry button click', async () => {
    mockListTrash.mockRejectedValueOnce(new Error('Network error'));
    render(<TrashPage repoId="repo-1" repoName="My Library" />);
    await waitFor(() => {
      expect(screen.getByText('Retry')).toBeInTheDocument();
    });

    mockListTrash.mockResolvedValue({ data: mockTrashItems, more: false, scan_stat: null });
    fireEvent.click(screen.getByText('Retry'));
    await waitFor(() => {
      expect(screen.getByText('document.pdf')).toBeInTheDocument();
    });
  });

  it('calls restore API when restore action is triggered', async () => {
    mockListTrash.mockResolvedValue({ data: mockTrashItems, more: false, scan_stat: null });
    mockRestoreTrashItem.mockResolvedValue(undefined);
    render(<TrashPage repoId="repo-1" repoName="My Library" />);

    await waitFor(() => {
      expect(screen.getByText('document.pdf')).toBeInTheDocument();
    });

    const restoreButtons = screen.getAllByTestId('action-restore');
    fireEvent.click(restoreButtons[0]);

    await waitFor(() => {
      expect(mockRestoreTrashItem).toHaveBeenCalledWith(
        'repo-1',
        'commit-1',
        '/docs/document.pdf',
        false,
      );
    });
  });

  it('shows confirmation dialog for clean trash', async () => {
    mockListTrash.mockResolvedValue({ data: mockTrashItems, more: false, scan_stat: null });
    render(<TrashPage repoId="repo-1" repoName="My Library" />);

    await waitFor(() => {
      expect(screen.getByTestId('clean-trash-btn')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('clean-trash-btn'));
    expect(screen.getByTestId('clean-confirm-dialog')).toBeInTheDocument();
    expect(screen.getByText('Delete All')).toBeInTheDocument();
  });

  it('calls clean trash API after confirmation', async () => {
    mockListTrash.mockResolvedValue({ data: mockTrashItems, more: false, scan_stat: null });
    mockCleanTrash.mockResolvedValue(undefined);
    render(<TrashPage repoId="repo-1" repoName="My Library" />);

    await waitFor(() => {
      expect(screen.getByTestId('clean-trash-btn')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('clean-trash-btn'));
    fireEvent.click(screen.getByTestId('confirm-clean-btn'));

    await waitFor(() => {
      expect(mockCleanTrash).toHaveBeenCalledWith('repo-1');
    });
  });

  it('cancels clean trash dialog', async () => {
    mockListTrash.mockResolvedValue({ data: mockTrashItems, more: false, scan_stat: null });
    render(<TrashPage repoId="repo-1" repoName="My Library" />);

    await waitFor(() => {
      expect(screen.getByTestId('clean-trash-btn')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('clean-trash-btn'));
    expect(screen.getByTestId('clean-confirm-dialog')).toBeInTheDocument();

    fireEvent.click(screen.getByText('Cancel'));
    expect(screen.queryByTestId('clean-confirm-dialog')).not.toBeInTheDocument();
  });

  it('displays no library selected when repoId is missing', () => {
    render(<TrashPage />);
    expect(screen.getByText('No library selected')).toBeInTheDocument();
  });
});
