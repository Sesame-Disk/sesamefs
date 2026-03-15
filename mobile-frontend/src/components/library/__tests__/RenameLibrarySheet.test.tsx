import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import RenameLibrarySheet from '../RenameLibrarySheet';

const mockRenameRepo = vi.fn();

vi.mock('../../../lib/api', () => ({
  renameRepo: (...args: unknown[]) => mockRenameRepo(...args),
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

describe('RenameLibrarySheet', () => {
  const onClose = vi.fn();
  const onRenamed = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('pre-fills input with current name', () => {
    render(<RenameLibrarySheet isOpen={true} onClose={onClose} repo={mockRepo} onRenamed={onRenamed} />);
    expect(screen.getByDisplayValue('My Documents')).toBeInTheDocument();
  });

  it('shows error when name is empty', async () => {
    render(<RenameLibrarySheet isOpen={true} onClose={onClose} repo={mockRepo} onRenamed={onRenamed} />);
    fireEvent.change(screen.getByDisplayValue('My Documents'), { target: { value: '' } });
    fireEvent.click(screen.getByText('Rename'));
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Name cannot be empty');
    });
  });

  it('shows error when name is unchanged', async () => {
    render(<RenameLibrarySheet isOpen={true} onClose={onClose} repo={mockRepo} onRenamed={onRenamed} />);
    fireEvent.click(screen.getByText('Rename'));
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Name must be different');
    });
  });

  it('calls renameRepo API on valid submit', async () => {
    mockRenameRepo.mockResolvedValue(undefined);
    render(<RenameLibrarySheet isOpen={true} onClose={onClose} repo={mockRepo} onRenamed={onRenamed} />);
    fireEvent.change(screen.getByDisplayValue('My Documents'), { target: { value: 'New Name' } });
    fireEvent.click(screen.getByText('Rename'));
    await waitFor(() => {
      expect(mockRenameRepo).toHaveBeenCalledWith('repo-1', 'New Name');
    });
    expect(onRenamed).toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  it('shows error on API failure', async () => {
    mockRenameRepo.mockRejectedValue(new Error('Failed to rename library'));
    render(<RenameLibrarySheet isOpen={true} onClose={onClose} repo={mockRepo} onRenamed={onRenamed} />);
    fireEvent.change(screen.getByDisplayValue('My Documents'), { target: { value: 'New Name' } });
    fireEvent.click(screen.getByText('Rename'));
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Failed to rename library');
    });
  });
});
