// Command rename-verify is the ADR-0002 rename-phase and full-confirmation
// verifier for the predecessor-product to agent-fitness-functions
// product rename (calm-poc-q8d.8). It is an internal governance tool, not
// part of the product's public command surface.
//
// Usage:
//
//	rename-verify [--mode=rename-phase|full] [--repo=<path>]
//
// --mode=rename-phase (the default) runs the checks calm-poc-q8d.8 owns:
// no active-surface predecessor spellings outside protected paths,
// governance.json's exact projection, requirements.lock's line-1-only diff,
// marker/product prefix correctness, and protected-path exactness.
//
// --mode=full runs the ADR-0002 full-confirmation gate: every rename-phase
// check, the separator-insensitive predecessor sweep, and a source-level
// assertion that internal/client/client.go's four ADR-0006 marker-history
// arrays include the immediate predecessor product-name generation
// (calm-poc-phk.7's predecessor-hook upgrade capability). This mode is kept
// structurally separate from rename-phase so the two check sets are never
// conflated.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/renamecheck"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("rename-verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	mode := fs.String("mode", string(renamecheck.ModeRenamePhase), "verification mode: rename-phase or full")
	repo := fs.String("repo", ".", "repository root to verify")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var report renamecheck.Report
	switch renamecheck.Mode(*mode) {
	case renamecheck.ModeRenamePhase:
		report = renamecheck.RunRenamePhase(*repo)
	case renamecheck.ModeFull:
		report = renamecheck.RunFull(*repo)
	default:
		fmt.Fprintf(stderr, "unknown --mode %q; want rename-phase or full\n", *mode)
		return 2
	}

	allPass := true
	for _, c := range report.Checks {
		fmt.Fprintf(stdout, "[%s] %s: %s\n", c.Status, c.Name, c.Detail)
		if c.Status != renamecheck.StatusPass {
			allPass = false
		}
	}

	if !allPass {
		return 1
	}
	return 0
}
