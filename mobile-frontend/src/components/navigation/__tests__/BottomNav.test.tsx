import React from 'react';
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import BottomNav from '../BottomNav';

describe('BottomNav', () => {
  it('renders 5 tabs', () => {
    render(<BottomNav currentPath="/libraries/" />);
    const links = screen.getAllByRole('link');
    expect(links).toHaveLength(5);
  });

  it('renders correct labels', () => {
    render(<BottomNav currentPath="/libraries/" />);
    expect(screen.getByText('Libraries')).toBeInTheDocument();
    expect(screen.getByText('Shared')).toBeInTheDocument();
    expect(screen.getByText('Groups')).toBeInTheDocument();
    expect(screen.getByText('Starred')).toBeInTheDocument();
    expect(screen.getByText('More')).toBeInTheDocument();
  });

  it('links point to correct paths', () => {
    render(<BottomNav currentPath="/libraries/" />);
    const links = screen.getAllByRole('link');
    expect(links[0]).toHaveAttribute('href', '/libraries/');
    expect(links[1]).toHaveAttribute('href', '/shared/');
    expect(links[2]).toHaveAttribute('href', '/groups/');
    expect(links[3]).toHaveAttribute('href', '/starred/');
    expect(links[4]).toHaveAttribute('href', '/more/');
  });

  it('active tab has primary color class', () => {
    render(<BottomNav currentPath="/starred/" />);
    const starredLink = screen.getByText('Starred').closest('a');
    expect(starredLink?.className).toContain('text-[var(--color-primary)]');
  });

  it('inactive tab has gray color class', () => {
    render(<BottomNav currentPath="/libraries/" />);
    const sharedLink = screen.getByText('Shared').closest('a');
    expect(sharedLink?.className).toContain('text-gray-400');
  });
});
