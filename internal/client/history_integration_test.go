//go:build integration && (darwin || linux)

package client

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/history"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/historyipc"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/historyservice"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/osevent"
)

// These scenarios execute the shipped CLI and its foreground writer in separate
// processes. The HTTP fixture substitutes only the governance server.
func TestHistoryCrossProcess(t *testing.T) {
	root, binary := buildHistoryProcessBinary(t)
	for _, scenario := range []struct {
		name string
		run  func(*testing.T, *historyProcessFixture)
	}{
		{"proposals across worktrees", historyWorktreeScenario},
		{"staged source", historyStagedScenario},
		{"storage cannot gate validation", historyStorageFailureScenario},
		{"non-verdict diagnostics", historyNonVerdictScenario},
	} {
		t.Run(scenario.name, func(t *testing.T) { scenario.run(t, newHistoryProcessFixture(t, root, binary)) })
	}
}

func buildHistoryProcessBinary(t *testing.T) (string, string) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "agent-fitness-functions")
	build := exec.Command("go", "build", "-o", binary, "./cmd/agent-fitness-functions")
	build.Dir = root
	build.Env = historyProcessEnvironment()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}
	return root, binary
}

type historyProcessFixture struct {
	t                                *testing.T
	binary, root, repo, socket, logs string
	env                              []string
	writer                           *exec.Cmd
	writerDone                       chan error
	writerStderr                     *bytes.Buffer
}

func historyProcessEnvironment() []string {
	var environment []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			environment = append(environment, entry)
		}
	}
	return append(environment, "GIT_TRACE2_EVENT=0", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
}

