# Noteboard

Unified notes, todos, and ranking lists. API-first, agent-friendly.

## Stack
- Go + SQLite (`modernc.org/sqlite` — pure Go, no cgo, FTS5 built in)
- REST API
- Markdown bodies, JSON tags

## Build & deploy
```sh
make build          # default flags, CGO_ENABLED=0
make test
./scripts/e2e-smoke.sh   # boots the binary on a throwaway DB and drives every route
./deploy.sh              # build → test → smoke → install → restart → verify live
```
`deploy.sh` installs `deploy/noteboard.service` (a `--user` unit, no sudo) and
reads the binary path, port and database path back out of it, so there is no
second copy of those values to drift.

## API
- `GET /api/items` — list (filter by type, tag, status, created_by)
- `POST /api/items` — create
- `PATCH /api/items/:id` — update
- `DELETE /api/items/:id` — archive
- `POST /api/items/rerank` — reorder items in a list
- `GET /api/lists` — named lists
- `GET /api/tags` — all unique tags
- `GET /api/search?q=` — full-text search
- `GET /health` — health check
