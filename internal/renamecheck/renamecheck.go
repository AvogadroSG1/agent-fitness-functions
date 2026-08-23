// Package renamecheck implements the ADR-0002 rename-phase and
// full-confirmation verifier for the stack-fitness-functions to
// agent-fitness-functions product rename (calm-poc-q8d.8).
//
// Mode separation is deliberate: RunRenamePhase implements the checks that
// calm-poc-q8d.8 owns (active-surface old spellings, governance projection,
// requirements.lock line-1-only diff, marker/product prefix correctness, and
// protected-path exactness). RunFull is a distinct, explicitly-pending mode:
// it always reports NotImplemented until calm-poc-phk.7 lands predecessor
// hook recognition/replacement/cleanup and idempotent upgrade behavior. The
// two modes MUST NOT be conflated — a caller cannot get a false "pass" out of
// RunFull, and RunRenamePhase never silently skips the checks it owns.
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
	// ModeFull runs the ADR-0002 full-confirmation gate. It is intentionally
	// pending until calm-poc-phk.7 completes predecessor hook migration.
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
	protectedPredecessorLiteral    = `"calm-poc-dev-ca", "stack-fitness-functions"`
	protectedCallerRepoKey         = "calm-poc"
	protectedCallerRepoCN          = "dev-hook-pool"
	hookProductPrefixDecl          = `hookProductPrefix = "agent-fitness-functions"`
	sarifToolNameDecl              = `Name:           "agent-fitness-functions",`
)

// activeSurfaceExclusions lists the paths where predecessor spellings are
// permitted to remain: immutable ADR bodies, the legacy-reference index, the
// phk.6 evidence/escalation trail, and the certificate predecessor-identity
// recognizers (and their tests) that MUST keep matching the old CA/server
// names until calm-poc-phk.7 retires the predecessor path.
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

// RunFull executes the ADR-0002 full-confirmation gate. It is deliberately
// pending: calm-poc-phk.7 owns predecessor hook recognition, replacement,
// cleanup, and idempotent-upgrade behavior, and the certificate
// empty/published-target/legacy-direct-root/predecessor/partial/unknown-complete
// concurrency matrix. Until that lands, every full-mode check reports
// NOT_IMPLEMENTED rather than a false PASS or a misleading FAIL.
func RunFull(repoRoot string) Report {
	return Report{
		Mode: ModeFull,
		Checks: []CheckResult{
			{
				Name:   "full-confirmation-gate",
				Status: StatusNotImplemented,
				Detail: "ADR-0002 full-confirmation mode is pending calm-poc-phk.7 " +
					"(predecessor hook recognition/replacement/cleanup/idempotent " +
					"upgrade, and the certificate classification/concurrency matrix). " +
					"This mode intentionally never reports PASS or FAIL until that " +
					"work lands.",
			},
		},
	}
}

func checkActiveSurfaceEnvPrefix(repoRoot string) CheckResult {
	hits, err := gitGrepFiles(repoRoot, "STACK_FITNESS_FUNCTIONS_", envPrefixExclusions)
	return surfaceResult("active-surface-env-prefix", "STACK_FITNESS_FUNCTIONS_", envPrefixExclusions, hits, err)
}

func checkActiveSurfaceProductName(repoRoot string) CheckResult {
	hits, err := gitGrepFiles(repoRoot, "stack-fitness-functions", activeSurfaceExclusions)
	return surfaceResult("active-surface-product-name", "stack-fitness-functions", activeSurfaceExclusions, hits, err)
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
	args := []string{"grep", "-l", pattern, "--"}
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
		if !strings.Contains(curLines[0], "agent-fitness-functions") || strings.Contains(curLines[0], "stack-fitness-functions") {
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
	checkContains("caller-repos.json", protectedCallerRepoKey, "caller-repos.json calm-poc key")
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
