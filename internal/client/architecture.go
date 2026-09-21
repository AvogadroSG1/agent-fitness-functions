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
	"path/filepath"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/analyzer"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/architecture"
)

// RunArchitectureRefresh builds and persists the observed local C# graph.
func RunArchitectureRefresh(args []string, stdout, stderr io.Writer, httpClient *http.Client, starter func(DaemonStartConfig) error) error {
	options, err := parseArchitectureRefreshArgs(args)
	if err != nil {
		return err
	}
	return executeArchitectureRefresh(options, stdout, stderr, httpClient, starter)
}

type architectureRefreshOptions struct {
	repo         string
	addr         string
	path         string
	roslynPath   string
	solution     string
	allowPartial bool
}

func parseArchitectureRefreshArgs(args []string) (architectureRefreshOptions, error) {
	fs := flag.NewFlagSet("client architecture refresh", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repoFlag := fs.String("repo", "", "repository name")
	addr := fs.String("addr", defaultOnboardAddr, "local daemon base URL")
	pathFlag := fs.String("path", ".", "C# repository path")
	cli := fs.String("roslyn-path", "", "Roslyn analyzer executable")
	solution := fs.String("solution", "", "repository-relative .sln solution to analyze")
	allowPartial := fs.Bool("allow-partial", false, "allow a partial analysis to replace the accepted baseline")
	if err := fs.Parse(args); err != nil {
		return architectureRefreshOptions{}, usageError{err: err}
	}
	if len(fs.Args()) > 0 {
		return architectureRefreshOptions{}, usageError{err: errors.New("architecture refresh accepts no positional arguments")}
	}
	return architectureRefreshOptions{repo: *repoFlag, addr: *addr, path: *pathFlag, roslynPath: *cli, solution: *solution, allowPartial: *allowPartial}, nil
}

func executeArchitectureRefresh(options architectureRefreshOptions, stdout, stderr io.Writer, httpClient *http.Client, starter func(DaemonStartConfig) error) error {
	root := resolveRepoRoot(options.path, "")
	if root == "" {
		return errors.New("could not locate repository checkout")
	}
	repo := options.repo
	if repo == "" {
		repo = filepath.Base(root)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	payload, err := architectureRefreshPayload(ctx, root, options, stderr)
	if err != nil {
		return err
	}
	return uploadArchitecture(ctx, payload, options, root, repo, stdout, httpClient, starter)
}

func architectureRefreshPayload(ctx context.Context, root string, options architectureRefreshOptions, stderr io.Writer) ([]byte, error) {
	graph, err := analyzer.AnalyzeCSharpRepositoryWithSolution(ctx, root, options.roslynPath, options.solution)
	if err != nil {
		return nil, fmt.Errorf("architecture analysis failed: %w", err)
	}
	doc := architecture.Build(graph)
	if err := reportArchitectureAnalysis(doc, options.allowPartial, stderr); err != nil {
		return nil, err
	}
	payload, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(payload, '\n'), nil
}

func reportArchitectureAnalysis(doc architecture.Document, allowPartial bool, stderr io.Writer) error {
	analysis, ok := doc.Metadata["agent-fitness-functions"].(map[string]any)
	if !ok {
		return nil
	}
	if diagnostics, ok := analysis["diagnostics"].([]any); ok {
		for _, diagnostic := range diagnostics {
			fmt.Fprintf(stderr, "architecture analysis diagnostic: %v\n", diagnostic)
		}
	}
	if completeness, _ := analysis["completeness"].(string); completeness == "partial" && !allowPartial {
		return errors.New("architecture analysis is partial; inspect the emitted document or rerun with --allow-partial to replace the accepted baseline")
	}
	return nil
}

func uploadArchitecture(ctx context.Context, payload []byte, options architectureRefreshOptions, root, repo string, stdout io.Writer, httpClient *http.Client, starter func(DaemonStartConfig) error) error {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	endpoint, err := establishDaemon(httpClient, options.addr, repo, filepath.Join(root, "architecture.json"), "", "", "", starter)
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
	_, err = io.Copy(stdout, resp.Body)
	return err
}
