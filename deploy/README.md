# Docker Compose deployment

## Environment file (`.env`)

**Path:** `deploy/.env` (same folder as this README / `docker-compose.yml`)

```bash
# from repo root
cp deploy/env.example deploy/.env
```

Then edit secrets (`JWT_SECRET`, `ADMIN_PASSWORD`, `LLM_API_KEY`, …).

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

## Spec gate notes (V1)

Verified against design spec §1.1 via codebase review and unit tests (`go test ./...` in `services/api`, `pytest` in `services/agents/common`). **Live Docker Compose smoke was not run** on the verification host because the Docker daemon was not running (`docker ps` failed); use the smoke scripts above when Docker is available.

| Area | Status | Notes |
|------|--------|-------|
| EOD schedule + manual trigger | Implemented | Cron `30 16 * * 1-5` US/Eastern in API scheduler; `POST /api/v1/runs/eod` (JWT) and `POST /internal/eod/run` (internal token); web **Run EOD now** on `/runs`. |
| Five agents / ledger boundary | Implemented | `agent-data`, `agent-research`, `agent-decision`, `agent-portfolio`, `agent-risk` in Compose; ledger fills only via Go `ledger.Service.ApplyFill`. |
| Portfolio fields | Mostly implemented | Cash, positions, weights, stop-loss / take-profit on positions and UI; concentration in Go risk engine. |
| Risk auto vs approval | Implemented | Go `risk.Evaluate` → auto fill or `awaiting_approval` + approval rows with `breach_reasons`. |
| Compose stack | Present | `deploy/docker-compose.yml` + override: web, api, five agents, postgres, redis. |

**Known gaps (honest, not silent TBD):**

- **Alpaca market data** — `AlpacaMarketDataProvider` is a stub (`NotImplementedError`); default/local path uses `MARKET_DATA_PROVIDER=free` (Yahoo Finance).
- **JWT expiry** — tokens are signed HS256 with `user_id` only; no `exp` claim (acceptable for single-user V1, rotate `JWT_SECRET` to invalidate).
- **Max drawdown rule** — `max_drawdown` loads into the risk engine config but is **not evaluated** in `risk.Evaluate` (default `0` = disabled); NAV peak / drawdown is not shown in the web UI yet.
- **Live E2E smoke** — not executed in final gate when Docker daemon unavailable; API integration tests with stubbed agents cover auto-exec and approval paths.
