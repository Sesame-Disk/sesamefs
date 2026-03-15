import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import ZipDownloadProgress from '../ZipDownloadProgress';

// Mock the API
vi.mock('../../../lib/api', () => ({
  queryZipProgress: vi.fn(),
  cancelZipTask: vi.fn(),
}));

vi.mock('../../../lib/config', () => ({
  fileServerRoot: () => '/seafhttp/',
}));

// Mock framer-motion to avoid animation issues in tests
vi.mock('framer-motion', () => ({
  AnimatePresence: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  motion: {
    div: React.forwardRef(({ children, ...props }: any, ref: any) => (
      <div ref={ref} {...props}>{children}</div>
    )),
  },
}));

import { queryZipProgress, cancelZipTask } from '../../../lib/api';

const mockQueryZipProgress = queryZipProgress as ReturnType<typeof vi.fn>;
const mockCancelZipTask = cancelZipTask as ReturnType<typeof vi.fn>;

describe('ZipDownloadProgress', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  const defaultProps = {
    isOpen: true,
    zipToken: 'test-token-123',
    onClose: vi.fn(),
    onError: vi.fn(),
  };

  it('shows progress percentage', async () => {
    mockQueryZipProgress.mockResolvedValue({ zipped: 5, total: 10, failed: 0 });
    render(<ZipDownloadProgress {...defaultProps} />);

    await waitFor(() => {
      expect(screen.getByText('Packaging... 50.0%')).toBeInTheDocument();
    });
  });

  it('shows error when zip fails with size too large', async () => {
    mockQueryZipProgress.mockResolvedValue({
      zipped: 0,
      total: 10,
      failed: 1,
      failed_reason: 'size too large',
    });
    render(<ZipDownloadProgress {...defaultProps} />);

    await waitFor(() => {
      expect(screen.getByText('Failed to download. The total size of the files exceeded the limit.')).toBeInTheDocument();
    });
  });

  it('shows Internal Server Error for internal error', async () => {
    mockQueryZipProgress.mockResolvedValue({
      zipped: 0,
      total: 10,
      failed: 1,
      failed_reason: 'internal error',
    });
    render(<ZipDownloadProgress {...defaultProps} />);

    await waitFor(() => {
      expect(screen.getByText('Internal Server Error')).toBeInTheDocument();
    });
  });

  it('calls cancelZipTask and onClose when cancel is clicked', async () => {
    mockQueryZipProgress.mockResolvedValue({ zipped: 3, total: 10, failed: 0 });
    mockCancelZipTask.mockResolvedValue(undefined);
    const onClose = vi.fn();
    render(<ZipDownloadProgress {...defaultProps} onClose={onClose} />);

    await waitFor(() => {
      expect(screen.getByTestId('zip-cancel-btn')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('zip-cancel-btn'));

    await waitFor(() => {
      expect(mockCancelZipTask).toHaveBeenCalledWith('test-token-123');
      expect(onClose).toHaveBeenCalled();
    });
  });

  it('calls onClose when zipping completes', async () => {
    mockQueryZipProgress.mockResolvedValue({ zipped: 10, total: 10, failed: 0 });
    const onClose = vi.fn();

    render(<ZipDownloadProgress {...defaultProps} onClose={onClose} />);

    await waitFor(() => {
      expect(onClose).toHaveBeenCalled();
    });
  });

  it('does not render when isOpen is false', () => {
    const { container } = render(
      <ZipDownloadProgress {...defaultProps} isOpen={false} />
    );
    expect(container.innerHTML).toBe('');
  });

  it('does not poll when zipToken is null', () => {
    render(<ZipDownloadProgress {...defaultProps} zipToken={null} />);
    expect(mockQueryZipProgress).not.toHaveBeenCalled();
  });

  it('shows error when query fails', async () => {
    mockQueryZipProgress.mockRejectedValue(new Error('Network error'));
    render(<ZipDownloadProgress {...defaultProps} />);

    await waitFor(() => {
      expect(screen.getByText('Failed to check download progress')).toBeInTheDocument();
    });
  });

  it('renders download title', async () => {
    mockQueryZipProgress.mockResolvedValue({ zipped: 1, total: 10, failed: 0 });
    render(<ZipDownloadProgress {...defaultProps} />);

    await waitFor(() => {
      expect(screen.getByText('Download')).toBeInTheDocument();
    });
  });
});
