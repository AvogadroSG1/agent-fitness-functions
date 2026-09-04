// Package client owns the commit-time validation client for agent-fitness-functions.
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/sarif"
)

//go:embed hookassets/*
var embeddedHooks embed.FS

// Hook artifact naming. Generated git-hook artifacts carry the product name.
// FINOS CALM surfaces (.calm/, configs/, calm-poc, the calm CLI) are unaffected.
const hookProductPrefix = "agent-fitness-functions"

// predecessorProductPrefix is the immediate predecessor product-name
// generation (ADR-0006 "Migration from predecessor generations"). Assembled
// from fragments, mirroring internal/renamecheck's convention, so tracked
// sources never trip the rename-phase separator-insensitive sweep.
const predecessorProductPrefix = "stack-fitness" + "-functions"

const (
	gitGuardName       = hookProductPrefix + "-git-guard"
	legacyGitGuardName = "calm-git-guard"
	// agentHookName is the installed Edit/Write content-validation hook script; it
	// doubles as the .claude/settings.json command marker for idempotent upserts.
	agentHookName = hookProductPrefix + "-pre-tool-use"
)

// Marker/name history per ADR-0006 "Migration from predecessor generations":
// every product-name generation a hook artifact has ever carried, oldest
// first, checked as a full set-membership test on every install/upgrade so a
// repository onboarded under any prior generation upgrades cleanly in one
// pass. Each of the four families below is exactly one of the ADR's four
// literal arrays.

// gitHookMarkerPrefixes is family #1: git-hook markers ("<prefix> <hook> hook").
var gitHookMarkerPrefixes = []string{"CALM", predecessorProductPrefix, hookProductPrefix}

// sidecarMarkerPrefixes is family #2: sidecar markers ("# <prefix> <hook> hook (sidecar)").
var sidecarMarkerPrefixes = []string{"CALM", predecessorProductPrefix, hookProductPrefix}

// gitGuardNameHistory is family #3: git-guard names. legacyGitGuardName is the
// irregular literal "calm-git-guard" — lowercase and not template-derived the
// way its two successors are — and MUST NOT be "fixed" to match the template,
// since that would stop it matching hooks installed by the original
// calm-bridge generation.
var gitGuardNameHistory = []string{legacyGitGuardName, predecessorProductPrefix + "-git-guard", gitGuardName}

// agentHookNameHistory is family #4: agent Edit/Write hook names. Only two
// generations exist — no CALM-era predecessor ever shipped this hook, so no
// fictitious "calm-pre-tool-use" entry is invented here.
var agentHookNameHistory = []string{predecessorProductPrefix + "-pre-tool-use", agentHookName}

// sidecarNamePrefixHistory parallels sidecarMarkerPrefixes for the sidecar
// FILE name (as opposed to the in-file marker text), so a predecessor
// generation's sidecar script can be located and removed after upgrade. The
// CALM-era entry is lowercase ("calm-"), matching the file-name convention
// used elsewhere (gitGuardNameHistory's "calm-git-guard"), not the uppercase
// marker text.
var sidecarNamePrefixHistory = []string{"calm", predecessorProductPrefix, hookProductPrefix}

func managedHookMarkers(hook string) []string {
	markers := make([]string, len(gitHookMarkerPrefixes))
	for i, prefix := range gitHookMarkerPrefixes {
		markers[i] = prefix + " " + hook + " hook"
	}
	return markers
}

func sidecarHookMarkers(hook string) []string {
	markers := make([]string, len(sidecarMarkerPrefixes))
	for i, prefix := range sidecarMarkerPrefixes {
		markers[i] = "# " + prefix + " " + hook + " hook (sidecar)"
	}
	return markers
}

func sidecarHookMarker(hook string) string {
	return "# " + hookProductPrefix + " " + hook + " hook (sidecar)"
}

// sidecarHookNames returns every generation's sidecar file name for hook,
// oldest first (see sidecarNamePrefixHistory).
func sidecarHookNames(hook string) []string {
	names := make([]string, len(sidecarNamePrefixHistory))
	for i, prefix := range sidecarNamePrefixHistory {
		names[i] = prefix + "-" + hook
	}
	return names
}

func sidecarHookName(hook string) string { return hookProductPrefix + "-" + hook }

// knownOwnerSignature reports whether content carries a recognized co-tenant
// hook-manager signature (ADR-0006 "Migration from predecessor generations" /
// known-owner detection) that may be composed with automatically, without a
// human setting an escape-hatch env var. Beads' own integration marker is
// version-stamped (e.g. "BEGIN BEADS INTEGRATION v1.2.2"); this matches only
// the version-independent substring so a future Beads version bump the
// product has never seen still composes cleanly. No Lefthook installation
// exists in this repository to fixture a generated-header signature against
// (ADR-0006 Open Questions), so Lefthook is recognized by the invocation
// form every Lefthook-generated hook ends with instead: "lefthook run".
func knownOwnerSignature(content []byte) bool {
	return bytes.Contains(content, []byte("BEGIN BEADS INTEGRATION")) ||
		bytes.Contains(content, []byte("lefthook run"))
}

