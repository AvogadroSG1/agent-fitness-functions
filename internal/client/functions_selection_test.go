package client

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestParseFunctionsFlagBuildsFullFiveKeyMapFromSelection locks the onboarding
// picker contract: --functions selects a subset of the catalog, but the
// scaffolded config must always carry all five keys explicitly (unselected
// ones false) so the mounted-config schema stays uniform and the server's
// explicit-false semantics apply.
func TestParseFunctionsFlagBuildsFullFiveKeyMapFromSelection(t *testing.T) {
	selected, err := parseFunctionsFlag("cyclomatic-complexity,logic-density")
	if err != nil {
		t.Fatalf("parseFunctionsFlag: %v", err)
	}
	if len(selected) != len(fitnessFunctionKeys) {
		t.Fatalf("selected = %v, want all %d keys present", selected, len(fitnessFunctionKeys))
	}
	for _, key := range fitnessFunctionKeys {
		want := key == "cyclomatic-complexity" || key == "logic-density"
		if selected[key] != want {
			t.Fatalf("selected[%q] = %v, want %v", key, selected[key], want)
		}
	}
}

// TestParseFunctionsFlagRejectsUnknownAndUnderscoreNames locks the guard rail:
// hyphenated names are canonical; underscore spellings and unknown names are
// usage errors that name the valid set, caught before anything touches disk.
func TestParseFunctionsFlagRejectsUnknownAndUnderscoreNames(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{name: "underscore spelling", raw: "cyclomatic_complexity"},
		{name: "unknown function", raw: "quantum-entanglement"},
		{name: "empty selection", raw: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseFunctionsFlag(tc.raw)
			if err == nil {
				t.Fatalf("parseFunctionsFlag(%q) = nil error, want usage error", tc.raw)
			}
			if !IsUsageError(err) {
				t.Fatalf("parseFunctionsFlag(%q) err = %v, want usage error", tc.raw, err)
			}
			if !strings.Contains(err.Error(), "cyclomatic-complexity") {
				t.Fatalf("error %q must name the valid function set", err)
			}
		})
	}
}

// TestRenderScaffoldConfigHonorsSelectedFunctions locks that a subset selected
// during onboarding lands in the scaffolded config exactly: all five keys
// present, only the selection true, and nil selection preserving today's
// all-enabled default.
func TestRenderScaffoldConfigHonorsSelectedFunctions(t *testing.T) {
	selected := map[string]bool{
		"cyclomatic-complexity": true,
		"interface-width":       false,
		"implementation-depth":  false,
		"logic-density":         true,
		"dependency-discipline": false,
	}
	content, err := renderScaffoldConfig("advisory", selected)
	if err != nil {
		t.Fatalf("renderScaffoldConfig with selection: %v", err)
	}
	var doc scaffoldConfigDocument
	if err := json.Unmarshal(content, &doc); err != nil {
		t.Fatalf("unmarshal scaffolded config: %v\n%s", err, content)
	}
	if len(doc.FitnessFunctions) != len(fitnessFunctionKeys) {
		t.Fatalf("fitness-functions = %v, want %d keys", doc.FitnessFunctions, len(fitnessFunctionKeys))
	}
	for key, want := range selected {
		if doc.FitnessFunctions[key] != want {
			t.Fatalf("fitness-functions[%q] = %v, want %v", key, doc.FitnessFunctions[key], want)
		}
	}

	defaulted, err := renderScaffoldConfig("advisory", nil)
	if err != nil {
		t.Fatalf("renderScaffoldConfig with nil selection: %v", err)
	}
	var defaultDoc scaffoldConfigDocument
	if err := json.Unmarshal(defaulted, &defaultDoc); err != nil {
		t.Fatalf("unmarshal defaulted config: %v\n%s", err, defaulted)
	}
	for _, key := range fitnessFunctionKeys {
		if !defaultDoc.FitnessFunctions[key] {
			t.Fatalf("nil selection must keep %q enabled (all-five default)", key)
		}
	}
}

// TestFunctionsCommandListsEmbeddedCatalogOffline locks the discovery UX for
// scripts and air-gapped use: `client functions` renders the full catalog from
// the embedded governance pattern without needing a server.
func TestFunctionsCommandListsEmbeddedCatalogOffline(t *testing.T) {
	var stdout bytes.Buffer
	if err := RunFunctions(nil, &stdout, nil); err != nil {
		t.Fatalf("RunFunctions offline: %v", err)
	}
	output := stdout.String()
	for _, key := range fitnessFunctionKeys {
		if !strings.Contains(output, key) {
			t.Fatalf("catalog output missing %q:\n%s", key, output)
		}
	}
	for _, operator := range []string{"lte", "gte"} {
		if !strings.Contains(output, operator) {
			t.Fatalf("catalog output missing operator %q:\n%s", operator, output)
		}
	}
}
