import { describe, it, expect, vi, beforeEach } from 'vitest';
import { shareFile, downloadFile } from '../share';

describe('shareFile', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('uses Web Share API when available', async () => {
    const shareMock = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'share', {
      value: shareMock,
      writable: true,
      configurable: true,
    });

    const result = await shareFile('test file', 'https://example.com/file');
    expect(shareMock).toHaveBeenCalledWith({ title: 'test file', url: 'https://example.com/file' });
    expect(result).toBe(true);
  });

  it('falls back to clipboard when share API is not available', async () => {
    Object.defineProperty(navigator, 'share', {
      value: undefined,
      writable: true,
      configurable: true,
    });

    const writeTextMock = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: writeTextMock },
      writable: true,
      configurable: true,
    });

    const result = await shareFile('test file', 'https://example.com/file');
    expect(writeTextMock).toHaveBeenCalledWith('https://example.com/file');
    expect(result).toBe(true);
  });

  it('returns false when share is cancelled', async () => {
    const shareMock = vi.fn().mockRejectedValue(new Error('cancelled'));
    Object.defineProperty(navigator, 'share', {
      value: shareMock,
      writable: true,
      configurable: true,
    });

    const result = await shareFile('test', 'https://example.com');
    expect(result).toBe(false);
  });
});

describe('downloadFile', () => {
  it('creates a temporary anchor element and clicks it', () => {
    const clickMock = vi.fn();
    const appendChildMock = vi.spyOn(document.body, 'appendChild').mockImplementation((node) => node);
    const removeChildMock = vi.spyOn(document.body, 'removeChild').mockImplementation((node) => node);
    const createElementSpy = vi.spyOn(document, 'createElement').mockReturnValue({
      href: '',
      download: '',
      style: { display: '' },
      click: clickMock,
    } as unknown as HTMLAnchorElement);

    downloadFile('https://example.com/file.zip', 'file.zip');

    expect(createElementSpy).toHaveBeenCalledWith('a');
    expect(clickMock).toHaveBeenCalled();
    expect(appendChildMock).toHaveBeenCalled();
    expect(removeChildMock).toHaveBeenCalled();

    createElementSpy.mockRestore();
    appendChildMock.mockRestore();
    removeChildMock.mockRestore();
  });
});
