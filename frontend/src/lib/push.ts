import { API, authFetch, getToken, toast } from './api';

const SW_READY_TIMEOUT = 8000;

export function pushSupported(): boolean {
  return (
    typeof navigator !== 'undefined' &&
    'serviceWorker' in navigator &&
    'PushManager' in window &&
    'Notification' in window
  );
}

export async function registerServiceWorker(): Promise<void> {
  if (typeof navigator === 'undefined' || !('serviceWorker' in navigator)) return;
  try {
    const timeout = new Promise<never>((_, reject) =>
      setTimeout(() => reject(new Error('service worker timeout')), SW_READY_TIMEOUT)
    );
    await Promise.race([navigator.serviceWorker.register('/sw.js'), timeout]);
  } catch {
    /* offline support is a bonus, never a blocker */
  }
}

// navigator.serviceWorker.ready can stay pending forever if registration
// failed. Never let the Enable button hang on it.
async function swRegistration(): Promise<ServiceWorkerRegistration | null> {
  if (typeof navigator === 'undefined' || !('serviceWorker' in navigator)) return null;
  try {
    const timeout = new Promise<null>((resolve) => setTimeout(() => resolve(null), SW_READY_TIMEOUT));
    return await Promise.race([navigator.serviceWorker.ready, timeout]);
  } catch {
    return null;
  }
}

function urlBase64ToUint8Array(base64String: string): Uint8Array<ArrayBuffer> {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4);
  const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/');
  const raw = atob(base64);
  const out = new Uint8Array(new ArrayBuffer(raw.length));
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

// P-256 public keys are 65 bytes. A bad key would create a subscription the
// server rejects, so fail early with a useful message.
function vapidKey(cfg: any): Uint8Array<ArrayBuffer> | null {
  if (typeof cfg?.vapid_public_key !== 'string') return null;
  try {
    const bytes = urlBase64ToUint8Array(cfg.vapid_public_key);
    return bytes.length === 65 ? bytes : null;
  } catch {
    return null;
  }
}

export type PushResult = { ok: boolean; message: string };

export async function enablePush(forceRefresh = false): Promise<PushResult> {
  if (!getToken()) return { ok: false, message: 'Sign in first.' };
  if (!pushSupported()) {
    return { ok: false, message: 'This browser cannot show push notifications. The in-app inbox still works.' };
  }
  if (Notification.permission === 'denied') {
    return {
      ok: false,
      message: 'Notifications are blocked in browser settings. Allow them there, then try again.',
    };
  }

  let cfg: any = {};
  try {
    const res = await fetch(`${API}/api/v1/config`);
    cfg = await res.json().catch(() => ({}));
  } catch {
    return { ok: false, message: 'Could not reach the server.' };
  }
  const key = vapidKey(cfg);
  if (!cfg.push_enabled || !key) {
    return { ok: false, message: 'Push is not configured on this server. The in-app inbox still works.' };
  }

  if (Notification.permission === 'default') {
    const permission = await Notification.requestPermission();
    if (permission !== 'granted') {
      return { ok: false, message: 'Notifications stay off. You can still read the inbox.' };
    }
  }

  try {
    await registerServiceWorker();
    const reg = await swRegistration();
    if (!reg) {
      return { ok: false, message: 'Could not start notifications in this browser. The inbox still works.' };
    }

    let sub = await reg.pushManager.getSubscription();
    const rawBefore = sub?.toJSON() as { endpoint?: string; keys?: { p256dh?: string; auth?: string } } | undefined;
    // Refresh is what recovers from VAPID key rotation or a browser-side
    // subscription replacement. Drop the old subscription before subscribing.
    if (sub && (forceRefresh || !rawBefore?.keys?.p256dh || !rawBefore?.keys?.auth)) {
      const endpoint = rawBefore?.endpoint;
      try {
        await sub.unsubscribe();
      } catch {
        /* the subscription may already be gone; the server prune handles it */
      }
      if (endpoint) {
        await authFetch('/api/v1/push/unsubscribe', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ endpoint }),
        }).catch(() => {});
      }
      sub = null;
    }

    if (!sub) {
      sub = await reg.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: key,
      });
    }
    const raw = sub.toJSON() as { endpoint?: string; keys?: { p256dh?: string; auth?: string } };
    if (!raw.endpoint || !raw.keys?.p256dh || !raw.keys?.auth) {
      return { ok: false, message: 'This browser returned an incomplete subscription. Please try again.' };
    }

    const res = await authFetch('/api/v1/push/subscribe', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        endpoint: raw.endpoint,
        p256dh: raw.keys.p256dh,
        auth: raw.keys.auth,
      }),
    });
    if (!res.ok) return { ok: false, message: 'Could not save the subscription. Please try again.' };
    localStorage.setItem('betva_push_asked', '1');
    return { ok: true, message: 'Notifications are on. Quiet hours 22:00–07:00 and a 2/day cap still apply.' };
  } catch {
    return { ok: false, message: 'Could not enable notifications. The in-app inbox still works.' };
  }
}

