package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/poconnor/calm-poc/internal/analyzer"
	"github.com/poconnor/calm-poc/internal/bridge"
	"github.com/poconnor/calm-poc/internal/sarif"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWithDependencies(args, stdout, stderr, &http.Client{Timeout: 2 * time.Second}, startDaemon)
}

func runWithDependencies(args []string, stdout, stderr io.Writer, client *http.Client, starter func(string) error) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: calm-bridge <serve|check>")
		return 2
	}

	switch args[0] {
	case "serve":
		return runServe(args[1:], stderr)
	case "check":
		if err := runCheck(args[1:], stdout, client, starter); err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			if isUsageError(err) {
				return 2
			}
			return 1
		}
		return 0
	case "baseline":
		if err := runBaseline(args[1:], stdout); err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			if isUsageError(err) {
				return 2
			}
			return 1
		}
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		return 2
	}
}

func runServe(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	addr := flags.String("addr", "localhost:7890", "daemon listen address")
	tlsCert := flags.String("tls-cert", "", "server TLS certificate path")
	tlsKey := flags.String("tls-key", "", "server TLS private key path")
	tlsCA := flags.String("tls-ca", "", "client CA bundle path")
	trustedProxyHeaders := flags.Bool("trusted-proxy-headers", false, "trust X-Client-CN headers from an authenticated proxy")
	trustedProxyClientCNs := flags.String("trusted-proxy-client-cns", "", "comma-separated trusted proxy client certificate common names")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := bridge.ServeWithOptions(ctx, bridge.ServeOptions{
		Addr:      *addr,
		ConfigDir: os.Getenv("CALM_CONFIGS_DIR"),
		Ready:     os.Stdout,
		NewStore:  bridge.NewConfigStore,
		HandlerOptions: bridge.HandlerOptions{
			TrustedProxyHeaders:   *trustedProxyHeaders,
			TrustedProxyClientCNs: splitCommaSeparatedValues(*trustedProxyClientCNs),
		},
		TLS: bridge.ServerTLSConfig{
			CertPath: *tlsCert,
			KeyPath:  *tlsKey,
			CAPath:   *tlsCA,
		},
	}); err != nil && !errors.Is(err, context.Canceled) {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func runCheck(args []string, stdout io.Writer, client *http.Client, starter func(string) error) error {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	addr := flags.String("addr", "http://localhost:7890", "daemon base URL")
	file := flags.String("file", "", "file path being checked")
	repo := flags.String("repo", "", "repository root")
	content := flags.String("content", "", "proposed file content")
	contentFile := flags.String("content-file", "", "path to proposed file content")
	language := flags.String("language", "", "source language")
	staged := flags.Bool("staged", false, "read content from git staged state")
	format := flags.String("format", "json", "output format: json or sarif")
	clientCert := flags.String("client-cert", "", "mTLS client certificate path")
	clientKey := flags.String("client-key", "", "mTLS client private key path")
	clientCA := flags.String("client-ca", "", "server CA bundle path")
	if err := flags.Parse(args); err != nil {
		return usageError{err: err}
	}
	if *file == "" || *repo == "" {
		return usageError{err: errors.New("check requires --file and --repo")}
	}
	validFormats := map[string]bool{"json": true, "sarif": true}
	if !validFormats[*format] {
		return usageError{err: fmt.Errorf("unsupported format %q: use json or sarif", *format)}
	}
	configuredClient, err := configureClientTLS(client, *clientCert, *clientKey, *clientCA)
	if err != nil {
		return err
	}
	if err := ensureDaemon(configuredClient, *addr, starter); err != nil {
		return err
	}
	proposedContent, err := resolveContent(*repo, *file, *content, *contentFile, *staged)
	if err != nil {
		return err
	}
	body, err := postCheck(context.Background(), configuredClient, *addr, bridge.CheckRequest{
		Repo:            *repo,
		File:            *file,
		ProposedContent: proposedContent,
		Language:        *language,
	})
	if err != nil {
		return err
	}
	return writeCheckResponse(stdout, *format, *repo, body)
}

func ensureDaemon(client *http.Client, addr string, starter func(string) error) error {
	if isHealthy(client, addr) {
		return nil
	}
	if err := starter(addr); err != nil {
		return fmt.Errorf("starting daemon: %w", err)
	}
	return waitHealthy(client, addr, 500*time.Millisecond)
}

func postCheck(ctx context.Context, client *http.Client, addr string, request bridge.CheckRequest) ([]byte, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encoding check request: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(addr, "/")+"/check", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building check request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := client.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("posting check request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		message := strings.TrimSpace(string(errBody))
		if message == "" {
			return nil, fmt.Errorf("check failed with HTTP %d", response.StatusCode)
		}
		return nil, fmt.Errorf("check failed with HTTP %d: %s", response.StatusCode, message)
	}
	return io.ReadAll(io.LimitReader(response.Body, 10<<20))
}

func splitCommaSeparatedValues(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

func configureClientTLS(base *http.Client, certPath, keyPath, caPath string) (*http.Client, error) {
	if certPath == "" && keyPath == "" && caPath == "" {
		return base, nil
	}
	if (certPath == "") != (keyPath == "") {
		return nil, usageError{err: errors.New("check requires --client-cert and --client-key together")}
	}
	configured := cloneHTTPClient(base)
	baseTransport := http.DefaultTransport.(*http.Transport)
	if configured.Transport != nil {
		if transport, ok := configured.Transport.(*http.Transport); ok {
			baseTransport = transport
		}
	}
	transport := baseTransport.Clone()
	tlsConfig := transport.TLSClientConfig
	if tlsConfig != nil {
		tlsConfig = tlsConfig.Clone()
	} else {
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	if certPath != "" {
		certificate, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return nil, fmt.Errorf("loading client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	if caPath != "" {
		caContent, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("reading client CA bundle: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caContent) {
			return nil, errors.New("parsing client CA bundle")
		}
		tlsConfig.RootCAs = pool
	}
	transport.TLSClientConfig = tlsConfig
	configured.Transport = transport
	return configured, nil
}

func cloneHTTPClient(base *http.Client) *http.Client {
	if base == nil {
		return &http.Client{Timeout: 2 * time.Second}
	}
	clone := *base
	return &clone
}

func writeCheckResponse(stdout io.Writer, format, repo string, body []byte) error {
	if format != "sarif" {
		_, err := stdout.Write(body)
		return err
	}
	var cr bridge.CheckResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return fmt.Errorf("decoding check response: %w", err)
	}
	return json.NewEncoder(stdout).Encode(sarif.Convert(cr, repo))
}

func resolveContent(repo, file, explicitContent, contentFile string, staged bool) (string, error) {
	if contentFile != "" {
		output, err := os.ReadFile(contentFile)
		if err != nil {
			return "", fmt.Errorf("reading content file %s: %w", contentFile, err)
		}
		return string(output), nil
	}
	if explicitContent != "" {
		return explicitContent, nil
	}
	if staged {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, "git", "-C", repo, "show", ":"+file)
		output, err := command.CombinedOutput()
		if err != nil {
			detail := strings.TrimSpace(string(output))
			if detail == "" {
				return "", fmt.Errorf("reading staged content for %s: %w", file, err)
			}
			return "", fmt.Errorf("reading staged content for %s: %w: %s", file, err, detail)
		}
		return string(output), nil
	}
	path := file
	if !filepath.IsAbs(path) {
		path = filepath.Join(repo, file)
	}
	output, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return string(output), nil
}

func isHealthy(client *http.Client, addr string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(addr, "/")+"/health", nil)
	if err != nil {
		return false
	}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK
}

func waitHealthy(client *http.Client, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isHealthy(client, addr) {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("daemon at %s did not become healthy within %s", addr, timeout)
}

func startDaemon(addr string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	listenAddr := strings.TrimPrefix(strings.TrimPrefix(addr, "http://"), "https://")
	command := exec.Command(executable, "serve", "--addr", listenAddr)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func runBaseline(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("baseline", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	repo := flags.String("repo", "", "repository root")
	language := flags.String("language", "", "source language")
	output := flags.String("output", "", "baseline report output path")
	name := flags.String("name", "", "repository name for the report")
	radon := flags.String("radon", "", "radon executable path")
	roslyn := flags.String("roslyn", "", "Roslyn analyzer executable path")
	if err := flags.Parse(args); err != nil {
		return usageError{err: err}
	}
	if err := requireBaselineArgs(*repo, *language, *output); err != nil {
		return err
	}
	repositoryName := *name
	if repositoryName == "" {
		repositoryName = filepath.Base(*repo)
	}
	roslynPath, err := resolveRoslynPath(*language, *roslyn)
	if err != nil {
		return err
	}
	results, err := analyzer.AnalyzeRepository(context.Background(), *repo, *language, analyzer.RepositoryOptions{
		RadonPath:  *radon,
		RoslynPath: roslynPath,
	})
	if err != nil {
		return err
	}
	if err := analyzer.WriteBaselineReport(*output, repositoryName, *language, results); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "wrote %s (%d files)\n", *output, len(results))
	return err
}

func requireBaselineArgs(repo, language, output string) error {
	for _, f := range []string{repo, language, output} {
		if f == "" {
			return usageError{err: errors.New("baseline requires --repo, --language, and --output")}
		}
	}
	return nil
}

func resolveRoslynPath(language, roslynFlag string) (string, error) {
	if language != "csharp" || roslynFlag != "" {
		return roslynFlag, nil
	}
	return ensureLocalRoslynAnalyzer()
}

func ensureLocalRoslynAnalyzer() (string, error) {
	root, err := projectRoot()
	if err != nil {
		return "", err
	}
	project := filepath.Join(root, "tools", "roslyn-analyzer", "CalmRoslynAnalyzer.csproj")
	executable := filepath.Join(root, "tools", "roslyn-analyzer", "bin", "Debug", "net8.0", "CalmRoslynAnalyzer")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	if info, err := os.Stat(executable); err == nil && !info.IsDir() {
		return executable, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "dotnet", "build", project)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err = command.Run()
	if err != nil {
		detail := strings.TrimSpace(output.String())
		if ctx.Err() != nil {
			return "", errors.Join(ctx.Err(), fmt.Errorf("building local Roslyn analyzer: %w: %s", err, detail))
		}
		return "", fmt.Errorf("building local Roslyn analyzer: %w: %s", err, detail)
	}
	return executable, nil
}

func projectRoot() (string, error) {
	workingDir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for current := workingDir; ; current = filepath.Dir(current) {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("could not find project root from %s", workingDir)
		}
	}
}

type usageError struct {
	err error
}

func (e usageError) Error() string {
	return e.err.Error()
}

func (e usageError) Unwrap() error {
	return e.err
}

func isUsageError(err error) bool {
	var target usageError
	return errors.As(err, &target)
}
