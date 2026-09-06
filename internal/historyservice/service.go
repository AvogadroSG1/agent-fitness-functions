//go:build darwin || linux

// Package historyservice owns the user-local, serial validation history writer.
package historyservice

import (
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/buildinfo"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/history"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/historyipc"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/osevent"
)

// Config configures a foreground writer. Insert and Log MAY be supplied by tests.
type Config struct {
	SocketPath string
	Insert     func(context.Context, history.Event) error
	Log        func(context.Context, osevent.Diagnostic) error
}

type service struct {
	listener *net.UnixListener
	receiver *historyipc.Receiver
	identity historyipc.Identity
	cfg      Config
	stopOnce sync.Once
}

// Run serves until shutdown or cancellation, joining all work before releasing ownership.
func Run(ctx context.Context, cfg Config) (err error) {
	if cfg.SocketPath == "" {
		cfg.SocketPath = SocketPath(os.Getenv)
	}
	if cfg.Insert == nil {
		cfg.Insert = history.Insert
	}
	if cfg.Log == nil {
		cfg.Log = osevent.LogService
	}
	endpoint, err := acquireEndpoint(cfg.SocketPath)
	if err != nil {
		logFailure(ctx, cfg.Log, "startup", "endpoint")
		return err
	}
	defer func() { err = errors.Join(err, endpoint.close()) }()
	listener, err := historyipc.Listen(cfg.SocketPath)
	if err != nil {
		logFailure(ctx, cfg.Log, "startup", "listen")
		return err
	}
	// Cleanup compares the recorded inode; net's default unlink could remove a replacement.
	listener.SetUnlinkOnClose(false)
	if err := endpoint.recordSocket(); err != nil {
		logFailure(ctx, cfg.Log, "startup", "ownership_record")
		return errors.Join(err, listener.Close())
	}
	revision, modified := buildinfo.Current()
	s := &service{listener: listener, cfg: cfg, identity: historyipc.Identity{
		BuildRevision: revision, Modified: modified, PID: os.Getpid(), StartedAt: time.Now().UTC(),
	}}
	s.receiver = historyipc.NewReceiver(s.control)
	return s.serve(ctx)
}

func (s *service) stop() {
	s.stopOnce.Do(func() {
		s.receiver.Stop()
		if err := s.listener.Close(); err != nil {
			logFailure(context.Background(), s.cfg.Log, "shutdown", "listener")
		}
	})
}

func (s *service) control(ctx context.Context, message historyipc.Message) ([]byte, error) {
	if message.Kind == historyipc.KindIdentify {
		return historyipc.EncodeIdentity(s.identity)
	}
	s.stop()
	return historyipc.EncodeShutdownResponse()
}

func (s *service) serve(ctx context.Context) error {
	receiveCtx, cancelReceivers := context.WithCancel(ctx)
	defer cancelReceivers()
	writeCtx, cancelWrites := context.WithCancel(ctx)
	defer cancelWrites()
	doneWriting := make(chan struct{})
	go func() { defer close(doneWriting); s.persist(writeCtx) }()
	stopCancellation := context.AfterFunc(ctx, s.stop)
	defer stopCancellation()
	var receivers sync.WaitGroup
	err := s.accept(receiveCtx, &receivers)
	s.stop()
	// Graceful controls MUST flush their ACK before receiver cancellation.
	receivers.Wait()
	cancelReceivers()
	cancelWrites()
	<-doneWriting
	s.releaseQueued()
	return err
}

func (s *service) accept(ctx context.Context, receivers *sync.WaitGroup) error {
	for {
		connection, err := s.listener.AcceptUnix()
		if errors.Is(err, net.ErrClosed) {
			return nil
		}
		if err != nil {
			logFailure(ctx, s.cfg.Log, "receive", "accept")
			return err
		}
		admission, ok := s.receiver.Admit()
		if !ok {
			logFailure(ctx, s.cfg.Log, "receive", "overloaded")
			if err := connection.Close(); err != nil {
				logFailure(ctx, s.cfg.Log, "receive", "close")
			}
			continue
		}
		receivers.Add(1)
		go func() {
			defer receivers.Done()
			if err := admission.Receive(ctx, connection); err != nil {
				logFailure(ctx, s.cfg.Log, "receive", "frame")
			}
		}()
	}
}

func (s *service) persist(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case delivery := <-s.receiver.Completed():
			if ctx.Err() == nil {
				if err := s.cfg.Insert(ctx, *delivery.Message.Event); err != nil {
					logPersistenceFailure(ctx, s.cfg.Log, *delivery.Message.Event)
				}
			}
			delivery.Release()
		}
	}
}

func (s *service) releaseQueued() {
	for {
		select {
		case delivery := <-s.receiver.Completed():
			delivery.Release()
		default:
			return
		}
	}
}

func logFailure(ctx context.Context, log func(context.Context, osevent.Diagnostic) error, phase, category string) {
	logDiagnostic(ctx, log, osevent.Diagnostic{Name: "history_writer_error", Phase: phase, ErrorKind: category})
}

func logPersistenceFailure(ctx context.Context, log func(context.Context, osevent.Diagnostic) error, event history.Event) {
	diagnostic := osevent.Diagnostic{Name: "history_writer_error", Phase: "persist", ErrorKind: "insert",
		EventID: event.EventID, Repository: event.Repository, Worktree: event.Worktree}
	if event.Tool != nil {
		diagnostic.Tool = *event.Tool
	}
	if event.SessionID != nil {
		diagnostic.SessionID = *event.SessionID
	}
	logDiagnostic(ctx, log, diagnostic)
}

func logDiagnostic(ctx context.Context, log func(context.Context, osevent.Diagnostic) error, diagnostic osevent.Diagnostic) {
	// Operational diagnostics MUST NOT copy error text, source, or response bodies.
	if err := log(ctx, diagnostic); err != nil {
		// Logging is best-effort; recursion or stderr fallback could disclose payloads.
		return
	}
}
