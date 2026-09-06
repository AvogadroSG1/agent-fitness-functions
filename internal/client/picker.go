package client

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/analyzer"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/govconfig"
)

// functionOption is one row of the interactive onboarding picker: a fitness
// function from the embedded catalog plus its current enabled/disabled state.
type functionOption struct {
	name        string
	description string
	threshold   float64
	operator    string
	unit        string
	enabled     bool
	// requiresSettings marks a function that cannot be enabled by toggle
	// alone (layer-sovereignty needs layer definitions).
	requiresSettings bool
}

// loadPickerOptions builds the picker's nine rows from the embedded governance
// pattern (analyzer.GlobalThresholds), in the canonical allFitnessFunctionKeys
// order, pre-checked from govconfig.Default() — the five metric functions on,
// the four generalized ones off — so the picker opens on exactly the
// governance a repo gets when it selects nothing.
func loadPickerOptions() ([]functionOption, error) {
	rules, err := analyzer.GlobalThresholds()
	if err != nil {
		return nil, fmt.Errorf("loading embedded governance pattern: %w", err)
	}
	keys := allFitnessFunctionKeys()
	defaults := govconfig.Default().FitnessFunctions
	options := make([]functionOption, 0, len(keys))
	for _, key := range keys {
		rule := rules[key]
		options = append(options, functionOption{
			name:        key,
			description: rule.Description,
			threshold:   rule.Threshold,
			operator:    rule.Operator,
			unit:        rule.Unit,
			enabled:     defaults[key],
			// layer-sovereignty is the only function whose enablement needs
			// more than a toggle; the guard lives in
			// rejectUnsettableSelection so the picker itself stays a
			// straight checklist.
			requiresSettings: key == "layer-sovereignty",
		})
	}
	return options, nil
}

// runFunctionPicker drives the interactive checklist: it re-renders the
// current selection on every iteration, reads one line from in, and applies
// it as a toggle/select-all/select-none/confirm command. Confirming with
// nothing selected re-prompts rather than returning an empty governance set.
func runFunctionPicker(in io.Reader, out io.Writer, options []functionOption) (map[string]bool, error) {
	scanner := bufio.NewScanner(in)
	for {
		renderPickerOptions(out, options)
		line, ok := readPickerLine(scanner)
		if !ok {
			break
		}
		if applyPickerLine(out, options, line) {
			return pickerSelections(options), nil
		}
	}
	return pickerSelections(options), scanner.Err()
}

// applyPickerLine handles one line of picker input, mutating options in
// place, and reports whether it was a valid confirm the caller should return.
func applyPickerLine(out io.Writer, options []functionOption, line string) bool {
	switch trimmed := strings.TrimSpace(line); trimmed {
	case "":
		if !anyPickerSelected(options) {
			_, _ = fmt.Fprintln(out, "Select at least one fitness function before confirming.")
			return false
		}
		return true
	case "a":
		setAllPickerOptions(options, true)
	case "n":
		setAllPickerOptions(options, false)
	default:
		togglePickerDigit(options, trimmed)
	}
	return false
}

func readPickerLine(scanner *bufio.Scanner) (string, bool) {
	if !scanner.Scan() {
		return "", false
	}
	return scanner.Text(), true
}

// togglePickerDigit flips the enabled state of the 1-based option named by
// text; a non-digit or out-of-range entry is ignored rather than erroring, so
// a stray keystroke doesn't abort the whole onboard run.
func togglePickerDigit(options []functionOption, text string) {
	n, err := strconv.Atoi(text)
	if err != nil || n < 1 || n > len(options) {
		return
	}
	options[n-1].enabled = !options[n-1].enabled
}

func anyPickerSelected(options []functionOption) bool {
	for _, option := range options {
		if option.enabled {
			return true
		}
	}
	return false
}

func setAllPickerOptions(options []functionOption, enabled bool) {
	for i := range options {
		options[i].enabled = enabled
	}
}

func pickerSelections(options []functionOption) map[string]bool {
	selected := make(map[string]bool, len(options))
	for _, option := range options {
		selected[option.name] = option.enabled
	}
	return selected
}

// renderPickerOptions prints the numbered checklist: each row shows its
// checkbox state, name, description, and threshold so the user is choosing
// among the system's real fitness functions rather than guessing names.
func renderPickerOptions(out io.Writer, options []functionOption) {
	_, _ = fmt.Fprintf(out, "\nSelect fitness functions to enable (digits 1-%d toggle, 'a' all, 'n' none, empty line confirms):\n", len(options))
	for i, option := range options {
		mark := " "
		if option.enabled {
			mark = "x"
		}
		_, _ = fmt.Fprintf(out, "  [%s] %d. %-23s %s (%s %g %s)%s\n",
			mark, i+1, option.name, option.description, option.operator, option.threshold, option.unit,
			pickerSettingsNote(option))
	}
}

// pickerSettingsNote flags a row the user can tick but cannot finish here:
// enabling it also needs fitness-function-settings, which onboard rejects
// today (rejectUnsettableSelection) and prompts for from ADR-0010 slice S10.
func pickerSettingsNote(option functionOption) string {
	if option.requiresSettings {
		return " [needs fitness-function-settings]"
	}
	return ""
}

// stdinIsTerminal reports whether f is an interactive terminal, using only
// the standard library (no golang.org/x/term dependency). Checking
// Mode()&os.ModeCharDevice alone is not enough: `go test` runs the test
// binary with stdin connected to /dev/null, which is itself a character
// device, so that check alone reports a false positive under go test. A
// read-termios ioctl only succeeds against a real tty, so it distinguishes an
// actual terminal from /dev/null, pipes, and regular files alike — keeping
// non-interactive contexts (go test, pipes, CI) safely on the default path.
// The ioctl request number differs per platform, so the implementation lives
// in the tty_*.go build-tagged files.
func stdinIsTerminal(f *os.File) bool {
	if _, err := f.Stat(); err != nil {
		return false
	}
	return fileIsTerminal(f)
}

// wizardFacts is everything the onboard wizard shows before prompting: the
// repo's current governance (nil on first onboard) and the machine state
// gathered by the onboarder (sync, daemon, legacy artifacts).
type wizardFacts struct {
	RepoName      string
	Current       *govconfig.Config
	ConfigSynced  bool
	DaemonSummary string
	LegacyNotes   []string
}

// wizardOutcome is what the user chose: enforcement, the nine-key function
// selection, and whether they confirmed the diff (declining leaves every file
// untouched).
type wizardOutcome struct {
	Enforcement string
	Functions   map[string]bool
	Confirmed   bool
}

// runOnboardWizard drives the interactive flow: current-state panel,
// enforcement prompt (default = current mode on an update run), nine-function
// picker seeded from the current config, then a diff+confirm gate.
func runOnboardWizard(in io.Reader, out io.Writer, facts wizardFacts) (wizardOutcome, error) {
	return wizardOutcome{}, nil // stub pending S9 (calm-poc-a65i)
}
