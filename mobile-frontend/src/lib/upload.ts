import { serviceURL } from './config';
import { getAuthToken } from './api';
import { getStorage } from './storageBackend';

// ─── Types ──────────────────────────────────────────────────────────

export type UploadStatus = 'queued' | 'uploading' | 'completed' | 'failed' | 'cancelled' | 'paused';

export interface UploadFile {
  id: string;
  file: File;
  repoId: string;
  parentDir: string;
  relativePath: string;
  progress: number;
  status: UploadStatus;
  error?: string;
  /** Bytes uploaded so far */
  bytesUploaded: number;
  /** Total file size */
  totalBytes: number;
  /** Upload speed in bytes/sec (rolling average) */
  speed: number;
  /** Estimated seconds remaining */
  eta: number;
  /** Number of completed chunks */
  chunksCompleted: number;
  /** Total number of chunks */
  chunksTotal: number;
}

export type UploadEventType = 'progress' | 'completed' | 'failed' | 'cancelled' | 'paused' | 'resumed' | 'queue-changed';

export interface UploadEvent {
  type: UploadEventType;
  fileId: string;
  progress?: number;
  error?: string;
}

type UploadListener = (event: UploadEvent) => void;

const CHUNK_SIZE = 2 * 1024 * 1024; // 2MB
const MAX_CONCURRENT = 3;
const MAX_RETRIES = 4;
const SPEED_SAMPLE_INTERVAL = 500; // ms

let idCounter = 0;

function generateId(): string {
  return `upload-${Date.now()}-${++idCounter}`;
}

// ─── Speed Tracker ──────────────────────────────────────────────────

class SpeedTracker {
  private samples: { time: number; bytes: number }[] = [];
  private windowMs = 5000; // 5-second rolling window

  record(bytes: number): void {
    const now = Date.now();
    this.samples.push({ time: now, bytes });
    // Prune old samples
    this.samples = this.samples.filter(s => now - s.time < this.windowMs);
  }

  getSpeed(): number {
    if (this.samples.length < 2) return 0;
    const oldest = this.samples[0];
    const newest = this.samples[this.samples.length - 1];
    const elapsed = (newest.time - oldest.time) / 1000;
    if (elapsed <= 0) return 0;
    const totalBytes = this.samples.reduce((sum, s) => sum + s.bytes, 0);
    return totalBytes / elapsed;
  }

  reset(): void {
    this.samples = [];
  }
}

// ─── Upload Manager ─────────────────────────────────────────────────

class UploadManager {
  private queue: UploadFile[] = [];
  private listeners: UploadListener[] = [];
  private abortControllers = new Map<string, AbortController>();
  private speedTrackers = new Map<string, SpeedTracker>();
  private retryCounts = new Map<string, number>();
  private processing = false;
  private persistenceEnabled = false;

  constructor() {
    this.initPersistence();
  }

  private async initPersistence(): Promise<void> {
    try {
      const storage = await getStorage();
      if (storage.tier !== 'memory-fallback') {
        this.persistenceEnabled = true;
      }
    } catch {
      // Persistence not available, uploads still work in-memory
    }
  }

  getQueue(): UploadFile[] {
    return [...this.queue];
  }

  getActiveCount(): number {
    return this.queue.filter(f => f.status === 'uploading').length;
  }

  subscribe(listener: UploadListener): () => void {
    this.listeners.push(listener);
    return () => {
      this.listeners = this.listeners.filter(l => l !== listener);
    };
  }

  private emit(event: UploadEvent): void {
    for (const listener of this.listeners) {
      try {
        listener(event);
      } catch {
        // Don't let listener errors break the upload flow
      }
    }
  }

