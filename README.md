# Betva

A fast, scalable betting and community media platform built for budget smartphones and slow mobile networks in Tanzania.

**Core priorities:** low data usage · minimal device processing · ultra-fast server responses.

**Last updated:** 16 November 2026.

> **How to read this document.** It is both the product specification and the implementation record. Every claim that could drift carries a status marker:
>
> - **✅ implemented** — built, wired to the API, and verified locally.
> - **🟡 partial** — the data layer or API exists, but the UI or an edge case is still missing.
> - **⬜ planned** — approved design, not built yet. Nothing marked ⬜ should be treated as working.
>
> Sections that mix states mark each claim individually. Where a spec decision and the shipped behaviour differ, the shipped behaviour is stated first and the gap is called out.

### Documentation map

| File | What it covers |
| --- | --- |
| `README.md` | Product spec, architecture, API reference, environment variables, development status |
| `docs/PEN_TEST_CHECKLIST.md` | Pre-launch security gate, run against staging |
| `docs/DEPLOYMENT.md` | Staging topology, deploy/rollback steps, environment variables, verification |
| `docs/RESTORE_RUNBOOK.md` | Backup/restore procedure and the drill record |
| `frontend/README.md` | Frontend setup, scripts, and project structure |
| `frontend/AGENTS.md` | Working conventions for agents and contributors in the frontend |

---

## Table of Contents

