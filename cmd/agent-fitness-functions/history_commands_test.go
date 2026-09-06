package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/history"
)

func TestHistoryCommands(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	location, err := history.Resolve(context.Background(), repo, "example.go")
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"client", "history", "list", "--repo", repo, "--format", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("empty history read: exit=%d stderr=%s", code, &stderr)
	}
	var page map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &page); err != nil || string(page["records"]) != "[]" || string(page["next_before"]) != "null" {
		t.Fatalf("empty page must expose records and terminal cursor: %s, %v", &stdout, err)
	}
	if _, err := os.Stat(filepath.Dir(location.DatabasePath)); !os.IsNotExist(err) {
		t.Fatalf("read created history storage: %v", err)
	}
	from := insertHistoryCommandFixture(t, location, "0123456789abcdef0123456789abcdef", "λ", fitness.StatusBlock)
	to := insertHistoryCommandFixture(t, location, "1123456789abcdef0123456789abcdef", "λ\n", fitness.StatusPass)
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"client", "history", "diff", from, to, "--repo", repo, "--format", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("different proposals must be successful inspection: exit=%d stderr=%s", code, &stderr)
	}
	var comparison struct {
		From        history.Record `json:"from"`
		To          history.Record `json:"to"`
		UnifiedDiff string         `json:"unified_diff"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &comparison); err != nil {
		t.Fatalf("diff JSON: %v, %s", err, &stdout)
	}
	if comparison.From.EventID != from || comparison.To.EventID != to || comparison.From.Status != fitness.StatusBlock || comparison.To.Status != fitness.StatusPass {
		t.Fatalf("comparison lost explicit contexts/verdicts: %+v", comparison)
	}
	for _, marker := range []string{from, to, "example.go", "proposal", "\\ No newline at end of file"} {
		if !strings.Contains(comparison.UnifiedDiff, marker) {
			t.Errorf("final-newline difference missing %q: %q", marker, comparison.UnifiedDiff)
		}
	}
}

func insertHistoryCommandFixture(t *testing.T, location history.Location, id, content string, status fitness.Status) string {
	t.Helper()
	request, err := json.Marshal(fitness.ValidationRequest{Repo: "logical-key", File: location.File, ProposedContent: content, Language: "go", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	event := history.Event{
		EventID: id, CompletedAt: time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC),
		CommonGitDir: location.CommonGitDir, Worktree: location.Worktree, Repository: "logical-key",
		Branch: location.Branch, HeadOID: location.HeadOID, File: location.File, Source: "manual",
		Status: status, DryRun: true, RequestJSON: request,
		ResultJSON: json.RawMessage(`{"status":"` + string(status) + `","future":"kept"}`),
	}
	if err := history.Insert(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	return id
}
