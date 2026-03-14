import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import React from 'react';
import UploadButton from '../UploadButton';

// Mock the upload manager
vi.mock('../../../lib/upload', () => ({
  uploadManager: {
    addFiles: vi.fn().mockReturnValue([]),
    subscribe: vi.fn().mockReturnValue(() => {}),
    getQueue: vi.fn().mockReturnValue([]),
  },
}));

describe('UploadButton', () => {
  const defaultProps = {
    repoId: 'repo-1',
    path: '/',
    onUploadStarted: vi.fn(),
    onNewFolder: vi.fn(),
    onNewFile: vi.fn(),
  };

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders a FAB button', () => {
    render(<UploadButton {...defaultProps} />);
    const fab = screen.getByTestId('upload-fab');
    expect(fab).toBeDefined();
  });

  it('opens menu on FAB click', () => {
    render(<UploadButton {...defaultProps} />);
    const fab = screen.getByTestId('upload-fab');
    fireEvent.click(fab);
    expect(screen.getByTestId('upload-menu')).toBeDefined();
  });

  it('shows upload options in menu', () => {
    render(<UploadButton {...defaultProps} />);
    fireEvent.click(screen.getByTestId('upload-fab'));

    expect(screen.getByTestId('upload-files-btn')).toBeDefined();
    expect(screen.getByTestId('upload-camera-btn')).toBeDefined();
    expect(screen.getByTestId('upload-folder-btn')).toBeDefined();
    expect(screen.getByTestId('new-folder-btn')).toBeDefined();
    expect(screen.getByTestId('new-file-btn')).toBeDefined();
  });

  it('closes menu when overlay is clicked', () => {
    render(<UploadButton {...defaultProps} />);
    fireEvent.click(screen.getByTestId('upload-fab'));
    expect(screen.getByTestId('upload-menu')).toBeDefined();

    fireEvent.click(screen.getByTestId('upload-overlay'));
    expect(screen.queryByTestId('upload-menu')).toBeNull();
  });

  it('calls onNewFolder when new folder button is clicked', () => {
    render(<UploadButton {...defaultProps} />);
    fireEvent.click(screen.getByTestId('upload-fab'));
    fireEvent.click(screen.getByTestId('new-folder-btn'));

    expect(defaultProps.onNewFolder).toHaveBeenCalledOnce();
  });

  it('calls onNewFile when new file button is clicked', () => {
    render(<UploadButton {...defaultProps} />);
    fireEvent.click(screen.getByTestId('upload-fab'));
    fireEvent.click(screen.getByTestId('new-file-btn'));

    expect(defaultProps.onNewFile).toHaveBeenCalledOnce();
  });

  it('renders hidden file inputs', () => {
    render(<UploadButton {...defaultProps} />);

    const fileInput = screen.getByTestId('file-input');
    expect(fileInput).toBeDefined();
    expect(fileInput.getAttribute('type')).toBe('file');
    expect(fileInput.hasAttribute('multiple')).toBe(true);

    const cameraInput = screen.getByTestId('camera-input');
    expect(cameraInput).toBeDefined();
    expect(cameraInput.getAttribute('accept')).toBe('image/*');
    expect(cameraInput.getAttribute('capture')).toBe('environment');
  });

  it('has correct aria labels', () => {
    render(<UploadButton {...defaultProps} />);
    const fab = screen.getByTestId('upload-fab');
    expect(fab.getAttribute('aria-label')).toBe('Add new');

    fireEvent.click(fab);
    expect(fab.getAttribute('aria-label')).toBe('Close menu');
  });
});
