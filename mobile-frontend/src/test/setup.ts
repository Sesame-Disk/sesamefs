import '@testing-library/jest-dom';

// Mock window.app.config and window.app.pageOptions with dev defaults
if (typeof window !== 'undefined') {
  window.app = {
    config: {
      siteRoot: '/',
      loginUrl: '/login/',
      serviceURL: 'http://localhost:3000',
      fileServerRoot: 'http://localhost:8080/seafhttp/',
      mediaUrl: '/static/',
      siteTitle: 'Sesame Disk',
      siteName: 'Sesame Disk',
      logoPath: 'img/logo.png',
      faviconPath: '/favicon.png',
      logoWidth: 147,
      logoHeight: 64,
      isPro: true,
      isDocs: false,
      useGoFileserver: true,
      seafileVersion: '11.0.0',
      lang: 'en',
      enableRepoAutoDel: true,
    },
    pageOptions: {
      name: 'Dev User',
      username: 'dev@sesamefs.local',
      contactEmail: 'dev@sesamefs.local',
      avatarURL: '/default-avatar.png',
      canAddRepo: true,
      canShareRepo: true,
      canAddGroup: true,
      canGenerateShareLink: true,
      canGenerateUploadLink: true,
    },
  };
}

// Mock localStorage
const localStorageMock = (() => {
  let store: Record<string, string> = {};
  return {
    getItem: (key: string) => store[key] ?? null,
    setItem: (key: string, value: string) => { store[key] = String(value); },
    removeItem: (key: string) => { delete store[key]; },
    clear: () => { store = {}; },
    get length() { return Object.keys(store).length; },
    key: (index: number) => Object.keys(store)[index] ?? null,
  };
})();

if (typeof window !== 'undefined') {
  Object.defineProperty(window, 'localStorage', { value: localStorageMock });
}

// Mock IntersectionObserver
class MockIntersectionObserver {
  readonly root: Element | null = null;
  readonly rootMargin: string = '';
  readonly thresholds: ReadonlyArray<number> = [];
  observe = vi.fn();
  unobserve = vi.fn();
  disconnect = vi.fn();
  takeRecords = vi.fn().mockReturnValue([]);
}

if (typeof window !== 'undefined') {
  Object.defineProperty(window, 'IntersectionObserver', { value: MockIntersectionObserver, writable: true, configurable: true });
}

// Mock matchMedia
if (typeof window !== 'undefined') {
  Object.defineProperty(window, 'matchMedia', {
    value: vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
    writable: true,
    configurable: true,
  });
}

// Mock navigator.share
if (typeof navigator !== 'undefined') {
  Object.defineProperty(navigator, 'share', {
    value: vi.fn().mockResolvedValue(undefined),
    configurable: true,
  });
  Object.defineProperty(navigator, 'clipboard', {
    value: {
      writeText: vi.fn().mockResolvedValue(undefined),
      readText: vi.fn().mockResolvedValue(''),
    },
    configurable: true,
  });
}

// Mock window.location
const locationMock = {
  href: 'http://localhost:3000/',
  search: '',
  pathname: '/',
  assign: vi.fn(),
  replace: vi.fn(),
  reload: vi.fn(),
  origin: 'http://localhost:3000',
  protocol: 'http:',
  host: 'localhost:3000',
  hostname: 'localhost',
  port: '3000',
  hash: '',
};

if (typeof window !== 'undefined') {
  Object.defineProperty(window, 'location', {
    value: locationMock,
    writable: true,
  });
}

// Mock URL.createObjectURL / revokeObjectURL
if (typeof URL !== 'undefined') {
  URL.createObjectURL = vi.fn().mockReturnValue('blob:mock-url');
  URL.revokeObjectURL = vi.fn();
}

// Reset mocks between tests
afterEach(() => {
  vi.restoreAllMocks();
  localStorageMock.clear();
});
