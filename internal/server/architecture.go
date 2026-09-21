package server

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/architecture"
)

const architectureDirName = "architectures"

type ArchitectureCatalog struct {
	Architectures []ArchitectureSummary `json:"architectures"`
}

type ArchitectureSummary struct {
	Repo              string `json:"repo"`
	UpdatedAt         string `json:"updated_at,omitempty"`
	NodeCount         int    `json:"node_count"`
	RelationshipCount int    `json:"relationship_count"`
	Status            string `json:"status"`
	Error             string `json:"error,omitempty"`
}

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
		if !validArchitectureRepo(repo) {
			http.Error(w, "architecture requires a valid repo", http.StatusBadRequest)
			return
		}
		path := filepath.Join(root, repo+".json")
		switch r.Method {
		case http.MethodGet:
			serveArchitectureGet(w, path)
		case http.MethodPut:
			serveArchitecturePut(w, r, root, path)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func serveArchitectureGet(w http.ResponseWriter, path string) {
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
}

func serveArchitecturePut(w http.ResponseWriter, r *http.Request, root, path string) {
	body, err := readArchitectureBody(w, r)
	if err != nil {
		return
	}
	if _, err := architecture.Validate(body); err != nil {
		http.Error(w, "invalid architecture document: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := persistArchitecture(root, path, body); err != nil {
		http.Error(w, "persisting architecture baseline", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

func persistArchitecture(root, path string, body []byte) error {
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(root, ".architecture-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(body); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func architecturesHandler(options HandlerOptions) http.HandlerFunc {
	root := options.ArchitectureRoot
	if root == "" {
		root = filepath.Join(os.TempDir(), "agent-fitness-functions", architectureDirName)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !options.LocalHTTP {
			http.Error(w, "architecture API is supported only in local-http mode", http.StatusNotImplemented)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		catalog, err := readArchitectureCatalog(root)
		if err != nil && !os.IsNotExist(err) {
			http.Error(w, "reading architecture catalog", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(catalog)
	}
}

func readArchitectureCatalog(root string) (ArchitectureCatalog, error) {
	catalog := ArchitectureCatalog{Architectures: []ArchitectureSummary{}}
	entries, err := os.ReadDir(root)
	if err != nil {
		return catalog, err
	}
	for _, entry := range entries {
		if summary, ok := architectureSummary(root, entry); ok {
			catalog.Architectures = append(catalog.Architectures, summary)
		}
	}
	sort.Slice(catalog.Architectures, func(i, j int) bool { return catalog.Architectures[i].Repo < catalog.Architectures[j].Repo })
	return catalog, nil
}

func architectureSummary(root string, entry os.DirEntry) (ArchitectureSummary, bool) {
	if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || filepath.Ext(entry.Name()) != ".json" {
		return ArchitectureSummary{}, false
	}
	info, err := entry.Info()
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return ArchitectureSummary{}, false
	}
	repo := strings.TrimSuffix(entry.Name(), ".json")
	if !validArchitectureRepo(repo) {
		return ArchitectureSummary{}, false
	}
	summary := ArchitectureSummary{Repo: repo, UpdatedAt: info.ModTime().UTC().Format(time.RFC3339), Status: "available"}
	raw, readErr := os.ReadFile(filepath.Join(root, entry.Name()))
	if readErr != nil {
		summary.Status, summary.Error, summary.UpdatedAt = "unavailable", "architecture document cannot be read", ""
		return summary, true
	}
	doc, parseErr := architecture.Validate(raw)
	if parseErr != nil {
		summary.Status, summary.Error, summary.UpdatedAt = "unavailable", "architecture document is invalid", ""
		return summary, true
	}
	summary.NodeCount = len(doc.Nodes)
	summary.RelationshipCount = len(doc.Relationships)
	return summary, true
}

func validArchitectureRepo(repo string) bool {
	return repo != "" && repo != "." && repo != ".." && !strings.ContainsAny(repo, `/\\`) && !strings.ContainsRune(repo, 0)
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
