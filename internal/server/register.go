package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// RegisterRequest is the JSON body for POST /register: self-service
// registration of a repository against a running governance server.
type RegisterRequest struct {
	Repo             string          `json:"repo"`
	EnforcementMode  string          `json:"enforcement-mode,omitempty"`
	FitnessFunctions map[string]bool `json:"fitness-functions,omitempty"`
}

// RegisterResponse is the JSON response for POST /register.
type RegisterResponse struct {
	Repo    string `json:"repo"`
	Created bool   `json:"created"`
}

// registerHandler lets an authenticated caller register a repository: it
// persists configs/<repo>/config.json, binds the caller's CN in
// caller-repos.json, and makes the repo servable synchronously — no fsnotify
// debounce wait.
func registerHandler(checker Checker, options HandlerOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if options.DisableRegistration {
			writeForbidden(w, "registration disabled")
			return
		}
		if checker.ConfigStore == nil {
			writeCheckError(w, infrastructureError("config store is not configured", nil))
			return
		}
		caller, ok := authenticatedCaller(r)
		if !ok {
			writeUnauthorized(w)
			return
		}
		repoName, config, ok := decodeRegisterConfig(w, r)
		if !ok {
			return
		}
		callerIsAdmin := checker.ConfigStore.CallerIsAdmin(caller)
		created, err := checker.ConfigStore.Register(r.Context(), repoName, config, caller, callerIsAdmin)
		if err != nil {
			writeRegisterError(w, err)
			return
		}
		writeRegisterResponse(w, repoName, created)
	}
}

// decodeRegisterConfig reads and validates the request body, writing the
// appropriate error response and returning ok=false on any failure.
func decodeRegisterConfig(w http.ResponseWriter, r *http.Request) (string, Config, bool) {
	request, ok := decodeRegisterRequest(w, r)
	if !ok {
		return "", Config{}, false
	}
	repoName, err := validateRegisterRepoName(request.Repo)
	if err != nil {
		writeCheckError(w, err)
		return "", Config{}, false
	}
	config, err := buildRegisterConfig(request)
	if err != nil {
		writeCheckError(w, err)
		return "", Config{}, false
	}
	return repoName, config, true
}

func decodeRegisterRequest(w http.ResponseWriter, r *http.Request) (RegisterRequest, bool) {
	var request RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid register request", http.StatusBadRequest)
		return RegisterRequest{}, false
	}
	return request, true
}

// validateRegisterRepoName requires the repo name already be in its exact
// canonical form. validateRepoName alone silently lowercases before matching
// (so it accepts and normalizes mixed-case input, which is right for reading
// directory names off disk); registration is a fresh request, not a name
// recovered from a filesystem, so a name that only becomes valid after
// lowercasing must be rejected rather than silently renamed.
func validateRegisterRepoName(repo string) (string, error) {
	name, err := validateRepoName(repo)
	if err != nil {
		return "", err
	}
	if name != repo {
		return "", inputError("invalid repository name", nil)
	}
	return name, nil
}

// buildRegisterConfig normalizes a RegisterRequest into a Config the same way
// parseConfigContent normalizes a mounted config.json, except the default
// enforcement mode for a fresh self-service registration is "advisory"
// (parseConfigContent defaults to "block" for hand-authored configs).
func buildRegisterConfig(request RegisterRequest) (Config, error) {
	mode := EnforcementMode(request.EnforcementMode)
	if mode == "" {
		mode = EnforcementAdvisory
	}
	switch mode {
	case EnforcementBlock, EnforcementAdvisory, EnforcementOff:
	default:
		return Config{}, inputError(fmt.Sprintf("unsupported enforcement mode %q", mode), nil)
	}
	fitnessFunctions, err := normalizeFitnessFunctions(request.FitnessFunctions)
	if err != nil {
		return Config{}, err
	}
	return Config{
		EnforcementMode:    mode,
		EnforcementOnError: EnforcementOnErrorBlock,
		FitnessFunctions:   fitnessFunctions,
	}, nil
}

func writeRegisterResponse(w http.ResponseWriter, repo string, created bool) {
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(RegisterResponse{Repo: repo, Created: created})
}

func writeRegisterError(w http.ResponseWriter, err error) {
	var conflict *registerConflictError
	if errors.As(err, &conflict) {
		http.Error(w, conflict.Error(), http.StatusConflict)
		return
	}
	writeCheckError(w, err)
}
