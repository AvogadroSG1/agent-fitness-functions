package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestArchitectureCatalogListsAvailableAndDamagedDocuments(t *testing.T) {
	root := t.TempDir()
	good := []byte(`{"$schema":"x","nodes":[{"unique-id":"a","node-type":"service","name":"A"}],"relationships":[]}`)
	if err := os.WriteFile(filepath.Join(root, "zeta.json"), good, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "alpha.json"), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".architecture-temp.json"), good, 0600); err != nil {
		t.Fatal(err)
	}
	h := NewHandlerWithOptions(Checker{}, nil, HandlerOptions{LocalHTTP: true, ArchitectureRoot: root})
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/architectures", nil))
	if r.Code != http.StatusOK {
		t.Fatalf("status = %d", r.Code)
	}
	var got ArchitectureCatalog
	if err := json.Unmarshal(r.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Architectures) != 2 || got.Architectures[0].Repo != "alpha" || got.Architectures[1].Repo != "zeta" {
		t.Fatalf("catalog = %+v", got)
	}
	if got.Architectures[0].Status != "unavailable" || got.Architectures[1].NodeCount != 1 {
		t.Fatalf("catalog entries = %+v", got.Architectures)
	}
}

func TestArchitectureBrowserIsLocalReadOnlyAndEmbedded(t *testing.T) {
	h := NewHandlerWithOptions(Checker{}, nil, HandlerOptions{LocalHTTP: true, ArchitectureRoot: t.TempDir()})
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/", nil))
	if r.Code != http.StatusOK || r.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("browser response: %d %q", r.Code, r.Header().Get("Content-Type"))
	}
	asset := httptest.NewRecorder()
	h.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/assets/browser.js", nil))
	if asset.Code != http.StatusOK || asset.Header().Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Fatalf("asset status = %d content-type=%q", asset.Code, asset.Header().Get("Content-Type"))
	}
	for _, name := range regexp.MustCompile(`assets/([^"']+\.js)`).FindAllStringSubmatch(string(r.Body.Bytes()), -1) {
		worker := httptest.NewRecorder()
		h.ServeHTTP(worker, httptest.NewRequest(http.MethodGet, "/assets/"+name[1], nil))
		if worker.Code != http.StatusOK || worker.Header().Get("Content-Type") != "text/javascript; charset=utf-8" {
			t.Fatalf("referenced asset %q: status=%d content-type=%q", name[1], worker.Code, worker.Header().Get("Content-Type"))
		}
	}
	for _, name := range regexp.MustCompile(`assets/([^"']*worker[^"']+\.js)`).FindAllStringSubmatch(string(asset.Body.Bytes()), -1) {
		worker := httptest.NewRecorder()
		h.ServeHTTP(worker, httptest.NewRequest(http.MethodGet, "/assets/"+name[1], nil))
		if worker.Code != http.StatusOK || worker.Header().Get("Content-Type") != "text/javascript; charset=utf-8" {
			t.Fatalf("worker asset %q: status=%d content-type=%q", name[1], worker.Code, worker.Header().Get("Content-Type"))
		}
	}
	head := httptest.NewRecorder()
	h.ServeHTTP(head, httptest.NewRequest(http.MethodHead, "/assets/index.css", nil))
	if head.Code != http.StatusOK || head.Header().Get("Content-Type") != "text/css; charset=utf-8" {
		t.Fatalf("asset HEAD response: %d %q body=%d", head.Code, head.Header().Get("Content-Type"), head.Body.Len())
	}
	post := httptest.NewRecorder()
	h.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/", nil))
	if post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("browser POST status = %d", post.Code)
	}
	missing := httptest.NewRecorder()
	h.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing asset status = %d", missing.Code)
	}
	write := httptest.NewRecorder()
	h.ServeHTTP(write, httptest.NewRequest(http.MethodPut, "/architectures", nil))
	if write.Code != http.StatusMethodNotAllowed {
		t.Fatalf("catalog PUT status = %d", write.Code)
	}
	remote := httptest.NewRecorder()
	NewHandlerWithOptions(Checker{}, nil, HandlerOptions{}).ServeHTTP(remote, httptest.NewRequest(http.MethodGet, "/", nil))
	if remote.Code != http.StatusNotFound {
		t.Fatalf("remote browser status = %d", remote.Code)
	}
}
