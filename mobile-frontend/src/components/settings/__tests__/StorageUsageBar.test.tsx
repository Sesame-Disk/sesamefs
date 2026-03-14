import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import StorageUsageBar from '../StorageUsageBar';

describe('StorageUsageBar', () => {
  it('renders usage text', () => {
    render(<StorageUsageBar used={524288000} total={1073741824} />);
    expect(screen.getByText(/500 MB of 1 GB used/)).toBeInTheDocument();
  });

  it('shows percentage', () => {
    render(<StorageUsageBar used={524288000} total={1073741824} />);
    expect(screen.getByText('49%')).toBeInTheDocument();
  });

  it('shows red when usage is high', () => {
    render(<StorageUsageBar used={900000000} total={1073741824} />);
    const fill = screen.getByTestId('storage-bar-fill');
    expect(fill.className).toContain('bg-red-500');
  });

  it('shows primary color when usage is normal', () => {
    render(<StorageUsageBar used={100000000} total={1073741824} />);
    const fill = screen.getByTestId('storage-bar-fill');
    expect(fill.className).toContain('bg-primary');
  });

  it('handles zero total gracefully', () => {
    render(<StorageUsageBar used={0} total={0} />);
    expect(screen.getByText('0%')).toBeInTheDocument();
  });
});
