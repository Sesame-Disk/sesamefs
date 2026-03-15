/**
 * StorageBackend - Abstract interface for persistent queue + cache storage.
 *
 * Three tiers:
 *   1. SqliteBackend   - SQLite WASM (IDB-backed or in-memory), full SQL power
 *   2. MemoryBackend   - Pure JS in-memory maps, wraps existing upload behavior
 *                        so uploads still work when WASM fails entirely
 *
 * Usage: import { getStorage } from './storageBackend';
 *        const backend = await getStorage();
 */

import type { SQLiteDB } from './db';
import { initDb, type DbTier } from './db';
import type { Repo, Dirent } from './models';

// ─── Types ──────────────────────────────────────────────────────────

export type UploadStatus = 'queued' | 'uploading' | 'completed' | 'failed' | 'cancelled';
export type ShareOpStatus = 'queued' | 'processing' | 'completed' | 'failed';

export interface QueuedUpload {
  id: string;
  repoId: string;
  dir: string;
  fileName: string;
  status: UploadStatus;
  error?: string;
  retryCount: number;
  blobKey?: string;
  createdAt: number;
  updatedAt: number;
}

export interface QueuedShareOp {
  id: string;
  repoId: string;
  opType: string;
  payload: string;
  status: ShareOpStatus;
  error?: string;
  retryCount: number;
  createdAt: number;
  updatedAt: number;
}

export interface QueueStats {
  queued: number;
  processing: number;
  completed: number;
  failed: number;
  total: number;
}

// ─── Interface ──────────────────────────────────────────────────────

export interface StorageBackend {
  readonly tier: DbTier | 'memory-fallback';

  // Upload queue
  enqueueUpload(upload: Omit<QueuedUpload, 'status' | 'retryCount' | 'createdAt' | 'updatedAt'>): Promise<void>;
  getQueuedUploads(): Promise<QueuedUpload[]>;
  updateUploadStatus(id: string, status: UploadStatus, error?: string): Promise<void>;
  getUploadStats(): Promise<QueueStats>;

  // Share operations queue
  enqueueShareOp(op: Omit<QueuedShareOp, 'status' | 'retryCount' | 'createdAt' | 'updatedAt'>): Promise<void>;
  getQueuedShareOps(): Promise<QueuedShareOp[]>;
  updateShareOpStatus(id: string, status: ShareOpStatus, error?: string): Promise<void>;
  getShareOpStats(): Promise<QueueStats>;

  // Cache
  cacheRepos(repos: Repo[]): Promise<void>;
  getCachedRepos(): Promise<Repo[] | null>;
  cacheDirents(repoId: string, path: string, dirents: Dirent[]): Promise<void>;
  getCachedDirents(repoId: string, path: string): Promise<Dirent[] | null>;

  // Maintenance
  purgeCompleted(): Promise<void>;
}

// ─── SQLite Backend ─────────────────────────────────────────────────

export class SqliteBackend implements StorageBackend {
  constructor(private db: SQLiteDB, public readonly tier: DbTier) {}

  async enqueueUpload(u: Omit<QueuedUpload, 'status' | 'retryCount' | 'createdAt' | 'updatedAt'>): Promise<void> {
    const now = Date.now();
    await this.db.run(
      `INSERT OR REPLACE INTO uploads (id, repo_id, dir, file_name, status, error, retry_count, blob_key, created_at, updated_at)
       VALUES (?, ?, ?, ?, 'queued', NULL, 0, ?, ?, ?)`,
      [u.id, u.repoId, u.dir, u.fileName, u.blobKey ?? null, now, now],
    );
  }

  async getQueuedUploads(): Promise<QueuedUpload[]> {
    const rows = await this.db.run(
      `SELECT * FROM uploads WHERE status IN ('queued', 'failed') ORDER BY created_at ASC`,
    );
    return rows.map(rowToUpload);
  }

  async updateUploadStatus(id: string, status: UploadStatus, error?: string): Promise<void> {
    await this.db.run(
      `UPDATE uploads SET status = ?, error = ?, updated_at = ? WHERE id = ?`,
      [status, error ?? null, Date.now(), id],
    );
  }

  async getUploadStats(): Promise<QueueStats> {
    const rows = await this.db.run(
      `SELECT status, COUNT(*) as cnt FROM uploads GROUP BY status`,
    );
    return rowsToStats(rows);
  }

  async enqueueShareOp(op: Omit<QueuedShareOp, 'status' | 'retryCount' | 'createdAt' | 'updatedAt'>): Promise<void> {
    const now = Date.now();
    await this.db.run(
      `INSERT OR REPLACE INTO share_ops (id, repo_id, op_type, payload, status, error, retry_count, created_at, updated_at)
       VALUES (?, ?, ?, ?, 'queued', NULL, 0, ?, ?)`,
      [op.id, op.repoId, op.opType, op.payload, now, now],
    );
  }