export async function disablePush(): Promise<PushResult> {
  if (!pushSupported() || Notification.permission !== 'granted') {
    return { ok: true, message: 'Notifications are off on this device.' };
  }
  const reg = await swRegistration();
  if (!reg) return { ok: true, message: 'Notifications are off on this device.' };
  try {
    const sub = await reg.pushManager.getSubscription();
    const endpoint = sub?.toJSON()?.endpoint;
    if (sub) await sub.unsubscribe();
    if (endpoint) {
      await authFetch('/api/v1/push/unsubscribe', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ endpoint }),
      }).catch(() => {});
    }
    return { ok: true, message: 'Notifications are off on this device.' };
  } catch {
    return { ok: false, message: 'Could not fully turn off notifications on this device.' };
  }
}

// The service worker tells open tabs when the browser rotates or drops a
// subscription. Re-subscribing here avoids silently losing push for good.
export function watchPushChanges() {
  if (typeof navigator === 'undefined' || !('serviceWorker' in navigator)) return;
  navigator.serviceWorker.addEventListener('message', async (event: MessageEvent) => {
    if (event.data?.type !== 'betva:push-expired') return;
    if (Notification.permission !== 'granted' || !getToken()) {
      toast('Notifications need a refresh. Open Profile to turn them on again.', 'error');
      return;
    }
    const result = await enablePush(true);
    toast(result.message, result.ok ? 'ok' : 'error');
  });
}

// maybeAskPush is called only after a meaningful action (first bet, first post),
// never on page load. A denied browser prompt is hard to reverse, so the custom
// banner asks first and the browser prompt only runs on an explicit tap.
export function maybeAskPush() {
  if (typeof window === 'undefined' || !getToken() || !pushSupported()) return;
  if (localStorage.getItem('betva_push_asked')) return;
  if (Notification.permission !== 'default') return;
  if (document.getElementById('pushPrompt')) return;

  const el = document.createElement('div');
  el.id = 'pushPrompt';
  el.className = 'nudge';
  el.setAttribute('role', 'region');
  el.setAttribute('aria-label', 'Enable notifications');
  el.innerHTML = `
    <div class="font-semibold mb-1">Get told when your match starts</div>
    <p class="text-sm text-[var(--text-2)] mb-3">One short message. Quiet hours 22:00–07:00 and a 2/day cap always apply.</p>
    <div class="flex gap-2">
      <button class="btn btn-primary flex-1" type="button" data-push="on">Turn on</button>
      <button class="btn btn-secondary" type="button" data-push="later">Not now</button>
    </div>
  `;
  document.body.appendChild(el);

  el.addEventListener('click', async (e) => {
    const btn = (e.target as HTMLElement).closest('[data-push]') as HTMLElement | null;
    if (!btn) return;
    const action = btn.dataset.push;
    if (action === 'later' || action === 'dismiss') {
      localStorage.setItem('betva_push_asked', '1');
      el.remove();
      return;
    }
    btn.setAttribute('disabled', 'true');
    const result = await enablePush();
    if (result.ok) {
      el.innerHTML = `<p class="text-sm text-[var(--success)]">${result.message}</p>`;
      setTimeout(() => el.remove(), 4200);
      return;
    }
    el.innerHTML = `
      <p class="text-sm text-[var(--text-2)]">${result.message}</p>
      <button class="btn btn-secondary w-full mt-2" type="button" data-push="dismiss">Got it</button>
    `;
  });
}
