import '@testing-library/jest-dom';

// Mock window.app.config and window.app.pageOptions with dev defaults
// Window.app type is declared in src/lib/config.ts
if (typeof window !== 'undefined') {
  window.app = {
    config: {
      apiUrl: 'http://localhost:3000',
      environment: 'development',
    },
    pageOptions: {},
  };
}
