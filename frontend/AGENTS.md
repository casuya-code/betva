## Development

When starting the dev server, use background mode:

```
astro dev --background
```

Manage the background server with `astro dev stop`, `astro dev status`, and `astro dev logs`.

Before committing: `npm run check` (strict `astro check` + production build). It must pass.

## Project conventions

Betva is a low-data betting + community app for Tanzania. Read the root `README.md` before changing product behaviour.

- **No UI framework.** Pages are `.astro` files with vanilla TypeScript in `<script>` blocks. Do not add React/Vue/Svelte for a small interaction.
- **API base:** `PUBLIC_API_URL`, default `http://localhost:8080`, exposed as `window.BETVA_API`. Use `authFetch` / `adminFetch` from `src/lib/`.
- **Tokens:** `localStorage.betva_token` (user) and `localStorage.betva_admin_token` (admin). A `401` clears the user token and redirects to `/login`.
- **Money:** integer cents everywhere. Format with `money()`. Never do float maths on money.
- **Escaping:** every user-controlled value interpolated into `innerHTML` goes through `esc()`.
- **Design tokens:** use `--s*`, `--r-*`, `--t-*`, `--tap`, `--control` from `src/styles/global.css`. Do not hardcode spacing, radii, or font sizes.
- **Dynamic markup classes:** classes used by `innerHTML`-injected markup must be defined in `global.css`; Astro-scoped styles will not match.
- **Buttons set height, not padding** (`min-height: var(--tap)`), so labels centre and rows align.
- **Status badges:** use the fixed `badge-win`, `badge-loss`, `badge-pending`, `badge-live`, `badge-postponed` classes. Never invent a status colour. Losses are always visible.
- **Low-data rules:** no autoplay, images `loading="lazy"`, video `preload="none"`, no background images, no fake activity.
- **Dark only.** Do not add a light theme without a product decision.
- **Responsive:** no horizontal overflow at 320px; every target ≥ 44px. Verify with `scripts/responsive-audit.html` (see `README.md`).
- **Community media:** only `/media/*` URLs are accepted in posts; do not add remote image embeds.

## Documentation

Full Astro documentation: https://docs.astro.build

Consult these guides before working on related tasks:

- [Adding pages, dynamic routes, or middleware](https://docs.astro.build/en/guides/routing/)
- [Working with Astro components](https://docs.astro.build/en/basics/astro-components/)
- [Using React, Vue, Svelte, or other framework components](https://docs.astro.build/en/guides/framework-components/)
- [Adding or managing content](https://docs.astro.build/en/guides/content-collections/)
- [Adding styles or using Tailwind](https://docs.astro.build/en/guides/styling/)
- [Supporting multiple languages](https://docs.astro.build/en/guides/internationalization/)
