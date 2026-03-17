package api_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kayushkin/noteboard/internal/api"
	"github.com/kayushkin/noteboard/internal/db"
	"github.com/kayushkin/noteboard/internal/model"
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
