package report

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/analyzer"
)

// Red test for calm-poc-6w6 (S1): BuildArchitecture must emit the four
// generalized count functions on the analyzed node when their checker-scored
// counts (AnalysisResult.RuleCounts) are nonzero, and OMIT zero counts from
// the marshaled document entirely — the CALM CLI's
// pattern-has-no-empty-properties rule rejects zero-valued properties, so a
// clean count must be absent rather than 0. The synthetic actor node carries
// zero for all four and therefore emits none of them.
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

	assertCountValues(t, "actor", document.Nodes[0].Metadata.Fitness, 0, 0)
	assertCountValues(t, "service", document.Nodes[1].Metadata.Fitness, 2, 1)

	content, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal architecture document: %v", err)
	}
	assertDocumentFields(t, string(content),
		[]string{`"layer-sovereignty":2`, `"deterministic-ordering":1`},
		[]string{`"temporal-purity"`, `"sql-composition-safety"`})
}

// assertCountValues checks a node's generalized count values: the scored
// layer-sovereignty and deterministic-ordering counts plus always-zero values
// for the counts absent from RuleCounts.
func assertCountValues(t *testing.T, node string, fitness Fitness, wantLayer, wantOrdering float64) {
	t.Helper()
	if fitness.LayerSovereignty != wantLayer {
		t.Fatalf("%s layer sovereignty = %v, want %v", node, fitness.LayerSovereignty, wantLayer)
	}
	if fitness.DeterministicOrdering != wantOrdering {
		t.Fatalf("%s deterministic ordering = %v, want %v", node, fitness.DeterministicOrdering, wantOrdering)
	}
	if fitness.TemporalPurity != 0 || fitness.SQLCompositionSafety != 0 {
		t.Fatalf("%s fitness = %+v, want zero for counts absent from RuleCounts", node, fitness)
	}
}

// assertDocumentFields checks that the marshaled architecture document carries
// every nonzero count and omits every zero-valued generalized function key.
func assertDocumentFields(t *testing.T, content string, present, absent []string) {
	t.Helper()
	for _, field := range present {
		if !strings.Contains(content, field) {
			t.Errorf("architecture document missing %s: %s", field, content)
		}
	}
	for _, field := range absent {
		if strings.Contains(content, field) {
			t.Errorf("architecture document emits zero-valued %s, want omitted: %s", field, content)
		}
	}
}