// chainSiblingSuffixes are the rename suffixes under which a dispatcher-style
// host hook keeps the chain stages it delegates to: "<hook>.old" is Lefthook's
// clobber-rename convention (`lefthook install --force` preserves the prior
// hook there), and "<hook>.lefthook" is the forge scaffolder's
// repairBeadsHookChain rename target for Lefthook's own shim.
var chainSiblingSuffixes = []string{".old", ".lefthook"}

// knownOwnerChainSignature extends known-owner detection to dispatcher-style
// host hooks whose recognizable signatures live in sibling chain files rather
// than in the host itself (e.g. forge's beads→lefthook dispatcher, which only
// contains calls to "<hook>.old" and "<hook>.lefthook"). A sibling is
// consulted only when the host content references it by name, so a stale
// sibling beside an unrelated hand-written hook never relaxes the
// refuse-by-default path (ADR-0006: "refuse, not guess").
func knownOwnerChainSignature(hooksDir, hookName string, content []byte) bool {
	for _, suffix := range chainSiblingSuffixes {
		sibling := hookName + suffix
		if !bytes.Contains(content, []byte(sibling)) {
			continue
		}
		siblingContent, err := os.ReadFile(filepath.Join(hooksDir, sibling))
		if err != nil {
			continue
		}
		if knownOwnerSignature(siblingContent) {
			return true
		}
	}
	return false
}

// defaultCheckTimeout is the default duration to wait for a validation check response.
const defaultCheckTimeout = 10 * time.Second

// resolveClientTimeout determines the effective validation check timeout from
// the --timeout flag, AGENT_FITNESS_FUNCTIONS_CLIENT_TIMEOUT environment variable,
// or the default of 10s.
func resolveClientTimeout(flagTimeout string) (time.Duration, error) {
	if flagTimeout != "" {
		d, err := time.ParseDuration(flagTimeout)
		if err != nil {
			return 0, fmt.Errorf("invalid timeout %q: %w", flagTimeout, err)
		}
		if d <= 0 {
			return 0, fmt.Errorf("timeout must be positive, got %s", flagTimeout)
		}
		return d, nil
	}
	if envVal := os.Getenv(envClientTimeout); envVal != "" {
		d, err := time.ParseDuration(envVal)
		if err != nil {
			return 0, fmt.Errorf("invalid %s %q: %w", envClientTimeout, envVal, err)
		}
		if d <= 0 {
			return 0, fmt.Errorf("%s must be positive, got %s", envClientTimeout, envVal)
		}
		return d, nil
	}
	return defaultCheckTimeout, nil
}

// RunCheck validates one file by posting a validation request to the daemon.
func RunCheck(args []string, stdout io.Writer, httpClient *http.Client, starter func(DaemonStartConfig) error) error {
	flags := flag.NewFlagSet("client validate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	addr := flags.String("addr", "https://127.0.0.1:7890", "daemon base URL")
	file := flags.String("file", "", "file path being checked")
	repo := flags.String("repo", "", "repository root")
	content := flags.String("content", "", "proposed file content")
	contentFile := flags.String("content-file", "", "path to proposed file content")
	language := flags.String("language", "", "source language")
	staged := flags.Bool("staged", false, "read content from git staged state")
	format := flags.String("format", "json", "output format: json or sarif")
	timeout := flags.String("timeout", "", "timeout for check validation (default: 10s, env: AGENT_FITNESS_FUNCTIONS_CLIENT_TIMEOUT)")
	clientCert := flags.String("client-cert", "", "mTLS client certificate path")
	clientKey := flags.String("client-key", "", "mTLS client private key path")
	clientCA := flags.String("client-ca", "", "server CA bundle path")
	if err := flags.Parse(args); err != nil {
		return usageError{err: err}
	}
	checkTimeout, err := resolveClientTimeout(*timeout)
	if err != nil {
		return usageError{err: err}
	}
	if _, err := resolveClientTLSMode(*clientCert, *clientKey, *clientCA, ""); err != nil {
		return err
	}
	if *file == "" || *repo == "" {
		return usageError{err: errors.New("client validate requires --file and --repo")}
	}
	validFormats := map[string]bool{"json": true, "sarif": true}
	if !validFormats[*format] {
		return usageError{err: fmt.Errorf("unsupported format %q: use json or sarif", *format)}
	}
	configuredClient, err := establishDaemon(httpClient, *addr, *repo, *file, *clientCert, *clientKey, *clientCA, starter)
	if err != nil {
		return handleValidateFailure(stdout, err, *repo)
	}
	proposedContent, err := resolveContent(*repo, *file, *content, *contentFile, *staged)
	if err != nil {
		return err
	}
	body, err := postCheck(context.Background(), configuredClient, *addr, fitness.ValidationRequest{
		Repo:            *repo,
		File:            *file,
		ProposedContent: proposedContent,
		Language:        *language,
	}, checkTimeout)
	if err != nil {
		return handleValidateFailure(stdout, err, *repo)
	}
	return writeValidationResult(stdout, *format, *repo, body)
}

// RunInstallHooks installs embedded Git and Claude hooks into a repository.
func RunInstallHooks(args []string, stdout, stderr io.Writer) error {
	if len(args) > 1 {
		return usageError{err: errors.New("client install-hooks accepts at most one repository argument")}
	}
	repo := "."
	if len(args) == 1 {
		repo = args[0]
	}
	repoRoot, err := gitOutput(repo, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("resolving repository root: %w", err)
	}
	installer := hookInstaller{
		repoRoot: repoRoot,
		stdout:   stdout,
		stderr:   stderr,
	}
	if err := installer.installGitHook("pre-commit", "hookassets/pre-commit.sh"); err != nil {
		return err
	}
	if err := installer.installGitHook("pre-push", "hookassets/pre-push.sh"); err != nil {
		return err
	}
	if err := installer.installGitGuard(); err != nil {
		return err
	}
	return installer.installAgentHook()
}

type hookInstaller struct {
	repoRoot string
	stdout   io.Writer
	stderr   io.Writer
}

func (installer hookInstaller) installGitHook(hookName, embeddedPath string) error {
	targetHook, err := installer.gitHookPath(hookName)
	if err != nil {
		return err
	}
	hooksDir := filepath.Dir(targetHook)
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Errorf("creating hooks directory: %w", err)
	}
	handled, err := installer.handleExistingGitHook(targetHook, hooksDir, hookName, embeddedPath)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if err := installer.writeEmbeddedExecutable(embeddedPath, targetHook); err != nil {
		return err
	}
	if err := installer.writeFormatter(hooksDir); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(installer.stdout, "installed %s\n", targetHook)
	return nil
}

