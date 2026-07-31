package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kayushkin/noteboard/internal/db"
	"github.com/kayushkin/noteboard/model"
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
			IncludeHeld:    q.Get("include_held") == "true",
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
			// A parent_id naming no item is the caller's mistake, not the
			// store's failure, and it has to read as one: a 500 says "try
			// again" about a request that will never succeed as written, and
			// the caller keeps the id it got wrong.
			if errors.Is(err, db.ErrUnknownParent) {
				writeError(w, 400, err.Error())
				return
			}
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
		if err := req.Validate(); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		item, err := a.store.UpdateItem(id, &req)
		if err != nil {
			// A rejected schedule is a bad request, not a missing item. Collapsing
			// every store error to 404 would report "not found" for an item that is
			// plainly there, and hide the one message that says what is wrong with
			// the rule.
			if _, missing := a.store.GetItem(id); missing != nil {
				writeError(w, 404, "not found")
				return
			}
			writeError(w, 400, err.Error())
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

// itemAction serves the item sub-resources: the change log, the undo, and the
// recurrence preview.
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
	case action == "hold" && r.Method == "POST":
		// Body is optional: holding without a stated reason is allowed, because a
		// gate that is annoying to close is a gate that stays open.
		var req struct {
			Reason string `json:"reason"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		item, err := a.store.HoldItem(id, req.Reason)
		if err != nil {
			writeError(w, 404, "not found")
			return
		}
		writeJSON(w, 200, item)
	case action == "unhold" && r.Method == "POST":
		item, err := a.store.UnholdItem(id)
		if err != nil {
			writeError(w, 404, "not found")
			return
		}
		writeJSON(w, 200, item)
	case action == "occurrences" && r.Method == "GET":
		a.occurrences(w, r, id)
	default:
		writeError(w, 404, "no such item action: "+action)
	}
}

// occurrences expands an item's rule and answers the only question that matters
// when you write one: "so when does this actually fire?"
//
// A recurrence rule is uniquely bad at failing visibly — BYDAY=3TU and
// BYDAY=TU;BYSETPOS=3 both look plausible, and a wrong one does not error, it
// just quietly reminds you on the wrong Tuesday, which you find out about a month
// later. This endpoint makes the rule's behaviour checkable at the moment you
// write it rather than at the moment it fails.
//
// Both due occurrences and nag firings are returned, because they are separately
// wrong in different ways.
func (a *API) occurrences(w http.ResponseWriter, r *http.Request, id string) {
	item, err := a.store.GetItem(id)
	if err != nil {
		writeError(w, 404, "not found")
		return
	}
	if item.Schedule == nil {
		writeError(w, 400, "item has no schedule")
		return
	}

	q := r.URL.Query()
	from := time.Now()
	if v := q.Get("from"); v != "" {
		from, err = time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, 400, "from must be RFC3339: "+err.Error())
			return
		}
	}
	to := from.AddDate(1, 0, 0)
	if v := q.Get("to"); v != "" {
		to, err = time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, 400, "to must be RFC3339: "+err.Error())
			return
		}
	}

	due, err := item.Schedule.DueOccurrences(item.DueAt, from, to)
	if err != nil {
		writeError(w, 400, "expand due rule: "+err.Error())
		return
	}
	nag, err := item.Schedule.NagOccurrences(item.DueAt, from, to)
	if err != nil {
		writeError(w, 400, "expand nag rule: "+err.Error())
		return
	}

	// Capped, and the cap is REPORTED. A silently truncated preview is worse than
	// no preview: it reads as "this is when it fires" while being a lie of
	// omission, and a cadence bug hiding past item 500 would never be seen.
	const maxOccurrences = 500
	truncated := false
	if len(due) > maxOccurrences {
		due, truncated = due[:maxOccurrences], true
	}
	if len(nag) > maxOccurrences {
		nag, truncated = nag[:maxOccurrences], true
	}

	writeJSON(w, 200, map[string]any{
		"item_id":   item.ID,
		"tzid":      item.Schedule.TZID,
		"mode":      item.Schedule.EffectiveMode(),
		"from":      from.Format(time.RFC3339),
		"to":        to.Format(time.RFC3339),
		"due":       formatTimes(due),
		"nag":       formatTimes(nag),
		"truncated": truncated,
	})
}

// formatTimes renders occurrences in the rule's own zone, so a reader checking
// "is that 9am local?" can see the answer instead of doing offset arithmetic on
// a UTC timestamp.
func formatTimes(times []time.Time) []string {
	out := make([]string, 0, len(times))
	for _, t := range times {
		out = append(out, t.Format(time.RFC3339))
	}
	return out
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
		Query:       query,
		Type:        q.Get("type"),
		Tag:         q.Get("tag"),
		Status:      q.Get("status"),
		Limit:       limit,
		IncludeHeld: q.Get("include_held") == "true",
	})
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, items)
}
