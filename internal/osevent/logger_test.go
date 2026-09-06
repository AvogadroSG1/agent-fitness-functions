//go:build darwin || linux

package osevent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestValidationLoggerStartsAndReleasesExactlyOnce(t *testing.T) {
	runner := &fakeRunner{}
	logger := newLogger(func(string) (string, error) { return "/fixture/logger", nil }, runner)
	diagnostic := Diagnostic{
		Name: "history_publish_failed", Time: time.Date(2026, 9, 6, 12, 0, 0, 0, time.FixedZone("offset", 3600)),
		ErrorKind: "unavailable", Phase: "publish", EventID: "event-1", Repository: "repo",
		Worktree: "/checkout", Tool: "codex", SessionID: "session-1",
	}
	if err := logger.logValidation(diagnostic); err != nil {
		t.Fatal(err)
	}
	if runner.startCalls != 1 || runner.runCalls != 0 || runner.process.releaseCalls != 1 {
		t.Fatalf("start/run/release = %d/%d/%d", runner.startCalls, runner.runCalls, runner.process.releaseCalls)
	}
	assertInvocation(t, runner.name, runner.args, diagnostic)
}

func TestServiceLoggerRunsAndReapsHelper(t *testing.T) {
	runner := &fakeRunner{}
	logger := newLogger(func(string) (string, error) { return "/fixture/logger", nil }, runner)
	if err := logger.logService(context.Background(), Diagnostic{Name: "history_write_failed", Phase: "persist"}); err != nil {
		t.Fatal(err)
	}
	if runner.runCalls != 1 || runner.startCalls != 0 || runner.process.releaseCalls != 0 {
		t.Fatalf("run/start/release = %d/%d/%d", runner.runCalls, runner.startCalls, runner.process.releaseCalls)
	}
	if runner.deadlineRemaining <= 0 || runner.deadlineRemaining > time.Second {
		t.Fatalf("service logger deadline remaining = %v", runner.deadlineRemaining)
	}
}

func TestLoggerResolutionAndFailuresDoNotRetry(t *testing.T) {
	resolveCalls := 0
	want := errors.New("logger absent")
	logger := newLogger(func(string) (string, error) {
		resolveCalls++
		return "", want
	}, &fakeRunner{})
	for range 2 {
		if err := logger.logValidation(Diagnostic{Name: "failure", Phase: "publish"}); !errors.Is(err, want) {
			t.Fatalf("resolution error = %v", err)
		}
	}
	if resolveCalls != 1 {
		t.Fatalf("logger resolution calls = %d, want 1", resolveCalls)
	}
}

func TestLoggerStartFailureIsReturnedOnceWithoutRelease(t *testing.T) {
	runner := &fakeRunner{startErr: errors.New("start failed")}
	logger := newLogger(func(string) (string, error) { return "/fixture/logger", nil }, runner)
	if err := logger.logValidation(Diagnostic{Name: "failure", Phase: "publish"}); err == nil {
		t.Fatal("start failure was dropped internally")
	}
	if runner.startCalls != 1 || runner.runCalls != 0 || runner.process.releaseCalls != 0 {
		t.Fatalf("start/run/release = %d/%d/%d", runner.startCalls, runner.runCalls, runner.process.releaseCalls)
	}
}

func TestDiagnosticCannotCarryPayloadsOrErrorBodies(t *testing.T) {
	typeShape, err := json.Marshal(Diagnostic{Name: "safe", Phase: "receive", Repository: "repo"})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"request_json", "result_json", "proposed_content", "error_body", "authorization"} {
		if strings.Contains(string(typeShape), forbidden) {
			t.Fatalf("diagnostic contains forbidden field %q: %s", forbidden, typeShape)
		}
	}
}

func assertInvocation(t *testing.T, name string, args []string, want Diagnostic) {
	t.Helper()
	if name != "/fixture/logger" || len(args) != 3 || args[0] != "-t" || args[1] != loggerTag {
		t.Fatalf("invocation = %q %q", name, args)
	}
	var got Diagnostic
	if err := json.Unmarshal([]byte(args[2]), &got); err != nil {
		t.Fatalf("message is not diagnostic JSON: %v", err)
	}
	if got.Name != want.Name || got.Phase != want.Phase || got.Time.Location() != time.UTC {
		t.Fatalf("diagnostic = %+v", got)
	}
}

type fakeProcess struct {
	releaseCalls int
	releaseErr   error
}

func (p *fakeProcess) Release() error {
	p.releaseCalls++
	return p.releaseErr
}

type fakeRunner struct {
	process           fakeProcess
	name              string
	args              []string
	startCalls        int
	runCalls          int
	startErr          error
	runErr            error
	deadlineRemaining time.Duration
}

func (r *fakeRunner) Start(name string, args ...string) (process, error) {
	r.startCalls++
	r.name = name
	r.args = append([]string(nil), args...)
	return &r.process, r.startErr
}

func (r *fakeRunner) Run(ctx context.Context, name string, args ...string) error {
	r.runCalls++
	r.name = name
	r.args = append([]string(nil), args...)
	if deadline, ok := ctx.Deadline(); ok {
		r.deadlineRemaining = time.Until(deadline)
	}
	return r.runErr
}
