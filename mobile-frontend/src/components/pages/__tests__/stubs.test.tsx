import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import FileBrowser from '../FileBrowser';
import GroupDetail from '../GroupDetail';
import MorePage from '../MorePage';

vi.mock('../../../lib/api', () => ({
  getAccountInfo: () => Promise.resolve({
    usage: 0, total: 1073741824, email: 'test@test.com',
    name: 'Test', login_id: '', institution: '', is_staff: false, avatar_url: '',
  }),
  logout: () => Promise.resolve(),
}));

vi.mock('../../../lib/sortPreference', () => ({
  getSortPreference: () => ({ field: 'name', direction: 'asc' }),
  setSortPreference: vi.fn(),
}));

describe('GroupDetail', () => {
  it('renders without crashing when no groupId', () => {
    render(<GroupDetail />);
    expect(screen.getByText('No group selected')).toBeInTheDocument();
  });
});

describe('MorePage (Settings)', () => {
  it('renders settings with theme selector', () => {
    render(<MorePage />);
    expect(screen.getByText('Settings')).toBeInTheDocument();
    expect(screen.getByText('Light')).toBeInTheDocument();
    expect(screen.getByText('Dark')).toBeInTheDocument();
    expect(screen.getByText('System')).toBeInTheDocument();
  });
});

describe('FileBrowser', () => {
  it('renders without crashing', () => {
    render(<FileBrowser />);
    expect(screen.getByText('File Browser')).toBeInTheDocument();
  });

  it('shows placeholder when no repoId provided', () => {
    render(<FileBrowser />);
    expect(screen.getByText('Select a library to browse files')).toBeInTheDocument();
  });
});
