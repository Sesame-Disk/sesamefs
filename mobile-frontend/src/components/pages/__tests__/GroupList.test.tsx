import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import GroupList from '../GroupList';

const mockListGroups = vi.fn();
const mockListGroupMembers = vi.fn();
const mockGetAccountInfo = vi.fn();
const mockRenameGroup = vi.fn();
const mockDeleteGroup = vi.fn();
const mockTransferGroup = vi.fn();
const mockQuitGroup = vi.fn();
const mockCreateGroup = vi.fn();

vi.mock('../../../lib/api', () => ({
  listGroups: (...args: unknown[]) => mockListGroups(...args),
  listGroupMembers: (...args: unknown[]) => mockListGroupMembers(...args),
  getAccountInfo: (...args: unknown[]) => mockGetAccountInfo(...args),
  renameGroup: (...args: unknown[]) => mockRenameGroup(...args),
  deleteGroup: (...args: unknown[]) => mockDeleteGroup(...args),
  transferGroup: (...args: unknown[]) => mockTransferGroup(...args),
  quitGroup: (...args: unknown[]) => mockQuitGroup(...args),
  createGroup: (...args: unknown[]) => mockCreateGroup(...args),
}));

const groups = [
  { id: 1, name: 'My Group', owner: 'me@test.com', created_at: '2025-01-01', member_count: 3 },
  { id: 2, name: 'Other Group', owner: 'other@test.com', created_at: '2025-01-01', member_count: 5 },
];

beforeEach(() => {
  vi.clearAllMocks();
  mockListGroups.mockResolvedValue(groups);
  mockGetAccountInfo.mockResolvedValue({
    email: 'me@test.com', name: 'Me', usage: 0, total: 1000,
    login_id: '', institution: '', is_staff: false, avatar_url: '',
  });
  mockListGroupMembers.mockResolvedValue([]);
});

describe('GroupList', () => {
  it('renders groups after loading', async () => {
    render(<GroupList />);
    await waitFor(() => {
      expect(screen.getByText('My Group')).toBeInTheDocument();
      expect(screen.getByText('Other Group')).toBeInTheDocument();
    });
  });

  it('shows error state', async () => {
    mockListGroups.mockRejectedValueOnce(new Error('Network error'));
    render(<GroupList />);
    await waitFor(() => {
      expect(screen.getByText('Network error')).toBeInTheDocument();
    });
  });

  it('shows empty state when no groups', async () => {
    mockListGroups.mockResolvedValueOnce([]);
    render(<GroupList />);
    await waitFor(() => {
      expect(screen.getByText('No groups')).toBeInTheDocument();
    });
  });

  it('opens create group dialog on FAB click', async () => {
    render(<GroupList />);
    await waitFor(() => {
      expect(screen.getByText('My Group')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByLabelText('Create New Group'));
    expect(screen.getByText('Create New Group')).toBeInTheDocument();
  });
});
