/// <reference types="vitest/globals" />
import { getFileExtension, getViewerType, isImageFile, isVideoFile, getVideoMimeType } from '../utils';

describe('getFileExtension', () => {
  it('returns extension for normal file', () => {
    expect(getFileExtension('photo.jpg')).toBe('jpg');
  });

  it('returns last extension for multiple dots', () => {
    expect(getFileExtension('archive.tar.gz')).toBe('gz');
  });

  it('returns empty string for no extension', () => {
    expect(getFileExtension('README')).toBe('');
  });

  it('lowercases extension', () => {
    expect(getFileExtension('File.PNG')).toBe('png');
  });
});

describe('getViewerType', () => {
  it('returns image for image files', () => {
    expect(getViewerType('photo.jpg')).toBe('image');
    expect(getViewerType('icon.svg')).toBe('image');
    expect(getViewerType('pic.webp')).toBe('image');
  });

  it('returns video for video files', () => {
    expect(getViewerType('clip.mp4')).toBe('video');
    expect(getViewerType('movie.webm')).toBe('video');
  });

  it('returns pdf for pdf files', () => {
    expect(getViewerType('doc.pdf')).toBe('pdf');
  });

  it('returns code for code files', () => {
    expect(getViewerType('app.ts')).toBe('code');
    expect(getViewerType('main.go')).toBe('code');
    expect(getViewerType('style.css')).toBe('code');
  });

  it('returns text for text files', () => {
    expect(getViewerType('notes.txt')).toBe('text');
    expect(getViewerType('readme.md')).toBe('text');
    expect(getViewerType('data.json')).toBe('text');
  });

  it('returns generic for unknown extensions', () => {
    expect(getViewerType('file.xyz')).toBe('generic');
  });

  it('returns generic for no extension', () => {
    expect(getViewerType('Makefile')).toBe('generic');
  });
});

describe('isImageFile', () => {
  it('returns true for image files', () => {
    expect(isImageFile('photo.png')).toBe(true);
    expect(isImageFile('icon.gif')).toBe(true);
  });

  it('returns false for non-image files', () => {
    expect(isImageFile('doc.pdf')).toBe(false);
    expect(isImageFile('app.js')).toBe(false);
  });
});

describe('isVideoFile', () => {
  it('returns true for video files', () => {
    expect(isVideoFile('clip.mp4')).toBe(true);
    expect(isVideoFile('movie.webm')).toBe(true);
  });

  it('returns false for non-video files', () => {
    expect(isVideoFile('photo.jpg')).toBe(false);
  });
});

describe('getVideoMimeType', () => {
  it('returns correct mime types', () => {
    expect(getVideoMimeType('clip.mp4')).toBe('video/mp4');
    expect(getVideoMimeType('clip.webm')).toBe('video/webm');
    expect(getVideoMimeType('clip.ogg')).toBe('video/ogg');
  });

  it('defaults to video/mp4 for unknown', () => {
    expect(getVideoMimeType('clip.avi')).toBe('video/mp4');
  });
});