func (installer hookInstaller) handleExistingGitHook(targetHook, hooksDir, hookName, embeddedPath string) (bool, error) {
	content, exists, err := readExistingHook(targetHook, hookName)
	if err != nil || !exists {
		return false, err
	}
	if hookHasSidecar(content, hookName) {
		return true, installer.refreshHookSidecar(targetHook, hooksDir, hookName, embeddedPath, content)
	}
	if hookIsManaged(content, hookName) {
		return false, nil
	}
	return installer.resolveUnmanagedHook(targetHook, hooksDir, hookName, embeddedPath, content)
}

func readExistingHook(targetHook, hookName string) ([]byte, bool, error) {
	if _, err := os.Stat(targetHook); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("checking existing %s hook: %w", hookName, err)
	}
	content, err := os.ReadFile(targetHook)
	if err != nil {
		return nil, false, fmt.Errorf("reading existing %s hook: %w", hookName, err)
	}
	return content, true, nil
}

func hookHasSidecar(content []byte, hookName string) bool {
	return containsAny(content, sidecarHookMarkers(hookName))
}

func hookIsManaged(content []byte, hookName string) bool {
	return containsAny(content, managedHookMarkers(hookName))
}

func containsAny(content []byte, markers []string) bool {
	for _, marker := range markers {
		if bytes.Contains(content, []byte(marker)) {
			return true
		}
	}
	return false
}

// refreshHookSidecar rewrites the current-generation sidecar file and, when
// content still carries a predecessor generation's sidecar marker, upgrades
// the host file's marker/path in place and removes the stale predecessor
// sidecar script — the calm-poc-phk.7 upgrade path (ADR-0006 "Migration from
// predecessor generations").
func (installer hookInstaller) refreshHookSidecar(targetHook, hooksDir, hookName, embeddedPath string, content []byte) error {
	sidecar := filepath.Join(hooksDir, sidecarHookName(hookName))
	if err := installer.writeEmbeddedExecutable(embeddedPath, sidecar); err != nil {
		return err
	}
	if err := installer.writeFormatter(hooksDir); err != nil {
		return err
	}
	if err := installer.upgradeSidecarHostReferences(targetHook, hookName, content); err != nil {
		return err
	}
	if err := removeStalePredecessorArtifacts(hooksDir, sidecarHookNames(hookName), sidecarHookName(hookName)); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(installer.stdout, "updated %s\n", sidecar)
	return nil
}

// upgradeSidecarHostReferences rewrites a host-owned hook file's sidecar
// marker and path reference from any predecessor generation to the current
// one. It is a no-op once the host already carries the current marker.
func (installer hookInstaller) upgradeSidecarHostReferences(targetHook, hookName string, content []byte) error {
	currentMarker := sidecarHookMarker(hookName)
	if bytes.Contains(content, []byte(currentMarker)) {
		return nil
	}
	hooksDir := filepath.Dir(targetHook)
	currentPath := filepath.Join(hooksDir, sidecarHookName(hookName))
	markers := sidecarHookMarkers(hookName)
	names := sidecarHookNames(hookName)
	updated := content
	changed := false
	for i, marker := range markers {
		if marker == currentMarker || !bytes.Contains(updated, []byte(marker)) {
			continue
		}
		updated = bytes.ReplaceAll(updated, []byte(marker), []byte(currentMarker))
		stalePath := filepath.Join(hooksDir, names[i])
		updated = bytes.ReplaceAll(updated, []byte(fmt.Sprintf("%q", stalePath)), []byte(fmt.Sprintf("%q", currentPath)))
		changed = true
	}
	if !changed {
		return nil
	}
	if err := os.WriteFile(targetHook, updated, 0o755); err != nil {
		return fmt.Errorf("upgrading sidecar reference in %s: %w", targetHook, err)
	}
	return nil
}

