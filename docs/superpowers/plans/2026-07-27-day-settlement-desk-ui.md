# Day Settlement Desk UI Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restyle and re-hierarchize `apps/web` into the Day Settlement Desk look (light tokens, Fraunces/Source Sans 3/IBM Plex Mono, shell signature bar, Overview NAV-first layout) without changing API or auth behavior.

**Architecture:** Keep Next.js App Router + existing BEM-ish CSS in `globals.css`. Introduce `:root` design tokens and shared primitives (`.page-header`, `.stat-grid`, `.btn`, `.data-table`, `.alert`). Touch TSX only for markup hierarchy and class names; data contracts stay identical.

**Tech Stack:** Next.js 15, React 19, CSS custom properties, `next/font/google` (Fraunces, Source_Sans_3, IBM_Plex_Mono), Vitest + Testing Library.

**Spec:** `docs/superpowers/specs/2026-07-27-day-settlement-desk-ui-design.md`

## Global Constraints

- Scope Option B only: visual tokens + layout hierarchy in `apps/web/**`
- No new routes, API calls, component libraries, Tailwind, or dark mode
- Keep English UI labels; preserve existing accessible names used by tests (`/overview/i`, `/2 pending — review/i`, `/log in/i`, `/approve #7/i`, etc.) unless a step explicitly updates the test
- Forced light theme; remove `prefers-color-scheme: dark` overrides
- Signature bar always `--ledger`; pending urgency only on Overview strip / Approvals styling
- Run `cd apps/web && npm test` before claiming a task done when that task touches tested UI
- Do not commit unless the user explicitly asks (plan steps that say Commit are optional gates)

---

## File map

| File | Responsibility |
|------|----------------|
| `apps/web/src/app/layout.tsx` | Load Fraunces / Source Sans 3 / IBM Plex Mono; expose CSS variables |
| `apps/web/src/app/globals.css` | Tokens, primitives, page styles; remove dark scheme |
| `apps/web/src/components/AppShell.tsx` | Top nav + 4px Ledger signature bar |
| `apps/web/src/app/login/page.tsx` | Login plaque chrome |
| `apps/web/src/app/login/login-form.tsx` | Form control classes only |
| `apps/web/src/components/OverviewDashboard.tsx` | NAV-first stats, pending strip, positions\|sparkline grid |
| `apps/web/src/components/RunStatusBadge.tsx` | Keep class hooks; CSS maps to settle tokens |
| `apps/web/src/components/NavSparkline.tsx` | Keep markup; CSS stroke uses `--ledger` |
| `apps/web/src/app/(shell)/portfolio/page.tsx` | Page header + shared table/stat classes |
| `apps/web/src/app/(shell)/runs/page.tsx` | Page header + `.btn` actions |
| `apps/web/src/app/(shell)/runs/[id]/page.tsx` | Page header + timeline token colors |
| `apps/web/src/app/(shell)/approvals/page.tsx` | Page header + primary/ghost buttons |
| `apps/web/src/app/(shell)/settings/page.tsx` | Page header + grouped panels |
| `apps/web/src/components/OverviewDashboard.test.tsx` | Assert eyebrow + NAV emphasis if added |
| Existing login/approvals tests | Should keep passing unchanged |

---

### Task 1: Fonts + design tokens + CSS primitives

**Files:**
- Modify: `apps/web/src/app/layout.tsx`
- Modify: `apps/web/src/app/globals.css` (replace `:root` / body / add primitives; keep existing page blocks temporarily working via token aliases)
- Test: `cd apps/web && npm test` (baseline must still pass)

**Interfaces:**
- Produces CSS variables: `--paper`, `--ink`, `--ledger`, `--rule`, `--surface`, `--settle-plus`, `--settle-minus`, `--pending`, `--font-display`, `--font-body`, `--font-data`
- Produces utility classes: `.page-header`, `.page-header__eyebrow`, `.page-header__title`, `.stat-grid`, `.stat`, `.stat--emphasis`, `.stat__label`, `.stat__value`, `.btn`, `.btn--primary`, `.btn--ghost`, `.btn--danger`, `.data-table`, `.alert`, `.empty-state`, `.panel`, `.panel__title`

- [ ] **Step 1: Replace fonts in `layout.tsx`**

