import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import DeleteLibrarySheet from '../DeleteLibrarySheet';

const mockDeleteRepo = vi.fn();

vi.mock('../../../lib/api', () => ({
  deleteRepo: (...args: unknown[]) => mockDeleteRepo(...args),
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

describe('DeleteLibrarySheet', () => {
  const onClose = vi.fn();
  const onDeleted = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows library name and warning', () => {
    render(<DeleteLibrarySheet isOpen={true} onClose={onClose} repo={mockRepo} onDeleted={onDeleted} />);
    expect(screen.getByText('My Documents')).toBeInTheDocument();
    expect(screen.getByText(/permanent and cannot be undone/)).toBeInTheDocument();
  });

  it('shows cancel and delete buttons', () => {
    render(<DeleteLibrarySheet isOpen={true} onClose={onClose} repo={mockRepo} onDeleted={onDeleted} />);
    expect(screen.getByText('Cancel')).toBeInTheDocument();
    expect(screen.getByText('Delete')).toBeInTheDocument();
  });

  it('calls onClose when cancel is clicked', () => {
    render(<DeleteLibrarySheet isOpen={true} onClose={onClose} repo={mockRepo} onDeleted={onDeleted} />);
    fireEvent.click(screen.getByText('Cancel'));
    expect(onClose).toHaveBeenCalled();
  });

  it('calls deleteRepo API on confirm', async () => {
    mockDeleteRepo.mockResolvedValue(undefined);
    render(<DeleteLibrarySheet isOpen={true} onClose={onClose} repo={mockRepo} onDeleted={onDeleted} />);
    fireEvent.click(screen.getByText('Delete'));
    await waitFor(() => {
      expect(mockDeleteRepo).toHaveBeenCalledWith('repo-1');
    });
    expect(onDeleted).toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  it('shows error on API failure', async () => {
    mockDeleteRepo.mockRejectedValue(new Error('Failed to delete library'));
    render(<DeleteLibrarySheet isOpen={true} onClose={onClose} repo={mockRepo} onDeleted={onDeleted} />);
    fireEvent.click(screen.getByText('Delete'));
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Failed to delete library');
    });
  });
});
