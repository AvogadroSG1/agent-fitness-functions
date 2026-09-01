package analyzer

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/calm"
	"github.com/AvogadroSG1/agent-fitness-functions/patterns"
)

// EmittedConfig mirrors the mounted governance config shape written to
// configs/<repo>/config.json. It is deliberately kept in sync with the
// internal/server Config struct without importing that package.
type EmittedConfig struct {
	EnforcementMode    string          `json:"enforcement-mode"`
	EnforcementOnError string          `json:"enforcement-on-error"`
	FitnessFunctions   map[string]bool `json:"fitness-functions"`
}

// ThresholdDelta compares one fitness function's repository percentile against the
// global threshold embedded in patterns/governance.json.
type ThresholdDelta struct {
	FitnessFunction string  `json:"fitness_function"`
	Operator        string  `json:"operator"`
	PercentileLabel string  `json:"percentile_label"`
	RepositoryValue float64 `json:"repository_percentile"`
	GlobalThreshold float64 `json:"global_threshold"`
	Delta           float64 `json:"delta"`
	NeedsLooser     bool    `json:"needs_looser_threshold"`
	ViolatingCount  int     `json:"violating_count"`
	ViolationUnit   string  `json:"violation_unit"`
}

// OnboardingRecommendation bundles the enforcement-mode recommendation, per-function
// threshold deltas, and violation counts derived from a baseline analysis.
type OnboardingRecommendation struct {
	Repository      string           `json:"repository"`
	EnforcementMode string           `json:"enforcement_mode"`
	TotalViolations int              `json:"total_violations"`
	Deltas          []ThresholdDelta `json:"threshold_deltas"`
}

// GlobalThresholds returns the fitness rules embedded in patterns/governance.json.
func GlobalThresholds() (map[string]calm.FitnessRule, error) {
	pattern, err := calm.LoadPatternFromBytes("patterns/governance.json", patterns.GovernanceJSON)
	if err != nil {
		return nil, err
	}
	return pattern.FitnessFunctions, nil
}

// SummarizeResults computes the percentile summary for a set of analyzer results.
func SummarizeResults(results []AnalysisResult) BaselineSummary {
	return summarize(distributions(results), len(results))
}

// BuildOnboardingRecommendation derives an enforcement-mode recommendation and the
// per-function threshold deltas for a repository. Zero violations against the global
// thresholds recommends "block"; any violation recommends "advisory".
func BuildOnboardingRecommendation(repository string, results []AnalysisResult, rules map[string]calm.FitnessRule) OnboardingRecommendation {
	summary := SummarizeResults(results)
	deltas := thresholdDeltas(results, summary, rules)
	total := 0
	for _, delta := range deltas {
		total += delta.ViolatingCount
	}
	mode := "block"
	if total > 0 {
		mode = "advisory"
	}
	return OnboardingRecommendation{
		Repository:      repository,
		EnforcementMode: mode,
		TotalViolations: total,
		Deltas:          deltas,
	}
}

func thresholdDeltas(results []AnalysisResult, summary BaselineSummary, rules map[string]calm.FitnessRule) []ThresholdDelta {
	deltas := []ThresholdDelta{
		cyclomaticDelta(results, summary, rules),
		interfaceDelta(results, summary, rules),
		depthDelta(results, summary, rules),
		densityDelta(results, summary, rules),
		disciplineDelta(results, summary, rules),
	}
	deltas = append(deltas, findingsDeltas(results, rules)...)
	return deltas
}

// findingsDeltas adds threshold-delta rows for the generalized fitness
// functions computable offline purely by counting findings the language
// analyzers already emit (temporal-purity, sql-composition-safety). The other
// two generalized functions (layer-sovereignty, deterministic-ordering) are
// content-scored against a live repository checkout and cannot be computed
// from AnalysisResult alone, so they contribute no row here; a row only
// appears when the rule is present in the embedded pattern's rules map.
func findingsDeltas(results []AnalysisResult, rules map[string]calm.FitnessRule) []ThresholdDelta {
	var deltas []ThresholdDelta
	for _, name := range []string{"temporal-purity", "sql-composition-safety"} {
		rule, ok := rules[name]
		if !ok {
			continue
		}
		deltas = append(deltas, findingsDelta(results, name, rule))
	}
	return deltas
}

// findingsDelta builds one ThresholdDelta row by counting findings whose Rule
// matches name across all results. RepositoryValue mirrors the count (there
// is no percentile to summarize for a findings-based rule).
func findingsDelta(results []AnalysisResult, name string, rule calm.FitnessRule) ThresholdDelta {
	count := countFindings(results, name)
	return newDelta(name, "count", "findings", float64(count), count, rule)
}

func countFindings(results []AnalysisResult, rule string) int {
	count := 0
	for _, result := range results {
		for _, finding := range result.Findings {
			if finding.Rule == rule {
				count++
			}
		}
	}
	return count
}

func cyclomaticDelta(results []AnalysisResult, summary BaselineSummary, rules map[string]calm.FitnessRule) ThresholdDelta {
	rule := rules["cyclomatic-complexity"]
	count := countCyclomaticViolations(results, rule.Threshold)
	return newDelta("cyclomatic-complexity", "P90", "functions", float64(summary.P90CyclomaticComplexity), count, rule)
}

func interfaceDelta(results []AnalysisResult, summary BaselineSummary, rules map[string]calm.FitnessRule) ThresholdDelta {
	rule := rules["interface-width"]
	count := countInterfaceViolations(results, rule.Threshold)
	return newDelta("interface-width", "P90", "files", float64(summary.P90PublicMethods), count, rule)
}

