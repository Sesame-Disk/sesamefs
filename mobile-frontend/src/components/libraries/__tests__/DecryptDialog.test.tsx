import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import DecryptDialog from '../DecryptDialog';

vi.mock('../../../lib/api', () => ({
  setRepoPassword: vi.fn(),
}));

vi.mock('../../../lib/encryption', () => ({
  getRememberedPassword: vi.fn(() => null),
  setRememberedPassword: vi.fn(),
  markRepoDecrypted: vi.fn(),
}));

import { setRepoPassword } from '../../../lib/api';
import { getRememberedPassword, setRememberedPassword, markRepoDecrypted } from '../../../lib/encryption';

const defaultProps = {
  isOpen: true,
  onClose: vi.fn(),
  repoId: 'repo-123',
  repoName: 'My Encrypted Lib',
  onSuccess: vi.fn(),
};

describe('DecryptDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders password input', () => {
    render(<DecryptDialog {...defaultProps} />);
    expect(screen.getByTestId('decrypt-password-input')).toBeInTheDocument();
    expect(screen.getByText('This library is encrypted')).toBeInTheDocument();
    expect(screen.getByText(/Enter password to access My Encrypted Lib/)).toBeInTheDocument();
  });

  it('shows error on wrong password', async () => {
    (setRepoPassword as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('Incorrect password'));

    render(<DecryptDialog {...defaultProps} />);
    const input = screen.getByTestId('decrypt-password-input');
    fireEvent.change(input, { target: { value: 'wrong-pass' } });
    fireEvent.click(screen.getByTestId('decrypt-submit'));

    await waitFor(() => {
      expect(screen.getByTestId('decrypt-error')).toHaveTextContent('Incorrect password');
    });
  });

  it('calls setRepoPassword API on submit', async () => {
    (setRepoPassword as ReturnType<typeof vi.fn>).mockResolvedValue(undefined);

    render(<DecryptDialog {...defaultProps} />);
    const input = screen.getByTestId('decrypt-password-input');
    fireEvent.change(input, { target: { value: 'correct-pass' } });
    fireEvent.click(screen.getByTestId('decrypt-submit'));

    await waitFor(() => {
      expect(setRepoPassword).toHaveBeenCalledWith('repo-123', 'correct-pass');
      expect(markRepoDecrypted).toHaveBeenCalledWith('repo-123');
      expect(defaultProps.onSuccess).toHaveBeenCalled();
      expect(defaultProps.onClose).toHaveBeenCalled();
    });
  });

  it('stores password when remember checkbox is checked', async () => {
    (setRepoPassword as ReturnType<typeof vi.fn>).mockResolvedValue(undefined);

    render(<DecryptDialog {...defaultProps} />);
    const input = screen.getByTestId('decrypt-password-input');
    fireEvent.change(input, { target: { value: 'my-pass' } });
    fireEvent.click(screen.getByTestId('remember-checkbox'));
    fireEvent.click(screen.getByTestId('decrypt-submit'));

    await waitFor(() => {
      expect(setRememberedPassword).toHaveBeenCalledWith('repo-123', 'my-pass');
    });
  });

  it('auto-submits with remembered password', async () => {
    (getRememberedPassword as ReturnType<typeof vi.fn>).mockReturnValue('saved-pass');
    (setRepoPassword as ReturnType<typeof vi.fn>).mockResolvedValue(undefined);

    render(<DecryptDialog {...defaultProps} />);

    await waitFor(() => {
      expect(setRepoPassword).toHaveBeenCalledWith('repo-123', 'saved-pass');
      expect(defaultProps.onSuccess).toHaveBeenCalled();
    });
  });

  it('does not render when isOpen is false', () => {
    const { container } = render(<DecryptDialog {...defaultProps} isOpen={false} />);
    expect(container.innerHTML).toBe('');
  });
});