```tsx
import type { Metadata } from "next";
import { Fraunces, Source_Sans_3, IBM_Plex_Mono } from "next/font/google";
import "./globals.css";

const fraunces = Fraunces({
  variable: "--font-display",
  subsets: ["latin"],
});

const sourceSans = Source_Sans_3({
  variable: "--font-body",
  subsets: ["latin"],
});

const ibmPlexMono = IBM_Plex_Mono({
  variable: "--font-data",
  weight: ["400", "500"],
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "Stock Agents",
  description: "US equities EOD paper-trading dashboard",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <body
        className={`${fraunces.variable} ${sourceSans.variable} ${ibmPlexMono.variable}`}
      >
        {children}
      </body>
    </html>
  );
}
```

- [ ] **Step 2: Rewrite `:root` / `body` and add primitives at the top of `globals.css`; delete dark `@media (prefers-color-scheme: dark)` blocks; map legacy `--background`/`--foreground` to tokens so old classes keep working**

```css
:root {
  --paper: #eef1f4;
  --ink: #1a2332;
  --ledger: #2f5b8a;
  --rule: #c5cdd6;
  --surface: #f7f9fb;
  --settle-plus: #1f7a4d;
  --settle-minus: #b42318;
  --pending: #9a6b16;
  --ink-muted: color-mix(in srgb, var(--ink) 65%, transparent);
  --background: var(--paper);
  --foreground: var(--ink);
}

html,
body {
  max-width: 100vw;
  overflow-x: hidden;
}

body {
  color: var(--ink);
  background: var(--paper);
  font-family: var(--font-body), "Source Sans 3", system-ui, sans-serif;
  font-size: 0.9375rem;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
}

* {
  box-sizing: border-box;
  padding: 0;
  margin: 0;
}

a {
  color: inherit;
  text-decoration: none;
}

a:focus-visible,
button:focus-visible,
input:focus-visible,
textarea:focus-visible {
  outline: 2px solid var(--ledger);
  outline-offset: 2px;
}

.page-header {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  margin-bottom: 0.25rem;
}

.page-header__eyebrow {
  font-size: 0.75rem;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--ink-muted);
  font-family: var(--font-data), ui-monospace, monospace;
}

.page-header__title {
  font-family: var(--font-display), Georgia, serif;
  font-size: 1.75rem;
  font-weight: 600;
  color: var(--ink);
  line-height: 1.2;
}

.stat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(10rem, 1fr));
  gap: 1rem;
}

.stat {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  padding: 0.75rem 1rem;
  border: 1px solid var(--rule);
  border-radius: 0.25rem;
  background: var(--surface);
}

.stat--emphasis {
  border-color: var(--ledger);
  box-shadow: inset 3px 0 0 var(--ledger);
}

.stat__label {
  font-size: 0.875rem;
  color: var(--ink-muted);
}

.stat__value {
  font-family: var(--font-data), ui-monospace, monospace;
  font-size: 1.25rem;
  font-weight: 500;
  font-variant-numeric: tabular-nums;
}

.btn {
  padding: 0.375rem 0.75rem;
  border: 1px solid var(--rule);
  border-radius: 0.25rem;
  background: var(--surface);
  color: var(--ink);
  font: inherit;
  cursor: pointer;
  transition: background-color 150ms ease, border-color 150ms ease;
}

.btn:hover:not(:disabled) {
  background: color-mix(in srgb, var(--ledger) 8%, var(--surface));
}

.btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.btn--primary {
  background: var(--ledger);
  border-color: var(--ledger);
  color: #fff;
}

.btn--primary:hover:not(:disabled) {
  background: color-mix(in srgb, var(--ink) 20%, var(--ledger));
}

.btn--ghost {
  background: transparent;
}

.btn--danger {
  border-color: var(--settle-minus);
  color: var(--settle-minus);
  background: transparent;
}

.btn--danger:hover:not(:disabled) {
  background: color-mix(in srgb, var(--settle-minus) 8%, transparent);
}

.data-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.9375rem;
}

.data-table th,
.data-table td {
  padding: 0.5rem 0.75rem;
  text-align: left;
  border-bottom: 1px solid var(--rule);
}

.data-table th {
  font-weight: 600;
  color: var(--ink-muted);
  font-size: 0.8125rem;
}

.data-table td {
  font-family: var(--font-data), ui-monospace, monospace;
  font-variant-numeric: tabular-nums;
}

.panel {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.panel__title {
  font-size: 1rem;
  font-weight: 600;
}

.alert {
  color: var(--settle-minus);
}

.empty-state {
  color: var(--ink-muted);
}

.pending-strip {
  padding: 0.75rem 1rem;
  border: 1px solid color-mix(in srgb, var(--pending) 40%, var(--rule));
  border-radius: 0.25rem;
  background: color-mix(in srgb, var(--pending) 12%, var(--surface));
  color: var(--ink);
}

.pending-strip a {
  color: var(--ledger);
  text-decoration: underline;
  text-underline-offset: 2px;
}

.pending-strip--clear {
  background: transparent;
  border-color: transparent;
  padding-left: 0;
  color: var(--ink-muted);
}

@media (prefers-reduced-motion: reduce) {
  *,
  *::before,
  *::after {
    transition: none !important;
  }
}
```

