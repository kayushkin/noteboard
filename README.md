# Noteboard

Unified notes, todos, and ranking lists. API-first, agent-friendly.

## Stack
- Go + SQLite
- REST API
- Markdown bodies, JSON tags

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
