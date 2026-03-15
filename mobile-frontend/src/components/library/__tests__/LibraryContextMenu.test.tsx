import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import LibraryContextMenu from '../../libraries/LibraryContextMenu';

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

const defaultHandlers = {
  onOpen: vi.fn(),
  onShare: vi.fn(),
  onRename: vi.fn(),
  onDelete: vi.fn(),
  onTransfer: vi.fn(),
  onLeave: vi.fn(),
  onDetails: vi.fn(),
};

describe('LibraryContextMenu', () => {
  it('shows owner actions when isOwner is true', () => {
    render(
      <LibraryContextMenu
        isOpen={true}
        onClose={vi.fn()}
        repo={mockRepo}
        isOwner={true}
        {...defaultHandlers}
      />
    );
    expect(screen.getByText('Open')).toBeInTheDocument();
    expect(screen.getByText('Share')).toBeInTheDocument();
    expect(screen.getByText('Rename')).toBeInTheDocument();
    expect(screen.getByText('Delete')).toBeInTheDocument();
    expect(screen.getByText('Transfer')).toBeInTheDocument();
    expect(screen.getByText('Details')).toBeInTheDocument();
    expect(screen.queryByText('Leave Share')).not.toBeInTheDocument();
  });

  it('shows shared actions when isOwner is false', () => {
    render(
      <LibraryContextMenu
        isOpen={true}
        onClose={vi.fn()}
        repo={mockRepo}
        isOwner={false}
        {...defaultHandlers}
      />
    );
    expect(screen.getByText('Open')).toBeInTheDocument();
    expect(screen.getByText('Share')).toBeInTheDocument();
    expect(screen.getByText('Leave Share')).toBeInTheDocument();
    expect(screen.getByText('Details')).toBeInTheDocument();
    expect(screen.queryByText('Rename')).not.toBeInTheDocument();
    expect(screen.queryByText('Delete')).not.toBeInTheDocument();
    expect(screen.queryByText('Transfer')).not.toBeInTheDocument();
  });
});
