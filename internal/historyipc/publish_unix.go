//go:build darwin || linux

package historyipc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/history"
)

const (
	socketBufferSize = 6 << 20
	controlTimeout   = 5 * time.Second
)

type publisherSocket interface {
	SetSendBuffer(int) error
	Connect(string) error
	Write([]byte) (int, error)
	CloseWrite() error
	Close() error
}

type rawSocket struct {
	fd int
}

// Publish makes one nonblocking attempt to deliver a complete validation frame.
func Publish(socketPath string, event history.Event) error {
	frame, err := EncodeValidation(event)
	if err != nil {
		return err
	}
	socket, err := openRawSocket()
	if err != nil {
		return err
	}
	return publishFrame(socket, socketPath, frame)
}

func openRawSocket() (*rawSocket, error) {
	fd, err := openCloseOnExecSocket()
	if err != nil {
		return nil, err
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		closeErr := unix.Close(fd)
		return nil, errors.Join(fmt.Errorf("make history socket nonblocking: %w", err), closeErr)
	}
	return &rawSocket{fd: fd}, nil
}

func openCloseOnExecSocket() (int, error) {
	syscall.ForkLock.RLock()
	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		syscall.ForkLock.RUnlock()
		return -1, fmt.Errorf("open history socket: %w", err)
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, unix.FD_CLOEXEC); err != nil {
		closeErr := unix.Close(fd)
		syscall.ForkLock.RUnlock()
		return -1, errors.Join(fmt.Errorf("mark history socket close-on-exec: %w", err), closeErr)
	}
	syscall.ForkLock.RUnlock()
	return fd, nil
}

func publishFrame(socket publisherSocket, socketPath string, frame []byte) (err error) {
	defer func() { err = errors.Join(err, socket.Close()) }()
	if err := validateSocketPath(socketPath); err != nil {
		return err
	}
	if err := socket.SetSendBuffer(socketBufferSize); err != nil {
		return fmt.Errorf("request history send buffer: %w", err)
	}
	if err := socket.Connect(socketPath); err != nil {
		return fmt.Errorf("connect history writer: %w", err)
	}
	if err := writeImmediate(socket, frame); err != nil {
		return err
	}
	if err := socket.CloseWrite(); err != nil {
		return fmt.Errorf("close history write side: %w", err)
	}
	return nil
}

func writeImmediate(writer publisherSocket, remaining []byte) error {
	for len(remaining) != 0 {
		n, err := writer.Write(remaining)
		if err != nil {
			return fmt.Errorf("write history frame: %w", err)
		}
		if n <= 0 || n > len(remaining) {
			return errors.New("historyipc: history frame write made invalid progress")
		}
		remaining = remaining[n:]
	}
	return nil
}

func (s *rawSocket) SetSendBuffer(size int) error {
	return unix.SetsockoptInt(s.fd, unix.SOL_SOCKET, unix.SO_SNDBUF, size)
}

func (s *rawSocket) Connect(path string) error {
	return unix.Connect(s.fd, &unix.SockaddrUnix{Name: path})
}

func (s *rawSocket) Write(payload []byte) (int, error) {
	return unix.Write(s.fd, payload)
}

func (s *rawSocket) CloseWrite() error {
	return unix.Shutdown(s.fd, unix.SHUT_WR)
}

func (s *rawSocket) Close() error {
	return unix.Close(s.fd)
}

// RequestControl sends one lifecycle control and reads its framed response.
// The caller's context and the five-second protocol ceiling bound all waiting.
func RequestControl(ctx context.Context, socketPath, kind string) (message Message, err error) {
	if err := validateSocketPath(socketPath); err != nil {
		return Message{}, err
	}
	frame, err := EncodeControl(kind)
	if err != nil {
		return Message{}, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, controlTimeout)
	defer cancel()
	connection, err := dialControl(requestCtx, socketPath)
	if err != nil {
		return Message{}, err
	}
	defer func() { err = errors.Join(err, connection.Close()) }()
	stopCancellation := context.AfterFunc(requestCtx, func() {
		// The context error is returned below. Setting the deadline is best-effort
		// because a cancellation callback has no error return path.
		_ = connection.SetDeadline(time.Now())
	})
	defer stopCancellation()
	message, err = exchangeControl(connection, frame)
	if err != nil {
		if contextErr := requestCtx.Err(); contextErr != nil {
			return Message{}, contextErr
		}
		return Message{}, err
	}
	if err := validateControlResponse(kind, message); err != nil {
		return Message{}, err
	}
	return message, nil
}

func dialControl(ctx context.Context, socketPath string) (*net.UnixConn, error) {
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("connect history control: %w", err)
	}
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		closeErr := connection.Close()
		return nil, errors.Join(errors.New("historyipc: control connection is not unix"), closeErr)
	}
	deadline, _ := ctx.Deadline()
	if err := unixConnection.SetDeadline(deadline); err != nil {
		closeErr := unixConnection.Close()
		return nil, errors.Join(fmt.Errorf("set history control deadline: %w", err), closeErr)
	}
	return unixConnection, nil
}

func exchangeControl(connection *net.UnixConn, frame []byte) (Message, error) {
	if err := writeAll(connection, frame); err != nil {
		return Message{}, err
	}
	if err := connection.CloseWrite(); err != nil {
		return Message{}, fmt.Errorf("close history control write side: %w", err)
	}
	return DecodeFrame(connection)
}

func validateControlResponse(requestKind string, message Message) error {
	if requestKind == KindIdentify && message.Kind == KindIdentity && message.Identity != nil {
		return nil
	}
	if requestKind == KindShutdown && message.Kind == KindShutdown && message.Accepted {
		return nil
	}
	return fmt.Errorf("history control %q received mismatched %q response", requestKind, message.Kind)
}

func writeAll(writer net.Conn, remaining []byte) error {
	for len(remaining) != 0 {
		n, err := writer.Write(remaining)
		if err != nil {
			return fmt.Errorf("write history control: %w", err)
		}
		if n <= 0 || n > len(remaining) {
			return errors.New("historyipc: history control write made invalid progress")
		}
		remaining = remaining[n:]
	}
	return nil
}
