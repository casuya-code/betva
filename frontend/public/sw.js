/* Betva service worker: Web Push, notification clicks, and a small
   runtime cache for same-origin static assets (JS/CSS/images/fonts).
   HTML is never cached, so deploys always reach users. */
const CACHE_NAME = 'betva-static-v3';
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
          // Await the write: the worker can be terminated right after the
          // response is returned, which used to make caching unreliable.
          await caches.open(CACHE_NAME).then((c) => c.put(req, copy)).catch(() => {});
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
  if (!data || typeof data !== 'object') data = {};

  const title = typeof data.title === 'string' && data.title ? data.title : 'Betva';
  const options = {
    body: typeof data.body === 'string' ? data.body : '',
    icon: '/favicon.svg',
    badge: '/favicon.svg',
    tag: typeof data.tag === 'string' && data.tag ? data.tag : 'betva',
    renotify: false,
    data: { url: typeof data.link === 'string' ? data.link : '/' },
  };

  event.waitUntil(self.registration.showNotification(title, options));
});

// Browsers rotate push subscriptions. Tell open tabs to re-subscribe with the
// current VAPID key; if nobody is open, leave a single reminder notification.
self.addEventListener('pushsubscriptionchange', (event) => {
  event.waitUntil(
    (async () => {
      const clients = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
      for (const client of clients) client.postMessage({ type: 'betva:push-expired' });
      if (clients.length === 0) {
        await self.registration.showNotification('Betva notifications need a refresh', {
          body: 'Open Betva and tap Refresh on your Profile to keep alerts working.',
          icon: '/favicon.svg',
          badge: '/favicon.svg',
          tag: 'betva-push-refresh',
          renotify: false,
          data: { url: '/profile' },
        });
      }
    })()
  );
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  // Only same-origin, absolute paths are valid in-app links. A bare "//host"
  // would resolve to another site and must never be used.
  const raw = event.notification.data && event.notification.data.url;
  const link = typeof raw === 'string' && raw.startsWith('/') && !raw.startsWith('//') ? raw : '/';
  const target = new URL(link, self.location.origin);

  event.waitUntil(
    (async () => {
      const clientList = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
      for (const client of clientList) {
        const current = new URL(client.url);
        if (
          current.origin === target.origin &&
          current.pathname === target.pathname &&
          current.search === target.search &&
          'focus' in client
        ) {
          return client.focus();
        }
      }
      const windows = await self.clients.matchAll({ type: 'window' });
      if (windows.length && 'navigate' in windows[0]) {
        await windows[0].focus();
        return windows[0].navigate(target.href);
      }
      return self.clients.openWindow(target.href);
    })()
  );
});
