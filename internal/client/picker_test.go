package client

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestFunctionPickerTogglesSelectionsAndReturnsFullFiveKeyMap locks the
// interactive onboarding picker contract: options come from the embedded
// catalog in the canonical fitnessFunctionKeys order (all enabled to start),
// digits toggle individual functions, and an empty line confirms — returning
// the full five-key map the scaffold step consumes. The rendered prompt must
// show each function's description and threshold so the user is choosing
// among the system's existing fitness functions, not guessing names.
func TestFunctionPickerTogglesSelectionsAndReturnsFullFiveKeyMap(t *testing.T) {
	options, err := loadPickerOptions()
	if err != nil {
		t.Fatalf("loadPickerOptions: %v", err)
	}
	if len(options) != len(fitnessFunctionKeys) {
		t.Fatalf("picker options = %d, want %d", len(options), len(fitnessFunctionKeys))
	}
	for i, key := range fitnessFunctionKeys {
		if options[i].name != key {
			t.Fatalf("options[%d].name = %q, want %q (canonical order)", i, options[i].name, key)
		}
	}

	var output bytes.Buffer
	selected, err := runFunctionPicker(strings.NewReader("1\n4\n\n"), &output, options)
	if err != nil {
		t.Fatalf("runFunctionPicker: %v", err)
	}
	if len(selected) != len(fitnessFunctionKeys) {
		t.Fatalf("selected = %v, want all %d keys", selected, len(fitnessFunctionKeys))
	}
	for _, key := range fitnessFunctionKeys {
		want := key != "cyclomatic-complexity" && key != "logic-density"
		if selected[key] != want {
			t.Fatalf("selected[%q] = %v, want %v after toggling entries 1 and 4 off", key, selected[key], want)
		}
	}
	prompt := output.String()
	for _, fragment := range []string{"cyclomatic-complexity", "9", "Maximum cyclomatic complexity"} {
		if !strings.Contains(prompt, fragment) {
			t.Fatalf("picker prompt missing %q:\n%s", fragment, prompt)
		}
	}
}

// TestFunctionPickerRefusesEmptySelection locks the guard rail: confirming
// with zero functions selected re-prompts instead of scaffolding a config
// that governs nothing.
func TestFunctionPickerRefusesEmptySelection(t *testing.T) {
	options, err := loadPickerOptions()
	if err != nil {
		t.Fatalf("loadPickerOptions: %v", err)
	}
	var output bytes.Buffer
	selected, err := runFunctionPicker(strings.NewReader("n\n\na\n\n"), &output, options)
	if err != nil {
		t.Fatalf("runFunctionPicker: %v", err)
	}
	for _, key := range fitnessFunctionKeys {
		if !selected[key] {
			t.Fatalf("selected[%q] = false, want true after re-prompt and select-all", key)
		}
	}
	if !strings.Contains(output.String(), "at least one") {
		t.Fatalf("picker output %q must explain the empty-selection re-prompt", output.String())
	}
}

// TestStdinIsNotTerminalUnderGoTest locks that the interactive picker can
// never trigger in CI, agents, or pipes: go test stdin is not a character
// device, so non-TTY onboarding keeps today's non-interactive all-five
// default.
func TestStdinIsNotTerminalUnderGoTest(t *testing.T) {
	if stdinIsTerminal(os.Stdin) {
		t.Fatal("stdinIsTerminal(os.Stdin) = true under go test, want false")
	}
}
