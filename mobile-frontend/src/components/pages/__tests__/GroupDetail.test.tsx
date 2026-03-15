import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import GroupDetail from '../GroupDetail';

const mockListGroups = vi.fn();
const mockListGroupRepos = vi.fn();
const mockListGroupMembers = vi.fn();
const mockGetAccountInfo = vi.fn();
const mockAddGroupMembers = vi.fn();
const mockDeleteGroupMember = vi.fn();
const mockSetGroupAdmin = vi.fn();
const mockSearchUsers = vi.fn();

vi.mock('../../../lib/api', () => ({
  listGroups: (...args: unknown[]) => mockListGroups(...args),
  listGroupRepos: (...args: unknown[]) => mockListGroupRepos(...args),
  listGroupMembers: (...args: unknown[]) => mockListGroupMembers(...args),
  getAccountInfo: (...args: unknown[]) => mockGetAccountInfo(...args),
  addGroupMembers: (...args: unknown[]) => mockAddGroupMembers(...args),
  deleteGroupMember: (...args: unknown[]) => mockDeleteGroupMember(...args),
  setGroupAdmin: (...args: unknown[]) => mockSetGroupAdmin(...args),
  searchUsers: (...args: unknown[]) => mockSearchUsers(...args),
}));

const groups = [
  { id: 1, name: 'Test Group', owner: 'owner@test.com', created_at: '2025-01-01', member_count: 3 },
];

const members = [
  { email: 'owner@test.com', name: 'Owner', role: 'owner', avatar_url: '' },
  { email: 'admin@test.com', name: 'Admin User', role: 'admin', avatar_url: '' },
  { email: 'member@test.com', name: 'Regular Member', role: 'member', avatar_url: '' },
];

beforeEach(() => {
  vi.clearAllMocks();
  mockListGroups.mockResolvedValue(groups);
  mockListGroupRepos.mockResolvedValue([]);
  mockListGroupMembers.mockResolvedValue(members);
  mockGetAccountInfo.mockResolvedValue({
    email: 'owner@test.com', name: 'Owner', usage: 0, total: 1000,
    login_id: '', institution: '', is_staff: false, avatar_url: '',
  });
});

describe('GroupDetail', () => {
  it('renders group info and members', async () => {
    render(<GroupDetail groupId="1" />);
    await waitFor(() => {
      expect(screen.getByText('Test Group')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText(/Members/));
    await waitFor(() => {
      expect(screen.getByText('owner@test.com')).toBeInTheDocument();
      expect(screen.getByText('Admin User')).toBeInTheDocument();
      expect(screen.getByText('Regular Member')).toBeInTheDocument();
    });
  });

  it('shows role badges', async () => {
    render(<GroupDetail groupId="1" />);
    await waitFor(() => {
      expect(screen.getByText('Test Group')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText(/Members/));
    await waitFor(() => {
      expect(screen.getAllByText('Owner').length).toBeGreaterThanOrEqual(1);
      expect(screen.getByText('Admin')).toBeInTheDocument();
      expect(screen.getByText('Member')).toBeInTheDocument();
    });
  });

  it('shows Add Member button for owner on members tab', async () => {
    render(<GroupDetail groupId="1" />);
    await waitFor(() => {
      expect(screen.getByText('Test Group')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText(/Members/));
    await waitFor(() => {
      expect(screen.getByLabelText('Add Member')).toBeInTheDocument();
    });
  });

  it('hides management actions for regular members', async () => {
    mockGetAccountInfo.mockResolvedValue({
      email: 'member@test.com', name: 'Regular Member', usage: 0, total: 1000,
      login_id: '', institution: '', is_staff: false, avatar_url: '',
    });

    render(<GroupDetail groupId="1" />);
    await waitFor(() => {
      expect(screen.getByText('Test Group')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText(/Members/));
    await waitFor(() => {
      expect(screen.queryByLabelText('Add Member')).not.toBeInTheDocument();
      expect(screen.queryByLabelText('Remove Member')).not.toBeInTheDocument();
    });
  });

  it('shows remove button for non-owner members when user is owner', async () => {
    render(<GroupDetail groupId="1" />);
    await waitFor(() => {
      expect(screen.getByText('Test Group')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText(/Members/));
    await waitFor(() => {
      const removeButtons = screen.getAllByLabelText('Remove Member');
      expect(removeButtons.length).toBe(2); // admin + member, not owner
    });
  });

  it('shows confirm dialog when clicking remove member', async () => {
    render(<GroupDetail groupId="1" />);
    await waitFor(() => {
      expect(screen.getByText('Test Group')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText(/Members/));
    await waitFor(() => {
      const removeButtons = screen.getAllByLabelText('Remove Member');
      fireEvent.click(removeButtons[0]);
    });
    await waitFor(() => {
      expect(screen.getByText('Remove Member')).toBeInTheDocument();
      expect(screen.getByText('Cancel')).toBeInTheDocument();
      expect(screen.getByText('Remove')).toBeInTheDocument();
    });
  });

  it('shows no group selected message without groupId', () => {
    render(<GroupDetail />);
    expect(screen.getByText('No group selected')).toBeInTheDocument();
  });
});
