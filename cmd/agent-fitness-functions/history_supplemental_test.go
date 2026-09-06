package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/client"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/history"
)

func TestHistoryCommandExitCodesAndText(t *testing.T) {
	t.Setenv("GIT_TRACE2_EVENT", "0")
	repo := t.TempDir()
	runGit(t, repo, "init")
	location, err := history.Resolve(context.Background(), repo, "example.go")
	if err != nil {
		t.Fatal(err)
	}
	first := insertHistoryCommandFixture(t, location, "0123456789abcdef0123456789abcdef", "one\n", fitness.StatusBlock)
	second := insertHistoryCommandFixture(t, location, "1123456789abcdef0123456789abcdef", "two\n", fitness.StatusPass)
	other := location
	other.File = "other.go"
	unlike := insertHistoryCommandFixture(t, other, "2123456789abcdef0123456789abcdef", "one\n", fitness.StatusPass)
	for _, tt := range []struct {
		name   string
		args   []string
		code   int
		marker string
	}{
		{"show text", []string{"show", first}, 0, "Request:"},
		{"diff text", []string{"diff", first, second}, 0, "From proposal:"},
		{"list text", []string{"list"}, 0, second},
		{"unlike files", []string{"diff", first, unlike}, 2, "same normalized file"},
		{"missing ID", []string{"show", "absent"}, 1, "not found"},
		{"missing from", []string{"diff", "absent", first}, 1, "not found"},
		{"missing to", []string{"diff", first, "absent"}, 1, "not found"},
		{"unknown flag", []string{"show", first, "--unknown"}, 2, "flag provided but not defined"},
		{"escape file", []string{"list", "--file=../outside.go"}, 2, "inside the worktree"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := append([]string{"client", "history"}, tt.args...)
			args = append(args, "--repo", repo)
			starter := func(client.DaemonStartConfig) error { t.Fatal("history started daemon"); return nil }
			code := runWithDependencies(args, &stdout, &stderr, nil, starter)
			if code != tt.code || !strings.Contains(stdout.String()+stderr.String(), tt.marker) {
				t.Fatalf("exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
			}
		})
	}
}

func TestHistoryCommandCorruptStorageAndHelp(t *testing.T) {
	t.Setenv("GIT_TRACE2_EVENT", "0")
	repo := t.TempDir()
	runGit(t, repo, "init")
	directory := filepath.Join(repo, ".git", "agent-fitness-functions")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "history.sqlite3")
	if err := os.WriteFile(path, []byte("corrupt history"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range [][]string{{"list"}, {"show", "absent"}, {"diff", "one", "two"}} {
		var stdout, stderr bytes.Buffer
		args := append([]string{"client", "history"}, command...)
		code := run(append(args, "--repo", repo), &stdout, &stderr)
		if code != 1 || stdout.Len() != 0 {
			t.Fatalf("corrupt read: exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
		}
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "corrupt history" {
		t.Fatalf("read changed corrupt storage: %q, %v", content, err)
	}
	for _, args := range [][]string{{"--help"}, {"client", "history", "--help"}, {"client", "history", "list", "--help"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "client history") {
			t.Fatalf("help: exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
		}
	}
}
