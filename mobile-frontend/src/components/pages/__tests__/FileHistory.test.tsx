import React from 'react';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import FileHistory from '../FileHistory';

const mockHistoryRecords = [
  {
    commit_id: 'commit-aaa',
    rev_file_id: 'rev-1',
    ctime: 1710500000,
    description: 'Updated formatting',
    creator_name: 'Alice',
    creator_email: 'alice@example.com',
    creator_contact_email: 'alice@example.com',
    creator_avatar_url: '/avatar/alice.png',
    path: '/docs/report.pdf',
    rev_file_size: 4096,
  },
  {
    commit_id: 'commit-bbb',
    rev_file_id: 'rev-2',
    ctime: 1710400000,
    description: 'Initial version',
    creator_name: 'Bob',
    creator_email: 'bob@example.com',
    creator_contact_email: 'bob@example.com',
    creator_avatar_url: '/avatar/bob.png',
    path: '/docs/report.pdf',
    rev_file_size: 2048,
  },
];

const mockListFileHistory = vi.fn();
const mockGetFileRevision = vi.fn();
const mockRevertFile = vi.fn();

vi.mock('../../../lib/api', () => ({
  listFileHistory: (...args: unknown[]) => mockListFileHistory(...args),
  getFileRevision: (...args: unknown[]) => mockGetFileRevision(...args),
  revertFile: (...args: unknown[]) => mockRevertFile(...args),
}));

