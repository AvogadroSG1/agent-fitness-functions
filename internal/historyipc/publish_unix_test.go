//go:build darwin || linux

package historyipc

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestPublishFrameStopsAtFirstNonprogress(t *testing.T) {
	tests := []struct {
		name       string
		writes     []writeResult
		expected   int
		expectedOp string
	}{
		{name: "eagain after partial write", writes: []writeResult{{n: 3}, {err: unix.EAGAIN}}, expected: 2, expectedOp: "write"},
		{name: "zero progress", writes: []writeResult{{}}, expected: 1, expectedOp: "write"},
		{name: "pending connect", expectedOp: "connect"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			socket := &fakePublisherSocket{writes: tt.writes}
			if tt.name == "pending connect" {
				socket.connectErr = unix.EINPROGRESS
			}
			if err := publishFrame(socket, "/writer.sock", []byte("complete")); err == nil {
				t.Fatal("nonprogress reported success")
			}
			if len(socket.writeCalls) != tt.expected {
				t.Fatalf("write calls = %d, want %d", len(socket.writeCalls), tt.expected)
			}
			if socket.closeCalls != 1 {
				t.Fatalf("close calls = %d, want 1", socket.closeCalls)
			}
			if !socket.called(tt.expectedOp) {
				t.Fatalf("expected %s call, got %v", tt.expectedOp, socket.calls)
			}
		})
	}
}

func TestPublishFrameDropsWhenSendBufferRequestFails(t *testing.T) {
	socket := &fakePublisherSocket{bufferErr: errors.New("denied")}
	if err := publishFrame(socket, "/writer.sock", []byte("frame")); err == nil {
		t.Fatal("send buffer failure reported success")
	}
	if socket.called("connect") || socket.called("write") || socket.closeCalls != 1 {
		t.Fatalf("calls after buffer failure = %v, closes = %d", socket.calls, socket.closeCalls)
	}
}

func TestOpenRawSocketMarksDescriptorCloseOnExec(t *testing.T) {
	socket, err := openRawSocket()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := socket.Close(); err != nil {
			t.Errorf("close raw socket: %v", err)
		}
	}()
	flags, err := unix.FcntlInt(uintptr(socket.fd), unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}
	if flags&unix.FD_CLOEXEC == 0 {
		t.Fatal("publisher socket descriptor is inheritable")
	}
}

func TestSocketOperationsRejectTooLongPathsClearly(t *testing.T) {
	path := "/" + strings.Repeat("x", len(unix.RawSockaddrUnix{}.Path))
	if _, err := Listen(path); err == nil || !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("Listen error = %v, want explicit maximum", err)
	}
	if _, err := RequestControl(context.Background(), path, KindIdentify); err == nil || !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("RequestControl error = %v, want explicit maximum", err)
	}
}

func TestControlResponseMustMatchRequest(t *testing.T) {
	if err := validateControlResponse(KindIdentify, Message{Kind: KindShutdown, Accepted: true}); err == nil {
		t.Fatal("identify request accepted shutdown response")
	}
	if err := validateControlResponse(KindShutdown, Message{Kind: KindIdentity, Identity: &Identity{}}); err == nil {
		t.Fatal("shutdown request accepted identity response")
	}
}

func TestPublishDeliversOneCompleteSmallFrame(t *testing.T) {
	path := shortSocketPath(t)
	listener, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("socket permissions = %04o, want 0600", got)
	}
	receiver := NewReceiver()
	received := make(chan error, 1)
	go func() {
		conn, err := listener.AcceptUnix()
		if err != nil {
			received <- err
			return
		}
		admission, ok := receiver.Admit()
		if !ok {
			conn.Close()
			received <- errors.New("receiver refused admission")
			return
		}
		received <- admission.Receive(context.Background(), conn)
	}()
	if err := Publish(path, protocolEvent()); err != nil {
		t.Fatal(err)
	}
	if err := <-received; err != nil {
		t.Fatal(err)
	}
	select {
	case delivery := <-receiver.Completed():
		if delivery.Message.Event == nil || delivery.Message.Event.EventID != protocolEvent().EventID {
			t.Fatalf("delivery = %+v", delivery.Message)
		}
		delivery.Release()
	case <-time.After(time.Second):
		t.Fatal("complete publication was not delivered")
	}
}

