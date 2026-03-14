import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import Loading from '../Loading';

describe('Loading', () => {
  it('renders spinner', () => {
    render(<Loading />);
    expect(screen.getByTestId('loading')).toBeInTheDocument();
  });

  it('renders with message', () => {
    render(<Loading message="Please wait..." />);
    expect(screen.getByText('Please wait...')).toBeInTheDocument();
  });

  it('does not render message when not provided', () => {
    render(<Loading />);
    expect(screen.queryByText(/./)).not.toBeInTheDocument();
  });
});
