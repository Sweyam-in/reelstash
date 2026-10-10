import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

// Component and hook tests run in jsdom. Plain-logic tests (*.test.js) use node:test instead —
// `npm test` runs both.
export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    include: ['src/**/*.spec.{js,jsx}'],
    setupFiles: ['src/testing/setup.js'],
    css: false,
    restoreMocks: true,
    // Design-system packages ship ESM with CSS imports; inline them so vite handles both.
    server: { deps: { inline: [/@spatika/] } },
  },
});
