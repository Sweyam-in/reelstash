import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// reelstash-app: the media library (index.html). ReelStash Admin (admin/) is a separate app with
// its own build, tests and image; the two only meet over the HTTP API and links between origins
// (REELSTASH_MEDIA_URLS / REELSTASH_ADMIN_URLS tell each one where the other lives).
export default defineConfig({
  plugins: [react()],
  // Keep the bundle loadable on older phones and browsers (Safari 14+, Firefox 78+, Chrome 87+).
  build: { target: ['es2020', 'chrome87', 'firefox78', 'safari14', 'edge88'] },
  server: {
    port: 3000,
    proxy: {
      '/api': 'http://localhost:8902',
      '/auth': 'http://localhost:8902',
      '/healthz': 'http://localhost:8902',
    },
  },
});
