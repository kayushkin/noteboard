package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kayushkin/noteboard/internal/model"
	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db *sql.DB
}

func New(dbPath string) (*Store, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}

	return &Store{db: db}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS items (
			id          TEXT PRIMARY KEY,
			type        TEXT NOT NULL,
			title       TEXT NOT NULL,
			body        TEXT DEFAULT '',
			tags        TEXT DEFAULT '[]',
			priority    INTEGER DEFAULT 0,
			rank        REAL DEFAULT 0,
			status      TEXT DEFAULT 'open',
			list_id     TEXT DEFAULT '',
			due_at      DATETIME,
			parent_id   TEXT,
			links       TEXT DEFAULT '[]',
			created_by  TEXT DEFAULT '',
			created_at  DATETIME NOT NULL,
			updated_at  DATETIME NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_items_type ON items(type);
		CREATE INDEX IF NOT EXISTS idx_items_status ON items(status);
		CREATE INDEX IF NOT EXISTS idx_items_list ON items(list_id);
		CREATE INDEX IF NOT EXISTS idx_items_created_by ON items(created_by);
	`)
	if err != nil {
		return err
	}

	// Create FTS table if not exists
	var ftsExists int
	db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='items_fts'").Scan(&ftsExists)
	if ftsExists == 0 {
		_, err = db.Exec(`CREATE VIRTUAL TABLE items_fts USING fts5(title, body, content=items, content_rowid=rowid)`)
		if err != nil {
			return err
		}
		// Populate FTS from existing data
		_, err = db.Exec(`INSERT INTO items_fts(rowid, title, body) SELECT rowid, title, body FROM items`)
		if err != nil {
			return err
		}
	}

	// Triggers to keep FTS in sync
	db.Exec(`CREATE TRIGGER IF NOT EXISTS items_ai AFTER INSERT ON items BEGIN
		INSERT INTO items_fts(rowid, title, body) VALUES (new.rowid, new.title, new.body);
	END`)
	db.Exec(`CREATE TRIGGER IF NOT EXISTS items_ad AFTER DELETE ON items BEGIN
		INSERT INTO items_fts(items_fts, rowid, title, body) VALUES('delete', old.rowid, old.title, old.body);
	END`)
	db.Exec(`CREATE TRIGGER IF NOT EXISTS items_au AFTER UPDATE ON items BEGIN
		INSERT INTO items_fts(items_fts, rowid, title, body) VALUES('delete', old.rowid, old.title, old.body);
		INSERT INTO items_fts(rowid, title, body) VALUES (new.rowid, new.title, new.body);
	END`)

	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func scanItem(row interface{ Scan(...any) error }) (*model.Item, error) {
	var item model.Item
	var tagsJSON, linksJSON string
	var dueAt, parentID sql.NullString

	err := row.Scan(
		&item.ID, &item.Type, &item.Title, &item.Body,
		&tagsJSON, &item.Priority, &item.Rank, &item.Status,
		&item.ListID, &dueAt, &parentID, &linksJSON,
		&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	json.Unmarshal([]byte(tagsJSON), &item.Tags)
	if item.Tags == nil {
		item.Tags = []string{}
	}
	json.Unmarshal([]byte(linksJSON), &item.Links)
	if item.Links == nil {
		item.Links = []string{}
	}
	if dueAt.Valid {
		t, _ := time.Parse(time.RFC3339, dueAt.String)
		item.DueAt = &t
	}
	if parentID.Valid {
		item.ParentID = &parentID.String
	}

	return &item, nil
}

const itemCols = "id, type, title, body, tags, priority, rank, status, list_id, due_at, parent_id, links, created_by, created_at, updated_at"

func (s *Store) CreateItem(req *model.CreateItemRequest) (*model.Item, error) {
	now := time.Now().UTC()
	item := &model.Item{
		ID:        uuid.New().String(),
		Type:      req.Type,
		Title:     req.Title,
		Tags:      []string{},
		Links:     []string{},
		Status:    "open",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if req.Body != nil {
		item.Body = *req.Body
	}
	if req.Tags != nil {
		item.Tags = req.Tags
	}
	if req.Priority != nil {
		item.Priority = *req.Priority
	}
	if req.Rank != nil {
		item.Rank = *req.Rank
	}
	if req.Status != nil {
		item.Status = *req.Status
	}
	if req.ListID != nil {
		item.ListID = *req.ListID
	}
	if req.DueAt != nil {
		item.DueAt = req.DueAt
	}
	if req.ParentID != nil {
		item.ParentID = req.ParentID
	}
	if req.Links != nil {
		item.Links = req.Links
	}
	if req.CreatedBy != nil {
		item.CreatedBy = *req.CreatedBy
	}

	tagsJSON, _ := json.Marshal(item.Tags)
	linksJSON, _ := json.Marshal(item.Links)

	var dueAt interface{}
	if item.DueAt != nil {
		dueAt = item.DueAt.Format(time.RFC3339)
	}

	_, err := s.db.Exec(
		"INSERT INTO items ("+itemCols+") VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
		item.ID, item.Type, item.Title, item.Body,
		string(tagsJSON), item.Priority, item.Rank, item.Status,
		item.ListID, dueAt, item.ParentID, string(linksJSON),
		item.CreatedBy, item.CreatedAt, item.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Store) GetItem(id string) (*model.Item, error) {
	row := s.db.QueryRow("SELECT "+itemCols+" FROM items WHERE id = ?", id)
	return scanItem(row)
}

func (s *Store) UpdateItem(id string, req *model.UpdateItemRequest) (*model.Item, error) {
	existing, err := s.GetItem(id)
	if err != nil {
		return nil, err
	}

	sets := []string{}
	args := []interface{}{}

	if req.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *req.Title)
	}
	if req.Body != nil {
		sets = append(sets, "body = ?")
		args = append(args, *req.Body)
	}
	if req.HasTags {
		tags := req.Tags
		if tags == nil {
			tags = []string{}
		}
		j, _ := json.Marshal(tags)
		sets = append(sets, "tags = ?")
		args = append(args, string(j))
	}
	if req.Priority != nil {
		sets = append(sets, "priority = ?")
		args = append(args, *req.Priority)
	}
	if req.Rank != nil {
		sets = append(sets, "rank = ?")
		args = append(args, *req.Rank)
	}
	if req.Status != nil {
		sets = append(sets, "status = ?")
		args = append(args, *req.Status)
	}
	if req.ListID != nil {
		sets = append(sets, "list_id = ?")
		args = append(args, *req.ListID)
	}
	if req.DueAt != nil {
		sets = append(sets, "due_at = ?")
		args = append(args, req.DueAt.Format(time.RFC3339))
	}
	if req.ParentID != nil {
		sets = append(sets, "parent_id = ?")
		args = append(args, *req.ParentID)
	}
	if req.HasLinks {
		links := req.Links
		if links == nil {
			links = []string{}
		}
		j, _ := json.Marshal(links)
		sets = append(sets, "links = ?")
		args = append(args, string(j))
	}

	if len(sets) == 0 {
		return existing, nil
	}

	now := time.Now().UTC()
	sets = append(sets, "updated_at = ?")
	args = append(args, now)
	args = append(args, id)

	_, err = s.db.Exec("UPDATE items SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	if err != nil {
		return nil, err
	}
	return s.GetItem(id)
}

func (s *Store) DeleteItem(id string, hard bool) error {
	if hard {
		_, err := s.db.Exec("DELETE FROM items WHERE id = ?", id)
		return err
	}
	_, err := s.db.Exec("UPDATE items SET status = 'archived', updated_at = ? WHERE id = ?", time.Now().UTC(), id)
	return err
}

type ListParams struct {
	Type      string
	Tag       string
	Status    string
	ListID    string
	CreatedBy string
	Limit     int
	Offset    int
	Sort      string
}

func (s *Store) ListItems(p ListParams) ([]*model.Item, error) {
	where := []string{}
	args := []interface{}{}

	if p.Type != "" {
		where = append(where, "type = ?")
		args = append(args, p.Type)
	}
	if p.Tag != "" {
		where = append(where, "EXISTS (SELECT 1 FROM json_each(tags) WHERE json_each.value = ?)")
		args = append(args, p.Tag)
	}
	if p.Status != "" {
		where = append(where, "status = ?")
		args = append(args, p.Status)
	}
	if p.ListID != "" {
		where = append(where, "list_id = ?")
		args = append(args, p.ListID)
	}
	if p.CreatedBy != "" {
		where = append(where, "created_by = ?")
		args = append(args, p.CreatedBy)
	}

	q := "SELECT " + itemCols + " FROM items"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}

	sort := "created_at DESC"
	switch p.Sort {
	case "rank":
		sort = "rank ASC"
	case "priority":
		sort = "priority DESC"
	case "updated_at":
		sort = "updated_at DESC"
	case "created_at":
		sort = "created_at DESC"
	}
	q += " ORDER BY " + sort

	if p.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", p.Limit)
	} else {
		q += " LIMIT 100"
	}
	if p.Offset > 0 {
		q += fmt.Sprintf(" OFFSET %d", p.Offset)
	}

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*model.Item
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if items == nil {
		items = []*model.Item{}
	}
	return items, nil
}

func (s *Store) Rerank(items []model.RerankItem) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("UPDATE items SET rank = ?, updated_at = ? WHERE id = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UTC()
	for _, item := range items {
		_, err := stmt.Exec(item.Rank, now, item.ID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListLists() ([]model.ListInfo, error) {
	rows, err := s.db.Query("SELECT list_id, COUNT(*) as count FROM items WHERE list_id != '' AND status != 'archived' GROUP BY list_id ORDER BY list_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lists []model.ListInfo
	for rows.Next() {
		var li model.ListInfo
		if err := rows.Scan(&li.ListID, &li.Count); err != nil {
			return nil, err
		}
		lists = append(lists, li)
	}
	if lists == nil {
		lists = []model.ListInfo{}
	}
	return lists, nil
}

func (s *Store) ListTags() ([]model.TagInfo, error) {
	rows, err := s.db.Query("SELECT j.value as tag, COUNT(*) as count FROM items, json_each(items.tags) as j WHERE items.status != 'archived' GROUP BY j.value ORDER BY count DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []model.TagInfo
	for rows.Next() {
		var ti model.TagInfo
		if err := rows.Scan(&ti.Tag, &ti.Count); err != nil {
			return nil, err
		}
		tags = append(tags, ti)
	}
	if tags == nil {
		tags = []model.TagInfo{}
	}
	return tags, nil
}

type SearchParams struct {
	Query  string
	Type   string
	Tag    string
	Status string
	Limit  int
}

func (s *Store) Search(p SearchParams) ([]*model.Item, error) {
	where := []string{"items.rowid IN (SELECT rowid FROM items_fts WHERE items_fts MATCH ?)"}
	args := []interface{}{p.Query}

	if p.Type != "" {
		where = append(where, "type = ?")
		args = append(args, p.Type)
	}
	if p.Tag != "" {
		where = append(where, "EXISTS (SELECT 1 FROM json_each(tags) WHERE json_each.value = ?)")
		args = append(args, p.Tag)
	}
	if p.Status != "" {
		where = append(where, "status = ?")
		args = append(args, p.Status)
	}

	limit := 50
	if p.Limit > 0 {
		limit = p.Limit
	}

	q := fmt.Sprintf("SELECT %s FROM items WHERE %s ORDER BY created_at DESC LIMIT %d", itemCols, strings.Join(where, " AND "), limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*model.Item
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if items == nil {
		items = []*model.Item{}
	}
	return items, nil
}

func (s *Store) ItemCount() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM items WHERE status != 'archived'").Scan(&count)
	return count, err
}