// removeStalePredecessorArtifacts deletes any predecessor-generation-named
// file for one artifact family that still exists beside the current one, so
// an upgrade leaves no orphaned predecessor script behind.
func removeStalePredecessorArtifacts(dir string, nameHistory []string, currentName string) error {
	for _, name := range nameHistory {
		if name == currentName {
			continue
		}
		stalePath := filepath.Join(dir, name)
		if err := os.Remove(stalePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("removing stale predecessor artifact %s: %w", stalePath, err)
		}
	}
	return nil
}

func (installer hookInstaller) resolveUnmanagedHook(targetHook, hooksDir, hookName, embeddedPath string, content []byte) (bool, error) {
	if knownOwnerSignature(content) || knownOwnerChainSignature(hooksDir, hookName, content) || os.Getenv("AGENT_FITNESS_FUNCTIONS_HOOK_APPEND") == "1" {
		return true, installer.appendHookSidecar(targetHook, hooksDir, hookName, embeddedPath)
	}
	if os.Getenv("AGENT_FITNESS_FUNCTIONS_HOOK_OVERWRITE") == "1" {
		return false, nil
	}
	_, _ = fmt.Fprintf(installer.stderr, "refusing to overwrite existing unmanaged %s hook: %s\n", hookName, targetHook)
	_, _ = fmt.Fprintln(installer.stderr, "set AGENT_FITNESS_FUNCTIONS_HOOK_OVERWRITE=1 to replace it, or AGENT_FITNESS_FUNCTIONS_HOOK_APPEND=1 to append")
	return true, errors.New("refusing to overwrite existing unmanaged hook")
}

func (installer hookInstaller) appendHookSidecar(targetHook, hooksDir, hookName, embeddedPath string) error {
	sidecar := filepath.Join(hooksDir, sidecarHookName(hookName))
	if err := installer.writeEmbeddedExecutable(embeddedPath, sidecar); err != nil {
		return err
	}
	if err := installer.writeFormatter(hooksDir); err != nil {
		return err
	}
	if err := insertHookSidecarCall(targetHook, hookName, sidecar); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(installer.stdout, "appended %s call to %s (sidecar: %s)\n", hookProductPrefix, targetHook, sidecar)
	return nil
}

// sidecarStatusVar is the shell variable used to capture the delegated
// sidecar call's exit status (ADR-0006 Decision Outcome item 4: the
// delegated call MUST be a checked subprocess call whose failure exits the
// host hook with the sidecar's code, since only the last stage in a chain
// may terminate via exec). Namespaced under the product prefix to make an
// accidental collision with a host hook's own variables vanishingly
// unlikely.
const sidecarStatusVar = "__agent_fitness_functions_sidecar_status"

// sidecarCallBlock renders the marker plus a checked invocation of
// sidecarPath: the sidecar runs, its exact exit status is captured, and a
// nonzero status exits the host hook immediately with that same status —
// before control can reach any later stage, including a terminal exec. In a
// host hook already running under `set -e` (e.g. a forge dispatcher chain
// stage), the sidecar's nonzero status would exit the host at the call site
// itself, before the explicit `if` check below ever runs — that is the same
// propagation outcome the check produces explicitly, so no special-casing of
// `set -e` hosts is needed here.
func sidecarCallBlock(hookName, sidecarPath string) string {
	return fmt.Sprintf(
		"\n%s\n%q\n%s=$?\nif [ \"$%s\" -ne 0 ]; then\n  exit \"$%s\"\nfi\n",
		sidecarHookMarker(hookName), sidecarPath,
		sidecarStatusVar, sidecarStatusVar, sidecarStatusVar,
	)
}

// insertHookSidecarCall inserts a checked call to sidecarPath into the host
// hook at targetHook. Per ADR-0006 Decision Outcome item 3 ("insert
// immediately before the first detected unconditional trailing exit/exec
// statement, never after"), the call is placed before a detected terminal
// exec/exit line so it is never dead code; when no such line is found, it
// falls back to end-of-file append (today's behavior, still correct for
// host files with no terminal statement at all).
func insertHookSidecarCall(targetHook, hookName, sidecarPath string) error {
	content, err := os.ReadFile(targetHook)
	if err != nil {
		return fmt.Errorf("reading %s for sidecar insertion: %w", targetHook, err)
	}
	block := sidecarCallBlock(hookName, sidecarPath)
	offset, found := terminalInsertionPoint(content)
	if !found {
		return appendFile(targetHook, []byte(block))
	}
	updated := make([]byte, 0, len(content)+len(block))
	updated = append(updated, content[:offset]...)
	updated = append(updated, []byte(block)...)
	updated = append(updated, content[offset:]...)
	return os.WriteFile(targetHook, updated, 0o755)
}

