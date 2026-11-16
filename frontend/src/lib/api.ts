// The API origin is baked into a meta tag by the layout, so pages need no
// inline script (a strict CSP forbids inline script). window.BETVA_API is kept
// for compatibility and set once this module loads.
function resolveApiBase(): string {
  if (typeof document !== 'undefined') {
    const meta = document.querySelector<HTMLMetaElement>('meta[name="betva-api"]');
    if (meta?.content) return meta.content;
  }
  if (typeof window !== 'undefined' && (window as any).BETVA_API) {
    return (window as any).BETVA_API;
  }
  return 'http://localhost:8080';
}

export const API: string = resolveApiBase();
if (typeof window !== 'undefined') (window as any).BETVA_API = API;

const TOKEN_KEY = 'betva_token';

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(t: string) {
  localStorage.setItem(TOKEN_KEY, t);
}

export function clearToken() {
  localStorage.removeItem(TOKEN_KEY);
}

export async function authFetch(path: string, opts: RequestInit = {}): Promise<Response> {
  const res = await fetch(`${API}${path}`, {
    ...opts,
    headers: {
      ...(opts.headers || {}),
      Authorization: `Bearer ${getToken() || ''}`,
    },
  });
  if (res.status === 401) {
    clearToken();
    window.location.href = '/login';
    throw new Error('unauthorized');
  }
  return res;
}

export function esc(v: unknown): string {
  return String(v ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

export function money(cents: number | null | undefined): string {
  return Math.round((cents || 0) / 100).toLocaleString('en-TZ');
}

export const MARKET_LABEL: Record<string, string> = {
  ft_over_0_5: 'Full Time Over 0.5',
  ht_over_0_5: 'Half Time Over 0.5',
};

// Status text always ships with a mark, never colour alone.
export const BET_STATUS: Record<string, { cls: string; label: string }> = {
  pending: { cls: 'badge-pending', label: '⏳ PENDING' },
  win: { cls: 'badge-win', label: '✓ WIN' },
  loss: { cls: 'badge-loss', label: '✕ LOSS' },
  refunded: { cls: 'badge-postponed', label: '⟲ POSTPONED' },
};

export function betStatus(status: string): { cls: string; label: string } {
  return BET_STATUS[status] || BET_STATUS.pending;
}

export function toast(message: string, kind: 'ok' | 'error' = 'ok', ms = 3200) {
  const el = document.createElement('div');
  el.className = 'toast';
  el.setAttribute('role', 'status');
  el.textContent = message;
  if (kind === 'error') {
    el.style.borderColor = 'rgba(239,68,68,0.5)';
    el.style.color = '#fca5a5';
  }
  document.body.appendChild(el);
  setTimeout(() => el.remove(), ms);
}
