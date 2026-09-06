package client

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/govconfig"
)

// wizardEnforcementAdvisory and wizardEnforcementBlock are the only two modes
// `client onboard` can scaffold, so they are the only two the prompt accepts.
const (
	wizardEnforcementAdvisory = "advisory"
	wizardEnforcementBlock    = "block"
)

// wizardPromptAttempts bounds the enforcement re-prompt loop. Input that never
// becomes valid — a closed stdin, a script piping noise — must fall back to
// the default rather than spin, so onboarding can never hang on a prompt.
const wizardPromptAttempts = 5

// wizardFacts is everything the onboard wizard shows before prompting: the
// repo's current governance (nil on first onboard) and the machine state
// gathered by the onboarder (sync, daemon, legacy artifacts).
type wizardFacts struct {
	RepoName      string
	Current       *govconfig.Config
	ConfigSynced  bool
	DaemonSummary string
	// DaemonStale is the daemon dimension of the attention verdict. It is a
	// separate fact from DaemonSummary deliberately: the panel must never
	// decide whether this machine is healthy by pattern-matching prose the
	// gatherer wrote.
	DaemonStale bool
	LegacyNotes []string
}

// wizardOutcome is what the user chose: enforcement, the nine-key function
// selection, and whether they confirmed the diff (declining leaves every file
// untouched).
type wizardOutcome struct {
	Enforcement string
	Functions   map[string]bool
	Confirmed   bool
	// Layers carries the layer-sovereignty definitions prompted for when the
	// user enables that function without existing settings (S10).
	Layers []govconfig.LayerRule
}

// runOnboardWizard drives the interactive flow: current-state panel,
// enforcement prompt (default = current mode on an update run), nine-function
// picker seeded from the current config, then a diff+confirm gate. The
// outcome always carries the choices that were made, confirmed or not, so a
// caller can name what the user declined.
func runOnboardWizard(in io.Reader, out io.Writer, facts wizardFacts) (wizardOutcome, error) {
	reader := bufio.NewReader(in)
	renderWizardPanel(out, facts)
	enforcement, err := promptWizardEnforcement(reader, out, defaultWizardEnforcement(facts))
	if err != nil {
		return wizardOutcome{}, err
	}
	functions, err := promptWizardFunctions(reader, out, facts)
	if err != nil {
		return wizardOutcome{Enforcement: enforcement}, err
	}
	renderWizardDiff(out, facts, enforcement, functions)
	confirmed, err := confirmWizardChoices(reader, out)
	return wizardOutcome{Enforcement: enforcement, Functions: functions, Confirmed: confirmed}, err
}

// renderWizardPanel prints the current-state panel: what repository this is,
// what the daemon is doing, and one verdict line for its governance followed
// by every item that earned an attention verdict.
func renderWizardPanel(out io.Writer, facts wizardFacts) {
	_, _ = fmt.Fprintln(out, "\nagent-fitness-functions onboard wizard")
	if facts.RepoName != "" {
		_, _ = fmt.Fprintf(out, "  repository: %s\n", facts.RepoName)
	}
	if facts.DaemonSummary != "" {
		_, _ = fmt.Fprintf(out, "  %s\n", facts.DaemonSummary)
	}
	items := wizardAttentionItems(facts)
	_, _ = fmt.Fprintf(out, "  governance: %s\n", wizardStatusLine(facts, items))
	for _, item := range items {
		_, _ = fmt.Fprintf(out, "    - %s\n", item)
	}
}

// wizardStatusLine is the panel's one-line verdict. A repo with no config is
// not onboarded — attention items about a machine cannot describe governance
// that does not exist yet. A governed repo is up to date exactly when nothing
// asked for attention.
func wizardStatusLine(facts wizardFacts, items []string) string {
	if facts.Current == nil {
		return "not onboarded (no governance config for this repository yet)"
	}
	if len(items) > 0 {
		return "needs attention"
	}
	return fmt.Sprintf("up to date (enforcement=%s, %d of %d fitness functions enabled)",
		facts.Current.EnforcementMode, countEnabledFunctions(facts.Current.FitnessFunctions), len(allFitnessFunctionKeys()))
}

// wizardAttentionItems lists every fact that makes this machine less than
// fully governed. The config-sync item applies only to a repo that already
// has a config; before that there is nothing to be out of sync with.
func wizardAttentionItems(facts wizardFacts) []string {
	items := make([]string, 0, len(facts.LegacyNotes)+2)
	if facts.Current != nil && !facts.ConfigSynced {
		items = append(items, "the repo-local config is not registered in the machine governance root")
	}
	if facts.DaemonStale {
		items = append(items, "the local governance daemon is not current; onboard will restart it")
	}
	return append(items, facts.LegacyNotes...)
}

func countEnabledFunctions(functions map[string]bool) int {
	count := 0
	for _, enabled := range functions {
		if enabled {
			count++
		}
	}
	return count
}

