import { defineConfig } from 'astro/config';
import react from '@astrojs/react';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  integrations: [react()],
  output: 'static',
  site: 'http://localhost:4321',
  vite: {
    plugins: [tailwindcss()],
  },
});
