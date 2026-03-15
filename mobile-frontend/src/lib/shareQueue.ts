/**
 * Batch share queue - processes sharing operations for multiple files
 * using the OperationQueue with StorageBackend persistence.
 */

import { OperationQueue, type QueueItem, type QueueEvent, type QueueStats } from './operationQueue';
import { getStorage, type QueuedShareOp } from './storageBackend';
import { createShareLink, shareToUser, shareToGroup, type ShareLinkOptions } from './api';

// ─── Types ──────────────────────────────────────────────────────────

export type ShareType = 'link' | 'user' | 'group';

export interface ShareTask {
  id: string;
  repoId: string;
  path: string;
  fileName: string;
  shareType: ShareType;
  /** For 'user' type */
  userEmail?: string;
  /** For 'group' type */
  groupId?: number;
  /** For 'link' type */
  linkOptions?: ShareLinkOptions;
  /** Permission: 'r' or 'rw' */
  permission: string;
}

export interface ShareResult {
  taskId: string;
  path: string;
  fileName: string;
  success: boolean;
  shareLink?: string;
  error?: string;
}

interface ShareQueueItem extends QueueItem {
  repoId: string;
  path: string;
  fileName: string;
  shareType: ShareType;
  userEmail?: string;
  groupId?: number;
  linkOptions?: ShareLinkOptions;
  permission: string;
}

export type ShareQueueEvent = QueueEvent;
export type { QueueStats as ShareQueueStats };

// ─── Share Queue Manager ────────────────────────────────────────────

class ShareQueueManager {
  private queue: OperationQueue<ShareQueueItem> | null = null;
  private items = new Map<string, ShareQueueItem>();
  private results = new Map<string, ShareResult>();
  private listeners: ((event: ShareQueueEvent) => void)[] = [];
  private initPromise: Promise<void> | null = null;

  private async init(): Promise<void> {
    if (this.queue) return;
    if (this.initPromise) { await this.initPromise; return; }

    this.initPromise = this.doInit();
    await this.initPromise;
  }

  private async doInit(): Promise<void> {
    const storage = await getStorage();

    this.queue = new OperationQueue<ShareQueueItem>({
      concurrency: 2,
      maxRetries: 3,
      maxBackoffMs: 8000,

      fetchQueued: async () => {
        return [...this.items.values()];
      },

      execute: async (item: ShareQueueItem) => {
        await this.executeShare(item);
      },

      updateStatus: async (id, status, error) => {
        const item = this.items.get(id);
        if (item) {
          item.status = status as ShareQueueItem['status'];
          if (error) item.error = error;
        }
        // Persist to StorageBackend
        try {
          const mappedStatus = status === 'processing' ? 'processing' : status;
          await storage.updateShareOpStatus(id, mappedStatus as any, error);
        } catch {
          // Non-fatal
        }
      },

      incrementRetry: async (id, retryCount) => {
        const item = this.items.get(id);
        if (item) item.retryCount = retryCount;
      },
    });

    // Forward events to our listeners
    this.queue.subscribe((event) => {
      for (const fn of this.listeners) {
        try { fn(event); } catch {}
      }
    });
  }

  private async executeShare(item: ShareQueueItem): Promise<void> {
    switch (item.shareType) {
      case 'link': {
        const link = await createShareLink(item.repoId, item.path, item.linkOptions);
        this.results.set(item.id, {
          taskId: item.id,
          path: item.path,
          fileName: item.fileName,
          success: true,
          shareLink: link.link,
        });
        break;
      }
      case 'user': {
        if (!item.userEmail) throw new Error('User email required');
        await shareToUser(item.repoId, item.path, item.userEmail, item.permission);
        this.results.set(item.id, {
          taskId: item.id,
          path: item.path,
          fileName: item.fileName,
          success: true,
        });
        break;
      }
      case 'group': {
        if (!item.groupId) throw new Error('Group ID required');
        await shareToGroup(item.repoId, item.path, item.groupId, item.permission);
        this.results.set(item.id, {
          taskId: item.id,
          path: item.path,
          fileName: item.fileName,
          success: true,
        });
        break;
      }
    }
  }

  /** Enqueue batch share tasks */
  async addTasks(tasks: ShareTask[]): Promise<void> {
    await this.init();

    const storage = await getStorage();

    for (const task of tasks) {
      const item: ShareQueueItem = {
        id: task.id,
        status: 'queued',
        retryCount: 0,
        repoId: task.repoId,
        path: task.path,
        fileName: task.fileName,
        shareType: task.shareType,
        userEmail: task.userEmail,
        groupId: task.groupId,
        linkOptions: task.linkOptions,
        permission: task.permission,
      };
      this.items.set(task.id, item);

      // Persist
      try {
        await storage.enqueueShareOp({
          id: task.id,
          repoId: task.repoId,
          opType: task.shareType,
          payload: JSON.stringify({
            path: task.path,
            fileName: task.fileName,
            userEmail: task.userEmail,
            groupId: task.groupId,
            linkOptions: task.linkOptions,
            permission: task.permission,
          }),
        });
      } catch {
        // Non-fatal
      }
    }

    await this.queue!.enqueue();
  }

  async getStats(): Promise<QueueStats> {
    if (!this.queue) return { queued: 0, processing: 0, completed: 0, failed: 0, total: 0 };
    return this.queue.getStats();
  }

  getResults(): ShareResult[] {
    return [...this.results.values()];
  }

  getItems(): ShareQueueItem[] {
    return [...this.items.values()];
  }

  async cancelAll(): Promise<void> {
    if (this.queue) await this.queue.cancelAll();
  }

  async retryAllFailed(): Promise<void> {
    if (this.queue) await this.queue.retryAllFailed();
  }

  subscribe(fn: (event: ShareQueueEvent) => void): () => void {
    this.listeners.push(fn);
    return () => {
      this.listeners = this.listeners.filter(l => l !== fn);
    };
  }

  clear(): void {
    this.items.clear();
    this.results.clear();
  }

  destroy(): void {
    if (this.queue) this.queue.destroy();
    this.items.clear();
    this.results.clear();
  }
}

export const shareQueueManager = new ShareQueueManager();