// defaultWizardEnforcement is the mode an empty answer accepts: the repo's
// current mode on an update run, advisory on a first onboard. A current mode
// onboard cannot scaffold (off) falls back to advisory rather than offering a
// default the rest of the command would reject.
func defaultWizardEnforcement(facts wizardFacts) string {
	if facts.Current == nil {
		return wizardEnforcementAdvisory
	}
	switch mode := string(facts.Current.EnforcementMode); mode {
	case wizardEnforcementAdvisory, wizardEnforcementBlock:
		return mode
	default:
		return wizardEnforcementAdvisory
	}
}

// promptWizardEnforcement reads the enforcement choice: an empty line takes
// the default, advisory and block are accepted verbatim, and anything else
// re-prompts with the two valid answers named.
func promptWizardEnforcement(reader *bufio.Reader, out io.Writer, defaultMode string) (string, error) {
	for attempt := 0; attempt < wizardPromptAttempts; attempt++ {
		_, _ = fmt.Fprintf(out, "\nEnforcement mode [advisory/block] (default %s): ", defaultMode)
		line, err := readWizardLine(reader)
		if err != nil {
			return defaultMode, err
		}
		switch line {
		case "":
			return defaultMode, nil
		case wizardEnforcementAdvisory, wizardEnforcementBlock:
			return line, nil
		}
		_, _ = fmt.Fprintln(out, "  Enter advisory, block, or an empty line for the default.")
	}
	return defaultMode, nil
}

// promptWizardFunctions runs the nine-function picker seeded from the repo's
// current config, so an update run opens on the governance in force rather
// than on the product defaults.
func promptWizardFunctions(reader *bufio.Reader, out io.Writer, facts wizardFacts) (map[string]bool, error) {
	options, err := loadPickerOptions()
	if err != nil {
		return nil, err
	}
	seedPickerOptions(options, facts.Current)
	return runFunctionPicker(unbufferedReader{inner: reader}, out, options)
}

// seedPickerOptions overwrites the picker's default check marks with the
// repo's current selection. A function the current config never mentions
// keeps the product default rather than being invented as disabled.
func seedPickerOptions(options []functionOption, current *govconfig.Config) {
	if current == nil {
		return
	}
	for i := range options {
		if enabled, ok := current.FitnessFunctions[options[i].name]; ok {
			options[i].enabled = enabled
		}
	}
}

// renderWizardDiff states what confirming would do: the enforcement move and
// every function whose state changes. A first onboard has nothing to diff
// against, so it lists the governance the new config would carry.
func renderWizardDiff(out io.Writer, facts wizardFacts, enforcement string, functions map[string]bool) {
	_, _ = fmt.Fprintln(out, "\nReview:")
	_, _ = fmt.Fprintf(out, "  enforcement: %s\n", describeEnforcementChange(facts.Current, enforcement))
	if facts.Current == nil {
		_, _ = fmt.Fprintf(out, "  fitness functions to enable: %s\n", joinEnabledFunctions(functions))
		return
	}
	changes := functionChangeLines(facts.Current, functions)
	if len(changes) == 0 {
		_, _ = fmt.Fprintf(out, "  fitness functions: unchanged (%s)\n", joinEnabledFunctions(functions))
		return
	}
	for _, change := range changes {
		_, _ = fmt.Fprintf(out, "  %s\n", change)
	}
}

func describeEnforcementChange(current *govconfig.Config, chosen string) string {
	if current == nil {
		return chosen + " (new)"
	}
	if string(current.EnforcementMode) == chosen {
		return chosen + " (unchanged)"
	}
	return fmt.Sprintf("%s -> %s", current.EnforcementMode, chosen)
}

// functionChangeLines names every function whose enabled state differs from
// the current config, in the canonical catalog order so two runs of the same
// change read identically.
func functionChangeLines(current *govconfig.Config, functions map[string]bool) []string {
	lines := make([]string, 0, len(functions))
	for _, key := range allFitnessFunctionKeys() {
		was := current.Enabled(key)
		if is := functions[key]; was != is {
			lines = append(lines, fmt.Sprintf("%s: %s -> %s", key, enabledWord(was), enabledWord(is)))
		}
	}
	return lines
}

func enabledWord(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

// joinEnabledFunctions lists the enabled functions in catalog order.
func joinEnabledFunctions(functions map[string]bool) string {
	enabled := make([]string, 0, len(functions))
	for _, key := range allFitnessFunctionKeys() {
		if functions[key] {
			enabled = append(enabled, key)
		}
	}
	if len(enabled) == 0 {
		return "none"
	}
	return strings.Join(enabled, ", ")
}

// confirmWizardChoices is the gate. Only an explicit yes confirms: a stray
// answer, a refusal, or a closed stdin all leave every file untouched.
func confirmWizardChoices(reader *bufio.Reader, out io.Writer) (bool, error) {
	_, _ = fmt.Fprint(out, "\nApply these choices? [y/N]: ")
	line, err := readWizardLine(reader)
	if err != nil {
		return false, err
	}
	return line == "y" || line == "yes", nil
}

// readWizardLine reads one answer, normalized to lower case. End of input is
// an empty answer rather than an error: a developer closing stdin takes the
// default and declines the confirmation, which writes nothing.
func readWizardLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.ToLower(strings.TrimSpace(line)), nil
}