Keep the existing `.app-shell*`, `.overview*`, `.portfolio*`, `.runs*`, `.run-status-badge*`, `.nav-sparkline*` blocks below for now, but change hardcoded greens/reds to `var(--settle-plus)` / `var(--settle-minus)` and borders to `var(--rule)`.

- [ ] **Step 3: Run tests**

Run: `cd apps/web && npm test`  
Expected: all existing tests PASS

- [ ] **Step 4: Commit (only if user asked)**

```bash
git add apps/web/src/app/layout.tsx apps/web/src/app/globals.css
git commit -m "style: add settlement desk tokens and fonts"
```

---

### Task 2: AppShell signature bar

**Files:**
- Modify: `apps/web/src/components/AppShell.tsx`
- Modify: `apps/web/src/app/globals.css` (`.app-shell__header` / brand / nav active)

**Interfaces:**
- Consumes: token classes from Task 1
- Produces: markup with `.app-shell__signature` (4px bar); brand uses display font via CSS

- [ ] **Step 1: Update `AppShell.tsx` markup**

```tsx
"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

const NAV_ITEMS = [
  { href: "/", label: "Overview" },
  { href: "/portfolio", label: "Portfolio" },
  { href: "/runs", label: "Runs" },
  { href: "/approvals", label: "Approvals" },
  { href: "/settings", label: "Settings" },
] as const;

function isActive(pathname: string, href: string): boolean {
  if (href === "/") {
    return pathname === "/";
  }
  return pathname === href || pathname.startsWith(`${href}/`);
}

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();

  return (
    <div className="app-shell">
      <header className="app-shell__header">
        <span className="app-shell__signature" aria-hidden="true" />
        <p className="app-shell__brand">Stock Agents</p>
        <nav className="app-shell__nav" aria-label="Main">
          {NAV_ITEMS.map(({ href, label }) => (
            <Link
              key={href}
              href={href}
              className={
                isActive(pathname, href)
                  ? "app-shell__link app-shell__link--active"
                  : "app-shell__link"
              }
              aria-current={isActive(pathname, href) ? "page" : undefined}
            >
              {label}
            </Link>
          ))}
        </nav>
      </header>
      <main className="app-shell__main">{children}</main>
    </div>
  );
}
```

- [ ] **Step 2: Update shell CSS**

```css
.app-shell {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
}

.app-shell__header {
  display: flex;
  align-items: center;
  gap: 1.5rem;
  padding: 0.75rem 1.5rem;
  border-bottom: 1px solid var(--rule);
  background: var(--surface);
  position: relative;
}

.app-shell__signature {
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 4px;
  background: var(--ledger);
}

.app-shell__brand {
  font-family: var(--font-display), Georgia, serif;
  font-weight: 600;
  font-size: 1.125rem;
  padding-left: 0.25rem;
}

.app-shell__nav {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}

.app-shell__link {
  padding: 0.35rem 0.6rem;
  border-radius: 0.25rem;
  color: var(--ink-muted);
  border-bottom: 2px solid transparent;
  transition: color 150ms ease, border-color 150ms ease;
}

.app-shell__link--active {
  color: var(--ink);
  border-bottom-color: var(--ledger);
  background: transparent;
}

.app-shell__main {
  flex: 1;
  padding: 1.5rem;
  width: min(72rem, 100%);
  margin-inline: auto;
}
```

- [ ] **Step 3: Run tests**

Run: `cd apps/web && npm test`  
Expected: PASS

- [ ] **Step 4: Commit (only if user asked)**

