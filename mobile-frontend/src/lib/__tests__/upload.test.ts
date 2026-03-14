import { describe, it, expect, vi, beforeEach } from 'vitest';

// Must mock before importing upload module
vi.mock('../config', () => ({
  serviceURL: () => 'http://localhost:8080',
}));

vi.mock('../api', () => ({
  getAuthToken: () => 'test-token',
}));

// Import after mocks
import { uploadManager, type UploadEvent } from '../upload';

function createMockFile(name: string, size = 1024): File {
  const content = new ArrayBuffer(size);
  return new File([content], name, { type: 'application/octet-stream' });
}

describe('UploadManager', () => {
  beforeEach(() => {
    // Clear the queue
    uploadManager.cancelAll();
    uploadManager.clearCompleted();
  });

  it('starts with an empty queue', () => {
    expect(uploadManager.getQueue()).toEqual([]);
  });

  it('adds files to the queue', () => {
    // Mock fetch so upload link request doesn't fail
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve('http://localhost:8080/upload'),
    }));

    const file = createMockFile('test.txt');
    const result = uploadManager.addFiles([file], 'repo-1', '/');

    expect(result).toHaveLength(1);
    expect(result[0].file.name).toBe('test.txt');
    expect(result[0].repoId).toBe('repo-1');
    expect(result[0].parentDir).toBe('/');

    vi.restoreAllMocks();
  });

  it('generates unique IDs for each upload', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve('http://localhost:8080/upload'),
    }));

    const file1 = createMockFile('a.txt');
    const file2 = createMockFile('b.txt');
    const result = uploadManager.addFiles([file1, file2], 'repo-1', '/');

    expect(result[0].id).not.toBe(result[1].id);

    vi.restoreAllMocks();
  });

  it('emits queue-changed when files are added', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve('http://localhost:8080/upload'),
    }));

    const events: UploadEvent[] = [];
    const unsub = uploadManager.subscribe((e) => events.push(e));

    const file = createMockFile('test.txt');
    uploadManager.addFiles([file], 'repo-1', '/');

    expect(events.some(e => e.type === 'queue-changed')).toBe(true);

    unsub();
    vi.restoreAllMocks();
  });

  it('cancels a specific file', () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() => new Promise(() => {
      // never resolves - simulates pending upload
    })));

    const file = createMockFile('test.txt');
    const [upload] = uploadManager.addFiles([file], 'repo-1', '/');

    uploadManager.cancelFile(upload.id);

    const queue = uploadManager.getQueue();
    const cancelled = queue.find(f => f.id === upload.id);
    expect(cancelled?.status).toBe('cancelled');

    vi.restoreAllMocks();
  });

  it('cancels all uploads', () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() => new Promise(() => {})));

    const files = [createMockFile('a.txt'), createMockFile('b.txt')];
    uploadManager.addFiles(files, 'repo-1', '/');

    uploadManager.cancelAll();

    const queue = uploadManager.getQueue();
    expect(queue.every(f => f.status === 'cancelled')).toBe(true);

    vi.restoreAllMocks();
  });

  it('clears completed/cancelled/failed items from queue', () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() => new Promise(() => {})));

    const file = createMockFile('test.txt');
    uploadManager.addFiles([file], 'repo-1', '/');
    uploadManager.cancelAll();

    uploadManager.clearCompleted();

    expect(uploadManager.getQueue()).toEqual([]);

    vi.restoreAllMocks();
  });

  it('unsubscribe stops receiving events', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve('http://localhost:8080/upload'),
    }));

    const events: UploadEvent[] = [];
    const unsub = uploadManager.subscribe((e) => events.push(e));
    unsub();

    const file = createMockFile('test.txt');
    uploadManager.addFiles([file], 'repo-1', '/');

    expect(events).toEqual([]);

    vi.restoreAllMocks();
  });

  it('uses webkitRelativePath when available', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve('http://localhost:8080/upload'),
    }));

    const file = createMockFile('test.txt');
    Object.defineProperty(file, 'webkitRelativePath', { value: 'folder/test.txt' });
    const [upload] = uploadManager.addFiles([file], 'repo-1', '/');

    expect(upload.relativePath).toBe('folder/test.txt');

    vi.restoreAllMocks();
  });
});
