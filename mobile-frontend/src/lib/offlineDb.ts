import { openDB } from 'idb';
import type { DBSchema, IDBPDatabase } from 'idb';
import type { Repo, Dirent } from './models';

interface SesameFSDB extends DBSchema {
  repos: {
    key: string;
    value: { data: Repo[]; cachedAt: number };
  };
  dirents: {
    key: string;
    value: { data: Dirent[]; cachedAt: number };
  };
  starred: {
    key: string;
    value: { data: unknown[]; cachedAt: number };
  };
  pendingUploads: {
    key: string;
    value: {
      id: string;
      repoId: string;
      path: string;
      fileName: string;
      fileData: ArrayBuffer;
      createdAt: number;
    };
  };
}

const DB_NAME = 'sesamefs-offline';
const DB_VERSION = 1;

let dbPromise: Promise<IDBPDatabase<SesameFSDB>> | null = null;

function getDb(): Promise<IDBPDatabase<SesameFSDB>> {
  if (!dbPromise) {
    dbPromise = openDB<SesameFSDB>(DB_NAME, DB_VERSION, {
      upgrade(db) {
        if (!db.objectStoreNames.contains('repos')) {
          db.createObjectStore('repos');
        }
        if (!db.objectStoreNames.contains('dirents')) {
          db.createObjectStore('dirents');
        }
        if (!db.objectStoreNames.contains('starred')) {
          db.createObjectStore('starred');
        }
        if (!db.objectStoreNames.contains('pendingUploads')) {
          db.createObjectStore('pendingUploads', { keyPath: 'id' });
        }
      },
    });
  }
  return dbPromise;
}

export async function cacheRepos(repos: Repo[]): Promise<void> {
  const db = await getDb();
  await db.put('repos', { data: repos, cachedAt: Date.now() }, 'all');
}

export async function getCachedRepos(): Promise<Repo[] | null> {
  const db = await getDb();
  const entry = await db.get('repos', 'all');
  return entry ? entry.data : null;
}

export async function cacheDirents(repoId: string, path: string, dirents: Dirent[]): Promise<void> {
  const db = await getDb();
  const key = `${repoId}:${path}`;
  await db.put('dirents', { data: dirents, cachedAt: Date.now() }, key);
}

export async function getCachedDirents(repoId: string, path: string): Promise<Dirent[] | null> {
  const db = await getDb();
  const key = `${repoId}:${path}`;
  const entry = await db.get('dirents', key);
  return entry ? entry.data : null;
}

export async function addPendingUpload(upload: {
  id: string;
  repoId: string;
  path: string;
  fileName: string;
  fileData: ArrayBuffer;
}): Promise<void> {
  const db = await getDb();
  await db.put('pendingUploads', { ...upload, createdAt: Date.now() });
}

export async function getPendingUploads() {
  const db = await getDb();
  return db.getAll('pendingUploads');
}

export async function removePendingUpload(id: string): Promise<void> {
  const db = await getDb();
  await db.delete('pendingUploads', id);
}
