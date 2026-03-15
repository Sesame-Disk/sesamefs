import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import TransferLibrarySheet from '../TransferLibrarySheet';

const mockSearchUsers = vi.fn();
const mockTransferRepo = vi.fn();

vi.mock('../../../lib/api', () => ({
  searchUsers: (...args: unknown[]) => mockSearchUsers(...args),
  transferRepo: (...args: unknown[]) => mockTransferRepo(...args),
}));

const mockRepo = {
  repo_id: 'repo-1',
  repo_name: 'My Documents',
  size: 1048576,
  permission: 'rw',
  owner_email: 'user@example.com',
  owner_name: 'Test User',
  encrypted: false,
  last_modified: '2025-12-01T10:00:00Z',
};

const mockUsers = [
  { email: 'alice@example.com', name: 'Alice', avatar_url: '/avatar/alice' },
  { email: 'bob@example.com', name: 'Bob', avatar_url: '/avatar/bob' },
];

describe('TransferLibrarySheet', () => {
  const onClose = vi.fn();
  const onTransferred = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows search input and library name', () => {
    render(<TransferLibrarySheet isOpen={true} onClose={onClose} repo={mockRepo} onTransferred={onTransferred} />);
    expect(screen.getByText('My Documents')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('Type a name or email...')).toBeInTheDocument();
  });

  it('searches users when typing', async () => {
    mockSearchUsers.mockResolvedValue(mockUsers);
    render(<TransferLibrarySheet isOpen={true} onClose={onClose} repo={mockRepo} onTransferred={onTransferred} />);
    fireEvent.change(screen.getByPlaceholderText('Type a name or email...'), { target: { value: 'ali' } });
    await waitFor(() => {
      expect(mockSearchUsers).toHaveBeenCalledWith('ali');
    });
    expect(screen.getByText('Alice')).toBeInTheDocument();
    expect(screen.getByText('Bob')).toBeInTheDocument();
  });

  it('selects user and calls transferRepo', async () => {
    mockSearchUsers.mockResolvedValue(mockUsers);
    mockTransferRepo.mockResolvedValue(undefined);
    render(<TransferLibrarySheet isOpen={true} onClose={onClose} repo={mockRepo} onTransferred={onTransferred} />);
    fireEvent.change(screen.getByPlaceholderText('Type a name or email...'), { target: { value: 'ali' } });
    await waitFor(() => {
      expect(screen.getByText('Alice')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText('Alice'));
    fireEvent.click(screen.getByText('Transfer to Alice'));
    await waitFor(() => {
      expect(mockTransferRepo).toHaveBeenCalledWith('repo-1', 'alice@example.com');
    });
    expect(onTransferred).toHaveBeenCalled();
  });

  it('shows disabled button when no user selected', () => {
    render(<TransferLibrarySheet isOpen={true} onClose={onClose} repo={mockRepo} onTransferred={onTransferred} />);
    const button = screen.getByText('Select a user');
    expect(button).toBeDisabled();
  });
});
