/* Betva service worker: Web Push, notification clicks, and a small
   runtime cache for same-origin static assets (JS/CSS/images/fonts) and a
   short-lived document cache for instant repeat navigation.

   Hashed /_astro/* files are immutable: cache-first forever.
   Unhashed files (theme-init.js, manifest, icons) may change on deploy, so
   they use stale-while-revalidate: served from cache immediately for speed
   and offline use, then refreshed quietly once they are older than
   REVALIDATE_MS. Updates reach returning users without adding latency to
   the render-blocking theme script. */
const STATIC_CACHE = 'betva-static-v5';
const HTML_CACHE = 'betva-html-v1';
const KNOWN_CACHES = [STATIC_CACHE, HTML_CACHE];
const ASSET_RE = /\.(?:js|css|svg|png|jpe?g|webp|avif|ico|woff2?|webmanifest)$/i;
const REVALIDATE_MS = 15 * 60 * 1000;
// HTML is short-lived: a cached document is served instantly only inside this
// window. Outside it we go to the network so a deploy is never masked by stale
// markup. Offline still falls back to the last good document.
const HTML_FRESH_MS = 60 * 1000;

self.addEventListener('install', () => self.skipWaiting());

self.addEventListener('activate', (event) => {
  event.waitUntil(
    (async () => {
      const keys = await caches.keys();
      await Promise.all(keys.filter((k) => !KNOWN_CACHES.includes(k)).map((k) => caches.delete(k)));
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

  if (req.mode === 'navigate' && req.destination === 'document') {
    event.respondWith(handleNavigation(req, event));
    return;
  }

  if (!ASSET_RE.test(url.pathname)) return;

  event.respondWith(
    (async () => {
      const cache = await caches.open(STATIC_CACHE);
      const cached = await cache.match(req);

      // Content-hashed build output never changes at a given URL.
      if (url.pathname.startsWith('/_astro/')) {
        if (cached) return cached;
        return fetchAndCache(req, cache, event);
      }

      // Unhashed assets: serve the cached copy first, refresh in background.
      if (cached) {
        const servedAt = Date.parse(cached.headers.get('date') || '') || 0;
        if (Date.now() - servedAt < REVALIDATE_MS) return cached;
        event.waitUntil(
          fetch(req, { cache: 'no-store' })
            .then((res) => (res.ok ? cache.put(req, res) : null))
            .catch(() => null)
        );
        return cached;
      }

      return fetchAndCache(req, cache, event);
    })()
  );
});

// Documents use a much shorter freshness window than immutable assets: the
// speed win is eliminating the network round trip for repeat navigation, but
// a deploy must not be hidden behind old markup for long.
async function handleNavigation(req, event) {
  const cache = await caches.open(HTML_CACHE);
  let cached = null;
  try {
    cached = await cache.match(req);
  } catch {
    /* cache unavailable */
  }

  if (cached) {
    const servedAt = Date.parse(cached.headers.get('date') || '') || 0;
    if (Date.now() - servedAt < HTML_FRESH_MS) {
      event.waitUntil(refreshHtml(req, cache));
      return cached;
    }
  }

  try {
    const res = await fetch(req);
    if (res.ok) event.waitUntil(cache.put(req, res.clone()).catch(() => {}));
    return res;
  } catch {
    if (cached) return cached;
    const shell = await cache.match('/').catch(() => null);
    return shell || Response.error();
  }
}

async function refreshHtml(req, cache) {
  try {
    const res = await fetch(req, { cache: 'no-store' });
    if (res.ok) await cache.put(req, res.clone());
  } catch {
    /* offline or cache unavailable: keep the existing copy */
  }
}

// Never hold the response open for a disk write: clone synchronously (before
// returning the original stream) and let waitUntil keep the worker alive.
async function fetchAndCache(req, cache, event) {
  try {
    const res = await fetch(req);
    if (res.ok) event.waitUntil(cache.put(req, res.clone()).catch(() => {}));
    return res;
  } catch {
    return Response.error();
  }
}

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
