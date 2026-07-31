package api_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kayushkin/noteboard/internal/api"
	"github.com/kayushkin/noteboard/internal/db"
	"github.com/kayushkin/noteboard/model"
)

func setup(t *testing.T) (*api.API, func()) {
	t.Helper()
	dir := t.TempDir()
	store, err := db.New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(dir, 0755)
	a := api.New(store)
	return a, func() { store.Close() }
}

func TestHealthEndpoint(t *testing.T) {
	a, cleanup := setup(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Fatalf("expected ok, got %v", resp["status"])
	}
}

func TestCRUD(t *testing.T) {
	a, cleanup := setup(t)
	defer cleanup()
	h := a.Handler()

	// Create
	body, _ := json.Marshal(model.CreateItemRequest{Type: "note", Title: "Test Note"})
	req := httptest.NewRequest("POST", "/api/items", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var item model.Item
	json.NewDecoder(w.Body).Decode(&item)
	if item.Title != "Test Note" {
		t.Fatalf("expected title 'Test Note', got %q", item.Title)
	}

	// Get
	req = httptest.NewRequest("GET", "/api/items/"+item.ID, nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("get: expected 200, got %d", w.Code)
	}

	// List
	req = httptest.NewRequest("GET", "/api/items", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("list: expected 200, got %d", w.Code)
	}
	var items []model.Item
	json.NewDecoder(w.Body).Decode(&items)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	// Update
	body, _ = json.Marshal(map[string]string{"title": "Updated"})
	req = httptest.NewRequest("PATCH", "/api/items/"+item.ID, bytes.NewReader(body))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("update: expected 200, got %d", w.Code)
	}

	// Delete (soft)
	req = httptest.NewRequest("DELETE", "/api/items/"+item.ID, nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("delete: expected 200, got %d", w.Code)
	}
}

func TestSearch(t *testing.T) {
	a, cleanup := setup(t)
	defer cleanup()
	h := a.Handler()

	body, _ := json.Marshal(model.CreateItemRequest{Type: "note", Title: "Searchable Thing"})
	req := httptest.NewRequest("POST", "/api/items", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	req = httptest.NewRequest("GET", "/api/search?q=Searchable", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("search: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var items []model.Item
	json.NewDecoder(w.Body).Decode(&items)
	if len(items) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(items))
	}
}

func TestTags(t *testing.T) {
	a, cleanup := setup(t)
	defer cleanup()
	h := a.Handler()

	tags := []string{"go", "backend"}
	body, _ := json.Marshal(map[string]interface{}{"type": "note", "title": "Tagged", "tags": tags})
	req := httptest.NewRequest("POST", "/api/items", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	req = httptest.NewRequest("GET", "/api/tags", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("tags: expected 200, got %d", w.Code)
	}
	var tagInfos []model.TagInfo
	json.NewDecoder(w.Body).Decode(&tagInfos)
	if len(tagInfos) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tagInfos))
	}
}

// TestExcludeTag covers the server-side exclude_tag list filter, which lets a
// consumer drop machine-generated noise (e.g. autoworker dispatch cards) from
// the open-todo query before the limit is applied.
func TestExcludeTag(t *testing.T) {
	a, cleanup := setup(t)
	defer cleanup()
	h := a.Handler()

	mk := func(title string, tags []string) {
		body, _ := json.Marshal(map[string]interface{}{"type": "todo", "title": title, "tags": tags})
		req := httptest.NewRequest("POST", "/api/items", bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 201 {
			t.Fatalf("create %q: expected 201, got %d: %s", title, w.Code, w.Body.String())
		}
	}
	mk("Real human todo", []string{"home"})
	mk("autoworker dispatch", []string{"autoworker", "anthropic"})
	mk("scheduler noise", []string{"autoworker", "scheduler"})

	list := func(query string) []model.Item {
		req := httptest.NewRequest("GET", "/api/items?type=todo&status=open"+query, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("list %q: expected 200, got %d", query, w.Code)
		}
		var items []model.Item
		json.NewDecoder(w.Body).Decode(&items)
		return items
	}

	if got := list(""); len(got) != 3 {
		t.Fatalf("no filter: expected 3 todos, got %d", len(got))
	}

	got := list("&exclude_tag=autoworker")
	if len(got) != 1 {
		t.Fatalf("exclude autoworker: expected 1 todo, got %d", len(got))
	}
	if got[0].Title != "Real human todo" {
		t.Fatalf("exclude autoworker: expected the human todo, got %q", got[0].Title)
	}

	// Multiple exclude_tag values are AND-combined (item must lack all of them).
	if got := list("&exclude_tag=autoworker&exclude_tag=home"); len(got) != 0 {
		t.Fatalf("exclude autoworker+home: expected 0 todos, got %d", len(got))
	}
}

// TestPostingAnUnknownParentIDIsABadRequest. The caller mistyped an id; the
// request will never succeed as written. A 500 would tell it to retry, and the
// child it wanted would sit outside the hold and the ceiling of the tree it
// meant to join.
func TestPostingAnUnknownParentIDIsABadRequest(t *testing.T) {
	a, cleanup := setup(t)
	defer cleanup()

	missing := "a88bca06-3c94-4b7a-9a2d-59c2d1a3e9d1"
	body, _ := json.Marshal(model.CreateItemRequest{
		Type: "todo", Title: "child of nothing", ParentID: &missing,
	})
	req := httptest.NewRequest("POST", "/api/items", bytes.NewReader(body))
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("posting an unknown parent_id returned %d, want 400: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), missing) {
		t.Errorf("the error does not name the id that was wrong: %s", w.Body.String())
	}
}
