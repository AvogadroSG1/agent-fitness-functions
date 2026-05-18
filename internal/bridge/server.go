package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const (
	// StatusPass indicates that no active fitness function blocked the change.
	StatusPass = "pass"
	// StatusBlock indicates that an active fitness function rejected the change.
	StatusBlock = "block"

	maxCheckRequestBytes = 10 << 20
)

// CheckRequest is the JSON body accepted by POST /check.
type CheckRequest struct {
	Repo            string `json:"repo"`
	File            string `json:"file"`
	ProposedContent string `json:"proposed_content"`
	Language        string `json:"language"`
}

// CheckResponse is the JSON response returned by POST /check.
type CheckResponse struct {
	Status     string      `json:"status"`
	Warming    bool        `json:"warming,omitempty"`
	Violations []Violation `json:"violations,omitempty"`
}

// Violation describes one architectural fitness function failure.
type Violation struct {
	FitnessFunction string  `json:"fitness_function"`
	CALMNode        string  `json:"calm_node"`
	Function        string  `json:"function,omitempty"`
	Value           float64 `json:"value"`
	Limit           float64 `json:"limit"`
	Message         string  `json:"message"`
}

// NewHandler builds the calm-bridge HTTP daemon routes.
func NewHandler(shutdown func()) http.Handler {
	return NewHandlerWithChecker(Checker{}, shutdown)
}

// NewHandlerWithChecker builds the calm-bridge HTTP daemon routes with injected check dependencies.
func NewHandlerWithChecker(checker Checker, shutdown func()) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/check", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var request CheckRequest
		body := http.MaxBytesReader(w, r.Body, maxCheckRequestBytes)
		defer body.Close()
		if err := json.NewDecoder(body).Decode(&request); err != nil {
			http.Error(w, "invalid check request", http.StatusBadRequest)
			return
		}
		response, err := checker.Check(r.Context(), request)
		if err != nil {
			http.Error(w, fmt.Sprintf("check failed: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, response)
	})
	mux.HandleFunc("/state", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, map[string]any{"repositories": map[string]any{}})
	})
	mux.HandleFunc("/shutdown", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, map[string]string{"status": "shutting_down"})
		if shutdown != nil {
			shutdown()
		}
	})
	return mux
}

// Serve starts the daemon on addr and blocks until ctx is canceled or the server fails.
func Serve(ctx context.Context, addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	shutdownRequested := make(chan struct{}, 1)
	server := &http.Server{
		Addr:              addr,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
	}
	server.Handler = NewHandler(func() {
		select {
		case shutdownRequested <- struct{}{}:
		default:
		}
	})

	errs := make(chan error, 1)
	go func() {
		errs <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		return shutdown(server, ctx.Err())
	case <-shutdownRequested:
		return shutdown(server, nil)
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
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
