import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import TagBadge from '../TagBadge';

describe('TagBadge', () => {
  it('renders tag name', () => {
    render(<TagBadge name="Important" color="#F44336" />);
    expect(screen.getByText('Important')).toBeInTheDocument();
  });

  it('applies background color from tag color', () => {
    render(<TagBadge name="Urgent" color="#E91E63" />);
    const badge = screen.getByTestId('tag-badge');
    expect(badge.style.backgroundColor).toBe('rgb(233, 30, 99)');
  });

  it('renders small size with smaller classes', () => {
    render(<TagBadge name="Bug" color="#9C27B0" size="small" />);
    const badge = screen.getByTestId('tag-badge');
    expect(badge.className).toContain('text-[10px]');
  });

  it('renders normal size by default', () => {
    render(<TagBadge name="Feature" color="#3F51B5" />);
    const badge = screen.getByTestId('tag-badge');
    expect(badge.className).toContain('text-xs');
  });

  it('is clickable when onClick is provided', () => {
    const onClick = vi.fn();
    render(<TagBadge name="Click me" color="#4CAF50" onClick={onClick} />);
    const badge = screen.getByTestId('tag-badge');
    expect(badge.className).toContain('cursor-pointer');
  });
});