  async getQueuedShareOps(): Promise<QueuedShareOp[]> {
    const rows = await this.db.run(
      `SELECT * FROM share_ops WHERE status IN ('queued', 'failed') ORDER BY created_at ASC`,
    );
    return rows.map(rowToShareOp);
  }

  async updateShareOpStatus(id: string, status: ShareOpStatus, error?: string): Promise<void> {
    await this.db.run(
      `UPDATE share_ops SET status = ?, error = ?, updated_at = ? WHERE id = ?`,
      [status, error ?? null, Date.now(), id],
    );
  }

  async getShareOpStats(): Promise<QueueStats> {
    const rows = await this.db.run(
      `SELECT status, COUNT(*) as cnt FROM share_ops GROUP BY status`,
    );
    return rowsToStats(rows);
  }

  async cacheRepos(repos: Repo[]): Promise<void> {
    await this.db.run(
      `INSERT OR REPLACE INTO cached_repos (key, data, cached_at) VALUES (?, ?, ?)`,
      ['all', JSON.stringify(repos), Date.now()],
    );
  }

  async getCachedRepos(): Promise<Repo[] | null> {
    const rows = await this.db.run(
      `SELECT data FROM cached_repos WHERE key = ?`, ['all'],
    );
    if (rows.length === 0) return null;
    return JSON.parse(rows[0].data as string) as Repo[];
  }

  async cacheDirents(repoId: string, path: string, dirents: Dirent[]): Promise<void> {
    const key = `${repoId}:${path}`;
    await this.db.run(
      `INSERT OR REPLACE INTO cached_dirents (key, data, cached_at) VALUES (?, ?, ?)`,
      [key, JSON.stringify(dirents), Date.now()],
    );
  }

  async getCachedDirents(repoId: string, path: string): Promise<Dirent[] | null> {
    const key = `${repoId}:${path}`;
    const rows = await this.db.run(
      `SELECT data FROM cached_dirents WHERE key = ?`, [key],
    );
    if (rows.length === 0) return null;
    return JSON.parse(rows[0].data as string) as Dirent[];
  }

  async purgeCompleted(): Promise<void> {
    await this.db.run(`DELETE FROM uploads WHERE status IN ('completed', 'cancelled')`);
    await this.db.run(`DELETE FROM share_ops WHERE status = 'completed'`);
  }
}

// ─── Memory Backend (fallback) ──────────────────────────────────────

export class MemoryBackend implements StorageBackend {
  readonly tier = 'memory-fallback' as const;

  private uploads = new Map<string, QueuedUpload>();
  private shareOps = new Map<string, QueuedShareOp>();
  private repoCache: { data: Repo[]; cachedAt: number } | null = null;
  private direntCache = new Map<string, { data: Dirent[]; cachedAt: number }>();

  async enqueueUpload(u: Omit<QueuedUpload, 'status' | 'retryCount' | 'createdAt' | 'updatedAt'>): Promise<void> {
    const now = Date.now();
    this.uploads.set(u.id, {
      ...u,
      status: 'queued',
      retryCount: 0,
      createdAt: now,
      updatedAt: now,
    });
  }

  async getQueuedUploads(): Promise<QueuedUpload[]> {
    return [...this.uploads.values()]
      .filter(u => u.status === 'queued' || u.status === 'failed')
      .sort((a, b) => a.createdAt - b.createdAt);
  }

  async updateUploadStatus(id: string, status: UploadStatus, error?: string): Promise<void> {
    const u = this.uploads.get(id);
    if (u) {
      u.status = status;
      u.error = error;
      u.updatedAt = Date.now();
    }
  }

  async getUploadStats(): Promise<QueueStats> {
    return mapToStats(this.uploads);
  }

  async enqueueShareOp(op: Omit<QueuedShareOp, 'status' | 'retryCount' | 'createdAt' | 'updatedAt'>): Promise<void> {
    const now = Date.now();
    this.shareOps.set(op.id, {
      ...op,
      status: 'queued',
      retryCount: 0,
      createdAt: now,
      updatedAt: now,
    });
  }

  async getQueuedShareOps(): Promise<QueuedShareOp[]> {
    return [...this.shareOps.values()]
      .filter(o => o.status === 'queued' || o.status === 'failed')
      .sort((a, b) => a.createdAt - b.createdAt);
  }

