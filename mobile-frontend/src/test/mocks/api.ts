import { vi } from 'vitest';
import {
  MOCK_REPOS,
  MOCK_DIRENTS,
  MOCK_STARRED,
  MOCK_GROUPS,
  MOCK_GROUP_MEMBERS,
  MOCK_GROUP_REPOS,
  MOCK_ACTIVITIES,
  MOCK_SHARE_LINKS,
  MOCK_SHARE_ITEMS,
  MOCK_SEARCHED_USERS,
  MOCK_SEARCH_RESULTS,
} from './data';

// Mock all api.ts exports
export const mockLogin = vi.fn().mockResolvedValue('mock-token-123');
export const mockGetAuthToken = vi.fn().mockReturnValue('mock-token-123');
export const mockSetAuthToken = vi.fn();
export const mockClearAuthToken = vi.fn();
export const mockListRepos = vi.fn().mockResolvedValue(MOCK_REPOS);
export const mockListDir = vi.fn().mockResolvedValue(MOCK_DIRENTS);
export const mockRenameFile = vi.fn().mockResolvedValue(undefined);
export const mockRenameDir = vi.fn().mockResolvedValue(undefined);
export const mockDeleteFile = vi.fn().mockResolvedValue(undefined);
export const mockDeleteDir = vi.fn().mockResolvedValue(undefined);
export const mockMoveFile = vi.fn().mockResolvedValue(undefined);
export const mockMoveDir = vi.fn().mockResolvedValue(undefined);
export const mockCopyFile = vi.fn().mockResolvedValue(undefined);
export const mockCopyDir = vi.fn().mockResolvedValue(undefined);
export const mockGetFileDownloadLink = vi.fn().mockResolvedValue('http://localhost:8080/files/download/mock-link');
export const mockStarFile = vi.fn().mockResolvedValue(undefined);
export const mockUnstarFile = vi.fn().mockResolvedValue(undefined);
export const mockListStarredFiles = vi.fn().mockResolvedValue(MOCK_STARRED);
export const mockListShareLinks = vi.fn().mockResolvedValue(MOCK_SHARE_LINKS);
export const mockCreateShareLink = vi.fn().mockResolvedValue(MOCK_SHARE_LINKS[0]);
export const mockDeleteShareLink = vi.fn().mockResolvedValue(undefined);
export const mockListRepoShareItems = vi.fn().mockResolvedValue(MOCK_SHARE_ITEMS);
export const mockListRepoGroupShares = vi.fn().mockResolvedValue([]);
export const mockShareToUser = vi.fn().mockResolvedValue(undefined);
export const mockShareToGroup = vi.fn().mockResolvedValue(undefined);
export const mockRemoveUserShare = vi.fn().mockResolvedValue(undefined);
export const mockRemoveGroupShare = vi.fn().mockResolvedValue(undefined);
export const mockListGroups = vi.fn().mockResolvedValue(MOCK_GROUPS);
export const mockCreateGroup = vi.fn().mockResolvedValue(MOCK_GROUPS[0]);
export const mockListGroupRepos = vi.fn().mockResolvedValue(MOCK_GROUP_REPOS);
export const mockListGroupMembers = vi.fn().mockResolvedValue(MOCK_GROUP_MEMBERS);
export const mockSetRepoPassword = vi.fn().mockResolvedValue(undefined);
export const mockListActivities = vi.fn().mockResolvedValue({ events: MOCK_ACTIVITIES, more: false });
export const mockSearchFiles = vi.fn().mockResolvedValue({ results: MOCK_SEARCH_RESULTS, total: 3 });
export const mockListSharedRepos = vi.fn().mockResolvedValue([]);
export const mockListBeSharedRepos = vi.fn().mockResolvedValue([]);
export const mockSearchUsers = vi.fn().mockResolvedValue(MOCK_SEARCHED_USERS);

// Mock oidc.ts exports
export const mockIsDevBypass = vi.fn().mockReturnValue(false);
export const mockGetOIDCLoginURL = vi.fn().mockResolvedValue('http://sso.example.com/authorize');
export const mockExchangeOIDCCode = vi.fn().mockResolvedValue('sso-token-456');

/**
 * Sets up all API mocks via vi.mock. Call this in your test file or setup.
 */
