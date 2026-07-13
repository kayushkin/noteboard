package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/kayushkin/noteboard/internal/db"
	"github.com/kayushkin/noteboard/internal/model"
)

type API struct {
	store *db.Store
}

func New(store *db.Store) *API {
	return &API{store: store}
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", a.health)
	mux.HandleFunc("/api/items/rerank", a.rerank)
	mux.HandleFunc("/api/items/", a.itemByID)
	mux.HandleFunc("/api/items", a.items)
	mux.HandleFunc("/api/lists", a.lists)
	mux.HandleFunc("/api/tags", a.tags)
	mux.HandleFunc("/api/search", a.search)
	return cors(mux)
}

func cors(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	count, _ := a.store.ItemCount()
	writeJSON(w, 200, map[string]interface{}{"status": "ok", "items": count})
}

func (a *API) items(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		offset, _ := strconv.Atoi(q.Get("offset"))
		items, err := a.store.ListItems(db.ListParams{
			Type:           q.Get("type"),
			Tag:            q.Get("tag"),
			ExcludeTags:    q["exclude_tag"],
			Status:         q.Get("status"),
			ListID:         q.Get("list_id"),
			CreatedBy:      q.Get("created_by"),
			ParentID:       q.Get("parent_id"),
			IncludeDeleted: q.Get("include_deleted") == "true",
			Limit:          limit,
			Offset:         offset,
			Sort:           q.Get("sort"),
		})
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, items)
	case "POST":
		var req model.CreateItemRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, 400, "invalid JSON")
			return
		}
		if err := req.Validate(); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		item, err := a.store.CreateItem(&req)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, 201, item)
	default:
		writeError(w, 405, "method not allowed")
	}
}

func (a *API) itemByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/items/")
	if id == "" {
		writeError(w, 400, "missing id")
		return
	}

	// Sub-resources: /api/items/{id}/revisions, /api/items/{id}/restore.
	if base, action, found := strings.Cut(id, "/"); found {
		a.itemAction(w, r, base, action)
		return
	}

	switch r.Method {
	case "GET":
		// A deleted item is not found unless asked for by name — the caller has
		// to say it wants a tombstone.
		var item *model.Item
		var err error
		if r.URL.Query().Get("include_deleted") == "true" {
			item, err = a.store.GetItemIncludingDeleted(id)
		} else {
			item, err = a.store.GetItem(id)
		}
		if err != nil {
			writeError(w, 404, "not found")
			return
		}
		writeJSON(w, 200, item)
	case "PATCH":
		var req model.UpdateItemRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, 400, "invalid JSON")
			return
		}
		item, err := a.store.UpdateItem(id, &req)
		if err != nil {
			writeError(w, 404, "not found")
			return
		}
		writeJSON(w, 200, item)
	case "DELETE":
		// Default is reversible: deleted_at is stamped and the row stays.
		// ?hard=true purges, and even that snapshots into item_revisions first.
		hard := r.URL.Query().Get("hard") == "true"
		if err := a.store.DeleteItem(id, hard); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})
	default:
		writeError(w, 405, "method not allowed")
	}
}

// itemAction serves the item sub-resources that make every mutation reversible:
// the change log, and the undo.
func (a *API) itemAction(w http.ResponseWriter, r *http.Request, id, action string) {
	switch {
	case action == "revisions" && r.Method == "GET":
		revisions, err := a.store.ListRevisions(id)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, revisions)
	case action == "restore" && r.Method == "POST":
		item, err := a.store.RestoreItem(id)
		if err != nil {
			writeError(w, 404, "not found")
			return
		}
		writeJSON(w, 200, item)
	default:
		writeError(w, 404, "no such item action: "+action)
	}
}

func (a *API) rerank(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, 405, "method not allowed")
		return
	}
	var req model.RerankRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if err := a.store.Rerank(req.Items); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (a *API) lists(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		lists, err := a.store.ListLists()
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, lists)
	case "POST":
		var req struct {
			ListID string `json:"list_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ListID == "" {
			writeError(w, 400, "list_id is required")
			return
		}
		writeJSON(w, 201, map[string]string{"list_id": req.ListID, "status": "created"})
	default:
		writeError(w, 405, "method not allowed")
	}
}

func (a *API) tags(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, 405, "method not allowed")
		return
	}
	tags, err := a.store.ListTags()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, tags)
}

func (a *API) search(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, 405, "method not allowed")
		return
	}
	q := r.URL.Query()
	query := q.Get("q")
	if query == "" {
		writeError(w, 400, "q parameter is required")
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	items, err := a.store.Search(db.SearchParams{
		Query:  query,
		Type:   q.Get("type"),
		Tag:    q.Get("tag"),
		Status: q.Get("status"),
		Limit:  limit,
	})
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, items)
}
