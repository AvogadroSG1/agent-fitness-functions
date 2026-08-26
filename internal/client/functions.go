package client

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/analyzer"
)

// functionCatalogEntry is the rendered-table shape shared by the offline (embedded
// governance pattern) and remote (GET /functions) catalog sources.
type functionCatalogEntry struct {
	Name        string
	Description string
	Threshold   float64
	Operator    string
	Unit        string
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

// RunFunctions renders the catalog of the five fitness functions: their description,
// threshold, operator, and unit. Offline (the default) reads the embedded governance
// pattern via analyzer.GlobalThresholds(), so the catalog is available without a
// running server. --remote fetches the same catalog from a live daemon's
// GET /functions instead.
func RunFunctions(args []string, stdout io.Writer, httpClient *http.Client) error {
	flags := flag.NewFlagSet("client functions", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	addr := flags.String("addr", defaultOnboardAddr, "governance daemon base URL")
	remote := flags.Bool("remote", false, "fetch the catalog from a live daemon instead of the embedded pattern")
	if err := flags.Parse(args); err != nil {
		return usageError{err: err}
	}
	entries, err := resolveFunctionCatalog(*remote, *addr, httpClient)
	if err != nil {
		return err
	}
	renderFunctionsTable(stdout, entries)
	return nil
}

func resolveFunctionCatalog(remote bool, addr string, httpClient *http.Client) ([]functionCatalogEntry, error) {
	if !remote {
		return offlineFunctionCatalog()
	}
	return remoteFunctionCatalog(addr, httpClient)
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
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 3 * time.Second}
	}
	body, err := fetchRemoteFunctions(httpClient, addr)
	if err != nil {
		return nil, err
	}
	return buildCatalogEntries(func(key string) remoteFunctionsEntry {
		return body.Functions[key]
	}), nil
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

// buildCatalogEntries iterates fitnessFunctionKeys in deterministic order, resolving
// each entry's fields via lookup (map iteration order is otherwise random).
func buildCatalogEntries(lookup func(key string) remoteFunctionsEntry) []functionCatalogEntry {
	entries := make([]functionCatalogEntry, 0, len(fitnessFunctionKeys))
	for _, key := range fitnessFunctionKeys {
		found := lookup(key)
		entries = append(entries, functionCatalogEntry{
			Name:        key,
			Description: found.Description,
			Threshold:   found.Threshold,
			Operator:    found.Operator,
			Unit:        found.Unit,
		})
	}
	return entries
}

func renderFunctionsTable(w io.Writer, entries []functionCatalogEntry) {
	_, _ = fmt.Fprintf(w, "%-24s %-4s %-10s %-10s %s\n", "fitness function", "op", "threshold", "unit", "description")
	for _, entry := range entries {
		_, _ = fmt.Fprintf(w, "%-24s %-4s %-10.3f %-10s %s\n",
			entry.Name, entry.Operator, entry.Threshold, entry.Unit, entry.Description)
	}
}

// parseFunctionsFlag validates a comma-separated --functions selection against the
// canonical hyphenated fitnessFunctionKeys and expands it to a full five-key map
// (selected keys true, the rest explicitly false) so the scaffolded config schema
// stays uniform. Underscore spellings, unknown names, and an empty selection are all
// rejected as usage errors naming the valid set.
func parseFunctionsFlag(raw string) (map[string]bool, error) {
	valid := make(map[string]bool, len(fitnessFunctionKeys))
	for _, key := range fitnessFunctionKeys {
		valid[key] = false
	}
	selectedAny := false
	for _, rawName := range strings.Split(raw, ",") {
		name := strings.TrimSpace(rawName)
		if name == "" {
			continue
		}
		if _, ok := valid[name]; !ok {
			return nil, functionsFlagUsageError(name)
		}
		valid[name] = true
		selectedAny = true
	}
	if !selectedAny {
		return nil, functionsFlagUsageError(raw)
	}
	return valid, nil
}

func functionsFlagUsageError(bad string) error {
	return usageError{err: fmt.Errorf(
		"invalid --functions selection %q: choose one or more of %s (comma-separated, hyphenated names)",
		bad, strings.Join(fitnessFunctionKeys, ", "))}
}
