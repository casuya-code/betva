/* Betva service worker: Web Push, notification clicks, and a small
   runtime cache for same-origin static assets (JS/CSS/images/fonts).
   HTML is never cached, so deploys always reach users. */
const CACHE_NAME = 'betva-static-v2';
const ASSET_RE = /\.(?:js|css|svg|png|jpe?g|webp|avif|ico|woff2?|webmanifest)$/i;

self.addEventListener('install', () => self.skipWaiting());

self.addEventListener('activate', (event) => {
  event.waitUntil(
    (async () => {
      const keys = await caches.keys();
      await Promise.all(keys.filter((k) => k !== CACHE_NAME).map((k) => caches.delete(k)));
      await self.clients.claim();
    })()
  );
});

self.addEventListener('fetch', (event) => {
  const req = event.request;
  if (req.method !== 'GET') return;

  let url;
  try {
    url = new URL(req.url);
  } catch {
    return;
  }
  if (url.origin !== self.location.origin) return;
  if (!ASSET_RE.test(url.pathname)) return;

  event.respondWith(
    (async () => {
      const cached = await caches.match(req);
      if (cached) return cached;
      try {
        const res = await fetch(req);
        if (res.ok) {
          const copy = res.clone();
          caches.open(CACHE_NAME).then((c) => c.put(req, copy)).catch(() => {});
        }
        return res;
      } catch {
        return cached || Response.error();
      }
    })()
  );
});

self.addEventListener('push', (event) => {
  let data = {};
  try {
    data = event.data ? event.data.json() : {};
  } catch {
    data = { title: 'Betva', body: event.data ? event.data.text() : '' };
  }

  const title = data.title || 'Betva';
  const options = {
    body: data.body || '',
    icon: '/favicon.svg',
    badge: '/favicon.svg',
    tag: data.tag || 'betva',
    renotify: false,
    data: { url: data.link || '/' },
  };

  event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const target = new URL(event.notification.data?.url || '/', self.location.origin).href;

  event.waitUntil(
    (async () => {
      const clientList = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
      for (const client of clientList) {
        if (client.url === target && 'focus' in client) return client.focus();
      }
      const windows = await self.clients.matchAll({ type: 'window' });
      if (windows.length && 'navigate' in windows[0]) {
        await windows[0].focus();
        return windows[0].navigate(target);
      }
      return self.clients.openWindow(target);
    })()
  );
});