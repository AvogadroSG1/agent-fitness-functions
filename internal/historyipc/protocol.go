// Package historyipc transfers completed validation events to the local history writer.
package historyipc

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
	"unicode/utf8"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/history"
)

const (
	protocolVersion = 1
	frameHeaderSize = 8

	// MaxPayloadSize bounds declared JSON payload bytes before allocation.
	MaxPayloadSize = 16 << 20

	KindValidation = "validation"
	KindIdentify   = "identify"
	KindIdentity   = "identity"
	KindShutdown   = "shutdown"

	ServiceName = "agent-fitness-functions-history-writer"
)

const frameMagic = "AFH1"

// Message is one decoded protocol envelope.
type Message struct {
	Kind     string
	Event    *history.Event
	Identity *Identity
	Accepted bool
}

// Identity describes the foreground history writer answering an identify request.
type Identity struct {
	BuildRevision string    `json:"build_revision"`
	Modified      bool      `json:"modified"`
	PID           int       `json:"pid"`
	StartedAt     time.Time `json:"started_at"`
}

type validationEnvelope struct {
	Version int    `json:"version"`
	Kind    string `json:"kind"`
	history.Event
}

type controlEnvelope struct {
	Version int    `json:"version"`
	Kind    string `json:"kind"`
}

type identityEnvelope struct {
	Version     int       `json:"version"`
	Kind        string    `json:"kind"`
	ServiceName string    `json:"service_name"`
	Build       string    `json:"build_revision"`
	Modified    bool      `json:"modified"`
	PID         int       `json:"pid"`
	StartedAt   time.Time `json:"started_at"`
}

type shutdownEnvelope struct {
	Version  int    `json:"version"`
	Kind     string `json:"kind"`
	Accepted bool   `json:"accepted"`
}

// EncodeValidation validates and frames one complete validation event.
func EncodeValidation(event history.Event) ([]byte, error) {
	if err := event.Validate(); err != nil {
		return nil, fmt.Errorf("invalid validation event: %w", err)
	}
	return encodeEnvelope(validationEnvelope{Version: protocolVersion, Kind: KindValidation, Event: event})
}

// EncodeControl frames an identify or shutdown request. Controls carry no other fields.
func EncodeControl(kind string) ([]byte, error) {
	if kind != KindIdentify && kind != KindShutdown {
		return nil, fmt.Errorf("unsupported control kind %q", kind)
	}
	return encodeEnvelope(controlEnvelope{Version: protocolVersion, Kind: kind})
}

// EncodeIdentity frames the response to an identify control request.
func EncodeIdentity(identity Identity) ([]byte, error) {
	if identity.PID <= 0 || identity.StartedAt.IsZero() {
		return nil, errors.New("historyipc: identity requires pid and start time")
	}
	envelope := identityEnvelope{
		Version: protocolVersion, Kind: KindIdentity, ServiceName: ServiceName,
		Build: identity.BuildRevision, Modified: identity.Modified,
		PID: identity.PID, StartedAt: identity.StartedAt.UTC(),
	}
	return encodeEnvelope(envelope)
}

// EncodeShutdownResponse acknowledges that the writer stopped new admission.
func EncodeShutdownResponse() ([]byte, error) {
	return encodeEnvelope(shutdownEnvelope{Version: protocolVersion, Kind: KindShutdown, Accepted: true})
}

// DecodeFrame reads exactly one bounded frame and requires EOF after its payload.
func DecodeFrame(reader io.Reader) (Message, error) {
	payload, err := readPayload(reader)
	if err != nil {
		return Message{}, err
	}
	return decodePayload(payload)
}

func encodeEnvelope(envelope any) ([]byte, error) {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("encode history envelope: %w", err)
	}
	if len(payload) > MaxPayloadSize {
		return nil, fmt.Errorf("history envelope is %d bytes; maximum is %d", len(payload), MaxPayloadSize)
	}
	frame := make([]byte, frameHeaderSize+len(payload))
	copy(frame, frameMagic)
	binary.BigEndian.PutUint32(frame[4:], uint32(len(payload)))
	copy(frame[frameHeaderSize:], payload)
	return frame, nil
}

func readPayload(reader io.Reader) ([]byte, error) {
	length, err := readHeader(reader)
	if err != nil {
		return nil, err
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, fmt.Errorf("read history payload: %w", err)
	}
	if err := requireEOF(reader); err != nil {
		return nil, err
	}
	return payload, nil
}