  async updateShareOpStatus(id: string, status: ShareOpStatus, error?: string): Promise<void> {
    const o = this.shareOps.get(id);
    if (o) {
      o.status = status;
      o.error = error;
      o.updatedAt = Date.now();
    }
  }

  async getShareOpStats(): Promise<QueueStats> {
    return mapToStats(this.shareOps);
  }

  async cacheRepos(repos: Repo[]): Promise<void> {
    this.repoCache = { data: repos, cachedAt: Date.now() };
  }

  async getCachedRepos(): Promise<Repo[] | null> {
    return this.repoCache ? this.repoCache.data : null;
  }

  async cacheDirents(repoId: string, path: string, dirents: Dirent[]): Promise<void> {
    this.direntCache.set(`${repoId}:${path}`, { data: dirents, cachedAt: Date.now() });
  }

  async getCachedDirents(repoId: string, path: string): Promise<Dirent[] | null> {
    const entry = this.direntCache.get(`${repoId}:${path}`);
    return entry ? entry.data : null;
  }

  async purgeCompleted(): Promise<void> {
    for (const [id, u] of this.uploads) {
      if (u.status === 'completed' || u.status === 'cancelled') this.uploads.delete(id);
    }
    for (const [id, o] of this.shareOps) {
      if (o.status === 'completed') this.shareOps.delete(id);
    }
  }
}

// ─── Helpers ────────────────────────────────────────────────────────

type SQLiteCompatible = string | number | bigint | Uint8Array | null;

function rowToUpload(row: Record<string, SQLiteCompatible>): QueuedUpload {
  return {
    id: row.id as string,
    repoId: row.repo_id as string,
    dir: row.dir as string,
    fileName: row.file_name as string,
    status: row.status as UploadStatus,
    error: row.error as string | undefined,
    retryCount: Number(row.retry_count),
    blobKey: row.blob_key as string | undefined,
    createdAt: Number(row.created_at),
    updatedAt: Number(row.updated_at),
  };
}

function rowToShareOp(row: Record<string, SQLiteCompatible>): QueuedShareOp {
  return {
    id: row.id as string,
    repoId: row.repo_id as string,
    opType: row.op_type as string,
    payload: row.payload as string,
    status: row.status as ShareOpStatus,
    error: row.error as string | undefined,
    retryCount: Number(row.retry_count),
    createdAt: Number(row.created_at),
    updatedAt: Number(row.updated_at),
  };
}

function rowsToStats(rows: Record<string, SQLiteCompatible>[]): QueueStats {
  const stats: QueueStats = { queued: 0, processing: 0, completed: 0, failed: 0, total: 0 };
  for (const row of rows) {
    const status = row.status as string;
    const cnt = Number(row.cnt);
    if (status === 'queued') stats.queued = cnt;
    else if (status === 'uploading' || status === 'processing') stats.processing = cnt;
    else if (status === 'completed') stats.completed = cnt;
    else if (status === 'failed') stats.failed = cnt;
    stats.total += cnt;
  }
  return stats;
}

function mapToStats<T extends { status: string }>(map: Map<string, T>): QueueStats {
  const stats: QueueStats = { queued: 0, processing: 0, completed: 0, failed: 0, total: 0 };
  for (const item of map.values()) {
    if (item.status === 'queued') stats.queued++;
    else if (item.status === 'uploading' || item.status === 'processing') stats.processing++;
    else if (item.status === 'completed') stats.completed++;
    else if (item.status === 'failed') stats.failed++;
    stats.total++;
  }
  return stats;
}

// ─── Singleton ──────────────────────────────────────────────────────

let storagePromise: Promise<StorageBackend> | null = null;

/**
 * Get the storage backend singleton. Initializes on first call.
 * Always returns a working backend - falls back to MemoryBackend if WASM fails.
 */
export function getStorage(): Promise<StorageBackend> {
  if (storagePromise) return storagePromise;
  storagePromise = createStorage();
  return storagePromise;
}

async function createStorage(): Promise<StorageBackend> {
  try {
    const { db, tier } = await initDb();
    if (db) {
      const backend = new SqliteBackend(db, tier);
      console.log(`[storage] Active tier: ${tier}`);
      return backend;
    }
  } catch (err) {
    console.warn('[storage] SQLite init failed:', err);
  }

  console.log('[storage] Active tier: memory-fallback');
  return new MemoryBackend();
}

/**
 * Reset the storage singleton. Used in tests.
 */
export function resetStorage(): void {
  storagePromise = null;
}

/**
 * Create a SqliteBackend from an existing SQLiteDB instance (for testing).
 */
export function createSqliteBackend(db: SQLiteDB, tier: DbTier = 'memory'): SqliteBackend {
  return new SqliteBackend(db, tier);
}
