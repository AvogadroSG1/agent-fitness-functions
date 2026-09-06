package history

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestHistoryStoreDoesNotMutateNewerDatabasePermissionsOrContents(t *testing.T) {
	ctx := context.Background()
	event, path := storedFixture(t)
	if err := Insert(ctx, event); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version = 2"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	event.EventID = "3123456789abcdef0123456789abcdef"
	if err := Insert(ctx, event); err == nil {
		t.Fatal("newer database accepted insert")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Error("newer database contents changed")
	}
	for target, want := range map[string]os.FileMode{filepath.Dir(path): 0o750, path: 0o640} {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("permissions for %s = %o, want %o", target, got, want)
		}
	}
}

func TestHistoryPageJSONAlwaysIncludesNextBefore(t *testing.T) {
	ctx := context.Background()
	event, path := storedFixture(t)
	if err := Insert(ctx, event); err != nil {
		t.Fatal(err)
	}
	finalPage, err := List(ctx, path, Query{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	assertNextBeforeJSON(t, finalPage, true)

	event.EventID = "4123456789abcdef0123456789abcdef"
	if err := Insert(ctx, event); err != nil {
		t.Fatal(err)
	}
	continuedPage, err := List(ctx, path, Query{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	assertNextBeforeJSON(t, continuedPage, false)
}

func assertNextBeforeJSON(t *testing.T, page Page, wantNull bool) {
	t.Helper()
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatal(err)
	}
	value, exists := envelope["next_before"]
	if !exists {
		t.Fatal("page JSON omitted next_before")
	}
	if gotNull := bytes.Equal(value, []byte("null")); gotNull != wantNull {
		t.Errorf("next_before JSON = %s, want null=%t", value, wantNull)
	}
}

func TestHistoryStorePreservesUnknownJSONAndFiltersRecords(t *testing.T) {
	ctx := context.Background()
	event, path := storedFixture(t)
	event.RequestJSON = []byte(`{ "repo":"governed-name", "file":"example.go", "proposed_content":"package example\n", "language":"go", "dry_run":true, "future":9007199254740993 }`)
	event.ResultJSON = []byte(`{ "status":"pass", "future":{"value":9007199254740993} }`)
	sessionID := "session-one"
	event.SessionID = &sessionID
	if err := Insert(ctx, event); err != nil {
		t.Fatal(err)
	}

	got, err := Show(ctx, path, event.EventID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.RequestJSON, event.RequestJSON) || !bytes.Equal(got.ResultJSON, event.ResultJSON) {
		t.Error("unknown JSON fields or original representation changed")
	}

	page, err := List(ctx, path, Query{File: event.File, SessionID: sessionID, Worktree: event.Worktree})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.Records[0].EventID != event.EventID {
		t.Fatalf("matching filters returned %+v", page.Records)
	}
	encoded, err := json.Marshal(page.Records[0])
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("request_json")) || bytes.Contains(encoded, []byte("result_json")) {
		t.Errorf("list JSON exposes payload fields: %s", encoded)
	}
	page, err = List(ctx, path, Query{SessionID: "another-session"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 0 {
		t.Fatalf("nonmatching filter returned %+v", page.Records)
	}
}

func TestHistoryReaderHandlesURICharactersInDatabasePath(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	repo := filepath.Join(root, "repo #1?")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	gitHistory(t, repo, "init")
	location, err := Resolve(ctx, repo, "example.go")
	if err != nil {
		t.Fatal(err)
	}
	event := fixtureEvent(t)
	event.CommonGitDir = location.CommonGitDir
	event.Worktree = location.Worktree
	if err := Insert(ctx, event); err != nil {
		t.Fatal(err)
	}
	got, err := Show(ctx, location.DatabasePath, event.EventID)
	if err != nil {
		t.Fatal(err)
	}
	if got.EventID != event.EventID {
		t.Errorf("event ID = %q, want %q", got.EventID, event.EventID)
	}
}

func TestHistoryStoreRejectsUnsafeOrCorruptTargets(t *testing.T) {
	ctx := context.Background()
	t.Run("database symlink", func(t *testing.T) {
		event, path := storedFixture(t)
		if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "outside.sqlite3")
		if err := os.WriteFile(target, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		if err := Insert(ctx, event); err == nil {
			t.Error("database symlink accepted")
		}
	})

	t.Run("storage directory symlink", func(t *testing.T) {
		event, path := storedFixture(t)
		if err := os.Symlink(t.TempDir(), filepath.Dir(path)); err != nil {
			t.Fatal(err)
		}
		if err := Insert(ctx, event); err == nil {
			t.Error("storage directory symlink accepted")
		}
		if _, err := List(ctx, path, Query{}); err == nil {
			t.Error("reader accepted storage directory symlink")
		}
	})

	t.Run("corrupt database", func(t *testing.T) {
		event, path := storedFixture(t)
		if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("not sqlite"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := List(ctx, path, Query{}); err == nil {
			t.Error("corrupt database accepted")
		}
		if err := Insert(ctx, event); err == nil {
			t.Error("writer accepted corrupt database")
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, []byte("not sqlite")) {
			t.Errorf("corrupt database changed to %q", got)
		}
	})
}

func TestHistoryListRejectsOutOfRangeLimits(t *testing.T) {
	_, path := storedFixture(t)
	for _, limit := range []int{-1, 1001} {
		if _, err := List(context.Background(), path, Query{Limit: limit}); err == nil {
			t.Errorf("limit %d accepted", limit)
		}
	}
	if _, err := Show(context.Background(), path, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing database error = %v, want ErrNotFound", err)
	}
}
