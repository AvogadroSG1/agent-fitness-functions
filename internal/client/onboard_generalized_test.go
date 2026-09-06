package client

import (
	"encoding/json"
	"strings"
	"testing"
)

// Red tests for calm-poc-zak (S10): the scaffolded config lists all nine
// fitness functions explicitly — the five classic ones enabled, the four
// generalized ones disabled (opt-in) — and --functions accepts the
// generalized names except layer-sovereignty, which cannot be scaffolded
// without layer definitions and must point the user at
// fitness-function-settings.
func TestRenderScaffoldConfigListsGeneralizedFunctionsDisabled(t *testing.T) {
	content, err := renderGovernanceConfig("block", nil, nil, configExtras{})
	if err != nil {
		t.Fatalf("renderGovernanceConfig: %v", err)
	}
	var doc scaffoldConfigDocument
	if err := json.Unmarshal(content, &doc); err != nil {
		t.Fatalf("unmarshal scaffolded config: %v\n%s", err, content)
	}
	for _, key := range generalizedFitnessFunctionKeys {
		enabled, ok := doc.FitnessFunctions[key]
		if !ok {
			t.Fatalf("scaffold missing generalized function %q: %v", key, doc.FitnessFunctions)
		}
		if enabled {
			t.Fatalf("scaffold enables %q by default, want opt-in false", key)
		}
	}
}

func TestParseFunctionsFlagAcceptsGeneralizedFunctions(t *testing.T) {
	selected, err := parseFunctionsFlag("cyclomatic-complexity,temporal-purity,deterministic-ordering")
	if err != nil {
		t.Fatalf("parseFunctionsFlag: %v", err)
	}
	for _, key := range []string{"temporal-purity", "deterministic-ordering"} {
		if !selected[key] {
			t.Fatalf("selected = %v, want %q enabled", selected, key)
		}
	}
	content, err := renderGovernanceConfig("block", selected, nil, configExtras{})
	if err != nil {
		t.Fatalf("renderGovernanceConfig: %v", err)
	}
	var doc scaffoldConfigDocument
	if err := json.Unmarshal(content, &doc); err != nil {
		t.Fatalf("unmarshal scaffolded config: %v", err)
	}
	if !doc.FitnessFunctions["temporal-purity"] {
		t.Fatalf("scaffold = %v, want temporal-purity true", doc.FitnessFunctions)
	}
	if enabled, ok := doc.FitnessFunctions["layer-sovereignty"]; !ok || enabled {
		t.Fatalf("scaffold = %v, want layer-sovereignty present and false", doc.FitnessFunctions)
	}
}

func TestParseFunctionsFlagRejectsLayerSovereignty(t *testing.T) {
	_, err := parseFunctionsFlag("layer-sovereignty")
	if err == nil {
		t.Fatal("parseFunctionsFlag(layer-sovereignty) = nil error, want usage error")
	}
	if !strings.Contains(err.Error(), "fitness-function-settings") {
		t.Fatalf("error = %v, want pointer to fitness-function-settings layer configuration", err)
	}
}
