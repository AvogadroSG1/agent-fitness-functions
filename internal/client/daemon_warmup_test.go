package client

import (
	"slices"
	"testing"
)

// calm-poc-q8d.6 C# row: a block-enforcement repository must never
// receive an optimistic first C# verdict, so every locally autostarted
// daemon runs with deterministic analyzer warmup blocking.
func TestDaemonStartArgsAlwaysBlockOnWarmup(t *testing.T) {
	args := daemonStartArgs(DaemonStartConfig{Addr: "https://127.0.0.1:7890"})
	if !slices.Contains(args, "--block-on-warmup") {
		t.Fatalf("daemonStartArgs = %v, want --block-on-warmup so first C# checks are deterministic", args)
	}
}
