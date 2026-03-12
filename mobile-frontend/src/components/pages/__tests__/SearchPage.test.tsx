import React from 'react';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import SearchPage from '../SearchPage';

// Mock the API module
vi.mock('../../../lib/api', () => ({
  searchFiles: vi.fn(),
}));

import { searchFiles } from '../../../lib/api';
const mockSearchFiles = vi.mocked(searchFiles);

beforeEach(() => {
  localStorage.clear();
  mockSearchFiles.mockReset();
  vi.useFakeTimers({ shouldAdvanceTime: true });
});

afterEach(() => {
  vi.useRealTimers();
});

describe('SearchPage', () => {
  it('renders search input', () => {
    render(<SearchPage />);
    expect(screen.getByTestId('search-input')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('Search files...')).toBeInTheDocument();
  });

  it('renders cancel button', () => {
    render(<SearchPage />);
    expect(screen.getByTestId('cancel-button')).toBeInTheDocument();
  });

  it('auto-focuses the search input', () => {
    render(<SearchPage />);
    expect(screen.getByTestId('search-input')).toHaveFocus();
  });

  it('does not search with fewer than 2 characters', async () => {
    render(<SearchPage />);
    const input = screen.getByTestId('search-input');
    fireEvent.change(input, { target: { value: 'a' } });
    await act(async () => {
      vi.advanceTimersByTime(400);
    });
    expect(mockSearchFiles).not.toHaveBeenCalled();
  });

  it('triggers debounced search after 300ms for query >= 2 chars', async () => {
    mockSearchFiles.mockResolvedValue({ results: [], total: 0 });
    render(<SearchPage />);
    const input = screen.getByTestId('search-input');

    fireEvent.change(input, { target: { value: 'test' } });

    // Not called yet (debounce)
    expect(mockSearchFiles).not.toHaveBeenCalled();

    await act(async () => {
      vi.advanceTimersByTime(300);
    });

    expect(mockSearchFiles).toHaveBeenCalledWith('test', 1, 25);
  });

  it('renders results grouped by library name', async () => {
    mockSearchFiles.mockResolvedValue({
      results: [
        { repo_id: 'r1', repo_name: 'Library A', name: 'doc.pdf', path: '/doc.pdf', size: 100, mtime: 1000, is_dir: false },
        { repo_id: 'r1', repo_name: 'Library A', name: 'photo.jpg', path: '/photo.jpg', size: 200, mtime: 1001, is_dir: false },
        { repo_id: 'r2', repo_name: 'Library B', name: 'notes.txt', path: '/notes.txt', size: 50, mtime: 1002, is_dir: false },
      ],
      total: 3,
    });

    render(<SearchPage />);
    const input = screen.getByTestId('search-input');
    fireEvent.change(input, { target: { value: 'doc' } });

    await act(async () => {
      vi.advanceTimersByTime(300);
    });

    await waitFor(() => {
      expect(screen.getByTestId('search-results')).toBeInTheDocument();
    });

    const groups = screen.getAllByTestId('result-group');
    expect(groups).toHaveLength(2);

    expect(screen.getByText('Library A')).toBeInTheDocument();
    expect(screen.getByText('Library B')).toBeInTheDocument();

    const items = screen.getAllByTestId('search-result-item');
    expect(items).toHaveLength(3);
  });

  it('shows no results message when search returns empty', async () => {
    mockSearchFiles.mockResolvedValue({ results: [], total: 0 });
    render(<SearchPage />);
    const input = screen.getByTestId('search-input');
    fireEvent.change(input, { target: { value: 'nonexistent' } });

    await act(async () => {
      vi.advanceTimersByTime(300);
    });

    await waitFor(() => {
      expect(screen.getByTestId('no-results')).toBeInTheDocument();
    });
  });

  it('displays recent searches from localStorage', () => {
    localStorage.setItem('recent_searches', JSON.stringify(['hello', 'world']));
    render(<SearchPage />);
    expect(screen.getByTestId('recent-searches')).toBeInTheDocument();
    const chips = screen.getAllByTestId('recent-search-chip');
    expect(chips).toHaveLength(2);
    expect(screen.getByText('hello')).toBeInTheDocument();
    expect(screen.getByText('world')).toBeInTheDocument();
  });

  it('clicking a recent search fills input and triggers search', async () => {
    localStorage.setItem('recent_searches', JSON.stringify(['myquery']));
    mockSearchFiles.mockResolvedValue({ results: [], total: 0 });
    render(<SearchPage />);

    const recentText = screen.getByTestId('recent-search-text');
    fireEvent.click(recentText);

    expect(screen.getByTestId('search-input')).toHaveValue('myquery');

    await act(async () => {
      vi.advanceTimersByTime(300);
    });

    expect(mockSearchFiles).toHaveBeenCalledWith('myquery', 1, 25);
  });

  it('removes individual recent search', () => {
    localStorage.setItem('recent_searches', JSON.stringify(['first', 'second']));
    render(<SearchPage />);

    const removeButtons = screen.getAllByTestId('remove-recent');
    fireEvent.click(removeButtons[0]);

    const chips = screen.getAllByTestId('recent-search-chip');
    expect(chips).toHaveLength(1);
    expect(screen.queryByText('first')).not.toBeInTheDocument();
    expect(screen.getByText('second')).toBeInTheDocument();
  });

  it('clears all recent searches', () => {
    localStorage.setItem('recent_searches', JSON.stringify(['a', 'b', 'c']));
    render(<SearchPage />);

    fireEvent.click(screen.getByTestId('clear-all-recent'));
    expect(screen.queryByTestId('recent-searches')).not.toBeInTheDocument();
  });

  it('highlights matching text in results', async () => {
    mockSearchFiles.mockResolvedValue({
      results: [
        { repo_id: 'r1', repo_name: 'Lib', name: 'testfile.txt', path: '/testfile.txt', size: 100, mtime: 1000, is_dir: false },
      ],
      total: 1,
    });

    render(<SearchPage />);
    fireEvent.change(screen.getByTestId('search-input'), { target: { value: 'test' } });

    await act(async () => {
      vi.advanceTimersByTime(300);
    });

    await waitFor(() => {
      const marks = document.querySelectorAll('mark');
      expect(marks.length).toBeGreaterThan(0);
      expect(marks[0].textContent).toBe('test');
    });
  });
});
