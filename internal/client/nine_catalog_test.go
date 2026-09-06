package client

import (
	"strings"
	"testing"
)

// S8 red-test contract (calm-poc-ld1u): the client catalog surfaces all nine
// fitness functions everywhere — `client functions`, the interactive picker —
// with the govconfig defaults (five metric on, four generalized off), and the
// --functions rejection of layer-sovereignty points at interactive onboard.

var allNineFunctionKeys = []string{
	"cyclomatic-complexity",
	"interface-width",
	"implementation-depth",
	"logic-density",
	"dependency-discipline",
	"layer-sovereignty",
	"temporal-purity",
	"sql-composition-safety",
	"deterministic-ordering",
}

func TestBuildCatalogEntriesListsAllNineWithDefaults(t *testing.T) {
	entries := buildCatalogEntries(func(key string) remoteFunctionsEntry {
		return remoteFunctionsEntry{Description: "about " + key}
	})
	if len(entries) != len(allNineFunctionKeys) {
		t.Fatalf("catalog has %d entries, want %d", len(entries), len(allNineFunctionKeys))
	}
	defaults := map[string]bool{}
	for index, entry := range entries {
		if entry.Name != allNineFunctionKeys[index] {
			t.Errorf("entry %d = %q, want %q (canonical order: five metric then four generalized)", index, entry.Name, allNineFunctionKeys[index])
		}
		defaults[entry.Name] = entry.DefaultEnabled
	}
	for _, metric := range allNineFunctionKeys[:5] {
		if !defaults[metric] {
			t.Errorf("%s must default enabled", metric)
		}
	}
	for _, generalized := range allNineFunctionKeys[5:] {
		if defaults[generalized] {
			t.Errorf("%s must default disabled", generalized)
		}
	}
}

func TestRenderFunctionsTableShowsDefaultColumnAndNineRows(t *testing.T) {
	var out strings.Builder
	renderFunctionsTable(&out, buildCatalogEntries(func(key string) remoteFunctionsEntry {
		return remoteFunctionsEntry{Description: "about " + key}
	}))
	rendered := out.String()
	if !strings.Contains(rendered, "default") {
		t.Errorf("table header must include a default column:\n%s", rendered)
	}
	for _, key := range allNineFunctionKeys {
		if !strings.Contains(rendered, key) {
			t.Errorf("table must list %s:\n%s", key, rendered)
		}
	}
}

func TestLoadPickerOptionsListsAllNine(t *testing.T) {
	options, err := loadPickerOptions()
	if err != nil {
		t.Fatalf("loadPickerOptions: %v", err)
	}
	if len(options) != len(allNineFunctionKeys) {
		t.Fatalf("picker has %d options, want %d", len(options), len(allNineFunctionKeys))
	}
	for index, option := range options {
		if option.name != allNineFunctionKeys[index] {
			t.Errorf("option %d = %q, want %q", index, option.name, allNineFunctionKeys[index])
		}
		if option.description == "" {
			t.Errorf("option %s must carry a description from the embedded pattern", option.name)
		}
		wantEnabled := index < 5
		if option.enabled != wantEnabled {
			t.Errorf("option %s enabled = %v, want %v (govconfig defaults)", option.name, option.enabled, wantEnabled)
		}
		wantSettings := option.name == "layer-sovereignty"
		if option.requiresSettings != wantSettings {
			t.Errorf("option %s requiresSettings = %v, want %v", option.name, option.requiresSettings, wantSettings)
		}
	}
}

func TestLayerSovereigntyFlagErrorPointsAtInteractiveOnboard(t *testing.T) {
	_, err := parseFunctionsFlag("layer-sovereignty")
	if err == nil {
		t.Fatal("parseFunctionsFlag(layer-sovereignty) succeeded, want a usage error")
	}
	message := err.Error()
	if !strings.Contains(message, "fitness-function-settings") {
		t.Errorf("error %q must point at the settings requirement", message)
	}
	if !strings.Contains(message, "onboard") {
		t.Errorf("error %q must point at interactive onboard as the way to enable it", message)
	}
}
