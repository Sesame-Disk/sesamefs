import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import SwipeableListItem from '../SwipeableListItem';

describe('SwipeableListItem', () => {
  it('renders children', () => {
    render(
      <SwipeableListItem>
        <p>Item content</p>
      </SwipeableListItem>,
    );
    expect(screen.getByText('Item content')).toBeInTheDocument();
  });

  it('renders left actions when provided', () => {
    render(
      <SwipeableListItem
        leftActions={[{ icon: <span>ic</span>, label: 'Pin', color: '#3b82f6', onClick: () => {} }]}
      >
        <p>Content</p>
      </SwipeableListItem>,
    );
    expect(screen.getByTestId('left-actions')).toBeInTheDocument();
    expect(screen.getByText('Pin')).toBeInTheDocument();
  });

  it('renders right actions when provided', () => {
    render(
      <SwipeableListItem
        rightActions={[{ icon: <span>ic</span>, label: 'Delete', color: '#ef4444', onClick: () => {} }]}
      >
        <p>Content</p>
      </SwipeableListItem>,
    );
    expect(screen.getByTestId('right-actions')).toBeInTheDocument();
    expect(screen.getByText('Delete')).toBeInTheDocument();
  });

  it('does not render action panels when no actions provided', () => {
    render(
      <SwipeableListItem>
        <p>Content</p>
      </SwipeableListItem>,
    );
    expect(screen.queryByTestId('left-actions')).not.toBeInTheDocument();
    expect(screen.queryByTestId('right-actions')).not.toBeInTheDocument();
  });
});
