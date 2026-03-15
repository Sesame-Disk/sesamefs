/**
 * @deprecated Use `getStorage()` from `./storageBackend` instead.
 *
 * This module is kept for backward compatibility. It delegates to the new
 * StorageBackend which uses SQLite WASM when available, with graceful fallback.
 */

import { getStorage } from './storageBackend';
import type { Repo, Dirent } from './models';

/** @deprecated Use getStorage().cacheRepos() */
export async function cacheRepos(repos: Repo[]): Promise<void> {
  const storage = await getStorage();
  await storage.cacheRepos(repos);
}

/** @deprecated Use getStorage().getCachedRepos() */
export async function getCachedRepos(): Promise<Repo[] | null> {
  const storage = await getStorage();
  return storage.getCachedRepos();
}

/** @deprecated Use getStorage().cacheDirents() */
export async function cacheDirents(repoId: string, path: string, dirents: Dirent[]): Promise<void> {
  const storage = await getStorage();
  await storage.cacheDirents(repoId, path, dirents);
}

/** @deprecated Use getStorage().getCachedDirents() */
export async function getCachedDirents(repoId: string, path: string): Promise<Dirent[] | null> {
  const storage = await getStorage();
  return storage.getCachedDirents(repoId, path);
}

/** @deprecated Use getStorage().enqueueUpload() */
export async function addPendingUpload(upload: {
  id: string;
  repoId: string;
  path: string;
  fileName: string;
  fileData: ArrayBuffer;
}): Promise<void> {
  const storage = await getStorage();
  await storage.enqueueUpload({
    id: upload.id,
    repoId: upload.repoId,
    dir: upload.path,
    fileName: upload.fileName,
    blobKey: upload.id, // File blobs stored separately in IndexedDB
  });
}

/** @deprecated Use getStorage().getQueuedUploads() */
export async function getPendingUploads() {
  const storage = await getStorage();
  const uploads = await storage.getQueuedUploads();
  // Return in the old format for backward compatibility
  return uploads.map(u => ({
    id: u.id,
    repoId: u.repoId,
    path: u.dir,
    fileName: u.fileName,
    fileData: new ArrayBuffer(0), // Blob data now stored in IndexedDB separately
    createdAt: u.createdAt,
  }));
}

/** @deprecated Use getStorage().updateUploadStatus() */
export async function removePendingUpload(id: string): Promise<void> {
  const storage = await getStorage();
  await storage.updateUploadStatus(id, 'completed');
}