func depthDelta(results []AnalysisResult, summary BaselineSummary, rules map[string]calm.FitnessRule) ThresholdDelta {
	rule := rules["implementation-depth"]
	count := countDepthViolations(results, rule.Threshold)
	return newDelta("implementation-depth", "P10", "files", summary.P10AverageLOCPerPublicAPI, count, rule)
}

func densityDelta(results []AnalysisResult, summary BaselineSummary, rules map[string]calm.FitnessRule) ThresholdDelta {
	rule := rules["logic-density"]
	count := countDensityViolations(results, rule.Threshold)
	return newDelta("logic-density", "P10", "files", summary.P10LogicDensityRatio, count, rule)
}

func disciplineDelta(results []AnalysisResult, summary BaselineSummary, rules map[string]calm.FitnessRule) ThresholdDelta {
	rule := rules["dependency-discipline"]
	count := countDisciplineViolations(results, rule.Threshold)
	return newDelta("dependency-discipline", "P10", "files", summary.P10DependencyDiscipline, count, rule)
}

func newDelta(name, percentileLabel, unit string, repoValue float64, count int, rule calm.FitnessRule) ThresholdDelta {
	needsLooser := repoValue < rule.Threshold
	if rule.Operator == "lte" {
		needsLooser = repoValue > rule.Threshold
	}
	return ThresholdDelta{
		FitnessFunction: name,
		Operator:        rule.Operator,
		PercentileLabel: percentileLabel,
		RepositoryValue: repoValue,
		GlobalThreshold: rule.Threshold,
		Delta:           repoValue - rule.Threshold,
		NeedsLooser:     needsLooser,
		ViolatingCount:  count,
		ViolationUnit:   unit,
	}
}

func countCyclomaticViolations(results []AnalysisResult, threshold float64) int {
	count := 0
	for _, result := range results {
		for _, function := range result.Functions {
			if float64(function.CyclomaticComplexity) > threshold {
				count++
			}
		}
	}
	return count
}

func countInterfaceViolations(results []AnalysisResult, threshold float64) int {
	count := 0
	for _, result := range results {
		result = EnsureModuleMetric(result)
		if float64(result.ModuleMetric.PublicMethods) > threshold {
			count++
		}
	}
	return count
}

func countDepthViolations(results []AnalysisResult, threshold float64) int {
	count := 0
	for _, result := range results {
		result = EnsureModuleMetric(result)
		if result.ModuleMetric.PublicMethods > 0 && result.ModuleMetric.AverageLOCPerPublicMethod < threshold {
			count++
		}
	}
	return count
}

func countDensityViolations(results []AnalysisResult, threshold float64) int {
	count := 0
	for _, result := range results {
		if result.FileMetric.TotalLOC > 0 && result.FileMetric.LDR < threshold {
			count++
		}
	}
	return count
}

func countDisciplineViolations(results []AnalysisResult, threshold float64) int {
	count := 0
	for _, result := range results {
		if result.Imports.Total > 0 && result.Imports.DDC < threshold {
			count++
		}
	}
	return count
}

// WriteOnboardingConfig writes a ready-to-use per-repo governance config (the
// configs/<repo>/config.json shape) with all five fitness functions enabled and the
// recommended enforcement mode.
func WriteOnboardingConfig(path, enforcementMode string) error {
	config := EmittedConfig{
		EnforcementMode:    enforcementMode,
		EnforcementOnError: "block",
		FitnessFunctions: map[string]bool{
			"cyclomatic-complexity": true,
			"interface-width":       true,
			"implementation-depth":  true,
			"logic-density":         true,
			"dependency-discipline": true,
		},
	}
	content, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return os.WriteFile(path, content, 0o644)
}

// WriteOnboardingReport renders the enforcement recommendation, the threshold-delta
// table, and the global-threshold limitation note to w.
func WriteOnboardingReport(w io.Writer, rec OnboardingRecommendation) error {
	var b strings.Builder
	fmt.Fprintf(&b, "\nOnboarding recommendation for %q\n", rec.Repository)
	fmt.Fprintf(&b, "  enforcement-mode: %s (%d files/functions violate the global thresholds)\n\n",
		rec.EnforcementMode, rec.TotalViolations)
	b.WriteString("Threshold delta (repository percentile vs. global embedded threshold):\n")
	b.WriteString(deltaTable(rec.Deltas))
	b.WriteString(onboardingLimitationNote)
	_, err := io.WriteString(w, b.String())
	return err
}

func deltaTable(deltas []ThresholdDelta) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %-22s %-4s %-9s %-9s %-9s %-14s %s\n",
		"fitness function", "op", "repo", "global", "delta", "needs looser?", "violations")
	for _, delta := range deltas {
		fmt.Fprintf(&b, "  %-22s %-4s %-9.3f %-9.3f %-+9.3f %-14s %d %s\n",
			delta.FitnessFunction, delta.Operator, delta.RepositoryValue, delta.GlobalThreshold,
			delta.Delta, looserLabel(delta.NeedsLooser), delta.ViolatingCount, delta.ViolationUnit)
	}
	return b.String()
}

func looserLabel(needsLooser bool) string {
	if needsLooser {
		return "yes"
	}
	return "no"
}

const onboardingLimitationNote = `
NOTE: Thresholds are GLOBAL and compiled into the binary (patterns/governance.json).
A per-repo config.json can only toggle fitness functions and the enforcement mode; it
CANNOT change any threshold. A "needs looser? yes" row means this repository's tail
exceeds the global threshold, so files will violate under enforcement-mode block.
Recalibration requires editing patterns/governance.json and rebuilding the binary.
See docs/threshold-calibration.md for the calibration rule.
`
