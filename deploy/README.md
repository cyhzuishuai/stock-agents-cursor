# Docker Compose deployment

## Browser ports (local override)

| Service | URL |
|---------|-----|
| Web UI | http://localhost:3000 |
| API | http://localhost:8080 |

The web app calls the API via `NEXT_PUBLIC_API_BASE_URL` (default `http://localhost:8080`), which must be reachable from the browser—not an internal Docker hostname like `http://api:8080`.

## Run the stack

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

Optional environment variables (defaults match `env.example`):

| Variable | Default |
|----------|---------|
| `API_BASE_URL` | `http://localhost:8080` |
| `ADMIN_USERNAME` | `admin` |
| `ADMIN_PASSWORD` | `admin123` |
| `HEALTH_TIMEOUT` | `120` (seconds) |
| `POLL_TIMEOUT` | `300` (seconds) |
| `POLL_INTERVAL` | `2` (seconds) |

Bash requires `curl` plus `jq` or `python3` for JSON parsing.
