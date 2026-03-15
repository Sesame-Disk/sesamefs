import { defineConfig } from 'astro/config';
import react from '@astrojs/react';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  integrations: [react()],
  output: 'static',
  site: 'http://localhost:4321',
  vite: {
    plugins: [tailwindcss()],
    optimizeDeps: {
      exclude: ['@subframe7536/sqlite-wasm'],
    },
    server: {
      proxy: {
        '/api2': 'http://localhost:3000',
        '/api/v2.1': 'http://localhost:3000',
        '/seafhttp': 'http://localhost:3000',
        '/media': 'http://localhost:3000',
        '/accounts': 'http://localhost:3000',
      },
      headers: {
        'Cross-Origin-Opener-Policy': 'same-origin',
        'Cross-Origin-Embedder-Policy': 'require-corp',
      },
    },
  },
});
