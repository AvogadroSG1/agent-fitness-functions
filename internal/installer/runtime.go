// Managed runtime provisioning and doctor/repair (ADR-0005 "Managed Tool
// Components and Integrity" and "doctor and Repair"). Each managed tool
// (`calm`, `python`) is versioned identically to the binary: its own
// `runtimes/<tool>/versions/<pinned-version>/` directory with a `.verified`
// completion sentinel and a `runtimes/<tool>/current` atomic pointer,
// published with the same publishCurrent helper the binary version uses.
//
// The Roslyn analyzer is deliberately NOT a runtimes/<tool> component: it
// ships prebuilt and self-contained inside each release's own
// versions/<version>/share/roslyn-analyzer/, so there is nothing to provision
// or repair for it — doctor only verifies it is present and executable.
package installer

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// runtimeTools is the fixed set of provisionable managed runtime components,
// in a stable order so provision/doctor output is deterministic.
var runtimeTools = []string{"calm", "python"}

// runtimeRoot returns root/runtimes/<tool>, the ADR-0005 per-tool state
// directory mirroring the binary's own versions/+current shape.
func runtimeRoot(root, tool string) string {
	return filepath.Join(root, "runtimes", tool)
}

// runtimeVersionDir returns the pinned version directory for tool, e.g.
// root/runtimes/calm/versions/1.40.0.
func runtimeVersionDir(root, tool, version string) string {
	return filepath.Join(runtimeRoot(root, tool), "versions", version)
}

// pinnedRuntimeVersion returns the single pinned version this installer
// build provisions for tool, from the pins.go manifest.
func pinnedRuntimeVersion(tool string) string {
	switch tool {
	case "calm":
		return CALMCLIVersion
	case "python":
		return RadonVersion
	default:
		return ""
	}
}

// defaultAssetDir resolves the installed release's share/ directory (the
// committed calm-runtime package.json/package-lock.json and requirements.lock
// copied there by scripts/package-release.sh) relative to the currently
// running binary: <state-root>/versions/<version>/bin/agent-fitness-functions
// -> <state-root>/versions/<version>/share.
func defaultAssetDir() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve running executable: %w", err)
	}
	real, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		return "", fmt.Errorf("resolve executable symlinks: %w", err)
	}
	versionDir := filepath.Dir(filepath.Dir(real)) // .../bin/x -> .../bin -> version dir
	return filepath.Join(versionDir, "share"), nil
}

// RunRuntimeProvision implements `agent-fitness-functions runtime provision`:
// it provisions every managed runtime tool (or just --only <tool> when
// given) at its pinned version, publishing each tool's current pointer.
// Already-verified components are left untouched (idempotent); pass force
// only through doctor --repair's internal call.
func RunRuntimeProvision(args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	flags := flag.NewFlagSet("runtime provision", flag.ContinueOnError)
	flags.SetOutput(stderr)
	assetsFlag := flags.String("assets", "", "override the release asset directory (default: resolved from the running binary's installed location)")
	only := flags.String("only", "", "comma-separated subset of tools to provision (default: all)")
	if err := flags.Parse(args); err != nil {
		return usageError{err: err}
	}
	if flags.NArg() > 0 {
		return usageError{err: errors.New("runtime provision accepts no positional arguments")}
	}

	assetDir := *assetsFlag
	if assetDir == "" {
		resolved, err := defaultAssetDir()
		if err != nil {
			return fmt.Errorf("resolve release asset directory: %w", err)
		}
		assetDir = resolved
	}

	tools := runtimeTools
	if *only != "" {
		tools = splitCommaSeparatedValues(*only)
	}

	root := StateRoot(getenv)
	for _, tool := range tools {
		if err := provisionRuntimeTool(root, tool, assetDir, false); err != nil {
			return fmt.Errorf("provision %s runtime: %w", tool, err)
		}
		if _, err := fmt.Fprintf(stdout, "provisioned %s runtime %s\n", tool, pinnedRuntimeVersion(tool)); err != nil {
			return err
		}
	}
	return nil
}

func splitCommaSeparatedValues(raw string) []string {
	if raw == "" {
		return nil
	}
	var values []string
	for _, part := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

// provisionRuntimeTool provisions one managed tool at its pinned version.
// When force is false (the normal `runtime provision` path) an already
// `.verified` version directory is trusted and only the current pointer is
// (re)published. When force is true (doctor --repair on a component doctor
// found corrupt) the version directory is removed and recreated even if a
// stale .verified sentinel remains, so a partially-corrupted-but-marked-
// verified install can actually be repaired.
func provisionRuntimeTool(root, tool, assetDir string, force bool) error {
	version := pinnedRuntimeVersion(tool)
	if version == "" {
		return fmt.Errorf("unknown managed runtime tool %q", tool)
	}
	versionsDir := filepath.Join(runtimeRoot(root, tool), "versions")
	if err := os.MkdirAll(versionsDir, 0o755); err != nil {
		return fmt.Errorf("create %s versions directory: %w", tool, err)
	}
	if err := cleanupUnverifiedPartials(versionsDir); err != nil {
		return err
	}

	targetDir := runtimeVersionDir(root, tool, version)
	if force {
		if err := os.RemoveAll(targetDir); err != nil {
			return fmt.Errorf("remove corrupt %s runtime: %w", tool, err)
		}
	}
	if !verifiedSentinelExists(targetDir) {
		if err := os.RemoveAll(targetDir); err != nil {
			return fmt.Errorf("clear partial %s runtime: %w", tool, err)
		}
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return fmt.Errorf("create %s runtime directory: %w", tool, err)
		}
		var provisionErr error
		switch tool {
		case "calm":
			provisionErr = provisionCalmRuntime(targetDir, assetDir)
		case "python":
			provisionErr = provisionPythonRuntime(targetDir, assetDir)
		default:
			provisionErr = fmt.Errorf("unknown managed runtime tool %q", tool)
		}
		if provisionErr != nil {
			return provisionErr
		}
		if err := os.WriteFile(filepath.Join(targetDir, verifiedSentinelName), nil, 0o644); err != nil {
			return fmt.Errorf("write %s completion sentinel: %w", tool, err)
		}
	}
	if err := publishCurrent(runtimeRoot(root, tool), targetDir); err != nil {
		return fmt.Errorf("publish %s current pointer: %w", tool, err)
	}
	return nil
}

