package db

import (
	"fmt"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/kayushkin/noteboard/internal/model"
)

// legacyTimeLayout is SQLite's own timestamp encoding, and the one
// mattn/go-sqlite3 wrote for this table's entire history — every row in the
// live database is stored in it.
//
// modernc.org/sqlite writes Go's time.Time.String() instead
// ("2026-07-12 23:56:23.353827032 +0000 UTC") unless _time_format pins it to
// this. The two are different strings in a TEXT column that ORDER BY compares
// lexicographically, so a mismatch would not error anywhere — it would just
// quietly interleave new rows with old ones in the wrong order.
const legacyTimeLayout = "2006-01-02 15:04:05.999999999-07:00"

var sqliteTimeText = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}(\.\d{1,9})?\+00:00$`)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	// New() runs migrate(), which issues CREATE VIRTUAL TABLE ... USING fts5.
	// That statement is the reason this package used to need CGO_CFLAGS: a
	// driver without the FTS5 module compiled in fails right here. Every test
	// below therefore also asserts, just by getting a store, that a default
	// `go build` produces a binary that can open its own database.
	store, err := New(filepath.Join(t.TempDir(), "noteboard.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func mustCreate(t *testing.T, s *Store, title string) *model.Item {
	t.Helper()
	item, err := s.CreateItem(&model.CreateItemRequest{Type: "todo", Title: title})
	if err != nil {
		t.Fatalf("CreateItem(%q): %v", title, err)
	}
	return item
}

// rawText reads a column's stored bytes, bypassing the driver's habit of
// decoding a DATETIME column into a time.Time. CAST(...) makes the result an
// expression, which has no declared type, so the driver hands back the text.
func rawText(t *testing.T, s *Store, col, id string) string {
	t.Helper()
	var got string
	q := fmt.Sprintf("SELECT CAST(%s AS TEXT) FROM items WHERE id = ?", col)
	if err := s.db.QueryRow(q, id).Scan(&got); err != nil {
		t.Fatalf("read raw %s: %v", col, err)
	}
	return got
}

// insertLegacyRow writes a row the way the mattn-era binary wrote it: the
// timestamp columns as pre-formatted SQLite-format text, bound as a string so
// nothing in the current driver touches the encoding. This is what all 4100
// rows in the live database look like.
func insertLegacyRow(t *testing.T, s *Store, id, title string, created time.Time, dueAt any) {
	t.Helper()
	stamp := created.Format(legacyTimeLayout)
	_, err := s.db.Exec(
		"INSERT INTO items ("+itemCols+") VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
		id, "todo", title, "", "[]", 0, 0.0, "open", "", dueAt, nil, "[]", "", stamp, stamp,
	)
	if err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
}

// TestTimestampsAreStoredInSQLiteFormat is the regression guard for the driver
// swap. Drop _time_format=sqlite from the DSN and this fails: the stored text
// becomes Go's time.Time.String().
func TestTimestampsAreStoredInSQLiteFormat(t *testing.T) {
	s := newTestStore(t)
	item := mustCreate(t, s, "format check")

	for _, col := range []string{"created_at", "updated_at"} {
		got := rawText(t, s, col, item.ID)
		if !sqliteTimeText.MatchString(got) {
			t.Errorf("%s stored as %q, which is not SQLite's timestamp format\n"+
				"(a Go time.Time.String() value looks like \"2026-07-12 23:56:23.35 +0000 UTC\")", col, got)
		}
		if _, err := time.Parse(legacyTimeLayout, got); err != nil {
			t.Errorf("%s = %q does not parse as the format the live rows use: %v", col, got, err)
		}
	}
}

// TestReadsRowsWrittenByTheLegacyDriver proves the swap can read the data
// already on disk — the 4100 rows mattn/go-sqlite3 wrote.
func TestReadsRowsWrittenByTheLegacyDriver(t *testing.T) {
	s := newTestStore(t)
	want := time.Date(2026, 7, 12, 23, 56, 23, 353827032, time.UTC)
	insertLegacyRow(t, s, "legacy-1", "written by the old driver", want, nil)

	got, err := s.GetItem("legacy-1")
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if !got.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want)
	}
	if !got.UpdatedAt.Equal(want) {
		t.Errorf("UpdatedAt = %v, want %v", got.UpdatedAt, want)
	}
}

// TestStoredTimestampsAreReadableBySQLiteDateFunctions is the sharpest reason
// the format pin exists. A Go time.Time.String() value is not a timestamp
// SQLite recognises, and SQLite does not complain about that — date() and
// friends just return NULL. Any date arithmetic in SQL would quietly evaluate
// to nothing, so the loud assertion belongs here.
func TestStoredTimestampsAreReadableBySQLiteDateFunctions(t *testing.T) {
	s := newTestStore(t)
	item := mustCreate(t, s, "date function check")
	today := time.Now().UTC().Format("2006-01-02")

	var day, julian any
	err := s.db.QueryRow(
		"SELECT date(created_at), julianday(created_at) FROM items WHERE id = ?", item.ID,
	).Scan(&day, &julian)
	if err != nil {
		t.Fatalf("date(created_at): %v", err)
	}
	if day == nil || julian == nil {
		t.Fatalf("SQLite cannot parse the stored created_at: date()=%v julianday()=%v\n"+
			"The value was written in an encoding SQLite does not recognise as a timestamp.", day, julian)
	}
	if day != today {
		t.Errorf("date(created_at) = %v, want %v", day, today)
	}
}

// TestListOrdersChronologicallyAcrossDriverEras covers rows written on either
// side of the swap sorting together correctly. ORDER BY on these TEXT columns
// is a lexicographic byte compare, so this is worth an assertion even though
// the encodings do happen to collate consistently with each other.
func TestListOrdersChronologicallyAcrossDriverEras(t *testing.T) {
	s := newTestStore(t)

	// Oldest, written the legacy way.
	insertLegacyRow(t, s, "old", "oldest", time.Now().UTC().Add(-2*time.Hour), nil)
	// Newest, written through CreateItem by the current driver.
	newest := mustCreate(t, s, "newest")

	items, err := s.ListItems(ListParams{Type: "todo", Sort: "created_at"})
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].ID != newest.ID {
		t.Errorf("created_at DESC put %q first; want the newest row %q", items[0].Title, newest.Title)
	}
}

// TestSearchMatchesViaFTS5 exercises the FTS5 module at runtime. A build
// without it never reaches this — it dies in migrate() — but the assertion also
// covers the index and its triggers actually working.
func TestSearchMatchesViaFTS5(t *testing.T) {
	s := newTestStore(t)
	item := mustCreate(t, s, "xylophone reconciliation")
	mustCreate(t, s, "unrelated")

	found, err := s.Search(SearchParams{Query: "xylophone"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(found) != 1 || found[0].ID != item.ID {
		t.Fatalf("Search(xylophone) = %d hits, want just %q", len(found), item.Title)
	}
}

// TestDueAtPreservesStoredOffset covers the one non-null due_at in the live
// database, which is RFC3339 with a -07:00 offset. The offset is part of the
// value the caller supplied, so it has to survive the round trip rather than
// being flattened to UTC.
func TestDueAtPreservesStoredOffset(t *testing.T) {
	s := newTestStore(t)
	insertLegacyRow(t, s, "due-1", "has a due date", time.Now().UTC(), "2026-06-30T09:00:00-07:00")

	got, err := s.GetItem("due-1")
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if got.DueAt == nil {
		t.Fatal("DueAt is nil, want 2026-06-30T09:00:00-07:00")
	}
	if want := time.Date(2026, 6, 30, 16, 0, 0, 0, time.UTC); !got.DueAt.Equal(want) {
		t.Errorf("DueAt = %v, want the same instant as %v", got.DueAt, want)
	}
	if got, want := got.DueAt.Format(time.RFC3339), "2026-06-30T09:00:00-07:00"; got != want {
		t.Errorf("DueAt renders as %q, want %q — the stored zone was lost", got, want)
	}
}

// TestDueAtRoundTripsThroughCreate covers the write side of the same column.
func TestDueAtRoundTripsThroughCreate(t *testing.T) {
	s := newTestStore(t)
	due := time.Date(2026, 8, 1, 17, 30, 0, 0, time.UTC)
	created, err := s.CreateItem(&model.CreateItemRequest{
		Type: "todo", Title: "with a due date", DueAt: &due,
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	got, err := s.GetItem(created.ID)
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if got.DueAt == nil || !got.DueAt.Equal(due) {
		t.Errorf("DueAt = %v, want %v", got.DueAt, due)
	}
}
