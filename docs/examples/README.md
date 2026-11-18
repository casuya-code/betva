# Media delivery examples

## Cloudflare R2 auth Worker (advanced, optional)

`cloudflare-r2-auth-worker.js` is a reference Worker that verifies a short-lived
HS256 token/JWT before serving an object from a bound R2 bucket.

- **When to use it:** private media where you want authorization at the edge and
  direct browser streaming from R2, without proxying bytes through the API.
- **Requires:** a Cloudflare-routed domain/Worker route and an R2 bucket binding.
  This is optional and separate from the app's default public direct delivery.
- **Public community media:** prefer direct R2 delivery (a public bucket via
  `r2.dev` or a custom domain set in `R2_MEDIA_PUBLIC_URL`). No Worker and no
  custom domain are required for that path.

Without a custom domain, Betva can still do direct R2 delivery using the bucket's
`r2.dev` public URL, or presigned/private delivery by setting
`R2_MEDIA_PRESIGN_TTL_SECONDS` (the API signs short-lived GET URLs; the browser
streams directly from R2). See `docs/DEPLOYMENT.md`.