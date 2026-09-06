// Package installer implements the lifecycle subcommands the installed
// agent-fitness-functions binary owns per ADR-0005 (release installer and
// managed runtime): uninstall, upgrade, and rollback. The initial install is a
// small POSIX-shell bootstrap (scripts/install.sh); every subsequent lifecycle
// operation is a subcommand of the binary itself.
//
// This package manages only the product's own binary/helper-script version
// directory under $XDG_STATE_HOME/agent-fitness-functions/. Managed runtime
// components (CALM CLI, the Python venv, the Roslyn analyzer/.NET SDK) are
// later calm-poc-phk.2 slices and are out of scope here.
package installer

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// productDirName is both the state-root leaf directory name and the archive
// name prefix; it must match the naming surface in CLAUDE.md.
const productDirName = "agent-fitness-functions"

// verifiedSentinelName is the completion marker ADR-0005 requires: a version
// directory lacking it is an unverified partial and must never be reused.
const verifiedSentinelName = ".verified"

var archiveNamePattern = regexp.MustCompile(`^agent-fitness-functions-(.+)-darwin-arm64\.tar\.gz$`)

// usageError marks a CLI input error (bad flags, missing required arguments)
// distinct from an operational failure, mirroring internal/client's
// usageError/IsUsageError convention so main can map it to exit code 2.
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

// IsUsageError reports whether err is a CLI usage error rather than an
// operational failure.
func IsUsageError(err error) bool {
	var target usageError
	return errors.As(err, &target)
}

// StateRoot resolves the product-owned installer state root:
// $XDG_STATE_HOME/agent-fitness-functions, defaulting XDG_STATE_HOME to
// ~/.local/state when unset, per ADR-0005.
func StateRoot(getenv func(string) string) string {
	if home := getenv("XDG_STATE_HOME"); home != "" {
		return filepath.Join(home, productDirName)
	}
	return filepath.Join(getenv("HOME"), ".local", "state", productDirName)
}

// cacheRoot resolves the extraction scratch space: $XDG_CACHE_HOME/agent-fitness-functions,
// defaulting XDG_CACHE_HOME to ~/.cache. ADR-0005 requires scratch space to
// live outside the state root so a failed extraction never leaves partial
// state under it.
func cacheRoot(getenv func(string) string) string {
	if home := getenv("XDG_CACHE_HOME"); home != "" {
		return filepath.Join(home, productDirName)
	}
	return filepath.Join(getenv("HOME"), ".cache", productDirName)
}

// RunUninstall removes all product-owned installer state (versions/, current,
// and runtimes/ if present) under the resolved state root. It requires --yes
// and MUST NOT touch anything outside the state root — no governed
// repository's .git/hooks or .claude/settings.json.
func RunUninstall(args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	return RunUninstallBeforeRemoval(args, stdout, stderr, getenv, nil)
}

// RunUninstallBeforeRemoval validates confirmation and the state root before stopping
// an external service. A failed callback MUST preserve every installation artifact.
func RunUninstallBeforeRemoval(args []string, stdout, stderr io.Writer, getenv func(string) string, beforeRemoval func() error) error {
	flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	flags.SetOutput(stderr)
	yes := flags.Bool("yes", false, "confirm removal of all product-owned installer state")
	if err := flags.Parse(args); err != nil {
		return usageError{err: err}
	}
	if flags.NArg() > 0 {
		return usageError{err: errors.New("uninstall accepts no positional arguments")}
	}
	if !*yes {
		return usageError{err: errors.New("uninstall requires --yes to confirm removal of installer state")}
	}

	root := StateRoot(getenv)
	if !filepath.IsAbs(root) || filepath.Base(root) != productDirName {
		return fmt.Errorf("refusing to uninstall: resolved state root %q does not look like a product-owned directory", root)
	}

	if err := validateUninstallRoot(root); err != nil {
		return err
	}
	if beforeRemoval != nil {
		if err := beforeRemoval(); err != nil {
			return fmt.Errorf("uninstall incomplete: history writer stop unconfirmed: %w", err)
		}
	}
	return removeInstallation(root, stdout)
}

func validateUninstallRoot(root string) error {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing to uninstall: state root is not a real directory")
	}
	return nil
}

func removeInstallation(root string, stdout io.Writer) error {
	var removed []string
	for _, name := range []string{"versions", "current", "runtimes"} {
		target := filepath.Join(root, name)
		if _, err := os.Lstat(target); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("inspect installation artifact: %w", err)
		}
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("remove %s: %w", target, err)
		}
		removed = append(removed, target)
	}

	if len(removed) == 0 {
		_, err := fmt.Fprintf(stdout, "nothing to remove under %s\n", root)
		return err
	}
	if _, err := fmt.Fprintln(stdout, "uninstall removed:"); err != nil {
		return err
	}
	for _, target := range removed {
		if _, err := fmt.Fprintf(stdout, "  %s\n", target); err != nil {
			return err
		}
	}
	return nil
}

