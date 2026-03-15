import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import GroupContextMenu from '../GroupContextMenu';

const mockGroup = {
  id: 1,
  name: 'Test Group',
  owner: 'owner@test.com',
  created_at: '2025-01-01',
  member_count: 5,
};

const defaultHandlers = {
  onOpen: vi.fn(),
  onRename: vi.fn(),
  onTransfer: vi.fn(),
  onDelete: vi.fn(),
  onLeave: vi.fn(),
};

describe('GroupContextMenu', () => {
  it('shows owner actions when isOwner is true', () => {
    render(
      <GroupContextMenu
        isOpen={true}
        onClose={vi.fn()}
        group={mockGroup}
        isOwner={true}
        isAdmin={false}
        {...defaultHandlers}
      />
    );
    expect(screen.getByText('Open')).toBeInTheDocument();
    expect(screen.getByText('Rename')).toBeInTheDocument();
    expect(screen.getByText('Transfer')).toBeInTheDocument();
    expect(screen.getByText('Delete')).toBeInTheDocument();
    expect(screen.queryByText('Leave')).not.toBeInTheDocument();
  });

  it('shows member actions when not owner', () => {
    render(
      <GroupContextMenu
        isOpen={true}
        onClose={vi.fn()}
        group={mockGroup}
        isOwner={false}
        isAdmin={false}
        {...defaultHandlers}
      />
    );
    expect(screen.getByText('Open')).toBeInTheDocument();
    expect(screen.getByText('Leave')).toBeInTheDocument();
    expect(screen.queryByText('Rename')).not.toBeInTheDocument();
    expect(screen.queryByText('Transfer')).not.toBeInTheDocument();
    expect(screen.queryByText('Delete')).not.toBeInTheDocument();
  });

  it('shows rename for admin but not transfer/delete', () => {
    render(
      <GroupContextMenu
        isOpen={true}
        onClose={vi.fn()}
        group={mockGroup}
        isOwner={false}
        isAdmin={true}
        {...defaultHandlers}
      />
    );
    expect(screen.getByText('Rename')).toBeInTheDocument();
    expect(screen.getByText('Leave')).toBeInTheDocument();
    expect(screen.queryByText('Transfer')).not.toBeInTheDocument();
    expect(screen.queryByText('Delete')).not.toBeInTheDocument();
  });

  it('returns null when group is null', () => {
    const { container } = render(
      <GroupContextMenu
        isOpen={true}
        onClose={vi.fn()}
        group={null}
        isOwner={false}
        isAdmin={false}
        {...defaultHandlers}
      />
    );
    expect(container.innerHTML).toBe('');
  });
});
