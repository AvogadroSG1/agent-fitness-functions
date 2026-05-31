package bridge

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

const (
	// StatusPass indicates that no active fitness function blocked the change.
	StatusPass CheckStatus = "pass"
	// StatusBlock indicates that an active fitness function rejected the change.
	StatusBlock CheckStatus = "block"
	// StatusAdvisory indicates that an active fitness function reported guidance without blocking.
	StatusAdvisory CheckStatus = "advisory"

	maxCheckRequestBytes = 10 << 20
	defaultConfigsDir    = "/app/configs"
)

// CheckStatus is the closed set of check outcomes returned by /check.
type CheckStatus string

// CheckRequest is the JSON body accepted by POST /check.
type CheckRequest struct {
	Repo            string `json:"repo"`
	File            string `json:"file"`
	ProposedContent string `json:"proposed_content"`
	Language        string `json:"language"`
}

// CheckResponse is the JSON response returned by POST /check.
type CheckResponse struct {
	Status     CheckStatus `json:"status"`
	Warming    bool        `json:"warming,omitempty"`
	Violations []Violation `json:"violations,omitempty"`
}

// StateResponse is the JSON response returned by GET /state for one repository.
type StateResponse struct {
	Repo       string      `json:"repo"`
	Violations []Violation `json:"violations"`
}

// ConfigsResponse is the JSON response returned by GET /configs.
type ConfigsResponse struct {
	Version  string                         `json:"version"`
	LoadedAt time.Time                      `json:"loaded_at"`
	Repos    map[string]ConfigsRepoResponse `json:"repos"`
}

// ConfigsRepoResponse describes one repo entry in GET /configs.
type ConfigsRepoResponse struct {
	Status           string          `json:"status"`
	EnforcementMode  EnforcementMode `json:"enforcement-mode,omitempty"`
	FitnessFunctions map[string]bool `json:"fitness-functions,omitempty"`
	Error            string          `json:"error,omitempty"`
	LastValidAt      *time.Time      `json:"last_valid_at,omitempty"`
}

// Violation describes one architectural fitness function failure.
type Violation struct {
	FitnessFunction string  `json:"fitness_function"`
	CALMNode        string  `json:"calm_node"`
	File            string  `json:"file,omitempty"`
	Function        string  `json:"function,omitempty"`
	Value           float64 `json:"value"`
	Limit           float64 `json:"limit"`
	Message         string  `json:"message"`
}

type ServeOptions struct {
	Addr           string
	ConfigDir      string
	Ready          io.Writer
	NewStore       func(context.Context, string) (*ConfigStore, error)
	HandlerOptions HandlerOptions
	TLS            ServerTLSConfig
}

// NewHandler builds the calm-bridge HTTP daemon routes.
func NewHandler(configStore *ConfigStore, shutdown func()) http.Handler {
	return NewHandlerWithOptions(Checker{ConfigStore: configStore}, shutdown, HandlerOptions{})
}

// NewHandlerWithChecker builds the calm-bridge HTTP daemon routes with injected check dependencies.
func NewHandlerWithChecker(checker Checker, shutdown func()) http.Handler {
	return NewHandlerWithOptions(checker, shutdown, HandlerOptions{})
}