export function setupMockAPI() {
  vi.mock('../../lib/api', () => ({
    login: mockLogin,
    getAuthToken: mockGetAuthToken,
    setAuthToken: mockSetAuthToken,
    clearAuthToken: mockClearAuthToken,
    listRepos: mockListRepos,
    listDir: mockListDir,
    renameFile: mockRenameFile,
    renameDir: mockRenameDir,
    deleteFile: mockDeleteFile,
    deleteDir: mockDeleteDir,
    moveFile: mockMoveFile,
    moveDir: mockMoveDir,
    copyFile: mockCopyFile,
    copyDir: mockCopyDir,
    getFileDownloadLink: mockGetFileDownloadLink,
    starFile: mockStarFile,
    unstarFile: mockUnstarFile,
    listStarredFiles: mockListStarredFiles,
    listShareLinks: mockListShareLinks,
    createShareLink: mockCreateShareLink,
    deleteShareLink: mockDeleteShareLink,
    listRepoShareItems: mockListRepoShareItems,
    listRepoGroupShares: mockListRepoGroupShares,
    shareToUser: mockShareToUser,
    shareToGroup: mockShareToGroup,
    removeUserShare: mockRemoveUserShare,
    removeGroupShare: mockRemoveGroupShare,
    listGroups: mockListGroups,
    createGroup: mockCreateGroup,
    listGroupRepos: mockListGroupRepos,
    listGroupMembers: mockListGroupMembers,
    setRepoPassword: mockSetRepoPassword,
    listActivities: mockListActivities,
    searchFiles: mockSearchFiles,
    listSharedRepos: mockListSharedRepos,
    listBeSharedRepos: mockListBeSharedRepos,
    searchUsers: mockSearchUsers,
  }));

  vi.mock('../../lib/oidc', () => ({
    isDevBypass: mockIsDevBypass,
    getOIDCLoginURL: mockGetOIDCLoginURL,
    exchangeOIDCCode: mockExchangeOIDCCode,
    setAuthToken: mockSetAuthToken,
  }));
}

/**
 * Resets all mock function call history and restores default implementations.
 */
export function resetMockAPI() {
  mockLogin.mockClear().mockResolvedValue('mock-token-123');
  mockGetAuthToken.mockClear().mockReturnValue('mock-token-123');
  mockSetAuthToken.mockClear();
  mockClearAuthToken.mockClear();
  mockListRepos.mockClear().mockResolvedValue(MOCK_REPOS);
  mockListDir.mockClear().mockResolvedValue(MOCK_DIRENTS);
  mockRenameFile.mockClear().mockResolvedValue(undefined);
  mockRenameDir.mockClear().mockResolvedValue(undefined);
  mockDeleteFile.mockClear().mockResolvedValue(undefined);
  mockDeleteDir.mockClear().mockResolvedValue(undefined);
  mockMoveFile.mockClear().mockResolvedValue(undefined);
  mockMoveDir.mockClear().mockResolvedValue(undefined);
  mockCopyFile.mockClear().mockResolvedValue(undefined);
  mockCopyDir.mockClear().mockResolvedValue(undefined);
  mockGetFileDownloadLink.mockClear().mockResolvedValue('http://localhost:8080/files/download/mock-link');
  mockStarFile.mockClear().mockResolvedValue(undefined);
  mockUnstarFile.mockClear().mockResolvedValue(undefined);
  mockListStarredFiles.mockClear().mockResolvedValue(MOCK_STARRED);
  mockListShareLinks.mockClear().mockResolvedValue(MOCK_SHARE_LINKS);
  mockCreateShareLink.mockClear().mockResolvedValue(MOCK_SHARE_LINKS[0]);
  mockDeleteShareLink.mockClear().mockResolvedValue(undefined);
  mockListRepoShareItems.mockClear().mockResolvedValue(MOCK_SHARE_ITEMS);
  mockListRepoGroupShares.mockClear().mockResolvedValue([]);
  mockShareToUser.mockClear().mockResolvedValue(undefined);
  mockShareToGroup.mockClear().mockResolvedValue(undefined);
  mockRemoveUserShare.mockClear().mockResolvedValue(undefined);
  mockRemoveGroupShare.mockClear().mockResolvedValue(undefined);
  mockListGroups.mockClear().mockResolvedValue(MOCK_GROUPS);
  mockCreateGroup.mockClear().mockResolvedValue(MOCK_GROUPS[0]);
  mockListGroupRepos.mockClear().mockResolvedValue(MOCK_GROUP_REPOS);
  mockListGroupMembers.mockClear().mockResolvedValue(MOCK_GROUP_MEMBERS);
  mockSetRepoPassword.mockClear().mockResolvedValue(undefined);
  mockListActivities.mockClear().mockResolvedValue({ events: MOCK_ACTIVITIES, more: false });
  mockSearchFiles.mockClear().mockResolvedValue({ results: MOCK_SEARCH_RESULTS, total: 3 });
  mockListSharedRepos.mockClear().mockResolvedValue([]);
  mockListBeSharedRepos.mockClear().mockResolvedValue([]);
  mockSearchUsers.mockClear().mockResolvedValue(MOCK_SEARCHED_USERS);
  mockIsDevBypass.mockClear().mockReturnValue(false);
  mockGetOIDCLoginURL.mockClear().mockResolvedValue('http://sso.example.com/authorize');
  mockExchangeOIDCCode.mockClear().mockResolvedValue('sso-token-456');
}