  addFiles(files: File[], repoId: string, parentDir: string): UploadFile[] {
    const uploadFiles: UploadFile[] = files.map(file => {
      const totalChunks = Math.max(1, Math.ceil(file.size / CHUNK_SIZE));
      return {
        id: generateId(),
        file,
        repoId,
        parentDir,
        relativePath: (file as any).webkitRelativePath || file.name,
        progress: 0,
        status: 'queued' as const,
        bytesUploaded: 0,
        totalBytes: file.size,
        speed: 0,
        eta: 0,
        chunksCompleted: 0,
        chunksTotal: totalChunks,
      };
    });

    this.queue.push(...uploadFiles);

    // Persist to StorageBackend if available
    if (this.persistenceEnabled) {
      this.persistUploads(uploadFiles);
    }

    this.emit({ type: 'queue-changed', fileId: '' });
    this.processQueue();
    return uploadFiles;
  }

  private async persistUploads(uploads: UploadFile[]): Promise<void> {
    try {
      const storage = await getStorage();
      for (const u of uploads) {
        await storage.enqueueUpload({
          id: u.id,
          repoId: u.repoId,
          dir: u.parentDir,
          fileName: u.relativePath,
          blobKey: u.id,
        });
      }
    } catch {
      // Non-fatal: uploads continue in-memory
    }
  }

  pauseFile(fileId: string): void {
    const file = this.queue.find(f => f.id === fileId);
    if (!file || (file.status !== 'uploading' && file.status !== 'queued')) return;

    const controller = this.abortControllers.get(fileId);
    if (controller) {
      controller.abort();
      this.abortControllers.delete(fileId);
    }
    file.status = 'paused';
    file.speed = 0;
    file.eta = 0;
    this.emit({ type: 'paused', fileId });
    this.emit({ type: 'queue-changed', fileId: '' });
    this.processQueue();
  }

  resumeFile(fileId: string): void {
    const file = this.queue.find(f => f.id === fileId);
    if (!file || file.status !== 'paused') return;

    file.status = 'queued';
    this.speedTrackers.get(fileId)?.reset();
    this.emit({ type: 'resumed', fileId });
    this.emit({ type: 'queue-changed', fileId: '' });
    this.processQueue();
  }

  pauseAll(): void {
    for (const file of this.queue) {
      if (file.status === 'uploading' || file.status === 'queued') {
        this.pauseFile(file.id);
      }
    }
  }

  resumeAll(): void {
    for (const file of this.queue) {
      if (file.status === 'paused') {
        file.status = 'queued';
        this.speedTrackers.get(file.id)?.reset();
        this.emit({ type: 'resumed', fileId: file.id });
      }
    }
    this.emit({ type: 'queue-changed', fileId: '' });
    this.processQueue();
  }

  cancelFile(fileId: string): void {
    const file = this.queue.find(f => f.id === fileId);
    if (!file) return;

    const controller = this.abortControllers.get(fileId);
    if (controller) {
      controller.abort();
      this.abortControllers.delete(fileId);
    }
    file.status = 'cancelled';
    file.progress = 0;
    file.speed = 0;
    file.eta = 0;
    this.speedTrackers.delete(fileId);
    this.updatePersistenceStatus(fileId, 'cancelled');
    this.emit({ type: 'cancelled', fileId });
    this.emit({ type: 'queue-changed', fileId: '' });
    this.processQueue();
  }

  cancelAll(): void {
    for (const file of this.queue) {
      if (file.status === 'uploading' || file.status === 'queued' || file.status === 'paused') {
        const controller = this.abortControllers.get(file.id);
        if (controller) controller.abort();
        file.status = 'cancelled';
        file.progress = 0;
        file.speed = 0;
        file.eta = 0;
        this.updatePersistenceStatus(file.id, 'cancelled');
        this.emit({ type: 'cancelled', fileId: file.id });
      }
    }
    this.abortControllers.clear();
    this.speedTrackers.clear();
    this.emit({ type: 'queue-changed', fileId: '' });
  }

  retryFile(fileId: string): void {
    const file = this.queue.find(f => f.id === fileId);
    if (!file || file.status !== 'failed') return;

    file.status = 'queued';
    file.error = undefined;
    // Keep chunksCompleted so we resume from where we left off
    this.retryCounts.delete(fileId);
    this.emit({ type: 'queue-changed', fileId: '' });
    this.processQueue();
  }

