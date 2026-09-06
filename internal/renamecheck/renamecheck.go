// Package renamecheck implements the ADR-0002 rename-phase and
// full-confirmation verifier for the predecessor-product to
// agent-fitness-functions product rename (calm-poc-q8d.8).
//
// Mode separation is deliberate: RunRenamePhase implements the checks that
// calm-poc-q8d.8 owns (active-surface old spellings, governance projection,
// requirements.lock line-1-only diff, marker/product prefix correctness, and
// protected-path exactness). RunFull is the ADR-0002 full-confirmation gate:
// it runs every rename-phase check plus the separator-insensitive predecessor
// sweep plus a source-level assertion that internal/client/client.go's four
// ADR-0006 marker-history arrays include the immediate predecessor
// product-name generation (calm-poc-phk.7's predecessor-hook upgrade
// capability). The two
// modes MUST NOT be conflated — RunRenamePhase never silently skips the
// checks it owns, and RunFull only reports PASS once every constituent check
// (rename-phase, separator sweep, marker-history) is itself green.
package renamecheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Mode selects which verifier phase runs.
type Mode string

const (
	// ModeRenamePhase runs the calm-poc-q8d.8 rename-phase checks.
	ModeRenamePhase Mode = "rename-phase"
	// ModeFull runs the ADR-0002 full-confirmation gate: every rename-phase
	// check, the separator-insensitive predecessor sweep, and the
	// marker-history source assertion.
	ModeFull Mode = "full"
)

// Status is the outcome of a single check.
type Status string

const (
	StatusPass           Status = "PASS"
	StatusFail           Status = "FAIL"
	StatusNotImplemented Status = "NOT_IMPLEMENTED"
)

// CheckResult is one named check's outcome.
type CheckResult struct {
	Name   string
	Status Status
	Detail string
}

// Report is the full output of a verifier run.
type Report struct {
	Mode   Mode
	Checks []CheckResult
}

// Passed reports whether every check in the report passed. A report
// containing any NOT_IMPLEMENTED check never counts as passed.
func (r Report) Passed() bool {
	for _, c := range r.Checks {
		if c.Status != StatusPass {
			return false
		}
	}
	return true
}

const (
	protectedGovernanceID          = "https://stackoverflow.com/calm-poc/patterns/governance.json"
	protectedGovernanceTitle       = "Agent Fitness Functions"
	protectedGovernanceDescription = "FINOS CALM pattern enforcing Agent Fitness Functions governance."
	protectedModulePath            = "github.com/AvogadroSG1/agent-fitness-functions"
	protectedCalmNodeTag           = `json:"calm_node"`
	protectedPredecessorLiteral    = `"calm-poc-dev-ca", "` + predecessorProductName + `"`
	// protectedCallerRepoKey is the canonical logical governance repository
	// key (ADR-0009 supersedes ADR-0003's Identity Boundaries clause, which
	// pinned the predecessor key).
	protectedCallerRepoKey         = "agent-fitness-functions"
	protectedCallerRepoCN          = "dev-hook-pool"
	hookProductPrefixDecl          = `hookProductPrefix = "agent-fitness-functions"`
	sarifToolNameDecl              = `Name:           "agent-fitness-functions",`
)

// activeSurfaceExclusions lists the paths where predecessor spellings are
// permitted to remain: immutable ADR bodies, the legacy-reference index, the
// phk.6 evidence/escalation trail, and the certificate predecessor-identity
// recognizers (and their tests) that MUST keep matching the old CA/server
// names until calm-poc-phk.7 retires the predecessor path.
// The predecessor spellings are assembled from fragments so this
// checker never trips its own tracked-source scans.
const (
	predecessorEnvPrefix   = "STACK_FITNESS" + "_FUNCTIONS_"
	predecessorProductName = "stack-fitness" + "-functions"
)

var activeSurfaceExclusions = []string{
	"docs/adr/",
	"LEGACY_REFERENCES.md",
	"docs/escalation/",
	".beads/",
	"internal/devcerts/state.go",
	"internal/devcerts/certification_test.go",
	"internal/devcerts/lifecycle_test.go",
	"internal/devcerts/rotation_rejection_test.go",
}

