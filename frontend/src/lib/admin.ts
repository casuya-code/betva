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

const TOKEN_KEY = 'betva_admin_token';

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(t: string) {
  localStorage.setItem(TOKEN_KEY, t);
}

export function clearToken() {
  localStorage.removeItem(TOKEN_KEY);
}

export function requireAdmin(): boolean {
  if (!getToken()) {
    window.location.href = '/admin/login';
    return false;
  }
  return true;
}

export async function adminFetch(path: string, opts: RequestInit = {}): Promise<Response> {
  const res = await fetch(`${API}${path}`, {
    ...opts,
    headers: {
      ...(opts.headers || {}),
      Authorization: `Bearer ${getToken() || ''}`,
    },
  });
  if (res.status === 401 || res.status === 403) {
    clearToken();
    window.location.href = '/admin/login';
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

export function money(cents: number): string {
  return (cents / 100).toLocaleString('en-TZ');
}

// Odds travel as basis points (15000 = 1.50x), the same unit the API uses.
export function oddsText(bp: number | null | undefined): string {
  return ((bp || 0) / 10000).toFixed(2);
}

export function oddsBadge(bp: number | null | undefined): string {
  return bp && bp > 0 ? `${oddsText(bp)}×` : '—';
}

export function todayISO(): string {
  const now = new Date();
  // EAT is UTC+3
  const eat = new Date(now.getTime() + 3 * 60 * 60 * 1000);
  return eat.toISOString().slice(0, 10);
}

export function statusBadge(status: string): string {
  const map: Record<string, string> = {
    active: 'badge-win',
    inactive: 'badge-pending',
    suspended: 'badge-live',
    banned: 'badge-loss',
    upcoming: 'badge-pending',
    live: 'badge-live',
    finished: 'badge-win',
    postponed: 'badge-postponed',
    cancelled: 'badge-loss',
  };
  return map[status] || 'badge-pending';
}

export function confidenceBadge(c: string): string {
  const map: Record<string, string> = {
    high: 'badge-win',
    medium: 'badge-live',
    low: 'badge-pending',
  };
  return map[c] || 'badge-pending';
}
