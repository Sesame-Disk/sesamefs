import { describe, it, expect, beforeEach } from 'vitest';
import { MemoryBackend } from '../storageBackend';
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

describe('MemoryBackend', () => {
  let backend: MemoryBackend;

  beforeEach(() => {
    backend = new MemoryBackend();
  });

  it('reports memory-fallback tier', () => {
    expect(backend.tier).toBe('memory-fallback');
  });

  // ─── Upload Queue ──────────────────────────────────────────

  it('enqueues and retrieves uploads', async () => {
    await backend.enqueueUpload({
      id: 'up-1',
      repoId: 'repo-1',
      dir: '/',
      fileName: 'photo.jpg',
    });
    const queued = await backend.getQueuedUploads();
    expect(queued).toHaveLength(1);
    expect(queued[0].id).toBe('up-1');
    expect(queued[0].status).toBe('queued');
    expect(queued[0].retryCount).toBe(0);
  });

  it('updates upload status', async () => {
    await backend.enqueueUpload({
      id: 'up-2',
      repoId: 'repo-1',
      dir: '/',
      fileName: 'doc.pdf',
    });
    await backend.updateUploadStatus('up-2', 'completed');
    const queued = await backend.getQueuedUploads();
    expect(queued).toHaveLength(0);
  });

  it('reports upload stats', async () => {
    await backend.enqueueUpload({ id: 'a', repoId: 'r', dir: '/', fileName: 'a.txt' });
    await backend.enqueueUpload({ id: 'b', repoId: 'r', dir: '/', fileName: 'b.txt' });
    await backend.updateUploadStatus('b', 'failed', 'network error');

    const stats = await backend.getUploadStats();
    expect(stats.queued).toBe(1);
    expect(stats.failed).toBe(1);
    expect(stats.total).toBe(2);
  });

  // ─── Share Ops Queue ───────────────────────────────────────

  it('enqueues and retrieves share ops', async () => {
    await backend.enqueueShareOp({
      id: 'sh-1',
      repoId: 'repo-1',
      opType: 'create-link',
      payload: '{"path":"/doc.pdf"}',
    });
    const ops = await backend.getQueuedShareOps();
    expect(ops).toHaveLength(1);
    expect(ops[0].opType).toBe('create-link');
  });

  it('updates share op status', async () => {
    await backend.enqueueShareOp({
      id: 'sh-2',
      repoId: 'repo-1',
      opType: 'delete-link',
      payload: '{}',
    });
    await backend.updateShareOpStatus('sh-2', 'completed');
    const ops = await backend.getQueuedShareOps();
    expect(ops).toHaveLength(0);
  });

  // ─── Cache ─────────────────────────────────────────────────

  it('caches and retrieves repos', async () => {
    await backend.cacheRepos(mockRepos);
    const result = await backend.getCachedRepos();
    expect(result).toEqual(mockRepos);
  });

  it('returns null for uncached repos', async () => {
    const result = await backend.getCachedRepos();
    expect(result).toBeNull();
  });

  it('caches and retrieves dirents', async () => {
    await backend.cacheDirents('repo-1', '/', mockDirents);
    const result = await backend.getCachedDirents('repo-1', '/');
    expect(result).toEqual(mockDirents);
  });

  it('returns null for uncached dirents', async () => {
    const result = await backend.getCachedDirents('nonexistent', '/');
    expect(result).toBeNull();
  });

  // ─── Purge ─────────────────────────────────────────────────

  it('purges completed items', async () => {
    await backend.enqueueUpload({ id: 'p1', repoId: 'r', dir: '/', fileName: '1.txt' });
    await backend.enqueueUpload({ id: 'p2', repoId: 'r', dir: '/', fileName: '2.txt' });
    await backend.updateUploadStatus('p1', 'completed');

    await backend.enqueueShareOp({ id: 's1', repoId: 'r', opType: 'x', payload: '{}' });
    await backend.updateShareOpStatus('s1', 'completed');

    await backend.purgeCompleted();

    const uploadStats = await backend.getUploadStats();
    expect(uploadStats.total).toBe(1);
    expect(uploadStats.queued).toBe(1);

    const shareStats = await backend.getShareOpStats();
    expect(shareStats.total).toBe(0);
  });
});