```bash
git add apps/web/src/components/AppShell.tsx apps/web/src/app/globals.css
git commit -m "style: add shell signature bar and nav chrome"
```

---

### Task 3: Login plaque

**Files:**
- Modify: `apps/web/src/app/login/page.tsx`
- Modify: `apps/web/src/app/login/login-form.tsx`
- Modify: `apps/web/src/app/globals.css` (`.login*` rules)
- Test: `apps/web/src/app/login/login-form.test.tsx` (must keep passing — do not rename Log in / Username / Password)

**Interfaces:**
- Consumes: `.btn--primary`, tokens
- Produces: `.login`, `.login__plaque`, `.login__brand`, `.login__tagline`, `.login__form`, `.login__field`

- [ ] **Step 1: Update login page**

```tsx
import { LoginForm } from "./login-form";

export default function LoginPage() {
  return (
    <main className="login">
      <div className="login__plaque">
        <p className="login__brand">Stock Agents</p>
        <p className="login__tagline">EOD paper desk</p>
        <h1 className="login__title">Login</h1>
        <LoginForm />
      </div>
    </main>
  );
}
```

- [ ] **Step 2: Add form classNames in `login-form.tsx` without changing labels or button text**

Wrap fields with `className="login__form"`, each field `login__field`, error `className="alert" role="alert"`, submit `className="btn btn--primary"`.

- [ ] **Step 3: Add `.login*` CSS** (centered plaque, surface background, display brand, data tagline, stacked fields with bordered inputs using `--rule`)

- [ ] **Step 4: Run login test**

Run: `cd apps/web && npm test -- src/app/login/login-form.test.tsx`  
Expected: PASS

- [ ] **Step 5: Commit (only if user asked)**

```bash
git add apps/web/src/app/login/page.tsx apps/web/src/app/login/login-form.tsx apps/web/src/app/globals.css
git commit -m "style: login settlement plaque"
```

---

### Task 4: Overview hierarchy

**Files:**
- Modify: `apps/web/src/components/OverviewDashboard.tsx`
- Modify: `apps/web/src/components/OverviewDashboard.test.tsx`
- Modify: `apps/web/src/app/globals.css` (`.overview*` + grid)

**Interfaces:**
- Consumes: `OverviewResponse` unchanged; `.page-header`, `.stat-grid`, `.stat--emphasis`, `.pending-strip`, `.data-table`
- Produces: eyebrow text `EOD desk`; pending link text still `N pending — review`

- [ ] **Step 1: Extend the existing test for eyebrow + NAV label emphasis**

In `OverviewDashboard.test.tsx`, add:

```tsx
expect(screen.getByText(/eod desk/i)).toBeTruthy();
expect(screen.getByText("NAV")).toBeTruthy();
```

Keep all existing assertions (especially `/2 pending — review/i` and `/run #42/i`).

- [ ] **Step 2: Run test to confirm current failure on eyebrow (optional if already absent)**

Run: `cd apps/web && npm test -- src/components/OverviewDashboard.test.tsx`  
Expected: FAIL on `/eod desk/i` until Step 3

- [ ] **Step 3: Rewrite `OverviewDashboard` structure**

