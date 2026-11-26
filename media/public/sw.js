// ReelStash media app service worker: makes the app installable and quick to open by
// caching the app shell. It never caches the API, sign-in or video: those always go
// to the server, so what people see stays current and private.
const VERSION = 'reelstash-shell-v1';
const SHELL = ['/', '/site.webmanifest', '/favicon.svg', '/web-app-manifest-192x192.png'];

self.addEventListener('install', event => {
  event.waitUntil(caches.open(VERSION).then(cache => cache.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener('activate', event => {
  event.waitUntil(
    caches.keys()
      .then(keys => Promise.all(keys.filter(key => key !== VERSION).map(key => caches.delete(key))))
      .then(() => self.clients.claim()),
  );
});

const passThrough = url => url.pathname.startsWith('/api/') || url.pathname.startsWith('/auth/') || url.pathname === '/healthz' || url.pathname === '/sw.js';

self.addEventListener('fetch', event => {
  const { request } = event;
  if (request.method !== 'GET' || request.headers.has('range')) return;
  const url = new URL(request.url);
  if (url.origin !== self.location.origin || passThrough(url)) return;

  // Pages: the network first so a new release shows at once; the cached shell when offline.
  if (request.mode === 'navigate') {
    event.respondWith(
      fetch(request)
        .then(response => {
          if (response.ok) caches.open(VERSION).then(cache => cache.put('/', response.clone()));
          return response;
        })
        .catch(() => caches.match('/')),
    );
    return;
  }

  // Built files have content hashes in their names, so a cached copy never goes stale.
  if (url.pathname.startsWith('/assets/')) {
    event.respondWith(
      caches.match(request).then(hit => hit || fetch(request).then(response => {
        if (response.ok) { const copy = response.clone(); caches.open(VERSION).then(cache => cache.put(request, copy)); }
        return response;
      })),
    );
    return;
  }

  // Icons and other public files: answer from the cache, refresh in the background.
  event.respondWith(
    caches.match(request).then(hit => {
      const refresh = fetch(request).then(response => {
        if (response.ok) { const copy = response.clone(); caches.open(VERSION).then(cache => cache.put(request, copy)); }
        return response;
      });
      return hit || refresh;
    }),
  );
});
