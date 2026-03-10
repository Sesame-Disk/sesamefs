import { serviceURL } from './config';

const TOKEN_KEY = 'seahub_token';

export function getAuthToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function setAuthToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token);
}

export function clearAuthToken(): void {
  localStorage.removeItem(TOKEN_KEY);
}

function authHeaders(): Record<string, string> {
  const token = getAuthToken();
  return {
    'Authorization': `Token ${token}`,
    'Accept': 'application/json',
  };
}

export async function login(email: string, password: string): Promise<string> {
  const res = await fetch(`${serviceURL()}/api2/auth-token/`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({ username: email, password }),
  });

  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data.non_field_errors?.[0] || 'Login failed');
  }

  const data = await res.json();
  const token: string = data.token;
  setAuthToken(token);
  return token;
}

// Group types

export interface Group {
  id: number;
  name: string;
  owner: string;
  created_at: string;
  member_count: number;
}

export interface GroupMember {
  email: string;
  name: string;
  role: string;
  avatar_url: string;
}

export interface GroupRepo {
  repo_id: string;
  repo_name: string;
  permission: string;
  size: number;
  owner_email: string;
  owner_name: string;
  encrypted: boolean;
  last_modified: string;
  modifier_email: string;
  modifier_name: string;
}

// Group API methods

export async function listGroups(): Promise<Group[]> {
  const res = await fetch(`${serviceURL()}/api/v2.1/groups/`, {
    headers: authHeaders(),
  });
  if (!res.ok) throw new Error('Failed to load groups');
  const data = await res.json();
  return data as Group[];
}