```tsx
import Link from "next/link";
import { NavSparkline } from "@/components/NavSparkline";
import { RunStatusBadge } from "@/components/RunStatusBadge";
import type { OverviewResponse } from "@/lib/types";

const currency = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  maximumFractionDigits: 0,
});

const percent = new Intl.NumberFormat("en-US", {
  style: "percent",
  minimumFractionDigits: 1,
  maximumFractionDigits: 1,
});

export function OverviewDashboard({ data }: { data: OverviewResponse }) {
  return (
    <div className="overview">
      <header className="page-header">
        <p className="page-header__eyebrow">EOD desk</p>
        <h1 className="page-header__title">Overview</h1>
      </header>

      <section className="stat-grid" aria-label="Account summary">
        <div className="stat stat--emphasis">
          <span className="stat__label">NAV</span>
          <span className="stat__value">{currency.format(data.nav)}</span>
        </div>
        <div className="stat">
          <span className="stat__label">Equity</span>
          <span className="stat__value">{currency.format(data.equity)}</span>
        </div>
        <div className="stat">
          <span className="stat__label">Cash</span>
          <span className="stat__value">{currency.format(data.cash)}</span>
        </div>
      </section>

      <section
        className={
          data.pending_approvals_count > 0
            ? "pending-strip"
            : "pending-strip pending-strip--clear"
        }
        aria-label="Pending approvals"
      >
        {data.pending_approvals_count === 0 ? (
          <p className="empty-state">None pending</p>
        ) : (
          <p>
            <Link href="/approvals">
              {data.pending_approvals_count} pending — review
            </Link>
          </p>
        )}
      </section>

      <section className="panel">
        <h2 className="panel__title">Latest run</h2>
        {data.latest_run ? (
          <p className="overview__run">
            <Link href={`/runs/${data.latest_run.id}`}>
              Run #{data.latest_run.id}
            </Link>
            <span className="overview__run-meta">
              {data.latest_run.trade_date}
            </span>
            <RunStatusBadge status={data.latest_run.status} />
          </p>
        ) : (
          <p className="empty-state">No runs yet</p>
        )}
      </section>

      <div className="overview__split">
        <section className="panel">
          <h2 className="panel__title">Positions</h2>
          {data.positions_summary.length === 0 ? (
            <p className="empty-state">No open positions</p>
          ) : (
            <table className="data-table">
              <thead>
                <tr>
                  <th scope="col">Symbol</th>
                  <th scope="col">Qty</th>
                  <th scope="col">Market value</th>
                  <th scope="col">Weight</th>
                </tr>
              </thead>
              <tbody>
                {data.positions_summary.map((row) => (
                  <tr key={row.symbol}>
                    <td>{row.symbol}</td>
                    <td>{row.qty}</td>
                    <td>{currency.format(row.market_value)}</td>
                    <td>{percent.format(row.weight)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </section>

        <section className="panel">
          <h2 className="panel__title">NAV history</h2>
          <NavSparkline series={data.nav_series} />
        </section>
      </div>
    </div>
  );
}
```

Add CSS:

```css
.overview {
  display: flex;
  flex-direction: column;
  gap: 1.25rem;
}

.overview__split {
  display: grid;
  grid-template-columns: minmax(0, 1.4fr) minmax(0, 1fr);
  gap: 1.25rem;
  align-items: start;
}

@media (max-width: 48rem) {
  .overview__split {
    grid-template-columns: 1fr;
  }
}

.overview__run {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.75rem;
}

.overview__run-meta {
  font-family: var(--font-data), ui-monospace, monospace;
  color: var(--ink-muted);
}
```

Remove obsolete `.overview__title` / `.overview__stats` / `.overview__stat*` if unused.

- [ ] **Step 4: Run overview test**

Run: `cd apps/web && npm test -- src/components/OverviewDashboard.test.tsx`  
Expected: PASS

- [ ] **Step 5: Commit (only if user asked)**

```bash
git add apps/web/src/components/OverviewDashboard.tsx apps/web/src/components/OverviewDashboard.test.tsx apps/web/src/app/globals.css
git commit -m "feat: overview NAV-first settlement layout"
```

---

### Task 5: Portfolio, Runs, Approvals, Settings chrome

**Files:**
- Modify: `apps/web/src/app/(shell)/portfolio/page.tsx`
- Modify: `apps/web/src/app/(shell)/runs/page.tsx`
- Modify: `apps/web/src/app/(shell)/runs/[id]/page.tsx`
- Modify: `apps/web/src/app/(shell)/approvals/page.tsx`
- Modify: `apps/web/src/app/(shell)/settings/page.tsx`
- Modify: `apps/web/src/app/globals.css` (align `.portfolio*` / `.runs*` with tokens; prefer shared `.page-header` / `.stat-grid` / `.data-table` / `.btn`)
- Test: `apps/web/src/app/(shell)/approvals/page.test.tsx` must keep passing

**Interfaces:**
- Approvals: Approve button remains accessible name `/approve #7/i`; Reject `/reject #7/i`; note label unchanged
- Empty approvals copy may append guidance: keep heading `No pending approvals` text present (can be `No pending approvals. Wait for the next EOD run.` — ensure test does not assert exact empty string; current test does not)

- [ ] **Step 1: Portfolio — wrap title in `.page-header` with eyebrow `EOD desk`; use `.stat-grid` / `.stat` / `.data-table`; map PnL classes to settle tokens**

```css
.portfolio__pnl--positive { color: var(--settle-plus); }
.portfolio__pnl--negative { color: var(--settle-minus); }
```

