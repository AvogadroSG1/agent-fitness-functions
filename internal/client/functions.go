package client

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/analyzer"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/govconfig"
)

// functionCatalogEntry is the rendered-table shape shared by the offline (embedded
// governance pattern) and remote (GET /functions) catalog sources.
type functionCatalogEntry struct {
	Name        string
	Description string
	Threshold   float64
	Operator    string
	Unit        string
	// DefaultEnabled mirrors govconfig.Default(): five metric functions on,
	// four generalized functions off.
	DefaultEnabled bool
}

// remoteFunctionsResponse mirrors the server's GET /functions body.
type remoteFunctionsResponse struct {
	Version   string                          `json:"version"`
	Functions map[string]remoteFunctionsEntry `json:"functions"`
}

type remoteFunctionsEntry struct {
	Description    string  `json:"description"`
	Threshold      float64 `json:"threshold"`
	Operator       string  `json:"operator"`
	Unit           string  `json:"unit"`
	DefaultEnabled bool    `json:"default_enabled"`
}

// RunFunctions renders the catalog of governed fitness functions: their description,
// threshold, operator, and unit. Offline (the default) reads the embedded governance
// pattern via analyzer.GlobalThresholds(), so the catalog is available without a
// running server. --remote fetches the same catalog from a live daemon's
// GET /functions instead.
func RunFunctions(args []string, stdout io.Writer, httpClient *http.Client) error {
	flags := flag.NewFlagSet("client functions", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	addr := flags.String("addr", defaultOnboardAddr, "governance daemon base URL")
	remote := flags.Bool("remote", false, "fetch the catalog from a live daemon instead of the embedded pattern")
	clientCert := flags.String("client-cert", "", "client certificate for --remote (PEM)")
	clientKey := flags.String("client-key", "", "client key for --remote (PEM)")
	clientCA := flags.String("client-ca", "", "server CA bundle for --remote (PEM)")
	if err := flags.Parse(args); err != nil {
		return usageError{err: err}
	}
	entries, err := resolveFunctionCatalog(*remote, *addr, httpClient, *clientCert, *clientKey, *clientCA)
	if err != nil {
		return err
	}
	renderFunctionsTable(stdout, entries)
	return nil
}

func resolveFunctionCatalog(remote bool, addr string, httpClient *http.Client, certFlag, keyFlag, caFlag string) ([]functionCatalogEntry, error) {
	if !remote {
		return offlineFunctionCatalog()
	}
	client, err := remoteCatalogClient(httpClient, addr, certFlag, keyFlag, caFlag)
	if err != nil {
		return nil, err
	}
	return remoteFunctionCatalog(addr, client)
}

// remoteCatalogClient resolves the TLS material for --remote the same way
// client validate does (flags, then AGENT_FITNESS_FUNCTIONS_CLIENT_* env, then
// the managed dev-cert directory), minus the daemon auto-start: fetching a
// catalog is read-only and must not spawn anything.
func remoteCatalogClient(httpClient *http.Client, addr, certFlag, keyFlag, caFlag string) (*http.Client, error) {
	if httpClient != nil {
		return httpClient, nil
	}
	mode, err := resolveClientTLSMode(certFlag, keyFlag, caFlag, resolveDevCertDir())
	if err != nil {
		return nil, err
	}
	if mode.managed && !isLocalHTTPS(addr) {
		mode = clientTLSMode{}
	}
	material, err := loadClientTLSMode(mode, mode.managed)
	if err != nil {
		return nil, err
	}
	return configureClientTLSMaterial(&http.Client{Timeout: 3 * time.Second}, material)
}

func offlineFunctionCatalog() ([]functionCatalogEntry, error) {
	rules, err := analyzer.GlobalThresholds()
	if err != nil {
		return nil, fmt.Errorf("loading embedded governance pattern: %w", err)
	}
	return buildCatalogEntries(func(key string) remoteFunctionsEntry {
		rule := rules[key]
		return remoteFunctionsEntry{Description: rule.Description, Threshold: rule.Threshold, Operator: rule.Operator, Unit: rule.Unit}
	}), nil
}

func remoteFunctionCatalog(addr string, httpClient *http.Client) ([]functionCatalogEntry, error) {
	body, err := fetchRemoteFunctions(httpClient, addr)
	if err != nil {
		return nil, err
	}
	entries := buildCatalogEntries(func(key string) remoteFunctionsEntry {
		return body.Functions[key]
	})
	return overlayRemoteDefaults(entries, body.Functions), nil
}

func fetchRemoteFunctions(httpClient *http.Client, addr string) (remoteFunctionsResponse, error) {
	resp, err := httpClient.Get(strings.TrimRight(addr, "/") + "/functions")
	if err != nil {
		return remoteFunctionsResponse{}, fmt.Errorf("fetching %s/functions: %w", addr, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body remoteFunctionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return remoteFunctionsResponse{}, fmt.Errorf("decoding %s/functions response: %w", addr, err)
	}
	return body, nil
}

// buildCatalogEntries iterates all nine keys in canonical catalog order,
// resolving each entry's rule fields via lookup (map iteration order is
// otherwise random). DefaultEnabled comes from govconfig.Default() rather than
// lookup, so an offline catalog and a lookup that carries no enablement both
// report the shipped defaults; remote mode overlays the daemon's own answer.
func buildCatalogEntries(lookup func(key string) remoteFunctionsEntry) []functionCatalogEntry {
	keys := allFitnessFunctionKeys()
	defaults := govconfig.Default().FitnessFunctions
	entries := make([]functionCatalogEntry, 0, len(keys))
	for _, key := range keys {
		found := lookup(key)
		entries = append(entries, functionCatalogEntry{
			Name:           key,
			Description:    found.Description,
			Threshold:      found.Threshold,
			Operator:       found.Operator,
			Unit:           found.Unit,
			DefaultEnabled: defaults[key],
		})
	}
	return entries
}

// overlayRemoteDefaults replaces DefaultEnabled with the daemon's own answer for
// every key the response actually carried, so --remote reports the governance
// the server applies rather than the client's compiled-in defaults.
func overlayRemoteDefaults(entries []functionCatalogEntry, remote map[string]remoteFunctionsEntry) []functionCatalogEntry {
	for i, entry := range entries {
		if found, ok := remote[entry.Name]; ok {
			entries[i].DefaultEnabled = found.DefaultEnabled
		}
	}
	return entries
}

func renderFunctionsTable(w io.Writer, entries []functionCatalogEntry) {
	_, _ = fmt.Fprintf(w, "%-24s %-4s %-10s %-10s %-8s %s\n",
		"fitness function", "op", "threshold", "unit", "default", "description")
	for _, entry := range entries {
		_, _ = fmt.Fprintf(w, "%-24s %-4s %-10.3f %-10s %-8s %s\n",
			entry.Name, entry.Operator, entry.Threshold, entry.Unit,
			defaultEnabledLabel(entry.DefaultEnabled), entry.Description)
	}
}

// defaultEnabledLabel renders the default column: on for the metric functions
// every governed repo gets, off for the opt-in generalized ones.
func defaultEnabledLabel(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

// scaffoldableGeneralizedFunctionKeys are the generalized fitness functions
// --functions may select. layer-sovereignty is deliberately excluded: it
// requires layer definitions under fitness-function-settings that onboard has
// no way to infer non-interactively, so selecting it is a usage error instead.
var scaffoldableGeneralizedFunctionKeys = []string{
	"temporal-purity",
	"sql-composition-safety",
	"deterministic-ordering",
}

// parseFunctionsFlag validates a comma-separated --functions selection against the
// canonical hyphenated fitnessFunctionKeys plus the scaffoldable generalized
// functions. The five classic keys are always present in the returned map
// (selected ones true, the rest explicitly false) so the scaffolded config
// schema stays uniform. A scaffoldable generalized key is only added to the
// returned map when the caller actually selected it — an unselected
// generalized function is omitted rather than pinned false here, so a
// purely-classic selection returns exactly the historical five-key map;
// renderGovernanceConfig widens it to all nine keys only once a generalized
// function is present. Underscore spellings, unknown names, and an empty
// selection are all rejected as usage errors naming the valid set; selecting
// layer-sovereignty is rejected with a pointer at fitness-function-settings
// layer configuration.
func parseFunctionsFlag(raw string) (map[string]bool, error) {
	acceptable := make(map[string]bool, len(fitnessFunctionKeys)+len(scaffoldableGeneralizedFunctionKeys))
	for _, key := range fitnessFunctionKeys {
		acceptable[key] = false // classic: always present
	}
	for _, key := range scaffoldableGeneralizedFunctionKeys {
		acceptable[key] = true // generalized: present only when selected
	}
	selected := make(map[string]bool, len(fitnessFunctionKeys))
	for _, key := range fitnessFunctionKeys {
		selected[key] = false
	}
	selectedAny := false
	for _, rawName := range strings.Split(raw, ",") {
		name := strings.TrimSpace(rawName)
		if name == "" {
			continue
		}
		if name == "layer-sovereignty" {
			return nil, layerSovereigntyFlagUsageError()
		}
		if _, ok := acceptable[name]; !ok {
			return nil, functionsFlagUsageError(name)
		}
		selected[name] = true
		selectedAny = true
	}
	if !selectedAny {
		return nil, functionsFlagUsageError(raw)
	}
	return selected, nil
}

func functionsFlagUsageError(bad string) error {
	allowed := make([]string, 0, len(fitnessFunctionKeys)+len(scaffoldableGeneralizedFunctionKeys))
	allowed = append(allowed, fitnessFunctionKeys...)
	allowed = append(allowed, scaffoldableGeneralizedFunctionKeys...)
	return usageError{err: fmt.Errorf(
		"invalid --functions selection %q: choose one or more of %s (comma-separated, hyphenated names)",
		bad, strings.Join(allowed, ", "))}
}

// layerSovereigntyRemedy is shared by the --functions rejection and the
// interactive picker guard so both name the same way forward. A flag-driven
// run has no way to ask for layers, so it names the two that do: the onboard
// wizard on a terminal, or a hand-written settings block.
const layerSovereigntyRemedy = "it requires layer definitions: " +
	"run client onboard on a terminal and enable it there — the wizard prompts for the layers — " +
	"or define fitness-function-settings.layer-sovereignty.layers in the config file by hand"

func layerSovereigntyFlagUsageError() error {
	return usageError{err: errors.New(
		"layer-sovereignty cannot be scaffolded via --functions: " + layerSovereigntyRemedy)}
}

// rejectUnsettableSelection fails a selection that turned on a fitness
// function whose settings the caller cannot supply, before anything is
// written to disk. layer-sovereignty is the only one today. The onboard
// wizard no longer needs this guard — it prompts for the layers — so it
// covers the selections assembled without a terminal to prompt at.
func rejectUnsettableSelection(selected map[string]bool) error {
	if selected["layer-sovereignty"] {
		return fmt.Errorf("layer-sovereignty cannot be enabled from the picker alone: %s", layerSovereigntyRemedy)
	}
	return nil
}
