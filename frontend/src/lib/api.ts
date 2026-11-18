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

// Cache the public config (stake tiers, markets, payment number, fee) so pages
// render instantly on repeat visits and on slow connections. Nothing
// user-specific lives in it. A fresh cache is used as-is: a background refresh
// is throttled to at most once per REFRESH window, so opening several pages in
// a row does not spend data re-fetching a config that barely ever changes.
const CONFIG_CACHE_KEY = 'betva_config_cache';
const CONFIG_TTL_MS = 60 * 60 * 1000;
const CONFIG_REFRESH_MS = 10 * 60 * 1000;

let configCache: any = null;
let configFetchedAt = 0;
let configPromise: Promise<any> | null = null;

function readCachedConfig(): any | null {
  if (configCache) return configCache;
  if (typeof localStorage === 'undefined') return null;
  try {
    const raw = localStorage.getItem(CONFIG_CACHE_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object' || !parsed.t || Date.now() - parsed.t > CONFIG_TTL_MS) return null;
    configCache = parsed.v;
    configFetchedAt = parsed.t;
    return configCache;
  } catch {
    return null;
  }
}

function storeConfig(d: any): void {
  configCache = d;
  configFetchedAt = Date.now();
  try {
    localStorage.setItem(CONFIG_CACHE_KEY, JSON.stringify({ v: d, t: configFetchedAt }));
  } catch {
    /* quota or private mode */
  }
}

export async function fetchConfig(): Promise<any> {
  const cached = readCachedConfig();
  if (cached) {
    // Only refresh in the background when the cached copy is old enough to be
    // worth another request, and never run two refreshes at once.
    if (!configPromise && Date.now() - configFetchedAt >= CONFIG_REFRESH_MS) {
      configPromise = fetch(`${API}/api/v1/config`)
        .then((r) => (r.ok ? r.json() : null))
        .then((d) => {
          if (d) storeConfig(d);
          return d;
        })
        .catch(() => null)
        .finally(() => {
          configPromise = null;
        });
    }
    return cached;
  }
  if (configPromise) return configPromise;
  configPromise = fetch(`${API}/api/v1/config`)
    .then((r) => (r.ok ? r.json() : {}))
    .then((d) => {
      storeConfig(d);
      return d;
    })
    .catch(() => ({}))
    .finally(() => {
      configPromise = null;
    });
  return configPromise;
}

// The published match list changes only when an admin publishes, but it used
// to block every home-page visit on a network round trip. Cache the last good
// response and render it instantly, then refresh quietly. The payload differs
// for signed-in users (preview list), so the cache is keyed by auth scope.
const MATCHES_CACHE_KEY = 'betva_matches_cache';
const MATCHES_TTL_MS = 10 * 60 * 1000;
const MATCHES_REFRESH_MS = 2 * 60 * 1000;

type MatchesEnvelope = { v: any; t: number; s: string };
type MatchesCache = { data: any; at: number; scope: string };

let matchesCache: MatchesCache | null = null;
let matchesPromise: Promise<any> | null = null;
let matchesUpdateHandler: ((data: any) => void) | null = null;

function matchesScope(): string {
  try {
    return getToken() ? 'auth' : 'guest';
  } catch {
    return 'guest';
  }
}

export function setMatchesUpdateHandler(fn: ((data: any) => void) | null): void {
  matchesUpdateHandler = fn;
}

export function readCachedMatches(): any | null {
  const scope = matchesScope();
  // A signed-in response can be a 3-match preview or the full list depending
  // on account state, so only the identical anonymous payload is cacheable.
  if (scope !== 'guest') return null;
  if (matchesCache && matchesCache.scope === scope) return matchesCache.data;
  if (typeof localStorage === 'undefined') return null;
  try {
    const raw = localStorage.getItem(MATCHES_CACHE_KEY);
    if (!raw) return null;
    const parsed: MatchesEnvelope = JSON.parse(raw);
    if (!parsed || parsed.s !== scope || !parsed.v || typeof parsed.t !== 'number') return null;
    if (Date.now() - parsed.t > MATCHES_TTL_MS) return null;
    matchesCache = { data: parsed.v, at: parsed.t, scope };
    return parsed.v;
  } catch {
    return null;
  }
}

function storeMatches(data: any): void {
  const scope = matchesScope();
  if (scope !== 'guest') return;
  const now = Date.now();
  matchesCache = { data, at: now, scope };
  try {
    const envelope: MatchesEnvelope = { v: data, t: now, s: scope };
    localStorage.setItem(MATCHES_CACHE_KEY, JSON.stringify(envelope));
  } catch {
    /* quota or private mode */
  }
}

async function requestMatches(): Promise<any> {
  const req = matchesScope() === 'auth'
    ? authFetch('/api/v1/matches')
    : fetch(`${API}/api/v1/matches`);
  const res = await req;
  if (!res.ok) throw new Error('matches request failed');
  return res.json();
}

function refreshMatches(throwOnError: boolean): Promise<any> {
  if (matchesPromise) return matchesPromise;
  matchesPromise = requestMatches()
    .then((data) => {
      if (data && typeof data === 'object') {
        storeMatches(data);
        matchesUpdateHandler?.(data);
      }
      return data;
    })
    .catch((err) => {
      if (throwOnError) throw err;
      // A failed refresh must not leave the UI stuck on "Refreshing odds…".
      if (matchesCache) matchesUpdateHandler?.(matchesCache.data);
      return null;
    })
    .finally(() => {
      matchesPromise = null;
    });
  return matchesPromise;
}

export async function fetchMatches(): Promise<{ data: any; fromCache: boolean; refreshing: boolean }> {
  const cached = readCachedMatches();
  if (cached) {
    // Only spend data on a refresh when the cached copy is old enough.
    let refreshing = false;
    if (matchesCache && Date.now() - matchesCache.at >= MATCHES_REFRESH_MS) {
      void refreshMatches(false);
      refreshing = true;
    }
    return { data: cached, fromCache: true, refreshing };
  }
  const data = await refreshMatches(true);
  return { data: data || {}, fromCache: false, refreshing: false };
}

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

// Compact labels keep market buttons + odds inside a 320px screen.
export const MARKET_SHORT: Record<string, string> = {
  ft_over_0_5: 'FT O0.5',
  ht_over_0_5: 'HT O0.5',
};

// Odds travel as basis points (15000 = 1.50x), the same unit the API uses.
export function oddsText(bp: number | null | undefined): string {
  return ((bp || 0) / 10000).toFixed(2);
}

export function payoutFor(stakeCents: number, bp: number): number {
  return Math.floor((stakeCents * bp) / 10000);
}

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
  el.setAttribute('role', kind === 'error' ? 'alert' : 'status');
  el.textContent = message;
  if (kind === 'error') {
    el.style.borderColor = 'rgba(217,48,37,0.35)';
    el.style.color = 'var(--danger)';
  } else {
    el.style.borderColor = 'rgba(24,128,56,0.3)';
    el.style.color = 'var(--success-strong)';
  }
  document.body.appendChild(el);
  setTimeout(() => el.remove(), ms);
}