// terminalInsertionPoint locates the byte offset of the last substantive
// (non-blank) line in content, so a delegated call can be inserted
// immediately before it when that line is a terminal, unconditional `exec `
// or `exit ` statement (ADR-0006 Decision Outcome item 3). Trailing blank
// lines after that statement are tolerated by construction: they are simply
// part of the preserved tail written back after the inserted block. When
// the last substantive line is a comment, or does not start with `exec ` or
// `exit `, found is false and the caller MUST fall back to end-of-file
// append rather than guess at reachability (ADR-0006: "refuse, not guess").
func terminalInsertionPoint(content []byte) (offset int, found bool) {
	text := string(content)
	lastStart := -1
	var lastLine string
	for lineStart := 0; lineStart <= len(text); {
		newlineIdx := strings.IndexByte(text[lineStart:], '\n')
		var line string
		if newlineIdx == -1 {
			line = text[lineStart:]
		} else {
			line = text[lineStart : lineStart+newlineIdx]
		}
		if strings.TrimSpace(line) != "" {
			lastStart = lineStart
			lastLine = line
		}
		if newlineIdx == -1 {
			break
		}
		lineStart += newlineIdx + 1
	}
	if lastStart == -1 {
		return 0, false
	}
	trimmed := strings.TrimSpace(lastLine)
	if strings.HasPrefix(trimmed, "#") {
		return 0, false
	}
	if strings.HasPrefix(trimmed, "exec ") || strings.HasPrefix(trimmed, "exit ") {
		return lastStart, true
	}
	return 0, false
}

func (installer hookInstaller) installGitGuard() error {
	guardPath, err := installer.gitHookPath(gitGuardName)
	if err != nil {
		return err
	}
	if err := installer.writeEmbeddedExecutable("hookassets/git-guard.sh", guardPath); err != nil {
		return err
	}
	if err := removeStalePredecessorArtifacts(filepath.Dir(guardPath), gitGuardNameHistory, gitGuardName); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(installer.stdout, "installed %s\n", guardPath)
	return installer.applyClaudeHook(gitGuardSpec(gitGuardName))
}

// installAgentHook installs the Edit/Write content-validation hook — the flagship
// affordance that validates a coding agent's proposed file content before it lands.
// The script and its formatter dependency live beside the git-guard sidecar, and a
// second PreToolUse entry (matcher Edit|Write) is registered in .claude/settings.json.
func (installer hookInstaller) installAgentHook() error {
	scriptPath, err := installer.gitHookPath(agentHookName)
	if err != nil {
		return err
	}
	if err := installer.writeEmbeddedExecutable("hookassets/pre-tool-use.sh", scriptPath); err != nil {
		return err
	}
	if err := installer.writeFormatter(filepath.Dir(scriptPath)); err != nil {
		return err
	}
	if err := removeStalePredecessorArtifacts(filepath.Dir(scriptPath), agentHookNameHistory, agentHookName); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(installer.stdout, "installed %s\n", scriptPath)
	return installer.applyClaudeHook(agentHookSpec(agentHookName))
}

// applyClaudeHook idempotently upserts a single PreToolUse entry into
// .claude/settings.json, creating the file and directory when absent and surfacing
// a clean error on malformed JSON (via loadClaudeSettings).
//
// A repo generated by the forge scaffolder (ADR-0006 "forge interop") ships a
// .claude/settings.json that forge's own `forge upgrade` command overwrites
// wholesale on every managed-file sync, silently destroying any PreToolUse
// entry landed there. When forgeManagedClaudeSettings detects that shape,
// the upsert instead targets .claude/settings.local.json — which forge's
// allowlist reconciler only ever rewrites between its own marker strings,
// leaving a PreToolUse hooks section untouched — and any product entry a
// pre-fix install left behind in settings.json is removed so forge's next
// upgrade can no longer clobber it silently.
func (installer hookInstaller) applyClaudeHook(spec claudeHookSpec) error {
	settingsJSONPath := filepath.Join(installer.repoRoot, ".claude", "settings.json")
	settingsPath := settingsJSONPath
	if forgeManagedClaudeSettings(settingsJSONPath) {
		if _, err := removeClaudeHookEntry(settingsJSONPath, spec.markers); err != nil {
			return err
		}
		settingsPath = filepath.Join(installer.repoRoot, ".claude", "settings.local.json")
	}
	settings, err := loadClaudeSettings(settingsPath)
	if err != nil {
		return err
	}
	message := upsertClaudeHook(settings, spec)
	if err := writeClaudeSettings(settingsPath, settings); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(installer.stdout, "%s in %s\n", message, settingsPath)
	return nil
}

