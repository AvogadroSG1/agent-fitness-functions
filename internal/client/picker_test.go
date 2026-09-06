package client

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestFunctionPickerTogglesSelectionsAndReturnsFullNineKeyMap locks the
// interactive onboarding picker contract: options come from the embedded
// catalog in the canonical allFitnessFunctionKeys order (pre-checked from the
// govconfig defaults), digits toggle individual functions, and an empty line
// confirms — returning the full nine-key map the scaffold step consumes. The
// rendered prompt must show each function's description and threshold so the
// user is choosing among the system's existing fitness functions, not
// guessing names.
func TestFunctionPickerTogglesSelectionsAndReturnsFullNineKeyMap(t *testing.T) {
	options, err := loadPickerOptions()
	if err != nil {
		t.Fatalf("loadPickerOptions: %v", err)
	}
	keys := allFitnessFunctionKeys()
	if len(options) != len(keys) {
		t.Fatalf("picker options = %d, want %d", len(options), len(keys))
	}
	for i, key := range keys {
		if options[i].name != key {
			t.Fatalf("options[%d].name = %q, want %q (canonical order)", i, options[i].name, key)
		}
	}

	var output bytes.Buffer
	// Entries 1 and 4 are cyclomatic-complexity and logic-density; entry 7 is
	// temporal-purity, off by default, so toggling it opts in.
	selected, err := runFunctionPicker(strings.NewReader("1\n4\n7\n\n"), &output, options)
	if err != nil {
		t.Fatalf("runFunctionPicker: %v", err)
	}
	if len(selected) != len(keys) {
		t.Fatalf("selected = %v, want all %d keys", selected, len(keys))
	}
	for _, key := range fitnessFunctionKeys {
		want := key != "cyclomatic-complexity" && key != "logic-density"
		if selected[key] != want {
			t.Fatalf("selected[%q] = %v, want %v after toggling entries 1 and 4 off", key, selected[key], want)
		}
	}
	for _, key := range generalizedFitnessFunctionKeys {
		want := key == "temporal-purity"
		if selected[key] != want {
			t.Fatalf("selected[%q] = %v, want %v (generalized functions start off)", key, selected[key], want)
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

// TestPickerRejectsLayerSovereigntySelection locks the S8 boundary: the
// checklist itself lets a user tick layer-sovereignty (select-all does), but
// the onboard path refuses that selection before writing anything, naming the
// settings it still needs. ADR-0010 slice S10 replaces the refusal with an
// interactive layer prompt.
func TestPickerRejectsLayerSovereigntySelection(t *testing.T) {
	if err := rejectUnsettableSelection(map[string]bool{"cyclomatic-complexity": true}); err != nil {
		t.Fatalf("settable selection rejected: %v", err)
	}
	err := rejectUnsettableSelection(map[string]bool{"layer-sovereignty": true})
	if err == nil {
		t.Fatal("layer-sovereignty selection accepted, want a rejection")
	}
	for _, fragment := range []string{"layer-sovereignty", "fitness-function-settings", "onboard"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("rejection %q must mention %q", err, fragment)
		}
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
