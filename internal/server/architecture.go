package server

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/architecture"
)

const architectureDirName = "architectures"

func architectureHandler(options HandlerOptions) http.HandlerFunc {
	root := options.ArchitectureRoot
	if root == "" {
		root = filepath.Join(os.TempDir(), "agent-fitness-functions", architectureDirName)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !options.LocalHTTP {
			http.Error(w, "architecture API is supported only in local-http mode", http.StatusNotImplemented)
			return
		}
		repo := r.URL.Query().Get("repo")
		if repo == "" || strings.ContainsAny(repo, `/\\`) || repo == "." || repo == ".." {
			http.Error(w, "architecture requires a valid repo", http.StatusBadRequest)
			return
		}
		path := filepath.Join(root, repo+".json")
		switch r.Method {
		case http.MethodGet:
			raw, err := os.ReadFile(path)
			if os.IsNotExist(err) {
				http.Error(w, "architecture baseline not found", http.StatusNotFound)
				return
			}
			if err != nil {
				http.Error(w, "reading architecture baseline", 500)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(raw)
		case http.MethodPut:
			body, err := readArchitectureBody(w, r)
			if err != nil {
				return
			}
			if _, err := architecture.Validate(body); err != nil {
				http.Error(w, "invalid architecture document: "+err.Error(), http.StatusBadRequest)
				return
			}
			if err := os.MkdirAll(root, 0755); err != nil {
				http.Error(w, "creating architecture store", 500)
				return
			}
			tmp, err := os.CreateTemp(root, ".architecture-*.tmp")
			if err != nil {
				http.Error(w, "creating architecture temp", 500)
				return
			}
			tmpName := tmp.Name()
			defer os.Remove(tmpName)
			if _, err = tmp.Write(body); err == nil {
				err = tmp.Sync()
			}
			if closeErr := tmp.Close(); err == nil {
				err = closeErr
			}
			if err == nil {
				err = os.Rename(tmpName, path)
			}
			if err != nil {
				http.Error(w, "persisting architecture baseline", 500)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func readArchitectureBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	limited := http.MaxBytesReader(w, r.Body, 20<<20)
	defer limited.Close()
	raw, err := io.ReadAll(limited)
	if err != nil {
		http.Error(w, "invalid architecture request", 400)
		return nil, err
	}
	return raw, nil
}