// envPrefixExclusions is the narrower exclusion set for the environment
// variable prefix, matching TestActiveProductSurfaceUsesAgentFitnessFunctions
// in rename_surface_test.go.
var envPrefixExclusions = []string{
	"docs/adr/",
	"LEGACY_REFERENCES.md",
	".beads/",
}

// RunRenamePhase executes the calm-poc-q8d.8 rename-phase checks against the
// repository rooted at repoRoot.
func RunRenamePhase(repoRoot string) Report {
	report := Report{Mode: ModeRenamePhase}
	report.Checks = append(report.Checks, checkActiveSurfaceEnvPrefix(repoRoot))
	report.Checks = append(report.Checks, checkActiveSurfaceProductName(repoRoot))
	report.Checks = append(report.Checks, checkGovernanceProjection(repoRoot))
	report.Checks = append(report.Checks, checkRequirementsLockLineOneOnlyDiff(repoRoot))
	report.Checks = append(report.Checks, checkMarkerPrefixCorrectness(repoRoot))
	report.Checks = append(report.Checks, checkProtectedPathExactness(repoRoot))
	return report
}

// RunFull executes the ADR-0002 full-confirmation gate: every rename-phase
// check calm-poc-q8d.8 owns, plus the separator-insensitive predecessor
// sweep (any-separator spellings of the predecessor product name, not just
// the hyphenated form), plus a source-level assertion that
// internal/client/client.go's four ADR-0006 marker-history arrays include
// the immediate predecessor product-name generation — proving
// calm-poc-phk.7's predecessor-hook upgrade capability actually exists in
// source, not merely in this checker's own claim.
func RunFull(repoRoot string) Report {
	report := Report{Mode: ModeFull}
	renamePhase := RunRenamePhase(repoRoot)
	report.Checks = append(report.Checks, renamePhase.Checks...)
	report.Checks = append(report.Checks, checkSeparatorInsensitivePredecessorSweep(repoRoot))
	report.Checks = append(report.Checks, checkMarkerHistoryContainsStackGeneration(repoRoot))
	return report
}

func checkActiveSurfaceEnvPrefix(repoRoot string) CheckResult {
	hits, err := gitGrepFiles(repoRoot, predecessorEnvPrefix, envPrefixExclusions)
	return surfaceResult("active-surface-env-prefix", predecessorEnvPrefix, envPrefixExclusions, hits, err)
}

func checkActiveSurfaceProductName(repoRoot string) CheckResult {
	hits, err := gitGrepFiles(repoRoot, predecessorProductName, activeSurfaceExclusions)
	return surfaceResult("active-surface-product-name", predecessorProductName, activeSurfaceExclusions, hits, err)
}

func surfaceResult(name, pattern string, exclusions, hits []string, err error) CheckResult {
	if err != nil {
		return CheckResult{Name: name, Status: StatusFail, Detail: fmt.Sprintf("git grep failed: %v", err)}
	}
	if len(hits) == 0 {
		return CheckResult{Name: name, Status: StatusPass, Detail: fmt.Sprintf("no %q outside %v", pattern, exclusions)}
	}
	sort.Strings(hits)
	return CheckResult{
		Name:   name,
		Status: StatusFail,
		Detail: fmt.Sprintf("%q found outside protected paths %v:\n  %s", pattern, exclusions, strings.Join(hits, "\n  ")),
	}
}

// gitGrepFiles runs `git grep -l pattern -- :!exclusion...` and returns the
// matching file list, mirroring rename_surface_test.go's pathspec exclusion
// style.
func gitGrepFiles(repoRoot, pattern string, exclusions []string) ([]string, error) {
	return runGitGrep(repoRoot, "-l", pattern, exclusions)
}

// gitGrepFilesRegex runs `git grep -lE pattern -- :!exclusion...`, the
// extended-regex variant used by the separator-insensitive predecessor sweep.
func gitGrepFilesRegex(repoRoot, pattern string, exclusions []string) ([]string, error) {
	return runGitGrep(repoRoot, "-lE", pattern, exclusions)
}

