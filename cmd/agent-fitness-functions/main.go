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

	"github.com/AvogadroSG1/agent-fitness-functions/internal/analyzer"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/client"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/installer"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/server"
)

const usageLine = "usage: agent-fitness-functions <client validate|client install-hooks|client onboard|client functions|client resolve-dev-cert-version|server start|baseline|doctor|uninstall|upgrade|rollback|runtime provision|runtime doctor>"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWithDependencies(args, stdout, stderr, &http.Client{Timeout: 30 * time.Second}, client.StartDaemon)
}

func runWithDependencies(args []string, stdout, stderr io.Writer, httpClient *http.Client, starter func(client.DaemonStartConfig) error) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, usageLine)
		return 2
	}

	if code, handled := dispatchPrimaryCommand(args, stdout, stderr, httpClient, starter); handled {
		return code
	}
	if code, handled := dispatchLifecycleCommand(args, stdout, stderr); handled {
		return code
	}
	_, _ = fmt.Fprintf(stderr, "unknown command %q\n", args[0])
	return 2
}

// dispatchPrimaryCommand handles the everyday top-level subcommands (help,
// client, server, doctor, baseline). It reports handled=false for anything
// else so the caller can fall through to dispatchLifecycleCommand.
func dispatchPrimaryCommand(args []string, stdout, stderr io.Writer, httpClient *http.Client, starter func(client.DaemonStartConfig) error) (int, bool) {
	switch args[0] {
	case "--help", "-h", "help":
		_, _ = fmt.Fprintln(stdout, usageLine)
		return 0, true
	case "client":
		return runClient(args[1:], stdout, stderr, httpClient, starter), true
	case "server":
		return runServer(args[1:], stderr), true
	case "doctor":
		return runDoctorCommand(args[1:], stdout, stderr, httpClient), true
	case "baseline":
		return runBaselineCommand(args[1:], stdout, stderr), true
	default:
		return 0, false
	}
}

// dispatchLifecycleCommand handles the ADR-0005 lifecycle/runtime/internal
// subcommands (uninstall, upgrade, rollback, runtime, internal). It reports
// handled=false for anything else so the caller can report "unknown command".
func dispatchLifecycleCommand(args []string, stdout, stderr io.Writer) (int, bool) {
	switch args[0] {
	case "uninstall":
		return runUninstallCommand(args[1:], stdout, stderr), true
	case "upgrade":
		return runUpgradeCommand(args[1:], stdout, stderr), true
	case "rollback":
		return runRollbackCommand(args[1:], stdout, stderr), true
	case "runtime":
		return runRuntimeCommand(args[1:], stdout, stderr), true
	case "internal":
		return runInternalCommand(args[1:], stdout, stderr), true
	default:
		return 0, false
	}
}

func runDoctorCommand(args []string, stdout, stderr io.Writer, httpClient *http.Client) int {
	if err := client.RunDoctor(args, stdout, stderr, httpClient); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		if client.IsUsageError(err) {
			return 2
		}
		return 1
	}
	return 0
}

// runUninstallCommand, runUpgradeCommand, and runRollbackCommand are the
// ADR-0005 lifecycle subcommands: uninstall removes product-owned installer
// state, upgrade installs a new version through the same verified atomic path
// install.sh uses, and rollback repoints current at the retained predecessor.
func runUninstallCommand(args []string, stdout, stderr io.Writer) int {
	if err := installer.RunUninstall(args, stdout, stderr, os.Getenv); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		if installer.IsUsageError(err) {
			return 2
		}
		return 1
	}
	return 0
}

func runUpgradeCommand(args []string, stdout, stderr io.Writer) int {
	if err := installer.RunUpgrade(args, stdout, stderr, os.Getenv); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		if installer.IsUsageError(err) {
			return 2
		}
		return 1
	}
	return 0
}

func runRollbackCommand(args []string, stdout, stderr io.Writer) int {
	if err := installer.RunRollback(args, stdout, stderr, os.Getenv); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		if installer.IsUsageError(err) {
			return 2
		}
		return 1
	}
	return 0
}