// runtimeHealth reports one managed component's doctor result.
type runtimeHealth struct {
	tool       string
	healthy    bool
	detail     string
	repairable bool // true when a failing check can be fixed by re-provisioning
}

// RunRuntimeDoctor implements `agent-fitness-functions runtime doctor
// [--repair]`: it verifies every managed component (the CALM CLI, the Python
// venv, and the shipped Roslyn analyzer) and, without --repair, exits non-
// zero naming exactly which component(s) are missing or corrupt. With
// --repair it re-provisions only the components it found unhealthy, leaving
// healthy components and the published binary version untouched.
func RunRuntimeDoctor(args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	flags := flag.NewFlagSet("runtime doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repair := flags.Bool("repair", false, "re-provision only the components found missing or corrupt")
	if err := flags.Parse(args); err != nil {
		return usageError{err: err}
	}
	if flags.NArg() > 0 {
		return usageError{err: errors.New("runtime doctor accepts no positional arguments")}
	}

	root := StateRoot(getenv)
	results := checkAllRuntimeHealth(root)

	if *repair {
		assetDir, err := defaultAssetDir()
		if err != nil {
			return fmt.Errorf("resolve release asset directory for repair: %w", err)
		}
		for i, result := range results {
			if result.healthy || !result.repairable {
				continue
			}
			if err := provisionRuntimeTool(root, result.tool, assetDir, true); err != nil {
				return fmt.Errorf("repair %s runtime: %w", result.tool, err)
			}
			results[i] = checkRuntimeToolHealth(root, result.tool)
		}
	}

	return reportRuntimeHealth(stdout, results)
}

func checkAllRuntimeHealth(root string) []runtimeHealth {
	results := make([]runtimeHealth, 0, len(runtimeTools)+1)
	tools := append([]string(nil), runtimeTools...)
	sort.Strings(tools)
	for _, tool := range tools {
		results = append(results, checkRuntimeToolHealth(root, tool))
	}
	results = append(results, checkRoslynAnalyzerHealth(root))
	return results
}

func checkRuntimeToolHealth(root, tool string) runtimeHealth {
	version := pinnedRuntimeVersion(tool)
	targetDir := runtimeVersionDir(root, tool, version)
	var err error
	switch tool {
	case "calm":
		err = verifyCalmRuntime(targetDir)
	case "python":
		err = verifyPythonRuntime(targetDir)
	default:
		err = fmt.Errorf("unknown managed runtime tool %q", tool)
	}
	if err != nil {
		return runtimeHealth{tool: tool, healthy: false, detail: err.Error(), repairable: true}
	}
	return runtimeHealth{tool: tool, healthy: true, repairable: true}
}

// checkRoslynAnalyzerHealth verifies the shipped self-contained Roslyn
// analyzer is present and executable in the currently published binary
// version. It is not repairable by `doctor --repair`: the analyzer is
// shipped as part of a release, not independently provisioned, so a missing
// or corrupt analyzer requires a fresh install/upgrade rather than a repair.
func checkRoslynAnalyzerHealth(root string) runtimeHealth {
	const tool = "roslyn-analyzer"
	version, err := readCurrentVersion(root)
	if err != nil {
		return runtimeHealth{tool: tool, healthy: false, detail: fmt.Sprintf("no current binary version installed: %v", err)}
	}
	analyzerDir := filepath.Join(root, "versions", version, "share", "roslyn-analyzer")
	entries, err := os.ReadDir(analyzerDir)
	if err != nil {
		return runtimeHealth{tool: tool, healthy: false, detail: fmt.Sprintf("roslyn analyzer directory missing: %v", err)}
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.Contains(entry.Name(), "RoslynAnalyzer") {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.Mode()&0o111 != 0 {
			return runtimeHealth{tool: tool, healthy: true}
		}
	}
	return runtimeHealth{tool: tool, healthy: false, detail: "no executable Roslyn analyzer found in " + analyzerDir}
}

// reportRuntimeHealth prints one line per checked component and returns an
// error naming every still-unhealthy one (nil when all are healthy).
func reportRuntimeHealth(stdout io.Writer, results []runtimeHealth) error {
	var broken []string
	for _, result := range results {
		status := "OK"
		if !result.healthy {
			status = "CORRUPT"
		}
		if _, err := fmt.Fprintf(stdout, "%-16s %s\n", result.tool, status); err != nil {
			return err
		}
		if !result.healthy {
			if _, err := fmt.Fprintf(stdout, "  %s\n", result.detail); err != nil {
				return err
			}
			broken = append(broken, result.tool)
		}
	}
	if len(broken) > 0 {
		return fmt.Errorf("runtime doctor: corrupt component(s): %s", strings.Join(broken, ", "))
	}
	return nil
}
