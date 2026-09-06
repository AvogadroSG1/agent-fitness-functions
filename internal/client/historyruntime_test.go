package client

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/historyipc"
)

func TestHistoryCurrentDoesNotRestart(t *testing.T) {
	runtime := historyRuntimeFixture()
	starts := 0
	runtime.Start = func() error { starts++; return nil }
	if err := runtime.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if starts != 0 {
		t.Fatal("current writer restarted")
	}
}

func TestHistoryShutdownMustReleaseBeforeStart(t *testing.T) {
	runtime := historyRuntimeFixture()
	runtime.Revision = "new"
	runtime.Released = func(string) (bool, error) { return false, nil }
	starts := 0
	runtime.Start = func() error { starts++; return nil }
	ctx, cancel := context.WithCancel(context.Background())
	runtime.Request = func(ctx context.Context, path, kind string) (historyipc.Message, error) {
		if kind == historyipc.KindShutdown {
			cancel()
			return historyipc.Message{Accepted: true}, nil
		}
		return historyipc.Message{Identity: &historyipc.Identity{BuildRevision: "old"}}, nil
	}
	if err := runtime.Reconcile(ctx); err == nil {
		t.Fatal("unconfirmed shutdown succeeded")
	}
	if starts != 0 {
		t.Fatal("second writer launched while ownership held")
	}
}

func TestHistoryAbsentStartsOnce(t *testing.T) {
	runtime := historyRuntimeFixture()
	started := false
	runtime.Request = func(context.Context, string, string) (historyipc.Message, error) {
		if !started {
			return historyipc.Message{}, syscall.ENOENT
		}
		return historyipc.Message{Identity: &historyipc.Identity{BuildRevision: "current"}}, nil
	}
	runtime.Start = func() error {
		if started {
			t.Fatal("duplicate start")
		}
		started = true
		return nil
	}
	if err := runtime.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !started {
		t.Fatal("absent writer was not started")
	}
}

func TestHistoryForeignResponseDoesNotStartOrStop(t *testing.T) {
	runtime := historyRuntimeFixture()
	runtime.Request = func(context.Context, string, string) (historyipc.Message, error) {
		return historyipc.Message{}, errors.New("foreign response")
	}
	runtime.Start = func() error { t.Fatal("foreign endpoint started"); return nil }
	if err := runtime.Reconcile(context.Background()); err == nil {
		t.Fatal("foreign response accepted")
	}
}

func historyRuntimeFixture() HistoryRuntime {
	return HistoryRuntime{
		SocketPath: "fixture", Revision: "current",
		Request: func(_ context.Context, _, kind string) (historyipc.Message, error) {
			if kind == historyipc.KindShutdown {
				return historyipc.Message{Accepted: true}, nil
			}
			return historyipc.Message{Identity: &historyipc.Identity{BuildRevision: "current", PID: 1, StartedAt: time.Now()}}, nil
		},
		Released: func(string) (bool, error) { return true, nil },
	}
}

func TestKnownStaleHistoryStopsThenStartsCurrent(t *testing.T) {
	runtime := historyRuntimeFixture()
	runtime.Revision = "new"
	shutdown := false
	released := false
	started := false
	revision := "old"
	runtime.Request = func(_ context.Context, _, kind string) (historyipc.Message, error) {
		if kind == historyipc.KindShutdown {
			shutdown = true
			return historyipc.Message{Accepted: true}, nil
		}
		return historyipc.Message{Identity: &historyipc.Identity{BuildRevision: revision}}, nil
	}
	runtime.Released = func(string) (bool, error) { released = shutdown; return released, nil }
	runtime.Start = func() error {
		if !shutdown || !released {
			t.Fatal("writer started before shutdown and release")
		}
		started = true
		revision = "new"
		return nil
	}
	if err := runtime.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !started {
		t.Fatal("stale writer was not replaced")
	}
}

func TestDoctorHistoryStatusIsReadOnlyWarning(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENT_FITNESS_FUNCTIONS_HISTORY_SOCKET", filepath.Join(dir, "absent.sock"))
	result := checkHistoryWriter()
	if !result.warning || result.passed {
		t.Fatalf("status=%+v", result)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("doctor changed state: %v %v", entries, err)
	}
}
