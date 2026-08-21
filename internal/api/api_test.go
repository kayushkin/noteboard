package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
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

// createAndRewrite makes an item and rewrites its body n times, leaving n
// revisions behind. It is the fixture for the paging tests below: they need a
// history longer than one page, and a one-revision history cannot tell a limit
// that works from a limit that is ignored.
func createAndRewrite(t *testing.T, h http.Handler, n int) string {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"type": "workspace", "title": "harness-watch memory", "body": "version 0",
	})
	req := httptest.NewRequest("POST", "/api/items", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var item model.Item
	json.NewDecoder(w.Body).Decode(&item)

	for i := 1; i <= n; i++ {
		body, _ = json.Marshal(map[string]string{"body": fmt.Sprintf("version %d", i)})
		req = httptest.NewRequest("PATCH", "/api/items/"+item.ID, bytes.NewReader(body))
		w = httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("update %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	return item.ID
}

func TestRevisionsEndpointServesAPage(t *testing.T) {
	a, cleanup := setup(t)
	defer cleanup()
	h := a.Handler()
	id := createAndRewrite(t, h, 5)

	req := httptest.NewRequest("GET", "/api/items/"+id+"/revisions?limit=2", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("revisions: %d %s", w.Code, w.Body.String())
	}
	var page []model.Revision
	json.NewDecoder(w.Body).Decode(&page)
	if len(page) != 2 {
		t.Fatalf("?limit=2 returned %d revisions", len(page))
	}

	req = httptest.NewRequest("GET", "/api/items/"+id+"/revisions?limit=2&offset=2", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var second []model.Revision
	json.NewDecoder(w.Body).Decode(&second)
	if len(second) != 2 {
		t.Fatalf("second page returned %d revisions", len(second))
	}
	if second[0].ID == page[0].ID {
		t.Fatalf("offset did not move the window: both pages start at revision %d", page[0].ID)
	}
}

func TestRevisionsEndpointStillDefaultsToTheWholeHistory(t *testing.T) {
	a, cleanup := setup(t)
	defer cleanup()
	h := a.Handler()
	id := createAndRewrite(t, h, 5)

	req := httptest.NewRequest("GET", "/api/items/"+id+"/revisions", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var all []model.Revision
	json.NewDecoder(w.Body).Decode(&all)
	if len(all) != 5 {
		t.Fatalf("unpaged request returned %d revisions, want all 5", len(all))
	}
}

// A limit that cannot be parsed must be refused, not swallowed. Swallowing it
// yields 0, and 0 on this endpoint means the whole history — so ?limit=abc
// would answer with the single most expensive response this service can produce
// while the caller believes it asked for a small one, and nothing in the reply
// would say otherwise.
func TestAnUnusableLimitIsRefusedRatherThanTreatedAsUnlimited(t *testing.T) {
	a, cleanup := setup(t)
	defer cleanup()
	h := a.Handler()
	id := createAndRewrite(t, h, 5)

	for _, q := range []string{"?limit=abc", "?limit=-1", "?offset=abc", "?offset=-1", "?limit=2&offset=oops"} {
		req := httptest.NewRequest("GET", "/api/items/"+id+"/revisions"+q, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatalf("GET /revisions%s = %d, want 400 (body %s)", q, w.Code, strings.TrimSpace(w.Body.String()))
		}
	}
}