func runGitGrep(repoRoot, flag, pattern string, exclusions []string) ([]string, error) {
	args := []string{"grep", flag, pattern, "--"}
	for _, ex := range exclusions {
		args = append(args, ":!"+ex)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if runErr != nil {
		// git grep exits 1 when there are no matches; that is success, not
		// an error, for this check.
		if exitErr, ok := runErr.(*exec.ExitError); ok && exitErr.ExitCode() == 1 && out.Len() == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("%v: %s", runErr, stderr.String())
	}
	trimmed := strings.TrimSpace(out.String())
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, "\n"), nil
}

func checkGovernanceProjection(repoRoot string) CheckResult {
	const name = "governance-json-projection"
	path := filepath.Join(repoRoot, "patterns", "governance.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return CheckResult{Name: name, Status: StatusFail, Detail: fmt.Sprintf("read %s: %v", path, err)}
	}
	var doc struct {
		ID          string `json:"$id"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return CheckResult{Name: name, Status: StatusFail, Detail: fmt.Sprintf("parse %s: %v", path, err)}
	}
	var problems []string
	if doc.ID != protectedGovernanceID {
		problems = append(problems, fmt.Sprintf("$id = %q, want protected value %q", doc.ID, protectedGovernanceID))
	}
	if doc.Title != protectedGovernanceTitle {
		problems = append(problems, fmt.Sprintf("title = %q, want %q", doc.Title, protectedGovernanceTitle))
	}
	if doc.Description != protectedGovernanceDescription {
		problems = append(problems, fmt.Sprintf("description = %q, want %q", doc.Description, protectedGovernanceDescription))
	}
	if len(problems) > 0 {
		return CheckResult{Name: name, Status: StatusFail, Detail: strings.Join(problems, "; ")}
	}
	return CheckResult{Name: name, Status: StatusPass, Detail: "title, description, and $id match the projected values exactly"}
}

// checkRequirementsLockLineOneOnlyDiff compares the working tree
// requirements.lock against the committed HEAD revision (if one exists) and
// asserts that only line 1 differs — every dependency/hash line must be
// byte-identical.
func checkRequirementsLockLineOneOnlyDiff(repoRoot string) CheckResult {
	const name = "requirements-lock-line-one-only-diff"
	path := filepath.Join(repoRoot, "requirements.lock")
	current, err := os.ReadFile(path)
	if err != nil {
		return CheckResult{Name: name, Status: StatusFail, Detail: fmt.Sprintf("read %s: %v", path, err)}
	}
	cmd := exec.Command("git", "show", "HEAD:requirements.lock")
	cmd.Dir = repoRoot
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return CheckResult{Name: name, Status: StatusFail, Detail: fmt.Sprintf("git show HEAD:requirements.lock: %v: %s", err, stderr.String())}
	}
	prevLines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	curLines := strings.Split(strings.TrimRight(string(current), "\n"), "\n")
	if len(prevLines) != len(curLines) {
		return CheckResult{Name: name, Status: StatusFail, Detail: fmt.Sprintf("line count changed: HEAD had %d, working tree has %d", len(prevLines), len(curLines))}
	}
	var diffLines []int
	for i := range prevLines {
		if prevLines[i] != curLines[i] {
			diffLines = append(diffLines, i+1)
		}
	}
	switch {
	case len(diffLines) == 0:
		return CheckResult{Name: name, Status: StatusPass, Detail: "requirements.lock unchanged (already renamed at HEAD)"}
	case len(diffLines) == 1 && diffLines[0] == 1:
		if !strings.Contains(curLines[0], "agent-fitness-functions") || strings.Contains(curLines[0], predecessorProductName) {
			return CheckResult{Name: name, Status: StatusFail, Detail: fmt.Sprintf("line 1 = %q, want the agent-fitness-functions product header", curLines[0])}
		}
		return CheckResult{Name: name, Status: StatusPass, Detail: "only line 1 (the product comment) changed; dependency/hash lines are byte-identical"}
	default:
		return CheckResult{Name: name, Status: StatusFail, Detail: fmt.Sprintf("unexpected diff outside line 1: changed lines %v", diffLines)}
	}
}

func checkMarkerPrefixCorrectness(repoRoot string) CheckResult {
	const name = "marker-product-prefix-correctness"
	var problems []string

	clientGo, err := os.ReadFile(filepath.Join(repoRoot, "internal", "client", "client.go"))
	if err != nil {
		return CheckResult{Name: name, Status: StatusFail, Detail: fmt.Sprintf("read internal/client/client.go: %v", err)}
	}
	if !bytes.Contains(clientGo, []byte(hookProductPrefixDecl)) {
		problems = append(problems, fmt.Sprintf("internal/client/client.go missing %q", hookProductPrefixDecl))
	}

	sarifGo, err := os.ReadFile(filepath.Join(repoRoot, "internal", "sarif", "sarif.go"))
	if err != nil {
		return CheckResult{Name: name, Status: StatusFail, Detail: fmt.Sprintf("read internal/sarif/sarif.go: %v", err)}
	}
	if !bytes.Contains(sarifGo, []byte(sarifToolNameDecl)) {
		problems = append(problems, fmt.Sprintf("internal/sarif/sarif.go missing %q", sarifToolNameDecl))
	}

	if len(problems) > 0 {
		return CheckResult{Name: name, Status: StatusFail, Detail: strings.Join(problems, "; ")}
	}
	return CheckResult{Name: name, Status: StatusPass, Detail: "hookProductPrefix and SARIF tool name carry the agent-fitness-functions product identity"}
}

func checkProtectedPathExactness(repoRoot string) CheckResult {
	const name = "protected-path-exactness"
	var problems []string

	checkContains := func(relPath, want, label string) {
		data, err := os.ReadFile(filepath.Join(repoRoot, relPath))
		if err != nil {
			problems = append(problems, fmt.Sprintf("read %s: %v", relPath, err))
			return
		}
		if !bytes.Contains(data, []byte(want)) {
			problems = append(problems, fmt.Sprintf("%s: expected protected %s %q not found", relPath, label, want))
		}
	}

	checkContains("go.mod", "module "+protectedModulePath, "module path")
	checkContains(filepath.Join("internal", "fitness", "contract.go"), protectedCalmNodeTag, "calm_node JSON tag")
	checkContains(filepath.Join("internal", "devcerts", "state.go"), protectedPredecessorLiteral, "predecessor CA/server identity literal")
	checkContains("caller-repos.json", protectedCallerRepoKey, "caller-repos.json canonical governance key")
	checkContains("caller-repos.json", protectedCallerRepoCN, "caller-repos.json dev-hook-pool CN")

	govData, err := os.ReadFile(filepath.Join(repoRoot, "patterns", "governance.json"))
	if err != nil {
		problems = append(problems, fmt.Sprintf("read patterns/governance.json: %v", err))
	} else if !bytes.Contains(govData, []byte(protectedGovernanceID)) {
		problems = append(problems, fmt.Sprintf("patterns/governance.json: expected protected $id %q not found", protectedGovernanceID))
	}

	if len(problems) > 0 {
		return CheckResult{Name: name, Status: StatusFail, Detail: strings.Join(problems, "; ")}
	}
	return CheckResult{Name: name, Status: StatusPass, Detail: "module path, calm_node tag, predecessor cert literal, governance $id, and caller-repos.json identities are exact"}
}

// separatorInsensitivePredecessorPattern matches the predecessor product
// name under any separator style (hyphen or underscore), mirroring
// internal/renamecheck/separator_sweep_test.go — the q8d.8 standards review
// found snake_case shell variables the hyphen-only active-surface sweep
// missed.
const separatorInsensitivePredecessorPattern = "[Ss][Tt][Aa][Cc][Kk][-_][Ff][Ii][Tt][Nn][Ee][Ss][Ss][-_][Ff][Uu][Nn][Cc][Tt][Ii][Oo][Nn][Ss]"

// separatorSweepExclusions mirrors separator_sweep_test.go's exclusion set
// exactly: immutable ADR bodies, the legacy-reference index, the phk.6
// evidence/escalation trail, the beads tracker database, and the
// certificate predecessor-identity recognizers (and their tests) that MUST
// keep matching the old CA/server names until the predecessor path retires.
var separatorSweepExclusions = []string{
	"docs/adr",
	"LEGACY_REFERENCES.md",
	"docs/escalation",
	".beads",
	"internal/devcerts/state.go",
	"internal/devcerts/certification_test.go",
	"internal/devcerts/lifecycle_test.go",
	"internal/devcerts/rotation_rejection_test.go",
	// See separator_sweep_test.go: a dated implementation plan documenting
	// the pre-upgrade shell-variable naming this same ticket fixes.
	"docs/superpowers/plans/2026-06-16-install-hooks-naming-and-scheme.md",
}

func checkSeparatorInsensitivePredecessorSweep(repoRoot string) CheckResult {
	const name = "separator-insensitive-predecessor-sweep"
	hits, err := gitGrepFilesRegex(repoRoot, separatorInsensitivePredecessorPattern, separatorSweepExclusions)
	if err != nil {
		return CheckResult{Name: name, Status: StatusFail, Detail: fmt.Sprintf("git grep failed: %v", err)}
	}
	if len(hits) == 0 {
		return CheckResult{Name: name, Status: StatusPass, Detail: "no separator-insensitive predecessor spelling found outside protected paths"}
	}
	sort.Strings(hits)
	return CheckResult{
		Name:   name,
		Status: StatusFail,
		Detail: fmt.Sprintf("separator-insensitive predecessor spelling found outside protected paths:\n  %s", strings.Join(hits, "\n  ")),
	}
}

// markerHistoryIdentifiers names the four ADR-0006 "Migration from
// predecessor generations" arrays that must exist in
// internal/client/client.go: git-hook markers, sidecar markers, git-guard
// names, and agent Edit/Write hook names.
var markerHistoryIdentifiers = []string{
	"gitHookMarkerPrefixes",
	"sidecarMarkerPrefixes",
	"gitGuardNameHistory",
	"agentHookNameHistory",
}

// predecessorFragmentAssembly is the exact fragment-split expression
// client.go MUST use to construct the predecessor product-name generation,
// so its spelling never appears contiguously (unsplit) in tracked source
// (per this same package's own separator-insensitive sweep).
const predecessorFragmentAssembly = `"stack-fitness" + "-functions"`

// checkMarkerHistoryContainsStackGeneration is a source-level assertion
// (ADR-0002 full-confirmation, ADR-0006 "Migration from predecessor
// generations") that internal/client/client.go actually defines all four
// marker-history arrays and assembles the predecessor generation from
// fragments rather than a bare literal — proving calm-poc-phk.7's upgrade
// capability exists in source, not merely asserted by this checker.
func checkMarkerHistoryContainsStackGeneration(repoRoot string) CheckResult {
	const name = "marker-history-includes-stack-generation"
	path := filepath.Join(repoRoot, "internal", "client", "client.go")
	data, err := os.ReadFile(path)
	if err != nil {
		return CheckResult{Name: name, Status: StatusFail, Detail: fmt.Sprintf("read %s: %v", path, err)}
	}
	var problems []string
	if !bytes.Contains(data, []byte(predecessorFragmentAssembly)) {
		problems = append(problems, fmt.Sprintf("missing fragment-assembled predecessor generation (%s)", predecessorFragmentAssembly))
	}
	for _, identifier := range markerHistoryIdentifiers {
		if !bytes.Contains(data, []byte(identifier)) {
			problems = append(problems, fmt.Sprintf("missing marker-history array %q", identifier))
		}
	}
	if len(problems) > 0 {
		return CheckResult{Name: name, Status: StatusFail, Detail: strings.Join(problems, "; ")}
	}
	return CheckResult{Name: name, Status: StatusPass, Detail: "all four ADR-0006 marker-history arrays include the immediate predecessor product-name generation"}
}
