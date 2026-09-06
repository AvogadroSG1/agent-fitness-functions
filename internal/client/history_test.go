package client

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/history"
)

func TestHistoryReadWithoutWriterOrStorage(t *testing.T) {
	repo := historyReadRepo(t)
	t.Chdir(repo)
	var out bytes.Buffer
	if err := RunHistory([]string{"list", "--format=json"}, &out); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != `{"records":[],"next_before":null}` {
		t.Fatalf("empty list: %s", &out)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "agent-fitness-functions")); !os.IsNotExist(err) {
		t.Fatalf("read initialized storage: %v", err)
	}
	for _, args := range [][]string{{"show", "absent"}, {"diff", "absent", "missing"}} {
		if err := RunHistory(args, &out); err == nil || IsUsageError(err) {
			t.Fatalf("missing ID: %v", err)
		}
	}
}

func TestHistoryReadFiltersPaginationAndUnknownPayloads(t *testing.T) {
	repo := historyReadRepo(t)
	historyReadGit(t, repo, "-c", "user.name=History Test", "-c", "user.email=history@example.invalid", "commit", "--allow-empty", "-m", "fixture")
	linked := filepath.Join(t.TempDir(), "removed-worktree")
	historyReadGit(t, repo, "worktree", "add", "--detach", linked)
	first := historyReadEvent(t, linked, "0123456789abcdef0123456789abcdef", "before")
	second := historyReadEvent(t, linked, "1123456789abcdef0123456789abcdef", "after")
	historyReadGit(t, repo, "worktree", "remove", linked)
	var out bytes.Buffer
	args := []string{"list", "--repo", repo, "--file", filepath.Join(repo, "deleted", "example.go"), "--session", "session", "--worktree", second.Worktree, "--limit", "1", "--format", "json"}
	if err := RunHistory(args, &out); err != nil {
		t.Fatal(err)
	}
	var page history.Page
	if err := json.Unmarshal(out.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.Records[0].EventID != second.EventID || page.NextBefore == nil {
		t.Fatalf("page: %s", &out)
	}
	if bytes.Contains(out.Bytes(), []byte("request_json")) {
		t.Fatal("list exposed payload")
	}
	out.Reset()
	if err := RunHistory(append(args, "--before", "2"), &out); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.Records[0].EventID != first.EventID || page.NextBefore != nil {
		t.Fatalf("older page: %s", &out)
	}
	out.Reset()
	if err := RunHistory([]string{"show", first.EventID, "--repo", repo, "--format=json"}, &out); err != nil {
		t.Fatal(err)
	}
	for _, preserved := range []string{`"future_request":9007199254740993`, `"future_result":{"kept":true}`, `"proposed_content":"before"`} {
		if !strings.Contains(out.String(), preserved) {
			t.Errorf("show lost %s: %s", preserved, &out)
		}
	}
}

func TestHistoryRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		nil, {"unknown"}, {"list", "extra"}, {"show"}, {"show", "a", "b"}, {"diff", "a"},
		{"list", "--limit=0"}, {"list", "--limit=-1"}, {"list", "--limit=1001"}, {"list", "--before=-1"},
		{"list", "--format=sarif"}, {"show", "a", "--file=x"}, {"diff", "a", "b", "--unknown"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out bytes.Buffer
			if err := RunHistory(args, &out); !IsUsageError(err) {
				t.Fatalf("expected usage error: %v", err)
			}
		})
	}
}

func TestHistoryFlagsCanSurroundIDsAndRetainTerminatorSemantics(t *testing.T) {
	for _, args := range [][]string{
		{"show", "--repo", "--", "event", "--format=json"},
		{"show", "--format=json", "event", "--repo=--"},
		{"show", "--repo=--", "--format=json", "--", "event"},
	} {
		opts, err := parseHistoryOptions(args)
		if err != nil || opts.repo != "--" || opts.format != "json" || len(opts.ids) != 1 || opts.ids[0] != "event" {
			t.Fatalf("parse %v: %+v, %v", args, opts, err)
		}
	}
	if _, err := parseHistoryOptions([]string{"show", "--", "event", "--format=json"}); err == nil {
		t.Fatal("parsed a flag after explicit terminator")
	}
}

func historyReadRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	t.Setenv("GIT_TRACE2_EVENT", "0")
	cmd := exec.Command("git", "init", repo)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	return repo
}

func historyReadEvent(t *testing.T, repo, id, content string) history.Event {
	t.Helper()
	location, err := history.Resolve(context.Background(), repo, "deleted/example.go")
	if err != nil {
		t.Fatal(err)
	}
	session := "session"
	event := history.Event{
		EventID: id, CompletedAt: time.Now().UTC(), CommonGitDir: location.CommonGitDir,
		Worktree: location.Worktree, Repository: "logical", File: location.File,
		Source: "manual", SessionID: &session, Status: fitness.StatusPass,
		RequestJSON: json.RawMessage(`{"repo":"logical","file":"deleted/example.go","proposed_content":"` + content + `","dry_run":false,"future_request":9007199254740993}`),
		ResultJSON:  json.RawMessage(`{"status":"pass","future_result":{"kept":true}}`),
	}
	if err := history.Insert(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	return event
}

func historyReadGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}
