package historyipc

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestValidationEnvelopeFlattensEventFields(t *testing.T) {
	frame, err := EncodeValidation(protocolEvent())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(frame[8:], &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["event"]; ok {
		t.Fatal("validation envelope nested the event")
	}
	for _, field := range []string{"version", "kind", "event_id", "common_git_dir", "request_json", "result_json"} {
		if _, ok := payload[field]; !ok {
			t.Errorf("validation envelope lacks top-level %q", field)
		}
	}
}

func TestProtocolRejectsInvalidPayloads(t *testing.T) {
	valid := protocolEvent()
	tests := []struct {
		name    string
		payload []byte
	}{
		{name: "invalid utf8", payload: []byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}},
		{name: "unknown version", payload: validationPayload(t, valid, func(values map[string]any) { values["version"] = 2 })},
		{name: "unknown kind", payload: []byte(`{"version":1,"kind":"other"}`)},
		{name: "inconsistent status", payload: validationPayload(t, valid, func(values map[string]any) { values["status"] = "block" })},
		{name: "inconsistent dry run", payload: validationPayload(t, valid, func(values map[string]any) { values["dry_run"] = false })},
		{name: "identify extra field", payload: []byte(`{"version":1,"kind":"identify","extra":true}`)},
		{name: "shutdown extra field", payload: []byte(`{"version":1,"kind":"shutdown","extra":true}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := DecodeFrame(bytes.NewReader(framePayload(tt.payload))); err == nil {
				t.Fatal("invalid payload accepted")
			}
		})
	}
}

func TestProtocolRejectsDeclaredOversizeBeforeAllocation(t *testing.T) {
	header := make([]byte, frameHeaderSize)
	copy(header, frameMagic)
	binary.BigEndian.PutUint32(header[4:], uint32(MaxPayloadSize+1))
	if _, err := DecodeFrame(bytes.NewReader(header)); err == nil {
		t.Fatal("oversize payload accepted")
	}
}

func TestControlAndResponseFramesRoundTrip(t *testing.T) {
	for _, kind := range []string{KindIdentify, KindShutdown} {
		t.Run(kind, func(t *testing.T) {
			frame, err := EncodeControl(kind)
			if err != nil {
				t.Fatal(err)
			}
			message, err := DecodeFrame(bytes.NewReader(frame))
			if err != nil || message.Kind != kind {
				t.Fatalf("control = %+v, %v", message, err)
			}
		})
	}

	identity := Identity{
		BuildRevision: "abc123", Modified: true, PID: 42,
		StartedAt: time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC),
	}
	frame, err := EncodeIdentity(identity)
	if err != nil {
		t.Fatal(err)
	}
	message, err := DecodeFrame(bytes.NewReader(frame))
	if err != nil || message.Kind != KindIdentity || message.Identity == nil || message.Identity.PID != 42 {
		t.Fatalf("identity = %+v, %v", message, err)
	}

	frame, err = EncodeShutdownResponse()
	if err != nil {
		t.Fatal(err)
	}
	message, err = DecodeFrame(bytes.NewReader(frame))
	if err != nil || message.Kind != KindShutdown || !message.Accepted {
		t.Fatalf("shutdown response = %+v, %v", message, err)
	}
}

func TestDecodeFrameAcceptsCompleteLargePayload(t *testing.T) {
	event := protocolEvent()
	source := strings.Repeat("λ", 2<<20)
	event.RequestJSON = json.RawMessage(`{"repo":"fixture","file":"example.go","proposed_content":` + quoteJSON(t, source) + `,"language":"go","dry_run":true}`)
	frame, err := EncodeValidation(event)
	if err != nil {
		t.Fatal(err)
	}
	message, err := DecodeFrame(bytes.NewReader(frame))
	if err != nil || message.Event == nil || !bytes.Equal(message.Event.RequestJSON, event.RequestJSON) {
		t.Fatalf("complete large frame was not preserved: %v", err)
	}
}

func validationPayload(t *testing.T, event any, change func(map[string]any)) []byte {
	t.Helper()
	payload, err := json.Marshal(struct {
		Version int    `json:"version"`
		Kind    string `json:"kind"`
		Event   any    `json:",inline"`
	}{Version: protocolVersion, Kind: KindValidation, Event: event})
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if err := json.Unmarshal(payload, &values); err != nil {
		t.Fatal(err)
	}
	// The helper starts from the same flattened shape as production encoding.
	delete(values, "Event")
	encodedEvent, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var eventValues map[string]any
	if err := json.Unmarshal(encodedEvent, &eventValues); err != nil {
		t.Fatal(err)
	}
	for key, value := range eventValues {
		values[key] = value
	}
	change(values)
	payload, err = json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func quoteJSON(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func framePayload(payload []byte) []byte {
	frame := make([]byte, frameHeaderSize+len(payload))
	copy(frame, frameMagic)
	binary.BigEndian.PutUint32(frame[4:], uint32(len(payload)))
	copy(frame[frameHeaderSize:], payload)
	return frame
}
