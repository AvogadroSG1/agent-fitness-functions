package historyipc

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/history"
)

func TestValidationFramePreservesTheCompleteEvent(t *testing.T) {
	event := protocolEvent()
	frame, err := EncodeValidation(event)
	if err != nil {
		t.Fatal(err)
	}
	if string(frame[:4]) != "AFH1" || int(binary.BigEndian.Uint32(frame[4:8])) != len(frame)-8 {
		t.Fatal("frame does not declare its complete payload")
	}
	message, err := DecodeFrame(bytes.NewReader(frame))
	if err != nil {
		t.Fatal(err)
	}
	if message.Kind != "validation" || message.Event == nil || !bytes.Equal(message.Event.RequestJSON, event.RequestJSON) {
		t.Fatalf("decoded message lost event values: %+v", message)
	}
}

func TestIncompleteOrTrailingFramesNeverProduceAnEvent(t *testing.T) {
	frame, err := EncodeValidation(protocolEvent())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		frame []byte
	}{
		{"header only", frame[:8]},
		{"truncated payload", frame[:len(frame)-1]},
		{"trailing content", append(append([]byte(nil), frame...), 'x')},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeFrame(bytes.NewReader(tc.frame)); err == nil {
				t.Fatal("incomplete or trailing frame accepted")
			}
		})
	}
}

func TestAbsentWriterDropsPublication(t *testing.T) {
	if err := Publish(t.TempDir()+"/absent.sock", protocolEvent()); err == nil {
		t.Fatal("publication to an absent writer reported success")
	}
}

func protocolEvent() history.Event {
	return history.Event{
		EventID:      "0123456789abcdef0123456789abcdef",
		CompletedAt:  time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC),
		CommonGitDir: "/fixture/.git", Worktree: "/fixture", Repository: "fixture",
		File: "example.go", Source: "manual", Status: fitness.StatusPass, DryRun: true,
		RequestJSON: json.RawMessage(`{"repo":"fixture","file":"example.go","proposed_content":"π\n","language":"go","dry_run":true}`),
		ResultJSON:  json.RawMessage(`{"status":"pass"}`),
	}
}
