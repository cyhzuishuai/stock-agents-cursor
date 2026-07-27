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
