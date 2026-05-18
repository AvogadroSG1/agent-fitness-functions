package main

import (
	"bytes"
	"context"
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
	if err := flags.Parse(args); err != nil {
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := bridge.Serve(ctx, *addr); err != nil && !errors.Is(err, context.Canceled) {
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
	language := flags.String("language", "", "source language")
	staged := flags.Bool("staged", false, "read content from git staged state")
	if err := flags.Parse(args); err != nil {
		return usageError{err: err}
	}
	if *file == "" || *repo == "" {
		return usageError{err: errors.New("check requires --file and --repo")}
	}

	if !isHealthy(client, *addr) {
		if err := starter(*addr); err != nil {
			return fmt.Errorf("starting daemon: %w", err)
		}
		if err := waitHealthy(client, *addr, 500*time.Millisecond); err != nil {
			return err
		}
	}

	proposedContent, err := resolveContent(*repo, *file, *content, *staged)
	if err != nil {
		return err
	}
	request := bridge.CheckRequest{
		Repo:            *repo,
		File:            *file,
		ProposedContent: proposedContent,
		Language:        *language,
	}
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encoding check request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(*addr, "/")+"/check", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building check request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := client.Do(httpRequest)
	if err != nil {
		return fmt.Errorf("posting check request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		message := strings.TrimSpace(string(body))
		if message == "" {
			return fmt.Errorf("check failed with HTTP %d", response.StatusCode)
		}
		return fmt.Errorf("check failed with HTTP %d: %s", response.StatusCode, message)
	}
	_, err = io.Copy(stdout, response.Body)
	return err
}

func resolveContent(repo, file, explicitContent string, staged bool) (string, error) {
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
	if *repo == "" || *language == "" || *output == "" {
		return usageError{err: errors.New("baseline requires --repo, --language, and --output")}
	}
	repositoryName := *name
	if repositoryName == "" {
		repositoryName = filepath.Base(*repo)
	}
	roslynPath := *roslyn
	if *language == "csharp" && roslynPath == "" {
		var err error
		roslynPath, err = ensureLocalRoslynAnalyzer()
		if err != nil {
			return err
		}
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
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("building local Roslyn analyzer: %w: %s", err, strings.TrimSpace(string(output)))
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
