import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import FileIcon from '../FileIcon';

describe('FileIcon', () => {
  it('renders with folder type', () => {
    render(<FileIcon type="folder" />);
    const el = screen.getByTestId('file-icon');
    expect(el).toBeInTheDocument();
    expect(el).toHaveAttribute('data-file-type', 'folder');
  });

  it('renders with image type', () => {
    render(<FileIcon type="image" />);
    expect(screen.getByTestId('file-icon')).toHaveAttribute('data-file-type', 'image');
  });

  it('renders with pdf type', () => {
    render(<FileIcon type="pdf" />);
    expect(screen.getByTestId('file-icon')).toHaveAttribute('data-file-type', 'pdf');
  });

  it('renders with generic type', () => {
    render(<FileIcon type="generic" />);
    expect(screen.getByTestId('file-icon')).toHaveAttribute('data-file-type', 'generic');
  });

  it('renders with custom size', () => {
    render(<FileIcon type="code" size={32} />);
    expect(screen.getByTestId('file-icon')).toBeInTheDocument();
  });
});
