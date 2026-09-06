//go:build darwin || linux

// Package osevent writes bounded operational diagnostics to the native OS event stream.
package osevent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

const (
	loggerTag      = "agent-fitness-functions"
	serviceTimeout = time.Second
)

// Diagnostic is the complete permitted OS event shape. It deliberately has no
// source payload, response body, credential, or arbitrary error-text field.
type Diagnostic struct {
	Name       string    `json:"name"`
	Time       time.Time `json:"time"`
	ErrorKind  string    `json:"error_kind,omitempty"`
	Phase      string    `json:"phase"`
	EventID    string    `json:"event_id,omitempty"`
	Repository string    `json:"repository,omitempty"`
	Worktree   string    `json:"worktree,omitempty"`
	Tool       string    `json:"tool,omitempty"`
	SessionID  string    `json:"session_id,omitempty"`
}

type process interface {
	Release() error
}

type commandRunner interface {
	Start(name string, args ...string) (process, error)
	Run(ctx context.Context, name string, args ...string) error
}

type logger struct {
	once    sync.Once
	resolve func(string) (string, error)
	runner  commandRunner
	path    string
	err     error
}

type execRunner struct{}

var nativeLogger = newLogger(exec.LookPath, execRunner{})

// LogValidation starts one helper and releases its process handle without waiting.
// Callers SHOULD treat its error as a dropped observability event.
func LogValidation(diagnostic Diagnostic) error {
	return nativeLogger.logValidation(diagnostic)
}

// LogService runs and reaps one helper under a one-second timeout.
// Callers SHOULD treat its error as a dropped observability event.
func LogService(ctx context.Context, diagnostic Diagnostic) error {
	return nativeLogger.logService(ctx, diagnostic)
}

func newLogger(resolve func(string) (string, error), runner commandRunner) *logger {
	return &logger{resolve: resolve, runner: runner}
}

func (l *logger) logValidation(diagnostic Diagnostic) error {
	path, args, err := l.invocation(diagnostic)
	if err != nil {
		return err
	}
	process, err := l.runner.Start(path, args...)
	if err != nil {
		return fmt.Errorf("start native logger: %w", err)
	}
	if err := process.Release(); err != nil {
		return fmt.Errorf("release native logger process: %w", err)
	}
	return nil
}

func (l *logger) logService(ctx context.Context, diagnostic Diagnostic) error {
	path, args, err := l.invocation(diagnostic)
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithTimeout(ctx, serviceTimeout)
	defer cancel()
	if err := l.runner.Run(runCtx, path, args...); err != nil {
		return fmt.Errorf("run native logger: %w", err)
	}
	return nil
}

func (l *logger) invocation(diagnostic Diagnostic) (string, []string, error) {
	path, err := l.executable()
	if err != nil {
		return "", nil, err
	}
	message, err := encodeDiagnostic(diagnostic)
	if err != nil {
		return "", nil, err
	}
	return path, []string{"-t", loggerTag, string(message)}, nil
}

func (l *logger) executable() (string, error) {
	l.once.Do(func() {
		l.path, l.err = l.resolve("logger")
	})
	if l.err != nil {
		return "", fmt.Errorf("resolve native logger: %w", l.err)
	}
	if l.path == "" {
		return "", errors.New("osevent: resolved native logger path is empty")
	}
	return l.path, nil
}

func encodeDiagnostic(diagnostic Diagnostic) ([]byte, error) {
	if diagnostic.Name == "" || diagnostic.Phase == "" {
		return nil, errors.New("osevent: diagnostic requires name and phase")
	}
	if diagnostic.Time.IsZero() {
		diagnostic.Time = time.Now()
	}
	diagnostic.Time = diagnostic.Time.UTC()
	message, err := json.Marshal(diagnostic)
	if err != nil {
		return nil, fmt.Errorf("encode OS diagnostic: %w", err)
	}
	return message, nil
}

func (execRunner) Start(name string, args ...string) (process, error) {
	command := exec.Command(name, args...)
	command.Stdin = nil
	command.Stdout = nil
	command.Stderr = nil
	if err := command.Start(); err != nil {
		return nil, err
	}
	return command.Process, nil
}

func (execRunner) Run(ctx context.Context, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin = nil
	command.Stdout = nil
	command.Stderr = nil
	return command.Run()
}

var _ process = (*os.Process)(nil)
