# Day Settlement Desk UI Redesign — Design Spec

**Date:** 2026-07-27  
**Status:** Draft for user review  
**Scope:** Visual + layout (Option B) for `apps/web`  
**Direction:** Day Settlement Desk (Option 1)

## 1. Goal

Reshape the Stock Agents Next.js console into a **day-settlement desk**: a calm, light, table-first UI for a single admin to scan NAV/positions and clear pending approvals after the US equity EOD agent run.

### 1.1 Success criteria

- Distinct visual identity via shared CSS tokens (color, type, spacing); no component library.
- App shell and key pages re-hierarchied so NAV / pending approvals / latest run are the first scan path.
- Existing routes, auth, API clients, and business logic unchanged.
- Existing frontend tests still pass (update selectors/copy only when markup changes require it).
- Responsive down to mobile; visible keyboard focus; `prefers-reduced-motion` respected.
- Forced light theme (no `prefers-color-scheme: dark` flip).

### 1.2 Out of scope

- New pages, routes, or API endpoints
- Auth/JWT/session behavior changes
- Charting library beyond the existing SVG `NavSparkline`
- Dark mode / theme toggle
- i18n / Chinese UI copy (keep current English labels unless a label must change for clarity)
- Tailwind or third-party UI kits

## 2. Product & design decisions (locked)

| Topic | Choice |
|-------|--------|
| Scope | B — visual tokens + layout hierarchy |
| Aesthetic | Day Settlement Desk (light institutional) |
| Theme | Light only |
| Shell | Top header (no sidebar) |
| Signature | Left trade-date color bar on the shell header (always Ledger); pending urgency shown on Overview strip / Approvals, not via extra shell fetch |
| Stack | Existing CSS modules/classes in `globals.css` + Next font loaders |

## 3. Visual system

### 3.1 Color tokens

| Token | Hex | Role |
|-------|-----|------|
| `--paper` | `#EEF1F4` | Page background |
| `--ink` | `#1A2332` | Primary text |
| `--ledger` | `#2F5B8A` | Primary action / nav emphasis |
| `--rule` | `#C5CDD6` | Dividers, table rules, input borders |
| `--surface` | `#F7F9FB` | Raised panels / login plaque |
| `--settle-plus` | `#1F7A4D` | Positive PnL / success status |
| `--settle-minus` | `#B42318` | Negative PnL / failed status |
| `--pending` | `#9A6B16` | Awaiting approval / header bar when pending |

Muted text: `color-mix` of `--ink` at ~65% opacity. Focus ring: `--ledger` at 2px outline offset 2px.

### 3.2 Typography

| Role | Face | Usage |
|------|------|--------|
| Display | Fraunces (Google font) | Brand wordmark, page `h1` only |
| Body | Source Sans 3 | Nav, labels, prose, buttons |
| Data | IBM Plex Mono | Currency, qty, symbols, timestamps, IDs |

Type scale (approx): eyebrow 0.75rem / page title 1.75rem / section 1rem / body 0.9375rem / data 0.875rem. Tabular nums on data face.

Replace unused Geist variables in root layout with the three faces above; apply faces via CSS variables on `body`.

### 3.3 Signature element

A **4px vertical bar** on the left edge of `.app-shell__header`, always `--ledger` (trade-date / settlement stamp).

Pending urgency uses `--pending` on the **Overview pending strip** and Approvals action emphasis — not on the shell bar — so the shell never needs its own approvals fetch. No floating badges on charts, no glow, no gradient hero.

### 3.4 Motion

- Nav active underline: short color/width transition (~150ms)
- Button hover: slight background shift
- No page-load choreography, no scroll reveals
- `@media (prefers-reduced-motion: reduce)` → transitions none

## 4. Architecture (frontend only)

