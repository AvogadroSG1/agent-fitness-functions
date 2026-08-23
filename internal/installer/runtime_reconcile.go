package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// reconcileRuntimePointers implements ADR-0005's rollback-reconciliation
// rule: after repointing the binary's own current pointer at restoredVersionDir,
// every runtimes/<tool>/current pointer is reconciled to the pinned version
// that release recorded in its own share/pins.json, not left at whatever
// version happened to be current before the rollback. This treats the binary
// and its pinned runtime set as one atomic rollback unit, per the ADR's
// "why does rollback reconcile runtime pointers" rationale.
//
// A tool named in pins.json whose pinned version is not locally provisioned
// (never provisioned, or removed) is left untouched — reconciliation only
// repoints to an already-verified local version; it does not provision one.
// `doctor` will subsequently report that tool as needing repair.
func reconcileRuntimePointers(root, restoredVersionDir string) error {
	pinsPath := filepath.Join(restoredVersionDir, "share", "pins.json")
	data, err := os.ReadFile(pinsPath)
	if os.IsNotExist(err) {
		// Older or unpackaged releases without a pins manifest: nothing to
		// reconcile against.
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", pinsPath, err)
	}
	var pins map[string]string
	if err := json.Unmarshal(data, &pins); err != nil {
		return fmt.Errorf("parse %s: %w", pinsPath, err)
	}
	for tool, version := range pins {
		toolVersionDir := runtimeVersionDir(root, tool, version)
		if !verifiedSentinelExists(toolVersionDir) {
			// Not locally provisioned at the restored version's pin; doctor
			// will flag this rather than reconciliation silently repointing
			// at an unverified directory.
			continue
		}
		if err := publishCurrent(runtimeRoot(root, tool), toolVersionDir); err != nil {
			return fmt.Errorf("reconcile %s runtime pointer: %w", tool, err)
		}
	}
	return nil
}
