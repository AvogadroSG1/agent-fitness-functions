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
// ADR-0005 describes this as `npm ci --prefix <targetDir>`; this
// implementation instead runs npm with its working directory set to
// targetDir directly (equivalent effect — npm ci installs into the directory
// containing the package.json/package-lock.json it operates on). The two
// forms differ in this environment: `npm ci --prefix <dir>` run from a
// different cwd fails with a spurious "Missing: <cwd-basename>@<version> from
// lock file" EUSAGE error in the installed npm version, while running npm
// with Dir set to targetDir (equivalent to `cd targetDir && npm ci`) works
// correctly. `npm ls --json --prefix <path>` (used by verifyCalmRuntime,
// unaffected by this quirk) is used exactly as ADR-0005 specifies.
func provisionCalmRuntime(targetDir, assetDir string) error {
	lockDir := filepath.Join(assetDir, "calm-runtime")
	for _, name := range []string{"package.json", "package-lock.json"} {
		if err := copyFile(filepath.Join(lockDir, name), filepath.Join(targetDir, name)); err != nil {
			return fmt.Errorf("stage %s for CALM CLI provisioning: %w", name, err)
		}
	}

	cmd := exec.Command("npm", "ci")
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
