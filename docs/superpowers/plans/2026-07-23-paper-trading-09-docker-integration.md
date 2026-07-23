# Plan 09 — Docker Compose & Integration Smoke

> **Wave:** 4  
> **Track:** T-DEPLOY (+ tiny wiring fixes only with orchestrator approval)  
> **Depends on:** Plans 02–08 substantially complete  
> **Parallel:** limited — prefer one subagent; second only for agent Dockerfiles already done

**Goal:** `docker compose -f deploy/docker-compose.yml up --build` brings full stack; one EOD smoke path works with `LLM_MODE=mock` and `MARKET_DATA_PROVIDER=free`.

---

### Task 09.1: API + Web Dockerfiles finalize

**Files:**
- Create/Modify: `services/api/Dockerfile`
- Modify: `apps/web/Dockerfile` (multi-stage if needed)
- Create: `deploy/docker-compose.yml`
- Create: `deploy/docker-compose.override.yml` (dev: mock LLM, published ports)

**Services:** `postgres`, `redis`, `api`, `web`, `agent-data`, `agent-research`, `agent-decision`, `agent-portfolio`, `agent-risk`

- [ ] **Step 1: Write compose with healthchecks** (`pg_isready`, redis `PING`, api `/healthz`)

- [ ] **Step 2: Env from `deploy/env.example`**

- [ ] **Step 3: Commit** `chore: docker compose stack`

---

### Task 09.2: CORS + URL wiring check

**Files:**
- Modify only if needed: `services/api/internal/httpserver/router.go` (CORS allow web origin)
- Modify: `deploy/docker-compose.yml` env `NEXT_PUBLIC_API_BASE_URL`

- [ ] **Step 1: Document browser access ports** (web `:3000`, api `:8080`)

- [ ] **Step 2: Commit** `fix: cors and public api url for compose`

---

### Task 09.3: Integration smoke script

**Files:**
- Create: `deploy/smoke_eod.sh` (or `smoke_eod.ps1` for Windows)
- Create: `deploy/README.md`

**Script steps:**
1. Wait for `/healthz`
2. Login → token
3. `POST /api/v1/runs/eod`
4. Poll run until `executed|awaiting_approval|failed`
5. Exit 0 if not `failed`

- [ ] **Step 1: Write script**

- [ ] **Step 2: Run against compose (orchestrator)**

- [ ] **Step 3: Commit** `chore: eod smoke script`

---

### Task 09.4: Final spec gate

**Files:** none

- [ ] **Step 1: Checklist vs spec §1.1 success criteria** — each item verified manually once
- [ ] **Step 2: Note known gaps in `deploy/README.md` if any (no silent TBD in code)
- [ ] **Step 3: Commit** `docs: deploy readme smoke results notes` (if notes added)
