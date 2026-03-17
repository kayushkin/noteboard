package model

import (
	"encoding/json"
	"fmt"
	"time"
)

type Item struct {
	ID        string     `json:"id"`
	Type      string     `json:"type"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Tags      []string   `json:"tags"`
	Priority  int        `json:"priority"`
	Rank      float64    `json:"rank"`
	Status    string     `json:"status"`
	ListID    string     `json:"list_id"`
	DueAt     *time.Time `json:"due_at,omitempty"`
	ParentID  *string    `json:"parent_id,omitempty"`
	Links     []string   `json:"links"`
	CreatedBy string     `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
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
	if r.Type != "note" && r.Type != "todo" && r.Type != "rank" {
		return fmt.Errorf("type must be note, todo, or rank")
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
