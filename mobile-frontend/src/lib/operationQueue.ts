/**
 * Generic operation queue processor with concurrency control,
 * exponential backoff retries, and visibility/online event resumption.
 */

export type OpStatus = 'queued' | 'processing' | 'completed' | 'failed' | 'cancelled';

export interface QueueItem {
  id: string;
  status: OpStatus;
  retryCount: number;
  error?: string;
}

export interface QueueStats {
  queued: number;
  processing: number;
  completed: number;
  failed: number;
  total: number;
}

export type QueueEvent =
  | { type: 'started'; id: string }
  | { type: 'completed'; id: string }
  | { type: 'failed'; id: string; error: string }
  | { type: 'retrying'; id: string; attempt: number }
  | { type: 'cancelled'; id: string }
  | { type: 'stats-changed'; stats: QueueStats };

export type QueueSubscriber = (event: QueueEvent) => void;

export interface OperationQueueOptions<T extends QueueItem> {
  /** Max concurrent operations */
  concurrency: number;
  /** Max retries per item (default: 4 => delays: 1s, 2s, 4s, 8s) */
  maxRetries?: number;
  /** Cap on backoff delay in ms (default: 8000) */
  maxBackoffMs?: number;
  /** Fetch queued items from storage */
  fetchQueued: () => Promise<T[]>;
  /** Execute a single operation. Throw on failure. */
  execute: (item: T) => Promise<void>;
  /** Update item status in storage */
  updateStatus: (id: string, status: OpStatus, error?: string) => Promise<void>;
  /** Increment retry count in storage (optional if storage auto-increments) */
  incrementRetry?: (id: string, retryCount: number) => Promise<void>;
}

export class OperationQueue<T extends QueueItem> {
  private opts: Required<Pick<OperationQueueOptions<T>, 'concurrency' | 'maxRetries' | 'maxBackoffMs'>> &
    OperationQueueOptions<T>;
  private active = new Map<string, AbortController>();
  private paused = false;
  private subscribers: QueueSubscriber[] = [];
  private retryTimers = new Map<string, ReturnType<typeof setTimeout>>();
  private processing = false;
  private onlineHandler: (() => void) | null = null;
  private visibilityHandler: (() => void) | null = null;

  constructor(options: OperationQueueOptions<T>) {
    this.opts = {
      maxRetries: 4,
      maxBackoffMs: 8000,
      ...options,
    };
    this.setupEventListeners();
  }

  // ─── Public API ─────────────────────────────────────────────────

  /** Add an item and start processing */
  async enqueue(): Promise<void> {
    if (!this.paused) {
      await this.processQueue();
    }
  }

  /** Pause queue processing (in-flight ops continue) */
  pause(): void {
    this.paused = true;
  }

  /** Resume queue processing */
  async resume(): Promise<void> {
    this.paused = false;
    await this.processQueue();
  }

  /** Cancel a specific operation */
  async cancel(id: string): Promise<void> {
    const controller = this.active.get(id);
    if (controller) {
      controller.abort();
      this.active.delete(id);
    }
    // Clear pending retry timer
    const timer = this.retryTimers.get(id);
    if (timer) {
      clearTimeout(timer);
      this.retryTimers.delete(id);
    }
    await this.opts.updateStatus(id, 'cancelled');
    this.emit({ type: 'cancelled', id });
    await this.emitStats();
  }

  /** Cancel all in-flight and queued operations */
  async cancelAll(): Promise<void> {
    for (const [id, controller] of this.active) {
      controller.abort();
      await this.opts.updateStatus(id, 'cancelled');
      this.emit({ type: 'cancelled', id });
    }
    this.active.clear();
    for (const [id, timer] of this.retryTimers) {
      clearTimeout(timer);
      await this.opts.updateStatus(id, 'cancelled');
    }
    this.retryTimers.clear();
    await this.emitStats();
  }

  /** Retry a specific failed item */
  async retry(id: string): Promise<void> {
    await this.opts.updateStatus(id, 'queued');
    await this.processQueue();
  }

  /** Retry all failed items */
  async retryAllFailed(): Promise<void> {
    const items = await this.opts.fetchQueued();
    for (const item of items) {
      if (item.status === 'failed') {
        await this.opts.updateStatus(item.id, 'queued');
      }
    }
    await this.processQueue();
  }

