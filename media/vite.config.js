import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// reelstash-media: the media library (index.html). ReelStash Admin (admin/) is a separate app with
// its own build, tests, image and API server; the two only meet through links between origins
// (REELSTASH_MEDIA_URLS / REELSTASH_ADMIN_URLS tell each one where the other lives).
// In development it talks to the media API server (scripts/dev.sh starts it on :8902).
const api = process.env.REELSTASH_MEDIA_API || 'http://localhost:8902';

export default defineConfig({
  plugins: [react()],
  // Keep the bundle loadable on older phones and browsers (Safari 14+, Firefox 78+, Chrome 87+).
  build: { target: ['es2020', 'chrome87', 'firefox78', 'safari14', 'edge88'] },
  server: {
    port: 3000,
    proxy: {
      '/api': api,
      '/auth': api,
      '/healthz': api,
    },
  },
});
