/** @type {import('tailwindcss').Config} */
const systemFonts = [
  'ui-sans-serif',
  'system-ui',
  '-apple-system',
  'BlinkMacSystemFont',
  'Segoe UI',
  'Helvetica Neue',
  'Arial',
  'Noto Sans',
  'sans-serif',
  'Apple Color Emoji',
  'Segoe UI Emoji',
  'Segoe UI Symbol',
  'Noto Color Emoji',
];

export default {
  darkMode: 'class',
  content: ['./src/**/*.{astro,html,js,jsx,md,mdx,svelte,ts,tsx,vue}'],
  theme: {
    extend: {
      colors: {
        primary: '#eb8205',
        'primary-button': '#e8780a',
        'primary-hover': '#f7931e',
        text: '#333',
        background: '#f5f5f5',
        border: '#e0e0e0',
        'dark-bg': '#1a1a1a',
        'dark-surface': '#2d2d2d',
        'dark-text': '#e0e0e0',
        'dark-border': '#404040',
      },
      fontFamily: {
        sans: ['Roboto', ...systemFonts],
      },
    },
  },
  plugins: [],
};