  retryAllFailed(): void {
    for (const file of this.queue) {
      if (file.status === 'failed') {
        file.status = 'queued';
        file.error = undefined;
        this.retryCounts.delete(file.id);
      }
    }
    this.emit({ type: 'queue-changed', fileId: '' });
    this.processQueue();
  }

  clearCompleted(): void {
    const removed = this.queue.filter(f =>
      f.status === 'completed' || f.status === 'cancelled' || f.status === 'failed'
    );
    this.queue = this.queue.filter(f =>
      f.status !== 'completed' && f.status !== 'cancelled' && f.status !== 'failed'
    );
    for (const f of removed) {
      this.speedTrackers.delete(f.id);
      this.retryCounts.delete(f.id);
    }
    this.emit({ type: 'queue-changed', fileId: '' });
  }

  getStats(): { uploading: number; queued: number; completed: number; failed: number; paused: number; totalSpeed: number } {
    let uploading = 0, queued = 0, completed = 0, failed = 0, paused = 0, totalSpeed = 0;
    for (const f of this.queue) {
      switch (f.status) {
        case 'uploading': uploading++; totalSpeed += f.speed; break;
        case 'queued': queued++; break;
        case 'completed': completed++; break;
        case 'failed': failed++; break;
        case 'paused': paused++; break;
      }
    }
    return { uploading, queued, completed, failed, paused, totalSpeed };
  }

  // ─── Internal ───────────────────────────────────────────────────

  private processQueue(): void {
    if (this.processing) return;
    this.processing = true;

    try {
      const active = this.getActiveCount();
      const queued = this.queue.filter(f => f.status === 'queued');
      const slotsAvailable = MAX_CONCURRENT - active;
      const toStart = queued.slice(0, slotsAvailable);

      for (const file of toStart) {
        this.uploadFile(file);
      }
    } finally {
      this.processing = false;
    }
  }

  private async uploadFile(uploadFile: UploadFile): Promise<void> {
    uploadFile.status = 'uploading';
    this.emit({ type: 'queue-changed', fileId: uploadFile.id });

    const controller = new AbortController();
    this.abortControllers.set(uploadFile.id, controller);

    const tracker = new SpeedTracker();
    this.speedTrackers.set(uploadFile.id, tracker);

    try {
      // Small files (<=CHUNK_SIZE): single upload, same API as before
      if (uploadFile.file.size <= CHUNK_SIZE) {
        await this.uploadSingleFile(uploadFile, controller.signal, tracker);
      } else {
        await this.uploadChunkedFile(uploadFile, controller.signal, tracker);
      }

      uploadFile.status = 'completed';
      uploadFile.progress = 100;
      uploadFile.speed = 0;
      uploadFile.eta = 0;
      this.updatePersistenceStatus(uploadFile.id, 'completed');
      this.emit({ type: 'completed', fileId: uploadFile.id });
    } catch (err) {
      if (controller.signal.aborted) return; // paused or cancelled

      const retries = this.retryCounts.get(uploadFile.id) || 0;
      if (retries < MAX_RETRIES) {
        this.retryCounts.set(uploadFile.id, retries + 1);
        uploadFile.status = 'queued';
        uploadFile.speed = 0;
        uploadFile.eta = 0;
        // Don't reset progress/chunksCompleted - resume from last chunk
        const delay = Math.min(1000 * Math.pow(2, retries), 8000);
        setTimeout(() => this.processQueue(), delay);
      } else {
        uploadFile.status = 'failed';
        uploadFile.error = err instanceof Error ? err.message : 'Upload failed';
        uploadFile.speed = 0;
        uploadFile.eta = 0;
        this.updatePersistenceStatus(uploadFile.id, 'failed');
        this.emit({ type: 'failed', fileId: uploadFile.id, error: uploadFile.error });
      }
    } finally {
      this.abortControllers.delete(uploadFile.id);
      this.emit({ type: 'queue-changed', fileId: '' });
      this.processQueue();
    }
  }

