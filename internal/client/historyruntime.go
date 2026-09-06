package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/buildinfo"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/historyipc"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/historyservice"
)

// HistoryRuntime reconciles the local writer independently of governance transport.
// Dependencies MUST be immutable during use; lifecycle requests have a five-second ceiling.
type HistoryRuntime struct {
	SocketPath string
	Revision   string
	Modified   bool
	Start      func() error
	Request    func(context.Context, string, string) (historyipc.Message, error)
	Released   func(string) (bool, error)
}

// NewHistoryRuntime composes the actual user-local writer lifecycle.
func NewHistoryRuntime(getenv func(string) string) HistoryRuntime {
	revision, modified := buildinfo.Current()
	return HistoryRuntime{SocketPath: historyservice.SocketPath(getenv), Revision: revision, Modified: modified,
		Start: startHistoryWriter, Request: historyipc.RequestControl, Released: historyservice.Released}
}

// Reconcile starts an absent writer or gracefully replaces a known stale build.
// Callers MUST invoke it only after successful onboarding, never from validation.
func (r HistoryRuntime) Reconcile(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	message, err := r.Request(ctx, r.SocketPath, historyipc.KindIdentify)
	if err == nil {
		if r.current(message.Identity) {
			return nil
		}
		if !r.knownBuild(message.Identity) {
			return errors.New("history writer build cannot be verified")
		}
		if err := r.shutdown(ctx); err != nil {
			return err
		}
	} else if !absentHistory(err) {
		return errors.New("history writer identity cannot be verified")
	}
	if err := r.waitReleased(ctx); err != nil {
		return err
	}
	if err := r.Start(); err != nil {
		return errors.New("history writer could not start")
	}
	return r.waitReady(ctx)
}

func (r HistoryRuntime) knownBuild(identity *historyipc.Identity) bool {
	return identity != nil && identity.BuildRevision != "" && r.Revision != ""
}

func absentHistory(err error) bool {
	return errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED)
}

func (r HistoryRuntime) current(identity *historyipc.Identity) bool {
	return identity != nil && identity.BuildRevision == r.Revision && identity.Modified == r.Modified
}

// Stop verifies service identity, requests graceful shutdown, and confirms ownership release.
// It MUST NOT signal arbitrary PIDs or infer release from a missing listener.
func (r HistoryRuntime) Stop(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := r.Request(ctx, r.SocketPath, historyipc.KindIdentify)
	if absentHistory(err) {
		return r.waitReleased(ctx)
	}
	if err != nil {
		return errors.New("history writer identity cannot be verified")
	}
	return r.shutdown(ctx)
}

func (r HistoryRuntime) shutdown(ctx context.Context) error {
	if _, err := r.Request(ctx, r.SocketPath, historyipc.KindShutdown); err != nil {
		return errors.New("history writer shutdown was not acknowledged")
	}
	return r.waitReleased(ctx)
}

func (r HistoryRuntime) waitReleased(ctx context.Context) error {
	for {
		released, err := r.Released(r.SocketPath)
		if err != nil {
			return errors.New("history writer ownership cannot be verified")
		}
		if released {
			return nil
		}
		if err := historyPoll(ctx); err != nil {
			return errors.New("history writer still owns its endpoint; no replacement was started")
		}
	}
}

func (r HistoryRuntime) waitReady(ctx context.Context) error {
	for {
		message, err := r.Request(ctx, r.SocketPath, historyipc.KindIdentify)
		if err == nil && r.current(message.Identity) {
			return nil
		}
		if err := historyPoll(ctx); err != nil {
			return errors.New("history writer readiness was not confirmed")
		}
	}
}

func historyPoll(ctx context.Context) error {
	timer := time.NewTimer(20 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func startHistoryWriter() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	command := exec.Command(executable, "internal", "history-writer")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func reconcileHistory(runtime *HistoryRuntime, stderr io.Writer) {
	if runtime == nil {
		return
	}
	if err := runtime.Reconcile(context.Background()); err != nil {
		// Lifecycle errors carry fixed diagnostics; validation payloads never enter output.
		if _, writeErr := fmt.Fprintf(stderr, "warning: local history writer unavailable: %v; run agent-fitness-functions client onboard to retry\n", err); writeErr != nil {
			return
		}
	}
}

func checkHistoryWriter() checkResult {
	runtime := NewHistoryRuntime(os.Getenv)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	message, err := runtime.Request(ctx, runtime.SocketPath, historyipc.KindIdentify)
	result := checkResult{name: "Local history writer", warning: true, detail: "unavailable or unverified", remediation: "run agent-fitness-functions client onboard"}
	if err != nil {
		return result
	}
	if !runtime.current(message.Identity) {
		result.detail = "stale build"
		return result
	}
	result.passed = true
	result.warning = false
	result.detail = "current build"
	return result
}
