import { describe, it, expect, beforeEach } from 'vitest';
import {
  cacheRepos,
  getCachedRepos,
  cacheDirents,
  getCachedDirents,
  addPendingUpload,
  getPendingUploads,
  removePendingUpload,
} from '../offlineDb';
import { resetStorage } from '../storageBackend';
import { resetDb } from '../db';
import type { Repo, Dirent } from '../models';

const mockRepos: Repo[] = [
  {
    repo_id: 'repo-1',
    repo_name: 'Test Library',
    size: 1024,
    permission: 'rw',
    owner_email: 'user@test.com',
    owner_name: 'Test',
    encrypted: false,
    last_modified: '2025-01-01T00:00:00Z',
  },
];

const mockDirents: Dirent[] = [
  {
    id: 'file-1',
    type: 'file',
    name: 'test.txt',
    size: 100,
    mtime: 1700000000,
    permission: 'rw',
  },
];

describe('offlineDb', () => {
  beforeEach(() => {
    // Reset singletons so each test gets a fresh MemoryBackend
    resetStorage();
    resetDb();
  });

  it('caches and retrieves repos', async () => {
    await cacheRepos(mockRepos);
    const result = await getCachedRepos();
    expect(result).toEqual(mockRepos);
  });

  it('returns null when no cached repos', async () => {
    const result = await getCachedRepos();
    expect(result).toBeNull();
  });

  it('caches and retrieves dirents', async () => {
    await cacheDirents('repo-1', '/', mockDirents);
    const result = await getCachedDirents('repo-1', '/');
    expect(result).toEqual(mockDirents);
  });

  it('returns null for uncached dirents', async () => {
    const result = await getCachedDirents('nonexistent', '/');
    expect(result).toBeNull();
  });

  it('adds and retrieves pending uploads', async () => {
    const upload = {
      id: 'upload-1',
      repoId: 'repo-1',
      path: '/',
      fileName: 'test.txt',
      fileData: new ArrayBuffer(10),
    };
    await addPendingUpload(upload);
    const pending = await getPendingUploads();
    expect(pending.length).toBeGreaterThanOrEqual(1);
    expect(pending.find((p) => p.id === 'upload-1')).toBeDefined();
  });

  it('removes pending upload', async () => {
    const upload = {
      id: 'upload-1',
      repoId: 'repo-1',
      path: '/',
      fileName: 'test.txt',
      fileData: new ArrayBuffer(10),
    };
    await addPendingUpload(upload);
    await removePendingUpload('upload-1');
    const pending = await getPendingUploads();
    expect(pending.find((p) => p.id === 'upload-1')).toBeUndefined();
  });
});
