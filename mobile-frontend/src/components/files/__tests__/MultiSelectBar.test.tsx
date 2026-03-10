import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import MultiSelectBar from '../MultiSelectBar';
import type { Dirent } from '../../../lib/models';

const mockItems: Dirent[] = [
  { id: 'file-1', type: 'file', name: 'doc1.pdf', size: 1024, mtime: 1700000000, permission: 'rw' },
  { id: 'file-2', type: 'file', name: 'doc2.pdf', size: 2048, mtime: 1700000000, permission: 'rw' },
  { id: 'dir-1', type: 'dir', name: 'Folder', size: 0, mtime: 1700000000, permission: 'rw' },
];

const defaultProps = {
  selectedItems: mockItems.slice(0, 2),
  totalItems: 5,
  onSelectAll: vi.fn(),
  onDeselectAll: vi.fn(),
  onShare: vi.fn(),
  onMove: vi.fn(),
  onCopy: vi.fn(),
  onDelete: vi.fn(),
};

describe('MultiSelectBar', () => {
  it('shows count of selected items', () => {
    render(<MultiSelectBar {...defaultProps} />);
    expect(screen.getByText('2 selected')).toBeInTheDocument();
  });

  it('shows Select All when not all selected', () => {
    render(<MultiSelectBar {...defaultProps} />);
    expect(screen.getByText('Select All')).toBeInTheDocument();
  });

  it('shows Deselect All when all selected', () => {
    render(<MultiSelectBar {...defaultProps} selectedItems={mockItems} totalItems={3} />);
    expect(screen.getByText('Deselect All')).toBeInTheDocument();
  });

  it('calls onSelectAll when Select All is clicked', () => {
    const onSelectAll = vi.fn();
    render(<MultiSelectBar {...defaultProps} onSelectAll={onSelectAll} />);
    fireEvent.click(screen.getByText('Select All'));
    expect(onSelectAll).toHaveBeenCalled();
  });

  it('calls onDelete when Delete button is clicked', () => {
    const onDelete = vi.fn();
    render(<MultiSelectBar {...defaultProps} onDelete={onDelete} />);
    fireEvent.click(screen.getByText('Delete'));
    expect(onDelete).toHaveBeenCalled();
  });

  it('does not render when no items selected', () => {
    const { container } = render(<MultiSelectBar {...defaultProps} selectedItems={[]} />);
    expect(container.innerHTML).toBe('');
  });

  it('shows all action buttons', () => {
    render(<MultiSelectBar {...defaultProps} />);
    expect(screen.getByText('Share')).toBeInTheDocument();
    expect(screen.getByText('Move')).toBeInTheDocument();
    expect(screen.getByText('Copy')).toBeInTheDocument();
    expect(screen.getByText('Delete')).toBeInTheDocument();
  });
});
