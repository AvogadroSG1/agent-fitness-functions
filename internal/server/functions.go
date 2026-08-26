package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/calm"
	"github.com/AvogadroSG1/agent-fitness-functions/patterns"
)

// FunctionsResponse is the JSON response returned by GET /functions.
type FunctionsResponse struct {
	Version   string                          `json:"version"`
	Functions map[string]FunctionCatalogEntry `json:"functions"`
}

// FunctionCatalogEntry describes one fitness function in the discovery catalog.
type FunctionCatalogEntry struct {
	Description    string  `json:"description"`
	Threshold      float64 `json:"threshold"`
	Operator       string  `json:"operator"`
	Unit           string  `json:"unit"`
	DefaultEnabled bool    `json:"default_enabled"`
}

// functionsHandler answers "what fitness functions exist and what do they
// enforce?" for any authenticated caller (authentication is enforced by
// withAuthenticatedCaller, not here). It is unprivileged and repo-agnostic so
// self-service onboarding can offer a picker before a repo is registered.
func functionsHandler(checker Checker, options HandlerOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		catalog, err := loadFunctionCatalog(checker.PatternPath)
		if err != nil {
			writeCheckError(w, infrastructureError("loading fitness function catalog", err))
			return
		}
		writeJSON(w, buildFunctionsResponse(catalog))
	}
}

// loadFunctionCatalog loads the governance pattern the same way Checker.resolvePatternPath
// does for /check, so the catalog always matches what /check enforces.
func loadFunctionCatalog(patternPath string) (map[string]FunctionCatalogEntry, error) {
	var pattern calm.Pattern
	var err error
	if patternPath != "" {
		pattern, err = calm.LoadPattern(patternPath)
	} else {
		pattern, err = calm.LoadPatternFromBytes("patterns/governance.json", patterns.GovernanceJSON)
	}
	if err != nil {
		return nil, err
	}
	defaults := defaultConfig().FitnessFunctions
	catalog := make(map[string]FunctionCatalogEntry, len(pattern.FitnessFunctions))
	for name, rule := range pattern.FitnessFunctions {
		catalog[name] = FunctionCatalogEntry{
			Description:    rule.Description,
			Threshold:      rule.Threshold,
			Operator:       rule.Operator,
			Unit:           rule.Unit,
			DefaultEnabled: defaults[name],
		}
	}
	return catalog, nil
}

func buildFunctionsResponse(catalog map[string]FunctionCatalogEntry) FunctionsResponse {
	return FunctionsResponse{
		Version:   functionsResponseVersion(catalog),
		Functions: catalog,
	}
}

func functionsResponseVersion(functions map[string]FunctionCatalogEntry) string {
	payload, err := json.Marshal(functions)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}
