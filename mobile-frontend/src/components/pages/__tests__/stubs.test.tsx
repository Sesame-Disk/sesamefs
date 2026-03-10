import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import LibraryList from '../LibraryList';
import FileBrowser from '../FileBrowser';
import SharedLibraries from '../SharedLibraries';
import GroupDetail from '../GroupDetail';
import StarredFiles from '../StarredFiles';
import ActivityFeed from '../ActivityFeed';
import SearchPage from '../SearchPage';
import MorePage from '../MorePage';

const stubs = [
  { Component: LibraryList, title: 'My Libraries' },
  { Component: SharedLibraries, title: 'Shared Libraries' },
  { Component: GroupDetail, title: 'Group Detail' },
  { Component: StarredFiles, title: 'Starred Files' },
  { Component: ActivityFeed, title: 'Activity Feed' },
  { Component: SearchPage, title: 'Search' },
];

describe('Stub page components', () => {
  stubs.forEach(({ Component, title }) => {
    it(`${Component.name} renders without crashing`, () => {
      render(<Component />);
      expect(screen.getByText(title)).toBeInTheDocument();
    });

    it(`${Component.name} shows "Coming soon" message`, () => {
      render(<Component />);
      expect(screen.getByText('Coming soon')).toBeInTheDocument();
    });
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
