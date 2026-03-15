import React from 'react';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import LibraryList from '../LibraryList';

const mockRepos = [
  {
    repo_id: 'repo-1',
    repo_name: 'My Documents',
    size: 1048576,
    permission: 'rw',
    owner_email: 'user@example.com',
    owner_name: 'Test User',
    encrypted: false,
    last_modified: '2025-12-01T10:00:00Z',
  },
  {
    repo_id: 'repo-2',
    repo_name: 'Archive',
    size: 5242880,
    permission: 'rw',
    owner_email: 'user@example.com',
    owner_name: 'Test User',
    encrypted: true,
    last_modified: '2025-11-15T08:30:00Z',
  },
];

const mockListRepos = vi.fn();
const mockCreateRepo = vi.fn();
const mockGetAccountInfo = vi.fn();
const mockLeaveShareRepo = vi.fn();
const mockRenameRepo = vi.fn();
const mockDeleteRepo = vi.fn();
const mockTransferRepo = vi.fn();
const mockSearchUsers = vi.fn();

vi.mock('../../../lib/api', () => ({
  listRepos: (...args: unknown[]) => mockListRepos(...args),
  createRepo: (...args: unknown[]) => mockCreateRepo(...args),
  getAccountInfo: (...args: unknown[]) => mockGetAccountInfo(...args),
  leaveShareRepo: (...args: unknown[]) => mockLeaveShareRepo(...args),
  renameRepo: (...args: unknown[]) => mockRenameRepo(...args),
  deleteRepo: (...args: unknown[]) => mockDeleteRepo(...args),
  transferRepo: (...args: unknown[]) => mockTransferRepo(...args),
  searchUsers: (...args: unknown[]) => mockSearchUsers(...args),
}));

vi.mock('../../../lib/sortPreference', () => ({
  getSortPreference: () => ({ field: 'name', direction: 'asc' }),
  setSortPreference: vi.fn(),
}));

describe('LibraryList', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetAccountInfo.mockResolvedValue({ email: 'user@example.com', name: 'Test User' });
  });

  it('shows loading skeleton initially', () => {
    mockListRepos.mockReturnValue(new Promise(() => {}));
    render(<LibraryList />);
    expect(screen.getByTestId('skeleton-list')).toBeInTheDocument();
  });

  it('renders libraries after loading', async () => {
    mockListRepos.mockResolvedValue(mockRepos);
    render(<LibraryList />);
    await waitFor(() => {
      expect(screen.getByText('My Documents')).toBeInTheDocument();
    });
    expect(screen.getByText('Archive')).toBeInTheDocument();
  });

  it('shows empty state when no libraries', async () => {
    mockListRepos.mockResolvedValue([]);
    render(<LibraryList />);
    await waitFor(() => {
      expect(screen.getByText('No libraries yet')).toBeInTheDocument();
    });
    expect(screen.getByText('Create a library')).toBeInTheDocument();
  });

  it('shows error state with retry button', async () => {
    mockListRepos.mockRejectedValue(new Error('Network error'));
    render(<LibraryList />);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Network error');
    });
    expect(screen.getByText('Retry')).toBeInTheDocument();
  });

  it('retries loading on retry button click', async () => {
    mockListRepos.mockRejectedValueOnce(new Error('Network error'));
    render(<LibraryList />);
    await waitFor(() => {
      expect(screen.getByText('Retry')).toBeInTheDocument();
    });

    mockListRepos.mockResolvedValue(mockRepos);
    fireEvent.click(screen.getByText('Retry'));
    await waitFor(() => {
      expect(screen.getByText('My Documents')).toBeInTheDocument();
    });
  });

  it('shows sort button when repos are loaded', async () => {
    mockListRepos.mockResolvedValue(mockRepos);
    render(<LibraryList />);
    await waitFor(() => {
      expect(screen.getByTestId('sort-button')).toBeInTheDocument();
    });
  });

  it('shows FAB toggle button', async () => {
    mockListRepos.mockResolvedValue(mockRepos);
    render(<LibraryList />);
    await waitFor(() => {
      expect(screen.getByTestId('fab-toggle')).toBeInTheDocument();
    });
  });

  it('renders page title', async () => {
    mockListRepos.mockResolvedValue([]);
    render(<LibraryList />);
    await waitFor(() => {
      expect(screen.getByText('My Libraries')).toBeInTheDocument();
    });
  });
});