  /** Get current queue statistics */
  async getStats(): Promise<QueueStats> {
    const items = await this.opts.fetchQueued();
    const stats: QueueStats = { queued: 0, processing: 0, completed: 0, failed: 0, total: 0 };
    for (const item of items) {
      if (item.status === 'queued') stats.queued++;
      else if (item.status === 'processing') stats.processing++;
      else if (item.status === 'completed') stats.completed++;
      else if (item.status === 'failed') stats.failed++;
    }
    stats.processing += this.active.size;
    stats.total = stats.queued + stats.processing + stats.completed + stats.failed;
    return stats;
  }

  /** Subscribe to queue events. Returns unsubscribe function. */
  subscribe(fn: QueueSubscriber): () => void {
    this.subscribers.push(fn);
    return () => {
      this.subscribers = this.subscribers.filter(s => s !== fn);
    };
  }

  /** Remove completed/cancelled items from storage */
  async purgeCompleted(): Promise<void> {
    // Delegate to the storage backend (caller should handle this)
  }

  /** Tear down event listeners */
  destroy(): void {
    if (this.onlineHandler && typeof window !== 'undefined') {
      window.removeEventListener('online', this.onlineHandler);
    }
    if (this.visibilityHandler && typeof document !== 'undefined') {
      document.removeEventListener('visibilitychange', this.visibilityHandler);
    }
    for (const timer of this.retryTimers.values()) {
      clearTimeout(timer);
    }
    this.retryTimers.clear();
  }

  // ─── Internal ───────────────────────────────────────────────────

  private setupEventListeners(): void {
    if (typeof window !== 'undefined') {
      this.onlineHandler = () => {
        if (!this.paused) this.processQueue();
      };
      window.addEventListener('online', this.onlineHandler);
    }
    if (typeof document !== 'undefined') {
      this.visibilityHandler = () => {
        if (document.visibilityState === 'visible' && !this.paused) {
          this.processQueue();
        }
      };
      document.addEventListener('visibilitychange', this.visibilityHandler);
    }
  }

  private async processQueue(): Promise<void> {
    if (this.processing || this.paused) return;
    this.processing = true;

    try {
      while (this.active.size < this.opts.concurrency) {
        const items = await this.opts.fetchQueued();
        const queued = items.filter(
          i => i.status === 'queued' && !this.active.has(i.id) && !this.retryTimers.has(i.id),
        );
        if (queued.length === 0) break;

        const item = queued[0];
        this.startItem(item);
      }
    } finally {
      this.processing = false;
    }
  }

  private startItem(item: T): void {
    const controller = new AbortController();
    this.active.set(item.id, controller);

    this.opts.updateStatus(item.id, 'processing').then(() => {
      this.emit({ type: 'started', id: item.id });
      return this.opts.execute(item);
    }).then(async () => {
      this.active.delete(item.id);
      await this.opts.updateStatus(item.id, 'completed');
      this.emit({ type: 'completed', id: item.id });
      await this.emitStats();
      // Process next
      this.processing = false;
      await this.processQueue();
    }).catch(async (err) => {
      this.active.delete(item.id);

      if (controller.signal.aborted) return;

      const errorMsg = err instanceof Error ? err.message : String(err);
      const retryCount = (item.retryCount ?? 0) + 1;

      if (retryCount <= this.opts.maxRetries) {
        // Schedule retry with exponential backoff
        const delay = Math.min(1000 * Math.pow(2, retryCount - 1), this.opts.maxBackoffMs);
        if (this.opts.incrementRetry) {
          await this.opts.incrementRetry(item.id, retryCount);
        }
        await this.opts.updateStatus(item.id, 'queued', errorMsg);
        this.emit({ type: 'retrying', id: item.id, attempt: retryCount });

        const timer = setTimeout(async () => {
          this.retryTimers.delete(item.id);
          this.processing = false;
          await this.processQueue();
        }, delay);
        this.retryTimers.set(item.id, timer);
      } else {
        await this.opts.updateStatus(item.id, 'failed', errorMsg);
        this.emit({ type: 'failed', id: item.id, error: errorMsg });
      }

      await this.emitStats();
      // Try to fill remaining slots
      this.processing = false;
      await this.processQueue();
    });
  }

  private emit(event: QueueEvent): void {
    for (const fn of this.subscribers) {
      try {
        fn(event);
      } catch {
        // Don't let subscriber errors break the queue
      }
    }
  }

  private async emitStats(): Promise<void> {
    const stats = await this.getStats();
    this.emit({ type: 'stats-changed', stats });
  }
}
