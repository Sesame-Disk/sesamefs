import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import FileContextMenu from '../FileContextMenu';
import type { Dirent } from '../../../lib/models';

const mockDirent: Dirent = {
  id: 'abc123',
  type: 'file',
  name: 'test-doc.pdf',
  size: 1024,
  mtime: 1700000000,
  permission: 'rw',
  starred: false,
};

const defaultProps = {
  isOpen: true,
  onClose: vi.fn(),
  dirent: mockDirent,
  repoId: 'repo-1',
  path: '/',
  onStar: vi.fn(),
  onShare: vi.fn(),
  onRename: vi.fn(),
  onCopy: vi.fn(),
  onMove: vi.fn(),
  onDownload: vi.fn(),
  onDetails: vi.fn(),
  onDelete: vi.fn(),
};

describe('FileContextMenu', () => {
  it('renders all operation options', () => {
    render(<FileContextMenu {...defaultProps} />);
    expect(screen.getByText('Star')).toBeInTheDocument();
    expect(screen.getByText('Share')).toBeInTheDocument();
    expect(screen.getByText('Rename')).toBeInTheDocument();
    expect(screen.getByText('Copy')).toBeInTheDocument();
    expect(screen.getByText('Move')).toBeInTheDocument();
    expect(screen.getByText('Download')).toBeInTheDocument();
    expect(screen.getByText('Details')).toBeInTheDocument();
    expect(screen.getByText('Delete')).toBeInTheDocument();
  });

  it('shows Unstar when item is starred', () => {
    const starredDirent = { ...mockDirent, starred: true };
    render(<FileContextMenu {...defaultProps} dirent={starredDirent} />);
    expect(screen.getByText('Unstar')).toBeInTheDocument();
  });

  it('triggers delete action when Delete is clicked', () => {
    const onDelete = vi.fn();
    const onClose = vi.fn();
    render(<FileContextMenu {...defaultProps} onDelete={onDelete} onClose={onClose} />);
    fireEvent.click(screen.getByText('Delete'));
    expect(onClose).toHaveBeenCalled();
    expect(onDelete).toHaveBeenCalled();
  });

  it('triggers rename action when Rename is clicked', () => {
    const onRename = vi.fn();
    const onClose = vi.fn();
    render(<FileContextMenu {...defaultProps} onRename={onRename} onClose={onClose} />);
    fireEvent.click(screen.getByText('Rename'));
    expect(onClose).toHaveBeenCalled();
    expect(onRename).toHaveBeenCalled();
  });

  it('does not render when dirent is null', () => {
    const { container } = render(<FileContextMenu {...defaultProps} dirent={null} />);
    expect(container.innerHTML).toBe('');
  });

  it('does not render when isOpen is false', () => {
    const { container } = render(<FileContextMenu {...defaultProps} isOpen={false} />);
    expect(container.innerHTML).toBe('');
  });
});