// runRuntimeCommand dispatches the ADR-0005 managed-runtime subcommands:
// `runtime provision` (used by install.sh --provision-runtimes) and
// `runtime doctor [--repair]`.
func runRuntimeCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: agent-fitness-functions runtime <provision|doctor>")
		return 2
	}
	switch args[0] {
	case "provision":
		if err := installer.RunRuntimeProvision(args[1:], stdout, stderr, os.Getenv); err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			if installer.IsUsageError(err) {
				return 2
			}
			return 1
		}
		return 0
	case "doctor":
		if err := installer.RunRuntimeDoctor(args[1:], stdout, stderr, os.Getenv); err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			if installer.IsUsageError(err) {
				return 2
			}
			return 1
		}
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "unknown command %q\n", "runtime "+args[0])
		return 2
	}
}

// runInternalCommand dispatches hidden subcommands that exist only for
// delegation from other installer entrypoints, not for direct operator use:
// `internal publish-current` is how scripts/install.sh performs the atomic
// current-pointer swap after extraction, since a shell-only `ln -sfn` is not
// atomic (see installer.RunPublishCurrent).
func runInternalCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: agent-fitness-functions internal <publish-current>")
		return 2
	}
	switch args[0] {
	case "publish-current":
		if err := installer.RunPublishCurrent(args[1:], stdout, stderr, os.Getenv); err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			if installer.IsUsageError(err) {
				return 2
			}
			return 1
		}
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "unknown command %q\n", "internal "+args[0])
		return 2
	}
}

func runBaselineCommand(args []string, stdout, stderr io.Writer) int {
	if err := runBaseline(args, stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		if isUsageError(err) {
			return 2
		}
		return 1
	}
	return 0
}

func runClient(args []string, stdout, stderr io.Writer, httpClient *http.Client, starter func(client.DaemonStartConfig) error) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: agent-fitness-functions client <validate|install-hooks|onboard|functions|resolve-dev-cert-version>")
		return 2
	}
	switch args[0] {
	case "validate":
		return clientExitCode(client.RunCheck(args[1:], stdout, httpClient, starter), stderr)
	case "install-hooks":
		return clientExitCode(client.RunInstallHooks(args[1:], stdout, stderr), stderr)
	case "onboard":
		return clientExitCode(client.RunOnboard(args[1:], stdout, stderr, httpClient, starter), stderr)
	case "functions":
		return clientExitCode(client.RunFunctions(args[1:], stdout, nil), stderr)
	case "resolve-dev-cert-version":
		return clientExitCode(client.RunResolveDevCertVersion(args[1:], stdout), stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "unknown command %q\n", "client "+args[0])
		return 2
	}
}

// clientExitCode maps a client subcommand error to a process exit code: 0 on success,
// InfraErrorExitCode for an infrastructure failure (already reported as a
// machine-readable object on stdout), 2 for usage errors, 1 otherwise. It prints the
// error to stderr for the human-facing cases but not for infra failures, whose
// structured stdout output is what the hooks and agents consume.
func clientExitCode(err error, stderr io.Writer) int {
	if err == nil {
		return 0
	}
	if client.IsInfraError(err) {
		return client.InfraErrorExitCode
	}
	_, _ = fmt.Fprintln(stderr, err)
	if client.IsUsageError(err) {
		return 2
	}
	return 1
}

func runServer(args []string, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: agent-fitness-functions server <start>")
		return 2
	}
	switch args[0] {
	case "start":
		return runServe(args[1:], stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "unknown command %q\n", "server "+args[0])
		return 2
	}
}

