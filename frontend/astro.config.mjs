// @ts-check
import { defineConfig } from 'astro/config';

import tailwindcss from '@tailwindcss/vite';

// https://astro.build/config
export default defineConfig({
  // Prefetch only the links that opt in (the bottom-nav routes). This keeps
  // navigation instant without downloading a full HTML page for every link
  // that merely scrolls into view — a real data cost on cheap phones.
  prefetch: {
    prefetchAll: false,
    defaultStrategy: 'hover',
  },
  // Smaller HTML output.
  compressHTML: true,
  build: {
    // Keep CSS in external files so the deployed CSP can forbid inline styles.
    inlineStylesheets: 'never',
  },
  vite: {
    build: {
      // Never inline JS or CSS: the deployed CSP allows only same-origin files.
      assetsInlineLimit: 0,
    },
    plugins: [tailwindcss()]
  }
});