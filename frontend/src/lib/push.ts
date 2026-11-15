import { API, authFetch, getToken } from './api';

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
    await navigator.serviceWorker.register('/sw.js');
  } catch {
    /* offline support is a bonus, never a blocker */
  }
}

function urlBase64ToUint8Array(base64String: string): Uint8Array {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4);
  const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/');
  const raw = atob(base64);
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

export type PushResult = { ok: boolean; message: string };

export async function enablePush(): Promise<PushResult> {
  if (!getToken()) return { ok: false, message: 'Sign in first.' };
  if (!pushSupported()) {
    return { ok: false, message: 'This browser cannot show push notifications. The in-app inbox still works.' };
  }

  let cfg: any = {};
  try {
    cfg = await fetch(`${API}/api/v1/config`).then((r) => r.json());
  } catch {
    return { ok: false, message: 'Could not reach the server.' };
  }
  if (!cfg.push_enabled || !cfg.vapid_public_key) {
    return { ok: false, message: 'Push is not configured on this server. The in-app inbox still works.' };
  }

  const permission = await Notification.requestPermission();
  if (permission !== 'granted') {
    return { ok: false, message: 'Notifications stay off. You can still read the inbox.' };
  }

  try {
    await registerServiceWorker();
    const reg = await navigator.serviceWorker.ready;
    let sub = await reg.pushManager.getSubscription();
    if (!sub) {
      sub = await reg.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: urlBase64ToUint8Array(cfg.vapid_public_key),
      });
    }
    const raw = sub.toJSON() as any;
    const res = await authFetch('/api/v1/push/subscribe', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        endpoint: raw.endpoint,
        p256dh: raw.keys?.p256dh,
        auth: raw.keys?.auth,
      }),
    });
    if (!res.ok) return { ok: false, message: 'Could not save the subscription.' };
    localStorage.setItem('betva_push_asked', '1');
    return { ok: true, message: 'Notifications are on. Quiet hours 22:00–07:00 and a 2/day cap still apply.' };
  } catch {
    return { ok: false, message: 'Could not enable notifications. The in-app inbox still works.' };
  }
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
    if (btn.dataset.push === 'later') {
      localStorage.setItem('betva_push_asked', '1');
      el.remove();
      return;
    }
    const result = await enablePush();
    el.innerHTML = `<p class="text-sm ${result.ok ? 'text-green-400' : 'text-[var(--text-2)]'}">${result.message}</p>`;
    setTimeout(() => el.remove(), 4200);
  });
}
