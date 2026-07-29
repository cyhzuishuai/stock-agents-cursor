# Docker Compose deployment

## Authority (Phase 1)

Alpaca Paper is the source of truth for cash, equity, positions, orders, and fill prices. Go API is the only component that talks to Alpaca Trading. Postgres stores runs, proposals, approvals, and order mirrors for audit — not fill authority. See `docs/superpowers/specs/2026-07-28-alpaca-paper-authority-design.md`.

## Cadence (strategy-driven)

Automatic runs come from the **active strategy** (pre-open + intraday jobs; hot-reload on activate/PATCH). Manual run: Web **Run now** or `POST /api/v1/runs/trigger` / `POST /internal/runs/trigger`. Spec: `docs/superpowers/specs/2026-07-28-strategy-scheduler-runs-observability-design.md`.

## Environment file (`.env`)

**Path:** `deploy/.env` (same folder as this README / `docker-compose.yml`)

```bash
# from repo root
cp deploy/env.example deploy/.env
```

Then edit secrets (`JWT_SECRET`, `ADMIN_PASSWORD`, `LLM_API_KEY`, `ALPACA_API_KEY`, `ALPACA_API_SECRET`, `FINNHUB_API_KEY`, `WEB_SEARCH_API_KEY`, …).

| Variable | Notes |
|----------|-------|
| `ALPACA_API_KEY` / `ALPACA_API_SECRET` | Required for Paper trading and market data; server-side only |
| `ALPACA_BASE_URL` | Default `https://paper-api.alpaca.markets` |
| `ALPACA_DATA_BASE_URL` | Default `https://data.alpaca.markets` (bars + symbol-search snapshots) |
| `MARKET_DATA_PROVIDER` | Default `alpaca`; set `free` (Yahoo) only as dev fallback without keys |
| `INITIAL_CASH` | Offline/test seed only; live cash comes from Alpaca Paper account |
| `AGENT_RUNTIME_URL` | Internal URL for Go → agent-runtime (Compose default `http://agent-runtime:8001`) |
| `AGENT_CHECKPOINT_SQLITE_PATH` | SQLite checkpoint file path (Compose default `/data/checkpoints.sqlite`; persisted via `agent_runtime_checkpoints` volume) |
| `INTERNAL_RUN_TOKEN` | Required for `POST /internal/runs/trigger` (`X-Internal-Token` header) |
| `LLM_MODE` | `mock` for local/CI (override sets this); `live` for real LLM |
| `LLM_PRIMARY_*` / `LLM_FALLBACK_*` | Recommended for live: Volcengine Ark primary + MiniMax fallback (HTTP failure only) |
| `LLM_API_KEY` / `LLM_BASE_URL` / `LLM_MODEL` | Legacy single-provider when `LLM_PRIMARY_*` unset |
| `FINNHUB_API_KEY` | Optional; news tool degrades gracefully when missing |
| `WEB_SEARCH_ENABLED` | Default `true`; set `false` to disable Tavily web search tool |
| `WEB_SEARCH_PROVIDER` | Default `tavily` |
| `WEB_SEARCH_API_KEY` | Optional; web search tool degrades when missing |
| `MAX_TOOL_ROUNDS_ANALYST` | Default `8` |
| `MAX_TOOL_ROUNDS_PORTFOLIO` | Default `8` |
| `LANGSMITH_TRACING` | Default `false`; set `true`/`1`/`yes` with `LANGSMITH_API_KEY` to export traces (fail-open) |
| `LANGSMITH_API_KEY` / `LANGSMITH_PROJECT` / `LANGSMITH_ENDPOINT` | Optional LangSmith; view runs in project UI when enabled |

Settings watchlist symbol search (`GET /api/v1/symbols/search`) uses Alpaca assets + snapshots (not Yahoo). Requires `ALPACA_API_KEY` / `ALPACA_API_SECRET`.

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
| agent-runtime | http://localhost:8001/healthz |

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

Health check: `curl http://localhost:8080/healthz` and optionally `curl http://localhost:8001/healthz` (agent-runtime).

## Workflow smoke test

After the stack is up, run the integration smoke script from the repo root or `deploy/`:

**Linux / macOS / Git Bash:**

```bash
chmod +x deploy/smoke_run.sh
./deploy/smoke_run.sh
```

**Windows (PowerShell):**

```powershell
.\deploy\smoke_run.ps1
```

The script:

