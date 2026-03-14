import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import ImageViewer from '../ImageViewer';

const defaultProps = {
  url: 'https://example.com/image.jpg',
  fileName: 'photo.jpg',
  onClose: vi.fn(),
};

describe('ImageViewer', () => {
  it('renders image with the download URL', () => {
    render(<ImageViewer {...defaultProps} />);
    const img = screen.getByAltText('photo.jpg');
    expect(img).toBeInTheDocument();
    expect(img).toHaveAttribute('src', 'https://example.com/image.jpg');
  });

  it('displays the file name', () => {
    render(<ImageViewer {...defaultProps} />);
    expect(screen.getByText('photo.jpg')).toBeInTheDocument();
  });

  it('calls onClose when close button is clicked', () => {
    const onClose = vi.fn();
    render(<ImageViewer {...defaultProps} onClose={onClose} />);
    fireEvent.click(screen.getByTestId('close-button'));
    expect(onClose).toHaveBeenCalled();
  });

  it('renders the image viewer container', () => {
    render(<ImageViewer {...defaultProps} />);
    expect(screen.getByTestId('image-viewer')).toBeInTheDocument();
  });

  it('shows navigation arrows when there are sibling images', () => {
    const siblings = [
      { name: 'a.jpg', url: '' },
      { name: 'b.jpg', url: '' },
      { name: 'c.jpg', url: '' },
    ];
    render(
      <ImageViewer {...defaultProps} siblingImages={siblings} currentIndex={1} onNavigate={vi.fn()} />
    );
    expect(screen.getByLabelText('Previous image')).toBeInTheDocument();
    expect(screen.getByLabelText('Next image')).toBeInTheDocument();
  });

  it('hides previous arrow on first image', () => {
    const siblings = [
      { name: 'a.jpg', url: '' },
      { name: 'b.jpg', url: '' },
    ];
    render(
      <ImageViewer {...defaultProps} siblingImages={siblings} currentIndex={0} onNavigate={vi.fn()} />
    );
    expect(screen.queryByLabelText('Previous image')).not.toBeInTheDocument();
    expect(screen.getByLabelText('Next image')).toBeInTheDocument();
  });
});
