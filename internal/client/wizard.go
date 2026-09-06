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
	"regexp"
	"slices"
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

// wizardLayerLimit bounds one pass of the layer loop for the same reason:
// input that never ends the list must not spin forever.
const wizardLayerLimit = 32

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
// picker seeded from the current config, layer definitions for a newly
// enabled layer-sovereignty, then a diff+confirm gate. The outcome always
// carries the choices that were made, confirmed or not, so a caller can name
// what the user declined.
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
	layers, err := resolveWizardLayers(reader, out, facts, enforcement, functions)
	if err != nil {
		return wizardOutcome{Enforcement: enforcement, Functions: functions}, err
	}
	renderWizardDiff(out, facts, enforcement, functions)
	confirmed, err := confirmWizardChoices(reader, out)
	return wizardOutcome{Enforcement: enforcement, Functions: functions, Layers: layers, Confirmed: confirmed}, err
}

// resolveWizardLayers settles the layer-sovereignty definitions this run
// carries. A repository that already defines layers keeps them verbatim — an
// update run must never re-ask for settings it can read — and a run that
// leaves the function off needs none at all. Only newly enabling it prompts.
func resolveWizardLayers(reader *bufio.Reader, out io.Writer, facts wizardFacts, enforcement string, functions map[string]bool) ([]govconfig.LayerRule, error) {
	if !functions["layer-sovereignty"] {
		return nil, nil
	}
	if existing := currentLayerRules(facts); len(existing) > 0 {
		return existing, nil
	}
	return promptWizardLayers(reader, out, enforcement, functions)
}

// currentLayerRules returns the layers already in force, as a copy: the
// panel's facts describe what is on disk and no later step may write through
// them.
func currentLayerRules(facts wizardFacts) []govconfig.LayerRule {
	if facts.Current == nil {
		return nil
	}
	return slices.Clone(facts.Current.LayerRules())
}

// promptWizardLayers collects layer definitions until the developer enters an
// empty name, then accepts them only if the config they would produce is one
// the daemon would load. An empty set and a rejected set both re-open the
// loop: enabling layer-sovereignty without usable layers is the failure this
// prompt exists to prevent.
func promptWizardLayers(reader *bufio.Reader, out io.Writer, enforcement string, functions map[string]bool) ([]govconfig.LayerRule, error) {
	_, _ = fmt.Fprintln(out, "\nlayer-sovereignty needs at least one layer: a name, the paths that belong to it, and the patterns those paths must not contain.")
	for attempt := 0; attempt < wizardPromptAttempts; attempt++ {
		layers, err := readWizardLayers(reader, out)
		if err != nil {
			return nil, err
		}
		if err := validateWizardLayers(enforcement, functions, layers); err != nil {
			_, _ = fmt.Fprintf(out, "  invalid layer definitions: %v\n", err)
			continue
		}
		return layers, nil
	}
	return nil, fmt.Errorf("layer-sovereignty was selected but no usable layer was defined after %d attempts", wizardPromptAttempts)
}

// readWizardLayers reads one pass of the layer loop: a name, its paths, and
// its forbidden patterns, repeated until an empty name ends the list.
func readWizardLayers(reader *bufio.Reader, out io.Writer) ([]govconfig.LayerRule, error) {
	layers := make([]govconfig.LayerRule, 0, 2)
	for {
		// The bound is checked after reading the name so the terminating empty
		// line is always consumed — exiting early would leave it queued as the
		// next prompt's answer.
		name, err := promptWizardValue(reader, out, "\nLayer name (empty line when done): ")
		if err != nil || name == "" {
			return layers, err
		}
		if len(layers) >= wizardLayerLimit {
			return nil, fmt.Errorf("layer limit of %d reached; define the remainder in fitness-function-settings directly", wizardLayerLimit)
		}
		layer, err := promptWizardLayer(reader, out, name)
		if err != nil {
			return nil, err
		}
		layers = append(layers, layer)
	}
}

// promptWizardLayer reads the two pattern lists that define one named layer.
func promptWizardLayer(reader *bufio.Reader, out io.Writer, name string) (govconfig.LayerRule, error) {
	paths, err := promptWizardPatterns(reader, out, fmt.Sprintf("  Paths in %s (comma-separated regexes): ", name))
	if err != nil {
		return govconfig.LayerRule{}, err
	}
	forbidden, err := promptWizardPatterns(reader, out, fmt.Sprintf("  Patterns forbidden in %s (comma-separated regexes): ", name))
	if err != nil {
		return govconfig.LayerRule{}, err
	}
	return govconfig.LayerRule{Name: name, Paths: paths, ForbiddenPatterns: forbidden}, nil
}

// promptWizardPatterns reads one comma-separated pattern list, re-prompting
// the same field until every entry compiles. Rejecting a pattern here, where
// the developer can retype it, is the whole point: the identical rejection
// from the daemon arrives at commit time against a config already written.
func promptWizardPatterns(reader *bufio.Reader, out io.Writer, prompt string) ([]string, error) {
	for attempt := 0; attempt < wizardPromptAttempts; attempt++ {
		line, err := promptWizardValue(reader, out, prompt)
		if err != nil {
			return nil, err
		}
		patterns, patternErr := compilablePatterns(line)
		if patternErr == nil {
			return patterns, nil
		}
		_, _ = fmt.Fprintf(out, "  %v\n", patternErr)
	}
	return nil, fmt.Errorf("no valid pattern entered after %d attempts", wizardPromptAttempts)
}

// compilablePatterns splits a comma-separated answer into patterns, failing
// on the first one Go's regexp package rejects and on an answer that names no
// pattern at all.
func compilablePatterns(line string) ([]string, error) {
	patterns := make([]string, 0, 2)
	for _, field := range strings.Split(line, ",") {
		pattern := strings.TrimSpace(field)
		if pattern == "" {
			continue
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return nil, fmt.Errorf("invalid pattern %q: %w", pattern, err)
		}
		patterns = append(patterns, pattern)
	}
	if len(patterns) == 0 {
		return nil, errors.New("invalid entry: name at least one pattern")
	}
	return patterns, nil
}

// validateWizardLayers renders the config these answers would produce and
// parses it exactly as the daemon would, so a rejection re-opens the prompt
// instead of surfacing later as an unwritable or unloadable config.
func validateWizardLayers(enforcement string, functions map[string]bool, layers []govconfig.LayerRule) error {
	content, err := renderGovernanceConfig(enforcement, functions, layers, configExtras{})
	if err != nil {
		return err
	}
	_, err = govconfig.Parse(content)
	return err
}

// promptWizardValue prints a prompt and reads one answer verbatim. Layer
// names and regexes are case-sensitive, so this is the raw counterpart to
// readWizardLine's normalized menu answers.
func promptWizardValue(reader *bufio.Reader, out io.Writer, prompt string) (string, error) {
	_, _ = fmt.Fprint(out, prompt)
	return readWizardValue(reader)
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

// readWizardLine reads one menu answer, normalized to lower case, for the
// prompts whose vocabulary is fixed (advisory/block, y/n).
func readWizardLine(reader *bufio.Reader) (string, error) {
	line, err := readWizardValue(reader)
	return strings.ToLower(line), err
}

// readWizardValue reads one answer verbatim, trimmed. End of input is an
// empty answer rather than an error: a developer closing stdin takes the
// default and declines the confirmation, which writes nothing.
func readWizardValue(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
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
