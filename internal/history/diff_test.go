package history

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestProposalDiffPreservesLines(t *testing.T) {
	for _, tt := range []struct{ name, from, to, hunk string }{
		{"empty", "", "", ""},
		{"identical Unicode", "λ\n", "λ\n", ""},
		{"empty to line", "", "λ\n", "@@ -0,0 +1 @@\n+λ\n"},
		{"line to empty", "λ\n", "", "@@ -1 +0,0 @@\n-λ\n"},
		{"final newline", "λ", "λ\n", "@@ -1 +1 @@\n-λ\n\\ No newline at end of file\n+λ\n"},
		{"both incomplete", "before", "after", "@@ -1 +1 @@\n-before\n\\ No newline at end of file\n+after\n\\ No newline at end of file\n"},
		{"CRLF to LF", "λ\r\n", "λ\n", "@@ -1 +1 @@\n-λ\r\n+λ\n"},
		{"empty to incomplete", "", "λ", "@@ -0,0 +1 @@\n+λ\n\\ No newline at end of file\n"},
		{"three context lines", "0\n1\n2\n3\n4\nold\n6\n7\n8\n9\n10\n", "0\n1\n2\n3\n4\nnew\n6\n7\n8\n9\n10\n", "@@ -3,7 +3,7 @@\n 2\n 3\n 4\n-old\n+new\n 6\n 7\n 8\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			from, to := diffRecord(t, "from", tt.from), diffRecord(t, "to", tt.to)
			comparison, err := Diff(from, to)
			if err != nil {
				t.Fatal(err)
			}
			want := tt.hunk
			if want != "" {
				want = "--- \"proposal/from/example.go\"\n+++ \"proposal/to/example.go\"\n" + want
			}
			if comparison.UnifiedDiff != want {
				t.Errorf("diff = %q, want %q", comparison.UnifiedDiff, want)
			}
			if comparison.From.EventID != "from" || comparison.To.EventID != "to" {
				t.Fatal("lost selected contexts")
			}
		})
	}
}

func TestProposalDiffRejectsUnlikeFilesAndQuotesLabels(t *testing.T) {
	from, to := diffRecord(t, "from", "a"), diffRecord(t, "to", "b")
	to.File = "another.go"
	if _, err := Diff(from, to); !errors.Is(err, ErrDifferentFiles) {
		t.Fatalf("unlike files: %v", err)
	}
	from.File, to.File = "odd\t\n\"λ.go", "odd\t\n\"λ.go"
	to.Worktree = "/removed/linked"
	got, err := Diff(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.UnifiedDiff, "--- \"proposal/from/odd\\t\\n\\\"λ.go\"\n") {
		t.Fatalf("ambiguous label: %q", got.UnifiedDiff)
	}
}

func diffRecord(t *testing.T, id, content string) Record {
	t.Helper()
	request, err := json.Marshal(map[string]any{"proposed_content": content})
	if err != nil {
		t.Fatal(err)
	}
	return Record{Event: Event{EventID: id, File: "example.go", RequestJSON: request}}
}
