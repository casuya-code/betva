/**
 * Example: Cloudflare Worker to authorize private R2 media with a signed token/JWT.
 *
 * Use this when you need private videos without hitting your origin API for every
 * byte: the app issues a short-lived token, the browser requests media through a
 * Worker route, and the Worker verifies the token before serving from R2.
 *
 * Requirements / notes:
 * - Attach this Worker to a route on a domain you control (for example
 *   https://media.example.com/*). R2 custom-domain/Worker routing requires DNS
 *   on Cloudflare; this is the "advanced" path and is optional.
 * - Bind your R2 bucket to the Worker as `MEDIA_BUCKET` (Wrangler: [[r2_buckets]]).
 * - Set `MEDIA_JWT_SECRET` (and optionally `MEDIA_JWT_ISSUER`) as Worker secrets.
 * - The token should be short-lived and scoped to the object/path the client may read
 *   (for example a `key` or `path` claim matching /media/<name>). Never trust a
 *   client-supplied bucket key without validating it.
 * - Public community media does not need this: use direct R2 delivery (r2.dev or a
 *   custom domain) with public access instead.
 *
 * This is a reference implementation; review and harden before production use.
 */

const encoder = new TextEncoder();

function base64UrlToUint8Array(input) {
  const base64 = input.replace(/-/g, "+").replace(/_/g, "/");
  const pad = "=".repeat((4 - (base64.length % 4)) % 4);
  const binary = atob(base64 + pad);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes;
}

async function verifyHs256Jwt(token, secret, issuer) {
  const parts = token.split(".");
  if (parts.length !== 3) throw new Error("invalid token");
  const [h, p, s] = parts;
  const header = JSON.parse(new TextDecoder().decode(base64UrlToUint8Array(h)));
  if (header.alg !== "HS256") throw new Error("unsupported alg");

  const key = await crypto.subtle.importKey(
    "raw",
    encoder.encode(secret),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["verify"],
  );
  const ok = await crypto.subtle.verify(
    "HMAC",
    key,
    base64UrlToUint8Array(s),
    encoder.encode(`${h}.${p}`),
  );
  if (!ok) throw new Error("bad signature");

  const payload = JSON.parse(new TextDecoder().decode(base64UrlToUint8Array(p)));
  const now = Math.floor(Date.now() / 1000);
  if (payload.exp && now >= payload.exp) throw new Error("expired");
  if (payload.nbf && now < payload.nbf) throw new Error("not active");
  if (issuer && payload.iss !== issuer) throw new Error("bad issuer");
  return payload;
}

function objectKeyFromRequest(url, payload) {
  // Prefer an explicit claim so the token is scoped to one object.
  const key = payload.key || payload.path;
  if (typeof key === "string" && /^media\/[A-Za-z0-9_-]+\.[A-Za-z0-9]+$/.test(key)) {
    return key;
  }
  // Fallback: map /media/<name> requests to the media/ prefix in the bucket.
  const m = url.pathname.match(/^\/media\/([A-Za-z0-9_-]+\.[A-Za-z0-9]+)$/);
  if (m) return `media/${m[1]}`;
  return null;
}

export default {
  async fetch(request, env) {
    if (request.method !== "GET" && request.method !== "HEAD") {
      return new Response("Method Not Allowed", { status: 405 });
    }

    const url = new URL(request.url);
    const auth = request.headers.get("Authorization") || "";
    const bearer = auth.startsWith("Bearer ") ? auth.slice(7) : "";
    const token = bearer || url.searchParams.get("token") || "";
    if (!token) return new Response("Unauthorized", { status: 401 });

    let payload;
    try {
      payload = await verifyHs256Jwt(token, env.MEDIA_JWT_SECRET, env.MEDIA_JWT_ISSUER);
    } catch {
      return new Response("Unauthorized", { status: 401 });
    }

    const key = objectKeyFromRequest(url, payload);
    if (!key) return new Response("Forbidden", { status: 403 });

    const object = await env.MEDIA_BUCKET.get(key);
    if (!object) return new Response("Not Found", { status: 404 });

    const headers = new Headers();
    object.writeHttpMetadata(headers);
    headers.set("etag", object.httpEtag);
    headers.set("cache-control", "private, max-age=0, must-revalidate");
    return new Response(request.method === "HEAD" ? null : object.body, { headers });
  },
};
