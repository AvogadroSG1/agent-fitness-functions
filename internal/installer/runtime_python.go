package installer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
)

// provisionPythonRuntime creates a venv directly at targetDir (already
// created empty by the caller) using host python3, then installs radon and
// its pinned, hash-locked dependencies from requirements.lock.
//
// The venv is created directly at its final path rather than staged
// elsewhere and moved into place: a venv's console-script shebangs (bin/pip,
// bin/radon, ...) embed the venv's own absolute path at creation time, so
// relocating the directory afterward would silently break every entry point
// in it.
func provisionPythonRuntime(targetDir, assetDir string) error {
	requirementsLock := filepath.Join(assetDir, "requirements.lock")
	if err := requirePyyamlHashPinned(requirementsLock); err != nil {
		return err
	}

	if output, err := exec.Command("python3", "-m", "venv", targetDir).CombinedOutput(); err != nil {
		return fmt.Errorf("python3 -m venv %s: %w\n%s", targetDir, err, output)
	}

	pip := filepath.Join(targetDir, "bin", "pip")
	installCmd := exec.Command(pip, "install", "--require-hashes", "-r", requirementsLock)
	if output, err := installCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pip install --require-hashes -r %s: %w\n%s", requirementsLock, err, output)
	}
	return verifyPythonRuntime(targetDir)
}

// pyyamlHashEntryPattern matches a requirements.lock "pyyaml==<version>"
// pinned requirement line followed (possibly on later continuation lines)
// by at least one "--hash=" entry, the same layout as the file's existing
// radon/colorama/mando/six entries.
var pyyamlHashEntryPattern = regexp.MustCompile(`(?s)pyyaml==\S+.*?--hash=`)

// requirePyyamlHashPinned fails closed (ADR-0005) rather than falling back to
// an unpinned pyyaml install when requirements.lock lacks a hash-pinned
// pyyaml entry.
func requirePyyamlHashPinned(requirementsLockPath string) error {
	data, err := os.ReadFile(requirementsLockPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", requirementsLockPath, err)
	}
	if !pyyamlHashEntryPattern.Match(data) {
		return fmt.Errorf("%s lacks a hash-pinned pyyaml entry; refusing to provision the Python runtime rather than fall back to an unpinned install", requirementsLockPath)
	}
	return nil
}

// verifyPythonRuntime confirms the venv's radon reports the pinned version —
// the same check doctor uses to flag a missing or corrupt Python runtime.
func verifyPythonRuntime(targetDir string) error {
	radon := filepath.Join(targetDir, "bin", "radon")
	output, err := exec.Command(radon, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s --version: %w\n%s", radon, err, output)
	}
	if !regexp.MustCompile(regexp.QuoteMeta(RadonVersion)).Match(output) {
		return fmt.Errorf("%s --version reported %q, want pinned %s", radon, output, RadonVersion)
	}
	return nil
}
