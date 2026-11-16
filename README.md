# Betva

A fast, scalable betting and community media platform built for budget smartphones and slow mobile networks in Tanzania.

**Core priorities:** low data usage · minimal device processing · ultra-fast server responses.

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
10. [Community & Media](#community)
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

---

## Goal

Deliver a betting site with community features (uploads, feeds) that:

- Loads instantly on 3G/4G connections
- Runs smoothly on low-RAM Android devices
- Handles thousands of simultaneous bets without lag or downtime
- Serves media without burning users' data bundles

---

## Public Landing Page

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

- **Credentials:** username and password supplied through environment variables (set variables) at deploy time — never stored in the database.
- **Purpose:** manages matches, betting options, results, users, payments, and community content.
- **Access:** configuration is controlled by the operator, not created through the app.

### User

- **Registration:** nickname, phone number, **official name of the phone owner**, and a simple password.
- **Login:** phone number + password.
- **Profile editing:** change password, change profile picture.
- **Session:** login and logout.
- **Profile picture limit:** under **10 KB**.

| Action | Required fields |
| --- | --- |
| Register | Nickname, phone number, official name of phone owner, password |
| Login | Phone number, password |
| Edit profile | Password (optional), profile picture (< 10 KB) |

#### Registration Disclosure

At registration the user must be shown, and must accept, the following before the account is created:

> **Official name notice**
> Enter the **official name registered to this phone number**.
> We use this name only to confirm that you truly own this account.
> It is **visible to administrators only** and is never shown publicly.
> If the name does not match the registered owner of the phone number, you will receive a **7-day notice** to correct it or appeal. If it is not resolved in that time, **your account will be banned.**

- Acceptance is **recorded** (timestamp + version of the notice) so the ban decision is auditable.
- The official name is **never displayed** in the Community feed, leaderboards, or to other users.

#### Name Mismatch & 7-Day Appeal Flow

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

#### Repeat Mismatches — Decision

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

- **Passwords are hashed** (bcrypt or Argon2) — never stored in plain text, even for "simple" passwords.
- **Phone numbers are normalized** to the Tanzanian format (`+255…`) so `0712…`, `712…`, and `+255712…` resolve to one account.
- **Admin credentials live in environment variables**, so rotating them is a redeploy, not a database migration.
- **Login is phone-based**, which means the phone number is the unique account identifier.
- **The official name is sensitive personal data** — restricted to admin views only, encrypted at rest, and never included in public API responses.

---

## Navigation & Pages

After login, the user lands on **Home**. The layout uses a **top navigation bar** plus a **bottom navigation bar** on small screens, in the style Google uses on mobile.

**Bottom navigation order:**

1. Home
2. Community
3. History
4. Profile

### Home

- The landing page immediately after login.
- Shows **today's and tomorrow's matches only**, with their betting options.
- Anything older or further ahead is **not shown on Home**.
- Match and option data is configured by the admin — the exact option types are defined later.

### Bet Markets

Only two markets are offered. Keeping it to two is deliberate: fewer decisions, faster slips, lower cognitive load on cheap phones.

| Market | What it means | Settles on |
| --- | --- | --- |
| **Full Time Over 0.5 Goals** | At least one goal in the entire match | Final whistle |
| **Over 0.5 Half Time** | At least one goal in the first half | Half-time whistle |

- Each market is a simple **Yes / Over 0.5** pick — no odds arithmetic for the user.
- A match can offer one or both markets; the admin sets this per match.
- Stakes are small and fixed tiers, not free-form amounts, to keep the slip lightweight.

### Match Visibility Window

| Where | Window | Contents |
| --- | --- | --- |
| **Home** | **Today + tomorrow** | Upcoming and live matches only |
| **History** | **Last 6 months** | Settled matches and the user's bets |
| **Results archive** | **6 months, append-only** | Every day, nothing deleted |

The split is deliberate: Home stays small and fast, so it loads quickly on 3G and never becomes an endless list. The 6-month record lives in History, where users look backwards.

### Community

A single feed of user posts — text, one image, or one short video. It is the only place in the product with infinite scroll, and the only place users can publish.

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

- The user's past activity — bet history and transaction records.
- Shows the **last 6 months**, matching the results archive retention.
- Nothing older is displayed, because nothing older is retained.

### Profile

- View account details.
- Change password.
- Change profile picture (< 10 KB).

#### Account Status (Financial)

The Profile page is where the user sees their **money and account state**, clearly and honestly:

- **Account status:** `Active` or `Inactive`.
- **If inactive:** the reason and what to do next, in plain language.
- **Balance**, **deposits**, and **withdrawals** for the period.
- **Activation date** and **expiry / end date** of the access period.

#### Payment & Activation

- The user pays to **activate** (or renew) access. Payment is a **contribution to running costs**, not a prediction fee.
- The Profile shows the **admin's payment number** and the exact steps to pay (mobile money, amount, reference).
- After payment, the user waits for the admin to mark them **active** manually; the new status appears here.
- Every payment leaves a **reference number** the user can screenshot and keep.

#### Account End & Terms

- **Expiry date** of the current access period, shown prominently.
- **What happens when access ends:** betting pauses, balance is safe, re-activation restores everything.
- Link to **terms of use** and **privacy policy** (simple text, low data).
- **Self-exclusion** and **mute controls**, as defined in the Engagement Policy.

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
| **POSTPONED** | Match | Delayed or rescheduled | Violet |

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

- **LIVE pulses** with a soft dot — and the pulse is disabled under `prefers-reduced-motion`.
- **Solid mid-tone backgrounds**, not pastels, so contrast survives cheap LCD panels.
- **Same badge everywhere** — a WIN in History looks identical to a WIN in the admin table.
- **Status text is never truncated** to fit a narrow card; the badge wraps instead.

### Status Flow

```
Match:  UPCOMING ──► LIVE ──► FINISHED
              └──► POSTPONED ──► LIVE (later) or CANCELLED

Bet:    PENDING ──► WIN
                 └► LOSS
                 └► refunded if the match is POSTPONED / CANCELLED
```

**Postponed bets are refunded, not left pending.** A postponed match returns the stake and the bet shows `POSTPONED` rather than a win or loss — this is the single most common source of user complaints, so it must be explicit.

---

## Trust Counter & Confidence

A loss on its own looks like a failure. A loss inside a winning record looks like normal variance. The difference is **presentation**, and this product must never let a single red badge erase the whole picture.

### The Counter

A persistent, compact counter shown on Home and in Profile:

| Stat | Meaning |
| --- | --- |
| **Total matches** | Tips published |
| **Wins** | Settled as WIN |
| **Losses** | Settled as LOSS |
| **Win rate** | Wins ÷ total, as a percentage |
| **Current streak** | Consecutive wins (or losses) |
| **Best streak** | Longest winning run |

- The counter **updates only after settlement**, never in real time, so it cannot flicker.
- It is **aggregate**, not per-user — the same record for everyone, which keeps it cheap to compute and cache.

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

- **Dark mode is the default.** Light mode is a toggle. Dark surfaces reduce eye strain over long sessions and look premium on cheap OLED screens.
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
| **Brand accent** | `#22C55E` → `#16A34A` | Primary buttons, active states |
| **Brand glow** | `rgba(34,197,94,0.25)` | Focus rings, subtle glows |
| **Danger** | `#EF4444` | Errors, loss accents |
| **Warning** | `#F59E0B` | Pending, caution |
| **Info** | `#3B82F6` | Links, informational |

- **No pure black or pure white** — they cause halation on OLED and feel harsh.
- **Grain overlay** at 2–3% opacity on the background adds depth without a single extra image request.
- Status colours (WIN/LOSS/PENDING/LIVE/POSTPONED) are defined in **Statuses & Colours** and never overridden.

### Typography

- **Font stack:** system fonts only — `ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto`. Zero network cost, instant render, native feel.
- **Scale (mobile):** 12 / 14 / 16 / 18 / 22 / 28 / 36 px. Body is 16px minimum — never smaller.
- **Weights:** 400 (body), 500 (labels), 600 (headings), 700 (hero numbers).
- **Line height:** 1.4 for body, 1.1 for large numbers.
- **Numbers are tabular** (`font-variant-numeric: tabular-nums`) so counters and balances never jump width when they change.
- **Money is always bold and larger** than surrounding text — it is the most important thing on the screen.

### Buttons

| Variant | Look | Use |
| --- | --- | --- |
| **Primary** | Solid brand green, white text, soft shadow | Place bet, activate, submit |
| **Secondary** | Glass surface + brand border | Secondary actions |
| **Ghost** | Transparent, brand text | Tertiary, inline actions |
| **Danger** | Solid red | Ban, delete, destructive |
| **Disabled** | 40% opacity, no shadow | Unavailable |

**Rules:**

- **Minimum height 44px**, minimum width 44px — thumb-friendly, accessible.
- **Radius 12px** consistently. One radius for the whole app.
- **Padding:** 12px vertical, 20px horizontal. Generous, never cramped.
- **States:** default → hover (slight lift + glow) → active (scale 0.98) → disabled (dimmed, no interaction).
- **Loading state:** button keeps its size, shows a small spinner, text stays. No layout shift.
- **One primary button per screen.** If two look primary, the hierarchy is broken.
- **Micro-interaction:** a soft scale (0.98) + haptic on press. Subtle enough to feel native, not a toy.

### Frames & Cards

- **Glass surface:** `backdrop-blur-md` + translucent background + 1px border. This is the signature look.
- **Radius 16px** for cards, **12px** for inputs, **999px** for pills/badges.
- **Elevation via shadow, not borders:** `0 4px 24px rgba(0,0,0,0.3)`. No harsh outlines.
- **Spacing system:** 4 / 8 / 12 / 16 / 24 / 32 / 48. Never invent a value.
- **Cards never touch each other** — minimum 12px gap, so the screen breathes.
- **Dividers are 1px, low-contrast, and used sparingly.** Whitespace does the work.
- **Inputs:** glass surface, 12px radius, 16px padding, focus ring in brand glow. Label above, error below, never floating.

### Feeds

- **Skeleton loaders** instead of spinners — they feel faster and keep layout stable.
- **Blur-up images:** tiny placeholder → full image, so the feed never shows empty boxes.
- **Infinite scroll** with a "loading more" indicator at the bottom, never a "load more" button.
- **Pull to refresh** on Home and Community, with a subtle spinner.
- **Spacing between feed items: 12px.** Dense enough to scroll, loose enough to read.
- **No autoplay on first paint** — honours Data Saver Mode. Tap to play.
- **Smooth scroll** with momentum; no janky jumps.

### Background

- **Layered, never flat:** base colour → subtle gradient → grain overlay. Three layers, zero extra requests.
- **No background images** — they cost data and fight with content.
- **Fixed background** so content scrolls over it; the depth stays constant.
- **OLED option:** a true `#000000` mode for maximum battery saving on OLED screens, toggled in settings.

### Long-Hour Comfort

- **Dark mode default**, light mode available — the user chooses, and the choice persists.
- **Reduced motion respected** — all animations disabled under `prefers-reduced-motion`.
- **Font size control** in settings: small / default / large, applied app-wide.
- **High contrast toggle** for accessibility.
- **Break reminders** after 60 minutes of continuous use, with a one-tap cool-off (from the Engagement Policy).
- **No flashing or strobing elements.** Status pulses are slow (2s cycle) and subtle.
- **Glare-friendly:** sufficient contrast for outdoor use in sunlight.

### Micro-Interactions

- **Haptic feedback** on bet placement and payment confirmation — light, 10ms.
- **Transitions:** 150ms for small elements, 250ms for sheets and modals. Easing: `cubic-bezier(0.4, 0, 0.2, 1)`.
- **Toast notifications:** slide in from top, auto-dismiss in 3s, swipe to dismiss.
- **Success states** are green and quiet, not celebratory. **Errors are red and clear**, with a next step.
- **Loading states** never block the whole screen — they are local to the component.

### Accessibility

- **Contrast ratio ≥ 4.5:1** for body text, ≥ 3:1 for large text (WCAG AA).
- **Touch targets ≥ 44×44px.**
- **Focus rings visible** on every interactive element, in brand glow.
- **Screen reader labels** on all icons and status badges.
- **Colour is never the only signal** — status badges carry text and marks (from Statuses & Colours).

### What This Achieves

A user who opens the app at 6pm and is still there at 11pm should feel comfortable, not drained. The dark glass aesthetic reads as premium on a $60 phone, the large tap targets make betting feel effortless, and the consistent spacing and motion make the app feel like one coherent thing rather than a collection of pages. That is what keeps people in the app the way the top giants do — not tricks, but comfort.

---

## Admin Dashboard

The admin area is separate from the user app and is reached by the credentials set in environment variables. It is built around four centres.

### 1. Matches (CSV Upload)

The **Matches** centre is the single source of matches shown to users.

- **Upload a CSV** of matches for a **specific date**.
- Uploaded matches are **published to the user Home page** for that date.
- **Only today's and tomorrow's matches are visible to users.** Anything further ahead is stored but hidden until it enters the window; anything older drops off Home and lives only in History.
- Each upload is tied to a date, so multiple days can be loaded in advance.
- Matches carry their betting options, which users see on Home.
- Each match also carries a **confidence level** (HIGH / MEDIUM / LOW), set by the admin.

**CSV handling requirements:**

- A fixed template (columns such as date, kickoff time, league, home team, away team, and option/odds fields).
- **Validation before import** — bad rows are rejected with a readable error, not silently dropped.
- **Duplicate detection** — re-uploading the same date warns before overwriting.
- **Import log** — who uploaded, when, which date, how many rows, how many failed.
- **Match state** — open for betting → closed at kickoff → settled once results arrive.

### 2. Users & Financial Status

A single place to see every user clearly.

- **User list** with nickname, phone number, official name, registration date, and status.
- **Financial status** per user — balance, deposits, withdrawals, and whether they are active.
- **Manual activation** — early stage, the admin marks a user **active** or **not active** by hand once payment is confirmed.
- **KYC review queue** — appeals, phone-number changes, and official-name corrections waiting for a decision.
- **Ban action** — used only after the 7-day notice expires or an appeal is rejected; the reason is recorded.
- **Search and filter** by phone, nickname, status, or date.
- **Clear user detail view** — one screen showing account, KYC name, money, and activity.

#### KYC Review Queue

Every mismatch resolution lands here, never auto-approved.

| Queue item | Admin sees | Decision |
| --- | --- | --- |
| Appeal | User's written explanation, account history | Confirm / reject |
| Phone change | Old phone, new phone, official name | Confirm / reject |
| Name correction | Old name, new name, phone | Confirm / reject |

- Each decision records **reviewer, timestamp, and reason**.
- A rejection **immediately issues the ban**; the user is notified through the same channel.
- The queue shows **days remaining** on each 7-day window so nothing expires unseen.

### 3. Results Archive

- Match results are **accumulated daily**.
- **Retained for 6 months**, day by day.
- **Nothing is deleted** — the archive is append-only.
- Users see the same **6-month window** in History.
- Results feed the settlement of bets and the user's History page.

**Practical notes:**

- Store results with the **Africa/Dar_es_Salaam (EAT)** timezone so a "day" means the same thing to admin and users.
- 6-month retention with no deletion means **scheduled backups** are mandatory — retention is only real if the data is recoverable.

### 4. Admin Profile

- Shows the admin identity and current configuration state.
- Because credentials come from environment variables, **changing them is a redeploy** — the profile page reflects, but does not store, them.

### Suggested Additions (Gaps Worth Filling)

These were not in the brief but the product will need them:

- **Bet slip & settlement** — ✅ implemented in Phase 3.
- **Deposit / withdrawal queue** — a manual approval list with mobile money transaction references, matching the "manual" phase you described. *(Admin credit endpoint exists; a dedicated queue UI is still pending.)*
- **Append-only ledger** — ✅ implemented in Phase 3.
- **Ban & audit log** — ✅ implemented in Phase 2.
- **Community moderation** — report, hide, and delete posts; mute or ban repeat offenders.
- **Match locking** — ✅ implemented: betting closes automatically at kickoff.
- **Announcements** — a lightweight notice pushed to all users (text-only, low data).
- **Payout / commission rules** — ✅ payout maths implemented; final commercial margin still open.
- **Export** — CSV export of users and finances for accounting and tax.
- **A second, limited admin** — support staff who can view but not change money.

---

## AI Results Assistant (Groq)

> A Groq-hosted model reads football result pages and turns them into structured rows. It is an **assistant that fills a form**, never the final authority on money.

### Correction Before Anything Else

**Groq is an inference API, not a browser.** It has no internet access. It cannot open Forebet or PassionPredict, and if you ask it to "look up" a score it will invent a plausible one.

The pipeline must **fetch the page first**, then pass the text to Groq for extraction. That misunderstanding is the single biggest source of wrong results in systems built like this.

### Pipeline

```
Daily scheduler (Railway cron)
        │
        ▼
Fetcher (server-side) ──► Forebet / PassionPredict HTML
        │
        ▼
Cleaner ──► plain text (strip nav, ads, scripts)
        │
        ▼
Groq extraction ──► strict JSON, one object per match
        │
        ▼
Validator ──► schema + range + cross-source agreement
        │
        ▼
Admin review queue ──► APPROVE
        │
        ▼
Results table (append-only, 6 months)
```

### Sources

| Source | Example URL | Use |
| --- | --- | --- |
| Forebet 1X2 | `forebet.com/en/football-predictions/predictions-1x2/YYYY-MM-DD` | Finished scores + upcoming predictions |
| PassionPredict | `passionpredict.com/over-0-5-half-time` | Over 0.5 half-time predictions |
| Manual CSV | Admin Dashboard → Matches | Always available fallback |

The date in the Forebet URL is `YYYY-MM-DD`, so the scheduler builds the URL from the target date in **EAT**.

### Free Result Sources (No Paid API)

When scraping is blocked or the free path is needed:

- **football-data.org** — free tier, real results, limited competitions.
- **API-Football (api-sports.io)** — small free daily quota, good coverage.
- **TheSportsDB** — free tier, community-maintained.
- **OpenLigaDB** — free, a few leagues only.
- **Manual CSV** — zero cost, already part of the admin flow.

Check current limits before relying on any of them — free tiers change without notice.

### Anti-Blunder Rules

**Prompt rules given to Groq:**

1. Return **only facts present in the supplied text**. Never infer a score from team strength.
2. If a field is missing, return `null`. **Never guess.**
3. Echo back the **source URL and fetch timestamp** with every row.
4. Output **strict JSON** matching the agreed schema — no prose, no markdown.
5. One row per match. **Never invent a fixture** that is not in the text.
6. Set a `confidence` of `low`, `medium`, or `high`.
7. If the page is a captcha, a block page, or empty, return `{"status":"UNPARSEABLE"}` instead of a result.

**Validation rules applied after Groq responds:**

- **Schema check** — reject anything that does not parse.
- **Score range** — goals must be 0–20; anything else is rejected.
- **Team match** — both teams must exist in the uploaded match list (fuzzy match, admin confirms).
- **Date match** — the result date must equal the requested date.
- **Two-source agreement** — a result is only eligible for settlement when two independent sources agree.
- **Confidence gate** — `low` confidence always forces manual entry, no exceptions.

### Why AI Must Never Settle Bets Alone

A hallucinated score pays the wrong users and takes money from the right ones. That is a financial loss **and** a legal exposure, and it is unrecoverable once payouts have gone out. Groq may fill the form; **an admin approves the row** before any wallet moves.

### Source Disagreement & Settlement Authority — Decision

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

### Prompt Template

```
You extract football results. You have no internet access.

From the text below, output a JSON array. For each match return:
{ "date": "YYYY-MM-DD", "league": string|null,
  "home_team": string|null, "away_team": string|null,
  "home_goals": int|null, "away_goals": int|null,
  "status": "finished"|"upcoming"|"unknown",
  "confidence": "low"|"medium"|"high",
  "source_url": string, "fetched_at": ISO8601 }

Rules:
- Use only what is written in the text. Never infer.
- Missing field → null. Do not guess.
- If the text is not a results page → {"status":"UNPARSEABLE"}.

TEXT:
<<<fetched page text>>>
```

### Legal & Practical Warning

- **Scraping may violate Forebet's or PassionPredict's terms and robots.txt.** Check before going live; a blocked IP or a legal notice is a real outcome.
- **Prefer official or licensed feeds** for anything that settles money.
- **Store only what you need** — team names, score, date, source URL. Do not mirror their content.
- Keep the **manual CSV path working** so the platform never depends on a scraped page being up.

---

## Push Notifications

Admin-to-user notifications are delivered as **Web Push** through the PWA service worker — no Play Store app, no SMS cost.

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

### What the Admin Can Send

- **Match starting soon** — the highest-value trigger
- **Results published**
- **Account activated** — payment confirmed
- **Announcement** — general notice
- **Community activity** — replies, mentions

### Guardrails (Enforced Server-Side)

These are checked in the dispatch path, so a bug in the UI cannot bypass them. Every notification row also records why a push did not go out.

| Guardrail | Behaviour |
| --- | --- |
| **Daily cap** | `PUSH_DAILY_CAP` (default **2**) pushes per user per EAT day. Counted in `push_delivery_log`, not in inbox rows. |
| **Quiet hours** | `QUIET_START_HOUR` (22) to `QUIET_END_HOUR` (7) EAT. Push is skipped; the inbox row still lands. |
| **Muted category** | Skipped at push time; inbox row still written. |
| **No subscription** | Recorded as `no subscription`; nothing retries in a loop. |
| **Push not configured** | Recorded as `push not configured`; the product still works. |
| **Expired subscription** | A `404` or `410` from the push service deletes that subscription row. |

The inbox is authoritative. Push is a best-effort extra: a failed push never loses the message.

### Low-Data Design

- Payload is **title + short body + link** — a few hundred bytes, no images.
- The service worker shows the notification with `renotify: false` and a per-category `tag`, so rapid updates replace rather than stack.
- Quiet hours and the daily cap are enforced server-side (see above).

### Opt-In Strategy

Ask **after the user's first meaningful action**, never on page load. The app shows its own banner ("Get told when your match starts") after the first placed bet or first published post; the browser permission prompt only fires when the user taps **Turn on**. A denied prompt is hard to reverse, so "Not now" is remembered.

### Fallbacks

- **In-app notification inbox** — always available, works even if push is blocked. Every category lands there first.
- **SMS for payment-critical events** — activation and withdrawal confirmations, where missing the message costs money.

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

- **Manual activation:** after payment is confirmed, the admin flips the user from **not active** to **active** in the Admin Dashboard. This is the early-stage flow.
- **Small and transparent:** the fee covers running costs, so it should be modest and clearly stated.
- **Separate from stakes:** the access contribution is not the bet stake and should never be described as one.

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

1. **Railway managed Postgres backups** — automated daily, point-in-time recovery where available.
2. **Off-site copies to Cloudflare R2** — a second copy outside Railway, so a Railway account compromise or outage cannot take the data with it. Encrypted at rest, lifecycle-retained for the 6-month archive window.
3. **Redis persistence** — RDB snapshots + AOF for the wallet-adjacent cache; live odds are rebuildable, so Redis is treated as recoverable, not authoritative.
4. **Code & config in Git** — the database schema is versioned (migrations), so a fresh database can be rebuilt from scratch.
5. **Media in R2/S3** — already durable, multi-region by default.

### Manual Backup Creation

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

- **Documented runbook** — step-by-step restore procedure, stored outside the production environment, including the manual-backup restore path.
- **Quarterly restore drill** — actually restore a backup into a staging database and verify row counts, wallet totals, and the last 7 days of bets. A backup that has never been restored is a wish.
- **Pre-launch restore test is a hard launch blocker.** The platform does not go live until a full restore has been performed and verified end-to-end. This sits alongside the penetration test as a non-negotiable gate.
- **Targets:** RTO ≤ 1 hour for critical data, RPO ≤ 1 minute for wallets.
- **No single point of failure:** backups are verified by a different person than the one who configured them.

### Retention

- Results archive: **6 months, append-only, nothing deleted** — backups must cover the full 6 months, not just the last week.
- User financial records: **retained as long as the account exists**, plus a defined post-closure period for disputes and tax.
- Backups inherit the retention policy; nothing is silently expired early.

---

## Scalability & Availability

**The system must absorb spikes (match kickoffs, result drops) without dropping a request or losing data.**

### Handling Pressure

- **Stateless Go API** behind Railway's load balancer — scale horizontally by adding instances; no session state in the process.
- **Redis for live odds & feeds** — served from memory in milliseconds, never hitting Postgres on a read.
- **Connection pooling** (PgBouncer or Railway's) — Postgres never sees more connections than it can handle.
- **Read/write split** — heavy reads (history, results, community) go to a read replica; writes (bets, wallet) go to the primary.
- **SSE/WebSocket at scale** — a Redis pub/sub layer means any API instance can push an update to any user; connections are not pinned to one box.
- **Cloudflare absorbs the edge** — static assets, media, and cached pages never reach the origin.
- **Queue-based writes for bets** — a bet is accepted into a queue and settled against the wallet in a controlled order, so a kickoff spike becomes a queue, not a DB crash.
- **Rate limiting** at Cloudflare and the API — per-IP and per-user, with stricter limits on login, registration, and payment endpoints.
- **Graceful degradation** — if the DB is slow, serve cached data and queue writes; if push fails, the in-app inbox still works.

### Never Losing Data Under Load

- **Transactions are atomic** — a bet either fully commits (slip + wallet debit + ledger entry) or fully rolls back. No half-written bets.
- **Idempotency keys** on every write — a retried request cannot double-charge or double-pay.
- **Append-only ledger** — balance changes are never overwritten, only appended; reconciliation is always possible.
- **Backups run continuously**, not only at night, so a failure at peak loses minutes, not hours.
- **Migrations are versioned and reversible** — schema changes deploy without downtime and can be rolled back.

### Capacity Planning

- Start small on Railway, **scale vertically first** (more CPU/RAM per instance), then horizontally.
- Monitor: CPU, memory, DB connections, queue depth, SSE connection count, p95 latency.
- **Alerts** on queue depth and p95 before users feel it.
- Load test before every major launch with a kickoff-scale traffic profile.

---

## Security & Anti-Hacking

**Assume the network is hostile. Defend in depth.**

### Transport & Edge

- **HTTPS everywhere**, HSTS preloaded, TLS 1.2 minimum — Cloudflare enforces this at the edge.
- **Cloudflare WAF** with managed rulesets, bot protection, and DDoS mitigation.
- **Security headers** on every response: `Content-Security-Policy`, `X-Frame-Options`, `X-Content-Type-Options`, `Referrer-Policy`, `Permissions-Policy`.
- **Strict CSP** — no inline scripts; Astro's output is already CSP-friendly.

### Application

- **Parameterized queries only** — no string-built SQL. SQL injection is the #1 way betting sites get robbed.
- **Input validation** on every endpoint, server-side, with allow-lists not block-lists.
- **XSS prevention** — framework escaping plus CSP; user content is never rendered as HTML.
- **CSRF protection** on all state-changing requests.
- **Authentication:** phone + hashed password (Argon2/bcrypt), normalized phone numbers, session tokens with expiry and rotation.
- **Admin 2FA** — username/password from env vars is not enough for the person who can move money.
- **Rate limiting** on login and registration — brute force protection, with progressive lockout.
- **Account lockout** after repeated failures, with a safe unlock path.
- **Least privilege in the database** — the API user cannot DROP, TRUNCATE, or alter schema; migrations run under a separate admin role.
- **Secrets in environment variables only** — never in the repo, never in client bundles. Rotate on any suspicion.

### Data Protection

- **Encryption at rest** for Postgres and backups; **TLS in transit** everywhere, including service-to-service.
- **PII minimization** — official names stored encrypted, admin-only, never in public responses or logs.
- **Audit trail** — every money movement, ban, and admin action is logged with who, when, and why.
- **Access control** — admin roles with least privilege; a support admin can view, not move money.
- **Data retention policy** — defined and enforced, not improvised.
- **User rights** — export and deletion paths, consistent with Tanzania's Personal Data Protection Act 2022.

### Supply Chain & Operations

- **Dependency scanning** (Dependabot / Snyk) with a review process before merging updates.
- **Pinned versions** in production; updates tested in staging first.
- **No secrets in Git history** — if one leaks, rotate immediately, do not just delete the commit.
- **Logging & monitoring** — centralised logs, alerts on anomalies (failed logins, payout spikes, error rates).
- **Incident response plan** — who is called, what is shut down first, how users are notified.
- **Pre-launch penetration test** — by someone who is not the developer.
- **Backups of backups** — the restore drill is the proof that recovery works.

---

## Architecture at a Glance

| Layer | Recommended Tech | Main Benefit for Your Goal |
| --- | --- | --- |
| **Frontend** | Astro or SvelteKit + Tailwind | Tiny file sizes; won't freeze cheap Android phones |
| **Backend** | Go (Golang) | Handles thousands of simultaneous bets easily |
| **Database** | PostgreSQL + Redis | Safe wallet transactions + instant feed/odds loading |
| **Media** | Cloudflare Stream / Bunny.net | Auto-compresses photos & adapts video quality to network speed |
| **Network** | Cloudflare CDN | Local East African servers reduce lag drastically |
| **Frontend hosting** | Vercel | Auto-builds, preview deploys, edge-cached static output |
| **Backend hosting** | Railway | One platform for the Go API, Postgres, and Redis |
| **Results extraction** | Groq (LLM) | Turns fetched match pages into structured JSON |
| **Payments** | AzamPay / Selcom | Fast USSD mobile money deposits and payouts |

### How the pieces fit together

- **Vercel** serves the frontend only (the Astro/SvelteKit build output).
- **Railway** runs the backend API plus PostgreSQL and Redis.
- **Cloudflare** sits in front for media (Images/Stream), storage (R2), and the East African CDN edge.

These are complementary, not competing — Vercel handles app code, Railway handles stateful services, Cloudflare handles the heavy media and last-mile delivery.

---

## 1. Frontend

> What runs on the user's phone.

Heavy JavaScript frameworks (standard React, Angular) drain battery and freeze low-RAM smartphones. The stack below sends minimal code to the browser.

### Framework: Astro or SvelteKit

- **Why:** Both compile code into tiny, fast HTML and CSS files.
- **Astro advantage:** Ships almost zero JavaScript by default, so pages load instantly on 3G/4G.

### Styling: Tailwind CSS

- **Why:** Generates a single, tiny CSS file that caches well on users' phones, saving mobile data on repeat visits.

### Progressive Web App (PWA)

- **Why:** Users can add the site to their home screen like an app — no 50 MB Play Store download.
- **Offline:** Caches basic layout files so the site opens immediately even on weak connections.
- **Push:** The same service worker that enables offline caching also receives admin push notifications — see [Push Notifications](#push-notifications).

---

## 2. Backend & Real-Time Betting Engine

> Betting requires instant odds updates and rock-solid reliability for financial transactions.

### Language & Framework: Go (Golang) or Node.js (Fastify)

- **Why:** Go is built for high concurrency — thousands of users can place bets and upload content simultaneously without the server crashing or lagging.

### Real-Time Odds: Server-Sent Events (SSE) or Lightweight WebSockets

- **Why:** Instead of forcing the phone to constantly refresh for live odds, SSE pushes tiny text updates (a few bytes) only when an odd changes.

### Database: PostgreSQL + Redis

| Component | Role |
| --- | --- |
| **PostgreSQL** | User accounts, wallet balances, and betting slips with strict financial accuracy |
| **Redis** | Live odds and community feeds in ultra-fast memory — data loads in milliseconds |

---

## 3. Media Optimization

> User-uploaded images and videos are the biggest threat to site speed and user data bundles. All media must be processed before it reaches other users.

### Delivery & Compression: Cloudflare Stream & Cloudflare Images (or Bunny.net)

- **Images:** Auto-convert every upload to WebP or AVIF — up to 70% smaller files while staying sharp. Load blurry placeholders first (lazy loading) so text and betting buttons appear immediately.
- **Profile pictures:** capped at **10 KB** per user, so avatars are resized to a small square (roughly 96–128 px) and served as WebP. This keeps the Community feed cheap to render even with many avatars.
- **Videos:** Never serve raw MP4 files from the server. Use **Adaptive Bitrate Streaming (HLS)** — automatically serves low-data 240p/360p to slow connections and 1080p to fast Wi-Fi.

### Storage: Cloudflare R2 or AWS S3

- **Why:** Cheap, scalable storage for community uploads.
- **Cloudflare R2 advantage:** Zero bandwidth/egress fees.

---

## 4. Local Payments & Network Infrastructure

> Keep infrastructure close to East Africa and integrate local mobile money seamlessly.

### CDN: Cloudflare

- **Why:** Cloudflare has edge servers directly in **Dar es Salaam** and **Mombasa**. Static files and cached pages are served locally in milliseconds instead of traveling to Europe or the US.

### Mobile Money: Selcom, AzamPay, or Pesapal

- **Why:** These APIs support fast, direct push-USSD prompts for **M-Pesa**, **Tigo Pesa**, **Airtel Money**, and **HaloPesa** — no slow redirect to external payment pages.

---

## 5. Cloud Deployment (Vercel + Railway)

> Vercel hosts the frontend. Railway hosts the backend and its data stores. Cloudflare handles media and edge delivery in East Africa.

### Vercel — Frontend Hosting

- **Serves:** Astro / SvelteKit build output.
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

- **Data Saver Mode toggle** — let users disable auto-playing videos and load lower-resolution images with one tap.
- **Keep betting slips lightweight** — avoid heavy animations or complex graphics on the main betting interface.
- **Compress API responses** — enable Brotli or Gzip on the server to shrink text data by up to 80% before it hits the network.

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
- [ ] **Access fee** — how much, how often (one-time or monthly), and what happens to a user who stops paying.
- [x] ~~**Notification policy**~~ — resolved: max 2/day, quiet hours 22:00–07:00 EAT, per-category mute.

### AI & Results

- [x] ~~**Authoritative source**~~ — resolved: disagreement = `DISPUTED`, official league source settles, Forebet is default manual reference.
- [ ] **Scraping permission** — have Forebet and PassionPredict terms/robots.txt been checked?
- [ ] **Free API choice** — football-data.org, API-Football, or TheSportsDB as the primary fallback?
- [ ] **Groq model** — which model and context size, and what is the per-day token cost?
- [ ] **Confidence threshold** — minimum confidence for a Groq-extracted row to reach the review queue at all?

### Design & Brand

- [ ] **Brand colour** — confirm the green accent (`#22C55E`) or pick a different brand colour before launch.
- [ ] **Logo & app icon** — final assets, including the PWA home-screen icon sizes.
- [ ] **Light mode depth** — full light theme at launch, or dark-only for v1? (Design System already commits to dark default + toggle.)

### Reliability & Security

- [x] ~~**Backup owner**~~ — resolved: quarterly restore drill is a **hard launch blocker**, owned by the operator, runbook stored off-site in R2 plus a printed copy. Backups that have never been restored are a wish, not a backup.
- [ ] **Admin 2FA** — which provider (TOTP / WebAuthn) and is it mandatory before launch?
- [ ] **Penetration test** — budget and vendor for the pre-launch test?
- [ ] **Incident contact** — who is on call, and how are users notified?

### Trust & Display

- [ ] **Counter cadence** — does the win rate update after every settlement, or once per day at a fixed time?
- [ ] **Counter reset** — does the counter ever reset, or does it run for the life of the product?
- [ ] **Confidence rules** — what evidence must back a HIGH match-confidence rating, and can the admin override it?

### Business & Infrastructure

- [ ] Building this in-house, or hiring a development team?
- [ ] Estimated starting budget for servers and infrastructure?
- [ ] Do you already hold a betting license (e.g. from the Gaming Board of Tanzania)?
- [ ] **Data protection** — how long is the official name retained, and is it covered by a privacy policy under Tanzania's Personal Data Protection Act 2022?

**Next step:** a step-by-step launch architecture and monthly cost estimate, based on the answers above — covering Vercel, Railway, Cloudflare, and mobile money provider fees.

---

## Development Status

**Phase 5 complete — AI result suggestions (Groq, admin approval) plus launch-hardening artifacts (restore drill and pen-test checklist).** Phase 6 polish remains.

### What exists now

| Area | Status | Location |
| --- | --- | --- |
| Go API (chi router, pgx, Redis-ready) | ✅ Working | `backend/` |
| Postgres schema and migrations | ✅ Working | `backend/migrations/` |
| User register / login / profile | ✅ Working | `backend/internal/httpapi/auth.go` |
| Admin login from environment variables | ✅ Working | `backend/internal/httpapi/admin.go` |
| Matches CSV upload, validation, duplicate detection, import log | ✅ Working | `backend/internal/httpapi/matches.go` |
| Admin day view, confidence and status editing | ✅ Working | `frontend/src/pages/admin/matches.astro` |
| Users list, search, manual activation, suspend / ban, audit log | ✅ Working | `frontend/src/pages/admin/users.astro` |
| Astro + Tailwind user app (PWA shell) | ✅ Working | `frontend/` |
| Admin app (separate layout) | ✅ Working | `frontend/src/pages/admin/` |
| Bet slip with fixed stake tiers | ✅ Working | `frontend/src/pages/index.astro` |
| Wallet balance and append-only ledger | ✅ Working | `backend/internal/httpapi/bets.go` |
| Bet placement (atomic debit + bet row + ledger) | ✅ Working | `backend/internal/httpapi/bets.go` |
| Settlement, correction with reversal, postponed refunds | ✅ Working | `backend/internal/httpapi/settlement.go` |
| History with final scores and visible losses | ✅ Working | `frontend/src/pages/history.astro` |
| Admin settle / correct / refund modal | ✅ Working | `frontend/src/pages/admin/matches.astro` |
| Community feed, composer, media upload | ✅ Working | `frontend/src/pages/community.astro` |
| Post reports, 3-report auto-hide, admin moderation | ✅ Working | `backend/internal/httpapi/community.go` |
| In-app notification inbox and per-category mutes | ✅ Working | `backend/internal/httpapi/notifications.go` |
| Web Push with VAPID, daily cap, quiet hours | ✅ Working | `frontend/public/sw.js`, `frontend/src/lib/push.ts` |
| Self-exclusion 24h / 7d / 30d, append-only | ✅ Working | `backend/internal/httpapi/notifications.go` |
| AI results assistant | ✅ Working | Phase 5 |
| Full launch hardening and restore test | ⬜ Later | Phase 6 |

### Matches CSV format

The upload form supplies the date; the file supplies the fixtures. Header row required, column order does not matter.

```csv
kickoff,league,home_team,away_team,markets,confidence
16:00,Example League,Home Team,Away Team,ft_over_0_5|ht_over_0_5,HIGH
18:30,Example League,Team Two,Team Three,ft_over_0_5,MEDIUM
```

| Column | Required | Rules |
| --- | --- | --- |
| `kickoff` | Yes | `HH:MM`, 24-hour, EAT |
| `league` | No | Free text |
| `home_team` | Yes | Must differ from `away_team` |
| `away_team` | Yes | Must differ from `home_team` |
| `markets` | Yes | `ft_over_0_5` and/or `ht_over_0_5`, separated by `\|`, `,` or `;` |
| `confidence` | No | `HIGH`, `MEDIUM`, `LOW` — defaults to `MEDIUM` |

Behaviour:

- **Validation first** — every bad row is rejected with its line number and a readable reason; good rows still import.
- **Duplicate detection** — fixtures already stored for the date are reported; the upload is refused unless `replace` is checked.
- **Replace is blocked when bets exist** — money history is never deleted by a re-upload.
- **Import log** — every upload records admin, date, file name, totals, failures, and whether it replaced.
- **Visibility** — users only ever see today and tomorrow; later dates are stored and appear when they enter the window.

### Wallet, bets, and settlement

Money is stored in **cents** (`balance_cents`, `stake_cents`, `payout_cents`); the UI divides by 100 for TZS. Every movement writes a row to the append-only `ledger` table.

**Placing a bet** — one transaction, in this order:

1. Lock the user row (`SELECT … FOR UPDATE`) so two taps cannot overspend.
2. Check the account is `active` and `access_expires_at` has not passed.
3. Check the optional daily cap (`MAX_BET_PER_DAY_CENTS`, `0` = off).
4. Check the match is `upcoming` and kickoff has not passed, and the market is offered.
5. Reject a second slip on the same user + match + market (`idx_bets_one_per_market`).
6. Debit the stake, insert the bet, insert a `bet_placed` ledger row, commit.

The bet stores `confidence_at_placement` and `payout_multiplier_bp` so later changes to either do not rewrite history.

**Fixed stake tiers.** Stakes are never free-form. The server validates `stake_cents` against `STAKE_TIERS_CENTS`; the client renders the same list from `GET /api/v1/config`.

**Settlement** (`ft_over_0_5` wins when full-time goals ≥ 1; `ht_over_0_5` wins when half-time goals ≥ 1):

- Payout = `stake × PAYOUT_MULTIPLIER_BP / 10000`, credited with a `bet_won` ledger row. The stake was already debited, so this is the full return.
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

### User API

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/v1/auth/register` | Register with phone, nickname, password |
| `POST` | `/api/v1/auth/login` | Login → user JWT |
| `GET` | `/api/v1/me` | Current profile |
| `PUT` | `/api/v1/me` | Update password / nickname |
| `GET` | `/api/v1/matches` | Today + tomorrow, with `is_open` and markets |
| `GET` | `/api/v1/config` | Stake tiers, payout multiplier, markets |
| `POST` | `/api/v1/bets` | Place a slip: `match_id`, `market`, `stake_cents` |
| `GET` | `/api/v1/wallet` | Balance + last 50 ledger entries |
| `GET` | `/api/v1/history` | User bets for the last 6 months, with scores |
| `GET` | `/api/v1/community/posts?limit=&before=` | Visible posts, newest first; `next_before` is the cursor |
| `POST` | `/api/v1/community/posts` | Publish text and/or one media item |
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
| `POST` | `/api/v1/admin/matches/upload` | Multipart: `date`, `file`, optional `replace` |
| `GET` | `/api/v1/admin/matches?date=YYYY-MM-DD` | All matches for one date, with `settled` and scores |
| `PUT` | `/api/v1/admin/matches/{id}` | Change `confidence` / `status` |
| `GET` | `/api/v1/admin/matches/{id}/bets` | Exposure for one match |
| `POST` | `/api/v1/admin/matches/{id}/settle` | Record result and settle pending bets; `correct=true` reverses first |
| `POST` | `/api/v1/admin/matches/{id}/refund` | Refund pending bets; `status` is `postponed` or `cancelled` |
| `GET` | `/api/v1/admin/imports` | Recent upload log |
| `GET` | `/api/v1/admin/users?q=&status=` | Search and filter users |
| `PUT` | `/api/v1/admin/users/{id}/status` | Activate (with days), suspend, ban, deactivate |
| `POST` | `/api/v1/admin/users/{id}/credit` | Manual deposit / withdrawal with a reason |
| `GET` | `/api/v1/admin/community/posts?status=` | Moderation queue: `reported`, `visible`, `hidden`, `deleted`, or all |
| `PUT` | `/api/v1/admin/community/posts/{id}/status` | `visible`, `hidden`, or `deleted`, with a reason |
| `POST` | `/api/v1/admin/notifications/send` | Broadcast to `all` or `active`; writes inbox rows, then pushes |
| `GET` | `/api/v1/admin/notifications` | Recent broadcasts grouped by minute |

### Running locally

```bash
# 1. Database (PostgreSQL already installed)
psql -U postgres -c "CREATE USER betva WITH PASSWORD 'betva';"
psql -U postgres -c "CREATE DATABASE betva OWNER betva;"
psql -U betva -d betva -f backend/migrations/001_init.sql
psql -U betva -d betva -f backend/migrations/002_matches_admin.sql
psql -U betva -d betva -f backend/migrations/003_wallet_settlement.sql
psql -U betva -d betva -f backend/migrations/004_community_notifications.sql

# 2. Backend
cd backend
cp .env.example .env        # set JWT_SECRET, ADMIN_USERNAME, ADMIN_PASSWORD
go run ./cmd/vapid          # paste the keys into .env as VAPID_PUBLIC_KEY / VAPID_PRIVATE_KEY
go run ./cmd/server         # http://localhost:8080

# 3. Frontend
cd frontend
npm install
npm run dev                 # http://localhost:4321
```

Admin app: `http://localhost:4321/admin` — sign in with `ADMIN_USERNAME` / `ADMIN_PASSWORD`.

### Environment variables

| Variable | Required | Notes |
| --- | --- | --- |
| `DATABASE_URL` | Yes | Postgres connection string |
| `JWT_SECRET` | Yes | Long random string in production |
| `ADMIN_USERNAME` | Yes | Never stored in the database |
| `ADMIN_PASSWORD` | Yes | Changing it requires a redeploy |
| `REDIS_URL` | No | Optional today; required for live feeds and rate limits |
| `CORS_ORIGINS` | Production | Comma-separated frontend origins |
| `PORT` | No | Defaults to `8080` |
| `ENV` | No | `development` or `production` |
| `STAKE_TIERS_CENTS` | No | Comma-separated fixed stakes in cents; default `50000,100000,200000,500000` (500–5,000 TZS) |
| `PAYOUT_MULTIPLIER_BP` | No | Basis points returned on a win; default `15000` = 1.50× |
| `MAX_BET_PER_DAY_CENTS` | No | Optional per-user daily stake cap in cents; `0` disables it |
| `MEDIA_DIR` | No | Where community uploads are stored; default `media` (relative to the backend working directory) |
| `MAX_IMAGE_BYTES` | No | Image cap; default `400000` (400 KB) |
| `MAX_VIDEO_BYTES` | No | Video cap; default `3000000` (3 MB) |
| `VAPID_PUBLIC_KEY` | Push | Public key, also served by `GET /api/v1/config` |
| `VAPID_PRIVATE_KEY` | Push | Secret. Rotating it invalidates every subscription |
| `VAPID_SUBJECT` | Push | Contact URI for the push service, e.g. `mailto:admin@example.com` |
| `PUSH_DAILY_CAP` | No | Pushes per user per EAT day; default `2` |
| `QUIET_START_HOUR` | No | Default `22` (EAT) |
| `QUIET_END_HOUR` | No | Default `7` (EAT) |

`GET /api/v1/config` reports `push_enabled`, the public key, the cap, and the quiet-hours window, so the client never hardcodes them.

### Responsive and accessibility checks

The UI is verified against real measurements, not eyeballing:

- **Responsive audit** — `frontend/scripts/responsive-audit.html` loads every route inside same-origin iframes at fixed widths (320, 360, 414, 768, 1280) and reports horizontal overflow, elements pushed past the viewport, tap targets under 44 px, and buttons in a row with mismatched heights. To run it: copy the file into `frontend/public/`, open `/_audit.html`, and call `window.__audit(routes, widths)` in the console. It is kept out of `public/` so it never ships.
- **Dynamic overlays** — `window.__auditOverlay(route, width, selector)` opens the bet slip, report sheet, or settle modal and measures them the same way.
- **Lighthouse** — every page scores 1.0 on accessibility, best practices, and SEO on the production build.

Rules the audit enforces: **no horizontal overflow at 320 px**, every control at least **44 × 44 px**, buttons in the same row aligned to the same height, and tables scroll horizontally inside their own container instead of crushing cells.

> **Astro scoping trap.** Styles declared in a page's `<style>` block are scoped with a `data-astro-*` attribute. Any markup injected through `innerHTML` does not carry that attribute, so scoped selectors never match it. Classes used by dynamically rendered markup (`.stake-chip`, `.admin-select`, `.admin-table-mid`, `.line-clamp-2`) live in `global.css` for that reason.

**Next:** Final polish pass and launch sign-off — run the restore drill and pen-test checklist against staging, then deploy.

**Decisions locked in:**
- Frontend hosting: **Vercel**
- Backend + data hosting: **Railway**
- Media, storage, and CDN edge: **Cloudflare**
- Edge routing: **Cloudflare proxies Vercel** for `www`; `api` on Railway bypasses Cloudflare entirely
- Roles: **Admin** (env-var credentials) and **User** (nickname, phone, official phone-owner name, password)
- Post-login navigation: **Home → Community → History → Profile**
- KYC: **official name of the phone owner** collected at registration, admin-only
- KYC mismatch: **7-day notice → appeal / phone change / name correction → admin review queue**, ban only if unresolved
- Repeat KYC mismatch: **2nd = suspension + 48h review, 3rd = ban**; phone-only change is routine re-verification
- Settlement: **disagreement = `DISPUTED`**, official league source decides, corrections are reversing entries
- Statuses: **WIN / LOSS / PENDING / LIVE / POSTPONED** — fixed colour tokens, text + icon on every badge
- Markets: **Full Time Over 0.5 Goals**, **Over 0.5 Half Time** only
- Trust counter: **aggregate wins/losses/rate/streak**, confidence badge per match, losses never hidden
- Visibility: **Home = today + tomorrow**, **History = last 6 months**
- Design: **dark mode default**, glass surfaces, system fonts, 44px tap targets, 12/16px radius
- Engagement: **mechanics approved**, guardrails enforced as release blockers (see Engagement Policy)
- Admin centres: **Matches (CSV)**, **Users & Financial Status**, **Results Archive (6 months)**, **Admin Profile**
- User activation: **manual** (admin marks active after payment) until automated
- Public landing page: **live match ticker + activity feed + infinite scroll**, signup wall only at point of action
- Notifications: **PWA Web Push** from admin, with in-app inbox and SMS fallback for payments
- Results extraction: **Groq parses fetched pages → validator → admin approval**; AI never settles bets
- Data: **backups off-site to R2**, manual on-demand backups, quarterly restore drill is a **hard launch blocker**, RTO ≤ 1h / RPO ≤ 1min for wallets
- Security: **HTTPS + Cloudflare WAF**, parameterized queries, Argon2, admin 2FA, audit trail, pen test before launch
- Revenue: **access contribution** for running costs — explicitly **not** payment for predictions
