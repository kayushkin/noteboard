package model

import (
	"encoding/json"
	"fmt"
	"time"
)

// ItemType values. `workspace` is an agent's durable working memory: a
// timestamped markdown document a recurring job rewrites each run. It is a
// distinct type rather than a tagged `note` on purpose — a workspace is
// rewritten forever and must never be mistakable for work to do (the todo queue
// has already been flooded once) nor clutter the notes list. Being a type also
// lets the schema enforce one workspace per job, which a convention cannot.
const (
	TypeNote      = "note"
	TypeTodo      = "todo"
	TypeRank      = "rank"
	TypeWorkspace = "workspace"
)

func ValidType(t string) bool {
	switch t {
	case TypeNote, TypeTodo, TypeRank, TypeWorkspace:
		return true
	}
	return false
}

type Item struct {
	ID       string     `json:"id"`
	Type     string     `json:"type"`
	Title    string     `json:"title"`
	Body     string     `json:"body"`
	Tags     []string   `json:"tags"`
	Priority int        `json:"priority"`
	Rank     float64    `json:"rank"`
	Status   string     `json:"status"`
	ListID   string     `json:"list_id"`
	DueAt    *time.Time `json:"due_at,omitempty"`
	ParentID *string    `json:"parent_id,omitempty"`
	Links    []string   `json:"links"`
	// DeletedAt is the reversible delete. It is deliberately NOT the `archived`
	// status: archived is a state the user chose for a live item, deletion is
	// the item being taken away. Overloading one onto the other (which DELETE
	// used to do) means a restore cannot tell them apart.
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	CreatedBy string     `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// Revision is the prior state of an item, snapshotted before every mutation.
// Workspaces are rewritten on every job run, so an agent that corrupts its own
// memory would otherwise destroy the accumulated judgment with no undo.
type Revision struct {
	ID        int64      `json:"id"`
	ItemID    string     `json:"item_id"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Tags      []string   `json:"tags"`
	Status    string     `json:"status"`
	Priority  int        `json:"priority"`
	ListID    string     `json:"list_id"`
	ParentID  *string    `json:"parent_id,omitempty"`
	Links     []string   `json:"links"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	// Reason names the mutation that produced this snapshot: update, delete,
	// or restore.
	Reason     string    `json:"reason"`
	ReplacedAt time.Time `json:"replaced_at"`
}

type CreateItemRequest struct {
	Type      string     `json:"type"`
	Title     string     `json:"title"`
	Body      *string    `json:"body,omitempty"`
	Tags      []string   `json:"tags,omitempty"`
	Priority  *int       `json:"priority,omitempty"`
	Rank      *float64   `json:"rank,omitempty"`
	Status    *string    `json:"status,omitempty"`
	ListID    *string    `json:"list_id,omitempty"`
	DueAt     *time.Time `json:"due_at,omitempty"`
	ParentID  *string    `json:"parent_id,omitempty"`
	Links     []string   `json:"links,omitempty"`
	CreatedBy *string    `json:"created_by,omitempty"`
}

func (r *CreateItemRequest) Validate() error {
	if r.Type == "" {
		return fmt.Errorf("type is required")
	}
	if !ValidType(r.Type) {
		return fmt.Errorf("type must be note, todo, rank, or workspace")
	}
	if r.Title == "" {
		return fmt.Errorf("title is required")
	}
	return nil
}

type UpdateItemRequest struct {
	Title    *string    `json:"title,omitempty"`
	Body     *string    `json:"body,omitempty"`
	Tags     []string   `json:"tags,omitempty"`
	Priority *int       `json:"priority,omitempty"`
	Rank     *float64   `json:"rank,omitempty"`
	Status   *string    `json:"status,omitempty"`
	ListID   *string    `json:"list_id,omitempty"`
	DueAt    *time.Time `json:"due_at,omitempty"`
	ParentID *string    `json:"parent_id,omitempty"`
	Links    []string   `json:"links,omitempty"`
	// Track which fields were explicitly set
	HasTags  bool `json:"-"`
	HasLinks bool `json:"-"`
}

func (r *UpdateItemRequest) UnmarshalJSON(data []byte) error {
	type Alias UpdateItemRequest
	aux := &struct{ *Alias }{Alias: (*Alias)(r)}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if _, ok := raw["tags"]; ok {
		r.HasTags = true
	}
	if _, ok := raw["links"]; ok {
		r.HasLinks = true
	}
	return nil
}

type RerankRequest struct {
	Items []RerankItem `json:"items"`
}

type RerankItem struct {
	ID   string  `json:"id"`
	Rank float64 `json:"rank"`
}

type ListInfo struct {
	ListID string `json:"list_id"`
	Count  int    `json:"count"`
}

type TagInfo struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}