func readHeader(reader io.Reader) (uint32, error) {
	header := make([]byte, frameHeaderSize)
	if _, err := io.ReadFull(reader, header); err != nil {
		return 0, fmt.Errorf("read history frame header: %w", err)
	}
	if string(header[:4]) != frameMagic {
		return 0, errors.New("historyipc: invalid frame magic")
	}
	length := binary.BigEndian.Uint32(header[4:])
	if length == 0 || length > MaxPayloadSize {
		return 0, fmt.Errorf("history payload length %d is outside 1..%d", length, MaxPayloadSize)
	}
	return length, nil
}

func requireEOF(reader io.Reader) error {
	var trailing [1]byte
	n, err := reader.Read(trailing[:])
	if n != 0 {
		return errors.New("historyipc: trailing frame content")
	}
	if !errors.Is(err, io.EOF) {
		return fmt.Errorf("history frame did not end at payload boundary: %w", err)
	}
	return nil
}

func decodePayload(payload []byte) (Message, error) {
	if !utf8.Valid(payload) || !json.Valid(payload) {
		return Message{}, errors.New("historyipc: payload is not valid utf-8 json")
	}
	fields, header, err := inspectEnvelope(payload)
	if err != nil {
		return Message{}, err
	}
	switch header.Kind {
	case KindValidation:
		return decodeValidation(payload)
	case KindIdentify:
		return decodeControl(header, fields, 2)
	case KindIdentity:
		return decodeIdentity(payload, fields)
	case KindShutdown:
		return decodeShutdown(payload, header, fields)
	default:
		return Message{}, fmt.Errorf("unsupported history envelope kind %q", header.Kind)
	}
}

func inspectEnvelope(payload []byte) (map[string]json.RawMessage, controlEnvelope, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil || fields == nil {
		return nil, controlEnvelope{}, errors.New("historyipc: envelope must be a json object")
	}
	var header controlEnvelope
	if err := json.Unmarshal(payload, &header); err != nil {
		return nil, controlEnvelope{}, fmt.Errorf("decode history envelope header: %w", err)
	}
	if header.Version != protocolVersion || header.Kind == "" {
		return nil, controlEnvelope{}, errors.New("historyipc: unsupported or incomplete envelope header")
	}
	return fields, header, nil
}

func decodeValidation(payload []byte) (Message, error) {
	var envelope validationEnvelope
	if err := decodeStrict(payload, &envelope); err != nil {
		return Message{}, fmt.Errorf("decode validation envelope: %w", err)
	}
	if err := envelope.Event.Validate(); err != nil {
		return Message{}, fmt.Errorf("invalid validation event: %w", err)
	}
	return Message{Kind: KindValidation, Event: &envelope.Event}, nil
}

func decodeControl(header controlEnvelope, fields map[string]json.RawMessage, expected int) (Message, error) {
	if len(fields) != expected {
		return Message{}, fmt.Errorf("history control %q contains extra or missing fields", header.Kind)
	}
	return Message{Kind: header.Kind}, nil
}

func decodeIdentity(payload []byte, fields map[string]json.RawMessage) (Message, error) {
	if len(fields) != 7 {
		return Message{}, errors.New("historyipc: identity response contains extra or missing fields")
	}
	var envelope identityEnvelope
	if err := decodeStrict(payload, &envelope); err != nil {
		return Message{}, fmt.Errorf("decode identity response: %w", err)
	}
	if envelope.ServiceName != ServiceName || envelope.PID <= 0 || envelope.StartedAt.IsZero() {
		return Message{}, errors.New("historyipc: invalid identity response")
	}
	identity := &Identity{
		BuildRevision: envelope.Build, Modified: envelope.Modified,
		PID: envelope.PID, StartedAt: envelope.StartedAt,
	}
	return Message{Kind: KindIdentity, Identity: identity}, nil
}

func decodeShutdown(payload []byte, header controlEnvelope, fields map[string]json.RawMessage) (Message, error) {
	if len(fields) == 2 {
		return decodeControl(header, fields, 2)
	}
	if len(fields) != 3 {
		return Message{}, errors.New("historyipc: shutdown envelope contains extra or missing fields")
	}
	var envelope shutdownEnvelope
	if err := decodeStrict(payload, &envelope); err != nil || !envelope.Accepted {
		return Message{}, errors.New("historyipc: invalid shutdown response")
	}
	return Message{Kind: KindShutdown, Accepted: true}, nil
}

func decodeStrict(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}
