# Docker Compose deployment

**Authority (Phase 1):** Alpaca Paper is the source of truth for cash, equity, positions, orders, and fill prices. Go API is the only component that talks to Alpaca Trading. Postgres stores runs, proposals, approvals, and order mirrors for audit — not fill authority. See `docs/superpowers/specs/2026-07-28-alpaca-paper-authority-design.md`.

## Environment file (`.env`)

**Path:** `deploy/.env` (same folder as this README / `docker-compose.yml`)

```bash
# from repo root
cp deploy/env.example deploy/.env
```

Then edit secrets (`JWT_SECRET`, `ADMIN_PASSWORD`, `LLM_API_KEY`, `ALPACA_API_KEY`, `ALPACA_API_SECRET`, …).

| Variable | Notes |
|----------|-------|
| `ALPACA_API_KEY` / `ALPACA_API_SECRET` | Required for Paper trading and market data; server-side only |
| `ALPACA_BASE_URL` | Default `https://paper-api.alpaca.markets` |
| `ALPACA_DATA_BASE_URL` | Default `https://data.alpaca.markets` |
| `MARKET_DATA_PROVIDER` | Default `alpaca`; set `free` (Yahoo) only as dev fallback without keys |
| `INITIAL_CASH` | Offline/test seed only; live cash comes from Alpaca Paper account |

| File | Role |
|------|------|
| `deploy/env.example` | Committed template (safe defaults, empty secrets) |
| `deploy/.env` | Local secrets; gitignored; loaded by Compose `env_file` |

Compose also auto-loads `deploy/.env` for `${VAR}` substitution when you run compose from `deploy/`.

Do **not** commit `deploy/.env`.

## Browser ports (local override)

| Service | URL |
|---------|-----|
| Web UI | http://localhost:3000 |
| API | http://localhost:8080 |

The web app calls the API via `NEXT_PUBLIC_API_BASE_URL` (default `http://localhost:8080`), which must be reachable from the browser—not an internal Docker hostname like `http://api:8080`.

## Run the stack

**Prerequisite:** `deploy/.env` exists (copy from `env.example` first).

From the repo root:

```bash
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.override.yml up --build
```

Or from this directory (Compose auto-merges `docker-compose.override.yml`):

```bash
cd deploy
docker compose up --build
```

Health check: `curl http://localhost:8080/healthz`

## EOD smoke test

After the stack is up, run the integration smoke script from the repo root or `deploy/`:

**Linux / macOS / Git Bash:**

```bash
chmod +x deploy/smoke_eod.sh
./deploy/smoke_eod.sh
```

**Windows (PowerShell):**

```powershell
.\deploy\smoke_eod.ps1
```

The script:

1. Waits for `GET /healthz` (`status: ok`)
2. Logs in via `POST /api/v1/auth/login` and obtains a JWT
3. Triggers `POST /api/v1/runs/eod`
4. Polls `GET /api/v1/runs/:id` until status is `executed`, `awaiting_approval`, or `failed`
5. Exits `0` on success (`executed` or `awaiting_approval`), `1` on `failed` or timeout

Optional environment variables for the smoke script (defaults match `env.example` / your `.env`):

| Variable | Default |
|----------|---------|
| `API_BASE_URL` | `http://localhost:8080` |
| `ADMIN_USERNAME` | `admin` |
| `ADMIN_PASSWORD` | `admin123` |
| `HEALTH_TIMEOUT` | `120` (seconds) |
| `POLL_TIMEOUT` | `300` (seconds) |
| `POLL_INTERVAL` | `2` (seconds) |

Bash requires `curl` plus `jq` or `python3` for JSON parsing.

## API E2E (`e2e_api`)

Broader than smoke: hits overview, portfolio, runs, EOD → terminal status, approvals, risk settings. Same env vars as smoke. **Requires a running Compose stack** (real Postgres/Redis/API/agents); local override uses `LLM_MODE=mock` — this is integration E2E, not production brokers/LLMs.

```powershell
# from deploy/ with stack up
powershell -ExecutionPolicy Bypass -File .\e2e_api.ps1
```

```bash
chmod +x e2e_api.sh && ./e2e_api.sh
```

## Spec gate notes (V1 + Alpaca Phase 1)

Verified against design specs via codebase review and unit tests (`go test ./...` in `services/api`, `pytest` in `services/agents/common`). **Live Docker Compose smoke was not run** on the verification host because the Docker daemon was not running (`docker ps` failed); use the smoke scripts above when Docker is available.

| Area | Status | Notes |
|------|--------|-------|
| EOD schedule + manual trigger | Implemented | **DB active strategy is authoritative** (pre-open + intraday ticks via `strategy.BuildJobSpecs`; hot-reload on activate/PATCH). `EOD_CRON` is **legacy only** (unused when a strategy is active; no active strategy → no automatic ticks). Manual: `POST /api/v1/runs/eod` (JWT) and `POST /internal/eod/run` (internal token); web **Run EOD now** on `/runs`. |
| Five agents / broker boundary | Implemented | `agent-data`, `agent-research`, `agent-decision`, `agent-portfolio`, `agent-risk` in Compose; agents proposal-only; Go submits to Alpaca Paper after risk gate or `bypass_risk`. |
| Alpaca Paper authority | Implemented (Phase 1) | `AlpacaMarketDataProvider` fetches daily bars; Go `internal/broker` submits market orders; Overview/Portfolio/Orders read Alpaca with short TTL cache; frontend tiered polling. |
| Portfolio fields | Mostly implemented | Cash/positions from Alpaca; local `stop_loss` / `take_profit` on positions when mirrored; concentration in Go risk engine. |
| Risk + execution modes | Implemented | `require_approval`, `auto_reject_breaches`, `bypass_risk`; Go `risk.Evaluate` → submit, approval, or reject per mode. |
| Compose stack | Present | `deploy/docker-compose.yml` + override: web, api, five agents, postgres, redis. |

**Known gaps (honest, not silent TBD):**

- **Alpaca SSE streaming** — Phase 2; `ALPACA_STREAM_ENABLED=false` until stream hub ships. Phase 1 uses tiered REST polling.
- **JWT expiry** — tokens are signed HS256 with `user_id` only; no `exp` claim (acceptable for single-user V1, rotate `JWT_SECRET` to invalidate).
- **Max drawdown rule** — `max_drawdown` loads into the risk engine config but is **not evaluated** in `risk.Evaluate` (default `0` = disabled); NAV peak / drawdown is not shown in the web UI yet.
- **Live E2E smoke** — not executed in final gate when Docker daemon unavailable; API integration tests with stubbed broker cover submit and approval paths.