func NewHandlerWithOptions(checker Checker, shutdown func(), options HandlerOptions) http.Handler {
	if checker.State == nil {
		checker.State = NewState()
	}
	deferredCtx, cancelDeferred := context.WithCancel(context.Background())
	if checker.DeferredContext == nil {
		checker.DeferredContext = deferredCtx
	} else {
		cancelDeferred()
		cancelDeferred = func() {}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/check", withAuthenticatedCaller(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var caller string
		if options.RequireAuthentication {
			var ok bool
			caller, ok = authenticatedCaller(r)
			if !ok {
				writeUnauthorized(w)
				return
			}
		}
		var request CheckRequest
		body := http.MaxBytesReader(w, r.Body, maxCheckRequestBytes)
		defer body.Close()
		decoder := json.NewDecoder(body)
		if err := decoder.Decode(&request); err != nil {
			http.Error(w, "invalid check request", http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			http.Error(w, "invalid check request", http.StatusBadRequest)
			return
		}
		if options.RequireAuthentication {
			authorizedRepo, err := authorizeRepoAccess(checker.ConfigStore, caller, request.Repo)
			if err != nil {
				if isInputError(err) {
					writeCheckError(w, err)
					return
				}
				writeForbidden(w, err.Error())
				return
			}
			request.Repo = authorizedRepo
		}
		response, err := (&checker).Check(r.Context(), request)
		if err != nil {
			writeCheckError(w, err)
			return
		}
		writeJSON(w, response)
	}, options))
	mux.HandleFunc("/state", withAuthenticatedCaller(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var caller string
		if options.RequireAuthentication {
			var ok bool
			caller, ok = authenticatedCaller(r)
			if !ok {
				writeUnauthorized(w)
				return
			}
		}
		repo := r.URL.Query().Get("repo")
		if repo == "" {
			http.Error(w, "state requires repo", http.StatusBadRequest)
			return
		}
		targetRepo := repo
		if options.RequireAuthentication {
			authorizedRepo, err := authorizeRepoAccess(checker.ConfigStore, caller, repo)
			if err != nil {
				if isInputError(err) {
					writeCheckError(w, err)
					return
				}
				writeForbidden(w, err.Error())
				return
			}
			targetRepo = authorizedRepo
		}
		if _, canonicalRepo, err := loadConfig(checker.ConfigStore, targetRepo); err != nil {
			writeCheckError(w, err)
			return
		} else {
			writeJSON(w, StateResponse{Repo: canonicalRepo, Violations: checker.State.Violations(canonicalRepo)})
		}
	}, options))
	mux.HandleFunc("/configs", withAuthenticatedCaller(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var caller string
		if options.RequireAuthentication {
			var ok bool
			caller, ok = authenticatedCaller(r)
			if !ok {
				writeUnauthorized(w)
				return
			}
		}
		if checker.ConfigStore == nil {
			writeCheckError(w, infrastructureError("config store is not configured", nil))
			return
		}
		if options.RequireAuthentication && !checker.ConfigStore.CallerIsAdmin(caller) {
			writeForbidden(w, fmt.Sprintf("caller %q is not authorized for /configs", caller))
			return
		}
		loadedAt, snapshot := checker.ConfigStore.SnapshotState()
		writeJSON(w, buildConfigsResponse(loadedAt, snapshot))
	}, options))
	mux.HandleFunc("/shutdown", withAuthenticatedCaller(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if options.RequireAuthentication {
			caller, ok := authenticatedCaller(r)
			if !ok {
				writeUnauthorized(w)
				return
			}
			if checker.ConfigStore == nil {
				writeCheckError(w, infrastructureError("config store is not configured", nil))
				return
			}
			if !checker.ConfigStore.CallerIsAdmin(caller) {
				writeForbidden(w, fmt.Sprintf("caller %q is not authorized for /shutdown", caller))
				return
			}
		}
		writeJSON(w, map[string]string{"status": "shutting_down"})
		cancelDeferred()
		if shutdown != nil {
			shutdown()
		}
	}, options))
	return mux
}

// Serve starts the daemon on addr and blocks until ctx is canceled or the server fails.
func Serve(ctx context.Context, addr string) error {
	return ServeWithOptions(ctx, ServeOptions{
		Addr:      addr,
		ConfigDir: os.Getenv("CALM_CONFIGS_DIR"),
		Ready:     os.Stdout,
		NewStore:  NewConfigStore,
	})
}

func ServeWithOptions(ctx context.Context, options ServeOptions) error {
	if options.Addr == "" {
		options.Addr = "localhost:7890"
	}
	if options.ConfigDir == "" {
		options.ConfigDir = os.Getenv("CALM_CONFIGS_DIR")
	}
	if options.Ready == nil {
		options.Ready = os.Stdout
	}
	if options.NewStore == nil {
		options.NewStore = NewConfigStore
	}
	return serveWithOptions(ctx, options)
}

func serveWithDependencies(ctx context.Context, addr, configDir string, ready io.Writer, newStore func(context.Context, string) (*ConfigStore, error)) error {
	return serveWithOptions(ctx, ServeOptions{Addr: addr, ConfigDir: configDir, Ready: ready, NewStore: newStore})
}

