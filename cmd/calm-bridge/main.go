package main

import (
	"bytes"
	"context"
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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/poconnor/calm-poc/internal/analyzer"
	"github.com/poconnor/calm-poc/internal/client"
	"github.com/poconnor/calm-poc/internal/server"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWithDependencies(args, stdout, stderr, &http.Client{Timeout: 2 * time.Second}, client.StartDaemon)
}

func runWithDependencies(args []string, stdout, stderr io.Writer, httpClient *http.Client, starter func(string) error) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: calm-bridge <serve|check>")
		return 2
	}

	switch args[0] {
	case "serve":
		return runServe(args[1:], stderr)
	case "check":
		if err := client.RunCheck(args[1:], stdout, httpClient, starter); err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			if client.IsUsageError(err) {
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
	blockOnWarmup := flags.Bool("block-on-warmup", false, "block first C# check until analyzer is ready instead of optimistic pass")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	rateLimiter, err := buildRateLimiter()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	analyzerTimeout, err := resolveAnalyzerTimeout()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := server.ServeWithOptions(ctx, server.ServeOptions{
		Addr:      *addr,
		ConfigDir: os.Getenv("CALM_CONFIGS_DIR"),
		Ready:     os.Stdout,
		NewStore:  server.NewConfigStore,
		HandlerOptions: server.HandlerOptions{
			TrustedProxyHeaders:   *trustedProxyHeaders,
			TrustedProxyClientCNs: splitCommaSeparatedValues(*trustedProxyClientCNs),
			RateLimiter:           rateLimiter,
		},
		BlockOnWarmup:   *blockOnWarmup,
		AnalyzerTimeout: analyzerTimeout,
		TLS: server.ServerTLSConfig{
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

// buildRateLimiter creates a rate limiter from CALM_RATE_LIMIT (default 100 req/min).
// Returns nil when the env var is explicitly set to 0 (disables rate limiting).
func buildRateLimiter() (server.RateLimiter, error) {
	raw := os.Getenv("CALM_RATE_LIMIT")
	if raw == "" {
		return server.NewFixedWindowRateLimiter(100, time.Minute), nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		return nil, fmt.Errorf("CALM_RATE_LIMIT: expected non-negative integer, got %q", raw)
	}
	if limit == 0 {
		return nil, nil
	}
	return server.NewFixedWindowRateLimiter(limit, time.Minute), nil
}

// resolveAnalyzerTimeout parses CALM_ANALYZER_TIMEOUT (default 30s).
// Returns 0 when the env var is explicitly set to 0 (disables timeout).
func resolveAnalyzerTimeout() (time.Duration, error) {
	raw := os.Getenv("CALM_ANALYZER_TIMEOUT")
	if raw == "" {
		return 30 * time.Second, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("CALM_ANALYZER_TIMEOUT: invalid duration %q: %w", raw, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("CALM_ANALYZER_TIMEOUT: duration must be non-negative, got %q", raw)
	}
	return d, nil
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
