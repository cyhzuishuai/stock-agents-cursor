# Plan 08 — Next.js Frontend (parallelizable pages)

> **Wave:** 1 scaffold → 2–3 pages  
> **Track:** T-WEB only (`apps/web/**`)  
> **Depends on:** Plan 01 (`api_dto.md`); pages can use MSW mocks until Plan 07  
> **Parallel:** 08.1 with Wave1; 08.2–08.7 parallel after 08.1 if different files

**Goal:** Single-admin UI: login, overview, portfolio, runs, approvals, settings.

**Tech Stack:** Next.js 15 App Router, TypeScript, fetch wrapper, CSS modules or Tailwind (pick **one** in 08.1; do not mix).

---

### Task 08.1: Next.js scaffold + API client + types

**Files:**
- Create: `apps/web/package.json`, `apps/web/tsconfig.json`, `apps/web/next.config.ts`, `apps/web/src/app/layout.tsx`, `apps/web/src/app/globals.css`
- Create: `apps/web/src/lib/types.ts` (from api_dto)
- Create: `apps/web/src/lib/api.ts` (token in localStorage; `Authorization: Bearer`)
- Create: `apps/web/Dockerfile`
- Test: `apps/web/src/lib/api.test.ts` (vitest) — builds query string / attaches header

**Interfaces:**
- `api.get/post<T>(path, body?)`
- Env `NEXT_PUBLIC_API_BASE_URL` default `http://localhost:8080`

- [ ] **Step 1: `npx create-next-app` in `apps/web` (TS, App Router, no unnecessary extras)**

- [ ] **Step 2: types + api client + vitest**

- [ ] **Step 3: Commit** `feat: nextjs scaffold and api client`

---

### Task 08.2: Login page

**Files:** `apps/web/src/app/login/page.tsx`, `apps/web/src/app/login/login-form.tsx`  
**Test:** vitest/react-testing-library render + mock fetch

- [ ] **Step 1: Failing RTL test submit calls `/api/v1/auth/login`**

- [ ] **Step 2: Implement; store token; redirect `/`**

- [ ] **Step 3: Commit** `feat: login page`

---

### Task 08.3: Auth gate + shell nav

**Files:** `apps/web/src/components/AppShell.tsx`, `apps/web/src/components/AuthGate.tsx`, modify `layout` or template

- [ ] **Step 1: Redirect unauthenticated users to `/login`**

- [ ] **Step 2: Nav links: Overview, Portfolio, Runs, Approvals, Settings**

- [ ] **Step 3: Commit** `feat: app shell and auth gate`

---

### Task 08.4: Overview page

**Files:** `apps/web/src/app/page.tsx`, `apps/web/src/components/NavSparkline.tsx`, `apps/web/src/components/RunStatusBadge.tsx`

**UI content (spec §7):** cash, equity/nav, pending approvals count+link, latest run status, positions summary, nav sparkline (SVG polyline OK)

- [ ] **Step 1: Component test with fixture props**

- [ ] **Step 2: Fetch `GET /api/v1/overview`**

- [ ] **Step 3: Commit** `feat: overview dashboard`

---

### Task 08.5: Portfolio page

**Files:** `apps/web/src/app/portfolio/page.tsx`

- [ ] **Step 1: Table of positions with weight, pnl, stops**

- [ ] **Step 2: Commit** `feat: portfolio page`

---

### Task 08.6: Runs list + detail

**Files:** `apps/web/src/app/runs/page.tsx`, `apps/web/src/app/runs/[id]/page.tsx`

- [ ] **Step 1: List runs**

- [ ] **Step 2: Detail shows steps timeline + proposals + orders**

- [ ] **Step 3: Button “Run EOD now” → `POST /api/v1/runs/eod`**

- [ ] **Step 4: Commit** `feat: runs list and detail`

---

### Task 08.7: Approvals page

**Files:** `apps/web/src/app/approvals/page.tsx`

- [ ] **Step 1: List pending; approve/reject with note**

- [ ] **Step 2: RTL test decide posts correct body**

- [ ] **Step 3: Commit** `feat: approvals page`

---

### Task 08.8: Settings read-only

**Files:** `apps/web/src/app/settings/page.tsx`

- [ ] **Step 1: Show watchlist, risk rules, market_data_provider**

- [ ] **Step 2: Commit** `feat: settings readonly page`

---

### Parallel assignment hint (after 08.1+08.3)

| Subagent | Tasks |
|----------|-------|
| W1 | 08.4 Overview |
| W2 | 08.5 Portfolio |
| W3 | 08.6 Runs |
| W4 | 08.7 Approvals + 08.8 Settings |
