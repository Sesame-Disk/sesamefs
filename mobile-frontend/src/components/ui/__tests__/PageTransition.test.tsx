import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import PageTransition from '../PageTransition';

describe('PageTransition', () => {
  it('renders children', () => {
    render(
      <PageTransition pageKey="test">
        <p>Hello World</p>
      </PageTransition>
    );
    expect(screen.getByText('Hello World')).toBeInTheDocument();
  });

  it('applies animation style via data-testid', () => {
    render(
      <PageTransition pageKey="test-key">
        <p>Content</p>
      </PageTransition>
    );
    const wrapper = screen.getByTestId('page-transition');
    expect(wrapper).toBeInTheDocument();
    expect(wrapper.style.transform).toBeDefined();
  });

  it('renders with forward direction', () => {
    render(
      <PageTransition pageKey="fwd" direction="forward">
        <p>Forward</p>
      </PageTransition>
    );
    expect(screen.getByText('Forward')).toBeInTheDocument();
  });

  it('renders with back direction', () => {
    render(
      <PageTransition pageKey="back" direction="back">
        <p>Back</p>
      </PageTransition>
    );
    expect(screen.getByText('Back')).toBeInTheDocument();
  });
});
