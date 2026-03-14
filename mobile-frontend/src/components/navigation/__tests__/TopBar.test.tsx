import React from 'react';
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import TopBar from '../TopBar';

describe('TopBar', () => {
  it('renders the title', () => {
    render(<TopBar title="My Libraries" />);
    expect(screen.getByText('My Libraries')).toBeInTheDocument();
  });

  it('renders search button', () => {
    render(<TopBar title="Test" />);
    const searchLink = screen.getByLabelText('Search');
    expect(searchLink).toHaveAttribute('href', '/search/');
  });

  it('renders avatar button', () => {
    render(<TopBar title="Test" />);
    const profileLink = screen.getByLabelText('Profile');
    expect(profileLink).toHaveAttribute('href', '/more/');
  });

  it('shows initials when no avatar URL', () => {
    render(<TopBar title="Test" userName="Alice" />);
    expect(screen.getByText('A')).toBeInTheDocument();
  });

  it('shows avatar image when URL provided', () => {
    render(<TopBar title="Test" avatarUrl="/avatar.png" userName="Alice" />);
    const profileLink = screen.getByLabelText('Profile');
    const img = profileLink.querySelector('img');
    expect(img).toHaveAttribute('src', '/avatar.png');
  });
});