vi.mock('../../../lib/models', () => ({
  bytesToSize: (bytes: number) => `${bytes} B`,
  formatDate: (ts: number) => new Date(ts * 1000).toISOString(),
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

vi.mock('../../ui/EmptyState', () => ({
  default: ({ title, description }: { title: string; description?: string }) => (
    <div data-testid="empty-state">
      <span>{title}</span>
      {description && <span>{description}</span>}
    </div>
  ),
}));

vi.mock('../../ui/BottomSheet', () => ({
  default: ({ isOpen, children, title }: { isOpen: boolean; children: React.ReactNode; title?: string }) =>
    isOpen ? <div data-testid="bottom-sheet" aria-label={title}>{children}</div> : null,
}));

describe('FileHistory', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows empty state when no repoId', () => {
    render(<FileHistory />);
    expect(screen.getByTestId('empty-state')).toBeInTheDocument();
    expect(screen.getByText('No file selected')).toBeInTheDocument();
  });

  it('shows loading state initially', () => {
    mockListFileHistory.mockReturnValue(new Promise(() => {}));
    render(<FileHistory repoId="repo-1" path="/docs/report.pdf" fileName="report.pdf" />);
    expect(screen.getByText('Loading history...')).toBeInTheDocument();
  });

  it('renders history records after loading', async () => {
    mockListFileHistory.mockResolvedValue({ data: mockHistoryRecords, total_count: 2 });
    render(<FileHistory repoId="repo-1" path="/docs/report.pdf" fileName="report.pdf" />);

    await waitFor(() => {
      expect(screen.getByTestId('history-list')).toBeInTheDocument();
    });

    expect(screen.getByText('Alice')).toBeInTheDocument();
    expect(screen.getByText('Bob')).toBeInTheDocument();
    expect(screen.getByText('4096 B')).toBeInTheDocument();
    expect(screen.getByText('2048 B')).toBeInTheDocument();
  });

  it('shows current badge on first record', async () => {
    mockListFileHistory.mockResolvedValue({ data: mockHistoryRecords, total_count: 2 });
    render(<FileHistory repoId="repo-1" path="/docs/report.pdf" fileName="report.pdf" />);

    await waitFor(() => {
      expect(screen.getByText('Current')).toBeInTheDocument();
    });
  });

  it('shows empty state when no history', async () => {
    mockListFileHistory.mockResolvedValue({ data: [], total_count: 0 });
    render(<FileHistory repoId="repo-1" path="/docs/report.pdf" fileName="report.pdf" />);

    await waitFor(() => {
      expect(screen.getByText('No history available')).toBeInTheDocument();
    });
  });

  it('shows error state on API failure', async () => {
    mockListFileHistory.mockRejectedValue(new Error('Network error'));
    render(<FileHistory repoId="repo-1" path="/docs/report.pdf" fileName="report.pdf" />);

    await waitFor(() => {
      expect(screen.getByTestId('error-message')).toHaveTextContent('Network error');
    });
  });

  it('does not show restore swipe action on current version', async () => {
    mockListFileHistory.mockResolvedValue({ data: mockHistoryRecords, total_count: 2 });
    render(<FileHistory repoId="repo-1" path="/docs/report.pdf" fileName="report.pdf" />);

    await waitFor(() => {
      expect(screen.getByTestId('history-list')).toBeInTheDocument();
    });

    const restoreButtons = screen.getAllByTestId('action-restore');
    // Only the second record (non-current) should have a restore action
    expect(restoreButtons).toHaveLength(1);
  });

  it('calls revert API on restore confirmation', async () => {
    mockListFileHistory.mockResolvedValue({ data: mockHistoryRecords, total_count: 2 });
    mockRevertFile.mockResolvedValue(undefined);
    render(<FileHistory repoId="repo-1" path="/docs/report.pdf" fileName="report.pdf" />);

    await waitFor(() => {
      expect(screen.getByTestId('history-list')).toBeInTheDocument();
    });

    // Click restore swipe action on the second item
    const restoreButton = screen.getByTestId('action-restore');
    fireEvent.click(restoreButton);

    // Confirmation dialog should appear
    await waitFor(() => {
      expect(screen.getByTestId('confirm-restore')).toBeInTheDocument();
    });

    // Click confirm
    fireEvent.click(screen.getByTestId('confirm-restore'));

    await waitFor(() => {
      expect(mockRevertFile).toHaveBeenCalledWith('repo-1', 'commit-bbb', '/docs/report.pdf');
    });
  });

  it('shows toast on successful restore', async () => {
    mockListFileHistory.mockResolvedValue({ data: mockHistoryRecords, total_count: 2 });
    mockRevertFile.mockResolvedValue(undefined);
    render(<FileHistory repoId="repo-1" path="/docs/report.pdf" fileName="report.pdf" />);

    await waitFor(() => {
      expect(screen.getByTestId('history-list')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('action-restore'));
    await waitFor(() => {
      expect(screen.getByTestId('confirm-restore')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('confirm-restore'));

    await waitFor(() => {
      expect(screen.getByTestId('toast')).toHaveTextContent('File restored successfully');
    });
  });

  it('shows file name and version count in header', async () => {
    mockListFileHistory.mockResolvedValue({ data: mockHistoryRecords, total_count: 2 });
    render(<FileHistory repoId="repo-1" path="/docs/report.pdf" fileName="report.pdf" />);

    await waitFor(() => {
      expect(screen.getByText('report.pdf')).toBeInTheDocument();
    });
    expect(screen.getByText('Version History (2)')).toBeInTheDocument();
  });

  it('calls listFileHistory with correct params', async () => {
    mockListFileHistory.mockResolvedValue({ data: [], total_count: 0 });
    render(<FileHistory repoId="repo-1" path="/docs/report.pdf" fileName="report.pdf" />);

    await waitFor(() => {
      expect(mockListFileHistory).toHaveBeenCalledWith('repo-1', '/docs/report.pdf', 1, 25);
    });
  });

  it('shows load more sentinel when hasMore', async () => {
    const manyRecords = Array.from({ length: 25 }, (_, i) => ({
      ...mockHistoryRecords[0],
      commit_id: `commit-${i}`,
      ctime: 1710500000 - i * 1000,
    }));
    mockListFileHistory.mockResolvedValue({ data: manyRecords, total_count: 50 });
    render(<FileHistory repoId="repo-1" path="/docs/report.pdf" fileName="report.pdf" />);

    await waitFor(() => {
      expect(screen.getByTestId('load-more-sentinel')).toBeInTheDocument();
    });
  });

  it('refreshes on refresh button click', async () => {
    mockListFileHistory.mockResolvedValue({ data: mockHistoryRecords, total_count: 2 });
    render(<FileHistory repoId="repo-1" path="/docs/report.pdf" fileName="report.pdf" />);

    await waitFor(() => {
      expect(screen.getByTestId('history-list')).toBeInTheDocument();
    });

    mockListFileHistory.mockClear();
    fireEvent.click(screen.getByTestId('refresh-button'));

    await waitFor(() => {
      expect(mockListFileHistory).toHaveBeenCalledWith('repo-1', '/docs/report.pdf', 1, 25);
    });
  });
});
