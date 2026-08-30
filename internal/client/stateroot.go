package client

import (
	"os"
	"path/filepath"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/installer"
)

// governanceRoot is the ADR-0007 machine-scoped root all managed local
// governance state lives under: <installer StateRoot>/governance. This
// replaces the earlier per-repository <repo>/certs and <repo>/configs
// defaults so one local daemon can serve every governed repository on the
// machine. Env selectors keep their override semantics in their callers.
func governanceRoot() string {
	return filepath.Join(installer.StateRoot(os.Getenv), "governance")
}

func governanceCertsDir() string {
	return filepath.Join(governanceRoot(), "certs")
}

func governanceConfigsDir() string {
	return filepath.Join(governanceRoot(), "configs")
}

// ensureGovernanceRoot creates the governance root so a subsequent
// devcerts.Publish against the machine-default certs root finds an existing,
// pinned parent directory.
func ensureGovernanceRoot() error {
	return os.MkdirAll(governanceRoot(), 0o755)
}