// unbufferedReader hands its consumer one byte per Read so a bufio.Scanner
// built on it cannot read past the line it is currently scanning.
// runFunctionPicker owns its own scanner; without this cap that scanner would
// swallow the confirmation answer the wizard reads after the picker returns.
type unbufferedReader struct {
	inner io.Reader
}

func (r unbufferedReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return r.inner.Read(p[:1])
}

// gatherWizardFacts collects everything the panel reports, best effort: an
// unreadable config, an unreachable daemon, and an unknown legacy layout are
// all facts about the machine, never reasons to fail an onboard before it has
// asked the developer anything.
func gatherWizardFacts(repoName, repoRoot, addr string, httpClient *http.Client) wizardFacts {
	current, content := readRepoLocalGovernance(filepath.Join(repoRoot, "configs", repoName, "config.json"))
	summary, stale := summarizeWizardDaemon(httpClient, addr)
	return wizardFacts{
		RepoName:      repoName,
		Current:       current,
		ConfigSynced:  current != nil && sharedConfigMatches(repoName, content),
		DaemonSummary: summary,
		DaemonStale:   stale,
		LegacyNotes:   detectLegacyNotes(repoRoot),
	}
}

// readRepoLocalGovernance parses the tracked repo-local config that is the
// source of truth for this repository's governance, returning its raw bytes
// for the sync comparison. A missing or unparseable file yields no config at
// all: the wizard must not claim governance is in force that the daemon would
// reject.
func readRepoLocalGovernance(path string) (*govconfig.Config, []byte) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	parsed, err := govconfig.Parse(content)
	if err != nil {
		return nil, content
	}
	return &parsed, content
}

// sharedConfigMatches applies checkConfigSync's comparison (doctor.go): the
// repo-local config is registered only when the shared governance root holds
// a byte-identical copy.
func sharedConfigMatches(repoName string, content []byte) bool {
	shared, err := os.ReadFile(filepath.Join(governanceConfigsDir(), repoName, "config.json"))
	return err == nil && bytes.Equal(content, shared)
}

// summarizeWizardDaemon renders the panel's daemon line and reports whether
// that daemon is stale, using the same identity probe and staleness rules
// doctor applies.
func summarizeWizardDaemon(httpClient *http.Client, addr string) (string, bool) {
	if httpClient == nil {
		return "", false
	}
	identity, err := probeDaemonIdentity(httpClient, addr)
	if err != nil {
		return fmt.Sprintf("daemon: unreachable at %s (onboard will start one)", addr), false
	}
	reasons := daemonStaleness(identity, currentDaemonExpectations(addrListenMode(addr)))
	if len(reasons) == 0 {
		return fmt.Sprintf("daemon: current (%s)", describeDaemonIdentity(identity)), false
	}
	return fmt.Sprintf("daemon: stale (%s)", reasons[0]), true
}

// detectLegacyNotes reports the pre-ADR-0007 repo-local state migrateLegacy
// would quarantine, without touching any of it. Recognition mirrors
// legacymigration.go: only a managed cert layout is ours to move, and a
// repo-local caller-repos.json is stale whether or not it is tracked.
func detectLegacyNotes(repoRoot string) []string {
	notes := make([]string, 0, 2)
	certsDir := filepath.Join(repoRoot, legacyCertsDirName)
	if present, _ := pathExists(certsDir); present && isManagedCertLayout(certsDir) {
		notes = append(notes, fmt.Sprintf("legacy repo-local dev certs at %s (onboard quarantines them)", certsDir))
	}
	bindings := filepath.Join(repoRoot, callerRepoBindingsFileName)
	if present, _ := pathExists(bindings); present {
		notes = append(notes, fmt.Sprintf("legacy caller bindings at %s (the daemon reads the machine governance root's copy)", bindings))
	}
	return notes
}

// wizardOutcomeChanges names every difference between a repo's current
// governance and the choices the user confirmed, so an update run can state
// exactly what it is not applying yet.
func wizardOutcomeChanges(current govconfig.Config, outcome wizardOutcome) []string {
	changes := make([]string, 0, len(outcome.Functions)+1)
	if string(current.EnforcementMode) != outcome.Enforcement {
		changes = append(changes, fmt.Sprintf("enforcement: %s -> %s", current.EnforcementMode, outcome.Enforcement))
	}
	return append(changes, functionChangeLines(&current, outcome.Functions)...)
}

// describeWizardChoice summarizes an outcome in one line, so a declined
// onboard names what was declined instead of exiting silently.
func describeWizardChoice(outcome wizardOutcome) string {
	return fmt.Sprintf("enforcement=%s with %s", outcome.Enforcement, joinEnabledFunctions(outcome.Functions))
}