  /** Standard single-POST upload for small files. Same API as old frontend. */
  private async uploadSingleFile(
    uploadFile: UploadFile,
    signal: AbortSignal,
    tracker: SpeedTracker,
  ): Promise<void> {
    const uploadLink = await this.getUploadLink(uploadFile.repoId, uploadFile.parentDir, signal);
    await this.performXhrUpload(uploadFile, uploadLink, signal, tracker);
  }

  /** Chunked upload: split file into 2MB chunks, upload sequentially, resume from last chunk. */
  private async uploadChunkedFile(
    uploadFile: UploadFile,
    signal: AbortSignal,
    tracker: SpeedTracker,
  ): Promise<void> {
    const uploadLink = await this.getUploadLink(uploadFile.repoId, uploadFile.parentDir, signal);
    const totalChunks = uploadFile.chunksTotal;

    for (let i = uploadFile.chunksCompleted; i < totalChunks; i++) {
      if (signal.aborted) throw new Error('Aborted');

      const start = i * CHUNK_SIZE;
      const end = Math.min(start + CHUNK_SIZE, uploadFile.file.size);
      const chunk = uploadFile.file.slice(start, end);
      const isLast = i === totalChunks - 1;

      await this.uploadChunk(uploadFile, uploadLink, chunk, i, totalChunks, isLast, signal, tracker);

      uploadFile.chunksCompleted = i + 1;
      uploadFile.bytesUploaded = end;
      uploadFile.progress = Math.round((end / uploadFile.totalBytes) * 100);

      const speed = tracker.getSpeed();
      uploadFile.speed = speed;
      const remaining = uploadFile.totalBytes - end;
      uploadFile.eta = speed > 0 ? remaining / speed : 0;

      this.emit({
        type: 'progress',
        fileId: uploadFile.id,
        progress: uploadFile.progress,
      });
    }
  }

  private uploadChunk(
    uploadFile: UploadFile,
    uploadLink: string,
    chunk: Blob,
    chunkIdx: number,
    totalChunks: number,
    isLast: boolean,
    signal: AbortSignal,
    tracker: SpeedTracker,
  ): Promise<void> {
    return new Promise((resolve, reject) => {
      if (signal.aborted) { reject(new Error('Aborted')); return; }

      const xhr = new XMLHttpRequest();
      const formData = new FormData();

      // Seafile chunked upload format
      formData.append('file', chunk, uploadFile.relativePath);
      formData.append('parent_dir', uploadFile.parentDir);
      formData.append('relative_path', '');
      // Chunk metadata for server
      formData.append('chunk_offset', String(chunkIdx * CHUNK_SIZE));
      if (isLast) {
        formData.append('is_last_chunk', 'true');
      }

      xhr.upload.addEventListener('progress', (e) => {
        if (e.lengthComputable) {
          tracker.record(e.loaded);
          // Update within-chunk progress
          const chunkProgress = e.loaded / e.total;
          const baseProgress = (chunkIdx * CHUNK_SIZE) / uploadFile.totalBytes;
          const chunkWeight = Math.min(CHUNK_SIZE, uploadFile.totalBytes - chunkIdx * CHUNK_SIZE) / uploadFile.totalBytes;
          const totalProgress = Math.round((baseProgress + chunkWeight * chunkProgress) * 100);
          uploadFile.progress = totalProgress;

          const speed = tracker.getSpeed();
          uploadFile.speed = speed;
          const bytesRemaining = uploadFile.totalBytes - (chunkIdx * CHUNK_SIZE + e.loaded);
          uploadFile.eta = speed > 0 ? bytesRemaining / speed : 0;

          this.emit({ type: 'progress', fileId: uploadFile.id, progress: totalProgress });
        }
      });

      xhr.addEventListener('load', () => {
        if (xhr.status >= 200 && xhr.status < 300) {
          resolve();
        } else {
          reject(new Error(`Chunk upload failed with status ${xhr.status}`));
        }
      });

      xhr.addEventListener('error', () => reject(new Error('Network error during chunk upload')));
      xhr.addEventListener('abort', () => reject(new Error('Aborted')));

      const onAbort = () => xhr.abort();
      signal.addEventListener('abort', onAbort, { once: true });

      const token = getAuthToken();
      xhr.open('POST', uploadLink);
      if (token) {
        xhr.setRequestHeader('Authorization', `Token ${token}`);
      }
      xhr.send(formData);
    });
  }

