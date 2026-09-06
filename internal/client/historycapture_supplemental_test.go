//go:build darwin || linux

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/history"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/historyipc"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/osevent"
)

func TestHistoryCaptureFlagPrecedence(t *testing.T) {
	for key, value := range map[string]string{"WORKTREE": "/env/repo", "SOURCE": "env-source", "TOOL": "env-tool", "ACTION": "env-action", "SESSION_ID": "env-session"} {
		t.Setenv("AGENT_FITNESS_FUNCTIONS_HISTORY_"+key, value)
	}
	base := []string{"--repo", "logical", "--file", "source.go"}
	opts, err := parseValidateFlags(base)
	if err != nil {
		t.Fatal(err)
	}
	if opts.history != (historyOptions{worktree: "/env/repo", source: "env-source", tool: "env-tool", action: "env-action", sessionID: "env-session"}) {
		t.Fatalf("environment ignored: %+v", opts.history)
	}
	opts, err = parseValidateFlags(append(base, "--history-worktree=/flag/repo", "--history-source=", "--history-tool=", "--history-action=flag-action", "--history-session-id=flag-session"))
	if err != nil {
		t.Fatal(err)
	}
	if opts.history != (historyOptions{worktree: "/flag/repo", action: "flag-action", sessionID: "flag-session"}) {
		t.Fatalf("explicit flags MUST override even with empty values: %+v", opts.history)
	}
}

func TestHistoryCaptureDefaultsAndDistinctInvocations(t *testing.T) {
	opts, request := historyCaptureInput(t)
	capture, events, logs := historyCaptureFixture()
	completed := time.Now().In(time.FixedZone("fixture", 3600))
	for _, status := range []string{"pass", "advisory", "block"} {
		capture.complete(context.Background(), opts, request, []byte(`{"status":"`+status+`","future":["kept"]}`), completed)
	}
	if len(*logs) != 0 || len(*events) != 3 {
		t.Fatalf("events=%+v logs=%+v", *events, *logs)
	}
	ids := map[string]bool{}
	for _, event := range *events {
		if event.Source != "manual" || event.Tool != nil || event.Action != nil || event.SessionID != nil {
			t.Errorf("missing identity MUST remain unknown: %+v", event)
		}
		if !event.CompletedAt.Equal(completed) || event.CompletedAt.Location() != time.UTC {
			t.Errorf("completion not UTC: %v", event.CompletedAt)
		}
		if len(event.EventID) != 32 || ids[event.EventID] {
			t.Errorf("IDs MUST distinguish invocations: %q", event.EventID)
		}
		ids[event.EventID] = true
		if !bytes.Equal(event.RequestJSON, request) {
			t.Error("request was reserialized")
		}
	}
}

func TestHistoryCaptureRejectsNonVerdictsBeforeMetadata(t *testing.T) {
	opts, request := historyCaptureInput(t)
	for name, result := range map[string]string{"malformed": "{", "null": "null", "missing": "{}", "error": `{"status":"error"}`, "warming": `{"status":"pass","warming":true}`} {
		t.Run(name, func(t *testing.T) {
			capture, events, logs := historyCaptureFixture()
			capture.resolve = func(context.Context, string, string) (history.Location, error) {
				t.Error("ineligible response resolved metadata")
				return history.Location{}, errors.New("unexpected")
			}
			capture.complete(context.Background(), opts, request, []byte(result), time.Now())
			if len(*events) != 0 || len(*logs) != 1 {
				t.Fatalf("events=%+v diagnostics=%+v", *events, *logs)
			}
			if (*logs)[0].Phase != "response" {
				t.Errorf("wrong diagnostic: %+v", (*logs)[0])
			}
		})
	}
}

func TestHistoryCaptureDropsMetadataAndPublishFailures(t *testing.T) {
	opts, request := historyCaptureInput(t)
	for _, phase := range []string{"random", "resolve", "publish"} {
		t.Run(phase, func(t *testing.T) {
			capture, events, logs := historyCaptureFixture()
			attempts := 0
			switch phase {
			case "random":
				capture.random = bytes.NewReader(nil)
			case "resolve":
				capture.resolve = func(context.Context, string, string) (history.Location, error) {
					return history.Location{}, errors.New("PRIVATE_METADATA")
				}
			case "publish":
				capture.publish = func(string, history.Event) error { attempts++; return errors.New("PRIVATE_SOURCE") }
			}
			capture.complete(context.Background(), opts, request, []byte(`{"status":"pass"}`), time.Now())
			if len(*events) != 0 || len(*logs) != 1 || attempts > 1 {
				t.Fatalf("events=%+v logs=%+v attempts=%d", *events, *logs, attempts)
			}
			encoded, _ := json.Marshal((*logs)[0])
			if bytes.Contains(encoded, []byte("PRIVATE_")) {
				t.Errorf("arbitrary errors leaked: %s", encoded)
			}
			if phase == "publish" && (*logs)[0].Worktree != "/fixture/worktree" {
				t.Errorf("resolved worktree missing from publish diagnostic: %+v", (*logs)[0])
			}
		})
	}
}