// RunUpgrade installs a new version through the same verified atomic path
// install.sh uses (checksum verification before extraction, .verified written
// last, atomic current-pointer rename), retaining exactly one predecessor
// version directory. Re-running with an already-current archive is a no-op.
func RunUpgrade(args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	archivePath, version, err := resolveUpgradeInput(args, stderr)
	if err != nil {
		return err
	}
	root := StateRoot(getenv)
	versionsDir := filepath.Join(root, "versions")
	if err := prepareUpgradeDirectory(versionsDir); err != nil {
		return err
	}
	// A missing current installation has no predecessor to retain.
	previousVersion, _ := readCurrentVersion(root)
	if err := installUpgrade(root, archivePath, version, cacheRoot(getenv)); err != nil {
		return err
	}
	keepPrevious := version
	if previousVersion != "" {
		keepPrevious = previousVersion
	}
	if err := pruneOldVersions(versionsDir, version, keepPrevious); err != nil {
		return err
	}
	if previousVersion != "" && previousVersion != version {
		_, err = fmt.Fprintf(stdout, "upgraded to %s (predecessor retained: %s)\n", version, previousVersion)
	} else {
		_, err = fmt.Fprintf(stdout, "upgraded to %s\n", version)
	}
	return err
}

func resolveUpgradeInput(args []string, stderr io.Writer) (string, string, error) {
	flags := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	flags.SetOutput(stderr)
	archivePath := flags.String("archive", "", "release archive path (offline upgrade)")
	checksumsPath := flags.String("checksums", "", "SHA256SUMS manifest path covering the archive")
	if err := flags.Parse(args); err != nil {
		return "", "", usageError{err: err}
	}
	if flags.NArg() > 0 {
		return "", "", usageError{err: errors.New("upgrade accepts no positional arguments")}
	}
	if *archivePath == "" || *checksumsPath == "" {
		return "", "", usageError{err: errors.New("upgrade requires --archive and --checksums")}
	}
	if _, err := os.Stat(*archivePath); err != nil {
		return "", "", fmt.Errorf("archive not found: %w", err)
	}
	if _, err := os.Stat(*checksumsPath); err != nil {
		return "", "", fmt.Errorf("checksums manifest not found: %w", err)
	}

	// Verify BEFORE any state-root mutation: a tampered archive must abort
	// without touching current or any existing version directory.
	if err := verifyArchiveChecksum(*archivePath, *checksumsPath); err != nil {
		return "", "", err
	}
	version, err := parseArchiveVersion(*archivePath)
	return *archivePath, version, err
}

func prepareUpgradeDirectory(versionsDir string) error {
	if err := os.MkdirAll(versionsDir, 0o755); err != nil {
		return fmt.Errorf("create versions directory: %w", err)
	}
	return cleanupUnverifiedPartials(versionsDir)
}

func installUpgrade(root, archivePath, version, cacheDir string) error {
	targetDir := filepath.Join(root, "versions", version)
	if !verifiedSentinelExists(targetDir) {
		if err := publishVersionDirectory(archivePath, targetDir, cacheDir); err != nil {
			return err
		}
	}

	currentVersion, _ := readCurrentVersion(root)
	if currentVersion != version {
		if err := publishCurrent(root, targetDir); err != nil {
			return fmt.Errorf("publish current pointer: %w", err)
		}
	}

	return nil
}

// RunRollback repoints current at the retained predecessor version via the
// same atomic rename used for publication, and fails cleanly (without
// mutating state) when no predecessor is retained.
func RunRollback(args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	flags := flag.NewFlagSet("rollback", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return usageError{err: err}
	}
	if flags.NArg() > 0 {
		return usageError{err: errors.New("rollback accepts no positional arguments")}
	}

	root := StateRoot(getenv)
	currentVersion, err := readCurrentVersion(root)
	if err != nil {
		return fmt.Errorf("rollback failed: no current installation found: %w", err)
	}

	versionsDir := filepath.Join(root, "versions")
	predecessor, err := rollbackPredecessor(versionsDir, currentVersion)
	if err != nil {
		return err
	}
	predecessorDir := filepath.Join(versionsDir, predecessor)
	if err := publishCurrent(root, predecessorDir); err != nil {
		return fmt.Errorf("repoint current pointer: %w", err)
	}
	// Runtime pointers MUST match the restored version's pinned manifest.
	if err := reconcileRuntimePointers(root, predecessorDir); err != nil {
		return fmt.Errorf("reconcile managed runtime pointers: %w", err)
	}
	_, err = fmt.Fprintf(stdout, "rolled back to %s\n", predecessor)
	return err
}

