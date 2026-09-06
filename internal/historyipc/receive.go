//go:build darwin || linux

package historyipc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

const (
	defaultReceiverLimit = 8
	defaultByteLimit     = 64 << 20
	defaultQueueSize     = 8
	receiveTimeout       = time.Second
)

var (
	ErrAdmissionUsed     = errors.New("historyipc: admission already used")
	ErrOverloaded        = errors.New("historyipc: declared payload exceeds available receiver budget")
	ErrQueueFull         = errors.New("historyipc: completed queue is full")
	ErrUnhandledControl  = errors.New("historyipc: control has no handler")
	ErrUnexpectedMessage = errors.New("historyipc: message is not a receiver request")
)

// ControlHandler handles identify and shutdown independently of validation
// persistence. It MUST honor ctx and return one complete framed response. A
// shutdown handler MUST stop new admission but MUST NOT cancel ctx before
// Receive writes that response.
type ControlHandler func(ctx context.Context, message Message) ([]byte, error)

// Receiver owns bounded connection admission, frame memory, and the completed queue.
type Receiver struct {
	slots     chan struct{}
	budget    *byteBudget
	completed chan *Delivery
	control   ControlHandler
	stopped   atomic.Bool
}

// Admission reserves one receiver slot. Call Receive once, or Close if no
// connection will be read. Receive closes the connection and releases the slot.
type Admission struct {
	receiver *Receiver
	claimed  atomic.Bool
}

// Delivery retains its payload reservation until the persistence owner calls Release.
type Delivery struct {
	Message     Message
	reservation *byteReservation
}

type byteBudget struct {
	mu            sync.Mutex
	limit         uint64
	reservedBytes uint64
}

type byteReservation struct {
	budget *byteBudget
	size   uint64
	once   sync.Once
}

// NewReceiver creates the fixed eight-receiver, 64 MiB, eight-delivery pipeline.
// An optional control handler receives identify and shutdown outside Completed.
func NewReceiver(handlers ...ControlHandler) *Receiver {
	return newReceiver(defaultReceiverLimit, defaultByteLimit, defaultQueueSize, handlers...)
}

func newReceiver(receivers int, bytes uint64, queue int, handlers ...ControlHandler) *Receiver {
	receiver := &Receiver{
		slots: make(chan struct{}, receivers), budget: newByteBudget(bytes),
		completed: make(chan *Delivery, queue),
	}
	if len(handlers) != 0 {
		receiver.control = handlers[0]
	}
	return receiver
}

// Completed returns deliveries whose reservations are still charged.
func (r *Receiver) Completed() <-chan *Delivery {
	return r.completed
}

// Admit reserves a receiver slot without waiting.
func (r *Receiver) Admit() (*Admission, bool) {
	if r.stopped.Load() {
		return nil, false
	}
	select {
	case r.slots <- struct{}{}:
		if r.stopped.Load() {
			<-r.slots
			return nil, false
		}
		return &Admission{receiver: r}, true
	default:
		return nil, false
	}
}

// Stop prevents new admission. Already-admitted receivers remain bounded by
// their one-second contexts and MUST be joined by their service owner.
func (r *Receiver) Stop() {
	r.stopped.Store(true)
}

// Close releases an unused admission. It is safe to call more than once.
func (a *Admission) Close() {
	if a != nil && a.claimed.CompareAndSwap(false, true) {
		<-a.receiver.slots
	}
}

// Receive reads one connection under the one-second deadline. A validation
// transfers payload ownership to Completed. A control invokes the configured
// handler immediately. Every other path releases the payload reservation.
func (a *Admission) Receive(ctx context.Context, connection net.Conn) (err error) {
	if a == nil || !a.claimed.CompareAndSwap(false, true) {
		return ErrAdmissionUsed
	}
	defer func() { <-a.receiver.slots }()
	defer func() { err = errors.Join(err, connection.Close()) }()
	receiveCtx, cancel := context.WithTimeout(ctx, receiveTimeout)
	defer cancel()
	deadline, _ := receiveCtx.Deadline()
	if err := connection.SetReadDeadline(deadline); err != nil {
		return fmt.Errorf("set history receive deadline: %w", err)
	}
	stopCancellation := context.AfterFunc(receiveCtx, func() {
		// Deadline expiration is the observable error; setting it is best-effort
		// because a cancellation callback has no error return path.
		_ = connection.SetDeadline(time.Now())
	})
	defer stopCancellation()
	length, err := readHeader(connection)
	if err != nil {
		return err
	}
	reservation, ok := a.receiver.budget.reserve(uint64(length))
	if !ok {
		return ErrOverloaded
	}
	return a.receiver.readReserved(receiveCtx, connection, length, reservation)
}

