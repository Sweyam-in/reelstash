import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// reelstash-admin: ReelStash Admin (admin.html) plus the Files (files.html) and Downloads
// (downloads.html) workspaces that share its origin. The media library (app/) is a separate app;
// the dev server routes /admin/*, /files/* and /downloads/* to their entries.
const APP_ENTRY = [[/^\/admin(\/|$|\?)/, '/admin.html'], [/^\/files(\/|$|\?)/, '/files.html'], [/^\/downloads(\/|$|\?)/, '/downloads.html']];

function appEntries() {
  return {
    name: 'reelstash-admin-entries',
    configureServer(server) {
      server.middlewares.use((req, _res, next) => {
        const accept = req.headers.accept || '';
        const entry = req.method === 'GET' && accept.includes('text/html') && APP_ENTRY.find(([path]) => path.test(req.url));
        if (entry) req.url = entry[1];
        next();
      });
    },
  };
}

export default defineConfig({
  plugins: [react(), appEntries()],
  build: {
    target: ['es2020', 'chrome87', 'firefox78', 'safari14', 'edge88'],
    rollupOptions: { input: { admin: 'admin.html', files: 'files.html', downloads: 'downloads.html' } },
  },
  server: {
    port: 3001,
    proxy: {
      '/api': 'http://localhost:8902',
      '/auth': 'http://localhost:8902',
      '/healthz': 'http://localhost:8902',
    },
  },
});