func rollbackPredecessor(versionsDir, currentVersion string) (string, error) {
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		return "", fmt.Errorf("read versions directory: %w", err)
	}
	var candidates []string
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == currentVersion {
			continue
		}
		if verifiedSentinelExists(filepath.Join(versionsDir, entry.Name())) {
			candidates = append(candidates, entry.Name())
		}
	}
	switch len(candidates) {
	case 0:
		return "", errors.New("rollback failed: no predecessor version is retained")
	case 1:
		// proceed
	default:
		return "", fmt.Errorf("rollback failed: multiple predecessor versions present %v; refusing to guess", candidates)
	}

	return candidates[0], nil
}

// RunPublishCurrent is the internal, hidden subcommand `install.sh` delegates
// the current-pointer swap to: `agent-fitness-functions internal
// publish-current --state-root <root> --version <v>`. A shell-only `ln -sfn`
// is NOT atomic — it is an unlink-then-symlink sequence, so a reader can
// observe current missing entirely (ENOENT) between the two steps. Rather
// than reimplementing the atomic temp-symlink-plus-rename dance in POSIX
// shell (a second, harder-to-verify implementation of the same logic),
// install.sh extracts and verifies the version directory itself, then
// delegates the actual pointer swap to that freshly-extracted binary, which
// calls the same publishCurrent every other lifecycle command (upgrade,
// rollback) uses.
func RunPublishCurrent(args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	flags := flag.NewFlagSet("internal publish-current", flag.ContinueOnError)
	flags.SetOutput(stderr)
	stateRoot := flags.String("state-root", "", "resolved installer state root")
	version := flags.String("version", "", "version directory name under versions/ to publish as current")
	if err := flags.Parse(args); err != nil {
		return usageError{err: err}
	}
	if flags.NArg() > 0 {
		return usageError{err: errors.New("publish-current accepts no positional arguments")}
	}
	if *stateRoot == "" || *version == "" {
		return usageError{err: errors.New("publish-current requires --state-root and --version")}
	}
	if err := validateVersionSegment(*version); err != nil {
		return usageError{err: err}
	}
	if filepath.Base(*stateRoot) != productDirName {
		return usageError{err: fmt.Errorf("refusing to publish current: %q does not look like a product-owned state root", *stateRoot)}
	}
	versionDir := filepath.Join(*stateRoot, "versions", *version)
	if !verifiedSentinelExists(versionDir) {
		return fmt.Errorf("refusing to publish unverified version directory %s", versionDir)
	}
	if err := publishCurrent(*stateRoot, versionDir); err != nil {
		return fmt.Errorf("publish current pointer: %w", err)
	}
	_, err := fmt.Fprintf(stdout, "published %s as current\n", *version)
	return err
}

// publishVersionDirectory extracts archivePath into a fresh staging directory
// under cacheDir (ADR-0005's scratch-space requirement), moves the fully
// extracted tree into targetDir, then writes the .verified sentinel last —
// only once the version directory is completely and atomically in place.
func publishVersionDirectory(archivePath, targetDir, cacheDir string) error {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	stagingDir, err := os.MkdirTemp(cacheDir, "install-staging-*")
	if err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(stagingDir) }()

	if err := extractArchive(archivePath, stagingDir); err != nil {
		return fmt.Errorf("extract archive: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(targetDir), 0o755); err != nil {
		return fmt.Errorf("create versions directory: %w", err)
	}
	if err := moveDir(stagingDir, targetDir); err != nil {
		return fmt.Errorf("publish version directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, verifiedSentinelName), nil, 0o644); err != nil {
		return fmt.Errorf("write completion sentinel: %w", err)
	}
	return nil
}

// publishCurrent atomically repoints root/current at versionDir: it creates a
// fresh relative symlink at a temp path and renames it over "current" so
// readers never observe a half-updated pointer.
func publishCurrent(root, versionDir string) error {
	relTarget := filepath.Join("versions", filepath.Base(versionDir))
	tmp := filepath.Join(root, fmt.Sprintf(".current.tmp-%d", time.Now().UnixNano()))
	if err := os.Symlink(relTarget, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(root, "current")); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// readCurrentVersion returns the version name root/current points at, or an
// error if current does not exist or is not a symlink.
func readCurrentVersion(root string) (string, error) {
	target, err := os.Readlink(filepath.Join(root, "current"))
	if err != nil {
		return "", err
	}
	return filepath.Base(target), nil
}

// cleanupUnverifiedPartials removes every versions/ subdirectory lacking the
// .verified completion sentinel before a new install/upgrade attempt begins,
// per ADR-0005: an unverified partial is never reused or left to accumulate.
func cleanupUnverifiedPartials(versionsDir string) error {
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read versions directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(versionsDir, entry.Name())
		if !verifiedSentinelExists(dir) {
			if err := os.RemoveAll(dir); err != nil {
				return fmt.Errorf("remove unverified partial %s: %w", dir, err)
			}
		}
	}
	return nil
}

// pruneOldVersions removes every versions/ subdirectory except keepCurrent and
// keepPrevious, enforcing the exactly-one-retained-predecessor rule.
func pruneOldVersions(versionsDir, keepCurrent, keepPrevious string) error {
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		return fmt.Errorf("read versions directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == keepCurrent || name == keepPrevious {
			continue
		}
		if err := os.RemoveAll(filepath.Join(versionsDir, name)); err != nil {
			return fmt.Errorf("prune old version %s: %w", name, err)
		}
	}
	return nil
}