// forgeManagedClaudeSettings reports whether the .claude/settings.json file
// at settingsPath is one forge's own `forge upgrade` command rewrites
// wholesale (ADR-0006 "forge interop"): a repo generated by the forge
// scaffolder prompts `forge upgrade --check` at every Claude session start
// via its SessionStart chain, and that command clobbers settings.json
// entirely on every run, so any PreToolUse entry install-hooks landed there
// would be silently destroyed. Detection looks for the literal substrings
// "forge upgrade" and "forge sync-allowlist" WITH the space: forge's own
// hyphenated command names (e.g. "forge-guard", "forge-session-start") MUST
// NOT match, or a hand-written hook merely named after forge would be
// misdetected as forge-managed. A missing or unreadable file reports false.
func forgeManagedClaudeSettings(settingsPath string) bool {
	content, err := os.ReadFile(settingsPath)
	if err != nil {
		return false
	}
	return bytes.Contains(content, []byte("forge upgrade")) || bytes.Contains(content, []byte("forge sync-allowlist"))
}

// removeClaudeHookEntry deletes the PreToolUse entry matching markers from
// the .claude/settings.json at settingsPath, if one is present, and reports
// whether it removed anything. The file is rewritten ONLY when an entry was
// actually removed, so a forge-managed settings.json that never carried a
// product entry is left byte-identical (ADR-0006 "forge interop") — the case
// this cleans up is a pre-fix install-hooks run that landed a product entry
// in settings.json before forge-detection existed.
func removeClaudeHookEntry(settingsPath string, markers []string) (bool, error) {
	settings, err := loadClaudeSettings(settingsPath)
	if err != nil {
		return false, err
	}
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		return false, nil
	}
	preToolUse := preToolUseEntries(hooks)
	index, _, found := findClaudeHookEntry(preToolUse, markers)
	if !found {
		return false, nil
	}
	updated := make([]any, 0, len(preToolUse)-1)
	updated = append(updated, preToolUse[:index]...)
	updated = append(updated, preToolUse[index+1:]...)
	hooks["PreToolUse"] = updated
	if err := writeClaudeSettings(settingsPath, settings); err != nil {
		return false, err
	}
	return true, nil
}

func loadClaudeSettings(settingsPath string) (map[string]any, error) {
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return nil, fmt.Errorf("creating .claude directory: %w", err)
	}
	settings := map[string]any{}
	content, err := os.ReadFile(settingsPath)
	if errors.Is(err, os.ErrNotExist) || len(bytes.TrimSpace(content)) == 0 {
		return settings, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", settingsPath, err)
	}
	if err := json.Unmarshal(content, &settings); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", settingsPath, err)
	}
	return settings, nil
}

func writeClaudeSettings(settingsPath string, settings map[string]any) error {
	encoded, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", settingsPath, err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(settingsPath, encoded, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", settingsPath, err)
	}
	return nil
}

// claudeHookSpec describes one PreToolUse entry to upsert. markers are command
// substrings that identify a previously installed instance of this same hook (so an
// existing entry is updated in place rather than duplicated); name labels it in the
// status message; command is the path used to detect an already-current entry.
type claudeHookSpec struct {
	entry   map[string]any
	markers []string
	name    string
	command string
}

func upsertClaudeHook(settings map[string]any, spec claudeHookSpec) string {
	hooks := ensureHooksSection(settings)
	preToolUse := preToolUseEntries(hooks)
	if index, command, found := findClaudeHookEntry(preToolUse, spec.markers); found {
		preToolUse[index] = spec.entry
		hooks["PreToolUse"] = preToolUse
		if command == spec.command {
			return spec.name + " already configured"
		}
		return "updated " + spec.name + " path"
	}
	hooks["PreToolUse"] = append(preToolUse, spec.entry)
	return "added " + spec.name + " to PreToolUse hooks"
}

// portableHookCommand returns the PreToolUse command to register in the
// tracked .claude/settings.json for the installed hook script named name,
// instead of that script's machine-local absolute path. Claude Code runs
// PreToolUse commands with the project root as the working directory, so
// `git rev-parse --git-path hooks/<name>` re-resolves the real hooks
// directory at invocation time on whichever machine has this repo checked
// out — including under a core.hooksPath redirect, since it delegates to
// git's own resolution rather than re-deriving it. A literal absolute path
// baked into the entry instead would be correct only on the machine that ran
// install-hooks; onboarding the same repo on a second machine would then
// upsert the entry to that machine's path and break the first machine on its
// next pull, because .claude/settings.json is tracked and shared.
func portableHookCommand(name string) string {
	return `"$(git rev-parse --git-path hooks/` + name + `)"`
}

func gitGuardSpec(name string) claudeHookSpec {
	command := portableHookCommand(name)
	return claudeHookSpec{
		entry:   claudeCommandEntry("Bash", command),
		markers: gitGuardNameHistory,
		name:    name,
		command: command,
	}
}

func agentHookSpec(name string) claudeHookSpec {
	command := portableHookCommand(name)
	return claudeHookSpec{
		entry:   claudeCommandEntry("Edit|Write", command),
		markers: agentHookNameHistory,
		name:    name,
		command: command,
	}
}

func ensureHooksSection(settings map[string]any) map[string]any {
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		hooks = map[string]any{}
		settings["hooks"] = hooks
	}
	return hooks
}

func preToolUseEntries(hooks map[string]any) []any {
	preToolUse, ok := hooks["PreToolUse"].([]any)
	if !ok {
		return []any{}
	}
	return preToolUse
}

