import React from 'react';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import SharedLibraries from '../SharedLibraries';

const mockSharedWithMe = [
  {
    repo_id: 'repo-1',
    repo_name: 'Team Docs',
    repo_desc: '',
    permission: 'rw',
    share_type: 'personal',
    user: 'alice@example.com',
    last_modified: 1700000000,
    is_virtual: false,
    encrypted: 0,
  },
  {
    repo_id: 'repo-2',
    repo_name: 'Project Files',
    repo_desc: '',
    permission: 'r',
    share_type: 'personal',
    user: 'bob@example.com',
    last_modified: 1700100000,
    is_virtual: false,
    encrypted: 1,
  },
];

const mockSharedByMe = [
  {
    repo_id: 'repo-3',
    repo_name: 'My Shared Lib',
    repo_desc: '',
    permission: 'r',
    share_type: 'personal',
    user: 'charlie@example.com',
    last_modified: 1700200000,
    is_virtual: false,
    encrypted: 0,
  },
];

const mockListBeSharedRepos = vi.fn();
const mockListSharedRepos = vi.fn();

vi.mock('../../../lib/api', () => ({
  listBeSharedRepos: (...args: unknown[]) => mockListBeSharedRepos(...args),
  listSharedRepos: (...args: unknown[]) => mockListSharedRepos(...args),
}));

describe('SharedLibraries', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows loading skeleton initially', () => {
    mockListBeSharedRepos.mockReturnValue(new Promise(() => {}));
    render(<SharedLibraries />);
    expect(screen.getByTestId('skeleton-list')).toBeInTheDocument();
  });

  it('renders shared-with-me repos after loading', async () => {
    mockListBeSharedRepos.mockResolvedValue(mockSharedWithMe);
    render(<SharedLibraries />);
    await waitFor(() => {
      expect(screen.getByText('Team Docs')).toBeInTheDocument();
    });
    expect(screen.getByText('Project Files')).toBeInTheDocument();
    expect(screen.getByText('From alice@example.com')).toBeInTheDocument();
    expect(screen.getByText('Read-Write')).toBeInTheDocument();
    expect(screen.getByText('Read-Only')).toBeInTheDocument();
  });

  it('shows empty state for shared-with-me tab', async () => {
    mockListBeSharedRepos.mockResolvedValue([]);
    render(<SharedLibraries />);
    await waitFor(() => {
      expect(screen.getByText('No libraries shared with you')).toBeInTheDocument();
    });
  });

  it('switches to shared-by-me tab and loads data', async () => {
    mockListBeSharedRepos.mockResolvedValue(mockSharedWithMe);
    mockListSharedRepos.mockResolvedValue(mockSharedByMe);
    render(<SharedLibraries />);
    await waitFor(() => {
      expect(screen.getByText('Team Docs')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Shared by me'));
    await waitFor(() => {
      expect(screen.getByText('My Shared Lib')).toBeInTheDocument();
    });
    expect(screen.getByText('To charlie@example.com')).toBeInTheDocument();
  });

  it('shows empty state for shared-by-me tab', async () => {
    mockListBeSharedRepos.mockResolvedValue(mockSharedWithMe);
    mockListSharedRepos.mockResolvedValue([]);
    render(<SharedLibraries />);
    await waitFor(() => {
      expect(screen.getByText('Team Docs')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Shared by me'));
    await waitFor(() => {
      expect(screen.getByText("You haven't shared any libraries")).toBeInTheDocument();
    });
  });

  it('shows error state with retry button', async () => {
    mockListBeSharedRepos.mockRejectedValue(new Error('Network error'));
    render(<SharedLibraries />);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Network error');
    });
    expect(screen.getByText('Retry')).toBeInTheDocument();
  });

  it('retries loading on retry button click', async () => {
    mockListBeSharedRepos.mockRejectedValueOnce(new Error('Network error'));
    render(<SharedLibraries />);
    await waitFor(() => {
      expect(screen.getByText('Retry')).toBeInTheDocument();
    });

    mockListBeSharedRepos.mockResolvedValue(mockSharedWithMe);
    fireEvent.click(screen.getByText('Retry'));
    await waitFor(() => {
      expect(screen.getByText('Team Docs')).toBeInTheDocument();
    });
  });

  it('renders page title and tabs', async () => {
    mockListBeSharedRepos.mockResolvedValue([]);
    render(<SharedLibraries />);
    await waitFor(() => {
      expect(screen.getByText('Shared Libraries')).toBeInTheDocument();
    });
    expect(screen.getByText('Shared with me')).toBeInTheDocument();
    expect(screen.getByText('Shared by me')).toBeInTheDocument();
  });

  it('shows refresh button', async () => {
    mockListBeSharedRepos.mockResolvedValue(mockSharedWithMe);
    render(<SharedLibraries />);
    await waitFor(() => {
      expect(screen.getByText('Refresh')).toBeInTheDocument();
    });
  });
});