func verifiedSentinelExists(versionDir string) bool {
	_, err := os.Stat(filepath.Join(versionDir, verifiedSentinelName))
	return err == nil
}

// verifyArchiveChecksum computes archivePath's SHA-256 and compares it against
// the entry in checksumsPath matching the archive's basename (the same
// SHA256SUMS format `shasum -a 256 -c` produces/consumes).
func verifyArchiveChecksum(archivePath, checksumsPath string) error {
	data, err := os.ReadFile(checksumsPath)
	if err != nil {
		return fmt.Errorf("read checksums manifest: %w", err)
	}
	base := filepath.Base(archivePath)
	var expected string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if strings.TrimPrefix(fields[1], "*") == base {
			expected = fields[0]
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("no checksum entry for %s in %s", base, checksumsPath)
	}
	actual, err := sha256File(archivePath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum verification failed for %s: manifest expects %s, computed %s", base, expected, actual)
	}
	return nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func parseArchiveVersion(archivePath string) (string, error) {
	match := archiveNamePattern.FindStringSubmatch(filepath.Base(archivePath))
	if match == nil {
		return "", fmt.Errorf("archive name %q does not match agent-fitness-functions-<version>-darwin-arm64.tar.gz", filepath.Base(archivePath))
	}
	version := match[1]
	if err := validateVersionSegment(version); err != nil {
		return "", fmt.Errorf("archive name %q has an invalid version segment: %w", filepath.Base(archivePath), err)
	}
	return version, nil
}

// validateVersionSegment rejects version strings that cannot safely be used
// as a single path segment under versions/: empty, ".", "..", or anything
// containing a "/" (which could otherwise escape the versions directory when
// joined into a path).
func validateVersionSegment(version string) error {
	if version == "" {
		return errors.New("version must not be empty")
	}
	if version == "." || version == ".." {
		return fmt.Errorf("version %q is not a valid path segment", version)
	}
	if strings.Contains(version, "/") {
		return fmt.Errorf("version %q must not contain %q", version, "/")
	}
	return nil
}

// extractArchive extracts a tar.gz archive into destDir, rejecting any entry
// whose name escapes destDir (path traversal) and skipping non-regular,
// non-directory entries (this slice's archives contain only bin/ files).
func extractArchive(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("open gzip stream: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read archive entry: %w", err)
		}
		if err := extractArchiveEntry(tr, header, destDir); err != nil {
			return err
		}
	}
}

func extractArchiveEntry(reader io.Reader, header *tar.Header, destDir string) error {
	cleanName := filepath.Clean(header.Name)
	if unsafeArchiveName(cleanName) {
		return fmt.Errorf("refusing unsafe archive entry %q", header.Name)
	}
	target := filepath.Join(destDir, cleanName)
	switch header.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(target, 0o755)
	case tar.TypeReg:
		return extractArchiveFile(reader, target, header.FileInfo().Mode().Perm())
	default:
		// v1 archives contain only regular files under bin/; skip anything else.
		return nil
	}
}

func unsafeArchiveName(name string) bool {
	return name == "." || name == ".." || strings.HasPrefix(name, "../") || filepath.IsAbs(name)
}

func extractArchiveFile(reader io.Reader, target string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, reader) //nolint:gosec // archive is checksum-verified before extraction
	return errors.Join(copyErr, out.Close())
}

// moveDir moves src to dst, falling back to a recursive copy when they are on
// different filesystems (os.Rename returns EXDEV).
func moveDir(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !isCrossDeviceError(err) {
		return err
	}
	if err := copyDirRecursive(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

func isCrossDeviceError(err error) bool {
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return errors.Is(linkErr.Err, syscall.EXDEV)
	}
	return false
}

func copyDirRecursive(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}
