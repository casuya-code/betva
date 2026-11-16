// @ts-check
import { defineConfig } from 'astro/config';

import tailwindcss from '@tailwindcss/vite';

// https://astro.build/config
export default defineConfig({
  // Prefetch same-origin pages on hover/viewport so navigation feels instant.
  prefetch: {
    prefetchAll: true,
    defaultStrategy: 'viewport',
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