func claudeCommandEntry(matcher, command string) map[string]any {
	return map[string]any{
		"hooks": []any{map[string]any{
			"command": command,
			"type":    "command",
		}},
		"matcher": matcher,
	}
}

func findClaudeHookEntry(preToolUse []any, markers []string) (int, string, bool) {
	for index, rawEntry := range preToolUse {
		command, ok := hookEntryCommand(rawEntry)
		if !ok {
			continue
		}
		if commandMatchesMarker(command, markers) {
			return index, command, true
		}
	}
	return 0, "", false
}

func commandMatchesMarker(command string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(command, marker) {
			return true
		}
	}
	return false
}

func hookEntryCommand(rawEntry any) (string, bool) {
	entry, ok := rawEntry.(map[string]any)
	if !ok {
		return "", false
	}
	rawHooks, ok := entry["hooks"].([]any)
	if !ok {
		return "", false
	}
	for _, rawHook := range rawHooks {
		hook, ok := rawHook.(map[string]any)
		if !ok {
			continue
		}
		command, _ := hook["command"].(string)
		if command != "" {
			return command, true
		}
	}
	return "", false
}

func (installer hookInstaller) writeFormatter(hooksDir string) error {
	return installer.writeEmbeddedFile("hookassets/format-violations.py", filepath.Join(hooksDir, "format-violations.py"), 0o755)
}

func (installer hookInstaller) writeEmbeddedExecutable(embeddedPath, targetPath string) error {
	return installer.writeEmbeddedFile(embeddedPath, targetPath, 0o755)
}

func (installer hookInstaller) writeEmbeddedFile(embeddedPath, targetPath string, mode fs.FileMode) error {
	content, err := embeddedHooks.ReadFile(embeddedPath)
	if err != nil {
		return fmt.Errorf("reading embedded %s: %w", embeddedPath, err)
	}
	if err := os.WriteFile(targetPath, content, mode); err != nil {
		return fmt.Errorf("writing %s: %w", targetPath, err)
	}
	return nil
}

func (installer hookInstaller) gitHookPath(name string) (string, error) {
	path, err := gitOutput(installer.repoRoot, "rev-parse", "--git-path", "hooks/"+name)
	if err != nil {
		return "", fmt.Errorf("resolving %s hook path: %w", name, err)
	}
	if filepath.IsAbs(path) {
		return path, nil
	}
	return filepath.Join(installer.repoRoot, path), nil
}

func gitOutput(repo string, args ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", err, detail)
	}
	return strings.TrimSpace(string(output)), nil
}

func appendFile(path string, content []byte) (err error) {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("opening %s for append: %w", path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing %s: %w", path, closeErr))
		}
	}()
	if _, err = file.Write(content); err != nil {
		return fmt.Errorf("appending %s: %w", path, err)
	}
	return nil
}

// establishDaemon resolves dev-cert/configs discovery, provisions the zero-config
// local TLS material when applicable, configures the mTLS client, and ensures a
// reachable daemon. It returns the client to use for the validation request.
func establishDaemon(httpClient *http.Client, addr, repo, file, certFlag, keyFlag, caFlag string, starter func(DaemonStartConfig) error) (*http.Client, error) {
	certDir := resolveDevCertDir()
	mode, err := resolveClientTLSMode(certFlag, keyFlag, caFlag, certDir)
	if err != nil {
		return nil, err
	}
	if mode.managed && !isLocalHTTPS(addr) {
		mode = clientTLSMode{}
	}
	material, err := loadClientTLSMode(mode, mode.managed)
	if err != nil {
		return nil, err
	}
	daemonCfg := daemonStartConfigFromMaterial(addr, material)
	configuredClient, err := configureClientTLSMaterial(httpClient, material)
	if err != nil {
		return nil, err
	}
	if err := ensureDaemon(configuredClient, addr, daemonCfg, starter); err != nil {
		return nil, err
	}
	return configuredClient, nil
}

func configureClientTLSMaterial(base *http.Client, material clientTLSMaterial) (*http.Client, error) {
	if !material.mode.managed && (len(material.certificate.Certificate) == 0 || material.roots == nil) {
		return configureTLS(base, material.mode.cert, material.mode.key, material.mode.ca)
	}
	configured := cloneHTTPClient(base)
	transport := cloneTransport(configured)
	tlsConfig := cloneTLSConfig(transport.TLSClientConfig)
	tlsConfig.Certificates = []tls.Certificate{material.certificate}
	tlsConfig.RootCAs = material.roots
	transport.TLSClientConfig = tlsConfig
	configured.Transport = transport
	return configured, nil
}

