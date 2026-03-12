import { addPendingUpload, getPendingUploads, removePendingUpload } from './offlineDb';

let processing = false;

export async function queueFailedUpload(
  repoId: string,
  path: string,
  fileName: string,
  fileData: ArrayBuffer,
): Promise<void> {
  const id = `${repoId}:${path}:${fileName}:${Date.now()}`;
  await addPendingUpload({ id, repoId, path, fileName, fileData });
}

export async function processUploadQueue(
  uploadFn: (repoId: string, path: string, fileName: string, fileData: ArrayBuffer) => Promise<void>,
): Promise<void> {
  if (processing) return;
  processing = true;

  try {
    const pending = await getPendingUploads();
    for (const item of pending) {
      try {
        await uploadFn(item.repoId, item.path, item.fileName, item.fileData);
        await removePendingUpload(item.id);
      } catch {
        // Will retry on next online event
        break;
      }
    }
  } finally {
    processing = false;
  }
}

export async function getPendingUploadCount(): Promise<number> {
  const pending = await getPendingUploads();
  return pending.length;
}
