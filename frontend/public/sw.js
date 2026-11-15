/* Betva service worker: Web Push and notification clicks.
   Payload is title + short body + link, a few hundred bytes. No images. */

self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (event) => event.waitUntil(self.clients.claim()));

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
