//go:build darwin || linux

package client

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/historyipc"
)

func TestHistoryCapture(t *testing.T) {
	repo := t.TempDir()
	runGitClientTest(t, repo, "init")
	socket := filepath.Join(t.TempDir(), "s")
	listener, err := historyipc.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("AGENT_FITNESS_FUNCTIONS_HISTORY_SOCKET", socket)
	const result = `{"status":"block","violations":[],"future":{"value":"retained"}}`
	requests := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/check" {
			body, _ := io.ReadAll(r.Body)
			requests <- body
			_, _ = io.WriteString(w, result)
		}
	}))
	defer server.Close()
	const proposal = "package example\n// proposed λ; not applied\n"
	var stdout bytes.Buffer
	err = RunCheck([]string{
		"--addr", server.URL, "--repo", "logical-governance-key", "--file", "example.go",
		"--content", proposal, "--language", "go", "--dry-run",
		"--history-worktree", repo, "--history-source", "agent", "--history-tool", "codex",
		"--history-action", "Write", "--history-session-id", "session-123",
	}, &stdout, server.Client(), nil)
	if err != nil || stdout.String() != result {
		t.Fatalf("validation behavior changed: output=%q error=%v", stdout.String(), err)
	}
	if err := listener.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	conn, err := listener.AcceptUnix()
	if err != nil {
		t.Fatalf("completed verdict was not published: %v", err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	message, err := historyipc.DecodeFrame(conn)
	if err != nil || message.Event == nil {
		t.Fatalf("invalid captured event: %+v, %v", message, err)
	}
	event := message.Event
	if !bytes.Equal(event.RequestJSON, <-requests) || string(event.ResultJSON) != result {
		t.Fatalf("original payloads changed: request=%s result=%s", event.RequestJSON, event.ResultJSON)
	}
	var request struct {
		ProposedContent string `json:"proposed_content"`
	}
	if err := json.Unmarshal(event.RequestJSON, &request); err != nil || request.ProposedContent != proposal {
		t.Fatalf("submitted source changed: %q, %v", request.ProposedContent, err)
	}
	if event.Source != "agent" || event.Tool == nil || *event.Tool != "codex" || event.Action == nil || *event.Action != "Write" || event.SessionID == nil || *event.SessionID != "session-123" || !event.DryRun {
		t.Fatalf("origin or dry-run context lost: %+v", event)
	}
	if err := listener.SetDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	duplicate, err := listener.AcceptUnix()
	if err == nil {
		duplicate.Close()
		t.Fatal("one validation published more than once")
	}
	if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
		t.Fatalf("checking duplicate publication: %v", err)
	}
}
