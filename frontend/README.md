# Betva Frontend

The user-facing app and the separate admin app for Betva. Astro 7 + Tailwind CSS v4, static output, no UI framework.

- **User routes:** `/` (Home), `/community`, `/history`, `/profile`, `/login`, `/register`
- **Admin routes:** `/admin` (Overview), `/admin/login`, `/admin/matches`, `/admin/users`, `/admin/analytics`, `/admin/ai`, `/admin/community`, `/admin/notifications`

For the product spec, API reference, and environment variables, see the root [`README.md`](../README.md).

## Requirements

- Node **22.23+** (see `package.json` → `engines`)
- The Go API running (default `http://localhost:8080`)

## Setup

```bash
npm install
npm run dev        # http://localhost:4321
```

The API base URL comes from `PUBLIC_API_URL`, defaulting to `http://localhost:8080`:

```bash
# .env
PUBLIC_API_URL=http://localhost:8080
```

Both layouts expose it as `window.BETVA_API`; `src/lib/api.ts` and `src/lib/admin.ts` read that value.

## Scripts

| Script | What it does |
| --- | --- |
| `npm run dev` | Astro dev server with HMR |
| `npm run build` | Static production build into `dist/` |
| `npm run preview` | Serve the production build locally |
| `npm run check` | `astro check` (strict typecheck) **and** a production build |

`npm run check` is the gate before committing frontend changes.

## Structure

```
src/
  components/
    BrandName.astro      Google-coloured "Betva" wordmark
    TrackRecord.astro    Aggregate tips / wins / losses / rate / streak card
  layouts/
    Layout.astro         User shell: meta, manifest, bottom nav, session reminder
    AdminLayout.astro    Admin shell: sidebar nav, admin token guard
  lib/
    api.ts               API base, token storage, authFetch, esc(), money(), betStatus(), toast()
    admin.ts             Admin token storage and adminFetch()
    push.ts              Service-worker registration, VAPID subscribe, opt-in prompt
  pages/
    index.astro          Home: Today / Tomorrow tabs, match list, bet slip
    community.astro      Feed, composer, reports, desktop rail
    history.astro        Last 6 months of bets, personal record rail
    profile.astro        Wallet, access rate, inbox, mutes, self-exclusion, password
    login.astro / register.astro
    admin/               Overview, login, matches, users, analytics, ai, community, notifications
  styles/
    global.css           Design tokens, badges, frames, nav, shared component classes
public/
  favicon.svg            Eagle-B mark (also the PWA icon)
  logo-dark.svg          Horizontal logo
  favicon.ico            Legacy icon — regenerate before launch
  manifest.webmanifest   PWA manifest
  sw.js                  Web Push + notification click handling
  matches-template.csv   CSV template for the admin Matches upload
scripts/
  responsive-audit.html  Layout/a11y measurement harness (kept out of public/)
```

## Conventions

- **Design tokens only.** Spacing (`--s1`…`--s12`), radii (`--r-sm`…`--r-pill`), type (`--t-xs`…`--t-2xl`), `--tap` (44px), and `--control` (48px) live in `src/styles/global.css`. Do not hardcode a new size.
- **Classes used by `innerHTML` markup must live in `global.css`.** Astro scopes `<style>` blocks with `data-astro-*`, which injected markup does not carry.
- **Money is cents.** Store and transport integer cents; format with `money()` (divides by 100, `en-TZ`).
- **Escape before injecting.** Use `esc()` for every user-controlled string in template literals.
- **No autoplay, no fake data.** Media uses `preload="none"`; the UI never fabricates activity or wins.
- **Dark only.** There is no light theme in v1.
- **Touch targets ≥ 44px** and no horizontal overflow at 320px.

## Responsive audit

1. Copy `scripts/responsive-audit.html` to `public/_responsive-audit.html`.
2. Open `http://localhost:4321/_responsive-audit.html`.
3. For protected routes, set `localStorage.betva_token` (user JWT) and `localStorage.betva_admin_token` (admin JWT) in the console first.
4. Run a batch of at most 4 routes × 6 widths:

```js
window.__audit(
  ['/', '/community', '/history', '/profile'],
  [320, 360, 768, 1024, 1280, 1440]
);
```

5. Measure an overlay: `window.__auditOverlay('/', 360, '#slip')`.
6. Delete `public/_responsive-audit.html` when finished so it never ships.

The audit reports horizontal overflow, elements past the viewport, tap targets under 44px, clipped text, and mismatched button heights per parent row. Last full run: 14 routes × 6 widths = 84 checks, 0 failures.

## Notes

- Auth is a bearer JWT in `localStorage`; there is no server-side logout denylist.
- The admin app is a separate shell but shares the same API and design tokens.
- The service worker handles push only — there is no offline page cache yet.