- [ ] **Step 2: Runs list — `.page-header` + actions as `.btn` / `.btn--primary` for Run EOD; table `.data-table`; loading/error use `.alert` / plain text**

- [ ] **Step 3: Run detail — same header pattern; timeline step ok/failed use settle tokens**

```css
.runs__step-status--ok { color: var(--settle-plus); }
.runs__step-status--failed { color: var(--settle-minus); }
```

- [ ] **Step 4: Approvals — Approve `btn btn--primary`; Reject `btn btn--danger`; refresh `btn`; preserve button text `Approve #${id}` / `Reject #${id}`**

- [ ] **Step 5: Settings — `.page-header`; sections as `.panel` with hairline separators; tables `.data-table`**

- [ ] **Step 6: Run all web tests**

Run: `cd apps/web && npm test`  
Expected: PASS (including approvals decide body assertion)

- [ ] **Step 7: Commit (only if user asked)**

```bash
git add apps/web/src/app/(shell) apps/web/src/app/globals.css
git commit -m "style: settle desk chrome on portfolio runs approvals settings"
```

---

### Task 6: Status badge + sparkline token polish + final verification

**Files:**
- Modify: `apps/web/src/app/globals.css` (`.run-status-badge*`, `.nav-sparkline*`)
- Optionally touch: `apps/web/src/components/RunStatusBadge.tsx` / `NavSparkline.tsx` only if class names need renaming (prefer CSS-only)

**Interfaces:**
- Badge labels unchanged (`Awaiting approval`, etc.)
- Sparkline `aria-label="NAV history sparkline"` unchanged

- [ ] **Step 1: Map badge colors to tokens**

```css
.run-status-badge {
  display: inline-block;
  padding: 0.125rem 0.5rem;
  border-radius: 0.25rem;
  font-size: 0.8125rem;
  font-weight: 500;
  font-family: var(--font-data), ui-monospace, monospace;
  background: color-mix(in srgb, var(--ink) 8%, transparent);
}

.run-status-badge--success {
  background: color-mix(in srgb, var(--settle-plus) 18%, transparent);
  color: var(--settle-plus);
}

.run-status-badge--danger {
  background: color-mix(in srgb, var(--settle-minus) 18%, transparent);
  color: var(--settle-minus);
}

.run-status-badge--warning {
  background: color-mix(in srgb, var(--pending) 18%, transparent);
  color: var(--pending);
}

.run-status-badge--muted {
  opacity: 0.7;
}

.nav-sparkline {
  display: block;
  width: 100%;
  max-width: 20rem;
  border: 1px solid var(--rule);
  border-radius: 0.25rem;
  background: var(--surface);
}

.nav-sparkline__line {
  stroke: var(--ledger);
  stroke-width: 2;
}

.nav-sparkline__empty {
  fill: var(--ink-muted);
  font-size: 10px;
}
```

- [ ] **Step 2: Full test + lint**

Run:

```bash
cd apps/web
npm test
npm run lint
```

Expected: tests PASS; lint clean or only pre-existing issues unrelated to this change

- [ ] **Step 3: Optional production build smoke**

Run: `cd apps/web && npm run build`  
Expected: Next.js build succeeds (fonts download may need network)

- [ ] **Step 4: Commit (only if user asked)**

```bash
git add apps/web/src/app/globals.css
git commit -m "style: status and sparkline settlement tokens"
```

---

## Spec coverage checklist

| Spec item | Task |
|-----------|------|
| Color tokens + forced light | Task 1 |
| Fraunces / Source Sans 3 / IBM Plex Mono | Task 1 |
| Shared primitives | Task 1 |
| Shell signature bar (Ledger only) | Task 2 |
| Login plaque | Task 3 |
| Overview NAV-first + pending strip + split | Task 4 |
| Portfolio / Runs / Approvals / Settings chrome | Task 5 |
| Badge + sparkline tokens | Task 6 |
| Tests still pass | Tasks 1–6 |
| No shell pending fetch/badge | Task 2 (explicit omission) |
| `prefers-reduced-motion` | Task 1 |
| Focus rings | Task 1 |

## Plan self-review

- No TBD/placeholder steps remain.
- Approvals accessible names preserved for existing test.
- Overview pending link text preserved.
- Commit steps gated on user request per repo rules.
