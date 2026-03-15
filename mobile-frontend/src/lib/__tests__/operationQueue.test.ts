import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import { OperationQueue } from '../operationQueue';
import type { QueueItem, QueueEvent } from '../operationQueue';

interface TestItem extends QueueItem {
  data: string;
}

function createTestQueue(overrides: Partial<{
  concurrency: number;
  maxRetries: number;
  items: TestItem[];
  executeFn: (item: TestItem) => Promise<void>;
}> = {}) {
  const items: TestItem[] = overrides.items ?? [];
  const statusLog: Array<{ id: string; status: string; error?: string }> = [];

  const queue = new OperationQueue<TestItem>({
    concurrency: overrides.concurrency ?? 2,
    maxRetries: overrides.maxRetries ?? 2,
    maxBackoffMs: 100, // Fast for tests
    fetchQueued: async () => [...items],
    execute: overrides.executeFn ?? (async () => {}),
    updateStatus: async (id, status, error) => {
      const item = items.find(i => i.id === id);
      if (item) {
        item.status = status;
        item.error = error;
      }
      statusLog.push({ id, status, error });
    },
    incrementRetry: async (id, retryCount) => {
      const item = items.find(i => i.id === id);
      if (item) item.retryCount = retryCount;
    },
  });

  return { queue, items, statusLog };
}

describe('OperationQueue', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('processes queued items', async () => {
    const executed: string[] = [];
    const { queue } = createTestQueue({
      items: [
        { id: '1', status: 'queued', retryCount: 0, data: 'a' },
        { id: '2', status: 'queued', retryCount: 0, data: 'b' },
      ],
      executeFn: async (item) => {
        executed.push(item.id);
      },
    });

    await queue.enqueue();
    // Let microtasks settle
    await vi.advanceTimersByTimeAsync(50);

    expect(executed).toContain('1');
    queue.destroy();
  });

  it('respects concurrency limit', async () => {
    let concurrent = 0;
    let maxConcurrent = 0;
    const resolvers: Array<() => void> = [];

    const { queue } = createTestQueue({
      concurrency: 2,
      items: [
        { id: '1', status: 'queued', retryCount: 0, data: 'a' },
        { id: '2', status: 'queued', retryCount: 0, data: 'b' },
        { id: '3', status: 'queued', retryCount: 0, data: 'c' },
      ],
      executeFn: async () => {
        concurrent++;
        maxConcurrent = Math.max(maxConcurrent, concurrent);
        await new Promise<void>(resolve => resolvers.push(resolve));
        concurrent--;
      },
    });

    await queue.enqueue();
    await vi.advanceTimersByTimeAsync(50);

    expect(maxConcurrent).toBeLessThanOrEqual(2);
    // Resolve all pending
    resolvers.forEach(r => r());
    await vi.advanceTimersByTimeAsync(50);

    queue.destroy();
  });

  it('pauses and resumes', async () => {
    const executed: string[] = [];
    const { queue, items } = createTestQueue({
      items: [
        { id: '1', status: 'queued', retryCount: 0, data: 'a' },
      ],
      executeFn: async (item) => {
        executed.push(item.id);
      },
    });

    queue.pause();
    await queue.enqueue();
    await vi.advanceTimersByTimeAsync(50);
    expect(executed).toHaveLength(0);

    // Add another item and resume
    items.push({ id: '2', status: 'queued', retryCount: 0, data: 'b' });
    await queue.resume();
    await vi.advanceTimersByTimeAsync(50);

    expect(executed.length).toBeGreaterThan(0);
    queue.destroy();
  });

  it('cancels a specific item', async () => {
    const { queue, statusLog } = createTestQueue({
      items: [
        { id: '1', status: 'queued', retryCount: 0, data: 'a' },
      ],
    });

    await queue.cancel('1');
    expect(statusLog.some(s => s.id === '1' && s.status === 'cancelled')).toBe(true);
    queue.destroy();
  });

  it('emits events to subscribers', async () => {
    const events: QueueEvent[] = [];
    const { queue } = createTestQueue({
      items: [
        { id: '1', status: 'queued', retryCount: 0, data: 'a' },
      ],
      executeFn: async () => {},
    });

    queue.subscribe(e => events.push(e));
    await queue.enqueue();
    await vi.advanceTimersByTimeAsync(50);

    const types = events.map(e => e.type);
    expect(types).toContain('started');
    queue.destroy();
  });

  it('retries failed items with backoff', async () => {
    let attempts = 0;
    const { queue } = createTestQueue({
      maxRetries: 2,
      items: [
        { id: '1', status: 'queued', retryCount: 0, data: 'a' },
      ],
      executeFn: async () => {
        attempts++;
        if (attempts < 3) throw new Error('temporary failure');
      },
    });

    await queue.enqueue();
    // First attempt + backoff + second attempt + backoff + third attempt
    await vi.advanceTimersByTimeAsync(500);

    expect(attempts).toBeGreaterThanOrEqual(2);
    queue.destroy();
  });

  it('marks item as failed after max retries', async () => {
    const { queue, statusLog } = createTestQueue({
      maxRetries: 1,
      items: [
        { id: '1', status: 'queued', retryCount: 0, data: 'a' },
      ],
      executeFn: async () => {
        throw new Error('permanent failure');
      },
    });

    await queue.enqueue();
    await vi.advanceTimersByTimeAsync(500);

    expect(statusLog.some(s => s.id === '1' && s.status === 'failed')).toBe(true);
    queue.destroy();
  });

  it('does not stop processing on error', async () => {
    const executed: string[] = [];
    const { queue } = createTestQueue({
      maxRetries: 0,
      items: [
        { id: '1', status: 'queued', retryCount: 0, data: 'a' },
        { id: '2', status: 'queued', retryCount: 0, data: 'b' },
      ],
      executeFn: async (item) => {
        if (item.id === '1') throw new Error('fail');
        executed.push(item.id);
      },
    });

    await queue.enqueue();
    await vi.advanceTimersByTimeAsync(200);

    expect(executed).toContain('2');
    queue.destroy();
  });

  it('unsubscribes correctly', async () => {
    const events: QueueEvent[] = [];
    const { queue } = createTestQueue({
      items: [{ id: '1', status: 'queued', retryCount: 0, data: 'a' }],
    });

    const unsub = queue.subscribe(e => events.push(e));
    unsub();

    await queue.enqueue();
    await vi.advanceTimersByTimeAsync(50);

    // Should not have received events after unsubscribe
    expect(events).toHaveLength(0);
    queue.destroy();
  });
});
