# Task 9 Report: Deploy, env, docs, retire five agents

**Branch:** `feature/agent-runtime-tool-loop`  
**Status:** Done

## Summary

Compose stack switched from five single-shot agent services to one **agent-runtime** (`services/agents/runtime/Dockerfile`, port 8001). Go API now depends on `AGENT_RUNTIME_URL=http://agent-runtime:8001`; legacy `AGENT_*_URL` variables removed from compose and `env.example`.

## Changes

| Area | Action |
|------|--------|
| `deploy/docker-compose.yml` | Added `agent-runtime`; removed `agent-data/research/decision/portfolio/risk`; API env + `depends_on` updated |
| `deploy/docker-compose.override.yml` | Publish `:8001`, `LLM_MODE=mock` on agent-runtime |
| `deploy/env.example` | `AGENT_RUNTIME_URL`, tool-loop keys (`FINNHUB_*`, `WEB_SEARCH_*`, `MAX_TOOL_ROUNDS_*`, `LLM_MODE`) |
| `deploy/e2e_api.{ps1,sh}` | Comments updated (no five-agent assumptions in logic) |
| Docs | `product-overview.md`, `eod-workflow-flowchart.md`, `deploy/README.md`, root `README.md` → analyst→portfolio tool-loop, Go risk final |

Old agent Dockerfiles remain in tree (unused by compose).

## Verification

- `docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.override.yml config` — **PASS** (with temp `deploy/.env` copied from `env.example`; worktree has no committed `.env`)
- Grep `deploy/` for `AGENT_DATA_URL`, `agent-data`, etc. — **none found**
- Full `docker compose up --build` / E2E not run (per task scope)

## Not done

- No push
- No live stack smoke / `e2e_api` re-run against rebuilt images