func runServe(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("server start", flag.ContinueOnError)
	flags.SetOutput(stderr)
	addr := flags.String("addr", "localhost:7890", "daemon listen address")
	tlsCert := flags.String("tls-cert", "", "server TLS certificate path (overrides AGENT_FITNESS_FUNCTIONS_TLS_CERT)")
	tlsKey := flags.String("tls-key", "", "server TLS private key path (overrides AGENT_FITNESS_FUNCTIONS_TLS_KEY)")
	tlsCA := flags.String("tls-ca", "", "client CA bundle path (overrides AGENT_FITNESS_FUNCTIONS_TLS_CA)")
	configsDir := flags.String("configs-dir", "", "repository configs directory (overrides AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR)")
	trustedProxyHeaders := flags.Bool("trusted-proxy-headers", false, "trust X-Client-CN headers from an authenticated proxy")
	trustedProxyClientCNs := flags.String("trusted-proxy-client-cns", "", "comma-separated trusted proxy client certificate common names")
	blockOnWarmup := flags.Bool("block-on-warmup", false, "block first C# check until analyzer is ready instead of optimistic pass")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	tlsMode, err := resolveServerStartTLSMode(*tlsCert, *tlsKey, *tlsCA, os.Getwd)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		if isUsageError(err) {
			return 2
		}
		return 1
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
		ConfigDir: resolveServerConfigDir(*configsDir),
		Ready:     os.Stdout,
		NewStore:  server.NewConfigStore,
		HandlerOptions: server.HandlerOptions{
			TrustedProxyHeaders:   *trustedProxyHeaders,
			TrustedProxyClientCNs: splitCommaSeparatedValues(*trustedProxyClientCNs),
			RateLimiter:           rateLimiter,
			DisableRegistration:   os.Getenv("AGENT_FITNESS_FUNCTIONS_DISABLE_REGISTRATION") == "1",
		},
		BlockOnWarmup:   *blockOnWarmup,
		AnalyzerTimeout: analyzerTimeout,
		TLS:             tlsMode.TLS,
		ManagedRoot:     tlsMode.ManagedRoot,
		RuntimeDir:      os.Getenv("AGENT_FITNESS_FUNCTIONS_RUNTIME_DIR"),
	}); err != nil && !errors.Is(err, context.Canceled) {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

type serverStartTLSMode struct {
	TLS         server.ServerTLSConfig
	ManagedRoot string
}

func resolveServerStartTLSMode(certFlag, keyFlag, caFlag string, getWorkingDirectory func() (string, error)) (serverStartTLSMode, error) {
	selector := os.Getenv("AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR")
	serverInputs := []string{
		certFlag,
		keyFlag,
		caFlag,
		os.Getenv("AGENT_FITNESS_FUNCTIONS_TLS_CERT"),
		os.Getenv("AGENT_FITNESS_FUNCTIONS_TLS_KEY"),
		os.Getenv("AGENT_FITNESS_FUNCTIONS_TLS_CA"),
	}
	clientInputs := []string{
		os.Getenv("AGENT_FITNESS_FUNCTIONS_CLIENT_CERT"),
		os.Getenv("AGENT_FITNESS_FUNCTIONS_CLIENT_KEY"),
		os.Getenv("AGENT_FITNESS_FUNCTIONS_CLIENT_CA"),
	}
	if selector != "" && (hasNonEmpty(serverInputs) || hasNonEmpty(clientInputs)) {
		return serverStartTLSMode{}, usageError{err: errors.New("AGENT_FITNESS_FUNCTIONS_DEV_CERT_DIR cannot be combined with explicit server or client TLS inputs")}
	}
	if hasNonEmpty(serverInputs) {
		return serverStartTLSMode{TLS: server.ServerTLSConfig{
			CertPath: resolveTLSPath(certFlag, "AGENT_FITNESS_FUNCTIONS_TLS_CERT"),
			KeyPath:  resolveTLSPath(keyFlag, "AGENT_FITNESS_FUNCTIONS_TLS_KEY"),
			CAPath:   resolveTLSPath(caFlag, "AGENT_FITNESS_FUNCTIONS_TLS_CA"),
		}}, nil
	}
	if hasNonEmpty(clientInputs) {
		return serverStartTLSMode{}, nil
	}
	if selector != "" {
		return serverStartTLSMode{ManagedRoot: selector}, nil
	}
	workingDirectory, err := getWorkingDirectory()
	if err != nil {
		return serverStartTLSMode{}, fmt.Errorf("resolve server working directory: %w", err)
	}
	return serverStartTLSMode{ManagedRoot: filepath.Join(workingDirectory, "certs")}, nil
}

func hasNonEmpty(values []string) bool {
	for _, value := range values {
		if value != "" {
			return true
		}
	}
	return false
}

// resolveServerConfigDir prefers the --configs-dir flag, falling back to
// AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR so the auto-started local daemon can be told
// where per-repo configs live without depending on the container default.
func resolveServerConfigDir(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return os.Getenv("AGENT_FITNESS_FUNCTIONS_CONFIGS_DIR")
}

// resolveTLSPath prefers the --tls-* flag, falling back to the matching
// AGENT_FITNESS_FUNCTIONS_TLS_* env var. Without this fallback a container that sets
// only the env vars (as docker-compose.yml/Dockerfile do) would silently listen on
// plain HTTP and reject every authenticated client with a 401.
func resolveTLSPath(flagValue, envName string) string {
	if flagValue != "" {
		return flagValue
	}
	return os.Getenv(envName)
}

// buildRateLimiter creates a rate limiter from AGENT_FITNESS_FUNCTIONS_RATE_LIMIT (default 100 req/min).
// Returns nil when the env var is explicitly set to 0 (disables rate limiting).
func buildRateLimiter() (server.RateLimiter, error) {
	raw := os.Getenv("AGENT_FITNESS_FUNCTIONS_RATE_LIMIT")
	if raw == "" {
		return server.NewFixedWindowRateLimiter(100, time.Minute), nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		return nil, fmt.Errorf("AGENT_FITNESS_FUNCTIONS_RATE_LIMIT: expected non-negative integer, got %q", raw)
	}
	if limit == 0 {
		return nil, nil
	}
	return server.NewFixedWindowRateLimiter(limit, time.Minute), nil
}

// resolveAnalyzerTimeout parses AGENT_FITNESS_FUNCTIONS_ANALYZER_TIMEOUT (default 30s).
// Returns 0 when the env var is explicitly set to 0 (disables timeout).
func resolveAnalyzerTimeout() (time.Duration, error) {
	raw := os.Getenv("AGENT_FITNESS_FUNCTIONS_ANALYZER_TIMEOUT")
	if raw == "" {
		return 30 * time.Second, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("AGENT_FITNESS_FUNCTIONS_ANALYZER_TIMEOUT: invalid duration %q: %w", raw, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("AGENT_FITNESS_FUNCTIONS_ANALYZER_TIMEOUT: duration must be non-negative, got %q", raw)
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
	emitConfig := flags.String("emit-config", "", "additionally write a ready-to-use per-repo governance config to this path")
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
	if _, err := fmt.Fprintf(stdout, "wrote %s (%d files)\n", *output, len(results)); err != nil {
		return err
	}
	if *emitConfig == "" {
		return nil
	}
	return emitOnboardingConfig(*emitConfig, repositoryName, results, stdout)
}

// emitOnboardingConfig derives an enforcement-mode recommendation and threshold-delta
// report from the analysis, writes the per-repo governance config, and prints the
// recommendation plus the global-threshold limitation note.
func emitOnboardingConfig(path, repository string, results []analyzer.AnalysisResult, stdout io.Writer) error {
	rules, err := analyzer.GlobalThresholds()
	if err != nil {
		return err
	}
	recommendation := analyzer.BuildOnboardingRecommendation(repository, results, rules)
	if err := analyzer.WriteOnboardingConfig(path, recommendation.EnforcementMode); err != nil {
		return err
	}
	if err := analyzer.WriteOnboardingReport(stdout, recommendation); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "\nwrote %s (enforcement-mode: %s)\n", path, recommendation.EnforcementMode)
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
