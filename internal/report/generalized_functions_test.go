package report

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/analyzer"
)

// Red test for calm-poc-6w6 (S1): BuildArchitecture must emit the four
// generalized count functions on both nodes. The analyzed node reports the
// counts scored by the checker (carried on AnalysisResult.RuleCounts, absent
// keys meaning zero); the synthetic actor node MUST emit 0 for all four,
// because the governance pattern declares them with maximum 0 and the actor
// must always pass.
func TestBuildArchitectureEmitsGeneralizedCountFunctions(t *testing.T) {
	document := BuildArchitecture(analyzer.AnalysisResult{
		CALMNode: "pipeline",
		Language: "python",
		File:     "src/bronze/orders.py",
		RuleCounts: map[string]int{
			"layer-sovereignty":      2,
			"deterministic-ordering": 1,
		},
	})

	if len(document.Nodes) != 2 {
		t.Fatalf("nodes = %d, want actor and analyzed node", len(document.Nodes))
	}

	actor := document.Nodes[0].Metadata.Fitness
	if actor.LayerSovereignty != 0 || actor.TemporalPurity != 0 ||
		actor.SQLCompositionSafety != 0 || actor.DeterministicOrdering != 0 {
		t.Fatalf("actor fitness = %+v, want zero for all generalized count functions", actor)
	}

	node := document.Nodes[1].Metadata.Fitness
	if node.LayerSovereignty != 2 {
		t.Fatalf("layer sovereignty = %v, want RuleCounts value 2", node.LayerSovereignty)
	}
	if node.DeterministicOrdering != 1 {
		t.Fatalf("deterministic ordering = %v, want RuleCounts value 1", node.DeterministicOrdering)
	}
	if node.TemporalPurity != 0 || node.SQLCompositionSafety != 0 {
		t.Fatalf("fitness = %+v, want zero for counts absent from RuleCounts", node)
	}

	content, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal architecture document: %v", err)
	}
	for _, field := range []string{
		`"layer-sovereignty":2`,
		`"temporal-purity":0`,
		`"sql-composition-safety":0`,
		`"deterministic-ordering":1`,
	} {
		if !strings.Contains(string(content), field) {
			t.Errorf("architecture document missing %s: %s", field, content)
		}
	}
}
