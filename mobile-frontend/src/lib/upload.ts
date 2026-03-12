import { serviceURL } from './config';
import { getAuthToken } from './api';

export interface UploadFile {
  id: string;
  file: File;
  repoId: string;
  parentDir: string;
  relativePath: string;
  progress: number;
  status: 'queued' | 'uploading' | 'completed' | 'failed' | 'cancelled';
  error?: string;
  xhr?: XMLHttpRequest;
}

export type UploadEventType = 'progress' | 'completed' | 'failed' | 'cancelled' | 'queue-changed';

export interface UploadEvent {
  type: UploadEventType;
  fileId: string;
  progress?: number;
  error?: string;
}

type UploadListener = (event: UploadEvent) => void;

let idCounter = 0;

function generateId(): string {
  return `upload-${Date.now()}-${++idCounter}`;
}

class UploadManager {
  private queue: UploadFile[] = [];
  private maxConcurrent = 3;
  private maxRetries = 2;
  private listeners: UploadListener[] = [];
  private retryCounts = new Map<string, number>();

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

  private emit(event: UploadEvent) {
    for (const listener of this.listeners) {
      listener(event);
    }
  }

  addFiles(files: File[], repoId: string, parentDir: string): UploadFile[] {
    const uploadFiles: UploadFile[] = files.map(file => ({
      id: generateId(),
      file,
      repoId,
      parentDir,
      relativePath: (file as any).webkitRelativePath || file.name,
      progress: 0,
      status: 'queued' as const,
    }));

    this.queue.push(...uploadFiles);
    this.emit({ type: 'queue-changed', fileId: '' });
    this.processQueue();
    return uploadFiles;
  }

  cancelFile(fileId: string) {
    const file = this.queue.find(f => f.id === fileId);
    if (!file) return;

    if (file.status === 'uploading' && file.xhr) {
      file.xhr.abort();
    }
    file.status = 'cancelled';
    file.progress = 0;
    this.emit({ type: 'cancelled', fileId });
    this.emit({ type: 'queue-changed', fileId: '' });
    this.processQueue();
  }

  cancelAll() {
    for (const file of this.queue) {
      if (file.status === 'uploading' || file.status === 'queued') {
        if (file.xhr) file.xhr.abort();
        file.status = 'cancelled';
        file.progress = 0;
        this.emit({ type: 'cancelled', fileId: file.id });
      }
    }
    this.emit({ type: 'queue-changed', fileId: '' });
  }

  clearCompleted() {
    this.queue = this.queue.filter(f =>
      f.status !== 'completed' && f.status !== 'cancelled' && f.status !== 'failed'
    );
    this.emit({ type: 'queue-changed', fileId: '' });
  }

  private processQueue() {
    const active = this.getActiveCount();
    const queued = this.queue.filter(f => f.status === 'queued');

    const slotsAvailable = this.maxConcurrent - active;
    const toStart = queued.slice(0, slotsAvailable);

    for (const file of toStart) {
      this.uploadFile(file);
    }
  }

  private async uploadFile(uploadFile: UploadFile) {
    uploadFile.status = 'uploading';
    this.emit({ type: 'queue-changed', fileId: uploadFile.id });

    try {
      // Step 1: Get upload link
      const uploadLink = await this.getUploadLink(uploadFile.repoId, uploadFile.parentDir);

      // Step 2: Upload the file
      await this.performUpload(uploadFile, uploadLink);

      uploadFile.status = 'completed';
      uploadFile.progress = 100;
      this.emit({ type: 'completed', fileId: uploadFile.id });
    } catch (err) {
      if ((uploadFile.status as string) === 'cancelled') return;

      const retries = this.retryCounts.get(uploadFile.id) || 0;
      if (retries < this.maxRetries) {
        this.retryCounts.set(uploadFile.id, retries + 1);
        uploadFile.status = 'queued';
        uploadFile.progress = 0;
        this.emit({ type: 'queue-changed', fileId: uploadFile.id });
      } else {
        uploadFile.status = 'failed';
        uploadFile.error = err instanceof Error ? err.message : 'Upload failed';
        this.emit({ type: 'failed', fileId: uploadFile.id, error: uploadFile.error });
      }
    } finally {
      this.emit({ type: 'queue-changed', fileId: '' });
      this.processQueue();
    }
  }

  private async getUploadLink(repoId: string, parentDir: string): Promise<string> {
    const token = getAuthToken();
    const params = new URLSearchParams({ p: parentDir });
    const res = await fetch(`${serviceURL()}/api2/repos/${repoId}/upload-link/?${params}`, {
      headers: {
        'Authorization': `Token ${token}`,
        'Accept': 'application/json',
      },
    });
    if (!res.ok) throw new Error('Failed to get upload link');
    const url = await res.json();
    return url as string;
  }

  private performUpload(uploadFile: UploadFile, uploadLink: string): Promise<void> {
    return new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest();
      uploadFile.xhr = xhr;

      const formData = new FormData();
      formData.append('file', uploadFile.file, uploadFile.relativePath);
      formData.append('parent_dir', uploadFile.parentDir);
      formData.append('relative_path', '');

      xhr.upload.addEventListener('progress', (e) => {
        if (e.lengthComputable) {
          uploadFile.progress = Math.round((e.loaded / e.total) * 100);
          this.emit({ type: 'progress', fileId: uploadFile.id, progress: uploadFile.progress });
        }
      });

      xhr.addEventListener('load', () => {
        if (xhr.status >= 200 && xhr.status < 300) {
          resolve();
        } else {
          reject(new Error(`Upload failed with status ${xhr.status}`));
        }
      });

      xhr.addEventListener('error', () => reject(new Error('Network error during upload')));
      xhr.addEventListener('abort', () => reject(new Error('Upload aborted')));

      const token = getAuthToken();
      xhr.open('POST', uploadLink);
      if (token) {
        xhr.setRequestHeader('Authorization', `Token ${token}`);
      }
      xhr.send(formData);
    });
  }
}

export const uploadManager = new UploadManager();
