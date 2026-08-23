package hooks

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// calm-poc-81s: hooks/ scripts and their embedded twins under
// internal/client/hookassets/ are kept in sync manually with no
// generator; any divergence must fail the suite, not ship silently.
func TestHookScriptsAreByteIdenticalToEmbeddedTwins(t *testing.T) {
	for _, name := range []string{
		"pre-commit.sh",
		"pre-push.sh",
		"pre-tool-use.sh",
		"git-guard.sh",
		"format-violations.py",
	} {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read hooks/%s: %v", name, err)
		}
		twin, err := os.ReadFile(filepath.Join("..", "internal", "client", "hookassets", name))
		if err != nil {
			t.Fatalf("read hookassets twin %s: %v", name, err)
		}
		if !bytes.Equal(source, twin) {
			t.Errorf("hooks/%s and internal/client/hookassets/%s have drifted; the pairs must stay byte-identical", name, name)
		}
	}
}
