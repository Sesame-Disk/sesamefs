import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import Avatar from '../Avatar';

describe('Avatar', () => {
  it('renders initials when no src', () => {
    render(<Avatar name="John Doe" />);
    const el = screen.getByTestId('avatar');
    expect(el).toHaveTextContent('JD');
  });

  it('renders single initial for single name', () => {
    render(<Avatar name="Alice" />);
    expect(screen.getByTestId('avatar')).toHaveTextContent('A');
  });

  it('renders image when src is provided', () => {
    render(<Avatar name="John" src="/photo.jpg" />);
    const img = screen.getByTestId('avatar') as HTMLImageElement;
    expect(img.tagName).toBe('IMG');
    expect(img.src).toContain('/photo.jpg');
  });

  it('applies size variant sm', () => {
    render(<Avatar name="Test" size="sm" />);
    const el = screen.getByTestId('avatar');
    expect(el.className).toContain('w-8');
  });

  it('applies size variant lg', () => {
    render(<Avatar name="Test" size="lg" />);
    const el = screen.getByTestId('avatar');
    expect(el.className).toContain('w-14');
  });
});
