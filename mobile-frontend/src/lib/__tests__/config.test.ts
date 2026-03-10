import { describe, it, expect, beforeEach } from 'vitest';
import { getConfig, getPageOptions, parseBoolean } from '../config';

describe('parseBoolean', () => {
  it('handles boolean true', () => {
    expect(parseBoolean(true)).toBe(true);
  });

  it('handles boolean false', () => {
    expect(parseBoolean(false)).toBe(false);
  });

  it('handles string "True"', () => {
    expect(parseBoolean('True')).toBe(true);
  });

  it('handles string "False"', () => {
    expect(parseBoolean('False')).toBe(false);
  });

  it('handles arbitrary string as false', () => {
    expect(parseBoolean('anything')).toBe(false);
  });
});

describe('getConfig', () => {
  beforeEach(() => {
    // Reset window.app before each test
    delete (window as any).app;
  });

  it('returns dev defaults when window.app is undefined', () => {
    const config = getConfig();
    expect(config.siteRoot).toBe('/');
    expect(config.loginUrl).toBe('/login/');
    expect(config.serviceURL).toBe('http://localhost:8080');
    expect(config.siteTitle).toBe('Sesame Disk');
    expect(config.isPro).toBe(true);
    expect(config.isDocs).toBe(false);
    expect(config.lang).toBe('en');
  });

  it('returns window values when set', () => {
    (window as any).app = {
      config: {
        siteRoot: '/custom/',
        loginUrl: '/custom-login/',
        serviceURL: 'https://prod.example.com',
        siteTitle: 'Production',
        isPro: 'True',
        isDocs: 'False',
        lang: 'zh-cn',
      },
      pageOptions: {},
    };

    const config = getConfig();
    expect(config.siteRoot).toBe('/custom/');
    expect(config.loginUrl).toBe('/custom-login/');
    expect(config.serviceURL).toBe('https://prod.example.com');
    expect(config.siteTitle).toBe('Production');
    expect(config.isPro).toBe(true);
    expect(config.isDocs).toBe(false);
    expect(config.lang).toBe('zh-cn');
  });

  it('parses boolean string values from backend', () => {
    (window as any).app = {
      config: {
        isPro: 'False',
        isDocs: 'True',
      },
      pageOptions: {},
    };

    const config = getConfig();
    expect(config.isPro).toBe(false);
    expect(config.isDocs).toBe(true);
  });

  it('falls back to defaults for missing fields', () => {
    (window as any).app = {
      config: {
        siteRoot: '/partial/',
      },
      pageOptions: {},
    };

    const config = getConfig();
    expect(config.siteRoot).toBe('/partial/');
    expect(config.serviceURL).toBe('http://localhost:8080');
    expect(config.logoWidth).toBe(147);
  });
});

describe('getPageOptions', () => {
  beforeEach(() => {
    delete (window as any).app;
  });

  it('returns dev defaults when window.app is undefined', () => {
    const opts = getPageOptions();
    expect(opts.name).toBe('Dev User');
    expect(opts.username).toBe('dev@sesamefs.local');
    expect(opts.canAddRepo).toBe(true);
    expect(opts.maxFileName).toBe(255);
    expect(opts.enableOnlyoffice).toBe(false);
    expect(opts.storages).toEqual([]);
    expect(opts.thumbnailSizeForOriginal).toBe(1024);
  });

  it('returns window values when set', () => {
    (window as any).app = {
      config: {},
      pageOptions: {
        name: 'Test User',
        username: 'test@example.com',
        canAddRepo: 'True',
        enableOnlyoffice: 'False',
        maxFileName: 512,
        storages: ['s3'],
      },
    };

    const opts = getPageOptions();
    expect(opts.name).toBe('Test User');
    expect(opts.username).toBe('test@example.com');
    expect(opts.canAddRepo).toBe(true);
    expect(opts.enableOnlyoffice).toBe(false);
    expect(opts.maxFileName).toBe(512);
    expect(opts.storages).toEqual(['s3']);
  });

  it('parses boolean string values from backend', () => {
    (window as any).app = {
      config: {},
      pageOptions: {
        enableUploadFolder: 'True',
        isEmailConfigured: 'False',
        enableResetEncryptedRepoPassword: 'True',
      },
    };

    const opts = getPageOptions();
    expect(opts.enableUploadFolder).toBe(true);
    expect(opts.isEmailConfigured).toBe(false);
    expect(opts.enableResetEncryptedRepoPassword).toBe(true);
  });
});