```
apps/web/src/
  app/layout.tsx          # fonts + metadata
  app/globals.css         # tokens + all page styles
  app/login/*             # plaque layout
  app/(shell)/layout.tsx  # AuthGate + AppShell
  components/AppShell.tsx # header, nav, signature bar
  components/*            # OverviewDashboard, badges, sparkline — markup tweaks only
```

- Prefer extending existing BEM-ish class names over new frameworks.
- Shared primitives in CSS: `.page-header`, `.stat-grid`, `.data-table`, `.btn`, `.btn--primary`, `.btn--ghost`, `.status-badge`, `.empty-state`, `.alert`.
- Data flow, error handling, and loading states stay as today (local `useState` + `api` client); only presentation wrappers change.

## 5. Layout by page

### 5.1 AppShell

```
|█| Stock Agents     Overview  Portfolio  Runs  Approvals  Settings
```

- `█` = signature bar (Ledger only; no pending badge / no extra fetch)
- Main: padded, max-width 72rem, centered

### 5.2 Login

Centered plaque (`--surface`) on `--paper`: brand (Fraunces) + short line “EOD paper desk” + username/password + primary Log in. Errors use `--settle-minus` alert text. No full-bleed illustration.

### 5.3 Overview

1. Page header: eyebrow `EOD desk` + title `Overview`
2. Stat row: **NAV** (emphasized), Equity, Cash
3. Pending strip: if count > 0, Pending-tinted bar + link to Approvals; else muted “None pending”
4. Latest run: id link, trade_date, status badge
5. Two-column on wide viewports: Positions table | NAV sparkline; stack on narrow

### 5.4 Portfolio

Same page-header pattern; three stats; positions table with tabular PnL coloring (`--settle-plus` / `--settle-minus`).

### 5.5 Runs list & detail

- List: header + Refresh (and existing actions); table columns emphasize `trade_date`, `status`, `id`
- Detail: back link; meta row; step timeline with semantic step colors (ok / failed / pending / muted)

### 5.6 Approvals

- Header + Refresh
- Table: symbol, side, qty, breach reasons (wrap), note textarea, actions
- Approve = `.btn--primary`; Reject = `.btn--ghost` / danger outline
- Empty: “No pending approvals” with short direction to wait for next EOD run

### 5.7 Settings

Page header + grouped read-only definition list (risk params etc.); hairline rules between groups; no card chrome.

## 6. Components

| Component | Change |
|-----------|--------|
| `AppShell` | Signature bar; apply token classes; page chrome |
| `OverviewDashboard` | Reorder sections; NAV emphasis; pending strip; positions/sparkline grid |
| `RunStatusBadge` | Map statuses to settle/pending/muted token classes |
| `NavSparkline` | Stroke `--ledger`; empty state uses muted ink |
| Login form / pages | Plaque layout + shared form controls |
| Portfolio / Runs / Approvals / Settings pages | Shared page-header, tables, buttons |

## 7. Error, empty, loading

- Keep existing string patterns; restyle with `.alert` / muted empty copy.
- Loading: short plain text is fine (e.g. “Loading…”); no skeleton requirement in this redesign.
- Failures remain `role="alert"`.

## 8. Testing

- Update `OverviewDashboard.test.tsx`, `login-form.test.tsx`, `approvals/page.test.tsx` if role/label/structure assertions break.
- No new E2E required for pure CSS; smoke: login → overview → approvals visually in browser when convenient.
- Run `apps/web` unit tests before calling the work done.

## 9. Implementation notes

1. Introduce CSS custom properties at `:root`; remove dark `@media` overrides.
2. Wire Fraunces / Source Sans 3 / IBM Plex Mono in `app/layout.tsx`.
3. Refactor `globals.css` toward shared primitives; migrate page-specific blocks.
4. Adjust TSX structure for Overview two-column and login plaque; keep props/data contracts identical.
5. Verify focus styles on links, inputs, buttons.

## 10. Non-goals reminder

No API, agent, deploy, or contract changes. This spec covers `apps/web` presentation only.
