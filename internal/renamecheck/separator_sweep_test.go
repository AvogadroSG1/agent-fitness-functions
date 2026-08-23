package renamecheck

import (
	"os/exec"
	"strings"
	"testing"
)

// calm-poc-phk.7: the predecessor product name must not survive in the
// active surface under ANY separator style — the q8d.8 standards review
// found snake_case shell variables the hyphen-only sweep missed.
func TestNoSeparatorInsensitivePredecessorSpellingsRemain(t *testing.T) {
	pattern := "[Ss][Tt][Aa][Cc][Kk][-_][Ff][Ii][Tt][Nn][Ee][Ss][Ss][-_][Ff][Uu][Nn][Cc][Tt][Ii][Oo][Nn][Ss]"
	command := exec.Command("git", "grep", "-lE", pattern, "--",
		":!docs/adr", ":!LEGACY_REFERENCES.md", ":!docs/escalation", ":!.beads",
		":!internal/devcerts/state.go",
		":!internal/devcerts/certification_test.go",
		":!internal/devcerts/lifecycle_test.go",
		":!internal/devcerts/rotation_rejection_test.go",
		// A dated implementation plan (same historical-record category as the
		// docs/escalation/ trail and the other dated docs/superpowers/plans/
		// entries LEGACY_REFERENCES.md already keeps for the calm-bridge
		// rename): it documents the pre-upgrade shell-variable naming this
		// same ticket (calm-poc-phk.7) fixes, so rewriting it would falsify
		// the record rather than the active surface.
		":!docs/superpowers/plans/2026-06-16-install-hooks-naming-and-scheme.md",
	)
	command.Dir = "../.."
	output, _ := command.Output()
	if listing := strings.TrimSpace(string(output)); listing != "" {
		t.Errorf("active surface still spells the predecessor product name (any separator):\n%s", listing)
	}
}
