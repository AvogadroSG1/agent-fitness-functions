package analyzer

import (
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/calm"
)

// Red tests for calm-poc-zak (S10): onboarding recommendations must count
// generalized findings-based functions (temporal-purity,
// sql-composition-safety) so a repo with existing naive datetimes or
// interpolated SQL is recommended advisory, not block. Content-scored
// functions (layer-sovereignty, deterministic-ordering) are not computable
// offline and contribute no delta rows.
func TestBuildOnboardingRecommendationCountsGeneralizedFindings(t *testing.T) {
	rules := map[string]calm.FitnessRule{
		"cyclomatic-complexity":  {Threshold: 9, Operator: "lte"},
		"interface-width":        {Threshold: 20, Operator: "lte"},
		"implementation-depth":   {Threshold: 0.722, Operator: "gte"},
		"logic-density":          {Threshold: 0.255, Operator: "gte"},
		"dependency-discipline":  {Threshold: 0.8, Operator: "gte"},
		"temporal-purity":        {Threshold: 0, Operator: "lte"},
		"sql-composition-safety": {Threshold: 0, Operator: "lte"},
	}
	results := []AnalysisResult{
		{
			CALMNode:   "pipeline",
			FileMetric: FileMetric{TotalLOC: 40, LogicLOC: 20, PublicMethods: 2, LDR: 0.5},
			Imports:    ImportMetric{Total: 2, Used: 2, DDC: 1},
			Findings: []Finding{
				{Rule: "temporal-purity", Kind: "py-datetime-now-naive", Line: 7},
				{Rule: "temporal-purity", Kind: "py-datetime-utcnow", Line: 13},
				{Rule: "sql-composition-safety", Kind: "py-fstring-execute", Line: 21},
			},
		},
	}

	recommendation := BuildOnboardingRecommendation("sample", results, rules)

	deltas := deltasByFunction(recommendation)
	assertFindingsDelta(t, deltas, "temporal-purity", 2)
	assertFindingsDelta(t, deltas, "sql-composition-safety", 1)
	for _, absent := range []string{"layer-sovereignty", "deterministic-ordering"} {
		if _, ok := deltas[absent]; ok {
			t.Fatalf("deltas contain %q, want offline-incomputable functions omitted", absent)
		}
	}
	if recommendation.EnforcementMode != "advisory" {
		t.Fatalf("enforcement mode = %q, want advisory when findings exist", recommendation.EnforcementMode)
	}
}

func deltasByFunction(recommendation OnboardingRecommendation) map[string]ThresholdDelta {
	deltas := map[string]ThresholdDelta{}
	for _, delta := range recommendation.Deltas {
		deltas[delta.FitnessFunction] = delta
	}
	return deltas
}

// assertFindingsDelta checks one findings-counted delta row: lte-0 rule with
// the expected number of findings in the "findings" unit.
func assertFindingsDelta(t *testing.T, deltas map[string]ThresholdDelta, name string, want int) {
	t.Helper()
	delta, ok := deltas[name]
	if !ok {
		t.Fatalf("deltas = %v, want %s row", deltas, name)
	}
	if delta.ViolatingCount != want || delta.Operator != "lte" || delta.GlobalThreshold != 0 || delta.ViolationUnit != "findings" {
		t.Fatalf("%s delta = %+v, want %d findings against lte 0", name, delta, want)
	}
}

func TestBuildOnboardingRecommendationWithoutFindingsStaysBlock(t *testing.T) {
	rules := map[string]calm.FitnessRule{
		"cyclomatic-complexity":  {Threshold: 9, Operator: "lte"},
		"interface-width":        {Threshold: 20, Operator: "lte"},
		"implementation-depth":   {Threshold: 0.722, Operator: "gte"},
		"logic-density":          {Threshold: 0.255, Operator: "gte"},
		"dependency-discipline":  {Threshold: 0.8, Operator: "gte"},
		"temporal-purity":        {Threshold: 0, Operator: "lte"},
		"sql-composition-safety": {Threshold: 0, Operator: "lte"},
	}
	results := []AnalysisResult{{
		CALMNode:   "pipeline",
		FileMetric: FileMetric{TotalLOC: 40, LogicLOC: 20, PublicMethods: 2, LDR: 0.5},
		Imports:    ImportMetric{Total: 2, Used: 2, DDC: 1},
	}}

	recommendation := BuildOnboardingRecommendation("sample", results, rules)
	if recommendation.EnforcementMode != "block" || recommendation.TotalViolations != 0 {
		t.Fatalf("recommendation = %+v, want block with zero violations", recommendation)
	}
}