func ensureDaemon(httpClient *http.Client, addr string, cfg DaemonStartConfig, starter func(DaemonStartConfig) error) error {
	probeErr := probeDaemon(httpClient, addr)
	if probeErr == nil {
		return nil
	}
	// A TLS/certificate handshake failure is not fixed by (re)starting a daemon, and
	// auto-start would race an already-listening server. In managed local mode this
	// client's own dev-cert material has already loaded cleanly by the time the probe
	// runs, so a TLS failure here means the listener on the shared port belongs to
	// someone else's daemon; name that conflict instead of reporting raw cert wording.
	// Outside managed local mode the TLS material is caller-supplied, so a genuine
	// external CA mismatch is still possible and the raw error is preserved.
	if isTLSError(probeErr) {
		if cfg.Local {
			return daemonConflictError{addr: addr, cause: probeErr}
		}
		return probeErr
	}
	if err := starter(cfg); err != nil {
		return fmt.Errorf("starting daemon: %w", err)
	}
	if err := waitHealthy(httpClient, addr, 5*time.Second); err != nil {
		return describeDaemonFailure(cfg, err)
	}
	return nil
}

// describeDaemonFailure annotates a health-wait timeout with what auto-start
// attempted, including the captured daemon log path, so the user sees a
// setup problem (and where to look), not a bare timeout.
func describeDaemonFailure(cfg DaemonStartConfig, cause error) error {
	tlsState := "off"
	if cfg.ManagedRoot != "" {
		tlsState = "on"
	}
	return fmt.Errorf("%w [addr=%s tls=%s dev-cert-dir=%s configs-dir=%s log=%s]", cause, cfg.Addr, tlsState, orNone(cfg.CertDir), orNone(cfg.ConfigsDir), orNone(daemonLogPath(cfg)))
}

func orNone(value string) string {
	if value == "" {
		return "<none>"
	}
	return value
}

func postCheck(ctx context.Context, httpClient *http.Client, addr string, request fitness.ValidationRequest, timeout time.Duration) ([]byte, error) {
	if timeout <= 0 {
		timeout = defaultCheckTimeout
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encoding check request: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(addr, "/")+"/check", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building check request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := httpClient.Do(httpRequest)
	if err != nil {
		return nil, transportError{err: err}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		return nil, httpStatusError{status: response.StatusCode, body: strings.TrimSpace(string(errBody))}
	}
	return io.ReadAll(io.LimitReader(response.Body, 10<<20))
}

func configureTLS(base *http.Client, certPath, keyPath, caPath string) (*http.Client, error) {
	if certPath == "" && keyPath == "" && caPath == "" {
		return base, nil
	}
	if (certPath == "") != (keyPath == "") {
		return nil, usageError{err: errors.New("client validate requires --client-cert and --client-key together")}
	}
	configured := cloneHTTPClient(base)
	transport := cloneTransport(configured)
	tlsConfig, err := buildTLSConfig(transport.TLSClientConfig, certPath, keyPath, caPath)
	if err != nil {
		return nil, err
	}
	transport.TLSClientConfig = tlsConfig
	configured.Transport = transport
	return configured, nil
}

func cloneTransport(httpClient *http.Client) *http.Transport {
	base := http.DefaultTransport.(*http.Transport)
	if httpClient.Transport != nil {
		if transport, ok := httpClient.Transport.(*http.Transport); ok {
			base = transport
		}
	}
	return base.Clone()
}

func buildTLSConfig(existing *tls.Config, certPath, keyPath, caPath string) (*tls.Config, error) {
	tlsConfig := cloneTLSConfig(existing)
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
	return tlsConfig, nil
}

func cloneTLSConfig(existing *tls.Config) *tls.Config {
	if existing != nil {
		return existing.Clone()
	}
	return &tls.Config{MinVersion: tls.VersionTLS12}
}

func cloneHTTPClient(base *http.Client) *http.Client {
	if base == nil {
		return &http.Client{Timeout: 30 * time.Second}
	}
	clone := *base
	return &clone
}

func writeValidationResult(stdout io.Writer, format, repo string, body []byte) error {
	if format != "sarif" {
		_, err := stdout.Write(body)
		return err
	}
	var result fitness.ValidationResult
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("decoding check response: %w", err)
	}
	return json.NewEncoder(stdout).Encode(sarif.Convert(result, repo))
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
		return resolveContentFromGit(repo, file)
	}
	return resolveContentFromDisk(repo, file)
}

func resolveContentFromGit(repo, file string) (string, error) {
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

func resolveContentFromDisk(repo, file string) (string, error) {
	path := file
	if !filepath.IsAbs(path) {
		if _, err := os.Stat(path); err != nil {
			path = filepath.Join(repo, file)
		}
	}
	output, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return string(output), nil
}

func isHealthy(httpClient *http.Client, addr string) bool {
	return probeDaemon(httpClient, addr) == nil
}

// probeDaemon performs one GET /health and returns the underlying error (dial refused,
// TLS handshake, non-200) so callers can distinguish an unreachable server from a
// TLS/cert problem. It returns nil only when the server answers 200.
func probeDaemon(httpClient *http.Client, addr string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(addr, "/")+"/health", nil)
	if err != nil {
		return err
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned HTTP %d", response.StatusCode)
	}
	return nil
}

func waitHealthy(httpClient *http.Client, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isHealthy(httpClient, addr) {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("daemon at %s did not become healthy within %s", addr, timeout)
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

// IsUsageError reports whether err is a client usage error.
func IsUsageError(err error) bool {
	var target usageError
	return errors.As(err, &target)
}