func (r *Receiver) readReserved(ctx context.Context, connection net.Conn, length uint32, reservation *byteReservation) error {
	payload := make([]byte, length)
	if _, err := io.ReadFull(connection, payload); err != nil {
		reservation.release()
		return fmt.Errorf("read history payload: %w", err)
	}
	if err := requireEOF(connection); err != nil {
		reservation.release()
		return err
	}
	message, err := decodePayload(payload)
	if err != nil {
		reservation.release()
		return err
	}
	return r.dispatch(ctx, connection, message, reservation)
}

func (r *Receiver) dispatch(ctx context.Context, connection net.Conn, message Message, reservation *byteReservation) error {
	switch {
	case message.Kind == KindValidation:
		return r.enqueue(message, reservation)
	case message.Kind == KindIdentify:
		defer reservation.release()
		return r.handleControl(ctx, connection, message)
	case message.Kind == KindShutdown && !message.Accepted:
		defer reservation.release()
		return r.handleControl(ctx, connection, message)
	default:
		reservation.release()
		return ErrUnexpectedMessage
	}
}

func (r *Receiver) enqueue(message Message, reservation *byteReservation) error {
	delivery := &Delivery{Message: message, reservation: reservation}
	select {
	case r.completed <- delivery:
		return nil
	default:
		reservation.release()
		return ErrQueueFull
	}
}

func (r *Receiver) handleControl(ctx context.Context, connection net.Conn, message Message) error {
	if r.control == nil {
		return ErrUnhandledControl
	}
	response, err := r.control(ctx, message)
	if err != nil {
		return fmt.Errorf("handle history control: %w", err)
	}
	if len(response) == 0 {
		return errors.New("historyipc: control handler returned an empty response")
	}
	deadline, _ := ctx.Deadline()
	if err := connection.SetWriteDeadline(deadline); err != nil {
		return fmt.Errorf("set history response deadline: %w", err)
	}
	if err := writeAll(connection, response); err != nil {
		return err
	}
	writer, ok := connection.(interface{ CloseWrite() error })
	if !ok {
		return errors.New("historyipc: control connection cannot close its write side")
	}
	if err := writer.CloseWrite(); err != nil {
		return fmt.Errorf("close history response write side: %w", err)
	}
	return nil
}

// Release returns the delivery's declared payload bytes to the receiver budget.
func (d *Delivery) Release() {
	if d != nil && d.reservation != nil {
		d.reservation.release()
	}
}

func newByteBudget(limit uint64) *byteBudget {
	return &byteBudget{limit: limit}
}

func (b *byteBudget) reserve(size uint64) (*byteReservation, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if size > b.limit-b.reservedBytes {
		return nil, false
	}
	b.reservedBytes += size
	return &byteReservation{budget: b, size: size}, true
}

func (b *byteBudget) reserved() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.reservedBytes
}

func (r *Receiver) reservedBytes() uint64 {
	return r.budget.reserved()
}

func (r *byteReservation) release() {
	r.once.Do(func() {
		r.budget.mu.Lock()
		r.budget.reservedBytes -= r.size
		r.budget.mu.Unlock()
	})
}

// Listen creates a Unix listener and requests its receive buffer before accept.
func Listen(socketPath string) (*net.UnixListener, error) {
	if err := validateSocketPath(socketPath); err != nil {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("listen for history frames: %w", err)
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		closeErr := listener.Close()
		return nil, errors.Join(fmt.Errorf("restrict history socket permissions: %w", err), closeErr)
	}
	if err := setReceiveBuffer(listener); err != nil {
		closeErr := listener.Close()
		return nil, errors.Join(err, closeErr)
	}
	return listener, nil
}

func setReceiveBuffer(listener *net.UnixListener) error {
	raw, err := listener.SyscallConn()
	if err != nil {
		return fmt.Errorf("access history listener socket: %w", err)
	}
	var optionErr error
	if err := raw.Control(func(fd uintptr) {
		optionErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_RCVBUF, socketBufferSize)
	}); err != nil {
		return fmt.Errorf("configure history listener: %w", err)
	}
	if optionErr != nil {
		return fmt.Errorf("request history receive buffer: %w", optionErr)
	}
	return nil
}

func validateSocketPath(path string) error {
	maximum := len(unix.RawSockaddrUnix{}.Path) - 1
	if path == "" || len(path) > maximum {
		return fmt.Errorf("unix socket path is %d bytes; maximum is %d", len(path), maximum)
	}
	return nil
}