func newHistoryProcessFixture(t *testing.T, root, binary string) *historyProcessFixture {
	t.Helper()
	// Short native temporary paths meet Darwin's 103-byte socket limit and
	// avoid Docker host shares that reject chmod on Linux socket inodes.
	dir, err := os.MkdirTemp("", "hs-")
	if err != nil {
		t.Fatal(err)
	}
	f := &historyProcessFixture{t: t, root: root, binary: binary, repo: t.TempDir(), socket: filepath.Join(dir, "s"), logs: filepath.Join(dir, "events")}
	f.env = append(historyProcessEnvironment(), "AGENT_FITNESS_FUNCTIONS_HISTORY_SOCKET="+f.socket,
		"AGENT_FITNESS_FUNCTIONS_HISTORY_SOURCE=", "AGENT_FITNESS_FUNCTIONS_HISTORY_TOOL=", "AGENT_FITNESS_FUNCTIONS_HISTORY_ACTION=", "AGENT_FITNESS_FUNCTIONS_HISTORY_SESSION_ID=", "AGENT_FITNESS_FUNCTIONS_HISTORY_WORKTREE=",
		"HISTORY_TEST_LOG="+f.logs, "PATH="+filepath.Join(root, "internal/client/testdata/history-logger")+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Cleanup(func() {
		f.stop()
		released, err := historyservice.Released(f.socket)
		if err != nil || !released {
			t.Errorf("preserving unconfirmed writer endpoint %s: released=%t error=%v", dir, released, err)
			return
		}
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	f.git(f.repo, "init", "-b", "main")
	f.git(f.repo, "config", "user.name", "History Fixture")
	f.git(f.repo, "config", "user.email", "history-fixture@example.invalid")
	f.write(filepath.Join(f.repo, "example.go"), "package fixture\n")
	f.git(f.repo, "add", "example.go")
	f.git(f.repo, "-c", "core.hooksPath=/dev/null", "commit", "-m", "fixture")
	return f
}

func (f *historyProcessFixture) command(dir string, args ...string) *exec.Cmd {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	f.t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, f.binary, args...)
	cmd.Dir, cmd.Env = dir, f.env
	return cmd
}

func (f *historyProcessFixture) run(dir string, args ...string) (string, string, int) {
	f.t.Helper()
	cmd := f.command(dir, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.String(), stderr.String(), 0
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		f.t.Fatalf("run CLI: %v", err)
	}
	return stdout.String(), stderr.String(), exit.ExitCode()
}

func (f *historyProcessFixture) git(dir string, args ...string) string {
	f.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir, cmd.Env = dir, f.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (f *historyProcessFixture) write(path, content string) {
	f.t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *historyProcessFixture) start() {
	f.t.Helper()
	f.writer = exec.Command(f.binary, "internal", "history-writer")
	f.writer.Dir, f.writer.Env = f.repo, f.env
	f.writerStderr = new(bytes.Buffer)
	f.writer.Stderr = f.writerStderr
	if err := f.writer.Start(); err != nil {
		f.writer = nil
		f.t.Fatal(err)
	}
	f.writerDone = make(chan error, 1)
	go func(cmd *exec.Cmd, done chan error) { done <- cmd.Wait() }(f.writer, f.writerDone)
	f.eventually(func() bool {
		select {
		case err := <-f.writerDone:
			f.writer = nil
			f.t.Fatalf("writer exited before readiness: %v; stderr=%s", err, f.writerStderr)
		default:
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		m, err := historyipc.RequestControl(ctx, f.socket, historyipc.KindIdentify)
		return err == nil && m.Identity.PID == f.writer.Process.Pid
	})
}

func (f *historyProcessFixture) stop() {
	f.t.Helper()
	if f.writer == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, requestErr := historyipc.RequestControl(ctx, f.socket, historyipc.KindShutdown)
	select {
	case err := <-f.writerDone:
		if err != nil {
			f.t.Errorf("writer exit: %v (shutdown: %v); stderr=%s", err, requestErr, f.writerStderr)
		}
	case <-ctx.Done():
		// This is our own child; join it before releasing any fixture paths.
		if err := f.writer.Process.Kill(); err != nil {
			f.t.Errorf("stop owned writer: %v", err)
			return
		}
		if err := <-f.writerDone; err == nil {
			f.t.Error("writer needed forced shutdown")
		}
		f.t.Errorf("writer did not stop gracefully: %v", requestErr)
	}
	f.writer = nil
}

func (f *historyProcessFixture) eventually(ready func() bool) {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ready() {
		if time.Now().After(deadline) {
			f.t.Fatal("asynchronous observation did not arrive")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (f *historyProcessFixture) request(requests <-chan []byte) []byte {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	body, err := receiveHistoryRequest(ctx, requests)
	if err != nil {
		f.t.Fatalf("fixture governance request was not observed: %v", err)
	}
	return body
}

func receiveHistoryRequest(ctx context.Context, requests <-chan []byte) ([]byte, error) {
	select {
	case body, open := <-requests:
		if !open {
			return nil, errors.New("fixture request stream closed")
		}
		return body, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *historyProcessFixture) page(repo string) history.Page {
	f.t.Helper()
	out, stderr, code := f.run(repo, "client", "history", "list", "--format", "json")
	if code != 0 {
		f.t.Fatalf("history list: exit=%d stderr=%s", code, stderr)
	}
	var page history.Page
	if err := json.Unmarshal([]byte(out), &page); err != nil {
		f.t.Fatal(err)
	}
	return page
}

func (f *historyProcessFixture) show(repo, id string) history.Record {
	f.t.Helper()
	out, stderr, code := f.run(repo, "client", "history", "show", id, "--format", "json")
	if code != 0 {
		f.t.Fatalf("history show: exit=%d stderr=%s", code, stderr)
	}
	var record history.Record
	if err := json.Unmarshal([]byte(out), &record); err != nil {
		f.t.Fatal(err)
	}
	return record
}

func (f *historyProcessFixture) validate(repo, addr, source string, extra ...string) (string, string, int) {
	f.t.Helper()
	file := filepath.Join(f.t.TempDir(), "proposal")
	f.write(file, source)
	args := []string{"client", "validate", "--addr", addr, "--repo", "logical-governance-key", "--file", "example.go", "--language", "go", "--content-file", file, "--history-worktree", repo}
	return f.run(repo, append(args, extra...)...)
}

func historyFixtureServer(t *testing.T, result string) (*httptest.Server, chan []byte) {
	t.Helper()
	requests := make(chan []byte, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/check" {
			_, _ = io.WriteString(w, `{"status":"ok"}`)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read failure", 500)
			return
		}
		requests <- body
		_, _ = io.WriteString(w, result)
	}))
	t.Cleanup(server.Close)
	return server, requests
}

func historyWorktreeScenario(t *testing.T, f *historyProcessFixture) {
	linked := filepath.Join(t.TempDir(), "linked")
	f.git(f.repo, "worktree", "add", "-b", "revised", linked)
	block, requests := historyFixtureServer(t, `{"status":"block","violations":[],"future":{"number":12345678901234567890}}`)
	pass, passRequests := historyFixtureServer(t, `{"status":"pass","violations":[],"future":[null,"λ"]}`)
	f.start()
	before := "package fixture\r\n// proposed λ\r\n"
	out, stderr, code := f.validate(f.repo, block.URL, before, "--dry-run", "--history-source", "agent", "--history-tool", "codex", "--history-action", "Write", "--history-session-id", "session-1")
	if code != 0 || stderr != "" || !strings.Contains(out, `"status":"block"`) {
		t.Fatalf("block validation: %d %q %q", code, out, stderr)
	}
	sent := f.request(requests)
	f.eventually(func() bool { return len(f.page(f.repo).Records) == 1 })
	first := f.show(f.repo, f.page(f.repo).Records[0].EventID)
	if !bytes.Equal(first.RequestJSON, compactHistoryJSON(t, sent)) || !bytes.Equal(first.ResultJSON, compactHistoryJSON(t, []byte(out))) {
		t.Fatal("submitted request or unknown verdict values changed")
	}
	if first.Branch == nil || *first.Branch != "main" || first.HeadOID == nil || first.Worktree != f.git(f.repo, "rev-parse", "--show-toplevel") || first.Source != "agent" || first.Tool == nil || *first.Tool != "codex" || first.Action == nil || *first.Action != "Write" || first.SessionID == nil || *first.SessionID != "session-1" {
		t.Fatalf("origin lost: %+v", first.Event)
	}
	out, stderr, code = f.validate(linked, pass.URL, "", "--dry-run")
	if code != 0 || stderr != "" {
		t.Fatalf("empty proposal: %d %s", code, stderr)
	}
	passSent := f.request(passRequests)
	f.eventually(func() bool { return len(f.page(linked).Records) == 2 })
	second := f.show(linked, f.page(linked).Records[0].EventID)
	if !bytes.Equal(second.RequestJSON, compactHistoryJSON(t, passSent)) || second.Status != "pass" || !second.DryRun || second.Tool != nil || second.Action != nil || second.SessionID != nil || second.Source != "manual" || second.Branch == nil || *second.Branch != "revised" {
		t.Fatalf("revised proposal context changed: %+v", second.Event)
	}
	for _, repo := range []string{f.repo, linked} {
		if len(f.page(repo).Records) != 2 {
			t.Fatal("worktrees do not share history")
		}
		if f.show(repo, first.EventID).Status != "block" {
			t.Fatal("explicit show lost first verdict")
		}
		diff, stderr, code := f.run(repo, "client", "history", "diff", first.EventID, second.EventID)
		if code != 0 || stderr != "" || !strings.Contains(diff, "From proposal:") || !strings.Contains(diff, "To proposal:") || !strings.Contains(diff, "-// proposed λ") || !strings.Contains(diff, "Status: pass") {
			t.Fatalf("proposal diff: %d %s %s", code, diff, stderr)
		}
	}
	f.stop()
	if len(f.page(linked).Records) != 2 {
		t.Fatal("read depends on writer")
	}
	f.start()
	f.stop()
	f.git(f.repo, "worktree", "remove", linked)
	if f.show(f.repo, second.EventID).EventID != second.EventID {
		t.Fatal("worktree removal lost history")
	}
	clone := filepath.Join(t.TempDir(), "clone")
	f.git(f.repo, "clone", f.repo, clone)
	if len(f.page(clone).Records) != 0 {
		t.Fatal("history escaped its clone")
	}
	if _, err := os.Stat(filepath.Join(clone, ".git", "agent-fitness-functions")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty history read mutated clone: %v", err)
	}
	// Replay an old-dated event through the real receiver. Duplicate IDs remain one
	// record and a truncated frame never becomes a record.
	old := first.Event
	old.EventID = "11223344556677889900aabbccddeeff"
	old.CompletedAt = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	f.start()
	frame, err := historyipc.EncodeValidation(old)
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range [][]byte{frame, frame, frame[:len(frame)-1]} {
		conn, err := net.DialTimeout("unix", f.socket, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
	}
	f.eventually(func() bool { return len(f.page(f.repo).Records) == 3 })
	f.eventually(func() bool {
		data, _ := os.ReadFile(f.logs)
		return strings.Contains(string(data), `"phase":"receive"`)
	})
	f.stop()
	if len(f.page(f.repo).Records) != 3 || !f.show(f.repo, old.EventID).CompletedAt.Equal(old.CompletedAt) {
		t.Fatal("duplicate, truncated frame, or expiration changed history")
	}
}

func compactHistoryJSON(t *testing.T, raw []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := json.Compact(&out, raw); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func historyStagedScenario(t *testing.T, f *historyProcessFixture) {
	f.start()
	server, requests := historyFixtureServer(t, `{"status":"pass","violations":[]}`)
	const staged = "package staged\n// λ\n"
	f.write(filepath.Join(f.repo, "example.go"), staged)
	f.git(f.repo, "add", "example.go")
	f.write(filepath.Join(f.repo, "example.go"), "package unstaged\n")
	// Execute the repository's actual hook with the actual product binary.
	cmd := exec.Command("bash", filepath.Join(f.root, "hooks/pre-commit.sh"))
	cmd.Dir = f.repo
	cmd.Env = append(f.env, "AGENT_FITNESS_FUNCTIONS_BIN="+f.binary, "AGENT_FITNESS_FUNCTIONS_ADDR="+server.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pre-commit: %v\n%s", err, out)
	}
	sent := f.request(requests)
	f.eventually(func() bool { return len(f.page(f.repo).Records) == 1 })
	record := f.show(f.repo, f.page(f.repo).Records[0].EventID)
	var request struct {
		ProposedContent string `json:"proposed_content"`
	}
	if err := json.Unmarshal(record.RequestJSON, &request); err != nil {
		t.Fatal(err)
	}
	if request.ProposedContent != staged || !bytes.Equal(record.RequestJSON, compactHistoryJSON(t, sent)) || record.Source != "git" || record.Action == nil || *record.Action != "pre-commit" || record.DryRun {
		t.Fatalf("staged capture changed: %+v content=%q", record.Event, request.ProposedContent)
	}
}

func historyStorageFailureScenario(t *testing.T, f *historyProcessFixture) {
	const verdict = `{"status":"block","violations":[],"future":"unchanged"}`
	server, requests := historyFixtureServer(t, verdict)
	baseline, baseErr, baseCode := f.validate(f.repo, server.URL, "package fixture\n")
	if baseline != verdict {
		t.Fatalf("baseline output=%q", baseline)
	}
	f.request(requests)
	if _, err := os.Stat(f.socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("validation started writer: %v", err)
	}
	f.start()
	f.validate(f.repo, server.URL, "package fixture\n")
	f.request(requests)
	f.eventually(func() bool { return len(f.page(f.repo).Records) == 1 })
	location, err := history.ResolveCheckout(context.Background(), f.repo)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", location.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("ROLLBACK")
	// The lock is held until validation AND the writer's failed insert are observed.
	// Completing here proves validation has no dependency on lock release.
	got, gotErr, gotCode := f.validate(f.repo, server.URL, "package locked\n")
	f.request(requests)
	if got != baseline || gotErr != baseErr || gotCode != baseCode {
		t.Fatalf("storage changed output/exit: %q %q %d", got, gotErr, gotCode)
	}
	f.eventually(func() bool {
		data, _ := os.ReadFile(f.logs)
		return strings.Contains(string(data), `"phase":"persist"`)
	})
	if _, err := db.Exec("ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	f.stop()
	f.start()
	f.stop()
	if len(f.page(f.repo).Records) != 1 {
		t.Fatal("dropped event was retried or replayed")
	}
	if len(requests) != 0 {
		t.Fatal("history retried validation")
	}
}

func historyNonVerdictScenario(t *testing.T, f *historyProcessFixture) {
	f.start()
	for _, tc := range []struct {
		name, result, kind string
		timeout            bool
	}{
		{name: "warming", result: `{"status":"pass","warming":true}`, kind: "warming"},
		{name: "malformed", result: `{"private":"RESPONSE_SECRET"`, kind: "invalid_response"},
		{name: "timeout", kind: "timeout", timeout: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := *f
			fixture.t = t
			f := &fixture
			logPath := filepath.Join(t.TempDir(), "events")
			oldEnv := f.env
			f.env = append(append([]string{}, f.env...), "HISTORY_TEST_LOG="+logPath)
			defer func() { f.env = oldEnv }()
			requests := make(chan []byte, 4)
			release, cancel := context.WithCancel(context.Background())
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/check" {
					_, _ = io.WriteString(w, `{"status":"ok"}`)
					return
				}
				requests <- nil
				if tc.timeout {
					<-release.Done()
					return
				}
				_, _ = io.WriteString(w, tc.result)
			}))
			defer server.Close()
			defer cancel()
			out, _, code := f.validate(f.repo, server.URL, "SOURCE_SECRET", "--timeout", "100ms")
			cancel()
			f.request(requests)
			if tc.timeout && (code == 0 || !strings.Contains(out, `"status":"error"`)) {
				t.Fatalf("timeout behavior: code=%d output=%s", code, out)
			}
			f.eventually(func() bool { data, _ := os.ReadFile(logPath); return len(data) > 0 })
			data, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) != 1 || strings.Contains(string(data), "SOURCE_SECRET") || strings.Contains(string(data), "RESPONSE_SECRET") {
				t.Fatalf("unsafe or duplicate diagnostics: %s", data)
			}
			var diag osevent.Diagnostic
			if err := json.Unmarshal([]byte(lines[0]), &diag); err != nil {
				t.Fatal(err)
			}
			if diag.ErrorKind != tc.kind || diag.Repository != "logical-governance-key" {
				t.Fatalf("diagnostic=%+v", diag)
			}
			if len(f.page(f.repo).Records) != 0 || len(requests) != 0 {
				t.Fatal("non-verdict was persisted or retried")
			}
			if _, err := os.Stat(filepath.Join(f.repo, ".git", "agent-fitness-functions")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("non-verdict created storage: %v", err)
			}
		})
	}
}
