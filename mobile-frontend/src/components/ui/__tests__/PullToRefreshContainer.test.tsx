import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import PullToRefreshContainer from '../PullToRefreshContainer';

describe('PullToRefreshContainer', () => {
  it('renders children', () => {
    render(
      <PullToRefreshContainer onRefresh={async () => {}}>
        <p>Content</p>
      </PullToRefreshContainer>,
    );
    expect(screen.getByText('Content')).toBeInTheDocument();
  });

  it('renders pull-to-refresh container', () => {
    render(
      <PullToRefreshContainer onRefresh={async () => {}}>
        <p>Content</p>
      </PullToRefreshContainer>,
    );
    expect(screen.getByTestId('pull-to-refresh')).toBeInTheDocument();
  });

  it('renders pull indicator', () => {
    render(
      <PullToRefreshContainer onRefresh={async () => {}}>
        <p>Content</p>
      </PullToRefreshContainer>,
    );
    expect(screen.getByTestId('pull-indicator')).toBeInTheDocument();
  });
});
