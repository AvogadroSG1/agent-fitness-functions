package history

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestHistoryStorePreservesDistinctAttemptsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	event, path := storedFixture(t)
	if err := Insert(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err := Insert(ctx, event); err != nil {
		t.Fatal(err)
	}
	second := event
	second.EventID = "1123456789abcdef0123456789abcdef"
	if err := Insert(ctx, second); err != nil {
		t.Fatal(err)
	}
	page, err := List(ctx, path, Query{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.Records[0].EventID != second.EventID || page.NextBefore == nil {
		t.Fatalf("first page = %+v, want newer attempt and cursor", page)
	}
	if len(page.Records[0].RequestJSON) != 0 || len(page.Records[0].ResultJSON) != 0 {
		t.Error("list exposes full source/result payloads")
	}
	older, err := List(ctx, path, Query{Limit: 1, Before: *page.NextBefore})
	if err != nil {
		t.Fatal(err)
	}
	if len(older.Records) != 1 || older.Records[0].EventID != event.EventID || older.NextBefore != nil {
		t.Fatalf("second page = %+v, want original attempt without duplicate", older)
	}
	got, err := Show(ctx, path, event.EventID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.RequestJSON, event.RequestJSON) || !bytes.Equal(got.ResultJSON, event.ResultJSON) {
		t.Error("saved request/result changed after reopening database")
	}
	if got.Tool != nil || got.SessionID != nil || got.RecordedAt.IsZero() {
		t.Errorf("saved metadata = %+v", got)
	}
	for _, target := range []string{filepath.Dir(path), path} {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Errorf("history permissions = %o, want owner-only", info.Mode().Perm())
		}
	}
}

func TestHistoryReaderDoesNotCreateAbsentStorage(t *testing.T) {
	_, path := storedFixture(t)
	page, err := List(context.Background(), path, Query{})
	if err != nil || len(page.Records) != 0 {
		t.Fatalf("absent history = %+v, %v", page, err)
	}
	if _, err := Show(context.Background(), path, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("absent record error = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("read created history directory: %v", err)
	}
}

func TestHistoryStoreRejectsLockedOrNewerDatabase(t *testing.T) {
	ctx := context.Background()
	event, path := storedFixture(t)
	if err := Insert(ctx, event); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	event.EventID = "2123456789abcdef0123456789abcdef"
	if err := Insert(ctx, event); err == nil {
		t.Error("write-locked database accepted insert")
	}
	if _, err := db.Exec("ROLLBACK; PRAGMA user_version = 2"); err != nil {
		t.Fatal(err)
	}
	if err := Insert(ctx, event); err == nil {
		t.Error("newer database schema accepted insert")
	}
	if _, err := List(ctx, path, Query{}); err == nil {
		t.Error("newer database schema accepted read")
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM validation_history").Scan(&count); err != nil || count != 1 {
		t.Errorf("existing rows changed: count=%d error=%v", count, err)
	}
}

func storedFixture(t *testing.T) (Event, string) {
	t.Helper()
	repo := t.TempDir()
	gitHistory(t, repo, "init")
	location, err := Resolve(context.Background(), repo, "example.go")
	if err != nil {
		t.Fatal(err)
	}
	event := fixtureEvent(t)
	event.CommonGitDir = location.CommonGitDir
	event.Worktree = location.Worktree
	return event, location.DatabasePath
}
