//go:build darwin || linux

package historyipc

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestReceiverAdmissionIsBounded(t *testing.T) {
	receiver := NewReceiver()
	admissions := make([]*Admission, 0, defaultReceiverLimit)
	for range defaultReceiverLimit {
		admission, ok := receiver.Admit()
		if !ok {
			t.Fatal("receiver refused admission before its limit")
		}
		admissions = append(admissions, admission)
	}
	if admission, ok := receiver.Admit(); ok {
		admission.Close()
		t.Fatal("receiver admitted work beyond its limit")
	}
	for _, admission := range admissions {
		admission.Close()
	}
}

func TestReceiverStopPreventsNewAdmission(t *testing.T) {
	receiver := NewReceiver()
	receiver.Stop()
	if admission, ok := receiver.Admit(); ok {
		admission.Close()
		t.Fatal("stopped receiver admitted a connection")
	}
}

func TestByteBudgetRejectsOverloadAndReleasesReservations(t *testing.T) {
	budget := newByteBudget(100)
	first, ok := budget.reserve(80)
	if !ok {
		t.Fatal("initial reservation failed")
	}
	if _, ok := budget.reserve(21); ok {
		t.Fatal("byte budget admitted overload")
	}
	first.release()
	second, ok := budget.reserve(100)
	if !ok {
		t.Fatal("released bytes remained charged")
	}
	second.release()
	if got := budget.reserved(); got != 0 {
		t.Fatalf("reserved bytes = %d, want 0", got)
	}
}

func TestDeliveryOwnsReservationUntilPersistenceRelease(t *testing.T) {
	receiver := newReceiver(1, MaxPayloadSize, 1)
	if err := receiveFrame(receiver, frameForTest(t)); err != nil {
		t.Fatal(err)
	}
	delivery := <-receiver.Completed()
	if receiver.reservedBytes() == 0 {
		t.Fatal("completed delivery released its reservation before persistence")
	}
	delivery.Release()
	if got := receiver.reservedBytes(); got != 0 {
		t.Fatalf("reserved bytes after persistence = %d, want 0", got)
	}
}

func TestCompletedQueueNeverWaitsAndReleasesDiscardedFrame(t *testing.T) {
	receiver := newReceiver(1, MaxPayloadSize*2, 1)
	if err := receiveFrame(receiver, frameForTest(t)); err != nil {
		t.Fatal(err)
	}
	reserved := receiver.reservedBytes()
	if err := receiveFrame(receiver, frameForTest(t)); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("second receive error = %v, want queue full", err)
	}
	if got := receiver.reservedBytes(); got != reserved {
		t.Fatalf("discarded frame reservation leaked: got %d, want %d", got, reserved)
	}
	(<-receiver.Completed()).Release()
}

func TestPartialFrameIsDiscardedAndReleasesItsReservation(t *testing.T) {
	receiver := NewReceiver()
	frame := frameForTest(t)
	if err := receiveFrame(receiver, frame[:len(frame)-1]); err == nil {
		t.Fatal("partial frame reported success")
	}
	select {
	case delivery := <-receiver.Completed():
		delivery.Release()
		t.Fatal("partial frame produced a delivery")
	default:
	}
	if got := receiver.reservedBytes(); got != 0 {
		t.Fatalf("partial frame retained %d reserved bytes", got)
	}
}

func TestReceiverRejectsResponseEnvelopesAtIngress(t *testing.T) {
	identity, err := EncodeIdentity(Identity{PID: 42, StartedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	shutdown, err := EncodeShutdownResponse()
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name  string
		frame []byte
	}{
		{name: "identity response", frame: identity},
		{name: "shutdown response", frame: shutdown},
	} {
		t.Run(tt.name, func(t *testing.T) {
			controlCalls := 0
			receiver := NewReceiver(func(context.Context, Message) ([]byte, error) {
				controlCalls++
				return EncodeShutdownResponse()
			})
			if err := receiveFrame(receiver, tt.frame); err == nil {
				t.Fatal("response envelope was accepted as a request")
			}
			if controlCalls != 0 {
				t.Fatalf("control callback calls = %d, want 0", controlCalls)
			}
			select {
			case delivery := <-receiver.Completed():
				delivery.Release()
				t.Fatal("response envelope entered persistence queue")
			default:
			}
			if got := receiver.reservedBytes(); got != 0 {
				t.Fatalf("response envelope retained %d reserved bytes", got)
			}
		})
	}
}

func receiveFrame(receiver *Receiver, frame []byte) error {
	client, server := net.Pipe()
	admission, ok := receiver.Admit()
	if !ok {
		return errors.New("admission refused")
	}
	done := make(chan error, 1)
	go func() { done <- admission.Receive(context.Background(), server) }()
	if _, err := client.Write(frame); err != nil {
		client.Close()
		return err
	}
	if err := client.Close(); err != nil {
		return err
	}
	return <-done
}

func frameForTest(t *testing.T) []byte {
	t.Helper()
	frame, err := EncodeValidation(protocolEvent())
	if err != nil {
		t.Fatal(err)
	}
	return frame
}
