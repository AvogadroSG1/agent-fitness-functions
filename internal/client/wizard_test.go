package client

import (
	"strings"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/govconfig"
)

// S9 red-test contract (calm-poc-a65i): `client onboard` on a TTY is a wizard —
// a current-state panel, an enforcement prompt defaulting to the repo's
// current mode, a nine-function picker seeded from the current config, and a
// diff+confirm gate. Declining confirms nothing.

func wizardScript(lines ...string) *strings.Reader {
	return strings.NewReader(strings.Join(lines, "\n") + "\n")
}

func TestWizardFirstRunShowsNotOnboardedAndSeedsDefaults(t *testing.T) {
	var out strings.Builder
	outcome, err := runOnboardWizard(
		wizardScript("", "", "y"), // enforcement default, picker confirm, accept diff
		&out,
		wizardFacts{RepoName: "fresh-repo", DaemonSummary: "daemon: current (local-http)"},
	)
	if err != nil {
		t.Fatalf("runOnboardWizard: %v", err)
	}
	rendered := out.String()
	if !strings.Contains(rendered, "not onboarded") {
		t.Errorf("panel must say the repo is not onboarded:\n%s", rendered)
	}
	if !strings.Contains(rendered, "daemon: current (local-http)") {
		t.Errorf("panel must show the daemon summary:\n%s", rendered)
	}
	if !outcome.Confirmed {
		t.Fatal("accepting the diff must confirm the outcome")
	}
	if outcome.Enforcement != "advisory" {
		t.Errorf("first-run enforcement = %q, want the advisory default", outcome.Enforcement)
	}
	defaults := govconfig.Default().FitnessFunctions
	if len(outcome.Functions) != len(defaults) {
		t.Fatalf("outcome has %d functions, want the full nine-key map", len(outcome.Functions))
	}
	for key, enabled := range defaults {
		if outcome.Functions[key] != enabled {
			t.Errorf("function %s = %v, want the default %v", key, outcome.Functions[key], enabled)
		}
	}
}

func TestWizardUpdateRunSeedsFromCurrentConfigAndFlagsAttention(t *testing.T) {
	current := govconfig.Default()
	current.EnforcementMode = govconfig.EnforcementBlock
	current.FitnessFunctions["logic-density"] = false

	var out strings.Builder
	outcome, err := runOnboardWizard(
		wizardScript("", "", "y"),
		&out,
		wizardFacts{
			RepoName:      "governed-repo",
			Current:       &current,
			ConfigSynced:  false,
			DaemonSummary: "daemon: stale (listen mode mtls)",
			LegacyNotes:   []string{"legacy certs directory present"},
		},
	)
	if err != nil {
		t.Fatalf("runOnboardWizard: %v", err)
	}
	rendered := out.String()
	for _, want := range []string{"needs attention", "daemon: stale (listen mode mtls)", "legacy certs directory present"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("panel must contain %q:\n%s", want, rendered)
		}
	}
	if outcome.Enforcement != string(govconfig.EnforcementBlock) {
		t.Errorf("update-run enforcement = %q, want the current block mode as default", outcome.Enforcement)
	}
	if outcome.Functions["logic-density"] {
		t.Error("picker must seed logic-density off from the current config")
	}
	if !outcome.Functions["cyclomatic-complexity"] {
		t.Error("picker must keep currently enabled functions on")
	}
}

func TestWizardUpToDateRepoSaysSo(t *testing.T) {
	current := govconfig.Default()
	var out strings.Builder
	if _, err := runOnboardWizard(
		wizardScript("", "", "y"),
		&out,
		wizardFacts{RepoName: "governed-repo", Current: &current, ConfigSynced: true, DaemonSummary: "daemon: current (local-http)"},
	); err != nil {
		t.Fatalf("runOnboardWizard: %v", err)
	}
	if !strings.Contains(out.String(), "up to date") {
		t.Errorf("panel must say a healthy governed repo is up to date:\n%s", out.String())
	}
}

func TestWizardDecliningTheDiffConfirmsNothing(t *testing.T) {
	var out strings.Builder
	outcome, err := runOnboardWizard(
		wizardScript("block", "", "n"),
		&out,
		wizardFacts{RepoName: "fresh-repo"},
	)
	if err != nil {
		t.Fatalf("runOnboardWizard: %v", err)
	}
	if outcome.Confirmed {
		t.Fatal("declining the diff must not confirm")
	}
	if outcome.Enforcement != "block" {
		t.Errorf("enforcement = %q, want the typed block choice carried in the (unconfirmed) outcome", outcome.Enforcement)
	}
}