  /** Standard single-file XHR upload with progress tracking */
  private performXhrUpload(
    uploadFile: UploadFile,
    uploadLink: string,
    signal: AbortSignal,
    tracker: SpeedTracker,
  ): Promise<void> {
    return new Promise((resolve, reject) => {
      if (signal.aborted) { reject(new Error('Aborted')); return; }

      const xhr = new XMLHttpRequest();
      const formData = new FormData();
      formData.append('file', uploadFile.file, uploadFile.relativePath);
      formData.append('parent_dir', uploadFile.parentDir);
      formData.append('relative_path', '');

      xhr.upload.addEventListener('progress', (e) => {
        if (e.lengthComputable) {
          tracker.record(e.loaded);
          uploadFile.bytesUploaded = e.loaded;
          uploadFile.progress = Math.round((e.loaded / e.total) * 100);
          const speed = tracker.getSpeed();
          uploadFile.speed = speed;
          uploadFile.eta = speed > 0 ? (e.total - e.loaded) / speed : 0;
          this.emit({ type: 'progress', fileId: uploadFile.id, progress: uploadFile.progress });
        }
      });

      xhr.addEventListener('load', () => {
        if (xhr.status >= 200 && xhr.status < 300) resolve();
        else reject(new Error(`Upload failed with status ${xhr.status}`));
      });

      xhr.addEventListener('error', () => reject(new Error('Network error during upload')));
      xhr.addEventListener('abort', () => reject(new Error('Aborted')));

      const onAbort = () => xhr.abort();
      signal.addEventListener('abort', onAbort, { once: true });

      const token = getAuthToken();
      xhr.open('POST', uploadLink);
      if (token) {
        xhr.setRequestHeader('Authorization', `Token ${token}`);
      }
      xhr.send(formData);
    });
  }

  private async getUploadLink(repoId: string, parentDir: string, signal: AbortSignal): Promise<string> {
    const token = getAuthToken();
    const params = new URLSearchParams({ p: parentDir });
    const res = await fetch(`${serviceURL()}/api2/repos/${repoId}/upload-link/?${params}`, {
      headers: {
        'Authorization': `Token ${token}`,
        'Accept': 'application/json',
      },
      signal,
    });
    if (!res.ok) throw new Error('Failed to get upload link');
    return await res.json() as string;
  }

  private async updatePersistenceStatus(id: string, status: 'completed' | 'failed' | 'cancelled'): Promise<void> {
    if (!this.persistenceEnabled) return;
    try {
      const storage = await getStorage();
      await storage.updateUploadStatus(id, status);
    } catch {
      // Non-fatal
    }
  }
}

export const uploadManager = new UploadManager();

// ─── Helpers ────────────────────────────────────────────────────────

export function formatSpeed(bytesPerSec: number): string {
  if (bytesPerSec <= 0) return '';
  if (bytesPerSec < 1024) return `${Math.round(bytesPerSec)} B/s`;
  if (bytesPerSec < 1024 * 1024) return `${(bytesPerSec / 1024).toFixed(1)} KB/s`;
  return `${(bytesPerSec / (1024 * 1024)).toFixed(1)} MB/s`;
}

export function formatETA(seconds: number): string {
  if (seconds <= 0) return '';
  if (seconds < 60) return `${Math.ceil(seconds)}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${Math.ceil(seconds % 60)}s`;
  return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`;
}

export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}
