package history

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
)

func TestHistoryEventEligibility(t *testing.T) {
	for _, status := range []fitness.Status{fitness.StatusPass, fitness.StatusAdvisory, fitness.StatusBlock} {
		t.Run(string(status), func(t *testing.T) {
			event := fixtureEvent(t)
			event.Status = status
			event.ResultJSON = json.RawMessage(`{"status":"` + string(status) + `","future_field":"retained"}`)
			if err := event.Validate(); err != nil {
				t.Fatalf("genuine verdict rejected: %v", err)
			}
		})
	}
	for _, result := range []string{`{"status":"pass","warming":true}`, `{"status":"unknown"}`, `{}`, `null`, `{"status":"block"}`, `invalid`} {
		t.Run(result, func(t *testing.T) {
			event := fixtureEvent(t)
			event.ResultJSON = json.RawMessage(result)
			if err := event.Validate(); err == nil {
				t.Error("ineligible or inconsistent result accepted")
			}
		})
	}
	event := fixtureEvent(t)
	event.DryRun = false
	if err := event.Validate(); err == nil {
		t.Error("inconsistent dry-run metadata accepted")
	}
}

func fixtureEvent(t *testing.T) Event {
	t.Helper()
	return Event{
		EventID:      "0123456789abcdef0123456789abcdef",
		CompletedAt:  time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC),
		CommonGitDir: "/fixture/.git", Repository: "governed-name",
		Worktree: "/fixture", File: "example.go", Source: "manual",
		Status: fitness.StatusPass, DryRun: true,
		RequestJSON: json.RawMessage(`{"repo":"governed-name","file":"example.go","proposed_content":"package example\n","language":"go","dry_run":true}`),
		ResultJSON:  json.RawMessage(`{"status":"pass"}`),
	}
}