func TestRequestControlClosesWriteBeforeReadingResponse(t *testing.T) {
	path := shortSocketPath(t)
	listener, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	receiver := NewReceiver(func(_ context.Context, message Message) ([]byte, error) {
		if message.Kind != KindIdentify {
			return nil, errors.New("unexpected control kind")
		}
		return EncodeIdentity(Identity{PID: 42, StartedAt: time.Now()})
	})
	serverDone := make(chan error, 1)
	go func() {
		connection, err := listener.AcceptUnix()
		if err != nil {
			serverDone <- err
			return
		}
		admission, ok := receiver.Admit()
		if !ok {
			connection.Close()
			serverDone <- errors.New("receiver refused control")
			return
		}
		serverDone <- admission.Receive(context.Background(), connection)
	}()
	message, err := RequestControl(context.Background(), path, KindIdentify)
	if err != nil {
		t.Fatal(err)
	}
	if message.Identity == nil || message.Identity.PID != 42 {
		t.Fatalf("control response = %+v", message)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	select {
	case delivery := <-receiver.Completed():
		delivery.Release()
		t.Fatal("control entered the validation persistence queue")
	default:
	}
}

func TestRequestControlCancellationReleasesBlockedResponseRead(t *testing.T) {
	path := shortSocketPath(t)
	listener, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	requestRead := make(chan error, 1)
	releasePeer := make(chan struct{})
	serverDone := make(chan error, 1)
	go func() {
		connection, err := listener.AcceptUnix()
		if err != nil {
			serverDone <- err
			return
		}
		_, readErr := io.ReadAll(connection)
		requestRead <- readErr
		<-releasePeer
		serverDone <- connection.Close()
	}()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := RequestControl(ctx, path, KindIdentify)
		result <- err
	}()
	if err := <-requestRead; err != nil {
		close(releasePeer)
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			close(releasePeer)
			t.Fatalf("RequestControl error = %v, want context canceled", err)
		}
	case <-time.After(500 * time.Millisecond):
		close(releasePeer)
		<-result
		t.Fatal("RequestControl remained blocked after cancellation")
	}
	close(releasePeer)
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func shortSocketPath(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp("", "afh-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Errorf("remove socket fixture: %v", err)
		}
	})
	return directory + "/w.sock"
}

type writeResult struct {
	n   int
	err error
}

type fakePublisherSocket struct {
	bufferErr   error
	connectErr  error
	writes      []writeResult
	writeCalls  [][]byte
	calls       []string
	closeCalls  int
	shutdownErr error
}

func (s *fakePublisherSocket) SetSendBuffer(int) error {
	s.calls = append(s.calls, "buffer")
	return s.bufferErr
}

func (s *fakePublisherSocket) Connect(string) error {
	s.calls = append(s.calls, "connect")
	return s.connectErr
}

func (s *fakePublisherSocket) Write(payload []byte) (int, error) {
	s.calls = append(s.calls, "write")
	s.writeCalls = append(s.writeCalls, append([]byte(nil), payload...))
	if len(s.writes) == 0 {
		return len(payload), nil
	}
	result := s.writes[0]
	s.writes = s.writes[1:]
	return result.n, result.err
}

func (s *fakePublisherSocket) CloseWrite() error {
	s.calls = append(s.calls, "close-write")
	return s.shutdownErr
}

func (s *fakePublisherSocket) Close() error {
	s.calls = append(s.calls, "close")
	s.closeCalls++
	return nil
}

func (s *fakePublisherSocket) called(name string) bool {
	for _, call := range s.calls {
		if call == name {
			return true
		}
	}
	return false
}