export async function createGroup(name: string): Promise<Group> {
  const res = await fetch(`${serviceURL()}/api/v2.1/groups/`, {
    method: 'POST',
    headers: { ...authHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data.error_msg || 'Failed to create group');
  }
  return await res.json();
}

export async function listGroupRepos(groupId: string): Promise<GroupRepo[]> {
  const res = await fetch(`${serviceURL()}/api/v2.1/groups/${groupId}/libraries`, {
    headers: authHeaders(),
  });
  if (!res.ok) throw new Error('Failed to load group libraries');
  return await res.json();
}

export async function listGroupMembers(groupId: string): Promise<GroupMember[]> {
  const res = await fetch(`${serviceURL()}/api/v2.1/groups/${groupId}/members`, {
    headers: authHeaders(),
  });
  if (!res.ok) throw new Error('Failed to load group members');
  return await res.json();
}

// Encryption

export async function setRepoPassword(repoId: string, password: string): Promise<void> {
  const res = await fetch(`${serviceURL()}/api2/repos/${repoId}/`, {
    method: 'POST',
    headers: { ...authHeaders(), 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({ password }),
  });
  if (!res.ok) throw new Error('Incorrect password');
}

// File/Directory types

import type { Dirent, Repo } from './models';

// Repo API

export async function listRepos(): Promise<Repo[]> {
  const res = await fetch(`${serviceURL()}/api2/repos/`, {
    headers: authHeaders(),
  });
  if (!res.ok) throw new Error('Failed to load libraries');
  return await res.json();
}

// Directory listing

export async function listDir(repoId: string, path: string): Promise<Dirent[]> {
  const params = new URLSearchParams({ p: path });
  const res = await fetch(`${serviceURL()}/api2/repos/${repoId}/dir/?${params}`, {
    headers: authHeaders(),
  });
  if (!res.ok) throw new Error('Failed to load directory');
  return await res.json();
}

// Rename

export async function renameFile(repoId: string, path: string, newName: string): Promise<void> {
  const res = await fetch(`${serviceURL()}/api2/repos/${repoId}/file/?p=${encodeURIComponent(path)}`, {
    method: 'POST',
    headers: { ...authHeaders(), 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({ operation: 'rename', newname: newName }),
  });
  if (!res.ok) throw new Error('Failed to rename file');
}

export async function renameDir(repoId: string, path: string, newName: string): Promise<void> {
  const res = await fetch(`${serviceURL()}/api2/repos/${repoId}/dir/?p=${encodeURIComponent(path)}`, {
    method: 'POST',
    headers: { ...authHeaders(), 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({ operation: 'rename', newname: newName }),
  });
  if (!res.ok) throw new Error('Failed to rename folder');
}

// Delete

export async function deleteFile(repoId: string, path: string): Promise<void> {
  const res = await fetch(`${serviceURL()}/api2/repos/${repoId}/file/?p=${encodeURIComponent(path)}`, {
    method: 'DELETE',
    headers: authHeaders(),
  });
  if (!res.ok) throw new Error('Failed to delete file');
}

export async function deleteDir(repoId: string, path: string): Promise<void> {
  const res = await fetch(`${serviceURL()}/api2/repos/${repoId}/dir/?p=${encodeURIComponent(path)}`, {
    method: 'DELETE',
    headers: authHeaders(),
  });
  if (!res.ok) throw new Error('Failed to delete folder');
}

// Move / Copy

export async function moveFile(srcRepoId: string, srcPath: string, dstRepoId: string, dstDir: string): Promise<void> {
  const res = await fetch(`${serviceURL()}/api2/repos/${srcRepoId}/file/?p=${encodeURIComponent(srcPath)}`, {
    method: 'POST',
    headers: { ...authHeaders(), 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({ operation: 'move', dst_repo: dstRepoId, dst_dir: dstDir }),
  });
  if (!res.ok) throw new Error('Failed to move file');
}

export async function copyFile(srcRepoId: string, srcPath: string, dstRepoId: string, dstDir: string): Promise<void> {
  const res = await fetch(`${serviceURL()}/api2/repos/${srcRepoId}/file/?p=${encodeURIComponent(srcPath)}`, {
    method: 'POST',
    headers: { ...authHeaders(), 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({ operation: 'copy', dst_repo: dstRepoId, dst_dir: dstDir }),
  });
  if (!res.ok) throw new Error('Failed to copy file');
}

export async function moveDir(srcRepoId: string, srcPath: string, dstRepoId: string, dstDir: string): Promise<void> {
  const res = await fetch(`${serviceURL()}/api2/repos/${srcRepoId}/dir/?p=${encodeURIComponent(srcPath)}`, {
    method: 'POST',
    headers: { ...authHeaders(), 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({ operation: 'move', dst_repo: dstRepoId, dst_dir: dstDir }),
  });
  if (!res.ok) throw new Error('Failed to move folder');
}

export async function copyDir(srcRepoId: string, srcPath: string, dstRepoId: string, dstDir: string): Promise<void> {
  const res = await fetch(`${serviceURL()}/api2/repos/${srcRepoId}/dir/?p=${encodeURIComponent(srcPath)}`, {
    method: 'POST',
    headers: { ...authHeaders(), 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({ operation: 'copy', dst_repo: dstRepoId, dst_dir: dstDir }),
  });
  if (!res.ok) throw new Error('Failed to copy folder');
}

// File download link

export async function getFileDownloadLink(repoId: string, path: string): Promise<string> {
  const params = new URLSearchParams({ p: path });
  const res = await fetch(`${serviceURL()}/api2/repos/${repoId}/file/?${params}`, {
    headers: authHeaders(),
  });
  if (!res.ok) throw new Error('Failed to get download link');
  const url = await res.json();
  return url as string;
}

// Star / Unstar

export async function starFile(repoId: string, path: string): Promise<void> {
  const res = await fetch(`${serviceURL()}/api2/starredfiles/`, {
    method: 'POST',
    headers: { ...authHeaders(), 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({ repo_id: repoId, p: path }),
  });
  if (!res.ok) throw new Error('Failed to star file');
}

export async function unstarFile(repoId: string, path: string): Promise<void> {
  const params = new URLSearchParams({ repo_id: repoId, p: path });
  const res = await fetch(`${serviceURL()}/api2/starredfiles/?${params}`, {
    method: 'DELETE',
    headers: authHeaders(),
  });
  if (!res.ok) throw new Error('Failed to unstar file');
}

// Share link types

export interface ShareLink {
  token: string;
  link: string;
  repo_id: string;
  path: string;
  is_dir: boolean;
  is_expired: boolean;
  expire_date: string | null;
  permissions: {
    can_edit: boolean;
    can_download: boolean;
  };
  password?: string;
  ctime: string;
  view_cnt: number;
}

export interface ShareLinkOptions {
  password?: string;
  expire_days?: number;
  permissions?: {
    can_edit?: boolean;
    can_download?: boolean;
  };
}

// Share link API methods

export async function listShareLinks(repoId: string, path: string): Promise<ShareLink[]> {
  const params = new URLSearchParams({ repo_id: repoId, path });
  const res = await fetch(`${serviceURL()}/api/v2.1/share-links/?${params}`, {
    headers: authHeaders(),
  });
  if (!res.ok) throw new Error('Failed to list share links');
  return await res.json();
}

export async function createShareLink(repoId: string, path: string, options: ShareLinkOptions = {}): Promise<ShareLink> {
  const body: Record<string, unknown> = { repo_id: repoId, path };
  if (options.password) body.password = options.password;
  if (options.expire_days) body.expire_days = options.expire_days;
  if (options.permissions) body.permissions = options.permissions;
  const res = await fetch(`${serviceURL()}/api/v2.1/share-links/`, {
    method: 'POST',
    headers: { ...authHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data.error_msg || 'Failed to create share link');
  }
  return await res.json();
}

export async function deleteShareLink(token: string): Promise<void> {
  const res = await fetch(`${serviceURL()}/api/v2.1/share-links/${token}/`, {
    method: 'DELETE',
    headers: authHeaders(),
  });
  if (!res.ok) throw new Error('Failed to delete share link');
}

// Internal sharing types

export interface ShareItem {
  user_email: string;
  user_name: string;
  avatar_url: string;
  permission: string;
}

export interface GroupShareItem {
  group_id: number;
  group_name: string;
  permission: string;
}

export interface SearchedUser {
  email: string;
  name: string;
  avatar_url: string;
}

// Internal sharing API methods

export async function listRepoShareItems(repoId: string, path: string): Promise<ShareItem[]> {
  const params = new URLSearchParams({ repo_id: repoId, path });
  const res = await fetch(`${serviceURL()}/api/v2.1/repos/${repoId}/dir/shared_items/?${params}`, {
    headers: authHeaders(),
  });
  if (!res.ok) throw new Error('Failed to list shared items');
  const data = await res.json();
  return (data as any[]).filter(item => item.share_type === 'user');
}

export async function listRepoGroupShares(repoId: string, path: string): Promise<GroupShareItem[]> {
  const params = new URLSearchParams({ repo_id: repoId, path });
  const res = await fetch(`${serviceURL()}/api/v2.1/repos/${repoId}/dir/shared_items/?${params}`, {
    headers: authHeaders(),
  });
  if (!res.ok) throw new Error('Failed to list group shares');
  const data = await res.json();
  return (data as any[]).filter(item => item.share_type === 'group').map(item => ({
    group_id: item.group_id,
    group_name: item.group_name,
    permission: item.permission,
  }));
}

export async function shareToUser(repoId: string, path: string, email: string, permission: string): Promise<void> {
  const res = await fetch(`${serviceURL()}/api/v2.1/repos/${repoId}/dir/shared_items/`, {
    method: 'PUT',
    headers: { ...authHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify({ share_type: 'user', username: email, path, permission }),
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data.error_msg || 'Failed to share');
  }
}

export async function shareToGroup(repoId: string, path: string, groupId: number, permission: string): Promise<void> {
  const res = await fetch(`${serviceURL()}/api/v2.1/repos/${repoId}/dir/shared_items/`, {
    method: 'PUT',
    headers: { ...authHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify({ share_type: 'group', group_id: groupId, path, permission }),
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data.error_msg || 'Failed to share to group');
  }
}

export async function removeUserShare(repoId: string, path: string, email: string): Promise<void> {
  const params = new URLSearchParams({ share_type: 'user', username: email, path });
  const res = await fetch(`${serviceURL()}/api/v2.1/repos/${repoId}/dir/shared_items/?${params}`, {
    method: 'DELETE',
    headers: authHeaders(),
  });
  if (!res.ok) throw new Error('Failed to remove share');
}

export async function removeGroupShare(repoId: string, path: string, groupId: number): Promise<void> {
  const params = new URLSearchParams({ share_type: 'group', group_id: String(groupId), path });
  const res = await fetch(`${serviceURL()}/api/v2.1/repos/${repoId}/dir/shared_items/?${params}`, {
    method: 'DELETE',
    headers: authHeaders(),
  });
  if (!res.ok) throw new Error('Failed to remove group share');
}

export async function searchUsers(query: string): Promise<SearchedUser[]> {
  const params = new URLSearchParams({ q: query });
  const res = await fetch(`${serviceURL()}/api2/search-user/?${params}`, {
    headers: authHeaders(),
  });
  if (!res.ok) throw new Error('Failed to search users');
  const data = await res.json();
  return data.users || [];
}
