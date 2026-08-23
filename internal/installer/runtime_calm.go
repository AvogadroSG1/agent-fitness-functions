package installer

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// provisionCalmRuntime installs the pinned FINOS CALM CLI into targetDir
// (already created and empty by the caller) by copying the committed
// tools/calm-runtime/{package.json,package-lock.json} from assetDir there and
// running `npm ci` against them, then verifying the result.
//
// ADR-0005 describes this as `npm ci --prefix <targetDir>`; npm ci is run
// here with Dir set to targetDir instead (equivalent effect — npm ci installs
// into the directory containing the package.json/package-lock.json it
// operates on) for robustness across npm versions: `--prefix` behaved
// inconsistently in some npm 11 environments. `npm ls --json --prefix <path>`
// (used by verifyCalmRuntime, unaffected by this) is used exactly as
// ADR-0005 specifies.
//
// --ignore-scripts is passed because the committed lockfile pins zero
// packages with an install script (enforced by
// TestManagedToolPinsHaveNoInstallScripts in pins_drift_test.go); this keeps
// provisioning from silently starting to execute arbitrary npm lifecycle
// scripts if a future dependency bump introduces one.
func provisionCalmRuntime(targetDir, assetDir string) error {
	lockDir := filepath.Join(assetDir, "calm-runtime")
	for _, name := range []string{"package.json", "package-lock.json"} {
		if err := copyFile(filepath.Join(lockDir, name), filepath.Join(targetDir, name)); err != nil {
			return fmt.Errorf("stage %s for CALM CLI provisioning: %w", name, err)
		}
	}

	cmd := exec.Command("npm", "ci", "--ignore-scripts")
	cmd.Dir = targetDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("npm ci (CALM CLI %s): %w\n%s", CALMCLIVersion, err, output)
	}
	return verifyCalmRuntime(targetDir)
}

// verifyCalmRuntime runs `npm ls --json --prefix <path>` per ADR-0005 and
// confirms the resolved @finos/calm-cli dependency matches the pinned
// version — the same check doctor uses to flag a mismatched or incomplete
// CALM install as corrupt.
func verifyCalmRuntime(path string) error {
	if _, err := os.Stat(filepath.Join(path, "node_modules", ".bin", "calm")); err != nil {
		return fmt.Errorf("calm binary missing at %s: %w", path, err)
	}
	cmd := exec.Command("npm", "ls", "--json", "--prefix", path)
	output, _ := cmd.CombinedOutput() // npm ls exits non-zero on extraneous/missing deps; parse what it printed regardless
	var tree struct {
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(output, &tree); err != nil {
		return fmt.Errorf("parse npm ls --json output: %w\n%s", err, output)
	}
	dep, ok := tree.Dependencies["@finos/calm-cli"]
	if !ok {
		return fmt.Errorf("npm ls reports no @finos/calm-cli dependency in %s", path)
	}
	if dep.Version != CALMCLIVersion {
		return fmt.Errorf("npm ls reports @finos/calm-cli %s in %s, want pinned %s", dep.Version, path, CALMCLIVersion)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