func serveWithOptions(ctx context.Context, options ServeOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if options.ConfigDir == "" {
		options.ConfigDir = defaultConfigsDir
	}
	if options.NewStore == nil {
		options.NewStore = NewConfigStore
	}
	if err := options.TLS.Validate(); err != nil {
		return err
	}
	store, err := options.NewStore(ctx, options.ConfigDir)
	if err != nil {
		return err
	}
	defer func() {
		_ = store.Close()
	}()

	shutdownRequested := make(chan struct{}, 1)
	handlerOptions := options.HandlerOptions
	handlerOptions.RequireAuthentication = true
	if handlerOptions.TrustedProxyHeaders {
		if !options.TLS.Enabled() {
			return errors.New("trusted proxy mode requires TLS")
		}
		if len(handlerOptions.TrustedProxyClientCNs) == 0 {
			return errors.New("trusted proxy mode requires at least one trusted proxy client CN")
		}
	}
	listener, err := net.Listen("tcp", options.Addr)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr: options.Addr,
		Handler: NewHandlerWithOptions(Checker{ConfigStore: store}, func() {
			select {
			case shutdownRequested <- struct{}{}:
			default:
			}
		}, handlerOptions),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
	}
	if options.TLS.Enabled() {
		server.TLSConfig, err = loadServerTLSConfig(options.TLS)
		if err != nil {
			return err
		}
		listener = tlsListener(listener, server.TLSConfig)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()
	if options.Ready != nil {
		_, _ = fmt.Fprintln(options.Ready, "ready")
	}

	select {
	case <-ctx.Done():
		return shutdown(server, ctx.Err())
	case <-shutdownRequested:
		return shutdown(server, nil)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func isInputError(err error) bool {
	var checkErr *CheckError
	return errors.As(err, &checkErr) && checkErr.Kind == ErrorKindInput
}

func tlsListener(listener net.Listener, config *tls.Config) net.Listener {
	if config == nil {
		return listener
	}
	return tls.NewListener(listener, config)
}

func shutdown(server *http.Server, returnErr error) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	return returnErr
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		http.Error(w, "encoding response failed", http.StatusInternalServerError)
	}
}

func writeCheckError(w http.ResponseWriter, err error) {
	var checkErr *CheckError
	if errors.As(err, &checkErr) {
		switch checkErr.Kind {
		case ErrorKindInput:
			http.Error(w, checkErr.Message, http.StatusBadRequest)
			return
		case ErrorKindInfrastructure:
			http.Error(w, checkErr.Message, http.StatusServiceUnavailable)
			return
		case ErrorKindNotFound:
			http.Error(w, checkErr.Message, http.StatusNotFound)
			return
		}
	}
	http.Error(w, "check failed", http.StatusInternalServerError)
}

func buildConfigsResponse(loadedAt time.Time, snapshot map[string]ConfigEntry) ConfigsResponse {
	response := ConfigsResponse{
		LoadedAt: loadedAt,
		Repos:    make(map[string]ConfigsRepoResponse, len(snapshot)),
	}
	for repo, entry := range snapshot {
		repoResponse := ConfigsRepoResponse{Status: "invalid", Error: entry.Error}
		if entry.Valid {
			repoResponse.Status = "valid"
			repoResponse.EnforcementMode = entry.Config.EnforcementMode
			repoResponse.FitnessFunctions = cloneFitnessFunctions(entry.Config.FitnessFunctions)
		} else if !entry.LastValidAt.IsZero() {
			lastValidAt := entry.LastValidAt
			repoResponse.LastValidAt = &lastValidAt
		}
		response.Repos[repo] = repoResponse
	}
	response.Version = configsResponseVersion(response.LoadedAt, response.Repos)
	return response
}

func configsResponseVersion(loadedAt time.Time, repos map[string]ConfigsRepoResponse) string {
	payload, err := json.Marshal(struct {
		LoadedAt time.Time                      `json:"loaded_at"`
		Repos    map[string]ConfigsRepoResponse `json:"repos"`
	}{LoadedAt: loadedAt, Repos: repos})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func cloneFitnessFunctions(functions map[string]bool) map[string]bool {
	if functions == nil {
		return nil
	}
	cloned := make(map[string]bool, len(functions))
	for name, enabled := range functions {
		cloned[name] = enabled
	}
	return cloned
}