1. Waits for `GET /healthz` (`status: ok`)
2. Logs in via `POST /api/v1/auth/login` and obtains a JWT
3. Triggers a **manual** workflow run via `POST /api/v1/runs/trigger`
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
| `EOD_TRADE_DATE` | *(e2e only)* default **US/Eastern today**; set to force a specific `YYYY-MM-DD` |

Bash requires `curl` plus `jq` or `python3` for JSON parsing.

## API E2E (`e2e_api`)

Broader than smoke: hits **Alpaca-backed** overview / portfolio / orders, strategies, runs, **manual workflow run** (default `trade_date` = **US/Eastern today**; override with `EOD_TRADE_DATE`) → terminal status, approvals, settings, and stream endpoint (200 if enabled, **503** when `ALPACA_STREAM_ENABLED=false`). Same env vars as smoke. **Requires a running Compose stack** rebuilt with current images; local override uses `LLM_MODE=mock` on **agent-runtime**. **`ALPACA_API_KEY` / `ALPACA_API_SECRET` must be set** in `deploy/.env` or overview/portfolio/orders return `503 alpaca not configured`. A run may submit **Paper** market orders when Go risk / `bypass_risk` allows.

```powershell
# from deploy/ with stack up
powershell -ExecutionPolicy Bypass -File .\e2e_api.ps1
```

```bash
chmod +x e2e_api.sh && ./e2e_api.sh
```

**Last verified (local):** 2026-07-28 — `e2e_api.ps1` **17/17 PASS** against Compose (`MARKET_DATA_PROVIDER=alpaca`, Paper keys set, stream disabled → 503). Sample manual run terminal status `executed`.

## Live LLM E2E (`e2e_api_live_llm`)

Optional long-running check with `LLM_MODE=live` (rebuild `agent-runtime` so Compose picks up `LLM_PRIMARY_*` / `LLM_FALLBACK_*` / optional `LANGSMITH_*`). Polls until terminal; analyst/portfolio step timeouts are ~600s / ~480s.

```powershell
# from repo root, stack up with live env
powershell -File .\deploy\e2e_api_live_llm.ps1
```

Last verified (local): 2026-07-28 — live e2e **18/18 PASS** (`run_id=23`, Ark primary, `fallback_used_any=false`).

## Spec gate notes (V1 + strategy cadence + Alpaca Phase 1 + agent-runtime P0–P3)

Verified against design specs via unit tests (`go test ./...` in `services/api`, `pytest` in `services/agents`, Vitest in `apps/web`) and Compose E2E.

| Area | Status | Notes |
|------|--------|-------|
| Strategy schedule + manual trigger | Implemented | **DB active strategy is authoritative** (pre-open + intraday; hot-reload). Manual: `POST /api/v1/runs/trigger` / `POST /internal/runs/trigger`; web **Run now**. |
| agent-runtime / broker boundary | Implemented | Analyst + Portfolio **plan/act/reflect**; ModelRouter; Go injects handoff/memory; Go submits to Alpaca after risk gate or `bypass_risk`. |
| Runs observability | Implemented | `trace.events[]` timeline + handoff summary; optional LangSmith (default off). |
| Alpaca Paper authority | Implemented (Phase 1 + SSE client) | Bars, broker submit, Overview/Portfolio/Orders from Alpaca; tiered polling + optional JWT SSE. **E2E covered.** |
| Portfolio fields | Mostly implemented | Cash/positions from Alpaca; local stop/TP when mirrored; concentration in Go risk. |
| Risk + execution modes | Implemented | `require_approval`, `auto_reject_breaches`, `bypass_risk`. |
| Compose stack | Present | web, api, agent-runtime, postgres, redis. |

**Known gaps (honest, not silent TBD):**

- **Alpaca WS upstream pump** — Phase 2 hub + JWT SSE endpoints + web `useMarketStream` are in tree. Default remains `ALPACA_STREAM_ENABLED=false` (REST polling only; E2E asserts **503**). Enabling the flag starts the hub, but live quote fan-in from Alpaca WebSocket still needs a pump calling `PublishQuote` for real ticks. Manual: `curl -N -H "Authorization: Bearer $TOKEN" "$API/api/v1/stream/market"`.
- **JWT expiry** — tokens are signed HS256 with `user_id` only; no `exp` claim (acceptable for single-user V1, rotate `JWT_SECRET` to invalidate).
- **Max drawdown rule** — `max_drawdown` loads into the risk engine config but is **not evaluated** in `risk.Evaluate` (default `0` = disabled); NAV peak / drawdown is not shown in the web UI yet.
