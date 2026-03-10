import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import UserPicker from '../UserPicker';

vi.mock('../../../lib/api', () => ({
  searchUsers: vi.fn().mockResolvedValue([
    { email: 'alice@example.com', name: 'Alice Smith', avatar_url: '/avatar1.png' },
    { email: 'bob@example.com', name: 'Bob Jones', avatar_url: '/avatar2.png' },
  ]),
}));

const defaultProps = {
  selectedUsers: [],
  onSelect: vi.fn(),
  onRemove: vi.fn(),
};

describe('UserPicker', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders search input', () => {
    render(<UserPicker {...defaultProps} />);
    expect(screen.getByLabelText('Search users')).toBeInTheDocument();
  });

  it('shows search results after typing', async () => {
    render(<UserPicker {...defaultProps} />);
    const input = screen.getByLabelText('Search users');
    fireEvent.change(input, { target: { value: 'alice' } });

    await waitFor(() => {
      expect(screen.getByText('Alice Smith')).toBeInTheDocument();
    }, { timeout: 1000 });
  });

  it('calls onSelect when user is clicked', async () => {
    const onSelect = vi.fn();
    render(<UserPicker {...defaultProps} onSelect={onSelect} />);

    const input = screen.getByLabelText('Search users');
    fireEvent.change(input, { target: { value: 'alice' } });

    await waitFor(() => {
      expect(screen.getByText('Alice Smith')).toBeInTheDocument();
    }, { timeout: 1000 });

    fireEvent.click(screen.getByText('Alice Smith'));
    expect(onSelect).toHaveBeenCalledWith({
      email: 'alice@example.com',
      name: 'Alice Smith',
      avatar_url: '/avatar1.png',
    });
  });

  it('renders selected users as chips', () => {
    const selectedUsers = [
      { email: 'alice@example.com', name: 'Alice Smith', avatar_url: '/avatar1.png' },
    ];
    render(<UserPicker {...defaultProps} selectedUsers={selectedUsers} />);
    expect(screen.getByText('Alice Smith')).toBeInTheDocument();
    expect(screen.getByTestId('selected-chips')).toBeInTheDocument();
  });

  it('calls onRemove when chip remove button is clicked', () => {
    const onRemove = vi.fn();
    const selectedUsers = [
      { email: 'alice@example.com', name: 'Alice Smith', avatar_url: '/avatar1.png' },
    ];
    render(<UserPicker {...defaultProps} selectedUsers={selectedUsers} onRemove={onRemove} />);
    fireEvent.click(screen.getByLabelText('Remove Alice Smith'));
    expect(onRemove).toHaveBeenCalledWith('alice@example.com');
  });

  it('does not show results for short queries', async () => {
    render(<UserPicker {...defaultProps} />);
    const input = screen.getByLabelText('Search users');
    fireEvent.change(input, { target: { value: 'a' } });

    // Wait a bit and confirm no results appear
    await new Promise(r => setTimeout(r, 500));
    expect(screen.queryByText('Alice Smith')).not.toBeInTheDocument();
  });
});
