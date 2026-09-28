package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/analyzer"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/architecture"
)

// RunArchitectureRefresh builds and persists the observed local C# graph.
func RunArchitectureRefresh(args []string, stdout, stderr io.Writer, httpClient *http.Client, starter func(DaemonStartConfig) error) error {
	fs := flag.NewFlagSet("client architecture refresh", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repoFlag := fs.String("repo", "", "repository name")
	addr := fs.String("addr", defaultOnboardAddr, "local daemon base URL")
	pathFlag := fs.String("path", ".", "C# repository path")
	cli := fs.String("roslyn-path", "", "Roslyn analyzer executable")
	if err := fs.Parse(args); err != nil {
		return usageError{err: err}
	}
	if len(fs.Args()) > 0 {
		return usageError{err: errors.New("architecture refresh accepts no positional arguments")}
	}
	root, err := strictArchitectureRoot(*pathFlag)
	if err != nil {
		return err
	}
	repo := *repoFlag
	if repo == "" {
		repo = filepath.Base(root)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	graph, err := analyzer.AnalyzeCSharpRepository(ctx, root, *cli)
	if err != nil {
		return fmt.Errorf("architecture analysis failed: %w", err)
	}
	doc := architecture.Build(graph)
	payload, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	endpoint, err := establishDaemon(httpClient, *addr, repo, filepath.Join(root, "architecture.json"), "", "", "", starter)
	if err != nil {
		return err
	}
	u := endpoint.addr + "/architecture?repo=" + url.QueryEscape(repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := endpoint.client.Do(req)
	if err != nil {
		return fmt.Errorf("uploading architecture baseline: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("uploading architecture baseline: %s: %s", resp.Status, body)
	}
	putETag := resp.Header.Get("ETag")
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	get, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if putETag != "" {
		get.Header.Set("If-Match", putETag)
	}
	accepted, err := endpoint.client.Do(get)
	if err != nil {
		return fmt.Errorf("retrieving accepted architecture baseline: %w", err)
	}
	defer accepted.Body.Close()
	if accepted.StatusCode/100 != 2 {
		return fmt.Errorf("retrieving accepted architecture baseline: %s", accepted.Status)
	}
	raw, err := io.ReadAll(accepted.Body)
	if err != nil {
		return fmt.Errorf("reading accepted architecture baseline: %w", err)
	}
	if _, err := architecture.Validate(raw); err != nil {
		return fmt.Errorf("daemon returned invalid accepted architecture: %w", err)
	}
	_, err = stdout.Write(raw)
	return err
}

func strictArchitectureRoot(path string) (string, error) {
	if path == "" {
		return "", errors.New("architecture repository path is empty")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("architecture repository path %q: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("architecture repository path %q is not a directory", path)
	}
	root, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if gitRoot, gitErr := gitOutput(root, "rev-parse", "--show-toplevel"); gitErr == nil && strings.TrimSpace(gitRoot) != "" {
		return filepath.Clean(strings.TrimSpace(gitRoot)), nil
	}
	return "", fmt.Errorf("architecture path %q is not a Git checkout", path)
}
