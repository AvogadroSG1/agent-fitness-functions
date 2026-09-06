package history

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestHistoryEventRejectsIncompleteMetadata(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Event)
	}{
		{name: "short id", change: func(e *Event) { e.EventID = "1234" }},
		{name: "nonhex id", change: func(e *Event) { e.EventID = "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz" }},
		{name: "missing completion", change: func(e *Event) { e.CompletedAt = time.Time{} }},
		{name: "relative common directory", change: func(e *Event) { e.CommonGitDir = ".git" }},
		{name: "missing repository", change: func(e *Event) { e.Repository = "" }},
		{name: "relative worktree", change: func(e *Event) { e.Worktree = "fixture" }},
		{name: "escaping file", change: func(e *Event) { e.File = "../example.go" }},
		{name: "unnormalized file", change: func(e *Event) { e.File = "src/../example.go" }},
		{name: "missing source", change: func(e *Event) { e.Source = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := fixtureEvent(t)
			tt.change(&event)
			if err := event.Validate(); err == nil {
				t.Error("incomplete metadata accepted")
			}
		})
	}
}

func TestHistoryEventRejectsInvalidRequests(t *testing.T) {
	for _, request := range []string{
		``, `null`, `{}`, `[]`, `invalid`,
		`{"repo":"governed-name","file":"example.go","language":"go","dry_run":true}`,
		`{"repo":"governed-name","file":"example.go","proposed_content":null,"language":"go","dry_run":true}`,
		`{"repo":"another","file":"example.go","proposed_content":"","language":"go","dry_run":true}`,
	} {
		t.Run(request, func(t *testing.T) {
			event := fixtureEvent(t)
			event.RequestJSON = json.RawMessage(request)
			if err := event.Validate(); err == nil {
				t.Error("invalid request accepted")
			}
		})
	}
}

func TestHistoryEventPreservesOriginalJSON(t *testing.T) {
	event := fixtureEvent(t)
	event.RequestJSON = json.RawMessage(`{ "repo":"governed-name", "file":"/fixture/example.go", "proposed_content":"\r\nλ\u0000", "language":"go", "dry_run":true, "future":9007199254740993 }`)
	event.ResultJSON = json.RawMessage(`{ "status":"pass", "future":{"value":9007199254740993} }`)
	request := bytes.Clone(event.RequestJSON)
	result := bytes.Clone(event.ResultJSON)
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(event.RequestJSON, request) || !bytes.Equal(event.ResultJSON, result) {
		t.Error("validation changed submitted or received JSON")
	}
	event.RequestJSON = json.RawMessage(`{"repo":"governed-name","file":"example.go","proposed_content":"","language":"go","dry_run":true}`)
	if err := event.Validate(); err != nil {
		t.Errorf("empty proposed source rejected: %v", err)
	}
}

func TestHistoryEventAcceptsVerdictWithoutRequestLanguage(t *testing.T) {
	event := fixtureEvent(t)
	event.RequestJSON = json.RawMessage(`{"repo":"governed-name","file":"example.go","proposed_content":"","language":"","dry_run":true}`)
	if err := event.Validate(); err != nil {
		t.Errorf("genuine verdict without request language rejected: %v", err)
	}
}
