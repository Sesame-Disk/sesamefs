import React from 'react';
import { render, screen, waitFor, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import ActivityFeed from '../ActivityFeed';

const mockActivities = [
  {
    op_type: 'create' as const,
    repo_id: 'repo-1',
    repo_name: 'My Library',
    obj_type: 'file',
    commit_id: 'commit-1',
    name: 'document.pdf',
    path: '/document.pdf',
    author_email: 'alice@example.com',
    author_name: 'Alice',
    author_contact_email: 'alice@example.com',
    avatar_url: '',
    time: '2026-03-11T10:00:00Z',
  },
  {
    op_type: 'edit' as const,
    repo_id: 'repo-1',
    repo_name: 'My Library',
    obj_type: 'file',
    commit_id: 'commit-2',
    name: 'notes.md',
    path: '/notes.md',
    author_email: 'bob@example.com',
    author_name: 'Bob',
    author_contact_email: 'bob@example.com',
    avatar_url: '',
    time: '2026-03-11T09:00:00Z',
  },
  {
    op_type: 'delete' as const,
    repo_id: 'repo-2',
    repo_name: 'Shared Docs',
    obj_type: 'file',
    commit_id: 'commit-3',
    name: 'old-file.txt',
    path: '/old-file.txt',
    author_email: 'alice@example.com',
    author_name: 'Alice',
    author_contact_email: 'alice@example.com',
    avatar_url: '',
    time: '2026-03-10T15:00:00Z',
  },
];

vi.mock('../../../lib/api', () => ({
  listActivities: vi.fn(),
}));

// Mock framer-motion to avoid animation issues in tests
vi.mock('framer-motion', () => ({
  motion: {
    div: React.forwardRef(({ children, animate, transition, ...props }: any, ref: any) =>
      React.createElement('div', { ...props, ref }, children)
    ),
  },
  AnimatePresence: ({ children }: any) => children,
}));

import { listActivities } from '../../../lib/api';

const mockListActivities = vi.mocked(listActivities);

// Mock IntersectionObserver
const mockObserve = vi.fn();
const mockDisconnect = vi.fn();
let intersectionCallback: (entries: Array<{ isIntersecting: boolean }>) => void;

beforeEach(() => {
  vi.clearAllMocks();
  const MockIntersectionObserver = vi.fn().mockImplementation((callback: any) => {
    intersectionCallback = callback;
    return {
      observe: mockObserve,
      disconnect: mockDisconnect,
      unobserve: vi.fn(),
    };
  });
  vi.stubGlobal('IntersectionObserver', MockIntersectionObserver);
});

describe('ActivityFeed', () => {
  it('shows loading state initially', () => {
    mockListActivities.mockReturnValue(new Promise(() => {})); // never resolves
    render(<ActivityFeed />);
    expect(screen.getByTestId('loading')).toBeInTheDocument();
  });

  it('renders activity items after loading', async () => {
    mockListActivities.mockResolvedValue({ events: mockActivities, more: false });
    render(<ActivityFeed />);

    await waitFor(() => {
      expect(screen.getByTestId('activity-feed')).toBeInTheDocument();
    });

    const items = screen.getAllByTestId('activity-item');
    expect(items).toHaveLength(3);
  });

  it('shows empty state when no activities', async () => {
    mockListActivities.mockResolvedValue({ events: [], more: false });
    render(<ActivityFeed />);

    await waitFor(() => {
      expect(screen.getByText('No recent activity')).toBeInTheDocument();
    });
  });

  it('shows day dividers between different days', async () => {
    mockListActivities.mockResolvedValue({ events: mockActivities, more: false });
    render(<ActivityFeed />);

    await waitFor(() => {
      expect(screen.getByTestId('activity-feed')).toBeInTheDocument();
    });

    const dividers = screen.getAllByTestId('day-divider');
    // Activities span 2 days (March 11 and March 10), so 2 dividers
    expect(dividers.length).toBe(2);
  });

  it('renders file links for non-delete actions', async () => {
    mockListActivities.mockResolvedValue({ events: mockActivities, more: false });
    render(<ActivityFeed />);

    await waitFor(() => {
      expect(screen.getByTestId('activity-feed')).toBeInTheDocument();
    });

    const links = screen.getAllByTestId('activity-file-link');
    // create and edit actions have links, delete does not
    expect(links).toHaveLength(2);
  });

  it('renders scroll sentinel for infinite scroll', async () => {
    mockListActivities.mockResolvedValue({ events: mockActivities, more: true });
    render(<ActivityFeed />);

    await waitFor(() => {
      expect(screen.getByTestId('activity-feed')).toBeInTheDocument();
    });

    expect(screen.getByTestId('scroll-sentinel')).toBeInTheDocument();
  });

  it('calls listActivities with page 1 on mount', async () => {
    mockListActivities.mockResolvedValue({ events: mockActivities, more: false });
    render(<ActivityFeed />);

    await waitFor(() => {
      expect(screen.getByTestId('activity-feed')).toBeInTheDocument();
    });

    expect(mockListActivities).toHaveBeenCalledWith(1);
  });
});
