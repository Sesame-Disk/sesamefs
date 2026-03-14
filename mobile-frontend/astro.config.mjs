import { defineConfig } from 'astro/config';
import react from '@astrojs/react';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  integrations: [react()],
  output: 'static',
  site: 'http://localhost:4321',
  vite: {
    plugins: [tailwindcss()],
    server: {
      proxy: {
        '/api2': 'http://localhost:3000',
        '/api/v2.1': 'http://localhost:3000',
        '/seafhttp': 'http://localhost:3000',
        '/media': 'http://localhost:3000',
        '/accounts': 'http://localhost:3000',
      },
    },
  },
});
