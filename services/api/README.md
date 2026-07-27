# API service

Go HTTP API for paper-trading agents (auth, portfolio, workflows).

## Prerequisites

- Go 1.22+
- **PostgreSQL** — `db.Connect` uses Postgres; a running instance is required to start the server.

## Environment

Copy variables from [`deploy/env.example`](../../deploy/env.example). Minimum to run locally:

| Variable | Required | Notes |
|----------|----------|-------|
| `DATABASE_URL` | yes | Postgres DSN, e.g. `postgres://stock:stock@localhost:5432/stock?sslmode=disable` |
| `JWT_SECRET` | yes | Signing key for auth tokens |
| `ADMIN_USERNAME` | no | Default `admin` |
| `ADMIN_PASSWORD` | no | Default `admin123` |
| `INITIAL_CASH` | no | Paper account starting cash (default `100000`) |
| `WATCHLIST` | no | Comma-separated symbols |
| `RISK_*` | no | Risk rule defaults (see env.example) |
| `API_ADDR` | no | Listen address (default `:8080`) |

On startup the process **migrates** schema, **seeds** admin user / paper account / watchlist / risk configs (idempotent), then serves routes including `/healthz` and `/api/v1/auth/*`.

## Run locally

From this directory (`services/api`):

```powershell
# PowerShell — set env then start
$env:DATABASE_URL = "postgres://stock:stock@localhost:5432/stock?sslmode=disable"
$env:JWT_SECRET = "change-me"
go run ./cmd/api
```

```bash
# bash
export DATABASE_URL="postgres://stock:stock@localhost:5432/stock?sslmode=disable"
export JWT_SECRET="change-me"
go run ./cmd/api
```

Health check: `GET http://localhost:8080/healthz`

Login: `POST http://localhost:8080/api/v1/auth/login` with JSON `{"username":"admin","password":"admin123"}` (or your `ADMIN_*` values).

## Build & test

```bash
go build ./...
go test ./...
```

Tests use in-memory SQLite and do not require Postgres.
