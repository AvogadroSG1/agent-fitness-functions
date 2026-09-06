package client

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// legacyQuarantineSuffix names the pre-ADR-0007 artifacts this migration sets
// aside. Quarantine is a rename, never a delete: the material may hold the only
// copy of a dev CA a developer still wants, and onboarding is not the place to
// destroy state a human can inspect later.
const legacyQuarantineSuffix = ".pre-adr-0007.bak"

// legacyCertsDirName is the repo-local dev-cert root every pre-ADR-0007 repo
// carried. Its foreign CA is what used to flood the shared daemon's log with
// handshake failures, which is why onboard now moves it out of the way.
const legacyCertsDirName = "certs"

// migrateLegacyState quarantines pre-ADR-0007 repo-local governance state under
// repoRoot and reports every action through report. It touches only material it
// positively recognizes as this product's own: an unrecognized certs/ directory
// or a git-tracked caller-repos.json is reported and left alone. A clean tree
// produces no reports at all, so the step is idempotent.
func migrateLegacyState(repoRoot string, report func(string)) error {
	if err := migrateLegacyCerts(repoRoot, report); err != nil {
		return err
	}
	return migrateLegacyCallerBindings(repoRoot, report)
}

// migrateLegacyCerts quarantines <repoRoot>/certs when it matches the managed
// layout this product used to generate there.
func migrateLegacyCerts(repoRoot string, report func(string)) error {
	certsDir := filepath.Join(repoRoot, legacyCertsDirName)
	present, err := pathExists(certsDir)
	if err != nil || !present {
		return err
	}
	if !isManagedCertLayout(certsDir) {
		report(fmt.Sprintf("left alone: %s is not a managed cert layout, so it is not ours to move", certsDir))
		return nil
	}
	target, err := quarantineLegacyPath(certsDir)
	if err != nil {
		return err
	}
	report(fmt.Sprintf("migrated: %s -> %s (pre-ADR-0007 repo-local dev certs; delete once you no longer need them)", certsDir, target))
	return nil
}

// migrateLegacyCallerBindings quarantines an untracked repo-root
// caller-repos.json — the daemon reads the machine governance root's copy
// (ADR-0007), so a repo-local one is stale. A tracked file is a deliberate
// commit, so it is reported and left for a human to remove in a commit.
func migrateLegacyCallerBindings(repoRoot string, report func(string)) error {
	bindings := filepath.Join(repoRoot, callerRepoBindingsFileName)
	present, err := pathExists(bindings)
	if err != nil || !present {
		return err
	}
	if pathIsTrackedByGit(repoRoot, callerRepoBindingsFileName) {
		report(fmt.Sprintf("left alone: %s is tracked by git; remove it in a commit - the daemon reads the machine governance root's copy", bindings))
		return nil
	}
	target, err := quarantineLegacyPath(bindings)
	if err != nil {
		return err
	}
	report(fmt.Sprintf("migrated: %s -> %s (untracked pre-ADR-0007 caller bindings)", bindings, target))
	return nil
}

// pathExists reports whether path exists without following a final symlink, so
// a dangling certs/current still counts as present.
func pathExists(path string) (bool, error) {
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("inspecting %s: %w", path, err)
	}
	return true, nil
}

// isManagedCertLayout reports whether certsDir was produced by this product's
// versioned dev-cert generator: a current -> versions/v-<digest> symlink, or a
// versions/ directory still holding a v-<digest> child after the symlink was
// removed by hand. Anything else is a developer's own directory that happens to
// be called certs, and MUST be left untouched.
func isManagedCertLayout(certsDir string) bool {
	target, err := os.Readlink(filepath.Join(certsDir, "current"))
	if err == nil && strings.HasPrefix(filepath.ToSlash(target), "versions/") {
		return true
	}
	return hasVersionedCertChild(filepath.Join(certsDir, "versions"))
}

// hasVersionedCertChild reports whether versionsDir holds a v-<digest> entry.
func hasVersionedCertChild(versionsDir string) bool {
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "v-") {
			return true
		}
	}
	return false
}

// quarantineLegacyPath renames path aside and returns the new location. A name
// collision from an earlier migration gains a timestamp suffix rather than
// overwriting the earlier quarantine, so repeated onboards accumulate evidence
// instead of destroying it.
func quarantineLegacyPath(path string) (string, error) {
	target, err := freeQuarantinePath(path)
	if err != nil {
		return "", err
	}
	if err := os.Rename(path, target); err != nil {
		return "", fmt.Errorf("quarantining %s: %w", path, err)
	}
	return target, nil
}

// freeQuarantinePath resolves an unused quarantine name for path.
func freeQuarantinePath(path string) (string, error) {
	candidate := path + legacyQuarantineSuffix
	for attempt := 0; attempt < 10; attempt++ {
		taken, err := pathExists(candidate)
		if err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s%s.%s", path, legacyQuarantineSuffix, time.Now().UTC().Format("20060102T150405.000000000Z"))
	}
	return "", fmt.Errorf("no free quarantine name beside %s", path)
}

// pathIsTrackedByGit reports whether relPath is tracked in the repository at
// repoRoot. Only git's "pathspec did not match" status (1) proves a file is
// untracked; every other failure — no repository, no git binary — is treated as
// tracked so an unknown environment can never cause this migration to move a
// file it has not proven is stale.
func pathIsTrackedByGit(repoRoot, relPath string) bool {
	err := exec.Command("git", "-C", repoRoot, "ls-files", "--error-unmatch", "--", relPath).Run()
	if err == nil {
		return true
	}
	var exit *exec.ExitError
	return !errors.As(err, &exit) || exit.ExitCode() != 1
}
