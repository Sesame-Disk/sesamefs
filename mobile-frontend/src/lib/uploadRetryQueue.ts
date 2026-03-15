/**
 * @deprecated Use `OperationQueue` from `./operationQueue` with `getStorage()` instead.
 *
 * This module is kept for backward compatibility. It delegates to the new
 * StorageBackend which uses SQLite WASM when available, with graceful fallback.
 */

import { getStorage } from './storageBackend';

let processing = false;

/** @deprecated Use getStorage().enqueueUpload() */
export async function queueFailedUpload(
  repoId: string,
  path: string,
  fileName: string,
  _fileData: ArrayBuffer,
): Promise<void> {
  const id = `${repoId}:${path}:${fileName}:${Date.now()}`;
  const storage = await getStorage();
  await storage.enqueueUpload({ id, repoId, dir: path, fileName, blobKey: id });
}

/** @deprecated Use OperationQueue */
export async function processUploadQueue(
  uploadFn: (repoId: string, path: string, fileName: string, fileData: ArrayBuffer) => Promise<void>,
): Promise<void> {
  if (processing) return;
  processing = true;

  try {
    const storage = await getStorage();
    const pending = await storage.getQueuedUploads();
    for (const item of pending) {
      try {
        await uploadFn(item.repoId, item.dir, item.fileName, new ArrayBuffer(0));
        await storage.updateUploadStatus(item.id, 'completed');
      } catch {
        // Will retry on next online event
        break;
      }
    }
  } finally {
    processing = false;
  }
}

/** @deprecated Use getStorage().getUploadStats() */
export async function getPendingUploadCount(): Promise<number> {
  const storage = await getStorage();
  const stats = await storage.getUploadStats();
  return stats.queued + stats.failed;
}