func TestHistoryCaptureSARIFPreservesRawResult(t *testing.T) {
	repo := t.TempDir()
	runGitClientTest(t, repo, "init")
	socket := historyCaptureSocket(t)
	listener, err := historyipc.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("AGENT_FITNESS_FUNCTIONS_HISTORY_SOCKET", socket)
	const result = `{"status":"advisory","violations":[],"future":{"unchanged":true}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/check" {
			_, _ = io.WriteString(w, result)
		}
	}))
	defer server.Close()
	var out bytes.Buffer
	err = RunCheck([]string{"--addr", server.URL, "--repo", "logical", "--file", "source.go", "--content", "package source", "--history-worktree", repo, "--format", "sarif"}, &out, server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var want bytes.Buffer
	if err := writeValidationResult(&want, "sarif", "logical", []byte(result)); err != nil {
		t.Fatal(err)
	}
	if out.String() != want.String() {
		t.Errorf("SARIF output changed: %s", out.String())
	}
	if err := listener.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	conn, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	message, err := historyipc.DecodeFrame(conn)
	if err != nil || message.Event == nil {
		t.Fatalf("frame=%+v error=%v", message, err)
	}
	if string(message.Event.ResultJSON) != result {
		t.Errorf("raw result lost: %s", message.Event.ResultJSON)
	}
}

func TestHistoryCaptureIneligibleOutputIsUnchanged(t *testing.T) {
	for _, body := range []string{"{malformed", `{"status":"pass","warming":true}`, `{"status":"error"}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/check" {
					_, _ = io.WriteString(w, body)
				}
			}))
			defer server.Close()
			var out bytes.Buffer
			if err := RunCheck([]string{"--addr", server.URL, "--repo", "logical", "--file", "x.go", "--content", "package x"}, &out, server.Client(), nil); err != nil || out.String() != body {
				t.Fatalf("output=%q error=%v", out.String(), err)
			}
		})
	}
}

func TestDoctorPostCheckBypassesHistory(t *testing.T) {
	socket := historyCaptureSocket(t)
	listener, err := historyipc.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("AGENT_FITNESS_FUNCTIONS_HISTORY_SOCKET", socket)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"status":"pass"}`) }))
	defer server.Close()
	_, err = postCheck(context.Background(), server.Client(), server.URL, fitness.ValidationRequest{Repo: "logical", File: "source.go", ProposedContent: "package source"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.SetDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if conn, err := listener.AcceptUnix(); err == nil {
		conn.Close()
		t.Fatal("doctor probe entered history")
	}
}

func historyCaptureInput(t *testing.T) (validateOptions, []byte) {
	t.Helper()
	request, err := json.Marshal(fitness.ValidationRequest{Repo: "logical", File: "source.go", ProposedContent: "package source"})
	if err != nil {
		t.Fatal(err)
	}
	return validateOptions{repo: "logical", file: "source.go"}, request
}

func historyCaptureSocket(t *testing.T) string {
	t.Helper()
	// Darwin limits Unix addresses to 103 bytes, so test names cannot be part
	// of the endpoint path. The private parent still isolates every fixture.
	dir, err := os.MkdirTemp("", "aff-hc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "s")
}

func historyCaptureFixture() (historyCapture, *[]history.Event, *[]osevent.Diagnostic) {
	var events []history.Event
	var logs []osevent.Diagnostic
	capture := newHistoryCapture(historyOptions{})
	capture.resolve = func(context.Context, string, string) (history.Location, error) {
		return history.Location{Worktree: "/fixture/worktree", CommonGitDir: "/fixture/worktree/.git", File: "source.go"}, nil
	}
	capture.publish = func(_ string, event history.Event) error { events = append(events, event); return nil }
	capture.log = func(diagnostic osevent.Diagnostic) error { logs = append(logs, diagnostic); return nil }
	return capture, &events, &logs
}

func TestHistoryCaptureUnavailableWriterPreservesValidation(t *testing.T) {
	t.Setenv("AGENT_FITNESS_FUNCTIONS_HISTORY_SOCKET", filepath.Join(t.TempDir(), "absent"))
	repo := t.TempDir()
	runGitClientTest(t, repo, "init")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/check" {
			_, _ = io.WriteString(w, `{"status":"block"}`)
		}
	}))
	defer server.Close()
	var out bytes.Buffer
	if err := RunCheck([]string{"--addr", server.URL, "--repo", "logical", "--file", "x.go", "--content", "package x", "--history-worktree", repo}, &out, server.Client(), nil); err != nil || out.String() != `{"status":"block"}` {
		t.Fatalf("output=%q error=%v", out.String(), err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "agent-fitness-functions")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("validation created history storage: %v", err)
	}
}