1. [Goal](#goal)
2. [Public Landing Page](#public-landing-page)
3. [Users & Roles](#users--roles)
4. [Navigation & Pages](#navigation--pages)
5. [Statuses & Colours](#statuses--colours)
6. [Trust Counter & Confidence](#trust-counter--confidence)
7. [Design System & UI](#design-system--ui)
8. [Admin Dashboard](#admin-dashboard)
9. [AI Results Assistant (Groq)](#ai-results-assistant-groq)
10. [Community](#community)
11. [Push Notifications](#push-notifications)
12. [Business Model & Monetization](#business-model--monetization)
13. [Database Backup & Recovery](#database-backup--recovery)
14. [Scalability & Availability](#scalability--availability)
15. [Security & Anti-Hacking](#security--anti-hacking)
16. [Architecture at a Glance](#architecture-at-a-glance)
17. [1. Frontend](#1-frontend)
18. [2. Backend & Real-Time Betting Engine](#2-backend--real-time-betting-engine)
19. [3. Media Optimization](#3-media-optimization)
20. [4. Local Payments & Network Infrastructure](#4-local-payments--network-infrastructure)
21. [5. Cloud Deployment (Vercel + Railway)](#5-cloud-deployment-vercel--railway)
22. [Crucial Design Tips for Low-Data Users](#crucial-design-tips-for-low-data-users)
23. [Open Questions](#open-questions)
24. [Development Status](#development-status)
25. [API Reference](#api-reference)
26. [Running Locally](#running-locally)
27. [Environment Variables](#environment-variables)
28. [Responsive & Accessibility Checks](#responsive--accessibility-checks)

---

## Goal

Deliver a betting site with community features (uploads, feeds) that:

- Loads instantly on 3G/4G connections
- Runs smoothly on low-RAM Android devices
- Handles thousands of simultaneous bets without lag or downtime
- Serves media without burning users' data bundles

---

## Public Landing Page

> **Status: ⬜ planned — not built.** There is no public landing page yet. An unauthenticated visitor who opens any app route is redirected to `/login`, which links to `/register`. The design below is the approved build target, not a description of current behaviour.

The first page a visitor sees, before any account exists. Its job is to convert a stranger into a registered user — and to give them a reason to keep scrolling rather than bounce.

### Above the Fold

Everything that must be visible without scrolling:

- **One headline, one action.** A single clear promise and a single Register button.
- **Live match ticker** — today's matches with kickoff times, so the page is never static.
- **Proof of life** — users online now, recent community posts, recent winners (anonymized).

### Engagement Mechanics

- **Live activity feed** — new posts, new matches, and settled results appear continuously.
- **Infinite scroll** on the community preview — content never "ends".
- **Countdown timers** to the next kickoff, creating urgency without a hard sell.
- **Daily streak / check-in** — a small reason to return each day.
- **Personalized return state** — a logged-out returning visitor sees where they left off.
- **Momentum copy** — "one tap from your first slip" style progress framing.

### Conversion Funnel

```
Land  →  see live matches + activity
      →  scroll  →  social proof builds trust
      →  attempt an action  →  registration wall at peak intent
      →  register  →  land on Home
```

The registration wall appears **when the visitor tries to act**, not on first load — the page must be fully readable before signup is required.

### Engagement Policy — Decision

**Decision: the engagement mechanics above are approved, and the guardrails below are enforced as product rules, not suggestions.**

This is the position I recommend holding. The mechanics are what make the page work; the guardrails are what keep it defensible when a regulator, an app store, or a journalist looks at it. A betting product that maximizes scroll time on users with little money is the exact pattern regulators are targeting — the difference between a viable business and a shutdown is whether these rules are written down and actually enforced.

**Enforced rules:**

| Rule | Limit |
| --- | --- |
| Fabricated social proof | **Never** — no fake winners, no inflated counts, no resetting countdowns |
| Autoplay | **Off by default**, always respects Data Saver |
| Infinite scroll | Community and matches **only** — never on betting slips |
| Push notifications | Max **2 per day** per user, quiet hours **22:00–07:00 EAT** |
| Session reminder | At **60 minutes**, with a one-tap cool-off |
| Self-exclusion | User can lock their own account for **24h / 7d / 30d** |
| Mute control | Every notification type can be muted by the user |
| Claims | No guaranteed wins; advertising follows Tanzanian betting rules |
| Disclosure | Clear statement that this is betting, not prediction |

**Status:** the in-product guardrails are enforced (push cap and quiet hours server-side, self-exclusion append-only, mutes, 60-minute reminder, no autoplay, no fake data). The advertising/claims policy and a standalone disclosure page are ⬜ — registration carries a terms/ownership checkbox, but there is no policy page yet.

**Why each one exists:**

- **No fake proof** — fabricated activity is the fastest route to a regulator's attention, and it is trivial to disprove.
- **Slip scroll ends** — a user must be able to stop, review, and back out before committing money.
- **Push caps** — notification fatigue kills the channel; a capped channel stays valuable.
- **Self-exclusion** — the single cheapest harm-reduction feature, and the one regulators most often require.
- **Mute controls** — consent that cannot be withdrawn is not consent.

Anything not on this list is a design choice; anything on it is a **release blocker** if missing.

---

## Users & Roles

Two roles exist: **Admin** and **User**. There is no public admin registration.

### Admin

- **Credentials:** username and password supplied through environment variables at deploy time — never stored in the database. ✅
- **Purpose:** manages matches, betting options, results, users, payments, and community content. ✅
- **Access:** configuration is controlled by the operator, not created through the app. ✅
- **Token:** admin JWT with `is_admin=true`, 30-day expiry, no refresh token. ✅

### User

- **Registration:** nickname, phone number, **official name of the phone owner**, password, and an explicit terms/ownership checkbox. The acceptance is written to `consent_log` with a notice version and timestamp. ✅
- **Login:** phone number + password. ✅
- **Session:** stateless JWT, 30-day expiry. Logout discards the token on the client; there is no server-side denylist yet. 🟡
- **Profile editing:** password change ✅. Profile picture is accepted by `PUT /api/v1/me` but has **no upload UI** and **no server-side size check** — the 10 KB rule is client-side only. 🟡
- **Nickname editing:** not supported; the nickname is fixed at registration. ⬜
- **KYC mismatch flow** (7-day notice, appeal, phone/name change, review queue): **not built**. ⬜

| Action | Required fields | Status |
| --- | --- | --- |
| Register | Nickname, phone number, official name of phone owner, password, terms acceptance | ✅ |
| Login | Phone number, password | ✅ |
| Edit profile | Password | ✅ |
| Profile picture | ≤ 10 KB image | 🟡 API only, no UI, no server-side limit |

#### Registration Disclosure

At registration the user must be shown, and must accept, the following before the account is created:

> **Official name notice**
> Enter the **official name registered to this phone number**.
> We use this name only to confirm that you truly own this account.
> It is **visible to administrators only** and is never shown publicly.
> If the name does not match the registered owner of the phone number, you will receive a **7-day notice** to correct it or appeal. If it is not resolved in that time, **your account will be banned.**

- Acceptance is **recorded** (timestamp + version of the notice) so the ban decision is auditable.
- The official name is **never displayed** in the Community feed, leaderboards, or to other users.

#### Name Mismatch & 7-Day Appeal Flow ⬜

> **Status: designed, not built.** There is no mismatch detection, notice banner, appeal form, phone/name change, or review queue in the app. The flow below is the approved target.

A mismatch is never an instant ban. The user gets a **7-day window** to fix or appeal, and the account stays usable during it.

```
Mismatch detected (admin review)
        │
        ▼
7-day notice issued ──► account STILL USABLE
        │
        ├── User appeals  ─────────────┐
        ├── User changes phone number  │
        │   to one under the same name │
        └── User corrects official name│
                                       ▼
                              Admin review queue
                                       │
                        ┌──────────────┴──────────────┐
                        ▼                             ▼
                  Confirmed                     Rejected / no action
                        │                             │
                Account restored              Account banned
```

**Rules:**

- **The notice lives for 7 days.** It appears as a non-dismissable in-app banner, a push notification, and an entry in the notification inbox that expires after 7 days.
- **The account remains usable** during the window — the user is not locked out while under review. This is deliberate: a lockout before review punishes typos.
- **Three ways to resolve:**
  1. **Appeal** — explain the discrepancy in text.
  2. **Change the phone number** to one registered under the same official name.
  3. **Correct the official name** if it was entered wrong.
- **No instant self-service fix.** Every change enters the **admin review queue**; nothing is auto-approved.
- **Outcomes:**
  - **Confirmed** → mismatch cleared, account returns to normal.
  - **Rejected** → ban, with the reason recorded.
  - **No action after 7 days** → ban, with reason `KYC_UNRESOLVED`.
- **Everything is logged** — notice issued, appeal text, changes made, reviewer, decision, and timestamp.

| State | Account access | Can change phone/name | Can appeal |
| --- | --- | --- | --- |
| Normal | Full | No | — |
| Under 7-day notice | Full | Yes (queued for review) | Yes |
| Under review | Full | No | No (pending) |
| Suspended (repeat) | Login blocked, funds frozen | No | Yes, 48h |
| Banned | Login blocked | No | Via support only |

#### Repeat Mismatches — Decision ⬜

> **Status: designed, not built.** Escalation states (`suspended`, `banned`) exist and are enforced, but nothing in the app detects a repeat mismatch or starts the 48-hour review.

**Decision: a second mismatch does not get another 7-day window. It gets immediate suspension and a 48-hour priority review.**

Reasoning from how this actually plays out: a genuine typo is corrected once and never returns. A mismatch that comes back is a pattern, not an accident. But an automatic ban on the second hit is still too harsh — people change SIM cards, and a re-registered number can legitimately fail a name check that was fine last month.

So the escalation is deliberate and graded:

| Occurrence | Action | Account access |
| --- | --- | --- |
| **1st mismatch** | 7-day notice, self-correct or appeal | **Full use** |
| **2nd mismatch** | **Immediate suspension**, priority queue, 48h to respond | **Blocked, funds frozen** |
| **3rd mismatch** | **Permanent ban**, support appeal only | Blocked |

**Exception — phone change only:** if the second flag is a *phone-number change with the same official name*, it is treated as **routine re-verification**, not an escalation — a 48-hour hold, then release. Changing SIMs is normal; changing identities is not. Only a name change or a name/phone mismatch counts as a repeat offence.

**On frozen funds:** a suspended user cannot bet, but the balance is not confiscated. After review, a confirmed user can withdraw to the **original verified number**. This avoids the worst support scenario: a real user locked out of their own money.

### Auth Rules

- **Passwords are hashed** with bcrypt (default cost) — never stored in plain text, even for "simple" passwords. ✅
- **Phone numbers are normalized** to `+255XXXXXXXXX`, so `0712…`, `712…`, and `+255712…` resolve to one account. ✅
- **Admin credentials live in environment variables**, so rotating them is a redeploy, not a database migration. ✅
- **Login is phone-based**, which means the phone number is the unique account identifier. ✅
- **The official name is sensitive personal data** — returned only to the user and to admins, never in public or community responses. ✅ Encryption at rest and a separate PII store are still ⬜.
- **Password floor:** 6 characters, enforced server-side. A stronger policy (common-password rejection, length rules) is still an open question. 🟡
- **Rate limiting and lockout** on login/registration are ⬜ — see [Security & Anti-Hacking](#security--anti-hacking).

---

## Navigation & Pages

After login, the user lands on **Home**. There is **no top navigation bar**; each page carries its own header, and a persistent **bottom navigation** holds the four destinations. The bar spans the full device width at every breakpoint, with the four items sharing it evenly. Auth pages (`/login`, `/register`) hide the nav. The admin area uses a separate shell with its own sidebar.

**Bottom navigation order (implemented):**

1. Home (`/`)
2. Community (`/community`)
3. History (`/history`)
4. Profile (`/profile`)

A **60-minute session reminder** appears as a dismissible banner with a one-tap 24-hour break option. ✅

### Home

- The landing page immediately after login. ✅
- Shows **today's and tomorrow's matches only**, split into **Today** and **Tomorrow** buttons that fit the device width. Each button shows the match count for that day; the tab switches without a reload. ✅
- The page opens on Tomorrow automatically when Today has no matches but Tomorrow does. ✅
- Anything older or further ahead is **not shown on Home**. ✅
- Market buttons for a settled match show a **green tick** for a win and a **red cross** for a loss, so the outcome is readable without opening History. ✅
- The desktop layout puts the **Track record** and a "How Betva works" card in a sticky right rail. ✅

### Bet Markets

Only two markets are offered. Keeping it to two is deliberate: fewer decisions, faster slips, lower cognitive load on cheap phones.

| Market | What it means | Settles on |
| --- | --- | --- |
| **Full Time Over 0.5 Goals** | At least one goal in the entire match | Final whistle |
| **Over 0.5 Half Time** | At least one goal in the first half | Half-time whistle |

- Each market is a simple **Yes / Over 0.5** pick. Decimal odds are shown on the match, in the slip, and in History — no arithmetic for the user.
- A match can offer one or both markets; the admin sets this per match.
- Stakes are small and fixed tiers, not free-form amounts, to keep the slip lightweight.

### Match Visibility Window

| Where | Window | Contents |
| --- | --- | --- |
| **Home** | **Today + tomorrow** | Upcoming and live matches only |
| **History** | **Last 6 months** | Settled matches and the user's bets |
| **Results archive** | **6 months, append-only** | Every day, nothing deleted |

The split is deliberate: Home stays small and fast, so it loads quickly on 3G and never becomes an endless list. The 6-month record lives in History, where users look backwards.

**Access gating (2026-11-16):** an inactive or expired account sees only the **first three matches by kickoff** from the published list; the rest are hidden by the API, not just by CSS. A notice explains how to activate. Active accounts see the full list. Users never see a wallet balance: money is paid to the single official number and the admin activates the account manually. ✅

**Terms & the one payment number:** `/terms` is a public, versioned Terms of Service (v2). Registration links to it and requires acceptance; the accepted version and timestamp are recorded in `consent_log`. The terms state that there is exactly one official payment number (shown on Profile from `PAYMENT_NUMBER`, with `PAYMENT_NAME` identifying the account holder), that money sent to any other number or person is the user's responsibility, and that users own what they post. ✅

### Community

A single feed of user posts — text, one image, or one short video. It is the only place in the product with infinite scroll, and the only place users can publish. The layout follows a page feed: author avatar, name, timestamp, message, media, then a reaction bar.

**Reactions:** 👍 Like, ❤️ Love, 😂 Haha. One real reaction per user per post; tapping the active one removes it, tapping another changes it. Counts come from `post_reactions` rows — never seeded or fabricated. Any signed-in user may react, including while inactive; only publishing and uploading require an active account, enforced by the server with `403`. ✅

On phones the composer sits directly above the feed. From 1024px it moves into the **sticky right rail** (with a desktop-only house-rules card) and the feed takes the main column, so the composer stays reachable while scrolling.

**What a post is**

- Up to **500 characters** of text, optionally with **one** image or **one** video.
- Images: JPEG, PNG, WebP, AVIF or GIF, **≤ 400 KB**. Video: MP4 or WebM, **≤ 3 MB**.
- Media is sniffed from the bytes server-side; a client-supplied `Content-Type` is never trusted.
- Only media issued by `POST /api/v1/media` is accepted in a post, so a post cannot embed a remote tracker.
- Text-only is always the cheapest option and is stated in the composer.

**What users never see**

- The **official name** of any user is never shown in the feed — only the nickname and optional profile picture.
- No fabricated activity: no fake posts, no inflated counts, no "X users online now" that isn't real.

**Moderation**

| Action | Who | Effect |
| --- | --- | --- |
| **Report** | Any user, once per post | Enters the moderation queue |
| **Auto-hide** | System, at **3 distinct reports** | Post leaves the feed pending review; this is a guardrail, not a verdict |
| **Hide / restore** | Admin | Hides from the feed or puts it back; open reports are closed either way |
| **Delete** | Author or admin | Soft delete: removed from the feed, record kept for disputes |

- Every admin action records **reviewer, reason, and timestamp**.
- Restoring a post resolves its open reports as `dismissed`; it does not stay stuck in the queue.
- The author can delete their own post at any time; the row is soft-deleted so reports remain auditable.

**Data cost**

- Images use `loading="lazy"` and `decoding="async"`.
- Video uses `preload="none"`, `playsinline`, and native controls. **Autoplay is off**, so a post never spends data until the user taps play.
- The feed pages **10 posts at a time**; infinite scroll stops at the end of the feed, and the client shows "That is the start of the feed."

**Storage today:** uploads are written to `MEDIA_DIR` and served from `/media/*`. Production swaps this for **Cloudflare Images / Stream** behind the same upload endpoint; the size limits, MIME sniffing, and no-autoplay rule stay the same.

### History

- The user's past bets, newest first, capped at the **last 6 months** and 200 rows. ✅
- Every settled bet shows the final score (and half-time score where available), the stake, the status, and the payout. Losses appear in the same list with the same weight as wins. ✅
- A **personal record** card summarises settled / wins / losses / win rate / net return. On desktop it sits in a sticky right rail with an "About this record" card. ✅
- Wallet movements live on **Profile**, not History — History is bets only. ✅

### Profile

Implemented sections, in page order:

- **Identity:** nickname, phone, access expiry. ✅
- **Wallet:** balance and the last 50 ledger entries, in cents rendered as TZS. ✅
- **Deposit & activation:** plain-language note that deposits and activation are applied manually by an admin. ✅
- **Monthly access rate:** the flat rate from `ACCESS_FEE_CENTS`, shown as "per month · contribution to running costs"; falls back to "Ask the admin" when unset. ✅
- **Notifications:** in-app inbox (newest 20), mark-one/mark-all read, and five category mutes. ✅
- **Take a break:** self-exclusion for 24h / 7d / 30d. ✅
- **Edit profile:** password change only. ✅

#### Account Status (Financial)

- **Balance** and **access expiry** are shown. ✅
- **Account status:** `active`, `inactive`, `suspended`, or `banned`, with the effect explained in plain language. ✅
- **Deposits and withdrawals for the period:** the ledger list shows them individually; a period total is ⬜.
- **Activation date:** ⬜ (only the expiry date is returned by `/me`).

#### Payment & Activation

- The user pays to **activate** (or renew) access. Payment is a **contribution to running costs**, not a prediction fee. ✅
- After payment, the admin marks the user **active** manually (with a number of days) and the new expiry appears on Profile. ✅
- The **admin's payment number and exact payment steps** are not shown in-app yet. ⬜
- Payment reference capture and receipts are ⬜ — the admin credit endpoint records a reason, but there is no deposit queue UI.

#### Account End & Terms

- **Expiry date** of the current access period. ✅
- **What happens when access ends:** betting is blocked while the balance stays safe; re-activation restores access. ✅ (enforced server-side)
- Links to **terms of use** and **privacy policy**: ⬜ (the registration checkbox records consent, but there are no policy pages yet).
- **Self-exclusion** and **mute controls**: ✅ (below).

#### Self-Exclusion & Mute Controls

Both live on the Profile page and both are enforced by the server, not just the UI.

**Self-exclusion** (`GET` / `POST /api/v1/self-exclusion`):

- The user picks **24 hours, 7 days, or 30 days**. Nothing else is accepted.
- While active, **bet placement returns `403`** with the end time; browsing, history, and wallet stay available.
- The period is **append-only**: there is no endpoint to shorten, replace, or cancel it. A second attempt while one is active returns `409` with the existing end time.
- The 60-minute session reminder offers the 24-hour option in one tap.

**Mute controls** (`PUT /api/v1/notifications/prefs`):

- Five categories, each independently mutable: **match start, results, account, announcements, community**.
- A muted category is **skipped at push time** (`push_error = 'muted by user'`); the inbox row is still written, so nothing is silently lost.
- Mutes never block money-related account messages from being recorded in the inbox.

---

## Statuses & Colours

Two kinds of thing carry a status: **bets** (settled outcomes) and **matches** (game state). Both use the same badge component, so a user learns the colours once and reads them everywhere — Home, History, and the admin screens.

### The Five Statuses

| Status | Applies to | Meaning | Colour |
| --- | --- | --- | --- |
| **WIN** | Bet | Settled and paid | Green |
| **LOSS** | Bet | Settled and lost | Red |
| **PENDING** | Bet | Waiting for the result | Slate grey |
| **LIVE** | Match | In play right now | Orange, pulsing |
| **POSTPONED** | Match, and a refunded bet | Delayed, rescheduled, or voided | Violet |

### Colour Tokens

| Status | Hex | Background | Text | Tailwind |
| --- | --- | --- | --- | --- |
| **WIN** | `#16A34A` | `#DCFCE7` | `#166534` | `bg-green-100 text-green-800 border-green-300` |
| **LOSS** | `#DC2626` | `#FEE2E2` | `#991B1B` | `bg-red-100 text-red-800 border-red-300` |
| **PENDING** | `#64748B` | `#F1F5F9` | `#334155` | `bg-slate-100 text-slate-700 border-slate-300` |
| **LIVE** | `#EA580C` | `#FFEDD5` | `#9A3412` | `bg-orange-100 text-orange-800 border-orange-300` |
| **POSTPONED** | `#7C3AED` | `#EDE9FE` | `#5B21B6` | `bg-violet-100 text-violet-800 border-violet-300` |

The five hues are deliberately far apart — green, red, neutral grey, orange, violet — so they stay distinguishable on cheap screens with poor colour reproduction. **PENDING is grey on purpose**: it is the most common state, and it should be the quietest thing on the screen.

### Badge Rules

- **Never colour alone.** Every badge carries its word and a mark:

  | Status | Badge |
  | --- | --- |
  | WIN | `✓ WIN` |
  | LOSS | `✕ LOSS` |
  | PENDING | `⏳ PENDING` |
  | LIVE | `● LIVE` |
  | POSTPONED | `⏸ POSTPONED` |

  This is required for colour-blind users and for screens where colour washes out in sunlight.

- **LIVE pulses** with a soft dot — and the pulse is disabled under `prefers-reduced-motion`. ✅
- **Fixed light-on-dark badge tokens:** each status has one background, text, and border value in `global.css` (`.badge-win`, `.badge-loss`, `.badge-pending`, `.badge-live`, `.badge-postponed`). Light backgrounds with dark text keep contrast high on the dark glass UI. ✅
- **Same badge everywhere** — a WIN in History looks identical to a WIN in the admin table. ✅
- **Status text is never truncated** to fit a narrow card; the badge wraps instead. ✅
- **Settled market buttons** on Home carry a **✓** for a win and **✕** for a loss, in addition to the badge. ✅

### Status Flow

```
Match:  UPCOMING ──► LIVE ──► FINISHED
              └──► POSTPONED ──► LIVE (later) or CANCELLED

Bet:    PENDING ──► WIN
                 └► LOSS
                 └► refunded if the match is POSTPONED / CANCELLED
```

**Postponed bets are refunded, not left pending.** A postponed or cancelled match returns the stake; the bet row is stored as `refunded` and the UI renders the **POSTPONED** badge with the void reason. This is the single most common source of user complaints, so it must be explicit. ✅

---

## Trust Counter & Confidence

A loss on its own looks like a failure. A loss inside a winning record looks like normal variance. The difference is **presentation**, and this product must never let a single red badge erase the whole picture.

### The Counter

A persistent, compact counter shown on Home and in Profile:

| Stat | Meaning |
| --- | --- |
| **Total matches** | Settled tips — one per published market on a settled match |
| **Wins** | Settled as WIN |
| **Losses** | Settled as LOSS |
| **Win rate** | Wins ÷ total settled tips, as a percentage |
| **Current streak** | Consecutive wins (or losses) |
| **Best streak** | Longest winning run |

- The counter **updates only after settlement**, never in real time, so it cannot flicker.
- It is **aggregate**, not per-user — the same record for everyone, which keeps it cheap to compute and cache.
- **Implementation:** `GET /api/v1/stats` counts one tip per published market on a settled match, so a settled match with two markets counts twice. A correction replaces the match's current result, so a corrected match is counted once, at its corrected outcome. The response carries `total`, `wins`, `losses`, `win_rate_pct`, `current_streak`, `streak_type`, and `best_win_streak`, and is cached for 60 seconds server-side (Redis-backed when available, with an in-memory fallback) as well as in the browser. The UI (`components/TrackRecord.astro`) renders it on Home and Profile and shows losses in the same weight as wins.

### Confidence Badge

Every published match carries a **confidence level**:

| Level | Meaning | Visual |
| --- | --- | --- |
| **HIGH** | Strong backing from the data | Green accent |
| **MEDIUM** | Reasonable, but not strong | Amber accent |
| **LOW** | Speculative — flagged as such | Grey accent |

- Confidence is **set by the admin**, not by the AI, and **does not change after a loss**. A HIGH tip that loses is still shown as HIGH, with the loss recorded honestly.
- This is the core trust decision: **do not downgrade confidence retroactively.** Hiding a miss is worse than the miss.

### How Losses Are Framed

- **The counter stays visible next to a loss.** The win rate and streak do not disappear when one tip fails.
- **No celebratory animation on a win, no alarm on a loss.** Both are neutral events.
- **Losses are never hidden, re-labelled, or moved.** Transparency is the asset.
- **Copy is factual:** "Result: LOSS" — not "unlucky" or "close".
- The **History page** shows wins and losses in the same list, same styling weight, so the record reads as a record, not a highlight reel.

### Why This Builds Trust

A product that only shows wins is a scam waiting to be exposed. A product that shows a 60–70% win rate honestly, with the losses right there in the same list, is one a user can plan around. The counter is the proof that the product trusts itself.

---

## Design System & UI

**Goal: a premium, low-strain interface a user can stare at for hours — the way the top betting and social apps keep people engaged without exhausting them.**

### Design Philosophy

- **Dark mode is the default — and the only mode in v1.** Light mode is ⬜. Dark surfaces reduce eye strain over long sessions and look premium on cheap OLED screens.
- **Glass and depth, not flatness.** Translucent surfaces, soft borders, and subtle elevation — the app should feel layered, not like a web page.
- **Motion rewards, it does not distract.** Transitions are short and purposeful; nothing animates for its own sake.
- **One thumb, one hand.** Every primary action is reachable in the bottom third of the screen.
- **Data is the hero.** Match, status, and money are always the largest, clearest elements on the screen.

### Color Palette

| Token | Value | Use |
| --- | --- | --- |
| **Background base** | `#0B0F14` (near-black) | App background |
| **Background gradient** | `#0B0F14 → #111827` | Subtle vertical gradient, never flat |
| **Surface** | `rgba(255,255,255,0.04)` + blur | Cards, sheets, nav bars |
| **Surface elevated** | `rgba(255,255,255,0.07)` | Modals, popovers |
| **Border** | `rgba(255,255,255,0.08)` | Card and input borders |
| **Text primary** | `#F3F4F6` (off-white) | Body text — never pure white |
| **Text secondary** | `#9CA3AF` | Labels, captions |
| **Text muted** | `#6B7280` | Placeholders, hints |
| **Brand accent** | `#4285F4` → `#1A73E8` | Primary buttons, links, active states (Google blue) |
| **Brand glow** | `rgba(66,133,244,0.25)` | Focus rings, subtle glows |
| **Danger** | `#EF4444` | Errors, loss accents |
| **Danger strong** | `#DC2626` | Destructive buttons (white text passes 4.5:1) |
| **Warning** | `#F59E0B` | Pending, caution |
| **Info** | `#3B82F6` | Links, informational |

The brand moved from green to **Google blue** when the wordmark was finalized. The wordmark itself uses the five Google colours — B `#4285F4`, e `#EA4335`, t `#FBBC05`, v `#34A853`, a `#EA4335` — and the same blue drives buttons and focus rings. The favicon/logo is the eagle-cutout "B" emblem; see [Branding](#branding).

- **No pure black or pure white** — they cause halation on OLED and feel harsh. ✅
- **Grain overlay** at 2.5% opacity on the background adds depth without a single extra image request. ✅
- Status colours (WIN/LOSS/PENDING/LIVE/POSTPONED) are defined in **Statuses & Colours** and never overridden. ✅
- **Light mode** is ⬜: the app is dark-only today. The palette below is the dark theme.

### Typography

- **Font stack:** system fonts only — `ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto`. Zero network cost, instant render, native feel. ✅
- **Scale:** `11 / 13 / 16 / 20 / 24 / 30 px` (`--t-xs` … `--t-2xl`), a 1.25 modular ratio rounded to the 4-pt grid. Body is 16px minimum — never smaller, and 16px inputs stop iOS zoom. ✅
- **Weights:** 400 (body), 500 (labels), 600 (headings), 700 (hero numbers). ✅
- **Line height:** 1.4 for body, 1.1 for large numbers. ✅
- **Numbers are tabular** (`font-variant-numeric: tabular-nums`) so counters and balances never jump width when they change. ✅
- **Money is always bold and larger** than surrounding text — it is the most important thing on the screen. ✅

### Buttons

| Variant | Look | Use |
| --- | --- | --- |
| **Primary** | Solid Google-blue gradient, white text, soft glow | Place bet, activate, submit |
| **Secondary** | Glass surface + brand border | Secondary actions |
| **Ghost** | Transparent, brand text | Tertiary, inline actions |
| **Danger** | Solid red | Ban, delete, destructive |
| **Disabled** | 40% opacity, no shadow | Unavailable |

**Rules (implemented):**

- **Minimum height 44px**, minimum width 44px — thumb-friendly, accessible.
- **Height is set, not padded:** `min-height: var(--tap)` with 20px horizontal padding. Multi-line labels still centre exactly, and buttons in the same row line up.
- **Radius 12px** for buttons and inputs, from the radius ladder (`--r-md`), not a one-off.
- **One internal gap token** (8px) between icon and label.
- **States:** default → hover (slight lift + glow) → active (scale 0.98) → disabled (40% opacity, no interaction).
- **Loading state:** button keeps its size and swaps its label, so there is no layout shift.
- **One primary button per screen.** If two look primary, the hierarchy is broken.
- **Micro-interaction:** a soft scale (0.98) on press. Subtle enough to feel native, not a toy. Haptic feedback is ⬜ (web vibration is inconsistent).

### Frames & Cards

- **Glass surface:** `backdrop-blur-md` + translucent background + 1px border. This is the signature look.
- **Radius ladder:** 8px chips/cells → 12px buttons/inputs → 16px cards → 20px sheets → 999px pills/badges. Every value is a token (`--r-sm`, `--r-md`, `--r-lg`, `--r-xl`, `--r-pill`), never a literal.
- **Elevation via shadow:** `0 4px 24px rgba(0,0,0,0.3)`, with a 1px hairline border on the glass surface. No harsh outlines.
- **Spacing system:** 4 / 8 / 12 / 16 / 20 / 24 / 32 / 48 (`--s1`…`--s12`). Never invent a value.
- **Cards never touch each other** — minimum 12px gap, so the screen breathes.
- **Dividers are 1px, low-contrast, and used sparingly.** Whitespace does the work.
- **Inputs:** glass surface, 12px radius, 16px padding, focus ring in brand glow. Label above, error below, never floating.

### Layout Frame & Breakpoints

Every user route renders inside one shared frame, so gutters and content width line up page to page.

| Token / class | Phones (<640px) | Tablet (640–1023px) | Desktop (≥1024px) |
| --- | --- | --- | --- |
| `.page-frame` | full width, 16px gutter | 24px gutter, 720px cap | 32px gutter, 720px cap |
| `.page-frame-wide` (Home, History, Community) | as above | as above | **1120px cap, two columns** |
| `.page-frame-narrow` (login, register) | 440px cap, centred | same | same |
| `.bottom-nav` | full-width bar, 64px | same | full-width bar, 60px; items in a row |
| Bet slip | bottom sheet, full width | centred dialog (≥640px) | centred dialog |

- **Two-column split** (`.page-split`): mobile keeps a single column in DOM order — trust record / personal record / composer first, then the list. From 1024px the aside becomes a **sticky right rail** (360px, 24px gap) and the list takes the remaining 672px. Home's rail also carries a desktop-only "How Betva works" card; Community's carries a desktop-only "House rules" card, with the composer pinned at the top of the same rail so posting is always one tap away.
- **Team names wrap, never clip.** Match and history rows use `grid-template-columns: minmax(0,1fr) auto minmax(0,1fr)`, so the score stays optically centred and long names wrap to a second line instead of being truncated.
- **Control heights are tokens:** `--tap: 44px` (every touch target) and `--control: 48px` (form controls). Buttons set height, not padding, so labels centre exactly.
- **Section rhythm:** `.section-gap` (24px) between major blocks; `space-y-4` (16px) inside a page frame.

### Feeds

What the community feed does today:

- **Auto-loading pages of 10** posts, stopping at the end with "That is the start of the feed." ✅
- **12px spacing** between feed items — dense enough to scroll, loose enough to read. ✅
- **No autoplay on first paint** — images are lazy, video is `preload="none"` with native controls. ✅
- **Smooth, momentum-based scrolling** — native browser behaviour, no scroll-jacking. ✅

Still planned:

- Skeleton loaders instead of a "Loading…" line. ⬜
- Blur-up image placeholders (a few hundred bytes per post). ⬜
- Pull-to-refresh on Home and Community. ⬜

### Background

- **Layered, never flat:** base colour → subtle gradient → grain overlay. Three layers, zero extra requests. ✅
- **No background images** — they cost data and fight with content. ✅
- **Fixed gradient** so content scrolls over it and the depth stays constant. ✅
- **OLED true-black mode** for maximum battery saving — ⬜, not built. The app ships dark-only.

### Long-Hour Comfort

- **Dark mode only today.** Light mode and a persisted theme choice are ⬜.
- **Reduced motion respected** — the LIVE pulse and transitions stop under `prefers-reduced-motion`. ✅
- **Break reminders** after 60 minutes of continuous use, with a one-tap 24-hour cool-off. ✅
- **No flashing or strobing elements.** Status pulses are slow and subtle. ✅
- **Font size control** (small / default / large) in settings — ⬜.
- **High contrast toggle** — ⬜.
- **Glare-friendly contrast** — body text targets 4.5:1 on the dark surface; verified per component, not per page. 🟡

### Micro-Interactions

- **Transitions:** 150ms for small elements, 250ms for sheets and modals, `cubic-bezier(0.4, 0, 0.2, 1)`. ✅
- **Press feedback:** a soft scale (0.98) on buttons. ✅
- **Success and error states** are written in plain language next to the action that caused them; no full-screen blocking spinner. ✅
- **Toast notifications** — inline `toast()` helper: top-centre, auto-dismiss after ~3.2s, red border for errors. ✅
- **Haptic feedback** — ⬜. Web vibration is inconsistent, so it is not relied on.

### Accessibility

- **Contrast ratio ≥ 4.5:1** for body text, ≥ 3:1 for large text (WCAG AA). 🟡 Verified per component during the design pass, not yet audited with an automated contrast checker across every state.
- **Touch targets ≥ 44×44px** — enforced by the responsive audit (0 failures across 84 route/width checks). ✅
- **Focus rings visible** on every interactive element, in brand glow. ✅
- **Screen reader labels** on icons, tabs, and status badges (`aria-label`, `role="tab"`, `aria-selected`). ✅
- **Colour is never the only signal** — status badges carry text and marks, and settled market buttons carry ✓ / ✕. ✅

### Branding

**Wordmark.** "Betva" is set in the five Google colours, one letter each:

| Letter | Colour |
| --- | --- |
| **B** | `#4285F4` (blue) |
| **e** | `#EA4335` (red) |
| **t** | `#FBBC05` (yellow) |
| **v** | `#34A853` (green) |
| **a** | `#EA4335` (red) |

The wordmark is a logo, not body text: it never inherits surrounding text colour, and it is not used below 16px where the individual hues stop being distinguishable.

**App mark.** The logo is an **eagle-cutout "B"** — a letter B whose counters are cut into a stylised eagle head, eye, beak, and wing-feather openings, coloured with the same Google palette. It doubles as the favicon, so the browser tab, the home-screen icon, and the in-app header all show the same mark.

| Asset | Path | Use |
| --- | --- | --- |
| Eagle-B favicon | `frontend/public/favicon.svg` | `<link rel="icon">`, PWA manifest |
| Horizontal logo | `frontend/public/logo-dark.svg` | Headers and auth screens on the dark surface |
| Manifest | `frontend/public/manifest.webmanifest` | `name`, `theme_color` `#0b0f14`, SVG + 192/512 PNG icons |
| ICO fallback | `frontend/public/favicon.ico` | 32px PNG-compressed ICO generated from the eagle-B artwork ✅ |
| PNG icons | `frontend/public/favicon-32.png`, `apple-touch-icon.png` (180), `icon-192.png`, `icon-512.png` | Browser fallback, iOS home screen, Android install |

**Regenerating icons:** `cd frontend && npm run icons` rasterizes `favicon.svg` with `sharp` and rewrites all PNGs plus the `.ico`. The 512px PNG is also declared `maskable` for Android launchers. ✅

### What This Achieves

A user who opens the app at 6pm and is still there at 11pm should feel comfortable, not drained. The dark glass aesthetic reads as premium on a $60 phone, the large tap targets make betting feel effortless, and the consistent spacing and motion make the app feel like one coherent thing rather than a collection of pages. That is what keeps people in the app the way the top giants do — not tricks, but comfort.

---

## Admin Dashboard

The admin area is separate from the user app and is reached with the credentials set in environment variables. It opens on an **Overview** page (today/tomorrow match counts, user totals, active users, and shortcuts) and is built around six centres: **Matches**, **Users**, **Analytics**, **AI Results**, **Community**, and **Notifications**. Two further pages — a browsable Results Archive and an Admin Profile — are designed but not built. 🟡

### Matches (CSV Upload) ✅

The **Matches** centre is the single source of matches shown to users.

- **Upload a CSV** of matches for a **specific date**.
- Uploaded matches are **published to the user Home page** for that date.
- **Only today's and tomorrow's matches are visible to users.** Anything further ahead is stored but hidden until it enters the window; anything older drops off Home and lives only in History.
- Each upload is tied to a date, so multiple days can be loaded in advance.
- Matches carry their betting options, which users see on Home.
- Each match also carries a **confidence level** (HIGH / MEDIUM / LOW), set by the admin.

**CSV handling requirements:**

- A fixed template (`frontend/public/matches-template.csv`). ✅
- **Validation before import** — bad rows are rejected with their line number and a readable error; good rows still import. ✅
- **Duplicate detection** — re-uploading the same date warns before overwriting; replace is blocked when bets already exist. ✅
- **Import log** — who uploaded, when, which date, how many rows, how many failed, whether it replaced. ✅
- **Match state** — open for betting → closed automatically at kickoff → `awaiting_result` while no score exists → settled (and shown `FINISHED`) only once a result is recorded. A match is never shown as finished with an empty result. ✅

### Users & Financial Status ✅

A single place to see every user clearly.

- **User list** with nickname, phone number, official name, registration date, and status. ✅
- **Financial status** per user — balance, access expiry, and whether they are active. ✅
- **Manual activation** — the admin marks a user **active** for a number of days once payment is confirmed. ✅
- **Credit / withdrawal adjustment** with a required reason and an append-only ledger row. ✅
- **Suspend / ban / deactivate** with the reason recorded. ✅
- **Search and filter** by phone, nickname, or status. ✅
- **KYC review queue** — appeals, phone-number changes, and official-name corrections waiting for a decision. ⬜
- **Deposit / withdrawal queue** with mobile-money references. ⬜
- **CSV export** of users and finances. ⬜

#### KYC Review Queue ⬜

> **Not built.** There is no appeal, phone-change, or name-correction queue yet. The table below is the approved design for when it is.

Every mismatch resolution lands here, never auto-approved.

| Queue item | Admin sees | Decision |
| --- | --- | --- |
| Appeal | User's written explanation, account history | Confirm / reject |
| Phone change | Old phone, new phone, official name | Confirm / reject |
| Name correction | Old name, new name, phone | Confirm / reject |

- Each decision records **reviewer, timestamp, and reason**.
- A rejection **immediately issues the ban**; the user is notified through the same channel.
- The queue shows **days remaining** on each 7-day window so nothing expires unseen.

### Results Archive 🟡

> **Data layer built, admin page not built.** Results are written to `results_archive` at settlement and retained; there is no browsable archive screen in the admin app yet.

- Match results are **accumulated daily** at settlement. ✅
- **Retained for 6 months**, day by day. ✅ (the table is never pruned by application code)
- **Nothing is deleted** — the archive is append-only; corrections add a new row linked by `corrects_id`. ✅
- Users see the same **6-month window** in History. ✅
- Results feed the settlement of bets and the user's History page. ✅
- **Browsable archive page** with filters by date, league, and settlement state. ⬜

**Practical notes:**

- Store results with the **Africa/Dar_es_Salaam (EAT)** timezone so a "day" means the same thing to admin and users. ✅
- 6-month retention with no deletion means **scheduled backups** are mandatory — retention is only real if the data is recoverable. ✅ Daily off-site dumps with GFS retention cover the archive window.

### Admin Profile ⬜

> **Not built.** There is no admin profile page.

- Would show the admin identity and current configuration state (push configured, AI configured, fee planner configured) without ever exposing secret values.
- Because credentials come from environment variables, **changing them is a redeploy** — the page would reflect, but never store, them.

### Analytics (Daily Active Users & Access-Fee Planner)

The record of **daily active users (DAU)** used for the monthly access-fee decision. ✅

- **How it is counted:** every signed-in user is recorded once per EAT day (`Africa/Dar_es_Salaam`), on their first authenticated request of that day. Nothing is recorded for the admin token.
- **Storage:** the append-only `activity_days` table (`user_id`, `day`, `first_seen`, `last_seen`). It is a business record backing financial decisions — rows are never edited or deleted.
- **Cost model:** an in-memory per-day seen set means at most one Postgres write per user per day; repeat requests add no queries.
- **Admin view:** the **Analytics** centre (`/admin/analytics`) shows DAU today, the 7-day average, the peak, total/new users, a 30-day bar chart, and a day-by-day table.
- **API:** `GET /api/v1/admin/analytics?days=7..90` returns per-day `{day, dau, new_users}`, the summary counters, and the fee plan.
- **Access-fee planner:** two environment variables drive the suggestion:
  - `ACCESS_FEE_DAU_TARGET` — the DAU number at which the admin introduces (or re-decides) the monthly contribution.
  - `MONTHLY_OPERATING_COST_CENTS` — the platform's total monthly running cost in cents.
  - When DAU reaches the target, the planner suggests `monthly cost ÷ users (target or current DAU, whichever is larger)`, **rounded up to whole 500 TZS steps**. The suggested figure is decision support only — **the final amount is always the admin's decision**, and it remains a contribution to running costs, never a prediction fee.
  - **Admin-only by design:** the DAU target, running cost, and suggested figure never leave the admin analytics endpoint. Shortfalls against the target are covered by the admin from other sources — users are never shown targets, costs, or planning data.

#### The Flat Rate Users See

- `ACCESS_FEE_CENTS` — the flat monthly rate users actually pay, in cents.
- It is served by the **public** `GET /api/v1/config` (`access_fee_cents`) and shown on the user **Profile** ("Monthly access rate — per month · contribution to running costs").
- If it is `0` (not set), the Profile shows *"Ask the admin"* instead of an invented number.
- When the planner suggests a new rate, the admin updates `ACCESS_FEE_CENTS` and redeploys — one variable, one source of truth.

### AI Results, Community, and Notifications ✅

The remaining three centres each have a dedicated section:

- **AI Results** (`/admin/ai`) — paste a source URL, review the extracted score, approve or reject. See [AI Results Assistant (Groq)](#ai-results-assistant-groq).
- **Community** (`/admin/community`) — reported/visible/hidden/deleted queues with hide, restore, and delete reasons. See [Community](#community).
- **Notifications** (`/admin/notifications`) — compose a broadcast to all or active users, with the in-app inbox written first and push attempted second. See [Push Notifications](#push-notifications).

### Suggested Additions (Gaps Worth Filling)

These were not in the brief but the product will need them:

- **Bet slip & settlement** — ✅ implemented in Phase 3.
- **Deposit / withdrawal queue** — a manual approval list with mobile money transaction references. *(Admin credit endpoint exists; the queue UI is ⬜.)*
- **Append-only ledger** — ✅ implemented in Phase 3.
- **Ban & audit log** — ✅ implemented in Phase 2.
- **Community moderation** — ✅ report, hide, and delete posts; auto-hide at three distinct reports.
- **Match locking** — ✅ implemented: betting closes automatically at kickoff.
- **Announcements** — 🟡 text-only broadcast exists; scheduled announcements are ⬜.
- **Payout / commission rules** — ✅ payout maths implemented; final commercial margin still open.
- **Export** — CSV export of users and finances for accounting and tax. ⬜
- **A second, limited admin** — support staff who can view but not change money. ⬜ (one admin identity from env vars today)

---

## AI Results Assistant (Groq)

> A Groq-hosted model reads football result pages and turns them into structured rows. It is an **assistant that fills a form**, never the final authority on money.

> **Status: 🟡 partially built (AI path).** The fetch → extract → validate → admin review pipeline works end to end, and the settlement scheduler will settle a match from the newest **admin-approved** suggestion. Raw AI output never settles a match. Deterministic, non-AI source discovery and two-source agreement are built separately — see [Automatic web result discovery](#automatic-web-result-discovery-no-ai) below. A formal `DISPUTED` UI state is still not built; a disagreement is simply left in `awaiting_result` for an admin.

### Correction Before Anything Else

**Groq is an inference API, not a browser.** It has no internet access. It cannot open Forebet or PassionPredict, and if you ask it to "look up" a score it will invent a plausible one.

The pipeline must **fetch the page first**, then pass the text to Groq for extraction. That misunderstanding is the single biggest source of wrong results in systems built like this.

### Pipeline (implemented)

```
Admin opens a match in Matches, pastes a source URL
        │
        ▼
POST /api/v1/admin/ai/suggest {match_id, source_url}
        │
        ▼
Server fetches the page (20s timeout, 1 MB cap, http/https only)
        │
        ▼
cleanHTML strips scripts, styles, and markup ──► ≤ 8,000 chars of text
        │
        ▼
Groq (temperature 0, JSON object response format, 256 tokens)
        │
        ▼
Validator ──► strict JSON, both goals present, goals 0–30,
              half-time goals 0–30, confidence high/medium/low
        │
        ├── pass  → result_suggestions (status = pending)
        └── fail  → result_suggestions (status = failed, reason stored)
        │
        ▼
Admin approves or rejects in /admin/ai
        │
        ├── approved → suggestion is marked approved (no wallet moves)
        └── rejected → reason required
        │
        ▼
Settlement scheduler settles the match from the approved score, or an
admin settles manually in Matches (result + slips + notifications)
```

An in-process settlement scheduler runs every 15 minutes by default and now
includes deterministic web discovery (below). `AUTO_SETTLE_ENABLED=0` turns the
whole job off.

### Automatic web result discovery (no AI)

A separate, non-AI stage runs inside the same settlement sweep:

```
unsettled matches past grace
  → group by kickoff date (most recent first, up to AUTO_RESULT_FETCH_MAX_DATES)
  → ESPN public scoreboard JSON for that date
  → match fixtures by folded home + away team name (accents/punctuation ignored)
  → FT score from competitors[], HT score from goal minutes in details[]
  → 100Tip graded rows for today/yesterday cross-check the FT and HT scores
  → agreement → write an APPROVED result_suggestions row (provider 'espn')
  → disagreement or no source → leave the match in awaiting_result for an admin
  → settlement sweep settles the approved score with the normal locks
```

Important properties:

- **No model is involved.** No Groq key, prompt, or inference. Scores come from
  structured fields on public pages.
- **No unattended writes to money.** Discovery only writes an approved
  suggestion; the settlement handler remains the only code that updates
  `matches`, `bets`, `results_archive`, and sends notifications.
- **Conservative half-time grading.** HT is derived only when ESPN's goal list
  accounts for every full-time goal. A 0–0 full time safely implies a 0–0 half
  time. Otherwise HT stays unknown and an HT-market slip is not auto-graded.
- **Conflict safety.** If 100Tip has the same fixture with a different score,
  auto-approval is skipped and the mismatch is logged with `auto-result:`.
- **Fetched URLs are SSRF-guarded** by the same `publicHTTPClient` used for AI
  sources (public IPs only, DNS pinned, redirects refused).
- `AUTO_RESULT_FETCH_ENABLED=0` stops discovery; manual settlement and the AI
  assistant continue to work.

An admin can trigger discovery for one date on demand:

```
POST /api/v1/admin/results/scan   {"date":"YYYY-MM-DD"}
```

The response reports `dates_scanned`, `espn_fixtures`, `approved`, `conflicts`,
and `sources_unavailable`. Settlement still happens through the next sweep.

### Sources (manual AI flow)

The admin supplies the URL; the server does not know these sites by name.

| Source | Example URL | Use |
| --- | --- | --- |
| Forebet 1X2 | `forebet.com/en/football-predictions/predictions-1x2/YYYY-MM-DD` | Finished scores + upcoming predictions |
| PassionPredict | `passionpredict.com/over-0-5-half-time` | Over 0.5 half-time predictions |
| Manual CSV | Admin Dashboard → Matches | Always available fallback |

Building the Forebet URL from the target date in **EAT** is ⬜ — today the admin pastes the full URL.

### Free Result Sources (No Paid API) ⬜

None of these are integrated. They are options if scraping is blocked or a licensed feed is preferred:

- **football-data.org** — free tier, real results, limited competitions.
- **API-Football (api-sports.io)** — small free daily quota, good coverage.
- **TheSportsDB** — free tier, community-maintained.
- **OpenLigaDB** — free, a few leagues only.
- **Manual CSV** — zero cost, already part of the admin flow.

Check current limits before relying on any of them — free tiers change without notice.

### Anti-Blunder Rules

**Prompt rules given to Groq:**

1. Return **only facts present in the supplied text**. Never infer a score from team strength.
2. If the half-time score is missing, return `null`. **Never guess.**
3. Output **strict JSON** matching the schema — no prose, no markdown, no code fences.
4. `confidence` reflects how clearly the source states the result (`high`, `medium`, `low`).
5. `notes` is one short sentence, max 120 characters.
6. Temperature is `0`, and the request asks for a JSON object, so the model has little room to embellish.

**Validation rules applied after Groq responds (implemented):**

- **JSON parse** — code fences are stripped, then the outermost object is parsed. A parse failure is stored as a `failed` suggestion with the raw response.
- **Both goals present** — `home_goals` and `away_goals` are required.
- **Score range** — every goal value must be **0–30**; anything else is rejected and stored as `failed`.
- **Half-time range** — `home_goals_ht` / `away_goals_ht` must be 0–30 or `null`.
- **Confidence enum** — must be `high`, `medium`, or `low`; anything else falls back to `low`.
- **Source echoed** — the URL, page excerpt, model name, and raw model output are stored with the suggestion for audit.

**Not implemented (⬜):**

- **Team match** — fuzzy-matching extracted team names against the fixture.
- **Date match** — rejecting a result whose date differs from the match date.
- **Two-source agreement** — a second independent source confirming the score.
- **Confidence gate** — blocking `low` confidence from reaching the review queue. Today it reaches the admin with the badge visible.
- **Automatic settlement from an approved suggestion** — implemented. It never uses raw AI output and never discovers sources itself.

### Why AI Must Never Settle Bets Alone

A hallucinated score pays the wrong users and takes money from the right ones. That is a financial loss **and** a legal exposure, and it is unrecoverable once payouts have gone out. Groq may fill the form; **an admin approves the row** before any wallet moves.

### Source Disagreement & Settlement Authority — Decision ⬜

> **Designed, not built.** The app currently accepts one source at a time and has no `DISPUTED` state. The rule below is the approved target for the automated pipeline.

**Decision: no website wins a disagreement. Disagreement means "do not settle" — the row enters a `DISPUTED` state and waits for a human.**

This is the only defensible position, because **neither Forebet nor PassionPredict is a primary source**. Both are prediction and aggregation sites. Treating either one as the final word means a redesign of their page or a stale cache can silently move money.

**Authority hierarchy, highest first:**

| Rank | Source | Role |
| --- | --- | --- |
| 1 | **Official competition / league source** | Settles any dispute |
| 2 | **Two independent sources agreeing** | Auto-eligible for admin approval |
| 3 | **Single source** | Eligible only for matches with no open bets |
| 4 | **Disagreement** | `DISPUTED` — never settles, human required |

**The tiebreaker, in practice:**

1. If the two sources disagree → mark `DISPUTED`, notify the admin, **stop the settlement job** for that match.
2. The admin opens the stored **official league link** for that fixture and enters the score by hand.
3. That manual entry becomes the record, stamped with the **reviewer ID, source, and timestamp**.
4. If no official source exists, **Forebet is the default reference** — it carries actual score tables, while PassionPredict is a market-prediction site. But this is recorded as a **manual override**, not as "Forebet was right".

**Additional rule that saves money:** settlement only matters where money is on it.

- **Bets open on the match** → full standard above.
- **No bets on the match** → the result is still recorded, but a disagreement is low priority and never blocks the queue.

**Timing rule:** if one source says `upcoming` and the other says `finished`, the row is not disputed — it is **stale**. Re-fetch after 30 minutes, and only then apply the rules above.

**Corrections after settlement — decision:** mistakes happen, so corrections are allowed and **nothing is ever edited in place**.

- A correction is a **new reversing entry** in the append-only ledger, plus a fresh settlement row.
- The original result and the original payout stay visible, linked to the correction.
- Every correction records **reviewer, reason, and timestamp**.
- A corrected result triggers a **notification** to the affected users — silently changing a payout destroys trust faster than the mistake itself.

### Prompt Template (as implemented)

System prompt sent to Groq:

```
You extract football match results from page text.
Return ONLY a single JSON object. No markdown, no code fences, no prose.
Schema:
{"home_goals":integer,"away_goals":integer,"home_goals_ht":integer or null,
 "away_goals_ht":integer or null,"confidence":"high" or "medium" or "low","notes":string}
Rules:
- home_goals and away_goals are required non-negative integers (full-time score).
- home_goals_ht / away_goals_ht are the half-time score, or null if not stated.
- confidence reflects how clearly the source states the result.
- notes is one short sentence, max 120 characters.
- Output the JSON object and nothing else.
```

User message:

```
Page text:
<<<cleaned page text, max 8,000 characters>>>
```

Request settings: `temperature: 0`, `response_format: {"type":"json_object"}`, `max_tokens: 256`, model from `GROQ_MODEL` (default `openai/gpt-oss-20b`).

### Legal & Practical Warning

- **Scraping may violate Forebet's or PassionPredict's terms and robots.txt.** Check before going live; a blocked IP or a legal notice is a real outcome. ⬜ (not yet reviewed)
- **Prefer official or licensed feeds** for anything that settles money. ⬜
- **Store only what you need** — score, source URL, a short page excerpt, and the raw model response. The server does not mirror the page. ✅
- Keep the **manual CSV path working** so the platform never depends on a scraped page being up. ✅

---

## Push Notifications

Admin-to-user notifications are delivered as **Web Push** through the PWA service worker — no Play Store app, no SMS cost. The **in-app inbox is authoritative**; push is a best-effort extra. ✅

### How It Works

```
Admin composes a message
        │
        ▼
Railway API (Go) ──► Web Push service (VAPID)
        │
        ▼
Browser service worker ──► notification on the user's phone
```

1. The PWA registers a service worker.
2. The user opts in and the browser returns a **push subscription**.
3. The subscription is stored in **PostgreSQL**, tied to the user.
4. Admin sends through the Railway API, which signs with **VAPID keys** (environment variables).
5. The service worker displays the notification and opens the right page on tap.

### Platform Support

| Platform | Support | Note |
| --- | --- | --- |
| Android Chrome | Full | Covers most target users |
| iOS Safari 16.4+ | Supported | **Only after the PWA is added to the Home Screen** |
| Desktop browsers | Full | — |

iOS is the real constraint: on iPhone, push only works once the user installs the PWA. On Android — the majority in Tanzania — it works immediately.

### What Gets Sent

**Automatic, event-driven (implemented):**

- **Results published** — each settled bet writes a WIN or LOSS inbox row; a correction writes "Corrected result". ✅
- **Refunds** — postponed/cancelled bets write a refund notice. ✅
- **Account activated** — payment confirmed by the admin. ✅

**Admin broadcast (implemented):** the Notifications centre sends a title (≤ 120 chars), body (≤ 500 chars), optional in-app link, and a category, to **all** non-banned users or **active** users only. The five categories are `match_start`, `results`, `account`, `announcements`, and `community`, and each has its own mute switch. ✅

**Planned (⬜):**

- **Match starting soon** — needs a scheduler; none exists.
- **Community replies / mentions** — no per-post reply model yet.
- **SMS for payment-critical events** — not integrated; there is no SMS provider.

### Guardrails (Enforced Server-Side)

These are checked in the dispatch path, so a bug in the UI cannot bypass them. Every notification row records the last reason a push did not go out and when it will be tried again.

| Guardrail | Behaviour |
| --- | --- |
| **Daily cap** | `PUSH_DAILY_CAP` (default **2**) pushes per user per EAT day. Counted in `push_delivery_log`, not in inbox rows. A capped push is deferred to 00:01 EAT the next day. |
| **Quiet hours** | `QUIET_START_HOUR` (22) to `QUIET_END_HOUR` (7) EAT. Push is deferred until the window ends; the inbox row still lands immediately. |
| **Muted category** | Skipped at push time; inbox row still written. Rechecked every six hours and immediately when the user unmutes a category. |
| **No subscription** | Deferred for one hour and retried immediately when the user subscribes. |
| **Push not configured** | Recorded as `push not configured`; the product still works and retries hourly if keys are added later. |
| **Expired subscription** | A `403`, `404`, or `410` from the push service deletes that subscription row; `403` covers VAPID key rotation. |
| **Transport failure** | Retried with backoff (1m, 5m, 30m, 2h) up to five attempts, then marked terminal. |

The inbox is authoritative. Push is a best-effort extra: a failed push never loses the message.

**Delivery is a queue, not a one-shot send.** Broadcasters are processed in batches of 500 in a loop (no recipient limit), guarded by a Postgres advisory lock so concurrent dispatchers cannot double-send or double-count against the cap. A retry scheduler runs every five minutes (`PUSH_RETRY_ENABLED=0` disables it) and picks up deferred, failed, or interrupted rows. The Profile **Refresh** button re-subscribes with the current VAPID key, and `pushsubscriptionchange` in the service worker tells open tabs to refresh automatically. Logging out unsubscribes this device.

### Low-Data Design

- Payload is **title + short body + link** — a few hundred bytes, no images.
- The service worker shows the notification with `renotify: false` and a per-category `tag`, so rapid updates replace rather than stack.
- Quiet hours and the daily cap are enforced server-side (see above).

### Opt-In Strategy

Ask **after the user's first meaningful action**, never on page load. The app shows its own banner ("Get told when your match starts") after the first placed bet or first published post; the browser permission prompt only fires when the user taps **Turn on**. A denied prompt is hard to reverse, so "Not now" is remembered. ✅ (`maybeAskPush` is called only after a bet is placed or a post is published.)

### Fallbacks

- **In-app notification inbox** — always available, works even if push is blocked. Every category lands there first. ✅
- **SMS for payment-critical events** — activation and withdrawal confirmations, where missing the message costs money. ⬜ Not integrated; there is no SMS provider yet.

### Generating VAPID Keys

```bash
cd backend
go run ./cmd/vapid     # prints VAPID_PUBLIC_KEY and VAPID_PRIVATE_KEY
```

Put both in the environment and redeploy. Rotating the key pair invalidates every existing push subscription; users must opt in again.

---

## Business Model & Monetization

**The payment is a contribution to keep the platform running — not a payment for predictions.**

This distinction must be explicit everywhere: on the pricing page, in the receipt, and in the mobile money prompt description.

| What users pay for | What users do **not** pay for |
| --- | --- |
| Platform access | Predictions |
| Server, bandwidth, and internet costs | Tips or picks |
| Maintenance and support | Guaranteed outcomes |

### How It Fits the Build

- **One flat monthly rate, visible to users.** `ACCESS_FEE_CENTS` is served by `GET /api/v1/config` and shown on Profile as "Monthly access rate — per month · contribution to running costs". The activation panel and `/terms` state plainly that the payment covers **maintenance and internet service so Betva stays available always**, never a guarantee of results. If unset, Profile says "Ask the admin" rather than inventing a number. ✅
- **Manual activation:** after payment is confirmed, the admin marks the user **active** for a number of days. This is the early-stage flow. ✅
- **Planning figures stay admin-only:** the DAU target, monthly running cost, and suggested fee live only in the admin Analytics centre. Users never see targets, costs, or shortfalls. ✅
- **Small and transparent:** the fee covers running costs, so it should be modest and clearly stated. ✅
- **Separate from stakes:** the access contribution is not the bet stake and should never be described as one. ✅

> The planner and the flat rate are described in [Analytics (Daily Active Users & Access-Fee Planner)](#analytics-daily-active-users--access-fee-planner).

### Why the Wording Matters

- Describing the fee as "payment for predictions" changes its legal character and invites disputes about outcomes.
- Keep **receipts and wording consistent** — the mobile money reference, the in-app receipt, and the admin ledger should all say the same thing.
- The platform should never imply that paying improves a user's chances.

---

## Database Backup & Recovery

**Goal: zero data loss for money and identity, and a restore that is tested, not just assumed.**

### What Must Never Be Lost

| Data | Criticality | RPO target |
| --- | --- | --- |
| Wallet balances & transactions | Critical | Near-zero (sync replication) |
| User accounts & KYC names | Critical | Near-zero |
| Bet slips & settlements | Critical | Near-zero |
| Match results & archive | High | ≤ 24 hours |
| Community posts / media | Medium | ≤ 24 hours |
| Live odds / feeds (Redis) | Low | Rebuildable |

### Backup Layers

> **Status: ✅ database backups run daily and off-site.** A Railway cron service dumps
> Postgres, verifies the archive, uploads it to a storage bucket outside the database
> service, and prunes with grandfather-father-son retention. Railway's managed volume
> backups/PITR are Pro-only (this workspace is Hobby), and media backup is still manual.

1. **Daily off-site Postgres dumps** — Railway cron service `backup` runs
   `backend/cmd/backup` at `0 1 * * *` (04:00 EAT). It takes a custom-format `pg_dump`,
   verifies the archive with `pg_restore --list` (refuses to upload a dump with no table
   data), records size + SHA-256 in a JSON manifest, uploads both to the `betva-backups`
   bucket, then prunes. ✅ `docs/DEPLOYMENT.md#4-off-site-backups`
2. **Second-provider mirror** — the same dump and manifest are mirrored to Cloudflare R2
   (`betva-backups-r2`) with separate credentials, and both buckets are pruned. A Railway
   account-level outage cannot take both copies. ✅
3. **Grandfather-father-son retention** — daily 14 days, Sunday 56 days, first-of-month
   183 days. Nothing younger than 24 hours is deleted, so the 6-month archive window is
   covered. ✅
4. **Railway managed volume backups / PITR** — Pro-only. The Hobby workspace rejects the
   schedule API with "Manual backups and backup schedules are only available for Pro
   workspaces", so RPO is up to 24 hours until the workspace is upgraded. ⬜
5. **Redis persistence** — Redis now holds only rebuildable caches (`/stats`) and
   rate-limit counters, both of which expire on their own, so there is no durable
   state to back up. ✅
6. **Code & config in Git** — the schema is versioned (`001`–`012`), so a fresh database
   can be rebuilt from scratch. ✅
7. **Media storage** — uploads live on the Railway volume `media` mounted at `/data` and
   are **not** in a Postgres dump. A volume mounts to one service, so the cron cannot read
   it; backup is manual (`railway volume files download /media ./media-backup`) until
   media moves to R2. 🟡

### Manual Backup Creation ⬜

> **Not built.** There is no backup button, no backup endpoint, and no `backups/manual/` prefix yet. The design below is the target.

The admin can create a backup **on demand**, at any time, with one click. This is not only for disasters — it is used before anything risky.

**When to use it:**
- Before a database migration or schema change.
- Before a large CSV import of matches.
- Before a major match day with high stakes.
- Before any admin action that touches many rows.

**What it captures:**
- Full Postgres dump (schema + data), compressed.
- Redis snapshot (RDB).
- A manifest with timestamp, admin who triggered it, reason, and row counts.

**Where it goes:**
- Cloudflare R2, in a dedicated `backups/manual/` prefix, encrypted at rest.
- Named `manual-YYYYMMDD-HHMMSS-<label>.tar.gz`.

**Rules:**
- **Any admin can trigger; only a super-admin can restore.** Creation and restoration are separate permissions.
- **Retention: 90 days** for manual backups, then auto-deleted. Pinned backups are exempt.
- **Audit log entry** for every create and restore — who, when, why, and the resulting state.
- **Restore requires confirmation + admin 2FA** and is performed into a staging database first for verification, then promoted.

### Restore Discipline

- **Documented runbook** — step-by-step restore procedure, stored outside the production environment. ✅ `docs/RESTORE_RUNBOOK.md`
- **Restore drill** — actually restore a backup into a staging database and verify row counts, wallet totals, and the last 7 days of bets. A backup that has never been restored is a wish. ✅ local, ✅ staging.
  - Automated: `backend/scripts/restore_test.ps1` dumps the live database into a throwaway database, compares row counts for `users`, `bets`, `ledger`, `matches`, and `notifications`, checks the ledger sum, and confirms the restored schema is complete. It exits non-zero on any mismatch, and **fails closed** if the throwaway database was never created — a drill can never report success without a real restore. The role needs `CREATEDB` (`ALTER ROLE betva CREATEDB;`); the staging and production `postgres` roles already have it.
- **Pre-launch restore test is a hard launch blocker.** The platform does not go live until a full restore has been performed and verified end-to-end. This sits alongside the penetration test as a non-negotiable gate. ✅ staging run passed 2026-11-16; full off-site bucket restore also passed 2026-11-16.
- **Off-site restore path** — `go run ./cmd/backup -download <key> -out ./restore.dump` verifies size and SHA-256 against the manifest, then the dump is restored into a throwaway database and compared before promotion. ✅ full drill passed 2026-11-16.
- **Targets:** RTO ≤ 1 hour for critical data, RPO ≤ 24 hours today (daily dump); RPO ≤ 1 minute for wallets needs PITR, which is Pro-only on Railway and not enabled.
- **No single point of failure:** backups are verified by a different person than the one who configured them.

#### Drill Log

| Date | Environment | Result | Evidence |
| --- | --- | --- | --- |
| 2026-11-16 | Local Postgres 18, throwaway database | **Pass** | 5 users, 6 bets, 17 ledger rows, ledger sum 4,750,000 cents, 18 tables, schema check complete. Failing path also verified: with `CREATEDB` removed, the script exits `1` instead of reporting success. |
| 2026-11-16 | Staging (Railway, restored into a throwaway DB) | **Pass** | 1 user, 1 match, 1 bet, 3 ledger rows, ledger sum 225,000 cents, 6 migrations, 19 tables. Public TCP proxy deleted immediately afterwards. See `docs/DEPLOYMENT.md`. |
| 2026-11-16 | Off-site bucket (`betva-backups`), restored into a throwaway DB | **Pass** | 44,353-byte dump verified against its manifest SHA-256, then restored: 1 user, 1 match, 1 bet, 3 ledger rows, ledger sum 225,000 cents, 19 tables. Throwaway DB and temporary TCP proxy deleted. |

### Retention

- Results archive: **6 months, append-only, nothing deleted** — backups must cover the full 6 months, not just the last week. GFS retention keeps a first-of-month dump for 183 days, which covers that window.
- User financial records: **retained as long as the account exists**, plus a defined post-closure period for disputes and tax.
- Backups inherit the retention policy; nothing is silently expired early. The prune step never deletes anything under 24 hours old.

---

## Scalability & Availability

**The system must absorb spikes (match kickoffs, result drops) without dropping a request or losing data.**

### Handling Pressure

> **Status: designed for scale, currently a single instance.** The API is stateless and can run multiple copies, but the other layers are not wired yet.

- **Stateless Go API** — no session state in the process, so it can run behind a load balancer. ✅ (single instance today)
- **Redis for caches & shared rate limits** — backs the 60s `/stats` cache and the shared rate-limit counters; falls back to in-memory if Redis is down. ✅ Live odds/feeds still use Postgres (no live source yet). ⬜
- **Connection pooling** — pgx pool inside the API. PgBouncer or a managed pooler is ⬜.
- **Read/write split** — heavy reads go to a replica; writes go to the primary. ⬜
- **SSE/WebSocket at scale** — Redis pub/sub so any API instance can reach any user. ⬜ (no SSE endpoint exists)
- **Cloudflare absorbs the edge** — static assets, media, and cached pages never reach the origin. ⬜ (not deployed)
- **Queue-based writes for bets** — a kickoff spike becomes a queue, not a DB crash. ⬜ (bets are written synchronously inside one transaction)
- **Rate limiting** at Cloudflare and the API — per-IP and per-user, stricter on login, registration, and payment endpoints. API limits are shared across replicas via Redis; Cloudflare edge limiting is ⬜. See the pen-test checklist.
- **Graceful degradation** — if push fails, the in-app inbox still works. ✅ (DB slowness and write queueing are ⬜)

### Never Losing Data Under Load

- **Transactions are atomic** — a bet either fully commits (slip + wallet debit + ledger entry) or fully rolls back. No half-written bets. ✅
- **Idempotency keys** on every write — a retried request cannot double-charge or double-pay. ⬜ Not implemented; duplicate placement is currently blocked by the one-bet-per-market constraint and the row lock, not by request keys.
- **Append-only ledger** — balance changes are never overwritten, only appended; reconciliation is always possible. ✅
- **Backups run continuously**, not only at night, so a failure at peak loses minutes, not hours. ⬜
- **Migrations are versioned** — `001`–`012`, applied in order. They are forward-only; automatic rollback is ⬜. 🟡

### Capacity Planning

- Start small on Railway, **scale vertically first** (more CPU/RAM per instance), then horizontally.
- Monitor: CPU, memory, DB connections, queue depth, SSE connection count, p95 latency. ⬜ (only request logging today)
- **Alerts** on queue depth and p95 before users feel it. ⬜
- Load test before every major launch with a kickoff-scale traffic profile. ⬜

---

## Security & Anti-Hacking

**Assume the network is hostile. Defend in depth.**

### Transport & Edge

> **Status: ⬜ production edge not configured.** Locally the API runs plain HTTP; the items below are launch requirements.

- **HTTPS everywhere**, HSTS preloaded, TLS 1.2 minimum — Cloudflare enforces this at the edge. ⬜
- **Cloudflare WAF** with managed rulesets, bot protection, and DDoS mitigation. ⬜
- **Security headers** on every API and frontend response: `Content-Security-Policy`, `X-Frame-Options`, `X-Content-Type-Options`, `Referrer-Policy`, `Permissions-Policy`, HSTS. ✅ Go: `internal/httpapi/security.go`; Vercel: `frontend/vercel.json`.
- **Strict CSP** — no inline scripts or styles in the 14 built pages; `script-src 'self'`, `style-src 'self'`, no `unsafe-inline`. ✅ Astro builds external JS/CSS (`assetsInlineLimit: 0`, `inlineStylesheets: 'never'`) and the API origin is injected via a `<meta>` tag instead of inline script.

### Application

- **Parameterized queries only** — every query uses pgx placeholders; no string-built SQL. ✅
- **Input validation** on every endpoint, server-side, with allow-lists (stake tiers, markets, statuses, media MIME and size). ✅ Core paths; a full per-field audit is ⬜.
- **XSS prevention** — user content is escaped before rendering, post media is restricted to `/media/...`, and a strict CSP with no inline script backstops it. ✅
- **CSRF protection** — the API authenticates with a bearer token in `Authorization`, not an ambient cookie, so classic CSRF does not apply. Any future cookie-based auth must add tokens. ✅/⬜
- **Authentication:** phone + bcrypt-hashed password, normalized phone numbers, JWT with 30-day expiry. **Argon2 is not used** — bcrypt at the library default cost. ✅
- **Admin 2FA** — when `ADMIN_TOTP_SECRET` is set, admin login requires a current six-digit TOTP code (or `ADMIN_RECOVERY_CODE`) in addition to the env-var password. Generate secrets with `go run ./cmd/totp`; the login form shows the code field only when 2FA is configured. ✅ TOTP; WebAuthn/passkeys remain ⬜.
- **Rate limiting** on login, registration, admin login, media upload, and AI suggestions — fixed windows keyed by IP, phone, or user ID, with `Retry-After` on `429`. ✅ `backend/internal/httpapi/ratelimit.go`. Counters are shared across replicas via Redis (atomic Lua script) with an in-memory fallback when Redis is unavailable; Redis calls are bounded to 75ms so an outage cannot stall requests. Progressive lockout and a safe unlock flow are still ⬜.
- **Account enumeration** — unknown phone numbers run a dummy bcrypt comparison, so login timing does not reveal whether an account exists. ✅
- **Account lockout** after repeated failures, with a safe unlock path. ⬜ The per-phone attempt cap slows guessing but does not implement a formal lockout/unlock flow.
- **Least privilege in the database** — the API role currently owns the schema locally; production should split migration and runtime roles. 🟡
- **Secrets in environment variables only** — never in the repo, never in client bundles. ✅

### Data Protection

- **Encryption at rest** for Postgres and backups; **TLS in transit** everywhere, including service-to-service. ⬜ (depends on hosting configuration)
- **PII minimization** — official names are returned only to the user and admins, never in public or community responses. Stored in plaintext today; encryption at rest / a separate PII store is ⬜. 🟡
- **Audit trail** — money movements (ledger), bans and status changes, imports, moderation, AI reviews, and corrections record who/when/why. ✅
- **Access control** — a single admin role from env vars. Support roles with view-only money access are ⬜.
- **Data retention policy** — results are append-only; account-deletion and PII-retention rules are ⬜.
- **User rights** — export and deletion paths, consistent with Tanzania's Personal Data Protection Act 2022. ⬜

### Supply Chain & Operations

- **Dependency scanning** (Dependabot / Snyk) with a review process before merging updates. ⬜
- **Pinned versions** — Go modules and `package-lock.json` are committed; automated update testing is ⬜. 🟡
- **No secrets in Git history** — `.env` is ignored; if a secret leaks, rotate immediately, do not just delete the commit. ✅
- **Logging & monitoring** — chi request logging is on; centralised logs and anomaly alerts (failed logins, payout spikes, error rates) are ⬜. 🟡
- **Incident response plan** — who is called, what is shut down first, how users are notified. ⬜
- **Pre-launch penetration test** — by someone who is not the developer. ⬜ Checklist ready at `docs/PEN_TEST_CHECKLIST.md`.
- **Backups of backups** — the restore drill is the proof that recovery works. ✅ locally, ⬜ staging.

---

## Architecture at a Glance

| Layer | Recommended Tech | Main Benefit for Your Goal |
| --- | --- | --- |
| **Frontend** | Astro + Tailwind | Tiny file sizes; won't freeze cheap Android phones |
| **Backend** | Go (Golang) | Handles thousands of simultaneous bets easily |
| **Database** | PostgreSQL + Redis | Safe wallet transactions + instant feed/odds loading |
| **Media** | Cloudflare Stream / Bunny.net | Auto-compresses photos & adapts video quality to network speed |
| **Network** | Cloudflare CDN | Local East African servers reduce lag drastically |
| **Frontend hosting** | Vercel | Auto-builds, preview deploys, edge-cached static output |
| **Backend hosting** | Railway | One platform for the Go API, Postgres, and Redis |
| **Results extraction** | Groq (LLM) | Turns fetched match pages into structured JSON |
| **Payments** | AzamPay / Selcom | Fast USSD mobile money deposits and payouts |

### What is actually running

| Layer | Shipped today | Status |
| --- | --- | --- |
| Frontend | Astro 7 + Tailwind v4, static output, PWA manifest + service worker | ✅ |
| Backend | Go 1.x, chi, pgx | ✅ |
| Database | PostgreSQL 18 locally; migrations `001`–`012` | ✅ |
| Cache / queues | Redis backs the `/stats` cache and shared rate limits (in-memory fallback) | ✅ |
| Media | Local `MEDIA_DIR` by default; R2 when configured on the API | ✅ |
| Results extraction | Groq `openai/gpt-oss-20b`, admin-triggered, manual approval | ✅ |
| Payments | Manual admin credit only; no mobile-money integration | ⬜ |
| Hosting | Nothing deployed; local `:8080` API and `:4321` frontend | ⬜ |
| Edge | No Cloudflare, no Vercel, no Railway project yet | ⬜ |

### How the pieces fit together

- **Vercel** serves the frontend only (the Astro build output).
- **Railway** runs the backend API plus PostgreSQL and Redis.
- **Cloudflare** sits in front for media (Images/Stream), storage (R2), and the East African CDN edge.

These are complementary, not competing — Vercel handles app code, Railway handles stateful services, Cloudflare handles the heavy media and last-mile delivery.

---

## 1. Frontend

> What runs on the user's phone.

Heavy JavaScript frameworks (standard React, Angular) drain battery and freeze low-RAM smartphones. The stack below sends minimal code to the browser.

### Framework: Astro (chosen) ✅

- **Why:** It compiles pages into tiny HTML and CSS files and ships almost zero JavaScript by default, so pages load instantly on 3G/4G. SvelteKit was the alternative; Astro won because the app is mostly static pages with small islands of interactivity.
- **Status:** Astro 7 with Tailwind v4, static output. ✅

### Styling: Tailwind CSS ✅

- **Why:** Generates a single, tiny CSS file that caches well on users' phones, saving mobile data on repeat visits.
- **Status:** Tailwind v4 via `@tailwindcss/vite`, with the design tokens in `src/styles/global.css`. ✅

### Progressive Web App (PWA) 🟡

- **Why:** Users can add the site to their home screen like an app — no 50 MB Play Store download.
- **Manifest:** `manifest.webmanifest` with `display: standalone`, dark theme colour, and the eagle-B SVG icon. ✅
- **Service worker:** registered for Web Push and notification clicks. ✅
- **Offline caching:** ⬜ — the service worker does not cache pages or assets yet, so the app needs a connection.
- **Push:** The same service worker that enables push notifications also handles notification clicks — see [Push Notifications](#push-notifications). ✅

---

## 2. Backend & Real-Time Betting Engine

> Betting requires instant odds updates and rock-solid reliability for financial transactions.

### Language & Framework: Go (chosen) ✅

- **Why:** Go is built for high concurrency — thousands of users can place bets and upload content simultaneously without the server crashing or lagging.
- **Status:** chi router, pgx, and Redis for caching + shared rate limits. Node.js/Fastify was the alternative and was not used. ✅

### Real-Time Odds: Server-Sent Events (SSE) or Lightweight WebSockets ⬜

- **Why:** Instead of forcing the phone to constantly refresh for live odds, SSE pushes tiny text updates (a few bytes) only when an odd changes.
- **Status:** Not built. There are only two fixed markets and no live odds feed, so this is deferred until a live-pricing source exists.

### Database: PostgreSQL + Redis 🟡

| Component | Role | Status |
| --- | --- | --- |
| **PostgreSQL** | User accounts, wallet balances, betting slips, matches, community, notifications, analytics | ✅ Authoritative store |
| **Redis** | Short-lived `/stats` cache and shared rate-limit counters | ✅ Connected at startup; in-memory fallback if unavailable. Live odds/feeds/pub-sub still ⬜ (no live source yet). |

---

## 3. Media Optimization

> User-uploaded images and videos are the biggest threat to site speed and user data bundles. All media must be processed before it reaches other users.

### Delivery & Compression: local today, Cloudflare later 🟡

- **Images:** uploads are stored as received, capped at **400 KB**, and served from `/media/*`. Server-side MIME sniffing prevents a renamed executable from being served as an image. Auto-conversion to WebP/AVIF and blur-up placeholders are ⬜.
- **Profile pictures:** `PUT /me` now accepts only local `/media/<name>.(jpg|jpeg|png|webp|gif)` paths, so a tracking pixel, `javascript:` URL, or remote embed cannot be stored as an avatar. There is still no upload UI or separate 10 KB image-size check. 🟡
- **Videos:** uploads are capped at **3 MB**, served as a single file with `preload="none"` and native controls. No autoplay. HLS adaptive bitrate is ⬜.
- **Storage:** local `MEDIA_DIR` with random 24-character hex filenames and `Cache-Control: public, max-age=31536000, immutable` by default; when the five `R2_*` media variables are set, uploads go straight to Cloudflare R2 behind the same endpoint. ✅
- **Cloudflare Images/Stream** (auto WebP/AVIF, blur-up placeholders, adaptive HLS) is still ⬜.

### Storage: Cloudflare R2 ✅ (opt-in via env vars)

- **Why:** Cheap, scalable storage for community uploads, zero egress fees.
- **Status:** Implemented in `backend/internal/httpapi/media_storage.go`. When `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_MEDIA_BUCKET`, and `R2_MEDIA_PUBLIC_URL` are all set, `POST /api/v1/media` writes to R2 (S3 API, region `auto`) with immutable cache headers and stores the public URL in the post. Otherwise it falls back to the local `MEDIA_DIR`. No frontend change is needed to switch. Staging values are recorded in `docs/DEPLOYMENT.md`; the API service must have the media credentials set to enable it.

---

## 4. Local Payments & Network Infrastructure

> Keep infrastructure close to East Africa and integrate local mobile money seamlessly.

### CDN: Cloudflare

- **Why:** Cloudflare has edge servers directly in **Dar es Salaam** and **Mombasa**. Static files and cached pages are served locally in milliseconds instead of traveling to Europe or the US.

### Mobile Money: Selcom, AzamPay, or Pesapal ⬜

- **Why:** These APIs support fast, direct push-USSD prompts for **M-Pesa**, **Tigo Pesa**, **Airtel Money**, and **HaloPesa** — no slow redirect to external payment pages.
- **Status:** Not integrated. Deposits are recorded by the admin with a required reason and a ledger row; there is no provider callback, receipt, or automatic activation.

---

## 5. Cloud Deployment (Vercel + Railway)

> **Status: ✅ staging is live.** Frontend: <https://betva-two.vercel.app>. API: <https://api-production-a377a156.up.railway.app>. Railway hosts the Go API and Postgres in `europe-west4`; Vercel hosts the Astro build. Cloudflare edge/media is still ⬜. Full topology, deploy commands, environment variables, and rollback steps: [`docs/DEPLOYMENT.md`](docs/DEPLOYMENT.md).

> Vercel hosts the frontend. Railway hosts the backend and its data stores. Cloudflare handles media and edge delivery in East Africa.

### Vercel — Frontend Hosting

- **Serves:** Astro build output.
- **Why:** Zero-config builds, automatic preview deployments per branch, and global edge caching with Brotli/Gzip enabled by default.
- **Fits the plan because:** Astro's mostly-static output is exactly what Vercel optimizes for — near-zero JS shipped, cached at the edge.

### Railway — Backend & Data Hosting

- **Serves:** Go (Golang) API, PostgreSQL, and Redis.
- **Why:** Managed Postgres and Redis on the same platform as the API, so the betting engine doesn't need separate database vendors.
- **Fits the plan because:** Railway's Postgres provides the strict financial accuracy layer; its Redis keeps live odds and feeds fast.
- **Scales because:** Container-based services can run multiple instances behind Railway's load balancer for concurrent bet traffic.

### Vercel + Railway + Cloudflare (responsibility split)

| Platform | Owns | Does not own |
| --- | --- | --- |
| **Vercel** | Frontend builds & static delivery | Databases, live odds |
| **Railway** | API, PostgreSQL, Redis, background jobs | User media storage |
| **Cloudflare** | Images, Stream, R2 storage, CDN edge in Dar es Salaam / Mombasa | Application logic |

### Edge Decision: Cloudflare Proxies Vercel

**Decision: Cloudflare sits in front of Vercel for the frontend.** They are not competing — Vercel builds and serves, Cloudflare only fronts the delivery path.

**Why proxy instead of keeping them separate:**

1. **Vercel has no edge node in Africa.** A user in Dar es Salaam asking Vercel directly routes to Europe or the Middle East. Cloudflare's Dar es Salaam PoP absorbs that first hop, so uncached HTML and first paint get faster — not just static assets.
2. **Astro's output is nearly pure static files**, which is the ideal case for a proxy: extremely high cache hit rate and almost no origin round-trips.
3. **One domain, one certificate, one place for WAF and security headers.** Keeping them separate means two TLS setups and split caching logic to maintain.
4. **No added cost** on Cloudflare's free plan.

**Critical rule:** Cloudflare must never cache or serve stale data for anything user-specific or financial. A stale wallet balance or bet slip is a financial bug, not a slow page. Live odds SSE bypasses Cloudflare's cache entirely.

| Subdomain | Target | Cloudflare | Caching |
| --- | --- | --- | --- |
| `www` | Vercel | Proxied | Static only — bypass `/account/*`, `/wallet/*`, `/bets/*` |
| `media` | R2 / Stream / Images | Proxied | Cache everything |
| `api` | Railway | Bypass / grey-cloud | Never cache (SSE + financial writes) |

### Deployment Flow

```
User in Tanzania
      │
      ▼
Cloudflare edge (Dar es Salaam / Mombasa)
      ├── www      → Vercel   (static Astro output, cached)
      ├── media    → R2/Stream/Images (cached, compressed)
      └── api      → Railway  (SSE + Postgres + Redis, never cached)
```

**Note on live odds:** SSE/WebSockets run from Railway's API. Because SSE connections stay open, the API needs a service with persistent connections and horizontal scaling — Railway's container setup supports this. Keep the betting SSE/WebSockets endpoint off both Vercel (short-lived serverless requests) and Cloudflare's cache (stale odds).

---

## Crucial Design Tips for Low-Data Users

- **Data Saver Mode toggle** — let users disable auto-playing videos and load lower-resolution images with one tap. ⬜ (video never autoplays anyway)
- **Keep betting slips lightweight** — avoid heavy animations or complex graphics on the main betting interface. ✅
- **Compress API responses** — Brotli or Gzip on the server shrinks text data by up to 80% before it hits the network. ✅ `middleware.Compress(5)`

---

## Open Questions

### Product & Accounts

- [ ] **KYC check process** — how does the admin verify the official name matches the phone owner? Manual review, or a check against TCRA/NIDA records?
- [x] ~~**Ban appeal**~~ — resolved: 7-day notice, appeal, phone/name change, admin review.
- [x] ~~**Repeat mismatches**~~ — resolved: 2nd = suspension + 48h review, 3rd = ban; phone-only change = routine re-verification.
- [ ] **Phone verification** — is an OTP/SMS code required at registration, or is the password alone enough?
- [ ] **Password policy** — "simple password" needs a floor: minimum length, and whether weak/common passwords are rejected.
- [ ] **Nickname rules** — must nicknames be unique, and what is allowed (length, characters, profanity filter)?
- [ ] **Profile picture spec** — target dimensions and format for the < 10 KB limit (e.g. 96×96 or 128×128 WebP).
- [x] ~~**CSV template**~~ — resolved: template lives at `frontend/public/matches-template.csv`.
- [x] ~~**Match & option model**~~ — resolved: only Full Time Over 0.5 Goals and Over 0.5 Half Time.
- [x] ~~**Settlement rules**~~ — resolved: official source decides, admin approves, corrections are reversing ledger entries.
- [x] ~~**History contents**~~ — resolved: History shows bets only; the wallet ledger lives on Profile.
- [ ] **Commission** — the payout multiplier is configurable (`PAYOUT_MULTIPLIER_BP`, default 1.50×), but the final commercial margin and whether a separate commission line is shown still need sign-off.
- [x] ~~**Access fee**~~ — resolved: one **flat monthly rate** (`ACCESS_FEE_CENTS`), shown to users on Profile; planning figures (DAU target, running cost, suggested fee) are admin-only. The amount itself is set by the operator.
- [x] ~~**Notification policy**~~ — resolved: max 2/day, quiet hours 22:00–07:00 EAT, per-category mute.

### AI & Results

- [x] ~~**Authoritative source**~~ — decided: disagreement = `DISPUTED`, official league source settles, Forebet is the default manual reference. ⬜ the `DISPUTED` state and automated two-source check are not built.
- [ ] **Scraping permission** — have Forebet and PassionPredict terms/robots.txt been checked?
- [ ] **Free API choice** — football-data.org, API-Football, or TheSportsDB as the primary fallback?
- [x] ~~**Groq model**~~ — resolved: default `openai/gpt-oss-20b`, configurable via `GROQ_MODEL`. Per-day token cost still unmeasured.
- [ ] **Confidence threshold** — minimum confidence for a Groq-extracted row to reach the review queue at all? (Today every valid row reaches the queue with its badge.)

### Design & Brand

- [x] ~~**Brand colour**~~ — resolved: Google blue `#4285F4` → `#1A73E8`; the wordmark uses the five Google colours.
- [x] ~~**Logo & app icon**~~ — resolved: eagle-cutout "B" emblem used as favicon/logo, plus a horizontal wordmark. PNG icons and `.ico` are generated from the same SVG via `npm run icons`. ✅
- [x] ~~**Light mode depth**~~ — resolved: **dark-only for v1**. A light theme is deferred.

### Reliability & Security

- [x] ~~**Backup owner**~~ — resolved: the restore drill is a **hard launch blocker**, owned by the operator. Runbook: `docs/RESTORE_RUNBOOK.md`; staging drill passed 2026-11-16; daily off-site dumps run to the `betva-backups` bucket. ✅
- [x] ~~**Admin 2FA**~~ — resolved: TOTP (authenticator app) plus a recovery code, both stored in environment variables alongside the admin password. ✅ WebAuthn remains a future option.
- [ ] **Penetration test** — budget and vendor for the pre-launch test?
- [ ] **Incident contact** — who is on call, and how are users notified?

### Trust & Display

- [x] ~~**Counter cadence**~~ — resolved: the counter changes **at settlement**; `GET /api/v1/stats` is cached for 60 seconds.
- [x] ~~**Counter reset**~~ — resolved: it runs for the life of the product, over the append-only results archive. No reset.
- [x] ~~**Confidence rules**~~ — resolved: the admin sets HIGH / MEDIUM / LOW per match at import or edit time; it is displayed, not used in settlement maths, and snapshotted onto the bet at placement.

### Business & Infrastructure

- [ ] Building this in-house, or hiring a development team?
- [ ] Estimated starting budget for servers and infrastructure?
- [ ] Do you already hold a betting license (e.g. from the Gaming Board of Tanzania)?
- [ ] **Data protection** — how long is the official name retained, and is it covered by a privacy policy under Tanzania's Personal Data Protection Act 2022?

**Next step:** a step-by-step launch architecture and monthly cost estimate, based on the answers above — covering Vercel, Railway, Cloudflare, and mobile money provider fees.

---

## Development Status

**Phases 1–5 are complete and verified locally. Phase 6 (launch) is in progress: documentation, security checklist, restore runbook, staging deployment, both restore drills, daily off-site backups with an R2 mirror, admin TOTP 2FA, security headers, and rate limiting are done. The independent penetration test, Cloudflare edge/WAF, managed PITR, and real-device latency measurement remain.**

### What exists now

| Area | Status | Location |
| --- | --- | --- |
| Go API (chi router, pgx, Redis caches + shared rate limits) | ✅ Working | `backend/` |
| Postgres schema and migrations `001`–`012` | ✅ Working | `backend/migrations/` |
| User register / login / profile | ✅ Working | `backend/internal/httpapi/auth.go` |
| Admin login from environment variables | ✅ Working | `backend/internal/httpapi/admin.go` |
| Matches CSV upload, validation, duplicate detection, import log | ✅ Working | `backend/internal/httpapi/matches.go` |
| Admin day view, confidence and status editing | ✅ Working | `frontend/src/pages/admin/matches.astro` |
| Users list, search, manual activation, suspend / ban, audit log | ✅ Working | `frontend/src/pages/admin/users.astro` |
| Daily active users + access-fee planner | ✅ Working | `backend/internal/httpapi/analytics.go`, `frontend/src/pages/admin/analytics.astro` |
| Flat access rate shown on Profile | ✅ Working | `GET /api/v1/config`, `frontend/src/pages/profile.astro` |
| Astro + Tailwind user app (PWA shell) | ✅ Working | `frontend/` |
| Admin app (separate layout) | ✅ Working | `frontend/src/pages/admin/` |
| Bet slip with fixed stake tiers | ✅ Working | `frontend/src/pages/index.astro` |
| Home Today / Tomorrow tabs with settled ✓ / ✕ | ✅ Working | `frontend/src/pages/index.astro` |
| Internal wallet ledger (admin-only; never shown to users) | ✅ Working | `backend/internal/httpapi/bets.go` |
| Bet placement (atomic debit + bet row + ledger) | ✅ Working | `backend/internal/httpapi/bets.go` |
| Settlement, correction with reversal, postponed refunds | ✅ Working | `backend/internal/httpapi/settlement.go` |
| History with final scores and visible losses | ✅ Working | `frontend/src/pages/history.astro` |
| Admin settle / correct / refund modal | ✅ Working | `frontend/src/pages/admin/matches.astro` |
| Community feed, composer, media upload | ✅ Working | `frontend/src/pages/community.astro` |
| Post reports, 3-report auto-hide, admin moderation | ✅ Working | `backend/internal/httpapi/community.go` |
| In-app notification inbox and per-category mutes | ✅ Working | `backend/internal/httpapi/notifications.go` |
| Web Push with VAPID, daily cap, quiet hours | ✅ Working | `frontend/public/sw.js`, `frontend/src/lib/push.ts` |
| Self-exclusion 24h / 7d / 30d, append-only | ✅ Working | `backend/internal/httpapi/notifications.go` |
| AI result suggestions with admin approval | ✅ Working | `backend/internal/httpapi/ai.go`, `frontend/src/pages/admin/ai.astro` |
| Trust counter (tips, wins, losses, rate, streak) | ✅ Working | `backend/internal/httpapi/stats.go`, `frontend/src/components/TrackRecord.astro` |
| Responsive + accessibility audit harness | ✅ Working | `frontend/scripts/responsive-audit.html` |
| Restore drill script (fail-closed) | ✅ Working | `backend/scripts/restore_test.ps1` |
| Restore runbook | ✅ Written | `docs/RESTORE_RUNBOOK.md` |
| Penetration-test checklist | ✅ Written | `docs/PEN_TEST_CHECKLIST.md` |
| Backend container image + migration runner | ✅ Working | `backend/Dockerfile`, `backend/cmd/migrate`, `backend/migrations/embed.go` |
| Staging deployment (API + Postgres + frontend) | ✅ Live | <https://betva-two.vercel.app>, `docs/DEPLOYMENT.md` |
| Staging restore drill | ✅ Passed 2026-11-16 | 225,000-cent ledger sum verified in a throwaway DB |
| Daily off-site Postgres backup cron | ✅ Live | `backend/cmd/backup`, `backend/Dockerfile.backup`, `docs/DEPLOYMENT.md#4-off-site-backups` |
| PNG/ICO app icons from the eagle-B mark | ✅ Working | `frontend/scripts/generate-icons.mjs` |
| Penetration test and real-device latency measurement | ⬜ Later | Phase 6 |
| Cloudflare edge/WAF, managed PITR | ⬜ Later | Phase 6 |
| Public landing page, KYC queue, admin profile, results archive page | ⬜ Later | Post-launch |

### Matches CSV format

Each row carries its own `date` — the CSV date is authoritative, so one file can hold several days. The form date is only a fallback for rows without one. Header row required, column order does not matter.

```csv
date,kickoff,league,home_team,away_team,markets,ft_odds,ht_odds,confidence
2026-11-16,16:00,Example League,Home Team,Away Team,ft_over_0_5|ht_over_0_5,1.50,2.10,HIGH
2026-11-17,18:30,Example League,Team Two,Team Three,ft_over_0_5,1.80,,MEDIUM
```

| Column | Required | Rules |
| --- | --- | --- |
| `date` | No* | `YYYY-MM-DD`; authoritative per row. *Required when the form has no fallback date |
| `kickoff` | Yes | `HH:MM`, 24-hour, EAT |
| `league` | No | Free text |
| `home_team` | Yes | Must differ from `away_team` |
| `away_team` | Yes | Must differ from `home_team` |
| `markets` | Yes | `ft_over_0_5` and/or `ht_over_0_5`, separated by `\|`, `,` or `;` |
| `ft_odds` | No | Decimal FT odds, e.g. `1.50`; stored as basis points (`15000`) |
| `ht_odds` | No | Decimal HT odds, e.g. `2.10`; stored as basis points (`21000`) |
| `confidence` | No | `HIGH`, `MEDIUM`, `LOW` — defaults to `MEDIUM` |

A market with no odds falls back to `PAYOUT_MULTIPLIER_BP`. Odds are snapshotted on the bet at placement.

Behaviour:

- **Validation first** — every bad row is rejected with its line number and a readable reason; good rows still import.
- **Duplicate detection** — fixtures already stored for each row's date are reported; the upload is refused unless `replace` is checked.
- **Replace is blocked when bets or archived results exist** — money history is never deleted by a re-upload.
- **Delete** — `DELETE /api/v1/admin/matches/{id}` removes a wrongly uploaded match only before kickoff while upcoming/unsettled, and only when it has no bets or archived results. The reason and reviewer are written to `match_delete_log`.
- **Import log** — one row per date records admin, file name, totals, failures, and whether it replaced.
- **Visibility** — users only ever see today and tomorrow; later dates are stored and appear when they enter the window.

### Wallet, bets, and settlement

Money is stored in **cents** (`balance_cents`, `stake_cents`, `payout_cents`); the UI divides by 100 for TZS. Every movement writes a row to the append-only `ledger` table.

**Placing a bet** — one transaction, in this order:

1. Lock the user row (`SELECT … FOR UPDATE`) so two taps cannot overspend.
2. Check the account is `active` and `access_expires_at` has not passed.
3. Check the optional daily cap (`MAX_BET_PER_DAY_CENTS`, `0` = off).
4. Check the match is `upcoming` and kickoff has not passed, and the market is offered. Snapshot that market's odds (or the global multiplier).
5. Reject a second slip on the same user + match + market (`idx_bets_one_per_market`).
6. Debit the stake, insert the bet, insert a `bet_placed` ledger row, commit.

The bet stores `confidence_at_placement` and `payout_multiplier_bp` so later changes to either do not rewrite history.

**Fixed stake tiers.** Stakes are never free-form. The server validates `stake_cents` against `STAKE_TIERS_CENTS`; the client renders the same list from `GET /api/v1/config`.

**Settlement** (`ft_over_0_5` wins when full-time goals ≥ 1; `ht_over_0_5` wins when half-time goals ≥ 1):

- Payout = `stake × bet.payout_multiplier_bp / 10000` (the snapshot: that market's odds when present, else `PAYOUT_MULTIPLIER_BP`), credited with a `bet_won` ledger row. The stake was already debited, so this is the full return.
- Half-time score is required when any bet exists on the HT market.
- The result is written to `results_archive` and the match is marked `finished` / `settled_at`.

**Corrections** (`correct=true` plus a reason):

- Old winning payouts are reversed with a **negative ledger row** (`bet_correction_reversal`); nothing is edited in place.
- The old result stays in `results_archive`; the correction is a **new row** linked by `corrects_id`, with reviewer, reason, and timestamp.
- Bets are reset to `pending`, re-settled against the corrected score, and users are notified in the product copy.

**Postponed / cancelled matches** are refunded, never left pending:

- Every pending bet becomes `refunded` with `void_reason`, the stake is credited back, and a `bet_refund` ledger row is written.
- The match is marked `postponed` or `cancelled`.

### Community, notifications, and self-exclusion

**Community** (migration `004`, `community.go`):

- `posts` stores text and at most one media reference; `status` is `visible`, `hidden`, or `deleted` — nothing is hard-deleted.
- Media uploads are MIME-sniffed from the first 512 bytes, size-capped per type, written with a random hex name under `MEDIA_DIR`, and served from `/media/*`.
- A post may only reference `/media/...` URLs, blocking remote images and tracking pixels.
- `post_reports` has a unique `(post_id, reporter_id)` index, so one user cannot inflate a report count.
- Three **distinct** reports set the post to `hidden` automatically. Admin hide/restore/delete records `moderated_by`, `moderation_reason`, `updated_at`, and resolves open reports.

**Notifications** (`notifications.go`):

- Every message is first written to `notifications` (the inbox). Push is a second, best-effort step — settlement, refunds, activation, and broadcasts all use the same path.
- `notification_prefs` holds five category mutes; `push_subscriptions` stores endpoint + keys; `push_delivery_log` records delivered pushes so the daily cap is honest.
- Quiet hours and the cap are computed at dispatch time in `Africa/Dar_es_Salaam`.
- Settlement writes **WIN** and **LOSS** results in the same style, and a correction writes "Corrected result" — losses are never hidden.

**Self-exclusion** (`self_exclusions`):

- Append-only rows with `until` and `duration_hours` constrained to `24`, `168`, or `720`.
- Bet placement checks for an unexpired row inside the same transaction as the debit, so it cannot be raced.
- There is no update or delete endpoint. An active period blocks starting another.

## API Reference

All routes are under `/api/v1`. User routes require `Authorization: Bearer <user JWT>`; admin routes require an admin JWT (`is_admin=true`). Errors are JSON `{"error": "..."}`. Money is always integer cents.

### Public

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/health` | Liveness probe (outside `/api/v1`) |
| `GET` | `/api/v1/ping` | Cheap connectivity check |
| `GET` | `/api/v1/config` | Stake tiers, payout multiplier, markets, push settings, quiet hours, `access_fee_cents` |
| `GET` | `/api/v1/stats` | Aggregate track record (60s cache) |

### User API

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/v1/auth/register` | Register with nickname, phone, official name, password, `accepted_terms` |
| `POST` | `/api/v1/auth/login` | Login → user JWT |
| `POST` | `/api/v1/auth/logout` | Stateless: returns `logged out`; the client discards the token |
| `GET` | `/api/v1/me` | Current profile |
| `PUT` | `/api/v1/me` | Update password and/or `profile_pic` (no nickname change) |
| `GET` | `/api/v1/matches` | Today + tomorrow, with `is_open`, `market_odds`, and `preview: true` when an inactive account gets only three |
| `POST` | `/api/v1/bets` | Place a slip: `match_id`, `market`, `stake_cents` |
| `GET` | `/api/v1/wallet` | Balance + last 50 ledger entries |
| `GET` | `/api/v1/history` | User bets for the last 6 months, with scores and `odds_bp` |
| `GET` | `/api/v1/community/posts?limit=&before=` | Visible posts, newest first; `next_before` is the cursor |
| `POST` | `/api/v1/community/posts` | Publish text and/or one media item (active accounts only) |
| `POST` | `/api/v1/community/posts/{id}/react` | `like`, `love`, or `haha`; tapping the current one removes it (active accounts only) |
| `DELETE` | `/api/v1/community/posts/{id}` | Soft-delete your own post |
| `POST` | `/api/v1/community/posts/{id}/report` | Report a post once; 3 distinct reports auto-hide |
| `POST` | `/api/v1/media` | Multipart `file`; image ≤ 400 KB, video ≤ 3 MB |
| `GET` | `/api/v1/notifications?limit=` | Inbox, newest first, plus `unread` count |
| `POST` | `/api/v1/notifications/{id}/read` | Mark one read |
| `POST` | `/api/v1/notifications/read-all` | Mark all read |
| `GET` | `/api/v1/notifications/prefs` | Five category switches |
| `PUT` | `/api/v1/notifications/prefs` | Update any subset of the switches |
| `POST` | `/api/v1/push/subscribe` | Store browser subscription (`endpoint`, `p256dh`, `auth`) |
| `POST` | `/api/v1/push/unsubscribe` | Remove a subscription by endpoint |
| `GET` | `/api/v1/self-exclusion` | Active pause, if any |
| `POST` | `/api/v1/self-exclusion` | Start a `24`, `168`, or `720` hour pause |

### Admin API

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/v1/admin/login` | Env-var credentials → admin JWT |
| `GET` | `/api/v1/admin/me` | Current admin identity |
| `POST` | `/api/v1/admin/matches/upload` | Multipart: `file`, optional `date` fallback and `replace`; CSV rows carry `date` and odds |
| `GET` | `/api/v1/admin/matches?date=YYYY-MM-DD` | All matches for one date, with `market_odds`, `settled` and scores |
| `PUT` | `/api/v1/admin/matches/{id}` | Change `confidence` / `status` |
| `DELETE` | `/api/v1/admin/matches/{id}` | Delete a wrong upload before kickoff; optional `reason`; blocked if settled, started, or holding bets/results |
| `GET` | `/api/v1/admin/matches/{id}/bets` | Exposure for one match |
| `POST` | `/api/v1/admin/matches/{id}/settle` | Record result and settle pending bets; `correct=true` reverses first |
| `POST` | `/api/v1/admin/matches/{id}/refund` | Refund pending bets; `status` is `postponed` or `cancelled` |
| `GET` | `/api/v1/admin/imports` | Recent upload log |
| `GET` | `/api/v1/admin/users?q=&status=` | Search and filter users |
| `PUT` | `/api/v1/admin/users/{id}/status` | Activate (with days), suspend, ban, deactivate |
| `POST` | `/api/v1/admin/users/{id}/credit` | Manual deposit / withdrawal with a reason |
| `GET` | `/api/v1/admin/analytics?days=7..90` | DAU series, summary counters, and the admin-only fee plan |
| `GET` | `/api/v1/admin/community/posts?status=` | Moderation queue: `reported`, `visible`, `hidden`, `deleted`, or all |
| `PUT` | `/api/v1/admin/community/posts/{id}/status` | `visible`, `hidden`, or `deleted`, with a reason |
| `POST` | `/api/v1/admin/notifications/send` | Broadcast to `all` or `active`; writes inbox rows, then pushes |
| `GET` | `/api/v1/admin/notifications` | Recent broadcasts grouped by minute |
| `POST` | `/api/v1/admin/ai/suggest` | Fetch `source_url` and extract a score for `match_id` |
| `GET` | `/api/v1/admin/ai/suggestions?match_id=&status=` | Suggestion queue (`pending`, `approved`, `rejected`, `failed`) |
| `PUT` | `/api/v1/admin/ai/suggestions/{id}/review` | `approved` or `rejected`; rejection requires a reason |

> An approved suggestion does **not** settle a match or move money. Settlement is a separate admin action on `/admin/matches/{id}/settle`.

## Running Locally

Prerequisites: **Go 1.27+**, **Node 22.23+**, **PostgreSQL 16+**, and optionally Redis. Redis is not required — the API logs a non-fatal warning and continues without it.

```bash
# 1. Database
psql -U postgres -c "CREATE USER betva WITH PASSWORD 'betva';"
psql -U postgres -c "CREATE DATABASE betva OWNER betva;"

# 2. Schema — apply every migration in order
psql -U betva -d betva -f backend/migrations/001_init.sql
psql -U betva -d betva -f backend/migrations/002_matches_admin.sql
psql -U betva -d betva -f backend/migrations/003_wallet_settlement.sql
psql -U betva -d betva -f backend/migrations/004_community_notifications.sql
psql -U betva -d betva -f backend/migrations/005_ai_results.sql
psql -U betva -d betva -f backend/migrations/006_analytics.sql

# 3. Backend
cd backend
cp .env.example .env        # Windows PowerShell: Copy-Item .env.example .env
# set JWT_SECRET, ADMIN_USERNAME, ADMIN_PASSWORD; add GROQ_API_KEY to enable AI
go run ./cmd/vapid          # paste the keys into .env as VAPID_PUBLIC_KEY / VAPID_PRIVATE_KEY
go run ./cmd/server         # http://localhost:8080

# 4. Frontend (second terminal)
cd frontend
npm install
npm run dev                 # http://localhost:4321
```

On Windows, the `psql` binary may not be on `PATH`; use the full path, for example `& "C:\Program Files\PostgreSQL\18\bin\psql.exe" -U betva -d betva -f backend/migrations/001_init.sql`, and set `$env:PGPASSWORD='betva'` first.

**Admin app:** `http://localhost:4321/admin` — sign in with `ADMIN_USERNAME` / `ADMIN_PASSWORD`. The user app redirects unauthenticated visitors to `/login`.

**Checks:**

```bash
# Backend
cd backend
go vet ./...
go build ./...
go test ./...

# DB integration tests (uses the local betva database; creates and rolls back its own rows)
$env:BETVA_TEST_DATABASE_URL='postgres://betva:betva@localhost:5432/betva?sslmode=disable'
go test ./internal/httpapi -run Integration -v

# Frontend — strict typecheck + production build
cd frontend
npm run check
```

**Restore drill** (requires a role with `CREATEDB`):

```powershell
cd backend
.\scripts\restore_test.ps1
```

The script exits `0` only when the throwaway database was created, restored, and verified. See `docs/RESTORE_RUNBOOK.md`.

## Environment Variables

| Variable | Required | Notes |
| --- | --- | --- |
| `DATABASE_URL` | Yes | Postgres connection string |
| `JWT_SECRET` | Yes | Long random string in production |
| `ADMIN_USERNAME` | Yes | Never stored in the database |
| `ADMIN_PASSWORD` | Yes | Changing it requires a redeploy |
| `REDIS_URL` | No | Optional today; required for live feeds, shared rate-limit counters on multi-replica deploys |
| `CORS_ORIGINS` | Production | Comma-separated frontend origins |
| `PORT` | No | Defaults to `8080` |
| `ENV` | No | `development` or `production` |
| `STAKE_TIERS_CENTS` | No | Comma-separated fixed stakes in cents; default `50000,100000,200000,500000` (500–5,000 TZS). Staging adds `1000000` (10,000 TZS) for the two-option demo. |
| `PAYOUT_MULTIPLIER_BP` | No | Basis points returned on a win; default `15000` = 1.50× |
| `MAX_BET_PER_DAY_CENTS` | No | Optional per-user daily stake cap in cents; `0` disables it |
| `MEDIA_DIR` | No | Where community uploads are stored; default `media` (relative to the backend working directory) |
| `MAX_IMAGE_BYTES` | No | Image cap; default `400000` (400 KB) |
| `MAX_VIDEO_BYTES` | No | Video cap; default `3000000` (3 MB) |
| `VAPID_PUBLIC_KEY` | Push | Public key, also served by `GET /api/v1/config` |
| `VAPID_PRIVATE_KEY` | Push | Secret. Rotating it invalidates every subscription |
| `VAPID_SUBJECT` | Push | Contact URI for the push service, e.g. `mailto:admin@example.com` |
| `PUSH_DAILY_CAP` | No | Pushes per user per EAT day; default `2`, `0` = unlimited |
| `QUIET_START_HOUR` | No | Default `22` (EAT); invalid values fall back to default |
| `QUIET_END_HOUR` | No | Default `7` (EAT); equal start/end is nudged by one hour |
| `PUSH_RETRY_ENABLED` | No | Set to `0` to stop the five-minute retry scheduler |
| `AUTO_SETTLE_ENABLED` | No | Default `1`. Set to `0` to stop the settlement scheduler |
| `AUTO_SETTLE_GRACE_MINUTES` | No | Minutes after kickoff before a match is settled; default `120` |
| `AUTO_SETTLE_INTERVAL_MINUTES` | No | Settlement sweep interval; default `15` |
| `AUTO_RESULT_FETCH_ENABLED` | No | Default `1`. Set to `0` to stop deterministic web result discovery (ESPN + 100Tip) |
| `AUTO_RESULT_FETCH_MAX_DATES` | No | Most recent match dates scanned per sweep; default `7` |
| `GROQ_API_KEY` | AI | Groq API key. Empty disables the AI Results centre (`503`). Set on staging 2026-11-16 |
| `GROQ_MODEL` | No | Default `openai/gpt-oss-20b` |
| `ACCESS_FEE_CENTS` | No | Flat monthly access rate in cents; public via `/config`, shown on Profile. `0` shows "Ask the admin" |
| `ACCESS_FEE_DAU_TARGET` | No | **Admin-only.** DAU at which the fee planner activates; `0` = unconfigured |
| `MONTHLY_OPERATING_COST_CENTS` | No | **Admin-only.** Monthly running cost in cents, used to suggest a fee |

`GET /api/v1/config` reports `stake_tiers_cents`, `payout_multiplier_bp`, `markets`, `push_enabled`, `vapid_public_key`, `push_daily_cap`, `quiet_hours`, and `access_fee_cents`. It never returns the DAU target, running cost, or suggested fee.

## Responsive & Accessibility Checks

The UI is verified against real measurements, not eyeballing:

- **Responsive audit** — `frontend/scripts/responsive-audit.html` loads every route inside same-origin iframes at fixed widths (320, 360, 768, 1024, 1280, 1440) and reports horizontal overflow, elements pushed past the viewport, tap targets under 44 px, text clipped by a fixed height, and buttons in a row with mismatched heights. Rows are compared **per parent element**, so buttons from different components at the same vertical offset are never treated as one row.
  - **How to run:** copy the file to `frontend/public/_responsive-audit.html`, open `http://localhost:4321/_responsive-audit.html`, set `localStorage.betva_token` to a user JWT for protected routes (and `localStorage.betva_admin_token` for admin routes), then call `window.__audit(routes, widths)` in the console. Audit at most **4 routes × 6 widths** per batch. Delete the temp file when done so it never ships.
  - **Last run:** 16 November 2026 — 14 routes × 6 widths = **84 checks, 0 failures** (no overflow, no sub-44px targets, no clipped text, no misaligned rows).
- **Dynamic overlays** — `window.__auditOverlay(route, width, selector)` opens the bet slip, report sheet, or settle modal and measures them the same way.
- **Lighthouse** — ⬜ not yet run on a production build. The earlier 1.0 claim was aspirational and has been removed until measured.

Rules the audit enforces: **no horizontal overflow at 320 px**, every control at least **44 × 44 px**, no clipped text, buttons in the same row aligned to the same height, and tables scroll horizontally inside their own container instead of crushing cells.

> **Astro scoping trap.** Styles declared in a page's `<style>` block are scoped with a `data-astro-*` attribute. Any markup injected through `innerHTML` does not carry that attribute, so scoped selectors never match it. Classes used by dynamically rendered markup (`.stake-chip`, `.admin-select`, `.admin-table-mid`, `.line-clamp-2`) live in `global.css` for that reason.

**Next:** launch sign-off — run `docs/PEN_TEST_CHECKLIST.md` against staging, put Cloudflare in front of Vercel and media, enable managed PITR if the workspace moves to Pro, measure real East-Africa latency on a mid-range Android over 3G, then go live.

**Decisions locked in:**
- Frontend hosting: **Vercel**
- Backend + data hosting: **Railway**
- Media, storage, and CDN edge: **Cloudflare**
- Edge routing: **Cloudflare proxies Vercel** for `www`; `api` on Railway bypasses Cloudflare entirely
- Roles: **Admin** (env-var credentials) and **User** (nickname, phone, official phone-owner name, password)
- Post-login navigation: **Home → Community → History → Profile**
- KYC: **official name of the phone owner** collected at registration, admin-only
- KYC mismatch: **7-day notice → appeal / phone change / name correction → admin review queue**, ban only if unresolved ⬜ (not built)
- Repeat KYC mismatch: **2nd = suspension + 48h review, 3rd = ban**; phone-only change is routine re-verification ⬜ (not built)
- Settlement: **official league source decides**, admin enters and approves, corrections are reversing entries. Two-source `DISPUTED` automation is ⬜
- Statuses: **WIN / LOSS / PENDING / LIVE / POSTPONED** — fixed colour tokens, text + icon on every badge
- Markets: **Full Time Over 0.5 Goals**, **Over 0.5 Half Time** only
- Trust counter: **aggregate wins/losses/rate/streak**, confidence badge per match, losses never hidden, updates at settlement, never resets. It is labelled “Betva track record” in the UI to distinguish it from History, which lists only the signed-in account's own slips.
- Visibility: **Home = today + tomorrow** (separate Today/Tomorrow buttons), **History = last 6 months**
- Design: **dark-only v1**, glass surfaces, system fonts, 44px tap targets, 12/16px radius, Google-blue brand, eagle-B mark
- Engagement: **mechanics approved**, guardrails enforced as release blockers (see Engagement Policy)
- Admin centres: **Overview + Matches (CSV), Users, Analytics, AI Results, Community, Notifications**; Results Archive page and Admin Profile are ⬜
- Analytics: **append-only `activity_days`**, one row per user per EAT day, admin-only DAU series and fee planner
- Access fee: **one flat monthly rate** (`ACCESS_FEE_CENTS`) shown to users; DAU target, running cost, and suggested fee are **admin-only**; shortfalls are covered by the admin
- User activation: **manual** (admin marks active after payment) until automated
- Public landing page: **planned** (live match ticker + activity feed + signup wall at the point of action) ⬜
- Notifications: **PWA Web Push** from admin, with in-app inbox authoritative and per-category mutes; SMS fallback is ⬜
- Results extraction: **ESPN JSON + 100Tip cross-check → approved suggestion → settlement** (no AI), plus an optional **Groq parses a server-fetched page → validator → admin approval** path; neither raw feed nor AI output settles bets without an approved suggestion
- Data: **daily off-site database dumps** run to the `betva-backups` bucket with verified manifests and GFS retention, and are mirrored to Cloudflare R2 (`betva-backups-r2`). Restore drills passed for both the staging copy and the off-site object. Managed PITR remains ⬜.
- Security: **HTTPS + Cloudflare WAF**, parameterized queries, bcrypt, admin 2FA ⬜, audit trail, pen test before launch ⬜
- Revenue: **access contribution** for running costs — explicitly **not** payment for predictions
